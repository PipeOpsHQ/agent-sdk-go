package sqlite

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/PipeOpsHQ/agent-sdk-go/framework/state"
	fwtypes "github.com/PipeOpsHQ/agent-sdk-go/framework/types"
)

//go:embed schema.sql
var schemaSQL string

const (
	defaultBusyTimeout = 5 * time.Second
	defaultLimit       = 50
)

type Store struct {
	db          *sql.DB
	busyTimeout time.Duration
	enableWAL   bool
	maxOpenConn int
}

type Option func(*Store)

func WithBusyTimeout(timeout time.Duration) Option {
	return func(s *Store) {
		if timeout >= 0 {
			s.busyTimeout = timeout
		}
	}
}

func WithWAL(enabled bool) Option {
	return func(s *Store) {
		s.enableWAL = enabled
	}
}

func WithMaxOpenConns(n int) Option {
	return func(s *Store) {
		if n > 0 {
			s.maxOpenConn = n
		}
	}
}

func New(path string, opts ...Option) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("sqlite path is required")
	}

	s := &Store{
		busyTimeout: defaultBusyTimeout,
		enableWAL:   true,
		maxOpenConn: 1,
	}
	for _, opt := range opts {
		opt(s)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("failed to create sqlite directory: %w", err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite db: %w", err)
	}
	db.SetMaxOpenConns(s.maxOpenConn)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	s.db = db
	if err := s.initialize(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}

	return s, nil
}

func (s *Store) initialize(ctx context.Context) error {
	if s.busyTimeout > 0 {
		ms := int(s.busyTimeout / time.Millisecond)
		if _, err := s.db.ExecContext(ctx, fmt.Sprintf("PRAGMA busy_timeout=%d;", ms)); err != nil {
			return fmt.Errorf("failed to set busy_timeout: %w", err)
		}
	}
	if s.enableWAL {
		if _, err := s.db.ExecContext(ctx, "PRAGMA journal_mode=WAL;"); err != nil {
			return fmt.Errorf("failed to enable wal: %w", err)
		}
	}
	if _, err := s.db.ExecContext(ctx, schemaSQL); err != nil {
		return fmt.Errorf("failed to initialize schema: %w", err)
	}
	return nil
}

func (s *Store) SaveRun(ctx context.Context, run state.RunRecord) error {
	now := time.Now().UTC()
	if run.CreatedAt == nil {
		run.CreatedAt = &now
	}
	if run.UpdatedAt == nil {
		run.UpdatedAt = &now
	}
	if run.RunID == "" {
		return fmt.Errorf("run_id is required")
	}
	if run.SessionID == "" {
		return fmt.Errorf("session_id is required")
	}
	if run.Provider == "" {
		run.Provider = "unknown"
	}
	if run.Status == "" {
		run.Status = "running"
	}

	messagesRaw, err := json.Marshal(run.Messages)
	if err != nil {
		return fmt.Errorf("failed to marshal messages: %w", err)
	}
	usageRaw, err := json.Marshal(run.Usage)
	if err != nil {
		return fmt.Errorf("failed to marshal usage: %w", err)
	}
	if run.Metadata == nil {
		run.Metadata = map[string]any{}
	}
	metaRaw, err := json.Marshal(run.Metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	const q = `
INSERT INTO runs (
  run_id, session_id, provider, status, input, output, messages, usage, metadata, error, created_at, updated_at, completed_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(run_id) DO UPDATE SET
  session_id=excluded.session_id,
  provider=excluded.provider,
  status=excluded.status,
  input=excluded.input,
  output=excluded.output,
  messages=excluded.messages,
  usage=excluded.usage,
  metadata=excluded.metadata,
  error=excluded.error,
  updated_at=excluded.updated_at,
  completed_at=excluded.completed_at;
`

	_, err = s.db.ExecContext(
		ctx,
		q,
		run.RunID,
		run.SessionID,
		run.Provider,
		run.Status,
		run.Input,
		run.Output,
		string(messagesRaw),
		nullIfEmptyJSON(usageRaw),
		string(metaRaw),
		run.Error,
		toNullableTime(run.CreatedAt),
		toNullableTime(run.UpdatedAt),
		toNullableTime(run.CompletedAt),
	)
	if err != nil {
		return fmt.Errorf("failed to save run: %w", err)
	}
	return nil
}

func (s *Store) LoadRun(ctx context.Context, runID string) (state.RunRecord, error) {
	if strings.TrimSpace(runID) == "" {
		return state.RunRecord{}, fmt.Errorf("run_id is required")
	}

	const q = `
SELECT run_id, session_id, provider, status, input, output, messages, usage, metadata, error, created_at, updated_at, completed_at
FROM runs
WHERE run_id = ?;
`
	var (
		runRaw       state.RunRecord
		messagesRaw  string
		usageRaw     sql.NullString
		metadataRaw  string
		createdRaw   string
		updatedRaw   string
		completedRaw sql.NullString
	)

	err := s.db.QueryRowContext(ctx, q, runID).Scan(
		&runRaw.RunID,
		&runRaw.SessionID,
		&runRaw.Provider,
		&runRaw.Status,
		&runRaw.Input,
		&runRaw.Output,
		&messagesRaw,
		&usageRaw,
		&metadataRaw,
		&runRaw.Error,
		&createdRaw,
		&updatedRaw,
		&completedRaw,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return state.RunRecord{}, state.ErrNotFound
		}
		return state.RunRecord{}, fmt.Errorf("failed to load run: %w", err)
	}

	run, err := decodeRunRow(runRaw, messagesRaw, usageRaw, metadataRaw, createdRaw, updatedRaw, completedRaw)
	if err != nil {
		return state.RunRecord{}, err
	}
	return run, nil
}

func (s *Store) ListRuns(ctx context.Context, query state.ListRunsQuery) ([]state.RunRecord, error) {
	limit := query.Limit
	if limit <= 0 {
		limit = defaultLimit
	}
	offset := query.Offset
	if offset < 0 {
		offset = 0
	}

	var (
		where []string
		args  []any
	)
	if query.SessionID != "" {
		where = append(where, "session_id = ?")
		args = append(args, query.SessionID)
	}
	if query.Status != "" {
		where = append(where, "status = ?")
		args = append(args, query.Status)
	}

	sqlText := `
SELECT run_id, session_id, provider, status, input, output, messages, usage, metadata, error, created_at, updated_at, completed_at
FROM runs
`
	if len(where) > 0 {
		sqlText += " WHERE " + strings.Join(where, " AND ")
	}
	sqlText += " ORDER BY created_at DESC LIMIT ? OFFSET ?;"
	args = append(args, limit, offset)

	rows, err := s.db.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list runs: %w", err)
	}
	defer rows.Close()

	runs := make([]state.RunRecord, 0, limit)
	for rows.Next() {
		var (
			runRaw       state.RunRecord
			messagesRaw  string
			usageRaw     sql.NullString
			metadataRaw  string
			createdRaw   string
			updatedRaw   string
			completedRaw sql.NullString
		)
		if err := rows.Scan(
			&runRaw.RunID,
			&runRaw.SessionID,
			&runRaw.Provider,
			&runRaw.Status,
			&runRaw.Input,
			&runRaw.Output,
			&messagesRaw,
			&usageRaw,
			&metadataRaw,
			&runRaw.Error,
			&createdRaw,
			&updatedRaw,
			&completedRaw,
		); err != nil {
			return nil, fmt.Errorf("failed to scan run row: %w", err)
		}
		run, err := decodeRunRow(runRaw, messagesRaw, usageRaw, metadataRaw, createdRaw, updatedRaw, completedRaw)
		if err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate runs: %w", err)
	}
	return runs, nil
}

func (s *Store) SaveCheckpoint(ctx context.Context, checkpoint state.CheckpointRecord) error {
	if checkpoint.RunID == "" {
		return fmt.Errorf("run_id is required")
	}
	if checkpoint.Seq < 0 {
		return fmt.Errorf("seq must be >= 0")
	}
	if checkpoint.NodeID == "" {
		checkpoint.NodeID = "unknown"
	}
	if checkpoint.State == nil {
		checkpoint.State = map[string]any{}
	}
	if checkpoint.CreatedAt.IsZero() {
		checkpoint.CreatedAt = time.Now().UTC()
	}

	stateRaw, err := json.Marshal(checkpoint.State)
	if err != nil {
		return fmt.Errorf("failed to marshal checkpoint state: %w", err)
	}

	const q = `
INSERT INTO checkpoints (run_id, seq, node_id, state, created_at)
VALUES (?, ?, ?, ?, ?);
`
	_, err = s.db.ExecContext(
		ctx,
		q,
		checkpoint.RunID,
		checkpoint.Seq,
		checkpoint.NodeID,
		string(stateRaw),
		checkpoint.CreatedAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		if isUniqueViolation(err) {
			return state.ErrConflict
		}
		return fmt.Errorf("failed to save checkpoint: %w", err)
	}
	return nil
}

func (s *Store) LoadLatestCheckpoint(ctx context.Context, runID string) (state.CheckpointRecord, error) {
	if runID == "" {
		return state.CheckpointRecord{}, fmt.Errorf("run_id is required")
	}

	const q = `
SELECT run_id, seq, node_id, state, created_at
FROM checkpoints
WHERE run_id = ?
ORDER BY seq DESC
LIMIT 1;
`

	var (
		record       state.CheckpointRecord
		stateRaw     string
		createdAtRaw string
	)
	err := s.db.QueryRowContext(ctx, q, runID).Scan(
		&record.RunID,
		&record.Seq,
		&record.NodeID,
		&stateRaw,
		&createdAtRaw,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return state.CheckpointRecord{}, state.ErrNotFound
		}
		return state.CheckpointRecord{}, fmt.Errorf("failed to load latest checkpoint: %w", err)
	}
	record.CreatedAt, err = parseRequiredTime(createdAtRaw)
	if err != nil {
		return state.CheckpointRecord{}, fmt.Errorf("failed to parse checkpoint created_at: %w", err)
	}
	if err := json.Unmarshal([]byte(stateRaw), &record.State); err != nil {
		return state.CheckpointRecord{}, fmt.Errorf("failed to decode checkpoint state: %w", err)
	}
	return record, nil
}

func (s *Store) ListCheckpoints(ctx context.Context, runID string, limit int) ([]state.CheckpointRecord, error) {
	if runID == "" {
		return nil, fmt.Errorf("run_id is required")
	}
	if limit <= 0 {
		limit = defaultLimit
	}

	const q = `
SELECT run_id, seq, node_id, state, created_at
FROM checkpoints
WHERE run_id = ?
ORDER BY seq DESC
LIMIT ?;
`

	rows, err := s.db.QueryContext(ctx, q, runID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to list checkpoints: %w", err)
	}
	defer rows.Close()

	out := make([]state.CheckpointRecord, 0, limit)
	for rows.Next() {
		var (
			record       state.CheckpointRecord
			stateRaw     string
			createdAtRaw string
		)
		if err := rows.Scan(
			&record.RunID,
			&record.Seq,
			&record.NodeID,
			&stateRaw,
			&createdAtRaw,
		); err != nil {
			return nil, fmt.Errorf("failed to scan checkpoint row: %w", err)
		}
		record.CreatedAt, err = parseRequiredTime(createdAtRaw)
		if err != nil {
			return nil, fmt.Errorf("failed to parse checkpoint time: %w", err)
		}
		if err := json.Unmarshal([]byte(stateRaw), &record.State); err != nil {
			return nil, fmt.Errorf("failed to decode checkpoint state: %w", err)
		}
		out = append(out, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate checkpoints: %w", err)
	}
	return out, nil
}

func (s *Store) Close() error {
	if s.db == nil {
		return nil
	}
	return s.db.Close()
}

func decodeRunRow(
	base state.RunRecord,
	messagesRaw string,
	usageRaw sql.NullString,
	metadataRaw string,
	createdRaw string,
	updatedRaw string,
	completedRaw sql.NullString,
) (state.RunRecord, error) {
	if err := json.Unmarshal([]byte(messagesRaw), &base.Messages); err != nil {
		return state.RunRecord{}, fmt.Errorf("failed to decode run messages: %w", err)
	}
	if usageRaw.Valid && strings.TrimSpace(usageRaw.String) != "" && usageRaw.String != "null" {
		var usage fwtypes.Usage
		if err := json.Unmarshal([]byte(usageRaw.String), &usage); err != nil {
			return state.RunRecord{}, fmt.Errorf("failed to decode run usage: %w", err)
		}
		base.Usage = &usage
	}
	if strings.TrimSpace(metadataRaw) == "" {
		base.Metadata = map[string]any{}
	} else if err := json.Unmarshal([]byte(metadataRaw), &base.Metadata); err != nil {
		return state.RunRecord{}, fmt.Errorf("failed to decode run metadata: %w", err)
	}
	created, err := parseRequiredTime(createdRaw)
	if err != nil {
		return state.RunRecord{}, fmt.Errorf("failed to parse run created_at: %w", err)
	}
	updated, err := parseRequiredTime(updatedRaw)
	if err != nil {
		return state.RunRecord{}, fmt.Errorf("failed to parse run updated_at: %w", err)
	}
	base.CreatedAt = &created
	base.UpdatedAt = &updated
	if completedRaw.Valid && strings.TrimSpace(completedRaw.String) != "" {
		completed, err := parseRequiredTime(completedRaw.String)
		if err != nil {
			return state.RunRecord{}, fmt.Errorf("failed to parse run completed_at: %w", err)
		}
		base.CompletedAt = &completed
	}
	return base, nil
}

func parseRequiredTime(raw string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}, err
	}
	return t.UTC(), nil
}

func toNullableTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func nullIfEmptyJSON(raw []byte) any {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	return string(raw)
}

func isUniqueViolation(err error) bool {
	return strings.Contains(strings.ToLower(err.Error()), "unique constraint failed")
}
