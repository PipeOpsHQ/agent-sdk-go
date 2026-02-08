package factory

import (
	"context"
	"testing"
)

func TestFromEnv_OpenAI(t *testing.T) {
	t.Setenv("AGENT_PROVIDER", "openai")
	t.Setenv("OPENAI_API_KEY", "test-openai-key")
	t.Setenv("OPENAI_MODEL", "gpt-4o-mini")

	p, err := FromEnv(context.Background())
	if err != nil {
		t.Fatalf("FromEnv returned error: %v", err)
	}
	if p.Name() != "openai" {
		t.Fatalf("expected openai provider, got %q", p.Name())
	}
}

func TestFromEnv_Anthropic(t *testing.T) {
	t.Setenv("AGENT_PROVIDER", "anthropic")
	t.Setenv("ANTHROPIC_API_KEY", "test-anthropic-key")
	t.Setenv("ANTHROPIC_MODEL", "claude-3-5-sonnet-latest")

	p, err := FromEnv(context.Background())
	if err != nil {
		t.Fatalf("FromEnv returned error: %v", err)
	}
	if p.Name() != "anthropic" {
		t.Fatalf("expected anthropic provider, got %q", p.Name())
	}
}

func TestFromEnv_UnsupportedProvider(t *testing.T) {
	t.Setenv("AGENT_PROVIDER", "unknown-provider")

	if _, err := FromEnv(context.Background()); err == nil {
		t.Fatalf("expected unsupported provider error")
	}
}

func TestFromEnv_Ollama(t *testing.T) {
	t.Setenv("AGENT_PROVIDER", "ollama")
	t.Setenv("OLLAMA_MODEL", "llama3.1:8b")
	t.Setenv("OLLAMA_BASE_URL", "http://127.0.0.1:11434")

	p, err := FromEnv(context.Background())
	if err != nil {
		t.Fatalf("FromEnv returned error: %v", err)
	}
	if p.Name() != "ollama" {
		t.Fatalf("expected ollama provider, got %q", p.Name())
	}
}

func TestFromEnv_AzureOpenAI(t *testing.T) {
	t.Setenv("AGENT_PROVIDER", "azureopenai")
	t.Setenv("AZURE_OPENAI_API_KEY", "test-azure-key")
	t.Setenv("AZURE_OPENAI_ENDPOINT", "https://example.openai.azure.com")
	t.Setenv("AZURE_OPENAI_DEPLOYMENT", "gpt-4o-mini")
	t.Setenv("AZURE_OPENAI_API_VERSION", "2024-10-21")

	p, err := FromEnv(context.Background())
	if err != nil {
		t.Fatalf("FromEnv returned error: %v", err)
	}
	if p.Name() != "azureopenai" {
		t.Fatalf("expected azureopenai provider, got %q", p.Name())
	}
}
