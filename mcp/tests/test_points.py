"""积分与经验模块。"""

from __future__ import annotations

U = "/api/admin/apps/demo/users/7/points"


async def test_summary_and_transactions(backend, call, call_error):
    await call("points_summary", {"app": "demo", "user_id": 7})
    assert (backend.last().path, backend.last().query) == (f"{U}/integral/summary", {"days": "30"})
    await call("points_summary", {"app": "demo", "user_id": 7, "kind": "experience", "days": 0, "tz": "Asia/Shanghai"})
    assert (backend.last().path, backend.last().query) == (f"{U}/experience/summary", {"tz": "Asia/Shanghai"})

    await call("points_transactions", {
        "app": "demo", "user_id": 7, "direction": "out", "category": "admin_adjust",
        "from_time": "2026-10-01T00:00:00+08:00", "to_time": "2026-10-28T00:00:00+08:00", "limit": 50,
    })
    req = backend.last()
    assert (req.method, req.path) == ("GET", f"{U}/integral/transactions")
    assert req.query == {"direction": "out", "category": "admin_adjust", "from": "2026-10-01T00:00:00+08:00",
                         "to": "2026-10-28T00:00:00+08:00", "page": "1", "limit": "50"}
    await call_error("points_transactions", {"app": "demo", "user_id": 7, "kind": "coins"})
    await call_error("points_transactions", {"app": "demo", "user_id": 7, "limit": 101})


async def test_adjust_integral(backend, call, call_error):
    await call("points_adjust_integral", {"app": "demo", "user_id": 7, "amount": -50, "reason": "补扣", "request_id": "3f1c0d9e-aaaa-bbbb"})
    req = backend.last()
    assert (req.method, req.path) == ("POST", "/api/app/points/adjust-integral")
    assert req.json == {"appid": 42, "user_id": 7, "amount": -50, "reason": "补扣", "request_id": "3f1c0d9e-aaaa-bbbb"}
    await call("points_adjust_integral", {"app": "9", "user_id": 7, "amount": 5})
    assert backend.last().json == {"appid": 9, "user_id": 7, "amount": 5}

    assert "不能为 0" in await call_error("points_adjust_integral", {"app": "demo", "user_id": 7, "amount": 0})
    await call_error("points_adjust_integral", {"app": "demo", "user_id": 7, "amount": 100_000_001})
    assert "request_id" in await call_error("points_adjust_integral", {"app": "demo", "user_id": 7, "amount": 1, "request_id": "abc"})
    assert "apps_list" in await call_error("points_adjust_integral", {"app": "nope", "user_id": 7, "amount": 1})


async def test_add_experience(backend, call, call_error):
    await call("points_add_experience", {"app": "demo", "user_id": 7, "amount": 20})
    assert (backend.last().path, backend.last().json) == ("/api/app/points/adjust-experience", {"appid": 42, "user_id": 7, "amount": 20})
    await call_error("points_add_experience", {"app": "demo", "user_id": 7, "amount": -1})


async def test_adjust_experience(backend, call, call_error):
    await call("points_adjust_experience", {"app": "demo", "user_id": 7, "amount": -30, "level_mode": "recalc", "reason": "刷经验"})
    assert (backend.last().path, backend.last().json) == (
        "/api/app/points/adjust-experience", {"appid": 42, "user_id": 7, "amount": -30, "reason": "刷经验", "level_mode": "recalc"})
    await call("points_adjust_experience", {"app": "demo", "user_id": 7, "amount": 5})
    assert backend.last().json == {"appid": 42, "user_id": 7, "amount": 5}
    assert "不能为 0" in await call_error("points_adjust_experience", {"app": "demo", "user_id": 7, "amount": 0})
    await call_error("points_adjust_experience", {"app": "demo", "user_id": 7, "amount": -1, "level_mode": "drop"})


async def test_batch_adjust(backend, call, call_error):
    await call("points_batch_adjust", {"app": "demo", "user_ids": [1, 2, 3], "amount": 10, "operation": "consume", "reason": "回收"})
    req = backend.last()
    assert (req.method, req.path) == ("POST", "/api/app/points/batch-adjust")
    assert req.json == {"appid": 42, "user_ids": [1, 2, 3], "amount": 10, "operation_type": "consume", "reason": "回收"}
    await call("points_batch_adjust", {"app": "demo", "user_ids": [1], "amount": 10, "operation": "consume", "kind": "experience", "level_mode": "recalc"})
    assert backend.last().json == {"appid": 42, "user_ids": [1], "amount": 10, "operation_type": "consume", "kind": "experience", "level_mode": "recalc"}
    await call_error("points_batch_adjust", {"app": "demo", "user_ids": [], "amount": 10})
    await call_error("points_batch_adjust", {"app": "demo", "user_ids": [1], "amount": 0})


async def test_reverse(backend, call):
    backend.reply("POST", f"{U}/integral/transactions/ADI123/reverse", {"replayed": False})
    assert await call("points_reverse", {"app": "demo", "user_id": 7, "transaction_no": " ADI123 "}) == {"replayed": False}
    assert backend.last().json == {}
    await call("points_reverse", {"app": "demo", "user_id": 7, "transaction_no": "ADI123", "reason": "误发"})
    assert backend.last().json == {"reason": "误发"}
    backend.reply("POST", f"{U}/experience/transactions/EXP9/reverse", {"replayed": False})
    await call("points_reverse", {"app": "demo", "user_id": 7, "transaction_no": "EXP9", "kind": "experience", "level_mode": "recalc"})
    assert (backend.last().path, backend.last().json) == (f"{U}/experience/transactions/EXP9/reverse", {"levelMode": "recalc"})


async def test_stats_and_backend_error(backend, call, call_error):
    await call("points_stats", {"app": "demo", "days": 7})
    assert (backend.last().method, backend.last().path, backend.last().json) == ("POST", "/api/app/points/stats", {"appid": 42, "time_range": 7})
    backend.reply("POST", "/api/app/points/adjust-integral", code=40000, status=400, message="积分不足")
    assert "积分不足" in await call_error("points_adjust_integral", {"app": "demo", "user_id": 7, "amount": -5})
