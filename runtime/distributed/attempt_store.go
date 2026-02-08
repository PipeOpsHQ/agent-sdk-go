package distributed

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var distributedSchema string

type AttemptStore interface {
	StartAttempt(ctx context.Context, record AttemptRecord) error
	FinishAttempt(ctx context.Context, runID string, attempt int, status string, errText string) error
	ListAttempts(ctx context.Context, runID string, limit int) ([]AttemptRecord, error)
	SaveWorkerHeartbeat(ctx context.Context, heartbeat WorkerHeartbeat) error
	ListWorkerHeartbeats(ctx context.Context, limit int) ([]WorkerHeartbeat, error)
	SaveQueueEvent(ctx context.Context, event QueueEvent) error
	ListQueueEvents(ctx context.Context, runID string, limit int) ([]QueueEvent, error)
	Close() error
}

type SQLiteAttemptStore struct {
	db *sql.DB
}

func NewSQLiteAttemptStore(path string) (*SQLiteAttemptStore, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("sqlite path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("failed to create sqlite dir: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if _, err := db.ExecContext(context.Background(), "PRAGMA journal_mode=WAL;"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to enable wal: %w", err)
	}
	if _, err := db.ExecContext(context.Background(), distributedSchema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to initialize distributed schema: %w", err)
	}
	return &SQLiteAttemptStore{db: db}, nil
}

func (s *SQLiteAttemptStore) StartAttempt(ctx context.Context, record AttemptRecord) error {
	if record.RunID == "" {
		return fmt.Errorf("runID is required")
	}
	if record.Attempt <= 0 {
		return fmt.Errorf("attempt must be > 0")
	}
	if record.Status == "" {
		record.Status = "running"
	}
	if record.StartedAt.IsZero() {
		record.StartedAt = time.Now().UTC()
	}
	if record.Metadata == nil {
		record.Metadata = map[string]any{}
	}
	metaRaw, err := json.Marshal(record.Metadata)
	if err != nil {
		return fmt.Errorf("encode attempt metadata: %w", err)
	}
	const q = `
INSERT INTO run_attempts (run_id, attempt, worker_id, status, started_at, ended_at, error, metadata)
VALUES (?, ?, ?, ?, ?, NULL, '', ?)
ON CONFLICT(run_id, attempt) DO UPDATE SET
  worker_id=excluded.worker_id,
  status=excluded.status,
  started_at=excluded.started_at,
  ended_at=NULL,
  error='',
  metadata=excluded.metadata;
`
	_, err = s.db.ExecContext(ctx, q,
		record.RunID,
		record.Attempt,
		record.WorkerID,
		record.Status,
		record.StartedAt.UTC().Format(time.RFC3339Nano),
		string(metaRaw),
	)
	if err != nil {
		return fmt.Errorf("start attempt: %w", err)
	}
	return nil
}

func (s *SQLiteAttemptStore) FinishAttempt(ctx context.Context, runID string, attempt int, status string, errText string) error {
	if runID == "" {
		return fmt.Errorf("runID is required")
	}
	if attempt <= 0 {
		return fmt.Errorf("attempt must be > 0")
	}
	if status == "" {
		status = "failed"
	}
	const q = `
UPDATE run_attempts
SET status = ?, ended_at = ?, error = ?
WHERE run_id = ? AND attempt = ?;
`
	_, err := s.db.ExecContext(ctx, q, status, time.Now().UTC().Format(time.RFC3339Nano), errText, runID, attempt)
	if err != nil {
		return fmt.Errorf("finish attempt: %w", err)
	}
	return nil
}

func (s *SQLiteAttemptStore) ListAttempts(ctx context.Context, runID string, limit int) ([]AttemptRecord, error) {
	if strings.TrimSpace(runID) == "" {
		return nil, fmt.Errorf("runID is required")
	}
	if limit <= 0 {
		limit = 50
	}
	const q = `
SELECT run_id, attempt, worker_id, status, started_at, ended_at, error, metadata
FROM run_attempts
WHERE run_id = ?
ORDER BY attempt DESC
LIMIT ?;
`
	rows, err := s.db.QueryContext(ctx, q, runID, limit)
	if err != nil {
		return nil, fmt.Errorf("list attempts: %w", err)
	}
	defer rows.Close()
	out := make([]AttemptRecord, 0, limit)
	for rows.Next() {
		var (
			r        AttemptRecord
			started  string
			ended    sql.NullString
			metadata string
		)
		if err := rows.Scan(&r.RunID, &r.Attempt, &r.WorkerID, &r.Status, &started, &ended, &r.Error, &metadata); err != nil {
			return nil, fmt.Errorf("scan attempt: %w", err)
		}
		r.StartedAt = parseTime(started)
		if ended.Valid {
			t := parseTime(ended.String)
			r.EndedAt = &t
		}
		_ = json.Unmarshal([]byte(metadata), &r.Metadata)
		if r.Metadata == nil {
			r.Metadata = map[string]any{}
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate attempts: %w", err)
	}
	return out, nil
}

func (s *SQLiteAttemptStore) SaveWorkerHeartbeat(ctx context.Context, heartbeat WorkerHeartbeat) error {
	if heartbeat.WorkerID == "" {
		return fmt.Errorf("workerID is required")
	}
	if heartbeat.Status == "" {
		heartbeat.Status = "online"
	}
	if heartbeat.LastSeenAt.IsZero() {
		heartbeat.LastSeenAt = time.Now().UTC()
	}
	if heartbeat.Metadata == nil {
		heartbeat.Metadata = map[string]any{}
	}
	metaRaw, err := json.Marshal(heartbeat.Metadata)
	if err != nil {
		return fmt.Errorf("encode heartbeat metadata: %w", err)
	}
	const q = `
INSERT INTO worker_heartbeats (worker_id, status, last_seen_at, capacity, metadata)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(worker_id) DO UPDATE SET
  status=excluded.status,
  last_seen_at=excluded.last_seen_at,
  capacity=excluded.capacity,
  metadata=excluded.metadata;
`
	_, err = s.db.ExecContext(
		ctx,
		q,
		heartbeat.WorkerID,
		heartbeat.Status,
		heartbeat.LastSeenAt.UTC().Format(time.RFC3339Nano),
		heartbeat.Capacity,
		string(metaRaw),
	)
	if err != nil {
		return fmt.Errorf("save heartbeat: %w", err)
	}
	return nil
}

func (s *SQLiteAttemptStore) ListWorkerHeartbeats(ctx context.Context, limit int) ([]WorkerHeartbeat, error) {
	if limit <= 0 {
		limit = 100
	}
	const q = `
SELECT worker_id, status, last_seen_at, capacity, metadata
FROM worker_heartbeats
ORDER BY last_seen_at DESC
LIMIT ?;
`
	rows, err := s.db.QueryContext(ctx, q, limit)
	if err != nil {
		return nil, fmt.Errorf("list worker heartbeats: %w", err)
	}
	defer rows.Close()
	out := make([]WorkerHeartbeat, 0, limit)
	for rows.Next() {
		var (
			h        WorkerHeartbeat
			lastSeen string
			metadata string
		)
		if err := rows.Scan(&h.WorkerID, &h.Status, &lastSeen, &h.Capacity, &metadata); err != nil {
			return nil, fmt.Errorf("scan heartbeat: %w", err)
		}
		h.LastSeenAt = parseTime(lastSeen)
		_ = json.Unmarshal([]byte(metadata), &h.Metadata)
		if h.Metadata == nil {
			h.Metadata = map[string]any{}
		}
		out = append(out, h)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate heartbeats: %w", err)
	}
	return out, nil
}

func (s *SQLiteAttemptStore) SaveQueueEvent(ctx context.Context, event QueueEvent) error {
	if event.Event == "" {
		return fmt.Errorf("event is required")
	}
	if event.At.IsZero() {
		event.At = time.Now().UTC()
	}
	if event.Payload == nil {
		event.Payload = map[string]any{}
	}
	payloadRaw, err := json.Marshal(event.Payload)
	if err != nil {
		return fmt.Errorf("encode queue event payload: %w", err)
	}
	const q = `
INSERT INTO queue_events (run_id, event, at, payload)
VALUES (?, ?, ?, ?);
`
	_, err = s.db.ExecContext(ctx, q, event.RunID, event.Event, event.At.UTC().Format(time.RFC3339Nano), string(payloadRaw))
	if err != nil {
		return fmt.Errorf("save queue event: %w", err)
	}
	return nil
}

func (s *SQLiteAttemptStore) ListQueueEvents(ctx context.Context, runID string, limit int) ([]QueueEvent, error) {
	if limit <= 0 {
		limit = 100
	}
	base := `
SELECT id, run_id, event, at, payload
FROM queue_events
`
	args := []any{}
	if strings.TrimSpace(runID) != "" {
		base += "WHERE run_id = ?\n"
		args = append(args, runID)
	}
	base += "ORDER BY at DESC LIMIT ?;"
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, base, args...)
	if err != nil {
		return nil, fmt.Errorf("list queue events: %w", err)
	}
	defer rows.Close()
	out := make([]QueueEvent, 0, limit)
	for rows.Next() {
		var (
			e       QueueEvent
			atRaw   string
			payload string
		)
		if err := rows.Scan(&e.ID, &e.RunID, &e.Event, &atRaw, &payload); err != nil {
			return nil, fmt.Errorf("scan queue event: %w", err)
		}
		e.At = parseTime(atRaw)
		_ = json.Unmarshal([]byte(payload), &e.Payload)
		if e.Payload == nil {
			e.Payload = map[string]any{}
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate queue events: %w", err)
	}
	return out, nil
}

func (s *SQLiteAttemptStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func parseTime(raw string) time.Time {
	if raw == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}
	}
	return t
}

var _ AttemptStore = (*SQLiteAttemptStore)(nil)
