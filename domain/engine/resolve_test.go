package engine

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/shinya/shineflow/domain/nodetype"
	"github.com/shinya/shineflow/domain/run"
	"github.com/shinya/shineflow/domain/workflow"
)

func TestResolveLiteral(t *testing.T) {
	r := newTestResolver(t, nil)
	v, err := r.resolveOne(workflow.ValueSource{Kind: workflow.ValueKindLiteral, Value: 42}, mustSym(t, `{}`))
	if err != nil {
		t.Fatal(err)
	}
	if v != 42 {
		t.Fatalf("got %v", v)
	}
}

func TestResolveRefNoPortID(t *testing.T) {
	dsl := workflow.WorkflowDSL{Nodes: []workflow.Node{{ID: "n1", TypeKey: nodetype.BuiltinSetVariable}}}
	r := newTestResolver(t, &dsl)
	sym := mustSym(t, `{}`)
	if err := sym.SetNodeOutput("n1", map[string]any{"foo": "bar"}); err != nil {
		t.Fatal(err)
	}
	v, err := r.resolveOne(workflow.ValueSource{
		Kind:  workflow.ValueKindRef,
		Value: workflow.RefValue{NodeID: "n1", Path: "foo"},
	}, sym)
	if err != nil {
		t.Fatal(err)
	}
	if v != "bar" {
		t.Fatalf("got %v", v)
	}
}

func TestResolveRefMissingNode(t *testing.T) {
	dsl := workflow.WorkflowDSL{Nodes: []workflow.Node{}}
	r := newTestResolver(t, &dsl)
	_, err := r.resolveOne(workflow.ValueSource{
		Kind:  workflow.ValueKindRef,
		Value: workflow.RefValue{NodeID: "ghost"},
	}, mustSym(t, `{}`))
	if err == nil || !strings.Contains(err.Error(), "ref node not found") {
		t.Fatalf("got %v", err)
	}
}

func TestResolveTemplate(t *testing.T) {
	r := newTestResolver(t, nil)
	sym := mustSym(t, `{"x":"hello"}`)
	v, err := r.resolveOne(workflow.ValueSource{
		Kind:  workflow.ValueKindTemplate,
		Value: "world: {{trigger.x}}",
	}, sym)
	if err != nil {
		t.Fatal(err)
	}
	if v != "world: hello" {
		t.Fatalf("got %v", v)
	}
}

func TestResolveConfigTemplateRecursion(t *testing.T) {
	r := newTestResolver(t, nil)
	sym := mustSym(t, `{"host":"api.test"}`)
	cfg := json.RawMessage(`{"url":"https://{{trigger.host}}/v1","retries":3,"flag":true}`)
	out, err := r.ResolveConfig(cfg, sym)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := out["url"].(string)
	if !ok || got != "https://api.test/v1" {
		t.Fatalf("url: %v", out["url"])
	}
	if v, _ := out["retries"].(float64); v != 3 {
		t.Fatalf("retries: %v", out["retries"])
	}
	if v, _ := out["flag"].(bool); !v {
		t.Fatalf("flag: %v", out["flag"])
	}
}

func TestResolveErrorIncludesContext(t *testing.T) {
	r := newTestResolver(t, nil)
	cfg := json.RawMessage(`{"url":"{{trigger.missing}}"}`)
	_, err := r.ResolveConfig(cfg, mustSym(t, `{}`))
	if err == nil || !strings.Contains(err.Error(), "url") {
		t.Fatalf("error: %v", err)
	}
}

func TestResolveLenientTemplateKeepsMissingPlaceholder(t *testing.T) {
	r := newTestResolver(t, nil)
	r.mode = TemplateLenient
	cfg := json.RawMessage(`{"url":"https://{{trigger.missing}}/v1"}`)
	out, err := r.ResolveConfig(cfg, mustSym(t, `{}`))
	if err != nil {
		t.Fatal(err)
	}
	if out["url"] != "https://{{trigger.missing}}/v1" {
		t.Fatalf("url: %v", out["url"])
	}
}

func newTestResolver(t *testing.T, dsl *workflow.WorkflowDSL) *Resolver {
	t.Helper()
	if dsl == nil {
		empty := workflow.WorkflowDSL{}
		dsl = &empty
	}
	return &Resolver{dsl: dsl}
}

var _ = run.NewSymbols
