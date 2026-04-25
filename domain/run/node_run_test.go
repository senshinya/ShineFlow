package run

import "testing"

func TestNodeRunStatus_CanTransitionTo(t *testing.T) {
	cases := []struct {
		from, to NodeRunStatus
		want     bool
	}{
		{NodeRunStatusPending, NodeRunStatusRunning, true},
		{NodeRunStatusPending, NodeRunStatusSkipped, true},
		{NodeRunStatusRunning, NodeRunStatusSuccess, true},
		{NodeRunStatusRunning, NodeRunStatusFailed, true},

		// 终态不可回退
		{NodeRunStatusSuccess, NodeRunStatusRunning, false},
		{NodeRunStatusFailed, NodeRunStatusRunning, false},
		{NodeRunStatusSkipped, NodeRunStatusRunning, false},

		// 同态不可自转
		{NodeRunStatusRunning, NodeRunStatusRunning, false},

		// 不允许 Pending → Success（必须先 Running）
		{NodeRunStatusPending, NodeRunStatusSuccess, false},
	}
	for _, c := range cases {
		got := c.from.CanTransitionTo(c.to)
		if got != c.want {
			t.Errorf("%s → %s: got %v, want %v", c.from, c.to, got, c.want)
		}
	}
}
