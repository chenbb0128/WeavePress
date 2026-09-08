# WeavePress 生产环境搭建与部署设计

日期：2026-09-09
目标：将 WeavePress 部署到腾讯云 4G 服务器 `124.220.53.160`，通过 `wp.pdurl.cn` 对外提供 HTTPS 服务，并接入现有 GitHub、NAS、Jenkins 与 ACR 发布链路。

## 1. 已确认范围

- 发布源码以 WeavePress `master` 分支的已提交版本为准。
- 公网域名为 `wp.pdurl.cn`，A 记录已解析到 `116.62.159.237`。
- 应用运行在 `124.220.53.160`，不迁移或改动同机其他项目。
- 首发包含 Go API、Worker、数据库迁移、Nuxt Web 和 Vue Admin。
- 首发暂不启用七牛云和微信公众号发布，使用 `staging` 环境与本地持久化素材目录。
- 七牛云凭据准备后再切换到 `production + qiniu`；微信公众号凭据通过同一类隐藏交互入口启用。

## 2. 方案比较与决定

### 方案 A：老服务器 HTTPS bridge（采用）

`wp.pdurl.cn` 先到 `116.62.159.237`，由公共 Nginx 复用 `*.pdurl.cn` 证书终止 HTTPS，再保留 Host 和转发信息代理到 `124.220.53.160`。该方案复用已经在 YYEduSystem、CityParking 等项目中验证的备案过渡模式，改动范围清晰。

代价是公网链路多一跳，并依赖两台服务器均可用。

### 方案 B：域名直连腾讯云服务器

链路更短，但当前需要额外处理腾讯云接入备案、443 入站和证书部署，不适合作为本次首发路径。

### 方案 C：仅 IP 预览

无需 DNS 和证书配置，但不能提供正式 HTTPS、Secure Cookie 和稳定域名，不满足正式入口要求。

## 3. 运行架构

```text
浏览器
  -> https://wp.pdurl.cn
  -> 116.62.159.237 公共 Nginx（TLS 终止）
  -> http://124.220.53.160（保留 Host 与 HTTPS 转发信息）
  -> 124.220.53.160 公共 Nginx
  -> weavepress-gateway
       -> /            Nuxt Web 静态站
       -> /admin/      Vue Admin 静态站
       -> /api/        weavepress-api
       -> /media/      weavepress-api
       -> /health      weavepress-api
       -> /ready       weavepress-api
       -> /metrics     404
  -> weavepress-worker
  -> 公共 MySQL / 公共 Redis
```

4G 服务器只增加 WeavePress 应用容器，不增加项目级 Nginx、MySQL 或 Redis。应用部署目录固定为 `/opt/apps/weavepress`。

## 4. 应用组件

### Server 组件

- API、Worker 和 Migrate 从同一份 Go 源码构建为独立、不可变的 commit-SHA 镜像。
- Migrate 是一次性任务，成功后才允许启动或切换 API、Worker。
- API 和 Worker 同时加入共享 `app` 与 `infra` 网络，以访问公共数据服务并保留采集、对象存储和微信接口所需的正常出站能力；Migrate 只加入 `infra`。
- API、Worker、Migrate 均不发布宿主机端口。
- API 配置健康检查，Worker 通过进程状态和任务日志验收。

### Gateway 组件

- 沿用仓库现有 `deploy/Dockerfile.gateway`，一次构建 Nuxt Web 与 Vue Admin。
- Gateway 加入共享 `app` 网络，不发布宿主机端口。
- 腾讯云公共 Nginx 将 `wp.pdurl.cn` 全站代理到 Gateway。
- Gateway 将 `/api/`、`/media/`、`/health` 和 `/ready` 转发到 API，并明确拒绝公网 `/metrics`。
- Gateway 与 Server 分开构建、分开发布，前端变更不重启 API/Worker。

## 5. 数据与持久化

- 公共 MySQL 新建独立 database `weavepress` 和同名最小权限业务用户。
- 公共 Redis 使用 DB `7`，key prefix 为 `weavepress:production`。
- 首发素材存放在 `/opt/apps/weavepress/data/media`，使用绑定挂载并保留备份路径。
- 首发期间不录入正式素材，避免后续切换七牛云时产生复杂迁移。
- 后续切换七牛云前，先盘点本地对象并完成对象上传、键一致性校验，再切换配置。
- 数据库 migration 只向前执行；应用回滚不自动执行破坏性数据库 down migration。

## 6. 配置与秘密

- `/opt/apps/weavepress/.env` 只保存秘密或机器特有值，权限为 `root:root 0600`。
- JWT 和媒体签名密钥在服务器本地生成，不输出到聊天、日志或 Git。
- 数据库密码、七牛 AK/SK、微信公众号 AppID/AppSecret 不写入仓库、Jenkins Pipeline 或构建产物。
- 提供服务器端隐藏交互安装器，用于后续录入七牛和微信公众号凭据，并在原子替换前备份旧配置。
- 首发使用 `WEAVEPRESS_APP_ENV=staging`、本地素材驱动和 Secure Cookie；七牛配置就绪后切换为 `production + qiniu`。
- 微信公众号功能默认关闭，启用前同时完成公众号出口 IP 白名单和接口连通性验证。

## 7. CI/CD

标准链路为：

```text
GitHub master
  -> GitHub Actions 同步 NAS bare mirror
  -> NAS post-receive hook 按路径识别组件
  -> Jenkins 从 NAS mirror 检出精确 APP_SHA
  -> NAS 构建 commit-SHA 镜像并推送 ACR
  -> Jenkins 通过 WeavePress 专用受限 SSH 入口通知生产部署
  -> 生产服务器临时登录 ACR、拉取指定镜像、迁移、切换并健康检查
```

Jenkins Folder 为 `WeavePress`，包含两套组件级入口：

- `WeavePressServer` 与 `ServerSyncGitHubAndDeploy`。
- `WeavePressGateway` 与 `GatewaySyncGitHubAndDeploy`。

Server job 负责 API、Worker、Migrate；Gateway job 负责 Web 与 Admin。生产发布只允许 `master`，镜像必须使用完整 commit SHA，不使用 `latest`。

## 8. 发布顺序与影响范围

1. 本地运行 Go、Admin、Web 的相关测试、静态检查和构建。
2. 增加生产部署配置、GitHub mirror workflow 和项目交接文档。
3. 推送 `master`，建立 NAS mirror、hook 和 Jenkins 组件级任务。
4. 在 `124.220.53.160` 创建 `/opt/apps/weavepress`、数据库、业务用户、配置和受限发布入口。
5. 部署 Server：拉取镜像、执行首次 migration、启动 API/Worker 并检查 `/ready`。
6. 部署 Gateway，检查 `/` 与 `/admin/`。
7. 在腾讯云公共 Nginx 增加 `wp.pdurl.cn` origin 站点并运行 `nginx -t`。
8. 在 `116.62.159.237` 公共 Nginx 增加 HTTPS bridge，复用现有 `*.pdurl.cn` 证书并运行 `nginx -t`。
9. 验证公网 HTTPS、登录页、静态资源、API、素材路径和运行版本。

生产影响仅限新增 WeavePress database/user、Redis DB 7 命名空间、应用目录、应用容器、两台服务器的独立 Nginx 站点文件、NAS/Jenkins/GitHub 对应项目配置。不会停止、删除或重建任何公共基础设施及其他项目容器。

## 9. 失败处理与回滚

- 所有 Compose、Nginx 和发布脚本写入前保留带时间戳备份，并在切换前做语法检查。
- 首次 migration 失败时不启动 API/Worker；保留日志并修复后重试，不自动删除数据库。
- API/Worker 或 Gateway 健康检查失败时恢复上一版镜像 SHA 和 Compose 配置。
- 新 Nginx 站点测试失败时不 reload；公网 bridge 失败时撤下 WeavePress 独立站点文件，不影响其他站点。
- 首发为新项目且无上一应用版本时，回滚目标是停止 WeavePress 应用容器并撤下两个独立站点，公共 MySQL/Redis/Nginx 保持运行；数据库和素材目录保留待排查，不自动删除。

## 10. 验证与完成标准

- Go：`go test ./...`、`go build ./...`、`go vet ./...` 通过。
- Admin：lint、typecheck、unit test、production build 通过。
- Web：lint、typecheck、production build 通过。
- 生产 Compose 配置校验和四类镜像构建通过。
- NAS mirror、hook、Jenkins Server/Gateway 构建与 ACR 推送通过。
- MySQL migration 成功；API 健康；Worker 运行；容器实际镜像 SHA 与发布 SHA 一致。
- 腾讯云本机 Host 路由的 `/ready`、`/`、`/admin/` 通过。
- 公网 `https://wp.pdurl.cn/ready`、首页、Admin 登录页和必要静态资源通过。
- `/metrics` 不向公网开放；数据库、Redis 和应用端口不直接暴露宿主机。
- 工作区项目登记、生产拓扑和 WeavePress 交接文档记录最终版本、路径、容器、域名、Jenkins job 与回滚方式。

## 11. 本次不包含

- 创建或配置七牛云 Bucket。
- 在聊天中接收或保存任何七牛、微信、数据库或管理员秘密。
- 自动群发微信公众号内容。
- 修改腾讯云安全组、域名备案或现有项目配置。
- 清理旧镜像、数据库、目录、容器或备份。
