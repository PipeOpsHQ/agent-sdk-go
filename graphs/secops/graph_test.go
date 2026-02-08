package secops

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/nitrocode/ai-agents/framework/state"
	"github.com/nitrocode/ai-agents/framework/types"
)

type memoryStore struct {
	mu          sync.Mutex
	runs        map[string]state.RunRecord
	checkpoints map[string][]state.CheckpointRecord
}

func newMemoryStore() *memoryStore {
	return &memoryStore{runs: map[string]state.RunRecord{}, checkpoints: map[string][]state.CheckpointRecord{}}
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
	r, ok := m.runs[runID]
	if !ok {
		return state.RunRecord{}, state.ErrNotFound
	}
	return r, nil
}

func (m *memoryStore) ListRuns(ctx context.Context, query state.ListRunsQuery) ([]state.RunRecord, error) {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []state.RunRecord{}
	for _, r := range m.runs {
		if query.SessionID != "" && query.SessionID != r.SessionID {
			continue
		}
		out = append(out, r)
	}
	return out, nil
}

func (m *memoryStore) SaveCheckpoint(ctx context.Context, checkpoint state.CheckpointRecord) error {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	m.checkpoints[checkpoint.RunID] = append(m.checkpoints[checkpoint.RunID], checkpoint)
	return nil
}

func (m *memoryStore) LoadLatestCheckpoint(ctx context.Context, runID string) (state.CheckpointRecord, error) {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	all := m.checkpoints[runID]
	if len(all) == 0 {
		return state.CheckpointRecord{}, state.ErrNotFound
	}
	return all[len(all)-1], nil
}

func (m *memoryStore) ListCheckpoints(ctx context.Context, runID string, limit int) ([]state.CheckpointRecord, error) {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	all := append([]state.CheckpointRecord(nil), m.checkpoints[runID]...)
	if limit > 0 && len(all) > limit {
		all = all[len(all)-limit:]
	}
	return all, nil
}

func (m *memoryStore) Close() error { return nil }

type fakeRunner struct{}

func (fakeRunner) RunDetailed(ctx context.Context, input string) (types.RunResult, error) {
	_ = ctx
	return types.RunResult{Output: "ok:" + input}, nil
}

func TestSecOpsGraph_TrivyPath(t *testing.T) {
	store := newMemoryStore()
	exec, err := NewExecutor(fakeRunner{}, Config{Store: store, SessionID: "s-trivy"})
	if err != nil {
		t.Fatalf("NewExecutor failed: %v", err)
	}

	input := `{"SchemaVersion":2,"ArtifactName":"demo","Results":[{"Target":"a","Type":"npm","Vulnerabilities":[{"VulnerabilityID":"CVE-1","PkgName":"x","InstalledVersion":"1","Severity":"HIGH"}]}]}`
	res, err := exec.Run(context.Background(), input)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if !strings.Contains(res.Output, "ok:") {
		t.Fatalf("unexpected output: %q", res.Output)
	}
	trace := strings.Join(res.NodeTrace, ",")
	if !strings.Contains(trace, "parse_trivy") || !strings.Contains(trace, "assistant_trivy") {
		t.Fatalf("unexpected trivy path trace: %v", res.NodeTrace)
	}
}

func TestSecOpsGraph_LogsPath(t *testing.T) {
	store := newMemoryStore()
	exec, err := NewExecutor(fakeRunner{}, Config{Store: store, SessionID: "s-logs"})
	if err != nil {
		t.Fatalf("NewExecutor failed: %v", err)
	}

	res, err := exec.Run(context.Background(), "2025-01-01 ERROR failed token=abc123")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if !strings.Contains(res.Output, "ok:") {
		t.Fatalf("unexpected output: %q", res.Output)
	}
	trace := strings.Join(res.NodeTrace, ",")
	if !strings.Contains(trace, "redact_logs") || !strings.Contains(trace, "assistant_logs") {
		t.Fatalf("unexpected logs path trace: %v", res.NodeTrace)
	}
}
