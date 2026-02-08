package distributed

import "time"

type RuntimePolicy struct {
	MaxAttempts       int
	BaseBackoff       time.Duration
	MaxBackoff        time.Duration
	PollInterval      time.Duration
	ClaimBlock        time.Duration
	HeartbeatInterval time.Duration
}

func DefaultRuntimePolicy() RuntimePolicy {
	return RuntimePolicy{
		MaxAttempts:       3,
		BaseBackoff:       500 * time.Millisecond,
		MaxBackoff:        10 * time.Second,
		PollInterval:      200 * time.Millisecond,
		ClaimBlock:        2 * time.Second,
		HeartbeatInterval: 5 * time.Second,
	}
}

func NormalizeRuntimePolicy(policy RuntimePolicy) RuntimePolicy {
	if policy.MaxAttempts <= 0 {
		policy.MaxAttempts = 3
	}
	if policy.BaseBackoff <= 0 {
		policy.BaseBackoff = 500 * time.Millisecond
	}
	if policy.MaxBackoff <= 0 {
		policy.MaxBackoff = 10 * time.Second
	}
	if policy.MaxBackoff < policy.BaseBackoff {
		policy.MaxBackoff = policy.BaseBackoff
	}
	if policy.PollInterval <= 0 {
		policy.PollInterval = 200 * time.Millisecond
	}
	if policy.ClaimBlock < 0 {
		policy.ClaimBlock = 0
	}
	if policy.HeartbeatInterval <= 0 {
		policy.HeartbeatInterval = 5 * time.Second
	}
	return policy
}

func (p RuntimePolicy) Backoff(attempt int) time.Duration {
	p = NormalizeRuntimePolicy(p)
	if attempt <= 0 {
		attempt = 1
	}
	backoff := p.BaseBackoff
	for i := 1; i < attempt; i++ {
		backoff *= 2
		if backoff >= p.MaxBackoff {
			return p.MaxBackoff
		}
	}
	if backoff > p.MaxBackoff {
		return p.MaxBackoff
	}
	return backoff
}
