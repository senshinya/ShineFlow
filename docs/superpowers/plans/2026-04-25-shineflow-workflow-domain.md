# ShineFlow 工作流领域模型 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 按 `docs/superpowers/specs/2026-04-22-shineflow-workflow-domain-design.md` 落地 ShineFlow `domain/` 层的全部类型、接口、纯函数与不变式校验，给后续 application / infrastructure / facade 层提供契约。

**Architecture:** 严格 DDD：本计划只产出 `domain/` 层（类型 + 接口 + 纯函数 + 单元测试）；执行器实现、仓储 GORM 实现、HTTP 路由、application 用例编排都不在本计划范围。包之间的依赖只能朝以下方向流动：

```
domain/workflow      ← domain/workflow/check  ← (infrastructure / application)
domain/workflow      ← domain/nodetype        ← domain/workflow/check
domain/workflow      ← domain/plugin          ← domain/nodetype（projection 子文件）
domain/credential    ← domain/executor
domain/nodetype      ← domain/executor
domain/workflow      ← domain/run
```

**与 spec §4 的偏差**：spec 把校验逻辑挂在 `domain/workflow/` 下隐含，但严格校验需引用 `domain/nodetype`，而 `nodetype` 又需要 `workflow.PortSpec`，会构成循环依赖。本计划把严格校验放在 **sibling 子包** `domain/workflow/check/` 中（`workflow` 本体不引用 `nodetype`），其余文件布局完全遵从 spec §4。

**Tech Stack:** Go 1.26.1；测试用 stdlib `testing` + `reflect.DeepEqual`，不引入新依赖（不要 testify、不要 go-cmp）。所有 ID 都是 `string`，时间用 `time.Time`，可选字段用指针，枚举用具名 `string` + 包内常量，序列化字段用 `encoding/json` 的 `json.RawMessage`。

**TDD 约定**：

- 纯类型 / 接口声明（无行为）—— 不写测试，靠编译器保证。
- 任何带行为的函数 / 方法（状态转换辅助、纯函数、模式匹配、投影、校验）—— 必须先写失败测试。
- 每个 Task 结束 commit；commit 消息形如 `feat(domain/<pkg>): <做了什么>` 或 `test(domain/<pkg>): <测了什么>`。
- 每个 Task 结束都要执行：`go build ./... && go vet ./...`，必须通过。

---

## File Structure

最终落地的文件分布（本计划完成后）：

```
domain/
├── doc.go                                    # 已存在，无需改动
├── workflow/
│   ├── port.go                # PortSpec / SchemaType
│   ├── value.go               # ValueSource / ValueKind / RefValue
│   ├── error_policy.go        # ErrorPolicy / FailStrategy / BackoffKind
│   ├── node_ui.go             # NodeUI
│   ├── dsl.go                 # Node / Edge / WorkflowDSL + 保留端口名常量
│   ├── definition.go          # WorkflowDefinition + VersionState
│   ├── version.go             # WorkflowVersion
│   ├── repository.go          # WorkflowRepository + 哨兵 error
│   └── check/
│       ├── validator.go       # ValidateForPublish + ValidationError
│       └── validator_test.go
├── nodetype/
│   ├── nodetype.go            # NodeType
│   ├── builtin.go             # 内置 Key 常量
│   ├── registry.go            # NodeTypeRegistry / NodeTypeFilter
│   └── projection.go          # projectHttpPlugin / projectMcpTool / mcpSchemaToPortSpecs
│   └── projection_test.go
├── plugin/
│   ├── http_plugin.go
│   ├── mcp_server.go
│   ├── mcp_tool.go
│   └── repository.go
├── credential/
│   ├── credential.go
│   ├── resolver.go            # CredentialResolver / Payload
│   └── repository.go
├── run/
│   ├── trigger.go             # TriggerKind 常量
│   ├── workflow_run.go        # WorkflowRun + RunStatus + RunError
│   ├── workflow_run_test.go   # 状态机不变式
│   ├── node_run.go            # NodeRun + NodeRunStatus + NodeError + ExternalRef
│   ├── node_run_test.go
│   ├── context.go             # BuildContext 投影函数
│   ├── context_test.go
│   └── repository.go          # WorkflowRunRepository + 仓储辅助类型
├── cron/
│   ├── cronjob.go
│   └── repository.go
└── executor/
    ├── executor.go            # NodeExecutor 接口
    ├── exec_input.go          # ExecInput / ExecOutput / RunInfo / ExecServices / Logger / HTTPClient / ExternalRef
    ├── factory.go             # ExecutorFactory / ExecutorRegistry 接口
    ├── registry.go            # 默认 ExecutorRegistry 实现 + 前缀匹配
    ├── registry_test.go
    └── builtin/               # 【本计划不创建】所有 NodeExecutor 实现的预留落点；
                               # 由后续 executor spec 落全部 builtin + 插件 executor
```

未在本计划交付、由后续 spec 处理的内容（Ports & Adapters / 六边形架构）：

- **`domain/executor/builtin/`**：**所有** NodeExecutor 实现
  - builtin：start / end / if / switch / loop / set_variable / code / llm / http_request
  - 插件：plugin.http.* / plugin.mcp.*.*
  - 理由：执行器在做的事（读 Config/Inputs → 解凭证 → 装请求 → 解响应 → 选端口）都是工作流语义 = 领域逻辑；
    对外通信只通过 `ExecServices` 暴露的 port 接口（`HTTPClient` / 未来的 `LLMClient` / `MCPClient` / `Sandbox`）
- **`infrastructure/<protocol>/`**：各 port 的具体适配器
  - `infrastructure/http/`：`HTTPClient` 实现（包 `net/http`）
  - `infrastructure/llm/`：`LLMClient` 实现（OpenAI / Anthropic / …）
  - `infrastructure/mcp/`：`MCPClient` 实现（stdio / http / sse transport）
  - `infrastructure/sandbox/`：`builtin.code` 的运行时（goja / yaegi / wasmtime）
- **Registry 装配函数**：组合 domain 的 executor factory 与 infra 的 port 实现，由 main.go 调用
- **`infrastructure/storage/`**：domain 各仓储接口的 GORM / Postgres 实现
  - 单包 `package storage`，**不再分子包**
  - 一个聚合一个文件：`workflow.go` / `run.go` / `cron.go` / `plugin.go`（含 Http / McpServer / McpTool 三个实现）/ `credential.go` / `nodetype.go`（如 NodeTypeRegistry 需 DB 缓存）
  - `db.go` 仍只放连接初始化

---

## 任务总览

1. workflow 基础值对象（port / value / error_policy / node_ui）
2. workflow DSL（Node / Edge / WorkflowDSL）
3. workflow Definition & Version + VersionState
4. workflow 仓储接口与哨兵 error
5. credential 包（Credential / Resolver / Repository）
6. plugin 包（HttpPlugin / McpServer / McpTool / Repository）
7. nodetype 类型 + 内置 Key + Registry 接口
8. nodetype 投影（HttpPlugin / McpTool → NodeType）
9. workflow/check 严格校验器（PublishVersion 用）
10. run 类型与状态机（WorkflowRun / NodeRun / TriggerKind）
11. run context 投影
12. run 仓储接口
13. cron 包（CronJob + Repository）
14. executor 接口与服务结构（NodeExecutor / ExecInput / ExecOutput / Services）
15. executor Registry + 前缀匹配实现
16. 收尾：doc.go、整体 build / vet 验收

---

### Task 1: workflow 基础值对象

**Files:**
- Create: `domain/workflow/port.go`
- Create: `domain/workflow/value.go`
- Create: `domain/workflow/error_policy.go`
- Create: `domain/workflow/node_ui.go`

本任务全部为类型声明，无行为，不写测试。验收靠 `go build` + `go vet`。

- [ ] **Step 1：创建 `domain/workflow/port.go`**

```go
// Package workflow 定义工作流设计时（DSL）的核心值对象与聚合。
//
// 本包不依赖任何其他 domain 子包；其他子包（nodetype、plugin、check 等）反向依赖本包。
package workflow

// PortSpec 描述某个 NodeType / 插件输入或输出端口的静态契约。
// ID 是 DSL 中跨节点引用使用的稳定标识；Name 仅用于展示。
type PortSpec struct {
	ID       string
	Name     string
	Type     SchemaType
	Required bool
	Desc     string
}

// SchemaType 是 PortSpec.Type 使用的最小 JSON Schema 子集，支持嵌套。
//   - Type == "object" 时使用 Properties
//   - Type == "array"  时使用 Items
//   - Enum 仅对 string / number / integer 生效
type SchemaType struct {
	Type       string
	Properties map[string]*SchemaType
	Items      *SchemaType
	Enum       []any
}

// 允许的 SchemaType.Type 字面量。
const (
	SchemaTypeString  = "string"
	SchemaTypeNumber  = "number"
	SchemaTypeInteger = "integer"
	SchemaTypeBoolean = "boolean"
	SchemaTypeObject  = "object"
	SchemaTypeArray   = "array"
	SchemaTypeAny     = "any"
)
```

- [ ] **Step 2：创建 `domain/workflow/value.go`**

```go
package workflow

// ValueKind 区分 ValueSource 的求值策略。
type ValueKind string

const (
	ValueKindLiteral  ValueKind = "literal"
	ValueKindRef      ValueKind = "ref"
	ValueKindTemplate ValueKind = "template"
)

// ValueSource 是 Node.Inputs 中每个输入端口的取值描述。
//   - Kind == ValueKindLiteral  → Value 直接是字面量
//   - Kind == ValueKindRef      → Value 应能解为 RefValue
//   - Kind == ValueKindTemplate → Value 是含 {{var}} 的模板字符串
type ValueSource struct {
	Kind  ValueKind
	Value any
}

// RefValue 引用上游某个节点输出端口的值（可选深路径）。
//   - NodeID  指向 DSL 内某个 Node.ID
//   - PortID  指向该节点 OutputSchema 中某个 PortSpec.ID
//   - Path    在 object 类型 Output 上的深路径，如 "data.voice_url"；可为空
//   - Name    冗余的端口显示名，仅给前端读，不参与运行时解析
type RefValue struct {
	NodeID string
	PortID string
	Path   string
	Name   string
}
```

- [ ] **Step 3：创建 `domain/workflow/error_policy.go`**

```go
package workflow

import "time"

// BackoffKind 指明 ErrorPolicy.RetryDelay 的退避策略。
type BackoffKind string

const (
	BackoffFixed       BackoffKind = "fixed"
	BackoffExponential BackoffKind = "exponential"
)

// FailStrategy 指明所有重试耗尽后的兜底行为。
type FailStrategy string

const (
	FailStrategyFireErrorPort FailStrategy = "fire_error_port"
	FailStrategyFallback      FailStrategy = "fallback"
	FailStrategyFailRun       FailStrategy = "fail_run"
)

// ErrorPolicy 描述节点的超时 / 重试 / 兜底策略。Node.ErrorPolicy 为 nil 时引擎使用默认策略。
type ErrorPolicy struct {
	Timeout      time.Duration
	MaxRetries   int
	RetryBackoff BackoffKind
	RetryDelay   time.Duration

	OnFinalFail    FailStrategy
	FallbackOutput map[string]any
}
```

- [ ] **Step 4：创建 `domain/workflow/node_ui.go`**

```go
package workflow

// NodeUI 仅供画布使用，不影响执行。Width / Height 为 nil 时使用 NodeType 默认尺寸。
type NodeUI struct {
	X      float64
	Y      float64
	Width  *float64
	Height *float64
}
```

- [ ] **Step 5：编译 + vet**

Run: `go build ./... && go vet ./...`
Expected: 无输出（成功）

- [ ] **Step 6：commit**

```bash
git add domain/workflow/port.go domain/workflow/value.go domain/workflow/error_policy.go domain/workflow/node_ui.go
git commit -m "feat(domain/workflow): add port / value / error_policy / node_ui value objects"
```

---

### Task 2: workflow DSL（Node / Edge / WorkflowDSL）

**Files:**
- Create: `domain/workflow/dsl.go`

本任务也是纯类型声明，不写测试。

- [ ] **Step 1：创建 `domain/workflow/dsl.go`**

```go
package workflow

import "encoding/json"

// 保留端口名 —— 所有 NodeType 都不能用这两个名字以外的语义。
const (
	PortDefault = "default"
	PortError   = "error"
)

// 当前 DSL JSON 的 schema 版本；序列化时写到 WorkflowDSL JSON 根上。
// 升级 DSL 物理结构时递增并实现 schema migration。
const DSLSchemaVersion = "1"

// Node 是 DSL 中的节点实例。
//   - TypeKey  指向 NodeTypeRegistry 中的某个 NodeType（如 "builtin.llm"、"plugin.http.<id>"）
//   - TypeVer  绑定到具体 NodeType 版本，便于 NodeType 演进时老 DSL 不破
//   - Inputs   key 是 NodeType.InputSchema 中 PortSpec.ID（不是 Name！）
//   - Config   是符合 NodeType.ConfigSchema 的 JSON；其中字符串字段允许 {{var}} 模板
type Node struct {
	ID      string
	TypeKey string
	TypeVer string
	Name    string

	Config json.RawMessage
	Inputs map[string]ValueSource

	ErrorPolicy *ErrorPolicy
	UI          NodeUI
}

// Edge 是节点之间的控制流边。本系统采用 context-passing 模型，目标节点直接读共享变量表，
// 因此不需要 ToPort，只声明源节点的输出端口。
//   - FromPort 取值来自源节点 NodeType.Ports（含保留端口 default / error）
type Edge struct {
	ID       string
	From     string
	FromPort string
	To       string
}

// WorkflowDSL 是工作流的"纯图"形态：不含名称 / 描述 / 版本号 / 时间戳。
type WorkflowDSL struct {
	Nodes []Node
	Edges []Edge
}
```

- [ ] **Step 2：编译 + vet**

Run: `go build ./... && go vet ./...`
Expected: 成功

- [ ] **Step 3：commit**

```bash
git add domain/workflow/dsl.go
git commit -m "feat(domain/workflow): add DSL types (Node / Edge / WorkflowDSL)"
```

---

### Task 3: workflow Definition & Version

**Files:**
- Create: `domain/workflow/definition.go`
- Create: `domain/workflow/version.go`

类型为主，但 `WorkflowVersion` 上提供两个不变式辅助方法（`IsDraft / IsRelease`），无需测试；状态机的复杂逻辑落在 Task 4 的仓储接口语义里，由仓储实现层 + Task 9 的校验器共同保证。

- [ ] **Step 1：创建 `domain/workflow/definition.go`**

```go
package workflow

import "time"

// VersionState 区分一个 WorkflowVersion 是 draft 还是 release。
type VersionState string

const (
	VersionStateDraft   VersionState = "draft"
	VersionStateRelease VersionState = "release"
)

// WorkflowDefinition 是工作流的稳定身份：ID 不变，名称 / 描述可改。
//   - DraftVersionID     当前 head 是否为 draft；nil 表示当前没有 draft（懒创建）
//   - PublishedVersionID 当前最高号的 release 版本；nil 表示从未发布
type WorkflowDefinition struct {
	ID          string
	Name        string
	Description string

	DraftVersionID     *string
	PublishedVersionID *string

	CreatedBy string
	CreatedAt time.Time
	UpdatedAt time.Time
}
```

- [ ] **Step 2：创建 `domain/workflow/version.go`**

```go
package workflow

import "time"

// WorkflowVersion 是 DSL 的承载行。
//
// 不变式（强约束，由仓储 / 校验器联合保证）：
//   - 同一 DefinitionID 下，所有 Version 字段单调递增、唯一
//   - 至多一条 State == VersionStateDraft；若存在，其 Version 号 ≥ 所有 release
//   - State == VersionStateRelease 后，DSL / Version / Revision / PublishedAt / PublishedBy 全部冻结
type WorkflowVersion struct {
	ID           string
	DefinitionID string

	Version int
	State   VersionState
	DSL     WorkflowDSL

	// Revision 是乐观并发版本号；每次 SaveVersion 自增；翻 release 后冻结。
	Revision int

	// PublishedAt / PublishedBy 仅在 State == VersionStateRelease 时非空。
	PublishedAt *time.Time
	PublishedBy *string

	CreatedAt time.Time
	UpdatedAt time.Time
}

// IsDraft 是否处于 draft 状态。
func (v *WorkflowVersion) IsDraft() bool { return v.State == VersionStateDraft }

// IsRelease 是否处于 release 状态。
func (v *WorkflowVersion) IsRelease() bool { return v.State == VersionStateRelease }
```

- [ ] **Step 3：编译 + vet**

Run: `go build ./... && go vet ./...`
Expected: 成功

- [ ] **Step 4：commit**

```bash
git add domain/workflow/definition.go domain/workflow/version.go
git commit -m "feat(domain/workflow): add WorkflowDefinition / WorkflowVersion + VersionState"
```

---

### Task 4: workflow 仓储接口 + 哨兵 error

**Files:**
- Create: `domain/workflow/repository.go`

仓储是接口定义，无逻辑，故无单元测试。哨兵 error 由后续仓储实现 / 校验器返回，本任务只声明。

- [ ] **Step 1：创建 `domain/workflow/repository.go`**

```go
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

	// ErrDraftValidation PublishVersion 时严格校验失败；调用方应配合 check.ValidationError 拿详情。
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
// 关键事务约束（由 application 层 spec 决定具体 plumbing 形态）：
//   - SaveVersion / PublishVersion / DiscardDraft 实现内部不得各自起事务，
//     必须接受外部传入的事务上下文，便于 application 层把 SaveVersion + PublishVersion
//     组合成"保存并发布"用例时共享同一事务、串行化并发请求。
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
	//   - 是 draft → 走严格校验（domain/workflow/check.ValidateForPublish），失败返回
	//     ErrDraftValidation（外层应包装具体的 ValidationError 列表）
	//
	// 校验通过后：
	//   UPDATE versions SET state='release', published_at=NOW(), published_by=$1 WHERE id=$VersionID
	//   UPDATE definitions SET draft_version_id=NULL, published_version_id=$VersionID WHERE id=$DefID
	PublishVersion(ctx context.Context, versionID, publishedBy string) (*WorkflowVersion, error)

	// DiscardDraft 若有 draft 则硬删并清 Definition.DraftVersionID；
	// 若无 draft 则静默成功（幂等，不返回错误）。
	// 已存在的 try-run 行不动（NodeRun 历史保留）。
	DiscardDraft(ctx context.Context, definitionID string) error
}
```

- [ ] **Step 2：编译 + vet**

Run: `go build ./... && go vet ./...`
Expected: 成功

- [ ] **Step 3：commit**

```bash
git add domain/workflow/repository.go
git commit -m "feat(domain/workflow): add WorkflowRepository contract and sentinel errors"
```

---

### Task 5: credential 包

**Files:**
- Create: `domain/credential/credential.go`
- Create: `domain/credential/resolver.go`
- Create: `domain/credential/repository.go`

类型 + 接口，无单元测试。

- [ ] **Step 1：创建 `domain/credential/credential.go`**

```go
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
```

- [ ] **Step 2：创建 `domain/credential/resolver.go`**

```go
package credential

import "context"

// Payload 是解密后的明文，结构按 Kind 不同：
//
//   APIKey  → {"key": "..."}
//   Bearer  → {"token": "..."}
//   Basic   → {"username": "...", "password": "..."}
//   Custom  → 任意 map[string]string
//
// domain 层只看到 map[string]string，避免为每种 Kind 单独建类型。
type Payload map[string]string

// CredentialResolver 是 Executor 唯一可以拿到明文凭证的入口。
// 实现负责按 Credential.Kind 解密 EncryptedPayload。
type CredentialResolver interface {
	Resolve(ctx context.Context, credID string) (Credential, Payload, error)
}
```

- [ ] **Step 3：创建 `domain/credential/repository.go`**

```go
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
```

- [ ] **Step 4：编译 + vet**

Run: `go build ./... && go vet ./...`
Expected: 成功

- [ ] **Step 5：commit**

```bash
git add domain/credential/
git commit -m "feat(domain/credential): add Credential aggregate, Resolver and Repository contracts"
```

---

### Task 6: plugin 包

**Files:**
- Create: `domain/plugin/http_plugin.go`
- Create: `domain/plugin/mcp_server.go`
- Create: `domain/plugin/mcp_tool.go`
- Create: `domain/plugin/repository.go`

依赖 `domain/workflow.PortSpec`。类型为主，无单元测试。

- [ ] **Step 1：创建 `domain/plugin/http_plugin.go`**

```go
// Package plugin 定义 HTTP 插件 / MCP Server / MCP Tool 三类外部能力的聚合。
package plugin

import (
	"time"

	"github.com/shinya/shineflow/domain/workflow"
)

// HttpPlugin 描述一个由用户配置的通用 HTTP 能力。
// 在 NodeTypeRegistry 中会被投影成 NodeType "plugin.http.<HttpPlugin.ID>"。
type HttpPlugin struct {
	ID          string
	Name        string
	Description string

	// 请求构造
	Method       string
	URL          string
	Headers      map[string]string
	QueryParams  map[string]string
	BodyTemplate string

	// 认证
	AuthKind     string // "none" | "api_key" | "bearer" | "basic"
	CredentialID *string

	// 端口契约
	InputSchema  []workflow.PortSpec
	OutputSchema []workflow.PortSpec

	// 响应映射：OutputSchema 端口名 → JSONPath；未映射的端口尝试按同名顶层字段取
	ResponseMapping map[string]string

	Enabled   bool
	CreatedBy string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// HttpPlugin.AuthKind 允许的字面量。
const (
	HttpAuthNone   = "none"
	HttpAuthAPIKey = "api_key"
	HttpAuthBearer = "bearer"
	HttpAuthBasic  = "basic"
)
```

- [ ] **Step 2：创建 `domain/plugin/mcp_server.go`**

```go
package plugin

import (
	"encoding/json"
	"time"
)

// McpTransport 决定 McpServer.Config 应反序列化成什么。
type McpTransport string

const (
	McpTransportStdio McpTransport = "stdio"
	McpTransportHTTP  McpTransport = "http"
	McpTransportSSE   McpTransport = "sse"
)

// McpServer 是一个对外的 MCP 服务实例；其下可同步出多个 McpTool。
type McpServer struct {
	ID   string
	Name string

	Transport McpTransport
	// Config 按 Transport 解码：stdio → {Command, Args, Env}；http/sse → {URL, ...}
	Config json.RawMessage

	CredentialID *string

	Enabled       bool
	LastSyncedAt  *time.Time
	LastSyncError *string

	CreatedBy string
	CreatedAt time.Time
	UpdatedAt time.Time
}
```

- [ ] **Step 3：创建 `domain/plugin/mcp_tool.go`**

```go
package plugin

import (
	"encoding/json"
	"time"
)

// McpTool 是某个 McpServer 同步出的 tool 元数据。
// 由系统按 MCP 协议同步而来，用户只能 enable / disable，不能直接 CRUD。
type McpTool struct {
	ID          string
	ServerID    string
	Name        string
	Description string

	// MCP 返回的原生 JSON Schema；在投影成 NodeType.InputSchema 时按 §7.5 降维
	InputSchemaRaw json.RawMessage

	Enabled  bool
	SyncedAt time.Time
}
```

- [ ] **Step 4：创建 `domain/plugin/repository.go`**

```go
package plugin

import (
	"context"
	"errors"
)

var (
	ErrHttpPluginNotFound = errors.New("plugin: http plugin not found")
	ErrMcpServerNotFound  = errors.New("plugin: mcp server not found")
	ErrMcpToolNotFound    = errors.New("plugin: mcp tool not found")
)

// HttpPluginFilter 是 ListHttpPlugins 的过滤参数。
type HttpPluginFilter struct {
	EnabledOnly bool
	CreatedBy   string
	Limit       int
	Offset      int
}

// McpServerFilter 是 ListMcpServers 的过滤参数。
type McpServerFilter struct {
	EnabledOnly bool
	Limit       int
	Offset      int
}

// HttpPluginRepository 是 HttpPlugin 聚合的存储契约。
type HttpPluginRepository interface {
	Create(ctx context.Context, p *HttpPlugin) error
	Get(ctx context.Context, id string) (*HttpPlugin, error)
	List(ctx context.Context, filter HttpPluginFilter) ([]*HttpPlugin, error)
	Update(ctx context.Context, p *HttpPlugin) error
	Delete(ctx context.Context, id string) error
}

// McpServerRepository 是 McpServer 聚合的存储契约。
type McpServerRepository interface {
	Create(ctx context.Context, s *McpServer) error
	Get(ctx context.Context, id string) (*McpServer, error)
	List(ctx context.Context, filter McpServerFilter) ([]*McpServer, error)
	Update(ctx context.Context, s *McpServer) error
	Delete(ctx context.Context, id string) error
}

// McpToolRepository 是 McpTool 实体的存储契约。
//   - UpsertAll 在某个 server 一次同步后整体覆盖该 server 名下的 tools
//   - SetEnabled 仅切换 Enabled 字段
type McpToolRepository interface {
	GetByServerAndName(ctx context.Context, serverID, name string) (*McpTool, error)
	ListByServer(ctx context.Context, serverID string) ([]*McpTool, error)
	UpsertAll(ctx context.Context, serverID string, tools []*McpTool) error
	SetEnabled(ctx context.Context, id string, enabled bool) error
}
```

- [ ] **Step 5：编译 + vet**

Run: `go build ./... && go vet ./...`
Expected: 成功

- [ ] **Step 6：commit**

```bash
git add domain/plugin/
git commit -m "feat(domain/plugin): add HttpPlugin / McpServer / McpTool aggregates and repositories"
```

---

### Task 7: nodetype 类型 + 内置 Key + Registry 接口

**Files:**
- Create: `domain/nodetype/nodetype.go`
- Create: `domain/nodetype/builtin.go`
- Create: `domain/nodetype/registry.go`

类型 + 接口，无逻辑，不写测试。

- [ ] **Step 1：创建 `domain/nodetype/nodetype.go`**

```go
// Package nodetype 定义"节点类型"的统一目录。
//
// 关键设计：
//   - NodeType 不对应物理表，由 NodeTypeRegistry 现场合成
//   - 内置节点 / HttpPlugin / McpTool 投影出来的 NodeType 对调用方完全同构
//   - 前端节点面板、引擎执行器都只认 NodeType.Key
package nodetype

import (
	"encoding/json"

	"github.com/shinya/shineflow/domain/workflow"
)

// NodeType 是节点类型的元信息。
//
//   - Key      全局唯一，模式见 §7.3：builtin.* / plugin.http.<id> / plugin.mcp.<server>.<tool>
//   - Version  NodeType 自身契约的版本（与 WorkflowVersion.Version 不同）；
//              v1 内统一填 NodeTypeVersion1
//   - Ports    输出控制端口列表；默认 [PortDefault, PortError]
type NodeType struct {
	Key         string
	Version     string
	Name        string
	Description string
	Category    string
	Builtin     bool

	ConfigSchema json.RawMessage
	InputSchema  []workflow.PortSpec
	OutputSchema []workflow.PortSpec
	Ports        []string
}

// NodeType 自身契约的初始版本号，v1 内全部 NodeType 都填这个。
const NodeTypeVersion1 = "1"

// NodeType.Category 允许的字面量。
const (
	CategoryAI      = "AI"
	CategoryTool    = "Tool"
	CategoryControl = "Control"
	CategoryBasic   = "Basic"
)
```

- [ ] **Step 2：创建 `domain/nodetype/builtin.go`**

```go
package nodetype

// 内置 NodeType 的 Key 常量。由代码 init 时静态注册到 Registry。
const (
	BuiltinStart        = "builtin.start"
	BuiltinEnd          = "builtin.end"
	BuiltinLLM          = "builtin.llm"
	BuiltinIf           = "builtin.if"
	BuiltinSwitch       = "builtin.switch"
	BuiltinLoop         = "builtin.loop"
	BuiltinCode         = "builtin.code"
	BuiltinSetVariable  = "builtin.set_variable"
	BuiltinHTTPRequest  = "builtin.http_request"
)

// 插件 NodeType Key 的前缀；用于 Registry 投影 / 失效检索。
const (
	PluginHTTPPrefix = "plugin.http." // plugin.http.<HttpPlugin.ID>
	PluginMCPPrefix  = "plugin.mcp."  // plugin.mcp.<McpServer.ID>.<McpTool.Name>
)

// If 节点的两条非默认控制端口名。
// 其余保留端口（default / error）用 workflow.PortDefault / workflow.PortError，避免重复常量。
const (
	PortIfTrue  = "true"
	PortIfFalse = "false"
)
```

- [ ] **Step 3：创建 `domain/nodetype/registry.go`**

```go
package nodetype

// NodeTypeFilter 是 List 的过滤参数。空字段视为不约束。
type NodeTypeFilter struct {
	Category    string
	Builtin     *bool
	KeyPrefixes []string
}

// NodeTypeRegistry 是 NodeType 的统一查询入口。
//
// 实现要求（具体 impl 在 infrastructure 或 application 层）：
//   - Get 命中内置节点 → 返回静态 map 中的常量
//   - Get 命中 plugin.http.* → 调 HttpPluginRepository 后用 projectHttpPlugin 合成
//   - Get 命中 plugin.mcp.*.* → 调 McpServer + McpTool Repository 后用 projectMcpTool 合成
//   - 合成结果可缓存；HttpPlugin / McpServer / McpTool 变更时调 Invalidate / InvalidatePrefix 失效
type NodeTypeRegistry interface {
	Get(key string) (*NodeType, bool)
	List(filter NodeTypeFilter) []*NodeType

	Invalidate(key string)
	InvalidatePrefix(prefix string)
}
```

- [ ] **Step 4：编译 + vet**

Run: `go build ./... && go vet ./...`
Expected: 成功

- [ ] **Step 5：commit**

```bash
git add domain/nodetype/nodetype.go domain/nodetype/builtin.go domain/nodetype/registry.go
git commit -m "feat(domain/nodetype): add NodeType, builtin keys and Registry contract"
```

---

### Task 8: nodetype 投影函数

**Files:**
- Create: `domain/nodetype/projection.go`
- Create: `domain/nodetype/projection_test.go`

把 `HttpPlugin` / `McpTool` 投影成 `NodeType` 是纯函数，必须 TDD。

- [ ] **Step 1：先写失败测试 `domain/nodetype/projection_test.go`**

```go
package nodetype

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/shinya/shineflow/domain/plugin"
	"github.com/shinya/shineflow/domain/workflow"
)

func TestProjectHttpPlugin(t *testing.T) {
	p := &plugin.HttpPlugin{
		ID:          "hp_001",
		Name:        "Translate API",
		Description: "translate text",
		InputSchema: []workflow.PortSpec{
			{ID: "in_1", Name: "text", Type: workflow.SchemaType{Type: workflow.SchemaTypeString}, Required: true},
		},
		OutputSchema: []workflow.PortSpec{
			{ID: "out_1", Name: "translated", Type: workflow.SchemaType{Type: workflow.SchemaTypeString}},
		},
	}

	got := ProjectHttpPlugin(p)

	if got.Key != "plugin.http.hp_001" {
		t.Fatalf("Key = %q, want %q", got.Key, "plugin.http.hp_001")
	}
	if got.Version != NodeTypeVersion1 {
		t.Errorf("Version = %q, want %q", got.Version, NodeTypeVersion1)
	}
	if got.Name != p.Name || got.Description != p.Description {
		t.Errorf("Name/Description not propagated; got=%+v", got)
	}
	if got.Category != CategoryTool {
		t.Errorf("Category = %q, want %q", got.Category, CategoryTool)
	}
	if got.Builtin {
		t.Error("Builtin should be false for plugin")
	}
	if string(got.ConfigSchema) != "{}" {
		t.Errorf("ConfigSchema = %s, want '{}'", got.ConfigSchema)
	}
	if !reflect.DeepEqual(got.InputSchema, p.InputSchema) {
		t.Errorf("InputSchema mismatch")
	}
	if !reflect.DeepEqual(got.OutputSchema, p.OutputSchema) {
		t.Errorf("OutputSchema mismatch")
	}
	wantPorts := []string{workflow.PortDefault, workflow.PortError}
	if !reflect.DeepEqual(got.Ports, wantPorts) {
		t.Errorf("Ports = %v, want %v", got.Ports, wantPorts)
	}
}

func TestProjectMcpTool(t *testing.T) {
	server := &plugin.McpServer{ID: "svr_1", Name: "FS"}
	tool := &plugin.McpTool{
		ServerID:    "svr_1",
		Name:        "read_file",
		Description: "read a file",
		InputSchemaRaw: json.RawMessage(`{
			"type":"object",
			"properties":{"path":{"type":"string"}},
			"required":["path"]
		}`),
	}

	got := ProjectMcpTool(tool, server)

	if got.Key != "plugin.mcp.svr_1.read_file" {
		t.Fatalf("Key = %q", got.Key)
	}
	if got.Name != "FS / read_file" {
		t.Errorf("Name = %q, want %q", got.Name, "FS / read_file")
	}
	if got.Description != tool.Description {
		t.Errorf("Description not propagated")
	}
	if got.Category != CategoryTool || got.Builtin {
		t.Errorf("Category/Builtin wrong: %+v", got)
	}
	if len(got.OutputSchema) != 1 || got.OutputSchema[0].Name != "result" {
		t.Errorf("OutputSchema should be a single 'result' port, got %+v", got.OutputSchema)
	}
	if got.OutputSchema[0].ID == "" {
		t.Error("OutputSchema port ID should be a stable hash, not empty")
	}
	wantPorts := []string{workflow.PortDefault, workflow.PortError}
	if !reflect.DeepEqual(got.Ports, wantPorts) {
		t.Errorf("Ports = %v, want %v", got.Ports, wantPorts)
	}
	if len(got.InputSchema) != 1 || got.InputSchema[0].Name != "path" || !got.InputSchema[0].Required {
		t.Errorf("InputSchema降维结果不符: %+v", got.InputSchema)
	}
}

func TestMcpSchemaToPortSpecs_FlatObject(t *testing.T) {
	raw := json.RawMessage(`{
		"type":"object",
		"properties":{
			"a":{"type":"string","description":"hello"},
			"b":{"type":"integer"}
		},
		"required":["a"]
	}`)
	ports := mcpSchemaToPortSpecs(raw)

	if len(ports) != 2 {
		t.Fatalf("len = %d, want 2", len(ports))
	}
	byName := map[string]workflow.PortSpec{}
	for _, p := range ports {
		byName[p.Name] = p
	}
	if !byName["a"].Required || byName["a"].Type.Type != workflow.SchemaTypeString || byName["a"].Desc != "hello" {
		t.Errorf("port a wrong: %+v", byName["a"])
	}
	if byName["b"].Required || byName["b"].Type.Type != workflow.SchemaTypeInteger {
		t.Errorf("port b wrong: %+v", byName["b"])
	}
}

func TestMcpSchemaToPortSpecs_NotObject(t *testing.T) {
	// 顶层不是 object 时降维结果应是空切片（MCP tool 的 inputSchema 总应该是 object）
	raw := json.RawMessage(`{"type":"string"}`)
	ports := mcpSchemaToPortSpecs(raw)
	if len(ports) != 0 {
		t.Errorf("len = %d, want 0", len(ports))
	}
}
```

- [ ] **Step 2：跑测试确认失败**

Run: `go test ./domain/nodetype/...`
Expected: FAIL — `undefined: ProjectHttpPlugin / ProjectMcpTool / mcpSchemaToPortSpecs`

- [ ] **Step 3：创建 `domain/nodetype/projection.go`**

```go
package nodetype

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/shinya/shineflow/domain/plugin"
	"github.com/shinya/shineflow/domain/workflow"
)

// ProjectHttpPlugin 把 HttpPlugin 投影成 NodeType。
//
// 约束：
//   - HttpPlugin 自身已配置好请求骨架，故 ConfigSchema 固定为空对象 "{}"
//   - InputSchema / OutputSchema 直接透传
//   - Ports 固定 [default, error]
func ProjectHttpPlugin(p *plugin.HttpPlugin) *NodeType {
	return &NodeType{
		Key:          PluginHTTPPrefix + p.ID,
		Version:      NodeTypeVersion1,
		Name:         p.Name,
		Description:  p.Description,
		Category:     CategoryTool,
		Builtin:      false,
		ConfigSchema: json.RawMessage(`{}`),
		InputSchema:  p.InputSchema,
		OutputSchema: p.OutputSchema,
		Ports:        []string{workflow.PortDefault, workflow.PortError},
	}
}

// ProjectMcpTool 把 (McpTool, McpServer) 投影成 NodeType。
//
// 约束：
//   - InputSchema 由 mcpSchemaToPortSpecs 把 MCP 原生 JSON Schema 顶层 properties 降维而来
//   - OutputSchema 固定为单端口 "result"，类型 object；ID 由 (server.ID, tool.Name) 派生稳定 hash
func ProjectMcpTool(t *plugin.McpTool, s *plugin.McpServer) *NodeType {
	return &NodeType{
		Key:          fmt.Sprintf("%s%s.%s", PluginMCPPrefix, s.ID, t.Name),
		Version:      NodeTypeVersion1,
		Name:         fmt.Sprintf("%s / %s", s.Name, t.Name),
		Description:  t.Description,
		Category:     CategoryTool,
		Builtin:      false,
		ConfigSchema: json.RawMessage(`{}`),
		InputSchema:  mcpSchemaToPortSpecs(t.InputSchemaRaw),
		OutputSchema: []workflow.PortSpec{{
			ID:   "mcp_result_" + stableHash(s.ID+":"+t.Name),
			Name: "result",
			Type: workflow.SchemaType{Type: workflow.SchemaTypeObject},
		}},
		Ports: []string{workflow.PortDefault, workflow.PortError},
	}
}

// mcpSchemaToPortSpecs 把 MCP tool 的原生 JSON Schema（顶层应为 object）的 properties
// 降维成 []PortSpec。每个顶层 property 对应一个 PortSpec，类型只取 type 字段（嵌套留给 SchemaType）。
//
// PortSpec.ID 形如 "mcp_in_<sha1(name)前 8 字节>"，与 name 一一对应、可稳定回查。
// 若 raw 不是 object 或解析失败，返回 nil。
func mcpSchemaToPortSpecs(raw json.RawMessage) []workflow.PortSpec {
	if len(raw) == 0 {
		return nil
	}
	var schema struct {
		Type       string                     `json:"type"`
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                   `json:"required"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		return nil
	}
	if schema.Type != workflow.SchemaTypeObject {
		return nil
	}

	requiredSet := map[string]bool{}
	for _, r := range schema.Required {
		requiredSet[r] = true
	}

	names := make([]string, 0, len(schema.Properties))
	for name := range schema.Properties {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]workflow.PortSpec, 0, len(names))
	for _, name := range names {
		var prop struct {
			Type        string `json:"type"`
			Description string `json:"description"`
		}
		_ = json.Unmarshal(schema.Properties[name], &prop)
		out = append(out, workflow.PortSpec{
			ID:       "mcp_in_" + stableHash(name),
			Name:     name,
			Type:     workflow.SchemaType{Type: prop.Type},
			Required: requiredSet[name],
			Desc:     prop.Description,
		})
	}
	return out
}

func stableHash(s string) string {
	sum := sha1.Sum([]byte(s))
	return hex.EncodeToString(sum[:])[:16]
}
```

- [ ] **Step 4：跑测试确认通过**

Run: `go test ./domain/nodetype/... -v`
Expected: PASS — 全部 4 个 test

- [ ] **Step 5：编译 + vet**

Run: `go build ./... && go vet ./...`
Expected: 成功

- [ ] **Step 6：commit**

```bash
git add domain/nodetype/projection.go domain/nodetype/projection_test.go
git commit -m "feat(domain/nodetype): add HttpPlugin / McpTool projection functions"
```

---

### Task 9: workflow/check 严格校验器

**Files:**
- Create: `domain/workflow/check/validator.go`
- Create: `domain/workflow/check/validator_test.go`

实现 spec §6.6 严格校验的 8 条规则。这是本计划逻辑最重的一块，必须完整覆盖每条规则。

- [ ] **Step 1：先写失败测试 `domain/workflow/check/validator_test.go`**

测试用一个内联 fakeRegistry 替代真实 NodeTypeRegistry。

```go
package check

import (
	"strings"
	"testing"

	"github.com/shinya/shineflow/domain/nodetype"
	"github.com/shinya/shineflow/domain/workflow"
)

// fakeRegistry 在测试中按需返回 NodeType。
type fakeRegistry struct {
	types map[string]*nodetype.NodeType
}

func (f *fakeRegistry) Get(key string) (*nodetype.NodeType, bool) {
	nt, ok := f.types[key]
	return nt, ok
}
func (f *fakeRegistry) List(_ nodetype.NodeTypeFilter) []*nodetype.NodeType { return nil }
func (f *fakeRegistry) Invalidate(_ string)                                 {}
func (f *fakeRegistry) InvalidatePrefix(_ string)                           {}

// builtinTypes 构造 8 个常用内置 NodeType，便于复用。
func builtinTypes() map[string]*nodetype.NodeType {
	defaultPorts := []string{workflow.PortDefault, workflow.PortError}
	return map[string]*nodetype.NodeType{
		nodetype.BuiltinStart: {Key: nodetype.BuiltinStart, Ports: []string{workflow.PortDefault}},
		nodetype.BuiltinEnd:   {Key: nodetype.BuiltinEnd, Ports: []string{}},
		nodetype.BuiltinLLM:   {Key: nodetype.BuiltinLLM, Ports: defaultPorts},
		nodetype.BuiltinIf: {Key: nodetype.BuiltinIf, Ports: []string{
			nodetype.PortIfTrue, nodetype.PortIfFalse, workflow.PortError,
		}},
	}
}

// minimalDSL 构造一个能通过严格校验的最小 DSL：start → llm → end。
func minimalDSL() workflow.WorkflowDSL {
	return workflow.WorkflowDSL{
		Nodes: []workflow.Node{
			{ID: "n_start", TypeKey: nodetype.BuiltinStart, TypeVer: nodetype.NodeTypeVersion1},
			{ID: "n_llm", TypeKey: nodetype.BuiltinLLM, TypeVer: nodetype.NodeTypeVersion1},
			{ID: "n_end", TypeKey: nodetype.BuiltinEnd, TypeVer: nodetype.NodeTypeVersion1},
		},
		Edges: []workflow.Edge{
			{ID: "e1", From: "n_start", FromPort: workflow.PortDefault, To: "n_llm"},
			{ID: "e2", From: "n_llm", FromPort: workflow.PortDefault, To: "n_end"},
		},
	}
}

func TestValidate_Minimal_Pass(t *testing.T) {
	res := ValidateForPublish(minimalDSL(), &fakeRegistry{types: builtinTypes()})
	if !res.OK() {
		t.Fatalf("expected pass, got: %+v", res.Errors)
	}
}

func TestValidate_NoStartOrEnd(t *testing.T) {
	dsl := workflow.WorkflowDSL{
		Nodes: []workflow.Node{
			{ID: "n_llm", TypeKey: nodetype.BuiltinLLM, TypeVer: nodetype.NodeTypeVersion1},
		},
	}
	res := ValidateForPublish(dsl, &fakeRegistry{types: builtinTypes()})
	if res.OK() {
		t.Fatal("expected failure for missing start/end")
	}
	mustHaveCode(t, res, CodeMissingStart)
	mustHaveCode(t, res, CodeMissingEnd)
}

func TestValidate_DuplicateNodeID(t *testing.T) {
	dsl := minimalDSL()
	dsl.Nodes = append(dsl.Nodes, workflow.Node{
		ID: "n_llm", TypeKey: nodetype.BuiltinLLM, TypeVer: nodetype.NodeTypeVersion1,
	})
	res := ValidateForPublish(dsl, &fakeRegistry{types: builtinTypes()})
	mustHaveCode(t, res, CodeDuplicateNodeID)
}

func TestValidate_DuplicateEdgeID(t *testing.T) {
	dsl := minimalDSL()
	dsl.Edges = append(dsl.Edges, workflow.Edge{
		ID: "e1", From: "n_start", FromPort: workflow.PortDefault, To: "n_llm",
	})
	res := ValidateForPublish(dsl, &fakeRegistry{types: builtinTypes()})
	mustHaveCode(t, res, CodeDuplicateEdgeID)
}

func TestValidate_DanglingEdge(t *testing.T) {
	dsl := minimalDSL()
	dsl.Edges = append(dsl.Edges, workflow.Edge{
		ID: "e_bad", From: "n_llm", FromPort: workflow.PortDefault, To: "n_ghost",
	})
	res := ValidateForPublish(dsl, &fakeRegistry{types: builtinTypes()})
	mustHaveCode(t, res, CodeDanglingEdge)
}

func TestValidate_DanglingRef(t *testing.T) {
	dsl := minimalDSL()
	// llm 节点引用一个不存在的 node
	dsl.Nodes[1].Inputs = map[string]workflow.ValueSource{
		"in_prompt": {
			Kind:  workflow.ValueKindRef,
			Value: workflow.RefValue{NodeID: "n_ghost", PortID: "out_1"},
		},
	}
	res := ValidateForPublish(dsl, &fakeRegistry{types: builtinTypes()})
	mustHaveCode(t, res, CodeDanglingRef)
}

func TestValidate_UnknownFromPort(t *testing.T) {
	dsl := minimalDSL()
	dsl.Edges[1].FromPort = "wat"
	res := ValidateForPublish(dsl, &fakeRegistry{types: builtinTypes()})
	mustHaveCode(t, res, CodeUnknownFromPort)
}

func TestValidate_RequiredInputMissing(t *testing.T) {
	types := builtinTypes()
	types[nodetype.BuiltinLLM] = &nodetype.NodeType{
		Key:   nodetype.BuiltinLLM,
		Ports: []string{workflow.PortDefault, workflow.PortError},
		InputSchema: []workflow.PortSpec{
			{ID: "in_prompt", Name: "prompt", Required: true,
				Type: workflow.SchemaType{Type: workflow.SchemaTypeString}},
		},
	}
	res := ValidateForPublish(minimalDSL(), &fakeRegistry{types: types})
	mustHaveCode(t, res, CodeRequiredInputMissing)
}

func TestValidate_UnknownNodeType(t *testing.T) {
	dsl := minimalDSL()
	dsl.Nodes[1].TypeKey = "builtin.unknown"
	res := ValidateForPublish(dsl, &fakeRegistry{types: builtinTypes()})
	mustHaveCode(t, res, CodeUnknownNodeType)
}

func TestValidate_FallbackOnNodeWithoutDefault(t *testing.T) {
	types := builtinTypes()
	// switch 没有 default port
	types[nodetype.BuiltinSwitch] = &nodetype.NodeType{
		Key:   nodetype.BuiltinSwitch,
		Ports: []string{"case_1", workflow.PortError},
	}
	dsl := minimalDSL()
	dsl.Nodes[1] = workflow.Node{
		ID:      "n_llm",
		TypeKey: nodetype.BuiltinSwitch,
		TypeVer: nodetype.NodeTypeVersion1,
		ErrorPolicy: &workflow.ErrorPolicy{
			OnFinalFail: workflow.FailStrategyFallback,
		},
	}
	dsl.Edges = []workflow.Edge{
		{ID: "e1", From: "n_start", FromPort: workflow.PortDefault, To: "n_llm"},
		{ID: "e2", From: "n_llm", FromPort: "case_1", To: "n_end"},
	}
	res := ValidateForPublish(dsl, &fakeRegistry{types: types})
	mustHaveCode(t, res, CodeFallbackOnNonDefaultPortNode)
}

func TestValidate_Cycle(t *testing.T) {
	dsl := workflow.WorkflowDSL{
		Nodes: []workflow.Node{
			{ID: "n_start", TypeKey: nodetype.BuiltinStart, TypeVer: nodetype.NodeTypeVersion1},
			{ID: "a", TypeKey: nodetype.BuiltinLLM, TypeVer: nodetype.NodeTypeVersion1},
			{ID: "b", TypeKey: nodetype.BuiltinLLM, TypeVer: nodetype.NodeTypeVersion1},
			{ID: "n_end", TypeKey: nodetype.BuiltinEnd, TypeVer: nodetype.NodeTypeVersion1},
		},
		Edges: []workflow.Edge{
			{ID: "e1", From: "n_start", FromPort: workflow.PortDefault, To: "a"},
			{ID: "e2", From: "a", FromPort: workflow.PortDefault, To: "b"},
			{ID: "e3", From: "b", FromPort: workflow.PortDefault, To: "a"},
			{ID: "e4", From: "a", FromPort: workflow.PortDefault, To: "n_end"},
		},
	}
	res := ValidateForPublish(dsl, &fakeRegistry{types: builtinTypes()})
	mustHaveCode(t, res, CodeCycle)
}

func TestValidate_AllErrorsReturnedAtOnce(t *testing.T) {
	// 同时缺 start、缺 end、edge 悬空
	dsl := workflow.WorkflowDSL{
		Nodes: []workflow.Node{
			{ID: "a", TypeKey: nodetype.BuiltinLLM, TypeVer: nodetype.NodeTypeVersion1},
		},
		Edges: []workflow.Edge{
			{ID: "e1", From: "a", FromPort: workflow.PortDefault, To: "ghost"},
		},
	}
	res := ValidateForPublish(dsl, &fakeRegistry{types: builtinTypes()})
	codes := codeSet(res)
	for _, want := range []string{CodeMissingStart, CodeMissingEnd, CodeDanglingEdge} {
		if !codes[want] {
			t.Errorf("missing code %q in %v", want, codes)
		}
	}
}

// helpers

func mustHaveCode(t *testing.T, r ValidationResult, code string) {
	t.Helper()
	for _, e := range r.Errors {
		if e.Code == code {
			return
		}
	}
	dump := []string{}
	for _, e := range r.Errors {
		dump = append(dump, e.Code+":"+e.Message)
	}
	t.Fatalf("expected code %q, got: %s", code, strings.Join(dump, " | "))
}

func codeSet(r ValidationResult) map[string]bool {
	out := map[string]bool{}
	for _, e := range r.Errors {
		out[e.Code] = true
	}
	return out
}
```

- [ ] **Step 2：跑测试确认全部失败**

Run: `go test ./domain/workflow/check/...`
Expected: FAIL（package undefined）

- [ ] **Step 3：创建 `domain/workflow/check/validator.go`**

```go
// Package check 实现 WorkflowDSL 的严格校验（PublishVersion 时必过）。
//
// 本包独立于 domain/workflow，避免引入 workflow → nodetype → workflow 的循环依赖。
package check

import (
	"fmt"

	"github.com/shinya/shineflow/domain/nodetype"
	"github.com/shinya/shineflow/domain/workflow"
)

// ValidationError 描述一条校验违例。Code 是可机读分类，Message 是给用户的可读描述，
// Path 指向出错位置（如 "nodes[2].inputs.in_prompt"），可为空。
type ValidationError struct {
	Code    string
	Message string
	Path    string
}

// 校验违例 Code 常量；与 spec §6.6 的 8 条规则一一对应。
const (
	CodeMissingStart                 = "missing_start"
	CodeMissingEnd                   = "missing_end"
	CodeDuplicateNodeID              = "duplicate_node_id"
	CodeDuplicateEdgeID              = "duplicate_edge_id"
	CodeDuplicatePortID              = "duplicate_port_id"
	CodeDanglingEdge                 = "dangling_edge"
	CodeDanglingRef                  = "dangling_ref"
	CodeUnknownFromPort              = "unknown_from_port"
	CodeRequiredInputMissing         = "required_input_missing"
	CodeUnknownNodeType              = "unknown_node_type"
	CodeFallbackOnNonDefaultPortNode = "fallback_on_non_default_port_node"
	CodeCycle                        = "cycle"
)

// ValidationResult 是 ValidateForPublish 的返回值，一次性收集所有违例。
type ValidationResult struct {
	Errors []ValidationError
}

// OK 是否通过校验。
func (r ValidationResult) OK() bool { return len(r.Errors) == 0 }

// ValidateForPublish 对一个 WorkflowDSL 做 spec §6.6 的全部 8 条严格校验，
// 不在第一条违例时短路，而是收集所有违例后一并返回，便于前端一次提示。
func ValidateForPublish(dsl workflow.WorkflowDSL, reg nodetype.NodeTypeRegistry) ValidationResult {
	var errs []ValidationError

	errs = append(errs, checkStartEnd(dsl)...)
	errs = append(errs, checkUniqueIDs(dsl)...)

	nodeByID := map[string]*workflow.Node{}
	for i := range dsl.Nodes {
		nodeByID[dsl.Nodes[i].ID] = &dsl.Nodes[i]
	}

	typeCache := map[string]*nodetype.NodeType{}
	getType := func(key string) (*nodetype.NodeType, bool) {
		if nt, ok := typeCache[key]; ok {
			return nt, nt != nil
		}
		nt, ok := reg.Get(key)
		typeCache[key] = nt
		if !ok {
			return nil, false
		}
		return nt, true
	}

	errs = append(errs, checkNodeTypesExist(dsl, getType)...)
	errs = append(errs, checkEdgeTargets(dsl, nodeByID)...)
	errs = append(errs, checkEdgeFromPorts(dsl, nodeByID, getType)...)
	errs = append(errs, checkRefValues(dsl, nodeByID, getType)...)
	errs = append(errs, checkRequiredInputs(dsl, getType)...)
	errs = append(errs, checkFallbackOnly(dsl, getType)...)
	errs = append(errs, checkAcyclic(dsl, nodeByID)...)

	return ValidationResult{Errors: errs}
}

// 规则 1：至少 1 个 builtin.start，至少 1 个 builtin.end。
func checkStartEnd(dsl workflow.WorkflowDSL) []ValidationError {
	hasStart, hasEnd := false, false
	for _, n := range dsl.Nodes {
		switch n.TypeKey {
		case nodetype.BuiltinStart:
			hasStart = true
		case nodetype.BuiltinEnd:
			hasEnd = true
		}
	}
	var out []ValidationError
	if !hasStart {
		out = append(out, ValidationError{Code: CodeMissingStart, Message: "DSL must contain at least one builtin.start node"})
	}
	if !hasEnd {
		out = append(out, ValidationError{Code: CodeMissingEnd, Message: "DSL must contain at least one builtin.end node"})
	}
	return out
}

// 规则 2：Node.ID / Edge.ID / 单节点内 PortID 唯一。
func checkUniqueIDs(dsl workflow.WorkflowDSL) []ValidationError {
	var out []ValidationError
	seenN := map[string]int{}
	for i, n := range dsl.Nodes {
		if first, ok := seenN[n.ID]; ok {
			out = append(out, ValidationError{
				Code:    CodeDuplicateNodeID,
				Message: fmt.Sprintf("node id %q used twice (nodes[%d] and nodes[%d])", n.ID, first, i),
				Path:    fmt.Sprintf("nodes[%d].id", i),
			})
		} else {
			seenN[n.ID] = i
		}
	}
	seenE := map[string]int{}
	for i, e := range dsl.Edges {
		if first, ok := seenE[e.ID]; ok {
			out = append(out, ValidationError{
				Code:    CodeDuplicateEdgeID,
				Message: fmt.Sprintf("edge id %q used twice (edges[%d] and edges[%d])", e.ID, first, i),
				Path:    fmt.Sprintf("edges[%d].id", i),
			})
		} else {
			seenE[e.ID] = i
		}
	}
	// PortID 唯一性：在每个 Node.Inputs 的 key 集合内（key 即 PortID）。Inputs 是 map，天然 key 唯一，
	// 这里仅占位规则；如未来 PortSpec 落到 DSL 内还需扩展。
	return out
}

// 规则 6：Node.TypeKey 必须能在 Registry 解析到。
func checkNodeTypesExist(dsl workflow.WorkflowDSL, getType func(string) (*nodetype.NodeType, bool)) []ValidationError {
	var out []ValidationError
	for i, n := range dsl.Nodes {
		if _, ok := getType(n.TypeKey); !ok {
			out = append(out, ValidationError{
				Code:    CodeUnknownNodeType,
				Message: fmt.Sprintf("unknown NodeType %q on node %q", n.TypeKey, n.ID),
				Path:    fmt.Sprintf("nodes[%d].type_key", i),
			})
		}
	}
	return out
}

// 规则 3a：Edge.From / To 必须指向 DSL 内真实节点。
func checkEdgeTargets(dsl workflow.WorkflowDSL, nodeByID map[string]*workflow.Node) []ValidationError {
	var out []ValidationError
	for i, e := range dsl.Edges {
		if _, ok := nodeByID[e.From]; !ok {
			out = append(out, ValidationError{
				Code:    CodeDanglingEdge,
				Message: fmt.Sprintf("edge %q from non-existent node %q", e.ID, e.From),
				Path:    fmt.Sprintf("edges[%d].from", i),
			})
		}
		if _, ok := nodeByID[e.To]; !ok {
			out = append(out, ValidationError{
				Code:    CodeDanglingEdge,
				Message: fmt.Sprintf("edge %q to non-existent node %q", e.ID, e.To),
				Path:    fmt.Sprintf("edges[%d].to", i),
			})
		}
	}
	return out
}

// 规则 4：Edge.FromPort 必须是源节点 NodeType 声明的端口。
func checkEdgeFromPorts(
	dsl workflow.WorkflowDSL,
	nodeByID map[string]*workflow.Node,
	getType func(string) (*nodetype.NodeType, bool),
) []ValidationError {
	var out []ValidationError
	for i, e := range dsl.Edges {
		src, ok := nodeByID[e.From]
		if !ok {
			continue // 已由 checkEdgeTargets 报告
		}
		nt, ok := getType(src.TypeKey)
		if !ok {
			continue // 已由 checkNodeTypesExist 报告
		}
		valid := false
		for _, p := range nt.Ports {
			if p == e.FromPort {
				valid = true
				break
			}
		}
		if !valid {
			out = append(out, ValidationError{
				Code:    CodeUnknownFromPort,
				Message: fmt.Sprintf("edge %q uses port %q not declared by NodeType %q", e.ID, e.FromPort, src.TypeKey),
				Path:    fmt.Sprintf("edges[%d].from_port", i),
			})
		}
	}
	return out
}

// 规则 3b：RefValue.NodeID / PortID 必须真实存在。
func checkRefValues(
	dsl workflow.WorkflowDSL,
	nodeByID map[string]*workflow.Node,
	getType func(string) (*nodetype.NodeType, bool),
) []ValidationError {
	var out []ValidationError
	for ni, n := range dsl.Nodes {
		for portKey, vs := range n.Inputs {
			if vs.Kind != workflow.ValueKindRef {
				continue
			}
			ref, ok := vs.Value.(workflow.RefValue)
			if !ok {
				continue
			}
			target, ok := nodeByID[ref.NodeID]
			if !ok {
				out = append(out, ValidationError{
					Code:    CodeDanglingRef,
					Message: fmt.Sprintf("node %q input %q references non-existent node %q", n.ID, portKey, ref.NodeID),
					Path:    fmt.Sprintf("nodes[%d].inputs.%s", ni, portKey),
				})
				continue
			}
			nt, ok := getType(target.TypeKey)
			if !ok {
				continue
			}
			portFound := false
			for _, p := range nt.OutputSchema {
				if p.ID == ref.PortID {
					portFound = true
					break
				}
			}
			if !portFound {
				out = append(out, ValidationError{
					Code:    CodeDanglingRef,
					Message: fmt.Sprintf("node %q input %q references unknown port %q on node %q", n.ID, portKey, ref.PortID, ref.NodeID),
					Path:    fmt.Sprintf("nodes[%d].inputs.%s", ni, portKey),
				})
			}
		}
	}
	return out
}

// 规则 5：Required=true 的输入端口必须绑了非空 ValueSource。
func checkRequiredInputs(
	dsl workflow.WorkflowDSL,
	getType func(string) (*nodetype.NodeType, bool),
) []ValidationError {
	var out []ValidationError
	for ni, n := range dsl.Nodes {
		nt, ok := getType(n.TypeKey)
		if !ok {
			continue
		}
		for _, p := range nt.InputSchema {
			if !p.Required {
				continue
			}
			vs, bound := n.Inputs[p.ID]
			if !bound || vs.Value == nil {
				out = append(out, ValidationError{
					Code:    CodeRequiredInputMissing,
					Message: fmt.Sprintf("node %q missing required input %q (%s)", n.ID, p.Name, p.ID),
					Path:    fmt.Sprintf("nodes[%d].inputs.%s", ni, p.ID),
				})
			}
		}
	}
	return out
}

// 规则 7：OnFinalFail=fallback 仅允许出现在声明了 PortDefault 端口的 NodeType 上。
func checkFallbackOnly(
	dsl workflow.WorkflowDSL,
	getType func(string) (*nodetype.NodeType, bool),
) []ValidationError {
	var out []ValidationError
	for ni, n := range dsl.Nodes {
		if n.ErrorPolicy == nil || n.ErrorPolicy.OnFinalFail != workflow.FailStrategyFallback {
			continue
		}
		nt, ok := getType(n.TypeKey)
		if !ok {
			continue
		}
		hasDefault := false
		for _, p := range nt.Ports {
			if p == workflow.PortDefault {
				hasDefault = true
				break
			}
		}
		if !hasDefault {
			out = append(out, ValidationError{
				Code:    CodeFallbackOnNonDefaultPortNode,
				Message: fmt.Sprintf("node %q uses fallback strategy but NodeType %q has no 'default' port", n.ID, n.TypeKey),
				Path:    fmt.Sprintf("nodes[%d].error_policy.on_final_fail", ni),
			})
		}
	}
	return out
}

// 规则 8：节点之间不存在控制流环。Kahn's algorithm（拓扑排序）实现。
func checkAcyclic(dsl workflow.WorkflowDSL, nodeByID map[string]*workflow.Node) []ValidationError {
	if len(dsl.Nodes) == 0 {
		return nil
	}
	indeg := make(map[string]int, len(dsl.Nodes))
	adj := make(map[string][]string, len(dsl.Nodes))
	for id := range nodeByID {
		indeg[id] = 0
	}
	for _, e := range dsl.Edges {
		if _, ok := nodeByID[e.From]; !ok {
			continue
		}
		if _, ok := nodeByID[e.To]; !ok {
			continue
		}
		adj[e.From] = append(adj[e.From], e.To)
		indeg[e.To]++
	}

	queue := make([]string, 0)
	for id, d := range indeg {
		if d == 0 {
			queue = append(queue, id)
		}
	}
	visited := 0
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		visited++
		for _, nxt := range adj[cur] {
			indeg[nxt]--
			if indeg[nxt] == 0 {
				queue = append(queue, nxt)
			}
		}
	}
	if visited != len(nodeByID) {
		return []ValidationError{{
			Code:    CodeCycle,
			Message: "DSL contains a cycle (v1 only allows loops via builtin.loop nodes)",
		}}
	}
	return nil
}
```

- [ ] **Step 4：跑测试确认全部通过**

Run: `go test ./domain/workflow/check/... -v`
Expected: PASS — 全部 12 个 test

- [ ] **Step 5：编译 + vet**

Run: `go build ./... && go vet ./...`
Expected: 成功

- [ ] **Step 6：commit**

```bash
git add domain/workflow/check/
git commit -m "feat(domain/workflow/check): add strict DSL validator for PublishVersion"
```

---

### Task 10: run 类型 + 状态机不变式

**Files:**
- Create: `domain/run/trigger.go`
- Create: `domain/run/workflow_run.go`
- Create: `domain/run/workflow_run_test.go`
- Create: `domain/run/node_run.go`
- Create: `domain/run/node_run_test.go`

WorkflowRun / NodeRun 的状态有"单调推进"约束（spec §14：终态不可回退）。这条不变式做成方法 `CanTransitionTo`，TDD 覆盖。

- [ ] **Step 1：创建 `domain/run/trigger.go`**

```go
// Package run 定义工作流运行时聚合 WorkflowRun + 子实体 NodeRun，以及 context 投影。
package run

// TriggerKind 标识 WorkflowRun 是被什么触发的。
type TriggerKind string

const (
	TriggerKindManual  TriggerKind = "manual"
	TriggerKindWebhook TriggerKind = "webhook"
	TriggerKindAPI     TriggerKind = "api"
	TriggerKindCron    TriggerKind = "cron"
)
```

- [ ] **Step 2：先写失败测试 `domain/run/workflow_run_test.go`**

```go
package run

import "testing"

func TestRunStatus_CanTransitionTo(t *testing.T) {
	cases := []struct {
		from, to RunStatus
		want     bool
	}{
		{RunStatusPending, RunStatusRunning, true},
		{RunStatusRunning, RunStatusSuccess, true},
		{RunStatusRunning, RunStatusFailed, true},
		{RunStatusRunning, RunStatusCancelled, true},
		{RunStatusPending, RunStatusFailed, true},
		{RunStatusPending, RunStatusCancelled, true},

		// 不允许从终态回退
		{RunStatusSuccess, RunStatusRunning, false},
		{RunStatusFailed, RunStatusRunning, false},
		{RunStatusCancelled, RunStatusRunning, false},
		{RunStatusSuccess, RunStatusFailed, false},

		// 同状态无谓推进
		{RunStatusRunning, RunStatusRunning, false},
		{RunStatusSuccess, RunStatusSuccess, false},

		// 不允许 Pending → Success（必须先 Running）
		{RunStatusPending, RunStatusSuccess, false},
	}
	for _, c := range cases {
		got := c.from.CanTransitionTo(c.to)
		if got != c.want {
			t.Errorf("%s → %s: got %v, want %v", c.from, c.to, got, c.want)
		}
	}
}
```

- [ ] **Step 3：跑测试确认失败**

Run: `go test ./domain/run/...`
Expected: FAIL — `undefined: RunStatus`

- [ ] **Step 4：创建 `domain/run/workflow_run.go`**

```go
package run

import (
	"encoding/json"
	"time"
)

// RunStatus 是 WorkflowRun 的状态机。
//
// 不变式（CanTransitionTo 编码）：
//   - 终态（success / failed / cancelled）不可回退到 running
//   - 同状态不可"自转"
//   - pending 只能进 running / failed / cancelled，不能直接跳 success
type RunStatus string

const (
	RunStatusPending   RunStatus = "pending"
	RunStatusRunning   RunStatus = "running"
	RunStatusSuccess   RunStatus = "success"
	RunStatusFailed    RunStatus = "failed"
	RunStatusCancelled RunStatus = "cancelled"
)

// IsTerminal 是否为终态。
func (s RunStatus) IsTerminal() bool {
	switch s {
	case RunStatusSuccess, RunStatusFailed, RunStatusCancelled:
		return true
	}
	return false
}

// CanTransitionTo 当前状态是否允许推进到 next。
func (s RunStatus) CanTransitionTo(next RunStatus) bool {
	if s == next {
		return false
	}
	if s.IsTerminal() {
		return false
	}
	switch s {
	case RunStatusPending:
		return next == RunStatusRunning || next == RunStatusFailed || next == RunStatusCancelled
	case RunStatusRunning:
		return next == RunStatusSuccess || next == RunStatusFailed || next == RunStatusCancelled
	}
	return false
}

// RunError 是 WorkflowRun 失败时的错误现场。
type RunError struct {
	NodeID    string
	NodeRunID string
	Code      string
	Message   string
	Details   json.RawMessage
}

// 常用 RunError.Code。
const (
	RunErrCodeNodeExecFailed       = "node_exec_failed"
	RunErrCodeTimeout              = "timeout"
	RunErrCodeCancelled            = "cancelled"
	RunErrCodeVersionNotPublished  = "version_not_published"
)

// WorkflowRun 是运行时聚合根。
//
// 不变式（spec §14）：
//   - VersionID 创建后不可改
//   - Status 单调（CanTransitionTo 强制）
//   - Status == RunStatusSuccess 时 EndNodeID 必非空且指向 DSL 内真实 End 节点
type WorkflowRun struct {
	ID           string
	DefinitionID string
	VersionID    string

	TriggerKind    TriggerKind
	TriggerRef     string
	TriggerPayload json.RawMessage

	Status    RunStatus
	StartedAt *time.Time
	EndedAt   *time.Time

	Vars      json.RawMessage
	EndNodeID *string
	Output    json.RawMessage
	Error     *RunError

	CreatedBy string
	CreatedAt time.Time
}
```

- [ ] **Step 5：跑测试确认通过**

Run: `go test ./domain/run/... -v -run TestRunStatus`
Expected: PASS

- [ ] **Step 6：先写失败测试 `domain/run/node_run_test.go`**

```go
package run

import "testing"

func TestNodeRunStatus_CanTransitionTo(t *testing.T) {
	cases := []struct {
		from, to NodeRunStatus
		want     bool
	}{
		{NodeRunStatusPending, NodeRunStatusRunning, true},
		{NodeRunStatusPending, NodeRunStatusSkipped, true},
		{NodeRunStatusRunning, NodeRunStatusSuccess, true},
		{NodeRunStatusRunning, NodeRunStatusFailed, true},

		// 终态不可回退
		{NodeRunStatusSuccess, NodeRunStatusRunning, false},
		{NodeRunStatusFailed, NodeRunStatusRunning, false},
		{NodeRunStatusSkipped, NodeRunStatusRunning, false},

		// 同态不可自转
		{NodeRunStatusRunning, NodeRunStatusRunning, false},

		// 不允许 Pending → Success（必须先 Running）
		{NodeRunStatusPending, NodeRunStatusSuccess, false},
	}
	for _, c := range cases {
		got := c.from.CanTransitionTo(c.to)
		if got != c.want {
			t.Errorf("%s → %s: got %v, want %v", c.from, c.to, got, c.want)
		}
	}
}
```

- [ ] **Step 7：跑测试确认失败**

Run: `go test ./domain/run/... -run TestNodeRunStatus`
Expected: FAIL — `undefined: NodeRunStatus`

- [ ] **Step 8：创建 `domain/run/node_run.go`**

```go
package run

import (
	"encoding/json"
	"time"
)

// NodeRunStatus 是 NodeRun 的状态机。Skipped 表示因 If/Switch 分支裁剪而未执行。
type NodeRunStatus string

const (
	NodeRunStatusPending NodeRunStatus = "pending"
	NodeRunStatusRunning NodeRunStatus = "running"
	NodeRunStatusSuccess NodeRunStatus = "success"
	NodeRunStatusFailed  NodeRunStatus = "failed"
	NodeRunStatusSkipped NodeRunStatus = "skipped"
)

// IsTerminal 是否为终态。
func (s NodeRunStatus) IsTerminal() bool {
	switch s {
	case NodeRunStatusSuccess, NodeRunStatusFailed, NodeRunStatusSkipped:
		return true
	}
	return false
}

// CanTransitionTo 当前状态是否允许推进到 next。
func (s NodeRunStatus) CanTransitionTo(next NodeRunStatus) bool {
	if s == next {
		return false
	}
	if s.IsTerminal() {
		return false
	}
	switch s {
	case NodeRunStatusPending:
		// 允许 pending → running，或被裁剪直接 → skipped
		return next == NodeRunStatusRunning || next == NodeRunStatusSkipped
	case NodeRunStatusRunning:
		return next == NodeRunStatusSuccess || next == NodeRunStatusFailed
	}
	return false
}

// NodeError 是 NodeRun 失败时的错误现场。fallback 生效时仍保留最后一次 NodeError，便于审计。
type NodeError struct {
	Code    string
	Message string
	Details json.RawMessage
}

// 常用 NodeError.Code。
const (
	NodeErrCodeExecFailed        = "exec_failed"
	NodeErrCodeTimeout           = "timeout"
	NodeErrCodeCancelled         = "cancelled"
	NodeErrCodeValidationFailed  = "validation_failed"
)

// ExternalRef 记录节点执行过程中的外部调用 ID（LLM trace_id / HTTP request_id / MCP tool_call_id）。
type ExternalRef struct {
	Kind string // "llm_call" | "http_request" | "mcp_tool"
	Ref  string
}

// NodeRun 是 WorkflowRun 聚合内的子实体。
//
// 不变式（spec §10、§14）：
//   - (RunID, NodeID, Attempt) 唯一；Attempt 从 1 起递增
//   - FallbackApplied=true 时 Status 必为 NodeRunStatusFailed、FiredPort 必为 PortDefault、Output 即 fallback 值
//   - ResolvedInputs / ResolvedConfig 不得包含任何 Credential 明文（结构性保证，见 spec §11.4）
type NodeRun struct {
	ID          string
	RunID       string
	NodeID      string
	NodeTypeKey string
	Attempt     int

	Status    NodeRunStatus
	StartedAt *time.Time
	EndedAt   *time.Time

	ResolvedConfig json.RawMessage
	ResolvedInputs json.RawMessage
	Output         json.RawMessage
	FiredPort      string

	FallbackApplied bool
	Error           *NodeError

	ExternalRefs []ExternalRef
}
```

- [ ] **Step 9：跑测试确认全部通过**

Run: `go test ./domain/run/... -v`
Expected: PASS

- [ ] **Step 10：编译 + vet**

Run: `go build ./... && go vet ./...`
Expected: 成功

- [ ] **Step 11：commit**

```bash
git add domain/run/trigger.go domain/run/workflow_run.go domain/run/workflow_run_test.go domain/run/node_run.go domain/run/node_run_test.go
git commit -m "feat(domain/run): add WorkflowRun / NodeRun aggregates with status state machines"
```

---

### Task 11: run context 投影

**Files:**
- Create: `domain/run/context.go`
- Create: `domain/run/context_test.go`

`BuildContext` 把 `WorkflowRun.TriggerPayload + Vars` 与 NodeRun 的最新成功 attempt 投影成 `map[string]any`，前缀 `trigger.* / vars.* / nodes.<id>.*`。

- [ ] **Step 1：先写失败测试 `domain/run/context_test.go`**

```go
package run

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestBuildContext_TriggerAndVars(t *testing.T) {
	wr := &WorkflowRun{
		TriggerPayload: json.RawMessage(`{"text":"hello","count":3}`),
		Vars:           json.RawMessage(`{"theme":"dark"}`),
	}
	ctx, err := BuildContext(wr, nil)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	want := map[string]any{
		"trigger.text":  "hello",
		"trigger.count": float64(3), // encoding/json 默认数字解为 float64
		"vars.theme":    "dark",
	}
	if !reflect.DeepEqual(ctx, want) {
		t.Errorf("ctx = %#v, want %#v", ctx, want)
	}
}

func TestBuildContext_NodesPicksLatestSuccess(t *testing.T) {
	wr := &WorkflowRun{TriggerPayload: json.RawMessage(`{}`)}
	nrs := []*NodeRun{
		{NodeID: "llm1", Attempt: 1, Status: NodeRunStatusFailed,
			Output: json.RawMessage(`{"text":"v1"}`)},
		{NodeID: "llm1", Attempt: 2, Status: NodeRunStatusSuccess,
			Output: json.RawMessage(`{"text":"v2"}`)},
		// 同 NodeID 的更早 attempt 即使 success 也应被更高 Attempt 覆盖
		{NodeID: "llm2", Attempt: 1, Status: NodeRunStatusSuccess,
			Output: json.RawMessage(`{"text":"a"}`)},
		{NodeID: "llm2", Attempt: 2, Status: NodeRunStatusFailed,
			Output: json.RawMessage(`{"text":"b"}`)},
	}
	ctx, err := BuildContext(wr, nrs)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if ctx["nodes.llm1.text"] != "v2" {
		t.Errorf("llm1.text = %v, want v2", ctx["nodes.llm1.text"])
	}
	// llm2 最新 attempt 是 failed，不计入 context
	if _, exists := ctx["nodes.llm2.text"]; exists {
		t.Errorf("llm2 latest attempt is failed, should not appear in context, got %v", ctx["nodes.llm2.text"])
	}
}

func TestBuildContext_FallbackCounted(t *testing.T) {
	wr := &WorkflowRun{TriggerPayload: json.RawMessage(`{}`)}
	nrs := []*NodeRun{
		// fallback 生效时 Status=Failed 但 Output 是 fallback 值，仍应进 context
		{NodeID: "n1", Attempt: 1, Status: NodeRunStatusFailed, FallbackApplied: true,
			Output: json.RawMessage(`{"text":"fb"}`), FiredPort: "default"},
	}
	ctx, err := BuildContext(wr, nrs)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if ctx["nodes.n1.text"] != "fb" {
		t.Errorf("fallback output should appear, got %v", ctx["nodes.n1.text"])
	}
}

func TestBuildContext_BadJSON(t *testing.T) {
	wr := &WorkflowRun{TriggerPayload: json.RawMessage(`not json`)}
	if _, err := BuildContext(wr, nil); err == nil {
		t.Error("expected error on bad JSON")
	}
}
```

- [ ] **Step 2：跑测试确认失败**

Run: `go test ./domain/run/... -run TestBuildContext`
Expected: FAIL — `undefined: BuildContext`

- [ ] **Step 3：创建 `domain/run/context.go`**

```go
package run

import (
	"encoding/json"
	"fmt"
)

// BuildContext 将 WorkflowRun 的触发参数 / Vars / NodeRun Output 投影成扁平变量表。
//
// 输出 key 的前缀规则（与 spec §6.5 / §8.4 对齐）：
//
//   trigger.<top>             ← WorkflowRun.TriggerPayload 顶层字段（必为 JSON object）
//   vars.<top>                ← WorkflowRun.Vars 顶层字段；nil/空时跳过
//   nodes.<nodeID>.<top>      ← 每个 NodeID 取 Attempt 最大且 (Status==Success 或 FallbackApplied)
//                                的 NodeRun.Output 顶层字段
//
// 为何"取最大 Attempt 而不是最后一次 success"：FallbackApplied=true 时 Status=Failed，但 Output
// 是兜底值，下游期望读到这个兜底；规则统一为"按 Attempt 排序、取头部、判断是否可计入"。
//
// TriggerPayload / Vars / Output 都必须是 JSON object，否则返回错误（不支持顶层数组 / 标量）。
// 这是引擎契约，避免 {{trigger}} 这种"整体引用"的歧义。
func BuildContext(run *WorkflowRun, nodeRuns []*NodeRun) (map[string]any, error) {
	out := make(map[string]any)

	if err := flattenInto(out, "trigger", run.TriggerPayload); err != nil {
		return nil, fmt.Errorf("trigger payload: %w", err)
	}
	if len(run.Vars) > 0 {
		if err := flattenInto(out, "vars", run.Vars); err != nil {
			return nil, fmt.Errorf("vars: %w", err)
		}
	}

	// 选择每个 NodeID 的"最新可用" attempt
	type pick struct {
		attempt int
		nr      *NodeRun
	}
	latest := map[string]pick{}
	for _, nr := range nodeRuns {
		cur, ok := latest[nr.NodeID]
		if !ok || nr.Attempt > cur.attempt {
			latest[nr.NodeID] = pick{attempt: nr.Attempt, nr: nr}
		}
	}
	for nodeID, p := range latest {
		nr := p.nr
		usable := nr.Status == NodeRunStatusSuccess || nr.FallbackApplied
		if !usable || len(nr.Output) == 0 {
			continue
		}
		if err := flattenInto(out, "nodes."+nodeID, nr.Output); err != nil {
			return nil, fmt.Errorf("node %s output: %w", nodeID, err)
		}
	}
	return out, nil
}

// flattenInto 把 raw（必须是 JSON object）的顶层每个 key 写入 dest 的 "<prefix>.<key>"。
func flattenInto(dest map[string]any, prefix string, raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return fmt.Errorf("expected JSON object: %w", err)
	}
	for k, v := range obj {
		dest[prefix+"."+k] = v
	}
	return nil
}
```

- [ ] **Step 4：跑测试确认通过**

Run: `go test ./domain/run/... -v`
Expected: PASS — 全部 test

- [ ] **Step 5：编译 + vet**

Run: `go build ./... && go vet ./...`
Expected: 成功

- [ ] **Step 6：commit**

```bash
git add domain/run/context.go domain/run/context_test.go
git commit -m "feat(domain/run): add BuildContext projection for runtime variable table"
```

---

### Task 12: run 仓储接口

**Files:**
- Create: `domain/run/repository.go`

接口为主，不写测试。

- [ ] **Step 1：创建 `domain/run/repository.go`**

```go
package run

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

var (
	ErrRunNotFound     = errors.New("run: workflow run not found")
	ErrNodeRunNotFound = errors.New("run: node run not found")

	// ErrIllegalStatusTransition 是仓储在 UpdateStatus / UpdateNodeRunStatus 时
	// 通过 RunStatus.CanTransitionTo / NodeRunStatus.CanTransitionTo 判定失败时返回的哨兵 error。
	ErrIllegalStatusTransition = errors.New("run: illegal status transition")
)

// RunFilter 是 List 的过滤参数。
type RunFilter struct {
	DefinitionID string
	VersionID    string
	Status       RunStatus
	TriggerKind  TriggerKind
	StartedFrom  *time.Time
	StartedTo    *time.Time
	Limit        int
	Offset       int
}

// RunUpdateOpt / NodeRunUpdateOpt 是函数选项，让 UpdateStatus 可一次更新多个相关字段。
type RunUpdateOpt func(*RunUpdate)

type RunUpdate struct {
	StartedAt *time.Time
	EndedAt   *time.Time
}

func WithRunStartedAt(t time.Time) RunUpdateOpt {
	return func(u *RunUpdate) { u.StartedAt = &t }
}
func WithRunEndedAt(t time.Time) RunUpdateOpt {
	return func(u *RunUpdate) { u.EndedAt = &t }
}

type NodeRunUpdateOpt func(*NodeRunUpdate)

type NodeRunUpdate struct {
	StartedAt       *time.Time
	EndedAt         *time.Time
	Error           *NodeError
	FallbackApplied *bool
	ExternalRefs    []ExternalRef
}

func WithNodeRunStartedAt(t time.Time) NodeRunUpdateOpt {
	return func(u *NodeRunUpdate) { u.StartedAt = &t }
}
func WithNodeRunEndedAt(t time.Time) NodeRunUpdateOpt {
	return func(u *NodeRunUpdate) { u.EndedAt = &t }
}
func WithNodeRunError(e NodeError) NodeRunUpdateOpt {
	return func(u *NodeRunUpdate) { u.Error = &e }
}
func WithNodeRunFallbackApplied(b bool) NodeRunUpdateOpt {
	return func(u *NodeRunUpdate) { u.FallbackApplied = &b }
}
func WithNodeRunExternalRefs(refs []ExternalRef) NodeRunUpdateOpt {
	return func(u *NodeRunUpdate) { u.ExternalRefs = refs }
}

// WorkflowRunRepository 是 WorkflowRun 聚合（含 NodeRun 子实体）的存储契约。
//
// 关键约束：
//   - 所有写入 NodeRun 的方法必须经由本接口（NodeRun 非独立聚合根）
//   - UpdateStatus / UpdateNodeRunStatus 在写库前必须用 CanTransitionTo 校验，
//     不合法时返回 ErrIllegalStatusTransition
type WorkflowRunRepository interface {
	// WorkflowRun
	Create(ctx context.Context, run *WorkflowRun) error
	Get(ctx context.Context, id string) (*WorkflowRun, error)
	List(ctx context.Context, filter RunFilter) ([]*WorkflowRun, error)
	UpdateStatus(ctx context.Context, id string, status RunStatus, opts ...RunUpdateOpt) error
	SaveEndResult(ctx context.Context, id, endNodeID string, output json.RawMessage) error
	SaveVars(ctx context.Context, id string, vars json.RawMessage) error
	SaveError(ctx context.Context, id string, e RunError) error

	// NodeRun（聚合内子实体）
	AppendNodeRun(ctx context.Context, runID string, nr *NodeRun) error
	UpdateNodeRunStatus(ctx context.Context, runID, nodeRunID string, status NodeRunStatus, opts ...NodeRunUpdateOpt) error
	SaveNodeRunOutput(ctx context.Context, runID, nodeRunID string, output json.RawMessage, firedPort string) error
	GetNodeRun(ctx context.Context, runID, nodeRunID string) (*NodeRun, error)
	ListNodeRuns(ctx context.Context, runID string) ([]*NodeRun, error)
	GetLatestNodeRun(ctx context.Context, runID, nodeID string) (*NodeRun, error)
}
```

- [ ] **Step 2：编译 + vet**

Run: `go build ./... && go vet ./...`
Expected: 成功

- [ ] **Step 3：commit**

```bash
git add domain/run/repository.go
git commit -m "feat(domain/run): add WorkflowRunRepository contract"
```

---

### Task 13: cron 包

**Files:**
- Create: `domain/cron/cronjob.go`
- Create: `domain/cron/repository.go`

类型 + 接口，不写测试。

- [ ] **Step 1：创建 `domain/cron/cronjob.go`**

```go
// Package cron 定义工作流定时触发器聚合 CronJob。
//
// 设计取舍：
//   - 不内嵌 MaxConcurrency / StartAt / EndAt / Catchup，等真有需求再扩
//   - CronJob 绑 DefinitionID 而非 VersionID，fire 时再读 Definition.PublishedVersionID
package cron

import (
	"encoding/json"
	"time"
)

// CronJob 是定时触发器聚合根。
//
// 不变式：
//   - Expression 必须是合法的标准 5 段 cron（由仓储 / application 层校验，本聚合不强制）
//   - Timezone 必须是 IANA 名（如 "Asia/Shanghai"）
//   - DefinitionID 必须存在；fire 时若 Definition.PublishedVersionID 为 nil 则跳过并记 error log
type CronJob struct {
	ID           string
	DefinitionID string
	Name         string
	Description  string

	Expression string
	Timezone   string
	Payload    json.RawMessage

	Enabled bool

	NextFireAt *time.Time
	LastFireAt *time.Time
	LastRunID  *string

	CreatedBy string
	CreatedAt time.Time
	UpdatedAt time.Time
}
```

- [ ] **Step 2：创建 `domain/cron/repository.go`**

```go
package cron

import (
	"context"
	"errors"
	"time"
)

var ErrCronJobNotFound = errors.New("cron: cron job not found")

// CronJobFilter 是 List 的过滤参数。
type CronJobFilter struct {
	DefinitionID string
	EnabledOnly  bool
	Limit        int
	Offset       int
}

// CronJobRepository 是 CronJob 的存储契约。
//
// ClaimDue 是调度器 hot path：
//   - 实现应用 SELECT ... WHERE enabled AND next_fire_at <= now() FOR UPDATE SKIP LOCKED LIMIT N
//   - 返回的 CronJob 已被本调度器实例"占有"（NextFireAt 应在同事务推进，避免重复 fire）
type CronJobRepository interface {
	Create(ctx context.Context, j *CronJob) error
	Get(ctx context.Context, id string) (*CronJob, error)
	List(ctx context.Context, filter CronJobFilter) ([]*CronJob, error)
	Update(ctx context.Context, j *CronJob) error
	Delete(ctx context.Context, id string) error

	// ClaimDue 一次性认领最多 limit 条到期任务（行锁 SKIP LOCKED）。
	ClaimDue(ctx context.Context, now time.Time, limit int) ([]*CronJob, error)

	// MarkFired 在 fire 完成后更新调度元数据。
	MarkFired(ctx context.Context, id string, lastFireAt, nextFireAt time.Time, lastRunID string) error
}
```

- [ ] **Step 3：编译 + vet**

Run: `go build ./... && go vet ./...`
Expected: 成功

- [ ] **Step 4：commit**

```bash
git add domain/cron/
git commit -m "feat(domain/cron): add CronJob aggregate and repository contract"
```

---

### Task 14: executor 接口与服务结构

**Files:**
- Create: `domain/executor/exec_input.go`
- Create: `domain/executor/executor.go`

接口 + 数据结构，不写测试。

- [ ] **Step 1：创建 `domain/executor/exec_input.go`**

```go
// Package executor 定义节点执行器的统一接口。
//
// 设计（六边形架构 / Ports & Adapters）：
//
//   - 内置节点和插件节点对调用方完全同构，引擎只认 NodeExecutor
//   - 所有 NodeExecutor 实现 → domain/executor/builtin/
//     执行器在做的事（读 Config/Inputs → 解凭证 → 装请求 → 解响应 → 选端口）都是工作流语义，
//     是领域逻辑；executor 内只通过 ExecServices 暴露的 port 接口对外通信
//   - 各 port 的具体适配器 → infrastructure/<protocol>/
//       infrastructure/http/      HTTPClient 实现（net/http）
//       infrastructure/llm/       LLMClient 实现（OpenAI / Anthropic / …）
//       infrastructure/mcp/       MCPClient 实现（stdio / http / sse transport）
//       infrastructure/sandbox/   builtin.code 的运行时（goja / yaegi / wasmtime）
//   - Registry 装配函数：组合 domain executor factory + infra 提供的 port 实现，由 main.go 调用
//
// 具体 Executor 与 port 适配器实现由后续 executor spec 落地。
package executor

import (
	"context"
	"encoding/json"

	"github.com/shinya/shineflow/domain/credential"
	"github.com/shinya/shineflow/domain/nodetype"
	"github.com/shinya/shineflow/domain/run"
)

// RunInfo 是 Executor 在执行时可读的 Run 元信息（只读快照）。
type RunInfo struct {
	RunID        string
	NodeRunID    string
	Attempt      int
	DefinitionID string
	VersionID    string
	TriggerKind  run.TriggerKind
	TriggerRef   string
}

// Logger 是 Executor 可用的极简日志接口；具体实现由 infra 注入。
type Logger interface {
	Debugf(format string, args ...any)
	Infof(format string, args ...any)
	Warnf(format string, args ...any)
	Errorf(format string, args ...any)
}

// HTTPClient 是 Executor 可用的极简 HTTP 客户端接口（不绑 net/http），方便测试 mock。
type HTTPClient interface {
	Do(ctx context.Context, req HTTPRequest) (HTTPResponse, error)
}

type HTTPRequest struct {
	Method  string
	URL     string
	Headers map[string]string
	Body    []byte
}

type HTTPResponse struct {
	StatusCode int
	Headers    map[string]string
	Body       []byte
}

// ExecServices 是 Executor 可访问的能力集合。新增 LLM client / MCP client pool 时往这里加字段。
//
// 安全约束：Credentials 是唯一获取明文凭证的入口（spec §11.4）。
type ExecServices struct {
	Credentials credential.CredentialResolver
	Logger      Logger
	HTTPClient  HTTPClient
}

// ExecInput 是引擎传给 NodeExecutor 的入参快照。
//
//   - Config / Inputs 都已完成模板展开和 ValueSource 求值
//   - 任何字段都不应包含 Credential 明文；秘密只能通过 Services.Credentials.Resolve 拿
type ExecInput struct {
	NodeType *nodetype.NodeType
	Config   json.RawMessage
	Inputs   map[string]any
	Run      RunInfo
	Services ExecServices
}

// ExecOutput 是 NodeExecutor 的产出。
//
//   - Outputs key 应对齐 NodeType.OutputSchema 的 PortSpec.Name
//   - FiredPort 默认 "default"；If 用 "true"/"false"；失败用 "error"
//   - ExternalRefs 用于审计追踪（LLM trace_id / HTTP request_id / MCP tool_call_id）
type ExecOutput struct {
	Outputs      map[string]any
	FiredPort    string
	ExternalRefs []run.ExternalRef
}
```

- [ ] **Step 2：创建 `domain/executor/executor.go`**

```go
package executor

import "context"

// NodeExecutor 是节点执行器的统一接口。
//
// 实现要求：
//   - 不应在内部修改 in.Inputs / in.Config（已是引擎快照）
//   - 必须在合理时间内响应 ctx.Done()（超时 / 取消由引擎用 ctx 控制）
//   - 业务失败应返回非 nil error；error 由引擎按 ErrorPolicy 处理（重试 / fallback / fail_run）
//   - 任何明文凭证只可在本方法局部使用，不得写入 ExecOutput.Outputs / ExternalRefs
type NodeExecutor interface {
	Execute(ctx context.Context, in ExecInput) (ExecOutput, error)
}
```

- [ ] **Step 3：编译 + vet**

Run: `go build ./... && go vet ./...`
Expected: 成功

- [ ] **Step 4：commit**

```bash
git add domain/executor/exec_input.go domain/executor/executor.go
git commit -m "feat(domain/executor): add NodeExecutor interface and ExecInput/Output/Services contracts"
```

---

### Task 15: executor Registry + 前缀匹配

**Files:**
- Create: `domain/executor/factory.go`
- Create: `domain/executor/registry.go`
- Create: `domain/executor/registry_test.go`

模式匹配规则：精确匹配 > 前缀匹配（最长字面前缀优先）；前缀模式以 `*` 结尾按"."切分逐段匹配，`*` 匹配单段。TDD 必备。

- [ ] **Step 1：创建 `domain/executor/factory.go`**

```go
package executor

import "github.com/shinya/shineflow/domain/nodetype"

// ExecutorFactory 用 NodeType 构造 NodeExecutor。
// 同一 keyPattern 下所有 NodeType 共享一个 factory；factory 内部可按 NodeType 字段做差异化。
type ExecutorFactory func(nt *nodetype.NodeType) NodeExecutor

// ExecutorRegistry 是 NodeType.Key → NodeExecutor 的映射注册表。
//
// keyPattern 形态：
//   - 精确：              "builtin.llm"
//   - 前缀通配（按段）：  "plugin.http.*" / "plugin.mcp.*.*"
//
// 匹配优先级：
//   1) 精确 key 命中
//   2) 前缀通配中"字面前缀最长"的 pattern（即 pattern 中 '*' 之前的字符串最长）
//   3) 段数必须匹配："plugin.mcp.*.*" 不会匹配 "plugin.mcp.svr_1"（段数差）
type ExecutorRegistry interface {
	Register(keyPattern string, factory ExecutorFactory)
	Build(nt *nodetype.NodeType) (NodeExecutor, error)
}
```

- [ ] **Step 2：先写失败测试 `domain/executor/registry_test.go`**

```go
package executor

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/shinya/shineflow/domain/nodetype"
)

// stubExec 是测试用的 NodeExecutor 实现，仅记录自己叫什么。
type stubExec struct{ tag string }

func (s *stubExec) Execute(_ context.Context, _ ExecInput) (ExecOutput, error) {
	return ExecOutput{Outputs: map[string]any{"tag": s.tag}}, nil
}

func newStub(tag string) ExecutorFactory {
	return func(_ *nodetype.NodeType) NodeExecutor { return &stubExec{tag: tag} }
}

func tagOf(t *testing.T, ex NodeExecutor) string {
	t.Helper()
	out, err := ex.Execute(context.Background(), ExecInput{})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	return out.Outputs["tag"].(string)
}

func TestRegistry_ExactWinsOverPrefix(t *testing.T) {
	r := NewRegistry()
	r.Register("builtin.llm", newStub("exact"))
	r.Register("builtin.*", newStub("wildcard"))

	ex, err := r.Build(&nodetype.NodeType{Key: "builtin.llm"})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if got := tagOf(t, ex); got != "exact" {
		t.Errorf("got %q, want exact", got)
	}
}

func TestRegistry_LongestPrefixWins(t *testing.T) {
	r := NewRegistry()
	r.Register("plugin.*", newStub("short"))
	r.Register("plugin.http.*", newStub("long"))

	ex, err := r.Build(&nodetype.NodeType{Key: "plugin.http.hp_001"})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if got := tagOf(t, ex); got != "long" {
		t.Errorf("got %q, want long", got)
	}
}

func TestRegistry_TwoSegmentWildcard(t *testing.T) {
	r := NewRegistry()
	r.Register("plugin.mcp.*.*", newStub("mcp"))

	ex, err := r.Build(&nodetype.NodeType{Key: "plugin.mcp.svr_1.read_file"})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if got := tagOf(t, ex); got != "mcp" {
		t.Errorf("got %q, want mcp", got)
	}
}

func TestRegistry_SegmentCountMismatch(t *testing.T) {
	r := NewRegistry()
	r.Register("plugin.mcp.*.*", newStub("mcp"))

	// 段数不够，不应匹配
	if _, err := r.Build(&nodetype.NodeType{Key: "plugin.mcp.svr_1"}); !errors.Is(err, ErrNoExecutor) {
		t.Errorf("expected ErrNoExecutor, got %v", err)
	}
	// 段数过多，不应匹配
	if _, err := r.Build(&nodetype.NodeType{Key: "plugin.mcp.svr_1.tool.extra"}); !errors.Is(err, ErrNoExecutor) {
		t.Errorf("expected ErrNoExecutor, got %v", err)
	}
}

func TestRegistry_NoMatch(t *testing.T) {
	r := NewRegistry()
	r.Register("builtin.llm", newStub("exact"))

	if _, err := r.Build(&nodetype.NodeType{Key: "totally.unknown"}); !errors.Is(err, ErrNoExecutor) {
		t.Errorf("expected ErrNoExecutor, got %v", err)
	}
}

func TestRegistry_ExactDuplicateOverwrites(t *testing.T) {
	// 重复注册同一精确 key，后注册胜（明确该行为，避免初始化顺序歧义）
	r := NewRegistry()
	r.Register("builtin.llm", newStub("v1"))
	r.Register("builtin.llm", newStub("v2"))
	ex, err := r.Build(&nodetype.NodeType{Key: "builtin.llm"})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if got := tagOf(t, ex); got != "v2" {
		t.Errorf("got %q, want v2", got)
	}
}

func TestRegistry_PassesNodeTypeToFactory(t *testing.T) {
	// factory 应能拿到完整 NodeType（用 reflect.DeepEqual 而不是仅看 Key）
	var captured *nodetype.NodeType
	r := NewRegistry()
	r.Register("builtin.llm", func(nt *nodetype.NodeType) NodeExecutor {
		captured = nt
		return &stubExec{tag: "x"}
	})
	nt := &nodetype.NodeType{Key: "builtin.llm", Name: "LLM", Category: nodetype.CategoryAI}
	if _, err := r.Build(nt); err != nil {
		t.Fatalf("build: %v", err)
	}
	if !reflect.DeepEqual(captured, nt) {
		t.Errorf("factory received different NodeType: %+v vs %+v", captured, nt)
	}
}
```

- [ ] **Step 3：跑测试确认全部失败**

Run: `go test ./domain/executor/...`
Expected: FAIL — `undefined: NewRegistry / ErrNoExecutor`

- [ ] **Step 4：创建 `domain/executor/registry.go`**

```go
package executor

import (
	"errors"
	"fmt"
	"strings"

	"github.com/shinya/shineflow/domain/nodetype"
)

// ErrNoExecutor Build 找不到任何匹配的 ExecutorFactory。
var ErrNoExecutor = errors.New("executor: no matching factory")

// NewRegistry 构造一个内存版 ExecutorRegistry。
// 注册和构造都不并发安全；引擎初始化时单线程注册即可，后续 Build 也通常是同一 goroutine。
// 如果未来需要在 Build 高并发的同时支持热更新，再加 sync.RWMutex。
func NewRegistry() ExecutorRegistry { return &registry{} }

type registry struct {
	exact    map[string]ExecutorFactory
	wildcard []wildcardEntry
}

type wildcardEntry struct {
	pattern   string
	segments  []string // pattern split on "."；其中 "*" 是段通配
	factory   ExecutorFactory
	prefixLen int // pattern 中 '*' 之前的字面前缀长度（含末尾的"."）
}

func (r *registry) Register(keyPattern string, factory ExecutorFactory) {
	if !strings.Contains(keyPattern, "*") {
		if r.exact == nil {
			r.exact = map[string]ExecutorFactory{}
		}
		r.exact[keyPattern] = factory
		return
	}
	starIdx := strings.Index(keyPattern, "*")
	r.wildcard = append(r.wildcard, wildcardEntry{
		pattern:   keyPattern,
		segments:  strings.Split(keyPattern, "."),
		factory:   factory,
		prefixLen: starIdx,
	})
}

func (r *registry) Build(nt *nodetype.NodeType) (NodeExecutor, error) {
	if f, ok := r.exact[nt.Key]; ok {
		return f(nt), nil
	}
	keySegs := strings.Split(nt.Key, ".")
	var best *wildcardEntry
	for i := range r.wildcard {
		e := &r.wildcard[i]
		if !segmentMatch(e.segments, keySegs) {
			continue
		}
		if best == nil || e.prefixLen > best.prefixLen {
			best = e
		}
	}
	if best == nil {
		return nil, fmt.Errorf("%w: key %q", ErrNoExecutor, nt.Key)
	}
	return best.factory(nt), nil
}

// segmentMatch 段对段比较 pattern 与 key；"*" 匹配任意单段。段数必须相等。
func segmentMatch(pattern, key []string) bool {
	if len(pattern) != len(key) {
		return false
	}
	for i, p := range pattern {
		if p == "*" {
			continue
		}
		if p != key[i] {
			return false
		}
	}
	return true
}
```

- [ ] **Step 5：跑测试确认全部通过**

Run: `go test ./domain/executor/... -v`
Expected: PASS — 全部 7 个 test

- [ ] **Step 6：编译 + vet**

Run: `go build ./... && go vet ./...`
Expected: 成功

- [ ] **Step 7：commit**

```bash
git add domain/executor/factory.go domain/executor/registry.go domain/executor/registry_test.go
git commit -m "feat(domain/executor): add ExecutorRegistry with exact/prefix matching"
```

---

### Task 16: 收尾 —— doc.go、整体 build / vet / test 验收

**Files:**
- Modify: `domain/doc.go`（追加新增子包索引）

- [ ] **Step 1：更新 `domain/doc.go`**

把 `domain/doc.go` 内容改成：

```go
// Package domain 是领域层。
//
// 职责：核心业务规则 —— 实体、值对象、聚合、领域服务、仓储接口。
//
// 子包索引：
//
//   workflow         工作流定义聚合（Definition / Version / DSL / Node / Edge / 端口 / 值源 / 错误策略）
//   workflow/check   WorkflowDSL 严格校验（PublishVersion 时调用，独立子包以避免循环依赖）
//   nodetype         NodeType 统一目录与 Registry 接口；含 HttpPlugin / McpTool 投影函数
//   run              工作流运行时聚合（WorkflowRun / NodeRun + 状态机 + context 投影）
//   cron             定时触发器聚合 CronJob
//   plugin           HTTP 插件 / MCP Server / MCP Tool 三类外部能力
//   credential       秘密存储聚合 + CredentialResolver 接口
//   executor         节点执行器接口与 Registry（精确 + 前缀匹配）+ port 接口（HTTPClient 等）
//   executor/builtin 所有 NodeExecutor 实现的预留落点（六边形架构：执行器编排 = 领域逻辑），
//                    后续 executor spec 落 builtin + 插件 executor 全集
//
// 不在 domain：各 port 的具体适配器（infrastructure/http、infrastructure/llm、
// infrastructure/mcp、infrastructure/sandbox）和 Registry 装配函数。
//
// 禁止：依赖任何外部框架（hertz / gorm / sonic 等）。
// 仓储实现位于 infrastructure 层。
package domain
```

- [ ] **Step 2：执行整体 build / vet**

Run: `go build ./... && go vet ./...`
Expected: 无输出（成功）

- [ ] **Step 3：执行整体 test**

Run: `go test ./...`
Expected: PASS — `domain/nodetype`、`domain/workflow/check`、`domain/run`、`domain/executor` 全部 ok；其他无测试包显示 `[no test files]`。

- [ ] **Step 4：commit**

```bash
git add domain/doc.go
git commit -m "docs(domain): index new subpackages in doc.go"
```

---

## 验收清单（与 spec §17 对齐）

- [ ] `domain/workflow/`、`domain/workflow/check/`、`domain/nodetype/`、`domain/run/`、`domain/cron/`、`domain/plugin/`、`domain/credential/`、`domain/executor/` 全部按本计划落地
- [ ] `go build ./...` 通过
- [ ] `go vet ./...` 通过
- [ ] `go test ./...` 全部 PASS
- [ ] domain 层不引用 hertz / gorm / sonic 任何包
- [ ] PublishVersion 严格校验 8 条规则全部覆盖单元测试
- [ ] WorkflowRun / NodeRun 状态机不变式覆盖单元测试
- [ ] HttpPlugin / McpTool → NodeType 投影覆盖单元测试
- [ ] ExecutorRegistry 精确 + 前缀 + 最长前缀 + 段数匹配规则覆盖单元测试
- [ ] BuildContext 三前缀 + 最新可用 attempt 选择规则覆盖单元测试

---

## 跨 spec 边界（明确不做）

- application 层用例（`RunService.Start` / 保存并发布组合事务等）—— 由后续 application spec 处理
- infrastructure 层 GORM 仓储实现、数据库迁移脚本 —— 由后续 infra spec 处理
- HTTP API 路由（`POST /api/v1/workflows/.../runs` 等）—— 由后续 facade spec 处理
- 内置 / 插件 Executor 实现 —— 由后续 executor spec 处理（六边形架构）：
  - **所有 NodeExecutor**（builtin 全集 + 插件 executor）→ `domain/executor/builtin/`
  - 各 port 适配器 → `infrastructure/http/`、`infrastructure/llm/`、`infrastructure/mcp/`、`infrastructure/sandbox/`
  - Registry 装配函数（组合 domain factory + infra port impl）由 main.go 调用
- spec §18 列出的全部开放问题（NodeType.Version 启用、loop 语义、code 沙箱、模板引擎、MCP 客户端、API Token、并行执行）
