package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/PipeOpsHQ/agent-sdk-go/framework/llm"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/observe"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/state"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/tools"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/types"
	"github.com/google/uuid"
)

type Agent struct {
	provider        llm.Provider
	store           state.Store
	executionMode   ExecutionMode
	systemPrompt    string
	sessionID       string
	maxIterations   int
	maxOutputTokens int
	retryPolicy     RetryPolicy
	toolTimeout     time.Duration
	parallelTools   bool
	middlewares     []Middleware
	observer        observe.Sink

	mu        sync.RWMutex
	tools     map[string]tools.Tool
	sessionMu sync.Mutex
}

type Option func(*Agent)

type ExecutionMode string

const (
	ExecutionModeLocal       ExecutionMode = "local"
	ExecutionModeDistributed ExecutionMode = "distributed"
)

func WithSystemPrompt(prompt string) Option {
	return func(a *Agent) { a.systemPrompt = prompt }
}

func WithMaxIterations(max int) Option {
	return func(a *Agent) {
		if max > 0 {
			a.maxIterations = max
		}
	}
}

func WithMaxOutputTokens(max int) Option {
	return func(a *Agent) {
		if max > 0 {
			a.maxOutputTokens = max
		}
	}
}

// WithProviderRetries is kept for backward compatibility.
func WithProviderRetries(retries int) Option {
	return func(a *Agent) {
		if retries < 0 {
			return
		}
		policy := a.retryPolicy
		policy.MaxAttempts = retries + 1
		a.retryPolicy = normalizeRetryPolicy(policy)
	}
}

func WithRetryPolicy(policy RetryPolicy) Option {
	return func(a *Agent) {
		a.retryPolicy = normalizeRetryPolicy(policy)
	}
}

func WithToolTimeout(timeout time.Duration) Option {
	return func(a *Agent) {
		if timeout >= 0 {
			a.toolTimeout = timeout
		}
	}
}

func WithParallelToolCalls(enabled bool) Option {
	return func(a *Agent) { a.parallelTools = enabled }
}

func WithStore(store state.Store) Option {
	return func(a *Agent) { a.store = store }
}

func WithSessionID(sessionID string) Option {
	return func(a *Agent) {
		if sessionID != "" {
			a.sessionID = sessionID
		}
	}
}

func WithMiddleware(middlewares ...Middleware) Option {
	return func(a *Agent) {
		for _, middleware := range middlewares {
			if middleware != nil {
				a.middlewares = append(a.middlewares, middleware)
			}
		}
	}
}

func WithObserver(observer observe.Sink) Option {
	return func(a *Agent) {
		a.observer = observer
	}
}

func WithExecutionMode(mode ExecutionMode) Option {
	return func(a *Agent) {
		if mode != "" {
			a.executionMode = mode
		}
	}
}

func WithTool(tool tools.Tool) Option {
	return func(a *Agent) {
		if tool == nil {
			return
		}
		def := tool.Definition()
		if def.Name == "" {
			return
		}
		if a.tools == nil {
			a.tools = make(map[string]tools.Tool)
		}
		a.tools[def.Name] = tool
	}
}

func New(provider llm.Provider, opts ...Option) (*Agent, error) {
	if provider == nil {
		return nil, errors.New("provider is required")
	}

	a := &Agent{
		provider:      provider,
		executionMode: ExecutionModeLocal,
		maxIterations: 6,
		tools:         make(map[string]tools.Tool),
		retryPolicy:   defaultRetryPolicy(),
	}
	for _, opt := range opts {
		opt(a)
	}
	a.retryPolicy = normalizeRetryPolicy(a.retryPolicy)
	return a, nil
}

func (a *Agent) Run(ctx context.Context, input string) (string, error) {
	result, err := a.RunDetailed(ctx, input)
	if err != nil {
		return "", err
	}
	return result.Output, nil
}

func (a *Agent) RunDetailed(ctx context.Context, input string) (types.RunResult, error) {
	if input == "" {
		return types.RunResult{}, errors.New("input is required")
	}

	runID := uuid.NewString()
	sessionID := a.ensureSessionID()
	startedAt := time.Now().UTC()

	messages := []types.Message{
		{Role: types.RoleUser, Content: input},
	}
	usage := &types.Usage{}
	hasUsage := false
	events := []types.Event{
		{
			Type:      types.EventRunStarted,
			Timestamp: startedAt,
			RunID:     runID,
			SessionID: sessionID,
			Provider:  a.provider.Name(),
			Message:   "run started",
		},
	}
	a.emitRuntimeEvent(ctx, events[0])

	if err := a.saveRun(ctx, state.RunRecord{
		RunID:       runID,
		SessionID:   sessionID,
		Provider:    a.provider.Name(),
		Status:      "running",
		Input:       input,
		Output:      "",
		Messages:    append([]types.Message(nil), messages...),
		Usage:       nil,
		Metadata:    map[string]any{},
		Error:       "",
		CreatedAt:   &startedAt,
		UpdatedAt:   &startedAt,
		CompletedAt: nil,
	}); err != nil {
		return types.RunResult{}, fmt.Errorf("failed to persist run start: %w", err)
	}

	for i := 0; i < a.maxIterations; i++ {
		iteration := i + 1
		req := types.Request{
			SystemPrompt:    a.systemPrompt,
			Messages:        messages,
			Tools:           a.listToolDefinitions(),
			MaxOutputTokens: a.maxOutputTokens,
		}

		genStarted := time.Now().UTC()
		events = append(events, types.Event{
			Type:      types.EventBeforeGenerate,
			Timestamp: genStarted,
			RunID:     runID,
			SessionID: sessionID,
			Provider:  a.provider.Name(),
			Iteration: iteration,
		})
		a.emitRuntimeEvent(ctx, events[len(events)-1])
		genEvent := &GenerateMiddlewareEvent{
			RunID:      runID,
			SessionID:  sessionID,
			Provider:   a.provider.Name(),
			Iteration:  iteration,
			StartedAt:  genStarted,
			FinishedAt: genStarted,
			Request:    &req,
		}
		if err := a.runBeforeGenerate(ctx, genEvent); err != nil {
			if persistErr := a.markFailed(ctx, runID, sessionID, startedAt, input, messages, usageOrNil(usage, hasUsage), err); persistErr != nil {
				return types.RunResult{}, fmt.Errorf("middleware before-generate failed: %w (also failed to persist failure: %v)", err, persistErr)
			}
			return types.RunResult{}, fmt.Errorf("middleware before-generate failed: %w", err)
		}

		resp, err := a.generateWithRetry(ctx, req)
		if err != nil {
			a.notifyError(ctx, &ErrorMiddlewareEvent{
				RunID:     runID,
				SessionID: sessionID,
				Provider:  a.provider.Name(),
				Iteration: iteration,
				Stage:     "generate",
				Err:       err,
			})
			if persistErr := a.markFailed(ctx, runID, sessionID, startedAt, input, messages, usageOrNil(usage, hasUsage), err); persistErr != nil {
				return types.RunResult{}, fmt.Errorf("generation failed: %w (also failed to persist failure: %v)", err, persistErr)
			}
			return types.RunResult{}, fmt.Errorf("generation failed: %w", err)
		}

		genFinished := time.Now().UTC()
		genEvent.FinishedAt = genFinished
		genEvent.Response = &resp
		if err := a.runAfterGenerate(ctx, genEvent); err != nil {
			if persistErr := a.markFailed(ctx, runID, sessionID, startedAt, input, messages, usageOrNil(usage, hasUsage), err); persistErr != nil {
				return types.RunResult{}, fmt.Errorf("middleware after-generate failed: %w (also failed to persist failure: %v)", err, persistErr)
			}
			return types.RunResult{}, fmt.Errorf("middleware after-generate failed: %w", err)
		}
		events = append(events, types.Event{
			Type:      types.EventAfterGenerate,
			Timestamp: genFinished,
			RunID:     runID,
			SessionID: sessionID,
			Provider:  a.provider.Name(),
			Iteration: iteration,
		})
		a.emitRuntimeEvent(ctx, events[len(events)-1])

		if resp.Usage != nil {
			usage.InputTokens += resp.Usage.InputTokens
			usage.OutputTokens += resp.Usage.OutputTokens
			usage.TotalTokens += resp.Usage.TotalTokens
			hasUsage = true
		}

		modelMsg := resp.Message
		modelMsg.Role = types.RoleAssistant
		messages = append(messages, modelMsg)
		if err := a.saveProgress(ctx, runID, sessionID, startedAt, input, messages, usageOrNil(usage, hasUsage)); err != nil {
			return types.RunResult{}, fmt.Errorf("failed to persist run progress: %w", err)
		}

		if len(modelMsg.ToolCalls) == 0 {
			if modelMsg.Content == "" {
				emptyErr := errors.New("provider returned empty assistant content")
				a.notifyError(ctx, &ErrorMiddlewareEvent{
					RunID:     runID,
					SessionID: sessionID,
					Provider:  a.provider.Name(),
					Iteration: iteration,
					Stage:     "validate_response",
					Err:       emptyErr,
				})
				if persistErr := a.markFailed(ctx, runID, sessionID, startedAt, input, messages, usageOrNil(usage, hasUsage), emptyErr); persistErr != nil {
					return types.RunResult{}, fmt.Errorf("%w (also failed to persist failure: %v)", emptyErr, persistErr)
				}
				return types.RunResult{}, emptyErr
			}

			var finalUsage *types.Usage
			if hasUsage {
				finalUsage = usage
			}

			completedAt := time.Now().UTC()
			if err := a.saveRun(ctx, state.RunRecord{
				RunID:       runID,
				SessionID:   sessionID,
				Provider:    a.provider.Name(),
				Status:      "completed",
				Input:       input,
				Output:      modelMsg.Content,
				Messages:    append([]types.Message(nil), messages...),
				Usage:       copyUsage(finalUsage),
				Metadata:    map[string]any{},
				Error:       "",
				CreatedAt:   &startedAt,
				UpdatedAt:   &completedAt,
				CompletedAt: &completedAt,
			}); err != nil {
				return types.RunResult{}, fmt.Errorf("failed to persist run completion: %w", err)
			}

			events = append(events, types.Event{
				Type:      types.EventRunCompleted,
				Timestamp: completedAt,
				RunID:     runID,
				SessionID: sessionID,
				Provider:  a.provider.Name(),
				Iteration: iteration,
				Message:   "run completed",
			})
			a.emitRuntimeEvent(ctx, events[len(events)-1])

			return types.RunResult{
				Output:      modelMsg.Content,
				Messages:    append([]types.Message(nil), messages...),
				Usage:       finalUsage,
				Iterations:  iteration,
				Provider:    a.provider.Name(),
				RunID:       runID,
				SessionID:   sessionID,
				StartedAt:   &startedAt,
				CompletedAt: &completedAt,
				Events:      append([]types.Event(nil), events...),
			}, nil
		}

		toolMessages, toolEvents, err := a.executeToolCalls(ctx, runID, sessionID, iteration, modelMsg.ToolCalls)
		if err != nil {
			if persistErr := a.markFailed(ctx, runID, sessionID, startedAt, input, messages, usageOrNil(usage, hasUsage), err); persistErr != nil {
				return types.RunResult{}, fmt.Errorf("tool execution failed: %w (also failed to persist failure: %v)", err, persistErr)
			}
			return types.RunResult{}, fmt.Errorf("tool execution failed: %w", err)
		}
		events = append(events, toolEvents...)
		a.emitRuntimeEvents(ctx, toolEvents)
		messages = append(messages, toolMessages...)
		if err := a.saveProgress(ctx, runID, sessionID, startedAt, input, messages, usageOrNil(usage, hasUsage)); err != nil {
			return types.RunResult{}, fmt.Errorf("failed to persist tool progress: %w", err)
		}
	}

	iterationErr := fmt.Errorf("max iterations reached (%d)", a.maxIterations)
	a.notifyError(ctx, &ErrorMiddlewareEvent{
		RunID:     runID,
		SessionID: sessionID,
		Provider:  a.provider.Name(),
		Iteration: a.maxIterations,
		Stage:     "max_iterations",
		Err:       iterationErr,
	})
	if persistErr := a.markFailed(ctx, runID, sessionID, startedAt, input, messages, usageOrNil(usage, hasUsage), iterationErr); persistErr != nil {
		return types.RunResult{}, fmt.Errorf("%w (also failed to persist failure: %v)", iterationErr, persistErr)
	}
	return types.RunResult{}, iterationErr
}

func (a *Agent) generateWithRetry(ctx context.Context, req types.Request) (types.Response, error) {
	policy := normalizeRetryPolicy(a.retryPolicy)

	var lastErr error
	for attempt := 1; attempt <= policy.MaxAttempts; attempt++ {
		resp, err := a.provider.Generate(ctx, req)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if attempt == policy.MaxAttempts {
			break
		}

		backoff := policy.backoffForAttempt(attempt)
		select {
		case <-ctx.Done():
			return types.Response{}, ctx.Err()
		case <-time.After(backoff):
		}
	}

	return types.Response{}, fmt.Errorf("provider %q failed after %d attempt(s): %w", a.provider.Name(), policy.MaxAttempts, lastErr)
}

func (a *Agent) listToolDefinitions() []types.ToolDefinition {
	a.mu.RLock()
	defer a.mu.RUnlock()

	defs := make([]types.ToolDefinition, 0, len(a.tools))
	for _, tool := range a.tools {
		defs = append(defs, tool.Definition())
	}
	return defs
}

func (a *Agent) executeToolCalls(
	ctx context.Context,
	runID string,
	sessionID string,
	iteration int,
	calls []types.ToolCall,
) ([]types.Message, []types.Event, error) {
	toolset := a.snapshotTools()
	results := make([]types.Message, len(calls))
	eventSets := make([][]types.Event, len(calls))

	if a.parallelTools && len(calls) > 1 {
		var (
			wg       sync.WaitGroup
			errMu    sync.Mutex
			firstErr error
		)
		wg.Add(len(calls))
		for i, call := range calls {
			i, call := i, call
			go func() {
				defer wg.Done()
				msg, evs, err := a.executeOneToolCall(ctx, runID, sessionID, iteration, toolset, call)
				if err != nil {
					errMu.Lock()
					if firstErr == nil {
						firstErr = err
					}
					errMu.Unlock()
					return
				}
				results[i] = msg
				eventSets[i] = evs
			}()
		}
		wg.Wait()
		if firstErr != nil {
			return nil, nil, firstErr
		}
	} else {
		for i, call := range calls {
			msg, evs, err := a.executeOneToolCall(ctx, runID, sessionID, iteration, toolset, call)
			if err != nil {
				return nil, nil, err
			}
			results[i] = msg
			eventSets[i] = evs
		}
	}

	flatEvents := make([]types.Event, 0, len(calls)*2)
	for _, evs := range eventSets {
		flatEvents = append(flatEvents, evs...)
	}
	return results, flatEvents, nil
}

func (a *Agent) snapshotTools() map[string]tools.Tool {
	a.mu.RLock()
	defer a.mu.RUnlock()

	out := make(map[string]tools.Tool, len(a.tools))
	for name, tool := range a.tools {
		out[name] = tool
	}
	return out
}

func (a *Agent) executeOneToolCall(
	ctx context.Context,
	runID string,
	sessionID string,
	iteration int,
	toolset map[string]tools.Tool,
	call types.ToolCall,
) (types.Message, []types.Event, error) {
	toolCall := call
	startedAt := time.Now().UTC()
	events := []types.Event{
		{
			Type:       types.EventBeforeTool,
			Timestamp:  startedAt,
			RunID:      runID,
			SessionID:  sessionID,
			Provider:   a.provider.Name(),
			Iteration:  iteration,
			ToolName:   toolCall.Name,
			ToolCallID: toolCall.ID,
		},
	}

	toolEvent := &ToolMiddlewareEvent{
		RunID:      runID,
		SessionID:  sessionID,
		Provider:   a.provider.Name(),
		Iteration:  iteration,
		StartedAt:  startedAt,
		FinishedAt: startedAt,
		ToolCall:   &toolCall,
	}
	if err := a.runBeforeTool(ctx, toolEvent); err != nil {
		return types.Message{}, nil, err
	}

	tool, ok := toolset[toolCall.Name]
	var (
		payload any
		toolErr error
	)
	if !ok {
		toolErr = fmt.Errorf("tool %q not found", toolCall.Name)
		payload = map[string]any{"error": toolErr.Error()}
	} else {
		args := toolCall.Arguments
		if len(args) == 0 {
			args = json.RawMessage(`{}`)
		}

		toolCtx := ctx
		cancel := func() {}
		if a.toolTimeout > 0 {
			toolCtx, cancel = context.WithTimeout(ctx, a.toolTimeout)
		}
		out, err := tool.Execute(toolCtx, args)
		cancel()
		if err != nil {
			toolErr = err
			payload = map[string]any{"error": err.Error()}
		} else {
			payload = out
		}
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		encoded = []byte(fmt.Sprintf(`{"error":"failed to encode tool output","detail":%q}`, err.Error()))
	}
	result := types.Message{
		Role:       types.RoleTool,
		Name:       toolCall.Name,
		ToolCallID: toolCall.ID,
		Content:    string(encoded),
	}

	finishedAt := time.Now().UTC()
	toolEvent.FinishedAt = finishedAt
	toolEvent.Result = &result
	toolEvent.ToolError = toolErr
	if err := a.runAfterTool(ctx, toolEvent); err != nil {
		return types.Message{}, nil, err
	}
	if toolEvent.Result != nil {
		result = *toolEvent.Result
	}

	afterEvent := types.Event{
		Type:       types.EventAfterTool,
		Timestamp:  finishedAt,
		RunID:      runID,
		SessionID:  sessionID,
		Provider:   a.provider.Name(),
		Iteration:  iteration,
		ToolName:   toolCall.Name,
		ToolCallID: toolCall.ID,
	}
	if toolErr != nil {
		afterEvent.Error = toolErr.Error()
	}
	events = append(events, afterEvent)

	return result, events, nil
}

func (a *Agent) runBeforeGenerate(ctx context.Context, event *GenerateMiddlewareEvent) error {
	for _, middleware := range a.middlewares {
		if err := middleware.BeforeGenerate(ctx, event); err != nil {
			a.notifyError(ctx, &ErrorMiddlewareEvent{
				RunID:     event.RunID,
				SessionID: event.SessionID,
				Provider:  event.Provider,
				Iteration: event.Iteration,
				Stage:     "before_generate",
				Err:       err,
			})
			return err
		}
	}
	return nil
}

func (a *Agent) runAfterGenerate(ctx context.Context, event *GenerateMiddlewareEvent) error {
	for _, middleware := range a.middlewares {
		if err := middleware.AfterGenerate(ctx, event); err != nil {
			a.notifyError(ctx, &ErrorMiddlewareEvent{
				RunID:     event.RunID,
				SessionID: event.SessionID,
				Provider:  event.Provider,
				Iteration: event.Iteration,
				Stage:     "after_generate",
				Err:       err,
			})
			return err
		}
	}
	return nil
}

func (a *Agent) runBeforeTool(ctx context.Context, event *ToolMiddlewareEvent) error {
	for _, middleware := range a.middlewares {
		if err := middleware.BeforeTool(ctx, event); err != nil {
			a.notifyError(ctx, &ErrorMiddlewareEvent{
				RunID:     event.RunID,
				SessionID: event.SessionID,
				Provider:  event.Provider,
				Iteration: event.Iteration,
				Stage:     "before_tool",
				ToolName:  event.ToolCall.Name,
				Err:       err,
			})
			return err
		}
	}
	return nil
}

func (a *Agent) runAfterTool(ctx context.Context, event *ToolMiddlewareEvent) error {
	for _, middleware := range a.middlewares {
		if err := middleware.AfterTool(ctx, event); err != nil {
			a.notifyError(ctx, &ErrorMiddlewareEvent{
				RunID:     event.RunID,
				SessionID: event.SessionID,
				Provider:  event.Provider,
				Iteration: event.Iteration,
				Stage:     "after_tool",
				ToolName:  event.ToolCall.Name,
				Err:       err,
			})
			return err
		}
	}
	return nil
}

func (a *Agent) notifyError(ctx context.Context, event *ErrorMiddlewareEvent) {
	for _, middleware := range a.middlewares {
		func(m Middleware) {
			defer func() { _ = recover() }()
			m.OnError(ctx, event)
		}(middleware)
	}
}

func (a *Agent) ensureSessionID() string {
	a.sessionMu.Lock()
	defer a.sessionMu.Unlock()

	if a.sessionID == "" {
		a.sessionID = uuid.NewString()
	}
	return a.sessionID
}

func (a *Agent) saveProgress(
	ctx context.Context,
	runID string,
	sessionID string,
	createdAt time.Time,
	input string,
	messages []types.Message,
	usage *types.Usage,
) error {
	now := time.Now().UTC()
	return a.saveRun(ctx, state.RunRecord{
		RunID:     runID,
		SessionID: sessionID,
		Provider:  a.provider.Name(),
		Status:    "running",
		Input:     input,
		Output:    "",
		Messages:  append([]types.Message(nil), messages...),
		Usage:     copyUsage(usage),
		Metadata:  map[string]any{},
		Error:     "",
		CreatedAt: &createdAt,
		UpdatedAt: &now,
	})
}

func (a *Agent) markFailed(
	ctx context.Context,
	runID string,
	sessionID string,
	createdAt time.Time,
	input string,
	messages []types.Message,
	usage *types.Usage,
	runErr error,
) error {
	now := time.Now().UTC()
	errText := ""
	if runErr != nil {
		errText = runErr.Error()
	}
	err := a.saveRun(ctx, state.RunRecord{
		RunID:       runID,
		SessionID:   sessionID,
		Provider:    a.provider.Name(),
		Status:      "failed",
		Input:       input,
		Output:      "",
		Messages:    append([]types.Message(nil), messages...),
		Usage:       copyUsage(usage),
		Metadata:    map[string]any{},
		Error:       errText,
		CreatedAt:   &createdAt,
		UpdatedAt:   &now,
		CompletedAt: &now,
	})
	if err != nil {
		return err
	}
	a.emitRuntimeEvent(ctx, types.Event{
		Type:      types.EventRunFailed,
		Timestamp: now,
		RunID:     runID,
		SessionID: sessionID,
		Provider:  a.provider.Name(),
		Error:     errText,
		Message:   "run failed",
	})
	return nil
}

func (a *Agent) saveRun(ctx context.Context, run state.RunRecord) error {
	if a.store == nil {
		return nil
	}
	return a.store.SaveRun(ctx, run)
}

func usageOrNil(usage *types.Usage, hasUsage bool) *types.Usage {
	if !hasUsage || usage == nil {
		return nil
	}
	return copyUsage(usage)
}

func copyUsage(in *types.Usage) *types.Usage {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

func (a *Agent) emitRuntimeEvents(ctx context.Context, events []types.Event) {
	for _, event := range events {
		a.emitRuntimeEvent(ctx, event)
	}
}

func (a *Agent) emitRuntimeEvent(ctx context.Context, event types.Event) {
	if a == nil || a.observer == nil {
		return
	}
	_ = a.observer.Emit(ctx, observe.FromRuntimeEvent(event))
}
