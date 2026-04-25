// Package workflow 定义工作流设计时（DSL）的核心值对象与聚合。
//
// 本包不依赖任何其他 domain 子包；其他子包（nodetype、plugin、check 等）反向依赖本包。
package workflow

// PortSpec 描述某个 NodeType / 插件输入或输出端口的静态契约。
// ID 是 DSL 中跨节点引用使用的稳定标识；Name 仅用于展示。
type PortSpec struct {
	ID       string
	Name     string
	Type     SchemaType
	Required bool
	Desc     string
}

// SchemaType 是 PortSpec.Type 使用的最小 JSON Schema 子集，支持嵌套。
//   - Type == "object" 时使用 Properties
//   - Type == "array"  时使用 Items
//   - Enum 仅对 string / number / integer 生效
type SchemaType struct {
	Type       string
	Properties map[string]*SchemaType
	Items      *SchemaType
	Enum       []any
}

// 允许的 SchemaType.Type 字面量。
const (
	SchemaTypeString  = "string"
	SchemaTypeNumber  = "number"
	SchemaTypeInteger = "integer"
	SchemaTypeBoolean = "boolean"
	SchemaTypeObject  = "object"
	SchemaTypeArray   = "array"
	SchemaTypeAny     = "any"
)
