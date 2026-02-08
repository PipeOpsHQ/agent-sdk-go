package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"

	agentfw "github.com/PipeOpsHQ/agent-sdk-go/framework/agent"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/devui"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/flow"
	providerfactory "github.com/PipeOpsHQ/agent-sdk-go/framework/providers/factory"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/tools"
)

type riskInput struct {
	Critical int `json:"critical"`
	High     int `json:"high"`
	Medium   int `json:"medium"`
}

type riskOutput struct {
	Score int    `json:"score"`
	Tier  string `json:"tier"`
}

func main() {
	if len(os.Args) > 1 && strings.ToLower(os.Args[1]) == "ui" {
		runDevUI()
		return
	}

	ctx := context.Background()
	provider, err := providerfactory.FromEnv(ctx)
	if err != nil {
		log.Fatalf("provider setup failed: %v", err)
	}

	riskTool := tools.NewFuncTool(
		"calculate_risk_score",
		"Calculate a simple risk score from vulnerability counts.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"critical": map[string]any{"type": "integer"},
				"high":     map[string]any{"type": "integer"},
				"medium":   map[string]any{"type": "integer"},
			},
			"required": []string{"critical", "high", "medium"},
		},
		func(ctx context.Context, args json.RawMessage) (any, error) {
			_ = ctx
			var in riskInput
			if err := json.Unmarshal(args, &in); err != nil {
				return nil, err
			}
			score := (in.Critical * 10) + (in.High * 5) + (in.Medium * 2)
			tier := "low"
			switch {
			case score >= 60:
				tier = "critical"
			case score >= 30:
				tier = "high"
			case score >= 15:
				tier = "medium"
			}
			return riskOutput{Score: score, Tier: tier}, nil
		},
	)

	a, err := agentfw.New(
		provider,
		agentfw.WithSystemPrompt("Use tools when available and return compact security recommendations."),
		agentfw.WithTool(riskTool),
		agentfw.WithMaxIterations(4),
	)
	if err != nil {
		log.Fatalf("agent create failed: %v", err)
	}

	prompt := strings.Join([]string{
		"Use calculate_risk_score with critical=2, high=4, medium=3.",
		"Return: score, tier, and top 3 immediate remediation priorities.",
	}, " ")

	result, err := a.RunDetailed(ctx, prompt)
	if err != nil {
		log.Fatalf("run failed: %v", err)
	}

	fmt.Printf("run_id=%s session_id=%s\n\n", result.RunID, result.SessionID)
	fmt.Println(result.Output)
}

func runDevUI() {
	flow.MustRegister(&flow.Definition{
		Name:         "risk-scorer",
		Description:  "Agent with custom calculate_risk_score tool. Computes risk tiers from vulnerability counts and gives remediation advice.",
		Tools:        []string{"calculate_risk_score"},
		SystemPrompt: "Use tools when available and return compact security recommendations.",
		InputExample: "Use calculate_risk_score with critical=2, high=4, medium=3. Return: score, tier, and top 3 immediate remediation priorities.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"input": map[string]any{
					"type":        "string",
					"description": "Prompt describing vulnerability counts to score and analyze.",
				},
			},
			"required": []string{"input"},
		},
	})

	if err := devui.Start(context.Background()); err != nil {
		log.Fatal(err)
	}
}
