"""出海网关模块：按名字编辑画布（规则链、命中去向、线路、策略组、订阅、分流、核心）、应用、核心运行态。"""

from __future__ import annotations

import copy

BASE = "/api/admin/system/egress"
KEPT = "__AEGIS_SECRET_KEPT__"


def view(nodes, edges, **extra):
    return {
        "graph": {"nodes": nodes, "edges": edges}, "origin": "draft", "draftRevision": 5, "dirty": True, "appliedVersion": 2,
        "active": True, "compile": {"issues": [], "summary": {"rules": 1}, "bindings": {"ep": "hk"}, "chain": ["r1"]},
        "catalog": {"defaultGateway": {"enabled": True, "defaultAction": "direct", "health": {"enabled": True}},
                    "defaultCore": {"settings": {"mode": "rule"}, "dnsEnabled": False}},
        "runtime": {"enabled": True, "source": "graph", "routed": 10, "routedUdp": 3, "proxied": 6, "direct": 4, "rejected": 0, "failed": 0,
                    "endpoints": [
                        {"name": "hk", "enabled": True, "healthy": True, "udp": True, "udpHealthy": False, "udpLastError": "UDP 无回应"},
                        {"name": "us", "enabled": True, "healthy": True, "udp": False},
                    ], "rules": []},
        "core": {"status": "running", "version": "v1.19.33"},
        **extra,
    }


def node(node_id, kind, data):
    return {"id": node_id, "type": kind, "position": {"x": 0, "y": 0}, "data": data}


NODES = [
    node("gw", "gateway", {"enabled": True, "defaultAction": "direct", "health": {"enabled": True}}),
    node("r1", "rule", {"name": "stripe", "action": "proxy", "match": {"domainSuffixes": ["stripe.com"]}}),
    node("ep", "endpoint", {"name": "hk", "protocol": "socks5h", "address": "10.0.0.1:1080", "password": KEPT}),
    node("g1", "m_group", {"group": {"name": "节点选择", "type": "select"}}),
    node("p1", "m_proxy", {"proxy": {"name": "香港 01", "type": "vless", "server": "hk.example.com", "port": 443, "uuid": KEPT}}),
]
EDGES = [
    {"id": "gw:rules->r1", "source": "gw", "sourceHandle": "rules", "target": "r1"},
    {"id": "r1:hit->ep", "source": "r1", "sourceHandle": "hit", "target": "ep"},
    {"id": "g1:members->p1", "source": "g1", "sourceHandle": "members", "target": "p1"},
]


def setup(backend, nodes=None, edges=None, **extra):
    backend.reply("GET", f"{BASE}/graph", view(copy.deepcopy(nodes or NODES), copy.deepcopy(edges or EDGES), **extra))


def saved(backend):
    req = backend.find("PUT", f"{BASE}/graph")
    graph = req.json["graph"]
    return req.json["revision"], {n["id"]: n for n in graph["nodes"]}, graph["edges"]


def chain_of(edges, start="gw"):
    out, port, current = [], "rules", start
    while True:
        nxt = [e["target"] for e in edges if e["source"] == current and e["sourceHandle"] == port]
        if not nxt:
            return out
        out.append(nxt[0])
        current, port = nxt[0], "next"


async def test_overview_and_nodes(backend, call):
    setup(backend)
    data = await call("egress_overview")
    assert data["counters"]["routedUdp"] == 3 and data["core"]["status"] == "running"
    assert [u["endpoint"] for u in data["unhealthy"]] == ["hk"] and data["unhealthy"][0]["udp"] == "down"
    listing = await call("egress_nodes")
    by_name = {item["name"]: item for item in listing["items"]}
    assert by_name["stripe"]["order"] == 1 and by_name["stripe"]["targets"] == ["hk"]
    assert by_name["节点选择"]["members"] == ["香港 01"] and by_name["hk"]["endpoint"] == "hk"
    only = await call("egress_nodes", {"node_type": "m_proxy"})
    assert [i["name"] for i in only["items"]] == ["香港 01"]
    graph = await call("egress_graph")
    assert "catalog" not in graph and graph["draftRevision"] == 5


async def test_add_rule_inserts_into_chain(backend, call, call_error):
    setup(backend)
    result = await call("egress_add_rule", {
        "name": "openai", "domain_suffixes": ["openai.com"], "networks": ["tcp"], "targets": ["节点选择", "hk"], "before": "stripe",
    })
    revision, nodes, edges = saved(backend)
    assert revision == 5 and result["saved"] and result["nodeId"] in nodes
    new = result["nodeId"]
    assert chain_of(edges) == [new, "r1"]
    hits = [e["target"] for e in edges if e["source"] == new and e["sourceHandle"] == "hit"]
    assert hits == ["g1", "ep"] and nodes[new]["data"]["order"] == ["g1", "ep"]
    assert nodes[new]["data"]["match"] == {"domainSuffixes": ["openai.com"], "networks": ["tcp"]}
    assert nodes["ep"]["data"]["password"] == KEPT, "没改的密钥占位要原样提交"
    assert "已经有" in await call_error("egress_add_rule", {"name": "stripe", "match_all": True, "action": "direct"})
    assert "targets" in await call_error("egress_add_rule", {"name": "x", "match_all": True})
    assert "匹配条件" in await call_error("egress_add_rule", {"name": "x", "action": "direct"})
    assert "没有叫" in await call_error("egress_add_rule", {"name": "x", "match_all": True, "targets": ["nope"]})


async def test_add_rule_to_mihomo_rules_creates_node(backend, call):
    setup(backend)
    await call("egress_add_rule", {"name": "quic", "networks": ["udp"], "ports": [443], "targets": ["mihomo_rules"], "position": 1})
    _, nodes, edges = saved(backend)
    rules_node = next(n for n in nodes.values() if n["type"] == "m_rules")
    new = next(n["id"] for n in nodes.values() if n["type"] == "rule" and n["data"]["name"] == "quic")
    assert chain_of(edges) == [new, "r1"]
    assert any(e["source"] == new and e["target"] == rules_node["id"] for e in edges)


async def test_update_and_move_rule(backend, call):
    nodes = NODES + [node("r2", "rule", {"name": "block", "action": "reject", "match": {"domains": ["bad.example"]}})]
    edges = EDGES + [{"id": "r1:next->r2", "source": "r1", "sourceHandle": "next", "target": "r2"}]
    setup(backend, nodes, edges)
    await call("egress_update_rule", {"rule": "stripe", "add_domain_suffixes": ["stripe.network"], "profiles": ["payment.*"], "targets": ["节点选择"]})
    _, saved_nodes, saved_edges = saved(backend)
    assert saved_nodes["r1"]["data"]["match"] == {"domainSuffixes": ["stripe.com", "stripe.network"], "profiles": ["payment.*"]}
    assert [e["target"] for e in saved_edges if e["source"] == "r1" and e["sourceHandle"] == "hit"] == ["g1"]
    await call("egress_move_rule", {"rule": "block", "position": 1})
    _, _, moved = saved(backend)
    assert chain_of(moved) == ["r2", "r1"]


async def test_remove_rule_relinks_chain(backend, call, call_error):
    nodes = NODES + [node("r2", "rule", {"name": "b", "match": {"matchAll": True}}), node("r3", "rule", {"name": "c", "match": {"matchAll": True}})]
    edges = EDGES + [{"id": "a", "source": "r1", "sourceHandle": "next", "target": "r2"}, {"id": "b", "source": "r2", "sourceHandle": "next", "target": "r3"}]
    setup(backend, nodes, edges)
    await call("egress_remove_node", {"node": "b"})
    _, saved_nodes, saved_edges = saved(backend)
    assert "r2" not in saved_nodes and chain_of(saved_edges) == ["r1", "r3"]
    assert "不能删除" in await call_error("egress_remove_node", {"node": "gateway"})


async def test_gateway_endpoint_group_provider(backend, call):
    setup(backend)
    await call("egress_set_gateway", {"fallback": ["节点选择"], "health": {"udpProbe": True, "udpProbeServer": "8.8.8.8:53"}})
    _, nodes, edges = saved(backend)
    assert nodes["gw"]["data"]["defaultAction"] == "proxy" and nodes["gw"]["data"]["health"] == {"enabled": True, "udpProbe": True, "udpProbeServer": "8.8.8.8:53"}
    assert [e["target"] for e in edges if e["source"] == "gw" and e["sourceHandle"] == "fallback"] == ["g1"]

    await call("egress_upsert_endpoint", {"name": "jump", "protocol": "ssh", "address": "10.0.0.9:22", "ssh": {"user": "root", "password": "pw"}})
    _, nodes, _ = saved(backend)
    jump = next(n for n in nodes.values() if n["type"] == "endpoint" and n["data"]["name"] == "jump")
    assert jump["data"]["ssh"] == {"user": "root", "password": "pw"}
    await call("egress_upsert_endpoint", {"name": "hk", "udp": False, "via": "hk"})
    _, nodes, edges = saved(backend)
    assert nodes["ep"]["data"]["udp"] is False and nodes["ep"]["data"]["password"] == KEPT

    await call("egress_upsert_group", {"name": "自动", "group_type": "url-test", "members": ["香港 01"], "options": {"interval": 300}, "builtins": ["DIRECT"]})
    _, nodes, edges = saved(backend)
    auto = next(n for n in nodes.values() if n["type"] == "m_group" and n["data"]["group"]["name"] == "自动")
    assert auto["data"]["group"] == {"name": "自动", "type": "url-test", "interval": 300} and auto["data"]["builtins"] == ["DIRECT"]
    assert [e["target"] for e in edges if e["source"] == auto["id"]] == ["p1"]

    await call("egress_upsert_provider", {"name": "机场", "url": "https://sub.example.com/x", "exclude_filter": "(?i)过期", "auto_group": "机场 自动", "groups": ["节点选择"]})
    _, nodes, edges = saved(backend)
    sub = next(n for n in nodes.values() if n["type"] == "m_provider")
    assert sub["data"]["provider"]["url"] == "https://sub.example.com/x" and sub["data"]["provider"]["exclude-filter"] == "(?i)过期"
    group_ids = {n["id"] for n in nodes.values() if n["type"] == "m_group"}
    assert {e["source"] for e in edges if e["target"] == sub["id"]} <= group_ids and len([e for e in edges if e["target"] == sub["id"]]) == 2


async def test_upsert_proxy_merges_and_joins_groups(backend, call, call_error):
    setup(backend)
    await call("egress_upsert_proxy", {"proxy": {"name": "香港 01", "port": 8443, "flow": None}, "groups": ["节点选择"]})
    _, nodes, _ = saved(backend)
    assert nodes["p1"]["data"]["proxy"]["port"] == 8443 and nodes["p1"]["data"]["proxy"]["uuid"] == KEPT
    await call("egress_upsert_proxy", {"proxy": {"name": "日本", "type": "hysteria2", "server": "jp.example.com", "port": 443, "password": "x"}, "dialer": "香港 01"})
    _, nodes, edges = saved(backend)
    jp = next(n for n in nodes.values() if n["type"] == "m_proxy" and n["data"]["proxy"]["name"] == "日本")
    assert any(e["source"] == jp["id"] and e["sourceHandle"] == "dialer" and e["target"] == "p1" for e in edges)
    assert "type" in await call_error("egress_upsert_proxy", {"proxy": {"name": "新"}})


async def test_mihomo_rules_and_core(backend, call):
    rules = node("m", "m_rules", {"rules": [{"id": "a", "type": "GEOIP", "payload": "CN", "target": "DIRECT"}, {"id": "b", "type": "MATCH", "target": "节点选择"}]})
    setup(backend, NODES + [rules])
    await call("egress_set_mihomo_rules", {"rules": ["GEOSITE,openai,节点选择", "AND,((DOMAIN-SUFFIX,a.com),(DST-PORT,443)),DIRECT"],
                                           "rule_providers": {"ai": {"type": "http", "behavior": "domain", "format": "mrs", "url": "https://x/ai.mrs"}}})
    _, nodes, _ = saved(backend)
    types = [(r["type"], r.get("payload")) for r in nodes["m"]["data"]["rules"]]
    assert types == [("GEOIP", "CN"), ("GEOSITE", "openai"), ("AND", "((DOMAIN-SUFFIX,a.com),(DST-PORT,443))"), ("MATCH", None)]
    assert "ai" in nodes["m"]["data"]["ruleProviders"]
    await call("egress_set_core", {"settings": {"logLevel": "debug"}, "dns_enabled": True, "dns": {"nameserver": ["https://1.1.1.1/dns-query"]}})
    _, nodes, _ = saved(backend)
    core = next(n for n in nodes.values() if n["type"] == "m_core")
    assert core["data"]["settings"] == {"mode": "rule", "logLevel": "debug"} and core["data"]["dnsEnabled"] is True
    assert core["data"]["dns"] == {"nameserver": ["https://1.1.1.1/dns-query"]}


async def test_connect_disconnect(backend, call, call_error):
    setup(backend)
    await call("egress_connect", {"source": "stripe", "target": "节点选择"})
    _, nodes, edges = saved(backend)
    assert {e["target"] for e in edges if e["source"] == "r1" and e["sourceHandle"] == "hit"} == {"ep", "g1"}
    await call("egress_disconnect", {"source": "节点选择", "target": "香港 01"})
    _, _, edges = saved(backend)
    assert not any(e["source"] == "g1" for e in edges)
    assert "规则链" in await call_error("egress_connect", {"source": "gateway", "target": "stripe", "port": "rules"})


async def test_validate_apply_and_errors(backend, call, call_error):
    setup(backend)
    backend.reply("POST", f"{BASE}/graph/validate", {"compile": {"issues": []}, "yaml": "proxies: []", "routes": {}})
    data = await call("egress_validate")
    assert data["hasYaml"] and "yaml" not in data and backend.last().json["graph"]["nodes"][0]["id"] == "gw"
    backend.reply("POST", f"{BASE}/graph/apply", {"appliedVersion": 3, "compile": {"summary": {}}, "core": {"status": "running"}})
    result = await call("egress_apply", {"note": "加 OpenAI"})
    req = backend.find("POST", f"{BASE}/graph/apply")
    assert result["appliedVersion"] == 3 and req.json["revision"] == 5 and req.json["note"] == "加 OpenAI"
    broken = view(copy.deepcopy(NODES), copy.deepcopy(EDGES))
    broken["compile"]["issues"] = [{"level": "error", "message": "策略组没有成员"}]
    backend.reply("GET", f"{BASE}/graph", broken)
    assert "策略组没有成员" in await call_error("egress_apply", {})


async def test_explain_test_probe(backend, call):
    await call("egress_explain", {"host": "api.stripe.com", "network": "udp", "profile": "payment.stripe"})
    assert backend.last().json == {"host": "api.stripe.com", "port": 443, "profile": "payment.stripe", "network": "udp"}
    await call("egress_test", {"network": "udp", "url": "8.8.8.8:53", "name": "google.com"})
    assert backend.last().json == {"url": "8.8.8.8:53", "network": "udp", "name": "google.com", "timeoutMs": 15000}
    backend.reply("POST", f"{BASE}/probe", {"results": [{"endpoint": "hk", "ok": True, "udpChecked": True, "udpOk": False}]})
    data = await call("egress_probe")
    assert (data["tcpOk"], data["udpChecked"], data["udpOk"]) == (1, 1, 0)


async def test_import_and_export(backend, call, call_error):
    setup(backend)
    imported = {"nodes": NODES + [node("p9", "m_proxy", {"proxy": {"name": "新节点", "type": "ss"}})], "edges": EDGES}
    backend.reply("POST", f"{BASE}/graph/import", {"graph": imported, "format": "uri", "added": {"proxies": 1}, "notes": [], "compile": {"issues": []}})
    data = await call("egress_import_config", {"content": "ss://abc"})
    req = backend.find("POST", f"{BASE}/graph/import")
    assert req.json["options"] == {"mode": "merge", "routeFallback": False} and req.json["content"] == "ss://abc"
    _, nodes, _ = saved(backend)
    assert data["saved"] and "p9" in nodes
    assert "只能给一个" in await call_error("egress_import_config", {"content": "a", "url": "https://x"})
    await call("egress_export_yaml")
    assert backend.last().json["includeSecrets"] is False


async def test_core_tools(backend, call):
    backend.reply("GET", f"{BASE}/mihomo/proxies", {
        "proxies": {
            "节点选择": {"name": "节点选择", "type": "Selector", "now": "香港 01", "all": ["香港 01", "DIRECT"]},
            "香港 01": {"name": "香港 01", "type": "Vless", "alive": True, "history": [{"delay": 120}]},
            "DIRECT": {"name": "DIRECT", "type": "Direct"},
            "GLOBAL": {"name": "GLOBAL", "type": "Selector", "all": []},
        },
        "providers": {"default": {"name": "default", "vehicleType": "Compatible"}, "机场": {"name": "机场", "vehicleType": "HTTP", "proxies": [{}, {}]}},
        "ruleProviders": {},
    })
    groups = await call("egress_core_proxies")
    assert groups["total"] == 1 and groups["items"][0]["members"][0] == {"name": "香港 01", "delay": 120}
    providers = await call("egress_core_proxies", {"kind": "providers"})
    assert providers["items"] == [{"name": "机场", "vehicleType": "HTTP", "proxies": 2}]
    backend.reply("POST", f"{BASE}/mihomo/delay", {"delays": {"a": 0, "b": 80, "c": 30}})
    delays = await call("egress_core_delay", {"name": "节点选择", "group": True})
    assert [d["name"] for d in delays["delays"]] == ["c", "b", "a"] and delays["available"] == 2
    await call("egress_core_select", {"group": "节点选择", "name": "DIRECT"})
    assert backend.last().json == {"group": "节点选择", "name": "DIRECT"}
    backend.reply("GET", f"{BASE}/mihomo/connections", {"connections": [
        {"id": "1", "metadata": {"host": "a.com", "destinationPort": 443, "network": "tcp"}, "chains": ["香港 01", "节点选择"], "download": 10, "start": "2026-10-30T00:00:00Z"},
        {"id": "2", "metadata": {"destinationIP": "8.8.8.8", "destinationPort": 53, "network": "udp"}, "chains": ["DIRECT"], "download": 99, "start": "2026-10-30T00:00:01Z"},
    ]})
    conns = await call("egress_core_connections", {"sort": "download"})
    assert [c["id"] for c in conns["items"]] == ["2", "1"] and conns["items"][1]["chains"] == ["节点选择", "香港 01"]
    backend.reply("GET", f"{BASE}/mihomo/logs", {"items": [{"seq": 1, "level": "info", "message": "a"}, {"seq": 2, "level": "fatal", "message": "boom"}]})
    logs = await call("egress_core_logs", {"level": "error"})
    assert [line["seq"] for line in logs["items"]] == [2] and logs["lastSeq"] == 2
    backend.reply("GET", f"{BASE}/mihomo/status", {"status": "running", "traffic": [{"up": 1, "down": 4}, {"up": 3, "down": 8}]})
    status = await call("egress_core_status")
    assert status["traffic"] == {"seconds": 2, "upAvg": 2, "upMax": 3, "downAvg": 6, "downMax": 8} and "samples" not in status
    await call("egress_core_close_connections", {})
    assert backend.last().json == {"id": ""}
