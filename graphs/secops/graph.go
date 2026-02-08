package secops

import (
	"fmt"
	"strings"

	secopsadapters "github.com/nitrocode/ai-agents/framework/adapters/secops"
	"github.com/nitrocode/ai-agents/framework/graph"
	"github.com/nitrocode/ai-agents/framework/state"
)

type Config struct {
	Store     state.Store
	SessionID string
}

func NewExecutor(runner graph.AgentRunner, cfg Config) (*graph.Executor, error) {
	if runner == nil {
		return nil, fmt.Errorf("runner is required")
	}

	g := graph.New(Name)
	g.AddNode("route", secopsadapters.DetectInputRouteNode())

	g.AddNode("parse_trivy", secopsadapters.ParseTrivyNode())
	g.AddNode("build_trivy_prompt", secopsadapters.BuildTrivyPromptNode())
	g.AddNode("assistant_trivy", &graph.AgentNode{
		Runner: runner,
		Input: func(s *graph.State) (string, error) {
			s.EnsureData()
			if v, ok := s.Data[secopsadapters.KeyPromptTrivy].(string); ok && strings.TrimSpace(v) != "" {
				return v, nil
			}
			return s.Input, nil
		},
		OutputKey: "trivyAgentOutput",
	})

	g.AddNode("redact_logs", secopsadapters.RedactLogsNode())
	g.AddNode("classify_logs", secopsadapters.ClassifyLogsNode())
	g.AddNode("build_logs_prompt", secopsadapters.BuildLogsPromptNode())
	g.AddNode("assistant_logs", &graph.AgentNode{
		Runner: runner,
		Input: func(s *graph.State) (string, error) {
			s.EnsureData()
			if v, ok := s.Data[secopsadapters.KeyPromptLogs].(string); ok && strings.TrimSpace(v) != "" {
				return v, nil
			}
			return s.Input, nil
		},
		OutputKey: "logsAgentOutput",
	})

	g.AddNode("finalize", secopsadapters.FinalizeNode())
	g.SetStart("route")

	g.AddEdge("route", "parse_trivy", graph.RouteEquals(secopsadapters.RouteKey, secopsadapters.RouteTrivy))
	g.AddEdge("route", "redact_logs", graph.RouteEquals(secopsadapters.RouteKey, secopsadapters.RouteLogs))

	g.AddEdge("parse_trivy", "build_trivy_prompt", nil)
	g.AddEdge("build_trivy_prompt", "assistant_trivy", nil)
	g.AddEdge("assistant_trivy", "finalize", nil)

	g.AddEdge("redact_logs", "classify_logs", nil)
	g.AddEdge("classify_logs", "build_logs_prompt", nil)
	g.AddEdge("build_logs_prompt", "assistant_logs", nil)
	g.AddEdge("assistant_logs", "finalize", nil)

	opts := []graph.ExecutorOption{graph.WithStore(cfg.Store)}
	if cfg.SessionID != "" {
		opts = append(opts, graph.WithSessionID(cfg.SessionID))
	}
	return graph.NewExecutor(g, opts...)
}
