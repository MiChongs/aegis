"""物品发放：目录、画布规则（新建、修改、启停、删除）、校验与预演、立即执行与取消、执行记录、发放记录与人工处理、总览。

规则是一张画布（触发器 → 人群 / 条件 / 窗口 / 分流 / 限额 → 发放 / 随机礼包 → 站内信），触发方式有手动、定时（Cron）与事件
（注册 / 登录 / 签到 / 支付成功）三种。14 种物品各走自己的账本（积分、经验、会员、功能、免广告、抽奖次数、AI 额度、余额、授权……），
与从积分商城、卡密、抽奖得到的同口径。发出去的东西不能通过本模块收回。

权限：读需要 app:read，其余（含校验与预演）需要 app:write。
"""

from __future__ import annotations

from typing import Annotated, Any, Literal

from fastmcp import FastMCP
from fastmcp.exceptions import ToolError
from pydantic import Field

from ..core import DELETE, DESTRUCTIVE, GET, POST, PUT, READ, WRITE, AppRef, IsoTime, Limit, Page, body, q

NAMESPACE = "grant"
server = FastMCP("grant")

BASE = "/api/admin/apps/:appkey/item-grants"
CATALOG = GET(f"{BASE}/catalog")
OVERVIEW = GET(f"{BASE}/overview")
RULES = GET(f"{BASE}/rules")
RULE_CREATE = POST(f"{BASE}/rules")
RULE = GET(f"{BASE}/rules/:ruleId")
RULE_UPDATE = PUT(f"{BASE}/rules/:ruleId")
RULE_DELETE = DELETE(f"{BASE}/rules/:ruleId")
RULE_STATUS = PUT(f"{BASE}/rules/:ruleId/status")
RULE_RUN = POST(f"{BASE}/rules/:ruleId/run")
VALIDATE = POST(f"{BASE}/validate")
PREVIEW = POST(f"{BASE}/preview")
RUNS = GET(f"{BASE}/runs")
RUN = GET(f"{BASE}/runs/:runId")
RUN_CANCEL = POST(f"{BASE}/runs/:runId/cancel")
RECORDS = GET(f"{BASE}/records")
RECORD = GET(f"{BASE}/records/:recordId")
RECORD_FULFILL = POST(f"{BASE}/records/:recordId/fulfill")
RECORD_RETRY = POST(f"{BASE}/records/:recordId/retry")

RuleID = Annotated[int, Field(gt=0, description="发放规则 ID（grant_rules 结果里的 id）")]
RunID = Annotated[int, Field(gt=0, description="执行 ID（grant_runs 结果里的 id，或 grant_run 的返回）")]
RecordID = Annotated[int, Field(gt=0, description="发放记录 ID（grant_records 结果里的 id）")]
Status = Literal["draft", "active", "paused", "archived"]

ITEMS_HELP = (
    "物品列表（一个发放节点最多 12 项）。每项 {type, amount?, money?, refId?, tag?, text?, amountExpr?}。type："
    "points 积分（amount）/ experience 经验（amount）/ vip_days 会员天数（amount=天）/ vip_plan 会员套餐（refId=套餐 ID，试用套餐不能发）/ "
    "vip_lifetime 永久会员（无取值）/ feature 功能解锁（tag=功能标识，amount=天，0 为永久）/ feature_time 功能时长（tag，amount=分钟）/ "
    "ad_free 免广告（amount=天）/ lottery_draws 抽奖次数（amount）/ ai_credit AI 额度（amount=点）/ balance 钱包余额（money=元，如 \"1.50\"）/ "
    "license_days 授权时长（amount=天，名下须有限时授权卡）/ device_slots 设备位（amount=台，须有授权卡）/ manual 人工发放（text，记录进入待处理）/ "
    "number_change 改号卡（amount=张）/ number_ticket 靓号券（amount=张，refId=等级 1 精品 2 稀有 3 传说 4 至尊）/ number_draw 摇号次数（amount=次）/ number_assign 指定号码（text=5–12 位号码，不扣权益不看冷却；保留号、别人冻结或在用的号会让这笔发放失败、可重试）。"
    "数量型物品可写 amountExpr（如 \"50 + user.signStreak * 5\"），算不出来退回 amount。以 grant_catalog 的 items 为准"
)
GRAPH_HELP = (
    "画布（与控制台 React Flow 同形）：{nodes: [{id, type, position: {x, y}, data}], edges: [{id, source, sourceHandle, target}]}。"
    "节点 type 与端口：trigger（next；data {kind: manual|schedule|event, cron?, event?: register|login|signin|payment}，恰好一个）/ "
    "audience（next；data {scope: all|member|non_member|lifetime|non_lifetime, registeredWithinDays?, registeredBeforeDays?, "
    "activeWithinDays?, minLevel?, maxLevel?, userIds?, excludeUserIds?}，紧跟触发器的那个决定批量执行的候选人）/ "
    "condition（yes / no；data {mode: all|any|expr, rules: [{field, op, value: [数字]}], expr}）/ window（in / out；data {weekdays, startMinute, endMinute, dateFrom, dateTo}）/ "
    "split（每个分支 id 一个端口；data {branches: [{id, label, weight}], sticky}）/ limit（pass / over；data {period: total|day|week|month, perUser, global}）/ "
    "grant（next；data {items}）/ random（next；data {picks, options: [{id, label, weight, items}]}）/ "
    "notify（next；data {title, content, route?}，{items} {nickname} {rule} 占位）/ end。每个节点 data 可带 label、note。"
    "必须无环、至少一个发放节点可达。字段、运算符、模板以 grant_catalog 为准，保存前可用 grant_validate 校验"
)


def _time(value: str | None) -> Any:
    """None 不改；空串表示清空（发 null）。"""
    if value is None:
        return None
    return value or _CLEAR


_CLEAR = object()


def _rule_fields(**fields: Any) -> dict[str, Any]:
    return {key: (None if value is _CLEAR else value) for key, value in body(**fields).items()}


def _slim(rule: Any) -> Any:
    """列表里去掉画布（体积大），只留节点数。"""
    if not isinstance(rule, dict) or "graph" not in rule:
        return rule
    graph = rule.get("graph") or {}
    out = {key: value for key, value in rule.items() if key != "graph"}
    out["nodeCount"] = len(graph.get("nodes") or [])
    return out


async def _graph_of(app: str, rule_id: int) -> dict[str, Any]:
    rule = await RULE(appkey=app, ruleId=rule_id)
    if not isinstance(rule, dict) or not isinstance(rule.get("graph"), dict):
        raise ToolError(f"读取规则 {rule_id} 的画布失败")
    return rule["graph"]


@server.tool(**READ("grant"))
async def catalog(app: AppRef) -> Any:
    """物品发放的目录：14 种物品（取值形态、单位、上下界、能否写表达式）、节点与端口、触发方式、事件（及事件字段）、条件字段与运算符、
    人群范围、限额周期、站内信跳转页、5 个内置模板（含画布）、本应用可发的会员套餐与功能、各项上限与应用时区。新建或修改规则前先看它。"""
    return await CATALOG(appkey=app)


@server.tool(**READ("grant"))
async def overview(app: AppRef, days: Annotated[int, Field(ge=1, le=90, description="趋势取最近 N 天")] = 14) -> Any:
    """物品发放总览：规则数与生效数、发放记录数与人数、今日发放、待人工处理、失败、执行中，近 N 天趋势与近 30 天按物品汇总。"""
    return await OVERVIEW(appkey=app, query={"days": days})


@server.tool(**READ("grant"))
async def rules(
    app: AppRef,
    status: Annotated[Literal["", "draft", "active", "paused", "archived"], Field(description="只看这个状态，留空为全部")] = "",
    trigger: Annotated[Literal["", "manual", "schedule", "event"], Field(description="只看这种触发方式，留空为全部")] = "",
    include_graph: Annotated[bool, Field(description="附带完整画布（体积大；看单条规则用 grant_get）")] = False,
) -> Any:
    """发放规则列表：状态、触发方式（事件 / Cron）、生效期、版本、下次执行时间，附发放统计（记录、人数、今日、待处理、失败）与正在进行的执行。"""
    result = await RULES(appkey=app)
    if not isinstance(result, list):
        return result
    items = [rule for rule in result if isinstance(rule, dict)
             and (not status or rule.get("status") == status) and (not trigger or rule.get("trigger") == trigger)]
    return {"items": items if include_graph else [_slim(rule) for rule in items], "total": len(items)}


@server.tool(name="get", **READ("grant"))
async def get_rule(app: AppRef, rule_id: RuleID) -> Any:
    """一条发放规则的完整信息（含画布 graph、版本、生效期、下次执行时间）。"""
    return await RULE(appkey=app, ruleId=rule_id)


Name = Annotated[str, Field(min_length=1, max_length=40, description="规则名称（≤40 字）")]
Description = Annotated[str | None, Field(max_length=500, description="规则说明（≤500 字）")]
StartsAt = Annotated[str | None, Field(description="生效开始时间 ISO 8601（RFC 3339）；留空不限")]
EndsAt = Annotated[str | None, Field(description="生效结束时间 ISO 8601（RFC 3339），须晚于开始；留空不限")]
Graph = Annotated[dict[str, Any] | None, Field(description=GRAPH_HELP)]


@server.tool(**WRITE("grant"))
async def create(
    app: AppRef,
    name: Name,
    description: Description = None,
    starts_at: StartsAt = None,
    ends_at: EndsAt = None,
    graph: Graph = None,
    template: Annotated[str | None, Field(description="用内置模板的画布（grant_catalog 的 templates[].key：welcome / daily-login / sign-streak / member-weekly / win-back）；与 graph 二选一")] = None,
) -> Any:
    """新建发放规则，状态为草稿（draft，不会触发、不会发放）。graph 与 template 都不给时后端给一张初始画布（手动执行 → 全部用户 → 10 积分）。

    简单的「给指定人群发一组物品」用 grant_create_simple 更省事。建好后：grant_validate / grant_preview 核对 → grant_set_status 上线 →
    手动规则再 grant_run 执行。
    """
    if graph is not None and template:
        raise ToolError("graph 与 template 只能给一个")
    if template:
        info = await CATALOG(appkey=app)
        found = next((item for item in (info or {}).get("templates") or [] if item.get("key") == template), None)
        if found is None:
            keys = [item.get("key") for item in (info or {}).get("templates") or []]
            raise ToolError(f"没有叫 {template} 的模板，可选：{', '.join(map(str, keys))}")
        graph = found.get("graph")
    return await RULE_CREATE(appkey=app, body=_rule_fields(
        name=name, description=description, startsAt=starts_at or None, endsAt=ends_at or None, graph=graph,
    ))


def _simple_graph(
    *, trigger: str, cron: str, event: str, audience: dict[str, Any], items: list[dict[str, Any]],
    per_user: int, global_limit: int, period: str, notify_title: str, notify_content: str, notify_route: str,
) -> dict[str, Any]:
    nodes: list[dict[str, Any]] = []
    edges: list[dict[str, Any]] = []

    def add(node_id: str, node_type: str, data: dict[str, Any], handle: str | None = None) -> None:
        if nodes:
            edges.append({"id": f"e{len(edges) + 1}", "source": nodes[-1]["id"], "sourceHandle": handle or "next", "target": node_id})
        nodes.append({"id": node_id, "type": node_type, "position": {"x": 320 * len(nodes), "y": 120}, "data": data})

    add("trigger", "trigger", body(kind=trigger, cron=cron or None, event=event or None))
    add("audience", "audience", audience)
    pending_handle = None
    if per_user or global_limit:
        add("limit", "limit", body(period=period, perUser=per_user or None, **{"global": global_limit or None}))
        pending_handle = "pass"
    add("grant", "grant", {"items": items}, pending_handle)
    if notify_title or notify_content:
        add("notify", "notify", body(title=notify_title, content=notify_content, route=notify_route or None))
    return {"nodes": nodes, "edges": edges}


@server.tool(**WRITE("grant"))
async def create_simple(
    app: AppRef,
    name: Name,
    items: Annotated[list[dict[str, Any]], Field(min_length=1, max_length=12, description=ITEMS_HELP)],
    trigger: Annotated[Literal["manual", "schedule", "event"], Field(description="manual 手动执行（grant_run）/ schedule 定时 / event 用户行为触发")] = "manual",
    cron: Annotated[str, Field(description="trigger=schedule 时的五段 Cron（应用时区，相邻两次至少 10 分钟），如 \"0 10 * * 1\"")] = "",
    event: Annotated[Literal["", "register", "login", "signin", "payment"], Field(description="trigger=event 时的事件：register 注册 / login 登录 / signin 签到 / payment 支付成功")] = "",
    scope: Annotated[Literal["all", "member", "non_member", "lifetime", "non_lifetime"], Field(description="人群范围：全部 / 会员（含永久）/ 非会员 / 永久会员 / 非永久会员")] = "all",
    user_ids: Annotated[list[int] | None, Field(max_length=5000, description="只发给这些人（管理端用户 ID，≤5000）；留空不限")] = None,
    exclude_user_ids: Annotated[list[int] | None, Field(max_length=5000, description="排除这些人（管理端用户 ID）")] = None,
    registered_within_days: Annotated[int, Field(ge=0, description="只要注册不超过 N 天的（0 不限）")] = 0,
    registered_before_days: Annotated[int, Field(ge=0, description="只要注册满 N 天的（0 不限）")] = 0,
    active_within_days: Annotated[int, Field(ge=0, description="只要最近 N 天登录或在线过的（0 不限）")] = 0,
    min_level: Annotated[int, Field(ge=0, description="最低等级（0 不限）")] = 0,
    max_level: Annotated[int, Field(ge=0, description="最高等级（0 不限）")] = 0,
    per_user: Annotated[int, Field(ge=0, description="每人在周期内最多发几次（0 不加限额节点；事件规则强烈建议设置）")] = 0,
    global_limit: Annotated[int, Field(ge=0, description="全站在周期内最多发几人次（0 不限）")] = 0,
    period: Annotated[Literal["total", "day", "week", "month"], Field(description="限额周期：累计 / 天 / 周 / 月")] = "total",
    notify_title: Annotated[str, Field(max_length=60, description="发放成功后的站内信标题（留空不发站内信）")] = "",
    notify_content: Annotated[str, Field(max_length=500, description="站内信正文，可用 {items} {nickname} {rule} 占位")] = "",
    notify_route: Annotated[str, Field(description="点开站内信跳转的 App 页面（grant_catalog 的 routes[].key），留空不跳转")] = "",
    description: Description = None,
    starts_at: StartsAt = None,
    ends_at: EndsAt = None,
) -> Any:
    """按表单生成一条直线画布（触发器 → 目标人群 → [频次限额] → 发放物品 → [站内信]）并新建为草稿规则，不会立即发放。

    典型用法「给指定用户手动补发」：trigger=manual + user_ids + items，建好后 grant_preview 抽样核对 → 向用户确认 →
    grant_set_status 上线 → grant_run 执行。需要条件分支、随机礼包、A/B 分流时用 grant_create 传完整 graph。
    """
    if trigger == "schedule" and not cron.strip():
        raise ToolError("定时规则需要 cron")
    if trigger == "event" and not event:
        raise ToolError("事件规则需要 event")
    if trigger != "schedule" and cron.strip():
        raise ToolError("只有 trigger=schedule 才用 cron")
    if trigger != "event" and event:
        raise ToolError("只有 trigger=event 才用 event")
    if notify_content and not notify_title:
        raise ToolError("站内信需要标题")
    audience = body(
        scope=scope, userIds=user_ids or None, excludeUserIds=exclude_user_ids or None,
        registeredWithinDays=registered_within_days or None, registeredBeforeDays=registered_before_days or None,
        activeWithinDays=active_within_days or None, minLevel=min_level or None, maxLevel=max_level or None,
    )
    graph = _simple_graph(
        trigger=trigger, cron=cron.strip(), event=event, audience=audience, items=items, per_user=per_user,
        global_limit=global_limit, period=period, notify_title=notify_title, notify_content=notify_content, notify_route=notify_route,
    )
    return await RULE_CREATE(appkey=app, body=_rule_fields(
        name=name, description=description, startsAt=starts_at or None, endsAt=ends_at or None, graph=graph,
    ))


@server.tool(**WRITE("grant"))
async def update(
    app: AppRef,
    rule_id: RuleID,
    name: Annotated[str | None, Field(min_length=1, max_length=40, description="规则名称（≤40 字）")] = None,
    description: Description = None,
    starts_at: Annotated[str | None, Field(description="生效开始时间 ISO 8601；空串为清空（不限）")] = None,
    ends_at: Annotated[str | None, Field(description="生效结束时间 ISO 8601；空串为清空（不限）")] = None,
    graph: Annotated[dict[str, Any] | None, Field(description=GRAPH_HELP + "。整体替换画布；不传则保留现有画布")] = None,
) -> Any:
    """修改发放规则（版本号加一）。只传要改的字段，其余沿用当前值（后端整体覆盖，这里先读后合并；状态请用 grant_set_status）。

    生效中的规则保存时画布不能有错误（可先 grant_validate，或先暂停）；改动对之后的触发与执行立即生效（事件规则缓存最多 30 秒），
    已写下的发放记录不受影响。执行前向用户复述改动。
    """
    current = await RULE(appkey=app, ruleId=rule_id)
    if not isinstance(current, dict):
        raise ToolError(f"读取规则 {rule_id} 失败")
    request = {key: current[key] for key in ("name", "description", "startsAt", "endsAt", "graph") if key in current}
    request.update(_rule_fields(name=name, description=description, startsAt=_time(starts_at), endsAt=_time(ends_at), graph=graph))
    return await RULE_UPDATE(appkey=app, ruleId=rule_id, body=request)


@server.tool(**WRITE("grant", idempotent=True))
async def set_status(
    app: AppRef,
    rule_id: RuleID,
    status: Annotated[Status, Field(description="draft 草稿 / active 上线 / paused 暂停 / archived 归档")],
) -> Any:
    """上线、暂停、归档发放规则或退回草稿。上线前后端会完整校验画布与引用的套餐 / 功能。

    上线即生效：事件规则从此在用户注册 / 登录 / 签到 / 支付时自动发放，定时规则按 Cron 到点对整个目标人群发放，手动规则上线后才能 grant_run。
    上线前先 grant_preview 确认人群规模与物品，并向用户确认。
    """
    return await RULE_STATUS(appkey=app, ruleId=rule_id, body={"status": status})


@server.tool(name="delete", **DESTRUCTIVE("grant"))
async def delete_rule(app: AppRef, rule_id: RuleID) -> Any:
    """删除发放规则（仅限没有发放记录的规则，不可恢复；有记录的返回 40900，请用 grant_set_status 归档）。执行前先向用户确认规则名称与 ID。"""
    return await RULE_DELETE(appkey=app, ruleId=rule_id)


@server.tool(**WRITE("grant", idempotent=True))
async def validate(
    app: AppRef,
    graph: Annotated[dict[str, Any] | None, Field(description=GRAPH_HELP + "。与 rule_id 二选一")] = None,
    rule_id: Annotated[int | None, Field(gt=0, description="校验这条已保存规则的当前画布")] = None,
) -> Any:
    """校验画布（不保存、不发放）：结构、端口、无环、配置、表达式、引用的套餐与功能。返回 {valid, issues: [{level, nodeId?, edgeId?, message}]}。需要 app:write 权限。"""
    if (graph is None) == (rule_id is None):
        raise ToolError("需要 graph 或 rule_id 之一（只能给一个）")
    if graph is None:
        graph = await _graph_of(app, rule_id)  # type: ignore[arg-type]
    return await VALIDATE(appkey=app, body={"graph": graph})


@server.tool(**WRITE("grant", idempotent=True))
async def preview(
    app: AppRef,
    rule_id: Annotated[int | None, Field(gt=0, description="预演这条已保存的规则（不给 graph 时用它的当前画布；限额节点按它已有的发放记录计）")] = None,
    graph: Annotated[dict[str, Any] | None, Field(description="预演一张未保存的画布（同 grant_create 的 graph）；给了 rule_id 时代替它的已存画布")] = None,
    user: Annotated[str, Field(description="追踪这一个人：管理端用户 ID 或账号。给了就只追踪他，不抽样")] = "",
    event_data: Annotated[dict[str, Any] | None, Field(description="事件数据（条件里的 event.*），如签到 {consecutiveDays: 7}、支付 {amount: 30}")] = None,
    samples: Annotated[int, Field(ge=1, le=1000, description="不指定 user 时抽样多少人估算")] = 200,
) -> Any:
    """预演发放规则（只判定、不发放、不写记录）。

    指定 user：这个人此刻会走哪条路径、拿到哪些物品（trace）；不指定：目标人群总数（candidates）、抽样里各节点 / 端口到达人数、
    命中发放的人数、按物品汇总的数量并推算全量（projected），以及账号不可用被跳过的人数。画布有错误时只返回 issues。
    上线或执行前用它核对影响面。需要 app:write 权限。
    """
    if rule_id is None and graph is None:
        raise ToolError("需要 rule_id 或 graph 之一")
    if graph is None:
        graph = await _graph_of(app, rule_id)  # type: ignore[arg-type]
    return await PREVIEW(appkey=app, body=body(
        ruleId=rule_id, graph=graph, user=user.strip() or None, eventData=event_data, samples=None if user.strip() else samples,
    ))


@server.tool(name="run", **DESTRUCTIVE("grant"))
async def run_rule(app: AppRef, rule_id: RuleID) -> Any:
    """立即执行一次规则：对目标人群逐人判定并真实发放物品（后台执行，返回这次执行；用 grant_get_run 看进度）。

    只能执行已上线（active）、在生效期内的手动或定时规则；事件规则不能批量执行；同一规则同时只能有一次在执行。
    物品发出即到账、不能通过本工具收回（同一次执行内每人只发一次）。执行前必须先 grant_preview 核对人群规模与物品，
    并向用户复述规则名、预计人数与物品，取得明确确认。
    """
    return await RULE_RUN(appkey=app, ruleId=rule_id)


@server.tool(**READ("grant"))
async def runs(
    app: AppRef,
    rule_id: Annotated[int | None, Field(gt=0, description="只看这条规则，留空为全部")] = None,
    page: Page = 1,
    limit: Limit = 20,
) -> Any:
    """执行记录（手动与定时，新的在前）：状态 running / succeeded / failed / cancelled，总人数、已处理、发放、跳过、失败、操作人与起止时间。"""
    return await RUNS(appkey=app, query=q(ruleId=rule_id, page=page, limit=limit))


@server.tool(**READ("grant"))
async def get_run(app: AppRef, run_id: RunID) -> Any:
    """一次执行的详情：进度（已处理 / 发放 / 跳过 / 失败、游标、心跳）、错误与执行时的画布快照。"""
    return await RUN(appkey=app, runId=run_id)


@server.tool(**WRITE("grant"))
async def cancel_run(app: AppRef, run_id: RunID) -> Any:
    """取消一次正在进行的执行：当前这批（≤200 人）处理完就停，已发出的物品不会收回，取消后不能续跑（需要时重新 grant_run，已发过的人按幂等跳过不会再发）。"""
    return await RUN_CANCEL(appkey=app, runId=run_id)


@server.tool(**READ("grant"))
async def records(
    app: AppRef,
    rule_id: Annotated[int | None, Field(gt=0, description="只看这条规则")] = None,
    run_id: Annotated[int | None, Field(gt=0, description="只看这次执行")] = None,
    user_id: Annotated[int | None, Field(gt=0, description="只看这个用户（管理端用户 ID）")] = None,
    status: Annotated[Literal["", "granted", "pending", "fulfilled", "failed"], Field(description="granted 已到账 / pending 人工物品待处理 / fulfilled 人工物品已处理 / failed 发放失败（可重试），留空为全部")] = "",
    keyword: Annotated[str, Field(description="关键词（记录号、账号、昵称等）")] = "",
    start: IsoTime = "",
    end: IsoTime = "",
    page: Page = 1,
    limit: Limit = 20,
) -> Any:
    """发放记录（新的在前）：谁、哪条规则 / 哪次执行 / 哪个事件、状态、计划发放的物品与实际结果（含折算说明、流水号）、失败原因。查待人工处理的用 status=pending。"""
    return await RECORDS(appkey=app, query=q(
        ruleId=rule_id, runId=run_id, userId=user_id, status=status, keyword=keyword, start=start, end=end, page=page, limit=limit,
    ))


@server.tool(**READ("grant"))
async def get_record(app: AppRef, record_id: RecordID) -> Any:
    """一条发放记录的详情：判定路径（steps）、计划发放（items）、实际结果（results）、事件数据、错误、处理人与备注。"""
    return await RECORD(appkey=app, recordId=record_id)


@server.tool(**WRITE("grant"))
async def fulfill(
    app: AppRef,
    record_id: Annotated[int, Field(gt=0, description="发放记录 ID（状态须为 pending，即含人工发放物品）")],
    note: Annotated[str, Field(max_length=200, description="处理说明，会写进给用户的站内信（兑换码、快递单号等）")] = "",
) -> Any:
    """把一条人工发放（manual）的记录标记为已处理（fulfilled），并给用户发站内信（含 note）。消息发出后无法撤回，执行前先向用户确认 note 内容。"""
    return await RECORD_FULFILL(appkey=app, recordId=record_id, body=body(note=note))


@server.tool(**WRITE("grant"))
async def retry(app: AppRef, record_id: Annotated[int, Field(gt=0, description="发放记录 ID（状态须为 failed）")]) -> Any:
    """重试一条失败的发放记录：按规则当前的画布对这个人重新判定并真实发放（成功即到账、不能通过本工具收回）。

    先用 grant_get_record 看失败原因（如名下没有授权卡、套餐已删除），原因未消除时重试仍会失败。执行前向用户确认。
    """
    return await RECORD_RETRY(appkey=app, recordId=record_id)
