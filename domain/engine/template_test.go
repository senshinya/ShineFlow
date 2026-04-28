package engine

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/shinya/shineflow/domain/run"
)

func mustSym(t *testing.T, payload string) *run.Symbols {
	t.Helper()
	s, err := run.NewSymbols(json.RawMessage(payload))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestExpandWholeStringPreservesType(t *testing.T) {
	sym := mustSym(t, `{"count":42,"flag":true,"data":{"a":1}}`)

	got, err := ExpandTemplate("{{trigger.count}}", sym)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := got.(float64); v != 42 {
		t.Fatalf("count: %v", got)
	}

	got, err = ExpandTemplate("{{trigger.flag}}", sym)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := got.(bool); !v {
		t.Fatalf("flag: %v", got)
	}

	got, err = ExpandTemplate("{{trigger.data}}", sym)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got.(map[string]any); !ok {
		t.Fatalf("data type: %T", got)
	}
}

func TestExpandSubstringStringifies(t *testing.T) {
	sym := mustSym(t, `{"count":42,"name":"alice"}`)

	got, err := ExpandTemplate("#{{trigger.count}}", sym)
	if err != nil {
		t.Fatal(err)
	}
	if got != "#42" {
		t.Fatalf("got %q", got)
	}

	got, err = ExpandTemplate("hello {{trigger.name}}!", sym)
	if err != nil {
		t.Fatal(err)
	}
	if got != "hello alice!" {
		t.Fatalf("got %q", got)
	}
}

func TestExpandFloat64IntegerNoDecimalTail(t *testing.T) {
	sym := mustSym(t, `{"x":42}`)
	got, err := ExpandTemplate("v={{trigger.x}}", sym)
	if err != nil {
		t.Fatal(err)
	}
	if got != "v=42" {
		t.Fatalf("got %q", got)
	}
}

func TestExpandMapInSubstringJSON(t *testing.T) {
	sym := mustSym(t, `{"d":{"a":1}}`)
	got, err := ExpandTemplate("data={{trigger.d}}", sym)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.(string), `{"a":1}`) {
		t.Fatalf("got %q", got)
	}
}

func TestExpandStrictReportsError(t *testing.T) {
	sym := mustSym(t, `{}`)
	_, err := ExpandTemplate("{{trigger.missing}}", sym)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), `template "{{trigger.missing}}"`) {
		t.Fatalf("error: %v", err)
	}
}

func TestExpandLiteralPassthrough(t *testing.T) {
	sym := mustSym(t, `{}`)
	got, err := ExpandTemplate("plain text", sym)
	if err != nil {
		t.Fatal(err)
	}
	if got != "plain text" {
		t.Fatalf("got %v", got)
	}
}
