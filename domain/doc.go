// Package domain 是领域层。
//
// 职责：核心业务规则 —— 实体、值对象、聚合、领域服务、仓储接口。
//
// 子包索引：
//
//	workflow         工作流定义聚合（Definition / Version / DSL / Node / Edge / 端口 / 值源 / 错误策略）
//	validator        WorkflowDSL 严格校验（PublishVersion 时调用，独立子包以避免循环依赖）
//	nodetype         NodeType 统一目录与 Registry 接口；含 HttpPlugin / McpTool 投影函数
//	run              工作流运行时聚合（WorkflowRun / NodeRun + 状态机 + context 投影）
//	cron             定时触发器聚合 CronJob
//	plugin           HTTP 插件 / MCP Server / MCP Tool 三类外部能力
//	credential       秘密存储聚合 + CredentialResolver 接口
//	executor         节点执行器接口与 Registry（精确 + 前缀匹配）+ port 接口（HTTPClient 等）
//	executor/builtin 所有内置 NodeExecutor 实现（六边形架构：执行器编排 = 领域逻辑）
//	engine           工作流执行引擎：single-driver 事件循环、persister goroutine、worker goroutine，
//	                 驱动发布版 WorkflowVersion 到达终态；设计细节见 engine/doc.go
//
// 不在 domain：各 port 的具体适配器（infrastructure/http、infrastructure/llm、
// infrastructure/mcp、infrastructure/sandbox）和 Registry 装配函数。
//
// 禁止：直接依赖 hertz / gorm 等外部框架。
// JSON 序列化通过 infrastructure/util 这一层薄封装走，使全项目 JSON 入口统一。
// 该 import 是有意破例（不构成"domain 依赖外部框架"的反例）。
// 仓储实现位于 infrastructure 层。
package domain
