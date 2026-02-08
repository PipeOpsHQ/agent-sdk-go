package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
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
	"github.com/PipeOpsHQ/agent-sdk-go/framework/flow"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/graph"
	basicgraph "github.com/PipeOpsHQ/agent-sdk-go/framework/graphs/basic"
	_ "github.com/PipeOpsHQ/agent-sdk-go/framework/graphs/chain"     // registers "chain" workflow
	_ "github.com/PipeOpsHQ/agent-sdk-go/framework/graphs/mapreduce" // registers "map-reduce" workflow
	_ "github.com/PipeOpsHQ/agent-sdk-go/framework/graphs/router"    // registers "router" workflow
	"github.com/PipeOpsHQ/agent-sdk-go/framework/llm"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/observe"
	observesqlite "github.com/PipeOpsHQ/agent-sdk-go/framework/observe/store/sqlite"
	providerfactory "github.com/PipeOpsHQ/agent-sdk-go/framework/providers/factory"
	cronpkg "github.com/PipeOpsHQ/agent-sdk-go/framework/runtime/cron"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/runtime/distributed"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/runtime/queue"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/runtime/queue/redisstreams"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/skill"
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
	case "cron":
		runCronCLI(ctx, os.Args[2:])
	case "skill":
		runSkillCLI(os.Args[2:])
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

	// Resolve skills — merge flow skills + request skills
	allSkills := make(map[string]bool)
	for _, s := range flowSkills {
		allSkills[s] = true
	}
	for _, s := range req.Skills {
		allSkills[s] = true
	}
	systemPrompt := strings.TrimSpace(req.SystemPrompt)
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
	if systemPrompt != "" {
		req.SystemPrompt = systemPrompt
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

	// Register built-in flows so the Playground has example flows to choose from.
	flow.RegisterBuiltins()

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

	rtComponents, closeRuntime := buildRuntimeService(ctx, store, opts)
	defer closeRuntime()

	var runtimeService devuiapi.RuntimeService
	if rtComponents != nil {
		runtimeService = rtComponents.service
	}

	observer := observe.Sink(observe.NoopSink{})
	if traceStore != nil {
		async := observe.NewAsyncSink(observe.SinkFunc(func(ctx context.Context, event observe.Event) error {
			return traceStore.SaveEvent(ctx, event)
		}), 256)
		observer = async
		defer async.Close()
	}

	playground := &localPlaygroundRunner{store: store, observer: observer}

	// Start an inline worker that processes queued runs using the playground runner.
	if rtComponents != nil {
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

	// Create cron scheduler that delegates to the playground runner.
	scheduler := cronpkg.New(func(cfg cronpkg.JobConfig) (string, error) {
		resp, err := playground.Run(ctx, devuiapi.PlaygroundRequest{
			Input:        cfg.Input,
			Workflow:     cfg.Workflow,
			Tools:        cfg.Tools,
			SystemPrompt: cfg.SystemPrompt,
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

	// Register cron_manager as a tool so agents can manage scheduled jobs.
	_ = tools.RegisterTool("cron_manager",
		"Manage cron-scheduled agent jobs: list, add, remove, trigger, enable, disable recurring tasks.",
		func() tools.Tool { return tools.NewCronManager(scheduler) },
	)

	server := devuiapi.NewServer(devuiapi.Config{
		Addr:             opts.addr,
		StateStore:       store,
		TraceStore:       traceStore,
		CatalogStore:     catalogStore,
		AuthStore:        authStore,
		AuditStore:       auditStore,
		Runtime:          runtimeService,
		Playground:       playground,
		Scheduler:        scheduler,
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
	log.Println("🧹 Cleaning up resources...")
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

// ── Cron CLI ──

func runCronCLI(_ context.Context, args []string) {
	if len(args) == 0 {
		printCronUsage()
		os.Exit(1)
	}
	addr := "127.0.0.1:7070"
	apiKey := ""

	// Parse global flags
	var filtered []string
	for _, arg := range args {
		switch {
		case strings.HasPrefix(arg, "--addr="):
			addr = strings.TrimPrefix(arg, "--addr=")
		case strings.HasPrefix(arg, "--api-key="):
			apiKey = strings.TrimPrefix(arg, "--api-key=")
		default:
			filtered = append(filtered, arg)
		}
	}
	if len(filtered) == 0 {
		printCronUsage()
		os.Exit(1)
	}

	base := "http://" + addr
	client := &cronCLIClient{base: base, apiKey: apiKey}

	switch filtered[0] {
	case "list", "ls":
		client.list()
	case "add":
		client.add(filtered[1:])
	case "remove", "rm":
		client.remove(filtered[1:])
	case "trigger":
		client.trigger(filtered[1:])
	case "enable":
		client.setEnabled(filtered[1:], true)
	case "disable":
		client.setEnabled(filtered[1:], false)
	case "get":
		client.get(filtered[1:])
	default:
		log.Fatalf("unknown cron command %q", filtered[0])
	}
}

type cronCLIClient struct {
	base   string
	apiKey string
}

func (c *cronCLIClient) doRequest(method, path string, body any) ([]byte, error) {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reqBody = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.base+path, reqBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("X-API-Key", c.apiKey)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed (is the UI server running at %s?): %w", c.base, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("server returned %d: %s", resp.StatusCode, string(data))
	}
	return data, nil
}

func (c *cronCLIClient) list() {
	data, err := c.doRequest(http.MethodGet, "/api/v1/cron/jobs", nil)
	if err != nil {
		log.Fatal(err)
	}
	var jobs []cronpkg.Job
	if err := json.Unmarshal(data, &jobs); err != nil {
		log.Fatalf("parse response: %v", err)
	}
	if len(jobs) == 0 {
		fmt.Println("No scheduled jobs.")
		return
	}
	fmt.Printf("%-20s %-15s %-8s %-5s %s\n", "NAME", "SCHEDULE", "ENABLED", "RUNS", "LAST RUN")
	for _, j := range jobs {
		enabled := "yes"
		if !j.Enabled {
			enabled = "no"
		}
		lastRun := "never"
		if !j.LastRun.IsZero() {
			lastRun = j.LastRun.Format("2006-01-02 15:04:05")
		}
		fmt.Printf("%-20s %-15s %-8s %-5d %s\n", j.Name, j.CronExpr, enabled, j.RunCount, lastRun)
	}
}

func (c *cronCLIClient) add(args []string) {
	if len(args) < 3 {
		log.Fatal("usage: cron add <name> <cron-expr> <input> [--workflow=basic] [--tools=@default] [--system-prompt=TEXT]")
	}
	name := args[0]
	cronExpr := args[1]
	input := args[2]
	wf := "basic"
	var cronTools []string
	systemPrompt := ""
	for _, arg := range args[3:] {
		switch {
		case strings.HasPrefix(arg, "--workflow="):
			wf = strings.TrimPrefix(arg, "--workflow=")
		case strings.HasPrefix(arg, "--tools="):
			cronTools = strings.Split(strings.TrimPrefix(arg, "--tools="), ",")
		case strings.HasPrefix(arg, "--system-prompt="):
			systemPrompt = strings.TrimPrefix(arg, "--system-prompt=")
		}
	}
	body := map[string]any{
		"name":     name,
		"cronExpr": cronExpr,
		"config": map[string]any{
			"input":        input,
			"workflow":     wf,
			"tools":        cronTools,
			"systemPrompt": systemPrompt,
		},
	}
	_, err := c.doRequest(http.MethodPost, "/api/v1/cron/jobs", body)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("✅ Job %q scheduled (%s)\n", name, cronExpr)
}

func (c *cronCLIClient) remove(args []string) {
	if len(args) < 1 {
		log.Fatal("usage: cron remove <name>")
	}
	_, err := c.doRequest(http.MethodDelete, "/api/v1/cron/jobs/"+args[0], nil)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("✅ Job %q removed\n", args[0])
}

func (c *cronCLIClient) trigger(args []string) {
	if len(args) < 1 {
		log.Fatal("usage: cron trigger <name>")
	}
	data, err := c.doRequest(http.MethodPost, "/api/v1/cron/jobs/"+args[0]+"/trigger", nil)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("✅ Job %q triggered\n", args[0])
	fmt.Println(string(data))
}

func (c *cronCLIClient) setEnabled(args []string, enabled bool) {
	if len(args) < 1 {
		action := "enable"
		if !enabled {
			action = "disable"
		}
		log.Fatalf("usage: cron %s <name>", action)
	}
	_, err := c.doRequest(http.MethodPatch, "/api/v1/cron/jobs/"+args[0], map[string]any{"enabled": enabled})
	if err != nil {
		log.Fatal(err)
	}
	state := "enabled"
	if !enabled {
		state = "disabled"
	}
	fmt.Printf("✅ Job %q %s\n", args[0], state)
}

func (c *cronCLIClient) get(args []string) {
	if len(args) < 1 {
		log.Fatal("usage: cron get <name>")
	}
	data, err := c.doRequest(http.MethodGet, "/api/v1/cron/jobs/"+args[0], nil)
	if err != nil {
		log.Fatal(err)
	}
	var job cronpkg.Job
	if err := json.Unmarshal(data, &job); err != nil {
		log.Fatalf("parse response: %v", err)
	}
	b, _ := json.MarshalIndent(job, "", "  ")
	fmt.Println(string(b))
}

func printCronUsage() {
	fmt.Println("Cron CLI — manage scheduled agent jobs (requires running UI server)")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  go run ./framework cron list                       List all scheduled jobs")
	fmt.Println("  go run ./framework cron add <name> <expr> <input>  Add a new job")
	fmt.Println("  go run ./framework cron remove <name>              Remove a job")
	fmt.Println("  go run ./framework cron trigger <name>             Run a job immediately")
	fmt.Println("  go run ./framework cron enable <name>              Enable a job")
	fmt.Println("  go run ./framework cron disable <name>             Disable a job")
	fmt.Println("  go run ./framework cron get <name>                 Get job details")
	fmt.Println()
	fmt.Println("Global flags:")
	fmt.Println("  --addr=HOST:PORT    UI server address (default: 127.0.0.1:7070)")
	fmt.Println("  --api-key=KEY       API key for authentication")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println(`  go run ./framework cron add daily-report "0 9 * * *" "Generate a daily status report"`)
	fmt.Println(`  go run ./framework cron add cleanup "0 */6 * * *" "Clean up temp files" --tools=@system`)
	fmt.Println(`  go run ./framework cron list`)
	fmt.Println(`  go run ./framework cron trigger daily-report`)
	fmt.Println(`  go run ./framework cron disable daily-report`)
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

type runtimeComponents struct {
	service      devuiapi.RuntimeService
	attemptStore distributed.AttemptStore
	queue        *redisstreams.Queue
}

func buildRuntimeService(ctx context.Context, store state.Store, opts uiOptions) (*runtimeComponents, func()) {
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

	return &runtimeComponents{
		service:      service,
		attemptStore: attemptStore,
		queue:        queueStore,
	}, func() {
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
	fmt.Println("  go run ./framework cron list|add|remove|trigger|enable|disable|get")
	fmt.Println("  go run ./framework skill list|install|remove|show|create")
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

// ===== Skill CLI =====

func runSkillCLI(args []string) {
	if len(args) == 0 {
		printSkillUsage()
		os.Exit(1)
	}

	// Load built-ins + local skills
	skill.RegisterBuiltins()
	skill.ScanDefaults()

	switch args[0] {
	case "list", "ls":
		names := skill.Names()
		if len(names) == 0 {
			fmt.Println("No skills installed.")
			return
		}
		fmt.Printf("%-25s %-10s %s\n", "NAME", "SOURCE", "DESCRIPTION")
		for _, name := range names {
			s, _ := skill.Get(name)
			desc := s.Description
			if len(desc) > 60 {
				desc = desc[:57] + "..."
			}
			fmt.Printf("%-25s %-10s %s\n", s.Name, s.Source, desc)
		}

	case "show":
		if len(args) < 2 {
			log.Fatal("usage: skill show <name>")
		}
		s, ok := skill.Get(args[1])
		if !ok {
			log.Fatalf("skill %q not found", args[1])
		}
		fmt.Printf("Name:         %s\n", s.Name)
		fmt.Printf("Description:  %s\n", s.Description)
		fmt.Printf("Source:       %s\n", s.Source)
		fmt.Printf("License:      %s\n", s.License)
		fmt.Printf("Path:         %s\n", s.Path)
		if len(s.AllowedTools) > 0 {
			fmt.Printf("AllowedTools: %s\n", strings.Join(s.AllowedTools, ", "))
		}
		for k, v := range s.Metadata {
			fmt.Printf("Metadata.%s: %s\n", k, v)
		}
		if s.Instructions != "" {
			fmt.Println("\n--- Instructions ---")
			fmt.Println(s.Instructions)
		}

	case "install":
		if len(args) < 2 {
			log.Fatal("usage: skill install <github-repo> [--dest=./skills]")
		}
		destDir := "./skills"
		repoRef := args[1]
		for _, a := range args[2:] {
			if strings.HasPrefix(a, "--dest=") {
				destDir = strings.TrimPrefix(a, "--dest=")
			}
		}
		n, err := skill.InstallFromGitHub(repoRef, destDir)
		if err != nil {
			log.Fatalf("install failed: %v", err)
		}
		fmt.Printf("Installed %d skill(s) from %s → %s\n", n, repoRef, destDir)

	case "remove", "rm":
		if len(args) < 2 {
			log.Fatal("usage: skill remove <name>")
		}
		if skill.Remove(args[1]) {
			fmt.Printf("Removed skill %q\n", args[1])
		} else {
			log.Fatalf("skill %q not found", args[1])
		}

	case "create":
		if len(args) < 3 {
			log.Fatal("usage: skill create <name> <description>")
		}
		name := args[1]
		desc := strings.Join(args[2:], " ")
		destDir := "./skills"
		skillDir := filepath.Join(destDir, name)
		if err := os.MkdirAll(skillDir, 0755); err != nil {
			log.Fatalf("failed to create directory: %v", err)
		}
		content := fmt.Sprintf("---\nname: %s\ndescription: %s\n---\n# %s\n\nAdd your instructions here.\n", name, desc, name)
		path := filepath.Join(skillDir, "SKILL.md")
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			log.Fatalf("failed to write SKILL.md: %v", err)
		}
		fmt.Printf("Created skill scaffold: %s\n", path)

	default:
		log.Fatalf("unknown skill command %q", args[0])
	}
}

func printSkillUsage() {
	fmt.Println("Usage: agent skill <command>")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  list                        List installed skills")
	fmt.Println("  show <name>                 Show skill details and instructions")
	fmt.Println("  install <repo> [--dest=DIR] Install skills from GitHub repo")
	fmt.Println("  remove <name>               Remove a skill from registry")
	fmt.Println("  create <name> <description> Create a new skill scaffold")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  agent skill list")
	fmt.Println("  agent skill show k8s-debug")
	fmt.Println("  agent skill install openai/skills")
	fmt.Println("  agent skill create my-skill \"Custom automation skill\"")
}
