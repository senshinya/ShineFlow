package run

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestBuildContext_TriggerAndVars(t *testing.T) {
	wr := &WorkflowRun{
		TriggerPayload: json.RawMessage(`{"text":"hello","count":3}`),
		Vars:           json.RawMessage(`{"theme":"dark"}`),
	}
	ctx, err := BuildContext(wr, nil)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	want := map[string]any{
		"trigger.text":  "hello",
		"trigger.count": float64(3), // encoding/json 默认数字解为 float64
		"vars.theme":    "dark",
	}
	if !reflect.DeepEqual(ctx, want) {
		t.Errorf("ctx = %#v, want %#v", ctx, want)
	}
}

func TestBuildContext_NodesPicksLatestSuccess(t *testing.T) {
	wr := &WorkflowRun{TriggerPayload: json.RawMessage(`{}`)}
	nrs := []*NodeRun{
		{NodeID: "llm1", Attempt: 1, Status: NodeRunStatusFailed,
			Output: json.RawMessage(`{"text":"v1"}`)},
		{NodeID: "llm1", Attempt: 2, Status: NodeRunStatusSuccess,
			Output: json.RawMessage(`{"text":"v2"}`)},
		// 同 NodeID 的更早 attempt 即使 success 也应被更高 Attempt 覆盖
		{NodeID: "llm2", Attempt: 1, Status: NodeRunStatusSuccess,
			Output: json.RawMessage(`{"text":"a"}`)},
		{NodeID: "llm2", Attempt: 2, Status: NodeRunStatusFailed,
			Output: json.RawMessage(`{"text":"b"}`)},
	}
	ctx, err := BuildContext(wr, nrs)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if ctx["nodes.llm1.text"] != "v2" {
		t.Errorf("llm1.text = %v, want v2", ctx["nodes.llm1.text"])
	}
	// llm2 最新 attempt 是 failed，不计入 context
	if _, exists := ctx["nodes.llm2.text"]; exists {
		t.Errorf("llm2 latest attempt is failed, should not appear in context, got %v", ctx["nodes.llm2.text"])
	}
}

func TestBuildContext_FallbackCounted(t *testing.T) {
	wr := &WorkflowRun{TriggerPayload: json.RawMessage(`{}`)}
	nrs := []*NodeRun{
		// fallback 生效时 Status=Failed 但 Output 是 fallback 值，仍应进 context
		{NodeID: "n1", Attempt: 1, Status: NodeRunStatusFailed, FallbackApplied: true,
			Output: json.RawMessage(`{"text":"fb"}`), FiredPort: "default"},
	}
	ctx, err := BuildContext(wr, nrs)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if ctx["nodes.n1.text"] != "fb" {
		t.Errorf("fallback output should appear, got %v", ctx["nodes.n1.text"])
	}
}

func TestBuildContext_BadJSON(t *testing.T) {
	wr := &WorkflowRun{TriggerPayload: json.RawMessage(`not json`)}
	if _, err := BuildContext(wr, nil); err == nil {
		t.Error("expected error on bad JSON")
	}
}
