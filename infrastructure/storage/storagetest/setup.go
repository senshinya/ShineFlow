// Package storagetest 提供测试用 PostgreSQL 容器 + 事务隔离 helper。
//
// 用法：
//
//	func TestXxx(t *testing.T) {
//	    ctx := storagetest.Setup(t)
//	    repo := workflow.NewWorkflowRepository()
//	    // ... 调 repo 方法，全部跑在 tx 里
//	    // t.Cleanup 自动 rollback，数据无残留
//	}
package storagetest

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"gorm.io/driver/postgres"
	gormpkg "gorm.io/gorm"

	"github.com/shinya/shineflow/infrastructure/storage"
)

// schemaPath 返回 schema.sql 的绝对路径（相对于本文件所在目录推导）。
func schemaPath() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "schema", "schema.sql")
}

var (
	once     sync.Once
	sharedDB *gormpkg.DB
	initErr  error
)

// Setup 起容器（首次调用时）+ 灌 schema + 把 storage 包级 db 指向容器，
// 然后开一个事务、注入 ctx，cleanup 时 rollback。
func Setup(t *testing.T) context.Context {
	t.Helper()
	once.Do(func() { initErr = bootstrap() })
	if initErr != nil {
		t.Fatalf("storagetest bootstrap: %v", initErr)
	}
	tx := sharedDB.Begin()
	t.Cleanup(func() { tx.Rollback() })
	return storage.WithTx(context.Background(), tx)
}

func bootstrap() error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	container, err := tcpostgres.Run(ctx,
		"postgres:16-alpine",
		tcpostgres.WithDatabase("shineflow_test"),
		tcpostgres.WithUsername("test"),
		tcpostgres.WithPassword("test"),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		return err
	}

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		return err
	}

	db, err := gormpkg.Open(postgres.Open(dsn), &gormpkg.Config{})
	if err != nil {
		return err
	}

	sqlBytes, err := os.ReadFile(schemaPath())
	if err != nil {
		return err
	}
	if err := db.Exec(string(sqlBytes)).Error; err != nil {
		return err
	}

	sharedDB = db
	storage.UseDB(db)
	return nil
}
