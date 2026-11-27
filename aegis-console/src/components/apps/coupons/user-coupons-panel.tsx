"use client";

import { useState } from "react";
import { Tickets, Undo2 } from "lucide-react";
import { toast } from "sonner";
import { NumberUserCell } from "@/components/apps/user-number/number-shared";
import { formatMoney } from "@/components/commerce/commerce-format";
import { UserPicker, type PickedUser } from "@/components/commerce/user-picker";
import { ConfirmActionDialog } from "@/components/users/detail/records/records-shared";
import {
  CopyButton,
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
import { Button } from "@/components/ui/button";
import { ApiError } from "@/lib/api-client";
import type { UserCoupon, UserCouponStatus } from "@/lib/api/coupons";
import { useAllCouponTemplatesQuery, useRevokeUserCouponMutation, useUserCouponsQuery } from "@/lib/coupon-hooks";
import { formatDateTimeMinute } from "@/lib/datetime";
import { cn } from "@/lib/utils";
import { SOURCE_LABEL, THEME_VAR, USER_COUPON_STATUS_META, UserCouponStatusBadge, templateFace, themeStyle } from "./coupon-shared";

const STATUS_OPTIONS = (Object.keys(USER_COUPON_STATUS_META) as UserCouponStatus[]).map((status) => ({
  value: status,
  label: USER_COUPON_STATUS_META[status].label
}));
const SOURCE_OPTIONS = Object.entries(SOURCE_LABEL).map(([value, label]) => ({ value, label }));

/** 发到用户手里的每一张券：按券、状态、来源、用户筛选；可用的券能收回。 */
export function UserCouponsPanel({
  appKey,
  canIssue,
  templateId,
  onTemplateChange
}: {
  appKey: string;
  canIssue: boolean;
  /** 由券列表「查看已发的券」带过来 */
  templateId: number;
  onTemplateChange: (id: number) => void;
}) {
  const [status, setStatus] = useState("");
  const [source, setSource] = useState("");
  const [keyword, setKeyword] = useState("");
  const [user, setUser] = useState<PickedUser | null>(null);
  const debounced = useDebouncedValue(keyword.trim(), 300);
  const [page, setPage] = useState(1);
  const [limit, setLimit] = useState(20);
  const templates = useAllCouponTemplatesQuery(appKey).data?.items ?? [];
  const query = useUserCouponsQuery(appKey, {
    templateId: templateId || undefined,
    userId: user?.id,
    status: status || undefined,
    source: source || undefined,
    keyword: debounced || undefined,
    page,
    limit
  });
  const data = query.data;
  const [revoking, setRevoking] = useState<UserCoupon | null>(null);
  const [reason, setReason] = useState("");
  const revoke = useRevokeUserCouponMutation(appKey);
  const reset = (fn: () => void) => {
    fn();
    setPage(1);
  };
  const filtered = Boolean(templateId || status || source || debounced || user);

  const columns: Array<Column<UserCoupon>> = [
    {
      id: "coupon",
      header: "券",
      cell: (row) => (
        <div className="flex min-w-0 items-center gap-2" style={themeStyle(row.template?.theme)}>
          <span className={cn(THEME_VAR, "h-8 w-1 shrink-0 rounded-full bg-[var(--coupon)]")} />
          <div className="min-w-0">
            <div className="truncate text-xs font-medium">{row.template?.name ?? `#${row.templateId}`}</div>
            <div className="flex items-center gap-1 text-[11px] text-muted-foreground">
              <span className="truncate">{row.template ? templateFace(row.template) : ""}</span>
              <span className="font-mono">{row.code}</span>
              <CopyButton value={row.code} />
            </div>
          </div>
        </div>
      )
    },
    {
      id: "user",
      header: "用户",
      width: "15%",
      cell: (row) => <NumberUserCell appKey={appKey} userId={row.userId} account={row.userAccount} nickname={row.userNickname} />
    },
    { id: "status", header: "状态", width: "84px", cell: (row) => <UserCouponStatusBadge status={row.status} /> },
    {
      id: "source",
      header: "来源",
      width: "110px",
      hideBelow: "md",
      cell: (row) => (
        <div className="min-w-0 text-xs">
          <div className="truncate">{SOURCE_LABEL[row.source] ?? row.source}</div>
          {row.sourceRef ? <div className="truncate font-mono text-[10px] text-muted-foreground" title={row.sourceRef}>{row.sourceRef}</div> : null}
        </div>
      )
    },
    {
      id: "valid",
      header: "有效期",
      width: "150px",
      hideBelow: "lg",
      cell: (row) => (
        <div className="text-[11px] tabular-nums text-muted-foreground">
          <div>{formatDateTimeMinute(row.validFrom)}</div>
          <div>至 {formatDateTimeMinute(row.validUntil)}</div>
        </div>
      )
    },
    {
      id: "order",
      header: "订单 / 抵扣",
      width: "160px",
      cell: (row) =>
        row.orderNo ? (
          <div className="min-w-0 text-[11px]">
            <div className="truncate font-mono" title={row.orderNo}>{row.orderNo}</div>
            {row.status === "used" ? <div className="font-mono text-emerald-600">−{formatMoney(row.discount)}</div> : <div className="text-muted-foreground">待付款</div>}
          </div>
        ) : row.revokedReason ? (
          <span className="truncate text-[11px] text-muted-foreground" title={row.revokedReason}>{row.revokedReason}</span>
        ) : (
          <span className="text-[11px] text-muted-foreground">—</span>
        )
    },
    {
      id: "actions",
      header: "",
      width: "64px",
      align: "right",
      cell: (row) =>
        canIssue && (row.status === "available" || row.status === "not_started" || row.status === "expired") ? (
          <Button size="sm" variant="ghost" className="h-7 px-2 text-xs" onClick={() => { setReason(""); setRevoking(row); }}>
            <Undo2 className="size-3.5" />
            收回
          </Button>
        ) : null
    }
  ];

  return (
    <Section
      title="用户的券"
      description="发到用户手里的每一张券；被订单占用的券在订单关闭 15 分钟后自动返还"
      icon={Tickets}
      tone="violet"
      flush
      toolbar={
        <FilterBar>
          <SearchField value={keyword} onChange={(value) => reset(() => setKeyword(value))} placeholder="券号、订单号、账号" />
          <FilterSelect
            label="券"
            value={templateId ? String(templateId) : ""}
            onChange={(value) => reset(() => onTemplateChange(Number(value) || 0))}
            options={templates.map((t) => ({ value: String(t.id), label: t.name }))}
          />
          <FilterSelect label="状态" value={status} onChange={(value) => reset(() => setStatus(value))} options={STATUS_OPTIONS} />
          <FilterSelect label="来源" value={source} onChange={(value) => reset(() => setSource(value))} options={SOURCE_OPTIONS} />
          <div className="w-full sm:w-56">
            <UserPicker appKey={appKey} value={user} onChange={(next) => reset(() => setUser(next))} placeholder="按用户筛选" />
          </div>
          <ResetButton
            visible={filtered}
            onClick={() =>
              reset(() => {
                setKeyword("");
                setStatus("");
                setSource("");
                setUser(null);
                onTemplateChange(0);
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
        minWidth={880}
        empty={<StateBlock compact icon={Tickets} title={filtered ? "没有符合条件的券" : "还没有发出过券"} />}
      />
      <ConfirmActionDialog
        open={revoking !== null}
        onOpenChange={(open) => !open && setRevoking(null)}
        title="收回这张券？"
        description={revoking ? `「${revoking.template?.name ?? ""}」${revoking.code}，收回后用户不能再用。` : undefined}
        confirmLabel="收回"
        destructive
        pending={revoke.isPending}
        note={{ value: reason, onChange: setReason, placeholder: "收回原因（用户可见）", max: 200 }}
        onConfirm={async () => {
          if (!revoking) return;
          try {
            await revoke.mutateAsync({ id: revoking.id, reason: reason.trim() || undefined });
            toast.success("已收回");
            setRevoking(null);
          } catch (err) {
            toast.error(err instanceof ApiError ? err.message : "收回失败");
          }
        }}
      />
    </Section>
  );
}
