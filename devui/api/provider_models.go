package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
)

type providerModelsResponse struct {
	Provider string   `json:"provider"`
	ModelKey string   `json:"modelKey"`
	Current  string   `json:"current"`
	Models   []string `json:"models"`
	Source   string   `json:"source"`
	Error    string   `json:"error,omitempty"`
}

func providerModelEnvKey(provider string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "openai":
		return "OPENAI_MODEL"
	case "anthropic":
		return "ANTHROPIC_MODEL"
	case "azureopenai":
		return "AZURE_OPENAI_MODEL"
	case "ollama":
		return "OLLAMA_MODEL"
	default:
		return "GEMINI_MODEL"
	}
}

func listProviderModels(provider string) providerModelsResponse {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "" {
		provider = strings.ToLower(strings.TrimSpace(os.Getenv("AGENT_PROVIDER")))
	}
	if provider == "" {
		provider = "gemini"
	}

	key := providerModelEnvKey(provider)
	resp := providerModelsResponse{
		Provider: provider,
		ModelKey: key,
		Current:  strings.TrimSpace(os.Getenv(key)),
	}

	models, src, err := fetchProviderModels(provider)
	if err != nil {
		resp.Source = "fallback"
		resp.Error = err.Error()
		resp.Models = fallbackProviderModels(provider)
	} else {
		resp.Source = src
		resp.Models = models
	}

	if resp.Current != "" && !containsString(resp.Models, resp.Current) {
		resp.Models = append([]string{resp.Current}, resp.Models...)
	}
	resp.Models = uniqueSorted(resp.Models)
	return resp
}

func fetchProviderModels(provider string) ([]string, string, error) {
	switch provider {
	case "openai":
		return fetchOpenAIModels()
	case "gemini":
		return fetchGeminiModels()
	case "ollama":
		return fetchOllamaModels()
	case "anthropic":
		return nil, "fallback", fmt.Errorf("anthropic model listing API not configured")
	case "azureopenai":
		return nil, "fallback", fmt.Errorf("azure openai model listing API not configured")
	default:
		return nil, "fallback", fmt.Errorf("unsupported provider %q", provider)
	}
}

func fetchOpenAIModels() ([]string, string, error) {
	apiKey := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	if apiKey == "" {
		return nil, "fallback", fmt.Errorf("OPENAI_API_KEY not configured")
	}
	base := strings.TrimSpace(os.Getenv("OPENAI_BASE_URL"))
	if base == "" {
		base = "https://api.openai.com"
	}
	endpoint := strings.TrimRight(base, "/") + "/v1/models"
	req, _ := http.NewRequest(http.MethodGet, endpoint, nil)
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 20 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return nil, "fallback", err
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 2*1024*1024))
	if res.StatusCode >= 300 {
		return nil, "fallback", fmt.Errorf("openai HTTP %d", res.StatusCode)
	}
	var parsed struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, "fallback", err
	}
	out := make([]string, 0, len(parsed.Data))
	for _, d := range parsed.Data {
		if strings.TrimSpace(d.ID) != "" {
			out = append(out, d.ID)
		}
	}
	return uniqueSorted(out), "api", nil
}

func fetchGeminiModels() ([]string, string, error) {
	apiKey := strings.TrimSpace(os.Getenv("GEMINI_API_KEY"))
	if apiKey == "" {
		return nil, "fallback", fmt.Errorf("GEMINI_API_KEY not configured")
	}
	endpoint := "https://generativelanguage.googleapis.com/v1beta/models?key=" + url.QueryEscape(apiKey)
	client := &http.Client{Timeout: 20 * time.Second}
	res, err := client.Get(endpoint)
	if err != nil {
		return nil, "fallback", err
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 2*1024*1024))
	if res.StatusCode >= 300 {
		return nil, "fallback", fmt.Errorf("gemini HTTP %d", res.StatusCode)
	}
	var parsed struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, "fallback", err
	}
	out := make([]string, 0, len(parsed.Models))
	for _, m := range parsed.Models {
		name := strings.TrimPrefix(strings.TrimSpace(m.Name), "models/")
		if name != "" {
			out = append(out, name)
		}
	}
	return uniqueSorted(out), "api", nil
}

func fetchOllamaModels() ([]string, string, error) {
	base := strings.TrimSpace(os.Getenv("OLLAMA_BASE_URL"))
	if base == "" {
		base = "http://127.0.0.1:11434"
	}
	endpoint := strings.TrimRight(base, "/") + "/api/tags"
	client := &http.Client{Timeout: 10 * time.Second}
	res, err := client.Get(endpoint)
	if err != nil {
		return nil, "fallback", err
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 2*1024*1024))
	if res.StatusCode >= 300 {
		return nil, "fallback", fmt.Errorf("ollama HTTP %d", res.StatusCode)
	}
	var parsed struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, "fallback", err
	}
	out := make([]string, 0, len(parsed.Models))
	for _, m := range parsed.Models {
		if strings.TrimSpace(m.Name) != "" {
			out = append(out, strings.TrimSpace(m.Name))
		}
	}
	return uniqueSorted(out), "api", nil
}

func fallbackProviderModels(provider string) []string {
	switch provider {
	case "openai":
		return []string{"gpt-4o-mini", "gpt-4.1-mini", "gpt-4.1", "o3-mini"}
	case "anthropic":
		return []string{"claude-3-5-haiku-latest", "claude-3-5-sonnet-latest", "claude-3-7-sonnet-latest"}
	case "azureopenai":
		return []string{"gpt-4o-mini", "gpt-4.1-mini", "gpt-4.1"}
	case "ollama":
		return []string{"llama3.1", "qwen2.5", "mistral"}
	default:
		return []string{"gemini-2.5-flash", "gemini-2.5-pro", "gemini-2.0-flash"}
	}
}

func uniqueSorted(values []string) []string {
	set := map[string]struct{}{}
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v != "" {
			set[v] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for v := range set {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

func containsString(values []string, target string) bool {
	target = strings.TrimSpace(target)
	for _, v := range values {
		if strings.TrimSpace(v) == target {
			return true
		}
	}
	return false
}
