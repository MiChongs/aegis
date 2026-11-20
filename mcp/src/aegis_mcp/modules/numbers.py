"""用户号码：总览、目录、设置（只改给的字段）与概率试算、查号、用户号码页、指定号码、回滚、权益调整、换号记录、权益流水、摇号单、占用与保留号。

号码是按应用唯一的 5–12 位纯数字（展示与查找用），与终生不变的对外 ID 是两层身份，契约见 docs/user-number.md。
权限：读需要 app:number:read；改设置、试算概率、保留号需要 app:number:write；放出占用需要 app:number:delete；
指定号码、回滚、调整权益需要 app:number:grant。
"""

from __future__ import annotations

from typing import Annotated, Any, Literal

from fastmcp import FastMCP
from fastmcp.exceptions import ToolError
from pydantic import Field

from ..core import DELETE, DESTRUCTIVE, GET, POST, PUT, READ, WRITE, AppRef, IsoTime, Limit, Page, UserID, body, q

NAMESPACE = "numbers"
server = FastMCP("numbers")

B = "/api/admin/apps/:appkey/numbers"
CATALOG = GET(f"{B}/catalog")
OVERVIEW = GET(f"{B}/overview")
SETTINGS = GET(f"{B}/settings")
SETTINGS_SAVE = PUT(f"{B}/settings")
ODDS = POST(f"{B}/settings/odds")
INSPECT = GET(f"{B}/inspect")
USER = GET(f"{B}/users/:userId")
ASSIGN = POST(f"{B}/users/:userId/assign")
ROLLBACK = POST(f"{B}/users/:userId/rollback")
RIGHTS = POST(f"{B}/users/:userId/rights")
HISTORY = GET(f"{B}/history")
LEDGER = GET(f"{B}/ledger")
DRAWS = GET(f"{B}/draws")
HOLDS = GET(f"{B}/holds")
RESERVE = POST(f"{B}/holds")
RELEASE = DELETE(f"{B}/holds/:number")

NUMBER_PATTERN = r"^[1-9][0-9]{4,11}$"
Number = Annotated[str, Field(pattern=NUMBER_PATTERN, description="号码：5–12 位纯数字字符串，不以 0 开头（用字符串传，避免大数丢精度）")]
OptionalUser = Annotated[int | None, Field(gt=0, description="只看这个用户（管理端用户 ID），留空为全部")]
RightKind = Literal["change", "ticket", "draw"]
HoldKind = Literal["reserved", "frozen", "candidate"]

TIERS_HELP = "等级：0 普通 / 1 精品 / 2 稀有 / 3 传说 / 4 至尊"
PATTERN_KEYS = (
    "all_same 全同号 / len5 五位号 / len6 六位号 / len7 七位号 / leopard5 豹子号 / mirror 对称号 / run4 四连号 / "
    "straight4 顺子号 / year_tail 年份尾号 / run3_tail 尾号三连 / aabb / abab / lucky_tail 吉利尾号"
)

# 工具参数名 → 后端 Settings 字段名
_SETTING_FIELDS = {
    "enabled": "enabled", "min_length": "minLength", "max_length": "maxLength", "default_length": "defaultLength",
    "self_pick": "selfPick", "cooldown_days": "cooldownDays", "freeze_days": "freezeDays",
    "draw_candidates": "drawCandidates", "draw_hold_minutes": "drawHoldMinutes", "draw_weights": "drawWeights",
    "luck_spread": "luckSpread", "luck_supreme": "luckSupreme",
}
_SETTING_KEYS = (*_SETTING_FIELDS.values(), "patternTiers")

Weights = Annotated[
    list[Annotated[float, Field(ge=0)]] | None,
    Field(min_length=5, max_length=5, description="摇号里各级的基础权重，按等级 0–4 依次给 5 个非负数（默认 70 / 20 / 7 / 2.5 / 0.5）；非普通级会乘以今日号运"),
]
PatternTiers = Annotated[
    dict[str, int | None] | None,
    Field(description=f"规律等级覆盖：{{规律键: 等级 0–4，-1 停用，null 恢复默认}}，只改给出的键。规律键：{PATTERN_KEYS}"),
]


def _settings_body(current: dict[str, Any] | None) -> dict[str, Any]:
    """后端 Settings 只取可写字段（去掉 updatedBy / updatedAt）。"""
    current = current or {}
    return {key: current[key] for key in _SETTING_KEYS if key in current}


def _merge_settings(current: dict[str, Any] | None, values: dict[str, Any], pattern_tiers: dict[str, int | None] | None) -> dict[str, Any]:
    merged = _settings_body(current)
    for name, value in values.items():
        if value is not None:
            merged[_SETTING_FIELDS[name]] = value
    if pattern_tiers:
        tiers = dict(merged.get("patternTiers") or {})
        for key, tier in pattern_tiers.items():
            if tier is None:
                tiers.pop(key, None)
            elif tier == -1 or 0 <= tier <= 4:
                tiers[key] = tier
            else:
                raise ToolError(f"规律 {key} 的等级须为 0–4 或 -1（停用），实际 {tier}")
        merged["patternTiers"] = tiers or None
        if merged["patternTiers"] is None:
            merged.pop("patternTiers")
    return merged


# ── 目录、总览与设置 ──

@server.tool(**READ("numbers"))
async def catalog(app: AppRef) -> Any:
    """号码目录：五个等级（名称与建议主色）、规律（带本应用生效的等级）、权益种类、流水来源与设置边界。改设置前先看它。"""
    return await CATALOG(appkey=app)


@server.tool(**READ("numbers"))
async def overview(app: AppRef) -> Any:
    """号码总览：用户数、各级号码数、今日与近 7 天换号、今日摇号、打开的摇号单、各类占用数（保留 / 冻结 / 候选）、未用的权益合计。"""
    return await OVERVIEW(appkey=app)


@server.tool(**READ("numbers"))
async def settings(app: AppRef) -> Any:
    """应用的号码设置：开关、可换位数范围、默认位数、是否允许自选、换号冷却、旧号冻结、摇号候选数与锁定时长、各级权重、号运、规律等级覆盖。"""
    return await SETTINGS(appkey=app)


@server.tool(**WRITE("numbers", idempotent=True))
async def save_settings(
    app: AppRef,
    enabled: Annotated[bool | None, Field(description="开放换号与摇号；关闭时号码照常发、照常展示，只关掉更换入口")] = None,
    min_length: Annotated[int | None, Field(ge=5, le=12, description="用户能换到的最短位数（5–12）")] = None,
    max_length: Annotated[int | None, Field(ge=5, le=12, description="用户能换到的最长位数（5–12）")] = None,
    default_length: Annotated[int | None, Field(ge=8, le=12, description="新用户默认发的普通号位数（8–12，只影响之后建的号）")] = None,
    self_pick: Annotated[bool | None, Field(description="允许用改号卡 / 靓号券自选；关闭时只能摇号")] = None,
    cooldown_days: Annotated[int | None, Field(ge=0, le=365, description="两次换号之间至少隔几天（0 不限；管理员指定不受限）")] = None,
    freeze_days: Annotated[int | None, Field(ge=0, le=3650, description="换下来的旧号冻结几天（0 立即放出；冻结期内原主人可免费换回）")] = None,
    draw_candidates: Annotated[int | None, Field(ge=1, le=12, description="一次摇几个候选号（1–12）")] = None,
    draw_hold_minutes: Annotated[int | None, Field(ge=1, le=120, description="候选号锁定几分钟（1–120），过期作废、次数不退")] = None,
    draw_weights: Weights = None,
    luck_spread: Annotated[float | None, Field(ge=0, le=2, description="号运差异 σ（0 关闭号运，默认 0.3）")] = None,
    luck_supreme: Annotated[bool | None, Field(description="号运叠加抽奖设置里的应用级至尊加成")] = None,
    pattern_tiers: PatternTiers = None,
) -> Any:
    """修改号码设置：只传要改的字段，工具先读当前设置、合并后整份保存。

    改摇号权重、号运或规律等级会影响之后所有用户的摇号概率与靓号判定（已换上的号不受影响），可先用 numbers_preview_odds 试算；执行前向用户复述改动。
    """
    values = {
        "enabled": enabled, "min_length": min_length, "max_length": max_length, "default_length": default_length,
        "self_pick": self_pick, "cooldown_days": cooldown_days, "freeze_days": freeze_days,
        "draw_candidates": draw_candidates, "draw_hold_minutes": draw_hold_minutes, "draw_weights": draw_weights,
        "luck_spread": luck_spread, "luck_supreme": luck_supreme,
    }
    if all(value is None for value in values.values()) and not pattern_tiers:
        raise ToolError("至少给一个要修改的字段")
    current = await SETTINGS(appkey=app)
    return await SETTINGS_SAVE(appkey=app, body=_merge_settings(current, values, pattern_tiers))


@server.tool(**WRITE("numbers", idempotent=True))
async def preview_odds(
    app: AppRef,
    luck: Annotated[float, Field(gt=0, le=10, description="号运系数（1 为平均水平，大于 1 走运）")] = 1,
    draw_weights: Weights = None,
    luck_spread: Annotated[float | None, Field(ge=0, le=2, description="试算用的号运差异 σ")] = None,
    luck_supreme: Annotated[bool | None, Field(description="试算时是否叠加至尊加成")] = None,
) -> Any:
    """试算摇号各级出现概率：以当前设置为底、套上给出的改动（不保存），算某个号运下 0–4 级各自的概率。只算不写。"""
    current = await SETTINGS(appkey=app)
    values = {"draw_weights": draw_weights, "luck_spread": luck_spread, "luck_supreme": luck_supreme}
    return await ODDS(appkey=app, body={"settings": _merge_settings(current, values, None), "luck": luck})


# ── 查询 ──

@server.tool(**READ("numbers"))
async def inspect(app: AppRef, number: Number) -> Any:
    """查一个号码：等级、命中的规律、谁在用、是否被保留 / 冻结 / 锁定为候选。"""
    return await INSPECT(appkey=app, query={"number": number})


@server.tool(name="user", **READ("numbers"))
async def user_numbers(app: AppRef, user_id: UserID) -> Any:
    """一个用户的号码页：当前号码与等级、权益余额（改号卡 / 各级靓号券 / 摇号次数）、打开的摇号单、冻结中的旧号、最近换号与今日号运。"""
    return await USER(appkey=app, userId=user_id)


@server.tool(**READ("numbers"))
async def history(app: AppRef, user_id: OptionalUser = None, page: Page = 1, limit: Limit = 20) -> Any:
    """换号记录（新的在前）：旧号、新号、等级、来源（change 改号卡 / ticket 靓号券 / draw 摇号 / frozen 换回旧号 / admin 管理员指定 / grant 物品发放 / rollback 回滚）、用掉的权益。

    items[].id 即 numbers_rollback 的 history_id。
    """
    return await HISTORY(appkey=app, query=q(userId=user_id, page=page, limit=limit))


@server.tool(**READ("numbers"))
async def ledger(
    app: AppRef,
    user_id: OptionalUser = None,
    kind: Annotated[Literal["", "change", "ticket", "draw"], Field(description="权益种类：change 改号卡 / ticket 靓号券 / draw 摇号次数，留空为全部")] = "",
    page: Page = 1,
    limit: Limit = 20,
) -> Any:
    """号码权益流水（新的在前）：每笔发放与使用的变动量、变动后余额、来源（item_grant / level / card_key / points_store / lottery / invite / leaderboard / promo / rewarded_ad / admin / change / draw / refund）与单号。"""
    return await LEDGER(appkey=app, query=q(userId=user_id, kind=kind, page=page, limit=limit))


@server.tool(**READ("numbers"))
async def draws(app: AppRef, user_id: OptionalUser = None, page: Page = 1, limit: Limit = 20) -> Any:
    """摇号单（新的在前）：候选号与各自等级、当时的号运、状态、选中的号、锁定到期时间。"""
    return await DRAWS(appkey=app, query=q(userId=user_id, page=page, limit=limit))


@server.tool(**READ("numbers"))
async def holds(
    app: AppRef,
    kind: Annotated[Literal["", "reserved", "frozen", "candidate"], Field(description="reserved 保留号 / frozen 冻结的旧号 / candidate 摇号候选锁定，留空为全部")] = "",
    page: Page = 1,
    limit: Limit = 20,
) -> Any:
    """号码占用列表：保留号、冻结中的旧号（带原主人）、摇号锁定的候选号，含备注与到期时间。"""
    return await HOLDS(appkey=app, query=q(kind=kind, page=page, limit=limit))


# ── 写操作 ──

@server.tool(**DESTRUCTIVE("numbers"))
async def assign(
    app: AppRef,
    user_id: UserID,
    number: Number,
    force: Annotated[bool, Field(description="允许拿保留号、别人冻结中的旧号与锁定的候选号（仍不能抢别人正在用的号）")] = False,
    note: Annotated[str, Field(max_length=200, description="备注（记入换号记录）")] = "",
) -> Any:
    """给用户指定号码：不扣权益、不看冷却，用户原来的号按冻结天数冻结。

    会立即改掉用户展示与被查找用的号码；force 会占用保留号或别人冻结中的旧号。执行前向用户复述用户、号码与是否 force 并确认；撤销用 numbers_rollback。
    """
    return await ASSIGN(appkey=app, userId=user_id, body=body(number=number, force=force, note=note or None))


@server.tool(**DESTRUCTIVE("numbers"))
async def rollback(
    app: AppRef,
    user_id: UserID,
    history_id: Annotated[int, Field(gt=0, description="换号记录 ID（numbers_history 或 numbers_user 结果里的 id）")],
) -> Any:
    """把用户的号码回到某次换号之前的那个号（强制，不扣权益、不看冷却）。

    当前号码会被换下并冻结，那次换号用掉的权益不退。执行前向用户复述用户与要回到的号码并确认。
    """
    return await ROLLBACK(appkey=app, userId=user_id, body={"historyId": history_id})


@server.tool(**DESTRUCTIVE("numbers"))
async def adjust_rights(
    app: AppRef,
    user_id: UserID,
    kind: Annotated[RightKind, Field(description="change 改号卡 / ticket 靓号券 / draw 摇号次数")],
    delta: Annotated[int, Field(ge=-100000, le=100000, description="正数为发放、负数为扣减（扣到负数会报 42953），不能为 0")],
    level: Annotated[int, Field(ge=0, le=4, description="靓号券等级 1 精品 / 2 稀有 / 3 传说 / 4 至尊；change 与 draw 填 0")] = 0,
    note: Annotated[str, Field(max_length=200, description="原因（记入权益流水）")] = "",
) -> Any:
    """调整一个用户的号码权益余额，返回调整后的全部权益。

    扣减不可撤回（只能再发放补回）：执行前向用户复述种类、等级、数量与原因并确认。
    """
    if delta == 0:
        raise ToolError("delta 不能为 0")
    if kind == "ticket" and not 1 <= level <= 4:
        raise ToolError("靓号券的 level 须为 1–4（1 精品 2 稀有 3 传说 4 至尊）")
    if kind != "ticket" and level != 0:
        raise ToolError(f"{kind} 的 level 须为 0")
    return await RIGHTS(appkey=app, userId=user_id, body=body(kind=kind, level=level, delta=delta, note=note or None))


@server.tool(**WRITE("numbers", idempotent=True))
async def reserve(
    app: AppRef,
    numbers: Annotated[list[Annotated[str, Field(pattern=NUMBER_PATTERN)]], Field(min_length=1, max_length=500, description="要保留的号码（字符串，1–500 个）")],
    note: Annotated[str, Field(max_length=200, description="备注（如 运营活动预留）")] = "",
    expires_at: Annotated[IsoTime, Field(description="到期自动放出（ISO 8601），留空长期保留")] = "",
) -> Any:
    """把一批号码设为保留号：用户自选与摇号都拿不到，只能管理员 force 指定。已有人在用或格式不对的跳过，结果里分 reserved / skipped。"""
    return await RESERVE(appkey=app, body=body(numbers=numbers, note=note or None, expiresAt=expires_at or None))


@server.tool(**DESTRUCTIVE("numbers"))
async def release(app: AppRef, number: Number) -> Any:
    """删掉一条号码占用：保留号放出（之后任何人可自选或摇到）、冻结的旧号提前解除（原主人失去免费换回的机会）、锁定的候选号释放。

    不可撤回：放出后号码可能马上被别人换走。执行前向用户复述号码与占用种类并确认，可先用 numbers_inspect 查看。
    """
    return await RELEASE(appkey=app, number=number)
