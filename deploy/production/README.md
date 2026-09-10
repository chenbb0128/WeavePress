# WeavePress 生产部署

生产目录固定为 `/opt/apps/weavepress`。`compose.yaml`、`config.yaml` 和 `.env.example` 放在该目录；真实 `.env` 仅允许 `root:root 0600`。服务器脚本安装为：

- `/usr/local/sbin/initialize-weavepress`
- `/usr/local/sbin/deploy-weavepress`
- `/usr/local/sbin/weavepress-deploy-entrypoint`
- `/usr/local/sbin/weavepress-external-secrets-install`

## NAS 自动触发安装

创建并核验 bare mirror、准备好 hook 使用的凭据后，在 NAS 上从已检出的 WeavePress 仓库执行一次：

```bash
bash deploy/nas/install-dispatch-pending /path/to/checked-out/WeavePress
```

该命令会安装并验证 `post-receive`、0700 state/bin 目录和 `dispatch-pending`。启用门禁后，`master` push 会由 NAS hook 直接请求 Jenkins，与其他项目保持一致；不依赖 cron 或 heartbeat。连接 Jenkins 明确失败时会保留 pending 状态和日志，等待后续 push 或操作员手动执行 `dispatch-pending --scheduled` 重试。安装器不会创建 `.enable-auto-deploy`，自动发布仍需在其他生产前置条件验收后显式启用。

## 首次初始化

以 root 运行 `initialize-weavepress`。它只在 `.env` 不存在、公共 MySQL 尚无 `weavepress` schema、公共 Redis DB 7 为空时继续。脚本在容器内部使用公共 MySQL/Redis 的环境变量，不向日志打印密码；随后创建最小权限的 `weavepress_app` 与迁移专用 `weavepress_migrator`，以及素材和备份目录。

初始化是一次性操作。任何 guard 失败都应先只读核验现状，不要删除 schema、清空 Redis 或覆盖 `.env` 来强行重跑。

## Jenkins 发布协议

Jenkins 使用专用 forced-command SSH key，远端命令只能是：

```text
<lowercase-40-char-commit-sha> --component=server
<lowercase-40-char-commit-sha> --component=gateway
```

标准输入使用严格、有界、带超时的 framing。前两行固定为 ACR username、ACR password；Server 随后按 API、Worker、Migrate 顺序接收三个 `sha256:<64hex>` manifest digest，Gateway 随后接收一个 Gateway manifest digest，并要求输入在最后一个 digest 换行后立即 EOF。远端命令 argv 仍只有 `APP_SHA --component=server|gateway`，凭据和 digest 都不进入 SSH 命令参数。凭据不得写入日志；受 Docker CLI 登录接口限制，username 会作为 `docker login --username` 参数短暂出现在生产机本地进程参数中，password/token 则只能通过 `--password-stdin` 传入，绝不能进入 argv。发布脚本用临时 `DOCKER_CONFIG` 登录 ACR，按 Jenkins 提供的 manifest digest 拉取、核验 revision label/RepoDigest/image ID 后再绑定 Compose 使用的 SHA tag，完成后立即删除登录态。

Server 发布拉取同一 SHA 的 API、Worker、Migrate 镜像；数据库已有表时先在 `/opt/apps/weavepress/backup/mysql` 生成并验证 UTC gzip dump，再执行向前 migration，最后只重建 API/Worker。禁止在自动回滚中运行数据库 down migration。

Gateway 发布前要求现有 `weavepress-api` 为 healthy；切换 Gateway 时强制 `--no-deps`，避免 Gateway 的单一 `APP_SHA` 意外重建 API。Gateway 镜像在响应中固定暴露 `X-WeavePress-Release: <APP_SHA>`；Jenkins 切换后对公网 `/ready` 与 `/` 同时要求精确 HTTP 200 且该 header 等于本次 APP_SHA，从而确认新版本已穿过两层 Nginx。

成功发布的非敏感元数据原子写入 `/var/lib/zdzq-deploy/weavepress/server.env` 或 `gateway.env`。失败且存在完整旧版时只回滚本次请求组件，并复核恢复后的镜像与健康状态；首发没有完整旧版时会停止本次请求组件，明确报错，并保留数据库、素材目录和公共设施供排查。

## 备份与恢复

数据库备份位于 `backup/mysql/weavepress-<UTC>.<mktemp-unique>.sql.gz`。恢复前先停止应用写入，执行 `gzip -t`，在独立环境验证 dump，并经过变更审批后再导入。环境文件备份位于 `backup/env/.env.<UTC>.<mktemp-unique>`；唯一后缀避免同一秒内的并发备份互相覆盖。恢复时先验证 14 个键、权限与 Compose config，再原子替换 `.env`，随后显式发布 Server。任何备份都不得复制到聊天、构建日志或仓库。

## 切换七牛与微信

首发保持 `staging + local storage + WeChat disabled`。正式素材写入前先完成本地素材盘点/迁移，再在 root 的交互终端运行 `weavepress-external-secrets-install`。七牛 AK、SK、Bucket、Domain 必填；微信 AppID/AppSecret 必须同时填写或同时留空。所有凭据只通过隐藏输入终端录入。

安装器会先备份 `.env`，用临时文件生成严格 14 键的 `production + qiniu` 配置并执行 Compose config 校验，校验通过才原子替换。它不会 restart、recreate 或 deploy；复核后必须由操作员显式发布 Server。微信启用前还需完成出口 IP 白名单与接口连通性验证。
