package builtin

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/shinya/shineflow/domain/executor"
	"github.com/shinya/shineflow/domain/workflow"
)

func TestStartReturnsEmptyOutputs(t *testing.T) {
	exe := startFactory(nil)
	out, err := exe.Execute(context.Background(), executor.ExecInput{
		Run: executor.RunInfo{TriggerPayload: json.RawMessage(`{"user_id":"u1"}`)},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.FiredPort != workflow.PortDefault {
		t.Fatalf("FiredPort: got %q want %q", out.FiredPort, workflow.PortDefault)
	}
	if len(out.Outputs) != 0 {
		t.Fatalf("Outputs should be empty, got %v", out.Outputs)
	}
}
