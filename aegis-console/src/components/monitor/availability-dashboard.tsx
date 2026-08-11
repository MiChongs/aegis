"use client";

import type { ReactNode } from "react";
import { memo, useMemo, useState, useCallback } from "react";
import Link from "next/link";
import {
  Activity,
  AppWindow,
  Bell,
  Database,
  Globe2,
  HardDrive,
  ImageIcon,
  Radio,
  RefreshCw,
  Route,
  Search,
  Server,
  ServerCog,
  ShieldCheck,
  ShieldEllipsis,
  Waypoints,
  Layers,
  Cpu,
  LayoutGrid
} from "lucide-react";
import {
  useAppMonitorComponentsQuery,
  useAppMonitorHistoryQuery,
  useAppMonitorQuery,
  useSystemMonitorAppsQuery,
  useSystemMonitorComponentsQuery,
  useSystemMonitorHistoryQuery,
  useSystemMonitorQuery
} from "@/lib/admin-hooks";
import type { MonitorHistoryPoint } from "@/lib/api/monitor";
import type {
  AppSummary,
  MonitorAppBrief,
  MonitorComponent,
  MonitorEndpoint,
  MonitorRuntime,
  MonitorStatus
} from "@/lib/api-client";
import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from "@/components/ui/accordion";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { EmptyState, LoadingState } from "@/components/ui/data-state";
import { Input } from "@/components/ui/input";
import { SectionHeading } from "@/components/ui/section-heading";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { cn } from "@/lib/utils";

/* ------------------------------------------------------------------ */
/*  类型                                                               */
/* ------------------------------------------------------------------ */

type HistoryPoint = {
  checkedAt: string;
  status: MonitorStatus;
};

function convertHistory(points?: MonitorHistoryPoint[]): HistoryPoint[] {
  if (!points?.length) return [];
  return points.map((p) => ({
    checkedAt: new Date(p.t).toISOString(),
    status: p.s as MonitorStatus,
  }));
}

type AvailabilityDashboardProps = {
  mode: "console" | "public";
  apps?: AppSummary[];
  showPublicLinks?: boolean;
};

/* ------------------------------------------------------------------ */
/*  常量                                                               */
/* ------------------------------------------------------------------ */

const HISTORY_DISPLAY_POINTS = 60;
const DATA_FONT = "font-mono tabular-nums lining-nums slashed-zero tracking-tight";

const componentIconMap: Record<string, React.ComponentType<{ className?: string }>> = {
  postgres: Database,
  redis: Database,
  nats: Radio,
  temporal: Waypoints,
  workflow: Waypoints,
  auth: ShieldCheck,
  admin: ShieldCheck,
  notifications: Bell,
  storage: HardDrive,
  avatar: ImageIcon,
  location: Globe2,
  realtime: Activity,
  websocket: Activity,
  firewall: ShieldEllipsis,
  endpoint: Route,
  app: AppWindow,
};

/* ------------------------------------------------------------------ */
/*  格式化工具（纯函数，零开销）                                         */
/* ------------------------------------------------------------------ */

function fmtCount(v?: number | null) {
  return new Intl.NumberFormat("zh-CN").format(v || 0);
}

function fmtPct(v?: number | null) {
  return `${(v || 0).toFixed(2)}%`;
}

function fmtDate(v?: string | null) {
  if (!v) return "--";
  const d = new Date(v);
  if (Number.isNaN(d.getTime())) return "--";
  return new Intl.DateTimeFormat("zh-CN", { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit", second: "2-digit" }).format(d);
}

function fmtUptime(s?: number | null) {
  if (!s || s <= 0) return "--";
  const d = Math.floor(s / 86400), h = Math.floor((s % 86400) / 3600), m = Math.floor((s % 3600) / 60);
  if (d > 0) return `${d}天 ${h}时`;
  if (h > 0) return `${h}时 ${m}分`;
  return `${m}分`;
}

function statusLabel(s: MonitorStatus) {
  return s === "available" ? "正常" : s === "degraded" ? "降级" : "错误";
}

function statusBadge(s: MonitorStatus): "success" | "warning" | "danger" {
  return s === "available" ? "success" : s === "degraded" ? "warning" : "danger";
}

function statusColor(s: MonitorStatus) {
  return s === "available" ? "bg-emerald-500" : s === "degraded" ? "bg-amber-500" : "bg-red-500";
}

function dotClass(s: MonitorStatus) {
  return s === "available"
    ? "bg-emerald-500 ring-emerald-500/15"
    : s === "degraded"
    ? "bg-amber-500 ring-amber-500/15"
    : "bg-red-500 ring-red-500/15";
}

function healthScore(item: MonitorComponent) {
  return item.status === "available" ? 100 : item.status === "degraded" ? 60 : 10;
}

function sectionStatus(items: MonitorComponent[]): MonitorStatus {
  if (items.some((i) => i.status === "unavailable")) return "unavailable";
  if (items.some((i) => i.status === "degraded")) return "degraded";
  return "available";
}

function sectionSummary(items: MonitorComponent[]) {
  const a = items.filter((i) => i.status === "available").length;
  const d = items.filter((i) => i.status === "degraded").length;
  const u = items.length - a - d;
  const p: string[] = [];
  if (a) p.push(`${a} 正常`);
  if (d) p.push(`${d} 降级`);
  if (u) p.push(`${u} 异常`);
  return p.join(" · ") || `${items.length} 项`;
}

function getAvailability(history: HistoryPoint[], fallback: MonitorStatus) {
  const pts = history.length ? history : [{ checkedAt: "", status: fallback }];
  const ok = pts.filter((p) => p.status === "available").length;
  return { ok, total: pts.length, rate: pts.length ? (ok / pts.length) * 100 : 0 };
}

function sampleHistory(history: HistoryPoint[], fallback: MonitorStatus): HistoryPoint[] {
  if (!history.length) return Array.from({ length: HISTORY_DISPLAY_POINTS }, () => ({ checkedAt: "", status: fallback }));
  if (history.length <= HISTORY_DISPLAY_POINTS) return history;
  const out: HistoryPoint[] = [];
  for (let i = 0; i < HISTORY_DISPLAY_POINTS; i++) {
    out.push(history[Math.floor((i * (history.length - 1)) / (HISTORY_DISPLAY_POINTS - 1))]);
  }
  return out;
}

function normalizeItems(items: MonitorComponent[] = []) {
  const m = new Map<string, MonitorComponent>();
  for (const i of items) m.set(i.key, i);
  return Array.from(m.values());
}

function collectItems(groups: Array<MonitorComponent[] | undefined>) {
  return normalizeItems(groups.flatMap((g) => g || []));
}

function extraItems(primary: MonitorComponent[] = [], groups: Array<MonitorComponent[] | undefined>) {
  const known = new Set(groups.flatMap((g) => (g || []).map((i) => i.key)));
  return primary.filter((i) => !known.has(i.key));
}

function collectPrimEntries(record?: Record<string, unknown>) {
  if (!record) return [];
  return Object.entries(record)
    .map(([k, v]) => {
      const s = v === null || v === undefined ? null : typeof v === "string" ? (v || null) : typeof v === "number" || typeof v === "boolean" ? String(v) : Array.isArray(v) ? v.filter(Boolean).join(" / ") || null : null;
      return { label: k.replace(/([a-z0-9])([A-Z])/g, "$1 $2").replace(/[_-]+/g, " ").trim(), value: s };
    })
    .filter((e) => e.value)
    .slice(0, 8);
}

/* ------------------------------------------------------------------ */
/*  StatusDot                                                          */
/* ------------------------------------------------------------------ */

function StatusDot({ status }: { status: MonitorStatus }) {
  return <span className={cn("inline-block size-2 rounded-full ring-[3px]", dotClass(status))} />;
}

/* ------------------------------------------------------------------ */
/*  HistoryBar — 高性能历史条（无 Tooltip，纯 CSS）                       */
/* ------------------------------------------------------------------ */

const HistoryBar = memo(function HistoryBar({
  history,
  fallbackStatus
}: {
  history: HistoryPoint[];
  fallbackStatus: MonitorStatus;
}) {
  const points = useMemo(() => sampleHistory(history, fallbackStatus), [history, fallbackStatus]);

  // 合并连续相同状态为 segment，大幅减少 DOM 节点
  const segments = useMemo(() => {
    const segs: Array<{ status: MonitorStatus; count: number }> = [];
    for (const p of points) {
      const last = segs[segs.length - 1];
      if (last && last.status === p.status) {
        last.count++;
      } else {
        segs.push({ status: p.status, count: 1 });
      }
    }
    return segs;
  }, [points]);

  return (
    <div className="space-y-1">
      <div className="flex h-4 items-stretch gap-px overflow-hidden rounded-sm">
        {segments.map((seg, i) => (
          <span
            key={`${seg.status}-${i}`}
            className={cn("block h-full opacity-85", statusColor(seg.status))}
            style={{ flex: seg.count }}
          />
        ))}
      </div>
      <div className="flex justify-between text-[9px] text-muted-foreground/40">
        <span>Past</span>
        <span>Now</span>
      </div>
    </div>
  );
});

/* ------------------------------------------------------------------ */
/*  ServiceCard — 轻量级（无 Tooltip / 无 motion / 无 useInView）        */
/* ------------------------------------------------------------------ */

const ServiceCard = memo(function ServiceCard({
  item,
  history
}: {
  item: MonitorComponent;
  history: HistoryPoint[];
}) {
  const Icon = componentIconMap[item.key] || Server;
  const avail = useMemo(() => getAvailability(history, item.status as MonitorStatus), [history, item.status]);
  const duration = item.meta?.durationMs;
  const durStr = typeof duration === "number" && Number.isFinite(duration) ? `${duration}ms` : "--";
  const score = healthScore(item);

  return (
    <div className="rounded-xl border bg-card overflow-hidden">
      {/* 头部 */}
      <div className="flex items-center gap-3 px-4 py-2.5">
        <div className="flex size-7 shrink-0 items-center justify-center rounded-md border bg-muted/50">
          <Icon className="size-3.5 text-muted-foreground" />
        </div>
        <div className="min-w-0 flex-1">
          <div className="flex items-center justify-between gap-2">
            <h3 className="text-sm font-semibold truncate">{item.name}</h3>
            <Badge variant={statusBadge(item.status as MonitorStatus)} className="shrink-0 text-[10px]">
              <StatusDot status={item.status as MonitorStatus} />
              <span className="ml-1">{statusLabel(item.status as MonitorStatus)}</span>
            </Badge>
          </div>
          <p className="truncate text-[10px] text-muted-foreground">
            {item.dependsOn?.length ? item.dependsOn.join(" · ") : item.key}
          </p>
        </div>
      </div>

      {/* 指标行（3 列，grid gap 模拟分割线） */}
      <div className="grid grid-cols-3 gap-px bg-border">
        <div className="bg-card px-3 py-1.5">
          <div className="text-[9px] font-medium uppercase text-muted-foreground">耗时</div>
          <div className={cn("text-sm font-semibold", DATA_FONT)}>{durStr}</div>
        </div>
        <div className="bg-card px-3 py-1.5">
          <div className="text-[9px] font-medium uppercase text-muted-foreground">健康分</div>
          <div className={cn("text-sm font-semibold", DATA_FONT)}>{score}</div>
        </div>
        <div className="bg-card px-3 py-1.5">
          <div className="text-[9px] font-medium uppercase text-muted-foreground">可用率</div>
          <div className={cn("text-sm font-semibold", DATA_FONT,
            avail.rate >= 99 ? "text-emerald-600 dark:text-emerald-400" :
            avail.rate >= 80 ? "text-amber-600 dark:text-amber-400" :
            "text-red-600 dark:text-red-400"
          )}>{avail.rate.toFixed(1)}%</div>
        </div>
      </div>

      {/* 历史条 */}
      <div className="px-4 py-2">
        <HistoryBar history={history} fallbackStatus={item.status as MonitorStatus} />
      </div>
    </div>
  );
}, (prev, next) =>
  prev.item.key === next.item.key &&
  prev.item.status === next.item.status &&
  prev.item.summary === next.item.summary &&
  prev.item.checkedAt === next.item.checkedAt &&
  prev.history.length === next.history.length
);

/* ------------------------------------------------------------------ */
/*  EndpointCard                                                       */
/* ------------------------------------------------------------------ */

const EndpointCard = memo(function EndpointCard({ item }: { item: MonitorEndpoint }) {
  return (
    <div className="flex items-center gap-3 rounded-lg border bg-card px-3 py-2">
      <Badge variant="outline" className="text-[9px] px-1.5 py-0.5 shrink-0">{item.method}</Badge>
      <div className="min-w-0 flex-1">
        <span className={cn("text-sm font-semibold truncate block", DATA_FONT)}>{item.path}</span>
        <span className="text-[10px] text-muted-foreground">{item.name} · {item.protected ? "鉴权" : "公开"}</span>
      </div>
      <Badge variant={statusBadge(item.status)} className="text-[9px] shrink-0">{statusLabel(item.status)}</Badge>
    </div>
  );
});

/* ------------------------------------------------------------------ */
/*  AppOverviewCard                                                    */
/* ------------------------------------------------------------------ */

const AppOverviewCard = memo(function AppOverviewCard({
  item,
  active,
  onSelect
}: {
  item: MonitorAppBrief;
  active: boolean;
  onSelect: (id: number) => void;
}) {
  const handleClick = useCallback(() => onSelect(item.id), [onSelect, item.id]);

  return (
    <button
      type="button"
      className={cn(
        "flex w-full items-center gap-4 rounded-xl border bg-card px-4 py-3 text-left transition-colors",
        active ? "border-primary/50 ring-1 ring-primary/20" : "hover:bg-muted/30"
      )}
      onClick={handleClick}
    >
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <h3 className="text-sm font-semibold truncate">{item.name}</h3>
          <Badge variant={statusBadge(item.status)} className="shrink-0">{statusLabel(item.status)}</Badge>
        </div>
        <span className={cn("text-[10px] text-muted-foreground", DATA_FONT)}>APP {item.id}</span>
      </div>
      <div className="flex gap-3 shrink-0 text-right">
        <div>
          <div className="text-[9px] font-medium uppercase text-muted-foreground">健康分</div>
          <div className={cn("text-sm font-semibold", DATA_FONT)}>{item.score}</div>
        </div>
        <div>
          <div className="text-[9px] font-medium uppercase text-muted-foreground">可用率</div>
          <div className={cn("text-sm font-semibold", DATA_FONT)}>{fmtPct(item.availabilityRate)}</div>
        </div>
      </div>
    </button>
  );
});

/* ------------------------------------------------------------------ */
/*  LazyAccordionContent — 仅展开时渲染子组件                             */
/* ------------------------------------------------------------------ */

function LazyAccordionContent({ children, className }: { children: ReactNode; className?: string }) {
  // Radix AccordionContent 在关闭时仍保留子树。
  // 通过 data-state 属性配合 CSS 实现延迟卸载不现实，
  // 所以使用 state 跟踪是否曾经打开过：首次打开后才渲染内容，后续折叠保留缓存。
  const [hasOpened, setHasOpened] = useState(false);

  return (
    <AccordionContent
      className={className}
      onAnimationEnd={() => setHasOpened(true)}
      // 首次展开触发
      ref={(el) => {
        if (el && el.getAttribute("data-state") === "open") {
          setHasOpened(true);
        }
      }}
    >
      {hasOpened ? children : <div className="h-16 flex items-center justify-center text-xs text-muted-foreground">加载中...</div>}
    </AccordionContent>
  );
}

/* ------------------------------------------------------------------ */
/*  RuntimePanel                                                       */
/* ------------------------------------------------------------------ */

const RuntimePanel = memo(function RuntimePanel({
  summary,
  runtime,
  items = []
}: {
  summary: string;
  runtime?: MonitorRuntime;
  items?: Array<{ label: string; value: string | null | undefined }>;
}) {
  const allItems = useMemo(() => [
    { label: "环境", value: runtime?.environment ?? null },
    { label: "端口", value: runtime?.port ? String(runtime.port) : null },
    { label: "运行时长", value: fmtUptime(runtime?.uptimeSeconds) },
    { label: "启动时间", value: fmtDate(runtime?.startedAt) },
    { label: "时区", value: runtime?.timezone ?? null },
    { label: "检测时间", value: fmtDate(runtime?.checkedAt) },
    ...items,
  ].filter((i) => i.value), [runtime, items]);

  return (
    <div className="space-y-3">
      <p className="text-sm text-muted-foreground">{summary}</p>
      <div className="grid gap-2 grid-cols-2 lg:grid-cols-4">
        {allItems.map((i) => (
          <div key={i.label} className="rounded-lg border bg-muted/40 px-3 py-2">
            <div className="text-[10px] font-medium uppercase text-muted-foreground">{i.label}</div>
            <div className={cn("text-sm font-medium", DATA_FONT)}>{i.value}</div>
          </div>
        ))}
      </div>
    </div>
  );
});

/* ------------------------------------------------------------------ */
/*  ServiceCardGrid — 卡片网格                                          */
/* ------------------------------------------------------------------ */

const ServiceCardGrid = memo(function ServiceCardGrid({
  items,
  historyMap
}: {
  items: MonitorComponent[];
  historyMap: Record<string, HistoryPoint[]>;
}) {
  return (
    <div className="grid gap-3 md:grid-cols-2 2xl:grid-cols-3">
      {items.map((item) => (
        <ServiceCard key={item.key} item={item} history={historyMap[item.key] || []} />
      ))}
    </div>
  );
});

/* ------------------------------------------------------------------ */
/*  SectionTrigger                                                     */
/* ------------------------------------------------------------------ */

function SectionTrigger({
  icon: Icon,
  title,
  count,
  status,
  summary
}: {
  icon: React.ComponentType<{ className?: string }>;
  title: string;
  count: number;
  status?: MonitorStatus;
  summary?: string;
}) {
  return (
    <AccordionTrigger className="hover:no-underline py-3 px-4">
      <div className="flex items-center gap-3 flex-1 min-w-0">
        <div className="flex size-7 shrink-0 items-center justify-center rounded-md border bg-muted/50">
          <Icon className="size-3.5 text-muted-foreground" />
        </div>
        <span className="text-sm font-semibold">{title}</span>
        <Badge variant="outline" className="text-[10px]">{count} 项</Badge>
        {status && (
          <Badge variant={statusBadge(status)} className="text-[10px]">
            <StatusDot status={status} />
            <span className="ml-1">{summary || statusLabel(status)}</span>
          </Badge>
        )}
      </div>
    </AccordionTrigger>
  );
}

/* ------------------------------------------------------------------ */
/*  主组件                                                             */
/* ------------------------------------------------------------------ */

export function AvailabilityDashboard({ mode, apps = [], showPublicLinks = true }: AvailabilityDashboardProps) {
  const systemMonitorQuery = useSystemMonitorQuery();
  const systemMonitorAppsQuery = useSystemMonitorAppsQuery();
  const systemMonitorComponentsQuery = useSystemMonitorComponentsQuery();
  const [selectedAppId, setSelectedAppId] = useState<number | null>(null);
  const [manualAppId, setManualAppId] = useState("");
  const [manualAppliedAppId, setManualAppliedAppId] = useState<number | null>(null);
  const [historyRange, setHistoryRange] = useState<"hour" | "day" | "week" | "month">("hour");

  const selectedApp = useMemo(() => {
    if (mode !== "console" || !apps.length) return null;
    return apps.find((i) => i.id === selectedAppId) || apps[0];
  }, [apps, mode, selectedAppId]);

  const activeAppId = mode === "console" ? selectedApp?.id ?? null : manualAppliedAppId;
  const appMonitorQuery = useAppMonitorQuery(activeAppId);
  const appMonitorComponentsQuery = useAppMonitorComponentsQuery(activeAppId);

  const systemMonitor = systemMonitorQuery.data;
  const systemComponents = systemMonitorComponentsQuery.data ?? (systemMonitor ? {
    status: systemMonitor.status, score: systemMonitor.score,
    availabilityRate: systemMonitor.availabilityRate, checkedAt: systemMonitor.checkedAt,
    runtime: systemMonitor.runtime, counts: systemMonitor.counts,
    components: systemMonitor.components, infrastructure: systemMonitor.infrastructure,
    modules: systemMonitor.modules,
  } : undefined);

  const systemApps = systemMonitorAppsQuery.data ?? systemMonitor?.applications ?? [];
  const appMonitor = appMonitorQuery.data;
  const appComponents = appMonitorComponentsQuery.data ?? (appMonitor ? {
    status: appMonitor.status, score: appMonitor.score,
    availabilityRate: appMonitor.availabilityRate, checkedAt: appMonitor.checkedAt,
    runtime: appMonitor.runtime, counts: appMonitor.counts,
    app: appMonitor.app, entrypoints: appMonitor.entrypoints || [],
    infrastructure: appMonitor.infrastructure, modules: appMonitor.modules,
    components: appMonitor.components,
  } : undefined);

  const systemComponentKeys = useMemo(
    () => collectItems([systemComponents?.components, systemComponents?.infrastructure, systemComponents?.modules]).map((c) => c.key),
    [systemComponents]
  );
  const appComponentKeys = useMemo(
    () => collectItems([appComponents?.components, appComponents?.infrastructure, appComponents?.modules, appComponents?.entrypoints]).map((c) => c.key),
    [appComponents]
  );

  const systemHistoryQuery = useSystemMonitorHistoryQuery(systemComponentKeys, historyRange);
  const appHistoryQuery = useAppMonitorHistoryQuery(activeAppId, appComponentKeys, historyRange);

  const systemHistoryMap = useMemo(() => {
    const raw = systemHistoryQuery.data;
    if (!raw) return {} as Record<string, HistoryPoint[]>;
    const r: Record<string, HistoryPoint[]> = {};
    for (const [k, pts] of Object.entries(raw)) r[k] = convertHistory(pts);
    return r;
  }, [systemHistoryQuery.data]);

  const appHistoryMap = useMemo(() => {
    const raw = appHistoryQuery.data;
    if (!raw) return {} as Record<string, HistoryPoint[]>;
    const r: Record<string, HistoryPoint[]> = {};
    for (const [k, pts] of Object.entries(raw)) r[k] = convertHistory(pts);
    return r;
  }, [appHistoryQuery.data]);

  const systemExtraComponents = useMemo(
    () => extraItems(systemComponents?.components || [], [systemComponents?.infrastructure, systemComponents?.modules]),
    [systemComponents]
  );
  const appExtraComponents = useMemo(
    () => extraItems(appComponents?.components || [], [appComponents?.entrypoints, appComponents?.infrastructure, appComponents?.modules]),
    [appComponents]
  );
  const appMetricEntries = useMemo(() => collectPrimEntries(appMonitor?.metrics), [appMonitor?.metrics]);

  const handleRefresh = useCallback(() => {
    const tasks: Array<Promise<unknown>> = [
      systemMonitorQuery.refetch(), systemMonitorAppsQuery.refetch(),
      systemMonitorComponentsQuery.refetch(), systemHistoryQuery.refetch(),
    ];
    if (activeAppId) {
      tasks.push(appMonitorQuery.refetch(), appMonitorComponentsQuery.refetch(), appHistoryQuery.refetch());
    }
    void Promise.allSettled(tasks);
  }, [activeAppId, systemMonitorQuery, systemMonitorAppsQuery, systemMonitorComponentsQuery, systemHistoryQuery, appMonitorQuery, appMonitorComponentsQuery, appHistoryQuery]);

  const handleAppSelect = useCallback((appId: number) => {
    if (mode === "console") { setSelectedAppId(appId); return; }
    setManualAppId(String(appId));
    setManualAppliedAppId(appId);
  }, [mode]);

  if (systemMonitorQuery.isLoading && !systemMonitor) {
    return <LoadingState title="正在加载监测" description="正在读取状态数据。" />;
  }
  if (!systemMonitor || !systemComponents) {
    return (
      <div className="page-stack">
        <SectionHeading eyebrow="状态" title="可用性监测" />
        <EmptyState title="监测数据不可用" description="接口未返回状态数据。" />
      </div>
    );
  }

  const appHeading = mode === "console"
    ? selectedApp?.name ?? "--"
    : appMonitor?.app?.name ?? (activeAppId ? `APP ${activeAppId}` : "--");

  return (
    <div className="space-y-4">
      {/* 标题栏 */}
      <div className="flex items-center justify-between gap-3 flex-wrap">
        <div className="flex items-center gap-2">
          <Activity className="size-5 text-muted-foreground" />
          <h2 className="text-base font-semibold">可用性监测</h2>
          <Badge variant={statusBadge(systemMonitor.status)}>
            <StatusDot status={systemMonitor.status} />
            <span className="ml-1">{statusLabel(systemMonitor.status)}</span>
          </Badge>
        </div>

        <div className="flex items-center gap-2 flex-wrap">
          {mode === "console" && apps.length ? (
            <Select value={String(selectedApp?.id || "")} onValueChange={(v) => setSelectedAppId(Number(v))}>
              <SelectTrigger className="w-50 h-8 text-xs"><SelectValue placeholder="选择应用" /></SelectTrigger>
              <SelectContent>
                {apps.map((i) => <SelectItem key={i.id} value={String(i.id)}>{i.name} ({i.id})</SelectItem>)}
              </SelectContent>
            </Select>
          ) : null}

          {mode === "public" ? (
            <div className="flex items-center gap-1.5">
              <Input inputMode="numeric" placeholder="AppID" value={manualAppId}
                onChange={(e) => setManualAppId(e.target.value.replace(/[^\d]/g, ""))}
                className="w-24 h-8 text-xs" />
              <Button variant="outline" size="sm" onClick={() => setManualAppliedAppId(manualAppId ? Number(manualAppId) : null)}>
                <Search className="size-3.5" />
              </Button>
            </div>
          ) : null}

          <div className="flex items-center gap-0.5 rounded-lg border bg-muted/50 p-0.5">
            {(["hour", "day", "week", "month"] as const).map((r) => (
              <button key={r} type="button"
                className={`rounded-md px-2 py-1 text-[10px] font-medium transition-colors ${historyRange === r ? "bg-background text-foreground shadow-sm" : "text-muted-foreground hover:text-foreground"}`}
                onClick={() => setHistoryRange(r)}>
                {{ hour: "1H", day: "24H", week: "7D", month: "30D" }[r]}
              </button>
            ))}
          </div>

          <Button variant="outline" size="sm" onClick={handleRefresh}>
            <RefreshCw className="size-3.5" />
          </Button>
        </div>
      </div>

      {mode === "public" && showPublicLinks ? (
        <div className="flex flex-wrap items-center gap-2">
          <Button asChild variant="outline" size="sm"><Link href="/">首页</Link></Button>
          <Button asChild variant="outline" size="sm"><Link href="/login">登录</Link></Button>
        </div>
      ) : null}

      {/* 摘要 */}
      <div className="grid gap-3 grid-cols-2 lg:grid-cols-4">
        {[
          { label: "系统状态", value: statusLabel(systemMonitor.status), hint: `健康分 ${systemMonitor.score}` },
          { label: "组件总数", value: fmtCount(systemComponents.counts.total), hint: `正常 ${fmtCount(systemComponents.counts.available)}`, mono: true },
          { label: "公开端点", value: fmtCount(systemMonitor.endpoints?.length), hint: `可用率 ${fmtPct(systemMonitor.availabilityRate)}`, mono: true },
          { label: "应用总数", value: fmtCount(systemApps.length), hint: activeAppId ? `当前 APP ${activeAppId}` : "未选择应用", mono: true },
        ].map((s) => (
          <div key={s.label} className="rounded-xl border bg-card px-4 py-3 space-y-1">
            <div className="text-[10px] font-medium uppercase tracking-widest text-muted-foreground">{s.label}</div>
            <div className={cn("text-lg font-semibold", s.mono && DATA_FONT)}>{s.value}</div>
            <div className="text-xs text-muted-foreground">{s.hint}</div>
          </div>
        ))}
      </div>

      {/* Accordion 各分区（默认全部收缩，LazyAccordionContent 延迟渲染） */}
      <Accordion type="multiple" className="space-y-2">
        <AccordionItem value="system-runtime" className="rounded-xl border overflow-hidden border-b-0">
          <SectionTrigger icon={ServerCog} title="系统运行时" count={systemComponents.counts.total}
            status={systemMonitor.status} summary={`健康分 ${systemMonitor.score}`} />
          <LazyAccordionContent className="px-4">
            <RuntimePanel summary={systemMonitor.summary} runtime={systemMonitor.runtime}
              items={[
                { label: "状态", value: statusLabel(systemMonitor.status) },
                { label: "健康分", value: String(systemMonitor.score) },
                { label: "可用率", value: fmtPct(systemMonitor.availabilityRate) },
                { label: "组件总数", value: fmtCount(systemComponents.counts.total) },
              ]} />
          </LazyAccordionContent>
        </AccordionItem>

        {systemApps.length > 0 && (
          <AccordionItem value="app-overview" className="rounded-xl border overflow-hidden border-b-0">
            <SectionTrigger icon={LayoutGrid} title="应用总览" count={systemApps.length} />
            <LazyAccordionContent className="px-4">
              <div className="grid gap-2 md:grid-cols-2 2xl:grid-cols-3">
                {systemApps.map((i) => (
                  <AppOverviewCard key={i.id} item={i} active={i.id === activeAppId} onSelect={handleAppSelect} />
                ))}
              </div>
            </LazyAccordionContent>
          </AccordionItem>
        )}

        {(systemMonitor.endpoints || []).length > 0 && (
          <AccordionItem value="endpoints" className="rounded-xl border overflow-hidden border-b-0">
            <SectionTrigger icon={Route} title="公开端点" count={(systemMonitor.endpoints || []).length} />
            <LazyAccordionContent className="px-4">
              <div className="grid gap-2 lg:grid-cols-2">
                {(systemMonitor.endpoints || []).map((i) => <EndpointCard key={i.key} item={i} />)}
              </div>
            </LazyAccordionContent>
          </AccordionItem>
        )}

        {systemComponents.infrastructure.length > 0 && (
          <AccordionItem value="infrastructure" className="rounded-xl border overflow-hidden border-b-0">
            <SectionTrigger icon={Database} title="基础设施" count={systemComponents.infrastructure.length}
              status={sectionStatus(systemComponents.infrastructure)} summary={sectionSummary(systemComponents.infrastructure)} />
            <LazyAccordionContent className="px-4">
              <ServiceCardGrid items={systemComponents.infrastructure} historyMap={systemHistoryMap} />
            </LazyAccordionContent>
          </AccordionItem>
        )}

        {systemComponents.modules.length > 0 && (
          <AccordionItem value="modules" className="rounded-xl border overflow-hidden border-b-0">
            <SectionTrigger icon={Cpu} title="系统模块" count={systemComponents.modules.length}
              status={sectionStatus(systemComponents.modules)} summary={sectionSummary(systemComponents.modules)} />
            <LazyAccordionContent className="px-4">
              <ServiceCardGrid items={systemComponents.modules} historyMap={systemHistoryMap} />
            </LazyAccordionContent>
          </AccordionItem>
        )}

        {systemExtraComponents.length > 0 && (
          <AccordionItem value="extra" className="rounded-xl border overflow-hidden border-b-0">
            <SectionTrigger icon={Layers} title="其他组件" count={systemExtraComponents.length}
              status={sectionStatus(systemExtraComponents)} summary={sectionSummary(systemExtraComponents)} />
            <LazyAccordionContent className="px-4">
              <ServiceCardGrid items={systemExtraComponents} historyMap={systemHistoryMap} />
            </LazyAccordionContent>
          </AccordionItem>
        )}

        {/* 应用详情 */}
        {activeAppId && appMonitor && appComponents ? (
          <>
            <AccordionItem value="app-runtime" className="rounded-xl border overflow-hidden border-b-0">
              <SectionTrigger icon={AppWindow} title={`${appHeading} 运行时`} count={appComponents.counts.total}
                status={appMonitor.status} summary={`健康分 ${appMonitor.score}`} />
              <LazyAccordionContent className="px-4">
                <RuntimePanel summary={appMonitor.summary} runtime={appMonitor.runtime}
                  items={[
                    { label: "状态", value: statusLabel(appMonitor.status) },
                    { label: "健康分", value: String(appMonitor.score) },
                    { label: "可用率", value: fmtPct(appMonitor.availabilityRate) },
                    { label: "应用 ID", value: String(appMonitor.app.id) },
                    { label: "应用状态", value: appMonitor.app.status ? "开启" : "关闭" },
                    { label: "注册", value: appMonitor.app.registerStatus ? "开启" : "关闭" },
                    { label: "登录", value: appMonitor.app.loginStatus ? "开启" : "关闭" },
                    ...appMetricEntries.slice(0, 4),
                  ]} />
              </LazyAccordionContent>
            </AccordionItem>

            {(appComponents.entrypoints || []).length > 0 && (
              <AccordionItem value="app-entrypoints" className="rounded-xl border overflow-hidden border-b-0">
                <SectionTrigger icon={Route} title="应用入口" count={(appComponents.entrypoints || []).length}
                  status={sectionStatus(appComponents.entrypoints || [])} summary={sectionSummary(appComponents.entrypoints || [])} />
                <LazyAccordionContent className="px-4">
                  <ServiceCardGrid items={appComponents.entrypoints || []} historyMap={appHistoryMap} />
                </LazyAccordionContent>
              </AccordionItem>
            )}

            {appComponents.infrastructure.length > 0 && (
              <AccordionItem value="app-infra" className="rounded-xl border overflow-hidden border-b-0">
                <SectionTrigger icon={Database} title="应用基础设施" count={appComponents.infrastructure.length}
                  status={sectionStatus(appComponents.infrastructure)} summary={sectionSummary(appComponents.infrastructure)} />
                <LazyAccordionContent className="px-4">
                  <ServiceCardGrid items={appComponents.infrastructure} historyMap={appHistoryMap} />
                </LazyAccordionContent>
              </AccordionItem>
            )}

            {appComponents.modules.length > 0 && (
              <AccordionItem value="app-modules" className="rounded-xl border overflow-hidden border-b-0">
                <SectionTrigger icon={Cpu} title="应用模块" count={appComponents.modules.length}
                  status={sectionStatus(appComponents.modules)} summary={sectionSummary(appComponents.modules)} />
                <LazyAccordionContent className="px-4">
                  <ServiceCardGrid items={appComponents.modules} historyMap={appHistoryMap} />
                </LazyAccordionContent>
              </AccordionItem>
            )}

            {appExtraComponents.length > 0 && (
              <AccordionItem value="app-extra" className="rounded-xl border overflow-hidden border-b-0">
                <SectionTrigger icon={Layers} title="其他应用组件" count={appExtraComponents.length}
                  status={sectionStatus(appExtraComponents)} summary={sectionSummary(appExtraComponents)} />
                <LazyAccordionContent className="px-4">
                  <ServiceCardGrid items={appExtraComponents} historyMap={appHistoryMap} />
                </LazyAccordionContent>
              </AccordionItem>
            )}
          </>
        ) : null}
      </Accordion>
    </div>
  );
}
