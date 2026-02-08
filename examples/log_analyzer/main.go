// Log Analyzer Agent
//
// This agent analyzes application logs, identifies issues, suggests fixes,
// and can optionally clone a repository and create a PR with the fixes.
//
// Usage:
//   go run . analyze <log-file>
//   go run . analyze --repo=https://github.com/owner/repo <log-file>
//   go run . analyze --repo=https://github.com/owner/repo --create-pr <log-file>
//   cat logs.txt | go run . analyze
//
// Environment Variables:
//   OPENAI_API_KEY or ANTHROPIC_API_KEY - LLM provider credentials
//   GITHUB_TOKEN - GitHub token for creating PRs
//   STATE_BACKEND=sqlite (default) or redis

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	agentfw "github.com/PipeOpsHQ/agent-sdk-go/framework/agent"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/llm"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/observe"
	observesqlite "github.com/PipeOpsHQ/agent-sdk-go/framework/observe/store/sqlite"
	providerfactory "github.com/PipeOpsHQ/agent-sdk-go/framework/providers/factory"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/state"
	statefactory "github.com/PipeOpsHQ/agent-sdk-go/framework/state/factory"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/tools"
)

const logAnalyzerPrompt = `You are an expert log analyst and software engineer.

Your task is to:
1. Analyze the provided logs to identify errors, warnings, and issues
2. Classify issues by severity (critical, high, medium, low)
3. Identify root causes where possible
4. Suggest specific, actionable fixes with code examples
5. If code context is provided, suggest exact file and line changes

Output format:
## Issues Found
- [SEVERITY] Issue description
  - Root cause: explanation
  - Fix: specific action

## Suggested Code Changes
If you can identify specific code fixes, provide them in diff format:
` + "```diff" + `
- old line
+ new line
` + "```" + `

## Summary
Brief summary of overall health and priority actions.

Be concise and actionable. Focus on the most impactful issues first.`

const codeFixPrompt = `You are a senior software engineer fixing issues identified in log analysis.

Based on the log analysis and repository code, your task is to:
1. Identify the exact files that need modification
2. Create specific code fixes for each issue
3. Ensure fixes are backwards compatible
4. Add appropriate error handling
5. Include any necessary tests

For each fix, use the file_system tool to:
1. Read the relevant file
2. Write the fixed version

Be precise and surgical in your fixes. Only change what's necessary.`

type Config struct {
	LogFile  string
	RepoURL  string
	Branch   string
	CreatePR bool
	PRTitle  string
	PRBody   string
	DryRun   bool
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	cmd := strings.ToLower(os.Args[1])
	switch cmd {
	case "analyze":
		runAnalyze(os.Args[2:])
	case "help", "-h", "--help":
		printUsage()
	default:
		// Treat first arg as log file for convenience
		runAnalyze(os.Args[1:])
	}
}

func runAnalyze(args []string) {
	cfg := parseConfig(args)
	ctx := context.Background()

	// Read log input
	logContent, err := readLogInput(cfg, args)
	if err != nil {
		log.Fatalf("Failed to read logs: %v", err)
	}

	// Build dependencies
	provider, store, observer, cleanup := buildDeps(ctx)
	defer cleanup()

	// Phase 1: Analyze logs
	fmt.Println("🔍 Analyzing logs...")
	analysis, err := analyzeLogs(ctx, provider, store, observer, logContent)
	if err != nil {
		log.Fatalf("Log analysis failed: %v", err)
	}

	fmt.Println("\n📋 Analysis Results:")
	fmt.Println(strings.Repeat("─", 60))
	fmt.Println(analysis)
	fmt.Println(strings.Repeat("─", 60))

	// If no repo URL provided, we're done
	if cfg.RepoURL == "" {
		fmt.Println("\n✅ Analysis complete.")
		fmt.Println("💡 Tip: Add --repo=<url> to analyze with code context and suggest fixes")
		return
	}

	// Phase 2: Clone repo and analyze with code context
	fmt.Printf("\n📦 Cloning repository: %s\n", cfg.RepoURL)
	repoPath, err := cloneRepo(ctx, cfg.RepoURL, cfg.Branch)
	if err != nil {
		log.Fatalf("Failed to clone repository: %v", err)
	}
	fmt.Printf("   Cloned to: %s\n", repoPath)

	// Phase 3: Generate code fixes
	fmt.Println("\n🔧 Generating code fixes...")
	fixes, err := generateFixes(ctx, provider, store, observer, analysis, repoPath)
	if err != nil {
		log.Fatalf("Failed to generate fixes: %v", err)
	}

	fmt.Println("\n📝 Suggested Fixes:")
	fmt.Println(strings.Repeat("─", 60))
	fmt.Println(fixes)
	fmt.Println(strings.Repeat("─", 60))

	// Phase 4: Apply fixes and create PR if requested
	if cfg.CreatePR && !cfg.DryRun {
		fmt.Println("\n🚀 Creating pull request...")
		prURL, err := createPullRequest(ctx, provider, store, observer, cfg, repoPath, analysis, fixes)
		if err != nil {
			log.Fatalf("Failed to create PR: %v", err)
		}
		fmt.Printf("\n✅ Pull request created: %s\n", prURL)
	} else if cfg.CreatePR && cfg.DryRun {
		fmt.Println("\n🔍 Dry run mode - skipping PR creation")
		fmt.Println("   Would create PR with the above fixes")
	} else {
		fmt.Println("\n✅ Analysis complete with code context.")
		fmt.Println("💡 Tip: Add --create-pr to automatically create a pull request")
	}
}

func parseConfig(args []string) Config {
	cfg := Config{
		PRTitle: "fix: automated fixes from log analysis",
		Branch:  "",
	}

	for _, arg := range args {
		switch {
		case strings.HasPrefix(arg, "--repo="):
			cfg.RepoURL = strings.TrimPrefix(arg, "--repo=")
		case strings.HasPrefix(arg, "--branch="):
			cfg.Branch = strings.TrimPrefix(arg, "--branch=")
		case arg == "--create-pr":
			cfg.CreatePR = true
		case strings.HasPrefix(arg, "--pr-title="):
			cfg.PRTitle = strings.TrimPrefix(arg, "--pr-title=")
		case arg == "--dry-run":
			cfg.DryRun = true
		case !strings.HasPrefix(arg, "--"):
			cfg.LogFile = arg
		}
	}

	return cfg
}

func readLogInput(cfg Config, args []string) (string, error) {
	// Try to read from specified file
	if cfg.LogFile != "" {
		content, err := os.ReadFile(cfg.LogFile)
		if err != nil {
			return "", fmt.Errorf("read file %s: %w", cfg.LogFile, err)
		}
		return string(content), nil
	}

	// Try to read from stdin
	stat, err := os.Stdin.Stat()
	if err == nil && (stat.Mode()&os.ModeCharDevice) == 0 {
		content, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", fmt.Errorf("read stdin: %w", err)
		}
		return string(content), nil
	}

	return "", fmt.Errorf("no log input provided. Specify a file or pipe logs to stdin")
}

func buildDeps(ctx context.Context) (llm.Provider, state.Store, observe.Sink, func()) {
	provider, err := providerfactory.FromEnv(ctx)
	if err != nil {
		log.Fatalf("LLM provider setup failed: %v", err)
	}

	store, err := statefactory.FromEnv(ctx)
	if err != nil {
		log.Fatalf("State store setup failed: %v", err)
	}

	observer, closeObserver := buildObserver()

	return provider, store, observer, func() {
		closeObserver()
		_ = store.Close()
	}
}

func buildObserver() (observe.Sink, func()) {
	dbPath := "./.ai-agent/log-analyzer.db"
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return observe.NoopSink{}, func() {}
	}

	traceStore, err := observesqlite.New(dbPath)
	if err != nil {
		return observe.NoopSink{}, func() {}
	}

	async := observe.NewAsyncSink(observe.SinkFunc(func(ctx context.Context, event observe.Event) error {
		return traceStore.SaveEvent(ctx, event)
	}), 256)

	return async, func() {
		async.Close()
		_ = traceStore.Close()
	}
}

func analyzeLogs(ctx context.Context, provider llm.Provider, store state.Store, observer observe.Sink, logs string) (string, error) {
	// Preprocess logs
	processed := preprocessLogs(logs)

	// Build agent with analysis tools
	agent, err := agentfw.New(provider,
		agentfw.WithSystemPrompt(logAnalyzerPrompt),
		agentfw.WithStore(store),
		agentfw.WithObserver(observer),
		agentfw.WithMaxIterations(3),
		agentfw.WithMaxOutputTokens(2000),
		agentfw.WithRetryPolicy(agentfw.RetryPolicy{
			MaxAttempts: 2,
			BaseBackoff: 200 * time.Millisecond,
			MaxBackoff:  2 * time.Second,
		}),
	)
	if err != nil {
		return "", fmt.Errorf("create analysis agent: %w", err)
	}

	// Run analysis
	prompt := fmt.Sprintf("Analyze these application logs and identify issues:\n\n%s", processed)
	result, err := agent.Run(ctx, prompt)
	if err != nil {
		return "", fmt.Errorf("run analysis: %w", err)
	}

	return result, nil
}

func preprocessLogs(logs string) string {
	// Redact sensitive data
	redacted := redactSensitiveData(logs)

	// Truncate if too long (keep most recent)
	lines := strings.Split(redacted, "\n")
	if len(lines) > 500 {
		// Keep first 50 and last 450 lines
		kept := append(lines[:50], lines[len(lines)-450:]...)
		redacted = strings.Join(kept, "\n")
		redacted = "... (truncated) ...\n" + redacted
	}

	return redacted
}

var (
	sensitivePatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)(api[_-]?key|token|secret|password|passwd|authorization)\s*[:=]\s*[^\s,;]+`),
		regexp.MustCompile(`(?i)bearer\s+[a-z0-9\-._~+/]+=*`),
		regexp.MustCompile(`(?i)(aws_access_key_id|aws_secret_access_key)\s*[:=]\s*[^\s,;]+`),
	}
)

func redactSensitiveData(logs string) string {
	result := logs
	for _, pattern := range sensitivePatterns {
		result = pattern.ReplaceAllString(result, "[REDACTED]")
	}
	return result
}

func cloneRepo(ctx context.Context, repoURL, branch string) (string, error) {
	// Create temp directory for the repo
	repoName := extractRepoName(repoURL)
	baseDir := filepath.Join(os.TempDir(), "log-analyzer-repos")
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return "", fmt.Errorf("create repos dir: %w", err)
	}

	localPath := filepath.Join(baseDir, repoName)

	// Check if already cloned
	if _, err := os.Stat(filepath.Join(localPath, ".git")); err == nil {
		// Pull latest
		cmd := exec.CommandContext(ctx, "git", "-C", localPath, "pull", "--ff-only")
		cmd.Run() // Ignore errors
		return localPath, nil
	}

	// Clone the repo
	args := []string{"clone", "--depth", "1"}
	if branch != "" {
		args = append(args, "--branch", branch)
	}
	args = append(args, repoURL, localPath)

	cmd := exec.CommandContext(ctx, "git", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git clone failed: %v - %s", err, string(output))
	}

	return localPath, nil
}

func extractRepoName(url string) string {
	// Handle SSH format: git@github.com:owner/repo.git
	if strings.Contains(url, ":") && strings.Contains(url, "@") {
		parts := strings.Split(url, ":")
		if len(parts) >= 2 {
			path := parts[len(parts)-1]
			pathParts := strings.Split(path, "/")
			if len(pathParts) > 0 {
				return strings.TrimSuffix(pathParts[len(pathParts)-1], ".git")
			}
		}
	}

	// Handle HTTPS format
	parts := strings.Split(url, "/")
	if len(parts) > 0 {
		return strings.TrimSuffix(parts[len(parts)-1], ".git")
	}

	return "repo"
}

func generateFixes(ctx context.Context, provider llm.Provider, store state.Store, observer observe.Sink, analysis, repoPath string) (string, error) {
	// Get relevant tools for code modification
	selectedTools, err := tools.BuildSelection([]string{"file_system", "code_search"})
	if err != nil {
		return "", fmt.Errorf("build tools: %w", err)
	}

	opts := []agentfw.Option{
		agentfw.WithSystemPrompt(codeFixPrompt),
		agentfw.WithStore(store),
		agentfw.WithObserver(observer),
		agentfw.WithMaxIterations(10),
		agentfw.WithMaxOutputTokens(4000),
		agentfw.WithRetryPolicy(agentfw.RetryPolicy{
			MaxAttempts: 2,
			BaseBackoff: 200 * time.Millisecond,
			MaxBackoff:  2 * time.Second,
		}),
	}
	for _, t := range selectedTools {
		opts = append(opts, agentfw.WithTool(t))
	}

	agent, err := agentfw.New(provider, opts...)
	if err != nil {
		return "", fmt.Errorf("create fix agent: %w", err)
	}

	prompt := fmt.Sprintf(`Based on this log analysis, generate code fixes.

Repository path: %s

Log Analysis:
%s

Instructions:
1. Use file_system tool to read relevant files from the repository
2. Identify the exact changes needed
3. Provide the fixes in a clear, applicable format

Focus on the highest priority issues first.`, repoPath, analysis)

	result, err := agent.Run(ctx, prompt)
	if err != nil {
		return "", fmt.Errorf("generate fixes: %w", err)
	}

	return result, nil
}

func createPullRequest(ctx context.Context, provider llm.Provider, store state.Store, observer observe.Sink, cfg Config, repoPath, analysis, fixes string) (string, error) {
	githubToken := os.Getenv("GITHUB_TOKEN")
	if githubToken == "" {
		return "", fmt.Errorf("GITHUB_TOKEN environment variable is required for PR creation")
	}

	// Create a new branch
	branchName := fmt.Sprintf("fix/log-analysis-%d", time.Now().Unix())

	// Create branch
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "checkout", "-b", branchName)
	if output, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("create branch: %v - %s", err, string(output))
	}

	// Apply fixes using the agent
	selectedTools, err := tools.BuildSelection([]string{"file_system"})
	if err != nil {
		return "", fmt.Errorf("build tools: %w", err)
	}

	opts := []agentfw.Option{
		agentfw.WithSystemPrompt(`You are applying code fixes to files. 
Use the file_system tool with operation "write" to apply each fix.
Be precise and only modify what's necessary.`),
		agentfw.WithStore(store),
		agentfw.WithObserver(observer),
		agentfw.WithMaxIterations(20),
	}
	for _, t := range selectedTools {
		opts = append(opts, agentfw.WithTool(t))
	}

	agent, err := agentfw.New(provider, opts...)
	if err != nil {
		return "", fmt.Errorf("create apply agent: %w", err)
	}

	applyPrompt := fmt.Sprintf(`Apply these fixes to the repository at %s:

%s

Use file_system tool to write the fixed files.`, repoPath, fixes)

	_, err = agent.Run(ctx, applyPrompt)
	if err != nil {
		return "", fmt.Errorf("apply fixes: %w", err)
	}

	// Stage and commit changes
	cmd = exec.CommandContext(ctx, "git", "-C", repoPath, "add", "-A")
	if output, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("git add: %v - %s", err, string(output))
	}

	commitMsg := fmt.Sprintf("%s\n\nAutomated fixes based on log analysis.\n\n## Analysis Summary\n%s", cfg.PRTitle, truncate(analysis, 500))
	cmd = exec.CommandContext(ctx, "git", "-C", repoPath, "commit", "-m", commitMsg)
	if output, err := cmd.CombinedOutput(); err != nil {
		// Check if there are no changes
		if strings.Contains(string(output), "nothing to commit") {
			return "", fmt.Errorf("no changes to commit - fixes may not have been applicable")
		}
		return "", fmt.Errorf("git commit: %v - %s", err, string(output))
	}

	// Push branch
	cmd = exec.CommandContext(ctx, "git", "-C", repoPath, "push", "-u", "origin", branchName)
	cmd.Env = append(os.Environ(), fmt.Sprintf("GIT_ASKPASS=echo"), fmt.Sprintf("GIT_PASSWORD=%s", githubToken))
	if output, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("git push: %v - %s", err, string(output))
	}

	// Create PR using GitHub API
	prURL, err := createGitHubPR(ctx, cfg.RepoURL, branchName, cfg.PRTitle, analysis, githubToken)
	if err != nil {
		return "", fmt.Errorf("create GitHub PR: %w", err)
	}

	return prURL, nil
}

func createGitHubPR(ctx context.Context, repoURL, branchName, title, body, token string) (string, error) {
	// Extract owner/repo from URL
	owner, repo := parseGitHubURL(repoURL)
	if owner == "" || repo == "" {
		return "", fmt.Errorf("could not parse GitHub URL: %s", repoURL)
	}

	// Create PR via GitHub API
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/pulls", owner, repo)

	prBody := map[string]string{
		"title": title,
		"head":  branchName,
		"base":  "main",
		"body":  fmt.Sprintf("## Automated Log Analysis Fixes\n\n%s", truncate(body, 2000)),
	}

	jsonBody, _ := json.Marshal(prBody)

	cmd := exec.CommandContext(ctx, "curl", "-s", "-X", "POST",
		"-H", "Accept: application/vnd.github+json",
		"-H", fmt.Sprintf("Authorization: Bearer %s", token),
		"-H", "X-GitHub-Api-Version: 2022-11-28",
		"-d", string(jsonBody),
		apiURL)

	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("GitHub API call failed: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(output, &result); err != nil {
		return "", fmt.Errorf("parse GitHub response: %w", err)
	}

	if htmlURL, ok := result["html_url"].(string); ok {
		return htmlURL, nil
	}

	if errMsg, ok := result["message"].(string); ok {
		return "", fmt.Errorf("GitHub API error: %s", errMsg)
	}

	return "", fmt.Errorf("unexpected GitHub API response")
}

func parseGitHubURL(url string) (owner, repo string) {
	// Handle various GitHub URL formats
	url = strings.TrimSuffix(url, ".git")

	// SSH format: git@github.com:owner/repo
	if strings.HasPrefix(url, "git@github.com:") {
		parts := strings.Split(strings.TrimPrefix(url, "git@github.com:"), "/")
		if len(parts) >= 2 {
			return parts[0], parts[1]
		}
	}

	// HTTPS format: https://github.com/owner/repo
	if strings.Contains(url, "github.com") {
		parts := strings.Split(url, "/")
		for i, part := range parts {
			if part == "github.com" && i+2 < len(parts) {
				return parts[i+1], parts[i+2]
			}
		}
	}

	return "", ""
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func printUsage() {
	fmt.Println(`Log Analyzer Agent - Analyze logs and create fixes

Usage:
  go run . analyze <log-file> [options]
  go run . analyze --repo=<url> <log-file>
  cat logs.txt | go run . analyze [options]

Options:
  --repo=<url>       Git repository URL for code context
  --branch=<name>    Branch to checkout (default: main)
  --create-pr        Create a pull request with fixes
  --pr-title=<text>  PR title (default: "fix: automated fixes from log analysis")
  --dry-run          Show what would be done without making changes

Examples:
  # Analyze logs only
  go run . analyze application.log

  # Analyze with code context
  go run . analyze --repo=https://github.com/myorg/myapp application.log

  # Analyze and create PR
  go run . analyze --repo=https://github.com/myorg/myapp --create-pr application.log

  # From stdin
  kubectl logs my-pod | go run . analyze

Environment Variables:
  OPENAI_API_KEY      OpenAI API key
  ANTHROPIC_API_KEY   Anthropic API key
  GITHUB_TOKEN        GitHub token for PR creation
  LLM_PROVIDER        Provider to use (openai, anthropic, gemini, ollama)
  STATE_BACKEND       State backend (sqlite, redis)`)
}
