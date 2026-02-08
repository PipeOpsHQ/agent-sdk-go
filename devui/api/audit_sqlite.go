package api

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type sqliteAuditStore struct {
	db *sql.DB
}

func NewSQLiteAuditStore(path string) (AuditStore, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("audit sqlite path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("failed to create audit db dir: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("failed to open audit db: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if _, err := db.ExecContext(context.Background(), `
CREATE TABLE IF NOT EXISTS audit_logs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  actor_key_id TEXT,
  action TEXT NOT NULL,
  resource TEXT NOT NULL,
  payload TEXT NOT NULL,
  created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_audit_logs_created_at ON audit_logs(created_at DESC);
`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to initialize audit schema: %w", err)
	}
	return &sqliteAuditStore{db: db}, nil
}

func (s *sqliteAuditStore) Record(ctx context.Context, entry AuditLog) error {
	if s == nil || s.db == nil {
		return nil
	}
	if entry.Action == "" || entry.Resource == "" {
		return nil
	}
	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO audit_logs (actor_key_id, action, resource, payload, created_at) VALUES (?, ?, ?, ?, ?);`,
		entry.ActorKeyID,
		entry.Action,
		entry.Resource,
		entry.Payload,
		time.Now().UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("record audit log: %w", err)
	}
	return nil
}

func (s *sqliteAuditStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}
