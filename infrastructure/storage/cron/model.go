package cron

import (
	"encoding/json"
	"time"

	"gorm.io/gorm"
)

type cronJobModel struct {
	ID           string          `gorm:"primaryKey;type:uuid"`
	DefinitionID string          `gorm:"type:uuid;not null"`
	Name         string          `gorm:"not null"`
	Description  string          `gorm:"not null;default:''"`
	Expression   string          `gorm:"not null"`
	Timezone     string          `gorm:"not null"`
	Payload      json.RawMessage `gorm:"type:jsonb;not null;default:'{}'"`
	Enabled      bool            `gorm:"not null"`
	NextFireAt   *time.Time
	LastFireAt   *time.Time
	LastRunID    *string        `gorm:"type:uuid"`
	CreatedBy    string         `gorm:"not null"`
	CreatedAt    time.Time      `gorm:"not null"`
	UpdatedAt    time.Time      `gorm:"not null"`
	DeletedAt    gorm.DeletedAt `gorm:"index"`
}

func (cronJobModel) TableName() string { return "cron_jobs" }
