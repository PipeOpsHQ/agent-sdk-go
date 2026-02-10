package tools

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"time"
)

type curlArgs struct {
	URL            string            `json:"url"`
	Method         string            `json:"method,omitempty"`
	Headers        map[string]string `json:"headers,omitempty"`
	Data           string            `json:"data,omitempty"`
	BasicAuth      string            `json:"basicAuth,omitempty"`      // user:password
	BearerToken    string            `json:"bearerToken,omitempty"`    // Authorization: Bearer <token>
	ContentType    string            `json:"contentType,omitempty"`    // shorthand for Content-Type header
	FollowRedirect bool              `json:"followRedirect,omitempty"` // -L
	MaxRedirects   int               `json:"maxRedirects,omitempty"`
	Timeout        int               `json:"timeout,omitempty"`
	Insecure       bool              `json:"insecure,omitempty"`       // -k
	Verbose        bool              `json:"verbose,omitempty"`        // -v (include timing, TLS info)
	IncludeHeaders bool              `json:"includeHeaders,omitempty"` // -i
	UserAgent      string            `json:"userAgent,omitempty"`
	Cookies        map[string]string `json:"cookies,omitempty"`
	MaxBodySize    int               `json:"maxBodySize,omitempty"` // KB, default 1024
}

type curlResponse struct {
	StatusCode    int               `json:"statusCode"`
	Status        string            `json:"status"`
	Headers       map[string]string `json:"headers,omitempty"`
	Body          string            `json:"body"`
	ContentLength int64             `json:"contentLength"`
	Duration      string            `json:"duration"`
	DNSLookup     string            `json:"dnsLookup,omitempty"`
	TLSHandshake  string            `json:"tlsHandshake,omitempty"`
	ConnectTime   string            `json:"connectTime,omitempty"`
	RedirectCount int               `json:"redirectCount,omitempty"`
	FinalURL      string            `json:"finalUrl,omitempty"`
	Error         string            `json:"error,omitempty"`
}

func NewCurl() Tool {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"url": map[string]any{
				"type":        "string",
				"description": "Target URL.",
			},
			"method": map[string]any{
				"type":        "string",
				"enum":        []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"},
				"description": "HTTP method. Defaults to GET (POST when data is provided).",
			},
			"headers": map[string]any{
				"type":        "object",
				"description": "Custom HTTP headers as key-value pairs.",
			},
			"data": map[string]any{
				"type":        "string",
				"description": "Request body data (-d flag equivalent).",
			},
			"basicAuth": map[string]any{
				"type":        "string",
				"description": "Basic auth credentials as 'user:password'.",
			},
			"bearerToken": map[string]any{
				"type":        "string",
				"description": "Bearer token for Authorization header.",
			},
			"contentType": map[string]any{
				"type":        "string",
				"description": "Content-Type shorthand (e.g. 'json', 'form', 'xml', or full MIME type).",
			},
			"followRedirect": map[string]any{
				"type":        "boolean",
				"description": "Follow HTTP redirects (-L). Defaults to true.",
			},
			"maxRedirects": map[string]any{
				"type":        "integer",
				"description": "Maximum redirects to follow. Defaults to 10.",
				"minimum":     0,
				"maximum":     30,
			},
			"timeout": map[string]any{
				"type":        "integer",
				"description": "Request timeout in seconds. Defaults to 30.",
				"minimum":     1,
				"maximum":     300,
			},
			"insecure": map[string]any{
				"type":        "boolean",
				"description": "Skip TLS certificate verification (-k).",
			},
			"verbose": map[string]any{
				"type":        "boolean",
				"description": "Include timing details (DNS, TLS, connect). Like curl -v.",
			},
			"includeHeaders": map[string]any{
				"type":        "boolean",
				"description": "Include response headers in output (-i). Defaults to true.",
			},
			"userAgent": map[string]any{
				"type":        "string",
				"description": "Custom User-Agent string.",
			},
			"cookies": map[string]any{
				"type":        "object",
				"description": "Cookies to send as key-value pairs.",
			},
			"maxBodySize": map[string]any{
				"type":        "integer",
				"description": "Maximum response body size in KB. Defaults to 1024 (1MB).",
			},
		},
		"required": []string{"url"},
	}

	return NewFuncTool(
		"curl",
		"curl-style HTTP client with auth, cookies, TLS options, redirect following, and verbose timing. More featured than http_client.",
		schema,
		func(ctx context.Context, args json.RawMessage) (any, error) {
			var in curlArgs
			if err := json.Unmarshal(args, &in); err != nil {
				return nil, fmt.Errorf("invalid curl args: %w", err)
			}
			if in.URL == "" {
				return nil, fmt.Errorf("url is required")
			}
			return executeCurl(ctx, in)
		},
	)
}

func executeCurl(ctx context.Context, in curlArgs) (*curlResponse, error) {
	// Defaults
	method := strings.ToUpper(in.Method)
	if method == "" {
		if in.Data != "" {
			method = "POST"
		} else {
			method = "GET"
		}
	}

	timeout := in.Timeout
	if timeout <= 0 {
		timeout = 30
	}
	maxRedirects := in.MaxRedirects
	if maxRedirects <= 0 {
		maxRedirects = 10
	}
	maxBodyKB := in.MaxBodySize
	if maxBodyKB <= 0 {
		maxBodyKB = 1024
	}

	// Timing
	var dnsStart, dnsEnd, connStart, connEnd, tlsStart, tlsEnd time.Time

	trace := &net.Dialer{
		Timeout: time.Duration(timeout) * time.Second,
	}

	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			dnsStart = time.Now()
			conn, err := trace.DialContext(ctx, network, addr)
			dnsEnd = time.Now()
			connStart = dnsEnd
			if err == nil {
				connEnd = time.Now()
			}
			return conn, err
		},
	}

	if in.Insecure {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec
	}

	transport.TLSHandshakeTimeout = 10 * time.Second

	// Wrap transport to capture TLS timing
	if in.Verbose {
		origTLSConfig := transport.TLSClientConfig
		if origTLSConfig == nil {
			origTLSConfig = &tls.Config{}
		}
		origTLSConfig.InsecureSkipVerify = in.Insecure
		transport.TLSClientConfig = origTLSConfig
	}

	jar, _ := cookiejar.New(nil)
	redirectCount := 0

	client := &http.Client{
		Transport: transport,
		Jar:       jar,
		Timeout:   time.Duration(timeout) * time.Second,
	}

	followRedirect := in.FollowRedirect || in.Method == "" // default true
	if !followRedirect {
		client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		}
	} else {
		client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			redirectCount = len(via)
			if len(via) >= maxRedirects {
				return fmt.Errorf("stopped after %d redirects", maxRedirects)
			}
			return nil
		}
	}

	var bodyReader io.Reader
	if in.Data != "" {
		bodyReader = bytes.NewBufferString(in.Data)
	}

	req, err := http.NewRequestWithContext(ctx, method, in.URL, bodyReader)
	if err != nil {
		return &curlResponse{Error: err.Error()}, nil
	}

	// User agent
	ua := in.UserAgent
	if ua == "" {
		ua = "AI-Agent-Framework/1.0 (curl-tool)"
	}
	req.Header.Set("User-Agent", ua)

	// Content type
	if in.ContentType != "" {
		ct := in.ContentType
		switch strings.ToLower(ct) {
		case "json":
			ct = "application/json"
		case "form":
			ct = "application/x-www-form-urlencoded"
		case "xml":
			ct = "application/xml"
		case "text":
			ct = "text/plain"
		}
		req.Header.Set("Content-Type", ct)
	}

	// Custom headers
	for k, v := range in.Headers {
		req.Header.Set(k, v)
	}

	// Auth
	if in.BasicAuth != "" {
		parts := strings.SplitN(in.BasicAuth, ":", 2)
		if len(parts) == 2 {
			req.SetBasicAuth(parts[0], parts[1])
		}
	}
	if in.BearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+in.BearerToken)
	}

	// Cookies
	for name, val := range in.Cookies {
		req.AddCookie(&http.Cookie{Name: name, Value: val})
	}

	tlsStart = time.Now()
	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return &curlResponse{Error: err.Error(), Duration: time.Since(start).String()}, nil
	}
	tlsEnd = time.Now()
	defer resp.Body.Close()

	maxBytes := int64(maxBodyKB) * 1024
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes))
	if err != nil {
		return &curlResponse{
			StatusCode: resp.StatusCode,
			Status:     resp.Status,
			Error:      err.Error(),
			Duration:   time.Since(start).String(),
		}, nil
	}

	result := &curlResponse{
		StatusCode:    resp.StatusCode,
		Status:        resp.Status,
		Body:          string(body),
		ContentLength: resp.ContentLength,
		Duration:      time.Since(start).String(),
		RedirectCount: redirectCount,
		FinalURL:      resp.Request.URL.String(),
	}

	if in.IncludeHeaders || in.Verbose {
		hdrs := make(map[string]string)
		for k, v := range resp.Header {
			if len(v) > 0 {
				hdrs[k] = strings.Join(v, ", ")
			}
		}
		result.Headers = hdrs
	}

	if in.Verbose {
		if !dnsStart.IsZero() && !dnsEnd.IsZero() {
			result.DNSLookup = dnsEnd.Sub(dnsStart).String()
		}
		if !connStart.IsZero() && !connEnd.IsZero() {
			result.ConnectTime = connEnd.Sub(connStart).String()
		}
		if !tlsStart.IsZero() && !tlsEnd.IsZero() && strings.HasPrefix(in.URL, "https") {
			result.TLSHandshake = tlsEnd.Sub(tlsStart).String()
		}
	}

	return result, nil
}
