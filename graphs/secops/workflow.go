package secops

import (
	"github.com/PipeOpsHQ/agent-sdk-go/framework/graph"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/state"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/workflow"
)

const Name = "secops-static"

type Builder struct{}

func (Builder) Name() string { return Name }

func (Builder) Description() string {
	return "SecOps static graph that routes Trivy reports vs logs and applies deterministic preprocessing."
}

func (Builder) NewExecutor(runner graph.AgentRunner, store state.Store, sessionID string) (*graph.Executor, error) {
	return NewExecutor(runner, Config{Store: store, SessionID: sessionID})
}

func init() {
	workflow.MustRegister(Builder{})
}
