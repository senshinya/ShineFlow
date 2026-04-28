package nodetype

var endType = &NodeType{
	Key:         BuiltinEnd,
	Version:     NodeTypeVersion1,
	Name:        "End",
	Description: "工作流出口。Run.Output 等于该节点的 ResolvedInputs。",
	Category:    CategoryControl,
	Builtin:     true,
	Ports:       nil,
}
