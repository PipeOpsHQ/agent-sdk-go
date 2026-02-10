package cli

import (
	"log"
	"os"
	"strings"

	"github.com/PipeOpsHQ/agent-sdk-go/internal/config"
	"github.com/PipeOpsHQ/agent-sdk-go/state"
)

func parseArgs(args []string) (cliOptions, []string) {
	opts := cliOptions{}
	positional := make([]string, 0, len(args))
	for _, arg := range args {
		switch {
		case strings.HasPrefix(arg, "--workflow="):
			opts.workflow = strings.TrimSpace(strings.TrimPrefix(arg, "--workflow="))
		case strings.HasPrefix(arg, "--tools="):
			opts.tools = splitCSV(strings.TrimPrefix(arg, "--tools="))
		case strings.HasPrefix(arg, "--system-prompt="):
			opts.systemPrompt = strings.TrimSpace(strings.TrimPrefix(arg, "--system-prompt="))
		case strings.HasPrefix(arg, "--prompt-template="):
			opts.promptTemplate = strings.TrimSpace(strings.TrimPrefix(arg, "--prompt-template="))
		default:
			positional = append(positional, arg)
		}
	}
	return opts, positional
}

func normalizeInput(args []string) string {
	if len(args) > 0 && strings.TrimSpace(args[0]) == "--" {
		args = args[1:]
	}
	return strings.TrimSpace(strings.Join(args, " "))
}

func splitCSV(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func parseBoolEnv(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return config.ParseBoolString(value, fallback)
}

func closeStore(store state.Store) {
	if store == nil {
		return
	}
	if err := store.Close(); err != nil {
		log.Printf("state store close failed: %v", err)
	}
}
