package run

import (
	"encoding/json"
	"time"
)

type runModel struct {
	ID             string          `gorm:"primaryKey;type:uuid"`
	DefinitionID   string          `gorm:"type:uuid;not null"`
	VersionID      string          `gorm:"type:uuid;not null"`
	TriggerKind    string          `gorm:"not null"`
	TriggerRef     string          `gorm:"not null;default:''"`
	TriggerPayload json.RawMessage `gorm:"type:jsonb;not null;default:'{}'"`
	Status         string          `gorm:"not null"`
	StartedAt      *time.Time
	EndedAt        *time.Time
	Vars           json.RawMessage `gorm:"type:jsonb;not null;default:'{}'"`
	EndNodeID      *string
	Output         json.RawMessage `gorm:"type:jsonb"`
	Error          runErrorColumn  `gorm:"type:jsonb"`
	CreatedBy      string          `gorm:"not null"`
	CreatedAt      time.Time       `gorm:"not null"`
}

func (runModel) TableName() string { return "workflow_runs" }

type nodeRunModel struct {
	ID              string             `gorm:"primaryKey;type:uuid"`
	RunID           string             `gorm:"type:uuid;not null;index"`
	NodeID          string             `gorm:"not null"`
	NodeTypeKey     string             `gorm:"not null"`
	Attempt         int                `gorm:"not null"`
	Status          string             `gorm:"not null"`
	StartedAt       *time.Time
	EndedAt         *time.Time
	ResolvedConfig  json.RawMessage    `gorm:"type:jsonb;not null;default:'{}'"`
	ResolvedInputs  json.RawMessage    `gorm:"type:jsonb;not null;default:'{}'"`
	Output          json.RawMessage    `gorm:"type:jsonb"`
	FiredPort       string             `gorm:"not null;default:''"`
	FallbackApplied bool               `gorm:"not null;default:false"`
	Error           nodeErrorColumn    `gorm:"type:jsonb"`
	ExternalRefs    externalRefsColumn `gorm:"type:jsonb;not null;default:'[]'"`
}

func (nodeRunModel) TableName() string { return "workflow_node_runs" }
