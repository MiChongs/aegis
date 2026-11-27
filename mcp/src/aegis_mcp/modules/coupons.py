"""优惠券与优惠活动：目录、概览、券（模板）的增改与上下线、发券与收回、用户券、活动（限时折扣 / 满减 / 支付返券返积分）、订单优惠记录与试算。

契约见 docs/coupons.md。券与活动在支付下单时自动结算：活动自动生效，券由用户选；付款后核销，订单过期释放，全额退款时按券的设置返还。
权限：读与试算需要 app:coupon:read；新建、修改、改状态需要 app:coupon:write；删除需要 app:coupon:delete；发券与收回需要 app:coupon:issue。
"""

from __future__ import annotations

from typing import Annotated, Any, Literal

from fastmcp import FastMCP
from fastmcp.exceptions import ToolError
from pydantic import Field

from ..core import DELETE, DESTRUCTIVE, GET, POST, PUT, READ, WRITE, AppRef, Limit, Page, UserID, body, q

NAMESPACE = "coupons"
server = FastMCP("coupons")

B = "/api/admin/apps/:appkey/coupons"
CATALOG = GET(f"{B}/catalog")
OVERVIEW = GET(f"{B}/overview")
TEMPLATES = GET(f"{B}/templates")
TEMPLATE_CREATE = POST(f"{B}/templates")
TEMPLATE = GET(f"{B}/templates/:templateId")
TEMPLATE_UPDATE = PUT(f"{B}/templates/:templateId")
TEMPLATE_STATUS = PUT(f"{B}/templates/:templateId/status")
TEMPLATE_DELETE = DELETE(f"{B}/templates/:templateId")
ISSUE = POST(f"{B}/templates/:templateId/issue")
USER_COUPONS = GET(f"{B}/user-coupons")
REVOKE = POST(f"{B}/user-coupons/:couponId/revoke")
PROMOTIONS = GET(f"{B}/promotions")
PROMOTION_CREATE = POST(f"{B}/promotions")
PROMOTION = GET(f"{B}/promotions/:promotionId")
PROMOTION_UPDATE = PUT(f"{B}/promotions/:promotionId")
PROMOTION_STATUS = PUT(f"{B}/promotions/:promotionId/status")
PROMOTION_DELETE = DELETE(f"{B}/promotions/:promotionId")
ORDERS = GET(f"{B}/orders")
QUOTE = POST(f"{B}/quote")

TemplateID = Annotated[int, Field(gt=0, description="券编号（coupons_templates 结果里的 id）")]
PromotionID = Annotated[int, Field(gt=0, description="活动编号（coupons_promotions 结果里的 id）")]
Status = Literal["draft", "active", "paused", "ended"]
StatusFilter = Annotated[Literal["", "draft", "active", "paused", "ended"], Field(description="状态筛选，留空为全部")]
Keyword = Annotated[str, Field(description="关键词，留空为全部")]

TEMPLATE_FIELDS = (
    "券的完整配置（camelCase，与控制台表单同形）：name 名称、description、theme（red/orange/gold/green/blue/purple）、"
    "kind（amount_off 立减：amount 元；percent_off 折扣：rate 付款比例 1–99，85 即 8.5 折，maxDiscount 封顶，0 不封顶）、"
    "minAmount 使用门槛（0 无门槛）、scopes 适用订单（vip_purchase 买会员 / integral_purchase 积分充值 / goods 普通商品；余额充值不能用券）、"
    "planIds 只限这些会员套餐（空为全部）、stackWithPromotion 能否与活动同享、"
    "validityMode（fixed：validFrom 可空 + validUntil；relative：领取后 validDelayDays 天生效、再过 validDays 天失效）、"
    "totalQuantity 发放总量（0 不限）、perUserLimit 领券中心每人限领（0 不限）、claimable 出现在领券中心、claimStartsAt / claimEndsAt、"
    "audience {kind: all/new_user/first_order/non_vip/vip, newUserDays}、refundReturn 全额退款后返还。"
    "已发过券的模板不能再改方式、面额、门槛、范围、叠加与有效期（42322）。"
)
PROMOTION_FIELDS = (
    "活动的完整配置（camelCase）：name、description、tag 角标（空则按规则生成）、theme、priority（同等优惠时高者优先）、"
    "kind 与 rule：discount 限时折扣 rule={discountMode: percent（rate 付款比例、maxDiscount）/ amount_off（amount）/ fixed_price（amount，只能用于指定会员套餐）}；"
    "threshold 满减 rule={tiers:[{minAmount, off}]}（1–5 档，门槛越高减得越多）；"
    "rebate 支付返券返积分 rule={minAmount, rebateCoupons:[{templateId, count}], rebateIntegral}（不改价格）。"
    "scopes（vip_purchase / integral_purchase / goods；rebate 另可 wallet_recharge）、planIds、packageIds（充值档位，rebate 用）、"
    "audience、stackWithCoupon 能否与券同享、startsAt / endsAt、perUserLimit 每人参与几次、totalLimit 总次数、budget 优惠总额上限（0 不限）。"
)
ConfigDict = Annotated[dict[str, Any], Field(description="见工具说明")]


@server.tool(**READ("coupons"))
async def catalog(app: AppRef) -> Any:
    """优惠目录：券的优惠方式、适用范围、活动类型与折扣方式、人群、色调、状态与上限。建券、建活动前先看它。"""
    return await CATALOG(appkey=app)


@server.tool(**READ("coupons"))
async def overview(
    app: AppRef,
    tz: Annotated[str, Field(description="分日时区（IANA，如 Asia/Shanghai）")] = "Asia/Shanghai",
    days: Annotated[int, Field(ge=1, le=90, description="趋势天数")] = 14,
) -> Any:
    """优惠概览：券与活动数、发出 / 已用 / 可用 / 过期张数、用了优惠的订单数与实付、券与活动各优惠了多少、返出的券与积分、逐日趋势、券与活动排行、发券来源。"""
    return await OVERVIEW(appkey=app, query=q(tz=tz, days=days))


# ── 券（模板）──

@server.tool(**READ("coupons"))
async def templates(app: AppRef, status: StatusFilter = "", keyword: Keyword = "", page: Page = 1, limit: Limit = 20) -> Any:
    """券列表（进行中在前）：方式、面额、门槛、范围、有效期、库存与已发 / 已用张数、累计优惠金额。"""
    return await TEMPLATES(appkey=app, query=q(status=status, keyword=keyword, page=page, limit=limit))


@server.tool(**READ("coupons"))
async def template(app: AppRef, template_id: TemplateID) -> Any:
    """一种券的完整配置与统计。"""
    return await TEMPLATE(appkey=app, templateId=template_id)


@server.tool(description="新建一种券（草稿，coupons_set_template_status 上线后才能领、能发）。" + TEMPLATE_FIELDS, **WRITE("coupons"))
async def create_template(app: AppRef, config: ConfigDict) -> Any:
    return await TEMPLATE_CREATE(appkey=app, body=config)


@server.tool(**WRITE("coupons", idempotent=True))
async def update_template(app: AppRef, template_id: TemplateID, changes: ConfigDict) -> Any:
    """修改一种券：只传要改的字段，工具先读当前配置、合并后整份保存。"""
    current = await TEMPLATE(appkey=app, templateId=template_id)
    if not isinstance(current, dict):
        raise ToolError("读取当前配置失败")
    keys = ("name", "description", "theme", "kind", "amount", "rate", "maxDiscount", "minAmount", "scopes", "planIds",
            "stackWithPromotion", "validityMode", "validFrom", "validUntil", "validDays", "validDelayDays", "totalQuantity",
            "perUserLimit", "claimable", "claimStartsAt", "claimEndsAt", "audience", "refundReturn")
    merged = {key: current.get(key) for key in keys if key in current}
    merged.update(changes)
    return await TEMPLATE_UPDATE(appkey=app, templateId=template_id, body=merged)


@server.tool(**WRITE("coupons", idempotent=True))
async def set_template_status(app: AppRef, template_id: TemplateID, status: Status) -> Any:
    """上线（active）、暂停（paused：不能再领、再发，已发的照常能用）或结束（ended：终态，不能再上线）一种券。"""
    return await TEMPLATE_STATUS(appkey=app, templateId=template_id, body=body(status=status))


@server.tool(**DESTRUCTIVE("coupons"))
async def delete_template(app: AppRef, template_id: TemplateID) -> Any:
    """删除一种券：只能删一张都没发过的（否则 42324，请改为结束）。执行前向用户确认。"""
    return await TEMPLATE_DELETE(appkey=app, templateId=template_id)


@server.tool(**WRITE("coupons"))
async def issue(
    app: AppRef,
    template_id: TemplateID,
    user_ids: Annotated[list[Annotated[int, Field(gt=0)]], Field(min_length=1, max_length=500, description="管理端用户 ID，一次最多 500 人")],
    count: Annotated[int, Field(ge=1, le=20, description="每人发几张")] = 1,
    notify: Annotated[bool, Field(description="发站内信告诉用户")] = False,
) -> Any:
    """给一批用户发券（暂停中的券也能发；每人一个事务，结果逐人列出）。会占用库存，执行前向用户复述人数与张数。"""
    return await ISSUE(appkey=app, templateId=template_id, body=body(userIds=user_ids, count=count, notify=notify))


@server.tool(**READ("coupons"))
async def user_coupons(
    app: AppRef,
    template_id: Annotated[int | None, Field(gt=0, description="只看这种券")] = None,
    user_id: Annotated[int | None, Field(gt=0, description="只看这个用户（管理端用户 ID）")] = None,
    status: Annotated[Literal["", "usable", "available", "not_started", "locked", "used", "expired", "revoked"], Field(description="状态，留空为全部")] = "",
    source: Annotated[str, Field(description="来源：claim / admin / rebate / card_key / points_store / lottery / item_grant / invite / leaderboard，留空为全部")] = "",
    keyword: Annotated[str, Field(description="券号、订单号、券名或用户账号")] = "",
    page: Page = 1,
    limit: Limit = 20,
) -> Any:
    """用户手里的券（新的在前）：券号、状态、来源、有效期、占用 / 使用的订单与抵扣金额、用户。"""
    return await USER_COUPONS(appkey=app, query=q(templateId=template_id, userId=user_id, status=status, source=source,
                                                  keyword=keyword, page=page, limit=limit))


@server.tool(**DESTRUCTIVE("coupons"))
async def revoke(
    app: AppRef,
    coupon_id: Annotated[int, Field(gt=0, description="用户券编号（coupons_user_coupons 结果里的 id）")],
    reason: Annotated[str, Field(max_length=200, description="收回原因")] = "",
) -> Any:
    """收回一张券：只能收回还没用、也没被待支付订单占用的。执行前向用户确认。"""
    return await REVOKE(appkey=app, couponId=coupon_id, body=body(reason=reason))


# ── 活动 ──

@server.tool(**READ("coupons"))
async def promotions(
    app: AppRef,
    kind: Annotated[Literal["", "discount", "threshold", "rebate"], Field(description="类型：discount 限时折扣 / threshold 满减 / rebate 支付返券，留空为全部")] = "",
    status: StatusFilter = "",
    keyword: Keyword = "",
    page: Page = 1,
    limit: Limit = 20,
) -> Any:
    """活动列表：规则、范围、时段、人群、参与次数与累计优惠。"""
    return await PROMOTIONS(appkey=app, query=q(kind=kind, status=status, keyword=keyword, page=page, limit=limit))


@server.tool(**READ("coupons"))
async def promotion(app: AppRef, promotion_id: PromotionID) -> Any:
    """一期活动的完整配置与统计。"""
    return await PROMOTION(appkey=app, promotionId=promotion_id)


@server.tool(description="新建一期活动（草稿，coupons_set_promotion_status 上线后对下单生效）。" + PROMOTION_FIELDS, **WRITE("coupons"))
async def create_promotion(app: AppRef, config: ConfigDict) -> Any:
    return await PROMOTION_CREATE(appkey=app, body=config)


@server.tool(**WRITE("coupons", idempotent=True))
async def update_promotion(app: AppRef, promotion_id: PromotionID, changes: ConfigDict) -> Any:
    """修改一期活动：只传要改的字段，工具先读当前配置、合并后整份保存。改规则只影响之后的订单。"""
    current = await PROMOTION(appkey=app, promotionId=promotion_id)
    if not isinstance(current, dict):
        raise ToolError("读取当前配置失败")
    keys = ("name", "description", "tag", "theme", "kind", "rule", "priority", "scopes", "planIds", "packageIds", "audience",
            "stackWithCoupon", "startsAt", "endsAt", "perUserLimit", "totalLimit", "budget")
    merged = {key: current.get(key) for key in keys if key in current}
    merged.update(changes)
    return await PROMOTION_UPDATE(appkey=app, promotionId=promotion_id, body=merged)


@server.tool(**WRITE("coupons", idempotent=True))
async def set_promotion_status(app: AppRef, promotion_id: PromotionID, status: Status) -> Any:
    """上线、暂停或结束一期活动（结束是终态）。上线即对所有之后的下单生效，执行前向用户复述。"""
    return await PROMOTION_STATUS(appkey=app, promotionId=promotion_id, body=body(status=status))


@server.tool(**DESTRUCTIVE("coupons"))
async def delete_promotion(app: AppRef, promotion_id: PromotionID) -> Any:
    """删除一期活动：只能删没有任何订单参与过的（否则 42324，请改为结束）。执行前向用户确认。"""
    return await PROMOTION_DELETE(appkey=app, promotionId=promotion_id)


# ── 订单与试算 ──

@server.tool(**READ("coupons"))
async def orders(
    app: AppRef,
    promotion_id: Annotated[int | None, Field(gt=0, description="只看参与了这期活动（含返券活动）的")] = None,
    template_id: Annotated[int | None, Field(gt=0, description="只看用了这种券的")] = None,
    user_id: Annotated[int | None, Field(gt=0, description="只看这个用户")] = None,
    status: Annotated[Literal["", "reserved", "settled", "released", "refunded"], Field(description="reserved 待付款 / settled 已结算 / released 未付款已释放 / refunded 已全额退款")] = "",
    keyword: Annotated[str, Field(description="订单号、商品名或用户账号")] = "",
    page: Page = 1,
    limit: Limit = 20,
) -> Any:
    """订单优惠记录（新的在前）：原价、活动优惠、券优惠、应付、用的活动与券、返券状态与说明、订单状态。"""
    return await ORDERS(appkey=app, query=q(promotionId=promotion_id, templateId=template_id, userId=user_id, status=status,
                                            keyword=keyword, page=page, limit=limit))


@server.tool(**READ("coupons"))
async def quote(
    app: AppRef,
    user_id: UserID,
    purpose: Annotated[Literal["vip_purchase", "integral_purchase", "wallet_recharge", ""], Field(description="订单用途；空为普通商品")] = "vip_purchase",
    plan_id: Annotated[int | None, Field(gt=0, description="会员套餐 ID（vip_purchase）")] = None,
    package_id: Annotated[int | None, Field(gt=0, description="充值档位 ID（wallet_recharge）")] = None,
    amount: Annotated[str, Field(description="原价（十进制字符串）；会员与按档位充值可留空，由服务端定价")] = "",
    coupon_id: Annotated[int | None, Field(gt=0, description="指定用这张券（用户券编号）")] = None,
    coupon_mode: Annotated[Literal["auto", "none"], Field(description="auto 自动选最优的券 / none 不用券（指定了 coupon_id 时忽略）")] = "auto",
) -> Any:
    """替一个用户试算一笔订单（只算不写）：命中的活动、他每张券能不能用与原因、最后要付多少、返券预告。排查「为什么没打折」时用它。"""
    return await QUOTE(appkey=app, body=body(userId=user_id, purpose=purpose, planId=plan_id, packageId=package_id,
                                             amount=amount, couponId=coupon_id, couponMode=coupon_mode))
