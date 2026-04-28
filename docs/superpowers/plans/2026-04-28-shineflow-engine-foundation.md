# ShineFlow Engine Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Land all domain-layer changes the engine depends on: `RefValue` simplification, `ErrorPolicy.FallbackOutput` struct, new `NodeRunStatusCancelled` + 4 RunErrCodes, `LLMClient` port, `RunInfo.TriggerPayload`, `Symbols` symbol table replacing `BuildContext`, builtin `NodeType` catalog (8 types), and 11 new + 2 reused validator rules — with full unit-test coverage. After this plan, `go build ./domain/...` passes, `go test ./domain/...` is green, and the workflow engine + builtin executors (subsequent plans) can be implemented against a stable API.

**Architecture:** Pure additions / type-shape changes within `domain/`. No new packages outside `domain/`. Symbols stores `json.RawMessage` internally for immutability + zero-copy snapshot. Validator's `outputPortsOf` helper unifies handling of switch's dynamic ports.

**Tech Stack:** Go 1.26 stdlib, `encoding/json`, no third-party test framework (existing pattern is stdlib `testing` + hand-rolled fakes).

**Spec reference:** `docs/superpowers/specs/2026-04-27-shineflow-workflow-engine-design.md` §11 (Symbols), §13.5 (catalog), §14 (validator), §15 (domain改动总清单)

**Module path:** `github.com/shinya/shineflow`

---

## File Structure

| File | Status | Responsibility |
|---|---|---|
| `domain/workflow/value.go` | Modify | Drop `RefValue.PortID` |
| `domain/workflow/error_policy.go` | Modify | `FallbackOutput` becomes `FallbackOutput{Port, Output}` struct |
| `domain/run/node_run.go` | Modify | Add `NodeRunStatusCancelled` |
| `domain/run/workflow_run.go` | Modify | Add 4 `RunErrCode*` constants |
| `domain/run/symbols.go` | Create | New `Symbols` type (replaces `BuildContext`) |
| `domain/run/symbols_test.go` | Create | Unit tests for Symbols |
| `domain/run/context.go` | Delete | Replaced by Symbols |
| `domain/run/context_test.go` | Delete | Migrated to symbols_test.go |
| `domain/executor/exec_input.go` | Modify | Add `LLMClient` port + `ExecServices.LLMClient` + `RunInfo.TriggerPayload` |
| `domain/nodetype/builtin.go` | Modify | Add `BuiltinJoin`, `JoinModeAny`, `JoinModeAll` |
| `domain/nodetype/catalog.go` | Create | `NewBuiltinRegistry()` + 8 NodeType definitions (one file each) |
| `domain/nodetype/start_type.go` | Create | `startType` |
| `domain/nodetype/end_type.go` | Create | `endType` |
| `domain/nodetype/llm_type.go` | Create | `llmType` |
| `domain/nodetype/if_type.go` | Create | `ifType` |
| `domain/nodetype/switch_type.go` | Create | `switchType` |
| `domain/nodetype/join_type.go` | Create | `joinType` |
| `domain/nodetype/set_variable_type.go` | Create | `setVariableType` |
| `domain/nodetype/http_request_type.go` | Create | `httpRequestType` |
| `domain/nodetype/catalog_test.go` | Create | Test all 8 keys present, loop absent |
| `domain/validator/validator.go` | Modify | Add `outputPortsOf` + 11 new rules; reuse `CodeCycle`/`CodeUnknownFromPort` |
| `domain/validator/validator_test.go` | Modify | Cases for every new rule |

---

## Task 1: Drop `RefValue.PortID`

**Files:**
- Modify: `domain/workflow/value.go:33`
- Modify: any internal usage (search shows currently only DSL types reference it)

- [ ] **Step 1: Search current usage**

```bash
grep -rn "PortID" /Users/shinya/Downloads/ShineFlow/domain/ /Users/shinya/Downloads/ShineFlow/infrastructure/ /Users/shinya/Downloads/ShineFlow/application/ 2>/dev/null
```
Expected: lists all sites. Note any non-RefValue PortID (there shouldn't be any in workflow value context).

- [ ] **Step 2: Edit `RefValue`**

```go
// domain/workflow/value.go
type RefValue struct {
    NodeID string `json:"node_id"`
    Path   string `json:"path,omitempty"`
    Name   string `json:"name,omitempty"`
}
```

- [ ] **Step 3: Build to surface compile errors**

```bash
cd /Users/shinya/Downloads/ShineFlow && go build ./...
```
Expected: errors at every PortID call site OR clean build if PortID was unused. Fix each by removing the PortID field from struct literals.

- [ ] **Step 4: Run all domain tests**

```bash
cd /Users/shinya/Downloads/ShineFlow && go test ./domain/...
```
Expected: PASS (PortID was wire-format only; behaviour unchanged).

- [ ] **Step 5: Commit**

```bash
cd /Users/shinya/Downloads/ShineFlow
git add domain/workflow/value.go
# Add any other modified files surfaced in step 3
git commit -m "refactor(workflow): drop RefValue.PortID

Spec 2026-04-27 §3 decision #12: node output addressing does not
include port (aligns with n8n/Dify/GitHub Actions/Airflow).
RefValue is now {NodeID, Path, Name}.

Co-Authored-By: Craft Agent <agents-noreply@craft.do>"
```

---

## Task 2: `FallbackOutput` becomes struct `{Port, Output}`

**Files:**
- Modify: `domain/workflow/error_policy.go`
- Modify: any caller constructing `ErrorPolicy{FallbackOutput: map…}`

- [ ] **Step 1: Write the failing test**

```go
// domain/workflow/error_policy_test.go (create or append)
package workflow

import (
	"encoding/json"
	"testing"
)

func TestFallbackOutputJSONRoundTrip(t *testing.T) {
	in := ErrorPolicy{
		OnFinalFail: FailStrategyFallback,
		FallbackOutput: FallbackOutput{
			Port:   "default",
			Output: map[string]any{"text": "sorry"},
		},
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out ErrorPolicy
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.FallbackOutput.Port != "default" {
		t.Fatalf("port: got %q", out.FallbackOutput.Port)
	}
	if v, _ := out.FallbackOutput.Output["text"].(string); v != "sorry" {
		t.Fatalf("output.text: got %v", out.FallbackOutput.Output["text"])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /Users/shinya/Downloads/ShineFlow && go test ./domain/workflow/ -run TestFallbackOutputJSONRoundTrip
```
Expected: FAIL with "Output undefined" or similar (the type doesn't exist yet).

- [ ] **Step 3: Add `FallbackOutput` struct + replace field type**

```go
// domain/workflow/error_policy.go

type FallbackOutput struct {
	Port   string         `json:"port,omitempty"`
	Output map[string]any `json:"output,omitempty"`
}

type ErrorPolicy struct {
	Timeout        time.Duration  `json:"timeout,omitempty"`
	MaxRetries     int            `json:"max_retries,omitempty"`
	RetryBackoff   BackoffKind    `json:"retry_backoff,omitempty"`
	RetryDelay     time.Duration  `json:"retry_delay,omitempty"`
	OnFinalFail    FailStrategy   `json:"on_final_fail,omitempty"`
	FallbackOutput FallbackOutput `json:"fallback_output,omitempty"`
}
```

- [ ] **Step 4: Build and fix call sites**

```bash
cd /Users/shinya/Downloads/ShineFlow && go build ./...
```
Expected: errors at any old `FallbackOutput: map[string]any{...}` literals. Convert each to `FallbackOutput: FallbackOutput{Port: "default", Output: map[string]any{...}}` (or appropriate port).

- [ ] **Step 5: Run tests**

```bash
cd /Users/shinya/Downloads/ShineFlow && go test ./domain/...
```
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add domain/workflow/error_policy.go domain/workflow/error_policy_test.go
# plus any modified call-sites
git commit -m "refactor(workflow): FallbackOutput as struct {Port, Output}

Spec 2026-04-27 §3 decision #16, §9.5: fallback must declare which
output port it fires (if/switch/multi-port nodes). Default port='default'
covers single-default-port nodes.

Co-Authored-By: Craft Agent <agents-noreply@craft.do>"
```

---

## Task 3: Add `NodeRunStatusCancelled`

**Files:**
- Modify: `domain/run/node_run.go:8`

- [ ] **Step 1: Edit the enum**

```go
// domain/run/node_run.go
const (
	NodeRunStatusPending   NodeRunStatus = "pending"
	NodeRunStatusRunning   NodeRunStatus = "running"
	NodeRunStatusSuccess   NodeRunStatus = "success"
	NodeRunStatusFailed    NodeRunStatus = "failed"
	NodeRunStatusSkipped   NodeRunStatus = "skipped"
	NodeRunStatusCancelled NodeRunStatus = "cancelled"   // NEW: ctx-cancelled or first-end-wins drain
)
```

- [ ] **Step 2: Verify build**

```bash
cd /Users/shinya/Downloads/ShineFlow && go build ./...
```
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add domain/run/node_run.go
git commit -m "feat(run): add NodeRunStatusCancelled

Spec 2026-04-27 §6.2.2: first-end-wins cancels other inflight branches.
Their NodeRun must record cancelled status (Run terminal stays success).

Co-Authored-By: Craft Agent <agents-noreply@craft.do>"
```

---

## Task 4: Add 4 new `RunErrCode*` constants

**Files:**
- Modify: `domain/run/workflow_run.go:60`

- [ ] **Step 1: Edit constants**

```go
// domain/run/workflow_run.go
const (
	RunErrCodeNodeExecFailed        = "node_exec_failed"
	RunErrCodeTimeout               = "timeout"
	RunErrCodeCancelled             = "cancelled"
	RunErrCodeVersionNotPublished   = "version_not_published"
	RunErrCodeNoEndReached          = "no_end_reached"            // NEW
	RunErrCodeTriggerInvalid        = "trigger_invalid"           // NEW
	RunErrCodeOutputNotSerializable = "output_not_serializable"   // NEW
	RunErrCodePersistence           = "persistence_error"         // NEW
)
```

- [ ] **Step 2: Verify build**

```bash
cd /Users/shinya/Downloads/ShineFlow && go build ./...
```
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add domain/run/workflow_run.go
git commit -m "feat(run): add 4 RunErrCodes for engine

NoEndReached / TriggerInvalid / OutputNotSerializable / Persistence
required by engine spec §6.3 finalize + §6.2 NewSymbols + §8 propagate
+ §6.2 persister error handling.

Co-Authored-By: Craft Agent <agents-noreply@craft.do>"
```

---

## Task 5: Add `LLMClient` port + types

**Files:**
- Modify: `domain/executor/exec_input.go`

- [ ] **Step 1: Write failing test**

```go
// domain/executor/exec_input_test.go (create or append)
package executor

import (
	"context"
	"testing"
)

func TestExecServicesHasLLMClient(t *testing.T) {
	var s ExecServices
	// Compile-time check: field exists and is interface type.
	s.LLMClient = nil
	_ = s
}

type fakeLLM struct{}

func (fakeLLM) Complete(_ context.Context, _ LLMRequest) (LLMResponse, error) {
	return LLMResponse{Text: "hi", Model: "m1", Usage: LLMUsage{InputTokens: 1, OutputTokens: 2}}, nil
}

func TestLLMClientInterfaceShape(t *testing.T) {
	var _ LLMClient = fakeLLM{}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /Users/shinya/Downloads/ShineFlow && go test ./domain/executor/ -run "TestExecServicesHasLLMClient|TestLLMClientInterfaceShape"
```
Expected: FAIL — "LLMClient undefined".

- [ ] **Step 3: Implement port + types**

Append to `domain/executor/exec_input.go`:

```go
// LLMClient is a transport-agnostic LLM completion port. Real adapters
// live in independent specs (OpenAI-compatible / Anthropic / etc).
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

type LLMMessage struct {
	Role    string // "system" | "user" | "assistant"
	Content string
}

type LLMResponse struct {
	Text  string
	Model string
	Usage LLMUsage
}

type LLMUsage struct {
	InputTokens  int
	OutputTokens int
}
```

And modify `ExecServices`:

```go
type ExecServices struct {
	Credentials credential.CredentialResolver
	Logger      Logger
	HTTPClient  HTTPClient
	LLMClient   LLMClient   // NEW
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd /Users/shinya/Downloads/ShineFlow && go test ./domain/executor/ -run "TestExecServicesHasLLMClient|TestLLMClientInterfaceShape"
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add domain/executor/exec_input.go domain/executor/exec_input_test.go
git commit -m "feat(executor): add LLMClient port + ExecServices.LLMClient

Spec 2026-04-27 §13.4. Adapter is out-of-scope for this spec; main
wires nil and executors return ErrPortNotConfigured at runtime.

Co-Authored-By: Craft Agent <agents-noreply@craft.do>"
```

---

## Task 6: Add `RunInfo.TriggerPayload`

**Files:**
- Modify: `domain/executor/exec_input.go:28`

- [ ] **Step 1: Edit `RunInfo`**

```go
// domain/executor/exec_input.go
type RunInfo struct {
	RunID          string
	NodeRunID      string
	Attempt        int
	DefinitionID   string
	VersionID      string
	TriggerKind    run.TriggerKind
	TriggerRef     string
	TriggerPayload json.RawMessage   // NEW: needed by builtin.start
}
```

If `encoding/json` import not yet present in this file, add it.

- [ ] **Step 2: Verify build**

```bash
cd /Users/shinya/Downloads/ShineFlow && go build ./...
```
Expected: PASS (additive field).

- [ ] **Step 3: Commit**

```bash
git add domain/executor/exec_input.go
git commit -m "feat(executor): add RunInfo.TriggerPayload

Spec 2026-04-27 §13.3 builtin.start reads RunInfo.TriggerPayload to
project trigger fields. Engine populates this when constructing ExecInput.

Co-Authored-By: Craft Agent <agents-noreply@craft.do>"
```

---

## Task 7: `Symbols` — `NewSymbols` + Trigger validation

**Files:**
- Create: `domain/run/symbols.go`
- Create: `domain/run/symbols_test.go`

- [ ] **Step 1: Write failing tests**

```go
// domain/run/symbols_test.go
package run

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNewSymbolsAcceptsObject(t *testing.T) {
	s, err := NewSymbols(json.RawMessage(`{"user_id":"u1"}`))
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if s == nil {
		t.Fatal("nil symbols")
	}
}

func TestNewSymbolsAcceptsEmpty(t *testing.T) {
	s, err := NewSymbols(nil)
	if err != nil {
		t.Fatalf("nil payload should default to {}: %v", err)
	}
	_ = s

	s, err = NewSymbols(json.RawMessage(``))
	if err != nil {
		t.Fatalf("empty payload should default to {}: %v", err)
	}
	_ = s
}

func TestNewSymbolsRejectsNonObject(t *testing.T) {
	cases := []string{`42`, `"hello"`, `true`, `null`, `[1,2,3]`}
	for _, c := range cases {
		t.Run(c, func(t *testing.T) {
			_, err := NewSymbols(json.RawMessage(c))
			if err == nil {
				t.Fatalf("expected error for %q", c)
			}
			if !strings.Contains(err.Error(), "trigger payload must be a JSON object") {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd /Users/shinya/Downloads/ShineFlow && go test ./domain/run/ -run TestNewSymbols
```
Expected: FAIL — "NewSymbols undefined".

- [ ] **Step 3: Implement `Symbols` skeleton + `NewSymbols`**

```go
// domain/run/symbols.go
package run

import (
	"encoding/json"
	"fmt"
)

// Symbols is the per-Run variable namespace consumed by templates / refs.
//
// Three root namespaces:
//   trigger.<key>         ← TriggerPayload (must be JSON object)
//   vars.<key>            ← set_variable node accumulated writes
//   nodes.<nodeID>.<key>  ← node output (Status=success or FallbackApplied=true)
//
// Internal storage is json.RawMessage:
//   - Lookup unmarshals subtree on demand → callers get a fresh value (immutable).
//   - Snapshot just clones map headers; underlying RawMessage is shared (zero-copy).
type Symbols struct {
	trigger json.RawMessage
	vars    map[string]json.RawMessage
	nodes   map[string]json.RawMessage
}

// NewSymbols validates payload is a JSON object and seeds trigger.
func NewSymbols(payload json.RawMessage) (*Symbols, error) {
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}
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

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd /Users/shinya/Downloads/ShineFlow && go test ./domain/run/ -run TestNewSymbols
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add domain/run/symbols.go domain/run/symbols_test.go
git commit -m "feat(run): add Symbols with NewSymbols + trigger validation

Spec 2026-04-27 §11.1-§11.3. Internal storage is json.RawMessage for
immutability + zero-copy snapshot. Trigger payload must be JSON object.

Co-Authored-By: Craft Agent <agents-noreply@craft.do>"
```

---

## Task 8: `Symbols.SetNodeOutput` + `SetVar` + `SnapshotVars`

**Files:**
- Modify: `domain/run/symbols.go`
- Modify: `domain/run/symbols_test.go`

- [ ] **Step 1: Write failing tests**

```go
// domain/run/symbols_test.go (append)

func TestSetNodeOutput(t *testing.T) {
	s, _ := NewSymbols(nil)
	if err := s.SetNodeOutput("n1", map[string]any{"foo": "bar", "n": 42}); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.nodes["n1"]; !ok {
		t.Fatal("n1 missing")
	}
}

func TestSetNodeOutputNilTreatedAsEmpty(t *testing.T) {
	s, _ := NewSymbols(nil)
	if err := s.SetNodeOutput("n1", nil); err != nil {
		t.Fatal(err)
	}
	raw := s.nodes["n1"]
	if string(raw) != `{}` {
		t.Fatalf("expected {}, got %s", raw)
	}
}

func TestSetNodeOutputRejectsUnserializable(t *testing.T) {
	s, _ := NewSymbols(nil)
	err := s.SetNodeOutput("n1", map[string]any{"ch": make(chan int)})
	if err == nil {
		t.Fatal("expected marshal error")
	}
}

func TestSetVarAndSnapshotVars(t *testing.T) {
	s, _ := NewSymbols(nil)
	if err := s.SetVar("x", 42); err != nil {
		t.Fatal(err)
	}
	if err := s.SetVar("y", "hello"); err != nil {
		t.Fatal(err)
	}
	snap := s.SnapshotVars()
	if len(snap) != 2 {
		t.Fatalf("want 2, got %d", len(snap))
	}
	if string(snap["x"]) != `42` || string(snap["y"]) != `"hello"` {
		t.Fatalf("snap: %v", snap)
	}
	// Mutation of snapshot must not affect Symbols.
	snap["x"] = json.RawMessage(`999`)
	if string(s.SnapshotVars()["x"]) != `42` {
		t.Fatal("snapshot leak: original mutated")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd /Users/shinya/Downloads/ShineFlow && go test ./domain/run/ -run "TestSetNodeOutput|TestSetVar"
```
Expected: FAIL.

- [ ] **Step 3: Implement methods**

Append to `domain/run/symbols.go`:

```go
// SetNodeOutput marshals output into nodes[nodeID]. nil output -> {}.
func (s *Symbols) SetNodeOutput(nodeID string, output map[string]any) error {
	if output == nil {
		s.nodes[nodeID] = json.RawMessage(`{}`)
		return nil
	}
	b, err := json.Marshal(output)
	if err != nil {
		return fmt.Errorf("marshal node output %s: %w", nodeID, err)
	}
	s.nodes[nodeID] = b
	return nil
}

// SetVar marshals value into vars[key].
func (s *Symbols) SetVar(key string, value any) error {
	b, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal var %s: %w", key, err)
	}
	s.vars[key] = b
	return nil
}

// SnapshotVars returns a copy of the vars map (callers may mutate the returned
// map without affecting Symbols). RawMessage values are shared; they are immutable
// by convention (do not mutate them).
func (s *Symbols) SnapshotVars() map[string]json.RawMessage {
	out := make(map[string]json.RawMessage, len(s.vars))
	for k, v := range s.vars {
		out[k] = v
	}
	return out
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd /Users/shinya/Downloads/ShineFlow && go test ./domain/run/ -run "TestSetNodeOutput|TestSetVar"
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add domain/run/symbols.go domain/run/symbols_test.go
git commit -m "feat(run): Symbols SetNodeOutput / SetVar / SnapshotVars

Spec 2026-04-27 §11.2. Marshal-on-write to RawMessage; SnapshotVars
returns a fresh map header so callers can mutate freely.

Co-Authored-By: Craft Agent <agents-noreply@craft.do>"
```

---

## Task 9: `Symbols.Snapshot`

**Files:**
- Modify: `domain/run/symbols.go`
- Modify: `domain/run/symbols_test.go`

- [ ] **Step 1: Write failing tests**

```go
// domain/run/symbols_test.go (append)
func TestSnapshotIsolatedFromOriginal(t *testing.T) {
	s, _ := NewSymbols(json.RawMessage(`{"a":1}`))
	_ = s.SetVar("v", 1)
	_ = s.SetNodeOutput("n1", map[string]any{"k": 1})

	snap := s.Snapshot()

	// Mutate original after snapshot.
	_ = s.SetVar("v", 999)
	_ = s.SetNodeOutput("n1", map[string]any{"k": 999})
	_ = s.SetNodeOutput("n2", map[string]any{"new": true})

	// Snapshot must reflect dispatch-time view.
	got, err := snap.Lookup("vars.v")
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := got.(float64); v != 1 {
		t.Fatalf("snap vars.v: %v", got)
	}
	got, err = snap.Lookup("nodes.n1.k")
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := got.(float64); v != 1 {
		t.Fatalf("snap nodes.n1.k: %v", got)
	}
	if _, err := snap.Lookup("nodes.n2.new"); err == nil {
		t.Fatal("snap should not see n2 added after Snapshot()")
	}
}
```

(Lookup is implemented in Task 10; this test will compile-fail there. Reorder: implement Snapshot first as a one-liner, then Lookup, then this test runs against both.)

Reorder note: write the test, expect compile-fail until Task 10 lands, OR simplify this test to not call Lookup. Simpler test:

```go
func TestSnapshotMapHeaderForked(t *testing.T) {
	s, _ := NewSymbols(nil)
	_ = s.SetVar("v", 1)
	snap := s.Snapshot()
	_ = s.SetVar("v", 999)

	if string(snap.SnapshotVars()["v"]) != `1` {
		t.Fatalf("snap vars.v: %s", snap.SnapshotVars()["v"])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /Users/shinya/Downloads/ShineFlow && go test ./domain/run/ -run TestSnapshotMapHeaderForked
```
Expected: FAIL — "Snapshot undefined".

- [ ] **Step 3: Implement `Snapshot`**

Append to `domain/run/symbols.go`:

```go
// Snapshot returns a new *Symbols that shares underlying RawMessage values
// with s but has its own map headers. Subsequent Set* on s do not affect
// the returned snapshot. RawMessage values are immutable by convention.
func (s *Symbols) Snapshot() *Symbols {
	vars := make(map[string]json.RawMessage, len(s.vars))
	for k, v := range s.vars {
		vars[k] = v
	}
	nodes := make(map[string]json.RawMessage, len(s.nodes))
	for k, v := range s.nodes {
		nodes[k] = v
	}
	return &Symbols{trigger: s.trigger, vars: vars, nodes: nodes}
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd /Users/shinya/Downloads/ShineFlow && go test ./domain/run/ -run TestSnapshotMapHeaderForked
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add domain/run/symbols.go domain/run/symbols_test.go
git commit -m "feat(run): Symbols.Snapshot zero-copy fork

Spec 2026-04-27 §11.5: dispatch-time view, map headers cloned;
RawMessage values shared (immutable by convention).

Co-Authored-By: Craft Agent <agents-noreply@craft.do>"
```

---

## Task 10: `Symbols.Lookup` + `walkPath`

**Files:**
- Modify: `domain/run/symbols.go`
- Modify: `domain/run/symbols_test.go`

- [ ] **Step 1: Write failing tests**

```go
// domain/run/symbols_test.go (append)

func TestLookupTrigger(t *testing.T) {
	s, _ := NewSymbols(json.RawMessage(`{"user_id":"u1","count":42}`))
	got, err := s.Lookup("trigger.user_id")
	if err != nil {
		t.Fatal(err)
	}
	if got != "u1" {
		t.Fatalf("got %v", got)
	}
	got, _ = s.Lookup("trigger.count")
	if v, _ := got.(float64); v != 42 {
		t.Fatalf("got %v", got)
	}
}

func TestLookupVarsAndNodes(t *testing.T) {
	s, _ := NewSymbols(nil)
	_ = s.SetVar("x", 7)
	_ = s.SetNodeOutput("n1", map[string]any{"items": []any{"a", "b"}})

	got, _ := s.Lookup("vars.x")
	if v, _ := got.(float64); v != 7 {
		t.Fatalf("vars.x: %v", got)
	}

	got, err := s.Lookup("nodes.n1.items.1")
	if err != nil {
		t.Fatal(err)
	}
	if got != "b" {
		t.Fatalf("got %v", got)
	}
}

func TestLookupErrors(t *testing.T) {
	s, _ := NewSymbols(json.RawMessage(`{"a":{"b":1}}`))
	_ = s.SetNodeOutput("n1", map[string]any{"k": 1})

	cases := []struct {
		path, contains string
	}{
		{"", "empty path"},
		{"unknown.x", "unknown root"},
		{"nodes", "nodes.<id> required"},
		{"nodes.missing.x", "node not yet produced output"},
		{"vars.missing", "var not set"},
		{"vars", "vars.<key> required"},
		{"trigger.a.b.c", "cannot navigate"},
		{"nodes.n1.k.0", "cannot navigate"},
	}
	for _, c := range cases {
		t.Run(c.path, func(t *testing.T) {
			_, err := s.Lookup(c.path)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), c.contains) {
				t.Fatalf("got %v, want contains %q", err, c.contains)
			}
		})
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd /Users/shinya/Downloads/ShineFlow && go test ./domain/run/ -run "TestLookup"
```
Expected: FAIL — "Lookup undefined".

- [ ] **Step 3: Implement `Lookup` + `walkPath`**

Append to `domain/run/symbols.go`:

```go
import "strconv"
import "strings"

// (add to existing import block above)

// Lookup resolves a dotted path. Each call unmarshals the relevant subtree
// into a fresh value tree, so callers can mutate without affecting Symbols.
func (s *Symbols) Lookup(path string) (any, error) {
	if path == "" {
		return nil, fmt.Errorf("empty path")
	}
	parts := strings.Split(path, ".")
	if parts[0] == "" {
		return nil, fmt.Errorf("empty path")
	}
	var raw json.RawMessage
	var rest []string
	switch parts[0] {
	case "trigger":
		raw, rest = s.trigger, parts[1:]
	case "vars":
		if len(parts) < 2 {
			return nil, fmt.Errorf("vars.<key> required")
		}
		v, ok := s.vars[parts[1]]
		if !ok {
			return nil, fmt.Errorf("var not set: %s", parts[1])
		}
		raw, rest = v, parts[2:]
	case "nodes":
		if len(parts) < 2 {
			return nil, fmt.Errorf("nodes.<id> required")
		}
		n, ok := s.nodes[parts[1]]
		if !ok {
			return nil, fmt.Errorf("node not yet produced output: %s", parts[1])
		}
		raw, rest = n, parts[2:]
	default:
		return nil, fmt.Errorf("unknown root: %q", parts[0])
	}

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
			if !ok {
				return nil, fmt.Errorf("key not found: %s", p)
			}
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

- [ ] **Step 4: Run tests**

```bash
cd /Users/shinya/Downloads/ShineFlow && go test ./domain/run/ -run TestLookup
```
Expected: PASS.

Also run the deferred test from Task 9 if you wrote it:

```bash
cd /Users/shinya/Downloads/ShineFlow && go test ./domain/run/ -run TestSnapshotIsolatedFromOriginal
```

- [ ] **Step 5: Commit**

```bash
git add domain/run/symbols.go domain/run/symbols_test.go
git commit -m "feat(run): Symbols.Lookup with dotted-path + array index

Spec 2026-04-27 §11.4. Each Lookup unmarshals fresh objects for caller
isolation; navigation errors include path context.

Co-Authored-By: Craft Agent <agents-noreply@craft.do>"
```

---

## Task 11: `Symbols.FromPersistedState`

**Files:**
- Modify: `domain/run/symbols.go`
- Modify: `domain/run/symbols_test.go`

- [ ] **Step 1: Write failing test**

```go
// domain/run/symbols_test.go (append)
func TestFromPersistedState(t *testing.T) {
	rn := &WorkflowRun{
		TriggerPayload: json.RawMessage(`{"u":"u1"}`),
		Vars:           json.RawMessage(`{"v":42}`),
	}
	nrSuccess := &NodeRun{NodeID: "n1", Status: NodeRunStatusSuccess, Output: json.RawMessage(`{"out":1}`)}
	nrFallback := &NodeRun{NodeID: "n2", Status: NodeRunStatusFailed, FallbackApplied: true, Output: json.RawMessage(`{"fb":2}`)}
	nrFailed := &NodeRun{NodeID: "n3", Status: NodeRunStatusFailed, Output: json.RawMessage(`{"x":9}`)}
	nrSkipped := &NodeRun{NodeID: "n4", Status: NodeRunStatusSkipped}

	s, err := FromPersistedState(rn, []*NodeRun{nrSuccess, nrFallback, nrFailed, nrSkipped})
	if err != nil {
		t.Fatal(err)
	}

	got, _ := s.Lookup("trigger.u")
	if got != "u1" {
		t.Fatalf("trigger.u: %v", got)
	}
	got, _ = s.Lookup("vars.v")
	if v, _ := got.(float64); v != 42 {
		t.Fatalf("vars.v: %v", got)
	}
	got, _ = s.Lookup("nodes.n1.out")
	if v, _ := got.(float64); v != 1 {
		t.Fatalf("nodes.n1.out: %v", got)
	}
	got, _ = s.Lookup("nodes.n2.fb")
	if v, _ := got.(float64); v != 2 {
		t.Fatalf("nodes.n2 (fallback) should be visible: %v", got)
	}
	if _, err := s.Lookup("nodes.n3.x"); err == nil {
		t.Fatal("plain failed node must not be visible")
	}
	if _, err := s.Lookup("nodes.n4.anything"); err == nil {
		t.Fatal("skipped node must not be visible")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /Users/shinya/Downloads/ShineFlow && go test ./domain/run/ -run TestFromPersistedState
```
Expected: FAIL.

- [ ] **Step 3: Implement**

Append to `domain/run/symbols.go`:

```go
// FromPersistedState reconstructs Symbols from a finished/in-progress Run row
// + its NodeRun rows. Used by audit / replay paths.
//
// nodes namespace contains node outputs where Status==Success OR FallbackApplied=true
// (the "produced output for downstream" convention, mirrored in §8.1 of the spec).
// Multiple NodeRun rows for the same NodeID (retries/attempts) — last visible row wins.
func FromPersistedState(rn *WorkflowRun, nodeRuns []*NodeRun) (*Symbols, error) {
	s, err := NewSymbols(rn.TriggerPayload)
	if err != nil {
		return nil, err
	}
	if len(rn.Vars) > 0 {
		var varsRaw map[string]json.RawMessage
		if err := json.Unmarshal(rn.Vars, &varsRaw); err != nil {
			return nil, fmt.Errorf("decode persisted vars: %w", err)
		}
		s.vars = varsRaw
	}
	for _, nr := range nodeRuns {
		visible := nr.Status == NodeRunStatusSuccess || nr.FallbackApplied
		if !visible {
			continue
		}
		out := nr.Output
		if len(out) == 0 {
			out = json.RawMessage(`{}`)
		}
		s.nodes[nr.NodeID] = out
	}
	return s, nil
}
```

- [ ] **Step 4: Run tests**

```bash
cd /Users/shinya/Downloads/ShineFlow && go test ./domain/run/
```
Expected: PASS (all symbols tests).

- [ ] **Step 5: Commit**

```bash
git add domain/run/symbols.go domain/run/symbols_test.go
git commit -m "feat(run): Symbols.FromPersistedState for audit/replay

Spec 2026-04-27 §11.2 / §8.1. Visible nodes = Success OR FallbackApplied.
Last attempt wins for repeated NodeID.

Co-Authored-By: Craft Agent <agents-noreply@craft.do>"
```

---

## Task 12: Delete `context.go` + migrate any remaining test cases

**Files:**
- Delete: `domain/run/context.go`
- Delete: `domain/run/context_test.go`
- Search: callers of `BuildContext` — must be ZERO outside of test code

- [ ] **Step 1: Find all callers**

```bash
grep -rn "BuildContext" /Users/shinya/Downloads/ShineFlow --include='*.go' 2>/dev/null
```
Expected: only `domain/run/context.go` definition + `context_test.go`. If anything else, STOP and report — engine spec already obsoletes this; an unexpected caller is a real concern.

- [ ] **Step 2: Read `context_test.go` and migrate any unique behavior coverage to `symbols_test.go`**

```bash
cat /Users/shinya/Downloads/ShineFlow/domain/run/context_test.go
```

For each test case there, ensure an equivalent exists in `symbols_test.go`. Add any missing cases (e.g., specific edge cases around JSON shape).

- [ ] **Step 3: Delete files**

```bash
cd /Users/shinya/Downloads/ShineFlow
git rm domain/run/context.go domain/run/context_test.go
```

- [ ] **Step 4: Build + test**

```bash
cd /Users/shinya/Downloads/ShineFlow && go build ./... && go test ./domain/...
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "refactor(run): remove BuildContext (replaced by Symbols)

Coverage migrated to symbols_test.go.

Co-Authored-By: Craft Agent <agents-noreply@craft.do>"
```

---

## Task 13: Add `BuiltinJoin` + `JoinModeAny` / `JoinModeAll`

**Files:**
- Modify: `domain/nodetype/builtin.go`

- [ ] **Step 1: Edit constants**

```go
// domain/nodetype/builtin.go (append within existing const block)
const (
	// existing constants...
	BuiltinJoin    = "builtin.join"

	JoinModeAny = "any"
	JoinModeAll = "all"
)
```

- [ ] **Step 2: Verify build**

```bash
cd /Users/shinya/Downloads/ShineFlow && go build ./...
```
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add domain/nodetype/builtin.go
git commit -m "feat(nodetype): add BuiltinJoin + JoinMode constants

Spec 2026-04-27 §3 decision #15. join is a new control-flow node;
mode = any (race) or all (strict AND-join).

Co-Authored-By: Craft Agent <agents-noreply@craft.do>"
```

---

## Task 14: NodeType definitions — `start_type.go`, `end_type.go`

**Files:**
- Create: `domain/nodetype/start_type.go`
- Create: `domain/nodetype/end_type.go`

- [ ] **Step 1: Implement `startType`**

```go
// domain/nodetype/start_type.go
package nodetype

import "github.com/shinya/shineflow/domain/workflow"

var startType = &NodeType{
	Key:         BuiltinStart,
	Version:     "1",
	Name:        "Start",
	Description: "Workflow entry point. Outputs are accessed via trigger.<key> (Symbols).",
	Category:    "control",
	Builtin:     true,
	Ports:       []string{workflow.PortDefault},
	// ConfigSchema / InputSchema / OutputSchema: empty (start has no config or inputs).
}
```

- [ ] **Step 2: Implement `endType`**

```go
// domain/nodetype/end_type.go
package nodetype

var endType = &NodeType{
	Key:         BuiltinEnd,
	Version:     "1",
	Name:        "End",
	Description: "Workflow exit point. Run.Output = ResolvedInputs of this node.",
	Category:    "control",
	Builtin:     true,
	Ports:       nil,   // no outputs
}
```

- [ ] **Step 3: Verify build**

```bash
cd /Users/shinya/Downloads/ShineFlow && go build ./domain/nodetype/
```
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add domain/nodetype/start_type.go domain/nodetype/end_type.go
git commit -m "feat(nodetype): start / end NodeType definitions

Co-Authored-By: Craft Agent <agents-noreply@craft.do>"
```

---

## Task 15: NodeType definitions — `llm_type.go`, `if_type.go`, `switch_type.go`, `join_type.go`

**Files:**
- Create: `domain/nodetype/llm_type.go`
- Create: `domain/nodetype/if_type.go`
- Create: `domain/nodetype/switch_type.go`
- Create: `domain/nodetype/join_type.go`

- [ ] **Step 1: Implement all four**

```go
// domain/nodetype/llm_type.go
package nodetype

import "github.com/shinya/shineflow/domain/workflow"

var llmType = &NodeType{
	Key:         BuiltinLLM,
	Version:     "1",
	Name:        "LLM",
	Description: "Call an LLM provider via ExecServices.LLMClient.",
	Category:    "ai",
	Builtin:     true,
	Ports:       []string{workflow.PortDefault, workflow.PortError},
}
```

```go
// domain/nodetype/if_type.go
package nodetype

import "github.com/shinya/shineflow/domain/workflow"

var ifType = &NodeType{
	Key:         BuiltinIf,
	Version:     "1",
	Name:        "If",
	Description: "Binary condition. Fires true / false / error.",
	Category:    "control",
	Builtin:     true,
	Ports:       []string{workflow.PortIfTrue, workflow.PortIfFalse, workflow.PortError},
}
```

```go
// domain/nodetype/switch_type.go
package nodetype

import "github.com/shinya/shineflow/domain/workflow"

// switchType.Ports lists only the static ports. The full port set
// (case names + default + error) is computed dynamically by the validator
// and engine via outputPortsOf().
var switchType = &NodeType{
	Key:         BuiltinSwitch,
	Version:     "1",
	Name:        "Switch",
	Description: "Multi-case dispatch. Port names = user-defined case.name plus default/error.",
	Category:    "control",
	Builtin:     true,
	Ports:       []string{workflow.PortDefault, workflow.PortError},
}
```

```go
// domain/nodetype/join_type.go
package nodetype

import "github.com/shinya/shineflow/domain/workflow"

var joinType = &NodeType{
	Key:         BuiltinJoin,
	Version:     "1",
	Name:        "Join",
	Description: "Multi-input join. mode=any (race) | all (strict AND-join).",
	Category:    "control",
	Builtin:     true,
	Ports:       []string{workflow.PortDefault},
}
```

- [ ] **Step 2: Verify build**

```bash
cd /Users/shinya/Downloads/ShineFlow && go build ./domain/nodetype/
```
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add domain/nodetype/llm_type.go domain/nodetype/if_type.go \
        domain/nodetype/switch_type.go domain/nodetype/join_type.go
git commit -m "feat(nodetype): llm/if/switch/join NodeType definitions

Co-Authored-By: Craft Agent <agents-noreply@craft.do>"
```

---

## Task 16: NodeType definitions — `set_variable_type.go`, `http_request_type.go`

**Files:**
- Create: `domain/nodetype/set_variable_type.go`
- Create: `domain/nodetype/http_request_type.go`

- [ ] **Step 1: Implement**

```go
// domain/nodetype/set_variable_type.go
package nodetype

import "github.com/shinya/shineflow/domain/workflow"

// Output schema is dynamic ({<cfg.Name>: value}); port is always default.
var setVariableType = &NodeType{
	Key:         BuiltinSetVariable,
	Version:     "1",
	Name:        "Set Variable",
	Description: "Write Inputs.value into vars.<cfg.Name>.",
	Category:    "data",
	Builtin:     true,
	Ports:       []string{workflow.PortDefault},
}
```

```go
// domain/nodetype/http_request_type.go
package nodetype

import "github.com/shinya/shineflow/domain/workflow"

var httpRequestType = &NodeType{
	Key:         BuiltinHTTPRequest,
	Version:     "1",
	Name:        "HTTP Request",
	Description: "Outbound HTTP via ExecServices.HTTPClient. 2xx/3xx → default; 4xx/5xx → error.",
	Category:    "io",
	Builtin:     true,
	Ports:       []string{workflow.PortDefault, workflow.PortError},
}
```

- [ ] **Step 2: Verify build**

```bash
cd /Users/shinya/Downloads/ShineFlow && go build ./domain/nodetype/
```
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add domain/nodetype/set_variable_type.go domain/nodetype/http_request_type.go
git commit -m "feat(nodetype): set_variable / http_request NodeType definitions

Co-Authored-By: Craft Agent <agents-noreply@craft.do>"
```

---

## Task 17: `NewBuiltinRegistry` + integration test

**Files:**
- Create: `domain/nodetype/catalog.go`
- Create: `domain/nodetype/catalog_test.go`

- [ ] **Step 1: Write failing test**

```go
// domain/nodetype/catalog_test.go
package nodetype

import "testing"

func TestNewBuiltinRegistryHasAllEightKeys(t *testing.T) {
	r := NewBuiltinRegistry()
	expectKeys := []string{
		BuiltinStart, BuiltinEnd, BuiltinLLM, BuiltinIf, BuiltinSwitch,
		BuiltinJoin, BuiltinSetVariable, BuiltinHTTPRequest,
	}
	for _, k := range expectKeys {
		if _, ok := r.Get(k); !ok {
			t.Errorf("missing builtin: %s", k)
		}
	}
}

func TestNewBuiltinRegistryDoesNotHaveLoop(t *testing.T) {
	r := NewBuiltinRegistry()
	if _, ok := r.Get(BuiltinLoop); ok {
		t.Error("BuiltinLoop should NOT be in catalog (out-of-scope)")
	}
}

func TestNewBuiltinRegistryDoesNotHaveCode(t *testing.T) {
	r := NewBuiltinRegistry()
	if _, ok := r.Get(BuiltinCode); ok {
		t.Error("BuiltinCode should NOT be in catalog (out-of-scope)")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd /Users/shinya/Downloads/ShineFlow && go test ./domain/nodetype/ -run NewBuiltinRegistry
```
Expected: FAIL — "NewBuiltinRegistry undefined".

- [ ] **Step 3: Implement**

First check existing `NodeTypeRegistry` implementation. If there's an in-memory impl, reuse; otherwise add a small one in catalog.go.

```go
// domain/nodetype/catalog.go
package nodetype

var builtinCatalog = []*NodeType{
	startType, endType, llmType, ifType, switchType, joinType,
	setVariableType, httpRequestType,
	// loop / code intentionally excluded — out of scope this spec.
}

// NewBuiltinRegistry returns a NodeTypeRegistry pre-populated with the 8
// builtin NodeTypes. Plugin types (HTTP / MCP) are added by separate registry
// composition in main.
func NewBuiltinRegistry() NodeTypeRegistry {
	r := newInMemoryRegistry()
	for _, nt := range builtinCatalog {
		r.put(nt)
	}
	return r
}
```

If an in-memory registry doesn't already exist in this package, add:

```go
// domain/nodetype/catalog.go (continue)

type inMemoryRegistry struct {
	byKey map[string]*NodeType
}

func newInMemoryRegistry() *inMemoryRegistry {
	return &inMemoryRegistry{byKey: map[string]*NodeType{}}
}

func (r *inMemoryRegistry) put(nt *NodeType) {
	r.byKey[nt.Key] = nt
}

func (r *inMemoryRegistry) Get(key string) (*NodeType, bool) {
	nt, ok := r.byKey[key]
	return nt, ok
}

func (r *inMemoryRegistry) List(_ NodeTypeFilter) []*NodeType {
	out := make([]*NodeType, 0, len(r.byKey))
	for _, nt := range r.byKey {
		out = append(out, nt)
	}
	return out
}

func (r *inMemoryRegistry) Invalidate(key string)        { delete(r.byKey, key) }
func (r *inMemoryRegistry) InvalidatePrefix(prefix string) {
	for k := range r.byKey {
		if len(k) >= len(prefix) && k[:len(prefix)] == prefix {
			delete(r.byKey, k)
		}
	}
}
```

If an impl already exists in `registry.go`, prefer reusing it; collapse the in-memory bits in catalog.go to just the seeding loop.

- [ ] **Step 4: Run tests**

```bash
cd /Users/shinya/Downloads/ShineFlow && go test ./domain/nodetype/
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add domain/nodetype/catalog.go domain/nodetype/catalog_test.go
git commit -m "feat(nodetype): NewBuiltinRegistry with 8 builtin NodeTypes

Spec 2026-04-27 §13.5. loop/code intentionally excluded.

Co-Authored-By: Craft Agent <agents-noreply@craft.do>"
```

---

## Task 18: Validator helper `outputPortsOf`

**Files:**
- Modify: `domain/validator/validator.go`
- Modify: `domain/validator/validator_test.go`

- [ ] **Step 1: Write failing test**

```go
// domain/validator/validator_test.go (append)
func TestOutputPortsOfStaticNode(t *testing.T) {
	reg := newFakeRegistryWithBuiltins(t) // helper that returns NewBuiltinRegistry()
	node := &workflow.Node{TypeKey: nodetype.BuiltinIf}
	got := outputPortsOf(node, reg)
	want := []string{workflow.PortIfTrue, workflow.PortIfFalse, workflow.PortError}
	if !equalStringSet(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestOutputPortsOfSwitchUsesCases(t *testing.T) {
	reg := newFakeRegistryWithBuiltins(t)
	node := &workflow.Node{
		TypeKey: nodetype.BuiltinSwitch,
		Config:  json.RawMessage(`{"cases":[{"name":"hot"},{"name":"cold"}]}`),
	}
	got := outputPortsOf(node, reg)
	want := []string{"hot", "cold", workflow.PortDefault, workflow.PortError}
	if !equalStringSet(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

// helper — add to test file
func equalStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	m := map[string]int{}
	for _, s := range a {
		m[s]++
	}
	for _, s := range b {
		m[s]--
	}
	for _, n := range m {
		if n != 0 {
			return false
		}
	}
	return true
}

// And the fake helper:
func newFakeRegistryWithBuiltins(t *testing.T) nodetype.NodeTypeRegistry {
	t.Helper()
	return nodetype.NewBuiltinRegistry()
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /Users/shinya/Downloads/ShineFlow && go test ./domain/validator/ -run TestOutputPortsOf
```
Expected: FAIL — "outputPortsOf undefined".

- [ ] **Step 3: Implement helper**

```go
// domain/validator/validator.go (append)

// switchCase mirrors builtin.switch Config shape for port-name extraction.
type switchCase struct {
	Name string `json:"name"`
}

type switchConfig struct {
	Cases []switchCase `json:"cases"`
}

// outputPortsOf returns the actual output port set of a node, accounting
// for nodes whose ports depend on Config (currently: builtin.switch).
func outputPortsOf(node *workflow.Node, ntReg nodetype.NodeTypeRegistry) []string {
	if node.TypeKey == nodetype.BuiltinSwitch {
		var cfg switchConfig
		// Best-effort: invalid Config → fall back to {default, error};
		// CodeJoinConfigInvalid analog would catch malformed switch in a
		// separate rule if added, but for port-set computation we are tolerant.
		_ = json.Unmarshal(node.Config, &cfg)
		ports := make([]string, 0, len(cfg.Cases)+2)
		for _, c := range cfg.Cases {
			ports = append(ports, c.Name)
		}
		return append(ports, workflow.PortDefault, workflow.PortError)
	}
	nt, ok := ntReg.Get(node.TypeKey)
	if !ok {
		return nil
	}
	return nt.Ports
}
```

Add `encoding/json` to imports if not present.

- [ ] **Step 4: Run tests**

```bash
cd /Users/shinya/Downloads/ShineFlow && go test ./domain/validator/ -run TestOutputPortsOf
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add domain/validator/validator.go domain/validator/validator_test.go
git commit -m "feat(validator): add outputPortsOf helper for dynamic switch ports

Spec 2026-04-27 §14.1.4. switch port set = cases[*].name ∪ {default, error};
all other builtins use NodeType.Ports as-is. set_variable port stays 'default'
(its dynamic <cfg.Name> is a field key, not a port name).

Co-Authored-By: Craft Agent <agents-noreply@craft.do>"
```

---

## Task 19: Validator — `checkSingleStart` + `checkAtLeastOneEnd` (split + add multiple-starts rule)

**Files:**
- Modify: `domain/validator/validator.go`
- Modify: `domain/validator/validator_test.go`

- [ ] **Step 1: Write failing tests**

```go
// domain/validator/validator_test.go (append)

func TestSingleStartViolation(t *testing.T) {
	dsl := workflow.WorkflowDSL{
		Nodes: []workflow.Node{
			{ID: "s1", TypeKey: nodetype.BuiltinStart},
			{ID: "s2", TypeKey: nodetype.BuiltinStart},
			{ID: "e", TypeKey: nodetype.BuiltinEnd},
		},
		Edges: []workflow.Edge{
			{ID: "e1", From: "s1", FromPort: "default", To: "e"},
		},
	}
	errs := checkSingleStart(dsl)
	if len(errs) == 0 || errs[0].Code != CodeMultipleStarts {
		t.Fatalf("expected CodeMultipleStarts, got %v", errs)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /Users/shinya/Downloads/ShineFlow && go test ./domain/validator/ -run TestSingleStartViolation
```
Expected: FAIL.

- [ ] **Step 3: Add `CodeMultipleStarts` + `checkSingleStart`**

In the existing `const` block of error codes:

```go
// domain/validator/validator.go
const (
	// existing codes...
	CodeMultipleStarts = "multiple_starts"
)
```

```go
// domain/validator/validator.go
func checkSingleStart(dsl workflow.WorkflowDSL) []ValidationError {
	count := 0
	var firstID string
	for _, n := range dsl.Nodes {
		if n.TypeKey == nodetype.BuiltinStart {
			count++
			if firstID == "" {
				firstID = n.ID
			}
		}
	}
	if count > 1 {
		return []ValidationError{{
			NodeID:  firstID,
			Code:    CodeMultipleStarts,
			Message: "DSL must contain at most one Start node",
		}}
	}
	return nil
}
```

Wire it into the main `Validate()` entry point. If existing `checkStartEnd` is monolithic, split it: keep `checkAtLeastOneEnd` (no new rule, just renamed clarity), add `checkSingleStart`. If renaming risks churn, leave `checkStartEnd` as a wrapper that calls both.

- [ ] **Step 4: Run tests**

```bash
cd /Users/shinya/Downloads/ShineFlow && go test ./domain/validator/
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add domain/validator/validator.go domain/validator/validator_test.go
git commit -m "feat(validator): CodeMultipleStarts rule

Spec 2026-04-27 §14: at most one Start node (multi-Start was never supported
runtime-wise; engine bootstrap dispatches all in-degree-0 nodes).

Co-Authored-By: Craft Agent <agents-noreply@craft.do>"
```

---

## Task 20: Validator — `CodeNoPathToEnd` (replaces unreachability)

**Files:**
- Modify: `domain/validator/validator.go`
- Modify: `domain/validator/validator_test.go`

- [ ] **Step 1: Write failing tests**

```go
// domain/validator/validator_test.go (append)

func TestNoPathToEndDetected(t *testing.T) {
	dsl := workflow.WorkflowDSL{
		Nodes: []workflow.Node{
			{ID: "s", TypeKey: nodetype.BuiltinStart},
			{ID: "n", TypeKey: nodetype.BuiltinSetVariable},
			{ID: "e", TypeKey: nodetype.BuiltinEnd}, // not reachable
		},
		Edges: []workflow.Edge{
			{ID: "e1", From: "s", FromPort: "default", To: "n"},
			// no edge to "e"
		},
	}
	errs := checkNoPathToEnd(dsl)
	if len(errs) == 0 || errs[0].Code != CodeNoPathToEnd {
		t.Fatalf("expected CodeNoPathToEnd, got %v", errs)
	}
}

func TestPathToEndPasses(t *testing.T) {
	dsl := workflow.WorkflowDSL{
		Nodes: []workflow.Node{
			{ID: "s", TypeKey: nodetype.BuiltinStart},
			{ID: "e", TypeKey: nodetype.BuiltinEnd},
		},
		Edges: []workflow.Edge{
			{ID: "e1", From: "s", FromPort: "default", To: "e"},
		},
	}
	if errs := checkNoPathToEnd(dsl); len(errs) != 0 {
		t.Fatalf("unexpected: %v", errs)
	}
}

func TestFireAndForgetSibling_Passes(t *testing.T) {
	// Spec §3 #4: fire-and-forget side branches are legal.
	dsl := workflow.WorkflowDSL{
		Nodes: []workflow.Node{
			{ID: "s", TypeKey: nodetype.BuiltinStart},
			{ID: "side", TypeKey: nodetype.BuiltinSetVariable},
			{ID: "e", TypeKey: nodetype.BuiltinEnd},
		},
		Edges: []workflow.Edge{
			{ID: "e1", From: "s", FromPort: "default", To: "side"},
			{ID: "e2", From: "s", FromPort: "default", To: "e"},
		},
	}
	if errs := checkNoPathToEnd(dsl); len(errs) != 0 {
		t.Fatalf("fire-and-forget sibling must be legal: %v", errs)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd /Users/shinya/Downloads/ShineFlow && go test ./domain/validator/ -run "TestNoPathToEnd|TestPathToEndPasses|TestFireAndForgetSibling"
```
Expected: FAIL — "checkNoPathToEnd undefined".

- [ ] **Step 3: Implement**

Add to `domain/validator/validator.go`:

```go
const CodeNoPathToEnd = "no_path_to_end"

func checkNoPathToEnd(dsl workflow.WorkflowDSL) []ValidationError {
	var starts, ends []string
	for _, n := range dsl.Nodes {
		switch n.TypeKey {
		case nodetype.BuiltinStart:
			starts = append(starts, n.ID)
		case nodetype.BuiltinEnd:
			ends = append(ends, n.ID)
		}
	}
	if len(starts) == 0 || len(ends) == 0 {
		return nil // CodeMissingStart/End handles this case
	}

	// BFS from all Start nodes following outbound edges.
	adj := map[string][]string{}
	for _, e := range dsl.Edges {
		adj[e.From] = append(adj[e.From], e.To)
	}
	seen := map[string]bool{}
	var queue []string
	for _, s := range starts {
		seen[s] = true
		queue = append(queue, s)
	}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, nxt := range adj[cur] {
			if !seen[nxt] {
				seen[nxt] = true
				queue = append(queue, nxt)
			}
		}
	}
	for _, e := range ends {
		if seen[e] {
			return nil
		}
	}
	return []ValidationError{{
		Code:    CodeNoPathToEnd,
		Message: "no directed path from any Start to any End node",
	}}
}
```

Wire into `Validate()`.

- [ ] **Step 4: Run tests**

```bash
cd /Users/shinya/Downloads/ShineFlow && go test ./domain/validator/
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add domain/validator/validator.go domain/validator/validator_test.go
git commit -m "feat(validator): CodeNoPathToEnd; fire-and-forget side branches legal

Spec 2026-04-27 §3 decision #4 + §14.1.1. Replaces the original 'all reachable
nodes must reach End' rule which forbade legitimate fire-and-forget patterns.

Co-Authored-By: Craft Agent <agents-noreply@craft.do>"
```

---

## Task 21: Validator — `CodeIsolatedNode`

**Files:**
- Modify: `domain/validator/validator.go`
- Modify: `domain/validator/validator_test.go`

- [ ] **Step 1: Write failing tests**

```go
// domain/validator/validator_test.go (append)

func TestIsolatedNodeDetected(t *testing.T) {
	dsl := workflow.WorkflowDSL{
		Nodes: []workflow.Node{
			{ID: "s", TypeKey: nodetype.BuiltinStart},
			{ID: "orphan", TypeKey: nodetype.BuiltinSetVariable}, // no inbound, not Start
			{ID: "e", TypeKey: nodetype.BuiltinEnd},
		},
		Edges: []workflow.Edge{
			{ID: "e1", From: "s", FromPort: "default", To: "e"},
		},
	}
	errs := checkIsolatedNode(dsl)
	if len(errs) == 0 || errs[0].Code != CodeIsolatedNode || errs[0].NodeID != "orphan" {
		t.Fatalf("expected CodeIsolatedNode for orphan, got %v", errs)
	}
}

func TestStartIsNotIsolated(t *testing.T) {
	dsl := workflow.WorkflowDSL{
		Nodes: []workflow.Node{
			{ID: "s", TypeKey: nodetype.BuiltinStart},
			{ID: "e", TypeKey: nodetype.BuiltinEnd},
		},
		Edges: []workflow.Edge{{ID: "e1", From: "s", FromPort: "default", To: "e"}},
	}
	if errs := checkIsolatedNode(dsl); len(errs) != 0 {
		t.Fatalf("Start with no inbound is allowed: %v", errs)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd /Users/shinya/Downloads/ShineFlow && go test ./domain/validator/ -run "TestIsolatedNode|TestStartIsNotIsolated"
```
Expected: FAIL.

- [ ] **Step 3: Implement**

```go
// domain/validator/validator.go (append)

const CodeIsolatedNode = "isolated_node"

func checkIsolatedNode(dsl workflow.WorkflowDSL) []ValidationError {
	inDeg := map[string]int{}
	for _, e := range dsl.Edges {
		inDeg[e.To]++
	}
	var errs []ValidationError
	for _, n := range dsl.Nodes {
		if inDeg[n.ID] == 0 && n.TypeKey != nodetype.BuiltinStart {
			errs = append(errs, ValidationError{
				NodeID:  n.ID,
				Code:    CodeIsolatedNode,
				Message: fmt.Sprintf("node %s has no inbound edges and is not a Start", n.ID),
			})
		}
	}
	return errs
}
```

Add `fmt` import if not already there.

- [ ] **Step 4: Run tests + commit**

```bash
cd /Users/shinya/Downloads/ShineFlow && go test ./domain/validator/
```
Expected: PASS.

```bash
git add domain/validator/validator.go domain/validator/validator_test.go
git commit -m "feat(validator): CodeIsolatedNode

Spec 2026-04-27 §14: nodes with zero inbound edges must be Start.

Co-Authored-By: Craft Agent <agents-noreply@craft.do>"
```

---

## Task 22: Validator — `CodeMultiInputRequiresJoin`

**Files:**
- Modify: `domain/validator/validator.go`
- Modify: `domain/validator/validator_test.go`

- [ ] **Step 1: Write failing tests**

```go
// domain/validator/validator_test.go (append)
func TestMultiInputRequiresJoinViolation(t *testing.T) {
	dsl := workflow.WorkflowDSL{
		Nodes: []workflow.Node{
			{ID: "s", TypeKey: nodetype.BuiltinStart},
			{ID: "a", TypeKey: nodetype.BuiltinSetVariable},
			{ID: "b", TypeKey: nodetype.BuiltinSetVariable},
			{ID: "merge", TypeKey: nodetype.BuiltinSetVariable}, // 2 inbound, NOT join → error
			{ID: "e", TypeKey: nodetype.BuiltinEnd},
		},
		Edges: []workflow.Edge{
			{ID: "e1", From: "s", FromPort: "default", To: "a"},
			{ID: "e2", From: "s", FromPort: "default", To: "b"},
			{ID: "e3", From: "a", FromPort: "default", To: "merge"},
			{ID: "e4", From: "b", FromPort: "default", To: "merge"},
			{ID: "e5", From: "merge", FromPort: "default", To: "e"},
		},
	}
	errs := checkMultiInputRequiresJoin(dsl)
	if len(errs) == 0 || errs[0].Code != CodeMultiInputRequiresJoin || errs[0].NodeID != "merge" {
		t.Fatalf("expected CodeMultiInputRequiresJoin for merge, got %v", errs)
	}
}

func TestJoinWithMultiInputPasses(t *testing.T) {
	dsl := workflow.WorkflowDSL{
		Nodes: []workflow.Node{
			{ID: "s", TypeKey: nodetype.BuiltinStart},
			{ID: "a", TypeKey: nodetype.BuiltinSetVariable},
			{ID: "b", TypeKey: nodetype.BuiltinSetVariable},
			{ID: "j", TypeKey: nodetype.BuiltinJoin, Config: json.RawMessage(`{"mode":"any"}`)},
			{ID: "e", TypeKey: nodetype.BuiltinEnd},
		},
		Edges: []workflow.Edge{
			{ID: "e1", From: "s", FromPort: "default", To: "a"},
			{ID: "e2", From: "s", FromPort: "default", To: "b"},
			{ID: "e3", From: "a", FromPort: "default", To: "j"},
			{ID: "e4", From: "b", FromPort: "default", To: "j"},
			{ID: "e5", From: "j", FromPort: "default", To: "e"},
		},
	}
	if errs := checkMultiInputRequiresJoin(dsl); len(errs) != 0 {
		t.Fatalf("unexpected: %v", errs)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd /Users/shinya/Downloads/ShineFlow && go test ./domain/validator/ -run "TestMultiInputRequiresJoin|TestJoinWithMultiInputPasses"
```
Expected: FAIL.

- [ ] **Step 3: Implement**

```go
// domain/validator/validator.go (append)

const CodeMultiInputRequiresJoin = "multi_input_requires_join"

func checkMultiInputRequiresJoin(dsl workflow.WorkflowDSL) []ValidationError {
	inDeg := map[string]int{}
	for _, e := range dsl.Edges {
		inDeg[e.To]++
	}
	var errs []ValidationError
	for _, n := range dsl.Nodes {
		if inDeg[n.ID] > 1 && n.TypeKey != nodetype.BuiltinJoin {
			errs = append(errs, ValidationError{
				NodeID:  n.ID,
				Code:    CodeMultiInputRequiresJoin,
				Message: fmt.Sprintf("node %s has %d inbound edges; multi-input nodes must be builtin.join", n.ID, inDeg[n.ID]),
			})
		}
	}
	return errs
}
```

- [ ] **Step 4: Run tests + commit**

```bash
cd /Users/shinya/Downloads/ShineFlow && go test ./domain/validator/
git add domain/validator/validator.go domain/validator/validator_test.go
git commit -m "feat(validator): CodeMultiInputRequiresJoin

Spec 2026-04-27 §14 + §3 decision #15.

Co-Authored-By: Craft Agent <agents-noreply@craft.do>"
```

---

## Task 23: Validator — `checkJoinInsufficientInputs` + `checkJoinModeInvalid` + `checkJoinConfigInvalid`

**Files:**
- Modify: `domain/validator/validator.go`
- Modify: `domain/validator/validator_test.go`

- [ ] **Step 1: Write failing tests**

```go
// domain/validator/validator_test.go (append)

func TestJoinSingleInput_Reports(t *testing.T) {
	dsl := workflow.WorkflowDSL{
		Nodes: []workflow.Node{
			{ID: "s", TypeKey: nodetype.BuiltinStart},
			{ID: "j", TypeKey: nodetype.BuiltinJoin, Config: json.RawMessage(`{"mode":"any"}`)},
			{ID: "e", TypeKey: nodetype.BuiltinEnd},
		},
		Edges: []workflow.Edge{
			{ID: "e1", From: "s", FromPort: "default", To: "j"},
			{ID: "e2", From: "j", FromPort: "default", To: "e"},
		},
	}
	errs := checkJoin(dsl)
	if !containsCode(errs, CodeJoinInsufficientInputs) {
		t.Fatalf("expected CodeJoinInsufficientInputs, got %v", errs)
	}
}

func TestJoinModeInvalid_Reports(t *testing.T) {
	dsl := workflow.WorkflowDSL{
		Nodes: []workflow.Node{
			{ID: "s", TypeKey: nodetype.BuiltinStart},
			{ID: "a", TypeKey: nodetype.BuiltinSetVariable},
			{ID: "b", TypeKey: nodetype.BuiltinSetVariable},
			{ID: "j", TypeKey: nodetype.BuiltinJoin, Config: json.RawMessage(`{"mode":"first"}`)},
			{ID: "e", TypeKey: nodetype.BuiltinEnd},
		},
		Edges: []workflow.Edge{
			{ID: "e1", From: "s", FromPort: "default", To: "a"},
			{ID: "e2", From: "s", FromPort: "default", To: "b"},
			{ID: "e3", From: "a", FromPort: "default", To: "j"},
			{ID: "e4", From: "b", FromPort: "default", To: "j"},
			{ID: "e5", From: "j", FromPort: "default", To: "e"},
		},
	}
	errs := checkJoin(dsl)
	if !containsCode(errs, CodeJoinModeInvalid) {
		t.Fatalf("expected CodeJoinModeInvalid, got %v", errs)
	}
}

func TestJoinConfigInvalid_Reports(t *testing.T) {
	dsl := workflow.WorkflowDSL{
		Nodes: []workflow.Node{
			{ID: "s", TypeKey: nodetype.BuiltinStart},
			{ID: "a", TypeKey: nodetype.BuiltinSetVariable},
			{ID: "b", TypeKey: nodetype.BuiltinSetVariable},
			{ID: "j", TypeKey: nodetype.BuiltinJoin, Config: json.RawMessage(`{"mode": 123}`)},
			{ID: "e", TypeKey: nodetype.BuiltinEnd},
		},
		Edges: []workflow.Edge{
			{ID: "e1", From: "s", FromPort: "default", To: "a"},
			{ID: "e2", From: "s", FromPort: "default", To: "b"},
			{ID: "e3", From: "a", FromPort: "default", To: "j"},
			{ID: "e4", From: "b", FromPort: "default", To: "j"},
			{ID: "e5", From: "j", FromPort: "default", To: "e"},
		},
	}
	errs := checkJoin(dsl)
	if !containsCode(errs, CodeJoinConfigInvalid) {
		t.Fatalf("expected CodeJoinConfigInvalid, got %v", errs)
	}
}

// helper
func containsCode(errs []ValidationError, code string) bool {
	for _, e := range errs {
		if e.Code == code {
			return true
		}
	}
	return false
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd /Users/shinya/Downloads/ShineFlow && go test ./domain/validator/ -run TestJoin
```
Expected: FAIL.

- [ ] **Step 3: Implement**

```go
// domain/validator/validator.go (append)

const (
	CodeJoinInsufficientInputs = "join_insufficient_inputs"
	CodeJoinModeInvalid        = "join_mode_invalid"
	CodeJoinConfigInvalid      = "join_config_invalid"
)

type joinConfig struct {
	Mode string `json:"mode"`
}

func checkJoin(dsl workflow.WorkflowDSL) []ValidationError {
	inDeg := map[string]int{}
	for _, e := range dsl.Edges {
		inDeg[e.To]++
	}
	var errs []ValidationError
	for _, n := range dsl.Nodes {
		if n.TypeKey != nodetype.BuiltinJoin {
			continue
		}
		if inDeg[n.ID] < 2 {
			errs = append(errs, ValidationError{
				NodeID: n.ID, Code: CodeJoinInsufficientInputs,
				Message: fmt.Sprintf("builtin.join requires >=2 inputs, got %d", inDeg[n.ID]),
			})
		}
		var cfg joinConfig
		if err := json.Unmarshal(n.Config, &cfg); err != nil {
			errs = append(errs, ValidationError{
				NodeID: n.ID, Code: CodeJoinConfigInvalid,
				Message: fmt.Sprintf("builtin.join Config invalid: %v", err),
			})
			continue
		}
		if cfg.Mode != nodetype.JoinModeAny && cfg.Mode != nodetype.JoinModeAll {
			errs = append(errs, ValidationError{
				NodeID: n.ID, Code: CodeJoinModeInvalid,
				Message: fmt.Sprintf("builtin.join mode must be %q or %q, got %q",
					nodetype.JoinModeAny, nodetype.JoinModeAll, cfg.Mode),
			})
		}
	}
	return errs
}
```

- [ ] **Step 4: Run tests + commit**

```bash
cd /Users/shinya/Downloads/ShineFlow && go test ./domain/validator/
git add domain/validator/validator.go domain/validator/validator_test.go
git commit -m "feat(validator): join inputs / mode / config rules

Spec 2026-04-27 §14: join requires >=2 inputs, mode in {any,all},
Config must parse.

Co-Authored-By: Craft Agent <agents-noreply@craft.do>"
```

---

## Task 24: Validator — `checkSwitchCaseNames` (duplicate + reserved)

**Files:**
- Modify: `domain/validator/validator.go`
- Modify: `domain/validator/validator_test.go`

- [ ] **Step 1: Write failing tests**

```go
// domain/validator/validator_test.go (append)

func TestSwitchCaseDuplicate(t *testing.T) {
	dsl := workflow.WorkflowDSL{
		Nodes: []workflow.Node{
			{ID: "s", TypeKey: nodetype.BuiltinStart},
			{ID: "sw", TypeKey: nodetype.BuiltinSwitch, Config: json.RawMessage(`{"cases":[{"name":"hot"},{"name":"hot"}]}`)},
			{ID: "e", TypeKey: nodetype.BuiltinEnd},
		},
		Edges: []workflow.Edge{
			{ID: "e1", From: "s", FromPort: "default", To: "sw"},
			{ID: "e2", From: "sw", FromPort: "hot", To: "e"},
		},
	}
	errs := checkSwitchCaseNames(dsl)
	if !containsCode(errs, CodeSwitchCaseNameDuplicate) {
		t.Fatalf("expected CodeSwitchCaseNameDuplicate, got %v", errs)
	}
}

func TestSwitchCaseReservedName(t *testing.T) {
	cases := []string{"default", "error", "1foo", "with space", ""}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := fmt.Sprintf(`{"cases":[{"name":%q}]}`, name)
			dsl := workflow.WorkflowDSL{
				Nodes: []workflow.Node{
					{ID: "s", TypeKey: nodetype.BuiltinStart},
					{ID: "sw", TypeKey: nodetype.BuiltinSwitch, Config: json.RawMessage(cfg)},
					{ID: "e", TypeKey: nodetype.BuiltinEnd},
				},
				Edges: []workflow.Edge{
					{ID: "e1", From: "s", FromPort: "default", To: "sw"},
					{ID: "e2", From: "sw", FromPort: "default", To: "e"},
				},
			}
			errs := checkSwitchCaseNames(dsl)
			if !containsCode(errs, CodeSwitchCaseNameReserved) {
				t.Fatalf("expected CodeSwitchCaseNameReserved for %q, got %v", name, errs)
			}
		})
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd /Users/shinya/Downloads/ShineFlow && go test ./domain/validator/ -run TestSwitchCase
```
Expected: FAIL.

- [ ] **Step 3: Implement**

```go
// domain/validator/validator.go (append)
import "regexp"

const (
	CodeSwitchCaseNameDuplicate = "switch_case_name_duplicate"
	CodeSwitchCaseNameReserved  = "switch_case_name_reserved"
)

var portNamePattern = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

func checkSwitchCaseNames(dsl workflow.WorkflowDSL) []ValidationError {
	var errs []ValidationError
	for _, n := range dsl.Nodes {
		if n.TypeKey != nodetype.BuiltinSwitch {
			continue
		}
		var cfg switchConfig
		if err := json.Unmarshal(n.Config, &cfg); err != nil {
			continue // separate config-invalid rule could catch this; tolerate here
		}
		seen := map[string]bool{}
		for _, c := range cfg.Cases {
			if c.Name == workflow.PortDefault || c.Name == workflow.PortError ||
				!portNamePattern.MatchString(c.Name) {
				errs = append(errs, ValidationError{
					NodeID:  n.ID,
					Code:    CodeSwitchCaseNameReserved,
					Message: fmt.Sprintf("switch case name %q is invalid (must match [A-Za-z_][A-Za-z0-9_]* and not be 'default'/'error')", c.Name),
				})
				continue
			}
			if seen[c.Name] {
				errs = append(errs, ValidationError{
					NodeID:  n.ID,
					Code:    CodeSwitchCaseNameDuplicate,
					Message: fmt.Sprintf("switch case name %q duplicated", c.Name),
				})
			}
			seen[c.Name] = true
		}
	}
	return errs
}
```

- [ ] **Step 4: Run tests + commit**

```bash
cd /Users/shinya/Downloads/ShineFlow && go test ./domain/validator/
git add domain/validator/validator.go domain/validator/validator_test.go
git commit -m "feat(validator): switch case name duplicate / reserved rules

Spec 2026-04-27 §13.3 + §14. Port names must match [A-Za-z_][A-Za-z0-9_]*
and not collide with reserved 'default'/'error'.

Co-Authored-By: Craft Agent <agents-noreply@craft.do>"
```

---

## Task 25: Validator — `checkFallbackPort`

**Files:**
- Modify: `domain/validator/validator.go`
- Modify: `domain/validator/validator_test.go`

- [ ] **Step 1: Write failing tests**

```go
// domain/validator/validator_test.go (append)

func TestFallbackPortMissing(t *testing.T) {
	dsl := workflow.WorkflowDSL{
		Nodes: []workflow.Node{
			{ID: "s", TypeKey: nodetype.BuiltinStart},
			{ID: "n", TypeKey: nodetype.BuiltinHTTPRequest, ErrorPolicy: &workflow.ErrorPolicy{
				OnFinalFail: workflow.FailStrategyFallback,
				FallbackOutput: workflow.FallbackOutput{
					Port:   "", // missing
					Output: map[string]any{"k": 1},
				},
			}},
			{ID: "e", TypeKey: nodetype.BuiltinEnd},
		},
		Edges: []workflow.Edge{
			{ID: "e1", From: "s", FromPort: "default", To: "n"},
			{ID: "e2", From: "n", FromPort: "default", To: "e"},
		},
	}
	reg := nodetype.NewBuiltinRegistry()
	errs := checkFallbackPort(dsl, reg)
	if !containsCode(errs, CodeFallbackPortInvalid) {
		t.Fatalf("expected CodeFallbackPortInvalid (missing), got %v", errs)
	}
}

func TestFallbackPortNotInOutputs(t *testing.T) {
	dsl := workflow.WorkflowDSL{
		Nodes: []workflow.Node{
			{ID: "s", TypeKey: nodetype.BuiltinStart},
			{ID: "n", TypeKey: nodetype.BuiltinHTTPRequest, ErrorPolicy: &workflow.ErrorPolicy{
				OnFinalFail: workflow.FailStrategyFallback,
				FallbackOutput: workflow.FallbackOutput{
					Port:   "phantom", // not in http_request ports
					Output: map[string]any{"k": 1},
				},
			}},
			{ID: "e", TypeKey: nodetype.BuiltinEnd},
		},
		Edges: []workflow.Edge{
			{ID: "e1", From: "s", FromPort: "default", To: "n"},
			{ID: "e2", From: "n", FromPort: "default", To: "e"},
		},
	}
	reg := nodetype.NewBuiltinRegistry()
	errs := checkFallbackPort(dsl, reg)
	if !containsCode(errs, CodeFallbackPortInvalid) {
		t.Fatalf("expected CodeFallbackPortInvalid (phantom), got %v", errs)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd /Users/shinya/Downloads/ShineFlow && go test ./domain/validator/ -run TestFallbackPort
```
Expected: FAIL.

- [ ] **Step 3: Implement**

```go
// domain/validator/validator.go (append)
const CodeFallbackPortInvalid = "fallback_port_invalid"

func checkFallbackPort(dsl workflow.WorkflowDSL, ntReg nodetype.NodeTypeRegistry) []ValidationError {
	var errs []ValidationError
	for i := range dsl.Nodes {
		n := &dsl.Nodes[i]
		if n.ErrorPolicy == nil || n.ErrorPolicy.OnFinalFail != workflow.FailStrategyFallback {
			continue
		}
		port := n.ErrorPolicy.FallbackOutput.Port
		if port == "" {
			errs = append(errs, ValidationError{
				NodeID: n.ID, Code: CodeFallbackPortInvalid,
				Message: "FallbackOutput.Port required when OnFinalFail=Fallback",
			})
			continue
		}
		ports := outputPortsOf(n, ntReg)
		if !containsString(ports, port) {
			errs = append(errs, ValidationError{
				NodeID: n.ID, Code: CodeFallbackPortInvalid,
				Message: fmt.Sprintf("FallbackOutput.Port %q not in node ports %v", port, ports),
			})
		}
	}
	return errs
}

func containsString(ss []string, target string) bool {
	for _, s := range ss {
		if s == target {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Run tests + commit**

```bash
cd /Users/shinya/Downloads/ShineFlow && go test ./domain/validator/
git add domain/validator/validator.go domain/validator/validator_test.go
git commit -m "feat(validator): CodeFallbackPortInvalid

Spec 2026-04-27 §9.5 / §14: fallback must declare valid port from node's
output port set (uses outputPortsOf for switch dynamic ports).

Co-Authored-By: Craft Agent <agents-noreply@craft.do>"
```

---

## Task 26: Validator — `checkFireErrorPortRequiresErrorPort`

**Files:**
- Modify: `domain/validator/validator.go`
- Modify: `domain/validator/validator_test.go`

- [ ] **Step 1: Write failing tests**

```go
// domain/validator/validator_test.go (append)

func TestFireErrorPortRequiresErrorPort(t *testing.T) {
	// set_variable has only "default" port → FireErrorPort policy is invalid.
	dsl := workflow.WorkflowDSL{
		Nodes: []workflow.Node{
			{ID: "s", TypeKey: nodetype.BuiltinStart},
			{ID: "sv", TypeKey: nodetype.BuiltinSetVariable, ErrorPolicy: &workflow.ErrorPolicy{
				OnFinalFail: workflow.FailStrategyFireErrorPort,
			}},
			{ID: "e", TypeKey: nodetype.BuiltinEnd},
		},
		Edges: []workflow.Edge{
			{ID: "e1", From: "s", FromPort: "default", To: "sv"},
			{ID: "e2", From: "sv", FromPort: "default", To: "e"},
		},
	}
	reg := nodetype.NewBuiltinRegistry()
	errs := checkFireErrorPortRequiresErrorPort(dsl, reg)
	if !containsCode(errs, CodeFireErrorPortRequiresErrorPort) {
		t.Fatalf("expected CodeFireErrorPortRequiresErrorPort, got %v", errs)
	}
}

func TestFireErrorPortOnHTTPNodePasses(t *testing.T) {
	dsl := workflow.WorkflowDSL{
		Nodes: []workflow.Node{
			{ID: "s", TypeKey: nodetype.BuiltinStart},
			{ID: "h", TypeKey: nodetype.BuiltinHTTPRequest, ErrorPolicy: &workflow.ErrorPolicy{
				OnFinalFail: workflow.FailStrategyFireErrorPort,
			}},
			{ID: "e", TypeKey: nodetype.BuiltinEnd},
		},
		Edges: []workflow.Edge{
			{ID: "e1", From: "s", FromPort: "default", To: "h"},
			{ID: "e2", From: "h", FromPort: "default", To: "e"},
		},
	}
	reg := nodetype.NewBuiltinRegistry()
	if errs := checkFireErrorPortRequiresErrorPort(dsl, reg); len(errs) != 0 {
		t.Fatalf("http_request has error port: %v", errs)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd /Users/shinya/Downloads/ShineFlow && go test ./domain/validator/ -run TestFireErrorPort
```
Expected: FAIL.

- [ ] **Step 3: Implement**

```go
// domain/validator/validator.go (append)
const CodeFireErrorPortRequiresErrorPort = "fire_error_port_requires_error_port"

func checkFireErrorPortRequiresErrorPort(dsl workflow.WorkflowDSL, ntReg nodetype.NodeTypeRegistry) []ValidationError {
	var errs []ValidationError
	for i := range dsl.Nodes {
		n := &dsl.Nodes[i]
		if n.ErrorPolicy == nil || n.ErrorPolicy.OnFinalFail != workflow.FailStrategyFireErrorPort {
			continue
		}
		ports := outputPortsOf(n, ntReg)
		if !containsString(ports, workflow.PortError) {
			errs = append(errs, ValidationError{
				NodeID: n.ID, Code: CodeFireErrorPortRequiresErrorPort,
				Message: fmt.Sprintf("OnFinalFail=FireErrorPort but node has no 'error' output port (ports=%v)", ports),
			})
		}
	}
	return errs
}
```

- [ ] **Step 4: Run tests + commit**

```bash
cd /Users/shinya/Downloads/ShineFlow && go test ./domain/validator/
git add domain/validator/validator.go domain/validator/validator_test.go
git commit -m "feat(validator): CodeFireErrorPortRequiresErrorPort

Spec 2026-04-27 §9.4 + §14: prevents silent error swallowing on nodes
without an error output port.

Co-Authored-By: Craft Agent <agents-noreply@craft.do>"
```

---

## Task 27: Wire all rules into `Validate()` + final integration test

**Files:**
- Modify: `domain/validator/validator.go` (the `Validate` entry function)
- Modify: `domain/validator/validator_test.go`

- [ ] **Step 1: Inspect current `Validate` entry point**

```bash
grep -n "func Validate" /Users/shinya/Downloads/ShineFlow/domain/validator/validator.go
```

- [ ] **Step 2: Add all new rules to the dispatch list**

Modify `Validate(dsl workflow.WorkflowDSL, ntReg nodetype.NodeTypeRegistry) []ValidationError` (or whatever the exact signature is) to include calls to:
- `checkSingleStart`
- `checkNoPathToEnd`
- `checkIsolatedNode`
- `checkMultiInputRequiresJoin`
- `checkJoin`
- `checkSwitchCaseNames`
- `checkFallbackPort`
- `checkFireErrorPortRequiresErrorPort`

Keep all existing checks intact (`checkStartEnd` / `checkUniqueIDs` / `checkEdgeTargets` / `checkEdgeFromPorts` / `checkRefValues` / `checkRequiredInputs` / `checkFallbackOnly` / `checkAcyclic`).

Do NOT keep the old "all reachable from Start must reach End" rule — spec §3 #4 explicitly removes that.

- [ ] **Step 3: Write integration test**

```go
// domain/validator/validator_test.go (append)

func TestValidate_HappyPathWithAllNewRulesPasses(t *testing.T) {
	reg := nodetype.NewBuiltinRegistry()
	dsl := workflow.WorkflowDSL{
		Nodes: []workflow.Node{
			{ID: "s", TypeKey: nodetype.BuiltinStart},
			{ID: "h", TypeKey: nodetype.BuiltinHTTPRequest},
			{ID: "i", TypeKey: nodetype.BuiltinIf, Inputs: map[string]workflow.ValueSource{
				"left":  {Kind: workflow.ValueKindLiteral, Value: 1},
				"right": {Kind: workflow.ValueKindLiteral, Value: 1},
			}, Config: json.RawMessage(`{"operator":"eq"}`)},
			{ID: "j", TypeKey: nodetype.BuiltinJoin, Config: json.RawMessage(`{"mode":"any"}`)},
			{ID: "e", TypeKey: nodetype.BuiltinEnd},
		},
		Edges: []workflow.Edge{
			{ID: "e1", From: "s", FromPort: "default", To: "h"},
			{ID: "e2", From: "h", FromPort: "default", To: "i"},
			{ID: "e3", From: "i", FromPort: "true", To: "j"},
			{ID: "e4", From: "i", FromPort: "false", To: "j"},
			{ID: "e5", From: "j", FromPort: "default", To: "e"},
		},
	}
	if errs := Validate(dsl, reg); len(errs) != 0 {
		t.Fatalf("happy path should validate: %v", errs)
	}
}

func TestValidate_RejectsMultipleViolations(t *testing.T) {
	reg := nodetype.NewBuiltinRegistry()
	dsl := workflow.WorkflowDSL{
		Nodes: []workflow.Node{
			{ID: "s1", TypeKey: nodetype.BuiltinStart},
			{ID: "s2", TypeKey: nodetype.BuiltinStart}, // multiple starts
			{ID: "orphan", TypeKey: nodetype.BuiltinSetVariable}, // isolated
			{ID: "e", TypeKey: nodetype.BuiltinEnd},
		},
		Edges: []workflow.Edge{
			{ID: "e1", From: "s1", FromPort: "default", To: "e"},
		},
	}
	errs := Validate(dsl, reg)
	if !containsCode(errs, CodeMultipleStarts) || !containsCode(errs, CodeIsolatedNode) {
		t.Fatalf("expected both multiple_starts and isolated_node, got %v", errs)
	}
}
```

- [ ] **Step 4: Run all validator tests**

```bash
cd /Users/shinya/Downloads/ShineFlow && go test ./domain/validator/ -v
```
Expected: PASS.

- [ ] **Step 5: Run full domain suite**

```bash
cd /Users/shinya/Downloads/ShineFlow && go build ./... && go test ./domain/...
```
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add domain/validator/validator.go domain/validator/validator_test.go
git commit -m "feat(validator): wire all new rules into Validate entry

Spec 2026-04-27 §14 fully landed:
  - 11 new rules registered
  - existing 'all-reachable-must-reach-End' rule removed (replaced by NoPathToEnd)
  - happy-path integration test passes

Co-Authored-By: Craft Agent <agents-noreply@craft.do>"
```

---

## Final Verification

- [ ] **Run the full test suite + vet**

```bash
cd /Users/shinya/Downloads/ShineFlow
go vet ./...
go test ./domain/...
```
Both must be clean.

- [ ] **Confirm the engine spec's §15 "domain 改动总清单" is fully satisfied**

Manually walk through §15 items 1-11 and check each off against this plan's tasks. Report any gap.

- [ ] **Tag a milestone (optional)**

```bash
git tag engine-foundation-v1
```

---

## Self-Review Checklist (run before handoff)

1. **Spec coverage**: §3 decisions #4/#5/#11/#12/#16, §11 (Symbols), §13.5 (catalog), §14 (validator), §15 (domain change list). All covered? Yes.
2. **Placeholder scan**: no "TBD" / "TODO" / "similar to Task X" — checked.
3. **Type consistency**: `FallbackOutput` struct uses `Port` (not `port`), `Output` (not `output`); `JoinModeAny="any"` matches use in tests; `Symbols` method names align between symbols.go and tests.
4. **Existing-code reuse**: did NOT introduce duplicate codes for `Cycle`/`UnknownFromPort` — those are pre-existing in validator.go and remain intact (engine spec §14 reuses the existing names by accepting them; we only ADD the new ones listed here).
