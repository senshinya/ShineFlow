package workflow

import (
	"encoding/json"
	"testing"
)

func TestFallbackOutputJSONRoundTrip(t *testing.T) {
	in := ErrorPolicy{
		OnFinalFail: FailStrategyFallback,
		FallbackOutput: FallbackOutput{
			Port:   "default",
			Output: map[string]any{"text": "sorry"},
		},
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out ErrorPolicy
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.FallbackOutput.Port != "default" {
		t.Fatalf("port: got %q", out.FallbackOutput.Port)
	}
	if v, _ := out.FallbackOutput.Output["text"].(string); v != "sorry" {
		t.Fatalf("output.text: got %v", out.FallbackOutput.Output["text"])
	}
}
