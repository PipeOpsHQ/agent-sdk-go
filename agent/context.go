package agent

import (
	"strings"

	"github.com/PipeOpsHQ/agent-sdk-go/framework/types"
)

const (
	// DefaultMaxInputTokens is a safe default that stays well under Anthropic's
	// 30,000 input tokens/minute rate limit, leaving room for tool definitions
	// and system prompts.
	DefaultMaxInputTokens = 25000

	// charsPerToken is an approximate ratio for token estimation.
	// Most LLMs average ~4 characters per token for English text.
	charsPerToken = 4
)

// ContextManager handles token-aware context trimming to prevent
// exceeding provider rate limits.
type ContextManager struct {
	maxInputTokens int
}

// NewContextManager creates a ContextManager with the specified token limit.
// If maxTokens is <= 0, DefaultMaxInputTokens is used.
func NewContextManager(maxTokens int) *ContextManager {
	if maxTokens <= 0 {
		maxTokens = DefaultMaxInputTokens
	}
	return &ContextManager{maxInputTokens: maxTokens}
}

// EstimateTokens provides a rough token count for a string.
// This uses a simple character-based heuristic (~4 chars per token).
func EstimateTokens(text string) int {
	if len(text) == 0 {
		return 0
	}
	return (len(text) + charsPerToken - 1) / charsPerToken
}

// EstimateMessageTokens estimates tokens for a single message,
// including role overhead.
func EstimateMessageTokens(msg types.Message) int {
	// Base overhead for role and message structure
	tokens := 4

	// Content tokens
	tokens += EstimateTokens(msg.Content)

	// Tool call overhead
	for _, tc := range msg.ToolCalls {
		tokens += 10 // ID, name overhead
		tokens += EstimateTokens(string(tc.Arguments))
	}

	// Tool result overhead
	if msg.ToolCallID != "" {
		tokens += 5
	}

	return tokens
}

// EstimateMessagesTokens estimates total tokens for a slice of messages.
func EstimateMessagesTokens(messages []types.Message) int {
	total := 0
	for _, msg := range messages {
		total += EstimateMessageTokens(msg)
	}
	return total
}

// EstimateToolDefinitionsTokens estimates tokens for tool definitions.
func EstimateToolDefinitionsTokens(tools []types.ToolDefinition) int {
	total := 0
	for _, tool := range tools {
		total += 10 // name overhead
		total += EstimateTokens(tool.Description)
		// Rough estimate for JSON schema
		total += 50
	}
	return total
}

// TrimMessages trims conversation history to fit within the token budget.
// It preserves:
// 1. The system prompt (counted separately)
// 2. The most recent user message (last in slice)
// 3. As many recent messages as possible within budget
//
// Parameters:
// - messages: the conversation history
// - systemPrompt: the system prompt (tokens counted against budget)
// - tools: tool definitions (tokens counted against budget)
// - reserveTokens: additional tokens to reserve (e.g., for expected output)
func (cm *ContextManager) TrimMessages(
	messages []types.Message,
	systemPrompt string,
	tools []types.ToolDefinition,
	reserveTokens int,
) []types.Message {
	if len(messages) == 0 {
		return messages
	}

	// Calculate fixed overhead
	fixedTokens := EstimateTokens(systemPrompt) + EstimateToolDefinitionsTokens(tools) + reserveTokens
	availableTokens := cm.maxInputTokens - fixedTokens

	if availableTokens <= 0 {
		// Not enough budget even for overhead, return just the last message
		if len(messages) > 0 {
			return cm.ensureValidStructure(messages[len(messages)-1:])
		}
		return cm.ensureValidStructure(messages)
	}

	// Calculate total tokens needed
	totalTokens := EstimateMessagesTokens(messages)

	// If we're under budget, return all messages
	if totalTokens <= availableTokens {
		return cm.ensureValidStructure(messages)
	}

	// We need to trim - keep messages from the end (most recent)
	var trimmed []types.Message
	usedTokens := 0

	// Always include the last message (current user input)
	lastMsg := messages[len(messages)-1]
	lastMsgTokens := EstimateMessageTokens(lastMsg)
	usedTokens += lastMsgTokens

	// Work backwards from second-to-last message
	for i := len(messages) - 2; i >= 0; i-- {
		msg := messages[i]
		msgTokens := EstimateMessageTokens(msg)

		if usedTokens+msgTokens > availableTokens {
			break
		}

		usedTokens += msgTokens
		trimmed = append([]types.Message{msg}, trimmed...)
	}

	// Add the last message at the end
	trimmed = append(trimmed, lastMsg)

	// Ensure we maintain valid conversation structure
	// (tool results must follow their tool calls, etc.)
	trimmed = cm.ensureValidStructure(trimmed)

	return trimmed
}

// ensureValidStructure ensures the trimmed messages maintain valid
// conversation structure (e.g., tool results have their calls).
func (cm *ContextManager) ensureValidStructure(messages []types.Message) []types.Message {
	if len(messages) == 0 {
		return messages
	}

	var result []types.Message
	pendingToolCalls := make(map[string]bool)
	toolBlockStart := -1

	dropOpenToolBlock := func() {
		if len(pendingToolCalls) == 0 {
			return
		}
		if toolBlockStart >= 0 && toolBlockStart <= len(result) {
			result = result[:toolBlockStart]
		}
		pendingToolCalls = make(map[string]bool)
		toolBlockStart = -1
	}

	for _, msg := range messages {
		// Track tool calls from assistant messages
		if msg.Role == types.RoleAssistant {
			if len(msg.ToolCalls) > 0 {
				// New function-call turn starts a strict call/response block.
				dropOpenToolBlock()
				toolBlockStart = len(result)
				result = append(result, msg)
				for _, tc := range msg.ToolCalls {
					pendingToolCalls[tc.ID] = true
				}
				continue
			}

			// Plain assistant content cannot appear while a function-call block is unresolved.
			dropOpenToolBlock()
			result = append(result, msg)
			continue
		}

		// For tool results, only include if we have the corresponding call
		if msg.Role == types.RoleTool && msg.ToolCallID != "" {
			if pendingToolCalls[msg.ToolCallID] {
				result = append(result, msg)
				delete(pendingToolCalls, msg.ToolCallID)
				if len(pendingToolCalls) == 0 {
					toolBlockStart = -1
				}
			}
			// Skip orphaned tool results
			continue
		}

		// Any non-tool turn closes unresolved function-call blocks.
		dropOpenToolBlock()

		// Include user/system/other messages
		result = append(result, msg)
	}

	// Drop dangling call turns at the tail.
	dropOpenToolBlock()

	return result
}

// ShouldTrim returns true if the messages would exceed the token budget.
func (cm *ContextManager) ShouldTrim(
	messages []types.Message,
	systemPrompt string,
	tools []types.ToolDefinition,
) bool {
	fixedTokens := EstimateTokens(systemPrompt) + EstimateToolDefinitionsTokens(tools)
	availableTokens := cm.maxInputTokens - fixedTokens
	totalTokens := EstimateMessagesTokens(messages)
	return totalTokens > availableTokens
}

// SummarizeMessages creates a summary of old messages to preserve context
// while reducing token count. This returns a single user message with the summary.
func (cm *ContextManager) SummarizeMessages(messages []types.Message) types.Message {
	if len(messages) == 0 {
		return types.Message{Role: types.RoleUser, Content: ""}
	}

	var summaryParts []string
	summaryParts = append(summaryParts, "[Previous conversation summary]")

	for _, msg := range messages {
		switch msg.Role {
		case types.RoleUser:
			if len(msg.Content) > 200 {
				summaryParts = append(summaryParts, "User: "+msg.Content[:200]+"...")
			} else if msg.Content != "" {
				summaryParts = append(summaryParts, "User: "+msg.Content)
			}
		case types.RoleAssistant:
			if len(msg.ToolCalls) > 0 {
				var toolNames []string
				for _, tc := range msg.ToolCalls {
					toolNames = append(toolNames, tc.Name)
				}
				summaryParts = append(summaryParts, "Assistant used tools: "+strings.Join(toolNames, ", "))
			} else if msg.Content != "" {
				if len(msg.Content) > 200 {
					summaryParts = append(summaryParts, "Assistant: "+msg.Content[:200]+"...")
				} else {
					summaryParts = append(summaryParts, "Assistant: "+msg.Content)
				}
			}
		}
	}

	summaryParts = append(summaryParts, "[End of summary]")

	return types.Message{
		Role:    types.RoleUser,
		Content: strings.Join(summaryParts, "\n"),
	}
}
