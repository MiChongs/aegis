"""注册准入（IP 与设备）：策略与按规则编辑、模拟、IP 查询与设备查询（含两边关联）、解除限制、惩罚期、地址与设备名单、拦截记录、注册台账。"""

from __future__ import annotations

from typing import Annotated, Any, Literal

from fastmcp import FastMCP
from fastmcp.exceptions import ToolError
from pydantic import Field

from ..core import (
    DESTRUCTIVE,
    GET,
    POST,
    PUT,
    READ,
    WRITE,
    AppRef,
    IsoTime,
    Limit,
    Page,
    body,
    q,
)

NAMESPACE = "register"
server = FastMCP("register")

BASE = "/api/admin/apps/:appkey/register-ip"
OVERVIEW = GET(BASE)
POLICY = GET(f"{BASE}/policy")
POLICY_SAVE = PUT(f"{BASE}/policy")
SIMULATE = POST(f"{BASE}/simulate")
INSPECT_IP = GET(f"{BASE}/inspect")
INSPECT_DEVICE = GET(f"{BASE}/devices/inspect")
RELEASE = POST(f"{BASE}/release")
PENALTIES = GET(f"{BASE}/penalties")
EVENTS = GET(f"{BASE}/events")
REGISTRATIONS = GET(f"{BASE}/registrations")
IP_LISTS = GET(f"{BASE}/lists")
IP_LISTS_ADD = POST(f"{BASE}/lists")
IP_LIST_UPDATE = PUT(f"{BASE}/lists/:id")
IP_LISTS_DELETE = POST(f"{BASE}/lists/delete")
DEVICE_LISTS = GET(f"{BASE}/device-lists")
DEVICE_LISTS_ADD = POST(f"{BASE}/device-lists")
DEVICE_LIST_UPDATE = PUT(f"{BASE}/device-lists/:id")
DEVICE_LISTS_DELETE = POST(f"{BASE}/device-lists/delete")

Scope = Literal["ip", "subnet", "device", "ip_devices", "device_ips", "city", "app"]
Action = Literal["reject", "challenge", "observe"]
DeviceAction = Literal["allow", "challenge", "reject"]
Method = Literal["password", "sms", "oauth", "cardkey"]
Target = Annotated[Literal["ip", "device"], Field(description="名单对象：ip 地址名单（单个地址或 CIDR 网段）/ device 设备名单（设备标识）")]
ListKind = Annotated[Literal["allow", "deny", "elevated"], Field(description="allow 放行（不受任何规则与惩罚期约束）/ deny 拒绝 / elevated 放宽（配额乘 factor）")]
Tz = Annotated[str, Field(description="IANA 时区（如 Asia/Shanghai），按它划分自然日；留空用服务器默认")]
IP = Annotated[str, Field(description="IPv4 或 IPv6 地址")]
Device = Annotated[str, Field(description="设备标识（客户端上报的 X-Device-Id），不分大小写")]

SCOPE_HELP = (
    "统计主体：ip 同一地址（IPv6 按 /64 聚合）、subnet 同一网段、device 同一设备、"
    "ip_devices 同一地址下的不同设备数（群控）、device_ips 同一设备用过的不同地址数（代理池）、city 同一城市、app 整个应用"
)


# ═══════════════════════ 概览与策略 ═══════════════════════


@server.tool(**READ("register"))
async def overview(
    app: AppRef,
    days: Annotated[int, Field(ge=0, le=90, description="统计天数，0 用默认 7 天")] = 0,
    tz: Tz = "",
) -> Any:
    """注册准入概览：注册与拦截趋势、拦截原因、规则命中、每地址 / 每设备账号数分布、地址 / 网段 / 设备排行、设备最多的地址与换地址最多的设备、正在惩罚期的地址与设备、此刻的突增状态、名单数量。结果里带当前策略。"""
    return await OVERVIEW(appkey=app, query=q(days=days or None, tz=tz))


@server.tool(**READ("register"))
async def policy(app: AppRef) -> Any:
    """读取注册准入策略。configured=false 表示还没保存过、按旧开关「注册 IP 唯一」推导；defaults 是推荐起点。"""
    return await POLICY(appkey=app)


async def _current(app: str) -> dict[str, Any]:
    view = await POLICY(appkey=app) or {}
    return dict(view.get("policy") or {})


def _merge(current: dict[str, Any], key: str, patch: dict[str, Any] | None) -> dict[str, Any]:
    return {**(current.get(key) or {}), **(patch or {})}


@server.tool(**WRITE("register", idempotent=True))
async def update_policy(
    app: AppRef,
    enabled: Annotated[bool | None, Field(description="是否开启注册准入")] = None,
    mode: Annotated[Literal["enforce", "observe"] | None, Field(description="enforce 拦截 / observe 只记录本该拦截的注册、一律放行（新规则先观察）")] = None,
    rules: Annotated[list[dict[str, Any]] | None, Field(description=(
        "配额规则（整体替换；单条增改用 register_upsert_rule）。每条 {id, name, enabled, scope, windowSeconds（0 为累计，"
        "ip_devices / device_ips / app 必须有窗口）, limit, action: reject|challenge|observe, methods: [password|sms|oauth|cardkey]}"
    ))] = None,
    network: Annotated[dict[str, Any] | None, Field(description="地址聚合口径 {ipv4Subnet, ipv6Host, ipv6Subnet}（与现有值合并）")] = None,
    penalty: Annotated[dict[str, Any] | None, Field(description=(
        "惩罚期 {enabled, strikes, strikeWindowSeconds, baseSeconds, multiplier, maxSeconds, scope: ip|subnet, device: 设备规则拒绝时罚设备}（与现有值合并）"
    ))] = None,
    surge: Annotated[dict[str, Any] | None, Field(description=(
        "突增检测 {enabled, windowMinutes, baselineMinutes, sensitivity, minRegistrations, action, tightenFactor}（与现有值合并）"
    ))] = None,
    isp: Annotated[dict[str, Any] | None, Field(description="运营商限制 {enabled, keywords, action}（与现有值合并；keywords 整体替换）")] = None,
    geo: Annotated[dict[str, Any] | None, Field(description="属地限制 {mode: off|allow|deny, regions: [\"中国\", \"中国/广东\"], action, unknown: allow|reject}（与现有值合并）")] = None,
    device: Annotated[dict[str, Any] | None, Field(description="设备标识要求 {missing: allow|challenge|reject, invalid: allow|challenge|reject, minLength}（与现有值合并）")] = None,
    missing_ip: Annotated[Literal["allow", "reject"] | None, Field(description="拿不到注册 IP 时")] = None,
    fail_mode: Annotated[Literal["open", "closed"] | None, Field(description="计数存储故障时：open 放行 / closed 拒绝")] = None,
    reject_message: Annotated[str | None, Field(description="配额拒绝的提示，可用 {limit}、{window} 占位；空串恢复默认")] = None,
) -> Any:
    """修改注册准入策略。只传要改的字段：工具先读现有策略再合并保存（嵌套对象按字段合并，rules 整体替换）。需要 app:write。收紧规则前建议先 register_simulate 或把 mode 设为 observe 观察几天。"""
    current = await _current(app)
    patch = body(enabled=enabled, mode=mode, rules=rules, missingIp=missing_ip, failMode=fail_mode, rejectMessage=reject_message)
    for key, value in (("network", network), ("penalty", penalty), ("surge", surge), ("isp", isp), ("geo", geo), ("device", device)):
        if value is not None:
            patch[key] = _merge(current, key, value)
    if not patch:
        raise ToolError("没有要修改的字段")
    return await POLICY_SAVE(appkey=app, body={**current, **patch})


@server.tool(**WRITE("register", idempotent=True))
async def upsert_rule(
    app: AppRef,
    rule_id: Annotated[str, Field(min_length=1, max_length=32, description="规则 ID（字母、数字、横线）；已存在则修改，否则新增")],
    scope: Annotated[Scope | None, Field(description=SCOPE_HELP)] = None,
    window_seconds: Annotated[int | None, Field(ge=0, le=365 * 86400, description="统计窗口秒数，0 为累计（不限时间）；至少 60")] = None,
    limit: Annotated[int | None, Field(ge=1, le=1_000_000, description="窗口内最多注册数（去重型主体为最多设备 / 地址数）")] = None,
    action: Annotated[Action | None, Field(description="超出后：reject 拒绝 / challenge 要求图形验证码 / observe 只记录")] = None,
    name: Annotated[str | None, Field(description="规则名称，留空自动生成")] = None,
    enabled: Annotated[bool | None, Field(description="是否启用")] = None,
    methods: Annotated[list[Method] | None, Field(description="只约束这些注册方式；空列表为全部")] = None,
    position: Annotated[int | None, Field(ge=0, description="新增时插入的位置（0 为最前，留空追加）；规则顺序是同档处置的优先级")] = None,
) -> Any:
    """新增或修改一条配额规则（按 rule_id），其余规则不动。新增时 scope、limit 必填，window_seconds 默认 3600、action 默认 reject。"""
    current = await _current(app)
    rules = [dict(rule) for rule in current.get("rules") or []]
    fields = body(scope=scope, windowSeconds=window_seconds, limit=limit, action=action, name=name, enabled=enabled,
                  methods=methods if methods is None else (methods or None))
    for rule in rules:
        if rule.get("id") == rule_id:
            if not fields and methods is None:
                raise ToolError("没有要修改的字段")
            rule.update(fields)
            if methods is not None and not methods:
                rule.pop("methods", None)
            break
    else:
        if scope is None or limit is None:
            raise ToolError("新增规则时 scope 与 limit 必填")
        rule = {"id": rule_id, "enabled": True, "windowSeconds": 3600, "action": "reject", "name": "", **fields}
        if position is None or position >= len(rules):
            rules.append(rule)
        else:
            rules.insert(position, rule)
    return await POLICY_SAVE(appkey=app, body={**current, "rules": rules})


@server.tool(**WRITE("register", idempotent=True))
async def remove_rule(
    app: AppRef,
    rule_id: Annotated[str, Field(min_length=1, description="要删除的规则 ID")],
) -> Any:
    """删除一条配额规则（按 rule_id）。规则的计数随之失效，不影响已注册的账号。"""
    current = await _current(app)
    rules = [rule for rule in current.get("rules") or [] if rule.get("id") != rule_id]
    if len(rules) == len(current.get("rules") or []):
        raise ToolError(f"没有 ID 为 {rule_id} 的规则；先用 register_policy 查看现有规则")
    return await POLICY_SAVE(appkey=app, body={**current, "rules": rules})


@server.tool(**READ("register"))
async def simulate(
    app: AppRef,
    ip: Annotated[str, Field(description="注册地址；留空模拟拿不到 IP")] = "",
    device_id: Annotated[str, Field(description="设备标识；留空模拟没上报")] = "",
    method: Annotated[Method, Field(description="注册方式")] = "password",
    captcha_passed: Annotated[bool, Field(description="这次请求已通过图形验证码（加验型处置放行）")] = False,
    policy: Annotated[dict[str, Any] | None, Field(description="草稿策略（结构同 register_policy 的 policy）；留空用已保存的")] = None,
    counts: Annotated[dict[str, int] | None, Field(description="改写某些规则的已有计数 {规则 ID: 数量}，模拟「如果已经注册了 N 个」")] = None,
    location: Annotated[dict[str, Any] | None, Field(description="改写属地 {country, province, city, isp}")] = None,
    surge_active: Annotated[bool | None, Field(description="假定正在突增；留空用真实状态")] = None,
    penalty_minutes: Annotated[int | None, Field(ge=0, description="假定地址惩罚期剩余分钟；留空用真实值")] = None,
    device_penalty_minutes: Annotated[int | None, Field(ge=0, description="假定设备惩罚期剩余分钟；留空用真实值")] = None,
) -> Any:
    """模拟一次注册在注册准入下的判定（计数取真实值、只数不占：不写台账、不占名额、不进惩罚期），返回结论、原因、用户会看到的提示、每条规则的计数与上限，以及地址、设备、名单、属地、惩罚期、突增等事实。"""
    loc = None
    if location is not None:
        loc = {**location, "resolved": bool(location.get("country"))}
    return await SIMULATE(appkey=app, body=body(
        policy=policy, ip=ip, deviceId=device_id or None, method=method, captchaPassed=captcha_passed,
        counts=counts or None, location=loc, surgeActive=surge_active,
        penaltySeconds=None if penalty_minutes is None else penalty_minutes * 60,
        devicePenaltySeconds=None if device_penalty_minutes is None else device_penalty_minutes * 60,
    ))


# ═══════════════════════ 查询与解除 ═══════════════════════


@server.tool(**READ("register"))
async def inspect_ip(app: AppRef, ip: IP) -> Any:
    """查一个地址此刻的全部状况：此刻用它注册会怎样、地址一侧各规则用量与腾出名额时间、名单、惩罚期、属地、该地址与同网段注册过的账号、地址下出现过的设备（devices，配合 register_inspect_device 顺藤摸瓜）、最近拦截。"""
    return await INSPECT_IP(appkey=app, query=q(ip=ip))


@server.tool(**READ("register"))
async def inspect_device(app: AppRef, device: Device) -> Any:
    """查一台设备此刻的全部状况：标识是否有效、设备名单、设备惩罚期、设备一侧各规则用量、这台设备注册的账号、用过的地址（addresses，配合 register_inspect_ip）、最近拦截。"""
    return await INSPECT_DEVICE(appkey=app, query=q(device=device))


@server.tool(**DESTRUCTIVE("register"))
async def release(
    app: AppRef,
    ip: Annotated[str, Field(description="要解除的地址；与 device 至少给一个")] = "",
    device: Annotated[str, Field(description="要解除的设备标识")] = "",
    scope: Annotated[Literal["ip", "subnet"], Field(description="地址一侧：ip 只这个地址 / subnet 连同所在网段")] = "ip",
    counters: Annotated[bool, Field(description="清空窗口计数")] = True,
    penalty: Annotated[bool, Field(description="解除惩罚期")] = True,
    reset_level: Annotated[bool, Field(description="同时清空惩罚等级（再犯从首次时长算起）")] = False,
    ledger: Annotated[bool, Field(description="台账标为已解除：不再计入累计上限（账号不受影响，不可撤回）")] = False,
    note: Annotated[str, Field(description="备注，记入拦截记录")] = "",
) -> Any:
    """解除一个地址（或网段）和 / 或一台设备的注册限制。会放开对它的约束，执行前先向用户确认对象与范围。"""
    if not ip.strip() and not device.strip():
        raise ToolError("ip 与 device 至少给一个")
    return await RELEASE(appkey=app, body=body(
        ip=ip or None, device=device or None, scope=scope, counters=counters, penalty=penalty,
        resetLevel=reset_level, ledger=ledger, note=note,
    ))


@server.tool(**READ("register"))
async def penalties(
    app: AppRef,
    limit: Annotated[int, Field(ge=1, le=500, description="最多返回几条")] = 100,
) -> Any:
    """正在惩罚期的地址、网段与设备（subject 以 device: 开头的是设备），含等级与剩余秒数。"""
    return await PENALTIES(appkey=app, query=q(limit=limit))


# ═══════════════════════ 记录与台账 ═══════════════════════


@server.tool(**READ("register"))
async def events(
    app: AppRef,
    kind: Annotated[Literal["", "rejected", "challenged", "challenge_passed", "observed", "penalized", "released"], Field(description="记录类型")] = "",
    reason: Annotated[Literal["", "missing_ip", "missing_device", "invalid_device", "list_deny", "penalty", "geo", "isp", "surge", "rule", "failure"], Field(description="原因")] = "",
    rule_id: Annotated[str, Field(description="只看某条规则")] = "",
    method: Annotated[Literal["", "password", "sms", "oauth", "cardkey"], Field(description="注册方式")] = "",
    ip: Annotated[str, Field(description="地址或网段（CIDR）")] = "",
    device_id: Annotated[str, Field(description="设备标识（精确）")] = "",
    keyword: Annotated[str, Field(description="账号、设备、属地、详情、规则名")] = "",
    from_time: Annotated[IsoTime, Field(description="起始时间（RFC 3339）")] = "",
    to_time: Annotated[IsoTime, Field(description="结束时间（RFC 3339）")] = "",
    page: Page = 1,
    limit: Limit = 20,
) -> Any:
    """注册准入的拦截记录：拒绝、加验、加验后放行、观察、进入惩罚期、管理员解除，最新的在前。"""
    return await EVENTS(appkey=app, query=q(
        kind=kind, reason=reason, ruleId=rule_id, method=method, ip=ip, deviceId=device_id, keyword=keyword,
        **{"from": from_time, "to": to_time}, page=page, limit=limit,
    ))


@server.tool(**READ("register"))
async def registrations(
    app: AppRef,
    ip: Annotated[str, Field(description="地址或网段（CIDR）")] = "",
    device_id: Annotated[str, Field(description="设备标识（精确）")] = "",
    method: Annotated[Literal["", "password", "sms", "oauth", "cardkey"], Field(description="注册方式")] = "",
    released: Annotated[bool | None, Field(description="true 只看已解除 / false 只看计入上限的；留空全部")] = None,
    keyword: Annotated[str, Field(description="账号、设备、属地、运营商")] = "",
    from_time: Annotated[IsoTime, Field(description="起始时间（RFC 3339）")] = "",
    to_time: Annotated[IsoTime, Field(description="结束时间（RFC 3339）")] = "",
    page: Page = 1,
    limit: Limit = 20,
) -> Any:
    """注册台账：每个新账号从哪个地址、哪台设备、用什么方式注册的。累计型规则按它数；删号后行仍保留。"""
    return await REGISTRATIONS(appkey=app, query=q(
        ip=ip, deviceId=device_id, method=method, released=released, keyword=keyword,
        **{"from": from_time, "to": to_time}, page=page, limit=limit,
    ))


# ═══════════════════════ 名单 ═══════════════════════


@server.tool(**READ("register"))
async def lists(
    app: AppRef,
    target: Target = "ip",
    kind: Annotated[Literal["", "allow", "deny", "elevated"], Field(description="名单类型")] = "",
    keyword: Annotated[str, Field(description="地址名单：填地址时列出包含它的网段，否则匹配网段文本与备注；设备名单：匹配标识与备注")] = "",
    include_expired: Annotated[bool, Field(description="包含已过期的")] = False,
    page: Page = 1,
    limit: Limit = 20,
) -> Any:
    """地址名单或设备名单。判定时两边合并：任一边拒绝即拒绝，否则任一边放行即放行，否则取放宽倍数大的。"""
    route = IP_LISTS if target == "ip" else DEVICE_LISTS
    return await route(appkey=app, query=q(kind=kind, keyword=keyword, expired=include_expired or None, page=page, limit=limit))


@server.tool(**WRITE("register"))
async def add_lists(
    app: AppRef,
    values: Annotated[list[str], Field(min_length=1, max_length=500, description="地址 / CIDR 网段，或设备标识；最多 500 项")],
    kind: ListKind,
    target: Target = "ip",
    factor: Annotated[float, Field(ge=0, le=1000, description="放宽倍数（kind=elevated 时必填且大于 1）")] = 0,
    note: Annotated[str, Field(max_length=200, description="备注")] = "",
    expires_at: Annotated[IsoTime, Field(description="到期时间（ISO 8601），留空长期有效")] = "",
) -> Any:
    """批量加入地址名单或设备名单，已存在的按新值覆盖。拒绝名单立即生效，执行前先向用户确认对象；无效的设备标识只能加入拒绝名单。"""
    if kind == "elevated" and factor <= 1:
        raise ToolError("放宽名单需要 factor 大于 1")
    route = IP_LISTS_ADD if target == "ip" else DEVICE_LISTS_ADD
    return await route(appkey=app, body=body(
        values=values, kind=kind, factor=factor if kind == "elevated" else None, note=note, expiresAt=expires_at or None,
    ))


@server.tool(**WRITE("register", idempotent=True))
async def update_list(
    app: AppRef,
    entry_id: Annotated[int, Field(gt=0, description="名单项 ID（register_lists 结果里的 id）")],
    kind: ListKind,
    target: Target = "ip",
    factor: Annotated[float, Field(ge=0, le=1000, description="放宽倍数（kind=elevated 时大于 1）")] = 0,
    note: Annotated[str, Field(max_length=200, description="备注（整体替换）")] = "",
    expires_at: Annotated[IsoTime, Field(description="到期时间，留空改为长期有效")] = "",
) -> Any:
    """修改一项名单的类型、倍数、备注与到期（整体覆盖这几项）。"""
    if kind == "elevated" and factor <= 1:
        raise ToolError("放宽名单需要 factor 大于 1")
    route = IP_LIST_UPDATE if target == "ip" else DEVICE_LIST_UPDATE
    return await route(appkey=app, id=entry_id, body={
        "kind": kind, "factor": factor if kind == "elevated" else 0, "note": note, "expiresAt": expires_at or None,
    })


@server.tool(**DESTRUCTIVE("register"))
async def delete_lists(
    app: AppRef,
    ids: Annotated[list[Annotated[int, Field(gt=0)]], Field(min_length=1, max_length=500, description="名单项 ID 列表")],
    target: Target = "ip",
) -> Any:
    """删除地址名单或设备名单项。删掉放行项会让对应的地址 / 设备重新受规则约束，执行前先向用户确认。"""
    route = IP_LISTS_DELETE if target == "ip" else DEVICE_LISTS_DELETE
    return await route(appkey=app, body={"ids": ids})
