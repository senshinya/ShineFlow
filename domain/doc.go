// Package domain 是领域层。
//
// 职责：核心业务规则 —— 实体、值对象、聚合、领域服务、仓储接口。
//
// 子包索引：
//
//   workflow         工作流定义聚合（Definition / Version / DSL / Node / Edge / 端口 / 值源 / 错误策略）
//   validator        WorkflowDSL 严格校验（PublishVersion 时调用，独立子包以避免循环依赖）
//   nodetype         NodeType 统一目录与 Registry 接口；含 HttpPlugin / McpTool 投影函数
//   run              工作流运行时聚合（WorkflowRun / NodeRun + 状态机 + context 投影）
//   cron             定时触发器聚合 CronJob
//   plugin           HTTP 插件 / MCP Server / MCP Tool 三类外部能力
//   credential       秘密存储聚合 + CredentialResolver 接口
//   executor         节点执行器接口与 Registry（精确 + 前缀匹配）+ port 接口（HTTPClient 等）
//   executor/builtin 所有 NodeExecutor 实现的预留落点（六边形架构：执行器编排 = 领域逻辑），
//                    后续 executor spec 落 builtin + 插件 executor 全集
//
// 不在 domain：各 port 的具体适配器（infrastructure/http、infrastructure/llm、
// infrastructure/mcp、infrastructure/sandbox）和 Registry 装配函数。
//
// 禁止：依赖任何外部框架（hertz / gorm / sonic 等）。
// 仓储实现位于 infrastructure 层。
package domain
