# ShineFlow Builtin Executors Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the 8 builtin executors (`start`, `end`, `llm`, `if`, `switch`, `join`, `set_variable`, `http_request`) plus the `Register` wiring, each unit-tested with mocks. After this plan, `go test ./domain/executor/builtin/...` is green and any harness can build a working `ExecutorRegistry` from `builtin.Register(reg)`.

**Architecture:** One file per executor under `domain/executor/builtin/`. Each executor implements `executor.NodeExecutor` (`Execute(ctx, ExecInput) (ExecOutput, error)`). All ports / nodes use the constants defined in `domain/workflow` and `domain/nodetype`. Executors are pure: they consume `ExecInput.Inputs` (already-resolved `map[string]any`) and produce `ExecOutput`. They never touch repositories, never JSON-marshal output (driver does that on the way to Symbols / NodeRun).

**Tech Stack:** Go 1.26 stdlib + existing `domain/executor` types. `coerceFloat64` is a shared helper in `domain/executor/builtin/numeric.go`.

**Spec reference:** `docs/superpowers/specs/2026-04-27-shineflow-workflow-engine-design.md` §13 (full executor catalog), §13.3 (key behaviors), §13.4 (LLMClient port).

**Depends on:** `2026-04-28-shineflow-engine-foundation.md` (must be merged first — needs `Symbols`, `BuiltinJoin`, `LLMClient`, `RunInfo.TriggerPayload`, `FallbackOutput` struct).

---

## File Structure

| File | Status | Responsibility |
|---|---|---|
| `domain/executor/builtin/doc.go` | Create | package doc |
| `domain/executor/builtin/wire.go` | Create | `Register(reg)` registers all 8 executor factories |
| `domain/executor/builtin/errors.go` | Create | shared `ErrPortNotConfigured` sentinel |
| `domain/executor/builtin/numeric.go` | Create | `coerceFloat64` helper used by `if` / `switch` |
| `domain/executor/builtin/start.go` + `_test.go` | Create | `builtin.start` |
| `domain/executor/builtin/end.go` + `_test.go` | Create | `builtin.end` |
| `domain/executor/builtin/set_variable.go` + `_test.go` | Create | `builtin.set_variable` |
| `domain/executor/builtin/join.go` + `_test.go` | Create | `builtin.join` |
| `domain/executor/builtin/if.go` + `_test.go` | Create | `builtin.if` |
| `domain/executor/builtin/switch.go` + `_test.go` | Create | `builtin.switch` |
| `domain/executor/builtin/http_request.go` + `_test.go` | Create | `builtin.http_request` |
| `domain/executor/builtin/llm.go` + `_test.go` | Create | `builtin.llm` |
| `domain/executor/builtin/wire_test.go` | Create | Verify `Register` populates 8 keys |

---

## Task 1: Package skeleton + `ErrPortNotConfigured` + `Register` stub

**Files:**
- Create: `domain/executor/builtin/doc.go`
- Create: `domain/executor/builtin/errors.go`
- Create: `domain/executor/builtin/wire.go`

- [ ] **Step 1: Create package files**

```go
// domain/executor/builtin/doc.go
// Package builtin contains the 8 built-in NodeExecutor implementations:
// start / end / llm / if / switch / join / set_variable / http_request.
//
// Spec: docs/superpowers/specs/2026-04-27-shineflow-workflow-engine-design.md §13
package builtin
```

```go
// domain/executor/builtin/errors.go
package builtin

import "errors"

// ErrPortNotConfigured is returned by an executor when its required
// ExecServices port (HTTPClient / LLMClient / Credentials) is nil at runtime.
// Engine surfaces this as a normal node error; ErrorPolicy decides retry/fallback.
var ErrPortNotConfigured = errors.New("required executor service port not configured")
```

```go
// domain/executor/builtin/wire.go
package builtin

import (
	"github.com/shinya/shineflow/domain/executor"
	"github.com/shinya/shineflow/domain/nodetype"
)

// Register installs factories for all 8 built-in executors into reg.
// Call once during engine assembly (main.go).
func Register(reg executor.ExecutorRegistry) {
	reg.Register(nodetype.BuiltinStart,        startFactory)
	reg.Register(nodetype.BuiltinEnd,          endFactory)
	reg.Register(nodetype.BuiltinLLM,          llmFactory)
	reg.Register(nodetype.BuiltinIf,           ifFactory)
	reg.Register(nodetype.BuiltinSwitch,       switchFactory)
	reg.Register(nodetype.BuiltinJoin,         joinFactory)
	reg.Register(nodetype.BuiltinSetVariable,  setVariableFactory)
	reg.Register(nodetype.BuiltinHTTPRequest,  httpRequestFactory)
}
```

(Factories are stubs at this point; they'll be defined task-by-task. Build will fail until each is added — that's fine for incremental TDD.)

- [ ] **Step 2: Verify package compiles independently of factories**

Temporarily comment out the factory references in `wire.go` so the package builds:

```go
// domain/executor/builtin/wire.go (temporary)
func Register(reg executor.ExecutorRegistry) {
	// factories registered as each is implemented
	_ = reg
}
```

```bash
cd /Users/shinya/Downloads/ShineFlow && go build ./domain/executor/builtin/
```
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add domain/executor/builtin/
git commit -m "feat(builtin): scaffold builtin executor package

Package doc + ErrPortNotConfigured + empty Register skeleton.
Factories filled in by subsequent tasks.

Co-Authored-By: Craft Agent <agents-noreply@craft.do>"
```

---

## Task 2: `coerceFloat64` numeric helper

**Files:**
- Create: `domain/executor/builtin/numeric.go`
- Create: `domain/executor/builtin/numeric_test.go`

- [ ] **Step 1: Write failing tests**

```go
// domain/executor/builtin/numeric_test.go
package builtin

import (
	"encoding/json"
	"testing"
)

func TestCoerceFloat64(t *testing.T) {
	cases := []struct {
		name    string
		in      any
		want    float64
		wantOK  bool
	}{
		{"float64", float64(3.14), 3.14, true},
		{"float32", float32(2.5), 2.5, true},
		{"int", int(42), 42, true},
		{"int64", int64(-7), -7, true},
		{"json.Number", json.Number("12.5"), 12.5, true},
		{"numeric string", "100", 100, true},
		{"non-numeric string", "abc", 0, false},
		{"bool", true, 0, false},
		{"nil", nil, 0, false},
		{"map", map[string]any{}, 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := coerceFloat64(c.in)
			if ok != c.wantOK {
				t.Fatalf("ok: got %v want %v", ok, c.wantOK)
			}
			if ok && got != c.want {
				t.Fatalf("got %v want %v", got, c.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /Users/shinya/Downloads/ShineFlow && go test ./domain/executor/builtin/ -run TestCoerceFloat64
```
Expected: FAIL — "coerceFloat64 undefined".

- [ ] **Step 3: Implement**

```go
// domain/executor/builtin/numeric.go
package builtin

import (
	"encoding/json"
	"strconv"
)

// coerceFloat64 attempts to convert v to float64. Used by if/switch operators
// to bridge the JSON-decode-as-float64 reality with literal int / string inputs.
func coerceFloat64(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int32:
		return float64(x), true
	case int64:
		return float64(x), true
	case json.Number:
		if f, err := x.Float64(); err == nil {
			return f, true
		}
	case string:
		if f, err := strconv.ParseFloat(x, 64); err == nil {
			return f, true
		}
	}
	return 0, false
}
```

- [ ] **Step 4: Run tests + commit**

```bash
cd /Users/shinya/Downloads/ShineFlow && go test ./domain/executor/builtin/ -run TestCoerceFloat64
git add domain/executor/builtin/numeric.go domain/executor/builtin/numeric_test.go
git commit -m "feat(builtin): coerceFloat64 helper for if/switch numeric ops

Spec 2026-04-27 §13.3.1.

Co-Authored-By: Craft Agent <agents-noreply@craft.do>"
```

---

## Task 3: `builtin.start` executor

**Files:**
- Create: `domain/executor/builtin/start.go`
- Create: `domain/executor/builtin/start_test.go`

- [ ] **Step 1: Write failing test**

```go
// domain/executor/builtin/start_test.go
package builtin

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/shinya/shineflow/domain/executor"
	"github.com/shinya/shineflow/domain/workflow"
)

func TestStartReturnsEmptyOutputs(t *testing.T) {
	exe := startFactory(nil)
	out, err := exe.Execute(context.Background(), executor.ExecInput{
		Run: executor.RunInfo{TriggerPayload: json.RawMessage(`{"user_id":"u1"}`)},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.FiredPort != workflow.PortDefault {
		t.Fatalf("FiredPort: got %q want %q", out.FiredPort, workflow.PortDefault)
	}
	if len(out.Outputs) != 0 {
		t.Fatalf("Outputs should be empty (trigger accessed via Symbols.trigger), got %v", out.Outputs)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /Users/shinya/Downloads/ShineFlow && go test ./domain/executor/builtin/ -run TestStart
```
Expected: FAIL — "startFactory undefined".

- [ ] **Step 3: Implement**

```go
// domain/executor/builtin/start.go
package builtin

import (
	"context"

	"github.com/shinya/shineflow/domain/executor"
	"github.com/shinya/shineflow/domain/nodetype"
	"github.com/shinya/shineflow/domain/workflow"
)

type startExecutor struct{}

// startFactory matches executor.ExecutorFactory signature.
func startFactory(_ *nodetype.NodeType) executor.NodeExecutor { return startExecutor{} }

// Execute returns an empty output map. Trigger payload is accessed via
// Symbols.trigger.<key>, not via nodes.<startID>.<key> (spec §13.3).
func (startExecutor) Execute(_ context.Context, _ executor.ExecInput) (executor.ExecOutput, error) {
	return executor.ExecOutput{
		Outputs:   map[string]any{},
		FiredPort: workflow.PortDefault,
	}, nil
}
```

- [ ] **Step 4: Re-enable in `wire.go`**

```go
// domain/executor/builtin/wire.go
func Register(reg executor.ExecutorRegistry) {
	reg.Register(nodetype.BuiltinStart, startFactory)
}
```

- [ ] **Step 5: Run tests + commit**

```bash
cd /Users/shinya/Downloads/ShineFlow && go test ./domain/executor/builtin/
git add domain/executor/builtin/start.go domain/executor/builtin/start_test.go domain/executor/builtin/wire.go
git commit -m "feat(builtin): start executor

Spec 2026-04-27 §13.2/§13.3. start.Outputs = {}; trigger via Symbols.

Co-Authored-By: Craft Agent <agents-noreply@craft.do>"
```

---

## Task 4: `builtin.end` executor

**Files:**
- Create: `domain/executor/builtin/end.go`
- Create: `domain/executor/builtin/end_test.go`

- [ ] **Step 1: Write failing test**

```go
// domain/executor/builtin/end_test.go
package builtin

import (
	"context"
	"testing"

	"github.com/shinya/shineflow/domain/executor"
)

func TestEndReturnsNilOutputsAndDefaultPort(t *testing.T) {
	exe := endFactory(nil)
	out, err := exe.Execute(context.Background(), executor.ExecInput{
		Inputs: map[string]any{"text": "hello"},
	})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if out.Outputs != nil {
		t.Fatalf("Outputs should be nil (Run.Output = ResolvedInputs read by driver), got %v", out.Outputs)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /Users/shinya/Downloads/ShineFlow && go test ./domain/executor/builtin/ -run TestEnd
```
Expected: FAIL.

- [ ] **Step 3: Implement**

```go
// domain/executor/builtin/end.go
package builtin

import (
	"context"

	"github.com/shinya/shineflow/domain/executor"
	"github.com/shinya/shineflow/domain/nodetype"
)

type endExecutor struct{}

func endFactory(_ *nodetype.NodeType) executor.NodeExecutor { return endExecutor{} }

// Execute is a no-op. Driver detects TypeKey == BuiltinEnd, marks endHit
// and reads Run.Output from NodeRun.ResolvedInputs at finalize.
func (endExecutor) Execute(_ context.Context, _ executor.ExecInput) (executor.ExecOutput, error) {
	return executor.ExecOutput{}, nil
}
```

Append to `wire.go`:
```go
reg.Register(nodetype.BuiltinEnd, endFactory)
```

- [ ] **Step 4: Run tests + commit**

```bash
cd /Users/shinya/Downloads/ShineFlow && go test ./domain/executor/builtin/
git add domain/executor/builtin/end.go domain/executor/builtin/end_test.go domain/executor/builtin/wire.go
git commit -m "feat(builtin): end executor (no-op)

Co-Authored-By: Craft Agent <agents-noreply@craft.do>"
```

---

## Task 5: `builtin.set_variable` executor

**Files:**
- Create: `domain/executor/builtin/set_variable.go`
- Create: `domain/executor/builtin/set_variable_test.go`

- [ ] **Step 1: Write failing tests**

```go
// domain/executor/builtin/set_variable_test.go
package builtin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/shinya/shineflow/domain/executor"
	"github.com/shinya/shineflow/domain/workflow"
)

func TestSetVariableHappy(t *testing.T) {
	exe := setVariableFactory(nil)
	out, err := exe.Execute(context.Background(), executor.ExecInput{
		Config: json.RawMessage(`{"name":"my_var"}`),
		Inputs: map[string]any{"value": 42},
	})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if out.FiredPort != workflow.PortDefault {
		t.Fatalf("port: %s", out.FiredPort)
	}
	if v, _ := out.Outputs["my_var"].(int); v != 42 {
		t.Fatalf("Outputs.my_var: %v", out.Outputs["my_var"])
	}
}

func TestSetVariableMissingName(t *testing.T) {
	exe := setVariableFactory(nil)
	_, err := exe.Execute(context.Background(), executor.ExecInput{
		Config: json.RawMessage(`{}`),
		Inputs: map[string]any{"value": 1},
	})
	if err == nil || !strings.Contains(err.Error(), "name") {
		t.Fatalf("expected missing name error, got %v", err)
	}
}

func TestSetVariableMissingValue(t *testing.T) {
	exe := setVariableFactory(nil)
	_, err := exe.Execute(context.Background(), executor.ExecInput{
		Config: json.RawMessage(`{"name":"x"}`),
		Inputs: map[string]any{},
	})
	if err == nil || !strings.Contains(err.Error(), "value") {
		t.Fatalf("expected missing value error, got %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd /Users/shinya/Downloads/ShineFlow && go test ./domain/executor/builtin/ -run TestSetVariable
```
Expected: FAIL.

- [ ] **Step 3: Implement**

```go
// domain/executor/builtin/set_variable.go
package builtin

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/shinya/shineflow/domain/executor"
	"github.com/shinya/shineflow/domain/nodetype"
	"github.com/shinya/shineflow/domain/workflow"
)

type setVariableConfig struct {
	Name string `json:"name"`
}

type setVariableExecutor struct{}

func setVariableFactory(_ *nodetype.NodeType) executor.NodeExecutor { return setVariableExecutor{} }

func (setVariableExecutor) Execute(_ context.Context, in executor.ExecInput) (executor.ExecOutput, error) {
	var cfg setVariableConfig
	if err := json.Unmarshal(in.Config, &cfg); err != nil {
		return executor.ExecOutput{}, fmt.Errorf("set_variable: parse config: %w", err)
	}
	if cfg.Name == "" {
		return executor.ExecOutput{}, fmt.Errorf("set_variable: config.name is required")
	}
	value, ok := in.Inputs["value"]
	if !ok {
		return executor.ExecOutput{}, fmt.Errorf("set_variable: input.value is required")
	}
	return executor.ExecOutput{
		Outputs:   map[string]any{cfg.Name: value},
		FiredPort: workflow.PortDefault,
	}, nil
}
```

Append to `wire.go`:
```go
reg.Register(nodetype.BuiltinSetVariable, setVariableFactory)
```

- [ ] **Step 4: Run tests + commit**

```bash
cd /Users/shinya/Downloads/ShineFlow && go test ./domain/executor/builtin/
git add domain/executor/builtin/set_variable.go domain/executor/builtin/set_variable_test.go domain/executor/builtin/wire.go
git commit -m "feat(builtin): set_variable executor

Spec 2026-04-27 §13.3. Outputs = {<cfg.Name>: in.Inputs.value}.
Driver propagate sees TypeKey==BuiltinSetVariable and additionally
calls Symbols.SetVar + persister SaveVars.

Co-Authored-By: Craft Agent <agents-noreply@craft.do>"
```

---

## Task 6: `builtin.join` executor

**Files:**
- Create: `domain/executor/builtin/join.go`
- Create: `domain/executor/builtin/join_test.go`

- [ ] **Step 1: Write failing test**

```go
// domain/executor/builtin/join_test.go
package builtin

import (
	"context"
	"testing"

	"github.com/shinya/shineflow/domain/executor"
	"github.com/shinya/shineflow/domain/workflow"
)

func TestJoinNoOp(t *testing.T) {
	exe := joinFactory(nil)
	out, err := exe.Execute(context.Background(), executor.ExecInput{})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if out.FiredPort != workflow.PortDefault {
		t.Fatalf("port: %s", out.FiredPort)
	}
	if len(out.Outputs) != 0 {
		t.Fatalf("Outputs should be empty: %v", out.Outputs)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /Users/shinya/Downloads/ShineFlow && go test ./domain/executor/builtin/ -run TestJoin
```
Expected: FAIL.

- [ ] **Step 3: Implement**

```go
// domain/executor/builtin/join.go
package builtin

import (
	"context"

	"github.com/shinya/shineflow/domain/executor"
	"github.com/shinya/shineflow/domain/nodetype"
	"github.com/shinya/shineflow/domain/workflow"
)

// joinExecutor is a no-op control-flow node. The any/all semantics live
// in the driver's evaluate(), informed by buildTriggerTable's Config parsing.
type joinExecutor struct{}

func joinFactory(_ *nodetype.NodeType) executor.NodeExecutor { return joinExecutor{} }

func (joinExecutor) Execute(_ context.Context, _ executor.ExecInput) (executor.ExecOutput, error) {
	return executor.ExecOutput{
		Outputs:   map[string]any{},
		FiredPort: workflow.PortDefault,
	}, nil
}
```

Append to `wire.go`:
```go
reg.Register(nodetype.BuiltinJoin, joinFactory)
```

- [ ] **Step 4: Run tests + commit**

```bash
cd /Users/shinya/Downloads/ShineFlow && go test ./domain/executor/builtin/
git add domain/executor/builtin/join.go domain/executor/builtin/join_test.go domain/executor/builtin/wire.go
git commit -m "feat(builtin): join executor (no-op)

Spec 2026-04-27 §13.3. Mode (any/all) is enforced in engine evaluate.

Co-Authored-By: Craft Agent <agents-noreply@craft.do>"
```

---

## Task 7: `builtin.if` executor with all operators

**Files:**
- Create: `domain/executor/builtin/if.go`
- Create: `domain/executor/builtin/if_test.go`

- [ ] **Step 1: Write failing tests**

```go
// domain/executor/builtin/if_test.go
package builtin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/shinya/shineflow/domain/executor"
	"github.com/shinya/shineflow/domain/workflow"
)

func ifExec() executor.NodeExecutor { return ifFactory(nil) }

func TestIfEqNumericCoerce(t *testing.T) {
	out, err := ifExec().Execute(context.Background(), executor.ExecInput{
		Config: json.RawMessage(`{"operator":"eq"}`),
		Inputs: map[string]any{"left": float64(42), "right": int(42)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.FiredPort != workflow.PortIfTrue {
		t.Fatalf("port: %s", out.FiredPort)
	}
	if v, _ := out.Outputs["result"].(bool); !v {
		t.Fatalf("result: %v", out.Outputs["result"])
	}
}

func TestIfNeStrings(t *testing.T) {
	out, _ := ifExec().Execute(context.Background(), executor.ExecInput{
		Config: json.RawMessage(`{"operator":"ne"}`),
		Inputs: map[string]any{"left": "a", "right": "b"},
	})
	if out.FiredPort != workflow.PortIfTrue {
		t.Fatalf("port: %s", out.FiredPort)
	}
}

func TestIfGtNumeric(t *testing.T) {
	out, _ := ifExec().Execute(context.Background(), executor.ExecInput{
		Config: json.RawMessage(`{"operator":"gt"}`),
		Inputs: map[string]any{"left": 10, "right": 3},
	})
	if out.FiredPort != workflow.PortIfTrue {
		t.Fatalf("port: %s", out.FiredPort)
	}
}

func TestIfContainsString(t *testing.T) {
	out, _ := ifExec().Execute(context.Background(), executor.ExecInput{
		Config: json.RawMessage(`{"operator":"contains"}`),
		Inputs: map[string]any{"left": "hello world", "right": "world"},
	})
	if out.FiredPort != workflow.PortIfTrue {
		t.Fatalf("port: %s", out.FiredPort)
	}
}

func TestIfStartsWithString(t *testing.T) {
	out, _ := ifExec().Execute(context.Background(), executor.ExecInput{
		Config: json.RawMessage(`{"operator":"starts_with"}`),
		Inputs: map[string]any{"left": "hello world", "right": "hello"},
	})
	if out.FiredPort != workflow.PortIfTrue {
		t.Fatalf("port: %s", out.FiredPort)
	}
}

func TestIfIsEmpty(t *testing.T) {
	cases := []struct {
		v      any
		expect string
	}{
		{"", workflow.PortIfTrue},
		{"x", workflow.PortIfFalse},
		{nil, workflow.PortIfTrue},
		{[]any{}, workflow.PortIfTrue},
		{[]any{1}, workflow.PortIfFalse},
		{map[string]any{}, workflow.PortIfTrue},
		{map[string]any{"k": 1}, workflow.PortIfFalse},
	}
	for _, c := range cases {
		t.Run("", func(t *testing.T) {
			out, _ := ifExec().Execute(context.Background(), executor.ExecInput{
				Config: json.RawMessage(`{"operator":"is_empty"}`),
				Inputs: map[string]any{"left": c.v},
			})
			if out.FiredPort != c.expect {
				t.Fatalf("v=%v: got port %q want %q", c.v, out.FiredPort, c.expect)
			}
		})
	}
}

func TestIfFalsePath(t *testing.T) {
	out, _ := ifExec().Execute(context.Background(), executor.ExecInput{
		Config: json.RawMessage(`{"operator":"eq"}`),
		Inputs: map[string]any{"left": 1, "right": 2},
	})
	if out.FiredPort != workflow.PortIfFalse {
		t.Fatalf("port: %s", out.FiredPort)
	}
	if v, _ := out.Outputs["result"].(bool); v {
		t.Fatal("result should be false")
	}
}

func TestIfTypeMismatchReturnsError(t *testing.T) {
	_, err := ifExec().Execute(context.Background(), executor.ExecInput{
		Config: json.RawMessage(`{"operator":"gt"}`),
		Inputs: map[string]any{"left": "abc", "right": 1},
	})
	if err == nil || !strings.Contains(err.Error(), "type") {
		t.Fatalf("expected type mismatch err, got %v", err)
	}
}

func TestIfUnknownOperator(t *testing.T) {
	_, err := ifExec().Execute(context.Background(), executor.ExecInput{
		Config: json.RawMessage(`{"operator":"approx"}`),
		Inputs: map[string]any{"left": 1, "right": 1},
	})
	if err == nil || !strings.Contains(err.Error(), "operator") {
		t.Fatalf("expected unknown op err, got %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd /Users/shinya/Downloads/ShineFlow && go test ./domain/executor/builtin/ -run TestIf
```
Expected: FAIL.

- [ ] **Step 3: Implement**

```go
// domain/executor/builtin/if.go
package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/shinya/shineflow/domain/executor"
	"github.com/shinya/shineflow/domain/nodetype"
	"github.com/shinya/shineflow/domain/workflow"
)

type ifConfig struct {
	Operator string `json:"operator"`
}

type ifExecutor struct{}

func ifFactory(_ *nodetype.NodeType) executor.NodeExecutor { return ifExecutor{} }

func (ifExecutor) Execute(_ context.Context, in executor.ExecInput) (executor.ExecOutput, error) {
	var cfg ifConfig
	if err := json.Unmarshal(in.Config, &cfg); err != nil {
		return executor.ExecOutput{}, fmt.Errorf("if: parse config: %w", err)
	}
	left := in.Inputs["left"]
	right := in.Inputs["right"]

	result, err := evalCondition(cfg.Operator, left, right)
	if err != nil {
		return executor.ExecOutput{}, err
	}
	port := workflow.PortIfFalse
	if result {
		port = workflow.PortIfTrue
	}
	return executor.ExecOutput{
		Outputs:   map[string]any{"result": result},
		FiredPort: port,
	}, nil
}

// evalCondition is shared between if and switch.
func evalCondition(op string, left, right any) (bool, error) {
	switch op {
	case "is_empty":
		return isEmpty(left), nil
	case "is_not_empty":
		return !isEmpty(left), nil
	case "eq":
		return compareEqual(left, right), nil
	case "ne":
		return !compareEqual(left, right), nil
	case "gt", "lt", "gte", "lte":
		lf, lok := coerceFloat64(left)
		rf, rok := coerceFloat64(right)
		if !lok || !rok {
			return false, fmt.Errorf("operator %q: left/right type mismatch (need numeric)", op)
		}
		switch op {
		case "gt":
			return lf > rf, nil
		case "lt":
			return lf < rf, nil
		case "gte":
			return lf >= rf, nil
		case "lte":
			return lf <= rf, nil
		}
	case "contains":
		ls, lok := left.(string)
		rs, rok := right.(string)
		if !lok || !rok {
			return false, fmt.Errorf("operator contains: both sides must be string")
		}
		return strings.Contains(ls, rs), nil
	case "starts_with":
		ls, lok := left.(string)
		rs, rok := right.(string)
		if !lok || !rok {
			return false, fmt.Errorf("operator starts_with: both sides must be string")
		}
		return strings.HasPrefix(ls, rs), nil
	}
	return false, fmt.Errorf("unknown operator: %q", op)
}

func compareEqual(left, right any) bool {
	// Numeric coerce path.
	if lf, lok := coerceFloat64(left); lok {
		if rf, rok := coerceFloat64(right); rok {
			return lf == rf
		}
	}
	// String / bool / fallback to deep equal.
	return left == right
}

func isEmpty(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case string:
		return x == ""
	case []any:
		return len(x) == 0
	case map[string]any:
		return len(x) == 0
	}
	return false
}
```

Append to `wire.go`:
```go
reg.Register(nodetype.BuiltinIf, ifFactory)
```

- [ ] **Step 4: Run tests + commit**

```bash
cd /Users/shinya/Downloads/ShineFlow && go test ./domain/executor/builtin/
git add domain/executor/builtin/if.go domain/executor/builtin/if_test.go domain/executor/builtin/wire.go
git commit -m "feat(builtin): if executor with 10 operators + numeric coerce

Spec 2026-04-27 §13.2/§13.3.

Co-Authored-By: Craft Agent <agents-noreply@craft.do>"
```

---

## Task 8: `builtin.switch` executor

**Files:**
- Create: `domain/executor/builtin/switch.go`
- Create: `domain/executor/builtin/switch_test.go`

- [ ] **Step 1: Write failing tests**

```go
// domain/executor/builtin/switch_test.go
package builtin

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/shinya/shineflow/domain/executor"
	"github.com/shinya/shineflow/domain/workflow"
)

func switchExec() executor.NodeExecutor { return switchFactory(nil) }

func TestSwitchHitsFirstMatchingCase(t *testing.T) {
	cfg := `{"cases":[
		{"name":"hot","operator":"gte","right":{"kind":"literal","value":80}},
		{"name":"cold","operator":"lt","right":{"kind":"literal","value":40}}
	]}`
	out, err := switchExec().Execute(context.Background(), executor.ExecInput{
		Config: json.RawMessage(cfg),
		Inputs: map[string]any{"value": 90},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.FiredPort != "hot" {
		t.Fatalf("port: %s", out.FiredPort)
	}
	if out.Outputs["matched"] != "hot" {
		t.Fatalf("matched: %v", out.Outputs["matched"])
	}
}

func TestSwitchFallsToDefault(t *testing.T) {
	cfg := `{"cases":[
		{"name":"hot","operator":"gte","right":{"kind":"literal","value":80}}
	]}`
	out, _ := switchExec().Execute(context.Background(), executor.ExecInput{
		Config: json.RawMessage(cfg),
		Inputs: map[string]any{"value": 25},
	})
	if out.FiredPort != workflow.PortDefault {
		t.Fatalf("port: %s", out.FiredPort)
	}
	if out.Outputs["matched"] != workflow.PortDefault {
		t.Fatalf("matched: %v", out.Outputs["matched"])
	}
}

func TestSwitchOrderingFirstWins(t *testing.T) {
	cfg := `{"cases":[
		{"name":"any","operator":"is_not_empty","right":{"kind":"literal","value":null}},
		{"name":"specific","operator":"eq","right":{"kind":"literal","value":"x"}}
	]}`
	out, _ := switchExec().Execute(context.Background(), executor.ExecInput{
		Config: json.RawMessage(cfg),
		Inputs: map[string]any{"value": "x"},
	})
	if out.FiredPort != "any" {
		t.Fatalf("port: %s (first matching case wins)", out.FiredPort)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd /Users/shinya/Downloads/ShineFlow && go test ./domain/executor/builtin/ -run TestSwitch
```
Expected: FAIL.

- [ ] **Step 3: Implement**

```go
// domain/executor/builtin/switch.go
package builtin

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/shinya/shineflow/domain/executor"
	"github.com/shinya/shineflow/domain/nodetype"
	"github.com/shinya/shineflow/domain/workflow"
)

type switchCaseDef struct {
	Name     string               `json:"name"`
	Operator string               `json:"operator"`
	Right    workflow.ValueSource `json:"right"`
}

type switchConfigDef struct {
	Cases []switchCaseDef `json:"cases"`
}

type switchExecutor struct{}

func switchFactory(_ *nodetype.NodeType) executor.NodeExecutor { return switchExecutor{} }

func (switchExecutor) Execute(_ context.Context, in executor.ExecInput) (executor.ExecOutput, error) {
	var cfg switchConfigDef
	if err := json.Unmarshal(in.Config, &cfg); err != nil {
		return executor.ExecOutput{}, fmt.Errorf("switch: parse config: %w", err)
	}
	value := in.Inputs["value"]

	for _, c := range cfg.Cases {
		// Right is already resolved if engine resolved Config; if it's still a
		// ValueSource (because Config was passed through as raw), use Value field.
		// Engine spec §12.5 walkExpand only touches strings; ValueSource bodies
		// stay as Go structs after json.Unmarshal here. The right.Value is the
		// literal we compare against.
		right := c.Right.Value
		ok, err := evalCondition(c.Operator, value, right)
		if err != nil {
			return executor.ExecOutput{}, fmt.Errorf("switch case %q: %w", c.Name, err)
		}
		if ok {
			return executor.ExecOutput{
				Outputs:   map[string]any{"matched": c.Name},
				FiredPort: c.Name,
			}, nil
		}
	}
	return executor.ExecOutput{
		Outputs:   map[string]any{"matched": workflow.PortDefault},
		FiredPort: workflow.PortDefault,
	}, nil
}
```

Append to `wire.go`:
```go
reg.Register(nodetype.BuiltinSwitch, switchFactory)
```

- [ ] **Step 4: Run tests + commit**

```bash
cd /Users/shinya/Downloads/ShineFlow && go test ./domain/executor/builtin/
git add domain/executor/builtin/switch.go domain/executor/builtin/switch_test.go domain/executor/builtin/wire.go
git commit -m "feat(builtin): switch executor (cases + default fallback)

Spec 2026-04-27 §13.2/§13.3. First matching case wins; port name = case.name.

Co-Authored-By: Craft Agent <agents-noreply@craft.do>"
```

---

## Task 9: `builtin.http_request` executor

**Files:**
- Create: `domain/executor/builtin/http_request.go`
- Create: `domain/executor/builtin/http_request_test.go`

- [ ] **Step 1: Write failing tests**

```go
// domain/executor/builtin/http_request_test.go
package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/shinya/shineflow/domain/executor"
	"github.com/shinya/shineflow/domain/workflow"
)

type fakeHTTP struct {
	resp executor.HTTPResponse
	err  error
}

func (f *fakeHTTP) Do(_ context.Context, _ executor.HTTPRequest) (executor.HTTPResponse, error) {
	return f.resp, f.err
}

func TestHTTPHappy200(t *testing.T) {
	exe := httpRequestFactory(nil)
	out, err := exe.Execute(context.Background(), executor.ExecInput{
		Config: json.RawMessage(`{"method":"GET","url":"https://x"}`),
		Inputs: map[string]any{},
		Services: executor.ExecServices{
			HTTPClient: &fakeHTTP{resp: executor.HTTPResponse{Status: 200, Body: []byte(`{"ok":true}`)}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.FiredPort != workflow.PortDefault {
		t.Fatalf("port: %s", out.FiredPort)
	}
	if v, _ := out.Outputs["status"].(int); v != 200 {
		t.Fatalf("status: %v", out.Outputs["status"])
	}
}

func TestHTTP4xxFiresError(t *testing.T) {
	exe := httpRequestFactory(nil)
	out, err := exe.Execute(context.Background(), executor.ExecInput{
		Config: json.RawMessage(`{"method":"GET","url":"https://x"}`),
		Inputs: map[string]any{},
		Services: executor.ExecServices{
			HTTPClient: &fakeHTTP{resp: executor.HTTPResponse{Status: 404, Body: []byte(`not found`)}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.FiredPort != workflow.PortError {
		t.Fatalf("port: %s", out.FiredPort)
	}
	if v, _ := out.Outputs["status"].(int); v != 404 {
		t.Fatalf("status: %v", out.Outputs["status"])
	}
}

func TestHTTPTransportErrPropagates(t *testing.T) {
	exe := httpRequestFactory(nil)
	wantErr := errors.New("connect refused")
	_, err := exe.Execute(context.Background(), executor.ExecInput{
		Config:   json.RawMessage(`{"method":"GET","url":"https://x"}`),
		Services: executor.ExecServices{HTTPClient: &fakeHTTP{err: wantErr}},
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected wrapped %v, got %v", wantErr, err)
	}
}

func TestHTTPClientNotConfigured(t *testing.T) {
	exe := httpRequestFactory(nil)
	_, err := exe.Execute(context.Background(), executor.ExecInput{
		Config:   json.RawMessage(`{"method":"GET","url":"https://x"}`),
		Services: executor.ExecServices{HTTPClient: nil},
	})
	if !errors.Is(err, ErrPortNotConfigured) {
		t.Fatalf("expected ErrPortNotConfigured, got %v", err)
	}
}
```

- [ ] **Step 2: Verify `executor.HTTPRequest` / `HTTPResponse` shapes**

```bash
grep -n "HTTPRequest\|HTTPResponse" /Users/shinya/Downloads/ShineFlow/domain/executor/exec_input.go
```

If the shape doesn't include `Status`, `Body`, `Headers`, etc., adjust the test or add the missing fields. (Existing definitions may differ — adapt.)

- [ ] **Step 3: Run tests to verify they fail**

```bash
cd /Users/shinya/Downloads/ShineFlow && go test ./domain/executor/builtin/ -run TestHTTP
```
Expected: FAIL.

- [ ] **Step 4: Implement**

```go
// domain/executor/builtin/http_request.go
package builtin

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/shinya/shineflow/domain/executor"
	"github.com/shinya/shineflow/domain/nodetype"
	"github.com/shinya/shineflow/domain/workflow"
)

type httpRequestConfig struct {
	Method  string            `json:"method"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    json.RawMessage   `json:"body,omitempty"`
}

type httpRequestExecutor struct{}

func httpRequestFactory(_ *nodetype.NodeType) executor.NodeExecutor { return httpRequestExecutor{} }

func (httpRequestExecutor) Execute(ctx context.Context, in executor.ExecInput) (executor.ExecOutput, error) {
	if in.Services.HTTPClient == nil {
		return executor.ExecOutput{}, fmt.Errorf("http_request: %w", ErrPortNotConfigured)
	}
	var cfg httpRequestConfig
	if err := json.Unmarshal(in.Config, &cfg); err != nil {
		return executor.ExecOutput{}, fmt.Errorf("http_request: parse config: %w", err)
	}
	resp, err := in.Services.HTTPClient.Do(ctx, executor.HTTPRequest{
		Method:  cfg.Method,
		URL:     cfg.URL,
		Headers: cfg.Headers,
		Body:    cfg.Body,
	})
	if err != nil {
		return executor.ExecOutput{}, fmt.Errorf("http_request transport: %w", err)
	}
	port := workflow.PortDefault
	if resp.Status >= 400 {
		port = workflow.PortError
	}
	return executor.ExecOutput{
		Outputs: map[string]any{
			"status":  resp.Status,
			"headers": resp.Headers,
			"body":    resp.Body,
		},
		FiredPort: port,
	}, nil
}
```

Append to `wire.go`:
```go
reg.Register(nodetype.BuiltinHTTPRequest, httpRequestFactory)
```

- [ ] **Step 5: Run tests + commit**

```bash
cd /Users/shinya/Downloads/ShineFlow && go test ./domain/executor/builtin/
git add domain/executor/builtin/http_request.go domain/executor/builtin/http_request_test.go domain/executor/builtin/wire.go
git commit -m "feat(builtin): http_request executor

Spec 2026-04-27 §13.2/§13.3. 2xx/3xx → default; 4xx/5xx → error
(both with {status, headers, body}); transport err → ErrorPolicy.
nil HTTPClient → ErrPortNotConfigured.

Co-Authored-By: Craft Agent <agents-noreply@craft.do>"
```

---

## Task 10: `builtin.llm` executor

**Files:**
- Create: `domain/executor/builtin/llm.go`
- Create: `domain/executor/builtin/llm_test.go`

- [ ] **Step 1: Write failing tests**

```go
// domain/executor/builtin/llm_test.go
package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/shinya/shineflow/domain/executor"
	"github.com/shinya/shineflow/domain/workflow"
)

type fakeLLMResp struct {
	resp executor.LLMResponse
	err  error
}

func (f *fakeLLMResp) Complete(_ context.Context, _ executor.LLMRequest) (executor.LLMResponse, error) {
	return f.resp, f.err
}

func TestLLMHappy(t *testing.T) {
	exe := llmFactory(nil)
	out, err := exe.Execute(context.Background(), executor.ExecInput{
		Config: json.RawMessage(`{"provider":"openai","model":"gpt-4","temperature":0.5,"max_tokens":100}`),
		Inputs: map[string]any{
			"messages": []any{
				map[string]any{"role": "user", "content": "hi"},
			},
		},
		Services: executor.ExecServices{LLMClient: &fakeLLMResp{
			resp: executor.LLMResponse{Text: "hello", Model: "gpt-4", Usage: executor.LLMUsage{InputTokens: 5, OutputTokens: 1}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.FiredPort != workflow.PortDefault {
		t.Fatalf("port: %s", out.FiredPort)
	}
	if out.Outputs["text"] != "hello" {
		t.Fatalf("text: %v", out.Outputs["text"])
	}
	if out.Outputs["model"] != "gpt-4" {
		t.Fatalf("model: %v", out.Outputs["model"])
	}
}

func TestLLMTransportErrPropagates(t *testing.T) {
	exe := llmFactory(nil)
	wantErr := errors.New("network down")
	_, err := exe.Execute(context.Background(), executor.ExecInput{
		Config:   json.RawMessage(`{"provider":"openai","model":"gpt-4"}`),
		Inputs:   map[string]any{"messages": []any{}},
		Services: executor.ExecServices{LLMClient: &fakeLLMResp{err: wantErr}},
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected wrapped %v, got %v", wantErr, err)
	}
}

func TestLLMClientNotConfigured(t *testing.T) {
	exe := llmFactory(nil)
	_, err := exe.Execute(context.Background(), executor.ExecInput{
		Config:   json.RawMessage(`{"provider":"openai","model":"gpt-4"}`),
		Inputs:   map[string]any{"messages": []any{}},
		Services: executor.ExecServices{LLMClient: nil},
	})
	if !errors.Is(err, ErrPortNotConfigured) {
		t.Fatalf("expected ErrPortNotConfigured, got %v", err)
	}
}

func TestLLMPromptOnly(t *testing.T) {
	// "prompt" input shorthand -> single user message
	exe := llmFactory(nil)
	out, err := exe.Execute(context.Background(), executor.ExecInput{
		Config:   json.RawMessage(`{"provider":"openai","model":"gpt-4","system_prompt":"You are helpful"}`),
		Inputs:   map[string]any{"prompt": "Translate"},
		Services: executor.ExecServices{LLMClient: &fakeLLMResp{resp: executor.LLMResponse{Text: "ok"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Outputs["text"] != "ok" {
		t.Fatalf("text: %v", out.Outputs["text"])
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd /Users/shinya/Downloads/ShineFlow && go test ./domain/executor/builtin/ -run TestLLM
```
Expected: FAIL.

- [ ] **Step 3: Implement**

```go
// domain/executor/builtin/llm.go
package builtin

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/shinya/shineflow/domain/executor"
	"github.com/shinya/shineflow/domain/nodetype"
	"github.com/shinya/shineflow/domain/workflow"
)

type llmConfig struct {
	Provider     string  `json:"provider"`
	Model        string  `json:"model"`
	SystemPrompt string  `json:"system_prompt,omitempty"`
	Temperature  float64 `json:"temperature,omitempty"`
	MaxTokens    int     `json:"max_tokens,omitempty"`
}

type llmExecutor struct{}

func llmFactory(_ *nodetype.NodeType) executor.NodeExecutor { return llmExecutor{} }

func (llmExecutor) Execute(ctx context.Context, in executor.ExecInput) (executor.ExecOutput, error) {
	if in.Services.LLMClient == nil {
		return executor.ExecOutput{}, fmt.Errorf("llm: %w", ErrPortNotConfigured)
	}
	var cfg llmConfig
	if err := json.Unmarshal(in.Config, &cfg); err != nil {
		return executor.ExecOutput{}, fmt.Errorf("llm: parse config: %w", err)
	}
	if cfg.Model == "" {
		return executor.ExecOutput{}, fmt.Errorf("llm: config.model required")
	}

	messages, err := buildMessages(cfg.SystemPrompt, in.Inputs)
	if err != nil {
		return executor.ExecOutput{}, err
	}

	resp, err := in.Services.LLMClient.Complete(ctx, executor.LLMRequest{
		Provider:    cfg.Provider,
		Model:       cfg.Model,
		Messages:    messages,
		Temperature: cfg.Temperature,
		MaxTokens:   cfg.MaxTokens,
	})
	if err != nil {
		return executor.ExecOutput{}, fmt.Errorf("llm transport: %w", err)
	}
	return executor.ExecOutput{
		Outputs: map[string]any{
			"text":  resp.Text,
			"model": resp.Model,
			"usage": map[string]any{
				"input_tokens":  resp.Usage.InputTokens,
				"output_tokens": resp.Usage.OutputTokens,
			},
		},
		FiredPort: workflow.PortDefault,
	}, nil
}

// buildMessages accepts either an Inputs.messages array (preferred) or
// a single Inputs.prompt string (shorthand). System prompt is prepended.
func buildMessages(systemPrompt string, inputs map[string]any) ([]executor.LLMMessage, error) {
	var msgs []executor.LLMMessage
	if systemPrompt != "" {
		msgs = append(msgs, executor.LLMMessage{Role: "system", Content: systemPrompt})
	}
	if raw, ok := inputs["messages"]; ok {
		arr, ok := raw.([]any)
		if !ok {
			return nil, fmt.Errorf("llm: input.messages must be array, got %T", raw)
		}
		for i, item := range arr {
			m, ok := item.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("llm: input.messages[%d] must be object", i)
			}
			role, _ := m["role"].(string)
			content, _ := m["content"].(string)
			msgs = append(msgs, executor.LLMMessage{Role: role, Content: content})
		}
		return msgs, nil
	}
	if prompt, ok := inputs["prompt"].(string); ok {
		msgs = append(msgs, executor.LLMMessage{Role: "user", Content: prompt})
		return msgs, nil
	}
	return nil, fmt.Errorf("llm: must provide input.messages or input.prompt")
}
```

Append to `wire.go`:
```go
reg.Register(nodetype.BuiltinLLM, llmFactory)
```

- [ ] **Step 4: Run tests + commit**

```bash
cd /Users/shinya/Downloads/ShineFlow && go test ./domain/executor/builtin/
git add domain/executor/builtin/llm.go domain/executor/builtin/llm_test.go domain/executor/builtin/wire.go
git commit -m "feat(builtin): llm executor (provider-agnostic, messages or prompt)

Spec 2026-04-27 §13.2/§13.3. nil LLMClient → ErrPortNotConfigured.
Transport err → ErrorPolicy. Future spec adds error-port semantics for
provider business errors (4xx/safety/refusal).

Co-Authored-By: Craft Agent <agents-noreply@craft.do>"
```

---

## Task 11: Wire integration test

**Files:**
- Create: `domain/executor/builtin/wire_test.go`

- [ ] **Step 1: Write the test**

```go
// domain/executor/builtin/wire_test.go
package builtin

import (
	"testing"

	"github.com/shinya/shineflow/domain/executor"
	"github.com/shinya/shineflow/domain/nodetype"
)

// fakeRegistry captures Register calls.
type fakeRegistry struct {
	registered map[string]bool
}

func (r *fakeRegistry) Register(keyPattern string, _ executor.ExecutorFactory) {
	if r.registered == nil {
		r.registered = map[string]bool{}
	}
	r.registered[keyPattern] = true
}

func (r *fakeRegistry) Build(_ *nodetype.NodeType) (executor.NodeExecutor, error) {
	return nil, nil // not used here
}

func TestRegisterInstallsAllEightFactories(t *testing.T) {
	r := &fakeRegistry{}
	Register(r)
	want := []string{
		nodetype.BuiltinStart, nodetype.BuiltinEnd, nodetype.BuiltinLLM,
		nodetype.BuiltinIf, nodetype.BuiltinSwitch, nodetype.BuiltinJoin,
		nodetype.BuiltinSetVariable, nodetype.BuiltinHTTPRequest,
	}
	for _, k := range want {
		if !r.registered[k] {
			t.Errorf("missing registration: %s", k)
		}
	}
	if len(r.registered) != len(want) {
		t.Errorf("registered %d, want %d", len(r.registered), len(want))
	}
}
```

- [ ] **Step 2: Run test**

```bash
cd /Users/shinya/Downloads/ShineFlow && go test ./domain/executor/builtin/ -run TestRegister
```
Expected: PASS (all factories already wired in Tasks 3-10).

- [ ] **Step 3: Commit**

```bash
git add domain/executor/builtin/wire_test.go
git commit -m "test(builtin): wire integration — verify all 8 factories registered

Co-Authored-By: Craft Agent <agents-noreply@craft.do>"
```

---

## Final Verification

- [ ] **Run the full suite + vet**

```bash
cd /Users/shinya/Downloads/ShineFlow
go vet ./...
go test ./domain/...
```
Both must be clean.

- [ ] **Confirm spec §13 catalog rows are all implemented**

Walk §13.2 row-by-row:
- start ✓
- end ✓
- llm ✓
- if (10 operators) ✓
- switch ✓
- join ✓
- set_variable ✓
- http_request ✓

---

## Self-Review Checklist

1. **Spec coverage**: §13.1 (Register), §13.2 (8 executor table), §13.3 (key behaviors), §13.3.1 (numeric coerce), §13.4 (LLMClient port already covered by foundation plan). All covered. Note: error-port output for `if/switch/llm` (spec calls for `{error_code, error_message}` shape) is NOT exercised in this plan because driver decides when to fire error port via ErrorPolicy + the executor doesn't emit error-port outputs itself in this plan. The shape contract becomes relevant when the engine driver tests cover fire_error_port + propagate; covered in Plan 3.
2. **Placeholder scan**: no "TBD" / generic "implement later" in any step.
3. **Type consistency**: `ExecInput.Inputs` is `map[string]any` everywhere; `ExecOutput.Outputs` is `map[string]any`; port names use the constants from `domain/workflow` (`PortDefault`, `PortError`, `PortIfTrue`, `PortIfFalse`); switch case port = `case.name`.
4. **Existing-code reuse**: assumes `executor.HTTPRequest` / `HTTPResponse` already have `Method, URL, Headers, Body` / `Status, Headers, Body` fields. Task 9 step 2 explicitly verifies this and notes adaptation.
