package redis

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nitrocode/ai-agents/framework/state"
	"github.com/nitrocode/ai-agents/framework/types"
)

func newTestRedisStore(t *testing.T) *Store {
	t.Helper()

	addr := os.Getenv("TEST_REDIS_ADDR")
	if addr == "" {
		addr = "127.0.0.1:6379"
	}
	prefix := "aiag-test-" + uuid.NewString()

	s, err := New(addr, WithPrefix(prefix), WithTTL(5*time.Minute))
	if err != nil {
		t.Skipf("redis unavailable at %s: %v", addr, err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		keys, _ := s.client.Keys(ctx, prefix+":*").Result()
		if len(keys) > 0 {
			_ = s.client.Del(ctx, keys...).Err()
		}
		_ = s.Close()
	})
	return s
}

func TestRedisStore_SaveLoadRunAndTTL(t *testing.T) {
	s := newTestRedisStore(t)
	ctx := context.Background()

	now := time.Now().UTC()
	run := state.RunRecord{
		RunID:     "run-1",
		SessionID: "sess-1",
		Provider:  "p",
		Status:    "running",
		Input:     "hello",
		Messages:  []types.Message{{Role: types.RoleUser, Content: "hello"}},
		Metadata:  map[string]any{"m": "v"},
		CreatedAt: &now,
		UpdatedAt: &now,
	}
	if err := s.SaveRun(ctx, run); err != nil {
		t.Fatalf("SaveRun failed: %v", err)
	}

	got, err := s.LoadRun(ctx, "run-1")
	if err != nil {
		t.Fatalf("LoadRun failed: %v", err)
	}
	if got.RunID != "run-1" || got.SessionID != "sess-1" {
		t.Fatalf("unexpected run: %#v", got)
	}

	runs, err := s.ListRuns(ctx, state.ListRunsQuery{SessionID: "sess-1", Limit: 10})
	if err != nil {
		t.Fatalf("ListRuns failed: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("expected 1 run, got %d", len(runs))
	}

	ttl, err := s.client.TTL(ctx, s.runKey("run-1")).Result()
	if err != nil {
		t.Fatalf("failed to read run ttl: %v", err)
	}
	if ttl <= 0 {
		t.Fatalf("expected ttl > 0, got %v", ttl)
	}
}

func TestRedisStore_SaveCheckpointAndLatest(t *testing.T) {
	s := newTestRedisStore(t)
	ctx := context.Background()

	cp1 := state.CheckpointRecord{
		RunID:     "run-ckpt",
		Seq:       1,
		NodeID:    "n1",
		State:     map[string]any{"value": "one"},
		CreatedAt: time.Now().UTC(),
	}
	cp2 := state.CheckpointRecord{
		RunID:     "run-ckpt",
		Seq:       2,
		NodeID:    "n2",
		State:     map[string]any{"value": "two"},
		CreatedAt: time.Now().UTC().Add(time.Second),
	}
	if err := s.SaveCheckpoint(ctx, cp1); err != nil {
		t.Fatalf("SaveCheckpoint 1 failed: %v", err)
	}
	if err := s.SaveCheckpoint(ctx, cp2); err != nil {
		t.Fatalf("SaveCheckpoint 2 failed: %v", err)
	}
	if err := s.SaveCheckpoint(ctx, cp2); !errors.Is(err, state.ErrConflict) {
		t.Fatalf("expected ErrConflict for duplicate seq, got %v", err)
	}

	latest, err := s.LoadLatestCheckpoint(ctx, "run-ckpt")
	if err != nil {
		t.Fatalf("LoadLatestCheckpoint failed: %v", err)
	}
	if latest.Seq != 2 {
		t.Fatalf("expected latest seq=2, got %d", latest.Seq)
	}

	list, err := s.ListCheckpoints(ctx, "run-ckpt", 10)
	if err != nil {
		t.Fatalf("ListCheckpoints failed: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 checkpoints, got %d", len(list))
	}
	if list[0].Seq != 2 {
		t.Fatalf("expected descending sequence order, got %#v", list)
	}
}

func TestRedisStore_PrunesStaleSessionIndexEntries(t *testing.T) {
	s := newTestRedisStore(t)
	ctx := context.Background()

	now := time.Now().UTC()
	run := state.RunRecord{
		RunID:     "run-stale",
		SessionID: "sess-stale",
		Provider:  "p",
		Status:    "running",
		Input:     "hello",
		Messages:  []types.Message{{Role: types.RoleUser, Content: "hello"}},
		Metadata:  map[string]any{},
		CreatedAt: &now,
		UpdatedAt: &now,
	}
	if err := s.SaveRun(ctx, run); err != nil {
		t.Fatalf("SaveRun failed: %v", err)
	}

	if err := s.client.Del(ctx, s.runKey("run-stale")).Err(); err != nil {
		t.Fatalf("failed to delete run key: %v", err)
	}

	runs, err := s.ListRuns(ctx, state.ListRunsQuery{SessionID: "sess-stale", Limit: 10})
	if err != nil {
		t.Fatalf("ListRuns failed: %v", err)
	}
	if len(runs) != 0 {
		t.Fatalf("expected 0 runs after stale key prune, got %d", len(runs))
	}

	score, err := s.client.ZScore(ctx, s.sessionIndexKey("sess-stale"), "run-stale").Result()
	if err == nil {
		t.Fatalf("expected stale run index removed, found zscore=%f", score)
	}
}

func TestRedisStore_LockHelpers(t *testing.T) {
	s := newTestRedisStore(t)
	ctx := context.Background()
	runID := "run-lock-" + uuid.NewString()

	got, err := s.AcquireRunLock(ctx, runID, "owner-1", 5*time.Second)
	if err != nil {
		t.Fatalf("AcquireRunLock 1 failed: %v", err)
	}
	if !got {
		t.Fatalf("expected first lock acquisition to succeed")
	}
	got, err = s.AcquireRunLock(ctx, runID, "owner-2", 5*time.Second)
	if err != nil {
		t.Fatalf("AcquireRunLock 2 failed: %v", err)
	}
	if got {
		t.Fatalf("expected second lock acquisition to fail")
	}

	if err := s.ReleaseRunLock(ctx, runID, "owner-2"); err != nil {
		t.Fatalf("ReleaseRunLock with wrong owner should not error: %v", err)
	}
	got, err = s.AcquireRunLock(ctx, runID, "owner-3", 5*time.Second)
	if err != nil {
		t.Fatalf("AcquireRunLock 3 failed: %v", err)
	}
	if got {
		t.Fatalf("expected lock to remain held with wrong owner release")
	}

	if err := s.ReleaseRunLock(ctx, runID, "owner-1"); err != nil {
		t.Fatalf("ReleaseRunLock with right owner failed: %v", err)
	}
	got, err = s.AcquireRunLock(ctx, runID, "owner-4", 5*time.Second)
	if err != nil {
		t.Fatalf("AcquireRunLock 4 failed: %v", err)
	}
	if !got {
		t.Fatalf("expected lock acquisition after release")
	}
	if err := s.ReleaseRunLock(ctx, runID, "owner-4"); err != nil {
		t.Fatalf("final release failed: %v", err)
	}
}

func TestRedisStore_NotFound(t *testing.T) {
	s := newTestRedisStore(t)
	ctx := context.Background()

	_, err := s.LoadRun(ctx, "missing-"+uuid.NewString())
	if !errors.Is(err, state.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for missing run, got %v", err)
	}

	_, err = s.LoadLatestCheckpoint(ctx, "missing-"+uuid.NewString())
	if !errors.Is(err, state.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for missing checkpoint, got %v", err)
	}
}

func BenchmarkRedisStore_SaveRun(b *testing.B) {
	addr := os.Getenv("TEST_REDIS_ADDR")
	if addr == "" {
		addr = "127.0.0.1:6379"
	}
	s, err := New(addr, WithPrefix("aiag-bench-"+uuid.NewString()))
	if err != nil {
		b.Skipf("redis unavailable: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	now := time.Now().UTC()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		run := state.RunRecord{
			RunID:     fmt.Sprintf("run-%d", i),
			SessionID: "bench",
			Provider:  "bench",
			Status:    "running",
			Input:     "x",
			Messages:  []types.Message{{Role: types.RoleUser, Content: "x"}},
			Metadata:  map[string]any{},
			CreatedAt: &now,
			UpdatedAt: &now,
		}
		if err := s.SaveRun(ctx, run); err != nil {
			b.Fatalf("SaveRun failed: %v", err)
		}
	}
}
