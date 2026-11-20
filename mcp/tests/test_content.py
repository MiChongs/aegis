"""内容与通知模块。"""

from __future__ import annotations


async def test_overview_and_notice_list(backend, call):
    await call("content_overview", {"app": "demo"})
    assert backend.last().path == "/api/admin/apps/demo/content/overview"
    await call("content_notices", {"app": "demo", "status": "published", "keyword": "维护"})
    req = backend.last()
    assert req.path == "/api/admin/apps/demo/notices" and req.query == {"status": "published", "keyword": "维护", "page": "1", "limit": "20"}


async def test_create_and_update_notice(backend, call):
    await call("content_create_notice", {"app": "demo", "title": "t", "content": "c", "start_time": "2026-11-01T00:00:00Z"})
    assert backend.last().json == {"title": "t", "content": "c", "type": "notice", "level": "normal", "status": "draft", "pinned": False, "startTime": "2026-11-01T00:00:00Z"}
    await call("content_update_notice", {"app": "demo", "notice_id": 3, "status": "published"})
    req = backend.last()
    assert (req.method, req.path, req.json) == ("PUT", "/api/admin/apps/demo/notices/3", {"status": "published"})


async def test_delete_notices_single_and_bulk(backend, call):
    await call("content_delete_notices", {"app": "demo", "notice_ids": [5]})
    assert (backend.last().method, backend.last().path) == ("DELETE", "/api/admin/apps/demo/notices/5")
    await call("content_delete_notices", {"app": "demo", "notice_ids": [5, 6]})
    assert (backend.last().path, backend.last().json) == ("/api/admin/apps/demo/notices", {"ids": [5, 6]})


async def test_banners(backend, call):
    await call("content_banners", {"app": "demo", "status": "enabled"})
    assert backend.last().query == {"status": "enabled"}
    await call("content_create_banner", {"app": "demo", "title": "b", "header": "https://img/x.png"})
    assert backend.last().json == {"header": "https://img/x.png", "title": "b", "content": "", "url": "", "status": True}
    await call("content_update_banner", {"app": "demo", "banner_id": 2, "status": False})
    assert (backend.last().path, backend.last().json) == ("/api/admin/apps/demo/banners/2", {"status": False})
    await call("content_reorder_banners", {"app": "demo", "banner_ids": [3, 1, 2]})
    assert (backend.last().method, backend.last().path, backend.last().json) == ("PUT", "/api/admin/apps/demo/banners/order", {"ids": [3, 1, 2]})
    await call("content_delete_banners", {"app": "demo", "banner_ids": [1, 2]})
    assert (backend.last().method, backend.last().json) == ("DELETE", {"ids": [1, 2]})


async def test_notify_users(backend, call, call_error):
    await call("content_notify_users", {"app": "demo", "title": "t", "content": "c", "user_ids": [1, 2], "action_type": "route", "action_target": "vip"})
    req = backend.last()
    assert req.path == "/api/admin/apps/demo/notifications/bulk"
    assert req.json == {"userIds": [1, 2], "type": "system", "title": "t", "content": "c", "level": "info", "action": {"type": "route", "target": "vip"}}
    text = await call_error("content_notify_users", {"app": "demo", "title": "t", "content": "c"})
    assert "群发范围" in text


async def test_notifications_list_and_delete(backend, call, call_error):
    await call("content_notifications", {"app": "demo", "category": "activity"})
    assert backend.last().query["category"] == "activity"
    await call("content_delete_notifications", {"app": "demo", "notification_ids": [9]})
    assert (backend.last().method, backend.last().path, backend.last().json) == ("DELETE", "/api/admin/apps/demo/notifications", {"ids": [9]})
    await call("content_delete_notifications", {"app": "demo", "keyword": "测试", "max_count": 10})
    assert (backend.last().path, backend.last().json) == ("/api/admin/apps/demo/notifications/delete-by-filter", {"keyword": "测试", "limit": 10})
    await call_error("content_delete_notifications", {"app": "demo"})


async def test_notifications_new_filters(backend, call):
    await call("content_notifications", {"app": "demo", "status": "unread", "user_id": 7, "campaign_id": 3})
    assert backend.last().query == {"status": "unread", "userId": "7", "campaignId": "3", "page": "1", "limit": "20"}


CAMPAIGNS = "/api/admin/apps/demo/notifications/campaigns"


async def test_campaign_catalog_preview_and_list(backend, call, call_error):
    await call("content_campaign_catalog", {"app": "demo"})
    assert (backend.last().method, backend.last().path) == ("GET", f"{CAMPAIGNS}/catalog")

    await call("content_preview_audience", {"app": "demo"})
    req = backend.last()
    assert (req.method, req.path, req.json) == ("POST", f"{CAMPAIGNS}/preview", {"audience": {"scope": "all", "account": "available"}})
    await call("content_preview_audience", {"app": "demo", "scope": "filter", "membership": "non_member", "inactive_days": 30,
                                            "platforms": ["android", "ios"], "exclude_user_ids": [9], "max_recipients": 500})
    assert backend.last().json == {"audience": {"scope": "filter", "excludeUserIds": [9], "account": "available",
                                                "membership": "non_member", "inactiveDays": 30,
                                                "platforms": ["android", "ios"], "limit": 500}}
    assert "scope=filter" in await call_error("content_preview_audience", {"app": "demo", "scope": "all", "membership": "member"})
    assert "user_ids" in await call_error("content_preview_audience", {"app": "demo", "scope": "users"})
    await call_error("content_preview_audience", {"app": "demo", "scope": "filter"})
    await call_error("content_preview_audience", {"app": "demo", "scope": "filter", "active_within_days": 7, "inactive_days": 30})
    await call_error("content_preview_audience", {"app": "demo", "scope": "filter", "min_level": 9, "max_level": 3})
    await call_error("content_preview_audience", {"app": "demo", "scope": "filter", "platforms": ["symbian"]})

    await call("content_campaigns", {"app": "demo", "status": "running"})
    assert (backend.last().method, backend.last().path, backend.last().query) == (
        "GET", CAMPAIGNS, {"status": "running", "page": "1", "limit": "20"})
    await call_error("content_campaigns", {"app": "demo", "limit": 101})
    await call("content_campaign", {"app": "demo", "campaign_id": 12})
    assert (backend.last().method, backend.last().path) == ("GET", f"{CAMPAIGNS}/12")


async def test_create_campaign(backend, call, call_error):
    await call("content_create_campaign", {"app": "demo", "title": "双十一", "scope": "all"})
    req = backend.last()
    assert (req.method, req.path) == ("POST", CAMPAIGNS)
    generated = req.json.pop("requestId")
    assert len(generated) == 32
    assert req.json == {"title": "双十一", "level": "info", "audience": {"scope": "all", "account": "available"}}

    await call("content_create_campaign", {"app": "demo", "title": "召回", "content": "回来看看", "scope": "filter",
                                           "active_within_days": 7, "min_level": 3, "keyword": "vip",
                                           "category": "activity", "level": "success", "action_type": "route",
                                           "action_target": "points", "action_label": "去看看",
                                           "image": "https://img/x.png", "scheduled_at": "2026-11-21T10:00:00+08:00",
                                           "expires_at": "2026-11-28T10:00:00+08:00", "metadata": {"src": "mcp"},
                                           "request_id": "camp-1"})
    assert backend.last().json == {
        "requestId": "camp-1", "category": "activity", "title": "召回", "content": "回来看看", "level": "success",
        "action": {"type": "route", "target": "points", "label": "去看看"}, "image": "https://img/x.png",
        "expiresAt": "2026-11-28T10:00:00+08:00", "metadata": {"src": "mcp"}, "scheduledAt": "2026-11-21T10:00:00+08:00",
        "audience": {"scope": "filter", "account": "available", "keyword": "vip", "activeWithinDays": 7, "minLevel": 3},
    }

    await call("content_create_campaign", {"app": "demo", "title": "t", "scope": "users", "user_ids": [1, 2], "account": "all"})
    assert backend.last().json["audience"] == {"scope": "users", "userIds": [1, 2], "account": "all"}

    count = len(backend.requests)
    await call_error("content_create_campaign", {"app": "demo", "title": "t"})
    await call_error("content_create_campaign", {"app": "demo", "title": "t", "scope": "users"})
    await call_error("content_create_campaign", {"app": "demo", "title": "t", "scope": "all", "inactive_days": 30})
    await call_error("content_create_campaign", {"app": "demo", "title": "t", "scope": "all", "level": "fatal"})
    assert len(backend.requests) == count


async def test_cancel_and_recall_campaign(backend, call):
    await call("content_cancel_campaign", {"app": "demo", "campaign_id": 12})
    assert (backend.last().method, backend.last().path) == ("POST", f"{CAMPAIGNS}/12/cancel")
    await call("content_recall_campaign", {"app": "demo", "campaign_id": 12})
    assert (backend.last().method, backend.last().path) == ("POST", f"{CAMPAIGNS}/12/recall")


async def test_campaign_tool_categories(mcp):
    tools = {tool.name: tool for tool in await mcp.list_tools()}

    def tags(name):
        return set((tools[name].meta or {}).get("fastmcp", {}).get("tags") or [])

    for name in ("content_campaign_catalog", "content_preview_audience", "content_campaigns", "content_campaign"):
        assert "read" in tags(name)
    assert {"write", "destructive"} <= tags("content_create_campaign")
    assert {"write", "destructive"} <= tags("content_recall_campaign")
    assert "write" in tags("content_cancel_campaign") and "destructive" not in tags("content_cancel_campaign")


async def test_announcements(backend, call):
    await call("content_announcements")
    assert backend.last().path == "/api/admin/system/announcements"
    await call("content_announcements", {"announcement_id": 4})
    assert backend.last().path == "/api/admin/system/announcements/4"
    await call("content_save_announcement", {"title": "t", "content": "c"})
    assert (backend.last().method, backend.last().json) == ("POST", {"title": "t", "content": "c", "pinned": False})
    await call("content_save_announcement", {"title": "t", "content": "c", "announcement_id": 4})
    assert (backend.last().method, backend.last().path) == ("PUT", "/api/admin/system/announcements/4")
    for action, method, suffix in (("publish", "POST", "/publish"), ("archive", "POST", "/archive"), ("delete", "DELETE", "")):
        await call("content_announcement_status", {"announcement_id": 4, "action": action})
        assert (backend.last().method, backend.last().path) == (method, "/api/admin/system/announcements/4" + suffix)
