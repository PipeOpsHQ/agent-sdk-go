package factory

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/nitrocode/ai-agents/framework/llm"
	anthropicprov "github.com/nitrocode/ai-agents/framework/providers/anthropic"
	azureopenaiprov "github.com/nitrocode/ai-agents/framework/providers/azureopenai"
	geminiprov "github.com/nitrocode/ai-agents/framework/providers/gemini"
	ollamaprov "github.com/nitrocode/ai-agents/framework/providers/ollama"
	openaiprov "github.com/nitrocode/ai-agents/framework/providers/openai"
)

func FromEnv(ctx context.Context) (llm.Provider, error) {
	provider := strings.ToLower(strings.TrimSpace(getenv("AGENT_PROVIDER", "gemini")))
	switch provider {
	case "openai":
		key := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
		if key == "" {
			return nil, fmt.Errorf("OPENAI_API_KEY is required when AGENT_PROVIDER=openai")
		}
		model := getenv("OPENAI_MODEL", "gpt-4o-mini")
		baseURL := strings.TrimSpace(os.Getenv("OPENAI_BASE_URL"))

		opts := []openaiprov.Option{openaiprov.WithModel(model)}
		if baseURL != "" {
			opts = append(opts, openaiprov.WithBaseURL(baseURL))
		}
		return openaiprov.New(key, opts...)

	case "gemini":
		key := strings.TrimSpace(os.Getenv("GEMINI_API_KEY"))
		if key == "" {
			return nil, fmt.Errorf("GEMINI_API_KEY is required when AGENT_PROVIDER=gemini")
		}
		model := getenv("GEMINI_MODEL", "gemini-2.5-flash")
		return geminiprov.New(ctx, key, geminiprov.WithModel(model))

	case "anthropic":
		key := strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY"))
		if key == "" {
			return nil, fmt.Errorf("ANTHROPIC_API_KEY is required when AGENT_PROVIDER=anthropic")
		}
		model := getenv("ANTHROPIC_MODEL", "claude-3-5-sonnet-latest")
		baseURL := strings.TrimSpace(os.Getenv("ANTHROPIC_BASE_URL"))

		opts := []anthropicprov.Option{anthropicprov.WithModel(model)}
		if baseURL != "" {
			opts = append(opts, anthropicprov.WithBaseURL(baseURL))
		}
		return anthropicprov.New(key, opts...)

	case "ollama":
		model := getenv("OLLAMA_MODEL", "llama3.1:8b")
		baseURL := getenv("OLLAMA_BASE_URL", "http://127.0.0.1:11434")
		apiKey := strings.TrimSpace(os.Getenv("OLLAMA_API_KEY"))
		return ollamaprov.New(
			ollamaprov.WithModel(model),
			ollamaprov.WithBaseURL(baseURL),
			ollamaprov.WithAPIKey(apiKey),
		)

	case "azureopenai":
		apiKey := strings.TrimSpace(os.Getenv("AZURE_OPENAI_API_KEY"))
		if apiKey == "" {
			return nil, fmt.Errorf("AZURE_OPENAI_API_KEY is required when AGENT_PROVIDER=azureopenai")
		}
		endpoint := strings.TrimSpace(os.Getenv("AZURE_OPENAI_ENDPOINT"))
		if endpoint == "" {
			return nil, fmt.Errorf("AZURE_OPENAI_ENDPOINT is required when AGENT_PROVIDER=azureopenai")
		}
		deployment := strings.TrimSpace(os.Getenv("AZURE_OPENAI_DEPLOYMENT"))
		if deployment == "" {
			return nil, fmt.Errorf("AZURE_OPENAI_DEPLOYMENT is required when AGENT_PROVIDER=azureopenai")
		}
		model := getenv("AZURE_OPENAI_MODEL", deployment)
		apiVersion := getenv("AZURE_OPENAI_API_VERSION", "2024-10-21")

		return azureopenaiprov.New(
			apiKey,
			azureopenaiprov.WithEndpoint(endpoint),
			azureopenaiprov.WithDeployment(deployment),
			azureopenaiprov.WithModel(model),
			azureopenaiprov.WithAPIVersion(apiVersion),
		)
	}

	return nil, fmt.Errorf("unsupported AGENT_PROVIDER %q (use gemini, openai, anthropic, ollama, or azureopenai)", provider)
}

func getenv(key, fallback string) string {
	val := strings.TrimSpace(os.Getenv(key))
	if val == "" {
		return fallback
	}
	return val
}
