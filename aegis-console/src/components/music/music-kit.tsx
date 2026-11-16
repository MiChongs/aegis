"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { ArrowDownRight, ArrowUpRight, MicVocal, Music2, RefreshCw, ServerOff, type LucideIcon } from "lucide-react";
import { Panel, Segmented } from "@/components/traffic/traffic-kit";
import { ErrorState } from "@/components/risk/decisions";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { ApiError } from "@/lib/api/client";
import { MUSIC_SERVICE_CODES, type MusicTrack } from "@/lib/api/music";
import type { MusicUserName } from "@/lib/music-hooks";
import { cn } from "@/lib/utils";
import { SCENES, sceneLabel, sourceLabel } from "./music-format";

/**
 * 音乐推荐页面共用的展示件：服务状态提示、歌曲与用户单元格、分页条、周期切换、环比。
 *
 * 面板外壳、数值块、分段切换沿用流量管理的 traffic-kit（与风控平台同一套边距），
 * 这里只放音乐域独有的部件，避免每个页签各写一份「歌名 + 歌手 + 封面」。
 */

/* ── 路由 ── */

export const musicHref = (appKey: string, tab?: string) =>
  `/apps/${encodeURIComponent(appKey)}/music${tab && tab !== "overview" ? `?tab=${tab}` : ""}`;
export const musicUserHref = (appKey: string, userId: number) => `/apps/${encodeURIComponent(appKey)}/music/users/${userId}`;
export const musicTrackHref = (appKey: string, workKey: string) =>
  `/apps/${encodeURIComponent(appKey)}/music/tracks/${encodeURIComponent(workKey)}`;
export const appUserHref = (appKey: string, userId: number) => `/app-users/${encodeURIComponent(appKey)}/${userId}`;
export const musicArtistHref = (appKey: string, name: string) =>
  `/apps/${encodeURIComponent(appKey)}/music/artists/${encodeURIComponent(name)}`;

/* ── 服务状态 ── */

export function musicServiceCode(error: unknown): number | undefined {
  if (!(error instanceof ApiError)) return undefined;
  return Object.values<number>(MUSIC_SERVICE_CODES).includes(error.code ?? 0) ? error.code : undefined;
}

/** 50396 / 50397 的说明：不是红屏，是一行真实状态 + 下一步 */
export function MusicServiceNotice({ code, onRetry, className }: { code: number; onRetry?: () => void; className?: string }) {
  const notEnabled = code === MUSIC_SERVICE_CODES.notEnabled;
  return (
    <div className={cn("flex flex-col items-center gap-3 px-4 py-10 text-center", className)}>
      <span className="grid size-10 place-items-center rounded-full bg-muted text-muted-foreground">
        <ServerOff className="size-5" />
      </span>
      <div className="flex flex-col gap-1">
        <p className="text-sm font-medium">{notEnabled ? "音乐推荐服务未开通" : "音乐推荐服务暂不可用"}</p>
        <p className="max-w-md text-xs leading-5 text-muted-foreground">
          {notEnabled
            ? "主服务未配置 MUSIC_RPC_URL，客户端已隐藏推荐入口。"
            : "行为数据仍在排队，服务恢复后自动补处理。"}
        </p>
      </div>
      {!notEnabled && onRetry ? (
        <Button size="sm" variant="outline" onClick={onRetry}>
          <RefreshCw />
          重试
        </Button>
      ) : null}
    </div>
  );
}

/** 查询失败：服务状态码给说明，其它错误给通用错误态 */
export function MusicError({ error, onRetry, className }: { error: unknown; onRetry?: () => void; className?: string }) {
  const code = musicServiceCode(error);
  if (code) return <MusicServiceNotice code={code} onRetry={onRetry} className={className} />;
  return <ErrorState error={error} onRetry={onRetry} className={className} />;
}

/** 整块面板形态的错误：标题照常、内容换成说明 */
export function MusicErrorPanel({ icon, title, error, onRetry }: { icon: LucideIcon; title: string; error: unknown; onRetry?: () => void }) {
  return (
    <Panel icon={icon} tone="muted" title={title}>
      <MusicError error={error} onRetry={onRetry} />
    </Panel>
  );
}

/* ── 周期 ── */

export type DaysOption = { value: string; label: string };

export const DAYS_SHORT: DaysOption[] = [
  { value: "7", label: "7 天" },
  { value: "14", label: "14 天" },
  { value: "30", label: "30 天" },
  { value: "90", label: "90 天" }
];

export const DAYS_LONG: DaysOption[] = [
  { value: "7", label: "7 天" },
  { value: "30", label: "30 天" },
  { value: "90", label: "90 天" },
  { value: "365", label: "一年" }
];

export function DaysSwitch({ value, onChange, options = DAYS_SHORT }: { value: number; onChange: (days: number) => void; options?: DaysOption[] }) {
  return <Segmented value={String(value)} options={options} onChange={(v) => onChange(Number(v))} ariaLabel="统计周期" />;
}

/* ── 歌曲与用户 ── */

export function TrackCover({ track, size = 36, className }: { track?: MusicTrack | null; size?: number; className?: string }) {
  return (
    <span
      className={cn("relative grid shrink-0 place-items-center overflow-hidden rounded-md bg-muted text-muted-foreground", className)}
      style={{ width: size, height: size }}
    >
      <Music2 className="size-4" />
      {track?.cover ? (
        // 封面来自各音源 CDN，域名不固定，不走 next/image
        // eslint-disable-next-line @next/next/no-img-element
        <img src={track.cover} alt="" loading="lazy" referrerPolicy="no-referrer" className="absolute inset-0 size-full object-cover" />
      ) : null}
    </span>
  );
}

/** 歌手头像：圆形，取不到图时显示话筒图标 */
export function ArtistCover({ cover, size = 36, className }: { cover?: string | null; size?: number; className?: string }) {
  return (
    <span
      className={cn("relative grid shrink-0 place-items-center overflow-hidden rounded-full bg-muted text-muted-foreground", className)}
      style={{ width: size, height: size }}
    >
      <MicVocal className="size-4" />
      {cover ? (
        // 头像来自各音源 CDN，域名不固定，不走 next/image
        // eslint-disable-next-line @next/next/no-img-element
        <img src={cover} alt="" loading="lazy" referrerPolicy="no-referrer" className="absolute inset-0 size-full object-cover" />
      ) : null}
    </span>
  );
}

/** 歌手名（+ 头像），可选跳歌手详情 */
export function ArtistCell({ name, cover, href, sub, className }: { name: string; cover?: string | null; href?: string; sub?: React.ReactNode; className?: string }) {
  const body = (
    <>
      <ArtistCover cover={cover} />
      <span className="flex min-w-0 flex-col">
        <span className={cn("truncate text-xs font-medium", href && "group-hover:underline")} title={name}>
          {name || "未知歌手"}
        </span>
        {sub ? <span className="truncate text-[11px] text-muted-foreground">{sub}</span> : null}
      </span>
    </>
  );
  const cls = cn("group flex min-w-0 items-center gap-2.5", className);
  return href ? (
    <Link href={href} className={cls} onClick={(e) => e.stopPropagation()}>
      {body}
    </Link>
  ) : (
    <span className={cls}>{body}</span>
  );
}

/** 歌曲列表：歌名 + 歌手，右侧主次两行数字；有作品键时可跳作品详情 */
export function TrackList({
  appKey,
  items,
  empty = "暂无"
}: {
  appKey: string;
  items: Array<{ key: string; track: MusicTrack | null; workKey?: string; right?: React.ReactNode; sub?: React.ReactNode }>;
  empty?: string;
}) {
  if (!items.length) return <EmptyLine>{empty}</EmptyLine>;
  return (
    <ul className="flex min-w-0 flex-col divide-y divide-border/60">
      {items.map((item) => (
        <li key={item.key} className="flex min-w-0 items-center gap-3 py-2 first:pt-0 last:pb-0">
          <TrackCell
            track={item.track}
            fallback={item.workKey ? `作品 ${item.workKey}` : "未知歌曲"}
            href={item.workKey ? musicTrackHref(appKey, item.workKey) : undefined}
            className="flex-1"
          />
          {item.right || item.sub ? (
            <span className="flex shrink-0 flex-col items-end text-[11px] leading-4 text-muted-foreground tabular-nums">
              {item.right ? <span className="text-xs text-foreground">{item.right}</span> : null}
              {item.sub}
            </span>
          ) : null}
        </li>
      ))}
    </ul>
  );
}

/** 歌名 + 歌手（+ 音源），可选跳作品详情 */
export function TrackCell({
  track,
  href,
  showSource,
  cover = true,
  fallback = "未知歌曲",
  className
}: {
  track?: MusicTrack | null;
  href?: string;
  showSource?: boolean;
  cover?: boolean;
  fallback?: string;
  className?: string;
}) {
  const title = track?.title || fallback;
  const sub = [track?.artist, showSource && track?.source ? sourceLabel(track.source) : null].filter(Boolean).join("  ");
  const body = (
    <>
      {cover ? <TrackCover track={track} /> : null}
      <span className="flex min-w-0 flex-col">
        <span className={cn("truncate text-xs font-medium", href && "group-hover:underline")} title={title}>
          {title}
        </span>
        {sub ? <span className="truncate text-[11px] text-muted-foreground">{sub}</span> : null}
      </span>
    </>
  );
  const cls = cn("group flex min-w-0 items-center gap-2.5", className);
  return href ? (
    <Link href={href} className={cls} onClick={(e) => e.stopPropagation()}>
      {body}
    </Link>
  ) : (
    <span className={cls}>{body}</span>
  );
}

export function userDisplayName(userId: number, names?: Map<number, MusicUserName>) {
  const info = names?.get(userId);
  return info?.nickname || info?.account || `用户 #${userId}`;
}

/** 昵称 + #ID；昵称取不到时只显示「用户 #ID」 */
export function UserCell({ userId, names, href }: { userId: number; names?: Map<number, MusicUserName>; href?: string }) {
  const info = names?.get(userId);
  const label = info?.nickname || info?.account;
  const body = (
    <span className="flex min-w-0 flex-col">
      <span className={cn("truncate text-xs font-medium", href && "group-hover:underline")}>{label || `用户 #${userId}`}</span>
      {label ? <span className="font-mono text-[11px] text-muted-foreground tabular-nums">#{userId}</span> : null}
    </span>
  );
  return href ? (
    <Link href={href} className="group flex min-w-0" onClick={(e) => e.stopPropagation()}>
      {body}
    </Link>
  ) : (
    body
  );
}

/* ── 分页 ── */

export function MusicPager({ total, page, pageSize, onPage }: { total: number; page: number; pageSize: number; onPage: (page: number) => void }) {
  if (total <= 0) return null;
  const totalPages = Math.max(1, Math.ceil(total / pageSize));
  return (
    <div className="mt-3 flex items-center justify-between gap-3 text-xs text-muted-foreground">
      <span className="tabular-nums">
        共 {total.toLocaleString("zh-CN")} 条，第 {page} / {totalPages} 页
      </span>
      <div className="flex gap-1.5">
        <Button size="sm" variant="outline" disabled={page <= 1} onClick={() => onPage(page - 1)}>
          上一页
        </Button>
        <Button size="sm" variant="outline" disabled={page >= totalPages} onClick={() => onPage(page + 1)}>
          下一页
        </Button>
      </div>
    </div>
  );
}

/* ── 环比 ── */

export function Delta({ ratio, badWhenUp, className }: { ratio: number | null | undefined; badWhenUp?: boolean; className?: string }) {
  if (typeof ratio !== "number" || Math.abs(ratio) < 0.01) return null;
  const up = ratio > 0;
  const good = badWhenUp ? !up : up;
  const v = Math.abs(ratio * 100);
  return (
    <span
      className={cn(
        "inline-flex items-center text-xs font-medium tabular-nums",
        good ? "text-emerald-600 dark:text-emerald-400" : "text-rose-600 dark:text-rose-400",
        className
      )}
    >
      {up ? <ArrowUpRight className="size-3.5" /> : <ArrowDownRight className="size-3.5" />}
      {v >= 100 ? Math.round(v) : v.toFixed(1)}%
    </span>
  );
}

/* ── 场景多选 ── */

/**
 * 推荐场景多选：规则与实验共用。`allowAll` 时多一个「全部场景」，选中它等于空数组（契约：空 = 全部场景）。
 */
export function SceneToggles({
  value,
  onChange,
  allowAll,
  disabled,
  invalid
}: {
  value: string[];
  onChange: (next: string[]) => void;
  allowAll?: boolean;
  disabled?: boolean;
  invalid?: boolean;
}) {
  const set = new Set(value);
  const toggle = (scene: string) => {
    const next = new Set(set);
    if (next.has(scene)) next.delete(scene);
    else next.add(scene);
    onChange(SCENES.filter((s) => next.has(s)));
  };
  return (
    <div className={cn("flex flex-wrap gap-1.5 rounded-md", invalid && "ring-1 ring-rose-500/60 ring-offset-2 ring-offset-background")}>
      {allowAll ? (
        <Button type="button" size="xs" variant={value.length === 0 ? "secondary" : "outline"} disabled={disabled} onClick={() => onChange([])}>
          全部场景
        </Button>
      ) : null}
      {SCENES.map((scene) => (
        <Button
          key={scene}
          type="button"
          size="xs"
          variant={set.has(scene) ? "secondary" : "outline"}
          aria-pressed={set.has(scene)}
          disabled={disabled}
          onClick={() => toggle(scene)}
        >
          {sceneLabel(scene)}
        </Button>
      ))}
    </div>
  );
}

/** 场景列表的一句话：空数组为「全部场景」 */
export function scenesText(scenes: string[]) {
  return scenes.length ? scenes.map(sceneLabel).join("、") : "全部场景";
}

/* ── 时间 ── */

/** 每隔 `interval` 毫秒刷新一次的当前时间：渲染里判断「生效中 / 已结束」用，不在渲染时直接读 Date.now() */
export function useNow(interval = 30_000) {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), interval);
    return () => window.clearInterval(timer);
  }, [interval]);
  return now;
}

/* ── 占位 ── */

export function PanelSkeleton({ height = 240, className }: { height?: number; className?: string }) {
  return (
    <div className={cn("flex min-w-0 flex-col gap-3 rounded-2xl border border-border bg-card p-4 sm:p-5", className)}>
      <div className="flex items-center gap-3">
        <Skeleton className="size-7 rounded-lg" />
        <Skeleton className="h-4 w-28" />
      </div>
      <Skeleton className="w-full rounded-xl" style={{ height }} />
    </div>
  );
}

export function RowsSkeleton({ rows = 6 }: { rows?: number }) {
  return (
    <div className="flex flex-col gap-2">
      {Array.from({ length: rows }).map((_, i) => (
        <Skeleton key={i} className="h-10 w-full rounded-lg" />
      ))}
    </div>
  );
}

export function EmptyLine({ children, className }: { children: React.ReactNode; className?: string }) {
  return (
    <p className={cn("rounded-xl border border-dashed border-border px-4 py-8 text-center text-xs text-muted-foreground", className)}>{children}</p>
  );
}

/** 横向排名条：名称 + 条 + 数值。条宽按本组最大值缩放 */
export function RankBars({
  items,
  barClassName = "bg-sky-500",
  empty = "暂无数据"
}: {
  items: Array<{ key: string; label: React.ReactNode; value: number; display?: React.ReactNode; title?: string }>;
  barClassName?: string;
  empty?: string;
}) {
  if (!items.length) return <EmptyLine>{empty}</EmptyLine>;
  const max = Math.max(1, ...items.map((i) => i.value));
  return (
    <ul className="flex min-w-0 flex-col gap-2">
      {items.map((item) => (
        <li key={item.key} className="grid min-w-0 grid-cols-[minmax(0,7rem)_minmax(0,1fr)_auto] items-center gap-3" title={item.title}>
          <span className="truncate text-xs">{item.label}</span>
          <span className="h-2 min-w-0 overflow-hidden rounded-full bg-muted">
            <span className={cn("block h-full rounded-full transition-[width] duration-500", barClassName)} style={{ width: `${(item.value / max) * 100}%` }} />
          </span>
          <span className="text-right text-xs text-muted-foreground tabular-nums">{item.display ?? item.value.toLocaleString("zh-CN")}</span>
        </li>
      ))}
    </ul>
  );
}
