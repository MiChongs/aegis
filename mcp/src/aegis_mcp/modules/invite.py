"""邀请：开关与配置、邀请码（个人码 / 活动码）管理、邀请关系与邀请人奖励的发放 / 取消、统计与目录。"""

from __future__ import annotations

from typing import Annotated, Any, Literal

from fastmcp import FastMCP
from fastmcp.exceptions import ToolError
from pydantic import BaseModel, Field

from ..core import DELETE, DESTRUCTIVE, GET, POST, PUT, READ, WRITE, AppRef, Page, body, q

NAMESPACE = "invite"
server = FastMCP("invite")

B = "/api/admin/apps/:appkey/invite"
CATALOG = GET(f"{B}/catalog")
SETTINGS = GET(f"{B}/settings")
SETTINGS_SAVE = PUT(f"{B}/settings")
CODES = GET(f"{B}/codes")
CODES_CREATE = POST(f"{B}/codes")
CODE_UPDATE = PUT(f"{B}/codes/:codeId")
CODE_STATUS = PUT(f"{B}/codes/:codeId/status")
CODE_DELETE = DELETE(f"{B}/codes/:codeId")
RELATIONS = GET(f"{B}/relations")
RELATION_SETTLE = POST(f"{B}/relations/:relationId/settle")
RELATION_CANCEL = POST(f"{B}/relations/:relationId/cancel")
STATS = GET(f"{B}/stats")

CodeID = Annotated[int, Field(gt=0, description="邀请码 ID（invite_codes 结果里的 id，不是码本身）")]
RelationID = Annotated[int, Field(gt=0, description="邀请关系 ID（invite_relations 结果里的 id）")]


class Reward(BaseModel):
    """一项邀请 / 排行榜奖励（与积分商城同一套账本）。取值字段由类型决定（见 invite_catalog 的 rewards）。"""

    type: Literal["points", "vip_plan", "vip_days", "feature", "experience", "balance", "lottery_draws",
                  "number_change", "number_ticket", "number_draw", "coupon"] = Field(
        description="points 积分（amount）/ vip_plan 会员套餐（ref_id）/ vip_days 会员天数（amount）/ feature 功能解锁（tag + amount 天，0 为永久）/ "
                    "experience 经验（amount）/ balance 余额（money，单项不超过 100000）/ lottery_draws 抽奖次数（amount）/ "
                    "number_change 改号卡（amount 张）/ number_ticket 靓号券（amount 张 + ref_id 等级 1 精品 2 稀有 3 传说 4 至尊）/ number_draw 摇号次数（amount 次）/ coupon 优惠券（ref_id 券编号 + amount 张，1–20）")
    amount: int | None = Field(default=None, ge=0, description="数量：积分 / 天数 / 经验 / 次数")
    money: str | None = Field(default=None, description="余额金额（十进制字符串，如 \"2.00\"）")
    ref_id: int | None = Field(default=None, gt=0, description="会员套餐 ID（vip_plan 用，不能选试用套餐）；number_ticket 时为靓号券等级 1–4")
    tag: str | None = Field(default=None, description="功能标识（feature 用）")

    def payload(self) -> dict[str, Any]:
        return body(type=self.type, amount=self.amount, money=self.money, refId=self.ref_id, tag=self.tag)


class Milestone(BaseModel):
    """邀请人累计成功邀请满 count 人时的一档奖励（每档只发一次）。"""

    count: int = Field(ge=1, le=100_000, description="累计成功邀请人数")
    title: str = Field(default="", max_length=16, description="档位名称")
    rewards: list[Reward] = Field(min_length=1, max_length=6, description="这一档的奖励")

    def payload(self) -> dict[str, Any]:
        return {"count": self.count, "title": self.title, "rewards": [item.payload() for item in self.rewards]}


def rewards_payload(items: list[Reward] | None) -> list[dict[str, Any]] | None:
    return None if items is None else [item.payload() for item in items]


_SETTINGS_FIELDS = (
    "enabled", "registerField", "fieldLabel", "fieldPlaceholder", "bindWindowHours", "codeLength", "codeCharset", "codePrefix",
    "allowCustomCode", "inviterAudience", "inviterMinLevel", "inviteeRewards", "inviterRewards", "milestones", "inviterTrigger",
    "triggerSignInDays", "inviterDailyLimit", "inviterTotalLimit", "notifyInviter", "deviceLimit", "ipDailyLimit",
    "rulesText", "shareTemplate", "landingUrl",
)


# ── 目录与配置 ──

@server.tool(**READ("invite"))
async def catalog(app: AppRef) -> Any:
    """邀请表单目录：可发的奖励类型与取值形态、各枚举（注册字段、码字符集、邀请人资格、发放时机）、默认配置与上限。"""
    return await CATALOG(appkey=app)


@server.tool(**READ("invite"))
async def settings(app: AppRef) -> Any:
    """应用的邀请开关与完整配置（注册表单、个人码格式、邀请人资格、双方奖励、里程碑、发放时机与上限、防刷、文案）。"""
    return await SETTINGS(appkey=app)


@server.tool(**WRITE("invite"))
async def update_settings(
    app: AppRef,
    enabled: Annotated[bool | None, Field(description="开启 / 关闭邀请")] = None,
    register_field: Annotated[Literal["hidden", "optional", "required"] | None, Field(description="注册页邀请码字段：hidden 不出现（注册后可在时限内补填）/ optional 选填 / required 必填")] = None,
    field_label: Annotated[str | None, Field(max_length=16, description="字段标签")] = None,
    field_placeholder: Annotated[str | None, Field(max_length=32, description="字段占位文字")] = None,
    bind_window_hours: Annotated[int | None, Field(ge=0, le=720, description="注册后多少小时内可补填，0 为只能注册时填")] = None,
    code_length: Annotated[int | None, Field(ge=4, le=16, description="个人码长度（纯数字至少 6）")] = None,
    code_charset: Annotated[Literal["alnum", "digits", "letters"] | None, Field(description="个人码字符集")] = None,
    code_prefix: Annotated[str | None, Field(max_length=6, description="个人码前缀")] = None,
    allow_custom_code: Annotated[bool | None, Field(description="是否允许用户自定义个人码")] = None,
    inviter_audience: Annotated[Literal["all", "vip"] | None, Field(description="谁的个人码可以被填写：all 所有人 / vip 有效会员")] = None,
    inviter_min_level: Annotated[int | None, Field(ge=0, le=1000, description="邀请人等级门槛")] = None,
    invitee_rewards: Annotated[list[Reward] | None, Field(max_length=6, description="被邀请人奖励（整体替换）")] = None,
    inviter_rewards: Annotated[list[Reward] | None, Field(max_length=6, description="邀请人每次成功邀请的奖励（整体替换）")] = None,
    milestones: Annotated[list[Milestone] | None, Field(max_length=10, description="邀请人里程碑奖励（整体替换）")] = None,
    inviter_trigger: Annotated[Literal["bind", "signin"] | None, Field(description="邀请人奖励发放时机：bind 填码即发 / signin 被邀请人累计签到满 N 天")] = None,
    trigger_signin_days: Annotated[int | None, Field(ge=1, le=365, description="signin 时机要求的累计签到天数")] = None,
    inviter_daily_limit: Annotated[int | None, Field(ge=0, description="邀请人每天最多拿几次奖励，0 不限")] = None,
    inviter_total_limit: Annotated[int | None, Field(ge=0, description="邀请人累计最多拿几次奖励，0 不限")] = None,
    notify_inviter: Annotated[bool | None, Field(description="邀请人拿到奖励时发站内信")] = None,
    device_limit: Annotated[int | None, Field(ge=0, le=100, description="同一设备最多填写几次（全应用），0 不限")] = None,
    ip_daily_limit: Annotated[int | None, Field(ge=0, le=10_000, description="同一 IP 每天最多填写几次，0 不限")] = None,
    rules_text: Annotated[str | None, Field(max_length=2000, description="活动规则文案")] = None,
    share_template: Annotated[str | None, Field(max_length=300, description="分享文案（可用 {code} {link} {app}）")] = None,
    landing_url: Annotated[str | None, Field(max_length=512, description="落地页地址（可用 {code}），http / https")] = None,
) -> Any:
    """修改邀请开关与配置。只传要改的字段（后端整体覆盖，这里先读后合并）。奖励改动只影响之后的邀请。"""
    current = await SETTINGS(appkey=app) or {}
    merged = {key: current.get(key) for key in _SETTINGS_FIELDS if current.get(key) is not None}
    merged.update(body(
        enabled=enabled, registerField=register_field, fieldLabel=field_label, fieldPlaceholder=field_placeholder,
        bindWindowHours=bind_window_hours, codeLength=code_length, codeCharset=code_charset, codePrefix=code_prefix,
        allowCustomCode=allow_custom_code, inviterAudience=inviter_audience, inviterMinLevel=inviter_min_level,
        inviteeRewards=rewards_payload(invitee_rewards), inviterRewards=rewards_payload(inviter_rewards),
        milestones=None if milestones is None else [item.payload() for item in milestones],
        inviterTrigger=inviter_trigger, triggerSignInDays=trigger_signin_days, inviterDailyLimit=inviter_daily_limit,
        inviterTotalLimit=inviter_total_limit, notifyInviter=notify_inviter, deviceLimit=device_limit, ipDailyLimit=ip_daily_limit,
        rulesText=rules_text, shareTemplate=share_template, landingUrl=landing_url,
    ))
    merged.setdefault("enabled", False)
    return await SETTINGS_SAVE(appkey=app, body=merged)


# ── 邀请码 ──

@server.tool(**READ("invite"))
async def codes(
    app: AppRef,
    kind: Annotated[Literal["", "personal", "campaign"], Field(description="personal 个人码 / campaign 活动码，空为不限")] = "",
    status: Annotated[Literal["", "active", "disabled"], Field(description="状态，空为不限")] = "",
    owner_id: Annotated[int | None, Field(gt=0, description="只看某个推广人 / 码主（管理端用户 ID）")] = None,
    keyword: Annotated[str, Field(description="按码 / 活动名称 / 码主账号搜索")] = "",
    page: Page = 1,
    limit: Annotated[int, Field(ge=1, le=100, description="每页条数")] = 20,
) -> Any:
    """邀请码列表（个人码与活动码，含使用次数、码主、奖励与有效期）。"""
    return await CODES(appkey=app, query=q(kind=kind, status=status, ownerId=owner_id, keyword=keyword, page=page, limit=limit))


@server.tool(**WRITE("invite"))
async def create_codes(
    app: AppRef,
    title: Annotated[str, Field(min_length=1, max_length=32, description="活动名称")],
    code: Annotated[str, Field(description="指定一个码（4-16 位）；留空按应用格式批量生成 count 个")] = "",
    count: Annotated[int | None, Field(ge=1, le=500, description="批量生成个数（code 为空时用）")] = None,
    prefix: Annotated[str, Field(max_length=6, description="批量生成时另给的前缀")] = "",
    owner_user_id: Annotated[int | None, Field(gt=0, description="推广人（拿邀请人奖励），不传为纯渠道码")] = None,
    note: Annotated[str, Field(max_length=128, description="备注")] = "",
    rewards: Annotated[list[Reward] | None, Field(max_length=6, description="被邀请人奖励：不传为跟随应用默认；传空数组为这个码不发奖励")] = None,
    max_uses: Annotated[int | None, Field(ge=0, description="可使用次数，不传或 0 为不限")] = None,
    starts_at: Annotated[str | None, Field(description="开始时间（ISO 8601）")] = None,
    ends_at: Annotated[str | None, Field(description="结束时间（ISO 8601）")] = None,
    status: Annotated[Literal["active", "disabled"], Field(description="创建后的状态")] = "active",
) -> Any:
    """创建活动码：指定一个，或按应用的码格式批量生成。返回创建出的码。"""
    if not code.strip() and not count:
        raise ToolError("需要 code（指定一个码）或 count（批量生成个数）")
    return await CODES_CREATE(appkey=app, body=body(
        code=code.strip() or None, count=None if code.strip() else count, prefix=prefix or None, ownerUserId=owner_user_id,
        title=title, note=note or None, rewards=rewards_payload(rewards), maxUses=max_uses, startsAt=starts_at, endsAt=ends_at, status=status,
    ))


_CODE_FIELDS = ("ownerUserId", "title", "note", "rewards", "maxUses", "startsAt", "endsAt", "status")


async def _find_code(app: str, code_id: int) -> dict[str, Any]:
    for page in range(1, 51):
        data = await CODES(appkey=app, query={"kind": "campaign", "page": page, "limit": 100}) or {}
        items = data.get("items") or []
        for item in items:
            if int(item.get("id") or 0) == code_id:
                return item
        if len(items) < 100:
            break
    raise ToolError(f"找不到 ID 为 {code_id} 的活动码（个人码不能用这个工具修改），请用 invite_codes 核对")


@server.tool(**WRITE("invite"))
async def update_code(
    app: AppRef,
    code_id: CodeID,
    title: Annotated[str | None, Field(min_length=1, max_length=32, description="活动名称")] = None,
    owner_user_id: Annotated[int | None, Field(gt=0, description="推广人")] = None,
    note: Annotated[str | None, Field(max_length=128, description="备注")] = None,
    rewards: Annotated[list[Reward] | None, Field(max_length=6, description="被邀请人奖励（整体替换；传空数组为不发）")] = None,
    max_uses: Annotated[int | None, Field(ge=0, description="可使用次数，0 为不限")] = None,
    starts_at: Annotated[str | None, Field(description="开始时间（ISO 8601）")] = None,
    ends_at: Annotated[str | None, Field(description="结束时间（ISO 8601）")] = None,
    status: Annotated[Literal["active", "disabled"] | None, Field(description="状态")] = None,
    clear: Annotated[list[Literal["owner_user_id", "rewards", "max_uses", "starts_at", "ends_at"]] | None,
                     Field(description="要清空的字段：owner_user_id（改为纯渠道码）、rewards（改为跟随应用默认）、max_uses（不限次）、starts_at、ends_at")] = None,
) -> Any:
    """修改活动码（码本身不改）。只传要改的字段：后端整体覆盖，这里先查出现有配置再合并。"""
    current = await _find_code(app, code_id)
    merged = {key: current.get(key) for key in _CODE_FIELDS if current.get(key) is not None}
    merged.update(body(
        title=title, ownerUserId=owner_user_id, note=note, rewards=rewards_payload(rewards), maxUses=max_uses,
        startsAt=starts_at, endsAt=ends_at, status=status,
    ))
    names = {"owner_user_id": "ownerUserId", "rewards": "rewards", "max_uses": "maxUses", "starts_at": "startsAt", "ends_at": "endsAt"}
    for field in clear or []:
        merged.pop(names[field], None)
    return await CODE_UPDATE(appkey=app, codeId=code_id, body=merged)


@server.tool(**WRITE("invite", idempotent=True))
async def set_code_status(app: AppRef, code_id: CodeID, active: Annotated[bool, Field(description="true 启用 / false 停用")]) -> Any:
    """启用 / 停用一个邀请码（个人码也可以停用；停用后不能再被填写，已成立的邀请不受影响）。"""
    return await CODE_STATUS(appkey=app, codeId=code_id, body={"active": active})


@server.tool(**DESTRUCTIVE("invite"))
async def delete_code(app: AppRef, code_id: CodeID) -> Any:
    """删除活动码（不可恢复；已成立的邀请关系保留码的快照）。只想暂停请用 invite_set_code_status。先向用户确认。"""
    return await CODE_DELETE(appkey=app, codeId=code_id)


# ── 邀请关系 ──

@server.tool(**READ("invite"))
async def relations(
    app: AppRef,
    inviter_id: Annotated[int | None, Field(gt=0, description="邀请人（管理端用户 ID）")] = None,
    invitee_id: Annotated[int | None, Field(gt=0, description="被邀请人")] = None,
    code_id: Annotated[int | None, Field(gt=0, description="通过哪个码")] = None,
    status: Annotated[Literal["", "none", "pending", "granted", "limited", "cancelled"], Field(description="邀请人奖励状态：none 无邀请人 / pending 等条件达成 / granted 已发 / limited 达上限未发 / cancelled 已取消；空为不限")] = "",
    source: Annotated[Literal["", "register", "bind"], Field(description="register 注册时填写 / bind 注册后补填")] = "",
    keyword: Annotated[str, Field(description="按账号 / 码搜索")] = "",
    page: Page = 1,
    limit: Annotated[int, Field(ge=1, le=100, description="每页条数")] = 20,
) -> Any:
    """邀请关系（谁邀请了谁、通过哪个码、双方奖励的发放情况）。"""
    return await RELATIONS(appkey=app, query=q(
        inviterId=inviter_id, inviteeId=invitee_id, codeId=code_id, status=status, source=source, keyword=keyword, page=page, limit=limit,
    ))


@server.tool(**WRITE("invite"))
async def settle(app: AppRef, relation_id: RelationID) -> Any:
    """立即发放一条等待中（pending）的邀请人奖励，不看发放条件与上限。发出后不能收回，先向用户确认。"""
    return await RELATION_SETTLE(appkey=app, relationId=relation_id)


@server.tool(**DESTRUCTIVE("invite"))
async def cancel(app: AppRef, relation_id: RelationID) -> Any:
    """取消一条等待中（pending）的邀请人奖励（之后不会再发；被邀请人已到账的奖励不收回）。不可撤销，先向用户确认。"""
    return await RELATION_CANCEL(appkey=app, relationId=relation_id)


@server.tool(**READ("invite"))
async def stats(app: AppRef, days: Annotated[int, Field(ge=1, le=90, description="按天趋势的天数")] = 14) -> Any:
    """邀请概览：总邀请数、成功数、按天趋势、邀请最多的人与用得最多的活动码。"""
    return await STATS(appkey=app, query={"days": days})
