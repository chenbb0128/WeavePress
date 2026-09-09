# WeavePress（织文）

> 多来源内容采集、AI 辅助采编与多平台分发系统。

WeavePress 将微信公众号和普通网页中的公开或已授权内容统一采集到团队内容库。当前版本已经建立账号、异步采集、正文结构化、图片归档、AI 辅助采编、微信稿件审核和写入公众号草稿箱的闭环。

## 当前版本

- 管理员创建内部账号，角色分为 `admin` 和 `editor`。
- 提交微信公众号公开文章或普通网页链接。
- Redis + Asynq 异步完成页面获取、正文解析和素材归档。
- 保存来源、原始 HTML 快照、结构化正文和图片。
- 同一规范化 URL 幂等导入，相同正文内容标记重复关系。
- Vue 管理后台查看仪表盘、任务、内容和用户。
- Nuxt 阅读前台提供适合桌面和移动端的集中阅读体验。
- 对已采集文章异步生成摘要、事实、观点、引用、风险和三个创作角度组成的可追溯资料包。
- 编辑确认角度、目标读者、语气和目标字数后，由 OpenAI-compatible provider 生成新的 `editing` 稿件。
- AI 任务列表展示状态、Token 用量、耗时和错误，并允许重试符合条件的失败任务。
- 从采集文章创建微信稿件，保存历史版本并经过人工审核。
- 使用微信公众号官方接口上传正文图片和封面，异步写入草稿箱。
- 本地开发使用文件存储，生产环境使用七牛云私有 Bucket。

当前采集范围是微信公众号与普通网页的公开内容，AI 仅接入 OpenAI-compatible provider；Nuxt 阅读前台本轮不增加 AI 操作。公众号自动群发、小红书等平台适配器、RSS、OCR、全文搜索、多租户和计费仍未实现。

## 工程结构

```text
WeavePress/
├── server/   # Go、Gin、MySQL、Redis、Asynq
├── admin/    # Vue 3 + Vben + Element Plus 管理后台
├── web/      # Nuxt 4 + Nuxt UI + Tailwind CSS 阅读前台
├── deploy/   # Docker Compose、Nginx 和统一入口
└── docs/     # 架构与采集安全说明
```

生产统一入口：`/` 为阅读前台，`/admin/` 为管理后台，`/api/` 为 API，`/media/` 为签名素材访问。

## 本地启动

环境要求：Go 1.25+、Node.js 24+、pnpm 11+、Docker Desktop。

1. Compose 启动时复制 `.env.example` 为 `.env`，至少替换数据库密码和两个签名密钥；本机直接运行 Go 时，另将 `server/configs/config.example.yaml` 复制为 `server/configs/config.yaml`，或显式设置对应的 `WEAVEPRESS_*` 环境变量。
2. 启动基础设施：

   ```powershell
   docker compose -f deploy/compose.yaml up -d mysql redis
   ```

3. 在 `server/` 执行迁移并创建首个管理员：

   ```powershell
   go run ./cmd/migrate -command up
   go run ./cmd/admin -username admin -password "请替换为强密码" -nickname "管理员"
   ```

4. 分别启动 API、Worker 和两个前端：

   ```powershell
   # server/
   go run ./cmd/api
   go run ./cmd/worker

   # admin/
   pnpm dev

   # web/
   pnpm dev
   ```

默认地址：API `http://localhost:8080`，管理后台 `http://localhost:5173`，阅读前台 `http://localhost:3000`。

也可以执行 `docker compose -f deploy/compose.yaml up -d --build`，通过 `http://localhost/` 使用统一入口。首次管理员可通过 Compose 中的 `server` 镜像手动执行 `admin` 目标，或在本机运行上述 CLI。

## AI 辅助采编流程

1. 采集微信公众号或普通网页中的公开、已授权文章，等待内容状态变为就绪。
2. 在管理后台发起 AI 分析，检查资料包中的摘要、事实、观点、直接引用、风险和创作角度。
3. 选择创作角度，并填写目标读者、语气和目标字数；生成任务完成后打开新建的 `editing` 稿件。
4. 人工逐条核对事实、引用和来源，确认版权与转载许可，完成编辑后走原有的审核、批准流程。
5. 配置好公众号官方凭据、接口权限和出口 IP 白名单后，由管理员手动写入公众号草稿箱。

分析与生成均为异步任务。管理后台可查看任务状态、Token 用量、耗时和安全处理后的错误；只有标记为可重试的失败任务才能手动重试。运行 AI 需要同时为 API 和 Worker 配置 OpenAI-compatible provider，并确保 Worker 监听 `ai` 队列，完整变量与禁用行为见 [server/README.md](server/README.md#ai-运行配置)。

WeavePress 将 AI 定位为“辅助采编/重构”工具，不承诺规避原创检测，也不鼓励隐去来源或未经授权改写。系统保留来源信息与溯源块，但 AI 仍可能产生幻觉；编辑必须核验每项事实和引文并尊重版权。生成结果不会自动提交审核、通过审核或发布，微信公众号官方凭据和相应权限仍需单独配置。

## 七牛云配置

生产环境设置：

```dotenv
WEAVEPRESS_APP_ENV=production
WEAVEPRESS_AUTH_COOKIE_SECURE=true
WEAVEPRESS_STORAGE_DRIVER=qiniu
WEAVEPRESS_STORAGE_QINIU_ACCESS_KEY=...
WEAVEPRESS_STORAGE_QINIU_SECRET_KEY=...
WEAVEPRESS_STORAGE_QINIU_BUCKET=...
WEAVEPRESS_STORAGE_QINIU_DOMAIN=https://private.example.com
```

Bucket 必须为私有空间。数据库只保存对象键，前端通过十分钟有效的 `/media/` 应用签名访问，服务端再生成一分钟有效的七牛私有下载地址。

## 微信公众号配置

在公众号后台启用开发能力、配置服务器出口 IP 白名单后，通过服务端环境变量注入凭据：

```dotenv
WEAVEPRESS_WECHAT_ENABLED=true
WEAVEPRESS_WECHAT_APP_ID=...
WEAVEPRESS_WECHAT_APP_SECRET=...
WEAVEPRESS_WECHAT_API_BASE=https://api.weixin.qq.com
WEAVEPRESS_WECHAT_REQUEST_TIMEOUT=30s
```

密钥不会写入数据库或返回前端。当前发布流程只创建公众号草稿，必须由管理员审核后手动触发，也不会自动群发。详见 [docs/wechat-publishing.md](docs/wechat-publishing.md)。

## 开发验证

```powershell
# server/
go tool sqlc generate
go test ./...
go build ./...
go vet ./...

# admin/
pnpm lint
pnpm check:type
pnpm test:unit
pnpm build

# web/
pnpm lint
pnpm typecheck
pnpm build
```

API 契约见 [server/api/openapi.yaml](server/api/openapi.yaml)，实现和安全边界见 [docs/architecture.md](docs/architecture.md) 与 [docs/collectors.md](docs/collectors.md)。

## 后续路线

1. 提供可视化富文本编辑和更完整的公众号排版组件。
2. 以独立适配器扩展小红书等平台的内容包与官方发布能力。
3. RSS、定时来源、主题聚类、发布日历和运营分析。

任何连接器都不能绕过验证码、登录或平台风控；正式发布始终优先使用官方授权接口并保留人工审核。
