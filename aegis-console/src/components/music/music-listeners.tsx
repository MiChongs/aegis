"use client";

import { useMemo, useState } from "react";
import { useRouter } from "next/navigation";
import { Search, Users } from "lucide-react";
import { Panel, TabStack } from "@/components/traffic/traffic-kit";
import { Input } from "@/components/ui/input";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { formatDateTimeMinute, formatRelative } from "@/lib/datetime";
import { useMusicUserNames, useMusicUsersQuery } from "@/lib/music-hooks";
import { useDebouncedValue } from "@/lib/use-debounced-value";
import { cn } from "@/lib/utils";
import { minutesText, num, pct } from "./music-format";
import { DAYS_LONG, DaysSwitch, EmptyLine, MusicError, MusicPager, RowsSkeleton, UserCell, musicUserHref } from "./music-kit";

const PAGE_SIZE = 20;

/**
 * 听众排行：按听歌时长排序，点行进用户画像。搜索框只认用户 ID（接口 q 为用户 ID），输入即查（300ms 防抖）。
 * 昵称由用户列表接口按 ID 批量补齐，没有用户读权限时显示「用户 #ID」。
 */
export function MusicListeners({ appKey }: { appKey: string }) {
  const router = useRouter();
  const [days, setDays] = useState(30);
  const [q, setQ] = useState("");
  const [page, setPage] = useState(1);
  const keyword = useDebouncedValue(q.trim(), 300);
  const query = useMusicUsersQuery(appKey, { days, q: keyword, page, pageSize: PAGE_SIZE });
  const items = useMemo(() => query.data?.items ?? [], [query.data]);
  const ids = useMemo(() => items.map((i) => i.userId), [items]);
  const names = useMusicUserNames(appKey, ids);
  const offset = (page - 1) * PAGE_SIZE;

  return (
    <TabStack>
      <Panel
        icon={Users}
        tone="sky"
        title="听众排行"
        aside={
          <>
            <div className="relative">
              <Search className="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-muted-foreground" />
              <Input
                value={q}
                inputMode="numeric"
                onChange={(e) => {
                  setQ(e.target.value.replace(/[^\d]/g, ""));
                  setPage(1);
                }}
                placeholder="用户 ID"
                aria-label="按用户 ID 搜索"
                className="h-8 w-36 pl-8 text-xs"
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
          <EmptyLine>{keyword ? "没有这位用户的听歌记录" : "暂无听众"}</EmptyLine>
        ) : (
          <div className={cn("min-w-0 overflow-x-auto", query.isPlaceholderData && "opacity-60")}>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="w-10">排名</TableHead>
                  <TableHead>用户</TableHead>
                  <TableHead className="text-right">听歌时长</TableHead>
                  <TableHead className="text-right">播放</TableHead>
                  <TableHead className="text-right">歌曲</TableHead>
                  <TableHead className="text-right">收藏</TableHead>
                  <TableHead className="text-right">跳过率</TableHead>
                  <TableHead className="text-right">最近听歌</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {items.map((row, index) => (
                  <TableRow key={row.userId} className="cursor-pointer" onClick={() => router.push(musicUserHref(appKey, row.userId))}>
                    <TableCell className="text-xs text-muted-foreground tabular-nums">{offset + index + 1}</TableCell>
                    <TableCell className="max-w-56">
                      <UserCell userId={row.userId} names={names} href={musicUserHref(appKey, row.userId)} />
                    </TableCell>
                    <TableCell className="text-right text-xs font-medium tabular-nums">{minutesText(row.minutes)}</TableCell>
                    <TableCell className="text-right text-xs tabular-nums">{num(row.plays)}</TableCell>
                    <TableCell className="text-right text-xs tabular-nums">{num(row.tracks)}</TableCell>
                    <TableCell className="text-right text-xs tabular-nums">{num(row.likes)}</TableCell>
                    <TableCell
                      className={cn("text-right text-xs tabular-nums", row.skipRate >= 0.5 && "text-amber-600 dark:text-amber-400")}
                    >
                      {pct(row.skipRate, 0)}
                    </TableCell>
                    <TableCell className="text-right text-xs whitespace-nowrap text-muted-foreground" title={formatDateTimeMinute(row.lastActiveAt)}>
                      {formatRelative(row.lastActiveAt)}
                    </TableCell>
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
