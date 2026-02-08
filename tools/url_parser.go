package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

type urlParserArgs struct {
	URL       string `json:"url"`
	Operation string `json:"operation,omitempty"`
}

func NewURLParser() Tool {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"url": map[string]any{
				"type":        "string",
				"description": "The URL to parse or validate.",
			},
			"operation": map[string]any{
				"type":        "string",
				"enum":        []string{"parse", "validate", "encode", "decode"},
				"description": "Operation: parse (extract components), validate (check if valid), encode (URL encode), decode (URL decode). Defaults to parse.",
			},
		},
		"required": []string{"url"},
	}

	return NewFuncTool(
		"url_parser",
		"Parse, validate, encode, or decode URLs. Extracts components like scheme, host, path, and query parameters.",
		schema,
		func(ctx context.Context, args json.RawMessage) (any, error) {
			var in urlParserArgs
			if err := json.Unmarshal(args, &in); err != nil {
				return nil, fmt.Errorf("invalid url_parser args: %w", err)
			}
			if in.URL == "" {
				return nil, fmt.Errorf("url is required")
			}

			operation := in.Operation
			if operation == "" {
				operation = "parse"
			}

			switch operation {
			case "parse":
				return parseURL(in.URL)

			case "validate":
				parsed, err := url.Parse(in.URL)
				isValid := err == nil && parsed.Scheme != "" && parsed.Host != ""
				result := map[string]any{
					"valid": isValid,
					"url":   in.URL,
				}
				if !isValid && err != nil {
					result["error"] = err.Error()
				}
				return result, nil

			case "encode":
				return map[string]any{
					"result":   url.QueryEscape(in.URL),
					"original": in.URL,
				}, nil

			case "decode":
				decoded, err := url.QueryUnescape(in.URL)
				if err != nil {
					return map[string]any{
						"error":    err.Error(),
						"original": in.URL,
					}, nil
				}
				return map[string]any{
					"result":   decoded,
					"original": in.URL,
				}, nil

			default:
				return nil, fmt.Errorf("unsupported operation %q", operation)
			}
		},
	)
}

func parseURL(rawURL string) (map[string]any, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return map[string]any{
			"valid": false,
			"error": err.Error(),
			"url":   rawURL,
		}, nil
	}

	result := map[string]any{
		"valid":    true,
		"url":      rawURL,
		"scheme":   parsed.Scheme,
		"host":     parsed.Host,
		"hostname": parsed.Hostname(),
		"path":     parsed.Path,
		"rawQuery": parsed.RawQuery,
		"fragment": parsed.Fragment,
	}

	// Parse port if present
	if port := parsed.Port(); port != "" {
		result["port"] = port
	}

	// Parse user info if present
	if parsed.User != nil {
		result["username"] = parsed.User.Username()
		if pwd, set := parsed.User.Password(); set {
			result["password"] = pwd
		}
	}

	// Parse query parameters
	if parsed.RawQuery != "" {
		queryParams := make(map[string]any)
		for key, values := range parsed.Query() {
			if len(values) == 1 {
				queryParams[key] = values[0]
			} else {
				queryParams[key] = values
			}
		}
		result["queryParams"] = queryParams
	}

	// Parse path segments
	if parsed.Path != "" {
		segments := strings.Split(strings.Trim(parsed.Path, "/"), "/")
		if len(segments) > 0 && segments[0] != "" {
			result["pathSegments"] = segments
		}
	}

	return result, nil
}

// BuildURL constructs a URL from components.
func BuildURL(scheme, host, path string, queryParams map[string]string) string {
	u := url.URL{
		Scheme: scheme,
		Host:   host,
		Path:   path,
	}

	if len(queryParams) > 0 {
		q := url.Values{}
		for k, v := range queryParams {
			q.Set(k, v)
		}
		u.RawQuery = q.Encode()
	}

	return u.String()
}

// JoinPath safely joins path segments.
func JoinPath(base string, segments ...string) string {
	result := strings.TrimRight(base, "/")
	for _, seg := range segments {
		seg = strings.Trim(seg, "/")
		if seg != "" {
			result = result + "/" + seg
		}
	}
	return result
}
