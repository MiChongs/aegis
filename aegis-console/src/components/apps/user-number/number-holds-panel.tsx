"use client";

import { useMemo, useState } from "react";
import { Lock, LockOpen, Plus, Search, ShieldCheck } from "lucide-react";
import { toast } from "sonner";
import { ConfirmActionDialog } from "@/components/users/detail/records/records-shared";
import {
  FilterBar,
  FilterSelect,
  InfoGrid,
  Pager,
  RecordTable,
  Section,
  StateBlock,
  errorMessage,
  useDebouncedValue,
  type Column
} from "@/components/users/detail/kit";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { ApiError } from "@/lib/api-client";
import type { NumberHold, NumberHoldKind, NumberTierSpec } from "@/lib/api/user-number";
import { formatDateTime, fromLocalInputValue } from "@/lib/datetime";
import {
  useNumberHoldsQuery,
  useNumberInspectQuery,
  useReleaseNumberHoldMutation,
  useReserveNumbersMutation
} from "@/lib/user-number-hooks";
import { HOLD_KIND_META, HOLD_KIND_OPTIONS, HoldKindBadge, NumberText, NumberTierBadge, NumberUserCell, PatternList } from "./number-shared";

const NUMBER_RE = /^[1-9]\d{4,11}$/;
const MAX_RESERVE = 500;

/** 多行粘贴拆成号码：空白、逗号、顿号、分号都算分隔 */
export function splitNumbers(text: string) {
  return text
    .split(/[\s,，、;；]+/)
    .map((item) => item.trim())
    .filter(Boolean);
}

/** 号码查询：等级、命中规律、谁在用、占用；没人用也没占用时可以直接保留 */
export function NumberInspectPanel({ appKey, tiers, canWrite }: { appKey: string; tiers?: NumberTierSpec[] | null; canWrite: boolean }) {
  const [raw, setRaw] = useState("");
  const number = useDebouncedValue(raw.trim(), 300);
  const valid = NUMBER_RE.test(number);
  const query = useNumberInspectQuery(appKey, number);
  const reserve = useReserveNumbersMutation(appKey);
  const release = useReleaseNumberHoldMutation(appKey);
  const [releaseOpen, setReleaseOpen] = useState(false);
  const data = query.data;

  const doReserve = async () => {
    try {
      const result = await reserve.mutateAsync({ numbers: [number] });
      if (result.reserved?.length) toast.success("已设为保留号");
      else toast.error("未能保留，号码可能已被使用");
      await query.refetch();
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "保留失败");
    }
  };

  const doRelease = async () => {
    try {
      await release.mutateAsync(number);
      toast.success("已放出");
      setReleaseOpen(false);
      await query.refetch();
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "放出失败");
    }
  };

  return (
    <Section
      title="号码查询"
      icon={Search}
      tone="info"
      toolbar={
        <FilterBar>
          <div className="relative w-full sm:w-64">
            <Search className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
            <Input
              value={raw}
              inputMode="numeric"
              maxLength={12}
              placeholder="输入 5–12 位号码"
              onChange={(event) => setRaw(event.target.value.replace(/\D/g, ""))}
              className="h-8 pl-8 font-mono text-sm tabular-nums"
            />
          </div>
          {number && !valid ? <span className="text-xs text-muted-foreground">号码须为 5–12 位且不以 0 开头</span> : null}
        </FilterBar>
      }
    >
      {!valid ? (
        <StateBlock compact icon={Search} title="输入号码查询" />
      ) : query.isLoading ? (
        <div className="py-8 text-center text-xs text-muted-foreground">查询中</div>
      ) : query.error ? (
        <StateBlock kind="error" compact description={errorMessage(query.error)} onRetry={() => void query.refetch()} />
      ) : data ? (
        <div className="space-y-4">
          <div className="flex flex-wrap items-center gap-3">
            <NumberText number={data.number} size="lg" />
            <NumberTierBadge tier={data.tier} tiers={tiers} className="text-xs" />
            {!data.valid ? <span className="text-xs text-amber-600 dark:text-amber-400">位数不在本应用允许范围内</span> : null}
          </div>
          <InfoGrid
            columns={3}
            items={[
              { label: "命中规律", value: <PatternList names={data.patternNames} />, wide: true },
              {
                label: "使用者",
                value: data.ownerId ? (
                  <NumberUserCell appKey={appKey} userId={data.ownerId} account={data.ownerAccount} nickname={data.ownerName} />
                ) : (
                  "无人使用"
                )
              },
              { label: "占用", value: data.hold ? <HoldKindBadge kind={data.hold.kind} /> : "无" },
              { label: "占用备注", value: data.hold?.note },
              { label: "占用到期", value: data.hold?.expiresAt ? formatDateTime(data.hold.expiresAt) : data.hold ? "长期" : undefined },
              { label: "占用人", value: data.hold?.createdBy }
            ]}
          />
          {canWrite ? (
            <div className="flex flex-wrap gap-2">
              {!data.ownerId && !data.hold ? (
                <Button size="sm" variant="outline" disabled={reserve.isPending} onClick={doReserve}>
                  <Lock className="size-3.5" />
                  设为保留号
                </Button>
              ) : null}
              {data.hold ? (
                <Button size="sm" variant="outline" onClick={() => setReleaseOpen(true)}>
                  <LockOpen className="size-3.5" />
                  放出
                </Button>
              ) : null}
            </div>
          ) : null}
          <ConfirmActionDialog
            open={releaseOpen}
            onOpenChange={setReleaseOpen}
            title={`放出 ${data.number}`}
            description={data.hold ? `删除这条${HOLD_KIND_META[data.hold.kind]?.label ?? "占用"}，号码立即可被选用。` : undefined}
            confirmLabel="放出"
            destructive
            pending={release.isPending}
            onConfirm={doRelease}
          />
        </div>
      ) : null}
    </Section>
  );
}

/** 占用列表：保留号 / 冻结号 / 摇号候选，可批量添加保留号与放出任意一条 */
export function NumberHoldsPanel({
  appKey,
  tiers,
  canWrite,
  kind,
  onKindChange
}: {
  appKey: string;
  tiers?: NumberTierSpec[] | null;
  canWrite: boolean;
  kind: NumberHoldKind | "";
  onKindChange: (kind: NumberHoldKind | "") => void;
}) {
  const [page, setPage] = useState(1);
  const [limit, setLimit] = useState(20);
  const query = useNumberHoldsQuery(appKey, { kind: kind || undefined, page, limit });
  const release = useReleaseNumberHoldMutation(appKey);
  const [target, setTarget] = useState<NumberHold | null>(null);
  const [addOpen, setAddOpen] = useState(false);
  const [addKey, setAddKey] = useState(0);

  const doRelease = async () => {
    if (!target) return;
    try {
      await release.mutateAsync(target.number);
      toast.success("已放出");
      setTarget(null);
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "放出失败");
    }
  };

  const columns: Array<Column<NumberHold>> = [
    { id: "number", header: "号码", width: "22%", cell: (row) => <NumberText number={row.number} tier={row.tier} tiers={tiers} /> },
    { id: "kind", header: "类型", width: "96px", cell: (row) => <HoldKindBadge kind={row.kind} /> },
    {
      id: "user",
      header: "关联用户",
      width: "14%",
      hideBelow: "md",
      cell: (row) => <NumberUserCell appKey={appKey} userId={row.userId} />
    },
    {
      id: "note",
      header: "备注",
      hideBelow: "md",
      cell: (row) => (
        <span className="line-clamp-2 text-xs text-muted-foreground" title={row.note}>
          {row.note || "—"}
        </span>
      )
    },
    {
      id: "expires",
      header: "到期",
      width: "140px",
      cell: (row) => <span className="text-[11px] tabular-nums text-muted-foreground">{row.expiresAt ? formatDateTime(row.expiresAt) : "长期"}</span>
    },
    {
      id: "created",
      header: "创建",
      width: "150px",
      hideBelow: "lg",
      cell: (row) => (
        <div className="text-[11px] tabular-nums text-muted-foreground">
          <div>{formatDateTime(row.createdAt)}</div>
          {row.createdBy ? <div className="truncate">{row.createdBy}</div> : null}
        </div>
      )
    },
    ...(canWrite
      ? [
          {
            id: "actions",
            header: "",
            width: "72px",
            align: "right" as const,
            cell: (row: NumberHold) => (
              <Button size="sm" variant="ghost" className="h-7 px-2 text-xs" onClick={() => setTarget(row)}>
                放出
              </Button>
            )
          }
        ]
      : [])
  ];

  const data = query.data;

  return (
    <>
      <Section
        title="占用"
        icon={Lock}
        tone="warning"
        flush
        actions={
          canWrite ? (
            <Button
              size="sm"
              onClick={() => {
                setAddKey((key) => key + 1);
                setAddOpen(true);
              }}
            >
              <Plus className="size-3.5" />
              添加保留号
            </Button>
          ) : null
        }
        toolbar={
          <FilterBar>
            <FilterSelect
              label="类型"
              value={kind}
              onChange={(value) => {
                onKindChange(value as NumberHoldKind | "");
                setPage(1);
              }}
              options={HOLD_KIND_OPTIONS}
              allLabel="全部"
            />
          </FilterBar>
        }
        footer={
          data ? (
            <Pager
              page={page}
              limit={limit}
              total={data.total}
              onPage={setPage}
              onLimit={(value) => {
                setLimit(value);
                setPage(1);
              }}
            />
          ) : null
        }
      >
        <RecordTable
          columns={columns}
          rows={data?.items}
          rowKey={(row) => `${row.kind}-${row.number}`}
          loading={query.isLoading}
          fetching={query.isFetching}
          error={query.error}
          onRetry={() => void query.refetch()}
          empty={<StateBlock compact icon={ShieldCheck} title={kind ? `暂无${HOLD_KIND_META[kind].label}` : "暂无占用"} />}
        />
      </Section>

      {addOpen ? <ReserveDialog key={addKey} appKey={appKey} open={addOpen} onOpenChange={setAddOpen} /> : null}

      <ConfirmActionDialog
        open={Boolean(target)}
        onOpenChange={(open) => !open && setTarget(null)}
        title={target ? `放出 ${target.number}` : "放出"}
        description={target ? `删除这条${HOLD_KIND_META[target.kind]?.label ?? "占用"}，号码立即可被选用。` : undefined}
        confirmLabel="放出"
        destructive
        pending={release.isPending}
        onConfirm={doRelease}
      />
    </>
  );
}

function ReserveDialog({ appKey, open, onOpenChange }: { appKey: string; open: boolean; onOpenChange: (open: boolean) => void }) {
  const [text, setText] = useState("");
  const [note, setNote] = useState("");
  const [expiresAt, setExpiresAt] = useState("");
  const reserve = useReserveNumbersMutation(appKey);

  const parsed = useMemo(() => {
    const items = splitNumbers(text);
    const unique = Array.from(new Set(items));
    return { all: unique, valid: unique.filter((item) => NUMBER_RE.test(item)), invalid: unique.filter((item) => !NUMBER_RE.test(item)) };
  }, [text]);
  const tooMany = parsed.valid.length > MAX_RESERVE;

  const submit = async () => {
    try {
      const result = await reserve.mutateAsync({
        numbers: parsed.valid,
        note: note.trim() || undefined,
        expiresAt: fromLocalInputValue(expiresAt) ?? null
      });
      const reserved = result.reserved?.length ?? 0;
      const skipped = result.skipped?.length ?? 0;
      if (reserved) toast.success(skipped ? `已保留 ${reserved} 个，跳过 ${skipped} 个` : `已保留 ${reserved} 个`);
      else toast.error(skipped ? `${skipped} 个号码均未保留` : "没有可保留的号码");
      if (reserved) onOpenChange(false);
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "保留失败");
    }
  };

  return (
    <ConfirmActionDialog
      open={open}
      onOpenChange={onOpenChange}
      title="添加保留号"
      confirmLabel={parsed.valid.length ? `保留 ${parsed.valid.length} 个` : "保留"}
      pending={reserve.isPending}
      disabled={!parsed.valid.length || tooMany}
      onConfirm={submit}
    >
      <div className="space-y-3">
        <div className="space-y-1.5">
          <Label className="text-xs">号码</Label>
          <Textarea
            value={text}
            rows={6}
            placeholder={"每行一个，或用逗号分隔\n88888888\n12345678"}
            onChange={(event) => setText(event.target.value)}
            className="font-mono text-xs"
          />
          <div className="flex flex-wrap justify-between gap-2 text-[11px] text-muted-foreground">
            <span className={tooMany ? "text-red-600 dark:text-red-400" : undefined}>
              有效 {parsed.valid.length} 个{tooMany ? `，一次最多 ${MAX_RESERVE} 个` : ""}
            </span>
            {parsed.invalid.length ? (
              <span className="truncate text-amber-600 dark:text-amber-400" title={parsed.invalid.join(" ")}>
                格式不对 {parsed.invalid.length} 个：{parsed.invalid.slice(0, 3).join("、")}
                {parsed.invalid.length > 3 ? " 等" : ""}
              </span>
            ) : null}
          </div>
        </div>
        <div className="grid gap-3 sm:grid-cols-2">
          <div className="space-y-1.5">
            <Label className="text-xs">备注</Label>
            <Input value={note} maxLength={200} placeholder="例如：运营活动预留" onChange={(event) => setNote(event.target.value)} />
          </div>
          <div className="space-y-1.5">
            <Label className="text-xs">到期时间</Label>
            <Input type="datetime-local" value={expiresAt} onChange={(event) => setExpiresAt(event.target.value)} />
          </div>
        </div>
        <p className="text-[11px] text-muted-foreground">到期留空即长期保留；有人在用的号码会被跳过。</p>
      </div>
    </ConfirmActionDialog>
  );
}
