package hybrid

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/PipeOpsHQ/agent-sdk-go/framework/state"
)

type HybridStore struct {
	durable state.Store
	cache   state.Store
}

func New(durable state.Store, cache state.Store) (*HybridStore, error) {
	if durable == nil {
		return nil, fmt.Errorf("durable store is required")
	}
	return &HybridStore{
		durable: durable,
		cache:   cache,
	}, nil
}

func (h *HybridStore) SaveRun(ctx context.Context, run state.RunRecord) error {
	if err := h.durable.SaveRun(ctx, run); err != nil {
		return err
	}
	if h.cache != nil {
		if err := h.cache.SaveRun(ctx, run); err != nil {
			log.Printf("hybrid store cache SaveRun failed: %v", err)
		}
	}
	return nil
}

func (h *HybridStore) LoadRun(ctx context.Context, runID string) (state.RunRecord, error) {
	if h.cache != nil {
		run, err := h.cache.LoadRun(ctx, runID)
		if err == nil {
			return run, nil
		}
		if !errors.Is(err, state.ErrNotFound) {
			log.Printf("hybrid store cache LoadRun failed: %v", err)
		}
	}

	run, err := h.durable.LoadRun(ctx, runID)
	if err != nil {
		return state.RunRecord{}, err
	}
	if h.cache != nil {
		if err := h.cache.SaveRun(ctx, run); err != nil {
			log.Printf("hybrid store cache backfill SaveRun failed: %v", err)
		}
	}
	return run, nil
}

func (h *HybridStore) ListRuns(ctx context.Context, query state.ListRunsQuery) ([]state.RunRecord, error) {
	return h.durable.ListRuns(ctx, query)
}

func (h *HybridStore) SaveCheckpoint(ctx context.Context, checkpoint state.CheckpointRecord) error {
	if err := h.durable.SaveCheckpoint(ctx, checkpoint); err != nil {
		return err
	}
	if h.cache != nil {
		if err := h.cache.SaveCheckpoint(ctx, checkpoint); err != nil {
			log.Printf("hybrid store cache SaveCheckpoint failed: %v", err)
		}
	}
	return nil
}

func (h *HybridStore) LoadLatestCheckpoint(ctx context.Context, runID string) (state.CheckpointRecord, error) {
	if h.cache != nil {
		checkpoint, err := h.cache.LoadLatestCheckpoint(ctx, runID)
		if err == nil {
			return checkpoint, nil
		}
		if !errors.Is(err, state.ErrNotFound) {
			log.Printf("hybrid store cache LoadLatestCheckpoint failed: %v", err)
		}
	}

	checkpoint, err := h.durable.LoadLatestCheckpoint(ctx, runID)
	if err != nil {
		return state.CheckpointRecord{}, err
	}
	if h.cache != nil {
		if err := h.cache.SaveCheckpoint(ctx, checkpoint); err != nil {
			log.Printf("hybrid store cache backfill SaveCheckpoint failed: %v", err)
		}
	}
	return checkpoint, nil
}

func (h *HybridStore) ListCheckpoints(ctx context.Context, runID string, limit int) ([]state.CheckpointRecord, error) {
	return h.durable.ListCheckpoints(ctx, runID, limit)
}

func (h *HybridStore) Close() error {
	var firstErr error
	if h.cache != nil {
		if err := h.cache.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if h.durable != nil {
		if err := h.durable.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
