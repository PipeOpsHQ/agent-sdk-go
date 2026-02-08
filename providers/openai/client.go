package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/PipeOpsHQ/agent-sdk-go/framework/llm"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/types"
)

const defaultModel = "gpt-4o-mini"

type Client struct {
	apiKey     string
	model      string
	baseURL    string
	httpClient *http.Client
}

type Option func(*Client)

func WithModel(model string) Option {
	return func(c *Client) { c.model = model }
}

func WithBaseURL(baseURL string) Option {
	return func(c *Client) { c.baseURL = strings.TrimRight(baseURL, "/") }
}

func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) {
		if h != nil {
			c.httpClient = h
		}
	}
}

func New(apiKey string, opts ...Option) (*Client, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY is required")
	}
	c := &Client{
		apiKey:  apiKey,
		model:   defaultModel,
		baseURL: "https://api.openai.com",
		httpClient: &http.Client{
			Timeout: 90 * time.Second,
		},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

func (c *Client) Name() string { return "openai" }

func (c *Client) Capabilities() llm.Capabilities {
	return llm.Capabilities{
		Tools:            true,
		Streaming:        false,
		StructuredOutput: true,
	}
}

func (c *Client) Generate(ctx context.Context, req types.Request) (types.Response, error) {
	model := c.model
	if req.Model != "" {
		model = req.Model
	}

	payload := openAIRequest{
		Model:    model,
		Messages: make([]openAIMessage, 0, len(req.Messages)+1),
	}
	if req.MaxOutputTokens > 0 {
		payload.MaxTokens = req.MaxOutputTokens
	}

	if req.SystemPrompt != "" {
		payload.Messages = append(payload.Messages, openAIMessage{
			Role:    "system",
			Content: req.SystemPrompt,
		})
	}
	payload.Messages = append(payload.Messages, toOpenAIMessages(req.Messages)...)

	if len(req.Tools) > 0 {
		payload.ToolChoice = "auto"
		payload.Tools = toOpenAITools(req.Tools)
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return types.Response{}, fmt.Errorf("failed to marshal openai request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return types.Response{}, fmt.Errorf("failed to create openai request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return types.Response{}, fmt.Errorf("openai request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return types.Response{}, fmt.Errorf("failed to read openai response: %w", err)
	}
	if resp.StatusCode >= 300 {
		return types.Response{}, fmt.Errorf("openai API error (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var apiResp openAIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return types.Response{}, fmt.Errorf("failed to decode openai response: %w", err)
	}
	if len(apiResp.Choices) == 0 {
		return types.Response{}, fmt.Errorf("openai response had no choices")
	}

	msg := apiResp.Choices[0].Message
	out := types.Message{
		Role:    types.RoleAssistant,
		Content: messageContentToString(msg.Content),
	}
	if len(msg.ToolCalls) > 0 {
		out.ToolCalls = make([]types.ToolCall, 0, len(msg.ToolCalls))
		for _, tc := range msg.ToolCalls {
			out.ToolCalls = append(out.ToolCalls, types.ToolCall{
				ID:        tc.ID,
				Name:      tc.Function.Name,
				Arguments: normalizeJSONArgs(tc.Function.Arguments),
			})
		}
	}

	var usage *types.Usage
	if apiResp.Usage.TotalTokens > 0 {
		usage = &types.Usage{
			InputTokens:  apiResp.Usage.PromptTokens,
			OutputTokens: apiResp.Usage.CompletionTokens,
			TotalTokens:  apiResp.Usage.TotalTokens,
		}
	}

	return types.Response{
		Message: out,
		Usage:   usage,
	}, nil
}

func toOpenAIMessages(in []types.Message) []openAIMessage {
	msgs := make([]openAIMessage, 0, len(in))
	for _, m := range in {
		switch m.Role {
		case types.RoleUser:
			msgs = append(msgs, openAIMessage{
				Role:    "user",
				Content: m.Content,
			})
		case types.RoleAssistant:
			out := openAIMessage{
				Role:    "assistant",
				Content: m.Content,
			}
			if len(m.ToolCalls) > 0 {
				out.ToolCalls = make([]openAIToolCall, 0, len(m.ToolCalls))
				for _, tc := range m.ToolCalls {
					args := "{}"
					if len(tc.Arguments) > 0 {
						args = string(tc.Arguments)
					}
					out.ToolCalls = append(out.ToolCalls, openAIToolCall{
						ID:   tc.ID,
						Type: "function",
						Function: openAIFunctionCall{
							Name:      tc.Name,
							Arguments: args,
						},
					})
				}
			}
			msgs = append(msgs, out)
		case types.RoleTool:
			msgs = append(msgs, openAIMessage{
				Role:       "tool",
				Name:       m.Name,
				ToolCallID: m.ToolCallID,
				Content:    m.Content,
			})
		}
	}
	return msgs
}

func toOpenAITools(in []types.ToolDefinition) []openAITool {
	tools := make([]openAITool, 0, len(in))
	for _, t := range in {
		params := t.JSONSchema
		if len(params) == 0 {
			params = map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			}
		}
		tools = append(tools, openAITool{
			Type: "function",
			Function: openAIToolFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  params,
			},
		})
	}
	return tools
}

func messageContentToString(content any) string {
	switch c := content.(type) {
	case string:
		return c
	case nil:
		return ""
	default:
		b, err := json.Marshal(c)
		if err != nil {
			return fmt.Sprintf("%v", c)
		}
		return string(b)
	}
}

func normalizeJSONArgs(raw string) json.RawMessage {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return json.RawMessage(`{}`)
	}
	if json.Valid([]byte(raw)) {
		return json.RawMessage(raw)
	}
	escaped, _ := json.Marshal(raw)
	return json.RawMessage(fmt.Sprintf(`{"raw":%s}`, string(escaped)))
}

type openAIRequest struct {
	Model      string          `json:"model"`
	Messages   []openAIMessage `json:"messages"`
	Tools      []openAITool    `json:"tools,omitempty"`
	ToolChoice string          `json:"tool_choice,omitempty"`
	MaxTokens  int             `json:"max_tokens,omitempty"`
}

type openAIMessage struct {
	Role       string           `json:"role"`
	Name       string           `json:"name,omitempty"`
	Content    any              `json:"content"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
}

type openAITool struct {
	Type     string             `json:"type"`
	Function openAIToolFunction `json:"function"`
}

type openAIToolFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type openAIToolCall struct {
	ID       string             `json:"id,omitempty"`
	Type     string             `json:"type,omitempty"`
	Function openAIFunctionCall `json:"function"`
}

type openAIFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type openAIResponse struct {
	Choices []struct {
		Message openAIMessage `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}
