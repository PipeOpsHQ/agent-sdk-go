package catalog

import "strings"

// MergeSelection merges static CLI/environment tool selection with workflow bindings.
func MergeSelection(base []string, binding WorkflowToolBinding, bundlesByID map[string]ToolBundle) []string {
	ordered := make([]string, 0, len(base)+len(binding.ToolNames)+len(binding.BundleIDs))
	seen := map[string]bool{}

	appendToken := func(token string) {
		token = strings.TrimSpace(token)
		if token == "" || seen[token] {
			return
		}
		seen[token] = true
		ordered = append(ordered, token)
	}

	for _, token := range base {
		appendToken(token)
	}
	for _, bundleID := range binding.BundleIDs {
		bundle, ok := bundlesByID[bundleID]
		if !ok {
			continue
		}
		if bundle.Name != "" {
			appendToken("@" + bundle.Name)
			continue
		}
		for _, tool := range bundle.ToolNames {
			appendToken(tool)
		}
	}
	for _, tool := range binding.ToolNames {
		appendToken(tool)
	}
	return ordered
}
