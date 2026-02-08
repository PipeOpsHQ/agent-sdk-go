package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	osExec "os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	agentfw "github.com/PipeOpsHQ/agent-sdk-go/framework/agent"
	devuiapi "github.com/PipeOpsHQ/agent-sdk-go/framework/devui/api"
	devuiauth "github.com/PipeOpsHQ/agent-sdk-go/framework/devui/auth"
	authsqlite "github.com/PipeOpsHQ/agent-sdk-go/framework/devui/auth/sqlite"
	catalogsqlite "github.com/PipeOpsHQ/agent-sdk-go/framework/devui/catalog/sqlite"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/graph"
	basicgraph "github.com/PipeOpsHQ/agent-sdk-go/framework/graphs/basic"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/llm"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/observe"
	observesqlite "github.com/PipeOpsHQ/agent-sdk-go/framework/observe/store/sqlite"
	providerfactory "github.com/PipeOpsHQ/agent-sdk-go/framework/providers/factory"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/runtime/distributed"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/runtime/queue/redisstreams"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/state"
	statefactory "github.com/PipeOpsHQ/agent-sdk-go/framework/state/factory"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/tools"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/workflow"
)

const (
	defaultSystemPrompt = "You are a practical AI assistant. Be concise, accurate, and actionable."
	defaultWorkflow     = basicgraph.Name
)

type cliOptions struct {
	workflow       string
	tools          []string
	systemPrompt   string
	promptTemplate string
}

type uiOptions struct {
	addr             string
	dbPath           string
	attemptsPath     string
	redisAddr        string
	redisPassword    string
	redisDB          int
	queuePrefix      string
	queueGroup       string
	requireAPIKey    bool
	allowLocalNoAuth bool
	open             bool
}

type localPlaygroundRunner struct {
	store    state.Store
	observer observe.Sink
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
	case "ui":
		runUI(ctx, os.Args[2:], false)
	case "ui-api":
		runUI(ctx, os.Args[2:], true)
	case "ui-admin":
		runUIAdmin(ctx, os.Args[2:])
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

	// Determine system prompt with template support
	customPrompt := strings.TrimSpace(opts.systemPrompt)
	if customPrompt == "" {
		customPrompt = strings.TrimSpace(os.Getenv("AGENT_SYSTEM_PROMPT"))
	}

	templateName := strings.TrimSpace(opts.promptTemplate)
	if templateName == "" {
		templateName = strings.TrimSpace(os.Getenv("AGENT_PROMPT_TEMPLATE"))
	}

	// Build prompt context for interpolation
	toolNames := make([]string, 0, len(toolset))
	for _, tool := range toolset {
		toolNames = append(toolNames, tool.Definition().Name)
	}

	ctx := PromptContext{
		ToolCount: len(toolset),
		ToolNames: toolNames,
		Provider:  provider.Name(),
	}

	// Build final prompt using template or custom
	prompt := BuildPrompt(customPrompt, templateName, ctx)

	// Validate and warn about prompt quality
	if warnings := ValidatePrompt(prompt); len(warnings) > 0 {
		for _, warning := range warnings {
			log.Printf("prompt warning: %s", warning)
		}
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

func (r *localPlaygroundRunner) Run(ctx context.Context, req devuiapi.PlaygroundRequest) (devuiapi.PlaygroundResponse, error) {
	provider, err := providerfactory.FromEnv(ctx)
	if err != nil {
		return devuiapi.PlaygroundResponse{}, fmt.Errorf("provider setup failed: %w", err)
	}

	opts := cliOptions{
		workflow:     strings.TrimSpace(req.Workflow),
		tools:        append([]string(nil), req.Tools...),
		systemPrompt: strings.TrimSpace(req.SystemPrompt),
	}
	agent, err := buildAgent(provider, r.store, r.observer, opts)
	if err != nil {
		return devuiapi.PlaygroundResponse{}, fmt.Errorf("agent create failed: %w", err)
	}

	if strings.TrimSpace(opts.workflow) == "" {
		result, runErr := agent.RunDetailed(ctx, req.Input)
		if runErr != nil {
			return devuiapi.PlaygroundResponse{}, runErr
		}
		return devuiapi.PlaygroundResponse{
			Status:    "completed",
			Output:    result.Output,
			RunID:     result.RunID,
			SessionID: result.SessionID,
			Provider:  provider.Name(),
		}, nil
	}

	exec, err := buildExecutor(agent, r.store, r.observer, opts)
	if err != nil {
		return devuiapi.PlaygroundResponse{}, fmt.Errorf("executor create failed: %w", err)
	}
	result, runErr := exec.Run(ctx, req.Input)
	if runErr != nil {
		return devuiapi.PlaygroundResponse{}, runErr
	}
	return devuiapi.PlaygroundResponse{
		Status:    "completed",
		Output:    result.Output,
		RunID:     result.RunID,
		SessionID: result.SessionID,
		Provider:  provider.Name(),
	}, nil
}

func runUI(ctx context.Context, args []string, remoteMode bool) {
	opts := parseUIArgs(args, remoteMode)
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	var store state.Store
	var err error
	store, err = statefactory.FromEnv(ctx)
	if err != nil {
		log.Printf("state store unavailable: %v", err)
	}
	defer closeStore(store)

	traceStore, err := observesqlite.New(opts.dbPath)
	if err != nil {
		log.Printf("trace store unavailable: %v", err)
	}
	if traceStore != nil {
		defer func() { _ = traceStore.Close() }()
	}

	catalogStore, err := catalogsqlite.New(opts.dbPath)
	if err != nil {
		log.Printf("catalog store unavailable: %v", err)
	}
	if catalogStore != nil {
		defer func() { _ = catalogStore.Close() }()
	}

	authStore, err := authsqlite.New(opts.dbPath)
	if err != nil {
		if opts.requireAPIKey {
			log.Fatalf("auth store setup failed: %v", err)
		}
		log.Printf("auth store unavailable (local no-auth mode only): %v", err)
	}
	if authStore != nil {
		defer func() { _ = authStore.Close() }()
	}

	auditStore, err := devuiapi.NewSQLiteAuditStore(opts.dbPath)
	if err != nil {
		log.Printf("audit store unavailable: %v", err)
	}
	if closer, ok := auditStore.(interface{ Close() error }); ok {
		defer func() { _ = closer.Close() }()
	}

	runtimeService, closeRuntime := buildRuntimeService(ctx, store, opts)
	defer closeRuntime()

	observer := observe.Sink(observe.NoopSink{})
	if traceStore != nil {
		async := observe.NewAsyncSink(observe.SinkFunc(func(ctx context.Context, event observe.Event) error {
			return traceStore.SaveEvent(ctx, event)
		}), 256)
		observer = async
		defer async.Close()
	}

	server := devuiapi.NewServer(devuiapi.Config{
		Addr:             opts.addr,
		StateStore:       store,
		TraceStore:       traceStore,
		CatalogStore:     catalogStore,
		AuthStore:        authStore,
		AuditStore:       auditStore,
		Runtime:          runtimeService,
		Playground:       &localPlaygroundRunner{store: store, observer: observer},
		RequireAPIKey:    opts.requireAPIKey,
		AllowLocalNoAuth: opts.allowLocalNoAuth,
	})

	log.Printf("DevUI listening on http://%s", opts.addr)
	if opts.open {
		openBrowser("http://" + opts.addr)
	}
	if err := server.ListenAndServe(ctx); err != nil && !errors.Is(err, context.Canceled) {
		log.Fatalf("devui server failed: %v", err)
	}
}

func runUIAdmin(ctx context.Context, args []string) {
	if len(args) == 0 {
		log.Fatal("usage: ui-admin create-key [--role=viewer|operator|admin]")
	}
	action := strings.TrimSpace(args[0])
	if action != "create-key" {
		log.Fatalf("unknown ui-admin action %q", action)
	}
	opts := parseUIArgs(nil, true)
	role := devuiauth.RoleAdmin
	for _, arg := range args[1:] {
		if strings.HasPrefix(arg, "--role=") {
			role = devuiauth.Role(strings.TrimSpace(strings.TrimPrefix(arg, "--role=")))
		}
	}
	if !role.Valid() {
		log.Fatalf("invalid role %q", role)
	}

	authStore, err := authsqlite.New(opts.dbPath)
	if err != nil {
		log.Fatalf("auth store setup failed: %v", err)
	}
	defer func() { _ = authStore.Close() }()

	key, err := authStore.CreateKey(ctx, role)
	if err != nil {
		log.Fatalf("create key failed: %v", err)
	}
	fmt.Printf("id=%s role=%s\n", key.ID, key.Role)
	fmt.Printf("secret=%s\n", key.Secret)
}

func parseUIArgs(args []string, remoteMode bool) uiOptions {
	dbPath := strings.TrimSpace(os.Getenv("AGENT_DEVUI_DB_PATH"))
	if dbPath == "" {
		dbPath = "./.ai-agent/devui.db"
	}
	attemptsPath := strings.TrimSpace(os.Getenv("AGENT_RUNTIME_ATTEMPTS_DB_PATH"))
	if attemptsPath == "" {
		attemptsPath = filepath.Join(filepath.Dir(dbPath), "runtime.db")
	}
	opts := uiOptions{
		addr:             strings.TrimSpace(os.Getenv("AGENT_UI_ADDR")),
		dbPath:           dbPath,
		attemptsPath:     attemptsPath,
		redisAddr:        strings.TrimSpace(os.Getenv("AGENT_REDIS_ADDR")),
		redisPassword:    strings.TrimSpace(os.Getenv("AGENT_REDIS_PASSWORD")),
		redisDB:          parseIntEnv("AGENT_REDIS_DB", 0),
		queuePrefix:      strings.TrimSpace(os.Getenv("AGENT_RUNTIME_QUEUE_PREFIX")),
		queueGroup:       strings.TrimSpace(os.Getenv("AGENT_RUNTIME_QUEUE_GROUP")),
		requireAPIKey:    parseBoolEnv("AGENT_UI_REQUIRE_API_KEY", remoteMode),
		allowLocalNoAuth: parseBoolEnv("AGENT_UI_ALLOW_LOCAL_NOAUTH", !remoteMode),
		open:             parseBoolEnv("AGENT_UI_OPEN", false),
	}
	if opts.addr == "" {
		opts.addr = "127.0.0.1:7070"
	}
	if opts.redisAddr == "" {
		opts.redisAddr = "127.0.0.1:6379"
	}
	if opts.queuePrefix == "" {
		opts.queuePrefix = "aiag:queue"
	}
	if opts.queueGroup == "" {
		opts.queueGroup = "workers"
	}

	for _, arg := range args {
		switch {
		case strings.HasPrefix(arg, "--ui-addr="):
			opts.addr = strings.TrimSpace(strings.TrimPrefix(arg, "--ui-addr="))
		case strings.HasPrefix(arg, "--ui-db-path="):
			opts.dbPath = strings.TrimSpace(strings.TrimPrefix(arg, "--ui-db-path="))
		case strings.HasPrefix(arg, "--ui-attempts-db-path="):
			opts.attemptsPath = strings.TrimSpace(strings.TrimPrefix(arg, "--ui-attempts-db-path="))
		case strings.HasPrefix(arg, "--ui-open="):
			opts.open = parseBoolString(strings.TrimSpace(strings.TrimPrefix(arg, "--ui-open=")), opts.open)
		case strings.HasPrefix(arg, "--ui-allow-local-noauth="):
			opts.allowLocalNoAuth = parseBoolString(strings.TrimSpace(strings.TrimPrefix(arg, "--ui-allow-local-noauth=")), opts.allowLocalNoAuth)
		case strings.HasPrefix(arg, "--ui-require-api-key="):
			opts.requireAPIKey = parseBoolString(strings.TrimSpace(strings.TrimPrefix(arg, "--ui-require-api-key=")), opts.requireAPIKey)
		}
	}
	return opts
}

func parseIntEnv(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}

func parseBoolString(raw string, fallback bool) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

func openBrowser(target string) {
	if strings.TrimSpace(target) == "" {
		return
	}
	cmd := osExec.Command("open", target)
	if err := cmd.Start(); err != nil {
		log.Printf("failed to open browser: %v", err)
	}
}

func buildRuntimeService(ctx context.Context, store state.Store, opts uiOptions) (devuiapi.RuntimeService, func()) {
	if store == nil {
		return nil, func() {}
	}

	attemptStore, err := distributed.NewSQLiteAttemptStore(opts.attemptsPath)
	if err != nil {
		log.Printf("runtime attempt store unavailable: %v", err)
		return nil, func() {}
	}

	queueStore, err := redisstreams.New(
		opts.redisAddr,
		redisstreams.WithPassword(opts.redisPassword),
		redisstreams.WithDB(opts.redisDB),
		redisstreams.WithPrefix(opts.queuePrefix),
		redisstreams.WithGroup(opts.queueGroup),
	)
	if err != nil {
		_ = attemptStore.Close()
		log.Printf("runtime queue unavailable: %v", err)
		return nil, func() {}
	}

	service, err := distributed.NewCoordinator(
		store,
		attemptStore,
		queueStore,
		observe.NoopSink{},
		distributed.DistributedConfig{
			Queue: distributed.QueueConfig{
				Name:   "runs",
				Prefix: opts.queuePrefix,
			},
		},
	)
	if err != nil {
		_ = queueStore.Close()
		_ = attemptStore.Close()
		log.Printf("runtime service unavailable: %v", err)
		return nil, func() {}
	}

	// Ensure consumer group exists before UI operations query queue stats.
	if _, claimErr := queueStore.Claim(ctx, "devui-bootstrap", time.Millisecond, 1); claimErr != nil {
		log.Printf("runtime bootstrap claim warning: %v", claimErr)
	}

	return service, func() {
		_ = queueStore.Close()
		_ = attemptStore.Close()
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
		case strings.HasPrefix(arg, "--prompt-template="):
			opts.promptTemplate = strings.TrimSpace(strings.TrimPrefix(arg, "--prompt-template="))
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
	fmt.Println("  go run ./framework ui [--ui-addr=127.0.0.1:7070] [--ui-open=true]")
	fmt.Println("  go run ./framework ui-api [--ui-addr=0.0.0.0:7070]")
	fmt.Println("  go run ./framework ui-admin create-key [--role=admin]")
	fmt.Println()
	fmt.Println("Agent Configuration:")
	fmt.Println("  --system-prompt=TEXT          Custom system prompt (takes precedence over template)")
	fmt.Println("  --prompt-template=NAME        Use predefined prompt template")
	fmt.Println("  --tools=@default              Tool bundle (comma-separated)")
	fmt.Println("  --workflow=basic              Graph workflow name")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  go run ./framework run --prompt-template=analyst -- \"analyze this log\"")
	fmt.Println("  go run ./framework run --prompt-template=engineer -- \"fix this code\"")
	fmt.Println("  go run ./framework run --system-prompt=\"You are a helpful assistant.\" -- \"help me\"")
	fmt.Println()
	fmt.Printf("  available workflows: %s\n", strings.Join(workflow.Names(), ", "))
	fmt.Printf("  available tools: %s\n", strings.Join(tools.ToolNames(), ", "))
	fmt.Printf("  available bundles: %s\n", strings.Join(tools.BundleNames(), ", "))
	fmt.Printf("  available prompt templates: %s\n", strings.Join(AvailablePromptNames(), ", "))
	fmt.Println()
	fmt.Println("Environment Variables:")
	fmt.Println("  AGENT_SYSTEM_PROMPT          Custom system prompt")
	fmt.Println("  AGENT_PROMPT_TEMPLATE        Prompt template name")
	fmt.Println("  AGENT_TOOLS                  Tool selection (comma-separated)")
	fmt.Println("  AGENT_WORKFLOW               Graph workflow name")
}
