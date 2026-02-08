package api

import "context"

type AuditLog struct {
	ActorKeyID string
	Action     string
	Resource   string
	Payload    string
}

type AuditStore interface {
	Record(ctx context.Context, entry AuditLog) error
	Close() error
}
