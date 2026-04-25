package executor

import "context"

// NodeExecutor 是节点执行器的统一接口。
//
// 实现要求：
//   - 不应在内部修改 in.Inputs / in.Config（已是引擎快照）
//   - 必须在合理时间内响应 ctx.Done()（超时 / 取消由引擎用 ctx 控制）
//   - 业务失败应返回非 nil error；error 由引擎按 ErrorPolicy 处理（重试 / fallback / fail_run）
//   - 任何明文凭证只可在本方法局部使用，不得写入 ExecOutput.Outputs / ExternalRefs
type NodeExecutor interface {
	Execute(ctx context.Context, in ExecInput) (ExecOutput, error)
}
