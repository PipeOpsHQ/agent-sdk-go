package api

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type providerEnvField struct {
	Key    string `json:"key"`
	Secret bool   `json:"secret"`
}

var providerEnvFields = []providerEnvField{
	{Key: "AGENT_PROVIDER", Secret: false},
	{Key: "GEMINI_API_KEY", Secret: true},
	{Key: "GEMINI_MODEL", Secret: false},
	{Key: "OPENAI_API_KEY", Secret: true},
	{Key: "OPENAI_MODEL", Secret: false},
	{Key: "OPENAI_BASE_URL", Secret: false},
	{Key: "ANTHROPIC_API_KEY", Secret: true},
	{Key: "ANTHROPIC_MODEL", Secret: false},
	{Key: "ANTHROPIC_BASE_URL", Secret: false},
	{Key: "OLLAMA_API_KEY", Secret: true},
	{Key: "OLLAMA_MODEL", Secret: false},
	{Key: "OLLAMA_BASE_URL", Secret: false},
	{Key: "AZURE_OPENAI_API_KEY", Secret: true},
	{Key: "AZURE_OPENAI_ENDPOINT", Secret: false},
	{Key: "AZURE_OPENAI_DEPLOYMENT", Secret: false},
	{Key: "AZURE_OPENAI_MODEL", Secret: false},
	{Key: "AZURE_OPENAI_API_VERSION", Secret: false},
}

func providerEnvMeta() map[string]providerEnvField {
	out := make(map[string]providerEnvField, len(providerEnvFields))
	for _, field := range providerEnvFields {
		out[field.Key] = field
	}
	return out
}

func providerEnvFilePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return "./.ai-agent/provider_env.json"
	}
	return path
}

func loadProviderEnvFile(path string) (map[string]string, error) {
	path = providerEnvFilePath(path)
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, nil
		}
		return nil, fmt.Errorf("read provider env file: %w", err)
	}
	var values map[string]string
	if err := json.Unmarshal(content, &values); err != nil {
		return nil, fmt.Errorf("decode provider env file: %w", err)
	}
	if values == nil {
		values = map[string]string{}
	}
	return values, nil
}

func saveProviderEnvFile(path string, values map[string]string) error {
	path = providerEnvFilePath(path)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create provider env dir: %w", err)
	}
	content, err := json.MarshalIndent(values, "", "  ")
	if err != nil {
		return fmt.Errorf("encode provider env file: %w", err)
	}
	if err := os.WriteFile(path, content, 0600); err != nil {
		return fmt.Errorf("write provider env file: %w", err)
	}
	return nil
}

func applyProviderEnv(values map[string]string) {
	for _, field := range providerEnvFields {
		value := strings.TrimSpace(values[field.Key])
		if value == "" {
			_ = os.Unsetenv(field.Key)
			continue
		}
		_ = os.Setenv(field.Key, value)
	}
}

func LoadProviderEnvFile(path string) error {
	values, err := loadProviderEnvFile(path)
	if err != nil {
		return err
	}
	applyProviderEnv(values)
	return nil
}

func providerEnvSnapshot() map[string]string {
	out := make(map[string]string, len(providerEnvFields))
	for _, field := range providerEnvFields {
		out[field.Key] = strings.TrimSpace(os.Getenv(field.Key))
	}
	return out
}

type providerEnvResponse struct {
	Values     map[string]string  `json:"values"`
	Configured map[string]bool    `json:"configured"`
	Fields     []providerEnvField `json:"fields"`
	Keys       []string           `json:"keys"`
}

func buildProviderEnvResponse() providerEnvResponse {
	snapshot := providerEnvSnapshot()
	values := map[string]string{}
	configured := map[string]bool{}
	keys := make([]string, 0, len(providerEnvFields))
	for _, field := range providerEnvFields {
		value := strings.TrimSpace(snapshot[field.Key])
		configured[field.Key] = value != ""
		if !field.Secret {
			values[field.Key] = value
		}
		keys = append(keys, field.Key)
	}
	sort.Strings(keys)
	return providerEnvResponse{Values: values, Configured: configured, Fields: providerEnvFields, Keys: keys}
}
