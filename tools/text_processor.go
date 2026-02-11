package tools

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

type textProcessorArgs struct {
	Operation string   `json:"operation"`
	Text      string   `json:"text"`
	Texts     []string `json:"texts,omitempty"`
	Pattern   string   `json:"pattern,omitempty"`
	Replace   string   `json:"replace,omitempty"`
	Delimiter string   `json:"delimiter,omitempty"`
	Count     int      `json:"count,omitempty"`
}

// TextResult contains the result of a text operation.
type TextResult struct {
	Success bool   `json:"success"`
	Result  any    `json:"result,omitempty"`
	Error   string `json:"error,omitempty"`
}

func NewTextProcessor() Tool {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"operation": map[string]any{
				"type": "string",
				"enum": []string{
					"word_count", "char_count", "line_count",
					"uppercase", "lowercase", "titlecase",
					"trim", "split", "join", "reverse",
					"truncate", "wrap", "indent",
					"extract_emails", "extract_urls", "extract_numbers",
					"slugify", "camelcase", "snakecase", "kebabcase",
					"dedupe_lines", "sort_lines", "summarize",
					"random_string", "template",
				},
				"description": "Text operation to perform.",
			},
			"text": map[string]any{
				"type":        "string",
				"description": "Input text to process.",
			},
			"texts": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Array of texts for join operation.",
			},
			"pattern": map[string]any{
				"type":        "string",
				"description": "Pattern for template operation.",
			},
			"replace": map[string]any{
				"type":        "string",
				"description": "Replacement for template operation.",
			},
			"delimiter": map[string]any{
				"type":        "string",
				"description": "Delimiter for split/join.",
			},
			"count": map[string]any{
				"type":        "integer",
				"description": "Count for truncate/wrap/random_string.",
			},
		},
		"required": []string{"operation"},
	}

	return NewFuncTool(
		"text_processor",
		"Process and transform text: counting, case conversion, extraction, formatting, and more.",
		schema,
		func(ctx context.Context, args json.RawMessage) (any, error) {
			var in textProcessorArgs
			if err := json.Unmarshal(args, &in); err != nil {
				return nil, fmt.Errorf("invalid text_processor args: %w", err)
			}

			return processText(in)
		},
	)
}

func processText(args textProcessorArgs) (*TextResult, error) {
	text := args.Text

	switch args.Operation {
	case "word_count":
		words := strings.Fields(text)
		return &TextResult{Success: true, Result: map[string]int{"words": len(words)}}, nil

	case "char_count":
		return &TextResult{Success: true, Result: map[string]int{
			"characters":          len(text),
			"characters_no_space": len(strings.ReplaceAll(text, " ", "")),
		}}, nil

	case "line_count":
		lines := strings.Split(text, "\n")
		nonEmpty := 0
		for _, l := range lines {
			if strings.TrimSpace(l) != "" {
				nonEmpty++
			}
		}
		return &TextResult{Success: true, Result: map[string]int{"total": len(lines), "nonEmpty": nonEmpty}}, nil

	case "uppercase":
		return &TextResult{Success: true, Result: strings.ToUpper(text)}, nil

	case "lowercase":
		return &TextResult{Success: true, Result: strings.ToLower(text)}, nil

	case "titlecase":
		return &TextResult{Success: true, Result: cases.Title(language.Und).String(text)}, nil

	case "trim":
		return &TextResult{Success: true, Result: strings.TrimSpace(text)}, nil

	case "split":
		delimiter := args.Delimiter
		if delimiter == "" {
			delimiter = ","
		}
		parts := strings.Split(text, delimiter)
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		return &TextResult{Success: true, Result: parts}, nil

	case "join":
		delimiter := args.Delimiter
		if delimiter == "" {
			delimiter = ", "
		}
		return &TextResult{Success: true, Result: strings.Join(args.Texts, delimiter)}, nil

	case "reverse":
		runes := []rune(text)
		for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
			runes[i], runes[j] = runes[j], runes[i]
		}
		return &TextResult{Success: true, Result: string(runes)}, nil

	case "truncate":
		count := args.Count
		if count <= 0 {
			count = 100
		}
		if len(text) <= count {
			return &TextResult{Success: true, Result: text}, nil
		}
		return &TextResult{Success: true, Result: text[:count] + "..."}, nil

	case "wrap":
		count := args.Count
		if count <= 0 {
			count = 80
		}
		return &TextResult{Success: true, Result: wrapText(text, count)}, nil

	case "indent":
		count := args.Count
		if count <= 0 {
			count = 2
		}
		indent := strings.Repeat(" ", count)
		lines := strings.Split(text, "\n")
		for i := range lines {
			if lines[i] != "" {
				lines[i] = indent + lines[i]
			}
		}
		return &TextResult{Success: true, Result: strings.Join(lines, "\n")}, nil

	case "extract_emails":
		re := regexp.MustCompile(`[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}`)
		return &TextResult{Success: true, Result: re.FindAllString(text, -1)}, nil

	case "extract_urls":
		re := regexp.MustCompile(`https?://[^\s]+`)
		return &TextResult{Success: true, Result: re.FindAllString(text, -1)}, nil

	case "extract_numbers":
		re := regexp.MustCompile(`-?\d+\.?\d*`)
		return &TextResult{Success: true, Result: re.FindAllString(text, -1)}, nil

	case "slugify":
		return &TextResult{Success: true, Result: slugify(text)}, nil

	case "camelcase":
		return &TextResult{Success: true, Result: toCamelCase(text)}, nil

	case "snakecase":
		return &TextResult{Success: true, Result: toSnakeCase(text)}, nil

	case "kebabcase":
		return &TextResult{Success: true, Result: toKebabCase(text)}, nil

	case "dedupe_lines":
		lines := strings.Split(text, "\n")
		seen := make(map[string]bool)
		result := make([]string, 0)
		for _, line := range lines {
			if !seen[line] {
				seen[line] = true
				result = append(result, line)
			}
		}
		return &TextResult{Success: true, Result: strings.Join(result, "\n")}, nil

	case "sort_lines":
		lines := strings.Split(text, "\n")
		for i := 0; i < len(lines)-1; i++ {
			for j := 0; j < len(lines)-i-1; j++ {
				if lines[j] > lines[j+1] {
					lines[j], lines[j+1] = lines[j+1], lines[j]
				}
			}
		}
		return &TextResult{Success: true, Result: strings.Join(lines, "\n")}, nil

	case "summarize":
		words := strings.Fields(text)
		sentences := strings.Count(text, ".") + strings.Count(text, "!") + strings.Count(text, "?")
		if sentences == 0 {
			sentences = 1
		}
		wordCount := len(words)
		readTime := wordCount / 200
		if readTime < 1 {
			readTime = 1
		}
		return &TextResult{Success: true, Result: map[string]any{
			"characters": len(text),
			"words":      wordCount,
			"sentences":  sentences,
			"readTime":   fmt.Sprintf("%d min", readTime),
		}}, nil

	case "random_string":
		count := args.Count
		if count <= 0 {
			count = 32
		}
		bytes := make([]byte, count/2+1)
		rand.Read(bytes)
		return &TextResult{Success: true, Result: hex.EncodeToString(bytes)[:count]}, nil

	case "template":
		if args.Pattern == "" {
			return &TextResult{Success: false, Error: "pattern required"}, nil
		}
		result := strings.ReplaceAll(text, args.Pattern, args.Replace)
		return &TextResult{Success: true, Result: result}, nil

	default:
		return &TextResult{Success: false, Error: fmt.Sprintf("unknown operation: %s", args.Operation)}, nil
	}
}

func wrapText(text string, width int) string {
	var result strings.Builder
	words := strings.Fields(text)
	lineLen := 0

	for i, word := range words {
		if lineLen+len(word)+1 > width && lineLen > 0 {
			result.WriteString("\n")
			lineLen = 0
		}
		if lineLen > 0 {
			result.WriteString(" ")
			lineLen++
		}
		result.WriteString(word)
		lineLen += len(word)
		_ = i
	}

	return result.String()
}

func slugify(text string) string {
	text = strings.ToLower(text)
	re := regexp.MustCompile(`[^a-z0-9]+`)
	text = re.ReplaceAllString(text, "-")
	text = strings.Trim(text, "-")
	return text
}

func toCamelCase(text string) string {
	re := regexp.MustCompile(`[^a-zA-Z0-9]+`)
	parts := re.Split(text, -1)

	var result strings.Builder
	for i, part := range parts {
		if part == "" {
			continue
		}
		if i == 0 {
			result.WriteString(strings.ToLower(part))
		} else {
			result.WriteString(strings.ToUpper(string(part[0])))
			if len(part) > 1 {
				result.WriteString(strings.ToLower(part[1:]))
			}
		}
	}
	return result.String()
}

func toSnakeCase(text string) string {
	var result strings.Builder
	for i, r := range text {
		if unicode.IsUpper(r) {
			if i > 0 {
				result.WriteRune('_')
			}
			result.WriteRune(unicode.ToLower(r))
		} else if unicode.IsLetter(r) || unicode.IsDigit(r) {
			result.WriteRune(r)
		} else {
			result.WriteRune('_')
		}
	}
	re := regexp.MustCompile(`_+`)
	return strings.Trim(re.ReplaceAllString(result.String(), "_"), "_")
}

func toKebabCase(text string) string {
	return strings.ReplaceAll(toSnakeCase(text), "_", "-")
}
