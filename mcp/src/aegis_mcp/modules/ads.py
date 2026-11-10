"""广告：激励广告（配置与场景、观看 / 回调记录、补发、统计）与广告服务策略（条款版本、用户选择、开屏记录、统计、用户详情）。

权限：读需要 app:read，保存配置 / 补发需要 app:write。
"""

from __future__ import annotations

from typing import Annotated, Any, Literal

from fastmcp import FastMCP
from fastmcp.exceptions import ToolError
from pydantic import Field

from ..core import DESTRUCTIVE, GET, POST, PUT, READ, WRITE, AppRef, Limit, Page, UserID, body, q

NAMESPACE = "ads"
server = FastMCP("ads")

REWARDED_CONFIG = GET("/api/admin/apps/:appkey/rewarded-ads/config")
REWARDED_CONFIG_SAVE = PUT("/api/admin/apps/:appkey/rewarded-ads/config")
REWARDED_VIEWS = GET("/api/admin/apps/:appkey/rewarded-ads/views")
REWARDED_VIEW_GRANT = POST("/api/admin/apps/:appkey/rewarded-ads/views/:viewId/grant")
REWARDED_STATS = GET("/api/admin/apps/:appkey/rewarded-ads/stats")

POLICY = GET("/api/admin/apps/:appkey/ad-policy/config")
POLICY_SAVE = PUT("/api/admin/apps/:appkey/ad-policy/config")
CONSENTS = GET("/api/admin/apps/:appkey/ad-policy/consents")
CONSENT_LOGS = GET("/api/admin/apps/:appkey/ad-policy/consent-logs")
SPLASH_EVENTS = GET("/api/admin/apps/:appkey/ad-policy/splash-events")
POLICY_STATS = GET("/api/admin/apps/:appkey/ad-policy/stats")
USER_POLICY = GET("/api/admin/apps/:appkey/users/:userId/ad-policy")

OptionalUser = Annotated[int | None, Field(gt=0, description="只看这个用户（管理端用户 ID），留空为全部")]
Keyword = Annotated[str, Field(description="关键词（账号、交易号、设备等），留空不限")]
DateBound = Annotated[str, Field(description="时间，ISO 8601 或 YYYY-MM-DD（结束日期按当天整天算），留空不限")]
Days = Annotated[int, Field(ge=1, le=90, description="统计最近 N 天的趋势")]

REWARD_HELP = (
    "权益列表，每项 {type, amount?, money?, refId?, feature?}。type：vip_plan（refId=套餐 ID）/ vip_days（amount=天）/ "
    "integral（amount=积分）/ experience（amount=经验）/ lottery_draws（amount=次数）/ "
    "feature_minutes（feature=功能标识，amount=分钟 1–1440，每场景最多一项）/ ai_credit（amount=点数）。"
    "可选项以 ads_rewarded_config 的 catalog 为准"
)
SCENE_KEYS = ("key", "name", "placementId", "enabled", "rewards", "dailyLimit", "cooldownSeconds", "membership", "lifetime")
CONFIG_KEYS = ("enabled", "provider", "providerAppId", "verifyMode", "dailyLimit", "scenes")


# ── 激励广告 ──


@server.tool(**READ("ads"))
async def rewarded_config(app: AppRef) -> Any:
    """激励广告配置：开关、广告平台与应用 ID、校验模式、每日总上限、广告位（各自的 Key 状态 keySource、使用场景、近 7 天回调）、
    全部场景（权益、次数、冷却、会员权益、已拥有处理方式），以及回调地址、权益目录（catalog）、可选校验模式 / 平台 / 已拥有处理方式与折算去处。
    密钥只给提示（securityKeyHint）：顶层的是默认 Key，广告位里的是各自的 Key。"""
    return await REWARDED_CONFIG(appkey=app)


def _current_config(current: Any) -> dict[str, Any]:
    current = current if isinstance(current, dict) else {}
    config = {key: current[key] for key in CONFIG_KEYS if key in current}
    config["scenes"] = [{key: scene[key] for key in SCENE_KEYS if key in scene} for scene in (current.get("scenes") or [])]
    return config


@server.tool(**WRITE("ads"))
async def rewarded_save_config(
    app: AppRef,
    enabled: Annotated[bool | None, Field(description="是否开启激励广告")] = None,
    provider: Annotated[str | None, Field(description="广告平台（目前为 apex，可选值见 ads_rewarded_config 的 providers）")] = None,
    provider_app_id: Annotated[str | None, Field(description="广告平台的应用 ID")] = None,
    security_key: Annotated[str | None, Field(description="默认 Key：没单独配 Key 的广告位用它验签；不传则不修改。各广告位的 Key 用 ads_rewarded_save_placement")] = None,
    clear_security_key: Annotated[bool | None, Field(description="清除已保存的默认 Key")] = None,
    verify_mode: Annotated[Literal["dual", "server", "client"] | None, Field(description="发奖校验模式：dual 回调与客户端都到才发 / server 只认平台回调 / client 只认 SDK 结论")] = None,
    daily_limit: Annotated[int | None, Field(ge=0, description="每人每天全部场景合计最多领取次数，0 为不限")] = None,
    scenes: Annotated[list[dict[str, Any]] | None, Field(description="整体替换全部场景（字段同 ads_rewarded_save_scene 的请求体）；只改一个场景请用 ads_rewarded_save_scene")] = None,
) -> Any:
    """修改激励广告的全局配置。只传要改的字段，其余沿用当前值（后端整体覆盖，这里先读后合并）。

    关闭或改校验模式会立即影响全部用户领奖；执行前向用户复述改动。
    """
    config = _current_config(await REWARDED_CONFIG(appkey=app))
    config.update(body(
        enabled=enabled, provider=provider, providerAppId=provider_app_id, verifyMode=verify_mode,
        dailyLimit=daily_limit, scenes=scenes, securityKey=security_key or None, clearSecurityKey=clear_security_key,
    ))
    return await REWARDED_CONFIG_SAVE(appkey=app, body=config)


def _current_placements(current: Any) -> list[dict[str, Any]]:
    """配置里的广告位，转成保存载荷的形态（Key 不回传，留空即沿用）。"""
    current = current if isinstance(current, dict) else {}
    return [{"id": item["id"], "name": item.get("name", "")} for item in (current.get("placements") or []) if item.get("id")]


@server.tool(**WRITE("ads"))
async def rewarded_save_placement(
    app: AppRef,
    id: Annotated[str, Field(pattern=r"^[A-Za-z0-9_-]{1,64}$", description="广告位 ID（Apex 后台的聚合广告位 ID）。已登记即修改，否则新增")],
    name: Annotated[str | None, Field(max_length=32, description="广告位名称（如 激励视频）")] = None,
    security_key: Annotated[str | None, Field(description="这个广告位在 Apex 后台的 Security Key；不传则不修改")] = None,
    clear_security_key: Annotated[bool | None, Field(description="清除这个广告位的 Key，改用默认 Key 验签")] = None,
    previous_id: Annotated[str | None, Field(description="改广告位 ID 时填原 ID：已存的 Key 随之带过去，引用原 ID 的场景一并改到新 ID")] = None,
) -> Any:
    """登记或修改一个激励广告位及其 Security Key（其余广告位、场景与全局配置原样保留）。

    Apex 按广告位签发 Security Key，回调按回调里的 placementId 选 Key 验签；没单独配 Key 的广告位用默认 Key。
    Key 填错会让这个广告位的全部回调验签失败、用户领不到奖，执行前向用户复述广告位与 Key 末 4 位。
    """
    current = await REWARDED_CONFIG(appkey=app)
    config = _current_config(current)
    placements = _current_placements(current)
    locate = previous_id or id
    target = next((item for item in placements if item["id"] == locate), None)
    if target is None:
        if previous_id:
            raise ToolError(f"找不到广告位 {previous_id}，请先用 ads_rewarded_config 核对")
        target = {"id": id, "name": ""}
        placements.append(target)
    if previous_id and previous_id != id:
        target["previousId"] = previous_id
        target["id"] = id
        for scene in config["scenes"]:
            if scene.get("placementId") == previous_id:
                scene["placementId"] = id
    if name is not None:
        target["name"] = name
    if security_key:
        target["securityKey"] = security_key
    if clear_security_key:
        target["clearSecurityKey"] = True
    config["placements"] = placements
    return await REWARDED_CONFIG_SAVE(appkey=app, body=config)


@server.tool(**DESTRUCTIVE("ads"))
async def rewarded_delete_placement(
    app: AppRef,
    id: Annotated[str, Field(min_length=1, description="要删除的广告位 ID")],
) -> Any:
    """删除一个激励广告位及其 Key。还有场景在用的广告位不能删（先用 ads_rewarded_save_scene 给场景换广告位）。执行前先向用户确认。"""
    current = await REWARDED_CONFIG(appkey=app)
    config = _current_config(current)
    placements = _current_placements(current)
    remaining = [item for item in placements if item["id"] != id]
    if len(remaining) == len(placements):
        raise ToolError(f"找不到广告位 {id}，请先用 ads_rewarded_config 核对")
    users = [scene.get("key") for scene in config["scenes"] if scene.get("placementId") == id]
    if users:
        raise ToolError(f"广告位 {id} 仍被场景 {', '.join(users)} 使用，请先给这些场景换广告位")
    config["placements"] = remaining
    return await REWARDED_CONFIG_SAVE(appkey=app, body=config)


@server.tool(**WRITE("ads"))
async def rewarded_save_scene(
    app: AppRef,
    key: Annotated[str, Field(pattern=r"^[a-z0-9][a-z0-9_-]{0,31}$", description="场景标识（小写字母数字、_、-，1–32 位）。已存在即修改，否则新增")],
    name: Annotated[str | None, Field(description="场景名称（新增时必填）")] = None,
    placement_id: Annotated[str | None, Field(description="广告位 ID")] = None,
    enabled: Annotated[bool | None, Field(description="是否启用，新增默认启用")] = None,
    rewards: Annotated[list[dict[str, Any]] | None, Field(description=REWARD_HELP)] = None,
    daily_limit: Annotated[int | None, Field(ge=0, description="这个场景每人每天最多领取次数，0 为不限")] = None,
    cooldown_seconds: Annotated[int | None, Field(ge=0, description="两次领取之间的冷却秒数")] = None,
    membership: Annotated[dict[str, Any] | None, Field(description="送出的会员带什么权益：{features: [功能标识], adFree: bool}；features 只对 vip_days 生效，adFree 不填按免广告")] = None,
    lifetime: Annotated[dict[str, Any] | None, Field(description="已拥有权益时怎么办：{policy: convert|replace|hide|grant, target: integral|ai_credit|experience|lottery_draws, perVipDay, perFeatureHour, rewards: [...]}；不传沿用原值（新增默认按价值折算）")] = None,
) -> Any:
    """新增或修改一个激励广告场景（按 key 定位），其余场景与全局配置原样保留。修改只传要改的字段。

    改权益会影响之后每一次领取；membership 的权益对已领到的会员同样生效。
    """
    config = _current_config(await REWARDED_CONFIG(appkey=app))
    patch = body(
        name=name, placementId=placement_id, enabled=enabled, rewards=rewards, dailyLimit=daily_limit,
        cooldownSeconds=cooldown_seconds, membership=membership, lifetime=lifetime,
    )
    for scene in config["scenes"]:
        if scene.get("key") == key:
            scene.update(patch)
            break
    else:
        if not (name or "").strip():
            raise ToolError(f"场景 {key} 不存在，新增需要 name")
        if not rewards:
            raise ToolError(f"场景 {key} 不存在，新增需要 rewards")
        config["scenes"].append({"key": key, "enabled": True, **patch})
    return await REWARDED_CONFIG_SAVE(appkey=app, body=config)


@server.tool(**DESTRUCTIVE("ads"))
async def rewarded_delete_scene(
    app: AppRef,
    key: Annotated[str, Field(min_length=1, description="要删除的场景标识")],
) -> Any:
    """删除一个激励广告场景（其余配置原样保留）。客户端再上报这个场景会领不到奖，抽奖活动若引用它会失效。只想暂停请用 ads_rewarded_save_scene 设 enabled=false。执行前先向用户确认。"""
    config = _current_config(await REWARDED_CONFIG(appkey=app))
    remaining = [scene for scene in config["scenes"] if scene.get("key") != key]
    if len(remaining) == len(config["scenes"]):
        raise ToolError(f"找不到场景 {key}，请先用 ads_rewarded_config 核对")
    config["scenes"] = remaining
    return await REWARDED_CONFIG_SAVE(appkey=app, body=config)


@server.tool(**READ("ads"))
async def rewarded_views(
    app: AppRef,
    user_id: OptionalUser = None,
    status: Annotated[Literal["", "pending", "granted", "rejected"], Field(description="pending 等待回调 / granted 已发放 / rejected 已拒绝，留空为全部")] = "",
    scene: Annotated[str, Field(description="场景标识，留空为全部")] = "",
    keyword: Keyword = "",
    start: DateBound = "",
    end: DateBound = "",
    page: Page = 1,
    limit: Limit = 20,
) -> Any:
    """激励广告观看记录（含平台回调核验时间、客户端上报、拒绝原因、发放结果与折算），排查「看了广告没到账」用。"""
    return await REWARDED_VIEWS(appkey=app, query=q(
        userId=user_id, status=status, scene=scene, keyword=keyword, start=start, end=end, page=page, limit=limit,
    ))


@server.tool(**WRITE("ads"))
async def rewarded_grant_view(
    app: AppRef,
    view_id: Annotated[int, Field(gt=0, description="观看记录 ID（ads_rewarded_views 结果里的 id）")],
    scene: Annotated[str, Field(description="按哪个场景的权益补发，留空沿用记录上的场景")] = "",
) -> Any:
    """给一条未发放（pending / rejected）的观看记录补发奖励，跳过次数与冷却限制，立即到账且不可撤回。执行前先向用户确认记录与场景。"""
    return await REWARDED_VIEW_GRANT(appkey=app, viewId=view_id, body=body(scene=scene))


@server.tool(**READ("ads"))
async def rewarded_stats(app: AppRef, days: Days = 14) -> Any:
    """激励广告概览与按天趋势（观看、发放、拒绝、人数等）。"""
    return await REWARDED_STATS(appkey=app, query={"days": days})


# ── 广告服务策略 ──

POLICY_KEYS = ("enabled", "consentVersion", "policyUrl", "vipExempt", "basicTools", "splash")


@server.tool(**READ("ads"))
async def policy(app: AppRef) -> Any:
    """广告服务策略：是否要求同意、条款版本、条款地址、会员是否豁免、基础服务可用的工具、开屏广告配置；configured 表示是否保存过。"""
    return await POLICY(appkey=app)


@server.tool(**WRITE("ads"))
async def save_policy(
    app: AppRef,
    enabled: Annotated[bool | None, Field(description="要求同意广告服务：开启后拒绝或未选择的用户只能用基础服务")] = None,
    consent_version: Annotated[int | None, Field(ge=1, description="条款版本，只能调高；调高后所有用户都要按新版本重新选择")] = None,
    policy_url: Annotated[str | None, Field(description="条款页面地址")] = None,
    vip_exempt: Annotated[bool | None, Field(description="会员免除同意要求、不展示广告")] = None,
    basic_tools: Annotated[list[str] | None, Field(description="基础服务下仍可用的工具标识（整体替换）")] = None,
    splash_enabled: Annotated[bool | None, Field(description="是否展示开屏广告")] = None,
    splash_placement_id: Annotated[str | None, Field(description="开屏广告位 ID")] = None,
    splash_min_interval_seconds: Annotated[int | None, Field(ge=0, description="两次开屏的最短间隔（秒）")] = None,
    splash_daily_limit: Annotated[int | None, Field(ge=0, description="每人每天最多展示几次开屏，0 为不限")] = None,
) -> Any:
    """修改广告服务策略（含开屏广告）。只传要改的字段，其余沿用当前值（后端整体覆盖，这里先读后合并）。

    保存后立即推送给全部在线客户端。调高 consent_version 会让所有人重新选择、在此之前只有基础服务，且不能调回；执行前务必向用户确认。
    """
    current = await POLICY(appkey=app) or {}
    current = current if isinstance(current, dict) else {}
    if consent_version is not None and consent_version < int(current.get("consentVersion") or 0):
        raise ToolError(f"条款版本只能调高（当前为 {current.get('consentVersion')}）")
    merged = {key: current[key] for key in POLICY_KEYS if key in current}
    merged.update(body(enabled=enabled, consentVersion=consent_version, policyUrl=policy_url, vipExempt=vip_exempt, basicTools=basic_tools))
    splash = dict(merged.get("splash") or {})
    splash.update(body(
        enabled=splash_enabled, placementId=splash_placement_id,
        minIntervalSeconds=splash_min_interval_seconds, dailyLimit=splash_daily_limit,
    ))
    merged["splash"] = splash
    return await POLICY_SAVE(appkey=app, body=merged)


@server.tool(**READ("ads"))
async def consents(
    app: AppRef,
    accepted: Annotated[bool | None, Field(description="true 只看同意的 / false 只看拒绝的，留空为全部")] = None,
    outdated: Annotated[bool | None, Field(description="true 只看按旧版本条款做的选择")] = None,
    source: Annotated[Literal["", "app", "web", "guest_sync"], Field(description="选择来源，留空为全部")] = "",
    keyword: Keyword = "",
    page: Page = 1,
    limit: Limit = 20,
) -> Any:
    """用户对广告服务的当前选择（同意 / 拒绝、条款版本、来源、设备、时间）。"""
    return await CONSENTS(appkey=app, query=q(accepted=accepted, outdated=outdated or None, source=source, keyword=keyword, page=page, limit=limit))


@server.tool(**READ("ads"))
async def consent_logs(app: AppRef, user_id: OptionalUser = None, keyword: Keyword = "", page: Page = 1, limit: Limit = 20) -> Any:
    """用户广告服务选择的变更历史。"""
    return await CONSENT_LOGS(appkey=app, query=q(userId=user_id, keyword=keyword, page=page, limit=limit))


@server.tool(**READ("ads"))
async def splash_events(
    app: AppRef,
    user_id: OptionalUser = None,
    status: Annotated[Literal["", "shown", "clicked", "failed", "timeout"], Field(description="开屏结果，留空为全部")] = "",
    keyword: Keyword = "",
    start: DateBound = "",
    end: DateBound = "",
    page: Page = 1,
    limit: Limit = 20,
) -> Any:
    """开屏广告记录：每次开屏尝试的广告位、结果、错误、加载 / 展示耗时、设备与时间。"""
    return await SPLASH_EVENTS(appkey=app, query=q(userId=user_id, status=status, keyword=keyword, start=start, end=end, page=page, limit=limit))


@server.tool(**READ("ads"))
async def policy_stats(app: AppRef, days: Days = 14) -> Any:
    """广告服务概览与按天趋势（同意 / 拒绝人数、开屏展示 / 点击 / 失败等）。"""
    return await POLICY_STATS(appkey=app, query={"days": days})


@server.tool(**READ("ads"))
async def user_policy(app: AppRef, user_id: UserID) -> Any:
    """一个用户的广告服务情况：当前选择、服务档位（full / basic）、是否豁免、开屏频控与统计。"""
    return await USER_POLICY(appkey=app, userId=user_id)
