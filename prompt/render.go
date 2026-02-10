package prompt

import (
	"fmt"
	"regexp"
	"strings"
)

var tokenPattern = regexp.MustCompile(`\{\{\s*([a-zA-Z0-9_.-]+)\s*\}\}`)

func Render(template string, vars map[string]string) (string, error) {
	template = strings.TrimSpace(template)
	if template == "" {
		return "", fmt.Errorf("template is required")
	}
	missing := []string{}
	out := tokenPattern.ReplaceAllStringFunc(template, func(match string) string {
		parts := tokenPattern.FindStringSubmatch(match)
		if len(parts) < 2 {
			return match
		}
		key := strings.TrimSpace(parts[1])
		if vars == nil {
			missing = append(missing, key)
			return ""
		}
		value, ok := vars[key]
		if !ok {
			missing = append(missing, key)
			return ""
		}
		return value
	})
	if len(missing) > 0 {
		return "", fmt.Errorf("missing prompt variables: %s", strings.Join(unique(missing), ", "))
	}
	return out, nil
}

func unique(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, v := range values {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}
