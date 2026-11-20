"""抽奖：表单目录、活动与奖品（新建、修改、启停、删除）、策略模拟、计算引擎（精确分析、奖盘求解、大规模推演）、统计与出奖审计、抽奖记录与人工奖品发放、抽奖次数、至尊加成。

权限：读需要 app:read（精确分析与奖盘求解只算不写，也算读），其余（含模拟、发起推演）需要 app:write。
"""

from __future__ import annotations

from typing import Annotated, Any, Literal

from fastmcp import FastMCP
from fastmcp.exceptions import ToolError
from pydantic import Field

from ..core import DELETE, DESTRUCTIVE, GET, POST, PUT, READ, WRITE, AppRef, IsoTime, Limit, Page, body, q

NAMESPACE = "lottery"
server = FastMCP("lottery")

CATALOG = GET("/api/admin/apps/:appkey/lottery/catalog")
SETTINGS = GET("/api/admin/apps/:appkey/lottery/settings")
SETTINGS_SAVE = PUT("/api/admin/apps/:appkey/lottery/settings")
ACTIVITIES = GET("/api/admin/apps/:appkey/lottery/activities")
ACTIVITY = GET("/api/admin/apps/:appkey/lottery/activities/:activityId")
ACTIVITY_CREATE = POST("/api/admin/apps/:appkey/lottery/activities")
ACTIVITY_UPDATE = PUT("/api/admin/apps/:appkey/lottery/activities/:activityId")
ACTIVITY_STATUS = PUT("/api/admin/apps/:appkey/lottery/activities/:activityId/status")
ACTIVITY_DELETE = DELETE("/api/admin/apps/:appkey/lottery/activities/:activityId")
ACTIVITY_STATS = GET("/api/admin/apps/:appkey/lottery/activities/:activityId/stats")
ACTIVITY_AUDIT = GET("/api/admin/apps/:appkey/lottery/activities/:activityId/audit")
SIMULATE = POST("/api/admin/apps/:appkey/lottery/simulate")
ANALYZE = POST("/api/admin/apps/:appkey/lottery/analyze")
SOLVE = POST("/api/admin/apps/:appkey/lottery/solve")
COMPUTE_JOBS = GET("/api/admin/apps/:appkey/lottery/compute/jobs")
COMPUTE_JOB = GET("/api/admin/apps/:appkey/lottery/compute/jobs/:jobId")
COMPUTE_CREATE = POST("/api/admin/apps/:appkey/lottery/compute/jobs")
COMPUTE_CANCEL = POST("/api/admin/apps/:appkey/lottery/compute/jobs/:jobId/cancel")
COMPUTE_DELETE = DELETE("/api/admin/apps/:appkey/lottery/compute/jobs/:jobId")
DRAWS = GET("/api/admin/apps/:appkey/lottery/draws")
DRAW_FULFILL = POST("/api/admin/apps/:appkey/lottery/draws/:drawId/fulfill")
TICKETS = GET("/api/admin/apps/:appkey/lottery/tickets")
TICKET_ACCOUNTS = GET("/api/admin/apps/:appkey/lottery/tickets/accounts")
TICKET_LEDGER = GET("/api/admin/apps/:appkey/lottery/tickets/ledger")
TICKET_ACCOUNT = GET("/api/admin/apps/:appkey/lottery/tickets/users/:userId")
TICKET_ADJUST = POST("/api/admin/apps/:appkey/lottery/tickets/users/:userId/adjust")

ActivityID = Annotated[int, Field(gt=0, description="抽奖活动 ID（lottery_activities 结果里的 id）")]
JobID = Annotated[int, Field(gt=0, description="推演任务 ID（lottery_compute_run / lottery_compute_jobs 结果里的 id）")]
PERSONAS_HELP = (
    "人群列表（最多 6 种）。每项 {key?, name?, share?（占比，任意正数，会归一；只对推演有用）, drawsPerDay?（这类人每天抽几次，"
    "只对推演有用）, member?, lifetime?（永久会员，按已拥有奖盘里全部会员与功能奖品算）, adFree?, signedToday?, "
    "onlineMinutesToday?, tiers?（所在会员档位 ID）}。不传为只有普通用户"
)
Days = Annotated[int, Field(ge=1, le=90, description="统计最近 N 天")]
Status = Literal["draft", "active", "paused", "ended"]

PRIZES_HELP = (
    "奖品列表（整体替换，九宫格恰好 8 个，转盘 2–12 个）。每项 {id?（带 id 为更新已有奖品、保留已抽中数量，不带为新建）, name, "
    "reward: {type, amount?, refId?, tag?, text?}, imageUrl?, quantity（总库存，-1 不限）, weight（权重，0 不参与常规抽取）, "
    "position（奖盘位置，九宫格外圈顺时针 0–7）, fallback?（库存抢光时改发它，须不限库存）, dailyLimit?, userLimit?, userDailyLimit?, "
    "value?（积分当量）, lifetimeReward?（已拥有时改发，积分 / 经验 / 抽奖次数）}。reward.type：none 谢谢参与 / points 积分 / "
    "vip_plan（refId=套餐 ID）/ vip_days（amount=天）/ vip_lifetime / feature（tag=功能，amount=天，0 永久）/ "
    "feature_time（tag=功能，amount=分钟）/ ad_free（amount=天）/ experience / lottery_draws / manual（text=人工发放内容）/ "
    "number_change 改号卡（amount=张）/ number_ticket 靓号券（amount=张，refId=等级 1 精品 2 稀有 3 传说 4 至尊）/ number_draw 摇号次数（amount=次）。"
    "以 lottery_catalog 为准"
)
STRATEGY_HELP = (
    "多维度出奖策略（整体替换，可选键）：softPity {start, step} / pacing / cooldown / budget / risk / behavior / rules / "
    "luck / variance（independent | prd）/ winRate / guard / fortune {spread, supreme?}（每日运气；supreme 为本活动的至尊加成，"
    "不写跟随应用设置 lottery_settings，写 {enabled: false} 即本活动不加成）。不确定时先用 lottery_simulate 验证"
)
PERKS_HELP = (
    "人群特权（整体替换）。每项 {name, disabled?, audience: all | member | lifetime | non_member | non_lifetime | ad_free, "
    "signedToday?, onlineMinutes?, when?（表达式）, extraFree?, adSkip?（0–100）, adSkipSlots?, pointsDiscount?（百分比）, "
    "extraPoints?, pityCut?, winBoost?（1–3）}"
)

REQUEST_KEYS = (
    "name", "description", "rules", "uiMode", "status", "startTime", "endTime", "freeDaily", "pointsCost", "pointsDaily",
    "videoScene", "pityThreshold", "lifetimePolicy", "lifetimePointsPerDay", "lifetimePointsLifetime", "lifetimeDraws",
    "strategy", "perks", "sortOrder",
)
PRIZE_KEYS = (
    "id", "name", "reward", "imageUrl", "quantity", "weight", "position", "fallback",
    "dailyLimit", "userLimit", "userDailyLimit", "value", "lifetimeReward",
)


def _time(value: str | None) -> Any:
    """None 不改；空串表示清空（发 null）。"""
    if value is None:
        return None
    return value or _CLEAR


_CLEAR = object()


def _activity_fields(
    *, name: str | None, description: str | None, rules: str | None, ui_mode: str | None, status: str | None,
    start_time: str | None, end_time: str | None, free_daily: int | None, points_cost: int | None, points_daily: int | None,
    video_scene: str | None, pity_threshold: int | None, pity_position: int | None, lifetime_policy: str | None,
    lifetime_points_per_day: int | None, lifetime_points_lifetime: int | None, lifetime_draws: int | None,
    strategy: dict[str, Any] | None, perks: list[dict[str, Any]] | None, sort_order: int | None,
    prizes: list[dict[str, Any]] | None,
) -> dict[str, Any]:
    fields = body(
        name=name, description=description, rules=rules, uiMode=ui_mode, status=status,
        startTime=_time(start_time), endTime=_time(end_time), freeDaily=free_daily, pointsCost=points_cost,
        pointsDaily=points_daily, videoScene=video_scene, pityThreshold=pity_threshold, pityPosition=pity_position,
        lifetimePolicy=lifetime_policy, lifetimePointsPerDay=lifetime_points_per_day,
        lifetimePointsLifetime=lifetime_points_lifetime, lifetimeDraws=lifetime_draws,
        strategy=strategy, perks=perks, sortOrder=sort_order, prizes=prizes,
    )
    return {key: (None if value is _CLEAR else value) for key, value in fields.items()}


def _request_of(current: dict[str, Any]) -> dict[str, Any]:
    """已保存的活动 → 整体更新的请求体（保底奖从 pityPrizeId 换回位置）。"""
    request = {key: current[key] for key in REQUEST_KEYS if key in current}
    request["prizes"] = []
    pity_prize = current.get("pityPrizeId")
    for prize in current.get("prizes") or []:
        request["prizes"].append({key: prize[key] for key in PRIZE_KEYS if key in prize})
        if pity_prize is not None and prize.get("id") == pity_prize:
            request["pityPosition"] = prize.get("position")
    return request


@server.tool(**READ("lottery"))
async def catalog(app: AppRef) -> Any:
    """抽奖活动表单的目录：奖品类型与取值形态、界面形态、活动状态、已拥有处理方式、可选的会员套餐 / 功能标识 / 激励广告场景、策略与特权的可选项。新建或修改活动前先看它。"""
    return await CATALOG(appkey=app)


@server.tool(**READ("lottery"))
async def activities(app: AppRef) -> Any:
    """抽奖活动列表（含奖品、总抽奖次数、人数、今日次数、待人工发放数与积分套利检查）。"""
    return await ACTIVITIES(appkey=app)


@server.tool(name="get", **READ("lottery"))
async def get_activity(app: AppRef, activity_id: ActivityID) -> Any:
    """一个抽奖活动的完整配置（奖品、策略、人群特权、已拥有处理方式）与汇总。"""
    return await ACTIVITY(appkey=app, activityId=activity_id)


Name = Annotated[str | None, Field(max_length=32, description="活动名称（≤32 字）")]
Description = Annotated[str | None, Field(max_length=200, description="活动简介（≤200 字）")]
Rules = Annotated[str | None, Field(description="活动规则说明（用户可见）")]
UIMode = Annotated[Literal["grid", "wheel"] | None, Field(description="界面形态：grid 九宫格（8 个奖品）/ wheel 转盘（2–12 个）")]
StartTime = Annotated[str | None, Field(description="开始时间 ISO 8601（RFC 3339）；空串为不限")]
EndTime = Annotated[str | None, Field(description="结束时间 ISO 8601（RFC 3339）；空串为不限")]
FreeDaily = Annotated[int | None, Field(ge=0, description="每人每天免费抽几次，0 为不送")]
PointsCost = Annotated[int | None, Field(ge=0, description="积分抽奖每次消耗多少积分，0 为不开放积分抽奖")]
PointsDaily = Annotated[int | None, Field(ge=0, description="积分抽奖每人每天上限，0 为不限")]
VideoScene = Annotated[str | None, Field(description="激励广告场景标识（看完发抽奖次数），空串为不开放视频抽奖")]
PityThreshold = Annotated[int | None, Field(ge=0, description="连续这么多次未中时必中保底奖，0 为关闭")]
PityPosition = Annotated[int | None, Field(ge=0, description="保底奖在奖盘上的位置（position）")]
LifetimePolicy = Annotated[
    Literal["points", "draws", "exclude", "grant"] | None,
    Field(description="抽中已拥有的奖品（永久会员抽中会员等）：points 折算积分（默认）/ draws 改发抽奖次数 / exclude 不参与抽取 / grant 照常发放"),
]
LifetimePerDay = Annotated[int | None, Field(ge=0, description="折算积分时每天会员 / 功能折多少积分")]
LifetimePerLifetime = Annotated[int | None, Field(ge=0, description="折算积分时永久套餐 / 永久功能折多少积分")]
LifetimeDraws = Annotated[int | None, Field(ge=0, description="改发抽奖次数时发几次")]
Strategy = Annotated[dict[str, Any] | None, Field(description=STRATEGY_HELP)]
Perks = Annotated[list[dict[str, Any]] | None, Field(description=PERKS_HELP)]
SortOrder = Annotated[int | None, Field(description="排序，小的在前")]


@server.tool(**WRITE("lottery"))
async def create(
    app: AppRef,
    name: Annotated[str, Field(min_length=1, max_length=32, description="活动名称（≤32 字）")],
    prizes: Annotated[list[dict[str, Any]], Field(min_length=1, description=PRIZES_HELP)],
    ui_mode: UIMode = None,
    status: Annotated[Status | None, Field(description="初始状态，默认 draft（草稿，用户不可见）")] = None,
    description: Description = None,
    rules: Rules = None,
    start_time: StartTime = None,
    end_time: EndTime = None,
    free_daily: FreeDaily = None,
    points_cost: PointsCost = None,
    points_daily: PointsDaily = None,
    video_scene: VideoScene = None,
    pity_threshold: PityThreshold = None,
    pity_position: PityPosition = None,
    lifetime_policy: LifetimePolicy = None,
    lifetime_points_per_day: LifetimePerDay = None,
    lifetime_points_lifetime: LifetimePerLifetime = None,
    lifetime_draws: LifetimeDraws = None,
    strategy: Strategy = None,
    perks: Perks = None,
    sort_order: SortOrder = None,
) -> Any:
    """新建抽奖活动（连同全部奖品）。建议先以 draft 创建、用 lottery_simulate 验证中奖率，再用 lottery_set_status 上线。直接以 active 创建会立即对用户开放。"""
    return await ACTIVITY_CREATE(appkey=app, body=_activity_fields(
        name=name, description=description, rules=rules, ui_mode=ui_mode, status=status,
        start_time=start_time, end_time=end_time, free_daily=free_daily, points_cost=points_cost, points_daily=points_daily,
        video_scene=video_scene, pity_threshold=pity_threshold, pity_position=pity_position, lifetime_policy=lifetime_policy,
        lifetime_points_per_day=lifetime_points_per_day, lifetime_points_lifetime=lifetime_points_lifetime,
        lifetime_draws=lifetime_draws, strategy=strategy, perks=perks, sort_order=sort_order, prizes=prizes,
    ))


@server.tool(**WRITE("lottery"))
async def update(
    app: AppRef,
    activity_id: ActivityID,
    name: Name = None,
    description: Description = None,
    rules: Rules = None,
    ui_mode: UIMode = None,
    start_time: StartTime = None,
    end_time: EndTime = None,
    free_daily: FreeDaily = None,
    points_cost: PointsCost = None,
    points_daily: PointsDaily = None,
    video_scene: VideoScene = None,
    pity_threshold: PityThreshold = None,
    pity_position: PityPosition = None,
    lifetime_policy: LifetimePolicy = None,
    lifetime_points_per_day: LifetimePerDay = None,
    lifetime_points_lifetime: LifetimePerLifetime = None,
    lifetime_draws: LifetimeDraws = None,
    strategy: Strategy = None,
    perks: Perks = None,
    sort_order: SortOrder = None,
    prizes: Annotated[list[dict[str, Any]] | None, Field(description=PRIZES_HELP + "。不传则保留现有奖品")] = None,
) -> Any:
    """修改抽奖活动。只传要改的字段，其余沿用当前配置（后端整体覆盖，这里先读后合并；状态请用 lottery_set_status）。

    传 prizes 即整体替换奖品：不带 id 的是新奖品，现有奖品没出现在列表里的会被移除。已有抽奖记录是快照，不受影响。
    进行中的活动改概率、库存会立即影响用户，执行前向用户复述改动，必要时先 lottery_simulate。
    """
    current = await ACTIVITY(appkey=app, activityId=activity_id)
    if not isinstance(current, dict):
        raise ToolError(f"读取活动 {activity_id} 失败")
    request = _request_of(current)
    request.update(_activity_fields(
        name=name, description=description, rules=rules, ui_mode=ui_mode, status=None,
        start_time=start_time, end_time=end_time, free_daily=free_daily, points_cost=points_cost, points_daily=points_daily,
        video_scene=video_scene, pity_threshold=pity_threshold, pity_position=pity_position, lifetime_policy=lifetime_policy,
        lifetime_points_per_day=lifetime_points_per_day, lifetime_points_lifetime=lifetime_points_lifetime,
        lifetime_draws=lifetime_draws, strategy=strategy, perks=perks, sort_order=sort_order, prizes=prizes,
    ))
    return await ACTIVITY_UPDATE(appkey=app, activityId=activity_id, body=request)


@server.tool(**WRITE("lottery", idempotent=True))
async def set_status(
    app: AppRef,
    activity_id: ActivityID,
    status: Annotated[Status, Field(description="draft 草稿 / active 上线 / paused 暂停 / ended 结束")],
) -> Any:
    """上线、暂停或结束抽奖活动。上线前会按完整规则复核一遍（奖品、引用的套餐 / 功能 / 场景）；上线与结束立即对用户生效，执行前先确认。"""
    return await ACTIVITY_STATUS(appkey=app, activityId=activity_id, body={"status": status})


@server.tool(name="delete", **DESTRUCTIVE("lottery"))
async def delete_activity(app: AppRef, activity_id: ActivityID) -> Any:
    """删除抽奖活动（仅限没有抽奖记录的活动，不可恢复）。有记录的活动请用 lottery_set_status 结束。执行前先向用户确认活动名称与 ID。"""
    return await ACTIVITY_DELETE(appkey=app, activityId=activity_id)


@server.tool(**WRITE("lottery", idempotent=True))
async def simulate(
    app: AppRef,
    activity_id: Annotated[int | None, Field(gt=0, description="模拟已保存的这个活动")] = None,
    activity: Annotated[dict[str, Any] | None, Field(description="模拟一份还没保存的活动配置（字段同 lottery_create 的请求体，camelCase）；给了就不用 activity_id 的已存配置")] = None,
    users: Annotated[int, Field(ge=1, le=5000, description="虚拟用户数")] = 100,
    draws_per_user: Annotated[int, Field(ge=1, le=1000, description="每人连续抽几次（用户数 × 次数 ≤ 200000）")] = 20,
    source: Annotated[Literal["", "free", "bonus", "points"], Field(description="按哪种次数来源抽，留空自动")] = "",
    isolated_stock: Annotated[bool, Field(description="每个虚拟用户独立库存（不互相抢库存）")] = False,
    seed: Annotated[int | None, Field(ge=0, description="随机种子，固定后结果可复现")] = None,
    user_id: Annotated[int | None, Field(gt=0, description="用这个真实用户的画像（并附带他下一次抽奖的出奖方案）")] = None,
    profile: Annotated[dict[str, Any] | None, Field(description="不指定用户时的虚拟画像（行为信号，如 registeredDays、signStreak、member、paidAmount…）")] = None,
    perk: Annotated[dict[str, Any] | None, Field(description="不指定用户时虚拟用户的特权身份：{member, lifetime, adFree, signedToday, onlineMinutesToday, tiers?（所在会员档位 ID，至尊加成按档位分级时用）}")] = None,
) -> Any:
    """按出奖策略模拟抽奖（不写库、不发奖、不扣次数）：实际中奖率、各奖品分布、首中次数与连续未中分布。需要 app:write 权限。

    活动有生效的至尊加成时结果带 supreme（是否拿到、加成前后系数、同一批随机数不加成的对照组 baseline）。"""
    if activity_id is None and activity is None:
        raise ToolError("需要 activity_id（已保存的活动）或 activity（未保存的配置）之一")
    if users * draws_per_user > 200_000:
        raise ToolError("虚拟用户数 × 每人次数不能超过 200000")
    return await SIMULATE(appkey=app, body=body(
        activityId=activity_id, activity=activity, users=users, drawsPerUser=draws_per_user, source=source or None,
        isolatedStock=isolated_stock, seed=seed, userId=user_id, profile=profile, perk=perk,
    ))


@server.tool(**READ("lottery"))
async def stats(app: AppRef, activity_id: ActivityID, days: Days = 14) -> Any:
    """一个活动的概览、按天趋势、次数来源与奖品分布。"""
    return await ACTIVITY_STATS(appkey=app, activityId=activity_id, query={"days": days})


@server.tool(**READ("lottery"))
async def audit(app: AppRef, activity_id: ActivityID, days: Days = 14) -> Any:
    """出奖审计：实际出奖分布与配置概率的整体检验（Mahalanobis χ²）和各奖品的边际检验，判断是否偏离配置。

    结果的 replay 是逐笔复算：每条记录按方案里的权重与随机种子重抽一次，mismatches 列出对不上的记录
    （记录或方案被改过）；unseeded 是种子功能上线之前的记录，无法复算。"""
    return await ACTIVITY_AUDIT(appkey=app, activityId=activity_id, query={"days": days})


@server.tool(**READ("lottery"))
async def analyze(
    app: AppRef,
    activity_id: Annotated[int | None, Field(gt=0, description="分析已保存的这个活动")] = None,
    activity: Annotated[dict[str, Any] | None, Field(description="分析一份还没保存的活动配置（字段同 lottery_create 的请求体，camelCase）；给了就不用 activity_id 的已存配置")] = None,
    personas: Annotated[list[dict[str, Any]] | None, Field(description=PERSONAS_HELP)] = None,
) -> Any:
    """精确分析奖盘（不抽样、不写库，只读权限即可）：每种人群的综合概率（含保底、软保底、伪随机，longRun）对公示概率（nominal）、
    综合中奖率、每抽期望价值与积分回报、各奖品期望多少抽出一个（hitMean）与分位数、1/10/50/100 连抽至少出一个的概率（within），
    以及每日运气最倒霉、最走运、全体平均三档。调概率之前先用它看现状，调完再用它核对；比 lottery_simulate 快且没有抽样误差。

    库存、每日限量、个人上限、冷却等随时间变化的维度按此刻的状态固定处理（见结果的 notes），要看它们的效果用 lottery_compute_run。"""
    if activity_id is None and activity is None:
        raise ToolError("需要 activity_id（已保存的活动）或 activity（未保存的配置）之一")
    return await ANALYZE(appkey=app, body=body(activityId=activity_id, activity=activity, personas=personas))


@server.tool(**READ("lottery"))
async def solve(
    app: AppRef,
    goal: Annotated[dict[str, Any], Field(description=(
        "目标（概率都是 0–1，不设的不写）：{winRate?（综合中奖率；奖盘没有「谢谢参与」时不能用）, realRate?（兜底奖与谢谢参与以外奖品的综合概率）, "
        "value?（每抽期望价值，积分当量）, valueMode?: max（上限，默认）| equal, pointsReturn?（每抽期望积分回报上限，"
        "无套利即不超过用积分买一次的价格）, prizes?: [{position, mode: lock（权重占比不变）| target（综合概率 = probability）| "
        "range（综合概率在 [min, max]）, probability?, min?, max?}], total?（新权重的合计，默认自动）}"
    ))],
    activity_id: Annotated[int | None, Field(gt=0, description="求解已保存的这个活动")] = None,
    activity: Annotated[dict[str, Any] | None, Field(description="求解一份还没保存的活动配置（字段同 lottery_create 的请求体，camelCase）")] = None,
    persona: Annotated[dict[str, Any] | None, Field(description="目标提在哪类人眼里：{member?, lifetime?, adFree?, signedToday?, tiers?}；不传为普通用户")] = None,
    fortune: Annotated[Literal["average", "high", "low"], Field(description="按哪一天的运气算：average 平均 / high 最走运 / low 最倒霉")] = "average",
) -> Any:
    """按目标反解奖盘权重（不保存，只读权限即可）：在满足全部目标的权重里取对现有奖盘改动最小的一组（KL 散度最小），
    目标针对的是含保底、中奖加成、运气之后的综合概率。返回每格的新旧权重与综合概率、各目标的达成情况（checks）、
    求解前后的积分账（economyBefore / economyAfter）。feasible 为 false 表示目标互相矛盾或做不到。

    解出的权重要生效，再用 lottery_update 把 prizes[].weight 改成 newWeight 保存。"""
    if activity_id is None and activity is None:
        raise ToolError("需要 activity_id（已保存的活动）或 activity（未保存的配置）之一")
    return await SOLVE(appkey=app, body=body(
        activityId=activity_id, activity=activity, persona=persona, fortune=fortune, goal=goal,
    ))


@server.tool(**WRITE("lottery"))
async def compute_run(
    app: AppRef,
    activity_id: Annotated[int | None, Field(gt=0, description="推演已保存的这个活动")] = None,
    activity: Annotated[dict[str, Any] | None, Field(description="推演一份还没保存的活动配置（字段同 lottery_create 的请求体，camelCase）")] = None,
    users: Annotated[int, Field(ge=1, le=100_000, description="每个平行世界的人数")] = 2000,
    days: Annotated[int, Field(ge=1, le=60, description="连续抽多少天")] = 7,
    draws_per_day: Annotated[int, Field(ge=1, le=200, description="每人每天抽几次")] = 5,
    replications: Annotated[int, Field(ge=1, le=256, description="独立演多少遍（平行世界数）；越多置信区间越窄，至少 8 遍才有像样的区间")] = 24,
    personas: Annotated[list[dict[str, Any]] | None, Field(description=PERSONAS_HELP)] = None,
    source: Annotated[Literal["", "free", "bonus", "points"], Field(description="按哪种次数来源抽，留空为免费")] = "",
    seed: Annotated[str, Field(description="随机种子（十进制整数的字符串）；留空随机取一个。同一个种子得到同一份结果")] = "",
    name: Annotated[str, Field(max_length=32, description="任务备注名")] = "",
) -> Any:
    """发起一次大规模推演（异步任务，立刻返回任务；不发奖、不扣次数，需要 app:write 权限）：一批人按各自的身份连续抽若干天，
    共享库存、每日限量与积分预算，独立演很多遍，每个指标带 95% 置信区间。回答「限量奖品几天抽空」「预算够不够」
    「各类人各拿走多少」「永久会员用积分买次数到底赚不赚」这类问题。总抽奖次数（人数 × 天数 × 次数 × 遍数）不超过 3000 万。

    用 lottery_compute_job 查进度与结果（status 为 succeeded 时带 result）。"""
    if activity_id is None and activity is None:
        raise ToolError("需要 activity_id（已保存的活动）或 activity（未保存的配置）之一")
    if seed and not seed.isdigit():
        raise ToolError("seed 需要是非负整数的字符串")
    return await COMPUTE_CREATE(appkey=app, body=body(
        activityId=activity_id, activity=activity, name=name or None, users=users, days=days, drawsPerDay=draws_per_day,
        replications=replications, personas=personas, source=source or None, seed=seed or None,
    ))


@server.tool(**READ("lottery"))
async def compute_jobs(
    app: AppRef,
    activity_id: Annotated[int | None, Field(gt=0, description="只看这个活动的，留空为全部")] = None,
    page: Page = 1,
    limit: Limit = 20,
) -> Any:
    """推演任务列表（新的在前；状态、规模摘要、进度，不带结果）。"""
    return await COMPUTE_JOBS(appkey=app, query=q(activityId=activity_id, page=page, limit=limit))


@server.tool(**READ("lottery"))
async def compute_job(app: AppRef, job_id: JobID) -> Any:
    """一个推演任务的状态、进度与结果。status：queued 排队 / running 执行中（progress / total 为已演完的平行世界数）/
    succeeded 完成（带 result）/ failed 失败（message 是原因）/ cancelled 已取消。"""
    return await COMPUTE_JOB(appkey=app, jobId=job_id)


@server.tool(**WRITE("lottery", idempotent=True))
async def compute_cancel(app: AppRef, job_id: JobID) -> Any:
    """取消一个排队或执行中的推演任务（已经结束的原样返回）。执行中的任务几秒内停下，不留结果。"""
    return await COMPUTE_CANCEL(appkey=app, jobId=job_id)


@server.tool(**DESTRUCTIVE("lottery"))
async def compute_delete(app: AppRef, job_id: JobID) -> Any:
    """删除一个已经结束的推演任务连同它的结果（不可恢复）。还在排队或执行的要先 lottery_compute_cancel。"""
    return await COMPUTE_DELETE(appkey=app, jobId=job_id)


@server.tool(**READ("lottery"))
async def draws(
    app: AppRef,
    activity_id: Annotated[int | None, Field(gt=0, description="只看这个活动，留空为全部")] = None,
    user_id: Annotated[int | None, Field(gt=0, description="只看这个用户（管理端用户 ID）")] = None,
    status: Annotated[Literal["", "granted", "pending", "fulfilled"], Field(description="granted 已到账 / pending 人工奖品待发放 / fulfilled 人工奖品已发放，留空为全部")] = "",
    source: Annotated[Literal["", "free", "bonus", "points"], Field(description="次数来源：free 免费 / bonus 抽奖次数余额 / points 积分")] = "",
    winning: Annotated[bool, Field(description="只看抽中的（排除谢谢参与）")] = False,
    keyword: Annotated[str, Field(description="关键词（账号、奖品名等）")] = "",
    start: IsoTime = "",
    end: IsoTime = "",
    page: Page = 1,
    limit: Limit = 20,
) -> Any:
    """抽奖记录 / 中奖记录（奖品快照、来源、发放结果与折算、状态）。查待人工发放的奖品用 status=pending。"""
    return await DRAWS(appkey=app, query=q(
        activityId=activity_id, userId=user_id, status=status, source=source, winning=winning or None,
        keyword=keyword, start=start, end=end, page=page, limit=limit,
    ))


@server.tool(**WRITE("lottery"))
async def fulfill(
    app: AppRef,
    draw_id: Annotated[int, Field(gt=0, description="抽奖记录 ID（lottery_draws 结果里的 id，状态须为 pending）")],
    note: Annotated[str, Field(description="发放说明，会写进给用户的站内信（兑换码、快递单号等）")] = "",
) -> Any:
    """把一条人工发放的奖品标记为已发放，并给用户发站内信（含 note）。消息发出后无法撤回，执行前先向用户确认 note 内容。"""
    return await DRAW_FULFILL(appkey=app, drawId=draw_id, body=body(note=note))


@server.tool(**READ("lottery"))
async def tickets(
    app: AppRef,
    user_id: Annotated[int | None, Field(gt=0, description="查这个用户的次数账户与批次（管理端用户 ID）；留空为全应用总览")] = None,
) -> Any:
    """抽奖次数：留空 user_id 时为总览（持有人数、未用次数分赠送 / 兑换两档、今天发放与使用、各来源累计与剩余）；
    给了 user_id 时为这个人的余额、两档拆分、累计与还有剩余的批次（排在前面的先用）。

    扣除顺序：今日免费次数 → 赠送的抽奖次数（先到先用）→ 兑换的抽奖次数（先到先用）→ 积分。
    """
    if user_id:
        return await TICKET_ACCOUNT(appkey=app, userId=user_id)
    return await TICKETS(appkey=app)


@server.tool(**READ("lottery"))
async def ticket_accounts(
    app: AppRef,
    keyword: Annotated[str, Field(description="账号、昵称或用户 ID")] = "",
    page: Page = 1,
    limit: Limit = 20,
) -> Any:
    """有抽奖次数账户的用户（余额多的在前），含赠送 / 兑换两档与累计发放、使用。"""
    return await TICKET_ACCOUNTS(appkey=app, query=q(keyword=keyword, page=page, limit=limit))


@server.tool(**READ("lottery"))
async def ticket_ledger(
    app: AppRef,
    user_id: Annotated[int | None, Field(gt=0, description="只看这个用户（管理端用户 ID）")] = None,
    kind: Annotated[Literal["", "grant", "consume", "deduct"], Field(description="grant 获得 / consume 抽奖使用 / deduct 系统扣减，留空为全部")] = "",
    origin: Annotated[Literal["", "rewarded_ad", "points_store", "cardkey", "invite", "leaderboard", "lottery", "item_grant", "admin", "legacy"],
                      Field(description="次数来源，留空为全部")] = "",
    keyword: Annotated[str, Field(description="账号、昵称、单号或说明")] = "",
    page: Page = 1,
    limit: Limit = 20,
) -> Any:
    """抽奖次数流水（新的在前）：每一次发放、抽奖使用、扣减，带变动后余额与来源单号。"""
    return await TICKET_LEDGER(appkey=app, query=q(
        userId=user_id, kind=kind, origin=origin, keyword=keyword, page=page, limit=limit,
    ))


@server.tool(**WRITE("lottery"))
async def adjust_tickets(
    app: AppRef,
    user_id: Annotated[int, Field(gt=0, description="管理端用户 ID")],
    amount: Annotated[int, Field(ge=-10000, le=10000, description="正数为发放、负数为扣减（按扣除顺序先扣赠送的），不能为 0")],
    reason: Annotated[str, Field(min_length=1, max_length=100, description="原因：发放时写进给用户的站内信")],
    paid: Annotated[bool, Field(description="发放的次数按兑换所得记（排在赠送的之后用），用于补发用户买的次数")] = False,
) -> Any:
    """给一个用户发放或扣减抽奖次数，返回调整后的账户。发放会给用户发站内信，扣减不可撤回：执行前先向用户确认数量与原因。"""
    if amount == 0:
        raise ToolError("amount 不能为 0")
    return await TICKET_ADJUST(appkey=app, userId=user_id, body=body(amount=amount, reason=reason, paid=paid or None))


SupremeScope = Literal["lifetime", "tiers"]


@server.tool(**READ("lottery"))
async def settings(app: AppRef) -> Any:
    """应用的抽奖设置：每日运气的至尊加成（supreme），附可选的会员档位与开了每日运气的活动各自的加成来源（activity / app）。"""
    return await SETTINGS(appkey=app)


@server.tool(**WRITE("lottery", idempotent=True))
async def save_settings(
    app: AppRef,
    enabled: Annotated[bool, Field(description="开启至尊加成（只对开了每日运气、且没有单独设置的活动生效）")],
    scope: Annotated[SupremeScope, Field(description="lifetime 只对永久会员；tiers 按会员档位分级")] = "lifetime",
    boost: Annotated[float, Field(ge=0, le=3, description="加成幅度：运气系数乘多少（1–3，0 为不乘）；tiers 时是没落在任何一档的永久会员的幅度")] = 0,
    floor: Annotated[float, Field(ge=0, le=5, description="保底运气：加成之后至少多少（0 不设，1 为平均水平）")] = 0,
    cap: Annotated[float, Field(ge=0, le=5, description="上限：加成之后最多多少（0 按 5 封顶；自然运气已高过上限时按自然运气）")] = 0,
    tiers: Annotated[list[dict[str, Any]] | None, Field(description="tiers 时每档 {tierId, boost?, floor?, cap?, lifetimeOnly?}；档位 ID 见 lottery_settings 的 tiers")] = None,
    title: Annotated[str, Field(max_length=16, description="展示给用户的名称，默认「至尊加成」")] = "",
) -> Any:
    """保存应用的至尊加成（整体替换），会改变所有跟随应用设置的活动的出奖概率：执行前先向用户复述参数并确认。

    可先用 lottery_simulate 看效果。"""
    return await SETTINGS_SAVE(appkey=app, body=body(supreme=body(
        enabled=enabled, scope=scope, boost=boost or None, floor=floor or None, cap=cap or None,
        tiers=tiers if scope == "tiers" else None, title=title or None,
    )))
