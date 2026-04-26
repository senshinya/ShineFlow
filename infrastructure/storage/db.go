package storage

import (
	"context"
	"sync"

	"github.com/cloudwego/hertz/pkg/common/hlog"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/plugin/dbresolver"
)

type ctxKey string

const (
	keyDBWrite ctxKey = "shineflow_db_write"
	keyDBTx    ctxKey = "shineflow_db_tx"
)

type ClusterType int

const (
	ClusterRead  ClusterType = 1
	ClusterWrite ClusterType = 2
)

var (
	initOnce sync.Once
	db       *gorm.DB
)

// MustInit 打开 PostgreSQL 连接并初始化包级单例。重复调用幂等。
func MustInit(dsn string) {
	initOnce.Do(func() {
		var err error
		db, err = gorm.Open(postgres.Open(dsn), &gorm.Config{})
		if err != nil {
			hlog.Fatalf("open postgres: %v", err)
		}
		// 接副本时：db.Use(dbresolver.Register(dbresolver.Config{Replicas: ...}))
	})
}

// SetCluster 在 ctx 上挂强制路由标志，让后续 GetDB 走指定库。
// 事务内调用无效（GetDB 优先返回 tx）。
func SetCluster(ctx context.Context, c ClusterType) context.Context {
	return context.WithValue(ctx, keyDBWrite, c == ClusterWrite)
}

// GetDB repo 默认入口：tx > write 标志 > 读库。
func GetDB(ctx context.Context) *gorm.DB {
	if tx, ok := ctx.Value(keyDBTx).(*gorm.DB); ok {
		return tx
	}
	if w, _ := ctx.Value(keyDBWrite).(bool); w {
		return getWriteDB(ctx)
	}
	return db.WithContext(ctx)
}

// getWriteDB repo 写操作显式入口：tx > 写库。
func getWriteDB(ctx context.Context) *gorm.DB {
	if tx, ok := ctx.Value(keyDBTx).(*gorm.DB); ok {
		return tx
	}
	return db.Clauses(dbresolver.Write).WithContext(ctx)
}

// DBTransaction application 层用：开事务 + 把 tx 注入 ctx + 嵌套幂等。
// 已在事务中调用直接执行 fn(ctx)，不再嵌套 SAVEPOINT。
func DBTransaction(ctx context.Context, fn func(ctx context.Context) error) error {
	if _, ok := ctx.Value(keyDBTx).(*gorm.DB); ok {
		return fn(ctx)
	}
	root := getWriteDB(ctx)
	root.DisableNestedTransaction = true
	return root.Transaction(func(tx *gorm.DB) error {
		return fn(context.WithValue(ctx, keyDBTx, tx))
	})
}
