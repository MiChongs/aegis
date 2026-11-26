# 防火墙（规则集 v2）

2026-11-26 重构。代码在 `internal/firewall`（引擎，与 gin 无关）、`internal/middleware/firewall.go`（gin 适配）、
`internal/service/firewall_policy_service.go`（规则 / 例外 / 测试台）、`internal/service/firewall_log_service.go`（事件落库），
控制台在 `aegis-console/src/app/(console)/firewall/` 与 `components/firewall/`。

## 1. 为什么重构

v1 有三个结构性问题：

1. **WAF 在解密之前检测。** Coraza 挂在全局中间件的入口位置，排在 `AppGateway`（Transport v2 sealed）与
   `AppEncryption`（旧 `X-Aegis-Encrypted`）之前。加密请求的请求体与 `_payload` 查询串在那里只是一段密文，
   CRS 等于什么都没检查，base64 密文还会偶发误报。
2. **JSON 请求体一律免攻击规则。** 为了压误报，`id:1000003` 给所有 `application/json` 请求关掉了
   `attack-sqli / attack-xss / attack-rce / attack-protocol` 四类规则——几乎整个 API 都没有注入检测。
   同时全局移除 17 条规则、入站阈值抬到 40（八条 critical 才拦）。
3. **没有可运营的东西。** 没有自定义规则、没有按路径的例外、看不到命中了哪些规则、改一次参数要靠环境变量或
   猜，也没有地方先试一试。

## 2. 两段式检测

```
ClientIP → … → AccessLog
  → Firewall.Handler()      入口段：IP 白 / 黑名单 → 封禁（IP、地域、围栏）→ 可信网段 → 方法与长度
                            → 扫描器 UA → 探测路径与快速签名 → 自定义规则 → 限流
  → 流量管理 → ReplayGuard → AppGateway（sealed 解密）→ ClientControlGate → AppEncryption（旧加密解密）
  → Firewall.WAFHandler()   载荷段：Coraza + CRS v4 + Aegis 专属规则，看到的是明文
  → Location → 路由
```

- **入口段**只看连接、请求行与请求头，放在解密之前：被封禁的 IP、扫描器、命中自定义规则的请求不该消耗
  Nonce、签名校验与解密。加密 GET 的查询串是 `_payload=<密文>`，长度上限与签名对它不生效，事件里记成 `<密文>`。
- **载荷段**在两个解密中间件之后。它们通过 `middleware.SetRequestTransport` 标出载荷来源
  （`sealed` / `legacy`），WAF 把它写进 `TX:AEGIS_TRANSPORT` 并记到事件上。
  **拦截响应写进解密中间件包好的 Writer**：sealed 客户端收到的是加密过的错误信封，照常能解开看到 `40396`。
- 解密中间件替换过请求体，原 `Content-Length` 头是密文长度（sealed）或已被删掉（旧加密）。WAF 按实际要检查的
  明文长度重写，`920180` 才不会误报。
- 旧加密把明文写在临时文件里，WAF 读完会 `Close` 原 Body（临时文件随之删除），再换成内存里的副本。

## 3. CRS 调优（规则集 v2）

指令由 `internal/firewall/directives.go` 生成，控制台「规则集 → 查看指令」可看完整文本。顺序：

| 段 | 内容 |
|---|---|
| 引擎基线 200000-200009 | 对齐 coraza 3.8.1 的 `coraza.conf-recommended`（CRS 包里那份是旧版）：JSON / XML 处理器、`URI_PARSE_ERROR`、请求体解析失败 400、`SecArgumentsLimit`、二进制类型用 RAW 处理器（200007） |
| `Include @crs-setup.conf.example` + 1100001 | 偏执等级（拦截 / 检测分开）、入站与出站阈值、方法放行集、内容类型白名单（加上 octet-stream、text/plain 等 Aegis 实际接收的类型，取代 v1 全局移除 920420） |
| 调优档 1100099-1100999 | 按路径与字段的精确排除（见下表） |
| 管理员例外 1190000+ | 控制台「WAF 例外」页维护，表 `firewall_waf_exclusions` |
| 901 → `@aegis/aegis-protection.conf` → 905…949 →（出站检测开启时）950…980 → 999 | 规则本体；被整组停用的文件不 Include（901/905/949/959/980/999 不能停） |
| `SecRuleRemoveById` | 全局移除（只剩 6 条，见 `config.DefaultCorazaRemovedRules`） |

调优档：

| 规则 | 作用 |
|---|---|
| 1100099 | `/api/admin/system/firewall/` 不检查请求体（测试台的请求体就是攻击样本） |
| 1100100 | `/api/admin/` 不检查请求体（可在设置里打开）：已鉴权的运维请求里满是规则表达式、远程函数代码、邮件模板 |
| 1100110+ | 请求体豁免前缀：AI 中转、支付回调、邮件服务商回调（误拦就是丢单、丢投递事件） |
| 1100200 | 存储对象路径免 920440（文件名就是用户的 backup.db、crash.log） |
| 1100300 | **敏感字段**（password、secret、signature…）免全部攻击类标签，命中数据里的值打码 |
| 1100310 | **富文本字段**（content、html、markdown、reply…）免 XSS / SQLi / RCE / 注入，保留 LFI、RFI、SSRF、Java |

字段按最后一段匹配，同时认表单字段 `password` 与 JSON 的 `json.password`、`json.user.password`。

阈值默认 **5**（CRS 官方值，一条 critical 即拦）。v1 用 40 是因为 JSON 一律免检、误报只能靠抬阈值兜；
v2 改成按字段排除后，`internal/firewall/waf_test.go` 用一组真实的 Aegis 业务请求（中文昵称、带代码片段的反馈、
带特殊字符的口令、Passkey 断言、Markdown 工单回复、支付回调……）与一组攻击（JSON 里的 SQLi / XSS、解开后的
sealed 查询串、Log4Shell、阿里云元数据 SSRF、`alg=none`、原型链污染、伪装成 text/plain 的 JSON……）钉住：
前者一条都不能拦，后者一条都不能放。

### 规则集版本迁移

`CorazaRulesetVersion` 随设置持久化。读到 v1 配置时 `NormalizeFirewallConfig` 只迁「没人改过的旧默认值」：
移除清单正好是 v1 的 17 条 → 换成 v2 的 6 条；阈值正好是 40 → 换成 5。手工调过的值保持原样。

### 列表字段的约定

`nil` = 没填，用默认值；非 nil 的空切片（落库为 `[]`）或 `["none"]` = 显式清空。`Normalize` 必须幂等。

### 内容类型改判

gin 的 `ShouldBindJSON` 不看 `Content-Type`。声明成 text/plain、octet-stream 或不带类型、内容却是 JSON 的请求，
处理器照样当 JSON 解析；WAF 若按声明走 RAW 或 urlencoded，攻击串就原封不动地过去了。
`firewall.EffectiveContentType` 按内容改判，只改 WAF 看到的头。

### Windows 上的已知差异

Coraza 的 `t:normalizePath` 用 `filepath.Clean` 实现，Windows 上会把 `/etc/passwd` 变成 `\etc\passwd`，
依赖它的规则（930120 等）在 Windows 上永远匹配不上。线上与 CI 都是 Linux；测试里这类用例标了 `posixOnly`。

## 4. Aegis 专属规则（1101000-1101999）

`internal/firewall/rules/aegis-protection.conf`，放在 901 之后（要用它初始化的计分变量），全部计分不直接拦：

| ID | 检测 |
|---|---|
| 1101010 | 外部请求带了内部服务头 `X-Risk-Token` / `X-Risk-Operator`（只有主服务转发给风控后台时才加） |
| 1101020 | `alg=none` 的 JWT（过渡期内仍接受旧 HS256 用户令牌） |
| 1101030 | 接入网关路径里 appKey 不是 `[A-Za-z0-9_-]{1,64}` |
| 1101040 | 国内云的实例元数据地址（阿里云 100.100.100.200、腾讯云、火山引擎等；CRS 只收了 AWS / GCP / Azure 系） |
| 1101050 | 解密后的查询串里还有 `_payload`（二次封装） |
| 1101060 | 会话令牌（PASETO v4.local / JWT）放在 URL 里（PL2，notice） |

## 5. 自定义规则

表 `firewall_rules`，按 `priority` 升序求值（同优先级按 ID）。表达式用 [expr](https://expr-lang.org)，
变量（`firewall.RuleEnv`）：

| 变量 | 说明 |
|---|---|
| `ip` `method` `path` `query` `host` `ua` `referer` `contentType` | 请求行与常用头 |
| `headers["x-foo"]` | 请求头（键小写，取第一个值） |
| `args["page"]` | 查询参数（加密 GET 在入口段还没解开，为空） |
| `app` | 接入网关路径里的 appKey |
| `country` `region` `city` `isp` `asn` | 归属地（只有引用了这些变量的规则才会查，防火墙缓存 30 分钟） |
| `hour` `weekday` | 北京时间 |
| `encrypted` | 客户端声明了加密载荷 |

函数 `cidr(ip, "10.0.0.0/8", "1.2.3.4")`；运算符 `startsWith` `endsWith` `contains` `matches` `in` 都可用。

动作：`allow`（跳过其余入口检查与 WAF，封禁仍先生效）、`block`（可选响应方式复用封禁模式；可选同时封禁
来源 IP，由 Worker 执行）、`log`（记一条事件继续检查）、`skip_waf`（只跳过 WAF）。

命中数各副本在内存里累计，每 30 秒把增量加进库（`hits = hits + 增量`，多副本不会互相覆盖）。

## 6. 事件

`firewall_logs` 自迁移 000174 起不只记拦截：`action` 区分 `block` / `observe`（观察模式「本该拦截」、
出站命中）/ `log`（自定义规则的记录动作）；另有 `stage`（edge / payload / response）、`transport`、
`app_key`、`anomaly_score`、`matches`（命中明细 JSONB）、`rule_ref` / `rule_name`。

**凡是回答「拦了多少」的查询都必须带 `action = 'block'`**：自动封禁计数、报表、地理热力图与聚类、
监控面板都已加上。观察类事件按「来源 + 规则 + 路径」一分钟去重，观察模式不至于把表写爆。

## 7. 改动生效

设置（`security.firewall` 平台设置）、规则、例外都是「本机试编译 → 落库 → 本机热更新 → 广播」。
广播主题：设置走 `platform-settings`，规则与例外走 `firewall-policy`，消息里不带内容，各副本收到后自己从库重载。
例外拼进 Coraza 指令，拼错会让整个 WAF 编译失败，所以保存前一定先用「改动之后」的整套例外试编译。
热更新换下来的旧 WAF 实例 30 秒后才释放（上面可能还有正在跑的事务）。

## 8. 封禁

判定顺序：IP 精确封禁（Redis，O(1)）→ 地域规则 → 地理围栏，先命中的生效；封禁排在可信网段之前。

批量管理（迁移 000175 加 `revoke_note`、`delay_ms`、`updated_by` 与筛选索引）：

- **批量封禁**：一次最多 1000 个 IP，去重；IPv4 映射的 IPv6 按 IPv4 封；网段拒收（放「访问控制 → 黑名单网段」）；
  已在封禁中的跳过；归属地并发查询（最多 8 路）。逐条返回 created / exists / invalid / failed。
- **批量解封**：按选中的 ID，或按筛选解封全部匹配的生效中封禁。按筛选时必须带 `expected`（确认时看到的条数），
  实际条数不符返回 40990，避免「确认 30 条、实际解了 3000 条」；不带任何条件的「全部解封」直接拒绝；
  单次最多 5000 条（`truncated=true` 表示还有剩余）；子查询 `FOR UPDATE SKIP LOCKED`，两个人同时解封互不等锁。
- **修改**：生效中的封禁可改响应模式、原因、时长（从现在起算，0 = 永久）与 tarpit 延迟，同步重写 Redis 与 TTL；可批量。
- **IP 查询**：这个 IP 现在会不会被拦、被哪一层拦、封禁历史、24 小时 / 7 天拦截次数。地域规则用不计数的 `MatchIP`，查询不污染命中统计。
- 修了一个旧问题：服务启动把生效中的封禁同步回 Redis 时只带原因与来源，丢了响应模式与 tarpit 延迟，重启后所有封禁都退回默认 403。
- 风控处置（source=risk）的封禁在这里解除后，风控平台名单里的对象不会随之撤销。

## 9. 接口

全部在 `/api/admin/system/firewall/` 下、仅超级管理员；权限点 `system:firewall:read`（含测试台与表达式校验）、
`system:firewall:policy`（规则、例外与设置）、原有的 `ban` / `fence` / `log_clean`。

| 接口 | 说明 |
|---|---|
| `GET runtime` | 本副本运行态与计数 |
| `GET/PUT settings` | 防火墙设置（部分更新；列表传 `[]` = 清空） |
| `GET/POST rules`、`PUT/PATCH/DELETE rules/:ruleId`、`POST rules/reorder`、`POST rules/validate` | 自定义规则 |
| `GET/POST exclusions`、`PUT/PATCH/DELETE exclusions/:exclusionId` | WAF 例外 |
| `GET catalog`、`GET catalog/rules/:ruleId`、`GET directives` | 规则目录、规则详情与最近命中、完整指令 |
| `POST sandbox` | 测试台（可带一条草稿例外，看它能不能消掉误报） |
| `GET bans`（新增 country / mode / keyword / expiringWithin / permanent / sort）、`GET bans/stats`、`GET bans/count`、`GET bans/inspect` | 封禁列表、概况、计数、IP 查询 |
| `POST bans/batch`、`POST bans/unban`、`POST bans/update`、`PATCH bans/:banId`、`POST geo-bans/batch` | 批量封禁、批量解封、批量 / 单条修改、地域规则批量启停删 |
| `GET logs`（新增 action / stage / transport / appKey / ruleRef / keyword 筛选、score 排序）、`GET stats`（新增 minute 粒度与各维度聚合） | 事件 |

## 10. 测试

- `go test ./internal/firewall/ ./internal/middleware/`：指令编译、业务流量 / 攻击语料、例外、脱敏、观察模式、
  sealed 解密后拦截且错误信封经加密 Writer 返回、自定义规则、可信网段。
- `AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55432/aegis_fw_test go test ./internal/transport/http -run TestFirewallIntegration`：
  真库 + miniredis 端到端（用独立库，测试会清空 public schema）；`-run TestIPBanBatchIntegration` 覆盖封禁批量管理。
