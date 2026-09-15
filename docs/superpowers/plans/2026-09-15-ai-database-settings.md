# AI 数据库配置实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 管理员可在后台分别保存智谱 GLM、OpenAI 和自定义 OpenAI-compatible 配置，API 与 Worker 无需重启即可使用数据库中的当前配置。

**Architecture:** 新增独立 `aisettings` 领域模块与 MySQL Store，用 AES-256-GCM 加密 API Key；`aiwriting` 在创建任务时解析当前 Provider/Model，在执行任务时解析该 Provider 最新密钥并创建单任务 LLM Client。管理端通过管理员专用设置 API 编辑配置，出站请求统一使用阻断私网和重定向的安全 HTTP Client。

**Tech Stack:** Go 1.25、Gin、MySQL 8.4、Goose、Asynq、Vue 3、TypeScript、Element Plus、Vitest

**Spec:** `docs/superpowers/specs/2026-09-15-ai-database-settings-design.md`

## Global Constraints

- 只支持 `zhipu`、`openai`、`openai-compatible` 三个 Provider ID。
- API Key 只以 AES-256-GCM 密文入库；GET、日志、任务参数和浏览器存储均不得出现 Key。
- 密钥由 `auth.media_signing_key` 使用 HMAC-SHA256 和上下文 `weavepress/ai-provider-settings/v1` 派生，不增加生产依赖。
- 空 `apiKey` 表示保留原 Key；首次启用当前 Provider 必须已有或提交非空 Key。
- 自定义 Base URL 只允许公网 HTTPS 443，无 UserInfo、query、fragment；请求时重新解析 DNS 且不跟随重定向。
- 任务创建记录当时的 Provider/Model；Worker 使用任务 Provider/Model 和数据库中的最新 Base URL/API Key。
- 删除全部 `WEAVEPRESS_AI_*` 环境变量；请求超时、输入上限、输出上限和 Temperature 使用代码默认值。
- 仅执行本功能直接相关的 Go 测试、Vitest、管理端类型检查和单次生产构建。

---

### Task 1: 数据表与 AI 设置持久化

**Files:**
- Create: `server/database/migrations/20260915000100_create_ai_provider_settings.sql`
- Create: `server/internal/modules/aisettings/model.go`
- Create: `server/internal/modules/aisettings/store.go`
- Create: `server/internal/modules/workspace/mysqlstore/aisettings.go`
- Create: `server/internal/modules/workspace/mysqlstore/aisettings_test.go`

**Interfaces:**
- Produces: `aisettings.Store`, `aisettings.StoredRuntime`, `aisettings.StoredProvider`, `aisettings.StoreUpdate`
- Produces: `mysqlstore.NewAISettingsStore(*sql.DB) *AISettingsStore`

- [ ] **Step 1: 写 MySQL Store 的失败测试**

使用现有 MySQL 测试辅助方式验证默认单例、三个 Provider 分别保存，以及 `PreserveKey=true` 不覆盖密文。测试核心断言：

```go
func TestAISettingsStoreUpdatePreservesExistingKey(t *testing.T) {
    db, _ := layoutTestDatabase(t, 20260915000100)
    if _, err := db.Exec(`INSERT INTO users (id, username, password_hash) VALUES (1, 'ai-settings', 'unused')`); err != nil {
        t.Fatal(err)
    }
    store := mysqlstore.NewAISettingsStore(db)
    ctx := context.Background()

    first := aisettings.StoreUpdate{
        Enabled: true, ActiveProvider: aisettings.ProviderZhipu,
        Provider: aisettings.ProviderZhipu,
        BaseURL: "https://open.bigmodel.cn/api/paas/v4",
        Model: "glm-5.3-flash", APICiphertext: []byte("cipher-one"), UpdatedBy: 1,
    }
    if err := store.Update(ctx, first); err != nil { t.Fatal(err) }
    first.Model = "glm-custom"
    first.APICiphertext = nil
    first.PreserveKey = true
    if err := store.Update(ctx, first); err != nil { t.Fatal(err) }

    saved, err := store.GetProvider(ctx, aisettings.ProviderZhipu)
    if err != nil { t.Fatal(err) }
    if saved.Model != "glm-custom" { t.Fatalf("model = %q", saved.Model) }
    if !bytes.Equal(saved.APICiphertext, []byte("cipher-one")) { t.Fatal("key was overwritten") }
}
```

- [ ] **Step 2: 运行 Store 测试并确认失败**

Run: `cd server; go test ./internal/modules/workspace/mysqlstore -run AISettings -count=1`

Expected: FAIL，提示迁移、`aisettings` 类型或 `NewAISettingsStore` 尚不存在。

- [ ] **Step 3: 增加迁移和领域存储接口**

迁移创建两个表并插入关闭状态：

```sql
-- +goose Up
CREATE TABLE ai_provider_settings (
    provider VARCHAR(32) NOT NULL,
    base_url VARCHAR(500) NOT NULL,
    model VARCHAR(100) NOT NULL,
    api_key_ciphertext VARBINARY(4096) NULL,
    updated_by BIGINT UNSIGNED NOT NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (provider),
    CONSTRAINT fk_ai_provider_settings_user FOREIGN KEY (updated_by) REFERENCES users (id),
    CONSTRAINT chk_ai_provider_settings_provider CHECK (provider IN ('zhipu', 'openai', 'openai-compatible'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE ai_runtime_settings (
    id TINYINT UNSIGNED NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    active_provider VARCHAR(32) NOT NULL,
    updated_by BIGINT UNSIGNED NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    CONSTRAINT fk_ai_runtime_settings_user FOREIGN KEY (updated_by) REFERENCES users (id),
    CONSTRAINT chk_ai_runtime_settings_singleton CHECK (id = 1),
    CONSTRAINT chk_ai_runtime_settings_provider CHECK (active_provider IN ('zhipu', 'openai', 'openai-compatible'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

INSERT INTO ai_runtime_settings (id, enabled, active_provider, updated_by)
VALUES (1, FALSE, 'zhipu', NULL);

-- +goose Down
DROP TABLE ai_runtime_settings;
DROP TABLE ai_provider_settings;
```

领域接口固定为：

```go
type Store interface {
    GetRuntime(context.Context) (StoredRuntime, error)
    ListProviders(context.Context) ([]StoredProvider, error)
    GetProvider(context.Context, string) (StoredProvider, error)
    Update(context.Context, StoreUpdate) error
}

type StoreUpdate struct {
    Enabled, PreserveKey bool
    ActiveProvider, Provider, BaseURL, Model string
    APICiphertext []byte
    UpdatedBy uint64
}
```

- [ ] **Step 4: 实现事务更新**

`AISettingsStore.Update` 在同一事务中执行 Provider upsert 和单例更新；Key 保留使用参数化 SQL：

```sql
INSERT INTO ai_provider_settings
    (provider, base_url, model, api_key_ciphertext, updated_by)
VALUES (?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE
    base_url = VALUES(base_url),
    model = VALUES(model),
    api_key_ciphertext = IF(?, api_key_ciphertext, VALUES(api_key_ciphertext)),
    updated_by = VALUES(updated_by)
```

随后执行：

```sql
UPDATE ai_runtime_settings
SET enabled = ?, active_provider = ?, updated_by = ?
WHERE id = 1
```

- [ ] **Step 5: 运行 Store 测试并提交**

Run: `cd server; go test ./internal/modules/workspace/mysqlstore -run AISettings -count=1`

Expected: PASS。

```bash
git add server/database/migrations/20260915000100_create_ai_provider_settings.sql server/internal/modules/aisettings server/internal/modules/workspace/mysqlstore/aisettings.go server/internal/modules/workspace/mysqlstore/aisettings_test.go
git commit -m "feat: 增加 AI 服务配置存储"
```

---

### Task 2: API Key 加密、服务商目录与设置服务

**Files:**
- Create: `server/internal/modules/aisettings/cipher.go`
- Create: `server/internal/modules/aisettings/cipher_test.go`
- Create: `server/internal/modules/aisettings/service.go`
- Create: `server/internal/modules/aisettings/service_test.go`
- Modify: `server/internal/modules/aisettings/model.go`

**Interfaces:**
- Consumes: Task 1 的 `aisettings.Store`
- Produces: `NewCipher(string) (*Cipher, error)`、`Cipher.Encrypt([]byte)`、`Cipher.Decrypt([]byte)`
- Produces: `Service.View`、`Service.Update`、`Service.Active`、`Service.ForJob`

- [ ] **Step 1: 写加密和服务行为的失败测试**

覆盖随机 nonce、篡改失败、Key 不出现在视图、空 Key 保留、各 Provider 独立、启用前校验：

```go
func TestCipherRoundTripUsesRandomNonce(t *testing.T) {
    cipher, err := NewCipher(strings.Repeat("m", 32))
    if err != nil { t.Fatal(err) }
    left, _ := cipher.Encrypt([]byte("secret"))
    right, _ := cipher.Encrypt([]byte("secret"))
    if bytes.Equal(left, right) { t.Fatal("ciphertexts must use different nonces") }
    plain, err := cipher.Decrypt(left)
    if err != nil { t.Fatal(err) }
    if string(plain) != "secret" { t.Fatalf("plain = %q", plain) }
}

func TestUpdateEmptyKeyPreservesConfiguredKey(t *testing.T) {
    store := newFakeStoreWithProvider(ProviderZhipu, []byte("existing-cipher"))
    service := newTestService(t, store)
    _, err := service.Update(context.Background(), 7, UpdateInput{
        Enabled: true, ActiveProvider: ProviderZhipu,
        Model: "glm-5.3-flash", APIKey: "",
    })
    if err != nil { t.Fatal(err) }
    if !store.lastUpdate.PreserveKey || store.lastUpdate.APICiphertext != nil {
        t.Fatalf("unexpected update: %#v", store.lastUpdate)
    }
}
```

- [ ] **Step 2: 运行模块测试并确认失败**

Run: `cd server; go test ./internal/modules/aisettings -count=1`

Expected: FAIL，提示 Cipher、Service 和目录类型不存在。

- [ ] **Step 3: 实现 AES-256-GCM**

派生和密文格式固定为版本字节、12 字节 nonce、GCM ciphertext：

```go
const cipherVersion byte = 1
const keyContext = "weavepress/ai-provider-settings/v1"

func NewCipher(mediaSigningKey string) (*Cipher, error) {
    if len(mediaSigningKey) < 32 { return nil, ErrCipherUnavailable }
    mac := hmac.New(sha256.New, []byte(mediaSigningKey))
    _, _ = mac.Write([]byte(keyContext))
    block, err := aes.NewCipher(mac.Sum(nil))
    if err != nil { return nil, ErrCipherUnavailable }
    gcm, err := cipher.NewGCM(block)
    if err != nil { return nil, ErrCipherUnavailable }
    return &Cipher{aead: gcm}, nil
}
```

`Decrypt` 对错误版本、长度不足或 GCM Tag 失败统一返回 `ErrDecryptFailed`，不拼接输入内容。

- [ ] **Step 4: 实现目录、视图和运行时解析**

固定目录：

```go
var catalog = []ProviderDefinition{
    {ID: ProviderZhipu, Name: "智谱 GLM", BaseURL: "https://open.bigmodel.cn/api/paas/v4", DefaultModel: "glm-5.3-flash", ModelOptions: []string{"glm-5.3-flash"}},
    {ID: ProviderOpenAI, Name: "OpenAI", BaseURL: "https://api.openai.com/v1", DefaultModel: "gpt-5-mini", ModelOptions: []string{"gpt-5", "gpt-5-mini"}},
    {ID: ProviderOpenAICompatible, Name: "自定义 OpenAI-compatible", BaseURLEditable: true},
}
```

服务公开方法：

```go
func (s *Service) View(ctx context.Context) (SettingsView, error)
func (s *Service) Update(ctx context.Context, userID uint64, input UpdateInput) (SettingsView, error)
func (s *Service) Active(ctx context.Context) (RuntimeConfig, error)
func (s *Service) ForJob(ctx context.Context, provider, model string) (RuntimeConfig, error)
```

`Active` 和 `ForJob` 均在 `enabled=false`、Key 缺失、Model 缺失或密文解密失败时返回无敏感信息的稳定错误。`ForJob` 用传入的任务 Model 覆盖 Provider 当前 Model。

- [ ] **Step 5: 运行模块测试并提交**

Run: `cd server; go test ./internal/modules/aisettings -count=1`

Expected: PASS。

```bash
git add server/internal/modules/aisettings
git commit -m "feat: 增加 AI 配置加密与运行时解析"
```

---

### Task 3: AI 出站请求 SSRF 防护

**Files:**
- Create: `server/internal/platform/netguard/http.go`
- Create: `server/internal/platform/netguard/http_test.go`
- Modify: `server/internal/platform/llm/openai.go`
- Modify: `server/internal/platform/llm/openai_test.go`

**Interfaces:**
- Produces: `netguard.ValidateHTTPSURL(context.Context, Resolver, string) error`
- Produces: `netguard.NewHTTPClient(time.Duration, Resolver) *http.Client`
- Consumes: `llm.NewOpenAICompatible` 默认使用受保护 Client

- [ ] **Step 1: 写 URL 与 Dialer 的失败测试**

表驱动测试拒绝 `http://`、UserInfo、query、fragment、非 443 端口、localhost、`127.0.0.1`、`169.254.169.254` 和解析到私网的域名；允许解析到公网地址的 HTTPS URL：

```go
func TestValidateHTTPSURLRejectsPrivateTargets(t *testing.T) {
    resolver := fakeResolver{"private.example": {netip.MustParseAddr("10.0.0.8")}}
    for _, raw := range []string{
        "http://public.example/v1", "https://user@public.example/v1",
        "https://public.example:8443/v1", "https://127.0.0.1/v1",
        "https://169.254.169.254/latest/meta-data", "https://private.example/v1",
    } {
        if err := ValidateHTTPSURL(context.Background(), resolver, raw); err == nil {
            t.Fatalf("accepted unsafe URL %q", raw)
        }
    }
}
```

另测 `CheckRedirect` 总是返回 `http.ErrUseLastResponse`，Dialer 每次重新解析且只连接本次解析到的公网 IP。

- [ ] **Step 2: 运行网络与 LLM 测试并确认失败**

Run: `cd server; go test ./internal/platform/netguard ./internal/platform/llm -count=1`

Expected: FAIL，提示 `netguard` 包或安全 Client 不存在。

- [ ] **Step 3: 实现公网地址判断和受控 Client**

接口和 Dialer 固定为：

```go
type Resolver interface {
    LookupNetIP(context.Context, string, string) ([]netip.Addr, error)
}

func NewHTTPClient(timeout time.Duration, resolver Resolver) *http.Client {
    if resolver == nil { resolver = net.DefaultResolver }
    transport := http.DefaultTransport.(*http.Transport).Clone()
    transport.Proxy = nil
    transport.DialContext = restrictedDialer(resolver)
    return &http.Client{
        Transport: transport, Timeout: timeout,
        CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
    }
}
```

地址判断覆盖 IPv4/IPv6 私网、回环、链路本地、组播、未指定、文档保留段和云元数据地址。

- [ ] **Step 4: 让 LLM 默认构造器使用安全 Client**

```go
func NewOpenAICompatible(baseURL, apiKey, model string, timeout time.Duration) Provider {
    return newOpenAICompatibleWithClient(
        baseURL, apiKey, model,
        netguard.NewHTTPClient(timeout, nil),
    )
}
```

`aisettings.Service` 保存自定义地址时调用同一 `ValidateHTTPSURL`，官方地址保持服务端固定值。

- [ ] **Step 5: 运行网络与 LLM 测试并提交**

Run: `cd server; go test ./internal/platform/netguard ./internal/platform/llm ./internal/modules/aisettings -count=1`

Expected: PASS。

```bash
git add server/internal/platform/netguard server/internal/platform/llm server/internal/modules/aisettings
git commit -m "feat: 限制 AI 服务出站网络"
```

---

### Task 4: AI 任务动态读取数据库配置

**Files:**
- Modify: `server/internal/modules/aiwriting/service.go`
- Modify: `server/internal/modules/aiwriting/service_test.go`
- Modify: `server/internal/modules/aiwriting/model.go`
- Modify: `server/internal/app/api.go`
- Modify: `server/internal/app/worker.go`

**Interfaces:**
- Consumes: `aisettings.Service.Active` 和 `aisettings.Service.ForJob`
- Produces: `aiwriting.SettingsResolver`、`aiwriting.ProviderFactory`
- Changes: `AIService.Status(context.Context) (aiwriting.Status, error)`

- [ ] **Step 1: 写动态解析的失败测试**

保留现有 aiwriting 测试并把固定 `config.AIConfig` Fake 改为 Resolver Fake。新增三个关键场景：

```go
func TestStartAnalysisRecordsActiveProviderAndModel(t *testing.T) {
    resolver := &fakeSettingsResolver{active: aisettings.RuntimeConfig{
        Enabled: true, Provider: aisettings.ProviderZhipu,
        Model: "glm-5.3-flash", BaseURL: "https://open.bigmodel.cn/api/paas/v4", APIKey: "secret",
    }}
    service := newServiceWithResolver(t, resolver)
    _, _, err := service.StartAnalysis(context.Background(), 7, 42, false)
    if err != nil { t.Fatal(err) }
    if service.store.createAnalysisInput.Provider != aisettings.ProviderZhipu || service.store.createAnalysisInput.Model != "glm-5.3-flash" {
        t.Fatalf("input = %#v", service.store.createAnalysisInput)
    }
}

func TestWorkerUsesJobProviderModelAndLatestKey(t *testing.T) {
    resolver := &fakeSettingsResolver{forJob: aisettings.RuntimeConfig{
        Enabled: true, Provider: aisettings.ProviderOpenAI,
        Model: "gpt-5", BaseURL: "https://api.openai.com/v1", APIKey: "new-key",
    }}
    service := newServiceWithResolver(t, resolver)
    // 执行任务后断言 resolver 收到 job.Provider/job.Model，factory 收到 new-key。
}
```

第三个场景断言关闭设置后 `StartAnalysis` 不创建任务，已排队任务以不可重试 `AI_NOT_CONFIGURED` 失败。

- [ ] **Step 2: 运行 aiwriting 测试并确认失败**

Run: `cd server; go test ./internal/modules/aiwriting -run 'StartAnalysis|StartGeneration|WorkerUses|NotConfigured|Status' -count=1`

Expected: FAIL，因为 Service 仍持有启动时 Provider 和启用配置。

- [ ] **Step 3: 改造 Service 依赖与创建任务路径**

固定依赖：

```go
type SettingsResolver interface {
    Active(context.Context) (aisettings.RuntimeConfig, error)
    ForJob(context.Context, string, string) (aisettings.RuntimeConfig, error)
}

type ProviderFactory func(aisettings.RuntimeConfig) llm.Provider

func New(store Store, articles ArticleStore, queue Enqueuer,
    settings SettingsResolver, providerFactory ProviderFactory,
    limits Limits) *Service
```

`Limits` 只含 `RequestTimeout`、`MaxInputChars`、`MaxOutputTokens`、`Temperature`，提供 `DefaultLimits()`；创建分析、生成和重试前调用 `Active`，并把返回的 Provider/Model 写入 job。

- [ ] **Step 4: 改造 Worker 执行路径**

`processAnalysis`、`processGeneration` 开头执行：

```go
runtime, err := s.settings.ForJob(ctx, job.Provider, job.Model)
if err != nil { return normalizeSettingsError(err) }
provider := s.providerFactory(runtime)
if provider == nil { return ErrNotConfigured }
```

把同一个 `provider` 参数传给主请求和该任务的格式修复函数，确保单任务内不因管理员中途保存而混用客户端。

- [ ] **Step 5: 组装 API 与 Worker**

`app.NewAPI` 和 `Worker.Run` 都构造：

```go
settingsStore := mysqlstore.NewAISettingsStore(db.SQL)
settingsService, err := aisettings.New(settingsStore, cfg.Auth.MediaSigningKey, func(ctx context.Context, raw string) error {
    return netguard.ValidateHTTPSURL(ctx, nil, raw)
})
```

Worker ProviderFactory：

```go
func newAIProvider(runtime aisettings.RuntimeConfig, timeout time.Duration) llm.Provider {
    return llm.NewOpenAICompatible(runtime.BaseURL, runtime.APIKey, runtime.Model, timeout)
}
```

- [ ] **Step 6: 运行相关测试并提交**

Run: `cd server; go test ./internal/modules/aiwriting ./internal/app -count=1`

Expected: PASS。

```bash
git add server/internal/modules/aiwriting server/internal/app
git commit -m "feat: 动态加载 AI 运行配置"
```

---

### Task 5: 管理员 AI 设置 API 与权限

**Files:**
- Create: `server/internal/transport/weaveapi/ai_settings.go`
- Create: `server/internal/transport/weaveapi/ai_settings_test.go`
- Modify: `server/internal/transport/weaveapi/ai.go`
- Modify: `server/internal/transport/weaveapi/ai_test.go`
- Modify: `server/internal/transport/weaveapi/api.go`
- Modify: `server/internal/modules/authn/service.go`
- Modify: `server/api/openapi.yaml`

**Interfaces:**
- Consumes: `aisettings.Service.View` 和 `aisettings.Service.Update`
- Produces: `GET /api/ai/settings`、`PUT /api/ai/settings`
- Produces: `ai:settings:view`、`ai:settings:update`

- [ ] **Step 1: 写 API 与权限失败测试**

测试管理员可读写，editor 两个接口均 403，GET 不出现 Key，PUT 严格拒绝未知字段：

```go
func TestAISettingsRoutesRequireAdmin(t *testing.T) {
    for _, method := range []string{http.MethodGet, http.MethodPut} {
        recorder := performAISettingsRequest(t, workspace.RoleEditor, method, `/api/ai/settings`, validSettingsJSON())
        if recorder.Code != http.StatusForbidden { t.Fatalf("status = %d", recorder.Code) }
    }
}

func TestGetAISettingsNeverReturnsAPIKey(t *testing.T) {
    recorder := performAdminAISettingsRequest(t, http.MethodGet, `/api/ai/settings`, ``)
    if recorder.Code != http.StatusOK { t.Fatalf("status = %d", recorder.Code) }
    if strings.Contains(recorder.Body.String(), "secret-value") || !strings.Contains(recorder.Body.String(), `"keyConfigured":true`) {
        t.Fatalf("unsafe or incomplete response: %s", recorder.Body.String())
    }
}
```

- [ ] **Step 2: 运行 API 测试并确认失败**

Run: `cd server; go test ./internal/transport/weaveapi -run 'AISettings|AIStatus|Permissions' -count=1`

Expected: FAIL，路由与权限码尚不存在。

- [ ] **Step 3: 增加接口、路由和错误映射**

输入结构：

```go
type aiSettingsInput struct {
    Enabled bool `json:"enabled"`
    ActiveProvider string `json:"activeProvider"`
    BaseURL string `json:"baseUrl"`
    Model string `json:"model"`
    APIKey string `json:"apiKey"`
}
```

路由同时使用角色和权限码：

```go
protected.GET("/ai/settings", a.requireRole(workspace.RoleAdmin), a.requireCode(codeAISettingsView), a.getAISettings)
protected.PUT("/ai/settings", a.requireRole(workspace.RoleAdmin), a.requireCode(codeAISettingsUpdate), a.updateAISettings)
```

`aiStatus` 改为传入 request context；`aisettings.ErrInvalidSettings` 映射 HTTP 400/`code=10001`，解密与存储错误只走通用 HTTP 500。

- [ ] **Step 4: 更新权限与 OpenAPI**

admin 权限增加：

```go
"ai:settings:view", "ai:settings:update",
```

editor 权限保持原样。OpenAPI 定义 `AISettingsView`、`AIProviderView`、`UpdateAISettingsRequest`，并明确 `apiKey` 仅出现在 PUT request schema。

- [ ] **Step 5: 运行接口测试和 OpenAPI 检查并提交**

Run: `cd server; go test ./internal/transport/weaveapi ./internal/modules/authn -run 'AISettings|AIStatus|Permissions' -count=1`

Run: `cd server; redocly lint api/openapi.yaml`

Expected: PASS。

```bash
git add server/internal/transport/weaveapi server/internal/modules/authn/service.go server/api/openapi.yaml
git commit -m "feat: 提供管理员 AI 设置接口"
```

---

### Task 6: 管理端 AI 设置页面

**Files:**
- Modify: `admin/apps/web-ele/src/api/ai.ts`
- Create: `admin/apps/web-ele/src/views/ai/settings.vue`
- Create: `admin/apps/web-ele/src/views/ai/settings.test.ts`
- Modify: `admin/apps/web-ele/src/router/routes/modules/content.ts`
- Modify: `admin/apps/web-ele/src/locales/langs/zh-CN/page.json`
- Modify: `admin/apps/web-ele/src/locales/langs/en-US/page.json`

**Interfaces:**
- Consumes: `GET /ai/settings` 和 `PUT /ai/settings`
- Produces: 管理员专用 `/ai/settings` 页面

- [ ] **Step 1: 写页面失败测试**

Mock API 后验证初始化、Provider 切换、官方 URL 只读、自定义 URL 可编辑、Key 保存后清空：

```ts
it('keeps each provider form and clears the submitted key', async () => {
  mocks.getAISettingsApi.mockResolvedValue(settingsFixture());
  mocks.updateAISettingsApi.mockResolvedValue(settingsFixture(true));
  const { host } = mountComponent(AISettings);
  await flushPromises();

  selectProvider(host, 'openai');
  setInput(host, 'API Key', 'sk-new');
  buttonByText(host, '保存设置')?.click();
  await flushPromises();

  expect(mocks.updateAISettingsApi).toHaveBeenCalledWith(expect.objectContaining({
    activeProvider: 'openai', apiKey: 'sk-new', model: 'gpt-5-mini',
  }));
  expect(inputByLabel(host, 'API Key')?.value).toBe('');
});
```

另测 API 失败后 Key 也清空，Provider/Model/Base URL 保留。

- [ ] **Step 2: 运行页面测试并确认失败**

Run: `cd admin; pnpm vitest run apps/web-ele/src/views/ai/settings.test.ts --dom`

Expected: FAIL，设置 API 和组件尚不存在。

- [ ] **Step 3: 增加 TypeScript 契约与请求函数**

```ts
export type AIProviderId = 'openai' | 'openai-compatible' | 'zhipu';

export interface AIProviderSettings {
  baseUrl: string;
  baseUrlEditable: boolean;
  id: AIProviderId;
  keyConfigured: boolean;
  model: string;
  modelOptions: string[];
  name: string;
}

export interface AISettings {
  activeProvider: AIProviderId;
  enabled: boolean;
  providers: AIProviderSettings[];
}

export function getAISettingsApi() {
  return requestClient.get<AISettings>('/ai/settings');
}

export function updateAISettingsApi(input: UpdateAISettingsInput) {
  return requestClient.put<AISettings>('/ai/settings', input);
}
```

- [ ] **Step 4: 实现设置页面和管理员菜单**

页面沿用 `wp-page-hero`、`wp-panel` 和主题 CSS 变量，不写死绿色。表单使用 `ElSwitch`、`ElSelect filterable allow-create`、`ElInput type="password" show-password`；`finally` 中始终执行 `form.apiKey = ''`。

路由：

```ts
{
  name: 'AISettings',
  path: '/ai/settings',
  component: () => import('#/views/ai/settings.vue'),
  meta: {
    authority: ['admin'], icon: 'lucide:settings-2', order: 5,
    title: $t('page.ai.settingsTitle'),
  },
}
```

- [ ] **Step 5: 运行页面测试与类型检查并提交**

Run: `cd admin; pnpm vitest run apps/web-ele/src/views/ai/settings.test.ts --dom`

Run: `cd admin; pnpm -F @nova/admin run typecheck`

Expected: PASS。

```bash
git add admin/apps/web-ele/src/api/ai.ts admin/apps/web-ele/src/views/ai/settings.vue admin/apps/web-ele/src/views/ai/settings.test.ts admin/apps/web-ele/src/router/routes/modules/content.ts admin/apps/web-ele/src/locales/langs
git commit -m "feat: 增加 AI 服务设置页面"
```

---

### Task 7: 移除 AI 环境变量并做最小闭环验证

**Files:**
- Modify: `server/internal/config/config.go`
- Modify: `server/internal/config/loader.go`
- Modify: `server/internal/config/validate.go`
- Modify: `server/internal/config/loader_test.go`
- Modify: `server/internal/config/validate_test.go`
- Modify: `server/.env.example`
- Modify: `server/compose.yaml`
- Modify: `deploy/production/compose.yaml`
- Modify: `deploy/production/scripts/initialize-weavepress`
- Modify: `deploy/production/scripts/weavepress-external-secrets-install`
- Modify: `deploy/tests/production-compose-contract.py`
- Modify: `deploy/tests/production-scripts-contract.sh`
- Modify: `README.md`

**Interfaces:**
- Consumes: Task 1 至 6 的完整闭环
- Produces: 不含 `WEAVEPRESS_AI_*` 的运行与部署契约

- [ ] **Step 1: 写配置失败测试**

设置旧环境变量后加载配置，断言配置摘要不再包含 AI 业务项，并扫描生产 Compose：

```go
func TestLoadIgnoresLegacyAIEnvironmentVariables(t *testing.T) {
    t.Setenv("WEAVEPRESS_AI_ENABLED", "true")
    t.Setenv("WEAVEPRESS_AI_API_KEY", "must-not-load")
    t.Setenv("WEAVEPRESS_AI_REQUEST_TIMEOUT", "1s")
    cfg, err := Load("")
    if err != nil { t.Fatal(err) }
    for key := range cfg.SanitizedSummary() {
        if strings.HasPrefix(key, "ai_") { t.Fatalf("legacy AI config remains: %s", key) }
    }
}
```

生产契约改为断言 `WEAVEPRESS_AI_` 不存在于 Compose 环境和生成的 `.env`。

- [ ] **Step 2: 运行配置和部署契约测试并确认失败**

Run: `cd server; go test ./internal/config -run AI -count=1`

Run: `python deploy/tests/production-compose-contract.py deploy/production/compose.yaml`

Expected: FAIL，旧 AI 环境变量仍被绑定或输出。

- [ ] **Step 3: 删除环境绑定并固化内部限制**

删除 `Config.AI`、`AIConfig`、AI 校验和所有 `ai.*` Viper 绑定；运行限制集中到 `aiwriting.DefaultLimits()`：

```go
const (
    DefaultAIRequestTimeout = 120 * time.Second
    DefaultAIMaxInputChars = 60000
    DefaultAIMaxOutputTokens = 6000
    DefaultAITemperature = 0.4
)
```

删除 `.env.example`、本地/生产 Compose 和两个生产脚本中的全部 `WEAVEPRESS_AI_*`；外部密钥安装脚本不再询问或写入智谱 Key，README 改为指导管理员登录“AI 设置”页面录入。

- [ ] **Step 4: 运行直接相关的最小验证**

Run: `cd server; go test ./internal/modules/aisettings ./internal/platform/netguard ./internal/platform/llm ./internal/modules/aiwriting ./internal/transport/weaveapi ./internal/config -count=1`

Run: `cd admin; pnpm vitest run apps/web-ele/src/views/ai/settings.test.ts --dom`

Run: `cd admin; pnpm -F @nova/admin run typecheck`

Run: `cd admin; pnpm -F @nova/admin run build`

Run: `python deploy/tests/production-compose-contract.py deploy/production/compose.yaml`

Expected: 全部 PASS；下列运行配置扫描无结果：

Run: `rg -n "WEAVEPRESS_AI_" server/.env.example server/compose.yaml server/internal/config/config.go server/internal/config/loader.go server/internal/config/validate.go deploy/production README.md`

- [ ] **Step 5: 提交收尾改动**

```bash
git add server/internal/config server/.env.example server/compose.yaml deploy README.md
git commit -m "refactor: 移除 AI 环境变量配置"
```
