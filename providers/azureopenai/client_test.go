package azureopenai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nitrocode/ai-agents/framework/types"
)

func TestClientGenerate_MapsAzureRequest(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("api-version"); got != "2024-10-21" {
			t.Fatalf("unexpected api-version: %q", got)
		}
		if r.Header.Get("api-key") != "azure-key" {
			t.Fatalf("expected api-key header")
		}
		if r.URL.Path != "/openai/deployments/dep/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}

		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}
		if req["model"] != "gpt-4o-mini" {
			t.Fatalf("unexpected model: %#v", req["model"])
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"choices": [{
				"message": {
					"role": "assistant",
					"content": "azure-result",
					"tool_calls": [{
						"id": "tool-1",
						"type": "function",
						"function": {"name": "calc", "arguments": "{\"a\":1}"}
					}]
				}
			}],
			"usage": {"prompt_tokens": 8, "completion_tokens": 4, "total_tokens": 12}
		}`))
	}))
	defer ts.Close()

	client, err := New(
		"azure-key",
		WithEndpoint(ts.URL),
		WithDeployment("dep"),
		WithModel("gpt-4o-mini"),
		WithAPIVersion("2024-10-21"),
		WithHTTPClient(ts.Client()),
	)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	resp, err := client.Generate(context.Background(), types.Request{
		Messages: []types.Message{{Role: types.RoleUser, Content: "hello"}},
		Tools: []types.ToolDefinition{{
			Name:        "calc",
			Description: "calculator",
			JSONSchema:  map[string]any{"type": "object"},
		}},
	})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if resp.Message.Content != "azure-result" {
		t.Fatalf("unexpected content: %q", resp.Message.Content)
	}
	if len(resp.Message.ToolCalls) != 1 || resp.Message.ToolCalls[0].Name != "calc" {
		t.Fatalf("unexpected tool calls: %#v", resp.Message.ToolCalls)
	}
	if resp.Usage == nil || resp.Usage.TotalTokens != 12 {
		t.Fatalf("unexpected usage: %#v", resp.Usage)
	}
}

func TestNew_RequiresConfiguration(t *testing.T) {
	if _, err := New(""); err == nil {
		t.Fatalf("expected missing api key error")
	}

	if _, err := New("k", WithEndpoint("http://example")); err == nil {
		t.Fatalf("expected missing deployment error")
	}
}
