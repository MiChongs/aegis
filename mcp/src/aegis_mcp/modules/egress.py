"""出海网关：总览与运行态、画布（读、按名字增改节点与连线、校验、导入导出、应用、版本回滚）、路由解释与 TCP / UDP 自测、
托管的 mihomo 核心（安装、启停、策略组测速与切换、订阅与规则集、在途连接、日志、DNS）。

出海网关决定平台所有对外调用（支付、登录、存储、邮件、AI……）走直连、代理还是拒绝。配置是一张画布：
网关设置 →（规则链）规则 → 规则 …；规则「命中」、网关「未命中」连到网关线路 / mihomo 策略组 / mihomo 节点 / mihomo 分流；
线路「经由」上一跳；策略组「成员」连节点、策略组、订阅；节点与订阅「前置代理」连节点或策略组。
本模块的编辑工具（egress_add_rule、egress_upsert_group……）只改**草稿**、不影响线上；egress_apply 才让草稿生效，
生效前先用 egress_validate 看问题、egress_explain 核对某个域名会怎么走。

权限：全部接口限超级管理员（路由表的影响面等同防火墙）；写操作需要密钥授予全部权限，只读密钥只能用 GET 类工具。
读回的画布里密钥都是占位 __AEGIS_SECRET_KEPT__，原样提交即保持原值，提交空串即清空。
"""

from __future__ import annotations

import secrets
from typing import Annotated, Any, Literal

from fastmcp import FastMCP
from fastmcp.exceptions import ToolError
from pydantic import Field

from ..core import DESTRUCTIVE, GET, POST, PUT, READ, WRITE, body, q

NAMESPACE = "egress"
server = FastMCP("egress")

BASE = "/api/admin/system/egress"
SETTINGS = GET(BASE)
SETTINGS_RESET = POST(f"{BASE}/reset")
TEST = POST(f"{BASE}/test")
EXPLAIN = POST(f"{BASE}/explain")
PROBE = POST(f"{BASE}/probe")
GRAPH = GET(f"{BASE}/graph")
GRAPH_SAVE = PUT(f"{BASE}/graph")
GRAPH_VALIDATE = POST(f"{BASE}/graph/validate")
GRAPH_APPLY = POST(f"{BASE}/graph/apply")
GRAPH_DISCARD = POST(f"{BASE}/graph/discard")
GRAPH_IMPORT = POST(f"{BASE}/graph/import")
GRAPH_EXPORT = POST(f"{BASE}/graph/export")
HISTORY = GET(f"{BASE}/graph/history")
HISTORY_VERSION = GET(f"{BASE}/graph/history/:version")
ROLLBACK = POST(f"{BASE}/graph/rollback")
CORE_STATUS = GET(f"{BASE}/mihomo/status")
CORE_INSTALL = POST(f"{BASE}/mihomo/install")
CORE_RESTART = POST(f"{BASE}/mihomo/restart")
CORE_STOP = POST(f"{BASE}/mihomo/stop")
CORE_PROXIES = GET(f"{BASE}/mihomo/proxies")
CORE_DELAY = POST(f"{BASE}/mihomo/delay")
CORE_SELECT = POST(f"{BASE}/mihomo/select")
CORE_CONNECTIONS = GET(f"{BASE}/mihomo/connections")
CORE_CLOSE = POST(f"{BASE}/mihomo/connections/close")
CORE_PROVIDERS = POST(f"{BASE}/mihomo/providers")
CORE_RULES = GET(f"{BASE}/mihomo/rules")
CORE_LOGS = GET(f"{BASE}/mihomo/logs")
CORE_DNS = POST(f"{BASE}/mihomo/dns")
CORE_MAINTENANCE = POST(f"{BASE}/mihomo/maintenance")

SECRET_KEPT = "__AEGIS_SECRET_KEPT__"
Network = Literal["tcp", "udp"]
Action = Literal["proxy", "direct", "reject"]
Strategy = Literal["failover", "round_robin", "random", "weighted", "latency"]
NodeRef = Annotated[str, Field(min_length=1, description=(
    "节点：名字或节点 ID。名字按 规则名 / 网关线路名 / mihomo 策略组名 / mihomo 节点名 / 订阅名 查找；"
    "「mihomo 分流」写 mihomo_rules，网关设置写 gateway，核心设置写 mihomo_core"
))]
Targets = Annotated[list[str] | None, Field(description=(
    "命中后去哪些线路（按顺序即故障转移优先级）：网关线路名、mihomo 策略组名、mihomo 节点名，或 mihomo_rules（交给核心分流）。"
    "给了就整体替换原来的连线"
))]
Revision = Annotated[int | None, Field(description="草稿修订号（egress_graph 返回的 draftRevision）；留空自动取最新，并发编辑时填上可防止覆盖别人的改动")]

# ── 画布读写 ──

NODE_LABEL = {
    "gateway": "网关设置", "rule": "规则", "endpoint": "网关线路", "m_proxy": "mihomo 节点", "m_group": "策略组",
    "m_provider": "订阅", "m_rules": "mihomo 分流", "m_core": "mihomo 核心",
}
SPECIAL = {"gateway": "gateway", "mihomo_rules": "m_rules", "mihomo 分流": "m_rules", "rules": "m_rules", "mihomo_core": "m_core", "core": "m_core"}
ROUTE_TARGETS = {"endpoint", "m_group", "m_proxy", "m_rules"}
COLUMN = {"endpoint": 460, "m_group": 840, "m_rules": 840, "m_proxy": 1220, "m_provider": 1600, "m_core": -420}


def _name_of(node: dict[str, Any]) -> str:
    data = node.get("data") or {}
    kind = node.get("type")
    if kind == "m_proxy":
        return str((data.get("proxy") or {}).get("name") or "")
    if kind == "m_group":
        return str((data.get("group") or {}).get("name") or "")
    if kind in ("gateway", "m_rules", "m_core"):
        return NODE_LABEL[kind]
    return str(data.get("name") or "")


def _edge_id(source: str, port: str, target: str) -> str:
    return f"{source}:{port}->{target}"


class Draft:
    """读出来的草稿画布 + 按名字定位节点、改连线、维护规则链的小工具。改完调用 save()。"""

    def __init__(self, view: dict[str, Any]) -> None:
        self.view = view
        graph = view.get("graph") or {}
        self.nodes: list[dict[str, Any]] = list(graph.get("nodes") or [])
        self.edges: list[dict[str, Any]] = list(graph.get("edges") or [])
        self.viewport = graph.get("viewport")
        self.revision = int(view.get("draftRevision") or 0)

    # 查找
    def by_id(self, node_id: str) -> dict[str, Any] | None:
        return next((node for node in self.nodes if node.get("id") == node_id), None)

    def of_type(self, kind: str) -> list[dict[str, Any]]:
        return [node for node in self.nodes if node.get("type") == kind]

    def find(self, ref: str, kinds: set[str] | None = None) -> dict[str, Any] | None:
        ref = ref.strip()
        special = SPECIAL.get(ref.lower()) or SPECIAL.get(ref)
        if special and (kinds is None or special in kinds):
            found = self.of_type(special)
            return found[0] if found else None
        node = self.by_id(ref)
        if node and (kinds is None or node.get("type") in kinds):
            return node
        matches = [node for node in self.nodes if _name_of(node) == ref and (kinds is None or node.get("type") in kinds)]
        if len(matches) > 1:
            listing = "、".join(f"{NODE_LABEL.get(n['type'], n['type'])} {n['id']}" for n in matches)
            raise ToolError(f"名字「{ref}」对应多个节点（{listing}），请改用节点 ID")
        return matches[0] if matches else None

    def require(self, ref: str, kinds: set[str] | None = None) -> dict[str, Any]:
        node = self.find(ref, kinds)
        if node is None:
            wanted = "、".join(NODE_LABEL.get(k, k) for k in sorted(kinds or set())) or "节点"
            raise ToolError(f"画布上没有叫「{ref}」的{wanted}（egress_nodes 可列出全部节点）")
        return node

    def gateway(self) -> dict[str, Any]:
        found = self.of_type("gateway")
        if found:
            return found[0]
        node = {"id": "gateway", "type": "gateway", "position": {"x": 0, "y": 0},
                "data": dict((self.view.get("catalog") or {}).get("defaultGateway") or {"enabled": True, "defaultAction": "direct", "health": {"enabled": True, "passiveEnabled": True}})}
        self.nodes.insert(0, node)
        return node

    def singleton(self, kind: str, default: dict[str, Any]) -> dict[str, Any]:
        found = self.of_type(kind)
        if found:
            return found[0]
        return self.add(kind, default)

    # 增删
    def add(self, kind: str, data: dict[str, Any], position: dict[str, float] | None = None) -> dict[str, Any]:
        if position is None:
            same = [n for n in self.nodes if n.get("type") == kind]
            x = COLUMN.get(kind, 460)
            y = max((float((n.get("position") or {}).get("y", 0)) for n in same), default=-190.0) + 190
            position = {"x": x, "y": y}
        node = {"id": f"{kind}-{secrets.token_hex(4)}", "type": kind, "position": position, "data": data}
        self.nodes.append(node)
        return node

    def remove(self, node: dict[str, Any]) -> None:
        if node.get("type") == "rule":
            self._unchain(node["id"])
        self.nodes = [n for n in self.nodes if n.get("id") != node["id"]]
        self.edges = [e for e in self.edges if node["id"] not in (e.get("source"), e.get("target"))]

    # 连线
    def out(self, source: str, port: str) -> list[dict[str, Any]]:
        return [e for e in self.edges if e.get("source") == source and e.get("sourceHandle") == port]

    def link(self, source: str, port: str, target: str) -> None:
        if not any(e.get("source") == source and e.get("sourceHandle") == port and e.get("target") == target for e in self.edges):
            self.edges.append({"id": _edge_id(source, port, target), "source": source, "sourceHandle": port, "target": target})

    def unlink(self, source: str, port: str, target: str | None = None) -> None:
        self.edges = [e for e in self.edges if not (e.get("source") == source and e.get("sourceHandle") == port and (target is None or e.get("target") == target))]

    def set_targets(self, source: dict[str, Any], port: str, refs: list[str], kinds: set[str], order_key: str = "order") -> list[str]:
        """整体替换某个端口的连线，按 refs 的顺序写进节点的 order。返回目标节点 ID。"""
        ids = []
        for ref in refs:
            target = self.resolve_target(ref, kinds)
            if target["id"] not in ids:
                ids.append(target["id"])
        self.unlink(source["id"], port)
        for target_id in ids:
            self.link(source["id"], port, target_id)
        source.setdefault("data", {})[order_key] = ids
        return ids

    def resolve_target(self, ref: str, kinds: set[str]) -> dict[str, Any]:
        node = self.find(ref, kinds)
        if node is None and "m_rules" in kinds and SPECIAL.get(ref.strip().lower()) == "m_rules":
            node = self.add("m_rules", {"rules": [{"id": secrets.token_hex(3), "type": "MATCH", "target": "DIRECT"}], "ruleProviders": {}})
        if node is None:
            wanted = "、".join(NODE_LABEL.get(k, k) for k in sorted(kinds))
            raise ToolError(f"画布上没有叫「{ref}」的{wanted}（egress_nodes 可列出全部节点）")
        return node

    # 规则链
    def chain(self) -> list[str]:
        gateway = self.of_type("gateway")
        if not gateway:
            return []
        ids: list[str] = []
        edges = self.out(gateway[0]["id"], "rules")
        while edges and edges[0]["target"] not in ids:
            ids.append(edges[0]["target"])
            edges = self.out(ids[-1], "next")
        return ids

    def _relink_chain(self, ids: list[str]) -> None:
        """把规则链重接成 ids 的顺序。只动链上（旧链与新链）规则的「下一条」，没接入链的规则保持原样。"""
        gateway = self.gateway()
        touched = set(self.chain()) | set(ids)
        self.unlink(gateway["id"], "rules")
        for node_id in touched:
            self.unlink(node_id, "next")
        previous, port = gateway["id"], "rules"
        for node_id in ids:
            self.link(previous, port, node_id)
            previous, port = node_id, "next"

    def _unchain(self, node_id: str) -> None:
        ids = self.chain()
        if node_id in ids:
            ids.remove(node_id)
            self._relink_chain(ids)

    def place_rule(self, node_id: str, position: int | None, before: str | None, after: str | None) -> list[str]:
        ids = [i for i in self.chain() if i != node_id]
        if before:
            anchor = self.require(before, {"rule"})["id"]
            index = ids.index(anchor) if anchor in ids else len(ids)
        elif after:
            anchor = self.require(after, {"rule"})["id"]
            index = ids.index(anchor) + 1 if anchor in ids else len(ids)
        elif position is not None:
            index = max(0, min(position - 1, len(ids)))
        else:
            index = len(ids)
        ids.insert(index, node_id)
        self._relink_chain(ids)
        return ids

    def rule_slot(self) -> dict[str, float]:
        """新规则放在规则链最后一条的正下方（画布上可再用「整理排版」）。"""
        gateway = self.gateway()
        anchor = self.by_id(self.chain()[-1]) if self.chain() else gateway
        position = (anchor or {}).get("position") or {}
        return {"x": float(position.get("x", 0)), "y": float(position.get("y", 0)) + 230}

    def graph(self) -> dict[str, Any]:
        out: dict[str, Any] = {"nodes": self.nodes, "edges": self.edges}
        if self.viewport:
            out["viewport"] = self.viewport
        return out

    async def save(self, revision: int | None = None, changed: list[str] | None = None) -> dict[str, Any]:
        saved = await GRAPH_SAVE(body={"graph": self.graph(), "revision": self.revision if revision is None else revision})
        return _saved_summary(saved, changed or [])


async def _draft() -> Draft:
    view = await GRAPH()
    if not isinstance(view, dict):
        raise ToolError("读取出海网关画布失败")
    return Draft(view)


def _saved_summary(view: Any, changed: list[str]) -> dict[str, Any]:
    if not isinstance(view, dict):
        return {"saved": True}
    compile_view = view.get("compile") or {}
    issues = compile_view.get("issues") or []
    return {
        "saved": True,
        "draftRevision": view.get("draftRevision"),
        "dirty": view.get("dirty"),
        "changed": changed,
        "summary": compile_view.get("summary"),
        "errors": [i for i in issues if i.get("level") == "error"],
        "warnings": [i for i in issues if i.get("level") == "warning"],
        "next": "草稿已保存，线上未变。确认无误后用 egress_apply 生效。" if not any(i.get("level") == "error" for i in issues)
        else "草稿有错误，应用前需要先修好。",
    }


def _compact_node(node: dict[str, Any], draft: Draft, chain: list[str], bindings: dict[str, str]) -> dict[str, Any]:
    data = node.get("data") or {}
    kind = node.get("type")
    out: dict[str, Any] = {"id": node.get("id"), "type": kind, "kind": NODE_LABEL.get(kind, kind), "name": _name_of(node)}
    targets = lambda port: [_name_of(draft.by_id(e["target"]) or {}) or e["target"] for e in draft.out(node["id"], port)]  # noqa: E731
    if kind == "rule":
        out.update({
            "order": chain.index(node["id"]) + 1 if node["id"] in chain else None,
            "enabled": data.get("enabled", True), "action": data.get("action") or "proxy", "strategy": data.get("strategy"),
            "match": {k: v for k, v in (data.get("match") or {}).items() if v not in (None, [], False, "")},
            "targets": targets("hit"),
        })
    elif kind == "gateway":
        out.update({"enabled": data.get("enabled"), "defaultAction": data.get("defaultAction"), "fallback": targets("fallback"),
                    "health": data.get("health")})
    elif kind == "endpoint":
        out.update({"protocol": data.get("protocol"), "address": data.get("address"), "enabled": data.get("enabled", True),
                    "udp": data.get("udp", True), "via": targets("via"), "region": data.get("region")})
    elif kind == "m_proxy":
        proxy = data.get("proxy") or {}
        out.update({"proxyType": proxy.get("type"), "server": proxy.get("server"), "port": proxy.get("port"),
                    "network": proxy.get("network"), "udp": proxy.get("udp"), "dialer": targets("dialer")})
    elif kind == "m_group":
        group = data.get("group") or {}
        out.update({"groupType": group.get("type"), "members": targets("members"), "builtins": data.get("builtins") or [],
                    "options": {k: v for k, v in group.items() if k not in ("name", "type")}})
    elif kind == "m_provider":
        provider = data.get("provider") or {}
        out.update({"source": provider.get("type"), "url": "已保存" if provider.get("url") == SECRET_KEPT else provider.get("url"),
                    "interval": provider.get("interval"), "filter": provider.get("filter"), "dialer": targets("dialer")})
    elif kind == "m_rules":
        rules = data.get("rules") or []
        out.update({"rules": len(rules), "ruleProviders": sorted((data.get("ruleProviders") or {}).keys())})
    elif kind == "m_core":
        out.update({"settings": data.get("settings"), "dnsEnabled": data.get("dnsEnabled")})
    if node.get("id") in bindings:
        out["endpoint"] = bindings[node["id"]]
    if data.get("note"):
        out["note"] = data["note"]
    return out


# ── 总览与运行态 ──


@server.tool(**READ("egress"))
async def overview() -> Any:
    """出海网关一屏总览：是否启用、配置来源（画布第几版 / 旧版控制台 / 环境变量）、草稿是否未应用、规则线路策略组节点订阅数量、
    判定计数（走代理 / 直连 / 拒绝 / UDP / 选不出线路）、不健康的线路（含 UDP）、草稿里的错误与提醒数、mihomo 核心状态与速率。
    排查「为什么某个境外接口调不通」先看它，再用 egress_explain 看那个域名的路由。"""
    view = await GRAPH()
    if not isinstance(view, dict):
        return view
    runtime = view.get("runtime") or {}
    core = view.get("core") or {}
    issues = (view.get("compile") or {}).get("issues") or []
    unhealthy = [
        {"name": e.get("remark") if e.get("protocol") == "mihomo" else e.get("name"), "endpoint": e.get("name"),
         "tcp": "ok" if e.get("healthy") else "down", "udp": None if not e.get("udp") else ("ok" if e.get("udpHealthy") else "down"),
         "error": e.get("lastError") or e.get("udpLastError")}
        for e in runtime.get("endpoints") or []
        if e.get("enabled") and (not e.get("healthy") or (e.get("udp") and not e.get("udpHealthy")))
    ]
    return {
        "enabled": runtime.get("enabled"),
        "source": runtime.get("source"),
        "appliedVersion": view.get("appliedVersion"),
        "appliedAt": view.get("appliedAt"),
        "graphActive": view.get("active"),
        "draftDirty": view.get("dirty"),
        "draftRevision": view.get("draftRevision"),
        "defaultAction": runtime.get("defaultAction"),
        "summary": (view.get("compile") or {}).get("summary"),
        "counters": {k: runtime.get(k) for k in ("routed", "routedUdp", "proxied", "direct", "rejected", "failed")},
        "endpoints": len(runtime.get("endpoints") or []),
        "unhealthy": unhealthy,
        "draftErrors": sum(1 for i in issues if i.get("level") == "error"),
        "draftWarnings": sum(1 for i in issues if i.get("level") == "warning"),
        "core": {k: core.get(k) for k in ("status", "version", "binaryPresent", "lastError", "restarts", "proxies", "groups",
                                          "providers", "rules", "memoryInUse", "uploadSpeed", "downloadSpeed", "startedAt", "autoInstall")},
    }


@server.tool(**READ("egress"))
async def runtime(
    unhealthy_only: Annotated[bool, Field(description="只看不健康的线路（TCP 或 UDP 任一不健康）")] = False,
) -> Any:
    """网关运行态：每条线路的 TCP / UDP 健康、延迟、连接与失败次数、流量、UDP 会话与报文数、最近错误、所在代理链；每条规则的命中次数。
    计数是本进程启动以来的（多进程部署时只是收到请求的那个进程）。"""
    data = await SETTINGS()
    if not isinstance(data, dict):
        return data
    runtime_data = data.get("runtime") or {}
    endpoints = runtime_data.get("endpoints") or []
    if unhealthy_only:
        endpoints = [e for e in endpoints if e.get("enabled") and (not e.get("healthy") or (e.get("udp") and not e.get("udpHealthy")))]
    return {**{k: v for k, v in runtime_data.items() if k not in ("endpoints", "rules")}, "endpoints": endpoints, "rules": runtime_data.get("rules") or []}


@server.tool(**READ("egress"))
async def settings() -> Any:
    """当前生效的网关配置（编译后的路由表：端点、规则、健康检查、超时，密钥打码）与运行态、可选项目录。
    画布是配置的来源，改配置用画布工具；这里用来核对线上到底生效了什么。"""
    return await SETTINGS()


@server.tool(**READ("egress"))
async def graph(
    include_catalog: Annotated[bool, Field(description="附带画布目录（节点端口、mihomo 各协议字段表，体积很大；一般用 egress_catalog 按需取）")] = False,
    include_runtime: Annotated[bool, Field(description="附带网关运行态与核心状态")] = False,
) -> Any:
    """画布原样（nodes / edges，与控制台同形，密钥为占位）、来源（draft 草稿 / applied 已应用 / config 由当前配置生成）、
    草稿修订号、已应用版本、草稿是否未应用、编译结果（问题、概况、规则链顺序、节点 → 网关端点 / 核心名字的映射）。
    只想看结构用 egress_nodes 更省篇幅；改画布直接用 egress_add_rule / egress_upsert_* 这些按名字操作的工具。"""
    view = await GRAPH()
    if not isinstance(view, dict):
        return view
    out = {k: v for k, v in view.items() if k not in ("catalog", "runtime", "core")}
    if include_catalog:
        out["catalog"] = view.get("catalog")
    if include_runtime:
        out["runtime"], out["core"] = view.get("runtime"), view.get("core")
    return out


@server.tool(**READ("egress"))
async def nodes(
    node_type: Annotated[Literal["", "gateway", "rule", "endpoint", "m_proxy", "m_group", "m_provider", "m_rules", "m_core"],
                         Field(description="只看这类节点，留空为全部")] = "",
    keyword: Annotated[str, Field(description="按名字筛选（包含即可）")] = "",
) -> Any:
    """画布节点的精简清单：规则按链上顺序带匹配条件、动作与命中去向；线路带协议地址、UDP 开关与经由；策略组带类型与成员；
    节点带协议与服务器；订阅带来源与更新间隔；另有草稿修订号与问题。按名字操作的编辑工具认这里的 name。"""
    view = await GRAPH()
    if not isinstance(view, dict):
        return view
    draft = Draft(view)
    chain = draft.chain()
    bindings = (view.get("compile") or {}).get("bindings") or {}
    items = [_compact_node(node, draft, chain, bindings) for node in draft.nodes
             if (not node_type or node.get("type") == node_type) and (not keyword or keyword.lower() in _name_of(node).lower())]
    items.sort(key=lambda item: (["gateway", "rule", "endpoint", "m_group", "m_proxy", "m_provider", "m_rules", "m_core"].index(item["type"])
                                 if item["type"] in NODE_LABEL else 9, item.get("order") or 0))
    issues = (view.get("compile") or {}).get("issues") or []
    return {"origin": view.get("origin"), "draftRevision": view.get("draftRevision"), "dirty": view.get("dirty"),
            "items": items, "total": len(items), "issues": issues}


@server.tool(**READ("egress"))
async def catalog(
    section: Annotated[Literal["nodes", "protocols", "proxy_types", "group_types", "rule_types", "provider_fields", "rule_provider_fields",
                               "dns_fields", "profiles", "defaults", "all"],
                       Field(description="取哪一段：nodes 节点与端口 / protocols 网关协议与 UDP 承载 / proxy_types mihomo 节点协议字段 / group_types 策略组类型 / "
                                         "rule_types 分流规则类型 / provider_fields 订阅字段 / rule_provider_fields 规则集字段 / dns_fields / profiles 平台调用方 / defaults 默认值 / all")] = "nodes",
    proxy_type: Annotated[str, Field(description="section=proxy_types 时只看这一种协议（如 vless、hysteria2）")] = "",
) -> Any:
    """画布目录（按段取，避免一次返回过大）：节点类型与端口能连什么、网关协议与 UDP 承载方式、mihomo 各协议的字段（键、类型、选项、默认值、何时显示）、
    策略组类型、分流规则类型、订阅与规则集字段、DNS 字段、平台已知的调用方标识（规则的 profiles 条件）、新节点默认值、密钥占位与保留键。"""
    view = await GRAPH()
    info = (view or {}).get("catalog") or {}
    mihomo = info.get("mihomo") or {}
    sections = {
        "nodes": {"nodes": info.get("nodes"), "categories": info.get("categories")},
        "protocols": {"protocols": info.get("protocols"), "udpProtocols": info.get("udpProtocols"), "strategies": info.get("strategies"),
                      "actions": info.get("actions"), "shadowsocksMethods": info.get("shadowsocksMethods")},
        "proxy_types": [t for t in mihomo.get("proxyTypes") or [] if not proxy_type or t.get("type") == proxy_type],
        "group_types": mihomo.get("groupTypes"),
        "rule_types": {"ruleTypes": mihomo.get("ruleTypes"), "builtins": mihomo.get("builtins")},
        "provider_fields": mihomo.get("providerFields"),
        "rule_provider_fields": mihomo.get("ruleProviderFields"),
        "dns_fields": mihomo.get("dnsFields"),
        "profiles": info.get("profiles"),
        "defaults": {"defaultGateway": info.get("defaultGateway"), "defaultCore": info.get("defaultCore"), "secretKept": info.get("secretKept"),
                     "reservedKeys": mihomo.get("reservedKeys"), "secretKeys": mihomo.get("secretKeys"), "defaultTestUrl": mihomo.get("defaultTestUrl"),
                     "defaultProbeUrl": info.get("defaultProbeUrl")},
    }
    if section == "all":
        return sections
    result = sections[section]
    if section == "proxy_types" and proxy_type and not result:
        known = [t.get("type") for t in mihomo.get("proxyTypes") or []]
        raise ToolError(f"没有 {proxy_type} 这种协议的表单，可选：{', '.join(map(str, known))}（其它协议可直接写 mihomo 原生字段）")
    return result


@server.tool(**READ("egress"))
async def history() -> Any:
    """应用记录（最近 20 个版本）：版本号、时间、操作人、备注、当时的规模概况。回滚用 egress_rollback，查看某一版的画布用 egress_version。"""
    data = await HISTORY()
    items = (data or {}).get("items") if isinstance(data, dict) else data
    return {"items": items or [], "total": len(items or [])}


@server.tool(**READ("egress"))
async def version(version: Annotated[int, Field(gt=0, description="版本号（egress_history 里的 version）")]) -> Any:
    """某个已应用版本的画布（密钥打码），可与当前草稿对比，或交给 egress_save_graph 存成草稿再改。"""
    return await HISTORY_VERSION(version=version)


# ── 校验、解释、自测、探测 ──


@server.tool(**WRITE("egress", idempotent=True))
async def validate(
    graph: Annotated[dict[str, Any] | None, Field(description="要校验的画布 {nodes, edges}；留空校验当前草稿")] = None,
    include_yaml: Annotated[bool, Field(description="附带编译出的 mihomo 配置 YAML（密钥打码）")] = False,
) -> Any:
    """编译校验画布（与应用时同一套判据，不改任何东西）：错误与提醒、概况、编译出的网关路由表（规则顺序、每条规则去哪些线路、
    未命中去向、每条线路的协议地址与经由）、可选的核心 YAML。应用前必看。"""
    if graph is None:
        graph = (await _draft()).graph()
    result = await GRAPH_VALIDATE(body={"graph": graph})
    if isinstance(result, dict) and not include_yaml:
        result = {k: v for k, v in result.items() if k != "yaml"} | {"hasYaml": bool(result.get("yaml"))}
    return result


@server.tool(**WRITE("egress", idempotent=True))
async def explain(
    host: Annotated[str, Field(min_length=1, description="目标域名或 IP，如 api.stripe.com")],
    port: Annotated[int, Field(ge=1, le=65535, description="端口")] = 443,
    profile: Annotated[str, Field(description="模拟调用方标识（如 payment.stripe、auth.oauth），验证按调用方分流的规则")] = "",
    network: Annotated[Network, Field(description="tcp 或 udp（QUIC / DNS 等）")] = "tcp",
    scheme: Annotated[Literal["", "http", "https", "tcp"], Field(description="应用协议，留空按端口推断")] = "",
) -> Any:
    """「这个目标会怎么出去」：按线上生效的路由表逐条判定，返回命中的规则与原因、动作、选中的线路与代理链、逐条匹配过程。不发请求。"""
    return await EXPLAIN(body=body(host=host, port=port, profile=profile or None, network=network, scheme=scheme or None))


@server.tool(**WRITE("egress", idempotent=True))
async def test(
    url: Annotated[str, Field(description="TCP：要请求的 http(s) 地址，留空用健康探测地址（建议填查询出口 IP 的服务如 https://api.ipify.org）；"
                                          "UDP：DNS 服务器 host:port，留空用 UDP 探测服务器")] = "",
    network: Annotated[Network, Field(description="tcp 发一次 HTTP 请求；udp 经线路向 DNS 服务器查一次")] = "tcp",
    endpoint: Annotated[str, Field(description="指定网关端点名（egress_runtime 里的 name）绕过规则直测这条线路；留空按路由表")] = "",
    profile: Annotated[str, Field(description="模拟调用方标识")] = "",
    name: Annotated[str, Field(description="UDP 自测查询的域名")] = "",
    timeout_ms: Annotated[int, Field(ge=1000, le=60000, description="超时（毫秒）")] = 15000,
) -> Any:
    """真实发一次出站请求（与业务同一条代码路径）：TCP 返回状态码、延迟、截断的响应体；UDP 返回往返延迟与解析结果。
    附带走了哪条规则、哪条线路、代理链。"""
    return await TEST(body=body(url=url or None, network=network, endpoint=endpoint or None, profile=profile or None,
                                name=name or None, timeoutMs=timeout_ms))


@server.tool(**WRITE("egress", idempotent=True))
async def probe() -> Any:
    """立即探测全部线路并写回健康状态：TCP 经线路请求探测地址，能承载 UDP 的线路另经它发一次 DNS 查询。返回每条线路两项结果与延迟。"""
    data = await PROBE()
    results = (data or {}).get("results") if isinstance(data, dict) else data
    results = results or []
    return {
        "items": results, "total": len(results),
        "tcpOk": sum(1 for r in results if r.get("ok")),
        "udpChecked": sum(1 for r in results if r.get("udpChecked")),
        "udpOk": sum(1 for r in results if r.get("udpOk")),
    }


# ── 画布：整体保存、导入导出、应用、版本 ──


@server.tool(**WRITE("egress"))
async def save_graph(
    graph: Annotated[dict[str, Any], Field(description="整张画布 {nodes: [{id, type, position, data}], edges: [{id, source, sourceHandle, target}]}；节点 data 形态见 egress_catalog")],
    revision: Revision = None,
) -> Any:
    """整体替换草稿画布（不影响线上）。小改动优先用按名字操作的工具；这个用于从 egress_version 取回旧版再存为草稿、或批量生成画布。"""
    draft = await _draft()
    draft.nodes, draft.edges = list(graph.get("nodes") or []), list(graph.get("edges") or [])
    draft.viewport = graph.get("viewport")
    return await draft.save(revision, changed=["整张画布"])


@server.tool(**WRITE("egress"))
async def discard_draft() -> Any:
    """丢弃草稿的全部改动，回到已应用的版本（线上不变）。"""
    view = await GRAPH_DISCARD()
    return _saved_summary(view, ["丢弃草稿"])


@server.tool(**WRITE("egress"))
async def import_config(
    content: Annotated[str, Field(description="Clash / mihomo YAML 全文，或分享链接（ss / ssr / vmess / vless / trojan / hysteria / hy2 / tuic / anytls / socks / http，"
                                              "一行一条，也接受整段 base64 的订阅内容）；与 url 二选一")] = "",
    url: Annotated[str, Field(description="订阅地址：由服务器下载（带 clash.meta 标识、拒绝内网地址）后展开成节点；想让核心定时更新请改用 egress_upsert_provider")] = "",
    mode: Annotated[Literal["merge", "replace"], Field(description="merge 并入画布（重名加序号）/ replace 替换画布上的 mihomo 部分（网关规则按名字接回新节点）")] = "merge",
    route_fallback: Annotated[bool, Field(description="把网关「未命中」改成走代理并接到导入的分流或第一个策略组——影响全部出站，谨慎")] = False,
    save: Annotated[bool, Field(description="导入结果直接保存为草稿；false 只预览导入了什么")] = True,
) -> Any:
    """导入 mihomo / Clash 配置或分享链接到画布（入站、控制器、TUN 等托管键自动剔除，按进程匹配的规则去掉）。
    默认保存为草稿（线上不变），返回新增了多少节点、策略组、订阅、分流规则与说明。"""
    if bool(content.strip()) == bool(url.strip()):
        raise ToolError("content 与 url 需要且只能给一个")
    draft = await _draft()
    result = await _import_call(draft, content, url, mode, route_fallback)
    out = {"format": result.get("format"), "added": result.get("added"), "notes": result.get("notes") or []}
    if not save:
        issues = (result.get("compile") or {}).get("issues") or []
        return out | {"saved": False, "errors": [i for i in issues if i.get("level") == "error"]}
    imported = result.get("graph") or {}
    draft.nodes, draft.edges = list(imported.get("nodes") or []), list(imported.get("edges") or [])
    return out | await draft.save(changed=["导入"])


async def _import_call(draft: Draft, content: str, url: str, mode: str, route_fallback: bool) -> dict[str, Any]:
    result = await GRAPH_IMPORT(body=body(graph=draft.graph(), content=content or None, url=url or None,
                                          options={"mode": mode, "routeFallback": route_fallback}))
    if not isinstance(result, dict):
        raise ToolError("导入失败")
    return result


@server.tool(**WRITE("egress", idempotent=True))
async def export_yaml(
    include_secrets: Annotated[bool, Field(description="包含明文密钥（口令、UUID、订阅地址）；默认打码。导出明文前请确认用户确实需要")] = False,
) -> Any:
    """把当前草稿编译出的 mihomo 配置导出成 YAML（入站与控制器是示意值，运行时由网关注入）。"""
    draft = await _draft()
    return await GRAPH_EXPORT(body={"graph": draft.graph(), "includeSecrets": include_secrets})


@server.tool(**DESTRUCTIVE("egress"))
async def apply(
    note: Annotated[str, Field(max_length=200, description="版本备注：改了什么")] = "",
    revision: Revision = None,
) -> Any:
    """把当前草稿应用到线上：编译 → mihomo 核心校验并热重载（缺核心会先自动下载）→ 网关热替换路由表 → 记一个新版本。
    平台所有出站立即按新路由走；任何一步失败都停在旧配置上。应用前先 egress_validate 看错误、必要时 egress_explain 核对关键域名，
    并向用户复述影响后再调用。可用 egress_rollback 回到之前的版本。"""
    draft = await _draft()
    issues = (draft.view.get("compile") or {}).get("issues") or []
    errors = [i for i in issues if i.get("level") == "error"]
    if errors:
        raise ToolError("草稿有错误，不能应用：" + "；".join(i.get("message", "") for i in errors[:5]))
    view = await GRAPH_APPLY(body=body(graph=draft.graph(), revision=draft.revision if revision is None else revision, note=note or None))
    if not isinstance(view, dict):
        return view
    return {"applied": True, "appliedVersion": view.get("appliedVersion"), "appliedAt": view.get("appliedAt"),
            "summary": (view.get("compile") or {}).get("summary"), "core": (view.get("core") or {}).get("status")}


@server.tool(**DESTRUCTIVE("egress"))
async def rollback(version: Annotated[int, Field(gt=0, description="要回到的版本号（egress_history）")]) -> Any:
    """把某个历史版本重新应用到线上（记为新版本），mihomo 核心与网关路由表一起切换，当前草稿被它覆盖。"""
    view = await ROLLBACK(body={"version": version})
    if not isinstance(view, dict):
        return view
    return {"applied": True, "appliedVersion": view.get("appliedVersion"), "summary": (view.get("compile") or {}).get("summary")}


@server.tool(**DESTRUCTIVE("egress"))
async def reset_to_env() -> Any:
    """撤掉画布与旧版控制台的配置，网关回到 .env 的 EGRESS_* 设置，mihomo 核心停止（画布草稿保留，之后可再应用）。"""
    return await SETTINGS_RESET()


# ── 画布：按名字编辑（只改草稿） ──


def _match(
    *, domain_suffixes: list[str] | None, domains: list[str] | None, domain_keywords: list[str] | None, domain_regexps: list[str] | None,
    cidrs: list[str] | None, ports: list[int] | None, schemes: list[str] | None, profiles: list[str] | None, networks: list[str] | None,
    exclude_domain_suffixes: list[str] | None, match_all: bool | None, base: dict[str, Any] | None = None,
) -> dict[str, Any]:
    match = dict(base or {})
    for key, value in {
        "domainSuffixes": domain_suffixes, "domains": domains,
        "domainKeywords": domain_keywords, "domainRegexps": domain_regexps, "cidrs": cidrs, "ports": ports, "schemes": schemes,
        "profiles": profiles, "networks": networks, "excludeDomainSuffixes": exclude_domain_suffixes,
    }.items():
        if value is not None:
            if value:
                match[key] = value
            else:
                match.pop(key, None)
    if match_all is not None:
        if match_all:
            match["matchAll"] = True
        else:
            match.pop("matchAll", None)
    return match


ListArg = Annotated[list[str] | None, Field(description="给了就整体替换该条件；给空列表清空")]
Suffixes = Annotated[list[str] | None, Field(description="域名后缀（按标签边界：google.com 命中 www.google.com，不命中 notgoogle.com）；给空列表清空")]
Ports = Annotated[list[int] | None, Field(description="目标端口；给空列表清空")]
Profiles = Annotated[list[str] | None, Field(description="调用方标识（如 payment.stripe、payment.* 前缀通配；egress_catalog section=profiles 可查平台已知的）；给空列表清空")]
Networks = Annotated[list[Literal["tcp", "udp"]] | None, Field(description="传输层：只填 udp 可单独管 QUIC / DNS；留空两者都匹配；给空列表清空")]
Schemes = Annotated[list[Literal["http", "https", "tcp", "udp"]] | None, Field(description="应用协议；给空列表清空")]


@server.tool(**WRITE("egress"))
async def add_rule(
    name: Annotated[str, Field(min_length=1, max_length=64, description="规则名（全画布唯一）")],
    action: Annotated[Action, Field(description="命中后：proxy 走代理（需要 targets）/ direct 直连 / reject 拒绝")] = "proxy",
    targets: Targets = None,
    strategy: Annotated[Strategy | None, Field(description="多条线路时的选路策略，默认 failover 按顺序故障转移")] = None,
    domain_suffixes: Suffixes = None,
    domains: ListArg = None,
    domain_keywords: ListArg = None,
    domain_regexps: ListArg = None,
    cidrs: ListArg = None,
    ports: Ports = None,
    schemes: Schemes = None,
    profiles: Profiles = None,
    networks: Networks = None,
    exclude_domain_suffixes: ListArg = None,
    match_all: Annotated[bool | None, Field(description="无条件命中（兜底规则）")] = None,
    position: Annotated[int | None, Field(ge=1, description="插到规则链第几条（从 1 开始）；与 before / after 都不给时追加到最后")] = None,
    before: Annotated[str | None, Field(description="插到这条规则之前（规则名）")] = None,
    after: Annotated[str | None, Field(description="插到这条规则之后（规则名）")] = None,
    note: Annotated[str | None, Field(description="备注")] = None,
    enabled: Annotated[bool, Field(description="是否启用")] = True,
) -> Any:
    """在规则链上新增一条网关路由规则并保存草稿（线上不变）。规则自上而下匹配，同一条规则内不同条件「同时满足」、同一条件多项「任一」。
    例：Stripe 走香港线路 → name=stripe, domain_suffixes=[stripe.com], targets=[hk-socks]；QUIC 拒绝 → networks=[udp], ports=[443], action=reject。"""
    draft = await _draft()
    if draft.find(name, {"rule"}):
        raise ToolError(f"已经有叫「{name}」的规则，改它请用 egress_update_rule")
    match = _match(domain_suffixes=domain_suffixes, domains=domains, domain_keywords=domain_keywords, domain_regexps=domain_regexps,
                   cidrs=cidrs, ports=ports, schemes=schemes, profiles=profiles, networks=networks,
                   exclude_domain_suffixes=exclude_domain_suffixes, match_all=match_all)
    if not match:
        raise ToolError("至少要给一个匹配条件，或 match_all=true")
    if action == "proxy" and not targets:
        raise ToolError("action=proxy 时需要 targets（命中后去哪些线路）")
    data = body(name=name, action=action, strategy=strategy, match=match, note=note, enabled=None if enabled else False)
    node = draft.add("rule", data, draft.rule_slot())
    if targets:
        draft.set_targets(node, "hit", targets, ROUTE_TARGETS)
    chain = draft.place_rule(node["id"], position, before, after)
    result = await draft.save(changed=[f"新增规则「{name}」（第 {chain.index(node['id']) + 1} 条）"])
    return result | {"nodeId": node["id"]}


@server.tool(**WRITE("egress"))
async def update_rule(
    rule: Annotated[str, Field(min_length=1, description="规则名或节点 ID")],
    rename: Annotated[str | None, Field(description="改名")] = None,
    action: Annotated[Action | None, Field(description="命中后的动作")] = None,
    targets: Targets = None,
    strategy: Annotated[Strategy | None, Field(description="选路策略")] = None,
    domain_suffixes: Suffixes = None,
    domains: ListArg = None,
    domain_keywords: ListArg = None,
    domain_regexps: ListArg = None,
    cidrs: ListArg = None,
    ports: Ports = None,
    schemes: Schemes = None,
    profiles: Profiles = None,
    networks: Networks = None,
    exclude_domain_suffixes: ListArg = None,
    match_all: Annotated[bool | None, Field(description="无条件命中")] = None,
    add_domain_suffixes: Annotated[list[str] | None, Field(description="在原有域名后缀上追加（不替换）")] = None,
    remove_domain_suffixes: Annotated[list[str] | None, Field(description="从原有域名后缀里去掉")] = None,
    enabled: Annotated[bool | None, Field(description="启用 / 停用")] = None,
    position: Annotated[int | None, Field(ge=1, description="移到规则链第几条")] = None,
    note: Annotated[str | None, Field(description="备注；空串清空")] = None,
) -> Any:
    """修改一条规则并保存草稿：只改给了的字段，列表条件给了就整体替换（域名后缀也可用 add_ / remove_ 增减），targets 给了就整体替换去向。"""
    draft = await _draft()
    node = draft.require(rule, {"rule"})
    data = node.setdefault("data", {})
    if rename and rename != data.get("name"):
        if draft.find(rename, {"rule"}):
            raise ToolError(f"已经有叫「{rename}」的规则")
        data["name"] = rename
    match = _match(domain_suffixes=domain_suffixes, domains=domains, domain_keywords=domain_keywords, domain_regexps=domain_regexps,
                   cidrs=cidrs, ports=ports, schemes=schemes, profiles=profiles, networks=networks,
                   exclude_domain_suffixes=exclude_domain_suffixes, match_all=match_all, base=data.get("match"))
    if add_domain_suffixes or remove_domain_suffixes:
        current = [s for s in match.get("domainSuffixes") or [] if s not in (remove_domain_suffixes or [])]
        current += [s for s in add_domain_suffixes or [] if s not in current]
        if current:
            match["domainSuffixes"] = current
        else:
            match.pop("domainSuffixes", None)
    data["match"] = match
    if action:
        data["action"] = action
    if strategy:
        data["strategy"] = strategy
    if enabled is not None:
        data["enabled"] = enabled
    if note is not None:
        data["note"] = note
    if targets is not None:
        draft.set_targets(node, "hit", targets, ROUTE_TARGETS)
    if position is not None:
        draft.place_rule(node["id"], position, None, None)
    return await draft.save(changed=[f"修改规则「{data.get('name')}」"])


@server.tool(**WRITE("egress"))
async def move_rule(
    rule: Annotated[str, Field(min_length=1, description="规则名或节点 ID")],
    position: Annotated[int | None, Field(ge=1, description="移到第几条（从 1 开始）")] = None,
    before: Annotated[str | None, Field(description="移到这条规则之前")] = None,
    after: Annotated[str | None, Field(description="移到这条规则之后")] = None,
) -> Any:
    """调整规则在规则链上的顺序（也能把没接入链的规则接进来）并保存草稿。规则自上而下匹配，越靠前优先级越高。"""
    draft = await _draft()
    node = draft.require(rule, {"rule"})
    if position is None and not before and not after:
        raise ToolError("需要 position / before / after 之一")
    chain = draft.place_rule(node["id"], position, before, after)
    names = [_name_of(draft.by_id(i) or {}) for i in chain]
    return await draft.save(changed=["规则顺序：" + " → ".join(names)])


@server.tool(**WRITE("egress"))
async def set_gateway(
    enabled: Annotated[bool | None, Field(description="网关总开关；关闭后所有出站直连")] = None,
    default_action: Annotated[Action | None, Field(description="未命中任何规则时：direct 直连（白名单出海，推荐）/ proxy 走代理（需要 fallback）/ reject 拒绝")] = None,
    fallback: Annotated[list[str] | None, Field(description="未命中时去哪些线路（按顺序）；给了就整体替换，给了且 default_action 未给时自动改为 proxy")] = None,
    default_strategy: Annotated[Strategy | None, Field(description="未命中多条线路时的选路策略")] = None,
    health: Annotated[dict[str, Any] | None, Field(description=(
        "健康检查（只改给了的键）：enabled 主动探测、intervalSeconds、timeoutSeconds、failureThreshold、successThreshold、probeUrl、"
        "passiveEnabled 被动熔断、cooldownSeconds、allowUnhealthy 全部不健康仍尝试、udpProbe UDP 探测、udpProbeServer DNS 服务器、udpProbeName 查询域名"))] = None,
    timeouts: Annotated[dict[str, int] | None, Field(description="连接参数（毫秒）：dialTimeoutMs、tlsHandshakeTimeoutMs、responseHeaderTimeoutMs、idleConnTimeoutMs，以及 maxIdleConnsPerHost")] = None,
) -> Any:
    """修改「网关设置」节点并保存草稿：总开关、未命中时的动作与去向、健康检查（含 UDP 探测）、超时。"""
    draft = await _draft()
    node = draft.gateway()
    data = node.setdefault("data", {})
    if enabled is not None:
        data["enabled"] = enabled
    if fallback is not None:
        draft.set_targets(node, "fallback", fallback, ROUTE_TARGETS)
        if default_action is None and fallback:
            default_action = "proxy"
    if default_action:
        data["defaultAction"] = default_action
    if default_strategy:
        data["defaultStrategy"] = default_strategy
    if health:
        data["health"] = {**(data.get("health") or {}), **health}
    allowed = {"dialTimeoutMs", "tlsHandshakeTimeoutMs", "responseHeaderTimeoutMs", "idleConnTimeoutMs", "maxIdleConnsPerHost"}
    for key, value in (timeouts or {}).items():
        if key not in allowed:
            raise ToolError(f"不认识的连接参数 {key}，可选：{', '.join(sorted(allowed))}")
        data[key] = value
    return await draft.save(changed=["网关设置"])


@server.tool(**WRITE("egress"))
async def upsert_endpoint(
    name: Annotated[str, Field(pattern=r"^[A-Za-z0-9._-]{1,64}$", description="线路名（字母、数字、._-；不能以 mihomo. 开头），已存在则修改")],
    protocol: Annotated[Literal["http", "https", "socks5", "socks5h", "ssh", "trojan", "shadowsocks", "direct"] | None,
                        Field(description="协议；新建时必填。socks5h 由代理解析域名（出海推荐）")] = None,
    address: Annotated[str | None, Field(description="host:port（direct 不需要）")] = None,
    username: Annotated[str | None, Field(description="用户名（http / https / socks5）")] = None,
    password: Annotated[str | None, Field(description="密码（http / https / socks5 / trojan）；不改就不传")] = None,
    via: Annotated[str | None, Field(description="先经由这条线路连出去（代理链：跳板 → 落地）；空串取消经由")] = None,
    udp: Annotated[bool | None, Field(description="承载 UDP：false 只走 TCP（服务端没开 UDP 时）；HTTP / SSH 本就不支持")] = None,
    enabled: Annotated[bool | None, Field(description="启用 / 停用")] = None,
    region: Annotated[str | None, Field(description="地区标记，如 hk")] = None,
    weight: Annotated[int | None, Field(ge=1, description="加权随机时的权重")] = None,
    remark: Annotated[str | None, Field(description="备注")] = None,
    tls: Annotated[dict[str, Any] | None, Field(description="TLS（只改给了的键）：enabled、serverName、insecureSkipVerify、alpn、minVersion、caPem、clientCertPem、clientKeyPem")] = None,
    ssh: Annotated[dict[str, Any] | None, Field(description="SSH（只改给了的键）：user、password、privateKeyPem、passphrase、hostKeyFingerprint（SHA256:…）、keepAliveSeconds")] = None,
    shadowsocks: Annotated[dict[str, Any] | None, Field(description="Shadowsocks（只改给了的键）：method（aes-128-gcm / aes-256-gcm / chacha20-ietf-poly1305）、password")] = None,
    extra: Annotated[dict[str, Any] | None, Field(description="其它字段原样合并：dialTimeoutMs、probeUrl、httpForwardMode、headers")] = None,
) -> Any:
    """新建或修改一条网关内置协议的线路并保存草稿（HTTP / HTTPS / SOCKS5 / SSH / Trojan / Shadowsocks / 直连）。
    要用 VLESS、Hysteria2 等更多协议请用 egress_upsert_proxy 建 mihomo 节点。新线路要被规则的 targets 引用才会有流量。"""
    draft = await _draft()
    node = draft.find(name, {"endpoint"})
    created = node is None
    if created:
        if not protocol:
            raise ToolError("新建线路需要 protocol")
        node = draft.add("endpoint", {"name": name, "tls": {"enabled": False}, "ssh": {}, "shadowsocks": {}})
    data = node.setdefault("data", {})
    for key, value in body(protocol=protocol, address=address, username=username, password=password, enabled=enabled,
                           region=region, weight=weight, remark=remark).items():
        data[key] = value
    if udp is not None:
        if udp:
            data.pop("udp", None)
        else:
            data["udp"] = False
    for key, value in (("tls", tls), ("ssh", ssh), ("shadowsocks", shadowsocks)):
        if value:
            data[key] = {**(data.get(key) or {}), **value}
    data.update(extra or {})
    if via is not None:
        draft.unlink(node["id"], "via")
        if via:
            target = draft.require(via, {"endpoint"})
            draft.link(node["id"], "via", target["id"])
    result = await draft.save(changed=[("新增" if created else "修改") + f"线路「{name}」"])
    return result | {"nodeId": node["id"], "endpoint": name}


@server.tool(**WRITE("egress"))
async def upsert_proxy(
    proxy: Annotated[dict[str, Any], Field(description=(
        "mihomo 节点的原生字段（与 mihomo 配置 proxies 下一项相同），必须有 name；新建还需 type 与 server / port。例："
        "{name: 香港 01, type: vless, server: hk.example.com, port: 443, uuid: …, tls: true, flow: xtls-rprx-vision, "
        "reality-opts: {public-key: …, short-id: …}, servername: www.apple.com, client-fingerprint: chrome}。"
        "修改时只合并给了的键；值为 null 删除该键。各协议字段见 egress_catalog section=proxy_types。分享链接请用 egress_import_config"))],
    dialer: Annotated[str | None, Field(description="前置代理：先经这个节点或策略组连出去（dialer-proxy）；空串取消")] = None,
    groups: Annotated[list[str] | None, Field(description="顺带加入这些策略组的成员（追加到末尾）")] = None,
    note: Annotated[str | None, Field(description="备注")] = None,
) -> Any:
    """新建或修改一个 mihomo 节点（支持 mihomo 的全部协议）并保存草稿，可顺带加入策略组。"""
    name = str(proxy.get("name") or "").strip()
    if not name:
        raise ToolError("proxy 需要 name")
    draft = await _draft()
    node = draft.find(name, {"m_proxy"})
    created = node is None
    if created:
        if not proxy.get("type"):
            raise ToolError("新建节点需要 type（如 vless、hysteria2、trojan、ss）")
        node = draft.add("m_proxy", {"proxy": {}})
    current = dict((node.get("data") or {}).get("proxy") or {})
    for key, value in proxy.items():
        if value is None:
            current.pop(key, None)
        else:
            current[key] = value
    node["data"]["proxy"] = current
    if note is not None:
        node["data"]["note"] = note
    if dialer is not None:
        draft.unlink(node["id"], "dialer")
        if dialer:
            draft.link(node["id"], "dialer", draft.require(dialer, {"m_proxy", "m_group"})["id"])
    for group_ref in groups or []:
        group = draft.require(group_ref, {"m_group"})
        draft.link(group["id"], "members", node["id"])
        order = group["data"].setdefault("order", [])
        if node["id"] not in order:
            order.append(node["id"])
    result = await draft.save(changed=[("新增" if created else "修改") + f"节点「{name}」"])
    return result | {"nodeId": node["id"]}


@server.tool(**WRITE("egress"))
async def upsert_group(
    name: Annotated[str, Field(min_length=1, description="策略组名，已存在则修改")],
    group_type: Annotated[Literal["select", "url-test", "fallback", "load-balance", "relay"] | None,
                          Field(description="select 手动选择 / url-test 自动测速 / fallback 故障转移 / load-balance 负载均衡 / relay 链式中继；新建默认 select")] = None,
    members: Annotated[list[str] | None, Field(description="成员（按顺序）：mihomo 节点名、其它策略组名、订阅名；给了就整体替换")] = None,
    add_members: Annotated[list[str] | None, Field(description="追加成员（不替换）")] = None,
    remove_members: Annotated[list[str] | None, Field(description="去掉成员")] = None,
    builtins: Annotated[list[Literal["DIRECT", "REJECT", "REJECT-DROP", "PASS", "COMPATIBLE"]] | None, Field(description="附加内置出站；给了就整体替换")] = None,
    builtins_first: Annotated[bool | None, Field(description="内置出站排在成员之前")] = None,
    options: Annotated[dict[str, Any] | None, Field(description=(
        "其它 mihomo 字段（只合并给了的键，null 删除）：url、interval、timeout、tolerance、lazy、max-failed-times、expected-status、strategy（load-balance）、"
        "filter / exclude-filter（正则筛订阅节点）、exclude-type、include-all-proxies、include-all-providers、disable-udp、hidden、icon"))] = None,
    note: Annotated[str | None, Field(description="备注")] = None,
) -> Any:
    """新建或修改一个 mihomo 策略组并保存草稿：类型、成员（节点 / 策略组 / 订阅，按顺序）、内置出站、测速与筛选参数。
    策略组要被网关规则的 targets 引用（或作为别的组的成员）才会有流量。"""
    draft = await _draft()
    node = draft.find(name, {"m_group"})
    created = node is None
    if created:
        node = draft.add("m_group", {"group": {"name": name, "type": group_type or "select"}})
    data = node["data"]
    group = dict(data.get("group") or {})
    if group_type:
        group["type"] = group_type
    for key, value in (options or {}).items():
        if key in ("name", "type", "proxies", "use"):
            continue
        if value is None:
            group.pop(key, None)
        else:
            group[key] = value
    data["group"] = group
    kinds = {"m_proxy", "m_group", "m_provider"}
    if members is not None:
        draft.set_targets(node, "members", members, kinds)
    current = [e["target"] for e in draft.out(node["id"], "members")]
    for ref in add_members or []:
        target = draft.require(ref, kinds)
        if target["id"] == node["id"]:
            raise ToolError("策略组不能把自己加为成员")
        draft.link(node["id"], "members", target["id"])
        if target["id"] not in current:
            current.append(target["id"])
    for ref in remove_members or []:
        target = draft.require(ref, kinds)
        draft.unlink(node["id"], "members", target["id"])
        current = [i for i in current if i != target["id"]]
    if add_members or remove_members:
        order = [i for i in data.get("order") or [] if i in current]
        data["order"] = order + [i for i in current if i not in order]
    if builtins is not None:
        data["builtins"] = builtins
    if builtins_first is not None:
        data["builtinsFirst"] = builtins_first
    if note is not None:
        data["note"] = note
    result = await draft.save(changed=[("新增" if created else "修改") + f"策略组「{name}」"])
    return result | {"nodeId": node["id"]}


@server.tool(**WRITE("egress"))
async def upsert_provider(
    name: Annotated[str, Field(min_length=1, description="订阅名，已存在则修改")],
    url: Annotated[str | None, Field(description="订阅地址（http 来源）；按密钥保存，读回为占位")] = None,
    source: Annotated[Literal["http", "file", "inline"] | None, Field(description="来源，新建默认 http")] = None,
    interval: Annotated[int | None, Field(ge=0, description="更新间隔（秒），新建默认 3600")] = None,
    filter: Annotated[str | None, Field(description="只保留名字匹配这个正则的节点，如 (?i)港|hk")] = None,  # noqa: A002
    exclude_filter: Annotated[str | None, Field(description="排除名字匹配这个正则的节点，如 (?i)过期|剩余|官网")] = None,
    health_check: Annotated[dict[str, Any] | None, Field(description="健康检查（只改给了的键）：enable、url、interval、timeout、lazy")] = None,
    override: Annotated[dict[str, Any] | None, Field(description="覆盖订阅节点的参数（只改给了的键）：additional-prefix、additional-suffix、udp、skip-cert-verify、ip-version、tfo")] = None,
    options: Annotated[dict[str, Any] | None, Field(description="其它 mihomo 字段原样合并（path、proxy 下载经由、header、size-limit、payload、exclude-type），null 删除")] = None,
    dialer: Annotated[str | None, Field(description="订阅节点的前置代理（节点或策略组名，写进 override.dialer-proxy）；空串取消")] = None,
    groups: Annotated[list[str] | None, Field(description="顺带作为这些策略组的成员")] = None,
    auto_group: Annotated[str | None, Field(description="新建一个 url-test 自动测速组（给组名）并把这个订阅设为它的成员")] = None,
) -> Any:
    """新建或修改一个 mihomo 订阅（proxy-provider，核心定时拉取，支持筛选、改名与健康检查）并保存草稿，
    可顺带加入已有策略组、或新建一个自动测速组。机场订阅推荐用它而不是一次性导入。"""
    draft = await _draft()
    node = draft.find(name, {"m_provider"})
    created = node is None
    if created:
        node = draft.add("m_provider", {"name": name, "provider": {"type": source or "http", "interval": 3600,
                                                                    "health-check": {"enable": True, "url": "https://www.gstatic.com/generate_204", "interval": 600, "lazy": True}}})
        if not url and (source or "http") == "http":
            raise ToolError("新建 http 订阅需要 url")
    provider = dict(node["data"].get("provider") or {})
    for key, value in body(url=url, type=source, interval=interval, filter=filter, **{"exclude-filter": exclude_filter}).items():
        provider[key] = value
    if health_check:
        provider["health-check"] = {**(provider.get("health-check") or {}), **health_check}
    if override:
        provider["override"] = {**(provider.get("override") or {}), **override}
    for key, value in (options or {}).items():
        if value is None:
            provider.pop(key, None)
        else:
            provider[key] = value
    node["data"]["provider"] = provider
    if dialer is not None:
        draft.unlink(node["id"], "dialer")
        if dialer:
            draft.link(node["id"], "dialer", draft.require(dialer, {"m_proxy", "m_group"})["id"])
    changed = [("新增" if created else "修改") + f"订阅「{name}」"]
    for group_ref in groups or []:
        group = draft.require(group_ref, {"m_group"})
        draft.link(group["id"], "members", node["id"])
        group["data"].setdefault("order", []).append(node["id"])
    if auto_group:
        if draft.find(auto_group, {"m_group", "m_proxy"}):
            raise ToolError(f"已经有叫「{auto_group}」的策略组或节点")
        group = draft.add("m_group", {"group": {"name": auto_group, "type": "url-test", "url": "https://www.gstatic.com/generate_204",
                                                "interval": 300, "tolerance": 50, "lazy": True}, "order": [node["id"]]})
        draft.link(group["id"], "members", node["id"])
        changed.append(f"新增自动测速组「{auto_group}」")
    result = await draft.save(changed=changed)
    return result | {"nodeId": node["id"]}


@server.tool(**WRITE("egress"))
async def set_mihomo_rules(
    rules: Annotated[list[str] | None, Field(description=(
        "分流规则，一行一条 mihomo 写法：TYPE,内容,目标[,no-resolve]，如 GEOSITE,openai,节点选择 / GEOIP,CN,DIRECT,no-resolve / "
        "RULE-SET,ai,节点选择 / AND,((DOMAIN-SUFFIX,a.com),(DST-PORT,443)),节点选择 / MATCH,节点选择。目标是策略组名、节点名或内置出站"))] = None,
    mode: Annotated[Literal["replace", "prepend", "append"], Field(description="replace 整表替换 / prepend 插到最前 / append 插到兜底（MATCH）之前")] = "append",
    rule_providers: Annotated[dict[str, dict[str, Any] | None] | None, Field(description=(
        "规则集（名字 → mihomo 原生字段，值为 null 删除）：{ai: {type: http, behavior: domain, format: mrs, url: …, interval: 86400}}"))] = None,
) -> Any:
    """修改「mihomo 分流」表并保存草稿（没有就新建）：整表替换或插入规则、增删规则集。只有网关规则连到 mihomo_rules 的流量按这张表走。"""
    draft = await _draft()
    node = draft.singleton("m_rules", {"rules": [], "ruleProviders": {}})
    data = node["data"]
    current = list(data.get("rules") or [])
    parsed = [_parse_rule(line) for line in rules or [] if line.strip() and not line.strip().startswith("#")]
    if mode == "replace" and rules is not None:
        current = parsed
    elif mode == "prepend":
        current = parsed + current
    else:
        match_at = next((i for i, rule in enumerate(current) if rule.get("type") == "MATCH"), len(current))
        incoming_match = [r for r in parsed if r["type"] == "MATCH"]
        normal = [r for r in parsed if r["type"] != "MATCH"]
        current = current[:match_at] + normal + (incoming_match or current[match_at:])
    data["rules"] = current
    providers = dict(data.get("ruleProviders") or {})
    for key, value in (rule_providers or {}).items():
        if value is None:
            providers.pop(key, None)
        else:
            providers[key] = {**(providers.get(key) or {}), **value}
    data["ruleProviders"] = providers
    return await draft.save(changed=[f"mihomo 分流：{len(current)} 条规则、{len(providers)} 个规则集"])


def _parse_rule(raw: str) -> dict[str, Any]:
    parts, depth, start = [], 0, 0
    for i, char in enumerate(raw):
        if char == "(":
            depth += 1
        elif char == ")":
            depth -= 1
        elif char == "," and depth == 0:
            parts.append(raw[start:i].strip())
            start = i + 1
    parts.append(raw[start:].strip())
    kind = parts[0].upper()
    if kind == "MATCH":
        if len(parts) < 2:
            raise ToolError(f"规则「{raw}」缺少目标")
        return {"id": secrets.token_hex(3), "type": kind, "target": parts[1]}
    if len(parts) < 3:
        raise ToolError(f"规则「{raw}」格式应为 TYPE,内容,目标[,参数]")
    return {"id": secrets.token_hex(3), "type": kind, "payload": parts[1], "target": parts[2], "params": parts[3:]}


@server.tool(**WRITE("egress"))
async def set_core(
    settings: Annotated[dict[str, Any] | None, Field(description=(
        "核心全局设置（只改给了的键）：mode（rule / global / direct）、logLevel、ipv6、unifiedDelay、tcpConcurrent、globalClientFingerprint、"
        "keepAliveIdle、keepAliveInterval、geodataMode、geoAutoUpdate、geoUpdateInterval、geoxUrl {geoip, geosite, mmdb, asn}、globalUa"))] = None,
    dns_enabled: Annotated[bool | None, Field(description="启用核心 DNS；关闭则用系统 DNS")] = None,
    dns: Annotated[dict[str, Any] | None, Field(description="dns 段 mihomo 原生字段（只合并给了的键，null 删除）：nameserver、default-nameserver、proxy-server-nameserver、fallback、nameserver-policy、respect-rules……")] = None,
    extra: Annotated[dict[str, Any] | None, Field(description="其它顶层键（hosts、sniffer、ntp、experimental……，null 删除）；入站、控制器、TUN 由网关托管，写了会被忽略")] = None,
) -> Any:
    """修改「mihomo 核心」节点并保存草稿（没有就新建）：运行模式、日志级别、Geo 数据库、DNS 与其它顶层键。"""
    draft = await _draft()
    defaults = (draft.view.get("catalog") or {}).get("defaultCore") or {"settings": {"mode": "rule", "logLevel": "info"}, "dnsEnabled": False}
    node = draft.singleton("m_core", dict(defaults))
    data = node["data"]
    if settings:
        data["settings"] = {**(data.get("settings") or {}), **settings}
    if dns_enabled is not None:
        data["dnsEnabled"] = dns_enabled
    for key, value in (("dns", dns), ("extra", extra)):
        if value:
            merged = dict(data.get(key) or {})
            for k, v in value.items():
                if v is None:
                    merged.pop(k, None)
                else:
                    merged[k] = v
            data[key] = merged
    if dns_enabled and not data.get("dns"):
        data["dns"] = {"enable": True}
    return await draft.save(changed=["mihomo 核心设置"])


@server.tool(**WRITE("egress"))
async def connect(
    source: NodeRef,
    target: NodeRef,
    port: Annotated[Literal["hit", "fallback", "via", "members", "dialer", "next", "rules"] | None, Field(
        description="从哪个端口连：规则 hit / 网关 fallback / 线路 via / 策略组 members / 节点与订阅 dialer；留空按源节点类型推断")] = None,
) -> Any:
    """在两个节点之间加一条连线并保存草稿（规则命中去向、网关未命中去向、线路经由、策略组成员、前置代理）。
    规则链顺序请用 egress_move_rule。单线端口（经由、前置代理）会替换原来的那条。"""
    draft = await _draft()
    src = draft.require(source)
    dst = draft.require(target)
    inferred = {"rule": "hit", "gateway": "fallback", "endpoint": "via", "m_group": "members", "m_proxy": "dialer", "m_provider": "dialer"}
    port = port or inferred.get(src["type"])
    if not port:
        raise ToolError(f"{NODE_LABEL.get(src['type'], src['type'])}没有输出端口")
    if port in ("rules", "next"):
        raise ToolError("规则链请用 egress_move_rule 调整")
    if port in ("via", "dialer"):
        draft.unlink(src["id"], port)
    draft.link(src["id"], port, dst["id"])
    if port in ("hit", "fallback", "members"):
        order = src["data"].setdefault("order", [])
        if dst["id"] not in order:
            order.append(dst["id"])
    if port == "fallback" and (src["data"].get("defaultAction") or "direct") != "proxy":
        src["data"]["defaultAction"] = "proxy"
    if port == "hit" and src["data"].get("action") in ("direct", "reject"):
        src["data"]["action"] = "proxy"
    return await draft.save(changed=[f"连线 {_name_of(src)} -[{port}]-> {_name_of(dst)}"])


@server.tool(**WRITE("egress"))
async def disconnect(
    source: NodeRef,
    target: Annotated[str | None, Field(description="去掉连到这个节点的线；留空去掉该端口的全部连线")] = None,
    port: Annotated[Literal["hit", "fallback", "via", "members", "dialer"] | None, Field(description="端口；留空按源节点类型推断")] = None,
) -> Any:
    """去掉连线并保存草稿。"""
    draft = await _draft()
    src = draft.require(source)
    inferred = {"rule": "hit", "gateway": "fallback", "endpoint": "via", "m_group": "members", "m_proxy": "dialer", "m_provider": "dialer"}
    port = port or inferred.get(src["type"])
    if not port:
        raise ToolError(f"{NODE_LABEL.get(src['type'], src['type'])}没有输出端口")
    target_id = draft.require(target)["id"] if target else None
    before = len(draft.edges)
    draft.unlink(src["id"], port, target_id)
    if len(draft.edges) == before:
        raise ToolError("没有这样的连线")
    if target_id and isinstance(src["data"].get("order"), list):
        src["data"]["order"] = [i for i in src["data"]["order"] if i != target_id]
    return await draft.save(changed=[f"去掉 {_name_of(src)} 的 {port} 连线"])


@server.tool(**WRITE("egress"))
async def remove_node(node: NodeRef) -> Any:
    """从草稿画布删除一个节点及其连线（规则会从规则链摘出、前后自动接上）。只改草稿，应用前线上不变。"""
    draft = await _draft()
    target = draft.require(node)
    if target["type"] == "gateway":
        raise ToolError("网关设置不能删除（要关掉网关用 egress_set_gateway enabled=false）")
    name = _name_of(target)
    draft.remove(target)
    return await draft.save(changed=[f"删除{NODE_LABEL.get(target['type'], '节点')}「{name}」"])


# ── mihomo 核心 ──


def _speed(samples: list[dict[str, Any]]) -> dict[str, Any]:
    if not samples:
        return {}
    ups = [s.get("up") or 0 for s in samples]
    downs = [s.get("down") or 0 for s in samples]
    return {"seconds": len(samples), "upAvg": sum(ups) // len(ups), "upMax": max(ups), "downAvg": sum(downs) // len(downs), "downMax": max(downs)}


@server.tool(**READ("egress", "mihomo"))
async def core_status(include_samples: Annotated[bool, Field(description="附带最近 5 分钟每秒的流量采样")] = False) -> Any:
    """mihomo 核心状态：运行状态、版本、PID、启动与配置载入时间、配置摘要、节点 / 策略组 / 订阅 / 规则数、重启次数与最近错误、
    程序路径与是否已安装、是否自动安装、实时速率与内存，以及最近 5 分钟的平均 / 峰值速率。"""
    data = await CORE_STATUS()
    if not isinstance(data, dict):
        return data
    samples = data.get("traffic") or []
    out = {k: v for k, v in data.items() if k != "traffic"} | {"traffic": _speed(samples)}
    if include_samples:
        out["samples"] = samples
    return out


@server.tool(**READ("egress", "mihomo"))
async def core_proxies(
    kind: Annotated[Literal["groups", "proxies", "providers", "rule_providers", "all"], Field(description="看哪部分")] = "groups",
    keyword: Annotated[str, Field(description="按名字筛选")] = "",
) -> Any:
    """核心里的运行态：策略组（类型、当前选中、是否手动固定、成员与各自最近延迟）、节点（类型、是否存活、最近延迟、来自哪个订阅）、
    订阅（节点数、更新时间、流量用量与到期）、规则集（条数、更新时间）。延迟 0 表示超时或未测。"""
    data = await CORE_PROXIES()
    if not isinstance(data, dict):
        return data
    proxies = data.get("proxies") or {}
    delay = lambda info: ((info.get("history") or [{}])[-1] or {}).get("delay", 0) if info.get("history") else 0  # noqa: E731
    kw = keyword.lower()
    group_types = {"Selector", "URLTest", "Fallback", "LoadBalance", "Relay"}
    groups = [
        {"name": n, "type": i.get("type"), "now": i.get("now"), "fixed": i.get("fixed") or None,
         "members": [{"name": m, "delay": delay(proxies.get(m) or {})} for m in i.get("all") or []]}
        for n, i in proxies.items() if i.get("type") in group_types and n != "GLOBAL" and (not kw or kw in n.lower())
    ]
    nodes = [
        {"name": n, "type": i.get("type"), "alive": i.get("alive"), "delay": delay(i), "udp": i.get("udp"), "provider": i.get("provider-name")}
        for n, i in proxies.items() if i.get("type") not in group_types and (not kw or kw in n.lower())
    ]
    providers = [
        {k: v for k, v in p.items() if k != "proxies"} | {"proxies": len(p.get("proxies") or [])}
        for p in (data.get("providers") or {}).values() if p.get("vehicleType") != "Compatible" and (not kw or kw in str(p.get("name", "")).lower())
    ]
    rule_providers = [p for p in (data.get("ruleProviders") or {}).values() if not kw or kw in str(p.get("name", "")).lower()]
    parts = {"groups": groups, "proxies": nodes, "providers": providers, "rule_providers": rule_providers}
    if kind == "all":
        return parts
    return {"items": parts[kind], "total": len(parts[kind])}


@server.tool(**READ("egress", "mihomo"))
async def core_connections(
    keyword: Annotated[str, Field(description="按目标、线路链或规则筛选")] = "",
    sort: Annotated[Literal["start", "download", "upload"], Field(description="排序：start 最新 / download 下行最多 / upload 上行最多")] = "start",
    limit: Annotated[int, Field(ge=1, le=500, description="最多返回多少条")] = 50,
) -> Any:
    """经 mihomo 的在途连接：目标（域名或 IP:端口）、网络、经过的线路链、命中的核心规则、上下行字节、开始时间；附累计流量与内存。"""
    data = await CORE_CONNECTIONS()
    if not isinstance(data, dict):
        return data
    items = []
    for c in data.get("connections") or []:
        meta = c.get("metadata") or {}
        host = f"{meta.get('host') or meta.get('sniffHost') or meta.get('destinationIP') or ''}:{meta.get('destinationPort') or ''}"
        item = {"id": c.get("id"), "target": host, "network": meta.get("network"), "chains": list(reversed(c.get("chains") or [])),
                "rule": f"{c.get('rule') or ''} {c.get('rulePayload') or ''}".strip(), "upload": c.get("upload"), "download": c.get("download"), "start": c.get("start")}
        if keyword and keyword.lower() not in f"{item['target']} {' '.join(item['chains'])} {item['rule']}".lower():
            continue
        items.append(item)
    key = {"start": lambda x: x.get("start") or "", "download": lambda x: x.get("download") or 0, "upload": lambda x: x.get("upload") or 0}[sort]
    items.sort(key=key, reverse=True)
    return {"total": len(items), "items": items[:limit], "uploadTotal": data.get("uploadTotal"), "downloadTotal": data.get("downloadTotal"), "memory": data.get("memory")}


@server.tool(**READ("egress", "mihomo"))
async def core_rules(keyword: Annotated[str, Field(description="按类型、内容或目标筛选")] = "") -> Any:
    """核心当前生效的分流规则（序号、类型、内容、目标、规则集条数、命中次数）。只有「交给 mihomo 分流」的流量按它们走。"""
    data = await CORE_RULES()
    items = (data or {}).get("items") if isinstance(data, dict) else data
    items = [r for r in items or [] if not keyword or keyword.lower() in f"{r.get('type')} {r.get('payload')} {r.get('proxy')}".lower()]
    return {"items": items, "total": len(items)}


@server.tool(**READ("egress", "mihomo"))
async def core_logs(
    level: Annotated[Literal["", "error", "warning", "info", "debug"], Field(description="只看这个级别（error 含 fatal），留空全部")] = "",
    keyword: Annotated[str, Field(description="按内容筛选")] = "",
    after: Annotated[int, Field(ge=0, description="只返回序号大于它的（增量拉取：上次结果的 lastSeq）")] = 0,
    limit: Annotated[int, Field(ge=1, le=2000, description="最多返回多少行（取最新的）")] = 200,
) -> Any:
    """核心日志（最近 2000 行）：核心自己的输出，以及网关托管核心时记下的事件（source=aegis：启动、热重载、安装、自动拉起、端口冲突）。"""
    data = await CORE_LOGS(query=q(after=after or None, limit=2000))
    items = (data or {}).get("items") if isinstance(data, dict) else data
    items = [
        line for line in items or []
        if (not level or line.get("level") == level or (level == "error" and line.get("level") == "fatal"))
        and (not keyword or keyword.lower() in str(line.get("message", "")).lower())
    ][-limit:]
    return {"items": items, "total": len(items), "lastSeq": items[-1]["seq"] if items else after}


@server.tool(**WRITE("egress", "mihomo", idempotent=True))
async def core_delay(
    name: Annotated[str, Field(min_length=1, description="节点或策略组名")],
    group: Annotated[bool, Field(description="测策略组内全部成员（否则只测这一个，或策略组当前选中的那个）")] = False,
    url: Annotated[str, Field(description="测试地址，留空用默认 generate_204")] = "",
    timeout_ms: Annotated[int, Field(ge=500, le=30000, description="超时（毫秒）")] = 5000,
) -> Any:
    """经核心测延迟（毫秒，0 为超时）。结果会写进核心的延迟历史，url-test / fallback 组据此选路。"""
    data = await CORE_DELAY(body=body(name=name, group=group, url=url or None, timeoutMs=timeout_ms))
    delays = (data or {}).get("delays") if isinstance(data, dict) else data
    delays = delays or {}
    ranked = sorted(delays.items(), key=lambda kv: (kv[1] == 0, kv[1]))
    return {"delays": [{"name": n, "delay": d} for n, d in ranked], "available": sum(1 for _, d in ranked if d), "total": len(ranked)}


@server.tool(**WRITE("egress", "mihomo", idempotent=True))
async def core_dns(
    name: Annotated[str, Field(min_length=1, description="要解析的域名")],
    qtype: Annotated[Literal["A", "AAAA", "CNAME", "TXT", "MX", "NS"], Field(description="记录类型")] = "A",
) -> Any:
    """经 mihomo 的 DNS 模块解析（核心 DNS 关闭时走系统解析），用来排查节点域名解析或 DNS 污染。"""
    return await CORE_DNS(body={"name": name, "type": qtype})


@server.tool(**WRITE("egress", "mihomo"))
async def core_select(
    group: Annotated[str, Field(min_length=1, description="策略组名（select / url-test / fallback）")],
    name: Annotated[str, Field(description="切到这个成员；留空表示取消手动固定、恢复自动选择（url-test / fallback）")] = "",
) -> Any:
    """切换策略组当前使用的节点，立即生效、不需要应用画布（核心会记住选择，重启后保留）。"""
    await CORE_SELECT(body={"group": group, "name": name})
    return {"ok": True, "group": group, "now": name or "（恢复自动选择）"}


@server.tool(**WRITE("egress", "mihomo"))
async def core_provider(
    name: Annotated[str, Field(min_length=1, description="订阅名或规则集名")],
    action: Annotated[Literal["update", "healthcheck"], Field(description="update 立即重新拉取 / healthcheck 对订阅节点做一次健康检查")] = "update",
    kind: Annotated[Literal["proxy", "rule"], Field(description="proxy 订阅 / rule 规则集（只能 update）")] = "proxy",
) -> Any:
    """立即更新订阅或规则集，或对订阅里的节点做一次健康检查。"""
    if kind == "rule" and action != "update":
        raise ToolError("规则集只能 update")
    await CORE_PROVIDERS(body={"kind": kind, "name": name, "action": action})
    return {"ok": True}


@server.tool(**WRITE("egress", "mihomo"))
async def core_maintenance(action: Annotated[Literal["geo", "flush-dns"], Field(description="geo 重新下载 Geo 数据库 / flush-dns 清空 DNS 缓存")]) -> Any:
    """核心维护：重新下载 GeoIP / GeoSite 数据库，或清空 DNS 缓存。"""
    await CORE_MAINTENANCE(body={"action": action})
    return {"ok": True}


@server.tool(**WRITE("egress", "mihomo"))
async def core_install(version: Annotated[str, Field(description="版本，如 v1.19.33；留空为最新正式版")] = "") -> Any:
    """安装或升级 mihomo 核心：从 GitHub 发布页下载本机平台的包（镜像优先）并校验 sha256，装好后已有配置会自动重启核心。
    升级会让核心重启一次，在途连接中断。"""
    return await CORE_INSTALL(body={"version": version})


@server.tool(**DESTRUCTIVE("egress", "mihomo"))
async def core_restart() -> Any:
    """重启 mihomo 核心（配置不变；核心没在跑时按已应用的画布拉起）。经核心的在途连接会中断。"""
    await CORE_RESTART()
    return {"ok": True}


@server.tool(**DESTRUCTIVE("egress", "mihomo"))
async def core_stop() -> Any:
    """停止 mihomo 核心，直到下一次应用或重启。期间所有 mihomo 线路不可用，规则会按健康检查切到其它线路或失败。"""
    await CORE_STOP()
    return {"ok": True}


@server.tool(**DESTRUCTIVE("egress", "mihomo"))
async def core_close_connections(connection_id: Annotated[str, Field(description="连接 ID（egress_core_connections）；留空关闭全部")] = "") -> Any:
    """关闭经 mihomo 的一条或全部在途连接，调用方会收到连接错误并按各自策略重试。"""
    await CORE_CLOSE(body={"id": connection_id})
    return {"ok": True, "closed": connection_id or "全部"}
