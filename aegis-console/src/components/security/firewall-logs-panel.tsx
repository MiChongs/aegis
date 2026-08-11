"use client";

import { useCallback, useMemo, useState } from "react";
import {
  AlertTriangle,
  ChevronLeft,
  ChevronRight,
  ExternalLink,
  Globe,
  Loader2,
  MapPin,
  Network,
  RefreshCw,
  RotateCcw,
  Search,
  Shield,
  ShieldAlert,
  ShieldX,
  Trash2
} from "lucide-react";
import { Area, AreaChart, CartesianGrid, Tooltip, XAxis, YAxis, Bar, BarChart } from "recharts";
import { toast } from "sonner";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { ChartContainer, type ChartConfig } from "@/components/ui/chart";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Separator } from "@/components/ui/separator";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { EmptyState } from "@/components/ui/data-state";
import { SurfaceCard } from "@/components/ui/surface-card";
import {
  Tooltip as UITooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger
} from "@/components/ui/tooltip";

import {
  useFirewallLogsQuery,
  useFirewallLogDetailQuery,
  useFirewallStatsQuery,
  useDeleteFirewallLogsMutation
} from "@/lib/admin-hooks";
import { AttackFlightMap } from "@/components/security/attack-flight-map";
import type { FirewallLogEntry, FirewallRankedItem } from "@/lib/api/types";
import type { FirewallLogListParams } from "@/lib/api/system";

// ── 常量映射 ──────────────────────────

const severityConfig: Record<string, { label: string; variant: "danger" | "warning" | "secondary" | "outline"; dot: string }> = {
  critical: { label: "严重", variant: "danger", dot: "bg-red-500" },
  high: { label: "高危", variant: "warning", dot: "bg-amber-500" },
  medium: { label: "中等", variant: "secondary", dot: "bg-zinc-400" },
  low: { label: "低危", variant: "outline", dot: "bg-zinc-300 dark:bg-zinc-600" }
};

const sevTooltipLabel: Record<string, string> = { critical: "严重", high: "高危", medium: "中等", low: "低危" };

const reasonLabels: Record<string, string> = {
  rate_limited: "速率限制",
  waf_blocked: "WAF 拦截",
  blocked_cidr: "CIDR 黑名单",
  not_in_allowlist: "不在白名单",
  blocked_user_agent: "UA 屏蔽",
  blocked_path: "路径屏蔽",
  blocked_signature: "危险签名",
  blocked_method: "方法禁止",
  path_too_long: "路径过长",
  query_too_long: "查询过长",
  invalid_ip: "无效 IP",
  waf_processing_error: "WAF 错误"
};

const methodColors: Record<string, string> = {
  GET: "text-emerald-600 dark:text-emerald-400",
  POST: "text-blue-600 dark:text-blue-400",
  PUT: "text-amber-600 dark:text-amber-400",
  DELETE: "text-red-600 dark:text-red-400",
  PATCH: "text-violet-600 dark:text-violet-400"
};

const timeRangeOptions = [
  { label: "1h", value: "1h", hours: 1 },
  { label: "6h", value: "6h", hours: 6 },
  { label: "24h", value: "24h", hours: 24 },
  { label: "7d", value: "7d", hours: 168 },
  { label: "30d", value: "30d", hours: 720 }
];

const cleanupOptions = [
  { label: "保留最近 7 天", days: 7 },
  { label: "保留最近 30 天", days: 30 },
  { label: "保留最近 90 天", days: 90 }
];

const chartConfig: ChartConfig = {
  count: { label: "拦截次数", color: "var(--color-destructive, #ef4444)" }
};

function formatTime(iso: string) {
  try {
    return new Date(iso).toLocaleString("zh-CN", {
      month: "2-digit",
      day: "2-digit",
      hour: "2-digit",
      minute: "2-digit",
      second: "2-digit"
    });
  } catch {
    return iso;
  }
}

function formatChartTime(iso: string) {
  try {
    return new Date(iso).toLocaleString("zh-CN", { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" });
  } catch {
    return iso;
  }
}

function getTimeRange(rangeKey: string) {
  const opt = timeRangeOptions.find((o) => o.value === rangeKey);
  const hours = opt?.hours ?? 24;
  const end = new Date();
  const start = new Date(end.getTime() - hours * 3600_000);
  return { startTime: start.toISOString(), endTime: end.toISOString(), granularity: hours > 48 ? "day" : "hour" };
}

function geoLabel(log: Pick<FirewallLogEntry, "country" | "city" | "region" | "countryCode">) {
  const parts = [log.country, log.region, log.city].filter(Boolean);
  return parts.length > 0 ? parts.join(" · ") : null;
}

// ── 主组件 ──────────────────────────

export function FirewallLogsPanel() {
  const [timeRange, setTimeRange] = useState("24h");
  const [filters, setFilters] = useState<FirewallLogListParams>({ page: 1, pageSize: 20 });
  const [filterIP, setFilterIP] = useState("");
  const [filterReason, setFilterReason] = useState("all");
  const [filterSeverity, setFilterSeverity] = useState("all");
  const [filterPathPattern, setFilterPathPattern] = useState("");
  const [selectedLogId, setSelectedLogId] = useState<number | null>(null);
  const [showCleanup, setShowCleanup] = useState(false);
  const [cleanupDays, setCleanupDays] = useState(30);

  const timeParams = useMemo(() => getTimeRange(timeRange), [timeRange]);

  const queryParams = useMemo<FirewallLogListParams>(() => {
    const p: FirewallLogListParams = {
      page: filters.page,
      pageSize: filters.pageSize,
      startTime: timeParams.startTime,
      endTime: timeParams.endTime,
      sortBy: "blocked_at",
      sortOrder: "desc"
    };
    if (filterIP.trim()) p.ip = filterIP.trim();
    if (filterReason !== "all") p.reason = filterReason;
    if (filterSeverity !== "all") p.severity = filterSeverity;
    if (filterPathPattern.trim()) p.pathPattern = filterPathPattern.trim();
    return p;
  }, [filters, timeParams, filterIP, filterReason, filterSeverity, filterPathPattern]);

  const statsParams = useMemo(
    () => ({ startTime: timeParams.startTime, endTime: timeParams.endTime, granularity: timeParams.granularity }),
    [timeParams]
  );

  const logsQuery = useFirewallLogsQuery(queryParams);
  const statsQuery = useFirewallStatsQuery(statsParams);
  const detailQuery = useFirewallLogDetailQuery(selectedLogId);
  const deleteMutation = useDeleteFirewallLogsMutation();

  const logData = logsQuery.data;
  const stats = statsQuery.data;

  const handleSearch = useCallback(() => {
    setFilters((prev) => ({ ...prev, page: 1 }));
  }, []);

  const handleReset = useCallback(() => {
    setFilterIP("");
    setFilterReason("all");
    setFilterSeverity("all");
    setFilterPathPattern("");
    setFilters({ page: 1, pageSize: 20 });
  }, []);

  const handlePageChange = useCallback((newPage: number) => {
    setFilters((prev) => ({ ...prev, page: newPage }));
  }, []);

  const handleCleanup = useCallback(async () => {
    const before = new Date(Date.now() - cleanupDays * 86400_000).toISOString();
    try {
      const result = await deleteMutation.mutateAsync(before);
      toast.success(`已清理 ${result.deleted} 条日志`);
      setShowCleanup(false);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "清理失败");
    }
  }, [cleanupDays, deleteMutation]);

  const severityCounts = useMemo(() => {
    const map: Record<string, number> = {};
    stats?.severityCounts?.forEach((s) => { map[s.key] = s.count; });
    return map;
  }, [stats]);

  const hasActiveFilters = filterIP || filterReason !== "all" || filterSeverity !== "all" || filterPathPattern;

  return (
    <TooltipProvider delayDuration={200}>
      <div className="space-y-5">
        {/* ── 统计卡片 + 时间范围 ── */}
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-1 rounded-lg border bg-muted/50 p-0.5">
            {timeRangeOptions.map((opt) => (
              <button
                key={opt.value}
                className={`rounded-md px-2.5 py-1 text-xs font-medium transition-colors ${
                  timeRange === opt.value
                    ? "bg-background text-foreground shadow-sm"
                    : "text-muted-foreground hover:text-foreground"
                }`}
                onClick={() => setTimeRange(opt.value)}
              >
                {opt.label}
              </button>
            ))}
          </div>
          <div className="flex items-center gap-2">
            <Button
              size="sm"
              variant="ghost"
              className="h-7 gap-1 text-xs"
              onClick={() => { logsQuery.refetch(); statsQuery.refetch(); }}
              disabled={logsQuery.isFetching}
            >
              <RefreshCw className={`size-3 ${logsQuery.isFetching ? "animate-spin" : ""}`} />
              刷新
            </Button>
            <Button size="sm" variant="ghost" className="h-7 gap-1 text-xs text-destructive hover:text-destructive" onClick={() => setShowCleanup(true)}>
              <Trash2 className="size-3" />
              清理
            </Button>
          </div>
        </div>

        {/* ── 统计指标 ── */}
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
          <StatCard
            label="总拦截"
            value={stats?.totalBlocked ?? 0}
            icon={<Shield className="size-4" />}
            loading={statsQuery.isLoading}
          />
          <StatCard
            label="严重"
            value={severityCounts.critical ?? 0}
            icon={<ShieldX className="size-4" />}
            accent="text-red-600 dark:text-red-400"
            iconAccent="text-red-500"
            loading={statsQuery.isLoading}
          />
          <StatCard
            label="高危"
            value={severityCounts.high ?? 0}
            icon={<ShieldAlert className="size-4" />}
            accent="text-amber-600 dark:text-amber-400"
            iconAccent="text-amber-500"
            loading={statsQuery.isLoading}
          />
          <StatCard
            label="中/低危"
            value={(severityCounts.medium ?? 0) + (severityCounts.low ?? 0)}
            icon={<AlertTriangle className="size-4" />}
            loading={statsQuery.isLoading}
          />
        </div>

        {/* ── 攻击飞行图 ── */}
        <AttackFlightMap timeRange={timeParams} />

        {/* ── 图表 + 排行 ── */}
        <div className="grid gap-4 lg:grid-cols-3">
          {/* 趋势图 - 占 2 列 */}
          <Card className="flex flex-col overflow-hidden lg:col-span-2">
            <CardContent className="flex min-h-0 flex-1 flex-col p-4 pb-2">
            <h3 className="mb-2 shrink-0 text-[10px] font-medium uppercase tracking-wider text-muted-foreground">拦截趋势</h3>
            {statsQuery.isLoading ? (
              <div className="min-h-[180px] flex-1"><Skeleton className="h-full w-full rounded-lg" /></div>
            ) : stats?.timeSeries && stats.timeSeries.length > 0 ? (
              <div className="min-h-[180px] flex-1">
              <ChartContainer config={chartConfig} className="h-full w-full">
                <BarChart data={stats.timeSeries.map((p) => ({ time: formatChartTime(p.time), critical: p.critical, high: p.high, medium: p.medium, low: p.low }))} margin={{ top: 8, right: 8, bottom: 0, left: -12 }}>
                  <CartesianGrid vertical={false} strokeDasharray="3 3" className="stroke-border/40" />
                  <XAxis
                    dataKey="time"
                    tick={{ fontSize: 10, fill: "var(--muted-foreground)" }}
                    axisLine={false}
                    tickLine={false}
                    tickMargin={8}
                    minTickGap={50}
                  />
                  <YAxis
                    tick={{ fontSize: 10, fill: "var(--muted-foreground)" }}
                    axisLine={false}
                    tickLine={false}
                    allowDecimals={false}
                    width={36}
                  />
                  <Tooltip
                    cursor={{ fill: "var(--muted)", opacity: 0.3 }}
                    content={({ active, payload, label }) => {
                      if (!active || !payload?.length) return null;
                      const total = payload.reduce((s, p) => s + (Number(p.value) || 0), 0);
                      return (
                        <div className="rounded-lg border bg-popover px-3 py-2 shadow-md">
                          <div className="text-[11px] text-muted-foreground">{label}</div>
                          <div className="mt-1 space-y-0.5">
                            {payload.filter(p => Number(p.value) > 0).reverse().map((p) => (
                              <div key={p.dataKey as string} className="flex items-center justify-between gap-4 text-xs">
                                <span className="flex items-center gap-1.5">
                                  <span className="inline-block size-2 rounded-sm" style={{ background: p.color }} />
                                  {sevTooltipLabel[String(p.dataKey)] || String(p.dataKey)}
                                </span>
                                <span className="font-semibold tabular-nums">{Number(p.value).toLocaleString()}</span>
                              </div>
                            ))}
                          </div>
                          <div className="mt-1.5 border-t pt-1.5 text-xs font-semibold tabular-nums">
                            合计 {total.toLocaleString()}
                          </div>
                        </div>
                      );
                    }}
                  />
                  <Bar dataKey="critical" stackId="a" fill="#ef4444" radius={[0, 0, 0, 0]} />
                  <Bar dataKey="high" stackId="a" fill="#f97316" radius={[0, 0, 0, 0]} />
                  <Bar dataKey="medium" stackId="a" fill="#eab308" radius={[0, 0, 0, 0]} />
                  <Bar dataKey="low" stackId="a" fill="#a1a1aa" radius={[2, 2, 0, 0]} />
                </BarChart>
              </ChartContainer>
              </div>
            ) : (
              <div className="flex min-h-[180px] flex-1 items-center justify-center text-xs text-muted-foreground">暂无趋势数据</div>
            )}
            </CardContent>
          </Card>

          {/* 排行榜 - 占 1 列，纵向堆叠 */}
          <div className="space-y-3">
            <CompactRankList title="Top IPs" icon={<Network className="size-3" />} items={stats?.topIPs} loading={statsQuery.isLoading} />
            <CompactRankList title="Top 原因" icon={<ShieldAlert className="size-3" />} items={stats?.topReasons} labelMap={reasonLabels} loading={statsQuery.isLoading} />
            <CompactRankList title="Top 国家" icon={<Globe className="size-3" />} items={stats?.topCountries} loading={statsQuery.isLoading} />
          </div>
        </div>

        {/* ── 过滤器 ── */}
        <div className="flex flex-wrap items-end gap-2.5 rounded-xl border bg-muted/30 px-4 py-3">
          <FilterField label="IP 地址">
            <Input placeholder="1.2.3.4" className="h-8 w-32 text-xs" value={filterIP} onChange={(e) => setFilterIP(e.target.value)} />
          </FilterField>
          <FilterField label="原因">
            <Select value={filterReason} onValueChange={setFilterReason}>
              <SelectTrigger className="h-8 w-28 text-xs"><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="all">全部</SelectItem>
                {Object.entries(reasonLabels).map(([k, v]) => <SelectItem key={k} value={k}>{v}</SelectItem>)}
              </SelectContent>
            </Select>
          </FilterField>
          <FilterField label="严重性">
            <Select value={filterSeverity} onValueChange={setFilterSeverity}>
              <SelectTrigger className="h-8 w-24 text-xs"><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="all">全部</SelectItem>
                {Object.entries(severityConfig).map(([k, v]) => <SelectItem key={k} value={k}>{v.label}</SelectItem>)}
              </SelectContent>
            </Select>
          </FilterField>
          <FilterField label="路径">
            <Input placeholder="/api/auth" className="h-8 w-32 text-xs" value={filterPathPattern} onChange={(e) => setFilterPathPattern(e.target.value)} />
          </FilterField>
          <div className="flex gap-1.5 pb-px">
            <Button size="sm" className="h-8 gap-1 text-xs" onClick={handleSearch}>
              <Search className="size-3" />
              搜索
            </Button>
            {hasActiveFilters && (
              <Button size="sm" variant="ghost" className="h-8 gap-1 text-xs" onClick={handleReset}>
                <RotateCcw className="size-3" />
                重置
              </Button>
            )}
          </div>
        </div>

        {/* ── 日志表格 ── */}
        {logsQuery.isLoading ? (
          <SurfaceCard>
            <div className="space-y-3">
              {Array.from({ length: 6 }).map((_, i) => (
                <Skeleton key={i} className="h-10 w-full rounded-lg" />
              ))}
            </div>
          </SurfaceCard>
        ) : !logData?.items?.length ? (
          <EmptyState title="暂无日志" description="当前时间范围与过滤条件下没有防火墙拦截记录" />
        ) : (
          <Card className="overflow-hidden">
            <div className="overflow-x-auto">
              <Table>
                <TableHeader>
                  <TableRow className="hover:bg-transparent">
                    <TableHead className="h-8 w-28 pl-3 text-[11px]">时间</TableHead>
                    <TableHead className="h-8 w-28 text-[11px]">IP</TableHead>
                    <TableHead className="h-8 text-[11px]">请求</TableHead>
                    <TableHead className="h-8 w-24 text-[11px]">位置</TableHead>
                    <TableHead className="h-8 w-20 text-[11px]">原因</TableHead>
                    <TableHead className="h-8 w-12 text-[11px]">级别</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {logData.items.map((log) => (
                    <TableRow
                      key={log.id}
                      className="cursor-pointer transition-colors hover:bg-muted/40"
                      onClick={() => setSelectedLogId(log.id)}
                    >
                      <TableCell className="py-1.5 pl-3 text-[11px] tabular-nums text-muted-foreground">
                        {formatTime(log.blockedAt)}
                      </TableCell>
                      <TableCell className="py-1.5 font-mono text-[11px]">{log.ip}</TableCell>
                      <TableCell className="py-1.5">
                        <span className="inline-flex items-center gap-1.5">
                          <span className={`font-mono text-[11px] font-medium ${methodColors[log.method] ?? "text-foreground"}`}>{log.method}</span>
                          <UITooltip>
                            <TooltipTrigger asChild>
                              <span className="max-w-[280px] truncate font-mono text-[11px] text-muted-foreground">{log.path}</span>
                            </TooltipTrigger>
                            <TooltipContent side="top" className="max-w-sm break-all font-mono text-xs">
                              {log.path}{log.queryString ? `?${log.queryString}` : ""}
                            </TooltipContent>
                          </UITooltip>
                        </span>
                      </TableCell>
                      <TableCell className="py-1.5 text-[11px] text-muted-foreground">
                        <UITooltip>
                          <TooltipTrigger asChild>
                            <span className="inline-flex max-w-[100px] items-center gap-1 truncate">
                              {log.countryCode && <span className="shrink-0 text-[10px] uppercase text-muted-foreground/60">{log.countryCode}</span>}
                              {[log.city || log.region].filter(Boolean)[0] || "—"}
                            </span>
                          </TooltipTrigger>
                          {geoLabel(log) && (
                            <TooltipContent side="top" className="text-xs">
                              <div>{geoLabel(log)}</div>
                              {log.isp && <div className="text-muted-foreground">{log.isp}</div>}
                            </TooltipContent>
                          )}
                        </UITooltip>
                      </TableCell>
                      <TableCell className="py-1.5 text-[11px]">{reasonLabels[log.reason] ?? log.reason}</TableCell>
                      <TableCell className="py-1.5">
                        <span className={`inline-block size-2 rounded-full ${severityConfig[log.severity]?.dot ?? "bg-zinc-400"}`} title={severityConfig[log.severity]?.label ?? log.severity} />
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>

            {/* 分页 */}
            <div className="flex items-center justify-between border-t px-4 py-2.5">
              <span className="text-xs tabular-nums text-muted-foreground">
                {logData.total.toLocaleString()} 条 · 第 {logData.page}/{logData.totalPages || 1} 页
              </span>
              <div className="flex items-center gap-1">
                <Button size="icon" variant="ghost" className="size-7" disabled={logData.page <= 1} onClick={() => handlePageChange(logData.page - 1)}>
                  <ChevronLeft className="size-3.5" />
                </Button>
                <span className="w-8 text-center text-xs tabular-nums text-muted-foreground">{logData.page}</span>
                <Button size="icon" variant="ghost" className="size-7" disabled={logData.page >= logData.totalPages} onClick={() => handlePageChange(logData.page + 1)}>
                  <ChevronRight className="size-3.5" />
                </Button>
              </div>
            </div>
          </Card>
        )}

        {/* ── 详情弹窗 ── */}
        <Dialog open={selectedLogId !== null} onOpenChange={(open) => !open && setSelectedLogId(null)}>
          <DialogContent className="max-h-[85vh] max-w-xl overflow-y-auto p-0">
            <DialogHeader className="sticky top-0 z-10 border-b bg-background px-5 py-4">
              <DialogTitle className="text-sm">拦截日志详情</DialogTitle>
              <DialogDescription className="font-mono text-[11px]">#{selectedLogId}</DialogDescription>
            </DialogHeader>
            {detailQuery.isLoading ? (
              <div className="flex items-center justify-center gap-2 py-16 text-xs text-muted-foreground">
                <Loader2 className="size-4 animate-spin" /> 加载中...
              </div>
            ) : detailQuery.data ? (
              <div className="px-5 py-4">
                <LogDetail log={detailQuery.data} />
              </div>
            ) : null}
          </DialogContent>
        </Dialog>

        {/* ── 清理弹窗 ── */}
        <Dialog open={showCleanup} onOpenChange={setShowCleanup}>
          <DialogContent className="max-w-sm">
            <DialogHeader>
              <DialogTitle className="text-sm">清理防火墙日志</DialogTitle>
              <DialogDescription>删除指定时间之前的拦截日志，此操作不可撤销。</DialogDescription>
            </DialogHeader>
            <Select value={String(cleanupDays)} onValueChange={(v) => setCleanupDays(Number(v))}>
              <SelectTrigger><SelectValue /></SelectTrigger>
              <SelectContent>
                {cleanupOptions.map((opt) => <SelectItem key={opt.days} value={String(opt.days)}>{opt.label}</SelectItem>)}
              </SelectContent>
            </Select>
            <DialogFooter>
              <Button variant="ghost" size="sm" onClick={() => setShowCleanup(false)}>取消</Button>
              <Button variant="destructive" size="sm" disabled={deleteMutation.isPending} onClick={handleCleanup}>
                {deleteMutation.isPending ? <><Loader2 className="mr-1 size-3 animate-spin" />清理中...</> : "确认清理"}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      </div>
    </TooltipProvider>
  );
}

// ── 子组件 ──────────────────────────

function FilterField({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="space-y-1">
      <label className="text-[10px] font-medium uppercase tracking-wider text-muted-foreground">{label}</label>
      {children}
    </div>
  );
}

function StatCard({
  label,
  value,
  icon,
  accent,
  iconAccent,
  loading
}: {
  label: string;
  value: number;
  icon: React.ReactNode;
  accent?: string;
  iconAccent?: string;
  loading?: boolean;
}) {
  return (
    <Card>
      <CardContent className="flex items-center gap-3 p-4">
        <div className={`flex size-8 shrink-0 items-center justify-center rounded-lg bg-muted ${iconAccent ?? "text-muted-foreground"}`}>
          {icon}
        </div>
        <div className="min-w-0">
          <p className="text-[10px] font-medium uppercase tracking-wider text-muted-foreground">{label}</p>
          {loading ? (
            <Skeleton className="mt-1 h-6 w-12 rounded" />
          ) : (
            <p className={`text-xl font-semibold tabular-nums leading-tight ${accent ?? "text-foreground"}`}>
              {value.toLocaleString()}
            </p>
          )}
        </div>
      </CardContent>
    </Card>
  );
}

function SeverityDot({ severity }: { severity: string }) {
  const cfg = severityConfig[severity] ?? severityConfig.medium;
  return (
    <UITooltip>
      <TooltipTrigger asChild>
        <span className="inline-flex items-center gap-1.5">
          <span className={`inline-block size-2 rounded-full ${cfg.dot}`} />
          <span className="text-[10px] text-muted-foreground">{cfg.label}</span>
        </span>
      </TooltipTrigger>
      <TooltipContent side="top" className="text-xs">
        严重性：{cfg.label}
      </TooltipContent>
    </UITooltip>
  );
}

function CompactRankList({
  title,
  icon,
  items,
  labelMap,
  loading
}: {
  title: string;
  icon: React.ReactNode;
  items?: FirewallRankedItem[];
  labelMap?: Record<string, string>;
  loading?: boolean;
}) {
  const list = items?.slice(0, 3) ?? [];
  const total = items?.reduce((sum, i) => sum + i.count, 0) ?? 0;

  return (
    <div className="rounded-xl border bg-background p-3">
      <div className="mb-2 flex items-center gap-1.5">
        <span className="text-muted-foreground">{icon}</span>
        <h4 className="text-[10px] font-medium uppercase tracking-wider text-muted-foreground">{title}</h4>
      </div>
      {loading ? (
        <div className="space-y-1.5">
          <Skeleton className="h-4 w-full rounded" />
          <Skeleton className="h-4 w-3/4 rounded" />
        </div>
      ) : list.length === 0 ? (
        <p className="text-[11px] text-muted-foreground">暂无数据</p>
      ) : (
        <div className="space-y-1.5">
          {list.map((item, i) => {
            const pct = total > 0 ? (item.count / total) * 100 : 0;
            return (
              <div key={item.key || i} className="group relative">
                {/* 进度条背景 */}
                <div className="absolute inset-0 rounded" style={{ width: `${pct}%`, background: "var(--muted)", opacity: 0.5 }} />
                <div className="relative flex items-center justify-between gap-2 px-1.5 py-0.5">
                  <span className="max-w-[120px] truncate text-[11px] text-foreground" title={item.key}>
                    {labelMap?.[item.key] ?? (item.key || "—")}
                  </span>
                  <span className="shrink-0 text-[10px] tabular-nums text-muted-foreground">{item.count}</span>
                </div>
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}

function LogDetail({ log }: { log: FirewallLogEntry }) {
  const geo = geoLabel(log);

  return (
    <div className="space-y-4">
      {/* 头部概览 */}
      <div className="flex flex-wrap items-center gap-2">
        <SeverityBadge severity={log.severity} />
        <Badge variant="outline" className="gap-1 text-[10px]">
          <span className={`font-mono font-semibold ${methodColors[log.method] ?? ""}`}>{log.method}</span>
          {log.path}
        </Badge>
      </div>

      <Separator />

      {/* 请求信息 */}
      <DetailSection title="请求信息" icon={<ExternalLink className="size-3" />}>
        <DetailRow label="请求 ID" value={log.requestId} mono />
        <DetailRow label="时间" value={new Date(log.blockedAt).toLocaleString("zh-CN")} />
        {log.queryString && <DetailRow label="查询参数" value={log.queryString} mono />}
        <DetailRow label="User-Agent" value={log.userAgent} mono small />
      </DetailSection>

      {/* 拦截详情 */}
      <DetailSection title="拦截详情" icon={<Shield className="size-3" />}>
        <DetailRow label="原因" value={reasonLabels[log.reason] ?? log.reason} />
        <DetailRow label="HTTP 状态" value={`${log.httpStatus} (code: ${log.responseCode})`} mono />
        {log.wafRuleId != null && (
          <>
            <DetailRow label="WAF 规则" value={String(log.wafRuleId)} mono />
            {log.wafAction && <DetailRow label="WAF 动作" value={log.wafAction} />}
            {log.wafData && <DetailRow label="WAF 数据" value={log.wafData} mono small />}
          </>
        )}
      </DetailSection>

      {/* 地理位置 */}
      <DetailSection title="地理位置" icon={<MapPin className="size-3" />}>
        <DetailRow label="IP" value={log.ip} mono />
        {geo && <DetailRow label="位置" value={geo} />}
        {log.countryCode && <DetailRow label="国家代码" value={log.countryCode} />}
        {log.isp && <DetailRow label="ISP" value={log.isp} />}
        {log.asn && <DetailRow label="ASN" value={log.asn} mono />}
        {log.timezone && <DetailRow label="时区" value={log.timezone} />}
        {log.latitude != null && log.longitude != null && (
          <DetailRow label="坐标" value={`${log.latitude}, ${log.longitude}`} mono />
        )}
      </DetailSection>

      {/* 请求头 */}
      {log.headers && Object.keys(log.headers).length > 0 && (
        <DetailSection title="请求头" icon={<Network className="size-3" />}>
          {Object.entries(log.headers).map(([k, v]) => (
            <DetailRow key={k} label={k} value={v} mono small />
          ))}
        </DetailSection>
      )}
    </div>
  );
}

function SeverityBadge({ severity }: { severity: string }) {
  const cfg = severityConfig[severity] ?? severityConfig.medium;
  return <Badge variant={cfg.variant} className="text-[10px]">{cfg.label}</Badge>;
}

function DetailSection({ title, icon, children }: { title: string; icon?: React.ReactNode; children: React.ReactNode }) {
  return (
    <div>
      <h4 className="mb-2 flex items-center gap-1.5 text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">
        {icon}
        {title}
      </h4>
      <div className="space-y-1 rounded-lg border bg-muted/20 px-3 py-2.5">{children}</div>
    </div>
  );
}

function DetailRow({ label, value, mono, small, children }: { label: string; value?: string; mono?: boolean; small?: boolean; children?: React.ReactNode }) {
  return (
    <div className="flex items-start gap-3 py-0.5">
      <span className="w-20 shrink-0 text-[11px] text-muted-foreground">{label}</span>
      {children ?? (
        <span className={`text-[11px] text-foreground break-all ${mono ? "font-mono" : ""} ${small ? "text-[10px] leading-relaxed opacity-80" : ""}`}>
          {value || "—"}
        </span>
      )}
    </div>
  );
}
