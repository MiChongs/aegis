"""risk 模块：风控平台（risk-admin 转发）与防火墙工具的请求形状与前置校验。"""

from __future__ import annotations

import pytest

R = "/api/admin/system/risk"
F = "/api/admin/system/firewall"
PAGE = {"page": "1", "pageSize": "20"}
LIMIT = {"page": "1", "limit": "50"}
T0 = "2026-10-01T00:00:00Z"

CASES = [
    # 元数据、状态、审计
    ("risk_metadata", {}, "GET", f"{R}/meta", {}, None),
    ("risk_validate_expression", {"expr": "F.ip_fail_1h > 5"}, "POST", f"{R}/expr/validate", {}, {"expr": "F.ip_fail_1h > 5"}),
    ("risk_status", {}, "GET", f"{R}/status", {}, None),
    ("risk_audit", {"limit": 20}, "GET", f"{R}/audit", {"limit": "20"}, None),
    # 场景
    ("risk_scenes", {}, "GET", f"{R}/scenes", {}, None),
    ("risk_scenes", {"scene": "login"}, "GET", f"{R}/scenes/login", {}, None),
    ("risk_scenes", {"scene": "login", "bundle": True}, "GET", f"{R}/scenes/login/bundle", {}, None),
    ("risk_delete_scene", {"scene": "my_scene"}, "DELETE", f"{R}/scenes/my_scene", {}, None),
    ("risk_validate_scene", {"scene": "login"}, "POST", f"{R}/scenes/login/validate", {}, None),
    ("risk_simulate", {"scene": "register", "app": "demo", "ip": "1.1.1.1", "device": {"id": "d1", "emulator": True},
                       "geo": {"country": "CN", "proxy": True}, "captcha_passed": True, "request_fields": {"amount": 9.9},
                       "features": {"ip_fail_1h": 8}, "lists": [{"listType": "ip", "kind": "watch", "value": "1.1.1.1"}]},
     "POST", f"{R}/scenes/register/simulate", {},
     {"request": {"appId": 42, "ip": "1.1.1.1", "device": {"id": "d1", "emulator": True}, "geo": {"country": "CN", "proxy": True},
                  "captchaPassed": True, "amount": 9.9},
      "draft": True, "mode": "fixed", "features": {"ip_fail_1h": 8.0}, "lists": [{"listType": "ip", "kind": "watch", "value": "1.1.1.1"}]}),
    ("risk_simulate", {"scene": "login", "user_id": 7, "draft": False, "mode": "live"},
     "POST", f"{R}/scenes/login/simulate", {}, {"request": {"userId": 7}, "draft": False, "mode": "live"}),
    # 发布与流量
    ("risk_releases", {"scene": "login"}, "GET", f"{R}/scenes/login/releases", {}, None),
    ("risk_releases", {"release_id": 12}, "GET", f"{R}/releases/12", {}, None),
    ("risk_publish", {"scene": "login", "note": "加机房规则"}, "POST", f"{R}/scenes/login/publish", {}, {"note": "加机房规则", "mode": "full"}),
    ("risk_publish", {"scene": "login", "mode": "canary", "canary_percent": 10},
     "POST", f"{R}/scenes/login/publish", {}, {"note": "", "mode": "canary", "canaryPercent": 10}),
    ("risk_promote", {"scene": "login"}, "POST", f"{R}/scenes/login/promote", {}, None),
    ("risk_rollback", {"scene": "login", "release_id": 11}, "POST", f"{R}/scenes/login/rollback", {}, {"releaseId": 11}),
    ("risk_withdraw", {"scene": "login"}, "POST", f"{R}/scenes/login/withdraw", {}, None),
    ("risk_set_traffic", {"scene": "login", "canary_percent": 50}, "POST", f"{R}/scenes/login/traffic", {}, {"canaryPercent": 50, "shadow": False, "drop": False}),
    ("risk_set_traffic", {"scene": "login", "drop": True}, "POST", f"{R}/scenes/login/traffic", {}, {"canaryPercent": 0, "shadow": False, "drop": True}),
    # 特征
    ("risk_features", {}, "GET", f"{R}/features", {}, None),
    ("risk_features", {"feature": "ip_fail_1h", "sql": True}, "GET", f"{R}/features/ip_fail_1h/sql", {}, None),
    ("risk_delete_feature", {"feature": "ip_fail_1h"}, "DELETE", f"{R}/features/ip_fail_1h", {}, None),
    ("risk_sync_features", {}, "POST", f"{R}/features/sync", {}, None),
    # 规则与模板
    ("risk_rules", {"scene": "login"}, "GET", f"{R}/rules", {"scene": "login"}, None),
    ("risk_save_rule", {"scene": "*", "key": "dc_ip", "name": "机房 IP", "expr": "geo.datacenter", "score": 30, "mode": "observe", "reason": "机房 {{F.x}}"},
     "POST", f"{R}/rules", {}, {"scene": "*", "key": "dc_ip", "name": "机房 IP", "expr": "geo.datacenter", "score": 30, "mode": "observe", "reason": "机房 {{F.x}}", "enabled": True}),
    ("risk_delete_rule", {"rule_id": 8}, "DELETE", f"{R}/rules/8", {}, None),
    ("risk_templates", {}, "GET", f"{R}/templates", {}, None),
    ("risk_apply_template", {"template": "basic", "scene": "login"}, "POST", f"{R}/templates/apply", {}, {"template": "basic", "scene": "login", "observe": True}),
    # 模型
    ("risk_models", {}, "GET", f"{R}/models", {}, None),
    ("risk_update_model", {"model": "fraud", "inputs": ["amount", "F.ip_fail_1h"], "transform": True, "description": "支付欺诈"},
     "PUT", f"{R}/models/fraud", {}, {"description": "支付欺诈", "inputs": ["amount", "F.ip_fail_1h"], "transform": True}),
    ("risk_delete_model", {"model": "fraud"}, "DELETE", f"{R}/models/fraud", {}, None),
    ("risk_predict_model", {"model": "fraud", "values": {"amount": 100}}, "POST", f"{R}/models/fraud/predict", {}, {"values": {"amount": 100.0}}),
    # 名单
    ("risk_lists", {"list_type": "ip", "kind": "block", "active": False, "app": "42", "value": "1.1.1.1"},
     "GET", f"{R}/lists", {"type": "ip", "kind": "block", "active": "false", "app": "42", "value": "1.1.1.1", **LIMIT}, None),
    ("risk_add_list", {"list_type": "cidr", "values": ["10.0.0.0/8"], "kind": "block", "reason": "内网", "duration_seconds": 3600, "app": "demo"},
     "POST", f"{R}/lists", {}, {"listType": "cidr", "kind": "block", "values": ["10.0.0.0/8"], "appId": 42, "reason": "内网", "durationSeconds": 3600}),
    ("risk_remove_lists", {"entry_ids": ["a1", "b2"]}, "POST", f"{R}/lists/remove", {}, {"ids": ["a1", "b2"]}),
    ("risk_check_lists", {"values": {"ip": "1.1.1.1"}, "scene": "login"}, "POST", f"{R}/lists/check", {}, {"scene": "login", "values": {"ip": "1.1.1.1"}}),
    ("risk_rebuild_lists", {}, "POST", f"{R}/lists/rebuild", {}, None),
    # 行为序列
    ("risk_cep_patterns", {}, "GET", f"{R}/cep/patterns", {}, None),
    ("risk_delete_cep_pattern", {"pattern": "burst"}, "DELETE", f"{R}/cep/patterns/burst", {}, None),
    # 大盘与留痕
    ("risk_dashboard", {"app": "demo", "start": T0}, "GET", f"{R}/dashboard", {"app": "42", "from": T0}, None),
    ("risk_decisions", {"user_id": 7, "action": "block", "rule": "dc_ip", "shadow": True, "start": T0, "end": T0},
     "GET", f"{R}/decisions", {"user": "7", "action": "block", "rule": "dc_ip", "shadow": "true", "from": T0, "to": T0, **LIMIT}, None),
    ("risk_decisions", {"decision_id": "dec-1"}, "GET", f"{R}/decisions/dec-1", {}, None),
    ("risk_events", {"event_type": "login.fail", "device_id": "d1"}, "GET", f"{R}/events", {"type": "login.fail", "device": "d1", "limit": "50"}, None),
    ("risk_alerts", {"pattern": "burst", "user_id": 7}, "GET", f"{R}/alerts", {"pattern": "burst", "user": "7", "limit": "100"}, None),
    # 案件与处置
    ("risk_cases", {}, "GET", f"{R}/cases", {"status": "pending", **LIMIT}, None),
    ("risk_cases", {"status": "", "scene": "payment", "user_id": 7}, "GET", f"{R}/cases", {"scene": "payment", "user": "7", **LIMIT}, None),
    ("risk_review_case", {"case_id": 5, "verdict": "approve", "allow": True}, "POST", f"{R}/cases/5/review", {}, {"verdict": "approve", "note": "", "allow": True}),
    ("risk_review_case", {"case_id": 5, "verdict": "reject", "note": "团伙", "dispose_targets": ["device", "ban_account"], "dispose_duration_seconds": 600},
     "POST", f"{R}/cases/5/review", {}, {"verdict": "reject", "note": "团伙", "allow": False, "dispose": {"kind": "block", "targets": ["device", "ban_account"], "durationSeconds": 600}}),
    ("risk_dispose", {"targets": ["ip", "firewall"], "ip": "6.6.6.6", "duration_seconds": 600},
     "POST", f"{R}/dispose", {}, {"ip": "6.6.6.6", "kind": "block", "targets": ["ip", "firewall"], "durationSeconds": 600, "reason": ""}),
    ("risk_dispose", {"targets": ["user"], "kind": "allow", "app": "demo", "user_id": 7, "reason": "误报"},
     "POST", f"{R}/dispose", {}, {"appId": 42, "userId": 7, "kind": "allow", "targets": ["user"], "durationSeconds": 0, "reason": "误报"}),
    # 关系图与画像
    ("risk_graph", {"app": "demo", "user_id": 7}, "GET", f"{R}/graph", {"type": "user", "app": "42", "user": "7", "depth": "2", "fanout": "40", "max": "300"}, None),
    ("risk_graph", {"entity_type": "device", "value": "d1", "depth": 3, "max_nodes": 100},
     "GET", f"{R}/graph", {"type": "device", "value": "d1", "depth": "3", "fanout": "40", "max": "100"}, None),
    ("risk_profile", {"app": "demo", "user_id": 7, "days": 7}, "GET", f"{R}/profile", {"app": "42", "user": "7", "days": "7"}, None),
    # 防火墙
    ("risk_ip_bans", {"status": "active", "source": "risk"}, "GET", f"{F}/bans", {"status": "active", "source": "risk", **PAGE}, None),
    ("risk_ip_bans", {"modes": True}, "GET", f"{F}/bans/modes", {}, None),
    ("risk_ban_ip", {"ip": "6.6.6.6", "duration": 3600, "mode": "tarpit", "delay_ms": 5000},
     "POST", f"{F}/bans", {}, {"ip": "6.6.6.6", "reason": "", "duration": 3600, "mode": "tarpit", "delayMs": 5000}),
    ("risk_unban_ip", {"ban_id": 9}, "DELETE", f"{F}/bans/9", {}, None),
    ("risk_geo_bans", {}, "GET", f"{F}/geo-bans", {}, None),
    ("risk_save_geo_ban", {"scope_type": "country", "scope_value": "KP"},
     "POST", f"{F}/geo-bans", {}, {"scopeType": "country", "scopeValue": "KP", "mode": "", "reason": "", "enabled": True, "expiresAt": ""}),
    ("risk_toggle_geo_ban", {"geo_ban_id": 2, "enabled": False}, "PATCH", f"{F}/geo-bans/2", {}, {"enabled": False}),
    ("risk_delete_geo_ban", {"geo_ban_id": 2}, "DELETE", f"{F}/geo-bans/2", {}, None),
    ("risk_geo_fences", {}, "GET", f"{F}/geo-fences", {}, None),
    ("risk_preview_geo_fence", {"name": "园区", "mode": "deny", "center_lat": 31.2, "center_lng": 121.5, "radius_m": 500, "window_days": 3},
     "POST", f"{F}/geo-fences/preview", {}, {"name": "园区", "mode": "deny", "centerLat": 31.2, "centerLng": 121.5, "radiusM": 500, "banMode": "", "reason": "", "enabled": True, "expiresAt": "", "windowDays": 3}),
    ("risk_save_geo_fence", {"name": "区", "mode": "allow", "fence": {"type": "Polygon", "coordinates": []}, "app": "demo"},
     "POST", f"{F}/geo-fences", {}, {"appId": 42, "name": "区", "mode": "allow", "fence": {"type": "Polygon", "coordinates": []}, "banMode": "", "reason": "", "enabled": True, "expiresAt": ""}),
    ("risk_save_geo_fence", {"fence_id": 6, "name": "区", "mode": "review", "fence": {"type": "Polygon", "coordinates": []}, "enabled": False},
     "PUT", f"{F}/geo-fences/6", {}, {"name": "区", "mode": "review", "fence": {"type": "Polygon", "coordinates": []}, "banMode": "", "reason": "", "enabled": False, "expiresAt": ""}),
    ("risk_toggle_geo_fence", {"fence_id": 6, "enabled": True}, "PATCH", f"{F}/geo-fences/6", {}, {"enabled": True}),
    ("risk_delete_geo_fence", {"fence_id": 6}, "DELETE", f"{F}/geo-fences/6", {}, None),
    ("risk_firewall_logs", {"ip": "6.6.6.6", "severity": "high", "sort_by": "ip", "waf_rule_id": 3},
     "GET", f"{F}/logs", {"ip": "6.6.6.6", "severity": "high", "sortBy": "ip", "wafRuleId": "3", **PAGE}, None),
    ("risk_firewall_logs", {"log_id": 77}, "GET", f"{F}/logs/77", {}, None),
    ("risk_firewall_stats", {"granularity": "day"}, "GET", f"{F}/stats", {"granularity": "day"}, None),
    ("risk_cleanup_firewall_logs", {"before": "2026-01-01T00:00:00Z"}, "DELETE", f"{F}/logs", {}, {"before": "2026-01-01T00:00:00Z"}),
]


@pytest.mark.parametrize(("tool", "args", "method", "path", "query", "payload"), CASES, ids=[f"{c[0]}-{i}" for i, c in enumerate(CASES)])
async def test_request_shape(backend, call, tool, args, method, path, query, payload):
    await call(tool, args)
    req = backend.last()
    assert (req.method, req.path) == (method, path)
    assert req.query == query
    assert req.json == payload


SCENE = {
    "key": "login", "name": "登录", "builtin": True, "timeoutMs": 50, "failMode": "open", "policy": "package aegis.risk.scene",
    "enforcement": {"targets": ["device"], "durationSeconds": 3600, "autoBanAllowed": False},
    "state": {"primaryId": 3}, "ruleCount": 4, "updatedAt": "2026-11-01T00:00:00Z", "updatedBy": "root",
}


async def test_save_scene_merges_into_current(backend, call):
    backend.reply("GET", f"{R}/scenes/login", SCENE)
    await call("risk_save_scene", {"scene": "login", "timeout_ms": 80, "auto_ban_allowed": True})
    req = backend.find("PUT", f"{R}/scenes/login")
    assert req.json == {
        "key": "login", "name": "登录", "timeoutMs": 80, "failMode": "open", "policy": "package aegis.risk.scene",
        "enforcement": {"targets": ["device"], "durationSeconds": 3600, "autoBanAllowed": True},
    }


async def test_save_scene_creates_when_missing(backend, call):
    backend.reply("GET", f"{R}/scenes/vip_buy", None, status=404, code=40400, message="不存在")
    await call("risk_save_scene", {"scene": "vip_buy", "name": "会员购买", "fail_mode": "closed", "bands": [{"min": 80, "max": 0, "action": "block", "level": "high"}]})
    req = backend.last()
    assert (req.method, req.path) == ("PUT", f"{R}/scenes/vip_buy")
    assert req.json == {"key": "vip_buy", "name": "会员购买", "failMode": "closed", "bands": [{"min": 80, "max": 0, "action": "block", "level": "high"}]}


async def test_save_scene_other_errors_propagate(backend, call_error):
    backend.reply("GET", f"{R}/scenes/login", None, status=503, code=50302, message="风控管理服务不可用")
    assert "50302" in await call_error("risk_save_scene", {"scene": "login", "enabled": False})
    assert all(r.method == "GET" for r in backend.requests)


async def test_save_feature_merges_or_creates(backend, call, call_error):
    backend.reply("GET", f"{R}/features", [
        {"key": "ip_fail_1h", "name": "IP 一小时失败", "kind": "velocity", "entity": "ip", "default": 0,
         "velocity": {"windowSeconds": 3600, "agg": "count"}, "updatedAt": "x", "updatedBy": "root"},
    ])
    await call("risk_save_feature", {"feature": "ip_fail_1h", "cross_app": True, "velocity": {"windowSeconds": 600, "agg": "count"}})
    assert backend.last().json == {
        "key": "ip_fail_1h", "name": "IP 一小时失败", "kind": "velocity", "entity": "ip", "default": 0,
        "velocity": {"windowSeconds": 600, "agg": "count"}, "global": True,
    }
    await call("risk_save_feature", {"feature": "risky", "kind": "derived", "expr": "F.ip_fail_1h * 2"})
    req = backend.last()
    assert (req.method, req.path) == ("PUT", f"{R}/features/risky")
    assert req.json == {"key": "risky", "kind": "derived", "expr": "F.ip_fail_1h * 2"}
    assert "kind" in await call_error("risk_save_feature", {"feature": "nope", "expr": "1"})


async def test_features_single_lookup(backend, call, call_error):
    backend.reply("GET", f"{R}/features", [{"key": "a"}, {"key": "b", "kind": "graph"}])
    assert await call("risk_features", {"feature": "b"}) == {"key": "b", "kind": "graph"}
    assert "没有特征" in await call_error("risk_features", {"feature": "c"})


async def test_rules_lookup_and_update_merge(backend, call, call_error):
    rule = {"id": 8, "scene": "login", "key": "dc_ip", "name": "机房", "expr": "geo.datacenter", "score": 20, "mode": "observe",
            "priority": 0, "enabled": True, "templateKey": "basic", "updatedAt": "x", "updatedBy": "root"}
    backend.reply("GET", f"{R}/rules", [rule])
    assert (await call("risk_rules", {"rule_id": 8}))["key"] == "dc_ip"
    await call("risk_save_rule", {"rule_id": 8, "mode": "enforce", "decision": ""})
    req = backend.last()
    assert (req.method, req.path) == ("PUT", f"{R}/rules/8")
    assert req.json == {"id": 8, "scene": "login", "key": "dc_ip", "name": "机房", "expr": "geo.datacenter", "score": 20,
                        "mode": "enforce", "priority": 0, "enabled": True, "templateKey": "basic", "decision": ""}
    assert "没有规则" in await call_error("risk_save_rule", {"rule_id": 9, "score": 1})
    assert "没有规则" in await call_error("risk_rules", {"rule_id": 9})


async def test_save_cep_pattern_merges(backend, call):
    backend.reply("GET", f"{R}/cep/patterns", [{"key": "burst", "name": "撞库", "enabled": True, "keyBy": "ip", "steps": [{"name": "a", "events": ["login.fail"], "times": 5}],
                                              "withinSeconds": 60, "severity": "high", "actions": [], "updatedAt": "x"}])
    await call("risk_save_cep_pattern", {"pattern": "burst", "enabled": False, "actions": [{"kind": "list_add", "target": "ip", "listKind": "block", "durationSeconds": 3600}]})
    req = backend.last()
    assert (req.method, req.path) == ("PUT", f"{R}/cep/patterns/burst")
    assert req.json == {"key": "burst", "name": "撞库", "enabled": False, "keyBy": "ip", "steps": [{"name": "a", "events": ["login.fail"], "times": 5}],
                        "withinSeconds": 60, "severity": "high", "actions": [{"kind": "list_add", "target": "ip", "listKind": "block", "durationSeconds": 3600}]}


async def test_update_model_keeps_description(backend, call):
    backend.reply("GET", f"{R}/models", [{"key": "fraud", "name": "欺诈", "description": "旧说明"}])
    await call("risk_update_model", {"model": "fraud", "name": "支付欺诈"})
    req = backend.last()
    assert (req.method, req.path) == ("PUT", f"{R}/models/fraud")
    assert req.json == {"name": "支付欺诈", "description": "旧说明"}


async def test_app_key_resolves_through_whoami(backend, call, call_error):
    await call("risk_cases", {"app": "demo"})
    assert backend.requests[0].path == "/api/admin/mcp/whoami"
    assert backend.last().query["app"] == "42"
    text = await call_error("risk_cases", {"app": "nope"})
    assert "nope" in text


async def test_precondition_errors_send_nothing(backend, call_error):
    assert "scene" in await call_error("risk_scenes", {"bundle": True})
    assert "release_id" in await call_error("risk_releases", {})
    assert "canary_percent" in await call_error("risk_publish", {"scene": "login", "mode": "canary"})
    assert "之一" in await call_error("risk_set_traffic", {"scene": "login"})
    assert "feature" in await call_error("risk_features", {"sql": True})
    assert "scene / key / expr" in await call_error("risk_save_rule", {"name": "x"})
    assert "没有要修改" in await call_error("risk_save_scene", {"scene": "login"})
    assert "没有要修改" in await call_error("risk_save_rule", {"rule_id": 1})
    assert "没有要修改" in await call_error("risk_update_model", {"model": "m"})
    assert "没有要修改" in await call_error("risk_save_cep_pattern", {"pattern": "p"})
    assert "reject" in await call_error("risk_review_case", {"case_id": 1, "verdict": "approve", "dispose_targets": ["ip"]})
    assert "dispose_targets" in await call_error("risk_review_case", {"case_id": 1, "verdict": "reject", "dispose_kind": "block"})
    assert "user_id" in await call_error("risk_dispose", {"targets": ["ip"]})
    assert "value" in await call_error("risk_graph", {"entity_type": "ip"})
    assert "user_id" in await call_error("risk_graph", {})
    assert "fence" in await call_error("risk_save_geo_fence", {"name": "x", "mode": "deny", "center_lat": 1.0})
    assert "fence" in await call_error("risk_preview_geo_fence", {"name": "x", "mode": "deny"})
    await call_error("risk_add_list", {"list_type": "ip", "values": [], "kind": "block"})
    await call_error("risk_add_list", {"list_type": "nope", "values": ["1"], "kind": "block"})
    await call_error("risk_dispose", {"targets": ["nope"], "ip": "1.1.1.1"})
    assert not backend.requests


async def test_backend_error_becomes_tool_error(backend, call_error):
    backend.reply("POST", f"{R}/scenes/login/publish", None, status=403, code=40313, message="仅超级管理员可管理风控配置")
    text = await call_error("risk_publish", {"scene": "login"})
    assert "40313" in text and "超级管理员" in text
    backend.reply("POST", f"{F}/bans", None, code=40313, message="仅超级管理员可管理 IP 封禁")
    text = await call_error("risk_ban_ip", {"ip": "6.6.6.6"})
    assert "40313" in text and "超级管理员" in text
