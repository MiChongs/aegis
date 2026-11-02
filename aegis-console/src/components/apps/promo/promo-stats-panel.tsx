"use client";

import { useMemo } from "react";
import Link from "next/link";
import { Bar, BarChart, CartesianGrid, XAxis, YAxis } from "recharts";
import {
  BadgeCheck,
  BarChart3,
  CircleX,
  Gift,
  Hourglass,
  Layers,
  Megaphone,
  Trophy,
  UserPlus,
  Users
} from "lucide-react";
import {
  PhaseBadge,
  StatusBadge,
  campaignPhase,
  formatCount,
  periodLabel,
  platformName
} from "@/components/apps/promo/promo-shared";
import { buildChartConfig, ChartCard, type ThemedColor } from "@/components/commerce/commerce-charts";
import { MetricGrid, percent } from "@/components/users/detail/records/records-shared";
import {
  FilterSelect,
  MetricCard,
  RecordTable,
  Section,
  StateBlock,
  errorMessage,
  type Column
} from "@/components/users/detail/kit";
import { userInitials } from "@/components/users/detail/user-detail-shared";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Card } from "@/components/ui/card";
import {
  ChartContainer,
  ChartLegend,
  ChartLegendContent,
  ChartTooltip,
  ChartTooltipContent
} from "@/components/ui/chart";
import { Skeleton } from "@/components/ui/skeleton";
import type { PromoRankEntry, PromoStats, PromoSubmissionStatus } from "@/lib/api/promo";
import { formatDateTime } from "@/lib/datetime";
import {
  usePromoCampaignQuery,
  usePromoCampaignsQuery,
  usePromoCatalogQuery,
  usePromoLeaderboardQuery,
  usePromoStatsQuery
} from "@/lib/promo-hooks";

const SERIES: Array<{ key: "invited" | "submitted" | "approved"; label: string; color: ThemedColor }> = [
  { key: "invited", label: "有效邀请", color: { light: "#6366f1", dark: "#818cf8" } },
  { key: "submitted", label: "提交文章", color: { light: "#0ea5e9", dark: "#38bdf8" } },
  { key: "approved", label: "审核通过", color: { light: "#10b981", dark: "#34d399" } }
];

type PlatformRow = PromoStats["byPlatform"][number];

/**
 * 活动统计。
 *
 * 选一期活动看：参与人数、活动期内的有效邀请与等待中邀请、各档领取、分享提交的审核结论（总数与分平台）、
 * 按日走势，以及邀请排行（管理端不打码，带账号）。数字口径见 docs/promo.md「邀请人数」一节；
 * 按日分桶以后端返回的日期为准。
 */
export function PromoStatsPanel({
  appKey,
  campaignId,
  onCampaignIdChange,
  onOpenSubmissions
}: {
  appKey: string;
  campaignId?: number;
  onCampaignIdChange: (campaignId?: number) => void;
  onOpenSubmissions: (campaignId: number, status?: PromoSubmissionStatus) => void;
}) {
  const campaignsQuery = usePromoCampaignsQuery(appKey, { page: 1, limit: 100 });
  const campaigns = useMemo(() => campaignsQuery.data?.items ?? [], [campaignsQuery.data]);
  // 没指定时落到第一期上线的活动，没有上线的就取列表第一期
  const selectedId = campaignId ?? (campaigns.find((item) => item.status === "online") ?? campaigns[0])?.id;
  const listed = campaigns.find((item) => item.id === selectedId);

  const detailQuery = usePromoCampaignQuery(appKey, selectedId);
  const statsQuery = usePromoStatsQuery(appKey, selectedId);
  const leaderboardQuery = usePromoLeaderboardQuery(appKey, selectedId);
  const catalogQuery = usePromoCatalogQuery(appKey);

  const campaign = detailQuery.data ?? listed;
  const stats = statsQuery.data;
  const loading = statsQuery.isLoading;
  const chartConfig = useMemo(() => buildChartConfig(SERIES), []);
  const daily = useMemo(
    () => (stats?.daily ?? []).map((day) => ({ ...day, label: day.date.slice(5).replace("-", "/") })),
    [stats]
  );
  const chartEmpty = daily.every((day) => day.invited === 0 && day.submitted === 0 && day.approved === 0);

  if (!campaignsQuery.isLoading && campaigns.length === 0) {
    return (
      <Section title="活动统计" icon={BarChart3} tone="primary">
        <StateBlock icon={Megaphone} title="暂无活动" description="新建并上线一期活动后，这里显示它的参与与审核数据" />
      </Section>
    );
  }

  const submissions = stats?.submissions;
  const tierClaimsTotal = (stats?.tierClaims ?? []).reduce((sum, tier) => sum + tier.claimed, 0);
  const tierTitles = new Map((campaign?.config?.invite?.tiers ?? []).map((tier) => [tier.count, tier.title]));
  const maxClaimed = Math.max(1, ...(stats?.tierClaims ?? []).map((tier) => tier.claimed));
  const inviteEnabled = Boolean(campaign?.config?.invite?.enabled);
  const shareEnabled = Boolean(campaign?.config?.share?.enabled);

  const platformColumns: Array<Column<PlatformRow>> = [
    {
      id: "platform",
      header: "平台",
      cell: (row) => <span className="font-medium">{row.name || platformName(row.platform, catalogQuery.data)}</span>
    },
    {
      id: "pending",
      header: "待审核",
      align: "right",
      cell: (row) => (
        <span className={row.pending ? "font-medium text-amber-600 tabular-nums dark:text-amber-400" : "tabular-nums"}>
          {formatCount(row.pending)}
        </span>
      )
    },
    { id: "approved", header: "已通过", align: "right", cell: (row) => <span className="tabular-nums">{formatCount(row.approved)}</span> },
    { id: "rejected", header: "已驳回", align: "right", cell: (row) => <span className="tabular-nums">{formatCount(row.rejected)}</span> },
    {
      id: "rate",
      header: "通过率",
      align: "right",
      cell: (row) => <span className="tabular-nums text-muted-foreground">{percent(row.approved, row.approved + row.rejected)}</span>
    }
  ];

  const rankColumns: Array<Column<PromoRankEntry>> = [
    {
      id: "rank",
      header: "名次",
      width: "64px",
      cell: (row) => <span className="font-semibold tabular-nums">{row.rank}</span>
    },
    {
      id: "user",
      header: "用户",
      cell: (row) => {
        const name = row.nickname || row.account || (row.userId ? `#${row.userId}` : "—");
        const body = (
          <>
            <Avatar className="size-7">
              {row.avatar ? <AvatarImage src={row.avatar} alt="" /> : null}
              <AvatarFallback className="text-xs">{userInitials(row.nickname, row.account)}</AvatarFallback>
            </Avatar>
            <span className="flex min-w-0 flex-col">
              <span className="truncate text-sm font-medium group-hover:underline">{name}</span>
              <span className="truncate text-xs text-muted-foreground">
                {row.account && row.nickname ? row.account : row.userId ? `ID ${row.userId}` : ""}
              </span>
            </span>
          </>
        );
        return row.userId ? (
          <Link href={`/app-users/${encodeURIComponent(appKey)}/${row.userId}`} className="group inline-flex min-w-0 items-center gap-2">
            {body}
          </Link>
        ) : (
          <span className="inline-flex min-w-0 items-center gap-2">{body}</span>
        );
      }
    },
    {
      id: "count",
      header: "有效邀请",
      align: "right",
      cell: (row) => <span className="font-medium tabular-nums">{formatCount(row.count)}</span>
    },
    {
      id: "reached",
      header: "达到时间",
      align: "right",
      hideBelow: "md",
      cell: (row) => <span className="whitespace-nowrap tabular-nums text-muted-foreground">{formatDateTime(row.reachedAt)}</span>
    }
  ];

  return (
    <div className="flex flex-col gap-4">
      <Card className="flex flex-col gap-3 px-4 py-3.5 sm:flex-row sm:items-center sm:justify-between sm:px-5">
        <div className="flex min-w-0 flex-col gap-1">
          {campaign ? (
            <>
              <div className="flex flex-wrap items-center gap-2">
                <span className="truncate text-sm font-semibold">{campaign.title}</span>
                <StatusBadge status={campaign.status} />
                <PhaseBadge phase={listed?.phase ?? campaignPhase(campaign)} />
              </div>
              <span className="text-xs tabular-nums text-muted-foreground">{periodLabel(campaign.startsAt, campaign.endsAt)}</span>
            </>
          ) : (
            <Skeleton className="h-9 w-56" />
          )}
        </div>
        <FilterSelect
          label="活动"
          allLabel="选择活动"
          value={selectedId ? String(selectedId) : ""}
          onChange={(next) => onCampaignIdChange(next ? Number(next) : undefined)}
          options={campaigns.map((item) => ({ value: String(item.id), label: item.title }))}
          className="max-w-[260px]"
        />
      </Card>

      {statsQuery.error ? (
        <Section title="活动统计" icon={BarChart3} tone="primary">
          <StateBlock kind="error" description={errorMessage(statsQuery.error, "加载失败")} onRetry={() => statsQuery.refetch()} />
        </Section>
      ) : (
        <>
          <MetricGrid>
            <MetricCard label="参与人数" icon={Users} tone="primary" loading={loading} value={formatCount(stats?.participants)} sub="领过档位或提交过文章" />
            <MetricCard
              label="有效邀请"
              icon={UserPlus}
              tone="violet"
              loading={loading}
              value={formatCount(stats?.invited)}
              sub={`等待中 ${formatCount(stats?.pendingInvites)}`}
              spark={daily.map((day) => day.invited)}
            />
            <MetricCard label="档位领取" icon={Gift} tone="success" loading={loading} value={formatCount(tierClaimsTotal)} sub="各档合计次数" />
            <MetricCard
              label="待审核"
              icon={Hourglass}
              tone={submissions?.pending ? "warning" : "neutral"}
              loading={loading}
              value={formatCount(submissions?.pending)}
              sub={submissions?.pending ? "点击去审核" : "没有积压"}
              onClick={submissions?.pending && selectedId ? () => onOpenSubmissions(selectedId, "pending") : undefined}
            />
            <MetricCard
              label="审核通过"
              icon={BadgeCheck}
              tone="success"
              loading={loading}
              value={formatCount(submissions?.approved)}
              sub={`通过率 ${percent(submissions?.approved ?? 0, (submissions?.approved ?? 0) + (submissions?.rejected ?? 0))}`}
              spark={daily.map((day) => day.approved)}
            />
            <MetricCard
              label="已驳回"
              icon={CircleX}
              tone="neutral"
              loading={loading}
              value={formatCount(submissions?.rejected)}
              onClick={submissions?.rejected && selectedId ? () => onOpenSubmissions(selectedId, "rejected") : undefined}
            />
            <MetricCard
              label="提交文章"
              icon={Megaphone}
              tone="info"
              loading={loading}
              value={formatCount((submissions?.pending ?? 0) + (submissions?.approved ?? 0) + (submissions?.rejected ?? 0))}
              sub="全部状态合计"
              spark={daily.map((day) => day.submitted)}
            />
            <MetricCard
              label="排行人数"
              icon={Trophy}
              tone="warning"
              loading={leaderboardQuery.isLoading}
              value={formatCount(leaderboardQuery.data?.length)}
              sub={campaign?.config?.invite?.leaderboard ? `用户端展示前 ${campaign.config.invite.leaderboardSize} 名` : "用户端不展示排行"}
            />
          </MetricGrid>

          <ChartCard title="按日走势" description="有效邀请、提交文章与审核通过" loading={loading} empty={chartEmpty} height={240}>
            <ChartContainer config={chartConfig} className="h-[240px] w-full">
              <BarChart data={daily} margin={{ left: 0, right: 8, top: 8 }} barGap={2}>
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
            <Section title="档位领取" icon={Layers} tone="violet" description={inviteEnabled ? "每档的领取人数" : "本期没有邀请任务"}>
              {loading ? (
                <div className="flex flex-col gap-2">
                  {[0, 1, 2].map((index) => (
                    <Skeleton key={index} className="h-8 w-full" />
                  ))}
                </div>
              ) : !stats?.tierClaims?.length ? (
                <StateBlock compact icon={Layers} title="暂无档位" />
              ) : (
                <ul className="flex flex-col gap-3">
                  {stats.tierClaims.map((tier) => (
                    <li key={tier.count} className="flex flex-col gap-1.5">
                      <div className="flex items-baseline justify-between gap-3 text-sm">
                        <span className="min-w-0 truncate">
                          邀请满 {formatCount(tier.count)} 人
                          {tierTitles.get(tier.count) ? (
                            <span className="ml-1.5 text-xs text-muted-foreground">{tierTitles.get(tier.count)}</span>
                          ) : null}
                        </span>
                        <span className="shrink-0 font-medium tabular-nums">{formatCount(tier.claimed)} 人领取</span>
                      </div>
                      <div className="h-1.5 overflow-hidden rounded-full bg-muted">
                        <div className="h-full rounded-full bg-violet-500" style={{ width: `${(tier.claimed / maxClaimed) * 100}%` }} />
                      </div>
                    </li>
                  ))}
                </ul>
              )}
            </Section>

            <Section flush title="分平台审核" icon={Megaphone} tone="info" description={shareEnabled ? "各平台的提交与审核结论" : "本期没有分享任务"}>
              <RecordTable
                columns={platformColumns}
                rows={stats?.byPlatform ?? []}
                rowKey={(row) => row.platform}
                loading={loading}
                minWidth={420}
                skeletonRows={3}
                empty={<StateBlock compact icon={Megaphone} title="暂无提交" />}
              />
            </Section>
          </div>
        </>
      )}

      <Section
        flush
        title="邀请排行"
        icon={Trophy}
        tone="warning"
        description="按有效邀请人数降序，同人数先达到的在前；管理端显示完整账号"
      >
        <RecordTable
          columns={rankColumns}
          rows={leaderboardQuery.data ?? []}
          rowKey={(row) => `${row.rank}-${row.userId ?? row.nickname}`}
          loading={leaderboardQuery.isLoading}
          error={leaderboardQuery.error}
          onRetry={() => leaderboardQuery.refetch()}
          minWidth={520}
          empty={<StateBlock compact icon={Trophy} title={inviteEnabled ? "暂无有效邀请" : "本期没有邀请任务"} />}
        />
      </Section>
    </div>
  );
}
