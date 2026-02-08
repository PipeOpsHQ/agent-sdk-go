package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

type kubectlArgs struct {
	Operation  string `json:"operation"`
	Resource   string `json:"resource,omitempty"`
	Name       string `json:"name,omitempty"`
	Namespace  string `json:"namespace,omitempty"`
	Context    string `json:"context,omitempty"`
	Kubeconfig string `json:"kubeconfig,omitempty"`
	Output     string `json:"output,omitempty"`
	Manifest   string `json:"manifest,omitempty"`
	FilePath   string `json:"filePath,omitempty"`
	Container  string `json:"container,omitempty"`
	Command    []string `json:"command,omitempty"`
	LocalPort  int    `json:"localPort,omitempty"`
	RemotePort int    `json:"remotePort,omitempty"`
	Replicas   int    `json:"replicas,omitempty"`
	Subcommand string `json:"subcommand,omitempty"`
	Selector   string `json:"selector,omitempty"`
	Tail       int    `json:"tail,omitempty"`
	Timeout    int    `json:"timeout,omitempty"`
}

// KubectlResult contains the result of a kubectl operation.
type KubectlResult struct {
	Success  bool   `json:"success"`
	Output   string `json:"output,omitempty"`
	Error    string `json:"error,omitempty"`
	Duration string `json:"duration,omitempty"`
}

// blockedNamespaces are protected from destructive operations.
var blockedNamespaces = map[string]bool{
	"kube-system": true,
	"kube-public": true,
	"kube-node-lease": true,
}

func NewKubectl() Tool {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"operation": map[string]any{
				"type":        "string",
				"enum":        []string{"get", "describe", "logs", "apply", "delete", "exec", "port_forward", "rollout", "scale"},
				"description": "Operation: get, describe, logs, apply, delete, exec, port_forward, rollout, scale.",
			},
			"resource": map[string]any{
				"type":        "string",
				"description": "Kubernetes resource type (e.g. pods, services, deployments, configmaps).",
			},
			"name": map[string]any{
				"type":        "string",
				"description": "Resource name.",
			},
			"namespace": map[string]any{
				"type":        "string",
				"description": "Kubernetes namespace. Defaults to current context namespace.",
			},
			"context": map[string]any{
				"type":        "string",
				"description": "Kubernetes context to use.",
			},
			"kubeconfig": map[string]any{
				"type":        "string",
				"description": "Path to kubeconfig file.",
			},
			"output": map[string]any{
				"type":        "string",
				"enum":        []string{"wide", "json", "yaml", "name"},
				"description": "Output format for get operation.",
			},
			"manifest": map[string]any{
				"type":        "string",
				"description": "YAML/JSON manifest content (for apply operation).",
			},
			"filePath": map[string]any{
				"type":        "string",
				"description": "Path to manifest file (for apply operation).",
			},
			"container": map[string]any{
				"type":        "string",
				"description": "Container name (for logs, exec operations in multi-container pods).",
			},
			"command": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Command to execute (for exec operation).",
			},
			"localPort": map[string]any{
				"type":        "integer",
				"description": "Local port (for port_forward operation).",
			},
			"remotePort": map[string]any{
				"type":        "integer",
				"description": "Remote port (for port_forward operation).",
			},
			"replicas": map[string]any{
				"type":        "integer",
				"description": "Number of replicas (for scale operation).",
			},
			"subcommand": map[string]any{
				"type":        "string",
				"enum":        []string{"status", "restart", "undo"},
				"description": "Rollout subcommand (for rollout operation).",
			},
			"selector": map[string]any{
				"type":        "string",
				"description": "Label selector for filtering resources (e.g. app=myapp).",
			},
			"tail": map[string]any{
				"type":        "integer",
				"description": "Number of log lines to show. Default: 100.",
			},
			"timeout": map[string]any{
				"type":        "integer",
				"description": "Timeout in seconds. Default: 60. Maximum: 300.",
			},
		},
		"required": []string{"operation"},
	}

	return NewFuncTool(
		"kubectl",
		"Interact with Kubernetes clusters. Get, describe, and manage resources; view logs; apply manifests; scale deployments.",
		schema,
		func(ctx context.Context, args json.RawMessage) (any, error) {
			var in kubectlArgs
			if err := json.Unmarshal(args, &in); err != nil {
				return nil, fmt.Errorf("invalid kubectl args: %w", err)
			}

			timeout := in.Timeout
			if timeout <= 0 {
				timeout = 60
			}
			if timeout > 300 {
				timeout = 300
			}

			switch in.Operation {
			case "get":
				return kubectlGet(ctx, timeout, in)
			case "describe":
				return kubectlDescribe(ctx, timeout, in)
			case "logs":
				return kubectlLogs(ctx, timeout, in)
			case "apply":
				return kubectlApply(ctx, timeout, in)
			case "delete":
				return kubectlDelete(ctx, timeout, in)
			case "exec":
				return kubectlExecCmd(ctx, timeout, in)
			case "port_forward":
				return kubectlPortForward(ctx, timeout, in)
			case "rollout":
				return kubectlRollout(ctx, timeout, in)
			case "scale":
				return kubectlScale(ctx, timeout, in)
			default:
				return nil, fmt.Errorf("unsupported operation %q", in.Operation)
			}
		},
	)
}

func buildKubectlBase(in kubectlArgs) []string {
	var args []string

	if in.Kubeconfig != "" {
		args = append(args, "--kubeconfig", in.Kubeconfig)
	}
	if in.Context != "" {
		args = append(args, "--context", in.Context)
	}
	if in.Namespace != "" {
		args = append(args, "-n", in.Namespace)
	}

	return args
}

func runKubectl(ctx context.Context, timeout int, cmdArgs []string) (*KubectlResult, error) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "kubectl", cmdArgs...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	result := &KubectlResult{
		Duration: time.Since(start).String(),
	}

	if err != nil {
		result.Success = false
		result.Error = fmt.Sprintf("%v: %s", err, stderr.String())
		result.Output = limitOutput(stdout.String(), 100*1024)
	} else {
		result.Success = true
		result.Output = limitOutput(stdout.String(), 100*1024)
	}

	return result, nil
}

func kubectlGet(ctx context.Context, timeout int, in kubectlArgs) (*KubectlResult, error) {
	if in.Resource == "" {
		return &KubectlResult{Success: false, Error: "resource is required for get"}, nil
	}

	args := buildKubectlBase(in)
	args = append(args, "get", in.Resource)

	if in.Name != "" {
		args = append(args, in.Name)
	}
	if in.Output != "" {
		args = append(args, "-o", in.Output)
	}
	if in.Selector != "" {
		args = append(args, "-l", in.Selector)
	}

	return runKubectl(ctx, timeout, args)
}

func kubectlDescribe(ctx context.Context, timeout int, in kubectlArgs) (*KubectlResult, error) {
	if in.Resource == "" {
		return &KubectlResult{Success: false, Error: "resource is required for describe"}, nil
	}

	args := buildKubectlBase(in)
	args = append(args, "describe", in.Resource)

	if in.Name != "" {
		args = append(args, in.Name)
	}
	if in.Selector != "" {
		args = append(args, "-l", in.Selector)
	}

	return runKubectl(ctx, timeout, args)
}

func kubectlLogs(ctx context.Context, timeout int, in kubectlArgs) (*KubectlResult, error) {
	if in.Name == "" {
		return &KubectlResult{Success: false, Error: "name (pod name) is required for logs"}, nil
	}

	args := buildKubectlBase(in)
	args = append(args, "logs", in.Name)

	if in.Container != "" {
		args = append(args, "-c", in.Container)
	}

	tail := in.Tail
	if tail <= 0 {
		tail = 100
	}
	args = append(args, "--tail", fmt.Sprintf("%d", tail))

	return runKubectl(ctx, timeout, args)
}

func kubectlApply(ctx context.Context, timeout int, in kubectlArgs) (*KubectlResult, error) {
	args := buildKubectlBase(in)
	args = append(args, "apply")

	if in.FilePath != "" {
		args = append(args, "-f", in.FilePath)
	} else if in.Manifest != "" {
		// Apply from stdin using manifest content
		start := time.Now()
		ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
		defer cancel()

		fullArgs := append([]string{"apply", "-f", "-"}, buildKubectlBase(in)...)
		cmd := exec.CommandContext(ctx, "kubectl", fullArgs...)
		cmd.Stdin = strings.NewReader(in.Manifest)

		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		err := cmd.Run()
		result := &KubectlResult{Duration: time.Since(start).String()}

		if err != nil {
			result.Success = false
			result.Error = fmt.Sprintf("%v: %s", err, stderr.String())
			result.Output = limitOutput(stdout.String(), 100*1024)
		} else {
			result.Success = true
			result.Output = limitOutput(stdout.String(), 100*1024)
		}

		return result, nil
	} else {
		return &KubectlResult{Success: false, Error: "filePath or manifest is required for apply"}, nil
	}

	return runKubectl(ctx, timeout, args)
}

func kubectlDelete(ctx context.Context, timeout int, in kubectlArgs) (*KubectlResult, error) {
	if in.Resource == "" {
		return &KubectlResult{Success: false, Error: "resource is required for delete"}, nil
	}

	// Safety: block destructive operations on protected namespaces
	if blockedNamespaces[in.Namespace] {
		return &KubectlResult{
			Success: false,
			Error:   fmt.Sprintf("delete operations on namespace %q are blocked for safety", in.Namespace),
		}, nil
	}

	args := buildKubectlBase(in)
	args = append(args, "delete", in.Resource)

	if in.Name != "" {
		args = append(args, in.Name)
	}
	if in.Selector != "" {
		args = append(args, "-l", in.Selector)
	}

	return runKubectl(ctx, timeout, args)
}

func kubectlExecCmd(ctx context.Context, timeout int, in kubectlArgs) (*KubectlResult, error) {
	if in.Name == "" {
		return &KubectlResult{Success: false, Error: "name (pod name) is required for exec"}, nil
	}
	if len(in.Command) == 0 {
		return &KubectlResult{Success: false, Error: "command is required for exec"}, nil
	}

	args := buildKubectlBase(in)
	args = append(args, "exec", in.Name)

	if in.Container != "" {
		args = append(args, "-c", in.Container)
	}

	args = append(args, "--")
	args = append(args, in.Command...)

	return runKubectl(ctx, timeout, args)
}

func kubectlPortForward(ctx context.Context, timeout int, in kubectlArgs) (*KubectlResult, error) {
	if in.Name == "" {
		return &KubectlResult{Success: false, Error: "name (pod name) is required for port_forward"}, nil
	}
	if in.LocalPort <= 0 || in.RemotePort <= 0 {
		return &KubectlResult{Success: false, Error: "localPort and remotePort are required for port_forward"}, nil
	}

	args := buildKubectlBase(in)
	args = append(args, "port-forward", in.Name, fmt.Sprintf("%d:%d", in.LocalPort, in.RemotePort))

	// Port forward runs briefly to verify it can start, then reports success
	// In practice an agent would use shell_command for long-running port-forward
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "kubectl", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Start the command but don't wait for it to complete (it's long-running)
	if err := cmd.Start(); err != nil {
		return &KubectlResult{
			Success:  false,
			Error:    fmt.Sprintf("failed to start port-forward: %v", err),
			Duration: time.Since(start).String(),
		}, nil
	}

	// Wait briefly for errors
	time.Sleep(1 * time.Second)

	// Check if process is still running (good = port forward is active)
	if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
		return &KubectlResult{
			Success:  false,
			Error:    fmt.Sprintf("port-forward exited: %s", stderr.String()),
			Duration: time.Since(start).String(),
		}, nil
	}

	// Kill the process since we just verified it works
	if cmd.Process != nil {
		cmd.Process.Kill()
	}

	return &KubectlResult{
		Success:  true,
		Output:   fmt.Sprintf("Port forward verified: localhost:%d -> %s:%d", in.LocalPort, in.Name, in.RemotePort),
		Duration: time.Since(start).String(),
	}, nil
}

func kubectlRollout(ctx context.Context, timeout int, in kubectlArgs) (*KubectlResult, error) {
	if in.Subcommand == "" {
		return &KubectlResult{Success: false, Error: "subcommand is required for rollout (status, restart, undo)"}, nil
	}
	if in.Resource == "" {
		return &KubectlResult{Success: false, Error: "resource is required for rollout"}, nil
	}

	args := buildKubectlBase(in)
	args = append(args, "rollout", in.Subcommand, in.Resource)

	if in.Name != "" {
		args = append(args, in.Name)
	}

	return runKubectl(ctx, timeout, args)
}

func kubectlScale(ctx context.Context, timeout int, in kubectlArgs) (*KubectlResult, error) {
	if in.Resource == "" {
		return &KubectlResult{Success: false, Error: "resource is required for scale"}, nil
	}
	if in.Name == "" {
		return &KubectlResult{Success: false, Error: "name is required for scale"}, nil
	}
	if in.Replicas < 0 {
		return &KubectlResult{Success: false, Error: "replicas must be >= 0"}, nil
	}

	args := buildKubectlBase(in)
	args = append(args, "scale", fmt.Sprintf("%s/%s", in.Resource, in.Name), fmt.Sprintf("--replicas=%d", in.Replicas))

	return runKubectl(ctx, timeout, args)
}
