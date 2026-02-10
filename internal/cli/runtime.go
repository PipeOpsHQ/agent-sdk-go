package cli

import (
	"context"
	"log"
	"time"

	devuiapi "github.com/PipeOpsHQ/agent-sdk-go/devui/api"
	"github.com/PipeOpsHQ/agent-sdk-go/observe"
	"github.com/PipeOpsHQ/agent-sdk-go/runtime/distributed"
	"github.com/PipeOpsHQ/agent-sdk-go/runtime/queue/redisstreams"
	"github.com/PipeOpsHQ/agent-sdk-go/state"
)

type runtimeComponents struct {
	service      devuiapi.RuntimeService
	attemptStore distributed.AttemptStore
	queue        *redisstreams.Queue
}

func buildRuntimeService(ctx context.Context, store state.Store, opts uiOptions) (*runtimeComponents, func()) {
	if store == nil {
		return nil, func() {}
	}

	attemptStore, err := distributed.NewSQLiteAttemptStore(opts.attemptsPath)
	if err != nil {
		log.Printf("runtime attempt store unavailable: %v", err)
		return nil, func() {}
	}

	queueStore, err := redisstreams.New(
		opts.redisAddr,
		redisstreams.WithPassword(opts.redisPassword),
		redisstreams.WithDB(opts.redisDB),
		redisstreams.WithPrefix(opts.queuePrefix),
		redisstreams.WithGroup(opts.queueGroup),
	)
	if err != nil {
		_ = attemptStore.Close()
		log.Printf("runtime queue unavailable: %v", err)
		return nil, func() {}
	}

	service, err := distributed.NewCoordinator(
		store,
		attemptStore,
		queueStore,
		observe.NoopSink{},
		distributed.DistributedConfig{Queue: distributed.QueueConfig{Name: "runs", Prefix: opts.queuePrefix}},
	)
	if err != nil {
		_ = queueStore.Close()
		_ = attemptStore.Close()
		log.Printf("runtime service unavailable: %v", err)
		return nil, func() {}
	}

	if _, claimErr := queueStore.Claim(ctx, "devui-bootstrap", time.Millisecond, 1); claimErr != nil {
		log.Printf("runtime bootstrap claim warning: %v", claimErr)
	}

	return &runtimeComponents{service: service, attemptStore: attemptStore, queue: queueStore}, func() {
		_ = queueStore.Close()
		_ = attemptStore.Close()
	}
}
