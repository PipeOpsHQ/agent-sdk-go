package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type fileSystemArgs struct {
	Operation string `json:"operation"`
	Path      string `json:"path"`
	Content   string `json:"content,omitempty"`
	Target    string `json:"target,omitempty"`
	Recursive bool   `json:"recursive,omitempty"`
	Pattern   string `json:"pattern,omitempty"`
	Lines     int    `json:"lines,omitempty"`
	Mode      string `json:"mode,omitempty"`
}

// FileInfo contains information about a file.
type FileInfo struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	IsDir   bool   `json:"isDir"`
	Mode    string `json:"mode"`
	ModTime string `json:"modTime"`
}

// FileResult contains the result of a file operation.
type FileResult struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
	Data    any    `json:"data,omitempty"`
}

// Blocked paths for security
var blockedPaths = []string{
	"/etc/passwd", "/etc/shadow", "/etc/sudoers",
	"~/.ssh", "~/.gnupg", "~/.aws/credentials", "~/.config/gcloud",
}

func NewFileSystem() Tool {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"operation": map[string]any{
				"type":        "string",
				"enum":        []string{"read", "write", "append", "list", "info", "exists", "mkdir", "copy", "move", "search", "delete", "rename", "chmod", "head", "tail", "grep", "symlink"},
				"description": "File operation to perform.",
			},
			"path": map[string]any{
				"type":        "string",
				"description": "File or directory path.",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "Content for write/append operations.",
			},
			"target": map[string]any{
				"type":        "string",
				"description": "Target path for copy/move/rename/symlink operations.",
			},
			"recursive": map[string]any{
				"type":        "boolean",
				"description": "Recursive operation for list/search/mkdir/delete.",
			},
			"pattern": map[string]any{
				"type":        "string",
				"description": "Glob pattern for search, or text/regex pattern for grep.",
			},
			"lines": map[string]any{
				"type":        "integer",
				"description": "Number of lines for head/tail operations (default 10).",
			},
			"mode": map[string]any{
				"type":        "string",
				"description": "File mode for chmod (e.g., '0755', '0644').",
			},
		},
		"required": []string{"operation", "path"},
	}

	return NewFuncTool(
		"file_system",
		"Perform file operations: read, write, list, search, copy, move. Some sensitive paths are blocked.",
		schema,
		func(ctx context.Context, args json.RawMessage) (any, error) {
			var in fileSystemArgs
			if err := json.Unmarshal(args, &in); err != nil {
				return nil, fmt.Errorf("invalid file_system args: %w", err)
			}
			if in.Operation == "" {
				return nil, fmt.Errorf("operation is required")
			}
			if in.Path == "" {
				return nil, fmt.Errorf("path is required")
			}

			if err := validatePath(in.Path); err != nil {
				return &FileResult{Success: false, Error: err.Error()}, nil
			}
			if in.Target != "" {
				if err := validatePath(in.Target); err != nil {
					return &FileResult{Success: false, Error: err.Error()}, nil
				}
			}

			switch in.Operation {
			case "read":
				return fsReadFile(in.Path)
			case "write":
				return fsWriteFile(in.Path, in.Content, false)
			case "append":
				return fsWriteFile(in.Path, in.Content, true)
			case "list":
				return fsListDir(in.Path, in.Recursive)
			case "info":
				return fsFileInfo(in.Path)
			case "exists":
				return fsFileExists(in.Path)
			case "mkdir":
				return fsMakeDir(in.Path, in.Recursive)
			case "copy":
				return fsCopyFile(in.Path, in.Target)
			case "move":
				return fsMoveFile(in.Path, in.Target)
			case "search":
				return fsSearchFiles(in.Path, in.Pattern, in.Recursive)
			case "delete":
				return fsDelete(in.Path, in.Recursive)
			case "rename":
				return fsRename(in.Path, in.Target)
			case "chmod":
				return fsChmod(in.Path, in.Mode)
			case "head":
				return fsHead(in.Path, in.Lines)
			case "tail":
				return fsTail(in.Path, in.Lines)
			case "grep":
				return fsGrep(in.Path, in.Pattern, in.Recursive)
			case "symlink":
				return fsSymlink(in.Path, in.Target)
			default:
				return nil, fmt.Errorf("unsupported operation %q", in.Operation)
			}
		},
	)
}

func validatePath(path string) error {
	if strings.HasPrefix(path, "~") {
		home, err := os.UserHomeDir()
		if err == nil {
			path = filepath.Join(home, path[1:])
		}
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("invalid path: %w", err)
	}

	for _, blocked := range blockedPaths {
		expandedBlocked := blocked
		if strings.HasPrefix(blocked, "~") {
			home, _ := os.UserHomeDir()
			expandedBlocked = filepath.Join(home, blocked[1:])
		}
		if strings.HasPrefix(absPath, expandedBlocked) {
			return fmt.Errorf("access to path %q is blocked", path)
		}
	}

	return nil
}

func fsReadFile(path string) (*FileResult, error) {
	info, err := os.Stat(path)
	if err != nil {
		return &FileResult{Success: false, Error: err.Error()}, nil
	}

	if info.Size() > 5*1024*1024 {
		return &FileResult{Success: false, Error: fmt.Sprintf("file too large (%d bytes, max 5MB)", info.Size())}, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return &FileResult{Success: false, Error: err.Error()}, nil
	}

	if isBinary(data) {
		return &FileResult{Success: false, Error: "file appears to be binary"}, nil
	}

	return &FileResult{
		Success: true,
		Data: map[string]any{
			"content": string(data),
			"size":    info.Size(),
			"path":    path,
		},
	}, nil
}

func fsWriteFile(path, content string, appendMode bool) (*FileResult, error) {
	var flag int
	if appendMode {
		flag = os.O_WRONLY | os.O_CREATE | os.O_APPEND
	} else {
		flag = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return &FileResult{Success: false, Error: err.Error()}, nil
	}

	file, err := os.OpenFile(path, flag, 0644)
	if err != nil {
		return &FileResult{Success: false, Error: err.Error()}, nil
	}
	defer file.Close()

	n, err := file.WriteString(content)
	if err != nil {
		return &FileResult{Success: false, Error: err.Error()}, nil
	}

	action := "written"
	if appendMode {
		action = "appended"
	}

	return &FileResult{
		Success: true,
		Message: fmt.Sprintf("%d bytes %s to %s", n, action, path),
		Data:    map[string]any{"bytesWritten": n, "path": path},
	}, nil
}

func fsListDir(path string, recursive bool) (*FileResult, error) {
	var files []FileInfo
	maxFiles := 1000

	walkFn := func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if len(files) >= maxFiles {
			return filepath.SkipAll
		}

		relPath, _ := filepath.Rel(path, p)
		if relPath == "." {
			return nil
		}

		files = append(files, FileInfo{
			Name:    info.Name(),
			Path:    relPath,
			Size:    info.Size(),
			IsDir:   info.IsDir(),
			Mode:    info.Mode().String(),
			ModTime: info.ModTime().Format("2006-01-02 15:04:05"),
		})

		if !recursive && info.IsDir() && p != path {
			return filepath.SkipDir
		}
		return nil
	}

	if err := filepath.Walk(path, walkFn); err != nil {
		return &FileResult{Success: false, Error: err.Error()}, nil
	}

	return &FileResult{
		Success: true,
		Data: map[string]any{
			"files":     files,
			"count":     len(files),
			"truncated": len(files) >= maxFiles,
		},
	}, nil
}

func fsFileInfo(path string) (*FileResult, error) {
	info, err := os.Stat(path)
	if err != nil {
		return &FileResult{Success: false, Error: err.Error()}, nil
	}

	return &FileResult{
		Success: true,
		Data: FileInfo{
			Name:    info.Name(),
			Path:    path,
			Size:    info.Size(),
			IsDir:   info.IsDir(),
			Mode:    info.Mode().String(),
			ModTime: info.ModTime().Format("2006-01-02 15:04:05"),
		},
	}, nil
}

func fsFileExists(path string) (*FileResult, error) {
	_, err := os.Stat(path)
	return &FileResult{
		Success: true,
		Data:    map[string]any{"exists": err == nil, "path": path},
	}, nil
}

func fsMakeDir(path string, recursive bool) (*FileResult, error) {
	var err error
	if recursive {
		err = os.MkdirAll(path, 0755)
	} else {
		err = os.Mkdir(path, 0755)
	}

	if err != nil {
		return &FileResult{Success: false, Error: err.Error()}, nil
	}

	return &FileResult{Success: true, Message: fmt.Sprintf("directory created: %s", path)}, nil
}

func fsCopyFile(src, dst string) (*FileResult, error) {
	srcFile, err := os.Open(src)
	if err != nil {
		return &FileResult{Success: false, Error: err.Error()}, nil
	}
	defer srcFile.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return &FileResult{Success: false, Error: err.Error()}, nil
	}

	dstFile, err := os.Create(dst)
	if err != nil {
		return &FileResult{Success: false, Error: err.Error()}, nil
	}
	defer dstFile.Close()

	n, err := io.Copy(dstFile, srcFile)
	if err != nil {
		return &FileResult{Success: false, Error: err.Error()}, nil
	}

	return &FileResult{Success: true, Message: fmt.Sprintf("copied %d bytes from %s to %s", n, src, dst)}, nil
}

func fsMoveFile(src, dst string) (*FileResult, error) {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return &FileResult{Success: false, Error: err.Error()}, nil
	}

	if err := os.Rename(src, dst); err != nil {
		return &FileResult{Success: false, Error: err.Error()}, nil
	}

	return &FileResult{Success: true, Message: fmt.Sprintf("moved %s to %s", src, dst)}, nil
}

func fsSearchFiles(basePath, pattern string, recursive bool) (*FileResult, error) {
	var matches []string
	maxMatches := 500

	walkFn := func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if len(matches) >= maxMatches {
			return filepath.SkipAll
		}

		if !recursive && info.IsDir() && path != basePath {
			return filepath.SkipDir
		}

		if info.IsDir() {
			return nil
		}

		matched, err := filepath.Match(pattern, info.Name())
		if err != nil {
			return nil
		}

		if matched {
			relPath, _ := filepath.Rel(basePath, path)
			matches = append(matches, relPath)
		}
		return nil
	}

	if err := filepath.Walk(basePath, walkFn); err != nil {
		return &FileResult{Success: false, Error: err.Error()}, nil
	}

	return &FileResult{
		Success: true,
		Data: map[string]any{
			"matches":   matches,
			"count":     len(matches),
			"pattern":   pattern,
			"truncated": len(matches) >= maxMatches,
		},
	}, nil
}

func fsDelete(path string, recursive bool) (*FileResult, error) {
	info, err := os.Stat(path)
	if err != nil {
		return &FileResult{Success: false, Error: err.Error()}, nil
	}

	if info.IsDir() {
		if !recursive {
			return &FileResult{Success: false, Error: "path is a directory; set recursive=true to delete"}, nil
		}
		if err := os.RemoveAll(path); err != nil {
			return &FileResult{Success: false, Error: err.Error()}, nil
		}
		return &FileResult{Success: true, Message: fmt.Sprintf("directory deleted: %s", path)}, nil
	}

	if err := os.Remove(path); err != nil {
		return &FileResult{Success: false, Error: err.Error()}, nil
	}
	return &FileResult{Success: true, Message: fmt.Sprintf("file deleted: %s", path)}, nil
}

func fsRename(src, dst string) (*FileResult, error) {
	if dst == "" {
		return &FileResult{Success: false, Error: "target is required for rename"}, nil
	}
	if err := os.Rename(src, dst); err != nil {
		return &FileResult{Success: false, Error: err.Error()}, nil
	}
	return &FileResult{Success: true, Message: fmt.Sprintf("renamed %s to %s", src, dst)}, nil
}

func fsChmod(path, mode string) (*FileResult, error) {
	if mode == "" {
		return &FileResult{Success: false, Error: "mode is required (e.g., \"0755\")"}, nil
	}
	var perm uint64
	var err error
	perm, err = strconv.ParseUint(mode, 8, 32)
	if err != nil {
		return &FileResult{Success: false, Error: fmt.Sprintf("invalid mode %q: %v", mode, err)}, nil
	}
	if err := os.Chmod(path, os.FileMode(perm)); err != nil {
		return &FileResult{Success: false, Error: err.Error()}, nil
	}
	return &FileResult{Success: true, Message: fmt.Sprintf("chmod %s %s", mode, path)}, nil
}

func fsHead(path string, n int) (*FileResult, error) {
	if n <= 0 {
		n = 10
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return &FileResult{Success: false, Error: err.Error()}, nil
	}
	lines := strings.SplitAfter(string(data), "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return &FileResult{
		Success: true,
		Data: map[string]any{
			"content":    strings.Join(lines, ""),
			"lines":      len(lines),
			"totalLines": strings.Count(string(data), "\n"),
			"path":       path,
		},
	}, nil
}

func fsTail(path string, n int) (*FileResult, error) {
	if n <= 0 {
		n = 10
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return &FileResult{Success: false, Error: err.Error()}, nil
	}
	allLines := strings.Split(string(data), "\n")
	// Remove trailing empty line from split
	if len(allLines) > 0 && allLines[len(allLines)-1] == "" {
		allLines = allLines[:len(allLines)-1]
	}
	start := 0
	if len(allLines) > n {
		start = len(allLines) - n
	}
	result := allLines[start:]
	return &FileResult{
		Success: true,
		Data: map[string]any{
			"content":    strings.Join(result, "\n") + "\n",
			"lines":      len(result),
			"totalLines": len(allLines),
			"path":       path,
		},
	}, nil
}

func fsGrep(path, pattern string, recursive bool) (*FileResult, error) {
	if pattern == "" {
		return &FileResult{Success: false, Error: "pattern is required for grep"}, nil
	}

	type grepMatch struct {
		File    string `json:"file"`
		Line    int    `json:"line"`
		Content string `json:"content"`
	}

	var matches []grepMatch
	maxMatches := 500

	grepFile := func(fpath string) error {
		data, err := os.ReadFile(fpath)
		if err != nil || isBinary(data) {
			return nil
		}
		lines := strings.Split(string(data), "\n")
		for i, line := range lines {
			if len(matches) >= maxMatches {
				return nil
			}
			if strings.Contains(line, pattern) {
				matches = append(matches, grepMatch{
					File:    fpath,
					Line:    i + 1,
					Content: strings.TrimRight(line, "\r\n"),
				})
			}
		}
		return nil
	}

	info, err := os.Stat(path)
	if err != nil {
		return &FileResult{Success: false, Error: err.Error()}, nil
	}

	if !info.IsDir() {
		_ = grepFile(path)
	} else {
		walkFn := func(p string, fi os.FileInfo, err error) error {
			if err != nil || fi.IsDir() {
				if !recursive && fi != nil && fi.IsDir() && p != path {
					return filepath.SkipDir
				}
				return nil
			}
			if len(matches) >= maxMatches {
				return filepath.SkipAll
			}
			return grepFile(p)
		}
		_ = filepath.Walk(path, walkFn)
	}

	return &FileResult{
		Success: true,
		Data: map[string]any{
			"matches":   matches,
			"count":     len(matches),
			"pattern":   pattern,
			"truncated": len(matches) >= maxMatches,
		},
	}, nil
}

func fsSymlink(target, linkPath string) (*FileResult, error) {
	if linkPath == "" {
		return &FileResult{Success: false, Error: "target is required for symlink (path=target, target=link_name)"}, nil
	}
	if err := os.Symlink(target, linkPath); err != nil {
		return &FileResult{Success: false, Error: err.Error()}, nil
	}
	return &FileResult{Success: true, Message: fmt.Sprintf("symlink created: %s -> %s", linkPath, target)}, nil
}
