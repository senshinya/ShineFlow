package run

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

var (
	ErrRunNotFound     = errors.New("run: workflow run not found")
	ErrNodeRunNotFound = errors.New("run: node run not found")

	// ErrIllegalStatusTransition 仓储在 UpdateStatus / UpdateNodeRunStatus 检测到
	// 当前状态不允许转到目标状态时返回的哨兵 error。
	// 校验语义对齐 RunStatus.CanTransitionTo / NodeRunStatus.CanTransitionTo；
	// 实现可选择直接调这两个方法，或把允许的前置状态编码进 SQL 的 WHERE 子句（单语句乐观锁），二者等价。
	ErrIllegalStatusTransition = errors.New("run: illegal status transition")
)

// RunFilter 是 List 的过滤参数。
type RunFilter struct {
	DefinitionID string
	VersionID    string
	Status       RunStatus
	TriggerKind  TriggerKind
	StartedFrom  *time.Time
	StartedTo    *time.Time
	Limit        int
	Offset       int
}

// RunUpdateOpt / NodeRunUpdateOpt 是函数选项，让 UpdateStatus 可一次更新多个相关字段。
type RunUpdateOpt func(*RunUpdate)

type RunUpdate struct {
	StartedAt *time.Time
	EndedAt   *time.Time
}

func WithRunStartedAt(t time.Time) RunUpdateOpt {
	return func(u *RunUpdate) { u.StartedAt = &t }
}
func WithRunEndedAt(t time.Time) RunUpdateOpt {
	return func(u *RunUpdate) { u.EndedAt = &t }
}

type NodeRunUpdateOpt func(*NodeRunUpdate)

type NodeRunUpdate struct {
	StartedAt       *time.Time
	EndedAt         *time.Time
	Error           *NodeError
	FallbackApplied *bool
	ExternalRefs    []ExternalRef
}

func WithNodeRunStartedAt(t time.Time) NodeRunUpdateOpt {
	return func(u *NodeRunUpdate) { u.StartedAt = &t }
}
func WithNodeRunEndedAt(t time.Time) NodeRunUpdateOpt {
	return func(u *NodeRunUpdate) { u.EndedAt = &t }
}
func WithNodeRunError(e NodeError) NodeRunUpdateOpt {
	return func(u *NodeRunUpdate) { u.Error = &e }
}
func WithNodeRunFallbackApplied(b bool) NodeRunUpdateOpt {
	return func(u *NodeRunUpdate) { u.FallbackApplied = &b }
}
func WithNodeRunExternalRefs(refs []ExternalRef) NodeRunUpdateOpt {
	return func(u *NodeRunUpdate) { u.ExternalRefs = refs }
}

// WorkflowRunRepository 是 WorkflowRun 聚合（含 NodeRun 子实体）的存储契约。
//
// 关键约束：
//   - 所有写入 NodeRun 的方法必须经由本接口（NodeRun 非独立聚合根）
//   - UpdateStatus / UpdateNodeRunStatus 必须保证状态机语义，不合法转移返回 ErrIllegalStatusTransition；
//     实现细节自由（直接调 CanTransitionTo，或把允许的前置状态编码进 SQL 的 WHERE 单语句乐观锁皆可）
type WorkflowRunRepository interface {
	// WorkflowRun
	Create(ctx context.Context, run *WorkflowRun) error
	Get(ctx context.Context, id string) (*WorkflowRun, error)
	List(ctx context.Context, filter RunFilter) ([]*WorkflowRun, error)
	UpdateStatus(ctx context.Context, id string, status RunStatus, opts ...RunUpdateOpt) error
	SaveEndResult(ctx context.Context, id, endNodeID string, output json.RawMessage) error
	SaveVars(ctx context.Context, id string, vars json.RawMessage) error
	SaveError(ctx context.Context, id string, e RunError) error

	// NodeRun（聚合内子实体）
	AppendNodeRun(ctx context.Context, runID string, nr *NodeRun) error
	UpdateNodeRunStatus(ctx context.Context, runID, nodeRunID string, status NodeRunStatus, opts ...NodeRunUpdateOpt) error
	SaveNodeRunResolved(ctx context.Context, runID, nodeRunID string, resolvedConfig, resolvedInputs json.RawMessage) error
	SaveNodeRunOutput(ctx context.Context, runID, nodeRunID string, output json.RawMessage, firedPort string) error
	GetNodeRun(ctx context.Context, runID, nodeRunID string) (*NodeRun, error)
	ListNodeRuns(ctx context.Context, runID string) ([]*NodeRun, error)
	GetLatestNodeRun(ctx context.Context, runID, nodeID string) (*NodeRun, error)
}
