"use client";

import { useState } from "react";
import { Ban, MoreHorizontal, Pause, Pencil, Play, Plus, Send, TicketPercent, Trash2, Users } from "lucide-react";
import { toast } from "sonner";
import { formatMoney } from "@/components/commerce/commerce-format";
import { ConfirmActionDialog } from "@/components/users/detail/records/records-shared";
import { FilterBar, FilterSelect, Pager, SearchField, Section, StateBlock, formatInt, useDebouncedValue } from "@/components/users/detail/kit";
import { Button } from "@/components/ui/button";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { Skeleton } from "@/components/ui/skeleton";
import { ApiError } from "@/lib/api-client";
import type { CouponStatus, CouponTemplate } from "@/lib/api/coupons";
import { useCouponTemplatesQuery, useDeleteCouponTemplateMutation, useSetCouponTemplateStatusMutation } from "@/lib/coupon-hooks";
import { formatDateTimeMinute } from "@/lib/datetime";
import { audienceText, CouponTicket, STATUS_META, StatusBadge } from "./coupon-shared";
import { CouponIssueDialog } from "./coupon-issue-dialog";
import { CouponTemplateDialog } from "./coupon-template-dialog";

const STATUS_OPTIONS = (Object.keys(STATUS_META) as CouponStatus[]).map((status) => ({ value: status, label: STATUS_META[status].label }));

type Pending = { kind: "status"; template: CouponTemplate; status: CouponStatus } | { kind: "delete"; template: CouponTemplate };

export function CouponTemplatesPanel({
  appKey,
  canWrite,
  canDelete,
  canIssue,
  onOpenCoupons
}: {
  appKey: string;
  canWrite: boolean;
  canDelete: boolean;
  canIssue: boolean;
  /** 跳到用户券并按这种券筛选 */
  onOpenCoupons: (template: CouponTemplate) => void;
}) {
  const [status, setStatus] = useState("");
  const [keyword, setKeyword] = useState("");
  const debounced = useDebouncedValue(keyword.trim(), 300);
  const [page, setPage] = useState(1);
  const [limit, setLimit] = useState(20);
  const query = useCouponTemplatesQuery(appKey, { status: status || undefined, keyword: debounced || undefined, page, limit });
  const data = query.data;

  const [editing, setEditing] = useState<{ key: number; template: CouponTemplate | null } | null>(null);
  const [issuing, setIssuing] = useState<{ key: number; template: CouponTemplate } | null>(null);
  const [pending, setPending] = useState<Pending | null>(null);
  const setStatusMutation = useSetCouponTemplateStatusMutation(appKey);
  const deleteMutation = useDeleteCouponTemplateMutation(appKey);

  const confirm = async () => {
    if (!pending) return;
    try {
      if (pending.kind === "status") {
        await setStatusMutation.mutateAsync({ id: pending.template.id, status: pending.status });
        toast.success(`已${STATUS_ACTION[pending.status]}`);
      } else {
        await deleteMutation.mutateAsync(pending.template.id);
        toast.success("已删除");
      }
      setPending(null);
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "操作失败");
    }
  };

  return (
    <Section
      title="优惠券"
      description="每一种券的规则；用户手里的每一张券都来自其中一种"
      icon={TicketPercent}
      tone="danger"
      actions={
        canWrite ? (
          <Button size="sm" onClick={() => setEditing({ key: Date.now(), template: null })}>
            <Plus />
            新建优惠券
          </Button>
        ) : null
      }
      toolbar={
        <FilterBar>
          <SearchField value={keyword} onChange={(value) => { setKeyword(value); setPage(1); }} placeholder="搜索名称或说明" />
          <FilterSelect label="状态" value={status} onChange={(value) => { setStatus(value); setPage(1); }} options={STATUS_OPTIONS} />
        </FilterBar>
      }
      footer={data && data.total > 0 ? <Pager page={page} limit={limit} total={data.total} onPage={setPage} onLimit={(value) => { setLimit(value); setPage(1); }} /> : null}
    >
      {query.isLoading ? (
        <div className="grid gap-3 lg:grid-cols-2">
          {Array.from({ length: 4 }).map((_, index) => (
            <Skeleton key={index} className="h-28 rounded-xl" />
          ))}
        </div>
      ) : query.error ? (
        <StateBlock kind="error" onRetry={() => void query.refetch()} />
      ) : !data || data.items.length === 0 ? (
        <StateBlock
          icon={TicketPercent}
          title={status || debounced ? "没有符合条件的券" : "还没有优惠券"}
          description="立减或折扣、门槛、适用订单与有效期都在一张券里配置；上线后可以放进领券中心、手动发放，或配进积分商城、抽奖、卡密与发放规则。"
        />
      ) : (
        <div className="grid gap-3 lg:grid-cols-2">
          {data.items.map((template) => (
            <TemplateCard
              key={template.id}
              template={template}
              canWrite={canWrite}
              canDelete={canDelete}
              canIssue={canIssue}
              onEdit={() => setEditing({ key: Date.now(), template })}
              onIssue={() => setIssuing({ key: Date.now(), template })}
              onStatus={(next) => setPending({ kind: "status", template, status: next })}
              onDelete={() => setPending({ kind: "delete", template })}
              onOpenCoupons={() => onOpenCoupons(template)}
            />
          ))}
        </div>
      )}

      {editing ? (
        <CouponTemplateDialog key={editing.key} appKey={appKey} template={editing.template} open onOpenChange={(open) => !open && setEditing(null)} />
      ) : null}
      {issuing ? (
        <CouponIssueDialog key={issuing.key} appKey={appKey} template={issuing.template} open onOpenChange={(open) => !open && setIssuing(null)} />
      ) : null}
      <ConfirmActionDialog
        open={pending !== null}
        onOpenChange={(open) => !open && setPending(null)}
        title={pending?.kind === "delete" ? "删除这种券？" : `${pending ? STATUS_ACTION[pending.status] : ""}「${pending?.template.name ?? ""}」？`}
        description={pending ? pendingDescription(pending) : undefined}
        confirmLabel={pending?.kind === "delete" ? "删除" : pending ? STATUS_ACTION[pending.status] : "确定"}
        destructive={pending?.kind === "delete" || (pending?.kind === "status" && pending.status === "ended")}
        pending={setStatusMutation.isPending || deleteMutation.isPending}
        onConfirm={() => void confirm()}
      />
    </Section>
  );
}

const STATUS_ACTION: Record<CouponStatus, string> = { draft: "改回草稿", active: "上线", paused: "暂停", ended: "结束" };

function pendingDescription(pending: Pending) {
  if (pending.kind === "delete") return "只能删除一张都没发出去的券；删除后不可恢复。";
  switch (pending.status) {
    case "active":
      return "上线后可以发放；开了领券中心的会立刻出现在用户的领券中心里。";
    case "paused":
      return "暂停后领券中心不再显示、积分商城与抽奖发不出这种券；管理员与返券活动仍可发。已发出的券照常能用。";
    case "ended":
      return "结束后任何渠道都不能再发，也不能再上线；已发出的券照常能用到各自的有效期。";
  }
  return undefined;
}

function TemplateCard({
  template,
  canWrite,
  canDelete,
  canIssue,
  onEdit,
  onIssue,
  onStatus,
  onDelete,
  onOpenCoupons
}: {
  template: CouponTemplate;
  canWrite: boolean;
  canDelete: boolean;
  canIssue: boolean;
  onEdit: () => void;
  onIssue: () => void;
  onStatus: (status: CouponStatus) => void;
  onDelete: () => void;
  onOpenCoupons: () => void;
}) {
  const stock = template.totalQuantity > 0 ? `${formatInt(template.issuedCount)} / ${formatInt(template.totalQuantity)}` : `${formatInt(template.issuedCount)} / 不限`;
  const issuable = template.status === "active" || template.status === "paused";
  const transitions: CouponStatus[] =
    template.status === "draft" ? ["active", "ended"] : template.status === "active" ? ["paused", "ended"] : template.status === "paused" ? ["active", "ended"] : [];

  return (
    <CouponTicket
      template={template}
      dimmed={template.status === "ended"}
      aside={
        <div className="flex items-center gap-1">
          <StatusBadge status={template.status} />
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button size="icon" variant="ghost" className="size-7" aria-label="更多操作">
                <MoreHorizontal className="size-4" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-44">
              {canIssue && issuable ? (
                <DropdownMenuItem onSelect={onIssue}>
                  <Send /> 发券
                </DropdownMenuItem>
              ) : null}
              <DropdownMenuItem onSelect={onOpenCoupons}>
                <Users /> 查看已发的券
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
              {canDelete && template.issuedCount === 0 ? (
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
      }
      footer={
        <div className="flex flex-wrap items-center gap-x-3 gap-y-0.5 text-muted-foreground">
          <span>
            已发 <span className="font-medium text-foreground tabular-nums">{stock}</span>
          </span>
          <span>
            已用 <span className="font-medium text-foreground tabular-nums">{formatInt(template.usedCount)}</span>
          </span>
          <span>
            让利 <span className="font-mono font-medium text-foreground">{formatMoney(template.discountTotal)}</span>
          </span>
          {template.claimable ? (
            <span title={template.claimEndsAt ? `领取截止 ${formatDateTimeMinute(template.claimEndsAt)}` : undefined}>
              领券中心 · {audienceText(template.audience)}
              {template.perUserLimit > 0 ? ` · 每人 ${template.perUserLimit} 张` : ""}
            </span>
          ) : null}
        </div>
      }
    />
  );
}
