package workflow

import "time"

// WorkflowVersion 是 DSL 的承载行。
//
// 不变式（强约束，由仓储 / 校验器联合保证）：
//   - 同一 DefinitionID 下，所有 Version 字段单调递增、唯一
//   - 至多一条 State == VersionStateDraft；若存在，其 Version 号 ≥ 所有 release
//   - State == VersionStateRelease 后，DSL / Version / Revision / PublishedAt / PublishedBy 全部冻结
type WorkflowVersion struct {
	ID           string
	DefinitionID string

	Version int
	State   VersionState
	DSL     WorkflowDSL

	// Revision 是乐观并发版本号；每次 SaveVersion 自增；翻 release 后冻结。
	Revision int

	// PublishedAt / PublishedBy 仅在 State == VersionStateRelease 时非空。
	PublishedAt *time.Time
	PublishedBy *string

	CreatedAt time.Time
	UpdatedAt time.Time
}

// IsDraft 是否处于 draft 状态。
func (v *WorkflowVersion) IsDraft() bool { return v.State == VersionStateDraft }

// IsRelease 是否处于 release 状态。
func (v *WorkflowVersion) IsRelease() bool { return v.State == VersionStateRelease }
