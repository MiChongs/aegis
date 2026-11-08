"""风控与防火墙：风控平台（场景、发布与灰度、特征、规则、模型、名单、行为序列、决策留痕、案件复核、处置、关系图与画像），以及 IP 封禁、地域封禁、地理围栏与防火墙日志。

风控平台是独立服务 risk-admin：/api/admin/system/risk/<X> 原样转发到它的 /v1/<X>，响应仍是 {code, message, data}，
分页结果为 {list, total, page, pageSize}，应用作用域的查询参数是 app（数字应用 ID）。
读接口对管理员开放；改配置、发布、处置、复核限超级管理员（否则 40313）。

工作流：场景 / 规则 / 特征 / 模型的修改只进草稿，线上决策只认已发布版本——
改完先 risk_validate_scene 编译检查、risk_simulate 用草稿试跑，再 risk_publish（full 直接生效、canary 灰度、shadow 影子），
灰度观察后 risk_promote 转正或 risk_set_traffic 调比例 / 放弃，出问题 risk_rollback 回到任一历史版本。
"""

from __future__ import annotations

from typing import Annotated, Any, Literal

from fastmcp import FastMCP
from fastmcp.exceptions import ToolError
from pydantic import Field

from ..core import DELETE, DESTRUCTIVE, GET, PATCH, POST, PUT, READ, WRITE, IsoTime, Page, app_id_of, body, q

NAMESPACE = "risk"
server = FastMCP("risk")

BASE = "/api/admin/system/risk"
FW = "/api/admin/system/firewall"

# ── 风控平台（risk-admin） ──
META = GET(f"{BASE}/meta")
EXPR_VALIDATE = POST(f"{BASE}/expr/validate")
STATUS = GET(f"{BASE}/status")
AUDIT = GET(f"{BASE}/audit")
SCENES = GET(f"{BASE}/scenes")
SCENE = GET(f"{BASE}/scenes/:key")
SCENE_SAVE = PUT(f"{BASE}/scenes/:key")
SCENE_DELETE = DELETE(f"{BASE}/scenes/:key")
SCENE_BUNDLE = GET(f"{BASE}/scenes/:key/bundle")
SCENE_VALIDATE = POST(f"{BASE}/scenes/:key/validate")
SCENE_SIMULATE = POST(f"{BASE}/scenes/:key/simulate")
SCENE_PUBLISH = POST(f"{BASE}/scenes/:key/publish")
SCENE_PROMOTE = POST(f"{BASE}/scenes/:key/promote")
SCENE_ROLLBACK = POST(f"{BASE}/scenes/:key/rollback")
SCENE_WITHDRAW = POST(f"{BASE}/scenes/:key/withdraw")
SCENE_TRAFFIC = POST(f"{BASE}/scenes/:key/traffic")
SCENE_RELEASES = GET(f"{BASE}/scenes/:key/releases")
RELEASE = GET(f"{BASE}/releases/:id")
FEATURES = GET(f"{BASE}/features")
FEATURE_SAVE = PUT(f"{BASE}/features/:key")
FEATURE_DELETE = DELETE(f"{BASE}/features/:key")
FEATURE_SQL = GET(f"{BASE}/features/:key/sql")
FEATURES_SYNC = POST(f"{BASE}/features/sync")
RULES = GET(f"{BASE}/rules")
RULE_CREATE = POST(f"{BASE}/rules")
RULE_UPDATE = PUT(f"{BASE}/rules/:id")
RULE_DELETE = DELETE(f"{BASE}/rules/:id")
TEMPLATES = GET(f"{BASE}/templates")
TEMPLATE_APPLY = POST(f"{BASE}/templates/apply")
MODELS = GET(f"{BASE}/models")
MODEL_UPDATE = PUT(f"{BASE}/models/:key")
MODEL_DELETE = DELETE(f"{BASE}/models/:key")
MODEL_PREDICT = POST(f"{BASE}/models/:key/predict")
LISTS = GET(f"{BASE}/lists")
LISTS_ADD = POST(f"{BASE}/lists")
LISTS_REMOVE = POST(f"{BASE}/lists/remove")
LISTS_CHECK = POST(f"{BASE}/lists/check")
LISTS_REBUILD = POST(f"{BASE}/lists/rebuild")
PATTERNS = GET(f"{BASE}/cep/patterns")
PATTERN_SAVE = PUT(f"{BASE}/cep/patterns/:key")
PATTERN_DELETE = DELETE(f"{BASE}/cep/patterns/:key")
DASHBOARD = GET(f"{BASE}/dashboard")
DECISIONS = GET(f"{BASE}/decisions")
DECISION = GET(f"{BASE}/decisions/:id")
EVENTS = GET(f"{BASE}/events")
ALERTS = GET(f"{BASE}/alerts")
CASES = GET(f"{BASE}/cases")
CASE_REVIEW = POST(f"{BASE}/cases/:id/review")
DISPOSE = POST(f"{BASE}/dispose")
GRAPH = GET(f"{BASE}/graph")
PROFILE = GET(f"{BASE}/profile")

# ── 防火墙 ──
FW_LOGS = GET(f"{FW}/logs")
FW_LOG = GET(f"{FW}/logs/:logId")
FW_LOGS_CLEANUP = DELETE(f"{FW}/logs")
FW_STATS = GET(f"{FW}/stats")
IP_BANS = GET(f"{FW}/bans")
IP_BAN_MODES = GET(f"{FW}/bans/modes")
IP_BAN = POST(f"{FW}/bans")
IP_UNBAN = DELETE(f"{FW}/bans/:banId")
GEO_BANS = GET(f"{FW}/geo-bans")
GEO_BAN_UPSERT = POST(f"{FW}/geo-bans")
GEO_BAN_TOGGLE = PATCH(f"{FW}/geo-bans/:id")
GEO_BAN_DELETE = DELETE(f"{FW}/geo-bans/:id")
GEO_FENCES = GET(f"{FW}/geo-fences")
GEO_FENCE_CREATE = POST(f"{FW}/geo-fences")
GEO_FENCE_PREVIEW = POST(f"{FW}/geo-fences/preview")
GEO_FENCE_UPDATE = PUT(f"{FW}/geo-fences/:id")
GEO_FENCE_TOGGLE = PATCH(f"{FW}/geo-fences/:id")
GEO_FENCE_DELETE = DELETE(f"{FW}/geo-fences/:id")

OptionalApp = Annotated[str, Field(description="限定应用：appKey 或数字应用 ID；留空为全平台 / 不限")]
PageSize = Annotated[int, Field(ge=1, le=200, description="每页条数（防火墙接口用 pageSize）")]
RiskLimit = Annotated[int, Field(ge=1, le=200, description="每页条数")]
DurationSeconds = Annotated[int, Field(ge=0, description="时长（秒），0 为永久")]
SceneKey = Annotated[str, Field(min_length=1, description=(
    "场景键。内置：login、admin_login、register、password_reset、verify_code、payment、invite、lottery、"
    "signin、cardkey、points_redeem、ad_reward；也可以是自建场景（risk_scenes 可查）"
))]
SceneFilter = Annotated[str, Field(description="按场景键过滤，留空不限")]
Start = Annotated[IsoTime, Field(description="起始时间（RFC 3339），留空用接口默认")]
End = Annotated[IsoTime, Field(description="结束时间（RFC 3339），默认现在")]
RiskAction = Literal["pass", "review", "captcha", "block", "ban"]
RiskLevel = Literal["none", "low", "medium", "high", "critical"]
ListKind = Literal["block", "allow", "watch"]
ListType = Literal["ip", "cidr", "device", "account", "user", "email_domain", "phone_prefix", "asn", "country", "ua_keyword"]
EnforceTarget = Literal["device", "ip", "account", "firewall"]
DisposeTarget = Literal["device", "ip", "account", "user", "ban_account", "firewall"]
Dimension = Literal["network", "device", "account", "behavior", "geo", "client", "list", "graph", "model", "custom"]

# 读回来再提交时去掉的只读字段（服务端会自己填）
_READONLY = ("state", "ruleCount", "builtin", "updatedAt", "updatedBy", "createdAt")


async def _app(app: str) -> int | None:
    return await app_id_of(app) if app and app.strip() else None


def _editable(item: dict[str, Any] | None) -> dict[str, Any]:
    return {k: v for k, v in (item or {}).items() if k not in _READONLY}


def _not_found(exc: ToolError) -> bool:
    return "错误码 40400" in str(exc)


def _find(items: Any, field: str, value: Any) -> dict[str, Any] | None:
    for item in items or []:
        if isinstance(item, dict) and item.get(field) == value:
            return item
    return None


# ═══════════════════════ 元数据、状态、审计 ═══════════════════════


@server.tool(**READ("risk"))
async def metadata() -> Any:
    """风控平台元数据：表达式变量、实体、维度、名单类型、事件类型、关系图模板、流式字段、处置动作、风险等级、Guard 策略与默认分数段。写规则 / 特征 / 场景前先看它。"""
    return await META()


@server.tool(**WRITE("risk", idempotent=True))
async def validate_expression(expr: Annotated[str, Field(min_length=1, description="规则条件表达式（expr-lang，变量见 risk_metadata，特征写 F.<key>、模型写 M.<key>）")]) -> Any:
    """校验一条风控表达式能否编译（不保存）。"""
    return await EXPR_VALIDATE(body={"expr": expr})


@server.tool(**READ("risk"))
async def status() -> Any:
    """风控平台各组件的运行状态（决策服务、存储、流计算、关系图、向量库等）与各场景生效版本。"""
    return await STATUS()


@server.tool(**READ("risk"))
async def audit(limit: Annotated[int, Field(ge=1, le=500, description="条数")] = 100) -> Any:
    """风控平台操作审计：谁在什么时候改了场景、规则、特征、名单，发布、回滚、处置、复核等。"""
    return await AUDIT(query=q(limit=limit))


# ═══════════════════════ 场景 ═══════════════════════


@server.tool(**READ("risk"))
async def scenes(
    scene: Annotated[str, Field(description="给了就取这个场景的配置与发布状态；留空列出全部场景")] = "",
    bundle: Annotated[bool, Field(description="改为返回该场景的草稿发布包（场景 + 规则 + 特征 + 模型，发布时就是它）")] = False,
) -> Any:
    """风控场景列表（含发布状态与规则数），或单个场景的配置 / 草稿发布包。"""
    if bundle:
        if not scene:
            raise ToolError("bundle=true 需要给 scene")
        return await SCENE_BUNDLE(key=scene)
    if scene:
        return await SCENE(key=scene)
    return await SCENES()


@server.tool(**WRITE("risk", idempotent=True))
async def save_scene(
    scene: SceneKey,
    name: Annotated[str | None, Field(description="名称")] = None,
    description: Annotated[str | None, Field(description="说明")] = None,
    app_ids: Annotated[list[int] | None, Field(description="只对这些应用（数字 ID）生效；空列表为全部应用")] = None,
    fields: Annotated[list[dict[str, Any]] | None, Field(description="场景携带的业务字段 [{key, label, type: string|number|bool|list|object, required}]（整体替换）")] = None,
    timeout_ms: Annotated[int | None, Field(ge=1, description="决策超时（毫秒）")] = None,
    fail_mode: Annotated[Literal["open", "closed"] | None, Field(description="决策出错 / 超时时：open 放行、closed 拒绝")] = None,
    captcha_supported: Annotated[bool | None, Field(description="接入方能弹图形验证码（否则 captcha 处置转人工复核）")] = None,
    default_action: Annotated[RiskAction | None, Field(description="没有任何规则 / 分数段命中时的处置")] = None,
    enforcement_targets: Annotated[list[EnforceTarget] | None, Field(description="最终处置为 ban 时封禁的对象")] = None,
    enforcement_duration_seconds: Annotated[int | None, Field(ge=0, description="ban 的封禁时长（秒），0 为永久")] = None,
    auto_ban_allowed: Annotated[bool | None, Field(description="允许自动封禁；false 时 ban 降为 block（只拦不封）")] = None,
    list_types: Annotated[list[ListType] | None, Field(description="本场景查哪些名单类型（整体替换）")] = None,
    features: Annotated[list[str] | None, Field(description="本场景用到的特征键（整体替换，见 risk_features）")] = None,
    models: Annotated[list[str] | None, Field(description="本场景用到的模型键（整体替换，见 risk_models）")] = None,
    bands: Annotated[list[dict[str, Any]] | None, Field(description="分数段 [{min, max（含，<=0 为不封顶）, action, level}]（整体替换）")] = None,
    flow: Annotated[dict[str, Any] | None, Field(description="决策流（JDM 决策图 JSON，整体替换）")] = None,
    policy: Annotated[str | None, Field(description="Rego 策略（package aegis.risk.scene），整体替换；空串为不用策略")] = None,
    enabled: Annotated[bool | None, Field(description="启用 / 停用")] = None,
) -> Any:
    """新建或修改风控场景（只传要改的字段，其余沿用当前配置；场景不存在则新建）。只改草稿，risk_publish 后才影响线上。"""
    patch = body(
        name=name, description=description, appIds=app_ids, fields=fields, timeoutMs=timeout_ms, failMode=fail_mode,
        captchaSupported=captcha_supported, defaultAction=default_action, listTypes=list_types, features=features,
        models=models, bands=bands, flow=flow, policy=policy, enabled=enabled,
    )
    enforcement = body(targets=enforcement_targets, durationSeconds=enforcement_duration_seconds, autoBanAllowed=auto_ban_allowed)
    if not patch and not enforcement:
        raise ToolError("没有要修改的字段")
    try:
        current = _editable(await SCENE(key=scene))
    except ToolError as exc:
        if not _not_found(exc):
            raise
        current = {}
    payload = {**current, **patch, "key": scene}
    if enforcement:
        payload["enforcement"] = {**(current.get("enforcement") or {}), **enforcement}
    return await SCENE_SAVE(key=scene, body=payload)


@server.tool(**DESTRUCTIVE("risk"))
async def delete_scene(scene: SceneKey) -> Any:
    """删除一个风控场景（不可恢复；还有生效发布的要先 risk_withdraw 下线）。"""
    return await SCENE_DELETE(key=scene)


@server.tool(**WRITE("risk", idempotent=True))
async def validate_scene(scene: SceneKey) -> Any:
    """编译检查场景草稿（场景 + 规则 + 特征 + 模型），返回是否可发布与问题列表。发布前必跑。"""
    return await SCENE_VALIDATE(key=scene)


@server.tool(**WRITE("risk", idempotent=True))
async def simulate(
    scene: SceneKey,
    app: OptionalApp = "",
    user_id: Annotated[int | None, Field(gt=0, description="用户 ID")] = None,
    account: Annotated[str, Field(description="账号")] = "",
    ip: Annotated[str, Field(description="IP")] = "",
    user_agent: Annotated[str, Field(description="User-Agent")] = "",
    device: Annotated[dict[str, Any] | None, Field(description="设备 {id, platform, brand, model, osVersion, appVersion, screen, locale, timezone, emulator, rooted, attrs}")] = None,
    geo: Annotated[dict[str, Any] | None, Field(description="网络情报 {country, province, city, isp, asn, proxy, vpn, tor, datacenter, ipScore}")] = None,
    extra: Annotated[dict[str, Any] | None, Field(description="场景业务字段（见场景 fields）")] = None,
    captcha_passed: Annotated[bool, Field(description="假设已通过图形验证码")] = False,
    request_fields: Annotated[dict[str, Any] | None, Field(description="其他请求字段，原样并入 request（如 phone、email、amount、targetUserId）")] = None,
    draft: Annotated[bool, Field(description="用草稿（true）还是当前生效版本（false）")] = True,
    mode: Annotated[Literal["fixed", "live"], Field(description="fixed：特征值与名单命中由 features / lists 给定；live：走决策服务的真实数据源（只读）")] = "fixed",
    features: Annotated[dict[str, float] | None, Field(description="fixed 模式的特征值 {特征键: 数值}")] = None,
    lists: Annotated[list[dict[str, Any]] | None, Field(description="fixed 模式的名单命中 [{listType, kind, value}]")] = None,
) -> Any:
    """模拟一次风控决策：返回处置、分数、命中规则、特征值与执行轨迹，不落库、不执行处置。改规则后、发布前先用它验证。"""
    request = body(
        appId=await _app(app), userId=user_id, account=account or None, ip=ip or None, userAgent=user_agent or None,
        device=device, geo=geo, extra=extra,
    )
    if captcha_passed:
        request["captchaPassed"] = True
    if request_fields:
        request.update(request_fields)
    return await SCENE_SIMULATE(key=scene, body=body(request=request, draft=draft, mode=mode, features=features, lists=lists))


# ═══════════════════════ 发布、灰度、回滚 ═══════════════════════


@server.tool(**READ("risk"))
async def releases(
    scene: Annotated[str, Field(description="列出这个场景的发布历史与当前状态（正式 / 候选版本、灰度比例、影子）")] = "",
    release_id: Annotated[int | None, Field(gt=0, description="给了就取这个版本的详情（含完整发布包）")] = None,
) -> Any:
    """场景的发布历史与当前流量状态，或给 release_id 取单个版本详情。"""
    if release_id:
        return await RELEASE(id=release_id)
    if not scene:
        raise ToolError("需要 scene 或 release_id")
    return await SCENE_RELEASES(key=scene)


@server.tool(**DESTRUCTIVE("risk"))
async def publish(
    scene: SceneKey,
    note: Annotated[str, Field(description="发布说明")] = "",
    mode: Annotated[Literal["full", "canary", "shadow"], Field(description="full 直接成为正式版本；canary 作为候选按比例灰度；shadow 作为候选影子运行（只留痕不处置）。场景第一次只能 full")] = "full",
    canary_percent: Annotated[int | None, Field(ge=1, le=99, description="canary 的灰度比例（1-99）")] = None,
) -> Any:
    """把场景草稿编译后发布成新版本并下发到决策服务（立即影响线上）。先 risk_validate_scene、risk_simulate，并向用户确认。"""
    if mode == "canary" and canary_percent is None:
        raise ToolError("canary 发布需要 canary_percent")
    return await SCENE_PUBLISH(key=scene, body=body(note=note, mode=mode, canaryPercent=canary_percent))


@server.tool(**DESTRUCTIVE("risk"))
async def promote(scene: SceneKey) -> Any:
    """把场景的候选版本（灰度 / 影子）转为正式版本，全量生效。"""
    return await SCENE_PROMOTE(key=scene)


@server.tool(**DESTRUCTIVE("risk"))
async def set_traffic(
    scene: SceneKey,
    canary_percent: Annotated[int | None, Field(ge=1, le=99, description="候选版本的灰度比例（1-99）")] = None,
    shadow: Annotated[bool, Field(description="候选版本改为影子运行（只留痕不处置）")] = False,
    drop: Annotated[bool, Field(description="放弃候选版本，全部流量回到正式版本")] = False,
) -> Any:
    """调整候选版本的流量：改灰度比例、切影子，或放弃候选。"""
    if not drop and not shadow and canary_percent is None:
        raise ToolError("需要 canary_percent / shadow / drop 之一")
    return await SCENE_TRAFFIC(key=scene, body=body(canaryPercent=canary_percent or 0, shadow=shadow, drop=drop))


@server.tool(**DESTRUCTIVE("risk"))
async def rollback(
    scene: SceneKey,
    release_id: Annotated[int, Field(gt=0, description="要回到的历史版本 ID（risk_releases 可查）")],
) -> Any:
    """把场景的正式版本切回任一历史版本（立即生效，同时放弃候选版本）。"""
    return await SCENE_ROLLBACK(key=scene, body={"releaseId": release_id})


@server.tool(**DESTRUCTIVE("risk"))
async def withdraw(scene: SceneKey) -> Any:
    """下线场景：不再评估，接入方按无发布处理（Guard 默认放行）。需要恢复时重新发布或回滚。"""
    return await SCENE_WITHDRAW(key=scene)


# ═══════════════════════ 特征 ═══════════════════════


@server.tool(**READ("risk"))
async def features(
    feature: Annotated[str, Field(description="给了就只返回这个特征")] = "",
    sql: Annotated[bool, Field(description="改为返回该流式特征生成的流计算 SQL 预览")] = False,
) -> Any:
    """风控特征列表（滑窗 velocity、流式 stream、关系图 graph、向量 vector、派生 derived），或单个特征 / 其流计算 SQL。"""
    if sql:
        if not feature:
            raise ToolError("sql=true 需要给 feature")
        return await FEATURE_SQL(key=feature)
    items = await FEATURES()
    if not feature:
        return items
    found = _find(items, "key", feature)
    if found is None:
        raise ToolError(f"没有特征 {feature}")
    return found


@server.tool(**WRITE("risk", idempotent=True))
async def save_feature(
    feature: Annotated[str, Field(min_length=1, description="特征键（小写字母、数字、下划线，字母开头；表达式里写 F.<key>）")],
    kind: Annotated[Literal["velocity", "stream", "graph", "vector", "derived"] | None, Field(description="类型（新建必填）")] = None,
    name: Annotated[str | None, Field(description="名称")] = None,
    description: Annotated[str | None, Field(description="说明")] = None,
    entity: Annotated[str | None, Field(description="统计主体（user / device / ip / account 等，见 risk_metadata 的 entities）")] = None,
    cross_app: Annotated[bool | None, Field(description="跨应用统计（global）；默认按应用隔离")] = None,
    default_value: Annotated[float | None, Field(description="取不到时的默认值")] = None,
    velocity: Annotated[dict[str, Any] | None, Field(description="velocity 定义 {windowSeconds, agg: count|distinct, distinctOf, allScenes}")] = None,
    stream: Annotated[dict[str, Any] | None, Field(description="stream 定义 {events[], scenes[], agg: count|distinct|sum|max|avg, field, windowSeconds, conditions[{field, op, value}]}")] = None,
    graph: Annotated[dict[str, Any] | None, Field(description="graph 定义 {template（见 risk_metadata 的 graphTemplates）, withinDays, cacheTtl}")] = None,
    vector: Annotated[dict[str, Any] | None, Field(description="vector 定义 {collection: device|behavior, topK, minScore, output: similar_count|similar_banned|max_score}")] = None,
    expr: Annotated[str | None, Field(description="derived 的表达式（可引用其他特征 F.<key>）")] = None,
) -> Any:
    """新建或修改风控特征（只传要改的字段，其余沿用当前定义；保存前服务端会编译校验）。场景引用后经发布生效。"""
    current = _editable(_find(await FEATURES(), "key", feature))
    if not current and kind is None:
        raise ToolError(f"特征 {feature} 不存在，新建需要 kind")
    patch = body(
        kind=kind, name=name, description=description, entity=entity, default=default_value,
        velocity=velocity, stream=stream, graph=graph, vector=vector, expr=expr,
    )
    if cross_app is not None:
        patch["global"] = cross_app
    if current and not patch:
        raise ToolError("没有要修改的字段")
    return await FEATURE_SAVE(key=feature, body={**current, **patch, "key": feature})


@server.tool(**DESTRUCTIVE("risk"))
async def delete_feature(feature: Annotated[str, Field(min_length=1, description="特征键")]) -> Any:
    """删除一个风控特征（不可恢复；还被场景或规则引用时草稿会编译失败）。"""
    return await FEATURE_DELETE(key=feature)


@server.tool(**WRITE("risk", idempotent=True))
async def sync_features() -> Any:
    """按已发布版本重新同步流式特征的流计算作业（缺的建、多的删），作业状态不一致时用。"""
    return await FEATURES_SYNC()


# ═══════════════════════ 规则与模板 ═══════════════════════


@server.tool(**READ("risk"))
async def rules(
    rule_id: Annotated[int | None, Field(gt=0, description="给了就只返回这条规则")] = None,
    scene: Annotated[str, Field(description="只看这个场景的规则（含 * 全场景规则）；留空为全部")] = "",
) -> Any:
    """风控规则列表（按优先级），或给 rule_id 取一条。"""
    items = await RULES(query=q(scene=scene))
    if not rule_id:
        return items
    found = _find(items, "id", rule_id)
    if found is None:
        raise ToolError(f"没有规则 {rule_id}")
    return found


@server.tool(**WRITE("risk"))
async def save_rule(
    rule_id: Annotated[int | None, Field(gt=0, description="给了就修改这条规则（只传要改的字段）；留空为新建")] = None,
    scene: Annotated[str | None, Field(description="所属场景键；* 为全部场景（新建必填）")] = None,
    key: Annotated[str | None, Field(description="规则键（小写字母、数字、下划线，字母开头；同场景唯一，新建必填）")] = None,
    name: Annotated[str | None, Field(description="名称")] = None,
    description: Annotated[str | None, Field(description="说明")] = None,
    dimension: Annotated[Dimension | None, Field(description="维度")] = None,
    expr: Annotated[str | None, Field(description="命中条件（expr-lang 表达式，新建必填；变量见 risk_metadata，可先 risk_validate_expression）")] = None,
    score: Annotated[int | None, Field(ge=0, le=1000, description="命中加分")] = None,
    mode: Annotated[Literal["enforce", "observe"] | None, Field(description="enforce 计分生效、observe 只留痕不计分（新规则建议先观察）")] = None,
    decision: Annotated[Literal["", "pass", "review", "captcha", "block", "ban"] | None, Field(description="命中即强制的处置；空串为只计分")] = None,
    priority: Annotated[int | None, Field(description="优先级（大的先跑）")] = None,
    reason: Annotated[str | None, Field(description="命中原因文案，可含 {{F.x}} 等占位")] = None,
    tags: Annotated[list[str] | None, Field(description="标签（整体替换）")] = None,
    enabled: Annotated[bool | None, Field(description="启用 / 停用")] = None,
) -> Any:
    """新建或修改风控规则。只改草稿，所属场景 risk_publish 后才影响线上；建议先以 observe 模式发布观察。"""
    patch = body(
        scene=scene, key=key, name=name, description=description, dimension=dimension, expr=expr, score=score,
        mode=mode, decision=decision, priority=priority, reason=reason, tags=tags, enabled=enabled,
    )
    if rule_id:
        if not patch:
            raise ToolError("没有要修改的字段")
        current = _find(await RULES(), "id", rule_id)
        if current is None:
            raise ToolError(f"没有规则 {rule_id}")
        return await RULE_UPDATE(id=rule_id, body={**_editable(current), **patch})
    missing = [n for n, v in (("scene", scene), ("key", key), ("expr", expr)) if not v]
    if missing:
        raise ToolError(f"新建规则需要 {' / '.join(missing)}")
    patch.setdefault("enabled", True)
    patch.setdefault("mode", "enforce")
    return await RULE_CREATE(body=patch)


@server.tool(**DESTRUCTIVE("risk"))
async def delete_rule(rule_id: Annotated[int, Field(gt=0, description="规则 ID")]) -> Any:
    """删除一条风控规则（不可恢复；只想暂停用 risk_save_rule enabled=false）。发布后才从线上消失。"""
    return await RULE_DELETE(id=rule_id)


@server.tool(**READ("risk"))
async def templates() -> Any:
    """规则模板目录（成套的常用规则，可一键装到某个场景）。"""
    return await TEMPLATES()


@server.tool(**WRITE("risk"))
async def apply_template(
    template: Annotated[str, Field(min_length=1, description="模板键（risk_templates 结果里的 key）")],
    scene: SceneKey,
    observe: Annotated[bool, Field(description="以观察模式安装（只留痕不计分），建议先观察")] = True,
) -> Any:
    """把规则模板装到一个场景（装完即普通规则，进草稿，发布后生效）。"""
    return await TEMPLATE_APPLY(body={"template": template, "scene": scene, "observe": observe})


# ═══════════════════════ 模型 ═══════════════════════


ModelKey = Annotated[str, Field(min_length=1, description="模型键（表达式里写 M.<key>）")]


@server.tool(**READ("risk"))
async def models() -> Any:
    """已上传的树模型（LightGBM / XGBoost）：输入顺序、输出变换、大小与更新人。模型文件上传是 multipart，只能在控制台操作。"""
    return await MODELS()


@server.tool(**WRITE("risk", idempotent=True))
async def update_model(
    model: ModelKey,
    name: Annotated[str | None, Field(description="名称")] = None,
    description: Annotated[str | None, Field(description="说明；不传则保留原说明")] = None,
    inputs: Annotated[list[str] | None, Field(description="输入顺序：特征键或请求字段（amount、device.emulator 等），个数必须等于模型特征数")] = None,
    transform: Annotated[bool | None, Field(description="是否套模型自带的输出变换（如 sigmoid）")] = None,
) -> Any:
    """修改模型的名称、说明、输入映射与输出变换（不换模型文件；换文件到控制台上传）。"""
    if name is None and description is None and inputs is None and transform is None:
        raise ToolError("没有要修改的字段")
    if description is None:
        current = _find(await MODELS(), "key", model)
        if current is None:
            raise ToolError(f"没有模型 {model}")
        description = str(current.get("description") or "")
    return await MODEL_UPDATE(key=model, body=body(name=name, description=description, inputs=inputs, transform=transform))


@server.tool(**DESTRUCTIVE("risk"))
async def delete_model(model: ModelKey) -> Any:
    """删除一个模型（不可恢复；还被场景引用时草稿会编译失败）。"""
    return await MODEL_DELETE(key=model)


@server.tool(**WRITE("risk", idempotent=True))
async def predict_model(
    model: ModelKey,
    values: Annotated[dict[str, float], Field(description="输入值 {输入名: 数值}；没给的输入按 0")],
) -> Any:
    """用一组输入试算模型分数（不落库）。"""
    return await MODEL_PREDICT(key=model, body={"values": values})


# ═══════════════════════ 名单 ═══════════════════════


@server.tool(name="lists", **READ("risk"))
async def list_entries(
    list_type: Annotated[Literal["", "ip", "cidr", "device", "account", "user", "email_domain", "phone_prefix", "asn", "country", "ua_keyword"], Field(description="名单类型")] = "",
    kind: Annotated[Literal["", "block", "allow", "watch"], Field(description="block 黑名单、allow 白名单、watch 观察名单")] = "",
    value: Annotated[str, Field(description="对象值")] = "",
    source: Annotated[str, Field(description="来源（如 manual、decision、cep、review）")] = "",
    active: Annotated[bool | None, Field(description="默认只看生效中；false 时包含已过期 / 已移除")] = None,
    app: OptionalApp = "",
    page: Page = 1,
    limit: RiskLimit = 50,
) -> Any:
    """风控名单（黑 / 白 / 观察 × 十种对象类型）。"""
    return await LISTS(query=q(
        type=list_type, kind=kind, value=value, app=await _app(app), source=source, active=active, page=page, limit=limit,
    ))


@server.tool(**DESTRUCTIVE("risk"))
async def add_list(
    list_type: Annotated[ListType, Field(description="名单类型：ip、cidr 网段、device、account、user 用户 ID、email_domain、phone_prefix、asn、country、ua_keyword")],
    values: Annotated[list[str], Field(min_length=1, description="对象值，一项一个；已在名单里的对象会被改写")],
    kind: Annotated[ListKind, Field(description="block 直接拦、allow 直接放、watch 交给规则加分")],
    app: OptionalApp = "",
    scenes: Annotated[list[str] | None, Field(description="只在这些场景生效，留空为全部场景")] = None,
    reason: Annotated[str, Field(description="原因")] = "",
    duration_seconds: DurationSeconds = 0,
) -> Any:
    """批量写入风控名单，立即对线上决策生效（不需要发布）。执行前先向用户确认对象。"""
    return await LISTS_ADD(body=body(
        listType=list_type, kind=kind, values=values, appId=await _app(app), scenes=scenes,
        reason=reason, durationSeconds=duration_seconds,
    ))


@server.tool(**DESTRUCTIVE("risk"))
async def remove_lists(entry_ids: Annotated[list[str], Field(min_length=1, description="名单条目 ID 列表（risk_lists 结果里的 id）")]) -> Any:
    """移除风控名单条目，立即生效。移除黑名单即解除拦截。"""
    return await LISTS_REMOVE(body={"ids": entry_ids})


@server.tool(**WRITE("risk", idempotent=True))
async def check_lists(
    values: Annotated[dict[str, str], Field(min_length=1, description="要查的对象 {名单类型: 值}，如 {\"ip\": \"1.1.1.1\", \"device\": \"d1\"}")],
    app: OptionalApp = "",
    scene: SceneFilter = "",
) -> Any:
    """检测一组对象命中了哪些名单（不跑规则）。"""
    return await LISTS_CHECK(body=body(appId=await _app(app), scene=scene or None, values=values))


@server.tool(**DESTRUCTIVE("risk"))
async def rebuild_lists() -> Any:
    """从数据库全量重建名单的 Redis 索引（索引丢失或怀疑不一致时用，重建期间名单判定可能短暂不准）。"""
    return await LISTS_REBUILD()


# ═══════════════════════ 行为序列（CEP） ═══════════════════════


PatternKey = Annotated[str, Field(min_length=1, description="模式键（小写字母、数字、下划线，字母开头）")]


@server.tool(**READ("risk"))
async def cep_patterns() -> Any:
    """行为序列模式（Flink CEP）：按用户 / 设备 / IP / 账号在时间窗内匹配事件序列，命中后加名单或开案件。"""
    return await PATTERNS()


@server.tool(**WRITE("risk", idempotent=True))
async def save_cep_pattern(
    pattern: PatternKey,
    name: Annotated[str | None, Field(description="名称")] = None,
    description: Annotated[str | None, Field(description="说明")] = None,
    enabled: Annotated[bool | None, Field(description="启用 / 停用")] = None,
    key_by: Annotated[Literal["user", "device", "ip", "account"] | None, Field(description="按什么分组匹配")] = None,
    app_ids: Annotated[list[int] | None, Field(description="只对这些应用（数字 ID）；空列表为全部")] = None,
    steps: Annotated[list[dict[str, Any]] | None, Field(description=(
        "步骤（整体替换）[{name, events[], scenes[], actions[], conditions[{field, op, value}], times, contiguous, distinct, absent}]；"
        "事件类型与字段见 risk_metadata 的 eventTypes / streamColumns"
    ))] = None,
    within_seconds: Annotated[int | None, Field(ge=1, description="整个序列的时间窗（秒）")] = None,
    severity: Annotated[Literal["low", "medium", "high", "critical"] | None, Field(description="告警严重程度")] = None,
    actions: Annotated[list[dict[str, Any]] | None, Field(description="命中后的动作（整体替换）[{kind: list_add|case, target: device|ip|account|user, listKind, durationSeconds}]")] = None,
) -> Any:
    """新建或修改行为序列模式（只传要改的字段，其余沿用当前定义）。保存即被 Flink 作业拉取，不需要发布。"""
    patch = body(
        name=name, description=description, enabled=enabled, keyBy=key_by, appIds=app_ids, steps=steps,
        withinSeconds=within_seconds, severity=severity, actions=actions,
    )
    if not patch:
        raise ToolError("没有要修改的字段")
    current = _editable(_find(await PATTERNS(), "key", pattern))
    if not current and not steps:
        raise ToolError(f"模式 {pattern} 不存在，新建需要 steps")
    return await PATTERN_SAVE(key=pattern, body={**current, **patch, "key": pattern})


@server.tool(**DESTRUCTIVE("risk"))
async def delete_cep_pattern(pattern: PatternKey) -> Any:
    """删除一个行为序列模式（不可恢复；只想暂停用 risk_save_cep_pattern enabled=false）。"""
    return await PATTERN_DELETE(key=pattern)


# ═══════════════════════ 大盘与留痕 ═══════════════════════


@server.tool(**READ("risk"))
async def dashboard(app: OptionalApp = "", start: Annotated[IsoTime, Field(description="起始时间（RFC 3339），默认 24 小时前")] = "", end: End = "") -> Any:
    """风控大盘：决策量、各处置 / 场景分布与趋势、命中最多的规则、决策耗时（均值 / P50 / P99）。"""
    return await DASHBOARD(query=q(app=await _app(app), **{"from": start, "to": end}))


@server.tool(**READ("risk"))
async def decisions(
    decision_id: Annotated[str, Field(description="给了就取这一次决策的详情（请求环境、特征、命中、轨迹）")] = "",
    app: OptionalApp = "",
    scene: SceneFilter = "",
    action: Annotated[Literal["", "pass", "review", "captcha", "block", "ban"], Field(description="处置")] = "",
    user_id: Annotated[int | None, Field(gt=0, description="用户 ID")] = None,
    device_id: Annotated[str, Field(description="设备 ID")] = "",
    ip: Annotated[str, Field(description="IP")] = "",
    account: Annotated[str, Field(description="账号")] = "",
    rule: Annotated[str, Field(description="命中过这个规则键")] = "",
    release: Annotated[str, Field(description="由这个发布版本做出的决策")] = "",
    shadow: Annotated[bool, Field(description="看影子版本的留痕（默认看正式决策）")] = False,
    start: Annotated[IsoTime, Field(description="起始时间（RFC 3339），默认 24 小时前")] = "",
    end: End = "",
    page: Page = 1,
    limit: RiskLimit = 50,
) -> Any:
    """风控决策留痕（排查起点：按用户 / 设备 / IP / 账号 / 规则 / 处置 / 时间筛），或给 decision_id 取详情。"""
    if decision_id:
        return await DECISION(id=decision_id)
    return await DECISIONS(query=q(
        app=await _app(app), scene=scene, action=action, user=user_id, device=device_id, ip=ip, account=account,
        rule=rule, release=release, shadow=True if shadow else None, page=page, limit=limit, **{"from": start, "to": end},
    ))


@server.tool(**READ("risk"))
async def events(
    app: OptionalApp = "",
    user_id: Annotated[int | None, Field(gt=0, description="用户 ID")] = None,
    event_type: Annotated[str, Field(description="事件类型（如 login.fail、payment.paid、decision，见 risk_metadata 的 eventTypes）")] = "",
    device_id: Annotated[str, Field(description="设备 ID")] = "",
    ip: Annotated[str, Field(description="IP")] = "",
    start: Annotated[IsoTime, Field(description="起始时间（RFC 3339），默认 7 天前")] = "",
    end: End = "",
    limit: Annotated[int, Field(ge=1, le=500, description="条数")] = 50,
) -> Any:
    """风控行为事件流（业务上报的登录、支付、邀请等事件），流式特征与行为序列都基于它。"""
    return await EVENTS(query=q(
        app=await _app(app), user=user_id, type=event_type, device=device_id, ip=ip, limit=limit, **{"from": start, "to": end},
    ))


@server.tool(**READ("risk"))
async def alerts(
    app: OptionalApp = "",
    pattern: Annotated[str, Field(description="行为序列模式键")] = "",
    user_id: Annotated[int | None, Field(gt=0, description="用户 ID")] = None,
    start: Annotated[IsoTime, Field(description="起始时间（RFC 3339），默认 7 天前")] = "",
    end: End = "",
    limit: Annotated[int, Field(ge=1, le=500, description="条数")] = 100,
) -> Any:
    """行为序列（CEP）告警：哪个模式在什么对象上命中、涉及哪些事件。"""
    return await ALERTS(query=q(app=await _app(app), pattern=pattern, user=user_id, limit=limit, **{"from": start, "to": end}))


# ═══════════════════════ 案件复核与人工处置 ═══════════════════════


@server.tool(**READ("risk"))
async def cases(
    status: Annotated[Literal["", "pending", "closed"], Field(description="pending 待复核、closed 已复核")] = "pending",
    app: OptionalApp = "",
    user_id: Annotated[int | None, Field(gt=0, description="用户 ID")] = None,
    scene: SceneFilter = "",
    page: Page = 1,
    limit: RiskLimit = 50,
) -> Any:
    """风控案件（review 处置与行为序列开出的人工复核单）。"""
    return await CASES(query=q(status=status, app=await _app(app), user=user_id, scene=scene, page=page, limit=limit))


@server.tool(**DESTRUCTIVE("risk"))
async def review_case(
    case_id: Annotated[int, Field(gt=0, description="案件 ID")],
    verdict: Annotated[Literal["approve", "reject"], Field(description="approve 放行（没问题）、reject 确认有风险")],
    note: Annotated[str, Field(description="复核意见")] = "",
    allow: Annotated[bool, Field(description="approve 时把案件里的对象加白")] = False,
    dispose_kind: Annotated[ListKind | None, Field(description="reject 时的处置：block 拉黑、allow 加白、watch 观察；留空只结案不处置")] = None,
    dispose_targets: Annotated[list[DisposeTarget] | None, Field(description="reject 处置的对象（device / ip / account / user 进名单，ban_account 封账号，firewall 防火墙封 IP）")] = None,
    dispose_duration_seconds: DurationSeconds = 0,
    dispose_reason: Annotated[str, Field(description="处置原因；留空用「复核：意见」")] = "",
) -> Any:
    """复核一个风控案件并结案：reject 可按所选对象拉黑 / 封禁，approve 可把对象加白。执行前先向用户确认。"""
    dispose_payload = None
    if dispose_kind or dispose_targets:
        if verdict != "reject":
            raise ToolError("只有 reject 才能附带处置")
        if not dispose_targets:
            raise ToolError("处置需要 dispose_targets")
        dispose_payload = body(
            kind=dispose_kind or "block", targets=dispose_targets, durationSeconds=dispose_duration_seconds,
            reason=dispose_reason or None,
        )
    return await CASE_REVIEW(id=case_id, body=body(verdict=verdict, note=note, allow=allow, dispose=dispose_payload))


@server.tool(**DESTRUCTIVE("risk"))
async def dispose(
    targets: Annotated[list[DisposeTarget], Field(min_length=1, description="处置对象：device / ip / account / user 进名单，ban_account 封账号，firewall 防火墙封 IP（全平台，仅 block）")],
    kind: Annotated[ListKind, Field(description="block 拉黑、allow 加白、watch 观察")] = "block",
    app: OptionalApp = "",
    user_id: Annotated[int | None, Field(gt=0, description="用户 ID")] = None,
    account: Annotated[str, Field(description="账号")] = "",
    device_id: Annotated[str, Field(description="设备 ID")] = "",
    ip: Annotated[str, Field(description="IP")] = "",
    duration_seconds: DurationSeconds = 0,
    reason: Annotated[str, Field(description="原因")] = "",
) -> Any:
    """人工处置：把用户 / 账号 / 设备 / IP 拉黑、加白或观察，或封账号、防火墙封 IP。立即生效，执行前先向用户确认。"""
    if not any((user_id, account, device_id, ip)):
        raise ToolError("需要 user_id / account / device_id / ip 之一来确定对象")
    return await DISPOSE(body=body(
        appId=await _app(app), userId=user_id, account=account or None, deviceId=device_id or None, ip=ip or None,
        kind=kind, targets=targets, durationSeconds=duration_seconds, reason=reason,
    ))


# ═══════════════════════ 关系图与画像 ═══════════════════════


@server.tool(**READ("risk"))
async def graph(
    entity_type: Annotated[Literal["user", "device", "ip", "account"], Field(description="中心对象类型")] = "user",
    app: OptionalApp = "",
    user_id: Annotated[int | None, Field(gt=0, description="type=user 时的用户 ID")] = None,
    value: Annotated[str, Field(description="device / ip / account 的值（user 也可直接填用户 ID）")] = "",
    depth: Annotated[int, Field(ge=1, le=3, description="展开层数")] = 2,
    fanout: Annotated[int, Field(ge=5, le=200, description="每个节点最多展开的邻居数")] = 40,
    max_nodes: Annotated[int, Field(ge=20, le=800, description="最多返回的节点数")] = 300,
) -> Any:
    """以一个用户 / 设备 / IP / 账号为中心展开关联关系图（共用设备、共用 IP、邀请关系等，含被封标记），查团伙用。"""
    if entity_type == "user" and not user_id and not value:
        raise ToolError("type=user 需要 user_id（以及 app）")
    if entity_type != "user" and not value:
        raise ToolError(f"type={entity_type} 需要 value")
    return await GRAPH(query=q(
        type=entity_type, app=await _app(app), user=user_id, value=value, depth=depth, fanout=fanout, max=max_nodes,
    ))


@server.tool(**READ("risk"))
async def profile(
    app: Annotated[str, Field(min_length=1, description="应用：appKey 或数字应用 ID")],
    user_id: Annotated[int, Field(gt=0, description="用户 ID")],
    days: Annotated[int, Field(ge=1, le=180, description="统计最近 N 天")] = 30,
) -> Any:
    """用户风险画像：近 N 天决策汇总与逐日趋势、用过的设备与 IP、最近的特征 / 模型分、所在名单、设备指纹与相似设备。"""
    return await PROFILE(query=q(app=await _app(app), user=user_id, days=days))


# ═══════════════════════ 防火墙：IP 封禁 ═══════════════════════


@server.tool(**READ("risk"))
async def ip_bans(
    modes: Annotated[bool, Field(description="改为返回可选的封禁响应模式与默认模式")] = False,
    ip: Annotated[str, Field(description="IP")] = "",
    status: Annotated[Literal["", "all", "active", "expired", "revoked"], Field(description="状态")] = "",
    source: Annotated[str, Field(description="来源（all / manual / auto / risk）")] = "",
    page: Page = 1,
    page_size: PageSize = 20,
) -> Any:
    """防火墙 IP 封禁记录，或 modes=true 列出可选的封禁响应模式。仅超级管理员。"""
    if modes:
        return await IP_BAN_MODES()
    return await IP_BANS(query=q(ip=ip, status=status, source=source, page=page, pageSize=page_size))


@server.tool(**DESTRUCTIVE("risk"))
async def ban_ip(
    ip: Annotated[str, Field(min_length=1, description="IP 地址")],
    reason: Annotated[str, Field(description="原因")] = "",
    duration: DurationSeconds = 0,
    mode: Annotated[str, Field(description="封禁响应模式（如 forbidden、silent_drop、tarpit、stealth_404，见 risk_ip_bans modes=true）；留空用全局默认")] = "",
    delay_ms: Annotated[int | None, Field(ge=0, description="tarpit 模式的延迟毫秒数")] = None,
) -> Any:
    """在防火墙封禁一个 IP（全平台所有请求都被拦）。执行前先向用户确认，别封到自己的出口 IP。仅超级管理员。"""
    return await IP_BAN(body=body(ip=ip, reason=reason, duration=duration, mode=mode or None, delayMs=delay_ms))


@server.tool(**WRITE("risk"))
async def unban_ip(ban_id: Annotated[int, Field(gt=0, description="封禁记录 ID（risk_ip_bans 结果里的 id）")]) -> Any:
    """解除一条防火墙 IP 封禁。仅超级管理员。"""
    return await IP_UNBAN(banId=ban_id)


# ═══════════════════════ 防火墙：地域封禁与地理围栏 ═══════════════════════


@server.tool(**READ("risk"))
async def geo_bans() -> Any:
    """地域封禁规则（按国家 / 地区 / 城市 / ASN / 运营商）。仅超级管理员。"""
    return await GEO_BANS()


@server.tool(**DESTRUCTIVE("risk"))
async def save_geo_ban(
    scope_type: Annotated[Literal["country", "region", "city", "asn", "isp"], Field(description="作用域类型")],
    scope_value: Annotated[str, Field(min_length=1, description="作用域取值（国家代码、地区名、城市名、ASN 号、运营商名）")],
    mode: Annotated[str, Field(description="封禁响应模式；留空用平台默认")] = "",
    reason: Annotated[str, Field(description="原因")] = "",
    enabled: Annotated[bool, Field(description="是否启用")] = True,
    expires_at: Annotated[IsoTime, Field(description="到期时间（RFC 3339），留空为永久")] = "",
) -> Any:
    """新建或更新一条地域封禁（同一作用域已存在则覆盖）。会拦掉整片地区的全部请求，执行前先向用户确认。仅超级管理员。"""
    return await GEO_BAN_UPSERT(body=body(
        scopeType=scope_type, scopeValue=scope_value, mode=mode, reason=reason, enabled=enabled, expiresAt=expires_at,
    ))


@server.tool(**WRITE("risk", idempotent=True))
async def toggle_geo_ban(
    geo_ban_id: Annotated[int, Field(gt=0, description="地域封禁规则 ID")],
    enabled: Annotated[bool, Field(description="启用（true）或停用（false）")],
) -> Any:
    """启用或停用一条地域封禁。仅超级管理员。"""
    return await GEO_BAN_TOGGLE(id=geo_ban_id, body={"enabled": enabled})


@server.tool(**DESTRUCTIVE("risk"))
async def delete_geo_ban(geo_ban_id: Annotated[int, Field(gt=0, description="地域封禁规则 ID")]) -> Any:
    """删除一条地域封禁（不可恢复；只想暂停用 risk_toggle_geo_ban）。仅超级管理员。"""
    return await GEO_BAN_DELETE(id=geo_ban_id)


@server.tool(**READ("risk"))
async def geo_fences() -> Any:
    """地理围栏（多边形或圆形区域：deny 区内拦截 / allow 区外拦截 / review 只记录）。仅超级管理员。"""
    return await GEO_FENCES()


def _fence_body(
    name: str, mode: str, app_id: int | None, fence: dict[str, Any] | None, center_lat: float | None,
    center_lng: float | None, radius_m: float | None, ban_mode: str, reason: str, enabled: bool, expires_at: str,
) -> dict[str, Any]:
    circle = (center_lat, center_lng, radius_m)
    if fence is None and any(v is None for v in circle):
        raise ToolError("需要 fence（GeoJSON Polygon / MultiPolygon），或 center_lat + center_lng + radius_m 画圆")
    return body(
        appId=app_id, name=name, mode=mode, fence=fence, centerLat=center_lat, centerLng=center_lng, radiusM=radius_m,
        banMode=ban_mode, reason=reason, enabled=enabled, expiresAt=expires_at,
    )


FenceName = Annotated[str, Field(min_length=1, description="围栏名称")]
FenceMode = Annotated[Literal["deny", "allow", "review"], Field(description="deny 区域内拦截、allow 存在 allow 围栏时区域外拦截、review 区域内只记录")]
FenceShape = Annotated[dict[str, Any] | None, Field(description="GeoJSON Polygon / MultiPolygon（与圆形三参数二选一）")]
CenterLat = Annotated[float | None, Field(ge=-90, le=90, description="圆心纬度")]
CenterLng = Annotated[float | None, Field(ge=-180, le=180, description="圆心经度")]
RadiusM = Annotated[float | None, Field(gt=0, description="半径（米）")]


@server.tool(**WRITE("risk", idempotent=True))
async def preview_geo_fence(
    name: FenceName,
    mode: FenceMode,
    fence: FenceShape = None,
    center_lat: CenterLat = None,
    center_lng: CenterLng = None,
    radius_m: RadiusM = None,
    window_days: Annotated[int, Field(ge=1, le=90, description="用最近 N 天的请求估算影响")] = 7,
    app: OptionalApp = "",
) -> Any:
    """预览一个地理围栏草稿：按最近 N 天的流量估算会命中多少请求（不保存）。保存前先预览。"""
    payload = _fence_body(name, mode, await _app(app), fence, center_lat, center_lng, radius_m, "", "", True, "")
    payload["windowDays"] = window_days
    return await GEO_FENCE_PREVIEW(body=payload)


@server.tool(**DESTRUCTIVE("risk"))
async def save_geo_fence(
    name: FenceName,
    mode: FenceMode,
    fence_id: Annotated[int | None, Field(gt=0, description="给了就整体覆盖这条围栏；留空为新建")] = None,
    fence: FenceShape = None,
    center_lat: CenterLat = None,
    center_lng: CenterLng = None,
    radius_m: RadiusM = None,
    ban_mode: Annotated[str, Field(description="拦截时的响应模式；留空用平台默认")] = "",
    reason: Annotated[str, Field(description="原因")] = "",
    enabled: Annotated[bool, Field(description="是否启用")] = True,
    expires_at: Annotated[IsoTime, Field(description="到期时间（RFC 3339），留空为永久")] = "",
    app: OptionalApp = "",
) -> Any:
    """新建或整体覆盖一个地理围栏（立即生效，可能拦掉整片区域）。先用 risk_preview_geo_fence 估算影响并向用户确认。仅超级管理员。"""
    payload = _fence_body(name, mode, await _app(app), fence, center_lat, center_lng, radius_m, ban_mode, reason, enabled, expires_at)
    if fence_id:
        return await GEO_FENCE_UPDATE(id=fence_id, body=payload)
    return await GEO_FENCE_CREATE(body=payload)


@server.tool(**WRITE("risk", idempotent=True))
async def toggle_geo_fence(
    fence_id: Annotated[int, Field(gt=0, description="围栏 ID")],
    enabled: Annotated[bool, Field(description="启用（true）或停用（false）")],
) -> Any:
    """启用或停用一个地理围栏。仅超级管理员。"""
    return await GEO_FENCE_TOGGLE(id=fence_id, body={"enabled": enabled})


@server.tool(**DESTRUCTIVE("risk"))
async def delete_geo_fence(fence_id: Annotated[int, Field(gt=0, description="围栏 ID")]) -> Any:
    """删除一个地理围栏（不可恢复；只想暂停用 risk_toggle_geo_fence）。仅超级管理员。"""
    return await GEO_FENCE_DELETE(id=fence_id)


# ═══════════════════════ 防火墙日志 ═══════════════════════


@server.tool(**READ("risk"))
async def firewall_logs(
    log_id: Annotated[int | None, Field(gt=0, description="给了就取这一条拦截日志的详情")] = None,
    ip: Annotated[str, Field(description="IP")] = "",
    country: Annotated[str, Field(description="国家代码")] = "",
    reason: Annotated[str, Field(description="拦截原因")] = "",
    waf_rule_id: Annotated[int | None, Field(description="WAF 规则 ID")] = None,
    path_pattern: Annotated[str, Field(description="请求路径匹配")] = "",
    severity: Annotated[Literal["", "low", "medium", "high", "critical"], Field(description="严重程度")] = "",
    start_time: Annotated[IsoTime, Field(description="起始时间（RFC 3339）")] = "",
    end_time: Annotated[IsoTime, Field(description="结束时间（RFC 3339）")] = "",
    sort_by: Annotated[Literal["", "blocked_at", "ip"], Field(description="排序字段")] = "",
    sort_order: Annotated[Literal["", "asc", "desc"], Field(description="排序方向")] = "",
    page: Page = 1,
    page_size: PageSize = 20,
) -> Any:
    """防火墙拦截日志列表，或给 log_id 取详情。仅超级管理员。"""
    if log_id:
        return await FW_LOG(logId=log_id)
    return await FW_LOGS(query=q(
        ip=ip, country=country, reason=reason, wafRuleId=waf_rule_id, pathPattern=path_pattern, severity=severity,
        startTime=start_time, endTime=end_time, sortBy=sort_by, sortOrder=sort_order, page=page, pageSize=page_size,
    ))


@server.tool(**READ("risk"))
async def firewall_stats(
    start_time: Annotated[IsoTime, Field(description="起始时间（RFC 3339），默认 24 小时前")] = "",
    end_time: Annotated[IsoTime, Field(description="结束时间（RFC 3339），默认现在")] = "",
    granularity: Annotated[Literal["hour", "day"], Field(description="趋势粒度")] = "hour",
) -> Any:
    """防火墙拦截统计：总量、按原因 / 国家 / IP 排行、趋势。仅超级管理员。"""
    return await FW_STATS(query=q(startTime=start_time, endTime=end_time, granularity=granularity))


@server.tool(**DESTRUCTIVE("risk"))
async def cleanup_firewall_logs(before: Annotated[str, Field(min_length=1, description="删除这个时间（RFC 3339）之前的全部拦截日志")]) -> Any:
    """清理指定时间之前的防火墙拦截日志（不可恢复）。仅超级管理员，执行前先向用户确认。"""
    return await FW_LOGS_CLEANUP(body={"before": before})
