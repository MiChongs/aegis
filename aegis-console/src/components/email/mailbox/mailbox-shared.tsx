"use client";

import type { ReactNode } from "react";
import { AlertCircle, ChevronLeft, ChevronRight, Loader2, Paperclip } from "lucide-react";
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
import { Checkbox } from "@/components/ui/checkbox";
import { Skeleton } from "@/components/ui/skeleton";
import { ApiError } from "@/lib/api/client";
import { SKYMAIL_STATUS, type SkymailEmail } from "@/lib/api/email-skymail";
import { formatDateTimeMinute } from "@/lib/datetime";
import { cn } from "@/lib/utils";

/** 邮局各页共用的小部件：错误条、翻页、确认框、邮件表格。 */

export function errorText(error: unknown, fallback = "操作失败") {
  return error instanceof ApiError ? error.message : error instanceof Error ? error.message : fallback;
}

export function formatBytes(size: number) {
  if (!Number.isFinite(size) || size <= 0) return "—";
  if (size < 1024) return `${size} B`;
  if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KB`;
  return `${(size / 1024 / 1024).toFixed(1)} MB`;
}

export function ErrorNote({ error, onRetry }: { error: unknown; onRetry?: () => void }) {
  return (
    <div className="flex items-start gap-2 rounded-xl border border-destructive/30 bg-destructive/5 px-4 py-3 text-sm">
      <AlertCircle className="mt-0.5 size-4 shrink-0 text-destructive" />
      <div className="min-w-0 flex-1">
        <p className="break-words text-destructive">{errorText(error, "加载失败")}</p>
      </div>
      {onRetry ? (
        <Button size="sm" variant="outline" className="h-7" onClick={onRetry}>
          重试
        </Button>
      ) : null}
    </div>
  );
}

export function EmptyNote({ icon, title, hint }: { icon: ReactNode; title: string; hint?: string }) {
  return (
    <div className="flex flex-col items-center gap-2 py-16 text-center">
      <span className="text-muted-foreground/60">{icon}</span>
      <p className="text-sm text-muted-foreground">{title}</p>
      {hint ? <p className="text-xs text-muted-foreground">{hint}</p> : null}
    </div>
  );
}

export function TableSkeleton({ rows = 6 }: { rows?: number }) {
  return (
    <div className="space-y-2 p-4">
      {Array.from({ length: rows }).map((_, index) => (
        <Skeleton key={index} className="h-10 w-full" />
      ))}
    </div>
  );
}

/** 上一页 / 下一页。游标分页没有总页数，只能告诉用户「还有没有」。 */
export function Pager({
  label,
  canPrev,
  canNext,
  onPrev,
  onNext
}: {
  label?: string;
  canPrev: boolean;
  canNext: boolean;
  onPrev: () => void;
  onNext: () => void;
}) {
  if (!canPrev && !canNext) return null;
  return (
    <div className="flex items-center justify-end gap-2 text-xs text-muted-foreground">
      <Button size="icon" variant="outline" className="size-7" aria-label="上一页" disabled={!canPrev} onClick={onPrev}>
        <ChevronLeft className="size-3.5" />
      </Button>
      {label ? <span className="tabular-nums">{label}</span> : null}
      <Button size="icon" variant="outline" className="size-7" aria-label="下一页" disabled={!canNext} onClick={onNext}>
        <ChevronRight className="size-3.5" />
      </Button>
    </div>
  );
}

export function ConfirmDialog({
  open,
  title,
  description,
  confirmLabel,
  destructive,
  pending,
  onConfirm,
  onOpenChange
}: {
  open: boolean;
  title: string;
  description: ReactNode;
  confirmLabel: string;
  destructive?: boolean;
  pending?: boolean;
  onConfirm: () => void;
  onOpenChange: (open: boolean) => void;
}) {
  return (
    <AlertDialog open={open} onOpenChange={onOpenChange}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{title}</AlertDialogTitle>
          <AlertDialogDescription asChild>
            <div className="space-y-2 text-sm text-muted-foreground">{description}</div>
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>取消</AlertDialogCancel>
          <AlertDialogAction
            className={cn(destructive && "bg-destructive text-white hover:bg-destructive/90")}
            disabled={pending}
            onClick={(event) => {
              event.preventDefault();
              onConfirm();
            }}
          >
            {pending ? <Loader2 className="size-3.5 animate-spin" /> : null}
            {confirmLabel}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}

export function MailStatusBadge({ status }: { status: number }) {
  const meta = SKYMAIL_STATUS[status] ?? { label: `状态 ${status}`, tone: "outline" as const };
  return (
    <Badge variant={meta.tone} size="sm">
      {meta.label}
    </Badge>
  );
}

/**
 * 邮件列表。收件看发件人，发件看收件人；全部邮件多一列「所属用户」。
 * 选择框只在能写时出现（删除是唯一的批量操作）。
 */
export function MailTable({
  rows,
  mode,
  selected,
  onToggle,
  onToggleAll,
  onOpen
}: {
  rows: SkymailEmail[];
  mode: "inbox" | "sent" | "all";
  selected?: Set<number>;
  onToggle?: (id: number) => void;
  onToggleAll?: (checked: boolean) => void;
  onOpen: (email: SkymailEmail) => void;
}) {
  const selectable = Boolean(selected && onToggle);
  const allChecked = selectable && rows.length > 0 && rows.every((row) => selected?.has(row.emailId));
  return (
    <div className="overflow-x-auto">
      <table className="w-full text-sm">
        <thead className="border-b bg-muted/40 text-xs text-muted-foreground">
          <tr>
            {selectable ? (
              <th className="w-10 px-4 py-2.5">
                <Checkbox
                  checked={allChecked}
                  aria-label="全选本页"
                  onCheckedChange={(value) => onToggleAll?.(value === true)}
                />
              </th>
            ) : null}
            <th className="px-3 py-2.5 text-left font-medium">{mode === "sent" ? "收件人" : "发件人"}</th>
            {mode === "all" ? <th className="px-3 py-2.5 text-left font-medium">收件人</th> : null}
            <th className="px-3 py-2.5 text-left font-medium">主题</th>
            {mode === "all" ? <th className="px-3 py-2.5 text-left font-medium">所属用户</th> : null}
            <th className="px-3 py-2.5 text-left font-medium">状态</th>
            <th className="px-3 py-2.5 text-left font-medium">时间</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((row) => {
            const unread = mode !== "sent" && row.type === 0 && row.unread === 0;
            const counterpart = mode === "sent" ? row.toEmail : row.name ? `${row.name} <${row.sendEmail}>` : row.sendEmail;
            return (
              <tr
                key={row.emailId}
                className={cn(
                  "cursor-pointer border-b transition-colors last:border-b-0 hover:bg-muted/40",
                  row.isDel === 1 && "opacity-60"
                )}
                onClick={() => onOpen(row)}
              >
                {selectable ? (
                  <td className="px-4 py-2.5" onClick={(event) => event.stopPropagation()}>
                    <Checkbox
                      checked={selected?.has(row.emailId)}
                      aria-label="选择"
                      onCheckedChange={() => onToggle?.(row.emailId)}
                    />
                  </td>
                ) : null}
                <td className={cn("max-w-[14rem] truncate px-3 py-2.5 text-xs", unread && "font-semibold")} title={counterpart}>
                  {counterpart || "—"}
                </td>
                {mode === "all" ? (
                  <td className="max-w-[12rem] truncate px-3 py-2.5 text-xs text-muted-foreground" title={row.toEmail}>
                    {row.toEmail || "—"}
                  </td>
                ) : null}
                <td className="max-w-[22rem] px-3 py-2.5">
                  <div className="flex items-center gap-1.5">
                    {unread ? <span className="size-1.5 shrink-0 rounded-full bg-primary" aria-label="未读" /> : null}
                    <span className={cn("truncate", unread && "font-semibold")}>{row.subject || "（无主题）"}</span>
                    {row.attList?.length ? <Paperclip className="size-3 shrink-0 text-muted-foreground" /> : null}
                  </div>
                  {row.text ? <p className="truncate text-[11px] text-muted-foreground">{row.text.slice(0, 120)}</p> : null}
                </td>
                {mode === "all" ? (
                  <td className="max-w-[12rem] truncate px-3 py-2.5 text-xs text-muted-foreground">{row.userEmail || "—"}</td>
                ) : null}
                <td className="whitespace-nowrap px-3 py-2.5">
                  <MailStatusBadge status={row.status} />
                  {row.isDel === 1 ? (
                    <Badge variant="outline" size="sm" className="ml-1">
                      已删除
                    </Badge>
                  ) : null}
                </td>
                <td className="whitespace-nowrap px-3 py-2.5 text-xs tabular-nums text-muted-foreground">
                  {formatDateTimeMinute(row.createTime)}
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}
