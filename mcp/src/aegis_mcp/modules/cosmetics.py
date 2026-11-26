"""个性装扮：头像框、资料卡背景、昵称样式、勋章、号码铭牌、会员铭牌的目录、设置、设计稿草稿与发布、版本回退、素材库、用户的发放 / 收回 / 摘下、持有记录与统计。"""

from __future__ import annotations

from typing import Annotated, Any, Literal

from fastmcp.exceptions import ToolError
from fastmcp import FastMCP
from pydantic import Field

from ..core import DELETE, DESTRUCTIVE, GET, POST, PUT, READ, WRITE, AppRef, Limit, Page, UserID, body, q

NAMESPACE = "cosmetics"
server = FastMCP("cosmetics")

B = "/api/admin/apps/:appkey/cosmetics"
CATALOG = GET(f"{B}/catalog")
SETTINGS = GET(f"{B}/settings")
SETTINGS_SAVE = PUT(f"{B}/settings")
ASSETS = GET(f"{B}/assets")
ASSET_DELETE = DELETE(f"{B}/assets/:assetId")
ITEMS = GET(f"{B}/items")
ITEM = GET(f"{B}/items/:itemId")
ITEM_CREATE = POST(f"{B}/items")
ITEM_UPDATE = PUT(f"{B}/items/:itemId")
ITEM_DELETE = DELETE(f"{B}/items/:itemId")
ITEM_DRAFT = PUT(f"{B}/items/:itemId/draft")
ITEM_PUBLISH = POST(f"{B}/items/:itemId/publish")
ITEM_STATUS = PUT(f"{B}/items/:itemId/status")
ITEM_DUPLICATE = POST(f"{B}/items/:itemId/duplicate")
ITEM_ORDER = PUT(f"{B}/item-order")
REVISIONS = GET(f"{B}/items/:itemId/revisions")
RESTORE = POST(f"{B}/items/:itemId/revisions/:revision/restore")
OWNERSHIPS = GET(f"{B}/ownerships")
STATS = GET(f"{B}/stats")
USER = GET(f"{B}/users/:userId")
GRANT = POST(f"{B}/users/:userId/grant")
REVOKE = POST(f"{B}/users/:userId/ownerships/:ownershipId/revoke")
UNEQUIP = POST(f"{B}/users/:userId/unequip")

ItemID = Annotated[int, Field(gt=0, description="装扮 ID（cosmetics_items 结果里的 id）")]
Slot = Literal["avatar_frame", "profile_card", "name_style", "badge", "id_plate", "vip_plate"]
Rarity = Literal["common", "rare", "epic", "legendary", "mythic"]
UnlockMode = Literal["grant", "free", "vip", "level", "feature"]
Days = Annotated[int, Field(ge=0, le=3650, description="天数，0 为永久；同一件多次限时发放时顺延")]


def _unlock(mode: str | None, tier_ids: list[int] | None, min_level: int | None, feature: str | None,
            hint: str | None) -> dict[str, Any] | None:
    if mode is None:
        return None
    return body(mode=mode, tierIds=tier_ids, minLevel=min_level, feature=feature, hint=hint)


# ── 目录与设置 ──

@server.tool(**READ("cosmetics"))
async def catalog(app: AppRef) -> Any:
    """装扮的表单目录：槽位（画布预设）、稀有度、获取方式、来源、设计稿规格枚举与上界、内置模板、本应用的会员档位与功能、当前设置。新建或设计前先看它。"""
    return await CATALOG(appkey=app)


@server.tool(**READ("cosmetics"))
async def settings(app: AppRef) -> Any:
    """装扮设置：是否开启、最多同时佩戴几枚勋章。"""
    return await SETTINGS(appkey=app)


@server.tool(**WRITE("cosmetics", idempotent=True))
async def save_settings(
    app: AppRef,
    enabled: Annotated[bool, Field(description="是否开启个性装扮（关闭后用户端入口隐藏、身上的装扮不再显示，持有记录保留）")],
    badge_slots: Annotated[int, Field(ge=1, le=5, description="最多同时佩戴几枚勋章；调少后超出的那几位会被摘下")] = 3,
) -> Any:
    """开启 / 关闭个性装扮并设置勋章位数。可随时改回。"""
    return await SETTINGS_SAVE(appkey=app, body={"enabled": enabled, "badgeSlots": badge_slots})


# ── 装扮 ──

@server.tool(**READ("cosmetics"))
async def items(
    app: AppRef,
    slot: Slot | None = None,
    status: Literal["draft", "active", "archived"] | None = None,
    rarity: Rarity | None = None,
    keyword: Annotated[str | None, Field(description="按名称、编码、系列搜索")] = None,
) -> Any:
    """装扮列表（不含设计稿），带持有人数与佩戴人数、是否有未发布的修改。"""
    return await ITEMS(appkey=app, query=q(slot=slot, status=status, rarity=rarity, keyword=keyword))


@server.tool(**READ("cosmetics"))
async def item(app: AppRef, item_id: ItemID) -> Any:
    """单件装扮：基本信息、已发布的设计稿 design、设计器草稿 draft（素材地址已解析）。"""
    return await ITEM(appkey=app, itemId=item_id)


@server.tool(**WRITE("cosmetics"))
async def create_item(
    app: AppRef,
    slot: Slot,
    code: Annotated[str, Field(min_length=1, max_length=48, description="编码：小写字母、数字、下划线、短横线与点，应用内唯一")],
    name: Annotated[str, Field(min_length=1, max_length=32, description="名称")],
    rarity: Rarity = "common",
    description: Annotated[str | None, Field(max_length=200, description="简介")] = None,
    series: Annotated[str | None, Field(max_length=32, description="系列名")] = None,
    unlock_mode: Annotated[UnlockMode, Field(description="获取方式：grant 只能发放获得 / free 免费 / vip 会员专属 / level 等级解锁 / feature 持有功能")] = "grant",
    tier_ids: Annotated[list[int] | None, Field(description="vip 模式限定的会员档位 ID")] = None,
    min_level: Annotated[int | None, Field(ge=1, le=10000, description="level 模式的最低等级")] = None,
    feature: Annotated[str | None, Field(description="feature 模式需要的功能标识")] = None,
    hint: Annotated[str | None, Field(max_length=64, description="给用户看的获取说明")] = None,
    hidden: Annotated[bool, Field(description="不在装扮中心列出（活动限定、补偿专用；仍可发放、持有者仍可佩戴）")] = False,
    template: Annotated[str | None, Field(description="套用的模板键（cosmetics_catalog 的 templates，槽位须一致）")] = None,
) -> Any:
    """新建一件装扮（草稿状态，需设计并发布后才能上架）。"""
    return await ITEM_CREATE(appkey=app, body=body(
        slot=slot, code=code, name=name, rarity=rarity, description=description, series=series, hidden=hidden,
        template=template, unlock=_unlock(unlock_mode, tier_ids, min_level, feature, hint)))


@server.tool(**WRITE("cosmetics", idempotent=True))
async def update_item(
    app: AppRef,
    item_id: ItemID,
    name: Annotated[str | None, Field(min_length=1, max_length=32)] = None,
    code: Annotated[str | None, Field(min_length=1, max_length=48)] = None,
    rarity: Rarity | None = None,
    description: Annotated[str | None, Field(max_length=200)] = None,
    series: Annotated[str | None, Field(max_length=32)] = None,
    unlock_mode: UnlockMode | None = None,
    tier_ids: list[int] | None = None,
    min_level: Annotated[int | None, Field(ge=1, le=10000)] = None,
    feature: str | None = None,
    hint: Annotated[str | None, Field(max_length=64)] = None,
    hidden: bool | None = None,
    sort_order: int | None = None,
) -> Any:
    """修改装扮的基本信息（没传的字段保持原值；槽位不能改）。已持有的人不受影响，获取方式变化即时生效。"""
    current = await ITEM(appkey=app, itemId=item_id)
    if not isinstance(current, dict):
        raise ToolError(f"找不到 ID 为 {item_id} 的装扮")
    old_unlock = current.get("unlock") or {}
    unlock = old_unlock
    if any(v is not None for v in (unlock_mode, tier_ids, min_level, feature, hint)):
        unlock = body(
            mode=unlock_mode or old_unlock.get("mode"),
            tierIds=tier_ids if tier_ids is not None else old_unlock.get("tierIds"),
            minLevel=min_level if min_level is not None else old_unlock.get("minLevel"),
            feature=feature if feature is not None else old_unlock.get("feature"),
            hint=hint if hint is not None else old_unlock.get("hint"),
        )
    pick = lambda new, key: new if new is not None else current.get(key)  # noqa: E731
    return await ITEM_UPDATE(appkey=app, itemId=item_id, body=body(
        code=pick(code, "code"), name=pick(name, "name"), rarity=pick(rarity, "rarity"),
        description=pick(description, "description"), series=pick(series, "series"), unlock=unlock,
        hidden=pick(hidden, "hidden"), sortOrder=pick(sort_order, "sortOrder")))


@server.tool(**WRITE("cosmetics", idempotent=True))
async def save_draft(
    app: AppRef,
    item_id: ItemID,
    design: Annotated[dict[str, Any], Field(description="整份设计稿（规格见 docs/cosmetics.md「设计稿」：version、canvas、avatar、duration、layers、nameStyle、assets；铭牌另有 plate（文字区域、template、可选 stretch 按字伸缩），且必须带 nameStyle）")],
) -> Any:
    """保存设计器草稿（整份校验，错误信息会点名第几个图层哪个字段）。不影响已发布的版本，发布后才对用户生效。"""
    return await ITEM_DRAFT(appkey=app, itemId=item_id, body={"design": design})


@server.tool(**WRITE("cosmetics"))
async def publish(app: AppRef, item_id: ItemID, note: Annotated[str | None, Field(max_length=128, description="发布说明")] = None) -> Any:
    """把草稿发布为新版本：上架中的装扮立即以新外观下发给所有用户（客户端按版本号刷新缓存）。可用 cosmetics_restore_revision 回退。"""
    return await ITEM_PUBLISH(appkey=app, itemId=item_id, body=body(note=note or ""))


@server.tool(**READ("cosmetics"))
async def revisions(app: AppRef, item_id: ItemID) -> Any:
    """发布历史（每个版本的设计稿，保留最近 50 份）。"""
    return await REVISIONS(appkey=app, itemId=item_id)


@server.tool(**WRITE("cosmetics"))
async def restore_revision(app: AppRef, item_id: ItemID, revision: Annotated[int, Field(gt=0, description="版本号")]) -> Any:
    """把某个历史版本放回草稿（不直接发布；确认后再调 cosmetics_publish）。会覆盖当前草稿。"""
    return await RESTORE(appkey=app, itemId=item_id, revision=revision)


@server.tool(**WRITE("cosmetics", idempotent=True))
async def set_status(app: AppRef, item_id: ItemID, status: Literal["active", "archived", "draft"]) -> Any:
    """上架 / 下架 / 回到草稿。下架后用户看不到、已持有的人也戴不上（重新上架即恢复）；未发布过的不能上架。"""
    return await ITEM_STATUS(appkey=app, itemId=item_id, body={"status": status})


@server.tool(**WRITE("cosmetics"))
async def duplicate(app: AppRef, item_id: ItemID) -> Any:
    """复制一件装扮（基本信息 + 草稿），新编码自动加后缀，状态为草稿。"""
    return await ITEM_DUPLICATE(appkey=app, itemId=item_id)


@server.tool(**WRITE("cosmetics", idempotent=True))
async def reorder(app: AppRef, item_ids: Annotated[list[int], Field(min_length=1, description="按此顺序重排的装扮 ID")]) -> Any:
    """调整装扮在装扮中心的排序。"""
    return await ITEM_ORDER(appkey=app, body={"itemIds": item_ids})


@server.tool(**DESTRUCTIVE("cosmetics"))
async def delete_item(app: AppRef, item_id: ItemID) -> Any:
    """删除一件装扮（不可恢复）。有人持有过的不能删，请改为下架。"""
    return await ITEM_DELETE(appkey=app, itemId=item_id)


# ── 素材 ──

@server.tool(**READ("cosmetics"))
async def assets(
    app: AppRef,
    kind: Literal["image", "sprite"] | None = None,
    keyword: str | None = None,
    page: Page = 1,
    limit: Limit = 60,
) -> Any:
    """素材库（设计稿 assets 里引用的素材编号来自这里），带尺寸、地址与被引用数。上传素材请在控制台进行。"""
    return await ASSETS(appkey=app, query=q(kind=kind, keyword=keyword, page=page, limit=limit))


@server.tool(**DESTRUCTIVE("cosmetics"))
async def delete_asset(app: AppRef, asset_id: Annotated[int, Field(gt=0, description="素材 ID")]) -> Any:
    """删除一个没被任何装扮引用的素材（连同存储里的文件，不可恢复）。"""
    return await ASSET_DELETE(appkey=app, assetId=asset_id)


# ── 用户与记录 ──

@server.tool(**READ("cosmetics"))
async def ownerships(
    app: AppRef,
    user_id: UserID | None = None,
    item_id: Annotated[int | None, Field(gt=0, description="装扮 ID")] = None,
    source: Annotated[str | None, Field(description="来源：admin_grant / points_store / lottery / card_key / item_grant / invite / leaderboard / promo")] = None,
    state: Literal["live", "ended", "revoked"] | None = None,
    page: Page = 1,
    limit: Limit = 50,
) -> Any:
    """装扮持有记录（谁从哪里得到了哪一件、到什么时候）。"""
    return await OWNERSHIPS(appkey=app, query=q(userId=user_id, cosmeticId=item_id, source=source, state=state, page=page, limit=limit))


@server.tool(**READ("cosmetics"))
async def stats(app: AppRef) -> Any:
    """装扮统计：件数、持有人数、佩戴人数、各槽位与来源分布、近 14 天发放、热门装扮、即将到期、素材占用、待发布草稿。"""
    return await STATS(appkey=app)


@server.tool(**READ("cosmetics"))
async def user(app: AppRef, user_id: UserID) -> Any:
    """一个用户的装扮：持有记录、佩戴行（各自是否生效与原因）、此刻身上的装扮、判定事实（会员、档位、等级、功能）。"""
    return await USER(appkey=app, userId=user_id)


@server.tool(**WRITE("cosmetics"))
async def grant(
    app: AppRef,
    user_id: UserID,
    item_id: ItemID,
    days: Days = 0,
    equip: Annotated[bool, Field(description="发放后顺手戴上：替换该槽位原有佩戴，勋章放第一个空位（满了不戴）；已永久持有也会戴上")] = False,
) -> Any:
    """给用户发一件装扮（站内信通知）。装扮须已上架；已永久持有时不重复发放。可用 cosmetics_revoke 收回。"""
    return await GRANT(appkey=app, userId=user_id, body={"cosmeticId": item_id, "days": days, "equip": equip})


@server.tool(**DESTRUCTIVE("cosmetics"))
async def revoke(
    app: AppRef,
    user_id: UserID,
    ownership_id: Annotated[int, Field(gt=0, description="持有记录 ID（cosmetics_user 的 ownerships）")],
    reason: Annotated[str | None, Field(max_length=256, description="收回原因")] = None,
) -> Any:
    """收回一条持有记录（立即失效，佩戴中的会消失；不可撤销，需要时重新发放）。"""
    return await REVOKE(appkey=app, userId=user_id, ownershipId=ownership_id, body=body(reason=reason or ""))


@server.tool(**WRITE("cosmetics", idempotent=True))
async def unequip(
    app: AppRef,
    user_id: UserID,
    slot: Slot,
    position: Annotated[int, Field(ge=-1, le=4, description="勋章位置（0 起）；-1 摘下整个槽位")] = -1,
) -> Any:
    """摘下用户某个槽位上的装扮（持有记录不变，用户可以重新佩戴）。"""
    return await UNEQUIP(appkey=app, userId=user_id, body={"slot": slot, "position": position})
