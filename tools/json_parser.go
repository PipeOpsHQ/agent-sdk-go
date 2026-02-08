package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

type jsonParserArgs struct {
	JSON  string `json:"json"`
	Query string `json:"query,omitempty"`
}

func NewJSONParser() Tool {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"json": map[string]any{
				"type":        "string",
				"description": "The JSON string to parse and validate.",
			},
			"query": map[string]any{
				"type":        "string",
				"description": "Optional dot-notation path to extract a specific field (e.g., 'user.name', 'items.0.id').",
			},
		},
		"required": []string{"json"},
	}

	return NewFuncTool(
		"json_parser",
		"Parse and validate JSON data. Optionally extract specific fields using dot-notation queries.",
		schema,
		func(ctx context.Context, args json.RawMessage) (any, error) {
			var in jsonParserArgs
			if err := json.Unmarshal(args, &in); err != nil {
				return nil, fmt.Errorf("invalid json_parser args: %w", err)
			}
			if in.JSON == "" {
				return nil, fmt.Errorf("json is required")
			}

			var parsed any
			if err := json.Unmarshal([]byte(in.JSON), &parsed); err != nil {
				return map[string]any{
					"valid": false,
					"error": err.Error(),
				}, nil
			}

			result := map[string]any{
				"valid":  true,
				"parsed": parsed,
			}

			if in.Query != "" {
				value, err := queryJSON(parsed, in.Query)
				if err != nil {
					result["queryError"] = err.Error()
				} else {
					result["queryResult"] = value
				}
			}

			return result, nil
		},
	)
}

// queryJSON extracts a value from parsed JSON using dot-notation.
// Supports object keys and array indices (e.g., "users.0.name").
func queryJSON(data any, query string) (any, error) {
	if query == "" {
		return data, nil
	}

	parts := strings.Split(query, ".")
	current := data

	for _, part := range parts {
		if part == "" {
			continue
		}

		switch v := current.(type) {
		case map[string]any:
			val, exists := v[part]
			if !exists {
				return nil, fmt.Errorf("key %q not found", part)
			}
			current = val

		case []any:
			index, err := strconv.Atoi(part)
			if err != nil {
				return nil, fmt.Errorf("invalid array index %q", part)
			}
			if index < 0 || index >= len(v) {
				return nil, fmt.Errorf("array index %d out of bounds (length %d)", index, len(v))
			}
			current = v[index]

		default:
			return nil, fmt.Errorf("cannot access %q on %T", part, current)
		}
	}

	return current, nil
}

// PrettyPrintJSON formats JSON with indentation.
func PrettyPrintJSON(data any) (string, error) {
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// FlattenJSON flattens a nested JSON structure into dot-notation keys.
func FlattenJSON(data any, prefix string) map[string]any {
	result := make(map[string]any)
	flattenRecursive(data, prefix, result)
	return result
}

func flattenRecursive(data any, prefix string, result map[string]any) {
	switch v := data.(type) {
	case map[string]any:
		for key, val := range v {
			newKey := key
			if prefix != "" {
				newKey = prefix + "." + key
			}
			flattenRecursive(val, newKey, result)
		}
	case []any:
		for i, val := range v {
			newKey := fmt.Sprintf("%d", i)
			if prefix != "" {
				newKey = prefix + "." + newKey
			}
			flattenRecursive(val, newKey, result)
		}
	default:
		if prefix != "" {
			result[prefix] = v
		}
	}
}
