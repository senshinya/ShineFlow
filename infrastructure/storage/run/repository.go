package run

import (
	"context"
	"encoding/json"
	"errors"

	"gorm.io/gorm"

	domainrun "github.com/shinya/shineflow/domain/run"
	"github.com/shinya/shineflow/infrastructure/storage"
)

type runRepo struct{}

// NewWorkflowRunRepository 构造一个 GORM 实现的 WorkflowRunRepository。
func NewWorkflowRunRepository() domainrun.WorkflowRunRepository {
	return &runRepo{}
}

// runAllowedPrev 各 status 允许的前置 status 集合，用于把状态机校验编码进 WHERE。
var runAllowedPrev = map[domainrun.RunStatus][]domainrun.RunStatus{
	domainrun.RunStatusRunning:   {domainrun.RunStatusPending},
	domainrun.RunStatusSuccess:   {domainrun.RunStatusRunning},
	domainrun.RunStatusFailed:    {domainrun.RunStatusPending, domainrun.RunStatusRunning},
	domainrun.RunStatusCancelled: {domainrun.RunStatusPending, domainrun.RunStatusRunning},
}

// nodeRunAllowedPrev 同上，针对 NodeRun。
var nodeRunAllowedPrev = map[domainrun.NodeRunStatus][]domainrun.NodeRunStatus{
	domainrun.NodeRunStatusRunning:   {domainrun.NodeRunStatusPending},
	domainrun.NodeRunStatusSkipped:   {domainrun.NodeRunStatusPending},
	domainrun.NodeRunStatusSuccess:   {domainrun.NodeRunStatusRunning},
	domainrun.NodeRunStatusFailed:    {domainrun.NodeRunStatusRunning},
	domainrun.NodeRunStatusCancelled: {domainrun.NodeRunStatusPending, domainrun.NodeRunStatusRunning},
}

// ---- WorkflowRun ----

func (r *runRepo) Create(ctx context.Context, run *domainrun.WorkflowRun) error {
	return storage.GetDB(ctx).Create(toRunModel(run)).Error
}

func (r *runRepo) Get(ctx context.Context, id string) (*domainrun.WorkflowRun, error) {
	var m runModel
	err := storage.GetDB(ctx).Where("id = ?", id).Take(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, domainrun.ErrRunNotFound
	}
	if err != nil {
		return nil, err
	}
	return toRun(&m), nil
}

func (r *runRepo) List(ctx context.Context, filter domainrun.RunFilter) ([]*domainrun.WorkflowRun, error) {
	q := storage.GetDB(ctx).Model(&runModel{})
	if filter.DefinitionID != "" {
		q = q.Where("definition_id = ?", filter.DefinitionID)
	}
	if filter.VersionID != "" {
		q = q.Where("version_id = ?", filter.VersionID)
	}
	if filter.Status != "" {
		q = q.Where("status = ?", string(filter.Status))
	}
	if filter.TriggerKind != "" {
		q = q.Where("trigger_kind = ?", string(filter.TriggerKind))
	}
	if filter.StartedFrom != nil {
		q = q.Where("started_at >= ?", *filter.StartedFrom)
	}
	if filter.StartedTo != nil {
		q = q.Where("started_at <= ?", *filter.StartedTo)
	}
	if filter.Limit > 0 {
		q = q.Limit(filter.Limit)
	}
	if filter.Offset > 0 {
		q = q.Offset(filter.Offset)
	}
	q = q.Order("created_at DESC")

	var ms []runModel
	if err := q.Find(&ms).Error; err != nil {
		return nil, err
	}
	out := make([]*domainrun.WorkflowRun, 0, len(ms))
	for i := range ms {
		out = append(out, toRun(&ms[i]))
	}
	return out, nil
}

// UpdateStatus 状态机校验编码进 WHERE，单语句乐观锁。0 行影响时区分 not found vs 非法转移。
func (r *runRepo) UpdateStatus(
	ctx context.Context, id string, status domainrun.RunStatus, opts ...domainrun.RunUpdateOpt,
) error {
	prev, ok := runAllowedPrev[status]
	if !ok {
		return domainrun.ErrIllegalStatusTransition
	}
	var u domainrun.RunUpdate
	for _, opt := range opts {
		opt(&u)
	}

	sets := map[string]any{"status": string(status)}
	if u.StartedAt != nil {
		sets["started_at"] = *u.StartedAt
	}
	if u.EndedAt != nil {
		sets["ended_at"] = *u.EndedAt
	}

	prevStrs := make([]string, len(prev))
	for i, p := range prev {
		prevStrs[i] = string(p)
	}

	res := storage.GetDB(ctx).Model(&runModel{}).
		Where("id = ? AND status IN ?", id, prevStrs).
		Updates(sets)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		var cnt int64
		storage.GetDB(ctx).Model(&runModel{}).Where("id = ?", id).Count(&cnt)
		if cnt == 0 {
			return domainrun.ErrRunNotFound
		}
		return domainrun.ErrIllegalStatusTransition
	}
	return nil
}

func (r *runRepo) SaveEndResult(
	ctx context.Context, id, endNodeID string, output json.RawMessage,
) error {
	res := storage.GetDB(ctx).Model(&runModel{}).Where("id = ?", id).
		Updates(map[string]any{
			"end_node_id": endNodeID,
			"output":      output,
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return domainrun.ErrRunNotFound
	}
	return nil
}

func (r *runRepo) SaveVars(ctx context.Context, id string, vars json.RawMessage) error {
	res := storage.GetDB(ctx).Model(&runModel{}).Where("id = ?", id).
		Updates(map[string]any{"vars": vars})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return domainrun.ErrRunNotFound
	}
	return nil
}

func (r *runRepo) SaveError(ctx context.Context, id string, e domainrun.RunError) error {
	res := storage.GetDB(ctx).Model(&runModel{}).Where("id = ?", id).
		Updates(map[string]any{
			"error": runErrorColumn{inner: &e},
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return domainrun.ErrRunNotFound
	}
	return nil
}

// ---- NodeRun ----

func (r *runRepo) AppendNodeRun(ctx context.Context, runID string, nr *domainrun.NodeRun) error {
	nr.RunID = runID
	return storage.GetDB(ctx).Create(toNodeRunModel(nr)).Error
}

func (r *runRepo) UpdateNodeRunStatus(
	ctx context.Context, runID, nodeRunID string, status domainrun.NodeRunStatus,
	opts ...domainrun.NodeRunUpdateOpt,
) error {
	prev, ok := nodeRunAllowedPrev[status]
	if !ok {
		return domainrun.ErrIllegalStatusTransition
	}
	var u domainrun.NodeRunUpdate
	for _, opt := range opts {
		opt(&u)
	}

	sets := map[string]any{"status": string(status)}
	if u.StartedAt != nil {
		sets["started_at"] = *u.StartedAt
	}
	if u.EndedAt != nil {
		sets["ended_at"] = *u.EndedAt
	}
	if u.Error != nil {
		sets["error"] = nodeErrorColumn{inner: u.Error}
	}
	if u.FallbackApplied != nil {
		sets["fallback_applied"] = *u.FallbackApplied
	}
	if u.ExternalRefs != nil {
		sets["external_refs"] = externalRefsColumn(u.ExternalRefs)
	}

	prevStrs := make([]string, len(prev))
	for i, p := range prev {
		prevStrs[i] = string(p)
	}

	res := storage.GetDB(ctx).Model(&nodeRunModel{}).
		Where("run_id = ? AND id = ? AND status IN ?", runID, nodeRunID, prevStrs).
		Updates(sets)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		var cnt int64
		storage.GetDB(ctx).Model(&nodeRunModel{}).
			Where("run_id = ? AND id = ?", runID, nodeRunID).Count(&cnt)
		if cnt == 0 {
			return domainrun.ErrNodeRunNotFound
		}
		return domainrun.ErrIllegalStatusTransition
	}
	return nil
}

func (r *runRepo) SaveNodeRunResolved(
	ctx context.Context, runID, nodeRunID string, resolvedConfig, resolvedInputs json.RawMessage,
) error {
	res := storage.GetDB(ctx).Model(&nodeRunModel{}).
		Where("run_id = ? AND id = ?", runID, nodeRunID).
		Updates(map[string]any{
			"resolved_config": resolvedConfig,
			"resolved_inputs": resolvedInputs,
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return domainrun.ErrNodeRunNotFound
	}
	return nil
}

func (r *runRepo) SaveNodeRunOutput(
	ctx context.Context, runID, nodeRunID string, output json.RawMessage, firedPort string,
) error {
	res := storage.GetDB(ctx).Model(&nodeRunModel{}).
		Where("run_id = ? AND id = ?", runID, nodeRunID).
		Updates(map[string]any{
			"output":     output,
			"fired_port": firedPort,
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return domainrun.ErrNodeRunNotFound
	}
	return nil
}

func (r *runRepo) GetNodeRun(ctx context.Context, runID, nodeRunID string) (*domainrun.NodeRun, error) {
	var m nodeRunModel
	err := storage.GetDB(ctx).Where("run_id = ? AND id = ?", runID, nodeRunID).Take(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, domainrun.ErrNodeRunNotFound
	}
	if err != nil {
		return nil, err
	}
	return toNodeRun(&m), nil
}

func (r *runRepo) ListNodeRuns(ctx context.Context, runID string) ([]*domainrun.NodeRun, error) {
	var ms []nodeRunModel
	err := storage.GetDB(ctx).Where("run_id = ?", runID).
		Order("attempt ASC").Find(&ms).Error
	if err != nil {
		return nil, err
	}
	out := make([]*domainrun.NodeRun, 0, len(ms))
	for i := range ms {
		out = append(out, toNodeRun(&ms[i]))
	}
	return out, nil
}

func (r *runRepo) GetLatestNodeRun(
	ctx context.Context, runID, nodeID string,
) (*domainrun.NodeRun, error) {
	var m nodeRunModel
	err := storage.GetDB(ctx).
		Where("run_id = ? AND node_id = ?", runID, nodeID).
		Order("attempt DESC").Limit(1).Take(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, domainrun.ErrNodeRunNotFound
	}
	if err != nil {
		return nil, err
	}
	return toNodeRun(&m), nil
}
