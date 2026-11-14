"use client";

import { useState, type ComponentType } from "react";
import Link from "next/link";
import {
  ArrowUpRight,
  CalendarClock,
  Check,
  CheckCircle2,
  Gavel,
  Hourglass,
  Loader2,
  OctagonAlert,
  Undo2,
  UserX,
  XCircle,
  Zap
} from "lucide-react";
import { toast } from "sonner";
import {
  DeletionActionDialog,
  ExecuteConfirm,
  type DeletionActionKind
} from "@/components/apps/account-deletion/account-deletion-action-dialog";
import { DeletionRequestSheet } from "@/components/apps/account-deletion/account-deletion-request-sheet";
import {
  DeletionStatusBadge,
  MODE_META,
  availableActions,
  modeLabel,
  reasonText,
  sourceLabel
} from "@/components/apps/account-deletion/account-deletion-shared";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import type { DeletionConfig, DeletionMode, DeletionRequest } from "@/lib/api/account-deletion";
import {
  useCreateUserDeletionMutation,
  useDeletionCatalogQuery,
  useUserDeletionQuery
} from "@/lib/account-deletion-hooks";
import { formatDateTimeMinute, formatRelative } from "@/lib/datetime";
import { useAppKeyPermissionChecker } from "@/lib/app-permission-hooks";
import { cn } from "@/lib/utils";
import { InfoGrid, RecordTable, Section, StateBlock, ToneIcon, errorMessage, type Column, type Tone } from "./kit";

const REASON_MAX = 500;
/** 说明的常用写法，点一下填入；与应用配置的原因（reasonCode）互不冲突 */
const ADMIN_NOTE_PRESETS = ["用户通过客服申请注销", "账号长期未使用", "违规账号清理"];

/**
 * 用户详情「处置」页签里的账号注销区块。
 *
 * 有进行中的申请时展示状态机进度与快捷处置（审核、撤销、改期、立即执行）；没有时提供「发起注销」：
 * 管理员代发起不过身份验证与前置条件（结论仍记进快照），立即注销要输入账号名确认。
 * 历史申请列在下方，点开看完整详情。账号在这里被注销之后用户已不存在，交给 `onUserDeleted` 跳回列表。
 */
export function UserAccountDeletionCard({
  appKey,
  userId,
  account,
  onUserDeleted
}: {
  appKey: string;
  userId: number;
  account: string;
  onUserDeleted: () => void;
}) {
  const can = useAppKeyPermissionChecker(appKey);
  // 发起注销走 /users/:id/account-deletion（删除与注销）；处置进行中的申请走申请接口（审核 / 立即执行）
  const canCreate = can("app:user:delete");
  const canReview = can("app:user:deletion:review");
  const canExecute = can("app:user:deletion:execute");
  const viewQuery = useUserDeletionQuery(appKey, userId);
  const catalogQuery = useDeletionCatalogQuery(appKey);
  const view = viewQuery.data;
  const open = view?.open ?? null;
  const history = (view?.history ?? []).filter((item) => item.id !== open?.id);

  const [sheetItem, setSheetItem] = useState<DeletionRequest | null>(null);
  const [sheetOpen, setSheetOpen] = useState(false);
  const [action, setAction] = useState<DeletionActionKind | null>(null);

  const showSheet = (item: DeletionRequest) => {
    setSheetItem(item);
    setSheetOpen(true);
  };
  const handleDone = (result: DeletionRequest) => {
    if (result.status === "completed") onUserDeleted();
  };

  const historyColumns: Array<Column<DeletionRequest>> = [
    { id: "status", header: "状态", width: "100px", cell: (item) => <DeletionStatusBadge status={item.status} /> },
    {
      id: "mode",
      header: "方式",
      width: "120px",
      cell: (item) => (
        <span className="text-xs">
          {modeLabel(item.mode)}
          <span className="text-muted-foreground">，{sourceLabel(item.source)}</span>
        </span>
      )
    },
    {
      id: "reason",
      header: "原因",
      hideBelow: "sm",
      cell: (item) => (
        <span className="block max-w-64 truncate text-xs text-muted-foreground">{reasonText(item, view?.config) || "—"}</span>
      )
    },
    {
      id: "time",
      header: "提交时间",
      align: "right",
      width: "150px",
      cell: (item) => <span className="text-xs text-muted-foreground tabular-nums">{formatDateTimeMinute(item.createdAt)}</span>
    }
  ];

  return (
    <Section
      title="账号注销"
      description="注销按应用配置走冷静期或审核，期满后删除账号并留下墓碑记录"
      icon={UserX}
      tone="warning"
      flush
      actions={
        <>
          {view && !view.enabled ? (
            <Badge variant="outline" size="sm">
              应用未开放注销
            </Badge>
          ) : null}
          <Button asChild size="sm" variant="ghost">
            <Link href={`/apps/${encodeURIComponent(appKey)}?tab=account-deletion`}>
              注销管理
              <ArrowUpRight />
            </Link>
          </Button>
        </>
      }
    >
      {viewQuery.isLoading ? (
        <div className="flex flex-col gap-3 px-4 py-5 sm:px-5">
          <Skeleton className="h-8 w-full max-w-md" />
          <Skeleton className="h-20 w-full rounded-xl" />
          <Skeleton className="h-4 w-64" />
        </div>
      ) : !view ? (
        <StateBlock kind="error" description={errorMessage(viewQuery.error, "未能读取注销状态")} onRetry={() => void viewQuery.refetch()} />
      ) : open ? (
        <OpenRequest
          item={open}
          config={view.config}
          canReview={canReview}
          canExecute={canExecute}
          onAction={setAction}
          onShowDetail={() => showSheet(open)}
        />
      ) : canCreate ? (
        <CreateForm
          appKey={appKey}
          userId={userId}
          account={account}
          defaultCoolingDays={view.config.coolingDays}
          reasons={view.config.reasons}
          maxCoolingDays={catalogQuery.data?.limits?.maxCoolingDays ?? 180}
          onDone={handleDone}
        />
      ) : (
        <StateBlock compact icon={UserX} title="暂无进行中的注销申请" description="当前账号没有发起注销权限" />
      )}

      {history.length ? (
        <div className="border-t">
          <div className="flex items-center justify-between px-4 pt-3 pb-2 sm:px-5">
            <span className="text-xs font-medium text-muted-foreground">历史申请</span>
            <span className="text-xs text-muted-foreground tabular-nums">共 {history.length} 条</span>
          </div>
          <RecordTable
            columns={historyColumns}
            rows={history}
            rowKey={(item) => item.id}
            onRowClick={showSheet}
            minWidth={480}
          />
        </div>
      ) : null}

      <DeletionActionDialog
        appKey={appKey}
        request={action ? open : null}
        kind={action}
        onOpenChange={(next) => !next && setAction(null)}
        onDone={handleDone}
      />
      <DeletionRequestSheet
        appKey={appKey}
        request={sheetItem}
        open={sheetOpen}
        onOpenChange={setSheetOpen}
        config={view?.config}
        canReview={canReview}
        canExecute={canExecute}
        onCompleted={handleDone}
      />
    </Section>
  );
}

// ── 进行中的申请 ──────────────────────────

type Step = { key: string; label: string };

/** 申请走过的状态机：提交 →（审核）→（冷静期）→ 执行。 */
function stepsOf(item: DeletionRequest): { steps: Step[]; current: number } {
  const steps: Step[] = [{ key: "submit", label: "提交申请" }];
  if (item.status === "reviewing" || item.reviewedAt) steps.push({ key: "review", label: "审核" });
  if (item.mode === "cooling") steps.push({ key: "cooling", label: `冷静期 ${item.coolingDays} 天` });
  steps.push({ key: "execute", label: "执行注销" });
  const target =
    item.status === "reviewing" ? "review" : item.status === "scheduled" ? "cooling" : "execute";
  const current = Math.max(0, steps.findIndex((step) => step.key === target));
  return { steps, current };
}

function Stepper({ item }: { item: DeletionRequest }) {
  const { steps, current } = stepsOf(item);
  const failed = item.status === "failed";
  return (
    <ol className="flex flex-wrap items-center gap-x-2 gap-y-2">
      {steps.map((step, index) => {
        const done = index < current;
        const active = index === current;
        return (
          <li key={step.key} className="flex items-center gap-2">
            <span
              className={cn(
                "flex items-center gap-1.5 rounded-full border px-2.5 py-1 text-xs",
                done && "border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-900/60 dark:bg-emerald-950/40 dark:text-emerald-300",
                active && !failed && "border-sky-200 bg-sky-50 font-medium text-sky-700 dark:border-sky-900/60 dark:bg-sky-950/40 dark:text-sky-300",
                active && failed && "border-red-200 bg-red-50 font-medium text-red-700 dark:border-red-900/60 dark:bg-red-950/40 dark:text-red-300",
                !done && !active && "text-muted-foreground"
              )}
            >
              {done ? <Check className="size-3" /> : <span className="tabular-nums">{index + 1}</span>}
              {step.label}
            </span>
            {index < steps.length - 1 ? <span aria-hidden className="h-px w-4 bg-border" /> : null}
          </li>
        );
      })}
    </ol>
  );
}

const OPEN_TONE: Record<string, { tone: Tone; icon: ComponentType<{ className?: string }> }> = {
  failed: { tone: "danger", icon: OctagonAlert },
  reviewing: { tone: "warning", icon: Gavel },
  processing: { tone: "violet", icon: Loader2 },
  scheduled: { tone: "info", icon: Hourglass }
};

function OpenRequest({
  item,
  config,
  canReview,
  canExecute,
  onAction,
  onShowDetail
}: {
  item: DeletionRequest;
  config?: DeletionConfig | null;
  canReview: boolean;
  canExecute: boolean;
  onAction: (kind: DeletionActionKind) => void;
  onShowDetail: () => void;
}) {
  const actions = availableActions(item.status);
  const reason = reasonText(item, config);
  const meta = OPEN_TONE[item.status] ?? OPEN_TONE.scheduled;

  return (
    <div className="flex flex-col gap-4 px-4 py-4 sm:px-5 sm:py-5">
      <Stepper item={item} />

      <div className="flex items-start gap-3 rounded-xl border bg-muted/30 px-4 py-3.5">
        <ToneIcon icon={meta.icon} tone={meta.tone} />
        <div className="flex min-w-0 flex-1 flex-col gap-1">
          <div className="flex flex-wrap items-center gap-2">
            <DeletionStatusBadge status={item.status} />
            <span className="text-sm font-medium">{modeLabel(item.mode)}</span>
            <span className="text-xs text-muted-foreground">{sourceLabel(item.source)}发起</span>
          </div>
          {item.status === "scheduled" && item.scheduledAt ? (
            <div className="text-sm tabular-nums">
              {formatDateTimeMinute(item.scheduledAt)} 注销
              <span className="ml-1.5 text-muted-foreground">{formatRelative(item.scheduledAt)}</span>
            </div>
          ) : null}
          {item.status === "failed" && item.failure ? (
            <p className="text-xs leading-5 break-words text-red-700 dark:text-red-300">{item.failure}</p>
          ) : null}
        </div>
      </div>

      <InfoGrid
        columns={3}
        items={[
          { label: "原因", value: reason || "未填写原因", wide: true },
          { label: "提交时间", value: formatDateTimeMinute(item.createdAt), hint: formatRelative(item.createdAt) },
          { label: "操作人", value: item.operator || (item.source === "user" ? "用户本人" : null) },
          { label: "执行次数", value: item.attempts ? String(item.attempts) : null }
        ]}
      />

      <div className="flex flex-wrap items-center justify-end gap-2 border-t pt-4">
        <Button size="sm" variant="ghost" onClick={onShowDetail} className="mr-auto">
          查看详情
        </Button>
        {canReview && actions.cancel ? (
          <Button size="sm" variant="outline" onClick={() => onAction("cancel")}>
            <Undo2 />
            撤销
          </Button>
        ) : null}
        {canReview && actions.reschedule ? (
          <Button size="sm" variant="outline" onClick={() => onAction("reschedule")}>
            <CalendarClock />
            改期
          </Button>
        ) : null}
        {canReview && actions.reject ? (
          <Button size="sm" variant="outline" onClick={() => onAction("reject")}>
            <XCircle />
            驳回
          </Button>
        ) : null}
        {canReview && actions.approve ? (
          <Button size="sm" onClick={() => onAction("approve")}>
            <CheckCircle2 />
            批准
          </Button>
        ) : null}
        {canExecute && actions.execute ? (
          <Button size="sm" variant="destructive" onClick={() => onAction("execute")}>
            <OctagonAlert />
            立即执行
          </Button>
        ) : null}
      </div>
    </div>
  );
}

// ── 发起注销 ──────────────────────────

function ModeOption({
  value,
  active,
  title,
  description,
  icon: Icon,
  tone
}: {
  value: DeletionMode;
  active: boolean;
  title: string;
  description: string;
  icon: ComponentType<{ className?: string }>;
  tone: Tone;
}) {
  return (
    <label
      className={cn(
        "flex cursor-pointer items-start gap-3 rounded-xl border p-3 transition-colors",
        active ? "border-primary/40 bg-primary/5 ring-1 ring-inset ring-primary/20" : "hover:bg-accent/40"
      )}
    >
      <ToneIcon icon={Icon} tone={active ? tone : "neutral"} size="sm" />
      <span className="flex min-w-0 flex-1 flex-col gap-0.5">
        <span className="text-sm font-medium">{title}</span>
        <span className="text-xs leading-5 text-muted-foreground">{description}</span>
      </span>
      <RadioGroupItem value={value} className="mt-0.5 shrink-0" />
    </label>
  );
}

function CreateForm({
  appKey,
  userId,
  account,
  defaultCoolingDays,
  reasons,
  maxCoolingDays,
  onDone
}: {
  appKey: string;
  userId: number;
  account: string;
  defaultCoolingDays: number;
  reasons: DeletionConfig["reasons"];
  maxCoolingDays: number;
  onDone: (result: DeletionRequest) => void;
}) {
  const mutation = useCreateUserDeletionMutation(appKey, userId);
  const [mode, setMode] = useState<DeletionMode>("cooling");
  const [coolingDays, setCoolingDays] = useState(String(defaultCoolingDays || 15));
  const [reasonCode, setReasonCode] = useState("");
  const [reason, setReason] = useState("");
  const [notify, setNotify] = useState(true);
  const [confirm, setConfirm] = useState("");

  const days = Number.parseInt(coolingDays, 10);
  const problem =
    !reasonCode && !reason.trim()
      ? "请选择或填写注销原因"
      : mode === "cooling" && !(days >= 1 && days <= maxCoolingDays)
        ? `冷静期为 1 至 ${maxCoolingDays} 天`
        : mode === "immediate" && confirm.trim() !== account
          ? "输入账号名确认后才能立即注销"
          : null;

  async function submit() {
    if (problem || mutation.isPending) return;
    try {
      const result = await mutation.mutateAsync({
        mode,
        coolingDays: mode === "cooling" ? days : undefined,
        reasonCode: reasonCode || undefined,
        reason: reason.trim(),
        notify,
        confirm: mode === "immediate" ? confirm.trim() : undefined
      });
      if (result.status === "completed") {
        toast.success(`账号 ${account} 已注销`);
      } else if (result.status === "failed") {
        toast.error(result.failure ? `执行失败：${result.failure}` : "执行失败");
      } else if (result.status === "reviewing") {
        toast.success("注销申请已提交，等待审核");
      } else {
        toast.success("已进入注销冷静期");
      }
      setReason("");
      setReasonCode("");
      setConfirm("");
      onDone(result);
    } catch (error) {
      toast.error(errorMessage(error, "发起失败"));
    }
  }

  return (
    <>
      <div className="grid gap-6 px-4 py-5 sm:px-5 lg:grid-cols-2">
        <div className="flex flex-col gap-4">
          <div className="flex flex-col gap-2">
            <Label className="text-xs text-muted-foreground">注销方式</Label>
            <RadioGroup value={mode} onValueChange={(value) => setMode(value as DeletionMode)} className="grid gap-2 sm:grid-cols-2">
              <ModeOption
                value="cooling"
                active={mode === "cooling"}
                title={MODE_META.cooling.label}
                description="期满自动注销，期间可撤销"
                icon={Hourglass}
                tone="info"
              />
              <ModeOption
                value="immediate"
                active={mode === "immediate"}
                title={MODE_META.immediate.label}
                description="当场删除账号，无法恢复"
                icon={Zap}
                tone="danger"
              />
            </RadioGroup>
          </div>
          {mode === "cooling" ? (
            <div className="flex flex-col gap-1.5">
              <Label htmlFor={`deletion-days-${userId}`} className="text-xs text-muted-foreground">
                冷静期（天）
              </Label>
              <Input
                id={`deletion-days-${userId}`}
                type="number"
                inputMode="numeric"
                min={1}
                max={maxCoolingDays}
                value={coolingDays}
                onChange={(event) => setCoolingDays(event.target.value)}
                className="w-32 font-mono tabular-nums"
              />
              <span className="text-xs text-muted-foreground">应用默认 {defaultCoolingDays || 15} 天，最长 {maxCoolingDays} 天</span>
            </div>
          ) : null}
          <label className="flex cursor-pointer items-center justify-between gap-3 rounded-xl border p-3">
            <span className="flex flex-col gap-0.5">
              <span className="text-sm font-medium">通知用户</span>
              <span className="text-xs text-muted-foreground">向用户发送注销通知</span>
            </span>
            <Switch checked={notify} onCheckedChange={setNotify} />
          </label>
        </div>

        <div className="flex flex-col gap-2">
          <Label className="text-xs text-muted-foreground">
            注销原因<span className="ml-0.5 text-red-500">*</span>
          </Label>
          {reasons.length ? (
            <div className="flex flex-wrap gap-1.5" role="radiogroup" aria-label="注销原因">
              {reasons.map((item) => {
                const active = item.code === reasonCode;
                return (
                  <button
                    key={item.code}
                    type="button"
                    role="radio"
                    aria-checked={active}
                    onClick={() => setReasonCode(active ? "" : item.code)}
                    className={cn(
                      "rounded-full border px-2.5 py-1 text-xs transition-colors",
                      active
                        ? "border-primary/40 bg-primary/10 font-medium text-primary"
                        : "text-muted-foreground hover:bg-accent hover:text-foreground"
                    )}
                  >
                    {item.label}
                  </button>
                );
              })}
            </div>
          ) : null}
          <div className="flex items-baseline justify-between gap-2 pt-1">
            <span className="text-xs text-muted-foreground">{reasons.length ? "补充说明（选了原因时可不填）" : "说明"}</span>
            <span className="text-xs text-muted-foreground tabular-nums">
              {reason.length}/{REASON_MAX}
            </span>
          </div>
          <Textarea
            rows={3}
            value={reason}
            maxLength={REASON_MAX}
            onChange={(event) => setReason(event.target.value)}
            className="resize-none"
          />
          <div className="flex flex-wrap gap-1.5">
            {ADMIN_NOTE_PRESETS.map((preset) => (
              <button
                key={preset}
                type="button"
                onClick={() => setReason(preset)}
                className="rounded-full border px-2.5 py-1 text-xs text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
              >
                {preset}
              </button>
            ))}
          </div>
          {mode === "immediate" ? (
            <div className="pt-2">
              <ExecuteConfirm account={account} value={confirm} onChange={setConfirm} />
            </div>
          ) : null}
        </div>
      </div>
      <div className="flex flex-col gap-2 border-t bg-muted/20 px-4 py-2.5 sm:flex-row sm:items-center sm:justify-between sm:px-5">
        <span className="text-xs text-muted-foreground">
          {problem ?? (mode === "cooling" ? `${days} 天后自动注销，期间可撤销` : "提交后立即删除账号")}
        </span>
        <Button
          size="sm"
          variant="destructive"
          disabled={Boolean(problem) || mutation.isPending}
          onClick={() => void submit()}
          className="self-end sm:self-auto"
        >
          {mutation.isPending ? <Loader2 className="animate-spin" /> : <UserX />}
          {mode === "immediate" ? "立即注销" : "发起注销"}
        </Button>
      </div>
    </>
  );
}
