package tools

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

type uuidGeneratorArgs struct {
	Count  int    `json:"count,omitempty"`
	Format string `json:"format,omitempty"`
}

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func NewUUIDGenerator() Tool {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"count": map[string]any{
				"type":        "integer",
				"description": "Number of UUIDs to generate. Defaults to 1. Maximum 100.",
				"minimum":     1,
				"maximum":     100,
			},
			"format": map[string]any{
				"type":        "string",
				"enum":        []string{"standard", "uppercase", "no-dashes"},
				"description": "Output format: standard (lowercase with dashes), uppercase, or no-dashes. Defaults to standard.",
			},
		},
	}

	return NewFuncTool(
		"uuid_generator",
		"Generate random UUIDs (v4). Can generate multiple UUIDs at once with different format options.",
		schema,
		func(ctx context.Context, args json.RawMessage) (any, error) {
			var in uuidGeneratorArgs
			if err := json.Unmarshal(args, &in); err != nil {
				return nil, fmt.Errorf("invalid uuid_generator args: %w", err)
			}

			count := in.Count
			if count <= 0 {
				count = 1
			}
			if count > 100 {
				count = 100
			}

			format := in.Format
			if format == "" {
				format = "standard"
			}

			uuids := make([]string, count)
			for i := 0; i < count; i++ {
				uuid, err := generateUUIDv4()
				if err != nil {
					return nil, fmt.Errorf("failed to generate UUID: %w", err)
				}

				switch format {
				case "uppercase":
					uuid = strings.ToUpper(uuid)
				case "no-dashes":
					uuid = strings.ReplaceAll(uuid, "-", "")
				}
				uuids[i] = uuid
			}

			if count == 1 {
				return map[string]any{
					"uuid":   uuids[0],
					"format": format,
				}, nil
			}

			return map[string]any{
				"uuids":  uuids,
				"count":  count,
				"format": format,
			}, nil
		},
	)
}

// generateUUIDv4 generates a random UUID v4.
func generateUUIDv4() (string, error) {
	uuid := make([]byte, 16)
	_, err := rand.Read(uuid)
	if err != nil {
		return "", err
	}

	// Set version (4) and variant (RFC 4122)
	uuid[6] = (uuid[6] & 0x0f) | 0x40 // Version 4
	uuid[8] = (uuid[8] & 0x3f) | 0x80 // Variant RFC 4122

	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		uuid[0:4],
		uuid[4:6],
		uuid[6:8],
		uuid[8:10],
		uuid[10:16],
	), nil
}

// IsValidUUID checks if a string is a valid UUID format.
func IsValidUUID(s string) bool {
	return uuidPattern.MatchString(s)
}

// ParseUUID extracts components from a UUID string.
func ParseUUID(s string) (map[string]any, error) {
	if !IsValidUUID(s) {
		return nil, fmt.Errorf("invalid UUID format")
	}

	s = strings.ToLower(s)
	parts := strings.Split(s, "-")

	// Extract version from the 3rd segment
	version := parts[2][0:1]

	return map[string]any{
		"uuid":      s,
		"version":   version,
		"valid":     true,
		"timeLow":   parts[0],
		"timeMid":   parts[1],
		"timeHigh":  parts[2],
		"clockSeq":  parts[3],
		"node":      parts[4],
		"compact":   strings.ReplaceAll(s, "-", ""),
		"uppercase": strings.ToUpper(s),
	}, nil
}
