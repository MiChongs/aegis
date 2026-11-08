"use client";

import { useMemo, useState, useSyncExternalStore, type ReactNode } from "react";
import NumberFlow from "@number-flow/react";
import { toast } from "sonner";
import {
  AlertTriangle,
  AtSign,
  Bot,
  Building2,
  CalendarClock,
  DatabaseZap,
  Flag,
  Globe,
  Hourglass,
  ListChecks,
  Loader2,
  Mail,
  Network,
  PieChart,
  Phone,
  Plus,
  RotateCw,
  ScanSearch,
  Search,
  ShieldCheck,
  Smartphone,
  Trash2,
  UserRound,
  X,
  type LucideIcon,
} from "lucide-react";

import { CardEmpty } from "@/components/overview/overview-kit";
import { ListPagination } from "@/components/payment/payment-list-parts";
import { Panel, Segmented, TabStack } from "@/components/traffic/traffic-kit";
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
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import type { ListEntry, ListHit, ListKind, ListType } from "@/lib/api/risk";
import { formatDateTime, formatWith, localDateKey, parseInstant, startOfLocalDay } from "@/lib/datetime";
import { useAddListEntries, useCheckLists, useRebuildLists, useRemoveListEntries, useRiskListStats, useRiskLists, useRiskScenes } from "@/lib/risk-hooks";
import { useDebouncedValue } from "@/lib/use-debounced-value";
import { cn } from "@/lib/utils";

import { errorText, useSceneName } from "./decisions";
import { AppScopeSelect, useAppName } from "./kit/app-scope";
import { listEntrySentence, remaining, scopeText, when } from "./kit/humanize";
import { DURATION_PRESETS, KIND_LABEL, LIST_TYPE_HINT, LIST_TYPE_LABEL } from "./kit/labels";

/**
 * 名单：黑名单、白名单、观察名单。
 *
 * 顶部两张图回答「名单里现在有什么」：三类名单的构成（环形图）与一周内陆续到期的条目（时间轴）。
 * 下方每条名单读作一句话：这台设备在黑名单中、为什么、还剩多久、在哪些应用与场景生效、谁加的。
 */

const PAGE_SIZE = 50;
const DAY = 86_400_000;
const LIST_TYPES = Object.keys(LIST_TYPE_LABEL) as ListType[];
const KINDS: ListKind[] = ["block", "allow", "watch"];
const SOURCE_LABEL: Record<string, string> = { manual: "手工添加", review: "复核时添加", decision: "决策自动添加", cep: "行为模式添加" };

const TYPE_ICON: Record<ListType, LucideIcon> = {
  ip: Globe, cidr: Network, device: Smartphone, account: AtSign, user: UserRound, email_domain: Mail,
  phone_prefix: Phone, asn: Building2, country: Flag, ua_keyword: Bot,
};

/** 三类名单的实色（明暗主题一致） */
const KIND_STYLE: Record<ListKind, { plate: string; dot: string; stroke: string; text: string; ring: string }> = {
  block: {
    plate: "bg-red-500/12 text-red-600 dark:text-red-400", dot: "bg-red-500", stroke: "stroke-red-500",
    text: "text-red-600 dark:text-red-400", ring: "border-red-500/50 bg-red-500/5",
  },
  allow: {
    plate: "bg-emerald-500/12 text-emerald-600 dark:text-emerald-400", dot: "bg-emerald-500", stroke: "stroke-emerald-500",
    text: "text-emerald-600 dark:text-emerald-400", ring: "border-emerald-500/50 bg-emerald-500/5",
  },
  watch: {
    plate: "bg-amber-500/12 text-amber-600 dark:text-amber-400", dot: "bg-amber-500", stroke: "stroke-amber-500",
    text: "text-amber-600 dark:text-amber-400", ring: "border-amber-500/50 bg-amber-500/5",
  },
};

const KIND_MEANING: Record<ListKind, string> = {
  block: "命中即拦截",
  allow: "命中即放行",
  watch: "交给规则参考，不直接拦截",
};

/* ── 时钟：渲染期不直接读 Date.now()，按半分钟取整 ── */

function subscribeClock(cb: () => void) {
  const id = setInterval(cb, 30_000);
  return () => clearInterval(id);
}
const readClock = () => Math.floor(Date.now() / 30_000) * 30_000;
const readServerClock = () => 0;

function useNow() {
  return useSyncExternalStore(subscribeClock, readClock, readServerClock);
}

/* ── 文案 ── */

/** 中文句子里夹英文词时补空格（「同一 IP 先」），再去掉标点前多余的空格 */
const sp = (w: string) => `${/^[A-Za-z0-9#]/.test(w) ? " " : ""}${w}${/[A-Za-z0-9]$/.test(w) ? " " : ""}`;
const tidy = (s: string) => s.replace(/\s+([，。、；：）」])/g, "$1").replace(/([（「])\s+/g, "$1").replace(/ {2,}/g, " ").trim();

function addedBy(e: Pick<ListEntry, "source" | "by">): string {
  const who = e.by?.trim();
  switch (e.source) {
    case "review":
      return who ? `${who} 复核时添加` : "复核时添加";
    case "decision":
      return "决策时自动添加";
    case "cep":
      return who ? `行为模式「${who}」添加` : "行为模式添加";
    default:
      return who ? `${who} 手工添加` : "手工添加";
  }
}

function sceneScope(scenes: string[] | undefined, sceneName: (k: string) => string) {
  return scenes?.length ? `${scenes.map(sceneName).join("、")}场景` : "全部场景";
}

/* ════════════════════════ 页面 ════════════════════════ */

export function RiskLists({ app }: { app?: number }) {
  const [type, setType] = useState("");
  const [kind, setKind] = useState<ListKind | "">("");
  const [value, setValue] = useState("");
  const [source, setSource] = useState("");
  const [active, setActive] = useState(true);
  const [page, setPage] = useState(1);
  const [selected, setSelected] = useState<Map<string, ListEntry>>(new Map());
  const [removing, setRemoving] = useState<ListEntry[] | null>(null);
  const [addOpen, setAddOpen] = useState(false);
  const [checkOpen, setCheckOpen] = useState(false);
  const [rebuildOpen, setRebuildOpen] = useState(false);
  const remove = useRemoveListEntries();
  const rebuild = useRebuildLists();
  const now = useNow();
  const debouncedValue = useDebouncedValue(value);

  const query = useRiskLists({
    type: type || undefined,
    kind: kind || undefined,
    value: debouncedValue.trim() || undefined,
    app,
    source: source || undefined,
    active,
    page,
    limit: PAGE_SIZE,
  });
  const rows = query.data?.list ?? [];
  const total = query.data?.total ?? 0;
  const filtered = !!(type || kind || debouncedValue.trim() || source || !active);

  const reset = () => {
    setPage(1);
    setSelected(new Map());
  };
  const clearFilters = () => {
    setType("");
    setKind("");
    setValue("");
    setSource("");
    setActive(true);
    reset();
  };

  const toggleRow = (e: ListEntry, on: boolean) =>
    setSelected((cur) => {
      const next = new Map(cur);
      if (on) next.set(e.id, e);
      else next.delete(e.id);
      return next;
    });
  const allOnPage = rows.length > 0 && rows.every((r) => selected.has(r.id));
  const someOnPage = rows.some((r) => selected.has(r.id));

  const doRemove = (entries: ListEntry[]) =>
    remove.mutate(
      entries.map((e) => e.id),
      {
        onSuccess: (res) => {
          toast.success(`已移除 ${res.removed} 条名单`);
          setSelected((cur) => {
            const next = new Map(cur);
            for (const e of entries) next.delete(e.id);
            return next;
          });
        },
        onError: (e) => toast.error(errorText(e, "移除失败")),
      }
    );

  return (
    <TabStack>
      <div className="grid min-w-0 gap-4 @4xl:grid-cols-[minmax(0,5fr)_minmax(0,7fr)]">
        <CompositionPanel
          app={app}
          activeKind={kind}
          onKind={(k) => {
            setKind((cur) => (cur === k ? "" : k));
            setActive(true);
            reset();
          }}
        />
        <ExpiryPanel app={app} />
      </div>

      <Panel
        icon={ListChecks}
        tone="emerald"
        title="名单条目"
        aside={
          <>
            <Button size="sm" variant="outline" onClick={() => setCheckOpen(true)}>
              <ScanSearch />
              查询命中
            </Button>
            <Button size="sm" variant="outline" disabled={rebuild.isPending} onClick={() => setRebuildOpen(true)}>
              {rebuild.isPending ? <Loader2 className="animate-spin" /> : <DatabaseZap />}
              重建索引
            </Button>
            <Button size="sm" onClick={() => setAddOpen(true)}>
              <Plus />
              添加名单
            </Button>
          </>
        }
        bodyClassName="px-0 pb-0 sm:px-0 sm:pb-0"
      >
        <div className="flex min-w-0 flex-wrap items-center gap-2 border-t px-4 py-3 sm:px-5">
          <div className="relative w-full min-w-0 sm:w-56">
            <Search className="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-muted-foreground" />
            <Input
              className="h-8 pl-8 font-mono text-xs"
              placeholder="按取值开头查找"
              value={value}
              onChange={(e) => {
                setValue(e.target.value);
                reset();
              }}
            />
          </div>
          <Segmented
            value={kind || "all"}
            options={[{ value: "all", label: "全部" }, ...KINDS.map((k) => ({ value: k, label: KIND_LABEL[k] }))]}
            onChange={(v) => {
              setKind(v === "all" ? "" : (v as ListKind));
              reset();
            }}
            ariaLabel="名单种类"
          />
          <Select
            value={type || "all"}
            onValueChange={(v) => {
              setType(v === "all" ? "" : v);
              reset();
            }}
          >
            <SelectTrigger size="sm" className="h-8 w-36">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">全部类型</SelectItem>
              {LIST_TYPES.map((t) => {
                const Icon = TYPE_ICON[t];
                return (
                  <SelectItem key={t} value={t}>
                    <Icon className="size-3.5 text-muted-foreground" />
                    {LIST_TYPE_LABEL[t]}
                  </SelectItem>
                );
              })}
            </SelectContent>
          </Select>
          <Select
            value={source || "all"}
            onValueChange={(v) => {
              setSource(v === "all" ? "" : v);
              reset();
            }}
          >
            <SelectTrigger size="sm" className="h-8 w-36">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">全部来源</SelectItem>
              {Object.entries(SOURCE_LABEL).map(([v, l]) => (
                <SelectItem key={v} value={v}>
                  {l}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Label className="inline-flex h-8 items-center gap-2 text-xs font-normal text-muted-foreground">
            <Switch
              checked={active}
              onCheckedChange={(v) => {
                setActive(v);
                reset();
              }}
            />
            只看有效
          </Label>
          {filtered ? (
            <Button size="sm" variant="ghost" className="h-8 text-muted-foreground" onClick={clearFilters}>
              <X />
              清除筛选
            </Button>
          ) : null}
        </div>

        {selected.size > 0 ? (
          <div className="flex min-w-0 flex-wrap items-center gap-2 border-t bg-primary/5 px-4 py-2 sm:px-5">
            <span className="text-xs">
              已选 <span className="font-semibold tabular-nums">{selected.size}</span> 条
            </span>
            <span className="flex-1" />
            <Button size="sm" variant="ghost" className="h-7" onClick={() => setSelected(new Map())}>
              取消选择
            </Button>
            <Button size="sm" variant="destructive" className="h-7" disabled={remove.isPending} onClick={() => setRemoving([...selected.values()])}>
              <Trash2 />
              移除所选
            </Button>
          </div>
        ) : null}

        {query.isError && !query.data ? (
          <div className="flex flex-col items-center gap-3 border-t px-5 py-10 text-center">
            <AlertTriangle className="size-5 text-red-500" />
            <p className="text-sm text-muted-foreground">{errorText(query.error, "名单读取失败")}</p>
            <Button size="sm" variant="outline" onClick={() => query.refetch()}>
              <RotateCw />
              重试
            </Button>
          </div>
        ) : query.isLoading ? (
          <ul className="flex flex-col border-t">
            {Array.from({ length: 6 }).map((_, i) => (
              <li key={i} className="flex items-start gap-3 border-b px-4 py-3.5 last:border-b-0 sm:px-5">
                <Skeleton className="mt-0.5 size-4 rounded" />
                <Skeleton className="size-9 rounded-xl" />
                <div className="flex min-w-0 flex-1 flex-col gap-2">
                  <Skeleton className="h-4 w-40" />
                  <Skeleton className="h-4 w-56" />
                  <Skeleton className="h-3 w-72 max-w-full" />
                </div>
                <Skeleton className="h-6 w-20 rounded-full" />
              </li>
            ))}
          </ul>
        ) : rows.length === 0 ? (
          <div className="border-t">
            {filtered ? (
              <div className="flex flex-col items-center gap-3 px-5 py-10 text-center">
                <Search className="size-5 text-muted-foreground" />
                <p className="text-sm text-muted-foreground">没有符合筛选条件的名单</p>
                <Button size="sm" variant="outline" onClick={clearFilters}>
                  清除筛选
                </Button>
              </div>
            ) : (
              <div className="flex flex-col items-center gap-1 pb-6">
                <CardEmpty icon={ShieldCheck} title="名单为空" description="添加后立即参与风控判断" />
                <Button size="sm" onClick={() => setAddOpen(true)}>
                  <Plus />
                  添加名单
                </Button>
              </div>
            )}
          </div>
        ) : (
          <>
            <div className="flex min-w-0 items-center gap-3 border-t bg-muted/30 px-4 py-1.5 sm:px-5">
              <Checkbox
                aria-label="全选本页"
                checked={allOnPage ? true : someOnPage ? "indeterminate" : false}
                onCheckedChange={(v) => {
                  for (const r of rows) toggleRow(r, v === true);
                }}
              />
              <span className="text-xs text-muted-foreground">
                {filtered ? "符合条件的" : "全部"} <span className="tabular-nums">{total.toLocaleString("zh-CN")}</span> 条
              </span>
            </div>
            <ul className={cn("flex flex-col transition-opacity", query.isFetching && query.isPlaceholderData && "opacity-60")}>
              {rows.map((e) => (
                <EntryRow key={e.id} entry={e} now={now} selected={selected.has(e.id)} onSelect={(on) => toggleRow(e, on)} onRemove={() => setRemoving([e])} />
              ))}
            </ul>
            {total > PAGE_SIZE ? (
              <ListPagination page={page} totalPages={Math.max(1, Math.ceil(total / PAGE_SIZE))} total={total} unit="条" onPage={setPage} />
            ) : null}
          </>
        )}
      </Panel>

      <AlertDialog open={!!removing} onOpenChange={(o) => !o && setRemoving(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{removing?.length === 1 ? "移出名单" : `移除 ${removing?.length ?? 0} 条名单`}</AlertDialogTitle>
            <AlertDialogDescription>
              {removing?.length === 1 && removing[0]
                ? `${removing[0].value} 将移出${KIND_LABEL[removing[0].kind] ?? "名单"}，立即生效。`
                : "所选名单将全部移除，立即生效。"}
            </AlertDialogDescription>
          </AlertDialogHeader>
          {removing && removing.length > 1 ? (
            <div className="flex max-h-40 flex-wrap gap-1 overflow-y-auto">
              {removing.slice(0, 30).map((e) => (
                <span key={e.id} className={cn("rounded-md px-1.5 py-0.5 font-mono text-[11px]", KIND_STYLE[e.kind]?.plate ?? "bg-muted")}>
                  {e.value}
                </span>
              ))}
              {removing.length > 30 ? <span className="text-[11px] text-muted-foreground">等 {removing.length} 条</span> : null}
            </div>
          ) : null}
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction
              className="bg-destructive text-white hover:bg-destructive/90"
              onClick={() => {
                if (removing) doRemove(removing);
                setRemoving(null);
              }}
            >
              移除
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <AlertDialog open={rebuildOpen} onOpenChange={setRebuildOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>重建名单索引</AlertDialogTitle>
            <AlertDialogDescription>按当前全部有效名单重新生成在线索引。名单命中结果与这里的记录不一致时使用。</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction
              onClick={() =>
                rebuild.mutate(undefined, {
                  onSuccess: (res) => toast.success(`索引已重建，共 ${res.entries} 条`),
                  onError: (e) => toast.error(errorText(e, "重建失败")),
                })
              }
            >
              重建
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <Dialog open={addOpen} onOpenChange={setAddOpen}>
        <DialogContent className="max-h-[92vh] gap-4 overflow-y-auto sm:max-w-2xl">
          {addOpen ? <AddListForm defaultApp={app} onDone={() => setAddOpen(false)} /> : null}
        </DialogContent>
      </Dialog>

      <Dialog open={checkOpen} onOpenChange={setCheckOpen}>
        <DialogContent className="max-h-[92vh] gap-4 overflow-y-auto sm:max-w-2xl">
          {checkOpen ? <CheckListsForm defaultApp={app} /> : null}
        </DialogContent>
      </Dialog>
    </TabStack>
  );
}

/* ════════════════════════ 构成环形图 ════════════════════════ */

function CompositionPanel({ app, activeKind, onKind }: { app?: number; activeKind: ListKind | ""; onKind: (k: ListKind) => void }) {
  const stats = useRiskListStats(app);
  const counts = Object.fromEntries(KINDS.map((k) => [k, stats.data?.byKind?.[k] ?? 0])) as Record<ListKind, number>;
  const total = stats.data?.total ?? counts.block + counts.allow + counts.watch;
  const permanent = stats.data?.permanent ?? 0;
  const types = LIST_TYPES.map((t) => ({ type: t, n: stats.data?.byType?.[t] ?? 0 }))
    .filter((t) => t.n > 0)
    .sort((a, b) => b.n - a.n);

  return (
    <Panel icon={PieChart} tone="rose" title="名单构成">
      {stats.isError && !stats.data ? (
        <div className="flex flex-col items-center gap-3 py-8 text-center">
          <p className="text-sm text-muted-foreground">{errorText(stats.error, "统计读取失败")}</p>
          <Button size="sm" variant="outline" onClick={() => stats.refetch()}>
            <RotateCw />
            重试
          </Button>
        </div>
      ) : stats.isLoading ? (
        <div className="flex min-w-0 flex-col items-center gap-5 @sm:flex-row">
          <Skeleton className="size-[152px] shrink-0 rounded-full" />
          <div className="flex w-full min-w-0 flex-col gap-2">
            {KINDS.map((k) => (
              <Skeleton key={k} className="h-12 rounded-xl" />
            ))}
          </div>
        </div>
      ) : (
        <div className="@container/comp min-w-0">
        <div className="flex min-w-0 flex-col items-center gap-5 @min-[26rem]/comp:flex-row">
          <div className="relative shrink-0">
            <Donut parts={KINDS.map((k) => ({ key: k, value: counts[k], stroke: KIND_STYLE[k].stroke }))} total={total} highlight={activeKind || undefined} />
            <div className="pointer-events-none absolute inset-0 flex flex-col items-center justify-center">
              <span className="text-2xl leading-7 font-semibold tracking-tight tabular-nums">
                <NumberFlow value={total} locales="zh-CN" />
              </span>
              <span className="text-[11px] text-muted-foreground">条有效名单</span>
            </div>
          </div>
          <ul className="flex w-full min-w-0 flex-col gap-1.5">
            {KINDS.map((k) => {
              const share = total ? counts[k] / total : 0;
              const on = activeKind === k;
              return (
                <li key={k}>
                  <button
                    type="button"
                    aria-pressed={on}
                    onClick={() => onKind(k)}
                    className={cn(
                      "flex w-full min-w-0 items-center gap-3 rounded-xl border px-3 py-2 text-left transition-colors focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none",
                      on ? KIND_STYLE[k].ring : "border-transparent hover:bg-muted/60"
                    )}
                  >
                    <span aria-hidden className={cn("size-2.5 shrink-0 rounded-[3px]", KIND_STYLE[k].dot)} />
                    <span className="min-w-0 flex-1">
                      <span className="block truncate text-sm font-medium">{KIND_LABEL[k]}</span>
                      <span className="block truncate text-[11px] text-muted-foreground">{KIND_MEANING[k]}</span>
                    </span>
                    <span className="flex shrink-0 flex-col items-end">
                      <span className="text-sm font-semibold tabular-nums">{counts[k].toLocaleString("zh-CN")}</span>
                      <span className="text-[11px] text-muted-foreground tabular-nums">{Math.round(share * 100)}%</span>
                    </span>
                  </button>
                </li>
              );
            })}
          </ul>
        </div>
        {total > 0 ? (
          <div className="mt-4 flex min-w-0 flex-col gap-2 border-t pt-3">
            <div className="flex min-w-0 items-center gap-2 text-[11px] text-muted-foreground">
              <span>
                长期有效 <span className="font-medium text-foreground tabular-nums">{permanent.toLocaleString("zh-CN")}</span> 条
              </span>
              <span className="text-muted-foreground/50">/</span>
              <span>
                设有期限 <span className="font-medium text-foreground tabular-nums">{Math.max(0, total - permanent).toLocaleString("zh-CN")}</span> 条
              </span>
            </div>
            {types.length ? (
              <ul className="flex min-w-0 flex-wrap gap-1.5">
                {types.map(({ type, n }) => {
                  const Icon = TYPE_ICON[type] ?? Globe;
                  return (
                    <li key={type} className="inline-flex items-center gap-1.5 rounded-md bg-muted/60 px-2 py-1 text-[11px]">
                      <Icon className="size-3 text-muted-foreground" />
                      {LIST_TYPE_LABEL[type]}
                      <span className="font-medium tabular-nums">{n.toLocaleString("zh-CN")}</span>
                    </li>
                  );
                })}
              </ul>
            ) : null}
          </div>
        ) : null}
        </div>
      )}
    </Panel>
  );
}

function Donut({
  parts,
  total,
  highlight,
  size = 152,
  stroke = 18,
}: {
  parts: Array<{ key: string; value: number; stroke: string }>;
  total: number;
  highlight?: string;
  size?: number;
  stroke?: number;
}) {
  // 留出选中时加粗的 4px
  const r = (size - stroke - 4) / 2;
  const c = 2 * Math.PI * r;
  const visible = parts.filter((p) => p.value > 0).length;
  const gap = visible > 1 ? 3 : 0;
  const lengths = parts.map((p) => (total ? (p.value / total) * c : 0));
  const offsets = lengths.map((_, i) => lengths.slice(0, i).reduce((a, b) => a + b, 0));
  return (
    <svg width={size} height={size} viewBox={`0 0 ${size} ${size}`} aria-hidden className="-rotate-90">
      <circle cx={size / 2} cy={size / 2} r={r} fill="none" strokeWidth={stroke} className="stroke-muted" />
      {parts.map((p, i) =>
        lengths[i]! > 0 ? (
          <circle
            key={p.key}
            cx={size / 2}
            cy={size / 2}
            r={r}
            fill="none"
            strokeWidth={highlight === p.key ? stroke + 4 : stroke}
            strokeDasharray={`${Math.max(0.5, lengths[i]! - gap)} ${c}`}
            strokeDashoffset={-offsets[i]!}
            className={cn(p.stroke, "transition-[stroke-dasharray,stroke-width,opacity] duration-500", highlight && highlight !== p.key && "opacity-35")}
          />
        ) : null
      )}
    </svg>
  );
}

/* ════════════════════════ 即将到期时间轴 ════════════════════════ */

function ExpiryPanel({ app }: { app?: number }) {
  const now = useNow();
  const stats = useRiskListStats(app);
  const first = useRiskLists({ app, active: true, sort: "exp", limit: 4 });
  const soon = (first.data?.list ?? []).filter((e) => !!parseInstant(e.exp));

  const days = useMemo(() => {
    const today = startOfLocalDay(new Date(now || Date.UTC(2000, 0, 1))).getTime();
    const byDay = new Map((stats.data?.expiring ?? []).map((d) => [d.day, d.byKind ?? {}]));
    return Array.from({ length: 7 }, (_, i) => {
      const start = today + i * DAY;
      const k = byDay.get(localDateKey(start)) ?? {};
      const counts = { block: k.block ?? 0, allow: k.allow ?? 0, watch: k.watch ?? 0 } as Record<ListKind, number>;
      return { start, counts, total: counts.block + counts.allow + counts.watch };
    });
  }, [stats.data, now]);
  const inWeek = days.reduce((a, d) => a + d.total, 0);
  const max = Math.max(1, ...days.map((d) => d.total));

  return (
    <Panel
      icon={CalendarClock}
      tone="amber"
      title="即将到期"
      aside={
        stats.data ? (
          <span className="text-xs text-muted-foreground">
            7 天内 <span className="font-medium text-foreground tabular-nums">{inWeek.toLocaleString("zh-CN")}</span> 条
          </span>
        ) : null
      }
    >
      {stats.isError && !stats.data ? (
        <div className="flex flex-col items-center gap-3 py-8 text-center">
          <p className="text-sm text-muted-foreground">{errorText(stats.error, "读取失败")}</p>
          <Button size="sm" variant="outline" onClick={() => stats.refetch()}>
            <RotateCw />
            重试
          </Button>
        </div>
      ) : stats.isLoading || !now ? (
        <div className="flex flex-col gap-4">
          <div className="grid h-[132px] grid-cols-7 items-end gap-2">
            {[40, 70, 30, 55, 20, 45, 25].map((h, i) => (
              <Skeleton key={i} className="rounded-md" style={{ height: `${h}%` }} />
            ))}
          </div>
          <Skeleton className="h-10 rounded-lg" />
        </div>
      ) : (
        <div className="flex min-w-0 flex-col gap-4">
          <div className="grid min-w-0 grid-cols-7 gap-1.5 sm:gap-2">
            {days.map((d, i) => (
              <div key={d.start} className="flex min-w-0 flex-col items-center gap-1.5">
                <span className={cn("text-xs tabular-nums", d.total ? "font-semibold" : "text-muted-foreground/60")}>{d.total || "0"}</span>
                <div className="flex h-24 w-full max-w-11 flex-col justify-end overflow-hidden rounded-md bg-muted/60">
                  <div className="flex w-full flex-col-reverse transition-[height] duration-500" style={{ height: `${(d.total / max) * 100}%` }}>
                    {KINDS.map((k) =>
                      d.counts[k] ? <div key={k} className={cn("w-full", KIND_STYLE[k].dot)} style={{ height: `${(d.counts[k] / d.total) * 100}%` }} /> : null
                    )}
                  </div>
                </div>
                <span className={cn("truncate text-[11px]", i === 0 ? "font-medium text-foreground" : "text-muted-foreground")}>
                  {i === 0 ? "今天" : i === 1 ? "明天" : formatWith(d.start, { month: "numeric", day: "numeric" })}
                </span>
              </div>
            ))}
          </div>
          {first.isLoading ? (
            <div className="grid min-w-0 gap-1.5 @2xl:grid-cols-2">
              {[0, 1].map((i) => (
                <Skeleton key={i} className="h-11 rounded-lg" />
              ))}
            </div>
          ) : soon.length ? (
            <ul className="grid min-w-0 gap-1.5 @2xl:grid-cols-2">
              {soon.map((entry) => {
                const Icon = TYPE_ICON[entry.type] ?? Globe;
                return (
                  <li key={entry.id} className="flex min-w-0 items-center gap-2.5 rounded-lg bg-muted/40 px-2.5 py-2">
                    <span className={cn("grid size-7 shrink-0 place-items-center rounded-lg", KIND_STYLE[entry.kind]?.plate)}>
                      <Icon className="size-3.5" />
                    </span>
                    <span className="min-w-0 flex-1">
                      <span className="block truncate font-mono text-xs" title={entry.value}>
                        {entry.value}
                      </span>
                      <span className="block truncate text-[11px] text-muted-foreground">{KIND_LABEL[entry.kind]}</span>
                    </span>
                    <span className="shrink-0 text-xs font-medium text-amber-600 tabular-nums dark:text-amber-400" title={formatDateTime(entry.exp)}>
                      {remaining(entry.exp).text}
                    </span>
                  </li>
                );
              })}
            </ul>
          ) : (
            <p className="flex items-center justify-center gap-2 rounded-lg bg-muted/40 px-3 py-3 text-xs text-muted-foreground">
              <Hourglass className="size-3.5" />
              {stats.data && stats.data.total > 0 && stats.data.permanent >= stats.data.total ? "全部名单长期有效" : "没有即将到期的名单"}
            </p>
          )}
        </div>
      )}
    </Panel>
  );
}

/* ════════════════════════ 名单行 ════════════════════════ */

function EntryRow({ entry: e, now, selected, onSelect, onRemove }: { entry: ListEntry; now: number; selected: boolean; onSelect: (on: boolean) => void; onRemove: () => void }) {
  const appName = useAppName();
  const sceneName = useSceneName();
  const Icon = TYPE_ICON[e.type] ?? Globe;
  const style = KIND_STYLE[e.kind] ?? KIND_STYLE.watch;
  const left = remaining(e.exp);
  const expMs = parseInstant(e.exp)?.getTime();
  const urgent = !left.expired && !!expMs && !!now && expMs - now < DAY;
  const added = when(e.at);

  return (
    <li
      className={cn(
        "group flex min-w-0 items-start gap-3 border-t px-4 py-3 transition-colors sm:px-5",
        selected ? "bg-primary/5" : "hover:bg-muted/40",
        left.expired && "opacity-60"
      )}
    >
      <Checkbox className="mt-2.5" aria-label={`选择 ${e.value}`} checked={selected} onCheckedChange={(v) => onSelect(v === true)} />
      <span className={cn("grid size-9 shrink-0 place-items-center rounded-xl", style.plate)} title={LIST_TYPE_LABEL[e.type]}>
        <Icon className="size-4" />
      </span>
      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
        <span className="text-[13px] leading-5 text-muted-foreground">{listEntrySentence(e)}</span>
        <span className="font-mono text-sm leading-5 font-medium break-all">{e.value}</span>
        {e.reason ? <p className="text-xs leading-5 break-words text-foreground/80">{e.reason}</p> : null}
        <div className="mt-0.5 flex min-w-0 flex-wrap items-center gap-x-3 gap-y-0.5 text-[11px] leading-4 text-muted-foreground">
          <span>
            {tidy(`在${sp(scopeText(e.app, appName))}的${sceneScope(e.scenes, sceneName)}生效`)}
          </span>
          <span title={added.absolute}>
            {addedBy(e)}
            {added.relative ? `，${added.relative}` : ""}
          </span>
        </div>
      </div>
      <div className="flex shrink-0 flex-col items-end gap-1.5">
        <span
          className={cn(
            "inline-flex h-6 items-center gap-1 rounded-full px-2 text-[11px] font-medium tabular-nums",
            left.expired
              ? "bg-muted text-muted-foreground"
              : !expMs
                ? "bg-muted/60 text-muted-foreground"
                : urgent
                  ? "bg-red-500/12 text-red-600 dark:text-red-400"
                  : "bg-amber-500/12 text-amber-700 dark:text-amber-300"
          )}
          title={expMs ? `${formatDateTime(e.exp)} 到期` : undefined}
        >
          <Hourglass className="size-3" />
          {left.text}
        </span>
        <Button size="icon-xs" variant="ghost" className="text-muted-foreground hover:text-destructive" aria-label={`移除 ${e.value}`} onClick={onRemove}>
          <Trash2 />
        </Button>
      </div>
    </li>
  );
}

/* ════════════════════════ 添加 ════════════════════════ */

function AddListForm({ defaultApp, onDone }: { defaultApp?: number; onDone: () => void }) {
  const scenes = useRiskScenes();
  const add = useAddListEntries();
  const appName = useAppName();
  const sceneName = useSceneName();
  const [type, setType] = useState<ListType>("ip");
  const [kind, setKind] = useState<ListKind>("block");
  const [text, setText] = useState("");
  const [sceneKeys, setSceneKeys] = useState<string[]>([]);
  const [appId, setAppId] = useState<number | undefined>(defaultApp);
  const [reason, setReason] = useState("");
  const [duration, setDuration] = useState("86400");
  const [invalid, setInvalid] = useState<string[]>([]);

  const values = useMemo(
    () => [
      ...new Set(
        text
          .split(type === "ua_keyword" ? /\r?\n/ : /[\s,，;；]+/)
          .map((v) => v.trim())
          .filter(Boolean)
      ),
    ],
    [text, type]
  );
  const durationLabel = DURATION_PRESETS.find((d) => String(d.seconds) === duration)?.label ?? "";
  const preview = values.length
    ? tidy(
        `将把 ${values.length} 个${sp(LIST_TYPE_LABEL[type])}加入${KIND_LABEL[kind]}，在${sp(scopeText(appId, appName))}的${sceneScope(sceneKeys, sceneName)}生效，${
          duration === "0" ? "长期有效" : `${durationLabel}后自动移除`
        }。`
      )
    : "填写取值后，这里会说明本次添加的效果。";

  const submit = () =>
    add.mutate(
      {
        listType: type,
        kind,
        values,
        appId,
        scenes: sceneKeys.length ? sceneKeys : undefined,
        reason: reason.trim() || undefined,
        durationSeconds: Number(duration),
      },
      {
        onSuccess: (res) => {
          const bad = res.invalid ?? [];
          if (res.added) toast.success(`已添加 ${res.added} 条名单`);
          if (bad.length) {
            setInvalid(bad);
            setText(bad.join("\n"));
          } else {
            onDone();
          }
        },
        onError: (e) => toast.error(errorText(e, "添加失败")),
      }
    );

  return (
    <>
      <DialogHeader>
        <DialogTitle>添加名单</DialogTitle>
        <DialogDescription>保存后立即参与风控判断。</DialogDescription>
      </DialogHeader>
      <div className="flex min-w-0 flex-col gap-5">
        <FormField label="名单种类">
          <div className="grid min-w-0 gap-2 sm:grid-cols-3">
            {KINDS.map((k) => (
              <button
                key={k}
                type="button"
                aria-pressed={kind === k}
                onClick={() => setKind(k)}
                className={cn(
                  "flex min-w-0 flex-col gap-0.5 rounded-xl border px-3 py-2.5 text-left transition-colors focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none",
                  kind === k ? KIND_STYLE[k].ring : "border-border hover:bg-muted/50"
                )}
              >
                <span className="flex items-center gap-2 text-sm font-medium">
                  <span aria-hidden className={cn("size-2 rounded-full", KIND_STYLE[k].dot)} />
                  {KIND_LABEL[k]}
                </span>
                <span className="text-[11px] leading-4 text-muted-foreground">{KIND_MEANING[k]}</span>
              </button>
            ))}
          </div>
        </FormField>

        <FormField label="取值类型">
          <div className="grid min-w-0 grid-cols-2 gap-1.5 sm:grid-cols-5">
            {LIST_TYPES.map((t) => {
              const Icon = TYPE_ICON[t];
              const on = type === t;
              return (
                <button
                  key={t}
                  type="button"
                  aria-pressed={on}
                  onClick={() => setType(t)}
                  className={cn(
                    "flex h-9 min-w-0 items-center gap-1.5 rounded-lg border px-2.5 text-xs transition-colors focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none",
                    on ? "border-primary/50 bg-primary/10 font-medium text-foreground" : "border-border text-muted-foreground hover:bg-muted/50"
                  )}
                >
                  <Icon className="size-3.5 shrink-0" />
                  <span className="truncate">{LIST_TYPE_LABEL[t]}</span>
                </button>
              );
            })}
          </div>
        </FormField>

        <FormField label={values.length ? `取值（${values.length} 个）` : "取值"}>
          <Textarea
            rows={5}
            className="font-mono text-xs"
            value={text}
            onChange={(e) => {
              setText(e.target.value);
              if (invalid.length) setInvalid([]);
            }}
            placeholder={`每行一个，例如 ${LIST_TYPE_HINT[type]}`}
          />
        </FormField>
        {invalid.length > 0 ? (
          <div className="flex flex-col gap-1.5 rounded-lg border border-amber-500/40 bg-amber-500/5 px-3 py-2.5">
            <span className="inline-flex items-center gap-1.5 text-xs font-medium text-amber-700 dark:text-amber-300">
              <AlertTriangle className="size-3.5" />
              {invalid.length} 个取值格式不对，未添加，已留在输入框中，修改后可再次提交
            </span>
          </div>
        ) : null}

        <div className="grid min-w-0 gap-4 sm:grid-cols-2">
          <FormField label="生效应用">
            <AppScopeSelect value={appId} onChange={setAppId} className="h-9 w-full" />
          </FormField>
          <FormField label="原因">
            <Input value={reason} onChange={(e) => setReason(e.target.value)} placeholder="选填，会显示在名单里" />
          </FormField>
        </div>
        <FormField label="生效场景">
          <div className="flex flex-wrap gap-1.5">
            <ChipToggle active={sceneKeys.length === 0} onClick={() => setSceneKeys([])}>
              全部场景
            </ChipToggle>
            {(scenes.data ?? []).map((s) => (
              <ChipToggle
                key={s.key}
                active={sceneKeys.includes(s.key)}
                onClick={() => setSceneKeys((cur) => (cur.includes(s.key) ? cur.filter((k) => k !== s.key) : [...cur, s.key]))}
              >
                {s.name}
              </ChipToggle>
            ))}
          </div>
        </FormField>
        <FormField label="有效期">
          <Segmented value={duration} options={DURATION_PRESETS.map((d) => ({ value: String(d.seconds), label: d.label }))} onChange={setDuration} ariaLabel="有效期" />
        </FormField>

        <p className={cn("rounded-lg px-3 py-2.5 text-xs leading-5", values.length ? KIND_STYLE[kind].ring + " border" : "bg-muted/50 text-muted-foreground")}>{preview}</p>
      </div>
      <DialogFooter>
        <Button variant="ghost" onClick={onDone}>
          {invalid.length ? "完成" : "取消"}
        </Button>
        <Button disabled={values.length === 0 || add.isPending} onClick={submit}>
          {add.isPending ? <Loader2 className="animate-spin" /> : <Plus />}
          {values.length > 0 ? `添加 ${values.length} 条` : "添加"}
        </Button>
      </DialogFooter>
    </>
  );
}

function FormField({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex min-w-0 flex-col gap-1.5">
      <span className="text-xs font-medium text-muted-foreground">{label}</span>
      {children}
    </div>
  );
}

function ChipToggle({ active, onClick, children }: { active: boolean; onClick: () => void; children: ReactNode }) {
  return (
    <button
      type="button"
      aria-pressed={active}
      onClick={onClick}
      className={cn(
        "inline-flex h-7 items-center rounded-md border px-2.5 text-xs transition-colors",
        active ? "border-primary/50 bg-primary/10 text-foreground" : "border-border text-muted-foreground hover:bg-accent/50"
      )}
    >
      {children}
    </button>
  );
}

/* ════════════════════════ 查询命中 ════════════════════════ */

const CHECK_FIELDS: Array<{ type: ListType; label: string; mono?: boolean }> = [
  { type: "ip", label: "IP", mono: true },
  { type: "device", label: "设备 ID", mono: true },
  { type: "account", label: "登录名" },
  { type: "user", label: "用户 ID", mono: true },
  { type: "phone_prefix", label: "手机号", mono: true },
  { type: "email_domain", label: "邮箱或域名" },
  { type: "asn", label: "ASN", mono: true },
  { type: "country", label: "国家或地区", mono: true },
  { type: "ua_keyword", label: "User-Agent" },
];

function CheckListsForm({ defaultApp }: { defaultApp?: number }) {
  const scenes = useRiskScenes();
  const check = useCheckLists();
  const [appId, setAppId] = useState<number | undefined>(defaultApp);
  const [scene, setScene] = useState("");
  const [values, setValues] = useState<Partial<Record<ListType, string>>>({});
  const [hits, setHits] = useState<ListHit[] | null>(null);

  const filled = Object.fromEntries(
    Object.entries(values)
      .map(([k, v]) => [k, (v ?? "").trim()])
      .filter(([, v]) => v)
  ) as Partial<Record<ListType, string>>;
  const any = Object.keys(filled).length > 0;
  const kinds = new Set((hits ?? []).map((h) => h.kind));
  const verdict: { text: string; tone: string } | null = hits
    ? !hits.length
      ? { text: "没有命中任何名单", tone: "bg-muted/50 text-muted-foreground" }
      : kinds.has("allow")
        ? { text: `命中 ${hits.length} 条，其中有白名单`, tone: KIND_STYLE.allow.ring + " border" }
        : kinds.has("block")
          ? { text: `命中 ${hits.length} 条，其中有黑名单`, tone: KIND_STYLE.block.ring + " border" }
          : { text: `命中 ${hits.length} 条观察名单`, tone: KIND_STYLE.watch.ring + " border" }
    : null;

  const run = () =>
    check.mutate({ appId, scene: scene || undefined, values: filled }, { onSuccess: setHits, onError: (e) => toast.error(errorText(e, "查询失败")) });

  return (
    <>
      <DialogHeader>
        <DialogTitle>查询命中</DialogTitle>
        <DialogDescription>填写一个或多个取值，查看它们在指定应用与场景下会命中哪些名单。</DialogDescription>
      </DialogHeader>
      <div className="flex min-w-0 flex-col gap-5">
        <div className="grid min-w-0 gap-4 sm:grid-cols-2">
          <FormField label="应用">
            <AppScopeSelect value={appId} onChange={setAppId} className="h-9 w-full" />
          </FormField>
          <FormField label="场景">
            <Select value={scene || "all"} onValueChange={(v) => setScene(v === "all" ? "" : v)}>
              <SelectTrigger size="sm" className="h-9 w-full">
                <SelectValue />
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
          </FormField>
        </div>
        <div className="grid min-w-0 gap-3 sm:grid-cols-3">
          {CHECK_FIELDS.map((f) => {
            const Icon = TYPE_ICON[f.type];
            return (
              <FormField key={f.type} label={f.label}>
                <div className="relative">
                  <Icon className="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-muted-foreground" />
                  <Input
                    className={cn("h-8 pl-8", f.mono && "font-mono text-xs")}
                    value={values[f.type] ?? ""}
                    placeholder={LIST_TYPE_HINT[f.type]}
                    onChange={(e) => setValues((cur) => ({ ...cur, [f.type]: e.target.value }))}
                    onKeyDown={(e) => {
                      if (e.key === "Enter" && any) run();
                    }}
                  />
                </div>
              </FormField>
            );
          })}
        </div>
        {verdict ? (
          <div className="flex min-w-0 flex-col gap-2">
            <p className={cn("rounded-lg px-3 py-2.5 text-sm font-medium", verdict.tone)}>{verdict.text}</p>
            {hits?.length ? (
              <ul className="flex min-w-0 flex-col gap-1.5">
                {hits.map((h, i) => {
                  const Icon = TYPE_ICON[h.listType] ?? Globe;
                  return (
                    <li key={`${h.listType}-${h.value}-${i}`} className="flex min-w-0 items-start gap-3 rounded-lg border px-3 py-2.5">
                      <span className={cn("grid size-8 shrink-0 place-items-center rounded-lg", KIND_STYLE[h.kind]?.plate)}>
                        <Icon className="size-4" />
                      </span>
                      <span className="flex min-w-0 flex-1 flex-col">
                        <span className="text-xs text-muted-foreground">{listEntrySentence({ type: h.listType, kind: h.kind })}</span>
                        <span className="font-mono text-sm break-all">{h.value}</span>
                        {h.reason ? <span className="text-xs text-foreground/80">{h.reason}</span> : null}
                      </span>
                      <span className={cn("shrink-0 text-xs font-medium", KIND_STYLE[h.kind]?.text)}>{KIND_MEANING[h.kind]}</span>
                    </li>
                  );
                })}
              </ul>
            ) : null}
          </div>
        ) : null}
      </div>
      <DialogFooter>
        <Button disabled={!any || check.isPending} onClick={run}>
          {check.isPending ? <Loader2 className="animate-spin" /> : <ScanSearch />}
          查询
        </Button>
      </DialogFooter>
    </>
  );
}
