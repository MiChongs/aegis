"""积分商城：开关、权益目录、商品增删改与上下架排序、兑换记录与人工发放 / 取消、统计、赠好友的赠送记录、审核队列、撤销追回与赠送风控（docs/points-store-gift.md）。"""

from __future__ import annotations

from typing import Annotated, Any, Literal

from fastmcp import FastMCP
from fastmcp.exceptions import ToolError
from pydantic import BaseModel, Field

from ..core import DELETE, DESTRUCTIVE, GET, POST, PUT, READ, WRITE, AppRef, IsoTime, Page, body, q

NAMESPACE = "store"
server = FastMCP("store")

B = "/api/admin/apps/:appkey/points-store"
CATALOG = GET(f"{B}/catalog")
SETTINGS = GET(f"{B}/settings")
SETTINGS_SAVE = PUT(f"{B}/settings")
ITEMS = GET(f"{B}/items")
ITEM_CREATE = POST(f"{B}/items")
ITEM_UPDATE = PUT(f"{B}/items/:itemId")
ITEM_STATUS = PUT(f"{B}/items/:itemId/status")
ITEM_DELETE = DELETE(f"{B}/items/:itemId")
ITEM_ORDER = PUT(f"{B}/item-order")
REDEMPTIONS = GET(f"{B}/redemptions")
FULFILL = POST(f"{B}/redemptions/:redemptionId/fulfill")
CANCEL = POST(f"{B}/redemptions/:redemptionId/cancel")
STATS = GET(f"{B}/stats")
GIFTS = GET(f"{B}/gifts")
GIFT = GET(f"{B}/gifts/:giftId")
GIFT_APPROVE = POST(f"{B}/gifts/:giftId/approve")
GIFT_REJECT = POST(f"{B}/gifts/:giftId/reject")
GIFT_REVOKE = POST(f"{B}/gifts/:giftId/revoke")
GIFT_POLICY = GET(f"{B}/gift-policy")
GIFT_POLICY_SAVE = PUT(f"{B}/gift-policy")

ItemID = Annotated[int, Field(gt=0, description="商品 ID（store_items 结果里的 id）")]
RedemptionID = Annotated[int, Field(gt=0, description="兑换记录 ID（store_redemptions 结果里的 id）")]
Period = Literal["total", "day", "week", "month"]
RestockPeriod = Literal["none", "day", "week", "month"]
Audience = Literal["all", "vip", "non_vip", "lifetime"]
Delivery = Literal["self", "gift"]
GiftID = Annotated[int, Field(gt=0, description="赠送记录 ID（store_gifts 结果里的 id）")]


class Reward(BaseModel):
    """一项权益。取值字段由权益类型决定（见 store_catalog）。"""

    type: Literal["vip_plan", "vip_days", "feature", "experience", "balance", "lottery_draws", "manual",
                  "number_change", "number_ticket", "number_draw", "cosmetic", "coupon"] = Field(
        description="vip_plan 会员套餐（ref_id）/ vip_days 会员天数（amount）/ feature 功能解锁（tag + amount 天，0 为永久）/ "
                    "experience 经验（amount）/ balance 余额（money）/ lottery_draws 抽奖次数（amount）/ manual 人工发放（text，只能单独成为一件商品）/ "
                    "number_change 改号卡（amount 张）/ number_ticket 靓号券（amount 张 + ref_id 等级 1 精品 2 稀有 3 传说 4 至尊）/ number_draw 摇号次数（amount 次）/ "
                    "cosmetic 个性装扮（ref_id 装扮编号 + amount 天，0 为永久）/ coupon 优惠券（ref_id 券编号 + amount 张，1–20，见 coupons_templates）。赠送商品（delivery=gift）只能配 vip_plan（限时）、vip_days、feature、cosmetic")
    amount: int | None = Field(default=None, ge=0, description="数量：天数 / 经验点 / 次数")
    money: str | None = Field(default=None, description="余额金额（十进制字符串，如 \"5.00\"）")
    ref_id: int | None = Field(default=None, gt=0, description="会员套餐 ID（vip_plan 用）；number_ticket 时为靓号券等级 1–4；cosmetic 时为装扮编号")
    tag: str | None = Field(default=None, description="功能标识（feature 用）")
    text: str | None = Field(default=None, description="人工发放的内容说明（manual 用，用户可见）")

    def payload(self) -> dict[str, Any]:
        return body(type=self.type, amount=self.amount, money=self.money, refId=self.ref_id, tag=self.tag, text=self.text)


# 工具参数名 → 后端字段名（PointsStoreItemRequest）
_FIELDS = {
    "title": "title", "subtitle": "subtitle", "description": "description", "category": "category", "badge": "badge",
    "cost": "cost", "original_cost": "originalCost", "stock": "stock", "restock_period": "restockPeriod",
    "restock_quantity": "restockQuantity", "global_limit_period": "globalLimitPeriod", "global_limit_count": "globalLimitCount",
    "limit_period": "limitPeriod", "limit_count": "limitCount", "user_total_limit": "userTotalLimit",
    "daily_open_minute": "dailyOpenMinute", "daily_close_minute": "dailyCloseMinute", "audience": "audience",
    "min_level": "minLevel", "starts_at": "startsAt", "ends_at": "endsAt", "status": "status", "sort_order": "sortOrder",
    "delivery": "delivery",
}
_NULLABLE = Literal["original_cost", "stock", "restock_quantity", "daily_open_minute", "daily_close_minute", "starts_at", "ends_at"]


def _payload(fields: dict[str, Any], rewards: list[Reward] | None) -> dict[str, Any]:
    out = {_FIELDS[name]: value for name, value in fields.items() if value is not None}
    if rewards is not None:
        out["rewards"] = [item.payload() for item in rewards]
    return out


async def _find_item(app: str, item_id: int) -> dict[str, Any]:
    items = await ITEMS(appkey=app) or []
    for item in items if isinstance(items, list) else items.get("items") or []:
        if int(item.get("id") or 0) == item_id:
            return item
    raise ToolError(f"找不到 ID 为 {item_id} 的商品，请用 store_items 核对")


# ── 开关与目录 ──

@server.tool(**READ("store"))
async def catalog(app: AppRef) -> Any:
    """积分商城的表单目录：权益类型与取值形态、限兑 / 补货周期、兑换对象、已有分类、应用时区与各项上限。新建商品前先看它。"""
    return await CATALOG(appkey=app)


@server.tool(**READ("store"))
async def settings(app: AppRef) -> Any:
    """积分商城开关（enabled；configured=false 表示从未保存过，按关闭处理）。"""
    return await SETTINGS(appkey=app)


@server.tool(**WRITE("store", idempotent=True))
async def set_enabled(app: AppRef, enabled: Annotated[bool, Field(description="开启或关闭积分商城")]) -> Any:
    """开启 / 关闭积分商城。关闭后用户端入口隐藏、无法兑换（已有兑换记录不受影响），可随时再打开。"""
    return await SETTINGS_SAVE(appkey=app, body={"enabled": enabled})


# ── 商品 ──

@server.tool(**READ("store"))
async def items(app: AppRef) -> Any:
    """商品列表（含下架的），带兑换笔数 / 人数、累计消耗积分、待发放数、此刻库存与名额使用情况。"""
    return await ITEMS(appkey=app)


ItemTitle = Annotated[str, Field(min_length=1, max_length=32, description="商品名称（最多 32 字）")]
Cost = Annotated[int, Field(ge=0, le=1_000_000_000, description="兑换所需积分")]


@server.tool(**WRITE("store"))
async def create_item(
    app: AppRef,
    title: ItemTitle,
    cost: Cost,
    rewards: Annotated[list[Reward], Field(min_length=1, max_length=6, description="权益清单（最多 6 项；manual 只能单独一项）")],
    subtitle: Annotated[str | None, Field(max_length=64, description="副标题")] = None,
    description: Annotated[str | None, Field(max_length=1000, description="详细说明")] = None,
    category: Annotated[str | None, Field(max_length=16, description="分类（用户端按它分组）")] = None,
    badge: Annotated[str | None, Field(max_length=6, description="角标文字（如「热门」）")] = None,
    original_cost: Annotated[int | None, Field(ge=0, description="划线价（高于 cost 才显示）")] = None,
    stock: Annotated[int | None, Field(ge=0, le=100_000_000, description="剩余库存，不传为不限；开了补货时是本周期剩余")] = None,
    restock_period: Annotated[RestockPeriod | None, Field(description="补货周期：none / day / week / month")] = None,
    restock_quantity: Annotated[int | None, Field(ge=0, description="每个补货周期补到多少")] = None,
    global_limit_period: Annotated[Period | None, Field(description="全站名额周期：total / day / week / month")] = None,
    global_limit_count: Annotated[int | None, Field(ge=0, description="全站每周期最多兑出多少份，0 为不限")] = None,
    limit_period: Annotated[Period | None, Field(description="每人限兑周期：total / day / week / month")] = None,
    limit_count: Annotated[int | None, Field(ge=0, le=10_000, description="每人每周期限兑次数，0 为不限")] = None,
    user_total_limit: Annotated[int | None, Field(ge=0, description="每人累计上限，0 为不限（limit_period=total 时无意义）")] = None,
    daily_open_minute: Annotated[int | None, Field(ge=0, le=1440, description="每日开放时刻（自零点起的分钟数，应用默认时区）")] = None,
    daily_close_minute: Annotated[int | None, Field(ge=0, le=1440, description="每日关闭时刻（分钟数，1440 即 24:00；早于开放表示跨零点）")] = None,
    audience: Annotated[Audience | None, Field(description="兑换对象：all / vip 仅会员 / non_vip 仅非会员 / lifetime 仅永久会员")] = None,
    min_level: Annotated[int | None, Field(ge=0, le=1000, description="等级门槛，0 为不限")] = None,
    starts_at: Annotated[str | None, Field(description="上架开始时间（ISO 8601），不传为立即")] = None,
    ends_at: Annotated[str | None, Field(description="下架时间（ISO 8601），不传为长期")] = None,
    status: Annotated[Literal["active", "inactive"] | None, Field(description="active 上架 / inactive 下架")] = None,
    sort_order: Annotated[int | None, Field(description="排序值，越小越靠前")] = None,
    delivery: Annotated[Delivery | None, Field(description="发给谁：self 兑换人自己（默认）/ gift 赠好友（用户兑换时先选人，权益发给对方）")] = None,
) -> Any:
    """新建积分商城商品。权益引用（套餐、功能标识）在保存时校验。status=active 时用户立即可兑，执行前确认价格与权益。"""
    fields = {k: v for k, v in locals().items() if k in _FIELDS}
    return await ITEM_CREATE(appkey=app, body=_payload(fields, rewards))


@server.tool(**WRITE("store"))
async def update_item(
    app: AppRef,
    item_id: ItemID,
    title: Annotated[str | None, Field(min_length=1, max_length=32, description="商品名称")] = None,
    cost: Annotated[int | None, Field(ge=0, le=1_000_000_000, description="兑换所需积分")] = None,
    rewards: Annotated[list[Reward] | None, Field(max_length=6, description="新的权益清单（整体替换），不改留空")] = None,
    subtitle: str | None = None,
    description: str | None = None,
    category: str | None = None,
    badge: str | None = None,
    original_cost: Annotated[int | None, Field(ge=0, description="划线价")] = None,
    stock: Annotated[int | None, Field(ge=0, le=100_000_000, description="剩余库存（本周期）")] = None,
    restock_period: Annotated[RestockPeriod | None, Field(description="补货周期")] = None,
    restock_quantity: Annotated[int | None, Field(ge=0, description="每周期补到多少")] = None,
    global_limit_period: Annotated[Period | None, Field(description="全站名额周期")] = None,
    global_limit_count: Annotated[int | None, Field(ge=0, description="全站每周期名额，0 为不限")] = None,
    limit_period: Annotated[Period | None, Field(description="每人限兑周期")] = None,
    limit_count: Annotated[int | None, Field(ge=0, le=10_000, description="每人每周期限兑次数，0 为不限")] = None,
    user_total_limit: Annotated[int | None, Field(ge=0, description="每人累计上限，0 为不限")] = None,
    daily_open_minute: Annotated[int | None, Field(ge=0, le=1440, description="每日开放时刻（分钟）")] = None,
    daily_close_minute: Annotated[int | None, Field(ge=0, le=1440, description="每日关闭时刻（分钟）")] = None,
    audience: Annotated[Audience | None, Field(description="兑换对象")] = None,
    min_level: Annotated[int | None, Field(ge=0, le=1000, description="等级门槛")] = None,
    starts_at: Annotated[str | None, Field(description="上架开始时间（ISO 8601）")] = None,
    ends_at: Annotated[str | None, Field(description="下架时间（ISO 8601）")] = None,
    status: Annotated[Literal["active", "inactive"] | None, Field(description="active 上架 / inactive 下架")] = None,
    sort_order: Annotated[int | None, Field(description="排序值")] = None,
    delivery: Annotated[Delivery | None, Field(description="发给谁：self / gift（赠好友）")] = None,
    clear: Annotated[list[_NULLABLE] | None, Field(description="要清空的可空字段：stock（改为不限库存）、original_cost、restock_quantity、"
                                                             "daily_open_minute / daily_close_minute（两者都清空为全天开放）、starts_at、ends_at")] = None,
) -> Any:
    """修改商品。只传要改的字段（后端是整体覆盖，这里先取现有商品再合并）。已兑出的记录是快照，不受影响；改价立即对新兑换生效。"""
    current = await _find_item(app, item_id)
    merged: dict[str, Any] = {key: current.get(key) for key in _FIELDS.values() if current.get(key) is not None}
    if current.get("currentStock") is not None:
        merged["stock"] = current["currentStock"]
    merged["rewards"] = current.get("rewards") or []
    fields = {k: v for k, v in locals().items() if k in _FIELDS}
    merged.update(_payload(fields, rewards))
    for name in clear or []:
        merged.pop(_FIELDS[name], None)
    return await ITEM_UPDATE(appkey=app, itemId=item_id, body=merged)


@server.tool(**WRITE("store", idempotent=True))
async def set_item_status(app: AppRef, item_id: ItemID, active: Annotated[bool, Field(description="true 上架 / false 下架")]) -> Any:
    """上架 / 下架一件商品（可随时切回，不影响已有兑换记录）。"""
    return await ITEM_STATUS(appkey=app, itemId=item_id, body={"active": active})


@server.tool(**WRITE("store", idempotent=True))
async def reorder_items(
    app: AppRef,
    item_ids: Annotated[list[Annotated[int, Field(gt=0)]], Field(min_length=1, description="商品 ID，按期望的展示顺序排列")],
) -> Any:
    """按给定顺序重排商品（排序值写成 10、20、30……，只改排序不碰库存）。"""
    return await ITEM_ORDER(appkey=app, body={"itemIds": item_ids})


@server.tool(**DESTRUCTIVE("store"))
async def delete_item(app: AppRef, item_id: ItemID) -> Any:
    """删除商品（不可恢复；兑换记录保留快照，商品关联置空）。只想暂停兑换请用 store_set_item_status 下架。先向用户确认。"""
    return await ITEM_DELETE(appkey=app, itemId=item_id)


# ── 兑换记录 ──

@server.tool(**READ("store"))
async def redemptions(
    app: AppRef,
    user_id: Annotated[int | None, Field(gt=0, description="只看某个用户（管理端用户 ID）")] = None,
    item_id: Annotated[int | None, Field(gt=0, description="只看某件商品")] = None,
    status: Annotated[Literal["", "completed", "pending", "cancelled", "revoked"], Field(description="completed 已到账 / pending 待人工发放（或赠送审核中）/ cancelled 已取消 / revoked 赠送已撤销且未退积分；空为不限")] = "",
    keyword: Annotated[str, Field(description="按用户账号 / 商品标题等搜索")] = "",
    from_time: IsoTime = "",
    to_time: IsoTime = "",
    page: Page = 1,
    limit: Annotated[int, Field(ge=1, le=100, description="每页条数")] = 20,
) -> Any:
    """兑换记录（快照：当时的标题、积分、权益与发放结果），可按用户、商品、状态与下单时间（RFC 3339，左闭右开）筛选。"""
    return await REDEMPTIONS(appkey=app, query=q(
        userId=user_id, itemId=item_id, status=status, keyword=keyword, **{"from": from_time, "to": to_time}, page=page, limit=limit,
    ))


@server.tool(**WRITE("store"))
async def fulfill(
    app: AppRef,
    redemption_id: RedemptionID,
    note: Annotated[str, Field(max_length=500, description="给用户的话（兑换码、快递单号等），随站内信发给用户")] = "",
) -> Any:
    """把一笔待人工发放（pending）的兑换标记为已发放，并站内信通知用户。处理后不能再改回或取消，先确认已经实际发出。
    赠送的兑换（带 giftNo）不能在这里处理（40969），用 store_approve_gift / store_reject_gift。"""
    return await FULFILL(appkey=app, redemptionId=redemption_id, body={"note": note})


@server.tool(**DESTRUCTIVE("store"))
async def cancel(
    app: AppRef,
    redemption_id: RedemptionID,
    note: Annotated[str, Field(max_length=500, description="取消原因（随站内信发给用户）")] = "",
) -> Any:
    """取消一笔待人工发放（pending）的兑换：退回积分、本周期库存加回、站内信通知用户。不可撤销；已到账的兑换不能取消。先向用户确认。"""
    return await CANCEL(appkey=app, redemptionId=redemption_id, body={"note": note})


@server.tool(**READ("store"))
async def stats(
    app: AppRef,
    days: Annotated[int, Field(ge=1, le=90, description="按天趋势的天数")] = 14,
    tz: Annotated[str, Field(description="切日时区（IANA，如 Asia/Shanghai），留空用服务器默认")] = "",
) -> Any:
    """积分商城概览与按天趋势：兑换笔数、人数、消耗积分、待发放、上架商品数、今日与取消笔数、热门商品。"""
    return await STATS(appkey=app, query=q(days=days, tz=tz))


# ── 赠好友 ──

GiftStatus = Literal["", "reviewing", "delivered", "rejected", "revoked", "blocked"]


@server.tool(**READ("store"))
async def gifts(
    app: AppRef,
    status: Annotated[GiftStatus, Field(description="reviewing 审核中（即审核队列）/ delivered 已送达 / rejected 已驳回 / "
                                                    "revoked 已撤销 / blocked 风控拦截（未扣积分）；空为不限")] = "",
    user_id: Annotated[int | None, Field(gt=0, description="作为赠送人或接收人（管理端用户 ID）")] = None,
    sender_id: Annotated[int | None, Field(gt=0, description="只看这个赠送人")] = None,
    recipient_id: Annotated[int | None, Field(gt=0, description="只看这个接收人")] = None,
    item_id: Annotated[int | None, Field(gt=0, description="只看某件商品")] = None,
    keyword: Annotated[str, Field(description="赠送单号、兑换单号、商品标题、双方账号 / 昵称 / 号码")] = "",
    from_time: IsoTime = "",
    to_time: IsoTime = "",
    page: Page = 1,
    limit: Annotated[int, Field(ge=1, le=100, description="每页条数")] = 20,
) -> Any:
    """赠送记录与各状态笔数（summary）。每条带双方、发了什么、风控结论（riskAction / riskHits / riskFacts）与审核撤销记录。"""
    return await GIFTS(appkey=app, query=q(
        status=status, userId=user_id, senderId=sender_id, recipientId=recipient_id, itemId=item_id, keyword=keyword,
        **{"from": from_time, "to": to_time}, page=page, limit=limit,
    ))


@server.tool(**READ("store"))
async def gift(app: AppRef, gift_id: GiftID) -> Any:
    """一笔赠送的详情：双方、权益与发放结果、命中的风控判据与当时的事实（注册天数、共用设备与地址、当日次数）、审核与追回记录。"""
    return await GIFT(appkey=app, giftId=gift_id)


GiftNote = Annotated[str, Field(max_length=500, description="备注（随站内信发给用户）")]


@server.tool(**WRITE("store"))
async def approve_gift(app: AppRef, gift_id: GiftID, note: GiftNote = "") -> Any:
    """审核通过一笔 reviewing 的赠送：在一个事务里把权益发给接收人并通知双方。发放前复核对方状态与是否已拥有，不满足时报错，应改为驳回。"""
    return await GIFT_APPROVE(appkey=app, giftId=gift_id, body={"note": note})


@server.tool(**DESTRUCTIVE("store"))
async def reject_gift(app: AppRef, gift_id: GiftID, note: GiftNote = "") -> Any:
    """驳回一笔 reviewing 的赠送：积分原路退回赠送人、库存加回、通知赠送人。不可撤销，先向用户确认。"""
    return await GIFT_REJECT(appkey=app, giftId=gift_id, body={"note": note})


@server.tool(**DESTRUCTIVE("store"))
async def revoke_gift(
    app: AppRef,
    gift_id: GiftID,
    refund_sender: Annotated[bool, Field(description="是否把积分退给赠送人：误送、客诉退；套利、欺诈不退")] = False,
    note: GiftNote = "",
) -> Any:
    """撤销一笔 delivered 的赠送并从接收人处追回：会员扣掉还没走完的时长、功能解锁与装扮收回；通知双方。不可撤销，先向用户确认。"""
    return await GIFT_REVOKE(appkey=app, giftId=gift_id, body={"note": note, "refundSender": refund_sender})


@server.tool(**READ("store"))
async def gift_policy(app: AppRef) -> Any:
    """赠送风控策略（policy）、判据目录（catalog：每条判据的含义、阈值范围、可选处置）与推荐默认值（defaults）。改之前先看它。"""
    return await GIFT_POLICY(appkey=app)


class GiftRule(BaseModel):
    """一条赠送风控判据（键与含义见 store_gift_policy 的 catalog）。"""

    key: Literal["sender_age", "recipient_age", "same_device", "same_network", "sender_daily_count", "sender_daily_points",
                 "recipient_daily_count", "pair_frequency", "mutual", "large_gift", "risk_platform"]
    enabled: bool
    action: Literal["review", "reject"] = Field(description="review 送审核（积分先扣）/ reject 直接拒绝（不扣积分）")
    threshold: int | None = Field(default=None, ge=0, description="阈值：天数 / 次数 / 积分（没有阈值的判据不传）")
    window_hours: int | None = Field(default=None, ge=1, description="时间窗（小时，只有 same_network / pair_frequency / mutual 有）")


@server.tool(**WRITE("store", idempotent=True))
async def save_gift_policy(
    app: AppRef,
    enabled: Annotated[bool, Field(description="是否允许赠送（关掉后赠送商品照列但不能提交）")],
    rules: Annotated[list[GiftRule] | None, Field(description="要配置的判据；没给的判据按推荐默认值")] = None,
) -> Any:
    """保存赠送风控策略，立即对新的赠送生效。放宽判据（关掉、改为审核、调高上限）前先向用户确认。"""
    payload = [body(key=r.key, enabled=r.enabled, action=r.action, threshold=r.threshold, windowHours=r.window_hours) for r in rules or []]
    return await GIFT_POLICY_SAVE(appkey=app, body={"enabled": enabled, "rules": payload})
