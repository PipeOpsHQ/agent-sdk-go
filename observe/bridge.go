package observe

import (
	"fmt"
	"strings"

	"github.com/PipeOpsHQ/agent-sdk-go/types"
)

func FromRuntimeEvent(in types.Event) Event {
	e := Event{
		Timestamp: in.Timestamp,
		RunID:     in.RunID,
		SessionID: in.SessionID,
		Provider:  in.Provider,
		ToolName:  in.ToolName,
		Message:   in.Message,
		Error:     in.Error,
		Attributes: map[string]any{
			"eventType": string(in.Type),
		},
	}
	if in.Iteration > 0 {
		e.Attributes["iteration"] = in.Iteration
	}
	if in.ToolCallID != "" {
		e.Attributes["toolCallId"] = in.ToolCallID
	}

	eventType := string(in.Type)
	switch {
	case strings.Contains(eventType, "before_generate"), strings.Contains(eventType, "after_generate"):
		e.Kind = KindProvider
	case strings.Contains(eventType, "before_tool"), strings.Contains(eventType, "after_tool"):
		e.Kind = KindTool
	case strings.Contains(eventType, "graph.node"):
		e.Kind = KindGraph
	case strings.HasPrefix(eventType, "run."):
		e.Kind = KindRun
	default:
		parts := strings.SplitN(eventType, ".", 2)
		if len(parts) > 0 && parts[0] == "graph" {
			e.Kind = KindGraph
		} else {
			e.Kind = KindCustom
		}
	}
	if strings.Contains(string(in.Type), "before") || strings.Contains(string(in.Type), "started") {
		e.Status = StatusStarted
	}
	if strings.Contains(string(in.Type), "after") || strings.Contains(string(in.Type), "completed") {
		e.Status = StatusCompleted
	}
	if strings.Contains(string(in.Type), "failed") {
		e.Status = StatusFailed
	}
	if e.Status == "" {
		e.Status = StatusCompleted
	}

	e.SpanID = spanIDForRuntimeEvent(in)
	e.ParentSpanID = parentSpanIDForRuntimeEvent(in)
	e.Normalize()
	return e
}

func spanIDForRuntimeEvent(in types.Event) string {
	if in.RunID == "" {
		return ""
	}
	if in.ToolCallID != "" {
		return fmt.Sprintf("%s:tool:%d:%s", in.RunID, in.Iteration, in.ToolCallID)
	}
	if in.ToolName != "" {
		return fmt.Sprintf("%s:node:%s", in.RunID, in.ToolName)
	}
	if in.Iteration > 0 {
		return fmt.Sprintf("%s:gen:%d", in.RunID, in.Iteration)
	}
	return in.RunID
}

func parentSpanIDForRuntimeEvent(in types.Event) string {
	if in.RunID == "" {
		return ""
	}
	if in.ToolCallID != "" {
		return fmt.Sprintf("%s:gen:%d", in.RunID, in.Iteration)
	}
	if in.ToolName != "" {
		return in.RunID
	}
	if in.Iteration > 0 {
		return in.RunID
	}
	return ""
}
