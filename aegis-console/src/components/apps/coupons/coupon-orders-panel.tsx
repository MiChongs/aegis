"use client";

import { useState } from "react";
import { ReceiptText, X } from "lucide-react";
import { NumberUserCell } from "@/components/apps/user-number/number-shared";
import { formatMoney } from "@/components/commerce/commerce-format";
import { UserPicker, type PickedUser } from "@/components/commerce/user-picker";
import {
  FilterBar,
  FilterSelect,
  Pager,
  RecordTable,
  ResetButton,
  SearchField,
  Section,
  StateBlock,
  useDebouncedValue,
  type Column
} from "@/components/users/detail/kit";
import type { OrderPromotion, OrderPromotionStatus } from "@/lib/api/coupons";
import { useOrderPromotionsQuery } from "@/lib/coupon-hooks";
import { formatDateTime } from "@/lib/datetime";
import { cn } from "@/lib/utils";
import { ORDER_STATUS_META, OrderStatusBadge, REBATE_STATUS_LABEL, SCOPE_LABEL } from "./coupon-shared";

const STATUS_OPTIONS = (Object.keys(ORDER_STATUS_META) as OrderPromotionStatus[]).map((status) => ({ value: status, label: ORDER_STATUS_META[status].label }));

export type OrderFilter = { kind: "promotion" | "template"; id: number; name: string } | null;

/**
 * 订单上的优惠：每一笔用了活动或券（或命中返券）的订单。
 * 待付款 = 券已占用、名额与预算已预占；已释放 = 没付款就关闭、都还回去了；已退款 = 全额退款后的冲正结果写在说明里。
 */
export function CouponOrdersPanel({
  appKey,
  filter,
  onFilterChange
}: {
  appKey: string;
  /** 由活动 / 券列表带过来的筛选 */
  filter: OrderFilter;
  onFilterChange: (filter: OrderFilter) => void;
}) {
  const [status, setStatus] = useState("");
  const [keyword, setKeyword] = useState("");
  const [user, setUser] = useState<PickedUser | null>(null);
  const debounced = useDebouncedValue(keyword.trim(), 300);
  const [page, setPage] = useState(1);
  const [limit, setLimit] = useState(20);
  const query = useOrderPromotionsQuery(appKey, {
    promotionId: filter?.kind === "promotion" ? filter.id : undefined,
    templateId: filter?.kind === "template" ? filter.id : undefined,
    userId: user?.id,
    status: status || undefined,
    keyword: debounced || undefined,
    page,
    limit
  });
  const data = query.data;
  const reset = (fn: () => void) => {
    fn();
    setPage(1);
  };

  const columns: Array<Column<OrderPromotion>> = [
    { id: "time", header: "下单时间", width: "150px", cell: (row) => <span className="text-[11px] tabular-nums text-muted-foreground">{formatDateTime(row.createdAt)}</span> },
    {
      id: "order",
      header: "订单",
      cell: (row) => (
        <div className="min-w-0 text-xs">
          <div className="truncate font-medium">{row.subject || SCOPE_LABEL[row.purpose] || "订单"}</div>
          <div className="truncate font-mono text-[11px] text-muted-foreground" title={row.orderNo}>{row.orderNo}</div>
        </div>
      )
    },
    {
      id: "user",
      header: "用户",
      width: "14%",
      hideBelow: "md",
      cell: (row) => <NumberUserCell appKey={appKey} userId={row.userId} account={row.userAccount} nickname={row.userNickname} />
    },
    {
      id: "price",
      header: "原价 → 实付",
      width: "150px",
      align: "right",
      cell: (row) => (
        <div className="text-xs tabular-nums">
          <div className="font-mono text-muted-foreground line-through">{formatMoney(row.originalAmount)}</div>
          <div className="font-mono font-medium">{formatMoney(row.payableAmount)}</div>
        </div>
      )
    },
    {
      id: "discount",
      header: "优惠",
      width: "22%",
      cell: (row) => (
        <div className="min-w-0 space-y-0.5 text-[11px]">
          {row.promotionName ? (
            <div className="flex items-center gap-1">
              <span className="truncate">{row.promotionName}</span>
              <span className="shrink-0 font-mono text-rose-600">−{formatMoney(row.promotionDiscount)}</span>
            </div>
          ) : null}
          {row.couponName ? (
            <div className="flex items-center gap-1">
              <span className="truncate">券 · {row.couponName}</span>
              <span className="shrink-0 font-mono text-rose-600">−{formatMoney(row.couponDiscount)}</span>
            </div>
          ) : null}
          {row.rebatePromotionName ? (
            <div className="truncate text-muted-foreground" title={row.rebateNote}>
              返利 · {row.rebatePromotionName}：{REBATE_STATUS_LABEL[row.rebateStatus]}
              {row.rebateNote ? `（${row.rebateNote}）` : ""}
            </div>
          ) : null}
        </div>
      )
    },
    {
      id: "status",
      header: "状态",
      width: "120px",
      cell: (row) => (
        <div className="min-w-0 space-y-0.5">
          <OrderStatusBadge status={row.status} />
          {row.note ? (
            <div className="truncate text-[10px] text-muted-foreground" title={row.note}>
              {row.note}
            </div>
          ) : null}
        </div>
      )
    }
  ];

  return (
    <Section
      title="订单优惠"
      description="每一笔用了活动或券的订单：预占、结算、释放与退款冲正"
      icon={ReceiptText}
      tone="info"
      flush
      toolbar={
        <FilterBar>
          {filter ? (
            <span className={cn("flex h-8 items-center gap-1 rounded-md border bg-background pr-1 pl-2.5 text-xs")}>
              {filter.kind === "promotion" ? "活动" : "券"}：{filter.name}
              <button type="button" aria-label="清除" className="rounded p-0.5 text-muted-foreground hover:bg-accent" onClick={() => reset(() => onFilterChange(null))}>
                <X className="size-3.5" />
              </button>
            </span>
          ) : null}
          <SearchField value={keyword} onChange={(value) => reset(() => setKeyword(value))} placeholder="订单号、商品、账号" />
          <FilterSelect label="状态" value={status} onChange={(value) => reset(() => setStatus(value))} options={STATUS_OPTIONS} />
          <div className="w-full sm:w-56">
            <UserPicker appKey={appKey} value={user} onChange={(next) => reset(() => setUser(next))} placeholder="按用户筛选" />
          </div>
          <ResetButton
            visible={Boolean(filter || status || debounced || user)}
            onClick={() =>
              reset(() => {
                setStatus("");
                setKeyword("");
                setUser(null);
                onFilterChange(null);
              })
            }
          />
        </FilterBar>
      }
      footer={data ? <Pager page={page} limit={limit} total={data.total} onPage={setPage} onLimit={(value) => reset(() => setLimit(value))} /> : null}
    >
      <RecordTable
        columns={columns}
        rows={data?.items}
        rowKey={(row) => row.id}
        loading={query.isLoading}
        fetching={query.isFetching}
        error={query.error}
        onRetry={() => void query.refetch()}
        minWidth={920}
        empty={<StateBlock compact icon={ReceiptText} title="还没有用到优惠的订单" />}
      />
    </Section>
  );
}
