package workflow

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	domainworkflow "github.com/shinya/shineflow/domain/workflow"
	"github.com/shinya/shineflow/infrastructure/storage"
)

type workflowRepo struct{}

// NewWorkflowRepository 构造一个 GORM 实现的 WorkflowRepository。
// 不持有任何状态，DB 句柄从 storage 包级单例 + ctx 拿。
func NewWorkflowRepository() domainworkflow.WorkflowRepository {
	return &workflowRepo{}
}

// ---- Definition CRUD ----

func (r *workflowRepo) CreateDefinition(ctx context.Context, d *domainworkflow.WorkflowDefinition) error {
	return storage.GetDB(ctx).Create(toDefinitionModel(d)).Error
}

func (r *workflowRepo) GetDefinition(ctx context.Context, id string) (*domainworkflow.WorkflowDefinition, error) {
	var m definitionModel
	err := storage.GetDB(ctx).Where("id = ?", id).Take(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, domainworkflow.ErrDefinitionNotFound
	}
	if err != nil {
		return nil, err
	}
	return toDefinition(&m), nil
}

func (r *workflowRepo) ListDefinitions(
	ctx context.Context, filter domainworkflow.DefinitionFilter,
) ([]*domainworkflow.WorkflowDefinition, error) {
	q := storage.GetDB(ctx).Model(&definitionModel{})
	if filter.CreatedBy != "" {
		q = q.Where("created_by = ?", filter.CreatedBy)
	}
	if filter.NameLike != "" {
		q = q.Where("name LIKE ?", "%"+filter.NameLike+"%")
	}
	if filter.Limit > 0 {
		q = q.Limit(filter.Limit)
	}
	if filter.Offset > 0 {
		q = q.Offset(filter.Offset)
	}
	q = q.Order("created_at DESC")

	var ms []definitionModel
	if err := q.Find(&ms).Error; err != nil {
		return nil, err
	}
	out := make([]*domainworkflow.WorkflowDefinition, 0, len(ms))
	for i := range ms {
		out = append(out, toDefinition(&ms[i]))
	}
	return out, nil
}

func (r *workflowRepo) UpdateDefinition(ctx context.Context, d *domainworkflow.WorkflowDefinition) error {
	res := storage.GetDB(ctx).Model(&definitionModel{}).
		Where("id = ?", d.ID).
		Updates(map[string]any{
			"name":                 d.Name,
			"description":          d.Description,
			"draft_version_id":     d.DraftVersionID,
			"published_version_id": d.PublishedVersionID,
			"updated_at":           d.UpdatedAt,
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return domainworkflow.ErrDefinitionNotFound
	}
	return nil
}

func (r *workflowRepo) DeleteDefinition(ctx context.Context, id string) error {
	res := storage.GetDB(ctx).Where("id = ?", id).Delete(&definitionModel{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return domainworkflow.ErrDefinitionNotFound
	}
	return nil
}

// ---- Version 查询 ----

func (r *workflowRepo) GetVersion(ctx context.Context, id string) (*domainworkflow.WorkflowVersion, error) {
	var m versionModel
	err := storage.GetDB(ctx).Where("id = ?", id).Take(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, domainworkflow.ErrVersionNotFound
	}
	if err != nil {
		return nil, err
	}
	return toVersion(&m), nil
}

func (r *workflowRepo) ListVersions(
	ctx context.Context, definitionID string,
) ([]*domainworkflow.WorkflowVersion, error) {
	var ms []versionModel
	err := storage.GetDB(ctx).
		Where("definition_id = ?", definitionID).
		Order("version DESC").
		Find(&ms).Error
	if err != nil {
		return nil, err
	}
	out := make([]*domainworkflow.WorkflowVersion, 0, len(ms))
	for i := range ms {
		out = append(out, toVersion(&ms[i]))
	}
	return out, nil
}

func (r *workflowRepo) SaveVersion(
	ctx context.Context,
	defID string,
	dsl domainworkflow.WorkflowDSL,
	expectedRevision int,
) (*domainworkflow.WorkflowVersion, error) {
	var out *domainworkflow.WorkflowVersion
	err := storage.DBTransaction(ctx, func(ctx context.Context) error {
		db := storage.GetDB(ctx)

		var head versionModel
		err := db.Where("definition_id = ?", defID).
			Order("version DESC").Limit(1).Take(&head).Error

		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			v, e := r.insertNewDraftWithinTx(ctx, defID, dsl, 1, 1)
			out = v
			return e

		case err != nil:
			return err

		case head.State == string(domainworkflow.VersionStateDraft):
			if expectedRevision != head.Revision {
				return domainworkflow.ErrRevisionMismatch
			}
			res := db.Model(&versionModel{}).
				Where("id = ? AND revision = ?", head.ID, expectedRevision).
				Updates(map[string]any{
					"dsl":        dslColumn(dsl),
					"revision":   gorm.Expr("revision + 1"),
					"updated_at": time.Now().UTC(),
				})
			if res.Error != nil { return res.Error }
			if res.RowsAffected == 0 {
				return domainworkflow.ErrRevisionMismatch
			}
			v, e := r.getVersionWithinTx(ctx, head.ID)
			out = v
			return e

		default: // head 是 release → 追加新 draft
			v, e := r.insertNewDraftWithinTx(ctx, defID, dsl, head.Version+1, 1)
			out = v
			return e
		}
	})
	return out, err
}

// insertNewDraftWithinTx 假定已在 tx 内：插入新 draft 行 + 把 Definition.draft_version_id 指过去。
func (r *workflowRepo) insertNewDraftWithinTx(
	ctx context.Context, defID string, dsl domainworkflow.WorkflowDSL, version, revision int,
) (*domainworkflow.WorkflowVersion, error) {
	db := storage.GetDB(ctx)
	now := time.Now().UTC()
	m := &versionModel{
		ID:           uuid.NewString(),
		DefinitionID: defID,
		Version:      version,
		State:        string(domainworkflow.VersionStateDraft),
		DSL:          dslColumn(dsl),
		Revision:     revision,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := db.Create(m).Error; err != nil { return nil, err }

	if err := db.Model(&definitionModel{}).
		Where("id = ?", defID).
		Updates(map[string]any{
			"draft_version_id": m.ID,
			"updated_at":       now,
		}).Error; err != nil {
		return nil, err
	}
	return toVersion(m), nil
}

// getVersionWithinTx 假定已在 tx 内的查询。
func (r *workflowRepo) getVersionWithinTx(ctx context.Context, id string) (*domainworkflow.WorkflowVersion, error) {
	var m versionModel
	if err := storage.GetDB(ctx).Where("id = ?", id).Take(&m).Error; err != nil {
		return nil, err
	}
	return toVersion(&m), nil
}

func (r *workflowRepo) PublishVersion(
	ctx context.Context, versionID, publishedBy string,
) (*domainworkflow.WorkflowVersion, error) {
	var out *domainworkflow.WorkflowVersion
	err := storage.DBTransaction(ctx, func(ctx context.Context) error {
		db := storage.GetDB(ctx)

		// 1. 读目标 version
		var target versionModel
		err := db.Where("id = ?", versionID).Take(&target).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return domainworkflow.ErrVersionNotFound
		}
		if err != nil { return err }

		// 2. 已是 release → 幂等成功
		if target.State == string(domainworkflow.VersionStateRelease) {
			out = toVersion(&target)
			return nil
		}

		// 3. 校验是 head（同一 definition_id 下最大 version）
		var headVersion int
		if err := db.Model(&versionModel{}).
			Where("definition_id = ?", target.DefinitionID).
			Select("COALESCE(MAX(version), 0)").
			Scan(&headVersion).Error; err != nil {
			return err
		}
		if target.Version != headVersion {
			return domainworkflow.ErrNotHead
		}

		// 4. DSL 严格校验已上移到 application 层（需要注入 NodeTypeRegistry，
		//    属于另一份 spec 的范围）。本 repo 假设 caller 已经跑过 validator。

		// 5. UPDATE version
		now := time.Now().UTC()
		if err := db.Model(&versionModel{}).Where("id = ?", versionID).Updates(map[string]any{
			"state":        string(domainworkflow.VersionStateRelease),
			"published_at": now,
			"published_by": publishedBy,
			"updated_at":   now,
		}).Error; err != nil {
			return err
		}

		// 6. UPDATE definition：清 draft，置 published
		if err := db.Model(&definitionModel{}).
			Where("id = ?", target.DefinitionID).
			Updates(map[string]any{
				"draft_version_id":     nil,
				"published_version_id": versionID,
				"updated_at":           now,
			}).Error; err != nil {
			return err
		}

		out, _ = r.getVersionWithinTx(ctx, versionID)
		return nil
	})
	return out, err
}

// DiscardDraft 硬删 head draft + 清 definition.draft_version_id；幂等。
func (r *workflowRepo) DiscardDraft(ctx context.Context, definitionID string) error {
	return storage.DBTransaction(ctx, func(ctx context.Context) error {
		db := storage.GetDB(ctx)
		// 硬删 draft 行（本表无 deleted_at；执行真 DELETE）
		if err := db.Where("definition_id = ? AND state = ?",
			definitionID, string(domainworkflow.VersionStateDraft)).
			Delete(&versionModel{}).Error; err != nil {
			return err
		}
		// 清 definition.draft_version_id（无 draft 时无副作用）
		return db.Model(&definitionModel{}).
			Where("id = ?", definitionID).
			Updates(map[string]any{
				"draft_version_id": nil,
				"updated_at":       time.Now().UTC(),
			}).Error
	})
}
