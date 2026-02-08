package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHTTPClient(t *testing.T) {
	tool := NewHTTPClient()

	t.Run("GET request", func(t *testing.T) {
		args, _ := json.Marshal(map[string]any{
			"url":    "https://httpbin.org/get",
			"method": "GET",
		})

		result, err := tool.Execute(context.Background(), args)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resp := result.(*HTTPResponse)
		if resp.StatusCode != 200 {
			t.Errorf("expected status 200, got %d", resp.StatusCode)
		}
	})

	t.Run("POST request with body", func(t *testing.T) {
		args, _ := json.Marshal(map[string]any{
			"url":    "https://httpbin.org/post",
			"method": "POST",
			"body":   `{"test": "data"}`,
			"headers": map[string]string{
				"Content-Type": "application/json",
			},
		})

		result, err := tool.Execute(context.Background(), args)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resp := result.(*HTTPResponse)
		if resp.StatusCode != 200 {
			t.Errorf("expected status 200, got %d", resp.StatusCode)
		}
	})
}

func TestShellCommand(t *testing.T) {
	tool := NewShellCommand()

	t.Run("echo command", func(t *testing.T) {
		args, _ := json.Marshal(map[string]any{
			"command": "echo",
			"args":    []string{"hello world"},
		})

		result, err := tool.Execute(context.Background(), args)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		sr := result.(*ShellResult)
		if !sr.Success {
			t.Errorf("command failed: %s", sr.Error)
		}
		if !strings.Contains(sr.Stdout, "hello world") {
			t.Errorf("expected 'hello world' in stdout, got: %s", sr.Stdout)
		}
	})

	t.Run("blocked command", func(t *testing.T) {
		args, _ := json.Marshal(map[string]any{
			"command": "rm",
			"args":    []string{"-rf", "/"},
		})

		result, err := tool.Execute(context.Background(), args)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		sr := result.(*ShellResult)
		if sr.Success {
			t.Error("expected command to be blocked")
		}
		if !strings.Contains(sr.Error, "blocked") {
			t.Errorf("expected blocked error, got: %s", sr.Error)
		}
	})
}

func TestFileSystem(t *testing.T) {
	tool := NewFileSystem()
	tempDir := t.TempDir()

	t.Run("write and read file", func(t *testing.T) {
		testFile := filepath.Join(tempDir, "test.txt")
		content := "Hello, World!"

		// Write
		args, _ := json.Marshal(map[string]any{
			"operation": "write",
			"path":      testFile,
			"content":   content,
		})

		result, err := tool.Execute(context.Background(), args)
		if err != nil {
			t.Fatalf("Write failed: %v", err)
		}

		fr := result.(*FileResult)
		if !fr.Success {
			t.Fatalf("write failed: %s", fr.Error)
		}

		// Read
		args, _ = json.Marshal(map[string]any{
			"operation": "read",
			"path":      testFile,
		})

		result, err = tool.Execute(context.Background(), args)
		if err != nil {
			t.Fatalf("Read failed: %v", err)
		}

		fr = result.(*FileResult)
		if !fr.Success {
			t.Fatalf("read failed: %s", fr.Error)
		}

		data := fr.Data.(map[string]any)
		if data["content"] != content {
			t.Errorf("expected %q, got %q", content, data["content"])
		}
	})

	t.Run("list directory", func(t *testing.T) {
		args, _ := json.Marshal(map[string]any{
			"operation": "list",
			"path":      tempDir,
		})

		result, err := tool.Execute(context.Background(), args)
		if err != nil {
			t.Fatalf("List failed: %v", err)
		}

		fr := result.(*FileResult)
		if !fr.Success {
			t.Fatalf("list failed: %s", fr.Error)
		}
	})

	t.Run("file exists", func(t *testing.T) {
		testFile := filepath.Join(tempDir, "test.txt")

		args, _ := json.Marshal(map[string]any{
			"operation": "exists",
			"path":      testFile,
		})

		result, err := tool.Execute(context.Background(), args)
		if err != nil {
			t.Fatalf("Exists failed: %v", err)
		}

		fr := result.(*FileResult)
		data := fr.Data.(map[string]any)
		if !data["exists"].(bool) {
			t.Error("expected file to exist")
		}
	})
}

func TestCodeSearch(t *testing.T) {
	tool := NewCodeSearch()
	tempDir := t.TempDir()

	// Create test file
	testFile := filepath.Join(tempDir, "test.go")
	content := `package main

func main() {
	println("hello")
}

func helper() {
	// do something
}
`
	os.WriteFile(testFile, []byte(content), 0644)

	t.Run("text search", func(t *testing.T) {
		args, _ := json.Marshal(map[string]any{
			"path":  tempDir,
			"query": "println",
		})

		result, err := tool.Execute(context.Background(), args)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		csr := result.(*CodeSearchResponse)
		if !csr.Success {
			t.Fatalf("search failed: %s", csr.Error)
		}
		if csr.TotalCount == 0 {
			t.Error("expected at least one result")
		}
	})

	t.Run("symbol search", func(t *testing.T) {
		args, _ := json.Marshal(map[string]any{
			"path":  tempDir,
			"query": "helper",
			"type":  "definition",
		})

		result, err := tool.Execute(context.Background(), args)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		csr := result.(*CodeSearchResponse)
		if !csr.Success {
			t.Fatalf("search failed: %s", csr.Error)
		}
	})
}

func TestDiffGenerator(t *testing.T) {
	tool := NewDiffGenerator()

	t.Run("generate diff", func(t *testing.T) {
		args, _ := json.Marshal(map[string]any{
			"operation": "generate",
			"original":  "line1\nline2\nline3",
			"modified":  "line1\nmodified\nline3",
		})

		result, err := tool.Execute(context.Background(), args)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		dr := result.(*DiffResult)
		if !dr.Success {
			t.Fatalf("diff failed: %s", dr.Error)
		}
		if dr.Added == 0 && dr.Removed == 0 {
			t.Error("expected some changes")
		}
	})

	t.Run("analyze diff", func(t *testing.T) {
		args, _ := json.Marshal(map[string]any{
			"operation": "analyze",
			"original":  "a\nb\nc",
			"modified":  "a\nb\nc\nd",
		})

		result, err := tool.Execute(context.Background(), args)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		dr := result.(*DiffResult)
		if !dr.Success {
			t.Fatalf("analyze failed: %s", dr.Error)
		}
		if dr.Added != 1 {
			t.Errorf("expected 1 added line, got %d", dr.Added)
		}
	})
}

func TestEnvVars(t *testing.T) {
	tool := NewEnvVars()

	t.Run("get env var", func(t *testing.T) {
		os.Setenv("TEST_VAR", "test_value")
		defer os.Unsetenv("TEST_VAR")

		args, _ := json.Marshal(map[string]any{
			"operation": "get",
			"name":      "TEST_VAR",
		})

		result, err := tool.Execute(context.Background(), args)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		er := result.(*EnvResult)
		if !er.Success {
			t.Fatalf("get failed: %s", er.Error)
		}

		vars := er.Data["variables"].(map[string]string)
		if vars["TEST_VAR"] != "test_value" {
			t.Errorf("expected test_value, got %s", vars["TEST_VAR"])
		}
	})

	t.Run("list env vars", func(t *testing.T) {
		args, _ := json.Marshal(map[string]any{
			"operation": "list",
		})

		result, err := tool.Execute(context.Background(), args)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		er := result.(*EnvResult)
		if !er.Success {
			t.Fatalf("list failed: %s", er.Error)
		}

		count := er.Data["count"].(int)
		if count == 0 {
			t.Error("expected some env vars")
		}
	})

	t.Run("sensitive var redacted", func(t *testing.T) {
		os.Setenv("API_SECRET_KEY", "supersecret")
		defer os.Unsetenv("API_SECRET_KEY")

		args, _ := json.Marshal(map[string]any{
			"operation": "get",
			"name":      "API_SECRET_KEY",
		})

		result, err := tool.Execute(context.Background(), args)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		er := result.(*EnvResult)
		vars := er.Data["variables"].(map[string]string)
		if vars["API_SECRET_KEY"] != "[REDACTED]" {
			t.Errorf("expected [REDACTED], got %s", vars["API_SECRET_KEY"])
		}
	})
}

func TestMemoryStore(t *testing.T) {
	tool := NewMemoryStore()

	// Clear memory before test
	ClearAllMemory()

	t.Run("set and get", func(t *testing.T) {
		// Set
		args, _ := json.Marshal(map[string]any{
			"operation": "set",
			"key":       "test_key",
			"value":     "test_value",
		})

		result, err := tool.Execute(context.Background(), args)
		if err != nil {
			t.Fatalf("Set failed: %v", err)
		}

		mr := result.(*MemoryResult)
		if !mr.Success {
			t.Fatalf("set failed: %s", mr.Error)
		}

		// Get
		args, _ = json.Marshal(map[string]any{
			"operation": "get",
			"key":       "test_key",
		})

		result, err = tool.Execute(context.Background(), args)
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}

		mr = result.(*MemoryResult)
		if !mr.Success {
			t.Fatalf("get failed: %s", mr.Error)
		}

		if mr.Data["value"] != "test_value" {
			t.Errorf("expected test_value, got %v", mr.Data["value"])
		}
	})

	t.Run("list keys", func(t *testing.T) {
		args, _ := json.Marshal(map[string]any{
			"operation": "list",
		})

		result, err := tool.Execute(context.Background(), args)
		if err != nil {
			t.Fatalf("List failed: %v", err)
		}

		mr := result.(*MemoryResult)
		if !mr.Success {
			t.Fatalf("list failed: %s", mr.Error)
		}

		count := mr.Data["count"].(int)
		if count == 0 {
			t.Error("expected at least one key")
		}
	})

	t.Run("search pattern", func(t *testing.T) {
		args, _ := json.Marshal(map[string]any{
			"operation": "search",
			"pattern":   "test*",
		})

		result, err := tool.Execute(context.Background(), args)
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}

		mr := result.(*MemoryResult)
		if !mr.Success {
			t.Fatalf("search failed: %s", mr.Error)
		}
	})

	// Cleanup
	ClearAllMemory()
}

func TestTextProcessor(t *testing.T) {
	tool := NewTextProcessor()

	t.Run("word count", func(t *testing.T) {
		args, _ := json.Marshal(map[string]any{
			"operation": "word_count",
			"text":      "hello world foo bar",
		})

		result, err := tool.Execute(context.Background(), args)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		tr := result.(*TextResult)
		if !tr.Success {
			t.Fatalf("word_count failed: %s", tr.Error)
		}

		data := tr.Result.(map[string]int)
		if data["words"] != 4 {
			t.Errorf("expected 4 words, got %d", data["words"])
		}
	})

	t.Run("slugify", func(t *testing.T) {
		args, _ := json.Marshal(map[string]any{
			"operation": "slugify",
			"text":      "Hello World! This is a Test",
		})

		result, err := tool.Execute(context.Background(), args)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		tr := result.(*TextResult)
		expected := "hello-world-this-is-a-test"
		if tr.Result != expected {
			t.Errorf("expected %q, got %q", expected, tr.Result)
		}
	})

	t.Run("extract emails", func(t *testing.T) {
		args, _ := json.Marshal(map[string]any{
			"operation": "extract_emails",
			"text":      "Contact us at test@example.com or support@company.org",
		})

		result, err := tool.Execute(context.Background(), args)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		tr := result.(*TextResult)
		emails := tr.Result.([]string)
		if len(emails) != 2 {
			t.Errorf("expected 2 emails, got %d", len(emails))
		}
	})

	t.Run("camelcase", func(t *testing.T) {
		args, _ := json.Marshal(map[string]any{
			"operation": "camelcase",
			"text":      "hello_world_test",
		})

		result, err := tool.Execute(context.Background(), args)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		tr := result.(*TextResult)
		if tr.Result != "helloWorldTest" {
			t.Errorf("expected helloWorldTest, got %q", tr.Result)
		}
	})
}
