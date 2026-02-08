package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	agentfw "github.com/nitrocode/ai-agents/framework/agent"
	"github.com/nitrocode/ai-agents/framework/graph"
	basicgraph "github.com/nitrocode/ai-agents/framework/graphs/basic"
	_ "github.com/nitrocode/ai-agents/framework/graphs/secops"
	"github.com/nitrocode/ai-agents/framework/llm"
	"github.com/nitrocode/ai-agents/framework/observe"
	observesqlite "github.com/nitrocode/ai-agents/framework/observe/store/sqlite"
	providerfactory "github.com/nitrocode/ai-agents/framework/providers/factory"
	"github.com/nitrocode/ai-agents/framework/state"
	statefactory "github.com/nitrocode/ai-agents/framework/state/factory"
	"github.com/nitrocode/ai-agents/framework/tools"
	"github.com/nitrocode/ai-agents/framework/workflow"
)

const (
	defaultSystemPrompt = "You are a practical AI assistant. Be concise, accurate, and actionable."
	defaultWorkflow     = basicgraph.Name
)

type cliOptions struct {
	workflow     string
	tools        []string
	systemPrompt string
}

func main() {
	ctx := context.Background()
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch strings.TrimSpace(os.Args[1]) {
	case "run":
		runSingle(ctx, os.Args[2:])
	case "graph-run":
		runGraph(ctx, os.Args[2:])
	case "graph-resume":
		resumeGraph(ctx, os.Args[2:])
	case "sessions":
		listSessions(ctx, os.Args[2:])
	case "help", "-h", "--help":
		printUsage()
	default:
		runSingle(ctx, os.Args[1:])
	}
}

func runSingle(ctx context.Context, args []string) {
	opts, positional := parseArgs(args)
	input := normalizeInput(positional)
	if input == "" {
		log.Fatal("input cannot be empty")
	}

	provider, store := buildRuntimeDeps(ctx)
	defer closeStore(store)
	observer, closeObserver := buildObserver()
	defer closeObserver()

	agent, err := buildAgent(provider, store, observer, opts)
	if err != nil {
		log.Fatalf("failed to create agent: %v", err)
	}
	output, err := agent.Run(ctx, input)
	if err != nil {
		log.Fatalf("run failed: %v", err)
	}
	fmt.Println(output)
}

func runGraph(ctx context.Context, args []string) {
	opts, positional := parseArgs(args)
	input := normalizeInput(positional)
	if input == "" {
		log.Fatal("input cannot be empty")
	}

	provider, store := buildRuntimeDeps(ctx)
	defer closeStore(store)
	observer, closeObserver := buildObserver()
	defer closeObserver()

	agent, err := buildAgent(provider, nil, observer, opts)
	if err != nil {
		log.Fatalf("failed to create agent: %v", err)
	}
	exec, err := buildExecutor(agent, store, observer, opts)
	if err != nil {
		log.Fatalf("failed to create graph executor: %v", err)
	}
	result, err := exec.Run(ctx, input)
	if err != nil {
		log.Fatalf("graph run failed: %v", err)
	}
	fmt.Println(result.Output)
}

func resumeGraph(ctx context.Context, args []string) {
	opts, positional := parseArgs(args)
	if len(positional) < 1 {
		log.Fatal("usage: graph-resume [--workflow=name] [--tools=list] <run-id>")
	}
	runID := strings.TrimSpace(positional[0])
	if runID == "" {
		log.Fatal("run-id cannot be empty")
	}

	provider, store := buildRuntimeDeps(ctx)
	defer closeStore(store)
	observer, closeObserver := buildObserver()
	defer closeObserver()

	agent, err := buildAgent(provider, nil, observer, opts)
	if err != nil {
		log.Fatalf("failed to create agent: %v", err)
	}
	exec, err := buildExecutor(agent, store, observer, opts)
	if err != nil {
		log.Fatalf("failed to create graph executor: %v", err)
	}
	result, err := exec.Resume(ctx, runID)
	if err != nil {
		log.Fatalf("graph resume failed: %v", err)
	}
	fmt.Println(result.Output)
}

func listSessions(ctx context.Context, args []string) {
	sessionID := ""
	if len(args) > 0 {
		sessionID = strings.TrimSpace(args[0])
	}

	store, err := statefactory.FromEnv(ctx)
	if err != nil {
		log.Fatal(err)
	}
	defer closeStore(store)

	runs, err := store.ListRuns(ctx, state.ListRunsQuery{SessionID: sessionID, Limit: 100})
	if err != nil {
		log.Fatalf("list sessions failed: %v", err)
	}
	sort.Slice(runs, func(i, j int) bool {
		left, right := time.Time{}, time.Time{}
		if runs[i].UpdatedAt != nil {
			left = *runs[i].UpdatedAt
		}
		if runs[j].UpdatedAt != nil {
			right = *runs[j].UpdatedAt
		}
		return left.After(right)
	})
	for _, run := range runs {
		updated := "-"
		if run.UpdatedAt != nil {
			updated = run.UpdatedAt.UTC().Format(time.RFC3339)
		}
		fmt.Printf("%s\t%s\t%s\t%s\t%s\n", run.RunID, run.SessionID, run.Status, run.Provider, updated)
	}
}

func buildRuntimeDeps(ctx context.Context) (llm.Provider, state.Store) {
	provider, err := providerfactory.FromEnv(ctx)
	if err != nil {
		log.Fatal(err)
	}
	store, err := statefactory.FromEnv(ctx)
	if err != nil {
		log.Fatal(err)
	}
	return provider, store
}

func buildAgent(provider llm.Provider, store state.Store, observer observe.Sink, opts cliOptions) (*agentfw.Agent, error) {
	selection := opts.tools
	if len(selection) == 0 {
		selection = splitCSV(strings.TrimSpace(os.Getenv("AGENT_TOOLS")))
	}
	if len(selection) == 0 {
		selection = []string{"@default"}
	}
	toolset, err := tools.BuildSelection(selection)
	if err != nil {
		return nil, fmt.Errorf("resolve tools: %w", err)
	}
	prompt := strings.TrimSpace(opts.systemPrompt)
	if prompt == "" {
		prompt = strings.TrimSpace(os.Getenv("AGENT_SYSTEM_PROMPT"))
	}
	if prompt == "" {
		prompt = defaultSystemPrompt
	}

	agentOpts := []agentfw.Option{
		agentfw.WithSystemPrompt(prompt),
		agentfw.WithObserver(observer),
		agentfw.WithStore(store),
	}
	for _, tool := range toolset {
		agentOpts = append(agentOpts, agentfw.WithTool(tool))
	}
	return agentfw.New(provider, agentOpts...)
}

func buildExecutor(agent *agentfw.Agent, store state.Store, observer observe.Sink, opts cliOptions) (*graph.Executor, error) {
	name := strings.TrimSpace(opts.workflow)
	if name == "" {
		name = strings.TrimSpace(os.Getenv("AGENT_WORKFLOW"))
	}
	if name == "" {
		name = defaultWorkflow
	}

	builder, ok := workflow.Get(name)
	if !ok {
		return nil, fmt.Errorf("unknown workflow %q (available: %s)", name, strings.Join(workflow.Names(), ", "))
	}
	exec, err := builder.NewExecutor(agent, store, "")
	if err != nil {
		return nil, err
	}
	exec.SetObserver(observer)
	return exec, nil
}

func buildObserver() (observe.Sink, func()) {
	if !parseBoolEnv("AGENT_OBSERVE_ENABLED", true) {
		return observe.NoopSink{}, func() {}
	}
	dbPath := strings.TrimSpace(os.Getenv("AGENT_DEVUI_DB_PATH"))
	if dbPath == "" {
		dbPath = "./.ai-agent/devui.db"
	}
	traceStore, err := observesqlite.New(dbPath)
	if err != nil {
		log.Printf("observer disabled: %v", err)
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

func parseArgs(args []string) (cliOptions, []string) {
	opts := cliOptions{}
	positional := make([]string, 0, len(args))
	for _, arg := range args {
		switch {
		case strings.HasPrefix(arg, "--workflow="):
			opts.workflow = strings.TrimSpace(strings.TrimPrefix(arg, "--workflow="))
		case strings.HasPrefix(arg, "--tools="):
			opts.tools = splitCSV(strings.TrimPrefix(arg, "--tools="))
		case strings.HasPrefix(arg, "--system-prompt="):
			opts.systemPrompt = strings.TrimSpace(strings.TrimPrefix(arg, "--system-prompt="))
		default:
			positional = append(positional, arg)
		}
	}
	return opts, positional
}

func normalizeInput(args []string) string {
	if len(args) > 0 && strings.TrimSpace(args[0]) == "--" {
		args = args[1:]
	}
	return strings.TrimSpace(strings.Join(args, " "))
}

func splitCSV(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func parseBoolEnv(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	switch strings.ToLower(value) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

func closeStore(store state.Store) {
	if store == nil {
		return
	}
	if err := store.Close(); err != nil {
		log.Printf("state store close failed: %v", err)
	}
}

func printUsage() {
	fmt.Println("PipeOps Agent Framework CLI")
	fmt.Println("Usage:")
	fmt.Println("  go run ./framework run [--tools=@default] -- \"your prompt\"")
	fmt.Println("  go run ./framework graph-run [--workflow=basic] [--tools=@default] -- \"your prompt\"")
	fmt.Println("  go run ./framework graph-resume [--workflow=basic] [--tools=@default] <run-id>")
	fmt.Println("  go run ./framework sessions [session-id]")
	fmt.Printf("  available workflows: %s\n", strings.Join(workflow.Names(), ", "))
	fmt.Printf("  available tools: %s\n", strings.Join(tools.ToolNames(), ", "))
	fmt.Printf("  available bundles: %s\n", strings.Join(tools.BundleNames(), ", "))
}
