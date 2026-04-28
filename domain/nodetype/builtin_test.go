package nodetype

import "testing"

func TestJoinBuiltins(t *testing.T) {
	cases := map[string]string{
		"builtin.join": BuiltinJoin,
		"any":          JoinModeAny,
		"all":          JoinModeAll,
	}
	for want, got := range cases {
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	}
}
