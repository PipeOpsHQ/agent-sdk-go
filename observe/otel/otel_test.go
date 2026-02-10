package otel

import (
	"context"
	"testing"
	"time"

	"github.com/PipeOpsHQ/agent-sdk-go/observe"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestSinkEmitsSpans(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	defer tp.Shutdown(context.Background())

	sink := NewSink(tp)

	now := time.Now()
	err := sink.Emit(context.Background(), observe.Event{
		Kind:       observe.KindRun,
		RunID:      "run-123",
		SessionID:  "sess-456",
		Status:     observe.StatusCompleted,
		Timestamp:  now,
		DurationMs: 150,
	})
	if err != nil {
		t.Fatal(err)
	}

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}

	span := spans[0]
	if span.Name != "agent.run" {
		t.Errorf("expected span name 'agent.run', got %q", span.Name)
	}

	attrMap := attrToMap(span.Attributes)
	if v, ok := attrMap["agent.run.id"]; !ok || v != "run-123" {
		t.Errorf("missing or wrong agent.run.id: %v", attrMap)
	}
	if v, ok := attrMap["agent.session.id"]; !ok || v != "sess-456" {
		t.Errorf("missing or wrong agent.session.id: %v", attrMap)
	}
}

func TestSpanNaming(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	defer tp.Shutdown(context.Background())

	sink := NewSink(tp)
	now := time.Now()

	tests := []struct {
		event    observe.Event
		wantName string
	}{
		{observe.Event{Kind: observe.KindProvider, Provider: "openai", Timestamp: now}, "agent.llm.openai"},
		{observe.Event{Kind: observe.KindTool, ToolName: "web_search", Timestamp: now}, "agent.tool.web_search"},
		{observe.Event{Kind: observe.KindGraph, Name: "myflow", Timestamp: now}, "agent.graph.myflow"},
		{observe.Event{Kind: observe.KindCheckpoint, Timestamp: now}, "agent.checkpoint"},
		{observe.Event{Kind: observe.KindCustom, Name: "custom_event", Timestamp: now}, "agent.custom_event"},
	}

	for _, tt := range tests {
		exporter.Reset()
		sink.Emit(context.Background(), tt.event)
		spans := exporter.GetSpans()
		if len(spans) != 1 {
			t.Errorf("expected 1 span for %s, got %d", tt.wantName, len(spans))
			continue
		}
		if spans[0].Name != tt.wantName {
			t.Errorf("expected span name %q, got %q", tt.wantName, spans[0].Name)
		}
	}
}

func TestSinkErrorStatus(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	defer tp.Shutdown(context.Background())

	sink := NewSink(tp)
	sink.Emit(context.Background(), observe.Event{
		Kind:      observe.KindRun,
		Status:    observe.StatusFailed,
		Error:     "something went wrong",
		Timestamp: time.Now(),
	})

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	if len(spans[0].Events) == 0 {
		t.Error("expected error event recorded on span")
	}
}

func TestNilTracerProvider(t *testing.T) {
	sink := NewSink(nil)
	err := sink.Emit(context.Background(), observe.Event{
		Kind:      observe.KindRun,
		Timestamp: time.Now(),
	})
	if err != nil {
		t.Errorf("expected no error with nil provider, got: %v", err)
	}
}

func attrToMap(attrs []attribute.KeyValue) map[string]string {
	m := make(map[string]string, len(attrs))
	for _, a := range attrs {
		m[string(a.Key)] = a.Value.Emit()
	}
	return m
}
