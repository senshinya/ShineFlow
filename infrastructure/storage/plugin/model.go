package plugin

import (
	"encoding/json"
	"time"

	"gorm.io/gorm"
)

type httpPluginModel struct {
	ID              string          `gorm:"primaryKey;type:uuid"`
	Name            string          `gorm:"not null"`
	Description     string          `gorm:"not null;default:''"`
	Method          string          `gorm:"not null"`
	URL             string          `gorm:"not null"`
	Headers         stringMapColumn `gorm:"type:jsonb;not null"`
	QueryParams     stringMapColumn `gorm:"type:jsonb;not null"`
	BodyTemplate    string          `gorm:"not null;default:''"`
	AuthKind        string          `gorm:"not null"`
	CredentialID    *string         `gorm:"type:uuid"`
	InputSchema     portSpecsColumn `gorm:"type:jsonb;not null"`
	OutputSchema    portSpecsColumn `gorm:"type:jsonb;not null"`
	ResponseMapping stringMapColumn `gorm:"type:jsonb;not null"`
	Enabled         bool            `gorm:"not null"`
	CreatedBy       string          `gorm:"not null"`
	CreatedAt       time.Time       `gorm:"not null"`
	UpdatedAt       time.Time       `gorm:"not null"`
	DeletedAt       gorm.DeletedAt  `gorm:"index"`
}

func (httpPluginModel) TableName() string { return "http_plugins" }

type mcpServerModel struct {
	ID            string          `gorm:"primaryKey;type:uuid"`
	Name          string          `gorm:"not null"`
	Transport     string          `gorm:"not null"`
	Config        json.RawMessage `gorm:"type:jsonb;not null"`
	CredentialID  *string         `gorm:"type:uuid"`
	Enabled       bool            `gorm:"not null"`
	LastSyncedAt  *time.Time
	LastSyncError *string
	CreatedBy     string         `gorm:"not null"`
	CreatedAt     time.Time      `gorm:"not null"`
	UpdatedAt     time.Time      `gorm:"not null"`
	DeletedAt     gorm.DeletedAt `gorm:"index"`
}

func (mcpServerModel) TableName() string { return "mcp_servers" }

type mcpToolModel struct {
	ID             string          `gorm:"primaryKey;type:uuid"`
	ServerID       string          `gorm:"type:uuid;not null;index"`
	Name           string          `gorm:"not null"`
	Description    string          `gorm:"not null;default:''"`
	InputSchemaRaw json.RawMessage `gorm:"type:jsonb;not null"`
	Enabled        bool            `gorm:"not null"`
	SyncedAt       time.Time       `gorm:"not null"`
}

func (mcpToolModel) TableName() string { return "mcp_tools" }
