"""积分与经验：用户积分 / 经验概况与流水、调整积分、调整经验（可扣减）、批量调整、冲正、应用积分统计。"""

from __future__ import annotations

import re
from typing import Annotated, Any, Literal

from fastmcp import FastMCP
from fastmcp.exceptions import ToolError
from pydantic import Field

from ..core import DESTRUCTIVE, GET, POST, READ, WRITE, AppRef, IsoTime, Page, RequestID, UserID, app_id_of, body, q

NAMESPACE = "points"
server = FastMCP("points")

SUMMARY = GET("/api/admin/apps/:appkey/users/:userId/points/:kind/summary")
LEDGER = GET("/api/admin/apps/:appkey/users/:userId/points/:kind/transactions")
REVERSE = {
    "integral": POST("/api/admin/apps/:appkey/users/:userId/points/integral/transactions/:transactionNo/reverse"),
    "experience": POST("/api/admin/apps/:appkey/users/:userId/points/experience/transactions/:transactionNo/reverse"),
}
ADJUST_INTEGRAL = POST("/api/app/points/adjust-integral")
ADJUST_EXPERIENCE = POST("/api/app/points/adjust-experience")
BATCH_ADJUST = POST("/api/app/points/batch-adjust")
STATS = POST("/api/app/points/stats")

# 与后端 internal/domain/points 的边界一致，提前拦下必然失败的请求。
MAX_ADJUST_AMOUNT = 100_000_000
MAX_BATCH_USERS = 1000
MAX_REASON_RUNES = 200
_REQUEST_ID = re.compile(r"[A-Za-z0-9_-]{8,64}")

Kind = Annotated[Literal["integral", "experience"], Field(description="integral 为积分，experience 为经验")]
LevelMode = Annotated[
    Literal["keep", "recalc"],
    Field(description="扣减经验时的等级处理：keep 保留现有等级只扣经验；recalc 按扣减后的经验重新判定，可能降级（不低于锁定的保底，封顶照旧）。已发的等级奖励都不收回"),
]
AdjustReason = Annotated[str, Field(description="调整原因（记入流水，用户可见），最多 200 字；留空由后端写默认文案", max_length=MAX_REASON_RUNES)]


def _request_id(value: str) -> str | None:
    value = (value or "").strip()
    if not value:
        return None
    if not _REQUEST_ID.fullmatch(value):
        raise ToolError("request_id 只能是 8-64 位的字母、数字、连字符或下划线（UUID 即可）")
    return value


def _check_amount(amount: int, *, positive: bool) -> None:
    if amount == 0:
        raise ToolError("调整数量不能为 0")
    if positive and amount < 0:
        raise ToolError("数量必须大于 0")
    if abs(amount) > MAX_ADJUST_AMOUNT:
        raise ToolError(f"单次调整数量的绝对值不能超过 {MAX_ADJUST_AMOUNT:,}")


@server.tool(**READ("points"))
async def summary(
    app: AppRef,
    user_id: UserID,
    kind: Kind = "integral",
    days: Annotated[int, Field(ge=0, le=180, description="每日走势统计窗口（天），0 只要汇总")] = 30,
    tz: Annotated[str, Field(description="切日时区（IANA，如 Asia/Shanghai），留空用服务器默认时区")] = "",
) -> Any:
    """某用户的积分或经验概况：当前余额、累计收支（含管理员调整部分）、分类聚合与每日走势。需要 app:user:read。"""
    return await SUMMARY(appkey=app, userId=user_id, kind=kind, query=q(days=days or None, tz=tz))


@server.tool(**READ("points"))
async def transactions(
    app: AppRef,
    user_id: UserID,
    kind: Kind = "integral",
    direction: Annotated[Literal["", "in", "out"], Field(description="in 收入 / out 支出，空为不限")] = "",
    category: Annotated[str, Field(description="流水分类（如 signin、admin_adjust、points_store、invite、card_key），留空不限")] = "",
    keyword: Annotated[str, Field(description="按流水号 / 标题 / 备注搜索")] = "",
    from_time: IsoTime = "",
    to_time: IsoTime = "",
    page: Page = 1,
    limit: Annotated[int, Field(ge=1, le=100, description="每页条数，最多 100")] = 20,
) -> Any:
    """某用户的积分或经验流水，可按方向、分类、关键词与时间区间（RFC 3339，左闭右开）筛选。需要 app:user:read。"""
    return await LEDGER(appkey=app, userId=user_id, kind=kind, query=q(
        direction=direction, category=category, keyword=keyword, **{"from": from_time, "to": to_time}, page=page, limit=limit,
    ))


@server.tool(**DESTRUCTIVE("points"))
async def adjust_integral(
    app: AppRef,
    user_id: UserID,
    amount: Annotated[int, Field(description="调整数量：正数增加、负数扣减；绝对值不超过 1 亿")],
    reason: AdjustReason = "",
    request_id: RequestID = "",
) -> Any:
    """给单个用户增加或扣减积分（立即生效，写入积分流水与审计）。

    扣减会让余额变少且余额不足时整笔拒绝；误操作可用 points_reverse 冲正这一笔。
    执行前先向用户确认用户、数量与方向。建议带 request_id，重试时不会重复入账（重放返回首次结果）。需要 points:write。
    """
    _check_amount(amount, positive=False)
    return await ADJUST_INTEGRAL(body=body(
        appid=await app_id_of(app), user_id=user_id, amount=amount, reason=reason or None, request_id=_request_id(request_id),
    ))


@server.tool(**WRITE("points"))
async def add_experience(
    app: AppRef,
    user_id: UserID,
    amount: Annotated[int, Field(gt=0, le=MAX_ADJUST_AMOUNT, description="增加的经验值（经验只增不减）")],
    reason: AdjustReason = "",
    request_id: RequestID = "",
) -> Any:
    """给单个用户增加经验（可能触发升级）。要扣减经验用 points_adjust_experience。执行前先确认。需要 points:write。"""
    _check_amount(amount, positive=True)
    return await ADJUST_EXPERIENCE(body=body(
        appid=await app_id_of(app), user_id=user_id, amount=amount, reason=reason or None, request_id=_request_id(request_id),
    ))


@server.tool(**DESTRUCTIVE("points"))
async def adjust_experience(
    app: AppRef,
    user_id: UserID,
    amount: Annotated[int, Field(description="调整数量：正数增加、负数扣减；绝对值不超过 1 亿")],
    level_mode: LevelMode = "keep",
    reason: AdjustReason = "",
    request_id: RequestID = "",
) -> Any:
    """给单个用户增加或扣减经验（立即生效，写入经验流水与审计）。

    扣减时经验不足整笔拒绝；level_mode 决定等级怎么处理（recalc 可能让用户降级）。误操作可用 points_reverse(kind=experience) 冲正。
    执行前先向用户确认用户、数量、方向与等级处理。建议带 request_id，重试时不会重复生效。需要 points:write。
    """
    _check_amount(amount, positive=False)
    return await ADJUST_EXPERIENCE(body=body(
        appid=await app_id_of(app), user_id=user_id, amount=amount, reason=reason or None, request_id=_request_id(request_id),
        level_mode=level_mode if amount < 0 else None,
    ))


@server.tool(**DESTRUCTIVE("points"))
async def batch_adjust(
    app: AppRef,
    user_ids: Annotated[list[Annotated[int, Field(gt=0)]], Field(min_length=1, max_length=MAX_BATCH_USERS, description="用户 ID 列表（管理端主键），最多 1000 个，重复的自动去重")],
    amount: Annotated[int, Field(gt=0, le=MAX_ADJUST_AMOUNT, description="每人调整的数量（正数），方向由 operation 决定")],
    operation: Annotated[Literal["add", "consume"], Field(description="add 增加 / consume 扣减")] = "add",
    kind: Kind = "integral",
    level_mode: LevelMode = "keep",
    reason: AdjustReason = "",
    request_id: RequestID = "",
) -> Any:
    """批量给多个用户增加或扣减同样数量的积分或经验。返回逐人结果与失败清单（余额不足等不影响其他人）。

    经验逐人在各自的事务里执行，扣减时 level_mode 决定等级处理。
    影响面大：执行前先向用户复述人数、数量、类型与方向并取得确认。建议带 request_id 防止重复提交。需要 points:write。
    """
    return await BATCH_ADJUST(body=body(
        appid=await app_id_of(app), user_ids=user_ids, amount=amount, operation_type=operation,
        kind=kind if kind != "integral" else None,
        level_mode=level_mode if kind == "experience" and operation == "consume" else None,
        reason=reason or None, request_id=_request_id(request_id),
    ))


@server.tool(**DESTRUCTIVE("points"))
async def reverse(
    app: AppRef,
    user_id: UserID,
    transaction_no: Annotated[str, Field(min_length=1, description="要冲正的流水号（points_transactions 里的 transactionNo）")],
    kind: Kind = "integral",
    level_mode: LevelMode = "keep",
    reason: Annotated[str, Field(description="冲正原因，留空写「冲正 <流水号>」", max_length=MAX_REASON_RUNES)] = "",
) -> Any:
    """冲正一笔积分或经验流水：按原数量反向调整一次（原来加的扣回、扣的退回）。

    同一笔只能冲正一次（重复提交返回首次结果），冲正产生的流水不能再冲正。
    扣回时若余额已不足会被拒绝；经验扣回时 level_mode 决定等级处理。执行前先确认流水号与用户。需要 app:user:write。
    """
    return await REVERSE[kind](appkey=app, userId=user_id, transactionNo=transaction_no.strip(), body=body(
        reason=reason or None, levelMode=level_mode if kind == "experience" else None,
    ))


@server.tool(**READ("points"))
async def stats(
    app: AppRef,
    days: Annotated[int, Field(ge=1, le=365, description="统计最近 N 天（含今天）")] = 30,
) -> Any:
    """应用级积分与经验统计：用户与活跃概览、按分类的发放 / 消耗、积分与经验排行前列、每日趋势。需要 points:read。"""
    return await STATS(body={"appid": await app_id_of(app), "time_range": days})
