"use client";

import { Suspense, useCallback, useMemo, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import {
  CheckCircle2,
  ImageIcon,
  Inbox,
  MessageSquareText,
  Paperclip,
  RefreshCw,
  Search,
  Sparkles,
  UserRoundPen
} from "lucide-react";
import { SectionHeading } from "@/components/ui/section-heading";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { EmptyState, LoadingState } from "@/components/ui/data-state";
import { WidgetBoundary } from "@/components/ui/error-boundary";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { useAdminAppsQuery } from "@/lib/admin-hooks";
import {
  useTicketCategoriesQuery,
  useTicketMetadataQuery,
  useTicketStatsQuery,
  useTicketsQuery
} from "@/lib/ticket-hooks";
import type { TicketItem, TicketListParams } from "@/lib/api/tickets";
import { TicketDetailSheet } from "@/components/tickets/ticket-detail-sheet";
import {
  FeedbackCategoryBadge,
  MetricTile,
  StatusBadge,
  formatClientShort,
  formatDateTime,
  formatRelativeTime,
  getTicketClient,
  isImageAttachment
} from "@/components/tickets/ticket-shared";

// 意见反馈。
//
// 反馈就是 kind=feedback 的工单：回复、改状态、关闭全部复用工单接口与详情抽屉，
// 这里只提供面向反馈的列表视图（分类、提交端、图片数），并与工单中心互不混排。
//
// URL 约定：/feedback?id=123 直接打开某条反馈详情。

export default function FeedbackPage() {
  return (
    <Suspense fallback={<LoadingState title="加载意见反馈" />}>
      <WidgetBoundary title="意见反馈加载失败">
        <FeedbackPageInner />
      </WidgetBoundary>
    </Suspense>
  );
}

function FeedbackPageInner() {
  const searchParams = useSearchParams();
  const router = useRouter();
  const detailId = searchParams.get("id");
  const selectedId = detailId ? Number(detailId) : null;

  const openDetail = useCallback(
    (id: number) => router.replace(`/feedback?id=${id}`, { scroll: false }),
    [router]
  );
  const closeDetail = useCallback(() => router.replace("/feedback", { scroll: false }), [router]);

  const metadataQuery = useTicketMetadataQuery();
  const appsQuery = useAdminAppsQuery();

  const [keyword, setKeyword] = useState("");
  const [status, setStatus] = useState("active");
  const [appId, setAppId] = useState("all");
  const [categoryId, setCategoryId] = useState("all");
  const [page, setPage] = useState(1);

  const numericAppId = appId === "all" ? undefined : Number(appId);
  const statsQuery = useTicketStatsQuery(numericAppId, "feedback");
  const stats = statsQuery.data;

  // 指定应用时后端返回「该应用 + 平台级」分类；未指定时只有平台级，自建的应用分类需先选应用
  const categoriesQuery = useTicketCategoriesQuery(numericAppId ?? 0);
  const categories = useMemo(
    () => (categoriesQuery.data ?? []).filter((item) => item.kind === "feedback"),
    [categoriesQuery.data]
  );
  const categoryKeyById = useMemo(
    () => new Map(categories.map((item) => [item.id, item.key])),
    [categories]
  );

  const params = useMemo<TicketListParams>(() => {
    const query: TicketListParams = {
      kind: "feedback",
      keyword: keyword.trim() || undefined,
      appid: numericAppId,
      categoryId: categoryId === "all" ? undefined : Number(categoryId),
      page,
      limit: 20
    };
    if (status === "all") query.includeClosed = true;
    else if (status !== "active") query.status = status;
    return query;
  }, [keyword, status, numericAppId, categoryId, page]);

  const listQuery = useTicketsQuery(params);
  const items = listQuery.data?.items ?? [];
  const scope = listQuery.data?.scope;
  const statuses = metadataQuery.data?.statuses ?? [];

  const resetPage = <T,>(setter: (value: T) => void) => (value: T) => {
    setter(value);
    setPage(1);
  };

  return (
    <div className="space-y-5">
      <SectionHeading
        eyebrow="服务台"
        title="意见反馈"
        action={
          <Button
            size="sm"
            variant="outline"
            onClick={() => {
              statsQuery.refetch();
              listQuery.refetch();
            }}
          >
            <RefreshCw className="mr-1 size-3.5" />
            刷新
          </Button>
        }
      />

      {stats ? (
        <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-5">
          <MetricTile label="待受理" value={stats.open} icon={<Inbox className="size-4" />} highlight />
          <MetricTile label="处理中" value={stats.processing} icon={<MessageSquareText className="size-4" />} />
          <MetricTile label="待用户补充" value={stats.pendingUser} icon={<UserRoundPen className="size-4" />} />
          <MetricTile label="今日新增" value={stats.createdToday} icon={<Sparkles className="size-4" />} />
          <MetricTile label="已解决" value={stats.resolved} icon={<CheckCircle2 className="size-4" />} />
        </div>
      ) : statsQuery.isLoading ? (
        <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-5">
          {Array.from({ length: 5 }, (_, index) => (
            <div key={index} className="h-[66px] animate-pulse rounded-xl border bg-muted/50" />
          ))}
        </div>
      ) : null}

      {scope && scope.level !== "all" ? (
        <p className="rounded-lg border border-dashed px-3 py-2 text-xs text-muted-foreground">{scope.label}</p>
      ) : null}

      <div className="flex flex-wrap items-center gap-2">
        <div className="relative min-w-52 flex-1">
          <Search className="absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input
            value={keyword}
            onChange={(event) => resetPage(setKeyword)(event.target.value)}
            placeholder="搜索编号、标题、反馈人或内容"
            className="h-9 pl-8"
          />
        </div>
        <Select
          value={appId}
          onValueChange={(value) => {
            setAppId(value);
            // 切换应用后原分类可能不在新列表里，一并复位
            setCategoryId("all");
            setPage(1);
          }}
        >
          <SelectTrigger className="h-9 w-40">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">全部应用</SelectItem>
            {(appsQuery.data ?? []).map((app) => (
              <SelectItem key={app.id} value={String(app.id)}>
                {app.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Select value={categoryId} onValueChange={resetPage(setCategoryId)}>
          <SelectTrigger className="h-9 w-36">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">全部分类</SelectItem>
            {categories.map((category) => (
              <SelectItem key={category.id} value={String(category.id)}>
                {category.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Select value={status} onValueChange={resetPage(setStatus)}>
          <SelectTrigger className="h-9 w-36">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="active">未结单</SelectItem>
            <SelectItem value="all">全部状态</SelectItem>
            {statuses.map((item) => (
              <SelectItem key={item.value} value={item.value}>
                {item.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      {listQuery.isLoading ? (
        <div className="space-y-1">
          {Array.from({ length: 6 }, (_, index) => (
            <div key={index} className="h-[66px] animate-pulse rounded-xl border bg-muted/50" />
          ))}
        </div>
      ) : items.length === 0 ? (
        <EmptyState title="暂无反馈" />
      ) : (
        <div className="space-y-1">
          {items.map((item) => (
            <FeedbackRow
              key={item.id}
              item={item}
              categoryKey={item.categoryId ? categoryKeyById.get(item.categoryId) : undefined}
              onOpen={openDetail}
            />
          ))}

          {(listQuery.data?.totalPages ?? 1) > 1 ? (
            <div className="flex items-center justify-center gap-2 pt-2">
              <Button size="sm" variant="outline" disabled={page <= 1} onClick={() => setPage((prev) => prev - 1)}>
                上一页
              </Button>
              <span className="text-xs text-muted-foreground">
                {listQuery.data?.page} / {listQuery.data?.totalPages}（共 {listQuery.data?.total} 条）
              </span>
              <Button
                size="sm"
                variant="outline"
                disabled={page >= (listQuery.data?.totalPages ?? 1)}
                onClick={() => setPage((prev) => prev + 1)}
              >
                下一页
              </Button>
            </div>
          ) : null}
        </div>
      )}

      <TicketDetailSheet ticketId={selectedId} onClose={closeDetail} />
    </div>
  );
}

/** 列表接口返回计数时直接用；否则按附件数组统计（旧后端两者都没有时不展示） */
function countAttachments(item: TicketItem) {
  if (item.imageCount !== undefined || item.attachmentCount !== undefined) {
    const images = item.imageCount ?? 0;
    return { images, files: Math.max(0, (item.attachmentCount ?? images) - images) };
  }
  const list = item.attachments ?? [];
  const images = list.filter(isImageAttachment).length;
  return { images, files: list.length - images };
}

function FeedbackRow({
  item,
  categoryKey,
  onOpen
}: {
  item: TicketItem;
  categoryKey?: string;
  onOpen: (id: number) => void;
}) {
  const client = formatClientShort(getTicketClient(item.metadata));
  const { images, files } = countAttachments(item);

  return (
    <Card className="transition-colors hover:border-primary/40">
      <CardContent className="p-0">
        <button
          type="button"
          className="flex w-full items-start gap-3 p-3 text-left"
          onClick={() => onOpen(item.id)}
        >
          <div className="min-w-0 flex-1 space-y-1">
            <div className="flex flex-wrap items-center gap-1.5">
              <FeedbackCategoryBadge name={item.categoryName} categoryKey={categoryKey} />
              <span className="truncate text-sm font-medium text-foreground">{item.title}</span>
            </div>
            <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-[11px] text-muted-foreground">
              <span>{item.requesterName}</span>
              {item.appName ? <span>{item.appName}</span> : null}
              {client ? <span>{client}</span> : null}
              {images > 0 ? (
                <span className="inline-flex items-center gap-1">
                  <ImageIcon className="size-3" />
                  {images} 张图片
                </span>
              ) : null}
              {files > 0 ? (
                <span className="inline-flex items-center gap-1">
                  <Paperclip className="size-3" />
                  {files} 个附件
                </span>
              ) : null}
              <span>{item.messageCount} 条会话</span>
              <span className="font-mono">{item.ticketNo}</span>
            </div>
          </div>
          <div className="shrink-0 space-y-1 text-right">
            <StatusBadge status={item.status} />
            <p className="text-[11px] text-muted-foreground" title={formatDateTime(item.createdAt)}>
              {formatRelativeTime(item.createdAt)}
            </p>
          </div>
        </button>
      </CardContent>
    </Card>
  );
}
