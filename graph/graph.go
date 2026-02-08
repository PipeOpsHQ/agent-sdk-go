package graph

import (
	"context"
	"fmt"
	"sort"
)

type Condition func(ctx context.Context, state *State) (bool, error)

type Edge struct {
	From      string
	To        string
	Condition Condition
}

type Graph struct {
	name        string
	nodes       map[string]Node
	edges       map[string][]Edge
	startNodeID string
	allowCycles bool
	buildErr    error
}

func New(name string) *Graph {
	return &Graph{
		name:  name,
		nodes: map[string]Node{},
		edges: map[string][]Edge{},
	}
}

func (g *Graph) Name() string {
	if g == nil {
		return ""
	}
	return g.name
}

func (g *Graph) AddNode(id string, node Node) *Graph {
	if g == nil {
		return g
	}
	if g.buildErr != nil {
		return g
	}
	if id == "" {
		g.buildErr = fmt.Errorf("node id is required")
		return g
	}
	if node == nil {
		g.buildErr = fmt.Errorf("node %q is nil", id)
		return g
	}
	if _, exists := g.nodes[id]; exists {
		g.buildErr = fmt.Errorf("node %q already exists", id)
		return g
	}
	g.nodes[id] = node
	return g
}

func (g *Graph) AddEdge(from, to string, condition Condition) *Graph {
	if g == nil {
		return g
	}
	if g.buildErr != nil {
		return g
	}
	if from == "" || to == "" {
		g.buildErr = fmt.Errorf("edge endpoints are required")
		return g
	}
	g.edges[from] = append(g.edges[from], Edge{
		From:      from,
		To:        to,
		Condition: condition,
	})
	return g
}

func (g *Graph) SetStart(id string) *Graph {
	if g == nil {
		return g
	}
	if g.buildErr != nil {
		return g
	}
	if id == "" {
		g.buildErr = fmt.Errorf("start node id is required")
		return g
	}
	g.startNodeID = id
	return g
}

func (g *Graph) AllowCycles(allow bool) *Graph {
	if g == nil {
		return g
	}
	g.allowCycles = allow
	return g
}

func (g *Graph) Compile() error {
	if g == nil {
		return fmt.Errorf("graph is nil")
	}
	if g.buildErr != nil {
		return g.buildErr
	}
	if g.name == "" {
		return fmt.Errorf("graph name is required")
	}
	if len(g.nodes) == 0 {
		return fmt.Errorf("graph has no nodes")
	}
	if g.startNodeID == "" {
		return fmt.Errorf("start node is not set")
	}
	if _, ok := g.nodes[g.startNodeID]; !ok {
		return fmt.Errorf("start node %q does not exist", g.startNodeID)
	}

	for from, edges := range g.edges {
		if _, ok := g.nodes[from]; !ok {
			return fmt.Errorf("edge source node %q does not exist", from)
		}
		for _, edge := range edges {
			if _, ok := g.nodes[edge.To]; !ok {
				return fmt.Errorf("edge target node %q does not exist", edge.To)
			}
		}
	}

	unreachable := g.unreachableNodes()
	if len(unreachable) > 0 {
		sort.Strings(unreachable)
		return fmt.Errorf("graph contains unreachable node(s): %v", unreachable)
	}

	if !g.allowCycles && g.hasCycle() {
		return fmt.Errorf("graph contains cycle(s); call AllowCycles(true) to enable")
	}

	return nil
}

func (g *Graph) unreachableNodes() []string {
	visited := map[string]bool{}
	var dfs func(nodeID string)
	dfs = func(nodeID string) {
		if visited[nodeID] {
			return
		}
		visited[nodeID] = true
		for _, edge := range g.edges[nodeID] {
			dfs(edge.To)
		}
	}
	dfs(g.startNodeID)

	out := make([]string, 0)
	for nodeID := range g.nodes {
		if !visited[nodeID] {
			out = append(out, nodeID)
		}
	}
	return out
}

func (g *Graph) hasCycle() bool {
	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := make(map[string]int, len(g.nodes))

	var visit func(nodeID string) bool
	visit = func(nodeID string) bool {
		color[nodeID] = gray
		for _, edge := range g.edges[nodeID] {
			switch color[edge.To] {
			case gray:
				return true
			case white:
				if visit(edge.To) {
					return true
				}
			}
		}
		color[nodeID] = black
		return false
	}

	for nodeID := range g.nodes {
		if color[nodeID] == white && visit(nodeID) {
			return true
		}
	}
	return false
}

func Always(_ context.Context, _ *State) (bool, error) { return true, nil }

// NodeInfo describes a node in the graph for introspection.
type NodeInfo struct {
	ID   string `json:"id"`
	Kind string `json:"kind"` // "agent", "tool", or "router"
}

// EdgeInfo describes an edge in the graph for introspection.
type EdgeInfo struct {
	From        string `json:"from"`
	To          string `json:"to"`
	Conditional bool   `json:"conditional"`
}

// NodeInfos returns metadata about all nodes in the graph.
func (g *Graph) NodeInfos() []NodeInfo {
	if g == nil {
		return nil
	}
	out := make([]NodeInfo, 0, len(g.nodes))
	for id, node := range g.nodes {
		kind := "tool"
		switch node.(type) {
		case *AgentNode:
			kind = "agent"
		case *RouterNode:
			kind = "router"
		}
		out = append(out, NodeInfo{ID: id, Kind: kind})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// EdgeInfos returns metadata about all edges in the graph.
func (g *Graph) EdgeInfos() []EdgeInfo {
	if g == nil {
		return nil
	}
	out := make([]EdgeInfo, 0)
	keys := make([]string, 0, len(g.edges))
	for k := range g.edges {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, from := range keys {
		for _, edge := range g.edges[from] {
			out = append(out, EdgeInfo{
				From:        edge.From,
				To:          edge.To,
				Conditional: edge.Condition != nil,
			})
		}
	}
	return out
}

// StartNodeID returns the ID of the start node.
func (g *Graph) StartNodeID() string {
	if g == nil {
		return ""
	}
	return g.startNodeID
}

func RouteEquals(key, expected string) Condition {
	if key == "" {
		key = "route"
	}
	return func(_ context.Context, state *State) (bool, error) {
		if state == nil {
			return false, nil
		}
		if state.Data == nil {
			return false, nil
		}
		value, ok := state.Data[key].(string)
		if !ok {
			return false, nil
		}
		return value == expected, nil
	}
}
