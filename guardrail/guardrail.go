// Package guardrail provides input/output validation for agent interactions.
//
// Guardrails run before (input) and after (output) LLM generation to enforce
// safety, compliance, and quality constraints. They can block requests,
// modify content, or flag issues for review.
package guardrail

import (
	"context"
	"fmt"
	"strings"
)

// Action defines what happens when a guardrail triggers.
type Action string

const (
	// ActionBlock rejects the request/response entirely.
	ActionBlock Action = "block"
	// ActionWarn flags the issue but allows the request to proceed.
	ActionWarn Action = "warn"
	// ActionRedact removes the offending content and continues.
	ActionRedact Action = "redact"
)

// Result is returned by a guardrail check.
type Result struct {
	// Triggered is true if the guardrail matched.
	Triggered bool `json:"triggered"`
	// Action to take when triggered.
	Action Action `json:"action,omitempty"`
	// Name of the guardrail that fired.
	Name string `json:"name"`
	// Message describes what was detected.
	Message string `json:"message,omitempty"`
	// RedactedText is the sanitized version (only for ActionRedact).
	RedactedText string `json:"redactedText,omitempty"`
}

// InputGuardrail validates user input before it reaches the LLM.
type InputGuardrail interface {
	Name() string
	CheckInput(ctx context.Context, input string) (Result, error)
}

// OutputGuardrail validates LLM output before it reaches the user.
type OutputGuardrail interface {
	Name() string
	CheckOutput(ctx context.Context, output string) (Result, error)
}

// Guardrail is a convenience interface for guards that check both directions.
type Guardrail interface {
	InputGuardrail
	OutputGuardrail
}

// Pipeline runs multiple guardrails in sequence.
type Pipeline struct {
	inputGuards  []InputGuardrail
	outputGuards []OutputGuardrail
}

// NewPipeline creates a guardrail pipeline.
func NewPipeline() *Pipeline {
	return &Pipeline{}
}

// AddInput registers an input guardrail.
func (p *Pipeline) AddInput(g InputGuardrail) *Pipeline {
	p.inputGuards = append(p.inputGuards, g)
	return p
}

// AddOutput registers an output guardrail.
func (p *Pipeline) AddOutput(g OutputGuardrail) *Pipeline {
	p.outputGuards = append(p.outputGuards, g)
	return p
}

// Add registers a bidirectional guardrail for both input and output.
func (p *Pipeline) Add(g Guardrail) *Pipeline {
	p.inputGuards = append(p.inputGuards, g)
	p.outputGuards = append(p.outputGuards, g)
	return p
}

// CheckInput runs all input guardrails. Returns the first blocking result,
// or accumulates warnings. If ActionRedact triggers, the returned text is
// the redacted version.
func (p *Pipeline) CheckInput(ctx context.Context, input string) (string, []Result, error) {
	text := input
	var warnings []Result
	for _, g := range p.inputGuards {
		res, err := g.CheckInput(ctx, text)
		if err != nil {
			return "", nil, fmt.Errorf("guardrail %q failed: %w", g.Name(), err)
		}
		if !res.Triggered {
			continue
		}
		switch res.Action {
		case ActionBlock:
			return "", []Result{res}, nil
		case ActionWarn:
			warnings = append(warnings, res)
		case ActionRedact:
			if res.RedactedText != "" {
				text = res.RedactedText
			}
			warnings = append(warnings, res)
		}
	}
	return text, warnings, nil
}

// CheckOutput runs all output guardrails with the same semantics as CheckInput.
func (p *Pipeline) CheckOutput(ctx context.Context, output string) (string, []Result, error) {
	text := output
	var warnings []Result
	for _, g := range p.outputGuards {
		res, err := g.CheckOutput(ctx, text)
		if err != nil {
			return "", nil, fmt.Errorf("guardrail %q failed: %w", g.Name(), err)
		}
		if !res.Triggered {
			continue
		}
		switch res.Action {
		case ActionBlock:
			return "", []Result{res}, nil
		case ActionWarn:
			warnings = append(warnings, res)
		case ActionRedact:
			if res.RedactedText != "" {
				text = res.RedactedText
			}
			warnings = append(warnings, res)
		}
	}
	return text, warnings, nil
}

// InputGuards returns the registered input guardrails.
func (p *Pipeline) InputGuards() []InputGuardrail { return p.inputGuards }

// OutputGuards returns the registered output guardrails.
func (p *Pipeline) OutputGuards() []OutputGuardrail { return p.outputGuards }

// BlockResult is a helper to create a blocking result.
func BlockResult(name, message string) Result {
	return Result{Triggered: true, Action: ActionBlock, Name: name, Message: message}
}

// WarnResult is a helper to create a warning result.
func WarnResult(name, message string) Result {
	return Result{Triggered: true, Action: ActionWarn, Name: name, Message: message}
}

// RedactResult is a helper to create a redaction result.
func RedactResult(name, message, redactedText string) Result {
	return Result{Triggered: true, Action: ActionRedact, Name: name, Message: message, RedactedText: redactedText}
}

// PassResult indicates the guardrail did not trigger.
func PassResult(name string) Result {
	return Result{Triggered: false, Name: name}
}

// HasBlock returns true if any result is a block action.
func HasBlock(results []Result) bool {
	for _, r := range results {
		if r.Triggered && r.Action == ActionBlock {
			return true
		}
	}
	return false
}

// Summary returns a human-readable summary of guardrail results.
func Summary(results []Result) string {
	if len(results) == 0 {
		return "all guardrails passed"
	}
	parts := make([]string, 0, len(results))
	for _, r := range results {
		if r.Triggered {
			parts = append(parts, fmt.Sprintf("[%s] %s: %s", r.Action, r.Name, r.Message))
		}
	}
	return strings.Join(parts, "; ")
}

// CatalogEntry describes a guardrail for discovery/UI purposes.
type CatalogEntry struct {
	Name         string `json:"name"`
	Description  string `json:"description"`
	Direction    string `json:"direction"` // "input", "output", or "both"
	Action       Action `json:"defaultAction"`
	Configurable bool   `json:"configurable"`
}

// BuiltinCatalog returns metadata for all built-in guardrails.
func BuiltinCatalog() []CatalogEntry {
	return []CatalogEntry{
		{Name: "max_length", Description: "Blocks input/output exceeding a character limit", Direction: "both", Action: ActionBlock, Configurable: true},
		{Name: "prompt_injection", Description: "Detects prompt injection attacks using pattern matching", Direction: "both", Action: ActionBlock, Configurable: false},
		{Name: "content_filter", Description: "Filters harmful, violent, or illegal content", Direction: "both", Action: ActionBlock, Configurable: false},
		{Name: "pii_filter", Description: "Detects and redacts PII (SSN, credit cards, emails, phone numbers, IPs)", Direction: "both", Action: ActionRedact, Configurable: false},
		{Name: "topic_filter", Description: "Restricts conversations to allowed topics via keyword matching", Direction: "both", Action: ActionBlock, Configurable: true},
		{Name: "secret_guard", Description: "Detects and redacts secrets (API keys, tokens, passwords)", Direction: "both", Action: ActionRedact, Configurable: false},
	}
}
