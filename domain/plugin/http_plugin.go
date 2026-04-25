// Package plugin 定义 HTTP 插件 / MCP Server / MCP Tool 三类外部能力的聚合。
package plugin

import (
	"time"

	"github.com/shinya/shineflow/domain/workflow"
)

// HttpPlugin 描述一个由用户配置的通用 HTTP 能力。
// 在 NodeTypeRegistry 中会被投影成 NodeType "plugin.http.<HttpPlugin.ID>"。
type HttpPlugin struct {
	ID          string
	Name        string
	Description string

	// 请求构造
	Method       string
	URL          string
	Headers      map[string]string
	QueryParams  map[string]string
	BodyTemplate string

	// 认证
	AuthKind     string // "none" | "api_key" | "bearer" | "basic"
	CredentialID *string

	// 端口契约
	InputSchema  []workflow.PortSpec
	OutputSchema []workflow.PortSpec

	// 响应映射：OutputSchema 端口名 → JSONPath；未映射的端口尝试按同名顶层字段取
	ResponseMapping map[string]string

	Enabled   bool
	CreatedBy string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// HttpPlugin.AuthKind 允许的字面量。
const (
	HttpAuthNone   = "none"
	HttpAuthAPIKey = "api_key"
	HttpAuthBearer = "bearer"
	HttpAuthBasic  = "basic"
)
