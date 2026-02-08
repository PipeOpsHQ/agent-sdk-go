package api

import (
	"context"

	"github.com/PipeOpsHQ/agent-sdk-go/framework/runtime/distributed"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/runtime/queue"
)

type RuntimeService interface {
	QueueStats(ctx context.Context) (queue.Stats, error)
	ListWorkers(ctx context.Context, limit int) ([]distributed.WorkerHeartbeat, error)
	ListRunAttempts(ctx context.Context, runID string, limit int) ([]distributed.AttemptRecord, error)
	CancelRun(ctx context.Context, runID string) error
	RequeueRun(ctx context.Context, runID string) error
	ListDLQ(ctx context.Context, limit int) ([]queue.Delivery, error)
}
