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

## AI 运行配置

首版仅支持 OpenAI-compatible provider。以下配置均可通过同名 `WEAVEPRESS_*` 环境变量覆盖；API 和 Worker 必须使用一致的 AI 配置，生产环境应通过部署平台的环境变量或 Secret 分别注入，不能把 API Key 写进 Git、配置示例、数据库或日志。

| 环境变量 | 默认值 | 说明 |
| --- | --- | --- |
| `WEAVEPRESS_AI_ENABLED` | `false` | 是否启用 AI 分析、生成和失败任务重试 |
| `WEAVEPRESS_AI_PROVIDER` | `openai-compatible` | 当前唯一支持的 provider |
| `WEAVEPRESS_AI_BASE_URL` | 空 | OpenAI-compatible 服务的 API 根地址或代理前缀；不要包含完整的 `/v1/chat/completions`，适配器会自动追加该路径 |
| `WEAVEPRESS_AI_API_KEY` | 空 | 仅以环境变量或 Secret 注入的服务端凭据 |
| `WEAVEPRESS_AI_MODEL` | 空 | Provider 提供的模型名 |
| `WEAVEPRESS_AI_REQUEST_TIMEOUT` | `120s` | 单次模型请求超时 |
| `WEAVEPRESS_AI_MAX_INPUT_CHARS` | `60000` | 输入字符上限，超限会拒绝任务而不是静默截断 |
| `WEAVEPRESS_AI_MAX_OUTPUT_TOKENS` | `6000` | 单次请求的最大输出 Token 数 |
| `WEAVEPRESS_AI_TEMPERATURE` | `0.4` | 生成温度，允许范围为 `0` 到 `2` |

启用时 `base_url`、`api_key` 和 `model` 都是必填项；生产环境的 `base_url` 必须使用 HTTPS。按所用服务商的 OpenAI-compatible 文档填写 API 根地址或版本根地址，但不要填写完整 Chat Completions endpoint；当前适配器会在配置路径后追加 `/v1/chat/completions`。

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
