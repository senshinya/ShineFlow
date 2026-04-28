package builtin

import (
	"encoding/json"
	"testing"
)

func TestCoerceFloat64(t *testing.T) {
	cases := []struct {
		name   string
		in     any
		want   float64
		wantOK bool
	}{
		{"float64", float64(3.14), 3.14, true},
		{"float32", float32(2.5), 2.5, true},
		{"int", int(42), 42, true},
		{"int64", int64(-7), -7, true},
		{"json.Number", json.Number("12.5"), 12.5, true},
		{"numeric string", "100", 100, true},
		{"non-numeric string", "abc", 0, false},
		{"bool", true, 0, false},
		{"nil", nil, 0, false},
		{"map", map[string]any{}, 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := coerceFloat64(c.in)
			if ok != c.wantOK {
				t.Fatalf("ok: got %v want %v", ok, c.wantOK)
			}
			if ok && got != c.want {
				t.Fatalf("got %v want %v", got, c.want)
			}
		})
	}
}
