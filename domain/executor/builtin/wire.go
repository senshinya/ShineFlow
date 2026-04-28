package builtin

import (
	"github.com/shinya/shineflow/domain/executor"
	"github.com/shinya/shineflow/domain/nodetype"
)

// Register 将内置执行器工厂注册到执行器注册表。
func Register(reg executor.ExecutorRegistry) {
	reg.Register(nodetype.BuiltinStart, startFactory)
	reg.Register(nodetype.BuiltinEnd, endFactory)
}
