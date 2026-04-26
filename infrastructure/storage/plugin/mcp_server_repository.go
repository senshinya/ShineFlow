package plugin

import (
	"context"
	"errors"

	"gorm.io/gorm"

	domainplugin "github.com/shinya/shineflow/domain/plugin"
	"github.com/shinya/shineflow/infrastructure/storage"
)

type mcpServerRepo struct{}

// NewMcpServerRepository 构造 GORM 实现的 McpServerRepository。
func NewMcpServerRepository() domainplugin.McpServerRepository { return &mcpServerRepo{} }

func (r *mcpServerRepo) Create(ctx context.Context, s *domainplugin.McpServer) error {
	return storage.GetDB(ctx).Select("*").Create(toMcpServerModel(s)).Error
}

func (r *mcpServerRepo) Get(ctx context.Context, id string) (*domainplugin.McpServer, error) {
	var m mcpServerModel
	err := storage.GetDB(ctx).Where("id = ?", id).Take(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, domainplugin.ErrMcpServerNotFound
	}
	if err != nil {
		return nil, err
	}
	return toMcpServer(&m), nil
}

func (r *mcpServerRepo) List(
	ctx context.Context, filter domainplugin.McpServerFilter,
) ([]*domainplugin.McpServer, error) {
	q := storage.GetDB(ctx).Model(&mcpServerModel{})
	if filter.EnabledOnly {
		q = q.Where("enabled = ?", true)
	}
	if filter.Limit > 0 {
		q = q.Limit(filter.Limit)
	}
	if filter.Offset > 0 {
		q = q.Offset(filter.Offset)
	}
	q = q.Order("created_at DESC")
	var ms []mcpServerModel
	if err := q.Find(&ms).Error; err != nil {
		return nil, err
	}
	out := make([]*domainplugin.McpServer, 0, len(ms))
	for i := range ms {
		out = append(out, toMcpServer(&ms[i]))
	}
	return out, nil
}

func (r *mcpServerRepo) Update(ctx context.Context, s *domainplugin.McpServer) error {
	res := storage.GetDB(ctx).Model(&mcpServerModel{}).Where("id = ?", s.ID).
		Updates(map[string]any{
			"name":            s.Name,
			"transport":       string(s.Transport),
			"config":          s.Config,
			"credential_id":   s.CredentialID,
			"enabled":         s.Enabled,
			"last_synced_at":  s.LastSyncedAt,
			"last_sync_error": s.LastSyncError,
			"updated_at":      s.UpdatedAt,
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return domainplugin.ErrMcpServerNotFound
	}
	return nil
}

func (r *mcpServerRepo) Delete(ctx context.Context, id string) error {
	res := storage.GetDB(ctx).Where("id = ?", id).Delete(&mcpServerModel{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return domainplugin.ErrMcpServerNotFound
	}
	return nil
}
