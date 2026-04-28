package nodetype

import "github.com/shinya/shineflow/domain/workflow"

var ifType = &NodeType{
	Key:         BuiltinIf,
	Version:     NodeTypeVersion1,
	Name:        "If",
	Description: "二元条件判断。触发 true / false / error。",
	Category:    CategoryControl,
	Builtin:     true,
	Ports:       []string{PortIfTrue, PortIfFalse, workflow.PortError},
}
