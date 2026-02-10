package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/PipeOpsHQ/agent-sdk-go/devui/auth"
)

func TestStore_CreateAndVerifyKey(t *testing.T) {
	store, err := New(filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatalf("new auth store: %v", err)
	}
	defer func() { _ = store.Close() }()

	ctx := context.Background()
	created, err := store.CreateKey(ctx, auth.RoleOperator)
	if err != nil {
		t.Fatalf("create key: %v", err)
	}
	if created.Secret == "" {
		t.Fatalf("expected key secret")
	}

	verified, err := store.VerifyKey(ctx, created.Secret)
	if err != nil {
		t.Fatalf("verify key: %v", err)
	}
	if verified.Role != auth.RoleOperator {
		t.Fatalf("unexpected role: %s", verified.Role)
	}

	if err := store.DisableKey(ctx, created.ID); err != nil {
		t.Fatalf("disable key: %v", err)
	}
	if _, err := store.VerifyKey(ctx, created.Secret); err == nil {
		t.Fatalf("expected disabled key verification to fail")
	}
}
