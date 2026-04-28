package enginetest

import (
	"context"
	"errors"

	"github.com/shinya/shineflow/domain/executor"
)

// MockHTTPClient 是测试用 HTTP port。
type MockHTTPClient struct {
	OnDo func(ctx context.Context, req executor.HTTPRequest) (executor.HTTPResponse, error)
}

func (m *MockHTTPClient) Do(ctx context.Context, req executor.HTTPRequest) (executor.HTTPResponse, error) {
	if m.OnDo == nil {
		return executor.HTTPResponse{}, errors.New("MockHTTPClient.OnDo not set")
	}
	return m.OnDo(ctx, req)
}

// MockLLMClient 是测试用 LLM port。
type MockLLMClient struct {
	OnComplete func(ctx context.Context, req executor.LLMRequest) (executor.LLMResponse, error)
}

func (m *MockLLMClient) Complete(ctx context.Context, req executor.LLMRequest) (executor.LLMResponse, error) {
	if m.OnComplete == nil {
		return executor.LLMResponse{}, errors.New("MockLLMClient.OnComplete not set")
	}
	return m.OnComplete(ctx, req)
}

// MockLogger 丢弃日志，满足 executor.Logger。
type MockLogger struct{}

func (MockLogger) Debugf(string, ...any) {}
func (MockLogger) Infof(string, ...any)  {}
func (MockLogger) Warnf(string, ...any)  {}
func (MockLogger) Errorf(string, ...any) {}
