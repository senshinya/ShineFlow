// Package nodetype 定义"节点类型"的统一目录。
//
// 关键设计：
//   - NodeType 不对应物理表，由 NodeTypeRegistry 现场合成
//   - 内置节点 / HttpPlugin / McpTool 投影出来的 NodeType 对调用方完全同构
//   - 前端节点面板、引擎执行器都只认 NodeType.Key
package nodetype

import (
	"encoding/json"

	"github.com/shinya/shineflow/domain/workflow"
)

// NodeType 是节点类型的元信息。
//
//   - Key      全局唯一，模式见 §7.3：builtin.* / plugin.http.<id> / plugin.mcp.<server>.<tool>
//   - Version  NodeType 自身契约的版本（与 WorkflowVersion.Version 不同）；
//              v1 内统一填 NodeTypeVersion1
//   - Ports    输出控制端口列表；默认 [PortDefault, PortError]
type NodeType struct {
	Key         string
	Version     string
	Name        string
	Description string
	Category    string
	Builtin     bool

	ConfigSchema json.RawMessage
	InputSchema  []workflow.PortSpec
	OutputSchema []workflow.PortSpec
	Ports        []string
}

// NodeType 自身契约的初始版本号，v1 内全部 NodeType 都填这个。
const NodeTypeVersion1 = "1"

// NodeType.Category 允许的字面量。
const (
	CategoryAI      = "AI"
	CategoryTool    = "Tool"
	CategoryControl = "Control"
	CategoryBasic   = "Basic"
)
