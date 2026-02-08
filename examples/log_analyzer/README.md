# Log Analyzer Agent

An intelligent log analysis agent built on the PipeOps Agent SDK that analyzes application logs, identifies issues, suggests fixes, and can automatically create pull requests with the fixes.

## Features

- **🔍 Log Analysis**: Parse and analyze application logs to identify errors, warnings, and issues
- **📊 Issue Classification**: Categorize issues by severity (critical, high, medium, low)
- **🔧 Fix Suggestions**: Generate specific, actionable code fixes
- **📦 Repository Integration**: Clone repos to analyze issues with full code context
- **🚀 PR Automation**: Automatically create pull requests with suggested fixes

## Quick Start

### Basic Log Analysis

```bash
# Analyze a log file
go run . analyze application.log

# Analyze from stdin
cat application.log | go run . analyze

# Analyze kubectl logs
kubectl logs my-pod | go run . analyze
```

### With Repository Context

```bash
# Clone repo and analyze with code context
go run . analyze --repo=https://github.com/myorg/myapp application.log

# Specify a branch
go run . analyze --repo=https://github.com/myorg/myapp --branch=develop app.log
```

### Create Pull Request

```bash
# Analyze, generate fixes, and create PR
go run . analyze \
  --repo=https://github.com/myorg/myapp \
  --create-pr \
  --pr-title="fix: resolve connection timeout issues" \
  application.log
```

## Environment Variables

| Variable | Required | Description |
|----------|----------|-------------|
| `OPENAI_API_KEY` | Yes* | OpenAI API key |
| `ANTHROPIC_API_KEY` | Yes* | Anthropic API key |
| `GITHUB_TOKEN` | For PRs | GitHub token with repo write access |
| `LLM_PROVIDER` | No | Provider to use (openai, anthropic, gemini, ollama) |
| `STATE_BACKEND` | No | State backend (sqlite, redis), default: sqlite |

*At least one LLM provider key is required

## Options

| Option | Description |
|--------|-------------|
| `--repo=<url>` | Git repository URL for code context |
| `--branch=<name>` | Branch to checkout (default: main) |
| `--create-pr` | Create a pull request with fixes |
| `--pr-title=<text>` | PR title |
| `--dry-run` | Show what would be done without making changes |

## How It Works

### Phase 1: Log Analysis

The agent reads your logs and:
1. Redacts sensitive data (API keys, tokens, passwords)
2. Classifies log entries (errors, warnings, info)
3. Identifies patterns and root causes
4. Generates a structured analysis report

### Phase 2: Code Context (with --repo)

If a repository URL is provided:
1. Clones the repository (shallow clone for speed)
2. Searches for relevant source files
3. Correlates log errors with code locations
4. Generates more precise fix suggestions

### Phase 3: PR Creation (with --create-pr)

If PR creation is enabled:
1. Creates a new branch
2. Applies suggested fixes to files
3. Commits changes with descriptive message
4. Pushes branch and creates PR via GitHub API

## Example Output

### Log Analysis Only

```
🔍 Analyzing logs...

📋 Analysis Results:
────────────────────────────────────────────────────────────

## Issues Found

- [CRITICAL] Database connection pool exhausted
  - Root cause: Max connections (10) reached under load
  - Fix: Increase pool size or implement connection recycling

- [HIGH] Memory allocation failures in worker threads
  - Root cause: Workers not releasing memory after tasks
  - Fix: Add defer statements for cleanup, review goroutine lifecycle

- [MEDIUM] Rate limiting triggered for /api/users
  - Root cause: Burst traffic pattern exceeding 100 req/min limit
  - Fix: Implement client-side throttling or increase limit

## Summary
Application experiencing resource exhaustion under load. 
Priority: 1) Database pool 2) Memory leaks 3) Rate limits

────────────────────────────────────────────────────────────
✅ Analysis complete.
💡 Tip: Add --repo=<url> to analyze with code context and suggest fixes
```

### With Repository Context

```
🔍 Analyzing logs...
📋 Analysis Results:
[... analysis ...]

📦 Cloning repository: https://github.com/myorg/myapp
   Cloned to: /tmp/log-analyzer-repos/myapp

🔧 Generating code fixes...

📝 Suggested Fixes:
────────────────────────────────────────────────────────────

## Fix 1: Database Connection Pool (db/pool.go)

```diff
- maxConnections: 10,
+ maxConnections: 50,
+ maxIdleTime: 5 * time.Minute,
```

## Fix 2: Worker Memory Leak (worker/processor.go)

```diff
  func (w *Worker) Process(job Job) error {
+     defer w.cleanup()
      result, err := w.execute(job)
```

## Fix 3: Rate Limit Configuration (config/limits.go)

```diff
  RateLimits: map[string]int{
-     "/api/users": 100,
+     "/api/users": 500,
  }
```

────────────────────────────────────────────────────────────
✅ Analysis complete with code context.
💡 Tip: Add --create-pr to automatically create a pull request
```

## Supported Log Formats

The agent handles various log formats:

- **Structured JSON logs**
- **Standard text logs** (timestamp prefix)
- **Kubernetes pod logs**
- **Docker container logs**
- **Application framework logs** (Rails, Django, Express, etc.)

## Security

### Sensitive Data Redaction

The agent automatically redacts:
- API keys and tokens
- Passwords and secrets
- AWS credentials
- Bearer tokens
- Authorization headers

### Repository Access

For PR creation, the agent needs:
- Read access to clone the repository
- Write access to push branches
- PR creation permissions

Use a GitHub token with appropriate scopes:
- `repo` (for private repos)
- `public_repo` (for public repos only)

## Building

```bash
# Build binary
cd examples/log_analyzer
go build -o log-analyzer .

# Run binary
./log-analyzer analyze application.log
```

## Integration Examples

### GitHub Actions

```yaml
name: Log Analysis
on:
  workflow_dispatch:
    inputs:
      pod_name:
        description: 'Pod name to analyze'
        required: true

jobs:
  analyze:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      
      - name: Get Pod Logs
        run: kubectl logs ${{ inputs.pod_name }} > logs.txt
        
      - name: Analyze Logs
        env:
          OPENAI_API_KEY: ${{ secrets.OPENAI_API_KEY }}
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
        run: |
          go run ./examples/log_analyzer analyze \
            --repo=${{ github.repository }} \
            --create-pr \
            logs.txt
```

### CI/CD Pipeline

```bash
#!/bin/bash
# analyze-and-fix.sh

# Collect logs from production
kubectl logs -l app=myapp --since=1h > /tmp/logs.txt

# Analyze and create fix PR
go run ./examples/log_analyzer analyze \
  --repo=https://github.com/myorg/myapp \
  --create-pr \
  --pr-title="fix: automated fixes for $(date +%Y-%m-%d)" \
  /tmp/logs.txt
```

### Monitoring Integration

```bash
# Triggered by alerting system
curl -X POST https://your-server/analyze \
  -d "logs=$(cat /var/log/app.log)" \
  -d "repo=https://github.com/myorg/myapp" \
  -d "create_pr=true"
```

## Troubleshooting

### "LLM provider setup failed"

Ensure you have set a valid API key:
```bash
export OPENAI_API_KEY=sk-...
# or
export ANTHROPIC_API_KEY=sk-ant-...
```

### "Failed to clone repository"

Check that:
1. The URL is correct and accessible
2. For private repos, you have proper SSH keys or HTTPS credentials
3. Git is installed and in PATH

### "GITHUB_TOKEN required for PR creation"

Create a GitHub personal access token:
1. Go to GitHub Settings → Developer Settings → Personal Access Tokens
2. Generate a token with `repo` scope
3. Export it: `export GITHUB_TOKEN=ghp_...`

### "No changes to commit"

The agent couldn't apply fixes, possibly because:
- The code structure doesn't match expected patterns
- Fixes are for configuration/infrastructure, not code
- The analysis was for monitoring, not code issues

## Architecture

```
log_analyzer/
└── main.go          # Complete agent implementation

Components:
├── Log Preprocessor   # Redact secrets, truncate, normalize
├── Analysis Agent     # LLM-powered log analysis
├── Fix Generator      # Code fix suggestions with context
├── PR Creator         # Git operations and GitHub API
└── Observer           # Tracing and debugging
```

## Customization

### Custom System Prompts

Edit the prompts in `main.go`:

```go
const logAnalyzerPrompt = `Your custom analysis prompt...`
const codeFixPrompt = `Your custom fix generation prompt...`
```

### Adding New Log Formats

Extend the `preprocessLogs` function:

```go
func preprocessLogs(logs string) string {
    // Add custom parsing logic
    if isCustomFormat(logs) {
        return parseCustomFormat(logs)
    }
    // ...existing code...
}
```

### Custom Tools

Add more tools to the agent:

```go
selectedTools, err := tools.BuildSelection([]string{
    "file_system",
    "code_search",
    "shell_command",  // Add more tools
})
```

## Related

- [Agent SDK Documentation](../../README.md)
- [SecOps Example](../secops/README.md)
- [Tools Reference](../../tools/README.md)
- [Prompt Templates](../prompt_templates/README.md)

## License

Apache 2.0 - See LICENSE file in repository root.

