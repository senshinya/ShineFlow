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
