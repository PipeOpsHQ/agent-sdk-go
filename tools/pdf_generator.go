package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/PipeOpsHQ/agent-sdk-go/storage"
)

type pdfGeneratorArgs struct {
	Title      string `json:"title,omitempty"`
	Text       string `json:"text,omitempty"`
	SourcePath string `json:"source_path,omitempty"`
	OutputPath string `json:"output_path,omitempty"`
}

type pdfGeneratorResult struct {
	Title         string              `json:"title,omitempty"`
	OutputPath    string              `json:"output_path"`
	Bytes         int                 `json:"bytes"`
	LinesIncluded int                 `json:"lines_included"`
	Truncated     bool                `json:"truncated"`
	Backup        *storage.BackupInfo `json:"backup,omitempty"`
}

func NewPDFGenerator() Tool {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"title":       map[string]any{"type": "string", "description": "Document title shown in PDF metadata."},
			"text":        map[string]any{"type": "string", "description": "Raw text content to render into PDF."},
			"source_path": map[string]any{"type": "string", "description": "Optional source file path to read text from when text is omitted."},
			"output_path": map[string]any{"type": "string", "description": "Output PDF path. Relative paths are saved under AGENT_STORAGE_DIR (default ./.ai-agent/generated)."},
		},
	}

	return NewFuncTool(
		"pdf_generator",
		"Generate a simple PDF from text content or a source document file.",
		schema,
		func(ctx context.Context, args json.RawMessage) (any, error) {
			var in pdfGeneratorArgs
			if err := json.Unmarshal(args, &in); err != nil {
				return nil, fmt.Errorf("invalid pdf_generator args: %w", err)
			}

			text := strings.TrimSpace(in.Text)
			if text == "" {
				sourcePath := strings.TrimSpace(in.SourcePath)
				if sourcePath == "" {
					return nil, fmt.Errorf("provide either text or source_path")
				}
				content, err := os.ReadFile(sourcePath)
				if err != nil {
					return nil, fmt.Errorf("read source_path: %w", err)
				}
				text = string(content)
			}

			outputPath := strings.TrimSpace(in.OutputPath)
			if outputPath == "" {
				outputPath = ""
			}

			pdf, linesIncluded, truncated := renderSimplePDF(strings.TrimSpace(in.Title), text)
			saved, err := storage.Default().SaveBytes(ctx, outputPath, defaultPDFFileName(strings.TrimSpace(in.Title)), pdf)
			if err != nil {
				return nil, fmt.Errorf("write PDF: %w", err)
			}

			return pdfGeneratorResult{
				Title:         strings.TrimSpace(in.Title),
				OutputPath:    saved.Path,
				Bytes:         len(pdf),
				LinesIncluded: linesIncluded,
				Truncated:     truncated,
				Backup:        saved.Backup,
			}, nil
		},
	)
}

func defaultPDFFileName(title string) string {
	base := strings.TrimSpace(strings.ToLower(title))
	if base == "" {
		base = "document"
	}
	base = strings.ReplaceAll(base, " ", "-")
	return base + ".pdf"
}

func renderSimplePDF(title, text string) ([]byte, int, bool) {
	wrapped := wrapTextLines(text, 95)
	const maxLines = 56
	truncated := len(wrapped) > maxLines
	if truncated {
		wrapped = wrapped[:maxLines]
		wrapped[len(wrapped)-1] = wrapped[len(wrapped)-1] + " ... (truncated)"
	}

	var stream strings.Builder
	stream.WriteString("BT\n")
	stream.WriteString("/F1 12 Tf\n")
	stream.WriteString("50 790 Td\n")
	stream.WriteString("14 TL\n")
	if strings.TrimSpace(title) != "" {
		stream.WriteString("(" + pdfEscape(title) + ") Tj\nT*\n")
		stream.WriteString("(" + pdfEscape(strings.Repeat("=", min(len(title), 70))) + ") Tj\nT*\n")
		stream.WriteString("T*\n")
	}
	for _, line := range wrapped {
		stream.WriteString("(" + pdfEscape(line) + ") Tj\n")
		stream.WriteString("T*\n")
	}
	stream.WriteString("ET\n")

	streamData := stream.String()

	obj1 := "<< /Type /Catalog /Pages 2 0 R >>"
	obj2 := "<< /Type /Pages /Kids [3 0 R] /Count 1 >>"
	obj3 := "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>"
	obj4 := fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(streamData), streamData)
	obj5 := "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>"

	objects := []string{obj1, obj2, obj3, obj4, obj5}
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects)+1)
	for i, obj := range objects {
		offsets[i+1] = buf.Len()
		buf.WriteString(fmt.Sprintf("%d 0 obj\n%s\nendobj\n", i+1, obj))
	}
	xrefPos := buf.Len()
	buf.WriteString(fmt.Sprintf("xref\n0 %d\n", len(objects)+1))
	buf.WriteString("0000000000 65535 f \n")
	for i := 1; i <= len(objects); i++ {
		buf.WriteString(fmt.Sprintf("%010d 00000 n \n", offsets[i]))
	}
	buf.WriteString(fmt.Sprintf("trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xrefPos))
	return buf.Bytes(), len(wrapped), truncated
}

func pdfEscape(line string) string {
	line = strings.ReplaceAll(line, `\`, `\\`)
	line = strings.ReplaceAll(line, "(", `\(`)
	line = strings.ReplaceAll(line, ")", `\)`)
	line = strings.ReplaceAll(line, "\r", "")
	line = strings.ReplaceAll(line, "\n", " ")
	return line
}

func wrapTextLines(text string, width int) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	paragraphs := strings.Split(text, "\n")
	lines := make([]string, 0, len(paragraphs))
	for _, p := range paragraphs {
		p = strings.TrimSpace(p)
		if p == "" {
			lines = append(lines, "")
			continue
		}
		words := strings.Fields(p)
		if len(words) == 0 {
			lines = append(lines, "")
			continue
		}
		cur := words[0]
		for _, w := range words[1:] {
			if len(cur)+1+len(w) <= width {
				cur += " " + w
			} else {
				lines = append(lines, cur)
				cur = w
			}
		}
		lines = append(lines, cur)
	}
	if len(lines) == 0 {
		return []string{""}
	}
	return lines
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
