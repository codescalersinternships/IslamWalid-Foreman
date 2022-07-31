package main

type Foreman struct {
    services map[string]Service
}

type Service struct {
    serviceName string
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
