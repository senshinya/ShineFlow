package executor

import (
	"context"
	"testing"
)

func TestExecServicesHasLLMClient(t *testing.T) {
	var s ExecServices
	// 编译期检查：字段存在且类型可赋 nil。
	s.LLMClient = nil
	_ = s
}

type fakeLLM struct{}

func (fakeLLM) Complete(_ context.Context, _ LLMRequest) (LLMResponse, error) {
	return LLMResponse{Text: "hi", Model: "m1", Usage: LLMUsage{InputTokens: 1, OutputTokens: 2}}, nil
}

func TestLLMClientInterfaceShape(t *testing.T) {
	var _ LLMClient = fakeLLM{}
}
