package graph

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/PipeOpsHQ/agent-sdk-go/framework/state"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/types"
)

type memoryStore struct {
	mu          sync.Mutex
	runs        map[string]state.RunRecord
	checkpoints map[string][]state.CheckpointRecord
}

func newMemoryStore() *memoryStore {
	return &memoryStore{
		runs:        map[string]state.RunRecord{},
		checkpoints: map[string][]state.CheckpointRecord{},
	}
}

func (m *memoryStore) SaveRun(ctx context.Context, run state.RunRecord) error {
	_ = ctx
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
	m.mu.Lock()
	defer m.mu.Unlock()
	existing := m.checkpoints[checkpoint.RunID]
	for _, e := range existing {
		if e.Seq == checkpoint.Seq {
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
	items := m.checkpoints[runID]
	if len(items) == 0 {
		return state.CheckpointRecord{}, state.ErrNotFound
	}
	latest := items[0]
	for i := 1; i < len(items); i++ {
		if items[i].Seq > latest.Seq {
			latest = items[i]
		}
	}
	return latest, nil
}

func (m *memoryStore) ListCheckpoints(ctx context.Context, runID string, limit int) ([]state.CheckpointRecord, error) {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	items := append([]state.CheckpointRecord(nil), m.checkpoints[runID]...)
	if limit > 0 && len(items) > limit {
		items = items[len(items)-limit:]
	}
	return items, nil
}

func (m *memoryStore) Close() error { return nil }

type fakeRunner struct {
	output string
}

func (f *fakeRunner) RunDetailed(ctx context.Context, input string) (types.RunResult, error) {
	_ = ctx
	return types.RunResult{Output: f.output + ":" + input}, nil
}

func TestGraphCompile_Validation(t *testing.T) {
	g := New("test")
	g.AddNode("start", NewToolNode(func(ctx context.Context, s *State) error { return nil }))
	g.SetStart("start")
	g.AddEdge("start", "missing", nil)

	if err := g.Compile(); err == nil {
		t.Fatalf("expected compile error for missing edge node")
	}
}

func TestGraphCompile_DetectsCyclesByDefault(t *testing.T) {
	g := New("cycle")
	g.AddNode("a", NewToolNode(func(ctx context.Context, s *State) error { return nil }))
	g.AddNode("b", NewToolNode(func(ctx context.Context, s *State) error { return nil }))
	g.SetStart("a")
	g.AddEdge("a", "b", nil)
	g.AddEdge("b", "a", nil)

	if err := g.Compile(); err == nil {
		t.Fatalf("expected cycle compile error")
	}

	if err := g.AllowCycles(true).Compile(); err != nil {
		t.Fatalf("expected compile success with allowed cycles: %v", err)
	}
}

func TestExecutor_Run_DeterministicThreeNodes(t *testing.T) {
	store := newMemoryStore()
	g := New("pipeline")
	g.AddNode("prepare", NewToolNode(func(ctx context.Context, s *State) error {
		s.ensureData()
		s.Data["prepared"] = strings.ToUpper(s.Input)
		return nil
	}))
	g.AddNode("agent", &AgentNode{
		Runner: &fakeRunner{output: "ok"},
		Input: func(s *State) (string, error) {
			v, _ := s.Data["prepared"].(string)
			return v, nil
		},
		OutputKey: "agent_result",
	})
	g.AddNode("finalize", NewToolNode(func(ctx context.Context, s *State) error {
		v, _ := s.Data["agent_result"].(string)
		s.Output = "FINAL " + v
		s.ensureData()
		s.Data["output"] = s.Output
		return nil
	}))
	g.SetStart("prepare")
	g.AddEdge("prepare", "agent", nil)
	g.AddEdge("agent", "finalize", nil)

	executor, err := NewExecutor(g, WithStore(store), WithSessionID("sess-1"))
	if err != nil {
		t.Fatalf("failed to build executor: %v", err)
	}

	result, err := executor.Run(context.Background(), "hello")
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if result.Output != "FINAL ok:HELLO" {
		t.Fatalf("unexpected output: %q", result.Output)
	}
	if len(result.NodeTrace) != 3 {
		t.Fatalf("expected 3 node trace entries, got %d", len(result.NodeTrace))
	}
	if result.NodeTrace[0] != "prepare" || result.NodeTrace[2] != "finalize" {
		t.Fatalf("unexpected node trace: %#v", result.NodeTrace)
	}

	run, err := store.LoadRun(context.Background(), result.RunID)
	if err != nil {
		t.Fatalf("load run failed: %v", err)
	}
	if run.Status != "completed" {
		t.Fatalf("expected completed run, got %q", run.Status)
	}

	checkpoints, err := store.ListCheckpoints(context.Background(), result.RunID, 10)
	if err != nil {
		t.Fatalf("list checkpoints failed: %v", err)
	}
	if len(checkpoints) != 3 {
		t.Fatalf("expected 3 checkpoints, got %d", len(checkpoints))
	}
}

func TestExecutor_Resume_FromCheckpoint(t *testing.T) {
	store := newMemoryStore()
	var midCalls int

	g := New("resume")
	g.AddNode("a", NewToolNode(func(ctx context.Context, s *State) error {
		s.ensureData()
		s.Data["v"] = "from-a"
		return nil
	}))
	g.AddNode("b", NewToolNode(func(ctx context.Context, s *State) error {
		midCalls++
		if midCalls == 1 {
			return errors.New("transient node error")
		}
		s.ensureData()
		s.Data["v"] = "from-b"
		return nil
	}))
	g.AddNode("c", NewToolNode(func(ctx context.Context, s *State) error {
		s.Output = "done"
		return nil
	}))
	g.SetStart("a")
	g.AddEdge("a", "b", nil)
	g.AddEdge("b", "c", nil)

	executor, err := NewExecutor(g, WithStore(store), WithSessionID("sess-r"))
	if err != nil {
		t.Fatalf("failed to build executor: %v", err)
	}

	first, err := executor.Run(context.Background(), "input")
	if err == nil {
		t.Fatalf("expected first run to fail")
	}
	if first.RunID != "" {
		t.Fatalf("expected empty run result on failure")
	}

	runs, err := store.ListRuns(context.Background(), state.ListRunsQuery{SessionID: "sess-r"})
	if err != nil {
		t.Fatalf("list runs failed: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("expected exactly one run, got %d", len(runs))
	}
	if runs[0].Status != "failed" {
		t.Fatalf("expected failed status, got %q", runs[0].Status)
	}

	resumed, err := executor.Resume(context.Background(), runs[0].RunID)
	if err != nil {
		t.Fatalf("resume failed: %v", err)
	}
	if resumed.Output != "done" {
		t.Fatalf("unexpected resumed output: %q", resumed.Output)
	}
	if len(resumed.NodeTrace) != 2 {
		t.Fatalf("expected resume node trace length 2, got %d", len(resumed.NodeTrace))
	}

	latest, err := store.LoadLatestCheckpoint(context.Background(), runs[0].RunID)
	if err != nil {
		t.Fatalf("load latest checkpoint failed: %v", err)
	}
	if latest.Seq != 3 {
		t.Fatalf("expected latest checkpoint seq 3, got %d", latest.Seq)
	}

	run, err := store.LoadRun(context.Background(), runs[0].RunID)
	if err != nil {
		t.Fatalf("load run failed: %v", err)
	}
	if run.Status != "completed" {
		t.Fatalf("expected completed status after resume, got %q", run.Status)
	}
	if run.CompletedAt == nil || run.CompletedAt.Before(time.Now().Add(-2*time.Minute)) {
		t.Fatalf("expected recent completion timestamp")
	}
}
