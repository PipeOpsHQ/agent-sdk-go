package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
)

type envVarsArgs struct {
	Operation string   `json:"operation"`
	Name      string   `json:"name,omitempty"`
	Value     string   `json:"value,omitempty"`
	Pattern   string   `json:"pattern,omitempty"`
	Names     []string `json:"names,omitempty"`
}

// EnvResult contains the result of an environment operation.
type EnvResult struct {
	Success bool           `json:"success"`
	Data    map[string]any `json:"data,omitempty"`
	Error   string         `json:"error,omitempty"`
}

// Sensitive environment variable patterns to redact
var sensitiveEnvPatterns = []string{
	"PASSWORD", "SECRET", "TOKEN", "KEY", "CREDENTIAL", "AUTH",
	"PRIVATE", "API_KEY", "APIKEY", "ACCESS_KEY", "SESSION",
}

func NewEnvVars() Tool {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"operation": map[string]any{
				"type":        "string",
				"enum":        []string{"get", "list", "search", "set", "check"},
				"description": "Operation: get, list, search, set, check (if exists).",
			},
			"name": map[string]any{
				"type":        "string",
				"description": "Variable name for get/set/check.",
			},
			"value": map[string]any{
				"type":        "string",
				"description": "Value for set operation.",
			},
			"pattern": map[string]any{
				"type":        "string",
				"description": "Regex pattern for search.",
			},
			"names": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "List of names for batch get/check.",
			},
		},
		"required": []string{"operation"},
	}

	return NewFuncTool(
		"env_vars",
		"Access environment variables. Sensitive values are automatically redacted.",
		schema,
		func(ctx context.Context, args json.RawMessage) (any, error) {
			var in envVarsArgs
			if err := json.Unmarshal(args, &in); err != nil {
				return nil, fmt.Errorf("invalid env_vars args: %w", err)
			}

			switch in.Operation {
			case "get":
				return envGet(in.Name, in.Names)
			case "list":
				return envList()
			case "search":
				return envSearch(in.Pattern)
			case "set":
				return envSet(in.Name, in.Value)
			case "check":
				return envCheck(in.Name, in.Names)
			default:
				return nil, fmt.Errorf("unsupported operation %q", in.Operation)
			}
		},
	)
}

func envGet(name string, names []string) (*EnvResult, error) {
	if name != "" {
		names = append([]string{name}, names...)
	}

	if len(names) == 0 {
		return &EnvResult{Success: false, Error: "name or names required"}, nil
	}

	vars := make(map[string]string)
	for _, n := range names {
		value := os.Getenv(n)
		if isSensitiveEnv(n) && value != "" {
			value = "[REDACTED]"
		}
		vars[n] = value
	}

	return &EnvResult{Success: true, Data: map[string]any{"variables": vars}}, nil
}

func envList() (*EnvResult, error) {
	env := os.Environ()
	vars := make(map[string]string)
	names := make([]string, 0, len(env))

	for _, e := range env {
		parts := strings.SplitN(e, "=", 2)
		if len(parts) != 2 {
			continue
		}
		name, value := parts[0], parts[1]
		names = append(names, name)

		if isSensitiveEnv(name) {
			value = "[REDACTED]"
		}
		vars[name] = value
	}

	sort.Strings(names)

	return &EnvResult{
		Success: true,
		Data: map[string]any{
			"variables": vars,
			"names":     names,
			"count":     len(vars),
		},
	}, nil
}

func envSearch(pattern string) (*EnvResult, error) {
	if pattern == "" {
		return &EnvResult{Success: false, Error: "pattern required"}, nil
	}

	re, err := regexp.Compile("(?i)" + pattern)
	if err != nil {
		return &EnvResult{Success: false, Error: fmt.Sprintf("invalid pattern: %v", err)}, nil
	}

	env := os.Environ()
	matches := make(map[string]string)
	names := make([]string, 0)

	for _, e := range env {
		parts := strings.SplitN(e, "=", 2)
		if len(parts) != 2 {
			continue
		}
		name, value := parts[0], parts[1]

		if re.MatchString(name) || re.MatchString(value) {
			names = append(names, name)
			if isSensitiveEnv(name) {
				value = "[REDACTED]"
			}
			matches[name] = value
		}
	}

	sort.Strings(names)

	return &EnvResult{
		Success: true,
		Data: map[string]any{
			"matches": matches,
			"names":   names,
			"count":   len(matches),
			"pattern": pattern,
		},
	}, nil
}

func envSet(name, value string) (*EnvResult, error) {
	if name == "" {
		return &EnvResult{Success: false, Error: "name required"}, nil
	}

	if isSensitiveEnv(name) {
		return &EnvResult{Success: false, Error: fmt.Sprintf("cannot set sensitive variable %q", name)}, nil
	}

	if err := os.Setenv(name, value); err != nil {
		return &EnvResult{Success: false, Error: err.Error()}, nil
	}

	return &EnvResult{
		Success: true,
		Data: map[string]any{
			"name":    name,
			"value":   value,
			"message": fmt.Sprintf("variable %s set", name),
		},
	}, nil
}

func envCheck(name string, names []string) (*EnvResult, error) {
	if name != "" {
		names = append([]string{name}, names...)
	}

	if len(names) == 0 {
		return &EnvResult{Success: false, Error: "name or names required"}, nil
	}

	checks := make(map[string]bool)
	allSet := true

	for _, n := range names {
		_, exists := os.LookupEnv(n)
		checks[n] = exists
		if !exists {
			allSet = false
		}
	}

	return &EnvResult{
		Success: true,
		Data: map[string]any{
			"checks": checks,
			"allSet": allSet,
		},
	}, nil
}

func isSensitiveEnv(name string) bool {
	nameUpper := strings.ToUpper(name)
	for _, pattern := range sensitiveEnvPatterns {
		if strings.Contains(nameUpper, pattern) {
			return true
		}
	}
	return false
}
