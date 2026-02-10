package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

type logViewerArgs struct {
	Action  string `json:"action"`               // tail, head, grep, follow
	File    string `json:"file,omitempty"`       // log file path
	Lines   int    `json:"lines,omitempty"`      // number of lines
	Pattern string `json:"pattern,omitempty"`    // grep pattern
	Service string `json:"service,omitempty"`    // journalctl service name
	Since   string `json:"since,omitempty"`      // journalctl --since
	Ignore  bool   `json:"ignoreCase,omitempty"` // case-insensitive grep
}

type logViewerResult struct {
	Action  string   `json:"action"`
	File    string   `json:"file,omitempty"`
	Lines   []string `json:"lines"`
	Count   int      `json:"count"`
	Matches int      `json:"matches,omitempty"`
	Error   string   `json:"error,omitempty"`
}

func NewLogViewer() Tool {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"tail", "head", "grep", "journalctl"},
				"description": "Action: tail (last N lines), head (first N lines), grep (search), journalctl (systemd logs).",
			},
			"file": map[string]any{
				"type":        "string",
				"description": "Path to the log file (for tail, head, grep).",
			},
			"lines": map[string]any{
				"type":        "integer",
				"description": "Number of lines to return. Defaults to 50.",
				"minimum":     1,
				"maximum":     1000,
			},
			"pattern": map[string]any{
				"type":        "string",
				"description": "Search pattern for grep action.",
			},
			"service": map[string]any{
				"type":        "string",
				"description": "Systemd service name for journalctl action (e.g. 'nginx', 'docker').",
			},
			"since": map[string]any{
				"type":        "string",
				"description": "Time filter for journalctl (e.g. '1 hour ago', '2024-01-01', 'today').",
			},
			"ignoreCase": map[string]any{
				"type":        "boolean",
				"description": "Case-insensitive pattern matching for grep.",
			},
		},
		"required": []string{"action"},
	}

	return NewFuncTool(
		"log_viewer",
		"View and search log files: tail, head, grep patterns, journalctl for systemd services.",
		schema,
		func(ctx context.Context, args json.RawMessage) (any, error) {
			var in logViewerArgs
			if err := json.Unmarshal(args, &in); err != nil {
				return nil, fmt.Errorf("invalid log_viewer args: %w", err)
			}
			return executeLogViewer(ctx, in)
		},
	)
}

func executeLogViewer(ctx context.Context, in logViewerArgs) (*logViewerResult, error) {
	if runtime.GOOS == "windows" {
		return &logViewerResult{Error: "log_viewer is not supported on Windows"}, nil
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	lines := in.Lines
	if lines <= 0 {
		lines = 50
	}

	switch in.Action {
	case "tail":
		if in.File == "" {
			return nil, fmt.Errorf("file is required for tail")
		}
		return runLogCommand(ctx, "tail", in.File, exec.CommandContext(ctx, "tail", "-n", fmt.Sprintf("%d", lines), in.File))

	case "head":
		if in.File == "" {
			return nil, fmt.Errorf("file is required for head")
		}
		return runLogCommand(ctx, "head", in.File, exec.CommandContext(ctx, "head", "-n", fmt.Sprintf("%d", lines), in.File))

	case "grep":
		if in.Pattern == "" {
			return nil, fmt.Errorf("pattern is required for grep")
		}
		if in.File == "" {
			return nil, fmt.Errorf("file is required for grep")
		}

		// Validate pattern is not a shell injection
		if strings.ContainsAny(in.Pattern, ";|&`$(){}") {
			return nil, fmt.Errorf("invalid characters in pattern")
		}

		args := []string{"-n"}
		if in.Ignore {
			args = append(args, "-i")
		}
		args = append(args, in.Pattern, in.File)

		result, err := runLogCommand(ctx, "grep", in.File, exec.CommandContext(ctx, "grep", args...))
		if result != nil {
			result.Matches = result.Count
			if result.Count > lines {
				result.Lines = result.Lines[:lines]
				result.Count = lines
			}
		}
		return result, err

	case "journalctl":
		args := []string{"--no-pager", "-n", fmt.Sprintf("%d", lines)}
		if in.Service != "" {
			args = append(args, "-u", in.Service)
		}
		if in.Since != "" {
			args = append(args, "--since", in.Since)
		}
		return runLogCommand(ctx, "journalctl", in.Service, exec.CommandContext(ctx, "journalctl", args...))

	default:
		return nil, fmt.Errorf("unknown action %q, use: tail, head, grep, journalctl", in.Action)
	}
}

func runLogCommand(ctx context.Context, action, file string, cmd *exec.Cmd) (*logViewerResult, error) {
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	output := stdout.String()

	// grep returns exit code 1 for no matches — that's not an error
	if err != nil && action != "grep" {
		errStr := stderr.String()
		if errStr == "" {
			errStr = err.Error()
		}
		return &logViewerResult{Action: action, File: file, Error: errStr}, nil
	}

	var lines []string
	if output != "" {
		lines = strings.Split(strings.TrimRight(output, "\n"), "\n")
	}

	return &logViewerResult{
		Action: action,
		File:   file,
		Lines:  lines,
		Count:  len(lines),
	}, nil
}
