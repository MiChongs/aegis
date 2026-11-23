"use client";

import { useState, type ReactNode } from "react";
import { AlertTriangle, RefreshCw, Trash2, type LucideIcon } from "lucide-react";
import { notify } from "@/lib/notify";
import { ApiError } from "@/lib/api/client";
import type { TicketKind, TicketPriority } from "@/lib/api/tickets";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle
} from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";

// 工单配置各分节共用的骨架：分节头、紧凑列表行、加载 / 空 / 错误三态、删除确认。

export const PRIORITIES: TicketPriority[] = ["urgent", "high", "normal", "low"];
export const KINDS: TicketKind[] = ["ticket", "feedback"];
export const WEEKDAYS = ["一", "二", "三", "四", "五", "六", "日"];

export const PRIORITY_TONE: Record<TicketPriority, string> = {
  urgent: "bg-red-500",
  high: "bg-orange-500",
  normal: "bg-sky-500",
  low: "bg-slate-400"
};

export type AssignStrategy = "manual" | "round_robin" | "least_open";

export const STRATEGY_LABEL: Record<AssignStrategy, string> = {
  manual: "手动指派",
  round_robin: "轮询分派",
  least_open: "最少待办优先"
};

export const STRATEGY_HINT: Record<AssignStrategy, string> = {
  manual: "新工单进组后等待组长或成员手动认领",
  round_robin: "按成员顺序依次分派",
  least_open: "分给当前未结工单最少的成员"
};

export function reportError(error: unknown, fallback: string) {
  notify.error(error instanceof ApiError ? error.message : fallback);
}

/** 分钟数转成「30 分钟 / 2 小时 / 1.5 天」 */
export function formatMinutes(value?: number) {
  if (value === undefined || value === null || Number.isNaN(value)) return "—";
  if (value <= 0) return "不限";
  if (value < 60) return `${value} 分钟`;
  if (value < 1440) return `${trimNumber(value / 60)} 小时`;
  return `${trimNumber(value / 1440)} 天`;
}

function trimNumber(value: number) {
  return Number.isInteger(value) ? String(value) : value.toFixed(1).replace(/\.0$/, "");
}

/** 分节头：标题、说明、右侧操作 */
export function SectionHeader({
  title,
  description,
  action
}: {
  title: string;
  description?: string;
  action?: ReactNode;
}) {
  return (
    <div className="flex flex-wrap items-start justify-between gap-3">
      <div className="min-w-0 space-y-0.5">
        <h2 className="text-base font-semibold tracking-tight text-foreground">{title}</h2>
        {description ? <p className="text-sm text-muted-foreground">{description}</p> : null}
      </div>
      {action ? <div className="flex shrink-0 items-center gap-2">{action}</div> : null}
    </div>
  );
}

/** 列表容器：一组紧凑行共用一个边框，行之间用分隔线 */
export function EntityList({ children, className }: { children: ReactNode; className?: string }) {
  return <div className={cn("divide-y overflow-hidden rounded-xl border bg-card", className)}>{children}</div>;
}

export function EntityGroupLabel({ children, count }: { children: ReactNode; count?: number }) {
  return (
    <div className="flex items-center gap-2 px-1 pb-1.5 pt-1 text-xs font-medium text-muted-foreground">
      {children}
      {count !== undefined ? <span className="tabular-nums">{count}</span> : null}
    </div>
  );
}

/** 平台级 / 停用这类状态标记，各分节一致 */
export function ScopeBadges({ platform, enabled }: { platform?: boolean; enabled?: boolean }) {
  return (
    <>
      {platform ? (
        <Badge variant="secondary" size="sm">
          平台级
        </Badge>
      ) : null}
      {enabled === false ? (
        <Badge variant="outline" size="sm" className="text-muted-foreground">
          已停用
        </Badge>
      ) : null}
    </>
  );
}

export function ListSkeleton({ rows = 4 }: { rows?: number }) {
  return (
    <div className="divide-y overflow-hidden rounded-xl border bg-card" aria-busy="true" aria-label="加载中">
      {Array.from({ length: rows }, (_, index) => (
        <div key={index} className="flex items-center gap-3 px-4 py-3.5">
          <Skeleton className="size-9 rounded-lg" />
          <div className="flex-1 space-y-2">
            <Skeleton className="h-3.5 w-40" />
            <Skeleton className="h-3 w-64 max-w-full" />
          </div>
          <Skeleton className="h-7 w-16 rounded-md" />
        </div>
      ))}
    </div>
  );
}

export function SectionEmpty({
  icon: Icon,
  title,
  description,
  action
}: {
  icon: LucideIcon;
  title: string;
  description?: string;
  action?: ReactNode;
}) {
  return (
    <Empty className="rounded-xl border border-dashed bg-card/50 py-10">
      <EmptyHeader>
        <EmptyMedia variant="icon">
          <Icon />
        </EmptyMedia>
        <EmptyTitle>{title}</EmptyTitle>
        {description ? <EmptyDescription>{description}</EmptyDescription> : null}
      </EmptyHeader>
      {action ? <EmptyContent>{action}</EmptyContent> : null}
    </Empty>
  );
}

export function SectionError({ error, onRetry }: { error: unknown; onRetry: () => void }) {
  return (
    <div className="flex flex-wrap items-center gap-3 rounded-xl border border-destructive/30 bg-destructive/5 px-4 py-3 text-sm">
      <AlertTriangle className="size-4 shrink-0 text-destructive" />
      <span className="min-w-0 flex-1 text-foreground">
        {error instanceof ApiError ? error.message : "加载失败，请稍后重试"}
      </span>
      <Button size="sm" variant="outline" onClick={onRetry}>
        <RefreshCw />
        重试
      </Button>
    </div>
  );
}

/** 删除按钮 + 二次确认 */
export function DeleteButton({
  title,
  description,
  onConfirm,
  pending
}: {
  title: string;
  description: string;
  onConfirm: () => Promise<void> | void;
  pending?: boolean;
}) {
  const [open, setOpen] = useState(false);
  return (
    <>
      <Tooltip>
        <TooltipTrigger asChild>
          <Button
            size="icon-sm"
            variant="ghost"
            aria-label="删除"
            className="text-muted-foreground hover:text-destructive"
            onClick={() => setOpen(true)}
          >
            <Trash2 />
          </Button>
        </TooltipTrigger>
        <TooltipContent>删除</TooltipContent>
      </Tooltip>
      <AlertDialog open={open} onOpenChange={setOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{title}</AlertDialogTitle>
            <AlertDialogDescription>{description}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction
              disabled={pending}
              className="bg-destructive text-white hover:bg-destructive/90"
              onClick={async (event) => {
                event.preventDefault();
                await onConfirm();
                setOpen(false);
              }}
            >
              删除
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}

/** 表单里的一项：标签 + 控件 + 说明 */
export function Field({
  label,
  hint,
  children,
  className
}: {
  label: string;
  hint?: ReactNode;
  children: ReactNode;
  className?: string;
}) {
  return (
    <div className={cn("space-y-1.5", className)}>
      <Label className="text-xs font-medium text-muted-foreground">{label}</Label>
      {children}
      {hint ? <p className="text-[11px] leading-4 text-muted-foreground">{hint}</p> : null}
    </div>
  );
}

/** 开关行：说明在左、开关在右 */
export function SwitchRow({
  title,
  description,
  control
}: {
  title: string;
  description?: string;
  control: ReactNode;
}) {
  return (
    <div className="flex items-center justify-between gap-4 rounded-lg border px-3 py-2.5">
      <div className="min-w-0">
        <p className="text-sm text-foreground">{title}</p>
        {description ? <p className="text-xs text-muted-foreground">{description}</p> : null}
      </div>
      {control}
    </div>
  );
}

/** 一周七格：亮的是计时日 */
export function WeekStrip({ days, className }: { days: number[]; className?: string }) {
  return (
    <div className={cn("flex gap-0.5", className)}>
      {WEEKDAYS.map((label, index) => {
        const on = days.includes(index + 1);
        return (
          <span
            key={label}
            className={cn(
              "flex size-5 items-center justify-center rounded text-[10px] font-medium",
              on ? "bg-primary text-primary-foreground" : "bg-muted text-muted-foreground/70"
            )}
          >
            {label}
          </span>
        );
      })}
    </div>
  );
}
