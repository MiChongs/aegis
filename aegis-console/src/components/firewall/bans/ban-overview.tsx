"use client";

import { useMemo, useState, type ReactNode } from "react";
import NumberFlow from "@number-flow/react";
import { Bar, BarChart, CartesianGrid, XAxis, YAxis } from "recharts";
import { ArrowUpRight, ChartColumn, Clock, Infinity as InfinityIcon, PieChart, ShieldBan, ShieldCheck, Activity, type LucideIcon } from "lucide-react";
import { toast } from "sonner";
import { TonalIcon, type KitTone } from "@/components/apps/detail/app-detail-kit";
import { CardEmpty, longDay, OverviewCard, shortDay } from "@/components/overview/overview-kit";
import { ChartContainer, ChartTooltip, ChartTooltipContent, type ChartConfig } from "@/components/ui/chart";
import { CountryFlag } from "@/components/ui/country-flag";
import { Skeleton } from "@/components/ui/skeleton";
import { Spinner } from "@/components/ui/spinner";
import type { BanStats } from "@/lib/api/firewall-bans";
import { banErrorMessage, useBanStatsQuery } from "@/lib/firewall-ban-hooks";
import { cn } from "@/lib/utils";
import { countryName, formatShare, RankedList, Segmented } from "../overview/overview-parts";
import { SOURCE_META, sourceLabel, useBanModes } from "./ban-format";
import type { BanListFilter } from "./ban-list-state";

/**
 * 封禁概况：四个指标格 + 近 14 天新增 / 解除 + 生效中的分布。
 * 指标格与分布项都能点：点了把对应条件套到下方列表（概况只负责「看出哪里多」，具体是哪些交给列表）。
 */

const CHART_CONFIG = {
  created: { label: "新增", theme: { light: "#e11d48", dark: "#fb7185" } },
  revoked: { label: "解除", theme: { light: "#0284c7", dark: "#38bdf8" } },
  expired: { label: "到期", theme: { light: "#94a3b8", dark: "#64748b" } }
} satisfies ChartConfig;

type Dimension = "source" | "mode" | "country";

export function BanOverview({ onApply }: { onApply: (patch: Partial<BanListFilter>) => void }) {
  const statsQuery = useBanStatsQuery();
  const stats = statsQuery.data;

  if (!stats) {
    if (statsQuery.isError) {
      return (
        <div className="rounded-xl border border-dashed px-4 py-6 text-center text-sm text-muted-foreground">{banErrorMessage(statsQuery.error, "封禁概况读取失败")}</div>
      );
    }
    return (
      <div className="flex min-w-0 flex-col gap-4">
        <div className="grid grid-cols-2 gap-3 @3xl:grid-cols-4">
          {Array.from({ length: 4 }).map((_, index) => (
            <Skeleton key={index} className="h-[104px] rounded-xl" />
          ))}
        </div>
        <div className="grid gap-4 @4xl:grid-cols-[minmax(0,1fr)_minmax(0,360px)]">
          <Skeleton className="h-[300px] rounded-xl" />
          <Skeleton className="h-[300px] rounded-xl" />
        </div>
      </div>
    );
  }

  return (
    <div className="flex min-w-0 flex-col gap-4">
      <div className="grid min-w-0 grid-cols-2 gap-3 @3xl:grid-cols-4">
        <BanTile
          icon={ShieldBan}
          tone="rose"
          label="生效中"
          value={stats.active}
          footer={
            <span className="truncate tabular-nums">
              {(stats.bySource ?? [])
                .filter((item) => item.count > 0)
                .slice(0, 2)
                .map((item) => `${sourceLabel(item.key)} ${item.count.toLocaleString("zh-CN")}`)
                .join(" · ") || "暂无"}
            </span>
          }
          onClick={() => onApply({ status: "active", expiring: false, permanent: "" })}
        />
        <BanTile
          icon={InfinityIcon}
          tone="violet"
          label="永久"
          value={stats.permanent}
          footer={<span>占生效中 {formatShare(stats.permanent, stats.active)}</span>}
          onClick={() => onApply({ status: "active", permanent: "true", expiring: false })}
        />
        <BanTile
          icon={Clock}
          tone="amber"
          label="24 小时内到期"
          value={stats.expiringSoon}
          footer={<span>到期后自动解除</span>}
          onClick={() => onApply({ status: "active", expiring: true, permanent: "", sort: "expires_asc" })}
        />
        <BanTile
          icon={Activity}
          tone="sky"
          label="近 24 小时新增"
          value={stats.created24h}
          footer={
            <span className="tabular-nums">
              解除 <span className="font-medium text-foreground">{stats.revoked24h.toLocaleString("zh-CN")}</span> · 到期{" "}
              <span className="font-medium text-foreground">{stats.expired24h.toLocaleString("zh-CN")}</span>
            </span>
          }
        />
      </div>
      <div className="grid min-w-0 gap-4 @4xl:grid-cols-[minmax(0,1fr)_minmax(0,360px)]">
        <DailyCard stats={stats} refreshing={statsQuery.isFetching} />
        <BreakdownCard stats={stats} onApply={onApply} />
      </div>
    </div>
  );
}

/* ── 指标格 ── */

function BanTile({
  icon,
  tone,
  label,
  value,
  footer,
  onClick
}: {
  icon: LucideIcon;
  tone: KitTone;
  label: string;
  value: number;
  footer?: ReactNode;
  onClick?: () => void;
}) {
  const body = (
    <>
      <span className="flex min-w-0 items-center gap-2">
        <TonalIcon icon={icon} tone={tone} size="sm" />
        <span className="truncate text-xs text-muted-foreground">{label}</span>
        {onClick ? <ArrowUpRight aria-hidden className="ml-auto size-3.5 shrink-0 text-muted-foreground opacity-0 transition-opacity group-hover:opacity-100" /> : null}
      </span>
      <span className="truncate font-data text-2xl leading-8 font-semibold tracking-tight tabular-nums">
        <NumberFlow value={value} locales="zh-CN" format={value >= 100_000 ? { notation: "compact", maximumFractionDigits: 1 } : undefined} />
      </span>
      <span className="flex min-h-4 min-w-0 items-center gap-1 truncate text-[11px] text-muted-foreground">{footer}</span>
    </>
  );
  const className = "group flex min-w-0 flex-col gap-2 rounded-xl border bg-card p-3.5 text-left";
  if (!onClick) return <div className={className}>{body}</div>;
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        className,
        "transition-colors hover:border-foreground/15 hover:bg-muted/30 focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none"
      )}
    >
      {body}
    </button>
  );
}

/* ── 近 14 天 ── */

function DailyCard({ stats, refreshing }: { stats: BanStats; refreshing: boolean }) {
  const rows = useMemo(() => stats.daily ?? [], [stats.daily]);
  const totals = useMemo(
    () => rows.reduce((acc, row) => ({ created: acc.created + row.created, revoked: acc.revoked + row.revoked, expired: acc.expired + row.expired }), { created: 0, revoked: 0, expired: 0 }),
    [rows]
  );
  const empty = totals.created + totals.revoked + totals.expired === 0;

  return (
    <OverviewCard
      icon={ChartColumn}
      tone="rose"
      title="近 14 天"
      description="每天新增与解除（含自动到期）"
      action={refreshing ? <Spinner className="size-3.5 text-muted-foreground" /> : null}
    >
      <div className="flex flex-wrap items-center gap-x-4 gap-y-1.5">
        {(Object.keys(CHART_CONFIG) as Array<keyof typeof CHART_CONFIG>).map((key) => (
          <span key={key} className="inline-flex items-center gap-1.5 text-[11px] text-muted-foreground">
            <span aria-hidden className={cn("size-2 rounded-[2px]", key === "created" ? "bg-rose-500" : key === "revoked" ? "bg-sky-500" : "bg-slate-400")} />
            {CHART_CONFIG[key].label}
            <span className={cn("font-data font-medium tabular-nums", totals[key] > 0 && "text-foreground")}>{totals[key].toLocaleString("zh-CN")}</span>
          </span>
        ))}
      </div>
      <div className="mt-3 min-h-[200px] flex-1">
        {empty ? (
          <CardEmpty icon={ShieldCheck} title="近 14 天没有封禁变动" />
        ) : (
          <ChartContainer config={CHART_CONFIG} className="aspect-auto h-[200px] w-full">
            <BarChart data={rows} margin={{ top: 6, right: 4, bottom: 0, left: 0 }} barGap={2} barCategoryGap="22%">
              <CartesianGrid vertical={false} strokeDasharray="3 3" />
              <XAxis dataKey="day" tickLine={false} axisLine={false} tickMargin={8} minTickGap={16} fontSize={11} tickFormatter={(value: string) => shortDay(value)} />
              <YAxis tickLine={false} axisLine={false} width={36} fontSize={11} allowDecimals={false} />
              <ChartTooltip
                cursor={{ fill: "var(--muted)", opacity: 0.5 }}
                content={<ChartTooltipContent indicator="dot" labelFormatter={(value) => (typeof value === "string" ? longDay(value) : "")} />}
              />
              <Bar dataKey="created" fill="var(--color-created)" radius={[3, 3, 0, 0]} isAnimationActive={false} />
              <Bar dataKey="revoked" stackId="out" fill="var(--color-revoked)" isAnimationActive={false} />
              <Bar dataKey="expired" stackId="out" fill="var(--color-expired)" radius={[3, 3, 0, 0]} isAnimationActive={false} />
            </BarChart>
          </ChartContainer>
        )}
      </div>
    </OverviewCard>
  );
}

/* ── 分布 ── */

function BreakdownCard({ stats, onApply }: { stats: BanStats; onApply: (patch: Partial<BanListFilter>) => void }) {
  const [dimension, setDimension] = useState<Dimension>("source");
  const modes = useBanModes();

  return (
    <OverviewCard
      icon={PieChart}
      tone="violet"
      title="生效中的分布"
      description="点一项筛选下方列表"
      action={
        <Segmented
          value={dimension}
          onChange={setDimension}
          ariaLabel="分布维度"
          options={[
            { value: "source", label: "来源" },
            { value: "mode", label: "模式" },
            { value: "country", label: "国家" }
          ]}
        />
      }
    >
      {dimension === "source" ? (
        <RankedList
          items={stats.bySource ?? []}
          total={stats.active}
          barClassName="bg-violet-500/10 dark:bg-violet-500/15"
          emptyIcon={ShieldCheck}
          emptyTitle="当前没有生效中的封禁"
          onPick={(item) => onApply({ status: "active", source: item.key })}
          renderLabel={(item) => {
            const Icon = SOURCE_META[item.key]?.icon;
            return (
              <>
                {Icon ? <Icon className="size-3.5 shrink-0 text-muted-foreground" /> : null}
                <span className="truncate">{sourceLabel(item.key)}</span>
              </>
            );
          }}
        />
      ) : dimension === "mode" ? (
        <RankedList
          items={stats.byMode ?? []}
          total={stats.active}
          barClassName="bg-rose-500/10 dark:bg-rose-500/15"
          emptyIcon={ShieldCheck}
          emptyTitle="当前没有生效中的封禁"
          onPick={(item) => onApply({ status: "active", mode: item.key })}
          renderLabel={(item) => (
            <span className="truncate" title={modes.label(item.key)}>
              {modes.label(item.key)}
            </span>
          )}
        />
      ) : (
        <RankedList
          items={stats.byCountry ?? []}
          total={stats.active}
          barClassName="bg-sky-500/10 dark:bg-sky-500/15"
          emptyIcon={ShieldCheck}
          emptyTitle="当前没有生效中的封禁"
          onPick={(item) => {
            if (item.key === "??") {
              toast.info("归属地未知的封禁无法按国家筛选");
              return;
            }
            onApply({ status: "active", country: item.key });
          }}
          renderLabel={(item) =>
            item.key === "??" ? (
              <span className="truncate text-muted-foreground">未知</span>
            ) : (
              <>
                <CountryFlag code={item.key} size={14} />
                <span className="truncate">{countryName(item.key)}</span>
                <span className="shrink-0 font-mono text-[10px] text-muted-foreground">{item.key}</span>
              </>
            )
          }
        />
      )}
    </OverviewCard>
  );
}
