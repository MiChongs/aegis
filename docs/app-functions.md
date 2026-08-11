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

- 用户当前的 VIP 是否有效、是否被封禁、积分余额（`aegis.user`）
- 只存在于服务端的计数器、配额、密钥（`aegis.kv`）
- 服务端才有的随机数与 HMAC 密钥（`aegis.crypto`）

客户端既读不到也伪造不了这些状态，本地重实现自然无从谈起。

## 编写脚本

入口固定为 `handle(ctx)`，**必须同步返回**（沙箱没有事件循环，`async` 会被拒绝）：

```js
/** @param {AegisContext} ctx */
function handle(ctx) {
  const me = aegis.user.get();
  if (!me || me.banned) aegis.fail("账号不可用", 40311);
  if (!me.vip) aegis.fail("该功能仅限会员", 40310);

  const usedToday = aegis.kv.user.incr("quota:" + new Date().toISOString().slice(0, 10), 1, 86400);
  if (usedToday > 100) aegis.fail("今日额度已用尽", 42901);

  return { ok: true, remaining: 100 - usedToday };
}
```

`ctx` 结构：

```json
{
  "eventId": "4ed57288-...", "appId": 1, "appKey": "demo_app",
  "function": "issue-license", "version": "1.0.0",
  "caller": { "type": "user", "userId": 42 },
  "input": { }
}
```

`caller` 由服务端认定，客户端无法伪造。

控制台的脚本编辑器是 Monaco，并按当前函数已声明的 capabilities 动态生成 SDK 类型定义
喂给 TypeScript 语言服务 —— 补全里出现什么，运行时就绑定了什么。
沙箱中不存在的 `document` / `setTimeout` / `require` 会被如实标红。

## SDK 与 capabilities

**声明即授权**：没有声明的能力在脚本里根本不会被绑定，`aegis.points` 直接是 `undefined`，
而不是「调用时才报错」。

| capability | 注入对象 | 说明 |
|---|---|---|
| `user.read` | `aegis.user.get(userId?)` | 省略参数即当前调用者；跨应用返回 `null` |
| `points.write` | `aegis.points.add / deduct` | 走正式积分流水，余额不足由服务端拒绝 |
| `vip.write` | `aegis.vip.grant(days, reason?)` | 按天延长会员 |
| `kv.read` | `aegis.kv.get` | 应用级；`aegis.kv.user.*` 为用户级隔离 |
| `kv.write` | `aegis.kv.set / incr / del` | `incr` 是原子操作，适合频次限制 |
| `notification.send` | `aegis.notify.send(title, content, options?)` | 发给当前调用者 |
| `audit.write` | `aegis.audit.log(action, summary?)` | 写平台审计 |
| `http.fetch` | `aegis.fetch(url, options?)` | 仅 HTTPS、禁重定向、拒绝内网与元数据地址 |

无需声明即可用：`aegis.log()`（只进服务端日志）、`aegis.fail(message, code?)`、
`aegis.crypto.sha256 / hmacSha256 / randomHex`。

用户级操作一律锁定到当前调用者，脚本无法跨用户写。

### 限额

单次调用：SDK 总调用 ≤ 128 次，写操作 ≤ 32 次，出站请求 ≤ 4 次；
脚本正文 ≤ 256KB；执行超时取函数的 `timeoutMs`（死循环会被中断）。

### 事务边界

每个 SDK 写操作各自原子，但**整个脚本不是一个大事务** —— 中途抛错不会回滚先前的写入。
因此脚本应「先校验、后写入」。

好在 `eventId` 幂等保证失败调用不会被重放：同一 `eventId` 重试直接返回 `40991`，不会二次执行副作用。

### effects

脚本每次写操作都会记录一条 effect（类型 + 参数 + 结果）写入调用审计，
反映的是**实际发生了什么**，不是脚本的一面之词。

> 历史说明：000056 版本的 effects 由函数自行返回、服务端只校验 capability 从不执行，
> 那套旧能力名（`storage.read` / `user.tag.write` 等）现仅为兼容存量数据保留，不再有实际作用。

## API

- `GET /api/functions/signing-key`：获取 Aegis 请求签名公钥（http runtime 用）
- `GET|POST /api/admin/apps/:appkey/functions`：列出或创建函数
- `GET|PUT|DELETE /api/admin/apps/:appkey/functions/:functionName`：函数管理
- `GET|POST /api/admin/apps/:appkey/functions/:functionName/versions`：版本管理
- `POST /api/admin/apps/:appkey/functions/:functionName/versions/:version/activate`：激活或回滚
- `POST /api/admin/apps/:appkey/functions/:functionName/invoke`：管理员测试调用
- `GET /api/admin/apps/:appkey/functions/:functionName/invocations`：调用审计
- `GET|POST /api/admin/apps/:appkey/function-keys`：列出或创建 App 后端调用密钥
- `DELETE /api/admin/apps/:appkey/function-keys/:keyId`：撤销密钥
- `POST /api/apps/:appkey/functions/:functionName/invoke`：接入应用调用

调用体：

```json
{
  "eventId": "4ed57288-55bd-4625-9c70-88989f95ec0b",
  "input": { "points": 1200 }
}
```

同一 App 内重复提交相同 `eventId` 会返回已有成功结果，避免客户端重试造成重复处理。

**脚本正文永远不会通过任何 API 下发**：版本接口只返回 `artifactSha256`，接入方拿不到逻辑本身。

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
