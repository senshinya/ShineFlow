package storage

import (
	"github.com/cloudwego/hertz/pkg/common/hlog"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func MustInit(dsn string) *gorm.DB {
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		hlog.Fatalf("open postgres: %v", err)
	}
	return db
}
