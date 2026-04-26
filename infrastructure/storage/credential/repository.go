package credential

import (
	"context"
	"errors"

	"gorm.io/gorm"

	domaincredential "github.com/shinya/shineflow/domain/credential"
	"github.com/shinya/shineflow/infrastructure/storage"
)

type credRepo struct{}

// NewCredentialRepository 构造 GORM 实现的 CredentialRepository。
func NewCredentialRepository() domaincredential.CredentialRepository { return &credRepo{} }

func (r *credRepo) Create(ctx context.Context, c *domaincredential.Credential) error {
	return storage.GetDB(ctx).Create(toCredentialModel(c)).Error
}

func (r *credRepo) Get(ctx context.Context, id string) (*domaincredential.Credential, error) {
	var m credentialModel
	err := storage.GetDB(ctx).Where("id = ?", id).Take(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, domaincredential.ErrCredentialNotFound
	}
	if err != nil { return nil, err }
	return toCredential(&m), nil
}

func (r *credRepo) List(
	ctx context.Context, filter domaincredential.CredentialFilter,
) ([]*domaincredential.Credential, error) {
	q := storage.GetDB(ctx).Model(&credentialModel{})
	if filter.Kind != "" { q = q.Where("kind = ?", string(filter.Kind)) }
	if filter.CreatedBy != "" { q = q.Where("created_by = ?", filter.CreatedBy) }
	if filter.Limit > 0 { q = q.Limit(filter.Limit) }
	if filter.Offset > 0 { q = q.Offset(filter.Offset) }
	q = q.Order("created_at DESC")
	var ms []credentialModel
	if err := q.Find(&ms).Error; err != nil { return nil, err }
	out := make([]*domaincredential.Credential, 0, len(ms))
	for i := range ms { out = append(out, toCredential(&ms[i])) }
	return out, nil
}

func (r *credRepo) Update(ctx context.Context, c *domaincredential.Credential) error {
	res := storage.GetDB(ctx).Model(&credentialModel{}).Where("id = ?", c.ID).
		Updates(map[string]any{
			"name":              c.Name,
			"kind":              string(c.Kind),
			"encrypted_payload": c.EncryptedPayload,
			"updated_at":        c.UpdatedAt,
		})
	if res.Error != nil { return res.Error }
	if res.RowsAffected == 0 { return domaincredential.ErrCredentialNotFound }
	return nil
}

func (r *credRepo) Delete(ctx context.Context, id string) error {
	res := storage.GetDB(ctx).Where("id = ?", id).Delete(&credentialModel{})
	if res.Error != nil { return res.Error }
	if res.RowsAffected == 0 { return domaincredential.ErrCredentialNotFound }
	return nil
}
