package workflow

import (
	"fmt"
	"sort"
	"sync"

	"github.com/PipeOpsHQ/agent-sdk-go/graph"
	"github.com/PipeOpsHQ/agent-sdk-go/state"
)

type Builder interface {
	Name() string
	Description() string
	NewExecutor(runner graph.AgentRunner, store state.Store, sessionID string) (*graph.Executor, error)
}

var (
	mu       sync.RWMutex
	builders = map[string]Builder{}
)

func Register(b Builder) error {
	if b == nil {
		return fmt.Errorf("workflow builder is nil")
	}
	name := b.Name()
	if name == "" {
		return fmt.Errorf("workflow name is required")
	}
	mu.Lock()
	defer mu.Unlock()
	if _, exists := builders[name]; exists {
		return fmt.Errorf("workflow %q already registered", name)
	}
	builders[name] = b
	return nil
}

func MustRegister(b Builder) {
	if err := Register(b); err != nil {
		panic(err)
	}
}

func Get(name string) (Builder, bool) {
	mu.RLock()
	defer mu.RUnlock()
	b, ok := builders[name]
	return b, ok
}

func Names() []string {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]string, 0, len(builders))
	for name := range builders {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
