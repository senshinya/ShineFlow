# ShineFlow 项目初始化设计

- 日期：2026-04-22
- 状态：已定稿，待实现
- Module：`github.com/shinya/shineflow`

## 1. 目标

搭建一个使用 Hertz 框架的 Go HTTP 服务骨架，采用 DDD 风格分层，
为后续 AI 工作流（workflow 编排、节点执行、LLM 调用等）业务模块
预留清晰、可扩展的边界。

本次仅交付**骨架**，不包含任何业务逻辑。

## 2. 范围

### In Scope

- Go module 初始化（`github.com/shinya/shineflow`）
- 根目录 `main.go` 入口
- DDD 四层目录结构：`facade` / `application` / `domain` / `infrastructure`
- HTTP 服务（Hertz）+ `GET /ping` 健康检查
- 配置加载（环境变量）
- PostgreSQL 数据库连接（gorm）
- JSON 序列化工具（sonic 封装）
- 基础 `.gitignore` 与 `README.md`

### Out of Scope（YAGNI）

- 任何业务实体（Workflow / Node / Run 等）
- 仓储接口与实现（等第一个真实用例再定义）
- 数据库迁移工具
- 中间件（鉴权 / 限流 / CORS / 日志增强）
- 配置文件（yaml / viper）
- 第三方日志库（用 hertz 自带 hlog）
- Redis、消息队列、对象存储等基础设施
- CI / Dockerfile / Makefile

## 3. 目录结构

```
shineflow/
├── go.mod
├── go.sum
├── .gitignore
├── README.md
├── main.go                          # 入口，位于根目录
│
├── facade/                          # 接入层
│   └── http/
│       ├── router.go                # 路由注册
│       └── handler/
│           └── ping.go              # GET /ping
│
├── application/                     # 应用层
│   └── doc.go                       # 占位（声明包及职责）
│
├── domain/                          # 领域层
│   └── doc.go                       # 占位（声明包及职责）
│
└── infrastructure/                  # 基础设施层
    ├── config/
    │   └── config.go                # 环境变量配置加载
    ├── storage/
    │   └── db.go                    # gorm + postgres 初始化
    └── util/
        └── sonic.go                 # sonic JSON 工具
```

## 4. 分层职责与依赖方向

### 4.1 职责定义（写进各层 doc.go）

| 层 | 职责 | 禁止事项 |
|---|---|---|
| **facade** | HTTP 协议处理：参数绑定、调用 application、响应封装 | 不写业务逻辑 |
| **application** | 用例编排：组合 domain 服务完成业务流；事务边界 | 不写领域规则、不直接操作 DB |
| **domain** | 核心业务规则：实体、聚合、领域服务、仓储接口 | 不依赖任何外部框架（hertz/gorm/sonic） |
| **infrastructure** | 技术实现：DB、外部 client、配置、仓储实现 | 不暴露技术细节给 domain |

### 4.2 依赖方向

```
facade ─→ application ─→ domain ←─ infrastructure
```

`domain` 不依赖任何其他层；`infrastructure` 反向依赖 `domain`（实现 domain 定义的接口）。

## 5. 第三方依赖

| 依赖 | 用途 |
|---|---|
| `github.com/cloudwego/hertz` | HTTP 框架 |
| `github.com/bytedance/sonic` | JSON 序列化（hertz 内部已使用，此处显式列为直接依赖） |
| `gorm.io/gorm` | ORM |
| `gorm.io/driver/postgres` | PostgreSQL 驱动（纯 Go，无 CGO） |

均使用最新稳定版。

## 6. 配置

通过环境变量加载，提供合理默认值：

| 变量 | 默认值 | 说明 |
|---|---|---|
| `SHINEFLOW_PORT` | `8888` | HTTP 监听端口 |
| `SHINEFLOW_DB_DSN` | `host=localhost port=5432 user=postgres password=postgres dbname=shineflow sslmode=disable TimeZone=Asia/Shanghai` | gorm Postgres DSN |

`Config` 结构示意：

```go
type Config struct {
    Port  string
    DBDSN string
}

func Load() *Config { /* 读环境变量，缺失走默认值 */ }
```

## 7. 启动流程（main.go）

```go
func main() {
    cfg := config.Load()
    db  := storage.MustInit(cfg.DBDSN)                       // 严格模式：连不上直接 fatal
    h   := server.New(server.WithHostPorts(":" + cfg.Port))
    httpfacade.Register(h, db)
    h.Spin()                                                 // 内置 graceful shutdown
}
```

**严格模式说明**：PostgreSQL 不像 SQLite 零配置，启动期就尝试连接。
连不上直接 `hlog.Fatalf` 退出，强制部署环境保证 DB 可用，避免运行时
才发现 DB 不通的隐性问题。

## 8. 关键代码片段

### 8.1 `infrastructure/storage/db.go`

```go
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
```

### 8.2 `infrastructure/util/sonic.go`

```go
package util

import (
    "context"

    "github.com/bytedance/sonic"
)

// json 是项目统一使用的 sonic 实例。
// UseInt64=true：JSON number 优先解码为 int64，避免雪花 ID、
// 时间戳等大整数走 float64 时的精度丢失。
var json = sonic.Config{
    UseInt64: true,
}.Froze()

// MarshalToString 将 source 序列化为 JSON 字符串。
// ctx 当前未被消费，预留给后续埋点 / 链路追踪。
func MarshalToString(ctx context.Context, source any) (string, error) {
    return json.MarshalToString(source)
}

// UnmarshalFromString 把 JSON 字符串反序列化到 target（必须是指针）。
func UnmarshalFromString(ctx context.Context, str string, target any) error {
    return json.UnmarshalFromString(str, target)
}
```

### 8.3 `facade/http/handler/ping.go`

```go
package handler

import (
    "context"

    "github.com/cloudwego/hertz/pkg/app"
    "github.com/cloudwego/hertz/pkg/common/utils"
    "github.com/cloudwego/hertz/pkg/protocol/consts"
)

func Ping(_ context.Context, c *app.RequestContext) {
    c.JSON(consts.StatusOK, utils.H{"message": "pong"})
}
```

### 8.4 `facade/http/router.go`

```go
package http

import (
    "github.com/cloudwego/hertz/pkg/app/server"
    "gorm.io/gorm"

    "github.com/shinya/shineflow/facade/http/handler"
)

func Register(h *server.Hertz, _ *gorm.DB) {
    h.GET("/ping", handler.Ping)
    // 后续路由继续在此挂载，db 通过闭包/依赖注入到对应 handler
}
```

`db` 形参当前未消费，但保留入口以便后续业务 handler 使用。

## 9. 测试策略

本次 init 不写单元测试。骨架的"测试"由以下手动验证完成：

1. `go build ./...` 通过
2. `go vet ./...` 无报错
3. 启动服务后 `curl http://localhost:8888/ping` 返回 `{"message":"pong"}`

业务模块入场后再补完整测试体系。

## 10. 验收标准

- [ ] 项目可 `go build ./...` 与 `go vet ./...` 通过
- [ ] 启动服务能在 `:8888` 监听
- [ ] `GET /ping` 返回 `{"message":"pong"}`
- [ ] DB 连不上时启动失败，错误信息清晰
- [ ] 目录结构与本文档 §3 一致
- [ ] 各层 `doc.go` 写明职责与禁止事项
