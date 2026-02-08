package graph

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/nitrocode/ai-agents/framework/observe"
	"github.com/nitrocode/ai-agents/framework/state"
	"github.com/nitrocode/ai-agents/framework/types"
)

type Executor struct {
	graph     *Graph
	store     state.Store
	sessionID string
	observer  observe.Sink
	mode      ExecutionMode
}

type ExecutionMode string

const (
	ExecutionModeLocal       ExecutionMode = "local"
	ExecutionModeDistributed ExecutionMode = "distributed"
)

type ExecutorOption func(*Executor)

func WithStore(store state.Store) ExecutorOption {
	return func(e *Executor) { e.store = store }
}

func WithSessionID(sessionID string) ExecutorOption {
	return func(e *Executor) {
		if sessionID != "" {
			e.sessionID = sessionID
		}
	}
}

func WithObserver(observer observe.Sink) ExecutorOption {
	return func(e *Executor) {
		e.observer = observer
	}
}

func WithExecutionMode(mode ExecutionMode) ExecutorOption {
	return func(e *Executor) {
		if mode != "" {
			e.mode = mode
		}
	}
}

func NewExecutor(graph *Graph, opts ...ExecutorOption) (*Executor, error) {
	if graph == nil {
		return nil, fmt.Errorf("graph is required")
	}
	if err := graph.Compile(); err != nil {
		return nil, err
	}
	executor := &Executor{graph: graph, mode: ExecutionModeLocal}
	for _, opt := range opts {
		opt(executor)
	}
	return executor, nil
}

func (e *Executor) SetObserver(observer observe.Sink) {
	if e == nil {
		return
	}
	e.observer = observer
}

func (e *Executor) SetExecutionMode(mode ExecutionMode) {
	if e == nil || mode == "" {
		return
	}
	e.mode = mode
}

func (e *Executor) Run(ctx context.Context, input string) (types.RunResult, error) {
	if e == nil || e.graph == nil {
		return types.RunResult{}, fmt.Errorf("executor is not initialized")
	}
	now := time.Now().UTC()
	runID := uuid.NewString()
	sessionID := e.sessionID
	if sessionID == "" {
		sessionID = uuid.NewString()
	}

	runtimeState := newState(runID, sessionID, input, now)
	return e.execute(ctx, runtimeState, e.graph.startNodeID, 1)
}

func (e *Executor) Resume(ctx context.Context, runID string) (types.RunResult, error) {
	if e == nil || e.graph == nil {
		return types.RunResult{}, fmt.Errorf("executor is not initialized")
	}
	if runID == "" {
		return types.RunResult{}, fmt.Errorf("runID is required")
	}
	if e.store == nil {
		return types.RunResult{}, fmt.Errorf("state store is required for resume")
	}

	run, err := e.store.LoadRun(ctx, runID)
	if err != nil {
		return types.RunResult{}, err
	}

	checkpoint, err := e.store.LoadLatestCheckpoint(ctx, runID)
	if err != nil {
		if errors.Is(err, state.ErrNotFound) {
			if run.Status == "completed" {
				return types.RunResult{
					Output:      run.Output,
					Provider:    run.Provider,
					RunID:       run.RunID,
					SessionID:   run.SessionID,
					StartedAt:   run.CreatedAt,
					CompletedAt: run.CompletedAt,
				}, nil
			}
			return types.RunResult{}, fmt.Errorf("no checkpoints found for run %q", runID)
		}
		return types.RunResult{}, err
	}

	runtimeState, nextNodeID, err := restoreStateFromCheckpoint(checkpoint.State)
	if err != nil {
		return types.RunResult{}, err
	}
	if runtimeState.RunID == "" {
		runtimeState.RunID = run.RunID
	}
	if runtimeState.SessionID == "" {
		runtimeState.SessionID = run.SessionID
	}
	if runtimeState.Input == "" {
		runtimeState.Input = run.Input
	}
	if runtimeState.StartedAt.IsZero() {
		if run.CreatedAt != nil {
			runtimeState.StartedAt = run.CreatedAt.UTC()
		} else {
			runtimeState.StartedAt = time.Now().UTC()
		}
	}
	if runtimeState.UpdatedAt.IsZero() {
		runtimeState.UpdatedAt = time.Now().UTC()
	}

	if nextNodeID == "" {
		nextNodeID, err = e.selectNextNode(ctx, runtimeState.LastNodeID, &runtimeState)
		if err != nil {
			return types.RunResult{}, err
		}
	}
	if nextNodeID == "" {
		completedAt := time.Now().UTC()
		if err := e.persistRun(ctx, runtimeState, "completed", run.Output, nil, &completedAt); err != nil {
			return types.RunResult{}, err
		}
		return types.RunResult{
			Output:      run.Output,
			Provider:    e.graphProviderName(),
			RunID:       runtimeState.RunID,
			SessionID:   runtimeState.SessionID,
			StartedAt:   &runtimeState.StartedAt,
			CompletedAt: &completedAt,
		}, nil
	}

	return e.execute(ctx, runtimeState, nextNodeID, checkpoint.Seq+1)
}

func (e *Executor) execute(ctx context.Context, runtimeState State, startNodeID string, seq int) (types.RunResult, error) {
	if startNodeID == "" {
		return types.RunResult{}, fmt.Errorf("start node is empty")
	}
	if err := e.persistRun(ctx, runtimeState, "running", "", nil, nil); err != nil {
		return types.RunResult{}, err
	}

	nodeTrace := []string{}
	events := []types.Event{
		{
			Type:      types.EventRunStarted,
			Timestamp: time.Now().UTC(),
			RunID:     runtimeState.RunID,
			SessionID: runtimeState.SessionID,
			Provider:  e.graphProviderName(),
			Message:   "graph run started",
		},
	}
	e.emitRuntimeEvent(ctx, events[0])

	currentNodeID := startNodeID
	for currentNodeID != "" {
		node, ok := e.graph.nodes[currentNodeID]
		if !ok {
			err := fmt.Errorf("node %q does not exist", currentNodeID)
			_ = e.persistFailure(ctx, runtimeState, err)
			return types.RunResult{}, err
		}

		events = append(events, types.Event{
			Type:      types.EventGraphNodeStarted,
			Timestamp: time.Now().UTC(),
			RunID:     runtimeState.RunID,
			SessionID: runtimeState.SessionID,
			Provider:  e.graphProviderName(),
			ToolName:  currentNodeID,
		})
		e.emitRuntimeEvent(ctx, events[len(events)-1])

		if err := node.Execute(ctx, &runtimeState); err != nil {
			_ = e.persistFailure(ctx, runtimeState, err)
			return types.RunResult{}, fmt.Errorf("node %q failed: %w", currentNodeID, err)
		}

		runtimeState.LastNodeID = currentNodeID
		runtimeState.UpdatedAt = time.Now().UTC()
		nodeTrace = append(nodeTrace, currentNodeID)

		nextNodeID, err := e.selectNextNode(ctx, currentNodeID, &runtimeState)
		if err != nil {
			_ = e.persistFailure(ctx, runtimeState, err)
			return types.RunResult{}, err
		}
		if err := e.persistCheckpoint(ctx, runtimeState, seq, currentNodeID, nextNodeID); err != nil {
			_ = e.persistFailure(ctx, runtimeState, err)
			return types.RunResult{}, err
		}
		seq++

		events = append(events, types.Event{
			Type:      types.EventGraphNodeCompleted,
			Timestamp: time.Now().UTC(),
			RunID:     runtimeState.RunID,
			SessionID: runtimeState.SessionID,
			Provider:  e.graphProviderName(),
			ToolName:  currentNodeID,
		})
		e.emitRuntimeEvent(ctx, events[len(events)-1])

		if err := e.persistRun(ctx, runtimeState, "running", "", nil, nil); err != nil {
			return types.RunResult{}, err
		}
		currentNodeID = nextNodeID
	}

	completedAt := time.Now().UTC()
	output := runtimeState.Output
	if output == "" {
		if raw, ok := runtimeState.Data["output"]; ok {
			if s, ok := raw.(string); ok {
				output = s
			}
		}
	}
	if err := e.persistRun(ctx, runtimeState, "completed", output, nil, &completedAt); err != nil {
		return types.RunResult{}, err
	}
	events = append(events, types.Event{
		Type:      types.EventRunCompleted,
		Timestamp: completedAt,
		RunID:     runtimeState.RunID,
		SessionID: runtimeState.SessionID,
		Provider:  e.graphProviderName(),
		Message:   "graph run completed",
	})
	e.emitRuntimeEvent(ctx, events[len(events)-1])

	startedAt := runtimeState.StartedAt
	return types.RunResult{
		Output:      output,
		Iterations:  len(nodeTrace),
		Provider:    e.graphProviderName(),
		RunID:       runtimeState.RunID,
		SessionID:   runtimeState.SessionID,
		StartedAt:   &startedAt,
		CompletedAt: &completedAt,
		Events:      events,
		NodeTrace:   nodeTrace,
	}, nil
}

func (e *Executor) selectNextNode(ctx context.Context, from string, runtimeState *State) (string, error) {
	edges := e.graph.edges[from]
	for _, edge := range edges {
		if edge.Condition == nil {
			return edge.To, nil
		}
		ok, err := edge.Condition(ctx, runtimeState)
		if err != nil {
			return "", fmt.Errorf("edge %q -> %q condition failed: %w", edge.From, edge.To, err)
		}
		if ok {
			return edge.To, nil
		}
	}
	return "", nil
}

func (e *Executor) persistCheckpoint(ctx context.Context, runtimeState State, seq int, nodeID string, nextNodeID string) error {
	if e.store == nil {
		return nil
	}
	snapshot, err := runtimeState.snapshot(nextNodeID)
	if err != nil {
		return err
	}
	err = e.store.SaveCheckpoint(ctx, state.CheckpointRecord{
		RunID:     runtimeState.RunID,
		Seq:       seq,
		NodeID:    nodeID,
		State:     snapshot,
		CreatedAt: time.Now().UTC(),
	})
	if err != nil && !errors.Is(err, state.ErrConflict) {
		return err
	}
	if err == nil {
		_ = e.emitObserverEvent(ctx, observe.Event{
			RunID:     runtimeState.RunID,
			SessionID: runtimeState.SessionID,
			Kind:      observe.KindCheckpoint,
			Status:    observe.StatusCompleted,
			Name:      nodeID,
			Attributes: map[string]any{
				"seq":        seq,
				"nextNodeId": nextNodeID,
			},
		})
	}
	return nil
}

func (e *Executor) persistFailure(ctx context.Context, runtimeState State, runErr error) error {
	completedAt := time.Now().UTC()
	errText := ""
	if runErr != nil {
		errText = runErr.Error()
	}
	e.emitRuntimeEvent(ctx, types.Event{
		Type:      types.EventRunFailed,
		Timestamp: completedAt,
		RunID:     runtimeState.RunID,
		SessionID: runtimeState.SessionID,
		Provider:  e.graphProviderName(),
		Error:     errText,
		Message:   "graph run failed",
	})
	return e.persistRun(ctx, runtimeState, "failed", "", &errText, &completedAt)
}

func (e *Executor) persistRun(
	ctx context.Context,
	runtimeState State,
	status string,
	output string,
	errText *string,
	completedAt *time.Time,
) error {
	if e.store == nil {
		return nil
	}

	now := time.Now().UTC()
	metadata := map[string]any{
		"graph":      e.graph.Name(),
		"lastNodeId": runtimeState.LastNodeID,
	}
	errValue := ""
	if errText != nil {
		errValue = *errText
	}

	createdAt := runtimeState.StartedAt
	if createdAt.IsZero() {
		createdAt = now
	}
	updatedAt := runtimeState.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = now
	}
	if completedAt != nil {
		updatedAt = *completedAt
	}

	return e.store.SaveRun(ctx, state.RunRecord{
		RunID:       runtimeState.RunID,
		SessionID:   runtimeState.SessionID,
		Provider:    e.graphProviderName(),
		Status:      status,
		Input:       runtimeState.Input,
		Output:      output,
		Messages:    nil,
		Usage:       nil,
		Metadata:    metadata,
		Error:       errValue,
		CreatedAt:   &createdAt,
		UpdatedAt:   &updatedAt,
		CompletedAt: completedAt,
	})
}

func (e *Executor) graphProviderName() string {
	return "graph:" + e.graph.Name()
}

func (e *Executor) emitRuntimeEvent(ctx context.Context, event types.Event) {
	if e == nil || e.observer == nil {
		return
	}
	_ = e.observer.Emit(ctx, observe.FromRuntimeEvent(event))
}

func (e *Executor) emitObserverEvent(ctx context.Context, event observe.Event) error {
	if e == nil || e.observer == nil {
		return nil
	}
	return e.observer.Emit(ctx, event)
}
