package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/PipeOpsHQ/agent-sdk-go/delivery"
	"github.com/PipeOpsHQ/agent-sdk-go/llm"
	"github.com/PipeOpsHQ/agent-sdk-go/observe"
	"github.com/PipeOpsHQ/agent-sdk-go/state"
	"github.com/PipeOpsHQ/agent-sdk-go/tools"
	"github.com/PipeOpsHQ/agent-sdk-go/types"
	"github.com/google/uuid"
)

type Agent struct {
	provider            llm.Provider
	store               state.Store
	executionMode       ExecutionMode
	systemPrompt        string
	sessionID           string
	maxIterations       int
	maxOutputTokens     int
	maxInputTokens      int
	retryPolicy         RetryPolicy
	toolTimeout         time.Duration
	parallelTools       bool
	maxParallelTools    int
	middlewares         []Middleware
	observer            observe.Sink
	conversationHistory []types.Message
	contextManager      *ContextManager
	responseSchema      map[string]any

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

// WithMaxInputTokens sets the maximum input tokens for context management.
// This enables automatic trimming of conversation history to prevent
// exceeding provider rate limits. If not set, defaults to 25000 tokens.
func WithMaxInputTokens(max int) Option {
	return func(a *Agent) {
		if max > 0 {
			a.maxInputTokens = max
			a.contextManager = NewContextManager(max)
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

func WithMaxParallelTools(max int) Option {
	return func(a *Agent) {
		if max > 0 {
			a.maxParallelTools = max
		}
	}
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

// WithConversationHistory prepends previous conversation messages before
// the current user input. This enables multi-turn conversations where the
// LLM has context from prior exchanges in the same session.
func WithConversationHistory(messages []types.Message) Option {
	return func(a *Agent) {
		a.conversationHistory = messages
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

// WithResponseSchema sets a JSON schema that the LLM response must conform to.
// Providers that support structured output will enforce the schema natively.
func WithResponseSchema(schema map[string]any) Option {
	return func(a *Agent) { a.responseSchema = schema }
}

func New(provider llm.Provider, opts ...Option) (*Agent, error) {
	if provider == nil {
		return nil, errors.New("provider is required")
	}

	a := &Agent{
		provider:         provider,
		executionMode:    ExecutionModeLocal,
		maxIterations:    6,
		maxParallelTools: 10,
		maxInputTokens:   DefaultMaxInputTokens,
		tools:            make(map[string]tools.Tool),
		retryPolicy:      defaultRetryPolicy(),
		contextManager:   NewContextManager(DefaultMaxInputTokens),
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

// RunLite executes a single provider turn without persistence, middleware,
// context trimming, or tool execution overhead.
func (a *Agent) RunLite(ctx context.Context, input string) (string, error) {
	resp, err := a.RunLiteDetailed(ctx, input)
	if err != nil {
		return "", err
	}
	return resp.Message.Content, nil
}

// RunLiteDetailed is the low-overhead variant of RunDetailed.
func (a *Agent) RunLiteDetailed(ctx context.Context, input string) (types.Response, error) {
	if input == "" {
		return types.Response{}, errors.New("input is required")
	}
	messages := a.buildInitialMessages(input)
	req := types.Request{
		SystemPrompt:    a.systemPrompt,
		Messages:        messages,
		MaxOutputTokens: a.maxOutputTokens,
		ResponseSchema:  a.responseSchema,
	}
	resp, err := a.generateWithRetry(ctx, req)
	if err != nil {
		return types.Response{}, fmt.Errorf("generation failed: %w", err)
	}
	resp.Message.Role = types.RoleAssistant
	return resp, nil
}

// RunStream executes one generation turn and streams text chunks when the
// provider supports it. When streaming is unavailable, it falls back to RunLite.
func (a *Agent) RunStream(ctx context.Context, input string, onChunk func(types.StreamChunk) error) (types.RunResult, error) {
	if input == "" {
		return types.RunResult{}, errors.New("input is required")
	}
	if onChunk == nil {
		return types.RunResult{}, errors.New("onChunk is required")
	}

	messages := a.buildInitialMessages(input)
	runID := uuid.NewString()
	sessionID := a.ensureSessionID()
	start := time.Now().UTC()

	sp, ok := a.provider.(llm.StreamProvider)
	if !ok {
		resp, err := a.RunLiteDetailed(ctx, input)
		if err != nil {
			return types.RunResult{}, err
		}
		if err := onChunk(types.StreamChunk{Text: resp.Message.Content, Done: true}); err != nil {
			return types.RunResult{}, err
		}
		completed := time.Now().UTC()
		resp.Message.Role = types.RoleAssistant
		all := append(messages, resp.Message)
		return types.RunResult{
			Output:      resp.Message.Content,
			Messages:    all,
			Usage:       resp.Usage,
			Iterations:  1,
			Provider:    a.provider.Name(),
			RunID:       runID,
			SessionID:   sessionID,
			StartedAt:   &start,
			CompletedAt: &completed,
		}, nil
	}

	toolDefs := a.listToolDefinitions()
	trimmed := messages
	if a.contextManager != nil {
		trimmed = a.contextManager.TrimMessages(messages, a.systemPrompt, toolDefs, a.maxOutputTokens)
	}
	req := types.Request{
		SystemPrompt:    a.systemPrompt,
		Messages:        trimmed,
		Tools:           toolDefs,
		MaxOutputTokens: a.maxOutputTokens,
		ResponseSchema:  a.responseSchema,
	}
	resp, err := sp.GenerateStream(ctx, req, onChunk)
	if err != nil {
		return types.RunResult{}, fmt.Errorf("generation failed: %w", err)
	}
	resp.Message.Role = types.RoleAssistant
	all := append(messages, resp.Message)
	completed := time.Now().UTC()
	return types.RunResult{
		Output:      resp.Message.Content,
		Messages:    all,
		Usage:       resp.Usage,
		Iterations:  1,
		Provider:    a.provider.Name(),
		RunID:       runID,
		SessionID:   sessionID,
		StartedAt:   &start,
		CompletedAt: &completed,
	}, nil
}

func (a *Agent) RunDetailed(ctx context.Context, input string) (types.RunResult, error) {
	if input == "" {
		return types.RunResult{}, errors.New("input is required")
	}

	runID := uuid.NewString()
	sessionID := a.ensureSessionID()
	startedAt := time.Now().UTC()
	metadata := runMetadataFromContext(ctx)

	messages := a.buildInitialMessages(input)
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
		Metadata:    metadata,
		Error:       "",
		CreatedAt:   &startedAt,
		UpdatedAt:   &startedAt,
		CompletedAt: nil,
	}); err != nil {
		return types.RunResult{}, fmt.Errorf("failed to persist run start: %w", err)
	}

	for i := 0; i < a.maxIterations; i++ {
		iteration := i + 1

		// Apply context trimming to prevent exceeding token limits
		toolDefs := a.listToolDefinitions()
		trimmedMessages := a.contextManager.TrimMessages(
			messages,
			a.systemPrompt,
			toolDefs,
			a.maxOutputTokens, // Reserve space for expected output
		)

		req := types.Request{
			SystemPrompt:    a.systemPrompt,
			Messages:        trimmedMessages,
			Tools:           toolDefs,
			MaxOutputTokens: a.maxOutputTokens,
			ResponseSchema:  a.responseSchema,
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

		if len(modelMsg.ToolCalls) == 0 && modelMsg.Content == "" {
			// Retry up to 2 times on empty content — transient provider issue
			const maxEmptyRetries = 2
			for emptyRetry := 1; emptyRetry <= maxEmptyRetries; emptyRetry++ {
				log.Printf("⚠️  Provider returned empty content (attempt %d/%d), retrying...", emptyRetry, maxEmptyRetries)
				// Remove the empty assistant message before retrying
				messages = messages[:len(messages)-1]
				time.Sleep(time.Duration(emptyRetry) * 500 * time.Millisecond)
				retryResp, retryErr := a.generateWithRetry(ctx, req)
				if retryErr != nil {
					continue
				}
				retryMsg := retryResp.Message
				retryMsg.Role = types.RoleAssistant
				if retryResp.Usage != nil {
					usage.InputTokens += retryResp.Usage.InputTokens
					usage.OutputTokens += retryResp.Usage.OutputTokens
					usage.TotalTokens += retryResp.Usage.TotalTokens
					hasUsage = true
				}
				if retryMsg.Content != "" || len(retryMsg.ToolCalls) > 0 {
					modelMsg = retryMsg
					messages = append(messages, modelMsg)
					break
				}
			}
			// If still empty after retries, fail
			if modelMsg.Content == "" && len(modelMsg.ToolCalls) == 0 {
				emptyErr := errors.New("provider returned empty assistant content after retries")
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
		}

		if len(modelMsg.ToolCalls) == 0 {
			// Validate response against schema if set
			if len(a.responseSchema) > 0 && modelMsg.Content != "" {
				if !json.Valid([]byte(modelMsg.Content)) {
					log.Printf("⚠️  Response is not valid JSON, retrying with schema hint...")
					messages = append(messages, types.Message{
						Role:    types.RoleUser,
						Content: "Your response was not valid JSON. Please respond with ONLY a valid JSON object matching the requested schema.",
					})
					continue
				}
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
				Metadata:    metadata,
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
	rateLimitAttempts := 0

	for attempt := 1; attempt <= policy.MaxAttempts; attempt++ {
		resp, err := a.provider.Generate(ctx, req)
		if err == nil {
			return resp, nil
		}
		lastErr = err

		// Check if this is a rate limit error
		if IsRateLimitError(err) {
			rateLimitAttempts++
			if rateLimitAttempts > policy.RateLimitMaxAttempts {
				return types.Response{}, fmt.Errorf("provider %q rate limited after %d attempt(s): %w", a.provider.Name(), rateLimitAttempts, lastErr)
			}

			// Use longer backoff for rate limits
			backoff := policy.rateLimitBackoffForAttempt(rateLimitAttempts)
			select {
			case <-ctx.Done():
				return types.Response{}, ctx.Err()
			case <-time.After(backoff):
			}
			// Don't count rate limit retries against regular attempts
			attempt--
			continue
		}

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
		maxConcurrent := a.maxParallelTools
		if maxConcurrent <= 0 {
			maxConcurrent = 10
		}
		if maxConcurrent > len(calls) {
			maxConcurrent = len(calls)
		}
		sem := make(chan struct{}, maxConcurrent)
		var (
			wg       sync.WaitGroup
			errMu    sync.Mutex
			firstErr error
		)
		wg.Add(len(calls))
		for i, call := range calls {
			i, call := i, call
			sem <- struct{}{} // acquire
			go func() {
				defer wg.Done()
				defer func() { <-sem }() // release
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
	metadata := runMetadataFromContext(ctx)
	return a.saveRun(ctx, state.RunRecord{
		RunID:     runID,
		SessionID: sessionID,
		Provider:  a.provider.Name(),
		Status:    "running",
		Input:     input,
		Output:    "",
		Messages:  append([]types.Message(nil), messages...),
		Usage:     copyUsage(usage),
		Metadata:  metadata,
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
	metadata := runMetadataFromContext(ctx)
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
		Metadata:    metadata,
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

func runMetadataFromContext(ctx context.Context) map[string]any {
	md := map[string]any{}
	if target := delivery.FromContext(ctx); target != nil {
		md["replyTo"] = map[string]any{
			"channel":     target.Channel,
			"destination": target.Destination,
			"threadId":    target.ThreadID,
			"userId":      target.UserID,
			"metadata":    target.Metadata,
		}
	}
	if turnType := delivery.TurnTypeFromContext(ctx); turnType != "" {
		md["turn_type"] = turnType
	}
	if parentRunID := delivery.ParentRunIDFromContext(ctx); parentRunID != "" {
		md["parent_run_id"] = parentRunID
	}
	return md
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

func (a *Agent) buildInitialMessages(input string) []types.Message {
	var messages []types.Message
	if len(a.conversationHistory) > 0 {
		messages = make([]types.Message, 0, len(a.conversationHistory)+1)
		for _, m := range a.conversationHistory {
			if m.Role == types.RoleUser || (m.Role == types.RoleAssistant && m.Content != "" && len(m.ToolCalls) == 0) {
				messages = append(messages, types.Message{Role: m.Role, Content: m.Content})
			}
		}
	}
	messages = append(messages, types.Message{Role: types.RoleUser, Content: input})
	return messages
}

// RegisterTool adds a tool to the agent at runtime.
func (a *Agent) RegisterTool(tool tools.Tool) {
	if a == nil || tool == nil {
		return
	}
	def := tool.Definition()
	if def.Name == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.tools == nil {
		a.tools = make(map[string]tools.Tool)
	}
	a.tools[def.Name] = tool
}

// UnregisterTool removes a tool from the agent.
func (a *Agent) UnregisterTool(name string) {
	if a == nil || name == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.tools, name)
}

// ListTools returns the names of all registered tools.
func (a *Agent) ListTools() []string {
	if a == nil {
		return nil
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	names := make([]string, 0, len(a.tools))
	for name := range a.tools {
		names = append(names, name)
	}
	return names
}
