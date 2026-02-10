package agent

import (
	"testing"

	"github.com/PipeOpsHQ/agent-sdk-go/types"
)

func TestEstimateTokens(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		expected int
	}{
		{"empty string", "", 0},
		{"short text", "hello", 2},
		{"longer text", "hello world this is a test", 7},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EstimateTokens(tt.text)
			if got != tt.expected {
				t.Errorf("EstimateTokens(%q) = %d, want %d", tt.text, got, tt.expected)
			}
		})
	}
}

func TestContextManager_TrimMessages(t *testing.T) {
	cm := NewContextManager(100) // Small limit for testing

	t.Run("no trimming needed", func(t *testing.T) {
		messages := []types.Message{
			{Role: types.RoleUser, Content: "hi"},
			{Role: types.RoleAssistant, Content: "hello"},
		}

		trimmed := cm.TrimMessages(messages, "", nil, 0)

		if len(trimmed) != 2 {
			t.Errorf("expected 2 messages, got %d", len(trimmed))
		}
	})

	t.Run("trimming needed preserves recent", func(t *testing.T) {
		// Create messages that exceed budget
		messages := []types.Message{
			{Role: types.RoleUser, Content: "first message with some content"},
			{Role: types.RoleAssistant, Content: "first response with some content"},
			{Role: types.RoleUser, Content: "second message with some content"},
			{Role: types.RoleAssistant, Content: "second response with some content"},
			{Role: types.RoleUser, Content: "final question"}, // This should always be kept
		}

		trimmed := cm.TrimMessages(messages, "", nil, 0)

		// Last message should always be preserved
		if len(trimmed) == 0 {
			t.Fatal("expected at least 1 message")
		}
		if trimmed[len(trimmed)-1].Content != "final question" {
			t.Error("last message was not preserved")
		}
	})

	t.Run("empty messages", func(t *testing.T) {
		trimmed := cm.TrimMessages(nil, "", nil, 0)
		if len(trimmed) != 0 {
			t.Errorf("expected 0 messages, got %d", len(trimmed))
		}
	})
}

func TestContextManager_ShouldTrim(t *testing.T) {
	cm := NewContextManager(50)

	t.Run("under budget", func(t *testing.T) {
		messages := []types.Message{
			{Role: types.RoleUser, Content: "hi"},
		}
		if cm.ShouldTrim(messages, "", nil) {
			t.Error("expected ShouldTrim to return false for small message")
		}
	})

	t.Run("over budget", func(t *testing.T) {
		// Each message needs to be long enough so total exceeds 50 tokens (200 chars)
		longContent := "this is a much longer message that should exceed our small token budget when combined with another message of similar length"
		messages := []types.Message{
			{Role: types.RoleUser, Content: longContent},
			{Role: types.RoleAssistant, Content: longContent},
			{Role: types.RoleUser, Content: longContent},
		}
		if !cm.ShouldTrim(messages, "", nil) {
			t.Error("expected ShouldTrim to return true for large messages")
		}
	})
}

func TestContextManager_EnsureValidStructure(t *testing.T) {
	cm := NewContextManager(1000)

	t.Run("removes orphaned tool results", func(t *testing.T) {
		messages := []types.Message{
			{Role: types.RoleTool, Content: "orphaned result", ToolCallID: "missing-call"},
			{Role: types.RoleUser, Content: "hello"},
		}

		result := cm.ensureValidStructure(messages)

		// Should only have the user message
		if len(result) != 1 {
			t.Errorf("expected 1 message, got %d", len(result))
		}
		if result[0].Role != types.RoleUser {
			t.Error("expected user message to be preserved")
		}
	})

	t.Run("preserves valid tool call pairs", func(t *testing.T) {
		messages := []types.Message{
			{Role: types.RoleAssistant, Content: "", ToolCalls: []types.ToolCall{{ID: "call-1", Name: "test"}}},
			{Role: types.RoleTool, Content: "result", ToolCallID: "call-1"},
			{Role: types.RoleUser, Content: "next question"},
		}

		result := cm.ensureValidStructure(messages)

		if len(result) != 3 {
			t.Errorf("expected 3 messages, got %d", len(result))
		}
	})
}

func TestNewContextManager(t *testing.T) {
	t.Run("uses default for zero", func(t *testing.T) {
		cm := NewContextManager(0)
		if cm.maxInputTokens != DefaultMaxInputTokens {
			t.Errorf("expected %d, got %d", DefaultMaxInputTokens, cm.maxInputTokens)
		}
	})

	t.Run("uses default for negative", func(t *testing.T) {
		cm := NewContextManager(-100)
		if cm.maxInputTokens != DefaultMaxInputTokens {
			t.Errorf("expected %d, got %d", DefaultMaxInputTokens, cm.maxInputTokens)
		}
	})

	t.Run("uses provided value", func(t *testing.T) {
		cm := NewContextManager(5000)
		if cm.maxInputTokens != 5000 {
			t.Errorf("expected 5000, got %d", cm.maxInputTokens)
		}
	})
}
