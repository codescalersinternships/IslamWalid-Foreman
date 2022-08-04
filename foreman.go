package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/shirou/gopsutil/process"
	"gopkg.in/yaml.v3"
)

const (
    notVisited vertixStatus = 0
    currentlyVisiting vertixStatus = 1
    visited vertixStatus = 2

    checkInterval = 100 * time.Millisecond
)

type vertixStatus int

type dependencyGraph map[string][]string

type Foreman struct {
    services map[string]*Service
    active bool
}

type Service struct {
    serviceName string
    process *os.Process
    cmd string
    runOnce bool
    deps []string
    checks Checks
}

type Checks struct {
    cmd string
    tcpPorts []string
    udpPorts []string
}

func New(procfilePath string) (*Foreman, error) {
    foreman := &Foreman{
    	services: make(map[string]*Service),
    	active:   true,
    }

    procfileData, err := os.ReadFile(procfilePath)
    if err != nil {
        return nil, err
    }

    procfileMap := map[string]map[string]any{}
    err = yaml.Unmarshal(procfileData, procfileMap)
    if err != nil {
        return nil, err
    }

    for key, value := range procfileMap {
        service := &Service{
        	serviceName: key,
        }

        parseService(value, service)
        foreman.services[key] = service
    }

    return foreman, nil
}

func (f *Foreman) Start() error {
    sigs := make(chan os.Signal)
    depGraph := f.buildDependencyGraph()

    if depGraph.isCyclic() {
        errMsg := "Cyclic dependency detected"
        return errors.New(errMsg)
    }

    startList := depGraph.topSort()

    for _, serviceName := range startList {
        err := f.startService(serviceName)
        if err != nil {
            return err
        }
    }

    signal.Notify(sigs, syscall.SIGCHLD, syscall.SIGINT)
    for {
        sig := <- sigs
        switch sig {
        case syscall.SIGINT:
            f.sigIntHandler()
        case syscall.SIGCHLD:
            f.sigChildHandler()
        }
    }
}

func (f *Foreman) buildDependencyGraph() dependencyGraph {
    graph := dependencyGraph{}

    for serviceName, service := range f.services {
        graph[serviceName] = service.deps
    }

    return graph
}

func (f *Foreman) startService(serviceName string) error {
    service := f.services[serviceName]

    serviceExec := exec.Command("bash", "-c", service.cmd)

    err := serviceExec.Start()
    if err != nil {
        return err
    }

    service.process = serviceExec.Process
    f.services[serviceName] = service

    syscall.Setpgid(serviceExec.Process.Pid, serviceExec.Process.Pid)

    fmt.Printf("%d %s: process started\n", service.process.Pid, service.serviceName)

    go service.checks.checker(serviceExec.Process.Pid)

    return nil
}

func (c *Checks) checker(pid int) {
    ticker := time.NewTicker(checkInterval)
    for {
        <-ticker.C

        err := syscall.Kill(pid, 0)
        if err != nil {
            return
        }
        
        err = c.checkCmd()
        if err != nil {
            syscall.Kill(pid, syscall.SIGINT)
        }

        err = c.checkPorts("tcp", pid)
        if err != nil {
            syscall.Kill(pid, syscall.SIGINT)
        }

        err = c.checkPorts("udp", pid)
        if err != nil {
            syscall.Kill(pid, syscall.SIGINT)
        }
    }
}

func (c *Checks) checkCmd() error {
    checkExec := exec.Command("bash", "-c", c.cmd)
    err := checkExec.Run()
    if err != nil {
        return err
    }
    return nil
}

func (c *Checks) checkPorts(portType string, servicePid int) error {
    var ports []string
    switch portType {
    case "tcp":
        ports = c.tcpPorts
    case "udp":
        ports = c.udpPorts
    }

    for _, port := range ports {
        cmd := fmt.Sprintf("netstat -lnptu | grep %s | grep %s -m 1 | awk '{print $7}'", portType, port)
        out, _ := exec.Command("bash", "-c", cmd).Output()
        pid, err := strconv.Atoi(strings.Split(string(out), "/")[0])
        if err != nil || pid != servicePid {
            return err
        }
    }

    return nil
}

func (f *Foreman) sigIntHandler() {
    f.active = false
    for _, service := range f.services {
        syscall.Kill(service.process.Pid, syscall.SIGINT)
    }
    os.Exit(0)
}

func (f *Foreman) sigChildHandler() {
    for _, service := range f.services {
        childProcess, _ := process.NewProcess(int32(service.process.Pid))
        childStatus, _ := childProcess.Status()
        if childStatus == "Z" {
            service.process.Wait()
            fmt.Printf("%d %s: process stopped\n", service.process.Pid, service.serviceName)
            if !service.runOnce && f.active {
                f.startService(service.serviceName)
            }
        }
    }
}

func (g dependencyGraph) isCyclic() bool {
    cyclic := false
    state := make(map[string]vertixStatus)

    var dfs func(string)
    dfs = func(vertix string) {
        if state[vertix] == visited {
            return
        }

        if state[vertix] == currentlyVisiting {
            cyclic = true
            return
        }

        state[vertix] = currentlyVisiting
        for _, child := range g[vertix] {
            dfs(child)
        }
        state[vertix] = visited
    }

    for vertix := range g {
        dfs(vertix)
    }

    return cyclic
}

func (g dependencyGraph) topSort() []string {
    out := make([]string, 0)
    state := make(map[string]vertixStatus)

    var dfs func(string)
    dfs = func(vertix string) {
        if state[vertix] == visited {
            return
        }

        state[vertix] = visited
        for _, child := range g[vertix] {
            dfs(child)
        }
        out = append(out, vertix)
    }

    for vertix := range g {
        dfs(vertix)
    }

    return out
}
