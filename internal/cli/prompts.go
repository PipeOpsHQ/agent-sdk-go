package cli

import (
	"fmt"
	"strings"

	"github.com/PipeOpsHQ/agent-sdk-go/framework/prompt"
)

// PromptTemplate defines a named prompt template with variable substitution support.
type PromptTemplate struct {
	Name        string
	Description string
	Content     string
}

// PromptContext contains variables available for prompt substitution.
type PromptContext struct {
	ToolCount     int
	ToolNames     []string
	Workflow      string
	Provider      string
	ExecutionMode string
}

// SystemPrompts contains all predefined system prompt templates.
var SystemPrompts = map[string]PromptTemplate{
	"default": {
		Name:        "default",
		Description: "Generic practical AI assistant",
		Content:     "You are a practical AI assistant. Be concise, accurate, and actionable.",
	},
	"analyst": {
		Name:        "analyst",
		Description: "Data-driven analyst focused on investigation and reporting",
		Content: `You are an expert analyst. Your role is to:
- Investigate and understand problems systematically
- Synthesize data into clear, actionable insights
- Support findings with evidence and reasoning
- Provide structured reports with findings, analysis, and recommendations
- Ask clarifying questions when information is ambiguous`,
	},
	"engineer": {
		Name:        "engineer",
		Description: "Technical engineer focused on implementation and solutions",
		Content: `You are a senior engineer. Your role is to:
- Design and implement technical solutions
- Prioritize code quality, maintainability, and performance
- Consider edge cases and error handling
- Provide clear technical explanations
- Suggest improvements and best practices
- Use available tools to diagnose and resolve issues`,
	},
	"specialist": {
		Name:        "specialist",
		Description: "Domain specialist with deep expertise",
		Content: `You are a subject matter expert. Your role is to:
- Apply deep domain knowledge to solve complex problems
- Provide authoritative guidance based on best practices
- Explain concepts clearly for different audiences
- Identify risks and recommend mitigations
- Stay focused on the domain's specific requirements`,
	},
	"assistant": {
		Name:        "assistant",
		Description: "Helpful assistant focused on user support",
		Content: `You are a helpful AI assistant. Your role is to:
- Understand user needs clearly before responding
- Provide accurate, complete information
- Break complex tasks into manageable steps
- Use available tools to accomplish goals efficiently
- Follow up to ensure the user is satisfied`,
	},
	"reasoning": {
		Name:        "reasoning",
		Description: "Careful reasoner focused on thorough analysis",
		Content: `You are a careful reasoner. Your role is to:
- Think through problems step-by-step
- Consider multiple perspectives and approaches
- Identify assumptions and validate them
- Break complex problems into components
- Explain your reasoning clearly
- Revise conclusions if new evidence appears`,
	},
}

// GetPromptTemplate retrieves a prompt template by name.
func GetPromptTemplate(name string) (PromptTemplate, bool) {
	if spec, ok := prompt.Resolve(name); ok {
		return PromptTemplate{
			Name:        spec.Name,
			Description: spec.Description,
			Content:     spec.System,
		}, true
	}
	tmpl, ok := SystemPrompts[strings.ToLower(strings.TrimSpace(name))]
	return tmpl, ok
}

// AvailablePromptNames returns a sorted list of available prompt template names.
func AvailablePromptNames() []string {
	nameSet := map[string]struct{}{}
	for _, n := range prompt.Names() {
		nameSet[n] = struct{}{}
	}
	names := make([]string, 0, len(SystemPrompts)+len(nameSet))
	for name := range SystemPrompts {
		nameSet[name] = struct{}{}
	}
	for name := range nameSet {
		names = append(names, name)
	}
	// Sort for consistent output
	for i := 0; i < len(names); i++ {
		for j := i + 1; j < len(names); j++ {
			if names[i] > names[j] {
				names[i], names[j] = names[j], names[i]
			}
		}
	}
	return names
}

// InterpolatePrompt performs variable substitution in a prompt template.
// Supports variables: {tool_count}, {tool_names}, {workflow}, {provider}, {execution_mode}
func InterpolatePrompt(prompt string, ctx PromptContext) string {
	result := prompt

	// Substitute variables
	result = strings.ReplaceAll(result, "{tool_count}", fmt.Sprintf("%d", ctx.ToolCount))
	result = strings.ReplaceAll(result, "{tool_names}", strings.Join(ctx.ToolNames, ", "))
	result = strings.ReplaceAll(result, "{workflow}", ctx.Workflow)
	result = strings.ReplaceAll(result, "{provider}", ctx.Provider)
	result = strings.ReplaceAll(result, "{execution_mode}", ctx.ExecutionMode)

	return result
}

// BuildPrompt constructs a system prompt from a template name or custom prompt.
// Priority: custom -> template -> default
func BuildPrompt(customPrompt, templateName string, ctx PromptContext) string {
	// If custom prompt provided, use it
	if strings.TrimSpace(customPrompt) != "" {
		rendered, err := prompt.Render(strings.TrimSpace(customPrompt), promptVars(ctx))
		if err == nil {
			return InterpolatePrompt(rendered, ctx)
		}
		return InterpolatePrompt(strings.TrimSpace(customPrompt), ctx)
	}

	// Try to use named template
	if strings.TrimSpace(templateName) != "" {
		if tmpl, ok := GetPromptTemplate(templateName); ok {
			rendered, err := prompt.Render(tmpl.Content, promptVars(ctx))
			if err == nil {
				return InterpolatePrompt(rendered, ctx)
			}
			return InterpolatePrompt(tmpl.Content, ctx)
		}
	}

	// Fall back to default
	defaultTmpl := SystemPrompts["default"]
	return InterpolatePrompt(defaultTmpl.Content, ctx)
}

func promptVars(ctx PromptContext) map[string]string {
	return map[string]string{
		"tool_count":     fmt.Sprintf("%d", ctx.ToolCount),
		"tool_names":     strings.Join(ctx.ToolNames, ", "),
		"workflow":       ctx.Workflow,
		"provider":       ctx.Provider,
		"execution_mode": ctx.ExecutionMode,
	}
}

// ValidatePrompt checks if a prompt is reasonable and returns warnings.
func ValidatePrompt(prompt string) []string {
	var warnings []string

	prompt = strings.TrimSpace(prompt)
	if len(prompt) < 10 {
		warnings = append(warnings, "prompt is very short and may not provide enough guidance")
	}
	if len(prompt) > 2000 {
		warnings = append(warnings, "prompt is very long and may hurt performance")
	}
	if !strings.Contains(strings.ToLower(prompt), "you") && !strings.Contains(strings.ToLower(prompt), "tool") {
		warnings = append(warnings, "prompt does not address the agent role or tool usage")
	}

	return warnings
}
