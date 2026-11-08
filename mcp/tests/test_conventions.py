"""全体工具的约定检查：标签、注解、文档、接口声明都对得上后端。"""

from __future__ import annotations

import re
from pathlib import Path

import pytest

from aegis_mcp.core.route import REGISTRY
from aegis_mcp.server import discover_modules

GOLDEN = Path(__file__).resolve().parents[2] / "internal" / "transport" / "http" / "testdata" / "routes.golden"
CJK = re.compile(r"[一-鿿]")


async def test_every_tool_follows_conventions(mcp):
    tools = await mcp.list_tools()
    assert tools, "一个工具都没有注册"
    names = [tool.name for tool in tools]
    assert len(names) == len(set(names)), "工具名重复"
    problems: list[str] = []
    for tool in tools:
        tags = set((tool.meta or {}).get("fastmcp", {}).get("tags") or [])
        description = (tool.description or "").strip()
        if not description or not CJK.search(description.splitlines()[0]):
            problems.append(f"{tool.name}：缺少中文说明")
        if ("read" in tags) == ("write" in tags):
            problems.append(f"{tool.name}：必须恰好带 read 或 write 标签之一，实际 {sorted(tags)}")
        annotations = tool.annotations
        if annotations is None:
            problems.append(f"{tool.name}：缺少 annotations（用 READ / WRITE / DESTRUCTIVE）")
            continue
        if "read" in tags and annotations.read_only_hint is not True:
            problems.append(f"{tool.name}：read 工具的 read_only_hint 必须为 True")
        if "write" in tags and annotations.read_only_hint is not False:
            problems.append(f"{tool.name}：write 工具的 read_only_hint 必须为 False")
        if "destructive" in tags and annotations.destructive_hint is not True:
            problems.append(f"{tool.name}：destructive 工具的 destructive_hint 必须为 True")
        if not re.fullmatch(r"[a-z][a-z0-9_]*", tool.name):
            problems.append(f"{tool.name}：工具名只能是小写字母、数字与下划线")
        for param, schema in (tool.input_schema.get("properties") or {}).items():
            if not re.fullmatch(r"[a-z][a-z0-9_]*", param):
                problems.append(f"{tool.name}.{param}：参数名用 snake_case")
    assert not problems, "\n".join(problems)


def test_every_module_is_well_formed():
    seen: dict[str, str] = {}
    for module in discover_modules():
        name = module.__name__.rsplit(".", 1)[-1]
        assert module.__doc__ and CJK.search(module.__doc__), f"模块 {name} 缺少中文模块说明"
        namespace = module.NAMESPACE
        assert namespace not in seen, f"模块 {name} 与 {seen.get(namespace)} 的 NAMESPACE 重复：{namespace!r}"
        seen[namespace] = name


@pytest.mark.skipif(not GOLDEN.exists(), reason="不在 Aegis 仓库里，找不到 routes.golden")
def test_every_declared_route_exists_in_backend():
    registered = set()
    # 通配路由（如风控平台的 /api/admin/system/risk/*path 整段转发到 risk-admin）：前缀对上即可
    wildcards: list[tuple[str, str]] = []
    for line in GOLDEN.read_text(encoding="utf-8").splitlines():
        parts = line.split("\t")
        if len(parts) >= 2:
            registered.add((parts[0], parts[1]))
            if "/*" in parts[1]:
                wildcards.append((parts[0], parts[1].split("/*", 1)[0] + "/"))

    def known(method: str, template: str) -> bool:
        return (method, template) in registered or any(m == method and template.startswith(p) for m, p in wildcards)

    missing = sorted(f"{method} {template}" for method, template in REGISTRY if not known(method, template))
    assert not missing, "这些接口在后端路由表里不存在（写错路径或参数名？）：\n" + "\n".join(missing)


async def test_read_only_mode_hides_write_tools(backend):
    from fastmcp import Client

    from aegis_mcp.core.config import Settings
    from aegis_mcp.server import build_server

    srv = build_server(Settings(base_url="https://aegis.test", api_key="aegis_mcp_" + "t" * 43), read_only=True)
    async with Client(srv) as client:
        tools = await client.list_tools()
    tags = [set((t.meta or {}).get("fastmcp", {}).get("tags") or []) for t in tools]
    assert tools and all("write" not in item for item in tags)


PERMISSIONS = Path(__file__).resolve().parents[2] / "internal" / "authz" / "testdata" / "route_permissions.json"


@pytest.mark.skipif(not PERMISSIONS.exists(), reason="不在 Aegis 仓库里")
def test_read_tools_only_hit_read_routes():
    """只读工具引用的接口必须是 GET，或规则表判为 *:read 的 POST 查询——否则只读密钥调不了、--read-only 模式下会露出用不了的工具。"""
    import ast
    import inspect
    import json

    from aegis_mcp.core.route import Route

    read_routes = {
        (row["method"], row["path"])
        for row in json.loads(PERMISSIONS.read_text(encoding="utf-8"))
        if not row.get("unmapped") and str(row.get("permission", "")).endswith(":read")
    }
    problems: list[str] = []
    for module in discover_modules():
        tree = ast.parse(inspect.getsource(module))
        routes = {name: value for name, value in vars(module).items() if isinstance(value, Route)}
        route_maps = {name: value for name, value in vars(module).items()
                      if isinstance(value, dict) and value and all(isinstance(v, Route) for v in value.values())}
        for node in ast.walk(tree):
            if not isinstance(node, ast.AsyncFunctionDef):
                continue
            is_read = any(
                isinstance(dec, ast.Call) and any(
                    isinstance(kw.value, ast.Call) and getattr(kw.value.func, "id", "") == "READ"
                    for kw in dec.keywords if kw.arg is None
                )
                for dec in node.decorator_list
            )
            if not is_read:
                continue
            used: list[Route] = []
            for sub in ast.walk(node):
                if isinstance(sub, ast.Name) and sub.id in routes:
                    used.append(routes[sub.id])
                if isinstance(sub, ast.Name) and sub.id in route_maps:
                    used.extend(route_maps[sub.id].values())
            for route in used:
                if route.method in ("GET", "HEAD") or (route.method, route.template) in read_routes:
                    continue
                problems.append(f"{module.__name__}.{node.name}：只读工具调用了 {route.method} {route.template}（不是读权限点），应改用 WRITE(..., idempotent=True)")
    assert not problems, "\n".join(problems)


async def test_tool_search_mode(backend):
    from fastmcp import Client

    from aegis_mcp.core.config import Settings
    from aegis_mcp.server import build_server

    srv = build_server(Settings(base_url="https://aegis.test", api_key="aegis_mcp_" + "t" * 43), tool_search=True)
    async with Client(srv) as client:
        names = {t.name for t in await client.list_tools()}
        assert names == {"search_tools", "run_tool", "whoami", "list_endpoints", "describe_endpoint", "call_api"}
        found = await client.call_tool("search_tools", {"query": "vip grant"})
        assert "vip_grant" in "".join(getattr(b, "text", "") for b in found.content)
