package cron

import (
	"context"
	"errors"
	"time"
)

var ErrCronJobNotFound = errors.New("cron: cron job not found")

// CronJobFilter 是 List 的过滤参数。
type CronJobFilter struct {
	DefinitionID string
	EnabledOnly  bool
	Limit        int
	Offset       int
}

// CronJobRepository 是 CronJob 的存储契约。
//
// ClaimDue 是调度器 hot path：
//   - 实现应用 SELECT ... WHERE enabled AND next_fire_at <= now() FOR UPDATE SKIP LOCKED LIMIT N
//   - 返回的 CronJob 已被本调度器实例"占有"（NextFireAt 应在同事务推进，避免重复 fire）
type CronJobRepository interface {
	Create(ctx context.Context, j *CronJob) error
	Get(ctx context.Context, id string) (*CronJob, error)
	List(ctx context.Context, filter CronJobFilter) ([]*CronJob, error)
	Update(ctx context.Context, j *CronJob) error
	Delete(ctx context.Context, id string) error

	// ClaimDue 一次性认领最多 limit 条到期任务（行锁 SKIP LOCKED）。
	ClaimDue(ctx context.Context, now time.Time, limit int) ([]*CronJob, error)

	// MarkFired 在 fire 完成后更新调度元数据。
	MarkFired(ctx context.Context, id string, lastFireAt, nextFireAt time.Time, lastRunID string) error
}
