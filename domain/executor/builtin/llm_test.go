package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/shinya/shineflow/domain/executor"
	"github.com/shinya/shineflow/domain/workflow"
)

type fakeLLMResp struct {
	req  executor.LLMRequest
	resp executor.LLMResponse
	err  error
}

func (f *fakeLLMResp) Complete(_ context.Context, req executor.LLMRequest) (executor.LLMResponse, error) {
	f.req = req
	return f.resp, f.err
}

func TestLLMHappy(t *testing.T) {
	client := &fakeLLMResp{resp: executor.LLMResponse{Text: "hello", Model: "gpt-4", Usage: executor.LLMUsage{InputTokens: 5, OutputTokens: 1}}}
	exe := llmFactory(nil)
	out, err := exe.Execute(context.Background(), executor.ExecInput{
		Config: json.RawMessage(`{"provider":"openai","model":"gpt-4","temperature":0.5,"max_tokens":100}`),
		Inputs: map[string]any{"messages": []any{map[string]any{"role": "user", "content": "hi"}}},
		Services: executor.ExecServices{LLMClient: client},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.FiredPort != workflow.PortDefault {
		t.Fatalf("port: %s", out.FiredPort)
	}
	if out.Outputs["text"] != "hello" {
		t.Fatalf("text: %v", out.Outputs["text"])
	}
	if out.Outputs["model"] != "gpt-4" {
		t.Fatalf("model: %v", out.Outputs["model"])
	}
	if client.req.Model != "gpt-4" || len(client.req.Messages) != 1 || client.req.Messages[0].Content != "hi" {
		t.Fatalf("request: %+v", client.req)
	}
}

func TestLLMTransportErrPropagates(t *testing.T) {
	exe := llmFactory(nil)
	wantErr := errors.New("network down")
	_, err := exe.Execute(context.Background(), executor.ExecInput{
		Config:   json.RawMessage(`{"provider":"openai","model":"gpt-4"}`),
		Inputs:   map[string]any{"messages": []any{}},
		Services: executor.ExecServices{LLMClient: &fakeLLMResp{err: wantErr}},
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected wrapped %v, got %v", wantErr, err)
	}
}

func TestLLMClientNotConfigured(t *testing.T) {
	exe := llmFactory(nil)
	_, err := exe.Execute(context.Background(), executor.ExecInput{
		Config:   json.RawMessage(`{"provider":"openai","model":"gpt-4"}`),
		Inputs:   map[string]any{"messages": []any{}},
		Services: executor.ExecServices{LLMClient: nil},
	})
	if !errors.Is(err, ErrPortNotConfigured) {
		t.Fatalf("expected ErrPortNotConfigured, got %v", err)
	}
}

func TestLLMPromptOnly(t *testing.T) {
	client := &fakeLLMResp{resp: executor.LLMResponse{Text: "ok"}}
	exe := llmFactory(nil)
	out, err := exe.Execute(context.Background(), executor.ExecInput{
		Config:   json.RawMessage(`{"provider":"openai","model":"gpt-4","system_prompt":"You are helpful"}`),
		Inputs:   map[string]any{"prompt": "Translate"},
		Services: executor.ExecServices{LLMClient: client},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Outputs["text"] != "ok" {
		t.Fatalf("text: %v", out.Outputs["text"])
	}
	if len(client.req.Messages) != 2 || client.req.Messages[0].Role != "system" || client.req.Messages[1].Content != "Translate" {
		t.Fatalf("messages: %+v", client.req.Messages)
	}
}
