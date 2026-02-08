package graph

import (
	"context"
	"fmt"

	"github.com/PipeOpsHQ/agent-sdk-go/framework/types"
)

type Node interface {
	Execute(ctx context.Context, state *State) error
}

type InputBuilder func(state *State) (string, error)

type AgentRunner interface {
	RunDetailed(ctx context.Context, input string) (types.RunResult, error)
}

type AgentNode struct {
	Runner    AgentRunner
	Input     InputBuilder
	OutputKey string
}

func NewAgentNode(runner AgentRunner, input InputBuilder) *AgentNode {
	return &AgentNode{Runner: runner, Input: input}
}

func (n *AgentNode) Execute(ctx context.Context, state *State) error {
	if n == nil || n.Runner == nil {
		return fmt.Errorf("agent node runner is required")
	}
	if state == nil {
		return fmt.Errorf("state is required")
	}

	input := state.Input
	if n.Input != nil {
		in, err := n.Input(state)
		if err != nil {
			return err
		}
		input = in
	}

	result, err := n.Runner.RunDetailed(ctx, input)
	if err != nil {
		return err
	}

	state.Output = result.Output
	state.ensureData()

	key := n.OutputKey
	if key == "" {
		key = "agent_output"
	}
	state.Data[key] = result.Output
	if result.RunID != "" {
		state.Data["agentRunID"] = result.RunID
	}
	if result.SessionID != "" {
		state.Data["agentSessionID"] = result.SessionID
	}

	return nil
}

type ToolFunc func(ctx context.Context, state *State) error

type ToolNode struct {
	Func ToolFunc
}

func NewToolNode(fn ToolFunc) *ToolNode {
	return &ToolNode{Func: fn}
}

func (n *ToolNode) Execute(ctx context.Context, state *State) error {
	if n == nil || n.Func == nil {
		return fmt.Errorf("tool node func is required")
	}
	if state == nil {
		return fmt.Errorf("state is required")
	}
	return n.Func(ctx, state)
}

type RouteFunc func(ctx context.Context, state *State) (string, error)

type RouterNode struct {
	Route    RouteFunc
	RouteKey string
}

func NewRouterNode(route RouteFunc) *RouterNode {
	return &RouterNode{Route: route}
}

func (n *RouterNode) Execute(ctx context.Context, state *State) error {
	if n == nil || n.Route == nil {
		return fmt.Errorf("router node route func is required")
	}
	if state == nil {
		return fmt.Errorf("state is required")
	}

	route, err := n.Route(ctx, state)
	if err != nil {
		return err
	}
	state.ensureData()
	key := n.RouteKey
	if key == "" {
		key = "route"
	}
	state.Data[key] = route
	return nil
}
