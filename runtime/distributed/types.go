package distributed

import (
	"context"
	"time"

	"github.com/PipeOpsHQ/agent-sdk-go/framework/runtime/queue"
)

type ExecutionMode string

const (
	ExecutionModeLocal       ExecutionMode = "local"
	ExecutionModeDistributed ExecutionMode = "distributed"
)

type SubmitRequest struct {
	RunID        string
	SessionID    string
	Input        string
	Mode         string
	Workflow     string
	WorkflowFile string
	Tools        []string
	SystemPrompt string
	Metadata     map[string]any
	MaxAttempts  int
}

type SubmitResult struct {
	RunID      string
	SessionID  string
	MessageID  string
	EnqueuedAt time.Time
}

type ProcessResult struct {
	Output   string
	Provider string
}

type ProcessFunc func(ctx context.Context, task queue.Task) (ProcessResult, error)

type AttemptRecord struct {
	RunID     string         `json:"runId"`
	Attempt   int            `json:"attempt"`
	WorkerID  string         `json:"workerId"`
	Status    string         `json:"status"`
	StartedAt time.Time      `json:"startedAt"`
	EndedAt   *time.Time     `json:"endedAt,omitempty"`
	Error     string         `json:"error,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

type WorkerHeartbeat struct {
	WorkerID   string         `json:"workerId"`
	Status     string         `json:"status"`
	LastSeenAt time.Time      `json:"lastSeenAt"`
	Capacity   int            `json:"capacity"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

type QueueEvent struct {
	ID      int64          `json:"id"`
	RunID   string         `json:"runId"`
	Event   string         `json:"event"`
	At      time.Time      `json:"at"`
	Payload map[string]any `json:"payload,omitempty"`
}
