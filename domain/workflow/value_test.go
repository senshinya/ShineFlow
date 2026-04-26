package workflow

import (
	"testing"

	"github.com/shinya/shineflow/infrastructure/util"
)

func TestValueSource_RoundTrip_Ref(t *testing.T) {
	src := ValueSource{
		Kind: ValueKindRef,
		Value: RefValue{
			NodeID: "n_start",
			PortID: "p_out",
			Path:   "data.url",
			Name:   "voice url",
		},
	}
	s, err := util.MarshalToString(src)
	if err != nil { t.Fatalf("marshal: %v", err) }

	var got ValueSource
	if err := util.UnmarshalFromString(s, &got); err != nil { t.Fatalf("unmarshal: %v", err) }

	if got.Kind != ValueKindRef { t.Fatalf("kind: got %q", got.Kind) }
	ref, ok := got.Value.(RefValue)
	if !ok { t.Fatalf("value type: %T", got.Value) }
	if ref.NodeID != "n_start" || ref.PortID != "p_out" || ref.Path != "data.url" {
		t.Fatalf("ref roundtrip mismatch: %+v", ref)
	}
}

func TestValueSource_RoundTrip_Literal(t *testing.T) {
	src := ValueSource{Kind: ValueKindLiteral, Value: "hello"}
	s, err := util.MarshalToString(src)
	if err != nil { t.Fatal(err) }
	var got ValueSource
	if err := util.UnmarshalFromString(s, &got); err != nil { t.Fatal(err) }
	if got.Kind != ValueKindLiteral || got.Value != "hello" {
		t.Fatalf("literal roundtrip: %+v", got)
	}
}

func TestValueSource_RoundTrip_Template(t *testing.T) {
	src := ValueSource{Kind: ValueKindTemplate, Value: "Hello {{name}}"}
	s, err := util.MarshalToString(src)
	if err != nil { t.Fatal(err) }
	var got ValueSource
	if err := util.UnmarshalFromString(s, &got); err != nil { t.Fatal(err) }
	if got.Kind != ValueKindTemplate || got.Value != "Hello {{name}}" {
		t.Fatalf("template roundtrip: %+v", got)
	}
}

func TestValueSource_UnknownKind_Errors(t *testing.T) {
	var got ValueSource
	err := util.UnmarshalFromString(`{"kind":"weird","value":1}`, &got)
	if err == nil { t.Fatal("expected error for unknown kind") }
}
