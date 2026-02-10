package mapreduce

import (
	"context"
	"fmt"
	"strings"

	"github.com/PipeOpsHQ/agent-sdk-go/graph"
	"github.com/PipeOpsHQ/agent-sdk-go/state"
	"github.com/PipeOpsHQ/agent-sdk-go/workflow"
)

const Name = "map-reduce"

type Builder struct{}

func (Builder) Name() string { return Name }
func (Builder) Description() string {
	return "Map-reduce: split input → process parts → combine results."
}

func (Builder) NewExecutor(runner graph.AgentRunner, store state.Store, sessionID string) (*graph.Executor, error) {
	return NewExecutor(runner, store, sessionID)
}

func NewExecutor(runner graph.AgentRunner, store state.Store, sessionID string) (*graph.Executor, error) {
	if runner == nil {
		return nil, fmt.Errorf("runner is required")
	}
	g := graph.New(Name)

	// Split — break input into logical parts for parallel-style processing
	g.AddNode("split", &graph.AgentNode{
		Runner: runner,
		Input: func(s *graph.State) (string, error) {
			s.EnsureData()
			return fmt.Sprintf(`Break the following request into 2-4 independent sub-tasks that can be worked on separately. Return ONLY a numbered list, one sub-task per line. Each sub-task should be self-contained.

Request: %s`, strings.TrimSpace(s.Input)), nil
		},
		OutputKey: "subtasks",
	})

	// Map — process all sub-tasks sequentially (the agent handles them as a batch)
	g.AddNode("map", &graph.AgentNode{
		Runner: runner,
		Input: func(s *graph.State) (string, error) {
			s.EnsureData()
			subtasks, _ := s.Data["subtasks"].(string)
			return fmt.Sprintf(`Complete each of the following sub-tasks. For each one, provide a clear, thorough response. Label each response with its sub-task number.

Original request: %s

Sub-tasks:
%s`, strings.TrimSpace(s.Input), subtasks), nil
		},
		OutputKey: "mapped_results",
	})

	// Reduce — combine all results into a coherent final output
	g.AddNode("reduce", &graph.AgentNode{
		Runner: runner,
		Input: func(s *graph.State) (string, error) {
			s.EnsureData()
			results, _ := s.Data["mapped_results"].(string)
			return fmt.Sprintf(`Combine the following sub-task results into a single coherent, well-structured response. Remove redundancy, ensure consistency, and present the final answer clearly.

Original request: %s

Sub-task results:
%s`, strings.TrimSpace(s.Input), results), nil
		},
		OutputKey: "final_output",
	})

	// Finalize — set output
	g.AddNode("finalize", graph.NewToolNode(func(ctx context.Context, s *graph.State) error {
		_ = ctx
		s.EnsureData()
		if v, ok := s.Data["final_output"].(string); ok {
			s.Output = strings.TrimSpace(v)
		}
		return nil
	}))

	g.SetStart("split")
	g.AddEdge("split", "map", nil)
	g.AddEdge("map", "reduce", nil)
	g.AddEdge("reduce", "finalize", nil)

	opts := []graph.ExecutorOption{graph.WithStore(store)}
	if sessionID != "" {
		opts = append(opts, graph.WithSessionID(sessionID))
	}
	return graph.NewExecutor(g, opts...)
}

func init() {
	workflow.MustRegister(Builder{})
}
