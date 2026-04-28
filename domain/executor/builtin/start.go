package builtin

import (
	"context"

	"github.com/shinya/shineflow/domain/executor"
	"github.com/shinya/shineflow/domain/nodetype"
	"github.com/shinya/shineflow/domain/workflow"
)

type startExecutor struct{}

func startFactory(_ *nodetype.NodeType) executor.NodeExecutor { return startExecutor{} }

// Execute 返回空输出；触发参数通过 Symbols.trigger.<key> 访问。
func (startExecutor) Execute(_ context.Context, _ executor.ExecInput) (executor.ExecOutput, error) {
	return executor.ExecOutput{Outputs: map[string]any{}, FiredPort: workflow.PortDefault}, nil
}
