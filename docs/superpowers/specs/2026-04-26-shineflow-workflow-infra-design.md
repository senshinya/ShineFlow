# ShineFlow Workflow Persistence 基础设施设计

- 日期：2026-04-26
- 状态：已定稿，待实现
- 依赖：`2026-04-22-shineflow-workflow-domain-design.md`（领域模型）、`2026-04-22-shineflow-init-design.md`（骨架）

## 1. 目标

> PostgreSQL ≥ 12（partial unique index 与 `FOR UPDATE SKIP LOCKED` 老版本就有；UUID 由 Go 端 `github.com/google/uuid` 的 `uuid.NewV7()` 生成，不依赖 PG 函数）。

把 domain 层定义的 5 个 aggregate 的仓储接口落地为 PostgreSQL + GORM 实现，包括：

- 9 张表的 DDL（schema + 索引 + 约束）
- `WorkflowDSL` JSONB 列的自定义序列化
- repo 实现的统一约定（事务、读写路由、错误映射、乐观并发）
- testcontainers 驱动的集成测试方案
- 凭据 AES-GCM 加解密与 `CredentialResolver` 实现

本 spec 只交付 **persistence 层**（仓储实现 + DDL）。`NodeTypeRegistry` / `ExecutorRegistry` 缓存实现、port adapter（HTTP/LLM/MCP/Sandbox）、内置 executor、application 用例、HTTP 入口各自留独立 spec。

## 2. 范围

### In Scope

- `WorkflowRepository` / `RunRepository` / `CronJobRepository` / `HttpPluginRepository` / `McpServerRepository` / `McpToolRepository` / `CredentialRepository` 全部 GORM 实现
- 9 张表的最终态 DDL（`infrastructure/storage/schema/schema.sql`）
- `CredentialResolver` 实现（含 AES-GCM 加解密）
- 软删（`deleted_at TIMESTAMPTZ`）方案与影响
- `WorkflowDSL` 自定义 marshal / unmarshal
- 测试 harness：testcontainers + 事务回滚隔离
- domain 层小幅破例：`domain/workflow/value.go` 引入 `infrastructure/util` 用于 JSON 序列化

### Out of Scope

- 数据库迁移工具选型（开发期手动 `psql -f schema.sql`，上 prod 时再补）
- `NodeTypeRegistry` / `ExecutorRegistry` 实现及缓存策略
- port adapter（HTTP / LLM / MCP / Sandbox 客户端）
- `executor/builtin/` 下的节点执行器实现
- application 层用例（含 `SaveAndPublish` 联合事务编排）
- HTTP / Webhook / Cron daemon 入口适配
- 多租户 / 行级权限 / 审计日志
- 软删行的 GC（清理 N 天前 `deleted_at IS NOT NULL`）
- Read replica 实际接入（spec 只预留 `dbresolver.Write` 路由钩子）

## 3. 架构决策总览

| #   | 决策                                                                                  | 理由摘要                                                            |
| --- | ------------------------------------------------------------------------------------- | ------------------------------------------------------------------- |
| 1   | 仓储实现按 aggregate 分子包，落 `infrastructure/storage/<aggregate>/`                 | 镜像 `domain/<aggregate>/`，对外只暴露构造函数 + 实现的接口         |
| 2   | GORM model 与 domain entity 通过 `mapper.go` 转换，不让 GORM tag 污染 domain 类型     | domain 保持 GORM-agnostic                                           |
| 3   | ID 由 application 层生成（UUIDv7），repo 不生成 ID，不设时间戳                         | 测试可注入确定值；事务内多写时间戳一致                              |
| 4   | `WorkflowDSL` 整列 JSONB；`ValueSource.Value` 自定义 `UnmarshalJSON` 按 `Kind` 分发   | DSL 是聚合内整体快照；查询模式只有"按 versionID 拿整 DSL"            |
| 5   | 所有用户可见删除 = 软删（`deleted_at`），FK 上 **不**加 `ON DELETE CASCADE`           | 保留运行历史；级联删除会让"指向已删行"语义模糊                      |
| 6   | repo 写方法的多 SQL / 读+写路径，方法内部 `storage.DBTransaction(ctx, fn)` 自包       | application 层不需要记忆"哪些方法需要事务"，self-tx 嵌套幂等        |
| 7   | 单条写直接 `storage.GetDB(ctx)`，dbresolver 按 op 自动路由到写库                      | 无需为 trivial 写包 tx                                              |
| 8   | 状态机校验编码进 `UPDATE ... WHERE status IN (允许的 prev)`，单语句乐观锁              | 0 行影响即冲突，避免读-改-写竞态                                    |
| 9   | 错误结构（`RunError` / `NodeError`）整块按 JSONB 存，不拆字段                         | 字段未来可演化不动 schema；v1 不需要按 error_code 聚合              |
| 10  | `schema/schema.sql` 单文件                                                            | 9 张表手动灌即可；将来上迁移工具时再按表拆                          |
| 11  | 凭据 AES-256-GCM 加密；`SHINEFLOW_CRED_KEY` 环境变量提供 base64 编码的 32 字节密钥    | 启动时校验，缺失或长度错直接 fatal                                  |
| 12  | 测试用 testcontainers 起真实 PG，每个 test 在事务里跑、`t.Cleanup` 时 rollback         | 真 PG 才能验 JSONB / 索引 / 约束；事务隔离让测试间无副作用          |
| 13  | `infrastructure/util.MarshalToString/UnmarshalFromString` 由 domain 端破例 import     | sonic 性能价值 + 全项目 JSON 入口统一；标记为有意破例               |

## 4. 包布局

```
infrastructure/storage/
├── db.go                           # 已有：连接 + tx + ctx 路由
├── schema/
│   └── schema.sql                  # 全部 9 张表 + 索引 + 约束，单文件
├── storagetest/
│   └── setup.go                    # testcontainers helper（Setup(t) -> ctx）
├── workflow/
│   ├── repository.go               # impl domain/workflow.WorkflowRepository
│   ├── model.go                    # GORM model 结构体 + TableName
│   ├── dsl_codec.go                # WorkflowDSL ↔ JSONB Scanner/Valuer
│   ├── mapper.go                   # model ↔ domain entity 转换
│   └── repository_test.go
├── run/
│   ├── repository.go               # impl domain/run.RunRepository
│   ├── model.go
│   ├── mapper.go
│   └── repository_test.go
├── cron/
│   ├── repository.go               # impl domain/cron.CronJobRepository
│   ├── model.go
│   ├── mapper.go
│   └── repository_test.go
├── plugin/                         # HttpPlugin + McpServer + McpTool 三 repo 共一包
│   ├── http_plugin_repository.go
│   ├── mcp_server_repository.go
│   ├── mcp_tool_repository.go
│   ├── model.go
│   ├── mapper.go
│   └── *_test.go
└── credential/
    ├── repository.go               # impl domain/credential.CredentialRepository
    ├── resolver.go                 # impl domain/credential.CredentialResolver
    ├── crypto.go                   # AES-GCM 加解密
    ├── model.go
    ├── mapper.go
    └── *_test.go
```

每个 aggregate 的子包对外只暴露：
- 一个无参构造函数 `NewXxxRepository() domain.XxxRepository`（DB 句柄从 `storage` 包级单例 + ctx 拿）
- 该 aggregate 的 sentinel error（如有，但通常 domain 已经定义）

GORM model 不外泄给 domain。

## 5. DB Schema

完整 DDL 落在 `infrastructure/storage/schema/schema.sql`：

```sql
-- ============================================================
-- 1) workflow_definitions
-- ============================================================
CREATE TABLE workflow_definitions (
    id                    UUID PRIMARY KEY,
    name                  TEXT NOT NULL,
    description           TEXT NOT NULL DEFAULT '',
    draft_version_id      UUID,                          -- 弱引用，无 FK
    published_version_id  UUID,                          -- 弱引用，无 FK
    created_by            TEXT NOT NULL,
    created_at            TIMESTAMPTZ NOT NULL,
    updated_at            TIMESTAMPTZ NOT NULL,
    deleted_at            TIMESTAMPTZ
);
CREATE INDEX idx_workflow_definitions_created_by ON workflow_definitions (created_by);
CREATE INDEX idx_workflow_definitions_name       ON workflow_definitions (name);

-- ============================================================
-- 2) workflow_versions
-- ============================================================
CREATE TABLE workflow_versions (
    id              UUID PRIMARY KEY,
    definition_id   UUID NOT NULL REFERENCES workflow_definitions(id),
    version         INTEGER NOT NULL,
    state           TEXT NOT NULL CHECK (state IN ('draft', 'release')),
    dsl             JSONB NOT NULL,
    revision        INTEGER NOT NULL,
    published_at    TIMESTAMPTZ,
    published_by    TEXT,
    created_at      TIMESTAMPTZ NOT NULL,
    updated_at      TIMESTAMPTZ NOT NULL,
    deleted_at      TIMESTAMPTZ,
    UNIQUE (definition_id, version)
);
-- 同一 definition 至多一个"活的" draft
CREATE UNIQUE INDEX uq_workflow_versions_one_draft
    ON workflow_versions (definition_id) WHERE state = 'draft' AND deleted_at IS NULL;
CREATE INDEX idx_workflow_versions_definition ON workflow_versions (definition_id, version DESC);

-- ============================================================
-- 3) workflow_runs
-- ============================================================
CREATE TABLE workflow_runs (
    id               UUID PRIMARY KEY,
    definition_id    UUID NOT NULL REFERENCES workflow_definitions(id),
    version_id       UUID NOT NULL REFERENCES workflow_versions(id),
    trigger_kind     TEXT NOT NULL CHECK (trigger_kind IN ('manual','webhook','api','cron')),
    trigger_ref      TEXT NOT NULL DEFAULT '',
    trigger_payload  JSONB NOT NULL DEFAULT '{}'::jsonb,
    status           TEXT NOT NULL CHECK (status IN ('pending','running','success','failed','cancelled')),
    started_at       TIMESTAMPTZ,
    ended_at         TIMESTAMPTZ,
    vars             JSONB NOT NULL DEFAULT '{}'::jsonb,
    end_node_id      TEXT,
    output           JSONB,
    error            JSONB,                              -- RunError 整块存
    created_by       TEXT NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL
);
CREATE INDEX idx_workflow_runs_definition_recent ON workflow_runs (definition_id, created_at DESC);
CREATE INDEX idx_workflow_runs_active            ON workflow_runs (status)
    WHERE status IN ('pending','running');

-- ============================================================
-- 4) workflow_node_runs
-- ============================================================
CREATE TABLE workflow_node_runs (
    id                UUID PRIMARY KEY,
    run_id            UUID NOT NULL REFERENCES workflow_runs(id),
    node_id           TEXT NOT NULL,
    node_type_key     TEXT NOT NULL,
    attempt           INTEGER NOT NULL,
    status            TEXT NOT NULL CHECK (status IN ('pending','running','success','failed','skipped')),
    started_at        TIMESTAMPTZ,
    ended_at          TIMESTAMPTZ,
    resolved_config   JSONB NOT NULL DEFAULT '{}'::jsonb,
    resolved_inputs   JSONB NOT NULL DEFAULT '{}'::jsonb,
    output            JSONB,
    fired_port        TEXT NOT NULL DEFAULT '',
    fallback_applied  BOOLEAN NOT NULL DEFAULT FALSE,
    error             JSONB,                             -- NodeError 整块存
    external_refs     JSONB NOT NULL DEFAULT '[]'::jsonb,
    UNIQUE (run_id, node_id, attempt)
);
-- GetLatestNodeRun(run_id, node_id) 取 attempt 最大的那条
CREATE INDEX idx_workflow_node_runs_latest ON workflow_node_runs (run_id, node_id, attempt DESC);

-- ============================================================
-- 5) cron_jobs
-- ============================================================
CREATE TABLE cron_jobs (
    id              UUID PRIMARY KEY,
    definition_id   UUID NOT NULL REFERENCES workflow_definitions(id),
    name            TEXT NOT NULL,
    description     TEXT NOT NULL DEFAULT '',
    expression      TEXT NOT NULL,
    timezone        TEXT NOT NULL,
    payload         JSONB NOT NULL DEFAULT '{}'::jsonb,
    enabled         BOOLEAN NOT NULL DEFAULT TRUE,
    next_fire_at    TIMESTAMPTZ,
    last_fire_at    TIMESTAMPTZ,
    last_run_id     UUID,                                -- 弱引用，无 FK
    created_by      TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL,
    updated_at      TIMESTAMPTZ NOT NULL,
    deleted_at      TIMESTAMPTZ
);
-- ClaimDue: ... WHERE enabled AND next_fire_at <= now() FOR UPDATE SKIP LOCKED
CREATE INDEX idx_cron_jobs_due ON cron_jobs (next_fire_at) WHERE enabled = TRUE;

-- ============================================================
-- 6) http_plugins
-- ============================================================
CREATE TABLE http_plugins (
    id                UUID PRIMARY KEY,
    name              TEXT NOT NULL,
    description       TEXT NOT NULL DEFAULT '',
    method            TEXT NOT NULL,
    url               TEXT NOT NULL,
    headers           JSONB NOT NULL DEFAULT '{}'::jsonb,
    query_params      JSONB NOT NULL DEFAULT '{}'::jsonb,
    body_template     TEXT NOT NULL DEFAULT '',
    auth_kind         TEXT NOT NULL CHECK (auth_kind IN ('none','api_key','bearer','basic')),
    credential_id     UUID,                              -- 弱引用，无 FK
    input_schema      JSONB NOT NULL DEFAULT '[]'::jsonb,
    output_schema     JSONB NOT NULL DEFAULT '[]'::jsonb,
    response_mapping  JSONB NOT NULL DEFAULT '{}'::jsonb,
    enabled           BOOLEAN NOT NULL DEFAULT TRUE,
    created_by        TEXT NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL,
    updated_at        TIMESTAMPTZ NOT NULL,
    deleted_at        TIMESTAMPTZ
);
CREATE UNIQUE INDEX uq_http_plugins_name ON http_plugins (name) WHERE deleted_at IS NULL;

-- ============================================================
-- 7) mcp_servers
-- ============================================================
CREATE TABLE mcp_servers (
    id               UUID PRIMARY KEY,
    name             TEXT NOT NULL,
    transport        TEXT NOT NULL CHECK (transport IN ('stdio','http','sse')),
    config           JSONB NOT NULL,
    credential_id    UUID,                               -- 弱引用，无 FK
    enabled          BOOLEAN NOT NULL DEFAULT TRUE,
    last_synced_at   TIMESTAMPTZ,
    last_sync_error  TEXT,
    created_by       TEXT NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL,
    updated_at       TIMESTAMPTZ NOT NULL,
    deleted_at       TIMESTAMPTZ
);
CREATE UNIQUE INDEX uq_mcp_servers_name ON mcp_servers (name) WHERE deleted_at IS NULL;

-- ============================================================
-- 8) mcp_tools
-- ============================================================
CREATE TABLE mcp_tools (
    id                UUID PRIMARY KEY,
    server_id         UUID NOT NULL REFERENCES mcp_servers(id),
    name              TEXT NOT NULL,
    description       TEXT NOT NULL DEFAULT '',
    input_schema_raw  JSONB NOT NULL,
    enabled           BOOLEAN NOT NULL DEFAULT TRUE,
    synced_at         TIMESTAMPTZ NOT NULL,
    UNIQUE (server_id, name)
);

-- ============================================================
-- 9) credentials
-- ============================================================
CREATE TABLE credentials (
    id                 UUID PRIMARY KEY,
    name               TEXT NOT NULL,
    kind               TEXT NOT NULL CHECK (kind IN ('api_key','bearer','basic','custom')),
    encrypted_payload  BYTEA NOT NULL,
    created_by         TEXT NOT NULL,
    created_at         TIMESTAMPTZ NOT NULL,
    updated_at         TIMESTAMPTZ NOT NULL,
    deleted_at         TIMESTAMPTZ
);
CREATE UNIQUE INDEX uq_credentials_name ON credentials (name) WHERE deleted_at IS NULL;
```

### 5.1 设计说明

- **软删的 6 张表**：`workflow_definitions` / `workflow_versions` / `cron_jobs` / `http_plugins` / `mcp_servers` / `credentials`。GORM model 端用 `gorm.DeletedAt` 类型，自动给所有 SELECT 加 `WHERE deleted_at IS NULL`，自动把 `db.Delete(&m)` 转成 UPDATE。
- **不软删的 3 张**：`workflow_runs` / `workflow_node_runs` 是纯历史，从不删；`mcp_tools` 由 sync 行为整体替换，不是用户语义的删除。
- **FK 全部不带 `ON DELETE CASCADE`**：因为软删后 parent 行永远不真正消失，FK 始终 valid；强引用只用于"运行时绝不能悬空"的关系（version → definition、run → definition+version、node_run → run、tool → server）。
- **弱引用不加 FK**：`workflow_definitions.{draft_version_id, published_version_id}`、`cron_jobs.last_run_id`、`http_plugins.credential_id`、`mcp_servers.credential_id`。这些是"指向最新一个"的指针，加 FK 反而要在删除时处理循环依赖。代码层面保证一致性。
- **`workflow_versions` UNIQUE (definition_id, version) 不带 `deleted_at` 过滤**：version 号严格单调不复用，软删的 draft v3 不会让新 draft 也叫 v3。
- **Name 类 unique 索引带 `WHERE deleted_at IS NULL`**：删了同名能再创建。
- **`schema.sql` 单文件**：9 张表手动灌即可（`psql $DSN -f schema.sql`），将来上迁移工具时再按表拆。

## 6. WorkflowDSL JSON 序列化

### 6.1 自定义 `UnmarshalJSON` 的必要性

`ValueSource.Value` 是 `any`，三种 Kind 解出来该是不同结构：

```
literal  → 原始字面量（string / number / bool / object / array）
ref      → RefValue 结构体
template → string
```

标准 `encoding/json` 反序列化时把所有 `any` 都解成 `map[string]any` / `float64`，丢掉 `RefValue` 的类型信息。`ValueSource` 必须自定义 `UnmarshalJSON` 按 `Kind` 分发。

### 6.2 domain 端 codec

`domain/workflow/value.go` 现有 `ValueSource` / `RefValue` / `ValueKind*` 上加 marshal 方法：

```go
package workflow

import (
    "encoding/json"
    "fmt"

    "github.com/shinya/shineflow/infrastructure/util"
)

func (v *ValueSource) UnmarshalJSON(data []byte) error {
    var raw struct {
        Kind  ValueKind       `json:"kind"`
        Value json.RawMessage `json:"value"`
    }
    if err := util.UnmarshalFromString(string(data), &raw); err != nil {
        return err
    }
    v.Kind = raw.Kind
    switch raw.Kind {
    case ValueKindRef:
        var ref RefValue
        if err := util.UnmarshalFromString(string(raw.Value), &ref); err != nil {
            return fmt.Errorf("ref value: %w", err)
        }
        v.Value = ref
    case ValueKindTemplate:
        var s string
        if err := util.UnmarshalFromString(string(raw.Value), &s); err != nil {
            return fmt.Errorf("template value: %w", err)
        }
        v.Value = s
    case ValueKindLiteral:
        var any any
        if err := util.UnmarshalFromString(string(raw.Value), &any); err != nil {
            return fmt.Errorf("literal value: %w", err)
        }
        v.Value = any
    default:
        return fmt.Errorf("unknown value kind: %q", raw.Kind)
    }
    return nil
}

func (v ValueSource) MarshalJSON() ([]byte, error) {
    s, err := util.MarshalToString(struct {
        Kind  ValueKind `json:"kind"`
        Value any       `json:"value"`
    }{v.Kind, v.Value})
    if err != nil {
        return nil, err
    }
    return []byte(s), nil
}
```

### 6.3 所有 DSL struct 加 `json:"snake_case"` tag

`WorkflowDSL` / `Node` / `Edge` / `ValueSource` / `RefValue` / `ErrorPolicy` / `NodeUI` / `PortSpec` / `SchemaType` 全部加上 `json` tag，例：

```go
type Node struct {
    ID          string                 `json:"id"`
    TypeKey     string                 `json:"type_key"`
    TypeVer     string                 `json:"type_ver"`
    Name        string                 `json:"name"`
    Config      json.RawMessage        `json:"config,omitempty"`
    Inputs      map[string]ValueSource `json:"inputs,omitempty"`
    ErrorPolicy *ErrorPolicy           `json:"error_policy,omitempty"`
    UI          NodeUI                 `json:"ui"`
}
```

JSON 字段名是 DSL 的稳定契约，重命名等价于 DB 迁移。

### 6.4 storage 端 GORM 接驳

`storage/workflow/dsl_codec.go`：

```go
package workflow

import (
    "database/sql/driver"
    "fmt"

    "github.com/shinya/shineflow/domain/workflow"
    "github.com/shinya/shineflow/infrastructure/util"
)

// dslColumn 是 GORM 模型里 dsl 列的实际类型。
// 不直接给 workflow.WorkflowDSL 加 Scan/Value，避免 domain 类型沾 database/sql。
type dslColumn workflow.WorkflowDSL

func (d *dslColumn) Scan(src any) error {
    var s string
    switch v := src.(type) {
    case []byte:
        s = string(v)
    case string:
        s = v
    default:
        return fmt.Errorf("dsl: unsupported scan type %T", src)
    }
    return util.UnmarshalFromString(s, (*workflow.WorkflowDSL)(d))
}

func (d dslColumn) Value() (driver.Value, error) {
    return util.MarshalToString(workflow.WorkflowDSL(d))
}
```

### 6.5 其他 JSONB 列

| 列 | domain 类型 | model 字段类型策略 |
|---|---|---|
| `workflow_runs.trigger_payload` | `json.RawMessage` | 直接 `json.RawMessage`，GORM 自动 raw bytes 写 JSONB |
| `workflow_runs.vars` | `json.RawMessage` | 同上 |
| `workflow_runs.output` | `json.RawMessage` | 同上 |
| `workflow_runs.error` | `*RunError` 结构体 | typed wrapper `runErrorColumn`（`*RunError`），`Scan`/`Value` 走 util |
| `workflow_node_runs.resolved_config` | `json.RawMessage` | 直接 `json.RawMessage` |
| `workflow_node_runs.resolved_inputs` | `json.RawMessage` | 同上 |
| `workflow_node_runs.output` | `json.RawMessage` | 同上 |
| `workflow_node_runs.error` | `*NodeError` 结构体 | typed wrapper `nodeErrorColumn` |
| `workflow_node_runs.external_refs` | `[]ExternalRef` | typed wrapper `externalRefsColumn` |
| `cron_jobs.payload` | `json.RawMessage` | 直接 `json.RawMessage` |
| `http_plugins.headers` / `query_params` / `response_mapping` | `map[string]string` | typed wrapper（共用一个 `stringMapColumn`） |
| `http_plugins.input_schema` / `output_schema` | `[]workflow.PortSpec` | typed wrapper `portSpecsColumn` |
| `mcp_servers.config` | `json.RawMessage` | 直接 `json.RawMessage` |
| `mcp_tools.input_schema_raw` | `json.RawMessage` | 直接 `json.RawMessage` |

通用规则：domain 端是 `json.RawMessage` 的，model 直接复用；domain 端是结构化类型的，model 端用一个 typed alias + `Scan`/`Value`，模板和 §6.4 的 `dslColumn` 完全一致。

### 6.6 domain 破例说明

domain `value.go` import `infrastructure/util` 是有意破例。原 `domain/doc.go` 写的"禁止依赖外部框架（hertz / gorm / sonic 等）"需微调成：

> 禁止直接依赖外部框架（hertz / gorm 等）；JSON 序列化通过 `infrastructure/util` 这一层薄封装走，使全项目 JSON 入口统一。

这一条 spec 落地时必须同时改 `domain/doc.go`。

## 7. Repo 实现规范

### 7.1 Model 层

每个 aggregate 的 `model.go` 定义 GORM model 结构体，**不外泄给 domain**。例：

```go
// storage/workflow/model.go
package workflow

import (
    "time"
    "gorm.io/gorm"
)

type definitionModel struct {
    ID                  string         `gorm:"primaryKey;type:uuid"`
    Name                string         `gorm:"not null"`
    Description         string         `gorm:"not null;default:''"`
    DraftVersionID      *string        `gorm:"type:uuid"`
    PublishedVersionID  *string        `gorm:"type:uuid"`
    CreatedBy           string         `gorm:"not null"`
    CreatedAt           time.Time      `gorm:"not null"`
    UpdatedAt           time.Time      `gorm:"not null"`
    DeletedAt           gorm.DeletedAt `gorm:"index"`         // 软删
}

func (definitionModel) TableName() string { return "workflow_definitions" }
```

`gorm.DeletedAt` 自动给 SELECT 加 `WHERE deleted_at IS NULL`，把 `db.Delete(&m)` 转 UPDATE。绕过用 `.Unscoped()`。

`mapper.go` 提供双向转换：

```go
func toDefinition(m *definitionModel) *workflow.WorkflowDefinition { ... }
func toDefinitionModel(d *workflow.WorkflowDefinition) *definitionModel { ... }
```

### 7.2 ID 与时间戳

约定：repo **不**生成 ID、**不**设时间戳。entity 进入 repo 时 `ID / CreatedAt / UpdatedAt` 由 application 层填好（`github.com/google/uuid` 的 `uuid.NewV7().String()` + `time.Now()`）。Update 路径同理。

好处：
- 测试时调用方注入确定值
- 一个事务内多写时间戳一致
- repo 不抓 clock，纯翻译逻辑

### 7.3 读写路由 / 事务约定

- **读**（Get / List）：`storage.GetDB(ctx)`，默认走读库（多副本时 dbresolver 路由）
- **单条写**（一条 SQL 完成的 Create / Update / Delete）：`storage.GetDB(ctx)` 即可。dbresolver 按 op 自动选写库
- **多条写、读+写**（需要原子性的复合动作）：repo 方法内部包 `storage.DBTransaction(ctx, fn)`
- **跨 repo 原子**：application 层显式 `storage.DBTransaction(ctx, fn)` 套，内层 self-tx 命中"已在 tx → 直接 fn(ctx)"分支幂等复用
- **强制读写库**（普通读路径下要"读自己刚写的"）：`ctx = storage.SetCluster(ctx, ClusterWrite)`

#### 哪些方法落哪一档

**单条写（无 tx）**：
- `WorkflowRepository`：CreateDefinition / UpdateDefinition / DeleteDefinition
- `RunRepository`：Create / UpdateStatus / SaveEndResult / SaveVars / SaveError / AppendNodeRun / UpdateNodeRunStatus / SaveNodeRunOutput
  - 状态机校验编码进 `UPDATE ... WHERE status IN (允许的 prev)`，0 行影响即冲突
- `CronJobRepository`：Create / Update / Delete / MarkFired
- `HttpPluginRepository` / `McpServerRepository` 全部 CRUD
- `CredentialRepository` 全部 CRUD

**需要 self-tx**：
- `WorkflowRepository.SaveVersion`：读 head → update or insert
- `WorkflowRepository.PublishVersion`：校验 head + update version state + update definition 两表
- `WorkflowRepository.DiscardDraft`：read draft + 软删 + 清 definition.draft_version_id
- `McpToolRepository.UpsertAll`：删失踪 + upsert 现有

**需要外部 tx**（不能自包）：
- `CronJobRepository.ClaimDue`：用 `SELECT ... FOR UPDATE SKIP LOCKED`，行锁要持续到 caller 处理完 `MarkFired` 再 COMMIT。该方法 godoc 必须点明"调用方必须已在 `storage.DBTransaction` 内"

### 7.4 错误映射

| 触发条件                                                                          | 返回                           |
| --------------------------------------------------------------------------------- | ------------------------------ |
| `errors.Is(err, gorm.ErrRecordNotFound)` 在 `GetDefinition` / `ListVersions` 等   | `workflow.ErrDefinitionNotFound` |
| `gorm.ErrRecordNotFound` 在 `GetVersion`                                          | `workflow.ErrVersionNotFound`   |
| `SaveVersion` 中 `expectedRevision` 与预读出来的 head.Revision 不一致；或 `UPDATE ... WHERE revision=?` 返回 `RowsAffected=0`（并发覆盖） | `workflow.ErrRevisionMismatch`  |
| `PublishVersion` 时 versionID ≠ head                                              | `workflow.ErrNotHead`           |
| `PublishVersion` 时 draft 校验失败                                                | `workflow.ErrDraftValidation`（外层包一份 `validator.ValidationResult.Errors`） |
| `RunRepository.UpdateStatus` 等状态机方法 `RowsAffected=0` 且行存在               | repo 内 sentinel：`run.ErrInvalidStateTransition`（domain 需新增） |
| 其他 PG / GORM 错误（unique 违反等"理论不该出现"的）                              | `fmt.Errorf("xxx repo: %w", err)` 透传                              |

### 7.5 SaveVersion 模板（self-tx）

```go
func (r *workflowRepo) SaveVersion(
    ctx context.Context, defID string, dsl workflow.WorkflowDSL, expectedRevision int,
) (*workflow.WorkflowVersion, error) {
    var out *workflow.WorkflowVersion
    err := storage.DBTransaction(ctx, func(ctx context.Context) error {
        db := storage.GetDB(ctx)

        var head versionModel
        err := db.Where("definition_id = ?", defID).
            Order("version DESC").Limit(1).Take(&head).Error

        switch {
        case errors.Is(err, gorm.ErrRecordNotFound):
            v, e := r.insertNewDraft(ctx, defID, dsl, 1, 1)
            out = v
            return e

        case err != nil:
            return err

        case head.State == "draft":
            if expectedRevision != head.Revision {
                return workflow.ErrRevisionMismatch
            }
            res := db.Model(&versionModel{}).
                Where("id = ? AND revision = ?", head.ID, expectedRevision).
                Updates(map[string]any{
                    "dsl":        dslColumn(dsl),
                    "revision":   gorm.Expr("revision + 1"),
                    "updated_at": time.Now(),
                })
            if res.Error != nil {
                return res.Error
            }
            if res.RowsAffected == 0 {
                return workflow.ErrRevisionMismatch
            }
            v, e := r.getVersionWithinTx(ctx, head.ID)
            out = v
            return e

        default: // head 是 release → 追加新 draft
            v, e := r.insertNewDraft(ctx, defID, dsl, head.Version+1, 1)
            out = v
            return e
        }
    })
    return out, err
}
```

`PublishVersion` / `DiscardDraft` / `McpToolRepository.UpsertAll` 同模板：方法首句 `storage.DBTransaction(ctx, fn)` 包整个动作。

未列出的私有 helper（`r.insertNewDraft` / `r.getVersionWithinTx` 等）是实现细节，约定 helper 不再自包 tx——它们假定调用方已经在事务里，直接 `storage.GetDB(ctx)` 拿 tx 句柄。命名上以 `WithinTx` / `*` 后缀提示。

### 7.6 DiscardDraft 行为变更

domain 注释原写"硬删 draft 行"，本 spec 改为软删：

```sql
UPDATE workflow_versions SET deleted_at = NOW(), updated_at = NOW()
WHERE definition_id = $1 AND state = 'draft' AND deleted_at IS NULL;

UPDATE workflow_definitions SET draft_version_id = NULL, updated_at = NOW()
WHERE id = $1;
```

效果对用户等价（草稿消失），但 try-run 历史里的 `version_id` 引用始终 valid。`domain/workflow/repository.go` 对应注释要随之微调（spec 落地时一起改）。

### 7.7 构造函数

每个 repo 暴露一个无参构造函数：

```go
func NewWorkflowRepository() workflow.WorkflowRepository {
    return &workflowRepo{}
}
```

repo 结构体不持有任何状态（DB 句柄从 ctx + storage 单例拿）。

## 8. 测试方案

### 8.1 testcontainers helper

`storage/storagetest/setup.go`：

```go
package storagetest

import (
    "context"
    _ "embed"
    "sync"
    "testing"

    "github.com/testcontainers/testcontainers-go/modules/postgres"
    "gorm.io/driver/postgres"
    gormpkg "gorm.io/gorm"

    "github.com/shinya/shineflow/infrastructure/storage"
)

//go:embed ../schema/schema.sql
var schemaSQL string

var (
    once       sync.Once
    sharedDB   *gormpkg.DB
    container  *postgres.PostgresContainer
)

// Setup 返回一个已经挂了 tx 的 ctx；test 结束时自动 rollback，数据无残留。
func Setup(t *testing.T) context.Context {
    t.Helper()
    once.Do(func() {
        ctx := context.Background()
        c, err := postgres.RunContainer(ctx, /* image, db, user, pwd */)
        if err != nil { t.Fatal(err) }
        container = c
        dsn, _ := c.ConnectionString(ctx)
        db, err := gormpkg.Open(postgres.Open(dsn), &gormpkg.Config{})
        if err != nil { t.Fatal(err) }
        // 灌 schema：用 //go:embed 嵌入，避免依赖测试 cwd
        if err := db.Exec(string(schemaSQL)).Error; err != nil { t.Fatal(err) }
        sharedDB = db
        storage.UseDB(db) // 将 storage 包级单例指向容器连接
    })
    tx := sharedDB.Begin()
    t.Cleanup(func() { tx.Rollback() })
    return storage.WithTx(context.Background(), tx)
}
```

需要 `storage` 包对外补两个口子：
- `storage.UseDB(*gorm.DB)`：仅 test 用，覆盖 package-level `db` 单例
- `storage.WithTx(ctx, *gorm.DB) context.Context`：把 tx 注入 ctx 的 `keyDBTx`，prod 内部和 test 都用得到（prod 侧的 `DBTransaction` 内部已经在做这件事，只是私有）

### 8.2 测试模式

```go
func TestSaveVersion_NewDraftFromRelease(t *testing.T) {
    ctx := storagetest.Setup(t)  // ctx 已挂 tx，t.Cleanup 自动 rollback

    repo := workflow.NewWorkflowRepository()
    // ... seed Definition + 一个已 release 的 version ...

    v, err := repo.SaveVersion(ctx, defID, newDSL, 0)
    if err != nil { t.Fatal(err) }
    if v.Version != 2 || v.State != workflow.VersionStateDraft {
        t.Fatalf("got version=%d state=%s", v.Version, v.State)
    }
}
```

**self-tx 与 test 事务的兼容**：`Setup` 返回的 ctx 已在 tx 中，repo 内部 `DBTransaction(ctx, fn)` 命中"已在 tx → 直接 fn(ctx)"分支，正常执行；测试结束 rollback 全部撤销。

### 8.3 覆盖率底线

每个 repo 包至少：
- 每条 happy path（Create / Get / Update / Delete / List 正常返回）
- 每条 sentinel error 路径（`ErrXxxNotFound` / `ErrRevisionMismatch` / `ErrNotHead` / `ErrDraftValidation` / 软删后再创建同名）
- state-machine 类方法至少一个非法转移（验证 `RowsAffected=0` → 返回 `ErrInvalidStateTransition`）

### 8.4 容器生命周期

- 整个 test binary 共享一个 container（`sync.Once`），冷启 5-10 秒摊销到一次
- 测试间用事务隔离，schema 不重置
- container 在 process 退出时由 testcontainers 的 reaper 清理（Ryuk）

## 9. 凭据加密

### 9.1 算法与密钥

- **算法**：AES-256-GCM
- **密钥**：环境变量 `SHINEFLOW_CRED_KEY`，base64 编码的 32 字节随机密钥
- **校验**：`storage/credential.NewResolver(...)` 启动时 decode + 长度校验，缺失或长度错直接 fatal（panic）
- **生成密钥示例**：`openssl rand -base64 32`

### 9.2 密文格式

```
nonce(12B) || ciphertext || tag(16B)
```

每次加密生成全新随机 nonce（`crypto/rand`）。整体 raw bytes 写 `BYTEA` 列。

### 9.3 明文格式

domain 的 `Payload` 类型即 `map[string]string`，AES 加密前用 `util.MarshalToString` 序列化为 JSON 字符串。

### 9.4 实现

`storage/credential/crypto.go`：

```go
package credential

import (
    "crypto/aes"
    "crypto/cipher"
    "crypto/rand"
    "errors"
    "io"
)

func Encrypt(key, plaintext []byte) ([]byte, error) {
    block, err := aes.NewCipher(key)
    if err != nil { return nil, err }
    gcm, err := cipher.NewGCM(block)
    if err != nil { return nil, err }
    nonce := make([]byte, gcm.NonceSize())
    if _, err := io.ReadFull(rand.Reader, nonce); err != nil { return nil, err }
    return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

func Decrypt(key, ciphertext []byte) ([]byte, error) {
    block, err := aes.NewCipher(key)
    if err != nil { return nil, err }
    gcm, err := cipher.NewGCM(block)
    if err != nil { return nil, err }
    if len(ciphertext) < gcm.NonceSize() {
        return nil, errors.New("ciphertext too short")
    }
    nonce := ciphertext[:gcm.NonceSize()]
    return gcm.Open(nil, nonce, ciphertext[gcm.NonceSize():], nil)
}
```

`storage/credential/resolver.go`：

```go
type resolver struct {
    repo domain.CredentialRepository
    key  []byte
}

func NewResolver(repo domain.CredentialRepository) (domain.CredentialResolver, error) {
    raw := os.Getenv("SHINEFLOW_CRED_KEY")
    key, err := base64.StdEncoding.DecodeString(raw)
    if err != nil || len(key) != 32 {
        return nil, fmt.Errorf("SHINEFLOW_CRED_KEY: must be base64-encoded 32 bytes")
    }
    return &resolver{repo: repo, key: key}, nil
}

func (r *resolver) Resolve(ctx context.Context, credID string) (domain.Credential, domain.Payload, error) {
    c, err := r.repo.Get(ctx, credID)
    if err != nil { return domain.Credential{}, nil, err }

    plain, err := Decrypt(r.key, c.EncryptedPayload)
    if err != nil { return domain.Credential{}, nil, fmt.Errorf("decrypt: %w", err) }

    var p domain.Payload
    if err := util.UnmarshalFromString(string(plain), &p); err != nil {
        return domain.Credential{}, nil, fmt.Errorf("unmarshal payload: %w", err)
    }
    return *c, p, nil
}
```

### 9.5 安全约束

- domain 的 `Credential` entity **不**带明文字段；明文 `Payload` 仅作为 `Resolve` 返回值流转，不能落任何持久层
- repo 端不感知凭据；application / executor 层负责确保 `ResolvedInputs` 写库前把凭据值脱敏（这是 application 的责任，不在本 spec）

## 10. 需要的 storage 包改动

本 spec 的实现需要 `infrastructure/storage/db.go` 增补两个 export：

```go
// UseDB 仅供 test 使用：覆盖 package-level db 单例。
// prod 路径不应调用。
func UseDB(d *gorm.DB) { db = d }

// WithTx 把 tx 注入 ctx 的 keyDBTx。
// prod 内部 DBTransaction 已经在做这件事；export 出来给 test 用。
func WithTx(ctx context.Context, tx *gorm.DB) context.Context {
    return context.WithValue(ctx, keyDBTx, tx)
}
```

## 11. 需要的 domain 改动

本 spec 落地时同步：

1. `domain/workflow/value.go`：`ValueSource` 加 `MarshalJSON` / `UnmarshalJSON`，import `infrastructure/util`
2. `domain/workflow/dsl.go` / `value.go` / `port.go` / `error_policy.go` / `node_ui.go`：所有 struct 字段加 `json:"snake_case"` tag
3. `domain/workflow/repository.go` 的 `DiscardDraft` 注释从"硬删"改为"软删"
4. `domain/run/repository.go` 新增 sentinel：`ErrInvalidStateTransition`
5. `domain/doc.go`：禁止依赖说明微调（允许 `infrastructure/util` 这一层薄封装）

## 12. 验收清单

- [ ] `infrastructure/storage/schema/schema.sql` 包含 9 张表 + 全部索引 + 全部约束，能在 fresh PG 上 `psql -f` 跑通
- [ ] `infrastructure/storage/{workflow,run,cron,plugin,credential}/` 各自实现 domain 接口；`go build ./...` 通过
- [ ] 每个 repo 包的 `_test.go` 覆盖 §8.3 的底线
- [ ] `storage.UseDB` / `storage.WithTx` / `storagetest.Setup` 可用
- [ ] domain §11 的 5 项改动落地
- [ ] `SHINEFLOW_CRED_KEY` 缺失时 `NewResolver` 返回 error；service 启动失败
- [ ] `go test ./infrastructure/storage/...` 全绿（CI 上 GitHub Actions runner 自带 Docker）
- [ ] `go vet ./...` / `go build ./...` 全绿

## 13. 后续 spec 钩子

实现完本 spec 后，下一份 spec 候选（按依赖顺序）：

1. **`NodeTypeRegistry` / `ExecutorRegistry` 实现 spec**：缓存策略、`Invalidate` / `InvalidatePrefix` 的失效传播、HttpPlugin / McpTool 投影到 NodeType 的逻辑
2. **port adapter spec**：`HTTPClient` / `LLMClient` / `MCPClient` / `Sandbox` 各自的接口约定 + 配置 + 重试 + 鉴权
3. **builtin executor spec**：`domain/executor/builtin/` 下每个内置节点的执行语义
4. **application 层 spec**：用例编排、`SaveAndPublish` 联合事务、错误到 HTTP 状态映射
5. **HTTP / Webhook / Cron daemon spec**：facade 层路由、参数校验、SSE 推送

每份 spec 仍走 `spec → plan → 实现` 的节奏。
