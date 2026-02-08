package tools

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestSecretRedactor(t *testing.T) {
	tool := NewSecretRedactor()

	tests := []struct {
		name         string
		input        string
		wantRedacted bool
		wantSecrets  []string
	}{
		{
			name:         "AWS Access Key",
			input:        "My key is AKIAIOSFODNN7EXAMPLE",
			wantRedacted: true,
			wantSecrets:  []string{"AWS Access Key"},
		},
		{
			name:         "GitHub Token",
			input:        "token: ghp_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
			wantRedacted: true,
			wantSecrets:  []string{"GitHub Token"},
		},
		{
			name:         "JWT Token",
			input:        "Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U",
			wantRedacted: true,
			wantSecrets:  []string{"JWT"},
		},
		{
			name:         "No secrets",
			input:        "This is a normal text without any secrets",
			wantRedacted: false,
		},
		{
			name:         "Password in config",
			input:        "password=supersecret123",
			wantRedacted: true,
			wantSecrets:  []string{"Password"},
		},
		{
			name:         "Short password",
			input:        "pass=abc",
			wantRedacted: true,
			wantSecrets:  []string{"Password"},
		},
		{
			name:         "Password in URL",
			input:        "postgres://admin:s3cretP@ss@db.example.com:5432/mydb",
			wantRedacted: true,
			wantSecrets:  []string{"Password in URL"},
		},
		{
			name:         "MongoDB connection string",
			input:        "mongodb+srv://user:hunter2@cluster0.abc.mongodb.net/test",
			wantRedacted: true,
			wantSecrets:  []string{"DB Connection String"},
		},
		{
			name:         "Redis connection string",
			input:        "redis://default:mypassword@redis.example.com:6379",
			wantRedacted: true,
			wantSecrets:  []string{"DB Connection String"},
		},
		{
			name:         "SendGrid Key",
			input:        "SENDGRID_API_KEY=" + "SG" + ".abcdefghijklmnopqrstuv.ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopq",
			wantRedacted: true,
			wantSecrets:  []string{"SendGrid Key"},
		},
		{
			name:         "Stripe secret key",
			input:        "sk_" + "live_abcdefghijklmnopqrstuvwx",
			wantRedacted: true,
			wantSecrets:  []string{"Stripe Key"},
		},
		{
			name:         "Generic secret assignment",
			input:        "client_secret=abcdef0123456789abcdef",
			wantRedacted: true,
		},
		{
			name:         "Private key header",
			input:        "-----BEGIN RSA PRIVATE KEY-----",
			wantRedacted: true,
			wantSecrets:  []string{"Private Key"},
		},
		{
			name:         "Encrypted private key header",
			input:        "-----BEGIN ENCRYPTED PRIVATE KEY-----",
			wantRedacted: true,
			wantSecrets:  []string{"Private Key"},
		},
		{
			name:         "Password with colon separator",
			input:        "password: MyStr0ngP@ss!",
			wantRedacted: true,
			wantSecrets:  []string{"Password"},
		},
		{
			name:         "Password in quotes",
			input:        `DB_PASSWORD="hunter2"`,
			wantRedacted: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args, _ := json.Marshal(map[string]string{"text": tt.input})
			result, err := tool.Execute(context.Background(), args)
			if err != nil {
				t.Fatalf("Execute failed: %v", err)
			}

			m := result.(RedactionResult)
			if tt.wantRedacted {
				if m.RedactionCount == 0 {
					t.Error("expected redactions but got none")
				}
				if !strings.Contains(m.RedactedText, "[REDACTED]") {
					t.Error("expected [REDACTED] in output")
				}
			} else {
				if m.RedactionCount > 0 {
					t.Errorf("expected no redactions, got %d", m.RedactionCount)
				}
			}
		})
	}
}

func TestHashGenerator(t *testing.T) {
	tool := NewHashGenerator()

	tests := []struct {
		algorithm   string
		input       string
		expectedLen int
	}{
		{"md5", "hello", 32},
		{"sha1", "hello", 40},
		{"sha256", "hello", 64},
		{"sha512", "hello", 128},
	}

	for _, tt := range tests {
		t.Run(tt.algorithm, func(t *testing.T) {
			args, _ := json.Marshal(map[string]string{
				"input":     tt.input,
				"algorithm": tt.algorithm,
			})
			result, err := tool.Execute(context.Background(), args)
			if err != nil {
				t.Fatalf("Execute failed: %v", err)
			}

			m := result.(map[string]any)
			hash := m["hash"].(string)
			if len(hash) != tt.expectedLen {
				t.Errorf("expected hash length %d, got %d", tt.expectedLen, len(hash))
			}
		})
	}
}

func TestJSONParser(t *testing.T) {
	tool := NewJSONParser()

	t.Run("valid JSON", func(t *testing.T) {
		args, _ := json.Marshal(map[string]string{
			"json": `{"user": {"name": "John", "age": 30}}`,
		})
		result, err := tool.Execute(context.Background(), args)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		m := result.(map[string]any)
		if !m["valid"].(bool) {
			t.Error("expected valid JSON")
		}
	})

	t.Run("query JSON", func(t *testing.T) {
		args, _ := json.Marshal(map[string]string{
			"json":  `{"users": [{"name": "Alice"}, {"name": "Bob"}]}`,
			"query": "users.0.name",
		})
		result, err := tool.Execute(context.Background(), args)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		m := result.(map[string]any)
		if m["queryResult"] != "Alice" {
			t.Errorf("expected Alice, got %v", m["queryResult"])
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		args, _ := json.Marshal(map[string]string{
			"json": `{invalid}`,
		})
		result, err := tool.Execute(context.Background(), args)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		m := result.(map[string]any)
		if m["valid"].(bool) {
			t.Error("expected invalid JSON")
		}
	})
}

func TestBase64Codec(t *testing.T) {
	tool := NewBase64Codec()

	t.Run("encode", func(t *testing.T) {
		args, _ := json.Marshal(map[string]any{
			"input":     "Hello, World!",
			"operation": "encode",
		})
		result, err := tool.Execute(context.Background(), args)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		m := result.(map[string]any)
		if m["result"] != "SGVsbG8sIFdvcmxkIQ==" {
			t.Errorf("unexpected encoded result: %v", m["result"])
		}
	})

	t.Run("decode", func(t *testing.T) {
		args, _ := json.Marshal(map[string]any{
			"input":     "SGVsbG8sIFdvcmxkIQ==",
			"operation": "decode",
		})
		result, err := tool.Execute(context.Background(), args)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		m := result.(map[string]any)
		if m["result"] != "Hello, World!" {
			t.Errorf("unexpected decoded result: %v", m["result"])
		}
	})

	t.Run("url-safe encode", func(t *testing.T) {
		args, _ := json.Marshal(map[string]any{
			"input":     "test+value/here",
			"operation": "encode",
			"urlSafe":   true,
		})
		result, err := tool.Execute(context.Background(), args)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		m := result.(map[string]any)
		encoded := m["result"].(string)
		if strings.Contains(encoded, "+") || strings.Contains(encoded, "/") {
			t.Errorf("URL-safe encoding should not contain + or /: %v", encoded)
		}
	})
}

func TestTimestampConverter(t *testing.T) {
	tool := NewTimestampConverter()

	t.Run("unix to all formats", func(t *testing.T) {
		args, _ := json.Marshal(map[string]any{
			"input":    "1704067200",
			"fromType": "unix",
		})
		result, err := tool.Execute(context.Background(), args)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		m := result.(map[string]any)
		if m["rfc3339"] == nil {
			t.Error("expected rfc3339 in result")
		}
	})

	t.Run("rfc3339 to unix", func(t *testing.T) {
		args, _ := json.Marshal(map[string]any{
			"input":    "2024-01-01T00:00:00Z",
			"fromType": "rfc3339",
			"toType":   "unix",
		})
		result, err := tool.Execute(context.Background(), args)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		m := result.(map[string]any)
		if m["result"].(int64) != 1704067200 {
			t.Errorf("unexpected unix timestamp: %v", m["result"])
		}
	})
}

func TestUUIDGenerator(t *testing.T) {
	tool := NewUUIDGenerator()

	t.Run("generate single UUID", func(t *testing.T) {
		args, _ := json.Marshal(map[string]any{})
		result, err := tool.Execute(context.Background(), args)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		m := result.(map[string]any)
		uuid := m["uuid"].(string)
		if !IsValidUUID(uuid) {
			t.Errorf("invalid UUID format: %s", uuid)
		}
	})

	t.Run("generate multiple UUIDs", func(t *testing.T) {
		args, _ := json.Marshal(map[string]any{
			"count": 5,
		})
		result, err := tool.Execute(context.Background(), args)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		m := result.(map[string]any)
		uuids := m["uuids"].([]string)
		if len(uuids) != 5 {
			t.Errorf("expected 5 UUIDs, got %d", len(uuids))
		}
	})

	t.Run("no-dashes format", func(t *testing.T) {
		args, _ := json.Marshal(map[string]any{
			"format": "no-dashes",
		})
		result, err := tool.Execute(context.Background(), args)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		m := result.(map[string]any)
		uuid := m["uuid"].(string)
		if strings.Contains(uuid, "-") {
			t.Errorf("UUID should not contain dashes: %s", uuid)
		}
	})
}

func TestRegexMatcher(t *testing.T) {
	tool := NewRegexMatcher()

	t.Run("test operation", func(t *testing.T) {
		args, _ := json.Marshal(map[string]any{
			"text":      "hello@example.com",
			"pattern":   `\w+@\w+\.\w+`,
			"operation": "test",
		})
		result, err := tool.Execute(context.Background(), args)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		m := result.(map[string]any)
		if !m["matches"].(bool) {
			t.Error("expected pattern to match")
		}
	})

	t.Run("find_all operation", func(t *testing.T) {
		args, _ := json.Marshal(map[string]any{
			"text":      "cat bat rat",
			"pattern":   `\w+at`,
			"operation": "find_all",
		})
		result, err := tool.Execute(context.Background(), args)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		m := result.(map[string]any)
		if m["count"].(int) != 3 {
			t.Errorf("expected 3 matches, got %v", m["count"])
		}
	})

	t.Run("replace operation", func(t *testing.T) {
		args, _ := json.Marshal(map[string]any{
			"text":      "hello world",
			"pattern":   `world`,
			"operation": "replace",
			"replace":   "universe",
		})
		result, err := tool.Execute(context.Background(), args)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		m := result.(map[string]any)
		if m["result"] != "hello universe" {
			t.Errorf("unexpected result: %v", m["result"])
		}
	})
}

func TestURLParser(t *testing.T) {
	tool := NewURLParser()

	t.Run("parse URL", func(t *testing.T) {
		args, _ := json.Marshal(map[string]any{
			"url": "https://example.com:8080/path/to/resource?foo=bar&baz=qux#section",
		})
		result, err := tool.Execute(context.Background(), args)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		m := result.(map[string]any)
		if m["scheme"] != "https" {
			t.Errorf("expected scheme https, got %v", m["scheme"])
		}
		if m["hostname"] != "example.com" {
			t.Errorf("expected hostname example.com, got %v", m["hostname"])
		}
		if m["port"] != "8080" {
			t.Errorf("expected port 8080, got %v", m["port"])
		}
		if m["fragment"] != "section" {
			t.Errorf("expected fragment section, got %v", m["fragment"])
		}
	})

	t.Run("validate URL", func(t *testing.T) {
		args, _ := json.Marshal(map[string]any{
			"url":       "https://example.com",
			"operation": "validate",
		})
		result, err := tool.Execute(context.Background(), args)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		m := result.(map[string]any)
		if !m["valid"].(bool) {
			t.Error("expected valid URL")
		}
	})

	t.Run("encode URL", func(t *testing.T) {
		args, _ := json.Marshal(map[string]any{
			"url":       "hello world & foo=bar",
			"operation": "encode",
		})
		result, err := tool.Execute(context.Background(), args)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		m := result.(map[string]any)
		encoded := m["result"].(string)
		if strings.Contains(encoded, " ") || strings.Contains(encoded, "&") {
			t.Errorf("encoded URL should not contain spaces or &: %v", encoded)
		}
	})
}

func TestGitRepo(t *testing.T) {
	tool := NewGitRepo()

	t.Run("extract repo name", func(t *testing.T) {
		tests := []struct {
			url      string
			expected string
		}{
			{"https://github.com/owner/repo.git", "repo"},
			{"https://github.com/owner/repo", "repo"},
			{"git@github.com:owner/repo.git", "repo"},
			{"https://gitlab.com/group/subgroup/project.git", "project"},
		}

		for _, tt := range tests {
			name := extractRepoName(tt.url)
			if name != tt.expected {
				t.Errorf("extractRepoName(%q) = %q, want %q", tt.url, name, tt.expected)
			}
		}
	})

	t.Run("clone public repo", func(t *testing.T) {
		// Skip in CI or if no git available
		if _, err := exec.LookPath("git"); err != nil {
			t.Skip("git not available")
		}

		args, _ := json.Marshal(map[string]any{
			"url":       "https://github.com/octocat/Hello-World.git",
			"operation": "clone",
			"depth":     1,
		})

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		result, err := tool.Execute(ctx, args)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		m := result.(*GitRepoResult)
		if !m.Success {
			t.Errorf("clone failed: %s", m.Error)
		}
		if m.LocalPath == "" {
			t.Error("expected localPath in result")
		}
		if m.RepoName != "Hello-World" {
			t.Errorf("expected repoName Hello-World, got %s", m.RepoName)
		}
	})

	t.Run("get repo info", func(t *testing.T) {
		if _, err := exec.LookPath("git"); err != nil {
			t.Skip("git not available")
		}

		args, _ := json.Marshal(map[string]any{
			"url":       "https://github.com/octocat/Hello-World.git",
			"operation": "get_info",
		})

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		result, err := tool.Execute(ctx, args)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		m := result.(map[string]any)
		if !m["success"].(bool) {
			t.Errorf("get_info failed: %v", m["error"])
		}
		if m["commit"] == nil {
			t.Error("expected commit in result")
		}
	})

	t.Run("list files", func(t *testing.T) {
		if _, err := exec.LookPath("git"); err != nil {
			t.Skip("git not available")
		}

		args, _ := json.Marshal(map[string]any{
			"url":       "https://github.com/octocat/Hello-World.git",
			"operation": "list_files",
		})

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		result, err := tool.Execute(ctx, args)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		m := result.(map[string]any)
		if !m["success"].(bool) {
			t.Errorf("list_files failed: %v", m["error"])
		}
		files := m["files"].([]string)
		if len(files) == 0 {
			t.Error("expected files in result")
		}
	})

	t.Run("read single file", func(t *testing.T) {
		if _, err := exec.LookPath("git"); err != nil {
			t.Skip("git not available")
		}

		args, _ := json.Marshal(map[string]any{
			"url":       "https://github.com/octocat/Hello-World.git",
			"operation": "read_file",
			"path":      "README",
		})

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		result, err := tool.Execute(ctx, args)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		m := result.(map[string]any)
		if !m["success"].(bool) {
			t.Errorf("read_file failed: %v", m["error"])
		}
		content := m["content"].(string)
		if content == "" {
			t.Error("expected content in result")
		}
	})

	// Cleanup after tests
	t.Cleanup(func() {
		CleanupRepos()
	})
}
