package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	agentfw "github.com/PipeOpsHQ/agent-sdk-go/framework/agent"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/devui"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/flow"
	providerfactory "github.com/PipeOpsHQ/agent-sdk-go/framework/providers/factory"
)

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

	prompt := strings.TrimSpace(strings.Join(os.Args[1:], " "))
	if prompt == "" {
		prompt = "Explain defense in depth in 4 bullets."
	}

	a, err := agentfw.New(
		provider,
		agentfw.WithSystemPrompt("You are concise, practical, and security-focused."),
		agentfw.WithMaxIterations(4),
		agentfw.WithMaxOutputTokens(500),
	)
	if err != nil {
		log.Fatalf("agent create failed: %v", err)
	}

	out, err := a.Run(ctx, prompt)
	if err != nil {
		log.Fatalf("run failed: %v", err)
	}
	fmt.Println(out)
}

func runDevUI() {
	flow.MustRegister(&flow.Definition{
		Name:         "minimal-agent",
		Description:  "Concise, practical, security-focused assistant. Answers general security questions.",
		SystemPrompt: "You are concise, practical, and security-focused.",
		InputExample: "Explain defense in depth in 4 bullets.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"input": map[string]any{
					"type":        "string",
					"description": "A security question or prompt.",
				},
			},
			"required": []string{"input"},
		},
	})

	if err := devui.Start(context.Background()); err != nil {
		log.Fatal(err)
	}
}
