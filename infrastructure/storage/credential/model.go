package credential

import (
	"time"

	"gorm.io/gorm"
)

type credentialModel struct {
	ID               string         `gorm:"primaryKey;type:uuid"`
	Name             string         `gorm:"not null"`
	Kind             string         `gorm:"not null"`
	EncryptedPayload []byte         `gorm:"type:bytea;not null"`
	CreatedBy        string         `gorm:"not null"`
	CreatedAt        time.Time      `gorm:"not null"`
	UpdatedAt        time.Time      `gorm:"not null"`
	DeletedAt        gorm.DeletedAt `gorm:"index"`
}

func (credentialModel) TableName() string { return "credentials" }
