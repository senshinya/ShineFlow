package plugin

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	domainplugin "github.com/shinya/shineflow/domain/plugin"
	"github.com/shinya/shineflow/infrastructure/storage"
)

type mcpToolRepo struct{}

// NewMcpToolRepository 构造 GORM 实现的 McpToolRepository。
func NewMcpToolRepository() domainplugin.McpToolRepository { return &mcpToolRepo{} }

func (r *mcpToolRepo) GetByServerAndName(
	ctx context.Context, serverID, name string,
) (*domainplugin.McpTool, error) {
	var m mcpToolModel
	err := storage.GetDB(ctx).Where("server_id = ? AND name = ?", serverID, name).Take(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, domainplugin.ErrMcpToolNotFound
	}
	if err != nil {
		return nil, err
	}
	return toMcpTool(&m), nil
}

func (r *mcpToolRepo) ListByServer(
	ctx context.Context, serverID string,
) ([]*domainplugin.McpTool, error) {
	var ms []mcpToolModel
	err := storage.GetDB(ctx).Where("server_id = ?", serverID).
		Order("name ASC").Find(&ms).Error
	if err != nil {
		return nil, err
	}
	out := make([]*domainplugin.McpTool, 0, len(ms))
	for i := range ms {
		out = append(out, toMcpTool(&ms[i]))
	}
	return out, nil
}

func (r *mcpToolRepo) SetEnabled(ctx context.Context, id string, enabled bool) error {
	res := storage.GetDB(ctx).Model(&mcpToolModel{}).Where("id = ?", id).
		Updates(map[string]any{"enabled": enabled})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return domainplugin.ErrMcpToolNotFound
	}
	return nil
}

// UpsertAll 同步覆盖一个 server 名下的全部 tool：
// 1) 删失踪：name 不在新列表里的 tool 全删
// 2) 用 ON CONFLICT (server_id, name) DO UPDATE 真 upsert，保留同名 tool 的原 ID
// 3) enabled 故意不在 DoUpdates 里 —— 同步不重置用户的 enable/disable 偏好
func (r *mcpToolRepo) UpsertAll(
	ctx context.Context, serverID string, tools []*domainplugin.McpTool,
) error {
	return storage.DBTransaction(ctx, func(ctx context.Context) error {
		db := storage.GetDB(ctx)

		names := make([]string, 0, len(tools))
		models := make([]*mcpToolModel, 0, len(tools))
		for _, t := range tools {
			t.ServerID = serverID
			names = append(names, t.Name)
			models = append(models, toMcpToolModel(t))
		}

		delQ := db.Where("server_id = ?", serverID)
		if len(names) > 0 {
			delQ = delQ.Where("name NOT IN ?", names)
		}
		if err := delQ.Delete(&mcpToolModel{}).Error; err != nil {
			return err
		}

		if len(models) == 0 {
			return nil
		}
		// Select("*") 强制写入 enabled（GORM 否则会跳过 false zero-value，
		// 让列级 DEFAULT TRUE 生效，导致用户原本想注入的 disabled tool 被反转）。
		return db.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "server_id"}, {Name: "name"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"description", "input_schema_raw", "synced_at",
			}),
		}).Select("*").Create(&models).Error
	})
}
