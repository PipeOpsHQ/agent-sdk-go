package cli

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	agentfw "github.com/PipeOpsHQ/agent-sdk-go/framework/agent"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/graph"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/llm"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/observe"
	observesqlite "github.com/PipeOpsHQ/agent-sdk-go/framework/observe/store/sqlite"
	providerfactory "github.com/PipeOpsHQ/agent-sdk-go/framework/providers/factory"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/state"
	statefactory "github.com/PipeOpsHQ/agent-sdk-go/framework/state/factory"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/tools"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/workflow"
)

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

	customPrompt := strings.TrimSpace(opts.systemPrompt)
	if customPrompt == "" {
		customPrompt = strings.TrimSpace(os.Getenv("AGENT_SYSTEM_PROMPT"))
	}

	templateName := strings.TrimSpace(opts.promptTemplate)
	if templateName == "" {
		templateName = strings.TrimSpace(os.Getenv("AGENT_PROMPT_TEMPLATE"))
	}

	toolNames := make([]string, 0, len(toolset))
	for _, tool := range toolset {
		toolNames = append(toolNames, tool.Definition().Name)
	}

	ctx := PromptContext{ToolCount: len(toolset), ToolNames: toolNames, Provider: provider.Name()}
	prompt := BuildPrompt(customPrompt, templateName, ctx)

	if warnings := ValidatePrompt(prompt); len(warnings) > 0 {
		for _, warning := range warnings {
			log.Printf("prompt warning: %s", warning)
		}
	}

	agentOpts := []agentfw.Option{
		agentfw.WithSystemPrompt(prompt),
		agentfw.WithObserver(observer),
		agentfw.WithStore(store),
		agentfw.WithMaxIterations(maxIterationsFromEnv()),
	}
	if len(opts.conversation) > 0 {
		agentOpts = append(agentOpts, agentfw.WithConversationHistory(opts.conversation))
	}
	for _, tool := range toolset {
		agentOpts = append(agentOpts, agentfw.WithTool(tool))
	}
	if len(opts.middlewares) > 0 {
		agentOpts = append(agentOpts, agentfw.WithMiddleware(opts.middlewares...))
	}
	return agentfw.New(provider, agentOpts...)
}

func maxIterationsFromEnv() int {
	raw := strings.TrimSpace(os.Getenv("AGENT_MAX_ITERATIONS"))
	if raw == "" {
		return 12
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v < 1 {
		return 12
	}
	if v > 64 {
		return 64
	}
	return v
}

func buildExecutor(agent *agentfw.Agent, store state.Store, observer observe.Sink, opts cliOptions) (*graph.Executor, error) {
	name := strings.TrimSpace(opts.workflow)
	if name == "" {
		name = strings.TrimSpace(os.Getenv("AGENT_WORKFLOW"))
	}
	if name == "" {
		name = defaultWorkflow
	}
	if name == "default" {
		name = defaultWorkflow
	}

	builder, ok := workflow.Get(name)
	if !ok {
		return nil, fmt.Errorf("unknown workflow %q (available: %s)", name, strings.Join(workflow.Names(), ", "))
	}
	exec, err := builder.NewExecutor(agent, store, opts.sessionID)
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
