package cli

import (
	"fmt"
	"strings"

	"github.com/PipeOpsHQ/agent-sdk-go/tools"
	"github.com/PipeOpsHQ/agent-sdk-go/workflow"
)

func printUsage() {
	fmt.Println("PipeOps Agent Framework CLI")
	fmt.Println("Usage:")
	fmt.Println("  go run ./framework run [--tools=@default] -- \"your prompt\"")
	fmt.Println("  go run ./framework graph-run [--workflow=basic] [--tools=@default] -- \"your prompt\"")
	fmt.Println("  go run ./framework graph-resume [--workflow=basic] [--tools=@default] <run-id>")
	fmt.Println("  go run ./framework sessions [session-id]")
	fmt.Println("  go run ./framework ui [--ui-addr=127.0.0.1:7070] [--ui-open=true]")
	fmt.Println("  go run ./framework ui-api [--ui-addr=0.0.0.0:7070]")
	fmt.Println("  go run ./framework ui-admin create-key [--role=admin]")
	fmt.Println("  go run ./framework cron list|add|remove|trigger|enable|disable|get")
	fmt.Println("  go run ./framework skill list|install|remove|show|create")
	fmt.Println()
	fmt.Println("Agent Configuration:")
	fmt.Println("  --system-prompt=TEXT          Custom system prompt (takes precedence over template)")
	fmt.Println("  --prompt-template=NAME        Use predefined prompt template")
	fmt.Println("  --tools=@default              Tool bundle (comma-separated)")
	fmt.Println("  --workflow=basic              Graph workflow name")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  go run ./framework run --prompt-template=analyst -- \"analyze this log\"")
	fmt.Println("  go run ./framework run --prompt-template=engineer -- \"fix this code\"")
	fmt.Println("  go run ./framework run --system-prompt=\"You are a helpful assistant.\" -- \"help me\"")
	fmt.Println()
	fmt.Printf("  available workflows: %s\n", strings.Join(workflow.Names(), ", "))
	fmt.Printf("  available tools: %s\n", strings.Join(tools.ToolNames(), ", "))
	fmt.Printf("  available bundles: %s\n", strings.Join(tools.BundleNames(), ", "))
	fmt.Printf("  available prompt templates: %s\n", strings.Join(AvailablePromptNames(), ", "))
	fmt.Println()
	fmt.Println("Environment Variables:")
	fmt.Println("  AGENT_SYSTEM_PROMPT          Custom system prompt")
	fmt.Println("  AGENT_PROMPT_TEMPLATE        Prompt template name")
	fmt.Println("  AGENT_TOOLS                  Tool selection (comma-separated)")
	fmt.Println("  AGENT_WORKFLOW               Graph workflow name")
}
