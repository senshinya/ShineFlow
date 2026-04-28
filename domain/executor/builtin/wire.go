package builtin

import "github.com/shinya/shineflow/domain/executor"

// Register 将内置执行器工厂注册到执行器注册表。
func Register(reg executor.ExecutorRegistry) {
	_ = reg
}
