// Package cron 定义工作流定时触发器聚合 CronJob。
//
// 设计取舍：
//   - 不内嵌 MaxConcurrency / StartAt / EndAt / Catchup，等真有需求再扩
//   - CronJob 绑 DefinitionID 而非 VersionID，fire 时再读 Definition.PublishedVersionID
package cron

import (
	"encoding/json"
	"time"
)

// CronJob 是定时触发器聚合根。
//
// 不变式：
//   - Expression 必须是合法的标准 5 段 cron（由仓储 / application 层校验，本聚合不强制）
//   - Timezone 必须是 IANA 名（如 "Asia/Shanghai"）
//   - DefinitionID 必须存在；fire 时若 Definition.PublishedVersionID 为 nil 则跳过并记 error log
type CronJob struct {
	ID           string
	DefinitionID string
	Name         string
	Description  string

	Expression string
	Timezone   string
	Payload    json.RawMessage

	Enabled bool

	NextFireAt *time.Time
	LastFireAt *time.Time
	LastRunID  *string

	CreatedBy string
	CreatedAt time.Time
	UpdatedAt time.Time
}
