// Package credential 定义秘密存储聚合 Credential 与对外 Resolver 接口。
//
// 安全约束（由整体类型系统保证）：
//   - Credential.EncryptedPayload 是密文，加密密钥从 SHINEFLOW_CRED_KEY 环境变量读取
//   - 解密后的 Payload 只在 CredentialResolver.Resolve 的返回值里出现一次，
//     仅供 Executor 内部局部使用，绝不写回 NodeRun.ResolvedInputs / ResolvedConfig
//   - 用户在 DSL 中无法通过 ValueSource 引用到 Credential，秘密不会进入变量表 context
package credential

import "time"

// CredentialKind 决定 EncryptedPayload 解密后应能反序列化为哪种 Payload 形态。
type CredentialKind string

const (
	CredentialKindAPIKey CredentialKind = "api_key" // Payload: {Key string}
	CredentialKindBearer CredentialKind = "bearer"  // Payload: {Token string}
	CredentialKindBasic  CredentialKind = "basic"   // Payload: {Username, Password string}
	CredentialKindCustom CredentialKind = "custom"  // Payload: map[string]string
)

// Credential 是聚合根；密文 EncryptedPayload 仅由 CredentialResolver 解密。
type Credential struct {
	ID   string
	Name string
	Kind CredentialKind

	EncryptedPayload []byte

	CreatedBy string
	CreatedAt time.Time
	UpdatedAt time.Time
}
