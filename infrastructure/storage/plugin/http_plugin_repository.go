package plugin

import (
	"context"
	"errors"

	"gorm.io/gorm"

	domainplugin "github.com/shinya/shineflow/domain/plugin"
	"github.com/shinya/shineflow/infrastructure/storage"
)

type httpRepo struct{}

// NewHttpPluginRepository 构造 GORM 实现的 HttpPluginRepository。
func NewHttpPluginRepository() domainplugin.HttpPluginRepository { return &httpRepo{} }

func (r *httpRepo) Create(ctx context.Context, p *domainplugin.HttpPlugin) error {
	return storage.GetDB(ctx).Select("*").Create(toHttpPluginModel(p)).Error
}

func (r *httpRepo) Get(ctx context.Context, id string) (*domainplugin.HttpPlugin, error) {
	var m httpPluginModel
	err := storage.GetDB(ctx).Where("id = ?", id).Take(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, domainplugin.ErrHttpPluginNotFound
	}
	if err != nil {
		return nil, err
	}
	return toHttpPlugin(&m), nil
}

func (r *httpRepo) List(
	ctx context.Context, filter domainplugin.HttpPluginFilter,
) ([]*domainplugin.HttpPlugin, error) {
	q := storage.GetDB(ctx).Model(&httpPluginModel{})
	if filter.EnabledOnly {
		q = q.Where("enabled = ?", true)
	}
	if filter.CreatedBy != "" {
		q = q.Where("created_by = ?", filter.CreatedBy)
	}
	if filter.Limit > 0 {
		q = q.Limit(filter.Limit)
	}
	if filter.Offset > 0 {
		q = q.Offset(filter.Offset)
	}
	q = q.Order("created_at DESC")
	var ms []httpPluginModel
	if err := q.Find(&ms).Error; err != nil {
		return nil, err
	}
	out := make([]*domainplugin.HttpPlugin, 0, len(ms))
	for i := range ms {
		out = append(out, toHttpPlugin(&ms[i]))
	}
	return out, nil
}

func (r *httpRepo) Update(ctx context.Context, p *domainplugin.HttpPlugin) error {
	res := storage.GetDB(ctx).Model(&httpPluginModel{}).Where("id = ?", p.ID).
		Updates(map[string]any{
			"name":             p.Name,
			"description":      p.Description,
			"method":           p.Method,
			"url":              p.URL,
			"headers":          nonNilStringMap(p.Headers),
			"query_params":     nonNilStringMap(p.QueryParams),
			"body_template":    p.BodyTemplate,
			"auth_kind":        p.AuthKind,
			"credential_id":    p.CredentialID,
			"input_schema":     nonNilPortSpecs(p.InputSchema),
			"output_schema":    nonNilPortSpecs(p.OutputSchema),
			"response_mapping": nonNilStringMap(p.ResponseMapping),
			"enabled":          p.Enabled,
			"updated_at":       p.UpdatedAt,
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return domainplugin.ErrHttpPluginNotFound
	}
	return nil
}

func (r *httpRepo) Delete(ctx context.Context, id string) error {
	res := storage.GetDB(ctx).Where("id = ?", id).Delete(&httpPluginModel{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return domainplugin.ErrHttpPluginNotFound
	}
	return nil
}
