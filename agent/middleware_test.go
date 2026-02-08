package agent

import (
	"context"
	"testing"

	"github.com/nitrocode/ai-agents/framework/types"
)

func TestNoopMiddleware_BeforeGenerate_NormalizesEvent(t *testing.T) {
	m := NoopMiddleware{}
	event := &GenerateMiddlewareEvent{}
	if err := m.BeforeGenerate(context.Background(), event); err != nil {
		t.Fatalf("BeforeGenerate failed: %v", err)
	}
	if event.Request == nil {
		t.Fatalf("expected request to be initialized")
	}
	if event.StartedAt.IsZero() || event.FinishedAt.IsZero() {
		t.Fatalf("expected timestamps to be initialized")
	}
}

func TestNoopMiddleware_BeforeGenerate_RespectsCanceledContext(t *testing.T) {
	m := NoopMiddleware{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := m.BeforeGenerate(ctx, &GenerateMiddlewareEvent{}); err == nil {
		t.Fatalf("expected context cancellation error")
	}
}

func TestNoopMiddleware_BeforeAndAfterTool_NormalizesEvent(t *testing.T) {
	m := NoopMiddleware{}
	event := &ToolMiddlewareEvent{}
	if err := m.BeforeTool(context.Background(), event); err != nil {
		t.Fatalf("BeforeTool failed: %v", err)
	}
	if event.ToolCall == nil {
		t.Fatalf("expected tool call to be initialized")
	}
	if err := m.AfterTool(context.Background(), event); err != nil {
		t.Fatalf("AfterTool failed: %v", err)
	}
	if event.StartedAt.IsZero() || event.FinishedAt.IsZero() {
		t.Fatalf("expected timestamps to be initialized")
	}
}

func TestNoopMiddleware_OnError_FillsDefaults(t *testing.T) {
	m := NoopMiddleware{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	event := &ErrorMiddlewareEvent{ToolName: "calculator"}

	m.OnError(ctx, event)
	if event.Stage != "unknown" {
		t.Fatalf("expected stage default, got %q", event.Stage)
	}
	if event.Err == nil {
		t.Fatalf("expected error to be set from context")
	}
}

func TestNoopMiddleware_AfterGenerate_AllowsProvidedData(t *testing.T) {
	m := NoopMiddleware{}
	nowEvent := &GenerateMiddlewareEvent{
		Request:  &types.Request{SystemPrompt: "x"},
		Response: &types.Response{},
	}
	if err := m.AfterGenerate(context.Background(), nowEvent); err != nil {
		t.Fatalf("AfterGenerate failed: %v", err)
	}
	if nowEvent.Request.SystemPrompt != "x" {
		t.Fatalf("expected existing request to stay intact")
	}
}
