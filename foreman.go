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

    checkInterval = 5 * time.Second

    inActive systemStatus = 0
    active systemStatus = 1
)

type vertixStatus int

type systemStatus int

type dependencyGraph map[string][]string

type Foreman struct {
    services map[string]Service
    status systemStatus
}

type Service struct {
    serviceName string
    pid int
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
    	services: map[string]Service{},
        status: active,
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
        service := Service{
        	serviceName: key,
        }

        parseService(value, &service)
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

    signal.Notify(sigs, syscall.SIGCHLD, syscall.SIGINT)
    for {
        sig := <-sigs
        switch sig {
        case syscall.SIGINT:
            foreman.sigIntHandler()
        case syscall.SIGCHLD:
            foreman.sigChldHandler()
        }
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
    ticker := time.NewTicker(checkInterval)
    service := foreman.services[serviceName]
    serviceExec := exec.Command(service.cmd, service.args...)

    err := serviceExec.Start()
    if err != nil {
        return err
    }
    service.pid = serviceExec.Process.Pid
    foreman.services[serviceName] = service

    fmt.Printf("%d %s: process started\n", service.pid, service.serviceName)
    
    if !service.runOnce {
        go func() {
            for {
                <-ticker.C
                checkExec := exec.Command(service.checks.cmd, service.checks.args...)
                err := checkExec.Run()
                checkExec.Wait()
                if err != nil {
                    syscall.Kill(service.pid, syscall.SIGINT)
                    return
                }
            }
        }()
    }

    return nil
}

func (foreman *Foreman) sigIntHandler() {
    foreman.status = inActive
    for _, service := range foreman.services {
        syscall.Kill(service.pid, syscall.SIGINT)
    }
    os.Exit(0)
}

func (foreman *Foreman) sigChldHandler() {
    for _, service := range foreman.services {
        childProcess, _ := os.FindProcess(service.pid)
        err := syscall.Kill(service.pid, 0)
        if err != nil {
            childProcess.Wait()
            if foreman.status == active && !service.runOnce {
                foreman.startService(service.serviceName)
                fmt.Printf("%d %s: process restarted\n", service.pid, service.serviceName)
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
