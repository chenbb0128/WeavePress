# WeavePress 生产部署

生产目录固定为 `/opt/apps/weavepress`。`compose.yaml`、`config.yaml` 和 `.env.example` 放在该目录；真实 `.env` 仅允许 `root:root 0600`。服务器脚本安装为：

- `/usr/local/sbin/initialize-weavepress`
- `/usr/local/sbin/deploy-weavepress`
- `/usr/local/sbin/weavepress-deploy-entrypoint`
- `/usr/local/sbin/weavepress-external-secrets-install`

## 首次初始化

以 root 运行 `initialize-weavepress`。它只在 `.env` 不存在、公共 MySQL 尚无 `weavepress` schema、公共 Redis DB 7 为空时继续。脚本在容器内部使用公共 MySQL/Redis 的环境变量，不向日志打印密码；随后创建最小权限的 `weavepress_app` 与迁移专用 `weavepress_migrator`，以及素材和备份目录。

初始化是一次性操作。任何 guard 失败都应先只读核验现状，不要删除 schema、清空 Redis 或覆盖 `.env` 来强行重跑。

## Jenkins 发布协议

Jenkins 使用专用 forced-command SSH key，远端命令只能是：

```text
<lowercase-40-char-commit-sha> --component=server
<lowercase-40-char-commit-sha> --component=gateway
```

标准输入必须恰好两行：ACR username、ACR password。两者都不得写入日志；受 Docker CLI 登录接口限制，username 会作为 `docker login --username` 参数短暂出现在本机进程参数中，password/token 则只能通过 `--password-stdin` 传入，绝不能进入 argv。发布脚本用临时 `DOCKER_CONFIG` 登录 ACR，完成后立即删除登录态。

Server 发布拉取同一 SHA 的 API、Worker、Migrate 镜像；数据库已有表时先在 `/opt/apps/weavepress/backup/mysql` 生成并验证 UTC gzip dump，再执行向前 migration，最后只重建 API/Worker。禁止在自动回滚中运行数据库 down migration。

Gateway 发布前要求现有 `weavepress-api` 为 healthy；切换 Gateway 时强制 `--no-deps`，避免 Gateway 的单一 `APP_SHA` 意外重建 API。切换后通过公共 Nginx 使用 `Host: wp.pdurl.cn` 验证首页。

成功发布的非敏感元数据原子写入 `/var/lib/zdzq-deploy/weavepress/server.env` 或 `gateway.env`。失败且存在完整旧版时只回滚本次请求组件，并复核恢复后的镜像与健康状态；首发没有完整旧版时会停止本次请求组件，明确报错，并保留数据库、素材目录和公共设施供排查。

## 备份与恢复

数据库备份位于 `backup/mysql/weavepress-<UTC>.<mktemp-unique>.sql.gz`。恢复前先停止应用写入，执行 `gzip -t`，在独立环境验证 dump，并经过变更审批后再导入。环境文件备份位于 `backup/env/.env.<UTC>.<mktemp-unique>`；唯一后缀避免同一秒内的并发备份互相覆盖。恢复时先验证 14 个键、权限与 Compose config，再原子替换 `.env`，随后显式发布 Server。任何备份都不得复制到聊天、构建日志或仓库。

## 切换七牛与微信

首发保持 `staging + local storage + WeChat disabled`。正式素材写入前先完成本地素材盘点/迁移，再在 root 的交互终端运行 `weavepress-external-secrets-install`。七牛 AK、SK、Bucket、Domain 必填；微信 AppID/AppSecret 必须同时填写或同时留空。所有凭据只通过隐藏输入终端录入。

安装器会先备份 `.env`，用临时文件生成严格 14 键的 `production + qiniu` 配置并执行 Compose config 校验，校验通过才原子替换。它不会 restart、recreate 或 deploy；复核后必须由操作员显式发布 Server。微信启用前还需完成出口 IP 白名单与接口连通性验证。
