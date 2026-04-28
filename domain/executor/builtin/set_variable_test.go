package builtin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/shinya/shineflow/domain/executor"
	"github.com/shinya/shineflow/domain/workflow"
)

func TestSetVariableHappy(t *testing.T) {
	exe := setVariableFactory(nil)
	out, err := exe.Execute(context.Background(), executor.ExecInput{
		Config: json.RawMessage(`{"name":"my_var"}`),
		Inputs: map[string]any{"value": 42},
	})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if out.FiredPort != workflow.PortDefault {
		t.Fatalf("port: %s", out.FiredPort)
	}
	if v, _ := out.Outputs["my_var"].(int); v != 42 {
		t.Fatalf("Outputs.my_var: %v", out.Outputs["my_var"])
	}
}

func TestSetVariableMissingName(t *testing.T) {
	exe := setVariableFactory(nil)
	_, err := exe.Execute(context.Background(), executor.ExecInput{
		Config: json.RawMessage(`{}`),
		Inputs: map[string]any{"value": 1},
	})
	if err == nil || !strings.Contains(err.Error(), "name") {
		t.Fatalf("expected missing name error, got %v", err)
	}
}

func TestSetVariableMissingValue(t *testing.T) {
	exe := setVariableFactory(nil)
	_, err := exe.Execute(context.Background(), executor.ExecInput{
		Config: json.RawMessage(`{"name":"x"}`),
		Inputs: map[string]any{},
	})
	if err == nil || !strings.Contains(err.Error(), "value") {
		t.Fatalf("expected missing value error, got %v", err)
	}
}
