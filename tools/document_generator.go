package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/PipeOpsHQ/agent-sdk-go/storage"
)

type documentSection struct {
	Heading string `json:"heading"`
	Content string `json:"content"`
}

type documentGeneratorArgs struct {
	DocType    string            `json:"doc_type,omitempty"`
	Title      string            `json:"title"`
	Summary    string            `json:"summary,omitempty"`
	Sections   []documentSection `json:"sections,omitempty"`
	Format     string            `json:"format,omitempty"`
	OutputPath string            `json:"output_path,omitempty"`
}

type documentGeneratorResult struct {
	DocType    string              `json:"doc_type"`
	Format     string              `json:"format"`
	Title      string              `json:"title"`
	Content    string              `json:"content"`
	OutputPath string              `json:"output_path,omitempty"`
	Bytes      int                 `json:"bytes"`
	Backup     *storage.BackupInfo `json:"backup,omitempty"`
}

func NewDocumentGenerator() Tool {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"doc_type": map[string]any{
				"type":        "string",
				"enum":        []string{"plan", "report", "rfc", "runbook", "notes", "generic"},
				"description": "Document type template to use.",
			},
			"title": map[string]any{
				"type":        "string",
				"description": "Document title.",
			},
			"summary": map[string]any{
				"type":        "string",
				"description": "Short summary for the document.",
			},
			"sections": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"heading": map[string]any{"type": "string"},
						"content": map[string]any{"type": "string"},
					},
				},
			},
			"format": map[string]any{
				"type":        "string",
				"enum":        []string{"markdown", "text", "html"},
				"description": "Output format. Defaults to markdown.",
			},
			"output_path": map[string]any{
				"type":        "string",
				"description": "Optional file path. Relative paths are saved under AGENT_STORAGE_DIR (default ./.ai-agent/generated).",
			},
		},
		"required": []string{"title"},
	}

	return NewFuncTool(
		"document_generator",
		"Generate structured documents (plan/report/rfc/runbook/notes) in markdown, text, or html and optionally save to file.",
		schema,
		func(ctx context.Context, args json.RawMessage) (any, error) {
			var in documentGeneratorArgs
			if err := json.Unmarshal(args, &in); err != nil {
				return nil, fmt.Errorf("invalid document_generator args: %w", err)
			}
			title := strings.TrimSpace(in.Title)
			if title == "" {
				return nil, fmt.Errorf("title is required")
			}
			docType := strings.ToLower(strings.TrimSpace(in.DocType))
			if docType == "" {
				docType = "generic"
			}
			format := strings.ToLower(strings.TrimSpace(in.Format))
			if format == "" {
				format = "markdown"
			}
			if format != "markdown" && format != "text" && format != "html" {
				return nil, fmt.Errorf("unsupported format %q", format)
			}

			sections := in.Sections
			if len(sections) == 0 {
				sections = defaultSectionsForType(docType)
			}

			content := renderDocument(format, docType, title, strings.TrimSpace(in.Summary), sections)
			result := documentGeneratorResult{
				DocType: docType,
				Format:  format,
				Title:   title,
				Content: content,
				Bytes:   len(content),
			}

			if path := strings.TrimSpace(in.OutputPath); path != "" {
				saved, err := storage.Default().SaveBytes(ctx, path, defaultDocumentFileName(title, format), []byte(content))
				if err != nil {
					return nil, fmt.Errorf("write document: %w", err)
				}
				result.OutputPath = saved.Path
				result.Backup = saved.Backup
			} else {
				saved, err := storage.Default().SaveBytes(ctx, "", defaultDocumentFileName(title, format), []byte(content))
				if err != nil {
					return nil, fmt.Errorf("write document: %w", err)
				}
				result.OutputPath = saved.Path
				result.Backup = saved.Backup
			}

			return result, nil
		},
	)
}

func defaultDocumentFileName(title, format string) string {
	base := strings.TrimSpace(strings.ToLower(title))
	if base == "" {
		base = "document"
	}
	base = strings.ReplaceAll(base, " ", "-")
	if format == "html" {
		return base + ".html"
	}
	if format == "text" {
		return base + ".txt"
	}
	return base + ".md"
}

func defaultSectionsForType(docType string) []documentSection {
	switch docType {
	case "plan":
		return []documentSection{{Heading: "Goals"}, {Heading: "Scope"}, {Heading: "Execution Steps"}, {Heading: "Risks"}, {Heading: "Validation"}}
	case "report":
		return []documentSection{{Heading: "Executive Summary"}, {Heading: "Findings"}, {Heading: "Analysis"}, {Heading: "Recommendations"}, {Heading: "Next Actions"}}
	case "rfc":
		return []documentSection{{Heading: "Context"}, {Heading: "Proposal"}, {Heading: "Alternatives"}, {Heading: "Trade-offs"}, {Heading: "Rollout Plan"}}
	case "runbook":
		return []documentSection{{Heading: "Purpose"}, {Heading: "Prerequisites"}, {Heading: "Procedure"}, {Heading: "Rollback"}, {Heading: "Verification"}}
	case "notes":
		return []documentSection{{Heading: "Key Points"}, {Heading: "Decisions"}, {Heading: "Action Items"}}
	default:
		return []documentSection{{Heading: "Overview"}, {Heading: "Details"}, {Heading: "Next Steps"}}
	}
}

func renderDocument(format, docType, title, summary string, sections []documentSection) string {
	if format == "html" {
		var b strings.Builder
		b.WriteString("<h1>" + escapeHTML(title) + "</h1>\n")
		if summary != "" {
			b.WriteString("<p><strong>Summary:</strong> " + escapeHTML(summary) + "</p>\n")
		}
		if docType != "" {
			b.WriteString("<p><em>Type: " + escapeHTML(docType) + "</em></p>\n")
		}
		for _, s := range sections {
			h := strings.TrimSpace(s.Heading)
			if h == "" {
				continue
			}
			b.WriteString("<h2>" + escapeHTML(h) + "</h2>\n")
			if strings.TrimSpace(s.Content) != "" {
				b.WriteString("<p>" + escapeHTML(strings.TrimSpace(s.Content)) + "</p>\n")
			} else {
				b.WriteString("<p>TODO</p>\n")
			}
		}
		return b.String()
	}

	var b strings.Builder
	if format == "markdown" {
		b.WriteString("# " + title + "\n\n")
		if summary != "" {
			b.WriteString("**Summary:** " + summary + "\n\n")
		}
		if docType != "" {
			b.WriteString("_Type: " + docType + "_\n\n")
		}
		for _, s := range sections {
			h := strings.TrimSpace(s.Heading)
			if h == "" {
				continue
			}
			b.WriteString("## " + h + "\n")
			if strings.TrimSpace(s.Content) != "" {
				b.WriteString(strings.TrimSpace(s.Content) + "\n\n")
			} else {
				b.WriteString("TODO\n\n")
			}
		}
		return b.String()
	}

	b.WriteString(title + "\n")
	b.WriteString(strings.Repeat("=", len(title)) + "\n\n")
	if summary != "" {
		b.WriteString("Summary: " + summary + "\n\n")
	}
	if docType != "" {
		b.WriteString("Type: " + docType + "\n\n")
	}
	for _, s := range sections {
		h := strings.TrimSpace(s.Heading)
		if h == "" {
			continue
		}
		b.WriteString(h + "\n")
		b.WriteString(strings.Repeat("-", len(h)) + "\n")
		if strings.TrimSpace(s.Content) != "" {
			b.WriteString(strings.TrimSpace(s.Content) + "\n\n")
		} else {
			b.WriteString("TODO\n\n")
		}
	}
	return b.String()
}

func escapeHTML(v string) string {
	v = strings.ReplaceAll(v, "&", "&amp;")
	v = strings.ReplaceAll(v, "<", "&lt;")
	v = strings.ReplaceAll(v, ">", "&gt;")
	v = strings.ReplaceAll(v, `"`, "&quot;")
	return v
}
