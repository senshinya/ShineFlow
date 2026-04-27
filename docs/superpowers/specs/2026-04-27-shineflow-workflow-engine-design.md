# ShineFlow 工作流执行引擎设计

- 日期:2026-04-27
- 状态:已定稿,待实现
- 依赖:`2026-04-22-shineflow-workflow-domain-design.md`(领域模型)、`2026-04-26-shineflow-workflow-infra-design.md`(持久化)
- 修订前置 spec:对 domain spec §6.6 / §11 的"多 End / 节点旁支"等条款做了收紧

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
- domain validator 收紧规则(单 End / 可达性 / 多入度 join 强制)
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
- 多 End / fire-and-forget 旁支(本 spec 收紧为单 End + 可达性,见 §3)
- Run 之间并发上限 / 全局 worker 池(单 Run 内已 goroutine 并发;Run 之间隔离由调用方控制)
- 子工作流 / `WorkflowCall` 节点

## 3. 关键决策总览

| #   | 决策                                                                                         | 理由摘要                                                  |
| --- | -------------------------------------------------------------------------------------------- | --------------------------------------------------------- |
| 1   | 引擎落 `domain/engine/`(domain service)                                                     | 编排是工作流业务规则,与 application 用例解耦              |
| 2   | 单 driver goroutine + worker 异步执行 + done/retry channel 事件驱动                          | 状态被 driver 独占,无锁;worker 无状态,纯执行            |
| 3   | DAG 预计算 `triggerTable` + `outAdj`,运行期不再遍历 edges                                   | 一次性投入换运行期 O(1) 查询                              |
| 4   | DSL 强制**单 End** + **可达性**(任意可达节点必须有路径到 End)                              | 简化 Run 终结语义;消除 fire-and-forget 旁支              |
| 5   | `inflight == 0` 是**唯一**的主循环终止条件                                                   | success / failed / cancelled 全走同一出口,逻辑同构        |
| 6   | NodeRun 落盘**只**由 driver 单点写,worker 不调 RunRepository                                | 避免并发写 + 简化推理                                     |
| 7   | retry 由 driver 决策,worker 无重试逻辑;每次 attempt 是独立 dispatch + 独立 NodeRun 行       | 对齐 domain spec §10.3                                    |
| 8   | retry timer 在 ctx 取消窗口里**无条件**推 retryEvent(带 cancelled 标志),保 inflight 守恒    | 避免 retry-cancel race 导致 inflight ghost                |
| 9   | `Symbols` 嵌套符号表(`trigger` / `vars` / `nodes` 三个根命名空间)替代原 `BuildContext`     | 对齐业界(n8n / Dify / GitHub Actions 风格);统一查路径   |
| 10  | `Symbols` 上移到 `domain/run/`,引擎运行期增量构造 + 审计回放一次性构造,共用读 API           | 避免两套类型重复表达"Run 变量表"概念                       |
| 11  | 模板语法 `{{path}}`(无 `text/template` 风格的点前缀;不支持条件 / 循环 / 函数)              | 用户输入语法直白;手写解析 < 100 行                         |
| 12  | `{{x}}` whole-string 时类型保留(数字 / 对象 / 数组不被字符串化)                              | 减少下游 executor 的类型还原成本                          |
| 13  | 新增 `builtin.join` 节点,mode = `any` / `all`;多入度节点必须显式 `builtin.join`              | 区分 race(any)/ 严格 AND-Join(all)语义,DSL 意图清晰        |
| 14  | 引擎不实现 LLMClient / HTTPClient adapter;ExecServices 注入 nil 时 executor 返 `ErrPortNotConfigured` | 真实 adapter 走独立 spec,本期单测 mock                   |
| 15  | 测试用 in-memory `fakeRepo` 跑引擎单测;E2E 才用 testcontainers PG                            | 保单测速度;PG 起一次的成本只摊到几个 E2E 用例             |
| 16  | template 路径解析默认 strict(找不到报错),可选 lenient                                       | debug 时静默替换是噩梦;有意要默认值用 literal             |

## 4. 包布局

```
domain/
├── engine/                              ← 本 spec 主体
│   ├── doc.go                           包说明
│   ├── engine.go                        Engine 结构体 + Start 入口 + Config 选项
│   ├── scheduler.go                     主循环 / triggerTable / evaluate / handleResult
│   ├── nodeexec.go                      worker(runNode):resolve → execute → push 事件
│   ├── retry.go                         scheduleRetry / retryEvent / backoff 计算
│   ├── resolve.go                       ValueSource 求值 / Config 模板递归展开
│   ├── template.go                      ExpandTemplate / regex / wholeMatch
│   ├── result.go                        nodeResult / runState / 持久化 helpers
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
    └── validator.go                     新增 checkSingleEnd / checkReachableToEnd / checkMultiInputRequiresJoin / checkJoinMinInputs / checkJoinMode
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
//   返回 *WorkflowRun.Status 必为终态:success / failed / cancelled。
//   ctx 取消会传播到所有 inflight worker;Run 终态为 cancelled。
func (e *Engine) Start(ctx context.Context, in StartInput) (*run.WorkflowRun, error)
```

### 6.2 单一终止条件:`inflight == 0`

driver 主循环只盯一个 int 计数器:**逻辑上未终结的节点数**。所有"为什么终结"的判断收敛到 `finalize` 一处分发。

```go
func (e *Engine) Start(ctx context.Context, in StartInput) (*run.WorkflowRun, error) {
    // 阶段 1:加载 + 校验 + 创建 Run row
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
    sym := run.NewSymbols()
    if err := sym.SetTrigger(in.TriggerPayload); err != nil { return nil, err }

    state := newRunState(v.DSL, triggers, oa, sym)

    // 阶段 3:Run 级 ctx 套 RunTimeout
    parentCtx := ctx
    if e.runTimeout > 0 {
        var cancelTO context.CancelFunc
        parentCtx, cancelTO = context.WithTimeout(ctx, e.runTimeout)
        defer cancelTO()
    }
    runCtx, cancel := context.WithCancel(parentCtx)
    defer cancel()

    done    := make(chan nodeResult, 32)
    retryCh := make(chan retryEvent, len(v.DSL.Nodes))

    // 阶段 4:bootstrap — 派发入度=0 的节点(只有 builtin.start)
    for nid, spec := range triggers {
        if len(spec.inEdges) == 0 {
            e.dispatch(runCtx, rn, state, nid, done, retryCh)
        }
    }

    // 阶段 5:事件循环 — 唯一退出条件 inflight=0
    ctxDone := runCtx.Done()
    for state.inflight > 0 {
        select {
        case res := <-done:
            state.inflight--
            e.handleResult(runCtx, rn, state, res, done, retryCh)
            if state.runFail != nil {
                cancel()                // 通知所有 inflight worker 退场,不立即 return
            }

        case rt := <-retryCh:
            if rt.cancelled {
                state.inflight--
                e.persistRetryAborted(runCtx, rn, state, rt)
            } else {
                e.dispatch(runCtx, rn, state, rt.nodeID, done, retryCh)
            }

        case <-ctxDone:
            cancel()
            ctxDone = nil               // 后续 select 不再触发该 case
        }
    }

    return e.finalize(ctx, rn, state)
}
```

### 6.3 终态分发

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
        return e.finalizeFailed(ctx, rn, run.RunError{
            Code:    run.RunErrCodeNoEndReached,
            Message: "all branches exhausted but End was not reached",
        })
    }
}
```

`finalizeSuccess` 内部:
- `Run.Output = nodeRuns[End.ID].ResolvedInputs`(domain spec §8.1)
- `Run.EndNodeID = &End.ID`
- `runRepo.SaveEndResult(...)` + `runRepo.SaveVars(...)` + `UpdateStatus(success)`

### 6.4 持久化时序

driver 单点写,worker 不写库:

| 时机 | 调用 |
|---|---|
| `Start` 入口 | `Create` + `UpdateStatus(running, started_at)` |
| dispatch 派发节点 | `AppendNodeRun(status=running, attempt=N)` |
| worker 推回 done | `SaveNodeRunOutput` + `UpdateNodeRunStatus(success/failed)` |
| set_variable 节点 success | 额外 `SaveVars(allVars)` |
| End 节点 success | `SaveEndResult(endNodeID, output)` |
| finalize success | `UpdateStatus(success, ended_at)` |
| finalize failed | `SaveError(runError)` + `UpdateStatus(failed, ended_at)` |
| finalize cancelled | `UpdateStatus(cancelled, ended_at)` |

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
) {
    node := st.byID[res.nodeID]
    ep   := effectivePolicy(node.ErrorPolicy)

    // a) 本次 attempt 落盘
    e.persistAttemptFinish(ctx, rn.ID, res)

    // b) 错误路径:retry / fallback / fail_run / fire_error_port
    if res.err != nil {
        code := classifyErr(res.err)
        if code == codeCancelled {
            // 父 ctx 被取消导致 worker 退场,该节点终结
            st.nodeStat[res.nodeID] = nodeDone
            return
        }
        if res.attempt < ep.MaxRetries+1 {
            // 安排重试
            delay := computeBackoff(ep, res.attempt)
            st.inflight++   // 抵消 case <-done 头部的 --,锁住"节点尚未终结"
            e.scheduleRetry(ctx, st, res.nodeID, res.attempt+1, delay, retryCh)
            return
        }
        // 重试用尽,应用 OnFinalFail
        switch ep.OnFinalFail {
        case workflow.FailStrategyFailRun:
            st.runFail = &run.RunError{
                NodeID: res.nodeID, NodeRunID: res.nodeRunID,
                Code: run.RunErrCodeNodeExecFailed, Message: res.err.Error(),
            }
            return
        case workflow.FailStrategyFallback:
            fbOut := mustMarshal(ep.FallbackOutput)
            e.persistFallback(ctx, rn.ID, res, fbOut)
            // 接下来按"成功 + firedPort=default + output=fallback"推进
            res.output = fbOut
            res.firedPort = workflow.PortDefault
            res.fallbackApplied = true
            // fall through 到下面的 propagate
        case workflow.FailStrategyFireErrorPort:
            res.firedPort = workflow.PortError
            // fall through 到下面的 propagate
        }
    }

    // c) 推进:更新 Symbols + edgeState + 评估下游
    e.propagate(ctx, rn, st, node, res, done, retryCh)
}

func (e *Engine) propagate(
    ctx context.Context, rn *run.WorkflowRun, st *runState,
    node *workflow.Node, res nodeResult,
    done chan<- nodeResult, retryCh chan<- retryEvent,
) {
    // 写 Symbols(成功路径或 fallback 生效)
    var output map[string]any
    if len(res.output) > 0 {
        _ = util.UnmarshalFromString(string(res.output), &output)
    }
    st.sym.SetNodeOutput(node.ID, output)

    if node.TypeKey == nodetype.BuiltinSetVariable {
        for k, v := range output { st.sym.SetVar(k, v) }
        e.runRepo.SaveVars(ctx, rn.ID, mustMarshal(st.sym.AllVars()))
    }

    st.nodeStat[node.ID] = nodeDone

    // End 节点:标 endHit,主循环靠 inflight=0 自然收尾
    if node.TypeKey == nodetype.BuiltinEnd {
        st.endHit = &node.ID
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
        e.tryAdvance(ctx, rn, st, edge.To, done, retryCh)
    }
}

func (e *Engine) tryAdvance(
    ctx context.Context, rn *run.WorkflowRun, st *runState, target string,
    done chan<- nodeResult, retryCh chan<- retryEvent,
) {
    if st.nodeStat[target] != nodeUnready { return }

    switch evaluate(st.triggers[target], st.edgeState) {
    case readyToRun:
        e.dispatch(ctx, rn, st, target, done, retryCh)
    case readyToSkip:
        e.markSkipped(ctx, rn, st, target)
        // skip 传播:把 target 自己的出边全标 dead,递归推进下游
        for _, oe := range st.outAdj[target] {
            st.edgeState[oe.ID] = edgeDead
        }
        for _, oe := range st.outAdj[target] {
            e.tryAdvance(ctx, rn, st, oe.To, done, retryCh)
        }
    case notReady:
        // 等其他入边
    }
}
```

## 9. retry 机制

### 9.1 driver-driven retry

worker 不重试,失败后 driver 决策。每次 attempt 是独立 dispatch + 独立 NodeRun 行(domain spec §10.3)。

### 9.2 retryEvent 与 inflight 守恒

```go
type retryEvent struct {
    nodeID    string
    attempt   int
    cancelled bool   // ctx 在 timer 触发前已取消 → 不 dispatch,只 inflight--
}

func (e *Engine) scheduleRetry(
    ctx context.Context, st *runState,
    nodeID string, nextAttempt int, delay time.Duration, retryCh chan<- retryEvent,
) {
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
    if rt.cancelled {
        state.inflight--          // 抵消 schedule 时的 ++
        e.persistRetryAborted(runCtx, rn, state, rt)   // 写一行 NodeRun(status=failed,error=cancelled)
    } else {
        e.dispatch(runCtx, rn, state, rt.nodeID, done, retryCh)
    }
```

inflight 守恒(retry 路径):

```
attempt N done(失败)        inflight: 1 → 0    ← case <-done 头部 --
decide retry, schedule       inflight: 0 → 1    ← 抵消上一步,锁住"节点尚未终结"
...
[delay 后 timer 触发]
retryCh 收到 cancelled=false:
  dispatch attempt N+1        inflight: 1(变 1,因为 dispatch ++ 自己 +1?抵消方式见下)
```

实现细节:`dispatch` 函数固定 `inflight++`,所以 retry 路径里 `scheduleRetry` 之前的 `++` 在 `<-retryCh` 收到 cancelled=false 时会被 `dispatch` 的 `++` 多算一次。修正为:**`scheduleRetry` 的 `++` 在 `<-retryCh` 非 cancelled 分支由 driver 显式 `--` 抵消**:

```go
case rt := <-retryCh:
    if rt.cancelled {
        state.inflight--
        e.persistRetryAborted(...)
    } else {
        state.inflight--                // 抵消 schedule 时的 ++
        e.dispatch(...)                 // 内部会再 ++
    }
```

净效果一致;读起来更直观。

### 9.3 Backoff 计算

```go
func computeBackoff(ep workflow.ErrorPolicy, attempt int) time.Duration {
    if ep.RetryDelay <= 0 { return 0 }
    switch ep.RetryBackoff {
    case workflow.BackoffExponential:
        return ep.RetryDelay << uint(attempt-1)   // 1×, 2×, 4×, 8×, ...
    default:
        return ep.RetryDelay
    }
}
```

测试侧通过 `Engine.afterFunc` 注入即时触发的假实现,跳过真实等待。

### 9.4 默认 ErrorPolicy

`Node.ErrorPolicy == nil` 时引擎使用包级默认值:

```go
var defaultErrorPolicy = workflow.ErrorPolicy{
    Timeout:        0,                                      // 0 = 不设节点级 timeout,继承父 ctx
    MaxRetries:     0,                                      // 不重试
    RetryBackoff:   workflow.BackoffFixed,
    RetryDelay:     0,
    OnFinalFail:    workflow.FailStrategyFireErrorPort,     // domain spec §10.1 默认
    FallbackOutput: nil,
}

// effectivePolicy 把 nil 解为默认副本;非 nil 直接 deref。
func effectivePolicy(ep *workflow.ErrorPolicy) workflow.ErrorPolicy {
    if ep == nil { return defaultErrorPolicy }
    return *ep
}
```

attempt 计数从 1 开始。给定 `MaxRetries=N`,最大 attempt 数 = `N+1`(初次 + N 次重试)。`res.attempt < ep.MaxRetries+1` 是 retry 判定的不变式。

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

    res.output = mustMarshal(out.Outputs)
    res.firedPort = orDefault(out.FiredPort, workflow.PortDefault)
    res.externalRefs = out.ExternalRefs
}
```

## 11. `Symbols` 符号表

### 11.1 类型

`domain/run/symbols.go`:

```go
// Symbols 是 Run 在某个时间点的变量命名空间。
//
// 三大根命名空间:
//
//   trigger.<key>         ← TriggerPayload 顶层字段
//   vars.<key>            ← set_variable 节点累计写入
//   nodes.<nodeID>.<key>  ← 每个节点最新可用 NodeRun.Output 顶层字段
//                          (可用 = Status==Success 或 FallbackApplied=true)
//
// 引擎运行期增量 Set*;审计 / 回放从持久化状态一次性 FromPersistedState。
// 读取统一走 Lookup / LookupNodeField。
type Symbols struct {
    trigger map[string]any
    vars    map[string]any
    nodes   map[string]map[string]any   // nodeID → output map(顶层字段)
}

func NewSymbols() *Symbols
func (s *Symbols) SetTrigger(payload json.RawMessage) error
func (s *Symbols) SetNodeOutput(nodeID string, output map[string]any)
func (s *Symbols) SetVar(key string, value any)
func (s *Symbols) AllVars() map[string]any
func (s *Symbols) Snapshot() *Symbols                    // 浅拷贝,worker 用
func (s *Symbols) Lookup(path string) (any, error)       // 按 dotted path 走树
func (s *Symbols) LookupNodeField(nodeID, portName, subPath string) (any, error)

// FromPersistedState 从落盘的 Run + NodeRun 一次性构造(审计 / 回放)。
func FromPersistedState(rn *WorkflowRun, nodeRuns []*NodeRun) (*Symbols, error)
```

### 11.2 Lookup 算法

```go
func (s *Symbols) Lookup(path string) (any, error) {
    parts := strings.Split(path, ".")
    var cur any
    var rest []string
    switch parts[0] {
    case "trigger": cur, rest = s.trigger, parts[1:]
    case "vars":    cur, rest = s.vars,    parts[1:]
    case "nodes":
        if len(parts) < 2 { return nil, fmt.Errorf("nodes.<id> required") }
        out, ok := s.nodes[parts[1]]
        if !ok { return nil, fmt.Errorf("node not yet produced output: %s", parts[1]) }
        cur, rest = out, parts[2:]
    default:
        return nil, fmt.Errorf("unknown root: %q", parts[0])
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

### 11.3 Snapshot 语义

driver dispatch 时给 worker 一个浅拷贝。`nodes` 子树共享引用安全(节点 output 落盘后不再变);`vars` 必须深拷贝(set_variable 节点并发写)。

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

```go
func (r *Resolver) resolveRef(ref workflow.RefValue, sym *run.Symbols) (any, error) {
    src, ok := r.findNode(ref.NodeID)
    if !ok { return nil, fmt.Errorf("ref node not found: %s", ref.NodeID) }

    nt, ok := r.ntReg.Get(src.TypeKey)
    if !ok { return nil, fmt.Errorf("ref node type unknown: %s", src.TypeKey) }
    portName, ok := portNameByID(nt.OutputSchema, ref.PortID)
    if !ok { return nil, fmt.Errorf("ref port not in output schema: %s", ref.PortID) }

    return sym.LookupNodeField(ref.NodeID, portName, ref.Path)
}
```

PortID(稳定 UUID)→ port name(NodeType.OutputSchema 翻),再走 `Symbols.LookupNodeField`。

### 12.3 模板语法与算法

```
{{ <path> }}             — path 用 . 分段;允许两端空白
```

不支持条件 / 循环 / 函数 / 过滤器。

```go
var templatePattern = regexp.MustCompile(`\{\{\s*([^{}]+?)\s*\}\}`)

func ExpandTemplate(s string, sym *run.Symbols) (any, error) {
    if m := wholeMatch(s); m != "" {
        return sym.Lookup(m)                       // 整字段 = 一个 {{path}} → 类型保留
    }
    var firstErr error
    out := templatePattern.ReplaceAllStringFunc(s, func(match string) string {
        path := strings.TrimSpace(match[2 : len(match)-2])
        v, err := sym.Lookup(path)
        if err != nil {
            if firstErr == nil { firstErr = err }
            return match
        }
        return fmt.Sprint(v)                        // 子串拼接 → 字符串化
    })
    if firstErr != nil { return nil, firstErr }
    return out, nil
}
```

### 12.4 类型保留(whole-string)

| Template | ctx 值 | 返回 |
|---|---|---|
| `"{{trigger.count}}"` | `42`(int) | `42`(int) |
| `"#{{trigger.count}}"` | `42` | `"#42"`(string) |
| `"{{nodes.n.data}}"` | `{voice_url:..., title:...}` | 整个 object |

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

| Key | Config | Inputs | Outputs | Ports |
|---|---|---|---|---|
| `builtin.start` | `{}` | `{}` | = TriggerPayload 顶层字段 | `["default"]` |
| `builtin.end` | `{}` | 用户声明返回契约 | 无(Run.Output = ResolvedInputs) | `[]` |
| `builtin.llm` | `{provider, model, system_prompt, temperature, max_tokens}` | `{messages 或 prompt}` | `{text, model, usage{input_tokens, output_tokens}}` | `["default", "error"]` |
| `builtin.if` | `{operator}` | `{left: ValueSource, right: ValueSource}` | `{result: bool}` | `["true", "false", "error"]` |
| `builtin.switch` | `{cases: [{name, operator, right}]}` | `{value: ValueSource}` | `{matched}` | `["case_*", "default", "error"]`(动态) |
| `builtin.join` | `{mode: "any" \| "all"}` | `{}` | `{}` | `["default"]` |
| `builtin.set_variable` | `{name}` | `{value: ValueSource}` | `{<name>: value}` | `["default"]` |
| `builtin.http_request` | `{method, url, headers, body}` | 用户自定义(模板可引用) | `{status, headers, body}` | `["default", "error"]` |

### 13.3 关键点

- **start**:读 `RunInfo.TriggerPayload`(本 spec 在 `RunInfo` 新增该字段)→ 反序列化为 map → 作为 Outputs 返回
- **end**:returns `ExecOutput{}`,driver 看到节点 TypeKey == BuiltinEnd 即触发 endHit;Run.Output 由 driver 从 NodeRun.ResolvedInputs 读
- **if**:operator 支持 `eq / ne / gt / lt / gte / lte / contains / starts_with / is_empty / is_not_empty`;类型不匹配返 err 走 ErrorPolicy
- **switch**:Config.cases 用户声明 `{name, operator, right}`,executor 顺序匹配命中即 fire;无命中 fire `default`。validator 校验出边 FromPort 必须在 `cases[*].name ∪ {"default", "error"}`
- **join**:无副作用,fired_port 恒 default;mode 由引擎在 `buildTriggerTable` 时读 Config 决定 evaluate 行为
- **set_variable**:Outputs = `{cfg.Name: in.Inputs["value"]}`;driver 在 `propagate` 里识别 TypeKey 触发额外 `sym.SetVar` + `runRepo.SaveVars`
- **http_request**:走 `ExecServices.HTTPClient`(已存在);4xx/5xx 返响应但 fire `error` 端口;transport 错误返 err 走 ErrorPolicy
- **llm**:走**新增** `ExecServices.LLMClient`;executor 装请求 → 调 → 解响应

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
| `CodeMissingEnd`(已有,语义不变) | DSL 至少 1 个 `builtin.end` |
| `CodeMultipleEnds`(**新增**) | DSL 至多 1 个 `builtin.end` |
| `CodeUnreachableFromEnd`(**新增**) | 任何从 Start 可达的节点必须能反向到 End(双向 BFS 取交集) |
| `CodeMultiInputRequiresJoin`(**新增**) | 入度 > 1 的节点必须是 `builtin.join` |
| `CodeJoinInsufficientInputs`(**新增**) | `builtin.join` 入度必须 ≥ 2 |
| `CodeJoinModeInvalid`(**新增**) | `builtin.join` 的 `Config.mode` 必须是 `any` 或 `all` |
| `CodeJoinConfigInvalid`(**新增**) | `builtin.join` 的 Config 反序列化失败 |

可达性规则伪码:

```go
func checkReachableToEnd(dsl workflow.WorkflowDSL) []ValidationError {
    forward  := bfs(startNodes(dsl),  dsl.Edges, "out")
    backward := bfs(endNodes(dsl),    dsl.Edges, "in")

    var errs []ValidationError
    for nid := range forward {
        if !backward[nid] {
            errs = append(errs, ValidationError{
                NodeID: nid, Code: CodeUnreachableFromEnd,
                Message: fmt.Sprintf("node %s is reachable from start but cannot reach end", nid),
            })
        }
    }
    return errs
}
```

## 15. domain 改动总清单

本 spec 落地时一并改 domain:

1. **`domain/run/symbols.go`** 新增(`Symbols` + 读写 + `FromPersistedState`)
2. **`domain/run/context.go`** 删除;`context_test.go` 改名 `symbols_test.go`,用例迁移
3. **`domain/run/workflow_run.go`** 加 `RunErrCodeNoEndReached = "no_end_reached"` 常量
4. **`domain/executor/exec_input.go`** 加:
   - `LLMClient` port 接口 + `LLMRequest / LLMMessage / LLMResponse / LLMUsage`
   - `ExecServices.LLMClient` 字段
   - `RunInfo.TriggerPayload json.RawMessage` 字段
5. **`domain/nodetype/builtin.go`** 加:
   - `BuiltinJoin = "builtin.join"` 常量
   - `JoinModeAny = "any"` / `JoinModeAll = "all"` 常量
6. **`domain/nodetype/catalog.go`** 新增,`NewBuiltinRegistry()` + 8 个 NodeType 元数据(loop 不含)
7. **`domain/validator/validator.go`** 新增 5 条规则 + 错误码;原 `checkStartEnd` 拆为 `checkSingleStart` + `checkSingleEnd`
8. **`domain/validator/validator_test.go`** 补 5 条规则的用例
9. **`domain/doc.go`** 顶层 godoc 加 `engine` 子包说明:工作流执行引擎,事件驱动并发调度

## 16. 测试方案

### 16.1 测试 utility:`domain/engine/enginetest/`

- `MockExecutor`:可配置的 NodeExecutor(returns / panics / err / sleep);atomic 计数 Calls
- `MockLLMClient` / `MockHTTPClient`:实现对应 port 接口,table-driven 配置
- `DSLBuilder`:链式构造 `WorkflowDSL`(`Start("s").Node("a", ...).Edge("s","a").End("e").Build()`)
- `EngineHarness`:fake `WorkflowRepository` + fake `WorkflowRunRepository` + 已注册 builtin 的 ExecutorRegistry + 已组装的 Engine,一行 `NewEngineHarness(t)` 即得

### 16.2 测试矩阵

| 文件 | 覆盖 |
|---|---|
| `domain/run/symbols_test.go` | Lookup 各路径 / FromPersistedState / Snapshot 隔离性 / vars 写入 / array index / nested path |
| `domain/engine/template_test.go` | whole-string 类型保留 / 子串拼接 / strict 报错 / lenient 替换 / array index |
| `domain/engine/resolve_test.go` | ValueSource 三种 Kind / Config 模板递归展开 / port name 翻译 / number 字段不被改 |
| `domain/engine/scheduler_test.go` | 单 End 完成 / fail_run cancel + drain / 父 ctx cancel / inflight 守恒 / retry 多 attempt / fallback 生效 |
| `domain/engine/scheduler_test.go`(续) | join(any) race / join(all) wait + skip / error 分支跑不到时 skip 传播 / retry 期间 ctx cancel 走 retryEvent.cancelled |
| `domain/executor/builtin/*_test.go` | 8 个 executor 各覆盖 happy + edge case(见 §13.2) |
| `domain/validator/validator_test.go` | 5 条新规则 + 原有规则 |
| `domain/nodetype/catalog_test.go` | NewBuiltinRegistry().Get 8 个 key 都命中;loop 不命中 |

### 16.3 端到端测试(`domain/engine/e2e_test.go`)

用 `infrastructure/storage/storagetest.Setup` 起 testcontainers + 真 RunRepository,3 条用例:

#### Sunshine
DSL:`Start → http_request → if(status==200) → llm → set_variable → End`,模板一路传值。
预期:Run.Status=success;每节点 1 行 NodeRun=success;`vars.<name>` 落库;Run.Output = End.ResolvedInputs。

#### Branch + Join
DSL:`Start → If →(true) A → Join(any) → End` / `... →(false) B → Join(any)`。
预期:走任意分支,Run.Status=success;另一分支节点 NodeRun.Status=skipped。

#### Retry + Fallback
DSL:`Start → llm(retry=2, fallback={text:"sorry"}) → End`,mock LLMClient 始终 err。
预期:3 行 NodeRun(attempt=1/2/3 全 failed);第 3 行 FallbackApplied=true、Output={text:"sorry"};Run.Status=success;Run.Output={text:"sorry"}。

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
- [ ] `domain/run/symbols.go` 替代 `context.go`;`FromPersistedState` 输出语义对齐原 `BuildContext`(测试覆盖)
- [ ] `domain/executor/builtin/` 8 个 executor + `wire.go::Register`;每个 executor 单测覆盖 happy + edge case
- [ ] `domain/nodetype/builtin.go` 加 `BuiltinJoin / JoinModeAny / JoinModeAll`(`BuiltinLoop` 保留但 catalog 不放)
- [ ] `domain/nodetype/catalog.go` 落地 `NewBuiltinRegistry()`,含 8 个 NodeType
- [ ] `domain/executor/exec_input.go` 补 `LLMClient` port + `ExecServices.LLMClient` + `RunInfo.TriggerPayload`
- [ ] `domain/validator/validator.go` 加 5 条规则 + 错误码
- [ ] `domain/engine/enginetest/` test utility 落地
- [ ] §16.2 测试矩阵全绿
- [ ] §16.3 三个 E2E 用例在 testcontainers 上通过
- [ ] inflight 守恒专项测试:`TestRetryDuringCtxCancel` 验证 retry timer 在 ctx cancel 后推 cancelled 事件,inflight 归零
- [ ] domain spec(`2026-04-22-shineflow-workflow-domain-design.md`)末尾加"修订历史",记录本 spec 对单 End / 可达性 / loop 删除 / merge → join 这几处的覆盖
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

本 spec 对前置 spec 做的覆盖性修订:

| 前置 spec | 章节 | 原条款 | 本 spec 覆盖 |
|---|---|---|---|
| domain spec | §11(决策表) | "允许多 End 节点" | **撤回**:DSL 强制单 End,validator `CodeMultipleEnds` |
| domain spec | §6.3 | "fire 了 port 但无出边:error 失败,其他 port 分支断头不视为异常" | 静态校验保证非 error 端口分支不会断头(可达性规则);§6.3 该条作防御性 fallback 描述保留 |
| domain spec | §7.4 | builtin NodeType 清单含 `builtin.code` / `builtin.loop` | code 仍 out of scope;**loop 本期砍掉**,catalog 不注册,key 占位 |
| 持久化 spec | §13(后续 spec 钩子) | 候选 #1 是 Registry spec | 本 spec 选了"工作流运行流程"作为下一份;Registry spec 顺延 |
