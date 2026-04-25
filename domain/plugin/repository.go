package plugin

import (
	"context"
	"errors"
)

var (
	ErrHttpPluginNotFound = errors.New("plugin: http plugin not found")
	ErrMcpServerNotFound  = errors.New("plugin: mcp server not found")
	ErrMcpToolNotFound    = errors.New("plugin: mcp tool not found")
)

// HttpPluginFilter 是 ListHttpPlugins 的过滤参数。
type HttpPluginFilter struct {
	EnabledOnly bool
	CreatedBy   string
	Limit       int
	Offset      int
}

// McpServerFilter 是 ListMcpServers 的过滤参数。
type McpServerFilter struct {
	EnabledOnly bool
	Limit       int
	Offset      int
}

// HttpPluginRepository 是 HttpPlugin 聚合的存储契约。
type HttpPluginRepository interface {
	Create(ctx context.Context, p *HttpPlugin) error
	Get(ctx context.Context, id string) (*HttpPlugin, error)
	List(ctx context.Context, filter HttpPluginFilter) ([]*HttpPlugin, error)
	Update(ctx context.Context, p *HttpPlugin) error
	Delete(ctx context.Context, id string) error
}

// McpServerRepository 是 McpServer 聚合的存储契约。
type McpServerRepository interface {
	Create(ctx context.Context, s *McpServer) error
	Get(ctx context.Context, id string) (*McpServer, error)
	List(ctx context.Context, filter McpServerFilter) ([]*McpServer, error)
	Update(ctx context.Context, s *McpServer) error
	Delete(ctx context.Context, id string) error
}

// McpToolRepository 是 McpTool 实体的存储契约。
//   - UpsertAll 在某个 server 一次同步后整体覆盖该 server 名下的 tools
//   - SetEnabled 仅切换 Enabled 字段
type McpToolRepository interface {
	GetByServerAndName(ctx context.Context, serverID, name string) (*McpTool, error)
	ListByServer(ctx context.Context, serverID string) ([]*McpTool, error)
	UpsertAll(ctx context.Context, serverID string, tools []*McpTool) error
	SetEnabled(ctx context.Context, id string, enabled bool) error
}
