package api

import (
	"context"

	"github.com/PipeOpsHQ/agent-sdk-go/delivery"
	"github.com/PipeOpsHQ/agent-sdk-go/types"
)

type PlaygroundRequest struct {
	Input        string           `json:"input"`
	SessionID    string           `json:"sessionId,omitempty"`
	History      []types.Message  `json:"history,omitempty"`
	PromptRef    string           `json:"promptRef,omitempty"`
	PromptInput  map[string]any   `json:"promptInput,omitempty"`
	Flow         string           `json:"flow,omitempty"`
	Workflow     string           `json:"workflow,omitempty"`
	WorkflowFile string           `json:"workflowFile,omitempty"`
	Tools        []string         `json:"tools,omitempty"`
	Skills       []string         `json:"skills,omitempty"`
	Guardrails   []string         `json:"guardrails,omitempty"`
	SystemPrompt string           `json:"systemPrompt,omitempty"`
	ReplyTo      *delivery.Target `json:"replyTo,omitempty"`
}

type PlaygroundResponse struct {
	Output        string           `json:"output,omitempty"`
	RunID         string           `json:"runId,omitempty"`
	SessionID     string           `json:"sessionId,omitempty"`
	Provider      string           `json:"provider,omitempty"`
	AppliedSkills []string         `json:"appliedSkills,omitempty"`
	Status        string           `json:"status"`
	Error         string           `json:"error,omitempty"`
	ReplyTo       *delivery.Target `json:"replyTo,omitempty"`
}

type PlaygroundRunner interface {
	Run(ctx context.Context, req PlaygroundRequest) (PlaygroundResponse, error)
}

type PlaygroundStreamRunner interface {
	RunStream(ctx context.Context, req PlaygroundRequest, onChunk func(types.StreamChunk) error) (PlaygroundResponse, error)
}
