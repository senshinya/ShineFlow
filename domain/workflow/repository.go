package workflow

import (
	"context"
	"errors"
)

// 哨兵 error。仓储实现 / 校验器以这些 error 作为可识别的失败语义。
var (
	// ErrRevisionMismatch SaveVersion 收到的 expectedRevision 与 head draft 当前 Revision 不一致。
	// 仓储实现可在外层包装更详细的 message（含服务端 latest revision），但根因 errors.Is 必须命中本变量。
	ErrRevisionMismatch = errors.New("workflow: version revision mismatch")

	// ErrNotHead PublishVersion 指向的 versionID 不是该 Definition 的 head（最大 Version 号）。
	ErrNotHead = errors.New("workflow: not head version")

	// ErrDraftValidation draft 在 publish 之前严格校验失败的 sentinel。
	// 由 application 层在调 PublishVersion 之前跑 domain/validator.ValidateForPublish 时产生（详情包成 validator.ValidationError 列表）。
	// 仓储层不返回此 error。
	ErrDraftValidation = errors.New("workflow: draft validation failed")

	// ErrDefinitionNotFound / ErrVersionNotFound 通用查不到。
	ErrDefinitionNotFound = errors.New("workflow: definition not found")
	ErrVersionNotFound    = errors.New("workflow: version not found")
)

// DefinitionFilter 是 ListDefinitions 的过滤参数，字段未来可扩展。
type DefinitionFilter struct {
	CreatedBy string
	NameLike  string
	Limit     int
	Offset    int
}

// WorkflowRepository 是 WorkflowDefinition / WorkflowVersion 聚合的仓储契约。
//
// 关键事务约束（由 infra spec 落地）：
//   - SaveVersion / PublishVersion / DiscardDraft 内部需要原子性，
//     允许实现自包 storage.DBTransaction（嵌套对外部 tx 幂等复用）。
//   - application 层若把 SaveVersion + PublishVersion 组合成"保存并发布"用例，
//     在外层显式 storage.DBTransaction 一次包住，内层 self-tx 自动复用同一 tx。
type WorkflowRepository interface {
	// Definition
	CreateDefinition(ctx context.Context, def *WorkflowDefinition) error
	GetDefinition(ctx context.Context, id string) (*WorkflowDefinition, error)
	ListDefinitions(ctx context.Context, filter DefinitionFilter) ([]*WorkflowDefinition, error)
	UpdateDefinition(ctx context.Context, def *WorkflowDefinition) error
	DeleteDefinition(ctx context.Context, id string) error

	// Version 查询
	GetVersion(ctx context.Context, id string) (*WorkflowVersion, error)
	// ListVersions 按 Version 倒序，含 draft（若有）。
	ListVersions(ctx context.Context, definitionID string) ([]*WorkflowVersion, error)

	// SaveVersion 保存（创建或覆盖）头部 draft；保存的 version 状态默认为 draft。
	//
	//   - head 是 draft → 原地覆盖该条 DSL，Revision++，UpdatedAt = now
	//   - head 是 release（或 Definition 还没有任何 version）→ append 新 draft，
	//     Version = max(Version) + 1，Revision = 1
	//
	// expectedRevision 语义：
	//   - head 是 draft 时，必须等于其当前 Revision；不匹配返回 ErrRevisionMismatch
	//   - head 非 draft / 无 version 时，传 0
	SaveVersion(ctx context.Context, definitionID string, dsl WorkflowDSL, expectedRevision int) (*WorkflowVersion, error)

	// PublishVersion 把指定 version 翻为 release。入参不带 DSL。
	//
	//   - versionID 必须是该 Definition 的 head（最大 Version 号），否则返回 ErrNotHead
	//   - 已是 release → 幂等成功（同一 versionID 重复 publish 安全）
	//   - 是 draft → 直接转 release。仓储**不再**内嵌 DSL 严格校验：
	//     调用方（application 层）应在 PublishVersion 之前调一次
	//     domain/validator.ValidateForPublish，失败时由 application 层包装并返回
	//     ErrDraftValidation。仓储侧假设 caller 已校验通过。
	//
	// SQL 形态：
	//   UPDATE versions SET state='release', published_at=NOW(), published_by=$1 WHERE id=$VersionID
	//   UPDATE definitions SET draft_version_id=NULL, published_version_id=$VersionID WHERE id=$DefID
	PublishVersion(ctx context.Context, versionID, publishedBy string) (*WorkflowVersion, error)

	// DiscardDraft 若有 draft 则硬删并清 Definition.DraftVersionID；
	// 若无 draft 则静默成功（幂等，不返回错误）。
	// 已存在的 try-run 行不动（NodeRun 历史保留）。
	DiscardDraft(ctx context.Context, definitionID string) error
}
