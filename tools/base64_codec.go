package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
)

type base64Args struct {
	Input     string `json:"input"`
	Operation string `json:"operation"`
	URLSafe   bool   `json:"urlSafe,omitempty"`
}

func NewBase64Codec() Tool {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"input": map[string]any{
				"type":        "string",
				"description": "The string to encode or decode.",
			},
			"operation": map[string]any{
				"type":        "string",
				"enum":        []string{"encode", "decode"},
				"description": "Whether to 'encode' or 'decode' the input.",
			},
			"urlSafe": map[string]any{
				"type":        "boolean",
				"description": "Use URL-safe base64 encoding (uses - and _ instead of + and /). Default is false.",
			},
		},
		"required": []string{"input", "operation"},
	}

	return NewFuncTool(
		"base64_codec",
		"Encode or decode base64 strings. Supports both standard and URL-safe encoding.",
		schema,
		func(ctx context.Context, args json.RawMessage) (any, error) {
			var in base64Args
			if err := json.Unmarshal(args, &in); err != nil {
				return nil, fmt.Errorf("invalid base64_codec args: %w", err)
			}
			if in.Input == "" {
				return nil, fmt.Errorf("input is required")
			}

			var encoding *base64.Encoding
			if in.URLSafe {
				encoding = base64.URLEncoding
			} else {
				encoding = base64.StdEncoding
			}

			switch in.Operation {
			case "encode":
				encoded := encoding.EncodeToString([]byte(in.Input))
				return map[string]any{
					"result":  encoded,
					"urlSafe": in.URLSafe,
				}, nil

			case "decode":
				// Try with padding first, then without
				decoded, err := encoding.DecodeString(in.Input)
				if err != nil {
					// Try raw encoding (no padding)
					var rawEncoding *base64.Encoding
					if in.URLSafe {
						rawEncoding = base64.RawURLEncoding
					} else {
						rawEncoding = base64.RawStdEncoding
					}
					decoded, err = rawEncoding.DecodeString(in.Input)
					if err != nil {
						return map[string]any{
							"error": fmt.Sprintf("failed to decode base64: %v", err),
						}, nil
					}
				}
				return map[string]any{
					"result":  string(decoded),
					"urlSafe": in.URLSafe,
				}, nil

			default:
				return nil, fmt.Errorf("operation must be 'encode' or 'decode', got %q", in.Operation)
			}
		},
	)
}
