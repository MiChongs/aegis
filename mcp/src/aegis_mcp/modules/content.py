"""内容与通知：应用公告、Banner、用户站内信（发送与清理）、站内信群发任务（全站或按人群、定时、取消、撤回）、平台公告、内容总览。"""

from __future__ import annotations

import uuid
from typing import Annotated, Any, Literal

from fastmcp import FastMCP
from fastmcp.exceptions import ToolError
from pydantic import Field

from ..core import DELETE, DESTRUCTIVE, GET, POST, PUT, READ, WRITE, AppRef, IsoTime, Limit, Page, body, q

NAMESPACE = "content"
server = FastMCP("content")

OVERVIEW = GET("/api/admin/apps/:appkey/content/overview")

NOTICES = GET("/api/admin/apps/:appkey/notices")
NOTICE_CREATE = POST("/api/admin/apps/:appkey/notices")
NOTICE_UPDATE = PUT("/api/admin/apps/:appkey/notices/:noticeId")
NOTICE_DELETE = DELETE("/api/admin/apps/:appkey/notices/:noticeId")
NOTICES_DELETE = DELETE("/api/admin/apps/:appkey/notices")

BANNERS = GET("/api/admin/apps/:appkey/banners")
BANNER_CREATE = POST("/api/admin/apps/:appkey/banners")
BANNER_UPDATE = PUT("/api/admin/apps/:appkey/banners/:bannerId")
BANNER_DELETE = DELETE("/api/admin/apps/:appkey/banners/:bannerId")
BANNERS_DELETE = DELETE("/api/admin/apps/:appkey/banners")
BANNERS_ORDER = PUT("/api/admin/apps/:appkey/banners/order")

NOTIFICATIONS = GET("/api/admin/apps/:appkey/notifications")
NOTIFY_BULK = POST("/api/admin/apps/:appkey/notifications/bulk")
NOTIFICATIONS_DELETE = DELETE("/api/admin/apps/:appkey/notifications")
NOTIFICATIONS_DELETE_BY_FILTER = POST("/api/admin/apps/:appkey/notifications/delete-by-filter")

CAMPAIGN_CATALOG = GET("/api/admin/apps/:appkey/notifications/campaigns/catalog")
CAMPAIGN_PREVIEW = POST("/api/admin/apps/:appkey/notifications/campaigns/preview")
CAMPAIGNS = GET("/api/admin/apps/:appkey/notifications/campaigns")
CAMPAIGN_CREATE = POST("/api/admin/apps/:appkey/notifications/campaigns")
CAMPAIGN = GET("/api/admin/apps/:appkey/notifications/campaigns/:campaignId")
CAMPAIGN_CANCEL = POST("/api/admin/apps/:appkey/notifications/campaigns/:campaignId/cancel")
CAMPAIGN_RECALL = POST("/api/admin/apps/:appkey/notifications/campaigns/:campaignId/recall")

ANNOUNCEMENTS = GET("/api/admin/system/announcements")
ANNOUNCEMENT = GET("/api/admin/system/announcements/:id")
ANNOUNCEMENT_CREATE = POST("/api/admin/system/announcements")
ANNOUNCEMENT_UPDATE = PUT("/api/admin/system/announcements/:id")
ANNOUNCEMENT_DELETE = DELETE("/api/admin/system/announcements/:id")
ANNOUNCEMENT_PUBLISH = POST("/api/admin/system/announcements/:id/publish")
ANNOUNCEMENT_ARCHIVE = POST("/api/admin/system/announcements/:id/archive")

NoticeType = Literal["notice", "activity", "maintenance", "update", "security"]
NoticeLevel = Literal["normal", "important", "critical"]
NoticeStatus = Literal["draft", "published", "archived"]
Ids = Annotated[list[int], Field(min_length=1, max_length=500, description="ID 列表")]


@server.tool(**READ("content"))
async def overview(app: AppRef) -> Any:
    """应用内容总览：Banner、公告的数量与状态分布。"""
    return await OVERVIEW(appkey=app)


# ── 公告 ──

@server.tool(**READ("content"))
async def notices(
    app: AppRef,
    status: Annotated[Literal["", "draft", "published", "archived"], Field(description="生命周期状态")] = "",
    type: Annotated[Literal["", "notice", "activity", "maintenance", "update", "security"], Field(description="公告类型")] = "",
    level: Annotated[Literal["", "normal", "important", "critical"], Field(description="提示级别")] = "",
    keyword: str = "",
    page: Page = 1,
    limit: Limit = 20,
) -> Any:
    """应用公告列表（分页）。"""
    return await NOTICES(appkey=app, query=q(status=status, type=type, level=level, keyword=keyword, page=page, limit=limit))


@server.tool(**WRITE("content"))
async def create_notice(
    app: AppRef,
    title: Annotated[str, Field(min_length=1)],
    content: Annotated[str, Field(min_length=1, description="正文（Markdown）")],
    type: Annotated[NoticeType, Field(description="公告类型")] = "notice",
    level: Annotated[NoticeLevel, Field(description="提示级别")] = "normal",
    status: Annotated[Literal["draft", "published"], Field(description="draft 存草稿 / published 立即对用户可见")] = "draft",
    pinned: bool = False,
    start_time: IsoTime = "",
    end_time: IsoTime = "",
) -> Any:
    """新建应用公告。默认存为草稿，确认后再用 content_update_notice 发布。"""
    return await NOTICE_CREATE(appkey=app, body=body(
        title=title, content=content, type=type, level=level, status=status, pinned=pinned,
        startTime=start_time or None, endTime=end_time or None,
    ))


@server.tool(**WRITE("content"))
async def update_notice(
    app: AppRef,
    notice_id: Annotated[int, Field(gt=0)],
    title: str | None = None,
    content: str | None = None,
    type: NoticeType | None = None,
    level: NoticeLevel | None = None,
    status: Annotated[NoticeStatus | None, Field(description="改为 published 即发布，archived 即下线归档")] = None,
    pinned: bool | None = None,
    start_time: Annotated[str | None, Field(description="生效时间 ISO 8601")] = None,
    end_time: Annotated[str | None, Field(description="结束时间 ISO 8601")] = None,
) -> Any:
    """修改公告（只传要改的字段），也用于发布与归档。"""
    return await NOTICE_UPDATE(appkey=app, noticeId=notice_id, body=body(
        title=title, content=content, type=type, level=level, status=status, pinned=pinned,
        startTime=start_time, endTime=end_time,
    ))


@server.tool(**DESTRUCTIVE("content"))
async def delete_notices(app: AppRef, notice_ids: Ids) -> Any:
    """删除公告（一条或多条，不可恢复）。"""
    if len(notice_ids) == 1:
        return await NOTICE_DELETE(appkey=app, noticeId=notice_ids[0])
    return await NOTICES_DELETE(appkey=app, body={"ids": notice_ids})


# ── Banner ──

@server.tool(**READ("content"))
async def banners(
    app: AppRef,
    status: Annotated[Literal["", "enabled", "disabled"], Field(description="启用状态")] = "",
    type: str = "",
    keyword: str = "",
) -> Any:
    """应用 Banner 列表（按展示顺序，不分页）。"""
    return await BANNERS(appkey=app, query=q(status=status, type=type, keyword=keyword))


def _banner_body(header, title, content, url, type, position, status, start_time, end_time) -> dict[str, Any]:
    return body(header=header, title=title, content=content, url=url, type=type, position=position,
                status=status, startTime=start_time or None, endTime=end_time or None)


@server.tool(**WRITE("content"))
async def create_banner(
    app: AppRef,
    title: Annotated[str, Field(min_length=1)],
    header: Annotated[str, Field(description="图片地址（先在控制台上传图片，或填外链）")],
    url: Annotated[str, Field(description="点击跳转地址")] = "",
    content: str = "",
    type: Annotated[str, Field(description="Banner 类型（应用自定义）")] = "",
    position: Annotated[int | None, Field(description="位置序号")] = None,
    status: Annotated[bool, Field(description="是否启用")] = True,
    start_time: IsoTime = "",
    end_time: IsoTime = "",
) -> Any:
    """新建 Banner。图片上传是二进制接口，请提供已有图片地址。"""
    return await BANNER_CREATE(appkey=app, body=_banner_body(header, title, content, url, type or None, position, status, start_time, end_time))


@server.tool(**WRITE("content"))
async def update_banner(
    app: AppRef,
    banner_id: Annotated[int, Field(gt=0)],
    title: str | None = None,
    header: Annotated[str | None, Field(description="图片地址")] = None,
    url: str | None = None,
    content: str | None = None,
    type: str | None = None,
    position: int | None = None,
    status: Annotated[bool | None, Field(description="启用 / 停用")] = None,
    start_time: str | None = None,
    end_time: str | None = None,
) -> Any:
    """修改 Banner（只传要改的字段），也用于启停。"""
    return await BANNER_UPDATE(appkey=app, bannerId=banner_id, body=body(
        header=header, title=title, content=content, url=url, type=type, position=position,
        status=status, startTime=start_time, endTime=end_time,
    ))


@server.tool(**WRITE("content", idempotent=True))
async def reorder_banners(app: AppRef, banner_ids: Annotated[list[int], Field(min_length=1, description="完整的展示顺序（全部 Banner ID）")]) -> Any:
    """调整 Banner 展示顺序：提交完整顺序。"""
    return await BANNERS_ORDER(appkey=app, body={"ids": banner_ids})


@server.tool(**DESTRUCTIVE("content"))
async def delete_banners(app: AppRef, banner_ids: Ids) -> Any:
    """删除 Banner（一条或多条，不可恢复）。"""
    if len(banner_ids) == 1:
        return await BANNER_DELETE(appkey=app, bannerId=banner_ids[0])
    return await BANNERS_DELETE(appkey=app, body={"ids": banner_ids})


# ── 站内信 ──

@server.tool(**READ("content"))
async def notifications(
    app: AppRef,
    keyword: str = "",
    category: Annotated[str, Field(description="站内信分类")] = "",
    type: str = "",
    level: str = "",
    status: Annotated[Literal["", "read", "unread"], Field(description="已读状态")] = "",
    user_id: Annotated[int | None, Field(gt=0, description="只看发给这个用户（管理端用户 ID）的")] = None,
    campaign_id: Annotated[int | None, Field(gt=0, description="只看某个群发任务发出的（content_campaigns 可查）")] = None,
    page: Page = 1,
    limit: Limit = 20,
) -> Any:
    """应用里已发出的用户站内信（管理视图，分页，含已过期的）。列表项带 campaignId（单条投递为 null）。"""
    return await NOTIFICATIONS(appkey=app, query=q(
        keyword=keyword, category=category, type=type, level=level, status=status, userId=user_id,
        campaignId=campaign_id, page=page, limit=limit,
    ))


@server.tool(**DESTRUCTIVE("content"))
async def notify_users(
    app: AppRef,
    title: Annotated[str, Field(min_length=1)],
    content: Annotated[str, Field(min_length=1)],
    user_ids: Annotated[list[int] | None, Field(max_length=5000, description="接收人用户 ID；不填则按 keyword / enabled 筛选用户群发")] = None,
    keyword: Annotated[str, Field(description="不指定 user_ids 时按关键词筛选接收人")] = "",
    enabled_only: Annotated[bool | None, Field(description="群发时只发给正常账号")] = None,
    max_recipients: Annotated[int | None, Field(gt=0, description="群发时最多发给多少人")] = None,
    type: Annotated[str, Field(description="消息类型，决定站内信分类（如 system / activity）")] = "system",
    category: str = "",
    level: Annotated[str, Field(description="info / warning / error 等")] = "info",
    image: Annotated[str, Field(description="配图地址")] = "",
    action_type: Annotated[Literal["", "route", "url"], Field(description="点击动作：route 应用内路由键 / url https 链接")] = "",
    action_target: str = "",
    action_label: str = "",
    expires_at: IsoTime = "",
) -> Any:
    """给用户发站内信，实时推送到用户全部设备。可指定用户，也可按筛选条件群发。发送前务必向用户确认接收范围与内容。

    内部也建一个群发任务。不超过 2000 人时当场发完，返回 delivered、recipientIds 与 async=false；更多时转后台，
    立刻返回 async=true、delivered=0 与 campaign（用 content_campaign 看进度，可取消或撤回）。
    全站、按会员 / 活跃 / 平台等条件或定时群发请用 content_create_campaign。
    """
    if not user_ids and not keyword and not max_recipients:
        raise ToolError("未指定 user_ids 时，必须给 keyword 或 max_recipients 限定群发范围，避免误发全体用户")
    action = body(type=action_type or None, target=action_target or None, label=action_label or None) or None
    return await NOTIFY_BULK(appkey=app, body=body(
        userIds=user_ids, keyword=keyword or None, enabled=enabled_only, limit=max_recipients,
        type=type, category=category or None, title=title, content=content, level=level,
        image=image or None, action=action, expiresAt=expires_at or None,
    ))


@server.tool(**DESTRUCTIVE("content"))
async def delete_notifications(
    app: AppRef,
    notification_ids: Annotated[list[int] | None, Field(description="按 ID 删除")] = None,
    keyword: Annotated[str, Field(description="按筛选条件删除：关键词")] = "",
    category: str = "",
    type: str = "",
    level: str = "",
    max_count: Annotated[int | None, Field(gt=0, description="按筛选删除时最多删除条数")] = None,
) -> Any:
    """删除已发出的站内信（用户端随之消失，不可恢复）。给 ID 就按 ID 删，否则按筛选条件删。"""
    if notification_ids:
        return await NOTIFICATIONS_DELETE(appkey=app, body={"ids": notification_ids})
    if not (keyword or category or type or level):
        raise ToolError("按筛选删除时至少给一个条件（keyword / category / type / level）")
    return await NOTIFICATIONS_DELETE_BY_FILTER(appkey=app, body=body(
        keyword=keyword or None, category=category or None, type=type or None, level=level or None, limit=max_count,
    ))


# ── 群发任务 ──

CampaignStatus = Literal["", "scheduled", "queued", "running", "succeeded", "failed", "cancelled", "recalling", "recalled"]
Platform = Literal["android", "ios", "harmonyos", "windows", "macos", "linux", "web"]
CampaignID = Annotated[int, Field(gt=0, description="群发任务 ID（content_campaigns 可查）")]

Scope = Annotated[Literal["all", "users", "filter"], Field(description="人群范围：all 全站（一键全站）/ users 指定用户 / filter 按条件筛选")]
AudienceUserIds = Annotated[list[int] | None, Field(max_length=10000, description="scope=users 时必填：接收人用户 ID，最多 10000（更多请用 filter）")]
ExcludeUserIds = Annotated[list[int] | None, Field(max_length=10000, description="排除这些用户（任何范围都生效），最多 10000")]
Account = Annotated[Literal["available", "all", "disabled"], Field(description="账号状态：available 可用账号（已启用、不在禁用期、无生效封禁）/ all 不论状态 / disabled 只发被禁用的")]
AudienceKeyword = Annotated[str, Field(max_length=64, description="仅 filter：账号、昵称、邮箱、手机号模糊匹配，或等于用户 ID")]
Membership = Annotated[Literal["", "member", "non_member", "lifetime", "non_lifetime"], Field(description="仅 filter：会员身份，空不限")]
Days = Annotated[int | None, Field(ge=1, le=3650)]
Level = Annotated[int | None, Field(ge=1, le=1000)]
Platforms = Annotated[list[Platform] | None, Field(description="仅 filter：用这些平台的设备登录过（任一即可）")]
MaxRecipients = Annotated[int | None, Field(gt=0, description="最多发给多少人（按用户 ID 升序取前 N 个），不填不限")]

_FILTER_ONLY = ("keyword", "membership", "registered_within_days", "registered_before_days", "active_within_days",
                "inactive_days", "min_level", "max_level", "platforms")


def _audience(scope: str, user_ids, exclude_user_ids, account, keyword, membership, registered_within_days,
              registered_before_days, active_within_days, inactive_days, min_level, max_level, platforms,
              max_recipients) -> dict[str, Any]:
    """组人群并做与后端同口径的前置检查：与范围无关的条件直接报错，不让后端静默清掉后扩大到全站。"""
    filters = dict(keyword=keyword, membership=membership, registered_within_days=registered_within_days,
                   registered_before_days=registered_before_days, active_within_days=active_within_days,
                   inactive_days=inactive_days, min_level=min_level, max_level=max_level, platforms=platforms)
    given = [name for name in _FILTER_ONLY if filters[name] not in (None, "", [])]
    if scope == "users" and not user_ids:
        raise ToolError("scope=users 时必须给 user_ids")
    if scope != "users" and user_ids:
        raise ToolError("user_ids 只在 scope=users 时有效")
    if scope != "filter" and given:
        raise ToolError(f"{', '.join(given)} 只在 scope=filter 时有效；要按条件筛选请把 scope 设为 filter")
    if scope == "filter" and not given:
        raise ToolError("scope=filter 时至少给一个筛选条件；要发全站请用 scope=all")
    if active_within_days and inactive_days:
        raise ToolError("active_within_days 与 inactive_days 不能同时设置")
    if registered_within_days and registered_before_days and registered_before_days > registered_within_days:
        raise ToolError("registered_before_days 不能大于 registered_within_days（范围为空）")
    if min_level and max_level and min_level > max_level:
        raise ToolError("min_level 不能高于 max_level")
    return q(
        scope=scope, userIds=user_ids, excludeUserIds=exclude_user_ids, account=account, keyword=keyword,
        membership=membership, registeredWithinDays=registered_within_days, registeredBeforeDays=registered_before_days,
        activeWithinDays=active_within_days, inactiveDays=inactive_days, minLevel=min_level, maxLevel=max_level,
        platforms=platforms, limit=max_recipients,
    )


@server.tool(**READ("content"))
async def campaign_catalog(app: AppRef) -> Any:
    """群发任务的表单目录：预设人群（全站、会员、新用户、活跃、沉默、各平台……及其 audience）、平台、会员身份、账号状态、分类、任务状态、上限。"""
    return await CAMPAIGN_CATALOG(appkey=app)


@server.tool(**READ("content"))
async def preview_audience(
    app: AppRef,
    scope: Scope = "all",
    user_ids: AudienceUserIds = None,
    exclude_user_ids: ExcludeUserIds = None,
    account: Account = "available",
    keyword: AudienceKeyword = "",
    membership: Membership = "",
    registered_within_days: Annotated[Days, Field(description="仅 filter：注册不超过 N 天（新用户）")] = None,
    registered_before_days: Annotated[Days, Field(description="仅 filter：注册满 N 天（老用户）")] = None,
    active_within_days: Annotated[Days, Field(description="仅 filter：最近 N 天登录或在线过（与 inactive_days 互斥）")] = None,
    inactive_days: Annotated[Days, Field(description="仅 filter：超过 N 天没登录也没在线（沉默召回）")] = None,
    min_level: Annotated[Level, Field(description="仅 filter：最低等级")] = None,
    max_level: Annotated[Level, Field(description="仅 filter：最高等级")] = None,
    platforms: Platforms = None,
    max_recipients: MaxRecipients = None,
) -> Any:
    """预览群发人群（只数人、不写数据）：返回归一后的 audience、中文摘要 label、人数 total 与至多 10 个样例用户。

    参数与 content_create_campaign 的人群参数相同；发起群发前先用它核对人数并告诉用户。
    """
    audience = _audience(scope, user_ids, exclude_user_ids, account, keyword, membership, registered_within_days,
                         registered_before_days, active_within_days, inactive_days, min_level, max_level, platforms,
                         max_recipients)
    return await CAMPAIGN_PREVIEW(appkey=app, body={"audience": audience})


@server.tool(**READ("content"))
async def campaigns(
    app: AppRef,
    status: Annotated[CampaignStatus, Field(description="任务状态：scheduled 定时 / queued 排队 / running 发送中 / succeeded 完成 / failed 失败 / cancelled 已取消 / recalling 撤回中 / recalled 已撤回")] = "",
    page: Page = 1,
    limit: Annotated[int, Field(ge=1, le=100, description="每页条数")] = 20,
) -> Any:
    """站内信群发任务列表（新的在前）：标题、人群摘要、状态、人数 total、进度 processed / delivered / skipped、撤回条数。"""
    return await CAMPAIGNS(appkey=app, query=q(status=status, page=page, limit=limit))


@server.tool(**READ("content"))
async def campaign(app: AppRef, campaign_id: CampaignID) -> Any:
    """一个群发任务的详情与进度，另现算已读人数 readCount。失败、取消、撤回的说明在 message。"""
    return await CAMPAIGN(appkey=app, campaignId=campaign_id)


@server.tool(**DESTRUCTIVE("content"))
async def create_campaign(
    app: AppRef,
    title: Annotated[str, Field(min_length=1, max_length=200)],
    scope: Scope,
    content: Annotated[str, Field(description="正文；留空用标题兜底")] = "",
    user_ids: AudienceUserIds = None,
    exclude_user_ids: ExcludeUserIds = None,
    account: Account = "available",
    keyword: AudienceKeyword = "",
    membership: Membership = "",
    registered_within_days: Annotated[Days, Field(description="仅 filter：注册不超过 N 天（新用户）")] = None,
    registered_before_days: Annotated[Days, Field(description="仅 filter：注册满 N 天（老用户）")] = None,
    active_within_days: Annotated[Days, Field(description="仅 filter：最近 N 天登录或在线过（与 inactive_days 互斥）")] = None,
    inactive_days: Annotated[Days, Field(description="仅 filter：超过 N 天没登录也没在线（沉默召回）")] = None,
    min_level: Annotated[Level, Field(description="仅 filter：最低等级")] = None,
    max_level: Annotated[Level, Field(description="仅 filter：最高等级")] = None,
    platforms: Platforms = None,
    max_recipients: MaxRecipients = None,
    type: Annotated[str, Field(description="消息类型，默认 announcement")] = "",
    category: Annotated[str, Field(description="站内信分类，默认 announcement（公告活动）；可选值见 content_campaign_catalog")] = "",
    level: Annotated[Literal["info", "success", "warning", "error"], Field(description="提示级别")] = "info",
    image: Annotated[str, Field(description="配图地址（https）")] = "",
    action_type: Annotated[Literal["", "route", "url"], Field(description="点击动作：route 应用内路由键 / url https 链接")] = "",
    action_target: str = "",
    action_label: str = "",
    expires_at: Annotated[str, Field(description="过期时间 ISO 8601，一年内；定时发送时必须晚于定时时间。留空不过期")] = "",
    scheduled_at: Annotated[str, Field(description="定时发送 ISO 8601，90 天内；留空或已到点即立刻排队")] = "",
    metadata: dict[str, Any] | None = None,
    request_id: Annotated[str, Field(max_length=64, description="幂等键：同一个值重复提交返回第一次的任务（created=false）；留空自动生成")] = "",
) -> Any:
    """发起站内信群发任务（后台分批写入并实时推送，立刻返回 {campaign, created}）。可一键全站、指定用户或按条件筛选，可定时。

    发起前必须先用 content_preview_audience 预览同样的人群，把人数 total 与标题、正文、发送时间复述给用户并取得确认，
    scope=all（全站）尤其如此。已发出的消息只能用 content_recall_campaign 整体撤回；重试同一次发起请复用返回的 requestId。
    """
    audience = _audience(scope, user_ids, exclude_user_ids, account, keyword, membership, registered_within_days,
                         registered_before_days, active_within_days, inactive_days, min_level, max_level, platforms,
                         max_recipients)
    action = body(type=action_type or None, target=action_target or None, label=action_label or None) or None
    return await CAMPAIGN_CREATE(appkey=app, body=body(
        requestId=request_id.strip() or uuid.uuid4().hex, type=type or None, category=category or None,
        title=title, content=content or None, level=level, action=action, image=image or None,
        expiresAt=expires_at or None, metadata=metadata, scheduledAt=scheduled_at or None, audience=audience,
    ))


@server.tool(**WRITE("content", idempotent=True))
async def cancel_campaign(app: AppRef, campaign_id: CampaignID) -> Any:
    """取消定时、排队或发送中的群发任务。发送中的写完当前一批就停，已发出的消息保留（要收回请用 content_recall_campaign）。"""
    return await CAMPAIGN_CANCEL(appkey=app, campaignId=campaign_id)


@server.tool(**DESTRUCTIVE("content"))
async def recall_campaign(app: AppRef, campaign_id: CampaignID) -> Any:
    """撤回群发任务：后台把这个任务发出的全部消息从每个收件人的消息中心删掉（不可恢复）；还没开始的直接取消。

    影响这个任务的全部收件人：执行前先用 content_campaign 查清标题与已发人数 delivered，向用户复述并取得确认。
    """
    return await CAMPAIGN_RECALL(appkey=app, campaignId=campaign_id)


# ── 平台公告 ──

@server.tool(**READ("content"))
async def announcements(announcement_id: Annotated[int | None, Field(gt=0, description="给 ID 时取详情")] = None) -> Any:
    """平台公告（面向全部管理员的控制台公告）列表或详情。"""
    if announcement_id:
        return await ANNOUNCEMENT(id=announcement_id)
    return await ANNOUNCEMENTS()


@server.tool(**WRITE("content"))
async def save_announcement(
    title: Annotated[str, Field(min_length=1)],
    content: Annotated[str, Field(min_length=1)],
    announcement_id: Annotated[int | None, Field(gt=0, description="给 ID 即修改，不给即新建")] = None,
    type: str = "",
    level: str = "",
    pinned: bool = False,
    expires_at: IsoTime = "",
    metadata: dict[str, Any] | None = None,
) -> Any:
    """新建或修改平台公告（新建为草稿，用 content_announcement_status 发布）。需要超级管理员。"""
    payload = body(title=title, content=content, type=type or None, level=level or None, pinned=pinned,
                   expiresAt=expires_at or None, metadata=metadata)
    if announcement_id:
        return await ANNOUNCEMENT_UPDATE(id=announcement_id, body=payload)
    return await ANNOUNCEMENT_CREATE(body=payload)


@server.tool(**DESTRUCTIVE("content"))
async def announcement_status(
    announcement_id: Annotated[int, Field(gt=0)],
    action: Annotated[Literal["publish", "archive", "delete"], Field(description="publish 发布 / archive 归档 / delete 删除")],
) -> Any:
    """发布、归档或删除平台公告。"""
    if action == "publish":
        return await ANNOUNCEMENT_PUBLISH(id=announcement_id)
    if action == "archive":
        return await ANNOUNCEMENT_ARCHIVE(id=announcement_id)
    return await ANNOUNCEMENT_DELETE(id=announcement_id)
