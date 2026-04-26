package validator

import (
	"strings"
	"testing"

	"github.com/shinya/shineflow/domain/nodetype"
	"github.com/shinya/shineflow/domain/workflow"
)

// fakeRegistry 在测试中按需返回 NodeType。
type fakeRegistry struct {
	types map[string]*nodetype.NodeType
}

func (f *fakeRegistry) Get(key string) (*nodetype.NodeType, bool) {
	nt, ok := f.types[key]
	return nt, ok
}
func (f *fakeRegistry) List(_ nodetype.NodeTypeFilter) []*nodetype.NodeType { return nil }
func (f *fakeRegistry) Invalidate(_ string)                                 {}
func (f *fakeRegistry) InvalidatePrefix(_ string)                           {}

// builtinTypes 构造 8 个常用内置 NodeType，便于复用。
func builtinTypes() map[string]*nodetype.NodeType {
	defaultPorts := []string{workflow.PortDefault, workflow.PortError}
	return map[string]*nodetype.NodeType{
		nodetype.BuiltinStart: {Key: nodetype.BuiltinStart, Ports: []string{workflow.PortDefault}},
		nodetype.BuiltinEnd:   {Key: nodetype.BuiltinEnd, Ports: []string{}},
		nodetype.BuiltinLLM:   {Key: nodetype.BuiltinLLM, Ports: defaultPorts},
		nodetype.BuiltinIf: {Key: nodetype.BuiltinIf, Ports: []string{
			nodetype.PortIfTrue, nodetype.PortIfFalse, workflow.PortError,
		}},
	}
}

// minimalDSL 构造一个能通过严格校验的最小 DSL：start → llm → end。
func minimalDSL() workflow.WorkflowDSL {
	return workflow.WorkflowDSL{
		Nodes: []workflow.Node{
			{ID: "n_start", TypeKey: nodetype.BuiltinStart, TypeVer: nodetype.NodeTypeVersion1},
			{ID: "n_llm", TypeKey: nodetype.BuiltinLLM, TypeVer: nodetype.NodeTypeVersion1},
			{ID: "n_end", TypeKey: nodetype.BuiltinEnd, TypeVer: nodetype.NodeTypeVersion1},
		},
		Edges: []workflow.Edge{
			{ID: "e1", From: "n_start", FromPort: workflow.PortDefault, To: "n_llm"},
			{ID: "e2", From: "n_llm", FromPort: workflow.PortDefault, To: "n_end"},
		},
	}
}

func TestValidate_Minimal_Pass(t *testing.T) {
	res := ValidateForPublish(minimalDSL(), &fakeRegistry{types: builtinTypes()})
	if !res.OK() {
		t.Fatalf("expected pass, got: %+v", res.Errors)
	}
}

func TestValidate_NoStartOrEnd(t *testing.T) {
	dsl := workflow.WorkflowDSL{
		Nodes: []workflow.Node{
			{ID: "n_llm", TypeKey: nodetype.BuiltinLLM, TypeVer: nodetype.NodeTypeVersion1},
		},
	}
	res := ValidateForPublish(dsl, &fakeRegistry{types: builtinTypes()})
	if res.OK() {
		t.Fatal("expected failure for missing start/end")
	}
	mustHaveCode(t, res, CodeMissingStart)
	mustHaveCode(t, res, CodeMissingEnd)
}

func TestValidate_DuplicateNodeID(t *testing.T) {
	dsl := minimalDSL()
	dsl.Nodes = append(dsl.Nodes, workflow.Node{
		ID: "n_llm", TypeKey: nodetype.BuiltinLLM, TypeVer: nodetype.NodeTypeVersion1,
	})
	res := ValidateForPublish(dsl, &fakeRegistry{types: builtinTypes()})
	mustHaveCode(t, res, CodeDuplicateNodeID)
}

func TestValidate_DuplicateEdgeID(t *testing.T) {
	dsl := minimalDSL()
	dsl.Edges = append(dsl.Edges, workflow.Edge{
		ID: "e1", From: "n_start", FromPort: workflow.PortDefault, To: "n_llm",
	})
	res := ValidateForPublish(dsl, &fakeRegistry{types: builtinTypes()})
	mustHaveCode(t, res, CodeDuplicateEdgeID)
}

func TestValidate_DanglingEdge(t *testing.T) {
	dsl := minimalDSL()
	dsl.Edges = append(dsl.Edges, workflow.Edge{
		ID: "e_bad", From: "n_llm", FromPort: workflow.PortDefault, To: "n_ghost",
	})
	res := ValidateForPublish(dsl, &fakeRegistry{types: builtinTypes()})
	mustHaveCode(t, res, CodeDanglingEdge)
}

func TestValidate_DanglingRef(t *testing.T) {
	dsl := minimalDSL()
	// llm 节点引用一个不存在的 node
	dsl.Nodes[1].Inputs = map[string]workflow.ValueSource{
		"in_prompt": {
			Kind:  workflow.ValueKindRef,
			Value: workflow.RefValue{NodeID: "n_ghost", PortID: "out_1"},
		},
	}
	res := ValidateForPublish(dsl, &fakeRegistry{types: builtinTypes()})
	mustHaveCode(t, res, CodeDanglingRef)
}

func TestValidate_UnknownFromPort(t *testing.T) {
	dsl := minimalDSL()
	dsl.Edges[1].FromPort = "wat"
	res := ValidateForPublish(dsl, &fakeRegistry{types: builtinTypes()})
	mustHaveCode(t, res, CodeUnknownFromPort)
}

func TestValidate_RequiredInputMissing(t *testing.T) {
	types := builtinTypes()
	types[nodetype.BuiltinLLM] = &nodetype.NodeType{
		Key:   nodetype.BuiltinLLM,
		Ports: []string{workflow.PortDefault, workflow.PortError},
		InputSchema: []workflow.PortSpec{
			{ID: "in_prompt", Name: "prompt", Required: true,
				Type: workflow.SchemaType{Type: workflow.SchemaTypeString}},
		},
	}
	res := ValidateForPublish(minimalDSL(), &fakeRegistry{types: types})
	mustHaveCode(t, res, CodeRequiredInputMissing)
}

func TestValidate_UnknownNodeType(t *testing.T) {
	dsl := minimalDSL()
	dsl.Nodes[1].TypeKey = "builtin.unknown"
	res := ValidateForPublish(dsl, &fakeRegistry{types: builtinTypes()})
	mustHaveCode(t, res, CodeUnknownNodeType)
}

func TestValidate_FallbackOnNodeWithoutDefault(t *testing.T) {
	types := builtinTypes()
	// switch 没有 default port
	types[nodetype.BuiltinSwitch] = &nodetype.NodeType{
		Key:   nodetype.BuiltinSwitch,
		Ports: []string{"case_1", workflow.PortError},
	}
	dsl := minimalDSL()
	dsl.Nodes[1] = workflow.Node{
		ID:      "n_llm",
		TypeKey: nodetype.BuiltinSwitch,
		TypeVer: nodetype.NodeTypeVersion1,
		ErrorPolicy: &workflow.ErrorPolicy{
			OnFinalFail: workflow.FailStrategyFallback,
		},
	}
	dsl.Edges = []workflow.Edge{
		{ID: "e1", From: "n_start", FromPort: workflow.PortDefault, To: "n_llm"},
		{ID: "e2", From: "n_llm", FromPort: "case_1", To: "n_end"},
	}
	res := ValidateForPublish(dsl, &fakeRegistry{types: types})
	mustHaveCode(t, res, CodeFallbackOnNonDefaultPortNode)
}

func TestValidate_Cycle(t *testing.T) {
	dsl := workflow.WorkflowDSL{
		Nodes: []workflow.Node{
			{ID: "n_start", TypeKey: nodetype.BuiltinStart, TypeVer: nodetype.NodeTypeVersion1},
			{ID: "a", TypeKey: nodetype.BuiltinLLM, TypeVer: nodetype.NodeTypeVersion1},
			{ID: "b", TypeKey: nodetype.BuiltinLLM, TypeVer: nodetype.NodeTypeVersion1},
			{ID: "n_end", TypeKey: nodetype.BuiltinEnd, TypeVer: nodetype.NodeTypeVersion1},
		},
		Edges: []workflow.Edge{
			{ID: "e1", From: "n_start", FromPort: workflow.PortDefault, To: "a"},
			{ID: "e2", From: "a", FromPort: workflow.PortDefault, To: "b"},
			{ID: "e3", From: "b", FromPort: workflow.PortDefault, To: "a"},
			{ID: "e4", From: "a", FromPort: workflow.PortDefault, To: "n_end"},
		},
	}
	res := ValidateForPublish(dsl, &fakeRegistry{types: builtinTypes()})
	mustHaveCode(t, res, CodeCycle)
}

func TestValidate_AllErrorsReturnedAtOnce(t *testing.T) {
	// 同时缺 start、缺 end、edge 悬空
	dsl := workflow.WorkflowDSL{
		Nodes: []workflow.Node{
			{ID: "a", TypeKey: nodetype.BuiltinLLM, TypeVer: nodetype.NodeTypeVersion1},
		},
		Edges: []workflow.Edge{
			{ID: "e1", From: "a", FromPort: workflow.PortDefault, To: "ghost"},
		},
	}
	res := ValidateForPublish(dsl, &fakeRegistry{types: builtinTypes()})
	codes := codeSet(res)
	for _, want := range []string{CodeMissingStart, CodeMissingEnd, CodeDanglingEdge} {
		if !codes[want] {
			t.Errorf("missing code %q in %v", want, codes)
		}
	}
}

// helpers

func mustHaveCode(t *testing.T, r ValidationResult, code string) {
	t.Helper()
	for _, e := range r.Errors {
		if e.Code == code {
			return
		}
	}
	dump := []string{}
	for _, e := range r.Errors {
		dump = append(dump, e.Code+":"+e.Message)
	}
	t.Fatalf("expected code %q, got: %s", code, strings.Join(dump, " | "))
}

func codeSet(r ValidationResult) map[string]bool {
	out := map[string]bool{}
	for _, e := range r.Errors {
		out[e.Code] = true
	}
	return out
}
