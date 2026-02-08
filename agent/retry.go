package agent

import "time"

const (
	defaultBaseBackoff = 200 * time.Millisecond
	defaultMaxBackoff  = 2 * time.Second
)

type RetryPolicy struct {
	MaxAttempts int
	BaseBackoff time.Duration
	MaxBackoff  time.Duration
}

func defaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		MaxAttempts: 1,
		BaseBackoff: defaultBaseBackoff,
		MaxBackoff:  defaultMaxBackoff,
	}
}

func normalizeRetryPolicy(in RetryPolicy) RetryPolicy {
	out := in
	if out.MaxAttempts < 1 {
		out.MaxAttempts = 1
	}
	if out.BaseBackoff <= 0 {
		out.BaseBackoff = defaultBaseBackoff
	}
	if out.MaxBackoff <= 0 {
		out.MaxBackoff = defaultMaxBackoff
	}
	if out.MaxBackoff < out.BaseBackoff {
		out.MaxBackoff = out.BaseBackoff
	}
	return out
}

func (p RetryPolicy) backoffForAttempt(retryNumber int) time.Duration {
	if retryNumber < 1 {
		retryNumber = 1
	}
	delay := p.BaseBackoff
	for i := 1; i < retryNumber; i++ {
		delay *= 2
		if delay >= p.MaxBackoff {
			return p.MaxBackoff
		}
	}
	if delay > p.MaxBackoff {
		return p.MaxBackoff
	}
	return delay
}
