package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/PipeOpsHQ/agent-sdk-go/framework/graph"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/state"
)

type FileSpec struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Start       string         `json:"start"`
	AllowCycles bool           `json:"allowCycles"`
	Nodes       []FileNodeSpec `json:"nodes"`
	Edges       []FileEdgeSpec `json:"edges"`
}

type FileNodeSpec struct {
	ID           string `json:"id"`
	Kind         string `json:"kind"`
	Key          string `json:"key,omitempty"`
	Value        any    `json:"value,omitempty"`
	Template     string `json:"template,omitempty"`
	OutputKey    string `json:"outputKey,omitempty"`
	InputFrom    string `json:"inputFrom,omitempty"`
	From         string `json:"from,omitempty"`
	CheckKey     string `json:"checkKey,omitempty"`
	ExistsValue  string `json:"existsValue,omitempty"`
	MissingValue string `json:"missingValue,omitempty"`
}

type FileEdgeSpec struct {
	From string        `json:"from"`
	To   string        `json:"to"`
	When *FileEdgeWhen `json:"when,omitempty"`
}

type FileEdgeWhen struct {
	Key    string `json:"key"`
	Equals string `json:"equals"`
}

type fileBuilder struct {
	spec FileSpec
}

func NewFileBuilder(spec FileSpec) (Builder, error) {
	normalized, err := normalizeFileSpec(spec, "inline workflow")
	if err != nil {
		return nil, err
	}
	return &fileBuilder{spec: normalized}, nil
}

func NewFileBuilderFromPath(path string) (Builder, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("workflow file path is required")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve workflow file path: %w", err)
	}
	content, err := os.ReadFile(abs)
	if err != nil {
		return nil, fmt.Errorf("failed to read workflow file %q: %w", abs, err)
	}
	var spec FileSpec
	if err := json.Unmarshal(content, &spec); err != nil {
		return nil, fmt.Errorf("failed to decode workflow file %q as JSON: %w", abs, err)
	}
	normalized, err := normalizeFileSpec(spec, abs)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(normalized.Name) == "" {
		base := filepath.Base(abs)
		normalized.Name = strings.TrimSuffix(base, filepath.Ext(base))
	}
	return &fileBuilder{spec: normalized}, nil
}

func normalizeFileSpec(spec FileSpec, source string) (FileSpec, error) {
	spec.Name = strings.TrimSpace(spec.Name)
	spec.Description = strings.TrimSpace(spec.Description)
	spec.Start = strings.TrimSpace(spec.Start)
	if spec.Start == "" {
		return FileSpec{}, fmt.Errorf("workflow %q missing start node", strings.TrimSpace(source))
	}
	if len(spec.Nodes) == 0 {
		return FileSpec{}, fmt.Errorf("workflow %q has no nodes", strings.TrimSpace(source))
	}
	for i := range spec.Nodes {
		spec.Nodes[i].ID = strings.TrimSpace(spec.Nodes[i].ID)
		spec.Nodes[i].Kind = strings.TrimSpace(spec.Nodes[i].Kind)
		spec.Nodes[i].Key = strings.TrimSpace(spec.Nodes[i].Key)
		spec.Nodes[i].Template = strings.TrimSpace(spec.Nodes[i].Template)
		spec.Nodes[i].OutputKey = strings.TrimSpace(spec.Nodes[i].OutputKey)
		spec.Nodes[i].InputFrom = strings.TrimSpace(spec.Nodes[i].InputFrom)
		spec.Nodes[i].From = strings.TrimSpace(spec.Nodes[i].From)
		spec.Nodes[i].CheckKey = strings.TrimSpace(spec.Nodes[i].CheckKey)
		spec.Nodes[i].ExistsValue = strings.TrimSpace(spec.Nodes[i].ExistsValue)
		spec.Nodes[i].MissingValue = strings.TrimSpace(spec.Nodes[i].MissingValue)
	}
	for i := range spec.Edges {
		spec.Edges[i].From = strings.TrimSpace(spec.Edges[i].From)
		spec.Edges[i].To = strings.TrimSpace(spec.Edges[i].To)
		if spec.Edges[i].When != nil {
			spec.Edges[i].When.Key = strings.TrimSpace(spec.Edges[i].When.Key)
			spec.Edges[i].When.Equals = strings.TrimSpace(spec.Edges[i].When.Equals)
		}
	}
	return spec, nil
}

func (b *fileBuilder) Name() string {
	if b == nil {
		return ""
	}
	return strings.TrimSpace(b.spec.Name)
}

func (b *fileBuilder) Description() string {
	if b == nil {
		return ""
	}
	return strings.TrimSpace(b.spec.Description)
}

func (b *fileBuilder) NewExecutor(runner graph.AgentRunner, store state.Store, sessionID string) (*graph.Executor, error) {
	if b == nil {
		return nil, fmt.Errorf("file builder is nil")
	}
	if runner == nil {
		return nil, fmt.Errorf("runner is required")
	}

	g := graph.New(b.spec.Name)
	if b.spec.AllowCycles {
		g.AllowCycles(true)
	}
	for _, nodeSpec := range b.spec.Nodes {
		node, err := buildNodeFromSpec(nodeSpec, runner)
		if err != nil {
			return nil, fmt.Errorf("node %q: %w", nodeSpec.ID, err)
		}
		g.AddNode(nodeSpec.ID, node)
	}
	g.SetStart(b.spec.Start)

	for _, edgeSpec := range b.spec.Edges {
		var condition graph.Condition
		if edgeSpec.When != nil {
			when := *edgeSpec.When
			condition = func(ctx context.Context, s *graph.State) (bool, error) {
				_ = ctx
				left := resolveToken(when.Key, s)
				return left == when.Equals, nil
			}
		}
		g.AddEdge(edgeSpec.From, edgeSpec.To, condition)
	}

	opts := []graph.ExecutorOption{graph.WithStore(store)}
	if sessionID != "" {
		opts = append(opts, graph.WithSessionID(sessionID))
	}
	return graph.NewExecutor(g, opts...)
}

func buildNodeFromSpec(spec FileNodeSpec, runner graph.AgentRunner) (graph.Node, error) {
	spec.ID = strings.TrimSpace(spec.ID)
	spec.Kind = strings.TrimSpace(spec.Kind)
	if spec.ID == "" {
		return nil, fmt.Errorf("node id is required")
	}
	if spec.Kind == "" {
		return nil, fmt.Errorf("node kind is required")
	}

	switch spec.Kind {
	case "noop":
		return graph.NewToolNode(func(ctx context.Context, s *graph.State) error {
			_ = ctx
			s.EnsureData()
			return nil
		}), nil

	case "set":
		if strings.TrimSpace(spec.Key) == "" {
			return nil, fmt.Errorf("set node requires key")
		}
		key := spec.Key
		value := spec.Value
		return graph.NewToolNode(func(ctx context.Context, s *graph.State) error {
			_ = ctx
			s.EnsureData()
			s.Data[key] = value
			return nil
		}), nil

	case "template":
		if strings.TrimSpace(spec.OutputKey) == "" {
			return nil, fmt.Errorf("template node requires outputKey")
		}
		tpl := spec.Template
		outputKey := spec.OutputKey
		return graph.NewToolNode(func(ctx context.Context, s *graph.State) error {
			_ = ctx
			s.EnsureData()
			s.Data[outputKey] = renderTemplate(tpl, s)
			return nil
		}), nil

	case "agent":
		inputFrom := strings.TrimSpace(spec.InputFrom)
		tpl := spec.Template
		value := spec.Value
		return &graph.AgentNode{
			Runner: runner,
			Input: func(s *graph.State) (string, error) {
				s.EnsureData()
				if inputFrom != "" {
					if v, ok := s.Data[inputFrom]; ok {
						return stringify(v), nil
					}
				}
				if strings.TrimSpace(tpl) != "" {
					return renderTemplate(tpl, s), nil
				}
				if value != nil {
					return stringify(value), nil
				}
				return s.Input, nil
			},
			OutputKey: strings.TrimSpace(spec.OutputKey),
		}, nil

	case "output":
		from := strings.TrimSpace(spec.From)
		tpl := spec.Template
		value := spec.Value
		return graph.NewToolNode(func(ctx context.Context, s *graph.State) error {
			_ = ctx
			s.EnsureData()
			switch {
			case from != "":
				s.Output = strings.TrimSpace(resolveToken(from, s))
			case strings.TrimSpace(tpl) != "":
				s.Output = strings.TrimSpace(renderTemplate(tpl, s))
			case value != nil:
				s.Output = strings.TrimSpace(stringify(value))
			}
			s.Data["output"] = s.Output
			return nil
		}), nil

	case "router_json_key":
		checkKey := strings.TrimSpace(spec.CheckKey)
		if checkKey == "" {
			return nil, fmt.Errorf("router_json_key node requires checkKey")
		}
		routeKey := strings.TrimSpace(spec.Key)
		if routeKey == "" {
			routeKey = "route"
		}
		existsVal := spec.ExistsValue
		if existsVal == "" {
			existsVal = "true"
		}
		missingVal := spec.MissingValue
		if missingVal == "" {
			missingVal = "false"
		}
		return graph.NewToolNode(func(ctx context.Context, s *graph.State) error {
			_ = ctx
			s.EnsureData()
			var obj map[string]any
			if err := json.Unmarshal([]byte(strings.TrimSpace(s.Input)), &obj); err == nil {
				if _, ok := obj[checkKey]; ok {
					s.Data[routeKey] = existsVal
					return nil
				}
			}
			s.Data[routeKey] = missingVal
			return nil
		}), nil
	}

	return nil, fmt.Errorf("unsupported node kind %q", spec.Kind)
}

var tokenPattern = regexp.MustCompile(`\{\{\s*([^}]+?)\s*\}\}`)

func renderTemplate(template string, s *graph.State) string {
	if template == "" {
		return ""
	}
	return tokenPattern.ReplaceAllStringFunc(template, func(match string) string {
		token := tokenPattern.FindStringSubmatch(match)
		if len(token) < 2 {
			return ""
		}
		return resolveToken(token[1], s)
	})
}

func resolveToken(token string, s *graph.State) string {
	token = strings.TrimSpace(token)
	if s == nil {
		return ""
	}
	s.EnsureData()

	switch token {
	case "input":
		return s.Input
	case "output":
		return s.Output
	case "runId":
		return s.RunID
	case "sessionId":
		return s.SessionID
	}
	if strings.HasPrefix(token, "data.") {
		key := strings.TrimPrefix(token, "data.")
		return stringify(s.Data[key])
	}
	return stringify(s.Data[token])
}

func stringify(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case []byte:
		return string(t)
	default:
		raw, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprintf("%v", v)
		}
		return string(raw)
	}
}
