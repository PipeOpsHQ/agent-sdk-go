package gemini

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"google.golang.org/genai"

	"github.com/PipeOpsHQ/agent-sdk-go/llm"
	"github.com/PipeOpsHQ/agent-sdk-go/types"
)

const defaultModel = "gemini-2.5-flash"

type Client struct {
	client *genai.Client
	model  string
}

type Option func(*Client)

func WithModel(model string) Option {
	return func(c *Client) { c.model = model }
}

func New(ctx context.Context, apiKey string, opts ...Option) (*Client, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("GEMINI_API_KEY is required")
	}
	c := &Client{model: defaultModel}
	for _, opt := range opts {
		opt(c)
	}

	gc, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  apiKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create gemini client: %w", err)
	}
	c.client = gc
	return c, nil
}

func (c *Client) Name() string { return "gemini" }

func (c *Client) Capabilities() llm.Capabilities {
	return llm.Capabilities{
		Tools:            true,
		Streaming:        true,
		StructuredOutput: true,
	}
}

func (c *Client) Generate(ctx context.Context, req types.Request) (types.Response, error) {
	model := c.model
	if req.Model != "" {
		model = req.Model
	}

	config := &genai.GenerateContentConfig{}
	if req.SystemPrompt != "" {
		config.SystemInstruction = genai.NewContentFromText(req.SystemPrompt, genai.RoleUser)
	}
	if req.MaxOutputTokens > 0 {
		config.MaxOutputTokens = clampInt32(req.MaxOutputTokens)
	}
	if len(req.Tools) > 0 {
		config.Tools = []*genai.Tool{
			{FunctionDeclarations: toGeminiFunctionDeclarations(req.Tools)},
		}
		config.ToolConfig = &genai.ToolConfig{
			FunctionCallingConfig: &genai.FunctionCallingConfig{
				Mode: genai.FunctionCallingConfigModeAuto,
			},
		}
	}

	resp, err := c.client.Models.GenerateContent(ctx, model, toGeminiContents(req.Messages), config)
	if err != nil {
		return types.Response{}, fmt.Errorf("gemini generation failed: %w", err)
	}
	return parseGeminiResponse(resp), nil
}

func (c *Client) GenerateStream(ctx context.Context, req types.Request, onChunk func(types.StreamChunk) error) (types.Response, error) {
	if onChunk == nil {
		return types.Response{}, fmt.Errorf("onChunk is required")
	}
	model := c.model
	if req.Model != "" {
		model = req.Model
	}

	config := &genai.GenerateContentConfig{}
	if req.SystemPrompt != "" {
		config.SystemInstruction = genai.NewContentFromText(req.SystemPrompt, genai.RoleUser)
	}
	if req.MaxOutputTokens > 0 {
		config.MaxOutputTokens = clampInt32(req.MaxOutputTokens)
	}
	if len(req.Tools) > 0 {
		config.Tools = []*genai.Tool{
			{FunctionDeclarations: toGeminiFunctionDeclarations(req.Tools)},
		}
		config.ToolConfig = &genai.ToolConfig{
			FunctionCallingConfig: &genai.FunctionCallingConfig{
				Mode: genai.FunctionCallingConfigModeAuto,
			},
		}
	}

	var last *genai.GenerateContentResponse
	stream := c.client.Models.GenerateContentStream(ctx, model, toGeminiContents(req.Messages), config)
	for chunk, err := range stream {
		if err != nil {
			return types.Response{}, fmt.Errorf("gemini generation failed: %w", err)
		}
		if chunk == nil {
			continue
		}
		last = chunk

		if len(chunk.Candidates) == 0 || chunk.Candidates[0].Content == nil {
			continue
		}
		candidate := chunk.Candidates[0].Content
		for _, part := range candidate.Parts {
			if part == nil || part.Text == "" || part.Thought {
				continue
			}
			if err := onChunk(types.StreamChunk{Text: part.Text}); err != nil {
				return types.Response{}, err
			}
		}
	}

	if last == nil {
		return types.Response{}, fmt.Errorf("gemini generation failed: empty stream")
	}
	resp := parseGeminiResponse(last)
	if err := onChunk(types.StreamChunk{Done: true}); err != nil {
		return types.Response{}, err
	}
	return resp, nil
}

func parseGeminiResponse(resp *genai.GenerateContentResponse) types.Response {
	if resp == nil || len(resp.Candidates) == 0 || resp.Candidates[0].Content == nil {
		fallback := "I could not produce a response from Gemini for this step. Please continue with the task using available tools and provide the best next result."
		if resp != nil && resp.PromptFeedback != nil && strings.TrimSpace(resp.PromptFeedback.BlockReasonMessage) != "" {
			fallback = "Gemini returned no candidates: " + strings.TrimSpace(resp.PromptFeedback.BlockReasonMessage)
		}
		return types.Response{Message: types.Message{Role: types.RoleAssistant, Content: fallback}}
	}

	candidate := resp.Candidates[0].Content
	out := types.Message{Role: types.RoleAssistant}
	for _, part := range candidate.Parts {
		if part == nil {
			continue
		}
		if part.Text != "" {
			if part.Thought {
				out.Reasoning += part.Text
			} else {
				out.Content += part.Text
			}
		}
		if part.FunctionCall != nil {
			args := part.FunctionCall.Args
			if args == nil {
				args = map[string]any{}
			}
			rawArgs, _ := json.Marshal(args)
			out.ToolCalls = append(out.ToolCalls, types.ToolCall{
				ID:        part.FunctionCall.ID,
				Name:      part.FunctionCall.Name,
				Arguments: rawArgs,
			})
		}
	}
	out.Content = strings.TrimSpace(out.Content)
	out.Reasoning = strings.TrimSpace(out.Reasoning)

	var usage *types.Usage
	if resp.UsageMetadata != nil {
		usage = &types.Usage{
			InputTokens:  int(resp.UsageMetadata.PromptTokenCount),
			OutputTokens: int(resp.UsageMetadata.CandidatesTokenCount),
			TotalTokens:  int(resp.UsageMetadata.TotalTokenCount),
		}
	}

	return types.Response{Message: out, Usage: usage}
}

func clampInt32(v int) int32 {
	if v <= 0 {
		return 0
	}
	if v > math.MaxInt32 {
		return math.MaxInt32
	}
	return int32(v)
}

func toGeminiFunctionDeclarations(defs []types.ToolDefinition) []*genai.FunctionDeclaration {
	out := make([]*genai.FunctionDeclaration, 0, len(defs))
	for _, d := range defs {
		schema := d.JSONSchema
		if len(schema) == 0 {
			schema = map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			}
		}
		out = append(out, &genai.FunctionDeclaration{
			Name:                 d.Name,
			Description:          d.Description,
			ParametersJsonSchema: schema,
		})
	}
	return out
}

func toGeminiContents(messages []types.Message) []*genai.Content {
	contents := make([]*genai.Content, 0, len(messages))
	for _, m := range messages {
		switch m.Role {
		case types.RoleUser:
			contents = append(contents, genai.NewContentFromText(m.Content, genai.RoleUser))

		case types.RoleAssistant:
			parts := make([]*genai.Part, 0, len(m.ToolCalls)+1)
			if m.Content != "" {
				parts = append(parts, genai.NewPartFromText(m.Content))
			}
			for _, tc := range m.ToolCalls {
				args := map[string]any{}
				if len(tc.Arguments) > 0 {
					_ = json.Unmarshal(tc.Arguments, &args)
				}
				p := genai.NewPartFromFunctionCall(tc.Name, args)
				if tc.ID != "" {
					p.FunctionCall.ID = tc.ID
				}
				parts = append(parts, p)
			}
			if len(parts) > 0 {
				contents = append(contents, genai.NewContentFromParts(parts, genai.RoleModel))
			}

		case types.RoleTool:
			response := map[string]any{}
			if err := json.Unmarshal([]byte(m.Content), &response); err != nil {
				response = map[string]any{"output": m.Content}
			}
			p := genai.NewPartFromFunctionResponse(m.Name, response)
			if m.ToolCallID != "" {
				p.FunctionResponse.ID = m.ToolCallID
			}
			contents = append(contents, genai.NewContentFromParts([]*genai.Part{p}, genai.RoleUser))
		}
	}
	return contents
}
