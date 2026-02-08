package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/PipeOpsHQ/agent-sdk-go/framework/types"
)

type Middleware interface {
	BeforeGenerate(ctx context.Context, event *GenerateMiddlewareEvent) error
	AfterGenerate(ctx context.Context, event *GenerateMiddlewareEvent) error
	BeforeTool(ctx context.Context, event *ToolMiddlewareEvent) error
	AfterTool(ctx context.Context, event *ToolMiddlewareEvent) error
	OnError(ctx context.Context, event *ErrorMiddlewareEvent)
}

type NoopMiddleware struct{}

func (NoopMiddleware) BeforeGenerate(ctx context.Context, event *GenerateMiddlewareEvent) error {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	if event == nil {
		return fmt.Errorf("before-generate event is required")
	}
	if event.StartedAt.IsZero() {
		event.StartedAt = time.Now().UTC()
	}
	if event.FinishedAt.IsZero() {
		event.FinishedAt = event.StartedAt
	}
	if event.Request == nil {
		event.Request = &types.Request{}
	}
	return nil
}

func (NoopMiddleware) AfterGenerate(ctx context.Context, event *GenerateMiddlewareEvent) error {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	if event == nil {
		return fmt.Errorf("after-generate event is required")
	}
	if event.StartedAt.IsZero() {
		event.StartedAt = time.Now().UTC()
	}
	if event.FinishedAt.IsZero() {
		event.FinishedAt = time.Now().UTC()
	}
	if event.Request == nil {
		event.Request = &types.Request{}
	}
	return nil
}

func (NoopMiddleware) BeforeTool(ctx context.Context, event *ToolMiddlewareEvent) error {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	if event == nil {
		return fmt.Errorf("before-tool event is required")
	}
	if event.StartedAt.IsZero() {
		event.StartedAt = time.Now().UTC()
	}
	if event.FinishedAt.IsZero() {
		event.FinishedAt = event.StartedAt
	}
	if event.ToolCall == nil {
		event.ToolCall = &types.ToolCall{}
	}
	return nil
}

func (NoopMiddleware) AfterTool(ctx context.Context, event *ToolMiddlewareEvent) error {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	if event == nil {
		return fmt.Errorf("after-tool event is required")
	}
	if event.StartedAt.IsZero() {
		event.StartedAt = time.Now().UTC()
	}
	if event.FinishedAt.IsZero() {
		event.FinishedAt = time.Now().UTC()
	}
	if event.ToolCall == nil {
		event.ToolCall = &types.ToolCall{}
	}
	return nil
}

func (NoopMiddleware) OnError(ctx context.Context, event *ErrorMiddlewareEvent) {
	if event == nil {
		return
	}
	if event.Stage == "" {
		event.Stage = "unknown"
	}
	if event.Err == nil && ctx != nil && ctx.Err() != nil {
		event.Err = ctx.Err()
	}
}

type GenerateMiddlewareEvent struct {
	RunID      string
	SessionID  string
	Provider   string
	Iteration  int
	StartedAt  time.Time
	FinishedAt time.Time
	Request    *types.Request
	Response   *types.Response
}

type ToolMiddlewareEvent struct {
	RunID      string
	SessionID  string
	Provider   string
	Iteration  int
	StartedAt  time.Time
	FinishedAt time.Time
	ToolCall   *types.ToolCall
	Result     *types.Message
	ToolError  error
}

type ErrorMiddlewareEvent struct {
	RunID     string
	SessionID string
	Provider  string
	Iteration int
	Stage     string
	ToolName  string
	Err       error
}
