package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/PipeOpsHQ/agent-sdk-go/framework/devui/catalog"
)

func TestStore_SaveAndListCatalog(t *testing.T) {
	store, err := New(filepath.Join(t.TempDir(), "catalog.db"))
	if err != nil {
		t.Fatalf("new catalog store: %v", err)
	}
	defer func() { _ = store.Close() }()

	ctx := context.Background()
	tpl, err := store.SaveTemplate(ctx, catalog.ToolTemplate{Name: "trivy-parser"})
	if err != nil {
		t.Fatalf("save template: %v", err)
	}
	if _, err := store.SaveInstance(ctx, catalog.ToolInstance{
		TemplateID: tpl.ID,
		Name:       "trivy-parser-default",
		Enabled:    true,
	}); err != nil {
		t.Fatalf("save instance: %v", err)
	}
	bundle, err := store.SaveBundle(ctx, catalog.ToolBundle{Name: "secops", ToolNames: []string{"calculator"}})
	if err != nil {
		t.Fatalf("save bundle: %v", err)
	}
	if _, err := store.SaveWorkflowBinding(ctx, catalog.WorkflowToolBinding{Workflow: "basic", BundleIDs: []string{bundle.ID}}); err != nil {
		t.Fatalf("save binding: %v", err)
	}

	templates, err := store.ListTemplates(ctx)
	if err != nil || len(templates) == 0 {
		t.Fatalf("list templates: %v len=%d", err, len(templates))
	}
	instances, err := store.ListInstances(ctx)
	if err != nil || len(instances) == 0 {
		t.Fatalf("list instances: %v len=%d", err, len(instances))
	}
	bundles, err := store.ListBundles(ctx)
	if err != nil || len(bundles) == 0 {
		t.Fatalf("list bundles: %v len=%d", err, len(bundles))
	}
	binding, err := store.GetWorkflowBinding(ctx, "basic")
	if err != nil {
		t.Fatalf("get binding: %v", err)
	}
	if binding.Workflow != "basic" || len(binding.BundleIDs) == 0 {
		t.Fatalf("unexpected binding: %+v", binding)
	}
}
