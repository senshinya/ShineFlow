package run

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNewSymbolsAcceptsObject(t *testing.T) {
	s, err := NewSymbols(json.RawMessage(`{"user_id":"u1"}`))
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if s == nil {
		t.Fatal("nil symbols")
	}
}

func TestNewSymbolsAcceptsEmpty(t *testing.T) {
	s, err := NewSymbols(nil)
	if err != nil {
		t.Fatalf("nil payload should default to {}: %v", err)
	}
	_ = s

	s, err = NewSymbols(json.RawMessage(``))
	if err != nil {
		t.Fatalf("empty payload should default to {}: %v", err)
	}
	_ = s
}

func TestNewSymbolsRejectsNonObject(t *testing.T) {
	cases := []string{`42`, `"hello"`, `true`, `null`, `[1,2,3]`}
	for _, c := range cases {
		t.Run(c, func(t *testing.T) {
			_, err := NewSymbols(json.RawMessage(c))
			if err == nil {
				t.Fatalf("expected error for %q", c)
			}
			if !strings.Contains(err.Error(), "trigger payload must be a JSON object") {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestSetNodeOutput(t *testing.T) {
	s, _ := NewSymbols(nil)
	if err := s.SetNodeOutput("n1", map[string]any{"foo": "bar", "n": 42}); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.nodes["n1"]; !ok {
		t.Fatal("n1 missing")
	}
}

func TestSetNodeOutputNilTreatedAsEmpty(t *testing.T) {
	s, _ := NewSymbols(nil)
	if err := s.SetNodeOutput("n1", nil); err != nil {
		t.Fatal(err)
	}
	raw := s.nodes["n1"]
	if string(raw) != `{}` {
		t.Fatalf("expected {}, got %s", raw)
	}
}

func TestSetNodeOutputRejectsUnserializable(t *testing.T) {
	s, _ := NewSymbols(nil)
	err := s.SetNodeOutput("n1", map[string]any{"ch": make(chan int)})
	if err == nil {
		t.Fatal("expected marshal error")
	}
}

func TestSetVarAndSnapshotVars(t *testing.T) {
	s, _ := NewSymbols(nil)
	if err := s.SetVar("x", 42); err != nil {
		t.Fatal(err)
	}
	if err := s.SetVar("y", "hello"); err != nil {
		t.Fatal(err)
	}
	snap := s.SnapshotVars()
	if len(snap) != 2 {
		t.Fatalf("want 2, got %d", len(snap))
	}
	if string(snap["x"]) != `42` || string(snap["y"]) != `"hello"` {
		t.Fatalf("snap: %v", snap)
	}
	snap["x"] = json.RawMessage(`999`)
	if string(s.SnapshotVars()["x"]) != `42` {
		t.Fatal("snapshot leak: original mutated")
	}
}

func TestSnapshotMapHeaderForked(t *testing.T) {
	s, _ := NewSymbols(nil)
	_ = s.SetVar("v", 1)
	snap := s.Snapshot()
	_ = s.SetVar("v", 999)

	if string(snap.SnapshotVars()["v"]) != `1` {
		t.Fatalf("snap vars.v: %s", snap.SnapshotVars()["v"])
	}
}

func TestLookupTrigger(t *testing.T) {
	s, _ := NewSymbols(json.RawMessage(`{"user_id":"u1","count":42}`))
	got, err := s.Lookup("trigger.user_id")
	if err != nil {
		t.Fatal(err)
	}
	if got != "u1" {
		t.Fatalf("got %v", got)
	}
	got, _ = s.Lookup("trigger.count")
	if v, _ := got.(float64); v != 42 {
		t.Fatalf("got %v", got)
	}
}

func TestLookupVarsAndNodes(t *testing.T) {
	s, _ := NewSymbols(nil)
	_ = s.SetVar("x", 7)
	_ = s.SetNodeOutput("n1", map[string]any{"items": []any{"a", "b"}})

	got, _ := s.Lookup("vars.x")
	if v, _ := got.(float64); v != 7 {
		t.Fatalf("vars.x: %v", got)
	}

	got, err := s.Lookup("nodes.n1.items.1")
	if err != nil {
		t.Fatal(err)
	}
	if got != "b" {
		t.Fatalf("got %v", got)
	}
}

func TestLookupErrors(t *testing.T) {
	s, _ := NewSymbols(json.RawMessage(`{"a":{"b":1}}`))
	_ = s.SetNodeOutput("n1", map[string]any{"k": 1})

	cases := []struct {
		path, contains string
	}{
		{"", "empty path"},
		{"unknown.x", "unknown root"},
		{"nodes", "nodes.<id> required"},
		{"nodes.missing.x", "node not yet produced output"},
		{"vars.missing", "var not set"},
		{"vars", "vars.<key> required"},
		{"trigger.a.b.c", "cannot navigate"},
		{"nodes.n1.k.0", "cannot navigate"},
	}
	for _, c := range cases {
		t.Run(c.path, func(t *testing.T) {
			_, err := s.Lookup(c.path)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), c.contains) {
				t.Fatalf("got %v, want contains %q", err, c.contains)
			}
		})
	}
}

func TestSnapshotIsolatedFromOriginal(t *testing.T) {
	s, _ := NewSymbols(json.RawMessage(`{"a":1}`))
	_ = s.SetVar("v", 1)
	_ = s.SetNodeOutput("n1", map[string]any{"k": 1})

	snap := s.Snapshot()

	_ = s.SetVar("v", 999)
	_ = s.SetNodeOutput("n1", map[string]any{"k": 999})
	_ = s.SetNodeOutput("n2", map[string]any{"new": true})

	got, err := snap.Lookup("vars.v")
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := got.(float64); v != 1 {
		t.Fatalf("snap vars.v: %v", got)
	}
	got, err = snap.Lookup("nodes.n1.k")
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := got.(float64); v != 1 {
		t.Fatalf("snap nodes.n1.k: %v", got)
	}
	if _, err := snap.Lookup("nodes.n2.new"); err == nil {
		t.Fatal("snap should not see n2 added after Snapshot()")
	}
}

func TestFromPersistedState(t *testing.T) {
	rn := &WorkflowRun{
		TriggerPayload: json.RawMessage(`{"u":"u1"}`),
		Vars:           json.RawMessage(`{"v":42}`),
	}
	nrSuccess := &NodeRun{NodeID: "n1", Status: NodeRunStatusSuccess, Output: json.RawMessage(`{"out":1}`)}
	nrFallback := &NodeRun{NodeID: "n2", Status: NodeRunStatusFailed, FallbackApplied: true, Output: json.RawMessage(`{"fb":2}`)}
	nrFailed := &NodeRun{NodeID: "n3", Status: NodeRunStatusFailed, Output: json.RawMessage(`{"x":9}`)}
	nrSkipped := &NodeRun{NodeID: "n4", Status: NodeRunStatusSkipped}

	s, err := FromPersistedState(rn, []*NodeRun{nrSuccess, nrFallback, nrFailed, nrSkipped})
	if err != nil {
		t.Fatal(err)
	}

	got, _ := s.Lookup("trigger.u")
	if got != "u1" {
		t.Fatalf("trigger.u: %v", got)
	}
	got, _ = s.Lookup("vars.v")
	if v, _ := got.(float64); v != 42 {
		t.Fatalf("vars.v: %v", got)
	}
	got, _ = s.Lookup("nodes.n1.out")
	if v, _ := got.(float64); v != 1 {
		t.Fatalf("nodes.n1.out: %v", got)
	}
	got, _ = s.Lookup("nodes.n2.fb")
	if v, _ := got.(float64); v != 2 {
		t.Fatalf("nodes.n2 fallback should be visible: %v", got)
	}
	if _, err := s.Lookup("nodes.n3.x"); err == nil {
		t.Fatal("plain failed node must not be visible")
	}
	if _, err := s.Lookup("nodes.n4.anything"); err == nil {
		t.Fatal("skipped node must not be visible")
	}
}

func TestFromPersistedStateLaterVisibleRowWins(t *testing.T) {
	rn := &WorkflowRun{TriggerPayload: json.RawMessage(`{}`)}
	nodeRuns := []*NodeRun{
		{NodeID: "n1", Attempt: 1, Status: NodeRunStatusSuccess, Output: json.RawMessage(`{"text":"v1"}`)},
		{NodeID: "n1", Attempt: 2, Status: NodeRunStatusSuccess, Output: json.RawMessage(`{"text":"v2"}`)},
		{NodeID: "n2", Attempt: 1, Status: NodeRunStatusSuccess, Output: json.RawMessage(`{"text":"visible"}`)},
		{NodeID: "n2", Attempt: 2, Status: NodeRunStatusFailed, Output: json.RawMessage(`{"text":"failed"}`)},
	}

	s, err := FromPersistedState(rn, nodeRuns)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := s.Lookup("nodes.n1.text")
	if got != "v2" {
		t.Fatalf("n1.text = %v, want v2", got)
	}
	got, _ = s.Lookup("nodes.n2.text")
	if got != "visible" {
		t.Fatalf("n2.text = %v, want visible", got)
	}
}

func TestFromPersistedStateBadTriggerJSON(t *testing.T) {
	rn := &WorkflowRun{TriggerPayload: json.RawMessage(`not json`)}
	if _, err := FromPersistedState(rn, nil); err == nil {
		t.Fatal("expected error on bad JSON")
	}
}
