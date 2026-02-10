package flow

// simpleTextSchema is the default input schema for text-based flows.
var simpleTextSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"input": map[string]any{
			"type":        "string",
			"description": "The text input or prompt for the agent.",
		},
	},
	"required": []string{"input"},
}

// simpleOutputSchema is the default output schema for text-based flows.
var simpleOutputSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"output": map[string]any{
			"type":        "string",
			"description": "The agent's text response.",
		},
		"provider": map[string]any{
			"type":        "string",
			"description": "The LLM provider used.",
		},
	},
}

// RegisterBuiltins registers a set of example flows that demonstrate
// common agent patterns. Silently skips any flow name already registered,
// so user-defined flows take priority.
func RegisterBuiltins() {
	_ = Register(&Definition{
		Name:        "code-reviewer",
		Description: "Reviews code changes and provides feedback on quality, bugs, and improvements.",
		Tools:       []string{"@code", "@default"},
		Skills:      []string{"code-audit", "secure-defaults"},
		SystemPrompt: `You are a senior code reviewer. Analyze code changes and provide:
1. Bug identification
2. Security concerns
3. Performance issues
4. Style and readability suggestions
Be constructive and specific with line references.`,
		InputExample: "Review this Go handler for security and correctness issues:\n\nfunc HandleLogin(w http.ResponseWriter, r *http.Request) {\n  username := r.FormValue(\"user\")\n  password := r.FormValue(\"pass\")\n  row := db.QueryRow(\"SELECT id FROM users WHERE name='\"+username+\"' AND pass='\"+password+\"'\")\n  var id int\n  row.Scan(&id)\n  http.SetCookie(w, &http.Cookie{Name: \"session\", Value: fmt.Sprint(id)})\n}",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"input": map[string]any{
					"type":        "string",
					"description": "Code snippet, diff, or pull request description to review.",
				},
				"language": map[string]any{
					"type":        "string",
					"description": "Programming language of the code (auto-detected if omitted).",
					"enum":        []string{"go", "python", "javascript", "typescript", "java", "rust", "other"},
				},
				"focus": map[string]any{
					"type":        "string",
					"description": "Specific review focus area (optional).",
					"enum":        []string{"security", "performance", "correctness", "style", "all"},
					"default":     "all",
				},
			},
			"required": []string{"input"},
		},
		OutputSchema: simpleOutputSchema,
	})

	_ = Register(&Definition{
		Name:        "devops-assistant",
		Description: "Helps with Docker, Kubernetes, and infrastructure tasks.",
		Tools:       []string{"@devops", "@system", "@default"},
		Skills:      []string{"k8s-debug"},
		SystemPrompt: `You are a DevOps engineer assistant. Help with:
- Docker container management
- Kubernetes cluster operations
- Infrastructure troubleshooting
- CI/CD pipeline questions
Be practical and provide ready-to-use commands.`,
		InputExample: "Show all pods in CrashLoopBackOff across all namespaces, then check the logs of the most recent crash",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"input": map[string]any{
					"type":        "string",
					"description": "DevOps task description — Docker, Kubernetes, CI/CD, or infrastructure question.",
				},
				"environment": map[string]any{
					"type":        "string",
					"description": "Target environment (optional).",
					"enum":        []string{"local", "staging", "production"},
				},
			},
			"required": []string{"input"},
		},
		OutputSchema: simpleOutputSchema,
	})

	_ = Register(&Definition{
		Name:        "security-analyst",
		Description: "Analyzes security vulnerabilities, log anomalies, and threat patterns.",
		Workflow:    "basic",
		Tools:       []string{"@security", "@code", "@network", "@default"},
		Skills:      []string{"incident-response", "code-audit"},
		SystemPrompt: `You are a cybersecurity analyst. Analyze inputs for:
- Vulnerability assessment (CVE identification, severity)
- Log anomaly detection (suspicious patterns, intrusion indicators)
- Security best practices and remediation steps
Provide severity ratings and actionable fixes.`,
		InputExample: `Analyze these auth logs for brute-force indicators:

2026-02-08 03:14:22 WARN sshd[2841]: Failed password for root from 45.33.32.156 port 52413
2026-02-08 03:14:23 WARN sshd[2841]: Failed password for root from 45.33.32.156 port 52414
2026-02-08 03:14:23 WARN sshd[2841]: Failed password for admin from 45.33.32.156 port 52415
2026-02-08 03:14:24 INFO sshd[2841]: Accepted publickey for deploy from 10.0.1.5 port 38920
2026-02-08 03:14:25 WARN sshd[2841]: Failed password for root from 45.33.32.156 port 52416`,
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"input": map[string]any{
					"type":        "string",
					"description": "Security logs, vulnerability scan output, or threat data to analyze.",
				},
				"severity_filter": map[string]any{
					"type":        "string",
					"description": "Minimum severity level to include in the report.",
					"enum":        []string{"info", "low", "medium", "high", "critical"},
					"default":     "low",
				},
				"output_format": map[string]any{
					"type":        "string",
					"description": "Desired output format (optional).",
					"enum":        []string{"summary", "detailed", "json"},
					"default":     "summary",
				},
			},
			"required": []string{"input"},
		},
		OutputSchema: simpleOutputSchema,
	})

	_ = Register(&Definition{
		Name:        "data-processor",
		Description: "Processes, transforms, and analyzes structured data (JSON, CSV, logs).",
		Tools:       []string{"@default", "file_system", "shell_command"},
		SystemPrompt: `You are a data processing assistant. Help with:
- Parsing and transforming JSON, CSV, and log data
- Data extraction and summarization
- Pattern matching and filtering
- Data format conversion
Work step-by-step and show intermediate results.`,
		InputExample: `{"orders": [{"id": "ORD-001", "total": 249.99, "status": "shipped", "region": "US"}, {"id": "ORD-002", "total": 89.50, "status": "pending", "region": "EU"}, {"id": "ORD-003", "total": 512.00, "status": "shipped", "region": "US"}, {"id": "ORD-004", "total": 34.99, "status": "cancelled", "region": "APAC"}]}

Group by region, compute total revenue for shipped orders, and output as a markdown table.`,
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"input": map[string]any{
					"type":        "string",
					"description": "Data payload (JSON, CSV, or log text) followed by processing instructions.",
				},
				"format": map[string]any{
					"type":        "string",
					"description": "Input data format (auto-detected if omitted).",
					"enum":        []string{"json", "csv", "log", "text", "auto"},
					"default":     "auto",
				},
				"output_format": map[string]any{
					"type":        "string",
					"description": "Desired output format.",
					"enum":        []string{"markdown", "json", "csv", "text"},
					"default":     "markdown",
				},
			},
			"required": []string{"input"},
		},
		OutputSchema: simpleOutputSchema,
	})

	_ = Register(&Definition{
		Name:        "cost-aware-assistant",
		Description: "Two-pass response with compact summary memory for lower-context follow-ups.",
		Workflow:    "summary-memory",
		Tools:       []string{"@default"},
		SystemPrompt: `You optimize for quality and token efficiency.
- First produce a complete draft internally
- Build a compact memory summary of durable facts and decisions
- Return a refined final response for the user
Keep answers actionable and concise unless detail is requested.`,
		InputExample: "Summarize this outage thread, propose remediation, and keep reusable context for follow-up questions.",
		InputSchema:  simpleTextSchema,
		OutputSchema: simpleOutputSchema,
	})

	_ = Register(&Definition{
		Name:        "support-engineer",
		Description: "Customer support assistant with research and document tooling for guided troubleshooting.",
		Workflow:    "summary-memory",
		Tools:       []string{"@default", "@network", "@docs"},
		Skills:      []string{"document-manager", "research-planner", "pdf-reporting"},
		SystemPrompt: `You are a customer support engineer for PipeOps.
- Be empathetic, direct, and solution-focused
- Ask only essential clarifying questions
- Prefer actionable steps and clear expected outcomes
- Produce shareable summaries or PDF-ready reports when requested
- Continue to the next troubleshooting step without unnecessary confirmation unless high-risk actions are involved`,
		InputExample: "A customer says their webhook integration fails intermittently. Diagnose likely causes and give a step-by-step resolution plan.",
		InputSchema:  simpleTextSchema,
		OutputSchema: simpleOutputSchema,
	})

	_ = Register(&Definition{
		Name:        "plan-profile",
		Description: "Planning-focused profile for requirements breakdown, solution options, and implementation plans.",
		Workflow:    "summary-memory",
		Tools:       []string{"@default", "@docs", "@text"},
		Skills:      []string{"research-planner", "api-design-review", "document-manager"},
		SystemPrompt: `You are a senior planning assistant for software and operations work.
- Clarify goals, constraints, and assumptions first
- Break work into phases with milestones and ownership
- Provide alternative approaches with tradeoffs
- End with an execution-ready plan and verification checklist
- Keep plans practical, explicit, and easy to follow`,
		InputExample: "Design a rollout plan to migrate from a monolith to services with minimal customer impact.",
		InputSchema:  simpleTextSchema,
		OutputSchema: simpleOutputSchema,
	})

	_ = Register(&Definition{
		Name:        "build-profile",
		Description: "Implementation-focused profile for coding, integration, validation, and release readiness.",
		Workflow:    "basic",
		Tools:       []string{"@code", "@system", "@default"},
		Skills:      []string{"secure-defaults", "release-readiness"},
		SystemPrompt: `You are a senior implementation engineer.
- Turn requirements into working code with clear incremental steps
- Prefer safe, testable changes and explain verification clearly
- Surface risks early (migrations, compatibility, operational impact)
- Include concise test/build commands and expected outcomes
- Optimize for correctness first, then maintainability and speed`,
		InputExample: "Implement JWT refresh token rotation in our Go API and include migration-safe rollout steps.",
		InputSchema:  simpleTextSchema,
		OutputSchema: simpleOutputSchema,
	})

	_ = Register(&Definition{
		Name:        "mod-profile",
		Description: "Personal moderator profile for priority triage, guardrails, and high-signal execution planning.",
		Workflow:    "router",
		Tools:       []string{"@default", "@system", "@network", "@docs"},
		Skills:      []string{"oncall-triage", "release-readiness", "secure-defaults"},
		SystemPrompt: `You are MOD, a pragmatic operator for autonomous work.
- Prioritize by impact and urgency
- Keep safety and rollback paths explicit
- Convert vague requests into ordered execution plans
- Escalate blockers immediately with options
- Keep updates concise and decision-oriented`,
		InputExample: "Review today's production alerts and produce a priority-ordered action plan.",
		InputSchema:  simpleTextSchema,
		OutputSchema: simpleOutputSchema,
	})

	_ = Register(&Definition{
		Name:        "jarvus-autonomous",
		Description: "Jarvus-like autonomous profile for long-horizon execution, tool orchestration, and progress reporting.",
		Workflow:    "summary-memory",
		Tools:       []string{"@all"},
		Skills:      []string{"research-planner", "release-readiness", "api-design-review", "document-manager"},
		SystemPrompt: `You are Jarvus, an autonomous execution assistant.
- Drive tasks end-to-end with minimal supervision
- Use tools proactively and verify intermediate results
- Maintain a running plan and adapt as new evidence appears
- Surface tradeoffs before high-risk decisions
- Provide periodic concise checkpoints (done / next / blocked)`,
		InputExample: "Take this backlog item and execute it fully with test/build verification and status checkpoints.",
		InputSchema:  simpleTextSchema,
		OutputSchema: simpleOutputSchema,
	})

	_ = Register(&Definition{
		Name:        "clawdbot",
		Description: "Autonomous engineering profile focused on repo-driven implementation, debugging, and dependable execution loops.",
		Workflow:    "summary-memory",
		Tools:       []string{"@code", "@system", "@network", "@default"},
		Skills:      []string{"code-audit", "release-readiness", "secure-defaults", "api-design-review"},
		SystemPrompt: `You are ClawdBot, an autonomous software engineer.
- Read code and infer project conventions before changing behavior
- Prefer incremental edits with clear validation and rollback awareness
- Execute through to completion: implement, verify, and summarize outcomes
- Surface blockers with concrete options and recommended default path
- Keep communication concise and execution-oriented`,
		InputExample: "Fix the flaky retry logic in our API client, add regression tests, and summarize the root cause.",
		InputSchema:  simpleTextSchema,
		OutputSchema: simpleOutputSchema,
	})

	_ = Register(&Definition{
		Name:        "openclaw-bot",
		Description: "Open-ended autonomous operator profile for research-heavy tasks, multi-step execution, and proactive status updates.",
		Workflow:    "router",
		Tools:       []string{"@all"},
		Skills:      []string{"research-planner", "oncall-triage", "release-readiness", "document-manager"},
		SystemPrompt: `You are OpenClaw Bot, a proactive autonomous operations and engineering agent.
- On first engagement in a new thread, run a short kickoff: ask what you should be called, confirm top priorities, success criteria, and risk boundaries before execution.
- If these are already known in the current thread, do not re-ask; proceed with execution.
- Plan, execute, and adapt across long-running multi-step tasks
- Use tools deliberately and verify each major step before proceeding
- Keep a concise running status: completed, in-progress, and next actions
- Default to safe decisions and highlight risk before high-impact operations
- Produce shareable outputs (notes/runbooks/reports) when useful`,
		InputExample: "Investigate repeated deployment failures, identify root causes, apply safe fixes, and provide a release-readiness report.",
		InputSchema:  simpleTextSchema,
		OutputSchema: simpleOutputSchema,
	})

	_ = Register(&Definition{
		Name:        "general-assistant",
		Description: "General-purpose AI assistant with all tools available.",
		Tools:       []string{"@all"},
		SystemPrompt: `You are a versatile AI assistant with access to all available tools.
Help the user with any task — writing, analysis, coding, research, calculations, etc.
Choose the most appropriate tools for each task automatically.`,
		InputExample: "Scan my current directory for any .env files, list their contents (redact secrets), and check if any ports from the configs are currently in use",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"input": map[string]any{
					"type":        "string",
					"description": "Any task or question — the agent selects appropriate tools automatically.",
				},
			},
			"required": []string{"input"},
		},
		OutputSchema: simpleOutputSchema,
	})
}
