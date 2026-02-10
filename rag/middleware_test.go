package rag

import (
	"context"
	"testing"

	agentfw "github.com/PipeOpsHQ/agent-sdk-go/agent"
	"github.com/PipeOpsHQ/agent-sdk-go/types"
)

func TestAgentMiddleware_InjectsContext(t *testing.T) {
	store := NewMemoryStore()
	embedder := &fakeEmbedder{}
	ctx := context.Background()

	// Add documents
	texts := []string{"Go is a compiled language", "Python is interpreted", "Rust has ownership"}
	vecs, _ := embedder.EmbedBatch(ctx, texts)
	docs := make([]Document, len(texts))
	for i, text := range texts {
		docs[i] = Document{ID: string(rune('a' + i)), Content: text, Embedding: vecs[i]}
	}
	store.Add(ctx, docs)

	retriever := &SimpleRetriever{Embedder: embedder, Store: store}
	mw := NewAgentMiddleware(retriever, WithTopK(2), WithPrefix("Knowledge base:"))

	event := &agentfw.GenerateMiddlewareEvent{
		Request: &types.Request{
			SystemPrompt: "You are helpful.",
			Messages: []types.Message{
				{Role: "user", Content: "Tell me about Go"},
			},
		},
	}

	err := mw.BeforeGenerate(ctx, event)
	if err != nil {
		t.Fatal(err)
	}

	// System prompt should now contain retrieved context
	if event.Request.SystemPrompt == "You are helpful." {
		t.Error("expected system prompt to be augmented with RAG context")
	}
	if len(event.Request.SystemPrompt) < 30 {
		t.Errorf("system prompt too short, expected context injection: %q", event.Request.SystemPrompt)
	}
}

func TestAgentMiddleware_NoUserMessage(t *testing.T) {
	retriever := &SimpleRetriever{Embedder: &fakeEmbedder{}, Store: NewMemoryStore()}
	mw := NewAgentMiddleware(retriever)

	event := &agentfw.GenerateMiddlewareEvent{
		Request: &types.Request{
			SystemPrompt: "original",
			Messages:     []types.Message{},
		},
	}

	err := mw.BeforeGenerate(context.Background(), event)
	if err != nil {
		t.Fatal(err)
	}
	if event.Request.SystemPrompt != "original" {
		t.Error("should not modify system prompt when there are no user messages")
	}
}

func TestSearchTool(t *testing.T) {
	store := NewMemoryStore()
	embedder := &fakeEmbedder{}
	ctx := context.Background()

	vecs, _ := embedder.EmbedBatch(ctx, []string{"hello world", "goodbye world"})
	store.Add(ctx, []Document{
		{ID: "1", Content: "hello world", Embedding: vecs[0]},
		{ID: "2", Content: "goodbye world", Embedding: vecs[1]},
	})

	retriever := &SimpleRetriever{Embedder: embedder, Store: store}
	tool := NewSearchTool(retriever, 2)

	def := tool.Definition()
	if def.Name != "rag_search" {
		t.Errorf("expected tool name 'rag_search', got %q", def.Name)
	}

	result, err := tool.Execute(ctx, []byte(`{"query": "hello world"}`))
	if err != nil {
		t.Fatal(err)
	}
	output, ok := result.(string)
	if !ok {
		t.Fatalf("expected string output, got %T", result)
	}
	if len(output) == 0 {
		t.Error("expected non-empty output")
	}
}
