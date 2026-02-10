// Package devui provides a Genkit-style developer UI for the agent framework.
//
// Users import this package in their own application and call [Start] to
// launch the DevUI alongside their registered flows, tools, and workflows.
//
// Example:
//
//	package main
//
//	import (
//	    "context"
//	    "log"
//
//	    "github.com/PipeOpsHQ/agent-sdk-go/devui"
//	    "github.com/PipeOpsHQ/agent-sdk-go/flow"
//	)
//
//	func main() {
//	    // Register your custom flows
//	    flow.MustRegister(&flow.Definition{
//	        Name:        "my-agent",
//	        Description: "My custom agent flow",
//	        Tools:       []string{"file_system", "shell_command"},
//	        SystemPrompt: "You are a helpful assistant.",
//	        InputExample: "Analyze this log file",
//	    })
//
//	    // Start the DevUI — blocks until interrupted
//	    if err := devui.Start(context.Background()); err != nil {
//	        log.Fatal(err)
//	    }
//	}
package devui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	agentfw "github.com/PipeOpsHQ/agent-sdk-go/agent"
	"github.com/PipeOpsHQ/agent-sdk-go/delivery"
	devuiapi "github.com/PipeOpsHQ/agent-sdk-go/devui/api"
	authsqlite "github.com/PipeOpsHQ/agent-sdk-go/devui/auth/sqlite"
	catalogsqlite "github.com/PipeOpsHQ/agent-sdk-go/devui/catalog/sqlite"
	"github.com/PipeOpsHQ/agent-sdk-go/flow"
	"github.com/PipeOpsHQ/agent-sdk-go/graph"
	_ "github.com/PipeOpsHQ/agent-sdk-go/graphs/basic"     // registers "basic" workflow
	_ "github.com/PipeOpsHQ/agent-sdk-go/graphs/chain"     // registers "chain" workflow
	_ "github.com/PipeOpsHQ/agent-sdk-go/graphs/mapreduce" // registers "map-reduce" workflow
	_ "github.com/PipeOpsHQ/agent-sdk-go/graphs/router"    // registers "router" workflow
	"github.com/PipeOpsHQ/agent-sdk-go/guardrail"
	"github.com/PipeOpsHQ/agent-sdk-go/observe"
	observesqlite "github.com/PipeOpsHQ/agent-sdk-go/observe/store/sqlite"
	providerfactory "github.com/PipeOpsHQ/agent-sdk-go/providers/factory"
	cronpkg "github.com/PipeOpsHQ/agent-sdk-go/runtime/cron"
	"github.com/PipeOpsHQ/agent-sdk-go/runtime/distributed"
	"github.com/PipeOpsHQ/agent-sdk-go/runtime/queue"
	"github.com/PipeOpsHQ/agent-sdk-go/runtime/queue/redisstreams"
	"github.com/PipeOpsHQ/agent-sdk-go/skill"
	"github.com/PipeOpsHQ/agent-sdk-go/state"
	statefactory "github.com/PipeOpsHQ/agent-sdk-go/state/factory"
	"github.com/PipeOpsHQ/agent-sdk-go/tools"
	"github.com/PipeOpsHQ/agent-sdk-go/workflow"
)

// Options configures the DevUI server.
type Options struct {
	// Addr is the listen address (default: "127.0.0.1:7070").
	// Can also be set via AGENT_UI_ADDR env var.
	Addr string

	// DBPath is the SQLite database path for traces, catalog, and auth.
	// Default: "./.ai-agent/devui.db". Env: AGENT_DEVUI_DB_PATH.
	DBPath string

	// RegisterBuiltinFlows controls whether built-in example flows
	// (code-reviewer, devops-assistant, etc.) are registered automatically.
	// Default: true. Set SkipBuiltinFlows to true to disable.
	SkipBuiltinFlows bool

	// Open automatically opens the browser when the server starts.
	// Default: false. Env: AGENT_UI_OPEN.
	Open bool

	// RequireAPIKey forces API key authentication.
	// Default: false. Env: AGENT_UI_REQUIRE_API_KEY.
	RequireAPIKey bool

	// AllowLocalNoAuth allows unauthenticated access from localhost.
	// Default: true. Env: AGENT_UI_ALLOW_LOCAL_NOAUTH.
	AllowLocalNoAuth bool

	// DefaultFlow is the flow name to auto-select in the Playground UI.
	// When set, the UI opens with this flow pre-selected instead of "(none)".
	DefaultFlow string

	// ToolSpecDir is where runtime custom tool specs are persisted.
	// Default: "./.ai-agent/tools". Env: AGENT_UI_TOOL_DIR.
	ToolSpecDir string
}

// Start launches the DevUI server with sensible defaults. It blocks until
// the context is cancelled or an interrupt signal is received.
//
// Users should register their flows (via [flow.Register]) and custom tools
// (via [tools.RegisterTool]) before calling Start.
func Start(ctx context.Context, opts ...Options) error {
	o := mergeOptions(opts)

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	if !o.SkipBuiltinFlows {
		flow.RegisterBuiltins()
	}

	// Load skills: built-ins + local directories
	skill.RegisterBuiltins()
	if n := skill.ScanDefaults(); n > 0 {
		log.Printf("📚 Loaded %d skill(s) from local directories", n)
	}
	if n := loadCustomToolSpecs(o.ToolSpecDir); n > 0 {
		log.Printf("🧰 Loaded %d custom runtime tool(s)", n)
	}

	// State store
	store, err := statefactory.FromEnv(ctx)
	if err != nil {
		log.Printf("state store unavailable: %v", err)
	}
	if store != nil {
		defer func() { _ = store.Close() }()
	}

	// Trace store
	traceStore, err := observesqlite.New(o.DBPath)
	if err != nil {
		log.Printf("trace store unavailable: %v", err)
	}
	if traceStore != nil {
		defer func() { _ = traceStore.Close() }()
	}

	// Catalog store
	catalogStore, err := catalogsqlite.New(o.DBPath)
	if err != nil {
		log.Printf("catalog store unavailable: %v", err)
	}
	if catalogStore != nil {
		defer func() { _ = catalogStore.Close() }()
	}

	// Auth store
	authStore, err := authsqlite.New(o.DBPath)
	if err != nil {
		if o.RequireAPIKey {
			return fmt.Errorf("auth store setup failed (required for API key mode): %w", err)
		}
		log.Printf("auth store unavailable (local no-auth mode): %v", err)
	}
	if authStore != nil {
		defer func() { _ = authStore.Close() }()
	}

	// Audit store
	auditStore, err := devuiapi.NewSQLiteAuditStore(o.DBPath)
	if err != nil {
		log.Printf("audit store unavailable: %v", err)
	}
	if closer, ok := auditStore.(interface{ Close() error }); ok {
		defer func() { _ = closer.Close() }()
	}

	// Observer — saves to DB and forwards to SSE stream for live UI updates.
	// The SSE forwarder is late-bound because the server is created after the observer.
	var sseForwarder sseForwardSink
	observer := observe.Sink(observe.NoopSink{})
	if traceStore != nil {
		dbSink := observe.SinkFunc(func(ctx context.Context, event observe.Event) error {
			return traceStore.SaveEvent(ctx, event)
		})
		combined := observe.NewMultiSink(dbSink, &sseForwarder)
		async := observe.NewAsyncSink(combined, 256)
		observer = async
		defer async.Close()
	}

	// Playground runner
	playground := &playgroundRunner{store: store, observer: observer}

	// Runtime (optional — requires Redis)
	rtComponents, closeRuntime := buildRuntime(ctx, store, o)
	defer closeRuntime()

	var runtimeService devuiapi.RuntimeService
	if rtComponents != nil {
		runtimeService = rtComponents.service

		// Inline worker
		processor := func(ctx context.Context, task queue.Task) (distributed.ProcessResult, error) {
			resp, err := playground.Run(ctx, devuiapi.PlaygroundRequest{
				Input:        task.Input,
				Workflow:     task.Workflow,
				Tools:        task.Tools,
				SystemPrompt: task.SystemPrompt,
			})
			if err != nil {
				return distributed.ProcessResult{}, err
			}
			return distributed.ProcessResult{Output: resp.Output, Provider: resp.Provider}, nil
		}

		inlineWorker, wErr := distributed.NewWorker(
			distributed.WorkerConfig{WorkerID: "devui-inline", Capacity: 2},
			store,
			rtComponents.attemptStore,
			rtComponents.queue,
			observer,
			distributed.DefaultRuntimePolicy(),
			processor,
		)
		if wErr != nil {
			log.Printf("inline worker unavailable: %v", wErr)
		} else {
			go func() {
				if err := inlineWorker.Start(ctx); err != nil && !errors.Is(err, context.Canceled) {
					log.Printf("inline worker stopped: %v", err)
				}
			}()
			defer func() {
				log.Println("  stopping inline worker...")
				_ = inlineWorker.Stop(context.Background())
			}()
			log.Println("inline worker started (capacity=2)")
		}
	}

	// Cron scheduler
	scheduler := cronpkg.New(func(cfg cronpkg.JobConfig) (string, error) {
		resp, err := playground.Run(ctx, devuiapi.PlaygroundRequest{
			Input:        cfg.Input,
			Workflow:     cfg.Workflow,
			Tools:        cfg.Tools,
			SystemPrompt: cfg.SystemPrompt,
			ReplyTo:      cfg.ReplyTo,
		})
		if err != nil {
			return "", err
		}
		return resp.Output, nil
	})
	scheduler.Start()
	defer func() {
		log.Println("  stopping cron scheduler...")
		scheduler.Stop()
	}()

	// Register cron_manager tool
	_ = tools.UpsertTool("cron_manager",
		"Manage cron-scheduled agent jobs: list, add, remove, trigger, enable, disable recurring tasks.",
		func() tools.Tool { return tools.NewCronManager(scheduler) },
	)

	// Register self_api tool — lets the agent call its own API
	selfAPIBase := "http://" + o.Addr
	_ = tools.UpsertTool("self_api",
		"Call the agent's own DevUI API to manage cron jobs, skills, flows, runs, tools, workflows, runtime, and more.",
		func() tools.Tool { return tools.NewSelfAPI(selfAPIBase) },
	)

	// Start HTTP server
	server := devuiapi.NewServer(devuiapi.Config{
		Addr:             o.Addr,
		StateStore:       store,
		TraceStore:       traceStore,
		CatalogStore:     catalogStore,
		AuthStore:        authStore,
		AuditStore:       auditStore,
		Runtime:          runtimeService,
		Playground:       playground,
		Scheduler:        scheduler,
		RequireAPIKey:    o.RequireAPIKey,
		AllowLocalNoAuth: o.AllowLocalNoAuth,
		ToolSpecDir:      o.ToolSpecDir,
		DefaultFlow:      o.DefaultFlow,
	})

	log.Printf("DevUI listening on http://%s", o.Addr)
	// Bind the SSE forwarder now that the server is ready.
	sseForwarder.bind(server)
	if o.Open {
		openBrowser("http://" + o.Addr)
	}
	if err := server.ListenAndServe(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("devui server failed: %w", err)
	}
	log.Println("🧹 Cleaning up resources...")
	return nil
}

// ── internals ──

func mergeOptions(opts []Options) Options {
	var o Options
	if len(opts) > 0 {
		o = opts[0]
	}

	// Parse CLI flags from os.Args (so users don't need to handle flags themselves).
	for _, arg := range os.Args[1:] {
		switch {
		case strings.HasPrefix(arg, "--ui-addr="):
			o.Addr = strings.TrimSpace(strings.TrimPrefix(arg, "--ui-addr="))
		case strings.HasPrefix(arg, "--ui-db-path="):
			o.DBPath = strings.TrimSpace(strings.TrimPrefix(arg, "--ui-db-path="))
		case strings.HasPrefix(arg, "--ui-open="):
			o.Open = parseBoolStr(strings.TrimPrefix(arg, "--ui-open="), o.Open)
		case arg == "--ui-open":
			o.Open = true
		case strings.HasPrefix(arg, "--ui-require-api-key="):
			o.RequireAPIKey = parseBoolStr(strings.TrimPrefix(arg, "--ui-require-api-key="), o.RequireAPIKey)
		case strings.HasPrefix(arg, "--ui-skip-builtins="):
			o.SkipBuiltinFlows = parseBoolStr(strings.TrimPrefix(arg, "--ui-skip-builtins="), o.SkipBuiltinFlows)
		case arg == "--ui-skip-builtins":
			o.SkipBuiltinFlows = true
		}
	}

	// Apply env vars as defaults (CLI flags and user-provided values take priority)
	if o.Addr == "" {
		o.Addr = strings.TrimSpace(os.Getenv("AGENT_UI_ADDR"))
	}
	if o.Addr == "" {
		o.Addr = "127.0.0.1:7070"
	}
	if o.DBPath == "" {
		o.DBPath = strings.TrimSpace(os.Getenv("AGENT_DEVUI_DB_PATH"))
	}
	if o.DBPath == "" {
		o.DBPath = "./.ai-agent/devui.db"
	}
	if strings.TrimSpace(o.ToolSpecDir) == "" {
		o.ToolSpecDir = strings.TrimSpace(os.Getenv("AGENT_UI_TOOL_DIR"))
	}
	if strings.TrimSpace(o.ToolSpecDir) == "" {
		o.ToolSpecDir = "./.ai-agent/tools"
	}
	if !o.Open {
		o.Open = parseBoolEnv("AGENT_UI_OPEN", false)
	}
	if !o.RequireAPIKey {
		o.RequireAPIKey = parseBoolEnv("AGENT_UI_REQUIRE_API_KEY", false)
	}
	// AllowLocalNoAuth defaults to true
	if !o.RequireAPIKey && !o.AllowLocalNoAuth {
		o.AllowLocalNoAuth = parseBoolEnv("AGENT_UI_ALLOW_LOCAL_NOAUTH", true)
	}

	return o
}

func loadCustomToolSpecs(dir string) int {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return 0
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("custom tool specs unavailable: %v", err)
		}
		return 0
	}
	loaded := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(entry.Name()))
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			log.Printf("custom tool spec %s skipped: %v", path, readErr)
			continue
		}
		var spec tools.CustomHTTPSpec
		if unmarshalErr := json.Unmarshal(raw, &spec); unmarshalErr != nil {
			log.Printf("custom tool spec %s skipped: %v", path, unmarshalErr)
			continue
		}
		if regErr := tools.UpsertCustomHTTPTool(spec); regErr != nil {
			log.Printf("custom tool spec %s registration failed: %v", path, regErr)
			continue
		}
		loaded++
	}
	return loaded
}

// playgroundRunner executes agent flows for the playground.
type playgroundRunner struct {
	store    state.Store
	observer observe.Sink
}

func (r *playgroundRunner) Run(ctx context.Context, req devuiapi.PlaygroundRequest) (devuiapi.PlaygroundResponse, error) {
	provider, err := providerfactory.FromEnv(ctx)
	if err != nil {
		return devuiapi.PlaygroundResponse{}, fmt.Errorf("provider setup failed: %w", err)
	}

	// Resolve flow defaults — request fields override flow defaults.
	var flowSkills []string
	if name := strings.TrimSpace(req.Flow); name != "" {
		if f, ok := flow.Get(name); ok {
			if strings.TrimSpace(req.Workflow) == "" {
				req.Workflow = f.Workflow
			}
			if len(req.Tools) == 0 {
				req.Tools = f.Tools
			}
			if strings.TrimSpace(req.SystemPrompt) == "" {
				req.SystemPrompt = f.SystemPrompt
			}
			flowSkills = f.Skills
		}
	}

	wfName := strings.TrimSpace(req.Workflow)
	systemPrompt := strings.TrimSpace(req.SystemPrompt)
	if systemPrompt == "" {
		systemPrompt = "You are a practical AI assistant. Be concise, accurate, and actionable."
	}

	// Resolve skills — merge flow skills + request skills, then inject instructions + tools
	allSkills := make(map[string]bool)
	for _, s := range flowSkills {
		s = strings.TrimSpace(s)
		if s != "" {
			allSkills[s] = true
		}
	}
	for _, s := range req.Skills {
		s = strings.TrimSpace(s)
		if s != "" {
			allSkills[s] = true
		}
	}
	appliedSkills := sortedSkillNames(allSkills)
	for skillName := range allSkills {
		if s, ok := skill.Get(skillName); ok {
			if s.Instructions != "" {
				systemPrompt += "\n\n## Skill: " + s.Name + "\n" + s.Instructions
			}
			if len(s.AllowedTools) > 0 {
				req.Tools = append(req.Tools, s.AllowedTools...)
			}
		}
	}
	req.ReplyTo = delivery.Normalize(req.ReplyTo)
	if req.ReplyTo != nil {
		systemPrompt = strings.TrimSpace(systemPrompt + "\n\n" + buildReplyChannelHint(req.ReplyTo))
	}
	runCtx := delivery.WithTarget(ctx, req.ReplyTo)
	runCtx = delivery.WithTurnType(runCtx, "user")

	// Build agent
	agentOpts := []agentfw.Option{
		agentfw.WithSystemPrompt(systemPrompt),
		agentfw.WithMaxIterations(25),
	}

	// Wire guardrails if requested
	if len(req.Guardrails) > 0 {
		pipeline := guardrail.NewPipeline()
		for _, name := range req.Guardrails {
			switch name {
			case "max_length":
				pipeline.Add(&guardrail.MaxLength{Limit: 10000})
			case "prompt_injection":
				pipeline.AddInput(&guardrail.PromptInjection{})
			case "content_filter":
				pipeline.Add(&guardrail.ContentFilter{})
			case "pii_filter":
				pipeline.Add(&guardrail.PIIFilter{})
			case "topic_filter":
				pipeline.Add(&guardrail.TopicFilter{})
			case "secret_guard":
				pipeline.Add(&guardrail.SecretGuard{})
			}
		}
		agentOpts = append(agentOpts, agentfw.WithMiddleware(guardrail.NewAgentMiddleware(pipeline)))
	}

	// Session continuity — reuse session ID and load previous conversation.
	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID != "" {
		agentOpts = append(agentOpts, agentfw.WithSessionID(sessionID))
		// Load previous messages from the most recent completed run in this session.
		if r.store != nil {
			prevRuns, _ := r.store.ListRuns(ctx, state.ListRunsQuery{
				SessionID: sessionID,
				Status:    "completed",
				Limit:     1,
			})
			if len(prevRuns) > 0 && len(prevRuns[0].Messages) > 0 {
				agentOpts = append(agentOpts, agentfw.WithConversationHistory(prevRuns[0].Messages))
			}
		}
	}

	if r.store != nil {
		agentOpts = append(agentOpts, agentfw.WithStore(r.store))
	}
	if r.observer != nil {
		agentOpts = append(agentOpts, agentfw.WithObserver(r.observer))
	}

	// Attach tools
	if len(req.Tools) > 0 {
		selected, err := tools.BuildSelection(req.Tools)
		if err != nil {
			return devuiapi.PlaygroundResponse{}, fmt.Errorf("tool selection failed: %w", err)
		}
		for _, t := range selected {
			agentOpts = append(agentOpts, agentfw.WithTool(t))
		}
	}

	agent, err := agentfw.New(provider, agentOpts...)
	if err != nil {
		return devuiapi.PlaygroundResponse{}, fmt.Errorf("agent create failed: %w", err)
	}

	// Direct run (no workflow graph)
	if wfName == "" {
		result, runErr := agent.RunDetailed(runCtx, req.Input)
		if runErr != nil {
			return devuiapi.PlaygroundResponse{}, runErr
		}
		return devuiapi.PlaygroundResponse{
			Status:        "completed",
			Output:        result.Output,
			RunID:         result.RunID,
			SessionID:     result.SessionID,
			Provider:      provider.Name(),
			AppliedSkills: appliedSkills,
			ReplyTo:       req.ReplyTo,
		}, nil
	}

	// Workflow graph run
	exec, err := buildExecutor(agent, r.store, r.observer, wfName)
	if err != nil {
		return devuiapi.PlaygroundResponse{}, fmt.Errorf("executor create failed: %w", err)
	}
	result, runErr := exec.Run(runCtx, req.Input)
	if runErr != nil {
		return devuiapi.PlaygroundResponse{}, runErr
	}
	return devuiapi.PlaygroundResponse{
		Status:        "completed",
		Output:        result.Output,
		RunID:         result.RunID,
		SessionID:     result.SessionID,
		Provider:      provider.Name(),
		AppliedSkills: appliedSkills,
		ReplyTo:       req.ReplyTo,
	}, nil
}

func buildReplyChannelHint(target *delivery.Target) string {
	if target == nil {
		return ""
	}
	parts := []string{}
	if v := strings.TrimSpace(target.Channel); v != "" {
		parts = append(parts, "channel="+v)
	}
	if v := strings.TrimSpace(target.Destination); v != "" {
		parts = append(parts, "destination="+v)
	}
	if v := strings.TrimSpace(target.ThreadID); v != "" {
		parts = append(parts, "threadId="+v)
	}
	if v := strings.TrimSpace(target.UserID); v != "" {
		parts = append(parts, "userId="+v)
	}
	if len(parts) == 0 {
		return ""
	}
	return "Current reply channel context: " + strings.Join(parts, ", ") + ". If asked to schedule reminders for this same channel, set cron job config.replyTo to this channel context unless the user explicitly asks for another destination."
}

func sortedSkillNames(skills map[string]bool) []string {
	if len(skills) == 0 {
		return nil
	}
	out := make([]string, 0, len(skills))
	for name := range skills {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func buildExecutor(agent *agentfw.Agent, store state.Store, observer observe.Sink, wfName string) (*graph.Executor, error) {
	if alias, ok := workflowAlias(wfName); ok {
		wfName = alias
	}
	builder, ok := workflow.Get(wfName)
	if !ok {
		return nil, fmt.Errorf("unknown workflow %q (available: %s)", wfName, strings.Join(workflow.Names(), ", "))
	}
	exec, err := builder.NewExecutor(agent, store, "")
	if err != nil {
		return nil, err
	}
	exec.SetObserver(observer)
	return exec, nil
}

func workflowAlias(name string) (string, bool) {
	normalized := strings.TrimSpace(strings.ToLower(name))
	switch normalized {
	case "web-scrape-and-summarize", "web_scrape_and_summarize", "webscrapeandsummarize":
		return "map-reduce", true
	default:
		return "", false
	}
}

type runtimeComponents struct {
	service      devuiapi.RuntimeService
	attemptStore distributed.AttemptStore
	queue        *redisstreams.Queue
}

func buildRuntime(ctx context.Context, store state.Store, o Options) (*runtimeComponents, func()) {
	if store == nil {
		return nil, func() {}
	}

	attemptsPath := filepath.Join(filepath.Dir(o.DBPath), "runtime.db")
	redisAddr := strings.TrimSpace(os.Getenv("AGENT_REDIS_ADDR"))
	if redisAddr == "" {
		redisAddr = "127.0.0.1:6379"
	}
	redisPassword := strings.TrimSpace(os.Getenv("AGENT_REDIS_PASSWORD"))
	redisDB := parseIntEnv("AGENT_REDIS_DB", 0)
	queuePrefix := strings.TrimSpace(os.Getenv("AGENT_RUNTIME_QUEUE_PREFIX"))
	if queuePrefix == "" {
		queuePrefix = "aiag:queue"
	}
	queueGroup := strings.TrimSpace(os.Getenv("AGENT_RUNTIME_QUEUE_GROUP"))
	if queueGroup == "" {
		queueGroup = "workers"
	}

	attemptStore, err := distributed.NewSQLiteAttemptStore(attemptsPath)
	if err != nil {
		log.Printf("runtime attempt store unavailable: %v", err)
		return nil, func() {}
	}

	queueStore, err := redisstreams.New(
		redisAddr,
		redisstreams.WithPassword(redisPassword),
		redisstreams.WithDB(redisDB),
		redisstreams.WithPrefix(queuePrefix),
		redisstreams.WithGroup(queueGroup),
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
				Prefix: queuePrefix,
			},
		},
	)
	if err != nil {
		_ = queueStore.Close()
		_ = attemptStore.Close()
		log.Printf("runtime service unavailable: %v", err)
		return nil, func() {}
	}

	if _, claimErr := queueStore.Claim(ctx, "devui-bootstrap", time.Millisecond, 1); claimErr != nil {
		log.Printf("runtime bootstrap claim warning: %v", claimErr)
	}

	return &runtimeComponents{
			service:      service,
			attemptStore: attemptStore,
			queue:        queueStore,
		}, func() {
			_ = queueStore.Close()
			_ = attemptStore.Close()
		}
}

func parseBoolEnv(key string, fallback bool) bool {
	return parseBoolStr(os.Getenv(key), fallback)
}

func parseBoolStr(raw string, fallback bool) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

func parseIntEnv(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return v
}

func openBrowser(target string) {
	if strings.TrimSpace(target) == "" {
		return
	}
	// Best-effort — works on macOS, Linux, Windows
	for _, cmd := range []string{"open", "xdg-open", "rundll32"} {
		if p, err := os.StartProcess(cmd, []string{cmd, target}, &os.ProcAttr{}); err == nil {
			_ = p.Release()
			return
		}
	}
}

// sseForwardSink is a late-bound sink that forwards observe events to the
// DevUI SSE stream. It is safe to call Emit before bind() — events are
// silently dropped until the server is ready.
type sseForwardSink struct {
	mu     sync.RWMutex
	server *devuiapi.Server
}

func (s *sseForwardSink) bind(srv *devuiapi.Server) {
	s.mu.Lock()
	s.server = srv
	s.mu.Unlock()
}

func (s *sseForwardSink) Emit(_ context.Context, event observe.Event) error {
	s.mu.RLock()
	srv := s.server
	s.mu.RUnlock()
	if srv != nil {
		srv.Emit(event)
	}
	return nil
}
