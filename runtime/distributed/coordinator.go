package distributed

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/PipeOpsHQ/agent-sdk-go/observe"
	"github.com/PipeOpsHQ/agent-sdk-go/runtime/queue"
	"github.com/PipeOpsHQ/agent-sdk-go/state"
	"github.com/google/uuid"
)

type DistributedConfig struct {
	Queue  QueueConfig
	Policy RuntimePolicy
}

type QueueConfig struct {
	Name   string
	Prefix string
}

type WorkerConfig struct {
	WorkerID string
	Capacity int
}

type Coordinator interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	SubmitRun(ctx context.Context, req SubmitRequest) (SubmitResult, error)
	CancelRun(ctx context.Context, runID string) error
	RequeueRun(ctx context.Context, runID string) error
	QueueStats(ctx context.Context) (queue.Stats, error)
	ListWorkers(ctx context.Context, limit int) ([]WorkerHeartbeat, error)
	ListRunAttempts(ctx context.Context, runID string, limit int) ([]AttemptRecord, error)
	ListQueueEvents(ctx context.Context, runID string, limit int) ([]QueueEvent, error)
	ListDLQ(ctx context.Context, limit int) ([]queue.Delivery, error)
}

type coordinator struct {
	store     state.Store
	attempts  AttemptStore
	queue     queue.Queue
	observer  observe.Sink
	policy    RuntimePolicy
	queueName string
	mu        sync.Mutex
	cancelled map[string]time.Time // value = when cancelled; entries expire after 1 hour
	started   bool
	cancel    context.CancelFunc
	done      chan struct{}
}

func NewCoordinator(store state.Store, attempts AttemptStore, queueStore queue.Queue, observer observe.Sink, cfg DistributedConfig) (Coordinator, error) {
	if store == nil {
		return nil, fmt.Errorf("state store is required")
	}
	if attempts == nil {
		return nil, fmt.Errorf("attempt store is required")
	}
	if queueStore == nil {
		return nil, fmt.Errorf("queue is required")
	}
	policy := NormalizeRuntimePolicy(cfg.Policy)
	queueName := strings.TrimSpace(cfg.Queue.Name)
	if queueName == "" {
		queueName = "runs"
	}
	return &coordinator{
		store:     store,
		attempts:  attempts,
		queue:     queueStore,
		observer:  observer,
		policy:    policy,
		queueName: queueName,
		cancelled: map[string]time.Time{},
	}, nil
}

func (c *coordinator) Start(ctx context.Context) error {
	if c == nil {
		return fmt.Errorf("coordinator is nil")
	}
	c.mu.Lock()
	if c.started {
		c.mu.Unlock()
		return fmt.Errorf("coordinator already started")
	}
	runCtx, cancel := context.WithCancel(ctx)
	c.started = true
	c.cancel = cancel
	c.done = make(chan struct{})
	done := c.done
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		c.started = false
		c.cancel = nil
		if c.done == done {
			close(done)
			c.done = nil
		}
		c.mu.Unlock()
	}()

	<-runCtx.Done()
	return runCtx.Err()
}

func (c *coordinator) Stop(ctx context.Context) error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	cancel := c.cancel
	done := c.done
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done == nil {
		return nil
	}
	if ctx == nil {
		<-done
		return nil
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *coordinator) setCancelled(runID string, value bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cancelled == nil {
		c.cancelled = map[string]time.Time{}
	}
	if value {
		c.cancelled[runID] = time.Now()
	} else {
		delete(c.cancelled, runID)
	}
	// Purge entries older than 1 hour to prevent unbounded growth.
	if len(c.cancelled) > 100 {
		cutoff := time.Now().Add(-1 * time.Hour)
		for id, ts := range c.cancelled {
			if ts.Before(cutoff) {
				delete(c.cancelled, id)
			}
		}
	}
}

func (c *coordinator) SubmitRun(ctx context.Context, req SubmitRequest) (SubmitResult, error) {
	if strings.TrimSpace(req.Input) == "" {
		return SubmitResult{}, fmt.Errorf("input is required")
	}
	runID := strings.TrimSpace(req.RunID)
	if runID == "" {
		runID = uuid.NewString()
	}
	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID == "" {
		sessionID = uuid.NewString()
	}
	now := time.Now().UTC()
	attempts := req.MaxAttempts
	if attempts <= 0 {
		attempts = c.policy.MaxAttempts
	}
	metadata := map[string]any{
		"executionMode": "distributed",
		"queue":         c.queueName,
		"attempt":       0,
		"retry_count":   0,
	}
	for k, v := range req.Metadata {
		metadata[k] = v
	}
	if err := c.store.SaveRun(ctx, state.RunRecord{
		RunID:     runID,
		SessionID: sessionID,
		Provider:  "distributed",
		Status:    "queued",
		Input:     req.Input,
		Output:    "",
		Messages:  nil,
		Usage:     nil,
		Metadata:  metadata,
		Error:     "",
		CreatedAt: &now,
		UpdatedAt: &now,
	}); err != nil {
		return SubmitResult{}, fmt.Errorf("failed to save queued run: %w", err)
	}
	task := queue.Task{
		RunID:        runID,
		SessionID:    sessionID,
		Input:        req.Input,
		Mode:         strings.TrimSpace(req.Mode),
		Workflow:     strings.TrimSpace(req.Workflow),
		WorkflowFile: strings.TrimSpace(req.WorkflowFile),
		Tools:        append([]string(nil), req.Tools...),
		SystemPrompt: req.SystemPrompt,
		Attempt:      1,
		MaxAttempts:  attempts,
		Metadata:     map[string]any{"queue": c.queueName},
		EnqueuedAt:   now,
	}
	msgID, err := c.queue.Enqueue(ctx, task)
	if err != nil {
		return SubmitResult{}, fmt.Errorf("failed to enqueue run: %w", err)
	}
	_ = c.attempts.SaveQueueEvent(ctx, QueueEvent{
		RunID: runID,
		Event: "queue.enqueued",
		At:    now,
		Payload: map[string]any{
			"messageId":   msgID,
			"maxAttempts": attempts,
		},
	})
	c.emit(ctx, observe.Event{
		RunID:      runID,
		SessionID:  sessionID,
		Kind:       observe.KindCustom,
		Status:     observe.StatusStarted,
		Name:       "queue.enqueued",
		Attributes: map[string]any{"messageId": msgID, "attempt": 1},
	})
	return SubmitResult{RunID: runID, SessionID: sessionID, MessageID: msgID, EnqueuedAt: now}, nil
}

func (c *coordinator) CancelRun(ctx context.Context, runID string) error {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return fmt.Errorf("runID is required")
	}
	run, err := c.store.LoadRun(ctx, runID)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	run.Status = "canceled"
	run.Error = "canceled"
	run.CompletedAt = &now
	run.UpdatedAt = &now
	if run.Metadata == nil {
		run.Metadata = map[string]any{}
	}
	run.Metadata["canceled"] = true
	if err := c.store.SaveRun(ctx, run); err != nil {
		return err
	}
	c.setCancelled(runID, true)
	_ = c.attempts.SaveQueueEvent(ctx, QueueEvent{RunID: runID, Event: "run.canceled", At: now})
	c.emit(ctx, observe.Event{
		RunID:      runID,
		SessionID:  run.SessionID,
		Kind:       observe.KindRun,
		Status:     observe.StatusFailed,
		Name:       "run.canceled",
		Message:    "run canceled",
		Attributes: map[string]any{"event": "run.canceled"},
	})
	return nil
}

func (c *coordinator) RequeueRun(ctx context.Context, runID string) error {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return fmt.Errorf("runID is required")
	}
	run, err := c.store.LoadRun(ctx, runID)
	if err != nil {
		return err
	}
	attempts, _ := c.attempts.ListAttempts(ctx, runID, 1)
	nextAttempt := 1
	if len(attempts) > 0 {
		nextAttempt = attempts[0].Attempt + 1
	}
	maxAttempts := c.policy.MaxAttempts
	if v, ok := run.Metadata["max_attempts"].(float64); ok && int(v) > 0 {
		maxAttempts = int(v)
	}
	task := queue.Task{
		RunID:        run.RunID,
		SessionID:    run.SessionID,
		Input:        run.Input,
		Mode:         metaString(run.Metadata, "mode"),
		Workflow:     metaString(run.Metadata, "workflow"),
		WorkflowFile: metaString(run.Metadata, "workflow_file"),
		Attempt:      nextAttempt,
		MaxAttempts:  maxAttempts,
		Metadata:     map[string]any{"requeued": true},
	}
	if rawTools, ok := run.Metadata["tools"].([]any); ok {
		for _, t := range rawTools {
			if s, ok := t.(string); ok {
				task.Tools = append(task.Tools, s)
			}
		}
	}
	_, err = c.queue.Enqueue(ctx, task)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	run.Status = "queued"
	run.Error = ""
	run.CompletedAt = nil
	run.UpdatedAt = &now
	if run.Metadata == nil {
		run.Metadata = map[string]any{}
	}
	run.Metadata["retry_count"] = nextAttempt - 1
	if err := c.store.SaveRun(ctx, run); err != nil {
		return err
	}
	_ = c.attempts.SaveQueueEvent(ctx, QueueEvent{RunID: runID, Event: "queue.requeued", At: now, Payload: map[string]any{"attempt": nextAttempt}})
	return nil
}

func (c *coordinator) QueueStats(ctx context.Context) (queue.Stats, error) {
	return c.queue.Stats(ctx)
}

func (c *coordinator) ListWorkers(ctx context.Context, limit int) ([]WorkerHeartbeat, error) {
	return c.attempts.ListWorkerHeartbeats(ctx, limit)
}

func (c *coordinator) ListRunAttempts(ctx context.Context, runID string, limit int) ([]AttemptRecord, error) {
	return c.attempts.ListAttempts(ctx, runID, limit)
}

func (c *coordinator) ListQueueEvents(ctx context.Context, runID string, limit int) ([]QueueEvent, error) {
	return c.attempts.ListQueueEvents(ctx, runID, limit)
}

func (c *coordinator) ListDLQ(ctx context.Context, limit int) ([]queue.Delivery, error) {
	return c.queue.ListDLQ(ctx, limit)
}

func (c *coordinator) emit(ctx context.Context, event observe.Event) {
	if c == nil || c.observer == nil {
		return
	}
	event.Normalize()
	_ = c.observer.Emit(ctx, event)
}

func metaString(metadata map[string]any, key string) string {
	if metadata == nil {
		return ""
	}
	if value, ok := metadata[key]; ok {
		if text, ok := value.(string); ok {
			return text
		}
	}
	return ""
}

var _ Coordinator = (*coordinator)(nil)
