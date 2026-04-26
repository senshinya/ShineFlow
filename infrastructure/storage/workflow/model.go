package workflow

import (
	"time"

	"gorm.io/gorm"
)

type definitionModel struct {
	ID                 string         `gorm:"primaryKey;type:uuid"`
	Name               string         `gorm:"not null"`
	Description        string         `gorm:"not null;default:''"`
	DraftVersionID     *string        `gorm:"type:uuid"`
	PublishedVersionID *string        `gorm:"type:uuid"`
	CreatedBy          string         `gorm:"not null"`
	CreatedAt          time.Time      `gorm:"not null"`
	UpdatedAt          time.Time      `gorm:"not null"`
	DeletedAt          gorm.DeletedAt `gorm:"index"`
}

func (definitionModel) TableName() string { return "workflow_definitions" }

type versionModel struct {
	ID           string    `gorm:"primaryKey;type:uuid"`
	DefinitionID string    `gorm:"type:uuid;not null;index"`
	Version      int       `gorm:"not null"`
	State        string    `gorm:"not null"`
	DSL          dslColumn `gorm:"type:jsonb;not null"`
	Revision     int       `gorm:"not null"`
	PublishedAt  *time.Time
	PublishedBy  *string
	CreatedAt    time.Time `gorm:"not null"`
	UpdatedAt    time.Time `gorm:"not null"`
}

func (versionModel) TableName() string { return "workflow_versions" }
