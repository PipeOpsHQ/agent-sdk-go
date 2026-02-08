package llm

import (
	"context"
	"errors"

	"github.com/nitrocode/ai-agents/framework/types"
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
