package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/PipeOpsHQ/agent-sdk-go/delivery"
	cronpkg "github.com/PipeOpsHQ/agent-sdk-go/runtime/cron"
)

type cronManagerArgs struct {
	Operation    string           `json:"operation"`
	Name         string           `json:"name,omitempty"`
	CronExpr     string           `json:"cronExpr,omitempty"`
	Input        string           `json:"input,omitempty"`
	Workflow     string           `json:"workflow,omitempty"`
	Tools        []string         `json:"tools,omitempty"`
	SystemPrompt string           `json:"systemPrompt,omitempty"`
	ReplyTo      *delivery.Target `json:"replyTo,omitempty"`
	Enabled      *bool            `json:"enabled,omitempty"`
}

// NewCronManager creates a tool that lets agents manage cron-scheduled jobs.
// The scheduler must be injected at creation time.
func NewCronManager(scheduler *cronpkg.Scheduler) Tool {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"operation": map[string]any{
				"type":        "string",
				"enum":        []string{"list", "get", "add", "remove", "trigger", "enable", "disable"},
				"description": "Cron job operation to perform.",
			},
			"name": map[string]any{
				"type":        "string",
				"description": "Job name (required for get/add/remove/trigger/enable/disable).",
			},
			"cronExpr": map[string]any{
				"type":        "string",
				"description": "Cron expression for scheduling (required for add). Examples: '*/5 * * * *' (every 5 min), '0 9 * * *' (daily 9am).",
			},
			"input": map[string]any{
				"type":        "string",
				"description": "Input prompt for the agent when the job runs.",
			},
			"workflow": map[string]any{
				"type":        "string",
				"description": "Workflow to use for the job (default: basic).",
			},
			"tools": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Tools or bundles to enable for the job (e.g., ['@default']).",
			},
			"systemPrompt": map[string]any{
				"type":        "string",
				"description": "System prompt for the scheduled agent run.",
			},
			"replyTo": map[string]any{
				"type":        "object",
				"description": "Optional response routing target (for Slack, Telegram, DevUI, API/webhook, etc.).",
				"properties": map[string]any{
					"channel":     map[string]any{"type": "string", "description": "Channel type: devui, api, slack, telegram, webhook."},
					"destination": map[string]any{"type": "string", "description": "Channel destination id (e.g., Slack channel id, Telegram chat id, webhook name)."},
					"threadId":    map[string]any{"type": "string", "description": "Optional thread or conversation id."},
					"userId":      map[string]any{"type": "string", "description": "Optional end-user id for direct replies."},
					"metadata":    map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}},
				},
			},
		},
		"required": []string{"operation"},
	}

	return NewFuncTool(
		"cron_manager",
		"Manage cron-scheduled agent jobs: list, add, remove, trigger, enable, disable recurring tasks.",
		schema,
		func(ctx context.Context, args json.RawMessage) (any, error) {
			if scheduler == nil {
				return map[string]any{"error": "scheduler not available"}, nil
			}

			var in cronManagerArgs
			if err := json.Unmarshal(args, &in); err != nil {
				return nil, fmt.Errorf("invalid cron_manager args: %w", err)
			}

			switch in.Operation {
			case "list":
				jobs := scheduler.List()
				return map[string]any{"jobs": jobs, "count": len(jobs)}, nil

			case "get":
				if in.Name == "" {
					return nil, fmt.Errorf("name is required")
				}
				job, ok := scheduler.Get(in.Name)
				if !ok {
					return map[string]any{"error": fmt.Sprintf("job %q not found", in.Name)}, nil
				}
				return job, nil

			case "add":
				if in.Name == "" || in.CronExpr == "" || in.Input == "" {
					return nil, fmt.Errorf("name, cronExpr, and input are required for add")
				}
				replyTo := delivery.Normalize(in.ReplyTo)
				if replyTo == nil {
					replyTo = delivery.FromContext(ctx)
				}
				cfg := cronpkg.JobConfig{
					Workflow:     in.Workflow,
					Tools:        in.Tools,
					SystemPrompt: in.SystemPrompt,
					Input:        in.Input,
					ReplyTo:      replyTo,
				}
				if err := scheduler.Add(in.Name, in.CronExpr, cfg); err != nil {
					return map[string]any{"error": err.Error()}, nil
				}
				return map[string]any{"success": true, "message": fmt.Sprintf("job %q scheduled with %q", in.Name, in.CronExpr)}, nil

			case "remove":
				if in.Name == "" {
					return nil, fmt.Errorf("name is required")
				}
				if err := scheduler.Remove(in.Name); err != nil {
					return map[string]any{"error": err.Error()}, nil
				}
				return map[string]any{"success": true, "message": fmt.Sprintf("job %q removed", in.Name)}, nil

			case "trigger":
				if in.Name == "" {
					return nil, fmt.Errorf("name is required")
				}
				output, err := scheduler.Trigger(in.Name)
				if err != nil {
					return map[string]any{"error": err.Error()}, nil
				}
				return map[string]any{"success": true, "output": output}, nil

			case "enable":
				if in.Name == "" {
					return nil, fmt.Errorf("name is required")
				}
				if err := scheduler.SetEnabled(in.Name, true); err != nil {
					return map[string]any{"error": err.Error()}, nil
				}
				return map[string]any{"success": true, "message": fmt.Sprintf("job %q enabled", in.Name)}, nil

			case "disable":
				if in.Name == "" {
					return nil, fmt.Errorf("name is required")
				}
				if err := scheduler.SetEnabled(in.Name, false); err != nil {
					return map[string]any{"error": err.Error()}, nil
				}
				return map[string]any{"success": true, "message": fmt.Sprintf("job %q disabled", in.Name)}, nil

			default:
				return nil, fmt.Errorf("unsupported operation %q", in.Operation)
			}
		},
	)
}
