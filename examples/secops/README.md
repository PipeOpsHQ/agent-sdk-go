# SecOps Example Agent

A standalone SecOps agent built on the PipeOps Agent SDK that analyzes Trivy vulnerability reports and processes/analyzes application logs.

## Features

- **Trivy Report Analysis**: Parses and categorizes vulnerabilities by severity
- **Log Processing**: Redacts sensitive data and classifies log entries
- **Intelligent Routing**: Automatically detects input type (Trivy JSON vs logs)
- **Actionable Output**: Provides compact, prioritized recommendations
- **Graph-based Workflow**: Uses the SDK's graph execution for deterministic processing

## Quick Start

### Analyze Trivy Report

```bash
# From file
go run . trivy-report.json

# From stdin
cat trivy-report.json | go run .

# Using trivy directly
trivy image myimage:latest -f json | go run .
```

### Analyze Logs

```bash
# From file
go run . application.log

# From stdin
cat application.log | go run .

# From kubectl
kubectl logs my-pod | go run .
```

## Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `OPENAI_API_KEY` | OpenAI API key | - |
| `ANTHROPIC_API_KEY` | Anthropic API key | - |
| `LLM_PROVIDER` | Provider to use (openai, anthropic, gemini, ollama) | auto-detect |
| `STATE_BACKEND` | State backend (sqlite, redis) | sqlite |
| `STATE_SQLITE_PATH` | SQLite database path | ./.ai-agent/state.db |

## Architecture

The agent uses a graph-based workflow with the following nodes:

```
                    ┌─────────────────┐
                    │     route       │
                    │ (detect input)  │
                    └────────┬────────┘
                             │
              ┌──────────────┴──────────────┐
              │                             │
              ▼                             ▼
     ┌────────────────┐            ┌────────────────┐
     │  parse_trivy   │            │  redact_logs   │
     └───────┬────────┘            └───────┬────────┘
             │                             │
             ▼                             ▼
     ┌────────────────┐            ┌────────────────┐
     │ build_trivy_   │            │ classify_logs  │
     │    prompt      │            └───────┬────────┘
     └───────┬────────┘                    │
             │                             ▼
             ▼                     ┌────────────────┐
     ┌────────────────┐            │ build_logs_    │
     │assistant_trivy │            │    prompt      │
     └───────┬────────┘            └───────┬────────┘
             │                             │
             │                             ▼
             │                     ┌────────────────┐
             │                     │ assistant_logs │
             │                     └───────┬────────┘
             │                             │
             └──────────────┬──────────────┘
                            │
                            ▼
                    ┌────────────────┐
                    │    finalize    │
                    └────────────────┘
```

## Example Output

### Trivy Analysis

```
run_id=abc123 session_id=def456

## Critical Findings (3)
• CVE-2024-1234: Upgrade openssl from 1.1.1 to 1.1.1w (RCE risk)
• CVE-2024-5678: Update curl to 8.5.0 (data exfiltration)
• CVE-2024-9012: Patch glibc to 2.38-4 (privilege escalation)

## Immediate Actions
1. Rebuild container with updated base image
2. Run `trivy image --severity CRITICAL` in CI pipeline
3. Enable automatic dependency updates
```

### Log Analysis

```
run_id=abc123 session_id=def456

## Issues Found (3)
• Connection timeouts to database (12 occurrences)
• Memory allocation failures in worker threads
• Rate limiting triggered for API endpoint /users

## Recommended Fixes
1. Increase database connection pool size
2. Tune worker memory limits or add horizontal scaling
3. Review rate limit thresholds for /users endpoint
```

## Security Features

### Sensitive Data Redaction

The agent automatically redacts:
- API keys and tokens
- Passwords and secrets
- Bearer tokens
- Authorization headers

Example:
```
Before: token=sk-1234567890abcdef
After:  token= [REDACTED]
```

## Building

```bash
cd examples/secops
go build -o secops-agent .
./secops-agent trivy-report.json
```

## Customization

### Modify System Prompt

Edit the `secOpsSystemPrompt` constant in `main.go`:

```go
const secOpsSystemPrompt = `Your custom prompt here...`
```

### Add Processing Steps

Add new nodes to the graph in `newSecOpsExecutor()`:

```go
g.AddNode("my_custom_node", graph.NewToolNode(func(ctx context.Context, state *graph.State) error {
    // Your custom processing logic
    return nil
}))
```

## Related

- [Log Analyzer Example](../log_analyzer/README.md) - Advanced log analysis with PR creation
- [Agent SDK Documentation](../../README.md)
- [Tools Reference](../../tools/README.md)

