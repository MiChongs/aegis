"use client";

import { useState } from "react";
import { AnimatePresence, motion } from "motion/react";
import { Ban, BellRing, Loader2, Undo2, X } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle
} from "@/components/ui/dialog";
import {
  BanDurationField,
  BanLevelField,
  BanNoticeField,
  BanPublicReasonField,
  BanReasonCodeField,
  BanReasonField,
  BanScopeField,
  describeBanDraft,
  useBanDraft,
  useNow
} from "@/components/users/ban-form";
import { ApiError } from "@/lib/api-client";
import { BulkNotifyDialog } from "@/components/users/app-users/bulk-notify-dialog";
import {
  useBatchBanAppUsersMutation,
  useBatchUpdateAppUserStatusMutation
} from "@/lib/app-user-hooks";
import { cn } from "@/lib/utils";

/**
 * 批量操作条。
 *
 * 后端三个批量端点此前前端一个都没接。这里全部按「选中的 userId 列表」下发，
 * 不用「按当前筛选条件批量」那种写法 —— 后者在管理员看到的列表与实际执行
 * 之间存在时间差（翻页期间有人注册、有人改状态），会误伤到没被看过的账号。
 *
 * 处置只有「封禁」一个入口：旧版另有「限制」按钮翻 users.enabled，后端早把它
 * 映射成永久封禁，却不让人填原因和时长，留着只会让人以为是两种处置。
 * 「解除封禁」走状态接口，逐个解除选中账号生效中的封禁（含旧版的受限）。
 */
export function BulkActionBar({
  appKey,
  selectedIds,
  onClear
}: {
  appKey: string | null;
  selectedIds: number[];
  onClear: () => void;
}) {
  const statusMutation = useBatchUpdateAppUserStatusMutation(appKey);
  const [showBan, setShowBan] = useState(false);
  const [showNotify, setShowNotify] = useState(false);

  const count = selectedIds.length;

  async function liftBans() {
    try {
      const result = await statusMutation.mutateAsync({
        userIds: selectedIds,
        enabled: true,
        clearDisabledEndTime: true
      });
      toast.success(`已解除 ${result?.updated ?? count} 个账号的封禁`);
      onClear();
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "操作失败");
    }
  }

  return (
    <>
      <AnimatePresence>
        {count > 0 ? (
          <motion.div
            initial={{ y: 16, opacity: 0 }}
            animate={{ y: 0, opacity: 1 }}
            exit={{ y: 16, opacity: 0 }}
            transition={{ duration: 0.16, ease: "easeOut" }}
            className={cn(
              "sticky bottom-4 z-20 mx-auto flex w-fit max-w-full flex-wrap items-center gap-1.5",
              "rounded-full border bg-popover px-3 py-2 shadow-lg"
            )}
          >
            <span className="px-1 text-xs tabular-nums text-muted-foreground">
              已选 <span className="font-semibold text-foreground">{count}</span> 项
            </span>
            <Button
              size="sm"
              variant="ghost"
              className="h-7 gap-1 text-xs text-destructive hover:text-destructive"
              onClick={() => setShowBan(true)}
            >
              <Ban className="size-3" />
              封禁
            </Button>
            <Button
              size="sm"
              variant="ghost"
              className="h-7 gap-1 text-xs"
              disabled={statusMutation.isPending}
              onClick={() => void liftBans()}
            >
              {statusMutation.isPending ? (
                <Loader2 className="size-3 animate-spin" />
              ) : (
                <Undo2 className="size-3" />
              )}
              解除封禁
            </Button>
            <Button
              size="sm"
              variant="ghost"
              className="h-7 gap-1 text-xs"
              onClick={() => setShowNotify(true)}
            >
              <BellRing className="size-3" />
              发通知
            </Button>
            <Button
              size="icon"
              variant="ghost"
              className="size-7"
              aria-label="取消选择"
              onClick={onClear}
            >
              <X className="size-3.5" />
            </Button>
          </motion.div>
        ) : null}
      </AnimatePresence>

      <BulkBanDialog
        open={showBan}
        onOpenChange={setShowBan}
        appKey={appKey}
        selectedIds={selectedIds}
        onDone={onClear}
      />
      <BulkNotifyDialog
        open={showNotify}
        onOpenChange={setShowNotify}
        appKey={appKey}
        selectedIds={selectedIds}
        onDone={onClear}
      />
    </>
  );
}

// ── 批量封禁 ──────────────────────────

function BulkBanDialog({
  open,
  onOpenChange,
  appKey,
  selectedIds,
  onDone
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  appKey: string | null;
  selectedIds: number[];
  onDone: () => void;
}) {
  const mutation = useBatchBanAppUsersMutation(appKey);
  const draft = useBanDraft();
  const now = useNow();
  const problem = draft.validate(now);

  async function submit() {
    const submitAt = Date.now();
    const error = draft.validate(submitAt);
    if (error) {
      toast.error(error);
      return;
    }
    try {
      const result = await mutation.mutateAsync({ userIds: selectedIds, ...draft.toPayload(submitAt) });
      const failed = result.failed ?? 0;
      toast.success(`已封禁 ${result.created ?? selectedIds.length} 个账号`, {
        description: failed ? `${failed} 个失败` : undefined
      });
      draft.reset();
      onOpenChange(false);
      onDone();
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "批量封禁失败");
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>批量封禁 {selectedIds.length} 个账号</DialogTitle>
          <DialogDescription>
            已有生效封禁的账号会被新的封禁替代。
            {draft.level === "platform" ? "选择全站时，每个账号的主人在所有应用的账号都会被封。" : ""}
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-5">
          <BanDurationField draft={draft} now={now} />
          <BanScopeField draft={draft} />
          <BanLevelField draft={draft} />
          <BanReasonCodeField draft={draft} />
          <BanReasonField draft={draft} rows={3} />
          <BanPublicReasonField draft={draft} rows={2} />
          <BanNoticeField draft={draft} />
        </div>

        <DialogFooter className="items-center gap-3 sm:justify-between">
          <span className="text-xs text-muted-foreground">{problem ?? describeBanDraft(draft)}</span>
          <div className="flex gap-2">
            <Button size="sm" variant="ghost" onClick={() => onOpenChange(false)}>
              取消
            </Button>
            <Button size="sm" variant="destructive" disabled={Boolean(problem) || mutation.isPending} onClick={submit}>
              {mutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : <Ban className="size-3.5" />}
              确认封禁
            </Button>
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
