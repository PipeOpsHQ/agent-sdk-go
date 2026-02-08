package factory

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/nitrocode/ai-agents/framework/state"
	"github.com/nitrocode/ai-agents/framework/state/hybrid"
	redisstore "github.com/nitrocode/ai-agents/framework/state/redis"
	sqlitestore "github.com/nitrocode/ai-agents/framework/state/sqlite"
)

func FromEnv(ctx context.Context) (state.Store, error) {
	_ = ctx

	backend := strings.ToLower(strings.TrimSpace(getenv("AGENT_STATE_BACKEND", "sqlite")))
	switch backend {
	case "sqlite":
		path := getenv("AGENT_SQLITE_PATH", "./.ai-agent/state.db")
		return sqlitestore.New(path)

	case "redis":
		return newRedisStoreFromEnv()

	case "hybrid":
		path := getenv("AGENT_SQLITE_PATH", "./.ai-agent/state.db")
		durable, err := sqlitestore.New(path)
		if err != nil {
			return nil, err
		}
		cache, err := newRedisStoreFromEnv()
		if err != nil {
			return hybrid.New(durable, nil)
		}
		return hybrid.New(durable, cache)

	default:
		return nil, fmt.Errorf("unsupported AGENT_STATE_BACKEND %q (use sqlite, redis, or hybrid)", backend)
	}
}

func newRedisStoreFromEnv() (state.Store, error) {
	addr := getenv("AGENT_REDIS_ADDR", "127.0.0.1:6379")
	password := strings.TrimSpace(os.Getenv("AGENT_REDIS_PASSWORD"))
	db := getenvInt("AGENT_REDIS_DB", 0)
	ttl := getenvDuration("AGENT_REDIS_TTL", 72*time.Hour)

	opts := []redisstore.Option{
		redisstore.WithPassword(password),
		redisstore.WithDB(db),
		redisstore.WithTTL(ttl),
	}
	return redisstore.New(addr, opts...)
}

func getenv(key, fallback string) string {
	val := strings.TrimSpace(os.Getenv(key))
	if val == "" {
		return fallback
	}
	return val
}

func getenvInt(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return n
}

func getenvDuration(key string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return fallback
	}
	return d
}
