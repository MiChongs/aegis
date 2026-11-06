# aegis-console — 管理前端

> 面包屑：[Aegis](../CLAUDE.md) › aegis-console

## 技术栈

| 技术 | 版本 |
|---|---|
| Next.js | ^16.3.0 |
| React | 19.2.8 |
| TypeScript | 7.0.2（编译器）+ 6.0.2（工具链 API，见下） |
| Tailwind CSS | ^4.3.3 |
| shadcn/ui | new-york-v4，41 个官方组件均为最新版（2026-11-06 补 `empty`，概览页空状态用）（见下方扩展说明） |
| radix-ui | ^1.6.7（**统一包**，已取代 32 个 `@radix-ui/react-*` 独立包） |
| TanStack React Query | ^5.101.4 |
| TanStack React Table | ^9.1.2（表格，v9 按需注册功能） |
| Zustand | ^5.0.14 |
| zundo | ^2.3.0（zustand 的撤销 / 重做中间件，物品发放画布用） |
| next-themes | ^0.4.6 |
| Zod | ^4.4.3（表单校验） |
| @xyflow/react | ^12.11.2（工作流画布、物品发放规则画布） |
| react-resizable-panels | ^4.12.2（可拖拽分栏，远程函数工作台用；**v4 是新 API**，见下） |
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
| screenfull | ^6.0.2（顶栏全屏开关；跨浏览器前缀差异交给它，见下方顶栏一节） |
| auto-skeleton-react | ^1.0.5（自动骨架屏，按子树真实 DOM 量出骨架；**只经 `components/ui/auto-skeleton.tsx` 使用**，见下方骨架屏一节） |
| ESLint | ^9.39.5 |
| bun | 1.4.1（包管理与脚本运行；运行时仍是 Node，见下方「包管理：bun」） |

### shadcn/ui 组件与项目扩展

组件源码由 CLI 复制进 `src/components/ui/`，**再次执行 `shadcn add --overwrite` 会覆盖下列扩展**，
升级后必须逐项补回（本文件即为该清单）：

| 组件 | 项目扩展 | 为什么不能丢 |
|---|---|---|
| `badge.tsx` | `success` / `warning` / `danger` / `info` 语义色 + `size`(sm/default/lg) | 官方无这些变体，183 处业务调用中 16 处依赖 `danger`/`success` |
| `avatar.tsx` | 图片 Blob LRU 缓存（200 条）、`evictAvatarCache()`、`preview` 点击预览 | 属业务功能：`admin-hooks.ts` 导入 `evictAvatarCache`，`console-shell` 用 `preview` |
| `input.tsx` | `suppressHydrationWarning` | 抑制浏览器扩展注入属性导致的 React 19 水合告警 |
| `breadcrumb` / `dialog` / `sheet` | `sr-only` 文案中文化（"更多" / "关闭"） | 无障碍文案本地化 |
| `resizable.tsx` | 按 **react-resizable-panels v4** 手写（`Group` / `Panel` / `Separator` / `orientation` / `defaultLayout`） | 官方注册表那份仍是 v2/v3 的 `PanelGroup` / `PanelResizeHandle` / `direction` / `autoSaveId`，装进来会直接报「导出不存在」 |

`src/components/ui/` 下另有 13 个**项目自有组件**（非 shadcn，CLI 不会碰）：
`auto-skeleton`、`brand-icon`、`country-flag`、`data-state`、`error-boundary`、`image-dropzone`、`image-lightbox`、
`json-viewer`、`rich-editor`、`section-heading`、`surface-card`、`toast-detail`、`virtual-list`。

补装组件用 `bunx shadcn@latest add <name>`，**永远不要带 `--overwrite`** ——
上表四项扩展会被一次性抹掉，而覆盖是静默的。CLI 对已存在且内容一致的文件会自己跳过。

> `ui/` 里不留没有任何页面用到的组件。官方注册表有 63 个 UI 项，装进来的判据是
> "现在就有地方用"，而不是"以后可能用得上" —— 后者的结果是没人知道哪些还活着。

> **统一 radix-ui 包**：新版组件一律 `import { Dialog as DialogPrimitive } from "radix-ui"`，
> 不再使用 `@radix-ui/react-*`。新增代码请沿用统一包写法，否则会重新引入已删除的依赖。

### TypeScript 7 / 6 并存（side-by-side）

TypeScript 7 是 Go 重写的编译器（tsgo），**不再提供 JS 版的 compiler API**，而 `typescript-eslint@8.66.0`
仍需要该 API，直接装 TS 7 会让它抛错、`bun run lint` 完全不可用
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
| `bun run typecheck`（`tsc --noEmit`） | **TS 7.0.2** | 权威类型校验，`bun run build` 的第一步就是它 |
| `bun run typecheck:ts6`（`tsc6 --noEmit`） | TS 6.0.2 | 对照校验 |
| `require("typescript")`（typescript-eslint） | TS 6.0.2 | 两个包的 bin 名不冲突（`tsc` / `tsc6`） |
| `next build` 期类型检查 | **不做**（`typescript.ignoreBuildErrors`） | 见下 |

**build 期不再做类型检查**：Next 内建的那一遍走 TS 6 的 JS API，而 TS 7 是 Go 重写的
原生编译器，同一份代码实测差接近一个数量级（当时是 17s vs 2.4s）—— 两者查的是同一件事，
留慢的那个没有意义。因此 `next.config.ts` 里 `typescript.ignoreBuildErrors: true`，
把这道关口前移成 `build` 脚本的第一步 `tsc --noEmit`：类型不过一样构建不出来，
而且报错来得更早（不用等 Turbopack 编译完）。**改 `build` 脚本时不要把它去掉** ——
`console-ci.yml` 里那一步 `bun run typecheck` 是**故意**与它重复的（闸门不该依赖 build
脚本怎么写），但本地开发只跑 `bun run build` 的人就此失去全部类型检查。

**`next.config.ts` 中的 `experimental.useTypeScriptCli: false` 同样不可删除**：Next 16.3 该项默认为 `true`，
CLI 模式会去找 `typescript/bin/tsc`，而 TS 6 兼容包只提供 `bin/tsc6`，Next 会误判「typescript 未安装」
并自动用当前包管理器装一份 `typescript`，把上面的 alias 覆盖掉。
`ignoreBuildErrors` 只是不做检查，Next 仍会探测这个包，所以两项要同时在。

> 待 typescript-eslint 支持 TS ≥7.1 后，可移除 `@typescript/native` 别名、把 `typescript` 直接指向 7.x，
> 并删除 `useTypeScriptCli: false` 与 `typecheck:ts6` 脚本。

### 版本上限说明

- **ESLint 停在 9.39.5**：ESLint 10 已发布，但 `eslint-config-next` 依赖的
  `eslint-plugin-react`(≤7.37.5)、`eslint-plugin-jsx-a11y`(6.10.2)、`eslint-plugin-import`(2.32.0)
  peer 上限均为 `^9`，实测 ESLint 10 下 lint 直接崩溃（`scopeManager.addGlobals is not a function`）。

> 原先本目录有一份 `pnpm-workspace.yaml`，用来挡住上级 `userSystem/` 那个旧 Node.js 系统的
> pnpm workspace 根。那个根已经不在了，迁到 bun 时这份文件随 pnpm 一起删除。

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
│   ├── brand/                  # 公开站点：AegisMark、PublicHeader、SiteFooter
│   │   └── home/               # 首页各分区 + home-content.ts（文案单一事实源）
│   ├── dashboard/              # MetricCard、ActivityFeed
│   ├── developers/             # PortalShell、CodeBlock、CodeSamples（门户 + 接入页共用）
│   ├── functions/              # 远程函数工作台（见下方「远程函数」一节）
│   ├── layout/                 # ConsoleShell（外壳装配：侧边栏 + 顶栏 + 快捷键）
│   │   ├── sidebar/            # 侧边栏各部件（导航树 / 折叠轨道 / 命令面板 / 宽度手柄）
│   │   └── topbar/             # 顶栏各部件（面包屑 / 动作区 / 通知铃铛 / 账户菜单 / 移动端抽屉）
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

## 公开站点（/ 与 /status）

免登录的品牌门面，与控制台共用同一套语义令牌，因此深浅两色都成立。

| 文件 | 职责 |
|---|---|
| `brand/brand-home.tsx` | 首页装配：九个分区的顺序即阅读顺序 |
| `brand/home/home-content.ts` | **文案与数据的单一事实源**（首屏 / 数字 / 能力 / 架构 / 接入 / FAQ / 赞助商 / 页脚） |
| `brand/home/section.tsx` | 容器宽度、标题组 `SectionHeading`、入场动效 `Reveal` |
| `brand/home/visuals.tsx` | 视觉原语：底纹 `Pattern`、光晕 `AuroraOrbs`、聚光卡 `SpotlightCard`、跑马灯 `Marquee`、数字滚动 `CountUp` |
| `brand/home/feature-visuals.tsx` | 六张能力卡各自的配图（两张走 recharts，四张纯 CSS） |
| `brand/home/*-section.tsx` | 各分区，只排版不写文案 |
| `brand/home/sponsors-section.tsx` | 赞助商跑马灯（`kibo-ui/marquee`，两行反向 + 渐变边缘） |
| `brand/sponsors/brand-logo.tsx` | 品牌标识渲染：图形标 + 字标，尺寸由字号决定，颜色走 `currentColor` |
| `brand/sponsors/brand-logos.generated.ts` | **生成产物**，由 `bun run logos:sync` 从 `@lobehub/icons-static-svg` 抽出 |
| `brand/public-header.tsx` | 公开顶栏（首页与状态页共用），`NavigationMenu` + 主题开关 + 移动端抽屉 |
| `brand/site-footer.tsx` | 公开页脚（三栏导航 + 版权），法律条款入口在这里 |
| `brand/public-entry-actions.tsx` | 「进控制台 / 次操作」一对入口 |

五条硬约束：

1. **一个颜色都不许写死。** 首页曾是 `bg-[#060a12] text-white` 加满屏 `white/6`，
   于是它成了全站唯一不认主题的页面 —— 浅色模式下从控制台点回首页，
   画面会毫无预告地黑掉一屏。现在全部走 `background` / `card` / `muted-foreground`
   / `border` 这些语义令牌，改调色板不需要回来改首页。
2. **文案不写在组件里。** 分区组件只排版，字全在 `home-content.ts`。
   改一句话不必在八个 tsx 里找它在哪。撰写口径是**产品文档的中性书面语**：
   不用第一人称口吻、反问句与旁白（「先进去看一眼」「不是…而是…」都不要），
   也**不要用破折号 `——`**，该断句就断句，该用冒号就用冒号。
3. **内容动效只有一种**：进入视口时淡入上移一次（`Reveal`），`prefers-reduced-motion`
   下直接不放。旧版为了播两段"卡片浮入"要了 **750vh** 的 sticky 滚动距离换四屏内容，
   用户以为在往下翻，实际什么都没翻到。装饰动效（光晕漂移 / 跑马灯 / 描边流光 /
   请求光点）走 CSS keyframes，**一律只做 transform 与 opacity**，
   并在 `globals.css` 末尾由一条 `prefers-reduced-motion` 规则统一停掉。
4. **数字必须能核对。** 「1000+ 接口」「16 支付渠道」「9 邮件服务商」都数得出来，
   每个下面还写了它是怎么来的。核不了的数字读者只能选择信或不信，两种反应都没用。
5. **顶栏里的分区锚点写绝对路径**（`/#features`）。这个顶栏也挂在 `/status` 上，
   裸 `#features` 在那里指向一个不存在的锚点。

`ButtonGroup` 只用在**同一件事的两个去处**（接入分区的「文档 / 接口」），
不要拿它摆首屏那对 CTA —— 它会把两个按钮粘成一个分段控件并吃掉次按钮的左边框，
而那两个是并列的两件事，该用间距分开。

### 首页的视觉层（`--home-*`）

控制台通体 zinc 单色是对的，那是**长时间盯着看**的界面该有的克制；
但同一套克制搬到门面上就只剩朴素。所以首页额外有一层强调色、光晕与底纹，
令牌与 keyframes 全部定义在 `globals.css` 末尾的 `--home-*` 命名空间下。

**配色刻意避开蓝紫。** 「青蓝 → 紫渐变字 + 大团发光球 + 玻璃拟态」是这两年
生成式配色的三件套，它出现在哪一页，哪一页就立刻显得像是随手生成的。
这三样在首页一件都没有：

| 令牌 | 取值 | 用途 |
|---|---|---|
| `--home-accent` / `--home-accent-soft` | 暖铜（浅 `#b45309` / 深 `#f0a339`） | 唯一强调色，覆盖率约 5% |
| `--home-accent-alt` | 中性石灰 | 图表第二序列。**不给第二种色相**，墨色加一个暖色永远比蓝配紫耐看 |
| `--home-accent-warm` | 暗酒红 | 出项 / 告警，与暖铜同色系但明显更沉，一眼分得出进项出项 |
| `--home-spotlight` / `--home-beam` | 由强调色 `color-mix` 派生 | 鼠标聚光、收尾 CTA 的描边流光 |
| `--home-vignette` | 中性 | 暗角，**取代**了原来的发光球 |

| 原语（`home/visuals.tsx`） | 用在哪 |
|---|---|
| `Pattern` | 网格 / 点阵底纹，靠 mask 在边缘溶解 |
| `Vignette` | 中性暗角，把视线压回版心 |
| `SpotlightCard` | 鼠标跟随高光（能力卡、架构卡、全景条目） |
| `Marquee` | 首屏底部的技术栈跑马灯（与赞助商那套是两回事，见下节） |
| `MaskLine` / `Typewriter` | 文字动画：遮罩逐行揭示 / 打字机 |
| `CountUp` | 数字带进入视口时从 0 滚上来 |

五条约束：

1. **深浅两套各给一份。** 浅色下强调色要压暗才不刺眼，深色下要提亮才看得见，
   只写一份的话总有一个方向是错的。
2. **图表颜色走 `ChartConfig` 的 `color: "var(--home-accent)"`**，不写十六进制。
   CSS 变量在使用点解析，一份配置同时管住深浅两色。
3. **文字动画的核心是遮罩，不是位移。** `MaskLine` 外层 `overflow-hidden`、
   内层从 `y: 108%` 升上来，看起来是被"印"出来的；淡入加位移只是元素在飘。
   `Typewriter` 用 `clipPath` 沿宽度展开而不是逐字 `setState`，
   代价是只能用等宽字体（否则裁切点会落在字形中间），这也是它只用在 mono 标签上的原因。
4. **鼠标跟随与数字滚动不许在动画帧里 `setState`。** 前者用 `useMotionTemplate`
   把指针位置直接喂给 style，后者用 `useMotionValueEvent` 写 `textContent`，两者都零 re-render。
5. **`CountUp` 的静态值必须留在 SSR 输出里**（滚动只是覆盖 `textContent`），
   否则没有 JS 或 reduce 档下，页面上会是一排 0。

### 赞助商跑马灯

第 3 条（动效只有一种）在这里有唯一一处例外：跑马灯本身就是这个分区的形态。
它同样受 `prefers-reduced-motion` 约束 —— 命中时 `play={false}` 直接停住，
名单仍然完整可读。

| 部件 | 来源 |
|---|---|
| 跑马灯 | shadcn 注册表 `@kibo-ui/marquee`（底层 `react-fast-marquee`，`autoFill` 自动按容器宽度补足份数），三行逐行反向、行速互不相同 |
| 边缘处理 | `MarqueeFade` + 单层 arbitrary `mask-image` |
| 品牌标识 | `@lobehub/icons-static-svg`（零依赖 SVG 资源包）经 `bun run logos:sync` 抽成内联标记 |
| 单条目 | shadcn `Item` + `Tooltip`（品牌名 + 领域，已内置适配的另说明它在 Aegis 里承担什么） |

五条约束，每一条都是踩过的坑：

1. **字标是品牌自有字体的轮廓，不是排出来的文字。** 用 `font-semibold` 打一行
   "Cloudflare" 单看像回事，二十几个 logo 摆在一起就会发现只有它是 Geist。
   图形标彩色、字标单色：整排全上色会让二十几套配色互相打架。
2. **不要用 Tailwind 的 `mask-r-from-*` / `mask-x-from-*` 做这个边缘。**
   它们会展开成六层 `mask-image` 再用 `mask-composite: intersect` 合成，
   在 Chromium 上合成结果恒为全透明，整个边缘层直接消失。这件事**不报错**，
   计算样式看上去也完全正常（宽度、底色、mask 渐变全对），只是画面上什么都没有。
   写成单层的 `[mask-image:linear-gradient(...)]` 就没有合成这一步。
3. **带子用 `bg-card`，不能用 `bg-background`。** 浅色档里 `--muted` 与 `--background`
   是同一个色值（都是 `#f4f4f5`），分区底色 `muted/30` 叠上去还是它自己，
   `background` 的带子在浅色模式下完全看不见。`--card`(`#ffffff`) 是浅色档里唯一
   与之有对比的中性面，深色档里它也比 `background` 亮一档。
   `MarqueeFade` 的底色必须与带子**完全**一致，因此它也是 `bg-card`。
4. **每行要占位高度。** `react-fast-marquee` 挂载前返回 `null`（它靠测量容器宽度
   决定复制几份），不占位的话页面会在水合那一刻整体上跳。`ROW_HEIGHT` 同时钉住
   跑马灯容器与条目，两者必须一致。
   同一个原因：**这一排不进服务端 HTML**，水合后才出现。
5. **彩色版主体是白色的品牌要退回单色版**（生成器里的 `mono: true`）。
   Kimi 的彩色标是白色字形加一个小蓝点，放在 `card` 色的带子上只剩那个点，
   看起来像图裂了。单色版走 `currentColor`，两种主题都成立。
   本身就没有彩色版的（Vercel / GitHub / Anthropic / OpenAI / Cursor / Notion）同理。

`HomeSponsor.slug` 的取值由生成产物约束，拼错过不了类型检查。
不要把 `@lobehub/icons`（React 版）装进来：它 peer 依赖 `antd` 与 `@lobehub/ui`，
为了几个 logo 会把 antd 全家桶拖进这个项目。静态 SVG 包是零依赖的。

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
bun install           # 装依赖（按 bun.lock；CI 与镜像里是 bun install --frozen-lockfile）
bun run dev           # 开发服务器（自动清理上次产物，保留编译缓存）
bun run build         # 生产构建 = tsc --noEmit && next build
bun run start         # 生产启动
bun run typecheck     # tsc --noEmit
bun run lint          # ESLint
bun run test          # node --test（底图供应商目录与坐标基准）
bun run clean         # 清理 .next，连编译缓存一起（怀疑缓存坏了时用这个）
```

### 包管理：bun（运行时仍是 Node）

bun 只负责**装依赖和跑脚本**。`next`、`tsc`、`eslint` 的 bin 都以 node 为解释器，
`bun run` 遵从这个 shebang，所以 dev / build / start 实际仍跑在 Node 上，镜像的运行阶段
也是 `node server.js`。不要给脚本加 `--bun` 让 Next 跑在 bun 运行时上，那是另一件事。

| 文件 | 管什么 |
|---|---|
| `package.json` 的 `packageManager` | bun 版本的**唯一事实源**：CI（setup-bun）与镜像（npm 装）都读它 |
| `bun.lock` | 文本锁文件，要提交。由 `pnpm-lock.yaml` 迁移而来，解析出的 1051 个包版本逐一相同 |
| `bunfig.toml` | 平铺式 `node_modules`（`linker = "hoisted"`）+ 新版本冷静期（`minimumReleaseAge`，一天） |
| `package.json` 的 `trustedDependencies` | 允许跑 postinstall 的依赖。bun 默认只信任自带白名单，其余的安装脚本**静默跳过** |

四条容易踩的：

1. **`bun run test`，不是 `bun test`。** 后者是 bun 自带的测试运行器，会绕开 `test` 脚本、
   用 bun 的语义去跑 `*.test.mjs`；这里的测试写的是 `node:test`。
2. **新增带安装脚本的依赖要看一眼 `bun pm untrusted`。** 被跳过不会报错，
   要放行就加进 `trustedDependencies`（现在只有 `unrs-resolver`，eslint-config-next 的原生解析器）。
3. **补装 shadcn 组件用 `bunx shadcn@latest add <name>`**，同样永远不带 `--overwrite`。
4. **Zeabur 的 zbpack 不读 `packageManager` 里的版本号**：它从这个字段认出 bun，
   但装的是 `bun@latest`，再跑 `bun install`（不带 `--frozen-lockfile`）。版本以 CI 与镜像为准。

### 构建缓存：`.next/cache` 不要删

`predev` / `prebuild` 会清掉上一次的产物，但 `scripts/clean-next.mjs` **刻意跳过 `.next/cache`** ——
那里是 Turbopack 的文件系统缓存（Next 16 起默认开启，本项目约 280MB）。
连它一起删的代价实测是编译阶段 **2.7s → 10.8s**，而清理本来只是想要一份干净的产物，
残留旧内容的是 `server` / `static` / `dev` 那几个目录，缓存自己带版本标记、Next 会判失效。

`bun run clean` 走 `--all`，连缓存一起删：手动清理的场景恰恰是怀疑缓存坏了。

`next build` 会把各阶段耗时逐行打出来（编译 / 静态生成），`tsc` 那一步单独计时即可。
构建变慢时先看是哪一段涨的，再决定查什么 —— 缓存被清掉表现为编译阶段成倍变长。

### `turbopack.root` 只能指本目录

`package.json`、`bun.lock` 与 `bunfig.toml` 都在 `aegis-console/` 下，这里就是依赖树的根，
所以 `next.config.ts` 里的 root 就是 `import.meta.dirname`。曾经写成 `../../`（**仓库之外**的 `userSystem/`），
在容器里那个相对路径会算成 `/` —— 等于告诉 Turbopack「整个文件系统都是项目」。

## 容器镜像（deploy/docker/console.Dockerfile）

```bash
docker build -f deploy/docker/console.Dockerfile \
  --build-arg AEGIS_API_BACKEND=http://aegis-server:8088 \
  -t aegis-console aegis-console
```

**构建上下文是 `aegis-console/`，而文件放在 `deploy/docker/` 下**，两件事都是刻意的：
前者与 Zeabur 上该服务的 RootDirectory 一致；后者是因为 zbpack 见到构建根下有
`Dockerfile` 就会从「Next.js 自动识别」切到「docker 计划」，而那种切换在 Dashboard 上
看不出来，一旦发生又没人传 build arg，就是下面第 1 条的事故。

两条硬约束：

1. **`AEGIS_API_BACKEND` 是构建期烘死的，不是运行期读的。** `rewrites()` 在 build 时
   求值，结果序列化进 `.next/routes-manifest.json`。所以换后端地址必须重新构建镜像，
   改容器环境变量没有任何作用；而漏传 build arg 会落到默认的 `127.0.0.1:8088` ——
   控制台反代到它自己。它还必须填**内网**地址，理由见下一节。
2. **`.next/static` 与 `public/` 要自己搬进运行阶段**，standalone 产物不含它们。
   `public/` 里有自托管的 monaco（23MB），漏搬的表现是脚本编辑器打不开。

`output: "standalone"` 由 `NEXT_OUTPUT=standalone` 开启，平时本地 build 不开 ——
它要对整棵依赖树做文件追踪，而那份产物本地用不上。

## 同源反代

`/api/*`、`/openapi.json`、`/healthz`、`/readyz` 由 `next.config.ts` 的 rewrites
同源反代到 `AEGIS_API_BACKEND`。浏览器只看到相对路径，后端 host 不外露。

代价是链路上多了一跳，而 **Next 内置的那个代理不追加转发头**
（`proxy-request.js` 只手工塞了 `x-forwarded-host`，httpxy 的 `xfwd` 没开），
控制台自己也不补。后端看到的直连对端是控制台，转发链原样是入站请求带来的那条：
前面有入口反代时就是入口写的 XFF；没有入口反代时（本机开发 / 自建单机）
链要么为空、退回控制台地址，要么是客户端自己写的。

**`AEGIS_API_BACKEND` 必须指向后端的内网地址。**
填公网域名的话，这一跳绕出公网再回来，途中的 CDN / 网关会如实写下「连接方是控制台」，
而那是个公网地址、后端不信任它，判定就正确地停在那里 —— 全站每个用户的每个请求
都收敛成控制台的出口地址。放宽后端 `TRUSTED_PROXIES` 不是解法
（等于把伪造权发给任何能直连后端的人）。这个错误在功能上完全看不出来。
完整说明见 [docs/client-ip.md](../docs/client-ip.md#控制台反代这一跳)。

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
- **文案极简**：控制台是工具，界面上只放标签、标题、按钮、列名、数值与短状态词。
  不写卡片说明段、字段提示、「否则 / 因为」式的理由；确认弹窗一句话说后果；
  空状态只写「暂无 X」；反映真实状态的提示（未配置 / 已冻结）保留为一行短句。
  取舍与理由写在代码注释和本文件里，不写在界面上。公开首页与开发者门户不受此条约束
- **不用毛玻璃**：不写 `backdrop-filter`（`backdrop-blur-*`），大面积或带动画的元素上也不用
  `filter: blur()` 与 `mix-blend-mode`。三者都要在身后内容每次变化时重新采样合成，
  压在滚动内容、地图、跑马灯上就是持续掉帧。吸顶栏与浮层一律用实色底
  （`bg-background` / `bg-card` / `bg-popover`）：半透明底色没了模糊，文字会压在内容上读不清

## 骨架屏：`<AutoSkeleton>` 与手写 `<Skeleton>`

`components/ui/auto-skeleton.tsx` 包装 auto-skeleton-react：加载期间照常渲染真实布局，
骨架从渲染出来的 DOM 量出来，不再另写一份「长得像」的占位组件。手写的那份迟早和真实布局
对不上，而且不会有任何报错提示。首个接入点是 `/profile`（原来的 `ProfileSkeleton` 已删除）。

| 场景 | 用哪个 |
|---|---|
| 表单、资料页、详情卡片这类静态结构 | `<AutoSkeleton>`，加载期间喂占位数据 |
| 列表 / 表格 | 先构造占位行再用 `<AutoSkeleton>`；没有占位行就量不出任何东西 |
| 图表、地图、Monaco、虚拟列表、WebGL | 手写 `<Skeleton>`：子树会被多挂一份，上游也写明这几类量不准 |

五条硬约束：

1. **不要直接从 `auto-skeleton-react` 导入。** 上游的配色是写死的 `#e0e0e0`，深色模式下是一排
   亮灰条；换成 `<Skeleton>` 用的 `--accent` 也不行，浅色档它与 `--background` 同为 `#f4f4f5`，
   而上游画卡片时不带底色，骨架条落在页面底色上完全看不见。包装层改走 `--color-input`，
   另外补了 reduce 档关动效、加载期间整块 `inert`、首帧不闪、绝对 / 固定定位元素不进骨架、
   按钮 / 标签页 / 徽标 / 头像预先标好 `data-skeleton-role`（上游会把带字的按钮按
   「高度 ÷ 行高」拆成两行字条）。
2. **加载期间要喂占位数据。** 骨架量的是渲染出来的东西，`data?.map()` 在没数据时什么都不渲染，
   量出来也是空的。占位值的长度要接近真实值，文字条宽度由它决定；内容本身始终不可见。
3. **间距容器不能拆开包。** 包装层会多出几层 div，`page-stack` 的 `gap` 只作用于直接子元素，
   包进去的那几块要自己再套一层 `flex flex-col gap-*`（见 `/profile`）。
4. **包装层不开放 config。** 它是上游量尺寸那个 effect 的依赖，调用处每次渲染传一个新对象，
   就会每次渲染都重新量一遍；包装层用的是模块级常量。
5. **升级 auto-skeleton-react 后核对 `globals.css` 末尾那两条规则。** 「首帧不闪」与「卡片补底色」
   都靠库的内部 DOM 结构与内联样式识别，结构一变会静默失效（退回首帧闪烁 / 卡片无底色）。

上游只量一次、用固定像素宽度：加载期间改变窗口宽度或折叠侧边栏，骨架不会跟着重排。

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
| `layout/console-shell.tsx` | 外壳装配：侧边栏骨架 + 顶栏挂载 + 全局快捷键 |
| `layout/with-active-tab.tsx` | `withActiveTab()`：给组件补 `?tab=` 并自带 Suspense 边界 |
| `layout/brand.tsx` | 品牌 logo / 标题（读 BrandingContext），侧边栏与移动端抽屉共用 |
| `layout/sidebar/sidebar-nav.tsx` | 展开态：收藏区（`Reorder` 拖拽排序）+ 分组 → 页面 → 页内子项 |
| `layout/sidebar/sidebar-rail.tsx` | 折叠态图标轨道；有子项的页面靠悬浮浮层列出全部面板 |
| `layout/sidebar/command-palette.tsx` | `⌘K` 命令面板：跳转 + 主题 / 收藏 / 退出等操作 |
| `layout/sidebar/sidebar-resizer.tsx` | 右边缘宽度拖拽手柄（208–380px，双击复位） |
| `layout/sidebar/sidebar-shared.tsx` | `ActivePill` / `PinToggle` / `Kbd` / 缓动常量 |
| `layout/sidebar/recent-tracker.tsx` | 最近访问打点（渲染 `null`） |
| `layout/sidebar/search-trigger.tsx` | 命令面板入口（侧边栏顶部与移动端抽屉共用一份） |
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

## 顶栏（components/layout/topbar/）

| 文件 | 职责 |
|---|---|
| `topbar/console-topbar.tsx` | 顶栏骨架：容器查询上下文、滚动态、左右两区装配 |
| `topbar/topbar-breadcrumb.tsx` | 面包屑：分组段 / 面板段是**可切换菜单**，窄屏折进 `…` |
| `topbar/topbar-actions.tsx` | 动作区：搜索 / 收藏本页 / 主题 / 全屏 / 溢出菜单 + 统一的 `TopbarButton` |
| `topbar/notification-bell.tsx` | 通知铃铛（见下方「通知铃铛与实时事件」） |
| `topbar/user-menu.tsx` | 账户菜单：身份块 + 命令面板 / 资料 / 安全 / 帮助文档 / 退出 |
| `topbar/mobile-nav.tsx` | 移动端导航抽屉（`lg` 以下），装的是同一棵导航树 |

### 响应式按容器，不按视口

顶栏可用宽度 = 视口 − 侧边栏，而侧边栏可折叠（56px）、可拖宽（208–380px）：
**同一个 1280px 视口下顶栏能差出 300px**。所以顶栏是一个命名容器
（`@container/topbar`），下列三件事全部以顶栏自身宽度为准：

| 断点（顶栏宽度） | 变化 |
|---|---|
| `@lg` 32rem | 倒数第二级面包屑、账户名显隐 |
| `@xl` 36rem | 顶栏搜索在「搜索条」与图标之间切换 |
| `@2xl` 42rem | 收藏本页 / 主题 / 全屏三项在「平铺」与「收进 `⋯`」之间切换 |
| `@3xl` 48rem | 完整路径 ↔ `…` 折叠（`…` 菜单里永远是完整路径） |

只有两件事仍按视口（`lg:`）：移动端导航按钮与顶栏搜索 ——
它们回答的是"侧边栏在不在"，而那正是 `lg` 断点的定义。

**不要把 `lg:hidden` 和容器变体写在同一个元素上**：媒体查询与容器查询谁压过谁
只取决于生成顺序，等于把显隐交给运气。要么套一层 wrapper，要么只用其中一种。

三条实现约束：

1. **面包屑的分隔符跟前一段同步显隐**，不是跟自己。跟自己同步的话，前一段折进 `…`
   之后它会和 `…` 自带的分隔符并排出现，路径上凭空多一个 `›`。
2. **段内菜单的同级页面必须经权限过滤**（`useVisibleGroups()`）——
   否则面包屑会成为绕过侧边栏鉴权的后门，与一维目标同一条约束。
3. **溢出菜单里一项不少**。顶栏放不下不等于这些能力在小屏上不存在，
   而"某个功能只在大屏有"是使用者最难自己发现的一类差异。

全屏走 `screenfull`（前缀差异交给它），状态用 `useSyncExternalStore` 订阅
`fullscreenchange` —— 用 `useEffect` + `setState` 同步的话，用户按 F11 / Esc
退出时按钮图标不会跟着变。滚动态走 motion 的 `useScroll`，布尔值只在跨过阈值时翻转。

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
| `lib/app-sections.ts` | **区块目录**：每个区块的键 / 标题 / 说明 / 图标 + 分组 |
| `lib/app-scope-store.ts` | 最近打开的应用、列表视图偏好（localStorage） |
| `components/apps/app-shared.tsx` | 标识块、状态徽标、AppKey 复制、格式化 |
| `components/apps/app-list-views.tsx` | 卡片网格 / 表格两种列表视图 |
| `components/apps/app-row-actions.tsx` | 单应用操作菜单（列表行与详情页头部共用） |
| `components/apps/app-detail-header.tsx` | 详情页标识条 + 带区块换应用 + 关联页面入口 |
| `components/apps/app-section-nav.tsx` | 区块导航（宽屏竖向分组 / 窄屏横向胶囊） |
| `components/apps/vip/` | 会员区块：套餐 / 功能标识 / 试用 / 会员查询（见下节） |
| `components/apps/card-key/` | 卡密区块：概览 / 批次 / 卡密 / 核销记录（见下方「卡密区块」） |
| `lib/api/card-key.ts` / `lib/card-key-hooks.ts` | 卡密域 API 与 React Query hooks |
| `components/apps/rewarded-ad/` | 激励广告区块：概览 / 记录 / 配置（见下节） |
| `lib/api/rewarded-ad.ts` / `lib/rewarded-ad-hooks.ts` | 激励广告域 API 与 React Query hooks |
| `components/apps/ad-policy/` | 广告服务区块：概览 / 用户 / 开屏 / 策略（见下节） |
| `lib/api/ad-policy.ts` / `lib/ad-policy-hooks.ts` | 广告服务策略域 API 与 React Query hooks |
| `components/apps/crash-reports/` | 崩溃报告区块：单页列表 + 筛选 + 详情抽屉（见下节） |
| `lib/api/crash-reports.ts` / `lib/crash-report-hooks.ts` | 崩溃报告域 API 与 React Query hooks |
| `components/apps/points-store/` | 积分商城区块：概览 / 商品 / 兑换记录（见下节） |
| `lib/api/points-store.ts` / `lib/points-store-hooks.ts` | 积分商城与用户功能解锁的 API 与 React Query hooks |
| `components/apps/invite/` | 邀请区块：概览 / 配置 / 邀请码 / 邀请记录（见下节） |
| `lib/api/invite.ts` / `lib/invite-hooks.ts` | 邀请域 API 与 React Query hooks |
| `components/apps/leaderboard/` | 排行榜区块：概览 / 设置 / 奖励 / 实时榜 / 移出名单 / 结算记录（见下节） |
| `lib/api/leaderboard.ts` / `lib/leaderboard-hooks.ts` | 排行榜域 API 与 React Query hooks |
| `components/apps/lottery/` | 抽奖区块：活动 / 统计 / 抽奖记录 / 运气控制 / 抽奖次数（见下节） |
| `lib/api/lottery.ts` / `lib/lottery-hooks.ts` | 抽奖域 API 与 React Query hooks |
| `components/apps/item-grant/` | 物品发放区块：规则 / 总览 / 发放记录 / 执行记录与画布编辑器（见下节） |
| `lib/api/item-grant.ts` / `lib/item-grant-hooks.ts` | 物品发放域 API 与 React Query hooks |
| `components/apps/account-deletion/` | 账号注销区块：概览 / 申请 / 配置 / 重新注册限制（见下节） |
| `lib/api/account-deletion.ts` / `lib/account-deletion-hooks.ts` | 账号注销域 API 与 React Query hooks |
| `components/apps/account-setup/` | 账号初始化区块（`?tab=account-setup`，认证与安全分组）：统计 + 策略单页；建号方式文案在 `account-setup-shared.ts`，用户详情也从这里取 |
| `lib/api/account-setup.ts` / `lib/account-setup-hooks.ts` | 账号初始化域 API 与 React Query hooks：应用策略、单个用户的初始化情况、要求重新设置、改用户名 |
| `components/apps/rewards/reward-list-editor.tsx` | 目录驱动的权益列表编辑器，积分商城商品、邀请奖励与排行榜奖励共用 |
| `components/apps/cloud-storage/` | 用户云存储区块：概览 / 用户 / 配置（见下节） |
| `lib/api/cloud-storage.ts` / `lib/cloud-storage-hooks.ts` | 用户云存储域 API 与 React Query hooks |
| `lib/api/vip.ts` / `lib/vip-hooks.ts` | 会员域 API 与 React Query hooks |
| `components/apps/ai-gateway/` | AI 中转区块：概览 / 模型 / 调用记录 / 密钥 / 额度 / 设置（见下方「AI 中转区块」） |
| `lib/api/ai-gateway.ts` / `lib/ai-gateway-hooks.ts` | AI 中转网关域 API 与 React Query hooks |

四条硬约束：

1. **区块目录是单一事实源**。侧边栏三级子项由 `appSections` 派生，详情页导航与列表页
   快捷入口也读它 —— 在任一处另抄一份，就会出现「侧边栏有这一项、详情页没这个区块」。
2. **`?tab=` 的键不可改名**。`policy` 与 `auth-protocol` 刻意保留旧名（显示名是
   「认证与会话」「接入」），已分享出去的深链不该因为改叫法而失效。
3. **侧边栏子项链接不带 appKey**（`/apps?tab=oauth`），由列表页转交给
   `app-scope-store` 记住的最近应用。因此**详情页必须写这条记忆**，
   否则侧边栏那一批子项会永远落到第一个应用上。
4. **详情页只挂载当前区块**。十几个面板同时挂载会在进页面的一瞬间打出十几条请求
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

七个页签同步到 URL（`?tab=dashboard|assessments|reviews|rules|lists|entities|simulator`），
另有 `section=rules|actions|templates`（规则策略分区）、`entity=account|device|ip`（实体画像类型）、
`app=<应用 ID>`（应用作用域）。旧值 `tab=actions|templates|devices|ips|accounts` 兼容跳到新位置。

| 文件 | 职责 |
|---|---|
| `(console)/risk-control/page.tsx` | 页面骨架、Tab / 分区 / 实体类型路由、跨页签导航（`RiskNavProvider`） |
| `components/risk/risk-shared.tsx` | 目录上下文 `RiskCatalogProvider`、配色（等级 / 动作 / 维度 / 名单 / 判定来源）、徽标、维度条、分页、格式化 |
| `components/risk/risk-scope.tsx` | 应用作用域（`RiskScopeProvider` / 页头选择器 / `ScopeBadge` / 编辑器里的 `ScopeField`） |
| `components/risk/risk-nav.tsx` | 跨页签导航上下文：带着对象去评估记录 / 实体画像 / 规则详情 / 模板 / 名单库 |
| `components/risk/risk-dashboard.tsx` | 大盘：引擎运行态与执行器就绪、KPI、趋势、维度贡献、判定来源、名单、场景矩阵、榜单 |
| `components/risk/risk-assessments.tsx` | 评估记录（维度条 + 判定来源）、详情抽屉（维度得分 / 名单命中 / 实际处置 / 规则轨迹 / 现行名单 / 重放） |
| `components/risk/risk-reviews.tsx` | 复核队列（卡片两列） |
| `components/risk/risk-dispose-dialog.tsx` | **唯一的**处置弹窗与复核弹窗、处置结果列表 `EnforcementList` |
| `components/risk/risk-policy.tsx` | 规则策略页签：规则 / 处置策略 / 规则模板三个分区 |
| `components/risk/risk-rules.tsx` | 规则列表（按维度 / 场景分组、生效 / 观察切换）、编辑器（作用域 / 模式 / 强制处置）、规则详情 |
| `components/risk/risk-actions.tsx` | 处置策略（作用域、全部场景通用、封禁对象、分数覆盖条） |
| `components/risk/risk-templates.tsx` | 规则模板卡片、逐条预览（已安装标记）、安装选项 |
| `components/risk/risk-condition-form.tsx` | **schema 驱动**的条件参数表单 + 表达式变量速查 |
| `components/risk/risk-lists.tsx` | 名单库（筛选 / 批量添加 / 编辑 / 批量删除 / 名单检测）、`CurrentListEntries` |
| `components/risk/risk-entities.tsx` | 实体画像外壳 + 设备 / IP 列表与详情（现行名单、防火墙封禁、处置） |
| `components/risk/risk-accounts.tsx` | 账号画像列表与详情（维度得分、现行封禁、名单、关联 IP / 设备） |
| `components/risk/risk-simulator.tsx` | 模拟器：草稿规则（可观察模式）+ 环境覆写 + 用户 ID / 已过验证码 |
| `lib/api/risk.ts` / `lib/risk-hooks.ts` | 风控域 API 与 React Query hooks |

六条硬约束：

1. **枚举与参数 schema 一律来自 `/risk/metadata`，不在前端硬编码。**
   场景（含服务、能否弹验证码）、维度、等级、动作、规则模式、名单种类与类型、封禁对象、判定来源、
   模板摘要、条件类型的参数字段都由后端目录下发。唯一的例外是「全部场景」取值 `*`（`SCENE_ALL`）。
2. **图表一律用 shadcn `ChartContainer`**，配色走 `risk-shared.tsx` 的色板
   （`LEVEL_COLORS` / `ACTION_COLORS` / `DIMENSION_COLORS` / `DECISION_COLORS` / `LIST_KIND_COLORS`）。
   等级、动作、维度三套刻意分色系，同屏出现时不会互相混淆。面积图实色半透明填充，不用渐变。
3. **分页信封在 `lib/api/risk.ts` 的 `paged()` 里统一转换**（后端 `{ list, total }`）。
4. **失效关系集中在 `risk-hooks.ts`**。名单、处置、复核三类写操作走同一组 `ENTITY_STATE_KEYS`
   （名单、账号 / 设备 / IP 画像、评估详情、大盘），复核另加评估列表与待复核队列。
5. **处置只有一个入口组件**（`risk-dispose-dialog.tsx`）：评估详情、复核、账号 / 设备 / IP 画像都复用它。
   对象没有对应标识时（没有设备号）该勾选项禁用；防火墙只在「黑名单」下出现。
   提交后展示逐项的 `enforcement` 结果，失败标红 —— 「判了封禁」与「真的封上了」是两件事。
6. **应用作用域放页面级**（`?app=`），作用于大盘、评估、复核、规则、策略、名单、账号画像与设备；
   IP 库是全平台数据，不按应用过滤。编辑器里的作用域默认取当前作用域；规则 PUT 时 `appId: 0` 表示改回全平台。

大盘顶部的**引擎运行态条**不是装饰：「大盘全是 0」有两种截然不同的原因
（真没风险 / 根本没规则在跑），零规则或零策略时直接给出「安装规则模板」入口。
执行器就绪徽标（情报源、GeoIP、Redis、限流、账号封禁、防火墙、验证码）回答「ban 动作能不能真的落地」。

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
钱包按流水时间），所以订单页与概览的笔数可能对不上；这是口径差异，界面上不另加脚注。

## 会员区块（/apps/{appKey}?tab=vip）

| 文件 | 职责 |
|---|---|
| `components/apps/vip/app-vip-panel.tsx` | 骨架 + 四个视图的内部切换 |
| `components/apps/vip/vip-plans-panel.tsx` | 套餐：付费与试用**分区**列出、逐条启停与删除 |
| `components/apps/vip/vip-plan-editor.tsx` | 套餐编辑抽屉（种类 / 期限 / 时长 / 价格 / 功能勾选 / 设备限制） |
| `components/apps/vip/vip-features-panel.tsx` | 功能标识目录：建 / 停用 / 删除，并说清被哪些套餐用着 |
| `components/apps/vip/vip-trial-panel.tsx` | 试用：汇总（累计 / 试用中 / **已转化** / 转化率）+ 领取记录 + 代领 + 恢复资格 |
| `components/apps/vip/vip-member-panel.tsx` | 会员查询：权益全貌 + 试用历史与资格 + 授予 + 收回 + 开通记录 |
| `components/apps/vip/vip-shared.tsx` | 来源 / 状态徽标、功能标识徽标、金额与剩余时长格式化、套餐期限与记录时长口径、收回确认弹窗（交易中心与用户详情也从这里取） |

四条硬约束：

1. **付费与试用分区展示，不混在一张表里。** 它们的入口完全不同（一个被购买、
   一个被领取），混着列会让人以为试用也能卖 —— 而那正是后端两道闸门拦下的事。
2. **功能标识只在目录里建，套餐上只能勾。** 允许在套餐里手打标识等于放弃了
   「拼错有报错」这件事：`exprot` 在服务端表现为校验永远返回 false，没有任何一处说得出为什么。
3. **删除前说清影响面，一句话即可。** 功能权益跟随套餐与功能目录的当前配置：
   删功能标识 = 所有在期会员立即失去它；删套餐不影响已开通的会员（后端定格为删除前的配置）。
4. **恢复试用资格只删资格，不收回时长。** 客服要的是"让他重领"，
   顺手扣掉时长会变成用户眼里的"我的会员没了"。

`UserPicker` 的触发器是 `w-full`，**塞进 `SectionCard` 的 `aside` 必须套一层定宽容器** ——
`SectionCard` 是 `overflow-hidden`，不定宽会把旁边的按钮整个顶出卡片外（不报错，只是看不见）。

永久会员与限时会员是并列的两条线（`vipLifetimeAt` 与 `vipExpireAt`）：永久套餐恒 0 天、只能是付费套餐、
期限只在新建时可选；永久会员没有到期时间，另买的限时会员到期时间看 `timedExpireAt`。
「扣减天数」只动限时那条线、「取消永久会员」只动永久那条线，两个按钮按这个人实际有哪条线分别出现。
套餐与开通记录的时长一律走 `vipPlanTerm` / `formatVipTerm`，不要再写 `{durationDays} 天` —— 永久套餐会显示成「0 天」。

会员时长与功能权益的语义（顺延、并集、跟随套餐当前配置）全部由后端决定，控制台只展示；
判定入口在 [internal/service](../internal/service/CLAUDE.md#会员判定与试用期会员)。

## 激励广告区块（/apps/{appKey}?tab=rewarded-ad）

| 文件 | 职责 |
|---|---|
| `components/apps/rewarded-ad/app-rewarded-ad-panel.tsx` | 骨架 + 概览 / 记录 / 配置三个视图的内部切换 |
| `components/apps/rewarded-ad/rewarded-ad-overview-panel.tsx` | 今日观看 / 发放 / 人数 / 待确认 + 趋势图 + 各场景的窗口内表现 |
| `components/apps/rewarded-ad/rewarded-ad-views-panel.tsx` | 观看记录：两方（回调 / 上报）各自到没到、原因、补发 |
| `components/apps/rewarded-ad/rewarded-ad-config-panel.tsx` | 接入（平台、应用 ID、Security Key、回调地址）+ 校验模式与总限额 + 奖励场景（含会员权益：会员天数带的功能、会员期间是否免广告） |
| `components/apps/rewarded-ad/rewarded-ad-shared.tsx` | 状态徽标、拒发原因、校验模式文案、权益摘要 |

四条硬约束：

1. **权益表单由后端目录驱动**（配置接口随附 `catalog`，是卡密目录的子集）。前端另抄一份，
   就会出现「控制台能勾、保存时报激励广告不能发放该权益」。
2. **Security Key 留空即不修改**，与其它密钥字段一致；后端只回 `hasSecurityKey` 与末 4 位提示。
3. **会员权益跟随场景当前配置**：改了对已领到的会员同样生效（后端在判定时实时取，见 docs/rewarded-ads.md）。
   功能只对「会员天数」生效，保存时没勾会员天数就把功能清空，否则后端拒收；「免广告」不填时后端按免广告，
   草稿里没设置过的场景同样按开着展示。
4. **记录表必须同时展示「回调」与「上报」两列。** 用户投诉没到账时，第一个问题永远是
   「哪一方没来」—— 回调没来查平台与签名，上报没来查客户端，只看最终状态回答不了。

结算语义（两方确认、限额、补发）全部由后端决定，见 [docs/rewarded-ads.md](../docs/rewarded-ads.md)。

## 广告服务区块（/apps/{appKey}?tab=ad-policy）

| 文件 | 职责 |
|---|---|
| `components/apps/ad-policy/app-ad-policy-panel.tsx` | 骨架 + 概览 / 用户 / 开屏 / 策略四个视图的内部切换 |
| `components/apps/ad-policy/ad-policy-overview-panel.tsx` | 选择分布（同意 / 拒绝 / 待重新选择）+ 今日开屏 + 开屏与选择变更两张柱状图 |
| `components/apps/ad-policy/ad-policy-consents-panel.tsx` | 用户选择：「当前」每人一行，「历史」逐次变更；账号链到用户详情的广告页签 |
| `components/apps/ad-policy/ad-policy-splash-panel.tsx` | 开屏记录：结局、平台错误码、加载与展示耗时；未登录的记录只有设备 |
| `components/apps/ad-policy/ad-policy-config-panel.tsx` | 同意要求（开关、会员免除、条款版本与链接）+ 基础服务清单 + 开屏广告位与频控 |
| `components/users/detail/user-ad-policy-tab.tsx` | 用户详情「广告」页签：当前处境、开屏累计与最近记录、变更历史 |

两条硬约束：

1. **服务模式只展示后端的结论**（`mode` / `exempt` / `decisionRequired`，出自 `adpolicy.Evaluate`），
   前端不再按「同意了没有、是不是会员」自己推一遍 —— 两处各写一遍迟早对不上。
2. **条款版本只能调高**，调高前弹窗确认：保存后所有人回到「需要重新选择」，此前同意的人在重新选择前只有基础服务。

`configured: false` 表示应用没保存过策略，客户端此时按自己内置的默认策略处理，界面上如实显示「未配置」。
判定与接口见 [docs/ad-policy.md](../docs/ad-policy.md)。

## 崩溃报告区块（/apps/{appKey}?tab=crash-reports）

客户端在崩溃屏上经用户逐份同意后上报，规则见 [docs/crash-reports.md](../docs/crash-reports.md)。放在「概览」分组，
**一页完成、不分标签**（报表类页面不做多标签）。

| 文件 | 职责 |
|---|---|
| `components/apps/crash-reports/app-crash-reports-panel.tsx` | 摘要（总数 / 未处理 / 近 24 小时）+ 筛选（关键字、类型、版本、状态、时间）+ 列表 + 本页批量标记与删除 |
| `components/apps/crash-reports/crash-report-detail-sheet.tsx` | 详情抽屉：全部字段、上报账号与对外用户 ID、报告正文（等宽、复制、下载 .txt）、标记与删除 |
| `components/apps/crash-reports/crash-report-shared.tsx` | 类型与状态徽标、版本 / 设备 / 位置的拼接、下载 |

三条约束：

1. **列表不带正文**，正文只在详情接口里取：一份报告最多 256 KB，一页 20 份全带上就是几 MB。
2. **版本下拉只列 facets 接口给的版本**（真实出现过的），不让管理员手打版本号。
3. **选择只作用于当前页**，翻页或改筛选即清空，批量操作不会落到看不见的行上。写操作整组失效（前缀 `admin-crash-reports`）。

## 积分商城区块（/apps/{appKey}?tab=points-store）

| 文件 | 职责 |
|---|---|
| `components/apps/points-store/app-points-store-panel.tsx` | 骨架 + 概览 / 商品 / 兑换记录三个视图的内部切换，跨视图带筛选（商品的「记录」、待发放） |
| `components/apps/points-store/points-store-overview-panel.tsx` | 开关 + 今日与窗口内的兑换笔数、人数、消耗积分、待发放、在售 + 两张柱状图 + 热门商品 |
| `components/apps/points-store/points-store-items-panel.tsx` | 商品按分类分组：权益摘要、价格与限量限兑、兑换汇总、上下架、编辑、删除 |
| `components/apps/points-store/points-store-item-editor.tsx` | 商品编辑抽屉：基本信息、权益（目录驱动，可增删行）、库存与限兑、兑换条件、上下架时间 |
| `components/apps/points-store/points-store-redemptions-panel.tsx` | 兑换记录：实际发放、状态；待发放的在这里「发放」（写给用户的话）或「取消」（退积分） |
| `components/apps/points-store/points-store-shared.tsx` | 周期 / 对象 / 状态文案、权益摘要、时长口径 |
| `components/users/detail/user-feature-unlocks-panel.tsx` | 用户详情「资产」页签里的功能解锁：当前生效、授予、逐条收回 |

四条硬约束：

1. **权益表单由后端目录驱动**（`/points-store/catalog`）：哪一档能重复（功能解锁）、哪一档只能单独存在（人工发放）
   都来自目录，编辑器据此决定「添加权益」里还能选什么。
2. **可兑换判定只在后端**（`pointstore.Evaluate`），控制台不推演「这个人能不能兑」。
3. **功能解锁不混进会员面板的「发放」**：它只加功能、不让人成为会员，放在一起会让人以为授予功能等于开了会员。
   授予与收回之后会员权益的缓存一并失效（`features` 变了）。
4. **只有待发放的兑换能处理**；已到账的权益已经被用上了，界面上不给取消入口。

兑换语义（单事务、幂等、快照、顺延）见 [docs/points-store.md](../docs/points-store.md)。

## 接入区块（/apps/{appKey}?tab=auth-protocol）

| 文件 | 职责 |
|---|---|
| `components/apps/app-auth-protocol-panel.tsx` | 骨架：概览 / 认证策略 / 安全与密钥 / 代码示例四个视图的切换、共享草稿、底部保存条、自检与密钥轮换的状态 |
| `components/apps/auth-protocol/auth-protocol-overview.tsx` | 四项状态（安全等级、登录方式、注册方式、自检）、接入凭据（复制 .env）、自检时间线 |
| `components/apps/auth-protocol/auth-protocol-policy.tsx` | 登录 / 注册方式与登录标识的勾选卡、依赖提示（第三方渠道、短信服务商、手机号标识）、行为开关、注册字段 |
| `components/apps/auth-protocol/auth-protocol-security.tsx` | 安全等级三选一、应用密钥（轮换、明文只显示一次）、加密档传输公钥、保留旧接口时的旧命名空间传输加密 |
| `components/apps/auth-protocol/auth-protocol-shared.tsx` | 枚举与图标、草稿取值与比较、占位策略、勾选卡与行内提示 |

三条约束：

1. **概览与代码示例读已保存的策略，策略与安全视图读草稿。** 概览回答的是「客户端此刻看到什么」，拿草稿说话会让人以为改动已生效。
2. **草稿只比较可写字段**（`draftOf` / `sameDraft`，集合按集合比）：密钥轮换会刷新 `signingSecretHint`，整份对象比较会把轮换误判成未保存的修改。
3. **依赖提示放在勾选卡正下方**：勾了第三方却没有可用渠道、配了渠道却没勾第三方、开了短信却没把手机号列为登录标识。
   这几种组合两边看上去都正常，客户端却一个入口都没有（Voyage 的 QQ 登录就卡在「没勾第三方」上）。

## 邀请区块（/apps/{appKey}?tab=invite）

| 文件 | 职责 |
|---|---|
| `components/apps/invite/app-invite-panel.tsx` | 骨架 + 概览 / 配置 / 邀请码 / 邀请记录四个视图的内部切换，跨视图带筛选（某个码、某个邀请人、等待中） |
| `components/apps/invite/invite-overview-panel.tsx` | 开关状态 + 累计与今日数字 + 邀请趋势柱状图 + 邀请排行与活动码排行（点进邀请记录） |
| `components/apps/invite/invite-config-panel.tsx` | 开关与全部配置一次保存：注册表单、邀请码格式（含示例码）、邀请人资格、双方奖励与阶梯、发放时机与上限、防刷、文案（占位符插入） |
| `components/apps/invite/invite-codes-panel.tsx` | 个人码与活动码同表：使用次数、有效期、启停；活动码可编辑、删除 |
| `components/apps/invite/invite-code-editor.tsx` | 活动码弹窗：指定一个或批量生成（可加前缀）、推广人（UserPicker）、被邀请人奖励跟随默认或自定义、次数与有效期；批量创建后列出全部码供复制 |
| `components/apps/invite/invite-relations-panel.tsx` | 邀请记录：双方实际所得、邀请人奖励状态；等待中的可立即发放或取消 |
| `components/apps/invite/invite-shared.tsx` | 枚举文案、状态徽标、示例码生成、有效期口径、分页条 |

四条硬约束：

1. **表单由后端目录驱动**（`/invite/catalog`）：权益、各枚举、默认配置与上限都从目录来；目录里没有下发的几项上限
   （字段名称、阶梯名称、等级门槛等）在配置面板顶部有一份常量，最终以后端校验（40000）为准，原因原样 toast。
2. **活动码的 `rewards` 有三态**：`null` 跟随应用默认、`[]` 不发奖励、非空为自定义。编辑器里「跟随默认 / 自定义」切换的就是它，
   自定义而列表为空即不发奖励，不要把空列表当成跟随默认。
3. **个人码只能停用不能删除**：删了主人下次打开邀请页又会生成一个新的。删除入口只给活动码。
4. **示例码在渲染期不调用随机数**：由种子确定性地取字符，「换一个」只换种子；字符集与后端 `invite/code.go` 保持一致。

能否填写、奖励发放与结算全部在后端，规则见 [docs/invite.md](../docs/invite.md)。

## 排行榜区块（/apps/{appKey}?tab=leaderboard）

| 文件 | 职责 |
|---|---|
| `components/apps/leaderboard/app-leaderboard-panel.tsx` | 骨架 + 六个视图的切换，概览可带榜单跳到实时榜、带失败提示跳到结算记录 |
| `components/apps/leaderboard/leaderboard-overview-panel.tsx` | 开关状态、待发 / 失败奖励提示、四个数字、各榜本期参与人数与前 3 名（真实账号、匿名 / 移出标记）、上次结算时间 |
| `components/apps/leaderboard/leaderboard-settings-panel.tsx` | 总开关、榜单启用与标签顺序（上移 / 下移）、展示名次、上榜门槛、展示身份、允许匿名、积分口径（分类表随「计入管理员调整」变化） |
| `components/apps/leaderboard/leaderboard-rewards-panel.tsx` | 每个周期榜一组名次档位（RewardListEditor），前端按后端同一说法提示范围不合法、超出展示名次、重叠与空档 |
| `components/apps/leaderboard/leaderboard-board-panel.tsx` | 实时榜：选榜单与期号（本期 / 已结算往期 / 指定期号），完整名次表，行操作移出与恢复 |
| `components/apps/leaderboard/leaderboard-exclusions-panel.tsx` | 移出名单：搜索、分页、批量移出、恢复 |
| `components/apps/leaderboard/leaderboard-exclude-dialog.tsx` | 移出弹窗：单人（实时榜行操作）或按用户 ID 批量，原因与人数上限来自 limits |
| `components/apps/leaderboard/leaderboard-snapshots-panel.tsx` | 结算记录列表、条目明细（按奖励状态筛选）、整期重试失败、手动结算弹窗 |
| `components/apps/leaderboard/leaderboard-shared.tsx` | 枚举文案、奖励状态徽标、用户单元格、期号格式与上一期期号、共用保存条与载荷 |

三条硬约束：

1. **设置与奖励共用一个 `PUT settings`**：两个视图各自只改自己那一半，另一半按服务端已保存的值带上（`savedPayload`）。
   已保存的权益要先按草稿往返一次（`normalizeBoardRewards`）再回传，后端出网的权益带着 `money: "0"` 这类零值。
2. **目录驱动**：榜单、权益、展示身份、奖励状态、积分分类与上下界全部来自 `GET settings`，不另抄一份；
   档位的前端提示只是提示，校验以后端（40000）为准，原因原样 toast。
3. **手动结算的默认期号按浏览器时区算**，周期边界以后端应用默认时区为准，跨期那几个小时可能差一期；
   周期没结束时后端回 40922，弹窗里原样显示。

名次、口径、结算与发放全部在后端，规则见 [docs/leaderboard.md](../docs/leaderboard.md)。

## 抽奖区块（/apps/{appKey}?tab=lottery）

| 文件 | 职责 |
|---|---|
| `components/apps/lottery/app-lottery-panel.tsx` | 骨架 + 活动 / 统计 / 抽奖记录 / 运气控制 / 抽奖次数五个视图的切换，跨视图带活动与「待发放」筛选 |
| `components/apps/lottery/lottery-activities-panel.tsx` | 活动卡片：状态与形态、抽奖方式、奖品与公示概率、累计 / 今日 / 待发放；上线 / 暂停 / 结束、编辑、删除 |
| `components/apps/lottery/lottery-activity-editor.tsx` | 活动编辑抽屉：基本信息与时间、三种次数与视频抽奖场景、奖盘、保底、规则；底部列出奖盘的待修正项 |
| `components/apps/lottery/lottery-prize-board.tsx` | 奖盘：九宫格是 3×3 的盘（外圈顺时针 0–7 从左上角起，中心 GO），转盘是可增删、上下移的列表；右侧改选中的一格 |
| `components/apps/lottery/lottery-activity-form.ts` | 草稿模型：种子（新活动的默认 8 格）、载荷、与后端 `ValidateInput` 同规则同说法的校验 |
| `components/apps/lottery/lottery-stats-panel.tsx` | 单活动统计：8 个数字、趋势（次数与中奖为柱、人数为线）、次数来源、奖品抽中数与库存 |
| `components/apps/lottery/lottery-draws-panel.tsx` | 抽奖记录：活动 / 用户 / 状态 / 来源 / 只看中奖 / 关键词 / 时间筛选；待发放的「标记已发放」写给用户的话 |
| `components/apps/lottery/lottery-fortune-panel.tsx` | 运气控制：每日运气的差异（σ 滑杆、开关）、今日分布（理论 / 干预前 / 干预后三列）、下干预（指定用户或全站 × 重抽 / 定档 / 偏移 × 时间窗，先预演再执行）、查一个人（干预前后读数与一键重抽 / 定档 / 撤销）、干预记录（状态与方式筛选、逐条撤销、全部恢复自然运气）。规则见 docs/lottery.md「运气控制」 |
| `components/apps/lottery/lottery-tickets-panel.tsx` | 抽奖次数（bonus 分批账本）：总览（持有人数、赠送 / 兑换两档、今日发放与使用、扣除顺序、各来源累计与剩余）、账户列表、一个人的批次（按使用顺序）与发放 / 扣减（确认后执行）、次数流水（种类 / 来源筛选）。规则见 docs/lottery.md「抽奖次数」 |
| `components/apps/lottery/lottery-shared.tsx` | 状态 / 形态 / 来源文案、徽标、九宫格位置表、公示概率、奖品摘要与默认名 |

五条硬约束：

1. **活动连同全部奖品一次保存**（POST / PUT 整体），没有单独的奖品接口：九宫格恰好 8 格、保底奖属于本活动、
   至少一个不限库存的奖品都是整张奖盘的约束。已有奖品必须带 `id` 回传，否则后端当成删了再建，已抽中数量清零。
2. **保底奖按位置指定**（`pityPosition`），草稿里按行键记（`pityKey`），保存时换算成下标：奖品挪位置时保底跟着奖品走。
   保底奖改成「谢谢参与」或被删除时草稿里的保底奖随之清空。
3. **公示概率与后端 `lottery.Probabilities` 同一算法**（有库存奖品的权重合计作分母、四舍五入到 0.01），
   编辑时按草稿实时算；改了算法两边显示的概率就会对不上，而用户端看到的是后端那一份。
4. **奖品表单由后端目录驱动**（`/lottery/catalog`）：类型、取值形态、上下界、可选套餐（已去掉试用）与功能、
   发「抽奖次数」的激励广告场景都来自目录。场景归激励广告区块配置，这里只选；激励广告未开启或没有这类场景时如实提示。
5. **前端校验只是提示**，保存时第一条问题 toast；后端的 40000 原因原样 toast。删除有记录的活动后端回 40927，
   toast 上给「结束活动」的快捷操作。控制台没有试抽入口：结果只在服务端产生，抽一次就是真的发了奖。

旧版（报名、种子承诺 / 揭示、上链校验、`/api/lottery/*`）已整体删除，规则见 [docs/lottery.md](../docs/lottery.md)。

## 物品发放区块（/apps/{appKey}?tab=item-grant）

| 文件 | 职责 |
|---|---|
| `components/apps/item-grant/app-item-grant-panel.tsx` | 骨架 + 规则 / 总览 / 发放记录 / 执行记录四个视图，跨视图带规则、执行与状态筛选 |
| `components/apps/item-grant/item-grant-rules-panel.tsx` | 规则卡片（触发方式、状态、节点构成、统计、执行进度与取消）；新建（空白或后端模板）、复制、上线 / 暂停 / 执行 / 归档 / 删除 |
| `components/apps/item-grant/item-grant-editor.tsx` | 全屏画布编辑器：节点库（拖放或点击）、React Flow 画布、右侧配置 / 问题 / 预演；⌘S / ⌘Z / ⌘⇧Z / ⌘D |
| `components/apps/item-grant/item-grant-store.ts` | 画布状态（zustand + **zundo** 撤销重做）：equality 只比配置与结构，连续变化合成一步 |
| `components/apps/item-grant/item-grant-graph.ts` | 后端画布 ↔ React Flow 往返、默认配置、dagre 自动排版、成环检测 |
| `components/apps/item-grant/item-grant-canvas-nodes.tsx` | 节点（输入 / 输出端口、摘要、问题角标、预演人数、追踪高亮）与连线（端口语义色、追踪流动、选中删除） |
| `components/apps/item-grant/item-grant-inspector.tsx` / `item-grant-fields.tsx` | 每种节点的配置表单；物品编辑器（目录驱动，按人计算数量） |
| `components/apps/item-grant/item-grant-preview-panel.tsx` | 预演：抽样估算（节点 / 端口到达人数、全量推算）与单人追踪（路径与物品） |
| `components/apps/item-grant/item-grant-records-panel.tsx` / `item-grant-runs-panel.tsx` / `item-grant-overview.tsx` | 发放记录（路径、结果、标记发放、重试）、执行记录（进度轮询）、总览 |

四条硬约束：

1. **目录驱动**：物品、节点与端口、触发方式、事件与事件字段、条件字段与运算符、人群、周期、跳转页面、模板全部来自
   `/item-grants/catalog`，前端不另抄；`describeItem` / `rulesExpr` 只是展示口径，与后端 `DescribeItem` / `RulesExpr` 同一份规则。
2. **React Flow 节点的 `data` 就是后端节点配置**，问题、预演、追踪这类叠加信息走 `CanvasOverlayContext`，不进 data、不进撤销历史。
3. **校验以后端为准**：画布变化 600ms 后调 `/validate`（与上线同一套判据），问题按 `nodeId` / `edgeId` 标在画布上；
   生效中的规则保存时后端拒绝有错误的画布（40000，原因原样 toast）。
4. **预演不发放**，与正式执行同一个判定函数；画布一改，旧的预演结果立即清掉。

规则见 [docs/item-grant.md](../docs/item-grant.md)。

## AI 通道编辑（/apps/{appKey}?tab=ai 与 /configuration?tab=ai）

平台级与应用级共用 `components/ai/ai-channel-panel.tsx`，只差 scope。新建 / 编辑走右侧大号 Sheet
（`ai-channel-editor-sheet.tsx`，`sm:max-w-5xl`）：左栏按目录分类、可搜索的供应商，右栏依次是基本信息、连接、型号、选项、连通性测试，⌘S 保存。

| 文件 | 职责 |
|---|---|
| `components/ai/ai-channel-editor-sheet.tsx` | 编辑 Sheet：供应商侧栏、分块表单、草稿测试、删除确认 |
| `components/ai/ai-model-manager.tsx` | 型号管理器：芯片列表、星标设默认、粘贴多行拆分、一键拉取后搜索勾选（加入 / 只保留选中） |
| `components/ai/ai-provider-fields.tsx` | 目录驱动的动态字段；`exclude` 让型号管理器接管 `models` / `defaultModel` |
| `components/ai/ai-sheet-parts.tsx` | 大号 Sheet 的版面零件 `SheetBlock` / `ToggleTile`，与 AI 中转的模型 Sheet 共用 |

三条硬约束：

1. **型号的存储形状不变**：仍是 `settings.models`（换行分隔）+ `settings.defaultModel`。管理器把默认型号排在第一个，
   与后端「默认型号留空取第一行」一致；不要改成数组，后端与已存配置都按字符串读。
2. **拉取与测试对草稿生效**（`POST …/ai/draft/models`、`…/ai/draft/test`）：新建不必先保存；编辑时密钥留空沿用已存的，
   换了供应商则不沿用。上游失败返回 `ok=false` 与上游原话，就地展示，不 toast 成「接口错误」。
3. **换供应商即重置字段与密钥**：上一家的地址、密钥不能带到下一家去提交。

## AI 中转区块（/apps/{appKey}?tab=ai-gateway）

| 文件 | 职责 |
|---|---|
| `components/apps/ai-gateway/app-ai-gateway-panel.tsx` | 骨架 + 六个视图的切换；从概览 / 密钥 / 额度跳到调用记录时带上筛选 |
| `components/apps/ai-gateway/ai-gateway-overview-panel.tsx` | Base URL（可复制）、扣费顺序、调用与扣费趋势、按来源与模型汇总、未收与进行中 |
| `components/apps/ai-gateway/ai-gateway-models-panel.tsx` | 模型列表（点行即编辑）与删除确认 |
| `components/apps/ai-gateway/ai-gateway-model-sheet.tsx` | 添加 / 编辑模型的大号 Sheet：左栏上游通道（当前链路，含平台共享），右栏从通道型号里挑上游模型（应用自有通道可一键拉取，新建可多选批量添加）、对外信息、种类、价格（含典型用量估价）、选项 |
| `components/apps/ai-gateway/ai-gateway-requests-panel.tsx` | 调用记录：用户 / 模型 / 状态 / 接口筛选；明细弹窗含暂扣、价格、实扣、扣费来源与整笔退款 |
| `components/apps/ai-gateway/ai-gateway-keys-panel.tsx` | 用户密钥：启用 / 停用（管理端不新建，明文只给用户本人看一次） |
| `components/apps/ai-gateway/ai-gateway-credits-panel.tsx` | 某个用户的 AI 账户（与用户端总览同一份）与赠送额度调整、流水 |
| `components/apps/ai-gateway/ai-gateway-settings-panel.tsx` | 开关、扣费来源勾选与排序、额度、余额折算、倍率、限制 |
| `components/apps/ai-gateway/ai-gateway-shared.tsx` | 状态 / 接口 / 来源 / 价格项文案、点数与用量格式化 |

五条硬约束：

1. **上游通道不在这里配**：通道在「AI 服务」区块，这里只决定谁能用、怎么收钱。模型 Sheet 的左栏读的是
   通道链路（`/ai/channel`），型号清单与「一键拉取」复用通道的 `/ai/draft/models`（以已存配置为底、不传密钥）；
   借用的平台共享通道在应用作用域拉不到，只列它登记的型号。对话以外的种类只能选 OpenAI 协议的通道
   （后端对这些接口原样转发），其余通道置灰；换种类时已钉的不兼容通道回到「按链路」。
   批量添加是逐个 POST，失败的留在选择里、成功的去掉。版面零件（分块、开关块）与通道编辑共用 `components/ai/ai-sheet-parts.tsx`。
2. **价格项由后端目录驱动**（`/ai-gateway/catalog` 的 `kinds[].prices`）：换种类只显示该种类会用到的价格项，
   保存时其余项一律置 0。单位是「点」，token 类按每百万 token，界面上标在输入框后。
3. **点数、倍率、单价按字符串往返**（后端 shopspring/decimal），`formatPoints` 只做展示；调用记录里的
   `holds` / `charges` 是**微点整数**，展示前除以 `MICRO_PER_POINT`。
4. **扣费来源存「全部来源的顺序 + 哪些启用」**：取消勾选不丢位置，保存时按顺序筛出启用的发给后端。
   余额是真钱，未勾选时余额折算输入框禁用。
5. **赠送额度调整用方向 + 正数**，每次提交带 `crypto.randomUUID()` 作请求编号，成功后换新编号：
   网络重试命中同一编号时后端只生效一次（回 `replayed`）。

规格见 [docs/ai-gateway.md](../docs/ai-gateway.md)。

## 登录设备区块（/apps/{appKey}?tab=device-limit）

规则见 [docs/device-limit.md](../docs/device-limit.md)。六个视图（状态不进 URL）：

| 文件 | 职责 |
|---|---|
| `components/apps/device-limit/app-device-limit-panel.tsx` | 视图切换；**策略草稿在这一层**（按 appKey 绑定），模拟要能拿未保存的草稿试 |
| `device-limit-overview-panel.tsx` | 策略摘要条、处置指标、趋势 / 终端分布 / 每人设备数 / 触发维度、撞上限最多的用户 |
| `device-limit-policy-panel.tsx` | 总数与类别上限、超限处置、同设备、会员档位、新设备频率；右侧「生效上限一览」 |
| `device-limit-users-panel.tsx` | 单个用户：上限来源、各维度占用、在线设备、台账、处置时间线；下线 / 整理 / 例外 / 清台账 |
| `device-limit-overrides-panel.tsx` / `device-limit-override-dialog.tsx` | 例外名单与编辑弹窗（两处共用） |
| `device-limit-events-panel.tsx` | 处置记录 |
| `device-limit-simulator-panel.tsx` | 模拟：真实用户或虚构场景，服务端用与登录相同的判定 |
| `device-limit-shared.tsx` | 类别 / 处置 / 事件 / 来源的文案、图标与配色，`DeviceSlots`、`LimitStepper` |
| `lib/api/device-limit.ts` / `lib/device-limit-hooks.ts` | API 与 hooks，写操作整组失效（前缀 `admin-device-limit`） |

三条约束：

1. **类别色两套写法要同步**：`CLASS_META.color` 给图表（ChartConfig theme），`fill` / `tint` 是 Tailwind 类给槽位与图标底，
   改一处要改另一处，否则图例和槽位颜色对不上。
2. **概览的设备分布是台账口径**（窗口内登录过），不是此刻在线；此刻在线要逐个用户看会话，在用户设备视图里。
3. **「认证与会话」不再提交 `multiDeviceLogin` 两个旧字段**，那里只留一个状态入口。旧表单总带一个默认上限，
   提交它会让没配过设备数的应用凭空被限制（后端旧接口也已不再写它们）。

## 卡密区块（/apps/{appKey}?tab=card-key）

| 文件 | 职责 |
|---|---|
| `components/apps/card-key/app-card-key-panel.tsx` | 骨架 + 概览 / 批次 / 卡密 / 核销记录四个视图的切换，批次筛选在卡密与核销记录之间共用 |
| `components/apps/card-key/card-key-overview-panel.tsx` | 8 个数字（总数、可提取、已提取、有效授权、今日 / 累计核销、人数、面值已用 / 发出）、核销趋势（次数为柱、人数为线，按浏览器时区分日）、来源、热门批次（点击跳到这批的卡） |
| `components/apps/card-key/card-key-batches-panel.tsx` | 批次卡片：类型、停用、待生效、渠道、面值、权益、规则摘要、进度条；启停、查看卡、提卡，更多菜单里编辑 / 补发 / 以此为模板新建 / 三种导出 / 删除（40982 时解释并给「停用批次」） |
| `components/apps/card-key/card-key-batch-editor.tsx` | 批次编辑抽屉：新建（随机生成或导入卡面，卡面样例与格式容量提示）、以模板新建、修改（只开放之后才生效的项，其余灰显） |
| `components/apps/card-key/card-key-batch-form.ts` | 草稿模型：种子、从批次起草、卡面归一化与导入解析（与后端 `NormalizeCode` 同口径）、容量（字符集 ^ 位数 / 4）、与后端同规则同说法的校验、载荷 |
| `components/apps/card-key/card-key-reward-editor.tsx` | 权益行编辑：目录驱动的添加菜单，数量 / 金额 / 套餐 / 功能 + 天数（0 为永久，可多行），需要授权卡的档位给提示 |
| `components/apps/card-key/card-key-batch-dialogs.tsx` | 补发对话框；提卡对话框（数量 + 备注 → 当场给出卡面，复制全部或下载 TXT） |
| `components/apps/card-key/card-key-codes-panel.tsx` | 单卡列表：批次 / 状态（含部分使用、已过期）/ 类型 / 提取 / 关键词筛选；多选后复制、标记 / 取消提取、作废、恢复、删除 |
| `components/apps/card-key/card-key-detail-sheet.tsx` | 单卡抽屉：信息、批次权益、调整（备注、设备数、到期时间延长 / 设为 / 永久）、设备解绑与清空、核销与解绑流水 |
| `components/apps/card-key/card-key-redemptions-panel.tsx` | 核销记录：批次 / 来源 / 用户 / 关键词 / 时间筛选，展示实际发放；「代兑」对话框 |
| `components/apps/card-key/card-key-shared.tsx` | 类型 / 状态 / 字符集 / 对象 / 来源文案、规则摘要、权益与结果摘要、进度条、复制 |

五条硬约束：

1. **权益表单由后端目录驱动**（`/card-keys/catalog`）：档位、单位、上下界、能否重复、是否需要授权卡，
   以及可选的会员套餐与功能都随目录下发，前端不另抄枚举。
2. **「已过期」「部分使用」是算出来的**（`resolveCardState`）：后端没有这两档状态，到期时间与 `useCount` 才是依据。
3. **批量操作按选中的 id**，翻页或改筛选即清空选择；导出与提卡按批次，不按当前筛选。
4. **修改批次只传之后才生效的项**；统一到期时间不改就不传（后端收到空值保留原值），否则过了到期日的批次连名字都改不了。
5. **前端校验只是提示**，提交时第一条问题 toast；后端的报错原样 toast。删除批次遇到 40982 不当成失败提示，
   而是解释原因并给「停用批次」。

接口与语义见 [docs/card-key.md](../docs/card-key.md)。「卡密登录」的开关在「接入」的登录方式里，不在这个区块。

## 账号注销区块（/apps/{appKey}?tab=account-deletion）

| 文件 | 职责 |
|---|---|
| `components/apps/account-deletion/app-account-deletion-panel.tsx` | 骨架 + 概览 / 申请 / 配置 / 重新注册限制四个视图的切换，概览带状态筛选跳到申请 |
| `components/apps/account-deletion/account-deletion-overview-panel.tsx` | 开关与方式摘要、待审核与执行失败提示、四个数字、趋势柱状图（按浏览器时区分日）、状态 / 方式 / 来源环形图与原因条形图 |
| `components/apps/account-deletion/account-deletion-requests-panel.tsx` | 申请列表：状态 / 方式 / 来源 / 关键词 / 提交时间筛选，点行开详情抽屉 |
| `components/apps/account-deletion/account-deletion-request-sheet.tsx` | 详情抽屉：进度时间线、申请信息、提交时的账号快照与前置条件结论、设备与网络，底部按状态给处置按钮 |
| `components/apps/account-deletion/account-deletion-action-dialog.tsx` | 批准 / 驳回 / 撤销 / 立即执行 / 改期五种处置的弹窗；`ExecuteConfirm` 输入账号名确认 |
| `components/apps/account-deletion/account-deletion-config-panel.tsx` | 开关与全部配置一次保存（目录驱动），七项前置条件逐项 off / warn / block |
| `components/apps/account-deletion/account-deletion-tombstones-panel.tsx` | 重新注册限制：打码标识、对应账号、到期，逐条解除 |
| `components/apps/account-deletion/account-deletion-shared.tsx` | 状态 / 方式 / 来源 / 验证方式 / 前置条件的文案与配色、各状态可做的处置、分页条 |
| `components/users/detail/user-account-deletion-card.tsx` | 用户详情「处置」页签里的注销卡片：进行中的申请与快捷处置、代发起、历史 |

四条硬约束：

1. **可做的处置只看状态**（`availableActions`，与后端各接口允许的起始状态一致）：批准 / 驳回只对 reviewing，
   撤销对 reviewing / scheduled / failed，立即执行对 scheduled / failed，改期只对 scheduled。
   按钮另受 `app:user:write` 控制，只读管理员看得到申请但没有任何处置入口。
2. **立即执行与代发起立即注销必须输入账号名**，逐字一致才放行（后端同样核对 `confirm`）。
   返回 200 不等于成功：状态为 failed 时原因在 `failure` 里，按状态 toast。
3. **立即注销成功后用户已不存在**：hooks 刻意不失效 `admin-app-user`，用户详情页拿到 completed 后自己跳回列表，
   否则会先闪一下「未找到该用户」。
4. **目录没有下发的上限**（原因标识 / 名称、须知标题、同意文案、客服联系方式）写在配置面板顶部，与
   `accountdeletion/types.go` 一致，最终以后端校验为准，原因原样 toast。

状态机、前置条件与验证判定全部在后端，规则见 [docs/account-deletion.md](../docs/account-deletion.md)。

## 用户云存储区块（/apps/{appKey}?tab=cloud-storage）

| 文件 | 职责 |
|---|---|
| `components/apps/cloud-storage/app-cloud-storage-panel.tsx` | 骨架 + 概览 / 用户 / 配置三个视图的内部切换 |
| `components/apps/cloud-storage/cloud-storage-overview-panel.tsx` | 人数 / 已用空间 / 条目 / 今日写入 + 写入位置 + 写入趋势 + 命名空间占用 + 用量最高的用户 |
| `components/apps/cloud-storage/cloud-storage-users-panel.tsx` | 用过云存储的用户：用量条、条目 / 回收站、冻结；点进用户详情的「云存储」页签 |
| `components/apps/cloud-storage/cloud-storage-config-panel.tsx` | 开关与存储配置 + 配额与限制 + 命名空间目录 + 手动清理过期回收站 |
| `components/apps/cloud-storage/cloud-storage-shared.tsx` | 字节格式化（1024 进位）、配额条、提供商名称、编码与修订来源文案 |
| `components/users/detail/user-cloud-tab.tsx` | 用户详情「云存储」页签：配额覆盖 / 冻结 / 备注、条目浏览、内容与修订、回滚 / 删除 / 恢复、清空 |

三条硬约束：

1. **单个用户的管理只在用户详情里。** 区块里的「用户」视图只列人和用量，条目与修订在
   `/app-users/{appKey}/{userId}?tab=cloud`。管理员处理一个人的问题时，资料、钱包、云存储在同一页。
2. **字节一律按 1024 进位。** 后端的默认配额与上限都是 2 的幂，按 1000 进位会把「20 MB」显示成 20.97 MB。
   表单里的配额以 MB 输入，提交前换算成字节。
3. **存储配置下拉按名称去重。** 后端按名称解析（应用级优先于平台级），同名的两项是同一个选择；
   目标存储是公开访问时，概览与配置两处都要提示。

写入、修订、配额与回收站的语义全部由后端决定，见 [docs/cloud-storage.md](../docs/cloud-storage.md)。

## 存储中心（/storage）

后端契约见 [docs/storage.md](../docs/storage.md)。页面按 `?tab=` 切十个面板，**键沿用旧页面**
（`global` 是「存储源」、`app` 是「应用存储」），已分享的深链不失效；`overview` 是默认页。

| 文件 | 职责 |
|---|---|
| `lib/api/storage-center.ts` / `lib/storage-center-hooks.ts` | 存储源、提供商目录、总览、池、事件、检测、CORS、转向、迁移的 API 与 hooks（写操作整组失效） |
| `components/storage/storage-shared.tsx` | 品牌徽章（Simple Icons，未收录用中性图标）、状态徽标、容量条、策略 / 角色元信息、MB/GB/TB 换算 |
| `storage-overview-panel.tsx` | 汇总、异常告警、各存储用量图、进行中的迁移、最近事件、池概况 |
| `storage-sources-panel.tsx` + `storage-source-sheet.tsx` + `storage-source-dialogs.tsx` | 存储源卡片网格（平台级 / 应用级共用）、目录驱动的新建与编辑、深度检测、CORS |
| `storage-pools-panel.tsx` + `storage-pool-editor.tsx` | 池卡片（成员容量与状态、恢复写入）、模拟写入、编辑器（拖动排序即优先级） |
| `storage-cloudflare-wizard.tsx` | Cloudflare 快速接入：连接令牌 → 选账户与桶（可新建）→ 凭据 / 访问 / CORS / 池 → 逐步结果；R2 卡片的「更新凭据」复用它 |
| `storage-migrations-panel.tsx` / `storage-events-panel.tsx` | 迁移任务与读取转向；运行事件时间线（概览复用紧凑版） |
| `storage-rules-panel.tsx` / `storage-cdn-panel.tsx` / `storage-usage-panel.tsx` | 结构化上传规则、CDN 与防盗链、用量趋势与类型分布 |
| `file-manager.tsx` 等 | 文件管理（未改动） |

六条硬约束：

1. **表单由后端目录驱动。** 字段、密钥标记、能力、品牌色都来自 `GET /api/admin/app/storage-config/providers`；
   新增提供商只改后端，这里零改动。不要在前端再抄一份字段表。
2. **密钥一律留空即不修改。** 存储源的密钥、CDN 签名密钥出网已被抹除（`secrets_set` / `signUrlSecretSet`
   只告诉你配没配过），编辑表单显示「已配置，留空不修改」，提交原样带空串。
3. **成员顺序就是优先级。** 编辑器按列表顺序写 `priority = index × 10`，不另给优先级输入框。
4. **轮询跟着状态走。** 总览在概览 / 存储池页 5 秒一刷、其余 30 秒；迁移列表只在有任务排队或进行中时 2 秒一刷。
5. **Cloudflare 令牌只放组件状态。** 不进 localStorage、不进查询键（键会出现在开发工具里），关闭向导即丢弃。
6. **池只收平台级存储源。** 应用存储页不出现池相关的东西；成员下拉只列 `scope = global` 的源。

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
| `components/users/detail/user-governance-tab.tsx` | 账号开关 / 新建封禁 / 封禁历史与撤销 / 账号注销 / 删除 |
| `components/users/detail/user-account-setup.tsx` | 改用户名弹窗（资料页签账号字段与安全页签共用，先输入再确认）+ 安全页签「账号初始化」区块（进度、要求重新设置） |
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

## 地图底图：多供应商 + 跟随浏览器语言

五个面板（用户活动地图 / 攻击飞线图 / 地理热力 / 围栏编辑 / 轨迹回放）共用
`components/maps/maplibre-map.tsx` 这一个底座，它们只管往上挂自己的图层，
**底图是哪一家、什么坐标系、注记什么语言，一律不需要知道**。

| 文件 | 职责 |
|---|---|
| `lib/geo/map-providers.ts` | **供应商目录（单一事实源）**：瓦片地址 / 坐标系 / 深色版 / 注记语言 / 密钥 / 版权，外加浏览器语言解析 |
| `lib/geo/map-provider-store.ts` | 偏好存储（`useSyncExternalStore`），全站一份、跨标签页同步、旧键自动迁移 |
| `lib/geo/gcj02-tiles.ts` | GCJ-02 瓦片纠偏（maplibre 自定义协议） |
| `lib/geo/datum.ts` | WGS-84 ⇄ GCJ-02 转换与中国境内判定 |
| `components/maps/map-provider-picker.tsx` | 选择器：自动档 + 按大区分组 + 缺密钥说明 |
| `components/maps/maplibre-map.tsx` | 底座：矢量底衬、在线瓦片装卸、纠偏闸门、失败回退、版权署名 |
| `scripts/map-providers.test.mjs` | 语言规则、供应商解析、坐标基准与目录完整性由 `bun run test` 钉住 |

13 家分三档：**离线**（本地矢量简图）、**全球**（CARTO / OSM / Esri 灰底 / Esri 卫星 /
OpenTopoMap / MapTiler✱ / Stadia✱）、**中国大陆**（高德 / 高德卫星 / 腾讯 / 天地图✱ /
天地图影像✱）。带 ✱ 的需要密钥（`NEXT_PUBLIC_MAP_*_KEY`，见 `.env.example`），
**未配置时照常列出但不可选，并直接写出要配哪个环境变量** —— 藏起来的话，
部署者永远不会知道自己还能多几家可选。

六条硬约束：

1. **供应商目录是单一事实源。** 渲染端与选择器都只读它，任何一处再抄一份，
   就会出现「选得出来但画不上去」或者「能画但选不到」—— 与风控条件目录、
   远程函数能力目录同源的约束。
2. **默认跟随浏览器语言，手动选过就锁定。** 简体中文（`zh-CN` / `zh-Hans` / 裸 `zh`）
   走境内档，其余走全球档；繁体中文要中文注记但仍归全球档 —— 走境内线路只会更慢。
   一个人一旦发现某家在自己网络下更快，这个结论不该被自动逻辑推翻。
3. **偏的是瓦片，不是数据。** 中国大陆的地图服务只提供 GCJ-02 偏移底图，与平台里
   GeoIP 的 WGS-84 坐标差 300–700 米。纠偏放在瓦片管线里做（取来相邻源瓦片按偏移量
   重绘），业务层一个坐标都不用改。另一种修法是把数据转成 GCJ-02 再画 ——
   那要在五个面板约四十处取坐标的地方各转一次，围栏面板还得在鼠标点击时反向转回去，
   **漏掉任何一处都不会报错，只是那一层悄悄错位**。
4. **纠偏有闸门**：只在 `z ≥ 8` 且瓦片中心在境内时启用（低层级偏移不足半个像素），
   退出阈值 `7.5` 形成滞回。控制台默认是全球视野，也就是说绝大多数时候这段代码不执行。
   瓦片服务没开 CORS 时读不到像素，一次失败即全局降级为未纠偏，并在图上如实标注。
5. **换供应商 / 换主题只增删 `aegis-tiles-*` 图层，永远不调 `setStyle`。**
   业务面板在 `onMapReady` 里挂的 source / layer 一旦被 `setStyle` 清掉，
   热力、围栏、轨迹会整层消失，而地图看起来完全正常。新瓦片层一律插在
   **第一个业务图层之前**，否则底图会盖住数据。
6. **本地矢量简图恒在最底层。** 在线瓦片被墙、超时、密钥失效时透出来的是一张配色
   正确的世界地图，而不是纯黑画布。连续 8 次瓦片错误即判定该家不可达、收起在线图层，
   角标可点重试（网络抖动之后没有这个入口就再也回不来了）。

没有原生深色版的供应商（OSM / 高德 / 腾讯 / 天地图矢量）在深色主题下用栅格 paint
压暗近似。这**只是近似** —— 栅格没有反相能力，压出来是「黄昏」不是真正的暗色图；
但深色控制台里放一张纯白地图会直接晃眼，且选择器里标了原生深色的几家随时可以换过去。

## 通知铃铛与实时事件

顶栏铃铛（`components/layout/topbar/notification-bell.tsx`）合并两个来源：

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

## 远程函数（/functions）

三个页签同步到 URL（`?tab=functions|kv|keys`）：

| 文件 | 职责 |
|---|---|
| `(console)/functions/page.tsx` | 页面骨架、应用选择、Tab 路由（**满屏**，见下） |
| `components/functions/function-manager.tsx` | 左侧函数列表（可折叠 / 可搜索）+ 四个面板的装配 |
| `components/ui/resizable.tsx` | 可拖拽分栏与布局记忆（`usePanelLayout`），本页全部分隔线都出自这里 |
| `components/functions/function-create-dialog.tsx` | 新建：运行时 / 起始模板 / 能力 |
| `components/functions/function-overview-panel.tsx` | 运行状况：成功率 / 耗时分位 / 趋势 / Top 错误 |
| `components/functions/function-editor-panel.tsx` | 脚本工作台：写 → 检查 → 试跑 → 发布 → 回滚 |
| `components/functions/function-invocations-panel.tsx` | 调用审计（筛选 + 分页 + 详情抽屉）与真实调用 |
| `components/functions/function-settings-panel.tsx` | 能力、运行闸门、函数配置、删除 |
| `components/functions/function-kv-panel.tsx` | KV 浏览器（脚本的服务端独占状态） |
| `components/functions/function-shared.tsx` | 能力勾选树、风险徽标、副作用清单、格式化 |
| `components/functions/script-editor.tsx` | Monaco：SDK 类型 + 诊断标记 + 差异视图 + 快捷键 |
| `components/functions/json-editor.tsx` | 带 JSON Schema 的 JSON 编辑器（试跑 input / 函数配置 / 入参契约共用） |
| `lib/monaco/aegis-language.ts` | 平台语义的 provider：悬浮 / 配置补全 / 快速修复 / Code Lens |
| `lib/monaco/aegis-theme.ts` | 把设计令牌喂给 `defineTheme`，语法色复用 `--hljs-*` |
| `lib/monaco/editor-motion.ts` | 动效档位（呼吸光标 / 插入符滑动 / 惯性滚动）+ 两道闸门 |
| `lib/monaco/aegis-snippets.ts` | 代码片段（额度 / 加锁 / 验签 / 金额 / JWT 等固定写法） |
| `lib/monaco/json-schema-meta.ts` | 入参契约编辑器自己的元 schema |
| `lib/function-workbench-store.ts` | 本地状态：未发布草稿、试跑用例、编辑器偏好 |
| `lib/api/app-functions.ts` / `lib/function-hooks.ts` | 远程函数域 API 与 React Query hooks |

### 一屏工作台：不靠滚动做完一次迭代

整页高度锁在 `workbench-viewport`（`globals.css`：视口 − 顶栏 − `<main>` 留白），
滚动条一律长在各面板自己的内容区上。**页面级滚动在这里是有害的**：编辑器一旦被
卷出屏幕，写脚本就变成「滚下去点试跑、滚回来改代码、再滚下去看结果」，
而屏幕右边整块是空的。旧形状正是如此 —— 编辑器写死 460px，
下面依次堆着诊断、试跑、结果、版本四张卡片。

```
┌ 页签行（函数 / 键值存储 / 调用密钥）· 应用选择器 · 接入文档 ┐
├──────────┬────────────────────────────────────────────────┤
│ 函数列表  │ 函数名 · 状态 · 不可被调用 │ 脚本 概览 调用 设置 │
│ 搜索 + 新 ├──────────────────────────┬─────────────────────┤
│ 建 + 草稿 │ 工具条（检查 / 格式化 …）  │                     │
│ 标记      ├──────────────────────────┤  试跑输入            │
│          │                          │  （JSON + 身份 + 用例）│
│          │  Monaco（占满剩余空间）    ├─────────────────────┤
│          ├──────────────────────────┤  试跑结果            │
│          │ 问题 │ 版本（可整块收起）  │  （返回值/日志/副作用）│
├──────────┴──────────────────────────┴─────────────────────┤
│ 状态栏：行列 · 能力 · 单次额度 · 正文大小 · 草稿时间 · 快捷键 │
└───────────────────────────────────────────────────────────┘
```

六条硬约束：

1. **四个面板仍然是四件事，不要合回一屏。** 一屏说的是「脚本这件事在一屏内做完」，
   不是「把概览、调用、设置也塞进来」。默认落在**脚本**上（进来十次有九次是改逻辑），
   而「为什么调不通」这条结论压缩成了函数名旁边那枚**不可被调用**徽标，
   点它跳概览 —— 不是把整页图表提到前面来。
2. **分栏尺寸落 localStorage，且只存用户拖出来的那一次**
   （`usePanelLayout` 里 `onlySaveAfterUserInteractions: true`）。
   窗口缩放、折叠、约束重算也会产生布局变化，一并存下来会让「我上次拖的比例」
   被一次窗口缩放悄悄改写。
3. **`useDefaultLayout` 必须显式传 storage。** 它的默认值是裸 `localStorage`，
   而那个默认值在调用时求值 —— 服务端渲染那一趟直接 ReferenceError、整页白屏。
   `resizable.tsx` 里的包装已经处理，**不要绕过 `usePanelLayout` 直接调库**。
4. **`ResizablePanel` 的 `overflow` 只能走 `style`。** 库把 `overflow: auto`
   写成了内联样式，类名压不过它；这里默认改成 `hidden`，需要滚动的内容自己
   再套一层 `overflow-y-auto`。
5. **Monaco 一律 `height="100%"` + 外层 `min-h-0 flex-1`**，不要再写死像素高度：
   那个数字在 13 寸上太高、在 27 寸上浪费掉半屏。`automaticLayout` 已经开着，
   拖分隔线时编辑器会自己重排。
6. **两个 dock 可以整块收起，但收起后不能失去入口。** 工具条上那两个开关是唯一的
   回来路径，检查徽标也可点（直接展开底部 dock 并切到「问题」）——
   否则收起一次就再也找不到诊断了。

### 「完整的 LSP 体验」具体指什么

Monaco 内置的 TypeScript worker **就是 tsserver 本体**：补全、悬浮、签名帮助、
诊断、重命名、跳转定义、引用查找、大纲、折叠本来就都在，不需要另起进程。
所以这件事不是「接一个语言服务器」，而是两件别的事：

1. **把默认没开的开关打开** —— inlay hints、语义高亮、code lens、灯泡、
   sticky scroll、同名高亮、联动编辑。
2. **补上 TypeScript 无从得知的平台语义**，这才是重点。TS 不知道
   「`aegis.points` 存不存在取决于勾了哪些能力」、不知道
   「`aegis.config.dailyQuota` 现在是多少」、也不知道「这行缺 points.write」。
   这三类分别由 hover / completion / codeAction provider 补，
   数据来自后端能力目录、函数配置与静态检查结论。

### 编辑器的观感：主题与动效

**配色接设计令牌**（`lib/monaco/aegis-theme.ts`）。内置的 `vs` / `vs-dark`
各自带一套写死的背景（`#fffffe` / `#1e1e1e`），与控制台的 zinc 差着一档 ——
表现是编辑器像一块从别处贴过来的白板。语法着色复用 `--hljs-*`，
于是同一段脚本在开发者门户和在编辑器里长得一模一样。

| 约束 | 理由 |
|---|---|
| 颜色必须规范化成 `#rrggbb`（走 canvas） | Monaco 内部是 `parseHex`，给它 `oklch()` / `color-mix()` 会**静默失效**，那一项颜色不生效也不报错 |
| token 规则里的颜色**不带 `#`** | Monaco 自己会拼；带了会变成 `##rrggbb` |
| `colors` 表里的颜色**必须带 `#`** | 与上一条相反，这是同一份 API 里的两套约定 |
| 主题只有一个名字，深浅切换时就地重定义 | 令牌是从 `<html>` 现读的，读的那一刻 `.dark` 已经在了；重定义放在 `requestAnimationFrame` 里，同帧读可能还是上一套值 |
| `inherit: true` | 内置主题有几百条我们没覆盖的规则，从零写一份的代价是某个语言的某类 token 突然变纯黑 |

**动效**（`lib/monaco/editor-motion.ts`）：呼吸光标（`cursorBlinking: "phase"`）、
插入符滑动（`cursorSmoothCaretAnimation: "explicit"`）、惯性滚动。两道闸门：

- **系统级 `prefers-reduced-motion` 有否决权**。一个持续呼吸的光标正是这条偏好
  要挡的东西，用户把开关打开也不能盖过它。
- **用户级开关**（编辑器工具条 → 平滑动效）管的是**性能**，不是无障碍：
  低端机上平滑插入符会掉帧，而掉帧的光标比不动的光标更难看。

CSS 侧（`globals.css` 末段）只补 Monaco 选项表达不了的三件事，且都在
`prefers-reduced-motion: no-preference` 里：光标圆角、把呼吸的
`animation-iteration-count` 从 `20` 改成 `infinite`（默认空闲十秒就停在常亮，
而 macOS 只要还聚焦就一直在呼吸）、以及补全 / 悬浮 / 参数提示三种浮层的淡入。

**不要给 `.aegis-code-editor *` 的 `transition-property: none` 加 `!important`。**
那条是用来免掉全局 280ms 颜色过渡的（主题切换时 Monaco 自己会重绘整棵 DOM，
再叠一层补间会明显卡顿），特指度 (0,1,0) 压得过全局的 `*`，但压不过 Monaco 自己那条
(0,3,0) 的 `.cursors-layer.cursor-smooth-caret-animation>.cursor{transition:all 80ms}` ——
而那正是插入符滑动的实现。加了 `!important` 就把要开的功能一起关掉了。

十条硬约束：

1. **能力目录来自后端**（`GET /function-catalog`），不在前端硬编码。
   在这里另抄一份会同时招来「后端加了一项、控制台勾不上」和「控制台能勾、
   保存时报不支持」两种漂移，而两边都没有报错提示 —— 与风控条件目录同源。
2. **编辑器的类型声明也来自那份目录**。`aegis-sdk-types.ts` 只负责按已声明能力
   过滤并拼装，声明片段本身在 Go 里。前端自己写一份类型的后果是
   「补全里有、运行时没有」，而那要到发版之后才暴露。
   拼装时**必须合并命名空间**（`user.read` 出 `get`、`user.write` 出 `ban`，同属
   `aegis.user`）：漏了会在同一个接口里产生两个 `user:` 成员，TypeScript 报重复
   声明后整份类型静默失效，表现是补全突然什么都没有。
3. **四个面板对应四件不同的事**，不要合并：概览回答「为什么调不通」，
   脚本负责迭代，调用负责排障，设置负责闸门。再往前一版最大的问题不是拥挤，
   是**没有试跑** —— 唯一的验证方式是把半成品激活到线上。
   （「一屏」指的是脚本这件事在一屏内做完，见上一节，与这条不冲突。）
4. **试跑失败不是接口错误**。后端在脚本执行失败时仍返回 200，判成功要看
   `result.ok`；按抛异常处理会把日志与副作用清单一起丢掉，而那正是要看的东西。
   试跑产生的 effect 必须显示「未执行」标记，否则一份「发了 100 积分」的清单
   会被当成真的发生过。
5. **草稿按 (应用, 函数) 绑定**，切换函数时靠 `key` 整块重挂载，不用 effect 同步 ——
   与配置面板、门户凭据同一条约束。草稿落 localStorage（`function-workbench-store`）：
   误刷新一次就清零半小时的改动，是这个界面上最常见也最没必要的一次损失。
   但它**不落服务端** —— 那要回答「谁能看见、两个人同时改怎么办、什么时候清理」，
   而它的实际寿命通常是几分钟。
6. **静态检查跟着正文自动重跑**（防抖 600ms），不做成「点一下检查」按钮：
   让作者主动去点，等于回到「发布被拦才知道有问题」。检查失败时**保留上一次结果**
   而不是清空 —— 网络抖动不该在编辑器上留一片红。
   「全绿」也要显式说出来：什么都不显示时，作者分不出「检查通过」与「检查没跑」，
   而这两种情况下他该做的事完全相反。
7. **zustand selector 的兜底值必须是模块级常量**（`state.testCases[scope] ?? NO_TEST_CASES`）。
   写成 `?? []` 会让 selector 每次返回新引用，zustand v5 走 `useSyncExternalStore`，
   直接抛「getSnapshot should be cached」并把组件打进无限重渲染。
8. **`ctx.input` 的类型由后端从 JSON Schema 生成后下发**（`AppFunction.inputTypes`），
   前端不做这个转换。再写一个转换器就有了第二份真相，而两份类型不一致的表现
   仍然是那句「补全里有、运行时没有」—— 与能力片段同一条约束。
9. **Monaco 的 provider 注册是全局的**，组件重挂载时不 dispose 会让同一个补全项
   出现两遍、三遍。且 provider 的上下文必须传 **getter 而不是值**：回调被 Monaco
   长期持有，闭包捕获会让它永远看到首次注册那一刻的能力与配置 ——
   表现是勾了新能力之后，悬浮提示里它还是「未声明」。
10. **每个 `JsonEditor` 实例必须有唯一的 model URI**（用 `useId`，不要用随机数）。
    schema 是按 `fileMatch` 绑到 URI 上的，两个实例共用一个 URI 会让后挂载的那份
    覆盖前一份，表现是「函数配置编辑器在按入参契约校验」。
    另外 `setDiagnosticsOptions` 是**整体覆盖式**的，注册自己那条时必须读改写，
    直接传一条新的会把页面上其它 JSON 编辑器的 schema 全部抹掉 —— 而那种失效
    没有任何报错，补全只是「不出来了」。

## Monaco 编辑器（JsonViewer / 远程函数脚本 / 插件 Expr）

Monaco 产物**自托管**在 `public/monaco/vs`，由 `scripts/sync-monaco-assets.mjs`
从 `node_modules/monaco-editor` 同步（`predev` / `prebuild` 自动执行，也可 `bun run monaco:sync`）。
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

<!-- BEGIN:nextjs-agent-rules -->

# This is NOT the Next.js you know

This version has breaking changes — APIs, conventions, and file structure may all differ from your training data. Read the relevant guide in `node_modules/next/dist/docs/` (resolved from this file's directory; in monorepos the `next` package may not be visible from the repo root) before writing any code. Heed deprecation notices.

This block is written and re-added by `next dev` — verify at `node_modules/next/dist/server/lib/generate-agent-files.js`. Removing it from a diff only re-creates the uncommitted change; committing it with your work keeps the tree clean.

<!-- END:nextjs-agent-rules -->
