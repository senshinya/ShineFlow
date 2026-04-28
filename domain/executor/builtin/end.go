package builtin

import (
	"context"

	"github.com/shinya/shineflow/domain/executor"
	"github.com/shinya/shineflow/domain/nodetype"
)

type endExecutor struct{}

func endFactory(_ *nodetype.NodeType) executor.NodeExecutor { return endExecutor{} }

// Execute 是空操作；驱动在 finalize 阶段读取该节点的 ResolvedInputs 作为 Run.Output。
func (endExecutor) Execute(_ context.Context, _ executor.ExecInput) (executor.ExecOutput, error) {
	return executor.ExecOutput{}, nil
}
