# aegis-console — 管理前端

> 面包屑：[Aegis](../CLAUDE.md) › aegis-console

## 技术栈

| 技术 | 版本 |
|---|---|
| Next.js | ^16.3.0 |
| React | 19.2.8 |
| TypeScript | 7.0.2（编译器）+ 6.0.2（工具链 API，见下） |
| Tailwind CSS | ^4.3.3 |
| shadcn/ui | new-york-v4，32 个官方组件均为最新版（见下方扩展说明） |
| radix-ui | ^1.6.7（**统一包**，已取代 32 个 `@radix-ui/react-*` 独立包） |
| TanStack React Query | ^5.101.4 |
| TanStack React Table | ^9.1.2（表格，v9 按需注册功能） |
| Zustand | ^5.0.14 |
| next-themes | ^0.4.6 |
| Zod | ^4.4.3（表单校验） |
| @xyflow/react | ^12.11.2（工作流画布） |
| Recharts | ^3.10.1（图表） |
| MapLibre GL | ^6.2.0（地图，无 default export） |
| three | ^0.185.1 |
| lucide-react | ^1.31.0（通用图标） |
| @icons-pack/react-simple-icons | ^13.13.0（品牌图标，代码示例的语言 Tab 用） |
| highlight.js | ^11.11.1（代码高亮，**只用 core + 按需注册**，见下方接入示例一节） |
| Sonner | ^2.0.8（Toast） |
| Motion | ^13.0.0（动画；侧边栏用到 `layoutId` 滑动高亮、`AnimatePresence` 折叠、`Reorder` 拖拽排序） |
| cmdk | ^1.1.1（命令面板 `⌘K` 的底座，shadcn `command` 组件依赖它） |
| pinyin-pro | ^3.28.2（命令面板的拼音检索，**动态 import 按需加载**，见下方侧边栏一节） |
| ESLint | ^9.39.5 |
| pnpm | 11.21.0 |

### shadcn/ui 组件与项目扩展

组件源码由 CLI 复制进 `src/components/ui/`，**再次执行 `shadcn add --overwrite` 会覆盖下列扩展**，
升级后必须逐项补回（本文件即为该清单）：

| 组件 | 项目扩展 | 为什么不能丢 |
|---|---|---|
| `badge.tsx` | `success` / `warning` / `danger` / `info` 语义色 + `size`(sm/default/lg) | 官方无这些变体，183 处业务调用中 16 处依赖 `danger`/`success` |
| `avatar.tsx` | 图片 Blob LRU 缓存（200 条）、`evictAvatarCache()`、`preview` 点击预览 | 属业务功能：`admin-hooks.ts` 导入 `evictAvatarCache`，`console-shell` 用 `preview` |
| `input.tsx` | `suppressHydrationWarning` | 抑制浏览器扩展注入属性导致的 React 19 水合告警 |
| `breadcrumb` / `dialog` / `sheet` | `sr-only` 文案中文化（"更多" / "关闭"） | 无障碍文案本地化 |

`src/components/ui/` 下另有 9 个**项目自有组件**（非 shadcn，CLI 不会碰）：
`country-flag`、`data-state`、`error-boundary`、`image-dropzone`、`image-lightbox`、
`json-viewer`、`rich-editor`、`section-heading`、`surface-card`、`toast-detail`、`virtual-list`。

> **统一 radix-ui 包**：新版组件一律 `import { Dialog as DialogPrimitive } from "radix-ui"`，
> 不再使用 `@radix-ui/react-*`。新增代码请沿用统一包写法，否则会重新引入已删除的依赖。

### TypeScript 7 / 6 并存（side-by-side）

TypeScript 7 是 Go 重写的编译器（tsgo），**不再提供 JS 版的 compiler API**，而 `typescript-eslint@8.66.0`
仍需要该 API，直接装 TS 7 会让它抛错、`pnpm lint` 完全不可用
（见 [typescript-eslint#10940](https://github.com/typescript-eslint/typescript-eslint/issues/10940)）。

因此采用 TS 官方的 side-by-side 方案，靠 npm alias 把「包名」和「二进制」分开路由：

```jsonc
"devDependencies": {
  "@typescript/native": "npm:typescript@^7.0.2",              // 提供 tsc  → TS 7
  "typescript":         "npm:@typescript/typescript6@^6.0.2"  // 提供 tsc6 + JS API → TS 6
}
```

| 使用方 | 解析到 | 说明 |
|---|---|---|
| `pnpm typecheck`（`tsc --noEmit`） | **TS 7.0.2** | 权威类型校验 |
| `pnpm typecheck:ts6`（`tsc6 --noEmit`） | TS 6.0.2 | 对照校验，与 build 期一致 |
| `require("typescript")`（typescript-eslint） | TS 6.0.2 | 两个包的 bin 名不冲突（`tsc` / `tsc6`） |
| `next build` 期类型检查 | TS 6.0.2 API | 见下方 `useTypeScriptCli` |

**`next.config.ts` 中的 `experimental.useTypeScriptCli: false` 不可删除**：Next 16.3 该项默认为 `true`，
CLI 模式会去找 `typescript/bin/tsc`，而 TS 6 兼容包只提供 `bin/tsc6`，Next 会误判「typescript 未安装」
并自动执行 `pnpm install --save-dev typescript`，把上面的 alias 覆盖掉。关闭后 Next 改用 API 模式
（`typescript/lib/typescript.js`，该文件存在），build 期类型检查恢复正常。

> 待 typescript-eslint 支持 TS ≥7.1 后，可移除 `@typescript/native` 别名、把 `typescript` 直接指向 7.x，
> 并删除 `useTypeScriptCli: false` 与 `typecheck:ts6` 脚本。

### 版本上限说明

- **ESLint 停在 9.39.5**：ESLint 10 已发布，但 `eslint-config-next` 依赖的
  `eslint-plugin-react`(≤7.37.5)、`eslint-plugin-jsx-a11y`(6.10.2)、`eslint-plugin-import`(2.32.0)
  peer 上限均为 `^9`，实测 ESLint 10 下 lint 直接崩溃（`scopeManager.addGlobals is not a function`）。

> 本目录存在独立的 `pnpm-workspace.yaml`：上级 `userSystem/` 是旧 Node.js 系统的 workspace 根，
> 缺少该文件会导致 pnpm 把依赖装到上级目录。请勿删除。

## 目录结构

```
src/
├── app/
│   ├── (auth)/login/           # 登录页
│   ├── (console)/              # 管理台路由组（需认证）
│   │   ├── layout.tsx          # 控制台 Shell 布局
│   │   ├── overview/           # 系统总览
│   │   ├── apps/               # 应用列表 + [appKey]/ 二级详情（应用级配置的唯一归属地）
│   │   ├── users/              # 用户管理
│   │   ├── platform/           # 平台治理台（全站作用域，无应用选择器）
│   │   ├── configuration/      # 平台级配置（不含任何应用级配置）
│   │   ├── content/            # 内容中心（Banner / 应用公告 / 系统公告）
│   │   ├── commerce/           # 交易中心（订单 / 退款 / 钱包流水 / 会员 / 凭证）
│   │   ├── storage/            # 存储管理
│   │   ├── workflows/          # 工作流（含画布编辑器）
│   │   ├── functions/          # 远程函数（函数版本 + 调用密钥）
│   │   ├── releases/           # 版本发布
│   │   ├── organization/       # 组织中心（组织 / 部门 / 成员 / 角色 / 审批）
│   │   ├── reviews/            # 角色申请审批
│   │   ├── security/           # 安全运行态（日志 / 封禁 / 地理风控）
│   │   └── profile/            # 管理员个人资料
│   ├── developers/             # 公开开发者门户（免登录，不走 AuthGate）
│   │   ├── layout.tsx          # PortalShell（独立外壳，无侧边栏）
│   │   ├── page.tsx            # 快速接入（三档安全等级 + 多语言示例）
│   │   └── api/                # 接口文档（消费 /openapi.json）
│   ├── api/bing-wallpaper/     # Next.js API Route（必应壁纸代理）
│   └── status/                 # 状态页
├── components/
│   ├── auth/                   # AuthGate、LoginForm、LoginBackground
│   ├── brand/                  # AegisMark、BrandHome、PublicHeader
│   ├── dashboard/              # MetricCard、ActivityFeed
│   ├── developers/             # PortalShell、CodeBlock、CodeSamples（门户 + 接入页共用）
│   ├── functions/              # FunctionManager、FunctionKeysPanel
│   ├── layout/                 # ConsoleShell（外壳装配：侧边栏 + 顶栏 + 快捷键）
│   │   └── sidebar/            # 侧边栏各部件（导航树 / 折叠轨道 / 命令面板 / 宽度手柄）
│   ├── configuration/          # 平台级配置面板（品牌、出海代理网关）
│   ├── monitor/                # AvailabilityDashboard
│   └── ui/                     # shadcn/ui 组件库
└── lib/
    ├── api/                    # API 客户端模块（按域拆分）
    │   ├── client.ts           # 基础 HTTP 客户端（fetch 封装）
    │   ├── auth.ts             # 认证 API
    │   ├── admin.ts            # 管理员 API
    │   ├── apps.ts             # App 维度 API（列表 / 统计 / 策略 / 应用级审计）
    │   ├── app-users.ts        # 单用户维度 API（封禁 / 钱包 / 单用户审计）
    │   ├── app-auth-protocol.ts # 接入配置 / 应用密钥 / 自检 / Transport 密钥 / /config
    │   ├── app-functions.ts    # 远程函数 / 版本 / 调用密钥
    │   ├── openapi.ts          # OpenAPI 类型、摊平/分组、curl 生成
    │   ├── monitor.ts          # 监控 API
    │   ├── configuration.ts    # 平台配置 API
    │   ├── egress.ts           # 出海代理网关 API
    │   ├── storage.ts          # 存储 API
    │   ├── workflow.ts         # 工作流 API
    │   └── types.ts            # 共享类型
    ├── auth-store.ts           # Zustand 认证状态（token、admin info）
    ├── sidebar-store.ts        # Zustand 侧边栏状态（折叠 / 宽度 / 分组 / 子树 / 收藏 / 最近访问）
    ├── api-client.ts           # 高层 API 客户端（含自动 token 注入）
    ├── admin-hooks.ts          # 常用 React Query hooks
    ├── app-user-hooks.ts       # 用户详情页 hooks（封禁 / 钱包 / 会员 / 单用户审计）
    ├── egress-hooks.ts         # 出海代理网关 hooks（配置 / 自测 / 路由解释 / 探测）
    ├── use-client-value.ts     # useIsClient / useOrigin（useSyncExternalStore）
    ├── providers.tsx           # QueryClient + ThemeProvider
    ├── query-client.ts         # TanStack Query 全局配置
    ├── navigation.ts           # 侧边栏三级导航（分组 → 页面 → 页内子项）+ 压平后的跳转目标
    ├── navigation-hooks.ts     # 权限过滤后的分组 / 跳转目标 / key→目标索引
    ├── pinyin-search.ts        # 命令面板的拼音检索（pinyin-pro 动态加载）
    └── env.ts                  # 环境变量（NEXT_PUBLIC_*）
```

## 开发者门户（/developers）

面向**接入方**的公开页面，与面向**管理员**的控制台严格分离：

| | 开发者门户 | 控制台 |
|---|---|---|
| 路径 | `/developers`、`/developers/api` | `/apps`、`/functions` 等 |
| 认证 | 免登录 | AuthGate |
| 外壳 | `PortalShell` | `ConsoleShell` |
| 内容 | 怎么接入（协议、示例代码、接口清单） | 怎么配置（策略、密钥、版本） |

- 接口文档直接消费后端实时生成的 `/openapi.json`（`next.config.ts` 有同源 rewrite，
  **不要删**，否则请求会打到 Next.js 自身并 404）。
- 后端 `/docs` 与 `/docs/tags/:slug` 已改为 302 到本门户，由 `DOCS_PORTAL_URL` 配置目标；
  Go 侧不再有任何手写 HTML 文档页。

### 接口文档（/developers/api）—— 可调试的参考手册

```
lib/api/openapi.ts             规范类型、摊平、分组、schema 采样
lib/api/openapi-request.ts     表单值 → 真实请求 → 七种语言代码示例
lib/developer-credentials.ts   调试凭据（localStorage + useSyncExternalStore）
components/developers/
  api-console.tsx              调试台：参数表单 / 发送 / 响应查看 / 代码示例
  schema-view.tsx              递归展开的 schema 树
```

- **调试面板、URL 预览、代码示例三者同源**：都由 `buildRequest()` 的返回值驱动，
  面板改什么，生成的 curl / Python / Go 就是什么，不会各说各话。
- 请求由浏览器直接发出。默认 baseUrl 与控制台同源（走 `/api` 反代），因此不涉及跨域；
  填了外部地址被 CORS 拦下时，`fetch` 抛错会原样提示。
- 代码示例中的令牌一律替换成 `$TOKEN` / `$ADMIN_TOKEN` 占位符
  （`maskedHeaders()`），避免复制出去的片段带着真实凭据。
- 凭据用 `useSyncExternalStore` 读写 localStorage，**不要改回 `useEffect` + `setState`** ——
  后者触发级联渲染且过不了 `react-hooks/set-state-in-effect`，与 `use-client-value.ts` 同一约束。
- 切换接口靠 `key={selected.key}` 整体重挂载 `ApiConsole` 来重置表单与响应，
  组件内部因此不需要任何同步 effect。
- 单个接口可深链：`?op=<METHOD> <path>`，「复制链接」按钮生成的就是它。

> **当前数据缺口**：778 个端点里只有 14 个带 `application/json` 请求体 schema，
> 409 个写操作缺 395 个。原因是 `internal/transport/http/docs.go` 的 `routeMetadata`
> 需要为每条路由手工登记 `RequestModel`，目前只覆盖了几十条。
> 未登记的接口在调试台上没有请求体编辑框，只能手填。补齐前这个页面对写接口的价值有限。

### 接入示例：一份数据，两处渲染

```
lib/integration-snippets.ts   buildScenarios() —— 场景 × 语言的示例数据
lib/code-languages.tsx        语言登记表：品牌图标 + highlight.js 语法 id + 品牌色
lib/highlight.ts              highlight.js core + 按需注册
components/developers/
  code-samples.tsx            场景/语言两级选择器（门户与控制台共用）
  code-block.tsx              单个代码块：图标 + 高亮 + 复制
```

- **示例只有一份**：`buildScenarios()` 同时供门户 `/developers` 与控制台「接入」页使用，
  两处不可能漂移。**新增语言只改 `code-languages.tsx` 登记 + 在对应场景数组里加一项**，
  UI 会自动多出一个带图标的 Tab。
- 只有「登录」场景随安全等级变化；其余场景的包装方式与它一致，统一按标准档展示形状。
- 高亮用 highlight.js 的 **core + 按需注册**，不是默认入口 —— 后者会把 190+ 种语法
  （~900KB）打进包里。token 配色定义在 `globals.css` 的 `.hljs-*` 规则，
  取自主题变量；**不要引 highlight.js 自带的主题 CSS**，那些主题写死颜色，深浅色切换会失效。
- 示例里的包装规格与后端逐字节对齐（签名 canonical、请求/响应 AAD、HKDF salt、
  `aegis-response-v2` 派生）。真实来源有三处，改协议时必须同步：

  | 位置 | 角色 |
  |---|---|
  | `internal/service/auth_protocol_service.go` | 服务端校验（`computeRequestSignature` / `transportRequestAAD` / `deriveTransportKey`） |
  | `internal/service/auth_protocol_selftest.go` | 参考客户端实现，`/auth-protocol/selftest` 会实跑 |
  | `lib/integration-snippets.ts` | 给接入方抄的示例 |

  漏改任何一处，控制台「接入自检」会立刻红掉 —— 这就是它存在的意义。

## 开发命令

```bash
cd aegis-console
pnpm dev          # 开发服务器（自动清理 .next 缓存）
pnpm build        # 生产构建
pnpm start        # 生产启动
pnpm typecheck    # tsc --noEmit
pnpm lint         # ESLint
pnpm clean        # 手动清理 .next
```

## 环境变量

复制 `.env.example` 为 `.env.local`：

```
NEXT_PUBLIC_API_BASE_URL=http://localhost:8088
```

## 设计规范

- **主题**：`next-themes` 管理深色/浅色，通过 `globals.css` CSS 变量实现
- **配色**：基于 Zinc 色阶（shadcn/ui 默认），浅色模式无阴影扁平风格
- **状态管理**：Zustand（认证 + 侧边栏），服务端状态用 React Query
- **API 调用**：所有 API 调用通过 `src/lib/api/` 模块，禁止在组件中直接 fetch
- **路由保护**：`AuthGate` 组件在 console layout 中守卫，未登录跳转 `/login`

## 侧边栏导航

`src/lib/navigation.ts` 定义三级结构，`components/layout/sidebar/` 负责渲染：

| 层级 | 类型 | 说明 |
|---|---|---|
| 一级 | `NavigationGroup` | 分组标题（概览 / 应用与内容 / 开发者 / 用户与权限 / 平台治理 / 安全与风控 / 平台运维），可折叠；整组无可见项时连标题一起隐藏 |
| 二级 | `NavigationItem` | 页面，带 `icon` + `permission`/`superAdmin` 鉴权 |
| 三级 | `NavigationChild` | 页内 Tab，链接为 `${item.href}?tab=${tab}` |

三级树是给「浏览」用的。但这份目录压平之后有 **130+ 个跳转目标**，
浏览到第 130 项没有意义 —— 所以同一份目录还被压成一维的 `NavigationTarget`
（`navigationTargets`，key 就是链接本身），供命令面板 / 收藏 / 最近访问消费。
**三级树与一维目标同源**，不存在"侧边栏有、面板里搜不到"。

| 文件 | 职责 |
|---|---|
| `layout/console-shell.tsx` | 外壳装配：侧边栏骨架 + 顶栏 + 全局快捷键 + 面包屑 |
| `layout/sidebar/sidebar-nav.tsx` | 展开态：收藏区（`Reorder` 拖拽排序）+ 分组 → 页面 → 页内子项 |
| `layout/sidebar/sidebar-rail.tsx` | 折叠态图标轨道；有子项的页面靠悬浮浮层列出全部面板 |
| `layout/sidebar/command-palette.tsx` | `⌘K` 命令面板：跳转 + 主题 / 收藏 / 退出等操作 |
| `layout/sidebar/sidebar-resizer.tsx` | 右边缘宽度拖拽手柄（208–380px，双击复位） |
| `layout/sidebar/sidebar-shared.tsx` | `ActivePill` / `PinToggle` / `Kbd` / 缓动常量 |
| `layout/sidebar/recent-tracker.tsx` | 最近访问打点（渲染 `null`） |
| `lib/navigation-hooks.ts` | 权限过滤后的分组与目标、`key → 目标` 索引 |
| `lib/pinyin-search.ts` | 拼音检索，`pinyin-pro` 动态加载 + `useSyncExternalStore` 通知就绪 |

快捷键：`⌘K` / `Ctrl K` 命令面板，`⌘B` / `Ctrl B` 折叠切换（在输入框内不抢）。

新增导航时注意：

- **三级子项只能挂在「Tab 同步到 URL」的页面上**（当前为 `/apps`、`/users`、`/reports`、`/storage`、`/functions`、
  `/tickets`、`/commerce`、`/configuration`、`/security`、`/risk-control`，即 `const tab = searchParams.get("tab")` + `router.replace(...?tab=)` 那套写法）。
  若某页 Tab 只存在于组件 state，登记子项后链接点了不会切面板。
  `children` 数组的**首项必须是该页默认 Tab**，否则无 `?tab=` 时高亮会错位。
- 分组鉴权用 `usePermissionChecker()` 批量过滤（Hook 不能在列表里逐项调用）。
  一维目标同样过滤 —— 否则命令面板会成为绕过侧边栏鉴权的后门。
- 侧边栏读 `?tab=` 需要 `useSearchParams()`，必须包在 Suspense 边界内
  （`console-shell.tsx` 的 `withActiveTab()` 已封装），否则 `next build` 报错、页面退化为客户端渲染。
  命令面板与最近访问打点自带 `useSearchParams()`，Shell 里各自套了 `<Suspense fallback={null}>` ——
  **不要把边界上提到整个 Shell**，那会让主内容区在边界 resolve 时整体重挂载。
- 承载导航的 `ScrollArea` 必须带 `min-h-0`，否则子项展开后会把底部用户区顶出屏幕。

### 收藏与最近访问：只存 key

两者都只存 `NavigationTarget.key`（`/users` 或 `/users?tab=admins`），标题与图标渲染时实时解析。

这样改个页面显示名不会在收藏区留下一条过期文案，删掉的页面、以及**当前管理员无权访问的页面**
会自动从收藏与最近里消失（`resolveTargets()` 查不到就跳过）。存标题反而要额外写一层同步。

拖拽排序的 `onReorder` 收到的是"可见收藏"的新顺序，借这次写回把解析不出来的 key 一并清掉。

### 动效的两条约束

1. **`layoutId` 必须按实例隔离。** 同一个 `layoutId` 在同一 `LayoutGroup` 内只能有一个实例，
   而同一页可能**同时**出现在收藏区和导航树里，桌面侧边栏与移动端抽屉也可能同时挂载。
   因此收藏区 / 导航树 / 折叠轨道 / 移动端抽屉各自套一层 `<LayoutGroup id>` 命名空间，
   否则两个实例会互相抢高亮，其中一个直接消失。
2. **拖拽宽度期间要关掉 `transition-[width]`。** 否则每一帧都在补间上一帧，手感明显发黏
   （`console-shell.tsx` 的 `resizing` state 就是干这个的）。

`prefers-reduced-motion` 下滑动与折叠动画全部退化为 0 时长，走 `useReducedMotion()`。

### 拼音检索为什么是动态加载的

`pinyin-pro` 的词典约 200KB。它只在命令面板里用，所以走 `import("pinyin-pro")`：
Shell 挂载 1.5s 后预热一次，面板打开时再兜底触发一次
（**不能只靠 Radix 的 `onOpenChange`** —— 那只在 Radix 自己改状态时触发，
父组件把 `open` 置 true 时它不响）。词典没到位时退回纯文本匹配，面板永远可用。

命令面板**自己算过滤**（`shouldFilter={false}`），不用 cmdk 内置的：
词典是异步到位的，就绪那一刻必须让已输入的关键词重新过一遍，
而内置过滤只在 search 变化时重算，会把"刚打完字、词典才加载完"这一秒钉在空结果上。

打分顺序：字面全等 > 前缀 > 包含 > 标题拼音 > 所属页面/分组拼音。
拼音档内再按「从第一个字起算」与「字字相邻」加成 —— 少了这一层，
输 `yh` 时「用户」和「远程函数」同分，排序退化成目录顺序，正命中的那个反而靠后。

## 配置的作用域划分（改配置页前必读）

配置只有两种作用域，各有唯一归属页面。**新增配置项先确定它属于哪一档，不要两边都放。**

| 作用域 | 页面 | 判据 |
|---|---|---|
| 应用级 | `/apps/{appKey}`（路径里的 appKey 即作用域） | 换个应用这项配置会不同 |
| 平台级 | `/configuration`（无应用选择器，超管） | 对所有应用一视同仁 |

`/platform`（治理台）是第四类，它**不放配置**：放的是平台对应用的**强制结论**
（冻结 / 封禁 / 限制 / 申诉裁决）。与 `/apps` 的分工不是「哪一层的配置」，
而是「谁说了算」—— `/apps` 上的开关应用管理员自己能改，治理台上的结论他改不动。
被治理时 `/apps/{appKey}` 顶部会出现红色横幅（`components/apps/app-governance-notice.tsx`），
告诉他被怎么了、为什么、以及申诉入口；没有它，应用管理员只会遇到一连串不明所以的 403。

`/security` 是第三个相关页面，但它**不放配置**：只管运行态（拦截日志 / IP·地域封禁 / 地理风控 / 账户安全）。
「防火墙规则在 /configuration、拦截日志在 /security」是刻意的分工 —— 前者是「改了什么」，后者是「发生了什么」。

历史问题（已修复，勿回退）：

- `/configuration` 曾同时挂应用选择器与平台级 Tab，其中「品牌」「系统安全」完全忽略那个选择器；
  「访问策略」「密码策略」与 `/apps?tab=policy` 重复，且那一份实现更差（裸 `<input type=checkbox>` +
  字符串输入框 + `JSON.stringify` 打屏）。现已全部删除。
- 防火墙的**规则配置**在 `/configuration`「系统安全」、**拦截日志与封禁**在 `/security`，同一件事跨两页。
  规则配置已并入 `/configuration?tab=firewall`。
- `/apps?tab=policy` 里混着「传输加密」，它其实只作用于旧 `/api/auth`、`/api/user` 命名空间，
  与「接入安全等级」是两套机制。已移到 `/apps?tab=auth-protocol`，且只在 `allowLegacy` 开启时渲染。

### 应用管理：列表页 + 二级详情页

| 路由 | 职责 |
|---|---|
| `/apps` | 应用列表：汇总即筛选、搜索、卡片 / 表格双视图、逐行快捷开关与删除 |
| `/apps/{appKey}` | 该应用的全部配置：顶部标识条 + 左侧分组区块导航 + `?tab=` 选中的那一个面板 |

| 文件 | 职责 |
|---|---|
| `lib/app-sections.ts` | **区块目录**：13 个区块的键 / 标题 / 说明 / 图标 + 分组 |
| `lib/app-scope-store.ts` | 最近打开的应用、列表视图偏好（localStorage） |
| `components/apps/app-shared.tsx` | 标识块、状态徽标、AppKey 复制、格式化 |
| `components/apps/app-list-views.tsx` | 卡片网格 / 表格两种列表视图 |
| `components/apps/app-row-actions.tsx` | 单应用操作菜单（列表行与详情页头部共用） |
| `components/apps/app-detail-header.tsx` | 详情页标识条 + 带区块换应用 + 关联页面入口 |
| `components/apps/app-section-nav.tsx` | 区块导航（宽屏竖向分组 / 窄屏横向胶囊） |

四条硬约束：

1. **区块目录是单一事实源**。侧边栏三级子项由 `appSections` 派生，详情页导航与列表页
   快捷入口也读它 —— 在任一处另抄一份，就会出现「侧边栏有这一项、详情页没这个区块」。
2. **`?tab=` 的键不可改名**。`policy` 与 `auth-protocol` 刻意保留旧名（显示名是
   「认证与会话」「接入」），已分享出去的深链不该因为改叫法而失效。
3. **侧边栏子项链接不带 appKey**（`/apps?tab=oauth`），由列表页转交给
   `app-scope-store` 记住的最近应用。因此**详情页必须写这条记忆**，
   否则侧边栏的 13 个子项会永远落到第一个应用上。
4. **详情页只挂载当前区块**。13 个面板同时挂载会在进页面的一瞬间打出十几条请求
   （OAuth 渠道、抽奖奖池、密码策略模板……），其中绝大多数当次不会被看到。

列表页的用户数 / 今日新增来自治理总览接口（一次返回全部应用的聚合值），
它需要 `platform:app:read`；没有该权限时整块指标**不渲染**而不是显示一排 0 ——
「查不到」和「真的是 0」必须能区分开。

旧形状（已废弃，勿回退）：应用是右上角一个下拉框，13 个 Tab 平铺在同一屏。
那种形状下既数不出自己有几个应用、也看不出哪个被停用了，「切换应用」不过是
换掉当前 Tab 的数据源，其余上下文全部丢失；分享出去的 `/apps?tab=oauth`
更是只说了区块、没说应用。

### 表单状态：草稿按作用域绑定，不要用 useEffect 同步

配置面板一律用这个形状，**不要改回 `useEffect` + `setState`**
（既触发级联渲染，也过不了 `react-hooks/set-state-in-effect`，与门户凭据那条约束同源）：

```tsx
const [draft, setDraft] = useState<{ scope: string; value: Form } | null>(null);
const scope = appKey ?? "";
const form = draft?.scope === scope ? draft.value : seed(query.data);   // 无草稿即从服务端派生
const patch = (k, v) => setDraft({ scope, value: { ...form, [k]: v } });
```

除了省掉一个 effect，它还顺手解决了「切换应用时上一个应用的未保存改动串过来」——
用 effect 同步反而要额外写一层比对才能避免。平台级面板（如 SAML）没有 scope，
用 `localDraft ?? seedDraft(server)`，「重置」就是 `setLocalDraft(null)`。

### 密钥类字段一律「留空即不修改」

SP 私钥 / IdP 元数据 XML / 对称密钥 / Client Secret 等编辑态**从不回显**，
只有 `hasXxx` 布尔位表示已配置。因此保存时必须判空后再放进 payload ——
无条件下发空串会让「改个显示名」把已配好的凭据清空。

## 组织中心（/organization）

租户边界的管理界面，Tab 同步到 URL（`?org=<uuid>&tab=...`）：

| 文件 | 职责 |
|---|---|
| `(console)/organization/page.tsx` | 页面骨架、组织切换、Tab 路由 |
| `components/organization/org-shared.tsx` | 权限闸门 `useOrgCan`、成员选择器、权限勾选树、徽标 |
| `components/organization/org-structure.tsx` | 部门树（拖拽移动）+ 部门详情与成员 |
| `components/organization/org-members.tsx` | 组织成员管理、邀请中心 |
| `components/organization/org-chart.tsx` | 架构图（@xyflow/react）：部门架构 + 汇报关系 |
| `components/organization/org-governance.tsx` | 组织角色、岗位、审批、权限模板、协作组、应用绑定 |
| `components/organization/org-settings.tsx` | 组织资料、转让、删除、导入导出、操作日志 |
| `lib/api/organization.ts` | 组织域 API（全部用 UUID） |
| `lib/org-hooks.ts` | 组织域 React Query hooks |

四条硬约束：

1. **实体 id 是 UUID 字符串，不是数字**。后端 `Organization.ID`（自增）带 `json:"-"`，
   序列化出来的 `id` 是 `uuid`。把它当 number 用会在拼 URL 时静默出错。
2. **按钮显隐一律读 `access.permissions`**（`GET /organizations/{orgId}` 随详情下发），
   不要在前端按 `orgRole` 推断 —— 自定义角色与部门范围限定都会让前端算错。
3. **当前组织由 URL 单一驱动**，不要用 `useEffect` 把 `?org=` 同步进本地 state
   （与工单中心同一约束：既触发 `react-hooks/set-state-in-effect`，也让深链与页面内点击走两条路径）。
4. **枚举与权限目录来自 `/org-metadata`**，不要在前端硬编码角色名、部门类型、审批场景 ——
   后端新增权限点时前端应当零改动。

Excel 导出走 `fetch` 拿 blob 再触发下载：令牌只在 Authorization 头里，
裸 `<a href>` 直链拿不到身份，后端会 401。

## 风控中心（/risk-control）

八个页签同步到 URL（`?tab=dashboard|assessments|reviews|rules|actions|simulator|devices|ips`）：

| 文件 | 职责 |
|---|---|
| `(console)/risk-control/page.tsx` | 页面骨架、Tab 路由、跨页签的「带着一个实体去看它的记录」 |
| `components/risk/risk-shared.tsx` | 目录上下文 `RiskCatalogProvider`、徽标、配色、格式化 |
| `components/risk/risk-dashboard.tsx` | 大盘：趋势 / 分布 / 直方图 / Top 榜（全部 shadcn chart） |
| `components/risk/risk-condition-form.tsx` | **schema 驱动**的条件参数表单 + 表达式变量速查 |
| `components/risk/risk-rules.tsx` | 规则列表、编辑器、启停、规则详情（效果 + 最近命中 + 趋势） |
| `components/risk/risk-simulator.tsx` | 模拟器：草稿规则试跑 + 环境变量逐项覆写 |
| `components/risk/risk-assessments.tsx` | 评估记录、详情抽屉（判据 / 快照 / 关联 / 重放）、清理 |
| `components/risk/risk-reviews.tsx` | 人工复核队列 |
| `components/risk/risk-entities.tsx` | 设备指纹与 IP 风险库（含详情抽屉与人工处置） |
| `lib/api/risk.ts` / `lib/risk-hooks.ts` | 风控域 API 与 React Query hooks |

四条硬约束：

1. **枚举与参数 schema 一律来自 `/risk/metadata`，不在前端硬编码。**
   场景、等级、动作、条件类型的参数字段都由后端目录下发；
   在组件里另抄一份，会让「后端新增一种条件类型」变成一次静默的漏项 ——
   规则存得进去，但表单上没有它的参数字段，配出来的是一条永不命中的规则。
2. **图表一律用 shadcn `ChartContainer`**，配色走 `ChartConfig` 的 `theme: { light, dark }`
   （`risk-shared.tsx` 里的 `LEVEL_COLORS` / `ACTION_COLORS`）。
   等级色与动作色刻意分色系：同一套色系会让「高危但放行」和「低危但拦截」
   这两种最值得注意的组合在图上看起来一模一样。
3. **分页信封在 `lib/api/risk.ts` 的 `paged()` 里统一转换**。后端返回 `{ list, total }`，
   重构前前端直接当 `{ items }` 用 —— 评估记录 / 待复核 / 设备 / IP 四个页签
   因此无论后端有多少数据都永远显示空列表。
4. **失效关系集中在 `risk-hooks.ts`**。一次复核会连带影响评估记录、待复核队列、
   大盘、IP 库与设备库五张表，散在组件里必然漏掉其中几张。

大盘顶部的**引擎运行态条**不是装饰：「大盘全是 0」有两种截然不同的原因
（真没风险 / 根本没规则在跑），不显式说出来管理员会把后者当成前者。

## 交易中心（/commerce）

五个页签同步到 URL（`?tab=overview|orders|refunds|wallet|vip`）：

| 文件 | 职责 |
|---|---|
| `(console)/commerce/page.tsx` | 页面骨架、应用与时间窗选择、Tab 路由 |
| `components/commerce/commerce-overview-panel.tsx` | 概览：KPI + 趋势 + 三张分布图 + 凭证能力 |
| `components/commerce/commerce-charts.tsx` | 图表外壳 `ChartCard` 与**配色目录**（资金 / 订单状态 / 流水类型 / 分类色轮） |
| `components/commerce/commerce-range-picker.tsx` | 时间窗：预设 + 日历范围，对外只吐 `{start, end}` |
| `components/commerce/wallet-transactions-panel.tsx` | 钱包流水台：筛选、凭证下载与寄送、调账入口 |
| `components/commerce/wallet-adjust-dialog.tsx` | 管理员调账（用户选择器 + 方向开关 + 面额/理由预设） |
| `components/commerce/user-picker.tsx` | 用户选择器（服务端搜索 + 300ms 防抖） |
| `components/commerce/currency-select.tsx` | 币种选择（常用表 + 表外代码直接可用） |
| `components/commerce/receipt-locale-select.tsx` | 凭证语言（页面级，缺字体的语言标注而不灰掉） |
| `components/commerce/receipt-email-dialog.tsx` | 凭证寄送弹窗（订单与流水共用，收件人一键填入） |
| `components/commerce/vip-transactions-panel.tsx` | 会员开通记录 |
| `components/commerce/commerce-format.tsx` | 金额 / 方向 / 类型徽标的**统一口径** |
| `components/payment/payment-orders-panel.tsx` | 订单台（原本写好却没有任何页面挂载） |
| `components/payment/payment-refunds-panel.tsx` | 退款台（同上） |
| `lib/api/commerce.ts` / `lib/commerce-hooks.ts` | 交易域 API 与 React Query hooks |
| `lib/use-debounced-value.ts` | 防抖取值，替代「输入框 + 查询按钮」 |

### 交互约定：别让管理员当输入机

这一节是硬约束，不是建议。凡是能由机器检索、推断或预置的，都不要做成输入框：

| 原本 | 现在 | 为什么 |
|---|---|---|
| 手输用户 ID | `UserPicker` 搜账号 / 昵称 / 邮箱 | 那个数字没人记得住，实际操作是「去用户页搜一遍、复制、切回来粘贴」 |
| 负数表示扣减 | 方向开关 + 恒为正数的金额框 | 少打一个减号就是反向调账，而这类操作不可撤销 |
| 手输金额 | 常用面额一键 + 仍可自由输入 | 客服补偿的金额高度集中 |
| 手输调账理由 | 常用理由 chips + 仍可编辑 | 同一句话不该每次重打 |
| 手输收件邮箱 | 「下单用户」「我自己」一键填入 | 手抄邮箱既慢又会抄错，而抄错等于把交易明细发给别人 |
| 手输 3 位币种代码 | `CurrencySelect`（可搜中文名） | 不该要求管理员背 ISO 4217 |
| 两个 `<input type=date>` | `CommerceRangePicker` | 手打 `2026-03-21` 打错了还没有提示 |
| 输入框 + 「查询」按钮 | 300ms 防抖，即时生效 | 节流是机器的活，不该转嫁给操作者 |

另外两条：**筛选任何一项变化都要回到第 1 页**（否则会停在新条件下不存在的页码上，
表现为「明明有数据却是空的」）；**表格里的账号可点**，点了即按该用户筛选。

### 图表：一律走 shadcn `ChartContainer`

不要手搓进度条、`<div style={{width}}>` 这类"看起来像图表"的东西 ——
它们没有 tooltip、没有图例、不响应主题、也没法和其它页面的图对齐。

- 配色走 `ChartConfig` 的 `theme: { light, dark }`（目录在 `commerce-charts.tsx`），
  由 `ChartContainer` 注入成 `var(--color-<key>)`。**不要在组件里写死十六进制色** ——
  深色模式下要么刺眼要么看不见。
- **进项与出项必须分色系**，否则「收了 10 万」和「退了 10 万」在图上一模一样。
- 趋势图分「收入 / 钱包」两组切换，而不是六条线挤在一张图上：
  六条线的图看起来很全，但没有人能从里面读出结论。
- **实收净额画成虚线**：它是算出来的，不是一笔一笔真实发生的资金。
- 坐标轴金额用紧凑写法（`12.8万`），写全位数会把刻度挤成一团。
- 粒度（日 / 周 / 月）由**服务端**按跨度决定并下发标签，前端不选也不推断 ——
  让前端选粒度的结果是「拉了两年、按天分桶、七百个点」。

五条硬约束：

1. **与 `/apps/{appKey}?tab=payment` 的分工是「运营 vs 配置」**：那边是渠道密钥、限额、
   积分兑换率这类应用配置，这里是**已经发生的**资金记录。同一件事只有一个入口。
2. **五个页签合起来才是完整的资金视图。** 订单只覆盖走支付渠道的钱；余额直购会员、
   业务消费、管理员调账**不产生订单**，只在钱包流水里；退款是反向资金流，
   不看它算出来的实收永远偏高。少看一个页签就会得出错误结论。
3. **金额一律按字符串处理**（后端 `shopspring/decimal`），`formatMoney()` 只做展示格式化。
   转 `number` 会丢分。格式化与类型徽标集中在 `commerce-format.tsx`：各面板各写一份，
   同一笔钱在概览页和流水页就会显示成两个数字，而对账的人无从判断哪个是对的。
4. **币种只标在钱包那一段**。订单金额按渠道计价，可能不止一种货币；
   给它统一贴一个币种符号就是在撒谎。钱包币种来自应用级 `walletCurrency`。
5. **凭证 PDF 走 `fetch` 拿 blob 再触发下载**（`lib/api/commerce.ts` 的 `downloadPdf`）：
   两条管理端下载都是 **POST + Bearer**，裸 `<a href>` 发出的请求不带任何头，后端会 401。

概览刻意用**一个**接口取齐订单 + 钱包 + 趋势 + 凭证能力（`/commerce/overview`）：
这四段数据在页面上是一起呈现的，分开拉会出现「订单已刷新、退款还是上一个时间窗」
这种自相矛盾的画面，而人会照着它做决定。

时间窗与凭证语言提在**页面级**：它们对每个页签都成立，放到各面板里会让人
切页签时反复设置同一件事。各口径的时间列不同（实收按到账、退款按退款成功、
钱包按流水时间），这一点写在日期选择器的脚注里 —— 不写就会有人问「为什么
订单页 30 笔、概览说 28 笔」。

## 内容中心（/content）

三个页签同步到 URL（`?tab=banners|notices|announcements`）：

| 文件 | 职责 |
|---|---|
| `(console)/content/page.tsx` | 页面骨架、应用选择、Tab 路由、总览条 |
| `(console)/content/announcements-panel.tsx` | 系统公告（平台级广播，与应用选择器无关） |
| `components/content/content-shared.tsx` | **枚举目录**（展示位 / 类型 / 级别 / 状态）、投放态推导、格式化、`StatTile` |
| `components/content/banner-panel.tsx` | Banner 台：按展示位分组、拖拽排序、逐行启停 |
| `components/content/banner-preview.tsx` | 投放预览（embla）：只画当前**真正生效**的那几条 |
| `components/content/banner-editor.tsx` | Banner 编辑抽屉（ImageDropzone 上传 + zod 校验） |
| `components/content/notice-panel.tsx` | 公告台：服务端过滤分页、发布 / 归档 / 置顶 |
| `components/content/notice-editor.tsx` | 公告编辑抽屉（tiptap 富文本，存草稿与发布分成两个动作） |
| `lib/api/content.ts` / `lib/content-hooks.ts` | 内容域 API 与 React Query hooks |

六条硬约束：

1. **作用域在页面上必须能一眼分清。** Banner 与应用公告属于上方选中的那个应用；
   系统公告是平台级广播，发给控制台管理员，**不随应用切换**。
   与 `/platform-banners` 的分工同理：那边是画在控制台总览页的平台横幅（限超管），
   这边是画在应用客户端里的素材。
2. **枚举目录是单一事实源**（`content-shared.tsx`），与后端
   `internal/domain/app/types.go` 的白名单一一对应。在面板里各抄一份，会同时招来
   「后端加了一档、控制台选不出来」和「控制台能选、保存时报不支持」两种漂移，
   而两边都没有报错提示。
3. **投放态是推导出来的结论，不是字段罗列**（`resolveSchedule`）。
   「已启用」回答不了「用户现在看不看得到」—— 一条启用但结束时间已过的 Banner，
   开关是开的，客户端上什么都没有。界面必须直接说结论，否则管理员要拿三个字段
   和当前时间做心算。
4. **Banner 列表不分页，公告列表分页。** 形状不同不是遗漏：Banner 要拖拽排序，
   分页会让第 2 页的第 1 条拖不到第 1 页去；公告会持续累积，运营两年有几千条。
5. **拖拽提交的是完整顺序，且要把其它展示位一并带上。** 后端按数组下标重写
   `position`，只提交当前展示位会让没被提交的那些保留旧值并与新值撞车。
6. **图片存 `reference` 不存预览 URL。** 上传返回 `{reference, url}`：前者是
   `storage://{configId}/{objectKey}`，后者是带票据的临时地址。把预览地址存进库里
   过两天就是死链。`<img src>` 一律用后端解析出的 `headerDisplayUrl`。

点击率在曝光为 0 时显示「—」而不是 0%：「没人看过」和「看过但没人点」是两回事，
只有后者说明素材有问题。点击数依赖客户端调 `POST /api/v1/apps/{appKey}/banners/{id}/click`
上报，官方 SDK 是 `content.reportBannerClick()`。

## 工单中心（/tickets）

一屏承载服务台与通知出口，Tab 同步到 URL（`?tab=inbox|mine|analytics|settings|notify`）：

| 文件 | 职责 |
|---|---|
| `(console)/tickets/page.tsx` | 页面骨架、指标卡、工单台列表与筛选、新建工单 |
| `components/tickets/ticket-detail-sheet.tsx` | 详情抽屉：会话 / 时间线 / 属性 / 底部操作条 |
| `components/tickets/ticket-settings-panel.tsx` | 分类 / 处理组（含成员授权）/ SLA / 快捷回复 |
| `components/tickets/notify-center-panel.tsx` | 渠道 / 事件订阅 / 模板 / 投递记录 |
| `components/tickets/ticket-shared.tsx` | 状态与优先级徽标、相对时间 / 时长 / 到期格式化 |
| `lib/ticket-hooks.ts` | 工单 + 通知出口的全部 React Query hooks |

三条硬约束：

1. **按钮显隐一律读后端返回的 `permissions`（ActionSet），不要在前端按角色推断**。
   「组成员能回复自己名下的工单但不能删除」这类规则只在服务端实现一次，
   前端自己算必然与后端漂移，用户会遇到"点了才 403"。
2. **详情打开状态由 URL 单一驱动**（`?id=`），不要用 `useEffect` 把 query 同步进本地 state
   —— 那既触发 `react-hooks/set-state-in-effect`，也会让飞书卡片深链与页面内点击走两条路径。
3. **导航项不挂 `permission`**：工单可见范围由后端按「权限点 + 应用作用域 + 处理组归属」推导，
   仅仅是处理组成员的管理员没有任何 `ticket:*` 权限点，但必须能看到入口。

渠道配置表单由 `/api/admin/notify/catalog` 返回的元数据动态渲染，
后端新增一种 IM 时前端零改动 —— 不要把渠道字段硬编码回组件里。

## 应用用户详情（/users/app-users/[appKey]/[userId]）

一屏承载对单个应用用户的**全部**管理动作。页签同步到 `?tab=`，`?from=` 原样保留：

| 文件 | 职责 |
|---|---|
| `components/users/detail/app-user-detail-page.tsx` | 页面骨架：身份栏 / 信号带 / 页签路由 |
| `components/users/detail/user-detail-shared.tsx` | 格式化、`Facts`/`Fact`/`StatTile`/`Panel` 原语、**信号推导** |
| `components/users/detail/user-overview-tab.tsx` | 概览：关键指标 + 账户档案 + 注册来源 + 登录方式 |
| `components/users/detail/user-profile-tab.tsx` | 资料编辑（仅昵称/邮箱）+ 只读明细 + 用户端设置 |
| `components/users/detail/user-security-tab.tsx` | 凭据矩阵 / 密码生命周期 / 2FA / Passkey / 登录绑定基线 |
| `components/users/detail/user-assets-tab.tsx` | 积分经验 / 钱包与流水 / 会员与赠送 |
| `components/users/detail/user-activity-tab.tsx` | 活动地图 / 活跃会话 / 登录记录 / 会话事件 / 风控评估 |
| `components/users/detail/user-activity-map.tsx` | 活动地图（deck.gl ⋈ MapLibre）：活动点 / 密度热力 / 位移轨迹 |
| `lib/geo/private-network.ts` | 内网 / 回环 / CGNAT 识别（"服务器地址"的判据） |
| `lib/geo/server-location.ts` | 服务端端点位置，与攻击飞线图共用一份 |
| `components/users/detail/user-governance-tab.tsx` | 账号开关 / 新建封禁 / 封禁历史与撤销 / 删除 |
| `lib/api/app-users.ts` | 单用户维度 API（封禁 / 钱包 / 单用户审计） |
| `lib/app-user-hooks.ts` | 上述 hooks + 成组失效 `USER_SCOPE_KEYS` |

五条硬约束：

1. **信号带是推导出来的结论，不是字段罗列。** `deriveUserSignals()` 把「有无生效封禁 /
   账号是否被限制 / 有没有可用登录方式 / 密码过没过期 / 恢复码剩几个」合成一条警示带。
   收录标准两条缺一不可：**可行动**（说清是什么、为什么、去哪处理）且**罕见**。
   只有 danger / warning 两档，刻意没有 info —— 一条对大多数用户都会亮的信号
   （如「会员已过期」）会把整条带子训练成背景噪音，那时真正的封禁提示也一起被跳过。
   这类信息放它本来该在的页签里。
2. **账号开关与封禁记录是两件事，界面上必须分开。**
   `users.enabled` 是一个布尔位（无起止、无操作人、无历史）；`app_user_bans` 才是
   有类型 / 范围 / 起止 / 操作人 / 证据、可撤销、可申诉的处置记录。
   旧版详情页只有前者，于是所有需要留痕的处置都只能在"原因"里写一句话。
3. **金额一律按字符串处理**（后端 `shopspring/decimal`）。`formatMoney()` 只做展示格式化，
   前端不做任何算术 —— 转 `number` 会丢精度。
4. **`GET /users/:userId/wallet` 从不返回 404**：没有钱包行时后端拼一个零值钱包返回
   （刻意不为只读请求建行）。因此界面分辨不出"没开户"与"余额为 0"，
   唯一旁证是 `createdAt` 为 Go 零值时间（`isZeroTime()`）。不要假装能分辨。
5. **单用户审计走 `/users/:userId/audits/{login,sessions}`**，不要用应用级
   `/apps/:appkey/audits/login?keyword=账号` 拼。keyword 同时匹配账号 / 昵称 / IP /
   deviceId / UA / provider，拼出来的结果会混进他人记录，分页总数也不是这个人的。
6. **内网地址不许伪造坐标。** GeoIP 对 127/8、RFC1918、fe80::/10 这些地址没有任何结论，
   旧版按 IP 求和取模把它们散布在中国上空 —— 看起来像真数据，其实全是噪音，
   还会在轨迹里凭空造出「不可能位移」。现在统一收敛到一个「服务器地址」节点
   （`lib/geo/private-network.ts` 判定，位置取 `lib/geo/server-location.ts`，
   与 `/security` 的攻击飞线图共用同一份配置），并在图下明写有多少次被这样归并。
   CGNAT（100.64/10）刻意不算内网：那是运营商级 NAT，归进机房会把真实的移动用户画错地方。

字段展示用 `<Facts>` 密集行表，**不要退回"每个字段一张 `rounded-2xl border` 卡片"** ——
旧版四十个字段就是四十张视觉权重相同的卡，"账号"和"自定义 ID 次数"长得一模一样。
需要强调靠 `<StatTile>` / 徽标 / 信号带，不是靠给每个字段加边框。

活动地图取的是**不带状态筛选**的登录样本（上限 100 条），与页面上那份「登录记录」分开查：
共用一份的话，选了「仅失败」整张图就只剩失败点，看图的人会得出"这个人只在国外登录失败过"
这种结论。会话那份则复用「活跃会话」面板的同一个 query key，不会多打一次请求。

## 通知铃铛与实时事件

顶栏铃铛（`components/layout/notification-bell.tsx`）合并两个来源：

| 标签 | 数据源 | 已读状态存哪 |
|---|---|---|
| 通知 | `/api/admin/notifications`（服务端 `admin_notifications`） | 服务端 |
| 公告 | `useNotificationStore`（平台广播） | 浏览器 localStorage |

**角标实时性靠 WebSocket，不靠轮询。** 后端每写入一条管理员通知就推
`admin.notification.created`（载荷带算好的 `unread`），`realtime-provider.tsx`
收到后直接 `setQueryData` 写缓存。`inbox-hooks.ts` 里 60s 的 `refetchInterval`
只是**断线兜底**，不要为了"更实时"把它调短 —— 那等于退回轮询。

工单实时事件统一以 `ticket.` 前缀分发，载荷带 `audience` 字段区分受众：
前端只处理 `audience === "admin"` 的那一份（同一事件也会推给提单人）。
收到即失效相关查询缓存；只有 `level === "critical"` 才弹 toast 打断操作。

## Monaco 编辑器（JsonViewer / 远程函数脚本 / 插件 Expr）

Monaco 产物**自托管**在 `public/monaco/vs`，由 `scripts/sync-monaco-assets.mjs`
从 `node_modules/monaco-editor` 同步（`predev` / `prebuild` 自动执行，也可 `pnpm monaco:sync`）。
以下三条都是踩过坑之后的硬约束，改动前务必读完：

1. **加载入口只有 `src/app/layout.tsx` `<head>` 里的那段内联引导脚本**
   （注入本地 `loader.js` → 建好 `window.require` → 把 `vs/editor/editor.main` 的 Promise
   挂到 `window.__aegisMonaco`）。`@monaco-editor/loader` 会在自己的 `init()` 里注入公网 CDN
   的 loader.js；AMD 一旦缓存了 `editor.main`，之后再改 `paths` 完全无效，
   重复注入还会抛 `_amdLoaderGlobal has already been declared`。
   **不要改回 `next/script` 的 `beforeInteractive`**：app router 下它只渲染一段
   `(self.__next_s=…).push([src])`，真正的 `<script src>` 由 Next 运行时 createElement
   追加到 body —— 动态插入的外链脚本默认 async，紧随其后的内联引导会先执行，
   那时 `window.require` 还不存在，引导直接空转（表现为「引导脚本在，但永远走
   `loader.ts` 的兜底路径」）；且两段 `<script>` 作为 `<html>` 的直接子节点是非法 HTML，
   React 19 会报 `In HTML, <script> cannot be a child of <html>` 并水合失败。
2. **不要配置 `"vs/nls"` 汉化**。monaco 0.56 的 `nls/lang/*.js` 是普通脚本
   （只给 `globalThis._VSCODE_NLS_MESSAGES` 赋值，没有 `define()`），
   而 `nls.messages-loader` 把它当 AMD 模块 require → 永远挂起 → `editor.main` 不 resolve
   → `monaco.languages.typescript` 不存在 → 脚本编辑器的补全与诊断整体失效。
3. **组件必须等 `loadMonaco()`（`src/lib/monaco/loader.ts`）resolve 后再渲染 `<Editor>`**，
   且**不要在模块作用域预热**：`RoutePrefetcher` 会空闲预取全部侧边栏路由并执行其模块体，
   模块级预热会让任意页面（包括登录页）都拉起 Monaco。

> 历史坑：旧代码里手写的 CDN 常量与 `@monaco-editor/loader` 的内置默认值
> (`cdn.jsdelivr.net/npm/monaco-editor@0.55.1/min/vs`) 一字不差，
> 所以「配置从未生效」这件事从表面完全看不出来。排查时请以
> `performance.getEntriesByType('resource')` 里 `/monaco/vs` 的请求数为准。

脚本编辑器（`components/functions/script-editor.tsx`）额外把
`lib/monaco/aegis-sdk-types.ts` 按函数已声明的 capabilities 生成的 `.d.ts`
注入 TS 语言服务：补全里出现什么，运行时就绑定了什么；
`lib` 只给 `es2020`（不含 DOM），于是 `document` / `setTimeout` 会如实标红，与 goja 沙箱一致。

## 工作流画布

`(console)/workflows/` 目录含完整的节点编辑器实现：
- `workflow-canvas.tsx` — @xyflow/react 画布
- `workflow-studio.tsx` — 编辑器主面板
- `workflow-dialogs.tsx` — 节点配置弹窗
- `workflow-helpers.ts` — dagre 自动布局工具函数
