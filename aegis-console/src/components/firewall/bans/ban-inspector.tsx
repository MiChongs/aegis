"use client";

import { useState, type ReactNode } from "react";
import { Check, Copy, Globe2, History, Loader2, MapPinned, Pencil, Search, ShieldBan, ShieldCheck, ShieldX, Unlock } from "lucide-react";
import { Button } from "@/components/ui/button";
import { CountryFlag } from "@/components/ui/country-flag";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { CardEmpty } from "@/components/overview/overview-kit";
import { FieldError } from "@/components/traffic/traffic-kit";
import type { IPInspection } from "@/lib/api/firewall-bans";
import type { IPBanEntry } from "@/lib/api/types";
import { formatDateTime, formatRelative } from "@/lib/datetime";
import { banErrorMessage, useInspectIPQuery } from "@/lib/firewall-ban-hooks";
import { cn } from "@/lib/utils";
import { banPlace, banTimeState, durationLabel, geoScopeLabel, isActiveBan, normalizeIP, SourceBadge, STATUS_META, TIME_TONE, useBanModes, useNow } from "./ban-format";

/**
 * IP 查询：这个地址现在会不会被拦、被哪一层拦。顺序与入口段一致：IP 精确封禁 → 地域规则 → 地理围栏，
 * 先命中的生效，后面的层不再判定。查询本身不累加地域规则的命中计数。
 */

export type InspectSeed = { key: number; ip: string };

export const LAYERS = [
  { key: "ip_ban", label: "IP 精确封禁", hint: "手动、自动、风控或规则封禁的单个地址", icon: ShieldBan },
  { key: "geo_ban", label: "地域规则", hint: "国家、省份、城市、ASN、运营商", icon: Globe2 },
  { key: "geo_fence", label: "地理围栏", hint: "按坐标范围", icon: MapPinned }
] as const;

export function InspectDialog({
  seed,
  onClose,
  canBan,
  onUnban,
  onEdit,
  onBan,
  onShowGeo
}: {
  seed: InspectSeed | null;
  onClose: () => void;
  canBan: boolean;
  onUnban: (ban: IPBanEntry) => void;
  onEdit: (ban: IPBanEntry) => void;
  onBan: (ip: string) => void;
  onShowGeo: () => void;
}) {
  return (
    <Dialog open={seed !== null} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="flex max-h-[min(880px,calc(100dvh-2rem))] flex-col gap-0 overflow-hidden p-0 sm:max-w-2xl">
        <DialogHeader className="border-b px-5 py-4 pr-12">
          <DialogTitle>IP 查询</DialogTitle>
          <DialogDescription>这个地址现在会不会被拦截、被哪一层拦截。</DialogDescription>
        </DialogHeader>
        {seed ? <InspectBody key={seed.key} initial={seed.ip} canBan={canBan} onUnban={onUnban} onEdit={onEdit} onBan={onBan} onShowGeo={onShowGeo} /> : null}
      </DialogContent>
    </Dialog>
  );
}

function InspectBody({
  initial,
  canBan,
  onUnban,
  onEdit,
  onBan,
  onShowGeo
}: {
  initial: string;
  canBan: boolean;
  onUnban: (ban: IPBanEntry) => void;
  onEdit: (ban: IPBanEntry) => void;
  onBan: (ip: string) => void;
  onShowGeo: () => void;
}) {
  const [input, setInput] = useState(initial);
  const [ip, setIp] = useState<string | null>(() => normalizeIP(initial));
  const [inputError, setInputError] = useState<string | null>(initial && !normalizeIP(initial) ? "不是合法的 IP 地址" : null);
  const query = useInspectIPQuery(ip);

  const submit = () => {
    const next = normalizeIP(input);
    if (!next) {
      setInputError(input.trim() ? "不是合法的 IP 地址" : "请输入 IP 地址");
      return;
    }
    setInputError(null);
    if (next === ip) void query.refetch();
    else setIp(next);
  };

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
      <form
        className="flex items-start gap-2 border-b px-5 py-3"
        onSubmit={(event) => {
          event.preventDefault();
          submit();
        }}
      >
        <div className="min-w-0 flex-1">
          <div className="relative">
            <Search className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
            <Input
              value={input}
              onChange={(event) => setInput(event.target.value)}
              placeholder="IPv4 或 IPv6 地址"
              spellCheck={false}
              className="pl-8 font-mono text-sm"
              aria-label="IP 地址"
              aria-invalid={Boolean(inputError) || undefined}
            />
          </div>
          <FieldError message={inputError} className="mt-1" />
        </div>
        <Button type="submit" disabled={query.isFetching}>
          {query.isFetching ? <Loader2 className="animate-spin" /> : null}
          查询
        </Button>
      </form>
      <div className="min-h-0 flex-1 overflow-y-auto px-5 py-4">
        {!ip ? (
          <CardEmpty icon={Search} title="输入一个 IP 地址" description="查看它是否会被拦截、命中哪一层，以及它的封禁历史。" />
        ) : query.isError ? (
          <CardEmpty icon={ShieldX} title={banErrorMessage(query.error, "查询失败")} />
        ) : !query.data ? (
          <div className="flex flex-col gap-3">
            <Skeleton className="h-24 rounded-xl" />
            <Skeleton className="h-14 rounded-xl" />
            <Skeleton className="h-32 rounded-xl" />
          </div>
        ) : (
          <InspectionView data={query.data} canBan={canBan} onUnban={onUnban} onEdit={onEdit} onBan={onBan} onShowGeo={onShowGeo} />
        )}
      </div>
    </div>
  );
}

function InspectionView({
  data,
  canBan,
  onUnban,
  onEdit,
  onBan,
  onShowGeo
}: {
  data: IPInspection;
  canBan: boolean;
  onUnban: (ban: IPBanEntry) => void;
  onEdit: (ban: IPBanEntry) => void;
  onBan: (ip: string) => void;
  onShowGeo: () => void;
}) {
  const now = useNow();
  const modes = useBanModes();
  const [copied, setCopied] = useState(false);
  const layer = LAYERS.find((item) => item.key === data.layer);
  const hitIndex = data.blocked ? LAYERS.findIndex((item) => item.key === data.layer) : -1;
  const place = banPlace({ country: data.country, countryCode: data.countryCode, region: data.region, city: data.city });
  const active = data.activeBan && isActiveBan(data.activeBan, now) ? data.activeBan : null;

  const copy = () => {
    void navigator.clipboard.writeText(data.ip).then(() => {
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1500);
    });
  };

  return (
    <div className="flex min-w-0 flex-col gap-4">
      {/* 结论 */}
      <section
        className={cn(
          "flex min-w-0 flex-col gap-3 rounded-xl border p-4",
          data.blocked ? "border-rose-500/30 bg-rose-500/5" : "border-emerald-500/30 bg-emerald-500/5"
        )}
      >
        <div className="flex min-w-0 items-start gap-3">
          <span
            className={cn(
              "grid size-10 shrink-0 place-items-center rounded-xl",
              data.blocked ? "bg-rose-500/12 text-rose-600 dark:text-rose-400" : "bg-emerald-500/12 text-emerald-600 dark:text-emerald-400"
            )}
          >
            {data.blocked ? <ShieldX className="size-5" /> : <ShieldCheck className="size-5" />}
          </span>
          <div className="min-w-0 flex-1">
            <div className={cn("text-base leading-6 font-semibold", data.blocked ? "text-rose-700 dark:text-rose-300" : "text-emerald-700 dark:text-emerald-300")}>
              {data.blocked ? "会被拦截" : "不会被拦截"}
            </div>
            <div className="flex min-w-0 items-center gap-1">
              <span className="min-w-0 truncate font-mono text-sm" title={data.ip}>
                {data.ip}
              </span>
              <Button size="icon-xs" variant="ghost" aria-label="复制 IP" onClick={copy}>
                {copied ? <Check /> : <Copy />}
              </Button>
            </div>
          </div>
          {canBan && !active && data.layer !== "ip_ban" ? (
            <Button size="sm" variant="outline" onClick={() => onBan(data.ip)}>
              <ShieldBan />
              封禁
            </Button>
          ) : null}
        </div>
        {data.blocked ? (
          <dl className="grid min-w-0 gap-x-4 gap-y-1.5 text-xs sm:grid-cols-[auto_minmax(0,1fr)]">
            <dt className="text-muted-foreground">拦截层</dt>
            <dd className="min-w-0 font-medium">{layer?.label ?? data.layer}</dd>
            <dt className="text-muted-foreground">响应模式</dt>
            <dd className="min-w-0">
              {modes.label(data.decision.mode)}
              {data.decision.mode === "tarpit" && data.decision.delayMs ? <span className="text-muted-foreground"> · 拖延 {(data.decision.delayMs / 1000).toFixed(1)} 秒</span> : null}
            </dd>
            <dt className="text-muted-foreground">原因</dt>
            <dd className="min-w-0 break-words">{data.decision.reason || "—"}</dd>
          </dl>
        ) : (
          <p className="text-xs text-muted-foreground">三层都没有命中。白名单、黑名单网段、限流与 WAF 仍会照常检查这个地址的请求。</p>
        )}
      </section>

      {/* 判定路径 */}
      <ol className="grid min-w-0 grid-cols-3 gap-1.5" aria-label="判定路径">
        {LAYERS.map((item, index) => {
          const state = hitIndex === -1 ? "pass" : index < hitIndex ? "pass" : index === hitIndex ? "hit" : "skip";
          const Icon = item.icon;
          return (
            <li
              key={item.key}
              title={item.hint}
              className={cn(
                "flex min-w-0 flex-col gap-1 rounded-lg border px-2.5 py-2",
                state === "hit" && "border-rose-500/40 bg-rose-500/5",
                state === "skip" && "border-dashed opacity-60"
              )}
            >
              <span className="flex min-w-0 items-center gap-1.5 text-xs font-medium">
                <Icon className={cn("size-3.5 shrink-0", state === "hit" ? "text-rose-500" : "text-muted-foreground")} />
                <span className="truncate">{item.label}</span>
              </span>
              <span
                className={cn(
                  "text-[11px]",
                  state === "hit" ? "font-medium text-rose-700 dark:text-rose-300" : state === "pass" ? "text-emerald-700 dark:text-emerald-400" : "text-muted-foreground"
                )}
              >
                {state === "hit" ? "命中" : state === "pass" ? "未命中" : "不再判定"}
              </span>
            </li>
          );
        })}
      </ol>

      {/* 归属与近期拦截 */}
      <dl className="grid min-w-0 grid-cols-2 gap-x-4 gap-y-3 rounded-xl border px-4 py-3 sm:grid-cols-5">
        <Fact label="归属地" className="col-span-2">
          <span className="flex min-w-0 items-center gap-1.5">
            {data.countryCode ? <CountryFlag code={data.countryCode} size={14} /> : null}
            <span className="truncate">{place || "未知"}</span>
          </span>
        </Fact>
        <Fact label="运营商">
          <span className="block truncate" title={data.isp}>
            {data.isp || "—"}
          </span>
        </Fact>
        <Fact label="ASN">
          <span className="font-mono">{data.asn || "—"}</span>
        </Fact>
        <Fact label="近 24 小时 / 7 天拦截">
          <span className="font-data tabular-nums">
            {data.blocks24h.toLocaleString("zh-CN")} <span className="text-muted-foreground">/</span> {data.blocks7d.toLocaleString("zh-CN")}
          </span>
        </Fact>
      </dl>

      {/* 当前封禁 */}
      {active ? (
        <section className="flex min-w-0 flex-col gap-2.5 rounded-xl border px-4 py-3">
          <div className="flex min-w-0 flex-wrap items-center gap-2">
            <h4 className="text-sm font-medium">当前封禁</h4>
            <SourceBadge source={active.source} />
            <span className={cn("text-xs tabular-nums", TIME_TONE[banTimeState(active, now).tone])}>{banTimeState(active, now).text}</span>
            {canBan ? (
              <div className="ml-auto flex items-center gap-1.5">
                <Button size="sm" variant="outline" onClick={() => onEdit(active)}>
                  <Pencil />
                  修改
                </Button>
                <Button size="sm" onClick={() => onUnban(active)}>
                  <Unlock />
                  解封
                </Button>
              </div>
            ) : null}
          </div>
          <dl className="grid min-w-0 gap-x-4 gap-y-1 text-xs sm:grid-cols-[auto_minmax(0,1fr)]">
            <dt className="text-muted-foreground">原因</dt>
            <dd className="min-w-0 break-words">{active.reason || "—"}</dd>
            {active.triggerRule ? (
              <>
                <dt className="text-muted-foreground">触发规则</dt>
                <dd className="min-w-0 font-mono break-all">
                  {active.triggerRule}
                  {active.triggerCount ? <span className="ml-1 font-sans text-muted-foreground">· 触发 {active.triggerCount} 次</span> : null}
                </dd>
              </>
            ) : null}
            <dt className="text-muted-foreground">封禁于</dt>
            <dd className="min-w-0">
              {formatDateTime(active.createdAt)} <span className="text-muted-foreground">· 时长 {durationLabel(active.duration)}</span>
            </dd>
          </dl>
        </section>
      ) : null}

      {/* 命中的地域规则 */}
      {data.layer === "geo_ban" && data.geoRule ? (
        <section className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-2 rounded-xl border px-4 py-3">
          <div className="min-w-0 flex-1">
            <div className="text-sm font-medium">
              命中地域规则 · {geoScopeLabel(data.geoRule.scopeType)} <span className="font-mono">{data.geoRule.scopeValue}</span>
            </div>
            <div className="truncate text-xs text-muted-foreground">
              {data.geoRule.reason || "无原因"} · 累计命中 {data.geoRule.matchCount.toLocaleString("zh-CN")} 次
            </div>
          </div>
          <Button size="sm" variant="outline" onClick={onShowGeo}>
            查看地域规则
          </Button>
        </section>
      ) : null}

      {/* 历史 */}
      <section className="flex min-w-0 flex-col gap-2">
        <h4 className="flex items-center gap-1.5 text-sm font-medium">
          <History className="size-4 text-muted-foreground" />
          封禁历史
          <span className="text-xs font-normal text-muted-foreground tabular-nums">{data.history.length}</span>
        </h4>
        {data.history.length === 0 ? (
          <p className="rounded-xl border border-dashed px-4 py-5 text-center text-xs text-muted-foreground">这个地址从未被封禁过</p>
        ) : (
          <ol className="relative ml-1.5 flex flex-col gap-3 border-l pl-4">
            {data.history.map((ban) => (
              <HistoryItem key={ban.id} ban={ban} now={now} />
            ))}
          </ol>
        )}
      </section>
    </div>
  );
}

function HistoryItem({ ban, now }: { ban: IPBanEntry; now: number }) {
  const effective = isActiveBan(ban, now) ? "active" : ban.status === "revoked" ? "revoked" : "expired";
  const meta = STATUS_META[effective]!;
  return (
    <li className="relative flex min-w-0 flex-col gap-1">
      <span aria-hidden className={cn("absolute top-1.5 -left-[21px] size-2.5 rounded-full ring-4 ring-background", meta.dot)} />
      <div className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1 text-xs">
        <span className={cn("font-medium", meta.text)}>{meta.label}</span>
        <SourceBadge source={ban.source} />
        <span className="text-muted-foreground tabular-nums" title={formatDateTime(ban.createdAt)}>
          {formatRelative(ban.createdAt, "—", now)}
        </span>
        <span className="text-muted-foreground">· {durationLabel(ban.duration)}</span>
      </div>
      <p className="min-w-0 text-xs break-words">{ban.reason || <span className="text-muted-foreground">无原因</span>}</p>
      {ban.triggerRule ? <p className="min-w-0 truncate font-mono text-[11px] text-muted-foreground">{ban.triggerRule}</p> : null}
      {ban.status === "revoked" ? (
        <p className="min-w-0 text-[11px] text-muted-foreground">
          {ban.revokedAt ? `解除于 ${formatDateTime(ban.revokedAt)}` : "已解除"}
          {ban.revokeNote ? `：${ban.revokeNote}` : ""}
        </p>
      ) : null}
    </li>
  );
}

function Fact({ label, children, className }: { label: string; children: ReactNode; className?: string }) {
  return (
    <div className={cn("min-w-0", className)}>
      <dt className="truncate text-[11px] text-muted-foreground">{label}</dt>
      <dd className="mt-0.5 min-w-0 text-xs font-medium">{children}</dd>
    </div>
  );
}
