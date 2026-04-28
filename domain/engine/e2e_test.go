//go:build e2e

package engine_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/shinya/shineflow/domain/engine"
	"github.com/shinya/shineflow/domain/engine/enginetest"
	"github.com/shinya/shineflow/domain/executor"
	"github.com/shinya/shineflow/domain/nodetype"
	domainrun "github.com/shinya/shineflow/domain/run"
	"github.com/shinya/shineflow/domain/workflow"
	storagerun "github.com/shinya/shineflow/infrastructure/storage/run"
	"github.com/shinya/shineflow/infrastructure/storage/storagetest"
	storageworkflow "github.com/shinya/shineflow/infrastructure/storage/workflow"
)

func e2eEngine(t *testing.T, services executor.ExecServices) (context.Context, *engine.Engine, domainrun.WorkflowRunRepository, workflow.WorkflowRepository) {
	t.Helper()
	ctx := storagetest.Setup(t)
	wfRepo := storageworkflow.NewWorkflowRepository()
	runRepo := storagerun.NewWorkflowRunRepository()
	ntReg := nodetype.NewBuiltinRegistry()
	exReg := executor.NewRegistry()
	enginetest.RegisterBuiltins(exReg)
	if services.Logger == nil {
		services.Logger = enginetest.MockLogger{}
	}
	eng := engine.New(wfRepo, runRepo, ntReg, exReg, services, engine.Config{RunTimeout: 5 * time.Second})
	return ctx, eng, runRepo, wfRepo
}

func TestE2ESunshine(t *testing.T) {
	ctx, eng, runRepo, wfRepo := e2eEngine(t, executor.ExecServices{})
	dsl := enginetest.NewDSL().
		Start("s").
		End("e").
		Edge("s", workflow.PortDefault, "e").
		Build()
	v := seedRelease(t, ctx, wfRepo, dsl)

	out, err := eng.Start(ctx, engine.StartInput{VersionID: v.ID, TriggerKind: domainrun.TriggerKindManual, TriggerPayload: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != domainrun.RunStatusSuccess {
		t.Fatalf("status: %v", out.Status)
	}
	nrs, err := runRepo.ListNodeRuns(ctx, out.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(nrs) != 2 {
		t.Fatalf("node runs: %d", len(nrs))
	}
}

func TestE2EBranchJoin(t *testing.T) {
	ctx, eng, runRepo, wfRepo := e2eEngine(t, executor.ExecServices{})
	dsl := enginetest.NewDSL().
		Start("s").
		NodeWithInputs("if1", nodetype.BuiltinIf, `{"operator":"eq"}`, map[string]workflow.ValueSource{
			"left":  {Kind: workflow.ValueKindLiteral, Value: 1},
			"right": {Kind: workflow.ValueKindLiteral, Value: 1},
		}).
		NodeWithInputs("a", nodetype.BuiltinSetVariable, `{"name":"branch"}`, map[string]workflow.ValueSource{
			"value": {Kind: workflow.ValueKindLiteral, Value: "a"},
		}).
		NodeWithInputs("b", nodetype.BuiltinSetVariable, `{"name":"branch"}`, map[string]workflow.ValueSource{
			"value": {Kind: workflow.ValueKindLiteral, Value: "b"},
		}).
		Node("j", nodetype.BuiltinJoin, `{"mode":"any"}`).
		End("e").
		Edge("s", workflow.PortDefault, "if1").
		Edge("if1", nodetype.PortIfTrue, "a").
		Edge("if1", nodetype.PortIfFalse, "b").
		Edge("a", workflow.PortDefault, "j").
		Edge("b", workflow.PortDefault, "j").
		Edge("j", workflow.PortDefault, "e").
		Build()
	v := seedRelease(t, ctx, wfRepo, dsl)

	out, err := eng.Start(ctx, engine.StartInput{VersionID: v.ID, TriggerKind: domainrun.TriggerKindManual, TriggerPayload: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != domainrun.RunStatusSuccess {
		t.Fatalf("status: %v", out.Status)
	}
	statuses := nodeStatuses(t, ctx, runRepo, out.ID)
	if statuses["a"] != domainrun.NodeRunStatusSuccess {
		t.Fatalf("a status: %v", statuses["a"])
	}
	if statuses["b"] != domainrun.NodeRunStatusSkipped {
		t.Fatalf("b status: %v", statuses["b"])
	}
	if statuses["j"] != domainrun.NodeRunStatusSuccess {
		t.Fatalf("j status: %v", statuses["j"])
	}
}

func TestE2EMultiEndFirstWins(t *testing.T) {
	httpMock := &enginetest.MockHTTPClient{
		OnDo: func(ctx context.Context, _ executor.HTTPRequest) (executor.HTTPResponse, error) {
			<-ctx.Done()
			return executor.HTTPResponse{}, ctx.Err()
		},
	}
	ctx, eng, runRepo, wfRepo := e2eEngine(t, executor.ExecServices{HTTPClient: httpMock})
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
	v := seedRelease(t, ctx, wfRepo, dsl)

	out, err := eng.Start(ctx, engine.StartInput{VersionID: v.ID, TriggerKind: domainrun.TriggerKindManual, TriggerPayload: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != domainrun.RunStatusSuccess {
		t.Fatalf("status: %v", out.Status)
	}
	if out.EndNodeID == nil || *out.EndNodeID != "eA" {
		t.Fatalf("EndNodeID: %v", out.EndNodeID)
	}
	statuses := nodeStatuses(t, ctx, runRepo, out.ID)
	if statuses["slow"] != domainrun.NodeRunStatusCancelled {
		t.Fatalf("slow status: %v", statuses["slow"])
	}
}

func TestE2ERetryFallback(t *testing.T) {
	llm := &enginetest.MockLLMClient{
		OnComplete: func(context.Context, executor.LLMRequest) (executor.LLMResponse, error) {
			return executor.LLMResponse{}, errors.New("simulated provider error")
		},
	}
	ctx, eng, runRepo, wfRepo := e2eEngine(t, executor.ExecServices{LLMClient: llm})
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
				MaxRetries:     2,
				RetryDelay:     time.Millisecond,
				OnFinalFail:    workflow.FailStrategyFallback,
				FallbackOutput: workflow.FallbackOutput{Port: workflow.PortDefault, Output: map[string]any{"text": "sorry"}},
			}
		}
	}
	v := seedRelease(t, ctx, wfRepo, dsl)

	out, err := eng.Start(ctx, engine.StartInput{VersionID: v.ID, TriggerKind: domainrun.TriggerKindManual, TriggerPayload: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != domainrun.RunStatusSuccess {
		t.Fatalf("status: %v", out.Status)
	}
	nrs, err := runRepo.ListNodeRuns(ctx, out.ID)
	if err != nil {
		t.Fatal(err)
	}
	llmRuns := 0
	var fallback *domainrun.NodeRun
	for _, nr := range nrs {
		if nr.NodeID == "llm1" {
			llmRuns++
			if nr.FallbackApplied {
				fallback = nr
				if nr.Status != domainrun.NodeRunStatusFailed {
					t.Fatalf("fallback status: %v", nr.Status)
				}
			}
		}
	}
	if llmRuns != 3 {
		t.Fatalf("attempts: %d", llmRuns)
	}
	if fallback == nil {
		t.Fatal("fallback missing")
	}
	var outMap map[string]any
	if err := json.Unmarshal(fallback.Output, &outMap); err != nil {
		t.Fatal(err)
	}
	if outMap["text"] != "sorry" {
		t.Fatalf("fallback output: %s", string(fallback.Output))
	}
}

func seedRelease(t *testing.T, ctx context.Context, repo workflow.WorkflowRepository, dsl workflow.WorkflowDSL) *workflow.WorkflowVersion {
	t.Helper()
	now := time.Now().UTC()
	def := &workflow.WorkflowDefinition{
		ID:        uuid.NewString(),
		Name:      "engine e2e",
		CreatedBy: "test",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := repo.CreateDefinition(ctx, def); err != nil {
		t.Fatal(err)
	}
	v, err := repo.SaveVersion(ctx, def.ID, dsl, 0)
	if err != nil {
		t.Fatal(err)
	}
	pub, err := repo.PublishVersion(ctx, v.ID, "test")
	if err != nil {
		t.Fatal(err)
	}
	return pub
}

func nodeStatuses(t *testing.T, ctx context.Context, repo domainrun.WorkflowRunRepository, runID string) map[string]domainrun.NodeRunStatus {
	t.Helper()
	nrs, err := repo.ListNodeRuns(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]domainrun.NodeRunStatus{}
	for _, nr := range nrs {
		out[nr.NodeID] = nr.Status
	}
	return out
}
