package workflow

// ValueKind 区分 ValueSource 的求值策略。
type ValueKind string

const (
	ValueKindLiteral  ValueKind = "literal"
	ValueKindRef      ValueKind = "ref"
	ValueKindTemplate ValueKind = "template"
)

// ValueSource 是 Node.Inputs 中每个输入端口的取值描述。
//   - Kind == ValueKindLiteral  → Value 直接是字面量
//   - Kind == ValueKindRef      → Value 应能解为 RefValue
//   - Kind == ValueKindTemplate → Value 是含 {{var}} 的模板字符串
type ValueSource struct {
	Kind  ValueKind
	Value any
}

// RefValue 引用上游某个节点输出端口的值（可选深路径）。
//   - NodeID  指向 DSL 内某个 Node.ID
//   - PortID  指向该节点 OutputSchema 中某个 PortSpec.ID
//   - Path    在 object 类型 Output 上的深路径，如 "data.voice_url"；可为空
//   - Name    冗余的端口显示名，仅给前端读，不参与运行时解析
type RefValue struct {
	NodeID string
	PortID string
	Path   string
	Name   string
}
