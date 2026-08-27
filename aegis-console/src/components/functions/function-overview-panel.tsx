"use client";

import { useState } from "react";
import { Area, AreaChart, CartesianGrid, XAxis, YAxis } from "recharts";
import { AlertCircle } from "lucide-react";
import type { AppFunction } from "@/lib/api/app-functions";
import { useFunctionStatsQuery } from "@/lib/function-hooks";
import {
  ChartContainer,
  ChartLegend,
  ChartLegendContent,
  ChartTooltip,
  ChartTooltipContent,
  type ChartConfig
} from "@/components/ui/chart";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { StatTile, formatDuration, formatRate, formatTime } from "./function-shared";

/**
 * 成功与失败刻意分色系（绿 / 红）而不是同色系深浅。
 * 「跑了很多次」和「失败了很多次」是必须一眼分得开的两件事。
 */
const CHART_CONFIG = {
  success: { label: "成功", theme: { light: "oklch(0.62 0.14 155)", dark: "oklch(0.72 0.15 155)" } },
  failed: { label: "失败", theme: { light: "oklch(0.58 0.20 25)", dark: "oklch(0.70 0.19 25)" } }
} satisfies ChartConfig;

export function FunctionOverviewPanel({
  appKey,
  selected
}: {
  appKey: string;
  selected: AppFunction;
}) {
  const [hours, setHours] = useState(24);
  const statsQuery = useFunctionStatsQuery(appKey, selected.name, hours);
  const stats = statsQuery.data;

  const buckets = (stats?.buckets ?? []).map((bucket) => ({
    at: new Date(bucket.at).toLocaleString("zh-CN", { month: "numeric", day: "numeric", hour: "2-digit" }),
    success: bucket.success,
    failed: bucket.failed
  }));

  return (
    <div className="space-y-4">
      {/* 未激活是「调用全部返回 40990」的唯一原因，必须直说 ——
          不说的话调用方只会看到一串不明所以的 409 */}
      {selected.status !== "active" || !selected.activeVersion ? (
        <div className="flex items-start gap-2 rounded-lg border border-amber-500/40 bg-amber-500/5 p-3 text-sm">
          <AlertCircle className="mt-0.5 size-4 shrink-0 text-amber-600 dark:text-amber-400" />
          <span>
            该函数<strong>当前不可被调用</strong>：
            {!selected.activeVersion
              ? "尚未激活版本，请在「脚本」页发布并激活。"
              : `当前状态为 ${selected.status}，请在「设置」页改为 active。`}
          </span>
        </div>
      ) : null}

      <Card>
        <CardHeader className="flex-row items-start justify-between gap-3">
          <div>
            <CardTitle>运行状况</CardTitle>
            <CardDescription>成功率仅统计已结束的调用。</CardDescription>
          </div>
          <Select value={String(hours)} onValueChange={(value) => setHours(Number(value))}>
            <SelectTrigger className="h-8 w-28 text-xs">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="1">近 1 小时</SelectItem>
              <SelectItem value="24">近 24 小时</SelectItem>
              <SelectItem value="168">近 7 天</SelectItem>
              <SelectItem value="720">近 30 天</SelectItem>
            </SelectContent>
          </Select>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="grid gap-3 sm:grid-cols-3 lg:grid-cols-6">
            <StatTile label="调用总数" value={String(stats?.total ?? 0)} />
            <StatTile
              label="成功率"
              value={formatRate(stats?.successRate ?? 0, stats?.total ?? 0)}
              tone={stats && stats.total > 0 && stats.successRate < 0.9 ? "danger" : "success"}
              hint={stats?.total ? undefined : "暂无调用"}
            />
            <StatTile label="失败" value={String(stats?.failed ?? 0)} tone={stats?.failed ? "danger" : "default"} />
            <StatTile label="平均耗时" value={formatDuration(stats?.avgMs ?? 0)} />
            <StatTile label="P95 耗时" value={formatDuration(stats?.p95Ms ?? 0)} hint={`超时 ${selected.timeoutMs} ms`} />
            <StatTile label="最近调用" value={stats?.lastInvokedAt ? formatTime(stats.lastInvokedAt) : "—"} />
          </div>

          {buckets.length ? (
            <ChartContainer config={CHART_CONFIG} className="h-56 w-full">
              <AreaChart data={buckets}>
                <CartesianGrid vertical={false} strokeDasharray="3 3" />
                <XAxis dataKey="at" tickLine={false} axisLine={false} fontSize={11} minTickGap={24} />
                <YAxis tickLine={false} axisLine={false} fontSize={11} width={32} allowDecimals={false} />
                <ChartTooltip content={<ChartTooltipContent />} />
                <ChartLegend content={<ChartLegendContent />} />
                <Area
                  type="monotone"
                  dataKey="success"
                  stackId="1"
                  stroke="var(--color-success)"
                  fill="var(--color-success)"
                  fillOpacity={0.2}
                />
                <Area
                  type="monotone"
                  dataKey="failed"
                  stackId="1"
                  stroke="var(--color-failed)"
                  fill="var(--color-failed)"
                  fillOpacity={0.25}
                />
              </AreaChart>
            </ChartContainer>
          ) : null}
        </CardContent>
      </Card>

      <div className="grid gap-4 lg:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle>高频错误</CardTitle>
            <CardDescription>所选时段内出现次数最多的失败原因</CardDescription>
          </CardHeader>
          <CardContent className="space-y-2">
            {stats?.topErrors?.length ? (
              stats.topErrors.map((item) => (
                <div key={item.message} className="flex items-start gap-2 rounded-lg border p-2.5">
                  <Badge variant="danger" size="sm" className="shrink-0">
                    {item.count}
                  </Badge>
                  <span className="min-w-0 break-all text-xs">{item.message}</span>
                </div>
              ))
            ) : (
              <p className="py-8 text-center text-sm text-muted-foreground">
                所选时段内无失败记录
              </p>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>当前配置</CardTitle>
            <CardDescription>运行参数可在「设置」页调整</CardDescription>
          </CardHeader>
          <CardContent className="grid gap-3 sm:grid-cols-2">
            <StatTile label="运行时" value={selected.runtime} />
            <StatTile label="激活版本" value={selected.activeVersion || "无"} />
            <StatTile label="超时" value={`${selected.timeoutMs} ms`} />
            <StatTile label="并发上限" value={`${selected.maxConcurrency}`} hint="单实例" />
            <StatTile
              label="频次上限"
              value={selected.rateLimitPerMin ? `${selected.rateLimitPerMin}/分钟` : "不限"}
              hint={selected.rateLimitPerMin ? "跨实例准确" : undefined}
            />
            <StatTile
              label="已声明能力"
              value={String(selected.capabilities.length)}
              hint={selected.capabilities.join("、") || "无"}
            />
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
