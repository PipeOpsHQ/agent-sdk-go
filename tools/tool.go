package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/PipeOpsHQ/agent-sdk-go/types"
)

type Tool interface {
	Definition() types.ToolDefinition
	Execute(ctx context.Context, args json.RawMessage) (any, error)
}

type FuncTool struct {
	def types.ToolDefinition
	fn  func(ctx context.Context, args json.RawMessage) (any, error)
}

func NewFuncTool(name, description string, schema map[string]any, fn func(ctx context.Context, args json.RawMessage) (any, error)) *FuncTool {
	return &FuncTool{
		def: types.ToolDefinition{
			Name:        name,
			Description: description,
			JSONSchema:  schema,
		},
		fn: fn,
	}
}

func (t *FuncTool) Definition() types.ToolDefinition {
	return t.def
}

func (t *FuncTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	if t.fn == nil {
		return nil, fmt.Errorf("tool %q has no execute function", t.def.Name)
	}
	return t.fn(ctx, args)
}
