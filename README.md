# WeavePress（织文）

> 多来源内容采集、AI 辅助采编与多平台分发系统。

WeavePress 将微信公众号和普通网页中的公开或已授权内容统一采集到团队内容库。当前版本已经建立账号、异步采集、正文结构化、图片归档和集中阅读的基础闭环；AI 采编和公众号、小红书分发将在此基础上继续扩展。

## 当前版本

- 管理员创建内部账号，角色分为 `admin` 和 `editor`。
- 提交微信公众号公开文章或普通网页链接。
- Redis + Asynq 异步完成页面获取、正文解析和素材归档。
- 保存来源、原始 HTML 快照、结构化正文和图片。
- 同一规范化 URL 幂等导入，相同正文内容标记重复关系。
- Vue 管理后台查看仪表盘、任务、内容和用户。
- Nuxt 阅读前台提供适合桌面和移动端的集中阅读体验。
- 本地开发使用文件存储，生产环境使用七牛云私有 Bucket。

暂不包含 AI 生成、公众号草稿箱、小红书发布、RSS、OCR、全文搜索、多租户和计费。

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

1. AI 摘要、观点、事实和引用提取，形成可追溯的资料包。
2. 原创母稿编辑、版本管理、人工审核与公众号排版预览。
3. 使用微信公众号官方接口写入草稿箱。
4. 生成小红书标题、文案、标签和图片卡片素材包。
5. RSS、定时来源、主题聚类、发布日历和运营分析。

任何连接器都不能绕过验证码、登录或平台风控；正式发布始终优先使用官方授权接口并保留人工审核。
