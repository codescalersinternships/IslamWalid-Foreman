package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
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
}

type Service struct {
    serviceName string
    process *os.Process
    cmd string
    args []string
    runOnce bool
    deps []string
    checks Checks
}

type Checks struct {
    cmd string
    args []string
    tcpPorts []string
    udpPorts []string
}

func New(procfilePath string) (*Foreman, error) {
    foreman := &Foreman{
    	services: make(map[string]*Service),
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

func (foreman *Foreman) Start() error {
    sigs := make(chan os.Signal)
    depGraph := foreman.buildDependencyGraph()

    if depGraph.isCyclic() {
        errMsg := "Cyclic dependency detected"
        return errors.New(errMsg)
    }

    startList := depGraph.topSort()

    for _, serviceName := range startList {
        err := foreman.startService(serviceName)
        if err != nil {
            return err
        }
    }

    signal.Notify(sigs, syscall.SIGCHLD)
    for {
        <-sigs
        foreman.sigChildHandler()
    }
}

func (foreman *Foreman) buildDependencyGraph() dependencyGraph {
    graph := dependencyGraph{}

    for serviceName, service := range foreman.services {
        graph[serviceName] = service.deps
    }

    return graph
}

func (foreman *Foreman) startService(serviceName string) error {
    service := foreman.services[serviceName]

    serviceExec := exec.Command(service.cmd, service.args...)

    err := serviceExec.Start()
    if err != nil {
        return err
    }

    service.process = serviceExec.Process
    foreman.services[serviceName] = service

    syscall.Setpgid(serviceExec.Process.Pid, serviceExec.Process.Pid)

    fmt.Printf("%d %s: process started\n", service.process.Pid, service.serviceName)

    go service.checks.checker(serviceExec.Process.Pid)

    return nil
}

func (check *Checks) checker(pid int) {
    ticker := time.NewTicker(checkInterval)
    for {
        <-ticker.C

        err := syscall.Kill(pid, 0)
        if err != nil {
            return
        }
        
        checkExec := exec.Command(check.cmd, check.args...)
        err = checkExec.Run()
        if err != nil {
            syscall.Kill(pid, syscall.SIGINT)
        }
    }
}

func (foreman *Foreman) sigChildHandler() {
    for _, service := range foreman.services {
        childProcess, _ := process.NewProcess(int32(service.process.Pid))
        childStatus, _ := childProcess.Status()
        if childStatus == "Z" {
            service.process.Wait()
            fmt.Printf("%d %s: process stopped\n", service.process.Pid, service.serviceName)
            if !service.runOnce {
                foreman.startService(service.serviceName)
            }
        }
    }
}

func (graph dependencyGraph) isCyclic() bool {
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
        for _, child := range graph[vertix] {
            dfs(child)
        }
        state[vertix] = visited
    }

    for vertix := range graph {
        dfs(vertix)
    }

    return cyclic
}

func (graph dependencyGraph) topSort() []string {
    out := make([]string, 0)
    state := make(map[string]vertixStatus)

    var dfs func(string)
    dfs = func(vertix string) {
        if state[vertix] == visited {
            return
        }

        state[vertix] = visited
        for _, child := range graph[vertix] {
            dfs(child)
        }
        out = append(out, vertix)
    }

    for vertix := range graph {
        dfs(vertix)
    }

    return out
}
