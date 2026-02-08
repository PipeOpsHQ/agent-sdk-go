package tools

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

type Factory func() Tool

type Bundle struct {
	Name        string
	Description string
	Tools       []string
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
