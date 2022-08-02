package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

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
    var err error
    sigs := make(chan os.Signal)
    runOnceErr := make(chan error, 1)
    runAlwaysErr := make(chan error)
    depGraph := foreman.buildDependencyGraph()

    if depGraph.isCyclic() {
        errMsg := "Cyclic dependency detected"
        return errors.New(errMsg)
    }

    startList := depGraph.topSort()

    for _, serviceName := range startList {
        service := foreman.services[serviceName]
        if service.runOnce {
            service.runService(runOnceErr)
            err = <- runOnceErr
        } else {
            go service.runService(runAlwaysErr)
            err = <- runAlwaysErr
        }

        if err != nil {
            return err
        }
    }

    signal.Notify(sigs, syscall.SIGINT)
    <-sigs

    return nil
}

func (foreman *Foreman) buildDependencyGraph() dependencyGraph {
    graph := dependencyGraph{}

    for serviceName, service := range foreman.services {
        graph[serviceName] = service.deps
    }

    return graph
}

func (service *Service) runService(errChan chan <- error) {
    serviceExec := exec.Command(service.cmd, service.args...)

    err := serviceExec.Start()
    if err != nil {
        errChan <- err
        return
    }
    errChan <- nil

    syscall.Setpgid(serviceExec.Process.Pid, serviceExec.Process.Pid)
    fmt.Printf("%d %s: process started\n", serviceExec.Process.Pid, service.serviceName)
    go service.checker(serviceExec.Process.Pid)
    serviceExec.Wait()

    for !service.runOnce {
        serviceExec = exec.Command(service.cmd, service.args...)
        serviceExec.Start()
        go service.checker(serviceExec.Process.Pid)
        fmt.Printf("%d %s: process started\n", serviceExec.Process.Pid, service.serviceName)
        serviceExec.Wait()
    }
}

func (service *Service) checker(pid int) {
    ticker := time.NewTicker(checkInterval)
    for {
        <-ticker.C

        err := syscall.Kill(pid, 0)
        if err != nil {
            return
        }
        
        checkExec := exec.Command(service.checks.cmd, service.checks.args...)
        err = checkExec.Run()
        if err != nil {
            syscall.Kill(pid, syscall.SIGINT)
        } else {
            checkExec.Process.Release()
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
