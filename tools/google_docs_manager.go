package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type googleDocsManagerArgs struct {
	Operation   string           `json:"operation"`
	AccessToken string           `json:"access_token,omitempty"`
	DocumentID  string           `json:"document_id,omitempty"`
	Title       string           `json:"title,omitempty"`
	Text        string           `json:"text,omitempty"`
	Requests    []map[string]any `json:"requests,omitempty"`
	PageSize    int              `json:"page_size,omitempty"`
	Query       string           `json:"query,omitempty"`
}

func NewGoogleDocsManager() Tool {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"operation": map[string]any{
				"type":        "string",
				"enum":        []string{"create_doc", "get_doc", "append_text", "batch_update", "list_docs", "export_pdf_link"},
				"description": "Google Docs operation to run.",
			},
			"access_token": map[string]any{
				"type":        "string",
				"description": "OAuth access token. Optional if AGENT_GOOGLE_ACCESS_TOKEN is set.",
			},
			"document_id": map[string]any{"type": "string"},
			"title":       map[string]any{"type": "string"},
			"text":        map[string]any{"type": "string"},
			"requests": map[string]any{
				"type":        "array",
				"description": "Docs API batchUpdate requests.",
			},
			"page_size": map[string]any{"type": "integer"},
			"query":     map[string]any{"type": "string"},
		},
		"required": []string{"operation"},
	}

	return NewFuncTool(
		"google_docs_manager",
		"Manage Google Docs: create, read, append, batch update, list docs, and produce export PDF links.",
		schema,
		func(ctx context.Context, args json.RawMessage) (any, error) {
			var in googleDocsManagerArgs
			if err := json.Unmarshal(args, &in); err != nil {
				return nil, fmt.Errorf("invalid google_docs_manager args: %w", err)
			}
			op := strings.ToLower(strings.TrimSpace(in.Operation))
			if op == "" {
				return nil, fmt.Errorf("operation is required")
			}

			token := strings.TrimSpace(in.AccessToken)
			if token == "" {
				token = strings.TrimSpace(os.Getenv("AGENT_GOOGLE_ACCESS_TOKEN"))
			}
			if token == "" {
				return nil, fmt.Errorf("access_token is required (or set AGENT_GOOGLE_ACCESS_TOKEN)")
			}

			client := &http.Client{Timeout: 30 * time.Second}
			svc := googleDocsService{client: client, token: token}
			switch op {
			case "create_doc":
				return svc.createDoc(ctx, strings.TrimSpace(in.Title))
			case "get_doc":
				return svc.getDoc(ctx, strings.TrimSpace(in.DocumentID))
			case "append_text":
				return svc.appendText(ctx, strings.TrimSpace(in.DocumentID), in.Text)
			case "batch_update":
				return svc.batchUpdate(ctx, strings.TrimSpace(in.DocumentID), in.Requests)
			case "list_docs":
				return svc.listDocs(ctx, in.PageSize, strings.TrimSpace(in.Query))
			case "export_pdf_link":
				id := strings.TrimSpace(in.DocumentID)
				if id == "" {
					return nil, fmt.Errorf("document_id is required")
				}
				return map[string]any{
					"document_id": id,
					"export_url":  "https://docs.google.com/document/d/" + id + "/export?format=pdf",
				}, nil
			default:
				return nil, fmt.Errorf("unsupported operation %q", op)
			}
		},
	)
}

type googleDocsService struct {
	client *http.Client
	token  string
}

func (s googleDocsService) createDoc(ctx context.Context, title string) (map[string]any, error) {
	if title == "" {
		title = "Untitled Document"
	}
	body := map[string]any{"title": title}
	resp, err := s.requestJSON(ctx, http.MethodPost, "https://docs.googleapis.com/v1/documents", body)
	if err != nil {
		return nil, err
	}
	id := asString(resp["documentId"])
	return map[string]any{
		"document_id": id,
		"title":       asString(resp["title"]),
		"url":         "https://docs.google.com/document/d/" + id + "/edit",
		"raw":         resp,
	}, nil
}

func (s googleDocsService) getDoc(ctx context.Context, documentID string) (map[string]any, error) {
	if documentID == "" {
		return nil, fmt.Errorf("document_id is required")
	}
	return s.requestJSON(ctx, http.MethodGet, "https://docs.googleapis.com/v1/documents/"+url.PathEscape(documentID), nil)
}

func (s googleDocsService) appendText(ctx context.Context, documentID, text string) (map[string]any, error) {
	if documentID == "" {
		return nil, fmt.Errorf("document_id is required")
	}
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("text is required")
	}
	end, err := s.documentEndIndex(ctx, documentID)
	if err != nil {
		return nil, err
	}
	requests := []map[string]any{
		{
			"insertText": map[string]any{
				"location": map[string]any{"index": end},
				"text":     text,
			},
		},
	}
	return s.batchUpdate(ctx, documentID, requests)
}

func (s googleDocsService) batchUpdate(ctx context.Context, documentID string, requests []map[string]any) (map[string]any, error) {
	if documentID == "" {
		return nil, fmt.Errorf("document_id is required")
	}
	if len(requests) == 0 {
		return nil, fmt.Errorf("requests cannot be empty")
	}
	body := map[string]any{"requests": requests}
	return s.requestJSON(ctx, http.MethodPost, "https://docs.googleapis.com/v1/documents/"+url.PathEscape(documentID)+":batchUpdate", body)
}

func (s googleDocsService) listDocs(ctx context.Context, pageSize int, query string) (map[string]any, error) {
	if pageSize <= 0 || pageSize > 50 {
		pageSize = 10
	}
	q := "mimeType='application/vnd.google-apps.document' and trashed=false"
	if strings.TrimSpace(query) != "" {
		escaped := strings.ReplaceAll(strings.TrimSpace(query), "'", "\\'")
		q += " and name contains '" + escaped + "'"
	}
	endpoint := "https://www.googleapis.com/drive/v3/files?pageSize=" + fmt.Sprintf("%d", pageSize) +
		"&q=" + url.QueryEscape(q) +
		"&fields=files(id,name,modifiedTime,webViewLink,owners(displayName)),nextPageToken"
	return s.requestJSON(ctx, http.MethodGet, endpoint, nil)
}

func (s googleDocsService) documentEndIndex(ctx context.Context, documentID string) (int, error) {
	doc, err := s.getDoc(ctx, documentID)
	if err != nil {
		return 0, err
	}
	body, ok := doc["body"].(map[string]any)
	if !ok {
		return 1, nil
	}
	content, ok := body["content"].([]any)
	if !ok || len(content) == 0 {
		return 1, nil
	}
	last := content[len(content)-1]
	item, ok := last.(map[string]any)
	if !ok {
		return 1, nil
	}
	end := asInt(item["endIndex"])
	if end <= 1 {
		return 1, nil
	}
	return end - 1, nil
}

func (s googleDocsService) requestJSON(ctx context.Context, method, endpoint string, body any) (map[string]any, error) {
	var bodyReader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		bodyReader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bodyReader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+s.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("google api HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	out := map[string]any{}
	if len(raw) == 0 {
		return out, nil
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func asString(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

func asInt(v any) int {
	switch val := v.(type) {
	case float64:
		return int(val)
	case int:
		return val
	case int64:
		return int(val)
	default:
		return 0
	}
}
