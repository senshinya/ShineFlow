package builtin

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/shinya/shineflow/domain/executor"
	"github.com/shinya/shineflow/domain/workflow"
)

func switchExec() executor.NodeExecutor { return switchFactory(nil) }

func TestSwitchHitsFirstMatchingCase(t *testing.T) {
	cfg := `{"cases":[
		{"name":"hot","operator":"gte","right":{"kind":"literal","value":80}},
		{"name":"cold","operator":"lt","right":{"kind":"literal","value":40}}
	]}`
	out, err := switchExec().Execute(context.Background(), executor.ExecInput{
		Config: json.RawMessage(cfg),
		Inputs: map[string]any{"value": 90},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.FiredPort != "hot" {
		t.Fatalf("port: %s", out.FiredPort)
	}
	if out.Outputs["matched"] != "hot" {
		t.Fatalf("matched: %v", out.Outputs["matched"])
	}
}

func TestSwitchFallsToDefault(t *testing.T) {
	cfg := `{"cases":[
		{"name":"hot","operator":"gte","right":{"kind":"literal","value":80}}
	]}`
	out, err := switchExec().Execute(context.Background(), executor.ExecInput{
		Config: json.RawMessage(cfg),
		Inputs: map[string]any{"value": 25},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.FiredPort != workflow.PortDefault {
		t.Fatalf("port: %s", out.FiredPort)
	}
	if out.Outputs["matched"] != workflow.PortDefault {
		t.Fatalf("matched: %v", out.Outputs["matched"])
	}
}

func TestSwitchOrderingFirstWins(t *testing.T) {
	cfg := `{"cases":[
		{"name":"any","operator":"is_not_empty","right":{"kind":"literal","value":null}},
		{"name":"specific","operator":"eq","right":{"kind":"literal","value":"x"}}
	]}`
	out, err := switchExec().Execute(context.Background(), executor.ExecInput{
		Config: json.RawMessage(cfg),
		Inputs: map[string]any{"value": "x"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.FiredPort != "any" {
		t.Fatalf("port: %s (first matching case wins)", out.FiredPort)
	}
}

func TestSwitchRejectsEmptyCaseName(t *testing.T) {
	cfg := `{"cases":[{"name":"","operator":"is_empty","right":{"kind":"literal","value":null}}]}`
	_, err := switchExec().Execute(context.Background(), executor.ExecInput{
		Config: json.RawMessage(cfg),
		Inputs: map[string]any{"value": ""},
	})
	if err == nil {
		t.Fatal("expected empty case name error")
	}
}

func TestSwitchRejectsNonLiteralRight(t *testing.T) {
	cfg := `{"cases":[{"name":"x","operator":"eq","right":{"kind":"ref","value":{"node_id":"n1","path":"k"}}}]}`
	_, err := switchExec().Execute(context.Background(), executor.ExecInput{
		Config: json.RawMessage(cfg),
		Inputs: map[string]any{"value": "x"},
	})
	if err == nil {
		t.Fatal("expected non-literal right error")
	}
}
