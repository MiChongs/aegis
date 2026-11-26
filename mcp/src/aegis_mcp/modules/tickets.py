"""工单与反馈：检索、详情、建单、回复与内部备注、状态流转、指派、关注、批量处理、统计，以及分类 / 处理组 / SLA / 快捷回复配置。"""

from __future__ import annotations

from typing import Annotated, Any, Literal

from fastmcp import FastMCP
from fastmcp.exceptions import ToolError
from pydantic import Field

from ..core import DELETE, DESTRUCTIVE, GET, PATCH, POST, PUT, READ, WRITE, IsoTime, Limit, Page, app_id_of, body, q

NAMESPACE = "tickets"
server = FastMCP("tickets")

TICKETS = GET("/api/admin/tickets")
TICKET = GET("/api/admin/tickets/:ticketId")
TICKET_CREATE = POST("/api/admin/tickets")
TICKET_UPDATE = PATCH("/api/admin/tickets/:ticketId")
TICKET_DELETE = DELETE("/api/admin/tickets/:ticketId")
TICKET_REPLY = POST("/api/admin/tickets/:ticketId/replies")
TICKET_MESSAGE_DELETE = DELETE("/api/admin/tickets/:ticketId/messages/:messageId")
TICKET_STATUS = POST("/api/admin/tickets/:ticketId/status")
TICKET_ASSIGN = POST("/api/admin/tickets/:ticketId/assign")
TICKET_WATCH = POST("/api/admin/tickets/:ticketId/watch")
TICKET_WATCHERS = PUT("/api/admin/tickets/:ticketId/watchers")
TICKETS_BULK = POST("/api/admin/tickets/bulk")
STATS = GET("/api/admin/tickets/stats")
TREND = GET("/api/admin/tickets/trend")
AGENTS = GET("/api/admin/tickets/agents")
WORKBENCH = GET("/api/admin/tickets/workbench")
METADATA = GET("/api/admin/tickets/metadata")

CATEGORIES = GET("/api/admin/tickets/categories")
CATEGORY_CREATE = POST("/api/admin/tickets/categories")
CATEGORY_UPDATE = PUT("/api/admin/tickets/categories/:id")
CATEGORY_DELETE = DELETE("/api/admin/tickets/categories/:id")
GROUPS = GET("/api/admin/tickets/groups")
GROUP_CREATE = POST("/api/admin/tickets/groups")
GROUP_UPDATE = PUT("/api/admin/tickets/groups/:id")
GROUP_DELETE = DELETE("/api/admin/tickets/groups/:id")
GROUP_MEMBERS = PUT("/api/admin/tickets/groups/:id/members")
SLA_POLICIES = GET("/api/admin/tickets/sla-policies")
SLA_CREATE = POST("/api/admin/tickets/sla-policies")
SLA_UPDATE = PUT("/api/admin/tickets/sla-policies/:id")
SLA_DELETE = DELETE("/api/admin/tickets/sla-policies/:id")
QUICK_REPLIES = GET("/api/admin/tickets/quick-replies")
QUICK_REPLY_CREATE = POST("/api/admin/tickets/quick-replies")
QUICK_REPLY_UPDATE = PUT("/api/admin/tickets/quick-replies/:id")
QUICK_REPLY_DELETE = DELETE("/api/admin/tickets/quick-replies/:id")

# 状态一律三个字：待受理 / 处理中 / 等待中 / 已解决 / 不采纳 / 已关闭 / 已撤销
Status = Literal["open", "processing", "pending", "resolved", "rejected", "closed", "cancelled"]
Priority = Literal["low", "normal", "high", "urgent"]
Kind = Literal["ticket", "feedback"]
TicketID = Annotated[int, Field(gt=0, description="工单 ID")]
AppFilter = Annotated[str, Field(description="只看某个应用（appKey 或应用 ID），空为可见的全部")]


async def _appid(app: str) -> int | None:
    return await app_id_of(app) if app else None


@server.tool(name="list", **READ("tickets"))
async def list_tickets(
    app: AppFilter = "",
    kind: Annotated[Kind | None, Field(description="ticket 工单 / feedback 意见反馈")] = None,
    status: Status | None = None,
    priority: Priority | None = None,
    category_id: int | None = None,
    group_id: int | None = None,
    assignee_id: Annotated[int | None, Field(description="受理管理员 ID")] = None,
    requester_id: Annotated[int | None, Field(description="提单用户 ID")] = None,
    keyword: str = "",
    tags: Annotated[list[str] | None, Field(description="标签（任一命中）")] = None,
    sla_state: Annotated[str, Field(description="SLA 状态，如 ok / warning / breached")] = "",
    mine: Annotated[bool | None, Field(description="只看指派给我的")] = None,
    unassigned: Annotated[bool | None, Field(description="只看未指派的")] = None,
    overdue_only: Annotated[bool | None, Field(description="只看超出 SLA 的")] = None,
    rated: Annotated[bool | None, Field(description="是否已评价")] = None,
    include_closed: Annotated[bool | None, Field(description="包含已关闭")] = None,
    created_from: IsoTime = "",
    created_to: IsoTime = "",
    sort_by: Annotated[str, Field(description="排序字段，如 createdAt / updatedAt / priority")] = "",
    sort_dir: Literal["", "asc", "desc"] = "",
    page: Page = 1,
    limit: Limit = 20,
) -> Any:
    """工单列表（可见范围由服务端按权限裁剪，结果里的 scope 说明可见范围）。"""
    return await TICKETS(query=q(
        appid=await _appid(app), kind=kind, status=status, priority=priority, categoryId=category_id, groupId=group_id,
        assigneeId=assignee_id, requesterId=requester_id, keyword=keyword, tags=tags, slaState=sla_state,
        mine=mine, unassigned=unassigned, overdueOnly=overdue_only, rated=rated, includeClosed=include_closed,
        createdFrom=created_from, createdTo=created_to, sortBy=sort_by, sortDir=sort_dir, page=page, limit=limit,
    ))


@server.tool(name="get", **READ("tickets"))
async def get_ticket(ticket_id: TicketID) -> Any:
    """一张工单的详情、往来回复、内部备注（有权限时）与事件流。"""
    return await TICKET(ticketId=ticket_id)


@server.tool(**WRITE("tickets"))
async def create(
    app: Annotated[str, Field(min_length=1, description="所属应用（appKey 或应用 ID）")],
    title: Annotated[str, Field(min_length=1)],
    content: Annotated[str, Field(min_length=1)],
    kind: Annotated[Kind, Field(description="ticket 内部工单（提单人只能是管理员）/ feedback 代用户登记的意见反馈")] = "ticket",
    category_id: int | None = None,
    priority: Priority | None = None,
    group_id: int | None = None,
    assignee_admin_id: int | None = None,
    requester_user_id: Annotated[int | None, Field(description="代某个用户登记反馈时填用户 ID（仅 kind=feedback）")] = None,
    requester_name: str = "",
    requester_contact: str = "",
    tags: list[str] | None = None,
    source: Annotated[Literal["api", "console", "email", "bot", "import", "app"], Field(description="来源渠道")] = "api",
) -> Any:
    """以管理员身份新建内部工单，或代用户登记一条意见反馈。工单只对管理员开放，不能以用户为提单人。"""
    if requester_user_id and kind == "ticket":
        raise ToolError("工单只对管理员开放；代用户登记请用 kind=feedback")
    return await TICKET_CREATE(body=body(
        appid=await app_id_of(app), kind=kind, title=title, content=content, categoryId=category_id, priority=priority,
        groupId=group_id, assigneeAdminId=assignee_admin_id, requesterUserId=requester_user_id,
        requesterType="user" if requester_user_id else None,
        requesterName=requester_name or None, requesterContact=requester_contact or None, tags=tags, source=source,
    ))


@server.tool(**WRITE("tickets"))
async def update(
    ticket_id: TicketID,
    title: str | None = None,
    category_id: int | None = None,
    priority: Priority | None = None,
    tags: Annotated[list[str] | None, Field(description="整体替换标签")] = None,
    locked: Annotated[bool | None, Field(description="锁定后提单人不能再追问")] = None,
) -> Any:
    """修改工单标题、分类、优先级、标签或锁定状态（只传要改的）。"""
    return await TICKET_UPDATE(ticketId=ticket_id, body=body(title=title, categoryId=category_id, priority=priority, tags=tags, locked=locked))


@server.tool(**WRITE("tickets"))
async def reply(
    ticket_id: TicketID,
    content: Annotated[str, Field(min_length=1, description="回复正文")],
    internal: Annotated[bool, Field(description="true 为内部备注（提单人不可见）")] = False,
    next_status: Annotated[Status | None, Field(description="回复后同时改成的状态")] = None,
    quick_reply_id: Annotated[int | None, Field(description="引用的快捷回复")] = None,
    content_type: Annotated[Literal["", "text", "markdown", "html"], Field(description="正文格式")] = "",
) -> Any:
    """回复工单或添加内部备注。对提单人可见的回复发出前，先让用户确认内容。"""
    return await TICKET_REPLY(ticketId=ticket_id, body=body(
        content=content, internal=internal, nextStatus=next_status, quickReplyId=quick_reply_id, contentType=content_type or None,
    ))


@server.tool(**WRITE("tickets"))
async def set_status(
    ticket_id: TicketID,
    status: Annotated[Status, Field(description="目标状态")],
    reason: str = "",
    solution: Annotated[str, Field(description="解决方案（改为 resolved 时建议填写）")] = "",
) -> Any:
    """修改工单状态（受理、等待中、解决、不采纳、关闭、重开）。改为 rejected 时 reason 即不采纳理由，会告知反馈人。"""
    return await TICKET_STATUS(ticketId=ticket_id, body=body(status=status, reason=reason, solution=solution))


@server.tool(**WRITE("tickets"))
async def assign(
    ticket_id: TicketID,
    assignee_admin_id: Annotated[int | None, Field(description="指派给哪个管理员")] = None,
    group_id: Annotated[int | None, Field(description="转派给处理组")] = None,
    auto_pick: Annotated[bool, Field(description="由处理组按分配策略自动挑人")] = False,
    reason: str = "",
) -> Any:
    """指派工单给管理员或处理组。"""
    if not (assignee_admin_id or group_id or auto_pick):
        raise ToolError("需要 assignee_admin_id、group_id 或 auto_pick 之一")
    return await TICKET_ASSIGN(ticketId=ticket_id, body=body(assigneeAdminId=assignee_admin_id, groupId=group_id, autoPick=auto_pick, reason=reason))


@server.tool(**WRITE("tickets", idempotent=True))
async def watch(
    ticket_id: TicketID,
    watch: Annotated[bool, Field(description="true 关注 / false 取消关注（当前管理员）")] = True,
    watcher_admin_ids: Annotated[list[int] | None, Field(description="给出时改为整体设置关注人列表")] = None,
) -> Any:
    """关注 / 取消关注工单，或整体设置关注人。"""
    if watcher_admin_ids is not None:
        return await TICKET_WATCHERS(ticketId=ticket_id, body={"adminIds": watcher_admin_ids})
    return await TICKET_WATCH(ticketId=ticket_id, query={"watch": "true" if watch else "false"})


@server.tool(**DESTRUCTIVE("tickets"))
async def bulk(
    ticket_ids: Annotated[list[int], Field(min_length=1, max_length=500)],
    action: Annotated[Literal["assign", "status", "close", "priority", "tag", "delete"], Field(description="批量动作")],
    status: Status | None = None,
    priority: Priority | None = None,
    assignee_admin_id: int | None = None,
    group_id: int | None = None,
    tags: list[str] | None = None,
    reason: str = "",
) -> Any:
    """批量处理工单（指派、改状态、关闭、改优先级、打标签、删除）。影响多张工单，先向用户确认。"""
    return await TICKETS_BULK(body=body(
        ids=ticket_ids, action=action, status=status, priority=priority, assigneeAdminId=assignee_admin_id,
        groupId=group_id, tags=tags, reason=reason,
    ))


@server.tool(**DESTRUCTIVE("tickets"))
async def delete_message(
    ticket_id: TicketID,
    message_id: Annotated[int, Field(gt=0, description="要撤回的消息 ID（tickets_get 的 messages[].id）")],
    reason: Annotated[str, Field(max_length=200, description="撤回原因，只在控制台与操作记录里可见")] = "",
) -> Any:
    """撤回工单里的一条回复或内部备注（软删除，原文留档）。执行前先向用户复述要撤回的内容。

    提单人 / 反馈人侧不再显示这条消息，它生成的站内信与管理员收件箱条目一并删除，时间线记一条撤回事件。
    提单人自己的消息不能撤回；撤回他人的回复需要 ticket:manage。已发出的邮件与 IM 消息收不回。"""
    return await TICKET_MESSAGE_DELETE(ticketId=ticket_id, messageId=message_id, body=body(reason=reason))


@server.tool(name="delete", **DESTRUCTIVE("tickets"))
async def delete_ticket(ticket_id: TicketID) -> Any:
    """删除工单（不可恢复）。"""
    return await TICKET_DELETE(ticketId=ticket_id)


@server.tool(**READ("tickets"))
async def stats(
    app: AppFilter = "",
    kind: Kind | None = None,
    trend_days: Annotated[int, Field(ge=0, le=365, description="另取最近 N 天趋势，0 为不取")] = 0,
    agents: Annotated[bool, Field(description="另取受理人工作量排行")] = False,
) -> Any:
    """工单统计：各状态数量、SLA、满意度；可附带趋势与受理人排行、我的待办数。"""
    appid = await _appid(app)
    result: dict[str, Any] = {
        "stats": await STATS(query=q(appid=appid, kind=kind)),
        "workbench": await WORKBENCH(),
    }
    if trend_days:
        result["trend"] = await TREND(query=q(appid=appid, days=trend_days))
    if agents:
        result["agents"] = await AGENTS(query=q(appid=appid, limit=20))
    return result


@server.tool(**READ("tickets"))
async def config(
    section: Annotated[Literal["metadata", "categories", "groups", "sla_policies", "quick_replies"], Field(description="要看的配置")],
    app: AppFilter = "",
    kind: Annotated[Kind | None, Field(description="分类的适用类型")] = None,
    enabled_only: bool = False,
) -> Any:
    """工单配置：元数据（状态 / 优先级枚举）、分类、处理组、SLA 策略、快捷回复。"""
    appid = await _appid(app)
    if section == "metadata":
        return await METADATA()
    if section == "categories":
        return await CATEGORIES(query=q(appid=appid, kind=kind, enabled="true" if enabled_only else None))
    if section == "groups":
        return await GROUPS(query=q(appid=appid))
    if section == "sla_policies":
        return await SLA_POLICIES(query=q(appid=appid))
    return await QUICK_REPLIES(query=q(appid=appid))


@server.tool(**WRITE("tickets"))
async def save_category(
    name: Annotated[str, Field(min_length=1)],
    key: Annotated[str, Field(min_length=1, description="分类代码（英文短标识，如 refund）")],
    category_id: Annotated[int | None, Field(gt=0, description="给 ID 即修改，不给即新建")] = None,
    app: AppFilter = "",
    kind: Kind | None = None,
    description: str = "",
    parent_id: int | None = None,
    default_group_id: int | None = None,
    default_priority: Priority | None = None,
    sla_policy_id: int | None = None,
    sort: int | None = None,
    enabled: bool = True,
    user_submittable: Annotated[bool, Field(description="用户端能否选这个分类提单")] = True,
    form_schema: Annotated[list[dict[str, Any]] | None, Field(description="附加表单字段：[{key,label,type,required,options,placeholder}]")] = None,
) -> Any:
    """新建或修改工单分类。"""
    payload = body(
        appid=await _appid(app), key=key, kind=kind, name=name, description=description, parentId=parent_id,
        defaultGroupId=default_group_id, defaultPriority=default_priority, slaPolicyId=sla_policy_id, sort=sort,
        enabled=enabled, userSubmittable=user_submittable, formSchema=form_schema,
    )
    return await (CATEGORY_UPDATE(id=category_id, body=payload) if category_id else CATEGORY_CREATE(body=payload))


@server.tool(**WRITE("tickets"))
async def save_group(
    name: Annotated[str, Field(min_length=1)],
    key: Annotated[str, Field(min_length=1, description="处理组代码（英文短标识，如 tier1）")],
    group_id: Annotated[int | None, Field(gt=0, description="给 ID 即修改，不给即新建")] = None,
    app: AppFilter = "",
    description: str = "",
    assign_strategy: Annotated[Literal["manual", "round_robin", "least_open"], Field(description="分配策略")] = "manual",
    enabled: bool = True,
    members: Annotated[list[dict[str, Any]] | None, Field(description="同时整体设置成员：[{adminId, role}]")] = None,
) -> Any:
    """新建或修改工单处理组，可同时设置成员。"""
    payload = body(appid=await _appid(app), key=key, name=name, description=description, assignStrategy=assign_strategy, enabled=enabled)
    saved = await (GROUP_UPDATE(id=group_id, body=payload) if group_id else GROUP_CREATE(body=payload))
    if members is not None:
        target = group_id or (saved or {}).get("id")
        if not target:
            raise ToolError("处理组已保存，但没拿到 ID，无法设置成员；请用 tickets_config 查到 ID 后重试")
        saved = {"group": saved, "members": await GROUP_MEMBERS(id=target, body={"members": members})}
    return saved


@server.tool(**WRITE("tickets"))
async def save_sla_policy(
    name: Annotated[str, Field(min_length=1)],
    policy_id: Annotated[int | None, Field(gt=0, description="给 ID 即修改，不给即新建")] = None,
    app: AppFilter = "",
    description: str = "",
    first_response_minutes: Annotated[dict[str, int] | None, Field(description="按优先级的首次响应时限（分钟），如 {\"urgent\": 30, \"normal\": 240}")] = None,
    resolve_minutes: Annotated[dict[str, int] | None, Field(description="按优先级的解决时限（分钟）")] = None,
    warn_ratio: Annotated[float | None, Field(gt=0, lt=1, description="用到时限的多少比例时预警")] = None,
    business_hours: Annotated[dict[str, Any] | None, Field(description="工作时间：{days, start, end, timezone}")] = None,
    enabled: bool = True,
) -> Any:
    """新建或修改 SLA 策略。"""
    payload = body(appid=await _appid(app), name=name, description=description, firstResponseMinutes=first_response_minutes,
                   resolveMinutes=resolve_minutes, warnRatio=warn_ratio, businessHours=business_hours, enabled=enabled)
    return await (SLA_UPDATE(id=policy_id, body=payload) if policy_id else SLA_CREATE(body=payload))


@server.tool(**WRITE("tickets"))
async def save_quick_reply(
    title: Annotated[str, Field(min_length=1)],
    content: Annotated[str, Field(min_length=1)],
    reply_id: Annotated[int | None, Field(gt=0, description="给 ID 即修改，不给即新建")] = None,
    app: AppFilter = "",
    category_id: int | None = None,
    private: Annotated[bool, Field(description="仅自己可用")] = False,
    sort: int | None = None,
    enabled: bool = True,
) -> Any:
    """新建或修改快捷回复。"""
    payload = body(appid=await _appid(app), title=title, content=content, categoryId=category_id, private=private, sort=sort, enabled=enabled)
    return await (QUICK_REPLY_UPDATE(id=reply_id, body=payload) if reply_id else QUICK_REPLY_CREATE(body=payload))


@server.tool(**DESTRUCTIVE("tickets"))
async def delete_config(
    section: Annotated[Literal["category", "group", "sla_policy", "quick_reply"], Field(description="删除哪类配置")],
    config_id: Annotated[int, Field(gt=0)],
) -> Any:
    """删除工单分类、处理组、SLA 策略或快捷回复。"""
    route = {"category": CATEGORY_DELETE, "group": GROUP_DELETE, "sla_policy": SLA_DELETE, "quick_reply": QUICK_REPLY_DELETE}[section]
    return await route(id=config_id)
