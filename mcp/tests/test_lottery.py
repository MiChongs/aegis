"""抽奖模块。"""

from __future__ import annotations

BASE = "/api/admin/apps/demo/lottery"

PRIZES = [{"name": "谢谢参与", "reward": {"type": "none"}, "quantity": -1, "weight": 80, "position": 0},
          {"name": "10 积分", "reward": {"type": "points", "amount": 10}, "quantity": -1, "weight": 20, "position": 1}]

CURRENT = {
    "id": 5, "appid": 42, "name": "国庆", "description": "d", "rules": "r", "uiMode": "wheel", "status": "active",
    "startTime": "2026-10-01T00:00:00+08:00", "freeDaily": 1, "pointsCost": 0, "pointsDaily": 0, "videoScene": "",
    "pityThreshold": 10, "pityPrizeId": 12, "lifetimePolicy": "points", "lifetimePointsPerDay": 10,
    "lifetimePointsLifetime": 100, "lifetimeDraws": 0, "strategy": {}, "perks": [], "sortOrder": 0,
    "createdAt": "x", "updatedAt": "y", "totalDraws": 3, "pending": 0,
    "prizes": [
        {"id": 11, "activityId": 5, "name": "谢谢参与", "kind": "none", "reward": {"type": "none"}, "imageUrl": "", "quantity": -1,
         "used": 2, "weight": 80, "position": 0, "fallback": True, "dailyLimit": 0, "userLimit": 0, "userDailyLimit": 0, "value": 0,
         "createdAt": "x", "updatedAt": "y"},
        {"id": 12, "activityId": 5, "name": "10 积分", "kind": "points", "reward": {"type": "points", "amount": 10}, "imageUrl": "",
         "quantity": 100, "used": 1, "weight": 20, "position": 1, "fallback": False, "dailyLimit": 0, "userLimit": 0,
         "userDailyLimit": 0, "value": 10, "createdAt": "x", "updatedAt": "y"},
    ],
}


async def test_catalog_list_get(backend, call):
    await call("lottery_catalog", {"app": "demo"})
    assert backend.last().path == f"{BASE}/catalog"
    await call("lottery_activities", {"app": "demo"})
    assert backend.last().path == f"{BASE}/activities"
    await call("lottery_get", {"app": "demo", "activity_id": 5})
    assert (backend.last().method, backend.last().path) == ("GET", f"{BASE}/activities/5")


async def test_create(backend, call, call_error):
    await call("lottery_create", {"app": "demo", "name": "国庆", "prizes": PRIZES, "ui_mode": "wheel", "free_daily": 1,
                                  "start_time": "2026-10-01T00:00:00+08:00", "perks": [{"name": "会员加次", "audience": "member", "extraFree": 1}]})
    req = backend.last()
    assert (req.method, req.path) == ("POST", f"{BASE}/activities")
    assert req.json == {"name": "国庆", "uiMode": "wheel", "startTime": "2026-10-01T00:00:00+08:00", "freeDaily": 1,
                        "perks": [{"name": "会员加次", "audience": "member", "extraFree": 1}], "prizes": PRIZES}
    await call_error("lottery_create", {"app": "demo", "name": "x", "prizes": []})
    await call_error("lottery_create", {"app": "demo", "name": "x", "prizes": PRIZES, "lifetime_policy": "keep"})


async def test_update_merges_current(backend, call):
    backend.reply("GET", f"{BASE}/activities/5", CURRENT)
    await call("lottery_update", {"app": "demo", "activity_id": 5, "name": "国庆加码", "end_time": "2026-10-08T00:00:00+08:00", "start_time": ""})
    req = backend.find("PUT", f"{BASE}/activities/5")
    assert req.json["name"] == "国庆加码"
    assert req.json["status"] == "active"
    assert req.json["startTime"] is None and req.json["endTime"] == "2026-10-08T00:00:00+08:00"
    assert req.json["pityPosition"] == 1 and "pityPrizeId" not in req.json
    assert [p["id"] for p in req.json["prizes"]] == [11, 12]
    assert "used" not in req.json["prizes"][0] and "kind" not in req.json["prizes"][0]
    assert "totalDraws" not in req.json and "createdAt" not in req.json
    await call("lottery_update", {"app": "demo", "activity_id": 5, "prizes": PRIZES})
    assert backend.last().json["prizes"] == PRIZES


async def test_status_delete(backend, call, call_error):
    await call("lottery_set_status", {"app": "demo", "activity_id": 5, "status": "paused"})
    assert (backend.last().method, backend.last().path, backend.last().json) == ("PUT", f"{BASE}/activities/5/status", {"status": "paused"})
    await call_error("lottery_set_status", {"app": "demo", "activity_id": 5, "status": "archived"})
    await call("lottery_delete", {"app": "demo", "activity_id": 5})
    assert (backend.last().method, backend.last().path) == ("DELETE", f"{BASE}/activities/5")


async def test_delete_with_draws_errors(backend, call_error):
    backend.reply("DELETE", f"{BASE}/activities/5", code=40990, status=409, message="活动已有抽奖记录")
    assert "40990" in await call_error("lottery_delete", {"app": "demo", "activity_id": 5})


async def test_simulate(backend, call, call_error):
    await call("lottery_simulate", {"app": "demo", "activity_id": 5, "users": 10, "draws_per_user": 5, "seed": 1, "perk": {"member": True}})
    req = backend.last()
    assert (req.method, req.path, req.json) == ("POST", f"{BASE}/simulate", {
        "activityId": 5, "users": 10, "drawsPerUser": 5, "isolatedStock": False, "seed": 1, "perk": {"member": True},
    })
    await call_error("lottery_simulate", {"app": "demo"})
    await call_error("lottery_simulate", {"app": "demo", "activity_id": 5, "users": 5000, "draws_per_user": 1000})


async def test_stats_audit(backend, call):
    await call("lottery_stats", {"app": "demo", "activity_id": 5, "days": 30})
    assert (backend.last().path, backend.last().query) == (f"{BASE}/activities/5/stats", {"days": "30"})
    await call("lottery_audit", {"app": "demo", "activity_id": 5})
    assert (backend.last().path, backend.last().query) == (f"{BASE}/activities/5/audit", {"days": "14"})


async def test_analyze_and_solve(backend, call, call_error):
    personas = [{"key": "user", "share": 8}, {"key": "lifetime", "lifetime": True, "share": 2}]
    await call("lottery_analyze", {"app": "demo", "activity_id": 5, "personas": personas})
    req = backend.last()
    assert (req.method, req.path, req.json) == ("POST", f"{BASE}/analyze", {"activityId": 5, "personas": personas})
    await call("lottery_analyze", {"app": "demo", "activity": {"name": "草稿", "prizes": PRIZES}})
    assert backend.last().json == {"activity": {"name": "草稿", "prizes": PRIZES}}
    await call_error("lottery_analyze", {"app": "demo"})

    goal = {"realRate": 0.3, "pointsReturn": 18, "prizes": [{"position": 1, "mode": "target", "probability": 0.001}]}
    await call("lottery_solve", {"app": "demo", "activity_id": 5, "goal": goal, "persona": {"lifetime": True}, "fortune": "high"})
    req = backend.last()
    assert (req.method, req.path) == ("POST", f"{BASE}/solve")
    assert req.json == {"activityId": 5, "persona": {"lifetime": True}, "fortune": "high", "goal": goal}
    await call("lottery_solve", {"app": "demo", "activity_id": 5, "goal": {"winRate": 0.4}})
    assert backend.last().json == {"activityId": 5, "fortune": "average", "goal": {"winRate": 0.4}}
    await call_error("lottery_solve", {"app": "demo", "goal": goal})
    await call_error("lottery_solve", {"app": "demo", "activity_id": 5, "goal": goal, "fortune": "best"})


async def test_compute_jobs(backend, call, call_error):
    await call("lottery_compute_run", {"app": "demo", "activity_id": 5, "users": 500, "days": 3, "draws_per_day": 4,
                                       "replications": 16, "seed": "20261120", "name": "上线前",
                                       "personas": [{"lifetime": True, "share": 1, "drawsPerDay": 10}]})
    req = backend.last()
    assert (req.method, req.path) == ("POST", f"{BASE}/compute/jobs")
    assert req.json == {"activityId": 5, "name": "上线前", "users": 500, "days": 3, "drawsPerDay": 4, "replications": 16,
                        "personas": [{"lifetime": True, "share": 1, "drawsPerDay": 10}], "seed": "20261120"}
    await call("lottery_compute_run", {"app": "demo", "activity_id": 5})
    assert backend.last().json == {"activityId": 5, "users": 2000, "days": 7, "drawsPerDay": 5, "replications": 24}
    await call_error("lottery_compute_run", {"app": "demo"})
    await call_error("lottery_compute_run", {"app": "demo", "activity_id": 5, "seed": "abc"})
    await call_error("lottery_compute_run", {"app": "demo", "activity_id": 5, "users": 200_000})

    await call("lottery_compute_jobs", {"app": "demo", "activity_id": 5})
    assert (backend.last().method, backend.last().path, backend.last().query) == (
        "GET", f"{BASE}/compute/jobs", {"activityId": "5", "page": "1", "limit": "20"})
    await call("lottery_compute_job", {"app": "demo", "job_id": 9})
    assert (backend.last().method, backend.last().path) == ("GET", f"{BASE}/compute/jobs/9")
    await call("lottery_compute_cancel", {"app": "demo", "job_id": 9})
    assert (backend.last().method, backend.last().path) == ("POST", f"{BASE}/compute/jobs/9/cancel")
    await call("lottery_compute_delete", {"app": "demo", "job_id": 9})
    assert (backend.last().method, backend.last().path) == ("DELETE", f"{BASE}/compute/jobs/9")


async def test_draws_and_fulfill(backend, call):
    await call("lottery_draws", {"app": "demo", "activity_id": 5, "status": "pending", "winning": True, "start": "2026-10-01T00:00:00Z"})
    assert (backend.last().path, backend.last().query) == (f"{BASE}/draws", {
        "activityId": "5", "status": "pending", "winning": "true", "start": "2026-10-01T00:00:00Z", "page": "1", "limit": "20",
    })
    await call("lottery_draws", {"app": "demo", "winning": False})
    assert backend.last().query == {"page": "1", "limit": "20"}
    await call("lottery_fulfill", {"app": "demo", "draw_id": 8, "note": "兑换码 ABC"})
    assert (backend.last().method, backend.last().path, backend.last().json) == ("POST", f"{BASE}/draws/8/fulfill", {"note": "兑换码 ABC"})


async def test_tickets(backend, call, call_error):
    await call("lottery_tickets", {"app": "demo"})
    assert backend.last().path == f"{BASE}/tickets"
    await call("lottery_tickets", {"app": "demo", "user_id": 7})
    assert backend.last().path == f"{BASE}/tickets/users/7"
    await call("lottery_ticket_accounts", {"app": "demo", "keyword": "fay"})
    assert backend.last().path == f"{BASE}/tickets/accounts" and backend.last().query["keyword"] == "fay"
    await call("lottery_ticket_ledger", {"app": "demo", "user_id": 7, "kind": "consume"})
    assert backend.last().query["userId"] == "7" and backend.last().query["kind"] == "consume"
    await call("lottery_adjust_tickets", {"app": "demo", "user_id": 7, "amount": -2, "reason": "多发"})
    req = backend.last()
    assert (req.method, req.path) == ("POST", f"{BASE}/tickets/users/7/adjust")
    assert req.json == {"amount": -2, "reason": "多发"}
    await call_error("lottery_adjust_tickets", {"app": "demo", "user_id": 7, "amount": 0, "reason": "x"})



async def test_settings(backend, call):
    await call("lottery_settings", {"app": "demo"})
    assert (backend.last().method, backend.last().path) == ("GET", f"{BASE}/settings")
    await call("lottery_save_settings", {"app": "demo", "enabled": True, "boost": 1.5, "floor": 1, "cap": 2.5})
    req = backend.last()
    assert (req.method, req.path) == ("PUT", f"{BASE}/settings")
    assert req.json == {"supreme": {"enabled": True, "scope": "lifetime", "boost": 1.5, "floor": 1, "cap": 2.5}}
    await call("lottery_save_settings", {"app": "demo", "enabled": True, "scope": "tiers",
                                         "tiers": [{"tierId": 3, "boost": 2, "lifetimeOnly": True}]})
    assert backend.last().json == {"supreme": {"enabled": True, "scope": "tiers", "tiers": [{"tierId": 3, "boost": 2, "lifetimeOnly": True}]}}
