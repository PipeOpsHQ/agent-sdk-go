package workflow_test

import (
	"testing"

	_ "github.com/PipeOpsHQ/agent-sdk-go/graphs/basic"
	_ "github.com/PipeOpsHQ/agent-sdk-go/graphs/chain"
	_ "github.com/PipeOpsHQ/agent-sdk-go/graphs/mapreduce"
	_ "github.com/PipeOpsHQ/agent-sdk-go/graphs/router"
	"github.com/PipeOpsHQ/agent-sdk-go/workflow"
)

func TestBuiltInWorkflowsRegistered(t *testing.T) {
	names := workflow.Names()
	if len(names) < 4 {
		t.Fatalf("expected at least 4 built-in workflows, got %d: %v", len(names), names)
	}

	for _, name := range []string{"basic", "chain", "router", "map-reduce"} {
		if _, ok := workflow.Get(name); !ok {
			t.Fatalf("expected %q workflow to be registered", name)
		}
	}
}
