package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type shellCommandArgs struct {
	Command    string            `json:"command"`
	Args       []string          `json:"args,omitempty"`
	WorkingDir string            `json:"workingDir,omitempty"`
	Env        map[string]string `json:"env,omitempty"`
	Timeout    int               `json:"timeout,omitempty"`
	Shell      bool              `json:"shell,omitempty"`
}

// ShellResult contains the result of a shell command execution.
type ShellResult struct {
	Success  bool   `json:"success"`
	ExitCode int    `json:"exitCode"`
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	Duration string `json:"duration"`
	Command  string `json:"command"`
	Error    string `json:"error,omitempty"`
}

// Blocklist of dangerous commands
var blockedCommands = map[string]bool{
	"rm": true, "rmdir": true, "del": true, "format": true, "mkfs": true,
	"dd": true, "shutdown": true, "reboot": true, "halt": true, "poweroff": true,
	"init": true, "kill": true, "killall": true, "pkill": true,
}

// Patterns that indicate dangerous operations
var dangerousPatterns = []string{
	"rm -rf", "rm -fr", "> /dev/", "| rm", "; rm", "&& rm",
	"mkfs", ":(){", "chmod 777", "sudo", "su -", "passwd",
}

func NewShellCommand() Tool {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{
				"type":        "string",
				"description": "The command to execute.",
			},
			"args": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Arguments to pass to the command.",
			},
			"workingDir": map[string]any{
				"type":        "string",
				"description": "Working directory for command execution.",
			},
			"env": map[string]any{
				"type":        "object",
				"description": "Additional environment variables.",
			},
			"timeout": map[string]any{
				"type":        "integer",
				"description": "Timeout in seconds. Defaults to 60. Maximum 300.",
				"minimum":     1,
				"maximum":     300,
			},
			"shell": map[string]any{
				"type":        "boolean",
				"description": "Execute through shell (allows pipes, redirects). Defaults to false.",
			},
		},
		"required": []string{"command"},
	}

	return NewFuncTool(
		"shell_command",
		"Execute shell commands safely with timeout and working directory support. Some dangerous commands are blocked.",
		schema,
		func(ctx context.Context, args json.RawMessage) (any, error) {
			var in shellCommandArgs
			if err := json.Unmarshal(args, &in); err != nil {
				return nil, fmt.Errorf("invalid shell_command args: %w", err)
			}
			if in.Command == "" {
				return nil, fmt.Errorf("command is required")
			}

			if err := validateCommand(in.Command, in.Args, in.Shell); err != nil {
				return &ShellResult{Success: false, Error: err.Error(), Command: in.Command}, nil
			}

			timeout := in.Timeout
			if timeout <= 0 {
				timeout = 60
			}
			if timeout > 300 {
				timeout = 300
			}

			return executeCommand(ctx, in, timeout)
		},
	)
}

func validateCommand(command string, args []string, useShell bool) error {
	cmdLower := strings.ToLower(command)
	baseName := filepath.Base(cmdLower)

	if blockedCommands[baseName] {
		return fmt.Errorf("command %q is blocked for safety", command)
	}

	if useShell {
		fullCmd := command + " " + strings.Join(args, " ")
		for _, pattern := range dangerousPatterns {
			if strings.Contains(strings.ToLower(fullCmd), pattern) {
				return fmt.Errorf("command contains blocked pattern %q", pattern)
			}
		}
	}

	return nil
}

func executeCommand(ctx context.Context, args shellCommandArgs, timeout int) (*ShellResult, error) {
	start := time.Now()

	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	var cmd *exec.Cmd

	if args.Shell {
		var shellCmd string
		var shellArgs []string

		if runtime.GOOS == "windows" {
			shellCmd = "cmd"
			fullCommand := args.Command
			if len(args.Args) > 0 {
				fullCommand += " " + strings.Join(args.Args, " ")
			}
			shellArgs = []string{"/C", fullCommand}
		} else {
			shellCmd = "/bin/sh"
			fullCommand := args.Command
			if len(args.Args) > 0 {
				fullCommand += " " + strings.Join(args.Args, " ")
			}
			shellArgs = []string{"-c", fullCommand}
		}

		cmd = exec.CommandContext(ctx, shellCmd, shellArgs...)
	} else {
		cmd = exec.CommandContext(ctx, args.Command, args.Args...)
	}

	if args.WorkingDir != "" {
		cmd.Dir = args.WorkingDir
	}

	if len(args.Env) > 0 {
		cmd.Env = os.Environ()
		for k, v := range args.Env {
			cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
		}
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	result := &ShellResult{
		Stdout:   limitOutput(stdout.String(), 100*1024),
		Stderr:   limitOutput(stderr.String(), 100*1024),
		Duration: time.Since(start).String(),
		Command:  args.Command,
	}

	if err != nil {
		result.Success = false
		if exitError, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitError.ExitCode()
		} else {
			result.Error = err.Error()
			result.ExitCode = -1
		}
	} else {
		result.Success = true
		result.ExitCode = 0
	}

	return result, nil
}

func limitOutput(s string, maxLen int) string {
	if len(s) > maxLen {
		return s[:maxLen] + "\n... (output truncated)"
	}
	return s
}
