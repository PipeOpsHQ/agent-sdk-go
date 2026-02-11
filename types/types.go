package types

import (
	"encoding/json"
	"time"
)

type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

type Message struct {
	Role       Role       `json:"role"`
	Content    string     `json:"content,omitempty"`
	Reasoning  string     `json:"reasoning,omitempty"`
	Name       string     `json:"name,omitempty"` // Tool name for tool role messages.
	ToolCallID string     `json:"toolCallId,omitempty"`
	ToolCalls  []ToolCall `json:"toolCalls,omitempty"`
}

type ToolCall struct {
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

type ToolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	JSONSchema  map[string]any `json:"jsonSchema,omitempty"`
}

type Request struct {
	Model           string           `json:"model,omitempty"`
	SystemPrompt    string           `json:"systemPrompt,omitempty"`
	Messages        []Message        `json:"messages"`
	Tools           []ToolDefinition `json:"tools,omitempty"`
	MaxOutputTokens int              `json:"maxOutputTokens,omitempty"`
	ResponseSchema  map[string]any   `json:"responseSchema,omitempty"`
}

type Usage struct {
	InputTokens  int `json:"inputTokens,omitempty"`
	OutputTokens int `json:"outputTokens,omitempty"`
	TotalTokens  int `json:"totalTokens,omitempty"`
}

type Response struct {
	Message Message `json:"message"`
	Usage   *Usage  `json:"usage,omitempty"`
}

type StreamChunk struct {
	Text string `json:"text,omitempty"`
	Done bool   `json:"done,omitempty"`
}

type RunResult struct {
	Output      string     `json:"output"`
	Messages    []Message  `json:"messages,omitempty"`
	Usage       *Usage     `json:"usage,omitempty"`
	Iterations  int        `json:"iterations"`
	Provider    string     `json:"provider,omitempty"`
	RunID       string     `json:"runId,omitempty"`
	SessionID   string     `json:"sessionId,omitempty"`
	StartedAt   *time.Time `json:"startedAt,omitempty"`
	CompletedAt *time.Time `json:"completedAt,omitempty"`
	Events      []Event    `json:"events,omitempty"`
	NodeTrace   []string   `json:"nodeTrace,omitempty"`
}
