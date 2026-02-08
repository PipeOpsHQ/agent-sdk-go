package distributed

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/nitrocode/ai-agents/framework/runtime/queue"
	statesqlite "github.com/nitrocode/ai-agents/framework/state/sqlite"
)

type fakeQueue struct {
	mu      sync.Mutex
	tasks   []queue.Task
	dlq     []queue.Delivery
	counter int
}

func (f *fakeQueue) Enqueue(ctx context.Context, task queue.Task) (string, error) {
	_ = ctx
	f.mu.Lock()
	defer f.mu.Unlock()
	f.counter++
	f.tasks = append(f.tasks, task)
	return time.Now().UTC().Format(time.RFC3339Nano), nil
}

func (f *fakeQueue) Claim(ctx context.Context, consumer string, block time.Duration, count int) ([]queue.Delivery, error) {
	_ = ctx
	_ = consumer
	_ = block
	_ = count
	return nil, nil
}
func (f *fakeQueue) Ack(ctx context.Context, consumer string, messageIDs ...string) error {
	_ = ctx
	_ = consumer
	_ = messageIDs
	return nil
}
func (f *fakeQueue) Nack(ctx context.Context, consumer string, deliveries []queue.Delivery, reason string) error {
	_ = ctx
	_ = consumer
	_ = deliveries
	_ = reason
	return nil
}
func (f *fakeQueue) Requeue(ctx context.Context, task queue.Task, reason string, delay time.Duration) (string, error) {
	_ = reason
	_ = delay
	return f.Enqueue(ctx, task)
}
func (f *fakeQueue) DeadLetter(ctx context.Context, delivery queue.Delivery, reason string) (string, error) {
	_ = ctx
	_ = reason
	f.mu.Lock()
	defer f.mu.Unlock()
	f.dlq = append(f.dlq, delivery)
	return "dlq-1", nil
}
func (f *fakeQueue) ListDLQ(ctx context.Context, limit int) ([]queue.Delivery, error) {
	_ = ctx
	_ = limit
	return f.dlq, nil
}
func (f *fakeQueue) Stats(ctx context.Context) (queue.Stats, error) {
	_ = ctx
	f.mu.Lock()
	defer f.mu.Unlock()
	return queue.Stats{StreamLength: int64(len(f.tasks)), DLQLength: int64(len(f.dlq))}, nil
}
func (f *fakeQueue) Close() error { return nil }

func TestCoordinatorSubmitAndCancel(t *testing.T) {
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

	fq := &fakeQueue{}
	c, err := NewCoordinator(store, attempts, fq, nil, DistributedConfig{})
	if err != nil {
		t.Fatalf("new coordinator: %v", err)
	}
	res, err := c.SubmitRun(context.Background(), SubmitRequest{Input: "hello", MaxAttempts: 2})
	if err != nil {
		t.Fatalf("submit run: %v", err)
	}
	if res.RunID == "" {
		t.Fatalf("expected run id")
	}
	run, err := store.LoadRun(context.Background(), res.RunID)
	if err != nil {
		t.Fatalf("load run: %v", err)
	}
	if run.Status != "queued" {
		t.Fatalf("expected queued status, got %s", run.Status)
	}
	if err := c.CancelRun(context.Background(), res.RunID); err != nil {
		t.Fatalf("cancel run: %v", err)
	}
	run, err = store.LoadRun(context.Background(), res.RunID)
	if err != nil {
		t.Fatalf("load run after cancel: %v", err)
	}
	if run.Status != "canceled" {
		t.Fatalf("expected canceled status, got %s", run.Status)
	}
}

func TestCoordinatorStopCancelsStartLoop(t *testing.T) {
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

	fq := &fakeQueue{}
	c, err := NewCoordinator(store, attempts, fq, nil, DistributedConfig{})
	if err != nil {
		t.Fatalf("new coordinator: %v", err)
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- c.Start(context.Background())
	}()

	time.Sleep(80 * time.Millisecond)
	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := c.Stop(stopCtx); err != nil {
		t.Fatalf("stop coordinator: %v", err)
	}

	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled from Start after Stop, got: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatalf("coordinator start loop did not exit after Stop")
	}
}
