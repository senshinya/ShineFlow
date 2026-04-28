package engine_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/shinya/shineflow/domain/engine"
	"github.com/shinya/shineflow/domain/engine/enginetest"
	"github.com/shinya/shineflow/domain/executor"
	"github.com/shinya/shineflow/domain/nodetype"
	"github.com/shinya/shineflow/domain/run"
	"github.com/shinya/shineflow/domain/workflow"
)

func TestEngineSunshineCompletes(t *testing.T) {
	h := enginetest.New(t)
	dsl := enginetest.NewDSL().
		Start("s").
		NodeWithInputs("sv", nodetype.BuiltinSetVariable, `{"name":"hello"}`, map[string]workflow.ValueSource{
			"value": {Kind: workflow.ValueKindLiteral, Value: "world"},
		}).
		End("e").
		Edge("s", workflow.PortDefault, "sv").
		Edge("sv", workflow.PortDefault, "e").
		Build()
	h.WorkflowRepo.PutVersion(releasedVersion("v1", dsl))

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
	nrs, err := h.RunRepo.ListNodeRuns(context.Background(), out.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(nrs) != 3 {
		t.Fatalf("node runs: %d", len(nrs))
	}
	for _, nr := range nrs {
		if nr.Status != run.NodeRunStatusSuccess {
			t.Errorf("%s: %v", nr.NodeID, nr.Status)
		}
	}
}

func TestEngineRejectsDraftVersion(t *testing.T) {
	h := enginetest.New(t)
	h.WorkflowRepo.PutVersion(&workflow.WorkflowVersion{
		ID:           "draft-v1",
		DefinitionID: "d1",
		Version:      1,
		State:        workflow.VersionStateDraft,
		DSL:          enginetest.NewDSL().Start("s").End("e").Edge("s", workflow.PortDefault, "e").Build(),
	})

	_, err := h.Engine.Start(context.Background(), engine.StartInput{VersionID: "draft-v1", TriggerKind: run.TriggerKindManual, TriggerPayload: json.RawMessage(`{}`)})
	if !errors.Is(err, engine.ErrVersionNotPublished) {
		t.Fatalf("error: %v", err)
	}
}

func TestEngineNoEndReachedFails(t *testing.T) {
	h := enginetest.New(t)
	dsl := enginetest.NewDSL().Start("s").Build()
	h.WorkflowRepo.PutVersion(releasedVersion("v1", dsl))

	out, err := h.Engine.Start(context.Background(), engine.StartInput{VersionID: "v1", TriggerKind: run.TriggerKindManual, TriggerPayload: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != run.RunStatusFailed {
		t.Fatalf("status: %v", out.Status)
	}
	if out.Error == nil || out.Error.Code != run.RunErrCodeNoEndReached {
		t.Fatalf("error: %+v", out.Error)
	}
}

func TestEngineUnserializableOutputFailsNodeAndRun(t *testing.T) {
	h := enginetest.New(t)
	h.NTReg.Put(&nodetype.NodeType{
		Key:      "test.bad_output",
		Version:  nodetype.NodeTypeVersion1,
		Name:     "Bad Output",
		Category: nodetype.CategoryTool,
		Ports:    []string{workflow.PortDefault},
	})
	h.ExReg.Register("test.bad_output", enginetest.MockFactory(&enginetest.MockExecutor{
		OnExecute: func(context.Context, executor.ExecInput) (executor.ExecOutput, error) {
			return executor.ExecOutput{Outputs: map[string]any{"ch": make(chan int)}, FiredPort: workflow.PortDefault}, nil
		},
	}))
	dsl := enginetest.NewDSL().
		Start("s").
		Node("bad", "test.bad_output", `{}`).
		End("e").
		Edge("s", workflow.PortDefault, "bad").
		Edge("bad", workflow.PortDefault, "e").
		Build()
	h.WorkflowRepo.PutVersion(releasedVersion("v1", dsl))

	out, err := h.Engine.Start(context.Background(), engine.StartInput{VersionID: "v1", TriggerKind: run.TriggerKindManual, TriggerPayload: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != run.RunStatusFailed || out.Error == nil || out.Error.Code != run.RunErrCodeOutputNotSerializable {
		t.Fatalf("run: status=%v error=%+v", out.Status, out.Error)
	}
	nrs, _ := h.RunRepo.ListNodeRuns(context.Background(), out.ID)
	for _, nr := range nrs {
		if nr.NodeID == "bad" {
			if nr.Status != run.NodeRunStatusFailed {
				t.Fatalf("bad node status: %v", nr.Status)
			}
			return
		}
	}
	t.Fatal("bad node run missing")
}

func TestEngineFireErrorPortReachesEnd(t *testing.T) {
	llm := &enginetest.MockLLMClient{
		OnComplete: func(context.Context, executor.LLMRequest) (executor.LLMResponse, error) {
			return executor.LLMResponse{}, errors.New("provider down")
		},
	}
	h := enginetest.New(t, enginetest.WithMockLLMClient(llm))
	dsl := enginetest.NewDSL().
		Start("s").
		NodeWithInputs("llm1", nodetype.BuiltinLLM, `{"provider":"x","model":"m1"}`, map[string]workflow.ValueSource{
			"prompt": {Kind: workflow.ValueKindLiteral, Value: "hi"},
		}).
		End("e").
		Edge("s", workflow.PortDefault, "llm1").
		Edge("llm1", workflow.PortError, "e").
		Build()
	for i := range dsl.Nodes {
		if dsl.Nodes[i].ID == "llm1" {
			dsl.Nodes[i].ErrorPolicy = &workflow.ErrorPolicy{OnFinalFail: workflow.FailStrategyFireErrorPort}
		}
	}
	h.WorkflowRepo.PutVersion(releasedVersion("v1", dsl))

	out, err := h.Engine.Start(context.Background(), engine.StartInput{VersionID: "v1", TriggerKind: run.TriggerKindManual, TriggerPayload: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != run.RunStatusSuccess {
		t.Fatalf("status: %v", out.Status)
	}
	nrs, _ := h.RunRepo.ListNodeRuns(context.Background(), out.ID)
	for _, nr := range nrs {
		if nr.NodeID == "llm1" {
			if nr.Status != run.NodeRunStatusFailed || nr.FiredPort != workflow.PortError {
				t.Fatalf("llm node run: status=%v fired=%q", nr.Status, nr.FiredPort)
			}
			return
		}
	}
	t.Fatal("llm node run missing")
}

func TestEngineJoinAllWaitsForAllLiveInputs(t *testing.T) {
	h := enginetest.New(t)
	dsl := enginetest.NewDSL().
		Start("s").
		NodeWithInputs("a", nodetype.BuiltinSetVariable, `{"name":"a"}`, map[string]workflow.ValueSource{
			"value": {Kind: workflow.ValueKindLiteral, Value: 1},
		}).
		NodeWithInputs("b", nodetype.BuiltinSetVariable, `{"name":"b"}`, map[string]workflow.ValueSource{
			"value": {Kind: workflow.ValueKindLiteral, Value: 2},
		}).
		Node("j", nodetype.BuiltinJoin, `{"mode":"all"}`).
		End("e").
		Edge("s", workflow.PortDefault, "a").
		Edge("s", workflow.PortDefault, "b").
		Edge("a", workflow.PortDefault, "j").
		Edge("b", workflow.PortDefault, "j").
		Edge("j", workflow.PortDefault, "e").
		Build()
	h.WorkflowRepo.PutVersion(releasedVersion("v1", dsl))

	out, err := h.Engine.Start(context.Background(), engine.StartInput{VersionID: "v1", TriggerKind: run.TriggerKindManual, TriggerPayload: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != run.RunStatusSuccess {
		t.Fatalf("status: %v", out.Status)
	}
	nrs, _ := h.RunRepo.ListNodeRuns(context.Background(), out.ID)
	seen := map[string]run.NodeRunStatus{}
	for _, nr := range nrs {
		seen[nr.NodeID] = nr.Status
	}
	if seen["a"] != run.NodeRunStatusSuccess || seen["b"] != run.NodeRunStatusSuccess || seen["j"] != run.NodeRunStatusSuccess {
		t.Fatalf("statuses: %+v", seen)
	}
}

func TestEngineNodeTimeoutUsesRetryFallback(t *testing.T) {
	httpMock := &enginetest.MockHTTPClient{
		OnDo: func(ctx context.Context, _ executor.HTTPRequest) (executor.HTTPResponse, error) {
			<-ctx.Done()
			return executor.HTTPResponse{}, ctx.Err()
		},
	}
	h := enginetest.New(t, enginetest.WithMockHTTPClient(httpMock))
	dsl := enginetest.NewDSL().
		Start("s").
		Node("http1", nodetype.BuiltinHTTPRequest, `{"method":"GET","url":"https://timeout.test"}`).
		End("e").
		Edge("s", workflow.PortDefault, "http1").
		Edge("http1", workflow.PortDefault, "e").
		Build()
	for i := range dsl.Nodes {
		if dsl.Nodes[i].ID == "http1" {
			dsl.Nodes[i].ErrorPolicy = &workflow.ErrorPolicy{
				Timeout:     time.Millisecond,
				MaxRetries:  1,
				RetryDelay:  0,
				OnFinalFail: workflow.FailStrategyFallback,
				FallbackOutput: workflow.FallbackOutput{
					Port:   workflow.PortDefault,
					Output: map[string]any{"ok": true},
				},
			}
		}
	}
	h.WorkflowRepo.PutVersion(releasedVersion("v1", dsl))

	out, err := h.Engine.Start(context.Background(), engine.StartInput{VersionID: "v1", TriggerKind: run.TriggerKindManual, TriggerPayload: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != run.RunStatusSuccess {
		t.Fatalf("status: %v", out.Status)
	}
	nrs, _ := h.RunRepo.ListNodeRuns(context.Background(), out.ID)
	attempts := 0
	fallback := false
	for _, nr := range nrs {
		if nr.NodeID == "http1" {
			attempts++
			fallback = fallback || nr.FallbackApplied
		}
	}
	if attempts != 2 || !fallback {
		t.Fatalf("attempts=%d fallback=%v", attempts, fallback)
	}
}

func TestEngineFinalizeWaitsForResolvedInputsPersisted(t *testing.T) {
	h := enginetest.New(t)
	delayed := &delayedResolvedRunRepo{FakeRunRepo: enginetest.NewFakeRunRepo(), delay: 10 * time.Millisecond}
	eng := engine.New(h.WorkflowRepo, delayed, h.NTReg, h.ExReg, executor.ExecServices{Logger: enginetest.MockLogger{}}, engine.Config{})
	dsl := enginetest.NewDSL().
		Start("s").
		End("e").
		Edge("s", workflow.PortDefault, "e").
		Build()
	for i := range dsl.Nodes {
		if dsl.Nodes[i].ID == "e" {
			dsl.Nodes[i].Inputs = map[string]workflow.ValueSource{"answer": {Kind: workflow.ValueKindLiteral, Value: "ok"}}
		}
	}
	h.WorkflowRepo.PutVersion(releasedVersion("v1", dsl))

	out, err := eng.Start(context.Background(), engine.StartInput{VersionID: "v1", TriggerKind: run.TriggerKindManual, TriggerPayload: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(out.Output, &got); err != nil {
		t.Fatal(err)
	}
	if got["answer"] != "ok" {
		t.Fatalf("output: %s", string(out.Output))
	}
}

func TestEngineMultiEndFirstWins(t *testing.T) {
	httpMock := &enginetest.MockHTTPClient{
		OnDo: func(ctx context.Context, _ executor.HTTPRequest) (executor.HTTPResponse, error) {
			<-ctx.Done()
			return executor.HTTPResponse{}, ctx.Err()
		},
	}
	h := enginetest.New(t, enginetest.WithMockHTTPClient(httpMock))
	dsl := enginetest.NewDSL().
		Start("s").
		NodeWithInputs("fast", nodetype.BuiltinSetVariable, `{"name":"x"}`, map[string]workflow.ValueSource{
			"value": {Kind: workflow.ValueKindLiteral, Value: "fast"},
		}).
		Node("slow", nodetype.BuiltinHTTPRequest, `{"method":"GET","url":"https://slow.test"}`).
		End("eA").
		End("eB").
		Edge("s", workflow.PortDefault, "fast").
		Edge("s", workflow.PortDefault, "slow").
		Edge("fast", workflow.PortDefault, "eA").
		Edge("slow", workflow.PortDefault, "eB").
		Build()
	h.WorkflowRepo.PutVersion(releasedVersion("v1", dsl))

	out, err := h.Engine.Start(context.Background(), engine.StartInput{VersionID: "v1", TriggerKind: run.TriggerKindManual, TriggerPayload: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != run.RunStatusSuccess {
		t.Fatalf("status: %v", out.Status)
	}
	if out.EndNodeID == nil || *out.EndNodeID != "eA" {
		t.Fatalf("EndNodeID: %v", out.EndNodeID)
	}
	nrs, err := h.RunRepo.ListNodeRuns(context.Background(), out.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, nr := range nrs {
		if nr.NodeID == "slow" && nr.Status != run.NodeRunStatusCancelled {
			t.Fatalf("slow status: %v", nr.Status)
		}
	}
}

func TestEngineLateGenericErrorAfterFirstEndStillSucceeds(t *testing.T) {
	httpMock := &enginetest.MockHTTPClient{
		OnDo: func(ctx context.Context, _ executor.HTTPRequest) (executor.HTTPResponse, error) {
			<-ctx.Done()
			return executor.HTTPResponse{}, errors.New("transport closed")
		},
	}
	h := enginetest.New(t, enginetest.WithMockHTTPClient(httpMock))
	dsl := enginetest.NewDSL().
		Start("s").
		NodeWithInputs("fast", nodetype.BuiltinSetVariable, `{"name":"x"}`, map[string]workflow.ValueSource{
			"value": {Kind: workflow.ValueKindLiteral, Value: "fast"},
		}).
		Node("slow", nodetype.BuiltinHTTPRequest, `{"method":"GET","url":"https://slow.test"}`).
		End("eA").
		End("eB").
		Edge("s", workflow.PortDefault, "fast").
		Edge("s", workflow.PortDefault, "slow").
		Edge("fast", workflow.PortDefault, "eA").
		Edge("slow", workflow.PortDefault, "eB").
		Build()
	h.WorkflowRepo.PutVersion(releasedVersion("v1", dsl))

	out, err := h.Engine.Start(context.Background(), engine.StartInput{VersionID: "v1", TriggerKind: run.TriggerKindManual, TriggerPayload: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != run.RunStatusSuccess {
		t.Fatalf("status: %v error=%+v", out.Status, out.Error)
	}
	if out.EndNodeID == nil || *out.EndNodeID != "eA" {
		t.Fatalf("EndNodeID: %v", out.EndNodeID)
	}
}

func TestEngineCallerCancelReturnsCancelled(t *testing.T) {
	outerCtx, cancel := context.WithCancel(context.Background())
	httpMock := &enginetest.MockHTTPClient{
		OnDo: func(ctx context.Context, _ executor.HTTPRequest) (executor.HTTPResponse, error) {
			cancel()
			<-ctx.Done()
			return executor.HTTPResponse{}, ctx.Err()
		},
	}
	h := enginetest.New(t, enginetest.WithMockHTTPClient(httpMock))
	dsl := enginetest.NewDSL().
		Start("s").
		Node("slow", nodetype.BuiltinHTTPRequest, `{"method":"GET","url":"https://slow.test"}`).
		End("e").
		Edge("s", workflow.PortDefault, "slow").
		Edge("slow", workflow.PortDefault, "e").
		Build()
	h.WorkflowRepo.PutVersion(releasedVersion("v1", dsl))

	out, err := h.Engine.Start(outerCtx, engine.StartInput{VersionID: "v1", TriggerKind: run.TriggerKindManual, TriggerPayload: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != run.RunStatusCancelled {
		t.Fatalf("status: %v", out.Status)
	}
}

func TestEngineExternalCancelPreventsLateSuccessPropagation(t *testing.T) {
	outerCtx, cancel := context.WithCancel(context.Background())
	h := enginetest.New(t)
	h.NTReg.Put(&nodetype.NodeType{Key: "test.ignore_cancel", Version: nodetype.NodeTypeVersion1, Name: "Ignore Cancel", Category: nodetype.CategoryTool, Ports: []string{workflow.PortDefault}})
	h.ExReg.Register("test.ignore_cancel", enginetest.MockFactory(&enginetest.MockExecutor{
		OnExecute: func(context.Context, executor.ExecInput) (executor.ExecOutput, error) {
			cancel()
			time.Sleep(time.Millisecond)
			return executor.ExecOutput{Outputs: map[string]any{}, FiredPort: workflow.PortDefault}, nil
		},
	}))
	dsl := enginetest.NewDSL().
		Start("s").
		Node("worker", "test.ignore_cancel", `{}`).
		End("e").
		Edge("s", workflow.PortDefault, "worker").
		Edge("worker", workflow.PortDefault, "e").
		Build()
	h.WorkflowRepo.PutVersion(releasedVersion("v1", dsl))

	out, err := h.Engine.Start(outerCtx, engine.StartInput{VersionID: "v1", TriggerKind: run.TriggerKindManual, TriggerPayload: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != run.RunStatusCancelled {
		t.Fatalf("status: %v", out.Status)
	}
	nrs, _ := h.RunRepo.ListNodeRuns(context.Background(), out.ID)
	for _, nr := range nrs {
		if nr.NodeID == "worker" && nr.Status != run.NodeRunStatusCancelled {
			t.Fatalf("worker status: %v", nr.Status)
		}
		if nr.NodeID == "e" {
			t.Fatal("end node should not run after external cancellation")
		}
	}
}

func TestEngineFirstEndCancelsLateSuccessfulBranch(t *testing.T) {
	h := enginetest.New(t)
	h.NTReg.Put(&nodetype.NodeType{Key: "test.late_success", Version: nodetype.NodeTypeVersion1, Name: "Late Success", Category: nodetype.CategoryTool, Ports: []string{workflow.PortDefault}})
	h.ExReg.Register("test.late_success", enginetest.MockFactory(&enginetest.MockExecutor{
		OnExecute: func(ctx context.Context, _ executor.ExecInput) (executor.ExecOutput, error) {
			<-ctx.Done()
			return executor.ExecOutput{Outputs: map[string]any{}, FiredPort: workflow.PortDefault}, nil
		},
	}))
	dsl := enginetest.NewDSL().
		Start("s").
		NodeWithInputs("fast", nodetype.BuiltinSetVariable, `{"name":"x"}`, map[string]workflow.ValueSource{
			"value": {Kind: workflow.ValueKindLiteral, Value: "fast"},
		}).
		Node("late", "test.late_success", `{}`).
		End("eA").
		End("eB").
		Edge("s", workflow.PortDefault, "fast").
		Edge("s", workflow.PortDefault, "late").
		Edge("fast", workflow.PortDefault, "eA").
		Edge("late", workflow.PortDefault, "eB").
		Build()
	h.WorkflowRepo.PutVersion(releasedVersion("v1", dsl))

	out, err := h.Engine.Start(context.Background(), engine.StartInput{VersionID: "v1", TriggerKind: run.TriggerKindManual, TriggerPayload: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != run.RunStatusSuccess {
		t.Fatalf("status: %v", out.Status)
	}
	nrs, _ := h.RunRepo.ListNodeRuns(context.Background(), out.ID)
	for _, nr := range nrs {
		if nr.NodeID == "late" && nr.Status != run.NodeRunStatusCancelled {
			t.Fatalf("late status: %v", nr.Status)
		}
	}
}

func TestEngineRunTimeoutReturnsCancelled(t *testing.T) {
	httpMock := &enginetest.MockHTTPClient{
		OnDo: func(ctx context.Context, _ executor.HTTPRequest) (executor.HTTPResponse, error) {
			<-ctx.Done()
			return executor.HTTPResponse{}, ctx.Err()
		},
	}
	h := enginetest.New(t, enginetest.WithMockHTTPClient(httpMock))
	h.Engine = engine.New(h.WorkflowRepo, h.RunRepo, h.NTReg, h.ExReg, executor.ExecServices{
		Logger:     enginetest.MockLogger{},
		HTTPClient: httpMock,
	}, engine.Config{RunTimeout: time.Millisecond})
	dsl := enginetest.NewDSL().
		Start("s").
		Node("slow", nodetype.BuiltinHTTPRequest, `{"method":"GET","url":"https://slow.test"}`).
		End("e").
		Edge("s", workflow.PortDefault, "slow").
		Edge("slow", workflow.PortDefault, "e").
		Build()
	h.WorkflowRepo.PutVersion(releasedVersion("v1", dsl))

	out, err := h.Engine.Start(context.Background(), engine.StartInput{VersionID: "v1", TriggerKind: run.TriggerKindManual, TriggerPayload: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != run.RunStatusCancelled {
		t.Fatalf("status: %v", out.Status)
	}
}

func TestEngineRetryThenFallback(t *testing.T) {
	llm := &enginetest.MockLLMClient{
		OnComplete: func(context.Context, executor.LLMRequest) (executor.LLMResponse, error) {
			return executor.LLMResponse{}, errors.New("simulated provider error")
		},
	}
	h := enginetest.New(t, enginetest.WithMockLLMClient(llm))
	dsl := enginetest.NewDSL().
		Start("s").
		NodeWithInputs("llm1", nodetype.BuiltinLLM, `{"provider":"x","model":"m1"}`, map[string]workflow.ValueSource{
			"prompt": {Kind: workflow.ValueKindLiteral, Value: "hi"},
		}).
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
		}
	}
	h.WorkflowRepo.PutVersion(releasedVersion("v1", dsl))

	out, err := h.Engine.Start(context.Background(), engine.StartInput{VersionID: "v1", TriggerKind: run.TriggerKindManual, TriggerPayload: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != run.RunStatusSuccess {
		t.Fatalf("status: %v", out.Status)
	}
	nrs, err := h.RunRepo.ListNodeRuns(context.Background(), out.ID)
	if err != nil {
		t.Fatal(err)
	}
	llmRuns := 0
	fallbackApplied := false
	for _, nr := range nrs {
		if nr.NodeID == "llm1" {
			llmRuns++
			if nr.FallbackApplied {
				fallbackApplied = true
				if nr.Status != run.NodeRunStatusFailed {
					t.Fatalf("fallback status: %v", nr.Status)
				}
			}
		}
	}
	if llmRuns != 3 {
		t.Fatalf("attempts: %d", llmRuns)
	}
	if !fallbackApplied {
		t.Fatal("fallback missing")
	}
}

func TestPersisterErrorFailsRun(t *testing.T) {
	wfRepo := enginetest.NewFakeWorkflowRepo()
	runRepo := &failAppendRunRepo{FakeRunRepo: enginetest.NewFakeRunRepo(), err: errors.New("append boom")}
	ntReg := enginetest.NewNodeTypeRegistry()
	exReg := executor.NewRegistry()
	enginetest.RegisterBuiltins(exReg)
	eng := engine.New(wfRepo, runRepo, ntReg, exReg, executor.ExecServices{Logger: enginetest.MockLogger{}}, engine.Config{})

	dsl := enginetest.NewDSL().
		Start("s").
		End("e").
		Edge("s", workflow.PortDefault, "e").
		Build()
	wfRepo.PutVersion(releasedVersion("v1", dsl))

	out, err := eng.Start(context.Background(), engine.StartInput{VersionID: "v1", TriggerKind: run.TriggerKindManual, TriggerPayload: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != run.RunStatusFailed {
		t.Fatalf("status: %v", out.Status)
	}
	if out.Error == nil || out.Error.Code != run.RunErrCodePersistence {
		t.Fatalf("error: %+v", out.Error)
	}
}

type delayedResolvedRunRepo struct {
	*enginetest.FakeRunRepo
	delay time.Duration
}

func (d *delayedResolvedRunRepo) SaveNodeRunResolved(ctx context.Context, runID, nodeRunID string, resolvedConfig, resolvedInputs json.RawMessage) error {
	time.Sleep(d.delay)
	return d.FakeRunRepo.SaveNodeRunResolved(ctx, runID, nodeRunID, resolvedConfig, resolvedInputs)
}

type failAppendRunRepo struct {
	*enginetest.FakeRunRepo
	err error
}

func (f *failAppendRunRepo) AppendNodeRun(context.Context, string, *run.NodeRun) error {
	return f.err
}

func releasedVersion(id string, dsl workflow.WorkflowDSL) *workflow.WorkflowVersion {
	return &workflow.WorkflowVersion{ID: id, DefinitionID: "d1", Version: 1, State: workflow.VersionStateRelease, DSL: dsl}
}
