"use client";

import { useDeferredValue, useMemo, useState } from "react";
import Link from "next/link";
import { CalendarDays, ChevronLeft, ChevronRight, Coins, Flame, Globe2, Search, SearchX, Sparkles, X } from "lucide-react";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { useAdminAppSignInRecordsQuery } from "@/lib/admin-hooks";
import type { AppSignInRecordItem, SignInRewardPolicy } from "@/lib/api/types";
import { cn } from "@/lib/utils";
import { bonusLabel, buildBonusLabels, formatNumber, formatTime, sourceLabel } from "./signin-shared";

type RangeKey = "all" | "today" | "7d" | "30d" | "custom";

const RANGE_LABEL: Record<RangeKey, string> = {
  all: "全部时间",
  today: "今天",
  "7d": "近 7 天",
  "30d": "近 30 天",
  custom: "自定义"
};

function isoDate(date: Date) {
  const y = date.getFullYear();
  const m = String(date.getMonth() + 1).padStart(2, "0");
  const d = String(date.getDate()).padStart(2, "0");
  return `${y}-${m}-${d}`;
}

function rangeDates(range: RangeKey, from: string, to: string) {
  const today = new Date();
  if (range === "today") return { dateFrom: isoDate(today), dateTo: isoDate(today) };
  if (range === "7d") return { dateFrom: isoDate(new Date(today.getTime() - 6 * 86_400_000)), dateTo: isoDate(today) };
  if (range === "30d") return { dateFrom: isoDate(new Date(today.getTime() - 29 * 86_400_000)), dateTo: isoDate(today) };
  if (range === "custom") return { dateFrom: from || undefined, dateTo: to || undefined };
  return { dateFrom: undefined, dateTo: undefined };
}

export function SignInRecords({ appKey, policy }: { appKey: string; policy: SignInRewardPolicy }) {
  const [keyword, setKeyword] = useState("");
  const deferredKeyword = useDeferredValue(keyword.trim());
  const [range, setRange] = useState<RangeKey>("all");
  const [from, setFrom] = useState("");
  const [to, setTo] = useState("");
  const [source, setSource] = useState("all");
  const [bonusType, setBonusType] = useState("all");
  const [page, setPage] = useState(1);
  const [limit, setLimit] = useState(20);

  const labels = useMemo(() => buildBonusLabels(policy), [policy]);
  const bonusOptions = useMemo(() => {
    const keys = new Set<string>(["normal", "compound", "milestone"]);
    for (const rule of policy.rules) if (rule.bonusType) keys.add(rule.bonusType);
    for (const m of policy.milestones) if (m.bonusType) keys.add(m.bonusType);
    return [...keys];
  }, [policy]);

  const dates = rangeDates(range, from, to);
  const query = useAdminAppSignInRecordsQuery(appKey, {
    keyword: deferredKeyword || undefined,
    source: source === "all" ? undefined : source,
    bonusType: bonusType === "all" ? undefined : bonusType,
    ...dates,
    page,
    limit
  });
  const items = query.data?.items ?? [];
  const total = query.data?.total ?? 0;
  const totalPages = Math.max(1, query.data?.totalPages ?? 1);
  const filtered = Boolean(deferredKeyword) || range !== "all" || source !== "all" || bonusType !== "all";

  // 任一筛选变化都回到第一页
  const reset = <T,>(setter: (value: T) => void) => (value: T) => {
    setter(value);
    setPage(1);
  };

  function clearFilters() {
    setKeyword("");
    setRange("all");
    setFrom("");
    setTo("");
    setSource("all");
    setBonusType("all");
    setPage(1);
  }

  return (
    <div className="space-y-3">
      {/* 筛选 */}
      <div className="space-y-2 rounded-2xl border bg-card p-3">
        <div className="flex flex-col gap-2 lg:flex-row lg:items-center">
          <div className="relative min-w-0 flex-1">
            <Search className="absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-muted-foreground" />
            <Input
              value={keyword}
              onChange={(e) => reset(setKeyword)(e.target.value)}
              placeholder="搜索账号、昵称、邮箱、手机号、IP 或用户 ID"
              className="h-9 pr-8 pl-8 text-sm"
            />
            {keyword ? (
              <button type="button" aria-label="清除搜索" className="absolute top-1/2 right-2 -translate-y-1/2 text-muted-foreground hover:text-foreground" onClick={() => reset(setKeyword)("")}>
                <X className="size-3.5" />
              </button>
            ) : null}
          </div>
          <div className="grid grid-cols-2 gap-2 sm:grid-cols-3 lg:flex">
            <Select value={range} onValueChange={(v) => reset(setRange)(v as RangeKey)}>
              <SelectTrigger className="h-9 w-full text-sm lg:w-32"><CalendarDays className="size-3.5 text-muted-foreground" /><SelectValue /></SelectTrigger>
              <SelectContent>
                {(Object.keys(RANGE_LABEL) as RangeKey[]).map((key) => <SelectItem key={key} value={key}>{RANGE_LABEL[key]}</SelectItem>)}
              </SelectContent>
            </Select>
            <Select value={source} onValueChange={reset(setSource)}>
              <SelectTrigger className="h-9 w-full text-sm lg:w-32"><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="all">全部来源</SelectItem>
                <SelectItem value="manual">手动签到</SelectItem>
                <SelectItem value="auto">自动签到</SelectItem>
              </SelectContent>
            </Select>
            <Select value={bonusType} onValueChange={reset(setBonusType)}>
              <SelectTrigger className="col-span-2 h-9 w-full text-sm sm:col-span-1 lg:w-40"><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="all">全部奖励类型</SelectItem>
                {bonusOptions.map((key) => <SelectItem key={key} value={key}>{bonusLabel(labels, key)}</SelectItem>)}
              </SelectContent>
            </Select>
          </div>
        </div>
        {range === "custom" ? (
          <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
            <Input type="date" value={from} max={to || undefined} onChange={(e) => reset(setFrom)(e.target.value)} className="h-8 w-40 text-sm" aria-label="开始日期" />
            <span>至</span>
            <Input type="date" value={to} min={from || undefined} onChange={(e) => reset(setTo)(e.target.value)} className="h-8 w-40 text-sm" aria-label="结束日期" />
          </div>
        ) : null}
        <div className="flex items-center justify-between gap-2 text-xs text-muted-foreground">
          <span className="tabular-nums">{query.isLoading ? "" : `共 ${formatNumber(total)} 条签到记录`}</span>
          {filtered ? <button type="button" className="hover:text-foreground" onClick={clearFilters}>清除筛选</button> : null}
        </div>
      </div>

      {query.isLoading ? (
        <RecordsSkeleton />
      ) : items.length === 0 ? (
        <div className="flex min-h-48 flex-col items-center justify-center gap-2 rounded-2xl border border-dashed text-center">
          <SearchX className="size-5 text-muted-foreground" />
          <p className="text-sm font-medium">{filtered ? "没有符合条件的签到记录" : "暂无签到记录"}</p>
          {filtered ? <Button size="sm" variant="outline" className="h-8" onClick={clearFilters}>清除筛选</Button> : null}
        </div>
      ) : (
        <div className={cn("transition-opacity", query.isFetching && "opacity-60")}>
          {/* 桌面表格 */}
          <div className="hidden overflow-hidden rounded-2xl border bg-card md:block">
            <Table>
              <TableHeader>
                <TableRow className="bg-muted/30 hover:bg-muted/30">
                  <TableHead className="h-10 pl-4 text-xs">用户</TableHead>
                  <TableHead className="h-10 text-xs">签到时间</TableHead>
                  <TableHead className="h-10 text-right text-xs">积分</TableHead>
                  <TableHead className="h-10 text-right text-xs">经验</TableHead>
                  <TableHead className="h-10 text-right text-xs">连签</TableHead>
                  <TableHead className="h-10 text-xs">奖励类型</TableHead>
                  <TableHead className="hidden h-10 text-xs xl:table-cell">来源与网络</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {items.map((item) => (
                  <TableRow key={item.id}>
                    <TableCell className="py-2.5 pl-4"><RecordUser appKey={appKey} item={item} /></TableCell>
                    <TableCell className="py-2.5">
                      <div className="text-sm tabular-nums">{item.signDate}</div>
                      <div className="text-[11px] text-muted-foreground tabular-nums">{formatTime(item.signedAt)}</div>
                    </TableCell>
                    <TableCell className="py-2.5 text-right">
                      <div className="text-sm font-medium text-amber-700 tabular-nums dark:text-amber-300">+{formatNumber(item.integralReward)}</div>
                      {item.rewardMultiplier !== 1 ? <div className="text-[11px] text-muted-foreground tabular-nums">×{item.rewardMultiplier}</div> : null}
                    </TableCell>
                    <TableCell className="py-2.5 text-right text-sm font-medium text-sky-700 tabular-nums dark:text-sky-300">+{formatNumber(item.experienceReward)}</TableCell>
                    <TableCell className="py-2.5 text-right"><StreakChip days={item.consecutiveDays} /></TableCell>
                    <TableCell className="max-w-64 py-2.5">
                      <Badge variant={item.bonusType && item.bonusType !== "normal" ? "info" : "outline"} size="sm">{bonusLabel(labels, item.bonusType)}</Badge>
                      {item.bonusDescription ? <div className="mt-0.5 line-clamp-2 text-[11px] text-muted-foreground">{item.bonusDescription}</div> : null}
                    </TableCell>
                    <TableCell className="hidden max-w-56 py-2.5 xl:table-cell">
                      <div className="text-xs">{sourceLabel(item.signInSource)}{item.deviceInfo ? <span className="text-muted-foreground">，{item.deviceInfo}</span> : null}</div>
                      <div className="truncate text-[11px] text-muted-foreground" title={item.location}>
                        <span className="font-mono">{item.ipAddress || "未知 IP"}</span>
                        {item.location ? ` ${item.location}` : ""}
                      </div>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>

          {/* 移动端卡片 */}
          <ul className="space-y-2.5 md:hidden">
            {items.map((item) => (
              <li key={item.id} className="rounded-2xl border bg-card p-3.5">
                <div className="flex items-start justify-between gap-3">
                  <RecordUser appKey={appKey} item={item} />
                  <StreakChip days={item.consecutiveDays} />
                </div>
                <div className="mt-3 grid grid-cols-3 gap-2 rounded-xl bg-muted/40 p-2.5 text-center">
                  <div>
                    <div className="flex items-center justify-center gap-1 text-[11px] text-muted-foreground"><Coins className="size-3" />积分</div>
                    <div className="text-sm font-semibold text-amber-700 tabular-nums dark:text-amber-300">+{formatNumber(item.integralReward)}</div>
                  </div>
                  <div>
                    <div className="flex items-center justify-center gap-1 text-[11px] text-muted-foreground"><Sparkles className="size-3" />经验</div>
                    <div className="text-sm font-semibold text-sky-700 tabular-nums dark:text-sky-300">+{formatNumber(item.experienceReward)}</div>
                  </div>
                  <div>
                    <div className="text-[11px] text-muted-foreground">倍率</div>
                    <div className="text-sm font-semibold tabular-nums">×{item.rewardMultiplier}</div>
                  </div>
                </div>
                <div className="mt-2.5 flex flex-wrap items-center gap-x-3 gap-y-1 text-[11px] text-muted-foreground">
                  <Badge variant={item.bonusType && item.bonusType !== "normal" ? "info" : "outline"} size="sm">{bonusLabel(labels, item.bonusType)}</Badge>
                  <span className="tabular-nums">{item.signDate} {formatTime(item.signedAt)}</span>
                  <span>{sourceLabel(item.signInSource)}</span>
                  <span className="flex items-center gap-1 font-mono"><Globe2 className="size-3" />{item.ipAddress || "未知 IP"}</span>
                </div>
                {item.bonusDescription ? <p className="mt-1.5 text-[11px] leading-5 text-muted-foreground">{item.bonusDescription}</p> : null}
              </li>
            ))}
          </ul>
        </div>
      )}

      {!query.isLoading && total > 0 ? (
        <div className="flex flex-wrap items-center justify-between gap-2 text-xs text-muted-foreground">
          <div className="flex items-center gap-2">
            <span>每页</span>
            <Select value={String(limit)} onValueChange={(v) => reset(setLimit)(Number(v))}>
              <SelectTrigger size="sm" className="h-8 w-20 text-xs"><SelectValue /></SelectTrigger>
              <SelectContent>
                {[20, 50, 100].map((n) => <SelectItem key={n} value={String(n)}>{n} 条</SelectItem>)}
              </SelectContent>
            </Select>
          </div>
          <div className="flex items-center gap-1.5">
            <Button variant="outline" size="icon" className="size-8" aria-label="上一页" disabled={page <= 1 || query.isFetching} onClick={() => setPage(page - 1)}>
              <ChevronLeft className="size-4" />
            </Button>
            <span className="min-w-16 text-center tabular-nums">{page} / {totalPages}</span>
            <Button variant="outline" size="icon" className="size-8" aria-label="下一页" disabled={page >= totalPages || query.isFetching} onClick={() => setPage(page + 1)}>
              <ChevronRight className="size-4" />
            </Button>
          </div>
        </div>
      ) : null}
    </div>
  );
}

function RecordUser({ appKey, item }: { appKey: string; item: AppSignInRecordItem }) {
  const name = item.nickname || item.account;
  return (
    <Link href={`/app-users/${encodeURIComponent(appKey)}/${item.userId}`} className="group flex min-w-0 items-center gap-2.5">
      <Avatar className="size-8 rounded-lg border">
        <AvatarImage src={item.avatar} alt={name} />
        <AvatarFallback className="rounded-lg text-[10px]">{name.slice(0, 2)}</AvatarFallback>
      </Avatar>
      <span className="min-w-0">
        <span className="block truncate text-sm font-medium group-hover:underline">{name}</span>
        <span className="block truncate text-[11px] text-muted-foreground">
          {item.nickname ? <span className="font-mono">{item.account}</span> : null}
          {item.nickname ? " " : ""}ID {item.userId}
        </span>
      </span>
    </Link>
  );
}

function StreakChip({ days }: { days: number }) {
  const hot = days >= 7;
  return (
    <span
      className={cn(
        "inline-flex shrink-0 items-center gap-1 rounded-full px-2 py-0.5 text-[11px] font-medium tabular-nums",
        hot ? "bg-orange-500/10 text-orange-700 dark:text-orange-300" : "bg-muted text-muted-foreground"
      )}
    >
      {hot ? <Flame className="size-3" /> : null}
      {formatNumber(days)} 天
    </span>
  );
}

function RecordsSkeleton() {
  return (
    <>
      <div className="hidden overflow-hidden rounded-2xl border bg-card md:block">
        <div className="h-10 border-b bg-muted/30" />
        {Array.from({ length: 8 }).map((_, i) => (
          <div key={i} className="flex items-center gap-6 border-b px-4 py-3 last:border-b-0">
            <div className="flex w-48 items-center gap-2.5">
              <Skeleton className="size-8 rounded-lg" />
              <div className="space-y-1.5"><Skeleton className="h-3.5 w-20" /><Skeleton className="h-3 w-14" /></div>
            </div>
            <Skeleton className="h-8 w-20" />
            <Skeleton className="ml-auto h-4 w-12" />
            <Skeleton className="h-4 w-12" />
            <Skeleton className="h-5 w-14 rounded-full" />
            <Skeleton className="h-5 w-20 rounded-full" />
          </div>
        ))}
      </div>
      <div className="space-y-2.5 md:hidden">
        {Array.from({ length: 4 }).map((_, i) => <Skeleton key={i} className="h-36 w-full rounded-2xl" />)}
      </div>
    </>
  );
}
