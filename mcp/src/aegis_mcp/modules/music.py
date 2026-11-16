"""音乐推荐：总览、听众与画像、推荐预览、作品与歌手排行、搜索分析、模型训练与发布、应用配置、干预规则、A/B 实验、清除听歌数据。"""

from __future__ import annotations

import re
from datetime import datetime, timedelta, timezone
from typing import Annotated, Any, Literal

from fastmcp import FastMCP
from fastmcp.exceptions import ToolError
from pydantic import BaseModel, Field

from ..core import DELETE, DESTRUCTIVE, GET, POST, PUT, READ, WRITE, AppRef, IsoTime, Page, UserID, body, q

NAMESPACE = "music"
server = FastMCP("music")

B = "/api/admin/apps/:appkey/music"
OVERVIEW = GET(f"{B}/overview")
USERS = GET(f"{B}/users")
USER = GET(f"{B}/users/:userId")
USER_EVENTS = GET(f"{B}/users/:userId/events")
USER_MIXES = GET(f"{B}/users/:userId/mixes")
USER_REPORT = GET(f"{B}/users/:userId/report")
USER_PREVIEW = GET(f"{B}/users/:userId/recommendations")
USER_FORGET = POST(f"{B}/users/:userId/forget")
EVENTS = GET(f"{B}/events")
TRACKS = GET(f"{B}/tracks")
TRACK = GET(f"{B}/tracks/:workKey")
ARTISTS = GET(f"{B}/artists")
ARTIST = GET(f"{B}/artists/:name")
SEARCH = GET(f"{B}/search")
MODELS = GET(f"{B}/models")
MODEL = GET(f"{B}/models/:runId")
TRAIN = POST(f"{B}/models/train")
PUBLISH = POST(f"{B}/models/:runId/publish")
CONFIG = GET(f"{B}/config")
CONFIG_SAVE = PUT(f"{B}/config")
RULES = GET(f"{B}/rules")
RULE_CREATE = POST(f"{B}/rules")
RULE_UPDATE = PUT(f"{B}/rules/:id")
RULE_DELETE = DELETE(f"{B}/rules/:id")
EXPERIMENTS = GET(f"{B}/experiments")
EXPERIMENT_CREATE = POST(f"{B}/experiments")
EXPERIMENT_UPDATE = PUT(f"{B}/experiments/:id")
EXPERIMENT_DELETE = DELETE(f"{B}/experiments/:id")
EXPERIMENT_START = POST(f"{B}/experiments/:id/start")
EXPERIMENT_STOP = POST(f"{B}/experiments/:id/stop")
EXPERIMENT_RESULTS = GET(f"{B}/experiments/:id/results")

Scene = Literal["home", "daily", "radio", "similar", "autoplay", "discover", "mix", "artist"]
SCENE_DOC = (
    "home 为你推荐 / daily 每日推荐 / radio 私人电台 / similar 相似歌曲（要 source + song_id）/ autoplay 智能续播（要 seeds）/ "
    "discover 每周雷达 / mix 风格歌单（要 mix_id）/ artist 歌手个性化热门（要 artist）"
)
EventType = Literal[
    "", "play_start", "play_end", "pause", "resume", "seek", "like", "unlike", "dislike", "undislike", "playlist_add",
    "download", "share", "search", "search_click", "click", "impression", "lyrics_open", "autoplay", "report_view", "onboarding",
]
StatDays = Annotated[Literal[1, 7, 14, 30, 90, 180, 365], Field(description="统计天数：1 / 7 / 14 / 30 / 90 / 180 / 365")]
ProfileDays = Annotated[Literal[7, 30, 90, 365], Field(description="画像统计天数：7 / 30 / 90 / 365")]
PageSize = Annotated[int, Field(ge=1, le=100, description="每页条数")]
RunID = Annotated[int, Field(gt=0, description="训练记录 ID（music_models 结果里的 id）")]
RuleID = Annotated[int, Field(gt=0, description="规则 ID（music_rules 结果里的 id）")]
ExperimentID = Annotated[int, Field(gt=0, description="实验 ID（music_experiments 结果里的 id）")]
CST = timezone(timedelta(hours=8))
_SEED = re.compile(r"[a-z0-9]+:\S+")


def _ms(value: str, name: str) -> int | None:
    """ISO 8601（不带时区按北京时间）或毫秒时间戳 → 毫秒；空串为 None。"""
    value = (value or "").strip()
    if not value:
        return None
    if value.isdigit():
        return int(value)
    try:
        moment = datetime.fromisoformat(value.replace("Z", "+00:00"))
    except ValueError as exc:
        raise ToolError(f"{name} 不是合法的时间（用 ISO 8601，如 2026-11-16T00:00:00+08:00）") from exc
    if moment.tzinfo is None:
        moment = moment.replace(tzinfo=CST)
    return int(moment.timestamp() * 1000)


def _camel(key: str) -> str:
    head, *rest = key.split("_")
    return head + "".join(part[:1].upper() + part[1:] for part in rest)


def _payload(model: BaseModel | None) -> dict[str, Any] | None:
    """嵌套参数对象 → 后端的驼峰键，只留给了值的字段。"""
    if model is None:
        return None
    return {_camel(key): value for key, value in model.model_dump(exclude_none=True).items()}


def _items(data: Any) -> list[dict[str, Any]]:
    if isinstance(data, list):
        return [item for item in data if isinstance(item, dict)]
    if isinstance(data, dict):
        return [item for item in data.get("items") or [] if isinstance(item, dict)]
    return []


async def _find(route: Any, app: str, item_id: int, what: str, hint: str) -> dict[str, Any]:
    for item in _items(await route(appkey=app)):
        if item.get("id") == item_id:
            return item
    raise ToolError(f"没有 ID 为 {item_id} 的{what}；先用 {hint} 查看")


# ═══════════════════════ 总览、听众与推荐预览 ═══════════════════════


@server.tool(**READ("music"))
async def overview(app: AppRef, days: StatDays = 7) -> Any:
    """音乐推荐总览：每日事件与听歌人数 / 分钟 / 完播率 / 跳过率（含上一周期对比）、事件类型、时段、各场景曝光点击播放、
    热门作品与歌手、音源与平台分布、当前发布的模型、按模型版本的点击率、运行中的实验、不感兴趣原因分布、消费积压。"""
    return await OVERVIEW(appkey=app, query=q(days=days))


@server.tool(**READ("music"))
async def user(
    app: AppRef,
    user_id: Annotated[int | None, Field(gt=0, description="管理端用户 ID；不给时看全应用（听众排行或事件检索）")] = None,
    view: Annotated[Literal["profile", "events", "mixes", "report"], Field(description=(
        "profile：给 user_id 为画像详情，不给为听众排行；events：给 user_id 为这个人的事件时间线，不给为全应用事件检索；"
        "mixes：这个人的风格歌单；report：这个人的听歌报告"
    ))] = "profile",
    days: Annotated[int, Field(description="profile 详情的画像天数 7 / 30 / 90 / 365；听众排行的统计天数 1 / 7 / 14 / 30 / 90 / 180 / 365")] = 30,
    event_type: Annotated[EventType, Field(description="events：只看这种事件，空为全部")] = "",
    keyword: Annotated[str, Field(description="events 全应用检索：按歌名 / 歌手 / 搜索词匹配")] = "",
    from_time: Annotated[IsoTime, Field(description="events 全应用检索的起始时间（ISO 8601，不带时区按北京时间），默认近 7 天")] = "",
    to_time: Annotated[IsoTime, Field(description="events 全应用检索的结束时间")] = "",
    before: Annotated[IsoTime, Field(description="events 时间线游标：只看这个时刻之前的（ISO 8601 或上一页的 nextBefore 毫秒数），留空从最新开始")] = "",
    period: Annotated[Literal["week", "month", "year"], Field(description="report：周报 / 月报 / 年报")] = "week",
    offset: Annotated[int, Field(ge=0, le=52, description="report：0 本期，1 上一期……（最多回看 52 周 / 24 月 / 5 年）")] = 0,
    page: Page = 1,
    limit: Annotated[int, Field(ge=1, le=200, description="每页条数（听众排行最多 100）")] = 20,
) -> Any:
    """听众与个人画像。给 user_id：画像（与客户端同形）+ 实时状态（最近播放、正负反馈、不喜欢的歌手、曝光疲劳）+ 模型里的口味、
    偏好设置、我的不喜欢、所在实验；也可切到事件时间线、风格歌单、听歌报告。不给 user_id：听众排行或全应用事件检索。"""
    if user_id is None:
        if view == "profile":
            if days not in (1, 7, 14, 30, 90, 180, 365):
                raise ToolError("days 只能是 1 / 7 / 14 / 30 / 90 / 180 / 365")
            return await USERS(appkey=app, query=q(days=days, page=page, pageSize=min(limit, 100)))
        if view == "events":
            return await EVENTS(appkey=app, query=q(
                type=event_type, q=keyword, **{"from": _ms(from_time, "from_time"), "to": _ms(to_time, "to_time")},
                page=page, pageSize=limit,
            ))
        raise ToolError(f"view={view} 需要 user_id")
    if view == "profile":
        if days not in (7, 30, 90, 365):
            raise ToolError("画像的 days 只能是 7 / 30 / 90 / 365")
        return await USER(appkey=app, userId=user_id, query=q(days=days))
    if view == "events":
        return await USER_EVENTS(appkey=app, userId=user_id, query=q(type=event_type, before=_ms(before, "before"), limit=limit))
    if view == "mixes":
        return await USER_MIXES(appkey=app, userId=user_id)
    return await USER_REPORT(appkey=app, userId=user_id, query=q(period=period, offset=offset))


@server.tool(**READ("music"))
async def preview(
    app: AppRef,
    user_id: UserID,
    scene: Annotated[Scene, Field(description=SCENE_DOC)] = "home",
    limit: Annotated[int, Field(ge=1, le=50, description="条数（daily 固定 30）")] = 20,
    source: Annotated[str, Field(description="similar：种子歌曲的音源（如 wy、tx）")] = "",
    song_id: Annotated[str, Field(description="similar：种子歌曲在该音源的 ID")] = "",
    seeds: Annotated[list[str] | None, Field(max_length=20, description="autoplay：最近放的歌，每项「音源:歌曲 ID」（如 wy:186016），最近的在后，最多 20 个")] = None,
    mix_id: Annotated[str, Field(description="mix：风格歌单 ID（music_user view=mixes 可查）")] = "",
    artist: Annotated[str, Field(description="artist：歌手名")] = "",
) -> Any:
    """以某个用户的身份预览推荐（不发曝光、不影响线上）：每首的分数、推荐理由与各通道分数拆解（als / itemcf / artist / trending /
    explore / foldin / ranker / rule / fatigue / penalty），以及是否冷启动、模型版本、命中的实验与变体。"""
    if scene == "similar" and not (source.strip() and song_id.strip()):
        raise ToolError("scene=similar 需要 source 与 song_id")
    if scene == "autoplay":
        if not seeds:
            raise ToolError("scene=autoplay 需要 seeds")
        bad = [seed for seed in seeds if not _SEED.fullmatch(seed.strip())]
        if bad:
            raise ToolError(f"seeds 每项写成「音源:歌曲 ID」，这些不对：{', '.join(bad)}")
    if scene == "mix" and not mix_id.strip():
        raise ToolError("scene=mix 需要 mix_id")
    if scene == "artist" and not artist.strip():
        raise ToolError("scene=artist 需要 artist")
    return await USER_PREVIEW(appkey=app, userId=user_id, query=q(
        scene=scene, limit=limit,
        source=source.strip() if scene == "similar" else "", songId=song_id.strip() if scene == "similar" else "",
        seeds=",".join(seed.strip() for seed in seeds) if scene == "autoplay" and seeds else "",
        mixId=mix_id.strip() if scene == "mix" else "", artist=artist.strip() if scene == "artist" else "",
    ))


@server.tool(**DESTRUCTIVE("music"))
async def forget_user(app: AppRef, user_id: UserID) -> Any:
    """清除一个用户在本应用的全部听歌数据：事件与曝光、实时状态、模型里的向量与画像（下次训练前就不再使用），不可恢复。
    执行前先向用户复述要清除的是哪个应用的哪个用户并取得确认。"""
    return await USER_FORGET(appkey=app, userId=user_id)


# ═══════════════════════ 作品、歌手与搜索 ═══════════════════════


@server.tool(**READ("music"))
async def tracks(
    app: AppRef,
    days: StatDays = 7,
    keyword: Annotated[str, Field(description="按歌名 / 歌手搜索")] = "",
    sort: Annotated[Literal["plays", "listeners", "completion", "skip"], Field(description="排序：plays 播放次数 / listeners 听众数 / completion 完播率 / skip 跳过率")] = "plays",
    page: Page = 1,
    limit: PageSize = 20,
) -> Any:
    """作品排行：播放、听众、分钟、完播率、跳过率、收藏。作品以 workKey 区分（同一首歌的各音源版本合并）。"""
    return await TRACKS(appkey=app, query=q(days=days, q=keyword, sort=sort, page=page, pageSize=limit))


@server.tool(**READ("music"))
async def track(
    app: AppRef,
    work_key: Annotated[str, Field(min_length=1, description="作品键（music_tracks 结果里的 workKey）")],
    days: StatDays = 30,
) -> Any:
    """作品详情：各音源版本、统计（含不感兴趣人数）、每日播放走势、相似作品（ItemCF 邻居与 ALS 近邻）。"""
    return await TRACK(appkey=app, workKey=work_key.strip(), query=q(days=days))


@server.tool(**READ("music"))
async def artists(
    app: AppRef,
    name: Annotated[str, Field(description="歌手名：给了看这位歌手的详情，留空看歌手排行")] = "",
    days: StatDays = 30,
    keyword: Annotated[str, Field(description="排行：按歌手名搜索")] = "",
    page: Page = 1,
    limit: PageSize = 20,
) -> Any:
    """歌手排行（播放、听众、分钟、作品数、完播率、跳过率、收藏），或一位歌手的详情（统计、听众走势、代表作、模型里的相似歌手、
    不喜欢这位歌手的人数）。"""
    if name.strip():
        return await ARTIST(appkey=app, name=name.strip(), query=q(days=days))
    return await ARTISTS(appkey=app, query=q(days=days, q=keyword, page=page, pageSize=limit))


@server.tool(**READ("music"))
async def search_stats(
    app: AppRef,
    days: StatDays = 7,
    limit: Annotated[int, Field(ge=1, le=200, description="热门词与无点击词各取多少个")] = 50,
) -> Any:
    """搜索分析：搜索次数与人数、搜索后点击率、热门搜索词、搜了没点的词（曲库或排序可能有问题）、每日走势。"""
    return await SEARCH(appkey=app, query=q(days=days, limit=limit))


# ═══════════════════════ 模型 ═══════════════════════


@server.tool(**READ("music"))
async def models(
    app: AppRef,
    run_id: Annotated[int | None, Field(gt=0, description="训练记录 ID：给了看详情，留空看训练记录列表")] = None,
    page: Page = 1,
    limit: PageSize = 20,
) -> Any:
    """训练记录列表（状态、触发方式、指标、规模、耗时、是否当前发布），或一次训练的详情（训练时的配置快照、指标对比热度基线、
    精排 ranker 的样本 / AUC / 特征权重、各阶段耗时、日志）。"""
    if run_id is not None:
        return await MODEL(appkey=app, runId=run_id)
    return await MODELS(appkey=app, query=q(page=page, pageSize=limit))


@server.tool(**WRITE("music"))
async def train(
    app: AppRef,
    note: Annotated[str, Field(max_length=200, description="备注（记在训练记录上）")] = "",
) -> Any:
    """立即训练一次模型（进队列，同一应用同时只训练一个）。返回这条训练记录；autoPublish 打开且指标达标时训练完自动发布。
    训练占用音乐服务算力，执行前先向用户复述并取得确认。"""
    return await TRAIN(appkey=app, body=body(note=note.strip() or None) or {})


@server.tool(**WRITE("music", idempotent=True))
async def publish(app: AppRef, run_id: RunID) -> Any:
    """发布一次训练成功的模型（回滚也用它：发布旧版本即可），立即影响本应用全部用户的推荐。只能发布训练成功且模型文件仍保留的版本（否则 40900）。
    执行前先用 music_models 核对指标，向用户复述要发布的版本并取得确认。"""
    return await PUBLISH(appkey=app, runId=run_id)


# ═══════════════════════ 应用配置 ═══════════════════════


class Weights(BaseModel):
    """隐式反馈打分权重，每项 −20–20。"""

    complete: float | None = Field(None, ge=-20, le=20, description="听完")
    partial: float | None = Field(None, ge=-20, le=20, description="听了 30%–80%")
    skip: float | None = Field(None, ge=-20, le=20, description="早跳")
    like: float | None = Field(None, ge=-20, le=20, description="收藏")
    dislike: float | None = Field(None, ge=-20, le=20, description="不感兴趣")
    playlist_add: float | None = Field(None, ge=-20, le=20, description="加入歌单")
    download: float | None = Field(None, ge=-20, le=20, description="下载")
    share: float | None = Field(None, ge=-20, le=20, description="分享")
    search_click: float | None = Field(None, ge=-20, le=20, description="点搜索结果")
    click: float | None = Field(None, ge=-20, le=20, description="点列表里的歌")
    seek_back: float | None = Field(None, ge=-20, le=20, description="大幅向后拖动")


class Als(BaseModel):
    """隐式 ALS 参数。"""

    factors: int | None = Field(None, ge=8, le=256, description="向量维数")
    iterations: int | None = Field(None, ge=1, le=50, description="迭代次数")
    regularization: float | None = Field(None, ge=0.0001, le=10, description="正则")
    alpha: float | None = Field(None, ge=1, le=200, description="置信度系数")


class ItemCF(BaseModel):
    """ItemCF 参数。"""

    neighbors: int | None = Field(None, ge=5, le=200, description="每个作品保留的邻居数")
    session_boost: float | None = Field(None, ge=0, le=2, description="会话内「下一首」转移加成")


class Blend(BaseModel):
    """各召回通道的融合权重，每项 0–5。"""

    als: float | None = Field(None, ge=0, le=5)
    itemcf: float | None = Field(None, ge=0, le=5)
    artist: float | None = Field(None, ge=0, le=5)
    trending: float | None = Field(None, ge=0, le=5)
    explore: float | None = Field(None, ge=0, le=5)


class Ranker(BaseModel):
    """精排（逻辑回归）参数。"""

    enabled: bool | None = Field(None, description="样本够且 AUC 胜过基线时是否启用")
    min_samples: int | None = Field(None, ge=200, le=1_000_000, description="最少样本数")
    l2: float | None = Field(None, ge=0, le=1, description="L2 正则")
    epochs: int | None = Field(None, ge=1, le=50, description="训练轮数")


class Fatigue(BaseModel):
    """曝光疲劳：近 7 天展示 ≥ threshold 次没点，分数 × decay^(次数−阈值+1)。"""

    threshold: int | None = Field(None, ge=1, le=20)
    decay: float | None = Field(None, ge=0.1, le=1)


class Discover(BaseModel):
    """每周雷达。"""

    size: int | None = Field(None, ge=10, le=50, description="首数")
    popularity_penalty: float | None = Field(None, ge=0, le=2, description="热度惩罚（越大越偏长尾）")


class Mixes(BaseModel):
    """风格歌单。"""

    max: int | None = Field(None, ge=0, le=10, description="最多几个歌单（0 关闭）")
    size: int | None = Field(None, ge=10, le=50, description="每个歌单首数")


class Autoplay(BaseModel):
    """智能续播。"""

    size: int | None = Field(None, ge=5, le=30, description="每批首数")


_READONLY_CONFIG = ("updatedAt", "updatedBy")


@server.tool(**READ("music"))
async def config(app: AppRef) -> Any:
    """音乐推荐的应用配置：开关（推荐 / 采集）、打分权重、训练窗口与半衰期、ALS / ItemCF / 精排参数、通道融合权重、多样性、
    实时折入、曝光疲劳、暂不推荐天数、每周雷达 / 风格歌单 / 智能续播、定时训练与自动发布门槛。"""
    return await CONFIG(appkey=app)


@server.tool(**WRITE("music", idempotent=True))
async def update_config(
    app: AppRef,
    enabled: Annotated[bool | None, Field(description="推荐开关；关掉后客户端推荐接口回 50396 并隐藏入口")] = None,
    collect_enabled: Annotated[bool | None, Field(description="采集开关；关掉后上报照常回 200 但不再记录")] = None,
    window_days: Annotated[int | None, Field(ge=7, le=730, description="训练取多少天的行为")] = None,
    half_life_days: Annotated[float | None, Field(ge=1, le=365, description="行为分数的半衰期（天）")] = None,
    min_item_users: Annotated[int | None, Field(ge=1, le=50, description="作品至少被几个人听过才进模型")] = None,
    weights: Annotated[Weights | None, Field(description="打分权重（只改给出的项）")] = None,
    als: Annotated[Als | None, Field(description="ALS 参数（只改给出的项）")] = None,
    itemcf: Annotated[ItemCF | None, Field(description="ItemCF 参数（只改给出的项）")] = None,
    blend: Annotated[Blend | None, Field(description="通道融合权重（只改给出的项）")] = None,
    artist_cap: Annotated[int | None, Field(ge=1, le=10, description="前 20 首里同一歌手最多几首")] = None,
    recent_exclude_days: Annotated[int | None, Field(ge=0, le=30, description="近几天播放过的降权")] = None,
    explore_ratio: Annotated[float | None, Field(ge=0, le=0.5, description="探索占比")] = None,
    schedule: Annotated[str | None, Field(description="定时训练，五段 cron（如 0 4 * * *）；空串为不定时")] = None,
    auto_publish: Annotated[bool | None, Field(description="训练完指标达标时自动发布")] = None,
    min_lift: Annotated[float | None, Field(ge=0, le=3, description="自动发布门槛：NDCG@20 ≥ 热度基线 × min_lift")] = None,
    fold_in: Annotated[bool | None, Field(description="实时折入：训练间隙按最新行为更新口味")] = None,
    ranker: Annotated[Ranker | None, Field(description="精排参数（只改给出的项）")] = None,
    fatigue: Annotated[Fatigue | None, Field(description="曝光疲劳（只改给出的项）")] = None,
    suppress_days: Annotated[int | None, Field(ge=1, le=180, description="「听腻了」的作品暂不推荐几天")] = None,
    discover: Annotated[Discover | None, Field(description="每周雷达（只改给出的项）")] = None,
    mixes: Annotated[Mixes | None, Field(description="风格歌单（只改给出的项）")] = None,
    autoplay: Annotated[Autoplay | None, Field(description="智能续播（只改给出的项）")] = None,
) -> Any:
    """修改音乐推荐的应用配置，立即影响本应用全部用户的推荐（训练参数下次训练生效）。只传要改的字段：工具先读现有配置再合并保存
    （嵌套对象按字段合并），越界由后端回 40000 并说明哪一项。执行前先向用户复述改动（旧值 → 新值）并取得确认。"""
    current = {key: value for key, value in dict(await CONFIG(appkey=app) or {}).items() if key not in _READONLY_CONFIG}
    patch = body(
        enabled=enabled, collectEnabled=collect_enabled, windowDays=window_days, halfLifeDays=half_life_days,
        minItemUsers=min_item_users, artistCap=artist_cap, recentExcludeDays=recent_exclude_days, exploreRatio=explore_ratio,
        schedule=None if schedule is None else schedule.strip(), autoPublish=auto_publish, minLift=min_lift, foldIn=fold_in,
        suppressDays=suppress_days,
    )
    nested = {"weights": weights, "als": als, "itemcf": itemcf, "blend": blend, "ranker": ranker, "fatigue": fatigue,
              "discover": discover, "mixes": mixes, "autoplay": autoplay}
    for key, model in nested.items():
        fields = _payload(model)
        if fields:
            patch[key] = {**(current.get(key) or {}), **fields}
    if not patch:
        raise ToolError("没有要修改的字段")
    return await CONFIG_SAVE(appkey=app, body={**current, **patch})


# ═══════════════════════ 干预规则 ═══════════════════════


@server.tool(**READ("music"))
async def rules(app: AppRef) -> Any:
    """运营干预规则列表：block 屏蔽 / boost 加权 / pin 置顶，目标（作品或歌手）、生效场景、权重或位置、生效时间段、启停、
    近 7 天影响的曝光与点击。"""
    return await RULES(appkey=app)


def _rule_target(work_key: str, source: str, song_id: str, artist: str) -> dict[str, Any] | None:
    given = [bool(work_key.strip()), bool(source.strip() or song_id.strip()), bool(artist.strip())]
    if sum(given) > 1:
        raise ToolError("目标只能给一种：work_key、source + song_id、artist 三选一")
    if work_key.strip():
        return {"type": "work", "workKey": work_key.strip()}
    if source.strip() or song_id.strip():
        if not (source.strip() and song_id.strip()):
            raise ToolError("按音源指定作品时 source 与 song_id 都要给")
        return {"type": "work", "source": source.strip(), "songId": song_id.strip()}
    if artist.strip():
        return {"type": "artist", "artist": artist.strip()}
    return None


def _existing_target(target: dict[str, Any]) -> dict[str, Any]:
    if target.get("type") == "artist":
        return {"type": "artist", "artist": target.get("artist") or ""}
    return {"type": "work", "workKey": target.get("workKey") or ""}


@server.tool(**WRITE("music"))
async def save_rule(
    app: AppRef,
    rule_id: Annotated[int | None, Field(gt=0, description="规则 ID：给了修改这条（只改给出的字段），留空新建")] = None,
    kind: Annotated[Literal["block", "boost", "pin"] | None, Field(description="block 屏蔽（全部场景）/ boost 分数乘 weight / pin 插到第 position 位；新建必填")] = None,
    work_key: Annotated[str, Field(description="目标作品的作品键（与 source + song_id、artist 三选一；新建必填其一）")] = "",
    source: Annotated[str, Field(description="按音源指定目标作品：音源（如 wy）")] = "",
    song_id: Annotated[str, Field(description="按音源指定目标作品：歌曲 ID")] = "",
    artist: Annotated[str, Field(description="目标歌手名")] = "",
    scenes: Annotated[list[Scene] | None, Field(description="生效场景，空列表为全部（block 恒为全部）")] = None,
    weight: Annotated[float | None, Field(ge=0.1, le=10, description="boost 的倍数 0.1–10")] = None,
    position: Annotated[int | None, Field(ge=1, le=20, description="pin 的位置 1–20")] = None,
    starts_at: Annotated[str | None, Field(description="生效开始（ISO 8601，不带时区按北京时间）；空串为不限；修改时不传则不变")] = None,
    ends_at: Annotated[str | None, Field(description="生效结束；空串为不限；修改时不传则不变")] = None,
    enabled: Annotated[bool | None, Field(description="是否启用（新建默认启用）")] = None,
    note: Annotated[str | None, Field(max_length=200, description="备注")] = None,
) -> Any:
    """新建或修改一条运营干预规则，全应用立即生效（影响所有人的推荐）。修改时工具先读出原规则再合并，只改给出的字段。
    执行前先向用户复述规则内容（类型、目标、场景、权重 / 位置、时间段）并取得确认。"""
    target = _rule_target(work_key, source, song_id, artist)
    starts = None if starts_at is None else _ms(starts_at, "starts_at")
    ends = None if ends_at is None else _ms(ends_at, "ends_at")
    if rule_id is None:
        if kind is None or target is None:
            raise ToolError("新建规则时 kind 与目标（work_key / source + song_id / artist）必填")
        if kind == "boost" and weight is None:
            raise ToolError("boost 规则需要 weight")
        if kind == "pin" and position is None:
            raise ToolError("pin 规则需要 position")
        rule: dict[str, Any] = {
            "kind": kind, "target": target, "scenes": [] if kind == "block" else list(scenes or []),
            "weight": weight if kind == "boost" else 1, "position": position if kind == "pin" else 0,
            "startsAt": starts, "endsAt": ends, "enabled": True if enabled is None else enabled, "note": note or "",
        }
        return await RULE_CREATE(appkey=app, body=rule)
    old = await _find(RULES, app, rule_id, "规则", "music_rules")
    rule = {
        "kind": old.get("kind"), "target": _existing_target(old.get("target") or {}), "scenes": list(old.get("scenes") or []),
        "weight": old.get("weight"), "position": old.get("position"), "startsAt": old.get("startsAt"), "endsAt": old.get("endsAt"),
        "enabled": old.get("enabled"), "note": old.get("note") or "",
    }
    rule.update(body(kind=kind, target=target, scenes=None if scenes is None else list(scenes), weight=weight, position=position,
                     enabled=enabled, note=note))
    if starts_at is not None:
        rule["startsAt"] = starts
    if ends_at is not None:
        rule["endsAt"] = ends
    if rule["kind"] == "block":
        rule["scenes"] = []
    return await RULE_UPDATE(appkey=app, id=rule_id, body=rule)


@server.tool(**DESTRUCTIVE("music"))
async def delete_rule(app: AppRef, rule_id: RuleID) -> Any:
    """删除一条运营干预规则，立即不再生效，不可恢复（只想暂停用 music_save_rule 设 enabled=false）。执行前先向用户复述并取得确认。"""
    return await RULE_DELETE(appkey=app, id=rule_id)


# ═══════════════════════ A/B 实验 ═══════════════════════


class Overrides(BaseModel):
    """变体覆盖的推荐参数；不给的沿用应用配置。"""

    blend: Blend | None = Field(None, description="通道融合权重")
    explore_ratio: float | None = Field(None, ge=0, le=0.5, description="探索占比")
    artist_cap: int | None = Field(None, ge=1, le=10, description="同一歌手最多几首")
    ranker: bool | None = Field(None, description="是否用精排")
    fold_in: bool | None = Field(None, description="是否用实时折入")
    model_run_id: int | None = Field(None, gt=0, description="固定用某次训练成功的模型")

    def payload(self) -> dict[str, Any]:
        data = {_camel(key): value for key, value in self.model_dump(exclude_none=True, exclude={"blend"}).items()}
        if self.blend is not None:
            data["blend"] = _payload(self.blend)
        return data


class Variant(BaseModel):
    """一个实验变体。"""

    key: str = Field(pattern=r"^[A-Z]$", description="变体键 A、B、C……（A 为对照组）")
    name: str = Field(min_length=1, max_length=40, description="变体名称")
    traffic: int = Field(ge=0, le=100, description="流量百分比；所有变体之和 ≤ 100，剩下的人不进实验")
    overrides: Overrides = Field(default_factory=Overrides, description="覆盖的推荐参数（对照组一般留空）")

    def payload(self) -> dict[str, Any]:
        return {"key": self.key, "name": self.name, "traffic": self.traffic, "overrides": self.overrides.payload()}


def _check_variants(variants: list[Variant]) -> None:
    keys = [variant.key for variant in variants]
    if len(keys) != len(set(keys)):
        raise ToolError("变体键重复")
    if "A" not in keys:
        raise ToolError("需要一个键为 A 的对照组")
    if sum(variant.traffic for variant in variants) > 100:
        raise ToolError("所有变体的流量之和不能超过 100")


@server.tool(**READ("music"))
async def experiments(app: AppRef) -> Any:
    """A/B 实验列表：名称、场景、状态（draft 草稿 / running 运行中 / stopped 已停止）、各变体流量与覆盖参数、开始与停止时间。"""
    return await EXPERIMENTS(appkey=app)


@server.tool(**WRITE("music"))
async def save_experiment(
    app: AppRef,
    experiment_id: Annotated[int | None, Field(gt=0, description="实验 ID：给了修改这个实验（只改给出的字段），留空新建草稿")] = None,
    name: Annotated[str | None, Field(min_length=1, max_length=60, description="实验名称；新建必填")] = None,
    description: Annotated[str | None, Field(max_length=500, description="说明")] = None,
    scenes: Annotated[list[Scene] | None, Field(min_length=1, description="参与的推荐场景，至少一个；新建必填")] = None,
    variants: Annotated[list[Variant] | None, Field(min_length=2, max_length=10, description="变体（整体替换），至少两个，含对照组 A；新建必填")] = None,
) -> Any:
    """新建（草稿）或修改一个 A/B 实验。用户按「实验 ID:用户 ID」哈希分到 0–99 的桶，命中的变体覆盖本次推荐参数。
    新建只是草稿，music_experiment_action start 才开始分流；修改运行中的实验会立刻改变线上分流，执行前先向用户复述改动并取得确认。"""
    if variants is not None:
        _check_variants(variants)
    if experiment_id is None:
        if not name or not scenes or variants is None:
            raise ToolError("新建实验时 name、scenes、variants 必填")
        return await EXPERIMENT_CREATE(appkey=app, body={
            "name": name, "description": description or "", "scenes": list(scenes), "variants": [v.payload() for v in variants],
        })
    old = await _find(EXPERIMENTS, app, experiment_id, "实验", "music_experiments")
    data = {"name": old.get("name") or "", "description": old.get("description") or "", "scenes": list(old.get("scenes") or []),
            "variants": list(old.get("variants") or [])}
    data.update(body(name=name, description=description, scenes=None if scenes is None else list(scenes),
                     variants=None if variants is None else [v.payload() for v in variants]))
    return await EXPERIMENT_UPDATE(appkey=app, id=experiment_id, body=data)


@server.tool(**DESTRUCTIVE("music"))
async def experiment_action(
    app: AppRef,
    experiment_id: ExperimentID,
    action: Annotated[Literal["start", "stop", "delete"], Field(description=(
        "start 开始分流（同一场景同时只能有一个运行中的实验，冲突回 40900）/ stop 停止 / delete 删除（运行中的不能删，先停止；不可恢复）"
    ))],
) -> Any:
    """开始、停止或删除一个 A/B 实验。开始与停止立即改变线上用户拿到的推荐，删除不可恢复；执行前先向用户复述实验与动作并取得确认。"""
    if action == "start":
        return await EXPERIMENT_START(appkey=app, id=experiment_id)
    if action == "stop":
        return await EXPERIMENT_STOP(appkey=app, id=experiment_id)
    return await EXPERIMENT_DELETE(appkey=app, id=experiment_id)


@server.tool(**READ("music"))
async def experiment_results(
    app: AppRef,
    experiment_id: ExperimentID,
    days: Annotated[int | None, Field(ge=1, le=365, description="看最近几天，留空为整个实验期")] = None,
) -> Any:
    """A/B 实验效果：各变体的用户数、请求、曝光、点击、播放、完播、人均分钟，点击率与播放率相对对照组 A 的提升和显著性（两比例 z 检验 p 值），
    以及每日走势。p < 0.05 才算显著。"""
    return await EXPERIMENT_RESULTS(appkey=app, id=experiment_id, query=q(days=days))
