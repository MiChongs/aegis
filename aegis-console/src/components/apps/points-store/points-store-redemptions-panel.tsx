"use client";

import { useMemo, useState } from "react";
import Link from "next/link";
import { Ban, ExternalLink, Gift, PackageCheck, ReceiptText, RotateCw, X } from "lucide-react";
import { RedemptionHandleDialog, type HandleRequest } from "@/components/apps/points-store/points-store-handle-dialog";
import {
  RedemptionStatusBadge,
  formatPoints,
  redemptionStatusLabel
} from "@/components/apps/points-store/points-store-shared";
import {
  ExportButton,
  ResultChips,
  ResultList,
  exportStamp,
  resultsCsv,
  usePaging
} from "@/components/users/detail/records/records-shared";
import {
  FilterBar,
  FilterSelect,
  Pager,
  RecordSheet,
  RecordTable,
  ResetButton,
  SearchField,
  Section,
  StateBlock,
  TimeRangePicker,
  formatInt,
  rangeToParams,
  useDebouncedValue,
  type Column,
  type CsvColumn,
  type TimeRange
} from "@/components/users/detail/kit";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { useAdminToken } from "@/lib/admin-hooks";
import {
  listPointsStoreRedemptions,
  type PointsStoreRedemption,
  type PointsStoreRedemptionStatus
} from "@/lib/api/points-store";
import { formatDateTime, formatRelative } from "@/lib/datetime";
import {
  usePointsStoreItemsQuery,
  usePointsStoreRedemptionCountsQuery,
  usePointsStoreRedemptionsQuery
} from "@/lib/points-store-hooks";
import { cn } from "@/lib/utils";

const STATUS_TABS: Array<{ value: "" | PointsStoreRedemptionStatus; label: string; countKey: "all" | PointsStoreRedemptionStatus }> = [
  { value: "", label: "全部", countKey: "all" },
  { value: "pending", label: "待发放", countKey: "pending" },
  { value: "completed", label: "已到账", countKey: "completed" },
  { value: "cancelled", label: "已取消", countKey: "cancelled" },
  { value: "revoked", label: "已撤销", countKey: "revoked" }
];

/**
 * 兑换记录。
 *
 * 展示 `results`（实际发出去了什么），不是商品上配的权益 —— 两者的差别正是排障要看的。
 * 状态页签带当前筛选下的笔数；待发放的记录可以勾选后批量发放或取消（逐笔提交，各自成败），
 * 也可以点开详情单独处理。发放时写给用户的话（兑换码、快递单号）进站内信，取消即退积分、加回库存。
 * 时间区间按浏览器时区，导出 CSV 与当前筛选一致（最多 5000 条）。
 */
export function PointsStoreRedemptionsPanel({
  appKey,
  itemId,
  status,
  onItemIdChange,
  onStatusChange,
  onOpenGifts
}: {
  appKey: string;
  itemId?: number;
  status?: PointsStoreRedemptionStatus;
  onItemIdChange: (itemId?: number) => void;
  onStatusChange: (status?: PointsStoreRedemptionStatus) => void;
  /** 赠送的兑换在赠送记录里审核、撤销 */
  onOpenGifts?: () => void;
}) {
  const token = useAdminToken();
  const [keyword, setKeyword] = useState("");
  const [range, setRange] = useState<TimeRange>({ preset: "all" });
  const paging = usePaging(20);
  const debouncedKeyword = useDebouncedValue(keyword.trim());
  const [selectedIds, setSelectedIds] = useState<Set<number>>(new Set());
  const [detail, setDetail] = useState<PointsStoreRedemption | null>(null);
  const [request, setRequest] = useState<HandleRequest>(null);

  const filters = {
    itemId,
    keyword: debouncedKeyword || undefined,
    ...rangeToParams(range)
  };
  const itemsQuery = usePointsStoreItemsQuery(appKey);
  const countsQuery = usePointsStoreRedemptionCountsQuery(appKey, filters);
  const listQuery = usePointsStoreRedemptionsQuery(appKey, { ...filters, status, page: paging.page, limit: paging.limit });

  const rows = useMemo(() => listQuery.data?.items ?? [], [listQuery.data]);
  // 赠送的兑换不在这里发放：标记发放不会把东西发到接收人手上，审核在「赠送」页签
  const pendingRows = rows.filter((row) => row.status === "pending" && !row.giftNo);
  const selectedRows = pendingRows.filter((row) => selectedIds.has(row.id));
  const allPendingSelected = pendingRows.length > 0 && pendingRows.every((row) => selectedIds.has(row.id));
  const filtered = Boolean(keyword || itemId || range.preset !== "all");
  const pageCost = rows.filter((row) => row.status !== "cancelled").reduce((sum, row) => sum + row.cost, 0);

  const refilter = () => {
    paging.reset();
    setSelectedIds(new Set());
  };

  const toggle = (id: number, next: boolean) =>
    setSelectedIds((current) => {
      const copy = new Set(current);
      if (next) copy.add(id);
      else copy.delete(id);
      return copy;
    });

  const userHref = (userId: number) =>
    `/app-users/${encodeURIComponent(appKey)}/${userId}?tab=records&sub=points-store`;

  const columns: Array<Column<PointsStoreRedemption>> = [
    {
      id: "select",
      width: "36px",
      header: (
        <Checkbox
          checked={allPendingSelected ? true : selectedRows.length ? "indeterminate" : false}
          disabled={!pendingRows.length}
          onCheckedChange={(next) => setSelectedIds(next ? new Set(pendingRows.map((row) => row.id)) : new Set())}
          aria-label="选择本页待发放"
        />
      ),
      cell: (row) =>
        row.status === "pending" && !row.giftNo ? (
          <span onClick={(event) => event.stopPropagation()}>
            <Checkbox checked={selectedIds.has(row.id)} onCheckedChange={(next) => toggle(row.id, Boolean(next))} aria-label="选择" />
          </span>
        ) : null
    },
    {
      id: "item",
      header: "兑换",
      cell: (row) => (
        <div className="flex min-w-0 flex-col">
          <span className="truncate font-medium">
            {row.itemTitle}
            {row.itemId ? null : <span className="ml-1.5 text-xs font-normal text-muted-foreground">已删除</span>}
            {row.giftNo ? <span className="ml-1.5 text-xs font-normal text-violet-600 dark:text-violet-400">赠送</span> : null}
          </span>
          <span className="truncate font-mono text-xs text-muted-foreground">{row.redemptionNo}</span>
        </div>
      )
    },
    {
      id: "user",
      header: "用户",
      cell: (row) => (
        <Link href={userHref(row.userId)} onClick={(event) => event.stopPropagation()} className="group flex min-w-0 flex-col">
          <span className="truncate font-medium group-hover:underline">{row.nickname || row.account || `#${row.userId}`}</span>
          <span className="truncate text-xs text-muted-foreground">{row.nickname && row.account ? row.account : `ID ${row.userId}`}</span>
        </Link>
      )
    },
    {
      id: "cost",
      header: "积分",
      align: "right",
      cell: (row) => (
        <span className={cn("tabular-nums", row.status === "cancelled" ? "text-muted-foreground line-through" : "font-medium")}>
          {formatPoints(row.cost)}
        </span>
      )
    },
    { id: "results", header: "实际发放", hideBelow: "lg", cell: (row) => <ResultChips results={row.results} /> },
    {
      id: "status",
      header: "状态",
      cell: (row) => (
        <div className="flex flex-col items-start gap-0.5">
          <RedemptionStatusBadge status={row.status} />
          {row.handledBy ? <span className="text-xs text-muted-foreground">{row.handledBy}</span> : null}
        </div>
      )
    },
    {
      id: "time",
      header: "时间",
      align: "right",
      hideBelow: "md",
      cell: (row) => (
        <div className="flex flex-col items-end" title={formatDateTime(row.createdAt)}>
          <span className="whitespace-nowrap tabular-nums">{formatDateTime(row.createdAt)}</span>
          <span className="text-xs text-muted-foreground">{formatRelative(row.createdAt)}</span>
        </div>
      )
    },
    {
      id: "actions",
      header: "",
      align: "right",
      width: "120px",
      cell: (row) =>
        row.status === "pending" && row.giftNo ? (
          <div className="flex justify-end" onClick={(event) => event.stopPropagation()}>
            <Button size="xs" variant="outline" onClick={onOpenGifts}>
              去审核
            </Button>
          </div>
        ) : row.status === "pending" ? (
          <div className="flex justify-end gap-1.5" onClick={(event) => event.stopPropagation()}>
            <Button size="xs" variant="outline" onClick={() => setRequest({ action: "fulfill", items: [row] })}>
              发放
            </Button>
            <Button size="xs" variant="ghost" className="text-muted-foreground" onClick={() => setRequest({ action: "cancel", items: [row] })}>
              取消
            </Button>
          </div>
        ) : null
    }
  ];

  const csvColumns: Array<CsvColumn<PointsStoreRedemption>> = [
    { header: "兑换单号", value: (row) => row.redemptionNo },
    { header: "时间", value: (row) => formatDateTime(row.createdAt, "") },
    { header: "用户 ID", value: (row) => row.userId },
    { header: "账号", value: (row) => row.account },
    { header: "昵称", value: (row) => row.nickname },
    { header: "商品", value: (row) => row.itemTitle },
    { header: "商品 ID", value: (row) => row.itemId },
    { header: "消耗积分", value: (row) => row.cost },
    { header: "状态", value: (row) => redemptionStatusLabel(row.status) },
    { header: "实际发放", value: (row) => resultsCsv(row.results) },
    { header: "扣积分流水", value: (row) => row.integralTransactionNo },
    { header: "退积分流水", value: (row) => row.refundTransactionNo },
    { header: "备注", value: (row) => row.note },
    { header: "处理人", value: (row) => row.handledBy },
    { header: "处理时间", value: (row) => formatDateTime(row.handledAt, "") },
    { header: "设备", value: (row) => row.deviceId },
    { header: "IP", value: (row) => row.clientIp }
  ];

  return (
    <div className="flex flex-col gap-4">
      <div className="-mx-1 flex gap-2 overflow-x-auto px-1 pb-0.5">
        {STATUS_TABS.map((tab) => {
          const active = (status ?? "") === tab.value;
          const count = countsQuery.data?.[tab.countKey];
          return (
            <button
              key={tab.countKey}
              type="button"
              onClick={() => {
                onStatusChange(tab.value || undefined);
                refilter();
              }}
              className={cn(
                "flex min-w-[112px] shrink-0 flex-col gap-0.5 rounded-xl border bg-card px-4 py-2.5 text-left transition-colors",
                active ? "border-primary ring-1 ring-primary" : "hover:bg-muted/40"
              )}
            >
              <span className={cn("text-xs", active ? "text-primary" : "text-muted-foreground")}>{tab.label}</span>
              <span
                className={cn(
                  "text-lg font-semibold tabular-nums",
                  tab.countKey === "pending" && count ? "text-amber-600 dark:text-amber-400" : undefined
                )}
              >
                {count === undefined ? "—" : formatInt(count)}
              </span>
            </button>
          );
        })}
      </div>

      <Section
        flush
        title="兑换记录"
        icon={ReceiptText}
        tone="primary"
        description="点开一行看完整字段；待发放的可勾选后批量处理，用户都会收到站内信"
        actions={
          <>
            <Button
              variant="ghost"
              size="icon-sm"
              aria-label="刷新"
              onClick={() => {
                listQuery.refetch();
                countsQuery.refetch();
              }}
            >
              <RotateCw className={listQuery.isFetching ? "animate-spin" : undefined} />
            </Button>
            <ExportButton
              filename={`积分商城兑换-${appKey}-${exportStamp()}`}
              columns={csvColumns}
              disabled={!token || !listQuery.data?.total}
              fetchPage={(page, limit) => listPointsStoreRedemptions(token as string, appKey, { ...filters, status, page, limit })}
            />
          </>
        }
        toolbar={
          selectedRows.length ? (
            <div className="flex flex-wrap items-center gap-2">
              <span className="text-sm font-medium tabular-nums">已选 {selectedRows.length} 笔待发放</span>
              <span className="text-xs tabular-nums text-muted-foreground">
                共 {formatPoints(selectedRows.reduce((sum, row) => sum + row.cost, 0))} 积分
              </span>
              <div className="mx-1 h-4 w-px bg-border" />
              <Button size="sm" onClick={() => setRequest({ action: "fulfill", items: selectedRows })}>
                <PackageCheck />
                批量发放
              </Button>
              <Button size="sm" variant="outline" className="text-red-600 dark:text-red-400" onClick={() => setRequest({ action: "cancel", items: selectedRows })}>
                <Ban />
                批量取消
              </Button>
              <Button size="sm" variant="ghost" className="ml-auto text-muted-foreground" onClick={() => setSelectedIds(new Set())}>
                <X />
                取消选择
              </Button>
            </div>
          ) : (
            <FilterBar>
              <SearchField
                value={keyword}
                onChange={(next) => {
                  setKeyword(next);
                  refilter();
                }}
                placeholder="搜索账号、昵称、单号、商品"
              />
              <FilterSelect
                label="商品"
                allLabel="全部商品"
                value={itemId ? String(itemId) : ""}
                onChange={(next) => {
                  onItemIdChange(next ? Number(next) : undefined);
                  refilter();
                }}
                options={(itemsQuery.data ?? []).map((item) => ({ value: String(item.id), label: item.title }))}
                className="max-w-[240px]"
              />
              <TimeRangePicker
                value={range}
                onChange={(next) => {
                  setRange(next);
                  refilter();
                }}
              />
              <ResetButton
                visible={filtered}
                onClick={() => {
                  setKeyword("");
                  setRange({ preset: "all" });
                  onItemIdChange(undefined);
                  refilter();
                }}
              />
            </FilterBar>
          )
        }
        footer={
          listQuery.data?.total ? (
            <Pager
              page={paging.page}
              limit={paging.limit}
              total={listQuery.data.total}
              onPage={(page) => {
                paging.setPage(page);
                setSelectedIds(new Set());
              }}
              onLimit={(limit) => {
                paging.setLimit(limit);
                setSelectedIds(new Set());
              }}
              summary={<span className="tabular-nums">本页消耗 {formatInt(pageCost)} 积分（不含已取消）</span>}
            />
          ) : null
        }
      >
        <RecordTable
          columns={columns}
          rows={rows}
          rowKey={(row) => row.id}
          loading={listQuery.isLoading}
          fetching={listQuery.isFetching}
          error={listQuery.error}
          onRetry={() => listQuery.refetch()}
          onRowClick={setDetail}
          rowClassName={(row) => (selectedIds.has(row.id) ? "bg-primary/5" : undefined)}
          minWidth={820}
          empty={
            <StateBlock
              icon={ReceiptText}
              title={filtered || status ? "没有符合条件的兑换" : "暂无兑换记录"}
              description={status === "pending" && !filtered ? "人工发放的商品兑换后会出现在这里" : undefined}
            />
          }
        />
      </Section>

      <RecordSheet
        open={Boolean(detail)}
        onOpenChange={(open) => (!open ? setDetail(null) : undefined)}
        title="兑换详情"
        description={detail ? `${detail.redemptionNo}，${formatDateTime(detail.createdAt)}` : undefined}
        hero={
          detail ? (
            <div className="flex flex-col gap-3 rounded-xl border p-4">
              <div className="flex items-start justify-between gap-3">
                <div className="flex min-w-0 flex-col gap-1">
                  <span className="truncate text-base font-semibold">{detail.itemTitle}</span>
                  <RedemptionStatusBadge status={detail.status} />
                </div>
                <span className={cn("shrink-0 text-xl font-semibold tabular-nums", detail.status === "cancelled" && "text-muted-foreground line-through")}>
                  {formatPoints(detail.cost)}
                  <span className="ml-1 text-xs font-normal text-muted-foreground">积分</span>
                </span>
              </div>
              <div className="flex items-center justify-between gap-3 border-t pt-3 text-sm">
                <div className="flex min-w-0 flex-col">
                  <span className="truncate font-medium">{detail.nickname || detail.account || `#${detail.userId}`}</span>
                  <span className="truncate text-xs text-muted-foreground">
                    {detail.account ? `${detail.account}，` : ""}ID {detail.userId}
                  </span>
                </div>
                <Button asChild variant="outline" size="sm">
                  <Link href={userHref(detail.userId)}>
                    <ExternalLink />
                    用户详情
                  </Link>
                </Button>
              </div>
            </div>
          ) : null
        }
        fields={
          detail
            ? [
                { label: "兑换单号", value: detail.redemptionNo, mono: true, copy: detail.redemptionNo },
                { label: "商品 ID", value: detail.itemId ? String(detail.itemId) : "已删除" },
                { label: "扣积分流水", value: detail.integralTransactionNo, mono: true, copy: detail.integralTransactionNo || null },
                { label: "退积分流水", value: detail.refundTransactionNo, mono: true, copy: detail.refundTransactionNo || null },
                { label: "赠送单号", value: detail.giftNo, mono: true, copy: detail.giftNo || null },
                { label: "处理备注", value: detail.note, wide: true },
                { label: "处理人", value: detail.handledBy },
                { label: "处理时间", value: detail.handledAt ? formatDateTime(detail.handledAt) : undefined },
                { label: "请求 ID", value: detail.requestId, mono: true, copy: detail.requestId || null },
                { label: "设备", value: detail.deviceId, mono: true, copy: detail.deviceId || null },
                { label: "IP", value: detail.clientIp, mono: true, copy: detail.clientIp || null },
                { label: "兑换时间", value: formatDateTime(detail.createdAt) },
                { label: "更新时间", value: formatDateTime(detail.updatedAt) }
              ]
            : undefined
        }
        raw={detail}
        actions={
          detail?.giftNo ? (
            <Button variant="outline" size="sm" onClick={onOpenGifts}>
              <Gift />
              在赠送记录里处理
            </Button>
          ) : detail?.status === "pending" ? (
            <>
              <Button variant="outline" size="sm" onClick={() => setRequest({ action: "cancel", items: [detail] })}>
                <Ban />
                取消并退积分
              </Button>
              <Button size="sm" onClick={() => setRequest({ action: "fulfill", items: [detail] })}>
                <PackageCheck />
                发放
              </Button>
            </>
          ) : undefined
        }
      >
        {detail ? <ResultList results={detail.results} title="实际发放" /> : null}
      </RecordSheet>

      <RedemptionHandleDialog
        appKey={appKey}
        request={request}
        onClose={() => setRequest(null)}
        onDone={({ updated, failedIds }) => {
          const done = new Set(updated.map((row) => row.id));
          setSelectedIds((current) => new Set([...current].filter((id) => failedIds.includes(id) && !done.has(id))));
          if (detail) {
            const fresh = updated.find((row) => row.id === detail.id);
            if (fresh) setDetail(fresh);
          }
        }}
      />
    </div>
  );
}
