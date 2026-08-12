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

## 工作流：写 → 试跑 → 发布 → 回滚

控制台 `/functions` 里一个函数有四个面板，对应四件不同的事：

| 面板 | 回答什么 |
|---|---|
| 概览 | 现在跑得怎么样；**为什么调不通**（未激活时直接说出来，而不是让人对着一串 40990 猜） |
| 脚本 | 写、试跑、发布、把任意历史版本载回编辑器 |
| 调用 | 发生过什么（可按状态 / 调用方 / eventId 筛选），以及真实调用入口 |
| 设置 | 能力、运行闸门、函数配置、删除 |

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

控制台的脚本编辑器是 Monaco，并按当前函数已声明的 capabilities 动态生成 SDK 类型定义
喂给 TypeScript 语言服务 —— 补全里出现什么，运行时就绑定了什么。
沙箱中不存在的 `document` / `setTimeout` / `require` 会被如实标红。
**类型声明片段由后端目录下发**，不是控制台里的一份副本：SDK 真正绑定什么由 Go 决定，
在前端另写一份类型只会让「补全里有、运行时没有」这种错误拖到发版之后才暴露。

## SDK 与 capabilities

**声明即授权**：没有声明的能力在脚本里根本不会被绑定，`aegis.points` 直接是 `undefined`，
而不是「调用时才报错」。

**能力目录是单一事实源**（`internal/domain/appfunction/capabilities.go`），
同时驱动服务端校验、SDK 绑定、控制台勾选框与编辑器类型提示。
新增一种能力只需在目录里加一行 + 在 SDK 里加一个 binder，控制台零改动即自动出现 ——
与支付渠道的 `Describe()`、风控条件目录是同一套做法。
`TestCapabilityCatalogMatchesBinders` 双向钉死「目录 ↔ 绑定分支」：
目录多一条 → 勾得上却没有那个对象；绑定多一条 → 没声明也能调。

### 免声明即可用

| API | 说明 |
|---|---|
| `aegis.log(...)` / `console.log` | 只进服务端日志；试跑时回传给作者 |
| `aegis.fail(message, code?)` | 主动返回业务错误并终止调用 |
| `aegis.crypto.*` | `md5` / `sha1` / `sha256` / `sha512` / `hmacSha256` / `hmacSha512` / `base64*` / `hex*` / `randomHex` / `randomInt` / `uuid` / `timingSafeEqual` |
| `aegis.time.*` | `now` / `unix` / `iso` / `dayKey` / `monthKey` |
| `aegis.config` | 函数级参数，控制台上可改，**不需要发新版本** |

`md5` / `sha1` 明知已不适合做安全摘要仍然提供：接第三方接口时用什么签名算法
不由我们决定（易支付系全是 MD5）。不给的话作者只能在脚本里手写一份，那才更危险。

`console` 是 `aegis.log` 的别名 —— 「沙箱里没有 DOM」与「没人绑 console」是两回事，
后者纯粹是缺了一个别名，代价是几乎每个作者的第一行都会撞上 ReferenceError。

`aegis.time.dayKey()` 不是为了「更准」（沙箱里的 `Date` 取同一台机器的时钟），
而是为了把「每日额度的键怎么算」收成一个写法：各人各写一遍
`toISOString().slice(0,10)`，迟早会出现两个函数用不同的日切。

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
| `notification.send` | `aegis.notify.send` | 发给当前调用者 |
| `email.send` | `aegis.email.send` | **恒发往调用者绑定的邮箱**，脚本填不了收件人 |
| `audit.write` | `aegis.audit.log` | 写平台审计，操作者记为 `function:<函数名>` |
| `http.fetch` | `aegis.fetch(url, options)` | 仅 HTTPS、禁重定向、拒绝内网与元数据地址；返回带响应头 |

用户级操作一律锁定到当前调用者，脚本无法跨用户写。
`user.write` / `wallet.write` / `vip.write` / `email.send` / `http.fetch` 属高风险档，
控制台在勾选后会单独提示。

`aegis.email.send` 不接受收件地址：允许任意填写等于把平台变成一个
任何人都能驱动的转发器（凭证邮件那条链路上是同一条约束）。

> 历史说明：000056 版本的旧能力名（`storage.read` / `user.tag.write` 等）现仅为
> 兼容存量数据保留，不绑定任何对象；`user.profile.read` 由目录里的 `replacedBy`
> 自动映射到 `user.read`，各消费方不再各判一次。

### 限额与闸门

单次调用：SDK 总调用 ≤ 256 次，写操作 ≤ 64 次，出站请求 ≤ 8 次；
脚本正文 ≤ 256KB；KV 键 ≤ 128 字符、值 ≤ 32KB；函数配置 ≤ 32KB。
额度随能力目录一起下发给控制台 —— 作者应当在动手之前就知道，
而不是在一次超限失败之后去翻文档。

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
| `GET` `PUT` `DELETE` | `/functions/:name` | 函数管理（能力、闸门、配置均可改） |
| `GET` `POST` | `/functions/:name/versions` | 版本列表 / 发布 |
| `GET` `DELETE` | `/functions/:name/versions/:version` | 版本详情（**带脚本正文**）/ 删除未激活版本 |
| `POST` | `/functions/:name/versions/:version/activate` | 激活或回滚 |
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
