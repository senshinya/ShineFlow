package cron_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	domaincron "github.com/shinya/shineflow/domain/cron"
	domainworkflow "github.com/shinya/shineflow/domain/workflow"
	storagecron "github.com/shinya/shineflow/infrastructure/storage/cron"
	"github.com/shinya/shineflow/infrastructure/storage/storagetest"
	storageworkflow "github.com/shinya/shineflow/infrastructure/storage/workflow"
)

func seedDef(t *testing.T, ctx context.Context) string {
	t.Helper()
	wfRepo := storageworkflow.NewWorkflowRepository()
	d := &domainworkflow.WorkflowDefinition{
		ID: uuid.NewString(), Name: "d", CreatedBy: "u",
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	_ = wfRepo.CreateDefinition(ctx, d)
	return d.ID
}

func newCronJob(t *testing.T, defID string) *domaincron.CronJob {
	t.Helper()
	return &domaincron.CronJob{
		ID:           uuid.NewString(),
		DefinitionID: defID,
		Name:         "daily",
		Expression:   "0 0 * * *",
		Timezone:     "Asia/Shanghai",
		Enabled:      true,
		CreatedBy:    "u",
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
}

func TestCron_CreateAndGet(t *testing.T) {
	ctx := storagetest.Setup(t)
	repo := storagecron.NewCronJobRepository()
	j := newCronJob(t, seedDef(t, ctx))
	if err := repo.Create(ctx, j); err != nil { t.Fatal(err) }
	got, err := repo.Get(ctx, j.ID)
	if err != nil { t.Fatal(err) }
	if got.Name != "daily" { t.Fatalf("name: %s", got.Name) }
}

func TestCron_GetNotFound(t *testing.T) {
	ctx := storagetest.Setup(t)
	repo := storagecron.NewCronJobRepository()
	_, err := repo.Get(ctx, uuid.NewString())
	if !errors.Is(err, domaincron.ErrCronJobNotFound) {
		t.Fatalf("expected ErrCronJobNotFound: %v", err)
	}
}

func TestCron_Update(t *testing.T) {
	ctx := storagetest.Setup(t)
	repo := storagecron.NewCronJobRepository()
	j := newCronJob(t, seedDef(t, ctx))
	_ = repo.Create(ctx, j)
	j.Enabled = false
	j.UpdatedAt = time.Now().UTC()
	if err := repo.Update(ctx, j); err != nil { t.Fatal(err) }
	got, _ := repo.Get(ctx, j.ID)
	if got.Enabled { t.Fatal("expected disabled") }
}

func TestCron_DeleteSoft(t *testing.T) {
	ctx := storagetest.Setup(t)
	repo := storagecron.NewCronJobRepository()
	j := newCronJob(t, seedDef(t, ctx))
	_ = repo.Create(ctx, j)
	if err := repo.Delete(ctx, j.ID); err != nil { t.Fatal(err) }
	_, err := repo.Get(ctx, j.ID)
	if !errors.Is(err, domaincron.ErrCronJobNotFound) {
		t.Fatalf("expected NotFound after soft delete: %v", err)
	}
}

func TestCron_List_FilterByEnabled(t *testing.T) {
	ctx := storagetest.Setup(t)
	repo := storagecron.NewCronJobRepository()
	defID := seedDef(t, ctx)
	on, off := newCronJob(t, defID), newCronJob(t, defID)
	off.Enabled = false
	_ = repo.Create(ctx, on)
	_ = repo.Create(ctx, off)

	list, err := repo.List(ctx, domaincron.CronJobFilter{EnabledOnly: true})
	if err != nil { t.Fatal(err) }
	if len(list) != 1 || list[0].ID != on.ID {
		t.Fatalf("filter wrong: %+v", list)
	}
}

func TestCron_MarkFired(t *testing.T) {
	ctx := storagetest.Setup(t)
	repo := storagecron.NewCronJobRepository()
	j := newCronJob(t, seedDef(t, ctx))
	_ = repo.Create(ctx, j)

	now := time.Now().UTC()
	nextFire := now.Add(24 * time.Hour)
	runID := uuid.NewString()
	if err := repo.MarkFired(ctx, j.ID, now, nextFire, runID); err != nil { t.Fatal(err) }
	got, _ := repo.Get(ctx, j.ID)
	if got.LastFireAt == nil || !got.LastFireAt.Equal(now) {
		t.Fatalf("last_fire_at: %v", got.LastFireAt)
	}
	if got.LastRunID == nil || *got.LastRunID != runID {
		t.Fatalf("last_run_id: %v", got.LastRunID)
	}
}

func TestCron_ClaimDue(t *testing.T) {
	ctx := storagetest.Setup(t)
	repo := storagecron.NewCronJobRepository()
	defID := seedDef(t, ctx)

	past := time.Now().UTC().Add(-time.Minute)
	future := time.Now().UTC().Add(time.Hour)

	due := newCronJob(t, defID); due.NextFireAt = &past
	_ = repo.Create(ctx, due)
	notYet := newCronJob(t, defID); notYet.NextFireAt = &future
	_ = repo.Create(ctx, notYet)
	disabled := newCronJob(t, defID); disabled.NextFireAt = &past; disabled.Enabled = false
	_ = repo.Create(ctx, disabled)

	got, err := repo.ClaimDue(ctx, time.Now().UTC(), 10)
	if err != nil { t.Fatal(err) }
	if len(got) != 1 || got[0].ID != due.ID {
		t.Fatalf("claim wrong: len=%d ids=%v", len(got), got)
	}
}
