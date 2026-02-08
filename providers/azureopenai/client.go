package azureopenai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/PipeOpsHQ/agent-sdk-go/framework/llm"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/types"
)

const defaultAPIVersion = "2024-10-21"

type Client struct {
	apiKey     string
	endpoint   string
	deployment string
	model      string
	apiVersion string
	httpClient *http.Client
}

type Option func(*Client)

func WithEndpoint(endpoint string) Option {
	return func(c *Client) { c.endpoint = strings.TrimRight(endpoint, "/") }
}

func WithDeployment(deployment string) Option {
	return func(c *Client) { c.deployment = deployment }
}

func WithModel(model string) Option {
	return func(c *Client) { c.model = model }
}

func WithAPIVersion(apiVersion string) Option {
	return func(c *Client) { c.apiVersion = apiVersion }
}

func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) {
		if h != nil {
			c.httpClient = h
		}
	}
}

func New(apiKey string, opts ...Option) (*Client, error) {
	if strings.TrimSpace(apiKey) == "" {
		return nil, fmt.Errorf("AZURE_OPENAI_API_KEY is required")
	}
	c := &Client{
		apiKey:     apiKey,
		apiVersion: defaultAPIVersion,
		httpClient: &http.Client{Timeout: 90 * time.Second},
	}
	for _, opt := range opts {
		opt(c)
	}
	if strings.TrimSpace(c.endpoint) == "" {
		return nil, fmt.Errorf("AZURE_OPENAI_ENDPOINT is required")
	}
	if strings.TrimSpace(c.deployment) == "" {
		return nil, fmt.Errorf("AZURE_OPENAI_DEPLOYMENT is required")
	}
	if strings.TrimSpace(c.model) == "" {
		c.model = c.deployment
	}
	return c, nil
}

func (c *Client) Name() string { return "azureopenai" }

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

	payload := azureChatRequest{
		Model:    model,
		Messages: make([]azureChatMessage, 0, len(req.Messages)+1),
	}
	if req.MaxOutputTokens > 0 {
		payload.MaxTokens = req.MaxOutputTokens
	}

	if req.SystemPrompt != "" {
		payload.Messages = append(payload.Messages, azureChatMessage{Role: "system", Content: req.SystemPrompt})
	}
	payload.Messages = append(payload.Messages, toAzureMessages(req.Messages)...)

	if len(req.Tools) > 0 {
		payload.ToolChoice = "auto"
		payload.Tools = toAzureTools(req.Tools)
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return types.Response{}, fmt.Errorf("failed to marshal azure openai request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpointForDeployment(), bytes.NewReader(raw))
	if err != nil {
		return types.Response{}, fmt.Errorf("failed to create azure openai request: %w", err)
	}
	httpReq.Header.Set("api-key", c.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return types.Response{}, fmt.Errorf("azure openai request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return types.Response{}, fmt.Errorf("failed to read azure openai response: %w", err)
	}
	if resp.StatusCode >= 300 {
		return types.Response{}, fmt.Errorf("azure openai API error (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var apiResp azureChatResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return types.Response{}, fmt.Errorf("failed to decode azure openai response: %w", err)
	}
	if len(apiResp.Choices) == 0 {
		return types.Response{}, fmt.Errorf("azure openai response had no choices")
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

	return types.Response{Message: out, Usage: usage}, nil
}

func (c *Client) endpointForDeployment() string {
	deployment := url.PathEscape(c.deployment)
	version := url.QueryEscape(c.apiVersion)
	return fmt.Sprintf("%s/openai/deployments/%s/chat/completions?api-version=%s", c.endpoint, deployment, version)
}

func toAzureMessages(in []types.Message) []azureChatMessage {
	msgs := make([]azureChatMessage, 0, len(in))
	for _, m := range in {
		switch m.Role {
		case types.RoleUser:
			msgs = append(msgs, azureChatMessage{Role: "user", Content: m.Content})
		case types.RoleAssistant:
			out := azureChatMessage{Role: "assistant", Content: m.Content}
			if len(m.ToolCalls) > 0 {
				out.ToolCalls = make([]azureToolCall, 0, len(m.ToolCalls))
				for _, tc := range m.ToolCalls {
					args := "{}"
					if len(tc.Arguments) > 0 {
						args = string(tc.Arguments)
					}
					out.ToolCalls = append(out.ToolCalls, azureToolCall{
						ID:   tc.ID,
						Type: "function",
						Function: azureFunctionCall{
							Name:      tc.Name,
							Arguments: args,
						},
					})
				}
			}
			msgs = append(msgs, out)
		case types.RoleTool:
			msgs = append(msgs, azureChatMessage{
				Role:       "tool",
				Name:       m.Name,
				ToolCallID: m.ToolCallID,
				Content:    m.Content,
			})
		}
	}
	return msgs
}

func toAzureTools(in []types.ToolDefinition) []azureTool {
	tools := make([]azureTool, 0, len(in))
	for _, t := range in {
		params := t.JSONSchema
		if len(params) == 0 {
			params = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		tools = append(tools, azureTool{
			Type: "function",
			Function: azureToolFunction{
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

type azureChatRequest struct {
	Model      string             `json:"model,omitempty"`
	Messages   []azureChatMessage `json:"messages"`
	Tools      []azureTool        `json:"tools,omitempty"`
	ToolChoice string             `json:"tool_choice,omitempty"`
	MaxTokens  int                `json:"max_tokens,omitempty"`
}

type azureChatMessage struct {
	Role       string          `json:"role"`
	Name       string          `json:"name,omitempty"`
	Content    any             `json:"content"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
	ToolCalls  []azureToolCall `json:"tool_calls,omitempty"`
}

type azureTool struct {
	Type     string            `json:"type"`
	Function azureToolFunction `json:"function"`
}

type azureToolFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type azureToolCall struct {
	ID       string            `json:"id,omitempty"`
	Type     string            `json:"type,omitempty"`
	Function azureFunctionCall `json:"function"`
}

type azureFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type azureChatResponse struct {
	Choices []struct {
		Message azureChatMessage `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}
