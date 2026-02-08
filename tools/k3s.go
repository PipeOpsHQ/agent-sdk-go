package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"
)

type k3sArgs struct {
	Operation  string   `json:"operation"`
	Command    []string `json:"command,omitempty"`
	InstallOps string   `json:"installOpts,omitempty"`
	Timeout    int      `json:"timeout,omitempty"`
}

// K3sResult contains the result of a k3s operation.
type K3sResult struct {
	Success   bool   `json:"success"`
	Output    string `json:"output,omitempty"`
	Error     string `json:"error,omitempty"`
	Duration  string `json:"duration,omitempty"`
	Installed bool   `json:"installed,omitempty"`
}

func NewK3s() Tool {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"operation": map[string]any{
				"type":        "string",
				"enum":        []string{"install", "uninstall", "status", "kubectl", "crictl"},
				"description": "Operation: install, uninstall, status, kubectl (run kubectl via k3s), crictl.",
			},
			"command": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Arguments to pass to kubectl or crictl (for kubectl, crictl operations).",
			},
			"installOpts": map[string]any{
				"type":        "string",
				"description": "Additional install options passed as INSTALL_K3S_EXEC (for install operation).",
			},
			"timeout": map[string]any{
				"type":        "integer",
				"description": "Timeout in seconds. Default: 120. Maximum: 600.",
			},
		},
		"required": []string{"operation"},
	}

	return NewFuncTool(
		"k3s",
		"Manage k3s lightweight Kubernetes. Install, uninstall, check status, run kubectl and crictl commands via k3s.",
		schema,
		func(ctx context.Context, args json.RawMessage) (any, error) {
			var in k3sArgs
			if err := json.Unmarshal(args, &in); err != nil {
				return nil, fmt.Errorf("invalid k3s args: %w", err)
			}

			timeout := in.Timeout
			if timeout <= 0 {
				timeout = 120
			}
			if timeout > 600 {
				timeout = 600
			}

			switch in.Operation {
			case "install":
				return k3sInstall(ctx, timeout, in)
			case "uninstall":
				return k3sUninstall(ctx, timeout)
			case "status":
				return k3sStatus(ctx, timeout)
			case "kubectl":
				return k3sKubectl(ctx, timeout, in)
			case "crictl":
				return k3sCrictl(ctx, timeout, in)
			default:
				return nil, fmt.Errorf("unsupported operation %q", in.Operation)
			}
		},
	)
}

func k3sExec(ctx context.Context, timeout int, name string, args ...string) (*K3sResult, error) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	result := &K3sResult{
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

func k3sIsInstalled() bool {
	cmd := exec.Command("k3s", "--version")
	return cmd.Run() == nil
}

func k3sInstall(ctx context.Context, timeout int, in k3sArgs) (*K3sResult, error) {
	if k3sIsInstalled() {
		// Already installed, return version info
		result, err := k3sExec(ctx, timeout, "k3s", "--version")
		if err != nil {
			return result, err
		}
		result.Installed = true
		result.Output = "k3s is already installed. " + result.Output
		return result, nil
	}

	// Run the k3s install script via curl | sh pattern
	args := []string{"-c", "curl -sfL https://get.k3s.io | sh -"}
	if in.InstallOps != "" {
		args = []string{"-c", fmt.Sprintf("curl -sfL https://get.k3s.io | INSTALL_K3S_EXEC='%s' sh -", in.InstallOps)}
	}

	result, err := k3sExec(ctx, timeout, "/bin/sh", args...)
	if err != nil {
		return result, err
	}

	result.Installed = result.Success
	return result, nil
}

func k3sUninstall(ctx context.Context, timeout int) (*K3sResult, error) {
	if !k3sIsInstalled() {
		return &K3sResult{
			Success:   true,
			Output:    "k3s is not installed",
			Installed: false,
		}, nil
	}

	return k3sExec(ctx, timeout, "/usr/local/bin/k3s-uninstall.sh")
}

func k3sStatus(ctx context.Context, timeout int) (*K3sResult, error) {
	installed := k3sIsInstalled()

	if !installed {
		return &K3sResult{
			Success:   true,
			Output:    "k3s is not installed",
			Installed: false,
		}, nil
	}

	// Check k3s service status
	result, err := k3sExec(ctx, timeout, "systemctl", "status", "k3s", "--no-pager")
	if err != nil {
		return result, err
	}

	result.Installed = true

	// Also get version
	versionCmd := exec.CommandContext(ctx, "k3s", "--version")
	if versionOut, vErr := versionCmd.Output(); vErr == nil {
		result.Output = string(versionOut) + "\n" + result.Output
	}

	return result, nil
}

func k3sKubectl(ctx context.Context, timeout int, in k3sArgs) (*K3sResult, error) {
	if !k3sIsInstalled() {
		return &K3sResult{
			Success:   false,
			Error:     "k3s is not installed",
			Installed: false,
		}, nil
	}

	args := append([]string{"kubectl"}, in.Command...)
	result, err := k3sExec(ctx, timeout, "k3s", args...)
	if err != nil {
		return result, err
	}
	result.Installed = true
	return result, nil
}

func k3sCrictl(ctx context.Context, timeout int, in k3sArgs) (*K3sResult, error) {
	if !k3sIsInstalled() {
		return &K3sResult{
			Success:   false,
			Error:     "k3s is not installed",
			Installed: false,
		}, nil
	}

	args := append([]string{"crictl"}, in.Command...)
	result, err := k3sExec(ctx, timeout, "k3s", args...)
	if err != nil {
		return result, err
	}
	result.Installed = true
	return result, nil
}
