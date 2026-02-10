package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"
)

type webSearchArgs struct {
	Query      string `json:"query"`
	NumResults int    `json:"num_results,omitempty"`
}

type SearchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet,omitempty"`
}

type SearchResponse struct {
	Query   string         `json:"query"`
	Engine  string         `json:"engine"`
	Count   int            `json:"count"`
	Results []SearchResult `json:"results"`
}

func NewWebSearch() Tool {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "Search query text, e.g. 'PipeOps company overview'.",
			},
			"num_results": map[string]any{
				"type":        "integer",
				"description": "Maximum number of results to return (1-10, default 5).",
			},
		},
		"required": []string{"query"},
	}

	return NewFuncTool(
		"web_search",
		"Search the web for pages related to a query and return top results with title, URL, and snippet.",
		schema,
		func(ctx context.Context, args json.RawMessage) (any, error) {
			var in webSearchArgs
			if err := json.Unmarshal(args, &in); err != nil {
				return nil, fmt.Errorf("invalid web_search args: %w", err)
			}
			query := strings.TrimSpace(in.Query)
			if query == "" {
				return nil, fmt.Errorf("query is required")
			}
			maxResults := in.NumResults
			if maxResults <= 0 {
				maxResults = 5
			}
			if maxResults > 10 {
				maxResults = 10
			}
			return runWebSearch(ctx, query, maxResults)
		},
	)
}

func runWebSearch(ctx context.Context, query string, maxResults int) (*SearchResponse, error) {
	endpoint := "https://duckduckgo.com/html/?q=" + url.QueryEscape(query)
	client := &http.Client{Timeout: 25 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; AI-Agent-Framework/1.0)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("search request failed with HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 3*1024*1024))
	if err != nil {
		return nil, err
	}
	doc, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}

	results := parseDuckResults(doc, maxResults)
	return &SearchResponse{Query: query, Engine: "duckduckgo", Count: len(results), Results: results}, nil
}

func parseDuckResults(doc *html.Node, maxResults int) []SearchResult {
	results := make([]SearchResult, 0, maxResults)
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n == nil || len(results) >= maxResults {
			return
		}
		if n.Type == html.ElementNode && n.Data == "a" && hasClass(n, "result__a") {
			title := strings.TrimSpace(nodeText(n))
			href := attrValue(n, "href")
			resolved := resolveDuckResultURL(href)
			snippet := extractResultSnippet(n)
			if title != "" && resolved != "" {
				results = append(results, SearchResult{Title: title, URL: resolved, Snippet: snippet})
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return results
}

func extractResultSnippet(anchor *html.Node) string {
	for p := anchor.Parent; p != nil; p = p.Parent {
		if p.Type == html.ElementNode && (hasClass(p, "result") || hasClass(p, "result__body")) {
			if snippet := findByClassText(p, "result__snippet"); snippet != "" {
				return snippet
			}
		}
	}
	return ""
}

func findByClassText(root *html.Node, className string) string {
	var walk func(*html.Node) string
	walk = func(n *html.Node) string {
		if n.Type == html.ElementNode && hasClass(n, className) {
			return strings.TrimSpace(nodeText(n))
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if text := walk(c); text != "" {
				return text
			}
		}
		return ""
	}
	return walk(root)
}

func hasClass(n *html.Node, className string) bool {
	classAttr := attrValue(n, "class")
	if classAttr == "" {
		return false
	}
	for _, c := range strings.Fields(classAttr) {
		if c == className {
			return true
		}
	}
	return false
}

func attrValue(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, key) {
			return strings.TrimSpace(a.Val)
		}
	}
	return ""
}

func nodeText(n *html.Node) string {
	if n == nil {
		return ""
	}
	if n.Type == html.TextNode {
		return n.Data
	}
	var b strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		b.WriteString(nodeText(c))
		b.WriteString(" ")
	}
	return strings.TrimSpace(b.String())
}

func resolveDuckResultURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if q := u.Query().Get("uddg"); q != "" {
		decoded, err := url.QueryUnescape(q)
		if err == nil && decoded != "" {
			return decoded
		}
	}
	if strings.HasPrefix(raw, "/") {
		return "https://duckduckgo.com" + raw
	}
	if i, err := strconv.Atoi(raw); err == nil && i > 0 {
		return ""
	}
	return raw
}
