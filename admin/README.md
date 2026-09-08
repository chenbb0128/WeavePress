# Nova Admin

Nova Admin 是一个轻量、可复用的 Vue 3 后台管理模板。它基于 [Vue Vben Admin](https://github.com/vbenjs/vue-vben-admin) 二次整理，保留后台系统的基础能力，移除了多 UI 应用、文档站、Playground 和大量演示页面，适合作为新业务后台的起点。

## 特性

- Vue 3、Vite、TypeScript、pnpm workspace
- Element Plus 单应用
- 登录、退出、Token 刷新和基础权限
- 路由、菜单、标签页、面包屑、主题和国际化
- Axios 请求封装和统一错误处理
- Pinia 状态管理
- Dashboard 和用户管理 CRUD 示例
- 本地 Nitro Mock API，随 `pnpm dev` 自动启动
- 403、404、500 异常页
- Docker 构建配置
- GitHub Actions CI
- 模板初始化脚本，可快速替换品牌、包名、端口、API 和 Logo

模板刻意不内置图表、富文本、复杂表格、低代码等重功能。业务需要时再按需安装，可以让项目长期保持轻一些。

## 环境要求

- Node.js `24.19.0` 或更高版本
- pnpm `11.21.0` 或更高版本

项目通过 `.node-version` 固定 Node.js 版本，并通过 `packageManager` 固定 pnpm 版本。

## 快速开始

```bash
pnpm install --frozen-lockfile
pnpm dev
```

打开 `http://localhost:5173`，使用本地 Mock 账号登录：

```text
admin / 123456
```

只需要运行 `pnpm dev`。前端开发服务会自动启动 Nitro Mock，不需要同时运行 `pnpm dev:mock`。

## 创建自己的项目

克隆模板后，先运行初始化脚本：

```bash
pnpm template:init
```

脚本会交互式修改：

- 产品名称和浏览器标题
- 本地存储命名空间和加密 key
- 根包名和前端包名
- 开发端口和 API 地址
- Logo 首字母
- README、Mock、Docker 等默认名称

然后重新安装依赖并启动：

```bash
pnpm install
pnpm dev
```

环境变量示例位于 `apps/web-ele/.env.example`。复制模板时，请为 `VITE_APP_STORE_SECURE_KEY` 设置项目专用值；它只用于前端本地存储混淆，不能替代服务端安全措施。

## 常用命令

```bash
pnpm dev            # 启动前端和本地 Mock
pnpm build          # 生产构建
pnpm preview        # 预览生产构建
pnpm check:type     # TypeScript 类型检查
pnpm check:dep      # 未使用依赖与 workspace 检查
pnpm lint           # 代码和样式检查
pnpm test           # 单元测试
pnpm template:init  # 初始化为自己的项目
```

提交前建议运行：

```bash
pnpm check:dep
pnpm check:type
pnpm lint
pnpm test
pnpm build
```

GitHub Actions 会在推送到 `main` 和 Pull Request 时运行同一组检查。

## 目录结构

```text
apps/
  web-ele/       Element Plus 主应用
  backend-mock/  本地 Mock 服务
packages/        布局、权限、请求、状态等共享能力
internal/        Vite、TypeScript、样式和代码规范配置
scripts/         初始化、检查和部署脚本
```

业务代码主要位于 `apps/web-ele/src`：

- `api`：接口定义和请求入口
- `config`：产品级集中配置
- `locales`：中英文业务文案
- `router`：路由和权限元数据
- `store`：应用状态
- `views`：页面

用户管理页面演示了列表查询、搜索、分页、新增、编辑、删除和按钮权限，可作为第一个业务模块的参考。

## 接入后端

- 开发环境 API：`apps/web-ele/.env.development`
- 生产环境 API：`apps/web-ele/.env.production`
- 请求封装：`apps/web-ele/src/api/request.ts`
- Mock API：`apps/backend-mock/api`

接入真实后端时，将 `VITE_GLOB_API_URL` 指向服务地址，并按后端协议调整登录、用户信息和菜单接口。需要完全停用 Mock 时，将 `VITE_NITRO_MOCK` 设置为 `false`。

## 部署

```bash
pnpm build
```

静态产物输出到 `apps/web-ele/dist`。仓库保留了 `scripts/deploy/Dockerfile` 和本地镜像构建脚本，可按部署平台调整 Nginx 配置与镜像名称。

## 许可证与来源

本项目基于 Vue Vben Admin 整理，继续遵循仓库中的 MIT License。内部包名中仍保留部分 `@vben/*` 命名，用于标识来自上游模板的共享基础能力。
