package redisstreams

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/nitrocode/ai-agents/framework/runtime/queue"
)

const (
	defaultPrefix = "aiag:queue"
	defaultGroup  = "workers"
)

type Queue struct {
	client    *goredis.Client
	addr      string
	password  string
	db        int
	prefix    string
	group     string
	runStream string
	dlqStream string
}

type Option func(*Queue)

func WithClient(client *goredis.Client) Option {
	return func(q *Queue) {
		if client != nil {
			q.client = client
		}
	}
}

func WithPrefix(prefix string) Option {
	return func(q *Queue) {
		prefix = strings.TrimSpace(prefix)
		if prefix != "" {
			q.prefix = prefix
		}
	}
}

func WithGroup(group string) Option {
	return func(q *Queue) {
		group = strings.TrimSpace(group)
		if group != "" {
			q.group = group
		}
	}
}

func WithPassword(password string) Option {
	return func(q *Queue) { q.password = password }
}

func WithDB(db int) Option {
	return func(q *Queue) { q.db = db }
}

func New(addr string, opts ...Option) (*Queue, error) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return nil, fmt.Errorf("redis addr is required")
	}
	q := &Queue{
		addr:   addr,
		prefix: defaultPrefix,
		group:  defaultGroup,
	}
	for _, opt := range opts {
		opt(q)
	}
	if q.client == nil {
		q.client = goredis.NewClient(&goredis.Options{Addr: q.addr, Password: q.password, DB: q.db})
	}
	if err := q.client.Ping(context.Background()).Err(); err != nil {
		return nil, fmt.Errorf("redis ping failed: %w", err)
	}
	q.runStream = q.prefix + ":runs"
	q.dlqStream = q.prefix + ":runs:dlq"
	if err := q.ensureGroup(context.Background()); err != nil {
		return nil, err
	}
	return q, nil
}

func (q *Queue) ensureGroup(ctx context.Context) error {
	res := q.client.XGroupCreateMkStream(ctx, q.runStream, q.group, "0")
	if err := res.Err(); err != nil && !strings.Contains(strings.ToUpper(err.Error()), "BUSYGROUP") {
		return fmt.Errorf("failed to ensure redis stream group: %w", err)
	}
	return nil
}

func (q *Queue) Enqueue(ctx context.Context, task queue.Task) (string, error) {
	if task.RunID == "" {
		return "", fmt.Errorf("runID is required")
	}
	if task.Attempt <= 0 {
		task.Attempt = 1
	}
	if task.MaxAttempts <= 0 {
		task.MaxAttempts = 3
	}
	if task.EnqueuedAt.IsZero() {
		task.EnqueuedAt = time.Now().UTC()
	}
	if task.Metadata == nil {
		task.Metadata = map[string]any{}
	}
	payload, err := json.Marshal(task)
	if err != nil {
		return "", fmt.Errorf("failed to marshal queue task: %w", err)
	}
	id, err := q.client.XAdd(ctx, &goredis.XAddArgs{
		Stream: q.runStream,
		Values: map[string]any{"payload": string(payload)},
	}).Result()
	if err != nil {
		return "", fmt.Errorf("failed to enqueue task: %w", err)
	}
	return id, nil
}

func (q *Queue) Claim(ctx context.Context, consumer string, block time.Duration, count int) ([]queue.Delivery, error) {
	if strings.TrimSpace(consumer) == "" {
		return nil, fmt.Errorf("consumer is required")
	}
	if count <= 0 {
		count = 1
	}
	if block < 0 {
		block = 0
	}
	res, err := q.client.XReadGroup(ctx, &goredis.XReadGroupArgs{
		Group:    q.group,
		Consumer: consumer,
		Streams:  []string{q.runStream, ">"},
		Count:    int64(count),
		Block:    block,
	}).Result()
	if err != nil {
		if err == goredis.Nil {
			return []queue.Delivery{}, nil
		}
		return nil, fmt.Errorf("failed to claim tasks: %w", err)
	}
	out := make([]queue.Delivery, 0, count)
	for _, stream := range res {
		for _, msg := range stream.Messages {
			payload, _ := msg.Values["payload"].(string)
			if payload == "" {
				continue
			}
			var task queue.Task
			if err := json.Unmarshal([]byte(payload), &task); err != nil {
				_ = q.client.XAck(ctx, q.runStream, q.group, msg.ID).Err()
				continue
			}
			out = append(out, queue.Delivery{
				ID:       msg.ID,
				Stream:   stream.Stream,
				Task:     task,
				Received: time.Now().UTC(),
			})
		}
	}
	return out, nil
}

func (q *Queue) Ack(ctx context.Context, consumer string, messageIDs ...string) error {
	_ = consumer
	if len(messageIDs) == 0 {
		return nil
	}
	args := make([]string, 0, len(messageIDs))
	for _, id := range messageIDs {
		id = strings.TrimSpace(id)
		if id != "" {
			args = append(args, id)
		}
	}
	if len(args) == 0 {
		return nil
	}
	if err := q.client.XAck(ctx, q.runStream, q.group, args...).Err(); err != nil {
		return fmt.Errorf("failed to ack queue message: %w", err)
	}
	_ = q.client.XDel(ctx, q.runStream, args...).Err()
	return nil
}

func (q *Queue) Nack(ctx context.Context, consumer string, deliveries []queue.Delivery, reason string) error {
	_ = consumer
	_ = reason
	ids := make([]string, 0, len(deliveries))
	for _, d := range deliveries {
		if d.ID != "" {
			ids = append(ids, d.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	if err := q.client.XAck(ctx, q.runStream, q.group, ids...).Err(); err != nil {
		return fmt.Errorf("failed to nack messages: %w", err)
	}
	return nil
}

func (q *Queue) Requeue(ctx context.Context, task queue.Task, reason string, delay time.Duration) (string, error) {
	if delay > 0 {
		t := time.Now().UTC().Add(delay)
		task.NotBefore = &t
	}
	if task.Metadata == nil {
		task.Metadata = map[string]any{}
	}
	if reason != "" {
		task.Metadata["requeue_reason"] = reason
	}
	return q.Enqueue(ctx, task)
}

func (q *Queue) DeadLetter(ctx context.Context, delivery queue.Delivery, reason string) (string, error) {
	if delivery.Task.Metadata == nil {
		delivery.Task.Metadata = map[string]any{}
	}
	delivery.Task.Metadata["dead_letter_reason"] = reason
	payload, err := json.Marshal(delivery.Task)
	if err != nil {
		return "", fmt.Errorf("failed to marshal dead letter task: %w", err)
	}
	id, err := q.client.XAdd(ctx, &goredis.XAddArgs{
		Stream: q.dlqStream,
		Values: map[string]any{
			"payload":   string(payload),
			"source_id": delivery.ID,
			"reason":    reason,
		},
	}).Result()
	if err != nil {
		return "", fmt.Errorf("failed to move task to dlq: %w", err)
	}
	_ = q.Ack(ctx, "", delivery.ID)
	return id, nil
}

func (q *Queue) ListDLQ(ctx context.Context, limit int) ([]queue.Delivery, error) {
	if limit <= 0 {
		limit = 50
	}
	entries, err := q.client.XRevRangeN(ctx, q.dlqStream, "+", "-", int64(limit)).Result()
	if err != nil {
		if err == goredis.Nil {
			return []queue.Delivery{}, nil
		}
		return nil, fmt.Errorf("failed to list dlq entries: %w", err)
	}
	out := make([]queue.Delivery, 0, len(entries))
	for _, entry := range entries {
		payload, _ := entry.Values["payload"].(string)
		if payload == "" {
			continue
		}
		var task queue.Task
		if err := json.Unmarshal([]byte(payload), &task); err != nil {
			continue
		}
		out = append(out, queue.Delivery{ID: entry.ID, Stream: q.dlqStream, Task: task, Received: time.Now().UTC()})
	}
	return out, nil
}

func (q *Queue) Stats(ctx context.Context) (queue.Stats, error) {
	runLen, err := q.client.XLen(ctx, q.runStream).Result()
	if err != nil && err != goredis.Nil {
		return queue.Stats{}, fmt.Errorf("failed to read queue length: %w", err)
	}
	dlqLen, err := q.client.XLen(ctx, q.dlqStream).Result()
	if err != nil && err != goredis.Nil {
		return queue.Stats{}, fmt.Errorf("failed to read dlq length: %w", err)
	}
	pending := int64(0)
	pendingRes, err := q.client.XPending(ctx, q.runStream, q.group).Result()
	if err == nil {
		pending = pendingRes.Count
	}
	return queue.Stats{StreamLength: runLen, DLQLength: dlqLen, Pending: pending}, nil
}

func (q *Queue) RequeueDLQByID(ctx context.Context, id string, resetAttempt bool) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", fmt.Errorf("id is required")
	}
	entries, err := q.client.XRangeN(ctx, q.dlqStream, id, id, 1).Result()
	if err != nil {
		return "", fmt.Errorf("failed to load dlq entry: %w", err)
	}
	if len(entries) == 0 {
		return "", fmt.Errorf("dlq entry %q not found", id)
	}
	payload, _ := entries[0].Values["payload"].(string)
	if payload == "" {
		return "", fmt.Errorf("dlq entry %q has empty payload", id)
	}
	var task queue.Task
	if err := json.Unmarshal([]byte(payload), &task); err != nil {
		return "", fmt.Errorf("failed to decode dlq payload: %w", err)
	}
	if resetAttempt {
		task.Attempt = 1
	} else {
		task.Attempt = max(task.Attempt, 1)
	}
	task.EnqueuedAt = time.Now().UTC()
	newID, err := q.Enqueue(ctx, task)
	if err != nil {
		return "", err
	}
	_ = q.client.XDel(ctx, q.dlqStream, id).Err()
	return newID, nil
}

func (q *Queue) Close() error {
	if q == nil || q.client == nil {
		return nil
	}
	return q.client.Close()
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

var _ queue.Queue = (*Queue)(nil)
