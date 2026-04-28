package builtin

import (
	"context"
	"testing"

	"github.com/shinya/shineflow/domain/executor"
	"github.com/shinya/shineflow/domain/workflow"
)

func TestJoinNoOp(t *testing.T) {
	exe := joinFactory(nil)
	out, err := exe.Execute(context.Background(), executor.ExecInput{})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if out.FiredPort != workflow.PortDefault {
		t.Fatalf("port: %s", out.FiredPort)
	}
	if len(out.Outputs) != 0 {
		t.Fatalf("Outputs should be empty: %v", out.Outputs)
	}
}
