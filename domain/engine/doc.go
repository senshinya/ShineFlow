// Package engine 驱动已发布的 WorkflowVersion 到达终态。
//
// 设计：
//   - 单个 driver 拥有 runState，状态修改集中在线程内完成。
//   - persister goroutine 顺序消费 persistOp，driver 避免阻塞在数据库 IO 上。
//   - worker goroutine 每次执行一个节点尝试，并把 nodeResult 推回 done channel。
//   - inflight 与 pendingRetries 两个计数器共同决定事件循环终止。
//
// 规格：docs/superpowers/specs/2026-04-27-shineflow-workflow-engine-design.md
package engine
