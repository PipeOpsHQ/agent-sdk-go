package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestBuiltinsRegistered(t *testing.T) {
	names := ToolNames()
	if len(names) == 0 {
		t.Fatalf("expected registered tools")
	}
	found := false
	for _, n := range names {
		if n == "calculator" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected calculator built-in tool")
	}

	bundles := BundleNames()
	found = false
	for _, b := range bundles {
		if b == "default" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected default bundle")
	}
}

func TestBuildSelection_BundleAndWildcard(t *testing.T) {
	tools, err := BuildSelection([]string{"@default"})
	if err != nil {
		t.Fatalf("BuildSelection failed: %v", err)
	}
	if len(tools) < 7 {
		t.Fatalf("expected at least 7 tools from default bundle, got %d", len(tools))
	}

	all, err := BuildSelection([]string{"*"})
	if err != nil {
		t.Fatalf("BuildSelection all failed: %v", err)
	}
	if len(all) < 1 {
		t.Fatalf("expected at least one tool")
	}
}

func TestBuildSelection_UnknownBundle(t *testing.T) {
	_, err := BuildSelection([]string{"@nope"})
	if err == nil || !strings.Contains(err.Error(), "unknown tool bundle") {
		t.Fatalf("expected unknown bundle error, got %v", err)
	}
}

func TestCalculatorFromRegistry_Works(t *testing.T) {
	tools, err := BuildSelection([]string{"calculator"})
	if err != nil {
		t.Fatalf("BuildSelection failed: %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("expected one tool")
	}

	out, err := tools[0].Execute(context.Background(), json.RawMessage(`{"expression":"(2+3)*4"}`))
	if err != nil {
		t.Fatalf("calculator execute failed: %v", err)
	}
	m, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("unexpected output type %T", out)
	}
	if m["result"] != "20" {
		t.Fatalf("unexpected calculator result: %#v", m)
	}
}
