package types

import "time"

type EventType string

const (
	EventRunStarted         EventType = "run.started"
	EventBeforeGenerate     EventType = "run.before_generate"
	EventAfterGenerate      EventType = "run.after_generate"
	EventBeforeTool         EventType = "run.before_tool"
	EventAfterTool          EventType = "run.after_tool"
	EventGraphNodeStarted   EventType = "graph.node.started"
	EventGraphNodeCompleted EventType = "graph.node.completed"
	EventRunCompleted       EventType = "run.completed"
	EventRunFailed          EventType = "run.failed"
)

type Event struct {
	Type       EventType `json:"type"`
	Timestamp  time.Time `json:"timestamp"`
	RunID      string    `json:"runId,omitempty"`
	SessionID  string    `json:"sessionId,omitempty"`
	Provider   string    `json:"provider,omitempty"`
	Iteration  int       `json:"iteration,omitempty"`
	ToolName   string    `json:"toolName,omitempty"`
	ToolCallID string    `json:"toolCallId,omitempty"`
	Message    string    `json:"message,omitempty"`
	Error      string    `json:"error,omitempty"`
}
