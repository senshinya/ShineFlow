package validator

import (
	"encoding/json"
	"fmt"
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

func builtinTypes() map[string]*nodetype.NodeType {
	return map[string]*nodetype.NodeType{
		nodetype.BuiltinStart:       {Key: nodetype.BuiltinStart, Ports: []string{workflow.PortDefault}},
		nodetype.BuiltinEnd:         {Key: nodetype.BuiltinEnd, Ports: []string{}},
		nodetype.BuiltinLLM:         {Key: nodetype.BuiltinLLM, Ports: []string{workflow.PortDefault, workflow.PortError}},
		nodetype.BuiltinIf:          {Key: nodetype.BuiltinIf, Ports: []string{nodetype.PortIfTrue, nodetype.PortIfFalse, workflow.PortError}},
		nodetype.BuiltinSwitch:      {Key: nodetype.BuiltinSwitch, Ports: []string{workflow.PortDefault, workflow.PortError}},
		nodetype.BuiltinJoin:        {Key: nodetype.BuiltinJoin, Ports: []string{workflow.PortDefault}},
		nodetype.BuiltinSetVariable: {Key: nodetype.BuiltinSetVariable, Ports: []string{workflow.PortDefault}},
		nodetype.BuiltinHTTPRequest: {Key: nodetype.BuiltinHTTPRequest, Ports: []string{workflow.PortDefault, workflow.PortError}},
	}
}

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
		Nodes: []workflow.Node{{ID: "n_llm", TypeKey: nodetype.BuiltinLLM, TypeVer: nodetype.NodeTypeVersion1}},
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
	dsl.Nodes = append(dsl.Nodes, workflow.Node{ID: "n_llm", TypeKey: nodetype.BuiltinLLM, TypeVer: nodetype.NodeTypeVersion1})
	res := ValidateForPublish(dsl, &fakeRegistry{types: builtinTypes()})
	mustHaveCode(t, res, CodeDuplicateNodeID)
}

func TestValidate_DuplicateEdgeID(t *testing.T) {
	dsl := minimalDSL()
	dsl.Edges = append(dsl.Edges, workflow.Edge{ID: "e1", From: "n_start", FromPort: workflow.PortDefault, To: "n_llm"})
	res := ValidateForPublish(dsl, &fakeRegistry{types: builtinTypes()})
	mustHaveCode(t, res, CodeDuplicateEdgeID)
}

func TestValidate_DanglingEdge(t *testing.T) {
	dsl := minimalDSL()
	dsl.Edges = append(dsl.Edges, workflow.Edge{ID: "e_bad", From: "n_llm", FromPort: workflow.PortDefault, To: "n_ghost"})
	res := ValidateForPublish(dsl, &fakeRegistry{types: builtinTypes()})
	mustHaveCode(t, res, CodeDanglingEdge)
}

func TestValidate_DanglingRef(t *testing.T) {
	dsl := minimalDSL()
	dsl.Nodes[1].Inputs = map[string]workflow.ValueSource{
		"in_prompt": {Kind: workflow.ValueKindRef, Value: workflow.RefValue{NodeID: "n_ghost"}},
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
		InputSchema: []workflow.PortSpec{{
			ID: "in_prompt", Name: "prompt", Required: true,
			Type: workflow.SchemaType{Type: workflow.SchemaTypeString},
		}},
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
	types["custom.no_default"] = &nodetype.NodeType{Key: "custom.no_default", Ports: []string{workflow.PortError}}
	dsl := minimalDSL()
	dsl.Nodes[1] = workflow.Node{
		ID: "n_custom", TypeKey: "custom.no_default", TypeVer: nodetype.NodeTypeVersion1,
		ErrorPolicy: &workflow.ErrorPolicy{OnFinalFail: workflow.FailStrategyFallback},
	}
	dsl.Edges = []workflow.Edge{
		{ID: "e1", From: "n_start", FromPort: workflow.PortDefault, To: "n_custom"},
		{ID: "e2", From: "n_custom", FromPort: workflow.PortError, To: "n_end"},
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
	dsl := workflow.WorkflowDSL{
		Nodes: []workflow.Node{{ID: "a", TypeKey: nodetype.BuiltinLLM, TypeVer: nodetype.NodeTypeVersion1}},
		Edges: []workflow.Edge{{ID: "e1", From: "a", FromPort: workflow.PortDefault, To: "ghost"}},
	}
	res := ValidateForPublish(dsl, &fakeRegistry{types: builtinTypes()})
	codes := codeSet(res)
	for _, want := range []string{CodeMissingStart, CodeMissingEnd, CodeDanglingEdge} {
		if !codes[want] {
			t.Errorf("missing code %q in %v", want, codes)
		}
	}
}

func TestOutputPortsOfStaticNode(t *testing.T) {
	reg := newFakeRegistryWithBuiltins(t)
	node := &workflow.Node{TypeKey: nodetype.BuiltinIf}
	got := outputPortsOf(node, reg)
	want := []string{nodetype.PortIfTrue, nodetype.PortIfFalse, workflow.PortError}
	if !equalStringSet(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestOutputPortsOfSwitchUsesCases(t *testing.T) {
	reg := newFakeRegistryWithBuiltins(t)
	node := &workflow.Node{
		TypeKey: nodetype.BuiltinSwitch,
		Config:  json.RawMessage(`{"cases":[{"name":"hot"},{"name":"cold"}]}`),
	}
	got := outputPortsOf(node, reg)
	want := []string{"hot", "cold", workflow.PortDefault, workflow.PortError}
	if !equalStringSet(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestSingleStartViolation(t *testing.T) {
	dsl := workflow.WorkflowDSL{
		Nodes: []workflow.Node{
			{ID: "s1", TypeKey: nodetype.BuiltinStart},
			{ID: "s2", TypeKey: nodetype.BuiltinStart},
			{ID: "e", TypeKey: nodetype.BuiltinEnd},
		},
		Edges: []workflow.Edge{{ID: "e1", From: "s1", FromPort: "default", To: "e"}},
	}
	errs := checkSingleStart(dsl)
	if len(errs) == 0 || errs[0].Code != CodeMultipleStarts {
		t.Fatalf("expected CodeMultipleStarts, got %v", errs)
	}
}

func TestNoPathToEndDetected(t *testing.T) {
	dsl := workflow.WorkflowDSL{
		Nodes: []workflow.Node{
			{ID: "s", TypeKey: nodetype.BuiltinStart},
			{ID: "n", TypeKey: nodetype.BuiltinSetVariable},
			{ID: "e", TypeKey: nodetype.BuiltinEnd},
		},
		Edges: []workflow.Edge{{ID: "e1", From: "s", FromPort: "default", To: "n"}},
	}
	errs := checkNoPathToEnd(dsl)
	if len(errs) == 0 || errs[0].Code != CodeNoPathToEnd {
		t.Fatalf("expected CodeNoPathToEnd, got %v", errs)
	}
}

func TestPathToEndPasses(t *testing.T) {
	dsl := workflow.WorkflowDSL{
		Nodes: []workflow.Node{{ID: "s", TypeKey: nodetype.BuiltinStart}, {ID: "e", TypeKey: nodetype.BuiltinEnd}},
		Edges: []workflow.Edge{{ID: "e1", From: "s", FromPort: "default", To: "e"}},
	}
	if errs := checkNoPathToEnd(dsl); len(errs) != 0 {
		t.Fatalf("unexpected: %v", errs)
	}
}

func TestFireAndForgetSibling_Passes(t *testing.T) {
	dsl := workflow.WorkflowDSL{
		Nodes: []workflow.Node{
			{ID: "s", TypeKey: nodetype.BuiltinStart},
			{ID: "side", TypeKey: nodetype.BuiltinSetVariable},
			{ID: "e", TypeKey: nodetype.BuiltinEnd},
		},
		Edges: []workflow.Edge{
			{ID: "e1", From: "s", FromPort: "default", To: "side"},
			{ID: "e2", From: "s", FromPort: "default", To: "e"},
		},
	}
	if errs := checkNoPathToEnd(dsl); len(errs) != 0 {
		t.Fatalf("fire-and-forget sibling must be legal: %v", errs)
	}
}

func TestIsolatedNodeDetected(t *testing.T) {
	dsl := workflow.WorkflowDSL{
		Nodes: []workflow.Node{
			{ID: "s", TypeKey: nodetype.BuiltinStart},
			{ID: "orphan", TypeKey: nodetype.BuiltinSetVariable},
			{ID: "e", TypeKey: nodetype.BuiltinEnd},
		},
		Edges: []workflow.Edge{{ID: "e1", From: "s", FromPort: "default", To: "e"}},
	}
	errs := checkIsolatedNode(dsl)
	if len(errs) == 0 || errs[0].Code != CodeIsolatedNode || errs[0].NodeID != "orphan" {
		t.Fatalf("expected CodeIsolatedNode for orphan, got %v", errs)
	}
}

func TestStartIsNotIsolated(t *testing.T) {
	dsl := workflow.WorkflowDSL{
		Nodes: []workflow.Node{{ID: "s", TypeKey: nodetype.BuiltinStart}, {ID: "e", TypeKey: nodetype.BuiltinEnd}},
		Edges: []workflow.Edge{{ID: "e1", From: "s", FromPort: "default", To: "e"}},
	}
	if errs := checkIsolatedNode(dsl); len(errs) != 0 {
		t.Fatalf("Start with no inbound is allowed: %v", errs)
	}
}

func TestMultiInputRequiresJoinViolation(t *testing.T) {
	dsl := workflow.WorkflowDSL{
		Nodes: []workflow.Node{
			{ID: "s", TypeKey: nodetype.BuiltinStart},
			{ID: "a", TypeKey: nodetype.BuiltinSetVariable},
			{ID: "b", TypeKey: nodetype.BuiltinSetVariable},
			{ID: "merge", TypeKey: nodetype.BuiltinSetVariable},
			{ID: "e", TypeKey: nodetype.BuiltinEnd},
		},
		Edges: []workflow.Edge{
			{ID: "e1", From: "s", FromPort: "default", To: "a"},
			{ID: "e2", From: "s", FromPort: "default", To: "b"},
			{ID: "e3", From: "a", FromPort: "default", To: "merge"},
			{ID: "e4", From: "b", FromPort: "default", To: "merge"},
			{ID: "e5", From: "merge", FromPort: "default", To: "e"},
		},
	}
	errs := checkMultiInputRequiresJoin(dsl)
	if len(errs) == 0 || errs[0].Code != CodeMultiInputRequiresJoin || errs[0].NodeID != "merge" {
		t.Fatalf("expected CodeMultiInputRequiresJoin for merge, got %v", errs)
	}
}

func TestJoinWithMultiInputPasses(t *testing.T) {
	dsl := workflow.WorkflowDSL{
		Nodes: []workflow.Node{
			{ID: "s", TypeKey: nodetype.BuiltinStart},
			{ID: "a", TypeKey: nodetype.BuiltinSetVariable},
			{ID: "b", TypeKey: nodetype.BuiltinSetVariable},
			{ID: "j", TypeKey: nodetype.BuiltinJoin, Config: json.RawMessage(`{"mode":"any"}`)},
			{ID: "e", TypeKey: nodetype.BuiltinEnd},
		},
		Edges: []workflow.Edge{
			{ID: "e1", From: "s", FromPort: "default", To: "a"},
			{ID: "e2", From: "s", FromPort: "default", To: "b"},
			{ID: "e3", From: "a", FromPort: "default", To: "j"},
			{ID: "e4", From: "b", FromPort: "default", To: "j"},
			{ID: "e5", From: "j", FromPort: "default", To: "e"},
		},
	}
	if errs := checkMultiInputRequiresJoin(dsl); len(errs) != 0 {
		t.Fatalf("unexpected: %v", errs)
	}
}

func TestJoinSingleInput_Reports(t *testing.T) {
	dsl := workflow.WorkflowDSL{
		Nodes: []workflow.Node{{ID: "s", TypeKey: nodetype.BuiltinStart}, {ID: "j", TypeKey: nodetype.BuiltinJoin, Config: json.RawMessage(`{"mode":"any"}`)}, {ID: "e", TypeKey: nodetype.BuiltinEnd}},
		Edges: []workflow.Edge{{ID: "e1", From: "s", FromPort: "default", To: "j"}, {ID: "e2", From: "j", FromPort: "default", To: "e"}},
	}
	errs := checkJoin(dsl)
	if !containsCode(errs, CodeJoinInsufficientInputs) {
		t.Fatalf("expected CodeJoinInsufficientInputs, got %v", errs)
	}
}

func TestJoinModeInvalid_Reports(t *testing.T) {
	dsl := joinDSLWithConfig(json.RawMessage(`{"mode":"first"}`))
	errs := checkJoin(dsl)
	if !containsCode(errs, CodeJoinModeInvalid) {
		t.Fatalf("expected CodeJoinModeInvalid, got %v", errs)
	}
}

func TestJoinConfigInvalid_Reports(t *testing.T) {
	dsl := joinDSLWithConfig(json.RawMessage(`{"mode": 123}`))
	errs := checkJoin(dsl)
	if !containsCode(errs, CodeJoinConfigInvalid) {
		t.Fatalf("expected CodeJoinConfigInvalid, got %v", errs)
	}
}

func TestSwitchCaseDuplicate(t *testing.T) {
	dsl := workflow.WorkflowDSL{
		Nodes: []workflow.Node{
			{ID: "s", TypeKey: nodetype.BuiltinStart},
			{ID: "sw", TypeKey: nodetype.BuiltinSwitch, Config: json.RawMessage(`{"cases":[{"name":"hot"},{"name":"hot"}]}`)},
			{ID: "e", TypeKey: nodetype.BuiltinEnd},
		},
		Edges: []workflow.Edge{{ID: "e1", From: "s", FromPort: "default", To: "sw"}, {ID: "e2", From: "sw", FromPort: "hot", To: "e"}},
	}
	errs := checkSwitchCaseNames(dsl)
	if !containsCode(errs, CodeSwitchCaseNameDuplicate) {
		t.Fatalf("expected CodeSwitchCaseNameDuplicate, got %v", errs)
	}
}

func TestSwitchCaseReservedName(t *testing.T) {
	cases := []string{"default", "error", "1foo", "with space", ""}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := fmt.Sprintf(`{"cases":[{"name":%q}]}`, name)
			dsl := workflow.WorkflowDSL{Nodes: []workflow.Node{{ID: "sw", TypeKey: nodetype.BuiltinSwitch, Config: json.RawMessage(cfg)}}}
			errs := checkSwitchCaseNames(dsl)
			if !containsCode(errs, CodeSwitchCaseNameReserved) {
				t.Fatalf("expected CodeSwitchCaseNameReserved for %q, got %v", name, errs)
			}
		})
	}
}

func TestFallbackPortMissing(t *testing.T) {
	dsl := fallbackPortDSL("")
	errs := checkFallbackPort(dsl, nodetype.NewBuiltinRegistry())
	if !containsCode(errs, CodeFallbackPortInvalid) {
		t.Fatalf("expected CodeFallbackPortInvalid (missing), got %v", errs)
	}
}

func TestFallbackPortNotInOutputs(t *testing.T) {
	dsl := fallbackPortDSL("phantom")
	errs := checkFallbackPort(dsl, nodetype.NewBuiltinRegistry())
	if !containsCode(errs, CodeFallbackPortInvalid) {
		t.Fatalf("expected CodeFallbackPortInvalid (phantom), got %v", errs)
	}
}

func TestFireErrorPortRequiresErrorPort(t *testing.T) {
	dsl := workflow.WorkflowDSL{
		Nodes: []workflow.Node{
			{ID: "s", TypeKey: nodetype.BuiltinStart},
			{ID: "sv", TypeKey: nodetype.BuiltinSetVariable, ErrorPolicy: &workflow.ErrorPolicy{OnFinalFail: workflow.FailStrategyFireErrorPort}},
			{ID: "e", TypeKey: nodetype.BuiltinEnd},
		},
		Edges: []workflow.Edge{{ID: "e1", From: "s", FromPort: "default", To: "sv"}, {ID: "e2", From: "sv", FromPort: "default", To: "e"}},
	}
	errs := checkFireErrorPortRequiresErrorPort(dsl, nodetype.NewBuiltinRegistry())
	if !containsCode(errs, CodeFireErrorPortRequiresErrorPort) {
		t.Fatalf("expected CodeFireErrorPortRequiresErrorPort, got %v", errs)
	}
}

func TestFireErrorPortOnHTTPNodePasses(t *testing.T) {
	dsl := workflow.WorkflowDSL{
		Nodes: []workflow.Node{
			{ID: "s", TypeKey: nodetype.BuiltinStart},
			{ID: "h", TypeKey: nodetype.BuiltinHTTPRequest, ErrorPolicy: &workflow.ErrorPolicy{OnFinalFail: workflow.FailStrategyFireErrorPort}},
			{ID: "e", TypeKey: nodetype.BuiltinEnd},
		},
		Edges: []workflow.Edge{{ID: "e1", From: "s", FromPort: "default", To: "h"}, {ID: "e2", From: "h", FromPort: "default", To: "e"}},
	}
	if errs := checkFireErrorPortRequiresErrorPort(dsl, nodetype.NewBuiltinRegistry()); len(errs) != 0 {
		t.Fatalf("http_request has error port: %v", errs)
	}
}

func TestValidate_HappyPathWithAllNewRulesPasses(t *testing.T) {
	reg := nodetype.NewBuiltinRegistry()
	dsl := workflow.WorkflowDSL{
		Nodes: []workflow.Node{
			{ID: "s", TypeKey: nodetype.BuiltinStart},
			{ID: "h", TypeKey: nodetype.BuiltinHTTPRequest},
			{ID: "i", TypeKey: nodetype.BuiltinIf, Inputs: map[string]workflow.ValueSource{
				"left": {Kind: workflow.ValueKindLiteral, Value: 1}, "right": {Kind: workflow.ValueKindLiteral, Value: 1},
			}, Config: json.RawMessage(`{"operator":"eq"}`)},
			{ID: "j", TypeKey: nodetype.BuiltinJoin, Config: json.RawMessage(`{"mode":"any"}`)},
			{ID: "e", TypeKey: nodetype.BuiltinEnd},
		},
		Edges: []workflow.Edge{
			{ID: "e1", From: "s", FromPort: "default", To: "h"},
			{ID: "e2", From: "h", FromPort: "default", To: "i"},
			{ID: "e3", From: "i", FromPort: "true", To: "j"},
			{ID: "e4", From: "i", FromPort: "false", To: "j"},
			{ID: "e5", From: "j", FromPort: "default", To: "e"},
		},
	}
	if errs := Validate(dsl, reg); len(errs) != 0 {
		t.Fatalf("happy path should validate: %v", errs)
	}
}

func TestValidate_RejectsMultipleViolations(t *testing.T) {
	reg := nodetype.NewBuiltinRegistry()
	dsl := workflow.WorkflowDSL{
		Nodes: []workflow.Node{
			{ID: "s1", TypeKey: nodetype.BuiltinStart},
			{ID: "s2", TypeKey: nodetype.BuiltinStart},
			{ID: "orphan", TypeKey: nodetype.BuiltinSetVariable},
			{ID: "e", TypeKey: nodetype.BuiltinEnd},
		},
		Edges: []workflow.Edge{{ID: "e1", From: "s1", FromPort: "default", To: "e"}},
	}
	errs := Validate(dsl, reg)
	if !containsCode(errs, CodeMultipleStarts) || !containsCode(errs, CodeIsolatedNode) {
		t.Fatalf("expected both multiple_starts and isolated_node, got %v", errs)
	}
}

func joinDSLWithConfig(config json.RawMessage) workflow.WorkflowDSL {
	return workflow.WorkflowDSL{
		Nodes: []workflow.Node{
			{ID: "s", TypeKey: nodetype.BuiltinStart},
			{ID: "a", TypeKey: nodetype.BuiltinSetVariable},
			{ID: "b", TypeKey: nodetype.BuiltinSetVariable},
			{ID: "j", TypeKey: nodetype.BuiltinJoin, Config: config},
			{ID: "e", TypeKey: nodetype.BuiltinEnd},
		},
		Edges: []workflow.Edge{
			{ID: "e1", From: "s", FromPort: "default", To: "a"},
			{ID: "e2", From: "s", FromPort: "default", To: "b"},
			{ID: "e3", From: "a", FromPort: "default", To: "j"},
			{ID: "e4", From: "b", FromPort: "default", To: "j"},
			{ID: "e5", From: "j", FromPort: "default", To: "e"},
		},
	}
}

func fallbackPortDSL(port string) workflow.WorkflowDSL {
	return workflow.WorkflowDSL{
		Nodes: []workflow.Node{
			{ID: "s", TypeKey: nodetype.BuiltinStart},
			{ID: "n", TypeKey: nodetype.BuiltinHTTPRequest, ErrorPolicy: &workflow.ErrorPolicy{
				OnFinalFail: workflow.FailStrategyFallback,
				FallbackOutput: workflow.FallbackOutput{Port: port, Output: map[string]any{"k": 1}},
			}},
			{ID: "e", TypeKey: nodetype.BuiltinEnd},
		},
		Edges: []workflow.Edge{{ID: "e1", From: "s", FromPort: "default", To: "n"}, {ID: "e2", From: "n", FromPort: "default", To: "e"}},
	}
}

func newFakeRegistryWithBuiltins(t *testing.T) nodetype.NodeTypeRegistry {
	t.Helper()
	return nodetype.NewBuiltinRegistry()
}

func equalStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	m := map[string]int{}
	for _, s := range a {
		m[s]++
	}
	for _, s := range b {
		m[s]--
	}
	for _, n := range m {
		if n != 0 {
			return false
		}
	}
	return true
}

func containsCode(errs []ValidationError, code string) bool {
	for _, e := range errs {
		if e.Code == code {
			return true
		}
	}
	return false
}

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
