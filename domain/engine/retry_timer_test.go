package engine

import (
	"context"
	"testing"
	"time"
)

func TestRetryDuringCtxCancel(t *testing.T) {
	timerStarted := make(chan struct{})
	stopCalled := make(chan struct{})
	e := &Engine{cfg: Config{AfterFunc: func(time.Duration, func()) func() {
		close(timerStarted)
		return func() { close(stopCalled) }
	}}}
	ctx, cancel := context.WithCancel(context.Background())
	retryCh := make(chan retryEvent, 1)
	st := &runState{pendingRetries: 1}

	e.scheduleRetry(ctx, "n1", 2, time.Hour, retryCh)
	<-timerStarted
	cancel()

	select {
	case <-stopCalled:
	case <-time.After(time.Second):
		t.Fatal("timer stop was not called")
	}

	select {
	case ev := <-retryCh:
		st.pendingRetries--
		if !ev.cancelled {
			t.Fatal("retry event should be cancelled")
		}
		if ev.nodeID != "n1" || ev.attempt != 2 {
			t.Fatalf("event: %+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled retry event was not delivered")
	}
	if st.pendingRetries != 0 {
		t.Fatalf("pendingRetries: %d", st.pendingRetries)
	}
}
