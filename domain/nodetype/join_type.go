package nodetype

import "github.com/shinya/shineflow/domain/workflow"

var joinType = &NodeType{
	Key:         BuiltinJoin,
	Version:     NodeTypeVersion1,
	Name:        "Join",
	Description: "多输入汇合。mode=any 表示竞速，mode=all 表示严格 AND-join。",
	Category:    CategoryControl,
	Builtin:     true,
	Ports:       []string{workflow.PortDefault},
}
