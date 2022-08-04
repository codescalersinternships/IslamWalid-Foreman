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

    checkInterval = 500 * time.Millisecond
)

type vertixStatus int

type dependencyGraph map[string][]string

type Foreman struct {
    services map[string]Service
    active bool
}

type Service struct {
    serviceName string
    active bool
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

// Parse and create a new foreman object.
// it returns error if the file path is wrong or not in yml format.
func New(procfilePath string) (*Foreman, error) {
    foreman := &Foreman{
    	services: make(map[string]Service),
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
        service := parseService(value)
        service.serviceName = key
        foreman.services[key] = service
    }

    return foreman, nil
}

// Start all the services and resolve their dependencies.
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

// Build graph out of services dependencies.
func (f *Foreman) buildDependencyGraph() dependencyGraph {
    graph := dependencyGraph{}

    for serviceName, service := range f.services {
        graph[serviceName] = service.deps
    }

    return graph
}

func (f *Foreman) startService(serviceName string) error {
    service := f.services[serviceName]

    err := f.checkDeps(serviceName)
    if err != nil {
        return errors.New("Broken dependency")
    }

    serviceExec := exec.Command("bash", "-c", service.cmd)

    err = serviceExec.Start()
    if err != nil {
        return err
    }

    service.active = true
    service.process = serviceExec.Process
    f.services[serviceName] = service

    fmt.Printf("%d %s: process started\n", service.process.Pid, service.serviceName)

    go f.checker(serviceName)

    return nil
}

// Perform the checks needed on a specific pid.
func (f *Foreman) checker(serviceName string) {
    service := f.services[serviceName]
    ticker := time.NewTicker(checkInterval)
    for {
        <-ticker.C

        err := syscall.Kill(service.process.Pid, 0)
        if err != nil {
            return
        }

        err = f.checkDeps(serviceName)
        if err != nil {
            syscall.Kill(service.process.Pid, syscall.SIGINT)
        }

        err = service.checkCmd()
        if err != nil {
            syscall.Kill(service.process.Pid, syscall.SIGINT)
        }

        err = service.checkPorts("tcp")
        if err != nil {
            syscall.Kill(service.process.Pid, syscall.SIGINT)
        }

        err = service.checkPorts("udp")
        if err != nil {
            syscall.Kill(service.process.Pid, syscall.SIGINT)
        }
    }
}

func (f *Foreman) checkDeps(serviceName string) error {
    service := f.services[serviceName]

    for _, depName := range service.deps {
        depService := f.services[depName]
        if !depService.active {
            return errors.New("Broken dependency")
        }
    }

    return nil
}

// Handles incoming SIGINT.
func (f *Foreman) sigIntHandler() {
    f.active = false
    for _, service := range f.services {
        syscall.Kill(service.process.Pid, syscall.SIGINT)
    }
    os.Exit(0)
}

// Handles incoming SIGCHLD.
func (f *Foreman) sigChildHandler() {
    for serviceName, service := range f.services {
        childProcess, _ := process.NewProcess(int32(service.process.Pid))
        childStatus, _ := childProcess.Status()
        if childStatus == "Z" {
            service.active = false
            service.process.Wait()
            fmt.Printf("%d %s: process stopped\n", service.process.Pid, service.serviceName)
            if !service.runOnce && f.active {
                f.startService(service.serviceName)
            }
            f.services[serviceName] = service
        }
    }
}

// Perform the command in the checks.
func (s *Service) checkCmd() error {
    checkExec := exec.Command("bash", "-c", s.checks.cmd)
    err := checkExec.Run()
    if err != nil {
        return err
    }
    return nil
}

// Checks all ports in the checks.
func (s *Service) checkPorts(portType string) error {
    var ports []string
    switch portType {
    case "tcp":
        ports = s.checks.tcpPorts
    case "udp":
        ports = s.checks.udpPorts
    }

    for _, port := range ports {
        cmd := fmt.Sprintf("netstat -lnptu | grep %s | grep %s -m 1 | awk '{print $7}'", portType, port)
        out, _ := exec.Command("bash", "-c", cmd).Output()
        pid, err := strconv.Atoi(strings.Split(string(out), "/")[0])
        if err != nil || pid != s.process.Pid {
            return err
        }
    }

    return nil
}

// Check if graph is cyclic.
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

// Topologically sort the dependency graph.
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
