package api

import "context"

type PlaygroundRequest struct {
	Input        string   `json:"input"`
	SessionID    string   `json:"sessionId,omitempty"`
	Flow         string   `json:"flow,omitempty"`
	Workflow     string   `json:"workflow,omitempty"`
	WorkflowFile string   `json:"workflowFile,omitempty"`
	Tools        []string `json:"tools,omitempty"`
	Skills       []string `json:"skills,omitempty"`
	SystemPrompt string   `json:"systemPrompt,omitempty"`
}

type PlaygroundResponse struct {
	Output    string `json:"output,omitempty"`
	RunID     string `json:"runId,omitempty"`
	SessionID string `json:"sessionId,omitempty"`
	Provider  string `json:"provider,omitempty"`
	Status    string `json:"status"`
	Error     string `json:"error,omitempty"`
}

type PlaygroundRunner interface {
	Run(ctx context.Context, req PlaygroundRequest) (PlaygroundResponse, error)
}
