# AI 中转网关

应用的**终端用户**凭自己的 API 密钥（或登录会话的访问令牌）调用 OpenAI / Anthropic 兼容接口，
网关把请求转给应用在「AI 服务」里配好的上游通道，按模型价格 × 本人倍率，
从免费额度、会员额度、赠送额度、积分、余额里依次扣费。**调用失败不扣费。**

| | `/api/apps/{appkey}/ai/v1` | `/api/ai/v1`（本文） |
|---|---|---|
| 调用方 | 接入方**服务端** | 应用的**终端用户**（App 内或任意 OpenAI 客户端） |
| 凭据 | 应用的函数调用密钥 | 用户的 `sk-ag-` 密钥 / 访问令牌 |
| 谁付钱 | 应用 | 用户自己的额度、积分、余额 |
| 接口 | chat / messages | chat、messages、embeddings、images、audio、rerank |

代码位置：领域规则 `internal/domain/aigateway`（计价、倍率、分摊、零头、估算、用量解析，全是纯函数），
事务 `internal/repository/postgres/ai_gateway_billing_repository.go`，编排 `internal/service/ai_gateway_relay.go`，
调用面 `internal/transport/http/ai_relay_handlers.go`，迁移 `000113_ai_gateway`。

## 接入

```python
from openai import OpenAI
client = OpenAI(base_url="https://aegis.example.com/api/ai/v1", api_key="sk-ag-…")
client.chat.completions.create(model="gpt-4o", messages=[{"role": "user", "content": "你好"}])
```

- 凭据放在 `Authorization: Bearer`（OpenAI SDK）、`x-api-key`（Anthropic SDK）或 `api-key`（Azure 客户端）。
  `sk-ag-` 前缀加 43 位 base62 的是用户密钥，其余按访问令牌处理（应用设置 `allowSessionToken` 关闭时拒绝）。
- 密钥由用户在 App 里自己领（`POST /api/v1/apps/{appKey}/ai/keys`，Kotlin SDK `client.ai.createKey()`），
  明文只返回一次；库里只有 sha256。可设可用模型白名单、累计消费上限、到期时间。
- 每个响应带 `X-Aegis-Request-No`，对得上用户的调用记录（`/ai/requests/{requestNo}`）与扣费流水。
- 密钥路径不经过会话校验，所以鉴权时另外核对：应用 `status`、平台治理 `blockApi`、账号封禁（`enforceAccountBan`）。

### 接口与计量

| 接口 | 模型种类 | 计量（以上游报回为准） |
|---|---|---|
| `GET /models` | — | 列出开放的、密钥白名单允许的模型 |
| `POST /chat/completions` | chat | `usage.prompt_tokens / completion_tokens`；流式自动带 `include_usage` |
| `POST /messages` | chat | 同上（Anthropic 形状进出，内部走同一条对话链路） |
| `POST /embeddings` | embedding | `usage.prompt_tokens` |
| `POST /images/generations`、`/images/edits` | image | **实际返回的张数**（要 3 张给 2 张收 2 张）；gpt-image 系列另报的 token 一并计入 |
| `POST /audio/speech` | speech | 输入文本的字符数 |
| `POST /audio/transcriptions`、`/translations` | transcription | `usage.seconds` / `usage` token / `duration` / 字幕最后一个时间戳 |
| `POST /rerank` | rerank | `usage.total_tokens` / Cohere `meta.billed_units` |

对话走 `AIProviderService` 的统一链路（OpenAI / Anthropic / Gemini 三种协议的通道都能接，可链路回退），
上游分别用官方 SDK `openai-go`、`anthropic-sdk-go`、`google.golang.org/genai` 调用；
其余接口经 **OpenAI 兼容协议的通道原样转发**（含 Azure），同样走 `openai-go` 的请求管线（地址、鉴权、
Azure 部署路径与 api-version、附加请求头、错误收敛与对话一致），只替换模型名，请求体与响应体都是原样字节，
响应里的 `model` 换回对外名。图像编辑与转写收 multipart，按原样重组（文件保留原 Content-Type）。

转写的客户端要 `response_format=text` 时，网关向上游要 `json`（带用量），再把 `text` 作为纯文本交回 ——
text 格式不带任何用量，原样转发就只能估算。上游什么用量都没报时，按上传文件大小以 32 kbps 估算时长并标记「估算」。

## 思考

对话接口收三种写法的思考设置，归一后按通道的协议翻译给上游；都不写时什么都不带，交给上游默认。

| 写法 | 例子 |
|---|---|
| OpenAI | `"reasoning_effort": "high"`（`none` / `minimal` / `low` / `medium` / `high` / `xhigh` / `max`） |
| OpenRouter | `"reasoning": {"effort": "low"}`、`{"max_tokens": 4000}`、`{"enabled": false}` |
| Anthropic 风格 | `"thinking": {"type": "enabled", "budget_tokens": 8000}`、`{"type": "disabled"}` |
| `/messages` | `thinking` 同上，另收新版的 `output_config.effort` |

| 上游 | 翻译 |
|---|---|
| OpenAI / Azure | `reasoning_effort`；输出上限改用 `max_completion_tokens`（推理模型不认 `max_tokens`） |
| Groq / xAI / Ollama / 聚合站 / 自定义 | `reasoning_effort` |
| OpenRouter | `reasoning` 对象 |
| DeepSeek、智谱 GLM | `thinking.type` 开关 |
| 豆包 | `thinking.type` 开关，另带 `reasoning_effort`（压进 minimal–high） |
| 通义千问、硅基流动 | `enable_thinking` 与 `thinking_budget` |
| Kimi | 由型号决定，不带参数 |
| Anthropic | `thinking.budget_tokens`：强度折算 minimal 1024 / low 2048 / medium 8192 / high 16384 / xhigh 32768 / max 63999；预算必须小于 `max_tokens`，请求带了上限（含余额不足时压低的上限）就在上限里给正文留 1024，留不出最低预算就不开；开着思考时去掉 `temperature`（`top_p` 只留 ≥ 0.95 的）；`tool_choice` 为 any 时不开 |
| Gemini 2.x | `thinkingBudget`（关闭给 0，Pro 最低 128），`includeThoughts` |
| Gemini 3 | `thinkingLevel`（关闭取最低一档，Pro 为 LOW），`includeThoughts` |

思考内容回给客户端：OpenAI 形状放在 `reasoning_content`（流式在 delta 里），Anthropic 形状是 `thinking` 块（非流式带 `signature`）。
思考 token 由上游计入输出 token，按输出单价计费；没带 `max_tokens` 时暂扣把思考预算加进输出估算。

**工具循环里的签名**。开着思考的工具循环，Anthropic 要求发起调用那一轮的 thinking 块连同 `signature` 原样带回，
Gemini 3 要求每个函数调用的 `thoughtSignature` 带回，DeepSeek、Kimi 等要求带回那一轮的 `reasoning_content`。
OpenAI 兼容的客户端没有地方放前两种签名，所以网关按工具调用编号在内存里记两小时（`service/ai_llm_reasoning.go`），
续轮里同一编号的调用缺签名就补回去；`reasoning_content` 由客户端回传（Koog、OpenAI 兼容客户端大多会带），
网关只转给认这个字段的上游（官方 OpenAI、Azure、Groq、xAI 不转）。多实例下续轮落到另一台时查不到签名：
Anthropic 这一轮不开思考，Gemini 3 用官方允许的 `skip_thought_signature_validator` 占位，调用照样成功。
Gemini 不给调用编号时合成的编号带随机段，不会在对话之间撞上。

## 模型能力

对话模型的 `capabilities`（迁移 `000127`，JSONB）：`vision`、`tools`、`reasoning`、`reasoningEfforts`
（可选强度，空表示 low / medium / high，含 `none` 表示能关闭思考）、`contextLength`。只是声明，网关不据此拒绝请求；
`GET /ai/models` 的对话模型带上它，`GET /api/ai/v1/models` 的列表项另带 `capabilities` 与 `context_length`、`max_output_tokens`。
存量模型默认只开工具；改模型时不传 `capabilities` 沿用原值。

## 模型目录

公开的模型元数据，用来填模型的能力、名称说明与价格（`internal/domain/modelcatalog` 纯函数，
`service/ai_model_catalog.go` 拉取与同步，迁移 `000128`）。

| 来源 | 地址 | 用途 |
|---|---|---|
| models.dev | `https://models.dev/api.json` | 主来源：能力、`reasoning_options`（可选强度 / 开关 / 预算）、上下文与输出上限、美元单价 |
| OpenRouter | `https://openrouter.ai/api/v1/models` | 建 `openrouter/…` 条目；按厂商前缀给原厂条目补缺 |
| LiteLLM | `model_prices_and_context_window.json` | 只收主流供应商（OpenAI、Anthropic、Gemini、DeepSeek、Mistral、xAI、Groq、通义、Kimi、智谱、OpenRouter、Cohere、豆包），补缺或新建 |

- 合并：models.dev 有值就不看后面的；能力布尔值以 models.dev 为准，其余来源只补空字段。价格统一成「美元 / 百万 token」，保留 6 位小数。
- 思考选项折算成强度：`effort` 照抄；`toggle` 记为 `none / high`；`budget_tokens` 记为 `low / medium / high`，预算下限为 0（或 Anthropic 一系）时加 `none`，预算够大时加 `xhigh`。
- 全平台一份快照（单行表 `ai_model_catalog`），各实例读它建内存索引，`fetched_at` 前进才重读（至多 5 分钟一查）。
  后台每小时检查一次，快照超过一天就刷新；多实例抢租约 `refreshing_until`，只有一台去拉。models.dev 拉不到时整次失败、旧快照不动；
  其余来源失败只记在来源状态里。手动刷新两分钟内只做一次。出网走 egress。
- 匹配（`Index.Match`）：逐字相同 → 去掉日期 / `-latest` / `:free` / 厂商前缀、`.` `_` 统一成 `-` 后相同 → 前缀包含（「相近」，同步时默认跳过）。
  通道的供应商是第一提示（`CatalogProvidersFor`），其次按模型名前缀认原厂（gpt→OpenAI、claude→Anthropic、qwen→通义 …），
  原厂条目即使要去后缀才对上也胜过转售商那里逐字相同的一条。

管理端 `…/ai-gateway/model-catalog`（概况）、`/refresh`、`/search?q=&provider=&kind=`、`/match?model=&configId=`，
以及 `POST …/ai-gateway/models/sync`：`fields` 取 `capabilities`（能力、上下文、最大输出、对上的条目）/ `info`（显示名、说明，
默认只填空的，`overwrite` 覆盖）/ `prices`（美元单价 × `priceRate` 点，全为 0 时标记免费）；`preview` 只算变化不写；
`keys` 给某个模型手动指定条目；`allowFuzzy` 允许按「相近」同步。目录的最大输出写进 `capabilities.maxOutput`（声明），
**不**写 `maxOutputTokens`：后者是网关的输出上限，没带 `max_tokens` 的请求按它暂扣，设成 128K 会让每次调用都暂扣一大笔。

设置 `catalogAutoSync`：每次刷新出新快照后，把这个应用的对话模型按目录更新能力与名称说明（只跟精确与去后缀的匹配，不动价格）。

App 端 `GET /api/v1/apps/{appKey}/ai/catalog/lookup?models=a,b&providers=openai`（免登录，一次 200 个，Kotlin `client.ai.catalogLookup`）：
用户自己接的供应商拉完模型列表后，用它补全能力；只回能力与上限，不回价格，「相近」不算查到。

## 长调用：异步任务与保活

生图、改图、转写常要一两分钟，而网关前面的反向代理都有读超时（常见 60–100 秒）。同步调用挂过这个时间，
客户端拿到的是代理的 502 / 504 页面（不是 JSON），网关这边看到的是客户端断开 —— 调用不扣费，但用户什么也没拿到。
两种出路：

**异步任务（推荐，Aegis SDK 默认用它）**。原样转发的接口（图像、嵌入、语音、转写、重排）都支持：

```http
POST /api/ai/v1/images/generations
Prefer: respond-async            # 或 ?async=true

202 Accepted
Location: /api/ai/v1/jobs/AIG…
{"id":"AIG…","object":"aegis.job","endpoint":"images.generations","status":"in_progress","created_at":…,"elapsed_ms":0,"poll_after_ms":2000}
```

- 鉴权、组装请求体、暂扣在提交时同步做：余额不足、模型不存在等错误照常立刻返回（4xx），不会变成一个失败的任务。
- `GET /jobs/{id}?wait=20`：长轮询，任务还在进行中就最多挂 `wait` 秒（上限 25）。状态 `in_progress` / `completed` /
  `failed` / `canceled`。完成时 JSON 结果原样内嵌在 `result`（与同步调用的响应体逐字节相同）；
  二进制结果（语音合成）给 `result_url`，即 `GET /jobs/{id}/content`。失败时 `error` 是与同步调用一致的错误
  `{message, type, code, aegis_code, status}`。
- `DELETE /jobs/{id}`：取消进行中的任务（经 NATS `aegis.aigateway.cancel` 找到跑它的实例），整笔放回、不扣费。
- 任务编号就是调用编号，计费、追踪、实时推送都与同步调用相同。凭据可以是同一用户的任意密钥或登录令牌。
- 结果在结束后保留 1 小时；进行中超过 15 分钟没有结论（实例被杀）的由后台判为 `failed`（`job_interrupted`），
  对应的暂扣按过期放回。进程退出时等进行中的任务最多 20 秒，之后取消（不扣费）。
- 表 `ai_gateway_jobs`（迁移 `000123`），服务端 `service/ai_gateway_jobs.go`。

**同步保活（兼容不改调用方式的 OpenAI SDK）**。图像、嵌入、JSON 格式的转写、重排与非流式对话：
调用超过 20 秒还没结果时，先回 `200` 与响应头（`X-Aegis-Keepalive: whitespace`），之后每 15 秒写一个空格，
代理看到持续有字节就不会断开；结果到了再写 JSON（JSON 允许前导空白，各家 SDK 都能解析）。
代价是保活开始后出错也只能是 `200` + 错误信封。请求头 `X-Aegis-Keepalive: off` 关掉。

## 流式

`"stream": true` 的 `/chat/completions` 与 `/messages`。网关前面隔着几层反向代理（CDN 的 nginx 默认 60 秒、
Cloudflare 约 100 秒读超时），推理模型出第一个字前可能想上几分钟；期间一个字节都不写，代理就会切断连接，
客户端看到的是「生成到一半自己停了」。所以流式的契约是：

- **暂扣成功就开流**：立刻回 `200`、`Content-Type: text/event-stream` 与 `X-Aegis-Request-No`，不等上游第一个字。
  鉴权、模型、余额、限流这些暂扣之前的错误照旧是带状态码的 JSON 错误（401 / 402 / 404 / 429 …）。
- **心跳**：从开流到结束，每 15 秒写一次心跳。OpenAI 形状是 SSE 注释行 `: ping`，Anthropic 形状是官方的
  `event: ping` / `data: {"type":"ping"}`。SSE 解析器（各家 SDK）都会忽略它们。
- **成功**：OpenAI 形状一定有一个带 `finish_reason` 的收尾块，然后是用量块与 `data: [DONE]`；
  Anthropic 形状一定有带 `stop_reason` 的 `message_delta` 与 `message_stop`。`finish_reason` 取值
  `stop` / `length` / `tool_calls` / `content_filter`；上游给了认不出的原因时原样转出（如 `pause_turn`），
  **不会**冒充成 `stop`。有工具调用而上游没给原因或给了 `stop` 时按 `tool_calls`。
- **开流之后出错**：HTTP 状态已经是 200 改不了，以一个错误事件收尾 ——
  OpenAI 形状 `data: {"error":{"message","type","code","aegis_code","status"}}`，**之后不写 `[DONE]`**；
  Anthropic 形状 `event: error` / `data: {"type":"error","error":{"type","message","aegis_code","status"}}`，不写 `message_stop`。
  `status` 是这个错误不开流时本该回的 HTTP 状态（502 上游失败、503 服务更新 …）。
  **客户端必须把「收到错误事件」与「流结束了却没有 finish_reason / `[DONE]`」都当成失败**（可重试），
  不能把已收到的半截内容当成一次正常的回答。
- **上游的流不完整就是失败**：上游没给出结束原因就断了（OpenAI 既无 `finish_reason` 也无 `[DONE]`；Anthropic
  既无 `stop_reason` 也无 `message_stop`；Gemini 没有 `finishReason`），或者 Gemini 报 `MALFORMED_FUNCTION_CALL`，
  都按上游失败处理：错误事件收尾、整笔放回不扣费。
- **上游空闲看门狗**：上游响应开始后连续 `AI_UPSTREAM_IDLE_TIMEOUT`（默认 5 分钟，`0` 关闭）一个字节都没有，
  判上游卡死、主动断开，同样是错误事件 + 不扣费。上游自己的保活字节（如 SSE 注释、Anthropic 的 ping）会重新计时。
- **工具调用**：按上游的序号归组（不按调用编号 —— 有的上游不给编号或几次调用编号相同），对外 `index` 从 0 连续；
  调用编号在 `tool_calls` 首块里就给出，上游没给时网关合成一个 `call_…`，流里、聚合结论与下一轮要带回的
  `tool_call_id` 是同一个。名字之前就到的入参攒着，在首块之后补发。
- **服务更新**：进程退出时在途的对话先跑一段宽限（`SHUTDOWN_TIMEOUT` 的一半，1–10 秒），之后主动中止：
  错误事件带 `aegis_code: 40231`（503），暂扣整笔放回、不扣费；客户端重试会落到新实例上。

传输层参数（环境变量）：

| 变量 | 缺省 | 作用 |
|---|---|---|
| `WRITE_TIMEOUT` | `0`（不限） | 整个响应的绝对期限。**必须是 0**：任何有限值都会把长流拦腰截断。流式与同步保活的处理器会按连接解除写期限，其余接口（含关掉保活的同步调用、控制台的其它长连接）仍受它约束。 |
| `AI_RELAY_MAX_BODY_BYTES` | 32 MiB | `/api/ai/v1` 的 JSON 请求体上限（Agent 多轮对话带着工具结果与截图） |
| `AI_RELAY_READ_TIMEOUT` | `5m` | `/api/ai/v1` 写请求读完请求体的期限（覆盖全局 5 秒的 `READ_TIMEOUT`） |
| `AI_UPSTREAM_IDLE_TIMEOUT` | `5m` | 上游空闲看门狗，见上 |

`/api/ai/v1/` 的请求体不交给 WAF（Coraza）检查，只检查请求头与 URI：对话历史里的源码、命令、路径会让 CRS 规则
成片误报，大请求体还会超过 WAF 的检查上限。限流、封禁照常生效，鉴权在处理器里。

## 计价

**单位是「点」，1 点按 1 积分定价**。库里一律存微点整数（1 点 = 10⁶ 微点），不用浮点。

价格挂在模型上（`ai_gateway_models`），**只有登记了的模型能调用**，没定价的模型 404；
全部价格为 0 的模型必须显式标记 `free`，否则保存时拒绝 —— 不存在「忘了配价格于是白用」。

| 价格项 | 单位 |
|---|---|
| `inputPrice` / `outputPrice` | 点 / 百万 token（数值上恰好等于「微点 / token」） |
| `requestPrice` | 点 / 次 |
| `imagePrice` + `imagePrices` | 点 / 张；分档键 `1024x1024`、`hd`、`hd:1024x1792`，按「质量:尺寸 > 尺寸 > 质量 > 默认」取 |
| `charPrice` | 点 / 百万字符 |
| `minutePrice` | 点 / 分钟音频 |

最终价格 = 计量 × 单价 × **倍率**，向上取整到 1 微点。倍率 = 全局倍率 ×（会员时）会员倍率 × 等级倍率
（`levelMultipliers` 取用户等级满足的最高一档）。模型的 `upstreamModel` 是发给上游的名字，
`configId` 非 0 时钉死一条通道、不回退。暂扣时把价格与倍率**快照**进调用记录，调用进行中改价不影响这一笔。

## 扣费来源

| 来源 | 说明 |
|---|---|
| `free_daily` | 每日免费额度，所有用户，按应用时区的日历日重置 |
| `vip_daily` / `vip_monthly` | 会员每日 / 每月额度，有效会员（含永久）才有 |
| `credit` | 赠送额度：看激励广告（权益 `ai_credit`）、管理员调整、退款进来，不过期 |
| `integral` | 积分：1 点扣 1 积分 |
| `wallet` | 余额：按 `walletRate`（元 / 点）折算；是真钱，默认不启用 |

设置里的 `sources` 是**启用的来源及其顺序**，费用按顺序依次承担。
另有 `dailySpendLimit`（每人每天全部来源合计）、`rpm`、`maxConcurrent`、`maxKeysPerUser` 与密钥自己的 `spendLimit`。
频率与并发在锁住用户行之后按调用记录数，是准数，不依赖 Redis。

## 什么时候扣费

| 情形 | 结论 | 记录状态 |
|---|---|---|
| 上游成功 | 按上游报回的用量结算 | `succeeded` |
| 上游成功但没报用量 | 按内容估算结算，记录标 `usageEstimated` | `succeeded` |
| 上游失败、网关出错、客户端在拿到任何内容前断开 | **整笔放回，不扣费** | `failed` |
| 流式输出到一半**上游**中断（含流没给结束原因就断开、上游空闲看门狗断开） | 不扣费 | `failed` |
| 进程退出时主动中止的在途对话（40231） | 不扣费 | `failed` |
| 流式输出到一半**客户端**断开 | 按上游已生成的内容估算输出 token 计费 | `aborted` |
| 暂扣超时未结算（进程中断等） | 后台循环放回，不扣费 | `expired` |
| 超时放回后上游结果才回来 | 照常结算，从实时余额扣 | `succeeded` |
| 管理员整笔退款 | 原路退回各来源 | `refunded` |

客户端断开要计费，是为了不让「快结束时主动断开」变成白用；上游的钱在那时已经花了。

## 账务：暂扣 → 结算

1. **暂扣**（调用前，单事务）：锁 `users` → `ai_gateway_accounts` → `ai_gateway_keys` → 周期额度行 → `user_wallets`
   （与全库 users → wallets 的锁序一致）。按估算价格从各来源取：积分从 `users.integral` 移出，
   余额从 `balance` 移进 `frozen`，额度记 `reserved`，赠送额度移进 `credit_frozen`。**暂扣不写任何流水。**
   余额不够整笔估算时，对话把 `max_tokens` 压到付得起的数量（至少 16），其余接口直接 402。
2. **结算**（成功后，单事务）：先把暂扣放回，再按实际价格扣，**积分、余额、赠送额度各至多写一条消费流水**。
   流水上看到的就是「余额 → 余额 − 价格」，失败的调用在积分与余额流水里什么都不留。
3. **放回**（失败）：原样放回，不写流水。

**估算宁高勿低**：文本按 UTF-8 字节数 / 3、每张图 1000 token、每条消息 4 token 开销；没带 `max_tokens` 时
输出按 `defaultMaxTokens` 估。实际用量超出暂扣时，按扣费顺序从实时余额补，补不上的记 `shortfall`
（只会少收，不会把余额扣成负数）。

**零头**：积分与余额只认整数单位（1 积分、1 分）。暂扣**向上**取整，结算把本次微点加到账户的零头上
（`integral_carry` / `wallet_carry`）再**向下**取整扣整数部分。于是任意多次调用累计扣掉的整数单位与累计价格之差
永远小于一个单位，且结算永远不超过暂扣（`TestChargeWithCarryNeverExceedsHold`）。退款按 `RefundWithCarry`
反向结算，保持「已扣单位 × 单位 + 零头 = 有效计价之和」（`TestRefundWithCarryKeepsLedgerBalanced`）。

**幂等**：带 `Idempotency-Key` 时，同一用户同一个键已有进行中或成功的调用就返回 409（40213），
失败与超时放回的不占位、可以重试。全局的幂等重放中间件对 `/api/ai/v1/*` 关闭（SSE 与大图不能进 Redis 缓存）。

积分流水分类 `ai_gateway`（扣减）与 `ai_gateway_refund`（退款）都登记为**不计入**排行榜周期榜；
钱包退款的流水类型是 `ai_refund`，不计入累计充值。

## 退款

管理员对 `succeeded` / `aborted` 的调用整笔退款（`POST …/ai-gateway/requests/{requestNo}/refund`，guarded 防重放）：
积分写一条 `ai_gateway_refund` 流水、余额写一条 `ai_refund` 流水、赠送额度写一条 `refund` 流水；
周期额度若还在当期就减回已用，已过期的退为赠送额度（退回一个已结束的周期等于没退）。不能退两次（40218）。

## 接口

用户端（接入网关，需登录，`client.ai` / Kotlin `AegisAIGatewayApi`）：

| 接口 | 说明 |
|---|---|
| `GET /ai/overview` | 是否开放、扣费顺序、各额度余量、积分与余额、倍率、今日已用、`baseUrl` |
| `GET /ai/models` | 可调用模型与**有效单价**（已乘本人倍率），会员专享而非会员时 `available=false` |
| `GET/POST /ai/keys`、`PUT/DELETE /ai/keys/{keyId}` | 密钥管理；修改只改传了的字段 |
| `GET /ai/requests`、`/ai/requests/{requestNo}` | 调用记录（用户端不含上游通道与上游模型名） |
| `GET /ai/credits` | 赠送额度流水 |

管理端 `/api/admin/apps/{appkey}/ai-gateway/*`：`catalog`、`settings`、`models`、`requests`（含 `refund`）、
`requests/summary`、`requests/live`、`keys`（含 `status`）、`stats`、`users/{userId}/account`、
`users/{userId}/credits`（GET 流水 / POST 调整，`requestId` 幂等）。权限沿用应用级 `app:read` / `app:write`。
`requests` 另收 `keyword`（调用编号或上游请求编号，前缀匹配）与 `configId`（实际使用的通道）。

## 调用追踪

调用记录只留计费需要的东西；追踪是给管理员排查用的，**只在管理端**，用户端的调用记录不带。

记录行上的摘要（列表直接显示）：`attempts`（上游尝试次数，>1 即发生过链路回退）、`firstTokenMs`（流式首字，
从暂扣成功算起）、`finishReason`、`upstreamRequestId`（成功那次尝试的上游请求编号）、`requestBytes`、
`responseBytes`、`configName`（通道名，联表取）。

详情在 `ai_gateway_request_traces`（迁移 `000122`），单条查询 `GET …/requests/{requestNo}` 的 `trace` 字段：

- `attempts[]`：链路里每一次上游调用 —— 通道、协议、是否平台共享、上游模型名、URL（**只有主机与路径**，
  Gemini 把密钥放在查询串里）、HTTP 状态、上游请求编号（`x-request-id` / `request-id` / `cf-ray` 等）、
  `headersMs` 首包、`firstTokenMs` 首字、`latencyMs`、请求与响应字节数、限流余量（`x-ratelimit-*` /
  `anthropic-ratelimit-*` / `retry-after`）、错误与是否可重试。状态 `succeeded` / `failed` / `canceled`（客户端断开）。
- `request` / `response`：内容摘要。对话记参数、系统提示、最后 20 条消息、工具名；其余接口记请求体。
  长文本截到 2000 字（JSON 里的字符串 1000 字）、base64 与 data URL 换成 `[image/png · 1.2 MB]` 占位、
  长向量只记个数、`api_key` 一类的键去掉；表单里的文件只记字段名、文件名、类型与大小。
  流式中途断开时 `response.partial=true`，是已生成的部分。
- 设置 `redactContent` 打开时不留提示词与正文，只留参数与 `[已隐藏 N 字]`。

采集方式：中转把 `aiTrace` 放进 ctx，链路循环每换一条通道记一次尝试，上游 HTTP 客户端外包的
`aiTraceTransport` 从同一个 ctx 取到它记 HTTP 细节（`service/ai_gateway_trace.go`）。追踪在结算或放回之后写，
不进计费事务，写失败只记日志。

## 实时

`GET …/ai-gateway/requests/live` 是 SSE（读响应体要带 `Authorization`，浏览器的 `EventSource` 带不了头，
控制台用 `fetch` 读流）。事件：

| 事件 | 何时 | 数据 |
|---|---|---|
| `ready` | 连上 | `appid` |
| `created` | 暂扣成功 | `request`（列表形状，`status=reserved`，带 `live` 进度） |
| `attempt` | 开始 / 收到响应头 / 结束一次上游尝试 | `attempts`、`progress` |
| `progress` | 首字到达；流式进行中至多每秒一次 | `progress`：已用时、首字、已生成字数与估算 token |
| `finished` | 结算、放回、超时放回、退款 | `request`（终态）、`attempts` |
| `resync` | 这条连接丢过事件 | 控制台应重新拉一次列表 |
| `bye` | 连接满 30 分钟 | 控制台重连 |

每 15 秒一条 `: ping` 注释行保活。多实例经 NATS 主题 `aegis.aigateway.live` 互通（调用落在哪个实例都推得到），
没有 NATS 时只推本机。推送尽力而为，不保证送达；控制台断线时退回定时刷新。

`GET …/requests/summary?minutes=60`（5–1440）：进行中、总数、成功 / 失败、发生回退的、用户数、token、扣费、
平均与 P95 耗时、平均首字，以及按通道汇总的上游尝试（次数、失败、平均耗时、最近一次错误）。

## 错误码（402xx）

| 码 | HTTP | 含义 |
|---|---|---|
| 40201 | 402 | 余额不足（提示里带预计需要与当前可用） |
| 40202 | 403 | 应用未开启 AI 中转 |
| 40203 | 404 | 模型不存在或未开放 |
| 40204 | 400 | 模型不支持这个接口 |
| 40205 | 403 | 仅会员可用（整个网关或这个模型） |
| 40206 | 401 | 密钥或令牌无效 |
| 40207 | 401 | 密钥已停用或过期 |
| 40208 | 403 | 密钥不允许这个模型 |
| 40209 | 402 | 密钥消费上限已用完 |
| 40210 | 402 | 今日消费已达上限 |
| 40211 / 40212 | 429 | 每分钟请求数 / 同时进行的请求数超限 |
| 40213 | 409 | 幂等键已被进行中或已成功的调用占用 |
| 40214 | 401 | 应用不接受登录令牌 |
| 40215 / 40216 | 409 / 404 | 密钥数量上限 / 密钥不存在 |
| 40217 / 40218 | 404 / 409 | 调用记录不存在 / 不可退款 |
| 40219 / 40220 / 40221 | 400 / 400 / 409 | 设置或入参不合法 / 钉的通道不可用 / 模型重名 |
| 40222 | 424 | 没有支持该接口的通道（非对话接口需要 OpenAI 协议通道） |
| 40223 | 400 | 请求体不合法 |
| 40224 / 40225 | 409 | 赠送额度不足以扣减 / 调整的请求编号被挪用 |
| 40226 / 40227 | 404 / 409 | 异步任务不存在或结果已过期 / 任务还没有完成（取结果原文时） |
| 40228 / 40229 | 502 / 409 | 模型目录刷新失败（models.dev 拉不到）/ 模型目录还没有拉取过 |
| 40231 | 503 | 服务正在更新，在途对话已中止且不扣费（流式时以错误事件给出），重试即可 |

`/api/ai/v1` 的错误按 OpenAI（`{"error":{"message","type","code","aegis_code"}}`）或
Anthropic（`{"type":"error","error":{"type","message","aegis_code"}}`）形状回（流式开流之后见「流式」）：402 是 `insufficient_quota` / `billing_error`，
429 是 `rate_limit_error`（SDK 会自动退避重试）。上游的 400 / 413 / 422 原样告诉调用方，
上游 401 / 403 是通道凭据问题，按 502 报。

## 验证

```bash
go test ./internal/domain/aigateway/              # 计价、零头、退款恒等式、估算、用量解析
# 真实 Postgres（测试会 DROP SCHEMA public，切勿指向开发库）
AEGIS_TEST_PG_DSN=postgres://…/aegis_aig_test go test ./internal/service -run TestAIGatewayBillingIntegration
AEGIS_TEST_PG_DSN=postgres://…/aegis_aig_test go test ./internal/transport/http -run TestAIRelayHTTPIntegration
go test ./internal/service -run 'LiveHub|AITrace'   # 实时推送的分发与补 resync、追踪的尝试与首字
```

中转集成测试还覆盖异步任务（提交 202、长轮询拿到内嵌结果并扣费、另一把密钥可取、取消不扣费、上游拒绝的错误信封、
语音的 result_url 与原文、后台判中断与删过期结果）和同步保活（前导空格、保活后出错、关掉保活）。

中转集成测试里链路是「总回 503 的通道 → 正常的假上游」，核对两次尝试的状态、上游请求编号、限流余量、
首包与字节数，SSE 的 ready / created / finished，按上游请求编号搜索，隐藏内容，以及概况里按通道的汇总。

计费集成测试经真实通道链路调 httptest 假上游，覆盖零头凑整、失败不扣费、余额不足、压低输出上限、免费额度、
赠送额度幂等、余额冻结、溢出 shortfall、流式断开与上游中断、图像按实际张数、密钥上限与停用、过期回收与迟到结算、
退款、30 路并发抢余额，最后核对全局不变式（无残留暂扣、冻结归零、零头在界内、积分非负、没有暂扣流水）。

## 未做

- `/v1/responses`、`/v1/completions`（旧版补全）、实时语音（Realtime）、视频生成接口。
- 调用记录与追踪没有自动清理，量大后需要按 `created_at` 归档或分区。
- 缓存命中与推理 token 没有单独计量（统一协议的 `Usage` 只有输入、输出与合计；推理 token 含在输出里）。
- 思考签名只记在本实例内存，多实例的续轮可能退化为不开思考（见「思考」）。
- 进程退出时只主动中止在途的**对话**（流式与非流式）；同步的原样转发（图像、语音等）仍等 `SHUTDOWN_TIMEOUT`，
  被硬切时暂扣按 `HoldTTL`（30 分钟）过期放回。进程崩溃（非正常退出）时流式暂扣仍要等 `StreamHoldTTL`（2 小时）
  才放回，期间占着用户的并发名额（40212）。
- 赠送额度不过期；激励广告发的额度与管理员发的额度不分开记余额。
