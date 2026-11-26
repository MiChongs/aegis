"""工单模块。"""

from __future__ import annotations

import pytest
from fastmcp.exceptions import ToolError


async def test_list_resolves_app_and_flags(backend, call):
    await call("tickets_list", {"app": "demo", "status": "open", "mine": True, "tags": ["a", "b"]})
    req = backend.find("GET", "/api/admin/tickets")
    assert req.query == {"appid": "42", "status": "open", "tags": "a,b", "mine": "true", "page": "1", "limit": "20"}


async def test_get_create_update(backend, call):
    await call("tickets_get", {"ticket_id": 7})
    assert backend.last().path == "/api/admin/tickets/7"
    await call("tickets_create", {"app": "12", "kind": "feedback", "title": "t", "content": "c", "requester_user_id": 5})
    assert backend.last().json == {"appid": 12, "kind": "feedback", "title": "t", "content": "c", "requesterUserId": 5, "requesterType": "user", "source": "api"}
    # 工单只对管理员开放：不能以用户为提单人建 kind=ticket
    with pytest.raises(ToolError, match="工单只对管理员开放"):
        await call("tickets_create", {"app": "12", "title": "t", "content": "c", "requester_user_id": 5})
    await call("tickets_update", {"ticket_id": 7, "priority": "high", "locked": True})
    assert (backend.last().method, backend.last().json) == ("PATCH", {"priority": "high", "locked": True})


async def test_reply_status_assign(backend, call, call_error):
    await call("tickets_reply", {"ticket_id": 7, "content": "已处理", "next_status": "resolved"})
    assert (backend.last().path, backend.last().json) == ("/api/admin/tickets/7/replies", {"content": "已处理", "internal": False, "nextStatus": "resolved"})
    await call("tickets_set_status", {"ticket_id": 7, "status": "closed"})
    assert backend.last().json == {"status": "closed", "reason": "", "solution": ""}
    await call("tickets_assign", {"ticket_id": 7, "group_id": 3, "auto_pick": True})
    assert backend.last().json == {"groupId": 3, "autoPick": True, "reason": ""}
    await call_error("tickets_assign", {"ticket_id": 7})


async def test_watch_bulk_delete(backend, call):
    await call("tickets_watch", {"ticket_id": 7, "watch": False})
    assert (backend.last().path, backend.last().query) == ("/api/admin/tickets/7/watch", {"watch": "false"})
    await call("tickets_watch", {"ticket_id": 7, "watcher_admin_ids": [1, 2]})
    assert (backend.last().method, backend.last().path, backend.last().json) == ("PUT", "/api/admin/tickets/7/watchers", {"adminIds": [1, 2]})
    await call("tickets_bulk", {"ticket_ids": [1, 2], "action": "close", "reason": "重复"})
    assert backend.last().json == {"ids": [1, 2], "action": "close", "reason": "重复"}
    await call("tickets_delete", {"ticket_id": 9})
    assert (backend.last().method, backend.last().path) == ("DELETE", "/api/admin/tickets/9")


async def test_delete_message(backend, call):
    await call("tickets_delete_message", {"ticket_id": 395, "message_id": 811, "reason": "功能已撤回"})
    req = backend.last()
    assert (req.method, req.path, req.json) == ("DELETE", "/api/admin/tickets/395/messages/811", {"reason": "功能已撤回"})
    await call("tickets_delete_message", {"ticket_id": 7, "message_id": 8})
    assert backend.last().path == "/api/admin/tickets/7/messages/8"


async def test_stats_and_config(backend, call):
    data = await call("tickets_stats", {"trend_days": 7, "agents": True})
    assert set(data) == {"stats", "workbench", "trend", "agents"}
    assert backend.find("GET", "/api/admin/tickets/trend").query == {"days": "7"}
    await call("tickets_config", {"section": "categories", "kind": "feedback", "enabled_only": True})
    assert backend.last().query == {"kind": "feedback", "enabled": "true"}
    await call("tickets_config", {"section": "sla_policies"})
    assert backend.last().path == "/api/admin/tickets/sla-policies"


async def test_save_configs(backend, call):
    await call("tickets_save_category", {"name": "退款", "key": "refund"})
    assert (backend.last().method, backend.last().path) == ("POST", "/api/admin/tickets/categories")
    await call("tickets_save_category", {"name": "退款", "key": "refund", "category_id": 4, "default_priority": "high"})
    assert (backend.last().method, backend.last().path, backend.last().json["defaultPriority"]) == ("PUT", "/api/admin/tickets/categories/4", "high")
    backend.reply("POST", "/api/admin/tickets/groups", {"id": 11})
    data = await call("tickets_save_group", {"name": "一线", "key": "tier1", "members": [{"adminId": 2, "role": "member"}]})
    assert backend.last().path == "/api/admin/tickets/groups/11/members" and data["group"] == {"id": 11}
    await call("tickets_save_sla_policy", {"name": "默认", "first_response_minutes": {"urgent": 30}, "policy_id": 2})
    assert (backend.last().path, backend.last().json["firstResponseMinutes"]) == ("/api/admin/tickets/sla-policies/2", {"urgent": 30})
    await call("tickets_save_quick_reply", {"title": "t", "content": "c"})
    assert backend.last().path == "/api/admin/tickets/quick-replies"
    await call("tickets_delete_config", {"section": "sla_policy", "config_id": 2})
    assert (backend.last().method, backend.last().path) == ("DELETE", "/api/admin/tickets/sla-policies/2")
