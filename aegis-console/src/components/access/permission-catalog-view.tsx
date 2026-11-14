"use client";

import { useMemo, useState, type ReactNode } from "react";
import {
  ChevronRight,
  ChevronsDownUp,
  ChevronsUpDown,
  History,
  KeyRound,
  Network,
  Search,
  ShieldAlert,
  X,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import {
  useAccessRolesQuery,
  usePermissionCatalogQuery,
  usePermissionRoutesQuery,
} from "@/lib/access-hooks";
import type { PermissionNode } from "@/lib/api/access";
import { usePermissionChecker } from "@/lib/permissions";
import { cn } from "@/lib/utils";
import {
  CodeText,
  DangerTag,
  KindTag,
  Pane,
  ScopeTag,
  NodeIcon,
} from "./access-kit";
import {
  METHOD_TONE,
  buildTreeIndex,
  visibleKeys,
  type TreeIndex,
} from "./tree-model";

/**
 * 权限目录：整棵权限树的只读浏览（目录 / 菜单 / 操作 + 权限字符 + 作用域 + 接口数），
 * 选中一个操作看它覆盖的接口与持有它的角色。用于回答「这个按钮背后是什么权限、谁有」。
 */
export function PermissionCatalogView() {
  const catalogQuery = usePermissionCatalogQuery();
  const can = usePermissionChecker();
  const rolesQuery = useAccessRolesQuery(can("system:role:read"));
  const index = useMemo(
    () => buildTreeIndex(catalogQuery.data),
    [catalogQuery.data],
  );
  const [keyword, setKeyword] = useState("");
  const [onlyDanger, setOnlyDanger] = useState(false);
  const [expanded, setExpanded] = useState<Set<string>>(() => new Set());
  const [selected, setSelected] = useState<string | null>(null);

  const visible = useMemo(
    () => visibleKeys(index, { keyword, onlyDanger }, () => false),
    [index, keyword, onlyDanger],
  );
  const filtering = visible !== null;

  const stats = useMemo(() => {
    let dirs = 0;
    let menus = 0;
    let danger = 0;
    let routes = 0;
    for (const key of index.order) {
      const node = index.entries.get(key)!.node;
      if (node.kind === "dir") dirs++;
      else if (node.kind === "menu") menus++;
      else {
        if (node.danger) danger++;
        routes += node.routes ?? 0;
      }
    }
    return {
      dirs,
      menus,
      actions: index.codes.length,
      danger,
      routes,
      legacy: Object.keys(index.legacyNames).length,
    };
  }, [index]);

  if (catalogQuery.isLoading) {
    return <Skeleton className="h-[32rem] rounded-2xl" />;
  }

  const toggle = (key: string) =>
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });

  const render = (nodes: PermissionNode[]): ReactNode =>
    nodes.map((node) => {
      if (visible && !visible.has(node.key)) return null;
      const entry = index.entries.get(node.key)!;
      const open = filtering || expanded.has(node.key);
      const iconName = node.icon;
      const isAction = node.kind === "action";
      return (
        <div key={node.key}>
          <div
            onClick={() =>
              isAction ? setSelected(node.code ?? null) : toggle(node.key)
            }
            className={cn(
              "grid cursor-pointer grid-cols-[minmax(0,1fr)_3.5rem] items-center gap-3 pr-4 transition-colors hover:bg-accent/50 md:grid-cols-[minmax(0,1fr)_3.5rem_minmax(0,14rem)_3.5rem_4rem]",
              node.kind === "dir" ? "h-10 font-semibold" : "h-9",
              isAction && selected === node.code && "bg-primary/8",
            )}
          >
            <div
              className="flex min-w-0 items-center gap-1.5"
              style={{ paddingLeft: 10 + entry.depth * 22 }}
            >
              <span className="grid size-5 shrink-0 place-items-center text-muted-foreground">
                {!isAction && (
                  <ChevronRight
                    className={cn(
                      "size-3.5 transition-transform duration-200",
                      open && "rotate-90",
                    )}
                  />
                )}
              </span>
              {!isAction && (
                <NodeIcon
                  name={iconName}
                  className={cn(
                    "size-3.5 shrink-0",
                    node.kind === "dir"
                      ? "text-primary"
                      : "text-muted-foreground",
                  )}
                />
              )}
              <span className="truncate text-sm">{node.name}</span>
              {!isAction && (
                <span className="shrink-0 text-[11px] text-muted-foreground tabular-nums">
                  {entry.leaves.length}
                </span>
              )}
              {node.danger && <DangerTag className="ml-1" />}
            </div>
            <KindTag kind={node.kind} />
            <div className="hidden min-w-0 md:block">
              {isAction && (
                <CodeText className="block truncate">{node.code}</CodeText>
              )}
            </div>
            <div className="hidden md:block">
              {isAction && <ScopeTag scope={node.scope} />}
            </div>
            <div className="hidden text-right text-[11px] text-muted-foreground tabular-nums md:block">
              {isAction ? node.routes || "业务" : ""}
            </div>
          </div>
          {!isAction && open && render(node.children ?? [])}
        </div>
      );
    });

  return (
    <div className="flex min-w-0 flex-col gap-4">
      <section
        className="grid grid-cols-3 gap-px overflow-hidden rounded-2xl border bg-border sm:grid-cols-6"
        style={{ boxShadow: "var(--shadow-soft)" }}
      >
        {[
          { label: "目录", value: stats.dirs },
          { label: "菜单", value: stats.menus },
          { label: "操作", value: stats.actions },
          { label: "高危", value: stats.danger },
          { label: "接口规则", value: stats.routes },
          { label: "旧版合并权限", value: stats.legacy },
        ].map((item) => (
          <div key={item.label} className="bg-card px-4 py-3">
            <div className="text-[11px] text-muted-foreground">
              {item.label}
            </div>
            <div className="text-xl font-semibold tabular-nums">
              {item.value}
            </div>
          </div>
        ))}
      </section>

      <div className="grid min-w-0 gap-4 xl:grid-cols-[minmax(0,1fr)_24rem]">
        <Pane bodyClassName="flex flex-col">
          <div className="flex flex-wrap items-center gap-2 border-b border-border/70 px-3 py-2.5">
            <div className="relative min-w-52 flex-1">
              <Search className="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-muted-foreground" />
              <Input
                value={keyword}
                onChange={(e) => setKeyword(e.target.value)}
                placeholder="搜索名称或权限字符"
                className="h-8 pl-8 text-xs"
              />
              {keyword && (
                <button
                  type="button"
                  aria-label="清除"
                  onClick={() => setKeyword("")}
                  className="absolute top-1/2 right-2 -translate-y-1/2 text-muted-foreground"
                >
                  <X className="size-3.5" />
                </button>
              )}
            </div>
            <button
              type="button"
              aria-pressed={onlyDanger}
              onClick={() => setOnlyDanger(!onlyDanger)}
              className={cn(
                "inline-flex h-8 items-center gap-1 rounded-lg border px-2.5 text-xs transition-colors",
                onlyDanger
                  ? "border-primary/40 bg-primary/10 text-primary"
                  : "text-muted-foreground hover:bg-accent",
              )}
            >
              <ShieldAlert className="size-3.5" />
              高危
            </button>
            <Button
              variant="ghost"
              size="sm"
              className="h-8 gap-1 px-2 text-xs"
              onClick={() =>
                setExpanded(
                  new Set(
                    index.order.filter(
                      (k) => index.entries.get(k)!.node.kind !== "action",
                    ),
                  ),
                )
              }
            >
              <ChevronsUpDown className="size-3.5" />
              展开
            </Button>
            <Button
              variant="ghost"
              size="sm"
              className="h-8 gap-1 px-2 text-xs"
              onClick={() => setExpanded(new Set())}
            >
              <ChevronsDownUp className="size-3.5" />
              折叠
            </Button>
          </div>
          <div className="hidden grid-cols-[minmax(0,1fr)_3.5rem_minmax(0,14rem)_3.5rem_4rem] gap-3 border-b border-border/70 bg-muted/25 px-4 py-2 pl-10 text-[11px] font-medium text-muted-foreground md:grid">
            <span>名称</span>
            <span>类型</span>
            <span>权限字符</span>
            <span>作用域</span>
            <span className="text-right">接口</span>
          </div>
          <div className="max-h-[calc(100dvh-22rem)] min-h-80 overflow-y-auto py-1">
            {render(index.roots)}
            {visible && visible.size === 0 && (
              <div className="py-10 text-center text-xs text-muted-foreground">
                暂无匹配项
              </div>
            )}
          </div>
        </Pane>

        <PermissionInspector
          index={index}
          code={selected}
          roles={rolesQuery.data ?? []}
        />
      </div>
    </div>
  );
}

function PermissionInspector({
  index,
  code,
  roles,
}: {
  index: TreeIndex;
  code: string | null;
  roles: Array<{ key: string; name: string; permissions: string[] }>;
}) {
  const routesQuery = usePermissionRoutesQuery(code);
  const entry = code
    ? index.entries.get(index.codeKey.get(code) ?? "")
    : undefined;
  const holders = useMemo(
    () => (code ? roles.filter((role) => role.permissions.includes(code)) : []),
    [roles, code],
  );

  if (!code || !entry) {
    return (
      <Pane className="xl:sticky xl:top-4 xl:self-start">
        <div className="flex flex-col items-center gap-2 py-16 text-center text-sm text-muted-foreground">
          <KeyRound className="size-6 opacity-50" />
          选择一个操作
        </div>
      </Pane>
    );
  }
  const node = entry.node;
  return (
    <Pane
      className="xl:sticky xl:top-4 xl:self-start"
      bodyClassName="flex flex-col"
    >
      <div className="space-y-2 border-b border-border/70 p-4">
        <div className="text-[11px] text-muted-foreground">
          {entry.path.join(" / ")}
        </div>
        <div className="flex items-center gap-2">
          <h3 className="text-base font-semibold">{node.name}</h3>
          {node.danger && <DangerTag />}
          <ScopeTag scope={node.scope} />
        </div>
        <CodeText className="block text-xs text-foreground">{code}</CodeText>
        {node.description && (
          <p className="text-xs text-muted-foreground">{node.description}</p>
        )}
        {node.legacy && (
          <div className="flex items-center gap-1.5 text-[11px] text-muted-foreground">
            <History className="size-3" />
            隶属旧权限 <CodeText>{node.legacy}</CodeText>
          </div>
        )}
      </div>
      <div className="border-b border-border/70 p-4">
        <div className="mb-2 flex items-center gap-1.5 text-xs font-medium">
          <Network className="size-3.5 text-muted-foreground" />
          覆盖接口
          <span className="text-muted-foreground tabular-nums">
            {routesQuery.data?.routes.length ?? 0}
          </span>
        </div>
        {routesQuery.isLoading && <Skeleton className="h-16" />}
        {routesQuery.data?.routes.length === 0 && (
          <div className="text-xs text-muted-foreground">在业务逻辑中判定</div>
        )}
        <div className="max-h-64 space-y-1 overflow-y-auto">
          {routesQuery.data?.routes.map((route, i) => (
            <div
              key={`${route.pattern}-${i}`}
              className="flex items-start gap-2"
            >
              <span className="flex shrink-0 gap-0.5">
                {(route.methods && route.methods.length
                  ? route.methods
                  : ["ANY"]
                )
                  .slice(0, 3)
                  .map((method) => (
                    <span
                      key={method}
                      className={cn(
                        "rounded px-1 font-mono text-[10px] leading-4 font-semibold",
                        METHOD_TONE[method] ?? "bg-muted text-muted-foreground",
                      )}
                    >
                      {method.slice(0, 4)}
                    </span>
                  ))}
              </span>
              <code className="min-w-0 font-mono text-[11px] leading-4 break-all">
                {route.pattern}
              </code>
            </div>
          ))}
        </div>
      </div>
      <div className="p-4">
        <div className="mb-2 text-xs font-medium">
          持有角色{" "}
          <span className="text-muted-foreground tabular-nums">
            {holders.length}
          </span>
        </div>
        <div className="flex flex-wrap gap-1.5">
          {holders.length === 0 && (
            <span className="text-xs text-muted-foreground">暂无</span>
          )}
          {holders.map((role) => (
            <span
              key={role.key}
              className="inline-flex h-6 items-center rounded-md border bg-muted/40 px-2 text-[11px]"
            >
              {role.name}
            </span>
          ))}
        </div>
      </div>
    </Pane>
  );
}
