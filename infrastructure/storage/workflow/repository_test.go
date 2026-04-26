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
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("expected empty, got %d", len(list))
	}
}

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
	v1, _ := repo.SaveVersion(ctx, d.ID, minimalValidDSL(), 0)
	if _, err := repo.PublishVersion(ctx, v1.ID, "u_alice"); err != nil {
		t.Fatalf("publish v1: %v", err)
	}

	v2, err := repo.SaveVersion(ctx, d.ID, domainworkflow.WorkflowDSL{}, 0)
	if err != nil { t.Fatalf("save after release: %v", err) }
	if v2.Version != 2 || v2.Revision != 1 || v2.State != domainworkflow.VersionStateDraft {
		t.Fatalf("expected v=2 rev=1 draft, got v=%d rev=%d state=%s", v2.Version, v2.Revision, v2.State)
	}
}

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
	v2, _ := repo.SaveVersion(ctx, d.ID, minimalValidDSL(), 0)
	_ = v2

	// 试图 publish 老的 v1（已 release，幂等）→ OK
	if _, err := repo.PublishVersion(ctx, v1.ID, "u_alice"); err != nil {
		t.Fatalf("re-publish v1 (idempotent) should succeed: %v", err)
	}
}

// 注：PublishVersion 不再在 repo 内跑 DSL 校验（迁到 application 层）。
// validator.ValidateForPublish 的覆盖见 domain/validator/validator_test.go。

// minimalValidDSL 是后续 SaveVersion / PublishVersion 测试常用的最小 DSL：start → end。
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

func TestDiscardDraft_DeletesDraftAndClearsPointer(t *testing.T) {
	ctx := storagetest.Setup(t)
	repo := storageworkflow.NewWorkflowRepository()
	d := newDef(t)
	_ = repo.CreateDefinition(ctx, d)
	v, _ := repo.SaveVersion(ctx, d.ID, domainworkflow.WorkflowDSL{}, 0)

	if err := repo.DiscardDraft(ctx, d.ID); err != nil {
		t.Fatalf("discard: %v", err)
	}

	_, err := repo.GetVersion(ctx, v.ID)
	if !errors.Is(err, domainworkflow.ErrVersionNotFound) {
		t.Fatalf("expected NotFound after discard, got: %v", err)
	}
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
