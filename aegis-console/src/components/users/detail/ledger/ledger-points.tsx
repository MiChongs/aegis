"use client";

/**
 * 积分 / 经验流水。
 *
 * 自上而下：四张指标卡（当前、累计入账、累计出账、管理员净调整）→ 走势图（每日入出账柱 + 余额线）
 * → 来源构成（点一项即按该分类筛选流水）→ 流水表（关键词、方向、分类、时间区间，导出 CSV，
 * 行点开看完整字段；积分与经验流水都可在抽屉里冲正）。
 *
 * 冲正走后端专用接口：请求编号由原流水号派生，同一笔重复点只生效一次；冲正产生的流水不能再冲正。
 * 经验扣回时要选等级处理（保留现有等级 / 按经验重新判定）。
 */

import * as React from "react";
import {
  ArrowDownLeft,
  ArrowUpRight,
  ChartColumn,
  Coins,
  Download,
  ListTree,
  Loader2,
  PieChart,
  Receipt,
  SlidersHorizontal,
  Sparkles,
  Undo2
} from "lucide-react";
import { Bar, CartesianGrid, ComposedChart, Line, XAxis, YAxis } from "recharts";
import { toast } from "sonner";
import {
  AlertDialog,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle
} from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ChartContainer, ChartTooltip, ChartTooltipContent, type ChartConfig } from "@/components/ui/chart";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { useAdminToken } from "@/lib/admin-hooks";
import { ApiError } from "@/lib/api-client";
import { getUserPointLedger, type PointKind, type PointLedgerCategory, type PointTransaction } from "@/lib/api/user-ledger";
import { formatDateTime, formatRelative } from "@/lib/datetime";
import { useReverseUserPointsMutation, useUserPointLedgerQuery, useUserPointSummaryQuery } from "@/lib/user-ledger-hooks";
import type { ExperienceLevelMode } from "@/lib/api/apps";
import { cn } from "@/lib/utils";
import {
  FilterBar,
  FilterSelect,
  MetricCard,
  Pager,
  RecordSheet,
  RecordTable,
  ResetButton,
  SearchField,
  Section,
  SignedAmount,
  StateBlock,
  TimeRangePicker,
  collectPages,
  downloadCsv,
  errorMessage,
  formatInt,
  rangeToParams,
  useDebouncedValue,
  type Column,
  type InfoItem,
  type TimeRange
} from "../kit";
import { RANGE_OPTIONS, categoryLabel, fillDays } from "./ledger-shared";

const KIND_META: Record<PointKind, { label: string; unit: string; icon: typeof Coins }> = {
  integral: { label: "积分", unit: "积分", icon: Coins },
  experience: { label: "经验", unit: "经验", icon: Sparkles }
};

const DIRECTION_OPTIONS = [
  { value: "in", label: "入账" },
  { value: "out", label: "出账" }
];

const chartConfig = {
  in: { label: "入账", theme: { light: "#10b981", dark: "#34d399" } },
  out: { label: "出账", theme: { light: "#ef4444", dark: "#f87171" } },
  balance: { label: "余额", theme: { light: "#6366f1", dark: "#a5b4fc" } }
} satisfies ChartConfig;

export function PointLedgerView({
  appKey,
  userId,
  kind,
  onAdjust
}: {
  appKey: string;
  userId: number;
  kind: PointKind;
  /** 去资产页调整 */
  onAdjust?: () => void;
}) {
  const meta = KIND_META[kind];
  const token = useAdminToken();

  const [days, setDays] = React.useState(30);
  const summaryQuery = useUserPointSummaryQuery(appKey, userId, kind, days);
  const summary = summaryQuery.data;

  const [keyword, setKeywordRaw] = React.useState("");
  const [direction, setDirectionRaw] = React.useState("");
  const [category, setCategoryRaw] = React.useState("");
  const [range, setRangeRaw] = React.useState<TimeRange>({ preset: "all" });
  const [page, setPage] = React.useState(1);
  const [limit, setLimitRaw] = React.useState(20);
  // 筛选一变回到第一页
  function resetting<T>(set: React.Dispatch<React.SetStateAction<T>>) {
    return (value: React.SetStateAction<T>) => {
      set(value);
      setPage(1);
    };
  }
  const setKeyword = resetting(setKeywordRaw);
  const setDirection = resetting(setDirectionRaw);
  const setCategory = resetting(setCategoryRaw);
  const setRange = resetting(setRangeRaw);
  const setLimit = resetting(setLimitRaw);
  const debouncedKeyword = useDebouncedValue(keyword.trim());

  const params = React.useMemo(
    () => ({
      keyword: debouncedKeyword || undefined,
      direction: (direction || undefined) as "in" | "out" | undefined,
      category: category || undefined,
      ...rangeToParams(range),
      page,
      limit
    }),
    [debouncedKeyword, direction, category, range, page, limit]
  );

  const ledgerQuery = useUserPointLedgerQuery(appKey, userId, kind, params);
  const ledger = ledgerQuery.data;
  const filtered = Boolean(debouncedKeyword || direction || category || range.preset !== "all");

  const [selected, setSelected] = React.useState<PointTransaction | null>(null);
  const [exporting, setExporting] = React.useState(false);

  const categories = summary?.categories ?? [];
  const categoryOptions = categories.map((item) => ({ value: item.category, label: categoryLabel(item.category, item.title) }));
  const titleOf = (txn: PointTransaction) => categoryLabel(txn.category, txn.title);

  const filledDays = React.useMemo(
    () => (summary ? fillDays(summary.daily, summary.days, summary.balanceBefore) : []),
    [summary]
  );

  async function exportCsv() {
    if (!token) return;
    setExporting(true);
    try {
      const { rows, total, truncated } = await collectPages((p, l) =>
        getUserPointLedger(token, appKey, userId, kind, { ...params, page: p, limit: l })
      );
      downloadCsv(`${meta.label}流水-${userId}`, [
        { header: "时间", value: (r: PointTransaction) => formatDateTime(r.createdAt) },
        { header: "流水号", value: (r) => r.transactionNo },
        { header: "分类", value: (r) => titleOf(r) },
        { header: "标题", value: (r) => r.title },
        { header: "说明", value: (r) => r.description },
        { header: "变动", value: (r) => r.amount },
        { header: "变动前", value: (r) => r.balanceBefore },
        { header: "变动后", value: (r) => r.balanceAfter },
        ...(kind === "experience"
          ? [
              { header: "等级前", value: (r: PointTransaction) => r.levelBefore },
              { header: "等级后", value: (r: PointTransaction) => r.levelAfter }
            ]
          : []),
        { header: "操作人", value: (r) => String(r.extraData?.adminAccount ?? "") },
        { header: "IP", value: (r) => r.clientIp }
      ], rows);
      toast.success(truncated ? `已导出前 ${rows.length} 条（共 ${total} 条）` : `已导出 ${rows.length} 条`);
    } catch (error) {
      toast.error(errorMessage(error, "导出失败"));
    } finally {
      setExporting(false);
    }
  }

  const columns: Array<Column<PointTransaction>> = [
    {
      id: "time",
      header: "时间",
      width: "150px",
      cell: (row) => (
        <div className="flex flex-col">
          <span className="text-sm tabular-nums">{formatDateTime(row.createdAt)}</span>
          <span className="text-xs text-muted-foreground">{formatRelative(row.createdAt)}</span>
        </div>
      )
    },
    {
      id: "title",
      header: "事项",
      cell: (row) => (
        <div className="flex min-w-0 flex-col gap-0.5">
          <div className="flex min-w-0 items-center gap-1.5">
            <span className="truncate font-medium">{row.title || titleOf(row)}</span>
            {row.extraData?.reversalOf ? (
              <Badge variant="info" size="sm">冲正</Badge>
            ) : null}
            {row.isLevelUp ? (
              <Badge variant="success" size="sm">升级</Badge>
            ) : null}
          </div>
          <span className="truncate text-xs text-muted-foreground">
            {titleOf(row)}
            {row.description && row.description !== row.title ? `，${row.description}` : ""}
          </span>
        </div>
      )
    },
    {
      id: "amount",
      header: "变动",
      align: "right",
      width: "110px",
      cell: (row) => <SignedAmount value={row.amount} />
    },
    {
      id: "balance",
      header: "变动后",
      align: "right",
      width: "130px",
      hideBelow: "sm",
      cell: (row) => (
        <div className="flex flex-col items-end">
          <span className="tabular-nums">{formatInt(row.balanceAfter)}</span>
          <span className="text-xs text-muted-foreground tabular-nums">原 {formatInt(row.balanceBefore)}</span>
        </div>
      )
    },
    ...(kind === "experience"
      ? [
          {
            id: "level",
            header: "等级",
            align: "center" as const,
            width: "90px",
            hideBelow: "md" as const,
            cell: (row: PointTransaction) =>
              row.levelBefore && row.levelAfter && row.levelBefore !== row.levelAfter ? (
                <span className="tabular-nums">
                  Lv{row.levelBefore} → Lv{row.levelAfter}
                </span>
              ) : (
                <span className="text-muted-foreground tabular-nums">{row.levelAfter ? `Lv${row.levelAfter}` : "—"}</span>
              )
          }
        ]
      : []),
    {
      id: "operator",
      header: "来源",
      width: "140px",
      hideBelow: "lg",
      cell: (row) => {
        const admin = typeof row.extraData?.adminAccount === "string" ? row.extraData.adminAccount : "";
        return (
          <span className="truncate text-xs text-muted-foreground">
            {admin ? `管理员 ${admin}` : row.sourceType || "系统"}
          </span>
        );
      }
    },
    {
      id: "no",
      header: "流水号",
      width: "170px",
      hideBelow: "xl",
      cell: (row) => <span className="font-mono text-xs text-muted-foreground">{row.transactionNo}</span>
    }
  ];

  return (
    <div className="flex flex-col gap-5">
      <div className="grid grid-cols-2 gap-3 sm:gap-4 xl:grid-cols-4">
        <MetricCard
          label={`当前${meta.label}`}
          value={formatInt(summary?.balance)}
          icon={meta.icon}
          tone="primary"
          loading={summaryQuery.isLoading}
          sub={summary?.lastAt ? `最近变动 ${formatRelative(summary.lastAt)}` : "暂无变动"}
          spark={filledDays.length > 1 ? filledDays.map((d) => d.balance) : undefined}
        />
        <MetricCard
          label="累计入账"
          value={formatInt(summary?.totalIn)}
          icon={ArrowDownLeft}
          tone="success"
          loading={summaryQuery.isLoading}
          sub={`共 ${formatInt(summary?.count)} 笔流水`}
          onClick={() => setDirection("in")}
        />
        <MetricCard
          label="累计出账"
          value={formatInt(summary?.totalOut)}
          icon={ArrowUpRight}
          tone="danger"
          loading={summaryQuery.isLoading}
          sub={summary?.firstAt ? `首笔 ${formatDateTime(summary.firstAt)}` : "—"}
          onClick={() => setDirection("out")}
        />
        <MetricCard
          label="管理员净调整"
          value={summary ? <SignedAmount value={summary.adminIn - summary.adminOut} className="font-semibold" /> : "—"}
          icon={SlidersHorizontal}
          tone="warning"
          loading={summaryQuery.isLoading}
          sub={summary ? `加 ${formatInt(summary.adminIn)}，减 ${formatInt(summary.adminOut)}` : undefined}
          onClick={() => setCategory("admin_adjust")}
        />
      </div>

      <div className="grid gap-5 xl:grid-cols-[minmax(0,1.6fr)_minmax(0,1fr)]">
        <Section
          title="每日走势"
          description={summary ? `按浏览器时区（${summary.timeZone}）切日` : undefined}
          icon={ChartColumn}
          tone="primary"
          actions={
            <ToggleGroup
              type="single"
              size="sm"
              variant="outline"
              value={String(days)}
              onValueChange={(value) => value && setDays(Number(value))}
            >
              {RANGE_OPTIONS.map((option) => (
                <ToggleGroupItem key={option.value} value={option.value} className="px-2.5 text-xs">
                  {option.label}
                </ToggleGroupItem>
              ))}
            </ToggleGroup>
          }
        >
          {summaryQuery.isLoading ? (
            <Skeleton className="h-64 w-full" />
          ) : summaryQuery.isError ? (
            <StateBlock kind="error" description={errorMessage(summaryQuery.error)} onRetry={() => summaryQuery.refetch()} compact />
          ) : (
            <ChartContainer config={chartConfig} className="aspect-auto h-64 w-full">
              <ComposedChart data={filledDays} margin={{ left: 4, right: 4, top: 8 }}>
                <CartesianGrid vertical={false} />
                <XAxis dataKey="label" tickLine={false} axisLine={false} minTickGap={24} tickMargin={8} />
                <YAxis yAxisId="flow" tickLine={false} axisLine={false} width={44} allowDecimals={false} />
                <YAxis yAxisId="balance" orientation="right" tickLine={false} axisLine={false} width={52} allowDecimals={false} />
                <ChartTooltip content={<ChartTooltipContent labelKey="date" indicator="dot" />} />
                <Bar yAxisId="flow" dataKey="in" fill="var(--color-in)" radius={[3, 3, 0, 0]} maxBarSize={18} />
                <Bar yAxisId="flow" dataKey="out" fill="var(--color-out)" radius={[3, 3, 0, 0]} maxBarSize={18} />
                <Line yAxisId="balance" type="monotone" dataKey="balance" stroke="var(--color-balance)" strokeWidth={2} dot={false} />
              </ComposedChart>
            </ChartContainer>
          )}
        </Section>

        <CategoryBreakdown
          loading={summaryQuery.isLoading}
          categories={categories}
          active={category}
          onPick={(value) => setCategory((current) => (current === value ? "" : value))}
        />
      </div>

      <Section
        title={`${meta.label}流水`}
        description={ledger ? `当前${meta.label} ${formatInt(ledger.balance)}` : undefined}
        icon={Receipt}
        flush
        actions={
          <>
            {onAdjust ? (
              <Button size="sm" variant="outline" onClick={onAdjust}>
                <SlidersHorizontal />
                调整{meta.label}
              </Button>
            ) : null}
            <Button size="sm" variant="outline" disabled={exporting || !ledger?.total} onClick={exportCsv}>
              {exporting ? <Loader2 className="animate-spin" /> : <Download />}
              导出 CSV
            </Button>
          </>
        }
        toolbar={
          <FilterBar>
            <SearchField value={keyword} onChange={setKeyword} placeholder="流水号、标题、原因、操作人" />
            <FilterSelect label="方向" value={direction} onChange={setDirection} options={DIRECTION_OPTIONS} />
            <FilterSelect label="分类" value={category} onChange={setCategory} options={categoryOptions} />
            <TimeRangePicker value={range} onChange={setRange} />
            <ResetButton
              visible={filtered}
              onClick={() => {
                setKeyword("");
                setDirection("");
                setCategory("");
                setRange({ preset: "all" });
              }}
            />
          </FilterBar>
        }
        footer={
          ledger ? (
            <Pager
              page={page}
              limit={limit}
              total={ledger.total}
              onPage={setPage}
              onLimit={setLimit}
              summary={
                <span className="tabular-nums">
                  {filtered ? "筛选结果" : "全部"}入账 <span className="text-emerald-600 dark:text-emerald-400">+{formatInt(ledger.sumIn)}</span>
                  ，出账 <span className="text-red-600 dark:text-red-400">-{formatInt(ledger.sumOut)}</span>
                </span>
              }
            />
          ) : null
        }
      >
        <RecordTable
          columns={columns}
          rows={ledger?.items}
          rowKey={(row) => row.id}
          loading={ledgerQuery.isLoading}
          fetching={ledgerQuery.isFetching}
          error={ledgerQuery.error}
          onRetry={() => ledgerQuery.refetch()}
          onRowClick={setSelected}
          empty={
            <StateBlock
              icon={ListTree}
              title={filtered ? "没有符合条件的流水" : `还没有${meta.label}流水`}
              description={filtered ? "换个筛选条件试试。" : undefined}
            />
          }
        />
      </Section>

      <PointTransactionSheet
        appKey={appKey}
        userId={userId}
        kind={kind}
        transaction={selected}
        titleOf={titleOf}
        onClose={() => setSelected(null)}
      />
    </div>
  );
}

function CategoryBreakdown({
  loading,
  categories,
  active,
  onPick
}: {
  loading: boolean;
  categories: PointLedgerCategory[];
  active: string;
  onPick: (category: string) => void;
}) {
  const max = Math.max(1, ...categories.map((item) => item.in + item.out));
  return (
    <Section title="来源构成" description="点一项按该分类筛选流水" icon={PieChart} tone="violet" flush>
      {loading ? (
        <div className="flex flex-col gap-3 px-4 py-4 sm:px-5">
          {[0, 1, 2, 3].map((i) => (
            <Skeleton key={i} className="h-9 w-full" />
          ))}
        </div>
      ) : !categories.length ? (
        <StateBlock title="暂无流水" compact />
      ) : (
        <ul className="flex max-h-[296px] flex-col overflow-y-auto py-1.5">
          {categories.map((item) => {
            const share = ((item.in + item.out) / max) * 100;
            return (
              <li key={item.category}>
                <button
                  type="button"
                  onClick={() => onPick(item.category)}
                  className={cn(
                    "flex w-full flex-col gap-1.5 px-4 py-2 text-left transition-colors hover:bg-muted/40 sm:px-5",
                    active === item.category && "bg-primary/5"
                  )}
                >
                  <div className="flex items-center justify-between gap-3 text-sm">
                    <span className="flex min-w-0 items-center gap-1.5">
                      <span className="truncate font-medium">{categoryLabel(item.category, item.title)}</span>
                      <span className="text-xs text-muted-foreground tabular-nums">{item.count} 笔</span>
                    </span>
                    <span className="flex shrink-0 items-center gap-2 text-xs tabular-nums">
                      {item.in ? <span className="text-emerald-600 dark:text-emerald-400">+{formatInt(item.in)}</span> : null}
                      {item.out ? <span className="text-red-600 dark:text-red-400">-{formatInt(item.out)}</span> : null}
                    </span>
                  </div>
                  <div className="flex h-1.5 w-full overflow-hidden rounded-full bg-muted">
                    <div
                      className="h-full bg-emerald-500 dark:bg-emerald-400"
                      style={{ width: `${(item.in / max) * 100}%` }}
                    />
                    <div className="h-full bg-red-500 dark:bg-red-400" style={{ width: `${(item.out / max) * 100}%` }} />
                    <span className="sr-only">{share.toFixed(0)}%</span>
                  </div>
                </button>
              </li>
            );
          })}
        </ul>
      )}
    </Section>
  );
}

function PointTransactionSheet({
  appKey,
  userId,
  kind,
  transaction,
  titleOf,
  onClose
}: {
  appKey: string;
  userId: number;
  kind: PointKind;
  transaction: PointTransaction | null;
  titleOf: (txn: PointTransaction) => string;
  onClose: () => void;
}) {
  const [confirming, setConfirming] = React.useState(false);
  const [reason, setReason] = React.useState("");
  const reverse = useReverseUserPointsMutation(appKey, userId, kind);
  const [levelMode, setLevelMode] = React.useState<ExperienceLevelMode>("keep");
  const unit = kind === "experience" ? "经验" : "积分";

  // 保留最后一笔：关闭动画期间内容不要突然变空
  const [shown, setShown] = React.useState<PointTransaction | null>(transaction);
  if (transaction && transaction !== shown) setShown(transaction);
  const txn = transaction ?? shown;

  const extra = txn?.extraData ?? {};
  const reversalOf = typeof extra.reversalOf === "string" ? extra.reversalOf : "";
  const canReverse = Boolean(txn) && !reversalOf && txn?.amount !== 0;
  // 经验：冲正一笔「获得」是扣回，要选等级处理；冲正一笔「扣减」是还回，按经验正常升级
  const reverseDeducts = kind === "experience" && (txn?.amount ?? 0) > 0;

  const fields: InfoItem[] = txn
    ? [
        { label: "流水号", value: txn.transactionNo, mono: true, copy: txn.transactionNo, wide: true },
        { label: "分类", value: titleOf(txn), hint: txn.category },
        { label: "状态", value: txn.status === "completed" ? "已完成" : txn.status },
        { label: "变动前", value: formatInt(txn.balanceBefore) },
        { label: "变动后", value: formatInt(txn.balanceAfter) },
        ...(kind === "experience"
          ? [
              { label: "等级变化", value: txn.levelBefore ? `Lv${txn.levelBefore} → Lv${txn.levelAfter}` : undefined },
              { label: "倍率", value: txn.multiplier ? `×${txn.multiplier}` : undefined }
            ]
          : [{ label: "倍率", value: txn.multiplier && txn.multiplier !== 1 ? `×${txn.multiplier}` : "×1" }]),
        { label: "来源类型", value: txn.sourceType, mono: true },
        { label: "来源 ID", value: txn.sourceId ? String(txn.sourceId) : undefined, mono: true },
        { label: "操作人", value: typeof extra.adminAccount === "string" ? extra.adminAccount : undefined },
        { label: "原因", value: typeof extra.reason === "string" ? extra.reason : txn.description, wide: true },
        { label: "冲正自", value: reversalOf || undefined, mono: true, copy: reversalOf || null, wide: true },
        { label: "IP", value: txn.clientIp, mono: true },
        { label: "时间", value: formatDateTime(txn.createdAt), hint: formatRelative(txn.createdAt) },
        { label: "客户端", value: txn.userAgent, wide: true }
      ]
    : [];

  async function submitReverse() {
    if (!txn) return;
    try {
      const result = await reverse.mutateAsync({
        transactionNo: txn.transactionNo,
        reason: reason.trim() || undefined,
        levelMode: reverseDeducts ? levelMode : undefined
      });
      toast.success(
        result.replayed
          ? "这笔已冲正过，未重复执行"
          : `已冲正，${unit} ${formatInt(result.beforeAmount)} → ${formatInt(result.afterAmount)}`,
        {
          description:
            result.levelChanged && typeof result.newLevel === "number" ? `等级 Lv.${result.oldLevel ?? "—"} → Lv.${result.newLevel}` : undefined
        }
      );
      setConfirming(false);
      setReason("");
      onClose();
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "冲正失败");
    }
  }

  return (
    <>
      <RecordSheet
        open={Boolean(transaction)}
        onOpenChange={(open) => !open && onClose()}
        title={txn?.title || "流水详情"}
        description={txn ? titleOf(txn) : undefined}
        hero={
          txn ? (
            <div className="flex items-end justify-between gap-4 rounded-xl border bg-muted/30 px-4 py-3.5">
              <div className="flex flex-col gap-1">
                <span className="text-xs text-muted-foreground">变动</span>
                <SignedAmount value={txn.amount} className="text-2xl font-semibold" />
              </div>
              <div className="flex flex-col items-end gap-1 text-xs text-muted-foreground">
                {reversalOf ? <Badge variant="info" size="sm">冲正流水</Badge> : null}
                {txn.isLevelUp ? <Badge variant="success" size="sm">本笔升级</Badge> : null}
                <span className="tabular-nums">
                  {formatInt(txn.balanceBefore)} → {formatInt(txn.balanceAfter)}
                </span>
              </div>
            </div>
          ) : null
        }
        fields={fields}
        raw={txn?.extraData && Object.keys(txn.extraData).length ? txn.extraData : undefined}
        actions={
          canReverse ? (
            <Button variant="outline" onClick={() => setConfirming(true)}>
              <Undo2 />
              冲正这笔
            </Button>
          ) : undefined
        }
      />

      <AlertDialog open={confirming} onOpenChange={setConfirming}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>冲正这笔{unit}流水？</AlertDialogTitle>
            <AlertDialogDescription>
              将按原数量反向调整 {txn ? <SignedAmount value={-txn.amount} /> : null} {unit}，并生成一笔新的流水。同一笔只能冲正一次；
              扣回时若用户{unit}不足会失败。
            </AlertDialogDescription>
          </AlertDialogHeader>
          {reverseDeducts ? (
            <div className="grid gap-2 sm:grid-cols-2" role="radiogroup" aria-label="扣回经验后的等级处理">
              {(
                [
                  { value: "keep", title: "保留现有等级", hint: "只扣回经验" },
                  { value: "recalc", title: "按经验重新判定", hint: "可能降级，不低于锁定的保底" }
                ] as const
              ).map((item) => (
                <button
                  key={item.value}
                  type="button"
                  role="radio"
                  aria-checked={levelMode === item.value}
                  onClick={() => setLevelMode(item.value)}
                  className={cn(
                    "flex flex-col items-start gap-0.5 rounded-lg border p-2.5 text-left transition-colors",
                    levelMode === item.value ? "border-primary bg-primary/5" : "hover:bg-accent/50"
                  )}
                >
                  <span className="text-sm font-medium">{item.title}</span>
                  <span className="text-xs text-muted-foreground">{item.hint}</span>
                </button>
              ))}
            </div>
          ) : null}
          <Input
            value={reason}
            maxLength={200}
            onChange={(event) => setReason(event.target.value)}
            placeholder={txn ? `原因（留空写「冲正 ${txn.transactionNo}」）` : "原因"}
          />
          <AlertDialogFooter>
            <AlertDialogCancel disabled={reverse.isPending}>取消</AlertDialogCancel>
            <Button onClick={submitReverse} disabled={reverse.isPending}>
              {reverse.isPending ? <Loader2 className="animate-spin" /> : <Undo2 />}
              确认冲正
            </Button>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}
