# 应用级远程函数

远程函数让接入方把**自定义 API 逻辑放在服务端**。函数严格归属于单个 App。

三种运行时，按需选择：

| Runtime | 逻辑写在哪 | 能否读写平台数据 | 适用场景 |
|---|---|---|---|
| `script` | 控制台里直接写 JavaScript | **能**（受 capabilities 约束） | 自定义 API 的主路径 |
| `wasm` | 自行编译 WASM 后上传 | 否，纯计算 | 确定性算法 |
| `http` | 接入方自建 HTTPS 端点 | 否，Aegis 只转发 | 已有微服务 |

## 为什么用 script 才防得住破解

把逻辑搬到服务端，防的不是「代码被看见」，而是**客户端无法复现结果**。
纯计算函数（`wasm`）做不到这点：攻击者收集若干组输入输出，就能在本地重写一份等价实现。

`script` 的价值在于它能依赖**服务端独占的状态**：

- 用户当前的会员是否有效、是否被封禁、积分与钱包余额（`aegis.user` / `aegis.wallet`）
- 只存在于服务端的计数器、配额、密钥（`aegis.kv`）
- 服务端才有的随机数与 HMAC 密钥（`aegis.crypto`）

客户端既读不到也伪造不了这些状态，本地重实现自然无从谈起。

## 工作流：写 → 检查 → 试跑 → 发布 → 回滚

控制台 `/functions` 里一个函数有四个面板，对应四件不同的事：

| 面板 | 回答什么 |
|---|---|
| 概览 | 现在跑得怎么样；**为什么调不通**（未激活时直接说出来，而不是让人对着一串 40990 猜） |
| 脚本 | 写、检查、试跑、发布、把任意历史版本载回编辑器或与之对比 |
| 调用 | 发生过什么（可按状态 / 调用方 / eventId 筛选），以及真实调用入口 |
| 设置 | 能力、运行闸门、函数配置、删除 |

### 静态检查：把运行时的 TypeError 提前到保存那一刻

沙箱是 deny-by-default 的，因此「勾了 kv.read 却调了 `aegis.points.add`」在运行时
只是一句 `TypeError: Cannot read property 'add' of undefined` ——
不说缺什么、不说在哪一行，而且要等到**真实调用**才出现。版本又是不可变的，
发现时只能再发一版。

`POST .../functions/:name/analyze` 不执行任何代码，只回一组带行列的诊断，
编辑器直接把它们画成波浪线。**发布走的是同一套判定**，因此不会出现
「检查全绿、发布被拦」。

| 规则 | 级别 | 判据 |
|---|---|---|
| `syntax` | error | 编译不过（位置由编译器给） |
| `entry` | error | 没有 `handle`（函数声明与 `const handle = function` 都算） |
| `async` | error | `async function handle` —— 沙箱没有事件循环 |
| `capability` | error | 用到了没声明的能力，**并指名要补哪一项** |
| `forbidden-global` | error | `require` / `process` / `setTimeout` / `document`… 写了必然 ReferenceError |
| `unknown-member` | warning | `aegis.points.increase` 这类拼错的成员名 |
| `dangerous` | warning | `eval` / `Function` 构造器：绕过了能力声明这套检查 |
| `busy-loop` | warning | 没有 `break` / `return` / `throw` 出口的循环 |
| `unused-capability` | info | 勾了却没用到 |

只有 error 挡发布。warning 与 info 不挡是刻意的：这套检查是**词法层面**的近似
（goja 这个版本的 ast 包只有节点定义，没有遍历器），把「我拿不准」也变成硬闸门，
代价是某个合法写法从此发不出去，而作者除了绕开检查别无办法。

同一个原因，扫描器分得清代码、字符串与注释：注释里写一句
`// 用法：aegis.points.add(10)` 被报成缺声明，只需要发生一次，
这套检查就会被所有人当成障碍。

反查「`aegis.x.y` 需要哪项能力」的依据是能力目录里的 `Members` 字段，
`TestCapabilityMembersAppearInDeclaration` 钉住它与 TypeScript 声明一致 ——
两者漂移的表现是分析器**认不出**那行代码，于是这条检查静默失效。

### 编辑器

Monaco 内置的 TypeScript worker **就是 tsserver 本体**，因此补全、悬浮文档、
签名帮助、诊断、重命名、跳转定义、引用查找、大纲、折叠这些不需要另起一个
语言服务器进程 —— 它们本来就在，只是默认没全打开。现在全部打开了，
包括 inlay hints、语义高亮、sticky scroll、灯泡、同名高亮与联动编辑。

在此之上，有三类**平台语义**是 TypeScript 无从得知的，由自己的 provider 补：

| 它不知道的事 | 补法 |
|---|---|
| `aegis.points` 存不存在取决于勾了哪些能力 | 悬浮显示能力键 / 风险档 / **有没有声明** |
| `aegis.config.dailyQuota` 现在是多少 | 补全列出真实键并带当前值；悬浮直接贴出值 |
| 「这行缺 points.write」 | 服务端诊断 → 波浪线 + 灯泡里的「声明能力 X」 |

其余编辑体验：

| 能力 | 说明 |
|---|---|
| 快速修复 | 灯泡就在出问题那一行；缺能力一键补、勾多了一键取消 |
| Code Lens | `handle` 上方常驻一行：已声明几项能力（含几项高风险）+ 试跑入口 |
| 诊断列表 | 侧栏列出全部问题，点一条跳到那一行 |
| 代码片段 | 输 `aegis-` 列出额度 / 加锁 / 验签 / 金额 / JWT 等固定写法 |
| 入参类型 | 配了入参契约后 `ctx.input.` 补全出真实字段（见下节） |
| 版本对比 | 与激活版本或任意历史版本做并排 diff（只读） |
| 草稿持久化 | 未发布的正文存在浏览器本地，误刷新不丢 |
| 试跑用例 | 存几组 input + 身份（「会员用户」「过期用户」「缺参数」），一键重跑 |
| 快捷键 | `⌘/Ctrl + Enter` 试跑、`⌘/Ctrl + S` 发布、`F8` 跳下一个问题 |
| 格式化 | 走 Monaco 内置的 JS 格式化器 |
| 观感 | 配色接控制台设计令牌（语法色与开发者门户共用一份），呼吸光标 + 插入符滑动 + 惯性滚动 |

呼吸光标那一档是 `phase`：它在两个不透明度之间脉动而**从不完全消失**，
周期 1 秒 —— 与 macOS 插入符一致，也让它在长代码里更容易被找回来。
动效受两道闸门约束：系统的 `prefers-reduced-motion` 有否决权（无障碍诉求），
工具条上的「平滑动效」开关管的是性能（低端机上掉帧的光标比不动的更难看）。

三处 JSON 输入（试跑 input、函数配置、入参契约）都是带 **JSON Schema** 的
Monaco 编辑器，不是文本域：键名补全、枚举值补全、必填校验、悬浮显示字段说明
全部由 JSON 语言服务提供。入参契约编辑器喂的是一份**只登记服务端真会处理的
关键字**的元 schema —— 一个能补全出来、却不起作用的关键字，比补不出来更误导人。

草稿、用例与编辑器偏好都只存在浏览器本地。草稿不落服务端是因为它是
「还没想好要不要发」的东西，落库就要回答「谁能看见、两个人同时改怎么办、
什么时候清理」；用例是本人的调试素材而不是团队资产。

## 入参契约（input schema）

一份 JSON Schema，存在 `app_functions.input_schema`，`{}` 表示不约束。
它同时驱动**三处**，而这正是它值得存在的理由：

| 驱动谁 | 没有它时是什么样 |
|---|---|
| 调用入口的前置校验 | 接入方少传一个字段 → 脚本第三行抛 TypeError → 调用方拿到 `50290 应用函数执行失败`，既不说少了什么，也不说是自己传错了 |
| 试跑输入框 | 一个纯文本域，写错一个逗号要等点了提交才知道 |
| 编辑器里的 `ctx.input` | 类型是 `any`，`ctx.input.orderNo` 与接入方实际传的 `order_no` 之间的差异，要等线上第一次调用才以 undefined 的形式暴露 |

```json
{
  "type": "object",
  "required": ["orderNo"],
  "additionalProperties": false,
  "properties": {
    "orderNo": { "type": "string", "minLength": 1, "description": "业务订单号" },
    "channel": { "type": "string", "enum": ["web", "ios", "android"] },
    "quantity": { "type": "integer", "minimum": 1, "maximum": 99, "default": 1 }
  }
}
```

编辑器里 `ctx.input.` 就此补全出 `orderNo: string`（必填，无问号）、
`channel?: "web" | "ios" | "android"`（字面量联合，拼错当场标红）、
`quantity?: number`（JSDoc 里带「范围 1–99 · 默认 1」）。

几处刻意的取舍：

- **JSON Schema → TypeScript 的转换只有一份，在 Go 里**（`internal/domain/appfunction/input_schema.go`），
  生成结果随函数一起下发。控制台完全可以自己转，但那样就有了第二个转换器，
  而两个转换器迟早给出不同的类型 —— 表现是「补全里有这个字段、运行时却没有」。
  与能力的类型片段是同一条约束。
- **转换有意保守**：`$ref`、认不出的 `type`、混杂的 `enum` 一律降级成 `any`，绝不猜。
  多一个 `any` 只是少一点帮助，而猜错一个类型会让作者对着一条根本不存在的
  编译错误改代码。
- **未写 `additionalProperties` 时按封闭渲染**（不补索引签名）：补了的话
  所有拼错的字段名都变成合法的 `any`，类型检查等于白做。显式写 `true` 时才补。
- **校验排在幂等之前**：一个形状就不对的请求不该占用一个 `eventId`，
  否则调用方改对参数之后重试，会撞上「这个 eventId 已经失败过」。
- **试跑同样过这道校验，而且先于执行**。放行的话，作者会用一份线上根本进不来的
  input 把脚本调通，然后在真实调用里撞上 `40109` —— 而试跑存在的全部意义
  就是复现真实调用。与「试跑只用已声明的能力」是同一条取舍。
- **保存时真的编译一遍**（与调用时校验共用同一条路径）：一份编译不过的 schema
  在调用时的表现是「校验永远抛错」或者更糟「校验被跳过」，两种都不会在保存那一刻暴露。
- **错误逐条列出**（最多 5 条）而不是只回第一条：调用方改一处再试一次、
  又冒出一条，这个来回在跨团队接入时是以天计的。
- **编译时不装 URLLoader**：schema 是接入方写的，按 `$ref` 里的 URL 发起请求
  就是一个 SSRF 入口。resource URI 用 `aegis://`，任何试图联网的路径当场失败。

### 试跑不是「先发一版看看」

`POST /api/admin/apps/:appkey/functions/:name/test` 在**不创建版本、不写调用审计、
不产生真实副作用**的前提下跑一遍脚本：

- **读是真的**：`aegis.user.get()`、`aegis.kv.get()`、会员判定全部查真实数据 ——
  脚本的分支几乎全部由服务端状态决定，喂假数据跑出来的结论没有意义。
- **写只记录**：每个写操作照常产出一条 effect，但标记 `simulated: true` 且不执行。
  `points.add` 与 `kv.incr` 会读一次真实值再返回「如果真的执行会变成多少」，
  否则「今日额度已用尽」那条分支永远测不到。
- **出站请求按方法分流**：GET / HEAD 照常发出（读多跑一次没有代价），
  其余方法跳过并在日志里说明 —— POST 可能是一次扣款、一条短信、一封信。
- **日志一并回传**：作者在控制台上写脚本，不该为了看自己打的那行 `console.log`
  去翻服务器日志。

试跑失败**返回 200** 而不是 4xx：失败是正常结果，作者要的是错误内容加上
那之前的日志与副作用清单，回 4xx 会把这些全部丢掉。判成功看 `result.ok`。

失败时另带 `errorLine` / `errorColumn` / `stack`：一句
`TypeError: Cannot read property 'x' of null` 不说在哪一行，
作者只能把两百行脚本从头读一遍。调用栈里只保留脚本自己的帧，宿主帧被剥掉。

日志是**结构化**的（`{ level, message, elapsedMs }`）而不是拼好的字符串：
级别要着色、要能过滤，而「从 `"warn 内容"` 里把级别切出来」在消息本身
以 warn 开头时就会切错。`elapsedMs` 是相对执行起点的毫秒数 ——
沙箱里没有计时器，「哪一步慢」只能靠日志之间的间隔看出来。

试跑一律用函数**已声明**的能力，请求侧不能临时加 —— 否则「试跑通过」
证明不了「发版之后能跑」，而那正是试跑本该拦住的事。函数设置随时可改，
先勾上再试跑即可。

### 版本正文取得回来

`GET .../versions/:version` 返回带 `source` 的版本详情（**仅管理端**）。
「版本不可变」说的是发出去的那一版不能被改，不是作者拿不回自己写过的东西 ——
没有这个接口，改一行脚本要从零重写整份。

接入方那条链路上，正文由 domain 类型的 `json:"-"` 挡住，不依赖任何一处记得剔除它。

## 编写脚本

入口固定为 `handle(ctx)`，**必须同步返回**（沙箱没有事件循环，`async` 会被拒绝）：

```js
/** @param {AegisContext} ctx */
function handle(ctx) {
  const me = aegis.user.get();
  if (!me || me.banned) aegis.fail("账号不可用", 40311);
  if (!me.vip) aegis.fail("该功能仅限会员", 40310);
  // 试用会员要不要放行由脚本自己决定 —— 服务端只如实告诉你他是哪一档
  if (me.vipTrial) aegis.fail("该功能不对试用会员开放", 40310);
  // 细分权益按功能标识判，不要拿套餐名做字符串比较（那是运营随时会改的展示文案）
  if (!me.vipFeatures.includes("export")) aegis.fail("当前会员不含导出功能", 40310);

  // 阈值放在函数配置里：改额度不需要发新版本
  const quota = Number(aegis.config.dailyQuota || 100);
  const used = aegis.kv.user.incr("quota:" + aegis.time.dayKey(), 1, 86400);
  if (used > quota) aegis.fail("今日额度已用尽", 42901);

  return { ok: true, remaining: quota - used };
}
```

`ctx` 结构：

```json
{
  "eventId": "4ed57288-...", "appId": 1, "appKey": "demo_app",
  "function": "issue-license", "version": "1.0.0",
  "caller": { "type": "user", "userId": 42 },
  "input": { },
  "dryRun": false
}
```

`caller` 由服务端认定，客户端无法伪造。`dryRun` 为 true 表示这是控制台里的试跑，
脚本可以据此跳过不可逆的外部动作。

编辑器按当前函数已声明的 capabilities 动态生成 SDK 类型定义喂给 TypeScript 语言服务
—— 补全里出现什么，运行时就绑定了什么。沙箱中不存在的 `document` / `setTimeout` /
`require` 会被如实标红。**类型声明片段由后端目录下发**，不是控制台里的一份副本：
SDK 真正绑定什么由 Go 决定，在前端另写一份类型只会让「补全里有、运行时没有」
这种错误拖到发版之后才暴露。编辑器的其余能力见[上文](#编辑器)。

## SDK 与 capabilities

**声明即授权**：没有声明的能力在脚本里根本不会被绑定，`aegis.points` 直接是 `undefined`，
而不是「调用时才报错」。

**能力目录是单一事实源**（`internal/domain/appfunction/capabilities.go`），
同时驱动服务端校验、SDK 绑定、控制台勾选框与编辑器类型提示。
新增一种能力只需在目录里加一行 + 在 SDK 里加一个 binder，控制台零改动即自动出现 ——
与支付渠道的 `Describe()`、风控条件目录是同一套做法。
`TestCapabilityCatalogMatchesBinders` 双向钉死「目录 ↔ 绑定分支」：
目录多一条 → 勾得上却没有那个对象；绑定多一条 → 没声明也能调。

### 免声明即可用（标准库）

判据只有一条：**纯计算，不碰平台数据、不出网、不产生副作用**。
满足这条的东西不该要一次能力声明 —— 声明的意义是「授权访问某样东西」，
而这里没有任何东西可授权。

不给的代价不是「作者少了个便利」，而是他会在脚本里**自己手写一份**，
而手写的这几样出错时都不报错，只是悄悄给出错误结果：

| 手写的东西 | 出错的样子 |
|---|---|
| 签名串拼接 | JS 对象遍历顺序对数字键与字符串键规则不同，签名时对时不对 |
| 每日日切 | `toISOString().slice(0,10)` 走 UTC，比东八区晚八小时切 |
| 金额加减 | `0.1 + 0.2 === 0.30000000000000004`，落到账上就是对不平的那一分 |
| JWT | base64url 的 padding 与算法白名单，写错就是「本地能签、对方验不过」 |

| 命名空间 | 内容 |
|---|---|
| `aegis.log(...)` / `console.*` | 只进服务端日志；试跑时回传给作者 |
| `aegis.fail(message, code?)` | 主动返回业务错误并终止调用 |
| `aegis.assert(cond, message, code?)` | 断言，不成立即以 40001 终止 |
| `aegis.crypto.*` | 摘要（`md5`/`sha1`/`sha256`/`sha512`/`sha3`/`crc32`）、HMAC（`hmacMd5`/`hmacSha1`/`hmacSha256`/`hmacSha512`）、编解码（`base64*`/`hex*`）、随机（`randomHex`/`randomBytes`/`randomInt`/`uuid`）、`timingSafeEqual`、对称加密（`aesEncrypt`/`aesDecrypt`）、`jwtSign`/`jwtVerify`、`totpVerify`、口令（`bcryptHash`/`bcryptVerify`/`pbkdf2`/`hkdf`） |
| `aegis.time.*` | `now`/`unix`/`iso`/`dayKey`/`monthKey`/`weekKey`/`dayKeyIn`/`format`/`parse`/`add`/`diff`/`startOfDay`/`cronNext` |
| `aegis.text.*` | `template`（Go text/template）/`slugify`/`pinyin`/`maskEmail`/`maskPhone`/`truncate`/`stripHtml`/`sanitizeHtml`/`escapeHtml`/`length` |
| `aegis.encoding.*` | `yamlParse`/`yamlStringify`/`csvParse`/`csvStringify`/`xmlToJson`/`jsonToXml`/`queryParse`/`queryStringify`/`urlEncode`/`urlDecode`/`gzip`/`gunzip` |
| `aegis.decimal.*` | `add`/`sub`/`mul`/`div`/`cmp`/`round`/`abs`/`isZero`/`format`，全部字符串进出 |
| `aegis.json.*` | `get`/`exists`（gjson 路径取值）/`pretty`/`parse`（失败给 fallback 而不抛错） |
| `aegis.validate.*` | `schema`（JSON Schema，一次返回全部错误）/`email`/`url`/`ip`/`uuid`/`phone`/`json` |
| `aegis.ua.parse(ua)` | 客户端解析：`kind` 是分类（desktop/mobile/tablet/bot），`device` 是机型 |
| `aegis.config` | 函数级参数，控制台上可改，**不需要发新版本** |

几处刻意的取舍：

- **`md5` / `sha1` 明知不适合做安全摘要仍然提供**：接第三方接口时用什么签名算法
  不由我们决定（易支付系全是 MD5）。不给的话作者只能手写一份，那才更危险。
- **`jwtSign` 只支持 HMAC 家族**：非对称签名要求脚本持有私钥，
  而 KV 里存私钥比存共享密钥危险得多，值得单独设计。`jwtVerify` 显式限定算法族 ——
  不限定的话把 `alg` 改成 `none` 就能伪造令牌，这是 JWT 最经典的一个坑。
- **`jwtVerify` 校验失败返回 `{ valid: false, error }` 而不是抛错**：令牌过期是最常见的
  正常分支，抛错会逼每个作者写一圈 try/catch，而沙箱里的 catch 拿不到结构化原因。
- **`aesDecrypt` 认证失败一律抛错**，绝不返回「尽力而为」的半截明文：
  GCM 的价值就在于改过一个比特就解不开。
- **`queryStringify` 按键名字典序输出**：签名串的顺序必须稳定。
- **`dayKeyIn(timezone)` 是给国内接入方的**：默认的 `dayKey` 走 UTC，
  而他们要的日切几乎都是东八区零点；自己 `+8 小时再取日期`会在跨年与月末那两天出错。
- **时区名给错直接报错**，不悄悄回落到 UTC —— 回落的表现是
  「配了 Asia/Shanghai，日切却在早上八点」。
- **`validate.schema` 一次返回全部错误**，不是遇到第一个就停：只回第一条会让作者
  陷入「改一个、再跑一次、又冒出一个」的循环。
- **`text.maskEmail` / `maskPhone` 复用平台既有的遮罩口径**：同一个邮箱在资料页、
  审计日志与脚本产出里必须遮成同一个样子。

`console` 是 `aegis.log` 的别名 —— 「沙箱里没有 DOM」与「没人绑 console」是两回事，
后者纯粹是缺了一个别名，代价是几乎每个作者的第一行都会撞上 ReferenceError。

### 沙箱里的标准全局

`Buffer` / `URL` / `URLSearchParams` / `TextEncoder` / `TextDecoder` / `atob` / `btoa`
恒定存在，与能力声明无关。它们全是**纯内存类型**，没有任何 I/O ——
「沙箱里没有 Node」不等于「连一个字节缓冲类型都没有」，后者纯粹是缺东西，
而接第三方二进制接口时没有 `Buffer` 只能用字符串硬拼字节，那才容易出错。

`Buffer` 与 `URL` 由 [goja_nodejs](https://github.com/dop251/goja_nodejs) 提供，
装载途径是它的模块系统 —— 因此**装完立刻把 `require` 全局删掉**：
那个库默认的加载器直接读宿主磁盘，留着 `require` 等于开了一个文件读取入口。
注册表另外配了一个恒定拒绝的加载器做纵深防御。

`btoa` / `atob` 复刻浏览器的「二进制串」语义（每个字符落在 0..255，超出报错），
不是按 UTF-8 编码 —— 差异只会在对接方验签失败时才被发现。

`require` / `process` / `setTimeout` / `document` / `window` / `XMLHttpRequest`
一个都不存在，编辑器如实标红，静态检查也会在发布前指出来。

### 需要声明的能力

| capability | 注入 | 说明 |
|---|---|---|
| `user.read` | `aegis.user.get / entitlement` | 会员字段走统一判定：`vip` / `vipTrial` / `vipSource` / `vipFeatures` / `vipExpireAt` / `vipRemainingSeconds`；另有积分、封禁状态 |
| `user.write` | `aegis.user.ban / unban` | 封禁落成正式记录（可撤销、可申诉、有操作人），不是翻一个布尔位 |
| `points.write` | `aegis.points.add / deduct` | 走正式积分流水，余额不足由服务端拒绝 |
| `vip.read` | `aegis.vip.status / hasFeature` | 只要会员结论时用它，比 `user.read` 窄 |
| `vip.write` | `aegis.vip.grant / revoke` | 按天延长或收回会员有效期 |
| `wallet.read` | `aegis.wallet.get` | 金额一律是字符串，转 number 会丢分 |
| `wallet.write` | `aegis.wallet.adjust` | 正数入账、负数扣减，走管理员调账流水 |
| `kv.read` | `aegis.kv.get / has / list` | 应用级；`aegis.kv.user.*` 为用户级隔离 |
| `kv.write` | `aegis.kv.set / incr / del` | `incr` 是数据库层面的原子操作 |
| `lock.acquire` | `aegis.lock.acquire / release / run` | **跨实例互斥锁**（Redis），保护「先查后写」那段临界区 |
| `notification.send` | `aegis.notify.send` | 发给当前调用者 |
| `realtime.push` | `aegis.realtime.send` | 推给当前调用者的**在线连接**；不落库、不补发 |
| `email.send` | `aegis.email.send` | **恒发往调用者绑定的邮箱**，脚本填不了收件人 |
| `audit.write` | `aegis.audit.log` | 写平台审计，操作者记为 `function:<函数名>` |
| `geo.read` | `aegis.geo.lookup(ip)` | IP 归属地与运营商（GeoIP2 + ASN） |
| `http.fetch` | `aegis.fetch(url, options)` | 仅 HTTPS、禁重定向、拒绝内网与元数据地址；支持 `form` / `query` |

用户级操作一律锁定到当前调用者，脚本无法跨用户写。
`user.write` / `wallet.write` / `vip.write` / `email.send` / `http.fetch` 属高风险档，
控制台在勾选后会单独提示。

#### 分布式锁为什么是必要的

`kv.incr` 挡得住计数型的并发，挡不住「判断 A、修改 B」这种跨键的临界区 ——
而那正是发奖、兑换、抽奖这几类脚本的标准形状。单实例下靠运气也能对，
多副本部署时两个请求会同时读到「还没领过」，然后各发一份。

```js
// 抢不到锁直接抛错；无论正常返回还是抛错都会释放
return aegis.lock.run("claim:" + me.id + ":" + task, function () {
  if (aegis.kv.user.has(claimKey)) aegis.fail("该奖励已领取", 40901);
  aegis.kv.user.set(claimKey, { at: aegis.time.iso() }, 0);
  return { points: aegis.points.add(100, "任务奖励") };
}, 10);
```

- **落 Redis 而不是 `app_function_kv`**：这里要的是「抢不到就立刻失败」，
  而数据库的 UPSERT 语义给不了这个（它总会成功）。
- **锁键按 (应用, 函数) 加前缀**：不加的话两个应用用同一个键名会互相锁住对方，
  而那种串扰在任何一侧都看不出来。
- **释放校验持有者**（复用 `bsm/redislock`，与幂等中间件同一套实现）：
  裸 `SetNX` + `DEL` 会在超时之后删掉**别人**的锁，那是分布式锁最经典的一个错误。
- **必然自动到期**（默认 10s，上限 60s）：脚本可能在持锁期间被超时中断，
  那时没有任何脚本代码会走到 release。宿主侧另有一道收口，在调用结束时
  统一归还本次还持有的锁 —— TTL 只是最后的保险。
- **优先用 `run` 而不是 `acquire` / `release`**：后者成对写在脚本里时，
  中间任何一次 `aegis.fail` 都会跳过 release，而那正是最常写的分支。

#### 实时推送与站内信的分工

`aegis.realtime.send` **不落库、不补发**，离线即丢，因此返回 `false` 是正常结果
而不是错误。两者混用是最常见的误解 ——「重要的通知用实时推送发出去了，
用户没收到」。要留痕、要能补看的走 `aegis.notify.send`。

`aegis.email.send` 不接受收件地址：允许任意填写等于把平台变成一个
任何人都能驱动的转发器（凭证邮件那条链路上是同一条约束）。

> 历史说明：000056 版本的旧能力名（`storage.read` / `user.tag.write` 等）现仅为
> 兼容存量数据保留，不绑定任何对象；`user.profile.read` 由目录里的 `replacedBy`
> 自动映射到 `user.read`，各消费方不再各判一次。

### 限额与闸门

单次调用：SDK 总调用 ≤ 256 次，写操作 ≤ 64 次，出站请求 ≤ 8 次；
脚本正文 ≤ 256KB；KV 键 ≤ 128 字符、值 ≤ 32KB；函数配置 ≤ 32KB；
标准库单次入参 ≤ 1MB；试跑回传日志 ≤ 200 行；单把锁最长 60 秒。
额度随能力目录一起下发给控制台 —— 作者应当在动手之前就知道，
而不是在一次超限失败之后去翻文档。

标准库那道 1MB 闸门不是形式：入参可以来自 `aegis.fetch` 拿回的响应体，
拼几次就到几兆，而模板渲染与 XML 解析在那个量级上会先吃满内存再超时。
`gunzip` 另按解压后大小限长 —— 压缩比可以做到上千倍，
几 KB 的输入能解出几百兆（zip bomb），而那块内存在超时之前已经分配出去了。

**编译产物按正文摘要缓存**（上限 512 份，超出整体清空）。
「每次调用新建运行时」是隔离要求，「每次重新编译」只是重复劳动 ——
同一个激活版本的正文永远不变，而一份 200 行脚本的编译在热路径上
比它自己的执行还贵。运行时仍然一次一个，跨请求状态残留的可能性没有变化。

函数级三个闸门，管的不是同一件事：

| 闸门 | 语义 | 超出 |
|---|---|---|
| `timeoutMs` | 单次执行时长（死循环会被中断） | 执行失败 |
| `maxConcurrency` | **单实例**同时执行数，保护本进程 | `42990` |
| `rateLimitPerMin` | 每分钟调用次数，**业务配额** | `42991` |

频次计数落在 `app_function_kv` 上走数据库原子自增，因此多实例部署下仍然准确 ——
放在各实例内存里的表现是「配了 60/分钟，实际放行 60×实例数」，
而控制台上完全看不出来：一个看起来在防、其实没防的闸门比没有这个闸门更糟。
计数键带 `__aegis:` 保留前缀，脚本读写不到，否则脚本可以把限制自己的那个计数清零。

### 函数配置（`aegis.config`）

控制台「设置」页里的一段 JSON 对象，脚本里读作 `aegis.config`，改完立即生效。

它存在的理由是「改一个阈值不该需要发一个新版本」：版本不可变是对的，
但把「每日额度 100」这种数字也钉进不可变产物里，等于每次调参都要走一遍
发版 + 激活，而回滚时还会把无关的逻辑一起滚回去。

顶层必须是 JSON 对象。数组或标量会让 `aegis.config.xxx` 恒为 `undefined`，
而那种失败不会报错，只会让阈值静默变回代码里的默认值，因此在保存时就拦住。
配置**永不下发给接入方**，与脚本正文同级。

### 事务边界

每个 SDK 写操作各自原子，但**整个脚本不是一个大事务** —— 中途抛错不会回滚先前的写入。
因此脚本应「先校验、后写入」。

好在 `eventId` 幂等保证失败调用不会被重放：同一 `eventId` 重试直接返回 `40991`，不会二次执行副作用。

### effects

脚本每次写操作都会记录一条 effect（类型 + 参数 + 结果）写入调用审计，
反映的是**实际发生了什么**，不是脚本的一面之词。
试跑产生的 effect 带 `simulated: true`：少了这个标记，一份看起来
「发了 100 积分、开了 30 天会员」的清单会被当成真的发生过。

## 键值存储

`aegis.kv`（应用级共享）与 `aegis.kv.user`（按调用者隔离）是脚本的
「服务端独占状态」载体：计数器、配额、服务端下发的配置与密钥。

控制台 `/functions?tab=kv` 提供浏览器：按作用域 / 用户 / 键前缀检索，可逐条删除。
排障时最常问的一句是「这个用户的配额计数现在是多少」——
没有这个视图，唯一的回答方式是临时写一个脚本去读它，而那本身就是一次真实的副作用，
还会把计数再加一。

过期条目会被列出并标注：判定上它们等于不存在，但看不见它们就解释不了
「为什么 KV 表里有几十万行」。

## API

管理端（`/api/admin/apps/:appkey/...`）：

| 方法 | 路径 | 用途 |
|---|---|---|
| `GET` | `/function-catalog` | 能力目录 + 运行时额度 + 内置模板 + 基础类型声明 |
| `GET` `POST` | `/functions` | 列出 / 创建函数 |
| `GET` `PUT` `DELETE` | `/functions/:name` | 函数管理（能力、闸门、配置、入参契约均可改） |
| `GET` `POST` | `/functions/:name/versions` | 版本列表 / 发布（**发布前跑一遍静态检查**） |
| `GET` `DELETE` | `/functions/:name/versions/:version` | 版本详情（**带脚本正文**）/ 删除未激活版本 |
| `POST` | `/functions/:name/versions/:version/activate` | 激活或回滚 |
| `POST` | `/functions/:name/analyze` | **静态检查**（不执行代码，只回诊断） |
| `POST` | `/functions/:name/test` | **试跑**（不建版本、不写审计、写只记录） |
| `POST` | `/functions/:name/invoke` | 管理员真实调用 |
| `GET` | `/functions/:name/invocations` | 调用审计（按 status / callerType / eventId 筛选，分页） |
| `GET` | `/functions/:name/stats` | 成功率、耗时分位、Top 错误、按小时分桶 |
| `GET` `DELETE` | `/function-kv` | KV 浏览 / 删除 |
| `GET` `POST` | `/function-keys` | 调用密钥列表 / 创建 |
| `DELETE` | `/function-keys/:keyId` | 撤销密钥 |

其余：

- `GET /api/functions/signing-key`：获取 Aegis 请求签名公钥（http runtime 用）
- `POST /api/apps/:appkey/functions/:functionName/invoke`：接入应用调用

调用体：

```json
{
  "eventId": "4ed57288-55bd-4625-9c70-88989f95ec0b",
  "input": { "points": 1200 }
}
```

同一 App 内重复提交相同 `eventId` 会返回已有成功结果，避免客户端重试造成重复处理。
频次闸门排在幂等重放之后 —— 一次网络重试拿回既有结果，不该再吃一次配额。
入参契约校验则排在幂等**之前**：一个形状就不对的请求不该占掉一个 `eventId`。

**脚本正文永远不会通过面向接入方的 API 下发**：版本列表只返回 `artifactSha256`，
正文只出现在管理端的版本详情接口里。

## 调用身份

两种：属于该 App 的用户 `Bearer` Token，或服务端持有的 `X-Aegis-Function-Key`。

函数密钥以 `afk_` 开头，只在创建响应中返回一次，数据库只保存 SHA-256 摘要。
它只适合放在接入 App 的服务端或 Cloudflare Worker Secret 中，**不能嵌入网页、桌面端或移动端安装包**。
客户端调用请用用户 Access Token。

## WASM 沙箱 ABI

WASM 版本最大 2MB，每次调用使用独立实例，内存上限 16MB；不启用 WASI，不提供网络、文件系统、环境变量或宿主函数。

模块必须导出：

```text
memory
malloc(size: i32) -> i32
free(ptr: i32)
handle(ptr: i32, len: i32) -> i32
```

`handle` 的输入是 UTF-8 JSON。返回地址前四个字节是小端序响应长度，之后是响应 JSON：

```json
{ "output": { "level": 6 }, "effects": [] }
```

WASM 不能访问 Aegis 数据库，也拿不到用户状态 —— 需要这些能力请改用 `script`。

## Cloudflare Worker 双向签名（http runtime）

Aegis 请求包含：

```text
X-Aegis-Event-ID
X-Aegis-Timestamp
X-Aegis-Content-SHA256
X-Aegis-Signature
```

请求签名内容：

```text
timestamp + "\n" + eventId + "\n" + contentSha256
```

Worker 使用 `/api/functions/signing-key` 返回的 Ed25519 公钥验证请求。响应也必须签名，
创建 HTTP 函数版本时将 Worker 的 Ed25519 公钥写入 `responsePublicKey`。

响应签名内容：

```text
eventId + "\n" + sha256(responseBody)
```

签名使用无填充 base64url 编码并放入 `X-Aegis-Response-Signature`。

HTTP 执行器仅允许 HTTPS，禁止重定向，并在实际连接时重新解析和检查 IP，拒绝环回、私网、链路本地、云元数据及保留地址。

## 发布与回滚

版本记录不可修改。发布新功能时创建新版本并激活；回滚时重新激活旧版本。
激活操作会在一个数据库事务中切换 `active_version`，调用始终解析到单一活动版本。

激活中的版本删不掉：那会让 `active_version` 指向一条不存在的记录，
调用时表现为 `40992`，而版本列表上看不出任何异常。
