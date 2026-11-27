"""会员：套餐、会员档位、功能标识目录、发放与预览、收回、权益判定、会员流水、试用领取、折算策略与折算记录。

权限：读接口需要 app:read，其余（含发放预览）需要 app:write。
"""

from __future__ import annotations

from typing import Annotated, Any, Literal

from fastmcp import FastMCP
from fastmcp.exceptions import ToolError
from pydantic import Field

from ..core import DELETE, DESTRUCTIVE, GET, POST, PUT, READ, WRITE, AppRef, Limit, Page, UserID, body, q

NAMESPACE = "vip"
server = FastMCP("vip")

PLANS = GET("/api/admin/apps/:appkey/vip/plans")
PLAN_SAVE = POST("/api/admin/apps/:appkey/vip/plans")
PLAN_DELETE = DELETE("/api/admin/apps/:appkey/vip/plans/:planId")
TIERS = GET("/api/admin/apps/:appkey/vip/tiers")
TIER_SAVE = POST("/api/admin/apps/:appkey/vip/tiers")
TIER_DELETE = DELETE("/api/admin/apps/:appkey/vip/tiers/:tierId")
FEATURES = GET("/api/admin/apps/:appkey/vip/features")
FEATURE_SAVE = POST("/api/admin/apps/:appkey/vip/features")
FEATURE_DELETE = DELETE("/api/admin/apps/:appkey/vip/features/:tag")
GRANT = POST("/api/admin/apps/:appkey/vip/grant")
GRANT_PREVIEW = POST("/api/admin/apps/:appkey/vip/grant/preview")
REVOKE = POST("/api/admin/apps/:appkey/vip/revoke")
ENTITLEMENT = GET("/api/admin/apps/:appkey/vip/entitlement")
TRANSACTIONS = GET("/api/admin/apps/:appkey/vip/transactions")
TRIAL_CLAIMS = GET("/api/admin/apps/:appkey/vip/trial/claims")
TRIAL_CLAIM = POST("/api/admin/apps/:appkey/vip/trial/claims")
TRIAL_RESET = DELETE("/api/admin/apps/:appkey/vip/trial/claims/:userId")
CONVERSION_POLICY = GET("/api/admin/apps/:appkey/vip/conversion-policy")
CONVERSION_POLICY_SAVE = PUT("/api/admin/apps/:appkey/vip/conversion-policy")
CONVERSIONS = GET("/api/admin/apps/:appkey/vip/conversions")

FEATURE_TAG = r"^[a-z][a-z0-9._-]{1,63}$"

PlanID = Annotated[int, Field(gt=0, description="套餐 ID（vip_plans 结果里的 id）")]
TierID = Annotated[int, Field(gt=0, description="会员档位 ID（vip_tiers 结果里的 id）")]
FeatureTags = Annotated[
    list[str] | None,
    Field(description="功能标识列表（须已在功能目录登记，见 vip_features）；不改留空，传空数组即清空"),
]
Money = Annotated[str | None, Field(pattern=r"^\d+(\.\d{1,2})?$", description="金额（元，十进制字符串，如 \"9.90\"）")]
OptionalUser = Annotated[int | None, Field(gt=0, description="只看这个用户（管理端用户 ID），留空为全部")]
OwnedPolicy = Annotated[
    Literal["", "convert", "grant"],
    Field(description="这笔对他已是空的（永久会员已包含 / 已永久解锁）时：空为按应用折算策略，convert 折成积分，grant 照常发放"),
]
UpgradePolicy = Annotated[
    Literal["", "convert", "keep"],
    Field(description="发永久会员时被覆盖的存量（限时会员、限时功能等）：空为按应用折算策略，convert 折算为积分并结束存量，keep 保留"),
]


# ── 套餐 ──


@server.tool(**READ("vip"))
async def plans(
    app: AppRef,
    plan_id: Annotated[int | None, Field(gt=0, description="只取这一个套餐的详情，留空为全部")] = None,
    active_only: Annotated[bool, Field(description="只列上架中的套餐")] = False,
) -> Any:
    """会员套餐列表（含下架的）：名称、类型（paid / trial）、是否永久、时长、价格、赠送积分、功能标识、上架状态。给 plan_id 返回单个套餐。"""
    items = await PLANS(appkey=app)
    items = items if isinstance(items, list) else []
    if plan_id is not None:
        for item in items:
            if int(item.get("id") or 0) == plan_id:
                return item
        raise ToolError(f"找不到 ID 为 {plan_id} 的套餐，请先用 vip_plans 核对")
    if active_only:
        items = [item for item in items if item.get("isActive")]
    return items


@server.tool(**WRITE("vip"))
async def save_plan(
    app: AppRef,
    plan_id: Annotated[int, Field(ge=0, description="要修改的套餐 ID；0 为新建")] = 0,
    name: Annotated[str | None, Field(description="套餐名称（新建必填）")] = None,
    kind: Annotated[Literal["paid", "trial"] | None, Field(description="paid 付费 / trial 试用（试用价格须为 0，每个应用最多一个上架的试用套餐）；不传沿用原值，新建默认 paid")] = None,
    lifetime: Annotated[bool | None, Field(description="永久套餐（不带时长）。只能在新建时指定，之后不能切换")] = None,
    trial_device_limited: Annotated[bool | None, Field(description="仅试用套餐：同一设备只能领一次")] = None,
    tier_id: Annotated[int | None, Field(gt=0, description="套餐卖的会员档位（vip_tiers）。新建时不传：功能完全相同的档位就并进去，否则按套餐名新建一个")] = None,
    features: Annotated[
        list[str] | None,
        Field(description="改的是所属档位的功能（同档位的套餐与全部在期会员一起变）；须已登记，不改留空"),
    ] = None,
    duration_days: Annotated[int | None, Field(gt=0, description="时长（天）；非永久套餐新建必填")] = None,
    price: Money = None,
    original_price: Money = None,
    bonus_integral: Annotated[int | None, Field(ge=0, description="购买时赠送的积分")] = None,
    description: Annotated[str | None, Field(description="套餐说明")] = None,
    is_active: Annotated[bool | None, Field(description="是否上架")] = None,
    sort_order: Annotated[int | None, Field(description="排序，小的在前")] = None,
) -> Any:
    """新建或修改会员套餐。plan_id 为 0 时新建（需 name、price，非永久还需 duration_days）；修改时只传要改的字段。

    权益属于会员档位，套餐只是档位的售卖规格：改价格、时长只影响之后的购买；改 tier_id 只影响之后的售卖；
    传 features 改的是档位的功能，对该档位全部在期会员立即生效。重新上架删掉的套餐时，用 tier_id 选回原档位。
    """
    if plan_id == 0:
        if not (name or "").strip():
            raise ToolError("新建套餐需要 name")
        if price is None:
            raise ToolError("新建套餐需要 price（0 元也要显式填 \"0\"）")
        if not lifetime and duration_days is None:
            raise ToolError("新建非永久套餐需要 duration_days")
    return await PLAN_SAVE(appkey=app, body=body(
        id=plan_id, name=name, kind=kind, lifetime=lifetime, trialDeviceLimited=trial_device_limited,
        tierId=tier_id, features=features, durationDays=duration_days, price=price, originalPrice=original_price,
        bonusIntegral=bonus_integral, description=description, isActive=is_active, sortOrder=sort_order,
    ))


@server.tool(**WRITE("vip", idempotent=True))
async def set_plan_active(
    app: AppRef,
    plan_id: PlanID,
    active: Annotated[bool, Field(description="true 上架 / false 下架")],
) -> Any:
    """上架或下架一个套餐。下架后用户端不再展示、不能购买，已开通的会员不受影响；可随时重新上架。"""
    return await PLAN_SAVE(appkey=app, body={"id": plan_id, "isActive": active})


@server.tool(**DESTRUCTIVE("vip"))
async def delete_plan(app: AppRef, plan_id: PlanID) -> Any:
    """删除会员套餐（不可恢复）。已开通的会员仍在原档位、权益不变。只想停售请用 vip_set_plan_active 下架。执行前先向用户确认套餐名称与 ID。"""
    return await PLAN_DELETE(appkey=app, planId=plan_id)


# ── 会员档位 ──


@server.tool(**READ("vip"))
async def tiers(app: AppRef) -> Any:
    """会员档位：权益的唯一来源。每个档位的功能、卖它的套餐、在期会员数（含永久）、是否永久会员默认档位。

    每段会员期记的是档位，权益按档位当前的功能算；删套餐、重新上架、调价都不影响已开通的会员。
    """
    return await TIERS(appkey=app)


@server.tool(**WRITE("vip"))
async def save_tier(
    app: AppRef,
    tier_id: Annotated[int, Field(ge=0, description="要修改的档位 ID；0 为新建")] = 0,
    name: Annotated[str | None, Field(description="档位名称（新建必填）")] = None,
    description: Annotated[str | None, Field(description="说明")] = None,
    features: FeatureTags = None,
    perks: Annotated[
        list[dict[str, str]] | None,
        Field(description="补充权益标签（只用于会员页展示，与功能标识无关），每项 {label, detail?}：最多 12 个、label ≤ 16 字、detail ≤ 60 字；不改留空，传空数组即清空。AI 优惠标签由计费设置自动生成，不用手填"),
    ] = None,
    sort_order: Annotated[int | None, Field(description="排序，小的在前")] = None,
) -> Any:
    """新建或修改会员档位。改 features 即对档位下全部在期会员立即生效（卖这个档位的套餐一起变），修改前先向用户说明影响人数（vip_tiers 的 members）。perks 只是展示用的补充权益标签。"""
    if tier_id == 0 and not (name or "").strip():
        raise ToolError("新建档位需要 name")
    return await TIER_SAVE(appkey=app, body=body(id=tier_id, name=name, description=description, features=features, perks=perks, sortOrder=sort_order))


@server.tool(**DESTRUCTIVE("vip"))
async def delete_tier(
    app: AppRef,
    tier_id: TierID,
    move_to: Annotated[int | None, Field(gt=0, description="迁往的档位 ID：档位下还有套餐或在期会员时必填，套餐与全部开通记录改挂它")] = None,
) -> Any:
    """删除会员档位（不可恢复）。档位下还有套餐或在期会员时必须给 move_to，否则报 40996；迁移后这些会员按目标档位的功能算。执行前向用户确认档位、迁往目标与影响人数。"""
    return await TIER_DELETE(appkey=app, tierId=tier_id, query=q(moveTo=move_to))


# ── 功能标识目录 ──


@server.tool(**READ("vip"))
async def features(app: AppRef) -> Any:
    """会员功能标识目录：每个标识的 tag、名称、说明、启用状态与排序。套餐、功能解锁、抽奖与激励广告引用的功能都要先在这里登记。"""
    return await FEATURES(appkey=app)


@server.tool(**WRITE("vip"))
async def save_feature(
    app: AppRef,
    tag: Annotated[str, Field(pattern=FEATURE_TAG, description="功能标识：小写字母开头，可含数字、点、下划线、连字符，2–64 位。已存在即修改，否则新建；创建后不可改")],
    name: Annotated[str | None, Field(description="显示名称（新建时必填，修改时可省略）")] = None,
    description: Annotated[str | None, Field(description="说明")] = None,
    is_active: Annotated[bool | None, Field(description="是否启用")] = None,
    sort_order: Annotated[int | None, Field(description="排序，小的在前")] = None,
) -> Any:
    """新建或修改一个会员功能标识（按 tag 定位）。只传要改的字段。"""
    return await FEATURE_SAVE(appkey=app, body=body(tag=tag, name=name, description=description, isActive=is_active, sortOrder=sort_order))


@server.tool(**DESTRUCTIVE("vip"))
async def delete_feature(
    app: AppRef,
    tag: Annotated[str, Field(min_length=1, description="要删除的功能标识")],
) -> Any:
    """删除功能标识（不可恢复）。引用它的套餐会随之失去这一项权益，返回的 affectedPlans 是受影响的套餐数。执行前先向用户确认；只想停用请用 vip_save_feature 设 is_active=false。"""
    return await FEATURE_DELETE(appkey=app, tag=tag)


# ── 发放与收回 ──


def _grant_body(
    user_id: int, plan_id: int | None, quantity: int | None, days: int | None, lifetime: bool,
    features: list[str] | None, reason: str, bonus_integral: int | None, owned_policy: str, upgrade_policy: str,
    tier_id: int | None = None,
) -> dict[str, Any]:
    if not plan_id and not lifetime and not days:
        raise ToolError("需要 plan_id（按套餐发放）、days（自定义天数）或 lifetime=true（永久会员）之一")
    if plan_id and (days or lifetime or features or tier_id):
        raise ToolError("按套餐发放时时长与权益取自套餐，不要同时给 days / lifetime / features / tier_id")
    return body(
        userId=user_id, planId=plan_id, quantity=quantity, days=days, lifetime=lifetime or None,
        tierId=tier_id, features=features, reason=reason, bonusIntegral=bonus_integral,
        ownedPolicy=owned_policy or None, upgradePolicy=upgrade_policy or None,
    )


GrantPlan = Annotated[int | None, Field(gt=0, description="按套餐发放：套餐 ID（试用套餐请用 vip_claim_trial）")]
GrantQuantity = Annotated[int | None, Field(ge=1, le=100, description="按套餐发放的份数（时长 × 份数），默认 1；永久套餐只能 1 份")]
GrantDays = Annotated[int | None, Field(gt=0, description="自定义发放：会员天数")]
GrantLifetime = Annotated[bool, Field(description="自定义发放：发永久会员（days 忽略）")]
GrantFeatures = Annotated[list[str] | None, Field(description="自定义发放时附带的功能标识（须已登记）；给了 tier_id 时是档位之外另给的")]
GrantTier = Annotated[int | None, Field(gt=0, description="自定义发放归入的会员档位（vip_tiers），权益随档位变化。不传：永久会员归入应用的永久档位，限时的只带 features")]
GrantReason = Annotated[str, Field(description="发放原因（记入会员流水；自定义发放时也作为套餐名显示）")]
GrantBonus = Annotated[int | None, Field(ge=0, description="额外赠送的积分")]


@server.tool(**WRITE("vip", idempotent=True))
async def grant_preview(
    app: AppRef,
    user_id: UserID,
    plan_id: GrantPlan = None,
    quantity: GrantQuantity = None,
    days: GrantDays = None,
    lifetime: GrantLifetime = False,
    tier_id: GrantTier = None,
    features: GrantFeatures = None,
    reason: GrantReason = "",
    bonus_integral: GrantBonus = None,
    owned_policy: OwnedPolicy = "",
    upgrade_policy: UpgradePolicy = "",
) -> Any:
    """发放会员前的预览（不落账、不改任何数据）：这一笔对他是否已拥有、会不会被折成积分、折多少；发永久会员时他原有存量会怎样。

    参数与 vip_grant 完全相同。需要 app:write 权限。
    """
    return await GRANT_PREVIEW(appkey=app, body=_grant_body(
        user_id, plan_id, quantity, days, lifetime, features, reason, bonus_integral, owned_policy, upgrade_policy, tier_id,
    ))


@server.tool(**WRITE("vip"))
async def grant(
    app: AppRef,
    user_id: UserID,
    plan_id: GrantPlan = None,
    quantity: GrantQuantity = None,
    days: GrantDays = None,
    lifetime: GrantLifetime = False,
    tier_id: GrantTier = None,
    features: GrantFeatures = None,
    reason: GrantReason = "",
    bonus_integral: GrantBonus = None,
    owned_policy: OwnedPolicy = "",
    upgrade_policy: UpgradePolicy = "",
) -> Any:
    """给用户发放会员（不收费）。两种方式二选一：plan_id 按套餐发放；或 days / lifetime 自定义发放，可附带功能标识。

    立即生效并记入会员流水；对已拥有的权益可能按折算策略折成积分，发永久会员可能结束并折算原有存量。
    发错了只能用 vip_revoke 扣回（赠送的积分不会自动收回）。执行前先调 vip_grant_preview 预览，并向用户复述用户、时长与折算结论后再执行。
    """
    return await GRANT(appkey=app, body=_grant_body(
        user_id, plan_id, quantity, days, lifetime, features, reason, bonus_integral, owned_policy, upgrade_policy, tier_id,
    ))


@server.tool(**DESTRUCTIVE("vip"))
async def revoke(
    app: AppRef,
    user_id: UserID,
    days: Annotated[int | None, Field(gt=0, le=36500, description="从限时会员到期时间往回扣的天数；扣过此刻即立即结束")] = None,
    lifetime: Annotated[bool, Field(description="取消永久会员（作废全部仍生效的永久开通，days 忽略）")] = False,
    reason: Annotated[str, Field(description="收回原因（记入会员流水）")] = "",
) -> Any:
    """收回会员：扣减限时会员天数，或取消永久会员。只收权益不退钱（付过款的请走订单退款）。

    取消永久会员时，若折算策略开启了 restoreOnRevoke，会还回开通时折掉的存量并收回折给的积分。不可撤销，执行前务必向用户确认。
    """
    if not lifetime and not days:
        raise ToolError("需要 days（扣减天数）或 lifetime=true（取消永久会员）")
    return await REVOKE(appkey=app, body=body(userId=user_id, lifetime=lifetime, days=None if lifetime else days, reason=reason))


# ── 判定与记录 ──


@server.tool(**READ("vip"))
async def entitlement(app: AppRef, user_id: UserID) -> Any:
    """用户当前的会员权益判定：是不是会员、是否永久、到期时间、拥有的功能（含功能解锁）。与用户端 /vip/status 同一个判定入口。"""
    return await ENTITLEMENT(appkey=app, query={"userId": user_id})


@server.tool(**READ("vip"))
async def transactions(app: AppRef, user_id: OptionalUser = None, page: Page = 1, limit: Limit = 20) -> Any:
    """会员流水：购买、管理员发放、扣减、取消永久、试用、折算等每一笔开通记录。"""
    return await TRANSACTIONS(appkey=app, query=q(userId=user_id, page=page, limit=limit))


@server.tool(**READ("vip"))
async def conversions(app: AppRef, user_id: OptionalUser = None, page: Page = 1, limit: Limit = 20) -> Any:
    """折算记录：已拥有发放折成积分、开通永久时存量折算、取消永久时还回的每一笔。"""
    return await CONVERSIONS(appkey=app, query=q(userId=user_id, page=page, limit=limit))


# ── 试用 ──


@server.tool(**READ("vip"))
async def trial_claims(app: AppRef, page: Page = 1, limit: Limit = 20) -> Any:
    """试用领取记录与汇总（领取人数等）。"""
    return await TRIAL_CLAIMS(appkey=app, query=q(page=page, limit=limit))


@server.tool(**WRITE("vip"))
async def claim_trial(app: AppRef, user_id: UserID) -> Any:
    """代用户领取试用会员（按应用上架中的试用套餐，占用他的试用资格）。需要应用已配置上架的试用套餐。"""
    return await TRIAL_CLAIM(appkey=app, body={"userId": user_id})


@server.tool(**DESTRUCTIVE("vip"))
async def reset_trial(app: AppRef, user_id: UserID) -> Any:
    """恢复用户的试用资格（删除他的试用领取记录，之后可以再领一次）。已开通的试用时长不受影响。执行前先向用户确认。"""
    return await TRIAL_RESET(appkey=app, userId=user_id)


# ── 折算策略 ──


@server.tool(**READ("vip"))
async def conversion_policy(app: AppRef) -> Any:
    """应用的会员折算策略（policy）、两种处理方式的可选项与说明（ownedModes / upgradeModes）以及默认值（defaults）。"""
    return await CONVERSION_POLICY(appkey=app)


@server.tool(**WRITE("vip"))
async def save_conversion_policy(
    app: AppRef,
    owned: Annotated[Literal["convert", "grant"] | None, Field(description="已拥有的权益被发放时：convert 折算为积分 / grant 照常发放")] = None,
    upgrade: Annotated[Literal["convert", "keep"] | None, Field(description="开通永久会员时被覆盖的存量：convert 折算为积分并结束存量 / keep 保留")] = None,
    per_vip_day: Annotated[int | None, Field(ge=0, le=1_000_000, description="会员每 1 天折多少积分")] = None,
    per_feature_day: Annotated[int | None, Field(ge=0, le=1_000_000, description="限时功能解锁 / 免广告时长每 1 天折多少积分")] = None,
    per_feature_hour: Annotated[int | None, Field(ge=0, le=1_000_000, description="功能使用时长每 60 分钟折多少积分")] = None,
    per_lifetime: Annotated[int | None, Field(ge=0, le=1_000_000, description="已包含的永久套餐 / 已永久解锁的功能再被发放时给的固定积分")] = None,
    restore_on_revoke: Annotated[bool | None, Field(description="取消或退款永久会员时，还回开通时折掉的存量并收回折给的积分")] = None,
) -> Any:
    """修改会员折算策略。只传要改的字段，其余沿用当前值（后端是整体覆盖，这里先读后合并）。

    影响之后所有入口（购买、卡密、抽奖、激励广告、管理员发放等）的折算结果，已发生的折算不受影响。
    """
    current = await CONVERSION_POLICY(appkey=app) or {}
    policy = current.get("policy") if isinstance(current, dict) else None
    policy = policy or (current.get("defaults") if isinstance(current, dict) else None) or {}
    merged = {key: policy.get(key) for key in ("owned", "upgrade", "perVipDay", "perFeatureDay", "perFeatureHour", "perLifetime", "restoreOnRevoke") if key in policy}
    merged.update(body(
        owned=owned, upgrade=upgrade, perVipDay=per_vip_day, perFeatureDay=per_feature_day,
        perFeatureHour=per_feature_hour, perLifetime=per_lifetime, restoreOnRevoke=restore_on_revoke,
    ))
    return await CONVERSION_POLICY_SAVE(appkey=app, body=merged)
