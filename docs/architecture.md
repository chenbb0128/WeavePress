# 架构说明

WeavePress 首版采用模块化单体。API 和 Worker 共享领域、采集器、MySQL 仓储与对象存储适配器，分别作为独立进程部署。

```text
Nuxt 阅读前台 ─┐
                ├─ Nginx ─ Gin API ─ MySQL
Vue 管理后台 ──┘              ├─ Redis / Asynq ─ Worker
                              └─ /media 签名网关 ─ 七牛私有 Bucket
```

## 关键边界

- `internal/modules/authn`：JWT、Refresh Token 轮换、密码和权限码。
- `internal/modules/content`：链接提交、任务编排、素材归档和媒体签名。
- `internal/collectors`：安全请求、URL 规范化、微信与普通网页解析。
- `internal/modules/workspace/mysqlstore`：首版 MySQL 持久化实现。
- `internal/platform/objectstore`：本地文件和七牛云实现。
- `internal/transport/weaveapi`：HTTP 输入验证、鉴权和响应映射。

Access Token 有效期十五分钟并通过 Bearer Header 传递；Refresh Token 有效期三十天，仅以哈希形式入库，通过 HttpOnly Cookie 轮换。`admin` 拥有用户管理权限，`editor` 只拥有采集、重试和阅读权限。

采集任务使用固定业务状态，数据库事件表记录每次转换。Asynq 任务使用文章任务 ID、执行次数和手动重试次数构造确定性键，避免重复点击产生并行处理。

后续 AI、母稿和发布能力应作为新的领域模块及异步队列接入，不能把平台 SDK 或非官方连接器直接耦合进现有采集核心。
