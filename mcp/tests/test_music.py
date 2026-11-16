"""音乐推荐模块。"""

from __future__ import annotations

B = "/api/admin/apps/demo/music"


async def test_overview_and_users(backend, call, call_error):
    await call("music_overview", {"app": "demo", "days": 30})
    assert (backend.last().path, backend.last().query) == (f"{B}/overview", {"days": "30"})
    await call_error("music_overview", {"app": "demo", "days": 3})

    await call("music_user", {"app": "demo"})
    assert (backend.last().path, backend.last().query) == (f"{B}/users", {"days": "30", "page": "1", "pageSize": "20"})
    await call("music_user", {"app": "demo", "limit": 150})
    assert backend.last().query["pageSize"] == "100"
    await call("music_user", {"app": "demo", "user_id": 7, "days": 90})
    assert (backend.last().path, backend.last().query) == (f"{B}/users/7", {"days": "90"})
    assert "7 / 30 / 90 / 365" in await call_error("music_user", {"app": "demo", "user_id": 7, "days": 14})

    await call("music_user", {"app": "demo", "user_id": 7, "view": "events", "event_type": "play_end", "before": "1763280000000"})
    assert (backend.last().path, backend.last().query) == (f"{B}/users/7/events", {"type": "play_end", "before": "1763280000000", "limit": "20"})
    await call("music_user", {"app": "demo", "view": "events", "keyword": "晴天", "from_time": "2026-11-16T00:00:00+08:00", "to_time": "2026-11-17"})
    req = backend.last()
    assert req.path == f"{B}/events"
    assert req.query == {"q": "晴天", "from": "1794758400000", "to": "1794844800000", "page": "1", "pageSize": "20"}
    assert "ISO 8601" in await call_error("music_user", {"app": "demo", "view": "events", "from_time": "昨天"})

    await call("music_user", {"app": "demo", "user_id": 7, "view": "mixes"})
    assert backend.last().path == f"{B}/users/7/mixes"
    await call("music_user", {"app": "demo", "user_id": 7, "view": "report", "period": "month", "offset": 1})
    assert (backend.last().path, backend.last().query) == (f"{B}/users/7/report", {"period": "month", "offset": "1"})
    assert "user_id" in await call_error("music_user", {"app": "demo", "view": "report"})


async def test_app_id_works_as_app(backend, call):
    await call("music_overview", {"app": "42"})
    assert backend.last().path == "/api/admin/apps/42/music/overview"


async def test_preview(backend, call, call_error):
    await call("music_preview", {"app": "demo", "user_id": 7})
    assert (backend.last().path, backend.last().query) == (f"{B}/users/7/recommendations", {"scene": "home", "limit": "20"})
    await call("music_preview", {"app": "demo", "user_id": 7, "scene": "similar", "source": "wy", "song_id": "186016", "artist": "忽略"})
    assert backend.last().query == {"scene": "similar", "limit": "20", "source": "wy", "songId": "186016"}
    await call("music_preview", {"app": "demo", "user_id": 7, "scene": "autoplay", "seeds": ["wy:1", "tx:2"], "limit": 10})
    assert backend.last().query == {"scene": "autoplay", "limit": "10", "seeds": "wy:1,tx:2"}
    await call("music_preview", {"app": "demo", "user_id": 7, "scene": "mix", "mix_id": "m1"})
    assert backend.last().query["mixId"] == "m1"
    await call("music_preview", {"app": "demo", "user_id": 7, "scene": "artist", "artist": "周杰伦"})
    assert backend.last().query["artist"] == "周杰伦"
    await call_error("music_preview", {"app": "demo", "user_id": 7, "scene": "similar", "source": "wy"})
    await call_error("music_preview", {"app": "demo", "user_id": 7, "scene": "autoplay"})
    assert "wy186016" in await call_error("music_preview", {"app": "demo", "user_id": 7, "scene": "autoplay", "seeds": ["wy186016"]})
    await call_error("music_preview", {"app": "demo", "user_id": 7, "scene": "mix"})
    await call_error("music_preview", {"app": "demo", "user_id": 7, "scene": "artist"})
    await call_error("music_preview", {"app": "demo", "user_id": 7, "scene": "nope"})


async def test_forget_user(backend, call):
    await call("music_forget_user", {"app": "demo", "user_id": 7})
    assert (backend.last().method, backend.last().path) == ("POST", f"{B}/users/7/forget")


async def test_tracks_artists_search(backend, call):
    await call("music_tracks", {"app": "demo", "keyword": "晴天", "sort": "skip"})
    assert (backend.last().path, backend.last().query) == (f"{B}/tracks", {"days": "7", "q": "晴天", "sort": "skip", "page": "1", "pageSize": "20"})
    await call("music_track", {"app": "demo", "work_key": "a1b2c3d4e5f60718"})
    assert (backend.last().path, backend.last().query) == (f"{B}/tracks/a1b2c3d4e5f60718", {"days": "30"})
    await call("music_artists", {"app": "demo", "keyword": "周"})
    assert (backend.last().path, backend.last().query) == (f"{B}/artists", {"days": "30", "q": "周", "page": "1", "pageSize": "20"})
    await call("music_artists", {"app": "demo", "name": "周杰伦", "days": 90})
    assert (backend.last().path, backend.last().query) == (f"{B}/artists/周杰伦", {"days": "90"})
    await call("music_search_stats", {"app": "demo", "limit": 10})
    assert (backend.last().path, backend.last().query) == (f"{B}/search", {"days": "7", "limit": "10"})


async def test_models(backend, call, call_error):
    await call("music_models", {"app": "demo"})
    assert (backend.last().path, backend.last().query) == (f"{B}/models", {"page": "1", "pageSize": "20"})
    await call("music_models", {"app": "demo", "run_id": 12})
    assert backend.last().path == f"{B}/models/12"
    await call("music_train", {"app": "demo", "note": "调参后"})
    assert (backend.last().method, backend.last().path, backend.last().json) == ("POST", f"{B}/models/train", {"note": "调参后"})
    await call("music_train", {"app": "demo"})
    assert backend.last().json == {}
    await call("music_publish", {"app": "demo", "run_id": 12})
    assert (backend.last().method, backend.last().path) == ("POST", f"{B}/models/12/publish")
    backend.reply("POST", f"{B}/models/11/publish", code=40900, status=409, message="只能发布训练成功且模型文件仍保留的版本")
    assert "40900" in await call_error("music_publish", {"app": "demo", "run_id": 11})


CONFIG = {
    "enabled": True, "collectEnabled": True, "windowDays": 180, "halfLifeDays": 45, "minItemUsers": 2,
    "weights": {"complete": 1.0, "skip": -0.6, "playlistAdd": 2.0},
    "als": {"factors": 48, "iterations": 12, "regularization": 0.05, "alpha": 20},
    "itemcf": {"neighbors": 50, "sessionBoost": 0.3},
    "blend": {"als": 1.0, "itemcf": 0.8, "artist": 0.5, "trending": 0.3, "explore": 0.1},
    "artistCap": 2, "recentExcludeDays": 2, "exploreRatio": 0.1, "schedule": "0 4 * * *", "autoPublish": True, "minLift": 1.0,
    "ranker": {"enabled": True, "minSamples": 2000, "l2": 0.001, "epochs": 8},
    "updatedAt": 1763280000000, "updatedBy": "root",
}


async def test_config_merge(backend, call, call_error):
    backend.reply("GET", f"{B}/config", CONFIG)
    await call("music_config", {"app": "demo"})
    assert backend.last().path == f"{B}/config"
    await call("music_update_config", {
        "app": "demo", "window_days": 90, "schedule": "", "fold_in": False,
        "weights": {"skip": -1, "seek_back": -0.2}, "als": {"factors": 64}, "ranker": {"min_samples": 5000}, "mixes": {"max": 0},
    })
    req = backend.find("PUT", f"{B}/config")
    expected = {key: value for key, value in CONFIG.items() if key not in ("updatedAt", "updatedBy")}
    expected.update({
        "windowDays": 90, "schedule": "", "foldIn": False,
        "weights": {"complete": 1.0, "skip": -1, "playlistAdd": 2.0, "seekBack": -0.2},
        "als": {"factors": 64, "iterations": 12, "regularization": 0.05, "alpha": 20},
        "ranker": {"enabled": True, "minSamples": 5000, "l2": 0.001, "epochs": 8},
        "mixes": {"max": 0},
    })
    assert req.json == expected
    assert isinstance(req.json["als"]["factors"], int) and isinstance(req.json["ranker"]["minSamples"], int)
    assert "没有要修改" in await call_error("music_update_config", {"app": "demo"})
    await call_error("music_update_config", {"app": "demo", "als": {"factors": 4}})
    await call_error("music_update_config", {"app": "demo", "explore_ratio": 0.9})


RULE = {
    "id": 3, "kind": "boost", "target": {"type": "work", "workKey": "w1", "title": "晴天", "track": None},
    "scenes": ["home"], "weight": 1.5, "position": 0, "startsAt": None, "endsAt": 1764000000000,
    "enabled": True, "note": "推新歌", "createdAt": 1, "updatedAt": 1, "operator": "root", "stats": {"served": 0, "clicks": 0},
}


async def test_rules(backend, call, call_error):
    await call("music_rules", {"app": "demo"})
    assert backend.last().path == f"{B}/rules"

    await call("music_save_rule", {"app": "demo", "kind": "pin", "source": "wy", "song_id": "186016", "position": 3,
                                   "scenes": ["home", "daily"], "starts_at": "2026-11-17T00:00:00+08:00"})
    req = backend.last()
    assert (req.method, req.path) == ("POST", f"{B}/rules")
    assert req.json == {"kind": "pin", "target": {"type": "work", "source": "wy", "songId": "186016"}, "scenes": ["home", "daily"],
                        "weight": 1, "position": 3, "startsAt": 1794844800000, "endsAt": None, "enabled": True, "note": ""}
    await call("music_save_rule", {"app": "demo", "kind": "block", "artist": "某歌手", "scenes": ["home"]})
    assert backend.last().json["target"] == {"type": "artist", "artist": "某歌手"} and backend.last().json["scenes"] == []
    await call_error("music_save_rule", {"app": "demo", "kind": "boost", "work_key": "w1"})
    await call_error("music_save_rule", {"app": "demo", "kind": "pin", "work_key": "w1"})
    await call_error("music_save_rule", {"app": "demo", "kind": "block"})
    assert "三选一" in await call_error("music_save_rule", {"app": "demo", "kind": "block", "work_key": "w1", "artist": "x"})
    await call_error("music_save_rule", {"app": "demo", "kind": "boost", "work_key": "w1", "weight": 20})

    backend.reply("GET", f"{B}/rules", {"items": [RULE]})
    await call("music_save_rule", {"app": "demo", "rule_id": 3, "weight": 2, "ends_at": ""})
    req = backend.find("PUT", f"{B}/rules/3")
    assert req.json == {"kind": "boost", "target": {"type": "work", "workKey": "w1"}, "scenes": ["home"], "weight": 2, "position": 0,
                        "startsAt": None, "endsAt": None, "enabled": True, "note": "推新歌"}
    backend.reply("GET", f"{B}/rules", [RULE])
    await call("music_save_rule", {"app": "demo", "rule_id": 3, "enabled": False})
    assert backend.last().json["enabled"] is False and backend.last().json["endsAt"] == 1764000000000
    assert "music_rules" in await call_error("music_save_rule", {"app": "demo", "rule_id": 9, "enabled": False})

    await call("music_delete_rule", {"app": "demo", "rule_id": 3})
    assert (backend.last().method, backend.last().path) == ("DELETE", f"{B}/rules/3")


EXPERIMENT = {
    "id": 5, "name": "精排对比", "description": "", "scenes": ["home"], "status": "draft",
    "variants": [{"key": "A", "name": "对照", "traffic": 50, "overrides": {}},
                 {"key": "B", "name": "精排", "traffic": 50, "overrides": {"ranker": True}}],
}


async def test_experiments(backend, call, call_error):
    await call("music_experiments", {"app": "demo"})
    assert backend.last().path == f"{B}/experiments"
    variants = [
        {"key": "A", "name": "对照", "traffic": 45},
        {"key": "B", "name": "多探索", "traffic": 45, "overrides": {"explore_ratio": 0.3, "fold_in": False, "model_run_id": 12,
                                                                   "blend": {"explore": 1.5}}},
    ]
    await call("music_save_experiment", {"app": "demo", "name": "探索", "scenes": ["home", "radio"], "variants": variants})
    req = backend.last()
    assert (req.method, req.path) == ("POST", f"{B}/experiments")
    assert req.json == {"name": "探索", "description": "", "scenes": ["home", "radio"], "variants": [
        {"key": "A", "name": "对照", "traffic": 45, "overrides": {}},
        {"key": "B", "name": "多探索", "traffic": 45, "overrides": {"exploreRatio": 0.3, "foldIn": False, "modelRunId": 12, "blend": {"explore": 1.5}}},
    ]}
    await call_error("music_save_experiment", {"app": "demo", "name": "x", "scenes": ["home"]})
    assert "100" in await call_error("music_save_experiment", {"app": "demo", "name": "x", "scenes": ["home"], "variants": [
        {"key": "A", "name": "a", "traffic": 60}, {"key": "B", "name": "b", "traffic": 60}]})
    assert "对照组" in await call_error("music_save_experiment", {"app": "demo", "name": "x", "scenes": ["home"], "variants": [
        {"key": "B", "name": "b", "traffic": 10}, {"key": "C", "name": "c", "traffic": 10}]})
    assert "重复" in await call_error("music_save_experiment", {"app": "demo", "name": "x", "scenes": ["home"], "variants": [
        {"key": "A", "name": "a", "traffic": 10}, {"key": "A", "name": "b", "traffic": 10}]})

    backend.reply("GET", f"{B}/experiments", {"items": [EXPERIMENT]})
    await call("music_save_experiment", {"app": "demo", "experiment_id": 5, "description": "看精排能否提升点击"})
    req = backend.find("PUT", f"{B}/experiments/5")
    assert req.json == {"name": "精排对比", "description": "看精排能否提升点击", "scenes": ["home"], "variants": EXPERIMENT["variants"]}

    for action, method, path in (("start", "POST", f"{B}/experiments/5/start"), ("stop", "POST", f"{B}/experiments/5/stop"),
                                 ("delete", "DELETE", f"{B}/experiments/5")):
        await call("music_experiment_action", {"app": "demo", "experiment_id": 5, "action": action})
        assert (backend.last().method, backend.last().path) == (method, path)
    await call_error("music_experiment_action", {"app": "demo", "experiment_id": 5, "action": "pause"})

    await call("music_experiment_results", {"app": "demo", "experiment_id": 5})
    assert (backend.last().path, backend.last().query) == (f"{B}/experiments/5/results", {})
    await call("music_experiment_results", {"app": "demo", "experiment_id": 5, "days": 14})
    assert backend.last().query == {"days": "14"}


async def test_tags(mcp):
    tools = {tool.name: set((tool.meta or {}).get("fastmcp", {}).get("tags") or []) for tool in await mcp.list_tools()}
    music = {name for name in tools if name.startswith("music_")}
    assert music == {
        "music_overview", "music_user", "music_preview", "music_tracks", "music_track", "music_artists", "music_search_stats",
        "music_models", "music_train", "music_publish", "music_config", "music_update_config", "music_rules", "music_save_rule",
        "music_delete_rule", "music_experiments", "music_save_experiment", "music_experiment_action", "music_experiment_results",
        "music_forget_user",
    }
    destructive = {name for name in music if "destructive" in tools[name]}
    assert destructive == {"music_forget_user", "music_delete_rule", "music_experiment_action"}
    writes = {name for name in music if "write" in tools[name]}
    assert writes == destructive | {"music_train", "music_publish", "music_update_config", "music_save_rule", "music_save_experiment"}
