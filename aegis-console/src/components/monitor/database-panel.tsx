"use client";

import { useCallback, useMemo, useState } from "react";
import {
  Activity,
  AlertTriangle,
  Ban,
  CheckCircle2,
  Database,
  Flame,
  Gauge,
  Loader2,
  RefreshCw,
  ShieldAlert,
  Timer,
  Wrench,
  XCircle,
  Zap
} from "lucide-react";
import { toast } from "sonner";
import { ApiError } from "@/lib/api-client";
import type {
  DBLeakFinding,
  DBSession,
  LeakSeverity,
  SlowQuerySample
} from "@/lib/api/database";
import {
  useDatabaseMaintenanceQuery,
  useDatabaseSessionsQuery,
  useDatabaseSnapshotQuery,
  useRefreshDatabaseMutation,
  useTerminateDatabaseSessionMutation,
  useWarmupDatabaseMutation
} from "@/lib/admin-hooks";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle
} from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";

function formatBytes(value: number) {
  if (!value) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let index = 0;
  let current = value;
  while (current >= 1024 && index < units.length - 1) {
    current /= 1024;
    index += 1;
  }
  return `${current.toFixed(index === 0 ? 0 : 1)} ${units[index]}`;
}

function formatSeconds(value: number) {
  if (value < 1) return "—";
  if (value < 60) return `${value.toFixed(0)}s`;
  if (value < 3600) return `${(value / 60).toFixed(1)}min`;
  return `${(value / 3600).toFixed(1)}h`;
}

function formatMs(value: number) {
  if (value < 1000) return `${value}ms`;
  return `${(value / 1000).toFixed(1)}s`;
}

function severityBadge(severity: LeakSeverity) {
  switch (severity) {
    case "critical":
      return <Badge variant="danger" size="sm">严重</Badge>;
    case "warning":
      return <Badge variant="warning" size="sm">警告</Badge>;
    default:
      return <Badge variant="info" size="sm">提示</Badge>;
  }
}

const leakKindLabel: Record<string, string> = {
  connection: "连接泄漏",
  transaction: "事务泄漏",
  snapshot: "快照未推进",
  wal: "WAL 堆积",
  prepared_xact: "两阶段事务悬挂",
  storage: "存储劣化",
  pool: "连接池水位",
  trend: "趋势异常"
};

export function DatabasePanel() {
  const [autoRefresh, setAutoRefresh] = useState(true);
  const snapshotQuery = useDatabaseSnapshotQuery({ refetchInterval: autoRefresh ? 15_000 : undefined });
  const refreshMutation = useRefreshDatabaseMutation();
  const warmupMutation = useWarmupDatabaseMutation();

  const snapshot = snapshotQuery.data;
  const metrics = snapshot?.metrics;
  const leak = snapshot?.leak;

  const handleRefresh = useCallback(async () => {
    try {
      await refreshMutation.mutateAsync();
      toast.success("已重新采集");
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "采集失败");
    }
  }, [refreshMutation]);

  const handleWarmup = useCallback(async () => {
    try {
      await warmupMutation.mutateAsync();
      toast.success("连接池已预热");
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "预热失败");
    }
  }, [warmupMutation]);

  if (snapshotQuery.isLoading) {
    return (
      <div className="space-y-3">
        <Skeleton className="h-8 w-56" />
        <Skeleton className="h-24 w-full" />
        <Skeleton className="h-40 w-full" />
      </div>
    );
  }

  if (snapshotQuery.isError) {
    return (
      <div className="rounded-lg border border-dashed py-10 text-center text-xs text-muted-foreground">
        无法获取数据库监控数据（需要超级管理员权限）
      </div>
    );
  }

  return (
    <section className="space-y-4">
      <header className="flex flex-wrap items-center gap-2">
        <Database className="size-4 text-muted-foreground" />
        <h2 className="text-sm font-semibold">数据库生命周期与泄漏监控</h2>
        {metrics && (
          metrics.healthy ? (
            <Badge variant="success" size="sm">
              健康 · {metrics.pingMs}ms
            </Badge>
          ) : (
            <Badge variant="danger" size="sm">探测失败</Badge>
          )
        )}
        {leak && (
          leak.suspicious ? (
            <Badge variant={leak.critical > 0 ? "danger" : "warning"} size="sm">
              {leak.critical > 0 ? `${leak.critical} 项严重` : `${leak.warning} 项警告`}
            </Badge>
          ) : (
            <Badge variant="success" size="sm">无泄漏迹象</Badge>
          )
        )}
        <div className="ml-auto flex items-center gap-2">
          <label className="flex items-center gap-1.5 text-[11px] text-muted-foreground">
            <Switch checked={autoRefresh} onCheckedChange={setAutoRefresh} aria-label="自动刷新" />
            自动刷新
          </label>
          <Button
            size="sm"
            variant="outline"
            className="h-7 gap-1 text-[11px]"
            disabled={warmupMutation.isPending}
            onClick={handleWarmup}
          >
            <Zap className="size-3" />
            预热连接池
          </Button>
          <Button
            size="sm"
            variant="outline"
            className="h-7 gap-1 text-[11px]"
            disabled={refreshMutation.isPending}
            onClick={handleRefresh}
          >
            {refreshMutation.isPending ? (
              <Loader2 className="size-3 animate-spin" />
            ) : (
              <RefreshCw className="size-3" />
            )}
            立即采集
          </Button>
        </div>
      </header>

      {metrics && <MetricGrid snapshot={snapshot} />}

      <Tabs defaultValue="leak" className="space-y-3">
        <TabsList className="w-fit">
          <TabsTrigger value="leak">
            <ShieldAlert className="size-4" />
            泄漏检测
          </TabsTrigger>
          <TabsTrigger value="sessions">
            <Activity className="size-4" />
            会话
          </TabsTrigger>
          <TabsTrigger value="slow">
            <Timer className="size-4" />
            慢查询
          </TabsTrigger>
          <TabsTrigger value="maintenance">
            <Wrench className="size-4" />
            存储维护
          </TabsTrigger>
          <TabsTrigger value="lifecycle">
            <Gauge className="size-4" />
            生命周期
          </TabsTrigger>
        </TabsList>

        <TabsContent value="leak">
          <LeakSection snapshot={snapshot} />
        </TabsContent>
        <TabsContent value="sessions">
          <SessionsSection />
        </TabsContent>
        <TabsContent value="slow">
          <SlowQuerySection items={snapshot?.slowQueries ?? []} />
        </TabsContent>
        <TabsContent value="maintenance">
          <MaintenanceSection />
        </TabsContent>
        <TabsContent value="lifecycle">
          <LifecycleSection snapshot={snapshot} />
        </TabsContent>
      </Tabs>
    </section>
  );
}

function MetricGrid({ snapshot }: { snapshot?: import("@/lib/api/database").DatabaseSnapshot }) {
  const metrics = snapshot?.metrics;
  if (!metrics) return null;
  const { pool, instrument, server } = metrics;

  const cells = [
    {
      label: "连接池",
      value: `${pool.acquiredConns} / ${pool.maxConns}`,
      hint: `空闲 ${pool.idleConns} · 使用率 ${pool.usagePct.toFixed(0)}%`,
      danger: pool.usagePct >= 90,
      warn: pool.usagePct >= 75
    },
    {
      label: "借出未还",
      value: String(instrument.checkedOut),
      hint: instrument.oldestCheckoutMs > 0 ? `最久 ${formatMs(instrument.oldestCheckoutMs)}` : "无",
      danger: instrument.leakSuspectCount > 0
    },
    {
      label: "在途语句",
      value: String(instrument.inFlightQueries),
      hint: instrument.oldestInFlightMs > 0 ? `最久 ${formatMs(instrument.oldestInFlightMs)}` : "无"
    },
    {
      label: "获取等待",
      value: `${pool.avgAcquireWaitMs.toFixed(1)}ms`,
      hint: `空池 ${pool.emptyAcquireCount} 次 · 取消 ${pool.canceledAcquireCount} 次`,
      warn: pool.canceledAcquireCount > 0
    },
    {
      label: "服务端连接",
      value: `${server.totalBackends} / ${server.maxConnections}`,
      hint: `活跃 ${server.activeBackends} · 等待 ${server.waitingBackends}`,
      danger: server.connUsagePct >= 90,
      warn: server.connUsagePct >= 80
    },
    {
      label: "idle in tx",
      value: String(server.idleInTxCount),
      hint: server.oldestIdleInTxSeconds > 0 ? `最久 ${formatSeconds(server.oldestIdleInTxSeconds)}` : "无",
      danger: server.idleInTxAborted > 0,
      warn: server.idleInTxCount > 0
    },
    {
      label: "缓冲命中率",
      value: `${server.cacheHitRatio.toFixed(1)}%`,
      hint: `回滚率 ${server.rolledBackPct.toFixed(1)}% · 死锁 ${server.deadlocks}`,
      warn: server.cacheHitRatio > 0 && server.cacheHitRatio < 90
    },
    {
      label: "慢查询",
      value: String(instrument.slowQueries),
      hint: `极慢 ${instrument.verySlowQueries} · 平均 ${instrument.avgQueryMs}ms`,
      warn: instrument.verySlowQueries > 0
    },
    {
      label: "库大小",
      value: formatBytes(server.databaseBytes),
      hint: `死元组 ${server.deadTuples.toLocaleString()}`
    },
    {
      label: "WAL 滞留",
      value: formatBytes(server.replicationRetainedBytes),
      hint: `未消费槽 ${server.inactiveReplicationSlots} 个`,
      danger: server.inactiveReplicationSlots > 0
    }
  ];

  return (
    <div className="grid grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-5">
      {cells.map((cell) => (
        <div
          key={cell.label}
          className={`rounded-lg border p-2.5 ${
            cell.danger
              ? "border-red-200 bg-red-50/50 dark:border-red-900/60 dark:bg-red-950/20"
              : cell.warn
                ? "border-amber-200 bg-amber-50/50 dark:border-amber-900/60 dark:bg-amber-950/20"
                : ""
          }`}
        >
          <div className="text-[10px] uppercase tracking-wider text-muted-foreground">{cell.label}</div>
          <div className="mt-0.5 font-mono text-sm font-semibold">{cell.value}</div>
          <div className="mt-0.5 truncate text-[10px] text-muted-foreground">{cell.hint}</div>
        </div>
      ))}
    </div>
  );
}

function LeakSection({ snapshot }: { snapshot?: import("@/lib/api/database").DatabaseSnapshot }) {
  const leak = snapshot?.leak;
  if (!leak) {
    return (
      <div className="rounded-lg border border-dashed py-10 text-center text-xs text-muted-foreground">
        泄漏检测未启用（DB_LEAK_DETECTION=false）
      </div>
    );
  }
  return (
    <div className="space-y-3">
      <div
        className={`flex items-start gap-2 rounded-lg border p-3 text-xs ${
          leak.critical > 0
            ? "border-red-200 bg-red-50/50 dark:border-red-900/60 dark:bg-red-950/20"
            : leak.warning > 0
              ? "border-amber-200 bg-amber-50/50 dark:border-amber-900/60 dark:bg-amber-950/20"
              : "border-emerald-200 bg-emerald-50/40 dark:border-emerald-900/60 dark:bg-emerald-950/20"
        }`}
      >
        {leak.suspicious ? (
          <AlertTriangle className="mt-px size-4 shrink-0" />
        ) : (
          <CheckCircle2 className="mt-px size-4 shrink-0" />
        )}
        <div>
          <div className="font-medium">{leak.summary}</div>
          <div className="mt-0.5 text-[11px] text-muted-foreground">
            检测覆盖：连接持有 / 事务 / 快照 / WAL / 两阶段事务 / 存储 / 连接池水位，外加指标趋势。
          </div>
        </div>
      </div>

      {leak.findings.length > 0 && (
        <div className="space-y-2">
          {leak.findings.map((finding, index) => (
            <FindingCard key={`${finding.kind}-${index}`} finding={finding} />
          ))}
        </div>
      )}

      {leak.trends.length > 0 && (
        <div className="rounded-lg border">
          <div className="border-b px-3 py-2 text-[11px] font-medium text-muted-foreground">
            指标趋势（滑动窗口）
          </div>
          <div className="divide-y">
            {leak.trends.map((trend) => (
              <div key={trend.name} className="flex items-center gap-2 px-3 py-1.5 text-xs">
                <span className="w-44 shrink-0 font-mono text-[11px]">{trend.name}</span>
                <Badge
                  variant={trend.suspectedLeak ? "warning" : trend.trending === "declining" ? "info" : "secondary"}
                  size="sm"
                >
                  {trend.trending === "rising" ? "上升" : trend.trending === "declining" ? "下降" : "平稳"}
                </Badge>
                <span className="text-muted-foreground">
                  最新 {trend.latestValue.toLocaleString()} · 变化 {trend.deltaPercent.toFixed(1)}%
                </span>
                <span className="ml-auto text-[10px] text-muted-foreground">{trend.samples} 个采样点</span>
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}

function FindingCard({ finding }: { finding: DBLeakFinding }) {
  const [expanded, setExpanded] = useState(false);
  return (
    <div className="rounded-lg border p-3">
      <div className="flex flex-wrap items-center gap-1.5">
        {severityBadge(finding.severity)}
        <Badge variant="outline" size="sm">
          {leakKindLabel[finding.kind] || finding.kind}
        </Badge>
        <span className="text-xs font-medium">{finding.title}</span>
        {finding.count ? (
          <span className="text-[11px] text-muted-foreground">×{finding.count}</span>
        ) : null}
      </div>
      <p className="mt-1.5 text-[11px] leading-relaxed text-muted-foreground">{finding.detail}</p>
      {finding.advice && (
        <p className="mt-1 text-[11px] leading-relaxed">
          <span className="font-medium">处置：</span>
          {finding.advice}
        </p>
      )}
      {finding.evidence && finding.evidence.length > 0 && (
        <div className="mt-2">
          <Button
            size="sm"
            variant="ghost"
            className="h-6 px-2 text-[11px]"
            onClick={() => setExpanded((prev) => !prev)}
          >
            {expanded ? "收起现场" : `查看现场（${finding.evidence.length}）`}
          </Button>
          {expanded && (
            <ScrollArea className="mt-1 max-h-64 rounded-md border bg-muted/40">
              <pre className="whitespace-pre-wrap p-2 font-mono text-[10px] leading-relaxed">
                {finding.evidence.join("\n\n")}
              </pre>
            </ScrollArea>
          )}
        </div>
      )}
    </div>
  );
}

function SessionsSection() {
  const [onlyProblematic, setOnlyProblematic] = useState(true);
  const sessionsQuery = useDatabaseSessionsQuery({ onlyProblematic, limit: 100 });
  const actionMutation = useTerminateDatabaseSessionMutation();
  const [pending, setPending] = useState<{ session: DBSession; mode: "terminate" | "cancel" } | null>(null);

  const items = sessionsQuery.data?.items ?? [];

  const handleAction = useCallback(async () => {
    if (!pending) return;
    try {
      await actionMutation.mutateAsync({ pid: pending.session.pid, mode: pending.mode });
      toast.success(pending.mode === "terminate" ? "会话已终止" : "语句已取消");
      setPending(null);
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "操作失败");
    }
  }, [actionMutation, pending]);

  return (
    <div className="space-y-2">
      <div className="flex items-center gap-2">
        <label className="flex items-center gap-1.5 text-[11px] text-muted-foreground">
          <Switch checked={onlyProblematic} onCheckedChange={setOnlyProblematic} aria-label="只看异常会话" />
          只看活跃 / 事务挂起 / 被锁阻塞
        </label>
        <Button
          size="sm"
          variant="outline"
          className="ml-auto h-7 gap-1 text-[11px]"
          disabled={sessionsQuery.isFetching}
          onClick={() => void sessionsQuery.refetch()}
        >
          {sessionsQuery.isFetching ? (
            <Loader2 className="size-3 animate-spin" />
          ) : (
            <RefreshCw className="size-3" />
          )}
          刷新
        </Button>
      </div>

      {sessionsQuery.isLoading ? (
        <Skeleton className="h-32 w-full" />
      ) : items.length === 0 ? (
        <div className="rounded-lg border border-dashed py-10 text-center text-xs text-muted-foreground">
          没有符合条件的会话
        </div>
      ) : (
        <div className="overflow-x-auto rounded-lg border">
          <table className="w-full text-xs">
            <thead className="bg-muted/50 text-muted-foreground">
              <tr>
                <th className="px-2 py-2 text-left font-medium">PID</th>
                <th className="px-2 py-2 text-left font-medium">来源</th>
                <th className="px-2 py-2 text-left font-medium">状态</th>
                <th className="px-2 py-2 text-left font-medium">事务 / 语句时长</th>
                <th className="px-2 py-2 text-left font-medium">语句</th>
                <th className="px-2 py-2 text-right font-medium">操作</th>
              </tr>
            </thead>
            <tbody>
              {items.map((session) => (
                <tr key={session.pid} className="border-t align-top">
                  <td className="px-2 py-2 font-mono">
                    {session.pid}
                    {session.isSelf && (
                      <Badge variant="secondary" size="sm" className="ml-1">
                        本连接
                      </Badge>
                    )}
                  </td>
                  <td className="px-2 py-2">
                    <div>{session.applicationName || "—"}</div>
                    <div className="text-[10px] text-muted-foreground">
                      {session.user}
                      {session.clientAddr ? ` @ ${session.clientAddr}` : ""}
                    </div>
                  </td>
                  <td className="px-2 py-2">
                    <Badge
                      variant={
                        session.state?.startsWith("idle in transaction")
                          ? "warning"
                          : session.state === "active"
                            ? "info"
                            : "secondary"
                      }
                      size="sm"
                    >
                      {session.state || "—"}
                    </Badge>
                    {session.blockedBy && session.blockedBy.length > 0 && (
                      <div className="mt-0.5 text-[10px] text-red-600 dark:text-red-400">
                        被 {session.blockedBy.join(", ")} 阻塞
                      </div>
                    )}
                    {session.waitEvent && (
                      <div className="mt-0.5 text-[10px] text-muted-foreground">
                        {session.waitEventType}:{session.waitEvent}
                      </div>
                    )}
                  </td>
                  <td className="px-2 py-2 font-mono text-[11px]">
                    <div>{formatSeconds(session.xactSeconds)}</div>
                    <div className="text-muted-foreground">{formatSeconds(session.querySeconds)}</div>
                  </td>
                  <td className="max-w-[280px] px-2 py-2">
                    <div className="truncate font-mono text-[10px] text-muted-foreground" title={session.query}>
                      {session.query || "—"}
                    </div>
                  </td>
                  <td className="px-2 py-2 text-right">
                    {!session.isSelf && (
                      <div className="flex justify-end gap-1">
                        <Button
                          size="sm"
                          variant="ghost"
                          className="h-6 gap-1 px-1.5 text-[11px]"
                          onClick={() => setPending({ session, mode: "cancel" })}
                        >
                          <Ban className="size-3" />
                          取消语句
                        </Button>
                        <Button
                          size="sm"
                          variant="ghost"
                          className="h-6 gap-1 px-1.5 text-[11px] text-destructive hover:text-destructive"
                          onClick={() => setPending({ session, mode: "terminate" })}
                        >
                          <XCircle className="size-3" />
                          终止
                        </Button>
                      </div>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <AlertDialog open={Boolean(pending)} onOpenChange={(open) => !open && setPending(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {pending?.mode === "terminate" ? "终止会话" : "取消语句"} PID {pending?.session.pid}？
            </AlertDialogTitle>
            <AlertDialogDescription>
              {pending?.mode === "terminate"
                ? "会话将被断开，其未提交的事务全部回滚。若该会话属于业务进程，对应请求会收到连接错误。"
                : "只中止当前正在执行的语句，连接与事务保留。若语句本身是长事务的一部分，事务仍会挂起。"}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction onClick={handleAction} disabled={actionMutation.isPending}>
              {actionMutation.isPending ? "执行中..." : "确认"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

function SlowQuerySection({ items }: { items: SlowQuerySample[] }) {
  if (items.length === 0) {
    return (
      <div className="rounded-lg border border-dashed py-10 text-center text-xs text-muted-foreground">
        暂无慢查询样本
      </div>
    );
  }
  return (
    <div className="space-y-1.5">
      {items.map((item, index) => (
        <div key={`${item.occurredAt}-${index}`} className="rounded-lg border p-2.5">
          <div className="flex items-center gap-2">
            <Flame
              className={`size-3.5 shrink-0 ${
                item.durationMs >= 2000 ? "text-red-500" : "text-amber-500"
              }`}
            />
            <span className="font-mono text-xs font-semibold">{formatMs(item.durationMs)}</span>
            <span className="text-[10px] text-muted-foreground">
              PID {item.pid} · {new Date(item.occurredAt).toLocaleTimeString("zh-CN")}
            </span>
            {item.rows > 0 && (
              <span className="text-[10px] text-muted-foreground">影响 {item.rows} 行</span>
            )}
            {item.err && (
              <Badge variant="danger" size="sm">
                执行出错
              </Badge>
            )}
          </div>
          <pre className="mt-1 overflow-x-auto whitespace-pre-wrap font-mono text-[10px] leading-relaxed text-muted-foreground">
            {item.sql}
          </pre>
          {item.err && <div className="mt-1 text-[10px] text-destructive">{item.err}</div>}
        </div>
      ))}
    </div>
  );
}

function MaintenanceSection() {
  const query = useDatabaseMaintenanceQuery(20);
  const bloated = query.data?.bloatedTables ?? [];
  const unused = query.data?.unusedIndexes ?? [];

  if (query.isLoading) return <Skeleton className="h-32 w-full" />;

  return (
    <div className="grid gap-3 lg:grid-cols-2">
      <div className="rounded-lg border">
        <div className="border-b px-3 py-2 text-[11px] font-medium text-muted-foreground">
          死元组占比高的表（VACUUM 未跟上）
        </div>
        {bloated.length === 0 ? (
          <div className="py-8 text-center text-xs text-muted-foreground">无</div>
        ) : (
          <div className="divide-y">
            {bloated.map((item) => (
              <div key={`${item.schema}.${item.table}`} className="px-3 py-1.5 text-xs">
                <div className="flex items-center gap-2">
                  <span className="font-mono">{item.table}</span>
                  <Badge variant={item.deadRatio > 20 ? "warning" : "secondary"} size="sm">
                    {item.deadRatio.toFixed(1)}%
                  </Badge>
                  <span className="ml-auto text-[10px] text-muted-foreground">
                    {formatBytes(item.totalBytes)}
                  </span>
                </div>
                <div className="mt-0.5 text-[10px] text-muted-foreground">
                  死元组 {item.deadTuples.toLocaleString()} / 存活 {item.liveTuples.toLocaleString()} · 上次
                  autovacuum{" "}
                  {item.lastAutoVacuum
                    ? new Date(item.lastAutoVacuum).toLocaleString("zh-CN")
                    : "从未"}
                </div>
              </div>
            ))}
          </div>
        )}
      </div>

      <div className="rounded-lg border">
        <div className="border-b px-3 py-2 text-[11px] font-medium text-muted-foreground">
          从未被扫描的索引（占空间、拖慢写入）
        </div>
        {unused.length === 0 ? (
          <div className="py-8 text-center text-xs text-muted-foreground">无</div>
        ) : (
          <div className="divide-y">
            {unused.map((item) => (
              <div key={`${item.schema}.${item.index}`} className="px-3 py-1.5 text-xs">
                <div className="flex items-center gap-2">
                  <span className="font-mono">{item.index}</span>
                  <span className="ml-auto text-[10px] text-muted-foreground">
                    {formatBytes(item.bytes)}
                  </span>
                </div>
                <div className="mt-0.5 text-[10px] text-muted-foreground">表 {item.table}</div>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

function LifecycleSection({ snapshot }: { snapshot?: import("@/lib/api/database").DatabaseSnapshot }) {
  const lifecycle = snapshot?.lifecycle;
  const sessionSettings = useMemo(
    () => Object.entries(lifecycle?.sessionSettings ?? {}).sort(([a], [b]) => a.localeCompare(b)),
    [lifecycle]
  );
  if (!lifecycle) return null;

  const rows: Array<[string, string]> = [
    ["连接数上下限", `${lifecycle.minConns} ~ ${lifecycle.maxConns}`],
    ["连接最长寿命", `${lifecycle.maxConnLifetime}（抖动 ${lifecycle.maxConnLifetimeJitter}）`],
    ["空闲回收", lifecycle.maxConnIdleTime],
    ["池自检周期", lifecycle.healthCheckPeriod],
    ["建连超时", lifecycle.connectTimeout],
    ["关闭排空上限", lifecycle.drainTimeout],
    ["启动预热", lifecycle.warmupOnStart ? "开启" : "关闭"],
    ["采集间隔", lifecycle.monitorInterval],
    ["慢查询阈值", lifecycle.slowQueryThreshold],
    ["连接持有告警阈值", lifecycle.connHoldThreshold],
    ["idle in tx 告警阈值", lifecycle.idleInTxThreshold],
    ["长语句告警阈值", lifecycle.longQueryThreshold],
    ["获取连接抓调用栈", lifecycle.trackAcquireStack ? "开启" : "关闭（无法定位到代码行）"],
    [
      "自动终止 idle in tx",
      lifecycle.autoTerminateIdleInTx ? `开启（${lifecycle.autoTerminateAfter}）` : "关闭"
    ]
  ];

  return (
    <div className="grid gap-3 lg:grid-cols-2">
      <div className="rounded-lg border">
        <div className="border-b px-3 py-2 text-[11px] font-medium text-muted-foreground">
          生命周期参数（当前生效值）
        </div>
        <div className="divide-y">
          {rows.map(([label, value]) => (
            <div key={label} className="flex items-center gap-2 px-3 py-1.5 text-xs">
              <span className="text-muted-foreground">{label}</span>
              <span className="ml-auto font-mono text-[11px]">{value}</span>
            </div>
          ))}
        </div>
      </div>
      <div className="rounded-lg border">
        <div className="border-b px-3 py-2 text-[11px] font-medium text-muted-foreground">
          会话级参数（随每条连接下发到服务端）
        </div>
        <div className="divide-y">
          {sessionSettings.map(([key, value]) => (
            <div key={key} className="flex items-center gap-2 px-3 py-1.5 text-xs">
              <span className="font-mono text-[11px] text-muted-foreground">{key}</span>
              <span className="ml-auto font-mono text-[11px]">{value}</span>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}
