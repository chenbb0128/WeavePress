# AI 数据库配置设计

## 目标

在管理后台提供管理员专用的 AI 设置页面，支持智谱 GLM、OpenAI 和自定义 OpenAI-compatible 服务商。每个服务商分别保存 Base URL、Model 和 API Key；管理员切换服务商时无需重复输入 Key。保存后 API 与 Worker 从数据库动态读取并立即生效，不再从 `WEAVEPRESS_AI_*` 环境变量读取 AI 业务配置。

本轮只解决 AI 服务配置，不增加用量计费、模型价格同步、组织级 Key、多租户或外部 Secret Manager。

## 方案选择

采用数据库密文保存方案。与后台修改 `.env` 并重启容器相比，它不需要给 Web 进程服务器配置写权限，也不会因切换模型而重启任务进程；与外部 Secret Manager 相比，它能以较小改动完成当前单团队 MVP。

API Key 使用 AES-256-GCM 加密后写入数据库。加密密钥不能和密文保存在同一个数据库，因此使用现有 `auth.media_signing_key` 通过 HMAC-SHA256 和固定上下文 `weavepress/ai-provider-settings/v1` 派生 32 字节密钥。该方式不新增 AI 环境变量，也不要求管理员登录服务器录入 AI Key。更换 `media_signing_key` 前必须先在后台重新录入各服务商 Key，否则旧密文无法解密。

## 服务商与模型

服务端提供固定服务商目录，前端不自行拼接地址：

| Provider ID | 名称 | Base URL | 模型输入 |
| --- | --- | --- | --- |
| `zhipu` | 智谱 GLM | 固定为 `https://open.bigmodel.cn/api/paas/v4` | 默认 `glm-5.3-flash`，允许输入其他 Model ID |
| `openai` | OpenAI | 固定为 `https://api.openai.com/v1` | 提供 `gpt-5`、`gpt-5-mini` 常用项，允许输入其他 Model ID |
| `openai-compatible` | 自定义 OpenAI-compatible | 管理员填写 | 由管理员输入 Model ID |

自定义 Base URL 必须使用 HTTPS，不允许 UserInfo、query、fragment、非 443 端口、localhost、内网、回环、链路本地、组播、保留地址或云元数据地址。请求时使用受控 DNS 解析和 Dialer，再次阻止 DNS Rebinding；不跟随重定向。官方智谱和 OpenAI 地址也使用相同的受控 HTTP Client。

## 数据模型

新增 `ai_provider_settings`：

- `provider VARCHAR(32)`：主键，只允许 `zhipu`、`openai`、`openai-compatible`。
- `base_url VARCHAR(500)`：实际 API 根地址。
- `model VARCHAR(100)`：该服务商当前模型。
- `api_key_ciphertext VARBINARY(4096)`：版本化 AES-GCM 密文，内容为 `version || nonce || ciphertext`。
- `updated_by BIGINT UNSIGNED`：最后修改管理员。
- `created_at`、`updated_at`：UTC 时间。

新增单例表 `ai_runtime_settings`：

- `id TINYINT UNSIGNED`：固定为 `1`。
- `enabled BOOLEAN`：是否允许新建和执行 AI 任务。
- `active_provider VARCHAR(32)`：当前新任务使用的服务商。
- `updated_by BIGINT UNSIGNED`：最后修改管理员。
- `created_at`、`updated_at`：UTC 时间。

数据库只保存密文，不保存明文 Key、Key 尾号或请求 Header。迁移插入 `ai_runtime_settings(id=1, enabled=false, active_provider='zhipu')`，不预置任何 Key。

## 加密边界

新增独立 `aisettings` 模块：

- `Cipher.Encrypt(plaintext)` 生成随机 12 字节 nonce，并返回版本化密文。
- `Cipher.Decrypt(ciphertext)` 验证版本和 GCM Tag；失败只返回稳定内部错误，不包含密文或 Key。
- 设置读取接口只返回 `keyConfigured: true|false`，永远不返回密文、明文、长度或尾号。
- 更新请求中的 `apiKey` 为空表示保留数据库中的原 Key；服务商首次配置时启用 AI 必须提交非空 Key。
- Key 只在 HTTPS 请求绑定、服务端解密和构造上游 Authorization Header 的短生命周期内存在，不写入日志、错误、任务参数或浏览器存储。

## 领域接口与运行时读取

`aisettings.Service` 负责目录、查询、更新、验证和解密，使用独立 Store 接口。它对外提供两类结果：

- 管理视图：包含服务商目录、已保存 Base URL/Model、`keyConfigured`、启用状态和当前服务商。
- 运行时配置：包含当前或指定服务商的 Base URL、Model 和解密后的 API Key，只供服务端 AI 模块使用。

`aiwriting.Service` 不再持有启动时固定的 Provider。创建分析或生成任务时读取当前运行时设置，将当时的 Provider 和 Model 写入现有 `ai_jobs`。Worker 开始执行时：

1. 读取 `ai_runtime_settings.enabled`；关闭时以现有 `AI_NOT_CONFIGURED` 失败，不请求上游。
2. 根据任务记录的 Provider 读取对应服务商配置，因此切换当前服务商不会改变已排队任务的 Provider。
3. 使用任务记录的 Model 和该服务商最新 Base URL/API Key 构造一次 Provider Client。
4. 分析、生成和格式修复在同一次任务中复用该 Client。

管理员更换某服务商 Key 后，该服务商尚未开始执行的任务使用新 Key；任务已经运行时不受中途保存影响。关闭 AI 会阻止新任务，并让尚未执行的任务以不可重试的 `AI_NOT_CONFIGURED` 结束，便于发生 Key 泄露时立即止损。

请求超时、输入上限、输出 Token 上限和 Temperature 属于应用内部运行参数，使用代码中的受控默认值，不进入数据库，也不再通过 `WEAVEPRESS_AI_*` 环境变量覆盖。

## API

新增管理员专用接口：

- `GET /api/ai/settings`
- `PUT /api/ai/settings`

新增权限码：

- `ai:settings:view`
- `ai:settings:update`

两个权限只授予 `admin`，`editor` 不获得菜单和接口访问权。

`GET /api/ai/settings` 返回：

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "enabled": false,
    "activeProvider": "zhipu",
    "providers": [
      {
        "id": "zhipu",
        "name": "智谱 GLM",
        "baseUrl": "https://open.bigmodel.cn/api/paas/v4",
        "baseUrlEditable": false,
        "model": "glm-5.3-flash",
        "modelOptions": ["glm-5.3-flash"],
        "keyConfigured": false
      }
    ]
  }
}
```

`PUT /api/ai/settings` 请求：

```json
{
  "enabled": true,
  "activeProvider": "zhipu",
  "baseUrl": "https://open.bigmodel.cn/api/paas/v4",
  "model": "glm-5.3-flash",
  "apiKey": "new-secret-or-empty-to-keep"
}
```

一次更新只修改 `activeProvider` 对应的服务商行，并在同一事务中更新单例运行设置。启用时如果 Key、Model 或 Base URL 无效，返回 HTTP `400`，并沿用现有响应格式中的数字错误码 `10001`；数据库解密失败返回通用 `500`，不暴露加密细节。现有 `GET /api/ai/status` 改为读取数据库，只返回 `enabled`、`provider` 和 `model`。

本轮不增加“测试连接”接口，避免保存动作产生额外模型费用；管理员可保存后用一篇已就绪文章发起分析验证。

## 管理后台

在 AI 菜单下增加“AI 设置”，仅具有 `ai:settings:view` 的管理员可见。页面包含：

- “启用 AI”开关。
- 服务商下拉：智谱 GLM、OpenAI、自定义 OpenAI-compatible。
- Model 可搜索下拉，支持 `allow-create` 输入自定义 Model ID。
- 自定义服务商显示可编辑 Base URL；官方服务商显示只读地址。
- API Key 密码输入框；已配置时提示“已安全保存，留空不修改”，保存成功后立即清空输入框。
- 保存按钮；提交期间禁止重复操作，失败时保留非敏感表单值但清空 API Key。

切换服务商时展示该服务商各自保存的 Model、Base URL 和 `keyConfigured` 状态。页面不把 Key 放入 Pinia、localStorage、URL、日志或错误通知。

## 生产升级

停止读取并从本地 Compose、生产 Compose、示例配置和外部密钥安装脚本移除全部 AI 环境变量：

- `WEAVEPRESS_AI_ENABLED`
- `WEAVEPRESS_AI_PROVIDER`
- `WEAVEPRESS_AI_BASE_URL`
- `WEAVEPRESS_AI_API_KEY`
- `WEAVEPRESS_AI_MODEL`
- `WEAVEPRESS_AI_REQUEST_TIMEOUT`
- `WEAVEPRESS_AI_MAX_INPUT_CHARS`
- `WEAVEPRESS_AI_MAX_OUTPUT_TOKENS`
- `WEAVEPRESS_AI_TEMPERATURE`

当前生产环境的 AI Key 为空，因此无需迁移明文。发布 Server 时先执行新增数据库迁移，再启动 API/Worker；迁移后 AI 默认关闭。管理员随后通过后台分别录入服务商设置并启用。Gateway 发布增加 AI 设置页面。

## 验收

1. 管理员能分别保存智谱、OpenAI 和自定义服务商配置，切换后原配置仍在。
2. API Key 不出现在 GET 响应、日志、AI 任务表、浏览器存储或数据库明文字段中。
3. 留空 Key 保存会保留旧 Key；首次启用未配置 Key 的服务商会被拒绝。
4. `editor` 看不到 AI 设置菜单，访问设置 API 返回 `403`。
5. 保存并启用后，不重启 API/Worker即可创建分析；任务记录实际 Provider/Model，Worker 使用数据库中的对应 Key。
6. 切换当前服务商不改变已排队任务的 Provider；关闭 AI 会阻止新任务并停止尚未执行的任务。
7. 自定义 Base URL 无法访问内网、localhost、云元数据或非 HTTPS 地址。
8. 数据库迁移、AI 设置模块、AI 任务模块和管理页面的相关最小测试通过，管理端类型检查与生产构建通过。
