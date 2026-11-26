"use client";

import { useState } from "react";
import {
  ArrowDownUp,
  CheckCircle2,
  Flag,
  Loader2,
  RefreshCw,
  Search,
  Trash2,
  UserCheck,
  X,
  XCircle
} from "lucide-react";
import { notify } from "@/lib/notify";
import { ApiError } from "@/lib/api/client";
import { useAuthStore } from "@/lib/auth-store";
import { useBulkTicketsMutation } from "@/lib/ticket-hooks";
import type { TicketBulkPayload, TicketPriority, TicketStatus } from "@/lib/api/tickets";
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
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Textarea } from "@/components/ui/textarea";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import { Kbd, PRIORITY_LABEL, STATUS_LABEL, StatusDot } from "@/components/tickets/ticket-shared";
import { useDesk } from "./desk-context";
import { DESK_SORTS, DESK_SORT_LABEL, type DeskSort, type DeskState, type SetDeskState } from "./desk-state";

// 收件箱的顶部两种形态：平时是「搜索 + 筛选 + 排序」，勾选了几条之后换成批量操作条。

export function SearchField({
  state,
  setState,
  searchRef
}: {
  state: DeskState;
  setState: SetDeskState;
  searchRef: React.RefObject<HTMLInputElement | null>;
}) {
  const { noun, person } = useDesk();
  return (
    <div className="relative min-w-0 flex-1">
      <Search className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
      <input
        ref={searchRef}
        value={state.q}
        onChange={(event) => void setState({ q: event.target.value })}
        onKeyDown={(event) => {
          if (event.key === "Escape") {
            void setState({ q: "" });
            event.currentTarget.blur();
          }
        }}
        placeholder={`搜索标题、编号、${person}、内容`}
        aria-label={`搜索${noun}`}
        className="h-8 w-full rounded-lg bg-muted/60 pl-8 pr-8 text-sm outline-none transition-colors placeholder:text-muted-foreground focus-visible:bg-background focus-visible:ring-2 focus-visible:ring-ring/40"
      />
      {state.q ? (
        <button
          type="button"
          onClick={() => void setState({ q: "" })}
          aria-label="清空搜索"
          className="absolute right-2 top-1/2 -translate-y-1/2 rounded p-0.5 text-muted-foreground transition-colors hover:text-foreground"
        >
          <X className="size-3.5" />
        </button>
      ) : (
        <Kbd className="pointer-events-none absolute right-2 top-1/2 -translate-y-1/2">/</Kbd>
      )}
    </div>
  );
}

export function SortMenu({ state, setState }: { state: DeskState; setState: SetDeskState }) {
  return (
    <DropdownMenu>
      <Tooltip>
        <TooltipTrigger asChild>
          <DropdownMenuTrigger asChild>
            <Button size="icon-sm" variant="ghost" aria-label="排序">
              <ArrowDownUp />
            </Button>
          </DropdownMenuTrigger>
        </TooltipTrigger>
        <TooltipContent>排序：{DESK_SORT_LABEL[state.sort]}</TooltipContent>
      </Tooltip>
      <DropdownMenuContent align="end" className="w-40">
        <DropdownMenuLabel className="text-xs text-muted-foreground">排序</DropdownMenuLabel>
        <DropdownMenuRadioGroup value={state.sort} onValueChange={(value) => void setState({ sort: value as DeskSort })}>
          {DESK_SORTS.map((value) => (
            <DropdownMenuRadioItem key={value} value={value}>
              {DESK_SORT_LABEL[value]}
            </DropdownMenuRadioItem>
          ))}
        </DropdownMenuRadioGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

export function RefreshButton({ spinning, onClick }: { spinning: boolean; onClick: () => void }) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button size="icon-xs" variant="ghost" onClick={onClick} aria-label="刷新">
          <RefreshCw className={cn(spinning && "animate-spin")} />
        </Button>
      </TooltipTrigger>
      <TooltipContent>刷新</TooltipContent>
    </Tooltip>
  );
}

// ─────────────── 批量操作 ───────────────

// 「不采纳」要填理由，单独走确认框；「关闭」是单独的按钮
const BULK_STATUSES: TicketStatus[] = ["processing", "pending", "resolved"];
const BULK_PRIORITIES: TicketPriority[] = ["urgent", "high", "normal", "low"];

/**
 * 批量操作条。逐条成败由后端判定（每条都按各自的权限与状态机走），
 * 部分失败时把失败条数与第一条原因说出来，不让人以为全成功了。
 */
export function BulkBar({
  selected,
  loaded,
  onSelectAll,
  onClear
}: {
  selected: number[];
  loaded: number;
  onSelectAll: () => void;
  onClear: () => void;
}) {
  const { noun, person } = useDesk();
  const operatorId = useAuthStore((state) => Number(state.operator?.id) || 0);
  const bulkMut = useBulkTicketsMutation();
  const [confirm, setConfirm] = useState<"close" | "reject" | "delete" | null>(null);
  const [rejectReason, setRejectReason] = useState("");
  const count = selected.length;
  const all = count >= loaded && loaded > 0;

  const run = async (payload: Omit<TicketBulkPayload, "ids">, done: string) => {
    try {
      const result = await bulkMut.mutateAsync({ ids: selected, ...payload });
      const failed = result.failed ?? [];
      if (failed.length === 0) notify.success(`${done}：${result.succeeded} 条`);
      else if (result.succeeded > 0) notify.warning(`成功 ${result.succeeded} 条，失败 ${failed.length} 条：${failed[0].reason}`);
      else notify.error(`全部失败：${failed[0]?.reason ?? "未知原因"}`);
      onClear();
    } catch (error) {
      notify.error(error instanceof ApiError ? error.message : "批量操作失败");
    }
  };

  return (
    <div className="flex h-8 min-w-0 flex-1 items-center gap-1">
      <Checkbox
        checked={all ? true : count > 0 ? "indeterminate" : false}
        onCheckedChange={() => (all ? onClear() : onSelectAll())}
        aria-label={all ? "取消全选" : "全选已加载的"}
        className="ml-1.5 mr-1"
      />
      <span className="mr-auto shrink-0 text-sm font-medium tabular-nums">
        已选 {count}
        {bulkMut.isPending ? <Loader2 className="ml-1.5 inline size-3.5 animate-spin text-muted-foreground" /> : null}
      </span>

      {operatorId ? (
        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              size="icon-sm"
              variant="ghost"
              disabled={bulkMut.isPending}
              onClick={() => void run({ action: "assign", assigneeAdminId: operatorId }, "已指派给你")}
              aria-label="指派给我"
            >
              <UserCheck />
            </Button>
          </TooltipTrigger>
          <TooltipContent>指派给我</TooltipContent>
        </Tooltip>
      ) : null}

      <DropdownMenu>
        <Tooltip>
          <TooltipTrigger asChild>
            <DropdownMenuTrigger asChild>
              <Button size="icon-sm" variant="ghost" disabled={bulkMut.isPending} aria-label="改状态">
                <CheckCircle2 />
              </Button>
            </DropdownMenuTrigger>
          </TooltipTrigger>
          <TooltipContent>改状态</TooltipContent>
        </Tooltip>
        <DropdownMenuContent align="end" className="w-40">
          <DropdownMenuLabel className="text-xs text-muted-foreground">改为</DropdownMenuLabel>
          {BULK_STATUSES.map((status) => (
            <DropdownMenuItem key={status} onSelect={() => void run({ action: "status", status }, `已改为「${STATUS_LABEL[status]}」`)}>
              <StatusDot status={status} className="mx-0.5" />
              {STATUS_LABEL[status]}
            </DropdownMenuItem>
          ))}
          <DropdownMenuItem onSelect={() => setConfirm("reject")}>
            <StatusDot status="rejected" className="mx-0.5" />
            {STATUS_LABEL.rejected}
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>

      <DropdownMenu>
        <Tooltip>
          <TooltipTrigger asChild>
            <DropdownMenuTrigger asChild>
              <Button size="icon-sm" variant="ghost" disabled={bulkMut.isPending} aria-label="改优先级">
                <Flag />
              </Button>
            </DropdownMenuTrigger>
          </TooltipTrigger>
          <TooltipContent>改优先级</TooltipContent>
        </Tooltip>
        <DropdownMenuContent align="end" className="w-36">
          <DropdownMenuLabel className="text-xs text-muted-foreground">优先级</DropdownMenuLabel>
          {BULK_PRIORITIES.map((priority) => (
            <DropdownMenuItem
              key={priority}
              onSelect={() => void run({ action: "priority", priority }, `优先级已改为「${PRIORITY_LABEL[priority]}」`)}
            >
              {PRIORITY_LABEL[priority]}
            </DropdownMenuItem>
          ))}
        </DropdownMenuContent>
      </DropdownMenu>

      <Tooltip>
        <TooltipTrigger asChild>
          <Button size="icon-sm" variant="ghost" disabled={bulkMut.isPending} onClick={() => setConfirm("close")} aria-label="关闭">
            <XCircle />
          </Button>
        </TooltipTrigger>
        <TooltipContent>关闭</TooltipContent>
      </Tooltip>
      <Tooltip>
        <TooltipTrigger asChild>
          <Button
            size="icon-sm"
            variant="ghost"
            disabled={bulkMut.isPending}
            onClick={() => setConfirm("delete")}
            aria-label="删除"
            className="text-destructive hover:text-destructive"
          >
            <Trash2 />
          </Button>
        </TooltipTrigger>
        <TooltipContent>删除</TooltipContent>
      </Tooltip>
      <span aria-hidden className="mx-0.5 h-4 w-px bg-border" />
      <Tooltip>
        <TooltipTrigger asChild>
          <Button size="icon-sm" variant="ghost" onClick={onClear} aria-label="取消选择">
            <X />
          </Button>
        </TooltipTrigger>
        <TooltipContent>
          取消选择 <Kbd className="ml-1">Esc</Kbd>
        </TooltipContent>
      </Tooltip>

      <AlertDialog
        open={confirm !== null}
        onOpenChange={(open) => {
          if (open) return;
          setConfirm(null);
          setRejectReason("");
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {confirm === "delete"
                ? `删除选中的 ${count} 条${noun}？`
                : confirm === "reject"
                  ? `不采纳选中的 ${count} 条${noun}？`
                  : `关闭选中的 ${count} 条${noun}？`}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {confirm === "delete"
                ? "会话与附件一并删除，无法恢复。没有删除权限的条目会被跳过。"
                : confirm === "reject"
                  ? `每位${person}都会收到通知并看到同一条理由；之后不能再回复，可以重新打开。没有改状态权限的条目会被跳过。`
                  : `关闭后${person}不能再回复，可以重新打开。没有关闭权限的条目会被跳过。`}
            </AlertDialogDescription>
          </AlertDialogHeader>
          {confirm === "reject" ? (
            <Textarea
              autoFocus
              rows={3}
              value={rejectReason}
              onChange={(event) => setRejectReason(event.target.value)}
              placeholder={`不采纳理由，选填，${person}可见`}
              aria-label="不采纳理由"
            />
          ) : null}
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction
              className={cn(confirm === "delete" && "bg-destructive text-white hover:bg-destructive/90")}
              onClick={() => {
                const action = confirm;
                const reason = rejectReason.trim();
                setConfirm(null);
                setRejectReason("");
                if (action === "delete") void run({ action: "delete" }, "已删除");
                else if (action === "close") void run({ action: "close" }, "已关闭");
                else if (action === "reject")
                  void run({ action: "status", status: "rejected", reason: reason || undefined }, `已改为「${STATUS_LABEL.rejected}」`);
              }}
            >
              {confirm === "delete" ? "删除" : confirm === "reject" ? "确认不采纳" : "关闭"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
