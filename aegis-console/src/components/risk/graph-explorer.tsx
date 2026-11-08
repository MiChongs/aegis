"use client";

import { useMemo, useState } from "react";
import Link from "next/link";
import { AlertTriangle, ChevronRight, Network, Search, Users } from "lucide-react";

import { CardEmpty } from "@/components/overview/overview-kit";
import { Panel, Segmented, TabStack } from "@/components/traffic/traffic-kit";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import type { GraphNode, GraphNodeKind, Subgraph, getGraph } from "@/lib/api/risk";
import { useAdminAppsQuery } from "@/lib/admin-hooks";
import { formatDateTime } from "@/lib/datetime";
import { useRiskGraph } from "@/lib/risk-hooks";
import { cn } from "@/lib/utils";

import { errorText } from "./decisions";
import { AppScopeSelect } from "./kit/app-scope";
import { NODE_KIND_LABEL } from "./kit/labels";
import { analyze, displayName, isBanned, narrative, peopleDegree } from "./graph/graph-model";
import { NodeDetail, parseUserVid } from "./graph/node-detail";
import { RelationList, RelationNarrative } from "./graph/relation-summary";
import { RiskGraph } from "./graph/risk-graph";

type GraphQuery = Parameters<typeof getGraph>[1];
type SearchType = "user" | "device" | "ip" | "account";

const SEARCH_TYPES: ReadonlyArray<{ value: SearchType; label: string }> = [
  { value: "user", label: "用户" },
  { value: "device", label: "设备" },
  { value: "ip", label: "IP" },
  { value: "account", label: "登录名" },
];

const DEPTHS = [
  { value: "1", label: "1 跳" },
  { value: "2", label: "2 跳" },
  { value: "3", label: "3 跳" },
] as const;

const FANOUTS = [10, 20, 40, 80, 200];

const WINDOWS = [
  { value: "7", label: "7 天" },
  { value: "30", label: "30 天" },
  { value: "90", label: "90 天" },
  { value: "", label: "全部" },
] as const;

interface Crumb {
  query: GraphQuery;
  label: string;
  kind?: GraphNodeKind;
}

export function RiskGraphExplorer({ app }: { app?: number }) {
  const [type, setType] = useState<SearchType>("user");
  const [formApp, setFormApp] = useState<number | undefined>(app);
  const [input, setInput] = useState("");
  const [depth, setDepth] = useState<"1" | "2" | "3">("2");
  const [fanout, setFanout] = useState(40);
  const [days, setDays] = useState<"7" | "30" | "90" | "">("90");
  const [trail, setTrail] = useState<Crumb[]>([]);

  const current = trail[trail.length - 1];
  const query = current ? { ...current.query, depth: Number(depth), fanout, ...(days ? { days: Number(days) } : {}) } : null;
  const graph = useRiskGraph(query);

  const value = input.trim();
  const userId = Number(value);
  const canSearch = type === "user" ? !!formApp && Number.isInteger(userId) && userId > 0 : !!value;

  const search = () => {
    if (!canSearch) return;
    const q: GraphQuery =
      type === "user"
        ? { type: "user", app: formApp, user: userId }
        : type === "account"
          ? { type: "account", app: formApp, value }
          : { type, value };
    setTrail([{ query: q, label: type === "user" ? `用户 #${userId}` : value, kind: type === "user" ? "user" : type === "account" ? "account" : type }]);
  };

  const recenter = (node: GraphNode) => {
    if (graph.data && node.id === graph.data.center) return;
    setTrail((cur) => [...cur, { query: { type: "vid", value: node.id }, label: displayName(node), kind: node.kind }]);
  };

  return (
    <TabStack>
      <div className="flex min-w-0 flex-wrap items-center gap-2 rounded-2xl border border-border bg-card p-4 sm:px-5">
        <Segmented value={type} options={SEARCH_TYPES} onChange={setType} ariaLabel="对象类型" />
        {type === "user" || type === "account" ? (
          <AppScopeSelect value={formApp} onChange={setFormApp} className="h-8 w-44" allLabel={type === "user" ? "选择应用" : "全部应用"} />
        ) : null}
        <Input
          className={cn("h-8 w-56", type !== "account" && "font-mono text-xs")}
          inputMode={type === "user" ? "numeric" : undefined}
          placeholder={type === "user" ? "用户 ID" : type === "device" ? "设备 ID" : type === "ip" ? "IP 地址" : "登录名"}
          value={input}
          onChange={(e) => setInput(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") search();
          }}
        />
        <Button size="sm" disabled={!canSearch} onClick={search}>
          <Search />
          查询
        </Button>
        <div className="ml-auto flex flex-wrap items-center gap-2">
          <Segmented value={days} options={WINDOWS} onChange={setDays} ariaLabel="时间范围" />
          <Segmented value={depth} options={DEPTHS} onChange={setDepth} ariaLabel="深度" />
          <Select value={String(fanout)} onValueChange={(v) => setFanout(Number(v))}>
            <SelectTrigger size="sm" className="h-8 w-28">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {FANOUTS.map((f) => (
                <SelectItem key={f} value={String(f)}>
                  扇出 {f}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      </div>

      {trail.length > 0 ? (
        <nav aria-label="浏览路径" className="flex min-w-0 flex-wrap items-center gap-1 text-xs">
          {trail.map((c, i) => {
            const last = i === trail.length - 1;
            return (
              <span key={i} className="inline-flex min-w-0 items-center gap-1">
                {i > 0 ? <ChevronRight className="size-3.5 shrink-0 text-muted-foreground" /> : null}
                <button
                  type="button"
                  disabled={last}
                  onClick={() => setTrail((cur) => cur.slice(0, i + 1))}
                  className={cn(
                    "inline-flex max-w-56 min-w-0 items-center gap-1.5 rounded-md px-2 py-1 transition-colors",
                    last ? "bg-muted font-medium text-foreground" : "text-muted-foreground hover:bg-accent hover:text-foreground"
                  )}
                >
                  {c.kind ? <span className="shrink-0 text-[11px] text-muted-foreground">{NODE_KIND_LABEL[c.kind]}</span> : null}
                  <span className="truncate">{c.label}</span>
                </button>
              </span>
            );
          })}
        </nav>
      ) : null}

      {!query ? (
        <Panel icon={Network} tone="violet" title="关系图谱">
          <CardEmpty icon={Network} title="暂无图谱" />
        </Panel>
      ) : graph.isError ? (
        <Panel icon={AlertTriangle} tone="rose" title="查询失败">
          <p className="text-sm text-muted-foreground">{errorText(graph.error)}</p>
        </Panel>
      ) : !graph.data ? (
        <div className="grid min-w-0 gap-4 @4xl:grid-cols-[minmax(0,1fr)_320px]">
          <Skeleton className="h-[600px] rounded-2xl" />
          <Skeleton className="h-[600px] rounded-2xl" />
        </div>
      ) : graph.data.nodes.length === 0 ? (
        <Panel icon={Network} tone="violet" title="关系图谱">
          <CardEmpty icon={Network} title="暂无关联" />
        </Panel>
      ) : (
        <GraphView graph={graph.data} onExpand={recenter} refreshing={graph.isFetching} days={days ? Number(days) : null} />
      )}
    </TabStack>
  );
}

function GraphView({ graph, onExpand, refreshing, days }: { graph: Subgraph; onExpand: (n: GraphNode) => void; refreshing: boolean; days: number | null }) {
  const apps = useAdminAppsQuery();
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const byId = useMemo(() => new Map(graph.nodes.map((n) => [n.id, n])), [graph.nodes]);
  const center = byId.get(graph.center) ?? null;
  const selected = (selectedId ? byId.get(selectedId) : null) ?? center;
  const degree = useMemo(() => peopleDegree(graph), [graph]);
  const stats = useMemo(() => (center?.kind === "user" ? analyze(graph) : null), [graph, center]);

  // 中心不是用户（设备、IP……）时：直接连着它的用户
  const users = useMemo(() => {
    if (!center || center.kind === "user") return [];
    const ids = new Set<string>();
    for (const l of graph.links) {
      if (l.source === graph.center) ids.add(l.target);
      if (l.target === graph.center) ids.add(l.source);
    }
    return [...ids].map((id) => byId.get(id)).filter((n): n is GraphNode => !!n && n.kind === "user");
  }, [graph, center, byId]);

  const appKeyOf = (n: GraphNode) => {
    const app = n.kind === "user" ? parseUserVid(n.id).app : undefined;
    return { appId: app, appKey: app ? apps.data?.find((a) => a.id === app)?.appKey : undefined };
  };

  return (
    <div className="flex min-w-0 flex-col gap-4">
      {stats && center ? <RelationNarrative text={narrative(displayName(center), days, stats)} stats={stats} /> : null}
      {graph.truncated ? (
        <div className="flex items-center gap-2 rounded-xl border border-amber-500/40 bg-amber-500/5 px-4 py-2.5 text-sm text-amber-700 dark:text-amber-300">
          <AlertTriangle className="size-4 shrink-0" />
          关系太多，只画出了最近的一部分；可以缩短时间范围、减少深度，或双击某个点以它为中心
        </div>
      ) : null}
      <div className="grid min-w-0 gap-4 @4xl:grid-cols-[minmax(0,1fr)_340px]">
        <Panel
          icon={Network}
          tone="violet"
          title="关系网络"
          aside={
            <span className="text-xs text-muted-foreground tabular-nums">
              {refreshing ? "加载中 " : ""}
              {graph.nodes.length} 个点，{graph.links.length} 条关系
            </span>
          }
        >
          <RiskGraph graph={graph} height={620} selectedId={selected?.id} onSelect={(n) => setSelectedId(n?.id ?? null)} onExpand={onExpand} />
        </Panel>

        <div className="flex min-w-0 flex-col gap-4">
          {selected ? (
            <NodeDetail
              node={selected}
              degree={degree.get(selected.id) ?? 0}
              {...appKeyOf(selected)}
              onClose={selected.id !== graph.center ? () => setSelectedId(null) : undefined}
              onExpand={selected.id !== graph.center ? onExpand : undefined}
            />
          ) : null}
          {stats ? (
            <Panel icon={Users} tone="sky" title="关联的人" aside={<span className="text-xs text-muted-foreground">按关联强弱排列</span>}>
              <RelationList relations={stats.relations} focusId={selectedId} onFocus={(n) => setSelectedId(n.id)} {...(center ? appKeyOf(center) : {})} />
            </Panel>
          ) : center ? (
            <Panel icon={Users} tone="sky" title={center.kind === "device" ? "在这台设备上出现过的用户" : `用过这个${NODE_KIND_LABEL[center.kind] ?? "对象"}的用户`}>
              {users.length === 0 ? (
                <p className="text-sm text-muted-foreground">没有</p>
              ) : (
                <ul className="flex flex-col gap-1">
                  {users.map((n) => (
                    <li key={n.id}>
                      <button
                        type="button"
                        onClick={() => setSelectedId(n.id)}
                        className={cn("flex w-full min-w-0 items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm transition-colors hover:bg-accent", selectedId === n.id && "bg-accent")}
                      >
                        <span className={cn("min-w-0 flex-1 truncate", isBanned(n) && "text-red-600 dark:text-red-400")}>{displayName(n)}</span>
                        {isBanned(n) ? <Badge variant="danger" size="sm">已封禁</Badge> : null}
                        <span className="shrink-0 text-xs text-muted-foreground">应用 #{parseUserVid(n.id).app}</span>
                      </button>
                    </li>
                  ))}
                </ul>
              )}
            </Panel>
          ) : null}
        </div>
      </div>
    </div>
  );
}
