# WeavePress Production Deployment Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将 WeavePress 以可重复发布、可回滚的方式部署到 `124.220.53.160`，并通过 `https://wp.pdurl.cn` 提供 Web、Admin、API 和素材访问。

**Architecture:** 老服务器 `116.62.159.237` 复用 `*.pdurl.cn` 证书终止 TLS，再保留 Host 转发到腾讯云公共 Nginx。腾讯云只运行 WeavePress API、Worker、Migrate 和 Gateway 应用容器，复用公共 MySQL、Redis、`app`/`infra` 网络；GitHub `master` 经 NAS mirror、Jenkins 和 ACR 发布 commit-SHA 镜像。

**Tech Stack:** Go 1.25、Gin、MySQL 8.4、Redis 7.4、Asynq、Vue 3、Nuxt 4、pnpm 11、Docker Compose、Nginx、GitHub Actions、Synology NAS、Jenkins、阿里云 ACR。

---

## 文件结构

### WeavePress 仓库 `C:\Develop\Hua\Projects\WeavePress`

- `deploy/nginx.conf`：Gateway 内部路由，补齐健康检查和指标拒绝规则。
- `deploy/tests/production-contract.sh`：验证 Gateway 与生产 Compose 的公开路由、共享基础设施和端口边界。
- `deploy/production/config.yaml`：非敏感生产/预览配置，首发固定为 `staging + local storage`。
- `deploy/production/.env.example`：秘密变量名称与安全示例，不含真实值。
- `deploy/production/compose.yaml`：四类应用镜像、资源限制、绑定挂载与外部网络。
- `deploy/production/nginx/new-server/wp.pdurl.cn.conf`：腾讯云 origin 站点。
- `deploy/production/nginx/old-server/wp.pdurl.cn.conf`：老服务器 HTTPS bridge。
- `deploy/production/scripts/initialize-weavepress`：首次创建数据库、用户、目录和 `.env`。
- `deploy/production/scripts/deploy-weavepress`：按 commit SHA 发布 Server/Gateway 并验证/回滚。
- `deploy/production/scripts/weavepress-deploy-entrypoint`：Jenkins forced-command 参数边界。
- `deploy/production/scripts/weavepress-external-secrets-install`：后续隐藏交互录入七牛/微信配置。
- `deploy/production/README.md`：部署、备份、恢复和凭据切换说明。
- `deploy/nas/post-receive`：NAS 按路径触发 Server/Gateway。
- `deploy/jenkins/WeavePressServer.groovy`：构建 API/Worker/Migrate，推送 ACR，发布 Server。
- `deploy/jenkins/WeavePressGateway.groovy`：构建并发布 Gateway。
- `deploy/jenkins/ServerSyncGitHubAndDeploy.groovy`：GitHub 同步 NAS 后触发 Server。
- `deploy/jenkins/GatewaySyncGitHubAndDeploy.groovy`：GitHub 同步 NAS 后触发 Gateway。
- `deploy/jenkins/jobs.json`：Jenkins Folder 和四个任务清单。
- `.github/workflows/ci-and-mirror.yml`：测试通过后同步 `master` 到 NAS。

### 服务器管理工作区

- `WeavePress-部署交接.md`：最终版本、拓扑、容器、发布入口和回滚。
- `生产变更记录-2026-09-09-WeavePress腾讯云首发.md`：本次操作与验证证据。
- `specs/project-registry.md`：登记 WeavePress。
- `specs/production-server-topology.md`：登记域名、容器、数据库和 Redis DB 7。
- `specs/README.md`：增加首发记录索引。

集中配置仓库 `C:\Develop\Hua\Projects\zdzq-deployment-config` 当前落后远端 20 个提交且包含 HoodlyJoy 用户改动，本计划不修改它，避免覆盖或混入无关状态；首发配置先由 WeavePress 仓库追踪，后续在集中配置仓库恢复干净状态后单独接入。

### Task 1: 建立可复现的基线验证

**Files:**
- Read: `server/go.mod`
- Read: `admin/package.json`
- Read: `web/package.json`
- Read: `deploy/compose.yaml`

- [ ] **Step 1: 记录发布基线**

Run:

```powershell
git status --short --branch
git rev-parse HEAD
git rev-parse origin/master
```

Expected: 工作区无未提交文件；`HEAD` 是设计/计划提交之后的完整 40 位 SHA，且本地 `master` 领先 `origin/master`。

- [ ] **Step 2: 验证 Go 服务**

Run:

```powershell
Set-Location server
go test ./...
go build ./...
go vet ./...
```

Expected: 三条命令 exit code 0。

- [ ] **Step 3: 验证 Admin**

Run:

```powershell
Set-Location admin
pnpm install --frozen-lockfile
pnpm lint
pnpm check:type
pnpm test:unit
pnpm build
```

Expected: lint、typecheck、Vitest 和 build 全部成功。

- [ ] **Step 4: 验证 Web**

Run:

```powershell
Set-Location web
pnpm install --frozen-lockfile
pnpm lint
pnpm typecheck
pnpm build
```

Expected: lint、typecheck 和 Nuxt build 全部成功。

### Task 2: 以契约测试补齐 Gateway 路由

**Files:**
- Create: `deploy/tests/production-contract.sh`
- Modify: `deploy/nginx.conf`

- [ ] **Step 1: 写入失败的 Gateway 路由契约**

Create `deploy/tests/production-contract.sh` with:

```bash
#!/usr/bin/env bash
set -Eeuo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
gateway="$repo_root/deploy/nginx.conf"
compose_file="$repo_root/deploy/production/compose.yaml"

grep -Fq 'location = /health {' "$gateway"
grep -Fq 'location = /ready {' "$gateway"
grep -Fq 'location = /metrics {' "$gateway"
grep -Fq 'return 404;' "$gateway"

if grep -Eq '^[[:space:]]+(mysql|redis|nginx):' "$compose_file"; then
  printf 'Production compose must reuse shared MySQL, Redis and Nginx.\n' >&2
  exit 1
fi
if grep -Eq '^[[:space:]]+ports:' "$compose_file"; then
  printf 'Application services must not publish host ports.\n' >&2
  exit 1
fi

docker run --rm --add-host api:127.0.0.1 \
  -v "$gateway:/etc/nginx/conf.d/default.conf:ro" \
  nginx:1.27-alpine nginx -t

APP_SHA=0000000000000000000000000000000000000000 \
  docker compose --env-file "$repo_root/deploy/production/.env.example" \
  -f "$compose_file" config --quiet
```

- [ ] **Step 2: 运行契约并确认失败**

Run: `bash deploy/tests/production-contract.sh`

Expected: FAIL，因为 `/health`、`/ready`、`/metrics` 或生产 Compose 尚不存在。

- [ ] **Step 3: 修改 Gateway 路由**

Add before `location /admin/` in `deploy/nginx.conf`:

```nginx
location = /health {
    proxy_pass http://api:8080;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $http_x_forwarded_proto;
}

location = /ready {
    proxy_pass http://api:8080;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $http_x_forwarded_proto;
}

location = /metrics {
    access_log off;
    return 404;
}
```

Also change `/api/` and `/media/` from `X-Forwarded-Proto $scheme` to:

```nginx
proxy_set_header X-Forwarded-Proto $http_x_forwarded_proto;
```

- [ ] **Step 4: 提交 Gateway 契约**

Run:

```powershell
git add deploy/nginx.conf deploy/tests/production-contract.sh
git commit -m "fix: 补齐生产网关健康路由"
```

Expected: 新提交仅包含 Gateway 配置和契约测试。

### Task 3: 创建生产 Compose 和非敏感配置

**Files:**
- Create: `deploy/production/config.yaml`
- Create: `deploy/production/.env.example`
- Create: `deploy/production/compose.yaml`
- Test: `deploy/tests/production-contract.sh`

- [ ] **Step 1: 写入 staging 配置**

Create `deploy/production/config.yaml` from `server/configs/config.example.yaml` with these exact production differences:

```yaml
app:
  name: weavepress
  env: staging
http:
  addr: ":8080"
  read_header_timeout: 5s
  read_timeout: 15s
  write_timeout: 30s
  idle_timeout: 60s
  shutdown_timeout: 10s
  max_header_bytes: 1048576
  max_body_bytes: 10485760
  trusted_proxies: ["172.16.0.0/12"]
  cors:
    allowed_origins: ["https://wp.pdurl.cn"]
    allowed_methods: [GET, POST, PUT, PATCH, DELETE, OPTIONS]
    allowed_headers: [Authorization, Content-Type, X-Request-ID, traceparent, tracestate]
    allow_credentials: true
database:
  enabled: true
  driver: mysql
  dsn: "overridden-by-environment"
  max_open_conns: 15
  max_idle_conns: 4
  conn_max_lifetime: 30m
  conn_max_idle_time: 5m
  ping_timeout: 3s
redis:
  enabled: true
  addr: "redis:6379"
  username: ""
  password: ""
  db: 7
  dial_timeout: 3s
  read_timeout: 2s
  write_timeout: 2s
  ping_timeout: 2s
  pool_size: 10
  min_idle_conns: 2
  key_prefix: "weavepress:production"
worker:
  enabled: false
  concurrency: 4
  shutdown_timeout: 10s
  queues: { collection: 2, publishing: 1, default: 1 }
auth:
  jwt_secret: "overridden-by-environment-at-least-32-characters"
  access_ttl: 15m
  refresh_ttl: 720h
  refresh_cookie: wp_refresh_token
  cookie_secure: true
  media_signing_key: "overridden-by-environment-at-least-32-characters"
  media_url_ttl: 10m
storage:
  driver: local
  local_dir: /app/data
  qiniu: { access_key: "", secret_key: "", bucket: "", domain: "" }
collector:
  page_max_bytes: 10485760
  image_max_bytes: 15728640
  article_max_bytes: 104857600
  max_images: 100
  request_timeout: 30s
  image_timeout: 30s
  image_concurrency: 4
  max_redirects: 5
  user_agent: "Mozilla/5.0 (compatible; WeavePress/1.0; +https://github.com/chenbb0128/WeavePress)"
wechat:
  enabled: false
  app_id: ""
  app_secret: ""
  api_base: "https://api.weixin.qq.com"
  request_timeout: 30s
observability:
  metrics: { enabled: true, path: /metrics, namespace: weavepress }
  tracing: { enabled: false, exporter: stdout, endpoint: localhost:4317, insecure: true, sample_ratio: 1.0 }
log: { level: info }
```

- [ ] **Step 2: 写入安全环境变量示例**

Create `deploy/production/.env.example`:

```dotenv
WEAVEPRESS_DB_APP_DSN=weavepress_app:replace-with-generated-password@tcp(mysql:3306)/weavepress
WEAVEPRESS_DB_MIGRATION_DSN=weavepress_migrator:replace-with-generated-password@tcp(mysql:3306)/weavepress
WEAVEPRESS_REDIS_PASSWORD=replace-from-shared-redis
WEAVEPRESS_AUTH_JWT_SECRET=replace-with-at-least-32-random-characters
WEAVEPRESS_AUTH_MEDIA_SIGNING_KEY=replace-with-another-32-character-secret
WEAVEPRESS_APP_ENV=staging
WEAVEPRESS_STORAGE_DRIVER=local
WEAVEPRESS_STORAGE_QINIU_ACCESS_KEY=
WEAVEPRESS_STORAGE_QINIU_SECRET_KEY=
WEAVEPRESS_STORAGE_QINIU_BUCKET=
WEAVEPRESS_STORAGE_QINIU_DOMAIN=
WEAVEPRESS_WECHAT_ENABLED=false
WEAVEPRESS_WECHAT_APP_ID=
WEAVEPRESS_WECHAT_APP_SECRET=
```

- [ ] **Step 3: 写入生产 Compose**

Create `deploy/production/compose.yaml` with services `migrate`, `api`, `worker`, `gateway`. Use:

```yaml
name: weavepress

x-server-environment: &server-environment
  WEAVEPRESS_DATABASE_DSN: ${WEAVEPRESS_DB_APP_DSN:?application DSN is required}
  WEAVEPRESS_REDIS_PASSWORD: ${WEAVEPRESS_REDIS_PASSWORD:?Redis password is required}
  WEAVEPRESS_AUTH_JWT_SECRET: ${WEAVEPRESS_AUTH_JWT_SECRET:?JWT secret is required}
  WEAVEPRESS_AUTH_MEDIA_SIGNING_KEY: ${WEAVEPRESS_AUTH_MEDIA_SIGNING_KEY:?media signing key is required}
  WEAVEPRESS_APP_ENV: ${WEAVEPRESS_APP_ENV:-staging}
  WEAVEPRESS_STORAGE_DRIVER: ${WEAVEPRESS_STORAGE_DRIVER:-local}
  WEAVEPRESS_STORAGE_QINIU_ACCESS_KEY: ${WEAVEPRESS_STORAGE_QINIU_ACCESS_KEY:-}
  WEAVEPRESS_STORAGE_QINIU_SECRET_KEY: ${WEAVEPRESS_STORAGE_QINIU_SECRET_KEY:-}
  WEAVEPRESS_STORAGE_QINIU_BUCKET: ${WEAVEPRESS_STORAGE_QINIU_BUCKET:-}
  WEAVEPRESS_STORAGE_QINIU_DOMAIN: ${WEAVEPRESS_STORAGE_QINIU_DOMAIN:-}
  WEAVEPRESS_WECHAT_ENABLED: ${WEAVEPRESS_WECHAT_ENABLED:-false}
  WEAVEPRESS_WECHAT_APP_ID: ${WEAVEPRESS_WECHAT_APP_ID:-}
  WEAVEPRESS_WECHAT_APP_SECRET: ${WEAVEPRESS_WECHAT_APP_SECRET:-}

x-server: &server
  environment: *server-environment
  volumes:
    - ./config.yaml:/app/configs/config.yaml:ro
    - ./data/media:/app/data
  networks: [app, infra]
  read_only: true
  tmpfs: [/tmp]
  security_opt: [no-new-privileges:true]

services:
  migrate:
    <<: *server
    image: registry.cn-hangzhou.aliyuncs.com/zdzq/weavepress-migrate:${APP_SHA:?APP_SHA is required}
    command: ["-command", "up"]
    environment:
      <<: *server-environment
      WEAVEPRESS_DATABASE_DSN: ${WEAVEPRESS_DB_MIGRATION_DSN:?migration DSN is required}
    networks: [infra]
    profiles: [migrate]
    restart: "no"
    mem_limit: 256m
    cpus: 0.50
    pids_limit: 128
  api:
    <<: *server
    image: registry.cn-hangzhou.aliyuncs.com/zdzq/weavepress-api:${APP_SHA:?APP_SHA is required}
    container_name: weavepress-api
    restart: unless-stopped
    mem_limit: 384m
    cpus: 0.75
    pids_limit: 128
    healthcheck:
      test: [CMD, /app/healthcheck, -url, http://127.0.0.1:8080/ready]
      interval: 10s
      timeout: 5s
      retries: 6
      start_period: 10s
  worker:
    <<: *server
    image: registry.cn-hangzhou.aliyuncs.com/zdzq/weavepress-worker:${APP_SHA:?APP_SHA is required}
    container_name: weavepress-worker
    environment:
      <<: *server-environment
      WEAVEPRESS_WORKER_ENABLED: "true"
    restart: unless-stopped
    mem_limit: 384m
    cpus: 0.50
    pids_limit: 128
  gateway:
    image: registry.cn-hangzhou.aliyuncs.com/zdzq/weavepress-gateway:${APP_SHA:?APP_SHA is required}
    container_name: weavepress-gateway
    networks: [app]
    read_only: true
    tmpfs: [/var/cache/nginx, /var/run, /tmp]
    security_opt: [no-new-privileges:true]
    restart: unless-stopped
    mem_limit: 128m
    cpus: 0.25
    pids_limit: 64
    depends_on:
      api: { condition: service_healthy }

networks:
  app: { external: true, name: app }
  infra: { external: true, name: infra }
```

- [ ] **Step 4: 运行生产契约**

Run: `bash deploy/tests/production-contract.sh`

Expected: Nginx syntax and Compose config pass; no project-level MySQL/Redis/Nginx or host ports are detected.

- [ ] **Step 5: 提交生产运行配置**

Run:

```powershell
git add deploy/production deploy/tests/production-contract.sh
git commit -m "feat: 增加 WeavePress 生产运行配置"
```

### Task 4: 实现受限初始化、发布和秘密切换脚本

**Files:**
- Create: `deploy/production/scripts/initialize-weavepress`
- Create: `deploy/production/scripts/deploy-weavepress`
- Create: `deploy/production/scripts/weavepress-deploy-entrypoint`
- Create: `deploy/production/scripts/weavepress-external-secrets-install`
- Create: `deploy/production/README.md`
- Test: `deploy/tests/production-contract.sh`

- [ ] **Step 1: 扩展脚本静态契约**

Append to `deploy/tests/production-contract.sh`:

```bash
for script in initialize-weavepress deploy-weavepress weavepress-deploy-entrypoint weavepress-external-secrets-install; do
  bash -n "$repo_root/deploy/production/scripts/$script"
done
grep -Fq 'DB_NAME="weavepress"' "$repo_root/deploy/production/scripts/initialize-weavepress"
grep -Fq 'redis-cli -a "$redis_password" -n 7 DBSIZE' "$repo_root/deploy/production/scripts/initialize-weavepress"
grep -Fq 'flock -w 1800' "$repo_root/deploy/production/scripts/deploy-weavepress"
grep -Fq '^[0-9a-f]{40}$' "$repo_root/deploy/production/scripts/deploy-weavepress"
```

Run once and expect FAIL because scripts do not exist.

- [ ] **Step 2: 实现首次初始化脚本**

`initialize-weavepress` must use `set -Eeuo pipefail`, `umask 077`, require root, and stop unless all of these read-only guards pass:

```bash
[[ ! -e /opt/apps/weavepress/.env ]]
[[ "$(docker exec redis sh -c 'redis-cli -a "$REDIS_PASSWORD" -n 7 DBSIZE' 2>/dev/null)" == "0" ]]
docker exec mysql mysql --user=root --execute="SELECT COUNT(*) FROM INFORMATION_SCHEMA.SCHEMATA WHERE SCHEMA_NAME='weavepress'"
```

Then generate hex-only application/migrator passwords and signing keys with `openssl rand -hex`, create `weavepress`, create users `weavepress_app` and `weavepress_migrator`, grant DML to the app user and DDL+DML to the migrator, create `/opt/apps/weavepress/{data/media,backup/mysql,backup/env}`, chown media to `65532:65532`, and atomically install `.env` as `0600`. The final `.env` contains exactly the keys declared in `.env.example`; no secret is printed. The API receives only `WEAVEPRESS_DB_APP_DSN`; only the Migrate service receives `WEAVEPRESS_DB_MIGRATION_DSN` as `WEAVEPRESS_DATABASE_DSN`.

- [ ] **Step 3: 实现部署脚本**

`deploy-weavepress` accepts:

```text
deploy-weavepress <40-char-app-sha> --component=server|gateway
```

It must acquire `/var/lock/weavepress-deploy.lock`, read ACR username/password from two stdin lines, use a temporary `DOCKER_CONFIG`, and perform these exact component actions:

```bash
case "$component" in
  server)
    pull weavepress-api, weavepress-worker, weavepress-migrate at "$app_sha"
    create a gzip MySQL backup when the database has tables
    APP_SHA="$app_sha" docker compose --profile migrate run --rm migrate
    APP_SHA="$app_sha" docker compose up -d --no-build --force-recreate api worker
    wait until weavepress-api is healthy
    ;;
  gateway)
    pull weavepress-gateway at "$app_sha"
    APP_SHA="$app_sha" docker compose up -d --no-build --force-recreate gateway
    curl -H 'Host: wp.pdurl.cn' http://127.0.0.1/
    ;;
esac
```

Before switching, capture current image IDs. On failure after switching, tag those IDs with a bounded rollback tag and recreate only the requested component. On success write `/var/lib/zdzq-deploy/weavepress/{server,gateway}.env` atomically with `APP_SHA`, image refs and UTC deployment time.

- [ ] **Step 4: 实现 forced-command 入口**

`weavepress-deploy-entrypoint` reads only `SSH_ORIGINAL_COMMAND`, accepts exactly one SHA and one `--component` argument, rejects control characters and additional arguments, validates lowercase 40-character SHA, then executes:

```bash
exec /usr/bin/sudo -n /usr/local/sbin/deploy-weavepress "$app_sha" "--component=$component"
```

- [ ] **Step 5: 实现后续外部凭据安装器**

`weavepress-external-secrets-install` requires an interactive root terminal, reads Qiniu AK/SK and WeChat AppID/AppSecret with `read -rsp`, never echoes values, creates a timestamped `.env` backup, rewrites through `mktemp`, sets `WEAVEPRESS_APP_ENV=production`, `WEAVEPRESS_STORAGE_DRIVER=qiniu`, the four Qiniu keys and optional WeChat keys, then runs Compose config validation. It must not restart containers automatically; the operator triggers an explicit Server deployment after review.

- [ ] **Step 6: 记录运维说明并提交**

Document paths, commands, backup/restore, staging-to-production transition, and the rule that credentials are entered only through a hidden terminal. Then run:

```powershell
bash deploy/tests/production-contract.sh
git add deploy/production deploy/tests/production-contract.sh
git commit -m "feat: 增加受限生产发布入口"
```

### Task 5: 增加 GitHub、NAS 和 Jenkins 自动发布配置

**Files:**
- Create: `.github/workflows/ci-and-mirror.yml`
- Create: `deploy/nas/post-receive`
- Create: `deploy/jenkins/WeavePressServer.groovy`
- Create: `deploy/jenkins/WeavePressGateway.groovy`
- Create: `deploy/jenkins/ServerSyncGitHubAndDeploy.groovy`
- Create: `deploy/jenkins/GatewaySyncGitHubAndDeploy.groovy`
- Create: `deploy/jenkins/jobs.json`

- [ ] **Step 1: 创建 GitHub Actions 门禁与 NAS mirror**

Workflow triggers on `master` and `workflow_dispatch`. Create jobs `server`, `admin`, `web`, `production-contract`, and `mirror`; `mirror` has `needs: [server, admin, web, production-contract]`, checks out full history, installs `${{ secrets.NAS_SSH_KEY }}` and `${{ secrets.NAS_KNOWN_HOSTS }}`, then executes:

```bash
export GIT_SSH_COMMAND="ssh -i ~/.ssh/nas_mirror_key -o BatchMode=yes -o IdentitiesOnly=yes -o StrictHostKeyChecking=yes"
git remote add nas "ssh://${NAS_USER}@${NAS_HOST}:${NAS_PORT}/volume1/docker/weavepress-git/WeavePress.git"
git push --force nas HEAD:refs/heads/master
```

Use Go 1.25, Node 24 and pnpm 11.21.0. Each component job runs the same commands as Task 1; the contract job runs `bash deploy/tests/production-contract.sh`.

- [ ] **Step 2: 创建 NAS hook**

`deploy/nas/post-receive` handles only `refs/heads/master`, derives changed paths, and maps:

```text
server/** or deploy/production/** -> WeavePress/WeavePressServer
admin/**, web/**, deploy/Dockerfile.gateway or deploy/nginx.conf -> WeavePress/WeavePressGateway
```

It reads Jenkins user/token from `/volume1/docker/weavepress-git/.secrets`, uses `curl --noproxy '*'`, passes `BRANCH=master`, exact `APP_SHA`, `DEPLOY=true`, and writes only non-secret audit data to `/volume1/docker/weavepress-git/post-receive.log`. Automatic dispatch remains disabled until `/volume1/docker/weavepress-git/.enable-auto-deploy` exists.

- [ ] **Step 3: 创建直接发布 Pipeline**

`WeavePressServer.groovy` builds three images with exact Docker targets:

```bash
docker build -f source/server/deployments/Dockerfile --build-arg TARGET=api -t registry.cn-hangzhou.aliyuncs.com/zdzq/weavepress-api:$APP_SHA source/server
docker build -f source/server/deployments/Dockerfile --build-arg TARGET=worker -t registry.cn-hangzhou.aliyuncs.com/zdzq/weavepress-worker:$APP_SHA source/server
docker build -f source/server/deployments/Dockerfile --build-arg TARGET=migrate -t registry.cn-hangzhou.aliyuncs.com/zdzq/weavepress-migrate:$APP_SHA source/server
```

`WeavePressGateway.groovy` builds:

```bash
docker build -f source/deploy/Dockerfile.gateway -t registry.cn-hangzhou.aliyuncs.com/zdzq/weavepress-gateway:$APP_SHA source
```

Both pipelines clone `ssh://chenhua@192.168.31.240/volume1/docker/weavepress-git/WeavePress.git`, enforce `master` for `DEPLOY=true`, retry ACR push up to three times, use Jenkins credential `weavepress-tencent-prod-ssh`, and send ACR credentials only over stdin. Server success is gated by migration completion, API container health and image-SHA verification; Gateway success additionally verifies `https://wp.pdurl.cn/ready` and `/` after both Nginx layers are active.

- [ ] **Step 4: 创建 GitHub 同步入口**

Both sync pipelines use credential `github-nas-mirror` and the existing Jenkins HTTPS proxy `http://192.168.31.227:7890`. They compare GitHub and NAS SHA; if different, clone GitHub `master` and push to NAS so the hook dispatches exactly once. If unchanged and `DEPLOY_WHEN_UNCHANGED=true`, they trigger only their matching direct job.

- [ ] **Step 5: 定义任务清单并提交**

Create `jobs.json`:

```json
{
  "folder": "WeavePress",
  "jobs": [
    {"name":"WeavePressServer","displayName":"后端 发布 NAS 代码镜像","pipeline":"WeavePressServer.groovy"},
    {"name":"ServerSyncGitHubAndDeploy","displayName":"后端 同步 GitHub 最新并发布","pipeline":"ServerSyncGitHubAndDeploy.groovy"},
    {"name":"WeavePressGateway","displayName":"前台与管理端 发布 NAS 代码镜像","pipeline":"WeavePressGateway.groovy"},
    {"name":"GatewaySyncGitHubAndDeploy","displayName":"前台与管理端 同步 GitHub 最新并发布","pipeline":"GatewaySyncGitHubAndDeploy.groovy"}
  ]
}
```

Run Groovy syntax/placeholder scans, `bash -n deploy/nas/post-receive`, then commit:

```powershell
git add .github deploy/nas deploy/jenkins
git commit -m "ci: 接入 WeavePress 自动发布链路"
```

### Task 6: 完整验证、推送源码并观察 GitHub Actions

**Files:**
- Verify: all files changed in Tasks 2-5

- [ ] **Step 1: 运行完整本地门禁**

Repeat Task 1 plus:

```powershell
bash deploy/tests/production-contract.sh
git diff --check
git status --short --branch
```

Expected: all checks pass and worktree is clean after commits.

- [ ] **Step 2: 推送 `master`**

Run: `git push origin master`

Expected: GitHub remote accepts all commits; local and `origin/master` SHAs match.

- [ ] **Step 3: 观察 GitHub Actions**

Use `gh run list` and `gh run view --log-failed` only if a check fails. Do not continue to production until Server、Admin、Web、production-contract all pass. Mirror may wait for GitHub repository secrets/NAS setup and is allowed to remain blocked until Task 8.

### Task 7: 对生产变更做最终只读门禁

**Files:**
- Read: live `/opt/AGENTS.md`
- Read: live `/opt/README.md`
- Read: live Compose/Nginx inventories

- [ ] **Step 1: 重新核验腾讯云**

Read only:

```bash
free -h
df -h /
docker compose ls
docker ps --format 'table {{.Names}}\t{{.Image}}\t{{.Status}}\t{{.Ports}}'
docker network inspect app infra
docker exec nginx nginx -t
docker exec redis sh -c 'redis-cli -a "$REDIS_PASSWORD" -n 7 DBSIZE'
```

Expected: shared services healthy, Redis DB 7 empty, `/opt/apps/weavepress` and `weavepress` containers absent.

- [ ] **Step 2: 重新核验老服务器与 DNS**

Read only:

```bash
docker exec docker-container-nginx-1 nginx -t
openssl x509 -in /data/docker/docker-container/nginx/ssl/pdurl.cn.crt -noout -subject -ext subjectAltName -dates
```

Resolve `wp.pdurl.cn` through at least two public resolvers and require `116.62.159.237`. Confirm no existing `wp.pdurl.cn` server block.

### Task 8: 初始化 NAS、GitHub、Jenkins 和腾讯云应用基础

**Files:**
- Install from: `deploy/nas/post-receive`
- Install from: `deploy/jenkins/*.groovy`
- Install from: `deploy/production/*`

- [ ] **Step 1: 声明生产影响范围**

Before writes, report that this task will create only WeavePress NAS mirror/hook/secrets, four Jenkins jobs, GitHub mirror secrets, ACR repositories/images, `/opt/apps/weavepress`, MySQL database/users, Redis DB 7 namespace, deploy account/forced command, and isolated Nginx site files. It will not stop or remove shared services or other projects.

- [ ] **Step 2: 初始化 NAS mirror**

Create `/volume1/docker/weavepress-git/WeavePress.git` as a bare repository, set `HEAD` to `refs/heads/master`, install the project-specific forced-command public key from `weavepress_ed25519.pub`, copy shared Jenkins callback credentials into the project `.secrets` directory without printing them, and leave `.enable-auto-deploy` absent. Then install and verify the hook, 0700 state, pending dispatcher, one-minute crontab, and fresh heartbeat in one command from the checked-out repository:

```bash
bash deploy/nas/install-dispatch-pending /path/to/checked-out/WeavePress
```

- [ ] **Step 3: 配置 GitHub repository secrets**

Set `NAS_HOST=nas.youquangou.cn`, `NAS_PORT=22222`, `NAS_USER` to the forced-command NAS identity, `NAS_KNOWN_HOSTS` from the verified NAS host key, and `NAS_SSH_KEY` from the matching private key. Use `gh secret set` with stdin; never echo values or include them in command output.

- [ ] **Step 4: 初始化腾讯云应用与数据库**

Generate a dedicated local key pair `weavepress_tencent_prod_ed25519` if it does not already exist; do not reuse the NAS mirror or another project's production identity. Copy production artifacts to a timestamped staging directory, verify SHA-256, install Compose/config/scripts atomically, create a dedicated `weavepress-deploy` user and forced-command authorized key, install a narrow sudoers rule for `/usr/local/sbin/deploy-weavepress`, then execute `/usr/local/sbin/initialize-weavepress` once. Verify `.env` is `0600`, database/users exist, Redis DB 7 remains empty before Worker starts, and no secret appears in logs.

- [ ] **Step 5: 创建 Jenkins 任务和凭据**

Back up any same-name Jenkins items first. Create Folder `WeavePress`, four jobs from `jobs.json`, and credential `weavepress-tencent-prod-ssh`. Compare the saved Pipeline scripts byte-for-byte or by SHA-256 against repository versions. Keep NAS auto-deploy disabled.

### Task 9: 首发应用与两层 Nginx

**Files:**
- Install: `deploy/production/nginx/new-server/wp.pdurl.cn.conf`
- Install: `deploy/production/nginx/old-server/wp.pdurl.cn.conf`

- [ ] **Step 1: 安装腾讯云 origin Nginx**

The origin config uses runtime Docker DNS resolution, so `nginx -t` can pass before the first application container exists:

```nginx
server {
    listen 80;
    server_name wp.pdurl.cn;
    resolver 127.0.0.11 valid=10s ipv6=off;
    set $weavepress_gateway http://weavepress-gateway:80;
    set_real_ip_from 116.62.159.237;
    real_ip_header X-Real-IP;
    real_ip_recursive on;
    location / {
        proxy_pass $weavepress_gateway;
        proxy_http_version 1.1;
        proxy_set_header Connection "";
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Host $host;
        proxy_set_header X-Forwarded-Proto $http_x_forwarded_proto;
    }
}
```

Install atomically, run `docker exec nginx nginx -t`, then reload only the Nginx container.

- [ ] **Step 2: 构建并发布 Server**

Trigger `WeavePress/WeavePressServer` with exact `APP_SHA`, `BRANCH=master`, `PUSH_ACR=true`, `DEPLOY=true`. Require three ACR pushes, migration success, healthy API, running Worker, matching image SHAs, and non-empty migration table list. On failure, inspect Jenkins console and production logs before changing anything. Do not require the public `/ready` path until Gateway is running.

- [ ] **Step 3: 构建并发布 Gateway**

Trigger `WeavePress/WeavePressGateway` at the same `APP_SHA` with `DEPLOY=true`. Verify `/`, `/admin/`, `/health`, `/ready` through `curl -H 'Host: wp.pdurl.cn' http://127.0.0.1/...`, and require `/metrics=404`.

- [ ] **Step 4: 安装老服务器 HTTPS bridge**

Install a dedicated `wp.pdurl.cn` site with HTTP-to-HTTPS redirect and this HTTPS proxy core:

```nginx
ssl_certificate /etc/nginx/ssl/pdurl.cn.crt;
ssl_certificate_key /etc/nginx/ssl/pdurl.cn.key;
location / {
    proxy_pass http://124.220.53.160;
    proxy_http_version 1.1;
    proxy_set_header Connection "";
    proxy_set_header Host wp.pdurl.cn;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Host $host;
    proxy_set_header X-Forwarded-Proto https;
}
```

Back up Nginx configuration, run `nginx -t`, reload only after success, and verify unrelated reference sites still return their pre-change status.

- [ ] **Step 5: 开启自动发布门禁**

After both direct jobs pass, create `/volume1/docker/weavepress-git/.enable-auto-deploy`. Test both manual sync jobs with `DEPLOY_WHEN_UNCHANGED=false`; expect success/no-op and no duplicate release.

### Task 10: 端到端验收与交接

**Files:**
- Create: `C:\Users\user\Documents\服务器管理\WeavePress-部署交接.md`
- Create: `C:\Users\user\Documents\服务器管理\生产变更记录-2026-09-09-WeavePress腾讯云首发.md`
- Modify: `C:\Users\user\Documents\服务器管理\specs\project-registry.md`
- Modify: `C:\Users\user\Documents\服务器管理\specs\production-server-topology.md`
- Modify: `C:\Users\user\Documents\服务器管理\specs\README.md`

- [ ] **Step 1: 公网功能验收**

Run without proxy:

```bash
curl --fail --silent --show-error https://wp.pdurl.cn/ready
curl --fail --silent --show-error -o /dev/null https://wp.pdurl.cn/
curl --fail --silent --show-error -o /dev/null https://wp.pdurl.cn/admin/
curl --silent --show-error -o /dev/null -w '%{http_code}' https://wp.pdurl.cn/metrics
curl --silent --show-error -o /dev/null -w '%{http_code}' http://wp.pdurl.cn/
```

Expected: ready=200、首页=200、Admin=200、metrics=404、HTTP=301。

- [ ] **Step 2: 安全与版本验收**

Verify application containers are non-root, have no host port bindings, respect memory/CPU limits, and use the exact ACR `APP_SHA`. Verify MySQL/Redis remain internal, `.env` is `0600`, recent logs contain no fatal/panic or secret values, and release metadata matches Jenkins.

- [ ] **Step 3: 管理员入口准备**

Install a root-only interactive command for `cmd/admin`. Do not generate or display an administrator password automatically. Tell the user to run the command in a private terminal when they are ready to create the first account.

- [ ] **Step 4: 更新交接与变更记录**

Record final APP_SHA, GitHub Actions run, Jenkins build numbers, ACR image names, production paths, containers, DNS/bridge topology, database name, Redis DB/prefix, backups, rollback commands, staging/local-storage limitation, and the later Qiniu/WeChat secret workflow. Do not include any secret value or DSN.

- [ ] **Step 5: 最终验证**

Run the entire verification suite again, perform `git diff --check` in every changed repository/workspace, and capture a fresh read-only production inventory. Only then report deployment complete.
