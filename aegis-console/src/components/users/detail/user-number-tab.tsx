"use client";

import { useState } from "react";
import Link from "next/link";
import { ArrowRight, Dices, Hash, History, LockOpen, PencilLine, Snowflake, Ticket, Undo2 } from "lucide-react";
import { toast } from "sonner";
import { DrawCandidates, historySourceText } from "@/components/apps/user-number/number-records-panel";
import {
  NumberText,
  NumberTierBadge,
  NumberUserCell,
  PatternList,
  RightLabel,
  flattenRights,
  rightUnit
} from "@/components/apps/user-number/number-shared";
import { ConfirmActionDialog } from "@/components/users/detail/records/records-shared";
import { InfoGrid, RecordTable, Section, StateBlock, errorMessage, formatInt, type Column } from "@/components/users/detail/kit";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { ApiError } from "@/lib/api-client";
import {
  NUMBER_GRANT,
  NUMBER_READ,
  NUMBER_TICKET_LEVELS,
  NUMBER_WRITE,
  type NumberHistory,
  type NumberHold,
  type NumberRight,
  type NumberRightKind
} from "@/lib/api/user-number";
import { useAppKeyPermissionChecker } from "@/lib/app-permission-hooks";
import { formatDateTime, formatRelative } from "@/lib/datetime";
import { usePermissionsLoading } from "@/lib/permissions";
import {
  useAdjustNumberRightMutation,
  useAssignNumberMutation,
  useNumberCatalogQuery,
  useNumberInspectQuery,
  useNumberUserQuery,
  useReleaseNumberHoldMutation,
  useRollbackNumberMutation
} from "@/lib/user-number-hooks";

/**
 * 用户详情「号码」页签（docs/user-number.md）：当前号码与等级、冷却、权益、打开的摇号单、冻结中的旧号、最近换号。
 *
 * 指定号码、回滚、调整权益要 `app:number:grant`；提前解除旧号冻结属于占用管理，要 `app:number:write`。
 * 号码等级在换号那一刻判定，这里显示的是按当前规则实时再判的结果（后端 AdminUser 已算好）。
 */
export function UserNumberTab({ appKey, userId }: { appKey: string; userId: number }) {
  const can = useAppKeyPermissionChecker(appKey);
  const permissionsLoading = usePermissionsLoading();
  const canRead = can(NUMBER_READ);
  const canGrant = can(NUMBER_GRANT);
  const canWrite = can(NUMBER_WRITE);
  const query = useNumberUserQuery(canRead ? appKey : null, userId);
  const catalog = useNumberCatalogQuery(canRead ? appKey : null);
  const tiers = catalog.data?.tiers;
  const [assignOpen, setAssignOpen] = useState(false);
  const [adjustOpen, setAdjustOpen] = useState(false);
  const [dialogKey, setDialogKey] = useState(0);
  const [rollbackTarget, setRollbackTarget] = useState<NumberHistory | null>(null);
  const [releaseTarget, setReleaseTarget] = useState<NumberHold | null>(null);
  const rollback = useRollbackNumberMutation(appKey, userId);
  const release = useReleaseNumberHoldMutation(appKey);

  if (permissionsLoading) return <Skeleton className="h-48 w-full rounded-xl" />;
  if (!canRead) return <StateBlock title="缺少「用户号码：查看」权限" />;
  if (query.isLoading) {
    return (
      <div className="space-y-4">
        <Skeleton className="h-40 w-full rounded-xl" />
        <Skeleton className="h-56 w-full rounded-xl" />
      </div>
    );
  }
  if (query.error || !query.data) {
    return <StateBlock kind="error" description={errorMessage(query.error)} onRetry={() => void query.refetch()} />;
  }

  const data = query.data;
  const rights = flattenRights(data.rights);
  const luck = data.luck;
  const openDialog = (which: "assign" | "adjust") => {
    setDialogKey((key) => key + 1);
    if (which === "assign") setAssignOpen(true);
    else setAdjustOpen(true);
  };

  const doRollback = async () => {
    if (!rollbackTarget) return;
    try {
      const result = await rollback.mutateAsync(rollbackTarget.id);
      toast.success(`已回滚为 ${result.newNumber}`);
      setRollbackTarget(null);
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "回滚失败");
    }
  };

  const doRelease = async () => {
    if (!releaseTarget) return;
    try {
      await release.mutateAsync(releaseTarget.number);
      toast.success("已解除冻结");
      setReleaseTarget(null);
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "解除失败");
    }
  };

  const rightColumns: Array<Column<NumberRight>> = [
    { id: "name", header: "权益", cell: (row) => <span className="text-xs"><RightLabel kind={row.kind} level={row.level} name={row.name} /></span> },
    {
      id: "balance",
      header: "余额",
      align: "right",
      width: "88px",
      cell: (row) => (
        <span className={row.balance > 0 ? "text-sm font-semibold tabular-nums" : "text-sm tabular-nums text-muted-foreground"}>
          {formatInt(row.balance)}
          <span className="ml-0.5 text-[11px] font-normal text-muted-foreground">{rightUnit(row.kind)}</span>
        </span>
      )
    },
    { id: "earned", header: "累计获得", align: "right", width: "88px", cell: (row) => <span className="text-xs tabular-nums text-muted-foreground">{formatInt(row.earned)}</span> },
    { id: "used", header: "已使用", align: "right", width: "88px", cell: (row) => <span className="text-xs tabular-nums text-muted-foreground">{formatInt(row.used)}</span> }
  ];

  const historyColumns: Array<Column<NumberHistory>> = [
    { id: "time", header: "时间", width: "150px", cell: (row) => <span className="text-[11px] tabular-nums text-muted-foreground">{formatDateTime(row.createdAt)}</span> },
    {
      id: "change",
      header: "号码",
      cell: (row) => (
        <span className="flex flex-wrap items-center gap-1.5">
          <span className="font-mono text-xs text-muted-foreground tabular-nums">{row.oldNumber ?? "—"}</span>
          <ArrowRight className="size-3 text-muted-foreground" />
          <NumberText number={row.newNumber} tier={row.tier} tiers={tiers} />
        </span>
      )
    },
    { id: "source", header: "方式", width: "100px", cell: (row) => <span className="text-xs">{historySourceText(row)}</span> },
    {
      id: "operator",
      header: "操作人与备注",
      width: "22%",
      hideBelow: "lg",
      cell: (row) => (
        <div className="min-w-0 text-[11px] text-muted-foreground">
          <div className="truncate">{row.operator || "—"}</div>
          {row.note ? <div className="truncate" title={row.note}>{row.note}</div> : null}
        </div>
      )
    },
    ...(canGrant
      ? [
          {
            id: "actions",
            header: "",
            width: "72px",
            align: "right" as const,
            cell: (row: NumberHistory) =>
              row.oldNumber ? (
                <Button size="sm" variant="ghost" className="h-7 px-2 text-xs" onClick={() => setRollbackTarget(row)}>
                  <Undo2 className="size-3.5" />
                  回滚
                </Button>
              ) : null
          }
        ]
      : [])
  ];

  return (
    <div className="space-y-4">
      <Section
        title="号码"
        icon={Hash}
        tone="primary"
        actions={
          canGrant ? (
            <>
              <Button size="sm" variant="outline" onClick={() => openDialog("adjust")}>
                <Ticket className="size-3.5" />
                调整权益
              </Button>
              <Button size="sm" onClick={() => openDialog("assign")}>
                <PencilLine className="size-3.5" />
                指定号码
              </Button>
            </>
          ) : null
        }
      >
        <div className="space-y-4">
          <div className="flex flex-wrap items-center gap-3">
            <NumberText number={data.number} size="lg" />
            <NumberTierBadge tier={data.tier} tiers={tiers} className="text-xs" />
          </div>
          <InfoGrid
            columns={3}
            items={[
              { label: "命中规律", value: <PatternList names={data.patternNames} />, wide: true },
              { label: "上次换号", value: data.changedAt ? formatDateTime(data.changedAt) : "未换过" },
              {
                label: "冷却",
                value: data.nextChangeAt ? `${formatDateTime(data.nextChangeAt)} 后可换` : "可以换号",
                tone: data.nextChangeAt ? "warning" : "success"
              },
              {
                label: "今日号运",
                value:
                  luck?.factor !== undefined
                    ? `${luck.factor.toFixed(2)}${luck.title ? ` ${luck.title}` : ""}${luck.percentile ? `（超过 ${luck.percentile}%）` : ""}`
                    : "未开启"
              }
            ]}
          />
        </div>
      </Section>

      <div className="grid gap-4 lg:grid-cols-2">
        <Section title="权益" icon={Ticket} tone="violet" flush>
          <RecordTable columns={rightColumns} rows={rights} rowKey={(row) => `${row.kind}-${row.level}`} minWidth={380} />
        </Section>

        <div className="space-y-4">
          <Section title="摇号单" icon={Dices} tone="warning">
            {data.draw ? (
              <div className="space-y-2">
                <DrawCandidates draw={data.draw} tiers={tiers} />
                <div className="text-[11px] text-muted-foreground">
                  号运 {data.draw.luck ? data.draw.luck.toFixed(2) : "—"}，{formatRelative(data.draw.expiresAt)}过期
                </div>
              </div>
            ) : (
              <p className="text-xs text-muted-foreground">暂无待选号的摇号单</p>
            )}
          </Section>

          <Section title="冻结的旧号" icon={Snowflake} tone="info" flush>
            {data.frozen?.length ? (
              <ul className="divide-y">
                {data.frozen.map((hold) => (
                  <li key={hold.number} className="flex items-center justify-between gap-3 px-4 py-2.5 sm:px-5">
                    <div className="min-w-0">
                      <NumberText number={hold.number} tier={hold.tier} tiers={tiers} />
                      <div className="text-[11px] text-muted-foreground">
                        {hold.expiresAt ? `${formatDateTime(hold.expiresAt)} 放出` : "长期冻结"}
                      </div>
                    </div>
                    {canWrite ? (
                      <Button size="sm" variant="ghost" className="h-7 px-2 text-xs" onClick={() => setReleaseTarget(hold)}>
                        <LockOpen className="size-3.5" />
                        解除
                      </Button>
                    ) : null}
                  </li>
                ))}
              </ul>
            ) : (
              <p className="px-4 py-4 text-xs text-muted-foreground sm:px-5">暂无冻结的旧号</p>
            )}
          </Section>
        </div>
      </div>

      <Section
        title="最近换号"
        icon={History}
        tone="info"
        flush
        actions={
          <Button asChild size="sm" variant="ghost" className="h-7 text-xs">
            <Link href={`/apps/${encodeURIComponent(appKey)}?tab=numbers`}>号码管理</Link>
          </Button>
        }
      >
        <RecordTable
          columns={historyColumns}
          rows={data.history}
          rowKey={(row) => row.id}
          empty={<StateBlock compact icon={History} title="暂无换号记录" />}
        />
      </Section>

      {assignOpen ? (
        <AssignDialog key={`assign-${dialogKey}`} appKey={appKey} userId={userId} currentNumber={data.number} open={assignOpen} onOpenChange={setAssignOpen} />
      ) : null}
      {adjustOpen ? (
        <AdjustDialog key={`adjust-${dialogKey}`} appKey={appKey} userId={userId} rights={rights} open={adjustOpen} onOpenChange={setAdjustOpen} />
      ) : null}

      <ConfirmActionDialog
        open={Boolean(rollbackTarget)}
        onOpenChange={(open) => !open && setRollbackTarget(null)}
        title="回滚号码"
        description={rollbackTarget ? `号码由 ${data.number} 改回 ${rollbackTarget.oldNumber}。` : undefined}
        confirmLabel="回滚"
        destructive
        pending={rollback.isPending}
        onConfirm={doRollback}
      />
      <ConfirmActionDialog
        open={Boolean(releaseTarget)}
        onOpenChange={(open) => !open && setReleaseTarget(null)}
        title={releaseTarget ? `解除 ${releaseTarget.number} 的冻结` : "解除冻结"}
        description="号码立即可被他人选用，原主人不再能免费换回。"
        confirmLabel="解除"
        destructive
        pending={release.isPending}
        onConfirm={doRelease}
      />
    </div>
  );
}

const NUMBER_RE = /^[1-9]\d{4,11}$/;

function AssignDialog({
  appKey,
  userId,
  currentNumber,
  open,
  onOpenChange
}: {
  appKey: string;
  userId: number;
  currentNumber: number;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const [number, setNumber] = useState("");
  const [force, setForce] = useState(false);
  const [note, setNote] = useState("");
  const valid = NUMBER_RE.test(number);
  const inspect = useNumberInspectQuery(appKey, number);
  const assign = useAssignNumberMutation(appKey, userId);
  const info = valid ? inspect.data : undefined;
  const same = valid && Number(number) === currentNumber;
  const takenByOther = Boolean(info?.ownerId && info.ownerId !== userId);
  const held = Boolean(info?.hold && !(info.hold.kind === "frozen" && info.hold.userId === userId));

  const submit = async () => {
    try {
      const result = await assign.mutateAsync({ number, force, note: note.trim() || undefined });
      toast.success(`已指定为 ${result.newNumber}`);
      onOpenChange(false);
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "指定失败");
    }
  };

  return (
    <ConfirmActionDialog
      open={open}
      onOpenChange={onOpenChange}
      title="指定号码"
      confirmLabel="指定"
      pending={assign.isPending}
      disabled={!valid || same || takenByOther || (held && !force)}
      onConfirm={submit}
    >
      <div className="space-y-3">
        <div className="space-y-1.5">
          <Label className="text-xs">号码</Label>
          <Input
            value={number}
            inputMode="numeric"
            maxLength={12}
            placeholder="5–12 位，不以 0 开头"
            onChange={(event) => setNumber(event.target.value.replace(/\D/g, ""))}
            className="font-mono tabular-nums"
          />
          <div className="min-h-5 text-[11px]">
            {!number ? null : !valid ? (
              <span className="text-muted-foreground">号码须为 5–12 位且不以 0 开头</span>
            ) : same ? (
              <span className="text-amber-600 dark:text-amber-400">与当前号码相同</span>
            ) : inspect.isLoading ? (
              <span className="text-muted-foreground">查询中</span>
            ) : info ? (
              <span className="flex flex-wrap items-center gap-1.5">
                <NumberTierBadge tier={info.tier} />
                {info.patternNames?.length ? <span className="text-muted-foreground">{info.patternNames.join("、")}</span> : null}
                {takenByOther ? (
                  <span className="inline-flex items-center gap-1 text-red-600 dark:text-red-400">
                    已被使用
                    <NumberUserCell appKey={appKey} userId={info.ownerId} account={info.ownerAccount} nickname={info.ownerName} />
                  </span>
                ) : held ? (
                  <span className="text-amber-600 dark:text-amber-400">
                    {info.hold?.kind === "reserved" ? "保留号" : info.hold?.kind === "frozen" ? "他人冻结中" : "摇号锁定中"}，需勾选强制
                  </span>
                ) : (
                  <span className="text-emerald-600 dark:text-emerald-400">可以指定</span>
                )}
                {!info.valid ? <span className="text-amber-600 dark:text-amber-400">位数超出用户可选范围</span> : null}
              </span>
            ) : null}
          </div>
        </div>
        <label className="flex cursor-pointer items-center gap-2 text-xs">
          <Checkbox checked={force} onCheckedChange={(value) => setForce(value === true)} />
          强制（可指定保留号、他人冻结号与候选号）
        </label>
        <div className="space-y-1.5">
          <Label className="text-xs">备注</Label>
          <Input value={note} maxLength={200} placeholder="例如：客服补偿" onChange={(event) => setNote(event.target.value)} />
        </div>
      </div>
    </ConfirmActionDialog>
  );
}

const KIND_OPTIONS: Array<{ value: NumberRightKind; label: string }> = [
  { value: "change", label: "改号卡" },
  { value: "ticket", label: "靓号券" },
  { value: "draw", label: "摇号次数" }
];

const AMOUNT_PRESETS = [1, 3, 5];

function AdjustDialog({
  appKey,
  userId,
  rights,
  open,
  onOpenChange
}: {
  appKey: string;
  userId: number;
  rights: NumberRight[];
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const [kind, setKind] = useState<NumberRightKind>("change");
  const [level, setLevel] = useState(1);
  const [direction, setDirection] = useState<"grant" | "deduct">("grant");
  const [amount, setAmount] = useState("1");
  const [note, setNote] = useState("");
  const adjust = useAdjustNumberRightMutation(appKey, userId);
  const effectiveLevel = kind === "ticket" ? level : 0;
  const balance = rights.find((right) => right.kind === kind && right.level === effectiveLevel)?.balance ?? 0;
  const value = Number.parseInt(amount, 10);
  const validAmount = Number.isFinite(value) && value > 0 && value <= 100000;
  const overdraw = direction === "deduct" && validAmount && value > balance;

  const submit = async () => {
    try {
      await adjust.mutateAsync({
        kind,
        level: effectiveLevel,
        delta: direction === "grant" ? value : -value,
        note: note.trim() || undefined
      });
      toast.success(direction === "grant" ? "已发放" : "已扣减");
      onOpenChange(false);
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "调整失败");
    }
  };

  return (
    <ConfirmActionDialog
      open={open}
      onOpenChange={onOpenChange}
      title="调整权益"
      confirmLabel={direction === "grant" ? "发放" : "扣减"}
      destructive={direction === "deduct"}
      pending={adjust.isPending}
      disabled={!validAmount || overdraw}
      onConfirm={submit}
    >
      <div className="space-y-3">
        <ToggleGroup
          type="single"
          size="sm"
          variant="outline"
          value={direction}
          onValueChange={(next) => next && setDirection(next as "grant" | "deduct")}
          className="w-full"
        >
          <ToggleGroupItem value="grant" className="flex-1 text-xs">
            发放
          </ToggleGroupItem>
          <ToggleGroupItem value="deduct" className="flex-1 text-xs">
            扣减
          </ToggleGroupItem>
        </ToggleGroup>
        <div className="grid grid-cols-2 gap-3">
          <div className="space-y-1.5">
            <Label className="text-xs">种类</Label>
            <Select value={kind} onValueChange={(next) => setKind(next as NumberRightKind)}>
              <SelectTrigger size="sm" className="h-8 w-full text-xs">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {KIND_OPTIONS.map((option) => (
                  <SelectItem key={option.value} value={option.value} className="text-xs">
                    {option.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="space-y-1.5">
            <Label className="text-xs">等级</Label>
            <Select value={String(level)} onValueChange={(next) => setLevel(Number(next))} disabled={kind !== "ticket"}>
              <SelectTrigger size="sm" className="h-8 w-full text-xs">
                <SelectValue placeholder="不适用" />
              </SelectTrigger>
              <SelectContent>
                {NUMBER_TICKET_LEVELS.map((tier) => (
                  <SelectItem key={tier.level} value={String(tier.level)} className="text-xs">
                    <span className="inline-flex items-center gap-1.5">
                      <span className="size-2 rounded-full" style={{ backgroundColor: tier.color }} aria-hidden />
                      {tier.name}
                    </span>
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        </div>
        <div className="space-y-1.5">
          <div className="flex items-baseline justify-between">
            <Label className="text-xs">数量</Label>
            <span className="text-[11px] text-muted-foreground">
              当前余额 {formatInt(balance)} {rightUnit(kind)}
            </span>
          </div>
          <div className="flex items-center gap-1.5">
            <Input
              value={amount}
              inputMode="numeric"
              onChange={(event) => setAmount(event.target.value.replace(/\D/g, ""))}
              className="h-8 w-28 font-mono tabular-nums"
            />
            {AMOUNT_PRESETS.map((preset) => (
              <Button key={preset} type="button" size="sm" variant="outline" className="h-8 px-2.5 text-xs" onClick={() => setAmount(String(preset))}>
                {preset}
              </Button>
            ))}
          </div>
          {overdraw ? <p className="text-[11px] text-red-600 dark:text-red-400">扣减数量超过余额</p> : null}
        </div>
        <div className="space-y-1.5">
          <Label className="text-xs">备注</Label>
          <Input value={note} maxLength={200} placeholder="例如：活动补发" onChange={(event) => setNote(event.target.value)} />
        </div>
      </div>
    </ConfirmActionDialog>
  );
}
