package run_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	domainrun "github.com/shinya/shineflow/domain/run"
	domainworkflow "github.com/shinya/shineflow/domain/workflow"
	storagerun "github.com/shinya/shineflow/infrastructure/storage/run"
	"github.com/shinya/shineflow/infrastructure/storage/storagetest"
	storageworkflow "github.com/shinya/shineflow/infrastructure/storage/workflow"
)

// seedReleasedVersion 工厂：把 def + release v 一次造好返回。
// run 必须挂在已 release 的 version 上（FK），所以 run 测试都靠这个 helper。
func seedReleasedVersion(t *testing.T, ctx context.Context, wf domainworkflow.WorkflowRepository) (
	*domainworkflow.WorkflowDefinition, *domainworkflow.WorkflowVersion,
) {
	t.Helper()
	d := &domainworkflow.WorkflowDefinition{
		ID: uuid.NewString(), Name: "d", CreatedBy: "u",
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	_ = wf.CreateDefinition(ctx, d)
	v, _ := wf.SaveVersion(ctx, d.ID, domainworkflow.WorkflowDSL{
		Nodes: []domainworkflow.Node{
			{ID: "n_start", TypeKey: "builtin.start", TypeVer: "1"},
			{ID: "n_end", TypeKey: "builtin.end", TypeVer: "1"},
		},
		Edges: []domainworkflow.Edge{
			{ID: "e1", From: "n_start", FromPort: domainworkflow.PortDefault, To: "n_end"},
		},
	}, 0)
	pub, _ := wf.PublishVersion(ctx, v.ID, "u")
	return d, pub
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

// ============== WorkflowRun ==============

func TestRun_CreateAndGet(t *testing.T) {
	ctx := storagetest.Setup(t)
	wfRepo := storageworkflow.NewWorkflowRepository()
	runRepo := storagerun.NewWorkflowRunRepository()
	d, v := seedReleasedVersion(t, ctx, wfRepo)
	r := newRun(t, d.ID, v.ID)
	if err := runRepo.Create(ctx, r); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := runRepo.Get(ctx, r.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
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
	d, v := seedReleasedVersion(t, ctx, wfRepo)
	_ = runRepo.Create(ctx, newRun(t, d.ID, v.ID))
	_ = runRepo.Create(ctx, newRun(t, d.ID, v.ID))
	list, err := runRepo.List(ctx, domainrun.RunFilter{DefinitionID: d.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2, got %d", len(list))
	}
}

func TestRun_UpdateStatus_PendingToRunning(t *testing.T) {
	ctx := storagetest.Setup(t)
	wfRepo := storageworkflow.NewWorkflowRepository()
	runRepo := storagerun.NewWorkflowRunRepository()
	d, v := seedReleasedVersion(t, ctx, wfRepo)
	r := newRun(t, d.ID, v.ID)
	_ = runRepo.Create(ctx, r)
	now := time.Now().UTC()
	err := runRepo.UpdateStatus(ctx, r.ID, domainrun.RunStatusRunning, domainrun.WithRunStartedAt(now))
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ := runRepo.Get(ctx, r.ID)
	if got.Status != domainrun.RunStatusRunning {
		t.Fatalf("status: %s", got.Status)
	}
	if got.StartedAt == nil {
		t.Fatal("started_at should be set")
	}
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

func TestRun_SaveEndResult(t *testing.T) {
	ctx := storagetest.Setup(t)
	wfRepo := storageworkflow.NewWorkflowRepository()
	runRepo := storagerun.NewWorkflowRunRepository()
	d, v := seedReleasedVersion(t, ctx, wfRepo)
	r := newRun(t, d.ID, v.ID)
	_ = runRepo.Create(ctx, r)
	out := json.RawMessage(`{"answer":42}`)
	if err := runRepo.SaveEndResult(ctx, r.ID, "n_end", out); err != nil {
		t.Fatal(err)
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
	if err := runRepo.SaveVars(ctx, r.ID, vars); err != nil {
		t.Fatal(err)
	}
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
	if err := runRepo.SaveError(ctx, r.ID, e); err != nil {
		t.Fatal(err)
	}
	got, _ := runRepo.Get(ctx, r.ID)
	if got.Error == nil || got.Error.Code != domainrun.RunErrCodeNodeExecFailed {
		t.Fatalf("error: %+v", got.Error)
	}
}

// ============== NodeRun ==============

func TestNodeRun_Append(t *testing.T) {
	ctx := storagetest.Setup(t)
	wfRepo := storageworkflow.NewWorkflowRepository()
	runRepo := storagerun.NewWorkflowRunRepository()
	d, v := seedReleasedVersion(t, ctx, wfRepo)
	r := newRun(t, d.ID, v.ID)
	_ = runRepo.Create(ctx, r)
	nr := newNodeRun(t, r.ID, 1)
	if err := runRepo.AppendNodeRun(ctx, r.ID, nr); err != nil {
		t.Fatalf("append: %v", err)
	}
	got, err := runRepo.GetNodeRun(ctx, r.ID, nr.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
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
	_ = runRepo.AppendNodeRun(ctx, r.ID, newNodeRun(t, r.ID, 1))
	err := runRepo.AppendNodeRun(ctx, r.ID, newNodeRun(t, r.ID, 1))
	if err == nil {
		t.Fatal("expected unique violation, got nil")
	}
}

func TestNodeRun_UpdateStatus(t *testing.T) {
	ctx := storagetest.Setup(t)
	wfRepo := storageworkflow.NewWorkflowRepository()
	runRepo := storagerun.NewWorkflowRunRepository()
	d, v := seedReleasedVersion(t, ctx, wfRepo)
	r := newRun(t, d.ID, v.ID)
	_ = runRepo.Create(ctx, r)
	nr := newNodeRun(t, r.ID, 1)
	_ = runRepo.AppendNodeRun(ctx, r.ID, nr)
	err := runRepo.UpdateNodeRunStatus(ctx, r.ID, nr.ID, domainrun.NodeRunStatusRunning,
		domainrun.WithNodeRunStartedAt(time.Now().UTC()))
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ := runRepo.GetNodeRun(ctx, r.ID, nr.ID)
	if got.Status != domainrun.NodeRunStatusRunning {
		t.Fatalf("status: %s", got.Status)
	}
}

func TestNodeRun_UpdateStatus_Illegal(t *testing.T) {
	ctx := storagetest.Setup(t)
	wfRepo := storageworkflow.NewWorkflowRepository()
	runRepo := storagerun.NewWorkflowRunRepository()
	d, v := seedReleasedVersion(t, ctx, wfRepo)
	r := newRun(t, d.ID, v.ID)
	_ = runRepo.Create(ctx, r)
	nr := newNodeRun(t, r.ID, 1)
	_ = runRepo.AppendNodeRun(ctx, r.ID, nr)
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
	r := newRun(t, d.ID, v.ID)
	_ = runRepo.Create(ctx, r)
	nr := newNodeRun(t, r.ID, 1)
	_ = runRepo.AppendNodeRun(ctx, r.ID, nr)
	out := json.RawMessage(`{"answer":42}`)
	if err := runRepo.SaveNodeRunOutput(ctx, r.ID, nr.ID, out, domainworkflow.PortDefault); err != nil {
		t.Fatal(err)
	}
	got, _ := runRepo.GetNodeRun(ctx, r.ID, nr.ID)
	if got.FiredPort != domainworkflow.PortDefault {
		t.Fatalf("fired_port: %s", got.FiredPort)
	}
}

func TestNodeRun_List(t *testing.T) {
	ctx := storagetest.Setup(t)
	wfRepo := storageworkflow.NewWorkflowRepository()
	runRepo := storagerun.NewWorkflowRunRepository()
	d, v := seedReleasedVersion(t, ctx, wfRepo)
	r := newRun(t, d.ID, v.ID)
	_ = runRepo.Create(ctx, r)
	_ = runRepo.AppendNodeRun(ctx, r.ID, newNodeRun(t, r.ID, 1))
	_ = runRepo.AppendNodeRun(ctx, r.ID, newNodeRun(t, r.ID, 2))
	list, err := runRepo.ListNodeRuns(ctx, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("got %d", len(list))
	}
}

func TestNodeRun_GetLatest(t *testing.T) {
	ctx := storagetest.Setup(t)
	wfRepo := storageworkflow.NewWorkflowRepository()
	runRepo := storagerun.NewWorkflowRunRepository()
	d, v := seedReleasedVersion(t, ctx, wfRepo)
	r := newRun(t, d.ID, v.ID)
	_ = runRepo.Create(ctx, r)
	_ = runRepo.AppendNodeRun(ctx, r.ID, newNodeRun(t, r.ID, 1))
	_ = runRepo.AppendNodeRun(ctx, r.ID, newNodeRun(t, r.ID, 2))
	nr3 := newNodeRun(t, r.ID, 3)
	_ = runRepo.AppendNodeRun(ctx, r.ID, nr3)
	got, err := runRepo.GetLatestNodeRun(ctx, r.ID, "n_llm")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != nr3.ID {
		t.Fatalf("expected latest %s, got %s", nr3.ID, got.ID)
	}
}
