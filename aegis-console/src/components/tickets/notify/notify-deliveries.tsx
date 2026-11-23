"use client";

import { useMemo, useState } from "react";
import { ChevronLeft, ChevronRight, History, RefreshCw, Trash2 } from "lucide-react";
import { notify } from "@/lib/notify";
import { formatDateTime, formatRelative } from "@/lib/datetime";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import {
  useNotifyCatalogQuery,
  useNotifyChannelsQuery,
  useNotifyDeliveriesQuery,
  usePurgeNotifyDeliveriesMutation
} from "@/lib/ticket-hooks";
import type { NotifyDelivery } from "@/lib/api/notify-channels";
import { cn } from "@/lib/utils";
import { ChannelMark, DELIVERY_STATUS_LABEL, DeliveryStatusBadge, SectionEmpty, SectionError, SectionHeader, reportError, useConfirm } from "./notify-kit";

// 投递记录：每一次向外发送的结果。点行看请求、响应与错误全文。

const PAGE_SIZE = 20;
const STATUS_FILTERS = ["success", "failed", "skipped", "dropped", "pending"] as const;

export function DeliveriesSection({ canWrite }: { canWrite: boolean }) {
  const [status, setStatus] = useState("all");
  const [channelId, setChannelId] = useState("all");
  const [eventKey, setEventKey] = useState("all");
  const [page, setPage] = useState(1);
  const [active, setActive] = useState<NotifyDelivery | null>(null);

  const catalogQuery = useNotifyCatalogQuery();
  const channelsQuery = useNotifyChannelsQuery();
  const listQuery = useNotifyDeliveriesQuery({
    status: status === "all" ? undefined : status,
    channelId: channelId === "all" ? undefined : Number(channelId),
    eventKey: eventKey === "all" ? undefined : eventKey,
    page,
    limit: PAGE_SIZE
  });
  const purgeMut = usePurgeNotifyDeliveriesMutation();
  const { confirm, dialog: confirmDialog } = useConfirm();

  const events = useMemo(() => catalogQuery.data?.events ?? [], [catalogQuery.data]);
  const eventName = useMemo(() => new Map(events.map((item) => [item.key, item.name])), [events]);
  const channels = channelsQuery.data ?? [];
  const data = listQuery.data;
  const items = data?.items ?? [];
  const filtered = status !== "all" || channelId !== "all" || eventKey !== "all";

  const resetPage = <T,>(setter: (value: T) => void) => (value: T) => {
    setter(value);
    setPage(1);
  };

  const askPurge = (days: number) =>
    confirm({
      title: `清理 ${days} 天前的投递记录？`,
      description: "清理后无法恢复，投递统计也会随之减少。",
      action: "清理",
      run: async () => {
        try {
          const result = await purgeMut.mutateAsync(days);
          notify.success(`已清理 ${result.deleted} 条`);
        } catch (error) {
          reportError(error, "清理失败");
        }
      }
    });

  return (
    <div className="space-y-4">
      <SectionHeader title="投递记录" description="每一次向外发送的结果，失败会显示渠道返回的原因。">
        <Button size="sm" variant="outline" onClick={() => listQuery.refetch()} disabled={listQuery.isFetching}>
          <RefreshCw className={cn(listQuery.isFetching && "animate-spin")} />
          刷新
        </Button>
        {canWrite ? (
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button size="sm" variant="ghost" className="text-destructive">
                <Trash2 />
                清理
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-40">
              <DropdownMenuLabel className="text-xs text-muted-foreground">清理更早的记录</DropdownMenuLabel>
              {[7, 30, 90].map((days) => (
                <DropdownMenuItem key={days} onSelect={() => askPurge(days)}>
                  {days} 天前
                </DropdownMenuItem>
              ))}
            </DropdownMenuContent>
          </DropdownMenu>
        ) : null}
      </SectionHeader>

      <div className="flex flex-wrap items-center gap-2">
        <Select value={status} onValueChange={resetPage(setStatus)}>
          <SelectTrigger size="sm" className="w-32">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">全部状态</SelectItem>
            {STATUS_FILTERS.map((value) => (
              <SelectItem key={value} value={value}>
                {DELIVERY_STATUS_LABEL[value]}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Select value={channelId} onValueChange={resetPage(setChannelId)}>
          <SelectTrigger size="sm" className="w-44">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">全部渠道</SelectItem>
            {channels.map((channel) => (
              <SelectItem key={channel.id} value={String(channel.id)}>
                <ChannelMark kind={channel.kind} size="sm" />
                {channel.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Select value={eventKey} onValueChange={resetPage(setEventKey)}>
          <SelectTrigger size="sm" className="w-48">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">全部事件</SelectItem>
            {events.map((event) => (
              <SelectItem key={event.key} value={event.key}>
                {event.group} · {event.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        {filtered ? (
          <Button
            size="sm"
            variant="ghost"
            onClick={() => {
              setStatus("all");
              setChannelId("all");
              setEventKey("all");
              setPage(1);
            }}
          >
            清除筛选
          </Button>
        ) : null}
        {data ? <span className="ml-auto text-xs tabular-nums text-muted-foreground">共 {data.total} 条</span> : null}
      </div>

      {listQuery.isPending ? (
        <Card className="gap-2 p-4">
          {Array.from({ length: 8 }, (_, index) => (
            <Skeleton key={index} className="h-9 w-full" />
          ))}
        </Card>
      ) : listQuery.isError ? (
        <SectionError error={listQuery.error} onRetry={() => listQuery.refetch()} />
      ) : items.length === 0 ? (
        <SectionEmpty
          icon={History}
          title={filtered ? "没有符合条件的记录" : "暂无投递记录"}
          description={filtered ? "换个筛选条件试试。" : "订阅的事件发生后，每次投递都会记在这里。"}
        />
      ) : (
        <Card className={cn("gap-0 overflow-hidden p-0 transition-opacity", listQuery.isPlaceholderData && "opacity-60")}>
          <Table>
            <TableHeader>
              <TableRow className="bg-muted/30 hover:bg-muted/30">
                <TableHead className="w-24 pl-4">状态</TableHead>
                <TableHead>事件</TableHead>
                <TableHead>渠道</TableHead>
                <TableHead className="hidden lg:table-cell">关联</TableHead>
                <TableHead className="hidden w-16 text-right md:table-cell">次数</TableHead>
                <TableHead className="w-20 text-right">耗时</TableHead>
                <TableHead className="w-32 pr-4 text-right">时间</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {items.map((delivery) => (
                <TableRow
                  key={delivery.id}
                  className="cursor-pointer"
                  onClick={() => setActive(delivery)}
                  data-state={active?.id === delivery.id ? "selected" : undefined}
                >
                  <TableCell className="pl-4">
                    <DeliveryStatusBadge status={delivery.status} />
                  </TableCell>
                  <TableCell className="max-w-56">
                    <p className="truncate text-sm">{eventName.get(delivery.eventKey) ?? delivery.eventKey}</p>
                    {delivery.error ? (
                      <p className="truncate text-[11px] text-red-600 dark:text-red-400" title={delivery.error}>
                        {delivery.error}
                      </p>
                    ) : (
                      <p className="truncate font-mono text-[10px] text-muted-foreground">{delivery.eventKey}</p>
                    )}
                  </TableCell>
                  <TableCell>
                    <span className="inline-flex max-w-44 items-center gap-1.5">
                      <ChannelMark kind={delivery.channelKind} size="sm" />
                      <span className={cn("truncate text-sm", !delivery.channelName && "text-muted-foreground")}>
                        {delivery.channelName || "已删除渠道"}
                      </span>
                    </span>
                  </TableCell>
                  <TableCell className="hidden font-mono text-[11px] text-muted-foreground lg:table-cell">
                    {delivery.resourceId ? `${delivery.resource}#${delivery.resourceId}` : "—"}
                  </TableCell>
                  <TableCell className="hidden text-right tabular-nums text-muted-foreground md:table-cell">{delivery.attempt}</TableCell>
                  <TableCell className="text-right tabular-nums text-muted-foreground">{delivery.latencyMs} ms</TableCell>
                  <TableCell className="pr-4 text-right text-xs text-muted-foreground" title={formatDateTime(delivery.createdAt)}>
                    {formatRelative(delivery.createdAt)}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
          {data && data.totalPages > 1 ? (
            <div className="flex items-center justify-end gap-2 border-t px-4 py-2">
              <span className="text-xs tabular-nums text-muted-foreground">
                第 {data.page} / {data.totalPages} 页
              </span>
              <Button size="icon-sm" variant="outline" disabled={page <= 1} onClick={() => setPage((prev) => prev - 1)} aria-label="上一页">
                <ChevronLeft />
              </Button>
              <Button
                size="icon-sm"
                variant="outline"
                disabled={page >= data.totalPages}
                onClick={() => setPage((prev) => prev + 1)}
                aria-label="下一页"
              >
                <ChevronRight />
              </Button>
            </div>
          ) : null}
        </Card>
      )}

      <Sheet open={active !== null} onOpenChange={(open) => !open && setActive(null)}>
        <SheetContent side="right" className="w-full gap-0 sm:max-w-lg">
          {active ? <DeliveryDetail delivery={active} eventName={eventName.get(active.eventKey)} /> : null}
        </SheetContent>
      </Sheet>

      {confirmDialog}
    </div>
  );
}

function DeliveryDetail({ delivery, eventName }: { delivery: NotifyDelivery; eventName?: string }) {
  return (
    <>
      <SheetHeader className="border-b">
        <SheetTitle className="flex items-center gap-2">
          投递详情
          <DeliveryStatusBadge status={delivery.status} />
        </SheetTitle>
        <SheetDescription>#{delivery.id} · {formatDateTime(delivery.createdAt)}</SheetDescription>
      </SheetHeader>
      <div className="flex-1 space-y-5 overflow-y-auto p-4">
        <dl className="grid grid-cols-[6rem_minmax(0,1fr)] gap-x-3 gap-y-2 text-sm">
          <Row label="事件">
            {eventName ?? delivery.eventKey}
            <span className="ml-1.5 font-mono text-[11px] text-muted-foreground">{delivery.eventKey}</span>
          </Row>
          <Row label="渠道">
            <span className="inline-flex items-center gap-1.5">
              <ChannelMark kind={delivery.channelKind} size="sm" />
              {delivery.channelName || "已删除渠道"}
            </span>
          </Row>
          <Row label="关联">{delivery.resourceId ? <span className="font-mono text-xs">{`${delivery.resource}#${delivery.resourceId}`}</span> : "—"}</Row>
          <Row label="尝试次数">{delivery.attempt}</Row>
          <Row label="耗时">{delivery.latencyMs} ms</Row>
          <Row label="完成时间">{delivery.completedAt ? formatDateTime(delivery.completedAt) : "—"}</Row>
          {delivery.dedupeKey ? (
            <Row label="去重键">
              <span className="break-all font-mono text-xs">{delivery.dedupeKey}</span>
            </Row>
          ) : null}
        </dl>

        {delivery.error ? <Snippet label="错误" tone="danger" value={delivery.error} /> : null}
        <Snippet label="请求" value={delivery.requestSnippet} />
        <Snippet label="响应" value={delivery.responseSnippet} />
      </div>
    </>
  );
}

function Row({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <>
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="min-w-0 text-foreground">{children}</dd>
    </>
  );
}

function Snippet({ label, value, tone }: { label: string; value?: string; tone?: "danger" }) {
  return (
    <div className="space-y-1.5">
      <div className="flex items-center justify-between">
        <p className="text-xs font-medium text-muted-foreground">{label}</p>
        {value ? (
          <Button
            size="sm"
            variant="ghost"
            className="h-6 px-2 text-[11px]"
            onClick={() => {
              void navigator.clipboard.writeText(value).then(() => notify.success("已复制"));
            }}
          >
            复制
          </Button>
        ) : null}
      </div>
      {value ? (
        <pre
          className={cn(
            "max-h-64 overflow-auto whitespace-pre-wrap break-all rounded-lg border p-3 font-mono text-[11px] leading-5",
            tone === "danger" ? "border-red-500/30 bg-red-500/5 text-red-600 dark:text-red-400" : "bg-muted/40 text-foreground"
          )}
        >
          {prettyJson(value)}
        </pre>
      ) : (
        <Badge variant="outline" size="sm">
          无
        </Badge>
      )}
    </div>
  );
}

function prettyJson(value: string) {
  const trimmed = value.trim();
  if (!trimmed.startsWith("{") && !trimmed.startsWith("[")) return value;
  try {
    return JSON.stringify(JSON.parse(trimmed), null, 2);
  } catch {
    return value;
  }
}
