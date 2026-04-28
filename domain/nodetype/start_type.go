package nodetype

import "github.com/shinya/shineflow/domain/workflow"

var startType = &NodeType{
	Key:         BuiltinStart,
	Version:     NodeTypeVersion1,
	Name:        "Start",
	Description: "工作流入口。触发字段通过 trigger.<key>（Symbols）读取。",
	Category:    CategoryControl,
	Builtin:     true,
	Ports:       []string{workflow.PortDefault},
}
