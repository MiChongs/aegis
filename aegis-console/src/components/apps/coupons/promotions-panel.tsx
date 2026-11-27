"use client";

import { useState } from "react";
import { Ban, CalendarClock, Megaphone, MoreHorizontal, Pause, Pencil, Play, Plus, ReceiptText, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { formatMoney, formatPercent, moneyRatio } from "@/components/commerce/commerce-format";
import { ConfirmActionDialog } from "@/components/users/detail/records/records-shared";
import { FilterBar, FilterSelect, Pager, SearchField, Section, StateBlock, formatInt, useDebouncedValue } from "@/components/users/detail/kit";
import { Button } from "@/components/ui/button";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { Skeleton } from "@/components/ui/skeleton";
import { ApiError } from "@/lib/api-client";
import type { CouponStatus, Promotion, PromotionKind } from "@/lib/api/coupons";
import { useDeletePromotionMutation, usePromotionsQuery, useSetPromotionStatusMutation } from "@/lib/coupon-hooks";
import { formatDateTimeMinute } from "@/lib/datetime";
import { cn } from "@/lib/utils";
import {
  PROMOTION_KIND_LABEL,
  STATUS_META,
  ScopeChips,
  StatusBadge,
  THEME_VAR,
  audienceText,
  promotionRuleText,
  themeStyle
} from "./coupon-shared";
import { PromotionDialog } from "./promotion-dialog";

const STATUS_OPTIONS = (Object.keys(STATUS_META) as CouponStatus[]).map((status) => ({ value: status, label: STATUS_META[status].label }));
const KIND_OPTIONS = (Object.keys(PROMOTION_KIND_LABEL) as PromotionKind[]).map((kind) => ({ value: kind, label: PROMOTION_KIND_LABEL[kind] }));
const STATUS_ACTION: Record<CouponStatus, string> = { draft: "改回草稿", active: "上线", paused: "暂停", ended: "结束" };

type Pending = { kind: "status"; promotion: Promotion; status: CouponStatus } | { kind: "delete"; promotion: Promotion };

/** 活动此刻处在哪个阶段（上线了不等于正在生效：还没到开始时间、或已过结束时间） */
function phaseOf(p: Promotion, now = Date.now()) {
  if (p.status !== "active") return null;
  if (p.startsAt && new Date(p.startsAt).getTime() > now) return { label: "未开始", className: "text-sky-600 dark:text-sky-400" };
  if (p.endsAt && new Date(p.endsAt).getTime() <= now) return { label: "已过期", className: "text-muted-foreground" };
  return { label: "生效中", className: "text-emerald-600 dark:text-emerald-400" };
}

export function PromotionsPanel({
  appKey,
  canWrite,
  canDelete,
  onOpenOrders
}: {
  appKey: string;
  canWrite: boolean;
  canDelete: boolean;
  onOpenOrders: (promotion: Promotion) => void;
}) {
  const [kind, setKind] = useState("");
  const [status, setStatus] = useState("");
  const [keyword, setKeyword] = useState("");
  const debounced = useDebouncedValue(keyword.trim(), 300);
  const [page, setPage] = useState(1);
  const [limit, setLimit] = useState(20);
  const query = usePromotionsQuery(appKey, { kind: kind || undefined, status: status || undefined, keyword: debounced || undefined, page, limit });
  const data = query.data;
  const [editing, setEditing] = useState<{ key: number; promotion: Promotion | null } | null>(null);
  const [pending, setPending] = useState<Pending | null>(null);
  const setStatusMutation = useSetPromotionStatusMutation(appKey);
  const deleteMutation = useDeletePromotionMutation(appKey);

  const confirm = async () => {
    if (!pending) return;
    try {
      if (pending.kind === "status") {
        await setStatusMutation.mutateAsync({ id: pending.promotion.id, status: pending.status });
        toast.success(`已${STATUS_ACTION[pending.status]}`);
      } else {
        await deleteMutation.mutateAsync(pending.promotion.id);
        toast.success("已删除");
      }
      setPending(null);
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "操作失败");
    }
  };

  return (
    <Section
      title="优惠活动"
      description="限时折扣、满减与支付返券；自动生效，不用领"
      icon={Megaphone}
      tone="warning"
      actions={
        canWrite ? (
          <Button size="sm" onClick={() => setEditing({ key: Date.now(), promotion: null })}>
            <Plus />
            新建活动
          </Button>
        ) : null
      }
      toolbar={
        <FilterBar>
          <SearchField value={keyword} onChange={(value) => { setKeyword(value); setPage(1); }} placeholder="搜索名称、说明或角标" />
          <FilterSelect label="类型" value={kind} onChange={(value) => { setKind(value); setPage(1); }} options={KIND_OPTIONS} />
          <FilterSelect label="状态" value={status} onChange={(value) => { setStatus(value); setPage(1); }} options={STATUS_OPTIONS} />
        </FilterBar>
      }
      footer={data && data.total > 0 ? <Pager page={page} limit={limit} total={data.total} onPage={setPage} onLimit={(value) => { setLimit(value); setPage(1); }} /> : null}
    >
      {query.isLoading ? (
        <div className="grid gap-3 lg:grid-cols-2">
          {Array.from({ length: 4 }).map((_, index) => (
            <Skeleton key={index} className="h-36 rounded-xl" />
          ))}
        </div>
      ) : query.error ? (
        <StateBlock kind="error" onRetry={() => void query.refetch()} />
      ) : !data || data.items.length === 0 ? (
        <StateBlock
          icon={Megaphone}
          title={kind || status || debounced ? "没有符合条件的活动" : "还没有优惠活动"}
          description="限时折扣给套餐打折、立减或直接定特价；满减按订单金额阶梯减；支付返券在付款后发券、发积分。活动可以设人群、时段、名额与预算。"
        />
      ) : (
        <div className="grid gap-3 lg:grid-cols-2">
          {data.items.map((promotion) => (
            <PromotionCard
              key={promotion.id}
              promotion={promotion}
              canWrite={canWrite}
              canDelete={canDelete}
              onEdit={() => setEditing({ key: Date.now(), promotion })}
              onStatus={(next) => setPending({ kind: "status", promotion, status: next })}
              onDelete={() => setPending({ kind: "delete", promotion })}
              onOpenOrders={() => onOpenOrders(promotion)}
            />
          ))}
        </div>
      )}

      {editing ? (
        <PromotionDialog key={editing.key} appKey={appKey} promotion={editing.promotion} open onOpenChange={(open) => !open && setEditing(null)} />
      ) : null}
      <ConfirmActionDialog
        open={pending !== null}
        onOpenChange={(open) => !open && setPending(null)}
        title={pending?.kind === "delete" ? "删除这期活动？" : `${pending ? STATUS_ACTION[pending.status] : ""}「${pending?.promotion.name ?? ""}」？`}
        description={
          pending?.kind === "delete"
            ? "只能删除没有任何订单参与过的活动；删除后不可恢复。"
            : pending?.status === "active"
              ? "上线后在设定的时段内自动生效，新下的订单立刻按活动价算。"
              : pending?.status === "paused"
                ? "暂停后新订单不再享受；已经下单待付款的订单仍按下单时的价格。"
                : "结束后不能再上线；已经下单待付款的订单仍按下单时的价格。"
        }
        confirmLabel={pending?.kind === "delete" ? "删除" : pending ? STATUS_ACTION[pending.status] : "确定"}
        destructive={pending?.kind === "delete" || (pending?.kind === "status" && pending.status === "ended")}
        pending={setStatusMutation.isPending || deleteMutation.isPending}
        onConfirm={() => void confirm()}
      />
    </Section>
  );
}

function PromotionCard({
  promotion: p,
  canWrite,
  canDelete,
  onEdit,
  onStatus,
  onDelete,
  onOpenOrders
}: {
  promotion: Promotion;
  canWrite: boolean;
  canDelete: boolean;
  onEdit: () => void;
  onStatus: (status: CouponStatus) => void;
  onDelete: () => void;
  onOpenOrders: () => void;
}) {
  const phase = phaseOf(p);
  const transitions: CouponStatus[] =
    p.status === "draft" ? ["active", "ended"] : p.status === "active" ? ["paused", "ended"] : p.status === "paused" ? ["active", "ended"] : [];
  const budgetRatio = p.kind !== "rebate" ? moneyRatio(p.discountTotal, p.budget) : null;

  return (
    <div
      style={themeStyle(p.theme)}
      className={cn(
        THEME_VAR,
        "flex min-w-0 flex-col gap-2.5 rounded-xl border border-[color-mix(in_oklab,var(--coupon)_30%,transparent)] bg-[color-mix(in_oklab,var(--coupon)_6%,var(--card))] p-3.5",
        p.status === "ended" && "opacity-60 saturate-50"
      )}
    >
      <div className="flex items-start gap-2">
        <span className="shrink-0 rounded-md bg-[var(--coupon)] px-2 py-0.5 text-[11px] font-semibold text-white">{p.tag}</span>
        <div className="min-w-0 flex-1">
          <div className="truncate text-sm font-semibold">{p.name}</div>
          <div className="truncate text-[11px] text-muted-foreground">
            {PROMOTION_KIND_LABEL[p.kind]} · {promotionRuleText(p)}
          </div>
        </div>
        <StatusBadge status={p.status} />
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button size="icon" variant="ghost" className="size-7" aria-label="更多操作">
              <MoreHorizontal className="size-4" />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="w-44">
            <DropdownMenuItem onSelect={onOpenOrders}>
              <ReceiptText /> 查看参与订单
            </DropdownMenuItem>
            {canWrite ? (
              <DropdownMenuItem onSelect={onEdit}>
                <Pencil /> 编辑
              </DropdownMenuItem>
            ) : null}
            {canWrite && transitions.length > 0 ? <DropdownMenuSeparator /> : null}
            {canWrite
              ? transitions.map((next) => (
                  <DropdownMenuItem key={next} onSelect={() => onStatus(next)} variant={next === "ended" ? "destructive" : undefined}>
                    {next === "active" ? <Play /> : next === "paused" ? <Pause /> : <Ban />}
                    {STATUS_ACTION[next]}
                  </DropdownMenuItem>
                ))
              : null}
            {canDelete && p.usedCount === 0 ? (
              <>
                <DropdownMenuSeparator />
                <DropdownMenuItem variant="destructive" onSelect={onDelete}>
                  <Trash2 /> 删除
                </DropdownMenuItem>
              </>
            ) : null}
          </DropdownMenuContent>
        </DropdownMenu>
      </div>

      <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-[11px] text-muted-foreground">
        <span className="flex items-center gap-1">
          <CalendarClock className="size-3" />
          {p.startsAt ? formatDateTimeMinute(p.startsAt) : "上线即开始"} 至 {p.endsAt ? formatDateTimeMinute(p.endsAt) : "不结束"}
        </span>
        {phase ? <span className={cn("font-medium", phase.className)}>{phase.label}</span> : null}
      </div>
      <div className="flex flex-wrap items-center gap-2 text-[11px] text-muted-foreground">
        <ScopeChips scopes={p.scopes} />
        <span>{audienceText(p.audience)}</span>
        {p.planIds.length ? <span>限 {p.planIds.length} 个套餐</span> : null}
        {p.packageIds.length ? <span>限 {p.packageIds.length} 个档位</span> : null}
        {p.kind !== "rebate" ? <span>{p.stackWithCoupon ? "可与券同享" : "不与券同享"}</span> : null}
      </div>

      <div className="grid grid-cols-3 gap-2 border-t border-[color-mix(in_oklab,var(--coupon)_20%,transparent)] pt-2.5 text-xs">
        <Stat label="参与订单" value={`${formatInt(p.usedCount)}${p.totalLimit > 0 ? ` / ${formatInt(p.totalLimit)}` : ""}`} />
        <Stat label={p.kind === "rebate" ? "每人限" : "让利"} value={p.kind === "rebate" ? (p.perUserLimit > 0 ? `${p.perUserLimit} 次` : "不限") : formatMoney(p.discountTotal)} mono={p.kind !== "rebate"} />
        <Stat
          label={p.kind === "rebate" ? "优先级" : "预算"}
          value={p.kind === "rebate" ? String(p.priority) : budgetRatio === null ? "不限" : `${formatPercent(budgetRatio)} 已用`}
        />
      </div>
    </div>
  );
}

function Stat({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="min-w-0">
      <div className="text-[10px] text-muted-foreground">{label}</div>
      <div className={cn("truncate font-medium tabular-nums", mono && "font-mono")}>{value}</div>
    </div>
  );
}
