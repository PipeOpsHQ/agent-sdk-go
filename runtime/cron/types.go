package cron

import "time"

// JobConfig defines the configuration for a scheduled agent run.
type JobConfig struct {
	Workflow     string   `json:"workflow,omitempty"`
	Tools        []string `json:"tools,omitempty"`
	SystemPrompt string   `json:"systemPrompt,omitempty"`
	Input        string   `json:"input"`
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

// RunFunc is called by the scheduler to execute a job.
type RunFunc func(cfg JobConfig) (string, error)
