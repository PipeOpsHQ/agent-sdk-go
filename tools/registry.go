package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
)

type Factory func() Tool

type Bundle struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Tools       []string `json:"tools"`
}

type ToolInfo struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

var (
	regMu         sync.RWMutex
	toolFactories = map[string]Factory{}
	toolDescs     = map[string]string{}
	bundles       = map[string]Bundle{}
)

func RegisterTool(name, description string, factory Factory) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("tool name is required")
	}
	if factory == nil {
		return fmt.Errorf("tool factory is required")
	}
	regMu.Lock()
	defer regMu.Unlock()
	if _, exists := toolFactories[name]; exists {
		return fmt.Errorf("tool %q already registered", name)
	}
	toolFactories[name] = factory
	toolDescs[name] = strings.TrimSpace(description)
	return nil
}

func MustRegisterTool(name, description string, factory Factory) {
	if err := RegisterTool(name, description, factory); err != nil {
		panic(err)
	}
}

func RegisterBundle(name, description string, toolNames []string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("bundle name is required")
	}
	cleaned := make([]string, 0, len(toolNames))
	for _, t := range toolNames {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		cleaned = append(cleaned, t)
	}
	if len(cleaned) == 0 {
		return fmt.Errorf("bundle %q has no tools", name)
	}

	regMu.Lock()
	defer regMu.Unlock()
	if _, exists := bundles[name]; exists {
		return fmt.Errorf("bundle %q already registered", name)
	}
	bundles[name] = Bundle{Name: name, Description: strings.TrimSpace(description), Tools: cleaned}
	return nil
}

func MustRegisterBundle(name, description string, toolNames []string) {
	if err := RegisterBundle(name, description, toolNames); err != nil {
		panic(err)
	}
}

func ToolNames() []string {
	regMu.RLock()
	defer regMu.RUnlock()
	out := make([]string, 0, len(toolFactories))
	for n := range toolFactories {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

func BundleNames() []string {
	regMu.RLock()
	defer regMu.RUnlock()
	out := make([]string, 0, len(bundles))
	for n := range bundles {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

func ToolCatalog() []ToolInfo {
	regMu.RLock()
	defer regMu.RUnlock()
	out := make([]ToolInfo, 0, len(toolFactories))
	for name := range toolFactories {
		out = append(out, ToolInfo{
			Name:        name,
			Description: toolDescs[name],
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ToolSchema returns the JSON schema for a single tool by name.
// It instantiates the tool from the factory to read its definition.
func ToolSchema(name string) (map[string]any, bool) {
	regMu.RLock()
	factory, ok := toolFactories[name]
	regMu.RUnlock()
	if !ok {
		return nil, false
	}
	t := factory()
	if t == nil {
		return nil, false
	}
	return t.Definition().JSONSchema, true
}

// ToolSchemas returns name→JSONSchema for all registered tools.
func ToolSchemas() map[string]map[string]any {
	regMu.RLock()
	names := make([]string, 0, len(toolFactories))
	for n := range toolFactories {
		names = append(names, n)
	}
	regMu.RUnlock()

	out := make(map[string]map[string]any, len(names))
	for _, n := range names {
		regMu.RLock()
		factory := toolFactories[n]
		regMu.RUnlock()
		t := factory()
		if t != nil {
			out[n] = t.Definition().JSONSchema
		}
	}
	return out
}

func BundleCatalog() []Bundle {
	regMu.RLock()
	defer regMu.RUnlock()
	out := make([]Bundle, 0, len(bundles))
	for _, bundle := range bundles {
		clone := Bundle{
			Name:        bundle.Name,
			Description: bundle.Description,
			Tools:       append([]string(nil), bundle.Tools...),
		}
		out = append(out, clone)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func BuildSelection(selection []string) ([]Tool, error) {
	names, err := expandSelection(selection)
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		return nil, nil
	}

	regMu.RLock()
	defer regMu.RUnlock()
	out := make([]Tool, 0, len(names))
	for _, name := range names {
		factory, ok := toolFactories[name]
		if !ok {
			return nil, fmt.Errorf("unknown tool %q", name)
		}
		tool := factory()
		if tool == nil {
			return nil, fmt.Errorf("tool %q factory returned nil", name)
		}
		out = append(out, tool)
	}
	return out, nil
}

func expandSelection(selection []string) ([]string, error) {
	regMu.RLock()
	defer regMu.RUnlock()

	ordered := make([]string, 0, len(selection))
	seen := map[string]bool{}

	appendName := func(name string) {
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		ordered = append(ordered, name)
	}

	for _, raw := range selection {
		entry := strings.TrimSpace(raw)
		if entry == "" {
			continue
		}
		if strings.HasPrefix(entry, "@") {
			bundleName := strings.TrimPrefix(entry, "@")
			bundle, ok := bundles[bundleName]
			if !ok {
				return nil, fmt.Errorf("unknown tool bundle %q", bundleName)
			}
			for _, n := range bundle.Tools {
				appendName(n)
			}
			continue
		}
		if entry == "*" {
			all := make([]string, 0, len(toolFactories))
			for n := range toolFactories {
				all = append(all, n)
			}
			sort.Strings(all)
			for _, n := range all {
				appendName(n)
			}
			continue
		}
		appendName(entry)
	}

	return ordered, nil
}

// ExecuteTool instantiates and runs a single tool by name with the given input.
func ExecuteTool(ctx context.Context, name string, input json.RawMessage) (any, error) {
	regMu.RLock()
	factory, ok := toolFactories[name]
	regMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("unknown tool %q", name)
	}
	t := factory()
	if t == nil {
		return nil, fmt.Errorf("tool %q factory returned nil", name)
	}
	return t.Execute(ctx, input)
}
