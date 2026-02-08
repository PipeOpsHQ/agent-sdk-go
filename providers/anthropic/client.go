package anthropic

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

const (
	defaultModel      = "claude-3-5-sonnet-latest"
	anthropicVersion  = "2023-06-01"
	defaultMaxTokens  = 1024
	defaultAPIBaseURL = "https://api.anthropic.com"
)

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
	if strings.TrimSpace(apiKey) == "" {
		return nil, fmt.Errorf("ANTHROPIC_API_KEY is required")
	}
	c := &Client{
		apiKey:  strings.TrimSpace(apiKey),
		model:   defaultModel,
		baseURL: defaultAPIBaseURL,
		httpClient: &http.Client{
			Timeout: 90 * time.Second,
		},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

func (c *Client) Name() string { return "anthropic" }

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

	maxTokens := req.MaxOutputTokens
	if maxTokens <= 0 {
		maxTokens = defaultMaxTokens
	}

	payload := anthropicRequest{
		Model:     model,
		System:    req.SystemPrompt,
		MaxTokens: maxTokens,
		Messages:  toAnthropicMessages(req.Messages),
	}
	if len(req.Tools) > 0 {
		payload.Tools = toAnthropicTools(req.Tools)
		payload.ToolChoice = &anthropicToolChoice{Type: "auto"}
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return types.Response{}, fmt.Errorf("failed to marshal anthropic request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/messages", bytes.NewReader(raw))
	if err != nil {
		return types.Response{}, fmt.Errorf("failed to create anthropic request: %w", err)
	}
	httpReq.Header.Set("x-api-key", c.apiKey)
	httpReq.Header.Set("anthropic-version", anthropicVersion)
	httpReq.Header.Set("content-type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return types.Response{}, fmt.Errorf("anthropic request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return types.Response{}, fmt.Errorf("failed to read anthropic response: %w", err)
	}
	if resp.StatusCode >= 300 {
		return types.Response{}, fmt.Errorf("anthropic API error (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var apiResp anthropicResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return types.Response{}, fmt.Errorf("failed to decode anthropic response: %w", err)
	}

	out := types.Message{Role: types.RoleAssistant}
	for _, block := range apiResp.Content {
		switch block.Type {
		case "text":
			out.Content += block.Text
		case "tool_use":
			args := block.Input
			if args == nil {
				args = map[string]any{}
			}
			rawArgs, _ := json.Marshal(args)
			out.ToolCalls = append(out.ToolCalls, types.ToolCall{
				ID:        block.ID,
				Name:      block.Name,
				Arguments: rawArgs,
			})
		}
	}
	out.Content = strings.TrimSpace(out.Content)

	var usage *types.Usage
	if apiResp.Usage.InputTokens > 0 || apiResp.Usage.OutputTokens > 0 {
		usage = &types.Usage{
			InputTokens:  apiResp.Usage.InputTokens,
			OutputTokens: apiResp.Usage.OutputTokens,
			TotalTokens:  apiResp.Usage.InputTokens + apiResp.Usage.OutputTokens,
		}
	}

	return types.Response{
		Message: out,
		Usage:   usage,
	}, nil
}

func toAnthropicTools(in []types.ToolDefinition) []anthropicTool {
	tools := make([]anthropicTool, 0, len(in))
	for _, t := range in {
		schema := t.JSONSchema
		if len(schema) == 0 {
			schema = map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			}
		}
		tools = append(tools, anthropicTool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: schema,
		})
	}
	return tools
}

func toAnthropicMessages(in []types.Message) []anthropicMessage {
	msgs := make([]anthropicMessage, 0, len(in))
	for _, m := range in {
		switch m.Role {
		case types.RoleUser:
			msgs = append(msgs, anthropicMessage{
				Role: "user",
				Content: []anthropicContentBlock{
					{Type: "text", Text: m.Content},
				},
			})
		case types.RoleAssistant:
			blocks := make([]anthropicContentBlock, 0, len(m.ToolCalls)+1)
			if m.Content != "" {
				blocks = append(blocks, anthropicContentBlock{
					Type: "text",
					Text: m.Content,
				})
			}
			for _, tc := range m.ToolCalls {
				args := map[string]any{}
				if len(tc.Arguments) > 0 {
					_ = json.Unmarshal(tc.Arguments, &args)
				}
				blocks = append(blocks, anthropicContentBlock{
					Type:  "tool_use",
					ID:    tc.ID,
					Name:  tc.Name,
					Input: args,
				})
			}
			if len(blocks) > 0 {
				msgs = append(msgs, anthropicMessage{
					Role:    "assistant",
					Content: blocks,
				})
			}
		case types.RoleTool:
			msgs = append(msgs, anthropicMessage{
				Role: "user",
				Content: []anthropicContentBlock{
					{
						Type:      "tool_result",
						ToolUseID: m.ToolCallID,
						Content:   m.Content,
					},
				},
			})
		}
	}
	return msgs
}

type anthropicRequest struct {
	Model      string               `json:"model"`
	System     string               `json:"system,omitempty"`
	MaxTokens  int                  `json:"max_tokens"`
	Messages   []anthropicMessage   `json:"messages"`
	Tools      []anthropicTool      `json:"tools,omitempty"`
	ToolChoice *anthropicToolChoice `json:"tool_choice,omitempty"`
}

type anthropicToolChoice struct {
	Type string `json:"type"`
}

type anthropicMessage struct {
	Role    string                  `json:"role"`
	Content []anthropicContentBlock `json:"content"`
}

type anthropicContentBlock struct {
	Type      string         `json:"type"`
	Text      string         `json:"text,omitempty"`
	ID        string         `json:"id,omitempty"`
	Name      string         `json:"name,omitempty"`
	Input     map[string]any `json:"input,omitempty"`
	ToolUseID string         `json:"tool_use_id,omitempty"`
	Content   any            `json:"content,omitempty"`
}

type anthropicTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"input_schema"`
}

type anthropicResponse struct {
	Content []anthropicContentBlock `json:"content"`
	Usage   struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}
