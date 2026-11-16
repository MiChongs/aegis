"""管理员与权限模块。"""

from __future__ import annotations

S = "/api/admin/system"


async def test_admins_and_access(backend, call):
    await call("admins_list", {"online": True})
    assert backend.last().path == f"{S}/admins/online"
    await call("admins_create", {"account": "ops01", "password": "Passw0rd!x", "assignments": [{"roleKey": "app_operator", "appid": 3}]})
    assert backend.last().json == {"account": "ops01", "password": "Passw0rd!x", "displayName": "", "email": "", "tier": "admin", "isSuperAdmin": False,
                                   "assignments": [{"roleKey": "app_operator", "appid": 3}]}
    await call("admins_set_access", {"admin_id": 4, "assignments": []})
    assert (backend.last().method, backend.last().path, backend.last().json) == ("PUT", f"{S}/admins/4/access", {"assignments": [], "tier": "admin", "isSuperAdmin": False})
    await call("admins_set_status", {"admin_id": 4, "status": "disabled"})
    assert backend.last().json == {"status": "disabled"}


async def test_sessions(backend, call, call_error):
    await call("admins_sessions")
    assert (backend.last().path, backend.last().query) == (f"{S}/sessions", {"page": "1", "limit": "50"})
    await call("admins_sessions", {"admin_id": 4})
    assert backend.last().path == f"{S}/admins/4/sessions"
    await call("admins_revoke_sessions", {"session_id": "abc"})
    assert backend.last().path == f"{S}/sessions/abc/revoke"
    await call("admins_revoke_sessions", {"admin_id": 4})
    assert backend.last().path == f"{S}/admins/4/force-logout"
    await call_error("admins_revoke_sessions", {})


async def test_roles(backend, call):
    await call("admins_roles", {"view": "matrix"})
    assert backend.last().path == f"{S}/roles/matrix"
    await call("admins_roles", {"impact_of": "ops"})
    assert backend.last().path == f"{S}/roles/ops/impact"
    await call("admins_save_role", {"role_key": "ops", "name": "运营", "permissions": ["ticket:*"], "scope": "app"})
    assert backend.last().json == {"roleKey": "ops", "name": "运营", "permissions": ["ticket:*"], "description": "", "scope": "app"}
    await call("admins_save_role", {"role_key": "ops", "name": "运营", "permissions": [], "create_new": False})
    assert (backend.last().method, backend.last().path) == ("PUT", f"{S}/roles/ops")
    await call("admins_delete_role", {"role_key": "ops"})
    assert backend.last().method == "DELETE"


async def test_authz(backend, call, call_error):
    await call("admins_authz", {"view": "subject", "subject": "admin:4"})
    assert backend.last().query == {"subject": "admin:4"}
    await call_error("admins_authz", {"view": "subject"})
    await call("admins_explain", {"admin_id": 4, "permission": "app:user:write", "app": "demo"})
    assert backend.find("POST", f"{S}/authz/explain").json == {"adminId": 4, "permission": "app:user:write", "appid": 42}
    await call("admins_set_policy", {"target": "role", "role_key": "app_viewer", "deny": ["app:user:read"]})
    assert backend.last().json == {"roleKey": "app_viewer", "allow": [], "deny": ["app:user:read"], "note": ""}
    await call("admins_set_policy", {"target": "admin", "admin_id": 4, "grants": [{"permission": "ticket:read", "effect": "allow"}]})
    assert (backend.last().method, backend.last().path) == ("PUT", f"{S}/authz/admins/4/grants")
    await call_error("admins_set_policy", {"target": "admin", "admin_id": 4})
    await call("admins_reload_policies")
    assert backend.last().path == f"{S}/authz/reload"


async def test_temp_and_delegation(backend, call):
    await call("admins_temp_permissions", {"admin_id": 4})
    assert backend.last().query == {"adminId": "4"}
    await call("admins_grant_temp_permission", {"admin_id": 4, "permission": "app:write", "expires_at": "2026-11-01T00:00:00Z", "reason": "排障", "app": "9"})
    assert backend.last().json == {"adminId": 4, "permission": "app:write", "expiresAt": "2026-11-01T00:00:00Z", "reason": "排障", "appId": 9}
    await call("admins_revoke_temp_permission", {"permission_id": 3})
    assert backend.last().path == f"{S}/temp-permissions/3/revoke"
    await call("admins_delegations", {"admin_id": 4, "role": "delegate"})
    assert backend.last().query == {"adminId": "4", "role": "delegate"}
    await call("admins_delegate", {"delegator_id": 4, "delegate_id": 5, "reason": "休假"})
    assert backend.last().json == {"delegatorId": 4, "delegateId": 5, "reason": "休假"}
    await call("admins_revoke_delegation", {"delegation_id": 2})
    assert backend.last().path == f"{S}/delegations/2/revoke"


async def test_online(backend, call):
    await call("admins_online")
    assert backend.last().path == f"{S}/online/stats"
    data = await call("admins_online", {"app": "demo", "users": True})
    assert set(data) == {"stats", "users"} and backend.last().path == f"{S}/online/apps/demo/users"
