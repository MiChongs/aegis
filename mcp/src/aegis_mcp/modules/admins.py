"""管理员与权限：管理员账号与角色分配、自定义角色、授权策略与判定解释、临时权限、权限委托、管理员会话、在线统计。"""

from __future__ import annotations

from typing import Annotated, Any, Literal

from fastmcp import FastMCP
from fastmcp.exceptions import ToolError
from pydantic import Field

from ..core import DELETE, DESTRUCTIVE, GET, POST, PUT, READ, WRITE, IsoTime, Limit, Page, Reason, app_id_of, body, q

NAMESPACE = "admins"
server = FastMCP("admins")

ADMINS = GET("/api/admin/system/admins")
ADMINS_ONLINE = GET("/api/admin/system/admins/online")
ADMIN_CREATE = POST("/api/admin/system/admins")
ADMIN_ACCESS = PUT("/api/admin/system/admins/:adminId/access")
ADMIN_STATUS = PUT("/api/admin/system/admins/:adminId/status")
ADMIN_SESSIONS = GET("/api/admin/system/admins/:adminId/sessions")
ADMIN_FORCE_LOGOUT = POST("/api/admin/system/admins/:adminId/force-logout")
SESSIONS = GET("/api/admin/system/sessions")
SESSION_REVOKE = POST("/api/admin/system/sessions/:sessionId/revoke")

ROLES = GET("/api/admin/system/roles")
ROLE_TREE = GET("/api/admin/system/roles/permissions")
ROLE_MATRIX = GET("/api/admin/system/roles/matrix")
ROLE_GRAPH = GET("/api/admin/system/roles/graph")
ROLE_IMPACT = GET("/api/admin/system/roles/:roleKey/impact")
ROLE_CREATE = POST("/api/admin/system/roles")
ROLE_UPDATE = PUT("/api/admin/system/roles/:roleKey")
ROLE_DELETE = DELETE("/api/admin/system/roles/:roleKey")

AUTHZ_MODEL = GET("/api/admin/system/authz/model")
AUTHZ_POLICIES = GET("/api/admin/system/authz/policies")
AUTHZ_SUBJECT = GET("/api/admin/system/authz/policies/subject")
AUTHZ_EXPLAIN = POST("/api/admin/system/authz/explain")
AUTHZ_RELOAD = POST("/api/admin/system/authz/reload")
AUTHZ_ROLE_OVERRIDE = POST("/api/admin/system/authz/roles/override")
AUTHZ_GRANTS = PUT("/api/admin/system/authz/admins/:adminId/grants")

TEMP_PERMISSIONS = GET("/api/admin/system/temp-permissions")
TEMP_GRANT = POST("/api/admin/system/temp-permissions")
TEMP_REVOKE = POST("/api/admin/system/temp-permissions/:permId/revoke")
DELEGATIONS = GET("/api/admin/system/delegations")
DELEGATION_CREATE = POST("/api/admin/system/delegations")
DELEGATION_REVOKE = POST("/api/admin/system/delegations/:delegationId/revoke")

ONLINE_STATS = GET("/api/admin/system/online/stats")
ONLINE_APP = GET("/api/admin/system/online/apps/:appkey")
ONLINE_APP_USERS = GET("/api/admin/system/online/apps/:appkey/users")

AdminID = Annotated[int, Field(gt=0, description="管理员 ID")]
Assignments = Annotated[list[dict[str, Any]], Field(description="角色分配：[{roleKey, appid}]，appid 为空表示全局角色；角色见 admins_roles")]


# ── 管理员 ──

@server.tool(name="list", **READ("admins"))
async def list_admins(online: Annotated[bool, Field(description="只看当前在线的管理员")] = False) -> Any:
    """管理员列表（含角色分配、状态、最近登录），或当前在线管理员。"""
    return await (ADMINS_ONLINE() if online else ADMINS())


@server.tool(**WRITE("admins"))
async def create(
    account: Annotated[str, Field(min_length=3, description="登录账号")],
    password: Annotated[str, Field(min_length=8, description="初始密码（由用户提供；建议对方首次登录后修改）")],
    display_name: str = "",
    email: str = "",
    tier: Annotated[Literal["admin", "super", "system"], Field(description="身份：admin 管理员 / super 超级管理员 / system 系统管理员（后两者只有系统管理员能授予）")] = "admin",
    assignments: Annotated[list[dict[str, Any]] | None, Field(description="角色分配：[{roleKey, appid}]")] = None,
) -> Any:
    """新建管理员账号。需要「管理员：新增」（system:admin:create）。授予超级管理员 / 系统管理员前务必与用户确认。"""
    return await ADMIN_CREATE(body=body(account=account, password=password, displayName=display_name, email=email,
                                        tier=tier, isSuperAdmin=tier != "admin", assignments=assignments or []))


@server.tool(**DESTRUCTIVE("admins"))
async def set_access(
    admin_id: AdminID,
    assignments: Assignments,
    tier: Annotated[Literal["admin", "super", "system"], Field(description="身份档位；只有系统管理员能授予或收回 super / system")] = "admin",
) -> Any:
    """整体替换一个管理员的身份档位与角色分配（立即生效）。只能管理档位低于自己的人，不能改自己。先用 admins_list 看清现状并向用户确认。"""
    return await ADMIN_ACCESS(adminId=admin_id, body={"assignments": assignments, "tier": tier, "isSuperAdmin": tier != "admin"})


@server.tool(**DESTRUCTIVE("admins"))
async def set_status(admin_id: AdminID, status: Annotated[Literal["active", "disabled"], Field(description="active 启用 / disabled 停用（停用即刻踢下线，其 MCP 密钥同时失效）")]) -> Any:
    """启用或停用管理员账号。"""
    return await ADMIN_STATUS(adminId=admin_id, body={"status": status})


@server.tool(**READ("admins"))
async def sessions(admin_id: Annotated[int | None, Field(gt=0, description="只看这个管理员的会话；不填看全部")] = None, page: Page = 1, limit: Limit = 50) -> Any:
    """管理员登录会话（设备、IP、最近活动）。"""
    if admin_id:
        return await ADMIN_SESSIONS(adminId=admin_id)
    return await SESSIONS(query=q(page=page, limit=limit))


@server.tool(**DESTRUCTIVE("admins"))
async def revoke_sessions(
    admin_id: Annotated[int | None, Field(gt=0, description="强制这个管理员全部下线")] = None,
    session_id: Annotated[str, Field(description="只撤销这一个会话")] = "",
) -> Any:
    """强制管理员下线：撤销单个会话，或该管理员的全部会话。"""
    if session_id:
        return await SESSION_REVOKE(sessionId=session_id)
    if admin_id:
        return await ADMIN_FORCE_LOGOUT(adminId=admin_id)
    raise ToolError("需要 admin_id 或 session_id")


# ── 角色 ──

@server.tool(**READ("admins"))
async def roles(
    view: Annotated[Literal["catalog", "permissions", "matrix", "graph"], Field(description="catalog 角色目录 / permissions 角色权限树 / matrix 权限矩阵 / graph 继承关系")] = "catalog",
    impact_of: Annotated[str, Field(description="给角色 key 时改为预览修改该角色会影响哪些管理员")] = "",
) -> Any:
    """角色与权限：目录、权限树、矩阵、继承图，或某个角色的变更影响预览。"""
    if impact_of:
        return await ROLE_IMPACT(roleKey=impact_of)
    return await {"catalog": ROLES, "permissions": ROLE_TREE, "matrix": ROLE_MATRIX, "graph": ROLE_GRAPH}[view]()


@server.tool(**WRITE("admins"))
async def save_role(
    role_key: Annotated[str, Field(min_length=2, description="角色 key（新建时自定义，修改时指定已有的自定义角色）")],
    name: Annotated[str, Field(min_length=1)],
    permissions: Annotated[list[str], Field(description="权限点列表，支持 `ticket:*` 前缀通配")],
    create_new: Annotated[bool, Field(description="true 新建 / false 修改")] = True,
    description: str = "",
    level: int | None = None,
    scope: Annotated[Literal["", "global", "app"], Field(description="新建时的作用域")] = "",
    base_role: Annotated[str, Field(description="新建时继承的基础角色")] = "",
) -> Any:
    """新建或修改自定义角色。改动立即影响所有持有该角色的管理员，修改前可先用 admins_roles(impact_of=...) 预览。"""
    if create_new:
        return await ROLE_CREATE(body=body(roleKey=role_key, name=name, permissions=permissions, description=description,
                                           level=level, scope=scope or None, baseRole=base_role or None))
    return await ROLE_UPDATE(roleKey=role_key, body=body(name=name, permissions=permissions, description=description, level=level))


@server.tool(**DESTRUCTIVE("admins"))
async def delete_role(role_key: Annotated[str, Field(min_length=2)]) -> Any:
    """删除自定义角色（持有者随即失去其权限）。"""
    return await ROLE_DELETE(roleKey=role_key)


# ── 授权策略 ──

@server.tool(**READ("admins"))
async def authz(
    view: Annotated[Literal["model", "policies", "subject"], Field(description="model 授权模型 / policies 生效中的全部策略 / subject 某主体的策略")] = "model",
    subject: Annotated[str, Field(description="view=subject 时的主体，如 admin:12 / role:app_admin")] = "",
) -> Any:
    """授权引擎自述：权限目录、内置角色、路由规则，或当前生效的策略。"""
    if view == "subject":
        if not subject:
            raise ToolError("view=subject 需要 subject")
        return await AUTHZ_SUBJECT(query={"subject": subject})
    return await (AUTHZ_MODEL() if view == "model" else AUTHZ_POLICIES())


@server.tool(**WRITE("admins", idempotent=True))
async def explain(
    admin_id: AdminID,
    permission: Annotated[str, Field(min_length=3, description="权限点，如 app:user:write")],
    app: Annotated[str, Field(description="在哪个应用作用域下判定（appKey 或 ID）；空为平台级")] = "",
) -> Any:
    """解释「某管理员在某作用域下能不能做某事、为什么」——排查 403 的首选。"""
    return await AUTHZ_EXPLAIN(body=body(adminId=admin_id, permission=permission, appid=await app_id_of(app) if app else None))


@server.tool(**DESTRUCTIVE("admins"))
async def set_policy(
    target: Annotated[Literal["role", "admin"], Field(description="role 对角色增减 / admin 对某个管理员直接授予或禁止")],
    role_key: Annotated[str, Field(description="target=role 时的角色")] = "",
    allow: Annotated[list[str] | None, Field(description="target=role：追加允许的权限点")] = None,
    deny: Annotated[list[str] | None, Field(description="target=role：显式禁止的权限点")] = None,
    inherits: Annotated[list[str] | None, Field(description="target=role：继承的角色")] = None,
    admin_id: Annotated[int | None, Field(gt=0, description="target=admin 时的管理员")] = None,
    grants: Annotated[list[dict[str, Any]] | None, Field(description="target=admin：整体替换的授予列表 [{permission, effect: allow|deny, appid, note}]")] = None,
    note: str = "",
) -> Any:
    """修改授权策略（超级管理员）：对任意角色做增减，或对单个管理员直接授予 / 禁止。一条 allow 就是提权，先向用户确认。"""
    if target == "role":
        if not role_key:
            raise ToolError("target=role 需要 role_key")
        return await AUTHZ_ROLE_OVERRIDE(body=body(roleKey=role_key, allow=allow or [], deny=deny or [], inherits=inherits, note=note))
    if not admin_id or grants is None:
        raise ToolError("target=admin 需要 admin_id 与 grants")
    return await AUTHZ_GRANTS(adminId=admin_id, body={"grants": grants})


@server.tool(**WRITE("admins", idempotent=True))
async def reload_policies() -> Any:
    """让所有实例重新加载授权策略（排障用，超级管理员）。"""
    return await AUTHZ_RELOAD()


# ── 临时权限与委托 ──

@server.tool(**READ("admins"))
async def temp_permissions(admin_id: Annotated[int | None, Field(gt=0, description="只看这个管理员的")] = None) -> Any:
    """临时权限（到点自动失效）列表。"""
    return await TEMP_PERMISSIONS(query=q(adminId=admin_id))


@server.tool(**WRITE("admins"))
async def grant_temp_permission(
    admin_id: AdminID,
    permission: Annotated[str, Field(min_length=3)],
    expires_at: Annotated[str, Field(min_length=10, description="到期时间 ISO 8601")],
    reason: Reason,
    app: Annotated[str, Field(description="限定在某个应用（appKey 或 ID）；空为平台级")] = "",
) -> Any:
    """授予临时权限，到期自动失效。"""
    return await TEMP_GRANT(body=body(adminId=admin_id, permission=permission, expiresAt=expires_at, reason=reason,
                                      appId=await app_id_of(app) if app else None))


@server.tool(**DESTRUCTIVE("admins"))
async def revoke_temp_permission(permission_id: Annotated[int, Field(gt=0)]) -> Any:
    """提前收回一条临时权限。"""
    return await TEMP_REVOKE(permId=permission_id)


@server.tool(**READ("admins"))
async def delegations(admin_id: AdminID, role: Annotated[Literal["delegator", "delegate"], Field(description="delegator 他委托出去的 / delegate 委托给他的")] = "delegator") -> Any:
    """权限委托记录。"""
    return await DELEGATIONS(query={"adminId": admin_id, "role": role})


@server.tool(**WRITE("admins"))
async def delegate(
    delegator_id: AdminID,
    delegate_id: AdminID,
    reason: Reason,
    expires_at: IsoTime = "",
    scope: Annotated[str, Field(description="委托范围，如 all / app")] = "",
    scope_id: Annotated[int | None, Field(description="范围对象 ID（如应用 ID）")] = None,
) -> Any:
    """创建权限委托（被委托人在期限内代行委托人的权限）。"""
    return await DELEGATION_CREATE(body=body(delegatorId=delegator_id, delegateId=delegate_id, reason=reason,
                                             expiresAt=expires_at or None, scope=scope or None, scopeId=scope_id))


@server.tool(**DESTRUCTIVE("admins"))
async def revoke_delegation(delegation_id: Annotated[int, Field(gt=0)]) -> Any:
    """撤销一条权限委托。"""
    return await DELEGATION_REVOKE(delegationId=delegation_id)


# ── 在线 ──

@server.tool(**READ("admins"))
async def online(
    app: Annotated[str, Field(description="给应用时看该应用的在线情况；空为全站统计")] = "",
    users: Annotated[bool, Field(description="给应用时同时列在线用户")] = False,
    page: Page = 1,
    limit: Limit = 20,
) -> Any:
    """终端用户实时在线统计：全站或某个应用，可列在线用户。"""
    if not app:
        return await ONLINE_STATS()
    result: dict[str, Any] = {"stats": await ONLINE_APP(appkey=app)}
    if users:
        result["users"] = await ONLINE_APP_USERS(appkey=app, query=q(page=page, limit=limit))
    return result
