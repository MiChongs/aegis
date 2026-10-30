# 一起听

在线听歌的「一起听」：一个人建房，其他人凭 6 位房间号或邀请链接加入，所有人听到同一首歌的同一个位置，可以聊天、点歌、发表情。需要登录，并拥有会员权益 `listen_together`。

## 架构

```
 App ──(1) POST /listen-together/ticket──▶ Aegis 主服务 ──判登录 + 权益──▶ 签 EdDSA 票据
  │                                          │  GET /api/listen-together/jwks.json（公钥）
  │                                          │  NATS listen.control.revoke（封禁、强制下线）
  │                                          │  Connect/gRPC（控制台管理房间）
  └──(2) wss /ws  hello{ticket}──▶ 一起听微服务 ×N ──Redis（房间状态）── NATS（跨实例事件）
```

- **独立进程、独立部署**：`services/listentogether`，入口 `cmd/listen-together`，镜像 `deploy/docker/listen-together.Dockerfile`。长连接、房间状态、事件扇出都在这里。主服务进程里只有签票、JWKS 与管理端转发，一起听挂了、重启、扩容都不影响主服务；反过来主服务繁忙或重启时，已连上的人照常一起听（验票只用缓存的公钥）。
- **服务间契约**：内部管理接口用 protobuf 定义（`api/listentogether/v1/admin.proto`，`buf generate` 生成 Go 与 Connect 代码），同一端口同时说 Connect / gRPC / gRPC-Web，附 gRPC 健康检查与反射；主服务侧客户端带 OpenTelemetry 拦截器与熔断器（gobreaker，连续 5 次失败熔断 20 秒）。
- **服务发现与负载均衡**：WebSocket 前面挂 CDN / 反代（任何实例都能接任何用户）；内部 RPC 走容器网络 DNS。实例间无主：房间状态在 Redis（乐观并发，WATCH/MULTI），房间事件经 NATS `lt.room.{app}.{room}` 只发给有这个房间连接的实例。
- **可观测性**：内部端口 `/metrics`（Prometheus：连接数、命令计数与耗时、投递事件数、慢消费者、巡检耗时）；`LT_OTLP_ENDPOINT` 打开链路上报，与主服务的链路经 traceparent 接上。对外端口 `/healthz`（存活）与 `/readyz`（公钥已加载、Redis 与 NATS 正常、未在排空）。
- **依赖注入与生命周期**：uber-go/fx。关停顺序：就绪探针转失败 → 所有连接收到 `bye{code:"shutdown", reconnect:true}` → 注销实例 → 关监听。客户端重连到其它实例，房间无感接回。

## 部署

```bash
# 容器化全栈会一起起来（profile app）；单独起：
docker compose --env-file .env -f deploy/docker/docker-compose.yml --profile listen up -d listen-together
# 水平扩容
docker compose ... up -d --scale listen-together=3
```

主服务环境变量（`internal/config/listen_together.go`）：

| 变量 | 说明 |
|---|---|
| `LISTEN_TOGETHER_ENDPOINTS` | 客户端可连的 wss 地址，逗号分隔、按顺序尝试；为空即未开通。**只写经 CDN 的域名，不写源站 IP** |
| `LISTEN_TOGETHER_TICKET_KEYS` | Ed25519 种子（base64 32 字节），第一把签名、其余只出现在 JWKS 里供轮换；为空从 `JWT_SECRET` 派生 |
| `LISTEN_TOGETHER_TICKET_TTL` | 票据有效期，默认 10m（1m–1h） |
| `LISTEN_TOGETHER_RPC_URL` | 一起听内部地址，如 `http://listen-together:8091` |
| `LISTEN_TOGETHER_INTERNAL_TOKEN` | 内部接口令牌，与 `LT_INTERNAL_TOKEN` 一致 |

一起听服务环境变量（`services/listentogether/internal/config`，前缀 `LT_`）：

| 变量 | 默认 | 说明 |
|---|---|---|
| `LT_PUBLIC_ADDR` / `LT_INTERNAL_ADDR` | `:8090` / `:8091` | 对外 WebSocket；对内 RPC 与指标（不得暴露公网） |
| `LT_REDIS_URL` | 空 | 为空用进程内存储（只能单实例） |
| `LT_NATS_URL` | 空 | 为空用进程内总线（只能单实例） |
| `LT_TICKET_JWKS_URL` / `LT_TICKET_JWKS` | — | 二选一：主服务 JWKS 地址（10 分钟刷新，遇到新 kid 立即补拉）或直接给 JWKS 文本 |
| `LT_INTERNAL_TOKEN` | 空 | 为空时内部接口拒绝一切调用 |
| `LT_ROOM_CAPACITY` / `LT_ROOM_CAPACITY_MAX` | 20 / 50 | 默认人数上限 / 房主可调的上限 |
| `LT_QUEUE_LIMIT` | 200 | 共享队列最多多少首 |
| `LT_CHAT_HISTORY` | 50 | 每个房间保留的最近聊天 |
| `LT_MEMBER_GRACE` | 3m | 掉线成员保留席位多久 |
| `LT_ROOM_IDLE_TTL` | 10m | 没人在线多久后解散 |
| `LT_MAX_CONNS_PER_INSTANCE` | 20000 | 单实例连接上限（超出 503，让负载均衡换实例） |

上线前在控制台「会员功能目录」新建标识 `listen_together`，并加进需要的会员档位。

## 票据

`POST /api/v1/apps/{appkey}/listen-together/ticket`（Bearer，三档传输照常）：

```json
{ "ticket": "eyJ…", "expiresAt": 1761800000000, "refreshAfter": 1761799880000,
  "endpoints": ["wss://listen.example.com/ws"], "serverTime": 1761799400000,
  "user": { "id": 1234567890123456, "nickname": "小明", "avatar": "https://…" } }
```

JWT（EdDSA，头部带 `kid`）：`iss=aegis`、`aud=listen-together`、`sub`=对外用户 ID、`app`、`nick`、`avt`、`dev`、`ent=["listen_together"]`、`exp`/`nbf`/`iat`/`jti`。实现在 `pkg/listenticket`，两边共用。用户 ID 一律是对外 ID，数据库主键不出主服务。

错误码：`40230` 没有权益（HTTP 402，引导开通会员）；`50360` 服务未开通（隐藏入口）；`50361` 一起听服务暂不可用；`40467` 管理端操作的房间不存在。

撤销：封禁（阻断会话的那一档）与控制台「踢出全部会话」时，主服务向 NATS `listen.control.revoke` 发 `{appKey, userId(对外), reason}`，一起听服务立即断开并把人移出房间。其它情况（失去权益）由票据过期兜底：换不到新票据的连接在过期后 `LT_TICKET_GRACE`（60 秒）内断开。

## WebSocket 协议（v1）

连 `endpoints` 里的地址。客户端帧 `{ "id": "可选，回复会带回", "type": "...", "data": {...} }`；服务端帧 `{ "type": "welcome|reply|snapshot|event|pong|bye", ... }`。

**握手**：10 秒内第一帧必须是 `hello`（票据放首帧而不是 URL，URL 会进 CDN 日志）：

```json
{ "type": "hello", "data": { "ticket": "…", "client": { "platform": "android", "version": "3.2.0" } } }
```

成功回 `welcome{protocol, connId, instance, serverTime, ticketExpiresAt, heartbeatSec, user, limits, room?}`；用户本来就在某个房间时 `room` 非空，紧跟一帧 `snapshot`（断线重连、换设备都靠这个无感接回）。失败回 `bye{code: ticket_invalid | not_entitled | hello_timeout | protocol_error}` 后关闭。同一用户只保留最新一条连接，旧连接收到 `bye{code:"replaced"}`。

**命令**（回复 `reply{id, ok, data|error{code,message}}`）：

| type | data | 谁能用 | 回复 data |
|---|---|---|---|
| `ping` | `{t0}` | 任何人 | 不走 reply：直接回 `pong{t0, ts}`，用于校时 |
| `auth` | `{ticket}` | 任何人 | `{ticketExpiresAt}`；新票据须同一应用、同一用户 |
| `room.create` | `{name?, controlMode?, allowRequests?, locked?, capacity?}` | 任何人（先离开当前房间） | Snapshot |
| `room.join` | `{code}` | 任何人 | Snapshot |
| `room.sync` | — | 成员 | Snapshot |
| `room.leave` | — | 成员（房主离开自动转交，最后一人离开即解散） | — |
| `room.close` | — | 房主 | — |
| `room.settings` | 同 create，只给要改的字段 | 房主 | — |
| `member.kick` / `member.transfer` | `{userId}` | 房主 | — |
| `member.grant` | `{userId, canControl}` | 房主 | — |
| `queue.set` | `{items: Track[]}`（整体替换，≤ queueLimit，itemId 唯一） | 房主 | — |
| `playback.update` | `{itemId, positionMs, playing, rate?, at?}` | 房主 | — |
| `request.add` | `{track}` | 成员（房主关了点歌时需控制权；每人最多 3 条待处理） | — |
| `request.resolve` | `{requestId, accept}` | 房主 | 被处理的 Request |
| `control` | `{action: play\|pause\|seek\|next\|previous\|jump, positionMs?, itemId?}` | 有控制权的成员 | — |
| `chat.send` | `{text}`（≤300 字） | 成员 | ChatMessage |
| `reaction` | `{emoji}` | 成员 | — |
| `status` | `{state: idle\|buffering\|playing\|paused\|error, itemId?, positionMs?}` | 成员 | — |

Track：`{itemId, source, songId, title, artist?, album?, cover?, durationMs?, requestedBy?}`。`source` 是音源平台标识（`[a-z0-9_]{1,16}`），`songId` 为 `[A-Za-z0-9_-]{1,256}`。服务端从不碰音频，各端用自己的音源取流。

**同步模型：房主权威。** 房主本地播放器是唯一事实：房主客户端观察自己的播放器，换歌、播放 / 暂停、拖动、卡顿恢复时推 `playback.update`，队列变化时推 `queue.set`；其他人照着渲染。有控制权的成员发 `control`，服务端只转给房主（`event{kind:"control", target:房主}`），房主执行后的结果再从房主那里广播出来。成员点歌进 `requests`，房主接受时由房主客户端把歌插进自己的队列。

**时钟**：`playback` 的含义是「服务端时刻 `at` 时，`itemId` 放到 `positionMs`」，正在播放时任意服务端时刻 t 的位置 = `positionMs + (t − at) × rate`。客户端用 `ping/pong` 估计与服务端的时钟差（取往返最短的几次），房主上报时把本地时刻换算成服务端时刻填进 `at`（服务端只接受 [now−10s, now+2s]，否则按收到时刻）。成员偏差超过约 1 秒时直接 seek，较小的偏差可用 ±3% 播放速度慢慢追平。

**事件** `event{room, data: Event}`，Event 的 `kind`：

| kind | 字段 |
|---|---|
| `member.joined` / `member.updated` | `member`（MemberView，含 `online`、`status`、`rev`） |
| `member.left` | `userId`, `reason: leave\|kick\|timeout\|revoked`（userId 是自己时说明你已离开房间） |
| `room.updated` | `room`（MetaView：设置、房主、`rev`） |
| `room.closed` | `reason: host\|idle\|admin\|empty` |
| `playback` | `playback`（含 `rev`） |
| `queue` | `queue{items, rev}` |
| `requests` | `requests{items, rev}` |
| `chat` | `chat`（按 `id` 去重） |
| `reaction` | `reaction{userId, emoji, at}` |
| `control` | `control`（只发给房主） |

**乱序**：多实例下事件可能乱序到达。房间每次变化 `rev` 加一，变化的那一块（播放、队列、设置、点歌、某个成员）记下新的 rev；客户端按块比较，收到的 rev 不比手里新就丢弃。块与块互不影响，不需要全局有序。拿不准时发 `room.sync` 取快照。

**连接维护**：服务端每 25 秒发 WebSocket ping，60 秒收不到任何帧即断开；发送缓冲（256 帧）写满的慢连接被断开（`bye{code:"slow_consumer", reconnect:true}`），重连取快照补齐。命令限流 20/秒（突发 40），聊天与表情 1/秒（突发 5）。掉线成员保留席位 `LT_MEMBER_GRACE`；房主掉线超过宽限期才转交。

## 管理端

控制台经主服务转发（权限按应用读 / 写，审计目录已登记）：

- `GET /api/admin/apps/:appkey/listen-together/stats` — 房间数、在播房间、成员与在线人数、各实例连接数
- `GET /api/admin/apps/:appkey/listen-together/rooms?page=&pageSize=`
- `GET /api/admin/apps/:appkey/listen-together/rooms/:roomId` — 成员、队列、最近聊天、被移出的人
- `POST /api/admin/apps/:appkey/listen-together/rooms/:roomId/close` `{reason?}`
- `POST /api/admin/apps/:appkey/listen-together/rooms/:roomId/kick` `{userId}`（应用内用户 ID）

管理端返回的 `userId` 是应用内主键（跳用户详情用），`publicId` 是对外 ID。

## 测试

```bash
go test -race ./services/listentogether/... ./pkg/listenticket/
```

- `room`：规则单测（权限、容量、锁定、移出、转交、巡检超时与空闲解散、点歌、聊天、校验）。
- `store`：Redis（miniredis）与内存两种实现跑同一套契约测试，另测并发更新不丢写。
- `gateway`：两个实例 + 共享 miniredis + 内嵌 nats-server 的真实 WebSocket 端到端：跨实例建房 / 加入 / 队列 / 播放 / 控制转发 / 聊天 / 校时 / 换实例重连顶替 / 主服务经 NATS 撤销 / 解散，以及坏票据、无权益、续期。
- `adminrpc`：Connect 客户端经 gRPC 协议调内部接口，含令牌校验。
