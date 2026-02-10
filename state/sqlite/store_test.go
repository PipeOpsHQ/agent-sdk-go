package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/PipeOpsHQ/agent-sdk-go/state"
	"github.com/PipeOpsHQ/agent-sdk-go/types"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "state.db")
	s, err := New(dbPath)
	if err != nil {
		t.Fatalf("failed to create sqlite store: %v", err)
	}
	t.Cleanup(func() {
		_ = s.Close()
	})
	return s
}

func TestSQLiteStore_SaveLoadRun(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	now := time.Now().UTC()
	record := state.RunRecord{
		RunID:       "run-1",
		SessionID:   "sess-1",
		Provider:    "test-provider",
		Status:      "running",
		Input:       "hello",
		Output:      "",
		Messages:    []types.Message{{Role: types.RoleUser, Content: "hello"}},
		Usage:       &types.Usage{InputTokens: 1, OutputTokens: 2, TotalTokens: 3},
		Metadata:    map[string]any{"source": "test"},
		CreatedAt:   &now,
		UpdatedAt:   &now,
		CompletedAt: nil,
	}
	if err := s.SaveRun(ctx, record); err != nil {
		t.Fatalf("SaveRun failed: %v", err)
	}

	got, err := s.LoadRun(ctx, "run-1")
	if err != nil {
		t.Fatalf("LoadRun failed: %v", err)
	}
	if got.RunID != "run-1" || got.SessionID != "sess-1" {
		t.Fatalf("unexpected run identity: %#v", got)
	}
	if got.Usage == nil || got.Usage.TotalTokens != 3 {
		t.Fatalf("unexpected run usage: %#v", got.Usage)
	}

	runs, err := s.ListRuns(ctx, state.ListRunsQuery{SessionID: "sess-1", Limit: 10})
	if err != nil {
		t.Fatalf("ListRuns failed: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("expected 1 run, got %d", len(runs))
	}
}

func TestSQLiteStore_SaveRunUpsert(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	now := time.Now().UTC()
	record := state.RunRecord{
		RunID:     "run-upsert",
		SessionID: "sess-1",
		Provider:  "p1",
		Status:    "running",
		Input:     "first",
		Messages:  []types.Message{{Role: types.RoleUser, Content: "first"}},
		Metadata:  map[string]any{},
		CreatedAt: &now,
		UpdatedAt: &now,
	}
	if err := s.SaveRun(ctx, record); err != nil {
		t.Fatalf("SaveRun initial failed: %v", err)
	}

	updated := record
	updated.Status = "completed"
	updated.Output = "done"
	updated.Provider = "p2"
	now2 := now.Add(time.Second)
	updated.UpdatedAt = &now2
	updated.CompletedAt = &now2
	if err := s.SaveRun(ctx, updated); err != nil {
		t.Fatalf("SaveRun upsert failed: %v", err)
	}

	got, err := s.LoadRun(ctx, "run-upsert")
	if err != nil {
		t.Fatalf("LoadRun failed: %v", err)
	}
	if got.Status != "completed" || got.Output != "done" || got.Provider != "p2" {
		t.Fatalf("upsert not applied: %#v", got)
	}
	if got.CreatedAt == nil || !got.CreatedAt.Equal(now) {
		t.Fatalf("created_at should remain unchanged: %#v", got.CreatedAt)
	}
}

func TestSQLiteStore_SaveCheckpointAndLatest(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	now := time.Now().UTC()
	if err := s.SaveRun(ctx, state.RunRecord{
		RunID:     "run-ckpt",
		SessionID: "sess-1",
		Provider:  "p",
		Status:    "running",
		Input:     "x",
		Messages:  []types.Message{{Role: types.RoleUser, Content: "x"}},
		Metadata:  map[string]any{},
		CreatedAt: &now,
		UpdatedAt: &now,
	}); err != nil {
		t.Fatalf("SaveRun failed: %v", err)
	}

	cp1 := state.CheckpointRecord{
		RunID:     "run-ckpt",
		Seq:       1,
		NodeID:    "n1",
		State:     map[string]any{"k": "v1"},
		CreatedAt: now,
	}
	cp2 := state.CheckpointRecord{
		RunID:     "run-ckpt",
		Seq:       2,
		NodeID:    "n2",
		State:     map[string]any{"k": "v2"},
		CreatedAt: now.Add(time.Second),
	}
	if err := s.SaveCheckpoint(ctx, cp1); err != nil {
		t.Fatalf("SaveCheckpoint 1 failed: %v", err)
	}
	if err := s.SaveCheckpoint(ctx, cp2); err != nil {
		t.Fatalf("SaveCheckpoint 2 failed: %v", err)
	}
	if err := s.SaveCheckpoint(ctx, cp2); !errors.Is(err, state.ErrConflict) {
		t.Fatalf("expected ErrConflict for duplicate checkpoint, got %v", err)
	}

	latest, err := s.LoadLatestCheckpoint(ctx, "run-ckpt")
	if err != nil {
		t.Fatalf("LoadLatestCheckpoint failed: %v", err)
	}
	if latest.Seq != 2 || latest.NodeID != "n2" {
		t.Fatalf("unexpected latest checkpoint: %#v", latest)
	}

	all, err := s.ListCheckpoints(ctx, "run-ckpt", 10)
	if err != nil {
		t.Fatalf("ListCheckpoints failed: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 checkpoints, got %d", len(all))
	}
	if all[0].Seq != 2 || all[1].Seq != 1 {
		t.Fatalf("unexpected checkpoint order: %#v", all)
	}
}

func TestSQLiteStore_NotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if _, err := s.LoadRun(ctx, "missing"); !errors.Is(err, state.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for missing run, got %v", err)
	}
	if _, err := s.LoadLatestCheckpoint(ctx, "missing"); !errors.Is(err, state.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for missing checkpoint, got %v", err)
	}
}
