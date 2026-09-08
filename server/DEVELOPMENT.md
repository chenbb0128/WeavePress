# 上游 Go 模板开发设计（历史参考）

> 本文随 `chenbb0128/go-template` 保留，用于记录基础设施设计来源；其中“业务暂不接入”、`/api/v1` 等描述已经过时。WeavePress 当前实现以根目录 `README.md`、`server/README.md`、`docs/` 和 `api/openapi.yaml` 为准。

> 状态：Implemented baseline v0.4（基础框架、OpenAPI、Metrics、Tracing 已落地，业务暂不接入）
> 项目：`weavepress`
> 文档日期：2026-08-17
> 当前阶段：已完成 API / MySQL / Redis / Worker / Migration / Docker / OpenAPI / Metrics / Tracing 基础设施，暂不实现业务接口
> 实现说明：当前仓库已经有可编译、可测试的基础框架代码；`identity` 仅保留领域模型、Port 与 MySQL repository adapter，不包含注册、登录等业务 handler/service。OpenAPI 当前只覆盖基础设施端点。

## 1. 文档目的

本文定义一套可运行、可测试、可观测、可长期维护的 Go 后端基础项目。它既是一份架构设计，也是后续开发阶段的实施与验收依据。

本项目采用模块化单体。当前代码先完成 API、Worker、MySQL、Redis、Migration、队列和工程化基础设施；用户身份认证、个人资料等业务能力暂不接入，等业务边界明确后再按模块补充。

开发开始前，应先评审并确认本文。评审通过后，再严格按第 16 节分阶段实施，每个阶段独立验证。

## 2. 待评审的默认决策

以下是本设计采用的默认值。若无修改意见，开发阶段按这些值执行。

| 项目 | 默认决策 |
|---|---|
| 项目名称 | `weavepress` |
| Go Module | `github.com/chenbb0128/weavepress/server`，开发前替换为正式地址 |
| 架构 | 模块化单体 Modular Monolith |
| HTTP | Gin + 标准库 `http.Server` |
| 数据库 | MySQL 8.4 LTS、`database/sql`、sqlc |
| 缓存和临时状态 | Redis |
| 异步任务 | Asynq + Redis |
| API 风格 | RESTful JSON，前缀 `/api/v1` |
| 身份模块 | Auth 与 User Profile 合并为 `identity` 模块 |
| Access Token | JWT，明确限定签名算法 |
| Refresh Token | 256-bit 随机不透明令牌，服务端仅保存哈希 |
| 配置 | Viper 独立实例，解析为强类型结构 |
| 日志 | `log/slog` JSON 结构化日志 |
| 可观测性 | Prometheus Metrics + OpenTelemetry Trace |
| 数据库迁移 | Goose |
| API 文档 | OpenAPI 3，规范文件作为事实来源 |
| 测试 | `testing`、`httptest`、手写 Fake、Testcontainers |
| 部署 | Docker 多阶段构建 + 根目录 `compose.yaml` |

具体 Go 和工具版本在开发阶段开始时确定，并固定在 `go.mod`、工具配置和 README 中。禁止使用不固定版本的 `@latest` 作为长期构建方式。

## 3. 目标与非目标

### 3.1 项目目标

- 克隆后可通过明确命令启动本地开发环境。
- API 与 Worker 使用同一仓库、同一版本、不同进程入口。
- 业务按模块组织，HTTP、数据库和 Redis 等技术实现不侵入业务逻辑。
- 所有外部调用传递 `context.Context`，支持超时和取消。
- 错误、日志、配置、响应和生命周期管理方式统一。
- 数据库结构、SQL 代码生成和工具版本可复现。
- 单元测试不依赖外部服务，集成测试使用真实 MySQL/Redis。
- 默认具备安全基线、健康检查、指标、链路追踪扩展能力。
- 模块将来可以拆分，但当前不为假想微服务承担额外复杂度。

### 3.2 当前非目标

第一版不实现：

- 微服务拆分、Kubernetes、Service Mesh。
- gRPC、Kafka、RabbitMQ、Elasticsearch。
- CQRS、Event Sourcing、完整 DDD 套件。
- 分布式事务框架和通用工作流引擎。
- Generic Repository、BaseController、BaseService、BaseRepository。
- 复杂依赖注入框架和全局 Service Locator。
- Order、Wallet、Product 等空业务模块。
- Redis 分布式锁的通用封装。
- 管理后台的 RBAC 权限系统。

## 4. 总体架构

### 4.1 进程模型

项目提供两个独立入口：

```text
cmd/api       HTTP API 进程
cmd/worker    Asynq Worker 进程
```

它们共享配置、业务模块和基础设施适配器，但生命周期彼此独立：

- API 关闭时：停止接收 HTTP 请求，等待在途请求，关闭 Redis 和 MySQL。
- Worker 关闭时：停止领取任务，等待在途任务，关闭 Redis 和 MySQL。
- API 进程不负责关闭 Worker；Worker 进程也不管理 API Server。

### 4.2 同步调用链

```text
HTTP Request
    ↓
Gin Router / Middleware
    ↓
Identity HTTP Handler
    ↓
Identity Service
    ↓
Identity Ports（小接口）
    ↓
MySQL / Redis / Token / Queue Adapters
    ↓
sqlc / database/sql / go-redis / Asynq
```

### 4.3 异步调用链

```text
Service
    ↓
TaskPublisher Port
    ↓
Asynq Adapter
    ↓
Redis
    ↓
Worker Handler
    ↓
Service / Repository Port
```

### 4.4 依赖规则

1. Gin 只存在于 HTTP 适配层。
2. Service 的公开 IO 方法第一个参数为 `context.Context`。
3. Handler 不访问数据库、Redis、sqlc 或事务对象。
4. Service 不依赖 `*gin.Context`、`*sql.DB`、sqlc 生成类型、go-redis 或 Asynq 类型。
5. 业务模块定义自己需要的小接口，由基础设施适配器实现。
6. sqlc 生成模型不得进入 Service 返回值或 HTTP Response。
7. `cmd` 只负责启动；依赖创建和组装放在 `internal/app`。
8. 不使用全局可变对象；依赖通过构造函数显式传递。
9. 接口由使用方定义，只为替换外部边界或测试创建，不为每个结构体机械创建接口。

## 5. 建议目录结构

```text
weavepress/
├── cmd/
│   ├── api/
│   │   └── main.go
│   └── worker/
│       └── main.go
├── internal/
│   ├── app/
│   │   ├── api.go
│   │   ├── worker.go
│   │   ├── wiring.go
│   │   └── lifecycle.go
│   ├── config/
│   │   ├── config.go
│   │   ├── loader.go
│   │   └── validate.go
│   ├── transport/
│   │   └── httpapi/
│   │       ├── router.go
│   │       ├── middleware/
│   │       └── response/
│   ├── modules/
│   │   └── identity/
│   │       ├── model.go
│   │       ├── service.go
│   │       ├── ports.go
│   │       ├── errors.go
│   │       ├── api/
│   │       │   ├── routes.go
│   │       │   ├── auth_handler.go
│   │       │   ├── profile_handler.go
│   │       │   └── dto.go
│   │       └── mysqlrepo/
│   │           └── repository.go
│   ├── platform/
│   │   ├── database/
│   │   │   ├── mysql.go
│   │   │   ├── transaction.go
│   │   │   └── sqlc/          # 生成代码，禁止手改
│   │   ├── redis/
│   │   ├── token/
│   │   ├── queue/
│   │   ├── logging/
│   │   ├── telemetry/
│   │   └── httpclient/
│   └── workers/
│       ├── mux.go
│       └── user_welcome.go
├── database/
│   ├── migrations/
│   └── queries/
├── api/
│   └── openapi.yaml
├── configs/
│   └── config.example.yaml
├── deployments/
│   └── Dockerfile
├── scripts/
├── .env.example
├── .gitignore
├── compose.yaml
├── Makefile
├── sqlc.yaml
├── go.mod
├── go.sum
├── README.md
└── DEVELOPMENT.md
```

约束：

- 不创建没有代码的预留目录；以上目录按实施阶段逐步出现。
- 不创建全局 `controllers/`、`services/`、`repositories/`、`models/`。
- 暂不创建 `pkg/`。只有代码确实需要被仓库外部引用时才考虑。
- `identity` 同时拥有认证和用户资料，是因为两者共享用户凭据、状态和生命周期。未来出现独立边界后再拆分，避免当前产生 Auth 与 User 循环依赖。

## 6. Identity 模块设计

### 6.1 第一版接口

```text
POST  /api/v1/auth/register
POST  /api/v1/auth/login
POST  /api/v1/auth/refresh
POST  /api/v1/auth/logout
GET   /api/v1/users/me
PATCH /api/v1/users/me
GET   /health
GET   /ready
GET   /metrics              # 默认仅在受控网络开放
```

Swagger UI 在开发环境提供，生产环境默认关闭。

### 6.2 用户数据

`users` 表第一版字段：

| 字段 | 说明 |
|---|---|
| `id` | `BIGINT UNSIGNED` 自增主键 |
| `username` | 唯一用户名，建立唯一索引 |
| `password_hash` | bcrypt 哈希，永不返回 API |
| `nickname` | 用户昵称 |
| `avatar` | 头像 URL |
| `status` | `active` / `disabled` |
| `created_at` | UTC，`DATETIME(3)` |
| `updated_at` | UTC，`DATETIME(3)` |

第一版不默认加入软删除。若未来存在合规删除或数据恢复需求，再明确删除语义。

数据库连接建立后显式保证会话时区为 UTC；DSN 使用 `parseTime=true`、`charset=utf8mb4` 和 UTC location。API 时间统一输出 RFC3339。

### 6.3 注册流程

```text
校验输入
    ↓
查询用户名（只用于友好提示）
    ↓
bcrypt 哈希密码
    ↓
插入用户
    ↓
由数据库唯一索引最终处理并发冲突
    ↓
提交 user.welcome 非关键异步任务
    ↓
返回用户 DTO
```

用户名预查询不能代替唯一索引。Repository 必须通过 MySQL 错误类型和错误码识别唯一键冲突，禁止匹配错误字符串。

欢迎任务属于允许补偿的非关键任务：用户创建成功后，即使任务提交失败，注册仍然成功，同时记录错误日志和指标。未来的支付、库存、佣金等关键任务必须使用 Transactional Outbox 与幂等消费。

### 6.4 登录流程

```text
校验输入
    ↓
按可信客户端 IP 限流
    ↓
查询用户
    ↓
统一校验用户名和密码
    ↓
检查用户状态
    ↓
签发 Access Token
    ↓
创建 Refresh Token Family
    ↓
保存 Refresh Token 哈希状态
    ↓
返回 Token DTO
```

用户名不存在和密码错误统一返回“用户名或密码错误”，避免用户枚举。限流规则应兼顾 IP 与账号维度，代理地址只在可信代理配置正确时使用。

### 6.5 Token 方案

Access Token：

- 使用 JWT，包含 `sub`、`jti`、`iat`、`exp`、`iss`、`aud`。
- 解析时固定允许的签名算法，不接受 Token Header 任意指定算法。
- 默认有效期 15 分钟，可配置。
- 签名密钥长度和复杂度在启动时校验。

Refresh Token：

- 使用加密安全随机数生成至少 256-bit 的不透明令牌，不使用包含业务信息的 JWT。
- 客户端持有原始令牌；服务端只保存 SHA-256 哈希、用户 ID、Family ID、状态和过期时间。
- 默认有效期 30 天，可配置。
- 每次刷新执行 Rotation：旧令牌立即失效并签发新令牌。
- 检测到已轮换令牌被重放时，撤销整个 Token Family。
- Logout 撤销当前 Family；用户禁用后禁止继续刷新。
- Redis 必须开启与业务风险相匹配的持久化和高可用策略。若项目需要强审计和长期会话追踪，改为数据库持久化。

### 6.6 密码规则

- 使用 bcrypt，不保存或记录明文密码。
- 默认 cost 在开发时通过基准测试后确定，建议从 12 开始评估。
- 输入长度按 UTF-8 字节校验，不能超过 bcrypt 的 72-byte 限制。
- 密码错误日志不得包含密码或哈希。

## 7. 数据访问与事务

### 7.1 SQL 与 sqlc

数据访问链：

```text
database/queries/*.sql
    ↓
sqlc generate
    ↓
internal/platform/database/sqlc
    ↓
database/sql
    ↓
go-sql-driver/mysql
    ↓
MySQL
```

规则：

- 所有条件值使用参数绑定，禁止拼接用户输入。
- sqlc 生成目录禁止手工修改。
- 生成代码提交仓库；CI 重新生成并检查是否存在未提交差异。
- sqlc 生成的模型只存在于 MySQL Adapter 内部。
- Repository 将 sqlc 模型映射成 `identity` 业务模型。
- API 使用独立 DTO，禁止直接序列化业务模型或数据库模型。
- 不使用 ORM 或生产环境 AutoMigrate。

### 7.2 迁移规则

- 使用 Goose 管理 Migration。
- 文件名采用时间戳加语义名称，降低多人分支编号冲突。
- 已在共享环境执行的 Migration 不再修改；通过新增 Migration 演进。
- Up 与 Down 必须经过评审；破坏性 Down 不以“形式完整”为理由自动提供。
- Migration 与对应代码在同一变更中提交。

### 7.3 连接池

MySQL 初始化必须：

- 配置连接、读、写超时。
- 配置 `MaxOpenConns`、`MaxIdleConns`、`ConnMaxLifetime`、`ConnMaxIdleTime`。
- 启动时使用有超时的 `PingContext`。
- 暴露连接池指标。
- 关闭进程时调用 `Close()`。

连接池大小应依据实例数、MySQL 最大连接数和压测结果确定，不把任意默认值当作生产结论。

### 7.4 事务边界

- 事务由 Service 用例决定，Handler 不开启事务。
- 单条原子写入不为了“架构完整”额外开启事务。
- 多条相关写入通过事务执行器完成。
- 事务回调只获得事务绑定后的模块 Store，不暴露 `*sql.Tx` 或 sqlc `*Queries` 给 Service。
- 所有事务内 Store 使用同一个底层 `*sql.Tx`。
- 正确处理 Begin、Rollback、Commit 及 Commit 错误。
- 事务中不执行不可控的外部 HTTP、Redis 或队列调用。

金额类字段未来统一使用 `int64` 最小货币单位，禁止用浮点数保存金额。

## 8. HTTP 与 API 规范

### 8.1 Server

- 使用 `gin.New()`，不使用 `gin.Default()`。
- 使用标准库 `http.Server`，不直接调用 `router.Run()`。
- 配置 `ReadHeaderTimeout`、`ReadTimeout`、`WriteTimeout`、`IdleTimeout`、`MaxHeaderBytes`。
- 配置请求体大小上限。
- 只信任配置中的代理网段。
- 接收 SIGINT/SIGTERM 后通过 `Shutdown` 优雅退出。

### 8.2 Request 规范

#### 8.2.1 Body 与媒体类型

- 带 JSON Body 的接口必须使用 `Content-Type: application/json`，允许 UTF-8 charset；其他媒体类型返回 HTTP 415。
- 客户端接受 `application/json`、兼容 JSON 类型或 `*/*`；明确排除 JSON 时返回 HTTP 406。
- 默认 Body 上限为 1 MiB，并允许按路由调整；超限立即返回 HTTP 413，不继续读取完整 Body。
- JSON 必须是一个完整对象。空 Body、语法错误和尾随第二个 JSON 值返回 HTTP 400。
- 第一版使用严格 JSON，DTO 未声明字段返回参数错误，避免客户端字段拼写错误被静默忽略。
- Handler 只能用独立 Request DTO 接收输入，禁止直接绑定数据库模型、业务模型或无约束的 `map[string]any`。
- 密码不得自动 Trim、大小写转换或 Unicode 归一化；其他字段的规范化必须由业务规则明确指定。

#### 8.2.2 Path、Query 与校验

- 数字资源 ID 仅接受十进制正整数；零、负数、溢出和非数字返回 HTTP 400。
- Query 解析与校验集中在 DTO，不在 Handler 中散落字符串转换。
- 分页默认 `page=1`、`page_size=20`，`page_size` 最大为 100；越界返回 HTTP 422。
- 时间输入统一使用 RFC3339，进入 Service 后转换为 UTC。
- JSON、Path、Query 无法解析属于语法错误，返回 HTTP 400；格式可解析但不满足字段约束返回 HTTP 422。
- Validation Details 使用 JSON 字段名和稳定的 Reason，不暴露 Go 字段名、validator Tag 或原始错误。
- Bind 和 Validate 完成后，Handler 只向 Service 传递 `c.Request.Context()`、显式身份 ID 和 DTO。

### 8.3 Router 规范

路由分区：

```text
/health                 Liveness
/ready                  Readiness
/metrics                Prometheus，受控开放
/swagger/*              仅开发环境启用
/api/v1/auth/*          Identity 公开认证路由
/api/v1/users/*         Identity 受保护用户路由
```

- Router 构造函数只接收显式依赖，不读取全局配置或全局 Service。
- 根 Router 只负责跨模块中间件和顶层分组；模块通过自己的 `RegisterRoutes` 注册路由。
- 公开路由与认证路由分组必须明确，不能依靠 Handler 临时决定是否认证。
- 开启 Method Not Allowed；404 和 405 均使用统一 JSON 错误响应。
- API 使用精确路径匹配，关闭自动尾斜杠和 Fixed Path 重定向，避免 POST/PATCH 被隐式重定向。
- Metrics 使用 Gin Route Template 而非原始 URL 作为 Label，防止用户 ID 等高基数值进入指标。
- 同一 Method + Path 重复注册必须在启动时失败。
- CORS Preflight 由统一中间件处理，`OPTIONS` 不进入业务 Handler。
- `/metrics` 和 `/swagger` 由配置控制，生产环境默认不公开。
- 非破坏性变更继续使用 `/api/v1`；只有破坏性协议变化才增加 Major Version。

### 8.4 Request ID 规范

Header 名称固定为 `X-Request-ID`：

1. Request ID 是最外层业务中间件，在进入其他中间件前确定。
2. 客户端值仅在满足 `^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$` 时沿用。
3. 缺失或非法时，使用 `crypto/rand` 生成 128-bit 随机值，编码成 32 位小写十六进制字符串。
4. 非法原值不回显、也不写入日志，直接替换，防止日志注入。
5. ID 同时写入 Gin Context 和标准 Request Context；Context Key 使用包内私有强类型。
6. Response Header 在进入后续 Handler 前写入，正常、404、405 和 Recovery 响应均携带该 ID。
7. slog、Trace 和支持时的 Metrics Exemplar 关联该 ID；Trace ID 与 Request ID 保持独立。
8. 下游 HTTP 传播 `X-Request-ID` 和标准 Trace Context；异步任务记录独立 `correlation_id`。
9. Service 通过 Context Helper 获取 Request ID，不依赖 Gin。
10. Request ID 只用于排障关联，不能作为认证凭据、幂等键、主键或安全 Token。

### 8.5 中间件顺序

固定顺序：

```text
Request ID
    ↓
Recovery
    ↓
Security Headers
    ↓
CORS
    ↓
Body Limit
    ↓
Access Log / Metrics / Trace
    ↓
Rate Limit（指定路由）
    ↓
JWT Auth（受保护路由）
```

- Access Log、Metrics 和 Trace 在 `c.Next()` 前记录开始状态，返回后读取最终 Status 与延迟。
- Recovery 对客户端只返回安全错误；堆栈仅写入内部日志，并关联 Request ID 与 Trace ID。
- JWT Middleware 只校验身份并写入用户 ID，不执行具体业务授权。
- Handler 一旦输出响应必须立即 return，禁止重复写 Header 或 Body。

### 8.6 Response 契约

普通成功响应：

```json
{
  "code": 0,
  "message": "success",
  "data": {}
}
```

错误响应：

```json
{
  "code": 30001,
  "message": "用户不存在"
}
```

字段校验错误：

```json
{
  "code": 10007,
  "message": "请求参数不合法",
  "details": [
    {
      "field": "username",
      "reason": "required"
    }
  ]
}
```

规则：

- 正确使用 HTTP 状态码，不把所有结果包装成 HTTP 200。
- 有响应实体的成功接口固定包含 `code`、`message` 和 `data`。
- 列表字段使用空数组而不是 `null`。
- 创建成功使用 HTTP 201，并在适用时返回 `Location` Header。
- `204 No Content` 和 HEAD 不返回 JSON Body，不强行套用响应包装。
- 除 Metrics、Swagger 和无 Body 状态外，JSON 响应使用 `Content-Type: application/json; charset=utf-8`。
- Login 和 Refresh 响应返回 `Cache-Control: no-store`。
- 错误响应不包含 `data`；只有可公开的参数问题才包含 `details`。
- `details.field` 使用 JSON 字段路径，`details.reason` 使用稳定枚举。
- 404、405、Body 超限、媒体类型、认证、限流和 Recovery 均通过统一 Response Writer 输出。
- 500、依赖错误和 Panic 不返回 SQL、堆栈、路径、第三方原文或内部错误链。
- Response Writer 统一完成 AppError 到 HTTP Status 与业务码的映射；Handler 不复制映射表。
- 每个应用响应包含 `X-Request-ID` Header。
- 错误码由常量集中定义，发布后不得改变既有语义；客户端不得依赖错误文案判断逻辑。
- 分页第一版使用 `page`、`page_size`、`total`；大数据量场景再引入游标分页。
- Response DTO 不包含密码哈希、Token 内部状态、数据库字段或未声明属性。
- 第一版不提供流式业务响应，Handler 不先写部分 Body 再调用可能失败的 Service。
- JSON 统一由 `internal/transport/httpapi/response` 输出；业务 Handler 不散落自定义 Envelope。
- OpenAPI Schema、实际 JSON 字段和测试断言必须一致。

## 9. 错误设计

应用错误包含：

```text
Code         稳定业务错误码
HTTPStatus   HTTP 状态码
Message      可安全返回的文案
Err          内部原始错误链
```

要求：

- 实现 `Error()` 和 `Unwrap()`，支持 `errors.Is/As`。
- Handler 只根据应用错误生成响应。
- 日志记录真实错误链，对外不暴露 SQL、堆栈、文件路径和基础设施信息。
- 数据库错误在 Repository Adapter 边界转换。
- 未知错误统一转换为内部服务器错误，并保留 Request ID 供排查。

第一版错误域：

```text
100xx  参数和通用资源错误
200xx  认证与 Token 错误
300xx  用户错误
500xx  内部依赖或服务错误
```

## 10. 配置与 Secret

配置加载顺序：

```text
代码默认值
    ↓
YAML 文件
    ↓
环境变量覆盖
    ↓
强类型解析
    ↓
启动校验
```

规则：

- 使用 `viper.New()`，不使用 Viper 全局单例。
- 业务代码不直接调用 `viper.Get*`。
- `.env.example` 只作为变量说明；是否加载本地 `.env` 必须显式实现和记录，不能假设 Viper 自动加载。
- 数据库密码、Redis 密码、Token 密钥等 Secret 只通过环境变量或 Secret Manager 注入。
- 日志输出配置摘要时必须脱敏。
- 环境名、端口、超时、DSN、Redis 地址、Token TTL、CORS 和可信代理配置启动时校验，错误时快速失败。

## 11. Redis 与异步任务

### 11.1 Redis

- 所有调用接收 `context.Context`。
- Key 通过统一 Builder 生成，并包含应用名和环境隔离前缀。
- Redis 只保存缓存、限流、会话状态和任务数据，不作为订单、余额等核心数据的唯一事实来源。
- 缓存必须定义 TTL、失效策略和降级行为。
- `/ready` 是否依赖 Redis 由启用的功能决定；第一版认证依赖 Refresh Token 状态，因此 Redis 被视为必需依赖。

### 11.2 Asynq

第一版任务：`user.welcome.v1`。

规则：

- Payload 只携带必要 ID 和任务版本，不序列化数据库模型。
- 设置超时、最大重试、重试退避和 Dead Letter/归档观察方式。
- Handler 必须具备幂等性。
- Worker 记录任务 ID、类型、重试次数、耗时和错误。
- 关键异步业务不能使用裸 `go func()` 替代持久队列。
- 关键数据库事件未来使用 Transactional Outbox，不假设“提交后立即入队”天然可靠。

## 12. 日志、指标与追踪

### 12.1 日志

使用 `log/slog` 输出 JSON。HTTP 日志至少包含：

```text
timestamp, level, message, request_id, trace_id,
method, route, status, latency, client_ip, user_agent, user_id, error
```

不得记录密码、完整 Token、Refresh Token、Cookie Secret、数据库密码和用户敏感信息。高基数字段不能随意作为 Metrics Label。

### 12.2 Metrics

第一版至少提供：

- HTTP 请求数、状态码、路由和耗时。
- MySQL 连接池状态。
- Redis 调用错误与耗时。
- Asynq 任务成功、失败、重试和耗时。
- 登录限流命中次数。

`/metrics` 默认只允许受控网络访问，不作为公开业务接口。

### 12.3 Trace

- 使用 OpenTelemetry 标准接口。
- HTTP、MySQL、Redis、外部 HTTP 和异步任务传播 Trace Context。
- 未配置 Collector 时允许关闭导出，不影响业务启动。
- 日志关联 `trace_id` 与 `request_id`。

## 13. 健康检查

```text
GET /health    Liveness：只证明进程事件循环正常
GET /ready     Readiness：检查当前进程的必需依赖
```

规则：

- `/health` 不查询 MySQL、Redis 或第三方服务。
- API 的 `/ready` 检查 MySQL 和 Redis，使用严格短超时。
- Worker 如提供管理探针，只检查 Worker 所需的 MySQL/Redis 与接收任务状态。
- Readiness 响应不泄露 DSN、地址、密码和底层错误详情。

## 14. 安全基线

- bcrypt 密码哈希与合理 cost。
- JWT 固定算法、Issuer、Audience 和过期时间校验。
- Refresh Token 哈希存储、Rotation、重放检测和撤销。
- 登录按 IP 与账号维度限流。
- 请求体大小、Header 大小和 HTTP 超时限制。
- CORS 使用允许列表；生产禁止通配来源与凭据组合。
- 可信代理白名单，避免伪造客户端 IP。
- SQL 全部参数化。
- 日志和错误响应脱敏。
- Swagger 和 Metrics 生产环境默认不公开。
- Docker 最终镜像以非 root 用户运行。
- CI 执行依赖漏洞检查。
- 为重要操作预留审计事件接口，但第一版不建设通用审计平台。

## 15. 测试与质量门禁

### 15.1 测试分层

单元测试：

- 测试 Service 业务规则。
- 使用模块定义的小接口和手写 Fake。
- 不连接 MySQL、Redis，不依赖复杂 Mock 框架。

Handler 测试：

- 使用 `httptest`。
- 覆盖 Content-Type、Body 上限、严格 JSON、Path/Query 解析和字段校验。
- 覆盖 404、405、认证失败、限流、Recovery、成功响应、状态码和统一 JSON。
- 覆盖合法 Request ID 沿用、非法值替换、自动生成、Context 与 Response Header 传播。
- 断言 Handler 不重复写响应，且内部错误不会出现在客户端 Body。

Repository/Adapter 集成测试：

- 使用 Testcontainers 启动真实 MySQL/Redis。
- 禁止使用 SQLite 模拟 MySQL 方言。
- 通过构建标签或独立命令运行，CI 中必须执行。
- 每个测试拥有隔离数据，自动运行 Migration，并验证唯一约束和错误转换。

端到端验收：

- 使用 `compose.yaml` 启动 API、Worker、MySQL、Redis。
- 真实调用注册、登录、刷新、退出、查询和更新资料接口。
- 验证欢迎任务可提交并消费。

### 15.2 必须通过的命令

```bash
gofmt
go vet ./...
staticcheck ./...
govulncheck ./...
go test ./...
go test -race ./...
go test -tags=integration ./...
go build ./...
sqlc generate
docker compose config
docker compose build
```

Makefile 至少提供：

```text
make run
make worker
make build
make test
make test-race
make test-integration
make fmt
make vet
make lint
make vuln
make sqlc
make migrate-up
make migrate-down
make migrate-status
make migrate-create name=...
make docker-up
make docker-down
make openapi-check
```

## 16. 分阶段实施计划

每个阶段完成后必须列出变更文件、执行验证命令、修复所有本阶段错误，再进入下一阶段。未经评审同意，不提前扩大业务范围。

### 阶段 0：版本与设计基线

交付：

- 确认 Go Module 正式地址。
- 固定 Go、sqlc、Goose、静态检查和 OpenAPI 工具版本。
- 创建 `go.mod`、`.gitignore`、基础 Makefile 和 README 框架。
- 记录关键架构决策。

验收：

- 工具版本可查询、可重复安装。
- `go mod tidy` 和基础质量命令可执行。

### 阶段 1：进程骨架

交付：

- 强类型配置与启动校验。
- slog JSON 日志。
- 模块化 Gin Router、404/405 和基础中间件。
- 严格 Request 解析、参数校验与 Body 限制。
- Request ID 生成、校验、Context/日志/Response 传播。
- 统一 Response Writer、Validation Details 与 AppError。
- `/health`。
- `http.Server` 超时和 API Graceful Shutdown。

验收：

```bash
go run ./cmd/api
curl http://localhost:8080/health
go test ./...
go build ./...
```

### 阶段 2：MySQL、Migration 与 sqlc

交付：

- MySQL 连接池和 UTC 配置。
- Goose Migration。
- `users` 表和查询 SQL。
- sqlc 配置及生成代码。
- Identity MySQL Repository Adapter。
- `/ready` 的 MySQL 检查。
- Repository 集成测试。

验收：

- Migration Up/Down/Status 通过。
- `sqlc generate` 可重复且工作区无意外差异。
- MySQL 集成测试通过。
- API 编译通过。

### 阶段 3：Identity、Redis 与 Token

交付：

- Redis Client、Key Builder 和 Ready Check。
- Identity Service、HTTP Handler 和路由。
- Register、Login、Refresh、Logout、Get Me、Update Me。
- JWT Access Token。
- 不透明 Refresh Token、哈希存储、Rotation 与重放检测。
- 登录限流。
- Service 与 Handler 测试。

验收：

- 第一版所有 HTTP 接口通过正常和异常场景验证。
- 用户禁用后不能刷新 Token。
- 旧 Refresh Token 重放会撤销 Token Family。
- 日志和 API 不出现密码、哈希及完整 Token。

### 阶段 4：Worker 与异步任务

交付：

- Asynq Client 与 Worker Server。
- `user.welcome.v1` 示例任务。
- 超时、重试、幂等、指标和日志。
- Worker 独立 Graceful Shutdown。

验收：

- API 可提交任务。
- Worker 可消费、重试并平滑退出。
- 重复处理不会产生重复副作用。

### 阶段 5：OpenAPI、可观测性与交付环境

交付：

- 完整 OpenAPI 3 规范和开发 Swagger UI。
- Prometheus Metrics 与 OpenTelemetry 接入。
- 多阶段、非 root Dockerfile。
- 根目录 `compose.yaml`。
- 完整 Makefile、CI 配置和 README。
- 端到端验收脚本。

验收：

```bash
go vet ./...
staticcheck ./...
govulncheck ./...
go test ./...
go test -race ./...
go test -tags=integration ./...
go build ./...
docker compose up -d
```

API、Worker、MySQL、Redis 均正常运行，全部第一版接口和异步任务通过端到端验证。

## 17. 最终完成标准

项目只有同时满足以下条件才算完成：

- API 与 Worker 可分别启动和优雅退出。
- MySQL Migration 和 sqlc 生成可重复执行。
- 第一版 8 个接口按 OpenAPI 正常工作。
- Refresh Token Rotation、撤销和重放检测生效。
- Worker 可可靠消费示例任务。
- 单元、Handler、集成、Race 和端到端测试通过。
- Docker Compose 可从仓库根目录启动完整环境。
- 日志、Metrics、Trace 和 Request ID 可以关联排障。
- CI 质量门禁全部通过。
- README 足以让新开发者独立完成配置、启动、迁移、生成、测试和新增模块。
- 不存在大段伪代码、无意义接口、空业务模块和未说明的关键 TODO。

## 18. 后续新增模块规范

新增业务模块前先明确：

1. 模块拥有的数据和业务规则。
2. 模块对外提供的用例。
3. 模块依赖的最小 Ports。
4. 一致性与事务边界。
5. 同步调用还是异步事件。
6. 错误码、权限、安全和审计要求。
7. 单元、集成和端到端测试范围。

标准开发顺序：

```text
确认业务边界
    ↓
新增 Migration
    ↓
执行 Migration
    ↓
编写参数化 SQL
    ↓
执行 sqlc generate
    ↓
实现 Adapter 和错误转换
    ↓
实现 Service 用例
    ↓
实现 HTTP / Worker 入口
    ↓
补充测试、OpenAPI、Metrics 和 README
```

任何微服务拆分都必须由真实的独立部署、扩缩容、团队所有权或故障隔离需求驱动，不能仅因为目录变大而拆分。

## 19. 评审清单

开发前请重点确认：

- [ ] 正式 Go Module 地址。
- [ ] 是否接受 Gin + MySQL 8.4 LTS。
- [ ] 是否接受 Auth 与 User Profile 合并为 `identity` 模块。
- [ ] 是否接受 JWT Access Token + 不透明 Refresh Token。
- [ ] 是否需要第一版就实现 Prometheus 和 OpenTelemetry。
- [ ] Redis 在目标生产环境中的持久化与高可用方案。
- [ ] 第一版接口和用户字段是否满足实际业务。
- [ ] 是否接受按阶段提交和验收，不一次性生成全部代码。
