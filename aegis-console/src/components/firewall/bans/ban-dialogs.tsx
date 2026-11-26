"use client";

import { useMemo, useState } from "react";
import { AlertTriangle, CheckCircle2, CircleSlash, Loader2, Radar, ShieldBan, XCircle } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { FieldError } from "@/components/traffic/traffic-kit";
import { ApiError } from "@/lib/api/client";
import type { BanBatchItem, BanBatchResult, BanFilter, BanUpdateInput } from "@/lib/api/firewall-bans";
import type { IPBanEntry, IPBanMode } from "@/lib/api/types";
import {
  banErrorMessage,
  useBanCountQuery,
  useBatchBanMutation,
  useBatchUpdateBansMutation,
  useUnbanFilterMutation,
  useUnbanIdsMutation,
  useUpdateBanMutation
} from "@/lib/firewall-ban-hooks";
import { cn } from "@/lib/utils";
import { DurationPicker, FormBlock, ModeSelect, TarpitField } from "./ban-fields";
import { banTimeState, describeFilter, durationLabel, isActiveBan, parseIPList, useBanModes, useNow } from "./ban-format";

/** 单次上限（与后端 ip_ban_batch.go 一致） */
export const MAX_BAN_BATCH = 1000;
export const MAX_UNBAN_BATCH = 5000;

export type UnbanRequest = { kind: "ids"; rows: IPBanEntry[] } | { kind: "filter"; filter: BanFilter };
export type EditRequest = { kind: "single"; ban: IPBanEntry } | { kind: "mode" | "duration"; rows: IPBanEntry[] };
export type UnbanTarget = UnbanRequest & { key: number };
export type EditTarget = EditRequest & { key: number };
export type BanSeed = { key: number; ips?: string };

/* ── 公共小件 ── */

function IPSample({ ips, limit = 12 }: { ips: string[]; limit?: number }) {
  if (ips.length === 0) return null;
  return (
    <div className="flex min-w-0 flex-wrap gap-1">
      {ips.slice(0, limit).map((ip) => (
        <span key={ip} className="inline-flex h-6 max-w-full items-center truncate rounded-md bg-muted px-1.5 font-mono text-[11px]" title={ip}>
          {ip}
        </span>
      ))}
      {ips.length > limit ? <span className="inline-flex h-6 items-center px-1 text-[11px] text-muted-foreground">等 {ips.length.toLocaleString("zh-CN")} 个</span> : null}
    </div>
  );
}

function RiskHint({ count }: { count: number | null }) {
  if (!count) return null;
  return (
    <div className="flex min-w-0 items-start gap-2 rounded-lg border border-violet-500/30 bg-violet-500/5 px-3 py-2.5 text-xs leading-5">
      <Radar className="mt-0.5 size-3.5 shrink-0 text-violet-600 dark:text-violet-400" />
      <p className="min-w-0">
        其中 <span className="font-medium tabular-nums">{count.toLocaleString("zh-CN")}</span> 条来自风控处置。这里只解除防火墙封禁，
        风控平台的名单不会随之撤销，如需一并撤销请到风控平台处理。
      </p>
    </div>
  );
}

function NoteField({ value, onChange }: { value: string; onChange: (value: string) => void }) {
  return (
    <FormBlock label="解封备注" hint="可选，记入封禁历史" htmlFor="unban-note">
      <Textarea
        id="unban-note"
        value={value}
        maxLength={200}
        rows={2}
        onChange={(event) => onChange(event.target.value)}
        placeholder="例如：误封，已核实为合作方出口"
        className="min-h-16 resize-none text-sm"
      />
    </FormBlock>
  );
}

/* ── 解封 ── */

export function UnbanDialog({ target, onClose, onDone }: { target: UnbanTarget | null; onClose: () => void; onDone: () => void }) {
  return (
    <Dialog open={target !== null} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="flex max-h-[min(720px,calc(100dvh-2rem))] flex-col gap-0 overflow-hidden p-0 sm:max-w-lg">
        {target ? (
          target.kind === "ids" ? (
            <UnbanIdsBody key={target.key} rows={target.rows} onClose={onClose} onDone={onDone} />
          ) : (
            <UnbanFilterBody key={target.key} filter={target.filter} onClose={onClose} onDone={onDone} />
          )
        ) : null}
      </DialogContent>
    </Dialog>
  );
}

function UnbanIdsBody({ rows, onClose, onDone }: { rows: IPBanEntry[]; onClose: () => void; onDone: () => void }) {
  const now = useNow();
  const [note, setNote] = useState("");
  const unban = useUnbanIdsMutation();
  const active = useMemo(() => rows.filter((ban) => isActiveBan(ban, now)), [rows, now]);
  const skipped = rows.length - active.length;
  const risk = active.filter((ban) => ban.source === "risk").length;
  const tooMany = active.length > MAX_UNBAN_BATCH;
  const single = rows.length === 1 ? rows[0] : null;

  return (
    <>
      <DialogHeader className="border-b px-5 py-4 pr-12">
        <DialogTitle>{single ? "解封 IP" : `解封 ${active.length.toLocaleString("zh-CN")} 个 IP`}</DialogTitle>
        <DialogDescription>解封后立即放行，所有副本同步生效。</DialogDescription>
      </DialogHeader>
      <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto px-5 py-4">
        {single ? (
          <div className="flex min-w-0 flex-col gap-1 rounded-lg border bg-muted/30 px-3 py-2.5">
            <span className="truncate font-mono text-sm font-medium" title={single.ip}>
              {single.ip}
            </span>
            <span className="truncate text-xs text-muted-foreground">
              {single.reason || "无原因"} · {banTimeState(single, now).text}
            </span>
          </div>
        ) : (
          <IPSample ips={active.map((ban) => ban.ip)} />
        )}
        {skipped > 0 ? (
          <p className="flex items-center gap-1.5 text-xs text-muted-foreground">
            <CircleSlash className="size-3.5 shrink-0" />
            选中的 {skipped.toLocaleString("zh-CN")} 条已过期或已解除，将跳过。
          </p>
        ) : null}
        {tooMany ? <FieldError message={`一次最多解封 ${MAX_UNBAN_BATCH} 条，请减少选择或改用按筛选解封。`} /> : null}
        <RiskHint count={risk} />
        <NoteField value={note} onChange={setNote} />
      </div>
      <DialogFooter className="border-t px-5 py-3">
        <Button variant="ghost" onClick={onClose}>
          取消
        </Button>
        <Button
          disabled={active.length === 0 || tooMany || unban.isPending}
          onClick={() => unban.mutate({ ids: active.map((ban) => ban.id), note: note.trim() || undefined }, { onSuccess: onDone })}
        >
          {unban.isPending ? <Loader2 className="animate-spin" /> : null}
          {active.length === 0 ? "没有可解封的记录" : `解封 ${active.length.toLocaleString("zh-CN")} 条`}
        </Button>
      </DialogFooter>
    </>
  );
}

function UnbanFilterBody({ filter, onClose, onDone }: { filter: BanFilter; onClose: () => void; onDone: () => void }) {
  const modes = useBanModes();
  const [note, setNote] = useState("");
  const [conflict, setConflict] = useState(false);
  const activeFilter = useMemo<BanFilter>(() => ({ ...filter, page: undefined, pageSize: undefined, status: "active" }), [filter]);
  const countQuery = useBanCountQuery(activeFilter, true);
  const riskFilter = useMemo<BanFilter>(() => ({ ...activeFilter, source: "risk" }), [activeFilter]);
  const riskQuery = useBanCountQuery(riskFilter, !filter.source);
  const unban = useUnbanFilterMutation();

  const expected = countQuery.data?.total ?? null;
  const risk = filter.source === "risk" ? expected : filter.source ? 0 : (riskQuery.data?.total ?? null);
  const summary = describeFilter(filter, modes);
  const counting = countQuery.isFetching;

  return (
    <>
      <DialogHeader className="border-b px-5 py-4 pr-12">
        <DialogTitle>按筛选解封</DialogTitle>
        <DialogDescription>解封当前条件下全部生效中的封禁，已过期、已解除的不受影响。</DialogDescription>
      </DialogHeader>
      <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto px-5 py-4">
        <div className="flex min-w-0 items-center gap-3 rounded-lg border bg-muted/30 px-3.5 py-3">
          <ShieldBan className="size-5 shrink-0 text-rose-500" />
          <div className="min-w-0 flex-1">
            <div className="text-xs text-muted-foreground">将解封</div>
            <div className="font-data text-2xl leading-8 font-semibold tabular-nums">
              {expected === null ? <span className="text-muted-foreground">—</span> : expected.toLocaleString("zh-CN")}
              <span className="ml-1 text-xs font-normal text-muted-foreground">条</span>
            </div>
          </div>
          {counting ? <Loader2 className="size-4 animate-spin text-muted-foreground" /> : null}
        </div>
        <div className="flex min-w-0 flex-col gap-1.5">
          <span className="text-xs font-medium">筛选条件</span>
          {summary.length > 0 ? (
            <div className="flex flex-wrap gap-1">
              {summary.map((item) => (
                <span key={item} className="inline-flex h-6 max-w-full items-center truncate rounded-md border bg-background px-2 text-[11px]">
                  {item}
                </span>
              ))}
            </div>
          ) : (
            <p className="flex items-center gap-1.5 text-xs text-amber-700 dark:text-amber-400">
              <AlertTriangle className="size-3.5 shrink-0" />
              没有附加条件：将解封全部生效中的封禁。
            </p>
          )}
        </div>
        {expected !== null && expected > MAX_UNBAN_BATCH ? (
          <p className="text-xs text-muted-foreground">单次最多处理 {MAX_UNBAN_BATCH.toLocaleString("zh-CN")} 条，剩余部分需要再执行一次。</p>
        ) : null}
        {conflict ? (
          <div className="flex items-start gap-2 rounded-lg border border-amber-500/40 bg-amber-500/5 px-3 py-2.5 text-xs leading-5">
            <AlertTriangle className="mt-0.5 size-3.5 shrink-0 text-amber-600 dark:text-amber-400" />
            <p>确认期间匹配的条数发生了变化（有新封禁或已被他人解封），上方已刷新为最新条数，请核对后再执行。</p>
          </div>
        ) : null}
        <RiskHint count={risk} />
        <NoteField value={note} onChange={setNote} />
      </div>
      <DialogFooter className="border-t px-5 py-3">
        <Button variant="ghost" onClick={onClose}>
          取消
        </Button>
        <Button
          disabled={!expected || counting || unban.isPending}
          onClick={() => {
            if (!expected) return;
            setConflict(false);
            unban.mutate(
              { filter: activeFilter, expected, note: note.trim() || undefined },
              {
                onSuccess: onDone,
                onError: (error) => {
                  if (error instanceof ApiError && error.code === 40990) {
                    setConflict(true);
                    void countQuery.refetch();
                    void riskQuery.refetch();
                  }
                }
              }
            );
          }}
        >
          {unban.isPending ? <Loader2 className="animate-spin" /> : null}
          {expected === 0 ? "没有可解封的记录" : `解封 ${expected === null ? "" : expected.toLocaleString("zh-CN")} 条`}
        </Button>
      </DialogFooter>
    </>
  );
}

/* ── 修改 ── */

export function BanEditDialog({ target, onClose, onDone }: { target: EditTarget | null; onClose: () => void; onDone: () => void }) {
  return (
    <Dialog open={target !== null} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="flex max-h-[min(760px,calc(100dvh-2rem))] flex-col gap-0 overflow-hidden p-0 sm:max-w-lg">
        {target ? <EditBody key={target.key} target={target} onClose={onClose} onDone={onDone} /> : null}
      </DialogContent>
    </Dialog>
  );
}

function EditBody({ target, onClose, onDone }: { target: EditTarget; onClose: () => void; onDone: () => void }) {
  const now = useNow();
  const modes = useBanModes();
  const single = target.kind === "single" ? target.ban : null;
  const rows = useMemo(() => (target.kind === "single" ? [target.ban] : target.rows), [target]);
  const active = useMemo(() => rows.filter((ban) => isActiveBan(ban, now)), [rows, now]);
  const showMode = target.kind !== "duration";
  const showDuration = target.kind !== "mode";

  const [mode, setMode] = useState<string>(single?.mode ?? "");
  const [reason, setReason] = useState(single?.reason ?? "");
  const [duration, setDuration] = useState<number | null>(target.kind === "duration" ? 86_400 : null);
  const [delayMs, setDelayMs] = useState(single?.delayMs ?? 0);
  const [failures, setFailures] = useState<BanBatchItem[] | null>(null);

  const updateOne = useUpdateBanMutation();
  const updateMany = useBatchUpdateBansMutation();
  const pending = updateOne.isPending || updateMany.isPending;

  const input: BanUpdateInput = {};
  if (showMode && (!single || mode !== single.mode)) input.mode = mode as BanUpdateInput["mode"];
  if (single && reason.trim() !== single.reason) input.reason = reason.trim();
  if (showDuration && duration !== null) input.duration = duration;
  if (showMode && mode === "tarpit" && (!single || delayMs !== (single.delayMs ?? 0))) input.delayMs = delayMs;
  const changed = Object.keys(input).length > 0;
  const tooMany = active.length > MAX_BAN_BATCH;

  const title = single ? "修改封禁" : target.kind === "mode" ? `修改响应模式 · ${active.length} 条` : `修改时长 · ${active.length} 条`;

  const submit = () => {
    if (single) {
      updateOne.mutate({ id: single.id, input }, { onSuccess: onDone });
      return;
    }
    updateMany.mutate(
      { ids: active.map((ban) => ban.id), input },
      {
        onSuccess: (result: BanBatchResult) => {
          const failed = result.items.filter((item) => item.status === "failed");
          if (failed.length > 0) setFailures(failed);
          else onDone();
        }
      }
    );
  };

  if (failures) {
    const ipOf = new Map(rows.map((ban) => [ban.id, ban.ip] as const));
    return (
      <>
        <DialogHeader className="border-b px-5 py-4 pr-12">
          <DialogTitle>部分修改失败</DialogTitle>
          <DialogDescription>
            成功 {active.length - failures.length} 条，失败 {failures.length} 条。
          </DialogDescription>
        </DialogHeader>
        <div className="min-h-0 flex-1 overflow-y-auto px-5 py-4">
          <ResultList items={failures.map((item) => ({ ...item, ip: item.ip || ipOf.get(item.banId ?? 0) || `#${item.banId}` }))} />
        </div>
        <DialogFooter className="border-t px-5 py-3">
          <Button onClick={onDone}>完成</Button>
        </DialogFooter>
      </>
    );
  }

  return (
    <>
      <DialogHeader className="border-b px-5 py-4 pr-12">
        <DialogTitle>{title}</DialogTitle>
        <DialogDescription>{single ? "只改动过的项会提交。" : "只修改选中且仍在生效的封禁。"}</DialogDescription>
      </DialogHeader>
      <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto px-5 py-4">
        {single ? (
          <div className="flex min-w-0 items-center justify-between gap-3 rounded-lg border bg-muted/30 px-3 py-2.5">
            <span className="min-w-0 truncate font-mono text-sm font-medium" title={single.ip}>
              {single.ip}
            </span>
            <span className="shrink-0 text-xs text-muted-foreground">{banTimeState(single, now).text}</span>
          </div>
        ) : (
          <>
            <IPSample ips={active.map((ban) => ban.ip)} limit={10} />
            {rows.length > active.length ? (
              <p className="text-xs text-muted-foreground">另有 {rows.length - active.length} 条已失效，将跳过。</p>
            ) : null}
          </>
        )}
        {single && !isActiveBan(single, now) ? <FieldError message="这条封禁已失效，不能修改；需要的话重新封禁。" /> : null}
        {tooMany ? <FieldError message={`一次最多修改 ${MAX_BAN_BATCH} 条。`} /> : null}

        {showMode ? (
          <FormBlock label="响应模式">
            <ModeSelect value={mode} onChange={setMode} modes={modes} />
          </FormBlock>
        ) : null}
        {showMode && mode === "tarpit" ? (
          <FormBlock label="拖延时间">
            <TarpitField value={delayMs} onChange={setDelayMs} />
          </FormBlock>
        ) : null}
        {single ? (
          <FormBlock label="原因" htmlFor="ban-edit-reason">
            <Input id="ban-edit-reason" value={reason} maxLength={200} onChange={(event) => setReason(event.target.value)} />
          </FormBlock>
        ) : null}
        {showDuration ? (
          <FormBlock label="时长" hint="从现在起重新计算">
            <DurationPicker value={duration} onChange={setDuration} keepLabel={single ? "保持不变" : undefined} />
          </FormBlock>
        ) : null}
      </div>
      <DialogFooter className="border-t px-5 py-3">
        <Button variant="ghost" onClick={onClose}>
          取消
        </Button>
        <Button disabled={!changed || pending || active.length === 0 || tooMany} onClick={submit}>
          {pending ? <Loader2 className="animate-spin" /> : null}
          {single ? "保存" : `修改 ${active.length} 条`}
        </Button>
      </DialogFooter>
    </>
  );
}

/* ── 批量封禁 ── */

const REASON_PRESETS = ["扫描探测", "暴力破解", "恶意爬虫", "刷接口", "撞库"] as const;

export function BatchBanDialog({ seed, onClose }: { seed: BanSeed | null; onClose: () => void }) {
  return (
    <Dialog open={seed !== null} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="flex max-h-[min(860px,calc(100dvh-2rem))] flex-col gap-0 overflow-hidden p-0 sm:max-w-2xl">
        {seed ? <BatchBanBody key={seed.key} initial={seed.ips ?? ""} onClose={onClose} /> : null}
      </DialogContent>
    </Dialog>
  );
}

type ResultItem = { ip: string; status: string; error?: string };

function BatchBanBody({ initial, onClose }: { initial: string; onClose: () => void }) {
  const modes = useBanModes();
  const [text, setText] = useState(initial);
  const [reason, setReason] = useState("");
  const [duration, setDuration] = useState<number | null>(86_400);
  const [mode, setMode] = useState("");
  const [delayMs, setDelayMs] = useState(0);
  const [result, setResult] = useState<{ data: BanBatchResult; skipped: ResultItem[] } | null>(null);
  const ban = useBatchBanMutation();

  const parsed = useMemo(() => parseIPList(text), [text]);
  const bad: ResultItem[] = useMemo(
    () => [
      ...parsed.cidrs.map((ip) => ({ ip, status: "cidr", error: "网段请到「访问控制 → 黑名单网段」里加" })),
      ...parsed.invalid.map((ip) => ({ ip, status: "invalid", error: "不是合法的 IP 地址" }))
    ],
    [parsed]
  );
  const tooMany = parsed.valid.length > MAX_BAN_BATCH;

  const restart = () => {
    setResult(null);
    setText("");
    ban.reset();
  };

  if (result) return <BatchBanResult result={result.data} skipped={result.skipped} onAgain={restart} onClose={onClose} />;

  const submit = () =>
    ban.mutate(
      {
        ips: parsed.valid,
        reason: reason.trim() || "手动封禁",
        duration: duration ?? 0,
        mode: mode as IPBanMode,
        delayMs: mode === "tarpit" && delayMs > 0 ? delayMs : undefined
      },
      { onSuccess: (data) => setResult({ data, skipped: bad }) }
    );

  return (
    <>
      <DialogHeader className="border-b px-5 py-4 pr-12">
        <DialogTitle>封禁 IP</DialogTitle>
        <DialogDescription>一次最多 {MAX_BAN_BATCH.toLocaleString("zh-CN")} 个；已在封禁中的会跳过。</DialogDescription>
      </DialogHeader>
      <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto px-5 py-4">
        <FormBlock label="IP 地址" hint="每行一个，或用逗号、空格分隔" htmlFor="ban-ips">
          <Textarea
            id="ban-ips"
            value={text}
            onChange={(event) => setText(event.target.value)}
            rows={7}
            spellCheck={false}
            placeholder={"203.0.113.7\n198.51.100.23, 198.51.100.24\n2001:db8::1"}
            className="max-h-64 min-h-36 font-mono text-xs leading-5"
          />
          <div className="flex flex-wrap items-center gap-1.5">
            <CountChip tone="emerald" label="有效" value={parsed.valid.length} />
            <CountChip tone="muted" label="重复" value={parsed.duplicates.length} />
            <CountChip tone="rose" label="无效" value={parsed.invalid.length} />
            <CountChip tone="amber" label="网段" value={parsed.cidrs.length} />
          </div>
          {bad.length > 0 ? (
            <div className="max-h-32 overflow-y-auto rounded-lg border bg-muted/30 px-3 py-2">
              <ul className="flex flex-col gap-1">
                {bad.slice(0, 50).map((item, index) => (
                  <li key={`${item.ip}-${index}`} className="flex min-w-0 items-baseline gap-2 text-[11px]">
                    <span className={cn("min-w-0 truncate font-mono", item.status === "cidr" ? "text-amber-700 dark:text-amber-400" : "text-rose-600 dark:text-rose-400")}>
                      {item.ip}
                    </span>
                    <span className="shrink-0 text-muted-foreground">{item.error}</span>
                  </li>
                ))}
                {bad.length > 50 ? <li className="text-[11px] text-muted-foreground">还有 {bad.length - 50} 项未列出</li> : null}
              </ul>
            </div>
          ) : null}
          {tooMany ? <FieldError message={`有效地址 ${parsed.valid.length} 个，超过单次上限 ${MAX_BAN_BATCH} 个，请分批提交。`} /> : null}
        </FormBlock>

        <FormBlock label="原因" htmlFor="ban-reason">
          <Input id="ban-reason" value={reason} maxLength={200} onChange={(event) => setReason(event.target.value)} placeholder="手动封禁" />
          <div className="flex flex-wrap gap-1">
            {REASON_PRESETS.map((preset) => (
              <button
                key={preset}
                type="button"
                onClick={() => setReason(preset)}
                className={cn(
                  "inline-flex h-6 items-center rounded-md border px-2 text-[11px] text-muted-foreground transition-colors hover:bg-accent hover:text-foreground",
                  reason === preset && "border-foreground/25 bg-muted text-foreground"
                )}
              >
                {preset}
              </button>
            ))}
          </div>
        </FormBlock>

        <FormBlock label="时长" hint={duration === null ? undefined : durationLabel(duration)}>
          <DurationPicker value={duration} onChange={setDuration} />
        </FormBlock>

        <FormBlock label="响应模式">
          <ModeSelect value={mode} onChange={setMode} modes={modes} />
        </FormBlock>
        {mode === "tarpit" ? (
          <FormBlock label="拖延时间">
            <TarpitField value={delayMs} onChange={setDelayMs} />
          </FormBlock>
        ) : null}
      </div>
      <DialogFooter className="flex-col gap-2 border-t px-5 py-3 sm:flex-row sm:items-center">
        <FieldError message={ban.error ? banErrorMessage(ban.error) : null} className="min-w-0 flex-1 sm:truncate" />
        {!ban.error ? <span className="hidden flex-1 sm:block" /> : null}
        <div className="flex justify-end gap-2">
          <Button variant="ghost" onClick={onClose}>
            取消
          </Button>
          <Button variant="destructive" disabled={parsed.valid.length === 0 || tooMany || ban.isPending} onClick={submit}>
            {ban.isPending ? <Loader2 className="animate-spin" /> : <ShieldBan />}
            {parsed.valid.length > 0 ? `封禁 ${parsed.valid.length.toLocaleString("zh-CN")} 个` : "封禁"}
          </Button>
        </div>
      </DialogFooter>
    </>
  );
}

const CHIP_TONE = {
  emerald: "border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300",
  rose: "border-rose-500/30 bg-rose-500/10 text-rose-700 dark:text-rose-300",
  amber: "border-amber-500/30 bg-amber-500/10 text-amber-700 dark:text-amber-300",
  muted: "border-border bg-muted/40 text-muted-foreground"
} as const;

function CountChip({ tone, label, value }: { tone: keyof typeof CHIP_TONE; label: string; value: number }) {
  return (
    <span className={cn("inline-flex h-6 items-center gap-1 rounded-md border px-2 text-[11px]", value > 0 ? CHIP_TONE[tone] : CHIP_TONE.muted, value === 0 && "opacity-70")}>
      {label}
      <span className="font-data font-semibold tabular-nums">{value.toLocaleString("zh-CN")}</span>
    </span>
  );
}

function BatchBanResult({ result, skipped, onAgain, onClose }: { result: BanBatchResult; skipped: ResultItem[]; onAgain: () => void; onClose: () => void }) {
  const created = result.items.filter((item) => item.status === "created");
  const exists = result.items.filter((item) => item.status === "exists");
  const invalid = [...result.items.filter((item) => item.status === "invalid"), ...skipped];
  const failed = result.items.filter((item) => item.status === "failed");

  return (
    <>
      <DialogHeader className="border-b px-5 py-4 pr-12">
        <DialogTitle>封禁结果</DialogTitle>
        <DialogDescription>新封禁立即生效，所有副本同步。</DialogDescription>
      </DialogHeader>
      <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto px-5 py-4">
        <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
          <ResultTile icon={CheckCircle2} tone="text-emerald-600 dark:text-emerald-400" label="新增封禁" value={created.length} />
          <ResultTile icon={CircleSlash} tone="text-muted-foreground" label="已在封禁中" value={exists.length} />
          <ResultTile icon={AlertTriangle} tone="text-amber-600 dark:text-amber-400" label="无效 / 网段" value={invalid.length} />
          <ResultTile icon={XCircle} tone="text-rose-600 dark:text-rose-400" label="失败" value={failed.length} />
        </div>
        {created.length > 0 ? (
          <section className="flex min-w-0 flex-col gap-1.5">
            <h4 className="text-xs font-medium">新增</h4>
            <IPSample ips={created.map((item) => item.ip)} limit={24} />
          </section>
        ) : null}
        {exists.length > 0 ? (
          <section className="flex min-w-0 flex-col gap-1.5">
            <h4 className="text-xs font-medium">已在封禁中（未改动）</h4>
            <IPSample ips={exists.map((item) => item.ip)} limit={24} />
          </section>
        ) : null}
        {invalid.length + failed.length > 0 ? (
          <section className="flex min-w-0 flex-col gap-1.5">
            <h4 className="text-xs font-medium">未封禁</h4>
            <ResultList items={[...failed, ...invalid]} />
          </section>
        ) : null}
      </div>
      <DialogFooter className="border-t px-5 py-3">
        <Button variant="outline" onClick={onAgain}>
          继续封禁
        </Button>
        <Button onClick={onClose}>完成</Button>
      </DialogFooter>
    </>
  );
}

function ResultTile({ icon: Icon, tone, label, value }: { icon: typeof CheckCircle2; tone: string; label: string; value: number }) {
  return (
    <div className="flex min-w-0 flex-col gap-1 rounded-lg border px-3 py-2.5">
      <span className="flex min-w-0 items-center gap-1.5 text-[11px] text-muted-foreground">
        <Icon className={cn("size-3.5 shrink-0", tone)} />
        <span className="truncate">{label}</span>
      </span>
      <span className="font-data text-xl leading-7 font-semibold tabular-nums">{value.toLocaleString("zh-CN")}</span>
    </div>
  );
}

function ResultList({ items }: { items: ResultItem[] }) {
  return (
    <ul className="flex max-h-56 flex-col divide-y overflow-y-auto rounded-lg border">
      {items.map((item, index) => (
        <li key={`${item.ip}-${index}`} className="flex min-w-0 items-baseline gap-3 px-3 py-1.5 text-xs">
          <span className="min-w-0 shrink truncate font-mono" title={item.ip}>
            {item.ip}
          </span>
          <span className="ml-auto min-w-0 truncate text-right text-[11px] text-muted-foreground" title={item.error}>
            {item.error || item.status}
          </span>
        </li>
      ))}
    </ul>
  );
}
