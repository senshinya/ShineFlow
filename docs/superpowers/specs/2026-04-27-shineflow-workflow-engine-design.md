# ShineFlow 工作流执行引擎设计

- 日期:2026-04-27(2026-04-28 修订)
- 状态:已定稿,待实现
- 依赖:`2026-04-22-shineflow-workflow-domain-design.md`(领域模型)、`2026-04-26-shineflow-workflow-infra-design.md`(持久化)
- 修订:本 spec 在 2026-04-28 内审中收到一轮反馈,已就 multi-End 语义、fire-and-forget 旁支、ref/template 寻址、fallback 端口、Symbols 不可变化、retry 计数、validator 规则、driver 异步持久化等做覆盖性修订。详见 §20。

## 1. 目标

落地 ShineFlow 工作流执行引擎的核心运行流程:给定一条已发布的 `WorkflowVersion` 和触发负载,引擎在进程内并发跑完 DAG,按 `ErrorPolicy` 处理重试 / 兜底 / 失败,把 `WorkflowRun` / `WorkflowNodeRun` 落库,返回 Run 终态。

本 spec 同时落地 7 个内置 executor + 1 个新增的 `builtin.join` 控制流节点,共 8 个 builtin executor;不含 `builtin.code` 和 `builtin.loop`。

## 2. 范围

### In Scope

- `domain/engine/` 引擎主体:driver 主循环、事件驱动调度、retry / fallback / fail_run 决策、ctx 传播、NodeRun 状态机持久化
- 静态 DAG 预计算(triggerTable / outAdj)
- `Symbols` 符号表(替代原 `BuildContext`,引擎运行期 + 审计回放共用)
- ValueSource 求值 + 模板引擎(`{{path}}`)
- 8 个 builtin executor:`start / end / llm / if / switch / join / set_variable / http_request`
- `domain/nodetype/catalog.go` builtin NodeType 静态目录 + `NewBuiltinRegistry()`
- domain validator 规则:DAG 无环、单 Start、至少一条 Start→End 路径、edge port 合法、孤儿节点禁止、多入度强制 join、switch case 名规范、fallback port 合法、FireErrorPort 必须有 error 端口
- 新增 port:`LLMClient`(仅接口,实际 adapter 后续 spec)
- `domain/engine/enginetest/` 测试 utility

### Out of Scope

- HTTP / Webhook / Cron 入口(facade 层,后续 spec)
- `RunService` / application 层用例编排
- 插件 NodeType(`plugin.http.*` / `plugin.mcp.*.*`)的投影、缓存、执行器
- port adapter 真实实现(`net/http` HTTPClient / OpenAI-compatible LLMClient / MCP Client / Sandbox)
- `builtin.code`(沙箱选型独立 spec)
- `builtin.loop`(子图 / fan-out / 环边任一种支持落地后再开)
- Draft 版本试跑(persistence spec §2 已划为后续 spec)
- Run 之间并发上限 / 全局 worker 池(单 Run 内已 goroutine 并发;Run 之间隔离由调用方控制)
- 子工作流 / `WorkflowCall` 节点

## 3. 关键决策总览

| #   | 决策                                                                                         | 理由摘要                                                  |
| --- | -------------------------------------------------------------------------------------------- | --------------------------------------------------------- |
| 1   | 引擎落 `domain/engine/`(domain service)                                                     | 编排是工作流业务规则,与 application 用例解耦              |
| 2   | 单 driver goroutine + worker 异步执行 + done/retry channel 事件驱动                          | 状态被 driver 独占,无锁;worker 无状态,纯执行            |
| 3   | DAG 预计算 `triggerTable` + `outAdj`,运行期不再遍历 edges                                   | 一次性投入换运行期 O(1) 查询                              |
| 4   | **允许多 End 节点**;约束改为"DSL 必须存在至少一条 Start→End 的路径";fire-and-forget 旁支合法 | 真实工作流常有多个终态出口(成功 / 拒绝 / 升级);旁支(日志、指标上报)是常见模式 |
| 5   | **First-end-wins**:任意 End 节点完成 → driver `cancel(runCtx)`,其他 inflight 走 cancelled,Run.Status = success | 多 End 语义清晰,与单 End 在工程上同构(都靠 cancel + drain 收尾) |
| 6   | `inflight == 0 && pendingRetries == 0` 是**唯一**的主循环终止条件                            | success / failed / cancelled 全走同一出口;两个计数器各自只在一处 ±,守恒易证 |
| 7   | NodeRun 落盘**只**由 driver 单点决策,但实际写入交给独立 persister goroutine,channel 异步 batch | driver 主循环不阻塞在 DB IO;持久化错误回推 driver,Run 失败 with `persistence_error` |
| 8   | retry 由 driver 决策,worker 无重试逻辑;每次 attempt 是独立 dispatch + 独立 NodeRun 行       | 对齐 domain spec §10.3                                    |
| 9   | retry timer 推 retryEvent 带 cancelled 标志;driver 用 `pendingRetries` 计数等待中的 retry,与 `inflight` 各管各 | 避免 inflight 计数自我修正,语义直观                      |
| 10  | `Symbols` 嵌套符号表(`trigger` / `vars` / `nodes` 三个根命名空间)替代原 `BuildContext`     | 对齐业界(n8n / Dify / GitHub Actions 风格);统一查路径   |
| 11  | `Symbols` 内部存 `json.RawMessage`,Lookup 时按需解析子树并返**新对象**                       | 天然 immutable,worker 无法 mutate 共享状态;无需深拷贝快照 |
| 12  | **节点输出地址不带 port**(对齐 n8n / Dify / GitHub Actions / Airflow):RefValue 只有 `nodeID + path`,模板 `nodes.<id>.<key>` 平铺 | port 是路由原语不是寻址原语;multi-port 节点须保证各 port output 形状兼容 |
| 13  | 模板语法 `{{path}}`(无 `text/template` 风格的点前缀;不支持条件 / 循环 / 函数)              | 用户输入语法直白;手写解析 < 100 行                         |
| 14  | `{{x}}` whole-string 时类型保留;数值比较(`if` / `switch`)统一 float64 coerce                | 对齐 n8n 单 Number 模型;绕开 Go json int/float 痛点      |
| 15  | 新增 `builtin.join` 节点,mode = `any` / `all`;多入度节点必须显式 `builtin.join`              | 区分 race(any)/ 严格 AND-Join(all)语义,DSL 意图清晰        |
| 16  | `ErrorPolicy.FallbackOutput` 改为 `{port, output}`;fallback 走用户指定端口                  | `if` / `switch` / 多端口节点 fallback 必须明确走哪条路径  |
| 17  | NodeRun fallback 落盘语义:`Status=failed + FallbackApplied=true + Output=fallback`;不引入新枚举 | attempt 状态保持失败真相;policy 兜底用独立 flag 表达      |
| 18  | `classifyErr` 严格用 `errors.Is(ctx.Canceled / DeadlineExceeded)`;executor 必须 `%w` wrap ctx 错误 | 避免业务错误被误判为 cancelled 而跳过 retry/fallback      |
| 19  | 引擎不实现 LLMClient / HTTPClient adapter;ExecServices 注入 nil 时 executor 返 `ErrPortNotConfigured` | 真实 adapter 走独立 spec,本期单测 mock                   |
| 20  | 测试用 in-memory `fakeRepo` 跑引擎单测;E2E 才用 testcontainers PG                            | 保单测速度;PG 起一次的成本只摊到几个 E2E 用例             |
| 21  | template 路径解析默认 strict(找不到报错),可选 lenient                                       | debug 时静默替换是噩梦;有意要默认值用 literal             |

## 4. 包布局

```
domain/
├── engine/                              ← 本 spec 主体
│   ├── doc.go                           包说明
│   ├── engine.go                        Engine 结构体 + Start 入口 + Config 选项
│   ├── scheduler.go                     主循环 / triggerTable / evaluate / handleResult / propagate / tryAdvance
│   ├── nodeexec.go                      worker(runNode):resolve → execute → push 事件
│   ├── retry.go                         scheduleRetry / retryEvent / computeBackoff(jitter+cap)
│   ├── resolve.go                       ValueSource 求值 / Config 模板递归展开 / coerceFloat64
│   ├── template.go                      ExpandTemplate / regex / wholeMatch / formatScalar
│   ├── result.go                        nodeResult / runState 类型
│   ├── persist.go                       persistOp 类型 + persistKind 枚举 + runPersister + applyPersistOp
│   └── enginetest/
│       ├── mock_executor.go             可配置 NodeExecutor mock
│       ├── mock_services.go             mock LLMClient / HTTPClient / Logger
│       ├── builder.go                   DSL 快速构造器
│       └── harness.go                   EngineHarness:fake repos + Engine 一键装配
│
├── executor/
│   ├── exec_input.go                    增补 LLMClient port、ExecServices.LLMClient、RunInfo.TriggerPayload
│   └── builtin/                         8 个 executor 落点
│       ├── doc.go
│       ├── wire.go                      Register(r) 把 8 个 factory 注册进 ExecutorRegistry
│       ├── start.go                     + start_test.go
│       ├── end.go                       + end_test.go
│       ├── llm.go                       + llm_test.go
│       ├── if.go                        + if_test.go
│       ├── switch.go                    + switch_test.go
│       ├── join.go                      + join_test.go      ← 新
│       ├── set_variable.go              + set_variable_test.go
│       └── http_request.go              + http_request_test.go
│
├── nodetype/
│   ├── builtin.go                       增补 BuiltinJoin / JoinModeAny / JoinModeAll(BuiltinLoop 保留但 catalog 不放)
│   └── catalog.go                       新增 NewBuiltinRegistry() + 8 个 NodeType 元数据(start/end/llm/if/switch/join/set_variable/http_request)
│
├── run/
│   ├── symbols.go                       新增 Symbols + Set* + Lookup* + FromPersistedState + Snapshot
│   ├── symbols_test.go                  原 context_test.go 迁过来 + 新用例
│   └── (删除 context.go)
│
└── validator/
    └── validator.go                     新增/调整 §14 全部规则:checkSingleStart / checkAtLeastOneEnd / checkNoPathToEnd / checkCycleDetected / checkEdgePortValid / checkIsolatedNode / checkMultiInputRequiresJoin / checkJoinMinInputs / checkJoinMode / checkJoinConfig / checkSwitchCaseNames / checkFallbackPort / checkFireErrorPortRequiresErrorPort
```

依赖方向:`domain/engine` → `domain/{run, workflow, executor, nodetype, validator, credential}`,引擎包不向 domain 层之外依赖。

## 5. DAG 预计算

进入主循环前一次性构造静态数据,之后运行期间只读不变。

```go
type triggerSpec struct {
    nodeID  string
    inEdges []inEdgeRef
    mode    joinMode    // 仅 len(inEdges) > 1 时有意义,即 builtin.join 节点
}

type inEdgeRef struct {
    EdgeID     string
    SourceNode string
    SourcePort string
}

type triggerTable map[string]*triggerSpec  // NodeID → triggerSpec
type outAdj      map[string][]workflow.Edge // NodeID → 出边列表

type joinMode int
const (
    joinAny joinMode = iota
    joinAll
)

func buildTriggerTable(dsl workflow.WorkflowDSL) (triggerTable, outAdj) {
    tt := triggerTable{}
    oa := outAdj{}
    for _, n := range dsl.Nodes {
        spec := &triggerSpec{nodeID: n.ID}
        if n.TypeKey == nodetype.BuiltinJoin {
            spec.mode = parseJoinMode(n.Config)
        }
        tt[n.ID] = spec
    }
    for _, e := range dsl.Edges {
        tt[e.To].inEdges = append(tt[e.To].inEdges, inEdgeRef{
            EdgeID: e.ID, SourceNode: e.From, SourcePort: e.FromPort,
        })
        oa[e.From] = append(oa[e.From], e)
    }
    return tt, oa
}
```

## 6. 主循环

### 6.1 Engine 接口

```go
type Engine struct {
    workflowRepo workflow.WorkflowRepository
    runRepo      run.WorkflowRunRepository
    ntReg        nodetype.NodeTypeRegistry
    exReg        executor.ExecutorRegistry
    services     executor.ExecServices

    clock        func() time.Time
    newID        func() string
    afterFunc    func(time.Duration, func()) (stop func())   // 测试可注入即时触发
    templateMode TemplateMode
    runTimeout   time.Duration
}

type Config struct {
    Clock        func() time.Time
    NewID        func() string
    AfterFunc    func(time.Duration, func()) (stop func())
    TemplateMode TemplateMode    // strict(default)/ lenient
    RunTimeout   time.Duration   // 0 = 无 Run 级 timeout(仍受 ErrorPolicy.Timeout 节点级控制)
}

func New(...) *Engine

type StartInput struct {
    VersionID      string
    TriggerKind    run.TriggerKind
    TriggerRef     string
    TriggerPayload json.RawMessage
    CreatedBy      string
}

// Start 同步驱动一条 Run 跑到终态。
//
//   返回 *WorkflowRun.Status 必为终态之一:
//     - success:   首个 End 节点完成(first-end-wins);其余 inflight 节点 NodeRun.Status 可能是 cancelled,
//                  但不影响 Run 终态。
//     - failed:    runFail 被设置(节点 fail_run / persistence 错误 / no_end_reached / output_not_serializable / trigger_invalid)。
//     - cancelled: 调用方传入的 ctx 被取消,且既无 endHit 也无 runFail。
//
//   driver 内部主动 cancel(runCtx) 用于"first-end-wins"和"runFail 后 drain";不会让 Run 终态变 cancelled。
func (e *Engine) Start(ctx context.Context, in StartInput) (*run.WorkflowRun, error)
```

### 6.2 终止条件:`inflight == 0 && pendingRetries == 0`

driver 主循环维护两个计数器,各自只在一处 ±:

| 计数器 | 含义 | ++ 时机 | -- 时机 |
|---|---|---|---|
| `inflight` | 有 worker goroutine 在跑 | `dispatch` 入口 | `case <-done` 头部 |
| `pendingRetries` | retry timer 等待中(尚未触发) | `scheduleRetry` 入口 | `case <-retryCh` 头部 |

任一计数器非零都意味着"流程未完结"。所有"为什么终结"的判断收敛到 `finalize` 一处分发。

driver 不直接调 `runRepo`;所有持久化通过 `persistCh` 推给独立 persister goroutine,driver 主循环不阻塞在 DB IO。persister 出错回推 `persistErrCh`,driver 标 runFail 并 cancel。

```go
func (e *Engine) Start(ctx context.Context, in StartInput) (*run.WorkflowRun, error) {
    // 阶段 1:加载 + 校验 + 创建 Run row(同步,失败直接返错)
    v, err := e.workflowRepo.GetVersion(ctx, in.VersionID)
    if err != nil { return nil, err }
    if v.State != workflow.VersionStateRelease {
        return nil, ErrVersionNotPublished
    }

    rn := buildRun(in, v, e.newID, e.clock)
    if err := e.runRepo.Create(ctx, rn); err != nil { return nil, err }
    if err := e.runRepo.UpdateStatus(ctx, rn.ID, run.RunStatusRunning,
        run.WithRunStartedAt(e.clock())); err != nil { return nil, err }

    // 阶段 2:DAG 预计算 + Symbols 初始化
    triggers, oa := buildTriggerTable(v.DSL)
    sym, err := run.NewSymbols(in.TriggerPayload)   // SetTrigger 内联,trigger 必须是 JSON object
    if err != nil {
        return e.finalizeFailed(ctx, rn, run.RunError{
            Code: run.RunErrCodeTriggerInvalid, Message: err.Error(),
        })
    }
    state := newRunState(v.DSL, triggers, oa, sym)

    // 阶段 3:Run 级 ctx + persister 启动
    parentCtx := ctx
    if e.runTimeout > 0 {
        var cancelTO context.CancelFunc
        parentCtx, cancelTO = context.WithTimeout(ctx, e.runTimeout)
        defer cancelTO()
    }
    runCtx, cancel := context.WithCancel(parentCtx)
    defer cancel()

    persistCh    := make(chan persistOp, 64)
    persistErrCh := make(chan error, 1)
    persistDone  := make(chan struct{})
    go e.runPersister(persistCh, persistErrCh, persistDone)
    defer func() {
        close(persistCh)
        <-persistDone   // 等 persister 排空,保证 finalize 之前所有 NodeRun 都落了盘
    }()

    done    := make(chan nodeResult, 32)
    retryCh := make(chan retryEvent, len(v.DSL.Nodes))

    // 阶段 4:bootstrap — 派发入度=0 的节点(必为 builtin.start)
    for nid, spec := range triggers {
        if len(spec.inEdges) == 0 {
            e.dispatch(runCtx, rn, state, nid, done, retryCh, persistCh)
        }
    }

    // 阶段 5:事件循环 — 退出条件 inflight==0 且 pendingRetries==0
    //
    // ctxDone 一旦 fire 过(无论是父 ctx 取消还是 driver 自己 cancel)就置 nil,
    // 后续 select 不再选中此 case;driver 任何主动 cancel 路径(runFail / endHit / persistErr)
    // 都必须同时 nil-out ctxDone,保持语义对称。
    ctxDone := runCtx.Done()
    cancelOnce := func() {
        cancel()
        ctxDone = nil
    }
    for state.inflight > 0 || state.pendingRetries > 0 {
        select {
        case res := <-done:
            state.inflight--
            e.handleResult(runCtx, rn, state, res, done, retryCh, persistCh)
            if state.runFail != nil {
                cancelOnce()             // 通知 inflight worker 退场,不立即 return,等 drain
            } else if state.endHit != nil {
                cancelOnce()             // first-end-wins:cancel 其余分支,自然 drain 到 success
            }

        case rt := <-retryCh:
            state.pendingRetries--
            // 防御:run 已被判定为失败(runFail set / ctx 已 cancel),不再 dispatch
            // 新 attempt;注意 timer afterFunc 内的 ctx.Done() 检查与 driver 设置 runFail 之间
            // 存在极小竞速窗口,这里再做一道兜底。
            if rt.cancelled || state.runFail != nil || ctxDone == nil {
                e.persistRetryAborted(rn, state, rt, persistCh)
            } else {
                e.dispatch(runCtx, rn, state, rt.nodeID, done, retryCh, persistCh)
            }

        case perr := <-persistErrCh:
            if state.runFail == nil {
                state.runFail = &run.RunError{
                    Code: run.RunErrCodePersistence, Message: perr.Error(),
                }
            }
            cancelOnce()

        case <-ctxDone:
            cancelOnce()
        }
    }

    return e.finalize(ctx, rn, state)
}
```

### 6.2.1 persister goroutine

```go
type persistOp struct {
    kind persistKind            // appendNodeRun / saveNodeRunOutput / saveVars / saveEndResult / updateRunStatus / saveError ...
    payload any
}

func (e *Engine) runPersister(in <-chan persistOp, errOut chan<- error, doneOut chan<- struct{}) {
    defer close(doneOut)
    for op := range in {
        if err := e.applyPersistOp(op); err != nil {
            select {
            case errOut <- err:    // 第一次出错才回推;后续 op 继续 best-effort 落盘
            default:
            }
        }
    }
}
```

设计要点:
- `persistOp` 是值类型,driver 推完即释放引用
- `persistCh` 的 buffer(64)够吸收常规 burst;持续打满意味着 DB 慢,driver 自然 backpressure
- persister 出错后**继续消费**剩余 op,不丢请求;driver 收到首个错误就 cancel + 走 finalize failed
- `defer` 里 `close(persistCh)` + `<-persistDone` 保证 finalize 之前所有写都落了盘

### 6.2.2 多 End 与 first-end-wins 语义

- DSL 允许 0..N 个 End 节点(validator 单独保证"至少一条 Start→End 路径存在")
- 任意 End 节点 propagate 完成时 driver 标 `state.endHit = &nodeID` 并立即 `cancel(runCtx)`
- 其他 inflight worker 走 ctx 取消路径,各自 NodeRun 落 `cancelled`,Run 终态 `success`
- 若 cancel 触发后又有别的 End 节点跑完(竞速窗口),仅记录第一个为 `Run.EndNodeID`,后续 NodeRun 仍按其真实结束状态落(success / cancelled)

### 6.2.3 fire-and-forget 旁支

- 旁支节点(无下游、不连 End)合法
- 主循环靠 `inflight + pendingRetries == 0` 自然等待旁支完成或被 cancel

### 6.3 终态分发

`finalize` 在 driver 退出主循环、persister 排空后调用:

```go
func (e *Engine) finalize(ctx context.Context, rn *run.WorkflowRun, st *runState) (*run.WorkflowRun, error) {
    switch {
    case st.runFail != nil:
        return e.finalizeFailed(ctx, rn, *st.runFail)
    case st.endHit != nil:
        return e.finalizeSuccess(ctx, rn, st)
    case ctx.Err() != nil:
        return e.finalizeCancelled(ctx, rn)
    default:
        // 所有分支 drain 完但没碰到 End:可能是 fire-and-forget 旁支跑完,
        // 主链上某个分支被 skip 链断头。Run 视为 failed with no_end_reached。
        return e.finalizeFailed(ctx, rn, run.RunError{
            Code:    run.RunErrCodeNoEndReached,
            Message: "all branches exhausted but no End was reached",
        })
    }
}
```

判定优先级(`runFail > endHit > ctx.Err > no_end_reached`):
- `runFail` 非空 → 失败,即使期间也有 End 完成,以失败为准(persistence error / fail_run policy 都算)
- `endHit` 非空 → 成功(first-end-wins),即使有其他分支 cancelled 不影响 Run 终态
- ctx 取消但没 endHit / runFail → 父 ctx 取消导致,Run = cancelled

`finalizeSuccess` 内部:
- `Run.Output = nodeRuns[*st.endHit].ResolvedInputs`(domain spec §8.1)
- `Run.EndNodeID = st.endHit`
- 通过 persister 写 `SaveEndResult` + `SaveVars` + `UpdateStatus(success)` —— 注意此时 persister 已 close,这三条改为同步直写 `runRepo`(finalize 阶段不再需要异步)

### 6.4 持久化时序

driver 单点决策,实际写入由 persister goroutine 异步完成(finalize 阶段同步直写)。

| 时机 | 调用 | 路径 |
|---|---|---|
| `Start` 入口 | `Create` + `UpdateStatus(running, started_at)` | 同步直写 |
| dispatch 派发节点 | `AppendNodeRun(status=running, attempt=N)` | persister |
| worker 推回 done | `SaveNodeRunOutput` + `UpdateNodeRunStatus(success/failed/cancelled)` | persister |
| fallback 生效 | `UpdateNodeRun(fallback_applied=true, output=fallback_output)` | persister |
| retry timer cancelled | `AppendNodeRun(status=cancelled, error=retry_aborted)` | persister |
| set_variable 节点 success | 额外 `SaveVars(allVars)` | persister |
| End 节点 success | (driver 仅记 endHit;`SaveEndResult` 留到 finalize) | — |
| finalize success | `SaveEndResult(endNodeID, output)` + `UpdateStatus(success, ended_at)` | 同步直写 |
| finalize failed | `SaveError(runError)` + `UpdateStatus(failed, ended_at)` | 同步直写 |
| finalize cancelled | `UpdateStatus(cancelled, ended_at)` | 同步直写 |

约定:
- `SaveNodeRunOutput` 与 `UpdateNodeRunStatus` 在 persister 内合并为一次 SQL(一次 row update)
- `SaveVars(allVars)` 全量写,本期接受写放大;后续做差量补丁是独立优化
- 在 persister close 后调用 finalize 写,意味着所有 NodeRun row 已落盘,不会出现"Run.success 但某个 NodeRun 还没 commit"的不一致

## 7. 节点 ready 判定 — `evaluate` 三态

```go
type readiness int
const (
    notReady readiness = iota
    readyToRun
    readyToSkip
)

type edgeState int
const (
    edgePending edgeState = iota
    edgeLive
    edgeDead
)

func evaluate(spec *triggerSpec, edgeState map[string]edgeState) readiness {
    n := len(spec.inEdges)
    if n == 0 {
        return readyToRun
    }

    hasLive, hasPending, hasDead := false, false, false
    for _, e := range spec.inEdges {
        switch edgeState[e.EdgeID] {
        case edgePending: hasPending = true
        case edgeLive:    hasLive = true
        case edgeDead:    hasDead = true
        }
    }

    // 单入度:常规节点
    if n == 1 {
        if hasPending { return notReady }
        if hasLive    { return readyToRun }
        return readyToSkip
    }

    // 多入度:validator 已保证只有 builtin.join 才能到这里
    switch spec.mode {
    case joinAny:
        if hasLive    { return readyToRun }     // race:第一个 live 立即 fire
        if hasPending { return notReady }
        return readyToSkip                       // 全 dead

    case joinAll:
        if hasPending { return notReady }        // 等齐
        if hasDead    { return readyToSkip }     // 任一 dead → skip
        return readyToRun                         // 全 live → fire

    default:
        return notReady
    }
}
```

## 8. 事件驱动核心 — `handleResult`

```go
func (e *Engine) handleResult(
    ctx context.Context, rn *run.WorkflowRun, st *runState,
    res nodeResult, done chan<- nodeResult, retryCh chan<- retryEvent,
    persistCh chan<- persistOp,
) {
    node := st.byID[res.nodeID]
    ep   := effectivePolicy(node.ErrorPolicy)

    // a) 错误路径:cancelled / retry / fail_run / fallback / fire_error_port
    if res.err != nil {
        // a.1 严格判定 cancelled:仅 ctx.Canceled / ctx.DeadlineExceeded
        //     executor 必须 errors.Wrap (`%w`) 透传 ctx 错误,否则按业务错处理
        if errors.Is(res.err, context.Canceled) || errors.Is(res.err, context.DeadlineExceeded) {
            persistCh <- persistOp{kind: persistNodeRunCancelled, payload: res}
            st.nodeStat[res.nodeID] = nodeDone
            return
        }

        // a.2 attempt 级失败先落盘(必落,后续 fallback 也是在这之上"补丁"一行 update)
        persistCh <- persistOp{kind: persistNodeRunFailed, payload: res}

        // a.3 还有重试预算 → 安排 retry
        if res.attempt < ep.MaxRetries+1 {
            delay := computeBackoff(ep, res.attempt)
            st.pendingRetries++
            e.scheduleRetry(ctx, st, res.nodeID, res.attempt+1, delay, retryCh)
            return
        }

        // a.4 重试用尽 → 应用 OnFinalFail
        switch ep.OnFinalFail {
        case workflow.FailStrategyFailRun:
            st.runFail = &run.RunError{
                NodeID: res.nodeID, NodeRunID: res.nodeRunID,
                Code: run.RunErrCodeNodeExecFailed, Message: res.err.Error(),
            }
            st.nodeStat[res.nodeID] = nodeDone
            return

        case workflow.FailStrategyFallback:
            // ErrorPolicy.FallbackOutput = { Port string; Output map[string]any }
            // fallback 的 firedPort 由用户显式声明,绕过"if/switch 默认 default 不合理"的问题
            res.output           = ep.FallbackOutput.Output
            res.firedPort        = ep.FallbackOutput.Port
            res.fallbackApplied  = true
            // 不再写 failed 行,改为"补丁"上一步落的 failed 行
            persistCh <- persistOp{kind: persistNodeRunFallbackPatch, payload: res}
            // fall through 到下面的 propagate

        case workflow.FailStrategyFireErrorPort:
            res.output    = nil   // error 端口默认无 output;executor 可在错误时返带数据
            res.firedPort = workflow.PortError
            // fall through 到下面的 propagate
        }
    } else {
        // 成功路径:落 success
        persistCh <- persistOp{kind: persistNodeRunSuccess, payload: res}
    }

    // b) 推进:更新 Symbols + edgeState + 评估下游
    e.propagate(ctx, rn, st, node, res, done, retryCh, persistCh)
}

func (e *Engine) propagate(
    ctx context.Context,
    rn *run.WorkflowRun, st *runState,
    node *workflow.Node, res nodeResult,
    done chan<- nodeResult, retryCh chan<- retryEvent,
    persistCh chan<- persistOp,
) {
    // 写 Symbols:executor 直接返 map[string]any,无 JSON round-trip
    // res.output 是 nil 时(error port 无数据 / End 节点),SetNodeOutput 存空 map
    if res.output == nil {
        res.output = map[string]any{}
    }
    if err := st.sym.SetNodeOutput(node.ID, res.output); err != nil {
        // SetNodeOutput 内部 marshal 失败 → 节点 output 包含不可序列化的值(chan、func 等)
        st.runFail = &run.RunError{
            NodeID: res.nodeID, NodeRunID: res.nodeRunID,
            Code: run.RunErrCodeOutputNotSerializable, Message: err.Error(),
        }
        st.nodeStat[res.nodeID] = nodeDone
        return
    }

    if node.TypeKey == nodetype.BuiltinSetVariable {
        for k, v := range res.output {
            _ = st.sym.SetVar(k, v)   // Symbols 内部 marshal 失败已在 SetNodeOutput 拦截
        }
        persistCh <- persistOp{kind: persistSaveVars, payload: st.sym.SnapshotVars()}
    }

    st.nodeStat[node.ID] = nodeDone

    // End 节点:标 endHit,主循环看到 endHit 后 cancel 其余分支(first-end-wins)
    if node.TypeKey == nodetype.BuiltinEnd {
        if st.endHit == nil {
            id := node.ID
            st.endHit = &id
        }
        return
    }

    // 更新出边状态 + 推进下游
    for _, edge := range st.outAdj[node.ID] {
        if edge.FromPort == res.firedPort {
            st.edgeState[edge.ID] = edgeLive
        } else {
            st.edgeState[edge.ID] = edgeDead
        }
    }
    for _, edge := range st.outAdj[node.ID] {
        e.tryAdvance(ctx, rn, st, edge.To, done, retryCh, persistCh)
    }
}

func (e *Engine) tryAdvance(
    ctx context.Context,
    rn *run.WorkflowRun, st *runState, target string,
    done chan<- nodeResult, retryCh chan<- retryEvent,
    persistCh chan<- persistOp,
) {
    if st.nodeStat[target] != nodeUnready { return }

    switch evaluate(st.triggers[target], st.edgeState) {
    case readyToRun:
        e.dispatch(ctx, rn, st, target, done, retryCh, persistCh)
    case readyToSkip:
        e.markSkipped(rn, st, target, persistCh)
        for _, oe := range st.outAdj[target] {
            st.edgeState[oe.ID] = edgeDead
        }
        for _, oe := range st.outAdj[target] {
            e.tryAdvance(ctx, rn, st, oe.To, done, retryCh, persistCh)
        }
    case notReady:
        // 等其他入边
    }
}
```

### 8.1 fallback 落库语义

- attempt 失败行先落 `Status=failed`(由 `persistNodeRunFailed` 写入)
- fallback 应用后,`persistNodeRunFallbackPatch` **更新同一行**:`FallbackApplied=true`、`Output=fallback_output`,`Status` 保持 `failed`
- 查询约定:**"节点是否对下游产生了 output"** = `status='success' OR fallback_applied=true`,所有依赖此语义的下游 SQL / Repository 方法在 spec §15 中列明

### 8.2 fire_error_port 的 output 语义

- 默认情况下 fire_error_port 后 `res.output = nil` → Symbols 存空 map
- executor 若想让 error 路径携带数据,需在 `Execute` 错误返回前先写 `ExecOutput.Outputs`(类似 `http_request` 4xx/5xx 仍带 body)。这个分支由 executor 自身决定,不在 ErrorPolicy 层
- 多 port 节点(`if`/`switch`)的 error 输出形状必须与 default 输出形状**字段不冲突**,见 §13

## 9. retry 机制

### 9.1 driver-driven retry

worker 不重试,失败后 driver 决策。每次 attempt 是独立 dispatch + 独立 NodeRun 行(domain spec §10.3)。

### 9.2 retryEvent 与两计数器守恒

driver 维护两个计数器:`inflight`(worker 在跑) + `pendingRetries`(retry timer 等待中)。各自只在一处 ±,不需要相互"抵消":

```go
type retryEvent struct {
    nodeID    string
    attempt   int
    cancelled bool   // ctx 在 timer 触发前已取消 → 不 dispatch,只走持久化
}

func (e *Engine) scheduleRetry(
    ctx context.Context, st *runState,
    nodeID string, nextAttempt int, delay time.Duration, retryCh chan<- retryEvent,
) {
    // 调用方已 st.pendingRetries++,此处只负责后续推事件
    e.afterFunc(delay, func() {
        ev := retryEvent{nodeID: nodeID, attempt: nextAttempt}
        select {
        case <-ctx.Done():
            ev.cancelled = true
        default:
        }
        retryCh <- ev    // 无条件推回(retryCh 容量 = len(dsl.Nodes))
    })
}
```

driver 在 `case <-retryCh` 分支:

```go
case rt := <-retryCh:
    state.pendingRetries--
    if rt.cancelled {
        e.persistRetryAborted(rn, state, rt, persistCh)     // NodeRun(status=cancelled, error=retry_aborted)
    } else {
        e.dispatch(runCtx, rn, state, rt.nodeID, done, retryCh, persistCh)   // 内部 inflight++
    }
```

时序示意:

```
attempt N done(失败)        inflight: 1 → 0           ← case <-done 头部 --
handleResult 决定 retry      pendingRetries: 0 → 1     ← scheduleRetry 调用前 ++
[delay 后 timer 触发,推 retryEvent]
case <-retryCh 收到 cancelled=false:
                            pendingRetries: 1 → 0      ← case 头部 --
  dispatch attempt N+1       inflight: 0 → 1            ← dispatch 内部 ++
```

终止条件 `inflight == 0 && pendingRetries == 0` 在以上每个时刻都准确反映"流程未完结"。

### 9.3 Backoff 计算 — 加 jitter + cap + attempt 上限

```go
const (
    maxBackoffDelay  = 30 * time.Second
    maxBackoffShift  = 8                       // 防 << 溢出;指数最大放大 256 倍
    jitterFraction   = 0.2                     // ±20%
)

func computeBackoff(ep workflow.ErrorPolicy, attempt int, rng *rand.Rand) time.Duration {
    if ep.RetryDelay <= 0 {
        return 0
    }
    base := ep.RetryDelay
    if ep.RetryBackoff == workflow.BackoffExponential {
        shift := attempt - 1
        if shift > maxBackoffShift {
            shift = maxBackoffShift
        }
        base = ep.RetryDelay << uint(shift)
    }
    if base > maxBackoffDelay {
        base = maxBackoffDelay
    }
    // ±20% jitter
    delta := time.Duration(float64(base) * jitterFraction * (rng.Float64()*2 - 1))
    return base + delta
}
```

测试侧通过 `Engine.afterFunc` 注入即时触发的假实现,跳过真实等待。jitter 用 `Engine` 持有的 `*rand.Rand`(测试可注入种子复现)。

### 9.4 默认 ErrorPolicy

`Node.ErrorPolicy == nil` 时引擎使用包级默认值:

```go
var defaultErrorPolicy = workflow.ErrorPolicy{
    Timeout:        0,                                      // 0 = 不设节点级 timeout,继承父 ctx
    MaxRetries:     0,                                      // 不重试
    RetryBackoff:   workflow.BackoffFixed,
    RetryDelay:     0,
    OnFinalFail:    workflow.FailStrategyFailRun,           // 安全默认:无 error 端口的节点不会被静默吞错
    FallbackOutput: workflow.FallbackOutput{},               // Port="", Output=nil
}

// effectivePolicy 把 nil 解为默认副本;非 nil 直接 deref。
func effectivePolicy(ep *workflow.ErrorPolicy) workflow.ErrorPolicy {
    if ep == nil { return defaultErrorPolicy }
    return *ep
}
```

attempt 计数从 1 开始。给定 `MaxRetries=N`,最大 attempt 数 = `N+1`(初次 + N 次重试)。`res.attempt < ep.MaxRetries+1` 是 retry 判定的不变式。

### 9.5 ErrorPolicy.FallbackOutput 类型

```go
// domain/workflow
type FallbackOutput struct {
    Port   string         // 必填(策略=Fallback 时);validator 强制 ∈ source NodeType.OutputPorts
    Output map[string]any // 可空(空 → Symbols.nodes.<id> = {})
}
```

validator 新规则 `CodeFallbackPortInvalid`:`OnFinalFail=Fallback` 时 `FallbackOutput.Port` 必须填,且必须在节点 NodeType 声明的 OutputPorts 中。

## 10. worker(`runNode`)

无状态,一次 attempt 一个 goroutine,push 结果即退出:

```go
func (e *Engine) runNode(
    ctx context.Context, rn *run.WorkflowRun, node *workflow.Node,
    nr *run.NodeRun, snap *run.Symbols, done chan<- nodeResult,
) {
    res := nodeResult{nodeID: node.ID, nodeRunID: nr.ID, attempt: nr.Attempt}

    defer func() {
        if r := recover(); r != nil {
            res.err = fmt.Errorf("executor panic: %v", r)
        }
        done <- res
    }()

    // a) Resolve inputs / config
    inputs, err := e.resolver.ResolveInputs(node, snap)
    if err != nil { res.err = fmt.Errorf("resolve inputs: %w", err); return }
    cfg, err := e.resolver.ResolveConfig(node.Config, snap)
    if err != nil { res.err = fmt.Errorf("resolve config: %w", err); return }
    res.resolvedInputs = mustMarshal(inputs)
    res.resolvedConfig = cfg

    // b) Lookup NodeType + Executor
    nt, ok := e.ntReg.Get(node.TypeKey)
    if !ok { res.err = fmt.Errorf("node type not registered: %s", node.TypeKey); return }
    exec, err := e.exReg.Build(nt)
    if err != nil { res.err = fmt.Errorf("executor build: %w", err); return }

    // c) Per-attempt timeout
    nodeCtx := ctx
    if t := effectivePolicy(node.ErrorPolicy).Timeout; t > 0 {
        var cancel context.CancelFunc
        nodeCtx, cancel = context.WithTimeout(ctx, t)
        defer cancel()
    }

    // d) Execute
    out, err := exec.Execute(nodeCtx, executor.ExecInput{
        NodeType: nt, Config: cfg, Inputs: inputs,
        Run: buildRunInfo(rn, nr),
        Services: e.services,
    })
    if err != nil { res.err = err; return }

    res.output       = out.Outputs                  // map[string]any 直传,driver propagate 不再 JSON round-trip
    res.firedPort    = orDefault(out.FiredPort, workflow.PortDefault)
    res.externalRefs = out.ExternalRefs
}
```

`nodeResult.output` 字段类型 = `map[string]any`(非 RawMessage)。Symbols 在 SetNodeOutput 内部一次 marshal 落到 RawMessage。NodeRun 持久化时,persister 也对 `out.Outputs` 做 marshal 落 `Output json` 字段——同一份 map 在 Symbols / 持久化两条路径上分别 marshal,允许的轻微冗余,换取"运行期类型保留"。

`res.resolvedInputs` / `res.resolvedConfig` 仍是 RawMessage(直接落 NodeRun.ResolvedInputs / ResolvedConfig)。

## 11. `Symbols` 符号表

### 11.1 设计原则:序列化即不可变

`Symbols` 内部对**节点 output 与 vars 子树**统一存 `json.RawMessage`。这带来两个性质:

1. **天然不可变**:RawMessage 是 `[]byte` 的别名,Lookup 时 `json.Unmarshal` 解出新对象返回给调用方,executor 怎么 mutate 都不会影响其他 worker / 后续 Lookup
2. **快照零拷贝**:`Snapshot()` 仅复制 map 头部和 RawMessage 切片头(共 ~2KB 量级),不需要深拷贝节点 output 内容

trigger 子树同样按 `json.RawMessage` 存储(SetTrigger 时一次性校验 payload 是 JSON object)。

### 11.2 类型

`domain/run/symbols.go`:

```go
// Symbols 是 Run 在某个时间点的变量命名空间。
//
// 三大根命名空间:
//
//   trigger.<key>         ← TriggerPayload(必须是 JSON object;非 object 在 NewSymbols 报错)
//   vars.<key>            ← set_variable 节点累计写入
//   nodes.<nodeID>.<key>  ← 节点 NodeRun.Output 顶层字段
//                          (可用 = Status==Success 或 FallbackApplied=true)
//
// 内部存储 json.RawMessage,Lookup 按需解析子树,返回新对象 → 调用方拿到的值随便改不影响 Symbols。
//
// 引擎运行期增量 Set*;审计 / 回放从持久化状态一次性 FromPersistedState。
type Symbols struct {
    trigger json.RawMessage              // 必为 JSON object,空对象 = `{}`
    vars    map[string]json.RawMessage   // varName → 已序列化的值(任意 JSON 类型)
    nodes   map[string]json.RawMessage   // nodeID  → 已序列化的 output object
}

// NewSymbols 校验并初始化 trigger 子树。payload 必须是 JSON object。
//   payload == nil 或 len == 0 → trigger = {}
func NewSymbols(payload json.RawMessage) (*Symbols, error)

// SetNodeOutput 将 output 序列化后写入 nodes[nodeID]。
//   output = nil 视为空 map。
//   marshal 失败(含不可序列化的值)→ 返错,driver 标 runFail。
func (s *Symbols) SetNodeOutput(nodeID string, output map[string]any) error

// SetVar 将单个 var 序列化后写入。marshal 失败返错。
func (s *Symbols) SetVar(key string, value any) error

// SnapshotVars 返回当前 vars 全量映射(已序列化的 RawMessage)。供 SaveVars 落盘。
func (s *Symbols) SnapshotVars() map[string]json.RawMessage

// Snapshot 返回一个新的 *Symbols,共享底层 RawMessage 切片(免拷贝,因为 RawMessage 不会被 mutate)。
//   - trigger:同一个 RawMessage 引用
//   - vars / nodes:新 map 但 value 仍是原 RawMessage 引用
//   后续 driver 对原 Symbols 的 Set 不影响 snapshot(map 已分叉)。
func (s *Symbols) Snapshot() *Symbols

// Lookup 按 dotted path 解析。每次调用都 unmarshal 出新对象。
func (s *Symbols) Lookup(path string) (any, error)

// FromPersistedState 从落盘的 Run + NodeRun 一次性构造(审计 / 回放)。
func FromPersistedState(rn *WorkflowRun, nodeRuns []*NodeRun) (*Symbols, error)
```

### 11.3 Trigger 校验

```go
func NewSymbols(payload json.RawMessage) (*Symbols, error) {
    if len(payload) == 0 {
        payload = json.RawMessage(`{}`)
    }
    // 严格校验:必须解析为 JSON object
    var probe map[string]json.RawMessage
    if err := json.Unmarshal(payload, &probe); err != nil {
        return nil, fmt.Errorf("trigger payload must be a JSON object: %w", err)
    }
    return &Symbols{
        trigger: payload,
        vars:    map[string]json.RawMessage{},
        nodes:   map[string]json.RawMessage{},
    }, nil
}
```

driver 在 §6.2 阶段 2 收到 `NewSymbols` 错误 → 直接 finalize Run 为 `failed` with `RunErrCodeTriggerInvalid`,不进入主循环。

### 11.4 Lookup 算法

```go
func (s *Symbols) Lookup(path string) (any, error) {
    parts := strings.Split(path, ".")
    if len(parts) == 0 || parts[0] == "" {
        return nil, fmt.Errorf("empty path")
    }
    var raw json.RawMessage
    var rest []string
    switch parts[0] {
    case "trigger":
        raw, rest = s.trigger, parts[1:]
    case "vars":
        if len(parts) < 2 { return nil, fmt.Errorf("vars.<key> required") }
        v, ok := s.vars[parts[1]]
        if !ok { return nil, fmt.Errorf("var not set: %s", parts[1]) }
        raw, rest = v, parts[2:]
    case "nodes":
        if len(parts) < 2 { return nil, fmt.Errorf("nodes.<id> required") }
        n, ok := s.nodes[parts[1]]
        if !ok { return nil, fmt.Errorf("node not yet produced output: %s", parts[1]) }
        raw, rest = n, parts[2:]
    default:
        return nil, fmt.Errorf("unknown root: %q", parts[0])
    }

    // 一次 unmarshal 出本次 lookup 需要的子树根
    var cur any
    if err := json.Unmarshal(raw, &cur); err != nil {
        return nil, fmt.Errorf("symbols decode at %q: %w", parts[0], err)
    }
    return walkPath(cur, rest)
}

func walkPath(cur any, parts []string) (any, error) {
    for _, p := range parts {
        switch x := cur.(type) {
        case map[string]any:
            v, ok := x[p]
            if !ok { return nil, fmt.Errorf("key not found: %s", p) }
            cur = v
        case []any:
            idx, err := strconv.Atoi(p)
            if err != nil || idx < 0 || idx >= len(x) {
                return nil, fmt.Errorf("invalid array index: %s", p)
            }
            cur = x[idx]
        default:
            return nil, fmt.Errorf("cannot navigate %T at %q", cur, p)
        }
    }
    return cur, nil
}
```

### 11.5 Snapshot 语义

driver 在 dispatch 时调 `Snapshot()`,把 snapshot 传给 worker。snapshot 是 dispatch **时刻**的冻结视图:

- 后续 driver 对原 Symbols 的 Set(包括 set_variable 节点产生的 vars 更新)**不影响** snapshot —— map header 已分叉
- snapshot 的所有 leaf 是 `json.RawMessage`,本身不可变;executor 拿到 Lookup 出来的 `any` 是新 unmarshal 的对象,随便改不影响 Symbols 也不影响其他 worker
- 不需要"vars 必须深拷贝"这种解释——RawMessage 已经把 mutation 的口子焊死

> **历史注**:初稿 §11.3 把 vars 深拷贝的原因写成"set_variable 节点并发写"。实际上 driver 是单点写,真实理由是**观察时刻冻结**(让 worker 的视图在它整个生命周期里保持不变,与 determinism 一致)。本版改用 RawMessage 之后这个问题自然消失。

### 11.6 性能权衡

`Lookup` 每次都对子树根做一次 `json.Unmarshal`,如果一个节点 output 50KB 且被多个下游模板/ref 反复引用,会产生重复反序列化开销。本期接受这个代价,理由:
- 大多数 output 在 KB 量级,unmarshal 耗时 < 100µs
- 单 Run 内 Lookup 次数与节点数 / 模板数线性相关,通常 < 100 次
- 一致性收益(immutability + snapshot 零拷贝)显著

后续优化空间(独立 spec):
- `Symbols` 内部加 lazy decoded cache:首次 Lookup 子树时缓存 `any` 结果,Set 时失效该 key
- 仅当 profiling 显示 Lookup 是热点时才做

## 12. ValueSource 求值 + 模板引擎

### 12.1 三种 ValueSource Kind

```go
func (r *Resolver) resolveOne(vs workflow.ValueSource, sym *run.Symbols) (any, error) {
    switch vs.Kind {
    case workflow.ValueKindLiteral:
        return vs.Value, nil
    case workflow.ValueKindRef:
        return r.resolveRef(vs.Value.(workflow.RefValue), sym)
    case workflow.ValueKindTemplate:
        return ExpandTemplate(vs.Value.(string), sym)
    }
}
```

### 12.2 ref 解析

`RefValue` 不携带 port —— 节点 output 是单一对象,与 n8n / Dify / GitHub Actions / Airflow 一致(详见 §3 决策 #12)。

```go
// domain/workflow
type RefValue struct {
    NodeID string   // 上游节点 ID
    Path   string   // 在节点 output 内的 dotted path,如 "body.items.0.name"
}

// domain/engine/resolve.go
func (r *Resolver) resolveRef(ref workflow.RefValue, sym *run.Symbols) (any, error) {
    if _, ok := r.findNode(ref.NodeID); !ok {
        return nil, fmt.Errorf("ref node not found in DSL: %s", ref.NodeID)
    }
    full := "nodes." + ref.NodeID
    if ref.Path != "" {
        full += "." + ref.Path
    }
    return sym.Lookup(full)
}
```

> **多 port 节点的 output 形状契约**:多输出端口的节点(`if` / `switch` / `http_request` / `llm`)必须保证各 port output 的字段集**互不冲突或一致**,否则下游通过 `nodes.<id>.<key>` 拿到的字段会因为本次实际触发的 port 而不同,产生静默歧义。8 个 builtin 的契约见 §13.2 的 Outputs 列。

### 12.3 模板语法与算法

```
{{ <path> }}             — path 用 . 分段;允许两端空白
```

不支持条件 / 循环 / 函数 / 过滤器。

```go
var templatePattern = regexp.MustCompile(`\{\{\s*([^{}]+?)\s*\}\}`)

func ExpandTemplate(s string, sym *run.Symbols) (any, error) {
    if m := wholeMatch(s); m != "" {
        v, err := sym.Lookup(m)
        if err != nil {
            return nil, fmt.Errorf("template %q: %w", s, err)
        }
        return v, nil
    }
    var firstErr error
    out := templatePattern.ReplaceAllStringFunc(s, func(match string) string {
        path := strings.TrimSpace(match[2 : len(match)-2])
        v, err := sym.Lookup(path)
        if err != nil {
            if firstErr == nil {
                firstErr = fmt.Errorf("template %q at %s: %w", s, match, err)
            }
            return match
        }
        return formatScalar(v)                      // 子串拼接 → 字符串化(数字按 -1 精度,无浮点尾巴)
    })
    if firstErr != nil { return nil, firstErr }
    return out, nil
}

// formatScalar:在子串拼接位置把值渲染成字符串。
//   float64(42)   → "42"        (不输出 "42.000000")
//   float64(3.14) → "3.14"
//   bool / string → 默认 fmt.Sprint
//   map / slice   → JSON 表示(否则 fmt.Sprint 输出 Go 风格 map 字面量,无法被下游消费)
func formatScalar(v any) string {
    switch x := v.(type) {
    case float64:
        return strconv.FormatFloat(x, 'f', -1, 64)
    case nil:
        return ""
    case map[string]any, []any:
        b, _ := json.Marshal(x)
        return string(b)
    default:
        return fmt.Sprint(v)
    }
}
```

`Resolver.ResolveConfig` / `ResolveInputs` 在调用 `ExpandTemplate` 后,对返回的 error 再包一层定位信息:

```go
ev, err := ExpandTemplate(s, sym)
if err != nil {
    return nil, fmt.Errorf("at %s.%s: %w", containerKind, fieldPath, err)
}
```

最终用户在错误日志里看到:`at config.url: template "{{trigger.host}}/api": var not set: trigger.host` —— 节点字段 + 原模板 + 原始失败一起呈现。

### 12.4 类型保留(whole-string)

| Template | Symbols 内部存储 | 返回 |
|---|---|---|
| `"{{trigger.count}}"` | JSON `42` | `float64(42)` |
| `"#{{trigger.count}}"` | JSON `42` | `"#42"`(string,经 `formatScalar`) |
| `"{{nodes.n.data}}"` | JSON object | `map[string]any{...}` |
| `"{{nodes.n.tags}}"` | JSON array | `[]any{...}` |
| `"{{trigger.flag}}"` | JSON `true` | `bool(true)` |

> **数字类型说明**:Symbols 走 JSON 存储,所有数字按 Go 默认 `json.Unmarshal` 出 `float64`。这与 n8n 的 single Number 模型一致,绕开了 Go int/float 的痛点。`if` / `switch` 的数字比较见 §13.3。

### 12.5 Config 模板递归展开

Config 是 `json.RawMessage`,只对**字符串值**做 ExpandTemplate;number / bool / null 不动:

```go
func walkExpand(v any, sym *run.Symbols) (any, error) {
    switch x := v.(type) {
    case string:
        return ExpandTemplate(x, sym)
    case map[string]any:
        out := make(map[string]any, len(x))
        for k, val := range x {
            ev, err := walkExpand(val, sym)
            if err != nil { return nil, fmt.Errorf("at %s: %w", k, err) }
            out[k] = ev
        }
        return out, nil
    case []any:
        out := make([]any, len(x))
        for i, val := range x {
            ev, err := walkExpand(val, sym)
            if err != nil { return nil, fmt.Errorf("at [%d]: %w", i, err) }
            out[i] = ev
        }
        return out, nil
    default:
        return v, nil
    }
}
```

### 12.6 错误处理

| 错误 | 触发 | 上抛 |
|---|---|---|
| `path not found`(strict 模式) | sym 里 lookup 不到 | worker 返 err → ErrorPolicy |
| `cannot navigate` | path 走到非 map / array | 同上 |
| `ref unresolved` | RefValue.NodeID 还没 success / fallback | 同上 |

**默认 strict**;`Engine.Config.TemplateMode = lenient` 时 lookup miss 替换为 `""`,whole-string miss 返 nil。

## 13. 8 个 builtin executor

### 13.1 注册一览

```go
// domain/executor/builtin/wire.go
func Register(r executor.ExecutorRegistry) {
    r.Register(nodetype.BuiltinStart,        newStartExecutor)
    r.Register(nodetype.BuiltinEnd,          newEndExecutor)
    r.Register(nodetype.BuiltinLLM,          newLLMExecutor)
    r.Register(nodetype.BuiltinIf,           newIfExecutor)
    r.Register(nodetype.BuiltinSwitch,       newSwitchExecutor)
    r.Register(nodetype.BuiltinJoin,         newJoinExecutor)
    r.Register(nodetype.BuiltinSetVariable,  newSetVariableExecutor)
    r.Register(nodetype.BuiltinHTTPRequest,  newHTTPRequestExecutor)
}
```

### 13.2 各 executor 摘要

| Key | Config | Inputs | Outputs(各 port 字段集) | Ports |
|---|---|---|---|---|
| `builtin.start` | `{}` | `{}` | `default`: 空 map(trigger 通过 `trigger.<key>` 访问,start 不再复述) | `["default"]` |
| `builtin.end` | `{}` | 用户声明返回契约 | 无(Run.Output = ResolvedInputs) | `[]` |
| `builtin.llm` | `{provider, model, system_prompt, temperature, max_tokens}` | `{messages 或 prompt}` | `default`: `{text, model, usage{input_tokens, output_tokens}}`<br>`error`: `{error_code, error_message}` | `["default", "error"]` |
| `builtin.if` | `{operator}` | DSL 声明 `{left, right}` 为 ValueSource;executor 拿到解析后的 `{left: any, right: any}` | `true`/`false`: `{result: bool}`<br>`error`: `{error_code, error_message}` | `["true", "false", "error"]` |
| `builtin.switch` | `{cases: [{name, operator, right}]}`(`right` 内嵌 ValueSource) | DSL 声明 `{value}` 为 ValueSource;executor 拿到 `{value: any}` | 各 case port + `default`: `{matched: <portName>}`<br>`error`: `{error_code, error_message}` | `[<case.name>...] ∪ {"default", "error"}` |
| `builtin.join` | `{mode: "any" \| "all"}` | `{}` | `default`: 空 map | `["default"]` |
| `builtin.set_variable` | `{name}` | `{value: ValueSource}` | `default`: `{<name>: value}` | `["default"]` |
| `builtin.http_request` | `{method, url, headers, body}` | 用户自定义(模板可引用) | `default` (2xx/3xx) / `error` (4xx/5xx): `{status, headers, body}`<br>(transport 错则 err 走 ErrorPolicy,不写 output) | `["default", "error"]` |

字段集设计原则(对应 §3 决策 #12):
- 所有多 port 节点的 `error` port output 字段集统一是 `{error_code, error_message}`,与各 default/分支 port 的字段**不冲突**(后者从不写 `error_code` / `error_message`)
- `http_request` 的 default 与 error port 字段集完全相同(`{status, headers, body}`),下游通过 `nodes.x.status` 读到的语义一致
- `if` 的 true / false port 字段相同(`{result: bool}`)
- `switch` 各 case port 字段相同(`{matched: portName}`)

### 13.3 关键点

- **start**:Outputs = 空 map(`{}`)。trigger 数据通过 `trigger.<key>` 访问,不通过 `nodes.<startID>.<key>`,消除双重表达
- **end**:returns `ExecOutput{Outputs: nil}`,driver 看到节点 TypeKey == BuiltinEnd 即标 `endHit`,触发 first-end-wins(§6.2.2)。Run.Output 由 driver 在 finalize 时从 NodeRun.ResolvedInputs 读
- **if**:
  - operator 支持 `eq / ne / gt / lt / gte / lte / contains / starts_with / is_empty / is_not_empty`
  - **数字比较**:left / right 任一是 `float64` 时,另一侧也尝试 `coerceFloat64`(支持 `int / int64 / float64 / json.Number / 数字字符串`),转不过去返"type mismatch" err 走 ErrorPolicy
  - 字符串比较仅在两侧都是 string 时生效;`contains` / `starts_with` 同理
- **switch**:
  - `Config.cases = [{name, operator, right}]`,executor 顺序匹配命中即 fire `port = case.name`;全部不命中 fire `default`
  - **port 名 = 用户填的 `case.name`**(不再用 `case_*` 占位语法);validator 校验:① 出边 FromPort ∈ `cases[*].name ∪ {"default", "error"}` ② case 名互不重复 ③ 不与 `default` / `error` 撞名 ④ 符合 port name 命名规则(字母/数字/下划线)
  - 数字比较语义同 if
- **join**:无副作用,fired_port 恒 `default`;mode 在 `buildTriggerTable` 时读 Config 决定 evaluate 行为(any/all)。worker 仍走完整 dispatch 路径(保对称性,接受少量 NodeRun 写开销)
- **set_variable**:Outputs = `{cfg.Name: in.Inputs["value"]}`;driver 在 `propagate` 里识别 TypeKey,触发 `sym.SetVar` + persister `SaveVars`(全量,异步)
- **http_request**:走 `ExecServices.HTTPClient`;2xx/3xx fire `default`,4xx/5xx fire `error`(都带完整 `{status, headers, body}`);transport 错(connect refused / timeout)返 err 走 ErrorPolicy
- **llm**:走 `ExecServices.LLMClient`;HTTP 200 fire `default`;provider 业务错(非 200 / refused / safety blocked)fire `error` with `{error_code, error_message}`;网络 / 超时错返 err 走 ErrorPolicy

### 13.3.1 数字 coerce helper

```go
func coerceFloat64(v any) (float64, bool) {
    switch x := v.(type) {
    case float64: return x, true
    case float32: return float64(x), true
    case int:     return float64(x), true
    case int64:   return float64(x), true
    case json.Number:
        if f, err := x.Float64(); err == nil { return f, true }
    case string:
        if f, err := strconv.ParseFloat(x, 64); err == nil { return f, true }
    }
    return 0, false
}
```

在 `if` / `switch` 的 `eq / ne / gt / lt / gte / lte` 算子里:任一侧 `coerceFloat64` 成功且另一侧也能 coerce,则按 float64 比较;否则按"类型严格相等(string == string / bool == bool)"比较;都不匹配返 `op_type_mismatch` 错。

### 13.4 新增 port:`LLMClient`

`domain/executor/exec_input.go` 增补:

```go
type LLMClient interface {
    Complete(ctx context.Context, req LLMRequest) (LLMResponse, error)
}

type LLMRequest struct {
    Provider    string
    Model       string
    Messages    []LLMMessage
    Temperature float64
    MaxTokens   int
}

type LLMMessage struct { Role, Content string }   // role: system | user | assistant

type LLMResponse struct {
    Text  string
    Model string
    Usage LLMUsage
}

type LLMUsage struct { InputTokens, OutputTokens int }

type ExecServices struct {
    Credentials credential.CredentialResolver
    Logger      Logger
    HTTPClient  HTTPClient
    LLMClient   LLMClient                   // 新增
}
```

`ExecServices.HTTPClient / LLMClient` 在 main 装配时**置 nil**(真实 adapter 留独立 spec)。executor 在 nil 时返 `ErrPortNotConfigured`,不让 nil deref 漏到运行期。

### 13.5 builtin NodeType 静态目录

`domain/nodetype/catalog.go`:

```go
func NewBuiltinRegistry() NodeTypeRegistry {
    r := &inMemoryRegistry{byKey: map[string]*NodeType{}}
    for _, nt := range builtinCatalog {
        r.byKey[nt.Key] = nt
    }
    return r
}

var builtinCatalog = []*NodeType{
    startType, endType, llmType, ifType, switchType, joinType, setVariableType, httpRequestType,
    // 注意:loopType 不在 catalog,将来 loop / iteration spec 落地后再加
}
```

每个 NodeType 单独文件:`start_type.go` / `end_type.go` / ...,声明 ConfigSchema / InputSchema / OutputSchema / Ports。

## 14. domain validator 改动

`domain/validator/validator.go` 新增 / 修订规则:

| Code | 规则 |
|---|---|
| `CodeMissingStart`(已有) | DSL 至少 1 个 `builtin.start` |
| `CodeMissingEnd`(已有) | DSL 至少 1 个 `builtin.end` |
| `CodeMultipleStarts`(**新增**) | DSL 至多 1 个 `builtin.start` |
| ~~`CodeMultipleEnds`~~ | **撤回**:多 End 合法,first-end-wins 处理终结(§3 决策 #4/#5) |
| ~~`CodeUnreachableFromEnd`~~ | **撤回**:fire-and-forget 旁支合法 |
| `CodeNoPathToEnd`(**新增**) | DSL 必须存在至少一条 Start→End 的有向路径(若 0 个 End,由 `CodeMissingEnd` 提前拦截;若所有 End 都不可达,本规则报) |
| `CodeCycleDetected`(**新增**) | DSL 必须无环(loop 砍掉后,环边非法) |
| `CodeEdgePortInvalid`(**新增**) | 每条 edge 的 `FromPort` 必须存在于 source 节点的 OutputPorts 中(由 `outputPortsOf` 动态算,见 §14.1.5) |
| `CodeIsolatedNode`(**新增**) | 任何节点必须满足:有入边,或者是 `builtin.start`(start 是 0 入度的合法源点) |
| `CodeMultiInputRequiresJoin`(**新增**) | 入度 > 1 的节点必须是 `builtin.join` |
| `CodeJoinInsufficientInputs`(**新增**) | `builtin.join` 入度必须 ≥ 2 |
| `CodeJoinModeInvalid`(**新增**) | `builtin.join` 的 `Config.mode` 必须是 `any` 或 `all` |
| `CodeJoinConfigInvalid`(**新增**) | `builtin.join` 的 Config 反序列化失败 |
| `CodeSwitchCaseNameDuplicate`(**新增**) | `builtin.switch` 的 `cases[*].name` 互不重复 |
| `CodeSwitchCaseNameReserved`(**新增**) | `builtin.switch` 的 `cases[*].name` 不得为 `default` / `error`,需符合 port name 命名规则(`^[a-zA-Z_][a-zA-Z0-9_]*$`) |
| `CodeFallbackPortInvalid`(**新增**) | `OnFinalFail=Fallback` 时 `FallbackOutput.Port` 必须填,且 ∈ 该节点 NodeType.OutputPorts(switch 节点动态算) |
| `CodeFireErrorPortRequiresErrorPort`(**新增**) | `OnFinalFail=FireErrorPort` 时,该节点 NodeType 必须声明 `error` 端口;否则用户(或默认)选了 FireErrorPort 但节点没 error 出边,会让所有下游被 skip 静默吞错 |

### 14.1 关键规则伪码

#### 14.1.1 至少一条 Start→End 路径

```go
func checkNoPathToEnd(dsl workflow.WorkflowDSL) []ValidationError {
    starts := startNodes(dsl)
    ends   := endNodes(dsl)
    if len(starts) == 0 || len(ends) == 0 { return nil }   // 由 CodeMissingStart/End 拦截

    forward := bfs(starts, dsl.Edges, dirOut)   // 从所有 Start 出发可达集
    for _, e := range ends {
        if forward[e.ID] { return nil }
    }
    return []ValidationError{{
        Code: CodeNoPathToEnd,
        Message: "no directed path from any Start to any End node",
    }}
}
```

#### 14.1.2 无环

标准 DFS three-color(white / gray / black),触发回边即报 `CodeCycleDetected`,Message 带回边的 `from→to`。

#### 14.1.3 边的 FromPort 合法

```go
func checkEdgePortValid(dsl workflow.WorkflowDSL, ntReg NodeTypeRegistry) []ValidationError {
    var errs []ValidationError
    for _, e := range dsl.Edges {
        src := dsl.NodeByID(e.From)
        ports := outputPortsOf(src, ntReg)
        if !contains(ports, e.FromPort) {
            errs = append(errs, ValidationError{
                EdgeID: e.ID, Code: CodeEdgePortInvalid,
                Message: fmt.Sprintf("edge fromPort %q not in source node ports %v", e.FromPort, ports),
            })
        }
    }
    return errs
}
```

#### 14.1.4 outputPortsOf — 动态端口集合

某些节点的输出端口集合不能纯从 NodeType 静态拿,需要看 Config:

```go
// outputPortsOf 计算节点实际拥有的 output 端口集合。
//   - builtin.switch:    cases[*].name ∪ {"default", "error"}
//   - builtin.set_variable: {"default"}(端口固定;Outputs 字段名 = Config.name 是 output schema,
//                           不是 port name。port 永远是 "default")
//   - 其他:               NodeType.OutputPorts(静态目录)
func outputPortsOf(node *workflow.Node, ntReg NodeTypeRegistry) []string {
    switch node.TypeKey {
    case nodetype.BuiltinSwitch:
        cases := parseSwitchCases(node.Config)
        ports := make([]string, 0, len(cases)+2)
        for _, c := range cases { ports = append(ports, c.Name) }
        return append(ports, workflow.PortDefault, workflow.PortError)
    default:
        nt, _ := ntReg.Get(node.TypeKey)
        return nt.OutputPorts
    }
}
```

`set_variable` 的 output 是 `{<cfg.Name>: value}`,**字段名**动态(`<cfg.Name>`),但**端口名**永远是 `default`,所以静态目录就够。switch 是真正的动态端口集合,需要这个 helper。`outputPortsOf` 同时被 `checkEdgePortValid` / `checkFallbackPortInvalid` / `checkFireErrorPortRequiresErrorPort` 共用。

#### 14.1.5 孤儿节点

```go
func checkIsolatedNode(dsl workflow.WorkflowDSL) []ValidationError {
    inDeg := computeInDegree(dsl)
    var errs []ValidationError
    for _, n := range dsl.Nodes {
        if inDeg[n.ID] == 0 && n.TypeKey != nodetype.BuiltinStart {
            errs = append(errs, ValidationError{
                NodeID: n.ID, Code: CodeIsolatedNode,
                Message: fmt.Sprintf("node %s has no inbound edges and is not a Start", n.ID),
            })
        }
    }
    return errs
}
```

## 15. domain 改动总清单

本 spec 落地时一并改 domain:

1. **`domain/run/symbols.go`** 新增(`Symbols` + 读写 + `FromPersistedState`,内部用 `json.RawMessage`)
2. **`domain/run/context.go`** 删除;`context_test.go` 改名 `symbols_test.go`,用例迁移
3. **`domain/run/workflow_run.go`** 加错误码常量:
   - `RunErrCodeNoEndReached = "no_end_reached"`
   - `RunErrCodeTriggerInvalid = "trigger_invalid"`
   - `RunErrCodeOutputNotSerializable = "output_not_serializable"`
   - `RunErrCodePersistence = "persistence_error"`
   - `RunErrCodeNodeExecFailed`(若未存在,补)
4. **`domain/run/node_run.go`** 落 `cancelled` 状态语义 + 文档,落库查询约定:**"节点是否对下游产生了 output"** = `Status='success' OR FallbackApplied=true`(供 RunRepository 实现参考)
5. **`domain/workflow/`** 改:
   - **`RefValue`**:删除 `PortID` 字段,只保留 `NodeID + Path`
   - **`ErrorPolicy.FallbackOutput`**:从 `map[string]any` 改为 `FallbackOutput { Port string; Output map[string]any }` 结构体
6. **`domain/executor/exec_input.go`** 加:
   - `LLMClient` port 接口 + `LLMRequest / LLMMessage / LLMResponse / LLMUsage`
   - `ExecServices.LLMClient` 字段
   - `RunInfo.TriggerPayload json.RawMessage` 字段
7. **`domain/nodetype/builtin.go`** 加:
   - `BuiltinJoin = "builtin.join"` 常量
   - `JoinModeAny = "any"` / `JoinModeAll = "all"` 常量
8. **`domain/nodetype/catalog.go`** 新增,`NewBuiltinRegistry()` + 8 个 NodeType 元数据(loop 不含)
9. **`domain/validator/validator.go`** 新增/调整规则(详见 §14):
   - 撤回 `CodeMultipleEnds` / `CodeUnreachableFromEnd`
   - 新增 `CodeMultipleStarts` / `CodeNoPathToEnd` / `CodeCycleDetected` / `CodeEdgePortInvalid` / `CodeIsolatedNode` / `CodeMultiInputRequiresJoin` / `CodeJoinInsufficientInputs` / `CodeJoinModeInvalid` / `CodeJoinConfigInvalid` / `CodeSwitchCaseNameDuplicate` / `CodeSwitchCaseNameReserved` / `CodeFallbackPortInvalid` / `CodeFireErrorPortRequiresErrorPort`
   - 原 `checkStartEnd` 拆为 `checkSingleStart` + `checkAtLeastOneEnd`
   - 引擎默认 ErrorPolicy.OnFinalFail 由 `FireErrorPort` 改为 `FailRun`(§9.4),domain spec §10.1 默认值需同步修订
10. **`domain/validator/validator_test.go`** 补全部新规则的用例
11. **`domain/doc.go`** 顶层 godoc 加 `engine` 子包说明:工作流执行引擎,事件驱动并发调度,内部 driver / persister 双 goroutine 模型

## 16. 测试方案

### 16.1 测试 utility:`domain/engine/enginetest/`

- `MockExecutor`:可配置的 NodeExecutor(returns / panics / err / sleep);atomic 计数 Calls
- `MockLLMClient` / `MockHTTPClient`:实现对应 port 接口,table-driven 配置
- `DSLBuilder`:链式构造 `WorkflowDSL`(`Start("s").Node("a", ...).Edge("s","a").End("e").Build()`)
- `EngineHarness`:fake `WorkflowRepository` + fake `WorkflowRunRepository` + 已注册 builtin 的 ExecutorRegistry + 已组装的 Engine,一行 `NewEngineHarness(t)` 即得

### 16.2 测试矩阵

| 文件 | 覆盖 |
|---|---|
| `domain/run/symbols_test.go` | NewSymbols 拒绝非 object trigger / Lookup 各路径(trigger/vars/nodes,nested,array index)/ Snapshot 隔离性(driver Set 不影响 snapshot,executor mutate Lookup 结果不影响 Symbols)/ FromPersistedState |
| `domain/engine/template_test.go` | whole-string 类型保留(float64 / map / array / bool)/ 子串拼接 + `formatScalar`(整数无小数尾巴 / map 走 JSON)/ strict 报错带模板原文 / lenient 替换 |
| `domain/engine/resolve_test.go` | ValueSource 三种 Kind / Config 模板递归展开 / RefValue 解析(无 PortID)/ 错误带 `at <field>: ...` 上下文 |
| `domain/engine/scheduler_test.go` | first-end-wins(多 End,某条先 fire,其他 cancel)/ fire-and-forget 旁支 cancelled 不影响 Run.success / fail_run cancel + drain / 父 ctx cancel / 两计数器守恒(`inflight + pendingRetries`)/ retry 多 attempt 且 attempt 行独立 / fallback 走用户指定 port |
| `domain/engine/scheduler_test.go`(续) | join(any) race / join(all) wait + skip / 跑不到的分支 skip 传播 / retry 期间 ctx cancel 走 retryEvent.cancelled / persister 出错 → Run failed with persistence_error / `classifyErr` 严格识别 ctx.Canceled(`fmt.Errorf` 包裹的 ctx 错也算)、不识别业务字符串 |
| `domain/engine/retry_test.go` | computeBackoff:exponential 上限 cap、attempt 上限不溢出、jitter 范围 ±20% / RetryDelay=0 直接 0 |
| `domain/executor/builtin/*_test.go` | 8 个 executor 各覆盖:happy / error port output 形状 / 数字 coerce(int/float/json.Number/string)/ switch 用户填的 case 名作为 port / fallback `FallbackOutput.Port` 触发后 propagate 走该 port |
| `domain/validator/validator_test.go` | 全部新规则 + 原规则;特别覆盖 fire-and-forget 旁支合法、多 End 合法、孤儿节点报错、环报错、edge.FromPort 不在 source ports 报错、switch case 名重复 / reserved 报错、Fallback 缺 Port 报错 |
| `domain/nodetype/catalog_test.go` | NewBuiltinRegistry().Get 8 个 key 都命中;loop 不命中 |

### 16.3 端到端测试(`domain/engine/e2e_test.go`)

用 `infrastructure/storage/storagetest.Setup` 起 testcontainers + 真 RunRepository,4 条用例:

#### Sunshine
DSL:`Start → http_request → if(status==200) → llm → set_variable → End`,模板一路传值。
预期:Run.Status=success;每节点 1 行 NodeRun=success;`vars.<name>` 落库;Run.Output = End.ResolvedInputs。

#### Branch + Join
DSL:`Start → If →(true) A → Join(any) → End` / `... →(false) B → Join(any)`。
预期:走任意分支,Run.Status=success;另一分支节点 NodeRun.Status=skipped。

#### Multi-End first-end-wins
DSL:`Start → If →(true) FastPath → EndA` / `... →(false) Slow → EndB`,mock executor 让 FastPath 立即返回、Slow 故意 sleep。
预期:`Run.Status=success`、`Run.EndNodeID=EndA`、`Slow` 节点 `NodeRun.Status=cancelled`。

#### Retry + Fallback
DSL:`Start → llm(retry=2, fallback={port:"default", output:{text:"sorry"}}) → End`,mock LLMClient 始终 err。
预期:3 行 NodeRun(attempt=1/2/3 全 failed);**第 3 行 FallbackApplied=true、Output={text:"sorry"}**(同一行 update,不新增行);Run.Status=success;Run.Output={text:"sorry"}。

## 17. 装配

### 17.1 main.go 形态

```go
func main() {
    cfg := config.Load()
    storage.MustInit(cfg.DBDSN)

    workflowRepo := wfRepo.NewWorkflowRepository()
    runRepo      := runRepo.NewWorkflowRunRepository()
    credRepoImpl := credRepo.NewCredentialRepository()
    credResolver, err := credRepo.NewResolver(credRepoImpl)
    if err != nil { hlog.Fatalf("credential resolver: %v", err) }

    ntReg := nodetype.NewBuiltinRegistry()
    exReg := executor.NewRegistry()
    builtin.Register(exReg)

    eng := engine.New(workflowRepo, runRepo, ntReg, exReg, executor.ExecServices{
        Credentials: credResolver,
        Logger:      hlogAdapter{},
        HTTPClient:  nil,   // 真实 adapter 后续 spec
        LLMClient:   nil,   // 同上
    }, engine.Config{
        TemplateMode: engine.TemplateStrict,
        RunTimeout:   30 * time.Minute,
    })
    _ = eng

    h := server.New(server.WithHostPorts(":" + cfg.Port))
    httpfacade.Register(h)
    h.Spin()
}
```

facade 仍只挂 `/ping`;`Engine` 实例由后续 facade spec 接入路由。

### 17.2 测试装配

```go
h := enginetest.NewEngineHarness(t,
    enginetest.WithMockHTTPClient(...),
    enginetest.WithMockLLMClient(...),
)
h.UseMock("builtin.if", &MockExecutor{OnExecute: ...})

// 注入 release 版本到 fake WorkflowRepo
h.WorkflowRepo.PutVersion(makeReleaseVersion("v1", dsl))

// 触发
out, err := h.Engine.Start(ctx, engine.StartInput{
    VersionID:   "v1",
    TriggerKind: run.TriggerKindManual,
    TriggerPayload: json.RawMessage(`{"user_id":"u1"}`),
    CreatedBy:   "tester",
})
```

## 18. 验收清单

- [ ] `domain/engine/` 全套文件落地,`go build ./...` 通过
- [ ] `domain/run/symbols.go` 替代 `context.go`,内部 `json.RawMessage` 存储;`FromPersistedState` 输出语义对齐持久化状态(测试覆盖)
- [ ] `domain/run/workflow_run.go` 5 个新错误码常量;`node_run.go` `cancelled` 状态语义清晰
- [ ] `domain/workflow/` `RefValue` 删 PortID;`ErrorPolicy.FallbackOutput` 改结构体 `{Port, Output}`
- [ ] `domain/executor/builtin/` 8 个 executor + `wire.go::Register`;每个 executor 单测覆盖 happy + edge case + error port output 形状
- [ ] `domain/nodetype/builtin.go` 加 `BuiltinJoin / JoinModeAny / JoinModeAll`(`BuiltinLoop` 保留但 catalog 不放)
- [ ] `domain/nodetype/catalog.go` 落地 `NewBuiltinRegistry()`,含 8 个 NodeType
- [ ] `domain/executor/exec_input.go` 补 `LLMClient` port + `ExecServices.LLMClient` + `RunInfo.TriggerPayload`
- [ ] `domain/validator/validator.go` §14 全部新规则 + 撤回旧规则
- [ ] `domain/engine/enginetest/` test utility 落地
- [ ] §16.2 测试矩阵全绿
- [ ] §16.3 4 个 E2E 用例在 testcontainers 上通过(含 multi-End first-end-wins)
- [ ] 守恒专项测试:`TestRetryDuringCtxCancel` 验证 retry timer 在 ctx cancel 后推 cancelled 事件,`pendingRetries` 归零
- [ ] 守恒专项测试:`TestPersisterErrorFailsRun` 验证 persister 出错后 Run 终态 = failed/persistence_error,且所有 inflight worker 完成 drain
- [ ] 守恒专项测试:`TestSymbolsImmutability` 验证 executor 对 Lookup 返回值的 mutation 不污染 Symbols
- [ ] domain spec(`2026-04-22-shineflow-workflow-domain-design.md`)末尾加"修订历史",记录本 spec 对单 End / 可达性 / loop 删除 / merge → join / RefValue / FallbackOutput 这几处的覆盖
- [ ] `go vet ./...` 通过

## 19. 后续 spec 钩子

实现完本 spec,下一份候选(按依赖):

1. **port adapter spec**:`HTTPClient`(net/http)+ `LLMClient`(OpenAI-compatible) 真实 adapter
2. **Registry / 插件投影 spec**:HttpPlugin / McpServer + McpTool 投影到 NodeType,`Invalidate` 缓存
3. **plugin executor spec**:`plugin.http.*` / `plugin.mcp.*.*` 通用 executor
4. **application 层 spec**:`RunService.Start` 入口 + `SaveAndPublish` 联合事务 + DTO 转换
5. **HTTP / Webhook / Cron daemon spec**:facade 路由 + cron 调度器 + sync / async endpoint
6. **builtin.code spec**:沙箱选型(goja / yaegi / wasmtime)+ 注册到 ExecutorRegistry
7. **builtin.loop / iteration spec**:子图嵌套 vs DAG fan-out vs 环边的设计选型 + 实现

每份 spec 仍走 `spec → plan → 实现` 的节奏。

## 20. 修订记录

### 20.1 本 spec 对前置 spec 的覆盖

| 前置 spec | 章节 | 原条款 | 本 spec 覆盖 |
|---|---|---|---|
| domain spec | §11(决策表) | "允许多 End 节点" | **保留**:多 End 合法,first-end-wins(§3 决策 #4/#5、§6.2.2) |
| domain spec | §6.3 | "fire 了 port 但无出边:error 失败,其他 port 分支断头不视为异常" | 由 validator `CodeEdgePortInvalid` + `CodeIsolatedNode` 提前拦截"用户笔误";真正的 fire-and-forget 旁支(节点无出边但有 inbound)合法 |
| domain spec | §7.4 | builtin NodeType 清单含 `builtin.code` / `builtin.loop` | code 仍 out of scope;**loop 本期砍掉**,catalog 不注册,key 占位 |
| domain spec | §10.x | `ErrorPolicy.FallbackOutput map[string]any` | **改结构体** `FallbackOutput { Port, Output }`,fallback 必须显式声明走哪个 port |
| domain spec | RefValue 定义 | `RefValue { NodeID, PortID, Path }` | **删 PortID**,改为 `RefValue { NodeID, Path }`;对齐 n8n / Dify / GitHub Actions / Airflow 寻址(§3 决策 #12) |
| 持久化 spec | §13(后续 spec 钩子) | 候选 #1 是 Registry spec | 本 spec 选了"工作流运行流程"作为下一份;Registry spec 顺延 |

### 20.2 本 spec 内部修订(2026-04-28)

针对内审反馈的覆盖性修订,以下条目相对 2026-04-27 初稿发生了变更:

| 章节 | 修订 | 动机 |
|---|---|---|
| §3 决策表 | 决策 #4 改为多 End + first-end-wins;新增 #5/#11/#12/#14/#16/#17/#18 | 多 End 表达能力;ref/template 寻址对齐业界;数字模型对齐 n8n |
| §6.2 主循环 | `inflight` 拆为 `inflight + pendingRetries`;新增 persister goroutine + persistErrCh;multi-End first-end-wins 通过 `cancel(runCtx)` 实现 | 计数器自我修正可读性差;driver 主循环不再阻塞 DB IO |
| §6.4 持久化时序 | 时序表新增 fallback / retry-cancelled 行,标记同步 vs persister 路径 | 反映 persister 抽象 |
| §8 handleResult | `classifyErr` 严格 `errors.Is(ctx.Canceled / DeadlineExceeded)`;fallback `firedPort` 改为用户指定 port;propagate 不再 JSON round-trip(executor 直接返 `map[string]any`) | 业务错被误判 cancelled;fallback default 端口对 if/switch 不合理;round-trip 损坏类型 |
| §9 retry | `pendingRetries` 替代 inflight 自我修正;backoff 加 jitter / cap / attempt 上限 | 守恒易证;防溢出 / 惊群 |
| §10 worker | `nodeResult.output` 类型 `map[string]any`,worker 不再 `mustMarshal` | 配合 §8 propagate 修订 |
| §11 Symbols | 内部 `json.RawMessage` 存 trigger / vars / nodes;Snapshot 零拷贝;trigger 必须是 JSON object | 关掉 executor mutate 共享状态的口子;修正初稿对 vars 并发的错误解释 |
| §12 ValueSource | RefValue 删 PortID;ExpandTemplate 错误带模板原文;`formatScalar` 处理 float64 整数渲染 | 寻址不一致;模板报错缺上下文;子串拼接出现 "42.000000" |
| §13 executors | switch port = 用户填的 case name;LLM/if/switch error port output = `{error_code, error_message}`;数字 `coerceFloat64` helper;start.outputs = `{}`(不再复述 trigger) | 端口位置编码 brittle;multi-port 字段冲突;trigger 双重表达 |
| §14 validator | 撤回 `CodeMultipleEnds` / `CodeUnreachableFromEnd`;新增 `CodeMultipleStarts` / `CodeNoPathToEnd` / `CodeCycleDetected` / `CodeEdgePortInvalid` / `CodeIsolatedNode` / `CodeSwitchCaseNameDuplicate` / `CodeSwitchCaseNameReserved` / `CodeFallbackPortInvalid` | 多 End 合法;补上 DAG / port / 孤儿等核心校验 |
| §15 domain 改动总清单 | 反映新错误码、`RefValue`/`FallbackOutput` 结构体改动 | 同步 |
| §16 测试矩阵 | 新增 multi-End / persister error / Symbols immutability / backoff jitter 等专项用例 | 覆盖修订带来的新行为 |
| §18 验收清单 | 新增 3 条守恒专项测试条目 | 反映 §16 新增 |

### 20.3 自审第二轮(2026-04-28 晚)

第一轮修订完成后做自审,发现并修复以下问题:

| 章节 | 修订 | 动机 |
|---|---|---|
| §2 Scope | 删除 "多 End / fire-and-forget 旁支" 的 Out of Scope 条目;In Scope 的 validator 描述同步更新 | 与 §3 决策 #4/#5 自相矛盾 |
| §4 包布局 | 新增 `engine/persist.go`(persistOp / persistKind / runPersister);validator.go 函数清单刷新 | 反映 §6.2.1 + §14 的实现拆分 |
| §6.1 Start docstring | 明确终态枚举的归属:driver 主动 cancel(first-end-wins / runFail drain)Run 终态不为 cancelled | 之前 docstring 让人误以为只要 cancel 就 cancelled |
| §6.2 主循环 | 引入 `cancelOnce` helper,所有主动 cancel 路径统一 nil-out ctxDone;`case <-retryCh` dispatch 前增加 `runFail / ctxDone` 防御 | runFail / endHit / persistErr 三条 cancel 路径不对称;retry timer 与 runFail set 之间存在极小 race 窗口 |
| §8 propagate / tryAdvance | 恢复 `ctx context.Context` 参数,从 handleResult 透传到 dispatch | 第一轮误删 ctx,后续 dispatch 拿不到正确 ctx |
| §9.4 默认 ErrorPolicy | OnFinalFail 由 `FireErrorPort` 改为 `FailRun` | set_variable / join 等无 error 端口节点遇到默认 policy 会被静默吞错 |
| §14 validator | 新增 `CodeFireErrorPortRequiresErrorPort`:OnFinalFail=FireErrorPort 时节点必须声明 error 端口 | 与默认改动配套;阻止用户显式选 FireErrorPort 但配在无 error 端口节点上 |
| §14.1.4 outputPortsOf | 抽出 helper,明确 switch 是动态端口、set_variable 端口固定 default(其 Outputs 字段名 = Config.name 与端口无关) | 第一轮在表里只提了 switch,实现共用 helper 才能避免漏 case |
| §13.2 if / switch Inputs 列 | 区分"DSL 声明 ValueSource"与"executor 拿到的解析后 any 值" | 之前 "Inputs: {left: ValueSource}" 容易让 executor 实现者以为运行期还能拿到 ValueSource |
| §11.6 性能权衡 | 新增 Lookup 重复 unmarshal 的代价说明 + lazy cache 优化方向 | reviewer 看到每次 Lookup 都 json.Unmarshal 会担心性能;明确权衡与未来路径 |
| §15 domain 改动总清单 | 加入新规则 + 默认值改动 | 同步落地清单 |
