package api

import (
	"sync"

	"github.com/nitrocode/ai-agents/framework/observe"
)

type eventStream struct {
	mu       sync.RWMutex
	nextID   int
	watchers map[int]chan observe.Event
}

func newEventStream() *eventStream {
	return &eventStream{watchers: map[int]chan observe.Event{}}
}

func (s *eventStream) subscribe(buffer int) (int, <-chan observe.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if buffer <= 0 {
		buffer = 64
	}
	id := s.nextID
	s.nextID++
	ch := make(chan observe.Event, buffer)
	s.watchers[id] = ch
	return id, ch
}

func (s *eventStream) unsubscribe(id int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ch, ok := s.watchers[id]; ok {
		delete(s.watchers, id)
		close(ch)
	}
}

func (s *eventStream) publish(event observe.Event) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, ch := range s.watchers {
		select {
		case ch <- event:
		default:
		}
	}
}
