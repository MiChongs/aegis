"use client";

import { useMemo, useState } from "react";
import { useRouter } from "next/navigation";
import { MicVocal, Search } from "lucide-react";
import { Panel, TabStack } from "@/components/traffic/traffic-kit";
import { Input } from "@/components/ui/input";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { useMusicArtistsQuery } from "@/lib/music-hooks";
import { useDebouncedValue } from "@/lib/use-debounced-value";
import { cn } from "@/lib/utils";
import { minutesText, num, pct } from "./music-format";
import { ArtistCell, DAYS_LONG, DaysSwitch, EmptyLine, MusicError, MusicPager, RowsSkeleton, musicArtistHref } from "./music-kit";

const PAGE_SIZE = 20;

/**
 * 歌手排行（GET /artists）：按播放排序，点行进歌手详情（统计、走势、代表作、相似歌手）。
 * 周期、搜索任一变化都回到第 1 页。
 */
export function MusicArtists({ appKey }: { appKey: string }) {
  const router = useRouter();
  const [days, setDays] = useState(30);
  const [q, setQ] = useState("");
  const [page, setPage] = useState(1);
  const keyword = useDebouncedValue(q.trim(), 300);
  const query = useMusicArtistsQuery(appKey, { days, q: keyword, page, pageSize: PAGE_SIZE });
  const items = useMemo(() => query.data?.items ?? [], [query.data]);
  const offset = (page - 1) * PAGE_SIZE;

  return (
    <TabStack>
      <Panel
        icon={MicVocal}
        tone="amber"
        title="歌手排行"
        aside={
          <>
            <div className="relative">
              <Search className="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-muted-foreground" />
              <Input
                value={q}
                onChange={(e) => {
                  setQ(e.target.value);
                  setPage(1);
                }}
                placeholder="歌手名"
                aria-label="搜索歌手"
                className="h-8 w-40 pl-8 text-xs"
              />
            </div>
            <DaysSwitch
              value={days}
              options={DAYS_LONG}
              onChange={(v) => {
                setDays(v);
                setPage(1);
              }}
            />
          </>
        }
      >
        {query.isError && !query.data ? (
          <MusicError error={query.error} onRetry={() => query.refetch()} />
        ) : query.isLoading ? (
          <RowsSkeleton />
        ) : !items.length ? (
          <EmptyLine>{keyword ? "没有匹配的歌手" : "暂无播放"}</EmptyLine>
        ) : (
          <div className={cn("min-w-0 overflow-x-auto", query.isPlaceholderData && "opacity-60")}>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="w-10">排名</TableHead>
                  <TableHead>歌手</TableHead>
                  <TableHead className="text-right">播放</TableHead>
                  <TableHead className="text-right">听众</TableHead>
                  <TableHead className="text-right">听歌时长</TableHead>
                  <TableHead className="text-right">作品</TableHead>
                  <TableHead className="text-right">完播率</TableHead>
                  <TableHead className="text-right">跳过率</TableHead>
                  <TableHead className="text-right">收藏</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {items.map((row, index) => (
                  <TableRow key={row.name} className="cursor-pointer" onClick={() => router.push(musicArtistHref(appKey, row.name))}>
                    <TableCell className="text-xs text-muted-foreground tabular-nums">{offset + index + 1}</TableCell>
                    <TableCell className="max-w-64">
                      <ArtistCell name={row.name} cover={row.cover} href={musicArtistHref(appKey, row.name)} />
                    </TableCell>
                    <TableCell className="text-right text-xs font-medium tabular-nums">{num(row.plays)}</TableCell>
                    <TableCell className="text-right text-xs tabular-nums">{num(row.listeners)}</TableCell>
                    <TableCell className="text-right text-xs tabular-nums">{minutesText(row.minutes)}</TableCell>
                    <TableCell className="text-right text-xs tabular-nums">{num(row.tracks)}</TableCell>
                    <TableCell className="text-right text-xs tabular-nums">{pct(row.completionRate)}</TableCell>
                    <TableCell className={cn("text-right text-xs tabular-nums", row.skipRate >= 0.5 && "text-amber-600 dark:text-amber-400")}>
                      {pct(row.skipRate)}
                    </TableCell>
                    <TableCell className="text-right text-xs tabular-nums">{num(row.likes)}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        )}
        <MusicPager total={query.data?.total ?? 0} page={page} pageSize={PAGE_SIZE} onPage={setPage} />
      </Panel>
    </TabStack>
  );
}
