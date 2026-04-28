// Package executor 定义节点执行器的统一接口。
//
// 设计（六边形架构 / Ports & Adapters）：
//
//   - 内置节点和插件节点对调用方完全同构，引擎只认 NodeExecutor
//   - 所有 NodeExecutor 实现 → domain/executor/builtin/
//     执行器在做的事（读 Config/Inputs → 解凭证 → 装请求 → 解响应 → 选端口）都是工作流语义，
//     是领域逻辑；executor 内只通过 ExecServices 暴露的 port 接口对外通信
//   - 各 port 的具体适配器 → infrastructure/<protocol>/
//       infrastructure/http/      HTTPClient 实现（net/http）
//       infrastructure/llm/       LLMClient 实现（OpenAI / Anthropic / …）
//       infrastructure/mcp/       MCPClient 实现（stdio / http / sse transport）
//       infrastructure/sandbox/   builtin.code 的运行时（goja / yaegi / wasmtime）
//   - Registry 装配函数：组合 domain executor factory + infra 提供的 port 实现，由 main.go 调用
//
// 具体 Executor 与 port 适配器实现由后续 executor spec 落地。
package executor

import (
	"context"
	"encoding/json"

	"github.com/shinya/shineflow/domain/credential"
	"github.com/shinya/shineflow/domain/nodetype"
	"github.com/shinya/shineflow/domain/run"
)

// RunInfo 是 Executor 在执行时可读的 Run 元信息（只读快照）。
type RunInfo struct {
	RunID        string
	NodeRunID    string
	Attempt      int
	DefinitionID string
	VersionID    string
	TriggerKind  run.TriggerKind
	TriggerRef   string
}

// Logger 是 Executor 可用的极简日志接口；具体实现由 infra 注入。
type Logger interface {
	Debugf(format string, args ...any)
	Infof(format string, args ...any)
	Warnf(format string, args ...any)
	Errorf(format string, args ...any)
}

// HTTPClient 是 Executor 可用的极简 HTTP 客户端接口（不绑 net/http），方便测试 mock。
type HTTPClient interface {
	Do(ctx context.Context, req HTTPRequest) (HTTPResponse, error)
}

type HTTPRequest struct {
	Method  string
	URL     string
	Headers map[string]string
	Body    []byte
}

type HTTPResponse struct {
	StatusCode int
	Headers    map[string]string
	Body       []byte
}

// LLMClient 是与传输协议无关的 LLM completion 端口；具体适配器由 infrastructure 层提供。
type LLMClient interface {
	Complete(ctx context.Context, req LLMRequest) (LLMResponse, error)
}

type LLMRequest struct {
	Provider    string
	Model       string
	Messages    []LLMMessage
	Temperature float64
	MaxTokens   int
}

type LLMMessage struct {
	Role    string
	Content string
}

type LLMResponse struct {
	Text  string
	Model string
	Usage LLMUsage
}

type LLMUsage struct {
	InputTokens  int
	OutputTokens int
}

// ExecServices 是 Executor 可访问的能力集合。新增 LLM client / MCP client pool 时往这里加字段。
//
// 安全约束：Credentials 是唯一获取明文凭证的入口（spec §11.4）。
type ExecServices struct {
	Credentials credential.CredentialResolver
	Logger      Logger
	HTTPClient  HTTPClient
	LLMClient   LLMClient
}

// ExecInput 是引擎传给 NodeExecutor 的入参快照。
//
//   - Config / Inputs 都已完成模板展开和 ValueSource 求值
//   - 任何字段都不应包含 Credential 明文；秘密只能通过 Services.Credentials.Resolve 拿
type ExecInput struct {
	NodeType *nodetype.NodeType
	Config   json.RawMessage
	Inputs   map[string]any
	Run      RunInfo
	Services ExecServices
}

// ExecOutput 是 NodeExecutor 的产出。
//
//   - Outputs key 应对齐 NodeType.OutputSchema 的 PortSpec.Name
//   - FiredPort 默认 "default"；If 用 "true"/"false"；失败用 "error"
//   - ExternalRefs 用于审计追踪（LLM trace_id / HTTP request_id / MCP tool_call_id）
type ExecOutput struct {
	Outputs      map[string]any
	FiredPort    string
	ExternalRefs []run.ExternalRef
}
