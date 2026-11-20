"""个性装扮模块。"""

from __future__ import annotations

B = "/api/admin/apps/demo/cosmetics"

ITEM = {
    "id": 7, "slot": "avatar_frame", "code": "halo", "name": "鎏金光环", "rarity": "legendary", "description": "",
    "series": "", "status": "active", "unlock": {"mode": "vip", "tierIds": [2]}, "revision": 1, "sortOrder": 10, "hidden": False,
}


async def test_catalog_settings(backend, call):
    await call("cosmetics_catalog", {"app": "demo"})
    assert backend.last().path == f"{B}/catalog"
    await call("cosmetics_save_settings", {"app": "demo", "enabled": True, "badge_slots": 4})
    assert (backend.last().method, backend.last().path, backend.last().json) == (
        "PUT", f"{B}/settings", {"enabled": True, "badgeSlots": 4})


async def test_create_and_update(backend, call, call_error):
    await call("cosmetics_create_item", {"app": "demo", "slot": "badge", "code": "founder", "name": "元老",
                                         "unlock_mode": "level", "min_level": 10, "template": "badge_text"})
    req = backend.last()
    assert (req.method, req.path) == ("POST", f"{B}/items")
    assert req.json == {"slot": "badge", "code": "founder", "name": "元老", "rarity": "common", "hidden": False,
                        "template": "badge_text", "unlock": {"mode": "level", "minLevel": 10}}
    await call_error("cosmetics_create_item", {"app": "demo", "slot": "hat", "code": "x", "name": "x"})

    backend.reply("GET", f"{B}/items/7", ITEM)
    await call("cosmetics_update_item", {"app": "demo", "item_id": 7, "name": "新名字", "min_level": 3, "unlock_mode": "level"})
    req = backend.find("PUT", f"{B}/items/7")
    assert req.json["name"] == "新名字" and req.json["code"] == "halo"
    assert req.json["unlock"] == {"mode": "level", "tierIds": [2], "minLevel": 3}


async def test_design_flow(backend, call):
    await call("cosmetics_save_draft", {"app": "demo", "item_id": 7, "design": {"version": 1, "layers": []}})
    assert (backend.last().method, backend.last().path) == ("PUT", f"{B}/items/7/draft")
    await call("cosmetics_publish", {"app": "demo", "item_id": 7, "note": "首发"})
    assert (backend.last().path, backend.last().json) == (f"{B}/items/7/publish", {"note": "首发"})
    await call("cosmetics_restore_revision", {"app": "demo", "item_id": 7, "revision": 2})
    assert backend.last().path == f"{B}/items/7/revisions/2/restore"
    await call("cosmetics_set_status", {"app": "demo", "item_id": 7, "status": "archived"})
    assert backend.last().json == {"status": "archived"}


async def test_users(backend, call):
    await call("cosmetics_grant", {"app": "demo", "user_id": 3, "item_id": 7, "days": 30})
    assert (backend.last().path, backend.last().json) == (f"{B}/users/3/grant", {"cosmeticId": 7, "days": 30})
    await call("cosmetics_revoke", {"app": "demo", "user_id": 3, "ownership_id": 9, "reason": "误发"})
    assert (backend.last().path, backend.last().json) == (f"{B}/users/3/ownerships/9/revoke", {"reason": "误发"})
    await call("cosmetics_unequip", {"app": "demo", "user_id": 3, "slot": "badge", "position": 1})
    assert backend.last().json == {"slot": "badge", "position": 1}
    await call("cosmetics_ownerships", {"app": "demo", "item_id": 7, "state": "live"})
    assert backend.last().query == {"cosmeticId": "7", "state": "live", "page": "1", "limit": "50"}
