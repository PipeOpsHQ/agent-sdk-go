package observe

import "time"

type Kind string

type Status string

const (
	KindRun        Kind = "run"
	KindProvider   Kind = "provider"
	KindTool       Kind = "tool"
	KindGraph      Kind = "graph"
	KindCheckpoint Kind = "checkpoint"
	KindCustom     Kind = "custom"
)

const (
	StatusStarted   Status = "started"
	StatusCompleted Status = "completed"
	StatusFailed    Status = "failed"
)

type Event struct {
	ID           string         `json:"id,omitempty"`
	Timestamp    time.Time      `json:"timestamp"`
	RunID        string         `json:"runId,omitempty"`
	SessionID    string         `json:"sessionId,omitempty"`
	SpanID       string         `json:"spanId,omitempty"`
	ParentSpanID string         `json:"parentSpanId,omitempty"`
	Kind         Kind           `json:"kind"`
	Status       Status         `json:"status,omitempty"`
	Name         string         `json:"name,omitempty"`
	Provider     string         `json:"provider,omitempty"`
	ToolName     string         `json:"toolName,omitempty"`
	Message      string         `json:"message,omitempty"`
	Error        string         `json:"error,omitempty"`
	DurationMs   int64          `json:"durationMs,omitempty"`
	Attributes   map[string]any `json:"attributes,omitempty"`
}

func (e *Event) Normalize() {
	if e == nil {
		return
	}
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now().UTC()
	}
	if e.Kind == "" {
		e.Kind = KindCustom
	}
	if e.Attributes == nil {
		e.Attributes = map[string]any{}
	}
}
