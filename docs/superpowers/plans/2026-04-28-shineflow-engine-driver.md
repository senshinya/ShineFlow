# ShineFlow Engine Driver Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the workflow execution engine itself: `domain/engine/` package with driver scheduler, async persister, retry timer, ValueSource resolver, template engine, and `enginetest` harness. Land 4 e2e tests on testcontainers PG that exercise sunshine + branch+join + multi-End first-end-wins + retry+fallback. After this plan, calling `engine.New(...).Start(ctx, in)` runs a real DSL through real builtins and returns a terminal `*WorkflowRun`.

**Architecture:** Single `Engine.Start` entry that spins up two goroutines per Run: the driver (main `Start` flow) handling event loop on `done`/`retryCh`/`persistErrCh`/`ctxDone`, and the persister consuming `persistOp`s sequentially against `runRepo`. Workers are short-lived goroutines spawned per attempt by `dispatch`. Symbols snapshot is given to each worker for read isolation.

**Tech Stack:** Go 1.26 stdlib + `domain/run` (Symbols), `domain/workflow`, `domain/executor` (+ builtin), `domain/nodetype`, `domain/validator`. Testcontainers via `infrastructure/storage/storagetest`.

**Spec reference:** `docs/superpowers/specs/2026-04-27-shineflow-workflow-engine-design.md` §5 (DAG precompute), §6 (main loop + persister), §7 (evaluate), §8 (handleResult/propagate), §9 (retry), §10 (worker), §12 (resolver/template), §16 (test matrix + e2e).

**Depends on:**
- `2026-04-28-shineflow-engine-foundation.md` — must be merged
- `2026-04-28-shineflow-engine-builtins.md` — must be merged

---

## File Structure

| File | Status | Responsibility |
|---|---|---|
| `domain/engine/doc.go` | Create | package doc |
| `domain/engine/engine.go` | Create | `Engine` struct + `Config` + `New` + `StartInput` + `Start` (top-level orchestration) |
| `domain/engine/result.go` | Create | `nodeResult`, `runState`, edge/node enums |
| `domain/engine/persist.go` | Create | `persistOp`, `persistKind`, `runPersister`, `applyPersistOp` |
| `domain/engine/scheduler.go` | Create | `buildTriggerTable`, `evaluate`, `dispatch`, `handleResult`, `propagate`, `tryAdvance`, `markSkipped` |
| `domain/engine/nodeexec.go` | Create | `runNode` worker |
| `domain/engine/retry.go` | Create | `retryEvent`, `scheduleRetry`, `computeBackoff` constants + helper, `effectivePolicy` |
| `domain/engine/resolve.go` | Create | `Resolver` + `ResolveInputs`/`ResolveConfig` + `resolveOne` + `resolveRef` + `walkExpand` |
| `domain/engine/template.go` | Create | `ExpandTemplate`, `formatScalar`, `wholeMatch`, regex |
| `domain/engine/finalize.go` | Create | `finalize`, `finalizeSuccess`, `finalizeFailed`, `finalizeCancelled`, `buildRun` helper |
| `domain/engine/enginetest/harness.go` | Create | `EngineHarness` + `NewEngineHarness` |
| `domain/engine/enginetest/mock_executor.go` | Create | `MockExecutor` + `MockFactory` |
| `domain/engine/enginetest/mock_services.go` | Create | `MockHTTPClient`, `MockLLMClient`, `MockLogger` |
| `domain/engine/enginetest/builder.go` | Create | `DSLBuilder` chainable DSL constructor |
| `domain/engine/enginetest/fake_repo.go` | Create | `FakeWorkflowRepo`, `FakeRunRepo` |
| `domain/engine/scheduler_test.go` | Create | unit tests for `buildTriggerTable`, `evaluate`, persist sequencing, retry race, multi-End, fail_run, fallback port, cancelled propagation |
| `domain/engine/template_test.go` | Create | unit tests for ExpandTemplate (whole-string preservation, formatScalar, strict/lenient errors) |
| `domain/engine/resolve_test.go` | Create | unit tests for Resolver (3 ValueKinds, walkExpand, ref no-PortID, error context) |
| `domain/engine/retry_test.go` | Create | computeBackoff (cap, attempt limit, jitter) |
| `domain/engine/e2e_test.go` | Create | 4 e2e cases via testcontainers |
| `domain/doc.go` | Modify | Append `engine` subpackage description |

---

## Task 1: Engine package skeleton + `Config` + `New`

**Files:**
- Create: `domain/engine/doc.go`, `domain/engine/engine.go`

- [ ] **Step 1: Implement skeleton**

```go
// domain/engine/doc.go
// Package engine drives a published WorkflowVersion to terminal status.
//
// Design:
//   - Single driver goroutine owns runState (no locking).
//   - Persister goroutine consumes persistOps sequentially → driver never blocks on DB IO.
//   - Workers are short-lived goroutines per node attempt; they push results to a done channel.
//   - Two counters (inflight + pendingRetries) decide loop termination.
//
// Spec: docs/superpowers/specs/2026-04-27-shineflow-workflow-engine-design.md
package engine
```

```go
// domain/engine/engine.go
package engine

import (
	"context"
	"errors"
	"math/rand"
	"time"

	"github.com/shinya/shineflow/domain/executor"
	"github.com/shinya/shineflow/domain/nodetype"
	"github.com/shinya/shineflow/domain/run"
	"github.com/shinya/shineflow/domain/workflow"
)

// ErrVersionNotPublished is returned by Start when the requested version
// is not in Release state.
var ErrVersionNotPublished = errors.New("workflow version is not in release state")

// TemplateMode controls strict vs lenient template lookup behavior.
type TemplateMode int

const (
	TemplateStrict TemplateMode = iota
	TemplateLenient
)

// Config holds all tunable engine behavior.
type Config struct {
	Clock        func() time.Time
	NewID        func() string
	AfterFunc    func(time.Duration, func()) (stop func())
	TemplateMode TemplateMode
	RunTimeout   time.Duration
	PersistBuf   int   // size of persistCh buffer; defaults to 64
	RNG          *rand.Rand
}

// Engine is a stateless orchestrator: each Start call gets its own runState.
type Engine struct {
	workflowRepo workflow.WorkflowRepository
	runRepo      run.WorkflowRunRepository
	ntReg        nodetype.NodeTypeRegistry
	exReg        executor.ExecutorRegistry
	services     executor.ExecServices

	cfg Config
}

// New constructs an Engine. Cfg fields with zero values get sensible defaults.
func New(
	workflowRepo workflow.WorkflowRepository,
	runRepo run.WorkflowRunRepository,
	ntReg nodetype.NodeTypeRegistry,
	exReg executor.ExecutorRegistry,
	services executor.ExecServices,
	cfg Config,
) *Engine {
	if cfg.Clock == nil {
		cfg.Clock = time.Now
	}
	if cfg.NewID == nil {
		cfg.NewID = defaultNewID
	}
	if cfg.AfterFunc == nil {
		cfg.AfterFunc = realAfterFunc
	}
	if cfg.PersistBuf <= 0 {
		cfg.PersistBuf = 64
	}
	if cfg.RNG == nil {
		cfg.RNG = rand.New(rand.NewSource(cfg.Clock().UnixNano()))
	}
	return &Engine{
		workflowRepo: workflowRepo,
		runRepo:      runRepo,
		ntReg:        ntReg,
		exReg:        exReg,
		services:     services,
		cfg:          cfg,
	}
}

// StartInput is the request to drive a Run.
type StartInput struct {
	VersionID      string
	TriggerKind    run.TriggerKind
	TriggerRef     string
	TriggerPayload []byte
	CreatedBy      string
}

// Start drives a Run to terminal status. See engine.go docs for terminal semantics.
func (e *Engine) Start(ctx context.Context, in StartInput) (*run.WorkflowRun, error) {
	// Implementation lands in Task 11 (after all subsystems exist).
	return nil, errors.New("not implemented")
}

func defaultNewID() string {
	// Use stdlib crypto/rand-based UUIDv4 in real impl; for now a small placeholder.
	// Replace with real UUID generator in Task 11 when wiring up.
	return "id-placeholder"
}

func realAfterFunc(d time.Duration, fn func()) (stop func()) {
	t := time.AfterFunc(d, fn)
	return func() { t.Stop() }
}
```

- [ ] **Step 2: Verify build**

```bash
cd /Users/shinya/Downloads/ShineFlow && go build ./domain/engine/
```
Expected: PASS (Start stub returns error; that's OK, no caller yet).

- [ ] **Step 3: Commit**

```bash
git add domain/engine/
git commit -m "feat(engine): scaffold engine package with Engine + Config + New

Spec 2026-04-27 §6.1. Start is stubbed; implementations land in
later tasks of the driver plan.

Co-Authored-By: Craft Agent <agents-noreply@craft.do>"
```

---

## Task 2: `result.go` — nodeResult, runState, edge/node enums

**Files:**
- Create: `domain/engine/result.go`

- [ ] **Step 1: Implement**

```go
// domain/engine/result.go
package engine

import (
	"encoding/json"

	"github.com/shinya/shineflow/domain/run"
	"github.com/shinya/shineflow/domain/workflow"
)

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

type nodeStatus int

const (
	nodeUnready nodeStatus = iota
	nodeRunning
	nodeDone
)

type joinMode int

const (
	joinAny joinMode = iota
	joinAll
)

// triggerSpec is the per-node static dispatch spec built once per Run.
type triggerSpec struct {
	nodeID  string
	inEdges []inEdgeRef
	mode    joinMode
}

type inEdgeRef struct {
	EdgeID     string
	SourceNode string
	SourcePort string
}

type triggerTable map[string]*triggerSpec
type outAdj map[string][]workflow.Edge

// nodeResult is the worker → driver event payload.
type nodeResult struct {
	nodeID          string
	nodeRunID       string
	attempt         int
	output          map[string]any   // executor outputs; nil ok
	resolvedInputs  json.RawMessage
	resolvedConfig  json.RawMessage
	firedPort       string
	externalRefs    []run.ExternalRef
	err             error
	fallbackApplied bool
}

// runState is the driver-private mutable state (no locking; only driver mutates).
type runState struct {
	dsl       workflow.WorkflowDSL
	byID      map[string]*workflow.Node
	triggers  triggerTable
	outAdj    outAdj
	sym       *run.Symbols

	edgeState map[string]edgeState
	nodeStat  map[string]nodeStatus

	// Counters for termination.
	inflight       int
	pendingRetries int

	// Outcome flags.
	endHit  *string
	runFail *run.RunError
}

func newRunState(dsl workflow.WorkflowDSL, t triggerTable, oa outAdj, sym *run.Symbols) *runState {
	byID := make(map[string]*workflow.Node, len(dsl.Nodes))
	for i := range dsl.Nodes {
		n := &dsl.Nodes[i]
		byID[n.ID] = n
	}
	return &runState{
		dsl:       dsl,
		byID:      byID,
		triggers:  t,
		outAdj:    oa,
		sym:       sym,
		edgeState: make(map[string]edgeState, len(dsl.Edges)),
		nodeStat:  make(map[string]nodeStatus, len(dsl.Nodes)),
	}
}
```

- [ ] **Step 2: Verify build**

```bash
cd /Users/shinya/Downloads/ShineFlow && go build ./domain/engine/
```
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add domain/engine/result.go
git commit -m "feat(engine): runState + nodeResult + edge/node enums

Spec 2026-04-27 §5/§7. State is driver-private; no locking required.

Co-Authored-By: Craft Agent <agents-noreply@craft.do>"
```

---

## Task 3: `persist.go` — persistOp, persistKind, runPersister

**Files:**
- Create: `domain/engine/persist.go`

- [ ] **Step 1: Implement**

```go
// domain/engine/persist.go
package engine

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/shinya/shineflow/domain/run"
)

type persistKind int

const (
	persistAppendNodeRun persistKind = iota
	persistNodeRunSuccess
	persistNodeRunFailed
	persistNodeRunCancelled
	persistNodeRunFallbackPatch
	persistRetryAborted
	persistSaveVars
	persistMarkSkipped
)

// persistOp is a value-typed message from driver to persister goroutine.
type persistOp struct {
	kind    persistKind
	runID   string
	payload any
}

// runPersister consumes persistOps sequentially. First error reported on errOut;
// loop continues to drain remaining ops (best-effort).
func (e *Engine) runPersister(ctx context.Context, in <-chan persistOp, errOut chan<- error, doneOut chan<- struct{}) {
	defer close(doneOut)
	for op := range in {
		if err := e.applyPersistOp(ctx, op); err != nil {
			select {
			case errOut <- err:
			default:
			}
		}
	}
}

// applyPersistOp dispatches one persistOp to the appropriate runRepo method.
func (e *Engine) applyPersistOp(ctx context.Context, op persistOp) error {
	switch op.kind {
	case persistAppendNodeRun:
		nr, _ := op.payload.(*run.NodeRun)
		return e.runRepo.AppendNodeRun(ctx, op.runID, nr)
	case persistNodeRunSuccess:
		return e.applyAttemptFinish(ctx, op, run.NodeRunStatusSuccess)
	case persistNodeRunFailed:
		return e.applyAttemptFinish(ctx, op, run.NodeRunStatusFailed)
	case persistNodeRunCancelled:
		return e.applyAttemptFinish(ctx, op, run.NodeRunStatusCancelled)
	case persistNodeRunFallbackPatch:
		res, _ := op.payload.(nodeResult)
		outRaw, err := json.Marshal(res.output)
		if err != nil {
			return fmt.Errorf("marshal fallback output: %w", err)
		}
		// Combined update: fallback_applied=true + output + fired_port.
		// Repository may need a dedicated method; for now use SaveNodeRunOutput
		// then UpdateNodeRunStatus(failed) preserving FallbackApplied via opt.
		if err := e.runRepo.SaveNodeRunOutput(ctx, op.runID, res.nodeRunID, outRaw, res.firedPort); err != nil {
			return err
		}
		return e.runRepo.UpdateNodeRunStatus(ctx, op.runID, res.nodeRunID, run.NodeRunStatusFailed,
			run.WithNodeRunFallbackApplied(true),
			run.WithNodeRunEndedAt(e.cfg.Clock()))
	case persistRetryAborted:
		// Append a synthetic NodeRun row recording the cancelled retry.
		nr, _ := op.payload.(*run.NodeRun)
		return e.runRepo.AppendNodeRun(ctx, op.runID, nr)
	case persistSaveVars:
		raw, _ := op.payload.(json.RawMessage)
		return e.runRepo.SaveVars(ctx, op.runID, raw)
	case persistMarkSkipped:
		nr, _ := op.payload.(*run.NodeRun)
		return e.runRepo.AppendNodeRun(ctx, op.runID, nr)
	}
	return fmt.Errorf("unknown persistKind: %d", op.kind)
}

func (e *Engine) applyAttemptFinish(ctx context.Context, op persistOp, status run.NodeRunStatus) error {
	res, _ := op.payload.(nodeResult)
	outRaw, err := json.Marshal(res.output)
	if err != nil {
		return fmt.Errorf("marshal node output: %w", err)
	}
	if err := e.runRepo.SaveNodeRunOutput(ctx, op.runID, res.nodeRunID, outRaw, res.firedPort); err != nil {
		return err
	}
	opts := []run.NodeRunUpdateOpt{run.WithNodeRunEndedAt(e.cfg.Clock())}
	if res.err != nil {
		opts = append(opts, run.WithNodeRunError(&run.NodeError{
			Code:    "node_exec_failed",
			Message: res.err.Error(),
		}))
	}
	return e.runRepo.UpdateNodeRunStatus(ctx, op.runID, res.nodeRunID, status, opts...)
}
```

- [ ] **Step 2: Verify build**

```bash
cd /Users/shinya/Downloads/ShineFlow && go build ./domain/engine/
```
Expected: errors if `WithNodeRun*Opt` helpers don't exist on the run package. Verify and adjust:

```bash
grep -n "WithNodeRun" /Users/shinya/Downloads/ShineFlow/domain/run/repository.go
```

If `WithNodeRunFallbackApplied` / `WithNodeRunEndedAt` / `WithNodeRunError` don't exist, add them to `domain/run/repository.go`:

```go
// domain/run/repository.go (append if missing)
type NodeRunUpdateOpt func(*NodeRunUpdate)

type NodeRunUpdate struct {
	EndedAt         *time.Time
	Error           *NodeError
	FallbackApplied *bool
}

func WithNodeRunEndedAt(t time.Time) NodeRunUpdateOpt {
	return func(u *NodeRunUpdate) { u.EndedAt = &t }
}

func WithNodeRunError(e *NodeError) NodeRunUpdateOpt {
	return func(u *NodeRunUpdate) { u.Error = e }
}

func WithNodeRunFallbackApplied(b bool) NodeRunUpdateOpt {
	return func(u *NodeRunUpdate) { u.FallbackApplied = &b }
}
```

Update repository implementation if needed to honor these opts.

- [ ] **Step 3: Verify build again + commit**

```bash
cd /Users/shinya/Downloads/ShineFlow && go build ./...
git add domain/engine/persist.go domain/run/
git commit -m "feat(engine): persister goroutine + applyPersistOp

Spec 2026-04-27 §6.2.1/§6.4. NodeRun update opts added to support
fallback patch + ended-at + error in a single repository call.

Co-Authored-By: Craft Agent <agents-noreply@craft.do>"
```

---

## Task 4: `template.go` — ExpandTemplate, formatScalar, wholeMatch

**Files:**
- Create: `domain/engine/template.go`
- Create: `domain/engine/template_test.go`

- [ ] **Step 1: Write failing tests**

```go
// domain/engine/template_test.go
package engine

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/shinya/shineflow/domain/run"
)

func mustSym(t *testing.T, payload string) *run.Symbols {
	t.Helper()
	s, err := run.NewSymbols(json.RawMessage(payload))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestExpandWholeStringPreservesType(t *testing.T) {
	sym := mustSym(t, `{"count":42,"flag":true,"data":{"a":1}}`)

	got, _ := ExpandTemplate("{{trigger.count}}", sym)
	if v, _ := got.(float64); v != 42 {
		t.Fatalf("count: %v", got)
	}

	got, _ = ExpandTemplate("{{trigger.flag}}", sym)
	if v, _ := got.(bool); !v {
		t.Fatalf("flag: %v", got)
	}

	got, _ = ExpandTemplate("{{trigger.data}}", sym)
	if _, ok := got.(map[string]any); !ok {
		t.Fatalf("data not map: %T", got)
	}
}

func TestExpandSubstringStringifies(t *testing.T) {
	sym := mustSym(t, `{"count":42,"name":"alice"}`)

	got, _ := ExpandTemplate("#{{trigger.count}}", sym)
	if got != "#42" {
		t.Fatalf("got %q", got)
	}

	got, _ = ExpandTemplate("hello {{trigger.name}}!", sym)
	if got != "hello alice!" {
		t.Fatalf("got %q", got)
	}
}

func TestExpandFloat64IntegerNoDecimalTail(t *testing.T) {
	sym := mustSym(t, `{"x":42}`)
	got, _ := ExpandTemplate("v={{trigger.x}}", sym)
	if got != "v=42" {
		t.Fatalf("got %q (want v=42, no .0 tail)", got)
	}
}

func TestExpandMapInSubstringJSON(t *testing.T) {
	sym := mustSym(t, `{"d":{"a":1}}`)
	got, _ := ExpandTemplate("data={{trigger.d}}", sym)
	if !strings.Contains(got.(string), `{"a":1}`) {
		t.Fatalf("got %q", got)
	}
}

func TestExpandStrictReportsError(t *testing.T) {
	sym := mustSym(t, `{}`)
	_, err := ExpandTemplate("{{trigger.missing}}", sym)
	if err == nil {
		t.Fatal("expected err in strict mode")
	}
	if !strings.Contains(err.Error(), `template "{{trigger.missing}}"`) {
		t.Fatalf("error must contain template text, got %v", err)
	}
}

func TestExpandLiteralPassthrough(t *testing.T) {
	sym := mustSym(t, `{}`)
	got, _ := ExpandTemplate("plain text", sym)
	if got != "plain text" {
		t.Fatalf("got %v", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd /Users/shinya/Downloads/ShineFlow && go test ./domain/engine/ -run TestExpand
```
Expected: FAIL — "ExpandTemplate undefined".

- [ ] **Step 3: Implement**

```go
// domain/engine/template.go
package engine

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/shinya/shineflow/domain/run"
)

var templatePattern = regexp.MustCompile(`\{\{\s*([^{}]+?)\s*\}\}`)

// ExpandTemplate evaluates `{{path}}` references against sym.
//
// Whole-string template ("{{x}}") preserves the underlying type.
// Mixed substring template stringifies via formatScalar.
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
		return formatScalar(v)
	})
	if firstErr != nil {
		return nil, firstErr
	}
	return out, nil
}

// wholeMatch returns the inner path if s is exactly one {{path}} (with optional whitespace).
func wholeMatch(s string) string {
	loc := templatePattern.FindStringIndex(s)
	if loc == nil || loc[0] != 0 || loc[1] != len(s) {
		return ""
	}
	m := templatePattern.FindStringSubmatch(s)
	if len(m) < 2 {
		return ""
	}
	return strings.TrimSpace(m[1])
}

// formatScalar renders a value for substring concatenation.
func formatScalar(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case map[string]any, []any:
		b, _ := json.Marshal(x)
		return string(b)
	default:
		return fmt.Sprint(v)
	}
}
```

- [ ] **Step 4: Run tests + commit**

```bash
cd /Users/shinya/Downloads/ShineFlow && go test ./domain/engine/ -run TestExpand
git add domain/engine/template.go domain/engine/template_test.go
git commit -m "feat(engine): ExpandTemplate + formatScalar

Spec 2026-04-27 §12.3/§12.4. Whole-string preserves type;
substring uses formatScalar (no float .0 tails).

Co-Authored-By: Craft Agent <agents-noreply@craft.do>"
```

---

## Task 5: `resolve.go` — Resolver, ResolveInputs, ResolveConfig, walkExpand

**Files:**
- Create: `domain/engine/resolve.go`
- Create: `domain/engine/resolve_test.go`

- [ ] **Step 1: Write failing tests**

```go
// domain/engine/resolve_test.go
package engine

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/shinya/shineflow/domain/nodetype"
	"github.com/shinya/shineflow/domain/run"
	"github.com/shinya/shineflow/domain/workflow"
)

func TestResolveLiteral(t *testing.T) {
	r := newTestResolver(t, nil)
	v, err := r.resolveOne(workflow.ValueSource{Kind: workflow.ValueKindLiteral, Value: 42}, mustSym(t, `{}`))
	if err != nil {
		t.Fatal(err)
	}
	if v != 42 {
		t.Fatalf("got %v", v)
	}
}

func TestResolveRefNoPortID(t *testing.T) {
	dsl := workflow.WorkflowDSL{Nodes: []workflow.Node{
		{ID: "n1", TypeKey: nodetype.BuiltinSetVariable},
	}}
	r := newTestResolver(t, &dsl)
	sym := mustSym(t, `{}`)
	_ = sym // need to add nodes.n1.foo
	if err := sym.SetNodeOutput("n1", map[string]any{"foo": "bar"}); err != nil {
		t.Fatal(err)
	}
	v, err := r.resolveOne(workflow.ValueSource{
		Kind:  workflow.ValueKindRef,
		Value: workflow.RefValue{NodeID: "n1", Path: "foo"},
	}, sym)
	if err != nil {
		t.Fatal(err)
	}
	if v != "bar" {
		t.Fatalf("got %v", v)
	}
}

func TestResolveRefMissingNode(t *testing.T) {
	dsl := workflow.WorkflowDSL{Nodes: []workflow.Node{}}
	r := newTestResolver(t, &dsl)
	_, err := r.resolveOne(workflow.ValueSource{
		Kind:  workflow.ValueKindRef,
		Value: workflow.RefValue{NodeID: "ghost"},
	}, mustSym(t, `{}`))
	if err == nil || !strings.Contains(err.Error(), "ref node not found") {
		t.Fatalf("got %v", err)
	}
}

func TestResolveTemplate(t *testing.T) {
	r := newTestResolver(t, nil)
	sym := mustSym(t, `{"x":"hello"}`)
	v, err := r.resolveOne(workflow.ValueSource{
		Kind:  workflow.ValueKindTemplate,
		Value: "world: {{trigger.x}}",
	}, sym)
	if err != nil {
		t.Fatal(err)
	}
	if v != "world: hello" {
		t.Fatalf("got %v", v)
	}
}

func TestResolveConfigTemplateRecursion(t *testing.T) {
	r := newTestResolver(t, nil)
	sym := mustSym(t, `{"host":"api.test"}`)
	cfg := json.RawMessage(`{"url":"https://{{trigger.host}}/v1","retries":3,"flag":true}`)
	out, err := r.ResolveConfig(cfg, sym)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := out["url"].(string)
	if !ok || got != "https://api.test/v1" {
		t.Fatalf("url: %v", out["url"])
	}
	// number/bool unchanged
	if v, _ := out["retries"].(float64); v != 3 {
		t.Fatalf("retries: %v", out["retries"])
	}
	if v, _ := out["flag"].(bool); !v {
		t.Fatalf("flag: %v", out["flag"])
	}
}

func TestResolveErrorIncludesContext(t *testing.T) {
	r := newTestResolver(t, nil)
	cfg := json.RawMessage(`{"url":"{{trigger.missing}}"}`)
	_, err := r.ResolveConfig(cfg, mustSym(t, `{}`))
	if err == nil || !strings.Contains(err.Error(), "url") {
		t.Fatalf("expected error with 'url' context, got %v", err)
	}
}

// helper
func newTestResolver(t *testing.T, dsl *workflow.WorkflowDSL) *Resolver {
	t.Helper()
	if dsl == nil {
		empty := workflow.WorkflowDSL{}
		dsl = &empty
	}
	return &Resolver{
		dsl:   dsl,
		ntReg: nodetype.NewBuiltinRegistry(),
	}
}

// re-use mustSym from template_test.go (same package)
var _ = run.NewSymbols
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd /Users/shinya/Downloads/ShineFlow && go test ./domain/engine/ -run TestResolve
```
Expected: FAIL.

- [ ] **Step 3: Implement**

```go
// domain/engine/resolve.go
package engine

import (
	"encoding/json"
	"fmt"

	"github.com/shinya/shineflow/domain/nodetype"
	"github.com/shinya/shineflow/domain/run"
	"github.com/shinya/shineflow/domain/workflow"
)

// Resolver evaluates ValueSources against a Symbols snapshot.
type Resolver struct {
	dsl   *workflow.WorkflowDSL
	ntReg nodetype.NodeTypeRegistry
}

func newResolver(dsl *workflow.WorkflowDSL, ntReg nodetype.NodeTypeRegistry) *Resolver {
	return &Resolver{dsl: dsl, ntReg: ntReg}
}

// ResolveInputs resolves every Input ValueSource on a node.
func (r *Resolver) ResolveInputs(node *workflow.Node, sym *run.Symbols) (map[string]any, error) {
	if len(node.Inputs) == 0 {
		return map[string]any{}, nil
	}
	out := make(map[string]any, len(node.Inputs))
	for k, vs := range node.Inputs {
		v, err := r.resolveOne(vs, sym)
		if err != nil {
			return nil, fmt.Errorf("at inputs.%s: %w", k, err)
		}
		out[k] = v
	}
	return out, nil
}

// ResolveConfig recursively expands templates in any string leaf of Config.
func (r *Resolver) ResolveConfig(raw json.RawMessage, sym *run.Symbols) (map[string]any, error) {
	if len(raw) == 0 {
		return map[string]any{}, nil
	}
	var root any
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	expanded, err := walkExpand(root, sym)
	if err != nil {
		return nil, err
	}
	m, ok := expanded.(map[string]any)
	if !ok {
		// Unusual: config root is non-object; wrap in single-key map for consistency.
		return map[string]any{"_root": expanded}, nil
	}
	return m, nil
}

func (r *Resolver) resolveOne(vs workflow.ValueSource, sym *run.Symbols) (any, error) {
	switch vs.Kind {
	case workflow.ValueKindLiteral:
		return vs.Value, nil
	case workflow.ValueKindRef:
		ref, ok := vs.Value.(workflow.RefValue)
		if !ok {
			// Common path: JSON-deserialized RefValue arrives as map[string]any.
			ref = coerceRefValue(vs.Value)
		}
		return r.resolveRef(ref, sym)
	case workflow.ValueKindTemplate:
		s, ok := vs.Value.(string)
		if !ok {
			return nil, fmt.Errorf("template value must be string, got %T", vs.Value)
		}
		return ExpandTemplate(s, sym)
	default:
		return nil, fmt.Errorf("unknown ValueSource kind: %v", vs.Kind)
	}
}

func (r *Resolver) resolveRef(ref workflow.RefValue, sym *run.Symbols) (any, error) {
	if r.dsl != nil {
		found := false
		for i := range r.dsl.Nodes {
			if r.dsl.Nodes[i].ID == ref.NodeID {
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("ref node not found in DSL: %s", ref.NodeID)
		}
	}
	full := "nodes." + ref.NodeID
	if ref.Path != "" {
		full += "." + ref.Path
	}
	return sym.Lookup(full)
}

func coerceRefValue(v any) workflow.RefValue {
	m, _ := v.(map[string]any)
	out := workflow.RefValue{}
	if s, ok := m["node_id"].(string); ok {
		out.NodeID = s
	}
	if s, ok := m["path"].(string); ok {
		out.Path = s
	}
	return out
}

func walkExpand(v any, sym *run.Symbols) (any, error) {
	switch x := v.(type) {
	case string:
		return ExpandTemplate(x, sym)
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			ev, err := walkExpand(val, sym)
			if err != nil {
				return nil, fmt.Errorf("at %s: %w", k, err)
			}
			out[k] = ev
		}
		return out, nil
	case []any:
		out := make([]any, len(x))
		for i, val := range x {
			ev, err := walkExpand(val, sym)
			if err != nil {
				return nil, fmt.Errorf("at [%d]: %w", i, err)
			}
			out[i] = ev
		}
		return out, nil
	default:
		return v, nil
	}
}
```

- [ ] **Step 4: Run tests + commit**

```bash
cd /Users/shinya/Downloads/ShineFlow && go test ./domain/engine/ -run TestResolve
git add domain/engine/resolve.go domain/engine/resolve_test.go
git commit -m "feat(engine): Resolver + ValueSource + walkExpand

Spec 2026-04-27 §12.1/§12.2/§12.5. Ref no longer carries PortID.
Errors wrap with 'at <field>' context.

Co-Authored-By: Craft Agent <agents-noreply@craft.do>"
```

---

## Task 6: `retry.go` — retryEvent, scheduleRetry, computeBackoff, effectivePolicy

**Files:**
- Create: `domain/engine/retry.go`
- Create: `domain/engine/retry_test.go`

- [ ] **Step 1: Write failing tests**

```go
// domain/engine/retry_test.go
package engine

import (
	"math/rand"
	"testing"
	"time"

	"github.com/shinya/shineflow/domain/workflow"
)

func TestComputeBackoffZeroDelay(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	d := computeBackoff(workflow.ErrorPolicy{RetryDelay: 0}, 1, rng)
	if d != 0 {
		t.Fatalf("got %v", d)
	}
}

func TestComputeBackoffFixed(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for i := 1; i < 5; i++ {
		d := computeBackoff(workflow.ErrorPolicy{RetryDelay: time.Second, RetryBackoff: workflow.BackoffFixed}, i, rng)
		// jitter ±20%
		if d < 800*time.Millisecond || d > 1200*time.Millisecond {
			t.Fatalf("attempt %d outside jitter range: %v", i, d)
		}
	}
}

func TestComputeBackoffExponentialCap(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	// huge attempt — must cap at maxBackoffDelay (30s) before jitter
	d := computeBackoff(workflow.ErrorPolicy{RetryDelay: time.Second, RetryBackoff: workflow.BackoffExponential}, 100, rng)
	// 30s ± 20% = [24s, 36s]
	if d < 24*time.Second || d > 36*time.Second {
		t.Fatalf("not capped: %v", d)
	}
}

func TestEffectivePolicyDefaultIsFailRun(t *testing.T) {
	p := effectivePolicy(nil)
	if p.OnFinalFail != workflow.FailStrategyFailRun {
		t.Fatalf("default OnFinalFail must be FailRun, got %v", p.OnFinalFail)
	}
}

func TestEffectivePolicyExplicitOverrides(t *testing.T) {
	p := effectivePolicy(&workflow.ErrorPolicy{OnFinalFail: workflow.FailStrategyFireErrorPort, MaxRetries: 5})
	if p.OnFinalFail != workflow.FailStrategyFireErrorPort {
		t.Fatalf("got %v", p.OnFinalFail)
	}
	if p.MaxRetries != 5 {
		t.Fatalf("got %d", p.MaxRetries)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd /Users/shinya/Downloads/ShineFlow && go test ./domain/engine/ -run "TestComputeBackoff|TestEffectivePolicy"
```
Expected: FAIL.

- [ ] **Step 3: Implement**

```go
// domain/engine/retry.go
package engine

import (
	"context"
	"math/rand"
	"time"

	"github.com/shinya/shineflow/domain/workflow"
)

const (
	maxBackoffDelay = 30 * time.Second
	maxBackoffShift = 8
	jitterFraction  = 0.2
)

var defaultErrorPolicy = workflow.ErrorPolicy{
	Timeout:        0,
	MaxRetries:     0,
	RetryBackoff:   workflow.BackoffFixed,
	RetryDelay:     0,
	OnFinalFail:    workflow.FailStrategyFailRun,
	FallbackOutput: workflow.FallbackOutput{},
}

func effectivePolicy(ep *workflow.ErrorPolicy) workflow.ErrorPolicy {
	if ep == nil {
		return defaultErrorPolicy
	}
	return *ep
}

type retryEvent struct {
	nodeID    string
	attempt   int
	cancelled bool
}

// scheduleRetry queues a retryEvent after delay. The fired event always pushes
// to retryCh; the cancelled flag tells driver to NOT dispatch a new attempt.
// Caller must `state.pendingRetries++` before calling.
func (e *Engine) scheduleRetry(ctx context.Context, nodeID string, nextAttempt int, delay time.Duration, retryCh chan<- retryEvent) {
	e.cfg.AfterFunc(delay, func() {
		ev := retryEvent{nodeID: nodeID, attempt: nextAttempt}
		select {
		case <-ctx.Done():
			ev.cancelled = true
		default:
		}
		retryCh <- ev
	})
}

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
	delta := time.Duration(float64(base) * jitterFraction * (rng.Float64()*2 - 1))
	return base + delta
}
```

- [ ] **Step 4: Run tests + commit**

```bash
cd /Users/shinya/Downloads/ShineFlow && go test ./domain/engine/ -run "TestComputeBackoff|TestEffectivePolicy"
git add domain/engine/retry.go domain/engine/retry_test.go
git commit -m "feat(engine): retry + backoff with jitter/cap + default FailRun policy

Spec 2026-04-27 §9.3/§9.4. Default OnFinalFail = FailRun (safer for
nodes lacking error ports).

Co-Authored-By: Craft Agent <agents-noreply@craft.do>"
```

---

## Task 7: `scheduler.go` — buildTriggerTable + evaluate

**Files:**
- Create: `domain/engine/scheduler.go`

- [ ] **Step 1: Write failing tests**

```go
// domain/engine/scheduler_test.go (create)
package engine

import (
	"encoding/json"
	"testing"

	"github.com/shinya/shineflow/domain/nodetype"
	"github.com/shinya/shineflow/domain/workflow"
)

func TestBuildTriggerTablePopulatesInEdgesAndOutAdj(t *testing.T) {
	dsl := workflow.WorkflowDSL{
		Nodes: []workflow.Node{
			{ID: "s", TypeKey: nodetype.BuiltinStart},
			{ID: "a", TypeKey: nodetype.BuiltinSetVariable},
			{ID: "j", TypeKey: nodetype.BuiltinJoin, Config: json.RawMessage(`{"mode":"any"}`)},
			{ID: "e", TypeKey: nodetype.BuiltinEnd},
		},
		Edges: []workflow.Edge{
			{ID: "e1", From: "s", FromPort: "default", To: "a"},
			{ID: "e2", From: "s", FromPort: "default", To: "j"},
			{ID: "e3", From: "a", FromPort: "default", To: "j"},
			{ID: "e4", From: "j", FromPort: "default", To: "e"},
		},
	}
	tt, oa := buildTriggerTable(dsl)

	if len(tt["s"].inEdges) != 0 {
		t.Fatalf("start has no inbound: got %d", len(tt["s"].inEdges))
	}
	if len(tt["j"].inEdges) != 2 {
		t.Fatalf("join inbound: got %d", len(tt["j"].inEdges))
	}
	if tt["j"].mode != joinAny {
		t.Fatalf("join mode: %v", tt["j"].mode)
	}
	if len(oa["s"]) != 2 {
		t.Fatalf("start outAdj: %d", len(oa["s"]))
	}
}

func TestEvaluateZeroInputs(t *testing.T) {
	r := evaluate(&triggerSpec{}, nil)
	if r != readyToRun {
		t.Fatalf("got %v", r)
	}
}

func TestEvaluateSingleInput(t *testing.T) {
	spec := &triggerSpec{inEdges: []inEdgeRef{{EdgeID: "e1"}}}
	es := map[string]edgeState{"e1": edgePending}
	if evaluate(spec, es) != notReady {
		t.Fatal("pending should be notReady")
	}
	es["e1"] = edgeLive
	if evaluate(spec, es) != readyToRun {
		t.Fatal("live should be ready")
	}
	es["e1"] = edgeDead
	if evaluate(spec, es) != readyToSkip {
		t.Fatal("dead should be skip")
	}
}

func TestEvaluateJoinAny(t *testing.T) {
	spec := &triggerSpec{
		inEdges: []inEdgeRef{{EdgeID: "e1"}, {EdgeID: "e2"}},
		mode:    joinAny,
	}
	es := map[string]edgeState{"e1": edgeLive, "e2": edgePending}
	if evaluate(spec, es) != readyToRun {
		t.Fatal("any+live should fire immediately")
	}
	es = map[string]edgeState{"e1": edgePending, "e2": edgePending}
	if evaluate(spec, es) != notReady {
		t.Fatal("any+all-pending should wait")
	}
	es = map[string]edgeState{"e1": edgeDead, "e2": edgeDead}
	if evaluate(spec, es) != readyToSkip {
		t.Fatal("any+all-dead should skip")
	}
}

func TestEvaluateJoinAll(t *testing.T) {
	spec := &triggerSpec{
		inEdges: []inEdgeRef{{EdgeID: "e1"}, {EdgeID: "e2"}},
		mode:    joinAll,
	}
	es := map[string]edgeState{"e1": edgeLive, "e2": edgePending}
	if evaluate(spec, es) != notReady {
		t.Fatal("all + pending should wait")
	}
	es = map[string]edgeState{"e1": edgeLive, "e2": edgeLive}
	if evaluate(spec, es) != readyToRun {
		t.Fatal("all live should fire")
	}
	es = map[string]edgeState{"e1": edgeLive, "e2": edgeDead}
	if evaluate(spec, es) != readyToSkip {
		t.Fatal("all + any-dead should skip")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd /Users/shinya/Downloads/ShineFlow && go test ./domain/engine/ -run "TestBuildTriggerTable|TestEvaluate"
```
Expected: FAIL.

- [ ] **Step 3: Implement**

```go
// domain/engine/scheduler.go
package engine

import (
	"encoding/json"

	"github.com/shinya/shineflow/domain/nodetype"
	"github.com/shinya/shineflow/domain/workflow"
)

func buildTriggerTable(dsl workflow.WorkflowDSL) (triggerTable, outAdj) {
	tt := triggerTable{}
	oa := outAdj{}
	for i := range dsl.Nodes {
		n := &dsl.Nodes[i]
		spec := &triggerSpec{nodeID: n.ID}
		if n.TypeKey == nodetype.BuiltinJoin {
			spec.mode = parseJoinMode(n.Config)
		}
		tt[n.ID] = spec
	}
	for _, e := range dsl.Edges {
		if t, ok := tt[e.To]; ok {
			t.inEdges = append(t.inEdges, inEdgeRef{
				EdgeID: e.ID, SourceNode: e.From, SourcePort: e.FromPort,
			})
		}
		oa[e.From] = append(oa[e.From], e)
	}
	return tt, oa
}

func parseJoinMode(raw json.RawMessage) joinMode {
	var cfg struct{ Mode string `json:"mode"` }
	_ = json.Unmarshal(raw, &cfg)
	if cfg.Mode == nodetype.JoinModeAll {
		return joinAll
	}
	return joinAny
}

func evaluate(spec *triggerSpec, es map[string]edgeState) readiness {
	n := len(spec.inEdges)
	if n == 0 {
		return readyToRun
	}
	hasLive, hasPending, hasDead := false, false, false
	for _, e := range spec.inEdges {
		switch es[e.EdgeID] {
		case edgePending:
			hasPending = true
		case edgeLive:
			hasLive = true
		case edgeDead:
			hasDead = true
		}
	}
	if n == 1 {
		if hasPending {
			return notReady
		}
		if hasLive {
			return readyToRun
		}
		return readyToSkip
	}
	switch spec.mode {
	case joinAny:
		if hasLive {
			return readyToRun
		}
		if hasPending {
			return notReady
		}
		return readyToSkip
	case joinAll:
		if hasPending {
			return notReady
		}
		if hasDead {
			return readyToSkip
		}
		return readyToRun
	}
	return notReady
}
```

- [ ] **Step 4: Run tests + commit**

```bash
cd /Users/shinya/Downloads/ShineFlow && go test ./domain/engine/ -run "TestBuildTriggerTable|TestEvaluate"
git add domain/engine/scheduler.go domain/engine/scheduler_test.go
git commit -m "feat(engine): buildTriggerTable + evaluate (3 readiness states)

Spec 2026-04-27 §5/§7. evaluate has 3 outcomes (notReady/run/skip)
across single-input + join(any/all).

Co-Authored-By: Craft Agent <agents-noreply@craft.do>"
```

---

## Task 8: `nodeexec.go` — worker `runNode` + dispatch + markSkipped

**Files:**
- Create: `domain/engine/nodeexec.go`
- Modify: `domain/engine/scheduler.go` (add dispatch + markSkipped)

- [ ] **Step 1: Implement worker**

```go
// domain/engine/nodeexec.go
package engine

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/shinya/shineflow/domain/executor"
	"github.com/shinya/shineflow/domain/run"
	"github.com/shinya/shineflow/domain/workflow"
)

func (e *Engine) runNode(
	ctx context.Context,
	rn *run.WorkflowRun, node *workflow.Node, nr *run.NodeRun,
	snap *run.Symbols, resolver *Resolver,
	done chan<- nodeResult,
) {
	res := nodeResult{nodeID: node.ID, nodeRunID: nr.ID, attempt: nr.Attempt}
	defer func() {
		if r := recover(); r != nil {
			res.err = fmt.Errorf("executor panic: %v", r)
		}
		done <- res
	}()

	inputs, err := resolver.ResolveInputs(node, snap)
	if err != nil {
		res.err = fmt.Errorf("resolve inputs: %w", err)
		return
	}
	resolvedConfig, err := resolver.ResolveConfig(node.Config, snap)
	if err != nil {
		res.err = fmt.Errorf("resolve config: %w", err)
		return
	}
	resolvedInputsRaw, _ := json.Marshal(inputs)
	resolvedConfigRaw, _ := json.Marshal(resolvedConfig)
	res.resolvedInputs = resolvedInputsRaw
	res.resolvedConfig = resolvedConfigRaw

	nt, ok := e.ntReg.Get(node.TypeKey)
	if !ok {
		res.err = fmt.Errorf("node type not registered: %s", node.TypeKey)
		return
	}
	exe, err := e.exReg.Build(nt)
	if err != nil {
		res.err = fmt.Errorf("executor build: %w", err)
		return
	}

	nodeCtx := ctx
	if t := effectivePolicy(node.ErrorPolicy).Timeout; t > 0 {
		var cancel context.CancelFunc
		nodeCtx, cancel = context.WithTimeout(ctx, t)
		defer cancel()
	}

	out, err := exe.Execute(nodeCtx, executor.ExecInput{
		NodeType: nt,
		Config:   resolvedConfigRaw,
		Inputs:   inputs,
		Run:      buildRunInfo(rn, nr),
		Services: e.services,
	})
	if err != nil {
		res.err = err
		return
	}
	res.output = out.Outputs
	res.firedPort = out.FiredPort
	if res.firedPort == "" {
		res.firedPort = workflow.PortDefault
	}
	res.externalRefs = out.ExternalRefs
}

func buildRunInfo(rn *run.WorkflowRun, nr *run.NodeRun) executor.RunInfo {
	return executor.RunInfo{
		RunID:          rn.ID,
		NodeRunID:      nr.ID,
		Attempt:        nr.Attempt,
		DefinitionID:   rn.DefinitionID,
		VersionID:      rn.VersionID,
		TriggerKind:    rn.TriggerKind,
		TriggerRef:     rn.TriggerRef,
		TriggerPayload: rn.TriggerPayload,
	}
}
```

- [ ] **Step 2: Add dispatch + markSkipped to scheduler.go**

```go
// domain/engine/scheduler.go (append)

import "github.com/shinya/shineflow/domain/run"

// dispatch creates a NodeRun, persists it (running), and spawns a worker.
// Caller is the driver only; this method increments inflight.
func (e *Engine) dispatch(
	ctx context.Context,
	rn *run.WorkflowRun, st *runState, nodeID string,
	done chan<- nodeResult, retryCh chan<- retryEvent, persistCh chan<- persistOp,
) {
	node := st.byID[nodeID]
	attempt := 1
	// Find existing attempt count for this node by counting successful dispatches.
	// Simpler: read prior nodeRun count from runRepo? Avoid; track in runState.
	// For first dispatch attempt = 1; retries pass attempt via retryEvent (handled in caller).
	// dispatch is called from main loop both for first run and from <-retryCh dispatch path,
	// so we accept attempt as part of state lookup. Use a small map for retry attempts.
	if a, ok := st.attemptCounter[nodeID]; ok {
		attempt = a + 1
	}
	st.attemptCounter[nodeID] = attempt

	nr := &run.NodeRun{
		ID:          e.cfg.NewID(),
		RunID:       rn.ID,
		NodeID:      nodeID,
		NodeTypeKey: node.TypeKey,
		Attempt:     attempt,
		Status:      run.NodeRunStatusRunning,
	}
	startedAt := e.cfg.Clock()
	nr.StartedAt = &startedAt

	persistCh <- persistOp{kind: persistAppendNodeRun, runID: rn.ID, payload: nr}

	st.nodeStat[nodeID] = nodeRunning
	st.inflight++

	snap := st.sym.Snapshot()
	resolver := newResolver(&st.dsl, e.ntReg)

	go e.runNode(ctx, rn, node, nr, snap, resolver, done)
}

func (e *Engine) markSkipped(rn *run.WorkflowRun, st *runState, nodeID string, persistCh chan<- persistOp) {
	st.nodeStat[nodeID] = nodeDone
	endedAt := e.cfg.Clock()
	nr := &run.NodeRun{
		ID:          e.cfg.NewID(),
		RunID:       rn.ID,
		NodeID:      nodeID,
		NodeTypeKey: st.byID[nodeID].TypeKey,
		Attempt:     1,
		Status:      run.NodeRunStatusSkipped,
		EndedAt:     &endedAt,
	}
	persistCh <- persistOp{kind: persistMarkSkipped, runID: rn.ID, payload: nr}
}

// persistRetryAborted writes a synthetic NodeRun row recording the cancelled retry.
func (e *Engine) persistRetryAborted(rn *run.WorkflowRun, st *runState, rt retryEvent, persistCh chan<- persistOp) {
	endedAt := e.cfg.Clock()
	nr := &run.NodeRun{
		ID:          e.cfg.NewID(),
		RunID:       rn.ID,
		NodeID:      rt.nodeID,
		NodeTypeKey: st.byID[rt.nodeID].TypeKey,
		Attempt:     rt.attempt,
		Status:      run.NodeRunStatusCancelled,
		EndedAt:     &endedAt,
		Error: &run.NodeError{
			Code: "retry_aborted", Message: "ctx cancelled before retry timer fired",
		},
	}
	persistCh <- persistOp{kind: persistRetryAborted, runID: rn.ID, payload: nr}
}
```

Add `attemptCounter map[string]int` to `runState` in result.go and initialize it in `newRunState`.

```go
// domain/engine/result.go (modify runState)
type runState struct {
	// ... existing fields ...
	attemptCounter map[string]int
}

func newRunState(...) *runState {
	return &runState{
		// ... existing init ...
		attemptCounter: map[string]int{},
	}
}
```

- [ ] **Step 3: Verify build**

```bash
cd /Users/shinya/Downloads/ShineFlow && go build ./domain/engine/
```
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add domain/engine/
git commit -m "feat(engine): dispatch + worker runNode + markSkipped + persistRetryAborted

Spec 2026-04-27 §6.4/§10. Worker resolves inputs/config from Symbols snapshot
and pushes nodeResult. Dispatch tracks attempt count via runState.attemptCounter.

Co-Authored-By: Craft Agent <agents-noreply@craft.do>"
```

---

## Task 9: `scheduler.go` — handleResult + propagate + tryAdvance

**Files:**
- Modify: `domain/engine/scheduler.go`

- [ ] **Step 1: Implement**

Append to `domain/engine/scheduler.go`:

```go
// handleResult is the driver event handler for `case res := <-done`.
func (e *Engine) handleResult(
	ctx context.Context,
	rn *run.WorkflowRun, st *runState, res nodeResult,
	done chan<- nodeResult, retryCh chan<- retryEvent, persistCh chan<- persistOp,
) {
	node := st.byID[res.nodeID]
	ep := effectivePolicy(node.ErrorPolicy)

	if res.err != nil {
		// Strict cancelled judgment.
		if errors.Is(res.err, context.Canceled) || errors.Is(res.err, context.DeadlineExceeded) {
			persistCh <- persistOp{kind: persistNodeRunCancelled, runID: rn.ID, payload: res}
			st.nodeStat[res.nodeID] = nodeDone
			return
		}
		// Persist failed attempt first.
		persistCh <- persistOp{kind: persistNodeRunFailed, runID: rn.ID, payload: res}

		// Retry budget?
		if res.attempt < ep.MaxRetries+1 {
			delay := computeBackoff(ep, res.attempt, e.cfg.RNG)
			st.pendingRetries++
			e.scheduleRetry(ctx, res.nodeID, res.attempt+1, delay, retryCh)
			return
		}

		// OnFinalFail.
		switch ep.OnFinalFail {
		case workflow.FailStrategyFailRun:
			st.runFail = &run.RunError{
				NodeID: res.nodeID, NodeRunID: res.nodeRunID,
				Code: run.RunErrCodeNodeExecFailed, Message: res.err.Error(),
			}
			st.nodeStat[res.nodeID] = nodeDone
			return
		case workflow.FailStrategyFallback:
			res.output = ep.FallbackOutput.Output
			res.firedPort = ep.FallbackOutput.Port
			res.fallbackApplied = true
			persistCh <- persistOp{kind: persistNodeRunFallbackPatch, runID: rn.ID, payload: res}
			// fall through to propagate
		case workflow.FailStrategyFireErrorPort:
			res.output = nil
			res.firedPort = workflow.PortError
			// fall through to propagate
		}
	} else {
		persistCh <- persistOp{kind: persistNodeRunSuccess, runID: rn.ID, payload: res}
	}

	e.propagate(ctx, rn, st, node, res, done, retryCh, persistCh)
}

func (e *Engine) propagate(
	ctx context.Context,
	rn *run.WorkflowRun, st *runState,
	node *workflow.Node, res nodeResult,
	done chan<- nodeResult, retryCh chan<- retryEvent, persistCh chan<- persistOp,
) {
	if res.output == nil {
		res.output = map[string]any{}
	}
	if err := st.sym.SetNodeOutput(node.ID, res.output); err != nil {
		st.runFail = &run.RunError{
			NodeID: res.nodeID, NodeRunID: res.nodeRunID,
			Code: run.RunErrCodeOutputNotSerializable, Message: err.Error(),
		}
		st.nodeStat[res.nodeID] = nodeDone
		return
	}
	if node.TypeKey == nodetype.BuiltinSetVariable {
		for k, v := range res.output {
			_ = st.sym.SetVar(k, v)
		}
		varsRaw, _ := json.Marshal(st.sym.SnapshotVars())
		persistCh <- persistOp{kind: persistSaveVars, runID: rn.ID, payload: json.RawMessage(varsRaw)}
	}
	st.nodeStat[node.ID] = nodeDone

	if node.TypeKey == nodetype.BuiltinEnd {
		if st.endHit == nil {
			id := node.ID
			st.endHit = &id
		}
		return
	}

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
	done chan<- nodeResult, retryCh chan<- retryEvent, persistCh chan<- persistOp,
) {
	if st.nodeStat[target] != nodeUnready {
		return
	}
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
		// wait for other inbound edges
	}
}
```

Add imports: `context`, `errors`, `encoding/json` (if not already), `domain/run`, `domain/workflow`, `domain/nodetype`.

- [ ] **Step 2: Verify build**

```bash
cd /Users/shinya/Downloads/ShineFlow && go build ./domain/engine/
```
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add domain/engine/scheduler.go
git commit -m "feat(engine): handleResult + propagate + tryAdvance

Spec 2026-04-27 §8. Strict ctx-cancelled judgment; fallback fires
user-specified port; setVariable triggers SaveVars; first-end-wins
records endHit (cancellation handled in main loop).

Co-Authored-By: Craft Agent <agents-noreply@craft.do>"
```

---

## Task 10: `finalize.go` + complete `Engine.Start`

**Files:**
- Create: `domain/engine/finalize.go`
- Modify: `domain/engine/engine.go`

- [ ] **Step 1: Implement finalize helpers**

```go
// domain/engine/finalize.go
package engine

import (
	"context"
	"encoding/json"

	"github.com/shinya/shineflow/domain/run"
	"github.com/shinya/shineflow/domain/workflow"
)

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
			Message: "all branches exhausted but no End was reached",
		})
	}
}

func (e *Engine) finalizeSuccess(ctx context.Context, rn *run.WorkflowRun, st *runState) (*run.WorkflowRun, error) {
	endID := *st.endHit
	endNR, err := e.runRepo.GetLatestNodeRun(ctx, rn.ID, endID)
	if err != nil {
		return nil, err
	}
	if err := e.runRepo.SaveEndResult(ctx, rn.ID, endID, endNR.ResolvedInputs); err != nil {
		return nil, err
	}
	endedAt := e.cfg.Clock()
	if err := e.runRepo.UpdateStatus(ctx, rn.ID, run.RunStatusSuccess, run.WithRunEndedAt(endedAt)); err != nil {
		return nil, err
	}
	rn.Status = run.RunStatusSuccess
	rn.EndNodeID = &endID
	rn.Output = endNR.ResolvedInputs
	rn.EndedAt = &endedAt
	return rn, nil
}

func (e *Engine) finalizeFailed(ctx context.Context, rn *run.WorkflowRun, runErr run.RunError) (*run.WorkflowRun, error) {
	if err := e.runRepo.SaveError(ctx, rn.ID, runErr); err != nil {
		return nil, err
	}
	endedAt := e.cfg.Clock()
	if err := e.runRepo.UpdateStatus(ctx, rn.ID, run.RunStatusFailed, run.WithRunEndedAt(endedAt)); err != nil {
		return nil, err
	}
	rn.Status = run.RunStatusFailed
	rn.Error = &runErr
	rn.EndedAt = &endedAt
	return rn, nil
}

func (e *Engine) finalizeCancelled(ctx context.Context, rn *run.WorkflowRun) (*run.WorkflowRun, error) {
	endedAt := e.cfg.Clock()
	if err := e.runRepo.UpdateStatus(context.Background(), rn.ID, run.RunStatusCancelled, run.WithRunEndedAt(endedAt)); err != nil {
		return nil, err
	}
	rn.Status = run.RunStatusCancelled
	rn.EndedAt = &endedAt
	return rn, nil
}

func buildRun(in StartInput, v *workflow.WorkflowVersion, newID func() string, clock func() interface{}) *run.WorkflowRun {
	return nil // helper signature placeholder; real impl below
}
```

(`WithRunEndedAt` may need to be added to the run repository options the same way as NodeRun ones — check and add if missing.)

- [ ] **Step 2: Implement `Start`**

Replace stub in `domain/engine/engine.go`:

```go
// Start drives a Run to terminal status. See package docs for terminal semantics.
func (e *Engine) Start(ctx context.Context, in StartInput) (*run.WorkflowRun, error) {
	v, err := e.workflowRepo.GetVersion(ctx, in.VersionID)
	if err != nil {
		return nil, err
	}
	if v.State != workflow.VersionStateRelease {
		return nil, ErrVersionNotPublished
	}

	rn := &run.WorkflowRun{
		ID:             e.cfg.NewID(),
		DefinitionID:   v.DefinitionID,
		VersionID:      v.ID,
		TriggerKind:    in.TriggerKind,
		TriggerRef:     in.TriggerRef,
		TriggerPayload: in.TriggerPayload,
		Status:         run.RunStatusRunning,
		CreatedBy:      in.CreatedBy,
		CreatedAt:      e.cfg.Clock(),
	}
	startedAt := e.cfg.Clock()
	rn.StartedAt = &startedAt

	if err := e.runRepo.Create(ctx, rn); err != nil {
		return nil, err
	}
	if err := e.runRepo.UpdateStatus(ctx, rn.ID, run.RunStatusRunning, run.WithRunStartedAt(startedAt)); err != nil {
		return nil, err
	}

	// DAG precompute + Symbols.
	triggers, oa := buildTriggerTable(v.DSL)
	sym, err := run.NewSymbols(in.TriggerPayload)
	if err != nil {
		return e.finalizeFailed(ctx, rn, run.RunError{
			Code: run.RunErrCodeTriggerInvalid, Message: err.Error(),
		})
	}
	st := newRunState(v.DSL, triggers, oa, sym)

	// Run-level ctx.
	parentCtx := ctx
	if e.cfg.RunTimeout > 0 {
		var cancelTO context.CancelFunc
		parentCtx, cancelTO = context.WithTimeout(ctx, e.cfg.RunTimeout)
		defer cancelTO()
	}
	runCtx, cancel := context.WithCancel(parentCtx)
	defer cancel()

	persistCh := make(chan persistOp, e.cfg.PersistBuf)
	persistErrCh := make(chan error, 1)
	persistDone := make(chan struct{})
	go e.runPersister(runCtx, persistCh, persistErrCh, persistDone)
	defer func() {
		close(persistCh)
		<-persistDone
	}()

	done := make(chan nodeResult, 32)
	retryCh := make(chan retryEvent, len(v.DSL.Nodes))

	// Bootstrap zero-in-degree nodes.
	for nid, spec := range triggers {
		if len(spec.inEdges) == 0 {
			e.dispatch(runCtx, rn, st, nid, done, retryCh, persistCh)
		}
	}

	// Event loop.
	ctxDone := runCtx.Done()
	cancelOnce := func() {
		cancel()
		ctxDone = nil
	}
	for st.inflight > 0 || st.pendingRetries > 0 {
		select {
		case res := <-done:
			st.inflight--
			e.handleResult(runCtx, rn, st, res, done, retryCh, persistCh)
			if st.runFail != nil {
				cancelOnce()
			} else if st.endHit != nil && ctxDone != nil {
				cancelOnce()
			}
		case rt := <-retryCh:
			st.pendingRetries--
			if rt.cancelled || st.runFail != nil || ctxDone == nil {
				e.persistRetryAborted(rn, st, rt, persistCh)
			} else {
				e.dispatch(runCtx, rn, st, rt.nodeID, done, retryCh, persistCh)
			}
		case perr := <-persistErrCh:
			if st.runFail == nil {
				st.runFail = &run.RunError{Code: run.RunErrCodePersistence, Message: perr.Error()}
			}
			cancelOnce()
		case <-ctxDone:
			cancelOnce()
		}
	}

	return e.finalize(ctx, rn, st)
}
```

- [ ] **Step 3: Verify build**

```bash
cd /Users/shinya/Downloads/ShineFlow && go build ./domain/engine/
```
Expected: PASS. Add `WithRunStartedAt`, `WithRunEndedAt` opts to `domain/run/repository.go` if missing.

- [ ] **Step 4: Commit**

```bash
git add domain/engine/ domain/run/repository.go
git commit -m "feat(engine): Engine.Start full event loop

Spec 2026-04-27 §6.2/§6.3. Two-counter termination + persister + multi-End
first-end-wins + retry-runFail race defense.

Co-Authored-By: Craft Agent <agents-noreply@craft.do>"
```

---

## Task 11: `enginetest` harness — fakes + mocks + DSL builder

**Files:**
- Create: `domain/engine/enginetest/harness.go`
- Create: `domain/engine/enginetest/mock_executor.go`
- Create: `domain/engine/enginetest/mock_services.go`
- Create: `domain/engine/enginetest/builder.go`
- Create: `domain/engine/enginetest/fake_repo.go`

- [ ] **Step 1: Implement `MockExecutor`**

```go
// domain/engine/enginetest/mock_executor.go
package enginetest

import (
	"context"
	"sync/atomic"

	"github.com/shinya/shineflow/domain/executor"
	"github.com/shinya/shineflow/domain/nodetype"
)

type MockExecutor struct {
	OnExecute func(ctx context.Context, in executor.ExecInput) (executor.ExecOutput, error)
	calls     int64
}

func (m *MockExecutor) Calls() int64 { return atomic.LoadInt64(&m.calls) }

func (m *MockExecutor) Execute(ctx context.Context, in executor.ExecInput) (executor.ExecOutput, error) {
	atomic.AddInt64(&m.calls, 1)
	if m.OnExecute == nil {
		return executor.ExecOutput{}, nil
	}
	return m.OnExecute(ctx, in)
}

func MockFactory(m *MockExecutor) executor.ExecutorFactory {
	return func(_ *nodetype.NodeType) executor.NodeExecutor { return m }
}
```

- [ ] **Step 2: Implement `MockHTTPClient` / `MockLLMClient` / `MockLogger`**

```go
// domain/engine/enginetest/mock_services.go
package enginetest

import (
	"context"
	"errors"

	"github.com/shinya/shineflow/domain/executor"
)

type MockHTTPClient struct {
	OnDo func(ctx context.Context, req executor.HTTPRequest) (executor.HTTPResponse, error)
}

func (m *MockHTTPClient) Do(ctx context.Context, req executor.HTTPRequest) (executor.HTTPResponse, error) {
	if m.OnDo == nil {
		return executor.HTTPResponse{}, errors.New("MockHTTPClient.OnDo not set")
	}
	return m.OnDo(ctx, req)
}

type MockLLMClient struct {
	OnComplete func(ctx context.Context, req executor.LLMRequest) (executor.LLMResponse, error)
}

func (m *MockLLMClient) Complete(ctx context.Context, req executor.LLMRequest) (executor.LLMResponse, error) {
	if m.OnComplete == nil {
		return executor.LLMResponse{}, errors.New("MockLLMClient.OnComplete not set")
	}
	return m.OnComplete(ctx, req)
}

type MockLogger struct{}

func (MockLogger) Debugf(string, ...any) {}
func (MockLogger) Infof(string, ...any)  {}
func (MockLogger) Warnf(string, ...any)  {}
func (MockLogger) Errorf(string, ...any) {}
```

- [ ] **Step 3: Implement `DSLBuilder`**

```go
// domain/engine/enginetest/builder.go
package enginetest

import (
	"encoding/json"

	"github.com/shinya/shineflow/domain/nodetype"
	"github.com/shinya/shineflow/domain/workflow"
)

type DSLBuilder struct {
	dsl  workflow.WorkflowDSL
	next int
}

func NewDSL() *DSLBuilder { return &DSLBuilder{} }

func (b *DSLBuilder) Start(id string) *DSLBuilder {
	b.dsl.Nodes = append(b.dsl.Nodes, workflow.Node{ID: id, TypeKey: nodetype.BuiltinStart})
	return b
}

func (b *DSLBuilder) End(id string) *DSLBuilder {
	b.dsl.Nodes = append(b.dsl.Nodes, workflow.Node{ID: id, TypeKey: nodetype.BuiltinEnd})
	return b
}

func (b *DSLBuilder) Node(id, typeKey string, config string) *DSLBuilder {
	b.dsl.Nodes = append(b.dsl.Nodes, workflow.Node{ID: id, TypeKey: typeKey, Config: json.RawMessage(config)})
	return b
}

func (b *DSLBuilder) NodeWithInputs(id, typeKey string, config string, inputs map[string]workflow.ValueSource) *DSLBuilder {
	b.dsl.Nodes = append(b.dsl.Nodes, workflow.Node{
		ID: id, TypeKey: typeKey,
		Config: json.RawMessage(config), Inputs: inputs,
	})
	return b
}

func (b *DSLBuilder) Edge(from, port, to string) *DSLBuilder {
	b.next++
	b.dsl.Edges = append(b.dsl.Edges, workflow.Edge{
		ID: itoa(b.next), From: from, FromPort: port, To: to,
	})
	return b
}

func (b *DSLBuilder) Build() workflow.WorkflowDSL { return b.dsl }

func itoa(i int) string {
	const digits = "0123456789"
	if i == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = digits[i%10]
		i /= 10
	}
	return "edge-" + string(buf[pos:])
}
```

- [ ] **Step 4: Implement fake repos**

```go
// domain/engine/enginetest/fake_repo.go
package enginetest

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	"github.com/shinya/shineflow/domain/run"
	"github.com/shinya/shineflow/domain/workflow"
)

type FakeWorkflowRepo struct {
	mu       sync.Mutex
	versions map[string]*workflow.WorkflowVersion
}

func NewFakeWorkflowRepo() *FakeWorkflowRepo {
	return &FakeWorkflowRepo{versions: map[string]*workflow.WorkflowVersion{}}
}

func (f *FakeWorkflowRepo) PutVersion(v *workflow.WorkflowVersion) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.versions[v.ID] = v
}

func (f *FakeWorkflowRepo) GetVersion(_ context.Context, id string) (*workflow.WorkflowVersion, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.versions[id]
	if !ok {
		return nil, errors.New("version not found")
	}
	return v, nil
}

// (Implement remaining workflow.WorkflowRepository methods as no-ops or pragmatic stubs.)

type FakeRunRepo struct {
	mu       sync.Mutex
	runs     map[string]*run.WorkflowRun
	nodeRuns map[string][]*run.NodeRun // runID → list (in append order)
}

func NewFakeRunRepo() *FakeRunRepo {
	return &FakeRunRepo{runs: map[string]*run.WorkflowRun{}, nodeRuns: map[string][]*run.NodeRun{}}
}

func (f *FakeRunRepo) Create(_ context.Context, rn *run.WorkflowRun) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.runs[rn.ID] = rn
	return nil
}

func (f *FakeRunRepo) Get(_ context.Context, id string) (*run.WorkflowRun, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	rn, ok := f.runs[id]
	if !ok {
		return nil, errors.New("run not found")
	}
	return rn, nil
}

func (f *FakeRunRepo) UpdateStatus(_ context.Context, id string, status run.RunStatus, opts ...run.RunUpdateOpt) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	rn := f.runs[id]
	rn.Status = status
	upd := run.RunUpdate{}
	for _, o := range opts {
		o(&upd)
	}
	if upd.StartedAt != nil {
		rn.StartedAt = upd.StartedAt
	}
	if upd.EndedAt != nil {
		rn.EndedAt = upd.EndedAt
	}
	return nil
}

func (f *FakeRunRepo) AppendNodeRun(_ context.Context, runID string, nr *run.NodeRun) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nodeRuns[runID] = append(f.nodeRuns[runID], nr)
	return nil
}

func (f *FakeRunRepo) UpdateNodeRunStatus(_ context.Context, runID, nodeRunID string, status run.NodeRunStatus, opts ...run.NodeRunUpdateOpt) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, nr := range f.nodeRuns[runID] {
		if nr.ID == nodeRunID {
			nr.Status = status
			upd := run.NodeRunUpdate{}
			for _, o := range opts {
				o(&upd)
			}
			if upd.EndedAt != nil {
				nr.EndedAt = upd.EndedAt
			}
			if upd.Error != nil {
				nr.Error = upd.Error
			}
			if upd.FallbackApplied != nil {
				nr.FallbackApplied = *upd.FallbackApplied
			}
			return nil
		}
	}
	return errors.New("nodeRun not found")
}

func (f *FakeRunRepo) SaveNodeRunOutput(_ context.Context, runID, nodeRunID string, output json.RawMessage, firedPort string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, nr := range f.nodeRuns[runID] {
		if nr.ID == nodeRunID {
			nr.Output = output
			nr.ResolvedInputs = output // best effort; real impl writes ResolvedInputs separately
			nr.FiredPort = firedPort
			return nil
		}
	}
	return errors.New("nodeRun not found")
}

func (f *FakeRunRepo) GetLatestNodeRun(_ context.Context, runID, nodeID string) (*run.NodeRun, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var latest *run.NodeRun
	for _, nr := range f.nodeRuns[runID] {
		if nr.NodeID == nodeID {
			latest = nr
		}
	}
	if latest == nil {
		return nil, errors.New("nodeRun not found")
	}
	return latest, nil
}

func (f *FakeRunRepo) ListNodeRuns(_ context.Context, runID string) ([]*run.NodeRun, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*run.NodeRun, len(f.nodeRuns[runID]))
	copy(out, f.nodeRuns[runID])
	return out, nil
}

func (f *FakeRunRepo) SaveEndResult(_ context.Context, id, endNodeID string, output json.RawMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	rn := f.runs[id]
	rn.EndNodeID = &endNodeID
	rn.Output = output
	return nil
}

func (f *FakeRunRepo) SaveVars(_ context.Context, id string, vars json.RawMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.runs[id].Vars = vars
	return nil
}

func (f *FakeRunRepo) SaveError(_ context.Context, id string, e run.RunError) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.runs[id].Error = &e
	return nil
}

// Implement remaining methods of WorkflowRunRepository as needed
// (List / GetNodeRun) — minimal stubs to satisfy interface.

func (f *FakeRunRepo) List(_ context.Context, _ run.RunFilter) ([]*run.WorkflowRun, error) {
	return nil, nil
}

func (f *FakeRunRepo) GetNodeRun(_ context.Context, runID, nodeRunID string) (*run.NodeRun, error) {
	for _, nr := range f.nodeRuns[runID] {
		if nr.ID == nodeRunID {
			return nr, nil
		}
	}
	return nil, errors.New("not found")
}
```

- [ ] **Step 5: Implement `EngineHarness`**

```go
// domain/engine/enginetest/harness.go
package enginetest

import (
	"math/rand"
	"testing"
	"time"

	"github.com/shinya/shineflow/domain/engine"
	"github.com/shinya/shineflow/domain/executor"
	"github.com/shinya/shineflow/domain/executor/builtin"
	"github.com/shinya/shineflow/domain/nodetype"
)

type EngineHarness struct {
	T            *testing.T
	WorkflowRepo *FakeWorkflowRepo
	RunRepo      *FakeRunRepo
	NTReg        nodetype.NodeTypeRegistry
	ExReg        executor.ExecutorRegistry
	Engine       *engine.Engine
	HTTPMock     *MockHTTPClient
	LLMMock      *MockLLMClient
}

type Option func(*EngineHarness)

func WithMockHTTPClient(m *MockHTTPClient) Option { return func(h *EngineHarness) { h.HTTPMock = m } }
func WithMockLLMClient(m *MockLLMClient) Option   { return func(h *EngineHarness) { h.LLMMock = m } }

// New constructs a harness with builtin executors registered + fake repos +
// in-memory builtin NodeType registry.
func New(t *testing.T, opts ...Option) *EngineHarness {
	t.Helper()
	h := &EngineHarness{
		T:            t,
		WorkflowRepo: NewFakeWorkflowRepo(),
		RunRepo:      NewFakeRunRepo(),
		NTReg:        nodetype.NewBuiltinRegistry(),
		ExReg:        executor.NewRegistry(),
	}
	builtin.Register(h.ExReg)
	for _, o := range opts {
		o(h)
	}

	services := executor.ExecServices{
		Logger:     MockLogger{},
		HTTPClient: h.HTTPMock,
		LLMClient:  h.LLMMock,
	}
	cfg := engine.Config{
		Clock:     fixedClock(),
		NewID:     newSeqID(),
		AfterFunc: instantAfter,
		RNG:       rand.New(rand.NewSource(1)),
	}
	h.Engine = engine.New(h.WorkflowRepo, h.RunRepo, h.NTReg, h.ExReg, services, cfg)
	return h
}

// instantAfter fires fn immediately in a goroutine; lets retry tests skip real waits.
func instantAfter(_ time.Duration, fn func()) func() {
	go fn()
	return func() {}
}

func fixedClock() func() time.Time {
	t := time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC)
	return func() time.Time { return t }
}

func newSeqID() func() string {
	var i int
	return func() string {
		i++
		return "id-" + itoa(i)
	}
}
```

- [ ] **Step 6: Verify build + commit**

```bash
cd /Users/shinya/Downloads/ShineFlow && go build ./domain/engine/...
git add domain/engine/enginetest/
git commit -m "feat(enginetest): test harness with fake repos + mocks + DSL builder

Co-Authored-By: Craft Agent <agents-noreply@craft.do>"
```

---

## Task 12: Scheduler unit test — sunshine path

**Files:**
- Modify: `domain/engine/scheduler_test.go`

- [ ] **Step 1: Write the test**

```go
// domain/engine/scheduler_test.go (append)
package engine_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/shinya/shineflow/domain/engine"
	"github.com/shinya/shineflow/domain/engine/enginetest"
	"github.com/shinya/shineflow/domain/nodetype"
	"github.com/shinya/shineflow/domain/run"
	"github.com/shinya/shineflow/domain/workflow"
)

func TestEngineSunshineCompletes(t *testing.T) {
	h := enginetest.New(t)

	dsl := enginetest.NewDSL().
		Start("s").
		NodeWithInputs("sv", nodetype.BuiltinSetVariable, `{"name":"hello"}`,
			map[string]workflow.ValueSource{
				"value": {Kind: workflow.ValueKindLiteral, Value: "world"},
			}).
		End("e").
		Edge("s", workflow.PortDefault, "sv").
		Edge("sv", workflow.PortDefault, "e").
		Build()

	v := &workflow.WorkflowVersion{
		ID:           "v1",
		DefinitionID: "d1",
		Version:      1,
		State:        workflow.VersionStateRelease,
		DSL:          dsl,
	}
	h.WorkflowRepo.PutVersion(v)

	out, err := h.Engine.Start(context.Background(), engine.StartInput{
		VersionID:      "v1",
		TriggerKind:    run.TriggerKindManual,
		TriggerPayload: json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != run.RunStatusSuccess {
		t.Fatalf("status: %v", out.Status)
	}
	nrs, _ := h.RunRepo.ListNodeRuns(context.Background(), out.ID)
	if len(nrs) != 3 {
		t.Fatalf("expected 3 NodeRun (s/sv/e), got %d", len(nrs))
	}
	for _, nr := range nrs {
		if nr.Status != run.NodeRunStatusSuccess {
			t.Errorf("%s: %v", nr.NodeID, nr.Status)
		}
	}
}
```

- [ ] **Step 2: Run test**

```bash
cd /Users/shinya/Downloads/ShineFlow && go test ./domain/engine/ -run TestEngineSunshine -v
```
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add domain/engine/scheduler_test.go
git commit -m "test(engine): sunshine path runs to success

Co-Authored-By: Craft Agent <agents-noreply@craft.do>"
```

---

## Task 13: Scheduler unit test — multi-End first-end-wins

**Files:**
- Modify: `domain/engine/scheduler_test.go`

- [ ] **Step 1: Write the test**

```go
// domain/engine/scheduler_test.go (append)

func TestEngineMultiEndFirstWins(t *testing.T) {
	// Branch via if; FastPath returns immediately, SlowPath blocks until ctx cancel.
	slowExe := &enginetest.MockExecutor{
		OnExecute: func(ctx context.Context, _ executor.ExecInput) (executor.ExecOutput, error) {
			<-ctx.Done()
			return executor.ExecOutput{}, ctx.Err()
		},
	}
	h := enginetest.New(t)
	// Override slow node executor (replacing whatever the registry would build).
	h.ExReg.Register("test.slow", enginetest.MockFactory(slowExe))

	dsl := enginetest.NewDSL().
		Start("s").
		Node("if1", nodetype.BuiltinIf, `{"operator":"eq"}`).
		Node("fast", nodetype.BuiltinSetVariable, `{"name":"x"}`).
		Node("slow", "test.slow", `{}`).
		End("eA").
		End("eB").
		Edge("s", workflow.PortDefault, "if1").
		// Need to wire if1 inputs:
		// Inputs left/right via builder helper not yet shown — use NodeWithInputs.
		Edge("if1", workflow.PortIfTrue, "fast").
		Edge("if1", workflow.PortIfFalse, "slow").
		Edge("fast", workflow.PortDefault, "eA").
		Edge("slow", workflow.PortDefault, "eB").
		Build()

	// Adjust if1 to have inputs that yield true:
	for i := range dsl.Nodes {
		if dsl.Nodes[i].ID == "if1" {
			dsl.Nodes[i].Inputs = map[string]workflow.ValueSource{
				"left":  {Kind: workflow.ValueKindLiteral, Value: 1},
				"right": {Kind: workflow.ValueKindLiteral, Value: 1},
			}
		}
	}

	// Register a fake node type for "test.slow" so worker can build executor.
	// Use a tiny inline registry override: extend NTReg in harness, or skip
	// (workaround: let the Engine error out fetching nodeType for "test.slow"
	//  → for this test, replace the NodeTypeRegistry with one that includes
	//  a custom registration).
	// For simplicity, add a helper to enginetest.NTReg that supports `Put`:

	// (See harness extension in Task 11; if missing, augment NTReg here.)

	v := &workflow.WorkflowVersion{
		ID: "v1", DefinitionID: "d1", Version: 1, State: workflow.VersionStateRelease, DSL: dsl,
	}
	h.WorkflowRepo.PutVersion(v)

	out, err := h.Engine.Start(context.Background(), engine.StartInput{
		VersionID:      "v1",
		TriggerKind:    run.TriggerKindManual,
		TriggerPayload: json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != run.RunStatusSuccess {
		t.Fatalf("status: %v", out.Status)
	}
	if out.EndNodeID == nil || *out.EndNodeID != "eA" {
		t.Fatalf("EndNodeID: %v", out.EndNodeID)
	}
}
```

- [ ] **Step 2: If the harness's NodeTypeRegistry doesn't expose a Put method, add one:**

```go
// domain/engine/enginetest/harness.go (append)
type registryWithPut interface {
	nodetype.NodeTypeRegistry
	Put(*nodetype.NodeType)
}
```

Or simpler: skip `test.slow` and use an existing node with ErrorPolicy.Timeout to simulate slowness.

Pragmatic alternative: use `builtin.http_request` with a `MockHTTPClient.OnDo` that blocks until ctx.Done().

- [ ] **Step 3: Run + iterate**

```bash
cd /Users/shinya/Downloads/ShineFlow && go test ./domain/engine/ -run TestEngineMultiEnd -v
```
Iterate until PASS.

- [ ] **Step 4: Commit**

```bash
git add domain/engine/scheduler_test.go domain/engine/enginetest/
git commit -m "test(engine): multi-End first-end-wins (slow branch cancelled)

Spec 2026-04-27 §6.2.2 / §16.3 case 3.

Co-Authored-By: Craft Agent <agents-noreply@craft.do>"
```

---

## Task 14: Scheduler unit test — retry + fallback

**Files:**
- Modify: `domain/engine/scheduler_test.go`

- [ ] **Step 1: Write the test**

```go
// domain/engine/scheduler_test.go (append)

func TestEngineRetryThenFallback(t *testing.T) {
	llm := &enginetest.MockLLMClient{
		OnComplete: func(_ context.Context, _ executor.LLMRequest) (executor.LLMResponse, error) {
			return executor.LLMResponse{}, errors.New("simulated provider error")
		},
	}
	h := enginetest.New(t, enginetest.WithMockLLMClient(llm))

	dsl := enginetest.NewDSL().
		Start("s").
		Node("llm1", nodetype.BuiltinLLM, `{"provider":"x","model":"m1"}`).
		End("e").
		Edge("s", workflow.PortDefault, "llm1").
		Edge("llm1", workflow.PortDefault, "e").
		Build()
	for i := range dsl.Nodes {
		if dsl.Nodes[i].ID == "llm1" {
			dsl.Nodes[i].ErrorPolicy = &workflow.ErrorPolicy{
				MaxRetries:  2,
				RetryDelay:  time.Millisecond,
				OnFinalFail: workflow.FailStrategyFallback,
				FallbackOutput: workflow.FallbackOutput{
					Port:   workflow.PortDefault,
					Output: map[string]any{"text": "sorry"},
				},
			}
			dsl.Nodes[i].Inputs = map[string]workflow.ValueSource{
				"prompt": {Kind: workflow.ValueKindLiteral, Value: "hi"},
			}
		}
	}

	v := &workflow.WorkflowVersion{
		ID: "v1", DefinitionID: "d1", Version: 1, State: workflow.VersionStateRelease, DSL: dsl,
	}
	h.WorkflowRepo.PutVersion(v)

	out, err := h.Engine.Start(context.Background(), engine.StartInput{
		VersionID:      "v1",
		TriggerKind:    run.TriggerKindManual,
		TriggerPayload: json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != run.RunStatusSuccess {
		t.Fatalf("status: %v", out.Status)
	}
	nrs, _ := h.RunRepo.ListNodeRuns(context.Background(), out.ID)
	llmRuns := 0
	for _, nr := range nrs {
		if nr.NodeID == "llm1" {
			llmRuns++
			t.Logf("attempt=%d status=%v fallback=%v", nr.Attempt, nr.Status, nr.FallbackApplied)
		}
	}
	if llmRuns != 3 {
		t.Fatalf("expected 3 attempts, got %d", llmRuns)
	}
}
```

- [ ] **Step 2: Run + iterate**

```bash
cd /Users/shinya/Downloads/ShineFlow && go test ./domain/engine/ -run TestEngineRetryThenFallback -v
```

- [ ] **Step 3: Commit**

```bash
git add domain/engine/scheduler_test.go
git commit -m "test(engine): retry + fallback (3 attempts, last patches with fallback)

Spec 2026-04-27 §16.3 case 4.

Co-Authored-By: Craft Agent <agents-noreply@craft.do>"
```

---

## Task 15: e2e — sunshine on testcontainers PG

**Files:**
- Create: `domain/engine/e2e_test.go`

- [ ] **Step 1: Write the test**

```go
// domain/engine/e2e_test.go
//go:build e2e

package engine_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/shinya/shineflow/domain/engine"
	"github.com/shinya/shineflow/domain/engine/enginetest"
	"github.com/shinya/shineflow/domain/executor"
	"github.com/shinya/shineflow/domain/nodetype"
	"github.com/shinya/shineflow/domain/run"
	"github.com/shinya/shineflow/domain/workflow"
	"github.com/shinya/shineflow/infrastructure/storage/storagetest"

	wfRepo "github.com/shinya/shineflow/infrastructure/storage/workflow"
	runRepo "github.com/shinya/shineflow/infrastructure/storage/run"
)

func e2eEngine(t *testing.T, services executor.ExecServices) (*engine.Engine, *runRepo.Repository, *wfRepo.Repository) {
	t.Helper()
	ctx := storagetest.Setup(t)
	wfR := wfRepo.NewWorkflowRepository(ctx)
	rR := runRepo.NewWorkflowRunRepository(ctx)
	ntReg := nodetype.NewBuiltinRegistry()
	exReg := executor.NewRegistry()
	enginetest.RegisterBuiltins(exReg) // helper that calls builtin.Register

	eng := engine.New(wfR, rR, ntReg, exReg, services, engine.Config{
		RunTimeout: 5 * time.Second,
	})
	return eng, rR, wfR
}

func TestE2ESunshine(t *testing.T) {
	eng, runR, wfR := e2eEngine(t, executor.ExecServices{})
	dsl := enginetest.NewDSL().
		Start("s").
		End("e").
		Edge("s", workflow.PortDefault, "e").
		Build()
	v := &workflow.WorkflowVersion{
		ID: "v1", DefinitionID: "d1", Version: 1,
		State: workflow.VersionStateRelease, DSL: dsl,
	}
	if err := wfR.PutVersion(context.Background(), v); err != nil {
		t.Fatal(err)
	}
	out, err := eng.Start(context.Background(), engine.StartInput{
		VersionID: "v1", TriggerKind: run.TriggerKindManual,
		TriggerPayload: json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != run.RunStatusSuccess {
		t.Fatalf("status: %v", out.Status)
	}
	nrs, _ := runR.ListNodeRuns(context.Background(), out.ID)
	if len(nrs) != 2 {
		t.Fatalf("expected 2 NodeRuns, got %d", len(nrs))
	}
}
```

(Adjust import paths per actual `infrastructure/storage/...` package names. If `wfR.PutVersion` doesn't exist, use the actual API to seed a release version — likely `SaveVersion` + `Publish`.)

- [ ] **Step 2: Run**

```bash
cd /Users/shinya/Downloads/ShineFlow && go test -tags e2e ./domain/engine/ -run TestE2ESunshine -v
```

Iterate until PASS. (Requires Docker for testcontainers.)

- [ ] **Step 3: Commit**

```bash
git add domain/engine/e2e_test.go domain/engine/enginetest/
git commit -m "test(engine): e2e sunshine on testcontainers PG

Spec 2026-04-27 §16.3 case 1.

Co-Authored-By: Craft Agent <agents-noreply@craft.do>"
```

---

## Task 16: e2e — Branch+Join, Multi-End first-end-wins, Retry+Fallback

**Files:**
- Modify: `domain/engine/e2e_test.go`

- [ ] **Step 1: Implement Branch+Join e2e**

Use the same DSL pattern as in unit test Task 13 but on real PG. Mirror the structure: `Start → If →(true) A → Join(any) → End` / `... →(false) B → Join(any)`. Assert one branch A or B is success, the other is skipped, Run.Status=success.

- [ ] **Step 2: Implement Multi-End first-end-wins e2e**

Mirror Task 13 unit test on real PG; assert NodeRun.Status of the slow path = cancelled after persistence.

- [ ] **Step 3: Implement Retry+Fallback e2e**

Mirror Task 14 unit test on real PG; assert exactly 3 NodeRun rows for the retried node, 3rd row has fallback_applied=true and output={text:"sorry"} in the database.

- [ ] **Step 4: Run all 4 e2e tests**

```bash
cd /Users/shinya/Downloads/ShineFlow && go test -tags e2e ./domain/engine/ -run TestE2E -v
```

- [ ] **Step 5: Commit**

```bash
git add domain/engine/e2e_test.go
git commit -m "test(engine): e2e Branch+Join + Multi-End + Retry+Fallback

Spec 2026-04-27 §16.3 cases 2-4.

Co-Authored-By: Craft Agent <agents-noreply@craft.do>"
```

---

## Task 17: domain/doc.go update + final verification

**Files:**
- Modify: `domain/doc.go`

- [ ] **Step 1: Append engine subpackage doc**

Read current `domain/doc.go`; append a paragraph explaining the engine subpackage:

```go
// Package domain — engine subpackage:
//
// engine/ contains the workflow execution engine. It drives a published
// WorkflowVersion to terminal status using a single-driver event loop,
// dedicated persister goroutine, and per-attempt worker goroutines.
// See engine/doc.go for design details.
```

- [ ] **Step 2: Final full-suite check**

```bash
cd /Users/shinya/Downloads/ShineFlow
go vet ./...
go test ./domain/...                      # unit tests must pass without -tags e2e
go test -tags e2e ./domain/engine/...     # e2e (requires Docker)
```

- [ ] **Step 3: Walk spec §18 verification checklist**

Open `docs/superpowers/specs/2026-04-27-shineflow-workflow-engine-design.md` §18, check each item against the three plans (foundation, builtins, driver). Report any gap.

- [ ] **Step 4: Commit**

```bash
git add domain/doc.go
git commit -m "docs(domain): note engine subpackage in package doc

Co-Authored-By: Craft Agent <agents-noreply@craft.do>"
```

---

## Self-Review Checklist

1. **Spec coverage**:
   - §5 buildTriggerTable — Task 7
   - §6.1 Engine struct/Config — Task 1
   - §6.2 main loop (two counters, persister, multi-End cancel) — Task 10
   - §6.2.1 persister goroutine — Task 3
   - §6.2.2 multi-End first-end-wins — Tasks 10, 13, 16
   - §6.2.3 fire-and-forget — implicit (validator allows; main loop drains via inflight)
   - §6.3 finalize — Task 10
   - §6.4 persistence timing — Tasks 3 + 8 + 10
   - §7 evaluate — Task 7
   - §8 handleResult/propagate/tryAdvance — Task 9
   - §8.1 fallback row patch — Tasks 3 + 9 + 16
   - §8.2 fire_error_port output — Task 9
   - §9.2 retry counters — Tasks 6 + 9 + 10
   - §9.3 backoff jitter/cap — Task 6
   - §9.4 default policy = FailRun — Task 6
   - §9.5 FallbackOutput {Port, Output} — comes from foundation plan; engine consumes in Task 9
   - §10 worker — Task 8
   - §11 Symbols — foundation plan
   - §12 Resolver/template — Tasks 4, 5
   - §16.2 unit test matrix — Tasks 4, 5, 6, 7, 12, 13, 14
   - §16.3 e2e — Tasks 15, 16
   - §18 acceptance — Task 17

2. **Placeholder scan**: Task 13 / Task 16 contain "Adjust if … doesn't exist" guidance. These are pragmatic adaptation notes (existing infra differs from spec assumptions) rather than placeholders for unwritten code — they explicitly cite the failure mode and the workaround.

3. **Type consistency**:
   - `nodeResult.output` is `map[string]any` everywhere (driver, worker, persister)
   - `firedPort` defaults to `workflow.PortDefault` in `runNode`
   - `runState.endHit *string` checked for nil before assigning to preserve first-end-wins
   - `effectivePolicy` returns value (no nil-guarded deref)

4. **Test isolation**: instantAfter fires on a goroutine; tests must not assume tight ordering between worker completion and retry timer fire (use channels or polling, not sleeps).
