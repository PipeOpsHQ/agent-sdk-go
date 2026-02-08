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
		SystemPrompt: `You are a senior code reviewer. Analyze code changes and provide:
1. Bug identification
2. Security concerns
3. Performance issues
4. Style and readability suggestions
Be constructive and specific with line references.`,
		InputExample: "Review this function for potential issues:\n\nfunc processUser(id string) error {\n  user, _ := db.FindUser(id)\n  sendEmail(user.Email)\n  return nil\n}",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"input": map[string]any{
					"type":        "string",
					"description": "Code snippet or diff to review.",
				},
				"language": map[string]any{
					"type":        "string",
					"description": "Programming language (optional).",
					"enum":        []string{"go", "python", "javascript", "typescript", "java", "rust", "other"},
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
		SystemPrompt: `You are a DevOps engineer assistant. Help with:
- Docker container management
- Kubernetes cluster operations
- Infrastructure troubleshooting
- CI/CD pipeline questions
Be practical and provide ready-to-use commands.`,
		InputExample:  "List all running Docker containers and show their resource usage",
		InputSchema:  simpleTextSchema,
		OutputSchema: simpleOutputSchema,
	})

	_ = Register(&Definition{
		Name:        "security-analyst",
		Description: "Analyzes security vulnerabilities, log anomalies, and threat patterns.",
		Workflow:    "basic",
		Tools:       []string{"@security", "@code", "@network", "@default"},
		SystemPrompt: `You are a cybersecurity analyst. Analyze inputs for:
- Vulnerability assessment (CVE identification, severity)
- Log anomaly detection (suspicious patterns, intrusion indicators)
- Security best practices and remediation steps
Provide severity ratings and actionable fixes.`,
		InputExample: `Analyze these suspicious log entries:
2026-02-08 10:23:45 WARN Failed login attempt for user admin from 192.168.1.100
2026-02-08 10:23:47 WARN Failed login attempt for user admin from 192.168.1.100
2026-02-08 10:23:48 WARN Failed login attempt for user admin from 192.168.1.100
2026-02-08 10:23:50 INFO User admin logged in from 192.168.1.100`,
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"input": map[string]any{
					"type":        "string",
					"description": "Log entries, vulnerability report, or security data to analyze.",
				},
				"severity_filter": map[string]any{
					"type":        "string",
					"description": "Minimum severity to report (optional).",
					"enum":        []string{"info", "low", "medium", "high", "critical"},
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
		InputExample: `{"users": [{"name": "Alice", "age": 30, "active": true}, {"name": "Bob", "age": 25, "active": false}]}

Extract only active users and format as a markdown table.`,
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"input": map[string]any{
					"type":        "string",
					"description": "Data payload (JSON, CSV, or log text) with instructions.",
				},
				"format": map[string]any{
					"type":        "string",
					"description": "Input data format hint (optional).",
					"enum":        []string{"json", "csv", "log", "text", "auto"},
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
		InputExample:  "What is the current date and time? Generate a UUID for my new project.",
		InputSchema:  simpleTextSchema,
		OutputSchema: simpleOutputSchema,
	})
}
