package builtin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/shinya/shineflow/domain/executor"
	"github.com/shinya/shineflow/domain/nodetype"
)

func ifExec() executor.NodeExecutor { return ifFactory(nil) }

func TestIfEqNumericCoerce(t *testing.T) {
	out, err := ifExec().Execute(context.Background(), executor.ExecInput{
		Config: json.RawMessage(`{"operator":"eq"}`),
		Inputs: map[string]any{"left": float64(42), "right": int(42)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.FiredPort != nodetype.PortIfTrue {
		t.Fatalf("port: %s", out.FiredPort)
	}
	if v, _ := out.Outputs["result"].(bool); !v {
		t.Fatalf("result: %v", out.Outputs["result"])
	}
}

func TestIfNeStrings(t *testing.T) {
	out, err := ifExec().Execute(context.Background(), executor.ExecInput{
		Config: json.RawMessage(`{"operator":"ne"}`),
		Inputs: map[string]any{"left": "a", "right": "b"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.FiredPort != nodetype.PortIfTrue {
		t.Fatalf("port: %s", out.FiredPort)
	}
}

func TestIfNumericComparisons(t *testing.T) {
	cases := []struct {
		op    string
		left  any
		right any
	}{
		{"gt", 10, 3},
		{"lt", 3, 10},
		{"gte", 10, 10},
		{"lte", 10, 10},
	}
	for _, c := range cases {
		t.Run(c.op, func(t *testing.T) {
			out, err := ifExec().Execute(context.Background(), executor.ExecInput{
				Config: json.RawMessage(`{"operator":"` + c.op + `"}`),
				Inputs: map[string]any{"left": c.left, "right": c.right},
			})
			if err != nil {
				t.Fatal(err)
			}
			if out.FiredPort != nodetype.PortIfTrue {
				t.Fatalf("port: %s", out.FiredPort)
			}
		})
	}
}

func TestIfContainsString(t *testing.T) {
	out, err := ifExec().Execute(context.Background(), executor.ExecInput{
		Config: json.RawMessage(`{"operator":"contains"}`),
		Inputs: map[string]any{"left": "hello world", "right": "world"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.FiredPort != nodetype.PortIfTrue {
		t.Fatalf("port: %s", out.FiredPort)
	}
}

func TestIfStartsWithString(t *testing.T) {
	out, err := ifExec().Execute(context.Background(), executor.ExecInput{
		Config: json.RawMessage(`{"operator":"starts_with"}`),
		Inputs: map[string]any{"left": "hello world", "right": "hello"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.FiredPort != nodetype.PortIfTrue {
		t.Fatalf("port: %s", out.FiredPort)
	}
}

func TestIfIsEmpty(t *testing.T) {
	cases := []struct {
		v      any
		expect string
	}{
		{"", nodetype.PortIfTrue},
		{"x", nodetype.PortIfFalse},
		{nil, nodetype.PortIfTrue},
		{[]any{}, nodetype.PortIfTrue},
		{[]any{1}, nodetype.PortIfFalse},
		{map[string]any{}, nodetype.PortIfTrue},
		{map[string]any{"k": 1}, nodetype.PortIfFalse},
	}
	for _, c := range cases {
		t.Run("case", func(t *testing.T) {
			out, err := ifExec().Execute(context.Background(), executor.ExecInput{
				Config: json.RawMessage(`{"operator":"is_empty"}`),
				Inputs: map[string]any{"left": c.v},
			})
			if err != nil {
				t.Fatal(err)
			}
			if out.FiredPort != c.expect {
				t.Fatalf("v=%v: got port %q want %q", c.v, out.FiredPort, c.expect)
			}
		})
	}
}

func TestIfIsNotEmpty(t *testing.T) {
	out, err := ifExec().Execute(context.Background(), executor.ExecInput{
		Config: json.RawMessage(`{"operator":"is_not_empty"}`),
		Inputs: map[string]any{"left": "x"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.FiredPort != nodetype.PortIfTrue {
		t.Fatalf("port: %s", out.FiredPort)
	}
}

func TestIfFalsePath(t *testing.T) {
	out, err := ifExec().Execute(context.Background(), executor.ExecInput{
		Config: json.RawMessage(`{"operator":"eq"}`),
		Inputs: map[string]any{"left": 1, "right": 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.FiredPort != nodetype.PortIfFalse {
		t.Fatalf("port: %s", out.FiredPort)
	}
	if v, _ := out.Outputs["result"].(bool); v {
		t.Fatal("result should be false")
	}
}

func TestIfTypeMismatchReturnsError(t *testing.T) {
	_, err := ifExec().Execute(context.Background(), executor.ExecInput{
		Config: json.RawMessage(`{"operator":"gt"}`),
		Inputs: map[string]any{"left": "abc", "right": 1},
	})
	if err == nil || !strings.Contains(err.Error(), "type") {
		t.Fatalf("expected type mismatch err, got %v", err)
	}
}

func TestIfUnknownOperator(t *testing.T) {
	_, err := ifExec().Execute(context.Background(), executor.ExecInput{
		Config: json.RawMessage(`{"operator":"approx"}`),
		Inputs: map[string]any{"left": 1, "right": 1},
	})
	if err == nil || !strings.Contains(err.Error(), "operator") {
		t.Fatalf("expected unknown op err, got %v", err)
	}
}
