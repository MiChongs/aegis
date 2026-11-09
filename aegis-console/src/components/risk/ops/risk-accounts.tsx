"use client";

import { useMemo, useState } from "react";
import Link from "next/link";
import { ExternalLink, Search, ShieldCheck, ShieldOff, UserSearch, UserX, Users } from "lucide-react";

import { TonalIcon } from "@/components/apps/detail/app-detail-kit";
import { CardEmpty } from "@/components/overview/overview-kit";
import { ListPagination } from "@/components/payment/payment-list-parts";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import type { OpsUser } from "@/lib/api/risk-ops";
import { useAdminAppsQuery } from "@/lib/admin-hooks";
import { formatDateTime, formatRelative } from "@/lib/datetime";
import { useCanOperateRisk, useRiskBanned } from "@/lib/risk-ops-hooks";
import { useDebouncedValue } from "@/lib/use-debounced-value";
import { cn } from "@/lib/utils";

import { AccountAvatar, accountMeta, accountName } from "./ops-kit";
import { RiskOpsFlow, type RiskOpsMode } from "./risk-ops-flow";

/**
 * 风控平台 → 账号处置。
 *
 * 上半是两种处置的说明与「按用户 ID 处置」入口；下半是正被风控平台封禁的账号，可多选后一键解除或删除。
 * 解除与删除都会先列出关联账号与逐项处置清单（RiskOpsFlow），确认后才执行。
 */

const LIMIT = 20;

export function RiskAccounts({ app }: { app?: number }) {
  const canOperate = useCanOperateRisk();
  const apps = useAdminAppsQuery();
  const appKey = useMemo(() => new Map((apps.data ?? []).map((a) => [a.id, a.appKey])), [apps.data]);
  const [keyword, setKeyword] = useState("");
  const q = useDebouncedValue(keyword.trim(), 300);
  const [page, setPage] = useState(1);
  const banned = useRiskBanned({ app, q, page, limit: LIMIT });
  const items = banned.data?.items ?? [];
  const total = banned.data?.total ?? 0;
  const [selected, setSelected] = useState<Map<number, OpsUser>>(new Map());
  const [flow, setFlow] = useState<{ mode: RiskOpsMode; appId: number; ids: number[] } | null>(null);
  const [manual, setManual] = useState("");

  const selectedApps = new Set([...selected.values()].map((u) => u.appid));
  const oneApp = selectedApps.size === 1 ? [...selectedApps][0] : undefined;
  const allOnPage = items.length > 0 && items.every((u) => selected.has(u.id));

  const toggle = (u: OpsUser, v: boolean) =>
    setSelected((prev) => {
      const next = new Map(prev);
      if (v) next.set(u.id, u);
      else next.delete(u.id);
      return next;
    });
  const togglePage = (v: boolean) =>
    setSelected((prev) => {
      const next = new Map(prev);
      for (const u of items) {
        if (v) next.set(u.id, u);
        else next.delete(u.id);
      }
      return next;
    });

  const manualIds = useMemo(
    () => [...new Set(manual.split(/[\s,，、;；]+/).map((x) => Number(x)).filter((n) => Number.isInteger(n) && n > 0))].slice(0, 100),
    [manual],
  );

  const start = (mode: RiskOpsMode, appId: number | undefined, ids: number[]) => {
    if (!appId || !ids.length) return;
    setFlow({ mode, appId, ids });
  };

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
            text="确认是刷量或作弊账号时使用。连同关系图找到的关联账号一起删除，可同时拉黑它们用过的设备。"
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
          <Input
            value={manual}
            onChange={(e) => setManual(e.target.value)}
            placeholder="例如 3660 3661"
            className="mt-3"
            disabled={!app}
          />
          <div className="mt-3 flex flex-wrap items-center justify-end gap-2">
            <Button size="sm" variant="outline" disabled={!app || !manualIds.length} onClick={() => start("release", app, manualIds)}>
              <ShieldOff className="size-3.5" />
              解除风控
            </Button>
            <Button size="sm" variant="outline" className="text-rose-600 hover:text-rose-700 dark:text-rose-400" disabled={!app || !manualIds.length} onClick={() => start("purge", app, manualIds)}>
              <UserX className="size-3.5" />
              删除账号
            </Button>
          </div>
        </div>
      </div>

      <section className="overflow-hidden rounded-xl border bg-card">
        <div className="flex flex-col gap-3 border-b px-4 py-3 sm:flex-row sm:items-center sm:justify-between">
          <div className="flex items-center gap-3">
            <TonalIcon icon={Users} tone="rose" />
            <div>
              <h3 className="text-sm font-medium">风控封禁中的账号</h3>
              <p className="text-xs text-muted-foreground">
                {banned.isLoading ? "正在读取" : total ? `共 ${total} 个账号被风控平台封禁` : "目前没有被风控平台封禁的账号"}
              </p>
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
              placeholder="搜索登录名、昵称或用户 ID"
              className="h-8 pl-8 text-sm"
            />
          </div>
        </div>

        {selected.size ? (
          <div className="flex flex-col gap-2 border-b bg-primary/[0.04] px-4 py-2.5 sm:flex-row sm:items-center sm:justify-between">
            <p className="text-sm">
              已选 <span className="font-medium tabular-nums">{selected.size}</span> 个账号
              {selectedApps.size > 1 ? <span className="ml-2 text-xs text-amber-600 dark:text-amber-400">分属 {selectedApps.size} 个应用，请只选同一应用的账号</span> : null}
            </p>
            <div className="flex items-center gap-2">
              <Button size="sm" variant="ghost" onClick={() => setSelected(new Map())}>取消选择</Button>
              <Button size="sm" variant="outline" disabled={!oneApp || !canOperate} onClick={() => start("release", oneApp, [...selected.keys()])}>
                <ShieldOff className="size-3.5" />
                解除风控
              </Button>
              <Button size="sm" variant="destructive" disabled={!oneApp || !canOperate} onClick={() => start("purge", oneApp, [...selected.keys()])}>
                <UserX className="size-3.5" />
                删除账号
              </Button>
            </div>
          </div>
        ) : null}

        {banned.isLoading ? (
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
        ) : banned.isError ? (
          <CardEmpty icon={ShieldOff} title="读不到风控封禁的账号" description={(banned.error as Error)?.message} />
        ) : !items.length ? (
          <CardEmpty icon={ShieldCheck} title={q ? "没有匹配的账号" : "没有被风控封禁的账号"} description={q ? "换个关键词试试" : "风控平台封禁账号后会出现在这里"} />
        ) : (
          <>
            <div className="flex items-center gap-3 border-b px-4 py-2 text-xs text-muted-foreground">
              <Checkbox checked={allOnPage} onCheckedChange={(v) => togglePage(v === true)} aria-label="选择本页全部" />
              <span>本页 {items.length} 个</span>
            </div>
            <ul className="divide-y">
              {items.map((u) => (
                <BannedRow
                  key={u.id}
                  user={u}
                  checked={selected.has(u.id)}
                  onToggle={(v) => toggle(u, v)}
                  showApp={!app}
                  href={appKey.get(u.appid) ? `/app-users/${appKey.get(u.appid)}/${u.id}?tab=risk` : undefined}
                  canOperate={canOperate}
                  onRelease={() => start("release", u.appid, [u.id])}
                  onPurge={() => start("purge", u.appid, [u.id])}
                />
              ))}
            </ul>
            <div className="border-t px-4 py-2">
              <ListPagination page={page} totalPages={Math.max(1, Math.ceil(total / LIMIT))} total={total} unit="个账号" onPage={setPage} />
            </div>
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
          if (flow?.ids === manualIds) setManual("");
        }}
      />
    </div>
  );
}

function ExplainCard({ icon, tone, title, text }: { icon: typeof ShieldCheck; tone: "emerald" | "rose"; title: string; text: string }) {
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

function BannedRow({
  user, checked, onToggle, showApp, href, canOperate, onRelease, onPurge,
}: {
  user: OpsUser;
  checked: boolean;
  onToggle: (v: boolean) => void;
  showApp: boolean;
  href?: string;
  canOperate: boolean;
  onRelease: () => void;
  onPurge: () => void;
}) {
  const ban = user.ban;
  return (
    <li className={cn("group flex items-start gap-3 px-4 py-3 transition-colors hover:bg-muted/30", checked && "bg-primary/[0.03]")}>
      <Checkbox checked={checked} onCheckedChange={(v) => onToggle(v === true)} className="mt-2.5" aria-label={`选择 ${accountName(user, user.id)}`} />
      <AccountAvatar user={user} id={user.id} />
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-x-2 gap-y-0.5">
          <span className="truncate text-sm font-medium">{accountName(user, user.id)}</span>
          {showApp && user.appName ? <span className="text-xs text-muted-foreground">{user.appName}</span> : null}
        </div>
        <p className="truncate text-xs text-muted-foreground">{accountMeta(user, user.id)}</p>
        {ban ? (
          <p className="mt-1 text-xs leading-5 text-foreground/80" title={formatDateTime(ban.startAt)}>
            {formatRelative(ban.startAt)}被风控封禁，{ban.endAt ? `到 ${formatDateTime(ban.endAt)} 解封` : "永久"}
            {ban.reason ? `，原因：${ban.reason}` : ""}
          </p>
        ) : null}
      </div>
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
    </li>
  );
}
