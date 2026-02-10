package cron

import "time"

import "github.com/PipeOpsHQ/agent-sdk-go/delivery"

// JobConfig defines the configuration for a scheduled agent run.
type JobConfig struct {
	Workflow     string           `json:"workflow,omitempty"`
	Tools        []string         `json:"tools,omitempty"`
	SystemPrompt string           `json:"systemPrompt,omitempty"`
	Input        string           `json:"input"`
	ReplyTo      *delivery.Target `json:"replyTo,omitempty"`
}

// Job represents a scheduled recurring agent task.
type Job struct {
	Name     string    `json:"name"`
	CronExpr string    `json:"cronExpr"`
	Config   JobConfig `json:"config"`
	Enabled  bool      `json:"enabled"`
	LastRun  time.Time `json:"lastRun,omitempty"`
	NextRun  time.Time `json:"nextRun,omitempty"`
	LastErr  string    `json:"lastError,omitempty"`
	RunCount int       `json:"runCount"`
}

// JobRun captures one execution attempt for a scheduled job.
type JobRun struct {
	At         time.Time `json:"at"`
	DurationMS int64     `json:"durationMs"`
	Trigger    string    `json:"trigger"`
	Status     string    `json:"status"`
	Output     string    `json:"output,omitempty"`
	Error      string    `json:"error,omitempty"`
}

// RunFunc is called by the scheduler to execute a job.
type RunFunc func(cfg JobConfig) (string, error)
