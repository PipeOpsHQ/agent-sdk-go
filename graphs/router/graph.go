package router

import (
	"context"
	"fmt"
	"strings"

	"github.com/PipeOpsHQ/agent-sdk-go/graph"
	"github.com/PipeOpsHQ/agent-sdk-go/state"
	"github.com/PipeOpsHQ/agent-sdk-go/workflow"
)

const Name = "router"

type Builder struct{}

func (Builder) Name() string { return Name }
func (Builder) Description() string {
	return "Classification router: classify input → route to specialized handler."
}

func (Builder) NewExecutor(runner graph.AgentRunner, store state.Store, sessionID string) (*graph.Executor, error) {
	return NewExecutor(runner, store, sessionID)
}

func NewExecutor(runner graph.AgentRunner, store state.Store, sessionID string) (*graph.Executor, error) {
	if runner == nil {
		return nil, fmt.Errorf("runner is required")
	}
	g := graph.New(Name)

	// Classify — determine the category of the input
	g.AddNode("classify", &graph.AgentNode{
		Runner: runner,
		Input: func(s *graph.State) (string, error) {
			s.EnsureData()
			return fmt.Sprintf(`Classify the following request into exactly ONE category. Respond with ONLY the category name, nothing else.

Categories:
- code: Writing, debugging, reviewing, or explaining code
- data: Data analysis, transformation, querying, or visualization
- writing: Creative writing, editing, summarizing, or translating text
- ops: DevOps, infrastructure, deployment, monitoring, or system administration
- general: Anything that doesn't fit the above categories

Request: %s`, strings.TrimSpace(s.Input)), nil
		},
		OutputKey: "category",
	})

	// Router — set the route based on classification
	g.AddNode("route", graph.NewRouterNode(func(ctx context.Context, s *graph.State) (string, error) {
		_ = ctx
		s.EnsureData()
		category := strings.ToLower(strings.TrimSpace(s.Data["category"].(string)))
		switch {
		case strings.Contains(category, "code"):
			return "code", nil
		case strings.Contains(category, "data"):
			return "data", nil
		case strings.Contains(category, "writing"):
			return "writing", nil
		case strings.Contains(category, "ops"):
			return "ops", nil
		default:
			return "general", nil
		}
	}))

	// Specialized handlers
	addHandler(g, runner, "handle_code", "code",
		"You are an expert software engineer. Write clean, efficient, well-documented code. Include error handling and tests when appropriate.")
	addHandler(g, runner, "handle_data", "data",
		"You are an expert data analyst. Provide clear analysis, use appropriate statistical methods, and present findings clearly.")
	addHandler(g, runner, "handle_writing", "writing",
		"You are an expert writer and editor. Produce clear, engaging, well-structured content tailored to the audience.")
	addHandler(g, runner, "handle_ops", "ops",
		"You are an expert DevOps/SRE engineer. Provide production-ready solutions with security, scalability, and reliability in mind.")
	addHandler(g, runner, "handle_general", "general",
		"You are a helpful, knowledgeable assistant. Provide accurate, well-reasoned answers.")

	// Finalize — collect output
	g.AddNode("finalize", graph.NewToolNode(func(ctx context.Context, s *graph.State) error {
		_ = ctx
		s.EnsureData()
		for _, key := range []string{"handlerOutput"} {
			if v, ok := s.Data[key].(string); ok && v != "" {
				s.Output = strings.TrimSpace(v)
				return nil
			}
		}
		return nil
	}))

	g.SetStart("classify")
	g.AddEdge("classify", "route", nil)
	g.AddEdge("route", "handle_code", graph.RouteEquals("route", "code"))
	g.AddEdge("route", "handle_data", graph.RouteEquals("route", "data"))
	g.AddEdge("route", "handle_writing", graph.RouteEquals("route", "writing"))
	g.AddEdge("route", "handle_ops", graph.RouteEquals("route", "ops"))
	g.AddEdge("route", "handle_general", graph.RouteEquals("route", "general"))
	g.AddEdge("handle_code", "finalize", nil)
	g.AddEdge("handle_data", "finalize", nil)
	g.AddEdge("handle_writing", "finalize", nil)
	g.AddEdge("handle_ops", "finalize", nil)
	g.AddEdge("handle_general", "finalize", nil)

	opts := []graph.ExecutorOption{graph.WithStore(store)}
	if sessionID != "" {
		opts = append(opts, graph.WithSessionID(sessionID))
	}
	return graph.NewExecutor(g, opts...)
}

func addHandler(g *graph.Graph, runner graph.AgentRunner, nodeID, category, systemContext string) {
	g.AddNode(nodeID, &graph.AgentNode{
		Runner: runner,
		Input: func(s *graph.State) (string, error) {
			s.EnsureData()
			return fmt.Sprintf("%s\n\nRequest: %s", systemContext, strings.TrimSpace(s.Input)), nil
		},
		OutputKey: "handlerOutput",
	})
}

func init() {
	workflow.MustRegister(Builder{})
}
