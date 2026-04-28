package engine

import (
	"math/rand"
	"testing"
	"time"

	"github.com/shinya/shineflow/domain/workflow"
)

func TestComputeBackoffZeroDelay(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	d := computeBackoff(workflow.ErrorPolicy{RetryDelay: 0}, 1, rng)
	if d != 0 {
		t.Fatalf("got %v", d)
	}
}

func TestComputeBackoffFixed(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for i := 1; i < 5; i++ {
		d := computeBackoff(workflow.ErrorPolicy{RetryDelay: time.Second, RetryBackoff: workflow.BackoffFixed}, i, rng)
		if d < 800*time.Millisecond || d > 1200*time.Millisecond {
			t.Fatalf("attempt %d: %v", i, d)
		}
	}
}

func TestComputeBackoffExponentialCap(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	d := computeBackoff(workflow.ErrorPolicy{RetryDelay: time.Second, RetryBackoff: workflow.BackoffExponential}, 100, rng)
	if d < 24*time.Second || d > 36*time.Second {
		t.Fatalf("not capped: %v", d)
	}
}

func TestEffectivePolicyDefaultIsFailRun(t *testing.T) {
	p := effectivePolicy(nil)
	if p.OnFinalFail != workflow.FailStrategyFailRun {
		t.Fatalf("got %v", p.OnFinalFail)
	}
}

func TestEffectivePolicyExplicitOverrides(t *testing.T) {
	p := effectivePolicy(&workflow.ErrorPolicy{OnFinalFail: workflow.FailStrategyFireErrorPort, MaxRetries: 5})
	if p.OnFinalFail != workflow.FailStrategyFireErrorPort {
		t.Fatalf("got %v", p.OnFinalFail)
	}
	if p.MaxRetries != 5 {
		t.Fatalf("got %d", p.MaxRetries)
	}
}
