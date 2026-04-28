package engine

import (
	"context"
	"math/rand"
	"sync"
	"time"

	"github.com/shinya/shineflow/domain/workflow"
)

const (
	maxBackoffDelay = 30 * time.Second
	maxBackoffShift = 8
	jitterFraction  = 0.2
)

var defaultErrorPolicy = workflow.ErrorPolicy{
	Timeout:        0,
	MaxRetries:     0,
	RetryBackoff:   workflow.BackoffFixed,
	RetryDelay:     0,
	OnFinalFail:    workflow.FailStrategyFailRun,
	FallbackOutput: workflow.FallbackOutput{},
}

func effectivePolicy(ep *workflow.ErrorPolicy) workflow.ErrorPolicy {
	if ep == nil {
		return defaultErrorPolicy
	}
	p := *ep
	if p.OnFinalFail == "" {
		p.OnFinalFail = workflow.FailStrategyFailRun
	}
	if p.RetryBackoff == "" {
		p.RetryBackoff = workflow.BackoffFixed
	}
	return p
}

type retryEvent struct {
	nodeID    string
	attempt   int
	cancelled bool
}

// scheduleRetry 在延迟后投递重试事件；ctx 取消会立刻投递 cancelled 事件。
func (e *Engine) scheduleRetry(ctx context.Context, nodeID string, nextAttempt int, delay time.Duration, retryCh chan<- retryEvent) {
	var once sync.Once
	done := make(chan struct{})
	send := func(cancelled bool) {
		once.Do(func() {
			close(done)
			retryCh <- retryEvent{nodeID: nodeID, attempt: nextAttempt, cancelled: cancelled}
		})
	}
	stopTimer := e.cfg.AfterFunc(delay, func() {
		select {
		case <-ctx.Done():
			send(true)
		default:
			send(false)
		}
	})
	go func() {
		select {
		case <-ctx.Done():
			stopTimer()
			send(true)
		case <-done:
		}
	}()
}

func computeBackoff(ep workflow.ErrorPolicy, attempt int, rng *rand.Rand) time.Duration {
	if ep.RetryDelay <= 0 {
		return 0
	}
	base := ep.RetryDelay
	if ep.RetryBackoff == workflow.BackoffExponential {
		shift := attempt - 1
		if shift > maxBackoffShift {
			shift = maxBackoffShift
		}
		for i := 0; i < shift; i++ {
			if base >= maxBackoffDelay/2 {
				base = maxBackoffDelay
				break
			}
			base *= 2
		}
	}
	if base > maxBackoffDelay {
		base = maxBackoffDelay
	}
	if rng == nil {
		return base
	}
	delta := time.Duration(float64(base) * jitterFraction * (rng.Float64()*2 - 1))
	return base + delta
}
