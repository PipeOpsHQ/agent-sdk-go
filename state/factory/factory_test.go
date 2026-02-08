package factory

import (
	"context"
	"path/filepath"
	"testing"
)

func TestFromEnv_SQLite(t *testing.T) {
	t.Setenv("AGENT_STATE_BACKEND", "sqlite")
	t.Setenv("AGENT_SQLITE_PATH", filepath.Join(t.TempDir(), "state.db"))

	s, err := FromEnv(context.Background())
	if err != nil {
		t.Fatalf("FromEnv sqlite failed: %v", err)
	}
	if s == nil {
		t.Fatalf("expected sqlite store")
	}
	defer s.Close()
}

func TestFromEnv_HybridFallsBackWhenRedisUnavailable(t *testing.T) {
	t.Setenv("AGENT_STATE_BACKEND", "hybrid")
	t.Setenv("AGENT_SQLITE_PATH", filepath.Join(t.TempDir(), "state.db"))
	t.Setenv("AGENT_REDIS_ADDR", "127.0.0.1:1")

	s, err := FromEnv(context.Background())
	if err != nil {
		t.Fatalf("FromEnv hybrid failed unexpectedly: %v", err)
	}
	if s == nil {
		t.Fatalf("expected hybrid store")
	}
	defer s.Close()
}

func TestFromEnv_InvalidBackend(t *testing.T) {
	t.Setenv("AGENT_STATE_BACKEND", "nope")
	if _, err := FromEnv(context.Background()); err == nil {
		t.Fatalf("expected error for invalid backend")
	}
}
