package ollama

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/PipeOpsHQ/agent-sdk-go/types"
)

func TestClientGenerate_OpenAICompatibleRoundTrip(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("expected bearer auth header")
		}
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}
		if req["model"] != "llama3.2" {
			t.Fatalf("unexpected model: %#v", req["model"])
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"choices": [{
				"message": {
					"role": "assistant",
					"content": "result",
					"tool_calls": [{
						"id": "tc-1",
						"type": "function",
						"function": {"name": "calc", "arguments": "{\"x\":1}"}
					}]
				}
			}],
			"usage": {"prompt_tokens": 7, "completion_tokens": 3, "total_tokens": 10}
		}`))
	}))
	defer ts.Close()

	client, err := New(
		WithBaseURL(ts.URL),
		WithModel("llama3.2"),
		WithAPIKey("test-key"),
		WithHTTPClient(ts.Client()),
	)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	resp, err := client.Generate(context.Background(), types.Request{
		SystemPrompt: "system",
		Messages:     []types.Message{{Role: types.RoleUser, Content: "hello"}},
		Tools: []types.ToolDefinition{{
			Name:        "calc",
			Description: "calculator",
			JSONSchema:  map[string]any{"type": "object"},
		}},
	})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if resp.Message.Content != "result" {
		t.Fatalf("unexpected content: %q", resp.Message.Content)
	}
	if len(resp.Message.ToolCalls) != 1 || resp.Message.ToolCalls[0].Name != "calc" {
		t.Fatalf("unexpected tool calls: %#v", resp.Message.ToolCalls)
	}
	if resp.Usage == nil || resp.Usage.TotalTokens != 10 {
		t.Fatalf("unexpected usage: %#v", resp.Usage)
	}
}

func TestClientGenerate_ErrorNormalization(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("bad request"))
	}))
	defer ts.Close()

	client, err := New(WithBaseURL(ts.URL), WithHTTPClient(ts.Client()))
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	_, err = client.Generate(context.Background(), types.Request{Messages: []types.Message{{Role: types.RoleUser, Content: "hi"}}})
	if err == nil {
		t.Fatalf("expected error")
	}
}
