package workflow

import "encoding/json"

// 保留端口名 —— 所有 NodeType 都不能用这两个名字以外的语义。
const (
	PortDefault = "default"
	PortError   = "error"
)

// 当前 DSL JSON 的 schema 版本；序列化时写到 WorkflowDSL JSON 根上。
// 升级 DSL 物理结构时递增并实现 schema migration。
const DSLSchemaVersion = "1"

// Node 是 DSL 中的节点实例。
//   - TypeKey  指向 NodeTypeRegistry 中的某个 NodeType（如 "builtin.llm"、"plugin.http.<id>"）
//   - TypeVer  绑定到具体 NodeType 版本，便于 NodeType 演进时老 DSL 不破
//   - Inputs   key 是 NodeType.InputSchema 中 PortSpec.ID（不是 Name！）
//   - Config   是符合 NodeType.ConfigSchema 的 JSON；其中字符串字段允许 {{var}} 模板
type Node struct {
	ID      string                 `json:"id"`
	TypeKey string                 `json:"type_key"`
	TypeVer string                 `json:"type_ver"`
	Name    string                 `json:"name"`

	Config json.RawMessage        `json:"config,omitempty"`
	Inputs map[string]ValueSource `json:"inputs,omitempty"`

	ErrorPolicy *ErrorPolicy `json:"error_policy,omitempty"`
	UI          NodeUI       `json:"ui"`
}

// Edge 是节点之间的控制流边。本系统采用 context-passing 模型，目标节点直接读共享变量表，
// 因此不需要 ToPort，只声明源节点的输出端口。
//   - FromPort 取值来自源节点 NodeType.Ports（含保留端口 default / error）
type Edge struct {
	ID       string `json:"id"`
	From     string `json:"from"`
	FromPort string `json:"from_port"`
	To       string `json:"to"`
}

// WorkflowDSL 是工作流的"纯图"形态：不含名称 / 描述 / 版本号 / 时间戳。
type WorkflowDSL struct {
	Nodes []Node `json:"nodes"`
	Edges []Edge `json:"edges"`
}
