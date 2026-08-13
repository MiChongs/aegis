# aegis-console — 管理前端

> 面包屑：[Aegis](../CLAUDE.md) › aegis-console

## 技术栈

| 技术 | 版本 |
|---|---|
| Next.js | ^16.3.0 |
| React | 19.2.8 |
| TypeScript | 7.0.2（编译器）+ 6.0.2（工具链 API，见下） |
| Tailwind CSS | ^4.3.3 |
| shadcn/ui | new-york-v4，40 个官方组件均为最新版（见下方扩展说明） |
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
| screenfull | ^6.0.2（顶栏全屏开关；跨浏览器前缀差异交给它，见下方顶栏一节） |
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

`src/components/ui/` 下另有 12 个**项目自有组件**（非 shadcn，CLI 不会碰）：
`brand-icon`、`country-flag`、`data-state`、`error-boundary`、`image-dropzone`、`image-lightbox`、
`json-viewer`、`rich-editor`、`section-heading`、`surface-card`、`toast-detail`、`virtual-list`。

补装组件用 `pnpm dlx shadcn@latest add <name>`，**永远不要带 `--overwrite`** ——
上表四项扩展会被一次性抹掉，而覆盖是静默的。CLI 对已存在且内容一致的文件会自己跳过。

> `ui/` 里不留没有任何页面用到的组件。官方注册表有 63 个 UI 项，装进来的判据是
> "现在就有地方用"，而不是"以后可能用得上" —— 后者的结果是没人知道哪些还活着。

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
| `pnpm typecheck`（`tsc --noEmit`） | **TS 7.0.2** | 权威类型校验，`pnpm build` 的第一步就是它 |
| `pnpm typecheck:ts6`（`tsc6 --noEmit`） | TS 6.0.2 | 对照校验 |
| `require("typescript")`（typescript-eslint） | TS 6.0.2 | 两个包的 bin 名不冲突（`tsc` / `tsc6`） |
| `next build` 期类型检查 | **不做**（`typescript.ignoreBuildErrors`） | 见下 |

**build 期不再做类型检查**：Next 内建的那一遍走 TS 6 的 JS API，而 TS 7 是 Go 重写的
原生编译器，同一份代码实测差接近一个数量级（当时是 17s vs 2.4s）—— 两者查的是同一件事，
留慢的那个没有意义。因此 `next.config.ts` 里 `typescript.ignoreBuildErrors: true`，
把这道关口前移成 `build` 脚本的第一步 `tsc --noEmit`：类型不过一样构建不出来，
而且报错来得更早（不用等 Turbopack 编译完）。**改 `build` 脚本时不要把它去掉**，
仓库目前没有前端 CI，去掉之后就真的没有任何地方在检查类型了。

**`next.config.ts` 中的 `experimental.useTypeScriptCli: false` 同样不可删除**：Next 16.3 该项默认为 `true`，
CLI 模式会去找 `typescript/bin/tsc`，而 TS 6 兼容包只提供 `bin/tsc6`，Next 会误判「typescript 未安装」
并自动执行 `pnpm install --save-dev typescript`，把上面的 alias 覆盖掉。
`ignoreBuildErrors` 只是不做检查，Next 仍会探测这个包，所以两项要同时在。

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
| `brand/home/sponsors-section.tsx` | 赞助商跑马灯（`kibo-ui/marquee`，两行反向 + 渐变模糊边缘） |
| `brand/sponsors/brand-logo.tsx` | 品牌标识渲染：图形标 + 字标，尺寸由字号决定，颜色走 `currentColor` |
| `brand/sponsors/brand-logos.generated.ts` | **生成产物**，由 `pnpm logos:sync` 从 `@lobehub/icons-static-svg` 抽出 |
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
| `--home-vignette` / `--home-grain-opacity` | 中性 | 暗角与胶片颗粒，**取代**了原来的发光球 |
| `--home-intro-*` | 墨底 / 纸白 / 暖铜 | 冷开场专用，不随主题切换（见下） |

| 原语（`home/visuals.tsx`） | 用在哪 |
|---|---|
| `Pattern` | 网格 / 点阵底纹，靠 mask 在边缘溶解 |
| `Grain` | 胶片颗粒（内联 SVG `feTurbulence`，不额外请求图片） |
| `Vignette` | 中性暗角，把视线压回版心 |
| `SpotlightCard` | 鼠标跟随高光（能力卡、架构卡、全景条目） |
| `Marquee` | 首屏底部的技术栈跑马灯（与赞助商那套是两回事，见下节） |
| `MaskLine` / `SplitChars` / `Typewriter` | 文字动画：遮罩逐行揭示 / 逐字入场 / 打字机 |
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

### 冷开场（`home/intro-overlay.tsx`）

一块墨底压在整页之上，三拍报出三个能力域，落版是产品定位，然后整层淡出露出首屏。
三拍与首屏的三列能力域**是同一份目录**，所以这段动画是内容的一部分，
而不是内容前面的一段广告。

一个每次进站都要看完的开场，第二次就变成了阻塞。四条闸门缺一不可：

1. **每个会话只播一次**（`sessionStorage`）。第二次进来直接是首屏。
2. **随时可跳过**：任意键 / 点击 / 滚动 / 触摸，外加一个始终可见的跳过按钮。
3. **进度条必须看得见。** 等待可以忍受的前提是知道还剩多久；
   一个不知道什么时候结束的黑屏，第三秒就会被当成页面挂了。
4. **`prefers-reduced-motion` 下根本不挂载。**

三条实现约束：

- **首屏内容始终在 DOM 里**，这一层只是盖在上面。爬虫与读屏软件看到的是完整页面。
- **"本会话是否播过"用惰性 `useState` 初始化器读取**（纯读，StrictMode 双调用返回同值），
  写入放在 effect 里。渲染期写存储会在 StrictMode 下执行两次。
  组件整体由 `useIsClient()` 把关，水合时两端都渲染 `null`，因此没有水合不一致。
- **推进节拍的 `setState` 在 `setTimeout` 回调里**（异步），不是 effect 体内的同步调用 ——
  后者过不了 `react-hooks/set-state-in-effect`，与全站同一条约束。

### 赞助商跑马灯

第 3 条（动效只有一种）在这里有唯一一处例外：跑马灯本身就是这个分区的形态。
它同样受 `prefers-reduced-motion` 约束 —— 命中时 `play={false}` 直接停住，
名单仍然完整可读。

| 部件 | 来源 |
|---|---|
| 跑马灯 | shadcn 注册表 `@kibo-ui/marquee`（底层 `react-fast-marquee`，`autoFill` 自动按容器宽度补足份数），三行逐行反向、行速互不相同 |
| 边缘处理 | `MarqueeFade` + 单层 arbitrary `mask-image` |
| 品牌标识 | `@lobehub/icons-static-svg`（零依赖 SVG 资源包）经 `pnpm logos:sync` 抽成内联标记 |
| 单条目 | shadcn `Item` + `Tooltip`（品牌名 + 领域，已内置适配的另说明它在 Aegis 里承担什么） |

六条约束，每一条都是踩过的坑：

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
4. **模糊必须跟着 mask 一起衰减。** 只给 `backdrop-blur` 不给 mask，会在渐变结束的
   位置留下一条"清晰度突变"的竖线，比不加模糊更显眼。
5. **每行要占位高度。** `react-fast-marquee` 挂载前返回 `null`（它靠测量容器宽度
   决定复制几份），不占位的话页面会在水合那一刻整体上跳。`ROW_HEIGHT` 同时钉住
   跑马灯容器与条目，两者必须一致。
   同一个原因：**这一排不进服务端 HTML**，水合后才出现。
6. **彩色版主体是白色的品牌要退回单色版**（生成器里的 `mono: true`）。
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
pnpm dev          # 开发服务器（自动清理上次产物，保留编译缓存）
pnpm build        # 生产构建 = tsc --noEmit && next build
pnpm start        # 生产启动
pnpm typecheck    # tsc --noEmit
pnpm lint         # ESLint
pnpm test         # node --test（启动期脚本 + 底图供应商目录与坐标基准）
pnpm clean        # 清理 .next，连编译缓存一起（怀疑缓存坏了时用这个）
```

### 构建缓存：`.next/cache` 不要删

`predev` / `prebuild` 会清掉上一次的产物，但 `scripts/clean-next.mjs` **刻意跳过 `.next/cache`** ——
那里是 Turbopack 的文件系统缓存（Next 16 起默认开启，本项目约 280MB）。
连它一起删的代价实测是编译阶段 **2.7s → 10.8s**，而清理本来只是想要一份干净的产物，
残留旧内容的是 `server` / `static` / `dev` 那几个目录，缓存自己带版本标记、Next 会判失效。

`pnpm clean` 走 `--all`，连缓存一起删：手动清理的场景恰恰是怀疑缓存坏了。

`next build` 会把各阶段耗时逐行打出来（编译 / 静态生成），`tsc` 那一步单独计时即可。
构建变慢时先看是哪一段涨的，再决定查什么 —— 缓存被清掉表现为编译阶段成倍变长。

### `turbopack.root` 只能指本目录

`pnpm-lock.yaml` 与 `pnpm-workspace.yaml` 都在 `aegis-console/` 下（原因见
`pnpm-workspace.yaml` 里的说明），所以 `next.config.ts` 里的 root 就是
`import.meta.dirname`。曾经写成 `../../`（**仓库之外**的 `userSystem/`），
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

四条硬约束：

1. **`AEGIS_API_BACKEND` 是构建期烘死的，不是运行期读的。** `rewrites()` 在 build 时
   求值，结果序列化进 `.next/routes-manifest.json`。所以换后端地址必须重新构建镜像，
   改容器环境变量没有任何作用；而漏传 build arg 会落到默认的 `127.0.0.1:8088` ——
   控制台反代到它自己。它还必须填**内网**地址，理由见下一节。
2. **`CMD` 里的 `--import=./scripts/forwarded-headers-preload.mjs` 不能省。**
   standalone 模式跑的是 `node server.js`，不再经过 `package.json` 的 `start` 脚本，
   预载得自己带上。少了它容器照常起、页面照常开，只是全站客户端 IP 都变成控制台自己。
3. **那两个预载脚本和 `ipaddr.js` 要显式列进 `outputFileTracingIncludes`。**
   文件追踪是从应用代码出发的，够不到「由 `node --import` 装载」的脚本；
   而 `outputFileTracingIncludes` 只**复制**列出的文件、不会再追它们自己的 import，
   所以 `ipaddr.js` 这个依赖也得手写一条。
4. **`.next/static` 与 `public/` 要自己搬进运行阶段**，standalone 产物不含它们。
   `public/` 里有自托管的 monaco（23MB），漏搬的表现是脚本编辑器打不开。

`output: "standalone"` 由 `NEXT_OUTPUT=standalone` 开启，平时本地 build 不开 ——
它要对整棵依赖树做文件追踪，而那份产物本地用不上。

## 同源反代与客户端 IP 透传

`/api/*`、`/openapi.json`、`/healthz`、`/readyz` 由 `next.config.ts` 的 rewrites
同源反代到 `AEGIS_API_BACKEND`。浏览器只看到相对路径，后端 host 不外露。

代价是链路上多了一跳，而 **Next 内置的那个代理不追加转发头**
（`proxy-request.js` 只手工塞了 `x-forwarded-host`，httpxy 的 `xfwd` 没开）。
后端的限流 / 封禁 / 地理风控 / 审计全部建立在客户端 IP 上，缺了这一跳的事实，
本机开发时全站请求都是 `127.0.0.1`，线上则完全押在入口反代写的那条 XFF 上。

因此启动时会预载一段逻辑，在**自己的 HTTP 服务器边界**把「本进程看到的直连对端」
追加到 `X-Forwarded-For` 末尾（上游用了 `Forwarded` 就一并续写）：

| 文件 | 职责 |
|---|---|
| `scripts/forwarded-headers.mjs` | 追加逻辑 + 后端跳判定 + `withForwardedPreload()`；行为由 `pnpm test` 钉住 |
| `scripts/forwarded-headers-preload.mjs` | `--import` 预载入口，改写 `http.createServer` |

**但追加是必要不充分的：`AEGIS_API_BACKEND` 必须指向后端的内网地址。**
填公网域名的话，这一跳绕出公网再回来，途中的 CDN / 网关会如实写下「连接方是控制台」，
而那是个公网地址、后端不信任它，判定就正确地停在那里 —— 全站每个用户的每个请求
都收敛成控制台的出口地址，控制台在自己那侧写的条目在链的更左边，永远够不着。
放宽后端 `TRUSTED_PROXIES` 不是解法（等于把伪造权发给任何能直连后端的人）。
启动时 `describeBackendHop` 会就此打一条 warn —— 这个错误在功能上完全看不出来。

五条硬约束：

1. **装载点有两处，形式不同不是随手写的。** `pnpm start` 在命令行上 `--import`；
   `pnpm dev` 必须走 `NODE_OPTIONS`（`scripts/dev-with-friendly-proxy.mjs` 里的
   `withForwardedPreload`）—— `next dev` 会 fork 出真正的服务器进程，且那一层的
   `execArgv` 由它自己按 `NODE_OPTIONS` 重拼，命令行上的 `--import` 传不过去。
2. **只追加，不判断谁可信，也不加任何环境变量。** 受信网段只有后端
   `TRUSTED_PROXIES` 一个入口；这里再配一份，「到底谁说了算」就没有答案了。
   追加而非覆盖也是刻意的：客户端伪造的条目留在链的左边，后端从右往左走时
   先遇到我们写的这条真事实。
3. **不要改成中间件（`proxy.ts`）或 Route Handler。** 它们拿到的是 Web `Request`，
   看不到 socket（`NextRequest.ip` 在 Next 15 已移除），没有对端地址可写。
   而那个 `http.Server` 由 Next 自己创建并持有，外部拿不到实例。
4. **`installOnServer` 钩的是 `server.emit`，不是 `prependListener('upgrade')`。**
   给一个本来没有 upgrade 监听器的 server 装上监听器，Node 就不再自动销毁升级
   连接了 —— 那会把「没人处理的 WebSocket 握手」从立刻断开变成一直挂着。
5. **地址的解析与归类走 `ipaddr.js`**（Express 的 `trust proxy` 底下经 proxy-addr
   用的就是它），不要手写。要判的两件事都不适合自己抄一份：IPv4 映射地址还原，
   以及「环回 / RFC1918 / CGNAT / IPv6 ULA / 公网单播」的归类 —— 后者是一组
   记不住的网段，抄一份的下场是它慢慢过期，而过期不会报错。

启动时打的那行 `▲ 客户端 IP 透传已启用…` 是这件事**唯一的自检线索**：托管平台若
绕过 `package.json` 的脚本直接跑 `next start`，预载不执行，而少一条转发链条目
在功能上完全看不出来。完整说明见 [docs/client-ip.md](../docs/client-ip.md#控制台反代这一跳)。

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
| `components/apps/card-key/` | 卡密区块：批次 / 卡密 / 核销记录（权益表单由后端目录驱动） |
| `lib/api/card-key.ts` / `lib/card-key-hooks.ts` | 卡密域 API 与 React Query hooks |
| `lib/api/vip.ts` / `lib/vip-hooks.ts` | 会员域 API 与 React Query hooks |

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

## 会员区块（/apps/{appKey}?tab=vip）

| 文件 | 职责 |
|---|---|
| `components/apps/vip/app-vip-panel.tsx` | 骨架 + 四个视图的内部切换 |
| `components/apps/vip/vip-plans-panel.tsx` | 套餐：付费与试用**分区**列出、逐条启停与删除 |
| `components/apps/vip/vip-plan-editor.tsx` | 套餐编辑抽屉（种类 / 时长 / 价格 / 功能勾选 / 设备限制） |
| `components/apps/vip/vip-features-panel.tsx` | 功能标识目录：建 / 停用 / 删除，并说清被哪些套餐用着 |
| `components/apps/vip/vip-trial-panel.tsx` | 试用：汇总（累计 / 试用中 / **已转化** / 转化率）+ 领取记录 + 代领 + 恢复资格 |
| `components/apps/vip/vip-member-panel.tsx` | 会员查询：权益全貌 + 试用历史与资格 + 授予 + 开通记录 |
| `components/apps/vip/vip-shared.tsx` | 来源 / 状态徽标、功能标识徽标、金额与剩余时长格式化 |

四条硬约束：

1. **付费与试用分区展示，不混在一张表里。** 它们的入口完全不同（一个被购买、
   一个被领取），混着列会让人以为试用也能卖 —— 而那正是后端两道闸门拦下的事。
2. **功能标识只在目录里建，套餐上只能勾。** 允许在套餐里手打标识等于放弃了
   「拼错有报错」这件事：`exprot` 在服务端表现为校验永远返回 false，没有任何一处说得出为什么。
3. **删除前必须说清影响面。** 删功能标识时告知还有几个套餐挂着它（那几个套餐从此不再
   发放这项权益，但已开通的用户不受影响 —— 他们拿的是账本快照）；删套餐时说明
   删的是售卖入口而不是已开通的会员。
4. **恢复试用资格只删资格，不收回时长。** 客服要的是"让他重领"，
   顺手扣掉时长会变成用户眼里的"我的会员没了"。

`UserPicker` 的触发器是 `w-full`，**塞进 `SectionCard` 的 `aside` 必须套一层定宽容器** ——
`SectionCard` 是 `overflow-hidden`，不定宽会把旁边的按钮整个顶出卡片外（不报错，只是看不见）。

会员时长与功能权益的语义（顺延、并集、快照）全部由后端决定，控制台只展示；
判定入口在 [internal/service](../internal/service/CLAUDE.md#会员判定与试用期会员)。

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
| `scripts/map-providers.test.mjs` | 语言规则、供应商解析、坐标基准与目录完整性由 `pnpm test` 钉住 |

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
| `(console)/functions/page.tsx` | 页面骨架、应用选择、Tab 路由 |
| `components/functions/function-manager.tsx` | 函数列表 + 四个面板的装配 |
| `components/functions/function-create-dialog.tsx` | 新建：运行时 / 起始模板 / 能力 |
| `components/functions/function-overview-panel.tsx` | 运行状况：成功率 / 耗时分位 / 趋势 / Top 错误 |
| `components/functions/function-editor-panel.tsx` | 脚本工作台：写 → 试跑 → 发布 → 回滚 |
| `components/functions/function-invocations-panel.tsx` | 调用审计（筛选 + 分页 + 详情抽屉）与真实调用 |
| `components/functions/function-settings-panel.tsx` | 能力、运行闸门、函数配置、删除 |
| `components/functions/function-kv-panel.tsx` | KV 浏览器（脚本的服务端独占状态） |
| `components/functions/function-shared.tsx` | 能力勾选树、风险徽标、副作用清单、格式化 |
| `components/functions/script-editor.tsx` | Monaco + 按能力生成的 SDK 类型 |
| `lib/api/app-functions.ts` / `lib/function-hooks.ts` | 远程函数域 API 与 React Query hooks |

五条硬约束：

1. **能力目录来自后端**（`GET /function-catalog`），不在前端硬编码。
   在这里另抄一份会同时招来「后端加了一项、控制台勾不上」和「控制台能勾、
   保存时报不支持」两种漂移，而两边都没有报错提示 —— 与风控条件目录同源。
2. **编辑器的类型声明也来自那份目录**。`aegis-sdk-types.ts` 只负责按已声明能力
   过滤并拼装，声明片段本身在 Go 里。前端自己写一份类型的后果是
   「补全里有、运行时没有」，而那要到发版之后才暴露。
   拼装时**必须合并命名空间**（`user.read` 出 `get`、`user.write` 出 `ban`，同属
   `aegis.user`）：漏了会在同一个接口里产生两个 `user:` 成员，TypeScript 报重复
   声明后整份类型静默失效，表现是补全突然什么都没有。
3. **四个面板对应四件不同的事**，不要合回一屏：概览回答「为什么调不通」，
   脚本负责迭代，调用负责排障，设置负责闸门。旧形状最大的问题不是拥挤，
   是**没有试跑** —— 唯一的验证方式是把半成品激活到线上。
4. **试跑失败不是接口错误**。后端在脚本执行失败时仍返回 200，判成功要看
   `result.ok`；按抛异常处理会把日志与副作用清单一起丢掉，而那正是要看的东西。
   试跑产生的 effect 必须显示「未执行」标记，否则一份「发了 100 积分」的清单
   会被当成真的发生过。
5. **草稿按 (应用, 函数) 绑定**，切换函数时靠 `key` 整块重挂载，不用 effect 同步 ——
   与配置面板、门户凭据同一条约束。

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
