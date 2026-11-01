"""register 模块：每个工具发出的请求（方法、路径、查询串、请求体）、先读后合并与前置校验。"""

from __future__ import annotations

import pytest

R = "/api/admin/apps/demo/register-ip"

# (工具, 参数, 方法, 路径, 查询串, 请求体)
CASES = [
    ("register_overview", {"app": "demo", "days": 14, "tz": "Asia/Shanghai"}, "GET", R, {"days": "14", "tz": "Asia/Shanghai"}, None),
    ("register_policy", {"app": "demo"}, "GET", f"{R}/policy", {}, None),
    ("register_inspect_ip", {"app": "demo", "ip": "1.2.3.4"}, "GET", f"{R}/inspect", {"ip": "1.2.3.4"}, None),
    ("register_inspect_device", {"app": "demo", "device": "a1b2c3d4e5f60718"}, "GET", f"{R}/devices/inspect", {"device": "a1b2c3d4e5f60718"}, None),
    ("register_penalties", {"app": "demo", "limit": 50}, "GET", f"{R}/penalties", {"limit": "50"}, None),
    ("register_simulate", {"app": "demo", "ip": "1.2.3.4", "device_id": "dev-abcdef12", "method": "sms", "counts": {"ip-day": 9},
                           "location": {"country": "中国"}, "penalty_minutes": 5},
     "POST", f"{R}/simulate", {}, {"ip": "1.2.3.4", "deviceId": "dev-abcdef12", "method": "sms", "captchaPassed": False,
                                   "counts": {"ip-day": 9}, "location": {"country": "中国", "resolved": True}, "penaltySeconds": 300}),
    ("register_release", {"app": "demo", "ip": "1.2.3.4", "device": "a1b2c3d4e5f60718", "scope": "subnet", "ledger": True, "note": "申诉"},
     "POST", f"{R}/release", {}, {"ip": "1.2.3.4", "device": "a1b2c3d4e5f60718", "scope": "subnet", "counters": True, "penalty": True,
                                  "resetLevel": False, "ledger": True, "note": "申诉"}),
    ("register_events", {"app": "demo", "kind": "rejected", "reason": "rule", "device_id": "d1", "ip": "1.2.3.0/24", "from_time": "2026-11-01T00:00:00Z"},
     "GET", f"{R}/events", {"kind": "rejected", "reason": "rule", "deviceId": "d1", "ip": "1.2.3.0/24", "from": "2026-11-01T00:00:00Z", "page": "1", "limit": "20"}, None),
    ("register_registrations", {"app": "demo", "device_id": "d1", "released": False},
     "GET", f"{R}/registrations", {"deviceId": "d1", "released": "false", "page": "1", "limit": "20"}, None),
    ("register_lists", {"app": "demo", "kind": "deny", "keyword": "1.2.3.4"}, "GET", f"{R}/lists", {"kind": "deny", "keyword": "1.2.3.4", "page": "1", "limit": "20"}, None),
    ("register_lists", {"app": "demo", "target": "device", "include_expired": True}, "GET", f"{R}/device-lists", {"expired": "true", "page": "1", "limit": "20"}, None),
    ("register_add_lists", {"app": "demo", "values": ["1.2.3.0/24"], "kind": "deny", "note": "工作室"},
     "POST", f"{R}/lists", {}, {"values": ["1.2.3.0/24"], "kind": "deny", "note": "工作室"}),
    ("register_add_lists", {"app": "demo", "target": "device", "values": ["a1b2c3d4e5f60718"], "kind": "elevated", "factor": 3, "expires_at": "2027-01-01T00:00:00Z"},
     "POST", f"{R}/device-lists", {}, {"values": ["a1b2c3d4e5f60718"], "kind": "elevated", "factor": 3.0, "note": "", "expiresAt": "2027-01-01T00:00:00Z"}),
    ("register_update_list", {"app": "demo", "entry_id": 9, "kind": "allow", "note": "公司"},
     "PUT", f"{R}/lists/9", {}, {"kind": "allow", "factor": 0, "note": "公司", "expiresAt": None}),
    ("register_update_list", {"app": "demo", "target": "device", "entry_id": 3, "kind": "deny"},
     "PUT", f"{R}/device-lists/3", {}, {"kind": "deny", "factor": 0, "note": "", "expiresAt": None}),
    ("register_delete_lists", {"app": "demo", "ids": [1, 2]}, "POST", f"{R}/lists/delete", {}, {"ids": [1, 2]}),
    ("register_delete_lists", {"app": "demo", "target": "device", "ids": [4]}, "POST", f"{R}/device-lists/delete", {}, {"ids": [4]}),
]


@pytest.mark.parametrize(("tool", "args", "method", "path", "query", "payload"), CASES, ids=[f"{c[0]}-{i}" for i, c in enumerate(CASES)])
async def test_request_shape(backend, call, tool, args, method, path, query, payload):
    await call(tool, args)
    req = backend.find(method, path)
    assert req.query == query
    assert req.json == payload


POLICY = {
    "enabled": False,
    "mode": "enforce",
    "rules": [
        {"id": "ip-day", "name": "同一地址每天", "enabled": True, "scope": "ip", "windowSeconds": 86400, "limit": 10, "action": "reject"},
        {"id": "device-day", "name": "同一设备每天", "enabled": True, "scope": "device", "windowSeconds": 86400, "limit": 3, "action": "reject"},
    ],
    "penalty": {"enabled": True, "strikes": 5, "scope": "ip", "device": True},
    "device": {"missing": "allow", "invalid": "challenge", "minLength": 8},
}


async def test_update_policy_merges_nested_fields(backend, call, call_error):
    backend.reply("GET", f"{R}/policy", {"policy": POLICY, "configured": True})
    await call("register_update_policy", {"app": "demo", "enabled": True, "device": {"missing": "reject"}, "penalty": {"strikes": 3}})
    saved = backend.find("PUT", f"{R}/policy").json
    assert saved["enabled"] is True
    assert saved["device"] == {"missing": "reject", "invalid": "challenge", "minLength": 8}
    assert saved["penalty"] == {"enabled": True, "strikes": 3, "scope": "ip", "device": True}
    assert saved["rules"] == POLICY["rules"]
    assert "没有要修改" in await call_error("register_update_policy", {"app": "demo"})


async def test_upsert_rule_modifies_inserts_and_validates(backend, call, call_error):
    backend.reply("GET", f"{R}/policy", {"policy": POLICY, "configured": True})
    await call("register_upsert_rule", {"app": "demo", "rule_id": "device-day", "limit": 1, "methods": ["sms"]})
    rules = backend.find("PUT", f"{R}/policy").json["rules"]
    assert rules[1]["limit"] == 1 and rules[1]["methods"] == ["sms"] and rules[0] == POLICY["rules"][0]

    await call("register_upsert_rule", {"app": "demo", "rule_id": "farm", "scope": "ip_devices", "limit": 8, "action": "challenge", "position": 0})
    rules = backend.last().json["rules"]
    assert [rule["id"] for rule in rules] == ["farm", "ip-day", "device-day"]
    assert rules[0] == {"id": "farm", "enabled": True, "windowSeconds": 3600, "action": "challenge", "name": "", "scope": "ip_devices", "limit": 8}

    assert "必填" in await call_error("register_upsert_rule", {"app": "demo", "rule_id": "new", "limit": 3})


async def test_remove_rule(backend, call, call_error):
    backend.reply("GET", f"{R}/policy", {"policy": POLICY, "configured": True})
    await call("register_remove_rule", {"app": "demo", "rule_id": "ip-day"})
    assert [rule["id"] for rule in backend.last().json["rules"]] == ["device-day"]
    assert "没有 ID" in await call_error("register_remove_rule", {"app": "demo", "rule_id": "nope"})


async def test_release_and_lists_validation(call_error):
    assert "至少给一个" in await call_error("register_release", {"app": "demo"})
    assert "factor" in await call_error("register_add_lists", {"app": "demo", "values": ["1.2.3.4"], "kind": "elevated", "factor": 1})
