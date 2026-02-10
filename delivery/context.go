package delivery

import "context"

type contextKey string

const (
	replyTargetContextKey contextKey = "delivery.reply_to"
	turnTypeContextKey    contextKey = "delivery.turn_type"
	parentRunContextKey   contextKey = "delivery.parent_run_id"
)

// WithTarget stores a normalized reply target on context.
func WithTarget(ctx context.Context, target *Target) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	normalized := Normalize(target)
	if normalized == nil {
		return ctx
	}
	return context.WithValue(ctx, replyTargetContextKey, normalized)
}

// FromContext returns the reply target previously attached to context.
func FromContext(ctx context.Context) *Target {
	if ctx == nil {
		return nil
	}
	v := ctx.Value(replyTargetContextKey)
	target, ok := v.(*Target)
	if !ok {
		return nil
	}
	return Normalize(target)
}

// WithTurnType stores a run turn type (user, clarification, background, tool, etc.).
func WithTurnType(ctx context.Context, turnType string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	turnType = trim(turnType)
	if turnType == "" {
		return ctx
	}
	return context.WithValue(ctx, turnTypeContextKey, turnType)
}

func TurnTypeFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	v, ok := ctx.Value(turnTypeContextKey).(string)
	if !ok {
		return ""
	}
	return trim(v)
}

// WithParentRunID stores a parent run id used for chaining related run records.
func WithParentRunID(ctx context.Context, runID string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	runID = trim(runID)
	if runID == "" {
		return ctx
	}
	return context.WithValue(ctx, parentRunContextKey, runID)
}

func ParentRunIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	v, ok := ctx.Value(parentRunContextKey).(string)
	if !ok {
		return ""
	}
	return trim(v)
}

func trim(v string) string {
	for len(v) > 0 && (v[0] == ' ' || v[0] == '\n' || v[0] == '\t' || v[0] == '\r') {
		v = v[1:]
	}
	for len(v) > 0 {
		last := v[len(v)-1]
		if last == ' ' || last == '\n' || last == '\t' || last == '\r' {
			v = v[:len(v)-1]
			continue
		}
		break
	}
	return v
}
