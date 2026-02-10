package cli

import (
	agentfw "github.com/PipeOpsHQ/agent-sdk-go/framework/agent"
	basicgraph "github.com/PipeOpsHQ/agent-sdk-go/framework/graphs/basic"
	_ "github.com/PipeOpsHQ/agent-sdk-go/framework/graphs/chain"
	_ "github.com/PipeOpsHQ/agent-sdk-go/framework/graphs/mapreduce"
	_ "github.com/PipeOpsHQ/agent-sdk-go/framework/graphs/router"
	_ "github.com/PipeOpsHQ/agent-sdk-go/framework/graphs/summarymemory"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/observe"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/state"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/types"
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
