# ShineFlow

Go 实现的 AI 工作流后端服务。

## 架构

DDD 分层：

```
facade          接入层（HTTP / RPC）
application     应用层（用例编排）
domain          领域层（实体、聚合、仓储接口）
infrastructure  基础设施（DB、配置、工具）
```

依赖方向：`facade → application → domain ← infrastructure`

## 运行

```bash
# 默认连接 localhost:5432 / postgres / postgres / shineflow
go run .

# 自定义
SHINEFLOW_PORT=9000 \
SHINEFLOW_DB_DSN="host=... port=... user=... password=... dbname=... sslmode=disable" \
  go run .
```

健康检查：

```bash
curl http://localhost:8888/ping
# {"message":"pong"}
```

## 配置

| 环境变量 | 默认值 |
|---|---|
| `SHINEFLOW_PORT` | `8888` |
| `SHINEFLOW_DB_DSN` | `host=localhost port=5432 user=postgres password=postgres dbname=shineflow sslmode=disable TimeZone=Asia/Shanghai` |
