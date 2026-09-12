# 微信排版编辑器 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在现有稿件详情页中交付可保存、可预览、可恢复并可继续发布到微信公众号草稿箱的三栏 TipTap 排版工作台。

**Architecture:** 结构化 `editorDocument` 是编辑源，服务端内置主题注册表是样式权威，保存时由服务端校验文档和稿件素材归属并确定性生成 `contentHtml`。来源图片和上传图片统一登记为 `draft_assets`，旧 HTML 只在读取时转换、首次保存时落库，微信公众号发布适配器只消费最近保存版本的服务端 HTML 和稿件素材。

**Tech Stack:** Go 1.25、Gin、MySQL 8.4、Goose、七牛/本地对象存储、Vue 3、TypeScript、TipTap 3、Element Plus、Vitest、pnpm 11.21.0

**Spec:** `docs/superpowers/specs/2026-09-12-wechat-layout-editor-design.md`

## Global Constraints

- 只改现有稿件详情页，不增加孤立的排版页面。
- 编辑器只允许 `paragraph`、二/三级 `heading`、`blockquote`、`bulletList`、`orderedList`、`listItem`、`horizontalRule`、`text`、`image`、`hardBreak`。
- 只允许 `bold`、`italic`、`underline`、`link` mark；链接协议只能是 HTTP/HTTPS。
- 图片节点属性只允许 `draftAssetId`、`width`、`align`、`alt`、`caption`；宽度只能是 50、75、100，首版对齐只允许 `center`。
- 内置主题固定为 `minimal-business`、`clear-blue`、`natural-green`、`warm-lifestyle`、`elegant-chinese`、`vibrant-brand`，首版版本均为 1。
- 最终发布 HTML 只能由服务端主题渲染器生成，客户端不得提交 CSS 或最终 `contentHtml`。
- 上传只接受 JPEG、PNG、GIF、WebP，单文件最大 10 MiB；超过 1 MiB 的素材不能插入正文。
- 每次手动保存生成新版本；不做自动保存、素材删除、自定义 CSS、图片裁剪、滤镜、拼图、水印、OCR 和自动群发。
- 桌面端显示三栏；窄屏把主题素材栏和手机预览折叠为抽屉。
- 沿用 `draft:view`、`draft:update`、`draft:review`、`draft:publish` 权限和现有审核、预检、发布状态机。
- 快速 MVP 优先，只运行排版增量直接相关的最小测试、管理端类型检查和一次生产构建。

---

## 文件结构与职责

- `server/database/migrations/20260912000100_add_wechat_layout_editor.sql`：增加结构化文档、主题版本和稿件素材表，并把历史封面引用迁移到稿件素材。
- `server/internal/modules/editorial/document.go`：TipTap/ProseMirror 受限 JSON 类型、严格校验、素材引用提取和错误定位。
- `server/internal/modules/editorial/document_test.go`：文档节点、mark、链接、深度、数量和素材引用的最小单测。
- `server/internal/modules/editorial/themes.go`：六套版本化主题注册表和返回前端的受控令牌。
- `server/internal/modules/editorial/theme_render.go`：可信主题到微信公众号内联样式 HTML 的确定性渲染。
- `server/internal/modules/editorial/theme_render_test.go`：六主题渲染、转义、危险链接和确定性测试。
- `server/internal/modules/editorial/legacy.go`：旧 `content_html` 白名单清洗后转为结构化文档。
- `server/internal/modules/editorial/legacy_test.go`：旧稿保真、丢弃警告和转换失败测试。
- `server/internal/modules/editorial/assets.go`：图片真实格式、尺寸、哈希、对象键和上传约束。
- `server/internal/modules/editorial/model.go`：扩展稿件、版本、稿件素材、主题和错误模型。
- `server/internal/modules/editorial/store.go`：在持久化实现任务中同步扩展稿件保存和素材端口。
- `server/internal/modules/editorial/service.go`：串联旧稿转换、素材归属、保存渲染、版本恢复、预检和发布。
- `server/internal/modules/editorial/preflight.go`：按稿件素材检查封面和正文图片限制。
- `server/internal/modules/editorial/render.go`：保留采集文章初始 HTML，统一支持新旧图片占位预览。
- `server/internal/modules/workspace/mysqlstore/editorial.go`：结构化稿件、版本和 `draft_assets` 的 MySQL 事务实现。
- `server/internal/modules/workspace/mysqlstore/aiwriting.go`：AI 生成稿件时同步登记来源素材并把封面映射为稿件素材 ID。
- `server/internal/platform/wechat/client.go`：从 `draft_assets` 上传正文图片与封面并替换新占位。
- `server/internal/platform/wechat/client_test.go`：验证上传和占位替换使用稿件素材 ID。
- `server/internal/transport/weaveapi/api.go`：主题、素材列表、上传、媒体访问和新版保存请求接口。
- `server/internal/transport/weaveapi/wechat_layout_test.go`：主题、上传边界和新版保存的 HTTP 最小测试。
- `server/internal/app/api.go`、`server/internal/app/worker.go`：把对象存储注入排版服务。
- `server/api/openapi.yaml`：同步主题、素材和结构化稿件契约。
- `server/configs/config.example.yaml`、`deploy/production/config.yaml`：把 HTTP 请求体上限设为 11 MiB，容纳 10 MiB 文件和 multipart 元数据。
- `deploy/nginx.conf`、`deploy/production/nginx/new-server/wp.pdurl.cn.conf`、`deploy/production/nginx/old-server/wp.pdurl.cn.conf`：三层代理统一允许 11 MiB 请求体。
- `admin/apps/web-ele/package.json`、`admin/pnpm-lock.yaml`：加入 TipTap 运行依赖。
- `admin/apps/web-ele/src/api/content.ts`：新增编辑器文档、主题、稿件素材类型和 API 方法。
- `admin/apps/web-ele/src/components/wechat-layout/document.ts`：前端结构化文档归一化、变更指纹和安全即时预览。
- `admin/apps/web-ele/src/components/wechat-layout/document.test.ts`：前端变更判断、转义和图片属性测试。
- `admin/apps/web-ele/src/components/wechat-layout/wechat-image.ts`：受限图片块 TipTap Node。
- `admin/apps/web-ele/src/components/wechat-layout/editor-toolbar.vue`：首版允许的格式工具栏。
- `admin/apps/web-ele/src/components/wechat-layout/theme-panel.vue`：六主题选择。
- `admin/apps/web-ele/src/components/wechat-layout/asset-panel.vue`：来源/上传素材、上传、插入和封面操作。
- `admin/apps/web-ele/src/components/wechat-layout/phone-preview.vue`：沙箱 iframe 微信手机预览。
- `admin/apps/web-ele/src/components/wechat-layout/layout-editor.vue`：TipTap 编辑器、图片属性和三栏工作台编排。
- `admin/apps/web-ele/src/views/drafts/detail.vue`：接入工作台、手动保存、未保存拦截、版本和流程操作。

---

### Task 1: 增加排版数据模型和无损迁移

**Files:**
- Create: `server/database/migrations/20260912000100_add_wechat_layout_editor.sql`
- Modify: `server/internal/modules/editorial/model.go`

**Interfaces:**
- Consumes: 现有 `drafts`、`draft_versions`、`article_assets` 和用户表。
- Produces: 可被后续文档、主题和持久化任务复用的 `DraftAsset`、`NewDraftAsset` 与稳定错误。

- [ ] **Step 1: 写入可回滚的数据迁移**

迁移的 `Up` 按以下顺序执行，确保历史封面不会短暂失去引用：

```sql
-- +goose Up
ALTER TABLE drafts
    ADD COLUMN editor_document JSON NULL AFTER content_html,
    ADD COLUMN theme_id VARCHAR(64) NOT NULL DEFAULT 'minimal-business' AFTER editor_document,
    ADD COLUMN theme_version INT UNSIGNED NOT NULL DEFAULT 1 AFTER theme_id;

ALTER TABLE draft_versions
    ADD COLUMN editor_document JSON NULL AFTER content_html,
    ADD COLUMN theme_id VARCHAR(64) NOT NULL DEFAULT 'minimal-business' AFTER editor_document,
    ADD COLUMN theme_version INT UNSIGNED NOT NULL DEFAULT 1 AFTER theme_id;

CREATE TABLE draft_assets (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    draft_id BIGINT UNSIGNED NOT NULL,
    origin VARCHAR(16) NOT NULL,
    article_asset_id BIGINT UNSIGNED NULL,
    object_key VARCHAR(1024) NOT NULL,
    media_type VARCHAR(128) NOT NULL,
    byte_size BIGINT UNSIGNED NOT NULL,
    width INT UNSIGNED NOT NULL DEFAULT 0,
    height INT UNSIGNED NOT NULL DEFAULT 0,
    sha256 BINARY(32) NOT NULL,
    uploaded_by BIGINT UNSIGNED NOT NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_draft_assets_article (draft_id, article_asset_id),
    KEY idx_draft_assets_draft_created (draft_id, created_at),
    KEY idx_draft_assets_sha256 (sha256),
    CONSTRAINT fk_draft_assets_draft FOREIGN KEY (draft_id) REFERENCES drafts (id) ON DELETE CASCADE,
    CONSTRAINT fk_draft_assets_article FOREIGN KEY (article_asset_id) REFERENCES article_assets (id),
    CONSTRAINT fk_draft_assets_user FOREIGN KEY (uploaded_by) REFERENCES users (id),
    CONSTRAINT chk_draft_assets_origin CHECK (
        (origin = 'article' AND article_asset_id IS NOT NULL) OR
        (origin = 'upload' AND article_asset_id IS NULL)
    )
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

INSERT INTO draft_assets
    (draft_id, origin, article_asset_id, object_key, media_type, byte_size, width, height, sha256, uploaded_by)
SELECT d.id, 'article', aa.id, aa.object_key, aa.media_type, aa.byte_size,
       aa.width, aa.height, aa.sha256, d.created_by
FROM drafts d
JOIN article_assets aa ON aa.article_id = d.source_article_id
WHERE aa.download_status = 'completed' AND aa.object_key <> '';

ALTER TABLE draft_versions DROP FOREIGN KEY fk_draft_versions_cover;
ALTER TABLE drafts DROP FOREIGN KEY fk_drafts_cover;

UPDATE drafts d
LEFT JOIN draft_assets da
  ON da.draft_id = d.id AND da.article_asset_id = d.cover_asset_id
SET d.cover_asset_id = da.id;

UPDATE draft_versions dv
LEFT JOIN draft_assets da
  ON da.draft_id = dv.draft_id AND da.article_asset_id = dv.cover_asset_id
SET dv.cover_asset_id = da.id;

ALTER TABLE drafts
    ADD CONSTRAINT fk_drafts_cover_draft_asset
    FOREIGN KEY (cover_asset_id) REFERENCES draft_assets (id) ON DELETE SET NULL;
ALTER TABLE draft_versions
    ADD CONSTRAINT fk_draft_versions_cover_draft_asset
    FOREIGN KEY (cover_asset_id) REFERENCES draft_assets (id) ON DELETE SET NULL;
```

`Down` 先移除新封面外键，把封面映射回 `article_asset_id`（上传素材映射为 `NULL`），恢复原外键，再删除 `draft_assets` 和六个新增列：

```sql
-- +goose Down
ALTER TABLE draft_versions DROP FOREIGN KEY fk_draft_versions_cover_draft_asset;
ALTER TABLE drafts DROP FOREIGN KEY fk_drafts_cover_draft_asset;
UPDATE draft_versions dv LEFT JOIN draft_assets da ON da.id = dv.cover_asset_id
SET dv.cover_asset_id = da.article_asset_id;
UPDATE drafts d LEFT JOIN draft_assets da ON da.id = d.cover_asset_id
SET d.cover_asset_id = da.article_asset_id;
ALTER TABLE drafts ADD CONSTRAINT fk_drafts_cover FOREIGN KEY (cover_asset_id) REFERENCES article_assets (id);
ALTER TABLE draft_versions ADD CONSTRAINT fk_draft_versions_cover FOREIGN KEY (cover_asset_id) REFERENCES article_assets (id);
DROP TABLE draft_assets;
ALTER TABLE draft_versions DROP COLUMN theme_version, DROP COLUMN theme_id, DROP COLUMN editor_document;
ALTER TABLE drafts DROP COLUMN theme_version, DROP COLUMN theme_id, DROP COLUMN editor_document;
```

- [ ] **Step 2: 扩展领域模型和稳定错误**

在 `model.go` 增加并在后续任务保持完全相同的名称：

```go
const DefaultThemeID = "minimal-business"
const DefaultThemeVersion uint = 1

var (
    ErrDocumentInvalid      = errors.New("editor document is invalid")
    ErrThemeNotFound        = errors.New("wechat layout theme not found")
    ErrDraftAssetInvalid    = errors.New("draft asset is invalid")
    ErrDraftAssetTooLarge   = errors.New("draft asset is too large")
    ErrDraftAssetType       = errors.New("draft asset type is unsupported")
    ErrLegacyConvertFailed  = errors.New("legacy draft conversion failed")
)

type DraftAsset struct {
    ID             uint64  `json:"id"`
    DraftID        uint64  `json:"draftId"`
    Origin         string  `json:"origin"`
    ArticleAssetID *uint64 `json:"articleAssetId,omitempty"`
    ObjectKey      string  `json:"-"`
    MediaType      string  `json:"mediaType"`
    ByteSize       uint64  `json:"byteSize"`
    Width          uint    `json:"width"`
    Height         uint    `json:"height"`
    SHA256         [32]byte `json:"-"`
    UploadedBy     uint64  `json:"uploadedBy"`
    CreatedAt      time.Time `json:"createdAt"`
    MediaURL       string  `json:"mediaUrl"`
    BodyEligible   bool    `json:"bodyEligible"`
    CoverEligible  bool    `json:"coverEligible"`
}

type NewDraftAsset struct {
    ObjectKey string
    MediaType string
    ByteSize  uint64
    Width     uint
    Height    uint
    SHA256    [32]byte
}
```

本任务不提前修改 `Store` 接口、`Draft` 或 `UpdateInput`，避免接口实现尚未落地时中间提交无法编译；这些类型在 Tasks 2 和 4 与实现一并切换。

- [ ] **Step 3: 格式化并提交模型与迁移**

Run: `cd server; gofmt -w internal/modules/editorial/model.go`

Expected: `gofmt` 无错误，迁移中不存在对尚未创建对象的引用。

```bash
git add server/database/migrations/20260912000100_add_wechat_layout_editor.sql server/internal/modules/editorial/model.go
git commit -m "feat: 增加微信排版数据模型"
```

### Task 2: 实现结构化文档严格校验和旧稿转换

**Files:**
- Create: `server/internal/modules/editorial/document.go`
- Create: `server/internal/modules/editorial/document_test.go`
- Create: `server/internal/modules/editorial/legacy.go`
- Create: `server/internal/modules/editorial/legacy_test.go`
- Modify: `server/internal/modules/editorial/render.go`

**Interfaces:**
- Consumes: Task 1 的 `DraftAsset` 类型和现有旧 `content_html`。
- Produces: `Document`、扩展后的 `Draft`/`DraftVersion`、`ParseDocument`、`ValidateDocument`、`ReferencedDraftAssetIDs`、`ConvertLegacyHTML`。

- [ ] **Step 1: 写结构化文档失败用例**

测试至少包含以下表格用例：

```go
func TestValidateDocument(t *testing.T) {
    tests := []struct {
        name string
        raw  string
        ok   bool
    }{
        {"paragraph", `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"正文","marks":[{"type":"bold"}]}]}]}`, true},
        {"image", `{"type":"doc","content":[{"type":"image","attrs":{"draftAssetId":18,"width":75,"align":"center","alt":"产品","caption":"说明"}}]}`, true},
        {"h1 rejected", `{"type":"doc","content":[{"type":"heading","attrs":{"level":1}}]}`, false},
        {"script node rejected", `{"type":"doc","content":[{"type":"script"}]}`, false},
        {"javascript link rejected", `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"链接","marks":[{"type":"link","attrs":{"href":"javascript:alert(1)"}}]}]}]}`, false},
        {"unknown image attr rejected", `{"type":"doc","content":[{"type":"image","attrs":{"draftAssetId":18,"width":100,"align":"center","alt":"","caption":"","style":"display:none"}}]}`, false},
    }
    for _, test := range tests {
        t.Run(test.name, func(t *testing.T) {
            _, err := ParseDocument(json.RawMessage(test.raw))
            if (err == nil) != test.ok { t.Fatalf("error = %v, ok = %v", err, test.ok) }
        })
    }
}
```

另加节点数 5001、嵌套深度 17、文本超过 200000 rune、图片 101 张和重复素材 ID 的用例；重复引用允许且 `ReferencedDraftAssetIDs` 必须去重并保持首次出现顺序。

- [ ] **Step 2: 运行测试确认失败**

Run: `cd server; go test ./internal/modules/editorial -run 'TestValidateDocument|TestReferencedDraftAssetIDs'`

Expected: FAIL，提示 `ParseDocument` 或 `ReferencedDraftAssetIDs` 未定义。

- [ ] **Step 3: 实现 JSON 类型和递归校验器**

`document.go` 使用以下协议类型：

```go
type Document struct {
    Type    string `json:"type"`
    Content []Node `json:"content,omitempty"`
}

type Node struct {
    Type    string                     `json:"type"`
    Attrs   map[string]json.RawMessage `json:"attrs,omitempty"`
    Content []Node                     `json:"content,omitempty"`
    Text    string                     `json:"text,omitempty"`
    Marks   []Mark                     `json:"marks,omitempty"`
}

type Mark struct {
    Type  string                     `json:"type"`
    Attrs map[string]json.RawMessage `json:"attrs,omitempty"`
}

const (
    maxDocumentNodes  = 5000
    maxDocumentDepth  = 16
    maxDocumentRunes  = 200000
    maxDocumentImages = 100
)
```

同一步给 `Draft` 和 `DraftVersion` 增加 `EditorDocument *Document`、`ThemeID string`、`ThemeVersion uint`；只给 `Draft` 增加 `MigrationNeeded bool`、`MigrationWarnings []string` 和内部使用且标记 `json:"-"` 的 `Assets []DraftAsset`。此时仍不切换保存接口。

`ParseDocument(raw json.RawMessage) (Document, error)` 先用 `json.Decoder.DisallowUnknownFields()` 解码根/节点/mark 固定字段，再调用 `ValidateDocument(Document) error`。递归校验节点允许的父子关系，禁止未知 attrs；`heading.level` 只能 2/3，`image` 五个属性必须类型正确，`width` 只能 50/75/100，`align` 必须 `center`，`draftAssetId > 0`。`link` 只允许 `href`、`title`，用 `url.ParseRequestURI` 并限定 scheme。

- [ ] **Step 4: 写旧稿转换失败用例**

```go
func TestConvertLegacyHTML(t *testing.T) {
    mapping := map[uint64]uint64{11: 101}
    result, err := ConvertLegacyHTML(`<h2>标题</h2><p>正文<strong>加粗</strong></p><figure><img data-weavepress-asset-id="11" alt="图"><figcaption>说明</figcaption></figure><script>alert(1)</script>`, mapping)
    if err != nil { t.Fatal(err) }
    if result.Document.Content[2].Type != "image" { t.Fatalf("node = %#v", result.Document.Content[2]) }
    if len(result.Warnings) == 0 { t.Fatal("expected dropped-node warning") }
    ids := ReferencedDraftAssetIDs(result.Document)
    if !reflect.DeepEqual(ids, []uint64{101}) { t.Fatalf("ids = %v", ids) }
}
```

再覆盖列表、引用、链接、`br`、未知结构降级为普通段落、缺失图片映射时丢弃并警告、完全无正文时返回 `ErrLegacyConvertFailed`。

- [ ] **Step 5: 实现只读旧稿转换**

定义：

```go
type LegacyConversion struct {
    Document Document
    Warnings []string
}

func ConvertLegacyHTML(content string, articleToDraft map[uint64]uint64) (LegacyConversion, error)
```

先调用现有 `SanitizeHTML`，再用 `golang.org/x/net/html` 遍历；`h2/h3/h4` 映射到 2/3 级 heading，`strong/b`、`em/i`、`u`、`a` 映射 mark，`figure/img/figcaption` 合并成图片节点，旧 `data-weavepress-asset-id` 必须经过 `articleToDraft` 映射。无法精确保留的安全文本落为 paragraph；脚本、样式和无映射图片只产生中文警告，不写数据库。

把 `PreviewHTML` 同时识别 `data-weavepress-draft-asset-id` 和旧占位，但新渲染只输出前者。

- [ ] **Step 6: 运行最小测试并提交**

Run: `cd server; go test ./internal/modules/editorial -run 'TestValidateDocument|TestReferencedDraftAssetIDs|TestConvertLegacyHTML'`

Expected: PASS。

```bash
git add server/internal/modules/editorial/document.go server/internal/modules/editorial/document_test.go server/internal/modules/editorial/legacy.go server/internal/modules/editorial/legacy_test.go server/internal/modules/editorial/render.go
git commit -m "feat: 校验并转换微信结构化稿件"
```

### Task 3: 实现六套可信主题和确定性渲染

**Files:**
- Create: `server/internal/modules/editorial/themes.go`
- Create: `server/internal/modules/editorial/theme_render.go`
- Create: `server/internal/modules/editorial/theme_render_test.go`

**Interfaces:**
- Consumes: Task 2 的 `Document`。
- Produces: `ListThemes() []ThemeSummary`、`ResolveTheme(string, uint) (Theme, error)`、`RenderDocument(Document, Theme) (string, error)`。

- [ ] **Step 1: 写六主题和安全渲染测试**

```go
func TestThemeRegistryAndRender(t *testing.T) {
    ids := []string{"minimal-business", "clear-blue", "natural-green", "warm-lifestyle", "elegant-chinese", "vibrant-brand"}
    if themes := ListThemes(); len(themes) != len(ids) { t.Fatalf("themes = %d", len(themes)) }
    doc, err := ParseDocument(json.RawMessage(`{"type":"doc","content":[{"type":"heading","attrs":{"level":2},"content":[{"type":"text","text":"<标题>"}]},{"type":"image","attrs":{"draftAssetId":18,"width":75,"align":"center","alt":"\"图\"","caption":"说明"}}]}`))
    if err != nil { t.Fatal(err) }
    for _, id := range ids {
        theme, err := ResolveTheme(id, 1)
        if err != nil { t.Fatal(err) }
        first, err := RenderDocument(doc, theme)
        if err != nil { t.Fatal(err) }
        second, _ := RenderDocument(doc, theme)
        if first != second { t.Fatalf("theme %s is not deterministic", id) }
        if strings.Contains(first, "<标题>") || !strings.Contains(first, `data-weavepress-draft-asset-id="18"`) { t.Fatalf("unsafe output: %s", first) }
    }
}
```

另测不存在的主题/版本返回 `ErrThemeNotFound`，链接输出 `target="_blank" rel="nofollow noopener noreferrer"`，所有用户文本与属性均转义。

- [ ] **Step 2: 运行测试确认失败**

Run: `cd server; go test ./internal/modules/editorial -run TestThemeRegistryAndRender`

Expected: FAIL，提示主题函数未定义。

- [ ] **Step 3: 实现版本化主题注册表**

主题类型固定为：

```go
type ThemeTokens struct {
    Accent     string `json:"accent"`
    Text       string `json:"text"`
    Muted      string `json:"muted"`
    Surface    string `json:"surface"`
    Border     string `json:"border"`
    Heading    string `json:"heading"`
    FontFamily string `json:"fontFamily"`
}

type Theme struct {
    ThemeSummary
    BodyStyle       string
    ParagraphStyle  string
    Heading2Style   string
    Heading3Style   string
    QuoteStyle      string
    ListStyle       string
    DividerStyle    string
    LinkStyle       string
    CaptionStyle    string
}

type ThemeSummary struct {
    ID      string      `json:"id"`
    Version uint        `json:"version"`
    Name    string      `json:"name"`
    Preview string      `json:"preview"`
    Tokens  ThemeTokens `json:"tokens"`
}
```

六套 v1 主题使用以下受控色板；所有 CSS 声明直接写在 Go 常量中，不拼接用户值：

| ID | 名称 | Accent | Text | Surface | Border |
|---|---|---|---|---|---|
| `minimal-business` | 极简商务 | `#2f3542` | `#262626` | `#f7f8fa` | `#d9d9d9` |
| `clear-blue` | 清爽蓝 | `#1677ff` | `#1f2937` | `#f0f7ff` | `#91caff` |
| `natural-green` | 自然绿 | `#389e0d` | `#243126` | `#f3f8ee` | `#95de64` |
| `warm-lifestyle` | 暖生活 | `#d46b08` | `#3d2b1f` | `#fff7e6` | `#ffd591` |
| `elegant-chinese` | 雅致中国风 | `#8c2f39` | `#2f2725` | `#faf5ef` | `#d6bfa8` |
| `vibrant-brand` | 活力品牌 | `#722ed1` | `#24212b` | `#f9f0ff` | `#d3adf7` |

`ListThemes` 返回按上表固定顺序复制出的摘要；`ResolveTheme` 用 `(id, version)` 精确匹配，不自动回退。

- [ ] **Step 4: 实现确定性 HTML 渲染器**

`RenderDocument` 先调用 `ValidateDocument`，再用 `strings.Builder` 依次渲染。顶层按主题输出受控 style，例如极简商务为 `<section style="color:#262626;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;font-size:16px;line-height:1.85">`；段落、标题、引用、列表、分割线和图片只使用主题定义中的内联样式。图片格式固定为：

```html
<figure style="margin:24px 0;text-align:center"><img data-weavepress-draft-asset-id="18" alt="产品展示" style="display:block;width:75%;max-width:100%;height:auto;margin:0 auto"><figcaption style="margin-top:8px;color:#6b7280;font-size:13px;line-height:1.6;text-align:center">图片说明</figcaption></figure>
```

不输出空 `figcaption`，不调用允许任意 style 的通用清洗器；所有文本、alt、caption、href、title 通过 `html.EscapeString`。

- [ ] **Step 5: 运行测试并提交**

Run: `cd server; go test ./internal/modules/editorial -run 'TestTheme|TestRenderDocument'`

Expected: PASS。

```bash
git add server/internal/modules/editorial/themes.go server/internal/modules/editorial/theme_render.go server/internal/modules/editorial/theme_render_test.go
git commit -m "feat: 增加微信内置主题渲染器"
```

### Task 4: 持久化稿件文档、版本和素材映射

**Files:**
- Modify: `server/internal/modules/editorial/model.go`
- Modify: `server/internal/modules/editorial/store.go`
- Modify: `server/internal/modules/workspace/mysqlstore/editorial.go`
- Modify: `server/internal/modules/workspace/mysqlstore/aiwriting.go`
- Modify: `server/internal/modules/workspace/mysqlstore/store_integration_test.go`

**Interfaces:**
- Consumes: Task 1 的扩展 `Store` 和数据表。
- Produces: 扩展后的 `Store`、`UpdateInput`、`RestoreInput`，以及原子保存/恢复、来源素材懒登记、上传素材写入和 AI 稿件素材映射。

- [ ] **Step 1: 增加一个迁移后持久化集成用例**

在现有测试数据库夹具中创建文章和两张已完成 `article_assets`，创建稿件后断言：

```go
assets, err := store.ListDraftAssets(ctx, draft.ID)
if err != nil { t.Fatal(err) }
if len(assets) != 2 || assets[0].Origin != "article" { t.Fatalf("assets = %#v", assets) }

doc, err := editorial.ParseDocument(json.RawMessage(fmt.Sprintf(`{"type":"doc","content":[{"type":"image","attrs":{"draftAssetId":%d,"width":100,"align":"center","alt":"","caption":""}}]}`, assets[0].ID)))
if err != nil { t.Fatal(err) }
updated, err := store.UpdateDraft(ctx, draft.ID, user.ID, editorial.UpdateInput{
    Title: "排版稿", EditorDocument: doc, ThemeID: "clear-blue", ThemeVersion: 1,
    RenderedHTML: "<section>saved</section>", ExpectedVersion: draft.CurrentVersion, ChangeNote: "排版保存",
})
if err != nil { t.Fatal(err) }
if updated.CurrentVersion != 2 || updated.EditorDocument == nil || updated.ThemeID != "clear-blue" { t.Fatalf("updated = %#v", updated) }
```

同时验证同一个来源素材重复执行 `EnsureArticleAssets` 不重复、其他稿件不能通过 `GetDraftAsset` 读取、旧 expectedVersion 保存冲突、恢复版本生成新版本而不改历史行。

- [ ] **Step 2: 运行单个集成用例确认失败**

Run: `cd server; go test ./internal/modules/workspace/mysqlstore -run TestDraftLayoutPersistence -count=1`

Expected: FAIL；若未设置 `WEAVEPRESS_TEST_MYSQL_DSN`，测试按现有夹具规则 SKIP，继续完成实现并在 Task 10 启动本地 MySQL 后重跑。

- [ ] **Step 3: 扩展查询列和扫描函数**

`draftColumns` 固定追加 `editor_document, theme_id, theme_version`。扫描 JSON 时使用 `sql.RawBytes`/`[]byte`，空值设为 `nil`，非空调用 `json.Unmarshal` 后再 `ValidateDocument`；损坏数据直接返回错误，禁止静默丢弃。`ListDraftVersions` 和 `GetDraftVersion` 使用同样顺序。

同一步在 `model.go` 固定保存输入：

```go
type UpdateInput struct {
    Title           string
    Author          string
    Digest          string
    EditorDocument Document
    ThemeID         string
    ThemeVersion    uint
    RenderedHTML    string
    CoverAssetID    *uint64
    ExpectedVersion uint
    ChangeNote      string
}

type RestoreInput struct {
    TargetVersion   uint
    ExpectedVersion uint
    Title           string
    Author          string
    Digest          string
    EditorDocument Document
    ThemeID         string
    ThemeVersion    uint
    RenderedHTML    string
    CoverAssetID    *uint64
}
```

并在 `store.go` 一次性增加完整端口，确保同一提交内所有实现就绪：

```go
EnsureArticleAssets(context.Context, uint64, uint64, uint64) error
ListDraftAssets(context.Context, uint64) ([]DraftAsset, error)
GetDraftAsset(context.Context, uint64, uint64) (DraftAsset, error)
GetDraftAssetByID(context.Context, uint64) (DraftAsset, error)
CreateUploadedDraftAsset(context.Context, uint64, uint64, NewDraftAsset) (DraftAsset, error)
RestoreDraftVersion(context.Context, uint64, uint64, RestoreInput) (Draft, error)
```

- [ ] **Step 4: 实现稿件素材 SQL**

`EnsureArticleAssets` 使用：

```sql
INSERT IGNORE INTO draft_assets
    (draft_id, origin, article_asset_id, object_key, media_type, byte_size, width, height, sha256, uploaded_by)
SELECT ?, 'article', id, object_key, media_type, byte_size, width, height, sha256, ?
FROM article_assets
WHERE article_id = ? AND download_status = 'completed' AND object_key <> ''
```

`GetDraftAsset` 必须同时匹配 `id = ? AND draft_id = ?`；`CreateUploadedDraftAsset` 在事务中 `SELECT status FROM drafts WHERE id=? FOR UPDATE`，只允许 `editing`，然后插入 `origin='upload'`。读取时使用现有 `WeChatMaxContentImageSize` 和 `WeChatMaxCoverImageSize` 计算 `BodyEligible`、`CoverEligible`，格式限制在 service 再校验，避免数据库层重复魔法数字。

- [ ] **Step 5: 修改创建、保存和恢复事务**

`CreateDraft` 和 `AIStore.CompleteGeneration` 都先以 `cover_asset_id = NULL` 创建稿件，再在同一事务登记来源素材，按 `(draft_id, article_asset_id)` 查到新的稿件素材 ID，最后更新当前稿和 v1 封面。初始 `content_html` 保留旧占位、`editor_document` 为 NULL，因此首次读取走 Task 2 的无写入转换。`GeneratedDraftInput.CoverAssetID` 继续表示来源文章素材 ID，只在事务内部映射，避免改动 AI 生成 service 的外部语义。

`UpdateDraft` 在同一事务中更新当前稿的 `editor_document/theme_id/theme_version/content_html/cover_asset_id` 并插入完全相同快照的 `draft_versions`。`RestoreDraftVersion` 接收 service 已准备好的 `RestoreInput`，锁定当前版本、检查 expectedVersion 后写入一个新版本，不修改目标历史记录。

- [ ] **Step 6: 运行最小持久化测试并提交**

Run: `cd server; go test ./internal/modules/workspace/mysqlstore -run TestDraftLayoutPersistence -count=1`

Expected: PASS 或因测试 DSN 未配置而明确 SKIP，不允许编译失败。

```bash
git add server/internal/modules/editorial/model.go server/internal/modules/editorial/store.go server/internal/modules/workspace/mysqlstore/editorial.go server/internal/modules/workspace/mysqlstore/aiwriting.go server/internal/modules/workspace/mysqlstore/store_integration_test.go
git commit -m "feat: 持久化微信排版版本与素材"
```

### Task 5: 实现稿件素材上传与媒体访问

**Files:**
- Create: `server/internal/modules/editorial/assets.go`
- Create: `server/internal/modules/editorial/assets_test.go`
- Modify: `server/internal/modules/editorial/store.go`
- Modify: `server/internal/modules/editorial/service.go`
- Modify: `server/internal/modules/editorial/service_test.go`
- Modify: `server/internal/transport/weaveapi/api.go`
- Create: `server/internal/transport/weaveapi/wechat_layout_test.go`
- Modify: `server/internal/app/api.go`
- Modify: `server/internal/app/worker.go`
- Modify: `server/configs/config.example.yaml`
- Modify: `deploy/production/config.yaml`
- Modify: `deploy/nginx.conf`
- Modify: `deploy/production/nginx/new-server/wp.pdurl.cn.conf`
- Modify: `deploy/production/nginx/old-server/wp.pdurl.cn.conf`

**Interfaces:**
- Consumes: Task 4 的 `CreateUploadedDraftAsset`、对象存储 `Put` 和现有媒体签名器。
- Produces: `GET /api/drafts/{id}/assets`、`POST /api/drafts/{id}/assets`、`GET /media/draft-assets/{id}`。

- [ ] **Step 1: 写图片校验单测**

```go
func TestInspectDraftImage(t *testing.T) {
    tests := []struct { name, mediaType string; body []byte; ok bool }{
        {"png", "image/png", validPNG(t, 20, 10), true},
        {"jpeg", "image/jpeg", validJPEG(t, 20, 10), true},
        {"gif", "image/gif", validGIF(t, 20, 10), true},
        {"webp", "image/webp", validWebP(20, 10), true},
        {"spoofed", "image/png", []byte("not an image"), false},
        {"svg", "image/svg+xml", []byte(`<svg/>`), false},
    }
    for _, test := range tests {
        _, err := InspectDraftImage(test.body, test.mediaType)
        if (err == nil) != test.ok { t.Fatalf("%s error = %v", test.name, err) }
    }
}
```

另测 `10 MiB + 1` 返回 `ErrDraftAssetTooLarge`，Content-Type 与真实文件头不一致返回 `ErrDraftAssetType`，0 宽高返回 `ErrDraftAssetInvalid`。

- [ ] **Step 2: 运行图片测试确认失败**

Run: `cd server; go test ./internal/modules/editorial -run TestInspectDraftImage`

Expected: FAIL，提示 `InspectDraftImage` 未定义。

- [ ] **Step 3: 实现上传图片检查和对象键**

定义：

```go
type InspectedImage struct {
    MediaType string
    Extension string
    ByteSize  uint64
    Width     uint
    Height    uint
    SHA256    [32]byte
}

func InspectDraftImage(body []byte, declared string) (InspectedImage, error)
```

使用 `http.DetectContentType`、JPEG/PNG/GIF `image.DecodeConfig` 和现有 WebP RIFF 尺寸解析；实际 MIME 必须是四种白名单之一且与声明的基础 MIME 一致。对象键固定为 `drafts/{yyyy}/{mm}/{sha256}.{ext}`，相同内容复用对象键。

- [ ] **Step 4: 给 Editorial Service 注入对象存储并实现素材方法**

在 `store.go` 增加窄接口，避免 service 依赖具体七牛实现：

```go
type AssetObjects interface {
    Put(context.Context, string, []byte, string) error
}
```

把构造器改为：

```go
func New(store Store, articles ArticleStore, queueClient *queue.Client, publisher Publisher, objects AssetObjects, enabled bool) *Service
```

新增：

```go
func (s *Service) Themes() []ThemeSummary
func (s *Service) Assets(ctx context.Context, draftID uint64) ([]DraftAsset, error)
func (s *Service) UploadAsset(ctx context.Context, draftID, userID uint64, filename, declared string, body []byte) (DraftAsset, error)
func (s *Service) AssetObject(ctx context.Context, id uint64) (string, string, error)
```

上传先检查稿件存在且为 `editing`，再校验图片、写对象存储，最后创建数据库记录；存储或数据库失败都不改变稿件文档和版本。`app/api.go` 注入真实 objects，Worker 仅按新构造器参数传 objects，不增加队列。

- [ ] **Step 5: 写 HTTP 上传边界测试**

测试路由必须覆盖：未登录 401、非 editing 409、SVG 400、超过 10 MiB 413、成功 201；成功响应包含 `origin=upload`、`mediaUrl`、`bodyEligible`、`coverEligible`。使用内存 fake object store，不访问外网。

- [ ] **Step 6: 增加主题、素材和媒体路由**

注册：

```go
protected.GET("/wechat-layout/themes", a.requireCode("draft:view"), a.listWeChatLayoutThemes)
protected.GET("/drafts/:id/assets", a.requireCode("draft:view"), a.listDraftAssets)
protected.POST("/drafts/:id/assets", a.requireCode("draft:update"), a.uploadDraftAsset)
router.GET("/media/draft-assets/:id", a.media("draft-assets"))
```

在 Task 6 切换新版保存时，为现有 draft 路由补齐相同权限门禁：读取/版本/预检用 `draft:view`，创建用 `draft:create`，保存用 `draft:update`，提交审核用 `draft:submit-review`，审核和发布继续只允许管理员并分别要求 `draft:review`、`draft:publish`。

上传 handler 用 `http.MaxBytesReader(c.Writer, c.Request.Body, 10<<20+1<<20)`，只读取字段名 `file` 的一个文件，拒绝额外文件。`decorateDraftAsset` 使用 `a.content.SignMedia("draft-assets", asset.ID)`；`media` 在 kind 为 `draft-assets` 时调用 `a.editorial.AssetObject`，其余继续调用 `a.content.MediaObject`。

`writeError` 映射：格式/内容不合法 400，超过 10 MiB 413，稿件不可编辑 409；消息分别为“图片格式不支持”“图片内容无效”“图片超过 10 MiB 限制”。

全局 BodyLimit 和三层 Nginx 的默认 1/10 MiB 会早于 handler 拒绝合法文件，因此把 `server/configs/config.example.yaml` 与 `deploy/production/config.yaml` 的 `http.max_body_bytes` 设为 `11534336`，并在 Gateway、腾讯云 origin、旧服务器 HTTPS bridge 三个 server block 中加入 `client_max_body_size 11m;`。业务层仍以实际文件内容 10 MiB 为严格上限，额外 1 MiB 只供 multipart 边界和字段使用。

- [ ] **Step 7: 运行最小服务端测试并提交**

Run: `cd server; go test ./internal/modules/editorial ./internal/transport/weaveapi -run 'TestInspectDraftImage|TestWeChatLayout|TestDraftAsset'`

Expected: PASS。

```bash
git add server/internal/modules/editorial/assets.go server/internal/modules/editorial/assets_test.go server/internal/modules/editorial/store.go server/internal/modules/editorial/service.go server/internal/modules/editorial/service_test.go server/internal/transport/weaveapi/api.go server/internal/transport/weaveapi/wechat_layout_test.go server/internal/app/api.go server/internal/app/worker.go server/configs/config.example.yaml deploy/production/config.yaml deploy/nginx.conf deploy/production/nginx/new-server/wp.pdurl.cn.conf deploy/production/nginx/old-server/wp.pdurl.cn.conf
git commit -m "feat: 增加微信稿件素材上传"
```

### Task 6: 串联保存、旧稿、预检、恢复和微信发布

**Files:**
- Modify: `server/internal/modules/editorial/service.go`
- Modify: `server/internal/modules/editorial/preflight.go`
- Modify: `server/internal/modules/editorial/preflight_test.go`
- Modify: `server/internal/modules/editorial/render.go`
- Modify: `server/internal/modules/editorial/service_test.go`
- Modify: `server/internal/platform/wechat/client.go`
- Modify: `server/internal/platform/wechat/client_test.go`
- Modify: `server/internal/transport/weaveapi/api.go`

**Interfaces:**
- Consumes: Tasks 2–5 的转换器、主题渲染器、稿件素材和持久化事务。
- Produces: 新版 `GET/PUT /api/drafts/{id}`、完整版本恢复和使用稿件素材的发布链路。

- [ ] **Step 1: 写 Service 保存与旧稿用例**

覆盖以下行为：

```go
func TestUpdateRendersTrustedDocument(t *testing.T) {
    store := newFakeEditorialStore()
    service := New(store, store, nil, nil, newFakeObjects(), false)
    doc := mustDocument(t, `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"正文"}]}]}`)
    saved, err := service.Update(context.Background(), 1, 7, UpdateInput{
        Title: "标题", EditorDocument: doc, ThemeID: "clear-blue",
        ExpectedVersion: 1, ChangeNote: "调整排版",
    })
    if err != nil { t.Fatal(err) }
    if !strings.Contains(store.updated.RenderedHTML, `style=`) || store.updated.ThemeVersion != 1 { t.Fatalf("update = %#v", store.updated) }
    if saved.ContentHTML != store.updated.RenderedHTML { t.Fatal("response must use server render") }
}
```

另测：Get 旧稿返回临时 document、`migrationNeeded=true` 且 store 未更新；旧稿无有效正文进入只读转换失败；外稿件素材 ID 保存失败；超过 1 MiB 素材用于正文失败但可作封面；expectedVersion 冲突不生成版本；历史旧 HTML 恢复时转换并创建新结构化版本。

- [ ] **Step 2: 运行 Service 测试确认失败**

Run: `cd server; go test ./internal/modules/editorial -run 'TestUpdateRendersTrustedDocument|TestGetLegacy|TestRestoreLegacy'`

Expected: FAIL，直到 Service 完成新版流程。

- [ ] **Step 3: 实现读取时 hydrate 和保存时渲染**

新增内部方法：

```go
func (s *Service) hydrateDraft(ctx context.Context, draft Draft) (Draft, error)
func (s *Service) prepareUpdate(ctx context.Context, draftID uint64, input UpdateInput) (UpdateInput, error)
func (s *Service) validateDraftAssets(ctx context.Context, draftID uint64, doc Document, coverID *uint64) error
```

`hydrateDraft` 调用 `EnsureArticleAssets` 和 `ListDraftAssets`。当 `EditorDocument == nil` 时，构造 `article_asset_id -> draft_assets.id` 映射并调用 `ConvertLegacyHTML`，只修改返回值，设置默认主题与 `MigrationNeeded=true`；转换失败保留原 `ContentHTML`，返回 `MigrationNeeded=true` 和中文警告，不写数据库。

`prepareUpdate` 固定 `ThemeVersion=1`，调用 `ResolveTheme`、`ValidateDocument`、素材归属/状态/正文 1 MiB 校验，再调用 `RenderDocument` 填充 `RenderedHTML`。API 的 PUT 请求不再接收 `contentHtml` 和 `themeVersion`。

- [ ] **Step 4: 修改预检和版本恢复**

`ValidateWeChatDraft` 只接受最近保存的非空 `EditorDocument` 和 `ContentHTML`；正文图片从 `ReferencedDraftAssetIDs(*draft.EditorDocument)` 获取，素材从 `draft.Assets` 匹配。封面必须属于当前稿件且 `CoverEligible=true`。增加稳定 issue：`EDITOR_DOCUMENT_REQUIRED`、`DRAFT_ASSET_MISSING`、`CONTENT_IMAGE_TOO_LARGE`、`COVER_ASSET_INVALID`。

`RestoreVersion` 对结构化历史版本直接重新校验并按记录的 `(theme_id, theme_version)` 渲染；旧版本先转换。恢复后的新版本完整保存文档、主题、HTML、元信息和封面。

- [ ] **Step 5: 修改微信发布适配器**

`Client.Publish` 从 `draft.Assets` 建立 `map[uint64]editorial.DraftAsset`，不再从 `draft.SourceArticle.Assets` 读取文件。`uploadContentImages` 只解析 `data-weavepress-draft-asset-id`；`uploadAsset/readAsset` 参数改为 `editorial.DraftAsset`。`supportedImage` 对 JPEG/PNG/GIF/WebP 返回 true，并继续执行正文 1 MiB、封面 10 MiB 上限；微信返回不支持时沿用稳定发布错误，不修改稿件版本。

- [ ] **Step 6: 扩展新版保存请求和稿件装饰**

请求体固定为：

```go
type updateDraftInput struct {
    Title           string          `json:"title"`
    Author          string          `json:"author"`
    Digest          string          `json:"digest"`
    EditorDocument json.RawMessage `json:"editorDocument"`
    ThemeID         string          `json:"themeId"`
    CoverAssetID    *uint64         `json:"coverAssetId"`
    ExpectedVersion uint            `json:"expectedVersion"`
    ChangeNote      string          `json:"changeNote"`
}
```

handler 调用 `ParseDocument` 后传给 service。`decorateDraft` 的 `PreviewHTML` 用 `a.content.SignMedia("draft-assets", id)` 替换新占位；`GET /drafts/:id` 返回临时转换结果、`migrationNeeded` 和 `migrationWarnings`。

- [ ] **Step 7: 运行发布链路最小测试并提交**

Run: `cd server; go test ./internal/modules/editorial ./internal/platform/wechat -run 'TestUpdate|TestGetLegacy|TestRestore|TestPreflight|TestPublish'`

Expected: PASS。

```bash
git add server/internal/modules/editorial server/internal/platform/wechat/client.go server/internal/platform/wechat/client_test.go server/internal/transport/weaveapi/api.go
git commit -m "feat: 串联微信排版保存与发布"
```

### Task 7: 固化 OpenAPI 契约

**Files:**
- Modify: `server/api/openapi.yaml`

**Interfaces:**
- Consumes: Tasks 5–6 的实际 HTTP 输入输出。
- Produces: 管理端和后续客户端可依赖的公开契约。

- [ ] **Step 1: 增加三个接口路径**

写入 `GET /api/wechat-layout/themes`、`GET/POST /api/drafts/{id}/assets`、`GET /media/draft-assets/{id}`。上传 requestBody 使用 `multipart/form-data` 且 `file` 为 required binary；成功状态用 201，413 声明统一错误响应。

- [ ] **Step 2: 扩展 schema**

新增 `EditorDocument`、`EditorNode`、`EditorMark`、`DraftAsset`、`ThemeSummary`、`ThemeTokens`；`Draft`/`DraftVersion` 增加 `editorDocument`、`themeId`、`themeVersion`，`Draft` 再增加 `migrationNeeded`、`migrationWarnings`。`UpdateDraftRequest.required` 固定包含 `title`、`editorDocument`、`themeId`、`expectedVersion`，并移除 `contentHtml`。

- [ ] **Step 3: 对照真实路由做静态检查并提交**

Run: `rg -n "wechat-layout/themes|drafts/\{id\}/assets|editorDocument|draftAssetId|themeVersion" server/api/openapi.yaml server/internal/transport/weaveapi/api.go`

Expected: 每个新增路径和字段同时出现在契约及实现中，名称大小写一致。

```bash
git add server/api/openapi.yaml
git commit -m "docs: 更新微信排版接口契约"
```

### Task 8: 搭建 TipTap 编辑器基础和前端数据协议

**Files:**
- Modify: `admin/apps/web-ele/package.json`
- Modify: `admin/pnpm-lock.yaml`
- Modify: `admin/apps/web-ele/src/api/content.ts`
- Create: `admin/apps/web-ele/src/components/wechat-layout/document.ts`
- Create: `admin/apps/web-ele/src/components/wechat-layout/document.test.ts`
- Create: `admin/apps/web-ele/src/components/wechat-layout/wechat-image.ts`
- Create: `admin/apps/web-ele/src/components/wechat-layout/editor-toolbar.vue`

**Interfaces:**
- Consumes: Task 7 的 JSON 和 API 契约。
- Produces: `EditorDocument` TS 类型、主题/素材 API、`documentFingerprint`、`renderPreviewHtml`、`WechatImage` TipTap Node。

- [ ] **Step 1: 安装已确认的 TipTap 生产依赖**

Run:

```bash
cd admin
pnpm --filter @nova/admin add @tiptap/vue-3 @tiptap/starter-kit @tiptap/extension-link @tiptap/extension-underline @tiptap/extension-placeholder @tiptap/extension-image
```

Expected: 依赖写入 `apps/web-ele/package.json`，锁文件只出现 TipTap 及其传递依赖。

- [ ] **Step 2: 扩展 API 类型和方法**

在 `content.ts` 定义与 Go JSON 完全一致的 `EditorDocument`、`EditorNode`、`EditorMark`、`EditorImageAttrs`、`DraftAsset`、`WeChatLayoutTheme`；`Draft.editorDocument` 为对象而非字符串。新增：

```ts
export function getWeChatLayoutThemesApi() {
  return requestClient.get<WeChatLayoutTheme[]>('/wechat-layout/themes');
}
export function getDraftAssetsApi(id: number) {
  return requestClient.get<DraftAsset[]>(`/drafts/${id}/assets`);
}
export function uploadDraftAssetApi(id: number, file: File) {
  return requestClient.upload<DraftAsset>(`/drafts/${id}/assets`, { file });
}
```

`updateDraftApi` 输入替换为 `editorDocument` 和 `themeId`，删除 `contentHtml`。

- [ ] **Step 3: 写前端纯函数测试**

```ts
it('renders escaped themed preview and draft asset URL', () => {
  const html = renderPreviewHtml(
    { type: 'doc', content: [
      { type: 'paragraph', content: [{ type: 'text', text: '<正文>' }] },
      { type: 'image', attrs: { draftAssetId: 18, width: 75, align: 'center', alt: '图', caption: '说明' } },
    ] },
    theme,
    new Map([[18, '/media/draft-assets/18?signed=1']]),
  );
  expect(html).toContain('&lt;正文&gt;');
  expect(html).toContain('/media/draft-assets/18?signed=1');
  expect(html).not.toContain('<正文>');
});

it('uses a stable fingerprint for dirty checking', () => {
  expect(documentFingerprint(document, 'clear-blue', metadata))
    .toBe(documentFingerprint(structuredClone(document), 'clear-blue', Object.assign({}, metadata)));
});
```

- [ ] **Step 4: 运行纯函数测试确认失败**

Run: `cd admin; pnpm exec vitest run apps/web-ele/src/components/wechat-layout/document.test.ts`

Expected: FAIL，提示模块未定义。

- [ ] **Step 5: 实现文档工具和图片 Node**

`document.ts` 导出：

```ts
export function normalizeDocument(value: EditorDocument): EditorDocument
export function documentFingerprint(document: EditorDocument, themeId: string, metadata: DraftMetadata): string
export function renderPreviewHtml(document: EditorDocument, theme: WeChatLayoutTheme, assetURLs: Map<number, string>): string
```

即时预览只按服务端令牌映射受控 style，所有文本和属性经过本地 `escapeHTML`；未知节点不渲染。`wechat-image.ts` 基于 `Node.create` 定义 block/atom/draggable 图片节点，attrs 默认值固定为 `draftAssetId: 0, width: 100, align: 'center', alt: '', caption: ''`，`parseHTML` 只读取 `data-draft-asset-id`，`renderHTML` 不接收 style 字符串。

工具栏只调用 TipTap command：粗体、斜体、下划线、H2、H3、引用、有序/无序列表、分割线和链接；链接输入只接受 `https?://`，取消链接用 `unsetLink()`。

- [ ] **Step 6: 运行测试并提交**

Run: `cd admin; pnpm exec vitest run apps/web-ele/src/components/wechat-layout/document.test.ts`

Expected: PASS。

```bash
git add admin/apps/web-ele/package.json admin/pnpm-lock.yaml admin/apps/web-ele/src/api/content.ts admin/apps/web-ele/src/components/wechat-layout/document.ts admin/apps/web-ele/src/components/wechat-layout/document.test.ts admin/apps/web-ele/src/components/wechat-layout/wechat-image.ts admin/apps/web-ele/src/components/wechat-layout/editor-toolbar.vue
git commit -m "feat: 搭建微信排版编辑器基础"
```

### Task 9: 完成三栏工作台、素材交互和未保存保护

**Files:**
- Create: `admin/apps/web-ele/src/components/wechat-layout/theme-panel.vue`
- Create: `admin/apps/web-ele/src/components/wechat-layout/asset-panel.vue`
- Create: `admin/apps/web-ele/src/components/wechat-layout/phone-preview.vue`
- Create: `admin/apps/web-ele/src/components/wechat-layout/layout-editor.vue`
- Modify: `admin/apps/web-ele/src/views/drafts/detail.vue`

**Interfaces:**
- Consumes: Task 8 的类型、API、预览渲染和 TipTap 扩展。
- Produces: `v-model:document`、`v-model:theme-id`、`cover-change`、`upload` 事件及完整稿件排版页；未保存状态由详情页基于指纹计算。

- [ ] **Step 1: 实现主题和素材面板**

`theme-panel.vue` props 为 `themes: WeChatLayoutTheme[]`、`modelValue: string`、`disabled: boolean`，emit `update:modelValue`；卡片使用 `theme.preview`、`theme.tokens.accent`、`theme.tokens.surface` 绘制，不写死绿色。

`asset-panel.vue` props 为 `assets: DraftAsset[]`、`coverAssetId?: number`、`disabled: boolean`，emit：

```ts
interface Emits {
  insert: [asset: DraftAsset];
  cover: [assetId: number];
  upload: [file: File];
}
```

素材卡显示来源、尺寸、大小、封面状态；`bodyEligible=false` 时禁用“插入正文”并显示“超过微信正文 1 MiB 限制”，不提供删除按钮。上传 input accept 固定为 `.jpg,.jpeg,.png,.gif,.webp` 且一次一张。

- [ ] **Step 2: 实现沙箱手机预览**

`phone-preview.vue` 接收标题、作者、摘要、封面 URL 和 `bodyHtml`。iframe 使用 `sandbox=""`，`srcdoc` CSP 固定为：

```html
<meta http-equiv="Content-Security-Policy" content="default-src 'none'; img-src https: http: data:; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'">
```

手机外框宽 390px、正文可滚动，不执行脚本。编辑时显示 `renderPreviewHtml`；成功保存后父页用服务端 `previewHtml` 替换基线预览。

- [ ] **Step 3: 实现 TipTap 工作台编排**

`layout-editor.vue` 使用 `useEditor`，extensions 固定为 StarterKit（关闭 h1、codeBlock）、Underline、Link、Placeholder、WechatImage。对外接口：

```ts
const props = defineProps<{
  assets: DraftAsset[];
  document: EditorDocument;
  editable: boolean;
  metadata: DraftMetadata;
  themes: WeChatLayoutTheme[];
  themeId: string;
}>();
const emit = defineEmits<{
  'cover-change': [assetId: number];
  'update:document': [value: EditorDocument];
  'update:themeId': [value: string];
  upload: [file: File];
}>();
```

图片插入命令写入五个 attrs；选择图片节点时显示 50/75/100 宽度、alt、caption 控件。图片节点 `draggable=true`，拖动后 `onUpdate` 发出新 JSON。粘贴规则移除 style/class/id/on*，外链 img 不产生节点。

当前主题令牌通过 CSS variables 注入中栏画布，heading、paragraph、blockquote、list、link 和 figure 只引用这些受控变量，使切换主题同时更新中间画布和右侧预览。工具栏增加“复制微信排版”按钮，优先用 `ClipboardItem` 同时写入 `text/html` 和去标签后的 `text/plain`，HTML 必须来自 `renderPreviewHtml`；浏览器不支持富剪贴板时回退到 `navigator.clipboard.writeText(html)`。

桌面 `min-width: 1280px` 使用 `280px minmax(520px, 1fr) 420px` 三栏；更窄时中栏保留，左/右通过两个 Element Plus Drawer 打开。

- [ ] **Step 4: 改造稿件详情页的数据流**

`load()` 并行读取 draft、versions、wechat status、preflight、themes、draft assets。表单源改为：

```ts
const form = reactive({
  title: '', author: '', digest: '', changeNote: '',
  coverAssetId: undefined as number | undefined,
  editorDocument: { type: 'doc', content: [] } as EditorDocument,
  themeId: 'minimal-business',
});
```

保存调用 `updateDraftApi` 提交结构化文档、主题和 expectedVersion；409 时保留本地表单，提示“稿件已被其他人更新，可复制当前内容后刷新”，不调用 `fillForm`。上传失败保留文档；成功把返回素材追加到列表。迁移有警告时显示 ElAlert；转换失败时编辑器只读但保留旧 `previewHtml`。

- [ ] **Step 5: 增加未保存判断和离开拦截**

加载/保存/恢复成功后记录 `savedFingerprint`，computed `dirty` 对比 `documentFingerprint`。使用：

```ts
onBeforeRouteLeave(async () => {
  if (!dirty.value) return true;
  try {
    await ElMessageBox.confirm('当前排版尚未保存，确认离开吗？', '未保存修改', { type: 'warning' });
    return true;
  } catch {
    return false;
  }
});

useEventListener(window, 'beforeunload', (event) => {
  if (!dirty.value) return;
  event.preventDefault();
  event.returnValue = '';
});
```

提交审核按钮在 `dirty=true` 时禁用并提示先保存。只有 `editing` 可改；审核和发布状态保留现有操作与权限指令。

- [ ] **Step 6: 运行管理端增量检查并提交**

Run: `cd admin; pnpm -F @nova/admin run typecheck`

Expected: PASS。

Run: `cd admin; pnpm -F @nova/admin run build`

Expected: Vite production build 成功，稿件详情 chunk 包含 TipTap，其他路由正常拆包。

```bash
git add admin/apps/web-ele/src/components/wechat-layout admin/apps/web-ele/src/views/drafts/detail.vue
git commit -m "feat: 完成微信三栏排版工作台"
```

### Task 10: 最小联调、提交汇总、推送和生产验证

**Files:**
- Modify only if contract mismatch is found: `server/api/openapi.yaml`
- Modify only if production build requires it: `deploy/Dockerfile.gateway`

**Interfaces:**
- Consumes: Tasks 1–9 的完整增量。
- Produces: master 上可部署版本和 `wp.pdurl.cn` 的真实验收证据。

- [ ] **Step 1: 运行服务端最小测试集**

Run:

```bash
cd server
go test ./internal/modules/editorial ./internal/platform/wechat ./internal/transport/weaveapi
```

Expected: PASS；不运行与排版无关的全项目测试。

- [ ] **Step 2: 运行管理端最小测试和生产构建**

Run:

```bash
cd admin
pnpm exec vitest run apps/web-ele/src/components/wechat-layout/document.test.ts
pnpm -F @nova/admin run typecheck
pnpm -F @nova/admin run build
```

Expected: 三条命令全部成功。

- [ ] **Step 3: 本地浏览器走一个最小闭环**

使用现有管理员登录后打开一篇 editing 稿件，完成：打开旧稿 → 插入一张来源图片 → 上传一张小于 1 MiB 的 PNG → 切换 `clear-blue` → 保存新版本 → 刷新页面 → 恢复上一版本 → 发布前检查。确认六主题不改变节点顺序、保存刷新一致、素材 URL 可访问、未保存离开会提示。

- [ ] **Step 4: 检查工作树并创建汇总提交（仅在 Step 1–3 产生修正时）**

Run: `git status --short; git diff --check`

Expected: 无空白错误；只包含排版功能文件。

```bash
git add server admin deploy
git commit -m "fix: 修正微信排版联调问题"
```

若没有联调修正则不创建空提交。

- [ ] **Step 5: 推送 master 并等待现有流水线发布**

Run:

```bash
git push origin master
gh run list --branch master --limit 1
```

Expected: GitHub Actions 最新 run 对应当前完整 commit SHA 且成功；NAS mirror 随后触发 `WeavePressServer` 和 `WeavePressGateway`，数据库迁移先于 API/Worker 切换完成。

- [ ] **Step 6: 验证生产版本和真实页面**

Run:

```bash
git rev-parse HEAD
curl.exe -fsS https://wp.pdurl.cn/ready
curl.exe -fsSI https://wp.pdurl.cn/admin/
```

Expected: `/ready` 返回 `code=0` 和 `status=ready`，`X-WeavePress-Release` 精确等于当前完整 commit SHA。

在真实浏览器刷新 `https://wp.pdurl.cn/admin/#/drafts/{id}`，用生产管理员完成：六主题切换、来源图片插入、上传图片、手机预览、保存、刷新、版本恢复、预检。若微信凭据仍未启用，验收到预检成功并确认“写入公众号草稿箱”保持禁用；不自动群发。

- [ ] **Step 7: 记录最终结果**

最终回复只报告：生产 commit SHA、Server/Gateway 发布结果、真实浏览器完成的场景、微信发布是否因凭据未启用而停在预检。不要把本地成功表述为线上成功。
