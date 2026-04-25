package plugin

import (
	"encoding/json"
	"time"
)

// McpTransport 决定 McpServer.Config 应反序列化成什么。
type McpTransport string

const (
	McpTransportStdio McpTransport = "stdio"
	McpTransportHTTP  McpTransport = "http"
	McpTransportSSE   McpTransport = "sse"
)

// McpServer 是一个对外的 MCP 服务实例；其下可同步出多个 McpTool。
type McpServer struct {
	ID   string
	Name string

	Transport McpTransport
	// Config 按 Transport 解码：stdio → {Command, Args, Env}；http/sse → {URL, ...}
	Config json.RawMessage

	CredentialID *string

	Enabled       bool
	LastSyncedAt  *time.Time
	LastSyncError *string

	CreatedBy string
	CreatedAt time.Time
	UpdatedAt time.Time
}
