package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"golang.org/x/net/html"
)

type webScraperArgs struct {
	URL     string `json:"url"`
	Extract string `json:"extract,omitempty"`
}

// ScrapedContent represents extracted content from a web page.
type ScrapedContent struct {
	URL         string            `json:"url"`
	Title       string            `json:"title"`
	Description string            `json:"description,omitempty"`
	Text        string            `json:"text,omitempty"`
	Links       []string          `json:"links,omitempty"`
	Headings    []string          `json:"headings,omitempty"`
	Images      []string          `json:"images,omitempty"`
	Meta        map[string]string `json:"meta,omitempty"`
	Error       string            `json:"error,omitempty"`
	StatusCode  int               `json:"statusCode,omitempty"`
}

func NewWebScraper() Tool {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"url": map[string]any{
				"type":        "string",
				"description": "The URL to scrape.",
			},
			"extract": map[string]any{
				"type":        "string",
				"enum":        []string{"text", "links", "headings", "images", "meta", "all"},
				"description": "What to extract: text, links, headings, images, meta, all. Defaults to all.",
			},
		},
		"required": []string{"url"},
	}

	return NewFuncTool(
		"web_scraper",
		"Scrape and extract content from web pages: text, links, headings, images, metadata.",
		schema,
		func(ctx context.Context, args json.RawMessage) (any, error) {
			var in webScraperArgs
			if err := json.Unmarshal(args, &in); err != nil {
				return nil, fmt.Errorf("invalid web_scraper args: %w", err)
			}
			if in.URL == "" {
				return nil, fmt.Errorf("url is required")
			}

			extract := in.Extract
			if extract == "" {
				extract = "all"
			}

			return scrapeURL(ctx, in.URL, extract)
		},
	)
}

func scrapeURL(ctx context.Context, url, extract string) (*ScrapedContent, error) {
	client := &http.Client{Timeout: 30 * time.Second}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return &ScrapedContent{URL: url, Error: err.Error()}, nil
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; AI-Agent-Framework/1.0)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")

	resp, err := client.Do(req)
	if err != nil {
		return &ScrapedContent{URL: url, Error: err.Error()}, nil
	}
	defer resp.Body.Close()

	content := &ScrapedContent{
		URL:        url,
		StatusCode: resp.StatusCode,
		Meta:       make(map[string]string),
	}

	if resp.StatusCode != 200 {
		content.Error = fmt.Sprintf("HTTP %d", resp.StatusCode)
		return content, nil
	}

	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 5*1024*1024))
	if err != nil {
		content.Error = err.Error()
		return content, nil
	}

	doc, err := html.Parse(strings.NewReader(string(bodyBytes)))
	if err != nil {
		content.Error = err.Error()
		return content, nil
	}

	content.Title = extractTitle(doc)

	switch extract {
	case "text":
		content.Text = extractText(doc)
	case "links":
		content.Links = extractLinks(doc, url)
	case "headings":
		content.Headings = extractHeadings(doc)
	case "images":
		content.Images = extractImages(doc, url)
	case "meta":
		content.Meta = extractMeta(doc)
		content.Description = content.Meta["description"]
	case "all":
		content.Text = extractText(doc)
		content.Links = extractLinks(doc, url)
		content.Headings = extractHeadings(doc)
		content.Images = extractImages(doc, url)
		content.Meta = extractMeta(doc)
		content.Description = content.Meta["description"]
	}

	if len(content.Text) > 50000 {
		content.Text = content.Text[:50000] + "... (truncated)"
	}

	return content, nil
}

func extractTitle(n *html.Node) string {
	if n.Type == html.ElementNode && n.Data == "title" {
		if n.FirstChild != nil {
			return strings.TrimSpace(n.FirstChild.Data)
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if title := extractTitle(c); title != "" {
			return title
		}
	}
	return ""
}

func extractText(n *html.Node) string {
	var sb strings.Builder
	extractTextRecursive(n, &sb, 10)
	text := sb.String()
	text = regexp.MustCompile(`\s+`).ReplaceAllString(text, " ")
	return strings.TrimSpace(text)
}

func extractTextRecursive(n *html.Node, sb *strings.Builder, depth int) {
	if depth <= 0 {
		return
	}

	if n.Type == html.ElementNode {
		switch n.Data {
		case "script", "style", "noscript", "iframe", "svg", "nav", "footer", "header":
			return
		}
	}

	if n.Type == html.TextNode {
		text := strings.TrimSpace(n.Data)
		if text != "" {
			sb.WriteString(text)
			sb.WriteString(" ")
		}
	}

	for c := n.FirstChild; c != nil; c = c.NextSibling {
		extractTextRecursive(c, sb, depth-1)
	}
}

func extractLinks(n *html.Node, baseURL string) []string {
	var links []string
	seen := make(map[string]bool)

	var extract func(*html.Node)
	extract = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "a" {
			for _, attr := range n.Attr {
				if attr.Key == "href" {
					href := resolveURL(baseURL, attr.Val)
					if href != "" && !seen[href] {
						seen[href] = true
						links = append(links, href)
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			extract(c)
		}
	}
	extract(n)

	if len(links) > 100 {
		links = links[:100]
	}
	return links
}

func extractHeadings(n *html.Node) []string {
	var headings []string

	var extract func(*html.Node)
	extract = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "h1", "h2", "h3", "h4", "h5", "h6":
				text := extractNodeText(n)
				if text != "" {
					headings = append(headings, fmt.Sprintf("[%s] %s", n.Data, text))
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			extract(c)
		}
	}
	extract(n)

	return headings
}

func extractImages(n *html.Node, baseURL string) []string {
	var images []string
	seen := make(map[string]bool)

	var extract func(*html.Node)
	extract = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "img" {
			for _, attr := range n.Attr {
				if attr.Key == "src" {
					src := resolveURL(baseURL, attr.Val)
					if src != "" && !seen[src] {
						seen[src] = true
						images = append(images, src)
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			extract(c)
		}
	}
	extract(n)

	if len(images) > 50 {
		images = images[:50]
	}
	return images
}

func extractMeta(n *html.Node) map[string]string {
	meta := make(map[string]string)

	var extract func(*html.Node)
	extract = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "meta" {
			var name, content string
			for _, attr := range n.Attr {
				switch attr.Key {
				case "name", "property":
					name = attr.Val
				case "content":
					content = attr.Val
				}
			}
			if name != "" && content != "" {
				meta[name] = content
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			extract(c)
		}
	}
	extract(n)

	return meta
}

func extractNodeText(n *html.Node) string {
	var sb strings.Builder
	var extract func(*html.Node)
	extract = func(n *html.Node) {
		if n.Type == html.TextNode {
			sb.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			extract(c)
		}
	}
	extract(n)
	return strings.TrimSpace(sb.String())
}

func resolveURL(base, href string) string {
	href = strings.TrimSpace(href)
	if href == "" || strings.HasPrefix(href, "#") || strings.HasPrefix(href, "javascript:") {
		return ""
	}
	if strings.HasPrefix(href, "http://") || strings.HasPrefix(href, "https://") {
		return href
	}
	if strings.HasPrefix(href, "//") {
		return "https:" + href
	}
	if strings.HasPrefix(href, "/") {
		parts := strings.SplitN(base, "/", 4)
		if len(parts) >= 3 {
			return parts[0] + "//" + parts[2] + href
		}
	}
	return ""
}
