package builtin

import (
	"testing"

	"github.com/shinya/shineflow/domain/executor"
	"github.com/shinya/shineflow/domain/nodetype"
)

type fakeRegistry struct {
	registered map[string]bool
}

func (r *fakeRegistry) Register(keyPattern string, _ executor.ExecutorFactory) {
	if r.registered == nil {
		r.registered = map[string]bool{}
	}
	r.registered[keyPattern] = true
}

func (r *fakeRegistry) Build(_ *nodetype.NodeType) (executor.NodeExecutor, error) {
	return nil, nil
}

func TestRegisterInstallsAllEightFactories(t *testing.T) {
	r := &fakeRegistry{}
	Register(r)
	want := []string{
		nodetype.BuiltinStart,
		nodetype.BuiltinEnd,
		nodetype.BuiltinLLM,
		nodetype.BuiltinIf,
		nodetype.BuiltinSwitch,
		nodetype.BuiltinJoin,
		nodetype.BuiltinSetVariable,
		nodetype.BuiltinHTTPRequest,
	}
	for _, k := range want {
		if !r.registered[k] {
			t.Errorf("missing registration: %s", k)
		}
	}
	if len(r.registered) != len(want) {
		t.Errorf("registered %d, want %d", len(r.registered), len(want))
	}
}
