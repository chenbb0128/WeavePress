# WeavePress AI 合规采编设计

日期：2026-09-09  
目标：基于单篇已采集文章，提供可追溯的两阶段 AI 分析与再创作能力，并将结果作为独立微信稿件进入现有人工审核和草稿箱发布流程。

## 1. 已确认范围

- 首版输入为一篇状态为 `ready` 的已采集文章。
- 采用两阶段工作流：先分析，再由编辑选择参数生成稿件。
- 模型通过可配置的 OpenAI-compatible 接口接入，不绑定单一厂商。
- 分析结果包含摘要、事实、观点、直接引用、风险和三个创作角度。
- 编辑选择创作角度、目标读者、语气和目标字数，并可填写补充要求。
- 生成成功后创建一篇新的 `editing` 微信稿件，不覆盖采集文章或已有稿件。
- 发布稿末尾由服务端自动添加参考来源；直接引用必须保留明确标记。
- AI 不得自动提交审核、审核通过或发布微信公众号草稿。
- 本功能用于合规再创作，不提供规避查重、隐藏抄袭来源或删除来源追溯的能力。

## 2. 方案比较与决定

### 方案 A：结构化双阶段流水线（采用）

第一次模型调用仅生成资料包，第二次调用根据编辑确认的创作参数生成稿件。两个阶段独立持久化、异步执行和审计，服务端验证所有结构化输出。

该方案比一次生成多一次交互，但编辑能在写稿前检查事实并选择角度，失败定位、成本记录和来源追溯也更清楚。

### 方案 B：单次调用同时分析和生成

开发和交互更简单，但无法在生成前确认事实与角度，分析与成稿错误也难以分开处理。

### 方案 C：多 Agent 采编链

将分析、核查、写作和审校拆成多次模型调用，质量上限更高，但调用成本、延迟、状态管理和错误面明显增加，不适合首版。

## 3. 架构边界

```text
已采集文章
  -> AI 分析任务
  -> 结构化资料包
  -> 编辑选择角度、读者、语气和字数
  -> AI 生成任务
  -> 服务端校验并渲染内容块
  -> 新建 editing 微信稿件
  -> 人工编辑、审核、发布
```

- `server/internal/modules/aiwriting`：分析和生成模型、状态机、校验、任务编排及审计。
- `server/internal/platform/llm`：统一 `Provider` 接口和 OpenAI-compatible HTTP 适配器。
- `server/internal/modules/workspace/mysqlstore`：AI 任务、分析资料包和生成结果的 MySQL 实现。
- `server/internal/workers`：注册 `ai:analyze` 和 `ai:generate` Asynq 任务。
- `server/internal/modules/editorial`：保持现有稿件职责，只接收验证后的生成结果并创建新稿件。
- `server/internal/transport/weaveapi`：提供 AI 状态、任务、分析和生成 API。
- `admin`：增加 AI 采编工作台和 AI 任务列表；Nuxt 阅读前台本轮不增加 AI 功能。

模型 SDK 和厂商错误不能进入 `content`、`editorial` 或采集器领域。后续更换模型只替换 `Provider` 实现和配置。

## 4. 数据模型

### `ai_jobs`

统一记录 `analysis` 和 `generation` 任务：

- 任务类型、来源文章、关联分析、请求人。
- 状态、执行次数、手动重试次数和请求幂等键。
- 输入指纹、provider、model 和 prompt version。
- input、output 和 total Token 数。
- 稳定错误码、用户可读错误、是否允许重试。
- 开始、结束、创建和更新时间。

状态固定为：

```text
queued -> running -> completed | failed
```

同一文章、相同 prompt version 和输入指纹的普通分析请求优先返回正在执行或最近已完成的结果；只有“重新分析”请求才创建新任务。生成接口要求客户端提供幂等键，同一幂等键只能创建一个生成任务。

### `ai_analyses`

每条记录关联一个完成的分析任务和来源文章，保存：

- `summary`
- `facts_json`
- `viewpoints_json`
- `quotes_json`
- `risks_json`
- `angles_json`

分析可以保留多个历史版本，不覆盖旧结果。

### `ai_generations`

每条记录关联分析任务和生成任务，保存：

- 选定角度 ID。
- 目标读者、语气、目标字数和补充要求。
- 模型返回的结构化内容块和内部事实映射。
- 服务端渲染后的标题、摘要和清洗 HTML。
- 成功创建的 `draft_id`。

`draft_id` 在生成记录中唯一。Worker 重试只能复用同一记录，不能重复创建稿件。

### `ai_job_events`

保存排队、开始、自动重试、手动重试、格式修复、完成和失败事件。事件不保存 API Key、完整提示词或完整模型输出。

## 5. 分析输出契约

模型必须返回 JSON 对象。正文内容块在发送模型前编号为稳定的 `B1`、`B2` 等来源 Block ID。

```json
{
  "summary": "文章摘要",
  "facts": [
    {
      "id": "F1",
      "text": "可验证事实",
      "sourceBlockIds": ["B3"],
      "confidence": "high"
    }
  ],
  "viewpoints": [
    {
      "id": "V1",
      "text": "文章观点",
      "holder": "作者或文中主体",
      "sourceBlockIds": ["B4"]
    }
  ],
  "quotes": [
    {
      "id": "Q1",
      "text": "原文中的连续原句",
      "sourceBlockId": "B6"
    }
  ],
  "risks": [
    {
      "id": "R1",
      "text": "无法确认、可能过时或明显主观的内容",
      "sourceBlockIds": ["B8"]
    }
  ],
  "angles": [
    {
      "id": "A1",
      "title": "创作角度",
      "thesis": "核心论点",
      "outline": ["引入", "主体", "结论"]
    }
  ]
}
```

服务端要求恰好三个创作角度，所有来源 Block ID 必须存在，直接引用必须能在对应原文 Block 中找到。`confidence` 只允许 `high`、`medium`、`low`。

## 6. 生成输出契约

生成请求包含已确认的分析资料包、选定角度和编辑参数。模型不直接返回 HTML，而是返回标题、摘要和受控内容块：

```json
{
  "title": "新稿标题",
  "digest": "新稿摘要",
  "blocks": [
    {
      "type": "heading",
      "level": 2,
      "text": "章节标题"
    },
    {
      "type": "paragraph",
      "text": "重新组织后的正文",
      "factIds": ["F1"]
    },
    {
      "type": "quote",
      "text": "明确标注的直接引用",
      "quoteId": "Q1"
    },
    {
      "type": "image",
      "assetId": 123,
      "alt": "来源图片说明"
    }
  ]
}
```

允许的内容块为 `heading`、`paragraph`、`quote`、`list` 和 `image`。事实 ID、引用 ID和素材 ID 必须存在于分析结果或来源文章中。AI 不生成作者字段，新稿作者默认留空，由编辑确认。

服务端按内容块渲染 HTML，使用现有清洗器处理，并在末尾追加包含原文章标题、公众号或站点名称及 canonical URL 的“参考来源”段落。非引用内容若与原文出现连续 80 个及以上相同字符，返回 `AI_EXCESSIVE_SOURCE_OVERLAP`，不创建稿件。

## 7. 任务与数据流

### 分析

1. 编辑对 `ready` 文章发起分析。
2. API 在事务中创建或复用任务，使用确定性 Asynq Task ID 入队。
3. Worker 将文章 Block 编号并构造受控输入。
4. Provider 调用模型，返回正文和 Token 用量。
5. 服务端解析并验证分析 JSON；合法后写入 `ai_analyses` 并完成任务。
6. 后台轮询任务详情并展示资料包。

### 生成

1. 编辑选择分析角度并提交结构化参数和幂等键。
2. API 校验参数和分析状态后创建生成任务。
3. Worker 调用模型并验证生成 JSON、事实映射、引用、素材和原文重合。
4. 服务端渲染、清洗正文并追加参考来源。
5. MySQL 事务同时创建 `editing` 稿件、初始稿件版本、稿件事件，并回填 `draft_id`。
6. 后台轮询完成后提供“打开稿件”入口，后续沿用现有审核和微信发布流程。

## 8. API 契约

所有接口继续使用 `{"code":0,"message":"success","data":{}}` 响应格式。

- `GET /api/ai/status`：返回是否启用、provider 和 model，不返回 API Key。
- `POST /api/articles/{id}/ai-analyses`：创建或复用分析；请求可使用 `force` 明确重新分析。
- `GET /api/articles/{id}/ai-analyses`：分页返回文章历史分析。
- `GET /api/ai-analyses/{id}`：返回资料包、任务和事件。
- `POST /api/ai-analyses/{id}/generations`：创建生成任务，请求包含角度、读者、语气、字数、补充要求和幂等键。
- `GET /api/ai-generations/{id}`：返回生成状态、用量、错误和 `draftId`。
- `GET /api/ai-jobs`：按任务类型、状态和文章筛选任务。
- `POST /api/ai-jobs/{id}/retry`：只重试最终失败且标记为 retryable 的任务。

权限码为：

- `ai:analysis:create`
- `ai:analysis:view`
- `ai:generation:create`
- `ai:generation:view`
- `ai:job:retry`

`admin` 和 `editor` 均拥有上述权限，服务端必须独立校验，不能只依赖菜单隐藏。

## 9. 管理后台

- 文章详情新增“AI 分析”入口；文章未就绪或 AI 未配置时禁用并显示原因。
- 新增 AI 采编工作台，展示来源文章、任务进度、摘要、事实、观点、引用、风险和三个创作角度。
- 创作参数使用结构化表单：角度单选、目标读者输入、语气选项、目标字数数值输入，以及最多 500 字的补充要求。
- 分析完成前不展示生成操作；生成进行中防止重复提交。
- 生成完成后展示标题、摘要、正文预览、Token 用量、耗时和“打开稿件”按钮。
- 新增 AI 任务列表，统一查看类型、来源文章、状态、模型、Token 用量、耗时、错误和重试入口。
- 任务进度首版使用轮询，不增加 WebSocket 或 SSE。

## 10. 模型配置与安全

新增配置：

```yaml
ai:
  enabled: false
  provider: openai-compatible
  base_url: https://api.example.com
  api_key: ""
  model: model-name
  request_timeout: 120s
  max_input_chars: 60000
  max_output_tokens: 6000
  temperature: 0.4
```

- 生产环境 `base_url` 必须使用 HTTPS。
- 功能启用时 `base_url`、`api_key` 和 `model` 必填。
- API Key 仅通过服务端环境变量注入，配置摘要、API、数据库和日志必须脱敏。
- Provider 使用标准库 HTTP 客户端调用可配置 `base_url` 下的 `/v1/chat/completions`。
- 文章正文作为带明确开始和结束边界的不可信数据传入；正文中的指令不得覆盖系统规则。
- 编辑补充要求最多 500 字，不能取消来源、事实、素材或安全约束。
- 输入超过 `max_input_chars` 时返回 `AI_INPUT_TOO_LARGE`，不静默截断。
- 普通日志不记录完整文章、提示词或模型输出；数据库通过文章 ID、prompt version 和结构化结果保留审计链。

首版不联网搜索和补充外部事实。模型不能确认的内容必须放入风险项，不能改写成确定事实。

## 11. 错误、重试与成本控制

- 网络超时、HTTP 429 和 5xx 为临时错误，Asynq 最多自动重试三次，间隔为 30 秒、2 分钟和 10 分钟。
- API Key、模型名、访问权限和请求参数错误为永久错误。
- 模型 JSON 无法解析或结构不合法时，使用低温度执行一次格式修复调用；仍不合法则以 `AI_OUTPUT_INVALID` 永久失败。
- 来源 Block、事实、引用或素材校验失败为永久错误，并在任务事件中记录稳定错误码和用户可读信息。
- Worker 发现任务已经完成时直接返回；发现 `running` 任务时允许恢复模型调用，但稿件创建始终通过数据库唯一约束和事务保证只执行一次。
- 每次原始调用和格式修复调用都累计 input、output 和 total Token 数。
- AI 未配置时 API 返回依赖不可用，不创建任务。

## 12. 测试策略

### Go 单元测试

- 分析和生成状态转换、自动重试、手动重试及不可重试状态。
- Prompt 边界、补充要求限制和系统约束不可被用户参数覆盖。
- 分析 JSON 字段、Block ID、引用原文和角度数量校验。
- 生成内容块、事实 ID、引用 ID、素材归属、长度和原文连续重合校验。
- OpenAI-compatible 请求头、模型参数、响应解析、Token 用量及 HTTP/厂商错误分类。
- 格式修复只执行一次，修复失败不会再次消耗额度。

### 集成测试

- 使用 Fake Provider 覆盖“文章 -> 分析 -> 生成 -> 新建稿件”，自动测试不访问外网。
- MySQL 覆盖任务复用、请求幂等、生成记录与稿件事务一致性、并发下只创建一篇稿件。
- Redis/Asynq 覆盖确定性 Task ID、Worker 重启恢复和失败任务重试。
- HTTP API 覆盖认证、权限、请求校验、分页和统一响应格式。

### 前端验证

- 对创作参数、轮询状态、错误展示和完成跳转提取可测试的视图模型。
- 运行 Admin lint、TypeScript 检查、单元测试和生产构建。
- 运行现有 Server 和 Web 全量测试、静态检查与生产构建，保证没有回归。

## 13. 验收标准

1. 编辑可以对一篇 `ready` 文章创建或复用 AI 分析任务。
2. 分析结果包含摘要、事实、观点、引用、风险和恰好三个创作角度。
3. 每个事实和引用都能定位到有效的原文 Block，直接引用能在原文中找到。
4. 编辑选择角度、读者、语气、字数和补充要求后可创建异步生成任务。
5. 生成结果通过服务端结构、事实、引用、素材和原文重合校验。
6. 成功结果创建一篇新的 `editing` 微信稿件并自动添加参考来源。
7. AI 生成任务不能自动提交审核、审核通过或发布。
8. 重复点击不会产生并行分析任务或重复稿件。
9. AI 未配置、输入过长、凭据错误、限流、网络失败和输出异常都有稳定错误码。
10. 管理后台可以查看任务模型、Token 用量、耗时、错误和审计事件。
11. 自动测试使用 Fake Provider；真实模型只执行一次受控联调。

## 14. 本轮不包含

- 多文章综合创作和跨来源事实合并。
- 联网搜索、外部事实核查和 RAG 知识库。
- 对话式反复修改、自动审校和多 Agent 工作流。
- 图片生成、OCR、音视频处理和小红书内容包。
- 规避查重、隐藏来源、自动审核或自动发布。
- Nuxt 阅读前台的 AI 操作入口。
