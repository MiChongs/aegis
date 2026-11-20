"""卡密：批次生成 / 导入 / 补发 / 提卡 / 启停 / 删除、单卡调整与作废恢复、设备解绑、管理员代兑、核销记录、统计与目录。"""

from __future__ import annotations

from typing import Annotated, Any, Literal

from fastmcp import FastMCP
from fastmcp.exceptions import ToolError
from pydantic import BaseModel, Field

from ..core import DELETE, DESTRUCTIVE, GET, POST, PUT, READ, WRITE, AppRef, IsoTime, Page, body, q, require_one

NAMESPACE = "cardkey"
server = FastMCP("cardkey")

B = "/api/admin/apps/:appkey/card-keys"
CATALOG = GET(f"{B}/catalog")
STATS = GET(f"{B}/stats")
BATCHES = GET(f"{B}/batches")
BATCH = GET(f"{B}/batches/:batchId")
BATCH_CREATE = POST(f"{B}/batches")
BATCH_UPDATE = PUT(f"{B}/batches/:batchId")
BATCH_STATUS = PUT(f"{B}/batches/:batchId/status")
BATCH_DELETE = DELETE(f"{B}/batches/:batchId")
BATCH_APPEND = POST(f"{B}/batches/:batchId/append")
BATCH_EXTRACT = POST(f"{B}/batches/:batchId/extract")
CODES = GET(f"{B}/codes")
CODE = GET(f"{B}/codes/:cardId")
CODE_UPDATE = PUT(f"{B}/codes/:cardId")
CODES_DISABLE = POST(f"{B}/codes/disable")
CODES_RESTORE = POST(f"{B}/codes/restore")
CODES_DELETE = POST(f"{B}/codes/delete")
CODES_DISTRIBUTE = POST(f"{B}/codes/distribute")
DEVICES = GET(f"{B}/codes/:cardId/devices")
DEVICES_RESET = DELETE(f"{B}/codes/:cardId/devices")
DEVICE_UNBIND = DELETE(f"{B}/codes/:cardId/devices/:deviceId")
REDEMPTIONS = GET(f"{B}/redemptions")
REDEEM = POST(f"{B}/redeem")

MAX_COUNT = 10_000

BatchID = Annotated[int, Field(gt=0, description="批次 ID（cardkey_batches 结果里的 id）")]
CardID = Annotated[int, Field(gt=0, description="卡密 ID（cardkey_list 结果里的 id，不是卡面）")]
CardIDs = Annotated[list[Annotated[int, Field(gt=0)]], Field(min_length=1, max_length=1000, description="卡密 ID 列表（按选中的卡执行，不按筛选条件）")]
Kind = Literal["login", "redeem"]
Audience = Literal["all", "vip", "non_vip"]


class Reward(BaseModel):
    """一项卡密权益。取值字段由权益类型决定（见 cardkey_catalog）。"""

    type: Literal["vip_plan", "vip_days", "feature", "integral", "experience", "balance", "lottery_draws", "license_days", "device_slots",
                  "number_change", "number_ticket", "number_draw"] = Field(
        description="vip_plan 会员套餐（ref_id）/ vip_days 会员天数 / feature 功能解锁（feature + amount 天，0 为永久）/ integral 积分 / "
                    "experience 经验 / balance 余额（money）/ lottery_draws 抽奖次数 / license_days 续费卡：顺延名下授权卡天数 / device_slots 授权卡设备位 / "
                    "number_change 改号卡（amount 张）/ number_ticket 靓号券（amount 张 + ref_id 等级 1 精品 2 稀有 3 传说 4 至尊）/ number_draw 摇号次数（amount 次）")
    amount: int | None = Field(default=None, ge=0, description="数量（天数 / 点数 / 次数 / 设备位）")
    money: str | None = Field(default=None, description="余额金额（十进制字符串，如 \"10.00\"）")
    ref_id: int | None = Field(default=None, gt=0, description="会员套餐 ID（vip_plan 用）；number_ticket 时为靓号券等级 1–4")
    feature: str | None = Field(default=None, description="功能标识（feature 用）")

    def payload(self) -> dict[str, Any]:
        return body(type=self.type, amount=self.amount, money=self.money, refId=self.ref_id, feature=self.feature)


def _rewards(items: list[Reward] | None) -> list[dict[str, Any]] | None:
    return None if items is None else [item.payload() for item in items]


# ── 目录与统计 ──

@server.tool(**READ("cardkey"))
async def catalog(app: AppRef) -> Any:
    """卡密表单目录：权益类型与取值形态、卡面字符集、有效期模式、领取对象、可选套餐与功能标识。生成批次前先看它。"""
    return await CATALOG(appkey=app)


@server.tool(**READ("cardkey"))
async def stats(
    app: AppRef,
    days: Annotated[int, Field(ge=1, le=365, description="核销趋势的天数")] = 30,
    tz: Annotated[str, Field(description="切日时区（IANA），留空用服务器默认")] = "",
) -> Any:
    """卡密统计：总量、可用、已提取、有效授权、面值（发出 / 已用）、按天核销趋势、来源分布与热门批次。"""
    return await STATS(appkey=app, query=q(days=days, tz=tz))


# ── 批次 ──

@server.tool(**READ("cardkey"))
async def batches(
    app: AppRef,
    keyword: Annotated[str, Field(description="按批次名称 / 备注 / 渠道搜索")] = "",
    kind: Annotated[Literal["", "login", "redeem"], Field(description="login 授权卡 / redeem 兑换卡，空为不限")] = "",
) -> Any:
    """卡密批次列表（随行带核销进度：总数、未用、已用、作废、过期、已提取）。"""
    return await BATCHES(appkey=app, query=q(keyword=keyword, kind=kind))


@server.tool(**READ("cardkey"))
async def batch(app: AppRef, batch_id: BatchID) -> Any:
    """单个批次的完整配置与进度。"""
    return await BATCH(appkey=app, batchId=batch_id)


@server.tool(**WRITE("cardkey"))
async def generate(
    app: AppRef,
    name: Annotated[str, Field(min_length=1, description="批次名称")],
    kind: Annotated[Kind, Field(description="login 授权卡（卡即登录凭证）/ redeem 兑换卡（给已登录用户发权益）")],
    count: Annotated[int | None, Field(ge=1, le=MAX_COUNT, description="生成张数（1-10000）；导入时不填")] = None,
    codes: Annotated[list[str] | None, Field(max_length=MAX_COUNT, description="导入已有卡面（给了就按导入处理，count 与卡面格式不生效）")] = None,
    rewards: Annotated[list[Reward] | None, Field(max_length=8, description="权益清单（兑换卡必填，授权卡可附带首登权益），最多 8 项")] = None,
    remark: Annotated[str | None, Field(description="备注")] = None,
    code_prefix: Annotated[str | None, Field(max_length=16, description="卡面前缀（大写字母与数字，如 VIP）")] = None,
    segments: Annotated[int | None, Field(ge=1, le=8, description="随机段数，默认 4")] = None,
    segment_length: Annotated[int | None, Field(ge=3, le=12, description="每段位数，默认 4")] = None,
    charset: Annotated[Literal["safe", "digits", "letters"] | None, Field(description="卡面字符集：safe 去掉易混字符（默认）/ digits 纯数字 / letters 纯字母")] = None,
    max_devices: Annotated[int | None, Field(ge=0, description="授权卡可绑设备数，0 为不限")] = None,
    max_uses: Annotated[int | None, Field(ge=1, description="每张可被多少人各用一次：1 为一卡一用，大于 1 为活动码")] = None,
    per_user_limit: Annotated[int | None, Field(ge=0, description="这一批每人限兑张数，0 为不限")] = None,
    audience: Annotated[Audience | None, Field(description="领取对象：all / vip 仅会员 / non_vip 仅非会员")] = None,
    starts_at: Annotated[str | None, Field(description="生效时间（ISO 8601），之前不能兑换 / 激活")] = None,
    validity_mode: Annotated[Literal["permanent", "fixed_until", "days_from_first_use"] | None, Field(description="有效期：permanent 永久 / fixed_until 统一到期（valid_until）/ days_from_first_use 首次使用起 validity_days 天")] = None,
    validity_days: Annotated[int | None, Field(ge=1, description="首次使用起有效天数（days_from_first_use 用）")] = None,
    valid_until: Annotated[str | None, Field(description="统一到期时间（ISO 8601，fixed_until 用）")] = None,
    self_unbind_limit: Annotated[int | None, Field(ge=0, description="每 30 天允许用户自助解绑设备的次数，0 为不允许")] = None,
    channel: Annotated[str | None, Field(description="渠道（如 淘宝店 / 代理商 A）")] = None,
    face_value: Annotated[str | None, Field(description="面值（十进制字符串，用于统计）")] = None,
) -> Any:
    """生成一批卡密，或给 codes 导入已有卡面。生成后的卡立即可用（批次默认启用），用 cardkey_extract 取卡面交给渠道。"""
    if not codes and not count:
        raise ToolError("生成需要 count（1-10000），导入需要 codes")
    return await BATCH_CREATE(appkey=app, body=body(
        name=name, kind=kind, remark=remark, count=None if codes else count, codes=codes or None, codePrefix=code_prefix,
        segments=segments, segmentLength=segment_length, charset=charset, rewards=_rewards(rewards), maxDevices=max_devices,
        maxUses=max_uses, perUserLimit=per_user_limit, audience=audience, startsAt=starts_at, validityMode=validity_mode,
        validityDays=validity_days, validUntil=valid_until, selfUnbindLimit=self_unbind_limit, channel=channel, faceValue=face_value,
    ))


_BATCH_FIELDS = ("name", "remark", "channel", "faceValue", "rewards", "perUserLimit", "audience", "startsAt", "validUntil", "selfUnbindLimit")


@server.tool(**WRITE("cardkey"))
async def update_batch(
    app: AppRef,
    batch_id: BatchID,
    name: Annotated[str | None, Field(min_length=1, description="批次名称")] = None,
    remark: Annotated[str | None, Field(description="备注")] = None,
    channel: Annotated[str | None, Field(description="渠道")] = None,
    face_value: Annotated[str | None, Field(description="面值（十进制字符串）")] = None,
    rewards: Annotated[list[Reward] | None, Field(max_length=8, description="新的权益清单（整体替换，只影响之后的核销）")] = None,
    per_user_limit: Annotated[int | None, Field(ge=0, description="每人限兑张数，0 为不限")] = None,
    audience: Annotated[Audience | None, Field(description="领取对象")] = None,
    starts_at: Annotated[str | None, Field(description="生效时间（ISO 8601）")] = None,
    valid_until: Annotated[str | None, Field(description="统一到期时间（ISO 8601，同步改这一批里还没用过的卡）")] = None,
    self_unbind_limit: Annotated[int | None, Field(ge=0, description="每 30 天自助解绑次数")] = None,
    clear: Annotated[list[Literal["starts_at", "valid_until"]] | None, Field(description="要清空的时间字段")] = None,
) -> Any:
    """修改批次（卡面格式、形态、有效期模式、单卡设备数不能改）。只传要改的字段：后端整体覆盖，这里先读批次再合并。"""
    current = await BATCH(appkey=app, batchId=batch_id) or {}
    merged = {key: current.get(key) for key in _BATCH_FIELDS if current.get(key) is not None}
    merged.update(body(
        name=name, remark=remark, channel=channel, faceValue=face_value, rewards=_rewards(rewards), perUserLimit=per_user_limit,
        audience=audience, startsAt=starts_at, validUntil=valid_until, selfUnbindLimit=self_unbind_limit,
    ))
    for field in clear or []:
        merged.pop({"starts_at": "startsAt", "valid_until": "validUntil"}[field], None)
    if isinstance(merged.get("faceValue"), (int, float)):
        merged["faceValue"] = str(merged["faceValue"])
    return await BATCH_UPDATE(appkey=app, batchId=batch_id, body=merged)


@server.tool(**WRITE("cardkey"))
async def append(
    app: AppRef,
    batch_id: BatchID,
    count: Annotated[int, Field(ge=1, le=MAX_COUNT, description="补发张数")],
) -> Any:
    """按原批次的前缀、段数、字符集再生成 N 张卡（批次总数随之增加）。导入的批次没有格式，不能补发。"""
    return await BATCH_APPEND(appkey=app, batchId=batch_id, body={"count": count})


@server.tool(**WRITE("cardkey"))
async def extract(
    app: AppRef,
    batch_id: BatchID,
    count: Annotated[int, Field(ge=1, le=MAX_COUNT, description="提取张数")],
    note: Annotated[str, Field(description="提取备注（交给了哪个渠道）")] = "",
) -> Any:
    """提卡：取出 N 张未使用、未提取、未过期的卡并标记为已提取，卡面随结果返回（交给渠道）。卡面是可兑换的凭证，注意保密。"""
    return await BATCH_EXTRACT(appkey=app, batchId=batch_id, body={"count": count, "note": note})


@server.tool(**WRITE("cardkey", idempotent=True))
async def set_batch_status(app: AppRef, batch_id: BatchID, enabled: Annotated[bool, Field(description="启用或停用整批")]) -> Any:
    """启用 / 停用整个批次（停用后整批不能兑换或登录，单卡状态不变，可随时恢复）。"""
    return await BATCH_STATUS(appkey=app, batchId=batch_id, body={"enabled": enabled})


@server.tool(**DESTRUCTIVE("cardkey"))
async def delete_batch(app: AppRef, batch_id: BatchID) -> Any:
    """删除批次及其全部卡（不可恢复）。只能删从没被用过的批次（否则 40982，请改为停用）。先向用户确认。"""
    return await BATCH_DELETE(appkey=app, batchId=batch_id)


# ── 单卡 ──

@server.tool(name="list", **READ("cardkey"))
async def list_cards(
    app: AppRef,
    batch_id: Annotated[int | None, Field(gt=0, description="只看某个批次")] = None,
    user_id: Annotated[int | None, Field(gt=0, description="只看绑定 / 兑换过的用户（管理端用户 ID）")] = None,
    status: Annotated[Literal["", "unused", "active", "used", "disabled", "expired"], Field(description="unused 未用 / active 授权中 / used 已用完 / disabled 已作废 / expired 已过期；空为不限")] = "",
    kind: Annotated[Literal["", "login", "redeem"], Field(description="卡密形态")] = "",
    keyword: Annotated[str, Field(description="按卡面 / 备注搜索")] = "",
    distributed: Annotated[Literal["", "yes", "no"], Field(description="yes 只看已提取 / no 只看未提取")] = "",
    page: Page = 1,
    limit: Annotated[int, Field(ge=1, le=100, description="每页条数")] = 20,
) -> Any:
    """卡密列表（卡面、状态、使用次数、绑定用户、到期时间、是否已提取）。"""
    return await CODES(appkey=app, query=q(
        batchId=batch_id, userId=user_id, status=status, kind=kind, keyword=keyword, distributed=distributed, page=page, limit=limit,
    ))


@server.tool(name="get", **READ("cardkey"))
async def get_card(
    app: AppRef,
    card_id: CardID,
    include_devices: Annotated[bool, Field(description="同时取这张卡绑定的设备")] = False,
) -> Any:
    """单张卡密的详情（所属批次、权益、绑定用户、核销记录），可附带绑定设备列表。"""
    result: dict[str, Any] = {"card": await CODE(appkey=app, cardId=card_id)}
    if include_devices:
        result["devices"] = await DEVICES(appkey=app, cardId=card_id)
    return result


@server.tool(**WRITE("cardkey"))
async def update(
    app: AppRef,
    card_id: CardID,
    remark: Annotated[str | None, Field(description="备注（传空串清空）")] = None,
    max_devices: Annotated[int | None, Field(ge=0, description="可绑设备数，0 为不限")] = None,
    expiry_action: Annotated[Literal["", "extend", "set", "permanent"], Field(description="到期时间调整：extend 延长 extend_days 天 / set 设为 expires_at / permanent 改为永久；空为不改")] = "",
    extend_days: Annotated[int | None, Field(ge=1, description="延长天数（extend 用，从原到期与现在较晚者起算）")] = None,
    expires_at: Annotated[str | None, Field(description="新的到期时间（ISO 8601，set 用）")] = None,
) -> Any:
    """调整单张卡：备注、可绑设备数、到期时间。只传要改的字段。"""
    if expiry_action == "extend" and not extend_days:
        raise ToolError("expiry_action=extend 需要 extend_days")
    if expiry_action == "set" and not expires_at:
        raise ToolError("expiry_action=set 需要 expires_at")
    if remark is None and max_devices is None and not expiry_action:
        raise ToolError("需要 remark / max_devices / expiry_action 之一")
    return await CODE_UPDATE(appkey=app, cardId=card_id, body=body(
        remark=remark, maxDevices=max_devices, expiryAction=expiry_action or None, extendDays=extend_days, expiresAt=expires_at,
    ))


@server.tool(**DESTRUCTIVE("cardkey"))
async def disable(app: AppRef, ids: CardIDs, reason: Annotated[str, Field(description="作废原因")] = "") -> Any:
    """作废选中的卡（作废后不能兑换 / 登录，已核销的卡不参与）。可用 cardkey_restore 恢复，但作废期间用户会被拒绝。先确认。"""
    return await CODES_DISABLE(appkey=app, body={"ids": ids, "reason": reason})


@server.tool(**WRITE("cardkey"))
async def restore(app: AppRef, ids: CardIDs, reason: Annotated[str, Field(description="恢复原因")] = "") -> Any:
    """恢复被作废的卡，使其重新可用。"""
    return await CODES_RESTORE(appkey=app, body={"ids": ids, "reason": reason})


@server.tool(name="delete", **DESTRUCTIVE("cardkey"))
async def delete_cards(app: AppRef, ids: CardIDs, reason: Annotated[str, Field(description="删除原因")] = "") -> Any:
    """删除选中的卡（不可恢复）。只删从没被用过的卡，用过的自动跳过（返回 skipped）。先向用户确认。"""
    return await CODES_DELETE(appkey=app, body={"ids": ids, "reason": reason})


@server.tool(**WRITE("cardkey", idempotent=True))
async def distribute(
    app: AppRef,
    ids: CardIDs,
    distributed: Annotated[bool, Field(description="true 标记为已提取（交给渠道）/ false 取消标记")] = True,
    note: Annotated[str, Field(description="备注")] = "",
) -> Any:
    """手动标记 / 取消选中卡的「已提取」状态（不改变卡能否使用）。"""
    return await CODES_DISTRIBUTE(appkey=app, body={"ids": ids, "distributed": distributed, "note": note})


@server.tool(**DESTRUCTIVE("cardkey"))
async def unbind_devices(
    app: AppRef,
    card_id: CardID,
    device_id: Annotated[str, Field(description="要解绑的设备标识；留空则清空这张卡的全部设备")] = "",
) -> Any:
    """解绑授权卡上的一台设备，或清空全部设备，把名额还回去（被解绑的设备下次登录需要重新占位）。先向用户确认。"""
    if device_id.strip():
        return await DEVICE_UNBIND(appkey=app, cardId=card_id, deviceId=device_id.strip())
    return await DEVICES_RESET(appkey=app, cardId=card_id)


# ── 核销 ──

@server.tool(**READ("cardkey"))
async def redemptions(
    app: AppRef,
    batch_id: Annotated[int | None, Field(gt=0, description="只看某个批次")] = None,
    card_id: Annotated[int | None, Field(gt=0, description="只看某张卡")] = None,
    user_id: Annotated[int | None, Field(gt=0, description="只看某个用户")] = None,
    source: Annotated[Literal["", "redeem", "login", "admin"], Field(description="redeem 用户兑换 / login 卡密登录 / admin 管理员代兑；空为不限")] = "",
    keyword: Annotated[str, Field(description="按卡面 / 账号搜索")] = "",
    from_time: IsoTime = "",
    to_time: IsoTime = "",
    page: Page = 1,
    limit: Annotated[int, Field(ge=1, le=100, description="每页条数")] = 20,
) -> Any:
    """核销记录（谁在何时用哪张卡领了什么，含发放结果快照）。"""
    return await REDEMPTIONS(appkey=app, query=q(
        batchId=batch_id, cardId=card_id, userId=user_id, source=source, keyword=keyword,
        **{"from": from_time, "to": to_time}, page=page, limit=limit,
    ))


@server.tool(**WRITE("cardkey"))
async def redeem(
    app: AppRef,
    code: Annotated[str, Field(min_length=1, description="卡面")],
    user_id: Annotated[int | None, Field(gt=0, description="替哪个用户兑换（管理端用户 ID），与 account 二选一")] = None,
    account: Annotated[str, Field(description="替哪个账号兑换，与 user_id 二选一")] = "",
) -> Any:
    """管理员代兑：替一个用户核销一张卡并发放权益（来源记为 admin，发站内信）。核销后不能撤回，先确认卡面与用户。"""
    require_one(user_id=user_id, account=account)
    return await REDEEM(appkey=app, body=body(code=code.strip(), userId=user_id, account=account or None))
