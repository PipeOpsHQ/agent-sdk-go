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

func TestStore_EnsureKey(t *testing.T) {
	store, err := New(filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatalf("new auth store: %v", err)
	}
	defer func() { _ = store.Close() }()

	ctx := context.Background()
	secret := "ak-7132d757-9ae3-4218-8f77-8aa511999f2c"

	first, err := store.EnsureKey(ctx, secret, auth.RoleAdmin)
	if err != nil {
		t.Fatalf("ensure key (first): %v", err)
	}
	if first.Role != auth.RoleAdmin {
		t.Fatalf("unexpected first role: %s", first.Role)
	}

	verified, err := store.VerifyKey(ctx, secret)
	if err != nil {
		t.Fatalf("verify ensured key: %v", err)
	}
	if verified.ID != first.ID {
		t.Fatalf("verify id mismatch: got %s want %s", verified.ID, first.ID)
	}

	second, err := store.EnsureKey(ctx, secret, auth.RoleViewer)
	if err != nil {
		t.Fatalf("ensure key (second): %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("ensure key should be idempotent: got %s want %s", second.ID, first.ID)
	}
	if second.Role != auth.RoleAdmin {
		t.Fatalf("ensure key should keep existing role: got %s", second.Role)
	}
}
