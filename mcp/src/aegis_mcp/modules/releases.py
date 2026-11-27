"""发布中心：版本发布（灰度、定向、暂停 / 恢复 / 撤回、更新说明预览、命中模拟）、发布渠道与成员、旧版本记录。"""

from __future__ import annotations

from typing import Annotated, Any, Literal

from fastmcp import FastMCP
from fastmcp.exceptions import ToolError
from pydantic import Field

from ..core import DELETE, DESTRUCTIVE, GET, POST, PUT, READ, WRITE, AppRef, IsoTime, Limit, Page, app_id_of, body, q

NAMESPACE = "releases"
server = FastMCP("releases")

RELEASES = GET("/api/admin/apps/:appkey/releases")
RELEASE = GET("/api/admin/apps/:appkey/releases/:rid")
RELEASE_STATS = GET("/api/admin/apps/:appkey/releases/:rid/stats")
RELEASE_OVERVIEW = GET("/api/admin/apps/:appkey/releases/overview")
RELEASE_CREATE = POST("/api/admin/apps/:appkey/releases")
RELEASE_UPDATE = PUT("/api/admin/apps/:appkey/releases/:rid")
RELEASE_DELETE = DELETE("/api/admin/apps/:appkey/releases/:rid")
RELEASE_PUBLISH = POST("/api/admin/apps/:appkey/releases/:rid/publish")
RELEASE_PAUSE = POST("/api/admin/apps/:appkey/releases/:rid/pause")
RELEASE_RESUME = POST("/api/admin/apps/:appkey/releases/:rid/resume")
RELEASE_REVOKE = POST("/api/admin/apps/:appkey/releases/:rid/revoke")
RELEASE_ROLLOUT = PUT("/api/admin/apps/:appkey/releases/:rid/rollout")
RELEASE_SIMULATE = POST("/api/admin/apps/:appkey/releases/simulate")
RELEASE_NOTES_PREVIEW = POST("/api/admin/apps/:appkey/releases/notes/preview")

CHANNELS = GET("/api/admin/apps/:appkey/channels")
CHANNEL = GET("/api/admin/apps/:appkey/channels/:cid")
CHANNEL_CREATE = POST("/api/admin/apps/:appkey/channels")
CHANNEL_UPDATE = PUT("/api/admin/apps/:appkey/channels/:cid")
CHANNEL_DELETE = DELETE("/api/admin/apps/:appkey/channels/:cid")
CHANNEL_USERS = GET("/api/admin/apps/:appkey/channels/:cid/users")
CHANNEL_USERS_ADD = POST("/api/admin/apps/:appkey/channels/:cid/users")
CHANNEL_USERS_REMOVE = DELETE("/api/admin/apps/:appkey/channels/:cid/users")

VERSIONS = GET("/api/admin/apps/:appkey/versions")
VERSION = GET("/api/admin/apps/:appkey/versions/:vid")
VERSION_STATS = GET("/api/admin/apps/:appkey/versions/stats")
VERSION_CREATE = POST("/api/admin/apps/:appkey/versions")
VERSION_UPDATE = PUT("/api/admin/apps/:appkey/versions/:vid")
VERSION_DELETE = DELETE("/api/admin/apps/:appkey/versions/:vid")
VERSION_PUBLISH = POST("/api/admin/apps/:appkey/versions/:vid/publish")
VERSION_REVOKE = POST("/api/admin/apps/:appkey/versions/:vid/revoke")

ReleaseID = Annotated[int, Field(gt=0, description="发布 ID")]
UpdateType = Literal["optional", "recommended", "force"]
Visibility = Literal["public", "signed_in", "testers"]


@server.tool(name="list", **READ("releases"))
async def list_releases(
    app: AppRef,
    status: Annotated[Literal["", "draft", "scheduled", "published", "paused", "revoked"], Field(description="发布状态")] = "",
    platform: Annotated[str, Field(description="平台，如 android / android_tv / ios / windows / macos / linux / web")] = "",
    channel_id: int | None = None,
    keyword: str = "",
    page: Page = 1,
    limit: Limit = 20,
    overview: Annotated[bool, Field(description="同时取发布总览（各平台最新版本、灰度中的发布）")] = False,
) -> Any:
    """发布列表，可附带总览。"""
    result = await RELEASES(appkey=app, query=q(status=status, platform=platform, channelId=channel_id, keyword=keyword, page=page, limit=limit))
    if overview:
        return {"overview": await RELEASE_OVERVIEW(appkey=app), "releases": result}
    return result


@server.tool(name="get", **READ("releases"))
async def get_release(app: AppRef, release_id: ReleaseID, stats: Annotated[bool, Field(description="同时取下载 / 检查更新 / 升级统计")] = True) -> Any:
    """一个发布的详情（安装包、定向、灰度），可附带统计。"""
    result: dict[str, Any] = {"release": await RELEASE(appkey=app, rid=release_id)}
    if stats:
        result["stats"] = await RELEASE_STATS(appkey=app, rid=release_id)
    return result


@server.tool(**WRITE("releases"))
async def save(
    app: AppRef,
    version: Annotated[str, Field(min_length=1, description="版本号，如 2.3.0")],
    version_code: Annotated[int, Field(gt=0, description="递增的版本代码")],
    platform: Annotated[str, Field(min_length=1, description="平台，如 android / android_tv / ios / windows")],
    release_id: Annotated[int | None, Field(gt=0, description="给 ID 即修改草稿，不给即新建草稿")] = None,
    title: str = "",
    notes_markdown: Annotated[str, Field(description="更新说明（Markdown）")] = "",
    update_type: Annotated[UpdateType, Field(description="optional 可选 / recommended 推荐 / force 强制")] = "optional",
    visibility: Annotated[Visibility, Field(description="public 所有人 / signed_in 仅登录用户 / testers 仅内测名单")] = "public",
    channel_id: Annotated[int | None, Field(description="挂到某个渠道（只有渠道成员能收到）")] = None,
    rollout_pct: Annotated[int, Field(ge=0, le=100, description="灰度比例")] = 100,
    min_supported_code: Annotated[int | None, Field(description="低于此版本代码的客户端强制更新")] = None,
    min_os_version: str = "",
    assets: Annotated[list[dict[str, Any]] | None, Field(description="安装包：[{url, abi, fileSize, sha256, label}]（文件上传请在控制台完成）")] = None,
    targeting: Annotated[dict[str, Any] | None, Field(description="定向条件：regions / locales / deviceModels / abis / minOsVersion / maxOsVersion / minSourceVersionCode / maxSourceVersionCode / excludeUserIds / excludeDeviceIds / testerUserIds / testerDeviceIds")] = None,
) -> Any:
    """新建或修改发布草稿（不会推送给用户，发布用 releases_control）。"""
    payload = body(
        version=version, versionCode=version_code, platform=platform, title=title or None,
        notesMarkdown=notes_markdown or None, notesFormat="markdown" if notes_markdown else None,
        updateType=update_type, visibility=visibility, channelId=channel_id, rolloutPct=rollout_pct,
        minSupportedCode=min_supported_code, minOsVersion=min_os_version or None, assets=assets, targeting=targeting,
    )
    if release_id:
        return await RELEASE_UPDATE(appkey=app, rid=release_id, body=payload)
    return await RELEASE_CREATE(appkey=app, body=payload)


@server.tool(**DESTRUCTIVE("releases"))
async def control(
    app: AppRef,
    release_id: ReleaseID,
    action: Annotated[Literal["publish", "pause", "resume", "revoke", "rollout", "delete"], Field(description="publish 发布（可定时）/ pause 暂停 / resume 恢复 / revoke 撤回 / rollout 调整灰度 / delete 删除草稿")],
    publish_at: Annotated[IsoTime, Field(description="定时发布时间，留空立即发布")] = "",
    rollout_pct: Annotated[int | None, Field(ge=0, le=100, description="action=rollout 时的新灰度比例")] = None,
) -> Any:
    """发布生命周期控制。发布与调高灰度会立刻推送给用户，撤回会让已推送的更新失效，执行前先确认。"""
    if action == "publish":
        return await RELEASE_PUBLISH(appkey=app, rid=release_id, body=body(publishAt=publish_at or None))
    if action == "pause":
        return await RELEASE_PAUSE(appkey=app, rid=release_id)
    if action == "resume":
        return await RELEASE_RESUME(appkey=app, rid=release_id)
    if action == "revoke":
        return await RELEASE_REVOKE(appkey=app, rid=release_id)
    if action == "rollout":
        if rollout_pct is None:
            raise ToolError("调整灰度需要 rollout_pct")
        return await RELEASE_ROLLOUT(appkey=app, rid=release_id, body={"rolloutPct": rollout_pct})
    return await RELEASE_DELETE(appkey=app, rid=release_id)


@server.tool(**READ("releases"))
async def simulate(
    app: AppRef,
    platform: Annotated[str, Field(min_length=1)],
    version_code: Annotated[int, Field(ge=0, description="客户端当前版本代码")],
    user_id: int | None = None,
    device_id: str = "",
    device_model: str = "",
    os_version: int | None = None,
    locale: str = "",
    region: str = "",
    abis: list[str] | None = None,
) -> Any:
    """模拟一个客户端检查更新：会命中哪个发布、为什么没命中（渠道、可见范围、定向、灰度逐项说明）。"""
    return await RELEASE_SIMULATE(appkey=app, body=body(
        platform=platform, versionCode=version_code, userId=user_id, deviceId=device_id or None, deviceModel=device_model or None,
        osVersion=os_version, locale=locale or None, region=region or None, abis=abis,
    ))


@server.tool(**READ("releases"))
async def preview_notes(app: AppRef, markdown: Annotated[str, Field(min_length=1, description="Markdown 更新说明")]) -> Any:
    """预览更新说明渲染后的净化 HTML（客户端看到的样子）。"""
    return await RELEASE_NOTES_PREVIEW(appkey=app, body={"format": "markdown", "markdown": markdown})


# ── 渠道 ──

@server.tool(**READ("releases"))
async def channels(
    app: AppRef,
    channel_id: Annotated[int | None, Field(gt=0, description="给 ID 时取详情")] = None,
    members: Annotated[bool, Field(description="给 ID 时同时列成员")] = False,
    page: Page = 1,
    limit: Limit = 20,
) -> Any:
    """发布渠道列表，或某个渠道的详情与成员。"""
    if not channel_id:
        return await CHANNELS(appkey=app)
    result: dict[str, Any] = {"channel": await CHANNEL(appkey=app, cid=channel_id)}
    if members:
        result["members"] = await CHANNEL_USERS(appkey=app, cid=channel_id, query=q(page=page, limit=limit))
    return result


@server.tool(**WRITE("releases"))
async def save_channel(
    app: AppRef,
    name: Annotated[str, Field(min_length=1)],
    code: Annotated[str, Field(min_length=1, description="渠道代码，如 beta / internal")],
    channel_id: Annotated[int | None, Field(gt=0, description="给 ID 即修改，不给即新建")] = None,
    description: str = "",
    level: Annotated[str, Field(description="渠道级别，如 stable / beta / alpha / internal")] = "",
    platforms: list[str] | None = None,
    priority: int | None = None,
    rollout_pct: Annotated[int | None, Field(ge=0, le=100)] = None,
    min_version_code: int | None = None,
    max_version_code: int | None = None,
    self_join: Annotated[bool | None, Field(description="用户能否自行加入")] = None,
    is_default: bool | None = None,
    status: Annotated[bool, Field(description="是否启用")] = True,
    color: str = "",
    rules: Annotated[list[dict[str, Any]] | None, Field(description="自动入组规则：[{field, op, value}]")] = None,
) -> Any:
    """新建或修改发布渠道。"""
    payload = body(
        name=name, code=code, description=description, level=level or None, platforms=platforms, priority=priority,
        rollout_pct=rollout_pct, min_version_code=min_version_code, max_version_code=max_version_code,
        self_join=self_join, is_default=is_default, status=status, color=color or None, rules=rules,
    )
    if channel_id:
        return await CHANNEL_UPDATE(appkey=app, cid=channel_id, body=payload)
    return await CHANNEL_CREATE(appkey=app, body=payload)


@server.tool(**WRITE("releases"))
async def channel_members(
    app: AppRef,
    channel_id: Annotated[int, Field(gt=0)],
    user_ids: Annotated[list[int], Field(min_length=1, max_length=1000)],
    action: Annotated[Literal["add", "remove"], Field(description="加入 / 移出")] = "add",
) -> Any:
    """把用户加入或移出发布渠道。"""
    route = CHANNEL_USERS_ADD if action == "add" else CHANNEL_USERS_REMOVE
    return await route(appkey=app, cid=channel_id, body={"user_ids": user_ids})


@server.tool(**DESTRUCTIVE("releases"))
async def delete_channel(app: AppRef, channel_id: Annotated[int, Field(gt=0)]) -> Any:
    """删除发布渠道（挂在上面的发布回到默认可见范围，先确认）。"""
    return await CHANNEL_DELETE(appkey=app, cid=channel_id)


# ── 旧版本记录（旧控制台与 /api/user/check-version 的形状） ──

@server.tool(**READ("releases"))
async def versions(
    app: AppRef,
    version_id: Annotated[int | None, Field(gt=0, description="给 ID 时取详情")] = None,
    status: str = "",
    platform: str = "",
    channel_id: int | None = None,
    stats: Annotated[bool, Field(description="改为取版本分布统计")] = False,
    page: Page = 1,
    limit: Limit = 20,
) -> Any:
    """旧版本记录：列表、详情或分布统计（新功能优先用 releases_*）。"""
    if stats:
        return await VERSION_STATS(appkey=app)
    if version_id:
        return await VERSION(appkey=app, vid=version_id)
    return await VERSIONS(appkey=app, query=q(status=status, platform=platform, channel_id=channel_id, page=page, limit=limit))


@server.tool(**WRITE("releases"))
async def save_version(
    app: AppRef,
    version: Annotated[str, Field(min_length=1)],
    version_code: Annotated[int, Field(gt=0)],
    platform: Annotated[str, Field(min_length=1)],
    version_id: Annotated[int | None, Field(gt=0, description="给 ID 即修改，不给即新建")] = None,
    download_url: str = "",
    release_notes: str = "",
    description: str = "",
    force_update: bool = False,
    update_type: str = "",
    channel_id: int | None = None,
    min_os_version: str = "",
    file_size: int | None = None,
    file_hash: str = "",
) -> Any:
    """新建或修改旧版本记录。"""
    payload = body(
        appid=await app_id_of(app),
        version=version, version_code=version_code, platform=platform, download_url=download_url or None,
        release_notes=release_notes or None, description=description or None, force_update=force_update,
        update_type=update_type or None, channel_id=channel_id, min_os_version=min_os_version or None,
        file_size=file_size, file_hash=file_hash or None,
    )
    if version_id:
        return await VERSION_UPDATE(appkey=app, vid=version_id, body=payload)
    return await VERSION_CREATE(appkey=app, body=payload)


@server.tool(**DESTRUCTIVE("releases"))
async def version_control(
    app: AppRef,
    version_id: Annotated[int, Field(gt=0)],
    action: Annotated[Literal["publish", "revoke", "delete"], Field(description="发布 / 撤回 / 删除")],
) -> Any:
    """发布、撤回或删除旧版本记录。"""
    route = {"publish": VERSION_PUBLISH, "revoke": VERSION_REVOKE, "delete": VERSION_DELETE}[action]
    return await route(appkey=app, vid=version_id)
