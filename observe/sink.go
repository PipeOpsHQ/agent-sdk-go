package observe

import (
	"context"
	"sync"
)

type Sink interface {
	Emit(ctx context.Context, event Event) error
}

type SinkFunc func(ctx context.Context, event Event) error

func (f SinkFunc) Emit(ctx context.Context, event Event) error {
	if f == nil {
		return nil
	}
	return f(ctx, event)
}

type NoopSink struct{}

func (NoopSink) Emit(ctx context.Context, event Event) error {
	_ = ctx
	_ = event
	return nil
}

type MultiSink struct {
	sinks []Sink
}

func NewMultiSink(sinks ...Sink) Sink {
	filtered := make([]Sink, 0, len(sinks))
	for _, s := range sinks {
		if s == nil {
			continue
		}
		filtered = append(filtered, s)
	}
	if len(filtered) == 0 {
		return NoopSink{}
	}
	if len(filtered) == 1 {
		return filtered[0]
	}
	return &MultiSink{sinks: filtered}
}

func (m *MultiSink) Emit(ctx context.Context, event Event) error {
	if m == nil {
		return nil
	}
	for _, sink := range m.sinks {
		if err := sink.Emit(ctx, event); err != nil {
			return err
		}
	}
	return nil
}

type AsyncSink struct {
	downstream Sink
	queue      chan Event
	once       sync.Once
}

func NewAsyncSink(downstream Sink, buffer int) *AsyncSink {
	if downstream == nil {
		downstream = NoopSink{}
	}
	if buffer <= 0 {
		buffer = 256
	}
	as := &AsyncSink{
		downstream: downstream,
		queue:      make(chan Event, buffer),
	}
	go as.loop()
	return as
}

func (s *AsyncSink) Emit(ctx context.Context, event Event) error {
	if s == nil {
		return nil
	}
	event.Normalize()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case s.queue <- event:
		return nil
	default:
		// Drop on pressure to avoid blocking runtime hot path.
		return nil
	}
}

func (s *AsyncSink) Close() {
	if s == nil {
		return
	}
	s.once.Do(func() { close(s.queue) })
}

func (s *AsyncSink) loop() {
	for event := range s.queue {
		_ = s.downstream.Emit(context.Background(), event)
	}
}
