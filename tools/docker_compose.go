package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"
)

type dockerComposeArgs struct {
	Operation   string   `json:"operation"`
	ProjectDir  string   `json:"projectDir,omitempty"`
	ComposeFile string   `json:"composeFile,omitempty"`
	Services    []string `json:"services,omitempty"`
	Command     []string `json:"command,omitempty"`
	Service     string   `json:"service,omitempty"`
	Detach      bool     `json:"detach,omitempty"`
	Build       bool     `json:"build,omitempty"`
	Tail        string   `json:"tail,omitempty"`
	Timeout     int      `json:"timeout,omitempty"`
}

// DockerComposeResult contains the result of a docker compose operation.
type DockerComposeResult struct {
	Success  bool   `json:"success"`
	Output   string `json:"output,omitempty"`
	Error    string `json:"error,omitempty"`
	Duration string `json:"duration,omitempty"`
}

func NewDockerCompose() Tool {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"operation": map[string]any{
				"type":        "string",
				"enum":        []string{"up", "down", "ps", "logs", "build", "restart", "exec"},
				"description": "Operation: up, down, ps, logs, build, restart, exec.",
			},
			"projectDir": map[string]any{
				"type":        "string",
				"description": "Project directory containing docker-compose.yml.",
			},
			"composeFile": map[string]any{
				"type":        "string",
				"description": "Path to compose file. Defaults to docker-compose.yml in projectDir.",
			},
			"services": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Specific services to target (for up, build, restart, logs operations).",
			},
			"service": map[string]any{
				"type":        "string",
				"description": "Service name (for exec operation).",
			},
			"command": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Command to execute (for exec operation).",
			},
			"detach": map[string]any{
				"type":        "boolean",
				"description": "Run in detached mode (for up operation). Defaults to true.",
			},
			"build": map[string]any{
				"type":        "boolean",
				"description": "Build images before starting (for up operation).",
			},
			"tail": map[string]any{
				"type":        "string",
				"description": "Number of lines to show from end of logs. Default: 100.",
			},
			"timeout": map[string]any{
				"type":        "integer",
				"description": "Timeout in seconds. Default: 120. Maximum: 600.",
			},
		},
		"required": []string{"operation"},
	}

	return NewFuncTool(
		"docker_compose",
		"Manage Docker Compose services. Start, stop, build, restart services; view logs; execute commands in services.",
		schema,
		func(ctx context.Context, args json.RawMessage) (any, error) {
			var in dockerComposeArgs
			if err := json.Unmarshal(args, &in); err != nil {
				return nil, fmt.Errorf("invalid docker_compose args: %w", err)
			}

			timeout := in.Timeout
			if timeout <= 0 {
				timeout = 120
			}
			if timeout > 600 {
				timeout = 600
			}

			switch in.Operation {
			case "up":
				return composeUp(ctx, timeout, in)
			case "down":
				return composeExec(ctx, timeout, in, "down")
			case "ps":
				return composeExec(ctx, timeout, in, "ps")
			case "logs":
				return composeLogs(ctx, timeout, in)
			case "build":
				return composeBuild(ctx, timeout, in)
			case "restart":
				return composeRestart(ctx, timeout, in)
			case "exec":
				return composeExecCmd(ctx, timeout, in)
			default:
				return nil, fmt.Errorf("unsupported operation %q", in.Operation)
			}
		},
	)
}

func buildComposeBase(in dockerComposeArgs) []string {
	args := []string{"compose"}

	if in.ComposeFile != "" {
		args = append(args, "-f", in.ComposeFile)
	}

	return args
}

func runComposeCmd(ctx context.Context, timeout int, in dockerComposeArgs, cmdArgs []string) (*DockerComposeResult, error) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	base := buildComposeBase(in)
	fullArgs := append(base, cmdArgs...)

	cmd := exec.CommandContext(ctx, "docker", fullArgs...)
	if in.ProjectDir != "" {
		cmd.Dir = in.ProjectDir
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	result := &DockerComposeResult{
		Duration: time.Since(start).String(),
	}

	if err != nil {
		result.Success = false
		result.Error = fmt.Sprintf("%v: %s", err, stderr.String())
		result.Output = limitOutput(stdout.String(), 100*1024)
	} else {
		result.Success = true
		result.Output = limitOutput(stdout.String()+stderr.String(), 100*1024)
	}

	return result, nil
}

func composeExec(ctx context.Context, timeout int, in dockerComposeArgs, subcmd string) (*DockerComposeResult, error) {
	return runComposeCmd(ctx, timeout, in, []string{subcmd})
}

func composeUp(ctx context.Context, timeout int, in dockerComposeArgs) (*DockerComposeResult, error) {
	args := []string{"up"}

	// Default to detached mode
	if !in.Detach {
		// Only run attached if explicitly set to false via JSON (detach defaults to true for up)
		args = append(args, "-d")
	} else {
		args = append(args, "-d")
	}

	if in.Build {
		args = append(args, "--build")
	}

	args = append(args, in.Services...)

	return runComposeCmd(ctx, timeout, in, args)
}

func composeLogs(ctx context.Context, timeout int, in dockerComposeArgs) (*DockerComposeResult, error) {
	args := []string{"logs"}

	tail := in.Tail
	if tail == "" {
		tail = "100"
	}
	args = append(args, "--tail", tail)
	args = append(args, in.Services...)

	return runComposeCmd(ctx, timeout, in, args)
}

func composeBuild(ctx context.Context, timeout int, in dockerComposeArgs) (*DockerComposeResult, error) {
	args := []string{"build"}
	args = append(args, in.Services...)
	return runComposeCmd(ctx, timeout, in, args)
}

func composeRestart(ctx context.Context, timeout int, in dockerComposeArgs) (*DockerComposeResult, error) {
	args := []string{"restart"}
	args = append(args, in.Services...)
	return runComposeCmd(ctx, timeout, in, args)
}

func composeExecCmd(ctx context.Context, timeout int, in dockerComposeArgs) (*DockerComposeResult, error) {
	if in.Service == "" {
		return &DockerComposeResult{Success: false, Error: "service is required for exec"}, nil
	}
	if len(in.Command) == 0 {
		return &DockerComposeResult{Success: false, Error: "command is required for exec"}, nil
	}

	args := []string{"exec", in.Service}
	args = append(args, in.Command...)

	return runComposeCmd(ctx, timeout, in, args)
}
