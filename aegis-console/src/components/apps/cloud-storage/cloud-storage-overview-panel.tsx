"use client";

import { useMemo, useState } from "react";
import Link from "next/link";
import { Bar, BarChart, CartesianGrid, XAxis, YAxis } from "recharts";
import {
  ArrowUpRight,
  CircleAlert,
  Database,
  FileText,
  HardDrive,
  PenLine,
  Settings2,
  Snowflake,
  Users
} from "lucide-react";
import { StatusDot } from "@/components/apps/app-config-primitives";
import {
  NamespaceLabel,
  QuotaMeter,
  describeTarget,
  formatBytes,
  formatDateTime
} from "@/components/apps/cloud-storage/cloud-storage-shared";
import { buildChartConfig, ChartCard, type ThemedColor } from "@/components/commerce/commerce-charts";
import { StatTile } from "@/components/content/content-shared";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { ChartContainer, ChartLegend, ChartLegendContent, ChartTooltip, ChartTooltipContent } from "@/components/ui/chart";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { useCloudStorageConfigQuery, useCloudStorageStatsQuery } from "@/lib/cloud-storage-hooks";

const SERIES: Array<{ key: "writes" | "users"; label: string; color: ThemedColor }> = [
  { key: "writes", label: "写入", color: { light: "#0ea5e9", dark: "#38bdf8" } },
  { key: "users", label: "人数", color: { light: "#10b981", dark: "#34d399" } }
];

const RANGES = [7, 14, 30] as const;

export function CloudStorageOverviewPanel({
  appKey,
  onOpenConfig,
  onOpenUsers
}: {
  appKey: string;
  onOpenConfig: () => void;
  onOpenUsers: () => void;
}) {
  const [days, setDays] = useState<number>(14);
  const configQuery = useCloudStorageConfigQuery(appKey);
  const statsQuery = useCloudStorageStatsQuery(appKey, days);

  const config = configQuery.data;
  const stats = statsQuery.data;
  const chartConfig = useMemo(() => buildChartConfig(SERIES), []);
  const rows = useMemo(() => (stats?.trend ?? []).map((day) => ({ ...day, label: day.day.slice(5) })), [stats]);
  const empty = rows.every((row) => row.writes === 0);
  const target = stats?.storageTarget ?? config?.target;
  const targetOption = config?.storageOptions.find((option) => option.configId === target?.configId);
  const publicTarget = targetOption?.accessMode === "public";
  const largestNamespace = Math.max(1, ...(stats?.namespaces ?? []).map((item) => item.storedBytes));

  if (configQuery.isLoading) {
    return <Skeleton className="h-64 w-full rounded-2xl" />;
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <StatusDot active={Boolean(config?.enabled)} labelActive="云存储已启用" labelInactive="云存储未启用" />
        {!config?.enabled ? (
          <Button size="sm" variant="outline" onClick={onOpenConfig}>
            <Settings2 className="size-3.5" />
            去配置
          </Button>
        ) : null}
      </div>

      {target?.error && config?.enabled ? (
        <Alert variant="destructive">
          <CircleAlert className="size-4" />
          <AlertTitle>存储不可用</AlertTitle>
          <AlertDescription>{target.error}</AlertDescription>
        </Alert>
      ) : null}

      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <StatTile
          label="使用人数"
          icon={Users}
          value={stats?.users ?? "—"}
          hint={stats?.frozenUsers ? `冻结 ${stats.frozenUsers}` : undefined}
        />
        <StatTile
          label="已用空间"
          icon={HardDrive}
          value={stats ? formatBytes(stats.usedBytes) : "—"}
          hint={stats ? `${stats.revisions} 个修订` : undefined}
        />
        <StatTile
          label="条目"
          icon={FileText}
          value={stats?.items ?? "—"}
          hint={stats ? `回收站 ${stats.trashItems}` : undefined}
        />
        <StatTile label="今日写入" icon={PenLine} tone="positive" value={stats?.writesToday ?? "—"} />
      </div>

      <div className="flex flex-wrap items-center gap-x-4 gap-y-1 rounded-xl border border-border bg-card px-4 py-3 text-xs">
        <span className="flex items-center gap-1.5 text-muted-foreground">
          <Database className="size-3.5" />
          写入位置
        </span>
        <span className="font-medium">{describeTarget(target)}</span>
        {publicTarget ? (
          <span className="text-amber-600 dark:text-amber-400">公开访问的存储配置，建议改用私有配置</span>
        ) : null}
        {config ? (
          <span className="ml-auto text-muted-foreground tabular-nums">
            每人 {formatBytes(config.quotaBytes)} · 单条 {formatBytes(config.maxItemBytes)} · 保留 {config.maxRevisions} 个修订
          </span>
        ) : null}
      </div>

      <ChartCard
        title="写入趋势"
        loading={statsQuery.isLoading}
        empty={empty}
        height={240}
        action={
          <ToggleGroup
            type="single"
            size="sm"
            value={String(days)}
            onValueChange={(value) => value && setDays(Number(value))}
            className="shrink-0"
          >
            {RANGES.map((range) => (
              <ToggleGroupItem key={range} value={String(range)} className="h-7 px-2.5 text-xs">
                {range} 天
              </ToggleGroupItem>
            ))}
          </ToggleGroup>
        }
      >
        <ChartContainer config={chartConfig} className="h-[240px] w-full">
          <BarChart data={rows} margin={{ left: 0, right: 8, top: 8 }} barGap={2}>
            <CartesianGrid vertical={false} strokeDasharray="3 3" />
            <XAxis dataKey="label" tickLine={false} axisLine={false} fontSize={11} />
            <YAxis allowDecimals={false} tickLine={false} axisLine={false} fontSize={11} width={32} />
            <ChartTooltip content={<ChartTooltipContent />} />
            {SERIES.map((series) => (
              <Bar key={series.key} dataKey={series.key} fill={`var(--color-${series.key})`} radius={[3, 3, 0, 0]} />
            ))}
            <ChartLegend content={<ChartLegendContent />} />
          </BarChart>
        </ChartContainer>
      </ChartCard>

      <div className="grid gap-4 lg:grid-cols-2">
        <div className="overflow-hidden rounded-2xl border border-border bg-card">
          <div className="border-b border-border px-4 py-3 text-sm font-semibold">命名空间</div>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>名称</TableHead>
                <TableHead className="text-right">条目 / 回收站</TableHead>
                <TableHead className="w-40 text-right">占用</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {(stats?.namespaces ?? []).length === 0 ? (
                <TableRow>
                  <TableCell colSpan={3} className="py-8 text-center text-xs text-muted-foreground">
                    暂无数据
                  </TableCell>
                </TableRow>
              ) : (
                (stats?.namespaces ?? []).map((item) => (
                  <TableRow key={item.namespace}>
                    <TableCell className="text-xs">
                      <NamespaceLabel namespace={item.namespace} name={item.name} />
                    </TableCell>
                    <TableCell className="text-right font-mono text-xs tabular-nums">
                      {item.itemCount} / {item.trashCount}
                    </TableCell>
                    <TableCell className="text-right">
                      <div className="ml-auto w-36 space-y-1">
                        <div className="font-mono text-xs tabular-nums">{formatBytes(item.storedBytes)}</div>
                        <div className="h-1 overflow-hidden rounded-full bg-muted">
                          <div
                            className="h-full rounded-full bg-sky-500"
                            style={{ width: `${Math.max(2, (item.storedBytes / largestNamespace) * 100)}%` }}
                          />
                        </div>
                      </div>
                    </TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
        </div>

        <div className="overflow-hidden rounded-2xl border border-border bg-card">
          <div className="flex items-center justify-between border-b border-border px-4 py-3">
            <span className="text-sm font-semibold">用量最高</span>
            <Button size="xs" variant="ghost" onClick={onOpenUsers}>
              全部用户
              <ArrowUpRight className="size-3" />
            </Button>
          </div>
          {(stats?.topUsers ?? []).length === 0 ? (
            <p className="py-8 text-center text-xs text-muted-foreground">暂无数据</p>
          ) : (
            <ul className="divide-y divide-border">
              {(stats?.topUsers ?? []).map((user) => (
                <li key={user.userId}>
                  <Link
                    href={`/app-users/${encodeURIComponent(appKey)}/${user.userId}?tab=cloud`}
                    className="flex items-center gap-3 px-4 py-2.5 transition-colors hover:bg-muted/50"
                  >
                    <div className="min-w-0 flex-1">
                      <div className="flex items-center gap-1.5 text-xs font-medium">
                        <span className="truncate">{user.nickname || user.account || `#${user.userId}`}</span>
                        {user.frozen ? <Snowflake className="size-3 shrink-0 text-sky-500" aria-label="已冻结" /> : null}
                      </div>
                      <div className="mt-0.5 text-[11px] text-muted-foreground">
                        {user.itemCount} 个条目 · 最近写入 {formatDateTime(user.lastWriteAt)}
                      </div>
                    </div>
                    <QuotaMeter used={user.usedBytes} quota={user.quotaBytes} className="w-36" />
                  </Link>
                </li>
              ))}
            </ul>
          )}
        </div>
      </div>
    </div>
  );
}
