"use client";

import { useMemo, useState } from "react";
import { Bar, BarChart, CartesianGrid, XAxis, YAxis } from "recharts";
import { Activity, Boxes, CircleAlert, Coins, Hourglass, Link2, Settings2, TriangleAlert, Users } from "lucide-react";
import { StatusDot } from "@/components/apps/app-config-primitives";
import { CopyButton } from "@/components/apps/auth-protocol/auth-protocol-shared";
import { SOURCE_LABEL, formatPoints, formatTokens } from "@/components/apps/ai-gateway/ai-gateway-shared";
import { buildChartConfig, ChartCard, type ThemedColor } from "@/components/commerce/commerce-charts";
import { StatTile } from "@/components/content/content-shared";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { ChartContainer, ChartLegend, ChartLegendContent, ChartTooltip, ChartTooltipContent } from "@/components/ui/chart";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import {
  useAIGatewayModelsQuery,
  useAIGatewaySettingsQuery,
  useAIGatewayStatsQuery
} from "@/lib/ai-gateway-hooks";

const REQUEST_SERIES: Array<{ key: "succeeded" | "failed"; label: string; color: ThemedColor }> = [
  { key: "succeeded", label: "成功", color: { light: "#10b981", dark: "#34d399" } },
  { key: "failed", label: "失败（不扣费）", color: { light: "#f43f5e", dark: "#fb7185" } }
];
const CHARGE_SERIES: Array<{ key: "charged"; label: string; color: ThemedColor }> = [
  { key: "charged", label: "扣费（额度）", color: { light: "#6366f1", dark: "#818cf8" } }
];
const RANGES = [7, 14, 30] as const;

export function AIGatewayOverviewPanel({
  appKey,
  onOpenSettings,
  onOpenModels,
  onOpenRequests
}: {
  appKey: string;
  onOpenSettings: () => void;
  onOpenModels: () => void;
  onOpenRequests: (filter: { model?: string }) => void;
}) {
  const [days, setDays] = useState<number>(14);
  const settingsQuery = useAIGatewaySettingsQuery(appKey);
  const modelsQuery = useAIGatewayModelsQuery(appKey);
  const statsQuery = useAIGatewayStatsQuery(appKey, days);
  const settings = settingsQuery.data;
  const stats = statsQuery.data;
  const models = modelsQuery.data?.items ?? [];

  const requestConfig = useMemo(() => buildChartConfig(REQUEST_SERIES), []);
  const chargeConfig = useMemo(() => buildChartConfig(CHARGE_SERIES), []);
  const rows = useMemo(
    () =>
      (stats?.daily ?? []).map((day) => ({
        label: day.date.slice(5),
        succeeded: day.requests - day.failed,
        failed: day.failed,
        charged: Number(day.charged)
      })),
    [stats]
  );
  const baseUrl =
    typeof window === "undefined" ? (settings ? "/api/ai/v1" : "") : `${window.location.origin}/api/ai/v1`;
  const successRate = stats && stats.requests > 0 ? Math.round((stats.succeeded / stats.requests) * 1000) / 10 : null;

  if (settingsQuery.isLoading) {
    return <Skeleton className="h-64 w-full rounded-2xl" />;
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <StatusDot active={Boolean(settings?.enabled)} labelActive="AI 中转已开启" labelInactive="AI 中转未开启" />
        <div className="flex items-center gap-2">
          {!settings?.enabled ? (
            <Button size="sm" variant="outline" onClick={onOpenSettings}>
              <Settings2 className="size-3.5" />
              去设置
            </Button>
          ) : null}
        </div>
      </div>

      {settings?.enabled && models.filter((model) => model.enabled).length === 0 && !modelsQuery.isLoading ? (
        <Alert>
          <CircleAlert className="size-4" />
          <AlertTitle>没有开放的模型</AlertTitle>
          <AlertDescription className="flex flex-wrap items-center gap-2">
            用户调用会全部返回 404
            <Button size="xs" variant="outline" onClick={onOpenModels}>
              添加模型
            </Button>
          </AlertDescription>
        </Alert>
      ) : null}

      <div className="flex flex-wrap items-center gap-x-3 gap-y-1 rounded-xl border border-border bg-card px-4 py-3 text-xs">
        <span className="flex items-center gap-1.5 text-muted-foreground">
          <Link2 className="size-3.5" />
          Base URL
        </span>
        <code className="font-mono">{baseUrl}</code>
        <CopyButton value={baseUrl} />
        {settings ? (
          <span className="ml-auto text-muted-foreground">
            扣费顺序 {settings.sources.map((source) => SOURCE_LABEL[source] ?? source).join(" → ")}
          </span>
        ) : null}
      </div>

      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <StatTile
          label="调用"
          icon={Activity}
          value={stats?.requests ?? "—"}
          hint={successRate !== null ? `成功率 ${successRate}%` : undefined}
        />
        <StatTile
          label="扣费"
          icon={Coins}
          tone="positive"
          value={stats ? formatPoints(stats.charged) : "—"}
          hint={stats && Number(stats.refunded) > 0 ? `已退 ${formatPoints(stats.refunded)} 额度` : "额度"}
        />
        <StatTile
          label="用户"
          icon={Users}
          value={stats?.users ?? "—"}
          hint={stats ? `永久额度余量 ${formatPoints(stats.creditBalance)}` : undefined}
        />
        <StatTile
          label="Token"
          icon={Boxes}
          value={stats ? `${formatTokens(stats.inputTokens)} / ${formatTokens(stats.outputTokens)}` : "—"}
          hint={stats?.images ? `${stats.images} 张图` : undefined}
        />
      </div>

      {stats && (Number(stats.shortfall) > 0 || stats.reservedNow > 0) ? (
        <div className="flex flex-wrap gap-x-6 gap-y-1 rounded-xl border border-border bg-card px-4 py-3 text-xs">
          {stats.reservedNow > 0 ? (
            <span className="flex items-center gap-1.5">
              <Hourglass className="size-3.5 text-muted-foreground" />
              进行中 {stats.reservedNow} 次
            </span>
          ) : null}
          {Number(stats.shortfall) > 0 ? (
            <span className="flex items-center gap-1.5 text-amber-600 dark:text-amber-400">
              <TriangleAlert className="size-3.5" />
              用量超出暂扣且余额不足未收 {formatPoints(stats.shortfall)} 额度
            </span>
          ) : null}
        </div>
      ) : null}

      <ChartCard
        title="调用趋势"
        loading={statsQuery.isLoading}
        empty={rows.every((row) => row.succeeded === 0 && row.failed === 0)}
        height={220}
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
        <ChartContainer config={requestConfig} className="h-[220px] w-full">
          <BarChart data={rows} margin={{ left: 0, right: 8, top: 8 }} barGap={2}>
            <CartesianGrid vertical={false} strokeDasharray="3 3" />
            <XAxis dataKey="label" tickLine={false} axisLine={false} fontSize={11} />
            <YAxis allowDecimals={false} tickLine={false} axisLine={false} fontSize={11} width={32} />
            <ChartTooltip content={<ChartTooltipContent />} />
            {REQUEST_SERIES.map((series) => (
              <Bar key={series.key} dataKey={series.key} stackId="requests" fill={`var(--color-${series.key})`} />
            ))}
            <ChartLegend content={<ChartLegendContent />} />
          </BarChart>
        </ChartContainer>
      </ChartCard>

      <ChartCard
        title="扣费趋势"
        loading={statsQuery.isLoading}
        empty={rows.every((row) => row.charged === 0)}
        height={200}
      >
        <ChartContainer config={chargeConfig} className="h-[200px] w-full">
          <BarChart data={rows} margin={{ left: 0, right: 8, top: 8 }}>
            <CartesianGrid vertical={false} strokeDasharray="3 3" />
            <XAxis dataKey="label" tickLine={false} axisLine={false} fontSize={11} />
            <YAxis tickLine={false} axisLine={false} fontSize={11} width={40} />
            <ChartTooltip content={<ChartTooltipContent />} />
            <Bar dataKey="charged" fill="var(--color-charged)" radius={[3, 3, 0, 0]} />
          </BarChart>
        </ChartContainer>
      </ChartCard>

      <div className="grid gap-4 lg:grid-cols-2">
        <div className="overflow-hidden rounded-2xl border border-border bg-card">
          <div className="border-b border-border px-4 py-3 text-sm font-semibold">扣费来源</div>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>来源</TableHead>
                <TableHead className="text-right">额度</TableHead>
                <TableHead className="text-right">实扣</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {(stats?.bySource ?? []).length === 0 ? (
                <TableRow>
                  <TableCell colSpan={3} className="py-8 text-center text-xs text-muted-foreground">
                    暂无数据
                  </TableCell>
                </TableRow>
              ) : (
                (stats?.bySource ?? []).map((item) => (
                  <TableRow key={item.source}>
                    <TableCell className="text-xs">{SOURCE_LABEL[item.source] ?? item.source}</TableCell>
                    <TableCell className="text-right font-mono text-xs tabular-nums">{formatPoints(item.amount)}</TableCell>
                    <TableCell className="text-right font-mono text-xs tabular-nums">
                      {item.source === "integral"
                        ? `${item.units} 积分`
                        : item.source === "wallet"
                          ? `¥${(item.units / 100).toFixed(2)}`
                          : "—"}
                    </TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
        </div>

        <div className="overflow-hidden rounded-2xl border border-border bg-card">
          <div className="border-b border-border px-4 py-3 text-sm font-semibold">模型</div>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>模型</TableHead>
                <TableHead className="text-right">调用</TableHead>
                <TableHead className="text-right">Token</TableHead>
                <TableHead className="text-right">扣费</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {(stats?.byModel ?? []).length === 0 ? (
                <TableRow>
                  <TableCell colSpan={4} className="py-8 text-center text-xs text-muted-foreground">
                    暂无数据
                  </TableCell>
                </TableRow>
              ) : (
                (stats?.byModel ?? []).map((item) => (
                  <TableRow
                    key={item.model}
                    className="cursor-pointer"
                    onClick={() => onOpenRequests({ model: item.model })}
                  >
                    <TableCell className="font-mono text-xs">{item.model}</TableCell>
                    <TableCell className="text-right font-mono text-xs tabular-nums">{item.requests}</TableCell>
                    <TableCell className="text-right font-mono text-xs tabular-nums">
                      {formatTokens(item.inputTokens)} / {formatTokens(item.outputTokens)}
                    </TableCell>
                    <TableCell className="text-right font-mono text-xs tabular-nums">{formatPoints(item.charged)}</TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
        </div>
      </div>
    </div>
  );
}
