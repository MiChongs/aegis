"use client";

import { useState } from "react";
import { toast } from "sonner";
import { Loader2, Pencil, RefreshCw, Search, Tags } from "lucide-react";
import { Panel, Segmented } from "@/components/traffic/traffic-kit";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle
} from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { ApiError } from "@/lib/api/client";
import type { MusicContentStatus, MusicContentTrack } from "@/lib/api/music";
import { formatDateTimeMinute, formatRelative } from "@/lib/datetime";
import { useMusicContentTracksQuery, useRetagMusicMutation } from "@/lib/music-hooks";
import { useDebouncedValue } from "@/lib/use-debounced-value";
import { cn } from "@/lib/utils";
import { CONTENT_STATUS, num, pct } from "./music-format";
import { EmptyLine, MusicError, MusicPager, RowsSkeleton, TrackCell, musicTrackHref } from "./music-kit";
import { TagChips, TagEditorDialog, tagsBrief } from "./music-tag-parts";

const PAGE_SIZE = 20;
const ALL = "all";
const STATUS_OPTIONS = [
  { value: ALL, label: "全部" },
  { value: "tagged", label: CONTENT_STATUS.tagged.label },
  { value: "pending", label: CONTENT_STATUS.pending.label },
  { value: "failed", label: CONTENT_STATUS.failed.label }
] as const;
type StatusFilter = (typeof STATUS_OPTIONS)[number]["value"];

/**
 * 作品标签列表：按状态筛选、按歌名歌手搜索、分页；勾选后重新打标签，或全部重新打标签（二次确认）。
 *
 * 勾选只在当前筛选条件下累积，换筛选或搜索词时清空，免得「看不见的已选」被一起提交。
 * 「全部重新打标签」会把整个曲库重新排队，按每日上限慢慢消化，并消耗大模型额度，所以单独确认。
 */
export function MusicContentTracks({ appKey, canManage }: { appKey: string; canManage: boolean }) {
  const [status, setStatus] = useState<StatusFilter>(ALL);
  const [q, setQ] = useState("");
  const [page, setPage] = useState(1);
  const keyword = useDebouncedValue(q.trim(), 300);
  const scope = `${status}|${keyword}`;
  const [selection, setSelection] = useState<{ scope: string; keys: Set<string> }>({ scope, keys: new Set() });
  const selected = selection.scope === scope ? selection.keys : new Set<string>();
  const [editing, setEditing] = useState<MusicContentTrack | null>(null);
  const [confirmAll, setConfirmAll] = useState(false);
  const query = useMusicContentTracksQuery(appKey, {
    status: status === ALL ? undefined : (status as MusicContentStatus),
    q: keyword,
    page,
    pageSize: PAGE_SIZE
  });
  const retag = useRetagMusicMutation(appKey);
  const items = query.data?.items ?? [];
  const pageKeys = items.map((i) => i.workKey);
  const allOnPage = pageKeys.length > 0 && pageKeys.every((k) => selected.has(k));
  const someOnPage = pageKeys.some((k) => selected.has(k));

  const setKeys = (keys: Set<string>) => setSelection({ scope, keys });
  const toggle = (key: string, on: boolean) => {
    const next = new Set(selected);
    if (on) next.add(key);
    else next.delete(key);
    setKeys(next);
  };
  const togglePage = (on: boolean) => {
    const next = new Set(selected);
    for (const k of pageKeys) {
      if (on) next.add(k);
      else next.delete(k);
    }
    setKeys(next);
  };

  const submitRetag = async (input: { workKeys?: string[]; all?: boolean }) => {
    try {
      const res = await retag.mutateAsync(input);
      const queued = typeof res?.queued === "number" ? res.queued : input.workKeys?.length;
      toast.success(typeof queued === "number" ? `已加入打标签队列，共 ${num(queued)} 首` : "已加入打标签队列");
      setKeys(new Set());
      setConfirmAll(false);
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "提交失败");
    }
  };

  return (
    <Panel
      icon={Tags}
      tone="violet"
      title="作品标签"
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
            value={status}
            options={STATUS_OPTIONS}
            onChange={(v) => {
              setStatus(v);
              setPage(1);
            }}
            ariaLabel="标签状态"
          />
        </>
      }
    >
      {canManage ? (
        <div className="mb-3 flex min-w-0 flex-wrap items-center gap-2">
          <span className="text-xs text-muted-foreground tabular-nums">{selected.size ? `已选 ${num(selected.size)} 首` : "勾选作品后可重新打标签"}</span>
          <div className="ml-auto flex flex-wrap gap-2">
            {selected.size ? (
              <Button size="sm" variant="ghost" disabled={retag.isPending} onClick={() => setKeys(new Set())}>
                清空选择
              </Button>
            ) : null}
            <Button size="sm" variant="outline" disabled={!selected.size || retag.isPending} onClick={() => submitRetag({ workKeys: [...selected] })}>
              {retag.isPending && !confirmAll ? <Loader2 className="animate-spin" /> : <RefreshCw />}
              重新打标签
            </Button>
            <Button size="sm" variant="outline" disabled={retag.isPending} onClick={() => setConfirmAll(true)}>
              <RefreshCw />
              全部重新打标签
            </Button>
          </div>
        </div>
      ) : null}

      {query.isError && !query.data ? (
        <MusicError error={query.error} onRetry={() => query.refetch()} />
      ) : query.isLoading ? (
        <RowsSkeleton />
      ) : !items.length ? (
        <EmptyLine>{keyword ? "没有匹配的作品" : status === ALL ? "曲库里还没有作品" : `没有${STATUS_OPTIONS.find((o) => o.value === status)?.label}的作品`}</EmptyLine>
      ) : (
        <div className={cn("min-w-0 overflow-x-auto", query.isPlaceholderData && "opacity-60")}>
          <Table>
            <TableHeader>
              <TableRow>
                {canManage ? (
                  <TableHead className="w-8">
                    <Checkbox
                      checked={allOnPage ? true : someOnPage ? "indeterminate" : false}
                      onCheckedChange={(v) => togglePage(v === true)}
                      aria-label="选择本页"
                    />
                  </TableHead>
                ) : null}
                <TableHead>作品</TableHead>
                <TableHead>曲风</TableHead>
                <TableHead>情绪</TableHead>
                <TableHead>场景</TableHead>
                <TableHead>其他</TableHead>
                <TableHead className="text-right">可信度</TableHead>
                <TableHead>状态</TableHead>
                {canManage ? <TableHead className="text-right">操作</TableHead> : null}
              </TableRow>
            </TableHeader>
            <TableBody>
              {items.map((row) => {
                const meta = CONTENT_STATUS[row.status] ?? { label: row.status, tone: "secondary" as const };
                const tags = row.tags;
                return (
                  <TableRow key={row.workKey} data-state={selected.has(row.workKey) ? "selected" : undefined}>
                    {canManage ? (
                      <TableCell>
                        <Checkbox checked={selected.has(row.workKey)} onCheckedChange={(v) => toggle(row.workKey, v === true)} aria-label="选择作品" />
                      </TableCell>
                    ) : null}
                    <TableCell className="max-w-64">
                      <TrackCell track={row.track} fallback={`作品 ${row.workKey}`} href={musicTrackHref(appKey, row.workKey)} />
                    </TableCell>
                    <TableCell className="max-w-44">
                      <TagChips kind="genres" keys={tags?.genres} />
                    </TableCell>
                    <TableCell className="max-w-44">
                      <TagChips kind="moods" keys={tags?.moods} />
                    </TableCell>
                    <TableCell className="max-w-44">
                      <TagChips kind="scenes" keys={tags?.scenes} />
                    </TableCell>
                    <TableCell className="text-[11px] leading-4 whitespace-nowrap text-muted-foreground">{tagsBrief(tags) || "—"}</TableCell>
                    <TableCell
                      className={cn("text-right text-xs tabular-nums", tags && tags.confidence < 0.5 && "text-amber-600 dark:text-amber-400")}
                      title={tags && tags.confidence < 0.5 ? "可信度偏低，建议人工核对" : undefined}
                    >
                      {tags ? pct(tags.confidence, 0) : "—"}
                    </TableCell>
                    <TableCell>
                      <div className="flex flex-col items-start gap-0.5">
                        <span className="flex flex-wrap gap-1">
                          <Badge variant={meta.tone} size="sm">
                            {meta.label}
                          </Badge>
                          {tags?.manual ? (
                            <Badge variant="outline" size="sm">
                              人工
                            </Badge>
                          ) : null}
                        </span>
                        {row.status === "failed" ? (
                          <span className="text-[11px] text-rose-600 tabular-nums dark:text-rose-400">已失败 {num(row.attempts)} 次</span>
                        ) : tags?.taggedAt ? (
                          <span className="text-[11px] whitespace-nowrap text-muted-foreground" title={formatDateTimeMinute(tags.taggedAt)}>
                            {formatRelative(tags.taggedAt)}
                          </span>
                        ) : null}
                      </div>
                    </TableCell>
                    {canManage ? (
                      <TableCell className="text-right">
                        <Button size="xs" variant="outline" onClick={() => setEditing(row)}>
                          <Pencil />
                          修改
                        </Button>
                      </TableCell>
                    ) : null}
                  </TableRow>
                );
              })}
            </TableBody>
          </Table>
        </div>
      )}
      <MusicPager total={query.data?.total ?? 0} page={page} pageSize={PAGE_SIZE} onPage={setPage} />

      <TagEditorDialog appKey={appKey} target={editing} onOpenChange={(open) => !open && setEditing(null)} />

      <AlertDialog open={confirmAll} onOpenChange={(open) => !retag.isPending && setConfirmAll(open)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>全部重新打标签？</AlertDialogTitle>
            <AlertDialogDescription>
              曲库里的全部作品会重新排队打标签并重算语义向量，按每日上限分批处理，会消耗相应的大模型额度。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={retag.isPending}>取消</AlertDialogCancel>
            <AlertDialogAction
              disabled={retag.isPending}
              onClick={(e) => {
                e.preventDefault();
                void submitRetag({ all: true });
              }}
            >
              {retag.isPending ? <Loader2 className="animate-spin" /> : null}
              全部重新打标签
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </Panel>
  );
}
