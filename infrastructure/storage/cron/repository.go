package cron

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	domaincron "github.com/shinya/shineflow/domain/cron"
	"github.com/shinya/shineflow/infrastructure/storage"
)

// emptyPayload 是 payload 字段的默认值，避免 NOT NULL 违反。
var emptyPayload = json.RawMessage("{}")

type cronRepo struct{}

// NewCronJobRepository 构造 GORM 实现的 CronJobRepository。
func NewCronJobRepository() domaincron.CronJobRepository { return &cronRepo{} }

func (r *cronRepo) Create(ctx context.Context, j *domaincron.CronJob) error {
	// Select("*") 强制写入所有字段，包括 false / 零值，避免 GORM 跳过 enabled=false。
	return storage.GetDB(ctx).Select("*").Create(toCronJobModel(j)).Error
}

func (r *cronRepo) Get(ctx context.Context, id string) (*domaincron.CronJob, error) {
	var m cronJobModel
	err := storage.GetDB(ctx).Where("id = ?", id).Take(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, domaincron.ErrCronJobNotFound
	}
	if err != nil { return nil, err }
	return toCronJob(&m), nil
}

func (r *cronRepo) List(ctx context.Context, filter domaincron.CronJobFilter) ([]*domaincron.CronJob, error) {
	q := storage.GetDB(ctx).Model(&cronJobModel{})
	if filter.DefinitionID != "" {
		q = q.Where("definition_id = ?", filter.DefinitionID)
	}
	if filter.EnabledOnly {
		q = q.Where("enabled = ?", true)
	}
	if filter.Limit > 0 { q = q.Limit(filter.Limit) }
	if filter.Offset > 0 { q = q.Offset(filter.Offset) }
	q = q.Order("created_at DESC")

	var ms []cronJobModel
	if err := q.Find(&ms).Error; err != nil { return nil, err }
	out := make([]*domaincron.CronJob, 0, len(ms))
	for i := range ms { out = append(out, toCronJob(&ms[i])) }
	return out, nil
}

func (r *cronRepo) Update(ctx context.Context, j *domaincron.CronJob) error {
	payload := j.Payload
	if len(payload) == 0 {
		payload = emptyPayload
	}
	res := storage.GetDB(ctx).Model(&cronJobModel{}).Where("id = ?", j.ID).
		Updates(map[string]any{
			"name":         j.Name,
			"description":  j.Description,
			"expression":   j.Expression,
			"timezone":     j.Timezone,
			"payload":      payload,
			"enabled":      j.Enabled,
			"next_fire_at": j.NextFireAt,
			"updated_at":   j.UpdatedAt,
		})
	if res.Error != nil { return res.Error }
	if res.RowsAffected == 0 { return domaincron.ErrCronJobNotFound }
	return nil
}

func (r *cronRepo) Delete(ctx context.Context, id string) error {
	res := storage.GetDB(ctx).Where("id = ?", id).Delete(&cronJobModel{})
	if res.Error != nil { return res.Error }
	if res.RowsAffected == 0 { return domaincron.ErrCronJobNotFound }
	return nil
}

// ClaimDue 用 FOR UPDATE SKIP LOCKED 锁住到期任务。
//
// **必须在 storage.DBTransaction 内调用** —— 行锁随事务结束释放。
// 调用方典型流程：DBTransaction(ctx, func(ctx){ ClaimDue → 创建 run → MarkFired }) 全部在同一事务里。
func (r *cronRepo) ClaimDue(
	ctx context.Context, now time.Time, limit int,
) ([]*domaincron.CronJob, error) {
	var ms []cronJobModel
	err := storage.GetDB(ctx).
		Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
		Where("enabled = ? AND next_fire_at IS NOT NULL AND next_fire_at <= ?", true, now).
		Order("next_fire_at ASC").
		Limit(limit).
		Find(&ms).Error
	if err != nil { return nil, err }
	out := make([]*domaincron.CronJob, 0, len(ms))
	for i := range ms { out = append(out, toCronJob(&ms[i])) }
	return out, nil
}

func (r *cronRepo) MarkFired(
	ctx context.Context, id string, lastFireAt, nextFireAt time.Time, lastRunID string,
) error {
	res := storage.GetDB(ctx).Model(&cronJobModel{}).Where("id = ?", id).
		Updates(map[string]any{
			"last_fire_at": lastFireAt,
			"next_fire_at": nextFireAt,
			"last_run_id":  lastRunID,
			"updated_at":   time.Now().UTC(),
		})
	if res.Error != nil { return res.Error }
	if res.RowsAffected == 0 { return domaincron.ErrCronJobNotFound }
	return nil
}
