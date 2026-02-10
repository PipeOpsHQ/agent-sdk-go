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

type archiveArgs struct {
	Action      string   `json:"action"`                // create, extract, list
	Format      string   `json:"format,omitempty"`      // tar, tar.gz, zip
	Source      string   `json:"source,omitempty"`      // file/directory to archive (for create)
	Files       []string `json:"files,omitempty"`       // specific files to include
	Archive     string   `json:"archive,omitempty"`     // archive file path
	Destination string   `json:"destination,omitempty"` // extract destination
	Compression string   `json:"compression,omitempty"` // gzip, bzip2, xz (for tar)
}

type archiveResult struct {
	Action  string   `json:"action"`
	Archive string   `json:"archive,omitempty"`
	Files   []string `json:"files,omitempty"`
	Count   int      `json:"count"`
	Output  string   `json:"output,omitempty"`
	Error   string   `json:"error,omitempty"`
}

func NewArchive() Tool {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"create", "extract", "list"},
				"description": "Action: create an archive, extract an archive, or list archive contents.",
			},
			"format": map[string]any{
				"type":        "string",
				"enum":        []string{"tar", "tar.gz", "tar.bz2", "tar.xz", "zip"},
				"description": "Archive format. Auto-detected from filename if omitted.",
			},
			"source": map[string]any{
				"type":        "string",
				"description": "Source file or directory to archive (for create action).",
			},
			"files": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Specific files to include in the archive.",
			},
			"archive": map[string]any{
				"type":        "string",
				"description": "Path to the archive file.",
			},
			"destination": map[string]any{
				"type":        "string",
				"description": "Destination directory for extraction. Defaults to current directory.",
			},
			"compression": map[string]any{
				"type":        "string",
				"enum":        []string{"gzip", "bzip2", "xz", "none"},
				"description": "Compression type for tar archives. Auto-detected from extension.",
			},
		},
		"required": []string{"action"},
	}

	return NewFuncTool(
		"archive",
		"Create, extract, and list archives: tar, tar.gz, tar.bz2, tar.xz, zip.",
		schema,
		func(ctx context.Context, args json.RawMessage) (any, error) {
			var in archiveArgs
			if err := json.Unmarshal(args, &in); err != nil {
				return nil, fmt.Errorf("invalid archive args: %w", err)
			}
			return executeArchive(ctx, in)
		},
	)
}

func executeArchive(ctx context.Context, in archiveArgs) (*archiveResult, error) {
	if runtime.GOOS == "windows" {
		return &archiveResult{Error: "archive tool is not supported on Windows"}, nil
	}

	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()

	switch in.Action {
	case "create":
		return archiveCreate(ctx, in)
	case "extract":
		return archiveExtract(ctx, in)
	case "list":
		return archiveList(ctx, in)
	default:
		return nil, fmt.Errorf("unknown action %q, use: create, extract, list", in.Action)
	}
}

func detectFormat(filename string) string {
	lower := strings.ToLower(filename)
	switch {
	case strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz"):
		return "tar.gz"
	case strings.HasSuffix(lower, ".tar.bz2") || strings.HasSuffix(lower, ".tbz2"):
		return "tar.bz2"
	case strings.HasSuffix(lower, ".tar.xz") || strings.HasSuffix(lower, ".txz"):
		return "tar.xz"
	case strings.HasSuffix(lower, ".tar"):
		return "tar"
	case strings.HasSuffix(lower, ".zip"):
		return "zip"
	default:
		return "tar.gz"
	}
}

func tarFlag(format string) string {
	switch format {
	case "tar.gz":
		return "z"
	case "tar.bz2":
		return "j"
	case "tar.xz":
		return "J"
	default:
		return ""
	}
}

func archiveCreate(ctx context.Context, in archiveArgs) (*archiveResult, error) {
	if in.Archive == "" {
		return nil, fmt.Errorf("archive path is required for create")
	}

	format := in.Format
	if format == "" {
		format = detectFormat(in.Archive)
	}

	var cmd *exec.Cmd
	if format == "zip" {
		args := []string{"-r", in.Archive}
		if in.Source != "" {
			args = append(args, in.Source)
		}
		args = append(args, in.Files...)
		cmd = exec.CommandContext(ctx, "zip", args...)
	} else {
		flag := "cf" + tarFlag(format)
		args := []string{flag, in.Archive}
		if in.Source != "" {
			args = append(args, in.Source)
		}
		args = append(args, in.Files...)
		cmd = exec.CommandContext(ctx, "tar", args...)
	}

	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return &archiveResult{Error: fmt.Sprintf("%v: %s", err, stderr.String())}, nil
	}

	return &archiveResult{Action: "create", Archive: in.Archive, Output: "archive created successfully"}, nil
}

func archiveExtract(ctx context.Context, in archiveArgs) (*archiveResult, error) {
	if in.Archive == "" {
		return nil, fmt.Errorf("archive path is required for extract")
	}

	format := in.Format
	if format == "" {
		format = detectFormat(in.Archive)
	}

	dest := in.Destination
	if dest == "" {
		dest = "."
	}

	var cmd *exec.Cmd
	if format == "zip" {
		cmd = exec.CommandContext(ctx, "unzip", "-o", in.Archive, "-d", dest)
	} else {
		flag := "xf" + tarFlag(format)
		cmd = exec.CommandContext(ctx, "tar", flag, in.Archive, "-C", dest)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return &archiveResult{Error: fmt.Sprintf("%v: %s", err, stderr.String())}, nil
	}

	return &archiveResult{Action: "extract", Archive: in.Archive, Output: fmt.Sprintf("extracted to %s", dest)}, nil
}

func archiveList(ctx context.Context, in archiveArgs) (*archiveResult, error) {
	if in.Archive == "" {
		return nil, fmt.Errorf("archive path is required for list")
	}

	format := in.Format
	if format == "" {
		format = detectFormat(in.Archive)
	}

	var cmd *exec.Cmd
	if format == "zip" {
		cmd = exec.CommandContext(ctx, "unzip", "-l", in.Archive)
	} else {
		flag := "tf" + tarFlag(format)
		cmd = exec.CommandContext(ctx, "tar", flag, in.Archive)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return &archiveResult{Error: fmt.Sprintf("%v: %s", err, stderr.String())}, nil
	}

	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	files := make([]string, 0, len(lines))
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l != "" {
			files = append(files, l)
		}
	}

	return &archiveResult{Action: "list", Archive: in.Archive, Files: files, Count: len(files)}, nil
}
