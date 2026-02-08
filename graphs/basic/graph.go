package basic

import (
	"context"
	"fmt"
	"strings"

	"github.com/PipeOpsHQ/agent-sdk-go/framework/graph"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/state"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/workflow"
)

const Name = "basic"

type Builder struct{}

func (Builder) Name() string { return Name }

func (Builder) Description() string {
	return "Generic single-agent graph (prepare -> assistant -> finalize)."
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
	g.AddNode("assistant", &graph.AgentNode{
		Runner: runner,
		Input: func(s *graph.State) (string, error) {
			s.EnsureData()
			if v, ok := s.Data["prompt"].(string); ok {
				return v, nil
			}
			return s.Input, nil
		},
		OutputKey: "assistantOutput",
	})
	g.AddNode("finalize", graph.NewToolNode(func(ctx context.Context, s *graph.State) error {
		_ = ctx
		s.EnsureData()
		if v, ok := s.Data["assistantOutput"].(string); ok {
			s.Output = strings.TrimSpace(v)
		}
		s.Data["output"] = s.Output
		return nil
	}))
	g.SetStart("prepare")
	g.AddEdge("prepare", "assistant", nil)
	g.AddEdge("assistant", "finalize", nil)

	opts := []graph.ExecutorOption{graph.WithStore(store)}
	if sessionID != "" {
		opts = append(opts, graph.WithSessionID(sessionID))
	}
	return graph.NewExecutor(g, opts...)
}

func init() {
	workflow.MustRegister(Builder{})
}
