"""优惠券与优惠活动模块。"""

from __future__ import annotations

BASE = "/api/admin/apps/demo/coupons"


async def test_reads(backend, call):
    for tool, path in (("coupons_catalog", "/catalog"), ("coupons_templates", "/templates"), ("coupons_promotions", "/promotions"),
                       ("coupons_user_coupons", "/user-coupons"), ("coupons_orders", "/orders")):
        await call(tool, {"app": "demo"})
        assert (backend.last().method, backend.last().path) == ("GET", BASE + path)
    await call("coupons_overview", {"app": "demo", "days": 7})
    assert backend.last().query == {"tz": "Asia/Shanghai", "days": "7"}
    await call("coupons_user_coupons", {"app": "demo", "status": "usable", "template_id": 3})
    assert backend.last().query == {"status": "usable", "templateId": "3", "page": "1", "limit": "20"}


async def test_template_writes(backend, call, call_error):
    await call("coupons_create_template", {"app": "demo", "config": {"name": "满 100 减 20", "kind": "amount_off"}})
    assert (backend.last().method, backend.last().path, backend.last().json["name"]) == ("POST", f"{BASE}/templates", "满 100 减 20")
    backend.reply("GET", f"{BASE}/templates/5", {"id": 5, "name": "旧名", "kind": "amount_off", "amount": "20", "issuedCount": 3})
    await call("coupons_update_template", {"app": "demo", "template_id": 5, "changes": {"name": "新名"}})
    req = backend.find("PUT", f"{BASE}/templates/5")
    assert req.json["name"] == "新名" and req.json["amount"] == "20" and "issuedCount" not in req.json
    await call("coupons_set_template_status", {"app": "demo", "template_id": 5, "status": "active"})
    assert (backend.last().path, backend.last().json) == (f"{BASE}/templates/5/status", {"status": "active"})
    await call("coupons_issue", {"app": "demo", "template_id": 5, "user_ids": [1, 2], "count": 2})
    assert (backend.last().path, backend.last().json) == (f"{BASE}/templates/5/issue", {"userIds": [1, 2], "count": 2, "notify": False})
    await call_error("coupons_issue", {"app": "demo", "template_id": 5, "user_ids": [], "count": 1})
    await call("coupons_revoke", {"app": "demo", "coupon_id": 9, "reason": "误发"})
    assert (backend.last().path, backend.last().json) == (f"{BASE}/user-coupons/9/revoke", {"reason": "误发"})
    await call("coupons_delete_template", {"app": "demo", "template_id": 5})
    assert (backend.last().method, backend.last().path) == ("DELETE", f"{BASE}/templates/5")


async def test_promotion_and_quote(backend, call):
    backend.reply("GET", f"{BASE}/promotions/4", {"id": 4, "name": "8 折", "kind": "discount", "rule": {"discountMode": "percent", "rate": 80}, "usedCount": 9})
    await call("coupons_update_promotion", {"app": "demo", "promotion_id": 4, "changes": {"rule": {"discountMode": "percent", "rate": 75}}})
    req = backend.find("PUT", f"{BASE}/promotions/4")
    assert req.json["rule"]["rate"] == 75 and req.json["name"] == "8 折" and "usedCount" not in req.json
    await call("coupons_quote", {"app": "demo", "user_id": 7, "plan_id": 3})
    assert (backend.last().method, backend.last().path) == ("POST", f"{BASE}/quote")
    assert backend.last().json == {"userId": 7, "purpose": "vip_purchase", "planId": 3, "amount": "", "couponMode": "auto"}
