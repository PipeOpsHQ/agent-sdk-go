// SecOps Example Agent
//
// This example demonstrates a SecOps agent that can analyze Trivy vulnerability
// reports and process/analyze application logs.
//
// Usage:
//   go run . <trivy-json-file>
//   go run . <log-file>
//   cat logs.txt | go run .
//   go run . ui                    # Launch DevUI at http://127.0.0.1:8000
//   go run . ui --ui-addr=:9090    # Launch DevUI on custom address
//
// Environment Variables:
//   OPENAI_API_KEY or ANTHROPIC_API_KEY - LLM provider credentials
//   STATE_BACKEND=sqlite (default) or redis
//   STATE_SQLITE_PATH=./.ai-agent/state.db

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"regexp"
	"strings"
	"time"

	agentfw "github.com/PipeOpsHQ/agent-sdk-go/framework/agent"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/devui"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/flow"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/graph"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/llm"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/observe"
	observesqlite "github.com/PipeOpsHQ/agent-sdk-go/framework/observe/store/sqlite"
	providerfactory "github.com/PipeOpsHQ/agent-sdk-go/framework/providers/factory"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/state"
	statefactory "github.com/PipeOpsHQ/agent-sdk-go/framework/state/factory"
	fwtools "github.com/PipeOpsHQ/agent-sdk-go/framework/tools"
)

const secOpsSystemPrompt = `You are a senior SecOps analyst.

Analyze Trivy findings and redacted logs.
Return compact, actionable output.
For logs, keep to max 3 key issues and max 3 fixes.`

// ─────────────────────────────────────────────────────────────────────────────
// Data Types
// ─────────────────────────────────────────────────────────────────────────────

type Vulnerability struct {
	VulnerabilityID  string `json:"vulnerabilityId"`
	PkgName          string `json:"pkgName"`
	InstalledVersion string `json:"installedVersion,omitempty"`
	FixedVersion     string `json:"fixedVersion,omitempty"`
	Severity         string `json:"severity"`
	Title            string `json:"title,omitempty"`
}

type CategorizedVulnerabilities struct {
	ArtifactName string          `json:"artifactName"`
	Critical     []Vulnerability `json:"critical"`
	High         []Vulnerability `json:"high"`
	MediumCount  int             `json:"mediumCount"`
	LowCount     int             `json:"lowCount"`
	TotalCount   int             `json:"totalCount"`
}

type ClassifiedLogs struct {
	Errors   []string `json:"errors"`
	Warnings []string `json:"warnings"`
	Info     []string `json:"info"`
}

type trivyReport struct {
	ArtifactName string        `json:"ArtifactName"`
	Results      []trivyResult `json:"Results"`
}

type trivyResult struct {
	Vulnerabilities []trivyVulnerability `json:"Vulnerabilities"`
}

type trivyVulnerability struct {
	VulnerabilityID  string `json:"VulnerabilityID"`
	PkgName          string `json:"PkgName"`
	InstalledVersion string `json:"InstalledVersion"`
	FixedVersion     string `json:"FixedVersion"`
	Severity         string `json:"Severity"`
	Title            string `json:"Title"`
}

// ─────────────────────────────────────────────────────────────────────────────
// Graph Node Keys
// ─────────────────────────────────────────────────────────────────────────────

const (
	RouteKey   = "route"
	RouteTrivy = "trivy"
	RouteLogs  = "logs"

	KeyCategorized  = "categorized"
	KeyRedactedLogs = "redactedLogs"
	KeyClassified   = "classifiedLogs"
	KeyPromptTrivy  = "trivyPrompt"
	KeyPromptLogs   = "logsPrompt"
	KeyFinalOutput  = "output"
)

// ─────────────────────────────────────────────────────────────────────────────
// Main Entry Point
// ─────────────────────────────────────────────────────────────────────────────

func main() {
	ctx := context.Background()

	if len(os.Args) > 1 && strings.ToLower(os.Args[1]) == "ui" {
		runDevUI()
		return
	}

	input, err := readInput(os.Args[1:])
	if err != nil {
		log.Fatal(err)
	}

	provider, store, observer, closeFn := buildDeps(ctx)
	defer closeFn()

	a, err := buildAgent(provider, store, observer)
	if err != nil {
		log.Fatalf("agent create failed: %v", err)
	}

	exec, err := newSecOpsExecutor(a, store)
	if err != nil {
		log.Fatalf("secops executor create failed: %v", err)
	}
	exec.SetObserver(observer)

	result, err := exec.Run(ctx, input)
	if err != nil {
		log.Fatalf("secops run failed: %v", err)
	}

	fmt.Printf("run_id=%s session_id=%s\n\n%s\n", result.RunID, result.SessionID, strings.TrimSpace(result.Output))
}

func runDevUI() {
	flow.MustRegister(&flow.Definition{
		Name:        "secops-analyzer",
		Description: "SecOps agent that analyzes Trivy vulnerability reports and application logs, returning compact actionable findings.",
		Tools:       []string{"@default", "@security"},
		SystemPrompt: secOpsSystemPrompt,
		InputExample: "Scan payment-service for critical CVEs, summarize top 5 findings with remediation steps",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"input": map[string]any{
					"type":        "string",
					"description": "Trivy JSON report, raw log text, or natural language description of vulnerabilities to analyze.",
				},
				"scope": map[string]any{
					"type":        "string",
					"description": "Analysis scope — focus area for the scan.",
					"enum":        []string{"all", "critical-only", "fixable-only", "runtime"},
					"default":     "all",
				},
			},
			"required": []string{"input"},
		},
	})

	if err := devui.Start(context.Background(), devui.Options{
		Addr:        "127.0.0.1:8000",
		DefaultFlow: "secops-analyzer",
	}); err != nil {
		log.Fatal(err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Input Reading
// ─────────────────────────────────────────────────────────────────────────────

func readInput(args []string) (string, error) {
	if len(args) > 0 {
		path := strings.TrimSpace(args[0])
		if path != "" {
			b, err := os.ReadFile(path)
			if err != nil {
				return "", fmt.Errorf("failed to read %s: %w", path, err)
			}
			return string(b), nil
		}
	}
	stat, err := os.Stdin.Stat()
	if err == nil && (stat.Mode()&os.ModeCharDevice) == 0 {
		b, readErr := io.ReadAll(os.Stdin)
		if readErr != nil {
			return "", fmt.Errorf("failed to read stdin: %w", readErr)
		}
		return string(b), nil
	}
	return "", fmt.Errorf("usage: go run . <trivy-json-or-log-file> OR cat file | go run .")
}

// ─────────────────────────────────────────────────────────────────────────────
// Dependencies
// ─────────────────────────────────────────────────────────────────────────────

func buildDeps(ctx context.Context) (llm.Provider, state.Store, observe.Sink, func()) {
	provider, err := providerfactory.FromEnv(ctx)
	if err != nil {
		log.Fatal(err)
	}
	store, err := statefactory.FromEnv(ctx)
	if err != nil {
		log.Fatal(err)
	}
	observer, closeObserver := buildObserver()
	return provider, store, observer, func() {
		closeObserver()
		_ = store.Close()
	}
}

func buildAgent(provider llm.Provider, store state.Store, observer observe.Sink) (*agentfw.Agent, error) {
	selected, err := fwtools.BuildSelection([]string{"@default", "@security"})
	if err != nil {
		return nil, err
	}
	opts := []agentfw.Option{
		agentfw.WithSystemPrompt(secOpsSystemPrompt),
		agentfw.WithStore(store),
		agentfw.WithObserver(observer),
		agentfw.WithMaxIterations(4),
		agentfw.WithMaxOutputTokens(600),
		agentfw.WithRetryPolicy(agentfw.RetryPolicy{MaxAttempts: 2, BaseBackoff: 200 * time.Millisecond, MaxBackoff: 2 * time.Second}),
	}
	for _, t := range selected {
		opts = append(opts, agentfw.WithTool(t))
	}
	return agentfw.New(provider, opts...)
}

func buildObserver() (observe.Sink, func()) {
	traceStore, err := observesqlite.New("./.ai-agent/devui.db")
	if err != nil {
		return observe.NoopSink{}, func() {}
	}
	async := observe.NewAsyncSink(observe.SinkFunc(func(ctx context.Context, event observe.Event) error {
		return traceStore.SaveEvent(ctx, event)
	}), 256)
	return async, func() {
		async.Close()
		_ = traceStore.Close()
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// SecOps Graph Executor
// ─────────────────────────────────────────────────────────────────────────────

func newSecOpsExecutor(runner graph.AgentRunner, store state.Store) (*graph.Executor, error) {
	if runner == nil {
		return nil, fmt.Errorf("runner is required")
	}

	g := graph.New("secops")
	g.AddNode("route", detectInputRouteNode())

	g.AddNode("parse_trivy", parseTrivyNode())
	g.AddNode("build_trivy_prompt", buildTrivyPromptNode())
	g.AddNode("assistant_trivy", &graph.AgentNode{
		Runner: runner,
		Input: func(s *graph.State) (string, error) {
			s.EnsureData()
			if v, ok := s.Data[KeyPromptTrivy].(string); ok && strings.TrimSpace(v) != "" {
				return v, nil
			}
			return s.Input, nil
		},
		OutputKey: "trivyAgentOutput",
	})

	g.AddNode("redact_logs", redactLogsNode())
	g.AddNode("classify_logs", classifyLogsNode())
	g.AddNode("build_logs_prompt", buildLogsPromptNode())
	g.AddNode("assistant_logs", &graph.AgentNode{
		Runner: runner,
		Input: func(s *graph.State) (string, error) {
			s.EnsureData()
			if v, ok := s.Data[KeyPromptLogs].(string); ok && strings.TrimSpace(v) != "" {
				return v, nil
			}
			return s.Input, nil
		},
		OutputKey: "logsAgentOutput",
	})

	g.AddNode("finalize", finalizeNode())
	g.SetStart("route")

	g.AddEdge("route", "parse_trivy", graph.RouteEquals(RouteKey, RouteTrivy))
	g.AddEdge("route", "redact_logs", graph.RouteEquals(RouteKey, RouteLogs))

	g.AddEdge("parse_trivy", "build_trivy_prompt", nil)
	g.AddEdge("build_trivy_prompt", "assistant_trivy", nil)
	g.AddEdge("assistant_trivy", "finalize", nil)

	g.AddEdge("redact_logs", "classify_logs", nil)
	g.AddEdge("classify_logs", "build_logs_prompt", nil)
	g.AddEdge("build_logs_prompt", "assistant_logs", nil)
	g.AddEdge("assistant_logs", "finalize", nil)

	opts := []graph.ExecutorOption{graph.WithStore(store)}
	return graph.NewExecutor(g, opts...)
}

// ─────────────────────────────────────────────────────────────────────────────
// Graph Nodes
// ─────────────────────────────────────────────────────────────────────────────

func detectInputRouteNode() graph.Node {
	return graph.NewRouterNode(func(ctx context.Context, state *graph.State) (string, error) {
		_ = ctx
		trimmed := strings.TrimSpace(state.Input)
		if trimmed == "" {
			return RouteLogs, nil
		}

		var obj map[string]any
		if json.Unmarshal([]byte(trimmed), &obj) == nil {
			if _, hasResults := obj["Results"]; hasResults {
				return RouteTrivy, nil
			}
			if _, hasArtifact := obj["ArtifactName"]; hasArtifact {
				return RouteTrivy, nil
			}
		}
		return RouteLogs, nil
	})
}

func parseTrivyNode() graph.Node {
	return graph.NewToolNode(func(ctx context.Context, state *graph.State) error {
		_ = ctx
		categorized, err := parseTrivyReport(json.RawMessage(state.Input))
		if err != nil {
			return err
		}
		state.EnsureData()
		state.Data[KeyCategorized] = categorized
		return nil
	})
}

func redactLogsNode() graph.Node {
	return graph.NewToolNode(func(ctx context.Context, state *graph.State) error {
		_ = ctx
		redacted := redactSensitiveData(state.Input)
		state.EnsureData()
		state.Data[KeyRedactedLogs] = redacted
		return nil
	})
}

func classifyLogsNode() graph.Node {
	return graph.NewToolNode(func(ctx context.Context, state *graph.State) error {
		_ = ctx
		state.EnsureData()
		logText, _ := state.Data[KeyRedactedLogs].(string)
		classified := classifyLogEntries(logText)
		state.Data[KeyClassified] = classified
		return nil
	})
}

func buildTrivyPromptNode() graph.Node {
	return graph.NewToolNode(func(ctx context.Context, state *graph.State) error {
		_ = ctx
		state.EnsureData()

		categorized, err := decodeCategorized(state.Data[KeyCategorized])
		if err != nil {
			return err
		}

		prompt := fmt.Sprintf(`Analyze this Trivy result and return compact, high-signal findings.
Constraints:
- Maximum 8 bullets.
- Prioritize CRITICAL/HIGH first.
- Keep total under 140 words.
- Include immediate actions only.

Artifact: %s
Counts: critical=%d high=%d medium=%d low=%d total=%d`,
			categorized.ArtifactName,
			len(categorized.Critical),
			len(categorized.High),
			categorized.MediumCount,
			categorized.LowCount,
			categorized.TotalCount,
		)
		state.Data[KeyPromptTrivy] = prompt
		return nil
	})
}

func buildLogsPromptNode() graph.Node {
	return graph.NewToolNode(func(ctx context.Context, state *graph.State) error {
		_ = ctx
		state.EnsureData()

		classified, err := decodeClassified(state.Data[KeyClassified])
		if err != nil {
			return err
		}
		redactedLogs, _ := state.Data[KeyRedactedLogs].(string)
		if len(redactedLogs) > 1200 {
			redactedLogs = redactedLogs[:1200]
		}
		prompt := fmt.Sprintf(`Analyze these redacted logs and return a compact response.
Constraints:
- Max 3 issues.
- Max 3 fixes.
- Summary <= 80 words.
- Avoid fluff and repetition.

Observed counts: errors=%d warnings=%d info=%d

Logs snippet:
%s`,
			len(classified.Errors),
			len(classified.Warnings),
			len(classified.Info),
			redactedLogs,
		)
		state.Data[KeyPromptLogs] = prompt
		return nil
	})
}

func finalizeNode() graph.Node {
	return graph.NewToolNode(func(ctx context.Context, state *graph.State) error {
		_ = ctx
		state.EnsureData()
		if state.Output == "" {
			if route, _ := state.Data[RouteKey].(string); route == RouteTrivy {
				if fallback, ok := state.Data["trivyAgentOutput"].(string); ok {
					state.Output = strings.TrimSpace(fallback)
				}
			} else {
				if fallback, ok := state.Data["logsAgentOutput"].(string); ok {
					state.Output = strings.TrimSpace(fallback)
				}
			}
		}
		state.Data[KeyFinalOutput] = state.Output
		return nil
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// Business Logic
// ─────────────────────────────────────────────────────────────────────────────

var (
	sensitivePairPattern = regexp.MustCompile(`(?i)\b(api[_-]?key|token|secret|password|passwd|authorization)\b\s*([:=])\s*([^\s,;]+)`)
	bearerPattern        = regexp.MustCompile(`(?i)\bbearer\s+[a-z0-9\-._~+/]+=*`)
)

func parseTrivyReport(raw json.RawMessage) (CategorizedVulnerabilities, error) {
	rawBytes := bytes.TrimSpace(raw)
	if len(rawBytes) == 0 {
		return CategorizedVulnerabilities{}, fmt.Errorf("trivy report payload is required")
	}

	// Allow callers to pass either JSON object bytes or a JSON-encoded string.
	if len(rawBytes) > 0 && rawBytes[0] == '"' {
		var embedded string
		if err := json.Unmarshal(rawBytes, &embedded); err != nil {
			return CategorizedVulnerabilities{}, fmt.Errorf("decode trivy string payload: %w", err)
		}
		rawBytes = []byte(strings.TrimSpace(embedded))
	}

	var report trivyReport
	if err := json.Unmarshal(rawBytes, &report); err != nil {
		return CategorizedVulnerabilities{}, fmt.Errorf("decode trivy report: %w", err)
	}

	out := CategorizedVulnerabilities{
		ArtifactName: strings.TrimSpace(report.ArtifactName),
		Critical:     []Vulnerability{},
		High:         []Vulnerability{},
	}
	for _, result := range report.Results {
		for _, vuln := range result.Vulnerabilities {
			item := Vulnerability{
				VulnerabilityID:  strings.TrimSpace(vuln.VulnerabilityID),
				PkgName:          strings.TrimSpace(vuln.PkgName),
				InstalledVersion: strings.TrimSpace(vuln.InstalledVersion),
				FixedVersion:     strings.TrimSpace(vuln.FixedVersion),
				Severity:         strings.ToUpper(strings.TrimSpace(vuln.Severity)),
				Title:            strings.TrimSpace(vuln.Title),
			}
			out.TotalCount++
			switch item.Severity {
			case "CRITICAL":
				out.Critical = append(out.Critical, item)
			case "HIGH":
				out.High = append(out.High, item)
			case "MEDIUM":
				out.MediumCount++
			default:
				out.LowCount++
			}
		}
	}
	return out, nil
}

func redactSensitiveData(logs string) string {
	redacted := strings.TrimSpace(logs)
	if redacted == "" {
		return ""
	}

	redacted = sensitivePairPattern.ReplaceAllStringFunc(redacted, func(match string) string {
		parts := sensitivePairPattern.FindStringSubmatch(match)
		if len(parts) < 3 {
			return "[REDACTED]"
		}
		return fmt.Sprintf("%s%s [REDACTED]", parts[1], parts[2])
	})

	redacted = bearerPattern.ReplaceAllStringFunc(redacted, func(_ string) string {
		return "Bearer [REDACTED]"
	})

	return redacted
}

func classifyLogEntries(logs string) ClassifiedLogs {
	out := ClassifiedLogs{
		Errors:   []string{},
		Warnings: []string{},
		Info:     []string{},
	}

	for _, line := range strings.Split(strings.TrimSpace(logs), "\n") {
		entry := strings.TrimSpace(line)
		if entry == "" {
			continue
		}
		lower := strings.ToLower(entry)
		switch {
		case strings.Contains(lower, "panic"), strings.Contains(lower, "fatal"), strings.Contains(lower, "error"):
			out.Errors = append(out.Errors, entry)
		case strings.Contains(lower, "warn"):
			out.Warnings = append(out.Warnings, entry)
		default:
			out.Info = append(out.Info, entry)
		}
	}

	return out
}

func decodeCategorized(v any) (CategorizedVulnerabilities, error) {
	var out CategorizedVulnerabilities
	raw, err := json.Marshal(v)
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, err
	}
	return out, nil
}

func decodeClassified(v any) (ClassifiedLogs, error) {
	var out ClassifiedLogs
	raw, err := json.Marshal(v)
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, err
	}
	return out, nil
}
