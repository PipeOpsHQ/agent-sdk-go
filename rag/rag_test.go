package rag

import (
	"context"
	"math"
	"testing"
)

// fakeEmbedder returns a deterministic embedding for testing.
type fakeEmbedder struct{}

func (f *fakeEmbedder) Embed(_ context.Context, text string) ([]float64, error) {
	// Simple hash-based embedding: each char contributes to a 4-dim vector
	vec := make([]float64, 4)
	for i, c := range text {
		vec[i%4] += float64(c)
	}
	// Normalize
	var norm float64
	for _, v := range vec {
		norm += v * v
	}
	norm = math.Sqrt(norm)
	if norm > 0 {
		for i := range vec {
			vec[i] /= norm
		}
	}
	return vec, nil
}

func (f *fakeEmbedder) EmbedBatch(_ context.Context, texts []string) ([][]float64, error) {
	vecs := make([][]float64, len(texts))
	for i, t := range texts {
		v, err := f.Embed(context.TODO(), t)
		if err != nil {
			return nil, err
		}
		vecs[i] = v
	}
	return vecs, nil
}

func TestMemoryStoreAddAndSearch(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	docs := []Document{
		{ID: "1", Content: "Go programming language", Embedding: []float64{1, 0, 0, 0}},
		{ID: "2", Content: "Python programming language", Embedding: []float64{0.9, 0.1, 0, 0}},
		{ID: "3", Content: "Cooking recipes", Embedding: []float64{0, 0, 1, 0}},
	}
	if err := store.Add(ctx, docs); err != nil {
		t.Fatal(err)
	}
	if store.Count() != 3 {
		t.Fatalf("expected 3 docs, got %d", store.Count())
	}

	// Search for something similar to Go
	results, err := store.Search(ctx, []float64{1, 0, 0, 0}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Document.ID != "1" {
		t.Errorf("expected top result to be doc 1, got %s", results[0].Document.ID)
	}
	if results[0].Score < 0.99 {
		t.Errorf("expected high similarity for exact match, got %f", results[0].Score)
	}
}

func TestMemoryStoreDelete(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	store.Add(ctx, []Document{
		{ID: "1", Content: "a", Embedding: []float64{1, 0}},
		{ID: "2", Content: "b", Embedding: []float64{0, 1}},
		{ID: "3", Content: "c", Embedding: []float64{1, 1}},
	})

	if err := store.Delete(ctx, []string{"2"}); err != nil {
		t.Fatal(err)
	}
	if store.Count() != 2 {
		t.Errorf("expected 2 docs after delete, got %d", store.Count())
	}

	results, _ := store.Search(ctx, []float64{0, 1}, 10)
	for _, r := range results {
		if r.Document.ID == "2" {
			t.Error("deleted doc should not appear in search results")
		}
	}
}

func TestSimpleRetriever(t *testing.T) {
	store := NewMemoryStore()
	embedder := &fakeEmbedder{}
	ctx := context.Background()

	// Pre-embed and store documents
	texts := []string{"Go concurrency patterns", "Python decorators", "Kubernetes deployments"}
	vecs, _ := embedder.EmbedBatch(ctx, texts)
	docs := make([]Document, len(texts))
	for i, text := range texts {
		docs[i] = Document{ID: string(rune('a' + i)), Content: text, Embedding: vecs[i]}
	}
	store.Add(ctx, docs)

	retriever := &SimpleRetriever{Embedder: embedder, Store: store}
	results, err := retriever.Retrieve(ctx, "Go concurrency patterns", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	// The exact query should be most similar to itself
	if results[0].Document.Content != "Go concurrency patterns" {
		t.Errorf("expected top result to be 'Go concurrency patterns', got %q", results[0].Document.Content)
	}
	if results[0].Score < 0.99 {
		t.Errorf("expected near-perfect similarity for identical query, got %f", results[0].Score)
	}
}

func TestCosineSimilarity(t *testing.T) {
	tests := []struct {
		name string
		a, b []float64
		want float64
	}{
		{"identical", []float64{1, 0, 0}, []float64{1, 0, 0}, 1.0},
		{"orthogonal", []float64{1, 0, 0}, []float64{0, 1, 0}, 0.0},
		{"opposite", []float64{1, 0}, []float64{-1, 0}, -1.0},
		{"empty", []float64{}, []float64{}, 0.0},
		{"mismatched", []float64{1, 0}, []float64{1, 0, 0}, 0.0},
		{"zero_vec", []float64{0, 0, 0}, []float64{1, 0, 0}, 0.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cosineSimilarity(tt.a, tt.b)
			if math.Abs(got-tt.want) > 1e-9 {
				t.Errorf("cosineSimilarity(%v, %v) = %f, want %f", tt.a, tt.b, got, tt.want)
			}
		})
	}
}
