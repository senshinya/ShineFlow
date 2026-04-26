package run

import (
	"encoding/json"
	"time"
)

// RunStatus 是 WorkflowRun 的状态机。
//
// 不变式（CanTransitionTo 编码）：
//   - 终态（success / failed / cancelled）不可回退到 running
//   - 同状态不可"自转"
//   - pending 只能进 running / failed / cancelled，不能直接跳 success
type RunStatus string

const (
	RunStatusPending   RunStatus = "pending"
	RunStatusRunning   RunStatus = "running"
	RunStatusSuccess   RunStatus = "success"
	RunStatusFailed    RunStatus = "failed"
	RunStatusCancelled RunStatus = "cancelled"
)

// IsTerminal 是否为终态。
func (s RunStatus) IsTerminal() bool {
	switch s {
	case RunStatusSuccess, RunStatusFailed, RunStatusCancelled:
		return true
	}
	return false
}

// CanTransitionTo 当前状态是否允许推进到 next。
func (s RunStatus) CanTransitionTo(next RunStatus) bool {
	if s == next {
		return false
	}
	if s.IsTerminal() {
		return false
	}
	switch s {
	case RunStatusPending:
		return next == RunStatusRunning || next == RunStatusFailed || next == RunStatusCancelled
	case RunStatusRunning:
		return next == RunStatusSuccess || next == RunStatusFailed || next == RunStatusCancelled
	}
	return false
}

// RunError 是 WorkflowRun 失败时的错误现场。
type RunError struct {
	NodeID    string          `json:"node_id"`
	NodeRunID string          `json:"node_run_id"`
	Code      string          `json:"code"`
	Message   string          `json:"message"`
	Details   json.RawMessage `json:"details,omitempty"`
}

// 常用 RunError.Code。
const (
	RunErrCodeNodeExecFailed      = "node_exec_failed"
	RunErrCodeTimeout             = "timeout"
	RunErrCodeCancelled           = "cancelled"
	RunErrCodeVersionNotPublished = "version_not_published"
)

// WorkflowRun 是运行时聚合根。
//
// 不变式（spec §14）：
//   - VersionID 创建后不可改
//   - Status 单调（CanTransitionTo 强制）
//   - Status == RunStatusSuccess 时 EndNodeID 必非空且指向 DSL 内真实 End 节点
type WorkflowRun struct {
	ID           string
	DefinitionID string
	VersionID    string

	TriggerKind    TriggerKind
	TriggerRef     string
	TriggerPayload json.RawMessage

	Status    RunStatus
	StartedAt *time.Time
	EndedAt   *time.Time

	Vars      json.RawMessage
	EndNodeID *string
	Output    json.RawMessage
	Error     *RunError

	CreatedBy string
	CreatedAt time.Time
}
