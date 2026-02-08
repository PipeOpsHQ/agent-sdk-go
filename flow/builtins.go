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
		InputExample:  "Show all pods in CrashLoopBackOff across all namespaces, then check the logs of the most recent crash",
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
		Name:        "general-assistant",
		Description: "General-purpose AI assistant with all tools available.",
		Tools:       []string{"@all"},
		SystemPrompt: `You are a versatile AI assistant with access to all available tools.
Help the user with any task — writing, analysis, coding, research, calculations, etc.
Choose the most appropriate tools for each task automatically.`,
		InputExample:  "Scan my current directory for any .env files, list their contents (redact secrets), and check if any ports from the configs are currently in use",
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
