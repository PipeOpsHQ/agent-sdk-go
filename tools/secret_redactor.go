package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

type secretRedactorArgs struct {
	Text        string `json:"text"`
	Replacement string `json:"replacement,omitempty"`
}

// SecretPattern defines a pattern for detecting secrets.
type SecretPattern struct {
	Name    string
	Pattern *regexp.Regexp
}

// Common secret patterns to detect and redact.
var defaultSecretPatterns = []SecretPattern{
	// AWS Access Key ID
	{Name: "AWS Access Key", Pattern: regexp.MustCompile(`(?i)(AKIA|ABIA|ACCA|ASIA)[0-9A-Z]{16}`)},
	// AWS Secret Access Key
	{Name: "AWS Secret Key", Pattern: regexp.MustCompile(`(?i)aws[_\-]?secret[_\-]?access[_\-]?key[\s]*[=:]\s*["']?([A-Za-z0-9/+=]{40})["']?`)},
	// GitHub Token (classic and fine-grained)
	{Name: "GitHub Token", Pattern: regexp.MustCompile(`(ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9_]{36,255}`)},
	// GitHub App Token
	{Name: "GitHub App Token", Pattern: regexp.MustCompile(`(ghs|ghr)_[A-Za-z0-9_]{36,255}`)},
	// Generic API Key patterns
	{Name: "API Key", Pattern: regexp.MustCompile(`(?i)(api[_\-]?key|apikey)[\s]*[=:]\s*["']?([A-Za-z0-9\-_]{20,64})["']?`)},
	// Bearer tokens
	{Name: "Bearer Token", Pattern: regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9\-_]+\.[A-Za-z0-9\-_]+\.[A-Za-z0-9\-_]+`)},
	// JWT tokens
	{Name: "JWT", Pattern: regexp.MustCompile(`eyJ[A-Za-z0-9\-_]+\.eyJ[A-Za-z0-9\-_]+\.[A-Za-z0-9\-_]+`)},
	// Private keys
	{Name: "Private Key", Pattern: regexp.MustCompile(`-----BEGIN\s+(RSA|DSA|EC|OPENSSH|PGP)?\s*PRIVATE KEY-----`)},
	// Password in connection strings or env vars
	{Name: "Password", Pattern: regexp.MustCompile(`(?i)(password|passwd|pwd)[\s]*[=:]\s*["']?([^\s"']{8,})["']?`)},
	// Slack tokens
	{Name: "Slack Token", Pattern: regexp.MustCompile(`xox[baprs]-[0-9]{10,13}-[0-9]{10,13}[a-zA-Z0-9-]*`)},
	// Stripe API keys
	{Name: "Stripe Key", Pattern: regexp.MustCompile(`(sk|pk)_(test|live)_[0-9a-zA-Z]{24,}`)},
	// Google API key
	{Name: "Google API Key", Pattern: regexp.MustCompile(`AIza[0-9A-Za-z\-_]{35}`)},
	// Generic secret/token assignment
	{Name: "Generic Secret", Pattern: regexp.MustCompile(`(?i)(secret|token|auth[_\-]?token)[\s]*[=:]\s*["']?([A-Za-z0-9\-_]{16,})["']?`)},
	// Database connection strings with credentials
	{Name: "DB Connection String", Pattern: regexp.MustCompile(`(?i)(mongodb|postgres|mysql|redis):\/\/[^:]+:[^@]+@`)},
}

// RedactionResult contains details about redactions performed.
type RedactionResult struct {
	RedactedText    string   `json:"redactedText"`
	RedactionCount  int      `json:"redactionCount"`
	DetectedSecrets []string `json:"detectedSecrets,omitempty"`
}

func NewSecretRedactor() Tool {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"text": map[string]any{
				"type":        "string",
				"description": "The text to scan for secrets and redact.",
			},
			"replacement": map[string]any{
				"type":        "string",
				"description": "Optional replacement string for secrets. Defaults to '[REDACTED]'.",
			},
		},
		"required": []string{"text"},
	}

	return NewFuncTool(
		"secret_redactor",
		"Detect and redact secrets (API keys, tokens, passwords, credentials) from text. Returns the redacted text and count of secrets found.",
		schema,
		func(ctx context.Context, args json.RawMessage) (any, error) {
			var in secretRedactorArgs
			if err := json.Unmarshal(args, &in); err != nil {
				return nil, fmt.Errorf("invalid secret_redactor args: %w", err)
			}
			if in.Text == "" {
				return nil, fmt.Errorf("text is required")
			}

			replacement := "[REDACTED]"
			if in.Replacement != "" {
				replacement = in.Replacement
			}

			result := redactSecrets(in.Text, replacement)
			return result, nil
		},
	)
}

func redactSecrets(text, replacement string) RedactionResult {
	redactedText := text
	var detected []string
	count := 0

	for _, sp := range defaultSecretPatterns {
		matches := sp.Pattern.FindAllString(redactedText, -1)
		if len(matches) > 0 {
			for range matches {
				detected = append(detected, sp.Name)
				count++
			}
			redactedText = sp.Pattern.ReplaceAllString(redactedText, replacement)
		}
	}

	// Deduplicate detected secret types for cleaner output
	uniqueDetected := uniqueStrings(detected)

	return RedactionResult{
		RedactedText:    redactedText,
		RedactionCount:  count,
		DetectedSecrets: uniqueDetected,
	}
}

func uniqueStrings(input []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0)
	for _, s := range input {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}
	return result
}

// RedactSecretsFromMap recursively redacts secrets from string values in a map.
func RedactSecretsFromMap(data map[string]any, replacement string) map[string]any {
	result := make(map[string]any)
	for k, v := range data {
		result[k] = redactValue(v, replacement)
	}
	return result
}

func redactValue(v any, replacement string) any {
	switch val := v.(type) {
	case string:
		return redactSecrets(val, replacement).RedactedText
	case map[string]any:
		return RedactSecretsFromMap(val, replacement)
	case []any:
		result := make([]any, len(val))
		for i, item := range val {
			result[i] = redactValue(item, replacement)
		}
		return result
	default:
		return v
	}
}

// IsSensitiveKey checks if a key name suggests it contains sensitive data.
func IsSensitiveKey(key string) bool {
	sensitivePatterns := []string{
		"password", "passwd", "pwd", "secret", "token", "api_key", "apikey",
		"auth", "credential", "private", "access_key", "secret_key",
	}
	lowerKey := strings.ToLower(key)
	for _, pattern := range sensitivePatterns {
		if strings.Contains(lowerKey, pattern) {
			return true
		}
	}
	return false
}
