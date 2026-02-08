package sqlite

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

	"github.com/google/uuid"
	"github.com/nitrocode/ai-agents/framework/observe"
	observestore "github.com/nitrocode/ai-agents/framework/observe/store"
	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaSQL string

const defaultLimit = 200

type Store struct {
	db *sql.DB
}

func New(path string) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("sqlite trace path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("failed to create trace db dir: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("failed to open trace sqlite db: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if _, err := db.ExecContext(context.Background(), "PRAGMA journal_mode=WAL;"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to enable wal: %w", err)
	}
	if _, err := db.ExecContext(context.Background(), schemaSQL); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to initialize trace schema: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) SaveEvent(ctx context.Context, event observe.Event) error {
	if s == nil || s.db == nil {
		return nil
	}
	event.Normalize()
	if event.ID == "" {
		event.ID = uuid.NewString()
	}
	attrs, err := json.Marshal(event.Attributes)
	if err != nil {
		return fmt.Errorf("failed to encode trace attributes: %w", err)
	}
	const q = `
INSERT INTO trace_events (
  event_id, run_id, session_id, span_id, parent_span_id, kind, status, name, provider, tool_name,
  message, error, duration_ms, attributes, timestamp
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);
`
	_, err = s.db.ExecContext(
		ctx,
		q,
		event.ID,
		event.RunID,
		event.SessionID,
		event.SpanID,
		event.ParentSpanID,
		string(event.Kind),
		string(event.Status),
		event.Name,
		event.Provider,
		event.ToolName,
		event.Message,
		event.Error,
		event.DurationMs,
		string(attrs),
		event.Timestamp.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("failed to save trace event: %w", err)
	}
	return nil
}

func (s *Store) ListEventsByRun(ctx context.Context, runID string, query observestore.ListQuery) ([]observe.Event, error) {
	if strings.TrimSpace(runID) == "" {
		return nil, fmt.Errorf("runID is required")
	}
	return s.list(ctx, "run_id = ?", runID, query)
}

func (s *Store) ListEventsBySession(ctx context.Context, sessionID string, query observestore.ListQuery) ([]observe.Event, error) {
	if strings.TrimSpace(sessionID) == "" {
		return nil, fmt.Errorf("sessionID is required")
	}
	return s.list(ctx, "session_id = ?", sessionID, query)
}

func (s *Store) list(ctx context.Context, predicate string, value string, query observestore.ListQuery) ([]observe.Event, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	limit := query.Limit
	if limit <= 0 {
		limit = defaultLimit
	}
	offset := query.Offset
	if offset < 0 {
		offset = 0
	}

	q := fmt.Sprintf(`
SELECT event_id, run_id, session_id, span_id, parent_span_id, kind, status, name, provider, tool_name,
       message, error, duration_ms, attributes, timestamp
FROM trace_events
WHERE %s
ORDER BY timestamp ASC
LIMIT ? OFFSET ?;
`, predicate)

	rows, err := s.db.QueryContext(ctx, q, value, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to list trace events: %w", err)
	}
	defer rows.Close()

	out := make([]observe.Event, 0, limit)
	for rows.Next() {
		event, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate trace events: %w", err)
	}
	return out, nil
}

func scanEvent(scanner interface{ Scan(dest ...any) error }) (observe.Event, error) {
	var (
		e      observe.Event
		kind   string
		status string
		attrs  string
		tsRaw  string
	)
	if err := scanner.Scan(
		&e.ID,
		&e.RunID,
		&e.SessionID,
		&e.SpanID,
		&e.ParentSpanID,
		&kind,
		&status,
		&e.Name,
		&e.Provider,
		&e.ToolName,
		&e.Message,
		&e.Error,
		&e.DurationMs,
		&attrs,
		&tsRaw,
	); err != nil {
		return observe.Event{}, fmt.Errorf("failed to scan trace event: %w", err)
	}
	e.Kind = observe.Kind(kind)
	e.Status = observe.Status(status)
	if tsRaw != "" {
		ts, err := time.Parse(time.RFC3339Nano, tsRaw)
		if err == nil {
			e.Timestamp = ts
		}
	}
	if attrs != "" {
		_ = json.Unmarshal([]byte(attrs), &e.Attributes)
	}
	e.Normalize()
	return e, nil
}

func (s *Store) AggregateMetrics(ctx context.Context, query observestore.MetricsQuery) (observestore.MetricsSummary, error) {
	if s == nil || s.db == nil {
		return observestore.MetricsSummary{}, nil
	}
	args := []any{}
	where := ""
	if query.Since != nil {
		where = " WHERE timestamp >= ?"
		args = append(args, query.Since.UTC().Format(time.RFC3339Nano))
	}

	metrics := observestore.MetricsSummary{}
	counter := func(kind observe.Kind, status observe.Status) (int64, error) {
		q := "SELECT COUNT(*) FROM trace_events" + where + " AND kind = ? AND status = ?"
		if where == "" {
			q = "SELECT COUNT(*) FROM trace_events WHERE kind = ? AND status = ?"
		}
		qArgs := append([]any{}, args...)
		qArgs = append(qArgs, string(kind), string(status))
		var n int64
		if err := s.db.QueryRowContext(ctx, q, qArgs...).Scan(&n); err != nil {
			return 0, err
		}
		return n, nil
	}

	var err error
	if metrics.RunsStarted, err = counter(observe.KindRun, observe.StatusStarted); err != nil {
		return observestore.MetricsSummary{}, fmt.Errorf("metrics runs started: %w", err)
	}
	if metrics.RunsCompleted, err = counter(observe.KindRun, observe.StatusCompleted); err != nil {
		return observestore.MetricsSummary{}, fmt.Errorf("metrics runs completed: %w", err)
	}
	if metrics.RunsFailed, err = counter(observe.KindRun, observe.StatusFailed); err != nil {
		return observestore.MetricsSummary{}, fmt.Errorf("metrics runs failed: %w", err)
	}
	if metrics.ProviderCalls, err = counter(observe.KindProvider, observe.StatusCompleted); err != nil {
		return observestore.MetricsSummary{}, fmt.Errorf("metrics provider calls: %w", err)
	}
	if metrics.ProviderFailures, err = counter(observe.KindProvider, observe.StatusFailed); err != nil {
		return observestore.MetricsSummary{}, fmt.Errorf("metrics provider failures: %w", err)
	}
	if metrics.ToolCalls, err = counter(observe.KindTool, observe.StatusCompleted); err != nil {
		return observestore.MetricsSummary{}, fmt.Errorf("metrics tool calls: %w", err)
	}
	if metrics.ToolFailures, err = counter(observe.KindTool, observe.StatusFailed); err != nil {
		return observestore.MetricsSummary{}, fmt.Errorf("metrics tool failures: %w", err)
	}

	return metrics, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

var _ observestore.Store = (*Store)(nil)
