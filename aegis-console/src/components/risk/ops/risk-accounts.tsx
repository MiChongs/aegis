"use client";

import { useMemo, useState, type ReactNode } from "react";
import Link from "next/link";
import {
  Ban, ClipboardX, ExternalLink, Gauge, ListX, Radar, Search, ShieldCheck, ShieldOff, Smartphone, UserSearch, UserX, Users,
  type LucideIcon,
} from "lucide-react";

import { TonalIcon } from "@/components/apps/detail/app-detail-kit";
import { CardEmpty } from "@/components/overview/overview-kit";
import { ListPagination } from "@/components/payment/payment-list-parts";
import { remaining } from "@/components/risk/kit/humanize";
import { Segmented } from "@/components/traffic/traffic-kit";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import type { CandidateSource, OpsCandidate, OpsCluster, OpsUser } from "@/lib/api/risk-ops";
import { useAdminAppsQuery } from "@/lib/admin-hooks";
import { formatDateTime, formatRelative } from "@/lib/datetime";
import { useCanOperateRisk, useRiskCandidates } from "@/lib/risk-ops-hooks";
import { useDebouncedValue } from "@/lib/use-debounced-value";
import { cn } from "@/lib/utils";

import { AccountAvatar, AccountState, accountMeta, accountName } from "./ops-kit";
import { RiskOpsFlow, type RiskOpsMode } from "./risk-ops-flow";

/**
 * 风控平台 → 账号处置。
 *
 * 不需要先打开某个用户：按风控信号直接找出风险账号（风控封禁中、多次被拦截、高风险分、在黑名单里、复核确认有风险、
 * 设备聚集），勾选后一键解除或删除；也可以粘贴用户 ID。解除与删除都会先列出关联账号与逐项处置清单（RiskOpsFlow），
 * 确认后才执行。
 */

const SOURCES: Array<{ value: CandidateSource; label: string; icon: LucideIcon; hint: string }> = [
  { value: "banned", label: "风控封禁中", icon: Ban, hint: "被风控平台封号、尚未解封的账号" },
  { value: "blocked", label: "多次被拦截", icon: ShieldOff, hint: "一段时间内被风控拦截或封禁达到次数门槛的账号" },
  { value: "score", label: "高风险分", icon: Gauge, hint: "一段时间内风险分达到门槛的账号" },
  { value: "device", label: "设备聚集", icon: Smartphone, hint: "同一台设备上登录过多个账号，常见于批量注册与养号" },
  { value: "listed", label: "黑名单中", icon: ListX, hint: "在风控用户黑名单里的账号" },
  { value: "confirmed", label: "复核确认有风险", icon: ClipboardX, hint: "人工复核结论为确认有风险的账号" },
];

const DAYS = [
  { value: "7", label: "近 7 天" },
  { value: "30", label: "近 30 天" },
  { value: "90", label: "近 90 天" },
] as const;

const THRESHOLDS: Partial<Record<CandidateSource, { options: ReadonlyArray<{ value: string; label: string }>; def: string }>> = {
  blocked: { options: [{ value: "3", label: "3 次以上" }, { value: "5", label: "5 次以上" }, { value: "10", label: "10 次以上" }], def: "3" },
  score: { options: [{ value: "60", label: "60 分以上" }, { value: "80", label: "80 分以上" }, { value: "90", label: "90 分以上" }], def: "80" },
  device: { options: [{ value: "3", label: "3 个账号以上" }, { value: "5", label: "5 个以上" }, { value: "10", label: "10 个以上" }], def: "3" },
};

const usesDays = (s: CandidateSource) => s === "blocked" || s === "score" || s === "confirmed";
const LIMIT = 20;

export function RiskAccounts({ app }: { app?: number }) {
  const canOperate = useCanOperateRisk();
  const apps = useAdminAppsQuery();
  const appKey = useMemo(() => new Map((apps.data ?? []).map((a) => [a.id, a.appKey])), [apps.data]);
  const [source, setSource] = useState<CandidateSource>("banned");
  const [days, setDays] = useState<"7" | "30" | "90">("30");
  const [threshold, setThreshold] = useState<Partial<Record<CandidateSource, string>>>({});
  const [keyword, setKeyword] = useState("");
  const q = useDebouncedValue(keyword.trim(), 300);
  const [page, setPage] = useState(1);
  const minValue = threshold[source] ?? THRESHOLDS[source]?.def;
  const found = useRiskCandidates({
    app, source, q: q || undefined,
    days: usesDays(source) ? Number(days) : undefined,
    min: minValue ? Number(minValue) : undefined,
    page: source === "banned" ? page : undefined,
    limit: source === "banned" ? LIMIT : 200,
  });
  const items = found.data?.items ?? [];
  const clusters = useMemo(() => {
    const all = found.data?.clusters ?? [];
    if (!q) return all;
    const kw = q.toLowerCase();
    return all.filter((c) => c.members.some((m) => `${m.account} ${m.nickname ?? ""} ${m.id} ${m.publicId}`.toLowerCase().includes(kw)) || (c.model ?? "").toLowerCase().includes(kw));
  }, [found.data, q]);
  const total = found.data?.total ?? 0;

  const [selected, setSelected] = useState<Map<number, OpsUser>>(new Map());
  const [flow, setFlow] = useState<{ mode: RiskOpsMode; appId: number; ids: number[]; manual?: boolean } | null>(null);
  const [manual, setManual] = useState("");

  const selectedApps = new Set([...selected.values()].map((u) => u.appid));
  const oneApp = selectedApps.size === 1 ? [...selectedApps][0] : undefined;
  const visibleUsers = source === "device" ? clusters.flatMap((c) => c.members) : items.map((i) => i.user);
  const allVisible = visibleUsers.length > 0 && visibleUsers.every((u) => selected.has(u.id));

  const setMany = (users: OpsUser[], on: boolean) =>
    setSelected((prev) => {
      const next = new Map(prev);
      for (const u of users) {
        if (on) next.set(u.id, u);
        else next.delete(u.id);
      }
      return next;
    });

  const manualIds = useMemo(
    () => [...new Set(manual.split(/[\s,，、;；]+/).map((x) => Number(x)).filter((n) => Number.isInteger(n) && n > 0))].slice(0, 100),
    [manual],
  );

  const start = (mode: RiskOpsMode, appId: number | undefined, ids: number[], isManual = false) => {
    if (!appId || !ids.length) return;
    setFlow({ mode, appId, ids, manual: isManual });
  };

  const meta = SOURCES.find((s) => s.value === source)!;
  const threshold_ = THRESHOLDS[source];
  const summary = found.isLoading
    ? "正在查找"
    : source === "device"
      ? clusters.length
        ? `找到 ${clusters.length} 台设备，共 ${clusters.reduce((n, c) => n + c.members.length, 0)} 个账号`
        : "没有找到设备聚集"
      : total
        ? `找到 ${total} 个账号`
        : "没有找到符合条件的账号";

  return (
    <div className="space-y-4">
      <div className="grid gap-4 lg:grid-cols-[minmax(0,1.4fr)_minmax(0,1fr)]">
        <div className="grid gap-3 sm:grid-cols-2">
          <ExplainCard
            icon={ShieldCheck}
            tone="emerald"
            title="一键解除风控"
            text="误判时使用。撤销风控下发的封号、防火墙封禁和名单，待复核单按「判定为正常」结案，可加一段白名单保护。"
          />
          <ExplainCard
            icon={UserX}
            tone="rose"
            title="删除风险账号"
            text="确认是刷量或作弊账号时使用。在下面按风控信号找出账号，连同关联账号一起删除，可同时拉黑它们用过的设备。"
          />
        </div>
        <div className="rounded-xl border bg-card p-4">
          <div className="flex items-center gap-2">
            <UserSearch className="size-4 text-muted-foreground" />
            <h3 className="text-sm font-medium">按用户 ID 处置</h3>
          </div>
          <p className="mt-1 text-xs text-muted-foreground">
            {app ? "填一个或多个用户 ID，用空格或逗号分隔，最多 100 个" : "先在右上角选择应用，再填用户 ID"}
          </p>
          <Input value={manual} onChange={(e) => setManual(e.target.value)} placeholder="例如 3660 3661" className="mt-3" disabled={!app} />
          <div className="mt-3 flex flex-wrap items-center justify-end gap-2">
            <Button size="sm" variant="outline" disabled={!app || !manualIds.length || !canOperate} onClick={() => start("release", app, manualIds, true)}>
              <ShieldOff className="size-3.5" />
              解除风控
            </Button>
            <Button
              size="sm"
              variant="outline"
              className="text-rose-600 hover:text-rose-700 dark:text-rose-400"
              disabled={!app || !manualIds.length || !canOperate}
              onClick={() => start("purge", app, manualIds, true)}
            >
              <UserX className="size-3.5" />
              删除账号
            </Button>
          </div>
        </div>
      </div>

      <section className="overflow-hidden rounded-xl border bg-card">
        <div className="space-y-3 border-b px-4 py-3">
          <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
            <div className="flex items-center gap-3">
              <TonalIcon icon={Radar} tone="rose" />
              <div>
                <h3 className="text-sm font-medium">发现风险账号</h3>
                <p className="text-xs text-muted-foreground">{meta.hint}</p>
              </div>
            </div>
            <div className="relative sm:w-64">
              <Search className="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-muted-foreground" />
              <Input
                value={keyword}
                onChange={(e) => {
                  setKeyword(e.target.value);
                  setPage(1);
                }}
                placeholder="在结果里搜登录名、昵称或用户 ID"
                className="h-8 pl-8 text-sm"
              />
            </div>
          </div>
          <div className="flex flex-wrap gap-1.5">
            {SOURCES.map((s) => (
              <button
                key={s.value}
                type="button"
                onClick={() => {
                  setSource(s.value);
                  setPage(1);
                }}
                className={cn(
                  "inline-flex items-center gap-1.5 rounded-lg border px-2.5 py-1.5 text-xs transition-colors",
                  source === s.value ? "border-primary/50 bg-primary/[0.06] font-medium text-foreground" : "text-muted-foreground hover:bg-muted/50 hover:text-foreground",
                )}
              >
                <s.icon className="size-3.5" />
                {s.label}
              </button>
            ))}
          </div>
          {usesDays(source) || threshold_ ? (
            <div className="flex flex-wrap items-center gap-2">
              {usesDays(source) ? <Segmented value={days} options={DAYS} onChange={setDays} ariaLabel="时间范围" /> : null}
              {threshold_ ? (
                <Segmented
                  value={minValue ?? threshold_.def}
                  options={threshold_.options}
                  onChange={(v) => setThreshold((t) => ({ ...t, [source]: v }))}
                  ariaLabel="门槛"
                />
              ) : null}
            </div>
          ) : null}
        </div>

        {selected.size ? (
          <div className="sticky top-0 z-10 flex flex-col gap-2 border-b bg-background/95 px-4 py-2.5 backdrop-blur sm:flex-row sm:items-center sm:justify-between">
            <p className="text-sm">
              已选 <span className="font-medium tabular-nums">{selected.size}</span> 个账号
              {selectedApps.size > 1 ? <span className="ml-2 text-xs text-amber-600 dark:text-amber-400">分属 {selectedApps.size} 个应用，请只选同一应用的账号</span> : null}
              {selected.size > 100 ? <span className="ml-2 text-xs text-amber-600 dark:text-amber-400">一次最多处置 100 个</span> : null}
            </p>
            <div className="flex items-center gap-2">
              <Button size="sm" variant="ghost" onClick={() => setSelected(new Map())}>取消选择</Button>
              <Button size="sm" variant="outline" disabled={!oneApp || !canOperate || selected.size > 100} onClick={() => start("release", oneApp, [...selected.keys()])}>
                <ShieldOff className="size-3.5" />
                解除风控
              </Button>
              <Button size="sm" variant="destructive" disabled={!oneApp || !canOperate || selected.size > 100} onClick={() => start("purge", oneApp, [...selected.keys()])}>
                <UserX className="size-3.5" />
                删除 {selected.size} 个账号
              </Button>
            </div>
          </div>
        ) : null}

        {found.isLoading ? (
          <LoadingList />
        ) : found.isError ? (
          <CardEmpty icon={Radar} title="暂时查不到风险账号" description={(found.error as Error)?.message} />
        ) : (source === "device" ? !clusters.length : !items.length) ? (
          <CardEmpty icon={ShieldCheck} title={q ? "没有匹配的账号" : "没有找到风险账号"} description={q ? "换个关键词试试" : "可以放宽时间范围或门槛再看看"} />
        ) : (
          <>
            <div className="flex items-center gap-3 border-b px-4 py-2 text-xs text-muted-foreground">
              <Checkbox checked={allVisible} onCheckedChange={(v) => setMany(visibleUsers, v === true)} aria-label="选择全部结果" />
              <span>{summary}</span>
            </div>
            {source === "device" ? (
              <div className="grid gap-3 p-3 xl:grid-cols-2">
                {clusters.map((c) => (
                  <ClusterCard
                    key={`${c.appid}-${c.deviceKey}`}
                    cluster={c}
                    showApp={!app}
                    selected={selected}
                    onToggle={(u, v) => setMany([u], v)}
                    onToggleAll={(v) => setMany(c.members, v)}
                    canOperate={canOperate}
                    onPurge={() => start("purge", c.appid, c.members.map((m) => m.id))}
                  />
                ))}
              </div>
            ) : (
              <ul className="divide-y">
                {items.map((it) => (
                  <CandidateRow
                    key={`${it.user.appid}-${it.user.id}`}
                    item={it}
                    source={source}
                    days={Number(days)}
                    checked={selected.has(it.user.id)}
                    onToggle={(v) => setMany([it.user], v)}
                    showApp={!app}
                    href={appKey.get(it.user.appid) ? `/app-users/${appKey.get(it.user.appid)}/${it.user.id}?tab=risk` : undefined}
                    canOperate={canOperate}
                    onRelease={() => start("release", it.user.appid, [it.user.id])}
                    onPurge={() => start("purge", it.user.appid, [it.user.id])}
                  />
                ))}
              </ul>
            )}
            {source === "banned" && total > LIMIT ? (
              <div className="border-t px-4 py-2">
                <ListPagination page={page} totalPages={Math.max(1, Math.ceil(total / LIMIT))} total={total} unit="个账号" onPage={setPage} />
              </div>
            ) : null}
          </>
        )}
      </section>

      <RiskOpsFlow
        open={!!flow}
        onOpenChange={(open) => !open && setFlow(null)}
        mode={flow?.mode ?? "release"}
        appId={flow?.appId}
        userIds={flow?.ids ?? []}
        onFinished={({ done }) => {
          setSelected((prev) => {
            const next = new Map(prev);
            for (const id of done) next.delete(id);
            return next;
          });
          if (flow?.manual) setManual("");
        }}
      />
    </div>
  );
}

function ExplainCard({ icon, tone, title, text }: { icon: LucideIcon; tone: "emerald" | "rose"; title: string; text: string }) {
  return (
    <div className="flex items-start gap-3 rounded-xl border bg-card p-4">
      <TonalIcon icon={icon} tone={tone} />
      <div className="min-w-0">
        <h3 className="text-sm font-medium">{title}</h3>
        <p className="mt-1 text-xs leading-5 text-muted-foreground">{text}</p>
      </div>
    </div>
  );
}

function LoadingList() {
  return (
    <div className="divide-y">
      {Array.from({ length: 5 }, (_, i) => (
        <div key={i} className="flex items-center gap-3 px-4 py-3">
          <Skeleton className="size-4" />
          <Skeleton className="size-9 rounded-full" />
          <div className="flex-1 space-y-1.5">
            <Skeleton className="h-3.5 w-48" />
            <Skeleton className="h-3 w-80" />
          </div>
        </div>
      ))}
    </div>
  );
}

/** 这个账号为什么被列出来 */
function signalText(it: OpsCandidate, source: CandidateSource, days: number): ReactNode {
  const last = it.lastAt ? formatRelative(it.lastAt) : "";
  const why = it.reason ? `：${it.reason}` : "";
  switch (source) {
    case "banned":
      return `${last}被风控封禁，${it.expiresAt ? `到 ${formatDateTime(it.expiresAt)} 解封` : "永久"}${it.reason ? `，原因：${it.reason}` : ""}`;
    case "blocked":
      return `近 ${days} 天被拦截 ${it.count} 次，最高风险分 ${it.maxScore}${last ? `，最近一次在${last}` : ""}${why}`;
    case "score":
      return `近 ${days} 天最高风险分 ${it.maxScore}，有 ${it.count} 次被判为有风险${last ? `，最近一次在${last}` : ""}${why}`;
    case "listed":
      return `在用户黑名单中，${remaining(it.expiresAt).text}${last ? `，${last}加入` : ""}${it.reason ? `，原因：${it.reason}` : ""}`;
    case "confirmed":
      return `复核确认有风险 ${it.count} 次${last ? `，最近一次在${last}` : ""}${why}`;
  }
  return "";
}

function CandidateRow({
  item, source, days, checked, onToggle, showApp, href, canOperate, onRelease, onPurge,
}: {
  item: OpsCandidate;
  source: CandidateSource;
  days: number;
  checked: boolean;
  onToggle: (v: boolean) => void;
  showApp: boolean;
  href?: string;
  canOperate: boolean;
  onRelease: () => void;
  onPurge: () => void;
}) {
  const u = item.user;
  return (
    <li className={cn("group flex items-start gap-3 px-4 py-3 transition-colors hover:bg-muted/30", checked && "bg-primary/[0.03]")}>
      <Checkbox checked={checked} onCheckedChange={(v) => onToggle(v === true)} className="mt-2.5" aria-label={`选择 ${accountName(u, u.id)}`} />
      <AccountAvatar user={u} id={u.id} />
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-x-2 gap-y-0.5">
          <span className="truncate text-sm font-medium">{accountName(u, u.id)}</span>
          {source !== "banned" ? <AccountState user={u} /> : null}
          {showApp && u.appName ? <span className="text-xs text-muted-foreground">{u.appName}</span> : null}
        </div>
        <p className="truncate text-xs text-muted-foreground">{accountMeta(u, u.id)}</p>
        <p className="mt-1 text-xs leading-5 text-foreground/80">{signalText(item, source, days)}</p>
      </div>
      <RowActions href={href} canOperate={canOperate} onRelease={onRelease} onPurge={onPurge} />
    </li>
  );
}

function RowActions({ href, canOperate, onRelease, onPurge }: { href?: string; canOperate: boolean; onRelease: () => void; onPurge: () => void }) {
  return (
    <div className="flex shrink-0 items-center gap-1 sm:opacity-60 sm:transition-opacity sm:group-hover:opacity-100">
      {href ? (
        <Button asChild size="sm" variant="ghost" className="h-8 px-2" title="查看风险详情">
          <Link href={href}>
            <ExternalLink className="size-3.5" />
          </Link>
        </Button>
      ) : null}
      <Button size="sm" variant="ghost" className="h-8 px-2" disabled={!canOperate} onClick={onRelease} title="解除风控">
        <ShieldOff className="size-3.5" />
        <span className="hidden md:inline">解除</span>
      </Button>
      <Button size="sm" variant="ghost" className="h-8 px-2 text-rose-600 hover:text-rose-700 dark:text-rose-400" disabled={!canOperate} onClick={onPurge} title="删除账号">
        <UserX className="size-3.5" />
        <span className="hidden md:inline">删除</span>
      </Button>
    </div>
  );
}

function ClusterCard({
  cluster, showApp, selected, onToggle, onToggleAll, canOperate, onPurge,
}: {
  cluster: OpsCluster;
  showApp: boolean;
  selected: Map<number, OpsUser>;
  onToggle: (u: OpsUser, v: boolean) => void;
  onToggleAll: (v: boolean) => void;
  canOperate: boolean;
  onPurge: () => void;
}) {
  const [open, setOpen] = useState(false);
  const all = cluster.members.every((m) => selected.has(m.id));
  const some = !all && cluster.members.some((m) => selected.has(m.id));
  const banned = cluster.members.filter((m) => m.ban).length;
  const label = cluster.model || cluster.name || "未知型号";
  const shown = open ? cluster.members : cluster.members.slice(0, 6);
  return (
    <div className={cn("overflow-hidden rounded-xl border", (all || some) && "border-primary/40")}>
      <div className="flex items-start gap-3 border-b bg-muted/30 px-3 py-3">
        <Checkbox checked={all ? true : some ? "indeterminate" : false} onCheckedChange={(v) => onToggleAll(v === true)} className="mt-2.5" aria-label="选中整组" />
        <TonalIcon icon={Smartphone} tone={cluster.members.length >= 10 ? "rose" : "amber"} />
        <div className="min-w-0 flex-1">
          <p className="truncate text-sm font-medium">
            {label} 上登录过 <span className="tabular-nums">{cluster.users}</span> 个账号
          </p>
          <p className="mt-0.5 truncate text-xs text-muted-foreground">
            {[
              showApp && cluster.appName ? cluster.appName : "",
              cluster.platform ? cluster.platform : "",
              `最近一次 ${formatRelative(cluster.lastSeen)}`,
              banned ? `其中 ${banned} 个已被封禁` : "",
            ]
              .filter(Boolean)
              .join("，")}
          </p>
          <p className="mt-0.5 truncate font-mono text-[11px] text-muted-foreground/80" title={cluster.deviceKey}>
            设备码 {cluster.deviceKey.length > 24 ? `${cluster.deviceKey.slice(0, 12)}…${cluster.deviceKey.slice(-8)}` : cluster.deviceKey}
          </p>
        </div>
        <Button
          size="sm"
          variant="ghost"
          className="h-8 shrink-0 px-2 text-rose-600 hover:text-rose-700 dark:text-rose-400"
          disabled={!canOperate || cluster.members.length > 100}
          title={cluster.members.length > 100 ? "一次最多删除 100 个账号，请勾选部分账号" : "删除这台设备上的全部账号"}
          onClick={onPurge}
        >
          <Users className="size-3.5" />
          整组删除
        </Button>
      </div>
      <ul className="divide-y">
        {shown.map((m) => (
          <li key={m.id}>
            <label className="flex cursor-pointer items-center gap-3 px-3 py-2 transition-colors hover:bg-muted/30">
              <Checkbox checked={selected.has(m.id)} onCheckedChange={(v) => onToggle(m, v === true)} aria-label={`选择 ${accountName(m, m.id)}`} />
              <AccountAvatar user={m} id={m.id} size="sm" />
              <span className="min-w-0 flex-1">
                <span className="block truncate text-sm">{accountName(m, m.id)}</span>
                <span className="block truncate text-xs text-muted-foreground">{accountMeta(m, m.id)}</span>
              </span>
              <AccountState user={m} />
            </label>
          </li>
        ))}
      </ul>
      {cluster.members.length > 6 ? (
        <button
          type="button"
          onClick={() => setOpen((o) => !o)}
          className="w-full border-t py-2 text-xs text-muted-foreground transition-colors hover:bg-muted/40 hover:text-foreground"
        >
          {open ? "收起" : `展开其余 ${cluster.members.length - 6} 个账号`}
        </button>
      ) : null}
    </div>
  );
}
