package eval

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/PipeOpsHQ/agent-sdk-go/types"
)

func TestLoadJSONL(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "dataset.jsonl")
	content := "{\"id\":\"c1\",\"input\":\"hello\"}\n{\"input\":\"world\"}\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write dataset: %v", err)
	}

	cases, err := LoadJSONL(path)
	if err != nil {
		t.Fatalf("LoadJSONL returned error: %v", err)
	}
	if len(cases) != 2 {
		t.Fatalf("expected 2 cases, got %d", len(cases))
	}
	if cases[0].ID != "c1" {
		t.Fatalf("expected first case id c1, got %q", cases[0].ID)
	}
	if cases[1].ID == "" {
		t.Fatal("expected generated case id for second case")
	}
}

func TestEvaluateAssertionJSONSchema(t *testing.T) {
	t.Parallel()

	out := `{"status":"ok","count":3}`
	a := Assertion{
		Type: "json_schema",
		Schema: map[string]any{
			"type":     "object",
			"required": []any{"status", "count"},
			"properties": map[string]any{
				"status": map[string]any{"type": "string"},
				"count":  map[string]any{"type": "integer"},
			},
		},
	}
	check := evaluateAssertion(out, a, "json_schema")
	if !check.Pass {
		t.Fatalf("expected schema check to pass, got failure: %s", check.Detail)
	}
}

func TestRunnerRun(t *testing.T) {
	t.Parallel()

	start := time.Now().UTC()
	end := start.Add(120 * time.Millisecond)

	agent := &fakeAgent{responses: map[string]fakeResult{
		"use tool": {
			result: types.RunResult{
				Output:      "done",
				Events:      []types.Event{{Type: types.EventBeforeTool, ToolName: "shell"}},
				Usage:       &types.Usage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15},
				StartedAt:   &start,
				CompletedAt: &end,
			},
		},
		"bad": {err: errors.New("boom")},
	}}

	runner, err := NewRunner(RunnerConfig{Agent: agent})
	if err != nil {
		t.Fatalf("NewRunner failed: %v", err)
	}

	report, err := runner.Run(context.Background(), []Case{
		{ID: "a", Input: "use tool", RequiredTools: []string{"shell"}, Assertions: []Assertion{{Type: "contains", Value: "done"}}},
		{ID: "b", Input: "bad"},
	}, RunOptions{DatasetPath: "test.jsonl", Provider: "fake", Workers: 2, Retries: 1})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if report.Total != 2 {
		t.Fatalf("expected total 2, got %d", report.Total)
	}
	if report.Passed != 1 || report.Failed != 1 {
		t.Fatalf("expected 1 pass and 1 fail, got %d pass and %d fail", report.Passed, report.Failed)
	}
	if report.TotalTokens != 15 {
		t.Fatalf("expected total tokens 15, got %d", report.TotalTokens)
	}
	if report.ToolConstraintCases != 1 || report.ToolConstraintPassed != 1 {
		t.Fatalf("unexpected tool constraint metrics: %+v", report)
	}
	if report.Results[1].Attempts != 2 {
		t.Fatalf("expected retried case to have 2 attempts, got %d", report.Results[1].Attempts)
	}
}

func TestRunnerJudgeCheck(t *testing.T) {
	t.Parallel()

	agent := &fakeAgent{responses: map[string]fakeResult{
		"judge": {result: types.RunResult{Output: "answer"}},
	}}
	judge := &fakeJudge{result: JudgeResult{Score: 0.8, Reason: "good"}}

	runner, err := NewRunner(RunnerConfig{Agent: agent, Judge: judge})
	if err != nil {
		t.Fatalf("NewRunner failed: %v", err)
	}

	report, err := runner.Run(context.Background(), []Case{{ID: "j1", Input: "judge", JudgeRubric: "quality", MinJudgeScore: 0.75}}, RunOptions{Workers: 1})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if report.Total != 1 || report.Passed != 1 {
		t.Fatalf("expected passing judge case, got %+v", report)
	}
	if len(report.Results[0].Checks) == 0 {
		t.Fatal("expected judge check to be present")
	}
}

func TestRunnerCaseTimeout(t *testing.T) {
	t.Parallel()

	agent := &fakeAgent{responses: map[string]fakeResult{
		"slow": {delay: 200 * time.Millisecond, result: types.RunResult{Output: "late"}},
	}}
	runner, err := NewRunner(RunnerConfig{Agent: agent})
	if err != nil {
		t.Fatalf("NewRunner failed: %v", err)
	}

	report, err := runner.Run(context.Background(), []Case{{ID: "t1", Input: "slow"}}, RunOptions{
		Workers:     1,
		Retries:     0,
		CaseTimeout: 40 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if report.Passed != 0 || report.Failed != 1 {
		t.Fatalf("expected timeout failure, got %+v", report)
	}
	if report.Results[0].Error == "" {
		t.Fatal("expected timeout error text")
	}
}

func TestRunnerGlobalTimeout(t *testing.T) {
	t.Parallel()

	agent := &fakeAgent{responses: map[string]fakeResult{
		"slow-1": {delay: 250 * time.Millisecond, result: types.RunResult{Output: "one"}},
		"slow-2": {delay: 250 * time.Millisecond, result: types.RunResult{Output: "two"}},
	}}
	runner, err := NewRunner(RunnerConfig{Agent: agent})
	if err != nil {
		t.Fatalf("NewRunner failed: %v", err)
	}

	report, err := runner.Run(context.Background(), []Case{{ID: "g1", Input: "slow-1"}, {ID: "g2", Input: "slow-2"}}, RunOptions{
		Workers: 1,
		Retries: 0,
		Timeout: 90 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if report.Total != 2 {
		t.Fatalf("expected 2 results, got %d", report.Total)
	}
	if report.Passed != 0 {
		t.Fatalf("expected all failures on global timeout, got %+v", report)
	}
}

type fakeAgent struct {
	mu        sync.Mutex
	responses map[string]fakeResult
	calls     map[string]int
}

type fakeResult struct {
	result types.RunResult
	err    error
	delay  time.Duration
}

func (f *fakeAgent) RunDetailed(ctx context.Context, input string) (types.RunResult, error) {
	f.mu.Lock()
	if f.calls == nil {
		f.calls = map[string]int{}
	}
	f.calls[input]++
	r := f.responses[input]
	f.mu.Unlock()
	if r.delay > 0 {
		select {
		case <-ctx.Done():
			return types.RunResult{}, ctx.Err()
		case <-time.After(r.delay):
		}
	}
	return r.result, r.err
}

type fakeJudge struct {
	result JudgeResult
	err    error
}

func (f *fakeJudge) Score(_ context.Context, _ JudgeInput) (JudgeResult, error) {
	return f.result, f.err
}
