package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type diffGeneratorArgs struct {
	Operation string `json:"operation"`
	Original  string `json:"original,omitempty"`
	Modified  string `json:"modified,omitempty"`
	Patch     string `json:"patch,omitempty"`
	Context   int    `json:"context,omitempty"`
}

// DiffResult contains the result of a diff operation.
type DiffResult struct {
	Success bool     `json:"success"`
	Diff    string   `json:"diff,omitempty"`
	Result  string   `json:"result,omitempty"`
	Added   int      `json:"added,omitempty"`
	Removed int      `json:"removed,omitempty"`
	Hunks   []string `json:"hunks,omitempty"`
	Error   string   `json:"error,omitempty"`
}

func NewDiffGenerator() Tool {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"operation": map[string]any{
				"type":        "string",
				"enum":        []string{"generate", "apply", "analyze"},
				"description": "Operation: generate (create diff), apply (apply patch), analyze (analyze changes).",
			},
			"original": map[string]any{
				"type":        "string",
				"description": "Original text content.",
			},
			"modified": map[string]any{
				"type":        "string",
				"description": "Modified text content.",
			},
			"patch": map[string]any{
				"type":        "string",
				"description": "Patch/diff to apply.",
			},
			"context": map[string]any{
				"type":        "integer",
				"description": "Context lines in diff. Defaults to 3.",
			},
		},
		"required": []string{"operation"},
	}

	return NewFuncTool(
		"diff_generator",
		"Generate, apply, and analyze text/code diffs. Create unified diffs and apply patches.",
		schema,
		func(ctx context.Context, args json.RawMessage) (any, error) {
			var in diffGeneratorArgs
			if err := json.Unmarshal(args, &in); err != nil {
				return nil, fmt.Errorf("invalid diff_generator args: %w", err)
			}

			contextLines := in.Context
			if contextLines <= 0 {
				contextLines = 3
			}

			switch in.Operation {
			case "generate":
				return generateDiff(in.Original, in.Modified, contextLines)
			case "apply":
				return applyPatch(in.Original, in.Patch)
			case "analyze":
				return analyzeDiff(in.Original, in.Modified)
			default:
				return nil, fmt.Errorf("unsupported operation %q", in.Operation)
			}
		},
	)
}

func generateDiff(original, modified string, contextLines int) (*DiffResult, error) {
	if original == "" && modified == "" {
		return &DiffResult{Success: true, Diff: ""}, nil
	}

	origLines := strings.Split(original, "\n")
	modLines := strings.Split(modified, "\n")

	diff, hunks, added, removed := computeDiff(origLines, modLines, contextLines)

	return &DiffResult{
		Success: true,
		Diff:    diff,
		Hunks:   hunks,
		Added:   added,
		Removed: removed,
	}, nil
}

func computeDiff(orig, mod []string, context int) (string, []string, int, int) {
	lcs := longestCommonSubsequence(orig, mod)

	var sb strings.Builder
	var hunks []string
	added, removed := 0, 0

	sb.WriteString("--- original\n")
	sb.WriteString("+++ modified\n")

	origIdx, modIdx, lcsIdx := 0, 0, 0
	var currentHunk strings.Builder
	hunkOrigStart, hunkModStart := 1, 1
	hunkOrigCount, hunkModCount := 0, 0
	inHunk := false

	flushHunk := func() {
		if inHunk {
			header := fmt.Sprintf("@@ -%d,%d +%d,%d @@\n",
				hunkOrigStart, hunkOrigCount, hunkModStart, hunkModCount)
			hunks = append(hunks, header+currentHunk.String())
			sb.WriteString(header)
			sb.WriteString(currentHunk.String())
			currentHunk.Reset()
			inHunk = false
			hunkOrigCount = 0
			hunkModCount = 0
		}
	}

	for origIdx < len(orig) || modIdx < len(mod) {
		if lcsIdx < len(lcs) && origIdx < len(orig) && modIdx < len(mod) &&
			orig[origIdx] == lcs[lcsIdx] && mod[modIdx] == lcs[lcsIdx] {
			if inHunk {
				currentHunk.WriteString(" " + orig[origIdx] + "\n")
				hunkOrigCount++
				hunkModCount++
			}
			origIdx++
			modIdx++
			lcsIdx++
		} else if origIdx < len(orig) && (lcsIdx >= len(lcs) || orig[origIdx] != lcs[lcsIdx]) {
			if !inHunk {
				inHunk = true
				hunkOrigStart = origIdx + 1
				hunkModStart = modIdx + 1
			}
			currentHunk.WriteString("-" + orig[origIdx] + "\n")
			hunkOrigCount++
			removed++
			origIdx++
		} else if modIdx < len(mod) && (lcsIdx >= len(lcs) || mod[modIdx] != lcs[lcsIdx]) {
			if !inHunk {
				inHunk = true
				hunkOrigStart = origIdx + 1
				hunkModStart = modIdx + 1
			}
			currentHunk.WriteString("+" + mod[modIdx] + "\n")
			hunkModCount++
			added++
			modIdx++
		}
	}

	flushHunk()

	return sb.String(), hunks, added, removed
}

func longestCommonSubsequence(a, b []string) []string {
	m, n := len(a), len(b)

	dp := make([][]int, m+1)
	for i := range dp {
		dp[i] = make([]int, n+1)
	}

	for i := 1; i <= m; i++ {
		for j := 1; j <= n; j++ {
			if a[i-1] == b[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
			} else {
				if dp[i-1][j] > dp[i][j-1] {
					dp[i][j] = dp[i-1][j]
				} else {
					dp[i][j] = dp[i][j-1]
				}
			}
		}
	}

	lcs := make([]string, 0, dp[m][n])
	i, j := m, n
	for i > 0 && j > 0 {
		if a[i-1] == b[j-1] {
			lcs = append([]string{a[i-1]}, lcs...)
			i--
			j--
		} else if dp[i-1][j] > dp[i][j-1] {
			i--
		} else {
			j--
		}
	}

	return lcs
}

func applyPatch(original, patch string) (*DiffResult, error) {
	if patch == "" {
		return &DiffResult{Success: true, Result: original}, nil
	}

	lines := strings.Split(original, "\n")
	patchLines := strings.Split(patch, "\n")

	result := make([]string, 0, len(lines))
	lineIdx := 0
	patchIdx := 0

	for patchIdx < len(patchLines) {
		line := patchLines[patchIdx]

		if strings.HasPrefix(line, "---") || strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "@@") {
			patchIdx++
			continue
		}

		if len(line) == 0 {
			patchIdx++
			continue
		}

		switch line[0] {
		case ' ':
			if lineIdx < len(lines) {
				result = append(result, lines[lineIdx])
				lineIdx++
			}
			patchIdx++
		case '-':
			lineIdx++
			patchIdx++
		case '+':
			result = append(result, line[1:])
			patchIdx++
		default:
			if lineIdx < len(lines) {
				result = append(result, lines[lineIdx])
				lineIdx++
			}
			patchIdx++
		}
	}

	for lineIdx < len(lines) {
		result = append(result, lines[lineIdx])
		lineIdx++
	}

	return &DiffResult{
		Success: true,
		Result:  strings.Join(result, "\n"),
	}, nil
}

func analyzeDiff(original, modified string) (*DiffResult, error) {
	origLines := strings.Split(original, "\n")
	modLines := strings.Split(modified, "\n")

	lcs := longestCommonSubsequence(origLines, modLines)

	added := len(modLines) - len(lcs)
	removed := len(origLines) - len(lcs)
	unchanged := len(lcs)

	totalLines := len(origLines)
	if len(modLines) > totalLines {
		totalLines = len(modLines)
	}

	similarity := 0.0
	if totalLines > 0 {
		similarity = float64(unchanged) / float64(totalLines) * 100
	}

	return &DiffResult{
		Success: true,
		Added:   added,
		Removed: removed,
		Diff: fmt.Sprintf("Lines: %d original, %d modified\nAdded: %d, Removed: %d, Unchanged: %d\nSimilarity: %.1f%%",
			len(origLines), len(modLines), added, removed, unchanged, similarity),
	}, nil
}
