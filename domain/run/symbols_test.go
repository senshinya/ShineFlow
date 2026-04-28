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
