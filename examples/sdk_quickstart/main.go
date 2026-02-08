package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	agentfw "github.com/PipeOpsHQ/agent-sdk-go/framework/agent"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/graph"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/observe"
	observesqlite "github.com/PipeOpsHQ/agent-sdk-go/framework/observe/store/sqlite"
	providerfactory "github.com/PipeOpsHQ/agent-sdk-go/framework/providers/factory"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/state"
	statehybrid "github.com/PipeOpsHQ/agent-sdk-go/framework/state/hybrid"
	stateredis "github.com/PipeOpsHQ/agent-sdk-go/framework/state/redis"
	statesqlite "github.com/PipeOpsHQ/agent-sdk-go/framework/state/sqlite"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/tools"
)

func main() {
	ctx := context.Background()

	provider, err := providerfactory.FromEnv(ctx)
	if err != nil {
		log.Fatalf("provider setup failed: %v", err)
	}

	store, err := buildStore()
	if err != nil {
		log.Fatalf("state store setup failed: %v", err)
	}
	defer func() { _ = store.Close() }()

	observer, closeObserver := buildObserver()
	defer closeObserver()

	selectedTools, err := tools.BuildSelection([]string{"@default"})
	if err != nil {
		log.Fatalf("tool selection failed: %v", err)
	}

	opts := []agentfw.Option{
		agentfw.WithSystemPrompt("You are concise and practical."),
		agentfw.WithStore(store),
		agentfw.WithObserver(observer),
	}
	for _, t := range selectedTools {
		opts = append(opts, agentfw.WithTool(t))
	}

	agent, err := agentfw.New(provider, opts...)
	if err != nil {
		log.Fatalf("agent create failed: %v", err)
	}

	// 1) Single-agent run.
	runResult, err := agent.RunDetailed(ctx, "Explain zero trust in 3 bullets.")
	if err != nil {
		log.Fatalf("agent run failed: %v", err)
	}
	fmt.Printf("single-run output:\n%s\n\n", runResult.Output)
	fmt.Printf("single-run ids: run=%s session=%s\n\n", runResult.RunID, runResult.SessionID)

	// 2) Static graph run with 3 nodes.
	g := graph.New("quickstart").
		AddNode("prepare", graph.NewToolNode(func(ctx context.Context, st *graph.State) error {
			_ = ctx
			st.EnsureData()
			st.Data["prompt"] = "Summarize why layered security matters in one paragraph."
			return nil
		})).
		AddNode("agent", graph.NewAgentNode(agent, func(st *graph.State) (string, error) {
			if st == nil || st.Data == nil {
				return st.Input, nil
			}
			if prompt, ok := st.Data["prompt"].(string); ok && strings.TrimSpace(prompt) != "" {
				return prompt, nil
			}
			return st.Input, nil
		})).
		AddNode("finalize", graph.NewToolNode(func(ctx context.Context, st *graph.State) error {
			_ = ctx
			st.Output = strings.TrimSpace(st.Output)
			return nil
		})).
		AddEdge("prepare", "agent", graph.Always).
		AddEdge("agent", "finalize", graph.Always).
		SetStart("prepare")

	exec, err := graph.NewExecutor(g, graph.WithStore(store), graph.WithObserver(observer))
	if err != nil {
		log.Fatalf("graph executor create failed: %v", err)
	}

	graphResult, err := exec.Run(ctx, "fallback input")
	if err != nil {
		log.Fatalf("graph run failed: %v", err)
	}
	fmt.Printf("graph output:\n%s\n\n", graphResult.Output)
	fmt.Printf("graph ids: run=%s session=%s\n", graphResult.RunID, graphResult.SessionID)
}

func buildStore() (state.Store, error) {
	// Use durable SQLite by default, and upgrade to hybrid if Redis is available.
	sqlitePath := "./.ai-agent/state.db"
	durable, err := statesqlite.New(sqlitePath)
	if err != nil {
		return nil, err
	}

	redisAddr := strings.TrimSpace(os.Getenv("AGENT_REDIS_ADDR"))
	if redisAddr == "" {
		return durable, nil
	}

	cache, err := stateredis.New(redisAddr)
	if err != nil {
		log.Printf("redis unavailable, continuing with sqlite only: %v", err)
		return durable, nil
	}

	hybridStore, err := statehybrid.New(durable, cache)
	if err != nil {
		_ = cache.Close()
		_ = durable.Close()
		return nil, err
	}
	return hybridStore, nil
}

func buildObserver() (observe.Sink, func()) {
	traceStore, err := observesqlite.New("./.ai-agent/devui.db")
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
