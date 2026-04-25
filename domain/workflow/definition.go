package workflow

import "time"

// VersionState 区分一个 WorkflowVersion 是 draft 还是 release。
type VersionState string

const (
	VersionStateDraft   VersionState = "draft"
	VersionStateRelease VersionState = "release"
)

// WorkflowDefinition 是工作流的稳定身份：ID 不变，名称 / 描述可改。
//   - DraftVersionID     当前 head 是否为 draft；nil 表示当前没有 draft（懒创建）
//   - PublishedVersionID 当前最高号的 release 版本；nil 表示从未发布
type WorkflowDefinition struct {
	ID          string
	Name        string
	Description string

	DraftVersionID     *string
	PublishedVersionID *string

	CreatedBy string
	CreatedAt time.Time
	UpdatedAt time.Time
}
