# WeavePress Server

WeavePress 的 Go 服务端，负责内部账号、鉴权、文章采集任务、正文结构化、素材归档和签名媒体访问。

本工程基于 [chenbb0128/go-template](https://github.com/chenbb0128/go-template) 初始化，沿用其 Gin、MySQL、Redis、Asynq、sqlc、Goose、OpenAPI、Metrics 和 Tracing 基础设施，并在此基础上实现 WeavePress 业务。

## 进程

- `cmd/api`：HTTP API、JWT 鉴权、用户与内容查询、采集任务提交、媒体签名网关。
- `cmd/worker`：执行微信与普通网页采集、正文解析、快照和图片归档。
- `cmd/migrate`：执行 Goose 数据库迁移。
- `cmd/admin`：创建首个或后续管理员，不提供公开注册。
- `cmd/healthcheck`：容器内 API 就绪探针。

## 本地配置

复制示例配置：

```powershell
Copy-Item configs/config.example.yaml configs/config.yaml
```

默认需要 MySQL `127.0.0.1:3306` 和 Redis `127.0.0.1:6379`。也可以使用 `WEAVEPRESS_*` 环境变量覆盖任意配置项。

执行迁移并创建管理员：

```powershell
go run ./cmd/migrate -command up
go run ./cmd/admin -username admin -password "请替换为强密码" -nickname "管理员"
```

分别启动 API 与 Worker：

```powershell
go run ./cmd/api

$env:WEAVEPRESS_WORKER_ENABLED='true'
go run ./cmd/worker
```

## 主要目录

```text
api/openapi.yaml                         # API 契约
database/migrations/                     # MySQL 迁移
database/queries/                        # sqlc 查询
internal/collectors/                     # URL 安全、Fetcher、微信与网页采集器
internal/modules/authn/                  # JWT、Refresh Token、权限与登录限流
internal/modules/content/                # 采集编排、素材归档、媒体签名
internal/modules/workspace/mysqlstore/   # 业务 MySQL 仓储
internal/platform/objectstore/           # 本地与七牛对象存储适配器
internal/transport/weaveapi/             # 业务 HTTP API
```

## 验证

默认测试不依赖外部服务：

```powershell
go tool sqlc generate
go test ./...
go build ./...
go vet ./...
```

提供测试服务地址后会额外执行 MySQL 与 Redis 集成测试：

```powershell
$env:WEAVEPRESS_TEST_MYSQL_DSN='weavepress:weavepress_pass@tcp(127.0.0.1:3306)/weavepress'
$env:WEAVEPRESS_TEST_REDIS_ADDR='127.0.0.1:6379'
go test ./... -count=1
```

集成测试使用唯一数据并在结束时清理；Redis 测试使用独立的 `weavepress-integration` 队列。

## 约束

- 只采集公开且允许访问的 HTTP/HTTPS 静态内容，不绕过登录、付费、验证码或平台风控。
- 正式环境必须配置七牛私有 Bucket、随机 JWT 密钥和独立媒体签名密钥。
- `api/openapi.yaml`、迁移和实际接口必须同步更新。
- `DEVELOPMENT.md` 是初始化工程时保留的上游模板设计参考，不代表当前业务状态。
