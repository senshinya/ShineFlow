# ShineFlow Workflow Persistence 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal：** 把 `2026-04-26-shineflow-workflow-infra-design.md` 落到代码：5 个 aggregate 的 GORM 仓储实现 + 9 张表 DDL + WorkflowDSL JSON codec + AES-GCM 凭据加密 + testcontainers 测试 harness。

**Architecture：** 每个 aggregate 一个子包，挂在 `infrastructure/storage/<aggregate>/` 下，镜像 `domain/<aggregate>/`。GORM model 不外泄 domain，通过 `mapper.go` 双向转换。读写路由由 `storage.GetDB(ctx)` + dbresolver 自动处理；多语句原子操作在 repo 方法内部包 `storage.DBTransaction(ctx, fn)`，对外部已存在的 tx 幂等复用。测试用 testcontainers-go 起 PG 容器（per-package 单例），每个 test 在独立 tx 中跑、cleanup 时 rollback。

**Tech Stack：** Go 1.26 / GORM v2 / PostgreSQL ≥ 12 / testcontainers-go / sonic JSON / google/uuid (UUIDv7) / AES-256-GCM

---

## 文件结构

### 新建

| 路径 | 职责 |
|---|---|
| `infrastructure/storage/schema/schema.sql` | 9 张表全部 DDL + 索引 + 约束 |
| `infrastructure/storage/storagetest/setup.go` | testcontainers helper：`Setup(t) -> ctx with tx` |
| `infrastructure/storage/workflow/repository.go` | impl `domain/workflow.WorkflowRepository` |
| `infrastructure/storage/workflow/model.go` | GORM model：`definitionModel` / `versionModel` |
| `infrastructure/storage/workflow/dsl_codec.go` | `dslColumn` Scanner/Valuer |
| `infrastructure/storage/workflow/mapper.go` | model ↔ domain entity 转换 |
| `infrastructure/storage/workflow/repository_test.go` | 集成测试 |
| `infrastructure/storage/run/repository.go` | impl `domain/run.WorkflowRunRepository` |
| `infrastructure/storage/run/model.go` | `runModel` / `nodeRunModel` |
| `infrastructure/storage/run/codec.go` | `runErrorColumn` / `nodeErrorColumn` / `externalRefsColumn` |
| `infrastructure/storage/run/mapper.go` | 转换函数 |
| `infrastructure/storage/run/repository_test.go` | |
| `infrastructure/storage/cron/repository.go` | impl `domain/cron.CronJobRepository` |
| `infrastructure/storage/cron/model.go` | `cronJobModel` |
| `infrastructure/storage/cron/mapper.go` | |
| `infrastructure/storage/cron/repository_test.go` | |
| `infrastructure/storage/plugin/http_plugin_repository.go` | impl `HttpPluginRepository` |
| `infrastructure/storage/plugin/mcp_server_repository.go` | impl `McpServerRepository` |
| `infrastructure/storage/plugin/mcp_tool_repository.go` | impl `McpToolRepository` |
| `infrastructure/storage/plugin/model.go` | 三种 model |
| `infrastructure/storage/plugin/codec.go` | `stringMapColumn` / `portSpecsColumn` |
| `infrastructure/storage/plugin/mapper.go` | |
| `infrastructure/storage/plugin/repository_test.go` | |
| `infrastructure/storage/credential/repository.go` | impl `CredentialRepository` |
| `infrastructure/storage/credential/resolver.go` | impl `CredentialResolver` |
| `infrastructure/storage/credential/crypto.go` | AES-GCM |
| `infrastructure/storage/credential/model.go` | `credentialModel` |
| `infrastructure/storage/credential/mapper.go` | |
| `infrastructure/storage/credential/repository_test.go` | |
| `infrastructure/storage/credential/crypto_test.go` | 单元测试 |

### 修改

| 路径 | 改动 |
|---|---|
| `infrastructure/storage/db.go` | 加 `UseDB(*gorm.DB)` 和 `WithTx(ctx, *gorm.DB)` |
| `domain/workflow/dsl.go` | 全部 struct 加 `json:"snake_case"` tag |
| `domain/workflow/value.go` | `ValueSource` 加 `MarshalJSON` / `UnmarshalJSON`；其他 struct 加 tag |
| `domain/workflow/port.go` | 加 tag |
| `domain/workflow/error_policy.go` | 加 tag |
| `domain/workflow/node_ui.go` | 加 tag |
| `domain/workflow/repository.go` | 注释微调（"实现内部不得各自起事务" → 允许 self-tx） |
| `domain/doc.go` | 微调"禁止依赖外部框架"措辞，允许 `infrastructure/util` 破例 |
| `go.mod` / `go.sum` | 新增 `github.com/google/uuid` + testcontainers + 子模块 |

---

## Phase 0：基础设施

### Task 1：拉依赖

**Files：**
- Modify：`go.mod`、`go.sum`

- [ ] **Step 1：拉 google/uuid（UUIDv7 来源）**

Run：`go get github.com/google/uuid`
Expected：`go: added github.com/google/uuid v1.x.x`

- [ ] **Step 2：拉 testcontainers-go 主包 + postgres 模块**

Run：`go get github.com/testcontainers/testcontainers-go github.com/testcontainers/testcontainers-go/modules/postgres`
Expected：两条 `go: added` 行

- [ ] **Step 3：验证 build 仍然过**

Run：`go build ./...`
Expected：无输出（成功）

- [ ] **Step 4：commit**

```bash
git add go.mod go.sum
git commit -m "chore: add uuid and testcontainers deps"
```

---

### Task 2：扩 storage 包对外口子（UseDB、WithTx）

**Files：**
- Modify：`infrastructure/storage/db.go`

- [ ] **Step 1：编辑 db.go，加两个 export 函数**

在 `infrastructure/storage/db.go` 文件末尾追加：

```go
// UseDB 仅供 test 使用：覆盖 package-level db 单例。
// prod 路径不应调用。
func UseDB(d *gorm.DB) { db = d }

// WithTx 把 tx 注入 ctx 的 keyDBTx。
// prod 内部 DBTransaction 已在做这件事；export 出来给 test 用。
func WithTx(ctx context.Context, tx *gorm.DB) context.Context {
    return context.WithValue(ctx, keyDBTx, tx)
}
```

- [ ] **Step 2：build 验证**

Run：`go build ./...`
Expected：无输出

- [ ] **Step 3：commit**

```bash
git add infrastructure/storage/db.go
git commit -m "feat(storage): expose UseDB and WithTx for test harness"
```

---

### Task 3：domain workflow 加 JSON tag

DSL 序列化的稳定 wire format。

**Files：**
- Modify：`domain/workflow/dsl.go` / `value.go` / `port.go` / `error_policy.go` / `node_ui.go`

- [ ] **Step 1：改 `domain/workflow/dsl.go`**

把 `Node` / `Edge` / `WorkflowDSL` 的字段全部加 `json:"snake_case"` tag。完整替换：

```go
package workflow

import "encoding/json"

const (
    PortDefault = "default"
    PortError   = "error"
)

const DSLSchemaVersion = "1"

type Node struct {
    ID      string `json:"id"`
    TypeKey string `json:"type_key"`
    TypeVer string `json:"type_ver"`
    Name    string `json:"name"`

    Config json.RawMessage        `json:"config,omitempty"`
    Inputs map[string]ValueSource `json:"inputs,omitempty"`

    ErrorPolicy *ErrorPolicy `json:"error_policy,omitempty"`
    UI          NodeUI       `json:"ui"`
}

type Edge struct {
    ID       string `json:"id"`
    From     string `json:"from"`
    FromPort string `json:"from_port"`
    To       string `json:"to"`
}

type WorkflowDSL struct {
    Nodes []Node `json:"nodes"`
    Edges []Edge `json:"edges"`
}
```

注意保留原 doc 注释（这里为节省篇幅省略，实际编辑时保留每个 struct 顶部的中文注释块）。

- [ ] **Step 2：改 `domain/workflow/value.go` 的 `RefValue`（其他改动留 Task 4）**

只改 `RefValue` 字段加 tag，先不动 `ValueSource`：

```go
type RefValue struct {
    NodeID string `json:"node_id"`
    PortID string `json:"port_id"`
    Path   string `json:"path,omitempty"`
    Name   string `json:"name,omitempty"`
}
```

- [ ] **Step 3：改 `domain/workflow/port.go`**

```go
type PortSpec struct {
    ID       string     `json:"id"`
    Name     string     `json:"name"`
    Type     SchemaType `json:"type"`
    Required bool       `json:"required"`
    Desc     string     `json:"desc,omitempty"`
}

type SchemaType struct {
    Type       string                 `json:"type"`
    Properties map[string]*SchemaType `json:"properties,omitempty"`
    Items      *SchemaType            `json:"items,omitempty"`
    Enum       []any                  `json:"enum,omitempty"`
}
```

常量保留不动。

- [ ] **Step 4：改 `domain/workflow/error_policy.go`**

```go
type ErrorPolicy struct {
    Timeout      time.Duration `json:"timeout,omitempty"`
    MaxRetries   int           `json:"max_retries,omitempty"`
    RetryBackoff BackoffKind   `json:"retry_backoff,omitempty"`
    RetryDelay   time.Duration `json:"retry_delay,omitempty"`

    OnFinalFail    FailStrategy   `json:"on_final_fail,omitempty"`
    FallbackOutput map[string]any `json:"fallback_output,omitempty"`
}
```

- [ ] **Step 5：改 `domain/workflow/node_ui.go`**

```go
type NodeUI struct {
    X      float64  `json:"x"`
    Y      float64  `json:"y"`
    Width  *float64 `json:"width,omitempty"`
    Height *float64 `json:"height,omitempty"`
}
```

- [ ] **Step 6：build + 既有测试不退化**

Run：`go build ./... && go test ./domain/...`
Expected：所有测试 PASS（domain 既有测试只测结构体值，加 tag 不影响）

- [ ] **Step 7：commit**

```bash
git add domain/workflow/
git commit -m "feat(domain/workflow): add json tags for DSL serialization"
```

---

### Task 4：domain `ValueSource` 加自定义 marshal/unmarshal

`ValueSource.Value any` 必须按 `Kind` 分发反序列化。

**Files：**
- Modify：`domain/workflow/value.go`

- [ ] **Step 1：先写失败测试**

新建 `domain/workflow/value_test.go`：

```go
package workflow

import (
    "testing"

    "github.com/shinya/shineflow/infrastructure/util"
)

func TestValueSource_RoundTrip_Ref(t *testing.T) {
    src := ValueSource{
        Kind: ValueKindRef,
        Value: RefValue{
            NodeID: "n_start",
            PortID: "p_out",
            Path:   "data.url",
            Name:   "voice url",
        },
    }
    s, err := util.MarshalToString(src)
    if err != nil { t.Fatalf("marshal: %v", err) }

    var got ValueSource
    if err := util.UnmarshalFromString(s, &got); err != nil { t.Fatalf("unmarshal: %v", err) }

    if got.Kind != ValueKindRef { t.Fatalf("kind: got %q", got.Kind) }
    ref, ok := got.Value.(RefValue)
    if !ok { t.Fatalf("value type: %T", got.Value) }
    if ref.NodeID != "n_start" || ref.PortID != "p_out" || ref.Path != "data.url" {
        t.Fatalf("ref roundtrip mismatch: %+v", ref)
    }
}

func TestValueSource_RoundTrip_Literal(t *testing.T) {
    src := ValueSource{Kind: ValueKindLiteral, Value: "hello"}
    s, err := util.MarshalToString(src)
    if err != nil { t.Fatal(err) }
    var got ValueSource
    if err := util.UnmarshalFromString(s, &got); err != nil { t.Fatal(err) }
    if got.Kind != ValueKindLiteral || got.Value != "hello" {
        t.Fatalf("literal roundtrip: %+v", got)
    }
}

func TestValueSource_RoundTrip_Template(t *testing.T) {
    src := ValueSource{Kind: ValueKindTemplate, Value: "Hello {{name}}"}
    s, err := util.MarshalToString(src)
    if err != nil { t.Fatal(err) }
    var got ValueSource
    if err := util.UnmarshalFromString(s, &got); err != nil { t.Fatal(err) }
    if got.Kind != ValueKindTemplate || got.Value != "Hello {{name}}" {
        t.Fatalf("template roundtrip: %+v", got)
    }
}

func TestValueSource_UnknownKind_Errors(t *testing.T) {
    var got ValueSource
    err := util.UnmarshalFromString(`{"kind":"weird","value":1}`, &got)
    if err == nil { t.Fatal("expected error for unknown kind") }
}
```

- [ ] **Step 2：跑测试确认失败**

Run：`go test ./domain/workflow/ -run TestValueSource_RoundTrip -v`
Expected：FAIL（`ref` 解出来是 `map[string]any` 而不是 `RefValue`）

- [ ] **Step 3：实现 ValueSource 的 marshal/unmarshal + 加 tag**

完全替换 `domain/workflow/value.go`：

```go
package workflow

import (
    "encoding/json"
    "fmt"

    "github.com/shinya/shineflow/infrastructure/util"
)

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
    Kind  ValueKind `json:"kind"`
    Value any       `json:"value"`
}

// RefValue 引用上游某个节点输出端口的值（可选深路径）。
type RefValue struct {
    NodeID string `json:"node_id"`
    PortID string `json:"port_id"`
    Path   string `json:"path,omitempty"`
    Name   string `json:"name,omitempty"`
}

// MarshalJSON 显式定义形态，锁定字段顺序、走 sonic。
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

// UnmarshalJSON 按 Kind 分发，让 Value 解出正确的 Go 类型。
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
```

- [ ] **Step 4：测试通过**

Run：`go test ./domain/workflow/ -run TestValueSource -v`
Expected：4 个测试全 PASS

- [ ] **Step 5：跑全 domain 测试确认无回退**

Run：`go test ./domain/...`
Expected：全 PASS

- [ ] **Step 6：commit**

```bash
git add domain/workflow/value.go domain/workflow/value_test.go
git commit -m "feat(domain/workflow): add ValueSource marshal/unmarshal with kind dispatch"
```

---

### Task 5：domain 注释微调

**Files：**
- Modify：`domain/doc.go`、`domain/workflow/repository.go`

- [ ] **Step 1：改 `domain/doc.go` 的"禁止"段**

把：
```
// 禁止：依赖任何外部框架（hertz / gorm / sonic 等）。
```
替换为：
```
// 禁止：直接依赖 hertz / gorm 等外部框架。
// JSON 序列化通过 infrastructure/util 这一层薄封装走，使全项目 JSON 入口统一。
// 该 import 是有意破例（不构成"domain 依赖外部框架"的反例）。
```

- [ ] **Step 2：改 `domain/workflow/repository.go` 的接口注释**

把第 35-38 行的事务约束注释段（`// 关键事务约束（由 application 层 spec 决定具体 plumbing 形态）：` 整段）替换为：

```go
// 关键事务约束（由 infra spec 落地）：
//   - SaveVersion / PublishVersion / DiscardDraft 内部需要原子性，
//     允许实现自包 storage.DBTransaction（嵌套对外部 tx 幂等复用）。
//   - application 层若把 SaveVersion + PublishVersion 组合成"保存并发布"用例，
//     在外层显式 storage.DBTransaction 一次包住，内层 self-tx 自动复用同一 tx。
```

- [ ] **Step 3：build + 测试**

Run：`go build ./... && go test ./domain/...`
Expected：全 PASS

- [ ] **Step 4：commit**

```bash
git add domain/doc.go domain/workflow/repository.go
git commit -m "docs(domain): allow infrastructure/util import; clarify tx convention"
```

---

### Task 6：写 schema.sql

**Files：**
- Create：`infrastructure/storage/schema/schema.sql`

- [ ] **Step 1：创建目录**

Run：`mkdir -p infrastructure/storage/schema`

- [ ] **Step 2：写完整 DDL**

新建 `infrastructure/storage/schema/schema.sql`，从 spec §5 完整复制（9 张表 + 索引）：

```sql
-- ============================================================
-- 1) workflow_definitions
-- ============================================================
CREATE TABLE workflow_definitions (
    id                    UUID PRIMARY KEY,
    name                  TEXT NOT NULL,
    description           TEXT NOT NULL DEFAULT '',
    draft_version_id      UUID,
    published_version_id  UUID,
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
    UNIQUE (definition_id, version)
);
CREATE UNIQUE INDEX uq_workflow_versions_one_draft
    ON workflow_versions (definition_id) WHERE state = 'draft';
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
    error            JSONB,
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
    error             JSONB,
    external_refs     JSONB NOT NULL DEFAULT '[]'::jsonb,
    UNIQUE (run_id, node_id, attempt)
);
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
    last_run_id     UUID,
    created_by      TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL,
    updated_at      TIMESTAMPTZ NOT NULL,
    deleted_at      TIMESTAMPTZ
);
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
    credential_id     UUID,
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
    credential_id    UUID,
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

- [ ] **Step 3：commit（这一步还不能跑测试，等 Task 7 起容器）**

```bash
git add infrastructure/storage/schema/
git commit -m "feat(storage/schema): add full DDL for 9 persistence tables"
```

---

### Task 7：testcontainers harness

**Files：**
- Create：`infrastructure/storage/storagetest/setup.go`

- [ ] **Step 1：创建目录**

Run：`mkdir -p infrastructure/storage/storagetest`

- [ ] **Step 2：写 setup.go**

```go
// Package storagetest 提供测试用 PostgreSQL 容器 + 事务隔离 helper。
//
// 用法：
//
//	func TestXxx(t *testing.T) {
//	    ctx := storagetest.Setup(t)
//	    repo := workflow.NewWorkflowRepository()
//	    // ... 调 repo 方法，全部跑在 tx 里
//	    // t.Cleanup 自动 rollback，数据无残留
//	}
package storagetest

import (
    "context"
    _ "embed"
    "sync"
    "testing"
    "time"

    "github.com/testcontainers/testcontainers-go/modules/postgres"
    "gorm.io/driver/postgres"
    gormpkg "gorm.io/gorm"

    "github.com/shinya/shineflow/infrastructure/storage"
)

//go:embed ../schema/schema.sql
var schemaSQL string

var (
    once     sync.Once
    sharedDB *gormpkg.DB
    initErr  error
)

// Setup 起容器（首次调用时）+ 灌 schema + 把 storage 包级 db 指向容器，
// 然后开一个事务、注入 ctx，cleanup 时 rollback。
func Setup(t *testing.T) context.Context {
    t.Helper()
    once.Do(func() { initErr = bootstrap() })
    if initErr != nil {
        t.Fatalf("storagetest bootstrap: %v", initErr)
    }
    tx := sharedDB.Begin()
    t.Cleanup(func() { tx.Rollback() })
    return storage.WithTx(context.Background(), tx)
}

func bootstrap() error {
    ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
    defer cancel()

    container, err := postgres.Run(ctx,
        "postgres:16-alpine",
        postgres.WithDatabase("shineflow_test"),
        postgres.WithUsername("test"),
        postgres.WithPassword("test"),
        postgres.BasicWaitStrategies(),
    )
    if err != nil { return err }

    dsn, err := container.ConnectionString(ctx, "sslmode=disable")
    if err != nil { return err }

    db, err := gormpkg.Open(postgres.Open(dsn), &gormpkg.Config{})
    if err != nil { return err }

    if err := db.Exec(schemaSQL).Error; err != nil { return err }

    sharedDB = db
    storage.UseDB(db)
    return nil
}
```

- [ ] **Step 3：build 验证（包能编过即可，目前还没有调用方测试）**

Run：`go build ./infrastructure/storage/storagetest/`
Expected：无输出

- [ ] **Step 4：commit**

```bash
git add infrastructure/storage/storagetest/
git commit -m "feat(storage/storagetest): testcontainers + per-test tx isolation harness"
```

---

## Phase 1：Workflow aggregate

### Task 8：workflow model + dsl_codec + mapper 骨架

**Files：**
- Create：`infrastructure/storage/workflow/model.go`
- Create：`infrastructure/storage/workflow/dsl_codec.go`
- Create：`infrastructure/storage/workflow/mapper.go`

- [ ] **Step 1：创建包目录**

Run：`mkdir -p infrastructure/storage/workflow`

- [ ] **Step 2：写 dsl_codec.go**

```go
package workflow

import (
    "database/sql/driver"
    "fmt"

    domainworkflow "github.com/shinya/shineflow/domain/workflow"
    "github.com/shinya/shineflow/infrastructure/util"
)

// dslColumn 是 GORM 模型里 dsl 列的实际类型。
// 不直接给 domainworkflow.WorkflowDSL 加 Scan/Value，避免 domain 沾 database/sql。
type dslColumn domainworkflow.WorkflowDSL

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
    return util.UnmarshalFromString(s, (*domainworkflow.WorkflowDSL)(d))
}

func (d dslColumn) Value() (driver.Value, error) {
    return util.MarshalToString(domainworkflow.WorkflowDSL(d))
}
```

- [ ] **Step 3：写 model.go**

```go
package workflow

import (
    "time"

    "gorm.io/gorm"
)

type definitionModel struct {
    ID                 string         `gorm:"primaryKey;type:uuid"`
    Name               string         `gorm:"not null"`
    Description        string         `gorm:"not null;default:''"`
    DraftVersionID     *string        `gorm:"type:uuid"`
    PublishedVersionID *string        `gorm:"type:uuid"`
    CreatedBy          string         `gorm:"not null"`
    CreatedAt          time.Time      `gorm:"not null"`
    UpdatedAt          time.Time      `gorm:"not null"`
    DeletedAt          gorm.DeletedAt `gorm:"index"`
}

func (definitionModel) TableName() string { return "workflow_definitions" }

type versionModel struct {
    ID           string    `gorm:"primaryKey;type:uuid"`
    DefinitionID string    `gorm:"type:uuid;not null;index"`
    Version      int       `gorm:"not null"`
    State        string    `gorm:"not null"`
    DSL          dslColumn `gorm:"type:jsonb;not null"`
    Revision     int       `gorm:"not null"`
    PublishedAt  *time.Time
    PublishedBy  *string
    CreatedAt    time.Time `gorm:"not null"`
    UpdatedAt    time.Time `gorm:"not null"`
}

func (versionModel) TableName() string { return "workflow_versions" }
```

- [ ] **Step 4：写 mapper.go**

```go
package workflow

import domainworkflow "github.com/shinya/shineflow/domain/workflow"

func toDefinition(m *definitionModel) *domainworkflow.WorkflowDefinition {
    return &domainworkflow.WorkflowDefinition{
        ID:                 m.ID,
        Name:               m.Name,
        Description:        m.Description,
        DraftVersionID:     m.DraftVersionID,
        PublishedVersionID: m.PublishedVersionID,
        CreatedBy:          m.CreatedBy,
        CreatedAt:          m.CreatedAt,
        UpdatedAt:          m.UpdatedAt,
    }
}

func toDefinitionModel(d *domainworkflow.WorkflowDefinition) *definitionModel {
    return &definitionModel{
        ID:                 d.ID,
        Name:               d.Name,
        Description:        d.Description,
        DraftVersionID:     d.DraftVersionID,
        PublishedVersionID: d.PublishedVersionID,
        CreatedBy:          d.CreatedBy,
        CreatedAt:          d.CreatedAt,
        UpdatedAt:          d.UpdatedAt,
    }
}

func toVersion(m *versionModel) *domainworkflow.WorkflowVersion {
    return &domainworkflow.WorkflowVersion{
        ID:           m.ID,
        DefinitionID: m.DefinitionID,
        Version:      m.Version,
        State:        domainworkflow.VersionState(m.State),
        DSL:          domainworkflow.WorkflowDSL(m.DSL),
        Revision:     m.Revision,
        PublishedAt:  m.PublishedAt,
        PublishedBy:  m.PublishedBy,
        CreatedAt:    m.CreatedAt,
        UpdatedAt:    m.UpdatedAt,
    }
}

func toVersionModel(v *domainworkflow.WorkflowVersion) *versionModel {
    return &versionModel{
        ID:           v.ID,
        DefinitionID: v.DefinitionID,
        Version:      v.Version,
        State:        string(v.State),
        DSL:          dslColumn(v.DSL),
        Revision:     v.Revision,
        PublishedAt:  v.PublishedAt,
        PublishedBy:  v.PublishedBy,
        CreatedAt:    v.CreatedAt,
        UpdatedAt:    v.UpdatedAt,
    }
}
```

- [ ] **Step 5：build 验证**

Run：`go build ./infrastructure/storage/workflow/`
Expected：无输出

- [ ] **Step 6：commit**

```bash
git add infrastructure/storage/workflow/model.go infrastructure/storage/workflow/dsl_codec.go infrastructure/storage/workflow/mapper.go
git commit -m "feat(storage/workflow): add model, dsl codec, and mapper skeletons"
```

---

### Task 9：WorkflowRepository 的 Definition CRUD

**Files：**
- Create：`infrastructure/storage/workflow/repository.go`
- Create：`infrastructure/storage/workflow/repository_test.go`

- [ ] **Step 1：先写失败测试 `repository_test.go`**

```go
package workflow_test

import (
    "errors"
    "testing"
    "time"

    "github.com/google/uuid"

    domainworkflow "github.com/shinya/shineflow/domain/workflow"
    "github.com/shinya/shineflow/infrastructure/storage/storagetest"
    storageworkflow "github.com/shinya/shineflow/infrastructure/storage/workflow"
)

func newDef(t *testing.T) *domainworkflow.WorkflowDefinition {
    t.Helper()
    now := time.Now().UTC()
    return &domainworkflow.WorkflowDefinition{
        ID:        uuid.NewString(),
        Name:      "test-def",
        CreatedBy: "u_alice",
        CreatedAt: now,
        UpdatedAt: now,
    }
}

func TestDefinition_CreateAndGet(t *testing.T) {
    ctx := storagetest.Setup(t)
    repo := storageworkflow.NewWorkflowRepository()

    d := newDef(t)
    if err := repo.CreateDefinition(ctx, d); err != nil {
        t.Fatalf("create: %v", err)
    }

    got, err := repo.GetDefinition(ctx, d.ID)
    if err != nil {
        t.Fatalf("get: %v", err)
    }
    if got.Name != d.Name || got.CreatedBy != d.CreatedBy {
        t.Fatalf("got %+v", got)
    }
}

func TestDefinition_GetNotFound(t *testing.T) {
    ctx := storagetest.Setup(t)
    repo := storageworkflow.NewWorkflowRepository()

    _, err := repo.GetDefinition(ctx, uuid.NewString())
    if !errors.Is(err, domainworkflow.ErrDefinitionNotFound) {
        t.Fatalf("expected ErrDefinitionNotFound, got: %v", err)
    }
}

func TestDefinition_Update(t *testing.T) {
    ctx := storagetest.Setup(t)
    repo := storageworkflow.NewWorkflowRepository()

    d := newDef(t)
    if err := repo.CreateDefinition(ctx, d); err != nil { t.Fatal(err) }

    d.Name = "updated"
    d.UpdatedAt = time.Now().UTC()
    if err := repo.UpdateDefinition(ctx, d); err != nil { t.Fatalf("update: %v", err) }

    got, _ := repo.GetDefinition(ctx, d.ID)
    if got.Name != "updated" {
        t.Fatalf("name not updated: %q", got.Name)
    }
}

func TestDefinition_DeleteSoft(t *testing.T) {
    ctx := storagetest.Setup(t)
    repo := storageworkflow.NewWorkflowRepository()

    d := newDef(t)
    if err := repo.CreateDefinition(ctx, d); err != nil { t.Fatal(err) }
    if err := repo.DeleteDefinition(ctx, d.ID); err != nil { t.Fatalf("delete: %v", err) }

    _, err := repo.GetDefinition(ctx, d.ID)
    if !errors.Is(err, domainworkflow.ErrDefinitionNotFound) {
        t.Fatalf("expected NotFound after soft delete, got: %v", err)
    }
}

func TestDefinition_List_FiltersByCreator(t *testing.T) {
    ctx := storagetest.Setup(t)
    repo := storageworkflow.NewWorkflowRepository()

    a, b := newDef(t), newDef(t)
    a.CreatedBy, b.CreatedBy = "u_alice", "u_bob"
    _ = repo.CreateDefinition(ctx, a)
    _ = repo.CreateDefinition(ctx, b)

    list, err := repo.ListDefinitions(ctx, domainworkflow.DefinitionFilter{CreatedBy: "u_alice"})
    if err != nil { t.Fatal(err) }
    if len(list) != 1 || list[0].ID != a.ID {
        t.Fatalf("filter not applied: %+v", list)
    }
}
```

- [ ] **Step 2：跑测试确认全失败**

Run：`go test ./infrastructure/storage/workflow/... -run TestDefinition -v`
Expected：FAIL（`workflowRepo` 没定义）

- [ ] **Step 3：实现 repository.go**

```go
package workflow

import (
    "context"
    "errors"

    "gorm.io/gorm"

    domainworkflow "github.com/shinya/shineflow/domain/workflow"
    "github.com/shinya/shineflow/infrastructure/storage"
)

type workflowRepo struct{}

// NewWorkflowRepository 构造一个 GORM 实现的 WorkflowRepository。
// 不持有任何状态，DB 句柄从 storage 包级单例 + ctx 拿。
func NewWorkflowRepository() domainworkflow.WorkflowRepository {
    return &workflowRepo{}
}

// ---- Definition CRUD ----

func (r *workflowRepo) CreateDefinition(ctx context.Context, d *domainworkflow.WorkflowDefinition) error {
    return storage.GetDB(ctx).Create(toDefinitionModel(d)).Error
}

func (r *workflowRepo) GetDefinition(ctx context.Context, id string) (*domainworkflow.WorkflowDefinition, error) {
    var m definitionModel
    err := storage.GetDB(ctx).Where("id = ?", id).Take(&m).Error
    if errors.Is(err, gorm.ErrRecordNotFound) {
        return nil, domainworkflow.ErrDefinitionNotFound
    }
    if err != nil {
        return nil, err
    }
    return toDefinition(&m), nil
}

func (r *workflowRepo) ListDefinitions(
    ctx context.Context, filter domainworkflow.DefinitionFilter,
) ([]*domainworkflow.WorkflowDefinition, error) {
    q := storage.GetDB(ctx).Model(&definitionModel{})
    if filter.CreatedBy != "" {
        q = q.Where("created_by = ?", filter.CreatedBy)
    }
    if filter.NameLike != "" {
        q = q.Where("name LIKE ?", "%"+filter.NameLike+"%")
    }
    if filter.Limit > 0 {
        q = q.Limit(filter.Limit)
    }
    if filter.Offset > 0 {
        q = q.Offset(filter.Offset)
    }
    q = q.Order("created_at DESC")

    var ms []definitionModel
    if err := q.Find(&ms).Error; err != nil {
        return nil, err
    }
    out := make([]*domainworkflow.WorkflowDefinition, 0, len(ms))
    for i := range ms {
        out = append(out, toDefinition(&ms[i]))
    }
    return out, nil
}

func (r *workflowRepo) UpdateDefinition(ctx context.Context, d *domainworkflow.WorkflowDefinition) error {
    res := storage.GetDB(ctx).Model(&definitionModel{}).
        Where("id = ?", d.ID).
        Updates(map[string]any{
            "name":                 d.Name,
            "description":          d.Description,
            "draft_version_id":     d.DraftVersionID,
            "published_version_id": d.PublishedVersionID,
            "updated_at":           d.UpdatedAt,
        })
    if res.Error != nil {
        return res.Error
    }
    if res.RowsAffected == 0 {
        return domainworkflow.ErrDefinitionNotFound
    }
    return nil
}

func (r *workflowRepo) DeleteDefinition(ctx context.Context, id string) error {
    res := storage.GetDB(ctx).Where("id = ?", id).Delete(&definitionModel{})
    if res.Error != nil {
        return res.Error
    }
    if res.RowsAffected == 0 {
        return domainworkflow.ErrDefinitionNotFound
    }
    return nil
}

// 占位：Version / Save / Publish / Discard 在后续 task 实现
func (r *workflowRepo) GetVersion(ctx context.Context, id string) (*domainworkflow.WorkflowVersion, error) {
    return nil, errors.New("not implemented")
}
func (r *workflowRepo) ListVersions(ctx context.Context, definitionID string) ([]*domainworkflow.WorkflowVersion, error) {
    return nil, errors.New("not implemented")
}
func (r *workflowRepo) SaveVersion(ctx context.Context, definitionID string, dsl domainworkflow.WorkflowDSL, expectedRevision int) (*domainworkflow.WorkflowVersion, error) {
    return nil, errors.New("not implemented")
}
func (r *workflowRepo) PublishVersion(ctx context.Context, versionID, publishedBy string) (*domainworkflow.WorkflowVersion, error) {
    return nil, errors.New("not implemented")
}
func (r *workflowRepo) DiscardDraft(ctx context.Context, definitionID string) error {
    return errors.New("not implemented")
}
```

- [ ] **Step 4：跑 Definition 测试确认通过**

Run：`go test ./infrastructure/storage/workflow/... -run TestDefinition -v`
Expected：5 个 test 全 PASS

- [ ] **Step 5：commit**

```bash
git add infrastructure/storage/workflow/repository.go infrastructure/storage/workflow/repository_test.go
git commit -m "feat(storage/workflow): implement Definition CRUD with soft delete"
```

---

### Task 10：GetVersion / ListVersions

**Files：**
- Modify：`infrastructure/storage/workflow/repository.go`
- Modify：`infrastructure/storage/workflow/repository_test.go`

- [ ] **Step 1：追加测试**

在 `repository_test.go` 末尾追加：

```go
func newDraft(t *testing.T, defID string, version int) *domainworkflow.WorkflowVersion {
    t.Helper()
    now := time.Now().UTC()
    return &domainworkflow.WorkflowVersion{
        ID:           uuid.NewString(),
        DefinitionID: defID,
        Version:      version,
        State:        domainworkflow.VersionStateDraft,
        DSL:          domainworkflow.WorkflowDSL{},
        Revision:     1,
        CreatedAt:    now,
        UpdatedAt:    now,
    }
}

// 测试 helper：直接通过 SaveVersion 生 head（依赖 Task 11 已实现的 SaveVersion）。
// 在 Task 10 阶段，可以临时手动 INSERT 一条 version 来跑下面的 GetVersion 测试。
// 这里写的是最终形态。
func TestVersion_GetNotFound(t *testing.T) {
    ctx := storagetest.Setup(t)
    repo := storageworkflow.NewWorkflowRepository()
    _, err := repo.GetVersion(ctx, uuid.NewString())
    if !errors.Is(err, domainworkflow.ErrVersionNotFound) {
        t.Fatalf("expected ErrVersionNotFound, got: %v", err)
    }
}

func TestVersion_ListEmpty(t *testing.T) {
    ctx := storagetest.Setup(t)
    repo := storageworkflow.NewWorkflowRepository()
    d := newDef(t)
    _ = repo.CreateDefinition(ctx, d)
    list, err := repo.ListVersions(ctx, d.ID)
    if err != nil { t.Fatal(err) }
    if len(list) != 0 { t.Fatalf("expected empty, got %d", len(list)) }
}
```

- [ ] **Step 2：跑测试确认失败**

Run：`go test ./infrastructure/storage/workflow/... -run TestVersion_ -v`
Expected：FAIL（"not implemented"）

- [ ] **Step 3：替换 GetVersion / ListVersions 实现**

把 `repository.go` 里 `GetVersion` 和 `ListVersions` 两个 stub 替换为：

```go
func (r *workflowRepo) GetVersion(ctx context.Context, id string) (*domainworkflow.WorkflowVersion, error) {
    var m versionModel
    err := storage.GetDB(ctx).Where("id = ?", id).Take(&m).Error
    if errors.Is(err, gorm.ErrRecordNotFound) {
        return nil, domainworkflow.ErrVersionNotFound
    }
    if err != nil {
        return nil, err
    }
    return toVersion(&m), nil
}

func (r *workflowRepo) ListVersions(
    ctx context.Context, definitionID string,
) ([]*domainworkflow.WorkflowVersion, error) {
    var ms []versionModel
    err := storage.GetDB(ctx).
        Where("definition_id = ?", definitionID).
        Order("version DESC").
        Find(&ms).Error
    if err != nil {
        return nil, err
    }
    out := make([]*domainworkflow.WorkflowVersion, 0, len(ms))
    for i := range ms {
        out = append(out, toVersion(&ms[i]))
    }
    return out, nil
}
```

- [ ] **Step 4：测试通过**

Run：`go test ./infrastructure/storage/workflow/... -run TestVersion_ -v`
Expected：2 个 test PASS

- [ ] **Step 5：commit**

```bash
git add infrastructure/storage/workflow/
git commit -m "feat(storage/workflow): implement GetVersion and ListVersions"
```

---

### Task 11：SaveVersion（最复杂）

3 个分支：head 不存在 → INSERT v1；head 是 draft → 乐观锁 UPDATE；head 是 release → INSERT v+1。整个方法 self-tx。

**Files：**
- Modify：`infrastructure/storage/workflow/repository.go`
- Modify：`infrastructure/storage/workflow/repository_test.go`

- [ ] **Step 1：追加测试**

在 `repository_test.go` 末尾追加：

```go
func TestSaveVersion_FirstDraft(t *testing.T) {
    ctx := storagetest.Setup(t)
    repo := storageworkflow.NewWorkflowRepository()
    d := newDef(t)
    _ = repo.CreateDefinition(ctx, d)

    v, err := repo.SaveVersion(ctx, d.ID, domainworkflow.WorkflowDSL{}, 0)
    if err != nil { t.Fatalf("save: %v", err) }
    if v.Version != 1 || v.Revision != 1 || v.State != domainworkflow.VersionStateDraft {
        t.Fatalf("unexpected first draft: version=%d rev=%d state=%s", v.Version, v.Revision, v.State)
    }
}

func TestSaveVersion_OverwriteDraft_Optimistic(t *testing.T) {
    ctx := storagetest.Setup(t)
    repo := storageworkflow.NewWorkflowRepository()
    d := newDef(t)
    _ = repo.CreateDefinition(ctx, d)

    v1, _ := repo.SaveVersion(ctx, d.ID, domainworkflow.WorkflowDSL{}, 0)
    if v1.Revision != 1 { t.Fatalf("v1.rev = %d", v1.Revision) }

    v2, err := repo.SaveVersion(ctx, d.ID, domainworkflow.WorkflowDSL{}, v1.Revision)
    if err != nil { t.Fatalf("save again: %v", err) }
    if v2.ID != v1.ID {
        t.Fatalf("expected in-place overwrite, got new id %s vs %s", v2.ID, v1.ID)
    }
    if v2.Revision != 2 {
        t.Fatalf("expected revision++ to 2, got %d", v2.Revision)
    }
}

func TestSaveVersion_RevisionMismatch(t *testing.T) {
    ctx := storagetest.Setup(t)
    repo := storageworkflow.NewWorkflowRepository()
    d := newDef(t)
    _ = repo.CreateDefinition(ctx, d)
    _, _ = repo.SaveVersion(ctx, d.ID, domainworkflow.WorkflowDSL{}, 0)

    _, err := repo.SaveVersion(ctx, d.ID, domainworkflow.WorkflowDSL{}, 99)
    if !errors.Is(err, domainworkflow.ErrRevisionMismatch) {
        t.Fatalf("expected ErrRevisionMismatch, got: %v", err)
    }
}

func TestSaveVersion_AppendAfterRelease(t *testing.T) {
    ctx := storagetest.Setup(t)
    repo := storageworkflow.NewWorkflowRepository()
    d := newDef(t)
    _ = repo.CreateDefinition(ctx, d)
    v1, _ := repo.SaveVersion(ctx, d.ID, domainworkflow.WorkflowDSL{}, 0)
    if _, err := repo.PublishVersion(ctx, v1.ID, "u_alice"); err != nil {
        t.Fatalf("publish v1: %v", err)
    }

    v2, err := repo.SaveVersion(ctx, d.ID, domainworkflow.WorkflowDSL{}, 0)
    if err != nil { t.Fatalf("save after release: %v", err) }
    if v2.Version != 2 || v2.Revision != 1 || v2.State != domainworkflow.VersionStateDraft {
        t.Fatalf("expected v=2 rev=1 draft, got v=%d rev=%d state=%s", v2.Version, v2.Revision, v2.State)
    }
}
```

注意 `TestSaveVersion_AppendAfterRelease` 还依赖 `PublishVersion`（Task 12 实现）；这一步它会 FAIL 在 `repo.PublishVersion`。运行 `-skip TestSaveVersion_AppendAfterRelease` 暂时跳过。

- [ ] **Step 2：跑前 3 个测试确认失败**

Run：`go test ./infrastructure/storage/workflow/... -run TestSaveVersion -skip TestSaveVersion_AppendAfterRelease -v`
Expected：3 个测试 FAIL（"not implemented"）

- [ ] **Step 3：实现 SaveVersion + 私有 helper**

把 `repository.go` 里 `SaveVersion` 替换 + 加 helper：

```go
import (
    "time"
    // ... 已有 imports

    "gorm.io/gorm"
    "github.com/google/uuid"
)

func (r *workflowRepo) SaveVersion(
    ctx context.Context,
    defID string,
    dsl domainworkflow.WorkflowDSL,
    expectedRevision int,
) (*domainworkflow.WorkflowVersion, error) {
    var out *domainworkflow.WorkflowVersion
    err := storage.DBTransaction(ctx, func(ctx context.Context) error {
        db := storage.GetDB(ctx)

        var head versionModel
        err := db.Where("definition_id = ?", defID).
            Order("version DESC").Limit(1).Take(&head).Error

        switch {
        case errors.Is(err, gorm.ErrRecordNotFound):
            v, e := r.insertNewDraftWithinTx(ctx, defID, dsl, 1, 1)
            out = v
            return e

        case err != nil:
            return err

        case head.State == string(domainworkflow.VersionStateDraft):
            if expectedRevision != head.Revision {
                return domainworkflow.ErrRevisionMismatch
            }
            res := db.Model(&versionModel{}).
                Where("id = ? AND revision = ?", head.ID, expectedRevision).
                Updates(map[string]any{
                    "dsl":        dslColumn(dsl),
                    "revision":   gorm.Expr("revision + 1"),
                    "updated_at": time.Now().UTC(),
                })
            if res.Error != nil { return res.Error }
            if res.RowsAffected == 0 {
                return domainworkflow.ErrRevisionMismatch
            }
            v, e := r.getVersionWithinTx(ctx, head.ID)
            out = v
            return e

        default: // head 是 release → 追加新 draft
            v, e := r.insertNewDraftWithinTx(ctx, defID, dsl, head.Version+1, 1)
            out = v
            return e
        }
    })
    return out, err
}

// insertNewDraftWithinTx 假定已在 tx 内：插入新 draft 行 + 把 Definition.draft_version_id 指过去。
func (r *workflowRepo) insertNewDraftWithinTx(
    ctx context.Context, defID string, dsl domainworkflow.WorkflowDSL, version, revision int,
) (*domainworkflow.WorkflowVersion, error) {
    db := storage.GetDB(ctx)
    now := time.Now().UTC()
    m := &versionModel{
        ID:           uuid.NewString(),
        DefinitionID: defID,
        Version:      version,
        State:        string(domainworkflow.VersionStateDraft),
        DSL:          dslColumn(dsl),
        Revision:     revision,
        CreatedAt:    now,
        UpdatedAt:    now,
    }
    if err := db.Create(m).Error; err != nil { return nil, err }

    if err := db.Model(&definitionModel{}).
        Where("id = ?", defID).
        Updates(map[string]any{
            "draft_version_id": m.ID,
            "updated_at":       now,
        }).Error; err != nil {
        return nil, err
    }
    return toVersion(m), nil
}

// getVersionWithinTx 假定已在 tx 内的查询。
func (r *workflowRepo) getVersionWithinTx(ctx context.Context, id string) (*domainworkflow.WorkflowVersion, error) {
    var m versionModel
    if err := storage.GetDB(ctx).Where("id = ?", id).Take(&m).Error; err != nil {
        return nil, err
    }
    return toVersion(&m), nil
}
```

记得在 import 段加上 `"github.com/google/uuid"` 和 `"time"`。

- [ ] **Step 4：跑 3 个 SaveVersion 测试通过**

Run：`go test ./infrastructure/storage/workflow/... -run "TestSaveVersion_FirstDraft|TestSaveVersion_OverwriteDraft|TestSaveVersion_RevisionMismatch" -v`
Expected：3 个 PASS

- [ ] **Step 5：commit**

```bash
git add infrastructure/storage/workflow/
git commit -m "feat(storage/workflow): implement SaveVersion with optimistic locking"
```

---

### Task 12：PublishVersion

需要：定位 head 校验、读 DSL 跑 validator、UPDATE version state、UPDATE definition 指针。三步全部 self-tx。

**Files：**
- Modify：`infrastructure/storage/workflow/repository.go`
- Modify：`infrastructure/storage/workflow/repository_test.go`

- [ ] **Step 1：追加测试**

```go
func TestPublishVersion_OK(t *testing.T) {
    ctx := storagetest.Setup(t)
    repo := storageworkflow.NewWorkflowRepository()
    d := newDef(t)
    _ = repo.CreateDefinition(ctx, d)
    v1, _ := repo.SaveVersion(ctx, d.ID, minimalValidDSL(), 0)

    pub, err := repo.PublishVersion(ctx, v1.ID, "u_alice")
    if err != nil { t.Fatalf("publish: %v", err) }
    if pub.State != domainworkflow.VersionStateRelease {
        t.Fatalf("state: %s", pub.State)
    }
    if pub.PublishedBy == nil || *pub.PublishedBy != "u_alice" {
        t.Fatalf("published_by: %v", pub.PublishedBy)
    }

    // Definition 的指针应已切换
    gotD, _ := repo.GetDefinition(ctx, d.ID)
    if gotD.DraftVersionID != nil {
        t.Fatalf("draft_version_id should be nil, got %v", gotD.DraftVersionID)
    }
    if gotD.PublishedVersionID == nil || *gotD.PublishedVersionID != v1.ID {
        t.Fatalf("published_version_id: %v", gotD.PublishedVersionID)
    }
}

func TestPublishVersion_NotHead(t *testing.T) {
    ctx := storagetest.Setup(t)
    repo := storageworkflow.NewWorkflowRepository()
    d := newDef(t)
    _ = repo.CreateDefinition(ctx, d)
    v1, _ := repo.SaveVersion(ctx, d.ID, minimalValidDSL(), 0)
    _, _ = repo.PublishVersion(ctx, v1.ID, "u_alice")
    // 现在 head 已是 release v1，再追加一个 draft v2
    v2, _ := repo.SaveVersion(ctx, d.ID, minimalValidDSL(), 0)
    _ = v2

    // 试图 publish 老的 v1（已 release，幂等）→ OK
    if _, err := repo.PublishVersion(ctx, v1.ID, "u_alice"); err != nil {
        t.Fatalf("re-publish v1 (idempotent) should succeed: %v", err)
    }
}

func TestPublishVersion_DraftValidationFails(t *testing.T) {
    ctx := storagetest.Setup(t)
    repo := storageworkflow.NewWorkflowRepository()
    d := newDef(t)
    _ = repo.CreateDefinition(ctx, d)
    // 空 DSL（无 start/end）必然校验失败
    v, _ := repo.SaveVersion(ctx, d.ID, domainworkflow.WorkflowDSL{}, 0)

    _, err := repo.PublishVersion(ctx, v.ID, "u_alice")
    if !errors.Is(err, domainworkflow.ErrDraftValidation) {
        t.Fatalf("expected ErrDraftValidation, got: %v", err)
    }
}

// minimalValidDSL 是能通过 validator.ValidateForPublish 的最小 DSL：
// start → llm → end，节点和端口齐备。校验依赖 NodeTypeRegistry，
// 但 PublishVersion 内部应允许传入 nil registry → 跳过校验？
//
// 注意：这里需要一个 NodeTypeRegistry。实际 publish 路径是 application 层
// 注入 registry 调 validator；本测试如果 PublishVersion 实现内部直接 new 一个
// 内置 registry，则 minimalValidDSL 可以构造为 builtin.start + builtin.end + 边。
// 详见 §下一步 task 注释。
func minimalValidDSL() domainworkflow.WorkflowDSL {
    return domainworkflow.WorkflowDSL{
        Nodes: []domainworkflow.Node{
            {ID: "n_start", TypeKey: "builtin.start", TypeVer: "1"},
            {ID: "n_end", TypeKey: "builtin.end", TypeVer: "1"},
        },
        Edges: []domainworkflow.Edge{
            {ID: "e1", From: "n_start", FromPort: domainworkflow.PortDefault, To: "n_end"},
        },
    }
}
```

- [ ] **Step 2：跑测试确认失败**

Run：`go test ./infrastructure/storage/workflow/... -run TestPublishVersion -v`
Expected：FAIL（"not implemented"）

- [ ] **Step 3：实现 PublishVersion**

`PublishVersion` 内部需要校验 DSL，因此要有一个 NodeTypeRegistry 实例。当前 spec scope 不含 registry impl，但 domain 已有 `nodetype.NodeTypeRegistry` 接口和 builtin NodeType 常量。临时方案：构造一个仅含 builtin 节点的 in-package registry helper，PublishVersion 内部用它跑校验。

把 stub `PublishVersion` 替换为：

```go
import (
    // ... 已有 imports

    "github.com/shinya/shineflow/domain/nodetype"
    "github.com/shinya/shineflow/domain/validator"
)

func (r *workflowRepo) PublishVersion(
    ctx context.Context, versionID, publishedBy string,
) (*domainworkflow.WorkflowVersion, error) {
    var out *domainworkflow.WorkflowVersion
    err := storage.DBTransaction(ctx, func(ctx context.Context) error {
        db := storage.GetDB(ctx)

        // 1. 读目标 version
        var target versionModel
        err := db.Where("id = ?", versionID).Take(&target).Error
        if errors.Is(err, gorm.ErrRecordNotFound) {
            return domainworkflow.ErrVersionNotFound
        }
        if err != nil { return err }

        // 2. 已是 release → 幂等成功
        if target.State == string(domainworkflow.VersionStateRelease) {
            out = toVersion(&target)
            return nil
        }

        // 3. 校验是 head（同一 definition_id 下最大 version）
        var headVersion int
        if err := db.Model(&versionModel{}).
            Where("definition_id = ?", target.DefinitionID).
            Select("COALESCE(MAX(version), 0)").
            Scan(&headVersion).Error; err != nil {
            return err
        }
        if target.Version != headVersion {
            return domainworkflow.ErrNotHead
        }

        // 4. draft → 跑严格校验
        result := validator.ValidateForPublish(domainworkflow.WorkflowDSL(target.DSL), builtinNodeTypeRegistry())
        if !result.OK() {
            return domainworkflow.ErrDraftValidation
        }

        // 5. UPDATE version
        now := time.Now().UTC()
        if err := db.Model(&versionModel{}).Where("id = ?", versionID).Updates(map[string]any{
            "state":        string(domainworkflow.VersionStateRelease),
            "published_at": now,
            "published_by": publishedBy,
            "updated_at":   now,
        }).Error; err != nil {
            return err
        }

        // 6. UPDATE definition：清 draft，置 published
        if err := db.Model(&definitionModel{}).
            Where("id = ?", target.DefinitionID).
            Updates(map[string]any{
                "draft_version_id":     nil,
                "published_version_id": versionID,
                "updated_at":           now,
            }).Error; err != nil {
            return err
        }

        out, _ = r.getVersionWithinTx(ctx, versionID)
        return nil
    })
    return out, err
}

// builtinNodeTypeRegistry 仅含 builtin 节点的最小 registry，
// 用于 PublishVersion 校验内部，避免和未来的 NodeTypeRegistry 实现耦合。
// 后续 NodeTypeRegistry impl spec 落地后改为注入式。
type fixedRegistry struct{ types map[string]*nodetype.NodeType }

func (r *fixedRegistry) Get(key string) (*nodetype.NodeType, bool) { nt, ok := r.types[key]; return nt, ok }
func (r *fixedRegistry) List(_ nodetype.NodeTypeFilter) []*nodetype.NodeType { return nil }
func (r *fixedRegistry) Invalidate(_ string)         {}
func (r *fixedRegistry) InvalidatePrefix(_ string)   {}

func builtinNodeTypeRegistry() nodetype.NodeTypeRegistry {
    return &fixedRegistry{
        types: map[string]*nodetype.NodeType{
            nodetype.BuiltinStart: {Key: nodetype.BuiltinStart, Ports: []string{domainworkflow.PortDefault}},
            nodetype.BuiltinEnd:   {Key: nodetype.BuiltinEnd, Ports: []string{}},
            nodetype.BuiltinLLM:   {Key: nodetype.BuiltinLLM, Ports: []string{domainworkflow.PortDefault, domainworkflow.PortError}},
            nodetype.BuiltinIf:    {Key: nodetype.BuiltinIf, Ports: []string{nodetype.PortIfTrue, nodetype.PortIfFalse, domainworkflow.PortError}},
        },
    }
}
```

import 段加上：`"github.com/shinya/shineflow/domain/nodetype"`、`"github.com/shinya/shineflow/domain/validator"`。

并把所有 `workflow.WorkflowDSL` 等用 `domainworkflow.WorkflowDSL` 写法（避免和当前包同名冲突）。

- [ ] **Step 4：跑测试通过**

Run：`go test ./infrastructure/storage/workflow/... -run TestPublishVersion -v`
Expected：3 个 PASS

- [ ] **Step 5：跑之前跳过的 SaveVersion 测试**

Run：`go test ./infrastructure/storage/workflow/... -run TestSaveVersion -v`
Expected：4 个 PASS（含 `TestSaveVersion_AppendAfterRelease`）

- [ ] **Step 6：commit**

```bash
git add infrastructure/storage/workflow/
git commit -m "feat(storage/workflow): implement PublishVersion with strict validation"
```

---

### Task 13：DiscardDraft

**Files：**
- Modify：`infrastructure/storage/workflow/repository.go`
- Modify：`infrastructure/storage/workflow/repository_test.go`

- [ ] **Step 1：追加测试**

```go
func TestDiscardDraft_DeletesDraftAndClearsPointer(t *testing.T) {
    ctx := storagetest.Setup(t)
    repo := storageworkflow.NewWorkflowRepository()
    d := newDef(t)
    _ = repo.CreateDefinition(ctx, d)
    v, _ := repo.SaveVersion(ctx, d.ID, domainworkflow.WorkflowDSL{}, 0)

    if err := repo.DiscardDraft(ctx, d.ID); err != nil { t.Fatalf("discard: %v", err) }

    // draft 行硬删
    _, err := repo.GetVersion(ctx, v.ID)
    if !errors.Is(err, domainworkflow.ErrVersionNotFound) {
        t.Fatalf("expected NotFound after discard, got: %v", err)
    }
    // definition.draft_version_id 清掉
    gotD, _ := repo.GetDefinition(ctx, d.ID)
    if gotD.DraftVersionID != nil {
        t.Fatalf("draft_version_id should be nil")
    }
}

func TestDiscardDraft_NoDraft_Idempotent(t *testing.T) {
    ctx := storagetest.Setup(t)
    repo := storageworkflow.NewWorkflowRepository()
    d := newDef(t)
    _ = repo.CreateDefinition(ctx, d)
    if err := repo.DiscardDraft(ctx, d.ID); err != nil {
        t.Fatalf("expected idempotent success, got: %v", err)
    }
}

func TestDiscardDraft_DoesNotTouchRelease(t *testing.T) {
    ctx := storagetest.Setup(t)
    repo := storageworkflow.NewWorkflowRepository()
    d := newDef(t)
    _ = repo.CreateDefinition(ctx, d)
    v1, _ := repo.SaveVersion(ctx, d.ID, minimalValidDSL(), 0)
    _, _ = repo.PublishVersion(ctx, v1.ID, "u_alice")

    if err := repo.DiscardDraft(ctx, d.ID); err != nil {
        t.Fatalf("discard with no draft: %v", err)
    }
    if _, err := repo.GetVersion(ctx, v1.ID); err != nil {
        t.Fatalf("release should still exist: %v", err)
    }
}
```

- [ ] **Step 2：跑测试确认失败**

Run：`go test ./infrastructure/storage/workflow/... -run TestDiscardDraft -v`
Expected：FAIL

- [ ] **Step 3：实现 DiscardDraft**

替换 stub：

```go
func (r *workflowRepo) DiscardDraft(ctx context.Context, definitionID string) error {
    return storage.DBTransaction(ctx, func(ctx context.Context) error {
        db := storage.GetDB(ctx)
        // 硬删 draft 行（Unscoped 走真删，绕过 GORM soft-delete；本表本来就没 deleted_at）
        if err := db.Where("definition_id = ? AND state = ?",
            definitionID, string(domainworkflow.VersionStateDraft)).
            Delete(&versionModel{}).Error; err != nil {
            return err
        }
        // 清 definition.draft_version_id（无 draft 时无副作用）
        return db.Model(&definitionModel{}).
            Where("id = ?", definitionID).
            Updates(map[string]any{
                "draft_version_id": nil,
                "updated_at":       time.Now().UTC(),
            }).Error
    })
}
```

- [ ] **Step 4：测试通过**

Run：`go test ./infrastructure/storage/workflow/... -run TestDiscardDraft -v`
Expected：3 个 PASS

- [ ] **Step 5：跑全 workflow 测试包确认无回退**

Run：`go test ./infrastructure/storage/workflow/...`
Expected：全 PASS

- [ ] **Step 6：commit**

```bash
git add infrastructure/storage/workflow/
git commit -m "feat(storage/workflow): implement DiscardDraft (hard delete)"
```

---

## Phase 2：Run aggregate

### Task 14：run model + codec + mapper 骨架

**Files：**
- Create：`infrastructure/storage/run/model.go`
- Create：`infrastructure/storage/run/codec.go`
- Create：`infrastructure/storage/run/mapper.go`

- [ ] **Step 1：创建包**

Run：`mkdir -p infrastructure/storage/run`

- [ ] **Step 2：写 codec.go（3 个 typed wrapper）**

```go
package run

import (
    "database/sql/driver"
    "fmt"

    domainrun "github.com/shinya/shineflow/domain/run"
    "github.com/shinya/shineflow/infrastructure/util"
)

// runErrorColumn 是 workflow_runs.error 列的 Scanner/Valuer。
type runErrorColumn struct{ inner *domainrun.RunError }

func (c *runErrorColumn) Scan(src any) error {
    if src == nil { c.inner = nil; return nil }
    var s string
    switch v := src.(type) {
    case []byte: s = string(v)
    case string: s = v
    default: return fmt.Errorf("run error: unsupported scan type %T", src)
    }
    var e domainrun.RunError
    if err := util.UnmarshalFromString(s, &e); err != nil { return err }
    c.inner = &e
    return nil
}

func (c runErrorColumn) Value() (driver.Value, error) {
    if c.inner == nil { return nil, nil }
    return util.MarshalToString(*c.inner)
}

// nodeErrorColumn 是 workflow_node_runs.error 列。
type nodeErrorColumn struct{ inner *domainrun.NodeError }

func (c *nodeErrorColumn) Scan(src any) error {
    if src == nil { c.inner = nil; return nil }
    var s string
    switch v := src.(type) {
    case []byte: s = string(v)
    case string: s = v
    default: return fmt.Errorf("node error: unsupported scan type %T", src)
    }
    var e domainrun.NodeError
    if err := util.UnmarshalFromString(s, &e); err != nil { return err }
    c.inner = &e
    return nil
}

func (c nodeErrorColumn) Value() (driver.Value, error) {
    if c.inner == nil { return nil, nil }
    return util.MarshalToString(*c.inner)
}

// externalRefsColumn 是 workflow_node_runs.external_refs 列。
type externalRefsColumn []domainrun.ExternalRef

func (c *externalRefsColumn) Scan(src any) error {
    var s string
    switch v := src.(type) {
    case []byte: s = string(v)
    case string: s = v
    case nil: *c = nil; return nil
    default: return fmt.Errorf("external_refs: unsupported scan type %T", src)
    }
    return util.UnmarshalFromString(s, (*[]domainrun.ExternalRef)(c))
}

func (c externalRefsColumn) Value() (driver.Value, error) {
    return util.MarshalToString([]domainrun.ExternalRef(c))
}
```

注：domain `RunError` / `NodeError` / `ExternalRef` 当前没有 json tag，需要回头给它们加 tag（同 §6.3 风格）。这里先假定加完。

- [ ] **Step 3：给 domain 的 RunError / NodeError / ExternalRef 加 tag**

修改 `domain/run/workflow_run.go` 的 `RunError`：

```go
type RunError struct {
    NodeID    string          `json:"node_id"`
    NodeRunID string          `json:"node_run_id"`
    Code      string          `json:"code"`
    Message   string          `json:"message"`
    Details   json.RawMessage `json:"details,omitempty"`
}
```

修改 `domain/run/node_run.go` 的 `NodeError` / `ExternalRef`：

```go
type NodeError struct {
    Code    string          `json:"code"`
    Message string          `json:"message"`
    Details json.RawMessage `json:"details,omitempty"`
}

type ExternalRef struct {
    Kind string `json:"kind"`
    Ref  string `json:"ref"`
}
```

- [ ] **Step 4：写 model.go**

```go
package run

import (
    "encoding/json"
    "time"
)

type runModel struct {
    ID             string          `gorm:"primaryKey;type:uuid"`
    DefinitionID   string          `gorm:"type:uuid;not null"`
    VersionID      string          `gorm:"type:uuid;not null"`
    TriggerKind    string          `gorm:"not null"`
    TriggerRef     string          `gorm:"not null;default:''"`
    TriggerPayload json.RawMessage `gorm:"type:jsonb;not null;default:'{}'"`
    Status         string          `gorm:"not null"`
    StartedAt      *time.Time
    EndedAt        *time.Time
    Vars           json.RawMessage `gorm:"type:jsonb;not null;default:'{}'"`
    EndNodeID      *string
    Output         json.RawMessage `gorm:"type:jsonb"`
    Error          runErrorColumn  `gorm:"type:jsonb"`
    CreatedBy      string          `gorm:"not null"`
    CreatedAt      time.Time       `gorm:"not null"`
}

func (runModel) TableName() string { return "workflow_runs" }

type nodeRunModel struct {
    ID              string             `gorm:"primaryKey;type:uuid"`
    RunID           string             `gorm:"type:uuid;not null;index"`
    NodeID          string             `gorm:"not null"`
    NodeTypeKey     string             `gorm:"not null"`
    Attempt         int                `gorm:"not null"`
    Status          string             `gorm:"not null"`
    StartedAt       *time.Time
    EndedAt         *time.Time
    ResolvedConfig  json.RawMessage    `gorm:"type:jsonb;not null;default:'{}'"`
    ResolvedInputs  json.RawMessage    `gorm:"type:jsonb;not null;default:'{}'"`
    Output          json.RawMessage    `gorm:"type:jsonb"`
    FiredPort       string             `gorm:"not null;default:''"`
    FallbackApplied bool               `gorm:"not null;default:false"`
    Error           nodeErrorColumn    `gorm:"type:jsonb"`
    ExternalRefs    externalRefsColumn `gorm:"type:jsonb;not null;default:'[]'"`
}

func (nodeRunModel) TableName() string { return "workflow_node_runs" }
```

- [ ] **Step 5：写 mapper.go**

```go
package run

import (
    domainrun "github.com/shinya/shineflow/domain/run"
)

func toRun(m *runModel) *domainrun.WorkflowRun {
    return &domainrun.WorkflowRun{
        ID:             m.ID,
        DefinitionID:   m.DefinitionID,
        VersionID:      m.VersionID,
        TriggerKind:    domainrun.TriggerKind(m.TriggerKind),
        TriggerRef:     m.TriggerRef,
        TriggerPayload: m.TriggerPayload,
        Status:         domainrun.RunStatus(m.Status),
        StartedAt:      m.StartedAt,
        EndedAt:        m.EndedAt,
        Vars:           m.Vars,
        EndNodeID:      m.EndNodeID,
        Output:         m.Output,
        Error:          m.Error.inner,
        CreatedBy:      m.CreatedBy,
        CreatedAt:      m.CreatedAt,
    }
}

func toRunModel(r *domainrun.WorkflowRun) *runModel {
    return &runModel{
        ID:             r.ID,
        DefinitionID:   r.DefinitionID,
        VersionID:      r.VersionID,
        TriggerKind:    string(r.TriggerKind),
        TriggerRef:     r.TriggerRef,
        TriggerPayload: r.TriggerPayload,
        Status:         string(r.Status),
        StartedAt:      r.StartedAt,
        EndedAt:        r.EndedAt,
        Vars:           r.Vars,
        EndNodeID:      r.EndNodeID,
        Output:         r.Output,
        Error:          runErrorColumn{inner: r.Error},
        CreatedBy:      r.CreatedBy,
        CreatedAt:      r.CreatedAt,
    }
}

func toNodeRun(m *nodeRunModel) *domainrun.NodeRun {
    return &domainrun.NodeRun{
        ID:              m.ID,
        RunID:           m.RunID,
        NodeID:          m.NodeID,
        NodeTypeKey:     m.NodeTypeKey,
        Attempt:         m.Attempt,
        Status:          domainrun.NodeRunStatus(m.Status),
        StartedAt:       m.StartedAt,
        EndedAt:         m.EndedAt,
        ResolvedConfig:  m.ResolvedConfig,
        ResolvedInputs:  m.ResolvedInputs,
        Output:          m.Output,
        FiredPort:       m.FiredPort,
        FallbackApplied: m.FallbackApplied,
        Error:           m.Error.inner,
        ExternalRefs:    []domainrun.ExternalRef(m.ExternalRefs),
    }
}

func toNodeRunModel(n *domainrun.NodeRun) *nodeRunModel {
    return &nodeRunModel{
        ID:              n.ID,
        RunID:           n.RunID,
        NodeID:          n.NodeID,
        NodeTypeKey:     n.NodeTypeKey,
        Attempt:         n.Attempt,
        Status:          string(n.Status),
        StartedAt:       n.StartedAt,
        EndedAt:         n.EndedAt,
        ResolvedConfig:  n.ResolvedConfig,
        ResolvedInputs:  n.ResolvedInputs,
        Output:          n.Output,
        FiredPort:       n.FiredPort,
        FallbackApplied: n.FallbackApplied,
        Error:           nodeErrorColumn{inner: n.Error},
        ExternalRefs:    externalRefsColumn(n.ExternalRefs),
    }
}
```

- [ ] **Step 6：build 验证**

Run：`go build ./infrastructure/storage/run/`
Expected：无输出

- [ ] **Step 7：commit**

```bash
git add domain/run/ infrastructure/storage/run/
git commit -m "feat(storage/run): add model, codec, mapper; add json tags to run domain types"
```

---

### Task 15：Run Create / Get / List

**Files：**
- Create：`infrastructure/storage/run/repository.go`
- Create：`infrastructure/storage/run/repository_test.go`

- [ ] **Step 1：写测试**

```go
package run_test

import (
    "errors"
    "testing"
    "time"

    "github.com/google/uuid"

    domainrun "github.com/shinya/shineflow/domain/run"
    domainworkflow "github.com/shinya/shineflow/domain/workflow"
    "github.com/shinya/shineflow/infrastructure/storage/storagetest"
    storagerun "github.com/shinya/shineflow/infrastructure/storage/run"
    storageworkflow "github.com/shinya/shineflow/infrastructure/storage/workflow"
)

// seedDefAndVersion 创建一个 release version，返回 (defID, versionID)。
// run 必须挂在已 release 的 version 上（FK），所以 run 测试都靠这个 helper。
func seedDefAndVersion(t *testing.T, ctx interface{}) (string, string) {
    // 略：在每个 test 内 inline。这里给一个公用版本。
    panic("inlined per test")
}

func newRun(t *testing.T, defID, versionID string) *domainrun.WorkflowRun {
    t.Helper()
    return &domainrun.WorkflowRun{
        ID:           uuid.NewString(),
        DefinitionID: defID,
        VersionID:    versionID,
        TriggerKind:  domainrun.TriggerKindManual,
        Status:       domainrun.RunStatusPending,
        CreatedBy:    "u_alice",
        CreatedAt:    time.Now().UTC(),
    }
}

func TestRun_CreateAndGet(t *testing.T) {
    ctx := storagetest.Setup(t)
    wfRepo := storageworkflow.NewWorkflowRepository()
    runRepo := storagerun.NewWorkflowRunRepository()

    d := &domainworkflow.WorkflowDefinition{
        ID: uuid.NewString(), Name: "d", CreatedBy: "u",
        CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
    }
    _ = wfRepo.CreateDefinition(ctx, d)
    v, _ := wfRepo.SaveVersion(ctx, d.ID, domainworkflow.WorkflowDSL{
        Nodes: []domainworkflow.Node{
            {ID: "n_start", TypeKey: "builtin.start", TypeVer: "1"},
            {ID: "n_end", TypeKey: "builtin.end", TypeVer: "1"},
        },
        Edges: []domainworkflow.Edge{
            {ID: "e1", From: "n_start", FromPort: domainworkflow.PortDefault, To: "n_end"},
        },
    }, 0)
    _, _ = wfRepo.PublishVersion(ctx, v.ID, "u")

    r := newRun(t, d.ID, v.ID)
    if err := runRepo.Create(ctx, r); err != nil { t.Fatalf("create: %v", err) }

    got, err := runRepo.Get(ctx, r.ID)
    if err != nil { t.Fatalf("get: %v", err) }
    if got.Status != domainrun.RunStatusPending {
        t.Fatalf("status: %s", got.Status)
    }
}

func TestRun_GetNotFound(t *testing.T) {
    ctx := storagetest.Setup(t)
    runRepo := storagerun.NewWorkflowRunRepository()
    _, err := runRepo.Get(ctx, uuid.NewString())
    if !errors.Is(err, domainrun.ErrRunNotFound) {
        t.Fatalf("expected ErrRunNotFound, got: %v", err)
    }
}

func TestRun_List_FilterByDefinition(t *testing.T) {
    ctx := storagetest.Setup(t)
    wfRepo := storageworkflow.NewWorkflowRepository()
    runRepo := storagerun.NewWorkflowRunRepository()

    d := &domainworkflow.WorkflowDefinition{
        ID: uuid.NewString(), Name: "d", CreatedBy: "u",
        CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
    }
    _ = wfRepo.CreateDefinition(ctx, d)
    v, _ := wfRepo.SaveVersion(ctx, d.ID, domainworkflow.WorkflowDSL{
        Nodes: []domainworkflow.Node{
            {ID: "n_start", TypeKey: "builtin.start", TypeVer: "1"},
            {ID: "n_end", TypeKey: "builtin.end", TypeVer: "1"},
        },
        Edges: []domainworkflow.Edge{
            {ID: "e1", From: "n_start", FromPort: domainworkflow.PortDefault, To: "n_end"},
        },
    }, 0)
    _, _ = wfRepo.PublishVersion(ctx, v.ID, "u")

    _ = runRepo.Create(ctx, newRun(t, d.ID, v.ID))
    _ = runRepo.Create(ctx, newRun(t, d.ID, v.ID))

    list, err := runRepo.List(ctx, domainrun.RunFilter{DefinitionID: d.ID})
    if err != nil { t.Fatal(err) }
    if len(list) != 2 { t.Fatalf("expected 2, got %d", len(list)) }
}
```

- [ ] **Step 2：跑测试确认失败**

Run：`go test ./infrastructure/storage/run/... -run TestRun_ -v`
Expected：FAIL（`NewWorkflowRunRepository` 不存在）

- [ ] **Step 3：实现 repository.go 的 Create / Get / List + 全 stub**

```go
package run

import (
    "context"
    "encoding/json"
    "errors"
    "time"

    "gorm.io/gorm"

    domainrun "github.com/shinya/shineflow/domain/run"
    "github.com/shinya/shineflow/infrastructure/storage"
)

type runRepo struct{}

func NewWorkflowRunRepository() domainrun.WorkflowRunRepository {
    return &runRepo{}
}

func (r *runRepo) Create(ctx context.Context, run *domainrun.WorkflowRun) error {
    return storage.GetDB(ctx).Create(toRunModel(run)).Error
}

func (r *runRepo) Get(ctx context.Context, id string) (*domainrun.WorkflowRun, error) {
    var m runModel
    err := storage.GetDB(ctx).Where("id = ?", id).Take(&m).Error
    if errors.Is(err, gorm.ErrRecordNotFound) {
        return nil, domainrun.ErrRunNotFound
    }
    if err != nil { return nil, err }
    return toRun(&m), nil
}

func (r *runRepo) List(ctx context.Context, filter domainrun.RunFilter) ([]*domainrun.WorkflowRun, error) {
    q := storage.GetDB(ctx).Model(&runModel{})
    if filter.DefinitionID != "" { q = q.Where("definition_id = ?", filter.DefinitionID) }
    if filter.VersionID != "" { q = q.Where("version_id = ?", filter.VersionID) }
    if filter.Status != "" { q = q.Where("status = ?", string(filter.Status)) }
    if filter.TriggerKind != "" { q = q.Where("trigger_kind = ?", string(filter.TriggerKind)) }
    if filter.StartedFrom != nil { q = q.Where("started_at >= ?", *filter.StartedFrom) }
    if filter.StartedTo != nil { q = q.Where("started_at <= ?", *filter.StartedTo) }
    if filter.Limit > 0 { q = q.Limit(filter.Limit) }
    if filter.Offset > 0 { q = q.Offset(filter.Offset) }
    q = q.Order("created_at DESC")

    var ms []runModel
    if err := q.Find(&ms).Error; err != nil { return nil, err }
    out := make([]*domainrun.WorkflowRun, 0, len(ms))
    for i := range ms {
        out = append(out, toRun(&ms[i]))
    }
    return out, nil
}

// 占位：剩余方法在后续 task 实现
func (r *runRepo) UpdateStatus(ctx context.Context, id string, status domainrun.RunStatus, opts ...domainrun.RunUpdateOpt) error {
    return errors.New("not implemented")
}
func (r *runRepo) SaveEndResult(ctx context.Context, id, endNodeID string, output json.RawMessage) error {
    return errors.New("not implemented")
}
func (r *runRepo) SaveVars(ctx context.Context, id string, vars json.RawMessage) error {
    return errors.New("not implemented")
}
func (r *runRepo) SaveError(ctx context.Context, id string, e domainrun.RunError) error {
    return errors.New("not implemented")
}
func (r *runRepo) AppendNodeRun(ctx context.Context, runID string, nr *domainrun.NodeRun) error {
    return errors.New("not implemented")
}
func (r *runRepo) UpdateNodeRunStatus(ctx context.Context, runID, nodeRunID string, status domainrun.NodeRunStatus, opts ...domainrun.NodeRunUpdateOpt) error {
    return errors.New("not implemented")
}
func (r *runRepo) SaveNodeRunOutput(ctx context.Context, runID, nodeRunID string, output json.RawMessage, firedPort string) error {
    return errors.New("not implemented")
}
func (r *runRepo) GetNodeRun(ctx context.Context, runID, nodeRunID string) (*domainrun.NodeRun, error) {
    return nil, errors.New("not implemented")
}
func (r *runRepo) ListNodeRuns(ctx context.Context, runID string) ([]*domainrun.NodeRun, error) {
    return nil, errors.New("not implemented")
}
func (r *runRepo) GetLatestNodeRun(ctx context.Context, runID, nodeID string) (*domainrun.NodeRun, error) {
    return nil, errors.New("not implemented")
}

var _ = time.Now // keep time import for later tasks
```

- [ ] **Step 4：测试通过**

Run：`go test ./infrastructure/storage/run/... -run TestRun_ -v`
Expected：3 个 PASS

- [ ] **Step 5：commit**

```bash
git add infrastructure/storage/run/
git commit -m "feat(storage/run): implement Run Create/Get/List"
```

---

### Task 16：UpdateStatus（state-machine 编进 WHERE）

**Files：**
- Modify：`infrastructure/storage/run/repository.go`
- Modify：`infrastructure/storage/run/repository_test.go`

- [ ] **Step 1：追加测试**

```go
func TestRun_UpdateStatus_PendingToRunning(t *testing.T) {
    ctx := storagetest.Setup(t)
    wfRepo := storageworkflow.NewWorkflowRepository()
    runRepo := storagerun.NewWorkflowRunRepository()

    d, v := seedReleasedVersion(t, ctx, wfRepo)
    r := newRun(t, d.ID, v.ID)
    _ = runRepo.Create(ctx, r)

    now := time.Now().UTC()
    err := runRepo.UpdateStatus(ctx, r.ID, domainrun.RunStatusRunning, domainrun.WithRunStartedAt(now))
    if err != nil { t.Fatalf("update: %v", err) }

    got, _ := runRepo.Get(ctx, r.ID)
    if got.Status != domainrun.RunStatusRunning { t.Fatalf("status: %s", got.Status) }
    if got.StartedAt == nil { t.Fatal("started_at should be set") }
}

func TestRun_UpdateStatus_TerminalRollback_Rejected(t *testing.T) {
    ctx := storagetest.Setup(t)
    wfRepo := storageworkflow.NewWorkflowRepository()
    runRepo := storagerun.NewWorkflowRunRepository()
    d, v := seedReleasedVersion(t, ctx, wfRepo)
    r := newRun(t, d.ID, v.ID)
    _ = runRepo.Create(ctx, r)
    _ = runRepo.UpdateStatus(ctx, r.ID, domainrun.RunStatusRunning)
    _ = runRepo.UpdateStatus(ctx, r.ID, domainrun.RunStatusSuccess)

    err := runRepo.UpdateStatus(ctx, r.ID, domainrun.RunStatusRunning)
    if !errors.Is(err, domainrun.ErrIllegalStatusTransition) {
        t.Fatalf("expected ErrIllegalStatusTransition, got: %v", err)
    }
}

// seedReleasedVersion 工厂：把 def + release v 一次造好返回。
func seedReleasedVersion(t *testing.T, ctx interface{}, wf domainworkflow.WorkflowRepository) (
    *domainworkflow.WorkflowDefinition, *domainworkflow.WorkflowVersion,
) {
    t.Helper()
    d := &domainworkflow.WorkflowDefinition{
        ID: uuid.NewString(), Name: "d", CreatedBy: "u",
        CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
    }
    _ = wf.CreateDefinition(ctx.(context.Context), d)
    v, _ := wf.SaveVersion(ctx.(context.Context), d.ID, domainworkflow.WorkflowDSL{
        Nodes: []domainworkflow.Node{
            {ID: "n_start", TypeKey: "builtin.start", TypeVer: "1"},
            {ID: "n_end", TypeKey: "builtin.end", TypeVer: "1"},
        },
        Edges: []domainworkflow.Edge{
            {ID: "e1", From: "n_start", FromPort: domainworkflow.PortDefault, To: "n_end"},
        },
    }, 0)
    pub, _ := wf.PublishVersion(ctx.(context.Context), v.ID, "u")
    return d, pub
}
```

注：`seedReleasedVersion` 在 `_test.go` 顶部 `import "context"`。

- [ ] **Step 2：跑测试确认失败**

Run：`go test ./infrastructure/storage/run/... -run TestRun_UpdateStatus -v`
Expected：FAIL

- [ ] **Step 3：实现 UpdateStatus（状态机进 WHERE）**

替换 `UpdateStatus` stub：

```go
// 各状态允许的 prev 集合（与 RunStatus.CanTransitionTo 一致）
var runAllowedPrev = map[domainrun.RunStatus][]domainrun.RunStatus{
    domainrun.RunStatusRunning:   {domainrun.RunStatusPending},
    domainrun.RunStatusSuccess:   {domainrun.RunStatusRunning},
    domainrun.RunStatusFailed:    {domainrun.RunStatusPending, domainrun.RunStatusRunning},
    domainrun.RunStatusCancelled: {domainrun.RunStatusPending, domainrun.RunStatusRunning},
}

func (r *runRepo) UpdateStatus(
    ctx context.Context, id string, status domainrun.RunStatus, opts ...domainrun.RunUpdateOpt,
) error {
    prev, ok := runAllowedPrev[status]
    if !ok {
        return domainrun.ErrIllegalStatusTransition
    }
    var u domainrun.RunUpdate
    for _, opt := range opts { opt(&u) }

    sets := map[string]any{"status": string(status)}
    if u.StartedAt != nil { sets["started_at"] = *u.StartedAt }
    if u.EndedAt != nil { sets["ended_at"] = *u.EndedAt }

    prevStrs := make([]string, len(prev))
    for i, p := range prev { prevStrs[i] = string(p) }

    res := storage.GetDB(ctx).Model(&runModel{}).
        Where("id = ? AND status IN ?", id, prevStrs).
        Updates(sets)
    if res.Error != nil { return res.Error }
    if res.RowsAffected == 0 {
        // 区分 not found vs 状态非法
        var cnt int64
        storage.GetDB(ctx).Model(&runModel{}).Where("id = ?", id).Count(&cnt)
        if cnt == 0 { return domainrun.ErrRunNotFound }
        return domainrun.ErrIllegalStatusTransition
    }
    return nil
}
```

- [ ] **Step 4：测试通过**

Run：`go test ./infrastructure/storage/run/... -run TestRun_UpdateStatus -v`
Expected：2 个 PASS

- [ ] **Step 5：commit**

```bash
git add infrastructure/storage/run/
git commit -m "feat(storage/run): implement UpdateStatus with state-machine WHERE"
```

---

### Task 17：SaveEndResult / SaveVars / SaveError

**Files：**
- Modify：`infrastructure/storage/run/repository.go`
- Modify：`infrastructure/storage/run/repository_test.go`

- [ ] **Step 1：追加测试**

```go
func TestRun_SaveEndResult(t *testing.T) {
    ctx := storagetest.Setup(t)
    wfRepo := storageworkflow.NewWorkflowRepository()
    runRepo := storagerun.NewWorkflowRunRepository()
    d, v := seedReleasedVersion(t, ctx, wfRepo)
    r := newRun(t, d.ID, v.ID)
    _ = runRepo.Create(ctx, r)

    out := json.RawMessage(`{"answer":42}`)
    if err := runRepo.SaveEndResult(ctx, r.ID, "n_end", out); err != nil {
        t.Fatalf("save: %v", err)
    }
    got, _ := runRepo.Get(ctx, r.ID)
    if got.EndNodeID == nil || *got.EndNodeID != "n_end" {
        t.Fatalf("end_node_id: %v", got.EndNodeID)
    }
}

func TestRun_SaveVars(t *testing.T) {
    ctx := storagetest.Setup(t)
    wfRepo := storageworkflow.NewWorkflowRepository()
    runRepo := storagerun.NewWorkflowRunRepository()
    d, v := seedReleasedVersion(t, ctx, wfRepo)
    r := newRun(t, d.ID, v.ID)
    _ = runRepo.Create(ctx, r)

    vars := json.RawMessage(`{"x":1}`)
    if err := runRepo.SaveVars(ctx, r.ID, vars); err != nil { t.Fatal(err) }
    got, _ := runRepo.Get(ctx, r.ID)
    if string(got.Vars) != `{"x": 1}` && string(got.Vars) != `{"x":1}` {
        t.Fatalf("vars: %s", string(got.Vars))
    }
}

func TestRun_SaveError(t *testing.T) {
    ctx := storagetest.Setup(t)
    wfRepo := storageworkflow.NewWorkflowRepository()
    runRepo := storagerun.NewWorkflowRunRepository()
    d, v := seedReleasedVersion(t, ctx, wfRepo)
    r := newRun(t, d.ID, v.ID)
    _ = runRepo.Create(ctx, r)

    e := domainrun.RunError{
        NodeID: "n_llm", Code: domainrun.RunErrCodeNodeExecFailed, Message: "boom",
    }
    if err := runRepo.SaveError(ctx, r.ID, e); err != nil { t.Fatal(err) }
    got, _ := runRepo.Get(ctx, r.ID)
    if got.Error == nil || got.Error.Code != domainrun.RunErrCodeNodeExecFailed {
        t.Fatalf("error: %+v", got.Error)
    }
}
```

需要顶部 `import "encoding/json"`。

- [ ] **Step 2：跑测试确认失败**

Run：`go test ./infrastructure/storage/run/... -run "TestRun_SaveEnd|TestRun_SaveVars|TestRun_SaveError" -v`
Expected：FAIL

- [ ] **Step 3：实现三个方法**

替换三个 stub：

```go
func (r *runRepo) SaveEndResult(
    ctx context.Context, id, endNodeID string, output json.RawMessage,
) error {
    res := storage.GetDB(ctx).Model(&runModel{}).Where("id = ?", id).
        Updates(map[string]any{
            "end_node_id": endNodeID,
            "output":      output,
        })
    if res.Error != nil { return res.Error }
    if res.RowsAffected == 0 { return domainrun.ErrRunNotFound }
    return nil
}

func (r *runRepo) SaveVars(ctx context.Context, id string, vars json.RawMessage) error {
    res := storage.GetDB(ctx).Model(&runModel{}).Where("id = ?", id).
        Updates(map[string]any{"vars": vars})
    if res.Error != nil { return res.Error }
    if res.RowsAffected == 0 { return domainrun.ErrRunNotFound }
    return nil
}

func (r *runRepo) SaveError(ctx context.Context, id string, e domainrun.RunError) error {
    res := storage.GetDB(ctx).Model(&runModel{}).Where("id = ?", id).
        Updates(map[string]any{
            "error": runErrorColumn{inner: &e},
        })
    if res.Error != nil { return res.Error }
    if res.RowsAffected == 0 { return domainrun.ErrRunNotFound }
    return nil
}
```

- [ ] **Step 4：测试通过**

Run：`go test ./infrastructure/storage/run/... -run "TestRun_SaveEnd|TestRun_SaveVars|TestRun_SaveError" -v`
Expected：3 个 PASS

- [ ] **Step 5：commit**

```bash
git add infrastructure/storage/run/
git commit -m "feat(storage/run): implement SaveEndResult/SaveVars/SaveError"
```

---

### Task 18：AppendNodeRun

**Files：**
- Modify：`infrastructure/storage/run/repository.go`
- Modify：`infrastructure/storage/run/repository_test.go`

- [ ] **Step 1：追加测试**

```go
func newNodeRun(t *testing.T, runID string, attempt int) *domainrun.NodeRun {
    t.Helper()
    return &domainrun.NodeRun{
        ID:          uuid.NewString(),
        RunID:       runID,
        NodeID:      "n_llm",
        NodeTypeKey: "builtin.llm",
        Attempt:     attempt,
        Status:      domainrun.NodeRunStatusPending,
    }
}

func TestNodeRun_Append(t *testing.T) {
    ctx := storagetest.Setup(t)
    wfRepo := storageworkflow.NewWorkflowRepository()
    runRepo := storagerun.NewWorkflowRunRepository()
    d, v := seedReleasedVersion(t, ctx, wfRepo)
    r := newRun(t, d.ID, v.ID)
    _ = runRepo.Create(ctx, r)

    nr := newNodeRun(t, r.ID, 1)
    if err := runRepo.AppendNodeRun(ctx, r.ID, nr); err != nil { t.Fatalf("append: %v", err) }

    got, err := runRepo.GetNodeRun(ctx, r.ID, nr.ID)
    if err != nil { t.Fatalf("get: %v", err) }
    if got.NodeID != "n_llm" || got.Attempt != 1 {
        t.Fatalf("got: %+v", got)
    }
}

func TestNodeRun_Append_DuplicateAttempt(t *testing.T) {
    ctx := storagetest.Setup(t)
    wfRepo := storageworkflow.NewWorkflowRepository()
    runRepo := storagerun.NewWorkflowRunRepository()
    d, v := seedReleasedVersion(t, ctx, wfRepo)
    r := newRun(t, d.ID, v.ID)
    _ = runRepo.Create(ctx, r)

    nr1 := newNodeRun(t, r.ID, 1)
    _ = runRepo.AppendNodeRun(ctx, r.ID, nr1)

    nr2 := newNodeRun(t, r.ID, 1) // 同 attempt 应失败 unique 约束
    err := runRepo.AppendNodeRun(ctx, r.ID, nr2)
    if err == nil { t.Fatal("expected unique violation, got nil") }
}
```

- [ ] **Step 2：跑测试确认失败**

Run：`go test ./infrastructure/storage/run/... -run TestNodeRun_Append -v`
Expected：FAIL

- [ ] **Step 3：实现 AppendNodeRun + GetNodeRun（GetNodeRun 顺手实现，下一 task 还会再覆盖）**

替换两个 stub：

```go
func (r *runRepo) AppendNodeRun(ctx context.Context, runID string, nr *domainrun.NodeRun) error {
    nr.RunID = runID
    return storage.GetDB(ctx).Create(toNodeRunModel(nr)).Error
}

func (r *runRepo) GetNodeRun(ctx context.Context, runID, nodeRunID string) (*domainrun.NodeRun, error) {
    var m nodeRunModel
    err := storage.GetDB(ctx).Where("run_id = ? AND id = ?", runID, nodeRunID).Take(&m).Error
    if errors.Is(err, gorm.ErrRecordNotFound) {
        return nil, domainrun.ErrNodeRunNotFound
    }
    if err != nil { return nil, err }
    return toNodeRun(&m), nil
}
```

- [ ] **Step 4：测试通过**

Run：`go test ./infrastructure/storage/run/... -run TestNodeRun_Append -v`
Expected：2 个 PASS

- [ ] **Step 5：commit**

```bash
git add infrastructure/storage/run/
git commit -m "feat(storage/run): implement AppendNodeRun and GetNodeRun"
```

---

### Task 19：UpdateNodeRunStatus + SaveNodeRunOutput

**Files：**
- Modify：`infrastructure/storage/run/repository.go`
- Modify：`infrastructure/storage/run/repository_test.go`

- [ ] **Step 1：追加测试**

```go
func TestNodeRun_UpdateStatus(t *testing.T) {
    ctx := storagetest.Setup(t)
    wfRepo := storageworkflow.NewWorkflowRepository()
    runRepo := storagerun.NewWorkflowRunRepository()
    d, v := seedReleasedVersion(t, ctx, wfRepo)
    r := newRun(t, d.ID, v.ID); _ = runRepo.Create(ctx, r)
    nr := newNodeRun(t, r.ID, 1); _ = runRepo.AppendNodeRun(ctx, r.ID, nr)

    err := runRepo.UpdateNodeRunStatus(ctx, r.ID, nr.ID, domainrun.NodeRunStatusRunning,
        domainrun.WithNodeRunStartedAt(time.Now().UTC()))
    if err != nil { t.Fatalf("update: %v", err) }

    got, _ := runRepo.GetNodeRun(ctx, r.ID, nr.ID)
    if got.Status != domainrun.NodeRunStatusRunning { t.Fatalf("status: %s", got.Status) }
}

func TestNodeRun_UpdateStatus_Illegal(t *testing.T) {
    ctx := storagetest.Setup(t)
    wfRepo := storageworkflow.NewWorkflowRepository()
    runRepo := storagerun.NewWorkflowRunRepository()
    d, v := seedReleasedVersion(t, ctx, wfRepo)
    r := newRun(t, d.ID, v.ID); _ = runRepo.Create(ctx, r)
    nr := newNodeRun(t, r.ID, 1); _ = runRepo.AppendNodeRun(ctx, r.ID, nr)
    _ = runRepo.UpdateNodeRunStatus(ctx, r.ID, nr.ID, domainrun.NodeRunStatusRunning)
    _ = runRepo.UpdateNodeRunStatus(ctx, r.ID, nr.ID, domainrun.NodeRunStatusSuccess)

    err := runRepo.UpdateNodeRunStatus(ctx, r.ID, nr.ID, domainrun.NodeRunStatusRunning)
    if !errors.Is(err, domainrun.ErrIllegalStatusTransition) {
        t.Fatalf("expected illegal: %v", err)
    }
}

func TestNodeRun_SaveOutput(t *testing.T) {
    ctx := storagetest.Setup(t)
    wfRepo := storageworkflow.NewWorkflowRepository()
    runRepo := storagerun.NewWorkflowRunRepository()
    d, v := seedReleasedVersion(t, ctx, wfRepo)
    r := newRun(t, d.ID, v.ID); _ = runRepo.Create(ctx, r)
    nr := newNodeRun(t, r.ID, 1); _ = runRepo.AppendNodeRun(ctx, r.ID, nr)

    out := json.RawMessage(`{"answer":42}`)
    if err := runRepo.SaveNodeRunOutput(ctx, r.ID, nr.ID, out, domainworkflow.PortDefault); err != nil {
        t.Fatal(err)
    }
    got, _ := runRepo.GetNodeRun(ctx, r.ID, nr.ID)
    if got.FiredPort != domainworkflow.PortDefault {
        t.Fatalf("fired_port: %s", got.FiredPort)
    }
}
```

- [ ] **Step 2：跑测试确认失败**

Run：`go test ./infrastructure/storage/run/... -run "TestNodeRun_UpdateStatus|TestNodeRun_SaveOutput" -v`
Expected：FAIL

- [ ] **Step 3：实现两个方法**

```go
var nodeRunAllowedPrev = map[domainrun.NodeRunStatus][]domainrun.NodeRunStatus{
    domainrun.NodeRunStatusRunning: {domainrun.NodeRunStatusPending},
    domainrun.NodeRunStatusSkipped: {domainrun.NodeRunStatusPending},
    domainrun.NodeRunStatusSuccess: {domainrun.NodeRunStatusRunning},
    domainrun.NodeRunStatusFailed:  {domainrun.NodeRunStatusRunning},
}

func (r *runRepo) UpdateNodeRunStatus(
    ctx context.Context, runID, nodeRunID string, status domainrun.NodeRunStatus,
    opts ...domainrun.NodeRunUpdateOpt,
) error {
    prev, ok := nodeRunAllowedPrev[status]
    if !ok { return domainrun.ErrIllegalStatusTransition }
    var u domainrun.NodeRunUpdate
    for _, opt := range opts { opt(&u) }

    sets := map[string]any{"status": string(status)}
    if u.StartedAt != nil { sets["started_at"] = *u.StartedAt }
    if u.EndedAt != nil { sets["ended_at"] = *u.EndedAt }
    if u.Error != nil { sets["error"] = nodeErrorColumn{inner: u.Error} }
    if u.FallbackApplied != nil { sets["fallback_applied"] = *u.FallbackApplied }
    if u.ExternalRefs != nil { sets["external_refs"] = externalRefsColumn(u.ExternalRefs) }

    prevStrs := make([]string, len(prev))
    for i, p := range prev { prevStrs[i] = string(p) }

    res := storage.GetDB(ctx).Model(&nodeRunModel{}).
        Where("run_id = ? AND id = ? AND status IN ?", runID, nodeRunID, prevStrs).
        Updates(sets)
    if res.Error != nil { return res.Error }
    if res.RowsAffected == 0 {
        var cnt int64
        storage.GetDB(ctx).Model(&nodeRunModel{}).
            Where("run_id = ? AND id = ?", runID, nodeRunID).Count(&cnt)
        if cnt == 0 { return domainrun.ErrNodeRunNotFound }
        return domainrun.ErrIllegalStatusTransition
    }
    return nil
}

func (r *runRepo) SaveNodeRunOutput(
    ctx context.Context, runID, nodeRunID string, output json.RawMessage, firedPort string,
) error {
    res := storage.GetDB(ctx).Model(&nodeRunModel{}).
        Where("run_id = ? AND id = ?", runID, nodeRunID).
        Updates(map[string]any{
            "output":     output,
            "fired_port": firedPort,
        })
    if res.Error != nil { return res.Error }
    if res.RowsAffected == 0 { return domainrun.ErrNodeRunNotFound }
    return nil
}
```

- [ ] **Step 4：测试通过**

Run：`go test ./infrastructure/storage/run/... -run "TestNodeRun_UpdateStatus|TestNodeRun_SaveOutput" -v`
Expected：3 个 PASS

- [ ] **Step 5：commit**

```bash
git add infrastructure/storage/run/
git commit -m "feat(storage/run): implement UpdateNodeRunStatus and SaveNodeRunOutput"
```

---

### Task 20：ListNodeRuns + GetLatestNodeRun

**Files：**
- Modify：`infrastructure/storage/run/repository.go`
- Modify：`infrastructure/storage/run/repository_test.go`

- [ ] **Step 1：追加测试**

```go
func TestNodeRun_List(t *testing.T) {
    ctx := storagetest.Setup(t)
    wfRepo := storageworkflow.NewWorkflowRepository()
    runRepo := storagerun.NewWorkflowRunRepository()
    d, v := seedReleasedVersion(t, ctx, wfRepo)
    r := newRun(t, d.ID, v.ID); _ = runRepo.Create(ctx, r)
    _ = runRepo.AppendNodeRun(ctx, r.ID, newNodeRun(t, r.ID, 1))
    _ = runRepo.AppendNodeRun(ctx, r.ID, newNodeRun(t, r.ID, 2))

    list, err := runRepo.ListNodeRuns(ctx, r.ID)
    if err != nil { t.Fatal(err) }
    if len(list) != 2 { t.Fatalf("got %d", len(list)) }
}

func TestNodeRun_GetLatest(t *testing.T) {
    ctx := storagetest.Setup(t)
    wfRepo := storageworkflow.NewWorkflowRepository()
    runRepo := storagerun.NewWorkflowRunRepository()
    d, v := seedReleasedVersion(t, ctx, wfRepo)
    r := newRun(t, d.ID, v.ID); _ = runRepo.Create(ctx, r)

    nr1 := newNodeRun(t, r.ID, 1); _ = runRepo.AppendNodeRun(ctx, r.ID, nr1)
    nr2 := newNodeRun(t, r.ID, 2); _ = runRepo.AppendNodeRun(ctx, r.ID, nr2)
    nr3 := newNodeRun(t, r.ID, 3); _ = runRepo.AppendNodeRun(ctx, r.ID, nr3)

    got, err := runRepo.GetLatestNodeRun(ctx, r.ID, "n_llm")
    if err != nil { t.Fatal(err) }
    if got.ID != nr3.ID { t.Fatalf("expected latest %s, got %s", nr3.ID, got.ID) }
}
```

- [ ] **Step 2：跑测试确认失败**

Run：`go test ./infrastructure/storage/run/... -run "TestNodeRun_List|TestNodeRun_GetLatest" -v`
Expected：FAIL

- [ ] **Step 3：实现两个方法**

```go
func (r *runRepo) ListNodeRuns(ctx context.Context, runID string) ([]*domainrun.NodeRun, error) {
    var ms []nodeRunModel
    err := storage.GetDB(ctx).Where("run_id = ?", runID).
        Order("attempt ASC").Find(&ms).Error
    if err != nil { return nil, err }
    out := make([]*domainrun.NodeRun, 0, len(ms))
    for i := range ms { out = append(out, toNodeRun(&ms[i])) }
    return out, nil
}

func (r *runRepo) GetLatestNodeRun(
    ctx context.Context, runID, nodeID string,
) (*domainrun.NodeRun, error) {
    var m nodeRunModel
    err := storage.GetDB(ctx).
        Where("run_id = ? AND node_id = ?", runID, nodeID).
        Order("attempt DESC").Limit(1).Take(&m).Error
    if errors.Is(err, gorm.ErrRecordNotFound) {
        return nil, domainrun.ErrNodeRunNotFound
    }
    if err != nil { return nil, err }
    return toNodeRun(&m), nil
}
```

- [ ] **Step 4：测试通过 + 跑全 run 包**

Run：`go test ./infrastructure/storage/run/...`
Expected：全 PASS

- [ ] **Step 5：commit**

```bash
git add infrastructure/storage/run/
git commit -m "feat(storage/run): implement ListNodeRuns and GetLatestNodeRun"
```

---

## Phase 3：Cron aggregate

### Task 21：cron model + mapper

**Files：**
- Create：`infrastructure/storage/cron/model.go`
- Create：`infrastructure/storage/cron/mapper.go`

- [ ] **Step 1：创建包 + 写 model**

Run：`mkdir -p infrastructure/storage/cron`

写 `model.go`：

```go
package cron

import (
    "encoding/json"
    "time"

    "gorm.io/gorm"
)

type cronJobModel struct {
    ID           string          `gorm:"primaryKey;type:uuid"`
    DefinitionID string          `gorm:"type:uuid;not null"`
    Name         string          `gorm:"not null"`
    Description  string          `gorm:"not null;default:''"`
    Expression   string          `gorm:"not null"`
    Timezone     string          `gorm:"not null"`
    Payload      json.RawMessage `gorm:"type:jsonb;not null;default:'{}'"`
    Enabled      bool            `gorm:"not null;default:true"`
    NextFireAt   *time.Time
    LastFireAt   *time.Time
    LastRunID    *string         `gorm:"type:uuid"`
    CreatedBy    string          `gorm:"not null"`
    CreatedAt    time.Time       `gorm:"not null"`
    UpdatedAt    time.Time       `gorm:"not null"`
    DeletedAt    gorm.DeletedAt  `gorm:"index"`
}

func (cronJobModel) TableName() string { return "cron_jobs" }
```

- [ ] **Step 2：写 mapper.go**

```go
package cron

import domaincron "github.com/shinya/shineflow/domain/cron"

func toCronJob(m *cronJobModel) *domaincron.CronJob {
    return &domaincron.CronJob{
        ID:           m.ID,
        DefinitionID: m.DefinitionID,
        Name:         m.Name,
        Description:  m.Description,
        Expression:   m.Expression,
        Timezone:     m.Timezone,
        Payload:      m.Payload,
        Enabled:      m.Enabled,
        NextFireAt:   m.NextFireAt,
        LastFireAt:   m.LastFireAt,
        LastRunID:    m.LastRunID,
        CreatedBy:    m.CreatedBy,
        CreatedAt:    m.CreatedAt,
        UpdatedAt:    m.UpdatedAt,
    }
}

func toCronJobModel(j *domaincron.CronJob) *cronJobModel {
    return &cronJobModel{
        ID:           j.ID,
        DefinitionID: j.DefinitionID,
        Name:         j.Name,
        Description:  j.Description,
        Expression:   j.Expression,
        Timezone:     j.Timezone,
        Payload:      j.Payload,
        Enabled:      j.Enabled,
        NextFireAt:   j.NextFireAt,
        LastFireAt:   j.LastFireAt,
        LastRunID:    j.LastRunID,
        CreatedBy:    j.CreatedBy,
        CreatedAt:    j.CreatedAt,
        UpdatedAt:    j.UpdatedAt,
    }
}
```

- [ ] **Step 3：build 验证**

Run：`go build ./infrastructure/storage/cron/`
Expected：无输出

- [ ] **Step 4：commit**

```bash
git add infrastructure/storage/cron/
git commit -m "feat(storage/cron): add cron job model and mapper"
```

---

### Task 22：Cron Create / Get / List / Update / Delete

**Files：**
- Create：`infrastructure/storage/cron/repository.go`
- Create：`infrastructure/storage/cron/repository_test.go`

- [ ] **Step 1：写测试**

```go
package cron_test

import (
    "context"
    "errors"
    "testing"
    "time"

    "github.com/google/uuid"

    domaincron "github.com/shinya/shineflow/domain/cron"
    domainworkflow "github.com/shinya/shineflow/domain/workflow"
    storagecron "github.com/shinya/shineflow/infrastructure/storage/cron"
    "github.com/shinya/shineflow/infrastructure/storage/storagetest"
    storageworkflow "github.com/shinya/shineflow/infrastructure/storage/workflow"
)

func seedDef(t *testing.T, ctx context.Context) string {
    t.Helper()
    wfRepo := storageworkflow.NewWorkflowRepository()
    d := &domainworkflow.WorkflowDefinition{
        ID: uuid.NewString(), Name: "d", CreatedBy: "u",
        CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
    }
    _ = wfRepo.CreateDefinition(ctx, d)
    return d.ID
}

func newCronJob(t *testing.T, defID string) *domaincron.CronJob {
    t.Helper()
    return &domaincron.CronJob{
        ID:           uuid.NewString(),
        DefinitionID: defID,
        Name:         "daily",
        Expression:   "0 0 * * *",
        Timezone:     "Asia/Shanghai",
        Enabled:      true,
        CreatedBy:    "u",
        CreatedAt:    time.Now().UTC(),
        UpdatedAt:    time.Now().UTC(),
    }
}

func TestCron_CreateAndGet(t *testing.T) {
    ctx := storagetest.Setup(t)
    repo := storagecron.NewCronJobRepository()
    j := newCronJob(t, seedDef(t, ctx))
    if err := repo.Create(ctx, j); err != nil { t.Fatal(err) }
    got, err := repo.Get(ctx, j.ID)
    if err != nil { t.Fatal(err) }
    if got.Name != "daily" { t.Fatalf("name: %s", got.Name) }
}

func TestCron_GetNotFound(t *testing.T) {
    ctx := storagetest.Setup(t)
    repo := storagecron.NewCronJobRepository()
    _, err := repo.Get(ctx, uuid.NewString())
    if !errors.Is(err, domaincron.ErrCronJobNotFound) {
        t.Fatalf("expected ErrCronJobNotFound: %v", err)
    }
}

func TestCron_Update(t *testing.T) {
    ctx := storagetest.Setup(t)
    repo := storagecron.NewCronJobRepository()
    j := newCronJob(t, seedDef(t, ctx))
    _ = repo.Create(ctx, j)
    j.Enabled = false
    j.UpdatedAt = time.Now().UTC()
    if err := repo.Update(ctx, j); err != nil { t.Fatal(err) }
    got, _ := repo.Get(ctx, j.ID)
    if got.Enabled { t.Fatal("expected disabled") }
}

func TestCron_DeleteSoft(t *testing.T) {
    ctx := storagetest.Setup(t)
    repo := storagecron.NewCronJobRepository()
    j := newCronJob(t, seedDef(t, ctx))
    _ = repo.Create(ctx, j)
    if err := repo.Delete(ctx, j.ID); err != nil { t.Fatal(err) }
    _, err := repo.Get(ctx, j.ID)
    if !errors.Is(err, domaincron.ErrCronJobNotFound) {
        t.Fatalf("expected NotFound after soft delete: %v", err)
    }
}

func TestCron_List_FilterByEnabled(t *testing.T) {
    ctx := storagetest.Setup(t)
    repo := storagecron.NewCronJobRepository()
    defID := seedDef(t, ctx)
    on, off := newCronJob(t, defID), newCronJob(t, defID)
    off.Enabled = false
    _ = repo.Create(ctx, on)
    _ = repo.Create(ctx, off)

    list, err := repo.List(ctx, domaincron.CronJobFilter{EnabledOnly: true})
    if err != nil { t.Fatal(err) }
    if len(list) != 1 || list[0].ID != on.ID {
        t.Fatalf("filter wrong: %+v", list)
    }
}
```

- [ ] **Step 2：跑测试确认失败**

Run：`go test ./infrastructure/storage/cron/... -v`
Expected：FAIL

- [ ] **Step 3：实现 repository.go**

```go
package cron

import (
    "context"
    "errors"
    "time"

    "gorm.io/gorm"

    domaincron "github.com/shinya/shineflow/domain/cron"
    "github.com/shinya/shineflow/infrastructure/storage"
)

type cronRepo struct{}

func NewCronJobRepository() domaincron.CronJobRepository { return &cronRepo{} }

func (r *cronRepo) Create(ctx context.Context, j *domaincron.CronJob) error {
    return storage.GetDB(ctx).Create(toCronJobModel(j)).Error
}

func (r *cronRepo) Get(ctx context.Context, id string) (*domaincron.CronJob, error) {
    var m cronJobModel
    err := storage.GetDB(ctx).Where("id = ?", id).Take(&m).Error
    if errors.Is(err, gorm.ErrRecordNotFound) {
        return nil, domaincron.ErrCronJobNotFound
    }
    if err != nil { return nil, err }
    return toCronJob(&m), nil
}

func (r *cronRepo) List(ctx context.Context, filter domaincron.CronJobFilter) ([]*domaincron.CronJob, error) {
    q := storage.GetDB(ctx).Model(&cronJobModel{})
    if filter.DefinitionID != "" { q = q.Where("definition_id = ?", filter.DefinitionID) }
    if filter.EnabledOnly { q = q.Where("enabled = ?", true) }
    if filter.Limit > 0 { q = q.Limit(filter.Limit) }
    if filter.Offset > 0 { q = q.Offset(filter.Offset) }
    q = q.Order("created_at DESC")

    var ms []cronJobModel
    if err := q.Find(&ms).Error; err != nil { return nil, err }
    out := make([]*domaincron.CronJob, 0, len(ms))
    for i := range ms { out = append(out, toCronJob(&ms[i])) }
    return out, nil
}

func (r *cronRepo) Update(ctx context.Context, j *domaincron.CronJob) error {
    res := storage.GetDB(ctx).Model(&cronJobModel{}).Where("id = ?", j.ID).
        Updates(map[string]any{
            "name":         j.Name,
            "description":  j.Description,
            "expression":   j.Expression,
            "timezone":     j.Timezone,
            "payload":      j.Payload,
            "enabled":      j.Enabled,
            "next_fire_at": j.NextFireAt,
            "updated_at":   j.UpdatedAt,
        })
    if res.Error != nil { return res.Error }
    if res.RowsAffected == 0 { return domaincron.ErrCronJobNotFound }
    return nil
}

func (r *cronRepo) Delete(ctx context.Context, id string) error {
    res := storage.GetDB(ctx).Where("id = ?", id).Delete(&cronJobModel{})
    if res.Error != nil { return res.Error }
    if res.RowsAffected == 0 { return domaincron.ErrCronJobNotFound }
    return nil
}

// 占位
func (r *cronRepo) ClaimDue(ctx context.Context, now time.Time, limit int) ([]*domaincron.CronJob, error) {
    return nil, errors.New("not implemented")
}
func (r *cronRepo) MarkFired(ctx context.Context, id string, lastFireAt, nextFireAt time.Time, lastRunID string) error {
    return errors.New("not implemented")
}
```

- [ ] **Step 4：测试通过**

Run：`go test ./infrastructure/storage/cron/... -run "TestCron_(CreateAndGet|GetNotFound|Update|DeleteSoft|List)" -v`
Expected：5 个 PASS

- [ ] **Step 5：commit**

```bash
git add infrastructure/storage/cron/
git commit -m "feat(storage/cron): implement CRUD with soft delete"
```

---

### Task 23：MarkFired

**Files：**
- Modify：`infrastructure/storage/cron/repository.go`
- Modify：`infrastructure/storage/cron/repository_test.go`

- [ ] **Step 1：追加测试**

```go
func TestCron_MarkFired(t *testing.T) {
    ctx := storagetest.Setup(t)
    repo := storagecron.NewCronJobRepository()
    j := newCronJob(t, seedDef(t, ctx))
    _ = repo.Create(ctx, j)

    now := time.Now().UTC()
    nextFire := now.Add(24 * time.Hour)
    runID := uuid.NewString()
    if err := repo.MarkFired(ctx, j.ID, now, nextFire, runID); err != nil {
        t.Fatal(err)
    }
    got, _ := repo.Get(ctx, j.ID)
    if got.LastFireAt == nil || !got.LastFireAt.Equal(now) {
        t.Fatalf("last_fire_at: %v", got.LastFireAt)
    }
    if got.LastRunID == nil || *got.LastRunID != runID {
        t.Fatalf("last_run_id: %v", got.LastRunID)
    }
}
```

- [ ] **Step 2：跑测试确认失败**

Run：`go test ./infrastructure/storage/cron/... -run TestCron_MarkFired -v`

- [ ] **Step 3：实现 MarkFired**

替换 stub：

```go
func (r *cronRepo) MarkFired(
    ctx context.Context, id string, lastFireAt, nextFireAt time.Time, lastRunID string,
) error {
    res := storage.GetDB(ctx).Model(&cronJobModel{}).Where("id = ?", id).
        Updates(map[string]any{
            "last_fire_at": lastFireAt,
            "next_fire_at": nextFireAt,
            "last_run_id":  lastRunID,
            "updated_at":   time.Now().UTC(),
        })
    if res.Error != nil { return res.Error }
    if res.RowsAffected == 0 { return domaincron.ErrCronJobNotFound }
    return nil
}
```

- [ ] **Step 4：测试通过**

Run：`go test ./infrastructure/storage/cron/... -run TestCron_MarkFired -v`

- [ ] **Step 5：commit**

```bash
git add infrastructure/storage/cron/
git commit -m "feat(storage/cron): implement MarkFired"
```

---

### Task 24：ClaimDue（SKIP LOCKED，要求外层 tx）

**Files：**
- Modify：`infrastructure/storage/cron/repository.go`
- Modify：`infrastructure/storage/cron/repository_test.go`

- [ ] **Step 1：追加测试**

```go
func TestCron_ClaimDue(t *testing.T) {
    ctx := storagetest.Setup(t)
    // ctx 已经在 tx 中（storagetest.Setup 提供），所以 ClaimDue 可直接调用
    repo := storagecron.NewCronJobRepository()
    defID := seedDef(t, ctx)

    past := time.Now().UTC().Add(-time.Minute)
    future := time.Now().UTC().Add(time.Hour)

    due := newCronJob(t, defID); due.NextFireAt = &past; _ = repo.Create(ctx, due)
    notYet := newCronJob(t, defID); notYet.NextFireAt = &future; _ = repo.Create(ctx, notYet)
    disabled := newCronJob(t, defID); disabled.NextFireAt = &past; disabled.Enabled = false; _ = repo.Create(ctx, disabled)

    got, err := repo.ClaimDue(ctx, time.Now().UTC(), 10)
    if err != nil { t.Fatal(err) }
    if len(got) != 1 || got[0].ID != due.ID {
        t.Fatalf("claim wrong: %+v", got)
    }
}
```

- [ ] **Step 2：跑测试确认失败**

Run：`go test ./infrastructure/storage/cron/... -run TestCron_ClaimDue -v`

- [ ] **Step 3：实现 ClaimDue**

```go
import (
    // ...
    "gorm.io/gorm/clause"
)

func (r *cronRepo) ClaimDue(
    ctx context.Context, now time.Time, limit int,
) ([]*domaincron.CronJob, error) {
    var ms []cronJobModel
    err := storage.GetDB(ctx).
        Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
        Where("enabled = ? AND next_fire_at IS NOT NULL AND next_fire_at <= ?", true, now).
        Order("next_fire_at ASC").
        Limit(limit).
        Find(&ms).Error
    if err != nil { return nil, err }
    out := make([]*domaincron.CronJob, 0, len(ms))
    for i := range ms { out = append(out, toCronJob(&ms[i])) }
    return out, nil
}
```

并在方法 godoc 上明确注明：

```go
// ClaimDue 用 FOR UPDATE SKIP LOCKED 锁住到期任务。
// **必须在 storage.DBTransaction 内调用**——锁随事务结束释放，
// 调用方典型流程：DBTransaction(ctx, func(ctx){ ClaimDue → MarkFired }) 全部在同一事务里。
```

- [ ] **Step 4：测试通过**

Run：`go test ./infrastructure/storage/cron/... -run TestCron_ClaimDue -v`

- [ ] **Step 5：跑全 cron 包**

Run：`go test ./infrastructure/storage/cron/...`
Expected：全 PASS

- [ ] **Step 6：commit**

```bash
git add infrastructure/storage/cron/
git commit -m "feat(storage/cron): implement ClaimDue with SKIP LOCKED"
```

---

## Phase 4：Plugin aggregate（HttpPlugin + McpServer + McpTool）

### Task 25：plugin model + codec + mapper 骨架

**Files：**
- Create：`infrastructure/storage/plugin/model.go`
- Create：`infrastructure/storage/plugin/codec.go`
- Create：`infrastructure/storage/plugin/mapper.go`

- [ ] **Step 1：创建包**

Run：`mkdir -p infrastructure/storage/plugin`

- [ ] **Step 2：写 codec.go（map[string]string + []PortSpec 的 wrapper）**

```go
package plugin

import (
    "database/sql/driver"
    "fmt"

    domainworkflow "github.com/shinya/shineflow/domain/workflow"
    "github.com/shinya/shineflow/infrastructure/util"
)

// stringMapColumn 复用给 headers / query_params / response_mapping 三列。
type stringMapColumn map[string]string

func (c *stringMapColumn) Scan(src any) error {
    var s string
    switch v := src.(type) {
    case []byte: s = string(v)
    case string: s = v
    case nil: *c = nil; return nil
    default: return fmt.Errorf("string map: unsupported scan type %T", src)
    }
    return util.UnmarshalFromString(s, (*map[string]string)(c))
}

func (c stringMapColumn) Value() (driver.Value, error) {
    if c == nil { return "{}", nil }
    return util.MarshalToString(map[string]string(c))
}

// portSpecsColumn 给 input_schema / output_schema 两列。
type portSpecsColumn []domainworkflow.PortSpec

func (c *portSpecsColumn) Scan(src any) error {
    var s string
    switch v := src.(type) {
    case []byte: s = string(v)
    case string: s = v
    case nil: *c = nil; return nil
    default: return fmt.Errorf("port specs: unsupported scan type %T", src)
    }
    return util.UnmarshalFromString(s, (*[]domainworkflow.PortSpec)(c))
}

func (c portSpecsColumn) Value() (driver.Value, error) {
    if c == nil { return "[]", nil }
    return util.MarshalToString([]domainworkflow.PortSpec(c))
}
```

- [ ] **Step 3：写 model.go（3 个表）**

```go
package plugin

import (
    "encoding/json"
    "time"

    "gorm.io/gorm"
)

type httpPluginModel struct {
    ID              string          `gorm:"primaryKey;type:uuid"`
    Name            string          `gorm:"not null"`
    Description     string          `gorm:"not null;default:''"`
    Method          string          `gorm:"not null"`
    URL             string          `gorm:"not null"`
    Headers         stringMapColumn `gorm:"type:jsonb;not null;default:'{}'"`
    QueryParams     stringMapColumn `gorm:"type:jsonb;not null;default:'{}'"`
    BodyTemplate    string          `gorm:"not null;default:''"`
    AuthKind        string          `gorm:"not null"`
    CredentialID    *string         `gorm:"type:uuid"`
    InputSchema     portSpecsColumn `gorm:"type:jsonb;not null;default:'[]'"`
    OutputSchema    portSpecsColumn `gorm:"type:jsonb;not null;default:'[]'"`
    ResponseMapping stringMapColumn `gorm:"type:jsonb;not null;default:'{}'"`
    Enabled         bool            `gorm:"not null;default:true"`
    CreatedBy       string          `gorm:"not null"`
    CreatedAt       time.Time       `gorm:"not null"`
    UpdatedAt       time.Time       `gorm:"not null"`
    DeletedAt       gorm.DeletedAt  `gorm:"index"`
}

func (httpPluginModel) TableName() string { return "http_plugins" }

type mcpServerModel struct {
    ID            string          `gorm:"primaryKey;type:uuid"`
    Name          string          `gorm:"not null"`
    Transport     string          `gorm:"not null"`
    Config        json.RawMessage `gorm:"type:jsonb;not null"`
    CredentialID  *string         `gorm:"type:uuid"`
    Enabled       bool            `gorm:"not null;default:true"`
    LastSyncedAt  *time.Time
    LastSyncError *string
    CreatedBy     string          `gorm:"not null"`
    CreatedAt     time.Time       `gorm:"not null"`
    UpdatedAt     time.Time       `gorm:"not null"`
    DeletedAt     gorm.DeletedAt  `gorm:"index"`
}

func (mcpServerModel) TableName() string { return "mcp_servers" }

type mcpToolModel struct {
    ID             string          `gorm:"primaryKey;type:uuid"`
    ServerID       string          `gorm:"type:uuid;not null;index"`
    Name           string          `gorm:"not null"`
    Description    string          `gorm:"not null;default:''"`
    InputSchemaRaw json.RawMessage `gorm:"type:jsonb;not null"`
    Enabled        bool            `gorm:"not null;default:true"`
    SyncedAt       time.Time       `gorm:"not null"`
}

func (mcpToolModel) TableName() string { return "mcp_tools" }
```

- [ ] **Step 4：写 mapper.go**

```go
package plugin

import (
    domainplugin "github.com/shinya/shineflow/domain/plugin"
    domainworkflow "github.com/shinya/shineflow/domain/workflow"
)

func toHttpPlugin(m *httpPluginModel) *domainplugin.HttpPlugin {
    return &domainplugin.HttpPlugin{
        ID:              m.ID,
        Name:            m.Name,
        Description:     m.Description,
        Method:          m.Method,
        URL:             m.URL,
        Headers:         map[string]string(m.Headers),
        QueryParams:     map[string]string(m.QueryParams),
        BodyTemplate:    m.BodyTemplate,
        AuthKind:        m.AuthKind,
        CredentialID:    m.CredentialID,
        InputSchema:     []domainworkflow.PortSpec(m.InputSchema),
        OutputSchema:    []domainworkflow.PortSpec(m.OutputSchema),
        ResponseMapping: map[string]string(m.ResponseMapping),
        Enabled:         m.Enabled,
        CreatedBy:       m.CreatedBy,
        CreatedAt:       m.CreatedAt,
        UpdatedAt:       m.UpdatedAt,
    }
}

func toHttpPluginModel(p *domainplugin.HttpPlugin) *httpPluginModel {
    return &httpPluginModel{
        ID:              p.ID,
        Name:            p.Name,
        Description:     p.Description,
        Method:          p.Method,
        URL:             p.URL,
        Headers:         stringMapColumn(p.Headers),
        QueryParams:     stringMapColumn(p.QueryParams),
        BodyTemplate:    p.BodyTemplate,
        AuthKind:        p.AuthKind,
        CredentialID:    p.CredentialID,
        InputSchema:     portSpecsColumn(p.InputSchema),
        OutputSchema:    portSpecsColumn(p.OutputSchema),
        ResponseMapping: stringMapColumn(p.ResponseMapping),
        Enabled:         p.Enabled,
        CreatedBy:       p.CreatedBy,
        CreatedAt:       p.CreatedAt,
        UpdatedAt:       p.UpdatedAt,
    }
}

func toMcpServer(m *mcpServerModel) *domainplugin.McpServer {
    return &domainplugin.McpServer{
        ID:            m.ID,
        Name:          m.Name,
        Transport:     domainplugin.McpTransport(m.Transport),
        Config:        m.Config,
        CredentialID:  m.CredentialID,
        Enabled:       m.Enabled,
        LastSyncedAt:  m.LastSyncedAt,
        LastSyncError: m.LastSyncError,
        CreatedBy:     m.CreatedBy,
        CreatedAt:     m.CreatedAt,
        UpdatedAt:     m.UpdatedAt,
    }
}

func toMcpServerModel(s *domainplugin.McpServer) *mcpServerModel {
    return &mcpServerModel{
        ID:            s.ID,
        Name:          s.Name,
        Transport:     string(s.Transport),
        Config:        s.Config,
        CredentialID:  s.CredentialID,
        Enabled:       s.Enabled,
        LastSyncedAt:  s.LastSyncedAt,
        LastSyncError: s.LastSyncError,
        CreatedBy:     s.CreatedBy,
        CreatedAt:     s.CreatedAt,
        UpdatedAt:     s.UpdatedAt,
    }
}

func toMcpTool(m *mcpToolModel) *domainplugin.McpTool {
    return &domainplugin.McpTool{
        ID:             m.ID,
        ServerID:       m.ServerID,
        Name:           m.Name,
        Description:    m.Description,
        InputSchemaRaw: m.InputSchemaRaw,
        Enabled:        m.Enabled,
        SyncedAt:       m.SyncedAt,
    }
}

func toMcpToolModel(t *domainplugin.McpTool) *mcpToolModel {
    return &mcpToolModel{
        ID:             t.ID,
        ServerID:       t.ServerID,
        Name:           t.Name,
        Description:    t.Description,
        InputSchemaRaw: t.InputSchemaRaw,
        Enabled:        t.Enabled,
        SyncedAt:       t.SyncedAt,
    }
}
```

- [ ] **Step 5：build 验证**

Run：`go build ./infrastructure/storage/plugin/`
Expected：无输出

- [ ] **Step 6：commit**

```bash
git add infrastructure/storage/plugin/
git commit -m "feat(storage/plugin): add models, codec, mapper for 3 plugin entities"
```

---

### Task 26：HttpPluginRepository CRUD

**Files：**
- Create：`infrastructure/storage/plugin/http_plugin_repository.go`
- Create：`infrastructure/storage/plugin/repository_test.go`

- [ ] **Step 1：写测试**

```go
package plugin_test

import (
    "errors"
    "testing"
    "time"

    "github.com/google/uuid"

    domainplugin "github.com/shinya/shineflow/domain/plugin"
    storageplugin "github.com/shinya/shineflow/infrastructure/storage/plugin"
    "github.com/shinya/shineflow/infrastructure/storage/storagetest"
)

func newHttpPlugin(t *testing.T) *domainplugin.HttpPlugin {
    t.Helper()
    return &domainplugin.HttpPlugin{
        ID:        uuid.NewString(),
        Name:      "weather-api",
        Method:    "GET",
        URL:       "https://api.example.com/weather",
        AuthKind:  domainplugin.HttpAuthNone,
        Enabled:   true,
        CreatedBy: "u",
        CreatedAt: time.Now().UTC(),
        UpdatedAt: time.Now().UTC(),
    }
}

func TestHttpPlugin_CreateAndGet(t *testing.T) {
    ctx := storagetest.Setup(t)
    repo := storageplugin.NewHttpPluginRepository()
    p := newHttpPlugin(t)
    if err := repo.Create(ctx, p); err != nil { t.Fatal(err) }
    got, err := repo.Get(ctx, p.ID)
    if err != nil { t.Fatal(err) }
    if got.URL != p.URL { t.Fatalf("url: %s", got.URL) }
}

func TestHttpPlugin_GetNotFound(t *testing.T) {
    ctx := storagetest.Setup(t)
    repo := storageplugin.NewHttpPluginRepository()
    _, err := repo.Get(ctx, uuid.NewString())
    if !errors.Is(err, domainplugin.ErrHttpPluginNotFound) {
        t.Fatalf("expected ErrHttpPluginNotFound: %v", err)
    }
}

func TestHttpPlugin_DeleteSoft_NameReusable(t *testing.T) {
    ctx := storagetest.Setup(t)
    repo := storageplugin.NewHttpPluginRepository()
    p := newHttpPlugin(t)
    _ = repo.Create(ctx, p)
    if err := repo.Delete(ctx, p.ID); err != nil { t.Fatal(err) }

    // 软删后同名再创建应成功（partial unique）
    p2 := newHttpPlugin(t)
    p2.Name = p.Name
    if err := repo.Create(ctx, p2); err != nil {
        t.Fatalf("expected reusable name after soft delete: %v", err)
    }
}

func TestHttpPlugin_List_FilterByEnabled(t *testing.T) {
    ctx := storagetest.Setup(t)
    repo := storageplugin.NewHttpPluginRepository()
    on, off := newHttpPlugin(t), newHttpPlugin(t)
    off.Enabled = false; off.Name = "off-one"
    _ = repo.Create(ctx, on)
    _ = repo.Create(ctx, off)
    list, err := repo.List(ctx, domainplugin.HttpPluginFilter{EnabledOnly: true})
    if err != nil { t.Fatal(err) }
    if len(list) != 1 || list[0].ID != on.ID { t.Fatalf("filter wrong: %+v", list) }
}
```

- [ ] **Step 2：跑测试确认失败**

Run：`go test ./infrastructure/storage/plugin/... -run TestHttpPlugin -v`
Expected：FAIL

- [ ] **Step 3：实现 http_plugin_repository.go**

```go
package plugin

import (
    "context"
    "errors"

    "gorm.io/gorm"

    domainplugin "github.com/shinya/shineflow/domain/plugin"
    "github.com/shinya/shineflow/infrastructure/storage"
)

type httpRepo struct{}

func NewHttpPluginRepository() domainplugin.HttpPluginRepository { return &httpRepo{} }

func (r *httpRepo) Create(ctx context.Context, p *domainplugin.HttpPlugin) error {
    return storage.GetDB(ctx).Create(toHttpPluginModel(p)).Error
}

func (r *httpRepo) Get(ctx context.Context, id string) (*domainplugin.HttpPlugin, error) {
    var m httpPluginModel
    err := storage.GetDB(ctx).Where("id = ?", id).Take(&m).Error
    if errors.Is(err, gorm.ErrRecordNotFound) {
        return nil, domainplugin.ErrHttpPluginNotFound
    }
    if err != nil { return nil, err }
    return toHttpPlugin(&m), nil
}

func (r *httpRepo) List(
    ctx context.Context, filter domainplugin.HttpPluginFilter,
) ([]*domainplugin.HttpPlugin, error) {
    q := storage.GetDB(ctx).Model(&httpPluginModel{})
    if filter.EnabledOnly { q = q.Where("enabled = ?", true) }
    if filter.CreatedBy != "" { q = q.Where("created_by = ?", filter.CreatedBy) }
    if filter.Limit > 0 { q = q.Limit(filter.Limit) }
    if filter.Offset > 0 { q = q.Offset(filter.Offset) }
    q = q.Order("created_at DESC")
    var ms []httpPluginModel
    if err := q.Find(&ms).Error; err != nil { return nil, err }
    out := make([]*domainplugin.HttpPlugin, 0, len(ms))
    for i := range ms { out = append(out, toHttpPlugin(&ms[i])) }
    return out, nil
}

func (r *httpRepo) Update(ctx context.Context, p *domainplugin.HttpPlugin) error {
    res := storage.GetDB(ctx).Model(&httpPluginModel{}).Where("id = ?", p.ID).
        Updates(map[string]any{
            "name":             p.Name,
            "description":      p.Description,
            "method":           p.Method,
            "url":              p.URL,
            "headers":          stringMapColumn(p.Headers),
            "query_params":     stringMapColumn(p.QueryParams),
            "body_template":    p.BodyTemplate,
            "auth_kind":        p.AuthKind,
            "credential_id":    p.CredentialID,
            "input_schema":     portSpecsColumn(p.InputSchema),
            "output_schema":    portSpecsColumn(p.OutputSchema),
            "response_mapping": stringMapColumn(p.ResponseMapping),
            "enabled":          p.Enabled,
            "updated_at":       p.UpdatedAt,
        })
    if res.Error != nil { return res.Error }
    if res.RowsAffected == 0 { return domainplugin.ErrHttpPluginNotFound }
    return nil
}

func (r *httpRepo) Delete(ctx context.Context, id string) error {
    res := storage.GetDB(ctx).Where("id = ?", id).Delete(&httpPluginModel{})
    if res.Error != nil { return res.Error }
    if res.RowsAffected == 0 { return domainplugin.ErrHttpPluginNotFound }
    return nil
}
```

- [ ] **Step 4：测试通过**

Run：`go test ./infrastructure/storage/plugin/... -run TestHttpPlugin -v`
Expected：4 个 PASS

- [ ] **Step 5：commit**

```bash
git add infrastructure/storage/plugin/
git commit -m "feat(storage/plugin): implement HttpPluginRepository CRUD"
```

---

### Task 27：McpServerRepository CRUD

**Files：**
- Create：`infrastructure/storage/plugin/mcp_server_repository.go`
- Modify：`infrastructure/storage/plugin/repository_test.go`

- [ ] **Step 1：追加测试**

```go
import "encoding/json"  // 顶部 import

func newMcpServer(t *testing.T) *domainplugin.McpServer {
    t.Helper()
    return &domainplugin.McpServer{
        ID:        uuid.NewString(),
        Name:      "echo",
        Transport: domainplugin.McpTransportStdio,
        Config:    json.RawMessage(`{"command":"echo"}`),
        Enabled:   true,
        CreatedBy: "u",
        CreatedAt: time.Now().UTC(),
        UpdatedAt: time.Now().UTC(),
    }
}

func TestMcpServer_CreateAndGet(t *testing.T) {
    ctx := storagetest.Setup(t)
    repo := storageplugin.NewMcpServerRepository()
    s := newMcpServer(t)
    if err := repo.Create(ctx, s); err != nil { t.Fatal(err) }
    got, err := repo.Get(ctx, s.ID)
    if err != nil { t.Fatal(err) }
    if got.Transport != domainplugin.McpTransportStdio { t.Fatalf("transport: %s", got.Transport) }
}

func TestMcpServer_GetNotFound(t *testing.T) {
    ctx := storagetest.Setup(t)
    repo := storageplugin.NewMcpServerRepository()
    _, err := repo.Get(ctx, uuid.NewString())
    if !errors.Is(err, domainplugin.ErrMcpServerNotFound) {
        t.Fatalf("expected ErrMcpServerNotFound: %v", err)
    }
}
```

- [ ] **Step 2：跑测试确认失败**

Run：`go test ./infrastructure/storage/plugin/... -run TestMcpServer -v`

- [ ] **Step 3：实现**

```go
package plugin

import (
    "context"
    "errors"

    "gorm.io/gorm"

    domainplugin "github.com/shinya/shineflow/domain/plugin"
    "github.com/shinya/shineflow/infrastructure/storage"
)

type mcpServerRepo struct{}

func NewMcpServerRepository() domainplugin.McpServerRepository { return &mcpServerRepo{} }

func (r *mcpServerRepo) Create(ctx context.Context, s *domainplugin.McpServer) error {
    return storage.GetDB(ctx).Create(toMcpServerModel(s)).Error
}

func (r *mcpServerRepo) Get(ctx context.Context, id string) (*domainplugin.McpServer, error) {
    var m mcpServerModel
    err := storage.GetDB(ctx).Where("id = ?", id).Take(&m).Error
    if errors.Is(err, gorm.ErrRecordNotFound) {
        return nil, domainplugin.ErrMcpServerNotFound
    }
    if err != nil { return nil, err }
    return toMcpServer(&m), nil
}

func (r *mcpServerRepo) List(
    ctx context.Context, filter domainplugin.McpServerFilter,
) ([]*domainplugin.McpServer, error) {
    q := storage.GetDB(ctx).Model(&mcpServerModel{})
    if filter.EnabledOnly { q = q.Where("enabled = ?", true) }
    if filter.Limit > 0 { q = q.Limit(filter.Limit) }
    if filter.Offset > 0 { q = q.Offset(filter.Offset) }
    q = q.Order("created_at DESC")
    var ms []mcpServerModel
    if err := q.Find(&ms).Error; err != nil { return nil, err }
    out := make([]*domainplugin.McpServer, 0, len(ms))
    for i := range ms { out = append(out, toMcpServer(&ms[i])) }
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
    if res.Error != nil { return res.Error }
    if res.RowsAffected == 0 { return domainplugin.ErrMcpServerNotFound }
    return nil
}

func (r *mcpServerRepo) Delete(ctx context.Context, id string) error {
    res := storage.GetDB(ctx).Where("id = ?", id).Delete(&mcpServerModel{})
    if res.Error != nil { return res.Error }
    if res.RowsAffected == 0 { return domainplugin.ErrMcpServerNotFound }
    return nil
}
```

- [ ] **Step 4：测试通过**

Run：`go test ./infrastructure/storage/plugin/... -run TestMcpServer -v`

- [ ] **Step 5：commit**

```bash
git add infrastructure/storage/plugin/
git commit -m "feat(storage/plugin): implement McpServerRepository CRUD"
```

---

### Task 28：McpToolRepository GetByServerAndName / ListByServer / SetEnabled

**Files：**
- Create：`infrastructure/storage/plugin/mcp_tool_repository.go`
- Modify：`infrastructure/storage/plugin/repository_test.go`

- [ ] **Step 1：追加测试**

```go
func newMcpTool(t *testing.T, serverID, name string) *domainplugin.McpTool {
    t.Helper()
    return &domainplugin.McpTool{
        ID:             uuid.NewString(),
        ServerID:       serverID,
        Name:           name,
        InputSchemaRaw: json.RawMessage(`{}`),
        Enabled:        true,
        SyncedAt:       time.Now().UTC(),
    }
}

func TestMcpTool_GetByServerAndName(t *testing.T) {
    ctx := storagetest.Setup(t)
    serverRepo := storageplugin.NewMcpServerRepository()
    toolRepo := storageplugin.NewMcpToolRepository()
    s := newMcpServer(t); _ = serverRepo.Create(ctx, s)
    tt := newMcpTool(t, s.ID, "echo_tool"); _ = toolRepo.UpsertAll(ctx, s.ID, []*domainplugin.McpTool{tt})

    got, err := toolRepo.GetByServerAndName(ctx, s.ID, "echo_tool")
    if err != nil { t.Fatal(err) }
    if got.Name != "echo_tool" { t.Fatalf("name: %s", got.Name) }
}

func TestMcpTool_GetByServerAndName_NotFound(t *testing.T) {
    ctx := storagetest.Setup(t)
    toolRepo := storageplugin.NewMcpToolRepository()
    _, err := toolRepo.GetByServerAndName(ctx, uuid.NewString(), "ghost")
    if !errors.Is(err, domainplugin.ErrMcpToolNotFound) {
        t.Fatalf("expected ErrMcpToolNotFound: %v", err)
    }
}

func TestMcpTool_ListByServer(t *testing.T) {
    ctx := storagetest.Setup(t)
    serverRepo := storageplugin.NewMcpServerRepository()
    toolRepo := storageplugin.NewMcpToolRepository()
    s := newMcpServer(t); _ = serverRepo.Create(ctx, s)
    _ = toolRepo.UpsertAll(ctx, s.ID, []*domainplugin.McpTool{
        newMcpTool(t, s.ID, "a"), newMcpTool(t, s.ID, "b"),
    })

    list, err := toolRepo.ListByServer(ctx, s.ID)
    if err != nil { t.Fatal(err) }
    if len(list) != 2 { t.Fatalf("len: %d", len(list)) }
}

func TestMcpTool_SetEnabled(t *testing.T) {
    ctx := storagetest.Setup(t)
    serverRepo := storageplugin.NewMcpServerRepository()
    toolRepo := storageplugin.NewMcpToolRepository()
    s := newMcpServer(t); _ = serverRepo.Create(ctx, s)
    tt := newMcpTool(t, s.ID, "x"); _ = toolRepo.UpsertAll(ctx, s.ID, []*domainplugin.McpTool{tt})

    if err := toolRepo.SetEnabled(ctx, tt.ID, false); err != nil { t.Fatal(err) }
    got, _ := toolRepo.GetByServerAndName(ctx, s.ID, "x")
    if got.Enabled { t.Fatal("expected disabled") }
}
```

- [ ] **Step 2：跑测试确认失败（UpsertAll 也用到了，要 stub 一下）**

Run：`go test ./infrastructure/storage/plugin/... -run TestMcpTool -v`
Expected：FAIL（`NewMcpToolRepository` 不存在）

- [ ] **Step 3：实现 mcp_tool_repository.go（含 UpsertAll stub）**

```go
package plugin

import (
    "context"
    "errors"

    "gorm.io/gorm"

    domainplugin "github.com/shinya/shineflow/domain/plugin"
    "github.com/shinya/shineflow/infrastructure/storage"
)

type mcpToolRepo struct{}

func NewMcpToolRepository() domainplugin.McpToolRepository { return &mcpToolRepo{} }

func (r *mcpToolRepo) GetByServerAndName(
    ctx context.Context, serverID, name string,
) (*domainplugin.McpTool, error) {
    var m mcpToolModel
    err := storage.GetDB(ctx).Where("server_id = ? AND name = ?", serverID, name).Take(&m).Error
    if errors.Is(err, gorm.ErrRecordNotFound) {
        return nil, domainplugin.ErrMcpToolNotFound
    }
    if err != nil { return nil, err }
    return toMcpTool(&m), nil
}

func (r *mcpToolRepo) ListByServer(
    ctx context.Context, serverID string,
) ([]*domainplugin.McpTool, error) {
    var ms []mcpToolModel
    err := storage.GetDB(ctx).Where("server_id = ?", serverID).
        Order("name ASC").Find(&ms).Error
    if err != nil { return nil, err }
    out := make([]*domainplugin.McpTool, 0, len(ms))
    for i := range ms { out = append(out, toMcpTool(&ms[i])) }
    return out, nil
}

func (r *mcpToolRepo) SetEnabled(ctx context.Context, id string, enabled bool) error {
    res := storage.GetDB(ctx).Model(&mcpToolModel{}).Where("id = ?", id).
        Updates(map[string]any{"enabled": enabled})
    if res.Error != nil { return res.Error }
    if res.RowsAffected == 0 { return domainplugin.ErrMcpToolNotFound }
    return nil
}

// 占位：UpsertAll 在 Task 29 实现
func (r *mcpToolRepo) UpsertAll(ctx context.Context, serverID string, tools []*domainplugin.McpTool) error {
    return errors.New("not implemented")
}
```

- [ ] **Step 4：测试通过（除了 UpsertAll 相关）**

UpsertAll 还没实现，前面测试用了 stub —— 此处仅 SetEnabled 和 GetByServerAndName_NotFound 单测能 PASS。其余依赖 UpsertAll 的 test 留到 Task 29 跑通。

Run：`go test ./infrastructure/storage/plugin/... -run "TestMcpTool_GetByServerAndName_NotFound|TestMcpTool_SetEnabled" -v`
注：需要 `TestMcpTool_SetEnabled` 改用直接 `db.Create(&mcpToolModel{...})` 绕过 UpsertAll，或者本 task 也临时把 UpsertAll 实现成最小版本（单 INSERT 循环）。

简化：在 Task 28 把 UpsertAll 简单实现为"先按 ID 删全部 + 逐条 INSERT"，测试都能跑：

```go
func (r *mcpToolRepo) UpsertAll(ctx context.Context, serverID string, tools []*domainplugin.McpTool) error {
    return storage.DBTransaction(ctx, func(ctx context.Context) error {
        db := storage.GetDB(ctx)
        if err := db.Where("server_id = ?", serverID).Delete(&mcpToolModel{}).Error; err != nil {
            return err
        }
        for _, t := range tools {
            t.ServerID = serverID
            if err := db.Create(toMcpToolModel(t)).Error; err != nil { return err }
        }
        return nil
    })
}
```

- [ ] **Step 5：测试通过**

Run：`go test ./infrastructure/storage/plugin/... -run TestMcpTool -v`
Expected：4 个 PASS

- [ ] **Step 6：commit**

```bash
git add infrastructure/storage/plugin/
git commit -m "feat(storage/plugin): implement McpToolRepository (delete-then-insert UpsertAll)"
```

---

### Task 29：McpToolRepository UpsertAll 优化为真 upsert

简单 delete-then-insert 在大量 tool / 存在 FK 引用时会丢历史 ID。改为 ON CONFLICT 真 upsert + 删失踪。

**Files：**
- Modify：`infrastructure/storage/plugin/mcp_tool_repository.go`
- Modify：`infrastructure/storage/plugin/repository_test.go`

- [ ] **Step 1：追加测试**

```go
func TestMcpTool_UpsertAll_PreservesIDsForExisting(t *testing.T) {
    ctx := storagetest.Setup(t)
    serverRepo := storageplugin.NewMcpServerRepository()
    toolRepo := storageplugin.NewMcpToolRepository()
    s := newMcpServer(t); _ = serverRepo.Create(ctx, s)

    tt1 := newMcpTool(t, s.ID, "stable")
    _ = toolRepo.UpsertAll(ctx, s.ID, []*domainplugin.McpTool{tt1})
    originalID := tt1.ID

    // 第二次同步：stable 仍在，新增 fresh
    tt2 := newMcpTool(t, s.ID, "stable") // 新对象，ID 不同
    tt3 := newMcpTool(t, s.ID, "fresh")
    _ = toolRepo.UpsertAll(ctx, s.ID, []*domainplugin.McpTool{tt2, tt3})

    got, _ := toolRepo.GetByServerAndName(ctx, s.ID, "stable")
    if got.ID != originalID {
        t.Fatalf("stable tool ID should be preserved: got %s, want %s", got.ID, originalID)
    }
}

func TestMcpTool_UpsertAll_RemovesMissing(t *testing.T) {
    ctx := storagetest.Setup(t)
    serverRepo := storageplugin.NewMcpServerRepository()
    toolRepo := storageplugin.NewMcpToolRepository()
    s := newMcpServer(t); _ = serverRepo.Create(ctx, s)

    _ = toolRepo.UpsertAll(ctx, s.ID, []*domainplugin.McpTool{
        newMcpTool(t, s.ID, "a"), newMcpTool(t, s.ID, "b"),
    })
    _ = toolRepo.UpsertAll(ctx, s.ID, []*domainplugin.McpTool{
        newMcpTool(t, s.ID, "a"), // b 失踪
    })
    list, _ := toolRepo.ListByServer(ctx, s.ID)
    if len(list) != 1 || list[0].Name != "a" {
        t.Fatalf("expected only [a], got %+v", list)
    }
}
```

- [ ] **Step 2：跑测试确认 PreservesIDs FAIL（当前实现是 delete-then-insert）**

Run：`go test ./infrastructure/storage/plugin/... -run TestMcpTool_UpsertAll -v`
Expected：`TestMcpTool_UpsertAll_PreservesIDsForExisting` FAIL；`TestMcpTool_UpsertAll_RemovesMissing` PASS（delete-then-insert 误打误撞也能过）

- [ ] **Step 3：用 ON CONFLICT 升级 UpsertAll**

```go
import (
    // ...
    "gorm.io/gorm/clause"
)

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

        // 1. 删失踪：server 名下不在 names 列表里的 tool 全删
        delQ := db.Where("server_id = ?", serverID)
        if len(names) > 0 {
            delQ = delQ.Where("name NOT IN ?", names)
        }
        if err := delQ.Delete(&mcpToolModel{}).Error; err != nil { return err }

        // 2. ON CONFLICT (server_id, name) DO UPDATE：保留原 ID
        if len(models) == 0 { return nil }
        return db.Clauses(clause.OnConflict{
            Columns: []clause.Column{{Name: "server_id"}, {Name: "name"}},
            DoUpdates: clause.AssignmentColumns([]string{
                "description", "input_schema_raw", "synced_at",
                // 注意：enabled 不被 upsert 覆盖，保留用户现有偏好
            }),
        }).Create(&models).Error
    })
}
```

注意 `enabled` 故意不在 DoUpdates 列表里——同步覆盖时不重置用户的 enable/disable 偏好。

- [ ] **Step 4：测试通过**

Run：`go test ./infrastructure/storage/plugin/... -run TestMcpTool -v`
Expected：6 个 PASS（含 PreservesIDs）

- [ ] **Step 5：跑全 plugin 包**

Run：`go test ./infrastructure/storage/plugin/...`
Expected：全 PASS

- [ ] **Step 6：commit**

```bash
git add infrastructure/storage/plugin/
git commit -m "feat(storage/plugin): UpsertAll uses ON CONFLICT to preserve IDs and enabled flags"
```

---

## Phase 5：Credential aggregate

### Task 30：crypto.go + crypto_test.go

**Files：**
- Create：`infrastructure/storage/credential/crypto.go`
- Create：`infrastructure/storage/credential/crypto_test.go`

- [ ] **Step 1：创建包**

Run：`mkdir -p infrastructure/storage/credential`

- [ ] **Step 2：先写失败测试 `crypto_test.go`**

```go
package credential

import (
    "crypto/rand"
    "testing"
)

func TestEncryptDecrypt_RoundTrip(t *testing.T) {
    key := make([]byte, 32)
    if _, err := rand.Read(key); err != nil { t.Fatal(err) }

    plain := []byte(`{"key":"hello-world"}`)
    cipher, err := Encrypt(key, plain)
    if err != nil { t.Fatalf("encrypt: %v", err) }
    if string(cipher) == string(plain) { t.Fatal("cipher should differ from plain") }

    got, err := Decrypt(key, cipher)
    if err != nil { t.Fatalf("decrypt: %v", err) }
    if string(got) != string(plain) {
        t.Fatalf("roundtrip mismatch: got %s, want %s", got, plain)
    }
}

func TestEncrypt_NonceRandomness(t *testing.T) {
    key := make([]byte, 32)
    _, _ = rand.Read(key)
    plain := []byte("abc")

    c1, _ := Encrypt(key, plain)
    c2, _ := Encrypt(key, plain)
    if string(c1) == string(c2) {
        t.Fatal("two encryptions of same plaintext should produce different ciphertexts (random nonce)")
    }
}

func TestDecrypt_TamperedFails(t *testing.T) {
    key := make([]byte, 32)
    _, _ = rand.Read(key)
    cipher, _ := Encrypt(key, []byte("hello"))
    cipher[len(cipher)-1] ^= 0xFF // 改最后一字节

    if _, err := Decrypt(key, cipher); err == nil {
        t.Fatal("expected decrypt to fail on tampered ciphertext")
    }
}

func TestDecrypt_TooShortFails(t *testing.T) {
    key := make([]byte, 32)
    _, _ = rand.Read(key)
    if _, err := Decrypt(key, []byte("xx")); err == nil {
        t.Fatal("expected error on too-short ciphertext")
    }
}
```

- [ ] **Step 3：跑测试确认失败**

Run：`go test ./infrastructure/storage/credential/... -run "TestEncrypt|TestDecrypt" -v`
Expected：编译失败（Encrypt/Decrypt 不存在）

- [ ] **Step 4：实现 crypto.go**

```go
// Package credential 实现凭据持久化与解密。
package credential

import (
    "crypto/aes"
    "crypto/cipher"
    "crypto/rand"
    "errors"
    "io"
)

// Encrypt 用 AES-256-GCM 加密 plaintext。
// key 必须是 32 字节。返回 nonce(12B) || ciphertext || tag(16B)。
func Encrypt(key, plaintext []byte) ([]byte, error) {
    block, err := aes.NewCipher(key)
    if err != nil { return nil, err }
    gcm, err := cipher.NewGCM(block)
    if err != nil { return nil, err }
    nonce := make([]byte, gcm.NonceSize())
    if _, err := io.ReadFull(rand.Reader, nonce); err != nil { return nil, err }
    return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// Decrypt 是 Encrypt 的逆。返回明文。
// 输入格式：nonce(12B) || ciphertext || tag(16B)。
func Decrypt(key, ciphertext []byte) ([]byte, error) {
    block, err := aes.NewCipher(key)
    if err != nil { return nil, err }
    gcm, err := cipher.NewGCM(block)
    if err != nil { return nil, err }
    if len(ciphertext) < gcm.NonceSize() {
        return nil, errors.New("credential crypto: ciphertext too short")
    }
    nonce := ciphertext[:gcm.NonceSize()]
    return gcm.Open(nil, nonce, ciphertext[gcm.NonceSize():], nil)
}
```

- [ ] **Step 5：测试通过**

Run：`go test ./infrastructure/storage/credential/... -run "TestEncrypt|TestDecrypt" -v`
Expected：4 个 PASS

- [ ] **Step 6：commit**

```bash
git add infrastructure/storage/credential/crypto.go infrastructure/storage/credential/crypto_test.go
git commit -m "feat(storage/credential): AES-256-GCM encrypt/decrypt with random nonce"
```

---

### Task 31：credential model + mapper + repository CRUD

**Files：**
- Create：`infrastructure/storage/credential/model.go`
- Create：`infrastructure/storage/credential/mapper.go`
- Create：`infrastructure/storage/credential/repository.go`
- Create：`infrastructure/storage/credential/repository_test.go`

- [ ] **Step 1：写 model.go**

```go
package credential

import (
    "time"

    "gorm.io/gorm"
)

type credentialModel struct {
    ID               string         `gorm:"primaryKey;type:uuid"`
    Name             string         `gorm:"not null"`
    Kind             string         `gorm:"not null"`
    EncryptedPayload []byte         `gorm:"type:bytea;not null"`
    CreatedBy        string         `gorm:"not null"`
    CreatedAt        time.Time      `gorm:"not null"`
    UpdatedAt        time.Time      `gorm:"not null"`
    DeletedAt        gorm.DeletedAt `gorm:"index"`
}

func (credentialModel) TableName() string { return "credentials" }
```

- [ ] **Step 2：写 mapper.go**

```go
package credential

import domaincredential "github.com/shinya/shineflow/domain/credential"

func toCredential(m *credentialModel) *domaincredential.Credential {
    return &domaincredential.Credential{
        ID:               m.ID,
        Name:             m.Name,
        Kind:             domaincredential.CredentialKind(m.Kind),
        EncryptedPayload: m.EncryptedPayload,
        CreatedBy:        m.CreatedBy,
        CreatedAt:        m.CreatedAt,
        UpdatedAt:        m.UpdatedAt,
    }
}

func toCredentialModel(c *domaincredential.Credential) *credentialModel {
    return &credentialModel{
        ID:               c.ID,
        Name:             c.Name,
        Kind:             string(c.Kind),
        EncryptedPayload: c.EncryptedPayload,
        CreatedBy:        c.CreatedBy,
        CreatedAt:        c.CreatedAt,
        UpdatedAt:        c.UpdatedAt,
    }
}
```

- [ ] **Step 3：写测试 `repository_test.go`**

```go
package credential_test

import (
    "errors"
    "testing"
    "time"

    "github.com/google/uuid"

    domaincredential "github.com/shinya/shineflow/domain/credential"
    storagecredential "github.com/shinya/shineflow/infrastructure/storage/credential"
    "github.com/shinya/shineflow/infrastructure/storage/storagetest"
)

func newCred(t *testing.T) *domaincredential.Credential {
    t.Helper()
    return &domaincredential.Credential{
        ID:               uuid.NewString(),
        Name:             "openai-key",
        Kind:             domaincredential.CredentialKindAPIKey,
        EncryptedPayload: []byte{1, 2, 3, 4}, // dummy ciphertext
        CreatedBy:        "u",
        CreatedAt:        time.Now().UTC(),
        UpdatedAt:        time.Now().UTC(),
    }
}

func TestCredential_CreateAndGet(t *testing.T) {
    ctx := storagetest.Setup(t)
    repo := storagecredential.NewCredentialRepository()
    c := newCred(t)
    if err := repo.Create(ctx, c); err != nil { t.Fatal(err) }

    got, err := repo.Get(ctx, c.ID)
    if err != nil { t.Fatal(err) }
    if got.Name != c.Name { t.Fatalf("name: %s", got.Name) }
}

func TestCredential_GetNotFound(t *testing.T) {
    ctx := storagetest.Setup(t)
    repo := storagecredential.NewCredentialRepository()
    _, err := repo.Get(ctx, uuid.NewString())
    if !errors.Is(err, domaincredential.ErrCredentialNotFound) {
        t.Fatalf("expected ErrCredentialNotFound: %v", err)
    }
}

func TestCredential_DeleteSoft(t *testing.T) {
    ctx := storagetest.Setup(t)
    repo := storagecredential.NewCredentialRepository()
    c := newCred(t); _ = repo.Create(ctx, c)
    if err := repo.Delete(ctx, c.ID); err != nil { t.Fatal(err) }
    _, err := repo.Get(ctx, c.ID)
    if !errors.Is(err, domaincredential.ErrCredentialNotFound) {
        t.Fatalf("expected NotFound after soft delete: %v", err)
    }
}
```

- [ ] **Step 4：跑测试确认失败**

Run：`go test ./infrastructure/storage/credential/... -run TestCredential -v`

- [ ] **Step 5：写 repository.go**

```go
package credential

import (
    "context"
    "errors"

    "gorm.io/gorm"

    domaincredential "github.com/shinya/shineflow/domain/credential"
    "github.com/shinya/shineflow/infrastructure/storage"
)

type credRepo struct{}

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
```

- [ ] **Step 6：测试通过**

Run：`go test ./infrastructure/storage/credential/... -run TestCredential -v`
Expected：3 个 PASS

- [ ] **Step 7：commit**

```bash
git add infrastructure/storage/credential/
git commit -m "feat(storage/credential): implement CredentialRepository CRUD"
```

---

### Task 32：CredentialResolver

**Files：**
- Create：`infrastructure/storage/credential/resolver.go`
- Modify：`infrastructure/storage/credential/repository_test.go`

- [ ] **Step 1：追加测试**

```go
import (
    // ...
    "encoding/base64"
    "os"

    "github.com/shinya/shineflow/infrastructure/util"
)

func TestResolver_RoundTrip(t *testing.T) {
    // 设环境变量
    keyBytes := make([]byte, 32)
    for i := range keyBytes { keyBytes[i] = byte(i) }
    keyB64 := base64.StdEncoding.EncodeToString(keyBytes)
    t.Setenv("SHINEFLOW_CRED_KEY", keyB64)

    repo := storagecredential.NewCredentialRepository()
    resolver, err := storagecredential.NewResolver(repo)
    if err != nil { t.Fatalf("new resolver: %v", err) }

    // 准备 plaintext + encrypt + 入库
    payload := domaincredential.Payload{"key": "sk-xxx"}
    payloadJSON, _ := util.MarshalToString(payload)
    cipher, err := storagecredential.Encrypt(keyBytes, []byte(payloadJSON))
    if err != nil { t.Fatal(err) }

    ctx := storagetest.Setup(t)
    c := newCred(t); c.EncryptedPayload = cipher
    _ = repo.Create(ctx, c)

    gotCred, gotPayload, err := resolver.Resolve(ctx, c.ID)
    if err != nil { t.Fatalf("resolve: %v", err) }
    if gotCred.ID != c.ID { t.Fatalf("cred id: %s", gotCred.ID) }
    if gotPayload["key"] != "sk-xxx" {
        t.Fatalf("payload: %+v", gotPayload)
    }
}

func TestResolver_MissingEnvKey(t *testing.T) {
    t.Setenv("SHINEFLOW_CRED_KEY", "")
    repo := storagecredential.NewCredentialRepository()
    if _, err := storagecredential.NewResolver(repo); err == nil {
        t.Fatal("expected error when env key missing")
    }
}

func TestResolver_BadKeyLength(t *testing.T) {
    t.Setenv("SHINEFLOW_CRED_KEY", base64.StdEncoding.EncodeToString([]byte("too-short")))
    repo := storagecredential.NewCredentialRepository()
    if _, err := storagecredential.NewResolver(repo); err == nil {
        t.Fatal("expected error for non-32B key")
    }
}

var _ = os.Setenv // keep import
```

- [ ] **Step 2：跑测试确认失败**

Run：`go test ./infrastructure/storage/credential/... -run TestResolver -v`

- [ ] **Step 3：实现 resolver.go**

```go
package credential

import (
    "context"
    "encoding/base64"
    "fmt"
    "os"

    domaincredential "github.com/shinya/shineflow/domain/credential"
    "github.com/shinya/shineflow/infrastructure/util"
)

type resolver struct {
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
    return &resolver{repo: repo, key: key}, nil
}

func (r *resolver) Resolve(
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
```

- [ ] **Step 4：测试通过**

Run：`go test ./infrastructure/storage/credential/... -run TestResolver -v`
Expected：3 个 PASS

- [ ] **Step 5：跑全 credential 包**

Run：`go test ./infrastructure/storage/credential/...`
Expected：全 PASS

- [ ] **Step 6：commit**

```bash
git add infrastructure/storage/credential/
git commit -m "feat(storage/credential): CredentialResolver with env key validation"
```

---

## Phase 6：收尾验收

### Task 33：全 build + 全测试 + push

**Files：** 无

- [ ] **Step 1：全包 build**

Run：`go build ./...`
Expected：无输出

- [ ] **Step 2：全包 vet**

Run：`go vet ./...`
Expected：无输出

- [ ] **Step 3：全包测试（含已有 domain test）**

Run：`go test ./...`
Expected：所有包全绿；testcontainers 拉镜像首次约 30s（如已 cache 5-10s）

- [ ] **Step 4：spec §12 验收清单逐项打勾**

打开 `docs/superpowers/specs/2026-04-26-shineflow-workflow-infra-design.md`，照 §12 一项项核对：

- 9 张表 DDL ✓
- 各 repo 实现 + go build ✓
- 测试覆盖 sentinel error 路径 ✓
- `storage.UseDB` / `storage.WithTx` / `storagetest.Setup` 可用 ✓
- domain §11 的 4 项改动落地（JSON tag / ValueSource codec / repository 注释 / doc.go）✓
- `SHINEFLOW_CRED_KEY` 缺失时报错 ✓
- 全测试 / vet / build 绿 ✓

- [ ] **Step 5：push**

```bash
git push
```

后续 spec 候选见 spec §13。

