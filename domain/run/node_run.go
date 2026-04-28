package run

import (
	"encoding/json"
	"time"
)

// NodeRunStatus 是 NodeRun 的状态机。Skipped 表示因 If/Switch 分支裁剪而未执行。
type NodeRunStatus string

const (
	NodeRunStatusPending   NodeRunStatus = "pending"
	NodeRunStatusRunning   NodeRunStatus = "running"
	NodeRunStatusSuccess   NodeRunStatus = "success"
	NodeRunStatusFailed    NodeRunStatus = "failed"
	NodeRunStatusSkipped   NodeRunStatus = "skipped"
	NodeRunStatusCancelled NodeRunStatus = "cancelled"
)

// IsTerminal 是否为终态。
func (s NodeRunStatus) IsTerminal() bool {
	switch s {
	case NodeRunStatusSuccess, NodeRunStatusFailed, NodeRunStatusSkipped, NodeRunStatusCancelled:
		return true
	}
	return false
}

// CanTransitionTo 当前状态是否允许推进到 next。
func (s NodeRunStatus) CanTransitionTo(next NodeRunStatus) bool {
	if s == next {
		return false
	}
	if s.IsTerminal() {
		return false
	}
	switch s {
	case NodeRunStatusPending:
		// 允许 pending → running，或被裁剪直接 → skipped/cancelled
		return next == NodeRunStatusRunning || next == NodeRunStatusSkipped || next == NodeRunStatusCancelled
	case NodeRunStatusRunning:
		return next == NodeRunStatusSuccess || next == NodeRunStatusFailed || next == NodeRunStatusCancelled
	}
	return false
}

// NodeError 是 NodeRun 失败时的错误现场。fallback 生效时仍保留最后一次 NodeError，便于审计。
type NodeError struct {
	Code    string          `json:"code"`
	Message string          `json:"message"`
	Details json.RawMessage `json:"details,omitempty"`
}

// 常用 NodeError.Code。
const (
	NodeErrCodeExecFailed       = "exec_failed"
	NodeErrCodeTimeout          = "timeout"
	NodeErrCodeCancelled        = "cancelled"
	NodeErrCodeValidationFailed = "validation_failed"
)

// ExternalRef 记录节点执行过程中的外部调用 ID（LLM trace_id / HTTP request_id / MCP tool_call_id）。
type ExternalRef struct {
	Kind string `json:"kind"` // "llm_call" | "http_request" | "mcp_tool"
	Ref  string `json:"ref"`
}

// NodeRun 是 WorkflowRun 聚合内的子实体。
//
// 不变式（spec §10、§14）：
//   - (RunID, NodeID, Attempt) 唯一；Attempt 从 1 起递增
//   - FallbackApplied=true 时 Status 必为 NodeRunStatusFailed、FiredPort 必为 PortDefault、Output 即 fallback 值
//   - ResolvedInputs / ResolvedConfig 不得包含任何 Credential 明文（结构性保证，见 spec §11.4）
type NodeRun struct {
	ID          string
	RunID       string
	NodeID      string
	NodeTypeKey string
	Attempt     int

	Status    NodeRunStatus
	StartedAt *time.Time
	EndedAt   *time.Time

	ResolvedConfig json.RawMessage
	ResolvedInputs json.RawMessage
	Output         json.RawMessage
	FiredPort      string

	FallbackApplied bool
	Error           *NodeError

	ExternalRefs []ExternalRef
}
