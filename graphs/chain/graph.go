package chain

import (
	"context"
	"fmt"
	"strings"

	"github.com/PipeOpsHQ/agent-sdk-go/graph"
	"github.com/PipeOpsHQ/agent-sdk-go/state"
	"github.com/PipeOpsHQ/agent-sdk-go/workflow"
)

const Name = "chain"

type Builder struct{}

func (Builder) Name() string { return Name }
func (Builder) Description() string {
	return "Multi-step chain: analyze → plan → execute → synthesize."
}

func (Builder) NewExecutor(runner graph.AgentRunner, store state.Store, sessionID string) (*graph.Executor, error) {
	return NewExecutor(runner, store, sessionID)
}

func NewExecutor(runner graph.AgentRunner, store state.Store, sessionID string) (*graph.Executor, error) {
	if runner == nil {
		return nil, fmt.Errorf("runner is required")
	}
	g := graph.New(Name)

	// Step 1: Analyze — understand the task
	g.AddNode("analyze", &graph.AgentNode{
		Runner: runner,
		Input: func(s *graph.State) (string, error) {
			s.EnsureData()
			return fmt.Sprintf("Analyze the following request carefully. Identify the key requirements, constraints, and what needs to be done. Be concise.\n\nRequest: %s", strings.TrimSpace(s.Input)), nil
		},
		OutputKey: "analysis",
	})

	// Step 2: Plan — create an action plan based on analysis
	g.AddNode("plan", &graph.AgentNode{
		Runner: runner,
		Input: func(s *graph.State) (string, error) {
			s.EnsureData()
			analysis, _ := s.Data["analysis"].(string)
			return fmt.Sprintf("Based on this analysis, create a step-by-step action plan. Be specific and actionable.\n\nOriginal request: %s\n\nAnalysis: %s", s.Input, analysis), nil
		},
		OutputKey: "plan",
	})

	// Step 3: Execute — carry out the plan
	g.AddNode("execute", &graph.AgentNode{
		Runner: runner,
		Input: func(s *graph.State) (string, error) {
			s.EnsureData()
			plan, _ := s.Data["plan"].(string)
			return fmt.Sprintf("Execute the following plan. Provide the complete result.\n\nOriginal request: %s\n\nPlan: %s", s.Input, plan), nil
		},
		OutputKey: "execution",
	})

	// Step 4: Synthesize — review and produce final output
	g.AddNode("synthesize", graph.NewToolNode(func(ctx context.Context, s *graph.State) error {
		_ = ctx
		s.EnsureData()
		execution, _ := s.Data["execution"].(string)
		s.Output = strings.TrimSpace(execution)
		return nil
	}))

	g.SetStart("analyze")
	g.AddEdge("analyze", "plan", nil)
	g.AddEdge("plan", "execute", nil)
	g.AddEdge("execute", "synthesize", nil)

	opts := []graph.ExecutorOption{graph.WithStore(store)}
	if sessionID != "" {
		opts = append(opts, graph.WithSessionID(sessionID))
	}
	return graph.NewExecutor(g, opts...)
}

func init() {
	workflow.MustRegister(Builder{})
}
