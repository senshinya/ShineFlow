// Package run 定义工作流运行时聚合 WorkflowRun + 子实体 NodeRun，以及 context 投影。
package run

// TriggerKind 标识 WorkflowRun 是被什么触发的。
type TriggerKind string

const (
	TriggerKindManual  TriggerKind = "manual"
	TriggerKindWebhook TriggerKind = "webhook"
	TriggerKindAPI     TriggerKind = "api"
	TriggerKindCron    TriggerKind = "cron"
)
