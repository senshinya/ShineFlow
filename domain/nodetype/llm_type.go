package nodetype

import "github.com/shinya/shineflow/domain/workflow"

var llmType = &NodeType{
	Key:         BuiltinLLM,
	Version:     NodeTypeVersion1,
	Name:        "LLM",
	Description: "通过 ExecServices.LLMClient 调用 LLM provider。",
	Category:    CategoryAI,
	Builtin:     true,
	Ports:       []string{workflow.PortDefault, workflow.PortError},
}
