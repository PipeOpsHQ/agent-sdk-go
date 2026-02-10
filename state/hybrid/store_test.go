package hybrid

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/PipeOpsHQ/agent-sdk-go/state"
	"github.com/PipeOpsHQ/agent-sdk-go/types"
)

type memoryStore struct {
	mu          sync.Mutex
	runs        map[string]state.RunRecord
	checkpoints map[string][]state.CheckpointRecord
	failWrites  bool
}

func newMemoryStore() *memoryStore {
	return &memoryStore{
		runs:        map[string]state.RunRecord{},
		checkpoints: map[string][]state.CheckpointRecord{},
	}
}

func (m *memoryStore) SaveRun(ctx context.Context, run state.RunRecord) error {
	_ = ctx
	if m.failWrites {
		return errors.New("write failed")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.runs[run.RunID] = run
	return nil
}

func (m *memoryStore) LoadRun(ctx context.Context, runID string) (state.RunRecord, error) {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	run, ok := m.runs[runID]
	if !ok {
		return state.RunRecord{}, state.ErrNotFound
	}
	return run, nil
}

func (m *memoryStore) ListRuns(ctx context.Context, query state.ListRunsQuery) ([]state.RunRecord, error) {
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

func (m *memoryStore) SaveCheckpoint(ctx context.Context, checkpoint state.CheckpointRecord) error {
	_ = ctx
	if m.failWrites {
		return errors.New("write failed")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	existing := m.checkpoints[checkpoint.RunID]
	for _, item := range existing {
		if item.Seq == checkpoint.Seq {
			return state.ErrConflict
		}
	}
	m.checkpoints[checkpoint.RunID] = append(existing, checkpoint)
	return nil
}

func (m *memoryStore) LoadLatestCheckpoint(ctx context.Context, runID string) (state.CheckpointRecord, error) {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	list := m.checkpoints[runID]
	if len(list) == 0 {
		return state.CheckpointRecord{}, state.ErrNotFound
	}
	latest := list[0]
	for _, item := range list[1:] {
		if item.Seq > latest.Seq {
			latest = item
		}
	}
	return latest, nil
}

func (m *memoryStore) ListCheckpoints(ctx context.Context, runID string, limit int) ([]state.CheckpointRecord, error) {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	list := append([]state.CheckpointRecord(nil), m.checkpoints[runID]...)
	if limit > 0 && len(list) > limit {
		list = list[:limit]
	}
	return list, nil
}

func (m *memoryStore) Close() error { return nil }

func TestHybridStore_WriteUsesDurableAsSourceOfTruth(t *testing.T) {
	durable := newMemoryStore()
	cache := newMemoryStore()
	cache.failWrites = true

	h, err := New(durable, cache)
	if err != nil {
		t.Fatalf("failed to create hybrid store: %v", err)
	}

	now := time.Now().UTC()
	run := state.RunRecord{
		RunID:     "run-1",
		SessionID: "sess-1",
		Provider:  "p",
		Status:    "running",
		Input:     "hello",
		Messages:  []types.Message{{Role: types.RoleUser, Content: "hello"}},
		Metadata:  map[string]any{},
		CreatedAt: &now,
		UpdatedAt: &now,
	}
	if err := h.SaveRun(context.Background(), run); err != nil {
		t.Fatalf("SaveRun should succeed when cache fails: %v", err)
	}
	if _, err := durable.LoadRun(context.Background(), "run-1"); err != nil {
		t.Fatalf("durable store should contain run: %v", err)
	}
}

func TestHybridStore_ReadFallbackAndBackfill(t *testing.T) {
	durable := newMemoryStore()
	cache := newMemoryStore()

	h, err := New(durable, cache)
	if err != nil {
		t.Fatalf("failed to create hybrid store: %v", err)
	}

	now := time.Now().UTC()
	run := state.RunRecord{
		RunID:     "run-2",
		SessionID: "sess-2",
		Provider:  "p",
		Status:    "running",
		Input:     "hello",
		Messages:  []types.Message{{Role: types.RoleUser, Content: "hello"}},
		Metadata:  map[string]any{},
		CreatedAt: &now,
		UpdatedAt: &now,
	}
	if err := durable.SaveRun(context.Background(), run); err != nil {
		t.Fatalf("durable SaveRun failed: %v", err)
	}

	got, err := h.LoadRun(context.Background(), "run-2")
	if err != nil {
		t.Fatalf("LoadRun failed: %v", err)
	}
	if got.RunID != "run-2" {
		t.Fatalf("unexpected run: %#v", got)
	}
	if _, err := cache.LoadRun(context.Background(), "run-2"); err != nil {
		t.Fatalf("expected backfill into cache, got err: %v", err)
	}
}

func TestHybridStore_FailsWhenDurableFails(t *testing.T) {
	durable := newMemoryStore()
	durable.failWrites = true
	cache := newMemoryStore()

	h, err := New(durable, cache)
	if err != nil {
		t.Fatalf("failed to create hybrid store: %v", err)
	}
	err = h.SaveRun(context.Background(), state.RunRecord{
		RunID:     "run-3",
		SessionID: "sess-3",
		Provider:  "p",
		Status:    "running",
		Input:     "x",
	})
	if err == nil {
		t.Fatalf("expected SaveRun to fail when durable write fails")
	}
}
