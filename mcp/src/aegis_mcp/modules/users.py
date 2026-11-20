"""应用用户：检索与详情、资料与启停、删除、密码与头像、会话、封禁、OAuth2 授权、登录绑定、账号注销、登录设备数、用户云存储。"""

from __future__ import annotations

from typing import Annotated, Any, Literal

from fastmcp import FastMCP
from fastmcp.exceptions import ToolError
from pydantic import Field

from ..core import (
    DELETE,
    DESTRUCTIVE,
    GET,
    POST,
    PUT,
    READ,
    WRITE,
    AppRef,
    IsoTime,
    Limit,
    Page,
    UserID,
    body,
    q,
)

NAMESPACE = "users"
server = FastMCP("users")

# ── 用户本体 ──
USERS = GET("/api/admin/apps/:appkey/users")
USER = GET("/api/admin/apps/:appkey/users/:userId")
USER_DELETE = DELETE("/api/admin/apps/:appkey/users/:userId")
USER_PROFILE = PUT("/api/admin/apps/:appkey/users/:userId/profile")
USER_STATUS = PUT("/api/admin/apps/:appkey/users/:userId/status")
USER_STATUS_BATCH = PUT("/api/admin/apps/:appkey/users/status/batch")
USER_RESET_PASSWORD = POST("/api/admin/apps/:appkey/users/:userId/reset-password")
USER_AVATAR_REMOVE = DELETE("/api/admin/apps/:appkey/users/:userId/avatar")
USER_DEVICES = GET("/api/admin/apps/:appkey/users/devices")
USER_WALLET = GET("/api/admin/apps/:appkey/users/:userId/wallet")
USER_AD_POLICY = GET("/api/admin/apps/:appkey/users/:userId/ad-policy")

# ── 会话 ──
SESSIONS = GET("/api/admin/apps/:appkey/users/:userId/sessions")
SESSION_REVOKE = DELETE("/api/admin/apps/:appkey/users/:userId/sessions/:tokenHash")
SESSIONS_REVOKE_BATCH = POST("/api/admin/apps/:appkey/users/:userId/sessions/revoke-batch")
SESSIONS_REVOKE_ALL = POST("/api/admin/apps/:appkey/users/:userId/revoke-sessions")

# ── 封禁 ──
BANS = GET("/api/admin/apps/:appkey/users/:userId/bans")
BAN_ACTIVE = GET("/api/admin/apps/:appkey/users/:userId/bans/active")
BAN_CREATE = POST("/api/admin/apps/:appkey/users/:userId/bans")
BAN_BATCH = POST("/api/admin/apps/:appkey/users/bans/batch")
BAN_REVOKE = POST("/api/admin/apps/:appkey/users/:userId/bans/:banId/revoke")

# ── OAuth2 授权与登录绑定 ──
OAUTH2_GRANTS = GET("/api/admin/apps/:appkey/users/:userId/oauth2-grants")
OAUTH2_GRANTS_REVOKE = DELETE("/api/admin/apps/:appkey/users/:userId/oauth2-grants")
LOGIN_BASELINE = GET("/api/admin/apps/:appkey/login-baseline/:userid")
LOGIN_BASELINE_RESET = DELETE("/api/admin/apps/:appkey/login-baseline/:userid")

# ── 账号注销 ──
DELETION_CATALOG = GET("/api/admin/apps/:appkey/account-deletion/catalog")
DELETION_SETTINGS = GET("/api/admin/apps/:appkey/account-deletion/settings")
DELETION_SETTINGS_SAVE = PUT("/api/admin/apps/:appkey/account-deletion/settings")
DELETION_STATS = GET("/api/admin/apps/:appkey/account-deletion/stats")
DELETION_REQUESTS = GET("/api/admin/apps/:appkey/account-deletion/requests")
DELETION_REQUEST = GET("/api/admin/apps/:appkey/account-deletion/requests/:requestId")
DELETION_APPROVE = POST("/api/admin/apps/:appkey/account-deletion/requests/:requestId/approve")
DELETION_REJECT = POST("/api/admin/apps/:appkey/account-deletion/requests/:requestId/reject")
DELETION_CANCEL = POST("/api/admin/apps/:appkey/account-deletion/requests/:requestId/cancel")
DELETION_EXECUTE = POST("/api/admin/apps/:appkey/account-deletion/requests/:requestId/execute")
DELETION_RESCHEDULE = POST("/api/admin/apps/:appkey/account-deletion/requests/:requestId/reschedule")
DELETION_TOMBSTONES = GET("/api/admin/apps/:appkey/account-deletion/tombstones")
DELETION_TOMBSTONE_RELEASE = DELETE("/api/admin/apps/:appkey/account-deletion/tombstones/:tombstoneId")
USER_DELETION = GET("/api/admin/apps/:appkey/users/:userId/account-deletion")
USER_DELETION_CREATE = POST("/api/admin/apps/:appkey/users/:userId/account-deletion")

# ── 登录设备数控制 ──
DL_OVERVIEW = GET("/api/admin/apps/:appkey/device-limit")
DL_POLICY = GET("/api/admin/apps/:appkey/device-limit/policy")
DL_POLICY_SAVE = PUT("/api/admin/apps/:appkey/device-limit/policy")
DL_SIMULATE = POST("/api/admin/apps/:appkey/device-limit/simulate")
DL_EVENTS = GET("/api/admin/apps/:appkey/device-limit/events")
DL_OVERRIDES = GET("/api/admin/apps/:appkey/device-limit/overrides")
DL_OVERRIDE_SAVE = PUT("/api/admin/apps/:appkey/device-limit/overrides/:userId")
DL_OVERRIDE_DELETE = DELETE("/api/admin/apps/:appkey/device-limit/overrides/:userId")
DL_USER = GET("/api/admin/apps/:appkey/device-limit/users/:userId")
DL_REMOVE = POST("/api/admin/apps/:appkey/device-limit/users/:userId/remove")
DL_ENFORCE = POST("/api/admin/apps/:appkey/device-limit/users/:userId/enforce")
DL_LEDGER_RESET = DELETE("/api/admin/apps/:appkey/device-limit/users/:userId/ledger")

# ── 用户云存储 ──
CLOUD_USERS = GET("/api/admin/apps/:appkey/cloud-storage/users")
CLOUD_USER = GET("/api/admin/apps/:appkey/users/:userId/cloud-storage")
CLOUD_USER_SAVE = PUT("/api/admin/apps/:appkey/users/:userId/cloud-storage")
CLOUD_USER_PURGE = DELETE("/api/admin/apps/:appkey/users/:userId/cloud-storage")
CLOUD_ITEMS = GET("/api/admin/apps/:appkey/users/:userId/cloud-storage/items")
CLOUD_ITEM = GET("/api/admin/apps/:appkey/users/:userId/cloud-storage/items/:itemId")
CLOUD_ITEM_DELETE = DELETE("/api/admin/apps/:appkey/users/:userId/cloud-storage/items/:itemId")
CLOUD_ITEM_REVISIONS = GET("/api/admin/apps/:appkey/users/:userId/cloud-storage/items/:itemId/revisions")
CLOUD_ITEM_LINK = POST("/api/admin/apps/:appkey/users/:userId/cloud-storage/items/:itemId/link")
CLOUD_ITEM_RESTORE = POST("/api/admin/apps/:appkey/users/:userId/cloud-storage/items/:itemId/restore")
CLOUD_ITEM_ROLLBACK = POST("/api/admin/apps/:appkey/users/:userId/cloud-storage/items/:itemId/rollback")

UserIDs = Annotated[list[Annotated[int, Field(gt=0)]], Field(min_length=1, max_length=500, description="用户 ID 列表（管理端主键）")]
DeviceKeys = Annotated[list[str], Field(min_length=1, description="设备 Key 列表（users_device_limit_user 结果里 devices[].key）")]
ItemID = Annotated[int, Field(gt=0, description="云存储条目 ID（users_cloud_items 结果里的 id）")]
RequestIdArg = Annotated[int, Field(gt=0, description="注销申请 ID（users_deletion_requests 结果里的 id）")]
Tz = Annotated[str, Field(description="IANA 时区（如 Asia/Shanghai），用于按自然日划分趋势；留空用服务器默认")]

SEARCH_SORT = Literal["", "createdAt", "updatedAt", "id", "account", "integral", "experience", "vipExpireAt", "registerTime", "nickname", "email"]
DETAIL_INCLUDE = Literal[
    "active_ban", "bans", "sessions", "wallet", "oauth2_grants", "login_baseline",
    "account_deletion", "device_limit", "cloud_storage", "ad_policy",
]


# ═══════════════════════ 检索与详情 ═══════════════════════


@server.tool(**READ("users"))
async def search(
    app: AppRef,
    keyword: Annotated[str, Field(description="模糊关键字（账号 / 昵称 / 邮箱 / 手机号等；5–12 位纯数字也按用户号码精确匹配）")] = "",
    account: Annotated[str, Field(description="账号（精确或前缀匹配，按后端实现）")] = "",
    nickname: Annotated[str, Field(description="昵称")] = "",
    email: Annotated[str, Field(description="邮箱")] = "",
    phone: Annotated[str, Field(description="手机号")] = "",
    invite_code: Annotated[str, Field(description="邀请码")] = "",
    register_ip: Annotated[str, Field(description="注册 IP")] = "",
    markcode: Annotated[str, Field(description="设备标识 markcode")] = "",
    custom_id: Annotated[str, Field(description="自定义 ID")] = "",
    user_id: Annotated[int | None, Field(gt=0, description="按用户 ID 精确查")] = None,
    ids: Annotated[list[int] | None, Field(description="按一批用户 ID 取")] = None,
    enabled: Annotated[bool | None, Field(description="只看启用（true）或禁用（false）的用户，留空不限")] = None,
    created_from: Annotated[str, Field(description="注册时间起，ISO 8601 或 YYYY-MM-DD")] = "",
    created_to: Annotated[str, Field(description="注册时间止，ISO 8601 或 YYYY-MM-DD（只给日期时含当天全天）")] = "",
    sort: Annotated[SEARCH_SORT, Field(description="排序字段，默认 createdAt")] = "",
    order: Annotated[Literal["", "asc", "desc"], Field(description="排序方向，默认 desc")] = "",
    page: Page = 1,
    limit: Limit = 20,
) -> Any:
    """检索应用用户（支持账号、昵称、邮箱、手机号、邀请码、注册 IP、设备码、自定义 ID、启用状态、注册时间等过滤）。

    返回分页结果，items[].id 即后续工具要用的用户 ID，items[].number / numberTier 为用户号码与等级（展示用，可更换，见 numbers_）。需要 app:user:read。
    """
    return await USERS(appkey=app, query=q(
        keyword=keyword, account=account, nickname=nickname, email=email, phone=phone,
        inviteCode=invite_code, registerIp=register_ip, markcode=markcode, customId=custom_id,
        userId=user_id, ids=ids, enabled=enabled, createdFrom=created_from, createdTo=created_to,
        sort=sort, order=order, page=page, limit=limit,
    ))


@server.tool(name="get", **READ("users"))
async def get_user(
    app: AppRef,
    user_id: UserID,
    include: Annotated[list[DETAIL_INCLUDE] | None, Field(description=(
        "附加信息：active_ban 当前封禁、bans 封禁记录、sessions 在线会话、wallet 钱包摘要、oauth2_grants 第三方授权、"
        "login_baseline 登录绑定、account_deletion 注销状况、device_limit 设备数视图、cloud_storage 云存储概况、ad_policy 广告策略画像"
    ))] = None,
) -> Any:
    """一个用户的完整资料，可按需附带封禁、会话、钱包、授权、注销、设备数、云存储、广告策略等。

    资料里的 number / numberTier 是用户号码与等级（按应用唯一、可更换，权益与换号记录用 numbers_user 查）。

    附加项单独失败时不影响主体，失败原因写在 errors 里。
    """
    detail = await USER(appkey=app, userId=user_id)
    if not include:
        return detail
    loaders = {
        "active_ban": lambda: BAN_ACTIVE(appkey=app, userId=user_id),
        "bans": lambda: BANS(appkey=app, userId=user_id, query={"page": 1, "limit": 20}),
        "sessions": lambda: SESSIONS(appkey=app, userId=user_id),
        "wallet": lambda: USER_WALLET(appkey=app, userId=user_id),
        "oauth2_grants": lambda: OAUTH2_GRANTS(appkey=app, userId=user_id),
        "login_baseline": lambda: LOGIN_BASELINE(appkey=app, userid=user_id),
        "account_deletion": lambda: USER_DELETION(appkey=app, userId=user_id),
        "device_limit": lambda: DL_USER(appkey=app, userId=user_id),
        "cloud_storage": lambda: CLOUD_USER(appkey=app, userId=user_id),
        "ad_policy": lambda: USER_AD_POLICY(appkey=app, userId=user_id),
    }
    result: dict[str, Any] = {"user": detail}
    errors: dict[str, str] = {}
    for name in dict.fromkeys(include):
        try:
            result[name] = await loaders[name]()
        except ToolError as exc:
            errors[name] = str(exc)
    if errors:
        result["errors"] = errors
    return result


@server.tool(**READ("users"))
async def devices(
    app: AppRef,
    keyword: Annotated[str, Field(description="设备关键字（设备码、型号等）")] = "",
    user_id: Annotated[int | None, Field(gt=0, description="只看某个用户的设备")] = None,
    ids: Annotated[list[str] | None, Field(description="按设备 ID 批量取")] = None,
    limit: Annotated[int, Field(ge=1, le=200, description="最多返回条数")] = 30,
) -> Any:
    """应用内的用户设备列表（按关键字、用户或设备 ID 查）。"""
    return await USER_DEVICES(appkey=app, query=q(keyword=keyword, userId=user_id, ids=ids, limit=limit))


# ═══════════════════════ 资料、启停、删除 ═══════════════════════


@server.tool(**WRITE("users", idempotent=True))
async def update_profile(
    app: AppRef,
    user_id: UserID,
    nickname: Annotated[str | None, Field(description="昵称，不改留空")] = None,
    email: Annotated[str | None, Field(description="邮箱；传空串为解绑")] = None,
    phone: Annotated[str | None, Field(description="手机号；传空串为解绑")] = None,
    avatar: Annotated[str | None, Field(description="外部 http(s) 头像链接；空串回到默认头像")] = None,
    birthday: Annotated[str | None, Field(description="生日 YYYY-MM-DD；空串为清空")] = None,
    clear_birthday: Annotated[bool, Field(description="清空生日")] = False,
    bio: Annotated[str | None, Field(description="个人简介")] = None,
) -> Any:
    """修改用户资料（昵称、邮箱、手机号、头像链接、生日、简介）。只传要改的字段；可再次修改回去。"""
    payload = body(nickname=nickname, email=email, phone=phone, avatar=avatar, birthday=birthday, bio=bio)
    if clear_birthday:
        payload["clearBirthday"] = True
    if not payload:
        raise ToolError("没有要修改的字段")
    return await USER_PROFILE(appkey=app, userId=user_id, body=payload)


def _status_body(enabled: bool | None, disabled_end_time: str, clear_disabled_end_time: bool, disabled_reason: str | None) -> dict[str, Any]:
    payload = body(enabled=enabled, disabledReason=disabled_reason)
    if disabled_end_time:
        payload["disabledEndTime"] = disabled_end_time
    if clear_disabled_end_time:
        payload["clearDisabledEndTime"] = True
    if not payload:
        raise ToolError("需要 enabled / disabled_end_time / clear_disabled_end_time / disabled_reason 之一")
    return payload


@server.tool(**WRITE("users", idempotent=True))
async def set_status(
    app: AppRef,
    user_id: UserID,
    enabled: Annotated[bool | None, Field(description="启用（true）或禁用（false）")] = None,
    disabled_end_time: Annotated[IsoTime, Field(description="禁用到期时间（ISO 8601），到期自动解禁；留空为不限期")] = "",
    clear_disabled_end_time: Annotated[bool, Field(description="清除禁用到期时间")] = False,
    disabled_reason: Annotated[str | None, Field(description="禁用原因（用户可见）")] = None,
) -> Any:
    """启用或禁用一个用户（旧式禁用开关，可随时改回）。需要更细的封禁范围与证据时用 users_ban。"""
    return await USER_STATUS(appkey=app, userId=user_id, body=_status_body(enabled, disabled_end_time, clear_disabled_end_time, disabled_reason))


@server.tool(**DESTRUCTIVE("users"))
async def set_status_batch(
    app: AppRef,
    user_ids: UserIDs,
    enabled: Annotated[bool | None, Field(description="启用（true）或禁用（false）")] = None,
    disabled_end_time: Annotated[IsoTime, Field(description="禁用到期时间（ISO 8601）")] = "",
    clear_disabled_end_time: Annotated[bool, Field(description="清除禁用到期时间")] = False,
    disabled_reason: Annotated[str | None, Field(description="禁用原因（用户可见）")] = None,
) -> Any:
    """批量启用或禁用用户。影响面大，执行前先向用户确认名单与原因。"""
    payload = _status_body(enabled, disabled_end_time, clear_disabled_end_time, disabled_reason)
    return await USER_STATUS_BATCH(appkey=app, body={"userIds": user_ids, **payload})


@server.tool(name="delete", **DESTRUCTIVE("users"))
async def delete_user(app: AppRef, user_id: UserID) -> Any:
    """彻底删除用户及其关联数据（会话、授权、云存储等，订单与审计留存）。不可恢复，务必先向用户确认账号。

    想走冷静期、留墓碑防重新注册，用 users_create_deletion。
    """
    return await USER_DELETE(appkey=app, userId=user_id)


@server.tool(**DESTRUCTIVE("users"))
async def reset_password(
    app: AppRef,
    user_id: UserID,
    new_password: Annotated[str, Field(min_length=1, description="新密码（按应用密码规则校验）")],
) -> Any:
    """把用户密码直接重置为指定值。原密码不可找回，执行前先向用户确认。"""
    return await USER_RESET_PASSWORD(appkey=app, userId=user_id, body={"newPassword": new_password})


@server.tool(**DESTRUCTIVE("users"))
async def remove_avatar(app: AppRef, user_id: UserID) -> Any:
    """移除用户头像（删除上传的头像文件，回到默认头像）。不可恢复。"""
    return await USER_AVATAR_REMOVE(appkey=app, userId=user_id)


# ═══════════════════════ 会话 ═══════════════════════


@server.tool(**READ("users"))
async def sessions(app: AppRef, user_id: UserID) -> Any:
    """用户的在线会话（含设备、IP 与归属地）。items[].tokenHash 用于 users_revoke_sessions。"""
    return await SESSIONS(appkey=app, userId=user_id)


@server.tool(**DESTRUCTIVE("users"))
async def revoke_sessions(
    app: AppRef,
    user_id: UserID,
    token_hashes: Annotated[list[str] | None, Field(description="要撤销的会话 tokenHash；一个走单个撤销，多个走批量撤销")] = None,
    all_sessions: Annotated[bool, Field(description="踢出该用户的全部会话（同时断开实时连接）")] = False,
) -> Any:
    """撤销用户会话：指定 token_hashes 撤销其中几个，或 all_sessions=true 全部踢下线。被撤销的设备需要重新登录。"""
    hashes = [item.strip() for item in token_hashes or [] if item and item.strip()]
    if all_sessions and hashes:
        raise ToolError("token_hashes 与 all_sessions 只能二选一")
    if all_sessions:
        return await SESSIONS_REVOKE_ALL(appkey=app, userId=user_id)
    if not hashes:
        raise ToolError("需要 token_hashes 或 all_sessions=true")
    if len(hashes) == 1:
        return await SESSION_REVOKE(appkey=app, userId=user_id, tokenHash=hashes[0])
    return await SESSIONS_REVOKE_BATCH(appkey=app, userId=user_id, body={"tokenHashes": hashes})


# ═══════════════════════ 封禁 ═══════════════════════


BanType = Annotated[Literal["", "temporary", "permanent"], Field(description="封禁类型：temporary 临时（必须给 end_at）、permanent 永久；留空按是否给 end_at 推断")]
BanScope = Annotated[Literal["", "all", "login"], Field(description="封禁范围：all 全部访问（当场撤销全部会话与授权，默认）、login 只拦新登录")]
BanReason = Annotated[str, Field(min_length=1, description="封禁原因（用户被拒时可见，也进申诉记录）")]


def _ban_body(ban_type: str, ban_scope: str, reason: str, evidence: dict[str, Any] | None, start_at: str, end_at: str) -> dict[str, Any]:
    if ban_type == "temporary" and not end_at:
        raise ToolError("临时封禁必须给 end_at")
    payload: dict[str, Any] = {"reason": reason}
    payload.update(q(banType=ban_type, banScope=ban_scope, startAt=start_at, endAt=end_at))
    if evidence:
        payload["evidence"] = evidence
    return payload


@server.tool(**READ("users"))
async def bans(
    app: AppRef,
    user_id: UserID,
    active_only: Annotated[bool, Field(description="只取当前生效的那条封禁")] = False,
    status: Annotated[Literal["", "active", "expired", "revoked"], Field(description="按状态过滤封禁记录")] = "",
    page: Page = 1,
    limit: Limit = 20,
) -> Any:
    """用户的封禁记录（或只看当前生效的封禁）。"""
    if active_only:
        return await BAN_ACTIVE(appkey=app, userId=user_id)
    return await BANS(appkey=app, userId=user_id, query=q(status=status, page=page, limit=limit))


@server.tool(**DESTRUCTIVE("users"))
async def ban(
    app: AppRef,
    user_id: UserID,
    reason: BanReason,
    ban_type: BanType = "",
    ban_scope: BanScope = "",
    end_at: Annotated[IsoTime, Field(description="封禁结束时间（ISO 8601），临时封禁必填")] = "",
    start_at: Annotated[IsoTime, Field(description="开始时间，留空为立即；不支持未来时间")] = "",
    evidence: Annotated[dict[str, Any] | None, Field(description="证据（任意键值，存档用）")] = None,
) -> Any:
    """封禁一个用户。范围 all 会立刻踢下线并吊销第三方授权。可用 users_unban 解除，执行前先向用户确认。"""
    return await BAN_CREATE(appkey=app, userId=user_id, body=_ban_body(ban_type, ban_scope, reason, evidence, start_at, end_at))


@server.tool(**DESTRUCTIVE("users"))
async def ban_batch(
    app: AppRef,
    user_ids: UserIDs,
    reason: BanReason,
    ban_type: BanType = "",
    ban_scope: BanScope = "",
    end_at: Annotated[IsoTime, Field(description="封禁结束时间（ISO 8601），临时封禁必填")] = "",
    start_at: Annotated[IsoTime, Field(description="开始时间，留空为立即")] = "",
    evidence: Annotated[dict[str, Any] | None, Field(description="证据（任意键值）")] = None,
) -> Any:
    """批量封禁一批用户（同一原因与范围）。影响面大，执行前先向用户确认名单。"""
    payload = _ban_body(ban_type, ban_scope, reason, evidence, start_at, end_at)
    return await BAN_BATCH(appkey=app, body={"userIds": user_ids, **payload})


@server.tool(**WRITE("users"))
async def unban(
    app: AppRef,
    user_id: UserID,
    ban_id: Annotated[int, Field(gt=0, description="封禁记录 ID（users_bans 结果里的 id）")],
    reason: Annotated[str, Field(description="解除原因")] = "",
) -> Any:
    """解除（撤销）一条封禁。被吊销的会话不会恢复，用户需重新登录。"""
    return await BAN_REVOKE(appkey=app, userId=user_id, banId=ban_id, body={"reason": reason})


# ═══════════════════════ OAuth2 授权与登录绑定 ═══════════════════════


@server.tool(**READ("users"))
async def oauth2_grants(app: AppRef, user_id: UserID) -> Any:
    """用户授予第三方客户端（本应用 OAuth2 服务端）的授权记录。"""
    return await OAUTH2_GRANTS(appkey=app, userId=user_id)


@server.tool(**DESTRUCTIVE("users"))
async def revoke_oauth2_grants(
    app: AppRef,
    user_id: UserID,
    client_id: Annotated[str, Field(description="只撤销对这个客户端的授权；留空撤销全部")] = "",
) -> Any:
    """撤销用户对第三方客户端的 OAuth2 授权（对应令牌失效，第三方需重新授权）。"""
    return await OAUTH2_GRANTS_REVOKE(appkey=app, userId=user_id, query=q(clientId=client_id))


@server.tool(**READ("users"))
async def login_baseline(app: AppRef, user_id: UserID) -> Any:
    """用户的登录绑定基线（首次登录记下的设备 / IP，登录策略开了设备或 IP 校验时据此比对）。"""
    return await LOGIN_BASELINE(appkey=app, userid=user_id)


@server.tool(**DESTRUCTIVE("users"))
async def reset_login_baseline(app: AppRef, user_id: UserID) -> Any:
    """重置用户的登录绑定基线：下次登录重新记录设备 / IP（用户换机后登录不上时用）。"""
    return await LOGIN_BASELINE_RESET(appkey=app, userid=user_id)


# ═══════════════════════ 账号注销 ═══════════════════════


@server.tool(**READ("users"))
async def deletion_settings(
    app: AppRef,
    catalog: Annotated[bool, Field(description="同时取配置目录（枚举、默认配置、取值范围与字数上限）")] = False,
) -> Any:
    """账号注销的开关与配置（方式、冷静期、审核、验证方式、前置条件、重新注册限制等）。"""
    settings = await DELETION_SETTINGS(appkey=app)
    if not catalog:
        return settings
    return {"settings": settings, "catalog": await DELETION_CATALOG(appkey=app)}


@server.tool(**WRITE("users", idempotent=True))
async def save_deletion_settings(
    app: AppRef,
    enabled: Annotated[bool | None, Field(description="是否开放用户自助注销；留空保持原值")] = None,
    config: Annotated[dict[str, Any] | None, Field(description=(
        "要修改的配置字段（与现有配置浅合并），如 allowImmediate、allowCooling、defaultMode、coolingDays、requireReview、"
        "verifyMethods、requireTotp、reasons、guards、loginDuringCooling、reregisterDays、notifyEmail；字段见 docs/account-deletion.md"
    ))] = None,
) -> Any:
    """修改账号注销配置。只传要改的字段，其余沿用现有配置（工具会先读再合并保存）。"""
    if enabled is None and not config:
        raise ToolError("没有要修改的字段")
    current = await DELETION_SETTINGS(appkey=app) or {}
    merged_config = {**(current.get("config") or {}), **(config or {})}
    payload = {
        "enabled": current.get("enabled", False) if enabled is None else enabled,
        "config": merged_config,
    }
    return await DELETION_SETTINGS_SAVE(appkey=app, body=payload)


@server.tool(**READ("users"))
async def deletion_stats(
    app: AppRef,
    days: Annotated[int, Field(ge=7, le=180, description="统计窗口天数")] = 30,
    tz: Tz = "",
) -> Any:
    """账号注销统计：进行中、即将到期、墓碑数、挽留率，以及最近 N 天按状态 / 方式 / 来源 / 原因的分布与趋势。"""
    return await DELETION_STATS(appkey=app, query=q(days=days, tz=tz))


@server.tool(**READ("users"))
async def deletion_requests(
    app: AppRef,
    request_id: Annotated[int | None, Field(gt=0, description="给了就只取这一条申请的详情（含提交时的账号快照）")] = None,
    status: Annotated[Literal["", "reviewing", "scheduled", "processing", "completed", "cancelled", "rejected", "failed"], Field(description="申请状态")] = "",
    mode: Annotated[Literal["", "immediate", "cooling"], Field(description="注销方式")] = "",
    source: Annotated[Literal["", "user", "admin"], Field(description="发起方")] = "",
    user_id: Annotated[int | None, Field(gt=0, description="只看某个用户")] = None,
    keyword: Annotated[str, Field(description="关键字（账号等）")] = "",
    from_time: Annotated[IsoTime, Field(description="提交时间起（RFC 3339）")] = "",
    to_time: Annotated[IsoTime, Field(description="提交时间止（RFC 3339）")] = "",
    page: Page = 1,
    limit: Limit = 20,
) -> Any:
    """账号注销申请列表，或给 request_id 取一条详情。"""
    if request_id:
        return await DELETION_REQUEST(appkey=app, requestId=request_id)
    return await DELETION_REQUESTS(appkey=app, query=q(
        status=status, mode=mode, source=source, userId=user_id, keyword=keyword,
        **{"from": from_time, "to": to_time}, page=page, limit=limit,
    ))


@server.tool(**DESTRUCTIVE("users"))
async def decide_deletion(
    app: AppRef,
    request_id: RequestIdArg,
    action: Annotated[Literal["approve", "reject", "cancel"], Field(description=(
        "approve 批准待审核申请（之后进入冷静期或立即执行）、reject 驳回（note 必填，用户可见）、cancel 撤销（待审核 / 冷静期 / 执行失败的申请）"
    ))],
    note: Annotated[str, Field(description="备注；驳回时必填且用户可见")] = "",
    notify: Annotated[bool | None, Field(description="撤销时是否发信告知用户（默认发）")] = None,
) -> Any:
    """审核或撤销一条账号注销申请。批准立即注销型申请会直接删除账号，执行前先向用户确认。"""
    if action == "reject" and not note.strip():
        raise ToolError("驳回必须填写 note（用户可见）")
    route = {"approve": DELETION_APPROVE, "reject": DELETION_REJECT, "cancel": DELETION_CANCEL}[action]
    payload = body(note=note, notify=notify if action == "cancel" else None)
    return await route(appkey=app, requestId=request_id, body=payload)


@server.tool(**DESTRUCTIVE("users"))
async def execute_deletion(
    app: AppRef,
    request_id: RequestIdArg,
    confirm: Annotated[str, Field(min_length=1, description="二次确认：填该申请的账号名")],
) -> Any:
    """立即执行一条冷静期中或执行失败的注销申请：吊销会话、清空云存储、删除账号。不可恢复，务必先向用户确认。"""
    return await DELETION_EXECUTE(appkey=app, requestId=request_id, body={"confirm": confirm})


@server.tool(**WRITE("users"))
async def reschedule_deletion(
    app: AppRef,
    request_id: RequestIdArg,
    scheduled_at: Annotated[str, Field(min_length=1, description="新的到期时刻（ISO 8601），晚于现在且不晚于一年后")],
    notify: Annotated[bool | None, Field(description="是否发信告知用户（默认发）")] = None,
) -> Any:
    """调整冷静期中注销申请的到期执行时刻。"""
    return await DELETION_RESCHEDULE(appkey=app, requestId=request_id, body=body(scheduledAt=scheduled_at, notify=notify))


@server.tool(**READ("users"))
async def user_deletion(app: AppRef, user_id: UserID) -> Any:
    """某个用户的注销状况：进行中的申请与历史申请。"""
    return await USER_DELETION(appkey=app, userId=user_id)


@server.tool(**DESTRUCTIVE("users"))
async def create_deletion(
    app: AppRef,
    user_id: UserID,
    mode: Annotated[Literal["cooling", "immediate"], Field(description="cooling 进入冷静期、immediate 立即注销（须 confirm 账号名）")],
    reason_code: Annotated[str, Field(description="配置里的注销原因 code（与 reason 至少给一个）")] = "",
    reason: Annotated[str, Field(description="自定义原因（与 reason_code 至少给一个）")] = "",
    cooling_days: Annotated[int | None, Field(ge=1, le=180, description="冷静期天数，留空用应用配置")] = None,
    notify: Annotated[bool | None, Field(description="是否发信告知用户（默认发）")] = None,
    confirm: Annotated[str, Field(description="立即注销时必填：该用户的账号名")] = "",
) -> Any:
    """管理员代用户发起账号注销（不过验证与前置条件）。立即注销直接删除账号且不可恢复，务必先向用户确认。"""
    if not reason_code and not reason:
        raise ToolError("reason_code 与 reason 至少给一个")
    if mode == "immediate" and not confirm:
        raise ToolError("立即注销必须用 confirm 填写该用户的账号名")
    return await USER_DELETION_CREATE(appkey=app, userId=user_id, body=body(
        mode=mode, coolingDays=cooling_days, reasonCode=reason_code or None, reason=reason or None,
        notify=notify, confirm=confirm or None,
    ))


@server.tool(**READ("users"))
async def deletion_tombstones(
    app: AppRef,
    keyword: Annotated[str, Field(description="关键字（打码后的账号 / 邮箱 / 手机号提示）")] = "",
    page: Page = 1,
    limit: Limit = 20,
) -> Any:
    """未到期的重新注册限制（注销墓碑）：注销后 N 天内同一账号名、邮箱、手机号不能再注册。"""
    return await DELETION_TOMBSTONES(appkey=app, query=q(keyword=keyword, page=page, limit=limit))


@server.tool(**DESTRUCTIVE("users"))
async def release_tombstone(
    app: AppRef,
    tombstone_id: Annotated[int, Field(gt=0, description="墓碑 ID（users_deletion_tombstones 结果里的 id）")],
) -> Any:
    """解除一条重新注册限制（删除墓碑后该账号名 / 邮箱 / 手机号可立即重新注册）。不可恢复。"""
    return await DELETION_TOMBSTONE_RELEASE(appkey=app, tombstoneId=tombstone_id)


# ═══════════════════════ 登录设备数控制 ═══════════════════════


@server.tool(**READ("users"))
async def device_limit_overview(
    app: AppRef,
    days: Annotated[int, Field(ge=0, le=365, description="统计天数，0 用默认")] = 0,
    tz: Tz = "",
    policy: Annotated[bool, Field(description="同时取当前策略")] = False,
) -> Any:
    """登录设备数控制概览（处置统计与趋势），可附带当前策略。"""
    overview = await DL_OVERVIEW(appkey=app, query=q(days=days or None, tz=tz))
    if not policy:
        return overview
    return {"overview": overview, "policy": await DL_POLICY(appkey=app)}


@server.tool(**WRITE("users", idempotent=True))
async def update_device_limit_policy(
    app: AppRef,
    enabled: Annotated[bool | None, Field(description="是否启用设备数控制")] = None,
    max_devices: Annotated[int | None, Field(ge=0, description="同时登录设备总数上限，0 为不限总数")] = None,
    class_limits: Annotated[dict[str, int] | None, Field(description="各终端类别上限 {mobile, desktop, web, other}，0 为不单独限制；整体替换")] = None,
    overflow: Annotated[Literal["kick_oldest", "kick_idle", "reject", "ask"] | None, Field(description="超限处置：kick_oldest 下线最早登录、kick_idle 下线最久未活动、reject 拒绝登录、ask 让用户选择")] = None,
    ask_fallback: Annotated[Literal["kick_oldest", "kick_idle", "reject"] | None, Field(description="ask 但客户端不支持选择时的处置")] = None,
    same_device: Annotated[Literal["replace", "coexist"] | None, Field(description="同一设备再登录：replace 顶掉旧登录、coexist 保留")] = None,
    tiers: Annotated[list[dict[str, Any]] | None, Field(description="会员档位规则（整体替换），每条 {id, name, audience: trial|vip|lifetime|feature, featureTag, maxDevices, classLimits}")] = None,
    new_device: Annotated[dict[str, Any] | None, Field(description="新设备频率 {enabled, windowDays, maxNew}（与现有值合并）")] = None,
    notify_kicked: Annotated[bool | None, Field(description="设备被自动下线时给用户发站内信")] = None,
    reject_message: Annotated[str | None, Field(description="拒绝登录时的提示，可用 {limit} 占位")] = None,
) -> Any:
    """修改登录设备数策略。只传要改的字段（工具先读现有策略再合并保存）。需要 app:write；调低上限不会自动下线已登录设备，需要时对用户调用 users_enforce_device_limit。"""
    view = await DL_POLICY(appkey=app) or {}
    current = dict(view.get("policy") or {})
    patch = body(
        enabled=enabled, maxDevices=max_devices, classLimits=class_limits, overflow=overflow,
        askFallback=ask_fallback, sameDevice=same_device, tiers=tiers,
        notifyKicked=notify_kicked, rejectMessage=reject_message,
    )
    if new_device is not None:
        patch["newDevice"] = {**(current.get("newDevice") or {}), **new_device}
    if not patch:
        raise ToolError("没有要修改的字段")
    return await DL_POLICY_SAVE(appkey=app, body={**current, **patch})


@server.tool(**READ("users"))
async def simulate_device_limit(
    app: AppRef,
    platform: Annotated[str, Field(description="正在登录的设备平台（android / ios / harmonyos / windows / macos / linux / web 等）")] = "",
    user_id: Annotated[int | None, Field(gt=0, description="用这个真实用户的会员身份、例外与在线设备（给了就忽略 member 与 devices）")] = None,
    policy: Annotated[dict[str, Any] | None, Field(description="草稿策略（结构同 users_device_limit_overview 的 policy）；留空用已保存的策略")] = None,
    member: Annotated[dict[str, Any] | None, Field(description="虚构的会员身份")] = None,
    override: Annotated[dict[str, Any] | None, Field(description="虚构的单人例外 {exempt, maxDevices, classLimits}")] = None,
    devices: Annotated[list[dict[str, Any]] | None, Field(description="虚构的已登录设备列表")] = None,
    device_key: Annotated[str, Field(description="正在登录的设备 Key")] = "",
    known: Annotated[bool, Field(description="正在登录的设备是否已在台账里")] = False,
    new_devices_in_window: Annotated[int, Field(ge=0, description="虚构场景下窗口内已有的新设备数")] = 0,
    can_ask: Annotated[bool, Field(description="客户端是否支持让用户选择")] = False,
) -> Any:
    """模拟一次登录在设备数策略下的判定结果（不落库、不影响用户）。改策略前先用它预览。"""
    return await DL_SIMULATE(appkey=app, body=body(
        policy=policy, userId=user_id, member=member, override=override, devices=devices,
        platform=platform or None, deviceKey=device_key or None, known=known,
        newDevicesInWindow=new_devices_in_window, canAsk=can_ask,
    ))


@server.tool(**READ("users"))
async def device_limit_events(
    app: AppRef,
    action: Annotated[Literal["", "kicked", "rejected", "challenged", "resolved", "new_device", "user_removed", "admin_removed", "admin_enforced"], Field(description="处置类型")] = "",
    dimension: Annotated[Literal["", "total", "class", "new_device"], Field(description="触发维度")] = "",
    user_id: Annotated[int | None, Field(gt=0, description="只看某个用户")] = None,
    keyword: Annotated[str, Field(description="关键字")] = "",
    from_time: Annotated[IsoTime, Field(description="起始时间（RFC 3339）")] = "",
    to_time: Annotated[IsoTime, Field(description="结束时间（RFC 3339）")] = "",
    page: Page = 1,
    limit: Limit = 20,
) -> Any:
    """登录设备数的处置记录（自动下线、拒绝、选择页、管理员下线等）。"""
    return await DL_EVENTS(appkey=app, query=q(
        action=action, dimension=dimension, userId=user_id, keyword=keyword,
        **{"from": from_time, "to": to_time}, page=page, limit=limit,
    ))


@server.tool(**READ("users"))
async def device_limit_overrides(
    app: AppRef,
    keyword: Annotated[str, Field(description="关键字（账号等）")] = "",
    page: Page = 1,
    limit: Limit = 20,
) -> Any:
    """登录设备数的单人例外名单。"""
    return await DL_OVERRIDES(appkey=app, query=q(keyword=keyword, page=page, limit=limit))


@server.tool(**WRITE("users", idempotent=True))
async def set_device_limit_override(
    app: AppRef,
    user_id: UserID,
    exempt: Annotated[bool, Field(description="豁免：不受任何设备数限制")] = False,
    max_devices: Annotated[int, Field(ge=0, description="该用户的设备总数上限，0 为不限总数")] = 0,
    class_limits: Annotated[dict[str, int] | None, Field(description="该用户的类别上限 {mobile, desktop, web, other}")] = None,
    note: Annotated[str, Field(description="备注")] = "",
    expires_at: Annotated[IsoTime, Field(description="到期时间（ISO 8601），留空长期有效")] = "",
) -> Any:
    """为单个用户设置设备数例外（豁免或另配上限，优先于会员档位）。整体覆盖该用户原有例外。"""
    payload: dict[str, Any] = {"exempt": exempt, "maxDevices": max_devices, "note": note}
    if class_limits:
        payload["classLimits"] = class_limits
    if expires_at:
        payload["expiresAt"] = expires_at
    return await DL_OVERRIDE_SAVE(appkey=app, userId=user_id, body=payload)


@server.tool(**WRITE("users"))
async def delete_device_limit_override(app: AppRef, user_id: UserID) -> Any:
    """删除用户的设备数例外，恢复按会员档位 / 默认策略限制（不会自动下线已登录设备）。"""
    return await DL_OVERRIDE_DELETE(appkey=app, userId=user_id)


@server.tool(**READ("users"))
async def device_limit_user(app: AppRef, user_id: UserID) -> Any:
    """用户的设备数视图：生效上限、在线设备（devices[].key）、台账、例外与最近处置。"""
    return await DL_USER(appkey=app, userId=user_id)


@server.tool(**DESTRUCTIVE("users"))
async def remove_devices(app: AppRef, user_id: UserID, device_keys: DeviceKeys) -> Any:
    """下线用户的指定设备（这些设备的令牌全部失效，下次请求报 40131）。"""
    return await DL_REMOVE(appkey=app, userId=user_id, body={"deviceKeys": device_keys})


@server.tool(**DESTRUCTIVE("users"))
async def enforce_device_limit(app: AppRef, user_id: UserID) -> Any:
    """按当前策略收回该用户超出上限的设备（调低上限后立即生效用）。被收回的设备需要重新登录。"""
    return await DL_ENFORCE(appkey=app, userId=user_id)


@server.tool(**DESTRUCTIVE("users"))
async def reset_device_ledger(
    app: AppRef,
    user_id: UserID,
    key: Annotated[str, Field(description="只删这一台设备的台账；留空清空全部")] = "",
) -> Any:
    """清空用户的设备台账（新设备频率从零算）。不会下线在线设备，不可恢复。"""
    return await DL_LEDGER_RESET(appkey=app, userId=user_id, query=q(key=key))


# ═══════════════════════ 用户云存储 ═══════════════════════


@server.tool(**READ("users"))
async def cloud_users(
    app: AppRef,
    keyword: Annotated[str, Field(description="关键字（账号等）")] = "",
    sort: Annotated[Literal["", "usage", "items", "recent"], Field(description="排序：usage 按用量（默认）、items 按条目数、recent 按最近写入")] = "",
    frozen: Annotated[bool | None, Field(description="只看冻结（true）或未冻结（false）的用户")] = None,
    page: Page = 1,
    limit: Limit = 20,
) -> Any:
    """使用云存储的用户列表（用量、条目数、配额、冻结状态）。"""
    return await CLOUD_USERS(appkey=app, query=q(
        keyword=keyword, sort="" if sort == "usage" else sort,
        frozen=None if frozen is None else ("true" if frozen else "false"), page=page, limit=limit,
    ))


@server.tool(**READ("users"))
async def cloud_storage(app: AppRef, user_id: UserID) -> Any:
    """单个用户的云存储概况：配额、用量、冻结、各命名空间用量与限额。"""
    return await CLOUD_USER(appkey=app, userId=user_id)


@server.tool(**WRITE("users", idempotent=True))
async def save_cloud_storage(
    app: AppRef,
    user_id: UserID,
    frozen: Annotated[bool, Field(description="冻结（只读，写入报 40361）；false 为解冻")],
    quota_bytes: Annotated[int | None, Field(gt=0, description="单独配额（字节）；留空用应用默认配额")] = None,
    frozen_reason: Annotated[str, Field(description="冻结原因（frozen=false 时忽略）")] = "",
    note: Annotated[str, Field(description="管理员备注")] = "",
) -> Any:
    """设置用户云存储的配额覆盖、冻结与备注。整体覆盖：未给 quota_bytes 即回到应用默认配额。"""
    return await CLOUD_USER_SAVE(appkey=app, userId=user_id, body={
        "quotaBytes": quota_bytes, "frozen": frozen, "frozenReason": frozen_reason, "note": note,
    })


@server.tool(**DESTRUCTIVE("users"))
async def purge_cloud_storage(app: AppRef, user_id: UserID) -> Any:
    """清空用户的全部云存储（条目、修订与对象文件）。不可恢复，务必先向用户确认。"""
    return await CLOUD_USER_PURGE(appkey=app, userId=user_id)


@server.tool(**READ("users"))
async def cloud_items(
    app: AppRef,
    user_id: UserID,
    item_id: Annotated[int | None, Field(gt=0, description="给了就取这一条的内容")] = None,
    revision: Annotated[int | None, Field(gt=0, description="配合 item_id：取指定修订版本")] = None,
    revisions: Annotated[bool, Field(description="配合 item_id：列出该条目的全部修订")] = False,
    namespace: Annotated[str, Field(description="命名空间")] = "",
    keyword: Annotated[str, Field(description="关键字（键名）")] = "",
    status: Annotated[Literal["", "active", "deleted", "all"], Field(description="active 正常、deleted 回收站、all 全部")] = "",
    page: Page = 1,
    limit: Limit = 20,
) -> Any:
    """用户的云存储条目列表；给 item_id 取单条内容（可指定修订），或 revisions=true 列出修订历史。"""
    if item_id:
        if revisions:
            return await CLOUD_ITEM_REVISIONS(appkey=app, userId=user_id, itemId=item_id)
        return await CLOUD_ITEM(appkey=app, userId=user_id, itemId=item_id, query=q(revision=revision))
    if revisions or revision:
        raise ToolError("revision / revisions 需要配合 item_id")
    return await CLOUD_ITEMS(appkey=app, userId=user_id, query=q(
        namespace=namespace, keyword=keyword, status=status, page=page, limit=limit,
    ))


@server.tool(**WRITE("users"))
async def cloud_item_link(
    app: AppRef,
    user_id: UserID,
    item_id: ItemID,
    revision: Annotated[int | None, Field(gt=0, description="指定修订版本，留空为当前版本")] = None,
    download: Annotated[bool, Field(description="生成附件下载链接（而非内联查看）")] = False,
) -> Any:
    """为用户的一条云存储文件签发临时访问链接。链接可直接读到用户私有数据，不要外传。"""
    return await CLOUD_ITEM_LINK(appkey=app, userId=user_id, itemId=item_id, body=body(revision=revision, download=download))


@server.tool(**DESTRUCTIVE("users"))
async def delete_cloud_item(
    app: AppRef,
    user_id: UserID,
    item_id: ItemID,
    permanent: Annotated[bool, Field(description="永久删除（不可恢复）；默认只移入回收站")] = False,
) -> Any:
    """删除用户的一条云存储条目：默认移入回收站（可用 users_restore_cloud_item 恢复），permanent=true 永久删除。"""
    return await CLOUD_ITEM_DELETE(appkey=app, userId=user_id, itemId=item_id, query=q(permanent=True if permanent else None))


@server.tool(**WRITE("users"))
async def restore_cloud_item(app: AppRef, user_id: UserID, item_id: ItemID) -> Any:
    """把回收站里的一条云存储条目恢复为正常状态。"""
    return await CLOUD_ITEM_RESTORE(appkey=app, userId=user_id, itemId=item_id)


@server.tool(**WRITE("users"))
async def rollback_cloud_item(
    app: AppRef,
    user_id: UserID,
    item_id: ItemID,
    revision: Annotated[int, Field(gt=0, description="回滚到的修订版本（users_cloud_items revisions=true 可查）")],
) -> Any:
    """把一条云存储条目回滚到指定修订（生成一条新修订，原有修订保留，可再回滚）。"""
    return await CLOUD_ITEM_ROLLBACK(appkey=app, userId=user_id, itemId=item_id, body={"revision": revision})

