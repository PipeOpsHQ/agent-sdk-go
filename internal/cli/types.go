package cli

import (
	agentfw "github.com/PipeOpsHQ/agent-sdk-go/agent"
	basicgraph "github.com/PipeOpsHQ/agent-sdk-go/graphs/basic"
	_ "github.com/PipeOpsHQ/agent-sdk-go/graphs/chain"
	_ "github.com/PipeOpsHQ/agent-sdk-go/graphs/mapreduce"
	_ "github.com/PipeOpsHQ/agent-sdk-go/graphs/router"
	_ "github.com/PipeOpsHQ/agent-sdk-go/graphs/summarymemory"
	"github.com/PipeOpsHQ/agent-sdk-go/observe"
	"github.com/PipeOpsHQ/agent-sdk-go/state"
	"github.com/PipeOpsHQ/agent-sdk-go/types"
)

const (
	defaultSystemPrompt = "You are a practical AI assistant. Be concise, accurate, and actionable."
	defaultWorkflow     = basicgraph.Name
)

type cliOptions struct {
	workflow       string
	sessionID      string
	tools          []string
	conversation   []types.Message
	systemPrompt   string
	promptTemplate string
	middlewares    []agentfw.Middleware
}

type localPlaygroundRunner struct {
	store    state.Store
	observer observe.Sink
}
