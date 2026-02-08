package workflow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PipeOpsHQ/agent-sdk-go/framework/types"
)

type fakeRunner struct{}

func (fakeRunner) RunDetailed(ctx context.Context, input string) (types.RunResult, error) {
	_ = ctx
	return types.RunResult{Output: "runner:" + input}, nil
}

func TestNewFileBuilderFromPath_RunBasicFlow(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wf.json")
	spec := `{
	  "name": "json-basic",
	  "start": "prep",
	  "nodes": [
	    {"id":"prep","kind":"template","template":"Q={{input}}","outputKey":"prompt"},
	    {"id":"ask","kind":"agent","inputFrom":"prompt","outputKey":"answer"},
	    {"id":"end","kind":"output","from":"answer"}
	  ],
	  "edges": [
	    {"from":"prep","to":"ask"},
	    {"from":"ask","to":"end"}
	  ]
	}`
	if err := os.WriteFile(path, []byte(spec), 0o644); err != nil {
		t.Fatalf("write spec failed: %v", err)
	}

	builder, err := NewFileBuilderFromPath(path)
	if err != nil {
		t.Fatalf("NewFileBuilderFromPath failed: %v", err)
	}
	exec, err := builder.NewExecutor(fakeRunner{}, nil, "")
	if err != nil {
		t.Fatalf("NewExecutor failed: %v", err)
	}

	res, err := exec.Run(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if res.Output != "runner:Q=hello" {
		t.Fatalf("unexpected output: %q", res.Output)
	}
	if strings.Join(res.NodeTrace, ",") != "prep,ask,end" {
		t.Fatalf("unexpected node trace: %v", res.NodeTrace)
	}
}

func TestNewFileBuilderFromPath_ConditionalEdge(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wf-cond.json")
	spec := `{
	  "name": "json-route",
	  "start": "route",
	  "nodes": [
	    {"id":"route","kind":"router_json_key","checkKey":"Results","key":"route","existsValue":"trivy","missingValue":"logs"},
	    {"id":"trivy","kind":"output","value":"trivy-path"},
	    {"id":"logs","kind":"output","value":"logs-path"}
	  ],
	  "edges": [
	    {"from":"route","to":"trivy","when":{"key":"route","equals":"trivy"}},
	    {"from":"route","to":"logs","when":{"key":"route","equals":"logs"}}
	  ]
	}`
	if err := os.WriteFile(path, []byte(spec), 0o644); err != nil {
		t.Fatalf("write spec failed: %v", err)
	}

	builder, err := NewFileBuilderFromPath(path)
	if err != nil {
		t.Fatalf("NewFileBuilderFromPath failed: %v", err)
	}
	exec, err := builder.NewExecutor(fakeRunner{}, nil, "")
	if err != nil {
		t.Fatalf("NewExecutor failed: %v", err)
	}

	resTrivy, err := exec.Run(context.Background(), `{"Results":[]}`)
	if err != nil {
		t.Fatalf("Run trivy failed: %v", err)
	}
	if resTrivy.Output != "trivy-path" {
		t.Fatalf("unexpected trivy output: %q", resTrivy.Output)
	}

	resLogs, err := exec.Run(context.Background(), "not-json")
	if err != nil {
		t.Fatalf("Run logs failed: %v", err)
	}
	if resLogs.Output != "logs-path" {
		t.Fatalf("unexpected logs output: %q", resLogs.Output)
	}
}
