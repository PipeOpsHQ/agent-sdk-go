package runtimeconfig

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_Config(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.json")
	content := `{"workflow":"basic","workflowFile":"./workflow.json","systemPrompt":"custom","tools":["@default","calculator"]}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.Workflow != "basic" {
		t.Fatalf("unexpected workflow: %q", cfg.Workflow)
	}
	if cfg.WorkflowFile != "./workflow.json" {
		t.Fatalf("unexpected workflow file: %q", cfg.WorkflowFile)
	}
	if cfg.SystemPrompt != "custom" {
		t.Fatalf("unexpected prompt: %q", cfg.SystemPrompt)
	}
	if len(cfg.Tools) != 2 {
		t.Fatalf("unexpected tools: %#v", cfg.Tools)
	}
}

func TestLoad_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte("{bad"), 0o644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}
	if _, err := Load(path); err == nil {
		t.Fatalf("expected parse error")
	}
}
