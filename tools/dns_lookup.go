package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"time"
)

type dnsLookupArgs struct {
	Hostname   string `json:"hostname"`
	RecordType string `json:"recordType,omitempty"` // A, AAAA, MX, NS, TXT, CNAME, SRV, PTR
	Server     string `json:"server,omitempty"`     // custom DNS server
}

type dnsResult struct {
	Hostname   string   `json:"hostname"`
	RecordType string   `json:"recordType"`
	Records    []string `json:"records"`
	Duration   string   `json:"duration"`
	Error      string   `json:"error,omitempty"`
}

func NewDNSLookup() Tool {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"hostname": map[string]any{
				"type":        "string",
				"description": "The hostname to look up (e.g. 'example.com').",
			},
			"recordType": map[string]any{
				"type":        "string",
				"enum":        []string{"A", "AAAA", "MX", "NS", "TXT", "CNAME", "SRV", "PTR"},
				"description": "DNS record type. Defaults to A.",
			},
			"server": map[string]any{
				"type":        "string",
				"description": "Custom DNS server (e.g. '8.8.8.8' or '1.1.1.1'). Uses system default if omitted.",
			},
		},
		"required": []string{"hostname"},
	}

	return NewFuncTool(
		"dns_lookup",
		"Resolve DNS records: A, AAAA, MX, NS, TXT, CNAME, SRV, PTR. Supports custom DNS servers.",
		schema,
		func(ctx context.Context, args json.RawMessage) (any, error) {
			var in dnsLookupArgs
			if err := json.Unmarshal(args, &in); err != nil {
				return nil, fmt.Errorf("invalid dns_lookup args: %w", err)
			}
			if in.Hostname == "" {
				return nil, fmt.Errorf("hostname is required")
			}
			return executeDNSLookup(ctx, in)
		},
	)
}

func executeDNSLookup(ctx context.Context, in dnsLookupArgs) (*dnsResult, error) {
	recType := strings.ToUpper(in.RecordType)
	if recType == "" {
		recType = "A"
	}

	resolver := net.DefaultResolver
	if in.Server != "" {
		server := in.Server
		if !strings.Contains(server, ":") {
			server += ":53"
		}
		resolver = &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
				d := net.Dialer{Timeout: 10 * time.Second}
				return d.DialContext(ctx, "udp", server)
			},
		}
	}

	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	start := time.Now()
	var records []string
	var lookupErr error

	switch recType {
	case "A", "AAAA":
		ips, err := resolver.LookupIPAddr(ctx, in.Hostname)
		lookupErr = err
		for _, ip := range ips {
			if recType == "A" && ip.IP.To4() != nil {
				records = append(records, ip.IP.String())
			} else if recType == "AAAA" && ip.IP.To4() == nil {
				records = append(records, ip.IP.String())
			}
		}
	case "MX":
		mxs, err := resolver.LookupMX(ctx, in.Hostname)
		lookupErr = err
		for _, mx := range mxs {
			records = append(records, fmt.Sprintf("%s (priority %d)", mx.Host, mx.Pref))
		}
	case "NS":
		nss, err := resolver.LookupNS(ctx, in.Hostname)
		lookupErr = err
		for _, ns := range nss {
			records = append(records, ns.Host)
		}
	case "TXT":
		txts, err := resolver.LookupTXT(ctx, in.Hostname)
		lookupErr = err
		records = txts
	case "CNAME":
		cname, err := resolver.LookupCNAME(ctx, in.Hostname)
		lookupErr = err
		if cname != "" {
			records = append(records, cname)
		}
	case "SRV":
		_, srvs, err := resolver.LookupSRV(ctx, "", "", in.Hostname)
		lookupErr = err
		for _, srv := range srvs {
			records = append(records, fmt.Sprintf("%s:%d (priority %d, weight %d)", srv.Target, srv.Port, srv.Priority, srv.Weight))
		}
	case "PTR":
		ptrs, err := resolver.LookupAddr(ctx, in.Hostname)
		lookupErr = err
		records = ptrs
	default:
		return &dnsResult{Hostname: in.Hostname, RecordType: recType, Error: "unsupported record type"}, nil
	}

	result := &dnsResult{
		Hostname:   in.Hostname,
		RecordType: recType,
		Records:    records,
		Duration:   time.Since(start).String(),
	}

	if lookupErr != nil {
		result.Error = lookupErr.Error()
	}

	return result, nil
}
