package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
)

type regexMatcherArgs struct {
	Text       string `json:"text"`
	Pattern    string `json:"pattern"`
	Operation  string `json:"operation,omitempty"`
	Replace    string `json:"replace,omitempty"`
	IgnoreCase bool   `json:"ignoreCase,omitempty"`
}

func NewRegexMatcher() Tool {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"text": map[string]any{
				"type":        "string",
				"description": "The text to search or modify.",
			},
			"pattern": map[string]any{
				"type":        "string",
				"description": "The regular expression pattern.",
			},
			"operation": map[string]any{
				"type":        "string",
				"enum":        []string{"match", "find_all", "replace", "split", "test"},
				"description": "Operation: match (first match), find_all (all matches), replace, split, or test (boolean check). Defaults to match.",
			},
			"replace": map[string]any{
				"type":        "string",
				"description": "Replacement string for replace operation. Supports $1, $2, etc. for capture groups.",
			},
			"ignoreCase": map[string]any{
				"type":        "boolean",
				"description": "Perform case-insensitive matching. Defaults to false.",
			},
		},
		"required": []string{"text", "pattern"},
	}

	return NewFuncTool(
		"regex_matcher",
		"Perform regex operations: match, find_all, replace, split, or test patterns against text.",
		schema,
		func(ctx context.Context, args json.RawMessage) (any, error) {
			var in regexMatcherArgs
			if err := json.Unmarshal(args, &in); err != nil {
				return nil, fmt.Errorf("invalid regex_matcher args: %w", err)
			}
			if in.Text == "" {
				return nil, fmt.Errorf("text is required")
			}
			if in.Pattern == "" {
				return nil, fmt.Errorf("pattern is required")
			}

			pattern := in.Pattern
			if in.IgnoreCase {
				pattern = "(?i)" + pattern
			}

			re, err := regexp.Compile(pattern)
			if err != nil {
				return map[string]any{
					"error":   fmt.Sprintf("invalid regex pattern: %v", err),
					"pattern": in.Pattern,
				}, nil
			}

			operation := in.Operation
			if operation == "" {
				operation = "match"
			}

			switch operation {
			case "test":
				return map[string]any{
					"matches": re.MatchString(in.Text),
					"pattern": in.Pattern,
				}, nil

			case "match":
				match := re.FindStringSubmatch(in.Text)
				if match == nil {
					return map[string]any{
						"found":   false,
						"pattern": in.Pattern,
					}, nil
				}
				result := map[string]any{
					"found":   true,
					"match":   match[0],
					"pattern": in.Pattern,
				}
				if len(match) > 1 {
					result["groups"] = match[1:]
				}
				return result, nil

			case "find_all":
				matches := re.FindAllStringSubmatch(in.Text, -1)
				if matches == nil {
					return map[string]any{
						"found":   false,
						"count":   0,
						"pattern": in.Pattern,
					}, nil
				}
				allMatches := make([]map[string]any, len(matches))
				for i, m := range matches {
					allMatches[i] = map[string]any{
						"match": m[0],
					}
					if len(m) > 1 {
						allMatches[i]["groups"] = m[1:]
					}
				}
				return map[string]any{
					"found":   true,
					"count":   len(matches),
					"matches": allMatches,
					"pattern": in.Pattern,
				}, nil

			case "replace":
				result := re.ReplaceAllString(in.Text, in.Replace)
				return map[string]any{
					"result":  result,
					"pattern": in.Pattern,
					"replace": in.Replace,
				}, nil

			case "split":
				parts := re.Split(in.Text, -1)
				return map[string]any{
					"parts":   parts,
					"count":   len(parts),
					"pattern": in.Pattern,
				}, nil

			default:
				return nil, fmt.Errorf("unsupported operation %q", operation)
			}
		},
	)
}

// CommonPatterns provides pre-built regex patterns for common use cases.
var CommonPatterns = map[string]string{
	"email":       `[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}`,
	"url":         `https?://[^\s]+`,
	"ipv4":        `\b(?:\d{1,3}\.){3}\d{1,3}\b`,
	"ipv6":        `(?:[0-9a-fA-F]{1,4}:){7}[0-9a-fA-F]{1,4}`,
	"phone":       `\+?[\d\s\-().]{10,}`,
	"date_iso":    `\d{4}-\d{2}-\d{2}`,
	"time_24h":    `(?:[01]\d|2[0-3]):[0-5]\d(?::[0-5]\d)?`,
	"hex_color":   `#[0-9a-fA-F]{6}\b`,
	"credit_card": `\b(?:\d{4}[\s-]?){3}\d{4}\b`,
	"ssn":         `\b\d{3}-\d{2}-\d{4}\b`,
	"zip_code":    `\b\d{5}(?:-\d{4})?\b`,
}
