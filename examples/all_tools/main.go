// All-Tools DevOps Agent
//
// This example demonstrates an agent with access to ALL 33 built-in tools
// across every bundle: @default, @security, @code, @network, @system,
// @memory, @container, @kubernetes, @linux, @scheduling.
//
// The agent acts as a full-stack DevOps assistant that can:
//   - Analyze code repos, generate diffs, search codebases
//   - Make HTTP requests, scrape web pages
//   - Execute shell commands, manage files and directories
//   - Build and run Docker containers, manage Compose stacks
//   - Interact with Kubernetes clusters (kubectl, k3s)
//   - Encode/decode data, parse JSON/URLs, generate UUIDs
//   - Detect and redact secrets, generate hashes
//   - Store context across interactions via memory_store
//
// Usage:
//   go run . "clone https://github.com/org/repo and find all TODO comments"
//   go run . "check if port 8080 is responding and show the response headers"
//   go run . "list all running docker containers and their resource usage"
//   go run . "scan this text for leaked secrets: aws_key=AKIA..."
//   go run . ui                    # Launch DevUI
//   go run . ui --ui-addr=:9090    # DevUI on custom port
//
// Environment Variables:
//   OPENAI_API_KEY or ANTHROPIC_API_KEY - LLM provider credentials

package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	agentfw "github.com/PipeOpsHQ/agent-sdk-go/framework/agent"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/devui"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/flow"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/observe"
	observesqlite "github.com/PipeOpsHQ/agent-sdk-go/framework/observe/store/sqlite"
	providerfactory "github.com/PipeOpsHQ/agent-sdk-go/framework/providers/factory"
	statefactory "github.com/PipeOpsHQ/agent-sdk-go/framework/state/factory"
	fwtools "github.com/PipeOpsHQ/agent-sdk-go/framework/tools"
)

const systemPrompt = `You are an expert full-stack DevOps engineer with access to a comprehensive toolkit.

You can:
- Clone and analyze Git repositories, search code, generate diffs
- Make HTTP/API requests (http_client or curl with auth/TLS), scrape web pages
- Execute shell commands, manage files and directories
- Build/run Docker containers, manage Docker Compose stacks
- Interact with Kubernetes clusters via kubectl and k3s
- Encode/decode data (base64, URL, JSON), generate UUIDs and hashes
- Detect and redact secrets from text
- Store notes and context using memory_store for multi-step tasks
- DNS lookups, port scanning, ping, network diagnostics
- List/find processes, check disk usage, get system info
- Create/extract archives (tar, tar.gz, zip)
- View and search log files (tail, head, grep, journalctl)
- Schedule recurring tasks with cron_manager

Guidelines:
1. Use the most appropriate tool for each task
2. Chain tools together for complex multi-step operations
3. Always redact secrets before displaying sensitive output
4. Provide clear, actionable results with context
5. When working with infrastructure, verify state before and after changes`

func main() {
	if len(os.Args) > 1 && strings.ToLower(os.Args[1]) == "ui" {
		runDevUI()
		return
	}

	prompt := strings.TrimSpace(strings.Join(os.Args[1:], " "))
	if prompt == "" {
		printUsage()
		os.Exit(1)
	}

	ctx := context.Background()

	provider, err := providerfactory.FromEnv(ctx)
	if err != nil {
		log.Fatalf("provider setup failed: %v", err)
	}

	store, err := statefactory.FromEnv(ctx)
	if err != nil {
		log.Fatalf("state store setup failed: %v", err)
	}
	defer func() { _ = store.Close() }()

	observer, closeObserver := buildObserver()
	defer closeObserver()

	// Select ALL tools via the @all bundle.
	allTools, err := fwtools.BuildSelection([]string{"@all"})
	if err != nil {
		log.Fatalf("tool selection failed: %v", err)
	}

	opts := []agentfw.Option{
		agentfw.WithSystemPrompt(systemPrompt),
		agentfw.WithStore(store),
		agentfw.WithObserver(observer),
		agentfw.WithMaxIterations(15),
		agentfw.WithMaxOutputTokens(4000),
		agentfw.WithParallelToolCalls(true),
		agentfw.WithMaxParallelTools(100),
		agentfw.WithRetryPolicy(agentfw.RetryPolicy{
			MaxAttempts: 3,
			BaseBackoff: 500 * time.Millisecond,
			MaxBackoff:  5 * time.Second,
		}),
	}
	for _, t := range allTools {
		opts = append(opts, agentfw.WithTool(t))
	}

	agent, err := agentfw.New(provider, opts...)
	if err != nil {
		log.Fatalf("agent create failed: %v", err)
	}

	fmt.Printf("🛠  All-Tools Agent (%d tools loaded)\n", len(allTools))
	fmt.Printf("📝 Prompt: %s\n\n", prompt)

	result, err := agent.RunDetailed(ctx, prompt)
	if err != nil {
		log.Fatalf("run failed: %v", err)
	}

	fmt.Println(strings.Repeat("─", 60))
	fmt.Println(result.Output)
	fmt.Println(strings.Repeat("─", 60))
	fmt.Printf("\nrun_id=%s session_id=%s provider=%s\n", result.RunID, result.SessionID, result.Provider)
}

func runDevUI() {
	flow.MustRegister(&flow.Definition{
		Name:         "all-tools-agent",
		Description:  "Full-stack DevOps agent with all 33 tools: code analysis, HTTP/web, shell/files, Docker, Kubernetes, encoding, security, memory, DNS, process/disk/system info, archives, logs, and cron scheduling.",
		Tools:        []string{"@all"},
		SystemPrompt: systemPrompt,
		InputExample: "Generate a UUID, hash it with SHA256, base64-encode the result, then curl https://httpbin.org/post with the encoded value as the body",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"input": map[string]any{
					"type":        "string",
					"description": "Multi-step task description — the agent chains tools automatically.",
				},
			},
			"required": []string{"input"},
		},
	})

	if err := devui.Start(context.Background(), devui.Options{
		DefaultFlow: "all-tools-agent",
	}); err != nil {
		log.Fatal(err)
	}
}

func buildObserver() (observe.Sink, func()) {
	traceStore, err := observesqlite.New("./.ai-agent/all-tools.db")
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

func printUsage() {
	fmt.Println(`All-Tools DevOps Agent — 33 tools at your disposal

Usage:
  go run . "<prompt>"                # Run a task
  go run . ui                        # Launch DevUI
  go run . ui --ui-addr=:9090        # DevUI on custom port

Tool Bundles:
  @default     calculator, json_parser, base64_codec, timestamp_converter,
               uuid_generator, url_parser, regex_matcher, text_processor
  @security    secret_redactor, hash_generator
  @code        git_repo, code_search, diff_generator
  @network     http_client, web_scraper, curl, dns_lookup, network_utils
  @system      shell_command, file_system, env_vars, tmpdir, process_manager,
               disk_usage, system_info, log_viewer, archive
  @memory      memory_store
  @container   docker, docker_compose
  @kubernetes  kubectl, k3s
  @scheduling  cron_manager
  @linux       curl, dns_lookup, network_utils, process_manager, disk_usage,
               system_info, archive, log_viewer

Examples:
  go run . "generate a UUID and base64-encode it"
  go run . "clone https://github.com/golang/go and count .go files"
  go run . "GET https://httpbin.org/json and parse the response"
  go run . "find all files larger than 1MB in the current directory"
  go run . "check for secrets in: api_key=sk-abc123 password=hunter2"
  go run . "list running docker containers"
  go run . "ping google.com and scan common ports"
  go run . "show disk usage for the current directory"
  go run . "tail the last 50 lines of /var/log/syslog"

Environment Variables:
  OPENAI_API_KEY      OpenAI API key
  ANTHROPIC_API_KEY   Anthropic API key
  LLM_PROVIDER        Provider (openai, anthropic, gemini, ollama)`)
}
