package cli

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	agentfw "github.com/PipeOpsHQ/agent-sdk-go/framework/agent"
	devuiapi "github.com/PipeOpsHQ/agent-sdk-go/framework/devui/api"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/flow"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/guardrail"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/prompt"
	providerfactory "github.com/PipeOpsHQ/agent-sdk-go/framework/providers/factory"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/skill"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/state"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/types"
)

func (r *localPlaygroundRunner) Run(ctx context.Context, req devuiapi.PlaygroundRequest) (devuiapi.PlaygroundResponse, error) {
	provider, err := providerfactory.FromEnv(ctx)
	if err != nil {
		return devuiapi.PlaygroundResponse{}, fmt.Errorf("provider setup failed: %w", err)
	}

	explicitSystemPrompt := strings.TrimSpace(req.SystemPrompt)

	var flowSkills []string
	if name := strings.TrimSpace(req.Flow); name != "" {
		if f, ok := flow.Get(name); ok {
			if strings.TrimSpace(req.Workflow) == "" {
				req.Workflow = f.Workflow
			}
			if len(req.Tools) == 0 {
				req.Tools = f.Tools
			}
			if strings.TrimSpace(req.SystemPrompt) == "" {
				req.SystemPrompt = f.SystemPrompt
			}
			flowSkills = f.Skills
		}
	}

	allSkills := make(map[string]bool)
	for _, s := range flowSkills {
		allSkills[s] = true
	}
	for _, s := range req.Skills {
		allSkills[s] = true
	}
	systemPrompt := strings.TrimSpace(req.SystemPrompt)
	for skillName := range allSkills {
		if s, ok := skill.Get(skillName); ok {
			if s.Instructions != "" {
				systemPrompt += "\n\n## Skill: " + s.Name + "\n" + s.Instructions
			}
			if len(s.AllowedTools) > 0 {
				req.Tools = append(req.Tools, s.AllowedTools...)
			}
		}
	}
	if explicitSystemPrompt != "" {
		req.SystemPrompt = explicitSystemPrompt
	} else if promptRef := strings.TrimSpace(req.PromptRef); promptRef != "" {
		spec, ok := prompt.Resolve(promptRef)
		if !ok {
			return devuiapi.PlaygroundResponse{}, fmt.Errorf("prompt %q not found", promptRef)
		}
		rendered, renderErr := prompt.Render(spec.System, promptInputVars(req.PromptInput))
		if renderErr != nil {
			return devuiapi.PlaygroundResponse{}, renderErr
		}
		req.SystemPrompt = rendered
	} else if systemPrompt != "" {
		req.SystemPrompt = systemPrompt
	}

	if strings.TrimSpace(req.Workflow) == "summary-memory" {
		if summary := loadLatestContextSummary(ctx, r.store, strings.TrimSpace(req.SessionID)); summary != "" {
			req.Input = fmt.Sprintf("Previous compact context summary:\n%s\n\nNew request:\n%s", summary, strings.TrimSpace(req.Input))
		}
	}

	var guardrailMiddleware agentfw.Middleware
	if len(req.Guardrails) > 0 {
		pipeline := guardrail.NewPipeline()
		for _, name := range req.Guardrails {
			switch name {
			case "max_length":
				pipeline.Add(&guardrail.MaxLength{Limit: 10000})
			case "prompt_injection":
				pipeline.AddInput(&guardrail.PromptInjection{})
			case "content_filter":
				pipeline.Add(&guardrail.ContentFilter{})
			case "pii_filter":
				pipeline.Add(&guardrail.PIIFilter{})
			case "topic_filter":
				pipeline.Add(&guardrail.TopicFilter{})
			case "secret_guard":
				pipeline.Add(&guardrail.SecretGuard{})
			}
		}
		guardrailMiddleware = guardrail.NewAgentMiddleware(pipeline)
	}

	opts := cliOptions{
		workflow:     strings.TrimSpace(req.Workflow),
		sessionID:    strings.TrimSpace(req.SessionID),
		tools:        append([]string(nil), req.Tools...),
		systemPrompt: strings.TrimSpace(req.SystemPrompt),
	}
	if len(req.History) > 0 {
		opts.conversation = sanitizeConversationHistory(req.History)
	} else if opts.sessionID != "" {
		opts.conversation = loadSessionConversationHistory(ctx, r.store, opts.sessionID)
	}
	if guardrailMiddleware != nil {
		opts.middlewares = append(opts.middlewares, guardrailMiddleware)
	}
	history := append([]types.Message(nil), opts.conversation...)
	currentInput := strings.TrimSpace(req.Input)
	currentSessionID := strings.TrimSpace(opts.sessionID)
	if currentInput == "" {
		return devuiapi.PlaygroundResponse{}, fmt.Errorf("input is required")
	}

	maxFollowUps := 3
	var result types.RunResult
	for turn := 0; turn < maxFollowUps; turn++ {
		turnOpts := opts
		turnOpts.sessionID = currentSessionID
		turnOpts.conversation = append([]types.Message(nil), history...)

		agent, buildErr := buildAgent(provider, r.store, r.observer, turnOpts)
		if buildErr != nil {
			return devuiapi.PlaygroundResponse{}, fmt.Errorf("agent create failed: %w", buildErr)
		}

		if strings.TrimSpace(turnOpts.workflow) == "" {
			result, err = agent.RunDetailed(ctx, currentInput)
		} else {
			exec, execErr := buildExecutor(agent, r.store, r.observer, turnOpts)
			if execErr != nil {
				return devuiapi.PlaygroundResponse{}, fmt.Errorf("executor create failed: %w", execErr)
			}
			result, err = exec.Run(ctx, currentInput)
		}
		if err != nil {
			return devuiapi.PlaygroundResponse{}, err
		}

		history = append(history,
			types.Message{Role: types.RoleUser, Content: currentInput},
			types.Message{Role: types.RoleAssistant, Content: result.Output},
		)
		history = sanitizeConversationHistory(history)
		if strings.TrimSpace(currentSessionID) == "" && strings.TrimSpace(result.SessionID) != "" {
			currentSessionID = strings.TrimSpace(result.SessionID)
		}

		if !shouldAutoContinue(result.Output) {
			break
		}
		currentInput = buildContinueInstruction(result.Output)
	}

	return devuiapi.PlaygroundResponse{
		Status:    "completed",
		Output:    result.Output,
		RunID:     result.RunID,
		SessionID: currentSessionID,
		Provider:  provider.Name(),
	}, nil
}

func shouldAutoContinue(output string) bool {
	o := strings.ToLower(strings.TrimSpace(output))
	if o == "" {
		return false
	}
	markers := []string{
		"please proceed with",
		"now, please proceed",
		"would you like me to proceed",
		"should i continue",
		"next step:",
		"next, i will",
	}
	for _, m := range markers {
		if strings.Contains(o, m) {
			return true
		}
	}
	return false
}

func buildContinueInstruction(previous string) string {
	return "Continue with the next step immediately. Do not ask for confirmation. Use tools as needed and keep going until the task is complete.\n\nPrevious output:\n" + strings.TrimSpace(previous)
}

func promptInputVars(input map[string]any) map[string]string {
	out := map[string]string{}
	for k, v := range input {
		key := strings.TrimSpace(k)
		if key == "" {
			continue
		}
		out[key] = strings.TrimSpace(fmt.Sprintf("%v", v))
	}
	return out
}

func loadLatestContextSummary(ctx context.Context, store state.Store, sessionID string) string {
	if store == nil || strings.TrimSpace(sessionID) == "" {
		return ""
	}
	runs, err := store.ListRuns(ctx, state.ListRunsQuery{SessionID: sessionID, Limit: 50})
	if err != nil || len(runs) == 0 {
		return ""
	}
	sort.Slice(runs, func(i, j int) bool {
		left := runUpdatedTime(runs[i])
		right := runUpdatedTime(runs[j])
		return left.After(right)
	})
	for _, run := range runs {
		cp, cpErr := store.LoadLatestCheckpoint(ctx, run.RunID)
		if cpErr != nil {
			continue
		}
		if summary := checkpointSummary(cp.State); summary != "" {
			return summary
		}
	}
	return ""
}

func runUpdatedTime(run state.RunRecord) time.Time {
	if run.UpdatedAt != nil {
		return run.UpdatedAt.UTC()
	}
	if run.CreatedAt != nil {
		return run.CreatedAt.UTC()
	}
	return time.Time{}
}

func checkpointSummary(raw map[string]any) string {
	stateRaw, ok := raw["state"].(map[string]any)
	if !ok {
		return ""
	}
	data, ok := stateRaw["data"].(map[string]any)
	if !ok {
		return ""
	}
	if v, ok := data["nextContextSummary"].(string); ok {
		return strings.TrimSpace(v)
	}
	if v, ok := data["memorySummary"].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

func loadSessionConversationHistory(ctx context.Context, store state.Store, sessionID string) []types.Message {
	if store == nil || strings.TrimSpace(sessionID) == "" {
		return nil
	}
	runs, err := store.ListRuns(ctx, state.ListRunsQuery{SessionID: sessionID, Limit: 100})
	if err != nil || len(runs) == 0 {
		return nil
	}
	sort.Slice(runs, func(i, j int) bool {
		return runUpdatedTime(runs[i]).After(runUpdatedTime(runs[j]))
	})
	latest := runs[0]
	if len(latest.Messages) == 0 {
		return nil
	}
	return sanitizeConversationHistory(latest.Messages)
}

func sanitizeConversationHistory(messages []types.Message) []types.Message {
	history := make([]types.Message, 0, len(messages))
	for _, msg := range messages {
		if msg.Role == types.RoleUser || msg.Role == types.RoleAssistant {
			if strings.TrimSpace(msg.Content) == "" {
				continue
			}
			history = append(history, types.Message{Role: msg.Role, Content: msg.Content})
		}
	}
	if len(history) > 24 {
		return history[len(history)-24:]
	}
	return history
}
