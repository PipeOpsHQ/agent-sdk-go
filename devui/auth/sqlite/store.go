package sqlite

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/PipeOpsHQ/agent-sdk-go/devui/auth"
	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaSQL string

type Store struct {
	db *sql.DB
}

func New(path string) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("auth sqlite path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("failed to create auth db dir: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("failed to open auth sqlite db: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if _, err := db.ExecContext(context.Background(), "PRAGMA journal_mode=WAL;"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to enable wal: %w", err)
	}
	if _, err := db.ExecContext(context.Background(), schemaSQL); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to initialize auth schema: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) CreateKey(ctx context.Context, role auth.Role) (auth.KeyWithSecret, error) {
	if !role.Valid() {
		return auth.KeyWithSecret{}, fmt.Errorf("invalid role %q", role)
	}
	secret, err := generateSecret()
	if err != nil {
		return auth.KeyWithSecret{}, err
	}
	now := time.Now().UTC()
	id := uuid.NewString()
	const q = `INSERT INTO api_keys (id, key_hash, role, created_at) VALUES (?, ?, ?, ?);`
	if _, err := s.db.ExecContext(ctx, q, id, hashSecret(secret), string(role), now.Format(time.RFC3339Nano)); err != nil {
		return auth.KeyWithSecret{}, fmt.Errorf("create key: %w", err)
	}
	return auth.KeyWithSecret{
		APIKey: auth.APIKey{ID: id, Role: role, CreatedAt: now},
		Secret: secret,
	}, nil
}

func (s *Store) EnsureKey(ctx context.Context, secret string, role auth.Role) (auth.APIKey, error) {
	if strings.TrimSpace(secret) == "" {
		return auth.APIKey{}, fmt.Errorf("api key is required")
	}
	if !role.Valid() {
		return auth.APIKey{}, fmt.Errorf("invalid role %q", role)
	}

	keyHash := hashSecret(secret)
	if existing, err := s.lookupByHash(ctx, keyHash); err == nil {
		if existing.DisabledAt != nil {
			return auth.APIKey{}, fmt.Errorf("api key is disabled")
		}
		return existing, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return auth.APIKey{}, fmt.Errorf("ensure key lookup: %w", err)
	}

	now := time.Now().UTC()
	id := uuid.NewString()
	const q = `INSERT INTO api_keys (id, key_hash, role, created_at) VALUES (?, ?, ?, ?);`
	if _, err := s.db.ExecContext(ctx, q, id, keyHash, string(role), now.Format(time.RFC3339Nano)); err != nil {
		existing, lookupErr := s.lookupByHash(ctx, keyHash)
		if lookupErr == nil {
			if existing.DisabledAt != nil {
				return auth.APIKey{}, fmt.Errorf("api key is disabled")
			}
			return existing, nil
		}
		return auth.APIKey{}, fmt.Errorf("ensure key: %w", err)
	}

	return auth.APIKey{ID: id, Role: role, CreatedAt: now}, nil
}

func (s *Store) ListKeys(ctx context.Context) ([]auth.APIKey, error) {
	const q = `
SELECT id, role, created_at, rotated_at, disabled_at
FROM api_keys
ORDER BY created_at DESC;
`
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("list keys: %w", err)
	}
	defer rows.Close()
	out := []auth.APIKey{}
	for rows.Next() {
		var (
			k          auth.APIKey
			createdRaw string
			rotatedRaw sql.NullString
			disRaw     sql.NullString
		)
		if err := rows.Scan(&k.ID, &k.Role, &createdRaw, &rotatedRaw, &disRaw); err != nil {
			return nil, fmt.Errorf("scan key: %w", err)
		}
		created := parseTime(createdRaw)
		k.CreatedAt = created
		if rotatedRaw.Valid {
			t := parseTime(rotatedRaw.String)
			k.RotatedAt = &t
		}
		if disRaw.Valid {
			t := parseTime(disRaw.String)
			k.DisabledAt = &t
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

func (s *Store) DisableKey(ctx context.Context, id string) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("key id is required")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := s.db.ExecContext(ctx, `UPDATE api_keys SET disabled_at = ? WHERE id = ?;`, now, id)
	if err != nil {
		return fmt.Errorf("disable key: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("key %q not found", id)
	}
	return nil
}

func (s *Store) VerifyKey(ctx context.Context, secret string) (auth.APIKey, error) {
	if strings.TrimSpace(secret) == "" {
		return auth.APIKey{}, fmt.Errorf("api key is required")
	}
	k, err := s.lookupByHash(ctx, hashSecret(secret))
	if err != nil {
		if err == sql.ErrNoRows {
			return auth.APIKey{}, fmt.Errorf("invalid api key")
		}
		return auth.APIKey{}, fmt.Errorf("verify key: %w", err)
	}
	if k.DisabledAt != nil {
		return auth.APIKey{}, fmt.Errorf("api key is disabled")
	}
	return k, nil
}

func (s *Store) lookupByHash(ctx context.Context, keyHash string) (auth.APIKey, error) {
	const q = `
SELECT id, role, created_at, rotated_at, disabled_at
FROM api_keys
WHERE key_hash = ?;
`
	var (
		k          auth.APIKey
		createdRaw string
		rotatedRaw sql.NullString
		disRaw     sql.NullString
	)
	err := s.db.QueryRowContext(ctx, q, keyHash).Scan(&k.ID, &k.Role, &createdRaw, &rotatedRaw, &disRaw)
	if err != nil {
		return auth.APIKey{}, err
	}
	k.CreatedAt = parseTime(createdRaw)
	if rotatedRaw.Valid {
		t := parseTime(rotatedRaw.String)
		k.RotatedAt = &t
	}
	if disRaw.Valid {
		t := parseTime(disRaw.String)
		k.DisabledAt = &t
	}
	return k, nil
}

func hashSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

func generateSecret() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate api key: %w", err)
	}
	return "ak_" + base64.RawURLEncoding.EncodeToString(buf), nil
}

func parseTime(raw string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}
	}
	return t
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

var _ auth.Store = (*Store)(nil)
