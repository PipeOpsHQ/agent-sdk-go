// Package otel bridges the observe.Sink to OpenTelemetry tracing.
//
// It converts framework observe.Event objects into OTel spans so that
// agent runs, tool calls, and provider interactions are visible in
// any OpenTelemetry-compatible backend (Jaeger, Zipkin, Grafana, etc.).
package otel

import (
	"context"
	"fmt"
	"time"

	"github.com/PipeOpsHQ/agent-sdk-go/observe"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

const instrumentationName = "github.com/PipeOpsHQ/agent-sdk-go/framework"

// Sink implements observe.Sink by emitting OpenTelemetry spans.
type Sink struct {
	tracer trace.Tracer
}

// NewSink creates an OTel sink using the given TracerProvider.
// If tp is nil, it uses a noop tracer provider.
func NewSink(tp trace.TracerProvider) *Sink {
	if tp == nil {
		tp = noop.NewTracerProvider()
	}
	return &Sink{
		tracer: tp.Tracer(instrumentationName),
	}
}

// Emit converts an observe.Event into an OTel span.
func (s *Sink) Emit(_ context.Context, event observe.Event) error {
	event.Normalize()

	spanName := spanNameFor(event)
	ctx := context.Background()
	startTime := event.Timestamp

	_, span := s.tracer.Start(ctx, spanName, trace.WithTimestamp(startTime))

	// Core attributes
	attrs := []attribute.KeyValue{
		attribute.String("agent.event.kind", string(event.Kind)),
	}
	if event.RunID != "" {
		attrs = append(attrs, attribute.String("agent.run.id", event.RunID))
	}
	if event.SessionID != "" {
		attrs = append(attrs, attribute.String("agent.session.id", event.SessionID))
	}
	if event.SpanID != "" {
		attrs = append(attrs, attribute.String("agent.span.id", event.SpanID))
	}
	if event.ParentSpanID != "" {
		attrs = append(attrs, attribute.String("agent.parent_span.id", event.ParentSpanID))
	}
	if event.Provider != "" {
		attrs = append(attrs, attribute.String("agent.provider", event.Provider))
	}
	if event.ToolName != "" {
		attrs = append(attrs, attribute.String("agent.tool.name", event.ToolName))
	}
	if event.Name != "" {
		attrs = append(attrs, attribute.String("agent.event.name", event.Name))
	}
	if event.Status != "" {
		attrs = append(attrs, attribute.String("agent.status", string(event.Status)))
	}
	if event.Message != "" {
		attrs = append(attrs, attribute.String("agent.message", truncate(event.Message, 1024)))
	}
	if event.DurationMs > 0 {
		attrs = append(attrs, attribute.Int64("agent.duration_ms", event.DurationMs))
	}

	// Custom attributes from event
	for k, v := range event.Attributes {
		attrs = append(attrs, attribute.String("agent.attr."+k, fmt.Sprintf("%v", v)))
	}

	span.SetAttributes(attrs...)

	// Mark span as error if the event represents a failure
	if event.Status == observe.StatusFailed {
		span.SetStatus(codes.Error, event.Error)
		if event.Error != "" {
			span.RecordError(fmt.Errorf("%s", event.Error))
		}
	} else if event.Status == observe.StatusCompleted {
		span.SetStatus(codes.Ok, "")
	}

	// End span with computed end time
	endTime := startTime
	if event.DurationMs > 0 {
		endTime = startTime.Add(time.Duration(event.DurationMs) * time.Millisecond)
	}
	span.End(trace.WithTimestamp(endTime))
	return nil
}

func spanNameFor(event observe.Event) string {
	switch event.Kind {
	case observe.KindRun:
		return "agent.run"
	case observe.KindProvider:
		if event.Provider != "" {
			return "agent.llm." + event.Provider
		}
		return "agent.llm.generate"
	case observe.KindTool:
		if event.ToolName != "" {
			return "agent.tool." + event.ToolName
		}
		return "agent.tool.call"
	case observe.KindGraph:
		if event.Name != "" {
			return "agent.graph." + event.Name
		}
		return "agent.graph.step"
	case observe.KindCheckpoint:
		return "agent.checkpoint"
	default:
		if event.Name != "" {
			return "agent." + event.Name
		}
		return "agent.event"
	}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
