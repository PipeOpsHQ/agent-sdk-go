package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/PipeOpsHQ/agent-sdk-go/framework/devui"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/flow"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/graph"
	statesqlite "github.com/PipeOpsHQ/agent-sdk-go/framework/state/sqlite"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/types"
)

type staticRunner struct{}

func (staticRunner) RunDetailed(ctx context.Context, input string) (types.RunResult, error) {
	_ = ctx
	now := time.Now().UTC()
	out := "STATIC-RUNNER: " + strings.ToUpper(strings.TrimSpace(input))
	return types.RunResult{Output: out, Provider: "static-runner", StartedAt: &now, CompletedAt: &now}, nil
}

func main() {
	if len(os.Args) > 1 && strings.ToLower(os.Args[1]) == "ui" {
		runDevUI()
		return
	}

	ctx := context.Background()
	store, err := statesqlite.New("./.ai-agent/examples-graph-resume.db")
	if err != nil {
		log.Fatalf("state store setup failed: %v", err)
	}
	defer func() { _ = store.Close() }()

	input := strings.TrimSpace(strings.Join(os.Args[1:], " "))
	if input == "" {
		input = "critical vulns found in payment-service"
	}

	runner := staticRunner{}
	g := graph.New("resume-demo").
		AddNode("prepare", graph.NewToolNode(func(ctx context.Context, st *graph.State) error {
			_ = ctx
			st.EnsureData()
			st.Data["prepared"] = true
			st.Data["prompt"] = "summarize incident: " + st.Input
			return nil
		})).
		AddNode("analyze", graph.NewAgentNode(runner, func(st *graph.State) (string, error) {
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
			st.Output = strings.TrimSpace(st.Output) + "\nSTATUS: READY_FOR_TRIAGE"
			return nil
		})).
		AddEdge("prepare", "analyze", graph.Always).
		AddEdge("analyze", "finalize", graph.Always).
		SetStart("prepare")

	exec, err := graph.NewExecutor(g, graph.WithStore(store))
	if err != nil {
		log.Fatalf("graph executor create failed: %v", err)
	}

	result, err := exec.Run(ctx, input)
	if err != nil {
		log.Fatalf("graph run failed: %v", err)
	}
	fmt.Printf("first run id=%s session=%s\n%s\n\n", result.RunID, result.SessionID, result.Output)

	resumed, err := exec.Resume(ctx, result.RunID)
	if err != nil {
		log.Fatalf("resume failed: %v", err)
	}
	fmt.Printf("resume run id=%s session=%s\n%s\n", resumed.RunID, resumed.SessionID, resumed.Output)
}

func runDevUI() {
	flow.MustRegister(&flow.Definition{
		Name:         "graph-resume",
		Description:  "Demonstrates graph execution with prepare→analyze→finalize nodes and run resume capability.",
		SystemPrompt: "You analyze security incidents and prepare triage reports.",
		InputExample: "critical vulns found in payment-service",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"input": map[string]any{
					"type":        "string",
					"description": "Incident description to process through the graph pipeline.",
				},
			},
			"required": []string{"input"},
		},
	})

	if err := devui.Start(context.Background()); err != nil {
		log.Fatal(err)
	}
}
