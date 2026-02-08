package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type httpClientArgs struct {
	URL             string            `json:"url"`
	Method          string            `json:"method,omitempty"`
	Headers         map[string]string `json:"headers,omitempty"`
	Body            string            `json:"body,omitempty"`
	Timeout         int               `json:"timeout,omitempty"`
	FollowRedirects bool              `json:"followRedirects,omitempty"`
}

// HTTPResponse represents the response from an HTTP request.
type HTTPResponse struct {
	StatusCode    int               `json:"statusCode"`
	Status        string            `json:"status"`
	Headers       map[string]string `json:"headers"`
	Body          string            `json:"body"`
	ContentLength int64             `json:"contentLength"`
	Duration      string            `json:"duration"`
	Error         string            `json:"error,omitempty"`
}

func NewHTTPClient() Tool {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"url": map[string]any{
				"type":        "string",
				"description": "The URL to make the request to.",
			},
			"method": map[string]any{
				"type":        "string",
				"enum":        []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"},
				"description": "HTTP method. Defaults to GET.",
			},
			"headers": map[string]any{
				"type":        "object",
				"description": "HTTP headers as key-value pairs.",
			},
			"body": map[string]any{
				"type":        "string",
				"description": "Request body (for POST, PUT, PATCH).",
			},
			"timeout": map[string]any{
				"type":        "integer",
				"description": "Request timeout in seconds. Defaults to 30.",
				"minimum":     1,
				"maximum":     300,
			},
			"followRedirects": map[string]any{
				"type":        "boolean",
				"description": "Whether to follow redirects. Defaults to true.",
			},
		},
		"required": []string{"url"},
	}

	return NewFuncTool(
		"http_client",
		"Make HTTP requests to APIs and web services. Supports GET, POST, PUT, PATCH, DELETE with custom headers and body.",
		schema,
		func(ctx context.Context, args json.RawMessage) (any, error) {
			var in httpClientArgs
			if err := json.Unmarshal(args, &in); err != nil {
				return nil, fmt.Errorf("invalid http_client args: %w", err)
			}
			if in.URL == "" {
				return nil, fmt.Errorf("url is required")
			}

			method := strings.ToUpper(in.Method)
			if method == "" {
				method = "GET"
			}

			timeout := in.Timeout
			if timeout <= 0 {
				timeout = 30
			}

			return makeHTTPRequest(ctx, method, in.URL, in.Headers, in.Body, timeout, in.FollowRedirects)
		},
	)
}

func makeHTTPRequest(ctx context.Context, method, url string, headers map[string]string, body string, timeout int, followRedirects bool) (*HTTPResponse, error) {
	start := time.Now()

	var bodyReader io.Reader
	if body != "" {
		bodyReader = bytes.NewBufferString(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return &HTTPResponse{Error: fmt.Sprintf("failed to create request: %v", err)}, nil
	}

	req.Header.Set("User-Agent", "AI-Agent-Framework/1.0")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	client := &http.Client{Timeout: time.Duration(timeout) * time.Second}
	if !followRedirects {
		client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		return &HTTPResponse{Error: fmt.Sprintf("request failed: %v", err), Duration: time.Since(start).String()}, nil
	}
	defer resp.Body.Close()

	maxSize := int64(10 * 1024 * 1024) // 10MB limit
	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, maxSize))
	if err != nil {
		return &HTTPResponse{StatusCode: resp.StatusCode, Status: resp.Status, Error: err.Error(), Duration: time.Since(start).String()}, nil
	}

	respHeaders := make(map[string]string)
	for k, v := range resp.Header {
		if len(v) > 0 {
			respHeaders[k] = v[0]
		}
	}

	return &HTTPResponse{
		StatusCode:    resp.StatusCode,
		Status:        resp.Status,
		Headers:       respHeaders,
		Body:          string(bodyBytes),
		ContentLength: resp.ContentLength,
		Duration:      time.Since(start).String(),
	}, nil
}
