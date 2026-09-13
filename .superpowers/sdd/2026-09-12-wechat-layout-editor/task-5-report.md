# Task 5 实施报告：稿件素材上传、素材列表与私有媒体访问

## 实现结果

- 新增 `InspectDraftImage`，接受 JPEG、PNG、GIF、WebP，使用实际文件头与声明基础 MIME 双重校验，解析非零宽高并计算 SHA-256。
- 业务层严格限制单文件最大 `10 MiB`；超过正文 `1 MiB`、不超过封面 `10 MiB` 的图片仍可上传，记录为 `BodyEligible=false`、`CoverEligible=true`。
- 上传对象键固定为 `drafts/{yyyy}/{mm}/{sha256}.{ext}`，年/月使用 UTC，相同内容复用对象键。
- Editorial Service 先确认稿件存在且处于 `editing`，再校验、写对象存储、创建素材记录；对象写入失败不会创建数据库记录，流程不修改稿件文档或版本。
- 新增主题列表、稿件素材列表、稿件素材上传和签名私有媒体访问。
- 未新增依赖、队列、删除、自定义处理、保存或发布逻辑。

## 文件

- 新增 `server/internal/modules/editorial/assets.go`
- 新增 `server/internal/modules/editorial/assets_test.go`
- 修改 `server/internal/modules/editorial/store.go`
- 修改 `server/internal/modules/editorial/service.go`
- 修改 `server/internal/modules/editorial/service_test.go`
- 修改 `server/internal/transport/weaveapi/api.go`
- 新增 `server/internal/transport/weaveapi/wechat_layout_test.go`
- 修改 `server/internal/app/api.go`
- 修改 `server/internal/app/worker.go`
- 修改 `server/configs/config.example.yaml`
- 修改 `deploy/production/config.yaml`
- 修改 `deploy/nginx.conf`
- 修改 `deploy/production/nginx/new-server/wp.pdurl.cn.conf`
- 修改 `deploy/production/nginx/old-server/wp.pdurl.cn.conf`

## 构造器与路由变更

- 新增窄接口：`type AssetObjects interface { Put(context.Context, string, []byte, string) error }`。
- Editorial 构造器改为 `New(store Store, articles ArticleStore, queueClient *queue.Client, publisher Publisher, objects AssetObjects, enabled bool) *Service`。
- API 与 Worker 均将既有真实对象存储传入 Editorial；Worker 未新增队列。
- 新增受保护路由：
  - `GET /api/wechat-layout/themes`：`draft:view`
  - `GET /api/drafts/:id/assets`：`draft:view`
  - `POST /api/drafts/:id/assets`：`draft:update`
- 新增签名媒体路由：`GET /media/draft-assets/:id`。
- 现有稿件路由补齐权限：读取/版本/预检 `draft:view`，创建 `draft:create`，保存/恢复 `draft:update`，提交审核 `draft:submit-review`，管理员审核/发布分别叠加 `draft:review`、`draft:publish`。
- `draft-assets` 媒体对象定位调用 `editorial.AssetObject`；签名验证、私有 URL/本地读取继续复用 Content Service。
- 上传 handler 使用 `http.MaxBytesReader` 限制 `11 MiB` multipart 请求，只接受字段名 `file` 的单个文件，拒绝额外文件。
- 错误映射：不支持格式 `400 图片格式不支持`，图片内容无效 `400 图片内容无效`，超过限制 `413 图片超过 10 MiB 限制`，非编辑态 `409 稿件当前不能编辑`。

## 11 MiB 配置

- `server/configs/config.example.yaml` 与 `deploy/production/config.yaml` 的 `http.max_body_bytes` 均改为 `11534336`。
- 仓库内 Gateway、腾讯云 origin、旧服务器 HTTPS bridge 三个目标 `server` block 均加入 `client_max_body_size 11m;`。
- 未修改生产机器外部 Nginx 的真实安装；该工作明确留给 Task 10。

## TDD RED / GREEN

### 图片校验

- RED：`cd server; go test ./internal/modules/editorial -run 'TestInspectDraftImage'`
  - 退出码 `1`；`undefined: InspectDraftImage`。
- GREEN：同命令退出码 `0`；`ok .../internal/modules/editorial 0.064s`。

### Editorial Service

- RED：`cd server; go test ./internal/modules/editorial -run 'TestUploadAsset|TestAssetsAndAssetObject|TestProcess|TestPublishRejects'`
  - 退出码 `1`；新构造器参数过多，且 `service.UploadAsset undefined`。
- GREEN：补最小实现后聚焦命令退出码 `0`；`ok .../internal/modules/editorial 0.064s`。

### HTTP 边界与媒体路由

- RED：`cd server; go test ./internal/transport/weaveapi -run 'TestWeChatLayout|TestDraftAsset'`
  - 退出码 `1`；所有新增目标路由返回 `404 page not found`，而测试分别期望 401/409/400/413/201/200/403。
- GREEN：同命令退出码 `0`；`ok .../internal/transport/weaveapi 0.110s`。

## 最小验证

- `cd server; go test -count=1 ./internal/modules/editorial ./internal/transport/weaveapi -run 'TestInspectDraftImage|TestWeChatLayout|TestDraftAsset'`
  - `ok .../internal/modules/editorial 0.074s`
  - `ok .../internal/transport/weaveapi 0.113s`
- `cd server; go test -count=1 ./internal/app -run '^$'`
  - `ok .../internal/app 0.072s [no tests to run]`
- `git diff --check` 与 `git diff --cached --check` 均退出码 `0`、无输出。
- 按快速 MVP 约束未运行全项目测试或完整构建矩阵。

## 提交

- `8f29f23 feat: 增加微信稿件素材上传`

## 自查

- 四种图片正向覆盖；伪造内容、SVG、声明 MIME 不匹配、零宽高和 `10 MiB + 1` 均有测试。
- HTTP 覆盖未登录 401、非 editing 409、SVG 400、超过 10 MiB 413、额外文件 400、成功 201、主题/素材列表、有效/无效媒体签名。
- 成功响应断言 `origin=upload`、`mediaUrl`、`bodyEligible=false`、`coverEligible=true`。
- 对象存储使用窄接口和内存 fake，测试不访问外网；Task 4 的五个素材 fake 已补为内存真实行为。
- 改动仅限简报列出的生产/测试/仓库配置文件，无无关重构或依赖变化。

## 顾虑

- 对象写入成功但数据库创建记录失败时可能留下未引用对象；按本任务“不实现删除”的边界不做补偿删除。稿件文档和版本不会改变，且内容哈希对象键允许后续相同内容复用。
- 生产机器上两层外部 Nginx 的真实安装尚未执行，按简报由 Task 10 完成。

## Fix round 1/5

### 修复内容

- 修复 WebP 伪文件被接受：`draftWebPDimensions` 不再按固定偏移仅读取尺寸，改为校验 RIFF 声明总长、逐个 WEBP chunk 的头/数据/偶数字节填充边界、VP8X 固定 10 字节画布头与保留位，并要求存在实际 VP8 或 VP8L 图像 chunk。
- VP8 校验 keyframe、同步码、非零宽高、首分区长度及剩余图像 payload；VP8L 校验签名字节、版本、非零 payload；VP8X 图像尺寸必须与画布一致，alpha 标记与 ALPH chunk 必须一致。
- 正向 WebP 样例替换为 libwebp 测试数据中的真实 1×1 VP8、VP8L、VP8X fixture；新增仅 VP8X 画布头、截断 payload、错误 RIFF 长度、错误 chunk 长度拒绝测试。
- 修复私有对象键泄漏：`GET /media/draft-assets/:id` 不再调用或重定向到 `PrivateURL`，始终由服务端 `Open` 后回传；`assets`、`raw` 等原有 kind 的重定向逻辑保持不变。
- 媒体测试模拟含底层 object key 的非空生产 `PrivateURL`，断言 draft-assets 仍返回 200 内容、`PrivateURL` 调用数为 0，且响应头和正文均不含 object key。
- 未新增生产依赖，未修改其他功能范围。

### 修改与测试文件

- 生产：`server/internal/modules/editorial/assets.go`
- 测试：`server/internal/modules/editorial/assets_test.go`
- 生产：`server/internal/transport/weaveapi/api.go`
- 测试：`server/internal/transport/weaveapi/wechat_layout_test.go`

### TDD RED

- `cd server; go test -count=1 ./internal/modules/editorial -run 'TestInspectDraftImage'`
  - 退出码 `1`；旧实现错误接受 `vp8x canvas without image`、`truncated image payload`、`wrong riff length`、`wrong chunk length`，并错误拒绝真实 28 字节 VP8L。
- `cd server; go test -count=1 ./internal/transport/weaveapi -run 'TestDraftAssetPrivateMediaAccess'`
  - 退出码 `1`；实际返回 `307`，响应正文暴露 `https://private-storage.example/drafts/2026/09/private.png`，测试期望 `200` 服务端回传。

### GREEN / 最终验证

- `cd server; go test -count=1 ./internal/modules/editorial -run 'TestInspectDraftImage'`
  - `ok github.com/chenbb0128/weavepress/server/internal/modules/editorial 0.067s`
- `cd server; go test -count=1 ./internal/transport/weaveapi -run 'TestDraftAssetPrivateMediaAccess'`
  - `ok github.com/chenbb0128/weavepress/server/internal/transport/weaveapi 0.077s`
- `cd server; go test -count=1 ./internal/modules/editorial ./internal/transport/weaveapi -run 'TestInspectDraftImage|TestDraftAssetPrivateMediaAccess'`
  - `ok github.com/chenbb0128/weavepress/server/internal/modules/editorial 0.068s`
  - `ok github.com/chenbb0128/weavepress/server/internal/transport/weaveapi 0.099s`
- `git diff --check`
  - 退出码 `0`，无输出。

### 提交

- `774e568 fix: 加固稿件素材校验与私有访问`

## Fix round 2/5

### 修复内容与算法边界

- VP8：在 10 字节未压缩帧头和声明的首分区之后，要求至少保留 4 字节算术编码 token 分区；真实 1×1 fixture 截去 2 字节并同步修正 RIFF/chunk 长度后，剩余 3 字节 token 数据会被拒绝。
- VP8L：payload 最小长度从 6 字节收紧为 8 字节；5 字节图像头之后，最短控制流至少包含 transform/cache/meta 3 位以及 5 棵最简 Huffman 树各 4 位，共 23 位。
- 使用本机缓存的 `golang.org/x/image/webp v0.45.0` 做一次性诊断：VP8 payload 24、23 字节可解码，22 字节起返回 `unexpected EOF`；诊断模块随后已删除，未加入项目依赖。
- 仅修改 WebP 位流长度校验，不修改媒体访问或其他已通过范围。

### 修改与测试文件

- 生产：`server/internal/modules/editorial/assets.go`
- 测试：`server/internal/modules/editorial/assets_test.go`
- 报告：`.superpowers/sdd/2026-09-12-wechat-layout-editor/task-5-report.md`

### TDD RED

- `cd server; go test -count=1 ./internal/modules/editorial -run 'TestInspectDraftImage'`
  - 退出码 `1`；旧实现错误接受 RIFF 与 chunk 长度均自洽、但 VP8 payload 截至 22 字节和 VP8L payload 截至 6 字节的两个样本，均得到 `<nil>`，期望 `ErrDraftAssetInvalid`。

### GREEN

- `cd server; go test -count=1 ./internal/modules/editorial -run 'TestInspectDraftImage'`
  - `ok github.com/chenbb0128/weavepress/server/internal/modules/editorial 0.060s`

### 自查

- 新增两个真实行为回归用例，声明长度均随截断同步调整，测试不会只命中 RIFF 外层长度检查。
- 最小生产改动仅收紧 VP8/VP8L 的合法位流下界；无无关重构。
- 未新增生产依赖；一次性诊断文件及临时 `go.sum` 已清理。

## Fix round 3/5

### 实现与依赖

- `draftWebPDimensions` 保留实际 MIME 识别之后的 RIFF/WEBP 签名、RIFF 声明总长以及逐 chunk 数据与 padding 边界校验；位流语义改由 `golang.org/x/image/webp.Decode` 完整解码，并从解码结果读取宽高。
- 删除手写的 VP8 首分区/token 最小长度、VP8L 最小 payload、VP8X/ALPH 语义及尺寸解析，避免用固定阈值代替完整位流验证。
- 新增直接生产依赖 `golang.org/x/image v0.45.0`；该版本声明 `go 1.25.0`，与当前 server 模块一致。`go mod tidy` 仅新增对应校验和，并将项目已直接使用、版本未变化的 `golang.org/x/net v0.58.0` 从 indirect 分组归入直接依赖。
- 未修改媒体代理或其他已通过范围。

### 修改与测试文件

- 生产：`server/internal/modules/editorial/assets.go`
- 依赖：`server/go.mod`、`server/go.sum`
- 测试：`server/internal/modules/editorial/assets_test.go`
- 测试数据：`server/internal/modules/editorial/testdata/blue-purple-pink.lossy.webp.b64`、`server/internal/modules/editorial/testdata/gopher-doc.1bpp.lossless.webp.b64`
- 报告：`.superpowers/sdd/2026-09-12-wechat-layout-editor/task-5-report.md`

### TDD RED

- `cd server; go test -count=1 ./internal/modules/editorial -run 'TestInspectDraftImage'`
  - 退出码 `1`；旧实现错误接受 RIFF 与图像 chunk 长度均同步修正的三种截断样本：2450 字节真实 VP8 fixture 截去 64 字节、442 字节真实 VP8L fixture 截去 16 字节，以及由同一真实 VP8 位流封装的合法 VP8X fixture 截去 64 字节；三个子测试均得到 `<nil>`，期望 `ErrDraftAssetInvalid`。

### GREEN 与依赖整理

- `cd server; GOPROXY=off GOSUMDB=off go mod tidy`
  - 退出码 `0`，无输出；仅使用本机模块缓存。
- `cd server; go test -count=1 ./internal/modules/editorial -run 'TestInspectDraftImage'`
  - `ok github.com/chenbb0128/weavepress/server/internal/modules/editorial 0.065s`
- 最终重复同一目标测试：`ok github.com/chenbb0128/weavepress/server/internal/modules/editorial 0.064s`，退出码 `0`。
- `git diff --check`
  - 退出码 `0`；仅提示报告文件工作区换行将在 Git 后续操作时由 LF 转为 CRLF，无 whitespace error。

### 覆盖与自查

- 现有真实 1×1 VP8、VP8L、VP8X 正向 fixture 继续通过完整解码。
- 较长 VP8、VP8L、VP8X fixture 的截断测试保留数百至数千字节 payload，不依赖“小于固定最小长度”触发拒绝。
- 只有 VP8X canvas、错误 RIFF 总长、错误 chunk 长度的既有拒绝测试继续保留并通过。
- 新增且仅新增 `golang.org/x/image` 生产依赖；官方测试 fixture 以 Base64 文本固化，测试不访问网络。
