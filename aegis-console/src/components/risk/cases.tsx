"use client";

import { useMemo, useState, type ReactNode } from "react";
import { toast } from "sonner";
import { CheckCircle2, ClipboardCheck, ExternalLink, Hourglass, Inbox, Loader2, RotateCcw, ShieldCheck, ShieldX, XCircle } from "lucide-react";

import { CardEmpty } from "@/components/overview/overview-kit";
import { ListPagination } from "@/components/payment/payment-list-parts";
import { Segmented, TabStack } from "@/components/traffic/traffic-kit";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Textarea } from "@/components/ui/textarea";
import type { CaseStats, DisposeInput, ListKind, RiskCase } from "@/lib/api/risk";
import { useReviewCase, useRiskCaseStats, useRiskCases, useRiskScenes } from "@/lib/risk-hooks";
import { useDebouncedValue } from "@/lib/use-debounced-value";
import { cn } from "@/lib/utils";

import { ACTION_UI, ActionGlyph, DecisionDetailDialog, ErrorState, Fact, ScoreDial, ScorePill, errorText, useSceneName } from "./decisions";
import { useAppName } from "./kit/app-scope";
import { ACTION_RESULT, caseStatus, scoreWord, subjectOf, when } from "./kit/humanize";
import { DURATION_PRESETS, KIND_LABEL } from "./kit/labels";

/**
 * 复核单：人工判断风控结论是否成立。
 *
 * 待复核按风险高低排成看板（极高、高、中、低四列，列内等得最久的在上），已结案是一列结论记录。
 * 页头一句话说清还有多少要处理、等了多久，分布条标出各风险段的积压；统计取全局，不只当前页。
 */

const STATUS = [
  { value: "pending", label: "待复核" },
  { value: "closed", label: "已结案" },
] as const;

type Status = (typeof STATUS)[number]["value"];

const PAGE_SIZE: Record<Status, number> = { pending: 60, closed: 30 };

const SOURCE_LABEL: Record<string, string> = { decision: "风控决策", cep: "行为模式", manual: "手工创建", review: "复核" };

/** 复核结论的说法，与后端一致：approve = 放行（判定为正常），reject = 确认有风险；文字取自统一文案层 */
const VERDICT: Record<"approve" | "reject", { label: string; short: string; text: string; soft: string; icon: typeof CheckCircle2 }> = {
  approve: { label: caseStatus({ verdict: "approve" }).label, short: "正常", text: "text-emerald-600 dark:text-emerald-400", soft: "bg-emerald-500/12", icon: ShieldCheck },
  reject: { label: caseStatus({ verdict: "reject" }).label, short: "有风险", text: "text-red-600 dark:text-red-400", soft: "bg-red-500/12", icon: ShieldX },
};

/** 已等待多久：「35 分钟」「6 小时」「3 天」 */
function waited(iso: string): { text: string; hours: number } {
  const ms = Date.now() - new Date(iso).getTime();
  if (!Number.isFinite(ms) || ms < 0) return { text: "刚刚", hours: 0 };
  const m = Math.floor(ms / 60_000);
  const h = ms / 3_600_000;
  if (m < 1) return { text: "不到 1 分钟", hours: h };
  if (m < 60) return { text: `${m} 分钟`, hours: h };
  if (h < 48) return { text: `${Math.floor(h)} 小时`, hours: h };
  return { text: `${Math.floor(h / 24)} 天`, hours: h };
}

const caseHeadline = (c: RiskCase, sceneName: (k: string) => string) => `${subjectOf(c)} 的${sceneName(c.scene)}请求`;

const COLUMNS = [
  { key: "critical", label: "极高风险", min: 80, max: Infinity, dot: "bg-red-500", head: "text-red-700 dark:text-red-300" },
  { key: "high", label: "高风险", min: 60, max: 80, dot: "bg-amber-500", head: "text-amber-700 dark:text-amber-300" },
  { key: "medium", label: "中等风险", min: 30, max: 60, dot: "bg-sky-500", head: "text-sky-700 dark:text-sky-300" },
  { key: "low", label: "低风险", min: -Infinity, max: 30, dot: "bg-emerald-500", head: "text-emerald-700 dark:text-emerald-300" },
] as const;

export function RiskCases({ app }: { app?: number }) {
  const [status, setStatus] = useState<Status>("pending");
  const [scene, setScene] = useState("");
  const [user, setUser] = useState("");
  const [page, setPage] = useState(1);
  const [openCase, setOpenCase] = useState<RiskCase | null>(null);
  const scenes = useRiskScenes();
  const sceneName = useSceneName();
  const debouncedUser = useDebouncedValue(user);
  const userId = Number(debouncedUser.trim());
  const size = PAGE_SIZE[status];

  const query = useRiskCases({
    status,
    app,
    scene: scene || undefined,
    user: Number.isFinite(userId) && userId > 0 ? userId : undefined,
    page,
    limit: size,
  });
  const rows = useMemo(() => query.data?.list ?? [], [query.data]);
  const total = query.data?.total ?? 0;
  const closed = status === "closed";
  const filtered = !!scene || !!user.trim();
  // 换了状态，上一状态的旧数据还在占位：此时不画，免得看板里出现已结案的卡片
  const stale = !!query.data && rows.some((r) => (closed ? r.status !== "closed" : r.status !== "pending"));
  // 全局统计不分场景与用户：没筛选时页头、分布条与列计数用它，筛选时退回按本页计算
  const caseStats = useRiskCaseStats(app);
  const stats = !filtered && caseStats.data ? caseStats.data : null;
  const statsLoading = !filtered && caseStats.isLoading;
  const busy = query.isLoading || stale || statsLoading;

  return (
    <TabStack>
      <section className="flex min-w-0 flex-col gap-4 rounded-2xl border border-border bg-card p-4 sm:p-5">
        <div className="flex min-w-0 flex-wrap items-start justify-between gap-3">
          <div className="flex min-h-12 min-w-0 flex-col gap-1">
            {busy ? (
              <>
                <Skeleton className="h-6 w-64 max-w-full" />
                <Skeleton className="h-4 w-48 max-w-full" />
              </>
            ) : (
              <CasesSummary rows={rows} total={total} closed={closed} filtered={filtered} stats={stats} />
            )}
          </div>
          <Segmented
            value={status}
            options={STATUS}
            onChange={(v) => {
              setStatus(v);
              setPage(1);
            }}
            ariaLabel="复核状态"
          />
        </div>

        {busy ? <Skeleton className="h-[52px] w-full rounded-xl" /> : closed ? <VerdictStrip rows={rows} stats={stats} /> : <LevelStrip rows={rows} stats={stats} />}

        <div className="flex min-w-0 flex-wrap items-center gap-2">
          <Select
            value={scene || "all"}
            onValueChange={(v) => {
              setScene(v === "all" ? "" : v);
              setPage(1);
            }}
          >
            <SelectTrigger size="sm" className="h-8 w-40">
              <SelectValue placeholder="全部场景" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">全部场景</SelectItem>
              {(scenes.data ?? []).map((s) => (
                <SelectItem key={s.key} value={s.key}>
                  {s.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Input
            className="h-8 w-36"
            inputMode="numeric"
            placeholder="用户编号"
            value={user}
            onChange={(e) => {
              setUser(e.target.value);
              setPage(1);
            }}
          />
          {filtered ? (
            <Button
              size="sm"
              variant="ghost"
              className="h-8"
              onClick={() => {
                setScene("");
                setUser("");
                setPage(1);
              }}
            >
              <RotateCcw />
              清除筛选
            </Button>
          ) : null}
          {query.isFetching && !query.isLoading ? <span className="ml-auto text-xs text-muted-foreground">正在刷新</span> : null}
        </div>
      </section>

      {query.isError && !query.data ? (
        <section className="rounded-2xl border border-border bg-card">
          <ErrorState error={query.error} onRetry={() => query.refetch()} />
        </section>
      ) : query.isLoading || stale ? (
        closed ? <ClosedSkeleton /> : <BoardSkeleton />
      ) : rows.length === 0 ? (
        <section className="flex flex-col items-center rounded-2xl border border-border bg-card pb-6">
          <CardEmpty
            icon={closed ? Inbox : CheckCircle2}
            title={filtered ? "没有符合条件的复核单" : closed ? "还没有结案记录" : "复核单已全部处理"}
            description={filtered ? "可以清除筛选后再看" : closed ? "完成复核的单子会出现在这里" : "风控转人工复核时，新的复核单会出现在这里"}
          />
        </section>
      ) : (
        <>
          {closed ? (
            <ClosedList rows={rows} sceneName={sceneName} onOpen={setOpenCase} />
          ) : (
            <Board rows={rows} stats={stats} sceneName={sceneName} onOpen={setOpenCase} />
          )}
          {total > size ? (
            <div className="overflow-hidden rounded-2xl border border-border bg-card">
              <ListPagination page={page} totalPages={Math.max(1, Math.ceil(total / size))} total={total} unit="张" onPage={setPage} />
            </div>
          ) : null}
        </>
      )}

      <Dialog open={!!openCase} onOpenChange={(o) => !o && setOpenCase(null)}>
        <DialogContent className="max-h-[90vh] gap-5 overflow-y-auto sm:max-w-2xl">
          {openCase ? <CaseReview key={openCase.id} item={openCase} onClose={() => setOpenCase(null)} /> : null}
        </DialogContent>
      </Dialog>
    </TabStack>
  );
}

/* ── 页头 ── */

type Level = (typeof COLUMNS)[number]["key"];

/** 本页按风险分段计数（筛选时用；不筛选时用全局统计） */
function pageLevels(rows: RiskCase[]): Record<Level, number> {
  const out: Record<Level, number> = { critical: 0, high: 0, medium: 0, low: 0 };
  for (const r of rows) {
    const col = COLUMNS.find((c) => r.score >= c.min && r.score < c.max);
    if (col) out[col.key] += 1;
  }
  return out;
}

function CasesSummary({ rows, total, closed, filtered, stats }: { rows: RiskCase[]; total: number; closed: boolean; filtered: boolean; stats: CaseStats | null }) {
  const scope = filtered ? "符合条件的" : "";
  if (closed) {
    const approve = stats ? stats.closed30d.approve ?? 0 : rows.filter((r) => r.verdict === "approve").length;
    const reject = stats ? stats.closed30d.reject ?? 0 : rows.filter((r) => r.verdict === "reject").length;
    const lead = stats ? "近 30 天" : rows.length < total ? `本页 ${rows.length} 张中，` : "";
    return (
      <>
        <h3 className="text-base leading-6 font-semibold tracking-tight">
          {total ? `${scope}已结案的复核单共 ${total.toLocaleString("zh-CN")} 张` : `暂无${scope}结案记录`}
        </h3>
        <p className="min-h-5 text-sm leading-5 text-muted-foreground">
          {approve + reject ? `${lead}${approve} 张判定为正常，${reject} 张确认有风险。` : stats ? "近 30 天没有结案。" : ""}
        </p>
      </>
    );
  }
  const pending = stats ? stats.pending : total;
  const levels = stats ? stats.pendingByLevel : pageLevels(rows);
  const high = (levels.critical ?? 0) + (levels.high ?? 0);
  const oldestAt = stats ? stats.oldestPendingAt : rows.reduce<string | undefined>((o, r) => (!o || r.createdAt < o ? r.createdAt : o), undefined);
  return (
    <>
      <h3 className="text-base leading-6 font-semibold tracking-tight">
        {pending ? `${pending.toLocaleString("zh-CN")} 张${scope}复核单等待处理${high ? `，其中 ${high} 张为高风险` : ""}` : `没有${scope}待处理的复核单`}
      </h3>
      <p className="min-h-5 text-sm leading-5 text-muted-foreground">
        {pending && oldestAt ? `等待最久的一张已有 ${waited(oldestAt).text}${rows.length < total ? `，当前显示 ${rows.length} 张` : ""}。` : ""}
      </p>
    </>
  );
}

function LevelStrip({ rows, stats }: { rows: RiskCase[]; stats: CaseStats | null }) {
  const levels = stats ? stats.pendingByLevel : pageLevels(rows);
  const parts = COLUMNS.map((c) => ({ label: c.label, count: levels[c.key] ?? 0, bar: c.dot }));
  return <Strip label={stats ? "待复核的风险分布" : "本页风险分布"} parts={parts} sum={parts.reduce((s, p) => s + p.count, 0)} />;
}

function VerdictStrip({ rows, stats }: { rows: RiskCase[]; stats: CaseStats | null }) {
  const approve = stats ? stats.closed30d.approve ?? 0 : rows.filter((r) => r.verdict === "approve").length;
  const reject = stats ? stats.closed30d.reject ?? 0 : rows.filter((r) => r.verdict === "reject").length;
  return (
    <Strip
      label={stats ? "近 30 天复核结论" : "本页复核结论"}
      sum={approve + reject}
      parts={[
        { label: VERDICT.reject.label, count: reject, bar: "bg-red-500" },
        { label: VERDICT.approve.label, count: approve, bar: "bg-emerald-500" },
      ]}
    />
  );
}

function Strip({ label, parts, sum }: { label: string; parts: Array<{ label: string; count: number; bar: string }>; sum: number }) {
  return (
    <div className="flex min-w-0 flex-col gap-2 rounded-xl bg-muted/40 px-3 py-2.5">
      <div className="flex h-2 w-full min-w-0 gap-0.5 overflow-hidden rounded-full bg-muted">
        {sum > 0
          ? parts
              .filter((p) => p.count > 0)
              .map((p) => <span key={p.label} className={cn("h-full", p.bar)} style={{ flexGrow: p.count / sum, flexBasis: 0 }} />)
          : null}
      </div>
      <div className="flex min-w-0 flex-wrap items-center gap-x-4 gap-y-1 text-xs">
        <span className="font-medium text-muted-foreground">{label}</span>
        {parts.map((p) => (
          <span key={p.label} className={cn("inline-flex items-center gap-1.5", p.count === 0 && "opacity-50")}>
            <span aria-hidden className={cn("size-2 rounded-[3px]", p.bar)} />
            <span className="text-muted-foreground">{p.label}</span>
            <span className="font-medium tabular-nums">{p.count.toLocaleString("zh-CN")}</span>
          </span>
        ))}
      </div>
    </div>
  );
}

/* ── 待复核看板 ── */

function Board({
  rows,
  stats,
  sceneName,
  onOpen,
}: {
  rows: RiskCase[];
  stats: CaseStats | null;
  sceneName: (k: string) => string;
  onOpen: (c: RiskCase) => void;
}) {
  const columns = COLUMNS.map((col) => {
    const items = rows.filter((r) => r.score >= col.min && r.score < col.max).sort((a, b) => a.createdAt.localeCompare(b.createdAt));
    // 列计数取全局统计；统计刚结案后可能略滞后，不让它小于本页实际张数
    const count = Math.max(stats ? stats.pendingByLevel[col.key] ?? 0 : items.length, items.length);
    return { ...col, items, count };
  });
  return (
    <div className="grid min-w-0 items-start gap-4 @2xl:grid-cols-2 @5xl:grid-cols-4">
      {columns.map((col) => (
        <section key={col.key} className="flex min-w-0 flex-col gap-3 rounded-2xl border border-border bg-muted/30 p-3">
          <header className="flex items-center gap-2 px-1">
            <span aria-hidden className={cn("size-2.5 rounded-full", col.dot)} />
            <h3 className={cn("text-sm font-semibold", col.head)}>{col.label}</h3>
            <span className="ml-auto rounded-full bg-background px-2 text-xs leading-5 font-medium tabular-nums">{col.count.toLocaleString("zh-CN")}</span>
          </header>
          {col.items.length === 0 ? (
            <p className="rounded-xl border border-dashed px-3 py-6 text-center text-xs text-muted-foreground">
              {col.count ? `${col.count.toLocaleString("zh-CN")} 张在其他页` : "暂无"}
            </p>
          ) : (
            <ul className="flex min-w-0 flex-col gap-2">
              {col.items.map((c) => (
                <li key={c.id}>
                  <CaseCard item={c} sceneName={sceneName} onOpen={() => onOpen(c)} />
                </li>
              ))}
              {col.count > col.items.length ? (
                <li className="px-1 text-center text-[11px] text-muted-foreground">另有 {(col.count - col.items.length).toLocaleString("zh-CN")} 张在其他页</li>
              ) : null}
            </ul>
          )}
        </section>
      ))}
    </div>
  );
}

function CaseCard({ item, sceneName, onOpen }: { item: RiskCase; sceneName: (k: string) => string; onOpen: () => void }) {
  const w = waited(item.createdAt);
  const t = when(item.createdAt);
  const reasons = (item.reasons ?? []).filter(Boolean);
  return (
    <button
      type="button"
      onClick={onOpen}
      className="flex w-full min-w-0 flex-col gap-2 rounded-xl border border-border bg-card p-3 text-left transition-colors hover:border-foreground/20 hover:bg-accent/40 focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none"
    >
      <div className="flex min-w-0 items-start gap-2.5">
        <ActionGlyph action={item.action} size="sm" />
        <div className="flex min-w-0 flex-1 flex-col">
          <span className="line-clamp-2 text-sm leading-5 font-medium">{caseHeadline(item, sceneName)}</span>
          <span className={cn("text-xs leading-5", ACTION_UI[item.action]?.text)}>{ACTION_RESULT[item.action] ?? item.action}</span>
        </div>
        <span className={cn("shrink-0 text-base leading-6 font-semibold tabular-nums")}>{item.score}</span>
      </div>
      <p className="line-clamp-2 text-xs leading-5 text-muted-foreground">
        {reasons.length ? reasons.join("；") : "未记录原因"}
      </p>
      <div className="flex min-w-0 items-center gap-2 border-t pt-2 text-[11px] text-muted-foreground">
        <span
          className={cn("inline-flex items-center gap-1 tabular-nums", w.hours >= 72 ? "text-red-600 dark:text-red-400" : w.hours >= 24 && "text-amber-600 dark:text-amber-400")}
          title={t.absolute}
        >
          <Hourglass className="size-3" />
          已等待 {w.text}
        </span>
        <span className="ml-auto truncate">{SOURCE_LABEL[item.source] ?? item.source}</span>
      </div>
    </button>
  );
}

function BoardSkeleton() {
  return (
    <div className="grid min-w-0 items-start gap-4 @2xl:grid-cols-2 @5xl:grid-cols-4" aria-hidden>
      {COLUMNS.map((col, i) => (
        <div key={col.key} className="flex min-w-0 flex-col gap-3 rounded-2xl border border-border bg-muted/30 p-3">
          <div className="flex items-center gap-2 px-1">
            <Skeleton className="size-2.5 rounded-full" />
            <Skeleton className="h-4 w-16" />
            <Skeleton className="ml-auto h-5 w-7 rounded-full" />
          </div>
          {Array.from({ length: 3 - (i % 2) }).map((_, j) => (
            <div key={j} className="flex flex-col gap-2 rounded-xl border bg-card p-3">
              <div className="flex gap-2.5">
                <Skeleton className="size-7 rounded-lg" />
                <div className="flex flex-1 flex-col gap-1.5">
                  <Skeleton className="h-4 w-4/5" />
                  <Skeleton className="h-3.5 w-1/3" />
                </div>
              </div>
              <Skeleton className="h-3.5 w-full" />
              <Skeleton className="h-3 w-1/2" />
            </div>
          ))}
        </div>
      ))}
    </div>
  );
}

/* ── 已结案 ── */

function ClosedList({ rows, sceneName, onOpen }: { rows: RiskCase[]; sceneName: (k: string) => string; onOpen: (c: RiskCase) => void }) {
  return (
    <section className="overflow-hidden rounded-2xl border border-border bg-card">
      <ul className="divide-y">
        {rows.map((c) => {
          const v = c.verdict ? VERDICT[c.verdict] : null;
          const Icon = v?.icon ?? ClipboardCheck;
          const t = when(c.reviewedAt ?? c.createdAt);
          return (
            <li key={c.id}>
              <button
                type="button"
                onClick={() => onOpen(c)}
                className="flex w-full min-w-0 items-center gap-3 px-4 py-3 text-left transition-colors hover:bg-muted/50 focus-visible:bg-muted/50 focus-visible:outline-none sm:px-5"
              >
                <span className={cn("grid size-9 shrink-0 place-items-center rounded-xl", v?.soft ?? "bg-muted", v?.text ?? "text-muted-foreground")} aria-hidden>
                  <Icon className="size-4.5" />
                </span>
                <div className="flex min-w-0 flex-1 flex-col gap-0.5">
                  <div className="flex min-w-0 flex-wrap items-center gap-x-2">
                    <span className="min-w-0 truncate text-sm font-medium">{caseHeadline(c, sceneName)}</span>
                    {v ? <span className={cn("text-sm font-medium", v.text)}>{v.label}</span> : null}
                  </div>
                  <p className="min-w-0 truncate text-xs leading-5 text-muted-foreground">
                    {c.note ? `复核说明：${c.note}` : (c.reasons ?? []).filter(Boolean).join("；") || "未填写复核说明"}
                  </p>
                  <div className="flex min-w-0 flex-wrap gap-x-3 text-[11px] leading-4 text-muted-foreground">
                    <span>{c.reviewer || "未知复核人"}</span>
                    <time title={t.absolute}>{t.relative}复核</time>
                    <span>系统结论为{ACTION_RESULT[c.action] ?? c.action}</span>
                  </div>
                </div>
                <ScorePill score={c.score} />
              </button>
            </li>
          );
        })}
      </ul>
    </section>
  );
}

function ClosedSkeleton() {
  return (
    <section className="overflow-hidden rounded-2xl border border-border bg-card" aria-hidden>
      <div className="divide-y">
        {Array.from({ length: 8 }).map((_, i) => (
          <div key={i} className="flex items-center gap-3 px-4 py-3 sm:px-5">
            <Skeleton className="size-9 rounded-xl" />
            <div className="flex min-w-0 flex-1 flex-col gap-1.5">
              <Skeleton className="h-4 w-2/5" />
              <Skeleton className="h-3.5 w-3/5" />
              <Skeleton className="h-3 w-1/3" />
            </div>
            <div className="flex w-14 flex-col items-end gap-1.5">
              <Skeleton className="h-5 w-8" />
              <Skeleton className="h-1 w-12" />
            </div>
          </div>
        ))}
      </div>
    </section>
  );
}

/* ── 复核 ── */

type Target = DisposeInput["targets"][number];

const TARGETS: Array<{ value: Target; label: string; severe?: boolean }> = [
  { value: "device", label: "设备" },
  { value: "ip", label: "IP" },
  { value: "account", label: "登录名" },
  { value: "user", label: "用户" },
  { value: "ban_account", label: "封禁账号", severe: true },
  { value: "firewall", label: "防火墙封禁 IP", severe: true },
];

function targetValue(c: RiskCase, t: Target): string {
  switch (t) {
    case "device":
      return c.deviceId;
    case "ip":
    case "firewall":
      return c.ip;
    case "account":
      return c.account;
    case "user":
    case "ban_account":
      return c.userId > 0 ? `用户 #${c.userId}` : "";
  }
}

const targetAvailable = (c: RiskCase, t: Target) => !!targetValue(c, t);

const RESULT_LABEL: Record<string, string> = { device: "设备", ip: "IP", account: "登录名", user: "用户", ban_account: "封禁账号", firewall: "防火墙封禁 IP" };
const RESULT_OK = new Set(["已下发", "已加入名单"]);

const KIND_HINT: Record<"block" | "watch", string> = {
  block: "命中后直接拦截",
  watch: "只做标记，供规则参考",
};

function CaseReview({ item, onClose }: { item: RiskCase; onClose: () => void }) {
  const appName = useAppName();
  const sceneName = useSceneName();
  const review = useReviewCase();
  const [decisionOpen, setDecisionOpen] = useState(false);
  const [verdict, setVerdict] = useState<"approve" | "reject">("reject");
  const [allow, setAllow] = useState(false);
  const [kind, setKind] = useState<Exclude<ListKind, "allow">>("block");
  const [targets, setTargets] = useState<Target[]>(() => (["device", "user"] as Target[]).filter((t) => targetAvailable(item, t)));
  const [duration, setDuration] = useState(String(7 * 86400));
  const [note, setNote] = useState("");
  const [confirm, setConfirm] = useState(false);
  const [result, setResult] = useState<Record<string, string> | null | undefined>(undefined);
  const pending = item.status === "pending" && result === undefined;
  const reasons = (item.reasons ?? []).filter(Boolean);
  const created = when(item.createdAt);
  const w = waited(item.createdAt);

  const effective = targets.filter((t) => targetAvailable(item, t) && !(TARGETS.find((x) => x.value === t)?.severe && kind !== "block"));
  const toggle = (t: Target, on: boolean) => setTargets((cur) => (on ? [...new Set([...cur, t])] : cur.filter((x) => x !== t)));
  const severe = verdict === "reject" && effective.some((t) => t === "ban_account" || t === "firewall");
  const durationLabel = DURATION_PRESETS.find((d) => String(d.seconds) === duration)?.label ?? "";

  const preview = (() => {
    if (verdict === "approve") return allow ? "判定为正常并放行，同时把这台设备和这位用户加入白名单 30 天。" : "判定为正常并放行，不做其他处置。";
    const list = effective.filter((t) => t !== "ban_account" && t !== "firewall").map((t) => RESULT_LABEL[t]);
    const parts: string[] = [];
    if (list.length) parts.push(`把${list.join("、")}加入${KIND_LABEL[kind]}`);
    if (effective.includes("ban_account")) parts.push("封禁账号");
    if (effective.includes("firewall")) parts.push("在防火墙封禁 IP");
    if (!parts.length) return "确认有风险并结案，不做其他处置。";
    return `确认有风险并结案，同时${parts.join("，")}，${duration === "0" ? "永久有效" : `期限 ${durationLabel}`}。`;
  })();

  const submit = () => {
    review.mutate(
      {
        id: item.id,
        verdict,
        note: note.trim(),
        allow: verdict === "approve" ? allow : undefined,
        dispose: verdict === "reject" && effective.length > 0 ? { kind, targets: effective, durationSeconds: Number(duration), reason: note.trim() } : undefined,
      },
      {
        onSuccess: (res) => {
          toast.success("已结案");
          if (res.dispose && Object.keys(res.dispose).length > 0) setResult(res.dispose);
          else onClose();
        },
        onError: (e) => toast.error(errorText(e, "提交失败")),
      }
    );
  };

  const closedVerdict = item.verdict ? VERDICT[item.verdict] : null;

  return (
    <>
      <DialogHeader className="min-w-0">
        <DialogTitle className="pr-6 text-lg leading-7">{caseHeadline(item, sceneName)}</DialogTitle>
        <DialogDescription className="flex flex-wrap gap-x-4 gap-y-0.5 tabular-nums">
          <span>复核单 #{item.id}</span>
          <span title={created.absolute}>创建于{created.relative}</span>
          <span>{appName(item.appId)}</span>
        </DialogDescription>
      </DialogHeader>

      <div className="flex min-w-0 flex-col gap-5">
        <section className="grid min-w-0 gap-4 rounded-xl border border-border p-4 sm:grid-cols-[168px_minmax(0,1fr)] sm:items-center">
          <ScoreDial score={item.score} size={160} className="mx-auto" />
          <div className="flex min-w-0 flex-col gap-2">
            <div className="flex flex-wrap items-center gap-2">
              <ActionGlyph action={item.action} size="sm" />
              <span className={cn("text-lg font-semibold", ACTION_UI[item.action]?.text)}>系统结论：{ACTION_RESULT[item.action] ?? item.action}</span>
            </div>
            <p className="text-sm leading-6 text-muted-foreground">
              风险分 {item.score}，属于{scoreWord(item.score).label}。由{SOURCE_LABEL[item.source] ?? "系统"}生成
              {item.status === "pending" ? `，已等待 ${w.text}` : ""}。
            </p>
            {item.decisionId ? (
              <Button size="sm" variant="outline" className="self-start" onClick={() => setDecisionOpen(true)}>
                <ExternalLink />
                查看完整决策
              </Button>
            ) : null}
          </div>
        </section>

        <ReviewSection title="触发原因">
          {reasons.length ? (
            <ol className="flex flex-col gap-1.5">
              {reasons.map((r, i) => (
                <li key={i} className="flex min-w-0 gap-3 rounded-lg bg-muted/40 px-3 py-2 text-sm leading-6">
                  <span className="grid size-5 shrink-0 translate-y-0.5 place-items-center rounded-full bg-amber-500/15 text-[11px] font-medium text-amber-700 tabular-nums dark:text-amber-300">
                    {i + 1}
                  </span>
                  <span className="min-w-0">{r}</span>
                </li>
              ))}
            </ol>
          ) : (
            <p className="rounded-lg bg-muted/40 px-3 py-2 text-sm text-muted-foreground">未记录原因。</p>
          )}
        </ReviewSection>

        <ReviewSection title="涉及对象">
          <dl className="grid min-w-0 grid-cols-2 gap-x-4 gap-y-3 rounded-xl border p-3 sm:grid-cols-3 sm:p-4">
            <Fact label="登录名" value={item.account || "无"} />
            <Fact label="用户" value={item.userId ? `#${item.userId}` : "未登录"} />
            <Fact label="场景" value={sceneName(item.scene)} />
            <Fact label="IP" value={item.ip || "无"} mono />
            <Fact label="设备标识" value={item.deviceId || "无"} mono className="col-span-2" />
          </dl>
        </ReviewSection>

        {result !== undefined ? (
          <DisposeResult result={result} />
        ) : item.status === "closed" ? (
          <ReviewSection title="复核结论">
            <div className="flex min-w-0 flex-col gap-3 rounded-xl border p-3 sm:p-4">
              <div className="flex flex-wrap items-center gap-2">
                {closedVerdict ? (
                  <span className={cn("inline-flex items-center gap-1.5 text-base font-semibold", closedVerdict.text)}>
                    <closedVerdict.icon className="size-4.5" />
                    {closedVerdict.label}
                  </span>
                ) : (
                  <span className="text-sm text-muted-foreground">未给出结论</span>
                )}
              </div>
              <dl className="grid min-w-0 grid-cols-2 gap-x-4 gap-y-3">
                <Fact label="复核人" value={item.reviewer || "未知"} />
                <Fact label="复核时间" value={when(item.reviewedAt).absolute || "未知"} />
                <Fact label="复核说明" value={item.note || "未填写"} className="col-span-2" wrap />
              </dl>
            </div>
          </ReviewSection>
        ) : (
          <ReviewSection title="你的判断">
            <div className="flex min-w-0 flex-col gap-4">
              <div className="grid gap-2 sm:grid-cols-2">
                <VerdictOption
                  active={verdict === "approve"}
                  tone="approve"
                  title="没有问题"
                  description="判定为正常，放行并结案"
                  onClick={() => setVerdict("approve")}
                />
                <VerdictOption
                  active={verdict === "reject"}
                  tone="reject"
                  title="确认有风险"
                  description="结案，并可处置相关对象"
                  onClick={() => setVerdict("reject")}
                />
              </div>

              {verdict === "approve" ? (
                <Label className="flex items-start gap-3 rounded-xl border p-3 text-sm font-normal">
                  <Checkbox className="mt-0.5" checked={allow} onCheckedChange={(v) => setAllow(v === true)} disabled={!item.deviceId && !item.userId} />
                  <span className="flex flex-col gap-0.5">
                    <span>同时加入白名单 30 天</span>
                    <span className="text-xs text-muted-foreground">这台设备和这位用户在期限内直接放行</span>
                  </span>
                </Label>
              ) : (
                <div className="flex min-w-0 flex-col gap-3 rounded-xl border p-3 sm:p-4">
                  <div className="flex min-w-0 flex-col gap-2">
                    <span className="text-xs text-muted-foreground">处置对象</span>
                    <div className="grid min-w-0 gap-2 sm:grid-cols-2">
                      {TARGETS.map((t) => {
                        const value = targetValue(item, t.value);
                        const disabled = !value || (t.severe && kind !== "block");
                        const checked = targets.includes(t.value) && !disabled;
                        return (
                          <Label
                            key={t.value}
                            className={cn(
                              "flex min-w-0 items-center gap-2.5 rounded-lg border px-3 py-2 text-sm font-normal transition-colors",
                              checked ? (t.severe ? "border-red-500/50 bg-red-500/5" : "border-foreground/25 bg-foreground/[0.03]") : "border-border",
                              disabled && "opacity-50"
                            )}
                          >
                            <Checkbox checked={checked} disabled={disabled} onCheckedChange={(v) => toggle(t.value, v === true)} />
                            <span className="flex min-w-0 flex-col leading-tight">
                              <span className={cn(t.severe && "text-red-700 dark:text-red-300")}>{t.label}</span>
                              <span className="truncate font-mono text-[11px] text-muted-foreground">{value || "无可用值"}</span>
                            </span>
                          </Label>
                        );
                      })}
                    </div>
                  </div>
                  <div className="grid min-w-0 gap-3 sm:grid-cols-[auto_minmax(0,1fr)]">
                    <div className="flex min-w-0 flex-col gap-1.5">
                      <span className="text-xs text-muted-foreground">加入</span>
                      <Segmented
                        value={kind}
                        options={[
                          { value: "block", label: KIND_LABEL.block },
                          { value: "watch", label: KIND_LABEL.watch },
                        ]}
                        onChange={(next) => {
                          setKind(next);
                          if (next !== "block") setTargets((cur) => cur.filter((t) => t !== "ban_account" && t !== "firewall"));
                        }}
                        ariaLabel="名单"
                      />
                      <span className="text-[11px] text-muted-foreground">{KIND_HINT[kind]}</span>
                    </div>
                    <div className="flex min-w-0 flex-col gap-1.5">
                      <span className="text-xs text-muted-foreground">期限</span>
                      <Segmented
                        value={duration}
                        options={DURATION_PRESETS.map((d) => ({ value: String(d.seconds), label: d.label }))}
                        onChange={setDuration}
                        ariaLabel="期限"
                      />
                    </div>
                  </div>
                </div>
              )}

              <div className="flex min-w-0 flex-col gap-1.5">
                <span className="text-xs text-muted-foreground">复核说明</span>
                <Textarea rows={2} value={note} onChange={(e) => setNote(e.target.value)} placeholder="记录判断依据，便于日后追溯" />
              </div>

              <p
                className={cn(
                  "rounded-lg px-3 py-2 text-sm leading-6",
                  verdict === "reject" ? "bg-red-500/8 text-red-700 dark:text-red-300" : "bg-emerald-500/8 text-emerald-700 dark:text-emerald-300"
                )}
              >
                {preview}
              </p>
            </div>
          </ReviewSection>
        )}
      </div>

      <DialogFooter>
        {pending ? (
          <>
            <Button variant="ghost" onClick={onClose}>
              取消
            </Button>
            <Button variant={verdict === "reject" ? "destructive" : "default"} disabled={review.isPending} onClick={() => (severe ? setConfirm(true) : submit())}>
              {review.isPending ? <Loader2 className="animate-spin" /> : null}
              {verdict === "reject" ? "确认有风险并结案" : "判定为正常并结案"}
            </Button>
          </>
        ) : (
          <Button variant="outline" onClick={onClose}>
            关闭
          </Button>
        )}
      </DialogFooter>

      <AlertDialog open={confirm} onOpenChange={setConfirm}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>确认处置</AlertDialogTitle>
            <AlertDialogDescription>
              将
              {[effective.includes("ban_account") ? "封禁该账号" : "", effective.includes("firewall") ? `在防火墙封禁 IP ${item.ip}` : ""].filter(Boolean).join("并")}
              {duration === "0" ? "，永久有效。" : `，期限 ${durationLabel}。`}
              该操作会立即生效。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction
              className="bg-destructive text-white hover:bg-destructive/90"
              onClick={() => {
                setConfirm(false);
                submit();
              }}
            >
              确认处置
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <DecisionDetailDialog id={item.decisionId || null} open={decisionOpen} onOpenChange={setDecisionOpen} />
    </>
  );
}

function ReviewSection({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="flex min-w-0 flex-col gap-2">
      <h4 className="text-sm font-semibold">{title}</h4>
      {children}
    </section>
  );
}

function VerdictOption({
  active,
  tone,
  title,
  description,
  onClick,
}: {
  active: boolean;
  tone: "approve" | "reject";
  title: string;
  description: string;
  onClick: () => void;
}) {
  const Icon = tone === "approve" ? CheckCircle2 : XCircle;
  return (
    <button
      type="button"
      aria-pressed={active}
      onClick={onClick}
      className={cn(
        "flex min-w-0 items-start gap-3 rounded-xl border p-3 text-left transition-colors focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none",
        !active && "border-border hover:bg-accent/50",
        active && tone === "approve" && "border-emerald-500/60 bg-emerald-500/8",
        active && tone === "reject" && "border-red-500/60 bg-red-500/8"
      )}
    >
      <Icon
        className={cn(
          "mt-0.5 size-5 shrink-0",
          !active && "text-muted-foreground",
          active && tone === "approve" && "text-emerald-600 dark:text-emerald-400",
          active && tone === "reject" && "text-red-600 dark:text-red-400"
        )}
      />
      <span className="flex min-w-0 flex-col gap-0.5">
        <span className="text-sm font-medium">{title}</span>
        <span className="text-xs text-muted-foreground">{description}</span>
      </span>
    </button>
  );
}

export function DisposeResult({ result }: { result: Record<string, string> | null }) {
  const entries = Object.entries(result ?? {});
  if (entries.length === 0) return null;
  const okCount = entries.filter(([, v]) => RESULT_OK.has(v)).length;
  return (
    <section className="flex min-w-0 flex-col gap-3 rounded-xl border border-border p-3 sm:p-4">
      <div className="flex items-center gap-2">
        <h4 className="text-sm font-semibold">处置结果</h4>
        <Badge variant={okCount === entries.length ? "success" : "warning"} size="sm">
          {okCount === entries.length ? "全部完成" : `${entries.length - okCount} 项未完成`}
        </Badge>
      </div>
      <ul className="flex flex-col divide-y rounded-lg border">
        {entries.map(([k, v]) => {
          const ok = RESULT_OK.has(v);
          return (
            <li key={k} className="flex min-w-0 items-center gap-2.5 px-3 py-2 text-sm">
              {ok ? <CheckCircle2 className="size-4 shrink-0 text-emerald-500" /> : <XCircle className="size-4 shrink-0 text-red-500" />}
              <span className="w-28 shrink-0">{RESULT_LABEL[k] ?? k}</span>
              <span className={cn("min-w-0 truncate", ok ? "text-muted-foreground" : "text-red-600 dark:text-red-400")} title={v}>
                {v}
              </span>
            </li>
          );
        })}
      </ul>
    </section>
  );
}
