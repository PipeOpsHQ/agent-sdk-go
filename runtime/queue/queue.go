package queue

import (
	"context"
	"time"
)

type Task struct {
	RunID        string         `json:"runId"`
	SessionID    string         `json:"sessionId"`
	Input        string         `json:"input"`
	Mode         string         `json:"mode,omitempty"`
	Workflow     string         `json:"workflow,omitempty"`
	WorkflowFile string         `json:"workflowFile,omitempty"`
	Tools        []string       `json:"tools,omitempty"`
	SystemPrompt string         `json:"systemPrompt,omitempty"`
	Attempt      int            `json:"attempt"`
	MaxAttempts  int            `json:"maxAttempts"`
	NotBefore    *time.Time     `json:"notBefore,omitempty"`
	Metadata     map[string]any `json:"metadata,omitempty"`
	EnqueuedAt   time.Time      `json:"enqueuedAt"`
}

type Delivery struct {
	ID       string    `json:"id"`
	Stream   string    `json:"stream"`
	Task     Task      `json:"task"`
	Received time.Time `json:"received"`
}

type Stats struct {
	StreamLength int64 `json:"streamLength"`
	DLQLength    int64 `json:"dlqLength"`
	Pending      int64 `json:"pending"`
}

type Queue interface {
	Enqueue(ctx context.Context, task Task) (string, error)
	Claim(ctx context.Context, consumer string, block time.Duration, count int) ([]Delivery, error)
	Ack(ctx context.Context, consumer string, messageIDs ...string) error
	Nack(ctx context.Context, consumer string, deliveries []Delivery, reason string) error
	Requeue(ctx context.Context, task Task, reason string, delay time.Duration) (string, error)
	DeadLetter(ctx context.Context, delivery Delivery, reason string) (string, error)
	ListDLQ(ctx context.Context, limit int) ([]Delivery, error)
	Stats(ctx context.Context) (Stats, error)
	Close() error
}
