package engine

import (
	"encoding/json"
	"testing"

	"github.com/shinya/shineflow/domain/nodetype"
	"github.com/shinya/shineflow/domain/workflow"
)

func TestBuildTriggerTablePopulatesInEdgesAndOutAdj(t *testing.T) {
	dsl := workflow.WorkflowDSL{
		Nodes: []workflow.Node{
			{ID: "s", TypeKey: nodetype.BuiltinStart},
			{ID: "a", TypeKey: nodetype.BuiltinSetVariable},
			{ID: "j", TypeKey: nodetype.BuiltinJoin, Config: json.RawMessage(`{"mode":"any"}`)},
			{ID: "e", TypeKey: nodetype.BuiltinEnd},
		},
		Edges: []workflow.Edge{
			{ID: "e1", From: "s", FromPort: workflow.PortDefault, To: "a"},
			{ID: "e2", From: "s", FromPort: workflow.PortDefault, To: "j"},
			{ID: "e3", From: "a", FromPort: workflow.PortDefault, To: "j"},
			{ID: "e4", From: "j", FromPort: workflow.PortDefault, To: "e"},
		},
	}
	tt, oa := buildTriggerTable(dsl)
	if len(tt["s"].inEdges) != 0 {
		t.Fatalf("start inbound: %d", len(tt["s"].inEdges))
	}
	if len(tt["j"].inEdges) != 2 {
		t.Fatalf("join inbound: %d", len(tt["j"].inEdges))
	}
	if tt["j"].mode != joinAny {
		t.Fatalf("join mode: %v", tt["j"].mode)
	}
	if len(oa["s"]) != 2 {
		t.Fatalf("start outAdj: %d", len(oa["s"]))
	}
}

func TestEvaluateZeroInputs(t *testing.T) {
	if r := evaluate(&triggerSpec{}, nil); r != readyToRun {
		t.Fatalf("got %v", r)
	}
}

func TestEvaluateSingleInput(t *testing.T) {
	spec := &triggerSpec{inEdges: []inEdgeRef{{EdgeID: "e1"}}}
	es := map[string]edgeState{"e1": edgePending}
	if evaluate(spec, es) != notReady {
		t.Fatal("pending should wait")
	}
	es["e1"] = edgeLive
	if evaluate(spec, es) != readyToRun {
		t.Fatal("live should run")
	}
	es["e1"] = edgeDead
	if evaluate(spec, es) != readyToSkip {
		t.Fatal("dead should skip")
	}
}

func TestEvaluateJoinAny(t *testing.T) {
	spec := &triggerSpec{inEdges: []inEdgeRef{{EdgeID: "e1"}, {EdgeID: "e2"}}, mode: joinAny}
	es := map[string]edgeState{"e1": edgeLive, "e2": edgePending}
	if evaluate(spec, es) != readyToRun {
		t.Fatal("any with live should run")
	}
	es = map[string]edgeState{"e1": edgePending, "e2": edgePending}
	if evaluate(spec, es) != notReady {
		t.Fatal("any with pending inputs should wait")
	}
	es = map[string]edgeState{"e1": edgeDead, "e2": edgeDead}
	if evaluate(spec, es) != readyToSkip {
		t.Fatal("any with all dead should skip")
	}
}

func TestEvaluateJoinAll(t *testing.T) {
	spec := &triggerSpec{inEdges: []inEdgeRef{{EdgeID: "e1"}, {EdgeID: "e2"}}, mode: joinAll}
	es := map[string]edgeState{"e1": edgeLive, "e2": edgePending}
	if evaluate(spec, es) != notReady {
		t.Fatal("all with pending input should wait")
	}
	es = map[string]edgeState{"e1": edgeLive, "e2": edgeLive}
	if evaluate(spec, es) != readyToRun {
		t.Fatal("all live should run")
	}
	es = map[string]edgeState{"e1": edgeLive, "e2": edgeDead}
	if evaluate(spec, es) != readyToSkip {
		t.Fatal("all with dead input should skip")
	}
}
