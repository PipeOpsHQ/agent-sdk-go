package redisstreams

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/PipeOpsHQ/agent-sdk-go/runtime/queue"
	"github.com/google/uuid"
)

func newTestQueue(t *testing.T) *Queue {
	t.Helper()
	addr := os.Getenv("TEST_REDIS_ADDR")
	if addr == "" {
		addr = "127.0.0.1:6379"
	}
	prefix := "aiag:qtest:" + uuid.NewString()
	q, err := New(addr, WithPrefix(prefix), WithGroup("test"))
	if err != nil {
		t.Skipf("redis unavailable at %s: %v", addr, err)
	}
	t.Cleanup(func() {
		ctx := context.Background()
		_ = q.client.Del(ctx, q.runStream, q.dlqStream).Err()
		_ = q.Close()
	})
	return q
}

func TestQueue_EnqueueClaimAck(t *testing.T) {
	q := newTestQueue(t)
	ctx := context.Background()

	id, err := q.Enqueue(ctx, queue.Task{RunID: "r1", SessionID: "s1", Input: "hello", Attempt: 1, MaxAttempts: 3})
	if err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}
	if id == "" {
		t.Fatalf("expected id")
	}

	deliveries, err := q.Claim(ctx, "worker-1", 500*time.Millisecond, 1)
	if err != nil {
		t.Fatalf("claim failed: %v", err)
	}
	if len(deliveries) != 1 {
		t.Fatalf("expected 1 delivery got %d", len(deliveries))
	}
	if deliveries[0].Task.RunID != "r1" {
		t.Fatalf("unexpected task: %+v", deliveries[0].Task)
	}
	if err := q.Ack(ctx, "worker-1", deliveries[0].ID); err != nil {
		t.Fatalf("ack failed: %v", err)
	}
}

func TestQueue_DeadLetterAndList(t *testing.T) {
	q := newTestQueue(t)
	ctx := context.Background()
	_, err := q.Enqueue(ctx, queue.Task{RunID: "r2", SessionID: "s2", Input: "x", Attempt: 3, MaxAttempts: 3})
	if err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}
	deliveries, err := q.Claim(ctx, "worker-2", 500*time.Millisecond, 1)
	if err != nil {
		t.Fatalf("claim failed: %v", err)
	}
	if len(deliveries) != 1 {
		t.Fatalf("expected one delivery")
	}
	if _, err := q.DeadLetter(ctx, deliveries[0], "failed"); err != nil {
		t.Fatalf("deadletter failed: %v", err)
	}
	dlq, err := q.ListDLQ(ctx, 10)
	if err != nil {
		t.Fatalf("list dlq failed: %v", err)
	}
	if len(dlq) == 0 {
		t.Fatalf("expected dlq entries")
	}
}
