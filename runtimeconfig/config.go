package runtimeconfig

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	Workflow     string   `json:"workflow"`
	WorkflowFile string   `json:"workflowFile"`
	SystemPrompt string   `json:"systemPrompt"`
	Tools        []string `json:"tools"`
}

func Load(path string) (Config, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return Config{}, fmt.Errorf("config path is required")
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return Config{}, fmt.Errorf("failed to resolve config path: %w", err)
	}
	data, err := os.ReadFile(absPath)
	if err != nil {
		return Config{}, fmt.Errorf("failed to read config file %q: %w", absPath, err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("failed to decode config file %q as JSON: %w", absPath, err)
	}

	cfg.Workflow = strings.TrimSpace(cfg.Workflow)
	cfg.WorkflowFile = strings.TrimSpace(cfg.WorkflowFile)
	cfg.SystemPrompt = strings.TrimSpace(cfg.SystemPrompt)
	cleanTools := make([]string, 0, len(cfg.Tools))
	for _, t := range cfg.Tools {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		cleanTools = append(cleanTools, t)
	}
	cfg.Tools = cleanTools
	return cfg, nil
}
