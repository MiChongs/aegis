"use client";

import { useState } from "react";
import { CalendarClock, CheckCircle2, Loader2, OctagonAlert, Undo2, XCircle } from "lucide-react";
import { toast } from "sonner";
import { SwitchRow } from "@/components/apps/app-config-primitives";
import { modeLabel } from "@/components/apps/account-deletion/account-deletion-shared";
import { useNow } from "@/components/users/ban-form";
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
import { Textarea } from "@/components/ui/textarea";
import { ApiError } from "@/lib/api/client";
import type { DeletionRequest } from "@/lib/api/account-deletion";
import { useDeletionActionMutation } from "@/lib/account-deletion-hooks";
import { formatDateTimeMinute, fromLocalInputValue, toLocalInputValue } from "@/lib/datetime";
import { cn } from "@/lib/utils";

export type DeletionActionKind = "approve" | "reject" | "cancel" | "execute" | "reschedule";

const NOTE_MAX = 500;
const DAY = 86_400_000;
const YEAR = 365 * DAY;

const REJECT_PRESETS = ["存在未处理完的订单或退款", "账号存在安全风险，请联系客服处理", "申请信息不完整，请重新提交"];
const RESCHEDULE_PRESETS = [
  { label: "1 天后", days: 1 },
  { label: "7 天后", days: 7 },
  { label: "15 天后", days: 15 },
  { label: "30 天后", days: 30 }
];

/**
 * 对一条申请的五种处置：批准、驳回、撤销、立即执行、改期。
 *
 * 表单状态挂在按「动作 + 申请」取键的内层组件上，换一条申请或换一个动作即是一份新表单，
 * 不需要 effect 去重置。处置结果按返回的状态 toast：立即执行与批准立即注销都可能以 failed 收场，
 * 那时后端返回 200 而状态是 failed，原因在 failure 里。
 */
export function DeletionActionDialog({
  appKey,
  request,
  kind,
  onOpenChange,
  onDone
}: {
  appKey: string;
  request: DeletionRequest | null;
  kind: DeletionActionKind | null;
  onOpenChange: (open: boolean) => void;
  onDone?: (result: DeletionRequest) => void;
}) {
  const open = Boolean(request && kind);
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        {request && kind ? (
          <ActionForm
            key={`${kind}-${request.id}`}
            appKey={appKey}
            request={request}
            kind={kind}
            onClose={() => onOpenChange(false)}
            onDone={onDone}
          />
        ) : null}
      </DialogContent>
    </Dialog>
  );
}

function ActionForm({
  appKey,
  request,
  kind,
  onClose,
  onDone
}: {
  appKey: string;
  request: DeletionRequest;
  kind: DeletionActionKind;
  onClose: () => void;
  onDone?: (result: DeletionRequest) => void;
}) {
  const now = useNow(15_000);
  const mutation = useDeletionActionMutation(appKey);
  const [note, setNote] = useState("");
  const [notify, setNotify] = useState(true);
  const [confirm, setConfirm] = useState("");
  const [scheduledAt, setScheduledAt] = useState(() => toLocalInputValue(request.scheduledAt));

  const immediateApproval = kind === "approve" && request.mode === "immediate";
  const confirmMatched = confirm.trim() === request.account;
  const scheduledIso = fromLocalInputValue(scheduledAt);
  const scheduledMs = scheduledIso ? new Date(scheduledIso).getTime() : Number.NaN;
  const scheduleProblem =
    kind !== "reschedule"
      ? null
      : !scheduledIso
        ? "请选择注销时间"
        : scheduledMs <= now + 60_000
          ? "注销时间必须晚于现在"
          : scheduledMs > now + YEAR
            ? "注销时间不能晚于一年之后"
            : null;

  const blocked =
    mutation.isPending ||
    (kind === "reject" && !note.trim()) ||
    (kind === "execute" && !confirmMatched) ||
    (kind === "reschedule" && Boolean(scheduleProblem));

  async function submit() {
    if (blocked) return;
    try {
      const id = request.id;
      const result = await mutation.mutateAsync(
        kind === "approve"
          ? { kind, id, note: note.trim() }
          : kind === "reject"
            ? { kind, id, note: note.trim() }
            : kind === "cancel"
              ? { kind, id, note: note.trim(), notify }
              : kind === "execute"
                ? { kind, id, confirm: confirm.trim() }
                : { kind, id, scheduledAt: scheduledIso as string, notify }
      );
      if (result.status === "failed") {
        toast.error(result.failure ? `执行失败：${result.failure}` : "执行失败");
      } else if (result.status === "completed") {
        toast.success(`账号 ${request.account} 已注销`);
      } else {
        toast.success(SUCCESS_TEXT[kind]);
      }
      onClose();
      onDone?.(result);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "操作失败");
    }
  }

  const header = HEADER[kind];
  const description =
    kind === "approve"
      ? immediateApproval
        ? "批准后立即注销账号，无法恢复。"
        : request.coolingDays
          ? `批准后进入 ${request.coolingDays} 天冷静期，期满自动注销。`
          : "批准后进入冷静期，期满自动注销。"
      : header.description;

  return (
    <>
      <DialogHeader>
        <div className="flex items-center gap-3">
          <span
            className={cn(
              "grid size-9 shrink-0 place-items-center rounded-xl ring-1 ring-inset",
              kind === "execute" || immediateApproval
                ? "bg-red-500/10 text-red-600 ring-red-500/20 dark:text-red-400"
                : "bg-muted text-muted-foreground ring-border"
            )}
          >
            <header.icon className="size-4" />
          </span>
          <div className="min-w-0 space-y-1 text-left">
            <DialogTitle>{header.title}</DialogTitle>
            <DialogDescription>{description}</DialogDescription>
          </div>
        </div>
      </DialogHeader>

      <div className="rounded-xl border bg-muted/30 px-3 py-2.5 text-xs leading-5">
        <div className="flex flex-wrap items-baseline justify-between gap-2">
          <span className="font-medium">{request.nickname || request.account}</span>
          <span className="font-mono text-muted-foreground">{request.account}</span>
        </div>
        <div className="text-muted-foreground">
          {modeLabel(request.mode)}
          {request.scheduledAt ? `，预定 ${formatDateTimeMinute(request.scheduledAt)} 注销` : ""}
        </div>
      </div>

      {kind === "approve" || kind === "cancel" ? (
        <NoteField label="说明（选填）" value={note} onChange={setNote} />
      ) : null}

      {kind === "reject" ? (
        <div className="space-y-2">
          <NoteField label="驳回理由" value={note} onChange={setNote} required />
          <div className="flex flex-wrap gap-1.5">
            {REJECT_PRESETS.map((preset) => (
              <button
                key={preset}
                type="button"
                onClick={() => setNote(preset)}
                className="rounded-md border border-border bg-muted px-2 py-0.5 text-[11px] text-muted-foreground transition-colors hover:text-foreground"
              >
                {preset}
              </button>
            ))}
          </div>
        </div>
      ) : null}

      {kind === "cancel" || kind === "reschedule" ? (
        <SwitchRow label="通知用户" checked={notify} onChange={setNotify} />
      ) : null}

      {kind === "reschedule" ? (
        <div className="space-y-2">
          <Label className="text-xs text-muted-foreground">注销时间</Label>
          <Input
            type="datetime-local"
            value={scheduledAt}
            min={toLocalInputValue(now + 60_000)}
            max={toLocalInputValue(now + YEAR)}
            onChange={(event) => setScheduledAt(event.target.value)}
            className="font-mono text-xs tabular-nums"
          />
          <div className="flex flex-wrap gap-1.5">
            {RESCHEDULE_PRESETS.map((preset) => (
              <button
                key={preset.days}
                type="button"
                onClick={() => setScheduledAt(toLocalInputValue(now + preset.days * DAY))}
                className="rounded-md border border-border bg-muted px-2 py-0.5 text-[11px] text-muted-foreground transition-colors hover:text-foreground"
              >
                {preset.label}
              </button>
            ))}
          </div>
          {scheduleProblem && scheduledAt ? (
            <p className="text-xs text-red-600 dark:text-red-400">{scheduleProblem}</p>
          ) : null}
        </div>
      ) : null}

      {kind === "execute" ? <ExecuteConfirm account={request.account} value={confirm} onChange={setConfirm} /> : null}

      {immediateApproval ? (
        <p className="rounded-xl border border-red-200 bg-red-50/60 px-3 py-2 text-xs leading-5 text-red-700 dark:border-red-900/60 dark:bg-red-950/30 dark:text-red-300">
          账号及其资产、会话与云端数据将被永久删除。
        </p>
      ) : null}

      <DialogFooter>
        <Button size="sm" variant="ghost" onClick={onClose}>
          返回
        </Button>
        <Button
          size="sm"
          variant={kind === "execute" || immediateApproval || kind === "reject" ? "destructive" : "default"}
          disabled={blocked}
          onClick={() => void submit()}
        >
          {mutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : <header.icon className="size-3.5" />}
          {header.confirm}
        </Button>
      </DialogFooter>
    </>
  );
}

const HEADER: Record<
  DeletionActionKind,
  { title: string; description: string; confirm: string; icon: typeof CheckCircle2 }
> = {
  approve: { title: "批准注销申请", description: "", confirm: "批准", icon: CheckCircle2 },
  reject: { title: "驳回注销申请", description: "驳回理由会展示给用户。", confirm: "驳回", icon: XCircle },
  cancel: { title: "撤销注销申请", description: "撤销后账号保留，用户可重新申请。", confirm: "撤销申请", icon: Undo2 },
  execute: {
    title: "立即注销账号",
    description: "不再等待冷静期，当场删除账号，无法恢复。",
    confirm: "立即注销",
    icon: OctagonAlert
  },
  reschedule: {
    title: "调整注销时间",
    description: "到期提醒按新的时间重新计算。",
    confirm: "保存",
    icon: CalendarClock
  }
};

const SUCCESS_TEXT: Record<DeletionActionKind, string> = {
  approve: "已批准，进入冷静期",
  reject: "已驳回",
  cancel: "已撤销",
  execute: "已提交执行",
  reschedule: "注销时间已调整"
};

function NoteField({
  label,
  value,
  onChange,
  required
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  required?: boolean;
}) {
  return (
    <div className="space-y-1.5">
      <div className="flex items-baseline justify-between gap-2">
        <Label className="text-xs text-muted-foreground">
          {label}
          {required ? <span className="ml-0.5 text-red-500">*</span> : null}
        </Label>
        <span className="text-[10px] tabular-nums text-muted-foreground">
          {value.length}/{NOTE_MAX}
        </span>
      </div>
      <Textarea
        rows={3}
        value={value}
        maxLength={NOTE_MAX}
        onChange={(event) => onChange(event.target.value)}
        className="resize-none text-sm"
      />
    </div>
  );
}

/** 输入账号名确认：危险操作的最后一道闸，账号名逐字一致才放行 */
export function ExecuteConfirm({
  account,
  value,
  onChange
}: {
  account: string;
  value: string;
  onChange: (value: string) => void;
}) {
  const matched = value.trim() === account;
  return (
    <div className="space-y-2.5 rounded-xl border border-red-200 bg-red-50/60 p-3 dark:border-red-900/60 dark:bg-red-950/30">
      <p className="text-xs leading-5 text-red-700 dark:text-red-300">
        账号及其资产、会话与云端数据将被永久删除，无法恢复。
      </p>
      <div className="space-y-1.5">
        <Label className="text-xs text-red-700 dark:text-red-300">
          输入账号名 <span className="font-mono font-semibold select-all">{account}</span> 确认
        </Label>
        <Input
          value={value}
          autoComplete="off"
          spellCheck={false}
          onChange={(event) => onChange(event.target.value)}
          aria-invalid={Boolean(value) && !matched}
          className="bg-background font-mono text-sm"
        />
      </div>
    </div>
  );
}
