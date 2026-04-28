package nodetype

import "github.com/shinya/shineflow/domain/workflow"

// switchType.Ports 只列静态端口；完整端口集由 validator/engine 按 Config.cases 动态计算。
var switchType = &NodeType{
	Key:         BuiltinSwitch,
	Version:     NodeTypeVersion1,
	Name:        "Switch",
	Description: "多分支分发。端口名来自用户定义的 case.name，加上 default/error。",
	Category:    CategoryControl,
	Builtin:     true,
	Ports:       []string{workflow.PortDefault, workflow.PortError},
}
