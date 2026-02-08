package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/PipeOpsHQ/agent-sdk-go/framework/state"
)

const (
	defaultTTL    = 72 * time.Hour
	defaultLimit  = 50
	defaultPrefix = "aiag"
)

type Store struct {
	client   *goredis.Client
	ttl      time.Duration
	prefix   string
	addr     string
	db       int
	password string
}

type Option func(*Store)

func WithPassword(password string) Option {
	return func(s *Store) {
		s.password = password
	}
}

func WithDB(db int) Option {
	return func(s *Store) {
		s.db = db
	}
}

func WithTTL(ttl time.Duration) Option {
	return func(s *Store) {
		if ttl > 0 {
			s.ttl = ttl
		}
	}
}

func WithPrefix(prefix string) Option {
	return func(s *Store) {
		if strings.TrimSpace(prefix) != "" {
			s.prefix = strings.TrimSpace(prefix)
		}
	}
}

func WithClient(client *goredis.Client) Option {
	return func(s *Store) {
		if client != nil {
			s.client = client
		}
	}
}

func New(addr string, opts ...Option) (*Store, error) {
	if strings.TrimSpace(addr) == "" {
		return nil, fmt.Errorf("redis addr is required")
	}

	s := &Store{
		ttl:    defaultTTL,
		prefix: defaultPrefix,
		addr:   addr,
	}
	for _, opt := range opts {
		opt(s)
	}
	if s.client == nil {
		s.client = goredis.NewClient(&goredis.Options{
			Addr:     s.addr,
			Password: s.password,
			DB:       s.db,
		})
	}

	if err := s.client.Ping(context.Background()).Err(); err != nil {
		return nil, fmt.Errorf("redis ping failed: %w", err)
	}

	return s, nil
}

func (s *Store) SaveRun(ctx context.Context, run state.RunRecord) error {
	if run.RunID == "" {
		return fmt.Errorf("run_id is required")
	}
	if run.SessionID == "" {
		return fmt.Errorf("session_id is required")
	}
	if run.UpdatedAt == nil {
		now := time.Now().UTC()
		run.UpdatedAt = &now
	}
	if run.CreatedAt == nil {
		now := time.Now().UTC()
		run.CreatedAt = &now
	}
	if run.Metadata == nil {
		run.Metadata = map[string]any{}
	}

	runRaw, err := json.Marshal(run)
	if err != nil {
		return fmt.Errorf("failed to marshal run: %w", err)
	}

	updatedUnix := float64(run.UpdatedAt.Unix())
	runKey := s.runKey(run.RunID)
	sessionIdx := s.sessionIndexKey(run.SessionID)

	pipe := s.client.TxPipeline()
	pipe.Set(ctx, runKey, string(runRaw), s.ttl)
	pipe.ZAdd(ctx, sessionIdx, goredis.Z{
		Score:  updatedUnix,
		Member: run.RunID,
	})
	pipe.Expire(ctx, sessionIdx, s.ttl)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("failed to save run in redis: %w", err)
	}
	return nil
}

func (s *Store) LoadRun(ctx context.Context, runID string) (state.RunRecord, error) {
	if runID == "" {
		return state.RunRecord{}, fmt.Errorf("run_id is required")
	}

	raw, err := s.client.Get(ctx, s.runKey(runID)).Result()
	if err != nil {
		if err == goredis.Nil {
			return state.RunRecord{}, state.ErrNotFound
		}
		return state.RunRecord{}, fmt.Errorf("failed to load run from redis: %w", err)
	}

	var run state.RunRecord
	if err := json.Unmarshal([]byte(raw), &run); err != nil {
		return state.RunRecord{}, fmt.Errorf("failed to decode run from redis: %w", err)
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

	ids := make([]string, 0, limit)
	if query.SessionID != "" {
		values, err := s.client.ZRevRange(ctx, s.sessionIndexKey(query.SessionID), int64(offset), int64(offset+limit-1)).Result()
		if err != nil {
			return nil, fmt.Errorf("failed to list run ids by session: %w", err)
		}
		ids = append(ids, values...)
	} else {
		var cursor uint64
		match := s.runPattern()
		for len(ids) < limit {
			keys, next, err := s.client.Scan(ctx, cursor, match, int64(limit)).Result()
			if err != nil {
				return nil, fmt.Errorf("failed to scan redis run keys: %w", err)
			}
			for _, key := range keys {
				if id := s.runIDFromKey(key); id != "" {
					ids = append(ids, id)
				}
				if len(ids) >= limit {
					break
				}
			}
			cursor = next
			if cursor == 0 {
				break
			}
		}
	}

	if len(ids) == 0 {
		return []state.RunRecord{}, nil
	}

	keys := make([]string, len(ids))
	for i, id := range ids {
		keys[i] = s.runKey(id)
	}

	loaded, err := s.client.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to mget runs from redis: %w", err)
	}

	out := make([]state.RunRecord, 0, len(loaded))
	staleIDs := make([]string, 0)
	for i, raw := range loaded {
		if raw == nil {
			staleIDs = append(staleIDs, ids[i])
			continue
		}
		var run state.RunRecord
		if err := json.Unmarshal([]byte(fmt.Sprintf("%v", raw)), &run); err != nil {
			continue
		}
		if query.Status != "" && run.Status != query.Status {
			continue
		}
		out = append(out, run)
	}

	if query.SessionID != "" && len(staleIDs) > 0 {
		members := make([]any, 0, len(staleIDs))
		for _, id := range staleIDs {
			members = append(members, id)
		}
		_ = s.client.ZRem(ctx, s.sessionIndexKey(query.SessionID), members...).Err()
	}

	sort.Slice(out, func(i, j int) bool {
		left := time.Time{}
		if out[i].UpdatedAt != nil {
			left = *out[i].UpdatedAt
		}
		right := time.Time{}
		if out[j].UpdatedAt != nil {
			right = *out[j].UpdatedAt
		}
		return left.After(right)
	})

	return out, nil
}

func (s *Store) SaveCheckpoint(ctx context.Context, checkpoint state.CheckpointRecord) error {
	if checkpoint.RunID == "" {
		return fmt.Errorf("run_id is required")
	}
	if checkpoint.State == nil {
		checkpoint.State = map[string]any{}
	}
	if checkpoint.CreatedAt.IsZero() {
		checkpoint.CreatedAt = time.Now().UTC()
	}

	raw, err := json.Marshal(checkpoint)
	if err != nil {
		return fmt.Errorf("failed to marshal checkpoint: %w", err)
	}

	seqKey := s.checkpointSeqKey(checkpoint.RunID, checkpoint.Seq)
	ok, err := s.client.SetNX(ctx, seqKey, string(raw), s.ttl).Result()
	if err != nil {
		return fmt.Errorf("failed to save checkpoint in redis: %w", err)
	}
	if !ok {
		return state.ErrConflict
	}

	latestKey := s.latestCheckpointKey(checkpoint.RunID)
	latestRaw, err := s.client.Get(ctx, latestKey).Result()
	if err != nil && err != goredis.Nil {
		return fmt.Errorf("failed to read latest checkpoint: %w", err)
	}

	updateLatest := true
	if err == nil && latestRaw != "" {
		var latest state.CheckpointRecord
		if json.Unmarshal([]byte(latestRaw), &latest) == nil && latest.Seq > checkpoint.Seq {
			updateLatest = false
		}
	}
	if updateLatest {
		if err := s.client.Set(ctx, latestKey, string(raw), s.ttl).Err(); err != nil {
			return fmt.Errorf("failed to set latest checkpoint: %w", err)
		}
	}
	return nil
}

func (s *Store) LoadLatestCheckpoint(ctx context.Context, runID string) (state.CheckpointRecord, error) {
	if runID == "" {
		return state.CheckpointRecord{}, fmt.Errorf("run_id is required")
	}

	raw, err := s.client.Get(ctx, s.latestCheckpointKey(runID)).Result()
	if err != nil {
		if err == goredis.Nil {
			return state.CheckpointRecord{}, state.ErrNotFound
		}
		return state.CheckpointRecord{}, fmt.Errorf("failed to load latest checkpoint: %w", err)
	}

	var checkpoint state.CheckpointRecord
	if err := json.Unmarshal([]byte(raw), &checkpoint); err != nil {
		return state.CheckpointRecord{}, fmt.Errorf("failed to decode checkpoint: %w", err)
	}
	return checkpoint, nil
}

func (s *Store) ListCheckpoints(ctx context.Context, runID string, limit int) ([]state.CheckpointRecord, error) {
	if runID == "" {
		return nil, fmt.Errorf("run_id is required")
	}
	if limit <= 0 {
		limit = defaultLimit
	}

	pattern := s.checkpointSeqPattern(runID)
	var (
		cursor uint64
		keys   []string
	)
	for len(keys) < limit {
		found, next, err := s.client.Scan(ctx, cursor, pattern, int64(limit)).Result()
		if err != nil {
			return nil, fmt.Errorf("failed to scan checkpoints: %w", err)
		}
		keys = append(keys, found...)
		cursor = next
		if cursor == 0 {
			break
		}
	}
	if len(keys) == 0 {
		return []state.CheckpointRecord{}, nil
	}

	values, err := s.client.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to load checkpoint values: %w", err)
	}
	out := make([]state.CheckpointRecord, 0, len(values))
	for _, raw := range values {
		if raw == nil {
			continue
		}
		var checkpoint state.CheckpointRecord
		if err := json.Unmarshal([]byte(fmt.Sprintf("%v", raw)), &checkpoint); err != nil {
			continue
		}
		out = append(out, checkpoint)
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].Seq > out[j].Seq
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *Store) AcquireRunLock(ctx context.Context, runID, owner string, ttl time.Duration) (bool, error) {
	if runID == "" || owner == "" {
		return false, fmt.Errorf("run_id and owner are required")
	}
	if ttl <= 0 {
		ttl = 15 * time.Second
	}
	ok, err := s.client.SetNX(ctx, s.lockKey(runID), owner, ttl).Result()
	if err != nil {
		return false, fmt.Errorf("failed to acquire run lock: %w", err)
	}
	return ok, nil
}

func (s *Store) ReleaseRunLock(ctx context.Context, runID, owner string) error {
	if runID == "" || owner == "" {
		return fmt.Errorf("run_id and owner are required")
	}

	script := goredis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
  return redis.call("DEL", KEYS[1])
end
return 0
`)
	if _, err := script.Run(ctx, s.client, []string{s.lockKey(runID)}, owner).Result(); err != nil {
		return fmt.Errorf("failed to release run lock: %w", err)
	}
	return nil
}

func (s *Store) Close() error {
	if s.client == nil {
		return nil
	}
	return s.client.Close()
}

func (s *Store) runKey(runID string) string {
	return fmt.Sprintf("%s:run:%s", s.prefix, runID)
}

func (s *Store) runPattern() string {
	return fmt.Sprintf("%s:run:*", s.prefix)
}

func (s *Store) runIDFromKey(key string) string {
	prefix := fmt.Sprintf("%s:run:", s.prefix)
	if !strings.HasPrefix(key, prefix) {
		return ""
	}
	return strings.TrimPrefix(key, prefix)
}

func (s *Store) sessionIndexKey(sessionID string) string {
	return fmt.Sprintf("%s:runidx:session:%s", s.prefix, sessionID)
}

func (s *Store) latestCheckpointKey(runID string) string {
	return fmt.Sprintf("%s:ckpt:latest:%s", s.prefix, runID)
}

func (s *Store) checkpointSeqKey(runID string, seq int) string {
	return fmt.Sprintf("%s:ckpt:%s:%d", s.prefix, runID, seq)
}

func (s *Store) checkpointSeqPattern(runID string) string {
	return fmt.Sprintf("%s:ckpt:%s:*", s.prefix, runID)
}

func (s *Store) lockKey(runID string) string {
	return fmt.Sprintf("%s:lock:run:%s", s.prefix, runID)
}

func parseSeqFromKey(key string) int {
	parts := strings.Split(key, ":")
	if len(parts) == 0 {
		return -1
	}
	seq, err := strconv.Atoi(parts[len(parts)-1])
	if err != nil {
		return -1
	}
	return seq
}
