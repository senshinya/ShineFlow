# ShineFlow 工作流领域模型设计

- 日期：2026-04-22
- 状态：已定稿，待实现
- 依赖：`2026-04-22-shineflow-init-design.md`（骨架）

## 1. 目标

定义 ShineFlow 工作流的**核心领域模型**：工作流的定义（DSL）、运行时记录、
触发渠道、插件/工具接入的结构体与聚合边界。

本文**只交付领域类型与聚合边界设计**，不涉及执行引擎的调度算法、并发模型、
事务策略、HTTP API 形态。这些由后续 spec 与 writing-plans 阶段展开。

## 2. 范围

### In Scope

- 工作流设计时模型：`WorkflowDefinition` / `WorkflowVersion` / `WorkflowDSL`
- 节点 / 边 / 端口 / 变量引用的结构体
- 节点类型目录（NodeType Registry）的组合方式
- 工作流运行时模型：`WorkflowRun` / `NodeRun`
- 触发：HTTP 直接路径 + `CronJob` 聚合
- 插件：`HttpPlugin` / `McpServer` / `McpTool`
- 秘密管理：`Credential`
- 节点执行器接口 `NodeExecutor`

### Out of Scope（YAGNI）

- 调度器实现（goroutine 池、分布式调度、并行分支的执行）
- 数据库迁移脚本（domain 这一层只给结构体与接口）
- HTTP API 路由（facade 层后续独立 spec）
- 前端画布细节（坐标系、缩放、拖拽交互）
- 工作流模板市场、版本回滚 UI 工作流
- 权限 / 多租户模型
- 可观测性（metrics、tracing 细节）
- 审批流 / 人工介入节点
- 子工作流调用（WorkflowCall 节点）
- 流式输出（LLM / End 节点 SSE）
- 分布式锁、CronJob 高可用
- MCP Server 连接池 / tool 增量同步细节

## 3. 架构决策总览

| #   | 决策                                                                                                   | 理由摘要                                                   |
| --- | ------------------------------------------------------------------------------------------------------ | ---------------------------------------------------------- |
| 1   | **执行模型为 context-passing**（共享变量表），边只表达控制流                                           | AI 场景下可读性 / 调试体验明显优于 items-based 数据流      |
| 2   | **DSL 与 Run 物理分离**，Run 冻结 `VersionID`                                                          | 编辑态不影响在跑任务；审计可回放                           |
| 3   | **工作流版本化**：`Definition`（稳定身份）+ `Version`（draft 可变 / release 冻结，共用版本号序列，至多一条 draft 在头部） | domain 暴露 `SaveVersion / PublishVersion / DiscardDraft` 三原语；"保存并发布"按钮 = application 层用单事务组合 `SaveVersion + PublishVersion`，行锁串行化 |
| 4   | **NodeType 统一目录**：内置节点与插件对调用方是同构的                                                  | 执行引擎 / 前端节点面板只认 NodeType，插件接入无感         |
| 5   | **NodeType 不落物理表**，由 Registry 从 builtin/HttpPlugin/McpTool 现场合成                            | 避免 derived data 导致的同步一致性问题                     |
| 6   | **Node 的参数分两处**：`Config`（设计时静态）+ `Inputs`（运行时数据绑定）                              | UI 渲染控件类型自然分化；Config 内支持 `{{var}}` 模板      |
| 7   | **端口（input/output port）有稳定 UUID**，DSL 引用用 ID、不用 name                                     | 重命名不破坏下游引用                                       |
| 8   | **PortSpec 支持递归 JSON-Schema 子集**（object.properties / array.items）                              | 深路径引用 `node.data.voice_url` 可被设计时校验与补全      |
| 9   | **分支通过节点输出端口表达**：`If` 有 `true/false` 端口、`Switch` 有 N 端口                            | 分支逻辑集中在节点 Config，比"条件边"更好追溯              |
| 10  | **`error` 是所有节点的保留端口**，配合 `ErrorPolicy` 统一错误处理                                      | 重试 / 超时 / 兜底成为正交能力                             |
| 11  | **`start` / `end` 是专属 NodeType**（不是 DSL 级字段）                                                 | 入参/返回契约在画布上可见；执行单元唯一                    |
| 12  | **允许多 End 节点**，`WorkflowRun.EndNodeID` 记录命中哪个                                              | 业务天然有多出口（审批通过/拒绝等）                        |
| 13  | **NodeRun 每次 attempt 一条独立记录**                                                                  | 重试现场完整保留、审计可追                                 |
| 14  | **context 不单独持久化**，由 `WorkflowRun.TriggerPayload + NodeRun.Output + WorkflowRun.Vars` 投影重建 | 避免 O(N²) 重复存储                                        |
| 15  | **大 payload 先走 Postgres JSONB**，DAO 层留 blob 接口                                                 | 简单、可平滑切对象存储                                     |
| 16  | **不建 `Trigger` 聚合**；HTTP 触发由标准路由承担；Cron 建 `CronJob` 聚合                               | Trigger 只在配置项非空时才值得建模                         |
| 17  | **Credential 独立聚合**，秘密加密落库，DSL 与插件定义只引 `CredentialID`                               | 秘密不进 NodeRun 审计、不随 DSL 导出                       |
| 18  | **NodeExecutor 接口对内置与插件同构**；`plugin.http.*` / `plugin.mcp.*.*` 各用一个通用 Executor        | 新增单个插件 = 写一行 HttpPlugin 记录，无需注册新 Executor |

## 4. 目录结构增量

本 spec 引入的代码主要落在 `domain/` 层，并在 `infrastructure/` 增加对应仓储实现。

```
domain/
├── workflow/                         # 工作流定义
│   ├── definition.go                 # WorkflowDefinition
│   ├── version.go                    # WorkflowVersion
│   ├── dsl.go                        # WorkflowDSL / Node / Edge
│   ├── node_ui.go                    # NodeUI
│   ├── port.go                       # PortSpec / SchemaType / PortID
│   ├── value.go                      # ValueSource / RefValue
│   ├── error_policy.go               # ErrorPolicy / FailStrategy
│   └── repository.go                 # 仓储接口
│
├── nodetype/                         # NodeType 目录
│   ├── nodetype.go                   # NodeType 结构
│   ├── registry.go                   # NodeTypeRegistry 接口
│   └── builtin.go                    # 内置 NodeType 常量 Key
│
├── run/                              # 运行时记录
│   ├── workflow_run.go               # WorkflowRun + 状态机
│   ├── node_run.go                   # NodeRun + 状态机
│   ├── context.go                    # Context 投影
│   ├── trigger.go                    # TriggerKind / TriggerRef 的语义
│   └── repository.go
│
├── cron/                             # 定时触发
│   ├── cronjob.go                    # CronJob 聚合
│   └── repository.go
│
├── plugin/                           # 插件
│   ├── http_plugin.go                # HttpPlugin
│   ├── mcp_server.go                 # McpServer
│   ├── mcp_tool.go                   # McpTool
│   └── repository.go
│
├── credential/                       # 秘密
│   ├── credential.go                 # Credential
│   ├── resolver.go                   # CredentialResolver 接口
│   └── repository.go
│
└── executor/                         # 执行器接口（domain 暴露契约）
    ├── executor.go                   # NodeExecutor 接口
    ├── exec_input.go                 # ExecInput / ExecOutput / ExecServices
    └── factory.go                    # ExecutorFactory / 前缀匹配规则
```

具体 executor 实现（如 `llmExecutor` / `httpPluginExecutor` / `mcpToolExecutor`）
放 `infrastructure/executor/`，依赖 domain 定义的接口。

## 5. 命名与基础类型约定

- **所有 ID 是字符串**，采用 UUID v4（或后续可换雪花 ID），domain 层只看到 `string`。
- **时间字段**统一 `time.Time`；DB 层用 `TIMESTAMPTZ`。
- **枚举**用具名 string type（`type RunStatus string`）+ 包内常量，不用 int。
- **JSON 字段**用 `json.RawMessage`，domain 不感知序列化细节；具体结构在 Config/Output 文档里声明。
- **可选字段**用指针（`*time.Time`、`*string`）而非空值约定。

## 6. 设计时模型（DSL 侧）

### 6.1 工作流聚合

```go
// WorkflowDefinition 是工作流的稳定身份，ID 不变，名称/描述可改。
type WorkflowDefinition struct {
    ID          string
    Name        string
    Description string

    DraftVersionID     *string  // 当前 draft 版本；nil 表示当前无 draft（懒创建）
    PublishedVersionID *string  // 当前最高号的 release 版本；nil 表示从未发布

    CreatedBy string
    CreatedAt time.Time
    UpdatedAt time.Time
}

type VersionState string
const (
    VersionStateDraft   VersionState = "draft"
    VersionStateRelease VersionState = "release"
)

// WorkflowVersion 是 DSL 的承载行。draft 可被原地覆盖、可被翻 release、可被硬删；
// release 后所有字段冻结、不可再变。
type WorkflowVersion struct {
    ID           string
    DefinitionID string

    Version  int            // 同 Definition 内单调递增；draft / release 共用同一序列
    State    VersionState
    DSL      WorkflowDSL

    Revision int            // 乐观并发版本号；每次 SaveVersion 自增；翻 release 后冻结

    PublishedAt *time.Time  // State=release 时非空（draft → release 那一刻填）
    PublishedBy *string

    CreatedAt time.Time
    UpdatedAt time.Time
}

// WorkflowDSL 是"纯图"：不含 Name / Description / Version / 时间戳。
type WorkflowDSL struct {
    Nodes []Node
    Edges []Edge
}
```

**版本结构不变式**：

- 同一 Definition 下，所有 `WorkflowVersion` 在 `Version` 字段上线性递增、唯一
- 至多一条 `State="draft"`；若存在，其 `Version` 号 ≥ 所有 `release` 版本的 `Version` 号（draft 永远是序列的"头"）
- `release` 版本不可变：`DSL` / `Version` / `Revision` / `PublishedAt` / `PublishedBy` 全部冻结
- `draft` 版本可被原地覆盖（`SaveVersion`）、可被翻为 release（`PublishVersion`）、可被硬删（`DiscardDraft`）

**懒创建**：新建 `Definition` 不立刻生成任何 `WorkflowVersion`；`DraftVersionID` 初值为 nil。首次 `SaveVersion` 才插入第一条 version。

**画布三按钮 ↔ domain 原语**：

| 前端按钮      | 后端实现                                                                                |
| ------------- | --------------------------------------------------------------------------------------- |
| 暂存          | 直接调 domain `SaveVersion`                                                             |
| 保存并发布    | application 层用例：单事务内组合 `SaveVersion + PublishVersion`（见下方"组合用例"段）   |
| 丢弃草稿      | 直接调 domain `DiscardDraft`                                                            |

domain **不直接暴露**"保存并发布"方法——它是 application 层的组合用例，原子性靠数据库事务 + 行锁，而不是靠合并方法。

**SaveVersion 语义**：

- 创建或覆盖头部 draft；保存的 version 状态默认为 draft
- head 已是 draft → 原地覆盖该条的 DSL，`Revision++`，`UpdatedAt=now`
- head 是 release（或 Definition 还无任何 version）→ append 一条新 draft，`Version=max(Version)+1, Revision=1`
- 调用方需带上 `expectedRevision`（head 非 draft 时传 `0`）；不匹配则拒绝并返回 `ErrRevisionMismatch`，避免静默覆盖
- **不做严格校验**（见 §6.6）

**PublishVersion 语义**：

- 入参只有 `versionID + publishedBy`，**不带 DSL**——只负责把已存在的某条 version 翻为 release
- 校验该 version 必须是该 Definition 内的 head（`Version` 号最大）；非 head 返回 `ErrNotHead`
- 若该 version 已是 release → **幂等成功**（同一 versionID 重复 publish 安全）
- 若该 version 是 draft → 做严格校验（见 §6.6），通过后：
  - `UPDATE versions SET state='release', published_at=NOW(), published_by=$1 WHERE id=$VersionID`
  - 同事务 `UPDATE definitions SET draft_version_id=NULL, published_version_id=$VersionID WHERE id=$DefID`
- 不复制 DSL、不新增 row、`Version` / `Revision` 不变

**"保存并发布"组合用例**（application 层持有，本 spec 不展开实现细节）：

- 在单个 DB 事务内顺序调：`SaveVersion(defID, dsl, expRev)` → 拿返回的 `*WorkflowVersion` → `PublishVersion(version.ID, publishedBy)`
- 共享一个事务上下文 → 行锁串行化并发的"保存并发布"，杜绝"Alice 保存后被 Bob 抢先覆盖、再被 Alice 发布"的 race
- 任一步失败整事务回滚
- 本 spec 对 domain 层的硬约束：**仓储方法不能在内部各自起事务**，必须接受外部传入的事务上下文（具体 plumbing 形态——`context.Context` 携带 `*sql.Tx` / `WithTx(tx)` 包装层等——由 application 层 spec 决定）

**DiscardDraft 语义**：

- 若当前有 draft：`DELETE FROM versions WHERE id=$DraftID AND state='draft'`，并 `Definition.DraftVersionID` 置 nil
- 若当前无 draft：**静默成功（幂等）**，不返回错误
- 已存在的、绑该 draft 的 try-run 行**不动**（`NodeRun` 历史保留，便于审计）
- 后续若再 `SaveVersion`，新 draft 的 `Version` 由当前剩余 row 的 `max(Version)+1` 计算，号段可能"复用"——有意简化（discard 即遗忘）

**运行绑定**：触发器（webhook / api / cron）只读 `Definition.PublishedVersionID`，draft 绝不会被自动触发选中。
UI 允许"试运行 draft"——走 `RunService.Start(versionID=<draftID>)` 的手动分支，Run 上标 `TriggerKind=manual`。
引擎在 `Start` 入口处把 `WorkflowVersion.DSL` 快照到内存执行上下文，运行期间对该 draft 的 `SaveVersion` / `DiscardDraft` 不影响 in-flight run。

### 6.2 Node

```go
type Node struct {
    ID      string                     // 在单个 DSL 内唯一
    TypeKey string                     // 见 §7；如 "builtin.llm" / "plugin.http.hp_001"
    TypeVer string                     // NodeType 版本号；兼容演进用
    Name    string                     // 用户起的别名；展示用，可重复

    Config  json.RawMessage            // 符合 NodeType.ConfigSchema；字符串字段支持 {{var}} 模板
    Inputs  map[string]ValueSource     // key = NodeType.InputSchema 的 PortID（不是 name！）

    ErrorPolicy *ErrorPolicy           // nil 表示默认策略（见 §10）
    UI          NodeUI                 // 画布渲染元数据
}

type NodeUI struct {
    X      float64
    Y      float64
    Width  *float64   // nil = 用 NodeType 默认宽度
    Height *float64   // nil = 用默认高度；大多数节点恒为 nil
}
```

**关键点**：

- `Inputs` 的 key 是 **PortID（UUID）**，不是 port name。这样 NodeType 的 port 改名不破坏 DSL。
- `Config` 内嵌 `{{var}}` 模板字符串由引擎在节点执行前统一展开；Executor 拿到的是已展开版本。
- `TypeVer`：NodeType 演进时，老 DSL 指向老版本保证兼容。v1 可以先全填 `"1"`，演进规则在 NodeType Registry 章节细化。

### 6.3 Edge

```go
type Edge struct {
    ID       string
    From     string   // 源 Node.ID
    FromPort string   // 源节点的输出端口名；默认 "default"；If 用 "true"/"false"；保留 "error"
    To       string   // 目标 Node.ID
    // 不需要 ToPort：context-passing 模型下目标节点直接读共享变量表，不存在"按输入端口接线"的概念
}
```

**端口模型**：

- 每个 NodeType 声明自己的 `Ports []string`（见 §7）。`default` / `error` 是保留名。
- 执行引擎拿到 NodeRun 结果 `ExecOutput.FiredPort` → 找 `(From=this, FromPort=that)` 的所有出边 → 触发目标节点。
- 若 fire 了某 port 但没有出边：`error` port 特殊——整个 Run 失败；其他 port 只是"分支断头"，不视为异常。

### 6.4 PortSpec 与 SchemaType

```go
// 每个输入/输出 port 的静态声明（出现在 NodeType 里）
type PortSpec struct {
    ID       string      // 稳定 UUID；DSL 引用用它
    Name     string      // 显示名；用户可改；不参与运行时解析
    Type     SchemaType
    Required bool
    Desc     string
}

// 最小 JSON Schema 子集；允许嵌套
type SchemaType struct {
    Type       string                  // "string" | "number" | "integer" | "boolean" | "object" | "array" | "any"
    Properties map[string]*SchemaType  // Type == "object" 时；key = 子字段名
    Items      *SchemaType             // Type == "array" 时
    Enum       []any                   // 可选；Type 为 string/number 时的枚举约束
}
```

**设计时能力**：

- 前端根据 `InputSchema` 渲染"变量绑定槽"，根据 `OutputSchema` 渲染"可引用字段树"。
- 用户拖一条 ref 从 `plugin1.data.voice_url` 引到下游节点时，前端用 `OutputSchema` 走树验证 path 合法。
- **运行时不做强 schema 校验**（档位 2），允许 LLM 等模糊返回——只做类型提示、不做硬 gate。

### 6.5 ValueSource / RefValue

```go
type ValueKind string
const (
    ValueKindLiteral  ValueKind = "literal"
    ValueKindRef      ValueKind = "ref"
    ValueKindTemplate ValueKind = "template"  // 可选：整字段是模板字符串（含 {{var}}）
)

type ValueSource struct {
    Kind  ValueKind
    Value any        // literal: 直接值；ref: RefValue；template: 模板字符串
}

type RefValue struct {
    NodeID string   // 被引用节点的 ID
    PortID string   // 被引用节点的 OutputSchema 里某个 port 的 ID（不是 name）
    Path   string   // 对象类型时的深路径，如 "data.voice_url"；可为空
    Name   string   // 冗余的 port 名；仅用于 UI 展示 / 可读性；不参与解析
}
```

**求值顺序**（引擎执行节点前）：

1. 对 `Node.Inputs` 的每个 `ValueSource` 求值：
   - `literal` → 直接用
   - `ref` → 查对应 `NodeRun.Output`（必要时按 `Path` 取子字段）
   - `template` → 用 context 变量表展开 `{{var}}`
2. 对 `Node.Config` 做模板展开（遍历 JSON 中字符串值，含 `{{var}}` 的替换为当前 context 值）
3. 得到的 `(resolvedConfig, resolvedInputs)` 传给 `NodeExecutor.Execute`

**变量表 context 的三个前缀**：

```
trigger.<key>         ← WorkflowRun.TriggerPayload 的顶层字段
nodes.<nodeID>.<port> ← 该节点此次 Run 中最新 attempt 的 Output；port 按 PortSpec 的 Name 暴露
vars.<key>            ← SetVariable 节点显式写入
```

`{{var}}` 模板里写的是 **Name 级路径**（`{{nodes.llm1.text}}`）——这是给用户的、用 name 方便；
结构化 `RefValue` 写的是 **PortID**——这是给机器的、稳定。两者在前端编辑时互相转换。

### 6.6 DSL 校验时机

| 操作                  | 校验严格度                                                                                  |
| --------------------- | ------------------------------------------------------------------------------------------- |
| `SaveVersion`         | 宽松：JSON 合法 + 能反序列化为 `WorkflowDSL` 即接受。允许悬空引用、缺必填输入、孤立节点、未知 NodeType |
| `PublishVersion`      | 严格：见下表，逐项验证，任一失败即拒绝并一次性返回所有违例（在组合事务中即触发回滚）        |
| Try-Run（手动跑 draft） | 宽松同 `SaveVersion`；运行时 Engine / Executor 自然报错                                     |

**严格校验规则**（`PublishVersion` 必过）：

1. 至少 1 个 `builtin.start`、至少 1 个 `builtin.end`
2. 所有 `Node.ID` / `Edge.ID` / `PortID` 在 DSL 内唯一
3. 所有 `Edge.From/To`、`RefValue.NodeID`、`RefValue.PortID` 指向 DSL 内真实存在的节点 / 端口
4. 所有 `Edge.FromPort` 是源节点 NodeType 声明的端口
5. 所有 `Required=true` 的输入端口都绑了非空 `ValueSource`
6. 所有 `Node.TypeKey` 能在 `NodeTypeRegistry` 解析到（含插件 NodeType）
7. `OnFinalFail=fallback` 仅出现在声明了 `default` 端口的 NodeType 上（与 §10.2 不变式一致）
8. 节点之间不存在控制流环（v1 循环只能由 `builtin.loop` 节点承载）

## 7. NodeType 目录

### 7.1 NodeType 结构

```go
type NodeType struct {
    Key         string       // 全局唯一；如 "builtin.llm" / "plugin.http.hp_001"
    Version     string       // NodeType 自身的版本；老 DSL 绑老 NodeType 实现
    Name        string
    Description string
    Category    string       // "AI" | "Tool" | "Control" | "Basic"
    Builtin     bool

    ConfigSchema json.RawMessage // JSON Schema，前端用它渲染"配置面板"（可为空对象）
    InputSchema  []PortSpec      // 运行时数据输入
    OutputSchema []PortSpec      // 产出字段
    Ports        []string        // 输出控制端口；默认 ["default","error"]；If 为 ["true","false","error"]
}
```

`NodeType` **不对应物理表**。由 `NodeTypeRegistry` 在被问到时合成。

### 7.2 NodeTypeRegistry 接口

```go
type NodeTypeRegistry interface {
    Get(key string) (*NodeType, bool)
    List(filter NodeTypeFilter) []*NodeType

    // 失效通知：插件变更时调用
    Invalidate(key string)
    InvalidatePrefix(prefix string)
}

type NodeTypeFilter struct {
    Category    string
    Builtin     *bool
    KeyPrefixes []string
}
```

### 7.3 合成来源

| Key 模式                           | 来源                                        | 合成器                                           |
| ---------------------------------- | ------------------------------------------- | ------------------------------------------------ |
| `builtin.*`                        | 代码 init 时注册到 registry 的静态 map      | 代码内常量                                       |
| `plugin.http.<pluginId>`           | `HttpPluginRepository.Get(pluginId)`        | `projectHttpPlugin(*HttpPlugin) *NodeType`       |
| `plugin.mcp.<serverId>.<toolName>` | `McpToolRepository.GetByServerAndName(...)` | `projectMcpTool(*McpTool, *McpServer) *NodeType` |

合成后结果进 registry 内存缓存；HttpPlugin / McpServer / McpTool 变更时对应 Invalidate。

### 7.4 内置 NodeType 初始清单（v1）

| Key                    | 职责                                                      | Ports                                   |
| ---------------------- | --------------------------------------------------------- | --------------------------------------- |
| `builtin.start`        | 工作流入口；OutputSchema = 工作流入参契约                 | `["default"]`                           |
| `builtin.end`          | 工作流出口；InputSchema = 工作流返回契约                  | `[]`（无出边）                          |
| `builtin.llm`          | 调 LLM；Config 持 model/temperature/system_prompt 等      | `["default","error"]`                   |
| `builtin.if`           | 二分分支；Config 持条件表达式                             | `["true","false","error"]`              |
| `builtin.switch`       | 多分支；Config 持 N 个 case 表达式                        | `[case_1, case_2, ..., default, error]` |
| `builtin.loop`         | 循环（v1 仅支持 foreach 数组）                            | `["default","error"]`                   |
| `builtin.code`         | 沙箱内跑一段用户脚本（JS/Python，v1 可仅 JS）             | `["default","error"]`                   |
| `builtin.set_variable` | 往 `vars.*` 写变量                                        | `["default"]`                           |
| `builtin.http_request` | 通用 HTTP 调用（不同于 HttpPlugin：这个是画布上临时写的） | `["default","error"]`                   |

> 更多内置节点（如 `builtin.merge` / `builtin.split` / `builtin.template`）按需加入，不属于 v1 必需。

### 7.5 投影规则（从插件到 NodeType）

```go
func projectHttpPlugin(p *HttpPlugin) *NodeType {
    return &NodeType{
        Key:          "plugin.http." + p.ID,
        Version:      "1",
        Name:         p.Name,
        Description:  p.Description,
        Category:     "Tool",
        Builtin:      false,
        ConfigSchema: json.RawMessage(`{}`),   // HTTP 插件的"配置"已包含在 HttpPlugin 本体里
        InputSchema:  p.InputSchema,
        OutputSchema: p.OutputSchema,
        Ports:        []string{"default", "error"},
    }
}

func projectMcpTool(t *McpTool, s *McpServer) *NodeType {
    return &NodeType{
        Key:          fmt.Sprintf("plugin.mcp.%s.%s", s.ID, t.Name),
        Version:      "1",
        Name:         fmt.Sprintf("%s / %s", s.Name, t.Name),
        Description:  t.Description,
        Category:     "Tool",
        Builtin:      false,
        ConfigSchema: json.RawMessage(`{}`),
        InputSchema:  mcpSchemaToPortSpecs(t.InputSchemaRaw),   // JSON Schema → []PortSpec 降维
        OutputSchema: []PortSpec{{
            ID:   "result-<stable-hash>",   // 稳定 ID，基于 serverId+toolName 派生
            Name: "result",
            Type: SchemaType{Type: "object"},
        }},
        Ports: []string{"default", "error"},
    }
}
```

## 8. 运行时模型（Run 侧）

### 8.1 WorkflowRun 聚合

```go
type WorkflowRun struct {
    ID           string
    DefinitionID string
    VersionID    string   // 冻结：Run 绑定到创建时的 WorkflowVersion

    TriggerKind    TriggerKind     // manual | webhook | api | cron
    TriggerRef     string          // 语义见 §9
    TriggerPayload json.RawMessage // 启动时的入参；即 start 节点的 "outputs"

    Status    RunStatus
    StartedAt *time.Time
    EndedAt   *time.Time

    Vars      json.RawMessage   // SetVariable 节点写入的 vars.*
    EndNodeID *string           // 走到的 End 节点 ID；多 End 支持下非空
    Output    json.RawMessage   // 命中的 End 节点的 Inputs 求值结果；对外返回值
    Error     *RunError         // Status == failed 时非空

    CreatedBy string   // 手动触发的用户 ID；cron 时为系统标识
    CreatedAt time.Time
}

type RunError struct {
    NodeID    string
    NodeRunID string
    Code      string   // "node_exec_failed" | "timeout" | "cancelled" | "version_not_published" | ...
    Message   string
    Details   json.RawMessage
}

type RunStatus string
const (
    RunStatusPending   RunStatus = "pending"
    RunStatusRunning   RunStatus = "running"
    RunStatusSuccess   RunStatus = "success"
    RunStatusFailed    RunStatus = "failed"
    RunStatusCancelled RunStatus = "cancelled"
)
```

### 8.2 NodeRun 实体

```go
type NodeRun struct {
    ID          string
    RunID       string    // → WorkflowRun.ID
    NodeID      string    // DSL 里的 Node.ID
    NodeTypeKey string    // 冗余：查询时不用 join DSL
    Attempt     int       // 从 1 起；同 (RunID, NodeID) 下递增

    Status    NodeRunStatus
    StartedAt *time.Time
    EndedAt   *time.Time

    ResolvedConfig json.RawMessage  // 模板展开后的 Config
    ResolvedInputs json.RawMessage  // ValueSource 求值后的 Inputs（不含秘密，见 §11.4）
    Output         json.RawMessage  // 节点产出；fallback 生效时是 fallback 值
    FiredPort      string           // "default" / "true" / "false" / "error" / ...

    FallbackApplied bool             // true 表示 ErrorPolicy.OnFinalFail=fallback 生效
    Error           *NodeError       // Status == failed 时非空；fallback 生效也会保留，用于审计

    ExternalRefs []ExternalRef       // LLM trace_id / HTTP request_id / MCP tool_call_id
}

type NodeError struct {
    Code    string         // "exec_failed" | "timeout" | "cancelled" | "validation_failed" | ...
    Message string
    Details json.RawMessage
}

type ExternalRef struct {
    Kind string  // "llm_call" | "http_request" | "mcp_tool"
    Ref  string
}

type NodeRunStatus string
const (
    NodeRunStatusPending NodeRunStatus = "pending"
    NodeRunStatusRunning NodeRunStatus = "running"
    NodeRunStatusSuccess NodeRunStatus = "success"
    NodeRunStatusFailed  NodeRunStatus = "failed"
    NodeRunStatusSkipped NodeRunStatus = "skipped"  // 因为 If/Switch 走了别的分支
)
```

### 8.3 聚合与存储关系

- **领域上**：`WorkflowRun` 是聚合根，`NodeRun` 是聚合内子实体。所有对 NodeRun 的写入必须经过 `WorkflowRunRepository`。
- **存储上**：`workflow_runs` 和 `node_runs` 是两张表，`node_runs.run_id` FK 关联。
- `WorkflowRun` struct **不内嵌** `NodeRuns []NodeRun` 字段；NodeRun 列表按需通过仓储获取。

### 8.4 context 变量表的重建

运行时 context 是内存 `map[string]any`，来自三处：

```
trigger.*       ← WorkflowRun.TriggerPayload
nodes.<id>.*    ← 对每个 NodeID，取 (RunID, NodeID) 下最新 Attempt 且 Status=success 的 NodeRun.Output
vars.<key>      ← WorkflowRun.Vars
```

**审计 / 回放时**：给定 Run，按上述三源投影就能复现每一步开始前的变量表。**不单独持久化 context 快照**。

### 8.5 大 payload 的存储策略

- v1 全部写入 Postgres JSONB 列。
- 仓储层定义 `BlobStore` 接口预留扩展：

```go
type BlobStore interface {
    Save(ctx context.Context, key string, data []byte) (ref string, err error)
    Load(ctx context.Context, ref string) ([]byte, error)
    Delete(ctx context.Context, ref string) error
}
```

NodeRun 的 `Output` / `ResolvedInputs` / `ResolvedConfig` 未来超阈值可切存 blob，
DB 里改存 `ref` 字符串。这个切换**领域层不感知**。

## 9. 触发模型

### 9.1 不建 Trigger 聚合

HTTP 触发（webhook / 同步 API）由标准路由承担，**无需建模**：

```
POST /api/v1/workflows/:definitionId/runs         → 异步：立刻返回 {run_id}
POST /api/v1/workflows/:definitionId/runs:sync    → 同步：阻塞到 Run 结束，返回 Output
```

鉴权走标准 `Authorization` 头 + API Token，Token 是用户级资源（独立聚合，后续 spec 详述），
与工作流解耦。

### 9.2 CronJob 聚合

```go
type CronJob struct {
    ID           string
    DefinitionID string              // 绑 Definition；fire 时读 PublishedVersionID
    Name         string
    Description  string

    Expression   string              // 标准 5 段 cron
    Timezone     string              // IANA，如 "Asia/Shanghai"
    Payload      json.RawMessage     // 固定入参；可为空对象

    Enabled      bool

    NextFireAt   *time.Time          // 调度器维护
    LastFireAt   *time.Time
    LastRunID    *string

    CreatedBy    string
    CreatedAt    time.Time
    UpdatedAt    time.Time
}
```

**字段取舍**：`MaxConcurrency` / `StartAt` / `EndAt` / `Catchup` 暂不加；真遇到再扩。

**fire 通路**：

```
调度器扫表：SELECT ... WHERE enabled AND next_fire_at <= now() FOR UPDATE SKIP LOCKED LIMIT N
  ↓
读 Definition.PublishedVersionID（为 nil 则跳过并记 error log）
  ↓
RunService.Start(versionID, TriggerKind=cron, TriggerRef=cronJob.ID, payload=cronJob.Payload)
  ↓
更新 LastFireAt / LastRunID / NextFireAt（下一次触发点）
```

### 9.3 TriggerKind / TriggerRef 的语义

Run 上的标签字段，不指向 Trigger 实体：

| `TriggerKind` | `TriggerRef` 含义       |
| ------------- | ----------------------- |
| `manual`      | 操作用户 ID             |
| `webhook`     | 发起调用的 API Token ID |
| `api`         | 发起调用的 API Token ID |
| `cron`        | CronJob ID              |

**RunService.Start() 是唯一创建 Run 的入口**，手动 / HTTP / Cron 全走同一条路。

> `RunService` 属 application 层，不在本 spec 的类型范围内；本文只要求 domain 侧提供足够的聚合和仓储支撑它存在。

## 10. 错误处理模型

### 10.1 ErrorPolicy

```go
type ErrorPolicy struct {
    Timeout       time.Duration   // 0 = 不设
    MaxRetries    int             // 0 = 不重试
    RetryBackoff  BackoffKind     // "fixed" | "exponential"
    RetryDelay    time.Duration   // fixed 时的固定间隔；exponential 时的初始间隔

    OnFinalFail    FailStrategy
    FallbackOutput map[string]any  // OnFinalFail == "fallback" 时生效
}

type BackoffKind string
const (
    BackoffFixed       BackoffKind = "fixed"
    BackoffExponential BackoffKind = "exponential"
)

type FailStrategy string
const (
    FailStrategyFireErrorPort FailStrategy = "fire_error_port"  // 默认
    FailStrategyFallback      FailStrategy = "fallback"
    FailStrategyFailRun       FailStrategy = "fail_run"
)
```

### 10.2 三种 FailStrategy 的语义

| 策略              | 最终失败时                                             | Run 是否继续 | 适合场景                 |
| ----------------- | ------------------------------------------------------ | ------------ | ------------------------ |
| `fire_error_port` | fire `error` 端口；有出边则继续；无出边则 Run 失败     | 看 DSL       | 默认；由画布决定容错路径 |
| `fallback`        | 写入 `FallbackOutput` 作为 Output，fire `default` 端口 | 是           | 外部可见容错、保 UX      |
| `fail_run`        | 立刻结束整个 Run                                       | 否           | 关键路径（扣费、订单等） |

**不变式**：`fallback` 策略只能用在**声明了 `default` 端口**的 NodeType 上（绝大多数节点）。DSL 校验阶段要对 `Switch` 之类没有 `default` 端口的节点拒绝 `OnFinalFail = "fallback"`。

### 10.3 每次 attempt 独立记录

```
attempt=1 → failed (timeout)
attempt=2 → failed (rate_limit)
attempt=3 → success
```

三条 NodeRun，共享同一个 `(RunID, NodeID)`，`Attempt` 递增。
审计时能完整看到重试历史。

### 10.4 `fallback` 生效时的 NodeRun 语义

- `Status = failed`（记录真实失败，不骗审计）
- `FallbackApplied = true`
- `Output = fallbackOutput`（给下游读到的值）
- `FiredPort = "default"`
- `Error` 非空，保留最后一次真实的错误细节

## 11. 插件与秘密管理

### 11.1 HttpPlugin

```go
type HttpPlugin struct {
    ID          string
    Name        string
    Description string

    // 请求构造
    Method       string             // GET/POST/PUT/DELETE
    URL          string             // 支持 {{var}} 模板
    Headers      map[string]string  // value 支持 {{var}}
    QueryParams  map[string]string
    BodyTemplate string              // 支持 {{var}}；Content-Type 由 Headers 声明

    // 认证
    AuthKind     string              // "none" | "api_key" | "bearer" | "basic"
    CredentialID *string             // AuthKind != none 时非空

    // 契约
    InputSchema  []PortSpec
    OutputSchema []PortSpec

    // 响应映射
    ResponseMapping map[string]string  // OutputSchema port name → JSONPath
                                       // 未映射的 port 尝试按同名顶层字段取

    Enabled     bool
    CreatedBy   string
    CreatedAt   time.Time
    UpdatedAt   time.Time
}
```

### 11.2 MCP Server & Tool

```go
type McpServer struct {
    ID          string
    Name        string

    Transport   McpTransport       // "stdio" | "http" | "sse"
    Config      json.RawMessage    // 按 Transport 解码（命令/URL/...）

    CredentialID *string

    Enabled       bool
    LastSyncedAt  *time.Time
    LastSyncError *string          // 上次同步 tool 列表失败的错误

    CreatedBy string
    CreatedAt time.Time
    UpdatedAt time.Time
}

type McpTool struct {
    ID          string
    ServerID    string
    Name        string             // MCP 协议返回的 tool 名
    Description string

    InputSchemaRaw json.RawMessage // MCP 返回的原生 JSON Schema，不预先降维

    Enabled   bool                 // 允许用户屏蔽某个 tool
    SyncedAt  time.Time
}
```

**注意**：`McpTool` 是系统根据 `McpServer` 同步而来，不由用户直接 CRUD；用户只能 enable/disable。

### 11.3 Credential

```go
type Credential struct {
    ID          string
    Name        string
    Kind        CredentialKind

    EncryptedPayload []byte    // 对称加密；密钥从环境变量/KMS 读，不落 DB

    CreatedBy string
    CreatedAt time.Time
    UpdatedAt time.Time
}

type CredentialKind string
const (
    CredentialKindAPIKey CredentialKind = "api_key" // Payload: {Key}
    CredentialKindBearer CredentialKind = "bearer"  // Payload: {Token}
    CredentialKindBasic  CredentialKind = "basic"   // Payload: {Username, Password}
    CredentialKindCustom CredentialKind = "custom"  // Payload: map[string]string
)

type CredentialResolver interface {
    Resolve(ctx context.Context, credID string) (Credential, Payload, error)
}
```

**密钥管理**：加解密密钥从 `SHINEFLOW_CRED_KEY` 环境变量读（v1）；后续可切 KMS。

### 11.4 敏感字段隔离规则

秘密**从不进入变量表 context**、**从不进入 `ResolvedInputs / ResolvedConfig`**。这由以下两条结构性约束保证：

1. **唯一出口**：HttpPlugin / LLM / MCP Executor 需要凭证时，只能通过 `ExecServices.Credentials.Resolve(credID)` 拿。解密后的明文只在 Executor 内部局部变量中使用，完成请求后即释放，不写回 `ExecInput` 或任何落库字段。
2. **输入无路径**：`ValueSource` 的三种 Kind（literal / ref / template）都没有一种能指向 Credential——Credential 既不是 `NodeRun.Output`、也不是 `TriggerPayload`、也不是 `Vars`。用户在画布上**根本无法**把 Credential 值绑到某个 Input，也无法在 `{{var}}` 模板里引用到它。

因此无需在落库前额外做 JSON 关键字扫描。

## 12. 节点执行器

### 12.1 接口

```go
type NodeExecutor interface {
    Execute(ctx context.Context, in ExecInput) (ExecOutput, error)
}

type ExecInput struct {
    NodeType *nodetype.NodeType  // 当前节点的类型元信息
    Config   json.RawMessage     // 模板已展开
    Inputs   map[string]any      // ValueSource 已求值
    Run      RunInfo             // 只读：runID / versionID / definitionID / triggerKind
    Services ExecServices
}

type RunInfo struct {
    RunID        string
    NodeRunID    string
    Attempt      int
    DefinitionID string
    VersionID    string
    TriggerKind  TriggerKind
    TriggerRef   string
}

type ExecServices struct {
    Credentials CredentialResolver
    Logger      Logger
    HTTPClient  HTTPClient
    // 其他随扩展而增：LLM client、MCP client pool...
}

type ExecOutput struct {
    Outputs      map[string]any   // 对齐 NodeType.OutputSchema
    FiredPort    string           // 默认 "default"
    ExternalRefs []ExternalRef
}
```

### 12.2 注册与前缀匹配

```go
type ExecutorFactory func(nt *nodetype.NodeType) NodeExecutor

type ExecutorRegistry interface {
    Register(keyPattern string, factory ExecutorFactory)
    Build(nt *nodetype.NodeType) (NodeExecutor, error)
}
```

`keyPattern` 支持：

- 精确匹配：`"builtin.llm"`
- 前缀通配：`"plugin.http.*"` / `"plugin.mcp.*.*"`

优先级：精确匹配 > 前缀匹配（最长前缀优先）。

**初始注册表（v1）**：

```go
reg.Register("builtin.start",         newStartExecutor)
reg.Register("builtin.end",           newEndExecutor)
reg.Register("builtin.llm",           newLLMExecutor)
reg.Register("builtin.if",            newIfExecutor)
reg.Register("builtin.switch",        newSwitchExecutor)
reg.Register("builtin.loop",          newLoopExecutor)
reg.Register("builtin.code",          newCodeExecutor)
reg.Register("builtin.set_variable",  newSetVariableExecutor)
reg.Register("builtin.http_request",  newHttpRequestExecutor)
reg.Register("plugin.http.*",         newHttpPluginExecutor)
reg.Register("plugin.mcp.*.*",        newMcpToolExecutor)
```

## 13. 仓储接口（domain 侧契约）

### 13.1 WorkflowRepository

```go
type WorkflowRepository interface {
    // Definition
    CreateDefinition(ctx context.Context, def *WorkflowDefinition) error
    GetDefinition(ctx context.Context, id string) (*WorkflowDefinition, error)
    ListDefinitions(ctx context.Context, filter DefinitionFilter) ([]*WorkflowDefinition, error)
    UpdateDefinition(ctx context.Context, def *WorkflowDefinition) error
    DeleteDefinition(ctx context.Context, id string) error

    // Version
    GetVersion(ctx context.Context, id string) (*WorkflowVersion, error)
    ListVersions(ctx context.Context, definitionID string) ([]*WorkflowVersion, error) // 按 Version 倒序，含 draft

    // SaveVersion：保存（创建或覆盖）头部 draft；保存的 version 状态默认为 draft。
    //   - head 为 draft → 原地覆盖
    //   - head 为 release（或无任何 version）→ append 一条新 draft（Version=max+1, Revision=1）
    // expectedRevision：head 为 draft 时必须等于其 Revision；head 非 draft（含无 version）时传 0。
    // 不匹配返回 ErrRevisionMismatch（带服务端 latest revision 便于前端冲突提示）。
    SaveVersion(ctx context.Context, definitionID string, dsl WorkflowDSL, expectedRevision int) (*WorkflowVersion, error)

    // PublishVersion：把指定 version 翻为 release。入参不带 DSL。
    //   - versionID 必须是该 Definition 的 head（最大 Version 号）；否则返回 ErrNotHead
    //   - 已是 release → 幂等成功
    //   - 是 draft → 严格校验（见 §6.6），通过后翻 state；失败返回 ErrDraftValidation
    // "保存并发布"按钮由 application 层用单事务组合 SaveVersion + PublishVersion 实现。
    PublishVersion(ctx context.Context, versionID, publishedBy string) (*WorkflowVersion, error)

    // DiscardDraft：若有 draft 则硬删并清 Definition.DraftVersionID；
    // 若无 draft 则静默成功（幂等，不返回错误）。
    DiscardDraft(ctx context.Context, definitionID string) error
}
```

### 13.2 WorkflowRunRepository

```go
type WorkflowRunRepository interface {
    // WorkflowRun
    Create(ctx context.Context, run *WorkflowRun) error
    Get(ctx context.Context, id string) (*WorkflowRun, error)
    List(ctx context.Context, filter RunFilter) ([]*WorkflowRun, error)
    UpdateStatus(ctx context.Context, id string, status RunStatus, opts ...RunUpdateOpt) error
    SaveEndResult(ctx context.Context, id, endNodeID string, output json.RawMessage) error
    SaveVars(ctx context.Context, id string, vars json.RawMessage) error
    SaveError(ctx context.Context, id string, e RunError) error

    // NodeRun 通过聚合根访问，不独立仓储
    AppendNodeRun(ctx context.Context, runID string, nr *NodeRun) error
    UpdateNodeRunStatus(ctx context.Context, runID, nodeRunID string, status NodeRunStatus, opts ...NodeRunUpdateOpt) error
    SaveNodeRunOutput(ctx context.Context, runID, nodeRunID string, output json.RawMessage, firedPort string) error
    GetNodeRun(ctx context.Context, runID, nodeRunID string) (*NodeRun, error)
    ListNodeRuns(ctx context.Context, runID string) ([]*NodeRun, error)
    GetLatestNodeRun(ctx context.Context, runID, nodeID string) (*NodeRun, error) // 用于 context 投影
}
```

### 13.3 其他

- `CronJobRepository`：`CRUD` + `ClaimDue(ctx, limit int) ([]*CronJob, error)`（调度器用）
- `HttpPluginRepository` / `McpServerRepository` / `McpToolRepository`：常规 CRUD + 按条件 List
- `CredentialRepository`：CRUD（`Get` 返回加密后的，解密由 `CredentialResolver` 服务做）

## 14. 不变式（Invariants）

- **Run 级**：`WorkflowRun.VersionID` 非空且指向存在的 `WorkflowVersion`；Run 创建后 VersionID 不可改。
- **Status 单调**：`WorkflowRun.Status` 不能从终态（success/failed/cancelled）回退到 running。
- **NodeRun 归属**：`NodeRun.RunID` 必须存在；`(RunID, NodeID, Attempt)` 唯一。
- **EndNodeID 一致**：`Status == success` 时 `EndNodeID` 非空且 DSL 里确实有该 End 节点。
- **Draft 与 Release 互斥**：`WorkflowVersion.State` 要么 `draft` 要么 `release`；`State=draft` 时 `PublishedAt` / `PublishedBy` 必为 nil。
- **至多一条 draft**：同一 Definition 下 `State=draft` 的 `WorkflowVersion` 最多 1 条；若存在，其 `Version` 号 ≥ 该 Definition 内所有 `release` 版本的 `Version` 号。
- **Release 不可变**：`State=release` 后该 Version 的 `DSL` / `Version` / `Revision` / `PublishedAt` / `PublishedBy` 全部冻结，仓储层不暴露任何修改入口。
- **只能发布 head**：`PublishVersion` 仅接受该 Definition 内 `Version` 号最大的那条 version；非 head 不可被发布（draft 永远是 head；非 head 必为 release，已 release 不重发）。
- **Credential 密文不出域**：`Credential.EncryptedPayload` 只允许 `CredentialResolver` 读取，不得出现在 `ResolvedInputs/Config` 中。
- **NodeType 不建表**：`NodeTypeRegistry` 实现不得写任何 NodeType 表；所有 NodeType 由内置 + 插件投影得到。

## 15. 版本演进策略

本 spec 里一共出现了**三种相互独立的"版本"**，这里统一澄清以防混淆：

| 名字                         | 类型                  | 谁的版本                     | v1 形态                    |
| ---------------------------- | --------------------- | ---------------------------- | -------------------------- |
| `WorkflowVersion.Version`    | `int`                 | 用户工作流的版本号（draft / release 共用） | 同 Definition 内单调递增；discard 后号段可复用 |
| `NodeType.Version`           | `string`              | NodeType 契约本身的版本      | v1 全填 `"1"`              |
| `WorkflowDSL.$schemaVersion` | `string`（JSON 字段） | DSL 序列化结构的 schema 版本 | v1 固定 `"1"`              |

**NodeType 演进**：

- 非破坏性变更（加可选 port、扩 ConfigSchema 可选字段）：保持 `Version` 不变，DSL 无感。
- 破坏性变更（删 port、改 port 类型、改 port ID）：发新 `Version`，旧 DSL 仍绑旧 Version。
- Registry 按 `(Key, Version)` 索引；v1 可以先单版本，等真正演进时再扩二级索引。

**DSL 演进**：

- Go 侧 `WorkflowDSL` 结构体变更走代码发布；老 `WorkflowVersion` 存的旧 JSON 通过 schema 迁移函数升级。
- `WorkflowDSL` JSON 根上加 `$schemaVersion` 字段（v1 固定 `"1"`），便于将来按版本分派解码。

## 16. 测试策略

本 spec 不引入实现代码，仅定义类型。实现阶段的测试粒度由 writing-plans 细化：

- **domain 层**：所有聚合根与值对象单元测试，重点覆盖不变式。
- **Registry**：合成正确性 + 缓存失效。
- **Executor**：每种内置节点集成测试；插件 Executor 用 mock HTTP / mock MCP server。
- **Run 编排**：端到端测试覆盖 If 分支、Loop、retry、fallback、多 End 命中等典型路径。

## 17. 验收标准

- [ ] `docs/superpowers/specs/` 下有本文件并已 commit
- [ ] 用户已复核本 spec
- [ ] writing-plans 基于本 spec 产出实现计划

## 18. 开放问题（实现阶段前需决断）

1. **NodeType.Version 字段是否 v1 就接入？** 倾向先全填 `"1"`，演进时再启用。
2. **`builtin.loop` 的循环语义**：仅 foreach 数组？要不要 while/until？v1 建议仅 foreach。
3. **`builtin.code` 的沙箱选型**：`goja`（JS）/ `yaegi`（Go 解释器）/ `wasmtime`？未决；不阻塞本 spec。
4. **模板引擎选型**：自研 `{{var}}` 解析 vs `text/template` vs 第三方如 `gonja`？需要在引擎章节明确。
5. **MCP 客户端库**：Go 侧成熟度；自实现 or 依赖开源？v1 可以先做 `http` transport 自实现一个最小 client。
6. **API Token 模型**：本 spec 假设存在，但没定义；独立 spec 处理。
7. **前端如何拿 NodeType 列表**：通过一个 `GET /api/v1/node-types` 接口；实现细节在 facade 层 spec。
8. **并行执行**：v1 支持 DAG 并行分支吗？若是，需要在"执行引擎 spec"里明确调度模型。

## 19. 修订历史

### 2026-04-27 工作流执行引擎 spec 覆盖

`2026-04-27-shineflow-workflow-engine-design.md` 对本 spec 的运行时相关条款作出以下覆盖：

- **多 End**：保留多 End 合法语义，引擎采用 first-end-wins，首个 End 成功后取消同次 Run 的其他在途分支。
- **可达性**：发布校验改为 DAG 可达性与端口有效性校验，移除单 End 强约束，允许显式 fire-and-forget 旁支。
- **loop**：`builtin.loop` 保留 key 常量，当前 builtin catalog 暂不注册，循环语义交给后续独立 spec。
- **merge → join**：汇合节点统一命名为 `builtin.join`，支持 `JoinModeAny` 与 `JoinModeAll`。
- **RefValue**：结构化引用删除 `PortID`，运行时统一通过 `nodes.<nodeID>.<path>` 读取最新可见输出。
- **FallbackOutput**：从 `map[string]any` 改为结构体 `{Port, Output}`，fallback 可声明要触发的输出端口。
