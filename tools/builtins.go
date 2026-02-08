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
		"web_scraper",
	})

	MustRegisterBundle("system", "System interaction tools", []string{
		"shell_command",
		"file_system",
		"env_vars",
	})

	MustRegisterBundle("memory", "State and memory tools", []string{
		"memory_store",
	})

	MustRegisterBundle("text", "Text processing tools", []string{
		"text_processor",
		"json_parser",
		"regex_matcher",
	})

	MustRegisterBundle("all", "All available built-in tools", []string{
		"calculator",
		"secret_redactor",
		"hash_generator",
		"json_parser",
		"regex_matcher",
		"text_processor",
		"base64_codec",
		"timestamp_converter",
		"uuid_generator",
		"url_parser",
		"git_repo",
		"code_search",
		"diff_generator",
		"http_client",
		"web_scraper",
		"shell_command",
		"file_system",
		"env_vars",
		"memory_store",
	})
}
