"""广告模块。"""

from __future__ import annotations

RA = "/api/admin/apps/demo/rewarded-ads"
AP = "/api/admin/apps/demo/ad-policy"

SCENE = {"key": "draw", "name": "抽奖", "placementId": "p1", "enabled": True,
         "rewards": [{"type": "lottery_draws", "amount": 1}], "dailyLimit": 5, "cooldownSeconds": 0,
         "membership": {"features": []}, "lifetime": {"policy": "convert"}}
CONFIG = {"enabled": True, "provider": "apex", "providerAppId": "a1", "hasSecurityKey": True, "securityKeyHint": "ab**",
          "verifyMode": "dual", "dailyLimit": 10, "scenes": [SCENE], "callbackUrl": "/cb", "catalog": [], "verifyModes": []}
SAVED = {"enabled": True, "provider": "apex", "providerAppId": "a1", "verifyMode": "dual", "dailyLimit": 10, "scenes": [SCENE]}


async def test_rewarded_config_and_save_merges(backend, call):
    backend.reply("GET", f"{RA}/config", CONFIG)
    assert (await call("ads_rewarded_config", {"app": "demo"}))["callbackUrl"] == "/cb"
    await call("ads_rewarded_save_config", {"app": "demo", "enabled": False, "verify_mode": "server", "security_key": "s3"})
    req = backend.find("PUT", f"{RA}/config")
    assert req.json == {**SAVED, "enabled": False, "verifyMode": "server", "securityKey": "s3"}
    await call("ads_rewarded_save_config", {"app": "demo", "clear_security_key": True, "scenes": []})
    assert backend.last().json == {**SAVED, "scenes": [], "clearSecurityKey": True}


async def test_rewarded_save_scene_update_and_add(backend, call, call_error):
    backend.reply("GET", f"{RA}/config", CONFIG)
    await call("ads_rewarded_save_scene", {"app": "demo", "key": "draw", "daily_limit": 3, "enabled": False})
    assert backend.last().json["scenes"] == [{**SCENE, "dailyLimit": 3, "enabled": False}]
    rewards = [{"type": "feature_minutes", "amount": 60, "feature": "online_music"}]
    await call("ads_rewarded_save_scene", {"app": "demo", "key": "music", "name": "听歌", "rewards": rewards})
    assert backend.last().json["scenes"] == [SCENE, {"key": "music", "enabled": True, "name": "听歌", "rewards": rewards}]
    assert "name" in await call_error("ads_rewarded_save_scene", {"app": "demo", "key": "new", "rewards": rewards})
    assert "rewards" in await call_error("ads_rewarded_save_scene", {"app": "demo", "key": "new", "name": "x"})
    await call_error("ads_rewarded_save_scene", {"app": "demo", "key": "Bad Key", "name": "x"})


async def test_rewarded_delete_scene(backend, call, call_error):
    backend.reply("GET", f"{RA}/config", CONFIG)
    await call("ads_rewarded_delete_scene", {"app": "demo", "key": "draw"})
    assert (backend.last().method, backend.last().json) == ("PUT", {**SAVED, "scenes": []})
    assert "找不到" in await call_error("ads_rewarded_delete_scene", {"app": "demo", "key": "nope"})


async def test_rewarded_save_placement(backend, call, call_error):
    placements = [{"id": "p1", "name": "激励视频", "hasSecurityKey": True, "keySource": "own", "scenes": ["draw"], "callbacks": 3},
                  {"id": "p2", "name": "", "hasSecurityKey": False, "keySource": "default", "scenes": [], "callbacks": 0}]
    backend.reply("GET", f"{RA}/config", {**CONFIG, "placements": placements})
    await call("ads_rewarded_save_placement", {"app": "demo", "id": "p2", "name": "激励视频2", "security_key": "k2"})
    assert backend.last().json == {**SAVED, "placements": [{"id": "p1", "name": "激励视频"},
                                                           {"id": "p2", "name": "激励视频2", "securityKey": "k2"}]}
    await call("ads_rewarded_save_placement", {"app": "demo", "id": "p3", "security_key": "k3"})
    assert backend.last().json["placements"][-1] == {"id": "p3", "name": "", "securityKey": "k3"}
    # 改 ID：Key 随 previousId 带过去，引用原 ID 的场景一并改
    await call("ads_rewarded_save_placement", {"app": "demo", "id": "p9", "previous_id": "p1"})
    saved = backend.last().json
    assert saved["placements"][0] == {"id": "p9", "name": "激励视频", "previousId": "p1"}
    assert saved["scenes"][0]["placementId"] == "p9"
    assert "找不到" in await call_error("ads_rewarded_save_placement", {"app": "demo", "id": "x", "previous_id": "nope"})


async def test_rewarded_delete_placement(backend, call, call_error):
    placements = [{"id": "p1", "name": "a", "scenes": ["draw"]}, {"id": "p2", "name": "b", "scenes": []}]
    backend.reply("GET", f"{RA}/config", {**CONFIG, "placements": placements})
    await call("ads_rewarded_delete_placement", {"app": "demo", "id": "p2"})
    assert backend.last().json["placements"] == [{"id": "p1", "name": "a"}]
    assert "仍被场景" in await call_error("ads_rewarded_delete_placement", {"app": "demo", "id": "p1"})
    assert "找不到" in await call_error("ads_rewarded_delete_placement", {"app": "demo", "id": "p9"})


async def test_rewarded_views_stats_grant(backend, call):
    await call("ads_rewarded_views", {"app": "demo", "user_id": 7, "status": "rejected", "start": "2026-10-01", "limit": 50})
    assert (backend.last().path, backend.last().query) == (f"{RA}/views", {"userId": "7", "status": "rejected", "start": "2026-10-01", "page": "1", "limit": "50"})
    await call("ads_rewarded_stats", {"app": "demo", "days": 30})
    assert (backend.last().path, backend.last().query) == (f"{RA}/stats", {"days": "30"})
    await call("ads_rewarded_grant_view", {"app": "demo", "view_id": 9})
    assert (backend.last().method, backend.last().path, backend.last().json) == ("POST", f"{RA}/views/9/grant", {"scene": ""})


async def test_policy_save_merges(backend, call, call_error):
    current = {"appid": 42, "enabled": False, "consentVersion": 2, "policyUrl": "https://x/p", "vipExempt": True,
               "basicTools": ["todo"], "splash": {"enabled": True, "placementId": "s1", "minIntervalSeconds": 60, "dailyLimit": 3},
               "configured": True}
    backend.reply("GET", f"{AP}/config", current)
    assert (await call("ads_policy", {"app": "demo"}))["consentVersion"] == 2
    await call("ads_save_policy", {"app": "demo", "enabled": True, "consent_version": 3, "splash_daily_limit": 5})
    assert backend.find("PUT", f"{AP}/config").json == {
        "enabled": True, "consentVersion": 3, "policyUrl": "https://x/p", "vipExempt": True, "basicTools": ["todo"],
        "splash": {"enabled": True, "placementId": "s1", "minIntervalSeconds": 60, "dailyLimit": 5},
    }
    assert "只能调高" in await call_error("ads_save_policy", {"app": "demo", "consent_version": 1})


async def test_consents_logs_splash_stats_user(backend, call):
    await call("ads_consents", {"app": "demo", "accepted": False, "outdated": True, "source": "web"})
    assert (backend.last().path, backend.last().query) == (f"{AP}/consents", {"accepted": "false", "outdated": "true", "source": "web", "page": "1", "limit": "20"})
    await call("ads_consents", {"app": "demo", "outdated": False})
    assert backend.last().query == {"page": "1", "limit": "20"}
    await call("ads_consent_logs", {"app": "demo", "user_id": 7, "keyword": "abc"})
    assert (backend.last().path, backend.last().query) == (f"{AP}/consent-logs", {"userId": "7", "keyword": "abc", "page": "1", "limit": "20"})
    await call("ads_splash_events", {"app": "demo", "status": "failed", "end": "2026-10-28"})
    assert (backend.last().path, backend.last().query) == (f"{AP}/splash-events", {"status": "failed", "end": "2026-10-28", "page": "1", "limit": "20"})
    await call("ads_policy_stats", {"app": "demo"})
    assert (backend.last().path, backend.last().query) == (f"{AP}/stats", {"days": "14"})
    await call("ads_user_policy", {"app": "demo", "user_id": 7})
    assert backend.last().path == "/api/admin/apps/demo/users/7/ad-policy"
