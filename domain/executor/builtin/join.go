package builtin

import (
	"context"

	"github.com/shinya/shineflow/domain/executor"
	"github.com/shinya/shineflow/domain/nodetype"
	"github.com/shinya/shineflow/domain/workflow"
)

// joinExecutor 是控制流空操作节点；any/all 汇合语义由驱动 evaluate 阶段处理。
type joinExecutor struct{}

func joinFactory(_ *nodetype.NodeType) executor.NodeExecutor { return joinExecutor{} }

func (joinExecutor) Execute(_ context.Context, _ executor.ExecInput) (executor.ExecOutput, error) {
	return executor.ExecOutput{Outputs: map[string]any{}, FiredPort: workflow.PortDefault}, nil
}
