package enginetest

import (
	"context"
	"sync/atomic"

	"github.com/shinya/shineflow/domain/executor"
	"github.com/shinya/shineflow/domain/nodetype"
)

// MockExecutor 是测试用可编程节点执行器。
type MockExecutor struct {
	OnExecute func(ctx context.Context, in executor.ExecInput) (executor.ExecOutput, error)
	calls     int64
}

func (m *MockExecutor) Calls() int64 { return atomic.LoadInt64(&m.calls) }

func (m *MockExecutor) Execute(ctx context.Context, in executor.ExecInput) (executor.ExecOutput, error) {
	atomic.AddInt64(&m.calls, 1)
	if m.OnExecute == nil {
		return executor.ExecOutput{}, nil
	}
	return m.OnExecute(ctx, in)
}

func MockFactory(m *MockExecutor) executor.ExecutorFactory {
	return func(*nodetype.NodeType) executor.NodeExecutor { return m }
}
