package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type gitRepoArgs struct {
	URL       string `json:"url"`
	Branch    string `json:"branch,omitempty"`
	Tag       string `json:"tag,omitempty"`
	Commit    string `json:"commit,omitempty"`
	Path      string `json:"path,omitempty"`
	Depth     int    `json:"depth,omitempty"`
	Operation string `json:"operation,omitempty"`
}

// GitRepoResult contains the result of a git operation.
type GitRepoResult struct {
	Success   bool     `json:"success"`
	LocalPath string   `json:"localPath,omitempty"`
	Branch    string   `json:"branch,omitempty"`
	Commit    string   `json:"commit,omitempty"`
	Message   string   `json:"message,omitempty"`
	Error     string   `json:"error,omitempty"`
	Files     []string `json:"files,omitempty"`
	FileCount int      `json:"fileCount,omitempty"`
	RepoName  string   `json:"repoName,omitempty"`
	ClonedAt  string   `json:"clonedAt,omitempty"`
}

// GitFileContent represents content read from a git repo.
type GitFileContent struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Size    int64  `json:"size"`
	Error   string `json:"error,omitempty"`
}

func NewGitRepo() Tool {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"url": map[string]any{
				"type":        "string",
				"description": "The Git repository URL (HTTPS or SSH). Examples: https://github.com/owner/repo.git, git@github.com:owner/repo.git",
			},
			"branch": map[string]any{
				"type":        "string",
				"description": "Branch to checkout. Defaults to the default branch (usually main/master).",
			},
			"tag": map[string]any{
				"type":        "string",
				"description": "Tag to checkout. Takes precedence over branch if both specified.",
			},
			"commit": map[string]any{
				"type":        "string",
				"description": "Specific commit SHA to checkout. Takes precedence over branch/tag.",
			},
			"path": map[string]any{
				"type":        "string",
				"description": "Subdirectory path within the repo to focus on (for read_files operation).",
			},
			"depth": map[string]any{
				"type":        "integer",
				"description": "Clone depth for shallow clones. Use 1 for fastest clone with only latest commit. Default is 1.",
				"minimum":     1,
			},
			"operation": map[string]any{
				"type":        "string",
				"enum":        []string{"clone", "read_files", "list_files", "get_info", "read_file"},
				"description": "Operation: clone (clone repo), list_files (list files in path), read_files (read multiple files), read_file (read single file), get_info (repo metadata). Defaults to clone.",
			},
		},
		"required": []string{"url"},
	}

	return NewFuncTool(
		"git_repo",
		"Clone Git repositories and read code for context. Supports GitHub, GitLab, Bitbucket, and any Git URL. Can checkout specific branches, tags, or commits.",
		schema,
		func(ctx context.Context, args json.RawMessage) (any, error) {
			var in gitRepoArgs
			if err := json.Unmarshal(args, &in); err != nil {
				return nil, fmt.Errorf("invalid git_repo args: %w", err)
			}
			if in.URL == "" {
				return nil, fmt.Errorf("url is required")
			}

			operation := in.Operation
			if operation == "" {
				operation = "clone"
			}

			// Set default depth for shallow clone
			if in.Depth <= 0 {
				in.Depth = 1
			}

			switch operation {
			case "clone":
				return cloneRepo(ctx, in)
			case "list_files":
				return listRepoFiles(ctx, in)
			case "read_files":
				return readRepoFiles(ctx, in)
			case "read_file":
				return readSingleFile(ctx, in)
			case "get_info":
				return getRepoInfo(ctx, in)
			default:
				return nil, fmt.Errorf("unsupported operation %q", operation)
			}
		},
	)
}

// getRepoLocalPath returns the local path for a cloned repo.
func getRepoLocalPath(repoURL string) (string, string, error) {
	repoName := extractRepoName(repoURL)
	if repoName == "" {
		return "", "", fmt.Errorf("could not extract repo name from URL")
	}

	// Use system temp directory with a consistent subdirectory
	baseDir := filepath.Join(os.TempDir(), "ai-agent-repos")
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return "", "", fmt.Errorf("failed to create repos directory: %w", err)
	}

	// Create a unique path based on URL hash to avoid conflicts
	urlHash := simpleHash(repoURL)
	localPath := filepath.Join(baseDir, fmt.Sprintf("%s-%s", repoName, urlHash[:8]))

	return localPath, repoName, nil
}

// extractRepoName extracts the repository name from a Git URL.
func extractRepoName(url string) string {
	// Handle SSH format: git@github.com:owner/repo.git
	if strings.Contains(url, ":") && strings.Contains(url, "@") {
		parts := strings.Split(url, ":")
		if len(parts) >= 2 {
			path := parts[len(parts)-1]
			// path is now "owner/repo.git", extract just repo
			pathParts := strings.Split(path, "/")
			if len(pathParts) > 0 {
				return cleanRepoName(pathParts[len(pathParts)-1])
			}
		}
	}

	// Handle HTTPS format: https://github.com/owner/repo.git
	parts := strings.Split(url, "/")
	if len(parts) > 0 {
		return cleanRepoName(parts[len(parts)-1])
	}

	return ""
}

func cleanRepoName(name string) string {
	name = strings.TrimSuffix(name, ".git")
	name = strings.TrimSpace(name)
	return name
}

func simpleHash(s string) string {
	var h uint64 = 0
	for _, c := range s {
		h = h*31 + uint64(c)
	}
	return fmt.Sprintf("%016x", h)
}

func cloneRepo(ctx context.Context, args gitRepoArgs) (*GitRepoResult, error) {
	localPath, repoName, err := getRepoLocalPath(args.URL)
	if err != nil {
		return &GitRepoResult{Success: false, Error: err.Error()}, nil
	}

	// Check if already cloned
	if _, err := os.Stat(filepath.Join(localPath, ".git")); err == nil {
		// Repo exists, do a pull instead
		return pullRepo(ctx, localPath, args)
	}

	// Build clone command
	cmdArgs := []string{"clone"}

	// Add depth for shallow clone
	if args.Depth > 0 {
		cmdArgs = append(cmdArgs, "--depth", fmt.Sprintf("%d", args.Depth))
	}

	// Add branch/tag specification
	if args.Tag != "" {
		cmdArgs = append(cmdArgs, "--branch", args.Tag)
	} else if args.Branch != "" {
		cmdArgs = append(cmdArgs, "--branch", args.Branch)
	}

	cmdArgs = append(cmdArgs, args.URL, localPath)

	cmd := exec.CommandContext(ctx, "git", cmdArgs...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return &GitRepoResult{
			Success: false,
			Error:   fmt.Sprintf("clone failed: %v - %s", err, string(output)),
		}, nil
	}

	// Checkout specific commit if specified
	if args.Commit != "" {
		if err := checkoutCommit(ctx, localPath, args.Commit); err != nil {
			return &GitRepoResult{
				Success: false,
				Error:   fmt.Sprintf("checkout commit failed: %v", err),
			}, nil
		}
	}

	// Get current commit info
	commit, _ := getCurrentCommit(ctx, localPath)
	branch, _ := getCurrentBranch(ctx, localPath)

	return &GitRepoResult{
		Success:   true,
		LocalPath: localPath,
		RepoName:  repoName,
		Branch:    branch,
		Commit:    commit,
		Message:   "Repository cloned successfully",
		ClonedAt:  time.Now().Format(time.RFC3339),
	}, nil
}

func pullRepo(ctx context.Context, localPath string, args gitRepoArgs) (*GitRepoResult, error) {
	// If specific commit requested, just checkout that commit
	if args.Commit != "" {
		if err := checkoutCommit(ctx, localPath, args.Commit); err != nil {
			return &GitRepoResult{
				Success: false,
				Error:   fmt.Sprintf("checkout commit failed: %v", err),
			}, nil
		}
	} else {
		// Switch branch if needed
		if args.Branch != "" || args.Tag != "" {
			ref := args.Branch
			if args.Tag != "" {
				ref = args.Tag
			}
			cmd := exec.CommandContext(ctx, "git", "-C", localPath, "checkout", ref)
			if output, err := cmd.CombinedOutput(); err != nil {
				return &GitRepoResult{
					Success: false,
					Error:   fmt.Sprintf("checkout failed: %v - %s", err, string(output)),
				}, nil
			}
		}

		// Pull latest changes
		cmd := exec.CommandContext(ctx, "git", "-C", localPath, "pull", "--ff-only")
		cmd.CombinedOutput() // Ignore errors for shallow clones
	}

	commit, _ := getCurrentCommit(ctx, localPath)
	branch, _ := getCurrentBranch(ctx, localPath)

	return &GitRepoResult{
		Success:   true,
		LocalPath: localPath,
		RepoName:  extractRepoName(args.URL),
		Branch:    branch,
		Commit:    commit,
		Message:   "Repository updated successfully",
	}, nil
}

func checkoutCommit(ctx context.Context, localPath, commit string) error {
	// For shallow clones, we may need to fetch the specific commit
	fetchCmd := exec.CommandContext(ctx, "git", "-C", localPath, "fetch", "--depth=1", "origin", commit)
	fetchCmd.CombinedOutput() // Ignore errors

	cmd := exec.CommandContext(ctx, "git", "-C", localPath, "checkout", commit)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%v - %s", err, string(output))
	}
	return nil
}

func getCurrentCommit(ctx context.Context, localPath string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", localPath, "rev-parse", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func getCurrentBranch(ctx context.Context, localPath string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", localPath, "rev-parse", "--abbrev-ref", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func listRepoFiles(ctx context.Context, args gitRepoArgs) (map[string]any, error) {
	// First ensure repo is cloned
	result, err := cloneRepo(ctx, args)
	if err != nil {
		return nil, err
	}
	if !result.Success {
		return map[string]any{
			"success": false,
			"error":   result.Error,
		}, nil
	}

	basePath := result.LocalPath
	if args.Path != "" {
		basePath = filepath.Join(result.LocalPath, args.Path)
	}

	var files []string
	maxFiles := 1000 // Limit to prevent overwhelming output

	err = filepath.Walk(basePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip files with errors
		}
		if len(files) >= maxFiles {
			return filepath.SkipAll
		}

		// Skip .git directory
		if info.IsDir() && info.Name() == ".git" {
			return filepath.SkipDir
		}

		if !info.IsDir() {
			relPath, _ := filepath.Rel(result.LocalPath, path)
			files = append(files, relPath)
		}
		return nil
	})

	if err != nil {
		return map[string]any{
			"success": false,
			"error":   err.Error(),
		}, nil
	}

	return map[string]any{
		"success":   true,
		"localPath": result.LocalPath,
		"repoName":  result.RepoName,
		"path":      args.Path,
		"files":     files,
		"fileCount": len(files),
		"truncated": len(files) >= maxFiles,
	}, nil
}

func readRepoFiles(ctx context.Context, args gitRepoArgs) (map[string]any, error) {
	// First ensure repo is cloned
	result, err := cloneRepo(ctx, args)
	if err != nil {
		return nil, err
	}
	if !result.Success {
		return map[string]any{
			"success": false,
			"error":   result.Error,
		}, nil
	}

	basePath := result.LocalPath
	if args.Path != "" {
		basePath = filepath.Join(result.LocalPath, args.Path)
	}

	var contents []GitFileContent
	maxTotalSize := int64(1024 * 1024) // 1MB total limit
	maxFileSize := int64(100 * 1024)   // 100KB per file limit
	currentSize := int64(0)
	maxFiles := 50 // Limit number of files

	// Common code file extensions
	codeExtensions := map[string]bool{
		".go": true, ".py": true, ".js": true, ".ts": true, ".jsx": true, ".tsx": true,
		".java": true, ".c": true, ".cpp": true, ".h": true, ".hpp": true,
		".rs": true, ".rb": true, ".php": true, ".swift": true, ".kt": true,
		".scala": true, ".clj": true, ".ex": true, ".exs": true,
		".cs": true, ".fs": true, ".vb": true,
		".sh": true, ".bash": true, ".zsh": true, ".fish": true,
		".sql": true, ".graphql": true, ".proto": true,
		".yaml": true, ".yml": true, ".json": true, ".toml": true, ".xml": true,
		".md": true, ".txt": true, ".rst": true,
		".html": true, ".css": true, ".scss": true, ".less": true,
		".dockerfile": true, ".tf": true, ".hcl": true,
		".makefile": true, ".cmake": true,
	}

	err = filepath.Walk(basePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if len(contents) >= maxFiles || currentSize >= maxTotalSize {
			return filepath.SkipAll
		}

		// Skip .git directory
		if info.IsDir() && info.Name() == ".git" {
			return filepath.SkipDir
		}

		if info.IsDir() {
			return nil
		}

		// Check extension
		ext := strings.ToLower(filepath.Ext(path))
		baseName := strings.ToLower(info.Name())

		// Include files with known extensions or known config files
		isCode := codeExtensions[ext] ||
			baseName == "makefile" ||
			baseName == "dockerfile" ||
			baseName == "gemfile" ||
			baseName == "rakefile" ||
			strings.HasPrefix(baseName, ".")

		if !isCode {
			return nil
		}

		// Skip large files
		if info.Size() > maxFileSize {
			relPath, _ := filepath.Rel(result.LocalPath, path)
			contents = append(contents, GitFileContent{
				Path:  relPath,
				Size:  info.Size(),
				Error: "file too large (>100KB)",
			})
			return nil
		}

		// Read file content
		data, err := os.ReadFile(path)
		if err != nil {
			relPath, _ := filepath.Rel(result.LocalPath, path)
			contents = append(contents, GitFileContent{
				Path:  relPath,
				Error: err.Error(),
			})
			return nil
		}

		// Skip binary files
		if isBinary(data) {
			return nil
		}

		relPath, _ := filepath.Rel(result.LocalPath, path)
		contents = append(contents, GitFileContent{
			Path:    relPath,
			Content: string(data),
			Size:    info.Size(),
		})
		currentSize += info.Size()

		return nil
	})

	if err != nil {
		return map[string]any{
			"success": false,
			"error":   err.Error(),
		}, nil
	}

	return map[string]any{
		"success":   true,
		"localPath": result.LocalPath,
		"repoName":  result.RepoName,
		"path":      args.Path,
		"files":     contents,
		"fileCount": len(contents),
		"totalSize": currentSize,
	}, nil
}

func readSingleFile(ctx context.Context, args gitRepoArgs) (map[string]any, error) {
	if args.Path == "" {
		return nil, fmt.Errorf("path is required for read_file operation")
	}

	// First ensure repo is cloned
	result, err := cloneRepo(ctx, args)
	if err != nil {
		return nil, err
	}
	if !result.Success {
		return map[string]any{
			"success": false,
			"error":   result.Error,
		}, nil
	}

	filePath := filepath.Join(result.LocalPath, args.Path)

	info, err := os.Stat(filePath)
	if err != nil {
		return map[string]any{
			"success": false,
			"error":   fmt.Sprintf("file not found: %s", args.Path),
		}, nil
	}

	if info.IsDir() {
		return map[string]any{
			"success": false,
			"error":   fmt.Sprintf("path is a directory: %s", args.Path),
		}, nil
	}

	// Check file size (limit to 500KB for single file)
	if info.Size() > 500*1024 {
		return map[string]any{
			"success": false,
			"error":   "file too large (>500KB)",
			"size":    info.Size(),
			"path":    args.Path,
		}, nil
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return map[string]any{
			"success": false,
			"error":   err.Error(),
		}, nil
	}

	if isBinary(data) {
		return map[string]any{
			"success": false,
			"error":   "file appears to be binary",
			"path":    args.Path,
		}, nil
	}

	return map[string]any{
		"success":   true,
		"path":      args.Path,
		"content":   string(data),
		"size":      info.Size(),
		"localPath": result.LocalPath,
		"repoName":  result.RepoName,
	}, nil
}

func getRepoInfo(ctx context.Context, args gitRepoArgs) (map[string]any, error) {
	// First ensure repo is cloned
	result, err := cloneRepo(ctx, args)
	if err != nil {
		return nil, err
	}
	if !result.Success {
		return map[string]any{
			"success": false,
			"error":   result.Error,
		}, nil
	}

	info := map[string]any{
		"success":   true,
		"localPath": result.LocalPath,
		"repoName":  result.RepoName,
		"branch":    result.Branch,
		"commit":    result.Commit,
		"url":       args.URL,
	}

	// Get remote info
	cmd := exec.CommandContext(ctx, "git", "-C", result.LocalPath, "remote", "-v")
	if output, err := cmd.Output(); err == nil {
		info["remotes"] = strings.TrimSpace(string(output))
	}

	// Get last commit message
	cmd = exec.CommandContext(ctx, "git", "-C", result.LocalPath, "log", "-1", "--pretty=%s")
	if output, err := cmd.Output(); err == nil {
		info["lastCommitMessage"] = strings.TrimSpace(string(output))
	}

	// Get last commit author
	cmd = exec.CommandContext(ctx, "git", "-C", result.LocalPath, "log", "-1", "--pretty=%an <%ae>")
	if output, err := cmd.Output(); err == nil {
		info["lastCommitAuthor"] = strings.TrimSpace(string(output))
	}

	// Get last commit date
	cmd = exec.CommandContext(ctx, "git", "-C", result.LocalPath, "log", "-1", "--pretty=%ci")
	if output, err := cmd.Output(); err == nil {
		info["lastCommitDate"] = strings.TrimSpace(string(output))
	}

	// Count files
	fileCount := 0
	filepath.Walk(result.LocalPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() && info.Name() == ".git" {
			return filepath.SkipDir
		}
		if !info.IsDir() {
			fileCount++
		}
		return nil
	})
	info["fileCount"] = fileCount

	return info, nil
}

// isBinary checks if data appears to be binary.
func isBinary(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	// Check first 512 bytes for null bytes (common in binary files)
	checkLen := 512
	if len(data) < checkLen {
		checkLen = len(data)
	}
	for i := 0; i < checkLen; i++ {
		if data[i] == 0 {
			return true
		}
	}
	return false
}

// CleanupRepos removes all cloned repositories.
func CleanupRepos() error {
	baseDir := filepath.Join(os.TempDir(), "ai-agent-repos")
	return os.RemoveAll(baseDir)
}

// GetRepoPath returns the local path for a repository URL if it exists.
func GetRepoPath(repoURL string) (string, error) {
	localPath, _, err := getRepoLocalPath(repoURL)
	if err != nil {
		return "", err
	}

	if _, err := os.Stat(filepath.Join(localPath, ".git")); err != nil {
		return "", fmt.Errorf("repository not cloned")
	}

	return localPath, nil
}
