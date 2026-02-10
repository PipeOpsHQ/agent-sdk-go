package store

import (
	"context"
	"time"

	"github.com/PipeOpsHQ/agent-sdk-go/observe"
)

type ListQuery struct {
	Limit  int
	Offset int
}

type MetricsQuery struct {
	Since *time.Time
}

type MetricsSummary struct {
	RunsStarted      int64 `json:"runsStarted"`
	RunsCompleted    int64 `json:"runsCompleted"`
	RunsFailed       int64 `json:"runsFailed"`
	ProviderCalls    int64 `json:"providerCalls"`
	ProviderFailures int64 `json:"providerFailures"`
	ToolCalls        int64 `json:"toolCalls"`
	ToolFailures     int64 `json:"toolFailures"`
}

type Store interface {
	SaveEvent(ctx context.Context, event observe.Event) error
	ListEventsByRun(ctx context.Context, runID string, query ListQuery) ([]observe.Event, error)
	ListEventsBySession(ctx context.Context, sessionID string, query ListQuery) ([]observe.Event, error)
	AggregateMetrics(ctx context.Context, query MetricsQuery) (MetricsSummary, error)
	Close() error
}
