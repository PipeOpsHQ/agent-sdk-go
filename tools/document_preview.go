package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type documentPreviewArgs struct {
	Operation string `json:"operation,omitempty"`
	Path      string `json:"path,omitempty"`
	Content   string `json:"content,omitempty"`
	Title     string `json:"title,omitempty"`
	MaxChars  int    `json:"max_chars,omitempty"`
}

func NewDocumentPreview() Tool {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"operation": map[string]any{
				"type":        "string",
				"enum":        []string{"from_path", "from_content"},
				"description": "Generate preview from a file path or direct content. Defaults to from_path when path is provided.",
			},
			"path": map[string]any{
				"type":        "string",
				"description": "Document path (pdf/md/txt/html).",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "Inline content to preview for chat response.",
			},
			"title": map[string]any{
				"type":        "string",
				"description": "Optional title for the chat-ready output.",
			},
			"max_chars": map[string]any{
				"type":        "integer",
				"description": "Maximum characters returned in preview (default 5000, max 20000).",
			},
		},
	}

	return NewFuncTool(
		"document_preview",
		"Prepare a chat-ready document preview and view/download links for generated docs.",
		schema,
		func(ctx context.Context, args json.RawMessage) (any, error) {
			_ = ctx
			var in documentPreviewArgs
			if err := json.Unmarshal(args, &in); err != nil {
				return nil, fmt.Errorf("invalid document_preview args: %w", err)
			}

			maxChars := in.MaxChars
			if maxChars <= 0 {
				maxChars = 5000
			}
			if maxChars > 20000 {
				maxChars = 20000
			}

			op := strings.ToLower(strings.TrimSpace(in.Operation))
			if op == "" {
				if strings.TrimSpace(in.Path) != "" {
					op = "from_path"
				} else {
					op = "from_content"
				}
			}

			switch op {
			case "from_path":
				return previewFromPath(strings.TrimSpace(in.Path), strings.TrimSpace(in.Title), maxChars)
			case "from_content":
				return previewFromContent(strings.TrimSpace(in.Content), strings.TrimSpace(in.Title), maxChars)
			default:
				return nil, fmt.Errorf("unsupported operation %q", op)
			}
		},
	)
}

func previewFromPath(path, title string, maxChars int) (map[string]any, error) {
	if path == "" {
		return nil, fmt.Errorf("path is required")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read path: %w", err)
	}
	ext := strings.ToLower(filepath.Ext(path))
	name := filepath.Base(path)
	if title == "" {
		title = name
	}

	view := "/api/v1/files/view?path=" + url.QueryEscape(path)
	download := "/api/v1/files/download?path=" + url.QueryEscape(path)

	out := map[string]any{
		"title":         title,
		"path":          path,
		"file_name":     name,
		"view_url":      view,
		"download_url":  download,
		"content_type":  ext,
		"bytes":         len(content),
		"chat_markdown": "",
	}

	if ext == ".pdf" {
		out["preview"] = "PDF generated. Use view/download links."
		out["chat_markdown"] = fmt.Sprintf("### %s\n- [View PDF](%s)\n- [Download PDF](%s)", title, view, download)
		return out, nil
	}

	text := string(content)
	if ext == ".html" || ext == ".htm" {
		text = stripHTMLTags(text)
	}
	preview, truncated := truncateText(text, maxChars)
	out["preview"] = preview
	out["truncated"] = truncated
	out["chat_markdown"] = fmt.Sprintf("### %s\n\n%s\n\n- [View File](%s)\n- [Download File](%s)", title, preview, view, download)
	return out, nil
}

func previewFromContent(content, title string, maxChars int) (map[string]any, error) {
	if content == "" {
		return nil, fmt.Errorf("content is required")
	}
	if title == "" {
		title = "Generated Document"
	}
	preview, truncated := truncateText(content, maxChars)
	return map[string]any{
		"title":         title,
		"preview":       preview,
		"truncated":     truncated,
		"chat_markdown": fmt.Sprintf("### %s\n\n%s", title, preview),
	}, nil
}

func truncateText(s string, maxChars int) (string, bool) {
	s = strings.TrimSpace(s)
	if len(s) <= maxChars {
		return s, false
	}
	return strings.TrimSpace(s[:maxChars]) + "\n\n... (truncated)", true
}

var htmlTagRegex = regexp.MustCompile(`<[^>]+>`)

func stripHTMLTags(s string) string {
	s = htmlTagRegex.ReplaceAllString(s, " ")
	s = strings.ReplaceAll(s, "&nbsp;", " ")
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&gt;", ">")
	s = regexp.MustCompile(`\s+`).ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}
