package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nitrocode/ai-agents/framework/llm"
	"github.com/nitrocode/ai-agents/framework/state"
	"github.com/nitrocode/ai-agents/framework/tools"
	"github.com/nitrocode/ai-agents/framework/types"
)

type mockProvider struct {
	calls int
}

func (m *mockProvider) Name() string {
	return "mock"
}

func (m *mockProvider) Capabilities() llm.Capabilities {
	return llm.Capabilities{Tools: true}
}

func (m *mockProvider) Generate(ctx context.Context, req types.Request) (types.Response, error) {
	_ = ctx
	m.calls++
	if m.calls == 1 {
		return types.Response{
			Message: types.Message{
				Role: types.RoleAssistant,
				ToolCalls: []types.ToolCall{
					{
						ID:        "call-1",
						Name:      "test_tool",
						Arguments: json.RawMessage(`{"value":"hello"}`),
					},
				},
			},
		}, nil
	}

	// Ensure the tool response is in the conversation.
	last := req.Messages[len(req.Messages)-1]
	if last.Role != types.RoleTool {
		t := "expected last message to be tool response"
		return types.Response{Message: types.Message{Role: types.RoleAssistant, Content: t}}, nil
	}

	return types.Response{
		Message: types.Message{
			Role:    types.RoleAssistant,
			Content: "done",
		},
	}, nil
}

func TestAgent_Run_UsesToolCalls(t *testing.T) {
	mock := &mockProvider{}

	testTool := tools.NewFuncTool(
		"test_tool",
		"test tool",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"value": map[string]any{"type": "string"},
			},
		},
		func(ctx context.Context, args json.RawMessage) (any, error) {
			_ = ctx
			var in struct {
				Value string `json:"value"`
			}
			_ = json.Unmarshal(args, &in)
			return map[string]any{"echo": in.Value}, nil
		},
	)

	a, err := New(mock, WithTool(testTool), WithMaxIterations(3))
	if err != nil {
		t.Fatalf("failed to build agent: %v", err)
	}

	out, err := a.Run(context.Background(), "run")
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if out != "done" {
		t.Fatalf("unexpected output: %q", out)
	}
	if mock.calls != 2 {
		t.Fatalf("expected 2 provider calls, got %d", mock.calls)
	}
}

type flakyProvider struct {
	calls int
}

func (f *flakyProvider) Name() string { return "flaky" }

func (f *flakyProvider) Capabilities() llm.Capabilities {
	return llm.Capabilities{}
}

func (f *flakyProvider) Generate(ctx context.Context, req types.Request) (types.Response, error) {
	_ = ctx
	_ = req
	f.calls++
	if f.calls == 1 {
		return types.Response{}, errors.New("transient provider failure")
	}
	return types.Response{
		Message: types.Message{
			Role:    types.RoleAssistant,
			Content: "retried-ok",
		},
	}, nil
}

func TestAgent_Run_RetriesProviderFailures(t *testing.T) {
	p := &flakyProvider{}
	a, err := New(p, WithProviderRetries(1))
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	out, err := a.Run(context.Background(), "hello")
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if out != "retried-ok" {
		t.Fatalf("unexpected output: %q", out)
	}
	if p.calls != 2 {
		t.Fatalf("expected 2 calls with retry, got %d", p.calls)
	}
}

func TestAgent_Run_FailsWithoutRetry(t *testing.T) {
	p := &flakyProvider{}
	a, err := New(p, WithProviderRetries(0))
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	_, err = a.Run(context.Background(), "hello")
	if err == nil {
		t.Fatalf("expected error when no retries are configured")
	}
	if p.calls != 1 {
		t.Fatalf("expected 1 call without retry, got %d", p.calls)
	}
}

type timeoutProvider struct {
	calls       int
	sawDeadline bool
}

func (p *timeoutProvider) Name() string { return "timeout-checker" }

func (p *timeoutProvider) Capabilities() llm.Capabilities {
	return llm.Capabilities{Tools: true}
}

func (p *timeoutProvider) Generate(ctx context.Context, req types.Request) (types.Response, error) {
	_ = ctx
	p.calls++
	if p.calls == 1 {
		return types.Response{
			Message: types.Message{
				Role: types.RoleAssistant,
				ToolCalls: []types.ToolCall{
					{
						ID:        "slow-1",
						Name:      "slow_tool",
						Arguments: json.RawMessage(`{}`),
					},
				},
			},
		}, nil
	}

	last := req.Messages[len(req.Messages)-1]
	p.sawDeadline = last.Role == types.RoleTool && strings.Contains(last.Content, "deadline exceeded")
	return types.Response{
		Message: types.Message{
			Role:    types.RoleAssistant,
			Content: "timeout-checked",
		},
	}, nil
}

func TestAgent_Run_RespectsToolTimeout(t *testing.T) {
	slowTool := tools.NewFuncTool(
		"slow_tool",
		"tool that runs longer than timeout",
		map[string]any{"type": "object"},
		func(ctx context.Context, args json.RawMessage) (any, error) {
			_ = args
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(75 * time.Millisecond):
				return map[string]any{"ok": true}, nil
			}
		},
	)

	p := &timeoutProvider{}
	a, err := New(
		p,
		WithTool(slowTool),
		WithToolTimeout(10*time.Millisecond),
		WithMaxIterations(3),
	)
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	out, err := a.Run(context.Background(), "run slow tool")
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if out != "timeout-checked" {
		t.Fatalf("unexpected output: %q", out)
	}
	if !p.sawDeadline {
		t.Fatalf("expected provider to observe tool timeout in tool message")
	}
}

type usageProvider struct {
	calls int
}

func (p *usageProvider) Name() string { return "usage-provider" }

func (p *usageProvider) Capabilities() llm.Capabilities {
	return llm.Capabilities{}
}

func (p *usageProvider) Generate(ctx context.Context, req types.Request) (types.Response, error) {
	_ = ctx
	_ = req
	p.calls++
	return types.Response{
		Message: types.Message{
			Role:    types.RoleAssistant,
			Content: "usage-ok",
		},
		Usage: &types.Usage{
			InputTokens:  10,
			OutputTokens: 5,
			TotalTokens:  15,
		},
	}, nil
}

func TestAgent_RunDetailed_ReturnsMetadata(t *testing.T) {
	p := &usageProvider{}
	a, err := New(p)
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	result, err := a.RunDetailed(context.Background(), "hello")
	if err != nil {
		t.Fatalf("run detailed failed: %v", err)
	}
	if result.Output != "usage-ok" {
		t.Fatalf("unexpected output: %q", result.Output)
	}
	if result.Provider != "usage-provider" {
		t.Fatalf("unexpected provider: %q", result.Provider)
	}
	if result.Iterations != 1 {
		t.Fatalf("expected 1 iteration, got %d", result.Iterations)
	}
	if result.Usage == nil || result.Usage.TotalTokens != 15 {
		t.Fatalf("unexpected usage: %#v", result.Usage)
	}
	if len(result.Messages) < 2 {
		t.Fatalf("expected conversation messages in result, got %d", len(result.Messages))
	}
	if result.RunID == "" || result.SessionID == "" {
		t.Fatalf("expected run/session ids in result: %#v", result)
	}
	if result.StartedAt == nil || result.CompletedAt == nil {
		t.Fatalf("expected started/completed timestamps in result")
	}
	if len(result.Events) == 0 {
		t.Fatalf("expected execution events in result")
	}
}

type memoryStateStore struct {
	mu          sync.Mutex
	runs        map[string]state.RunRecord
	checkpoints map[string][]state.CheckpointRecord
}

func newMemoryStateStore() *memoryStateStore {
	return &memoryStateStore{
		runs:        map[string]state.RunRecord{},
		checkpoints: map[string][]state.CheckpointRecord{},
	}
}

func (m *memoryStateStore) SaveRun(ctx context.Context, run state.RunRecord) error {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	m.runs[run.RunID] = run
	return nil
}

func (m *memoryStateStore) LoadRun(ctx context.Context, runID string) (state.RunRecord, error) {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	run, ok := m.runs[runID]
	if !ok {
		return state.RunRecord{}, state.ErrNotFound
	}
	return run, nil
}

func (m *memoryStateStore) ListRuns(ctx context.Context, query state.ListRunsQuery) ([]state.RunRecord, error) {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]state.RunRecord, 0, len(m.runs))
	for _, run := range m.runs {
		if query.SessionID != "" && run.SessionID != query.SessionID {
			continue
		}
		if query.Status != "" && run.Status != query.Status {
			continue
		}
		out = append(out, run)
	}
	return out, nil
}

func (m *memoryStateStore) SaveCheckpoint(ctx context.Context, checkpoint state.CheckpointRecord) error {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	m.checkpoints[checkpoint.RunID] = append(m.checkpoints[checkpoint.RunID], checkpoint)
	return nil
}

func (m *memoryStateStore) LoadLatestCheckpoint(ctx context.Context, runID string) (state.CheckpointRecord, error) {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	list := m.checkpoints[runID]
	if len(list) == 0 {
		return state.CheckpointRecord{}, state.ErrNotFound
	}
	return list[len(list)-1], nil
}

func (m *memoryStateStore) ListCheckpoints(ctx context.Context, runID string, limit int) ([]state.CheckpointRecord, error) {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	list := append([]state.CheckpointRecord(nil), m.checkpoints[runID]...)
	if limit > 0 && len(list) > limit {
		list = list[:limit]
	}
	return list, nil
}

func (m *memoryStateStore) Close() error { return nil }

type simpleProvider struct {
	fail bool
}

func (p *simpleProvider) Name() string { return "simple" }

func (p *simpleProvider) Capabilities() llm.Capabilities {
	return llm.Capabilities{}
}

func (p *simpleProvider) Generate(ctx context.Context, req types.Request) (types.Response, error) {
	_ = ctx
	_ = req
	if p.fail {
		return types.Response{}, errors.New("provider failure")
	}
	return types.Response{
		Message: types.Message{
			Role:    types.RoleAssistant,
			Content: "ok",
		},
	}, nil
}

func TestAgent_RunDetailed_PersistsCompletedRun(t *testing.T) {
	store := newMemoryStateStore()
	a, err := New(
		&simpleProvider{},
		WithStore(store),
		WithSessionID("session-fixed"),
	)
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	result, err := a.RunDetailed(context.Background(), "hello")
	if err != nil {
		t.Fatalf("RunDetailed failed: %v", err)
	}
	if result.RunID == "" || result.SessionID == "" {
		t.Fatalf("expected run and session ids in result: %#v", result)
	}

	run, err := store.LoadRun(context.Background(), result.RunID)
	if err != nil {
		t.Fatalf("expected persisted run: %v", err)
	}
	if run.Status != "completed" {
		t.Fatalf("expected completed status, got %q", run.Status)
	}
	if run.Output != "ok" {
		t.Fatalf("unexpected persisted output: %q", run.Output)
	}
	if run.SessionID != "session-fixed" {
		t.Fatalf("unexpected session id: %q", run.SessionID)
	}
}

func TestAgent_RunDetailed_PersistsFailedRun(t *testing.T) {
	store := newMemoryStateStore()
	a, err := New(
		&simpleProvider{fail: true},
		WithStore(store),
		WithSessionID("session-fail"),
	)
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	_, err = a.RunDetailed(context.Background(), "hello")
	if err == nil {
		t.Fatalf("expected run to fail")
	}

	runs, err := store.ListRuns(context.Background(), state.ListRunsQuery{SessionID: "session-fail"})
	if err != nil {
		t.Fatalf("ListRuns failed: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("expected one run persisted, got %d", len(runs))
	}
	if runs[0].Status != "failed" {
		t.Fatalf("expected failed status, got %q", runs[0].Status)
	}
	if runs[0].Error == "" {
		t.Fatalf("expected failure error to be persisted")
	}
}

type retryPolicyProvider struct {
	mu       sync.Mutex
	failTill int
	calls    int
}

func (p *retryPolicyProvider) Name() string { return "retry-policy-provider" }

func (p *retryPolicyProvider) Capabilities() llm.Capabilities { return llm.Capabilities{} }

func (p *retryPolicyProvider) Generate(ctx context.Context, req types.Request) (types.Response, error) {
	_ = ctx
	_ = req
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	if p.calls <= p.failTill {
		return types.Response{}, errors.New("transient error")
	}
	return types.Response{
		Message: types.Message{
			Role:    types.RoleAssistant,
			Content: "ok-after-retries",
		},
	}, nil
}

func TestAgent_Run_UsesStructuredRetryPolicy(t *testing.T) {
	p := &retryPolicyProvider{failTill: 2}
	a, err := New(
		p,
		WithRetryPolicy(RetryPolicy{
			MaxAttempts: 3,
			BaseBackoff: time.Millisecond,
			MaxBackoff:  2 * time.Millisecond,
		}),
	)
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	out, err := a.Run(context.Background(), "hello")
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if out != "ok-after-retries" {
		t.Fatalf("unexpected output: %q", out)
	}
	if p.calls != 3 {
		t.Fatalf("expected 3 calls, got %d", p.calls)
	}
}

func TestAgent_WithProviderRetries_CompatibilityWrapper(t *testing.T) {
	p := &retryPolicyProvider{failTill: 1}
	a, err := New(p, WithProviderRetries(1))
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	if a.retryPolicy.MaxAttempts != 2 {
		t.Fatalf("expected MaxAttempts=2 via compatibility wrapper, got %d", a.retryPolicy.MaxAttempts)
	}

	out, err := a.Run(context.Background(), "hello")
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if out != "ok-after-retries" {
		t.Fatalf("unexpected output: %q", out)
	}
}

type middlewareProbe struct {
	NoopMiddleware
	beforeGenerate func(event *GenerateMiddlewareEvent) error
	afterGenerate  func(event *GenerateMiddlewareEvent) error
	beforeTool     func(event *ToolMiddlewareEvent) error
	afterTool      func(event *ToolMiddlewareEvent) error
	onError        func(event *ErrorMiddlewareEvent)
}

func (m *middlewareProbe) BeforeGenerate(ctx context.Context, event *GenerateMiddlewareEvent) error {
	_ = ctx
	if m.beforeGenerate != nil {
		return m.beforeGenerate(event)
	}
	return nil
}

func (m *middlewareProbe) AfterGenerate(ctx context.Context, event *GenerateMiddlewareEvent) error {
	_ = ctx
	if m.afterGenerate != nil {
		return m.afterGenerate(event)
	}
	return nil
}

func (m *middlewareProbe) BeforeTool(ctx context.Context, event *ToolMiddlewareEvent) error {
	_ = ctx
	if m.beforeTool != nil {
		return m.beforeTool(event)
	}
	return nil
}

func (m *middlewareProbe) AfterTool(ctx context.Context, event *ToolMiddlewareEvent) error {
	_ = ctx
	if m.afterTool != nil {
		return m.afterTool(event)
	}
	return nil
}

func (m *middlewareProbe) OnError(ctx context.Context, event *ErrorMiddlewareEvent) {
	_ = ctx
	if m.onError != nil {
		m.onError(event)
	}
}

type inspectProvider struct {
	mu      sync.Mutex
	lastReq types.Request
	calls   int
}

func (p *inspectProvider) Name() string { return "inspect-provider" }

func (p *inspectProvider) Capabilities() llm.Capabilities { return llm.Capabilities{} }

func (p *inspectProvider) Generate(ctx context.Context, req types.Request) (types.Response, error) {
	_ = ctx
	p.mu.Lock()
	p.lastReq = req
	p.calls++
	p.mu.Unlock()
	return types.Response{
		Message: types.Message{
			Role:    types.RoleAssistant,
			Content: "provider-output",
		},
	}, nil
}

func TestAgent_Middleware_CanMutateGenerateRequestAndResponse(t *testing.T) {
	p := &inspectProvider{}
	m := &middlewareProbe{
		beforeGenerate: func(event *GenerateMiddlewareEvent) error {
			event.Request.SystemPrompt = "from-middleware"
			return nil
		},
		afterGenerate: func(event *GenerateMiddlewareEvent) error {
			event.Response.Message.Content = "mutated-output"
			return nil
		},
	}
	a, err := New(p, WithMiddleware(m))
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	out, err := a.Run(context.Background(), "hello")
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if out != "mutated-output" {
		t.Fatalf("expected middleware-mutated output, got %q", out)
	}
	if p.lastReq.SystemPrompt != "from-middleware" {
		t.Fatalf("expected request mutation from middleware, got %q", p.lastReq.SystemPrompt)
	}
}

type toolFlowProvider struct {
	calls int
}

func (p *toolFlowProvider) Name() string { return "tool-flow-provider" }

func (p *toolFlowProvider) Capabilities() llm.Capabilities {
	return llm.Capabilities{Tools: true}
}

func (p *toolFlowProvider) Generate(ctx context.Context, req types.Request) (types.Response, error) {
	_ = ctx
	p.calls++
	if p.calls == 1 {
		return types.Response{
			Message: types.Message{
				Role: types.RoleAssistant,
				ToolCalls: []types.ToolCall{
					{
						ID:        "tool-call-1",
						Name:      "echo_tool",
						Arguments: json.RawMessage(`{"value":"orig"}`),
					},
				},
			},
		}, nil
	}

	last := req.Messages[len(req.Messages)-1]
	if last.Role != types.RoleTool {
		return types.Response{}, fmt.Errorf("expected tool message as last message")
	}
	return types.Response{
		Message: types.Message{
			Role:    types.RoleAssistant,
			Content: last.Content,
		},
	}, nil
}

func TestAgent_Middleware_CanMutateToolCallAndResult(t *testing.T) {
	echoTool := tools.NewFuncTool(
		"echo_tool",
		"echo",
		map[string]any{"type": "object"},
		func(ctx context.Context, args json.RawMessage) (any, error) {
			_ = ctx
			var in struct {
				Value string `json:"value"`
			}
			_ = json.Unmarshal(args, &in)
			return map[string]any{"value": in.Value}, nil
		},
	)

	m := &middlewareProbe{
		beforeTool: func(event *ToolMiddlewareEvent) error {
			event.ToolCall.Arguments = json.RawMessage(`{"value":"from-before-tool"}`)
			return nil
		},
		afterTool: func(event *ToolMiddlewareEvent) error {
			event.Result.Content = `{"value":"from-after-tool"}`
			return nil
		},
	}

	a, err := New(
		&toolFlowProvider{},
		WithTool(echoTool),
		WithMiddleware(m),
		WithMaxIterations(3),
	)
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	out, err := a.Run(context.Background(), "run")
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if !strings.Contains(out, "from-after-tool") {
		t.Fatalf("expected tool result mutated by middleware, got %q", out)
	}
}

func TestAgent_Middleware_OnErrorIsCalled(t *testing.T) {
	var (
		mu        sync.Mutex
		errorSeen *ErrorMiddlewareEvent
	)
	m := &middlewareProbe{
		beforeGenerate: func(event *GenerateMiddlewareEvent) error {
			return errors.New("before-generate-failure")
		},
		onError: func(event *ErrorMiddlewareEvent) {
			mu.Lock()
			defer mu.Unlock()
			copyEvent := *event
			errorSeen = &copyEvent
		},
	}

	a, err := New(&inspectProvider{}, WithMiddleware(m))
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	_, err = a.Run(context.Background(), "hello")
	if err == nil {
		t.Fatalf("expected middleware failure")
	}
	if !strings.Contains(err.Error(), "middleware before-generate failed") {
		t.Fatalf("unexpected error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if errorSeen == nil {
		t.Fatalf("expected OnError to be called")
	}
	if errorSeen.Stage != "before_generate" {
		t.Fatalf("unexpected error stage: %q", errorSeen.Stage)
	}
}
