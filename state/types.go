package state

import (
	"time"

	"github.com/PipeOpsHQ/agent-sdk-go/framework/types"
)

type RunRecord struct {
	RunID       string          `json:"runId"`
	SessionID   string          `json:"sessionId"`
	Provider    string          `json:"provider"`
	Status      string          `json:"status"`
	Input       string          `json:"input"`
	Output      string          `json:"output"`
	Messages    []types.Message `json:"messages,omitempty"`
	Usage       *types.Usage    `json:"usage,omitempty"`
	Metadata    map[string]any  `json:"metadata,omitempty"`
	Error       string          `json:"error,omitempty"`
	CreatedAt   *time.Time      `json:"createdAt,omitempty"`
	UpdatedAt   *time.Time      `json:"updatedAt,omitempty"`
	CompletedAt *time.Time      `json:"completedAt,omitempty"`
}

type CheckpointRecord struct {
	RunID     string         `json:"runId"`
	Seq       int            `json:"seq"`
	NodeID    string         `json:"nodeId"`
	State     map[string]any `json:"state,omitempty"`
	CreatedAt time.Time      `json:"createdAt"`
}
