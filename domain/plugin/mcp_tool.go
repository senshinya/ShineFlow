package plugin

import (
	"encoding/json"
	"time"
)

// McpTool 是某个 McpServer 同步出的 tool 元数据。
// 由系统按 MCP 协议同步而来，用户只能 enable / disable，不能直接 CRUD。
type McpTool struct {
	ID          string
	ServerID    string
	Name        string
	Description string

	// MCP 返回的原生 JSON Schema；在投影成 NodeType.InputSchema 时按 §7.5 降维
	InputSchemaRaw json.RawMessage

	Enabled  bool
	SyncedAt time.Time
}
