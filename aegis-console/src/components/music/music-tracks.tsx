"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { Disc3, Search } from "lucide-react";
import { Panel, Segmented, TabStack } from "@/components/traffic/traffic-kit";
import { Input } from "@/components/ui/input";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import type { MusicTrackSort } from "@/lib/api/music";
import { useMusicTracksQuery } from "@/lib/music-hooks";
import { useDebouncedValue } from "@/lib/use-debounced-value";
import { cn } from "@/lib/utils";
import { SORT_LABEL, minutesText, num, pct } from "./music-format";
import { DaysSwitch, EmptyLine, MusicError, MusicPager, RowsSkeleton, TrackCell, musicTrackHref } from "./music-kit";

const PAGE_SIZE = 20;
const SORTS = (Object.keys(SORT_LABEL) as MusicTrackSort[]).map((value) => ({ value, label: SORT_LABEL[value] }));

/**
 * 作品排行：同一首歌在各音源的版本合并为一个作品（作品键见契约），点行进作品详情。
 * 排序、周期、搜索任一变化都回到第 1 页。
 */
export function MusicTracks({ appKey }: { appKey: string }) {
  const router = useRouter();
  const [days, setDays] = useState(7);
  const [sort, setSort] = useState<MusicTrackSort>("plays");
  const [q, setQ] = useState("");
  const [page, setPage] = useState(1);
  const keyword = useDebouncedValue(q.trim(), 300);
  const query = useMusicTracksQuery(appKey, { days, q: keyword, sort, page, pageSize: PAGE_SIZE });
  const items = query.data?.items ?? [];
  const offset = (page - 1) * PAGE_SIZE;

  return (
    <TabStack>
      <Panel
        icon={Disc3}
        tone="violet"
        title="作品排行"
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
                placeholder="歌名或歌手"
                aria-label="搜索作品"
                className="h-8 w-44 pl-8 text-xs"
              />
            </div>
            <Segmented
              value={sort}
              options={SORTS}
              onChange={(v) => {
                setSort(v);
                setPage(1);
              }}
              ariaLabel="排序"
            />
            <DaysSwitch
              value={days}
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
          <EmptyLine>{keyword ? "没有匹配的作品" : "暂无播放"}</EmptyLine>
        ) : (
          <div className={cn("min-w-0 overflow-x-auto", query.isPlaceholderData && "opacity-60")}>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="w-10">排名</TableHead>
                  <TableHead>作品</TableHead>
                  <TableHead className="text-right">播放</TableHead>
                  <TableHead className="text-right">听众</TableHead>
                  <TableHead className="text-right">听歌时长</TableHead>
                  <TableHead className="text-right">完播率</TableHead>
                  <TableHead className="text-right">跳过率</TableHead>
                  <TableHead className="text-right">收藏</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {items.map((row, index) => (
                  <TableRow key={row.workKey} className="cursor-pointer" onClick={() => router.push(musicTrackHref(appKey, row.workKey))}>
                    <TableCell className="text-xs text-muted-foreground tabular-nums">{offset + index + 1}</TableCell>
                    <TableCell className="max-w-72">
                      <TrackCell track={row.track} href={musicTrackHref(appKey, row.workKey)} />
                    </TableCell>
                    <TableCell className={cn("text-right text-xs tabular-nums", sort === "plays" && "font-medium")}>{num(row.plays)}</TableCell>
                    <TableCell className={cn("text-right text-xs tabular-nums", sort === "listeners" && "font-medium")}>{num(row.listeners)}</TableCell>
                    <TableCell className="text-right text-xs tabular-nums">{minutesText(row.minutes)}</TableCell>
                    <TableCell className={cn("text-right text-xs tabular-nums", sort === "completion" && "font-medium")}>{pct(row.completionRate)}</TableCell>
                    <TableCell
                      className={cn(
                        "text-right text-xs tabular-nums",
                        sort === "skip" && "font-medium",
                        row.skipRate >= 0.5 && "text-amber-600 dark:text-amber-400"
                      )}
                    >
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
