package secops

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	appsecops "github.com/PipeOpsHQ/agent-sdk-go/app/secops"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/graph"
)

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

func DetectInputRouteNode() graph.Node {
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

func ParseTrivyNode() graph.Node {
	return graph.NewToolNode(func(ctx context.Context, state *graph.State) error {
		_ = ctx
		categorized, err := appsecops.ParseTrivyReport(appsecops.ParseTrivyReportInput{ReportJSON: json.RawMessage(state.Input)})
		if err != nil {
			return err
		}
		state.EnsureData()
		state.Data[KeyCategorized] = categorized
		return nil
	})
}

func RedactLogsNode() graph.Node {
	return graph.NewToolNode(func(ctx context.Context, state *graph.State) error {
		_ = ctx
		redacted := appsecops.RedactSensitiveData(state.Input)
		state.EnsureData()
		state.Data[KeyRedactedLogs] = redacted.RedactedLogs
		return nil
	})
}

func ClassifyLogsNode() graph.Node {
	return graph.NewToolNode(func(ctx context.Context, state *graph.State) error {
		_ = ctx
		state.EnsureData()
		logText, _ := state.Data[KeyRedactedLogs].(string)
		classified := appsecops.ClassifyLogEntries(logText)
		state.Data[KeyClassified] = classified
		return nil
	})
}

func BuildTrivyPromptNode() graph.Node {
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

func BuildLogsPromptNode() graph.Node {
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

func FinalizeNode() graph.Node {
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

func decodeCategorized(v any) (appsecops.CategorizedVulnerabilities, error) {
	var out appsecops.CategorizedVulnerabilities
	raw, err := json.Marshal(v)
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, err
	}
	return out, nil
}

func decodeClassified(v any) (appsecops.ClassifiedLogs, error) {
	var out appsecops.ClassifiedLogs
	raw, err := json.Marshal(v)
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, err
	}
	return out, nil
}
