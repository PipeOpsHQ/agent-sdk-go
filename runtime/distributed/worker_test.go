package distributed

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/PipeOpsHQ/agent-sdk-go/runtime/queue"
	"github.com/PipeOpsHQ/agent-sdk-go/state"
	statesqlite "github.com/PipeOpsHQ/agent-sdk-go/state/sqlite"
)

type singleDeliveryQueue struct {
	delivery *queue.Delivery
	acked    bool
}

func (s *singleDeliveryQueue) Enqueue(ctx context.Context, task queue.Task) (string, error) {
	_ = ctx
	s.delivery = &queue.Delivery{ID: "1-0", Stream: "runs", Task: task, Received: time.Now().UTC()}
	return "1-0", nil
}
func (s *singleDeliveryQueue) Claim(ctx context.Context, consumer string, block time.Duration, count int) ([]queue.Delivery, error) {
	_ = ctx
	_ = consumer
	_ = block
	_ = count
	if s.delivery == nil || s.acked {
		return []queue.Delivery{}, nil
	}
	return []queue.Delivery{*s.delivery}, nil
}
func (s *singleDeliveryQueue) Ack(ctx context.Context, consumer string, messageIDs ...string) error {
	_ = ctx
	_ = consumer
	_ = messageIDs
	s.acked = true
	return nil
}
func (s *singleDeliveryQueue) Nack(ctx context.Context, consumer string, deliveries []queue.Delivery, reason string) error {
	_ = ctx
	_ = consumer
	_ = deliveries
	_ = reason
	return nil
}
func (s *singleDeliveryQueue) Requeue(ctx context.Context, task queue.Task, reason string, delay time.Duration) (string, error) {
	_ = ctx
	_ = reason
	_ = delay
	s.delivery = &queue.Delivery{ID: fmt.Sprintf("%d-0", task.Attempt+1), Stream: "runs", Task: task, Received: time.Now().UTC()}
	s.acked = false
	return s.delivery.ID, nil
}
func (s *singleDeliveryQueue) DeadLetter(ctx context.Context, delivery queue.Delivery, reason string) (string, error) {
	_ = ctx
	_ = delivery
	_ = reason
	s.acked = true
	return "dlq-1", nil
}
func (s *singleDeliveryQueue) ListDLQ(ctx context.Context, limit int) ([]queue.Delivery, error) {
	_ = ctx
	_ = limit
	return nil, nil
}
func (s *singleDeliveryQueue) Stats(ctx context.Context) (queue.Stats, error) {
	_ = ctx
	return queue.Stats{}, nil
}
func (s *singleDeliveryQueue) Close() error { return nil }

func TestWorkerProcessesTask(t *testing.T) {
	store, err := statesqlite.New(t.TempDir() + "/state.db")
	if err != nil {
		t.Fatalf("state store: %v", err)
	}
	defer func() { _ = store.Close() }()
	attempts, err := NewSQLiteAttemptStore(t.TempDir() + "/attempts.db")
	if err != nil {
		t.Fatalf("attempt store: %v", err)
	}
	defer func() { _ = attempts.Close() }()

	now := time.Now().UTC()
	if err := store.SaveRun(context.Background(), state.RunRecord{
		RunID:     "r1",
		SessionID: "s1",
		Provider:  "distributed",
		Status:    "queued",
		Input:     "hello",
		Output:    "",
		Metadata:  map[string]any{},
		CreatedAt: &now,
		UpdatedAt: &now,
	}); err != nil {
		t.Fatalf("seed run: %v", err)
	}

	q := &singleDeliveryQueue{delivery: &queue.Delivery{ID: "1-0", Stream: "runs", Task: queue.Task{RunID: "r1", SessionID: "s1", Input: "hello", Attempt: 1, MaxAttempts: 2}, Received: time.Now().UTC()}}
	w, err := NewWorker(WorkerConfig{WorkerID: "w1"}, store, attempts, q, nil, DefaultRuntimePolicy(), func(ctx context.Context, task queue.Task) (ProcessResult, error) {
		_ = ctx
		_ = task
		return ProcessResult{Output: "ok", Provider: "test"}, nil
	})
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	_ = w.Start(ctx)

	run, err := store.LoadRun(context.Background(), "r1")
	if err != nil {
		t.Fatalf("load run: %v", err)
	}
	if run.Status != "completed" {
		t.Fatalf("expected completed, got %s", run.Status)
	}
	if run.Output != "ok" {
		t.Fatalf("unexpected output: %s", run.Output)
	}
	attemptList, err := attempts.ListAttempts(context.Background(), "r1", 10)
	if err != nil {
		t.Fatalf("list attempts: %v", err)
	}
	if len(attemptList) == 0 {
		t.Fatalf("expected attempt records")
	}
}

func TestWorkerStopCancelsStartLoop(t *testing.T) {
	store, err := statesqlite.New(t.TempDir() + "/state.db")
	if err != nil {
		t.Fatalf("state store: %v", err)
	}
	defer func() { _ = store.Close() }()
	attempts, err := NewSQLiteAttemptStore(t.TempDir() + "/attempts.db")
	if err != nil {
		t.Fatalf("attempt store: %v", err)
	}
	defer func() { _ = attempts.Close() }()

	q := &singleDeliveryQueue{}
	policy := DefaultRuntimePolicy()
	policy.PollInterval = 10 * time.Millisecond
	policy.ClaimBlock = 10 * time.Millisecond
	policy.HeartbeatInterval = 25 * time.Millisecond

	w, err := NewWorker(WorkerConfig{WorkerID: "w-stop"}, store, attempts, q, nil, policy, func(ctx context.Context, task queue.Task) (ProcessResult, error) {
		_ = ctx
		_ = task
		return ProcessResult{}, nil
	})
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- w.Start(context.Background())
	}()

	time.Sleep(80 * time.Millisecond)
	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := w.Stop(stopCtx); err != nil {
		t.Fatalf("stop worker: %v", err)
	}

	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled from Start after Stop, got: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatalf("worker start loop did not exit after Stop")
	}
}
