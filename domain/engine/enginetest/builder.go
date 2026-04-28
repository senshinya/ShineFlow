package enginetest

import (
	"encoding/json"

	"github.com/shinya/shineflow/domain/nodetype"
	"github.com/shinya/shineflow/domain/workflow"
)

// DSLBuilder 提供测试 DSL 的链式构造器。
type DSLBuilder struct {
	dsl  workflow.WorkflowDSL
	next int
}

func NewDSL() *DSLBuilder { return &DSLBuilder{} }

func (b *DSLBuilder) Start(id string) *DSLBuilder {
	b.dsl.Nodes = append(b.dsl.Nodes, workflow.Node{ID: id, TypeKey: nodetype.BuiltinStart})
	return b
}

func (b *DSLBuilder) End(id string) *DSLBuilder {
	b.dsl.Nodes = append(b.dsl.Nodes, workflow.Node{ID: id, TypeKey: nodetype.BuiltinEnd})
	return b
}

func (b *DSLBuilder) Node(id, typeKey string, config string) *DSLBuilder {
	b.dsl.Nodes = append(b.dsl.Nodes, workflow.Node{ID: id, TypeKey: typeKey, Config: json.RawMessage(config)})
	return b
}

func (b *DSLBuilder) NodeWithInputs(id, typeKey string, config string, inputs map[string]workflow.ValueSource) *DSLBuilder {
	b.dsl.Nodes = append(b.dsl.Nodes, workflow.Node{
		ID: id, TypeKey: typeKey,
		Config: json.RawMessage(config), Inputs: inputs,
	})
	return b
}

func (b *DSLBuilder) Edge(from, port, to string) *DSLBuilder {
	b.next++
	b.dsl.Edges = append(b.dsl.Edges, workflow.Edge{
		ID: itoa(b.next), From: from, FromPort: port, To: to,
	})
	return b
}

func (b *DSLBuilder) Build() workflow.WorkflowDSL { return b.dsl }

func itoa(i int) string {
	const digits = "0123456789"
	if i == 0 {
		return "edge-0"
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = digits[i%10]
		i /= 10
	}
	return "edge-" + string(buf[pos:])
}
