package builtin

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/shinya/shineflow/domain/executor"
	"github.com/shinya/shineflow/domain/nodetype"
	"github.com/shinya/shineflow/domain/workflow"
)

type llmConfig struct {
	Provider     string  `json:"provider"`
	Model        string  `json:"model"`
	SystemPrompt string  `json:"system_prompt,omitempty"`
	Temperature  float64 `json:"temperature,omitempty"`
	MaxTokens    int     `json:"max_tokens,omitempty"`
}

type llmExecutor struct{}

func llmFactory(_ *nodetype.NodeType) executor.NodeExecutor { return llmExecutor{} }

func (llmExecutor) Execute(ctx context.Context, in executor.ExecInput) (executor.ExecOutput, error) {
	if in.Services.LLMClient == nil {
		return executor.ExecOutput{}, fmt.Errorf("llm: %w", ErrPortNotConfigured)
	}
	var cfg llmConfig
	if err := json.Unmarshal(in.Config, &cfg); err != nil {
		return executor.ExecOutput{}, fmt.Errorf("llm: parse config: %w", err)
	}
	if cfg.Model == "" {
		return executor.ExecOutput{}, fmt.Errorf("llm: config.model required")
	}
	messages, err := buildMessages(cfg.SystemPrompt, in.Inputs)
	if err != nil {
		return executor.ExecOutput{}, err
	}
	resp, err := in.Services.LLMClient.Complete(ctx, executor.LLMRequest{
		Provider:    cfg.Provider,
		Model:       cfg.Model,
		Messages:    messages,
		Temperature: cfg.Temperature,
		MaxTokens:   cfg.MaxTokens,
	})
	if err != nil {
		return executor.ExecOutput{}, fmt.Errorf("llm transport: %w", err)
	}
	return executor.ExecOutput{
		Outputs: map[string]any{
			"text":  resp.Text,
			"model": resp.Model,
			"usage": map[string]any{
				"input_tokens":  resp.Usage.InputTokens,
				"output_tokens": resp.Usage.OutputTokens,
			},
		},
		FiredPort: workflow.PortDefault,
	}, nil
}

// buildMessages 支持 messages 数组输入，也支持 prompt 字符串快捷输入。
func buildMessages(systemPrompt string, inputs map[string]any) ([]executor.LLMMessage, error) {
	msgs := make([]executor.LLMMessage, 0)
	if systemPrompt != "" {
		msgs = append(msgs, executor.LLMMessage{Role: "system", Content: systemPrompt})
	}
	if raw, ok := inputs["messages"]; ok {
		arr, ok := raw.([]any)
		if !ok {
			return nil, fmt.Errorf("llm: input.messages must be array, got %T", raw)
		}
		if len(arr) == 0 {
			return nil, fmt.Errorf("llm: input.messages must contain at least one message")
		}
		for i, item := range arr {
			m, ok := item.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("llm: input.messages[%d] must be object", i)
			}
			role, ok := m["role"].(string)
			if !ok || role == "" {
				return nil, fmt.Errorf("llm: input.messages[%d].role is required", i)
			}
			content, ok := m["content"].(string)
			if !ok || content == "" {
				return nil, fmt.Errorf("llm: input.messages[%d].content is required", i)
			}
			msgs = append(msgs, executor.LLMMessage{Role: role, Content: content})
		}
		return msgs, nil
	}
	if prompt, ok := inputs["prompt"].(string); ok {
		if prompt == "" {
			return nil, fmt.Errorf("llm: input.prompt is required")
		}
		msgs = append(msgs, executor.LLMMessage{Role: "user", Content: prompt})
		return msgs, nil
	}
	return nil, fmt.Errorf("llm: must provide input.messages or input.prompt")
}
