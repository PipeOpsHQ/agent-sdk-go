package tools

func init() {
	// Math/calculation tools
	MustRegisterTool(
		"calculator",
		"Evaluate arithmetic expressions with +, -, *, /, and parentheses.",
		func() Tool { return NewCalculator() },
	)

	// Security tools
	MustRegisterTool(
		"secret_redactor",
		"Detect and redact secrets (API keys, tokens, passwords, credentials) from text.",
		func() Tool { return NewSecretRedactor() },
	)
	MustRegisterTool(
		"hash_generator",
		"Generate cryptographic hashes (MD5, SHA1, SHA256, SHA512) of input strings.",
		func() Tool { return NewHashGenerator() },
	)

	// Data processing tools
	MustRegisterTool(
		"json_parser",
		"Parse, validate, and query JSON data using dot-notation.",
		func() Tool { return NewJSONParser() },
	)
	MustRegisterTool(
		"regex_matcher",
		"Perform regex operations: match, find_all, replace, split, or test patterns.",
		func() Tool { return NewRegexMatcher() },
	)
	MustRegisterTool(
		"text_processor",
		"Process and transform text: counting, case conversion, extraction, formatting.",
		func() Tool { return NewTextProcessor() },
	)
	MustRegisterTool(
		"document_generator",
		"Generate structured documents (plan/report/rfc/runbook/notes) and optionally save them.",
		func() Tool { return NewDocumentGenerator() },
	)
	MustRegisterTool(
		"pdf_generator",
		"Generate a simple PDF from text or source file content.",
		func() Tool { return NewPDFGenerator() },
	)
	MustRegisterTool(
		"google_docs_manager",
		"Manage Google Docs and Drive documents: create, update, list, and export PDF links.",
		func() Tool { return NewGoogleDocsManager() },
	)
	MustRegisterTool(
		"document_preview",
		"Create chat-ready previews and view/download links for generated documents.",
		func() Tool { return NewDocumentPreview() },
	)

	// Encoding tools
	MustRegisterTool(
		"base64_codec",
		"Encode or decode base64 strings (standard and URL-safe).",
		func() Tool { return NewBase64Codec() },
	)

	// Date/time tools
	MustRegisterTool(
		"timestamp_converter",
		"Convert between Unix timestamps and human-readable date formats.",
		func() Tool { return NewTimestampConverter() },
	)

	// Generator tools
	MustRegisterTool(
		"uuid_generator",
		"Generate random UUIDs (v4) with format options.",
		func() Tool { return NewUUIDGenerator() },
	)
	MustRegisterTool(
		"url_parser",
		"Parse, validate, encode, or decode URLs.",
		func() Tool { return NewURLParser() },
	)

	// Code/Git tools
	MustRegisterTool(
		"git_repo",
		"Clone Git repositories and read code for context. Supports GitHub, GitLab, Bitbucket, and any Git URL.",
		func() Tool { return NewGitRepo() },
	)
	MustRegisterTool(
		"code_search",
		"Search code files for patterns, symbols, and definitions with context.",
		func() Tool { return NewCodeSearch() },
	)
	MustRegisterTool(
		"diff_generator",
		"Generate, apply, and analyze text/code diffs.",
		func() Tool { return NewDiffGenerator() },
	)

	// Network tools
	MustRegisterTool(
		"http_client",
		"Make HTTP requests to APIs and web services.",
		func() Tool { return NewHTTPClient() },
	)
	MustRegisterTool(
		"web_scraper",
		"Scrape and extract content from web pages.",
		func() Tool { return NewWebScraper() },
	)
	MustRegisterTool(
		"web_search",
		"Search the web by query and return top result links/snippets.",
		func() Tool { return NewWebSearch() },
	)

	// System tools
	MustRegisterTool(
		"shell_command",
		"Execute shell commands safely with timeout and working directory support.",
		func() Tool { return NewShellCommand() },
	)
	MustRegisterTool(
		"file_system",
		"Perform file operations: read, write, list, search, copy, move.",
		func() Tool { return NewFileSystem() },
	)
	MustRegisterTool(
		"env_vars",
		"Access environment variables with automatic secret redaction.",
		func() Tool { return NewEnvVars() },
	)

	// Memory/State tools
	MustRegisterTool(
		"memory_store",
		"Store and retrieve information across agent interactions with TTL support.",
		func() Tool { return NewMemoryStore() },
	)
	MustRegisterTool(
		"tmpdir",
		"Create and manage temporary directories with file read/write support.",
		func() Tool { return NewTmpDir() },
	)

	// Container tools
	MustRegisterTool(
		"docker",
		"Manage Docker containers and images: run, stop, build, pull, inspect, logs.",
		func() Tool { return NewDocker() },
	)
	MustRegisterTool(
		"docker_compose",
		"Manage Docker Compose services: up, down, build, restart, logs, exec.",
		func() Tool { return NewDockerCompose() },
	)

	// Kubernetes tools
	MustRegisterTool(
		"kubectl",
		"Interact with Kubernetes clusters: get, describe, apply, delete, scale, rollout.",
		func() Tool { return NewKubectl() },
	)
	MustRegisterTool(
		"k3s",
		"Manage k3s lightweight Kubernetes: install, uninstall, status, kubectl, crictl.",
		func() Tool { return NewK3s() },
	)

	// Cron/Scheduling tools (scheduler injected at runtime by DevUI)
	MustRegisterTool(
		"cron_manager",
		"Schedule recurring agent tasks with cron expressions. Use when asked to: run something periodically, schedule a job, set up a recurring check, automate a task on a timer, create a cron job. Operations: list, add, remove, trigger, enable, disable.",
		func() Tool { return NewCronManager(nil) },
	)

	// Self-API tool (baseURL injected at runtime by DevUI)
	MustRegisterTool(
		"self_api",
		"Call the agent's own DevUI API to manage cron jobs, skills, flows, runs, tools, workflows, runtime, and more. The agent can introspect and control itself.",
		func() Tool { return NewSelfAPI("http://127.0.0.1:7070") },
	)

	// Linux system tools
	MustRegisterTool(
		"curl",
		"curl-style HTTP client with auth, cookies, TLS options, redirect following, and verbose timing. More featured than http_client.",
		func() Tool { return NewCurl() },
	)
	MustRegisterTool(
		"dns_lookup",
		"Resolve DNS records: A, AAAA, MX, NS, TXT, CNAME, SRV, PTR. Supports custom DNS servers.",
		func() Tool { return NewDNSLookup() },
	)
	MustRegisterTool(
		"network_utils",
		"Network utilities: ping hosts, check ports, scan port ranges, resolve hostnames.",
		func() Tool { return NewNetworkUtils() },
	)
	MustRegisterTool(
		"process_manager",
		"List, find, and inspect running processes. Get top CPU/memory consumers. Like ps, top, pgrep.",
		func() Tool { return NewProcessManager() },
	)
	MustRegisterTool(
		"disk_usage",
		"Check disk space (df) and directory sizes (du). Shows filesystem usage and largest directories.",
		func() Tool { return NewDiskUsage() },
	)
	MustRegisterTool(
		"system_info",
		"Get system information: hostname, OS, CPU, memory, uptime, network interfaces. Like uname, free, hostnamectl.",
		func() Tool { return NewSystemInfo() },
	)
	MustRegisterTool(
		"archive",
		"Create, extract, and list archives: tar, tar.gz, tar.bz2, tar.xz, zip.",
		func() Tool { return NewArchive() },
	)
	MustRegisterTool(
		"log_viewer",
		"View and search log files: tail, head, grep patterns, journalctl for systemd services.",
		func() Tool { return NewLogViewer() },
	)
	MustRegisterTool(
		"todo_manager",
		"Manage a task/todo list: add, update, remove, and list items with status, priority, dependencies, and tags.",
		func() Tool { return NewTodoManager() },
	)

	// Register bundles
	MustRegisterBundle("default", "Default built-in toolset", []string{
		"calculator",
		"json_parser",
		"base64_codec",
		"timestamp_converter",
		"uuid_generator",
		"url_parser",
		"regex_matcher",
		"text_processor",
		"document_generator",
	})

	MustRegisterBundle("security", "Security-focused tools", []string{
		"secret_redactor",
		"hash_generator",
	})

	MustRegisterBundle("encoding", "Encoding and decoding utilities", []string{
		"base64_codec",
		"json_parser",
		"url_parser",
	})

	MustRegisterBundle("code", "Code and Git repository tools", []string{
		"git_repo",
		"code_search",
		"diff_generator",
	})

	MustRegisterBundle("network", "Network and API tools", []string{
		"http_client",
		"web_search",
		"web_scraper",
		"curl",
		"dns_lookup",
		"network_utils",
	})

	MustRegisterBundle("system", "System interaction tools", []string{
		"shell_command",
		"file_system",
		"env_vars",
		"tmpdir",
		"process_manager",
		"disk_usage",
		"system_info",
		"log_viewer",
		"archive",
	})

	MustRegisterBundle("memory", "State and memory tools", []string{
		"memory_store",
		"todo_manager",
	})

	MustRegisterBundle("text", "Text processing tools", []string{
		"text_processor",
		"document_generator",
		"pdf_generator",
		"json_parser",
		"regex_matcher",
	})

	MustRegisterBundle("docs", "Document authoring and export tools", []string{
		"document_generator",
		"pdf_generator",
		"google_docs_manager",
		"document_preview",
		"file_system",
		"text_processor",
		"json_parser",
	})

	MustRegisterBundle("container", "Container tools", []string{
		"docker",
		"docker_compose",
	})

	MustRegisterBundle("kubernetes", "Kubernetes tools", []string{
		"kubectl",
		"k3s",
	})

	MustRegisterBundle("devops", "DevOps tools", []string{
		"docker",
		"docker_compose",
		"kubectl",
		"k3s",
	})

	MustRegisterBundle("scheduling", "Cron and scheduling tools", []string{
		"cron_manager",
		"self_api",
	})

	MustRegisterBundle("linux", "Essential Linux system tools", []string{
		"curl",
		"dns_lookup",
		"network_utils",
		"process_manager",
		"disk_usage",
		"system_info",
		"archive",
		"log_viewer",
	})

	MustRegisterBundle("all", "All available built-in tools", []string{
		"calculator",
		"secret_redactor",
		"hash_generator",
		"json_parser",
		"regex_matcher",
		"text_processor",
		"document_generator",
		"pdf_generator",
		"google_docs_manager",
		"document_preview",
		"base64_codec",
		"timestamp_converter",
		"uuid_generator",
		"url_parser",
		"git_repo",
		"code_search",
		"diff_generator",
		"http_client",
		"web_search",
		"web_scraper",
		"curl",
		"dns_lookup",
		"network_utils",
		"shell_command",
		"file_system",
		"env_vars",
		"memory_store",
		"tmpdir",
		"process_manager",
		"disk_usage",
		"system_info",
		"archive",
		"log_viewer",
		"docker",
		"docker_compose",
		"kubectl",
		"k3s",
		"cron_manager",
		"todo_manager",
		"self_api",
	})
}
