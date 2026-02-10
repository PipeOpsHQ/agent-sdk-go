package cli

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	osExec "os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	devuiapi "github.com/PipeOpsHQ/agent-sdk-go/framework/devui/api"
	devuiauth "github.com/PipeOpsHQ/agent-sdk-go/framework/devui/auth"
	authsqlite "github.com/PipeOpsHQ/agent-sdk-go/framework/devui/auth/sqlite"
	catalogsqlite "github.com/PipeOpsHQ/agent-sdk-go/framework/devui/catalog/sqlite"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/flow"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/internal/config"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/observe"
	observesqlite "github.com/PipeOpsHQ/agent-sdk-go/framework/observe/store/sqlite"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/prompt"
	cronpkg "github.com/PipeOpsHQ/agent-sdk-go/framework/runtime/cron"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/runtime/distributed"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/runtime/queue"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/skill"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/state"
	statefactory "github.com/PipeOpsHQ/agent-sdk-go/framework/state/factory"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/tools"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/workflow"
)

type uiOptions struct {
	addr             string
	dbPath           string
	attemptsPath     string
	workflowDir      string
	providerEnvFile  string
	promptDir        string
	redisAddr        string
	redisPassword    string
	redisDB          int
	queuePrefix      string
	queueGroup       string
	requireAPIKey    bool
	allowLocalNoAuth bool
	open             bool
}

func runUI(ctx context.Context, args []string, remoteMode bool) {
	opts := parseUIArgs(args, remoteMode)
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	flow.RegisterBuiltins()
	skill.RegisterBuiltins()
	skill.ScanDefaults()
	loadWorkflowSpecs(opts.workflowDir)
	if err := devuiapi.LoadProviderEnvFile(opts.providerEnvFile); err != nil {
		log.Printf("provider env file unavailable: %v", err)
	}
	if _, err := prompt.LoadDir(opts.promptDir); err != nil {
		log.Printf("prompt specs unavailable: %v", err)
	}

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

	_ = tools.UpsertTool(
		"cron_manager",
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
		WorkflowSpecDir:  opts.workflowDir,
		ProviderEnvFile:  opts.providerEnvFile,
		PromptSpecDir:    opts.promptDir,
	})

	log.Printf("DevUI listening on http://%s", opts.addr)
	if opts.open {
		openBrowser("http://" + opts.addr)
	}
	if err := server.ListenAndServe(ctx); err != nil && !errors.Is(err, context.Canceled) {
		log.Fatalf("devui server failed: %v", err)
	}
	log.Println("Cleaning up resources...")
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
	workflowDir := strings.TrimSpace(os.Getenv("AGENT_UI_WORKFLOW_DIR"))
	if workflowDir == "" {
		workflowDir = "./.ai-agent/workflows"
	}
	providerEnvFile := strings.TrimSpace(os.Getenv("AGENT_UI_PROVIDER_ENV_FILE"))
	if providerEnvFile == "" {
		providerEnvFile = "./.ai-agent/provider_env.json"
	}
	promptDir := strings.TrimSpace(os.Getenv("AGENT_UI_PROMPT_DIR"))
	if promptDir == "" {
		promptDir = "./.ai-agent/prompts"
	}
	attemptsPath := strings.TrimSpace(os.Getenv("AGENT_RUNTIME_ATTEMPTS_DB_PATH"))
	if attemptsPath == "" {
		attemptsPath = filepath.Join(filepath.Dir(dbPath), "runtime.db")
	}
	opts := uiOptions{
		addr:             strings.TrimSpace(os.Getenv("AGENT_UI_ADDR")),
		dbPath:           dbPath,
		attemptsPath:     attemptsPath,
		workflowDir:      workflowDir,
		providerEnvFile:  providerEnvFile,
		promptDir:        promptDir,
		redisAddr:        strings.TrimSpace(os.Getenv("AGENT_REDIS_ADDR")),
		redisPassword:    strings.TrimSpace(os.Getenv("AGENT_REDIS_PASSWORD")),
		redisDB:          config.ParseIntEnv("AGENT_REDIS_DB", 0),
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
		case strings.HasPrefix(arg, "--ui-workflow-dir="):
			opts.workflowDir = strings.TrimSpace(strings.TrimPrefix(arg, "--ui-workflow-dir="))
		case strings.HasPrefix(arg, "--ui-provider-env-file="):
			opts.providerEnvFile = strings.TrimSpace(strings.TrimPrefix(arg, "--ui-provider-env-file="))
		case strings.HasPrefix(arg, "--ui-prompt-dir="):
			opts.promptDir = strings.TrimSpace(strings.TrimPrefix(arg, "--ui-prompt-dir="))
		case strings.HasPrefix(arg, "--ui-open="):
			opts.open = config.ParseBoolString(strings.TrimSpace(strings.TrimPrefix(arg, "--ui-open=")), opts.open)
		case strings.HasPrefix(arg, "--ui-allow-local-noauth="):
			opts.allowLocalNoAuth = config.ParseBoolString(strings.TrimSpace(strings.TrimPrefix(arg, "--ui-allow-local-noauth=")), opts.allowLocalNoAuth)
		case strings.HasPrefix(arg, "--ui-require-api-key="):
			opts.requireAPIKey = config.ParseBoolString(strings.TrimSpace(strings.TrimPrefix(arg, "--ui-require-api-key=")), opts.requireAPIKey)
		}
	}
	return opts
}

func loadWorkflowSpecs(dir string) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		log.Printf("workflow specs unavailable: %v", err)
		return
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(entry.Name()))
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		builder, buildErr := workflow.NewFileBuilderFromPath(path)
		if buildErr != nil {
			log.Printf("workflow spec %s skipped: %v", path, buildErr)
			continue
		}
		if regErr := workflow.Register(builder); regErr != nil {
			if strings.Contains(regErr.Error(), "already registered") {
				continue
			}
			log.Printf("workflow spec %s registration failed: %v", path, regErr)
		}
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
