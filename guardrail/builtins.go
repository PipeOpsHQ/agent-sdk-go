package guardrail

import (
	"context"
	"regexp"
	"strings"
	"unicode/utf8"
)

// MaxLength blocks input/output exceeding a character limit.
type MaxLength struct {
	Limit  int
	Action Action // defaults to ActionBlock
}

func (g *MaxLength) Name() string { return "max_length" }

func (g *MaxLength) check(text string) Result {
	if utf8.RuneCountInString(text) <= g.Limit {
		return PassResult(g.Name())
	}
	action := g.Action
	if action == "" {
		action = ActionBlock
	}
	return Result{
		Triggered: true,
		Action:    action,
		Name:      g.Name(),
		Message:   "input exceeds maximum length",
	}
}

func (g *MaxLength) CheckInput(_ context.Context, input string) (Result, error) {
	return g.check(input), nil
}

func (g *MaxLength) CheckOutput(_ context.Context, output string) (Result, error) {
	return g.check(output), nil
}

// PromptInjection detects common prompt injection attempts.
type PromptInjection struct{}

var injectionPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)ignore\s+(all\s+)?previous\s+instructions`),
	regexp.MustCompile(`(?i)ignore\s+(all\s+)?above\s+instructions`),
	regexp.MustCompile(`(?i)disregard\s+(all\s+)?previous`),
	regexp.MustCompile(`(?i)forget\s+(all\s+)?(your\s+)?instructions`),
	regexp.MustCompile(`(?i)you\s+are\s+now\s+(a|an)\s+`),
	regexp.MustCompile(`(?i)new\s+instructions?\s*:`),
	regexp.MustCompile(`(?i)system\s*:\s*you\s+are`),
	regexp.MustCompile(`(?i)override\s+(all\s+)?safety`),
	regexp.MustCompile(`(?i)bypass\s+(all\s+)?restrictions`),
	regexp.MustCompile(`(?i)act\s+as\s+(if\s+)?you\s+have\s+no\s+(restrictions|rules|limits)`),
	regexp.MustCompile(`(?i)pretend\s+(that\s+)?(you\s+)?are\s+(not|no\s+longer)\s+(an?\s+)?AI`),
	regexp.MustCompile(`(?i)do\s+not\s+follow\s+(any\s+)?(safety|ethical|content)\s+(guidelines|rules|policies)`),
	regexp.MustCompile(`(?i)\bDAN\b.*\bmode\b`),
	regexp.MustCompile(`(?i)jailbreak`),
}

func (*PromptInjection) Name() string { return "prompt_injection" }

func (*PromptInjection) CheckInput(_ context.Context, input string) (Result, error) {
	for _, pat := range injectionPatterns {
		if pat.MatchString(input) {
			return BlockResult("prompt_injection", "potential prompt injection detected: "+pat.String()), nil
		}
	}
	return PassResult("prompt_injection"), nil
}

// ContentFilter blocks or warns on offensive, harmful, or inappropriate content.
type ContentFilter struct {
	// CustomPatterns adds extra patterns beyond the defaults.
	CustomPatterns []string
	Action         Action // defaults to ActionBlock
}

var defaultContentPatterns = []string{
	"kill yourself", "kys",
	"how to make a bomb", "how to make explosives",
	"how to hack", "how to phish",
	"child exploitation", "csam",
}

func (g *ContentFilter) Name() string { return "content_filter" }

func (g *ContentFilter) patterns() []string {
	return append(defaultContentPatterns, g.CustomPatterns...)
}

func (g *ContentFilter) check(text string) Result {
	lower := strings.ToLower(text)
	for _, pat := range g.patterns() {
		if strings.Contains(lower, strings.ToLower(pat)) {
			action := g.Action
			if action == "" {
				action = ActionBlock
			}
			return Result{
				Triggered: true,
				Action:    action,
				Name:      g.Name(),
				Message:   "prohibited content detected",
			}
		}
	}
	return PassResult(g.Name())
}

func (g *ContentFilter) CheckInput(_ context.Context, input string) (Result, error) {
	return g.check(input), nil
}

func (g *ContentFilter) CheckOutput(_ context.Context, output string) (Result, error) {
	return g.check(output), nil
}

// PIIFilter detects and optionally redacts personally identifiable information.
type PIIFilter struct {
	Action Action // defaults to ActionRedact
}

var piiPatterns = []struct {
	name    string
	pattern *regexp.Regexp
	replace string
}{
	{"SSN", regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`), "[SSN_REDACTED]"},
	{"credit card", regexp.MustCompile(`\b(?:\d{4}[\s\-]?){3}\d{4}\b`), "[CC_REDACTED]"},
	{"email", regexp.MustCompile(`\b[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}\b`), "[EMAIL_REDACTED]"},
	{"phone", regexp.MustCompile(`\b(?:\+?1[\s\-]?)?\(?\d{3}\)?[\s\-]?\d{3}[\s\-]?\d{4}\b`), "[PHONE_REDACTED]"},
	{"IP address", regexp.MustCompile(`\b\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}\b`), "[IP_REDACTED]"},
}

func (g *PIIFilter) Name() string { return "pii_filter" }

func (g *PIIFilter) check(text string) Result {
	redacted := text
	detected := false
	for _, p := range piiPatterns {
		if p.pattern.MatchString(redacted) {
			detected = true
			redacted = p.pattern.ReplaceAllString(redacted, p.replace)
		}
	}
	if !detected {
		return PassResult(g.Name())
	}
	action := g.Action
	if action == "" {
		action = ActionRedact
	}
	return Result{
		Triggered:    true,
		Action:       action,
		Name:         g.Name(),
		Message:      "PII detected and redacted",
		RedactedText: redacted,
	}
}

func (g *PIIFilter) CheckInput(_ context.Context, input string) (Result, error) {
	return g.check(input), nil
}

func (g *PIIFilter) CheckOutput(_ context.Context, output string) (Result, error) {
	return g.check(output), nil
}

// TopicFilter restricts conversations to allowed topics.
type TopicFilter struct {
	// BlockedTopics are substring patterns that should be blocked.
	BlockedTopics []string
	Action        Action // defaults to ActionBlock
}

func (g *TopicFilter) Name() string { return "topic_filter" }

func (g *TopicFilter) CheckInput(_ context.Context, input string) (Result, error) {
	lower := strings.ToLower(input)
	for _, topic := range g.BlockedTopics {
		if strings.Contains(lower, strings.ToLower(topic)) {
			action := g.Action
			if action == "" {
				action = ActionBlock
			}
			return Result{
				Triggered: true,
				Action:    action,
				Name:      g.Name(),
				Message:   "blocked topic detected: " + topic,
			}, nil
		}
	}
	return PassResult(g.Name()), nil
}

func (g *TopicFilter) CheckOutput(_ context.Context, output string) (Result, error) {
	return g.CheckInput(context.TODO(), output)
}

// SecretGuard detects secrets in input/output using patterns from the tools package.
type SecretGuard struct {
	// Patterns to detect. If empty, uses a default set.
	Patterns []SecretPattern
	Action   Action // defaults to ActionRedact
}

// SecretPattern defines a regex pattern for secret detection.
type SecretPattern struct {
	Name    string
	Pattern *regexp.Regexp
}

var defaultSecretGuardPatterns = []SecretPattern{
	{"AWS Key", regexp.MustCompile(`(?i)(AKIA|ABIA|ACCA|ASIA)[0-9A-Z]{16}`)},
	{"GitHub Token", regexp.MustCompile(`(ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9_]{36,255}`)},
	{"Private Key", regexp.MustCompile(`-----BEGIN\s+(RSA|DSA|EC|OPENSSH|PGP|ENCRYPTED)?\s*PRIVATE KEY-----`)},
	{"JWT", regexp.MustCompile(`eyJ[A-Za-z0-9\-_]+\.eyJ[A-Za-z0-9\-_]+\.[A-Za-z0-9\-_]+`)},
	{"Password", regexp.MustCompile(`(?i)(password|passwd|pwd|pass)[\s]*[=:]\s*["']?([^\s"',;}{)]{3,})["']?`)},
	{"Connection String", regexp.MustCompile(`(?i)(mongodb(\+srv)?|postgres(ql)?|mysql|redis|amqp):\/\/[^:/?#\s]+:[^@/?#\s]+@`)},
	{"Bearer Token", regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9\-_]+\.[A-Za-z0-9\-_]+\.[A-Za-z0-9\-_]+`)},
	{"Generic Secret", regexp.MustCompile(`(?i)(secret|token|auth[_\-]?token|api[_\-]?key)[\s]*[=:]\s*["']?([A-Za-z0-9\-_]{16,})["']?`)},
}

func (g *SecretGuard) Name() string { return "secret_guard" }

func (g *SecretGuard) patterns() []SecretPattern {
	if len(g.Patterns) > 0 {
		return g.Patterns
	}
	return defaultSecretGuardPatterns
}

func (g *SecretGuard) check(text string) Result {
	redacted := text
	detected := false
	for _, sp := range g.patterns() {
		if sp.Pattern.MatchString(redacted) {
			detected = true
			redacted = sp.Pattern.ReplaceAllString(redacted, "[SECRET_REDACTED]")
		}
	}
	if !detected {
		return PassResult(g.Name())
	}
	action := g.Action
	if action == "" {
		action = ActionRedact
	}
	return Result{
		Triggered:    true,
		Action:       action,
		Name:         g.Name(),
		Message:      "secrets detected and redacted",
		RedactedText: redacted,
	}
}

func (g *SecretGuard) CheckInput(_ context.Context, input string) (Result, error) {
	return g.check(input), nil
}

func (g *SecretGuard) CheckOutput(_ context.Context, output string) (Result, error) {
	return g.check(output), nil
}
