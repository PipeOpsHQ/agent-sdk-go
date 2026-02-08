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
	// Generic API Key patterns
	{Name: "API Key", Pattern: regexp.MustCompile(`(?i)(api[_\-]?key|apikey)[\s]*[=:]\s*["']?([A-Za-z0-9\-_]{20,64})["']?`)},
	// Bearer tokens
	{Name: "Bearer Token", Pattern: regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9\-_]+\.[A-Za-z0-9\-_]+\.[A-Za-z0-9\-_]+`)},
	// JWT tokens
	{Name: "JWT", Pattern: regexp.MustCompile(`eyJ[A-Za-z0-9\-_]+\.eyJ[A-Za-z0-9\-_]+\.[A-Za-z0-9\-_]+`)},
	// Private keys (PEM)
	{Name: "Private Key", Pattern: regexp.MustCompile(`-----BEGIN\s+(RSA|DSA|EC|OPENSSH|PGP|ENCRYPTED)?\s*PRIVATE KEY-----`)},
	// Password in key=value assignments (env vars, config files, CLI flags)
	{Name: "Password", Pattern: regexp.MustCompile(`(?i)(password|passwd|pwd|pass)[\s]*[=:]\s*["']?([^\s"',;}{)]{3,})["']?`)},
	// Password in URLs: scheme://user:password@host
	{Name: "Password in URL", Pattern: regexp.MustCompile(`(?i)://[^:/?#\s]+:([^@/?#\s]{3,})@`)},
	// Slack tokens
	{Name: "Slack Token", Pattern: regexp.MustCompile(`xox[baprs]-[0-9]{10,13}-[0-9]{10,13}[a-zA-Z0-9-]*`)},
	// Stripe API keys
	{Name: "Stripe Key", Pattern: regexp.MustCompile(`(sk|pk|rk)_(test|live)_[0-9a-zA-Z]{24,}`)},
	// Google API key
	{Name: "Google API Key", Pattern: regexp.MustCompile(`AIza[0-9A-Za-z\-_]{35}`)},
	// Google OAuth client secret
	{Name: "Google OAuth Secret", Pattern: regexp.MustCompile(`(?i)GOCSPX-[A-Za-z0-9\-_]{28}`)},
	// Heroku API key
	{Name: "Heroku API Key", Pattern: regexp.MustCompile(`(?i)heroku[_\-]?api[_\-]?key[\s]*[=:]\s*["']?[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}["']?`)},
	// SendGrid API key
	{Name: "SendGrid Key", Pattern: regexp.MustCompile(`SG\.[A-Za-z0-9\-_]{22}\.[A-Za-z0-9\-_]{43}`)},
	// Twilio API key
	{Name: "Twilio Key", Pattern: regexp.MustCompile(`SK[0-9a-fA-F]{32}`)},
	// Mailgun API key
	{Name: "Mailgun Key", Pattern: regexp.MustCompile(`key-[0-9a-zA-Z]{32}`)},
	// npm token
	{Name: "npm Token", Pattern: regexp.MustCompile(`npm_[A-Za-z0-9]{36}`)},
	// PyPI token
	{Name: "PyPI Token", Pattern: regexp.MustCompile(`pypi-[A-Za-z0-9\-_]{16,}`)},
	// Azure subscription / storage key
	{Name: "Azure Key", Pattern: regexp.MustCompile(`(?i)(azure[_\-]?(?:storage|subscription|tenant|client)[_\-]?(?:key|id|secret))[\s]*[=:]\s*["']?([A-Za-z0-9+/=\-_]{20,})["']?`)},
	// DigitalOcean token
	{Name: "DigitalOcean Token", Pattern: regexp.MustCompile(`dop_v1_[a-f0-9]{64}`)},
	// Datadog API key
	{Name: "Datadog Key", Pattern: regexp.MustCompile(`(?i)(dd[_\-]?api[_\-]?key|datadog[_\-]?api[_\-]?key)[\s]*[=:]\s*["']?[a-f0-9]{32}["']?`)},
	// Generic secret/token assignment
	{Name: "Generic Secret", Pattern: regexp.MustCompile(`(?i)(secret|token|auth[_\-]?token|access[_\-]?token|refresh[_\-]?token|client[_\-]?secret)[\s]*[=:]\s*["']?([A-Za-z0-9\-_]{16,})["']?`)},
	// Database connection strings with credentials
	{Name: "DB Connection String", Pattern: regexp.MustCompile(`(?i)(mongodb(\+srv)?|postgres(ql)?|mysql|mariadb|redis|amqp|amqps|mssql):\/\/[^:/?#\s]+:[^@/?#\s]+@`)},
	// Generic hex secrets (32+ hex chars assigned to a sensitive key)
	{Name: "Hex Secret", Pattern: regexp.MustCompile(`(?i)(secret|token|key|salt|hash|signature|signing)[\s]*[=:]\s*["']?([0-9a-f]{32,})["']?`)},
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
		"password", "passwd", "pwd", "pass",
		"secret", "token", "api_key", "apikey", "api-key",
		"auth", "credential", "private",
		"access_key", "secret_key", "signing_key",
		"connection_string", "conn_str",
		"bearer", "jwt", "session",
		"encrypt", "decrypt", "salt", "hash",
		"client_secret", "client_id",
		"ssh_key", "pem", "certificate",
	}
	lowerKey := strings.ToLower(key)
	for _, pattern := range sensitivePatterns {
		if strings.Contains(lowerKey, pattern) {
			return true
		}
	}
	return false
}
