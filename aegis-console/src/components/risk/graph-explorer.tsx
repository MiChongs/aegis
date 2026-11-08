"use client";

import { useMemo, useState } from "react";
import Link from "next/link";
import { AlertTriangle, ChevronRight, ExternalLink, Network, Search, ShieldBan } from "lucide-react";

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
import { RelationGraph, isBanned } from "./kit/relation-graph";

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
  const [trail, setTrail] = useState<Crumb[]>([]);

  const current = trail[trail.length - 1];
  const query = current ? { ...current.query, depth: Number(depth), fanout } : null;
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
    setTrail((cur) => [...cur, { query: { type: "vid", value: node.id }, label: node.label || node.id, kind: node.kind }]);
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
        <GraphView graph={graph.data} onNodeClick={recenter} refreshing={graph.isFetching} />
      )}
    </TabStack>
  );
}

function GraphView({ graph, onNodeClick, refreshing }: { graph: Subgraph; onNodeClick: (n: GraphNode) => void; refreshing: boolean }) {
  const center = graph.nodes.find((n) => n.id === graph.center) ?? null;
  const byId = useMemo(() => new Map(graph.nodes.map((n) => [n.id, n])), [graph.nodes]);

  const kindCounts = useMemo(() => {
    const m = new Map<GraphNodeKind, { total: number; banned: number }>();
    for (const n of graph.nodes) {
      const c = m.get(n.kind) ?? { total: 0, banned: 0 };
      c.total += 1;
      if (isBanned(n)) c.banned += 1;
      m.set(n.kind, c);
    }
    return [...m.entries()].sort((a, b) => b[1].total - a[1].total);
  }, [graph.nodes]);

  const bannedNeighbours = useMemo(() => {
    const ids = new Set<string>();
    for (const l of graph.links) {
      if (l.source === graph.center) ids.add(l.target);
      if (l.target === graph.center) ids.add(l.source);
    }
    return [...ids].map((id) => byId.get(id)).filter((n): n is GraphNode => !!n && isBanned(n));
  }, [graph.links, graph.center, byId]);

  const bannedTotal = graph.nodes.filter((n) => n.id !== graph.center && isBanned(n)).length;

  return (
    <div className="flex min-w-0 flex-col gap-4">
      {graph.truncated ? (
        <div className="flex items-center gap-2 rounded-xl border border-amber-500/40 bg-amber-500/5 px-4 py-2.5 text-sm text-amber-700 dark:text-amber-300">
          <AlertTriangle className="size-4 shrink-0" />
          结果已截断，可减少深度或扇出后查看
        </div>
      ) : null}
      <div className="grid min-w-0 gap-4 @4xl:grid-cols-[minmax(0,1fr)_320px]">
        <Panel
          icon={Network}
          tone="violet"
          title="关系图谱"
          aside={
            <span className="text-xs text-muted-foreground tabular-nums">
              {refreshing ? "加载中 " : ""}
              {graph.nodes.length} 个节点 {graph.links.length} 条关系
            </span>
          }
        >
          <RelationGraph graph={graph} height={560} onNodeClick={onNodeClick} focusId={graph.center} />
        </Panel>

        <div className="flex min-w-0 flex-col gap-4">
          {center ? <NodeCard node={center} /> : null}

          <Panel icon={ShieldBan} tone="rose" title="被封禁的邻居" aside={<span className="text-xs text-muted-foreground tabular-nums">全图 {bannedTotal}</span>}>
            {bannedNeighbours.length === 0 ? (
              <p className="text-sm text-muted-foreground">无</p>
            ) : (
              <ul className="flex flex-col gap-1">
                {bannedNeighbours.map((n) => (
                  <li key={n.id}>
                    <button
                      type="button"
                      onClick={() => onNodeClick(n)}
                      className="flex w-full min-w-0 items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm transition-colors hover:bg-accent"
                    >
                      <Badge variant="danger" size="sm">{NODE_KIND_LABEL[n.kind] ?? n.kind}</Badge>
                      <span className="min-w-0 flex-1 truncate">{n.label || n.id}</span>
                      <ChevronRight className="size-3.5 shrink-0 text-muted-foreground" />
                    </button>
                  </li>
                ))}
              </ul>
            )}
          </Panel>

          <Panel icon={Network} tone="sky" title="节点构成">
            <ul className="flex flex-col gap-2">
              {kindCounts.map(([kind, c]) => (
                <li key={kind} className="flex items-center gap-2 text-sm">
                  <span className="min-w-0 flex-1 truncate">{NODE_KIND_LABEL[kind] ?? kind}</span>
                  {c.banned > 0 ? <Badge variant="danger" size="sm">封禁 {c.banned}</Badge> : null}
                  <span className="w-10 text-right font-medium tabular-nums">{c.total}</span>
                </li>
              ))}
            </ul>
          </Panel>
        </div>
      </div>
    </div>
  );
}

const PROP_LABEL: Record<string, string> = {
  uid: "用户 ID", name: "名称", brand: "品牌", model: "型号", platform: "平台", os_version: "系统版本", country: "国家",
  province: "省份", city: "城市", isp: "运营商", asn: "ASN", last_action: "最近处置", emulator: "模拟器", rooted: "已 Root",
  banned: "已封禁", status: "状态", first_seen: "首次出现", last_seen: "最近出现", app: "应用", account: "登录名",
};

function propValue(key: string, v: unknown): string {
  if (v === null || v === undefined || v === "") return "—";
  if ((key === "first_seen" || key === "last_seen") && typeof v === "number") return v > 0 ? formatDateTime(v * 1000) : "—";
  if (typeof v === "boolean") return v ? "是" : "否";
  if (typeof v === "object") return JSON.stringify(v);
  return String(v);
}

/** u:<appId>:<userId> */
function parseUserVid(id: string): { appId: number; userId: number } | null {
  const m = /^u:(\d+):(\d+)$/.exec(id);
  return m ? { appId: Number(m[1]), userId: Number(m[2]) } : null;
}

function NodeCard({ node }: { node: GraphNode }) {
  const apps = useAdminAppsQuery();
  const user = node.kind === "user" ? parseUserVid(node.id) : null;
  const appKey = user ? apps.data?.find((a) => a.id === user.appId)?.appKey : undefined;
  const props = Object.entries(node.props ?? {}).sort(([a], [b]) => (PROP_LABEL[a] ? 0 : 1) - (PROP_LABEL[b] ? 0 : 1) || a.localeCompare(b));
  const banned = isBanned(node);

  return (
    <section className={cn("flex min-w-0 flex-col gap-3 rounded-2xl border bg-card p-4 sm:p-5", banned ? "border-rose-500/40" : "border-border")}>
      <div className="flex min-w-0 items-start gap-2">
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-1.5">
            <Badge variant="info" size="sm">{NODE_KIND_LABEL[node.kind] ?? node.kind}</Badge>
            {banned ? <Badge variant="danger" size="sm">已封禁</Badge> : null}
          </div>
          <div className="mt-1.5 truncate text-base font-semibold" title={node.label}>
            {node.label || node.id}
          </div>
          <div className="truncate font-mono text-[11px] text-muted-foreground" title={node.id}>
            {node.id}
          </div>
        </div>
      </div>
      {user && appKey ? (
        <Button asChild size="sm" variant="outline" className="w-fit">
          <Link href={`/app-users/${encodeURIComponent(appKey)}/${user.userId}?tab=risk`}>
            <ExternalLink />
            用户风险画像
          </Link>
        </Button>
      ) : null}
      {props.length > 0 ? (
        <dl className="flex flex-col divide-y divide-border border-t">
          {props.map(([k, v]) => (
            <div key={k} className="flex min-w-0 items-baseline gap-3 py-1.5">
              <dt className="w-20 shrink-0 truncate text-xs text-muted-foreground">{PROP_LABEL[k] ?? k}</dt>
              <dd className="min-w-0 flex-1 text-right text-sm break-all">{propValue(k, v)}</dd>
            </div>
          ))}
        </dl>
      ) : null}
    </section>
  );
}
