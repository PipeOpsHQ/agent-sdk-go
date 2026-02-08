package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type timestampConverterArgs struct {
	Input    string `json:"input"`
	FromType string `json:"fromType"`
	ToType   string `json:"toType,omitempty"`
	Timezone string `json:"timezone,omitempty"`
}

func NewTimestampConverter() Tool {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"input": map[string]any{
				"type":        "string",
				"description": "The timestamp to convert, or \"now\" to get the current time. Can be a Unix timestamp (seconds or milliseconds) or a date string.",
			},
			"fromType": map[string]any{
				"type":        "string",
				"enum":        []string{"unix", "unix_ms", "rfc3339", "iso8601", "date"},
				"description": "The format of the input: unix (seconds), unix_ms (milliseconds), rfc3339, iso8601, or date (YYYY-MM-DD). Not required when input is \"now\".",
			},
			"toType": map[string]any{
				"type":        "string",
				"enum":        []string{"unix", "unix_ms", "rfc3339", "iso8601", "human"},
				"description": "The desired output format. Defaults to providing all formats.",
			},
			"timezone": map[string]any{
				"type":        "string",
				"description": "Timezone for output (e.g., 'America/New_York', 'UTC'). Defaults to UTC.",
			},
		},
		"required": []string{"input"},
	}

	return NewFuncTool(
		"timestamp_converter",
		"Convert between Unix timestamps and human-readable date formats (RFC3339, ISO8601). Supports timezone conversion.",
		schema,
		func(ctx context.Context, args json.RawMessage) (any, error) {
			var in timestampConverterArgs
			if err := json.Unmarshal(args, &in); err != nil {
				return nil, fmt.Errorf("invalid timestamp_converter args: %w", err)
			}
			if in.Input == "" {
				return nil, fmt.Errorf("input is required")
			}

			// Handle "now" — return current time
			if strings.EqualFold(in.Input, "now") {
				result, err := Now(in.Timezone)
				if err != nil {
					return map[string]any{"error": err.Error()}, nil
				}
				if in.ToType != "" {
					loc := time.UTC
					if in.Timezone != "" {
						loc, _ = time.LoadLocation(in.Timezone)
					}
					return formatTimestamp(time.Now().In(loc), in.ToType), nil
				}
				return result, nil
			}

			if in.FromType == "" {
				return nil, fmt.Errorf("fromType is required when input is not \"now\"")
			}

			// Parse input to time.Time
			t, err := parseTimestamp(in.Input, in.FromType)
			if err != nil {
				return map[string]any{
					"error": err.Error(),
				}, nil
			}

			// Apply timezone
			loc := time.UTC
			if in.Timezone != "" {
				var tzErr error
				loc, tzErr = time.LoadLocation(in.Timezone)
				if tzErr != nil {
					return map[string]any{
						"error": fmt.Sprintf("invalid timezone %q: %v", in.Timezone, tzErr),
					}, nil
				}
			}
			t = t.In(loc)

			// Format output
			result := formatTimestamp(t, in.ToType)
			return result, nil
		},
	)
}

func parseTimestamp(input, fromType string) (time.Time, error) {
	switch fromType {
	case "unix":
		secs, err := strconv.ParseInt(input, 10, 64)
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid unix timestamp: %w", err)
		}
		return time.Unix(secs, 0), nil

	case "unix_ms":
		ms, err := strconv.ParseInt(input, 10, 64)
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid unix millisecond timestamp: %w", err)
		}
		return time.Unix(ms/1000, (ms%1000)*int64(time.Millisecond)), nil

	case "rfc3339":
		t, err := time.Parse(time.RFC3339, input)
		if err != nil {
			// Try with nanoseconds
			t, err = time.Parse(time.RFC3339Nano, input)
			if err != nil {
				return time.Time{}, fmt.Errorf("invalid RFC3339 timestamp: %w", err)
			}
		}
		return t, nil

	case "iso8601":
		// ISO8601 is similar to RFC3339 but with some variations
		formats := []string{
			"2006-01-02T15:04:05Z07:00",
			"2006-01-02T15:04:05",
			"2006-01-02T15:04:05Z",
			"2006-01-02 15:04:05",
		}
		for _, layout := range formats {
			if t, err := time.Parse(layout, input); err == nil {
				return t, nil
			}
		}
		return time.Time{}, fmt.Errorf("invalid ISO8601 timestamp")

	case "date":
		t, err := time.Parse("2006-01-02", input)
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid date (expected YYYY-MM-DD): %w", err)
		}
		return t, nil

	default:
		return time.Time{}, fmt.Errorf("unsupported fromType %q", fromType)
	}
}

func formatTimestamp(t time.Time, toType string) map[string]any {
	result := map[string]any{
		"timezone": t.Location().String(),
	}

	switch toType {
	case "unix":
		result["result"] = t.Unix()
	case "unix_ms":
		result["result"] = t.UnixMilli()
	case "rfc3339":
		result["result"] = t.Format(time.RFC3339)
	case "iso8601":
		result["result"] = t.Format("2006-01-02T15:04:05Z07:00")
	case "human":
		result["result"] = t.Format("Monday, January 2, 2006 at 3:04 PM MST")
	default:
		// Return all formats
		result["unix"] = t.Unix()
		result["unix_ms"] = t.UnixMilli()
		result["rfc3339"] = t.Format(time.RFC3339)
		result["iso8601"] = t.Format("2006-01-02T15:04:05Z07:00")
		result["human"] = t.Format("Monday, January 2, 2006 at 3:04 PM MST")
		result["date"] = t.Format("2006-01-02")
		result["time"] = t.Format("15:04:05")
	}

	return result
}

// Now returns the current time in various formats.
func Now(tz string) (map[string]any, error) {
	loc := time.UTC
	if tz != "" {
		var err error
		loc, err = time.LoadLocation(tz)
		if err != nil {
			return nil, err
		}
	}
	return formatTimestamp(time.Now().In(loc), ""), nil
}

// ParseDuration parses a duration string and returns its components.
func ParseDuration(s string) (map[string]any, error) {
	d, err := time.ParseDuration(s)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"nanoseconds":  d.Nanoseconds(),
		"microseconds": d.Microseconds(),
		"milliseconds": d.Milliseconds(),
		"seconds":      d.Seconds(),
		"minutes":      d.Minutes(),
		"hours":        d.Hours(),
		"string":       d.String(),
	}, nil
}
