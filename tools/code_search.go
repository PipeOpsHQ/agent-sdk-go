package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type codeSearchArgs struct {
	Path         string   `json:"path"`
	Query        string   `json:"query"`
	Type         string   `json:"type,omitempty"`
	Extensions   []string `json:"extensions,omitempty"`
	MaxResults   int      `json:"maxResults,omitempty"`
	ContextLines int      `json:"contextLines,omitempty"`
	IgnoreCase   bool     `json:"ignoreCase,omitempty"`
}

// CodeSearchResult represents a search match.
type CodeSearchResult struct {
	File       string   `json:"file"`
	Line       int      `json:"line"`
	Column     int      `json:"column"`
	Match      string   `json:"match"`
	Context    []string `json:"context,omitempty"`
	SymbolType string   `json:"symbolType,omitempty"`
}

// CodeSearchResponse contains all search results.
type CodeSearchResponse struct {
	Success    bool               `json:"success"`
	Query      string             `json:"query"`
	Results    []CodeSearchResult `json:"results"`
	TotalCount int                `json:"totalCount"`
	Truncated  bool               `json:"truncated"`
	Error      string             `json:"error,omitempty"`
}

func NewCodeSearch() Tool {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Root directory to search in.",
			},
			"query": map[string]any{
				"type":        "string",
				"description": "Search query (text or regex pattern).",
			},
			"type": map[string]any{
				"type":        "string",
				"enum":        []string{"text", "regex", "symbol", "definition"},
				"description": "Search type: text (literal), regex, symbol, definition. Defaults to text.",
			},
			"extensions": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "File extensions to search (e.g., ['.go', '.py']).",
			},
			"maxResults": map[string]any{
				"type":        "integer",
				"description": "Maximum results. Defaults to 50.",
				"minimum":     1,
				"maximum":     500,
			},
			"contextLines": map[string]any{
				"type":        "integer",
				"description": "Lines of context around matches. Defaults to 2.",
			},
			"ignoreCase": map[string]any{
				"type":        "boolean",
				"description": "Case-insensitive search. Defaults to false.",
			},
		},
		"required": []string{"path", "query"},
	}

	return NewFuncTool(
		"code_search",
		"Search code files for patterns, symbols, and definitions with context display.",
		schema,
		func(ctx context.Context, args json.RawMessage) (any, error) {
			var in codeSearchArgs
			if err := json.Unmarshal(args, &in); err != nil {
				return nil, fmt.Errorf("invalid code_search args: %w", err)
			}
			if in.Path == "" {
				return nil, fmt.Errorf("path is required")
			}
			if in.Query == "" {
				return nil, fmt.Errorf("query is required")
			}

			searchType := in.Type
			if searchType == "" {
				searchType = "text"
			}

			maxResults := in.MaxResults
			if maxResults <= 0 {
				maxResults = 50
			}

			contextLines := in.ContextLines
			if contextLines < 0 {
				contextLines = 2
			}

			return searchCode(ctx, in.Path, in.Query, searchType, in.Extensions, maxResults, contextLines, in.IgnoreCase)
		},
	)
}

func searchCode(ctx context.Context, path, query, searchType string, extensions []string, maxResults, contextLines int, ignoreCase bool) (*CodeSearchResponse, error) {
	response := &CodeSearchResponse{
		Success: true,
		Query:   query,
		Results: make([]CodeSearchResult, 0),
	}

	var pattern *regexp.Regexp
	var err error

	switch searchType {
	case "regex":
		if ignoreCase {
			pattern, err = regexp.Compile("(?i)" + query)
		} else {
			pattern, err = regexp.Compile(query)
		}
	case "symbol", "definition":
		symbolPattern := buildSymbolPattern(query, ignoreCase)
		pattern, err = regexp.Compile(symbolPattern)
	default: // text
		if ignoreCase {
			pattern, err = regexp.Compile("(?i)" + regexp.QuoteMeta(query))
		} else {
			pattern, err = regexp.Compile(regexp.QuoteMeta(query))
		}
	}

	if err != nil {
		return &CodeSearchResponse{Success: false, Error: fmt.Sprintf("invalid pattern: %v", err)}, nil
	}

	if len(extensions) == 0 {
		extensions = []string{
			".go", ".py", ".js", ".ts", ".jsx", ".tsx", ".java", ".c", ".cpp", ".h",
			".rs", ".rb", ".php", ".swift", ".kt", ".scala", ".cs",
			".sh", ".yaml", ".yml", ".json", ".toml", ".xml", ".md",
			".html", ".css", ".sql", ".tf", ".hcl",
		}
	}

	err = filepath.Walk(path, func(filePath string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if len(response.Results) >= maxResults {
			response.Truncated = true
			return filepath.SkipAll
		}

		if info.IsDir() {
			if info.Name() == ".git" || info.Name() == "node_modules" || info.Name() == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}

		ext := strings.ToLower(filepath.Ext(filePath))
		if !containsString(extensions, ext) {
			return nil
		}

		if info.Size() > 1024*1024 {
			return nil
		}

		results := searchInFile(filePath, path, pattern, searchType, contextLines, maxResults-len(response.Results))
		response.Results = append(response.Results, results...)
		return nil
	})

	if err != nil && err != filepath.SkipAll {
		response.Error = err.Error()
	}

	response.TotalCount = len(response.Results)
	return response, nil
}

func searchInFile(filePath, basePath string, pattern *regexp.Regexp, searchType string, contextLines, maxResults int) []CodeSearchResult {
	file, err := os.Open(filePath)
	if err != nil {
		return nil
	}
	defer file.Close()

	var results []CodeSearchResult
	var lines []string
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	for i, line := range lines {
		if len(results) >= maxResults {
			break
		}

		matches := pattern.FindAllStringIndex(line, -1)
		for _, match := range matches {
			relPath, _ := filepath.Rel(basePath, filePath)

			result := CodeSearchResult{
				File:   relPath,
				Line:   i + 1,
				Column: match[0] + 1,
				Match:  line[match[0]:match[1]],
			}

			if contextLines > 0 {
				start := i - contextLines
				if start < 0 {
					start = 0
				}
				end := i + contextLines + 1
				if end > len(lines) {
					end = len(lines)
				}
				for j := start; j < end; j++ {
					prefix := "  "
					if j == i {
						prefix = "> "
					}
					result.Context = append(result.Context, fmt.Sprintf("%d: %s%s", j+1, prefix, lines[j]))
				}
			}

			if searchType == "symbol" || searchType == "definition" {
				result.SymbolType = detectSymbolType(line, filepath.Ext(filePath))
			}

			results = append(results, result)
		}
	}

	return results
}

func buildSymbolPattern(query string, ignoreCase bool) string {
	quotedQuery := regexp.QuoteMeta(query)
	patterns := []string{
		`func\s+(?:\([^)]+\)\s+)?` + quotedQuery + `\s*[\[(]`,
		`type\s+` + quotedQuery + `\s+`,
		`(?:const|var)\s+` + quotedQuery + `\s*=`,
		`def\s+` + quotedQuery + `\s*\(`,
		`class\s+` + quotedQuery + `\s*[:(]`,
		`function\s+` + quotedQuery + `\s*\(`,
		`(?:const|let|var)\s+` + quotedQuery + `\s*=`,
	}

	combined := "(?:" + strings.Join(patterns, "|") + ")"
	if ignoreCase {
		return "(?i)" + combined
	}
	return combined
}

func detectSymbolType(line, ext string) string {
	lineLower := strings.ToLower(line)

	switch ext {
	case ".go":
		if strings.Contains(lineLower, "func ") {
			return "function"
		}
		if strings.Contains(lineLower, "type ") {
			if strings.Contains(lineLower, "struct") {
				return "struct"
			}
			if strings.Contains(lineLower, "interface") {
				return "interface"
			}
			return "type"
		}
		if strings.Contains(lineLower, "const ") {
			return "constant"
		}
		if strings.Contains(lineLower, "var ") {
			return "variable"
		}
	case ".py":
		if strings.Contains(lineLower, "def ") {
			return "function"
		}
		if strings.Contains(lineLower, "class ") {
			return "class"
		}
	case ".js", ".ts", ".jsx", ".tsx":
		if strings.Contains(lineLower, "function ") {
			return "function"
		}
		if strings.Contains(lineLower, "class ") {
			return "class"
		}
		if strings.Contains(lineLower, "const ") || strings.Contains(lineLower, "let ") {
			return "variable"
		}
	}

	return "unknown"
}

func containsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}
