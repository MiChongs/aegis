"""远程函数：能力目录、函数增改删、静态检查与试跑、版本发布激活与回滚、真实调用、调用审计与重放、运行统计与全景、触发器与触发任务、KV、调用密钥。

远程函数是挂在单个应用下的服务端自定义 API。三种运行时：script（控制台里写的 JavaScript，按声明的能力读写平台数据）、
wasm（上传的纯计算模块）、http（转发到接入方自建的 HTTPS 端点，双向 Ed25519 签名）。接入方用用户令牌或 afk_ 函数密钥调用
/api/apps/:appkey/functions/:name/invoke，只会解析到「已启用且有激活版本」的那一版。

写脚本的流程：functions_catalog 看能力（capabilities、groups）、平台事件（triggerEvents）、可发物品（grantItems）、额度与模板 →
functions_create 建函数并声明能力 → 写脚本 → functions_analyze 静态检查 → functions_test 试跑（读是真的、写只记录）→
functions_publish_version 发布（不可修改）→ functions_activate_version 激活（回滚就是激活旧版本）。
脚本对用户的写操作一律锁定到当前调用者；整个脚本不是一个事务，中途失败不回滚先前的写入。

权限：读需要 app:read，其余（含静态检查、试跑与重放）需要 app:write。
"""

from __future__ import annotations

from typing import Annotated, Any, Literal

from fastmcp import FastMCP
from fastmcp.exceptions import ToolError
from pydantic import Field

from ..core import DELETE, DESTRUCTIVE, GET, POST, PUT, READ, WRITE, AppRef, Limit, Page, body, q

NAMESPACE = "functions"
server = FastMCP("functions")

A = "/api/admin/apps/:appkey"
F = f"{A}/functions/:functionName"
CATALOG = GET(f"{A}/function-catalog")
OVERVIEW = GET(f"{A}/function-overview")
APP_TRIGGERS = GET(f"{A}/function-triggers")
FUNCTIONS = GET(f"{A}/functions")
FUNCTION_CREATE = POST(f"{A}/functions")
FUNCTION = GET(F)
FUNCTION_UPDATE = PUT(F)
FUNCTION_DELETE = DELETE(F)
VERSIONS = GET(f"{F}/versions")
VERSION_CREATE = POST(f"{F}/versions")
VERSION = GET(f"{F}/versions/:version")
VERSION_DELETE = DELETE(f"{F}/versions/:version")
VERSION_ACTIVATE = POST(f"{F}/versions/:version/activate")
ANALYZE = POST(f"{F}/analyze")
TEST = POST(f"{F}/test")
INVOKE = POST(f"{F}/invoke")
INVOCATIONS = GET(f"{F}/invocations")
INVOCATION = GET(f"{F}/invocations/:invocationId")
INVOCATION_REPLAY = POST(f"{F}/invocations/:invocationId/replay")
STATS = GET(f"{F}/stats")
TRIGGERS = GET(f"{F}/triggers")
TRIGGER_CREATE = POST(f"{F}/triggers")
TRIGGER_UPDATE = PUT(f"{F}/triggers/:triggerId")
TRIGGER_DELETE = DELETE(f"{F}/triggers/:triggerId")
TRIGGER_FIRE = POST(f"{F}/triggers/:triggerId/fire")
TRIGGER_JOBS = GET(f"{F}/trigger-jobs")
TRIGGER_JOB_RETRY = POST(f"{F}/trigger-jobs/:jobId/retry")
KV = GET(f"{A}/function-kv")
KV_DELETE = DELETE(f"{A}/function-kv")
KEYS = GET(f"{A}/function-keys")
KEY_CREATE = POST(f"{A}/function-keys")
KEY_REVOKE = DELETE(f"{A}/function-keys/:keyId")

FunctionName = Annotated[str, Field(
    min_length=1, max_length=64, pattern=r"^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)*$",
    description="函数名（functions_list 结果里的 name；小写字母开头，可含数字与 . _ -）",
)]
Version = Annotated[str, Field(
    min_length=1, max_length=64, pattern=r"^[0-9A-Za-z][0-9A-Za-z._-]{0,63}$",
    description="版本号（如 1.0.0；functions_versions 结果里的 version）",
)]
InvocationID = Annotated[int, Field(gt=0, description="调用记录 ID（functions_invocations 结果里的 id）")]
TriggerID = Annotated[int, Field(gt=0, description="触发器 ID（functions_triggers 结果里的 id）")]
JobID = Annotated[int, Field(gt=0, description="触发任务 ID（functions_trigger_jobs 结果里的 id）")]
Capabilities = Annotated[list[str] | None, Field(
    max_length=64,
    description="声明的能力（functions_catalog 的 capabilities[].key，如 user.read / points.write / kv.write / rights.grant）。"
                "没声明的能力在脚本里根本不会被绑定。rights.grant / vip.write / wallet.write / user.write / email.send / http.fetch "
                "等高风险能力（目录里 risk=high 的）勾选前先向用户确认",
)]
Config = Annotated[dict[str, Any] | None, Field(description="函数配置（JSON 对象，≤32KB），脚本里读作 aegis.config；改了立即生效、不必发版。永不下发给接入方")]
InputSchema = Annotated[dict[str, Any] | None, Field(description="入参契约（JSON Schema 对象，{} 为不约束）：调用入口先按它校验 input，接入方能看到")]
TimeoutMs = Annotated[int | None, Field(ge=10, le=30000, description="单次执行超时毫秒（10–30000，默认 500）")]
MaxConcurrency = Annotated[int | None, Field(ge=1, le=64, description="单实例同时执行上限（1–64，默认 8；超出返回 42990）")]
RateLimit = Annotated[int | None, Field(ge=0, le=600000, description="每分钟调用上限（跨实例准确，0 为不限；超出返回 42991）")]
MaxBytes = Annotated[int | None, Field(ge=1, description="字节上限（留空用默认）")]
Source = Annotated[str, Field(max_length=262144, description="脚本正文（JavaScript，入口为同步的 function handle(ctx)，≤256KB）；留空则用当前激活版本的正文")]
UserRef = Annotated[int | None, Field(gt=0, description="以这个应用用户的身份执行（管理端用户 ID）；脚本里 aegis.user 等用户级能力锁定到他")]


async def _active_source(app: str, function_name: str) -> str:
    """取当前激活版本的脚本正文（analyze / test 不给 source 时用）。"""
    function = await FUNCTION(appkey=app, functionName=function_name)
    active = (function or {}).get("activeVersion") if isinstance(function, dict) else None
    if not active:
        raise ToolError(f"函数 {function_name} 还没有激活版本，请直接传 source")
    detail = await VERSION(appkey=app, functionName=function_name, version=active)
    source = (detail or {}).get("source") if isinstance(detail, dict) else None
    if not source:
        raise ToolError(f"激活版本 {active} 没有脚本正文（不是 script 运行时？），请直接传 source")
    return source


# ── 目录与全景 ──

@server.tool(**READ("functions"))
async def catalog(app: AppRef) -> Any:
    """远程函数的能力目录、平台事件、可发物品、额度与模板；写脚本、声明能力、建触发器之前先看它。

    capabilities（能力键、注入的 SDK 对象、风险档 risk、是否需要用户身份）、groups（能力分组）、
    triggerEvents（可订阅的平台事件及事件字段）、grantItems（rights.grant 可发的物品）、limits（SDK 调用次数、写次数、出站、
    正文、KV、配置等额度）、templates（内置脚本模板，含契约与样例）与 SDK 类型声明。"""
    return await CATALOG(appkey=app)


@server.tool(**READ("functions"))
async def overview(
    app: AppRef,
    window_hours: Annotated[int, Field(ge=1, le=168, description="统计最近 N 小时（≤168）")] = 24,
) -> Any:
    """应用下全部函数的运行全景：调用量、成功率、来源分布、触发器积压，以及「为什么调不通」。

    含各函数状态与激活版本、耗时、按来源（api / event / schedule / console / function）分布、触发器与积压的触发任务、
    未启用或没有激活版本等原因。"""
    return await OVERVIEW(appkey=app, query={"windowHours": window_hours})


# ── 函数 ──

@server.tool(name="list", **READ("functions"))
async def list_functions(app: AppRef) -> Any:
    """函数列表：名称、说明、运行时（script / wasm / http）、状态（draft / active / disabled）、激活版本、声明的能力与闸门。"""
    return await FUNCTIONS(appkey=app)


@server.tool(name="get", **READ("functions"))
async def get_function(app: AppRef, function_name: FunctionName) -> Any:
    """一个函数的完整定义：能力、超时与并发 / 频次闸门、函数配置 config、入参契约 inputSchema（及生成的 TypeScript 类型）、激活版本。"""
    return await FUNCTION(appkey=app, functionName=function_name)


@server.tool(**WRITE("functions"))
async def create(
    app: AppRef,
    name: FunctionName,
    runtime: Annotated[Literal["script", "wasm", "http"], Field(description="script 控制台写 JavaScript（能读写平台数据，主路径）/ wasm 纯计算模块 / http 转发到自建 HTTPS 端点")] = "script",
    description: Annotated[str, Field(max_length=500, description="函数说明")] = "",
    capabilities: Capabilities = None,
    timeout_ms: TimeoutMs = None,
    max_request_bytes: MaxBytes = None,
    max_response_bytes: MaxBytes = None,
    max_concurrency: MaxConcurrency = None,
    rate_limit_per_min: RateLimit = None,
    config: Config = None,
    input_schema: InputSchema = None,
) -> Any:
    """新建函数（草稿，没有版本，接入方还调不到）。名字建好后不能改。

    之后：写脚本 → functions_analyze → functions_test → functions_publish_version → functions_activate_version，
    再用 functions_update 把状态改为 active 才对接入方开放。能力只声明脚本真正用到的；高风险能力先向用户确认。
    """
    return await FUNCTION_CREATE(appkey=app, body=body(
        name=name, description=description, runtime=runtime, capabilities=capabilities, timeoutMs=timeout_ms,
        maxRequestBytes=max_request_bytes, maxResponseBytes=max_response_bytes, maxConcurrency=max_concurrency,
        rateLimitPerMin=rate_limit_per_min, config=config, inputSchema=input_schema,
    ))


@server.tool(**WRITE("functions"))
async def update(
    app: AppRef,
    function_name: FunctionName,
    description: Annotated[str | None, Field(max_length=500, description="函数说明")] = None,
    status: Annotated[Literal["draft", "active", "disabled"] | None, Field(description="draft 草稿 / active 启用（接入方可调）/ disabled 停用（立即拒绝全部调用）")] = None,
    capabilities: Annotated[list[str] | None, Field(max_length=64, description="新的能力清单（整体替换；传空数组即收回全部能力）。高风险能力先向用户确认")] = None,
    timeout_ms: TimeoutMs = None,
    max_request_bytes: MaxBytes = None,
    max_response_bytes: MaxBytes = None,
    max_concurrency: MaxConcurrency = None,
    rate_limit_per_min: RateLimit = None,
    config: Annotated[dict[str, Any] | None, Field(description="新的函数配置（整体替换，JSON 对象），改完立即生效")] = None,
    input_schema: Annotated[dict[str, Any] | None, Field(description="新的入参契约（整体替换，JSON Schema 对象）")] = None,
) -> Any:
    """修改函数：只传要改的字段，其余不变。改动对之后的每次调用立即生效（不需要发新版本）。

    启用 / 停用、收回能力、改闸门都直接影响线上调用：执行前向用户复述改动。
    """
    request = body(
        description=description, status=status, capabilities=capabilities, timeoutMs=timeout_ms,
        maxRequestBytes=max_request_bytes, maxResponseBytes=max_response_bytes, maxConcurrency=max_concurrency,
        rateLimitPerMin=rate_limit_per_min, config=config, inputSchema=input_schema,
    )
    if not request:
        raise ToolError("没有要修改的字段")
    return await FUNCTION_UPDATE(appkey=app, functionName=function_name, body=request)


@server.tool(name="delete", **DESTRUCTIVE("functions"))
async def delete_function(app: AppRef, function_name: FunctionName) -> Any:
    """删除函数及其全部版本与触发器（不可恢复），接入方此后调用一律失败。只想暂停用 functions_update(status=disabled)。执行前先向用户确认函数名。"""
    return await FUNCTION_DELETE(appkey=app, functionName=function_name)


# ── 检查与试跑 ──

@server.tool(**WRITE("functions", idempotent=True))
async def analyze(app: AppRef, function_name: FunctionName, source: Source = "") -> Any:
    """静态检查脚本（不执行、无副作用），返回带行列的诊断；只有 error 挡发布。

    检查语法、handle 入口、async、用了没声明的能力（指名该补哪一项）、禁用全局、拼错的成员、
    危险写法、无出口循环、声明了没用的能力。发布走的是同一套判定。需要 app:write 权限。"""
    if not source.strip():
        source = await _active_source(app, function_name)
    return await ANALYZE(appkey=app, functionName=function_name, body={"source": source})


@server.tool(**WRITE("functions", idempotent=True))
async def test(
    app: AppRef,
    function_name: FunctionName,
    source: Source = "",
    input: Annotated[dict[str, Any] | None, Field(description="ctx.input（JSON 对象）")] = None,
    config: Annotated[dict[str, Any] | None, Field(description="本次试跑用的 aegis.config（不给则用函数当前配置）")] = None,
    as_user_id: UserRef = None,
    timeout_ms: Annotated[int | None, Field(ge=10, le=30000, description="本次试跑超时毫秒")] = None,
    trigger_kind: Annotated[Literal["", "event", "schedule"], Field(description="模拟触发器拉起（ctx.trigger）：event 平台事件 / schedule 定时；留空为普通调用")] = "",
    trigger_event: Annotated[str, Field(description="trigger_kind=event 时的事件键（functions_catalog 的 triggerEvents[].key，如 user.registered）")] = "",
    trigger_cron: Annotated[str, Field(description="trigger_kind=schedule 时的 Cron")] = "",
    trigger_data: Annotated[dict[str, Any] | None, Field(description="ctx.trigger.data（事件字段，如签到 {consecutiveDays: 7}）")] = None,
) -> Any:
    """试跑脚本：不建版本、不写调用审计、不产生真实副作用，发布前必跑。

    读是真的（查真实用户、KV、会员），写只记录为 simulated effect，非 GET 出站请求跳过。用函数已声明的能力（不能临时加）。失败也正常返回，看 ok、error、errorLine、stack、logs、effects。
    source 留空则试跑当前激活版本。需要 app:write 权限。"""
    if not source.strip():
        source = await _active_source(app, function_name)
    if not trigger_kind and (trigger_event or trigger_cron or trigger_data):
        raise ToolError("给了 trigger_event / trigger_cron / trigger_data 时需要 trigger_kind")
    if trigger_kind == "event" and not trigger_event.strip():
        raise ToolError("trigger_kind=event 需要 trigger_event")
    trigger = body(kind=trigger_kind, event=trigger_event.strip() or None, cron=trigger_cron.strip() or None,
                   data=trigger_data) if trigger_kind else None
    return await TEST(appkey=app, functionName=function_name, body=body(
        source=source, input=input if input is not None else {}, config=config, asUserId=as_user_id,
        timeoutMs=timeout_ms, trigger=trigger,
    ))


# ── 版本 ──

@server.tool(**READ("functions"))
async def versions(app: AppRef, function_name: FunctionName) -> Any:
    """版本列表（新的在前）：版本号、产物摘要、备注、发布人、时间、是否激活。正文用 functions_get_version 取。"""
    return await VERSIONS(appkey=app, functionName=function_name)


@server.tool(**READ("functions"))
async def get_version(app: AppRef, function_name: FunctionName, version: Version) -> Any:
    """一个版本的详情，含脚本正文 source（仅管理端可见）。改脚本时先取回当前版本正文再改。"""
    return await VERSION(appkey=app, functionName=function_name, version=version)


@server.tool(**WRITE("functions"))
async def publish_version(
    app: AppRef,
    function_name: FunctionName,
    version: Version,
    source: Annotated[str, Field(max_length=262144, description="script 运行时：脚本正文（≤256KB）")] = "",
    notes: Annotated[str, Field(max_length=500, description="版本备注（改了什么）")] = "",
    endpoint_url: Annotated[str, Field(description="http 运行时：HTTPS 端点地址")] = "",
    response_public_key: Annotated[str, Field(description="http 运行时：端点响应签名的 Ed25519 公钥")] = "",
    wasm_base64: Annotated[str, Field(description="wasm 运行时：模块的 Base64（≤2MB）")] = "",
) -> Any:
    """发布一个新版本（不可修改，版本号不能重复）。script 版本发布前后端会跑一遍静态检查，有 error 直接拒绝。

    发布不等于生效：调用仍走当前激活版本，要切换用 functions_activate_version。发布前先 functions_analyze 与 functions_test。
    """
    if not (source.strip() or endpoint_url.strip() or wasm_base64.strip()):
        raise ToolError("需要 source（script）、endpoint_url（http）或 wasm_base64（wasm）之一")
    return await VERSION_CREATE(appkey=app, functionName=function_name, body=body(
        version=version, source=source or None, notes=notes or None, endpointUrl=endpoint_url.strip() or None,
        responsePublicKey=response_public_key.strip() or None, wasmBase64=wasm_base64.strip() or None,
    ))


@server.tool(**WRITE("functions", idempotent=True))
async def activate_version(app: AppRef, function_name: FunctionName, version: Version) -> Any:
    """激活一个版本（原子切换，之后的每次调用都跑它）；回滚就是重新激活旧版本。立即影响线上调用，执行前向用户确认版本号。"""
    return await VERSION_ACTIVATE(appkey=app, functionName=function_name, version=version)


@server.tool(**DESTRUCTIVE("functions"))
async def delete_version(app: AppRef, function_name: FunctionName, version: Version) -> Any:
    """删除一个未激活的版本（不可恢复，激活中的版本删不掉）。删了就不能再回滚到它。执行前先向用户确认。"""
    return await VERSION_DELETE(appkey=app, functionName=function_name, version=version)


# ── 调用 ──

@server.tool(**WRITE("functions"))
async def invoke(
    app: AppRef,
    function_name: FunctionName,
    input: Annotated[dict[str, Any] | None, Field(description="ctx.input（JSON 对象，须满足入参契约）")] = None,
    as_user_id: UserRef = None,
    event_id: Annotated[str, Field(description="幂等键：同一 eventId 重复调用直接返回已有结果、不会二次执行；留空自动生成")] = "",
) -> Any:
    """以管理员身份真实调用激活版本（来源记为 console），写操作全部真实执行。

    脚本里的写操作（发积分、开会员、发物品、扣余额、发信……）不能通过本工具撤回。只想看效果用 functions_test。执行前向用户复述函数、身份与入参并取得确认。"""
    return await INVOKE(appkey=app, functionName=function_name, body=body(
        eventId=event_id.strip() or None, input=input if input is not None else {}, asUserId=as_user_id,
    ))


@server.tool(**READ("functions"))
async def invocations(
    app: AppRef,
    function_name: FunctionName,
    status: Annotated[Literal["", "success", "error", "running"], Field(description="只看这种结果，留空为全部")] = "",
    caller_type: Annotated[Literal["", "user", "app", "admin", "event", "schedule", "function"], Field(description="调用者：user 用户令牌 / app 函数密钥 / admin 管理员 / event 事件 / schedule 定时 / function 其他函数")] = "",
    source: Annotated[Literal["", "api", "event", "schedule", "console", "function"], Field(description="来源，留空为全部")] = "",
    event_id: Annotated[str, Field(description="按 eventId 精确查")] = "",
    trigger_id: Annotated[int | None, Field(gt=0, description="只看这个触发器拉起的")] = None,
    caller_id: Annotated[int | None, Field(gt=0, description="只看这个调用者（用户 ID 或密钥 ID）")] = None,
    page: Page = 1,
    limit: Limit = 20,
) -> Any:
    """调用审计（新的在前）：eventId、来源、调用者、版本、状态、耗时、错误码与错误。排障先 status=error；详情用 functions_get_invocation。"""
    return await INVOCATIONS(appkey=app, functionName=function_name, query=q(
        status=status, callerType=caller_type, source=source, eventId=event_id.strip(), triggerId=trigger_id,
        callerId=caller_id, page=page, limit=limit,
    ))


@server.tool(**READ("functions"))
async def get_invocation(app: AppRef, function_name: FunctionName, invocation_id: InvocationID) -> Any:
    """一条调用的完整记录：入参（过大会截断）、输出、脚本日志、实际发生的 effects、SDK 用量、错误与耗时。"""
    return await INVOCATION(appkey=app, functionName=function_name, invocationId=invocation_id)


@server.tool(**WRITE("functions", idempotent=True))
async def replay(app: AppRef, function_name: FunctionName, invocation_id: InvocationID) -> Any:
    """重放一条调用：用它记录下的入参与身份在当前激活版本上试跑（只记不做，同 functions_test）。修了 bug 后用它核对那条失败调用。需要 app:write 权限。"""
    return await INVOCATION_REPLAY(appkey=app, functionName=function_name, invocationId=invocation_id)


@server.tool(**READ("functions"))
async def stats(
    app: AppRef,
    function_name: FunctionName,
    window_hours: Annotated[int, Field(ge=1, le=720, description="统计最近 N 小时（≤720）")] = 24,
) -> Any:
    """一个函数的运行状况：调用量、成功率、耗时分位（p50 / p95 / p99）、Top 错误、按小时分桶。"""
    return await STATS(appkey=app, functionName=function_name, query={"hours": window_hours})


# ── 触发器 ──

@server.tool(**READ("functions"))
async def triggers(
    app: AppRef,
    function_name: Annotated[str, Field(description="只看这个函数的触发器；留空列出整个应用的（并附平台事件目录）")] = "",
) -> Any:
    """触发器列表：种类（event 平台事件 / schedule 定时）、事件或 Cron 与时区、固定入参、启用、下次执行时间、最近触发结果。"""
    if function_name.strip():
        return await TRIGGERS(appkey=app, functionName=function_name.strip())
    return await APP_TRIGGERS(appkey=app)


TriggerInput = Annotated[dict[str, Any] | None, Field(description="每次触发固定带上的 ctx.input（JSON 对象，须满足入参契约）")]
TriggerDescription = Annotated[str | None, Field(max_length=200, description="触发器说明")]


@server.tool(**WRITE("functions"))
async def create_trigger(
    app: AppRef,
    function_name: FunctionName,
    kind: Annotated[Literal["event", "schedule"], Field(description="event 平台事件发生时拉起 / schedule 按 Cron 定时拉起")],
    event: Annotated[str, Field(description="kind=event：事件键（functions_catalog 的 triggerEvents[].key：user.registered / user.login / user.signin / payment.completed ……）")] = "",
    cron: Annotated[str, Field(description="kind=schedule：五段 Cron（如 \"0 9 * * *\"）或 @daily 等描述符")] = "",
    timezone: Annotated[str, Field(description="kind=schedule：IANA 时区（如 Asia/Shanghai），留空用默认")] = "",
    description: TriggerDescription = None,
    input: TriggerInput = None,
    enabled: Annotated[bool, Field(description="建好即启用")] = True,
) -> Any:
    """给函数加一个事件或定时触发器（每个函数最多 16 个），启用后自动真实执行。

    事件触发器在每个用户发生该事件时以他的身份执行，定时触发器到点执行，
    写操作都是真实的。触发经持久队列执行、失败按退避重试。建之前先用 functions_test 的 trigger_kind 模拟一遍，并向用户确认。"""
    if kind == "event" and not event.strip():
        raise ToolError("事件触发器需要 event")
    if kind == "schedule" and not cron.strip():
        raise ToolError("定时触发器需要 cron")
    if kind == "event" and (cron.strip() or timezone.strip()):
        raise ToolError("事件触发器不用 cron / timezone")
    if kind == "schedule" and event.strip():
        raise ToolError("定时触发器不用 event")
    return await TRIGGER_CREATE(appkey=app, functionName=function_name, body=body(
        kind=kind, event=event.strip() or None, cron=cron.strip() or None, timezone=timezone.strip() or None,
        description=description, input=input, enabled=enabled,
    ))


@server.tool(**WRITE("functions"))
async def update_trigger(
    app: AppRef,
    function_name: FunctionName,
    trigger_id: TriggerID,
    event: Annotated[str, Field(description="新的事件键（仅事件触发器），留空不改")] = "",
    cron: Annotated[str, Field(description="新的 Cron（仅定时触发器），留空不改")] = "",
    timezone: Annotated[str, Field(description="新的时区（仅定时触发器），留空不改")] = "",
    description: TriggerDescription = None,
    input: Annotated[dict[str, Any] | None, Field(description="新的固定入参（整体替换），不传不改")] = None,
    enabled: Annotated[bool | None, Field(description="启用或停用，不传不改")] = None,
) -> Any:
    """修改触发器（种类不能改，要换种类请删了重建）。只传要改的字段；说明不传时沿用原值（后端会清空，这里先读后合并）。"""
    if description is None:
        listed = await TRIGGERS(appkey=app, functionName=function_name)
        items = listed.get("items") if isinstance(listed, dict) else listed
        current = next((t for t in items or [] if isinstance(t, dict) and t.get("id") == trigger_id), None)
        if current is None:
            raise ToolError(f"函数 {function_name} 下没有 ID 为 {trigger_id} 的触发器")
        description = current.get("description") or ""
    return await TRIGGER_UPDATE(appkey=app, functionName=function_name, triggerId=trigger_id, body=body(
        event=event.strip() or None, cron=cron.strip() or None, timezone=timezone.strip() or None,
        description=description, input=input, enabled=enabled,
    ))


@server.tool(**DESTRUCTIVE("functions"))
async def delete_trigger(app: AppRef, function_name: FunctionName, trigger_id: TriggerID) -> Any:
    """删除触发器（不可恢复），之后不再自动拉起。只想暂停用 functions_update_trigger(enabled=false)。执行前先向用户确认。"""
    return await TRIGGER_DELETE(appkey=app, functionName=function_name, triggerId=trigger_id)


@server.tool(**WRITE("functions"))
async def trigger_fire(
    app: AppRef,
    function_name: FunctionName,
    trigger_id: TriggerID,
    user_id: Annotated[int | None, Field(gt=0, description="事件触发器必填：模拟这个应用用户发生了该事件（管理端用户 ID）")] = None,
    data: Annotated[dict[str, Any] | None, Field(description="事件数据 ctx.trigger.data（如 {orderId, amount}）")] = None,
) -> Any:
    """立即真实执行一次触发器（同步，不是试跑），脚本的写操作真实生效、写调用审计。

    返回 {ok, eventId, output, effects, error, errorCode}，失败也正常返回、看 ok。只想验证逻辑用 functions_test（带 trigger_kind）。执行前向用户确认。"""
    return await TRIGGER_FIRE(appkey=app, functionName=function_name, triggerId=trigger_id, body=body(userId=user_id, data=data))


@server.tool(**READ("functions"))
async def trigger_jobs(
    app: AppRef,
    function_name: FunctionName,
    trigger_id: Annotated[int | None, Field(gt=0, description="只看这个触发器")] = None,
    status: Annotated[Literal["", "pending", "running", "succeeded", "failed"], Field(description="pending 排队或等待重试 / running / succeeded / failed 重试用尽，留空为全部")] = "",
    page: Page = 1,
    limit: Limit = 20,
) -> Any:
    """触发任务队列（新的在前）：哪个触发器、事件与用户、状态、已试次数、下次重试、最后错误。"""
    return await TRIGGER_JOBS(appkey=app, functionName=function_name, query=q(triggerId=trigger_id, status=status, page=page, limit=limit))


@server.tool(**WRITE("functions"))
async def retry_trigger_job(app: AppRef, function_name: FunctionName, job_id: JobID) -> Any:
    """把一个失败的触发任务放回队列重新执行（真实执行，eventId 不变所以已成功的不会重复）。先看 last error，原因未消除时重试仍会失败。"""
    return await TRIGGER_JOB_RETRY(appkey=app, functionName=function_name, jobId=job_id)


# ── KV ──

KVScope = Annotated[Literal["app", "user"], Field(description="app 应用级共享（aegis.kv）/ user 按用户隔离（aegis.kv.user）")]


@server.tool(**READ("functions"))
async def kv(
    app: AppRef,
    scope: Annotated[Literal["", "app", "user"], Field(description="app 应用级 / user 用户级，留空为全部")] = "",
    scope_id: Annotated[int | None, Field(gt=0, description="scope=user 时只看这个用户（管理端用户 ID）")] = None,
    prefix: Annotated[str, Field(description="键前缀")] = "",
    page: Page = 1,
    limit: Limit = 20,
) -> Any:
    """浏览脚本的服务端状态 KV（应用级，多个函数共用）：键、值（过长截断）、作用域、过期时间（过期的也列出并标注）。查某个用户的配额计数用 scope=user + scope_id。"""
    return await KV(appkey=app, query=q(scope=scope, scopeId=scope_id, prefix=prefix, page=page, limit=limit))


@server.tool(**DESTRUCTIVE("functions"))
async def delete_kv(
    app: AppRef,
    key: Annotated[str, Field(min_length=1, max_length=128, description="要删的键")],
    scope: KVScope = "app",
    scope_id: Annotated[int | None, Field(gt=0, description="scope=user 时的用户 ID（管理端用户 ID）")] = None,
) -> Any:
    """删除一个 KV 键（不可恢复）。计数、配额、已领取标记被删后，脚本会当作从未发生（如允许再领一次）。执行前先向用户确认。"""
    if scope == "user" and not scope_id:
        raise ToolError("scope=user 需要 scope_id")
    return await KV_DELETE(appkey=app, query=q(scope=scope, scopeId=scope_id, key=key))


# ── 调用密钥 ──

@server.tool(**READ("functions"))
async def keys(app: AppRef) -> Any:
    """函数调用密钥（afk_ 开头，供接入方服务端调用）列表：名称、前缀、创建人、最近使用、是否已撤销。不含明文。"""
    return await KEYS(appkey=app)


@server.tool(**WRITE("functions"))
async def create_key(app: AppRef, name: Annotated[str, Field(min_length=1, max_length=64, description="密钥名称（用在哪）")]) -> Any:
    """新建一把函数调用密钥（afk_ 开头），明文只在这次返回、之后取不回。

    它能以应用身份调用本应用全部已启用函数，只能放在服务端或 Worker Secret，
    不能嵌进网页或安装包。把明文交给用户时提醒立即保存。"""
    return await KEY_CREATE(appkey=app, body={"name": name})


@server.tool(**DESTRUCTIVE("functions"))
async def revoke_key(app: AppRef, key_id: Annotated[int, Field(gt=0, description="密钥 ID（functions_keys 结果里的 id）")]) -> Any:
    """撤销函数调用密钥（不可恢复），持有它的服务端此后调用一律 401。执行前先向用户确认密钥名称。"""
    return await KEY_REVOKE(appkey=app, keyId=key_id)
