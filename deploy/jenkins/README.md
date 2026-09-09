# WeavePress Jenkins 发布门禁

创建或更新四个 Jenkins Pipeline Job 前，必须先在可访问 Jenkins 的受控主机运行 Declarative linter：

```bash
bash deploy/jenkins/validate-declarative-pipelines.sh \
  http://127.0.0.1:18080 /path/to/jenkins-user /path/to/jenkins-api-token
```

任一 Pipeline 未得到 Jenkins 精确的 `Jenkinsfile successfully validated.` 响应时，不得创建或更新 Job。

Direct Pipeline 在任何 ACR 登录、push 或生产发布前，还会调用固定的 Jenkins 本地程序：

```text
/var/jenkins_home/weavepress/bin/verify-acr-immutable-policy <full-repository>
```

该程序必须由生产接入任务安装，并通过阿里云受支持的只读 API/CLI 实时验证目标仓库的 immutable-tag policy。成功输出必须严格为：

```text
repository=<full-repository> immutable=true
```

程序缺失、超时、查询失败或输出不精确时，Pipeline fail closed，不执行 ACR push 或生产发布。不得用本地 manifest 比较代替 registry 侧 immutable policy。

当前生产 Docker 构建输入使用 2026-09-09 只读 registry manifest 查询得到的 manifest-list digest：

- `node:24-alpine@sha256:e67514e5d0f6c46656005e1b693b2ec9d52e80b641307de684d4a015ba7a4eaf`
- `gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab`
- `docker/dockerfile:1@sha256:ecfaec9ed6d810b56388c508f4121597bfbba70d41a6dfeee4d8cad5f295fc32`

刷新时必须先用 `docker buildx imagetools inspect <tag>` 做只读查询，记录 manifest-list `Digest`，再同时更新 Dockerfile 与离线契约；禁止凭记忆或本地浮动 tag 生成 digest。
