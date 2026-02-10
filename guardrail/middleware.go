package guardrail

import (
	"context"
	"fmt"
	"strings"

	agentfw "github.com/PipeOpsHQ/agent-sdk-go/agent"
)

// GuardrailError is returned when a guardrail blocks a request.
type GuardrailError struct {
	GuardrailName string
	Message       string
}

func (e *GuardrailError) Error() string {
	return fmt.Sprintf("guardrail %q blocked: %s", e.GuardrailName, e.Message)
}

// AgentMiddleware implements agent.Middleware to run guardrails before/after LLM calls.
type AgentMiddleware struct {
	agentfw.NoopMiddleware
	pipeline *Pipeline
}

// NewAgentMiddleware creates a middleware that enforces guardrails during agent execution.
func NewAgentMiddleware(pipeline *Pipeline) *AgentMiddleware {
	return &AgentMiddleware{pipeline: pipeline}
}

// BeforeGenerate runs input guardrails on the last user message.
func (m *AgentMiddleware) BeforeGenerate(ctx context.Context, event *agentfw.GenerateMiddlewareEvent) error {
	if err := m.NoopMiddleware.BeforeGenerate(ctx, event); err != nil {
		return err
	}
	if m.pipeline == nil || event == nil || event.Request == nil {
		return nil
	}

	// Find the last user message to check
	msgs := event.Request.Messages
	if len(msgs) == 0 {
		return nil
	}
	lastIdx := -1
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "user" {
			lastIdx = i
			break
		}
	}
	if lastIdx < 0 {
		return nil
	}

	text, results, err := m.pipeline.CheckInput(ctx, msgs[lastIdx].Content)
	if err != nil {
		return err
	}
	if HasBlock(results) {
		return &GuardrailError{
			GuardrailName: results[0].Name,
			Message:       results[0].Message,
		}
	}
	// Apply redacted text back
	if text != msgs[lastIdx].Content {
		event.Request.Messages[lastIdx].Content = text
	}
	return nil
}

// AfterGenerate runs output guardrails on the LLM response.
func (m *AgentMiddleware) AfterGenerate(ctx context.Context, event *agentfw.GenerateMiddlewareEvent) error {
	if err := m.NoopMiddleware.AfterGenerate(ctx, event); err != nil {
		return err
	}
	if m.pipeline == nil || event == nil || event.Response == nil {
		return nil
	}

	content := strings.TrimSpace(event.Response.Message.Content)
	if content == "" {
		return nil
	}

	text, results, err := m.pipeline.CheckOutput(ctx, content)
	if err != nil {
		return err
	}
	if HasBlock(results) {
		// Replace output with a safe message
		event.Response.Message.Content = "I'm unable to provide that response due to content policy restrictions."
		return nil
	}
	// Apply redacted text back
	if text != content {
		event.Response.Message.Content = text
	}
	return nil
}
