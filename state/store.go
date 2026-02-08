package state

import (
	"context"
	"errors"
)

var (
	ErrNotFound = errors.New("state: not found")
	ErrConflict = errors.New("state: conflict")
)

type ListRunsQuery struct {
	SessionID string
	Limit     int
	Offset    int
	Status    string
}

type Store interface {
	SaveRun(ctx context.Context, run RunRecord) error
	LoadRun(ctx context.Context, runID string) (RunRecord, error)
	ListRuns(ctx context.Context, query ListRunsQuery) ([]RunRecord, error)

	SaveCheckpoint(ctx context.Context, checkpoint CheckpointRecord) error
	LoadLatestCheckpoint(ctx context.Context, runID string) (CheckpointRecord, error)
	ListCheckpoints(ctx context.Context, runID string, limit int) ([]CheckpointRecord, error)

	Close() error
}
