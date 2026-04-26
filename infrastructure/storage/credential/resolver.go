package credential

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"

	domaincredential "github.com/shinya/shineflow/domain/credential"
	"github.com/shinya/shineflow/infrastructure/util"
)

type resolverImpl struct {
	repo domaincredential.CredentialRepository
	key  []byte
}

// NewResolver 从 SHINEFLOW_CRED_KEY 环境变量读 base64 编码的 32 字节密钥，
// 解出 raw 字节后构造 Resolver。缺失或长度错返回 error（service 启动应 fatal）。
func NewResolver(repo domaincredential.CredentialRepository) (domaincredential.CredentialResolver, error) {
	raw := os.Getenv("SHINEFLOW_CRED_KEY")
	if raw == "" {
		return nil, fmt.Errorf("SHINEFLOW_CRED_KEY: env var not set")
	}
	key, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("SHINEFLOW_CRED_KEY: not valid base64: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("SHINEFLOW_CRED_KEY: must be 32 bytes after base64 decode, got %d", len(key))
	}
	return &resolverImpl{repo: repo, key: key}, nil
}

func (r *resolverImpl) Resolve(
	ctx context.Context, credID string,
) (domaincredential.Credential, domaincredential.Payload, error) {
	c, err := r.repo.Get(ctx, credID)
	if err != nil {
		return domaincredential.Credential{}, nil, err
	}
	plain, err := Decrypt(r.key, c.EncryptedPayload)
	if err != nil {
		return domaincredential.Credential{}, nil, fmt.Errorf("decrypt: %w", err)
	}
	var p domaincredential.Payload
	if err := util.UnmarshalFromString(string(plain), &p); err != nil {
		return domaincredential.Credential{}, nil, fmt.Errorf("unmarshal payload: %w", err)
	}
	return *c, p, nil
}
