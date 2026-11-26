"use client";

import { useMemo, useState } from "react";
import Link from "next/link";
import { ArrowRight, Ban, CheckCircle2, Gift, RotateCw, ShieldAlert, Undo2 } from "lucide-react";
import { toast } from "sonner";
import { formatPoints } from "@/components/apps/points-store/points-store-shared";
import { ConfirmActionDialog, ResultChips, ResultList, usePaging } from "@/components/users/detail/records/records-shared";
import {
  FilterBar,
  Pager,
  RecordSheet,
  RecordTable,
  ResetButton,
  SearchField,
  Section,
  StateBlock,
  TimeRangePicker,
  errorMessage,
  formatInt,
  rangeToParams,
  useDebouncedValue,
  type Column,
  type TimeRange
} from "@/components/users/detail/kit";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import type {
  PointsStoreGift,
  PointsStoreGiftAction,
  PointsStoreGiftStatus,
  PointsStoreGiftSummary,
  PointsStoreGiftUser
} from "@/lib/api/points-store";
import { formatDateTime, formatRelative } from "@/lib/datetime";
import { useHandlePointsStoreGiftMutation, usePointsStoreGiftsQuery } from "@/lib/points-store-hooks";
import { cn } from "@/lib/utils";

/**
 * 赠好友：赠送记录、审核队列与撤销追回（docs/points-store-gift.md）。
 *
 * 用户在 App 与官网里选好人直接提交，服务端在一个事务里扣积分、发给对方；风控判为可疑的进「审核中」
 * （积分已扣、东西没发），这里通过即发放、驳回即原路退积分。已送达的可以撤销：会员扣掉还没走完的时长，
 * 功能解锁与装扮收回；退不退积分由处理人决定（误送退，套利不退）。被风控直接拒绝的尝试记为「已拦截」，
 * 没扣积分，只供排查。
 */

export const GIFT_STATUS_META: Record<PointsStoreGiftStatus, { label: string; variant: "success" | "warning" | "secondary" | "danger" | "info" }> = {
  reviewing: { label: "审核中", variant: "warning" },
  delivered: { label: "已送达", variant: "success" },
  rejected: { label: "已驳回", variant: "secondary" },
  revoked: { label: "已撤销", variant: "danger" },
  blocked: { label: "已拦截", variant: "info" }
};

const STATUS_TABS: Array<{ value: "" | PointsStoreGiftStatus; label: string; count: (summary: PointsStoreGiftSummary) => number }> = [
  { value: "reviewing", label: "待审核", count: (s) => s.reviewing },
  { value: "", label: "全部", count: (s) => s.reviewing + s.delivered + s.rejected + s.revoked + s.blocked },
  { value: "delivered", label: "已送达", count: (s) => s.delivered },
  { value: "rejected", label: "已驳回", count: (s) => s.rejected },
  { value: "revoked", label: "已撤销", count: (s) => s.revoked },
  { value: "blocked", label: "已拦截", count: (s) => s.blocked }
];

const ACTION_VARIANT: Record<PointsStoreGiftAction, "success" | "warning" | "danger"> = {
  allow: "success",
  review: "warning",
  reject: "danger"
};

const REVOKE_OUTCOME: Record<string, string> = {
  recovered: "已追回",
  expired: "已过期，无可追回",
  missing: "找不到发放记录（已被收回或折算）"
};

type GiftRequest = { action: "approve" | "reject" | "revoke"; gift: PointsStoreGift } | null;

export function GiftStatusBadge({ status }: { status: PointsStoreGiftStatus }) {
  const meta = GIFT_STATUS_META[status] ?? GIFT_STATUS_META.delivered;
  return (
    <Badge variant={meta.variant} size="sm">
      {meta.label}
    </Badge>
  );
}

function userName(user: PointsStoreGiftUser) {
  return user.nickname || user.account || (user.number ? String(user.number) : user.userId ? `#${user.userId}` : "已注销");
}

export function PointsStoreGiftsPanel({ appKey, initialStatus }: { appKey: string; initialStatus?: "" | PointsStoreGiftStatus }) {
  const [status, setStatus] = useState<"" | PointsStoreGiftStatus>(initialStatus ?? "reviewing");
  const [keyword, setKeyword] = useState("");
  const [range, setRange] = useState<TimeRange>({ preset: "all" });
  const paging = usePaging(20);
  const debouncedKeyword = useDebouncedValue(keyword.trim());
  const [detail, setDetail] = useState<PointsStoreGift | null>(null);
  const [request, setRequest] = useState<GiftRequest>(null);

  const listQuery = usePointsStoreGiftsQuery(appKey, {
    status: status || undefined,
    keyword: debouncedKeyword || undefined,
    ...rangeToParams(range),
    page: paging.page,
    limit: paging.limit
  });
  const rows = useMemo(() => listQuery.data?.items ?? [], [listQuery.data]);
  const summary = listQuery.data?.summary;
  const filtered = Boolean(keyword || range.preset !== "all");

  const userHref = (user: PointsStoreGiftUser) =>
    user.userId ? `/app-users/${encodeURIComponent(appKey)}/${user.userId}?tab=records&sub=points-store` : undefined;

  const columns: Array<Column<PointsStoreGift>> = [
    {
      id: "item",
      header: "赠送",
      cell: (row) => (
        <div className="flex min-w-0 flex-col">
          <span className="truncate font-medium">{row.itemTitle}</span>
          <span className="truncate font-mono text-xs text-muted-foreground">{row.giftNo}</span>
        </div>
      )
    },
    {
      id: "parties",
      header: "赠送人 → 接收人",
      cell: (row) => (
        <div className="flex min-w-0 items-center gap-1.5 text-sm">
          <PartyLink user={row.sender} href={userHref(row.sender)} />
          <ArrowRight className="size-3.5 shrink-0 text-muted-foreground" />
          <PartyLink user={row.recipient} href={userHref(row.recipient)} />
        </div>
      )
    },
    {
      id: "cost",
      header: "积分",
      align: "right",
      cell: (row) => (
        <span className={cn("tabular-nums", row.status === "rejected" || row.status === "blocked" ? "text-muted-foreground line-through" : "font-medium")}>
          {formatPoints(row.cost)}
        </span>
      )
    },
    {
      id: "risk",
      header: "风控",
      hideBelow: "lg",
      cell: (row) =>
        row.riskHits?.length ? (
          <div className="flex flex-wrap gap-1">
            {row.riskHits.map((hit) => (
              <Badge key={hit.key} variant={ACTION_VARIANT[hit.action] ?? "warning"} size="sm" className="font-normal">
                {hit.label}
              </Badge>
            ))}
          </div>
        ) : (
          <span className="text-xs text-muted-foreground">无命中</span>
        )
    },
    { id: "results", header: "发放", hideBelow: "xl", cell: (row) => <ResultChips results={row.results} /> },
    { id: "status", header: "状态", cell: (row) => <GiftStatusBadge status={row.status} /> },
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
      width: "128px",
      cell: (row) => (
        <div className="flex justify-end gap-1.5" onClick={(event) => event.stopPropagation()}>
          <GiftActions gift={row} onRequest={setRequest} compact />
        </div>
      )
    }
  ];

  return (
    <div className="flex flex-col gap-4">
      <div className="-mx-1 flex gap-2 overflow-x-auto px-1 pb-0.5">
        {STATUS_TABS.map((tab) => {
          const active = status === tab.value;
          const count = summary ? tab.count(summary) : undefined;
          return (
            <button
              key={tab.value || "all"}
              type="button"
              onClick={() => {
                setStatus(tab.value);
                paging.reset();
              }}
              className={cn(
                "flex min-w-[104px] shrink-0 flex-col gap-0.5 rounded-xl border bg-card px-4 py-2.5 text-left transition-colors",
                active ? "border-primary ring-1 ring-primary" : "hover:bg-muted/40"
              )}
            >
              <span className={cn("text-xs", active ? "text-primary" : "text-muted-foreground")}>{tab.label}</span>
              <span
                className={cn(
                  "text-lg font-semibold tabular-nums",
                  tab.value === "reviewing" && count ? "text-amber-600 dark:text-amber-400" : undefined
                )}
              >
                {count === undefined ? "—" : formatInt(count)}
              </span>
            </button>
          );
        })}
        {summary ? (
          <div className="flex min-w-[148px] shrink-0 flex-col justify-center gap-0.5 rounded-xl border border-dashed px-4 py-2.5">
            <span className="text-xs text-muted-foreground">今日送达</span>
            <span className="text-sm font-medium tabular-nums">
              {formatInt(summary.todayDelivered)} 笔 · {formatPoints(summary.todayPoints)} 积分
            </span>
          </div>
        ) : null}
      </div>

      <Section
        flush
        title={status === "reviewing" ? "审核队列" : "赠送记录"}
        icon={status === "reviewing" ? ShieldAlert : Gift}
        tone={status === "reviewing" ? "warning" : "violet"}
        description={
          status === "reviewing"
            ? "风控判为可疑的赠送：积分已扣、权益未发。通过即发给接收人，驳回即退回积分，双方都会收到站内信"
            : "点开一行看双方、风控命中与当时的事实；已送达的可以撤销并追回"
        }
        actions={
          <Button variant="ghost" size="icon-sm" aria-label="刷新" onClick={() => listQuery.refetch()}>
            <RotateCw className={listQuery.isFetching ? "animate-spin" : undefined} />
          </Button>
        }
        toolbar={
          <FilterBar>
            <SearchField
              value={keyword}
              onChange={(next) => {
                setKeyword(next);
                paging.reset();
              }}
              placeholder="搜索单号、商品、双方账号 / 昵称 / 号码"
            />
            <TimeRangePicker
              value={range}
              onChange={(next) => {
                setRange(next);
                paging.reset();
              }}
            />
            <ResetButton
              visible={filtered}
              onClick={() => {
                setKeyword("");
                setRange({ preset: "all" });
                paging.reset();
              }}
            />
          </FilterBar>
        }
        footer={
          listQuery.data?.total ? (
            <Pager
              page={paging.page}
              limit={paging.limit}
              total={listQuery.data.total}
              onPage={paging.setPage}
              onLimit={paging.setLimit}
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
          minWidth={860}
          empty={
            <StateBlock
              icon={status === "reviewing" ? CheckCircle2 : Gift}
              title={status === "reviewing" && !filtered ? "没有待审核的赠送" : filtered ? "没有符合条件的赠送" : "暂无赠送记录"}
              description={status === "reviewing" && !filtered ? "风控判为可疑的赠送会出现在这里" : undefined}
            />
          }
        />
      </Section>

      <RecordSheet
        open={Boolean(detail)}
        onOpenChange={(open) => (!open ? setDetail(null) : undefined)}
        title="赠送详情"
        description={detail ? `${detail.giftNo}，${formatDateTime(detail.createdAt)}` : undefined}
        hero={detail ? <GiftHero gift={detail} userHref={userHref} /> : null}
        fields={
          detail
            ? [
                { label: "赠送单号", value: detail.giftNo, mono: true, copy: detail.giftNo },
                { label: "兑换单号", value: detail.redemptionNo, mono: true, copy: detail.redemptionNo || null },
                { label: "商品 ID", value: detail.itemId ? String(detail.itemId) : "已删除" },
                { label: "风控结论", value: detail.riskAction ? { allow: "放行", review: "送审核", reject: "拒绝" }[detail.riskAction] : undefined },
                { label: "风险决策", value: detail.decisionId, mono: true, copy: detail.decisionId || null },
                { label: "审核备注", value: detail.reviewNote, wide: true },
                { label: "审核人", value: detail.reviewedBy },
                { label: "审核时间", value: detail.reviewedAt ? formatDateTime(detail.reviewedAt) : undefined },
                { label: "撤销备注", value: detail.revokeNote, wide: true },
                { label: "撤销人", value: detail.revokedBy },
                { label: "撤销时间", value: detail.revokedAt ? formatDateTime(detail.revokedAt) : undefined },
                { label: "退积分流水", value: detail.refundTransactionNo, mono: true, copy: detail.refundTransactionNo || null },
                { label: "设备", value: detail.deviceId, mono: true, copy: detail.deviceId || null },
                { label: "IP", value: detail.clientIp, mono: true, copy: detail.clientIp || null }
              ]
            : undefined
        }
        raw={detail}
        actions={detail ? <GiftActions gift={detail} onRequest={setRequest} /> : undefined}
      >
        {detail ? (
          <div className="flex flex-col gap-4">
            <RiskBlock gift={detail} />
            <ResultList results={detail.results} title={detail.status === "reviewing" ? "通过后将发放" : "发放结果"} />
            {detail.revocation?.length ? (
              <div className="flex flex-col gap-2">
                <div className="text-xs font-medium text-muted-foreground">追回结果</div>
                <ul className="flex flex-col divide-y rounded-lg border">
                  {detail.revocation.map((item, index) => (
                    <li key={`${item.type}-${index}`} className="flex items-center justify-between gap-3 px-3 py-2 text-sm">
                      <span className="min-w-0 truncate">{item.detail || item.label}</span>
                      <Badge variant={item.outcome === "recovered" ? "success" : "secondary"} size="sm">
                        {REVOKE_OUTCOME[item.outcome] ?? item.outcome}
                      </Badge>
                    </li>
                  ))}
                </ul>
              </div>
            ) : null}
          </div>
        ) : null}
      </RecordSheet>

      <GiftHandleDialog
        appKey={appKey}
        request={request}
        onClose={() => setRequest(null)}
        onDone={(updated) => {
          if (detail && detail.id === updated.id) setDetail(updated);
        }}
      />
    </div>
  );
}

function PartyLink({ user, href }: { user: PointsStoreGiftUser; href?: string }) {
  const content = (
    <span className="flex min-w-0 flex-col">
      <span className="truncate font-medium group-hover:underline">{userName(user)}</span>
      {user.number ? <span className="truncate text-xs tabular-nums text-muted-foreground">{user.number}</span> : null}
    </span>
  );
  if (!href) return <span className="min-w-0 max-w-[140px]">{content}</span>;
  return (
    <Link href={href} onClick={(event) => event.stopPropagation()} className="group min-w-0 max-w-[140px]">
      {content}
    </Link>
  );
}

function GiftActions({
  gift,
  onRequest,
  compact = false
}: {
  gift: PointsStoreGift;
  onRequest: (request: GiftRequest) => void;
  compact?: boolean;
}) {
  const size = compact ? "xs" : "sm";
  if (gift.status === "reviewing") {
    return (
      <>
        <Button size={size} variant={compact ? "outline" : "default"} onClick={() => onRequest({ action: "approve", gift })}>
          {compact ? null : <CheckCircle2 />}
          通过
        </Button>
        <Button size={size} variant={compact ? "ghost" : "outline"} className={compact ? "text-muted-foreground" : undefined} onClick={() => onRequest({ action: "reject", gift })}>
          {compact ? null : <Ban />}
          驳回
        </Button>
      </>
    );
  }
  if (gift.status === "delivered") {
    return (
      <Button size={size} variant="outline" className="text-red-600 dark:text-red-400" onClick={() => onRequest({ action: "revoke", gift })}>
        {compact ? null : <Undo2 />}
        撤销
      </Button>
    );
  }
  return null;
}

function GiftHero({ gift, userHref }: { gift: PointsStoreGift; userHref: (user: PointsStoreGiftUser) => string | undefined }) {
  return (
    <div className="flex flex-col gap-3 rounded-xl border p-4">
      <div className="flex items-start justify-between gap-3">
        <div className="flex min-w-0 flex-col gap-1">
          <span className="truncate text-base font-semibold">{gift.itemTitle}</span>
          <GiftStatusBadge status={gift.status} />
        </div>
        <span className="shrink-0 text-xl font-semibold tabular-nums">
          {formatPoints(gift.cost)}
          <span className="ml-1 text-xs font-normal text-muted-foreground">积分</span>
        </span>
      </div>
      <div className="grid grid-cols-[1fr_auto_1fr] items-center gap-2 border-t pt-3 text-sm">
        <Party title="赠送人" user={gift.sender} href={userHref(gift.sender)} />
        <ArrowRight className="size-4 text-muted-foreground" />
        <Party title="接收人" user={gift.recipient} href={userHref(gift.recipient)} align="end" />
      </div>
      {gift.message ? (
        <p className="rounded-lg bg-muted/50 px-3 py-2 text-sm text-muted-foreground">
          <span className="mr-1 text-xs">赠言</span>
          {gift.message}
        </p>
      ) : null}
    </div>
  );
}

function Party({ title, user, href, align }: { title: string; user: PointsStoreGiftUser; href?: string; align?: "end" }) {
  const body = (
    <>
      <span className="text-xs text-muted-foreground">{title}</span>
      <span className="truncate font-medium">{userName(user)}</span>
      <span className="truncate text-xs text-muted-foreground">
        {[user.number ? `号码 ${user.number}` : "", user.userId ? `ID ${user.userId}` : ""].filter(Boolean).join("，")}
      </span>
    </>
  );
  const className = cn("flex min-w-0 flex-col", align === "end" && "items-end text-right");
  return href ? (
    <Link href={href} className={cn(className, "hover:underline")}>
      {body}
    </Link>
  ) : (
    <div className={className}>{body}</div>
  );
}

/** 命中的判据与当时的事实：管理员据此判断是不是同一个人的大小号 */
function RiskBlock({ gift }: { gift: PointsStoreGift }) {
  const facts = gift.riskFacts;
  return (
    <div className="flex flex-col gap-2">
      <div className="text-xs font-medium text-muted-foreground">风控</div>
      {gift.riskHits?.length ? (
        <ul className="flex flex-col divide-y rounded-lg border">
          {gift.riskHits.map((hit) => (
            <li key={hit.key} className="flex flex-col gap-0.5 px-3 py-2">
              <span className="flex items-center gap-2 text-sm font-medium">
                {hit.label}
                <Badge variant={ACTION_VARIANT[hit.action] ?? "warning"} size="sm">
                  {hit.action === "reject" ? "拒绝" : "审核"}
                </Badge>
              </span>
              {hit.detail ? <span className="text-xs text-muted-foreground">{hit.detail}</span> : null}
              <span className="text-xs text-muted-foreground">对用户：{hit.message}</span>
            </li>
          ))}
        </ul>
      ) : (
        <p className="rounded-lg border px-3 py-2 text-sm text-muted-foreground">没有命中任何判据</p>
      )}
      {facts ? (
        <dl className="grid grid-cols-2 gap-x-4 gap-y-1.5 rounded-lg bg-muted/40 px-3 py-2.5 text-xs">
          <Fact label="赠送人注册" value={`${facts.senderAgeDays} 天`} />
          <Fact label="接收人注册" value={`${facts.recipientAgeDays} 天`} />
          <Fact label="赠送人今日已送" value={`${facts.senderToday} 次 / ${formatPoints(facts.senderTodayPoints)} 积分`} />
          <Fact label="接收人今日已收" value={`${facts.recipientToday} 份`} />
          <Fact label="近期送过这个人" value={`${facts.pairRecent} 次`} />
          <Fact label="对方近期送过他" value={`${facts.mutualRecent} 次`} />
          <Fact label="共用设备" value={facts.sharedDevices?.length ? facts.sharedDevices.join("、") : "无"} wide />
          <Fact label="共用网络" value={facts.sharedNetworks?.length ? facts.sharedNetworks.join("、") : "无"} wide />
          {facts.platform ? (
            <Fact
              label="风险决策平台"
              value={`${facts.platform.action}，评分 ${facts.platform.score}${facts.platform.reasons?.length ? `：${facts.platform.reasons.join("；")}` : ""}`}
              wide
            />
          ) : null}
        </dl>
      ) : null}
    </div>
  );
}

function Fact({ label, value, wide }: { label: string; value: string; wide?: boolean }) {
  return (
    <div className={cn("flex min-w-0 flex-col", wide && "col-span-2")}>
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="break-all font-medium tabular-nums">{value}</dd>
    </div>
  );
}

function GiftHandleDialog({
  appKey,
  request,
  onClose,
  onDone
}: {
  appKey: string;
  request: GiftRequest;
  onClose: () => void;
  onDone?: (gift: PointsStoreGift) => void;
}) {
  const mutation = useHandlePointsStoreGiftMutation(appKey);
  const [note, setNote] = useState("");
  const [refund, setRefund] = useState(false);
  const [last, setLast] = useState<GiftRequest>(null);
  // 换了一笔就清空备注与退款选择（渲染期比对，不用 effect 同步）
  if (request !== last) {
    setLast(request);
    if (request) {
      setNote("");
      setRefund(false);
    }
  }
  const gift = request?.gift;
  const action = request?.action;
  const recipient = gift ? userName(gift.recipient) : "";
  const sender = gift ? userName(gift.sender) : "";

  const submit = async () => {
    if (!request) return;
    try {
      const updated = await mutation.mutateAsync({ giftId: request.gift.id, action: request.action, note: note.trim(), refundSender: refund });
      toast.success(
        action === "approve" ? "已通过，对方已收到" : action === "reject" ? "已驳回，积分已退回赠送人" : refund ? "已撤销并退回积分" : "已撤销，权益已追回"
      );
      onDone?.(updated);
      onClose();
    } catch (error) {
      toast.error(errorMessage(error, "操作失败"));
    }
  };

  return (
    <ConfirmActionDialog
      open={Boolean(request)}
      onOpenChange={(open) => (!open && !mutation.isPending ? onClose() : undefined)}
      title={action === "approve" ? "通过赠送" : action === "reject" ? "驳回赠送" : "撤销赠送"}
      description={
        gift
          ? action === "approve"
            ? `把「${gift.itemTitle}」发给 ${recipient}。发放前会复核对方的账号状态与是否已拥有，双方都会收到站内信。`
            : action === "reject"
              ? `驳回 ${sender} 送给 ${recipient} 的「${gift.itemTitle}」，${formatPoints(gift.cost)} 积分退回 ${sender}、库存加回。此操作不可撤销。`
              : `从 ${recipient} 处追回「${gift.itemTitle}」：会员扣掉还没走完的时长，功能解锁与装扮收回。双方都会收到站内信。此操作不可撤销。`
          : undefined
      }
      confirmLabel={action === "approve" ? "确认通过" : action === "reject" ? "确认驳回" : "确认撤销"}
      destructive={action !== "approve"}
      pending={mutation.isPending}
      note={{
        value: note,
        onChange: setNote,
        placeholder: action === "approve" ? "选填，附在给双方的站内信里" : "原因，会附在站内信里",
        hint: "写给用户看",
        max: 500
      }}
      onConfirm={submit}
    >
      {action === "revoke" && gift ? (
        <label className="flex items-center justify-between gap-3 rounded-lg border px-3 py-2.5">
          <span className="flex flex-col gap-0.5">
            <Label className="text-sm">退回 {formatPoints(gift.cost)} 积分给 {sender}</Label>
            <span className="text-xs text-muted-foreground">误送、客诉时退；套利、欺诈不退</span>
          </span>
          <Switch checked={refund} onCheckedChange={setRefund} aria-label="退回积分给赠送人" />
        </label>
      ) : null}
    </ConfirmActionDialog>
  );
}
