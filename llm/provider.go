package llm

import (
	"context"
	"errors"

	"github.com/PipeOpsHQ/agent-sdk-go/types"
)

var ErrNotSupported = errors.New("operation not supported by provider")

type Capabilities struct {
	Tools            bool
	Streaming        bool
	StructuredOutput bool
}

type Provider interface {
	Name() string
	Capabilities() Capabilities
	Generate(ctx context.Context, req types.Request) (types.Response, error)
}

// StreamProvider is an optional extension for providers that can emit
// incremental output chunks during generation.
type StreamProvider interface {
	GenerateStream(ctx context.Context, req types.Request, onChunk func(types.StreamChunk) error) (types.Response, error)
}
