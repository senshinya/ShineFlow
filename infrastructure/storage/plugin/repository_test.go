package plugin_test

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	domainplugin "github.com/shinya/shineflow/domain/plugin"
	storageplugin "github.com/shinya/shineflow/infrastructure/storage/plugin"
	"github.com/shinya/shineflow/infrastructure/storage/storagetest"
)

// ============== HttpPlugin ==============

func newHttpPlugin(t *testing.T) *domainplugin.HttpPlugin {
	t.Helper()
	return &domainplugin.HttpPlugin{
		ID:        uuid.NewString(),
		Name:      "weather-api",
		Method:    "GET",
		URL:       "https://api.example.com/weather",
		AuthKind:  domainplugin.HttpAuthNone,
		Enabled:   true,
		CreatedBy: "u",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
}

func TestHttpPlugin_CreateAndGet(t *testing.T) {
	ctx := storagetest.Setup(t)
	repo := storageplugin.NewHttpPluginRepository()
	p := newHttpPlugin(t)
	if err := repo.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	got, err := repo.Get(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.URL != p.URL {
		t.Fatalf("url: %s", got.URL)
	}
}

func TestHttpPlugin_GetNotFound(t *testing.T) {
	ctx := storagetest.Setup(t)
	repo := storageplugin.NewHttpPluginRepository()
	_, err := repo.Get(ctx, uuid.NewString())
	if !errors.Is(err, domainplugin.ErrHttpPluginNotFound) {
		t.Fatalf("expected ErrHttpPluginNotFound: %v", err)
	}
}

func TestHttpPlugin_DeleteSoft_NameReusable(t *testing.T) {
	ctx := storagetest.Setup(t)
	repo := storageplugin.NewHttpPluginRepository()
	p := newHttpPlugin(t)
	_ = repo.Create(ctx, p)
	if err := repo.Delete(ctx, p.ID); err != nil {
		t.Fatal(err)
	}

	p2 := newHttpPlugin(t)
	p2.Name = p.Name
	if err := repo.Create(ctx, p2); err != nil {
		t.Fatalf("expected reusable name after soft delete: %v", err)
	}
}

func TestHttpPlugin_List_FilterByEnabled(t *testing.T) {
	ctx := storagetest.Setup(t)
	repo := storageplugin.NewHttpPluginRepository()
	on, off := newHttpPlugin(t), newHttpPlugin(t)
	off.Enabled = false
	off.Name = "off-one"
	_ = repo.Create(ctx, on)
	_ = repo.Create(ctx, off)
	list, err := repo.List(ctx, domainplugin.HttpPluginFilter{EnabledOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != on.ID {
		t.Fatalf("filter wrong: %+v", list)
	}
}

// ============== McpServer ==============

func newMcpServer(t *testing.T) *domainplugin.McpServer {
	t.Helper()
	return &domainplugin.McpServer{
		ID:        uuid.NewString(),
		Name:      "echo",
		Transport: domainplugin.McpTransportStdio,
		Config:    json.RawMessage(`{"command":"echo"}`),
		Enabled:   true,
		CreatedBy: "u",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
}

func TestMcpServer_CreateAndGet(t *testing.T) {
	ctx := storagetest.Setup(t)
	repo := storageplugin.NewMcpServerRepository()
	s := newMcpServer(t)
	if err := repo.Create(ctx, s); err != nil {
		t.Fatal(err)
	}
	got, err := repo.Get(ctx, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Transport != domainplugin.McpTransportStdio {
		t.Fatalf("transport: %s", got.Transport)
	}
}

func TestMcpServer_GetNotFound(t *testing.T) {
	ctx := storagetest.Setup(t)
	repo := storageplugin.NewMcpServerRepository()
	_, err := repo.Get(ctx, uuid.NewString())
	if !errors.Is(err, domainplugin.ErrMcpServerNotFound) {
		t.Fatalf("expected ErrMcpServerNotFound: %v", err)
	}
}

// ============== McpTool ==============

func newMcpTool(t *testing.T, serverID, name string) *domainplugin.McpTool {
	t.Helper()
	return &domainplugin.McpTool{
		ID:             uuid.NewString(),
		ServerID:       serverID,
		Name:           name,
		InputSchemaRaw: json.RawMessage(`{}`),
		Enabled:        true,
		SyncedAt:       time.Now().UTC(),
	}
}

func TestMcpTool_GetByServerAndName(t *testing.T) {
	ctx := storagetest.Setup(t)
	serverRepo := storageplugin.NewMcpServerRepository()
	toolRepo := storageplugin.NewMcpToolRepository()
	s := newMcpServer(t)
	_ = serverRepo.Create(ctx, s)
	tt := newMcpTool(t, s.ID, "echo_tool")
	_ = toolRepo.UpsertAll(ctx, s.ID, []*domainplugin.McpTool{tt})

	got, err := toolRepo.GetByServerAndName(ctx, s.ID, "echo_tool")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "echo_tool" {
		t.Fatalf("name: %s", got.Name)
	}
}

func TestMcpTool_GetByServerAndName_NotFound(t *testing.T) {
	ctx := storagetest.Setup(t)
	toolRepo := storageplugin.NewMcpToolRepository()
	_, err := toolRepo.GetByServerAndName(ctx, uuid.NewString(), "ghost")
	if !errors.Is(err, domainplugin.ErrMcpToolNotFound) {
		t.Fatalf("expected ErrMcpToolNotFound: %v", err)
	}
}

func TestMcpTool_ListByServer(t *testing.T) {
	ctx := storagetest.Setup(t)
	serverRepo := storageplugin.NewMcpServerRepository()
	toolRepo := storageplugin.NewMcpToolRepository()
	s := newMcpServer(t)
	_ = serverRepo.Create(ctx, s)
	_ = toolRepo.UpsertAll(ctx, s.ID, []*domainplugin.McpTool{
		newMcpTool(t, s.ID, "a"), newMcpTool(t, s.ID, "b"),
	})

	list, err := toolRepo.ListByServer(ctx, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("len: %d", len(list))
	}
}

func TestMcpTool_SetEnabled(t *testing.T) {
	ctx := storagetest.Setup(t)
	serverRepo := storageplugin.NewMcpServerRepository()
	toolRepo := storageplugin.NewMcpToolRepository()
	s := newMcpServer(t)
	_ = serverRepo.Create(ctx, s)
	tt := newMcpTool(t, s.ID, "x")
	_ = toolRepo.UpsertAll(ctx, s.ID, []*domainplugin.McpTool{tt})

	got, _ := toolRepo.GetByServerAndName(ctx, s.ID, "x")
	if err := toolRepo.SetEnabled(ctx, got.ID, false); err != nil {
		t.Fatal(err)
	}
	got2, _ := toolRepo.GetByServerAndName(ctx, s.ID, "x")
	if got2.Enabled {
		t.Fatal("expected disabled")
	}
}

func TestMcpTool_UpsertAll_PreservesIDsForExisting(t *testing.T) {
	ctx := storagetest.Setup(t)
	serverRepo := storageplugin.NewMcpServerRepository()
	toolRepo := storageplugin.NewMcpToolRepository()
	s := newMcpServer(t)
	_ = serverRepo.Create(ctx, s)

	tt1 := newMcpTool(t, s.ID, "stable")
	_ = toolRepo.UpsertAll(ctx, s.ID, []*domainplugin.McpTool{tt1})
	got1, _ := toolRepo.GetByServerAndName(ctx, s.ID, "stable")
	originalID := got1.ID

	// 第二次同步：stable 仍在（新 ID 但应被忽略），新增 fresh
	tt2 := newMcpTool(t, s.ID, "stable")
	tt3 := newMcpTool(t, s.ID, "fresh")
	_ = toolRepo.UpsertAll(ctx, s.ID, []*domainplugin.McpTool{tt2, tt3})

	got, _ := toolRepo.GetByServerAndName(ctx, s.ID, "stable")
	if got.ID != originalID {
		t.Fatalf("stable tool ID should be preserved: got %s, want %s", got.ID, originalID)
	}
}

func TestMcpTool_UpsertAll_RemovesMissing(t *testing.T) {
	ctx := storagetest.Setup(t)
	serverRepo := storageplugin.NewMcpServerRepository()
	toolRepo := storageplugin.NewMcpToolRepository()
	s := newMcpServer(t)
	_ = serverRepo.Create(ctx, s)

	_ = toolRepo.UpsertAll(ctx, s.ID, []*domainplugin.McpTool{
		newMcpTool(t, s.ID, "a"), newMcpTool(t, s.ID, "b"),
	})
	_ = toolRepo.UpsertAll(ctx, s.ID, []*domainplugin.McpTool{
		newMcpTool(t, s.ID, "a"),
	})
	list, _ := toolRepo.ListByServer(ctx, s.ID)
	if len(list) != 1 || list[0].Name != "a" {
		t.Fatalf("expected only [a], got %+v", list)
	}
}
