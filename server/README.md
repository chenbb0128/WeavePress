# WeavePress Server

WeavePress 的 Go 服务端，负责内部账号、鉴权、文章采集任务、正文结构化、素材归档、AI 辅助采编、微信稿件审核和公众号草稿箱发布。

本工程基于 [chenbb0128/go-template](https://github.com/chenbb0128/go-template) 初始化，沿用其 Gin、MySQL、Redis、Asynq、sqlc、Goose、OpenAPI、Metrics 和 Tracing 基础设施，并在此基础上实现 WeavePress 业务。

## 进程

- `cmd/api`：HTTP API、JWT 鉴权、用户与内容查询、采集与 AI 任务提交、媒体签名网关。
- `cmd/worker`：执行文章采集、素材归档、AI 分析与生成，以及微信公众号素材上传和草稿箱创建。
- `cmd/migrate`：执行 Goose 数据库迁移。
- `cmd/admin`：创建首个或后续管理员，不提供公开注册。
- `cmd/healthcheck`：容器内 API 就绪探针。

## 本地配置

复制示例配置：

```powershell
Copy-Item configs/config.example.yaml configs/config.yaml
```

默认需要 MySQL `127.0.0.1:3306` 和 Redis `127.0.0.1:6379`。除 AI 服务设置外，也可以使用 `WEAVEPRESS_*` 环境变量覆盖运行配置。

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

## AI 运行配置

AI 服务设置全部由管理员在管理端“AI 设置”页面维护，支持智谱 GLM、OpenAI 和自定义 OpenAI-compatible 服务。各服务商的 Base URL、Model 和 API Key 分别保存；Key 使用 AES-256-GCM 密文入库，页面和 GET 接口只显示是否已配置，不回显 Key。

智谱和 OpenAI 使用服务端固定官方地址；自定义兼容服务必须填写公网 HTTPS 地址，服务端在保存和请求时阻断 localhost、内网、云元数据、DNS Rebinding 和重定向。Key 留空保存表示保留原值。启用 AI 后，API 与 Worker 每次从数据库动态读取，无需重启。

内部运行限制固定为单次请求 120 秒、输入 60000 字符、最大输出 6000 Token、Temperature 0.4，不提供 `WEAVEPRESS_AI_*` 环境变量覆盖。当前分析依赖 Chat Completions 的 `response_format: {"type":"json_object"}` 能力。

Worker 必须监听 `ai` 队列，否则任务会一直停留在排队状态。推荐在 `configs/config.yaml` 使用与默认配置一致的优先级：

```yaml
worker:
  enabled: true
  concurrency: 4
  queues:
    collection: 2
    ai: 1
    publishing: 1
    default: 1
```

`GET /api/ai/status` 在 AI 关闭时仍返回 `200`，其中 `enabled` 为 `false`，便于后台展示禁用状态。此时历史分析、生成和任务仍可查询，但新建分析、新建生成和重试失败任务会返回 `503 Service Unavailable`，且不会创建或重新排队任务。

生成接口的 `angleId` 使用分析结果中的角度 ID；传入保留值 `SOURCE` 时进入忠实复刻模式，保持来源文章的核心主题、事实、观点关系和总体结论，同时重组标题、结构与表达。非引用内容仍执行连续 80 字重合检查，生成结果继续保留来源标注并只进入待编辑稿件。

## 主要目录

```text
api/openapi.yaml                         # API 契约
database/migrations/                     # MySQL 迁移
database/queries/                        # sqlc 查询
internal/collectors/                     # URL 安全、Fetcher、微信与网页采集器
internal/modules/authn/                  # JWT、Refresh Token、权限与登录限流
internal/modules/aiwriting/              # AI 分析、生成、校验、任务与审计
internal/modules/content/                # 采集编排、素材归档、媒体签名
internal/modules/editorial/              # 稿件版本、审核与微信发布编排
internal/modules/workspace/mysqlstore/   # 业务 MySQL 仓储
internal/platform/llm/                   # OpenAI-compatible Provider 适配器
internal/platform/objectstore/           # 本地与七牛对象存储适配器
internal/platform/wechat/                # 微信公众号官方 API 适配器
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
- AI 功能用于辅助采编和内容重构，不承诺规避原创检测，也不鼓励隐去来源的改写；生成结果必须由编辑逐条核对事实和引文，并确认版权与转载许可。
- AI 可能产生幻觉。系统会保留来源与溯源块，生成稿只进入 `editing` 状态，不会自动提交审核、通过审核或发布。
- 正式环境必须配置七牛私有 Bucket、随机 JWT 密钥和独立媒体签名密钥。
- 微信发布必须通过环境变量配置 AppID/AppSecret；当前只写入草稿箱，不自动群发。
- `api/openapi.yaml`、迁移和实际接口必须同步更新。
- `DEVELOPMENT.md` 是初始化工程时保留的上游模板设计参考，不代表当前业务状态。
