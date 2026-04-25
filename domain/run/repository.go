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

	// ErrIllegalStatusTransition 是仓储在 UpdateStatus / UpdateNodeRunStatus 时
	// 通过 RunStatus.CanTransitionTo / NodeRunStatus.CanTransitionTo 判定失败时返回的哨兵 error。
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
//   - UpdateStatus / UpdateNodeRunStatus 在写库前必须用 CanTransitionTo 校验，
//     不合法时返回 ErrIllegalStatusTransition
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
	SaveNodeRunOutput(ctx context.Context, runID, nodeRunID string, output json.RawMessage, firedPort string) error
	GetNodeRun(ctx context.Context, runID, nodeRunID string) (*NodeRun, error)
	ListNodeRuns(ctx context.Context, runID string) ([]*NodeRun, error)
	GetLatestNodeRun(ctx context.Context, runID, nodeID string) (*NodeRun, error)
}
