package credential

import (
	"context"
	"errors"
)

var ErrCredentialNotFound = errors.New("credential: not found")

// CredentialFilter 是 List 的过滤参数。
type CredentialFilter struct {
	Kind      CredentialKind
	CreatedBy string
	Limit     int
	Offset    int
}

// CredentialRepository 是 Credential 的存储契约。
//   - Get / List 返回的 Credential 仍含密文 EncryptedPayload，不解密
//   - 解密语义留给 CredentialResolver 实现
type CredentialRepository interface {
	Create(ctx context.Context, c *Credential) error
	Get(ctx context.Context, id string) (*Credential, error)
	List(ctx context.Context, filter CredentialFilter) ([]*Credential, error)
	Update(ctx context.Context, c *Credential) error
	Delete(ctx context.Context, id string) error
}
