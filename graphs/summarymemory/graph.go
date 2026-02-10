package summarymemory

import (
	"context"
	"fmt"
	"strings"

	"github.com/PipeOpsHQ/agent-sdk-go/framework/graph"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/state"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/workflow"
)

const Name = "summary-memory"

type Builder struct{}

func (Builder) Name() string { return Name }

func (Builder) Description() string {
	return "Two-pass response with compressed summary memory (draft -> summarize -> refine)."
}

func (Builder) NewExecutor(runner graph.AgentRunner, store state.Store, sessionID string) (*graph.Executor, error) {
	return NewExecutor(runner, store, sessionID)
}

func NewExecutor(runner graph.AgentRunner, store state.Store, sessionID string) (*graph.Executor, error) {
	if runner == nil {
		return nil, fmt.Errorf("runner is required")
	}

	g := graph.New(Name)

	g.AddNode("prepare", graph.NewToolNode(func(ctx context.Context, s *graph.State) error {
		_ = ctx
		s.EnsureData()
		s.Input = strings.TrimSpace(s.Input)
		s.Data["prompt"] = s.Input
		return nil
	}))

	g.AddNode("draft", &graph.AgentNode{
		Runner: runner,
		Input: func(s *graph.State) (string, error) {
			s.EnsureData()
			prompt, _ := s.Data["prompt"].(string)
			return fmt.Sprintf("Draft an initial helpful response for the request below. Focus on completeness and correctness.\n\nRequest: %s", prompt), nil
		},
		OutputKey: "draftOutput",
	})

	g.AddNode("summarize", &graph.AgentNode{
		Runner: runner,
		Input: func(s *graph.State) (string, error) {
			s.EnsureData()
			draft, _ := s.Data["draftOutput"].(string)
			return fmt.Sprintf("Create a compact memory summary for future context reuse. Keep only durable facts, constraints, and decisions. Max 120 words.\n\nDraft response:\n%s", draft), nil
		},
		OutputKey: "memorySummary",
	})

	g.AddNode("refine", &graph.AgentNode{
		Runner: runner,
		Input: func(s *graph.State) (string, error) {
			s.EnsureData()
			prompt, _ := s.Data["prompt"].(string)
			draft, _ := s.Data["draftOutput"].(string)
			summary, _ := s.Data["memorySummary"].(string)
			return fmt.Sprintf("Improve the draft into a final response. Preserve correctness, improve clarity, and remove redundancy.\n\nOriginal request:\n%s\n\nDraft:\n%s\n\nCompact memory summary:\n%s", prompt, draft, summary), nil
		},
		OutputKey: "refinedOutput",
	})

	g.AddNode("finalize", graph.NewToolNode(func(ctx context.Context, s *graph.State) error {
		_ = ctx
		s.EnsureData()
		if refined, ok := s.Data["refinedOutput"].(string); ok {
			s.Output = strings.TrimSpace(refined)
		}
		if summary, ok := s.Data["memorySummary"].(string); ok {
			s.Data["nextContextSummary"] = strings.TrimSpace(summary)
		}
		s.Data["output"] = s.Output
		return nil
	}))

	g.SetStart("prepare")
	g.AddEdge("prepare", "draft", nil)
	g.AddEdge("draft", "summarize", nil)
	g.AddEdge("summarize", "refine", nil)
	g.AddEdge("refine", "finalize", nil)

	opts := []graph.ExecutorOption{graph.WithStore(store)}
	if sessionID != "" {
		opts = append(opts, graph.WithSessionID(sessionID))
	}
	return graph.NewExecutor(g, opts...)
}

func init() {
	workflow.MustRegister(Builder{})
}
