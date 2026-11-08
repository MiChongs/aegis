"use client";

import { useMemo, useState } from "react";
import Link from "next/link";
import { Bar, BarChart, CartesianGrid, Line, ComposedChart, XAxis, YAxis } from "recharts";
import {
  AlertTriangle, Ban, ChevronRight, Cpu, Fingerprint, Globe, History, Network, RotateCcw, ScrollText, ShieldAlert,
  ShieldCheck, ShieldQuestion, Siren, Smartphone, Sparkles,
} from "lucide-react";
import { toast } from "sonner";

import {
  AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter,
  AlertDialogHeader, AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ChartContainer, ChartTooltip, ChartTooltipContent, type ChartConfig } from "@/components/ui/chart";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Textarea } from "@/components/ui/textarea";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { ActionBadge, KindBadge, ScoreMeter } from "@/components/risk/kit/badges";
import {
  ACTION_COLOR, ACTION_LABEL, ACTION_ORDER, DURATION_PRESETS, EVENT_LABEL, NODE_KIND_LABEL, SEVERITY_LABEL, formatWindow,
} from "@/components/risk/kit/labels";
import { analyze, displayName, narrative, peopleDegree } from "@/components/risk/graph/graph-model";
import { NodeDetail } from "@/components/risk/graph/node-detail";
import { RelationList, RelationNarrative } from "@/components/risk/graph/relation-summary";
import { RiskGraph } from "@/components/risk/graph/risk-graph";
import type { AdminAppUserDetail } from "@/lib/api/types";
import type { DisposeInput, EntityUse, GraphNode, GraphNodeKind, ListEntry, RiskAction, Subgraph } from "@/lib/api/risk";
import { formatDate, formatDateTime, formatRelative } from "@/lib/datetime";
import { useDispose, useRiskGraph, useRiskUserProfile } from "@/lib/risk-hooks";
import { cn } from "@/lib/utils";

import { EmptyRow, Panel, StatTile } from "./user-detail-shared";

/**
 * 用户详情 → 风险。数据来自风控平台的用户画像（/risk/profile）：
 * 关系图（NebulaGraph 两跳）、风险时间线与决策（ClickHouse）、设备与相似设备（Milvus）、名单、复核、行为模式告警。
 */
export function UserRiskTab({ appKey, userId, user }: { appKey: string; userId: number; user: AdminAppUserDetail }) {
  const appId = user.appid;
  const [days, setDays] = useState(30);
  const profile = useRiskUserProfile(appId, userId, days);
  const data = profile.data;

  if (!appId) {
    return <Panel title="风险"><EmptyRow text="读不到用户所属应用" /></Panel>;
  }
  if (profile.isError) {
    return (
      <Panel title="风险" icon={<ShieldAlert className="size-4" />}>
        <EmptyRow text={`风控平台不可用：${(profile.error as Error)?.message ?? "未知错误"}`} />
      </Panel>
    );
  }

  const s = data?.summary;
  const blockedRate = s && s.total ? Math.round((s.blocked / s.total) * 1000) / 10 : 0;

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="text-sm text-muted-foreground">
          {s?.total && s.lastSeen ? `最近一次评估 ${formatRelative(s.lastSeen)}` : "近期没有风控评估"}
        </div>
        <ToggleGroup type="single" size="sm" variant="outline" value={String(days)} onValueChange={(v) => v && setDays(Number(v))}>
          {[7, 30, 90, 180].map((d) => (
            <ToggleGroupItem key={d} value={String(d)} className="px-3">{d} 天</ToggleGroupItem>
          ))}
        </ToggleGroup>
      </div>

      <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 xl:grid-cols-6">
        {profile.isLoading ? (
          Array.from({ length: 6 }).map((_, i) => <Skeleton key={i} className="h-[84px] rounded-2xl" />)
        ) : (
          <>
            <StatTile label="风控评估" value={s?.total ?? 0} hint={`${s?.scenes ?? 0} 个场景`} icon={<ScrollText className="size-3.5" />} />
            <StatTile label="拦截与封禁" value={s?.blocked ?? 0} hint={`占比 ${blockedRate}%`} tone={s?.blocked ? "danger" : "default"} icon={<Ban className="size-3.5" />} />
            <StatTile label="复核与验证码" value={(s?.reviewed ?? 0) + (s?.captcha ?? 0)} hint={`复核 ${s?.reviewed ?? 0}，验证码 ${s?.captcha ?? 0}`} tone={s?.reviewed ? "warning" : "default"} icon={<ShieldQuestion className="size-3.5" />} />
            <StatTile label="最高风险分" value={s?.maxScore ?? 0} hint={`平均 ${Math.round(s?.avgScore ?? 0)}`} tone={(s?.maxScore ?? 0) >= 80 ? "danger" : (s?.maxScore ?? 0) >= 60 ? "warning" : "default"} icon={<AlertTriangle className="size-3.5" />} />
            <StatTile label="使用过的设备" value={s?.devices ?? 0} hint={data?.devices?.some((d) => (d.similar ?? []).some((n) => n.banned)) ? "有相似设备已封禁" : "近期"} tone={data?.devices?.some((d) => d.lists?.some((l) => l.kind === "block")) ? "danger" : "default"} icon={<Smartphone className="size-3.5" />} />
            <StatTile label="使用过的 IP" value={s?.ips ?? 0} hint={data?.ips?.[0] ? [data.ips[0].country, data.ips[0].city].filter(Boolean).join(" ") || "最近一个" : "近期"} icon={<Globe className="size-3.5" />} />
          </>
        )}
      </div>

      <RiskGraphPanel appKey={appKey} appId={appId} graph={data?.graph} days={days} name={user.nickname || user.account || `用户 #${userId}`} loading={profile.isLoading} error={data?.errors?.graph} />

      <div className="grid gap-4 xl:grid-cols-[minmax(0,1.6fr)_minmax(0,1fr)]">
        <TimelinePanel timeline={data?.timeline ?? []} loading={profile.isLoading} />
        <DisposePanel appId={appId} userId={userId} account={user.account} lists={data?.lists ?? []} devices={data?.devices ?? []} ips={data?.ips ?? []} />
      </div>

      <div className="grid gap-4 xl:grid-cols-2">
        <DevicesPanel devices={data?.devices ?? []} loading={profile.isLoading} />
        <IPsPanel ips={data?.ips ?? []} loading={profile.isLoading} />
      </div>

      <div className="grid gap-4 xl:grid-cols-2">
        <RankPanel title="触达场景" icon={<History className="size-4" />} rows={data?.scenes ?? []} empty="没有评估记录" />
        <RankPanel title="命中最多的规则" icon={<ShieldAlert className="size-4" />} rows={data?.rules ?? []} empty="没有命中规则" />
      </div>

      <DecisionsPanel profile={data} loading={profile.isLoading} appId={appId} userId={userId} />

      <div className="grid gap-4 xl:grid-cols-2">
        <EventsPanel profile={data} />
        <AlertsCasesPanel profile={data} />
      </div>

      <FeaturesPanel profile={data} />
    </div>
  );
}

// ── 关系图 ──

function RiskGraphPanel({ appKey, appId, graph, days, name, loading, error }: {
  appKey: string; appId: number; graph?: Subgraph; days: number; name: string; loading: boolean; error?: string;
}) {
  const [center, setCenter] = useState<string | null>(null);
  const [selected, setSelected] = useState<GraphNode | null>(null);
  const [depth, setDepth] = useState(2);
  const recentered = useRiskGraph(center || depth !== 2 ? { type: "vid", value: center ?? graph?.center ?? "", depth, fanout: 40, max: 300, days } : null);
  const shown = (center || depth !== 2) && recentered.data ? recentered.data : graph;

  const stats = useMemo(() => (shown ? analyze(shown) : null), [shown]);
  const degree = useMemo(() => (shown ? peopleDegree(shown) : new Map<string, number>()), [shown]);
  const centerNode = shown?.nodes.find((n) => n.id === shown.center);
  const centerName = center && centerNode ? displayName(centerNode) : name;
  const expand = (n: GraphNode) => {
    setSelected(null);
    setCenter(n.id === graph?.center ? null : n.id);
  };

  return (
    <Panel
      title="关系网络"
      icon={<Network className="size-4" />}
      description="所选时间内与这位用户有关的设备、网络、第三方账号、邀请与支付关系。多人共用的出口和机房、代理地址只画成灰色虚线，不当作关联依据"
      action={
        <div className="flex items-center gap-2">
          <Select value={String(depth)} onValueChange={(v) => setDepth(Number(v))}>
            <SelectTrigger size="sm" className="h-8 w-24"><SelectValue /></SelectTrigger>
            <SelectContent>
              {[1, 2, 3].map((d) => <SelectItem key={d} value={String(d)}>{d} 跳</SelectItem>)}
            </SelectContent>
          </Select>
          {center ? (
            <Button size="sm" variant="outline" onClick={() => { setCenter(null); setSelected(null); }}>
              <RotateCcw className="size-3.5" />
              回到本人
            </Button>
          ) : null}
        </div>
      }
    >
      {loading ? (
        <div className="space-y-3">
          <Skeleton className="h-24 rounded-xl" />
          <Skeleton className="h-[560px] rounded-xl" />
        </div>
      ) : !shown || shown.nodes.length === 0 || !stats ? (
        <EmptyRow text={error ? `关系网络暂时不可用：${error}` : "这段时间里还没有关系数据：用户登录或产生业务后才会出现"} />
      ) : (
        <div className="space-y-4">
          <RelationNarrative text={narrative(centerName, days, stats)} stats={stats} />
          <div className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_340px]">
            <div className="relative min-w-0 rounded-xl border">
              {recentered.isFetching ? (
                <div className="absolute top-14 right-3 z-10 rounded-md bg-background/85 px-2 py-1 text-xs text-muted-foreground">展开中</div>
              ) : null}
              <RiskGraph graph={shown} height={600} selectedId={selected?.id} onSelect={setSelected} onExpand={expand} />
              {shown.truncated ? (
                <p className="border-t px-3 py-2 text-xs text-amber-600 dark:text-amber-400">关系太多，只画出了最近的一部分；双击某个点可以以它为中心看得更细</p>
              ) : null}
            </div>
            <div className="min-w-0 space-y-4">
              {selected ? (
                <NodeDetail
                  node={selected}
                  degree={degree.get(selected.id) ?? 0}
                  appKey={appKey}
                  appId={appId}
                  onClose={() => setSelected(null)}
                  onExpand={selected.id === shown.center ? undefined : expand}
                />
              ) : null}
              <div>
                <div className="mb-2 flex items-center justify-between px-1">
                  <span className="text-sm font-medium">关联的人</span>
                  <span className="text-xs text-muted-foreground">按关联强弱排列</span>
                </div>
                <RelationList relations={stats.relations} appKey={appKey} appId={appId} focusId={selected?.id} onFocus={setSelected} />
              </div>
            </div>
          </div>
        </div>
      )}
    </Panel>
  );
}

// ── 时间线 ──

const timelineConfig: ChartConfig = Object.fromEntries(
  ACTION_ORDER.map((a) => [a, { label: ACTION_LABEL[a], color: ACTION_COLOR[a] }]),
) as ChartConfig;

function TimelinePanel({ timeline, loading }: { timeline: Array<{ day: string; action: RiskAction; count: number; maxScore: number }>; loading: boolean }) {
  const rows = useMemo(() => {
    const byDay = new Map<string, Record<string, number | string>>();
    for (const t of timeline) {
      const key = t.day.slice(0, 10);
      const row = byDay.get(key) ?? { day: formatDate(t.day), maxScore: 0 };
      row[t.action] = ((row[t.action] as number) ?? 0) + t.count;
      row.maxScore = Math.max(row.maxScore as number, t.maxScore);
      byDay.set(key, row);
    }
    return [...byDay.entries()].sort(([a], [b]) => a.localeCompare(b)).map(([, r]) => r);
  }, [timeline]);
  return (
    <Panel title="风险时间线" icon={<History className="size-4" />} description="每天的评估按处置分色，折线为当天最高风险分">
      {loading ? (
        <Skeleton className="h-64 rounded-xl" />
      ) : rows.length === 0 ? (
        <EmptyRow text="这段时间没有评估" />
      ) : (
        <ChartContainer config={{ ...timelineConfig, maxScore: { label: "最高分", color: "var(--foreground)" } }} className="h-64 w-full">
          <ComposedChart data={rows} margin={{ left: -8, right: 8, top: 8 }}>
            <CartesianGrid vertical={false} />
            <XAxis dataKey="day" tickLine={false} axisLine={false} fontSize={11} minTickGap={24} />
            <YAxis yAxisId="n" tickLine={false} axisLine={false} fontSize={11} allowDecimals={false} />
            <YAxis yAxisId="s" orientation="right" tickLine={false} axisLine={false} fontSize={11} domain={[0, 120]} />
            <ChartTooltip content={<ChartTooltipContent />} />
            {ACTION_ORDER.map((a) => (
              <Bar key={a} yAxisId="n" dataKey={a} stackId="a" fill={`var(--color-${a})`} radius={a === "ban" ? [3, 3, 0, 0] : 0} maxBarSize={28} />
            ))}
            <Line yAxisId="s" dataKey="maxScore" type="monotone" stroke="var(--color-maxScore)" strokeWidth={1.5} dot={false} />
          </ComposedChart>
        </ChartContainer>
      )}
    </Panel>
  );
}

// ── 名单与处置 ──

function DisposePanel({ appId, userId, account, lists, devices, ips }: {
  appId: number; userId: number; account?: string; lists: ListEntry[]; devices: EntityUse[]; ips: EntityUse[];
}) {
  const dispose = useDispose();
  const [pending, setPending] = useState<{ title: string; input: DisposeInput } | null>(null);
  const [reason, setReason] = useState("");
  const [duration, setDuration] = useState(86400);
  const device = devices[0]?.value;
  const ip = ips[0]?.value;
  const all = [
    ...lists.map((l) => ({ ...l, on: "用户" })),
    ...devices.flatMap((d) => (d.lists ?? []).map((l) => ({ ...l, on: "设备" }))),
    ...ips.flatMap((p) => (p.lists ?? []).map((l) => ({ ...l, on: "IP" }))),
  ];
  const base = { appId, userId, account, deviceId: device, ip };
  const actions: Array<{ title: string; icon: React.ReactNode; input: Omit<DisposeInput, "durationSeconds" | "reason"> }> = [
    { title: "拉黑用户与最近设备", icon: <Ban className="size-3.5" />, input: { ...base, kind: "block", targets: ["user", ...(device ? (["device"] as const) : [])] } },
    { title: "封号并踢下线", icon: <ShieldAlert className="size-3.5" />, input: { ...base, kind: "block", targets: ["ban_account"] } },
    { title: "加入观察名单", icon: <ShieldQuestion className="size-3.5" />, input: { ...base, kind: "watch", targets: ["user"] } },
    { title: "加入白名单", icon: <ShieldCheck className="size-3.5" />, input: { ...base, kind: "allow", targets: ["user"] } },
    ...(ip ? [{ title: "防火墙封最近 IP", icon: <Globe className="size-3.5" />, input: { ...base, kind: "block" as const, targets: ["firewall" as const] } }] : []),
  ];
  return (
    <Panel title="名单与处置" icon={<ShieldAlert className="size-4" />} description="用户、最近设备与 IP 身上现行的名单；处置立即在风控平台生效，封号由主服务执行">
      <div className="space-y-3">
        {all.length ? (
          <ul className="space-y-1.5">
            {all.map((l) => (
              <li key={l.id} className="flex items-center justify-between gap-2 rounded-lg border px-3 py-2 text-sm">
                <div className="min-w-0">
                  <div className="flex items-center gap-1.5">
                    <KindBadge kind={l.kind} />
                    <span className="text-xs text-muted-foreground">{l.on}</span>
                    <span className="truncate font-mono text-xs">{l.value}</span>
                  </div>
                  {l.reason ? <div className="mt-0.5 truncate text-xs text-muted-foreground">{l.reason}</div> : null}
                </div>
                <span className="shrink-0 text-xs text-muted-foreground">{l.exp ? `${formatRelative(l.exp)}到期` : "永久"}</span>
              </li>
            ))}
          </ul>
        ) : (
          <p className="text-sm text-muted-foreground">没有现行名单</p>
        )}
        <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
          {actions.map((a) => (
            <Button key={a.title} variant="outline" size="sm" className="justify-start" onClick={() => setPending({ title: a.title, input: { ...a.input, durationSeconds: duration, reason } })}>
              {a.icon}
              {a.title}
            </Button>
          ))}
        </div>
      </div>
      <AlertDialog open={!!pending} onOpenChange={(o) => !o && setPending(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{pending?.title}</AlertDialogTitle>
            <AlertDialogDescription>对象：用户 #{userId}{device ? `、设备 ${device}` : ""}{ip ? `、IP ${ip}` : ""}。处置会记入风控平台审计。</AlertDialogDescription>
          </AlertDialogHeader>
          <div className="space-y-3">
            <Select value={String(duration)} onValueChange={(v) => setDuration(Number(v))}>
              <SelectTrigger className="w-full"><SelectValue /></SelectTrigger>
              <SelectContent>
                {DURATION_PRESETS.map((p) => <SelectItem key={p.seconds} value={String(p.seconds)}>{p.label}</SelectItem>)}
              </SelectContent>
            </Select>
            <Textarea placeholder="原因（会显示在名单与封禁记录上）" value={reason} onChange={(e) => setReason(e.target.value)} rows={3} />
          </div>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction
              disabled={dispose.isPending}
              onClick={() => {
                if (!pending) return;
                dispose.mutate({ ...pending.input, durationSeconds: duration, reason: reason || pending.title }, {
                  onSuccess: (res) => toast.success("已处置", { description: Object.values(res ?? {}).join("，") }),
                  onError: (e) => toast.error("处置失败", { description: (e as Error).message }),
                });
                setPending(null);
              }}
            >
              确认（{formatWindow(duration)}）
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </Panel>
  );
}

// ── 设备与 IP ──

function DevicesPanel({ devices, loading }: { devices: EntityUse[]; loading: boolean }) {
  return (
    <Panel title="设备" icon={<Fingerprint className="size-4" />} description="按最近使用排序；相似设备来自指纹向量检索（同应用内）">
      {loading ? <Skeleton className="h-40 rounded-xl" /> : devices.length === 0 ? <EmptyRow text="没有设备记录" /> : (
        <ul className="space-y-2">
          {devices.map((d) => {
            const fp = d.fingerprint ?? {};
            const similar = (d.similar ?? []).filter((n) => n.score >= 0.9);
            const bannedSimilar = similar.filter((n) => n.banned).length;
            return (
              <li key={d.value} className="rounded-xl border p-3">
                <div className="flex items-start justify-between gap-3">
                  <div className="min-w-0">
                    <div className="flex items-center gap-1.5">
                      <Cpu className="size-3.5 text-muted-foreground" />
                      <span className="truncate text-sm font-medium">{[fp.brand, fp.model].filter(Boolean).join(" ") || "未知型号"}</span>
                      {fp.emulator === "true" ? <Badge variant="danger" size="sm">模拟器</Badge> : null}
                      {fp.rooted === "true" ? <Badge variant="warning" size="sm">已 Root</Badge> : null}
                      {(d.lists ?? []).map((l) => <KindBadge key={l.id} kind={l.kind} />)}
                    </div>
                    <div className="mt-0.5 truncate font-mono text-xs text-muted-foreground">{d.value}</div>
                  </div>
                  <div className="shrink-0 text-right text-xs text-muted-foreground">
                    <div>{d.count} 次</div>
                    <div>{formatRelative(d.lastSeen)}</div>
                  </div>
                </div>
                {similar.length ? (
                  <div className="mt-2 rounded-lg bg-muted/60 px-2.5 py-2">
                    <div className="mb-1 flex items-center gap-1.5 text-xs">
                      <Sparkles className="size-3.5 text-muted-foreground" />
                      <span>{similar.length} 台高度相似的设备</span>
                      {bannedSimilar ? <Badge variant="danger" size="sm">{bannedSimilar} 台已封禁</Badge> : null}
                    </div>
                    <div className="flex flex-wrap gap-1">
                      {similar.slice(0, 6).map((n) => (
                        <Badge key={n.deviceId} variant={n.banned ? "danger" : "outline"} size="sm" className="font-mono">
                          {n.deviceId.slice(0, 10)} {(n.score * 100).toFixed(0)}%
                        </Badge>
                      ))}
                    </div>
                  </div>
                ) : null}
              </li>
            );
          })}
        </ul>
      )}
    </Panel>
  );
}

function IPsPanel({ ips, loading }: { ips: EntityUse[]; loading: boolean }) {
  return (
    <Panel title="IP" icon={<Globe className="size-4" />} description="按最近使用排序">
      {loading ? <Skeleton className="h-40 rounded-xl" /> : ips.length === 0 ? <EmptyRow text="没有 IP 记录" /> : (
        <ul className="divide-y">
          {ips.map((p) => (
            <li key={p.value} className="flex items-center justify-between gap-3 py-2">
              <div className="min-w-0">
                <div className="flex items-center gap-1.5">
                  <span className="font-mono text-sm">{p.value}</span>
                  {(p.lists ?? []).map((l) => <KindBadge key={l.id} kind={l.kind} />)}
                </div>
                <div className="text-xs text-muted-foreground">{[p.country, p.city].filter(Boolean).join(" ") || "归属地未知"}</div>
              </div>
              <div className="shrink-0 text-right text-xs text-muted-foreground">
                <div>{p.count} 次</div>
                <div>{formatRelative(p.lastSeen)}</div>
              </div>
            </li>
          ))}
        </ul>
      )}
    </Panel>
  );
}

function RankPanel({ title, icon, rows, empty }: { title: string; icon: React.ReactNode; rows: Array<{ key: string; count: number }>; empty: string }) {
  const max = Math.max(1, ...rows.map((r) => r.count));
  return (
    <Panel title={title} icon={icon}>
      {rows.length === 0 ? <EmptyRow text={empty} /> : (
        <ul className="space-y-2">
          {rows.slice(0, 10).map((r) => (
            <li key={r.key} className="grid grid-cols-[minmax(0,10rem)_1fr_3rem] items-center gap-3 text-sm">
              <span className="truncate font-mono text-xs">{r.key}</span>
              <div className="h-2 overflow-hidden rounded-full bg-muted">
                <div className="h-full rounded-full bg-primary/70" style={{ width: `${(r.count / max) * 100}%` }} />
              </div>
              <span className="text-right text-xs tabular-nums text-muted-foreground">{r.count}</span>
            </li>
          ))}
        </ul>
      )}
    </Panel>
  );
}

// ── 决策、事件、告警、复核、特征 ──

type Profile = ReturnType<typeof useRiskUserProfile>["data"];

function DecisionsPanel({ profile, loading, appId, userId }: { profile: Profile; loading: boolean; appId: number; userId: number }) {
  const rows = profile?.decisions ?? [];
  return (
    <Panel
      title="最近的风控决策"
      icon={<ScrollText className="size-4" />}
      action={
        <Button asChild size="sm" variant="outline" className="h-7">
          <Link href={`/risk-control?tab=decisions&app=${appId}&user=${userId}`}>全部</Link>
        </Button>
      }
      bodyClassName="px-0 py-0"
    >
      {loading ? <Skeleton className="m-4 h-40 rounded-xl" /> : rows.length === 0 ? <EmptyRow text="没有决策记录" /> : (
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead className="bg-muted/40 text-xs text-muted-foreground">
              <tr>
                <th className="px-4 py-2 text-left font-medium">时间</th>
                <th className="px-4 py-2 text-left font-medium">场景</th>
                <th className="px-4 py-2 text-left font-medium">处置</th>
                <th className="w-40 px-4 py-2 text-left font-medium">风险分</th>
                <th className="px-4 py-2 text-left font-medium">原因</th>
                <th className="px-4 py-2 text-left font-medium">IP</th>
              </tr>
            </thead>
            <tbody className="divide-y">
              {rows.map((d) => (
                <tr key={d.id}>
                  <td className="px-4 py-2 whitespace-nowrap text-xs text-muted-foreground">{formatDateTime(d.ts)}</td>
                  <td className="px-4 py-2 font-mono text-xs">{d.scene}</td>
                  <td className="px-4 py-2"><ActionBadge action={d.action} /></td>
                  <td className="px-4 py-2"><ScoreMeter score={d.score} /></td>
                  <td className="max-w-[24rem] px-4 py-2 text-xs"><span className="line-clamp-2">{d.reasons?.join("；") || "—"}</span></td>
                  <td className="px-4 py-2 font-mono text-xs whitespace-nowrap">{d.ip || "—"}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </Panel>
  );
}

function EventsPanel({ profile }: { profile: Profile }) {
  const rows = profile?.events ?? [];
  return (
    <Panel title="业务事件" icon={<History className="size-4" />} description="登录、注册、支付、签到、邀请等进入风控事件流的结果">
      {rows.length === 0 ? <EmptyRow text="没有事件" /> : (
        <ol className="relative space-y-3 border-l pl-4">
          {rows.map((e) => (
            <li key={e.id} className="relative">
              <span className={cn("absolute top-1.5 -left-[21px] size-2.5 rounded-full border-2 border-background", e.type.endsWith(".fail") || e.type === "account.ban" ? "bg-red-500" : "bg-primary/70")} />
              <div className="flex items-center justify-between gap-2 text-sm">
                <span>{EVENT_LABEL[e.type] ?? e.type}{e.amount ? `（${e.amount}）` : ""}</span>
                <span className="text-xs text-muted-foreground">{formatDateTime(e.ts)}</span>
              </div>
              <div className="text-xs text-muted-foreground">{[e.ip, [e.country, e.city].filter(Boolean).join(" "), e.deviceId].filter(Boolean).join("，") || "—"}</div>
            </li>
          ))}
        </ol>
      )}
    </Panel>
  );
}

function AlertsCasesPanel({ profile }: { profile: Profile }) {
  const alerts = profile?.alerts ?? [];
  const cases = profile?.cases ?? [];
  return (
    <Panel title="行为模式告警与复核" icon={<Siren className="size-4" />}>
      <div className="space-y-4">
        <div>
          <div className="mb-2 text-xs font-medium text-muted-foreground">告警（{alerts.length}）</div>
          {alerts.length === 0 ? <p className="text-sm text-muted-foreground">没有</p> : (
            <ul className="space-y-1.5">
              {alerts.map((a) => (
                <li key={a.id} className="flex items-center justify-between gap-2 rounded-lg border px-3 py-2 text-sm">
                  <div className="min-w-0">
                    <div className="truncate">{a.patternName || a.pattern}</div>
                    <div className="text-xs text-muted-foreground">{a.eventIds.length} 个事件</div>
                  </div>
                  <div className="shrink-0 text-right">
                    <Badge variant={a.severity === "critical" || a.severity === "high" ? "danger" : "warning"} size="sm">{SEVERITY_LABEL[a.severity] ?? a.severity}</Badge>
                    <div className="mt-0.5 text-xs text-muted-foreground">{formatRelative(a.ts)}</div>
                  </div>
                </li>
              ))}
            </ul>
          )}
        </div>
        <div>
          <div className="mb-2 text-xs font-medium text-muted-foreground">复核单（{cases.length}）</div>
          {cases.length === 0 ? <p className="text-sm text-muted-foreground">没有</p> : (
            <ul className="space-y-1.5">
              {cases.map((c) => (
                <li key={c.id} className="flex items-center justify-between gap-2 rounded-lg border px-3 py-2 text-sm">
                  <div className="min-w-0">
                    <div className="truncate">{c.reasons?.join("；") || c.scene}</div>
                    <div className="text-xs text-muted-foreground">{c.scene}，{formatDateTime(c.createdAt)}</div>
                  </div>
                  <Badge variant={c.status === "pending" ? "warning" : c.verdict === "reject" ? "danger" : "success"} size="sm">
                    {c.status === "pending" ? "待复核" : c.verdict === "reject" ? "确认有风险" : "已放行"}
                  </Badge>
                </li>
              ))}
            </ul>
          )}
        </div>
      </div>
    </Panel>
  );
}

function FeaturesPanel({ profile }: { profile: Profile }) {
  const snaps = profile?.features ?? [];
  const [scene, setScene] = useState<string | null>(null);
  const cur = snaps.find((s) => s.scene === scene) ?? snaps[0];
  if (!cur) return null;
  const entries = Object.entries(cur.features ?? {}).sort(([a], [b]) => a.localeCompare(b));
  const models = Object.entries(cur.models ?? {});
  return (
    <Panel
      title="最近一次评估的特征"
      icon={<Sparkles className="size-4" />}
      description={`场景 ${cur.scene}，${formatDateTime(cur.ts)}`}
      action={
        snaps.length > 1 ? (
          <Select value={cur.scene} onValueChange={setScene}>
            <SelectTrigger size="sm" className="h-8 w-40"><SelectValue /></SelectTrigger>
            <SelectContent>{snaps.map((s) => <SelectItem key={s.scene} value={s.scene}>{s.scene}</SelectItem>)}</SelectContent>
          </Select>
        ) : undefined
      }
    >
      <div className="grid grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-4">
        {entries.map(([k, v]) => (
          <div key={k} className="rounded-lg border px-3 py-2">
            <div className="truncate font-mono text-[11px] text-muted-foreground">{k}</div>
            <div className="text-sm font-semibold tabular-nums">{Number.isInteger(v) ? v : v.toFixed(3)}</div>
          </div>
        ))}
        {models.map(([k, v]) => (
          <div key={`m-${k}`} className="rounded-lg border border-violet-200 bg-violet-50/50 px-3 py-2 dark:border-violet-900/60 dark:bg-violet-950/30">
            <div className="truncate font-mono text-[11px] text-muted-foreground">模型 {k}</div>
            <div className="text-sm font-semibold tabular-nums">{v.toFixed(4)}</div>
          </div>
        ))}
      </div>
    </Panel>
  );
}
