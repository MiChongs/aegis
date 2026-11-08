"use client";

import { useCallback, useMemo, useState, useSyncExternalStore, type ReactNode } from "react";
import { AlertTriangle, RotateCcw, ScrollText } from "lucide-react";

import { CardEmpty } from "@/components/overview/overview-kit";
import { ListPagination } from "@/components/payment/payment-list-parts";
import { Panel, Segmented, TabStack } from "@/components/traffic/traffic-kit";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { CountryFlag } from "@/components/ui/country-flag";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { JsonViewer } from "@/components/ui/json-viewer";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import type { DecisionDetail, DecisionRow, ListHit } from "@/lib/api/risk";
import { formatCompact, formatDateTime } from "@/lib/datetime";
import { useRiskDecision, useRiskDecisions, useRiskScenes } from "@/lib/risk-hooks";
import { useDebouncedValue } from "@/lib/use-debounced-value";
import { cn } from "@/lib/utils";

import { useAppName } from "./kit/app-scope";
import { ActionBadge, KindBadge, LevelBadge, ScoreMeter } from "./kit/badges";
import { ACTION_LABEL, ACTION_ORDER, DECIDED_BY_LABEL, LIST_TYPE_LABEL, formatMicros, formatWindow } from "./kit/labels";
import { TraceView } from "./kit/trace-view";

/* ── 时间窗（总览与流水共用） ── */

export const RANGE_PRESETS = [
  { value: "1h", label: "1 小时", seconds: 3600 },
  { value: "24h", label: "24 小时", seconds: 86400 },
  { value: "7d", label: "7 天", seconds: 7 * 86400 },
  { value: "30d", label: "30 天", seconds: 30 * 86400 },
] as const;

export type RangeValue = (typeof RANGE_PRESETS)[number]["value"];

/**
 * 按分钟取整的「现在」。查询键里的起点由它推出：每分钟才变一次，
 * 既不会每次渲染都换键重查，窗口也不会停在打开页面那一刻。
 */
let clock = 0;
const floorMinute = (ms: number) => Math.floor(ms / 60_000) * 60_000;
function readClock() {
  if (!clock) clock = floorMinute(Date.now());
  return clock;
}
function subscribeClock(onChange: () => void) {
  const tick = () => {
    const next = floorMinute(Date.now());
    if (next !== clock) {
      clock = next;
      onChange();
    }
  };
  tick();
  const id = setInterval(tick, 15_000);
  return () => clearInterval(id);
}

/** 时间窗起点（毫秒时间戳字符串，后端同时接受 RFC3339 与毫秒） */
export function useRangeFrom(range: RangeValue) {
  const now = useSyncExternalStore(subscribeClock, readClock, readClock);
  const seconds = RANGE_PRESETS.find((r) => r.value === range)?.seconds ?? 86400;
  return String(now - seconds * 1000);
}

export function errorText(error: unknown, fallback = "读取失败") {
  return error instanceof Error && error.message ? error.message : fallback;
}

/* ── 流水 ── */

const PAGE_SIZE = 50;

interface Filters {
  scene: string;
  action: string;
  user: string;
  device: string;
  ip: string;
  account: string;
  rule: string;
  shadow: boolean;
  range: RangeValue;
}

const EMPTY: Filters = { scene: "", action: "", user: "", device: "", ip: "", account: "", rule: "", shadow: false, range: "24h" };

export function RiskDecisions({ app }: { app?: number }) {
  const [filters, setFilters] = useState<Filters>(EMPTY);
  const [page, setPage] = useState(1);
  const [openId, setOpenId] = useState<string | null>(null);
  const scenes = useRiskScenes();
  const sceneName = useSceneName();

  const set = <K extends keyof Filters>(key: K, value: Filters[K]) => {
    setFilters((f) => ({ ...f, [key]: value }));
    setPage(1);
  };

  // 五个文本框合成一个字符串防抖：传对象的话每次渲染都是新引用，计时器永远停不下来
  const [dUser = "", dDevice = "", dIp = "", dAccount = "", dRule = ""] = useDebouncedValue(
    [filters.user, filters.device, filters.ip, filters.account, filters.rule].join("\u0000")
  ).split("\u0000");
  const text = { user: dUser, device: dDevice, ip: dIp, account: dAccount, rule: dRule };
  const from = useRangeFrom(filters.range);
  const userId = Number(text.user.trim());
  const query = useRiskDecisions({
    app,
    scene: filters.scene || undefined,
    action: filters.action || undefined,
    user: Number.isFinite(userId) && userId > 0 ? userId : undefined,
    device: text.device.trim() || undefined,
    ip: text.ip.trim() || undefined,
    account: text.account.trim() || undefined,
    rule: text.rule.trim() || undefined,
    shadow: filters.shadow || undefined,
    from,
    page,
    limit: PAGE_SIZE,
  });

  const rows = query.data?.list ?? [];
  const total = query.data?.total ?? 0;
  const dirty = JSON.stringify({ ...filters, range: "" }) !== JSON.stringify({ ...EMPTY, range: "" });

  return (
    <TabStack>
      <div className="flex min-w-0 flex-col gap-3 rounded-2xl border border-border bg-card p-4 sm:px-5">
        <div className="flex min-w-0 flex-wrap items-center gap-2">
          <Select value={filters.scene || "all"} onValueChange={(v) => set("scene", v === "all" ? "" : v)}>
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
          <Select value={filters.action || "all"} onValueChange={(v) => set("action", v === "all" ? "" : v)}>
            <SelectTrigger size="sm" className="h-8 w-32">
              <SelectValue placeholder="全部处置" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">全部处置</SelectItem>
              {ACTION_ORDER.map((a) => (
                <SelectItem key={a} value={a}>
                  {ACTION_LABEL[a]}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Segmented value={filters.range} options={RANGE_PRESETS} onChange={(v) => set("range", v)} ariaLabel="时间范围" />
          <Label className="ml-auto inline-flex items-center gap-2 text-xs font-normal text-muted-foreground">
            <Switch checked={filters.shadow} onCheckedChange={(v) => set("shadow", v)} />
            影子流水
          </Label>
        </div>
        <div className="grid min-w-0 grid-cols-2 gap-2 md:grid-cols-3 xl:grid-cols-[repeat(5,minmax(0,1fr))_auto]">
          <Input className="h-8" inputMode="numeric" placeholder="用户 ID" value={filters.user} onChange={(e) => set("user", e.target.value)} />
          <Input className="h-8" placeholder="登录名" value={filters.account} onChange={(e) => set("account", e.target.value)} />
          <Input className="h-8 font-mono text-xs" placeholder="IP" value={filters.ip} onChange={(e) => set("ip", e.target.value)} />
          <Input className="h-8 font-mono text-xs" placeholder="设备 ID" value={filters.device} onChange={(e) => set("device", e.target.value)} />
          <Input className="h-8 font-mono text-xs" placeholder="规则键" value={filters.rule} onChange={(e) => set("rule", e.target.value)} />
          <Button
            size="sm"
            variant="ghost"
            disabled={!dirty}
            onClick={() => {
              setFilters((f) => ({ ...EMPTY, range: f.range }));
              setPage(1);
            }}
          >
            <RotateCcw />
            重置
          </Button>
        </div>
      </div>

      <Panel
        icon={ScrollText}
        tone="sky"
        title="决策流水"
        aside={query.isFetching && !query.isLoading ? <span className="text-xs text-muted-foreground">刷新中</span> : null}
        bodyClassName="px-0 pb-0 sm:px-0 sm:pb-0"
      >
        {query.isError && !query.data ? (
          <div className="flex items-center gap-2 border-t px-5 py-8 text-sm text-rose-600 dark:text-rose-400">
            <AlertTriangle className="size-4" />
            {errorText(query.error)}
          </div>
        ) : query.isLoading ? (
          <div className="flex flex-col gap-2 border-t p-4">
            {Array.from({ length: 8 }).map((_, i) => (
              <Skeleton key={i} className="h-10 rounded-lg" />
            ))}
          </div>
        ) : rows.length === 0 ? (
          <div className="border-t">
            <CardEmpty icon={ScrollText} title="暂无决策" />
          </div>
        ) : (
          <>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="pl-5">时间</TableHead>
                  <TableHead>场景</TableHead>
                  <TableHead>主体</TableHead>
                  <TableHead>IP</TableHead>
                  <TableHead>设备</TableHead>
                  <TableHead>处置</TableHead>
                  <TableHead className="w-32">分数</TableHead>
                  <TableHead className="text-right">命中</TableHead>
                  <TableHead className="text-right">耗时</TableHead>
                  <TableHead className="pr-5">版本</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {rows.map((row) => (
                  <DecisionTableRow key={row.id} row={row} sceneName={sceneName} onOpen={() => setOpenId(row.id)} />
                ))}
              </TableBody>
            </Table>
            <ListPagination page={page} totalPages={Math.max(1, Math.ceil(total / PAGE_SIZE))} total={total} unit="条" onPage={setPage} />
          </>
        )}
      </Panel>

      <DecisionDetailDialog id={openId} open={!!openId} onOpenChange={(o) => !o && setOpenId(null)} />
    </TabStack>
  );
}

/** 场景键 → 名称 */
export function useSceneName() {
  const scenes = useRiskScenes();
  const map = useMemo(() => new Map((scenes.data ?? []).map((s) => [s.key, s.name])), [scenes.data]);
  return useCallback((key: string) => map.get(key) ?? key, [map]);
}

function DecisionTableRow({ row, sceneName, onOpen }: { row: DecisionRow; sceneName: (k: string) => string; onOpen: () => void }) {
  return (
    <TableRow
      tabIndex={0}
      className="cursor-pointer"
      onClick={onOpen}
      onKeyDown={(e) => {
        if (e.key === "Enter") onOpen();
      }}
    >
      <TableCell className="pl-5 text-xs whitespace-nowrap text-muted-foreground tabular-nums" title={formatDateTime(row.ts)}>
        {formatCompact(row.ts)}
      </TableCell>
      <TableCell className="max-w-32 truncate text-sm">
        {sceneName(row.scene)}
        {row.shadow ? <Badge variant="secondary" size="sm" className="ml-1.5">影子</Badge> : null}
      </TableCell>
      <TableCell className="max-w-40">
        <Subject userId={row.userId} account={row.account} />
      </TableCell>
      <TableCell className="whitespace-nowrap">
        {row.ip ? (
          <span className="inline-flex items-center gap-1.5">
            {row.country ? <CountryFlag code={row.country} size={14} /> : null}
            <span className="font-mono text-xs">{row.ip}</span>
          </span>
        ) : (
          <span className="text-muted-foreground">—</span>
        )}
      </TableCell>
      <TableCell className="max-w-28 truncate font-mono text-xs text-muted-foreground" title={row.deviceId}>
        {row.deviceId || "—"}
      </TableCell>
      <TableCell>
        <ActionBadge action={row.action} />
      </TableCell>
      <TableCell>
        <ScoreMeter score={row.score} />
      </TableCell>
      <TableCell className="text-right text-xs tabular-nums">{row.hits?.length ?? 0}</TableCell>
      <TableCell className="text-right text-xs whitespace-nowrap text-muted-foreground tabular-nums">{formatMicros(row.latencyUs)}</TableCell>
      <TableCell className="max-w-24 truncate pr-5 font-mono text-[11px] text-muted-foreground">{row.release || "—"}</TableCell>
    </TableRow>
  );
}

export function Subject({ userId, account }: { userId: number; account: string }) {
  if (!userId && !account) return <span className="text-muted-foreground">—</span>;
  return (
    <div className="flex min-w-0 flex-col leading-tight">
      {userId ? <span className="truncate text-sm tabular-nums">#{userId}</span> : null}
      {account ? <span className={cn("truncate text-xs", userId ? "text-muted-foreground" : "text-sm")}>{account}</span> : null}
    </div>
  );
}

/* ── 决策详情 ── */

export function DecisionDetailDialog({ id, open, onOpenChange }: { id: string | null; open: boolean; onOpenChange: (open: boolean) => void }) {
  const detail = useRiskDecision(open ? id : null);
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[90vh] gap-4 overflow-y-auto sm:max-w-4xl">
        <DialogHeader>
          <DialogTitle>决策详情</DialogTitle>
          <DialogDescription className="font-mono text-xs break-all">{id}</DialogDescription>
        </DialogHeader>
        {detail.isError ? (
          <div className="flex items-center gap-2 rounded-lg border border-rose-500/30 bg-rose-500/5 px-3 py-6 text-sm text-rose-600 dark:text-rose-400">
            <AlertTriangle className="size-4" />
            {errorText(detail.error)}
          </div>
        ) : !detail.data ? (
          <div className="flex flex-col gap-3">
            <Skeleton className="h-20 rounded-xl" />
            <Skeleton className="h-9 w-80 rounded-lg" />
            <Skeleton className="h-64 rounded-xl" />
          </div>
        ) : (
          <DecisionDetailBody detail={detail.data} />
        )}
      </DialogContent>
    </Dialog>
  );
}

function DecisionDetailBody({ detail }: { detail: DecisionDetail }) {
  const d = detail.decision;
  const trace = detail.trace;
  const appName = useAppName();
  const sceneName = useSceneName();
  const features = Object.entries(detail.features ?? {}).sort(([a], [b]) => a.localeCompare(b));
  const models = Object.entries(detail.models ?? {});
  const device = Object.entries(detail.device ?? {}).filter(([, v]) => v !== "" && v !== null && v !== undefined);
  const listHits = trace?.listHits ?? [];
  const commands = trace?.commands ?? [];
  const extra = detail.extra && Object.keys(detail.extra).length > 0 ? detail.extra : null;

  return (
    <div className="flex min-w-0 flex-col gap-4">
      <div className="grid min-w-0 gap-3 rounded-xl border border-border bg-muted/30 p-4 sm:grid-cols-[auto_minmax(0,1fr)_auto] sm:items-center">
        <div className="flex items-center gap-2">
          <ActionBadge action={d.action} size="default" />
          <LevelBadge level={d.level} size="default" />
          {d.shadow ? <Badge variant="secondary">影子</Badge> : null}
        </div>
        <div className="flex min-w-0 items-center gap-3">
          <span className="text-3xl leading-8 font-semibold tracking-tight tabular-nums">{d.score}</span>
          <ScoreMeter score={d.score} className="min-w-0 flex-1" />
        </div>
        <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-muted-foreground">
          <span>
            耗时 <span className="font-medium text-foreground tabular-nums">{formatMicros(d.latencyUs)}</span>
          </span>
          {trace?.decidedBy ? (
            <span>
              决定方 <span className="font-medium text-foreground">{DECIDED_BY_LABEL[trace.decidedBy] ?? trace.decidedBy}</span>
            </span>
          ) : null}
        </div>
      </div>

      <Tabs defaultValue="summary" className="gap-4">
        <TabsList className="w-full justify-start overflow-x-auto sm:w-fit">
          <TabsTrigger value="summary">概要</TabsTrigger>
          <TabsTrigger value="trace">轨迹</TabsTrigger>
          <TabsTrigger value="features">特征与模型</TabsTrigger>
          <TabsTrigger value="device">设备与扩展</TabsTrigger>
        </TabsList>

        <TabsContent value="summary" className="flex min-w-0 flex-col gap-4">
          <dl className="grid min-w-0 grid-cols-2 gap-x-4 gap-y-3 md:grid-cols-3">
            <Fact label="时间" value={formatDateTime(d.ts)} />
            <Fact label="场景" value={sceneName(d.scene)} />
            <Fact label="应用" value={appName(d.appId)} />
            <Fact label="用户" value={d.userId ? `#${d.userId}` : "—"} />
            <Fact label="登录名" value={d.account || "—"} />
            <Fact label="IP" value={d.ip || "—"} mono />
            <Fact
              label="地区"
              value={
                [d.country, detail.province, d.city].filter(Boolean).length ? (
                  <span className="inline-flex items-center gap-1.5">
                    {d.country ? <CountryFlag code={d.country} size={14} /> : null}
                    {[d.country, detail.province, d.city].filter(Boolean).join(" ")}
                  </span>
                ) : (
                  "—"
                )
              }
            />
            <Fact label="运营商" value={[detail.isp, detail.asn ? `AS${detail.asn}` : ""].filter(Boolean).join(" ") || "—"} />
            <Fact label="设备 ID" value={d.deviceId || "—"} mono />
            <Fact label="版本" value={d.release || "—"} mono />
            <Fact label="UA" value={detail.ua || "—"} className="col-span-2 md:col-span-3" wrap />
          </dl>

          <Section title="原因">
            {d.reasons?.length ? (
              <ul className="flex flex-col gap-1">
                {d.reasons.map((r, i) => (
                  <li key={i} className="flex gap-2 text-sm">
                    <span className="mt-2 size-1.5 shrink-0 rounded-full bg-amber-500" />
                    {r}
                  </li>
                ))}
              </ul>
            ) : (
              <Muted>无</Muted>
            )}
          </Section>

          <Section title="名单命中">
            {listHits.length ? <ListHitTable hits={listHits} /> : <Muted>无</Muted>}
          </Section>

          <Section title="处置命令">
            {commands.length ? (
              <ul className="flex flex-col gap-1.5">
                {commands.map((c, i) => (
                  <CommandRow key={i} command={c} />
                ))}
              </ul>
            ) : (
              <Muted>无</Muted>
            )}
          </Section>
        </TabsContent>

        <TabsContent value="trace">
          {trace ? <TraceView trace={trace} /> : <Muted>暂无轨迹</Muted>}
        </TabsContent>

        <TabsContent value="features" className="flex min-w-0 flex-col gap-4">
          <Section title={`特征 ${features.length}`}>
            {features.length ? (
              <div className="overflow-hidden rounded-lg border">
                <Table>
                  <TableBody>
                    {features.map(([k, v]) => (
                      <TableRow key={k}>
                        <TableCell className="font-mono text-xs">{k}</TableCell>
                        <TableCell className={cn("text-right text-sm tabular-nums", v === 0 && "text-muted-foreground")}>{formatNumber(v)}</TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </div>
            ) : (
              <Muted>无</Muted>
            )}
          </Section>
          <Section title="模型分">
            {models.length ? (
              <ul className="flex flex-col gap-2">
                {models.map(([k, v]) => {
                  const pct = Math.max(0, Math.min(1, v)) * 100;
                  return (
                    <li key={k} className="flex min-w-0 items-center gap-3">
                      <span className="w-40 shrink-0 truncate font-mono text-xs">{k}</span>
                      <div className="h-2 min-w-0 flex-1 overflow-hidden rounded-full bg-muted">
                        <div className={cn("h-full rounded-full", pct >= 80 ? "bg-rose-500" : pct >= 50 ? "bg-amber-500" : "bg-sky-500")} style={{ width: `${pct}%` }} />
                      </div>
                      <span className="w-14 shrink-0 text-right text-xs tabular-nums">{v.toFixed(3)}</span>
                    </li>
                  );
                })}
              </ul>
            ) : (
              <Muted>无</Muted>
            )}
          </Section>
        </TabsContent>

        <TabsContent value="device" className="flex min-w-0 flex-col gap-4">
          <Section title="设备指纹">
            {device.length ? (
              <dl className="grid min-w-0 grid-cols-2 gap-x-4 gap-y-3 md:grid-cols-3">
                {device.map(([k, v]) => (
                  <Fact key={k} label={k} value={String(v)} mono />
                ))}
              </dl>
            ) : (
              <Muted>无</Muted>
            )}
          </Section>
          <Section title="扩展字段">
            {extra ? <JsonViewer value={JSON.stringify(extra, null, 2)} height={220} /> : <Muted>无</Muted>}
          </Section>
        </TabsContent>
      </Tabs>
    </div>
  );
}

const COMMAND_LABEL: Record<string, string> = {
  list_add: "加入名单", list_remove: "移出名单", account_ban: "封号", account_unban: "解封", firewall_ban: "封 IP",
  firewall_unban: "解封 IP", revoke_sessions: "踢下线",
};

const TARGET_LABEL: Record<string, string> = { device: "设备", ip: "IP", account: "登录名", user: "用户" };

function CommandRow({ command }: { command: unknown }) {
  if (!command || typeof command !== "object") {
    return <li className="font-mono text-xs">{JSON.stringify(command)}</li>;
  }
  const c = command as Record<string, unknown>;
  const kind = String(c.kind ?? "");
  const duration = Number(c.duration_seconds ?? c.durationSeconds ?? 0);
  return (
    <li className="flex min-w-0 flex-wrap items-center gap-2 rounded-lg border border-border px-3 py-2 text-sm">
      <Badge variant={kind.includes("ban") ? "danger" : "secondary"} size="sm">
        {COMMAND_LABEL[kind] ?? kind}
      </Badge>
      {c.target ? <span className="text-xs text-muted-foreground">{TARGET_LABEL[String(c.target)] ?? String(c.target)}</span> : null}
      {c.value ? <span className="font-mono text-xs">{String(c.value)}</span> : null}
      {kind.endsWith("ban") || kind === "list_add" ? <span className="text-xs text-muted-foreground">{formatWindow(duration)}</span> : null}
      {c.reason ? <span className="ml-auto truncate text-xs text-muted-foreground">{String(c.reason)}</span> : null}
    </li>
  );
}

export function ListHitTable({ hits }: { hits: ListHit[] }) {
  return (
    <div className="overflow-hidden rounded-lg border">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead className="pl-3">类型</TableHead>
            <TableHead>取值</TableHead>
            <TableHead>种类</TableHead>
            <TableHead className="pr-3">原因</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {hits.map((h, i) => (
            <TableRow key={`${h.listType}-${h.value}-${i}`}>
              <TableCell className="pl-3 text-xs">{LIST_TYPE_LABEL[h.listType] ?? h.listType}</TableCell>
              <TableCell className="font-mono text-xs">{h.value}</TableCell>
              <TableCell>
                <KindBadge kind={h.kind} />
              </TableCell>
              <TableCell className="max-w-64 truncate pr-3 text-xs text-muted-foreground">{h.reason || "—"}</TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  );
}

/* ── 小件 ── */

export function Fact({ label, value, mono, wrap, className }: { label: string; value: ReactNode; mono?: boolean; wrap?: boolean; className?: string }) {
  return (
    <div className={cn("min-w-0", className)}>
      <dt className="truncate text-[11px] leading-4 text-muted-foreground">{label}</dt>
      <dd className={cn("text-sm leading-5", wrap ? "break-all" : "truncate", mono && "font-mono text-xs leading-5")} title={typeof value === "string" ? value : undefined}>
        {value}
      </dd>
    </div>
  );
}

function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="flex min-w-0 flex-col gap-2">
      <h4 className="text-xs font-medium text-muted-foreground">{title}</h4>
      {children}
    </section>
  );
}

function Muted({ children }: { children: ReactNode }) {
  return <p className="text-sm text-muted-foreground">{children}</p>;
}

function formatNumber(v: number) {
  if (Number.isInteger(v)) return v.toLocaleString("zh-CN");
  return v.toLocaleString("zh-CN", { maximumFractionDigits: 4 });
}
