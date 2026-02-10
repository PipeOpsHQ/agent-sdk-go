package cli

import (
	"context"
	"strings"

	"github.com/PipeOpsHQ/agent-sdk-go/prompt"
)

func Run(ctx context.Context, args []string) {
	_, _ = prompt.LoadDir("./.ai-agent/prompts")
	if len(args) < 1 {
		printUsage()
		return
	}

	switch strings.TrimSpace(args[0]) {
	case "run":
		runSingle(ctx, args[1:])
	case "graph-run":
		runGraph(ctx, args[1:])
	case "graph-resume":
		resumeGraph(ctx, args[1:])
	case "sessions":
		listSessions(ctx, args[1:])
	case "ui":
		runUI(ctx, args[1:], false)
	case "ui-api":
		runUI(ctx, args[1:], true)
	case "ui-admin":
		runUIAdmin(ctx, args[1:])
	case "cron":
		runCronCLI(ctx, args[1:])
	case "skill":
		runSkillCLI(args[1:])
	case "help", "-h", "--help":
		printUsage()
	default:
		runSingle(ctx, args)
	}
}
