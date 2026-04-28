package nodetype

import "github.com/shinya/shineflow/domain/workflow"

// 输出字段是动态的（{<cfg.Name>: value}），控制端口固定为 default。
var setVariableType = &NodeType{
	Key:         BuiltinSetVariable,
	Version:     NodeTypeVersion1,
	Name:        "Set Variable",
	Description: "将 Inputs.value 写入 vars.<cfg.Name>。",
	Category:    CategoryBasic,
	Builtin:     true,
	Ports:       []string{workflow.PortDefault},
}
