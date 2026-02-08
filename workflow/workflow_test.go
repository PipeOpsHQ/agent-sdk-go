package workflow_test

import (
	"testing"

	_ "github.com/PipeOpsHQ/agent-sdk-go/framework/graphs/basic"
	_ "github.com/PipeOpsHQ/agent-sdk-go/framework/graphs/secops"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/workflow"
)

func TestBuiltInWorkflowsRegistered(t *testing.T) {
	names := workflow.Names()
	if len(names) == 0 {
		t.Fatalf("expected built-in workflows")
	}

	if _, ok := workflow.Get("basic"); !ok {
		t.Fatalf("expected basic workflow")
	}
	if _, ok := workflow.Get("secops-static"); !ok {
		t.Fatalf("expected secops-static workflow")
	}
}
