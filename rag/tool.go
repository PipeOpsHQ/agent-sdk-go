package rag

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/PipeOpsHQ/agent-sdk-go/types"
)

// SearchTool exposes RAG retrieval as a tool the agent can call explicitly.
type SearchTool struct {
	retriever Retriever
	topK      int
}

// NewSearchTool creates a rag_search tool backed by the given retriever.
func NewSearchTool(retriever Retriever, defaultTopK int) *SearchTool {
	if defaultTopK <= 0 {
		defaultTopK = 5
	}
	return &SearchTool{retriever: retriever, topK: defaultTopK}
}

func (t *SearchTool) Definition() types.ToolDefinition {
	return types.ToolDefinition{
		Name:        "rag_search",
		Description: "Search the knowledge base for relevant documents. Returns the most similar documents to the query.",
		JSONSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "The search query to find relevant documents",
				},
				"top_k": map[string]any{
					"type":        "integer",
					"description": "Number of results to return (default 5)",
				},
			},
			"required": []string{"query"},
		},
	}
}

func (t *SearchTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	var input struct {
		Query string `json:"query"`
		TopK  int    `json:"top_k"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return nil, fmt.Errorf("invalid rag_search args: %w", err)
	}
	if input.Query == "" {
		return nil, fmt.Errorf("query is required")
	}
	topK := t.topK
	if input.TopK > 0 {
		topK = input.TopK
	}

	results, err := t.retriever.Retrieve(ctx, input.Query, topK)
	if err != nil {
		return nil, fmt.Errorf("rag search failed: %w", err)
	}

	// Format results for the LLM
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d relevant documents:\n\n", len(results)))
	for i, r := range results {
		sb.WriteString(fmt.Sprintf("[%d] Score: %.3f\n", i+1, r.Score))
		sb.WriteString(r.Document.Content)
		sb.WriteString("\n\n")
	}
	return sb.String(), nil
}
