"use client";

import { useState } from "react";
import { AnimatePresence, motion } from "motion/react";
import { Ban, BellRing, CircleCheck, Loader2, ShieldX, X } from "lucide-react";
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
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue
} from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { ApiError } from "@/lib/api-client";
import {
  useBatchBanAppUsersMutation,
  useBatchUpdateAppUserStatusMutation,
  useBulkNotifyAppUsersMutation
} from "@/lib/app-user-hooks";
import type { AccountBanScope, AccountBanType } from "@/lib/api/types";
import { cn } from "@/lib/utils";

/**
 * 批量操作条。
 *
 * 后端三个批量端点此前前端一个都没接。这里全部按「选中的 userId 列表」下发，
 * 不用「按当前筛选条件批量」那种写法 —— 后者在管理员看到的列表与实际执行
 * 之间存在时间差（翻页期间有人注册、有人改状态），会误伤到没被看过的账号。
 *
 * 「限制」与「封禁」分成两个按钮而不是合并：前者写 users.enabled 开关位、
 * 立即生效但不留处置记录；后者写 app_user_bans、有起止有操作人可申诉。
 * 合成一个按钮就等于替管理员做了这个选择。
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

  async function toggleStatus(enabled: boolean) {
    try {
      await statusMutation.mutateAsync({
        userIds: selectedIds,
        enabled,
        disabledReason: enabled ? undefined : "控制台批量限制",
        clearDisabledEndTime: enabled
      });
      toast.success(enabled ? `已启用 ${count} 个账号` : `已限制 ${count} 个账号`);
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
              "rounded-full border bg-popover/95 px-3 py-2 shadow-lg backdrop-blur"
            )}
          >
            <span className="px-1 text-xs tabular-nums text-muted-foreground">
              已选 <span className="font-semibold text-foreground">{count}</span> 项
            </span>
            <Button
              size="sm"
              variant="ghost"
              className="h-7 gap-1 text-xs"
              disabled={statusMutation.isPending}
              onClick={() => void toggleStatus(true)}
            >
              {statusMutation.isPending ? (
                <Loader2 className="size-3 animate-spin" />
              ) : (
                <CircleCheck className="size-3" />
              )}
              启用
            </Button>
            <Button
              size="sm"
              variant="ghost"
              className="h-7 gap-1 text-xs"
              disabled={statusMutation.isPending}
              onClick={() => void toggleStatus(false)}
            >
              <ShieldX className="size-3" />
              限制
            </Button>
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

function toRFC3339(local: string) {
  if (!local) return undefined;
  const date = new Date(local);
  return Number.isNaN(date.getTime()) ? undefined : date.toISOString();
}

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
  const [banType, setBanType] = useState<AccountBanType>("temporary");
  const [banScope, setBanScope] = useState<AccountBanScope>("login");
  const [reason, setReason] = useState("");
  const [endAt, setEndAt] = useState("");

  const needsEnd = banType === "temporary";
  const canSubmit = reason.trim().length > 0 && (!needsEnd || Boolean(endAt));

  async function submit() {
    try {
      const result = await mutation.mutateAsync({
        userIds: selectedIds,
        banType,
        banScope,
        reason: reason.trim(),
        endAt: needsEnd ? toRFC3339(endAt) : null
      });
      const failed = result.failed ?? 0;
      toast.success(`已封禁 ${result.created ?? selectedIds.length} 个账号`, {
        description: failed ? `${failed} 个失败，可在各自详情页查看` : undefined
      });
      setReason("");
      setEndAt("");
      onOpenChange(false);
      onDone();
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "批量封禁失败");
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle className="text-sm">批量封禁 {selectedIds.length} 个账号</DialogTitle>
          <DialogDescription>
            会为每个账号各建一条封禁记录（有起止、有操作人、可撤销、可申诉）。
            已有生效封禁的账号会叠加一条，不会被替换。
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-3">
          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-1.5">
              <Label className="text-xs text-muted-foreground">类型</Label>
              <RadioGroup
                value={banType}
                onValueChange={(value) => setBanType(value as AccountBanType)}
                className="gap-1.5"
              >
                {[
                  { value: "temporary", label: "临时", hint: "到期自动解除" },
                  { value: "permanent", label: "永久", hint: "需人工撤销" }
                ].map((option) => (
                  <label
                    key={option.value}
                    className="flex cursor-pointer items-center gap-2 rounded-lg border px-2.5 py-1.5"
                  >
                    <RadioGroupItem value={option.value} />
                    <span className="text-xs">
                      {option.label}
                      <span className="ml-1 text-muted-foreground">{option.hint}</span>
                    </span>
                  </label>
                ))}
              </RadioGroup>
            </div>
            <div className="space-y-1.5">
              <Label className="text-xs text-muted-foreground">范围</Label>
              <RadioGroup
                value={banScope}
                onValueChange={(value) => setBanScope(value as AccountBanScope)}
                className="gap-1.5"
              >
                {[
                  { value: "login", label: "仅登录", hint: "现有会话保留" },
                  { value: "all", label: "全部访问", hint: "同时踢下线" }
                ].map((option) => (
                  <label
                    key={option.value}
                    className="flex cursor-pointer items-center gap-2 rounded-lg border px-2.5 py-1.5"
                  >
                    <RadioGroupItem value={option.value} />
                    <span className="text-xs">
                      {option.label}
                      <span className="ml-1 text-muted-foreground">{option.hint}</span>
                    </span>
                  </label>
                ))}
              </RadioGroup>
            </div>
          </div>

          {needsEnd ? (
            <div className="space-y-1.5">
              <Label className="text-xs text-muted-foreground">到期时间</Label>
              <Input
                type="datetime-local"
                value={endAt}
                className="h-8 text-xs"
                onChange={(event) => setEndAt(event.target.value)}
              />
            </div>
          ) : null}

          <div className="space-y-1.5">
            <Label className="text-xs text-muted-foreground">封禁原因（必填）</Label>
            <Textarea
              rows={3}
              value={reason}
              placeholder="例如：同一注册 IP 批量注册小号"
              onChange={(event) => setReason(event.target.value)}
            />
          </div>
        </div>

        <DialogFooter>
          <Button size="sm" variant="ghost" onClick={() => onOpenChange(false)}>
            取消
          </Button>
          <Button size="sm" variant="destructive" disabled={!canSubmit || mutation.isPending} onClick={submit}>
            {mutation.isPending ? <Loader2 className="mr-1 size-3 animate-spin" /> : null}
            确认封禁
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// ── 批量站内信 ──────────────────────────

const NOTIFY_LEVELS = [
  { value: "info", label: "普通" },
  { value: "warning", label: "提醒" },
  { value: "critical", label: "重要" }
];

function BulkNotifyDialog({
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
  const mutation = useBulkNotifyAppUsersMutation(appKey);
  const [title, setTitle] = useState("");
  const [content, setContent] = useState("");
  const [level, setLevel] = useState("info");

  const canSubmit = title.trim().length > 0 && content.trim().length > 0;

  async function submit() {
    try {
      await mutation.mutateAsync({
        userIds: selectedIds,
        type: "system",
        title: title.trim(),
        content: content.trim(),
        level
      });
      toast.success(`已向 ${selectedIds.length} 个用户发送站内信`);
      setTitle("");
      setContent("");
      onOpenChange(false);
      onDone();
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "发送失败");
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle className="text-sm">给 {selectedIds.length} 个用户发站内信</DialogTitle>
          <DialogDescription>
            写入应用用户的站内信收件箱，用户在客户端里可见。这不是管理员通知，也不走邮件。
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-3">
          <div className="space-y-1.5">
            <Label className="text-xs text-muted-foreground">标题</Label>
            <Input
              value={title}
              className="h-8 text-xs"
              placeholder="例如：账号安全提醒"
              onChange={(event) => setTitle(event.target.value)}
            />
          </div>
          <div className="space-y-1.5">
            <Label className="text-xs text-muted-foreground">正文</Label>
            <Textarea
              rows={4}
              value={content}
              placeholder="正文内容"
              onChange={(event) => setContent(event.target.value)}
            />
          </div>
          <div className="space-y-1.5">
            <Label className="text-xs text-muted-foreground">级别</Label>
            <Select value={level} onValueChange={setLevel}>
              <SelectTrigger size="sm" className="h-8 w-32 text-xs">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {NOTIFY_LEVELS.map((item) => (
                  <SelectItem key={item.value} value={item.value} className="text-xs">
                    {item.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        </div>

        <DialogFooter>
          <Button size="sm" variant="ghost" onClick={() => onOpenChange(false)}>
            取消
          </Button>
          <Button size="sm" disabled={!canSubmit || mutation.isPending} onClick={submit}>
            {mutation.isPending ? <Loader2 className="mr-1 size-3 animate-spin" /> : null}
            发送
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
