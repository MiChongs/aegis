"""远程函数模块。"""

from __future__ import annotations

A = "/api/admin/apps/demo"
F = f"{A}/functions/quota"


async def test_catalog_overview_list_get(backend, call):
    await call("functions_catalog", {"app": "demo"})
    assert backend.last().path == f"{A}/function-catalog"
    await call("functions_overview", {"app": "demo", "window_hours": 48})
    assert (backend.last().path, backend.last().query) == (f"{A}/function-overview", {"windowHours": "48"})
    await call("functions_list", {"app": "demo"})
    assert (backend.last().method, backend.last().path) == ("GET", f"{A}/functions")
    await call("functions_get", {"app": "demo", "function_name": "quota"})
    assert (backend.last().method, backend.last().path) == ("GET", F)


async def test_create_and_update(backend, call, call_error):
    await call("functions_create", {"app": "demo", "name": "quota", "capabilities": ["kv.write"], "timeout_ms": 800,
                                    "config": {"dailyQuota": 100}})
    req = backend.last()
    assert (req.method, req.path) == ("POST", f"{A}/functions")
    assert req.json == {"name": "quota", "description": "", "runtime": "script", "capabilities": ["kv.write"],
                        "timeoutMs": 800, "config": {"dailyQuota": 100}}
    await call("functions_update", {"app": "demo", "function_name": "quota", "status": "active", "capabilities": []})
    req = backend.last()
    assert (req.method, req.path, req.json) == ("PUT", F, {"status": "active", "capabilities": []})
    await call_error("functions_update", {"app": "demo", "function_name": "quota"})
    await call_error("functions_create", {"app": "demo", "name": "Bad Name"})
    await call("functions_delete", {"app": "demo", "function_name": "quota"})
    assert (backend.last().method, backend.last().path) == ("DELETE", F)


async def test_analyze_and_test_with_source(backend, call, call_error):
    await call("functions_analyze", {"app": "demo", "function_name": "quota", "source": "function handle(ctx){}"})
    assert (backend.last().path, backend.last().json) == (f"{F}/analyze", {"source": "function handle(ctx){}"})
    await call("functions_test", {"app": "demo", "function_name": "quota", "source": "x", "as_user_id": 7,
                                  "trigger_kind": "event", "trigger_event": "user.signin", "trigger_data": {"consecutiveDays": 7}})
    assert (backend.last().path, backend.last().json) == (f"{F}/test", {
        "source": "x", "input": {}, "asUserId": 7,
        "trigger": {"kind": "event", "event": "user.signin", "data": {"consecutiveDays": 7}},
    })
    await call_error("functions_test", {"app": "demo", "function_name": "quota", "source": "x", "trigger_event": "user.login"})
    await call_error("functions_test", {"app": "demo", "function_name": "quota", "source": "x", "trigger_kind": "event"})


async def test_analyze_falls_back_to_active_source(backend, call, call_error):
    backend.reply("GET", F, {"name": "quota", "activeVersion": "1.0.0"})
    backend.reply("GET", f"{F}/versions/1.0.0", {"version": "1.0.0", "source": "function handle(){return 1}"})
    await call("functions_analyze", {"app": "demo", "function_name": "quota"})
    assert backend.find("POST", f"{F}/analyze").json == {"source": "function handle(){return 1}"}
    backend.reply("GET", F, {"name": "quota"})
    assert "激活版本" in await call_error("functions_test", {"app": "demo", "function_name": "quota"})


async def test_versions(backend, call, call_error):
    await call("functions_versions", {"app": "demo", "function_name": "quota"})
    assert backend.last().path == f"{F}/versions"
    await call("functions_get_version", {"app": "demo", "function_name": "quota", "version": "1.0.0"})
    assert backend.last().path == f"{F}/versions/1.0.0"
    await call("functions_publish_version", {"app": "demo", "function_name": "quota", "version": "1.0.1", "source": "s", "notes": "n"})
    req = backend.last()
    assert (req.method, req.path, req.json) == ("POST", f"{F}/versions", {"version": "1.0.1", "source": "s", "notes": "n"})
    await call("functions_publish_version", {"app": "demo", "function_name": "quota", "version": "2", "endpoint_url": "https://w.example/f",
                                             "response_public_key": "pk"})
    assert backend.last().json == {"version": "2", "endpointUrl": "https://w.example/f", "responsePublicKey": "pk"}
    await call_error("functions_publish_version", {"app": "demo", "function_name": "quota", "version": "3"})
    await call("functions_activate_version", {"app": "demo", "function_name": "quota", "version": "1.0.0"})
    assert (backend.last().method, backend.last().path) == ("POST", f"{F}/versions/1.0.0/activate")
    await call("functions_delete_version", {"app": "demo", "function_name": "quota", "version": "0.9"})
    assert (backend.last().method, backend.last().path) == ("DELETE", f"{F}/versions/0.9")


async def test_invoke_invocations_replay_stats(backend, call):
    await call("functions_invoke", {"app": "demo", "function_name": "quota", "input": {"n": 1}, "as_user_id": 9, "event_id": "e-1"})
    assert (backend.last().path, backend.last().json) == (f"{F}/invoke", {"eventId": "e-1", "input": {"n": 1}, "asUserId": 9})
    await call("functions_invoke", {"app": "demo", "function_name": "quota"})
    assert backend.last().json == {"input": {}}
    await call("functions_invocations", {"app": "demo", "function_name": "quota", "status": "error", "caller_type": "app",
                                         "trigger_id": 3, "page": 2})
    assert (backend.last().path, backend.last().query) == (f"{F}/invocations", {
        "status": "error", "callerType": "app", "triggerId": "3", "page": "2", "limit": "20",
    })
    await call("functions_get_invocation", {"app": "demo", "function_name": "quota", "invocation_id": 55})
    assert (backend.last().method, backend.last().path) == ("GET", f"{F}/invocations/55")
    await call("functions_replay", {"app": "demo", "function_name": "quota", "invocation_id": 55})
    assert (backend.last().method, backend.last().path) == ("POST", f"{F}/invocations/55/replay")
    await call("functions_stats", {"app": "demo", "function_name": "quota", "window_hours": 72})
    assert (backend.last().path, backend.last().query) == (f"{F}/stats", {"hours": "72"})


async def test_triggers(backend, call, call_error):
    await call("functions_triggers", {"app": "demo"})
    assert backend.last().path == f"{A}/function-triggers"
    await call("functions_triggers", {"app": "demo", "function_name": "quota"})
    assert backend.last().path == f"{F}/triggers"
    await call("functions_create_trigger", {"app": "demo", "function_name": "quota", "kind": "schedule", "cron": "0 9 * * *",
                                            "timezone": "Asia/Shanghai", "input": {"mode": "daily"}})
    req = backend.last()
    assert (req.method, req.path, req.json) == ("POST", f"{F}/triggers", {
        "kind": "schedule", "cron": "0 9 * * *", "timezone": "Asia/Shanghai", "input": {"mode": "daily"}, "enabled": True,
    })
    await call_error("functions_create_trigger", {"app": "demo", "function_name": "quota", "kind": "event"})
    await call_error("functions_create_trigger", {"app": "demo", "function_name": "quota", "kind": "event", "event": "user.login", "cron": "* * * * *"})

    backend.reply("GET", f"{F}/triggers", {"items": [{"id": 4, "kind": "event", "description": "注册礼"}]})
    await call("functions_update_trigger", {"app": "demo", "function_name": "quota", "trigger_id": 4, "enabled": False})
    req = backend.find("PUT", f"{F}/triggers/4")
    assert req.json == {"description": "注册礼", "enabled": False}
    await call("functions_update_trigger", {"app": "demo", "function_name": "quota", "trigger_id": 4, "description": ""})
    assert backend.last().json == {"description": ""}
    await call_error("functions_update_trigger", {"app": "demo", "function_name": "quota", "trigger_id": 99, "enabled": True})

    await call("functions_delete_trigger", {"app": "demo", "function_name": "quota", "trigger_id": 4})
    assert (backend.last().method, backend.last().path) == ("DELETE", f"{F}/triggers/4")
    await call("functions_trigger_fire", {"app": "demo", "function_name": "quota", "trigger_id": 4, "user_id": 7, "data": {"orderId": "o1"}})
    assert (backend.last().path, backend.last().json) == (f"{F}/triggers/4/fire", {"userId": 7, "data": {"orderId": "o1"}})
    await call("functions_trigger_jobs", {"app": "demo", "function_name": "quota", "status": "failed"})
    assert (backend.last().path, backend.last().query) == (f"{F}/trigger-jobs", {"status": "failed", "page": "1", "limit": "20"})
    await call("functions_retry_trigger_job", {"app": "demo", "function_name": "quota", "job_id": 12})
    assert (backend.last().method, backend.last().path) == ("POST", f"{F}/trigger-jobs/12/retry")


async def test_kv_and_keys(backend, call, call_error):
    await call("functions_kv", {"app": "demo", "scope": "user", "scope_id": 7, "prefix": "quota:"})
    assert (backend.last().path, backend.last().query) == (f"{A}/function-kv", {
        "scope": "user", "scopeId": "7", "prefix": "quota:", "page": "1", "limit": "20",
    })
    await call("functions_delete_kv", {"app": "demo", "key": "quota:20261122", "scope": "user", "scope_id": 7})
    req = backend.last()
    assert (req.method, req.query) == ("DELETE", {"scope": "user", "scopeId": "7", "key": "quota:20261122"})
    await call_error("functions_delete_kv", {"app": "demo", "key": "k", "scope": "user"})
    await call("functions_keys", {"app": "demo"})
    assert backend.last().path == f"{A}/function-keys"
    await call("functions_create_key", {"app": "demo", "name": "worker"})
    assert (backend.last().method, backend.last().json) == ("POST", {"name": "worker"})
    await call("functions_revoke_key", {"app": "demo", "key_id": 3})
    assert (backend.last().method, backend.last().path) == ("DELETE", f"{A}/function-keys/3")


async def test_tool_classes(mcp):
    tools = {tool.name: set((tool.meta or {}).get("fastmcp", {}).get("tags") or []) for tool in await mcp.list_tools()}
    for name in ("functions_delete", "functions_delete_version", "functions_delete_trigger", "functions_delete_kv", "functions_revoke_key"):
        assert "destructive" in tools[name], name
    for name in ("functions_test", "functions_analyze", "functions_replay", "functions_invoke", "functions_trigger_fire"):
        assert "write" in tools[name] and "destructive" not in tools[name], name
    for name in ("functions_catalog", "functions_invocations", "functions_kv", "functions_triggers"):
        assert "read" in tools[name], name
