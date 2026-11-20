"""用户号码模块。"""

from __future__ import annotations

BASE = "/api/admin/apps/demo/numbers"

CURRENT = {
    "enabled": True, "minLength": 5, "maxLength": 12, "defaultLength": 9, "selfPick": True, "cooldownDays": 7,
    "freezeDays": 90, "drawCandidates": 6, "drawHoldMinutes": 15, "drawWeights": [70, 20, 7, 2.5, 0.5],
    "luckSpread": 0.3, "luckSupreme": True, "patternTiers": {"mirror": 1, "aabb": -1},
    "updatedBy": "admin", "updatedAt": "2026-11-20T00:00:00Z",
}


async def test_reads(backend, call):
    for tool, path in (("numbers_catalog", "/catalog"), ("numbers_overview", "/overview"), ("numbers_settings", "/settings")):
        await call(tool, {"app": "demo"})
        assert (backend.last().method, backend.last().path) == ("GET", BASE + path)
    await call("numbers_inspect", {"app": "demo", "number": "88888"})
    assert (backend.last().path, backend.last().query) == (f"{BASE}/inspect", {"number": "88888"})
    await call("numbers_user", {"app": "demo", "user_id": 7})
    assert backend.last().path == f"{BASE}/users/7"


async def test_lists(backend, call):
    await call("numbers_history", {"app": "demo", "user_id": 7, "page": 2})
    assert (backend.last().path, backend.last().query) == (f"{BASE}/history", {"userId": "7", "page": "2", "limit": "20"})
    await call("numbers_ledger", {"app": "demo", "kind": "ticket"})
    assert (backend.last().path, backend.last().query) == (f"{BASE}/ledger", {"kind": "ticket", "page": "1", "limit": "20"})
    await call("numbers_draws", {"app": "demo"})
    assert (backend.last().path, backend.last().query) == (f"{BASE}/draws", {"page": "1", "limit": "20"})
    await call("numbers_holds", {"app": "demo", "kind": "reserved", "limit": 50})
    assert (backend.last().path, backend.last().query) == (f"{BASE}/holds", {"kind": "reserved", "page": "1", "limit": "50"})


async def test_inspect_rejects_bad_number(call_error):
    await call_error("numbers_inspect", {"app": "demo", "number": "01234"})
    await call_error("numbers_inspect", {"app": "demo", "number": "1234"})
    await call_error("numbers_inspect", {"app": "demo", "number": "1234567890123"})


async def test_save_settings_merges_current(backend, call, call_error):
    backend.reply("GET", f"{BASE}/settings", CURRENT)
    await call("numbers_save_settings", {"app": "demo", "cooldown_days": 0, "self_pick": False,
                                         "pattern_tiers": {"mirror": None, "len7": 4, "aabb": 1}})
    req = backend.find("PUT", f"{BASE}/settings")
    assert req.json["cooldownDays"] == 0 and req.json["selfPick"] is False
    assert req.json["freezeDays"] == 90 and req.json["drawWeights"] == [70, 20, 7, 2.5, 0.5]
    assert req.json["patternTiers"] == {"len7": 4, "aabb": 1}
    assert "updatedBy" not in req.json and "updatedAt" not in req.json

    await call("numbers_save_settings", {"app": "demo", "draw_weights": [60, 25, 10, 4, 1]})
    req = backend.last()
    assert req.json["drawWeights"] == [60, 25, 10, 4, 1] and req.json["patternTiers"] == {"mirror": 1, "aabb": -1}

    await call("numbers_save_settings", {"app": "demo", "pattern_tiers": {"mirror": None, "aabb": None}})
    assert "patternTiers" not in backend.last().json

    await call_error("numbers_save_settings", {"app": "demo"})
    await call_error("numbers_save_settings", {"app": "demo", "pattern_tiers": {"mirror": 7}})
    await call_error("numbers_save_settings", {"app": "demo", "draw_weights": [1, 2, 3]})


async def test_preview_odds(backend, call):
    backend.reply("GET", f"{BASE}/settings", CURRENT)
    await call("numbers_preview_odds", {"app": "demo", "luck": 1.5, "luck_spread": 0.5})
    req = backend.find("POST", f"{BASE}/settings/odds")
    assert req.json["luck"] == 1.5
    assert req.json["settings"]["luckSpread"] == 0.5 and req.json["settings"]["drawCandidates"] == 6


async def test_assign_rollback(backend, call, call_error):
    await call("numbers_assign", {"app": "demo", "user_id": 7, "number": "520520", "force": True, "note": "活动奖励"})
    req = backend.last()
    assert (req.method, req.path, req.json) == ("POST", f"{BASE}/users/7/assign", {"number": "520520", "force": True, "note": "活动奖励"})
    await call("numbers_assign", {"app": "demo", "user_id": 7, "number": "123456789"})
    assert backend.last().json == {"number": "123456789", "force": False}
    await call("numbers_rollback", {"app": "demo", "user_id": 7, "history_id": 3})
    assert (backend.last().path, backend.last().json) == (f"{BASE}/users/7/rollback", {"historyId": 3})
    await call_error("numbers_rollback", {"app": "demo", "user_id": 7, "history_id": 0})


async def test_assign_error_passthrough(backend, call_error):
    backend.reply("POST", f"{BASE}/users/7/assign", code=42982, status=409, message="号码已被使用")
    assert "42982" in await call_error("numbers_assign", {"app": "demo", "user_id": 7, "number": "88888"})


async def test_adjust_rights(backend, call, call_error):
    await call("numbers_adjust_rights", {"app": "demo", "user_id": 7, "kind": "ticket", "level": 3, "delta": 2, "note": "补发"})
    req = backend.last()
    assert (req.path, req.json) == (f"{BASE}/users/7/rights", {"kind": "ticket", "level": 3, "delta": 2, "note": "补发"})
    await call("numbers_adjust_rights", {"app": "demo", "user_id": 7, "kind": "draw", "delta": -1})
    assert backend.last().json == {"kind": "draw", "level": 0, "delta": -1}
    await call_error("numbers_adjust_rights", {"app": "demo", "user_id": 7, "kind": "change", "delta": 0})
    await call_error("numbers_adjust_rights", {"app": "demo", "user_id": 7, "kind": "ticket", "delta": 1})
    await call_error("numbers_adjust_rights", {"app": "demo", "user_id": 7, "kind": "change", "level": 2, "delta": 1})


async def test_reserve_release(backend, call, call_error):
    await call("numbers_reserve", {"app": "demo", "numbers": ["88888", "66666"], "note": "预留",
                                   "expires_at": "2027-01-01T00:00:00+08:00"})
    req = backend.last()
    assert (req.method, req.path, req.json) == ("POST", f"{BASE}/holds", {
        "numbers": ["88888", "66666"], "note": "预留", "expiresAt": "2027-01-01T00:00:00+08:00",
    })
    await call("numbers_reserve", {"app": "demo", "numbers": ["99999"]})
    assert backend.last().json == {"numbers": ["99999"]}
    await call_error("numbers_reserve", {"app": "demo", "numbers": []})
    await call_error("numbers_reserve", {"app": "demo", "numbers": ["0999"]})
    await call("numbers_release", {"app": "demo", "number": "88888"})
    assert (backend.last().method, backend.last().path) == ("DELETE", f"{BASE}/holds/88888")


async def test_annotations(mcp):
    tools = {tool.name: tool for tool in await mcp.list_tools()}
    for name in ("numbers_assign", "numbers_rollback", "numbers_adjust_rights", "numbers_release"):
        assert tools[name].annotations.destructive_hint is True, name
    for name in ("numbers_save_settings", "numbers_reserve", "numbers_preview_odds"):
        assert tools[name].annotations.read_only_hint is False and tools[name].annotations.idempotent_hint is True, name
    assert tools["numbers_overview"].annotations.read_only_hint is True


async def test_reward_types_include_number_rights(mcp):
    import json

    tools = {tool.name: tool for tool in await mcp.list_tools()}
    for name in ("cardkey_generate", "store_create_item", "invite_update_settings"):
        schema = json.dumps(tools[name].input_schema, ensure_ascii=False)
        for kind in ("number_change", "number_ticket", "number_draw"):
            assert kind in schema, f"{name} 缺少 {kind}"
