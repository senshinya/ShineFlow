package credential

import "context"

// Payload 是解密后的明文，结构按 Kind 不同：
//
//	APIKey  → {"key": "..."}
//	Bearer  → {"token": "..."}
//	Basic   → {"username": "...", "password": "..."}
//	Custom  → 任意 map[string]string
//
// domain 层只看到 map[string]string，避免为每种 Kind 单独建类型。
type Payload map[string]string

// CredentialResolver 是 Executor 唯一可以拿到明文凭证的入口。
// 实现负责按 Credential.Kind 解密 EncryptedPayload。
type CredentialResolver interface {
	Resolve(ctx context.Context, credID string) (Credential, Payload, error)
}
