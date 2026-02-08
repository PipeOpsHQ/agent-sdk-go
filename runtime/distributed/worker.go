package distributed

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/PipeOpsHQ/agent-sdk-go/framework/observe"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/runtime/queue"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/state"
	"github.com/google/uuid"
)

type Worker interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}

type worker struct {
	cfg       WorkerConfig
	store     state.Store
	attempts  AttemptStore
	queue     queue.Queue
	observer  observe.Sink
	policy    RuntimePolicy
	processor ProcessFunc
	mu        sync.Mutex
	started   bool
	cancel    context.CancelFunc
	done      chan struct{}
}

func NewWorker(cfg WorkerConfig, store state.Store, attempts AttemptStore, queueStore queue.Queue, observer observe.Sink, policy RuntimePolicy, processor ProcessFunc) (Worker, error) {
	if store == nil {
		return nil, fmt.Errorf("state store is required")
	}
	if attempts == nil {
		return nil, fmt.Errorf("attempt store is required")
	}
	if queueStore == nil {
		return nil, fmt.Errorf("queue is required")
	}
	if processor == nil {
		return nil, fmt.Errorf("processor is required")
	}
	if strings.TrimSpace(cfg.WorkerID) == "" {
		cfg.WorkerID = "worker-" + uuid.NewString()
	}
	if cfg.Capacity <= 0 {
		cfg.Capacity = 1
	}
	return &worker{
		cfg:       cfg,
		store:     store,
		attempts:  attempts,
		queue:     queueStore,
		observer:  observer,
		policy:    NormalizeRuntimePolicy(policy),
		processor: processor,
	}, nil
}

func (w *worker) Start(ctx context.Context) error {
	w.mu.Lock()
	if w.started {
		w.mu.Unlock()
		return fmt.Errorf("worker already started")
	}
	runCtx, cancel := context.WithCancel(ctx)
	w.started = true
	w.cancel = cancel
	w.done = make(chan struct{})
	done := w.done
	w.mu.Unlock()

	defer func() {
		cancel()
		w.mu.Lock()
		w.started = false
		w.cancel = nil
		if w.done == done {
			close(done)
			w.done = nil
		}
		w.mu.Unlock()
	}()

	heartbeat := time.NewTicker(w.policy.HeartbeatInterval)
	defer heartbeat.Stop()

	if err := w.attempts.SaveWorkerHeartbeat(runCtx, WorkerHeartbeat{
		WorkerID:   w.cfg.WorkerID,
		Status:     "online",
		LastSeenAt: time.Now().UTC(),
		Capacity:   w.cfg.Capacity,
	}); err != nil {
		return err
	}
	for {
		select {
		case <-runCtx.Done():
			_ = w.attempts.SaveWorkerHeartbeat(context.Background(), WorkerHeartbeat{
				WorkerID:   w.cfg.WorkerID,
				Status:     "offline",
				LastSeenAt: time.Now().UTC(),
				Capacity:   w.cfg.Capacity,
			})
			return runCtx.Err()
		case <-heartbeat.C:
			_ = w.attempts.SaveWorkerHeartbeat(runCtx, WorkerHeartbeat{
				WorkerID:   w.cfg.WorkerID,
				Status:     "online",
				LastSeenAt: time.Now().UTC(),
				Capacity:   w.cfg.Capacity,
			})
			w.emit(runCtx, observe.Event{
				Kind:   observe.KindCustom,
				Status: observe.StatusCompleted,
				Name:   "worker.heartbeat",
				Attributes: map[string]any{
					"workerId": w.cfg.WorkerID,
				},
			})
		default:
			deliveries, err := w.queue.Claim(runCtx, w.cfg.WorkerID, w.policy.ClaimBlock, w.cfg.Capacity)
			if err != nil {
				select {
				case <-runCtx.Done():
					continue
				case <-time.After(w.policy.PollInterval):
				}
				continue
			}
			if len(deliveries) == 0 {
				select {
				case <-runCtx.Done():
					continue
				case <-time.After(w.policy.PollInterval):
				}
				continue
			}
			for _, delivery := range deliveries {
				if err := w.handleDelivery(runCtx, delivery); err != nil {
					_ = w.attempts.SaveQueueEvent(runCtx, QueueEvent{
						RunID: delivery.Task.RunID,
						Event: "worker.delivery.error",
						At:    time.Now().UTC(),
						Payload: map[string]any{
							"workerId": w.cfg.WorkerID,
							"error":    err.Error(),
						},
					})
				}
			}
		}
	}
}

func (w *worker) Stop(ctx context.Context) error {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	cancel := w.cancel
	done := w.done
	w.mu.Unlock()
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

func (w *worker) handleDelivery(ctx context.Context, delivery queue.Delivery) error {
	task := delivery.Task
	now := time.Now().UTC()
	if task.NotBefore != nil && now.Before(task.NotBefore.UTC()) {
		_, _ = w.queue.Requeue(ctx, task, "not_before", task.NotBefore.UTC().Sub(now))
		return w.queue.Ack(ctx, w.cfg.WorkerID, delivery.ID)
	}
	if task.RunID == "" {
		return w.queue.Ack(ctx, w.cfg.WorkerID, delivery.ID)
	}
	if task.Attempt <= 0 {
		task.Attempt = 1
	}
	if task.MaxAttempts <= 0 {
		task.MaxAttempts = w.policy.MaxAttempts
	}

	run, err := w.store.LoadRun(ctx, task.RunID)
	if err == nil {
		if run.Status == "canceled" || run.Status == "completed" {
			return w.queue.Ack(ctx, w.cfg.WorkerID, delivery.ID)
		}
	}

	_ = w.attempts.StartAttempt(ctx, AttemptRecord{
		RunID:     task.RunID,
		Attempt:   task.Attempt,
		WorkerID:  w.cfg.WorkerID,
		Status:    "running",
		StartedAt: now,
		Metadata: map[string]any{
			"messageId": delivery.ID,
		},
	})
	_ = w.attempts.SaveQueueEvent(ctx, QueueEvent{RunID: task.RunID, Event: "queue.claimed", At: now, Payload: map[string]any{"workerId": w.cfg.WorkerID, "attempt": task.Attempt}})

	if err := w.updateRunStatus(ctx, task, "running", "", nil); err != nil {
		_ = w.queue.Ack(ctx, w.cfg.WorkerID, delivery.ID)
		return err
	}
	w.emit(ctx, observe.Event{
		RunID:      task.RunID,
		SessionID:  task.SessionID,
		Kind:       observe.KindCustom,
		Status:     observe.StatusStarted,
		Name:       "queue.claimed",
		Attributes: map[string]any{"workerId": w.cfg.WorkerID, "attempt": task.Attempt},
	})

	result, runErr := w.processor(ctx, task)
	if runErr == nil {
		now := time.Now().UTC()
		_ = w.attempts.FinishAttempt(ctx, task.RunID, task.Attempt, "completed", "")
		_ = w.updateRunStatus(ctx, task, "completed", result.Output, &now)
		_ = w.attempts.SaveQueueEvent(ctx, QueueEvent{RunID: task.RunID, Event: "run.completed", At: now, Payload: map[string]any{"workerId": w.cfg.WorkerID, "attempt": task.Attempt}})
		w.emit(ctx, observe.Event{RunID: task.RunID, SessionID: task.SessionID, Kind: observe.KindRun, Status: observe.StatusCompleted, Name: "run.completed"})
		return w.queue.Ack(ctx, w.cfg.WorkerID, delivery.ID)
	}

	errText := runErr.Error()
	_ = w.attempts.FinishAttempt(ctx, task.RunID, task.Attempt, "failed", errText)
	if task.Attempt < task.MaxAttempts {
		next := task
		next.Attempt = task.Attempt + 1
		backoff := w.policy.Backoff(task.Attempt)
		_, _ = w.queue.Requeue(ctx, next, errText, backoff)
		_ = w.attempts.SaveQueueEvent(ctx, QueueEvent{RunID: task.RunID, Event: "queue.retried", At: time.Now().UTC(), Payload: map[string]any{"attempt": next.Attempt, "error": errText}})
		_ = w.updateRunStatus(ctx, task, "queued", "", nil)
		w.emit(ctx, observe.Event{RunID: task.RunID, SessionID: task.SessionID, Kind: observe.KindCustom, Status: observe.StatusFailed, Name: "queue.retried", Error: errText, Attributes: map[string]any{"attempt": next.Attempt}})
		return w.queue.Ack(ctx, w.cfg.WorkerID, delivery.ID)
	}

	_, _ = w.queue.DeadLetter(ctx, delivery, errText)
	finished := time.Now().UTC()
	_ = w.updateRunStatusFailed(ctx, task, errText, &finished)
	_ = w.attempts.SaveQueueEvent(ctx, QueueEvent{RunID: task.RunID, Event: "queue.dead_lettered", At: finished, Payload: map[string]any{"attempt": task.Attempt, "error": errText}})
	w.emit(ctx, observe.Event{RunID: task.RunID, SessionID: task.SessionID, Kind: observe.KindCustom, Status: observe.StatusFailed, Name: "queue.dead_lettered", Error: errText, Attributes: map[string]any{"attempt": task.Attempt}})
	return nil
}

func (w *worker) updateRunStatus(ctx context.Context, task queue.Task, status string, output string, completedAt *time.Time) error {
	run, err := w.store.LoadRun(ctx, task.RunID)
	if err != nil {
		now := time.Now().UTC()
		metadata := map[string]any{"attempt": task.Attempt, "worker_id": w.cfg.WorkerID, "retry_count": task.Attempt - 1, "queue": "runs", "mode": task.Mode, "workflow": task.Workflow, "workflow_file": task.WorkflowFile, "tools": task.Tools, "max_attempts": task.MaxAttempts}
		r := state.RunRecord{
			RunID:       task.RunID,
			SessionID:   task.SessionID,
			Provider:    "distributed",
			Status:      status,
			Input:       task.Input,
			Output:      output,
			Metadata:    metadata,
			CreatedAt:   &now,
			UpdatedAt:   &now,
			CompletedAt: completedAt,
		}
		if completedAt != nil {
			r.UpdatedAt = completedAt
		}
		return w.store.SaveRun(ctx, r)
	}
	now := time.Now().UTC()
	run.Status = status
	run.Output = output
	run.UpdatedAt = &now
	if completedAt != nil {
		run.CompletedAt = completedAt
		run.UpdatedAt = completedAt
	}
	if run.Metadata == nil {
		run.Metadata = map[string]any{}
	}
	run.Metadata["attempt"] = task.Attempt
	run.Metadata["worker_id"] = w.cfg.WorkerID
	run.Metadata["retry_count"] = task.Attempt - 1
	run.Metadata["queue"] = "runs"
	run.Metadata["mode"] = task.Mode
	run.Metadata["workflow"] = task.Workflow
	run.Metadata["workflow_file"] = task.WorkflowFile
	run.Metadata["tools"] = task.Tools
	run.Metadata["max_attempts"] = task.MaxAttempts
	return w.store.SaveRun(ctx, run)
}

func (w *worker) updateRunStatusFailed(ctx context.Context, task queue.Task, errText string, completedAt *time.Time) error {
	run, err := w.store.LoadRun(ctx, task.RunID)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	run.Status = "failed"
	run.Error = errText
	run.UpdatedAt = &now
	run.CompletedAt = completedAt
	if completedAt != nil {
		run.UpdatedAt = completedAt
	}
	if run.Metadata == nil {
		run.Metadata = map[string]any{}
	}
	run.Metadata["attempt"] = task.Attempt
	run.Metadata["worker_id"] = w.cfg.WorkerID
	run.Metadata["retry_count"] = task.Attempt - 1
	return w.store.SaveRun(ctx, run)
}

func (w *worker) emit(ctx context.Context, event observe.Event) {
	if w == nil || w.observer == nil {
		return
	}
	event.Normalize()
	_ = w.observer.Emit(ctx, event)
}

var _ Worker = (*worker)(nil)
