package auth

import "context"

type Store interface {
	CreateKey(ctx context.Context, role Role) (KeyWithSecret, error)
	ListKeys(ctx context.Context) ([]APIKey, error)
	DisableKey(ctx context.Context, id string) error
	VerifyKey(ctx context.Context, secret string) (APIKey, error)
	Close() error
}
