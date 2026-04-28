package nodetype

import "testing"

func TestNewBuiltinRegistryHasAllEightKeys(t *testing.T) {
	r := NewBuiltinRegistry()
	expectKeys := []string{
		BuiltinStart, BuiltinEnd, BuiltinLLM, BuiltinIf, BuiltinSwitch,
		BuiltinJoin, BuiltinSetVariable, BuiltinHTTPRequest,
	}
	for _, k := range expectKeys {
		if _, ok := r.Get(k); !ok {
			t.Errorf("missing builtin: %s", k)
		}
	}
}

func TestNewBuiltinRegistryDoesNotHaveLoop(t *testing.T) {
	r := NewBuiltinRegistry()
	if _, ok := r.Get(BuiltinLoop); ok {
		t.Error("BuiltinLoop should NOT be in catalog (out-of-scope)")
	}
}

func TestNewBuiltinRegistryDoesNotHaveCode(t *testing.T) {
	r := NewBuiltinRegistry()
	if _, ok := r.Get(BuiltinCode); ok {
		t.Error("BuiltinCode should NOT be in catalog (out-of-scope)")
	}
}

func TestNewBuiltinRegistryReturnsIsolatedNodeTypes(t *testing.T) {
	r1 := NewBuiltinRegistry()
	nt, ok := r1.Get(BuiltinLLM)
	if !ok {
		t.Fatal("missing builtin.llm")
	}
	nt.Ports[0] = "mutated"

	again, _ := r1.Get(BuiltinLLM)
	if again.Ports[0] == "mutated" {
		t.Fatal("same registry returned mutated NodeType")
	}

	r2 := NewBuiltinRegistry()
	fresh, _ := r2.Get(BuiltinLLM)
	if fresh.Ports[0] == "mutated" {
		t.Fatal("new registry reused mutated NodeType")
	}
}
