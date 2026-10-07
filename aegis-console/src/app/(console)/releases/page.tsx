"use client";

import { useMemo, useState } from "react";
import { Plus, RefreshCw, Search } from "lucide-react";
import type { Release } from "@/lib/api/types";
import { useAdminAppsQuery } from "@/lib/admin-hooks";
import { useReleaseChannelsQuery, useReleaseOverviewQuery, useReleasesQuery } from "@/lib/release-hooks";
import { useDebouncedValue } from "@/lib/use-debounced-value";
import { formatDateTime } from "@/components/content/content-shared";
import { ReleaseChannels } from "@/components/releases/release-channels";
import { ReleaseDetail } from "@/components/releases/release-detail";
import { ReleaseEditor } from "@/components/releases/release-editor";
import { ReleaseOverview } from "@/components/releases/release-overview";
import {
  ChannelDot,
  DATA_FONT,
  RELEASE_PLATFORMS,
  RELEASE_STATUSES,
  ReleaseStatusBadge,
  UpdateTypeBadge,
  platformLabel
} from "@/components/releases/release-shared";
import { ReleaseSimulator } from "@/components/releases/release-simulator";
import { Button } from "@/components/ui/button";
import { EmptyState, LoadingState } from "@/components/ui/data-state";
import { Input } from "@/components/ui/input";
import { SectionHeading } from "@/components/ui/section-heading";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { cn } from "@/lib/utils";

const PAGE_SIZE = 20;

export default function ReleasesPage() {
  const appsQuery = useAdminAppsQuery();
  const apps = useMemo(() => appsQuery.data ?? [], [appsQuery.data]);
  const [selectedAppKey, setSelectedAppKey] = useState<string | null>(null);
  const resolvedApp = useMemo(
    () => apps.find((app) => app.appKey === selectedAppKey) ?? apps[0] ?? null,
    [apps, selectedAppKey]
  );
  const appKey = resolvedApp?.appKey ?? null;

  const [page, setPage] = useState(1);
  const [status, setStatus] = useState("all");
  const [platform, setPlatform] = useState("all");
  const [channel, setChannel] = useState("all");
  const [keywordInput, setKeywordInput] = useState("");
  const keyword = useDebouncedValue(keywordInput.trim(), 300);

  const overviewQuery = useReleaseOverviewQuery(appKey);
  const channelsQuery = useReleaseChannelsQuery(appKey);
  const releasesQuery = useReleasesQuery(appKey, {
    page,
    limit: PAGE_SIZE,
    status: status === "all" ? undefined : status,
    platform: platform === "all" ? undefined : platform,
    channelId: channel === "all" ? undefined : Number(channel),
    keyword: keyword || undefined
  });
  const channels = channelsQuery.data ?? [];
  const releases = releasesQuery.data?.items ?? [];
  const totalPages = Math.max(1, releasesQuery.data?.totalPages ?? 1);

  const [editor, setEditor] = useState<{ open: boolean; item?: Release | null; seq: number }>({ open: false, seq: 0 });
  const [detailId, setDetailId] = useState<number | null>(null);

  const openEditor = (item?: Release | null) => setEditor((state) => ({ open: true, item: item ?? null, seq: state.seq + 1 }));
  const resetPage = <T,>(setter: (value: T) => void) => (value: T) => {
    setter(value);
    setPage(1);
  };

  if (appsQuery.isLoading) {
    return <LoadingState title="正在加载发布中心" />;
  }
  if (!resolvedApp || !appKey) {
    return (
      <div className="page-stack">
        <SectionHeading eyebrow="Releases" title="发布" description="当前没有可管理的应用。" />
        <EmptyState title="暂无应用" />
      </div>
    );
  }

  return (
    <div className="page-stack">
      <SectionHeading
        eyebrow="Releases"
        title="发布"
        description="版本发布、灰度放量与定向分发。"
        action={
          <div className="flex items-center gap-2">
            <Select
              value={resolvedApp.appKey}
              onValueChange={(value) => {
                setSelectedAppKey(value);
                setPage(1);
                setChannel("all");
              }}
            >
              <SelectTrigger className="w-56">
                <SelectValue placeholder="选择应用" />
              </SelectTrigger>
              <SelectContent>
                {apps.map((app) => (
                  <SelectItem key={app.appKey} value={app.appKey}>
                    {app.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Button size="sm" onClick={() => openEditor(null)}>
              <Plus className="size-3.5" />
              新建版本
            </Button>
          </div>
        }
      />

      <ReleaseOverview overview={overviewQuery.data} loading={overviewQuery.isLoading} onOpen={(release) => setDetailId(release.id)} />

      <Tabs defaultValue="releases" className="space-y-4">
        <TabsList>
          <TabsTrigger value="releases">版本</TabsTrigger>
          <TabsTrigger value="channels">渠道</TabsTrigger>
          <TabsTrigger value="simulate">模拟检测</TabsTrigger>
        </TabsList>

        <TabsContent value="releases" className="space-y-4">
          <div className="flex flex-wrap items-center gap-2">
            <div className="relative w-full sm:w-60">
              <Search className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
              <Input
                className="h-8 pl-8 text-xs"
                value={keywordInput}
                placeholder="版本名、标题或说明"
                onChange={(event) => {
                  setKeywordInput(event.target.value);
                  setPage(1);
                }}
              />
            </div>
            <FilterSelect value={status} onChange={resetPage(setStatus)} allLabel="全部状态" options={RELEASE_STATUSES} />
            <FilterSelect value={platform} onChange={resetPage(setPlatform)} allLabel="全部平台" options={RELEASE_PLATFORMS} />
            <FilterSelect
              value={channel}
              onChange={resetPage(setChannel)}
              allLabel="全部渠道"
              options={channels.map((item) => ({ value: String(item.id), label: item.name ?? item.code ?? String(item.id) }))}
            />
            <div className="flex-1" />
            <span className="text-xs text-muted-foreground">共 {releasesQuery.data?.total ?? 0} 个版本</span>
            <Button
              variant="outline"
              size="sm"
              className="h-8"
              aria-label="刷新"
              onClick={() => {
                void releasesQuery.refetch();
                void overviewQuery.refetch();
              }}
            >
              <RefreshCw className={cn("size-3.5", releasesQuery.isFetching && "animate-spin")} />
            </Button>
          </div>

          <div className="overflow-hidden rounded-xl border bg-card">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>版本</TableHead>
                  <TableHead>状态</TableHead>
                  <TableHead className="hidden md:table-cell">渠道</TableHead>
                  <TableHead className="hidden md:table-cell">平台</TableHead>
                  <TableHead>灰度</TableHead>
                  <TableHead className="hidden lg:table-cell">更新类型</TableHead>
                  <TableHead className="hidden lg:table-cell text-right">下载</TableHead>
                  <TableHead className="hidden xl:table-cell">发布时间</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {releasesQuery.isLoading ? (
                  Array.from({ length: 5 }, (_, index) => (
                    <TableRow key={index}>
                      {Array.from({ length: 8 }, (_, cell) => (
                        <TableCell key={cell} className={cn(cell >= 2 && "hidden md:table-cell")}>
                          <Skeleton className="h-4 w-full max-w-24" />
                        </TableCell>
                      ))}
                    </TableRow>
                  ))
                ) : releases.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={8} className="py-12 text-center text-sm text-muted-foreground">
                      {keyword || status !== "all" || platform !== "all" || channel !== "all" ? "没有符合条件的版本" : "暂无版本"}
                    </TableCell>
                  </TableRow>
                ) : (
                  releases.map((release) => (
                    <TableRow key={release.id} className="cursor-pointer" onClick={() => setDetailId(release.id)}>
                      <TableCell>
                        <div className={cn("font-medium", DATA_FONT)}>{release.version}</div>
                        <div className="max-w-56 truncate text-xs text-muted-foreground">
                          {release.title || <span className={DATA_FONT}>{release.versionCode}</span>}
                        </div>
                      </TableCell>
                      <TableCell>
                        <ReleaseStatusBadge release={release} />
                      </TableCell>
                      <TableCell className="hidden md:table-cell">
                        {release.channel ? (
                          <span className="inline-flex items-center gap-1.5 text-sm">
                            <ChannelDot color={release.channel.color} />
                            {release.channel.name}
                          </span>
                        ) : (
                          <span className="text-sm text-muted-foreground">不限</span>
                        )}
                      </TableCell>
                      <TableCell className="hidden md:table-cell text-sm">{platformLabel(release.platform)}</TableCell>
                      <TableCell>
                        <RolloutCell pct={release.rolloutPct} />
                      </TableCell>
                      <TableCell className="hidden lg:table-cell">
                        <UpdateTypeBadge value={release.updateType} />
                      </TableCell>
                      <TableCell className={cn("hidden lg:table-cell text-right text-sm", DATA_FONT)}>{release.downloadCount}</TableCell>
                      <TableCell className="hidden xl:table-cell text-xs text-muted-foreground">
                        {release.status === "scheduled" && release.publishAt
                          ? `定时 ${formatDateTime(release.publishAt)}`
                          : formatDateTime(release.publishedAt)}
                      </TableCell>
                    </TableRow>
                  ))
                )}
              </TableBody>
            </Table>
          </div>

          {totalPages > 1 ? (
            <div className="flex items-center justify-center gap-2">
              <Button variant="outline" size="sm" disabled={page <= 1} onClick={() => setPage(page - 1)}>
                上一页
              </Button>
              <span className={cn("text-xs text-muted-foreground", DATA_FONT)}>
                {page} / {totalPages}
              </span>
              <Button variant="outline" size="sm" disabled={page >= totalPages} onClick={() => setPage(page + 1)}>
                下一页
              </Button>
            </div>
          ) : null}
        </TabsContent>

        <TabsContent value="channels">
          <ReleaseChannels appKey={appKey} />
        </TabsContent>

        <TabsContent value="simulate">
          <ReleaseSimulator appKey={appKey} />
        </TabsContent>
      </Tabs>

      {editor.open ? (
        <ReleaseEditor
          key={editor.seq}
          appKey={appKey}
          item={editor.item}
          channels={channels}
          open={editor.open}
          onOpenChange={(open) => setEditor((state) => ({ ...state, open }))}
          onSaved={(release) => setDetailId(release.id)}
        />
      ) : null}

      <ReleaseDetail
        appKey={appKey}
        releaseId={detailId}
        open={detailId !== null}
        onOpenChange={(open) => !open && setDetailId(null)}
        onEdit={(release) => {
          setDetailId(null);
          openEditor(release);
        }}
      />
    </div>
  );
}

function FilterSelect({
  value,
  onChange,
  allLabel,
  options
}: {
  value: string;
  onChange: (value: string) => void;
  allLabel: string;
  options: Array<{ value: string; label: string }>;
}) {
  return (
    <Select value={value} onValueChange={onChange}>
      <SelectTrigger className="h-8 w-28 text-xs">
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        <SelectItem value="all">{allLabel}</SelectItem>
        {options.map((option) => (
          <SelectItem key={option.value} value={option.value}>
            {option.label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

function RolloutCell({ pct }: { pct: number }) {
  return (
    <div className="flex items-center gap-2">
      <div className="h-1.5 w-14 overflow-hidden rounded-full bg-muted">
        <div className={cn("h-full rounded-full", pct >= 100 ? "bg-emerald-500" : "bg-primary")} style={{ width: `${pct}%` }} />
      </div>
      <span className={cn("text-xs text-muted-foreground", DATA_FONT)}>{pct}%</span>
    </div>
  );
}
