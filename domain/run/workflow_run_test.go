package run

import "testing"

func TestRunStatus_CanTransitionTo(t *testing.T) {
	cases := []struct {
		from, to RunStatus
		want     bool
	}{
		{RunStatusPending, RunStatusRunning, true},
		{RunStatusRunning, RunStatusSuccess, true},
		{RunStatusRunning, RunStatusFailed, true},
		{RunStatusRunning, RunStatusCancelled, true},
		{RunStatusPending, RunStatusFailed, true},
		{RunStatusPending, RunStatusCancelled, true},

		// 不允许从终态回退
		{RunStatusSuccess, RunStatusRunning, false},
		{RunStatusFailed, RunStatusRunning, false},
		{RunStatusCancelled, RunStatusRunning, false},
		{RunStatusSuccess, RunStatusFailed, false},

		// 同状态无谓推进
		{RunStatusRunning, RunStatusRunning, false},
		{RunStatusSuccess, RunStatusSuccess, false},

		// 不允许 Pending → Success（必须先 Running）
		{RunStatusPending, RunStatusSuccess, false},
	}
	for _, c := range cases {
		got := c.from.CanTransitionTo(c.to)
		if got != c.want {
			t.Errorf("%s → %s: got %v, want %v", c.from, c.to, got, c.want)
		}
	}
}
