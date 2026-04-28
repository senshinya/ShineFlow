package builtin

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/shinya/shineflow/domain/executor"
	"github.com/shinya/shineflow/domain/nodetype"
	"github.com/shinya/shineflow/domain/workflow"
)

type httpRequestConfig struct {
	Method  string            `json:"method"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    json.RawMessage   `json:"body,omitempty"`
}

type httpRequestExecutor struct{}

func httpRequestFactory(_ *nodetype.NodeType) executor.NodeExecutor { return httpRequestExecutor{} }

func (httpRequestExecutor) Execute(ctx context.Context, in executor.ExecInput) (executor.ExecOutput, error) {
	if in.Services.HTTPClient == nil {
		return executor.ExecOutput{}, fmt.Errorf("http_request: %w", ErrPortNotConfigured)
	}
	var cfg httpRequestConfig
	if err := json.Unmarshal(in.Config, &cfg); err != nil {
		return executor.ExecOutput{}, fmt.Errorf("http_request: parse config: %w", err)
	}
	if cfg.Method == "" {
		return executor.ExecOutput{}, fmt.Errorf("http_request: config.method is required")
	}
	if cfg.URL == "" {
		return executor.ExecOutput{}, fmt.Errorf("http_request: config.url is required")
	}
	resp, err := in.Services.HTTPClient.Do(ctx, executor.HTTPRequest{
		Method:  cfg.Method,
		URL:     cfg.URL,
		Headers: cfg.Headers,
		Body:    cfg.Body,
	})
	if err != nil {
		return executor.ExecOutput{}, fmt.Errorf("http_request transport: %w", err)
	}
	port := workflow.PortDefault
	if resp.StatusCode >= 400 {
		port = workflow.PortError
	}
	return executor.ExecOutput{
		Outputs: map[string]any{
			"status":  resp.StatusCode,
			"headers": resp.Headers,
			"body":    resp.Body,
		},
		FiredPort: port,
	}, nil
}
