package rag

import (
	"context"
	"fmt"
	"strings"

	agentfw "github.com/PipeOpsHQ/agent-sdk-go/agent"
	"github.com/PipeOpsHQ/agent-sdk-go/types"
)

// AgentMiddleware implements agent.Middleware to inject retrieved context before generation.
type AgentMiddleware struct {
	agentfw.NoopMiddleware
	retriever Retriever
	topK      int
	prefix    string // prefix for injected context; defaults to "Relevant context:"
}

// MiddlewareOption configures the RAG middleware.
type MiddlewareOption func(*AgentMiddleware)

// WithTopK sets the number of documents to retrieve (default 3).
func WithTopK(k int) MiddlewareOption {
	return func(m *AgentMiddleware) {
		if k > 0 {
			m.topK = k
		}
	}
}

// WithPrefix sets the label for injected context in the system prompt.
func WithPrefix(prefix string) MiddlewareOption {
	return func(m *AgentMiddleware) { m.prefix = prefix }
}

// NewAgentMiddleware creates a middleware that retrieves relevant documents
// and injects them into the system prompt before each LLM call.
func NewAgentMiddleware(retriever Retriever, opts ...MiddlewareOption) *AgentMiddleware {
	m := &AgentMiddleware{
		retriever: retriever,
		topK:      3,
		prefix:    "Relevant context:",
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// BeforeGenerate retrieves documents relevant to the last user message
// and prepends them to the system prompt.
func (m *AgentMiddleware) BeforeGenerate(ctx context.Context, event *agentfw.GenerateMiddlewareEvent) error {
	if err := m.NoopMiddleware.BeforeGenerate(ctx, event); err != nil {
		return err
	}
	if m.retriever == nil || event == nil || event.Request == nil {
		return nil
	}

	// Find the last user message as the query
	query := lastUserMessage(event.Request.Messages)
	if query == "" {
		return nil
	}

	results, err := m.retriever.Retrieve(ctx, query, m.topK)
	if err != nil {
		// Non-fatal: log and continue without context
		return nil
	}
	if len(results) == 0 {
		return nil
	}

	// Build context block
	var sb strings.Builder
	sb.WriteString(m.prefix)
	sb.WriteString("\n")
	for i, r := range results {
		sb.WriteString(fmt.Sprintf("\n[%d] (score: %.2f)\n%s\n", i+1, r.Score, r.Document.Content))
	}

	// Prepend to system prompt
	event.Request.SystemPrompt = sb.String() + "\n" + event.Request.SystemPrompt
	return nil
}

func lastUserMessage(msgs []types.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "user" && msgs[i].Content != "" {
			return msgs[i].Content
		}
	}
	return ""
}
