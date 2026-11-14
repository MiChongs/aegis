"use client";

import { useState, type ReactNode } from "react";
import Link from "next/link";
import {
  AlertTriangle,
  BellRing,
  CalendarClock,
  CheckCircle2,
  CircleDashed,
  CircleSlash,
  Gavel,
  Hourglass,
  OctagonAlert,
  Send,
  ShieldCheck,
  ShieldX,
  Undo2,
  UserX,
  XCircle
} from "lucide-react";
import {
  DeletionActionDialog,
  type DeletionActionKind
} from "@/components/apps/account-deletion/account-deletion-action-dialog";
import {
  DeletionStatusBadge,
  GUARD_LEVEL_META,
  availableActions,
  cancelledByLabel,
  modeLabel,
  reasonText,
  sourceLabel,
  verifiedByLabel
} from "@/components/apps/account-deletion/account-deletion-shared";
import { Fact, Facts, formatMoney } from "@/components/users/detail/user-detail-shared";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Skeleton } from "@/components/ui/skeleton";
import type { DeletionConfig, DeletionRequest } from "@/lib/api/account-deletion";
import { useDeletionRequestQuery } from "@/lib/account-deletion-hooks";
import { formatDate, formatDateTimeMinute, formatRelative, parseInstant } from "@/lib/datetime";
import { cn } from "@/lib/utils";

/**
 * 一条申请的详情抽屉：进度、申请信息、提交时的账号快照与前置条件结论、验证方式、设备与网络，
 * 底部按状态给出处置按钮（无写权限时不出现）。
 *
 * 列表里那一行作为占位数据先渲染，详情请求回来再替换；处置之后整组失效，抽屉里的状态跟着刷新。
 * 账号已注销时（userExists 为假）不再链到用户详情。
 */
export function DeletionRequestSheet({
  appKey,
  request,
  open,
  onOpenChange,
  config,
  canReview,
  canExecute,
  onCompleted
}: {
  appKey: string;
  /** 关闭后仍保留上一条，关闭动画期间内容不会闪空 */
  request: DeletionRequest | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  config?: DeletionConfig | null;
  /** 撤销、改期、驳回、批准 */
  canReview: boolean;
  /** 立即执行 */
  canExecute: boolean;
  /** 账号在这里被注销之后（立即执行、批准立即注销） */
  onCompleted?: (result: DeletionRequest) => void;
}) {
  const detailQuery = useDeletionRequestQuery(appKey, open ? request?.id : null, request);
  const item = detailQuery.data ?? request;
  const [action, setAction] = useState<DeletionActionKind | null>(null);
  const available = availableActions(item?.status ?? "");
  const actions = {
    approve: canReview && available.approve,
    reject: canReview && available.reject,
    cancel: canReview && available.cancel,
    reschedule: canReview && available.reschedule,
    execute: canExecute && available.execute
  };
  const hasActions = Object.values(actions).some(Boolean);

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent side="right" className="flex w-full flex-col gap-0 p-0 sm:max-w-xl">
        <SheetHeader className="shrink-0 border-b px-5 py-4 sm:px-6">
          <SheetTitle className="flex flex-wrap items-center gap-2 pr-8">
            <span className="truncate">{item ? item.nickname || item.account : "注销申请"}</span>
            {item ? <DeletionStatusBadge status={item.status} /> : null}
          </SheetTitle>
          <SheetDescription className="font-mono text-xs">
            {item ? `${item.account}，申请 #${item.id}` : ""}
          </SheetDescription>
        </SheetHeader>

        <ScrollArea className="min-h-0 flex-1">
          {item ? (
            <div className="space-y-6 px-5 py-5 sm:px-6">
              <Headline item={item} />
              <Section title="进度">
                <Timeline item={item} />
              </Section>
              <Section title="申请信息">
                <Facts>
                  <Fact label="注销方式" value={modeLabel(item.mode)} />
                  {item.mode === "cooling" ? (
                    <Fact label="冷静期" value={item.coolingDays ? `${item.coolingDays} 天` : "—"} />
                  ) : null}
                  <Fact label="来源" value={sourceLabel(item.source)} />
                  {item.operator ? <Fact label="发起人" value={item.operator} /> : null}
                  <Fact label="原因" value={reasonText(item, config) || "未填写"} tone={reasonText(item, config) ? "default" : "muted"} />
                  <Fact
                    label="验证方式"
                    value={
                      item.verifiedBy?.length ? (
                        <span className="inline-flex flex-wrap justify-end gap-1">
                          {item.verifiedBy.map((method) => (
                            <Badge key={method} variant="outline" size="sm" className="font-normal">
                              {verifiedByLabel(method)}
                            </Badge>
                          ))}
                        </span>
                      ) : item.source === "admin" ? (
                        "管理员发起，无需验证"
                      ) : (
                        "—"
                      )
                    }
                  />
                  <Fact label="提交时间" value={formatDateTimeMinute(item.createdAt)} />
                </Facts>
              </Section>
              <Section title="提交时的账号">
                <SnapshotFacts appKey={appKey} item={item} />
              </Section>
              <Section title="前置条件">
                <ChecksList item={item} />
              </Section>
              <Section title="设备与网络">
                <Facts>
                  <Fact label="IP" value={item.ip} mono />
                  <Fact label="设备" value={item.device} />
                  <Fact label="设备 ID" value={item.deviceId} mono />
                  <Fact
                    label="User-Agent"
                    value={item.userAgent ? <span className="text-xs leading-5">{item.userAgent}</span> : ""}
                  />
                </Facts>
              </Section>
            </div>
          ) : (
            <div className="space-y-3 px-6 py-5">
              {[0, 1, 2, 3].map((index) => (
                <Skeleton key={index} className="h-16 w-full rounded-xl" />
              ))}
            </div>
          )}
        </ScrollArea>

        {item && hasActions ? (
          <div className="flex shrink-0 flex-wrap items-center justify-end gap-2 border-t px-5 py-3 sm:px-6">
            {actions.cancel ? (
              <Button size="sm" variant="ghost" onClick={() => setAction("cancel")}>
                <Undo2 className="size-3.5" /> 撤销
              </Button>
            ) : null}
            {actions.reschedule ? (
              <Button size="sm" variant="outline" onClick={() => setAction("reschedule")}>
                <CalendarClock className="size-3.5" /> 改期
              </Button>
            ) : null}
            {actions.reject ? (
              <Button size="sm" variant="outline" onClick={() => setAction("reject")}>
                <XCircle className="size-3.5" /> 驳回
              </Button>
            ) : null}
            {actions.approve ? (
              <Button size="sm" onClick={() => setAction("approve")}>
                <CheckCircle2 className="size-3.5" /> 批准
              </Button>
            ) : null}
            {actions.execute ? (
              <Button size="sm" variant="destructive" onClick={() => setAction("execute")}>
                <OctagonAlert className="size-3.5" /> 立即执行
              </Button>
            ) : null}
          </div>
        ) : null}

        <DeletionActionDialog
          appKey={appKey}
          request={action ? item : null}
          kind={action}
          onOpenChange={(next) => !next && setAction(null)}
          onDone={(result) => {
            if (result.status === "completed") onCompleted?.(result);
          }}
        />
      </SheetContent>
    </Sheet>
  );
}

function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="space-y-2">
      <h3 className="text-[11px] font-semibold tracking-[0.14em] text-muted-foreground">{title}</h3>
      {children}
    </section>
  );
}

/** 顶部一句话结论：冷静期中给出倒计时，失败给出原因，结束的给出结局 */
function Headline({ item }: { item: DeletionRequest }) {
  if (item.status === "failed") {
    return (
      <div className="flex gap-3 rounded-xl border border-red-200 bg-red-50/60 px-4 py-3 dark:border-red-900/60 dark:bg-red-950/30">
        <AlertTriangle className="mt-0.5 size-4 shrink-0 text-red-600 dark:text-red-400" />
        <div className="min-w-0 space-y-0.5">
          <div className="text-sm font-medium text-red-700 dark:text-red-300">
            执行失败{item.attempts > 1 ? `，已尝试 ${item.attempts} 次` : ""}
          </div>
          <p className="break-words text-xs leading-5 text-red-700/90 dark:text-red-300/90">{item.failure || "未记录原因"}</p>
        </div>
      </div>
    );
  }
  if (item.status === "scheduled" && item.scheduledAt) {
    return (
      <div className="flex items-center gap-3 rounded-xl border border-sky-200 bg-sky-50/60 px-4 py-3 dark:border-sky-900/60 dark:bg-sky-950/30">
        <Hourglass className="size-4 shrink-0 text-sky-600 dark:text-sky-400" />
        <div className="min-w-0 flex-1">
          <div className="text-xs text-muted-foreground">预定注销时间</div>
          <div className="text-sm font-semibold tabular-nums">{formatDateTimeMinute(item.scheduledAt)}</div>
        </div>
        <span className="shrink-0 text-sm font-medium tabular-nums text-sky-700 dark:text-sky-300">
          {formatRelative(item.scheduledAt)}
        </span>
      </div>
    );
  }
  if (item.status === "reviewing") {
    return (
      <div className="flex items-center gap-3 rounded-xl border border-amber-200 bg-amber-50/60 px-4 py-3 dark:border-amber-900/60 dark:bg-amber-950/30">
        <Gavel className="size-4 shrink-0 text-amber-600 dark:text-amber-400" />
        <div className="text-sm">
          等待审核
          {item.mode === "cooling" && item.coolingDays ? (
            <span className="text-muted-foreground">，批准后进入 {item.coolingDays} 天冷静期</span>
          ) : (
            <span className="text-muted-foreground">，批准后立即注销</span>
          )}
        </div>
      </div>
    );
  }
  return null;
}

type TimelineEvent = {
  key: string;
  icon: typeof Send;
  tone: "default" | "success" | "danger" | "info" | "warning" | "pending";
  title: string;
  at?: string;
  detail?: string;
};

function buildTimeline(item: DeletionRequest): TimelineEvent[] {
  const events: TimelineEvent[] = [
    {
      key: "created",
      icon: Send,
      tone: "default",
      title: item.source === "admin" ? `管理员${item.operator ? ` ${item.operator} ` : ""}发起注销` : "用户提交申请",
      at: item.createdAt
    }
  ];
  if (item.reviewedAt) {
    const rejected = item.status === "rejected";
    events.push({
      key: "reviewed",
      icon: rejected ? ShieldX : ShieldCheck,
      tone: rejected ? "warning" : "success",
      title: `${item.reviewedBy || "管理员"}${rejected ? "驳回" : "批准"}`,
      at: item.reviewedAt,
      detail: item.reviewNote
    });
  }
  if (item.remindedAt) {
    events.push({ key: "reminded", icon: BellRing, tone: "info", title: "已发送到期提醒", at: item.remindedAt });
  }
  if (item.cancelledAt) {
    events.push({
      key: "cancelled",
      icon: Undo2,
      tone: "success",
      title:
        item.cancelledBy === "login"
          ? "冷静期内登录，自动撤销"
          : `${cancelledByLabel(item.cancelledBy) || "已"}撤销`,
      at: item.cancelledAt,
      detail: item.cancelReason
    });
  }
  if (item.status === "processing") {
    events.push({ key: "processing", icon: CircleDashed, tone: "info", title: "正在执行", at: item.updatedAt });
  }
  if (item.status === "failed") {
    events.push({ key: "failed", icon: CircleSlash, tone: "danger", title: "执行失败", at: item.updatedAt, detail: item.failure });
  }
  if (item.status === "scheduled" && item.scheduledAt) {
    events.push({ key: "scheduled", icon: Hourglass, tone: "pending", title: "预定注销", at: item.scheduledAt });
  }
  if (item.status === "reviewing") {
    events.push({ key: "review", icon: Gavel, tone: "pending", title: "等待审核" });
  }
  if (item.completedAt) {
    events.push({ key: "completed", icon: UserX, tone: "danger", title: "账号已注销", at: item.completedAt });
  }
  return events;
}

const TONE_CLASS: Record<TimelineEvent["tone"], string> = {
  default: "bg-muted text-muted-foreground",
  success: "bg-emerald-500/10 text-emerald-600 dark:text-emerald-400",
  danger: "bg-red-500/10 text-red-600 dark:text-red-400",
  info: "bg-sky-500/10 text-sky-600 dark:text-sky-400",
  warning: "bg-amber-500/10 text-amber-600 dark:text-amber-400",
  pending: "border border-dashed border-border bg-background text-muted-foreground"
};

function Timeline({ item }: { item: DeletionRequest }) {
  const events = buildTimeline(item);
  return (
    <ol className="rounded-xl border px-4 py-3.5">
      {events.map((event, index) => {
        const last = index === events.length - 1;
        return (
          <li key={event.key} className={cn("relative flex gap-3", !last && "pb-4")}>
            {!last ? <span aria-hidden className="absolute top-7 bottom-0 left-3 w-px bg-border" /> : null}
            <span className={cn("relative grid size-6 shrink-0 place-items-center rounded-full", TONE_CLASS[event.tone])}>
              <event.icon className="size-3" />
            </span>
            <div className="min-w-0 flex-1 pt-0.5">
              <div className="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-0.5">
                <span className={cn("text-sm", event.tone === "pending" && "text-muted-foreground")}>{event.title}</span>
                {event.at ? (
                  <span className="text-xs tabular-nums text-muted-foreground">{formatDateTimeMinute(event.at)}</span>
                ) : null}
              </div>
              {event.detail ? (
                <p className="mt-1 rounded-lg bg-muted/50 px-2.5 py-1.5 text-xs leading-5 break-words text-muted-foreground">
                  {event.detail}
                </p>
              ) : null}
            </div>
          </li>
        );
      })}
    </ol>
  );
}

function SnapshotFacts({ appKey, item }: { appKey: string; item: DeletionRequest }) {
  const snap = item.snapshot ?? {};
  const vipExpire = parseInstant(snap.vipExpireAt);
  const vip = snap.lifetime ? "永久会员" : vipExpire ? `${formatDate(vipExpire)} 到期` : "无";
  return (
    <Facts>
      <Fact
        label="用户 ID"
        mono
        value={
          item.userExists ? (
            <Link
              href={`/app-users/${encodeURIComponent(appKey)}/${item.userId}`}
              className="underline-offset-4 hover:underline"
            >
              {item.userId}
            </Link>
          ) : (
            <span className="inline-flex items-center gap-1.5">
              {item.userId}
              <Badge variant="secondary" size="sm">
                已注销
              </Badge>
            </span>
          )
        }
      />
      <Fact label="邮箱" value={snap.email} mono />
      <Fact label="手机号" value={snap.phone} mono />
      <Fact label="注册时间" value={formatDateTimeMinute(snap.registeredAt, "")} />
      <Fact label="会员" value={vip} tone={vip === "无" ? "muted" : "default"} />
      <Fact label="钱包余额" value={snap.wallet !== undefined ? formatMoney(snap.wallet) : ""} mono />
      <Fact label="积分" value={snap.points !== undefined ? snap.points.toLocaleString("zh-CN") : ""} />
      <Fact
        label="登录密码"
        value={snap.hasPassword === undefined ? "" : snap.hasPassword ? "已设置" : "未设置"}
      />
      <Fact label="两步验证" value={snap.totp === undefined ? "" : snap.totp ? "已开启" : "未开启"} />
    </Facts>
  );
}

function ChecksList({ item }: { item: DeletionRequest }) {
  const checks = item.snapshot?.checks ?? [];
  if (!checks.length) {
    return <p className="rounded-xl border border-dashed px-4 py-5 text-center text-xs text-muted-foreground">未检查前置条件</p>;
  }
  return (
    <ul className="divide-y rounded-xl border">
      {checks.map((check) => {
        const level = GUARD_LEVEL_META[check.level] ?? GUARD_LEVEL_META.off;
        return (
          <li key={check.key} className="flex items-start gap-3 px-4 py-2.5">
            {check.passed ? (
              <CheckCircle2 className="mt-0.5 size-4 shrink-0 text-emerald-600 dark:text-emerald-400" />
            ) : check.level === "block" ? (
              <XCircle className="mt-0.5 size-4 shrink-0 text-red-600 dark:text-red-400" />
            ) : (
              <AlertTriangle className="mt-0.5 size-4 shrink-0 text-amber-600 dark:text-amber-400" />
            )}
            <div className="min-w-0 flex-1">
              <div className="flex flex-wrap items-center gap-1.5">
                <span className="text-sm">{check.title}</span>
                {!check.passed ? (
                  <Badge variant={level.variant} size="sm">
                    {level.label}
                  </Badge>
                ) : null}
              </div>
              {check.message ? <p className="mt-0.5 text-xs leading-5 text-muted-foreground">{check.message}</p> : null}
            </div>
          </li>
        );
      })}
    </ul>
  );
}
