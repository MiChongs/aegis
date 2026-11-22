"use client";

import { useMemo, useState } from "react";
import { Area, AreaChart, Bar, BarChart, CartesianGrid, Cell, Label, Pie, PieChart, XAxis, YAxis } from "recharts";
import {
  ArrowRight,
  Award,
  CalendarClock,
  FileClock,
  HardDrive,
  Layers,
  Loader2,
  Minus,
  PackageCheck,
  Plus,
  Settings2,
  Shirt,
  Sparkles,
  TrendingUp,
  Trophy,
  Users
} from "lucide-react";
import { toast } from "sonner";
import { MetricGrid } from "@/components/users/detail/records/records-shared";
import { MetricCard, Section, StateBlock, ToneIcon, errorMessage } from "@/components/users/detail/kit";
import { Button } from "@/components/ui/button";
import {
  ChartContainer,
  ChartLegend,
  ChartLegendContent,
  ChartTooltip,
  ChartTooltipContent,
  type ChartConfig
} from "@/components/ui/chart";
import { Label as FieldLabel } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import type { CosmeticCatalog, CosmeticSlot, CosmeticStats } from "@/lib/api/cosmetics";
import {
  useCosmeticSettingsQuery,
  useCosmeticStatsQuery,
  useSaveCosmeticSettingsMutation
} from "@/lib/cosmetic-hooks";
import { formatRelative, localDateKey } from "@/lib/datetime";
import { cn } from "@/lib/utils";
import { CosmeticItemPreview } from "./cosmetic-preview";
import { WorkshopSettingsCard } from "./cosmetics-workshop-settings";
import { RarityBadge, SLOT_META, SLOT_ORDER, formatBytes, formatCount, slotLabel, sourceLabel } from "./cosmetic-shared";

const SLOT_COLORS: Record<CosmeticSlot, { light: string; dark: string }> = {
  avatar_frame: { light: "#8b5cf6", dark: "#a78bfa" },
  profile_card: { light: "#0ea5e9", dark: "#38bdf8" },
  name_style: { light: "#6366f1", dark: "#818cf8" },
  badge: { light: "#f59e0b", dark: "#fbbf24" }
};

const SOURCE_PALETTE = [
  { light: "#6366f1", dark: "#818cf8" },
  { light: "#10b981", dark: "#34d399" },
  { light: "#f59e0b", dark: "#fbbf24" },
  { light: "#0ea5e9", dark: "#38bdf8" },
  { light: "#ec4899", dark: "#f472b6" },
  { light: "#8b5cf6", dark: "#a78bfa" },
  { light: "#ef4444", dark: "#f87171" },
  { light: "#64748b", dark: "#94a3b8" }
];

const GRANT_CHART: ChartConfig = { count: { label: "发放次数", theme: { light: "#8b5cf6", dark: "#a78bfa" } } };

const SLOT_CHART: ChartConfig = {
  items: { label: "装扮数", theme: { light: "#94a3b8", dark: "#64748b" } },
  owners: { label: "持有人数", theme: { light: "#8b5cf6", dark: "#a78bfa" } },
  equipped: { label: "佩戴中", theme: { light: "#10b981", dark: "#34d399" } }
};

/**
 * 概览：一个 stats 接口取齐（槽位、来源、近 14 天发放、热门、即将到期、素材占用、待发布草稿），
 * 右栏是应用设置（开关与勋章位数）。
 */
export function CosmeticsOverviewPanel({
  appKey,
  catalog,
  canWrite,
  onOpenLibrary,
  onOpenAssets,
  onOpenOwnerships,
  onOpenItem
}: {
  appKey: string;
  catalog?: CosmeticCatalog | null;
  canWrite: boolean;
  onOpenLibrary: (filter?: { slot?: CosmeticSlot; dirtyOnly?: boolean }) => void;
  onOpenAssets: () => void;
  onOpenOwnerships: (filter?: { cosmeticId?: number; source?: string; state?: string }) => void;
  onOpenItem: (itemId: number) => void;
}) {
  const statsQuery = useCosmeticStatsQuery(appKey);
  const stats = statsQuery.data;
  const loading = statsQuery.isLoading;

  if (statsQuery.isError && !stats) {
    return (
      <Section title="概览" icon={Sparkles} tone="violet">
        <StateBlock kind="error" description={errorMessage(statsQuery.error)} onRetry={() => void statsQuery.refetch()} />
      </Section>
    );
  }

  return (
    <div className="flex flex-col gap-4">
      <MetricGrid>
        <MetricCard
          label="装扮"
          icon={Shirt}
          tone="violet"
          loading={loading}
          value={formatCount(stats?.items)}
          sub={`上架 ${formatCount(stats?.liveItems)} 件`}
          onClick={() => onOpenLibrary()}
        />
        <MetricCard
          label="持有人数"
          icon={Users}
          tone="primary"
          loading={loading}
          value={formatCount(stats?.owners)}
          sub={`持有记录 ${formatCount(stats?.ownerships)} 条`}
          onClick={() => onOpenOwnerships({ state: "live" })}
        />
        <MetricCard label="佩戴人数" icon={PackageCheck} tone="success" loading={loading} value={formatCount(stats?.wearers)} sub="此刻身上至少一件" />
        <MetricCard
          label="即将到期"
          icon={CalendarClock}
          tone={stats?.expiringSoon ? "warning" : "neutral"}
          loading={loading}
          value={formatCount(stats?.expiringSoon)}
          sub="限时持有"
          onClick={() => onOpenOwnerships({ state: "live" })}
        />
        <MetricCard
          label="待发布草稿"
          icon={FileClock}
          tone={stats?.draftsPending ? "warning" : "neutral"}
          loading={loading}
          value={formatCount(stats?.draftsPending)}
          sub="有未发布的修改"
          onClick={() => onOpenLibrary({ dirtyOnly: true })}
        />
        <MetricCard
          label="素材"
          icon={HardDrive}
          tone="info"
          loading={loading}
          value={formatCount(stats?.assetCount)}
          sub={`占用 ${formatBytes(stats?.assetBytes)}`}
          onClick={onOpenAssets}
        />
        <MetricCard
          label="近 14 天发放"
          icon={TrendingUp}
          tone="violet"
          loading={loading}
          value={formatCount(fillDays(stats?.dailyGrants ?? [], 14).reduce((sum, row) => sum + row.count, 0))}
          sub="次"
          spark={fillDays(stats?.dailyGrants ?? [], 14).map((row) => row.count)}
        />
        <MetricCard
          label="来源"
          icon={Layers}
          tone="neutral"
          loading={loading}
          value={formatCount(stats?.bySource?.filter((row) => row.count > 0).length)}
          sub="种获得途径"
        />
      </MetricGrid>

      <div className="grid gap-4 xl:grid-cols-5">
        <GrantTrend className="xl:col-span-3" loading={loading} stats={stats} />
        <SettingsCard className="xl:col-span-2" appKey={appKey} catalog={catalog} canWrite={canWrite} />
      </div>

      <WorkshopSettingsCard appKey={appKey} catalog={catalog} canWrite={canWrite} />

      <div className="grid gap-4 xl:grid-cols-2">
        <SlotDistribution loading={loading} stats={stats} catalog={catalog} onOpenSlot={(slot) => onOpenLibrary({ slot })} />
        <SourceMix loading={loading} stats={stats} catalog={catalog} onOpenSource={(source) => onOpenOwnerships({ source })} />
      </div>

      <div className="grid gap-4 xl:grid-cols-5">
        <TopItems
          className="xl:col-span-3"
          appKey={appKey}
          loading={loading}
          stats={stats}
          catalog={catalog}
          onOpenItem={onOpenItem}
          onOpenOwnerships={(cosmeticId) => onOpenOwnerships({ cosmeticId })}
        />
        <RecentGrants className="xl:col-span-2" loading={loading} stats={stats} catalog={catalog} onOpenAll={() => onOpenOwnerships()} />
      </div>
    </div>
  );
}

/** 按浏览器时区补齐最近 n 天（服务端只返回有发放的日子） */
function fillDays(rows: Array<{ day: string; count: number }>, days: number) {
  const byDay = new Map(rows.map((row) => [row.day.slice(0, 10), row.count]));
  const out: Array<{ day: string; label: string; count: number }> = [];
  const today = new Date();
  for (let i = days - 1; i >= 0; i--) {
    const date = new Date(today.getFullYear(), today.getMonth(), today.getDate() - i);
    const key = localDateKey(date);
    out.push({ day: key, label: key.slice(5).replace("-", "/"), count: byDay.get(key) ?? 0 });
  }
  return out;
}

function GrantTrend({ className, loading, stats }: { className?: string; loading: boolean; stats?: CosmeticStats }) {
  const rows = useMemo(() => fillDays(stats?.dailyGrants ?? [], 14), [stats]);
  const empty = rows.every((row) => row.count === 0);
  return (
    <Section className={className} title="发放趋势" icon={TrendingUp} tone="violet" description="近 14 天每日新增的持有记录">
      {loading ? (
        <Skeleton className="h-[240px] w-full rounded-lg" />
      ) : empty ? (
        <StateBlock compact title="近 14 天没有发放" />
      ) : (
        <ChartContainer config={GRANT_CHART} className="h-[240px] w-full">
          <AreaChart data={rows} margin={{ left: 0, right: 8, top: 8 }}>
            <CartesianGrid vertical={false} strokeDasharray="3 3" />
            <XAxis dataKey="label" tickLine={false} axisLine={false} fontSize={11} minTickGap={16} />
            <YAxis allowDecimals={false} tickLine={false} axisLine={false} fontSize={11} width={32} />
            <ChartTooltip content={<ChartTooltipContent indicator="line" />} />
            <Area
              type="monotone"
              dataKey="count"
              stroke="var(--color-count)"
              fill="var(--color-count)"
              fillOpacity={0.12}
              strokeWidth={2}
              dot={false}
              activeDot={{ r: 4 }}
            />
          </AreaChart>
        </ChartContainer>
      )}
    </Section>
  );
}

function SlotDistribution({
  loading,
  stats,
  catalog,
  onOpenSlot
}: {
  loading: boolean;
  stats?: CosmeticStats;
  catalog?: CosmeticCatalog | null;
  onOpenSlot: (slot: CosmeticSlot) => void;
}) {
  const rows = useMemo(
    () =>
      SLOT_ORDER.map((slot) => {
        const row = stats?.bySlot?.find((item) => item.slot === slot);
        return { slot, label: slotLabel(slot, catalog), items: row?.items ?? 0, owners: row?.owners ?? 0, equipped: row?.equipped ?? 0 };
      }),
    [stats, catalog]
  );
  return (
    <Section
      title="槽位分布"
      icon={Layers}
      tone="primary"
      description="每个槽位的装扮数、持有人数与佩戴中"
      footer={
        <div className="flex flex-wrap gap-1.5">
          {rows.map((row) => {
            const meta = SLOT_META[row.slot];
            return (
              <button
                key={row.slot}
                type="button"
                onClick={() => onOpenSlot(row.slot)}
                className="inline-flex h-7 items-center gap-1.5 rounded-md border bg-background px-2.5 text-xs text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
              >
                <meta.icon className="size-3.5" />
                {row.label}
                <span className="tabular-nums text-foreground">{formatCount(row.items)}</span>
              </button>
            );
          })}
        </div>
      }
    >
      {loading ? (
        <Skeleton className="h-[220px] w-full rounded-lg" />
      ) : (
        <ChartContainer config={SLOT_CHART} className="h-[220px] w-full">
          <BarChart data={rows} margin={{ left: 0, right: 8, top: 8 }} barGap={2}>
            <CartesianGrid vertical={false} strokeDasharray="3 3" />
            <XAxis dataKey="label" tickLine={false} axisLine={false} fontSize={11} />
            <YAxis allowDecimals={false} tickLine={false} axisLine={false} fontSize={11} width={36} />
            <ChartTooltip content={<ChartTooltipContent />} />
            <ChartLegend content={<ChartLegendContent />} />
            <Bar dataKey="items" fill="var(--color-items)" radius={[3, 3, 0, 0]} maxBarSize={22} />
            <Bar dataKey="owners" fill="var(--color-owners)" radius={[3, 3, 0, 0]} maxBarSize={22} />
            <Bar dataKey="equipped" fill="var(--color-equipped)" radius={[3, 3, 0, 0]} maxBarSize={22} />
          </BarChart>
        </ChartContainer>
      )}
    </Section>
  );
}

function SourceMix({
  loading,
  stats,
  catalog,
  onOpenSource
}: {
  loading: boolean;
  stats?: CosmeticStats;
  catalog?: CosmeticCatalog | null;
  onOpenSource: (source: string) => void;
}) {
  const rows = useMemo(
    () =>
      (stats?.bySource ?? [])
        .filter((row) => row.count > 0)
        .sort((a, b) => b.count - a.count)
        .map((row, index) => ({ ...row, key: `s${index}`, label: sourceLabel(row.source, catalog) })),
    [stats, catalog]
  );
  const total = rows.reduce((sum, row) => sum + row.count, 0);
  const config = useMemo<ChartConfig>(
    () => Object.fromEntries(rows.map((row, index) => [row.key, { label: row.label, theme: SOURCE_PALETTE[index % SOURCE_PALETTE.length] }])),
    [rows]
  );
  return (
    <Section title="来源分布" icon={Award} tone="warning" description="持有记录从哪些系统发出">
      {loading ? (
        <Skeleton className="h-[220px] w-full rounded-lg" />
      ) : total === 0 ? (
        <StateBlock compact title="暂无持有记录" />
      ) : (
        <div className="grid items-center gap-4 sm:grid-cols-[220px_minmax(0,1fr)]">
          <ChartContainer config={config} className="mx-auto h-[200px] w-[200px]">
            <PieChart>
              <ChartTooltip content={<ChartTooltipContent nameKey="key" hideLabel />} />
              <Pie data={rows} dataKey="count" nameKey="key" innerRadius={56} outerRadius={84} strokeWidth={2} paddingAngle={2}>
                {rows.map((row) => (
                  <Cell key={row.key} fill={`var(--color-${row.key})`} />
                ))}
                <Label
                  content={({ viewBox }) => {
                    if (!viewBox || !("cx" in viewBox)) return null;
                    return (
                      <text x={viewBox.cx} y={viewBox.cy} textAnchor="middle" dominantBaseline="middle">
                        <tspan x={viewBox.cx} y={viewBox.cy} className="fill-foreground text-xl font-bold">
                          {total.toLocaleString("zh-CN")}
                        </tspan>
                        <tspan x={viewBox.cx} y={(viewBox.cy ?? 0) + 18} className="fill-muted-foreground text-[10px]">
                          条记录
                        </tspan>
                      </text>
                    );
                  }}
                />
              </Pie>
            </PieChart>
          </ChartContainer>
          <ul className="flex flex-col divide-y">
            {rows.map((row) => (
              <li key={row.key}>
                <button
                  type="button"
                  onClick={() => onOpenSource(row.source)}
                  className="flex w-full items-center gap-2.5 py-2 text-left text-sm transition-colors hover:text-foreground"
                >
                  <span className="size-2.5 shrink-0 rounded-sm" style={{ backgroundColor: `var(--color-${row.key})` }} />
                  <span className="min-w-0 flex-1 truncate">{row.label}</span>
                  <span className="tabular-nums text-muted-foreground">{formatCount(row.count)}</span>
                  <span className="w-12 text-right text-xs tabular-nums text-muted-foreground">
                    {Math.round((row.count / total) * 100)}%
                  </span>
                </button>
              </li>
            ))}
          </ul>
        </div>
      )}
    </Section>
  );
}

function TopItems({
  className,
  appKey,
  loading,
  stats,
  catalog,
  onOpenItem,
  onOpenOwnerships
}: {
  className?: string;
  appKey: string;
  loading: boolean;
  stats?: CosmeticStats;
  catalog?: CosmeticCatalog | null;
  onOpenItem: (itemId: number) => void;
  onOpenOwnerships: (cosmeticId: number) => void;
}) {
  const items = stats?.topItems ?? [];
  const max = Math.max(1, ...items.map((item) => item.owners));
  return (
    <Section className={className} flush title="热门装扮" icon={Trophy} tone="warning" description="持有人数最多的装扮">
      {loading ? (
        <div className="flex flex-col gap-3 p-5">
          {[0, 1, 2, 3].map((index) => (
            <Skeleton key={index} className="h-12 w-full" />
          ))}
        </div>
      ) : !items.length ? (
        <StateBlock compact title="暂无持有" />
      ) : (
        <ol className="flex flex-col divide-y">
          {items.map((item, index) => (
            <li key={item.id} className="group flex items-center gap-3 px-4 py-2.5 sm:px-5">
              <span
                className={cn(
                  "w-5 shrink-0 text-center text-sm font-semibold tabular-nums",
                  index < 3 ? "text-amber-600 dark:text-amber-400" : "text-muted-foreground"
                )}
              >
                {index + 1}
              </span>
              <button
                type="button"
                onClick={() => onOpenItem(item.id)}
                className="shrink-0 rounded-lg border bg-muted/40 transition-colors hover:bg-muted"
                aria-label={`打开 ${item.name}`}
              >
                <CosmeticItemPreview appKey={appKey} item={{ id: item.id, slot: item.slot, name: item.name }} width={item.slot === "name_style" ? 88 : 44} height={44} />
              </button>
              <div className="flex min-w-0 flex-1 flex-col gap-1.5">
                <div className="flex items-center justify-between gap-3">
                  <span className="flex min-w-0 items-center gap-2">
                    <span className="truncate text-sm font-medium">{item.name}</span>
                    <span className="shrink-0 text-xs text-muted-foreground">{slotLabel(item.slot, catalog)}</span>
                    <RarityBadge rarity={item.rarity} catalog={catalog} className="hidden sm:inline-flex" />
                  </span>
                  <span className="shrink-0 text-xs tabular-nums text-muted-foreground">
                    <span className="font-medium text-foreground">{formatCount(item.owners)}</span> 人持有
                    <span className="ml-2">{formatCount(item.equipped)} 人佩戴</span>
                  </span>
                </div>
                <div className="h-1.5 overflow-hidden rounded-full bg-muted">
                  <div className="h-full rounded-full bg-violet-500/70" style={{ width: `${(item.owners / max) * 100}%` }} />
                </div>
              </div>
              <Button size="icon-xs" variant="ghost" onClick={() => onOpenOwnerships(item.id)} aria-label="查看持有记录">
                <ArrowRight />
              </Button>
            </li>
          ))}
        </ol>
      )}
    </Section>
  );
}

function RecentGrants({
  className,
  loading,
  stats,
  catalog,
  onOpenAll
}: {
  className?: string;
  loading: boolean;
  stats?: CosmeticStats;
  catalog?: CosmeticCatalog | null;
  onOpenAll: () => void;
}) {
  const rows = (stats?.recentGrants ?? []).slice(0, 8);
  return (
    <Section
      className={className}
      flush
      title="最近发放"
      icon={Sparkles}
      tone="violet"
      actions={
        <Button size="xs" variant="ghost" onClick={onOpenAll}>
          全部记录
          <ArrowRight />
        </Button>
      }
    >
      {loading ? (
        <div className="flex flex-col gap-3 p-5">
          {[0, 1, 2].map((index) => (
            <Skeleton key={index} className="h-9 w-full" />
          ))}
        </div>
      ) : !rows.length ? (
        <StateBlock compact title="暂无发放" />
      ) : (
        <ul className="flex flex-col divide-y">
          {rows.map((row) => {
            const meta = row.slot ? SLOT_META[row.slot as CosmeticSlot] : undefined;
            return (
              <li key={row.id} className="flex items-center gap-3 px-4 py-2.5 sm:px-5">
                {meta ? <ToneIcon icon={meta.icon} tone={meta.tone} size="sm" /> : null}
                <div className="flex min-w-0 flex-1 flex-col">
                  <span className="truncate text-sm">
                    {row.userNickname || row.userAccount || `用户 #${row.userId}`}
                    <span className="mx-1.5 text-muted-foreground">获得</span>
                    {row.cosmeticName || `#${row.cosmeticId}`}
                  </span>
                  <span className="truncate text-xs text-muted-foreground">
                    {sourceLabel(row.source, catalog)}，{row.durationDays > 0 ? `${row.durationDays} 天` : "永久"}
                  </span>
                </div>
                <span className="shrink-0 text-xs text-muted-foreground">{formatRelative(row.createdAt)}</span>
              </li>
            );
          })}
        </ul>
      )}
    </Section>
  );
}

/** 应用设置：开关 + 勋章同时佩戴数（1–目录上限）。只提交这两项，工坊设置由 WorkshopSettingsCard 单独保存 */
function SettingsCard({
  className,
  appKey,
  catalog,
  canWrite
}: {
  className?: string;
  appKey: string;
  catalog?: CosmeticCatalog | null;
  canWrite: boolean;
}) {
  const settingsQuery = useCosmeticSettingsQuery(appKey);
  const save = useSaveCosmeticSettingsMutation(appKey);
  const settings = settingsQuery.data;
  const max = catalog?.vocabulary?.limits?.maxBadgeSlots ?? 5;
  const [draft, setDraft] = useState<{ scope: string; badgeSlots: number } | null>(null);
  const badgeSlots = draft?.scope === appKey ? draft.badgeSlots : (settings?.badgeSlots ?? 3);
  const dirty = Boolean(settings) && badgeSlots !== settings?.badgeSlots;

  const submit = async (next: { enabled: boolean; badgeSlots: number }) => {
    try {
      await save.mutateAsync(next);
      toast.success(next.enabled !== settings?.enabled ? (next.enabled ? "个性装扮已开启" : "个性装扮已关闭") : "设置已保存");
      setDraft(null);
    } catch (error) {
      toast.error(errorMessage(error, "保存失败"));
    }
  };

  return (
    <Section className={className} title="设置" icon={Settings2} tone="neutral">
      {settingsQuery.isLoading || !settings ? (
        <Skeleton className="h-[180px] w-full rounded-lg" />
      ) : (
        <div className="flex flex-col gap-5">
          <label className="flex items-center justify-between gap-4">
            <span className="flex flex-col gap-0.5">
              <span className="text-sm font-medium">对用户开放</span>
              <span className="text-xs text-muted-foreground">{settings.enabled ? "装扮中心可用" : "装扮中心未开启"}</span>
            </span>
            <span className="flex items-center gap-2">
              {save.isPending ? <Loader2 className="size-3.5 animate-spin text-muted-foreground" /> : null}
              <Switch
                checked={settings.enabled}
                disabled={!canWrite || save.isPending}
                onCheckedChange={(enabled) => void submit({ enabled, badgeSlots: settings.badgeSlots })}
                aria-label="对用户开放个性装扮"
              />
            </span>
          </label>

          <div className="flex flex-col gap-2">
            <FieldLabel className="text-xs text-muted-foreground">勋章同时佩戴数</FieldLabel>
            <div className="flex items-center gap-3">
              <div className="flex items-center rounded-lg border">
                <Button
                  size="icon-sm"
                  variant="ghost"
                  disabled={!canWrite || badgeSlots <= 1}
                  onClick={() => setDraft({ scope: appKey, badgeSlots: Math.max(1, badgeSlots - 1) })}
                  aria-label="减少"
                >
                  <Minus />
                </Button>
                <span className="w-10 text-center text-sm font-semibold tabular-nums">{badgeSlots}</span>
                <Button
                  size="icon-sm"
                  variant="ghost"
                  disabled={!canWrite || badgeSlots >= max}
                  onClick={() => setDraft({ scope: appKey, badgeSlots: Math.min(max, badgeSlots + 1) })}
                  aria-label="增加"
                >
                  <Plus />
                </Button>
              </div>
              <div className="flex items-center gap-1" aria-hidden>
                {Array.from({ length: max }, (_, index) => (
                  <span
                    key={index}
                    className={cn("size-5 rounded-full border", index < badgeSlots ? "border-amber-500/50 bg-amber-500/20" : "bg-muted")}
                  />
                ))}
              </div>
            </div>
          </div>

          <div className="flex items-center justify-between gap-2 border-t pt-4">
            <span className="text-xs text-muted-foreground">
              {settings.updatedAt ? `${formatRelative(settings.updatedAt)} 由${settings.updatedBy || "管理员"}修改` : "未修改过"}
            </span>
            <Button
              size="sm"
              disabled={!canWrite || !dirty || save.isPending}
              onClick={() => void submit({ enabled: settings.enabled, badgeSlots })}
            >
              {save.isPending ? <Loader2 className="animate-spin" /> : null}
              保存
            </Button>
          </div>
        </div>
      )}
    </Section>
  );
}
