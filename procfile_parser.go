package main

import "fmt"

func parseService(serviceMap map[string]any, out *Service) {
    for key, value := range serviceMap {
        switch key {
        case "cmd":
            out.cmd = value.(string)
        case "run_once":
            out.runOnce = value.(bool)
        case "deps":
            out.deps = parseDeps(value)
        case "checks":
            checks := Checks{}
            parseCheck(value, &checks)
            out.checks = checks
        }
    }
}

func parseDeps(deps any) []string {
    var resultList []string
    depsList := deps.([]any)

    for _, dep := range depsList {
        resultList = append(resultList, dep.(string))
    }

    return resultList
}

func parseCheck(check any, out *Checks)  {
    checkMap := check.(map[string]any)

    for key, value := range checkMap {
        switch key {
        case "cmd":
            out.cmd = value.(string)
        case "tcp_ports":
            out.tcpPorts = parsePorts(value)
        case "udp_ports":
            out.udpPorts = parsePorts(value)
        }
    }
}

func parsePorts(ports any) []string {
    var resultList []string
    portsList := ports.([]any)

    for _, port := range portsList {
        resultList = append(resultList, fmt.Sprint(port.(int)))
    }
    
    return resultList
}
