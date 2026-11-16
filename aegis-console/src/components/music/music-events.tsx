"use client";

import { Fragment, useMemo, useState } from "react";
import { ChevronRight, ScrollText, Search } from "lucide-react";
import { CommerceRangePicker, type CommerceRange } from "@/components/commerce/commerce-range-picker";
import { Panel, TabStack } from "@/components/traffic/traffic-kit";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import type { MusicEvent } from "@/lib/api/music";
import { formatDateTime, formatShortDateTime } from "@/lib/datetime";
import { useMusicEventsQuery, useMusicUserNames } from "@/lib/music-hooks";
import { useDebouncedValue } from "@/lib/use-debounced-value";
import { cn } from "@/lib/utils";
import { EVENT_TYPES, eventLabel, eventSummary, platformLabel, sceneLabel, sourceLabel } from "./music-format";
import { EmptyLine, MusicError, MusicPager, RowsSkeleton, TrackCell, UserCell, musicTrackHref, musicUserHref } from "./music-kit";

const PAGE_SIZE = 30;
const ALL = "all";

/**
 * 事件检索：按类型、用户、关键词（歌名 / 歌手 / 搜索词）与时间范围查原始行为事件，行展开看上下文与自由键值。
 * 时间范围在页面上按浏览器时区选日，传给接口的是毫秒时间戳。
 */
export function MusicEvents({ appKey, initialUserId }: { appKey: string; initialUserId?: number }) {
  const [type, setType] = useState<string>(ALL);
  const [userId, setUserId] = useState(initialUserId ? String(initialUserId) : "");
  const [q, setQ] = useState("");
  const [range, setRange] = useState<CommerceRange>({});
  const [page, setPage] = useState(1);
  const [open, setOpen] = useState<string | null>(null);
  const keyword = useDebouncedValue(q.trim(), 300);
  const uid = useDebouncedValue(userId, 300);

  const params = useMemo(
    () => ({
      type: type === ALL ? undefined : type,
      userId: uid ? Number(uid) : undefined,
      q: keyword || undefined,
      from: range.start ? Date.parse(range.start) : undefined,
      to: range.end ? Date.parse(range.end) : undefined,
      page,
      pageSize: PAGE_SIZE
    }),
    [type, uid, keyword, range, page]
  );
  const query = useMusicEventsQuery(appKey, params);
  const items = useMemo(() => query.data?.items ?? [], [query.data]);
  const ids = useMemo(() => items.map((e) => e.userId), [items]);
  const names = useMusicUserNames(appKey, ids);

  const reset = () => setPage(1);

  return (
    <TabStack>
      <Panel
        icon={ScrollText}
        tone="sky"
        title="事件检索"
        aside={
          <>
            <Select
              value={type}
              onValueChange={(v) => {
                setType(v);
                reset();
              }}
            >
              <SelectTrigger size="sm" className="h-8 w-36 text-xs" aria-label="事件类型">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={ALL}>全部类型</SelectItem>
                {EVENT_TYPES.map((t) => (
                  <SelectItem key={t} value={t}>
                    {eventLabel(t)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Input
              value={userId}
              inputMode="numeric"
              onChange={(e) => {
                setUserId(e.target.value.replace(/[^\d]/g, ""));
                reset();
              }}
              placeholder="用户 ID"
              aria-label="用户 ID"
              className="h-8 w-28 text-xs"
            />
            <div className="relative">
              <Search className="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-muted-foreground" />
              <Input
                value={q}
                onChange={(e) => {
                  setQ(e.target.value);
                  reset();
                }}
                placeholder="歌名、歌手或搜索词"
                aria-label="关键词"
                className="h-8 w-48 pl-8 text-xs"
              />
            </div>
            <CommerceRangePicker
              value={range}
              onChange={(next) => {
                setRange(next);
                reset();
              }}
            />
          </>
        }
      >
        {query.isError && !query.data ? (
          <MusicError error={query.error} onRetry={() => query.refetch()} />
        ) : query.isLoading ? (
          <RowsSkeleton rows={8} />
        ) : !items.length ? (
          <EmptyLine>没有符合条件的事件</EmptyLine>
        ) : (
          <div className={cn("min-w-0 overflow-x-auto", query.isPlaceholderData && "opacity-60")}>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="w-8" />
                  <TableHead>时间</TableHead>
                  <TableHead>用户</TableHead>
                  <TableHead>事件</TableHead>
                  <TableHead>歌曲</TableHead>
                  <TableHead>说明</TableHead>
                  <TableHead className="text-right">平台</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {items.map((event) => {
                  const expanded = open === event.id;
                  return (
                    <Fragment key={event.id}>
                      <TableRow className="cursor-pointer" data-state={expanded ? "selected" : undefined} onClick={() => setOpen(expanded ? null : event.id)}>
                        <TableCell className="py-2">
                          <ChevronRight className={cn("size-3.5 text-muted-foreground transition-transform", expanded && "rotate-90")} />
                        </TableCell>
                        <TableCell className="text-xs whitespace-nowrap tabular-nums" title={formatDateTime(event.ts)}>
                          {formatShortDateTime(event.ts)}
                        </TableCell>
                        <TableCell className="max-w-40">
                          <UserCell userId={event.userId} names={names} href={musicUserHref(appKey, event.userId)} />
                        </TableCell>
                        <TableCell>
                          <Badge variant="outline" size="sm">
                            {eventLabel(event.type)}
                          </Badge>
                        </TableCell>
                        <TableCell className="max-w-56">
                          {event.track ? (
                            <TrackCell track={event.track} cover={false} href={event.workKey ? musicTrackHref(appKey, event.workKey) : undefined} />
                          ) : (
                            <span className="text-xs text-muted-foreground">—</span>
                          )}
                        </TableCell>
                        <TableCell className="max-w-64 truncate text-xs text-muted-foreground">{eventSummary(event) || "—"}</TableCell>
                        <TableCell className="text-right text-xs whitespace-nowrap text-muted-foreground">
                          {platformLabel(event.platform)}
                          {event.appVersion ? ` ${event.appVersion}` : ""}
                        </TableCell>
                      </TableRow>
                      {expanded ? (
                        <TableRow className="hover:bg-transparent">
                          <TableCell colSpan={7} className="bg-muted/30 p-0">
                            <EventDetail event={event} />
                          </TableCell>
                        </TableRow>
                      ) : null}
                    </Fragment>
                  );
                })}
              </TableBody>
            </Table>
          </div>
        )}
        <MusicPager total={query.data?.total ?? 0} page={page} pageSize={PAGE_SIZE} onPage={setPage} />
      </Panel>
    </TabStack>
  );
}

/** 展开后的明细：上下文与自由键值，空字段不列 */
export function EventDetail({ event }: { event: MusicEvent }) {
  const ctx = event.context ?? {};
  const facts: Array<[string, string | number | undefined]> = [
    ["发生时间", formatDateTime(event.ts)],
    ["服务端收到", formatDateTime(event.receivedAt)],
    ["事件 ID", event.id],
    ["会话", event.sessionId],
    ["设备", event.deviceId],
    ["网络", event.network],
    ["音源", event.track ? `${sourceLabel(event.track.source)} ${event.track.songId}` : undefined],
    ["作品键", event.workKey],
    ["页面", ctx.page],
    ["分区", ctx.section],
    ["位置", ctx.position],
    ["搜索词", ctx.query],
    ["推荐场景", ctx.scene ? sceneLabel(ctx.scene) : undefined],
    ["推荐批次", ctx.recId],
    ["歌单", ctx.playlistId],
    ["播放队列来源", ctx.queueSource]
  ];
  const props = Object.entries(event.props ?? {});
  return (
    <div className="flex min-w-0 flex-col gap-3 px-4 py-3">
      <dl className="grid min-w-0 grid-cols-2 gap-x-6 gap-y-2 @2xl:grid-cols-4">
        {facts
          .filter(([, v]) => v !== undefined && v !== null && v !== "")
          .map(([label, value]) => (
            <div key={label} className="min-w-0">
              <dt className="text-[11px] text-muted-foreground">{label}</dt>
              <dd className="truncate font-mono text-xs" title={String(value)}>
                {value}
              </dd>
            </div>
          ))}
      </dl>
      {props.length ? (
        <div className="flex min-w-0 flex-col gap-1.5">
          <span className="text-[11px] text-muted-foreground">附加属性</span>
          <div className="flex min-w-0 flex-wrap gap-1.5">
            {props.map(([k, v]) => (
              <span key={k} className="inline-flex max-w-full items-center gap-1 rounded-md border border-border bg-background px-2 py-0.5 font-mono text-[11px]">
                <span className="text-muted-foreground">{k}</span>
                <span className="truncate">{v}</span>
              </span>
            ))}
          </div>
        </div>
      ) : null}
    </div>
  );
}
