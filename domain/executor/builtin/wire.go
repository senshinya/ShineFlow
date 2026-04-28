package builtin

import (
	"github.com/shinya/shineflow/domain/executor"
	"github.com/shinya/shineflow/domain/nodetype"
)

// Register 将内置执行器工厂注册到执行器注册表。
func Register(reg executor.ExecutorRegistry) {
	reg.Register(nodetype.BuiltinStart, startFactory)
	reg.Register(nodetype.BuiltinEnd, endFactory)
	reg.Register(nodetype.BuiltinSetVariable, setVariableFactory)
	reg.Register(nodetype.BuiltinJoin, joinFactory)
	reg.Register(nodetype.BuiltinIf, ifFactory)
	reg.Register(nodetype.BuiltinSwitch, switchFactory)
	reg.Register(nodetype.BuiltinHTTPRequest, httpRequestFactory)
	reg.Register(nodetype.BuiltinLLM, llmFactory)
}
