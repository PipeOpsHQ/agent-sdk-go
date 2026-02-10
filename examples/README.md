# SDK Examples

This folder contains runnable examples for common SDK use cases.

## Examples Overview

| Example | Description | Multi-Agent |
|---------|-------------|-------------|
| `log_analyzer` | Log analysis with fix suggestions and PR creation | ✅ Yes |
| `secops` | Security operations (Trivy + log analysis) | No (graph-based) |
| `prompt_templates` | Role-based prompt templates demo | No |
| `agent_minimal` | Minimal agent setup | No |
| `agent_custom_tool` | Custom tool implementation | No |
| `graph_resume` | Checkpoint and resume workflows | No |
| `distributed_enqueue` | Distributed task processing | No |
| `sdk_quickstart` | Quick start template | No |
| `openclaw_ui` | OpenClaw-style autonomous chat UI demo | No |

---

## 1) Log Analyzer (Multi-Agent)
Path: `framework/examples/log_analyzer`

Use case:
- Analyze application logs to identify issues
- Generate code fixes with repository context
- Automatically create pull requests with fixes

Features:
- **Multi-agent architecture**: Analysis Agent → Fix Generator Agent → Fix Applier Agent
- Sensitive data redaction
- Git integration for code context
- GitHub PR automation

Run:
```bash
# Basic analysis
go run ./framework/examples/log_analyzer analyze app.log

# With repository context
go run ./framework/examples/log_analyzer analyze --repo=https://github.com/org/repo app.log

# Create PR with fixes
go run ./framework/examples/log_analyzer analyze --repo=https://github.com/org/repo --create-pr app.log

# From stdin
kubectl logs my-pod | go run ./framework/examples/log_analyzer analyze
```

---

## 2) SecOps Agent
Path: `framework/examples/secops`

Use case:
- Analyze Trivy vulnerability reports
- Process and analyze application logs
- Redact sensitive data automatically

Run:
```bash
# Analyze Trivy report
go run ./framework/examples/secops trivy-report.json

# Analyze logs
go run ./framework/examples/secops app.log

# From stdin
cat logs.txt | go run ./framework/examples/secops
```

---

## 3) Prompt Templates
Path: `framework/examples/prompt_templates`

Use case:
- Demonstrate role-based prompt templates
- Compare agent behavior with different prompts

Available templates: `default`, `analyst`, `engineer`, `specialist`, `assistant`, `reasoning`

Run:
```bash
go run ./framework/examples/prompt_templates analyst "analyze this data"
go run ./framework/examples/prompt_templates engineer "fix this code"
go run ./framework/examples/prompt_templates reasoning "think through this"
```

---

## 4) Minimal Agent
Path: `framework/examples/agent_minimal`

Use case:
- Smallest possible runtime setup using provider factory + `agent.Run`.

Run:
```bash
go run ./framework/examples/agent_minimal "Explain least privilege in 3 bullets"
```

## 5) Agent with Custom Tool
Path: `framework/examples/agent_custom_tool`

Use case:
- Add a custom business tool (`calculate_risk_score`) and let the model invoke it.

Run:
```bash
go run ./framework/examples/agent_custom_tool
```

## 6) Graph + Resume (No LLM Required)
Path: `framework/examples/graph_resume`

Use case:
- Build a static graph with deterministic nodes.
- Persist checkpoints to SQLite.
- Resume a completed run by `run_id`.

Run:
```bash
go run ./framework/examples/graph_resume "critical findings in checkout service"
```

## 7) Distributed Enqueue
Path: `framework/examples/distributed_enqueue`

Use case:
- Submit a run into Redis Streams via distributed coordinator.
- Inspect queue stats.

Prerequisite:
- Redis running and reachable via `AGENT_REDIS_ADDR`.

Run:
```bash
go run ./framework/examples/distributed_enqueue "Investigate payment API timeout spikes"
```

## 8) Quickstart (Hybrid Store + Graph)
Path: `framework/examples/sdk_quickstart`

Use case:
- End-to-end quickstart: provider, store, observer, single run, and graph run.

Run:
```bash
go run ./framework/examples/sdk_quickstart
```

## Environment setup
Pick one template from repo root and export vars:
- `.env.local.example`
- `.env.ollama.example`
- `.env.gemini.example`
- `.env.openai.example`
- `.env.azureopenai.example`
- `.env.distributed.example`
