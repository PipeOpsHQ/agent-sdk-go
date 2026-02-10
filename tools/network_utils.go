package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"time"
)

type networkUtilsArgs struct {
	Action  string `json:"action"`            // ping, port_check, port_scan, resolve
	Host    string `json:"host"`              // target host
	Port    int    `json:"port,omitempty"`    // for port_check
	Ports   string `json:"ports,omitempty"`   // for port_scan: "80,443,8080" or "1-1024"
	Count   int    `json:"count,omitempty"`   // ping count
	Timeout int    `json:"timeout,omitempty"` // seconds
}

type networkResult struct {
	Action  string `json:"action"`
	Host    string `json:"host"`
	Success bool   `json:"success"`
	Details any    `json:"details"`
	Error   string `json:"error,omitempty"`
}

type pingDetail struct {
	Reachable bool   `json:"reachable"`
	Latency   string `json:"latency"`
	Error     string `json:"error,omitempty"`
}

type portCheckDetail struct {
	Port   int    `json:"port"`
	Open   bool   `json:"open"`
	Banner string `json:"banner,omitempty"`
}

type portScanDetail struct {
	OpenPorts   []int `json:"openPorts"`
	ClosedPorts int   `json:"closedPorts"`
	Total       int   `json:"total"`
}

func NewNetworkUtils() Tool {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"ping", "port_check", "port_scan", "resolve"},
				"description": "Network operation to perform.",
			},
			"host": map[string]any{
				"type":        "string",
				"description": "Target hostname or IP address.",
			},
			"port": map[string]any{
				"type":        "integer",
				"description": "Port number for port_check action.",
				"minimum":     1,
				"maximum":     65535,
			},
			"ports": map[string]any{
				"type":        "string",
				"description": "Ports to scan. Comma-separated (80,443,8080) or common preset ('common', 'web', 'db').",
			},
			"count": map[string]any{
				"type":        "integer",
				"description": "Number of ping attempts. Defaults to 3.",
				"minimum":     1,
				"maximum":     10,
			},
			"timeout": map[string]any{
				"type":        "integer",
				"description": "Timeout per operation in seconds. Defaults to 5.",
				"minimum":     1,
				"maximum":     30,
			},
		},
		"required": []string{"action", "host"},
	}

	return NewFuncTool(
		"network_utils",
		"Network utilities: ping hosts, check ports, scan port ranges, resolve hostnames.",
		schema,
		func(ctx context.Context, args json.RawMessage) (any, error) {
			var in networkUtilsArgs
			if err := json.Unmarshal(args, &in); err != nil {
				return nil, fmt.Errorf("invalid network_utils args: %w", err)
			}
			if in.Host == "" {
				return nil, fmt.Errorf("host is required")
			}
			return executeNetworkUtil(ctx, in)
		},
	)
}

func executeNetworkUtil(ctx context.Context, in networkUtilsArgs) (*networkResult, error) {
	timeout := in.Timeout
	if timeout <= 0 {
		timeout = 5
	}

	result := &networkResult{Action: in.Action, Host: in.Host}

	switch in.Action {
	case "ping":
		count := in.Count
		if count <= 0 {
			count = 3
		}
		details := make([]pingDetail, 0, count)
		allOK := true
		for i := 0; i < count; i++ {
			d := tcpPing(in.Host, timeout)
			if !d.Reachable {
				allOK = false
			}
			details = append(details, d)
		}
		result.Success = allOK
		result.Details = details

	case "port_check":
		if in.Port == 0 {
			return nil, fmt.Errorf("port is required for port_check")
		}
		detail := checkPort(in.Host, in.Port, timeout)
		result.Success = detail.Open
		result.Details = detail

	case "port_scan":
		ports := parsePorts(in.Ports)
		if len(ports) == 0 {
			ports = commonPorts()
		}
		if len(ports) > 100 {
			ports = ports[:100] // cap for safety
		}
		open := make([]int, 0)
		closed := 0
		for _, p := range ports {
			d := checkPort(in.Host, p, timeout)
			if d.Open {
				open = append(open, p)
			} else {
				closed++
			}
		}
		result.Success = true
		result.Details = portScanDetail{OpenPorts: open, ClosedPorts: closed, Total: len(ports)}

	case "resolve":
		ips, err := net.LookupHost(in.Host)
		if err != nil {
			result.Error = err.Error()
		} else {
			result.Success = true
			result.Details = ips
		}

	default:
		return nil, fmt.Errorf("unknown action %q, use: ping, port_check, port_scan, resolve", in.Action)
	}

	return result, nil
}

func tcpPing(host string, timeoutSec int) pingDetail {
	// Try common ports for TCP ping
	for _, port := range []int{80, 443, 22} {
		addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
		start := time.Now()
		conn, err := net.DialTimeout("tcp", addr, time.Duration(timeoutSec)*time.Second)
		if err == nil {
			conn.Close()
			return pingDetail{Reachable: true, Latency: time.Since(start).String()}
		}
	}
	return pingDetail{Reachable: false, Error: "no common ports responded"}
}

func checkPort(host string, port, timeoutSec int) portCheckDetail {
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	conn, err := net.DialTimeout("tcp", addr, time.Duration(timeoutSec)*time.Second)
	if err != nil {
		return portCheckDetail{Port: port, Open: false}
	}
	defer conn.Close()

	// Try to grab a banner
	banner := ""
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 512)
	n, err := conn.Read(buf)
	if err == nil && n > 0 {
		banner = strings.TrimSpace(string(buf[:n]))
	}

	return portCheckDetail{Port: port, Open: true, Banner: banner}
}

func commonPorts() []int {
	return []int{
		21, 22, 23, 25, 53, 80, 110, 143, 443, 465,
		587, 993, 995, 3306, 3389, 5432, 5900, 6379,
		8080, 8443, 9090, 9200, 27017,
	}
}

func parsePorts(spec string) []int {
	if spec == "" {
		return nil
	}
	switch strings.ToLower(spec) {
	case "common":
		return commonPorts()
	case "web":
		return []int{80, 443, 8080, 8443, 3000, 4000, 5000, 8000, 9090}
	case "db":
		return []int{3306, 5432, 6379, 27017, 9200, 5984, 1433, 1521, 7474}
	}

	var ports []int
	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		var p int
		if _, err := fmt.Sscanf(part, "%d", &p); err == nil && p > 0 && p <= 65535 {
			ports = append(ports, p)
		}
	}
	return ports
}
