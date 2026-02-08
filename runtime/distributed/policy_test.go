package distributed

import (
	"testing"
	"time"
)

func TestRuntimePolicyBackoff(t *testing.T) {
	p := NormalizeRuntimePolicy(RuntimePolicy{MaxAttempts: 3, BaseBackoff: 100 * time.Millisecond, MaxBackoff: 500 * time.Millisecond})
	if p.Backoff(1) != 100*time.Millisecond {
		t.Fatalf("unexpected backoff for attempt 1: %v", p.Backoff(1))
	}
	if p.Backoff(2) != 200*time.Millisecond {
		t.Fatalf("unexpected backoff for attempt 2: %v", p.Backoff(2))
	}
	if p.Backoff(8) != 500*time.Millisecond {
		t.Fatalf("expected capped backoff, got %v", p.Backoff(8))
	}
}
