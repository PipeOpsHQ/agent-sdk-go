package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/nitrocode/ai-agents/framework/observe"
	observestore "github.com/nitrocode/ai-agents/framework/observe/store"
)

func TestStore_SaveListAndMetrics(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "trace.db")
	store, err := New(dbPath)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer func() { _ = store.Close() }()

	ctx := context.Background()
	now := time.Now().UTC()
	inputs := []observe.Event{
		{RunID: "r1", SessionID: "s1", Kind: observe.KindRun, Status: observe.StatusStarted, Timestamp: now},
		{RunID: "r1", SessionID: "s1", Kind: observe.KindProvider, Status: observe.StatusCompleted, Timestamp: now.Add(time.Millisecond)},
		{RunID: "r1", SessionID: "s1", Kind: observe.KindTool, Status: observe.StatusCompleted, Timestamp: now.Add(2 * time.Millisecond)},
		{RunID: "r1", SessionID: "s1", Kind: observe.KindRun, Status: observe.StatusCompleted, Timestamp: now.Add(3 * time.Millisecond)},
	}
	for _, in := range inputs {
		if err := store.SaveEvent(ctx, in); err != nil {
			t.Fatalf("save event: %v", err)
		}
	}

	events, err := store.ListEventsByRun(ctx, "r1", observestore.ListQuery{Limit: 20})
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(events) != len(inputs) {
		t.Fatalf("expected %d events, got %d", len(inputs), len(events))
	}

	metrics, err := store.AggregateMetrics(ctx, observestore.MetricsQuery{})
	if err != nil {
		t.Fatalf("aggregate metrics: %v", err)
	}
	if metrics.RunsStarted != 1 || metrics.RunsCompleted != 1 || metrics.ToolCalls != 1 || metrics.ProviderCalls != 1 {
		t.Fatalf("unexpected metrics: %+v", metrics)
	}
}
