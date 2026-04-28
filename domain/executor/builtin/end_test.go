package builtin

import (
	"context"
	"testing"

	"github.com/shinya/shineflow/domain/executor"
)

func TestEndReturnsNilOutputs(t *testing.T) {
	exe := endFactory(nil)
	out, err := exe.Execute(context.Background(), executor.ExecInput{Inputs: map[string]any{"text": "hello"}})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if out.Outputs != nil {
		t.Fatalf("Outputs should be nil, got %v", out.Outputs)
	}
}
