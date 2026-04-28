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
