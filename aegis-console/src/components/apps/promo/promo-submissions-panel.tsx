"use client";

import { useMemo, useState } from "react";
import Link from "next/link";
import { AlertTriangle, Check, ClipboardCheck, ExternalLink, RotateCw, X } from "lucide-react";
import { toast } from "sonner";
import {
  REVIEW_REASON_LABEL,
  SUBMISSION_STATUSES,
  SUBMISSION_STATUS_META,
  SubmissionStatusBadge,
  catalogPlatforms,
  platformName,
  promoLimits
} from "@/components/apps/promo/promo-shared";
import {
  ConfirmActionDialog,
  ResultChips,
  ResultList,
  usePaging
} from "@/components/users/detail/records/records-shared";
import {
  FilterBar,
  FilterSelect,
  Pager,
  RecordSheet,
  RecordTable,
  ResetButton,
  SearchField,
  Section,
  StateBlock,
  errorMessage,
  useDebouncedValue,
  type Column
} from "@/components/users/detail/kit";
import { userInitials } from "@/components/users/detail/user-detail-shared";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Progress } from "@/components/ui/progress";
import type { PromoReviewAction, PromoReviewOutcome, PromoSubmission, PromoSubmissionStatus } from "@/lib/api/promo";
import { formatDateTime, formatRelative } from "@/lib/datetime";
import {
  usePromoCampaignsQuery,
  usePromoCatalogQuery,
  usePromoSubmissionsQuery,
  useReviewPromoSubmissionsMutation
} from "@/lib/promo-hooks";

type ReviewRequest = { action: PromoReviewAction; items: PromoSubmission[] } | null;
type Failure = { id: number; who: string; url: string; message: string };

/** 只把 http(s) 链接渲染成可点的链接 */
function isHttpUrl(value: string) {
  try {
    const parsed = new URL(value);
    return parsed.protocol === "http:" || parsed.protocol === "https:";
  } catch {
    return false;
  }
}

/** 列表里显示的链接：主机 + 路径，去掉协议与查询串 */
function shortUrl(value: string) {
  try {
    const parsed = new URL(value);
    return `${parsed.host}${parsed.pathname === "/" ? "" : parsed.pathname}`;
  } catch {
    return value;
  }
}

function whoOf(row: PromoSubmission) {
  return row.userNickname || row.userAccount || `#${row.userId}`;
}

/**
 * 提交审核。
 *
 * 按活动、状态、平台、用户与关键字筛选；待审核的可以单条或勾选后批量「通过」「驳回」。
 * 一批逐条处理、各自成败（`POST /promo/submissions/review` 返回每条结论），失败的条目与原因列在表格上方，
 * 并保持勾选，便于处理后重试。驳回必须写原因，原因随站内信发给用户；被驳回的链接用户可以重新提交。
 * 文章链接在新窗口打开（noopener noreferrer），审核前请先看原文。
 */
export function PromoSubmissionsPanel({
  appKey,
  campaignId,
  status,
  onCampaignIdChange,
  onStatusChange
}: {
  appKey: string;
  campaignId?: number;
  status?: PromoSubmissionStatus;
  onCampaignIdChange: (campaignId?: number) => void;
  onStatusChange: (status?: PromoSubmissionStatus) => void;
}) {
  const [platform, setPlatform] = useState("");
  const [userIdInput, setUserIdInput] = useState("");
  const [keyword, setKeyword] = useState("");
  const paging = usePaging(20);
  const debouncedKeyword = useDebouncedValue(keyword.trim());
  const debouncedUserId = useDebouncedValue(userIdInput.trim());
  const [selectedIds, setSelectedIds] = useState<Set<number>>(new Set());
  const [detail, setDetail] = useState<PromoSubmission | null>(null);
  const [review, setReview] = useState<ReviewRequest>(null);
  const [reason, setReason] = useState("");
  const [progress, setProgress] = useState<{ done: number; total: number } | null>(null);
  const [failures, setFailures] = useState<Failure[]>([]);

  const userId = /^\d+$/.test(debouncedUserId) ? Number(debouncedUserId) : undefined;
  const catalogQuery = usePromoCatalogQuery(appKey);
  const campaignsQuery = usePromoCampaignsQuery(appKey, { page: 1, limit: 100 });
  const listQuery = usePromoSubmissionsQuery(appKey, {
    campaignId,
    status,
    platform: platform || undefined,
    userId,
    keyword: debouncedKeyword || undefined,
    page: paging.page,
    limit: paging.limit
  });
  const reviewMutation = useReviewPromoSubmissionsMutation(appKey);

  const catalog = catalogQuery.data;
  const limits = promoLimits(catalog);
  const rows = useMemo(() => listQuery.data?.items ?? [], [listQuery.data]);
  const pendingRows = rows.filter((row) => row.status === "pending");
  const selectedRows = pendingRows.filter((row) => selectedIds.has(row.id));
  const allPendingSelected = pendingRows.length > 0 && pendingRows.every((row) => selectedIds.has(row.id));
  const filtered = Boolean(keyword || platform || userIdInput || campaignId || status);
  const busy = progress !== null;

  const refilter = () => {
    paging.reset();
    setSelectedIds(new Set());
  };

  const toggle = (id: number, next: boolean) =>
    setSelectedIds((current) => {
      const copy = new Set(current);
      if (next) copy.add(id);
      else copy.delete(id);
      return copy;
    });

  const openReview = (action: PromoReviewAction, items: PromoSubmission[]) => {
    setReason("");
    setProgress(null);
    setReview({ action, items });
  };

  const submitReview = async () => {
    if (!review) return;
    const approve = review.action === "approve";
    const trimmed = reason.trim();
    if (!approve && !trimmed) {
      toast.error("请填写驳回原因");
      return;
    }
    const items = review.items;
    const byId = new Map(items.map((item) => [item.id, item]));
    const outcomes: PromoReviewOutcome[] = [];
    let requestError: unknown = null;
    setProgress({ done: 0, total: items.length });
    // 接口一次最多 maxReviewBatch 条，超出时分批提交
    for (let start = 0; start < items.length; start += limits.maxReviewBatch) {
      const chunk = items.slice(start, start + limits.maxReviewBatch);
      try {
        outcomes.push(
          ...(await reviewMutation.mutateAsync({
            ids: chunk.map((item) => item.id),
            action: review.action,
            reason: approve ? "" : trimmed
          }))
        );
      } catch (error) {
        requestError = error;
        for (const item of chunk) {
          outcomes.push({ id: item.id, ok: false, message: errorMessage(error, "请求失败") });
        }
      }
      setProgress({ done: Math.min(items.length, start + chunk.length), total: items.length });
    }
    setProgress(null);

    const seen = new Set(outcomes.map((outcome) => outcome.id));
    const missing: PromoReviewOutcome[] = items
      .filter((item) => !seen.has(item.id))
      .map((item) => ({ id: item.id, ok: false, message: "没有返回结论" }));
    const all: PromoReviewOutcome[] = [...outcomes, ...missing];
    const succeeded = all.filter((outcome) => outcome.ok);
    const failed = all.filter((outcome) => !outcome.ok);
    const verb = approve ? "通过" : "驳回";

    const nextFailures: Failure[] = failed.map((outcome) => {
      const item = byId.get(outcome.id);
      return {
        id: outcome.id,
        who: item ? whoOf(item) : `#${outcome.id}`,
        url: item?.url ?? "",
        message: outcome.message || (outcome.reason ? REVIEW_REASON_LABEL[outcome.reason] ?? outcome.reason : "失败")
      };
    });
    setFailures(nextFailures);
    // 成功的取消勾选，失败的保留，便于处理后重试
    const failedIds = new Set(failed.map((outcome) => outcome.id));
    setSelectedIds((current) => new Set([...current].filter((id) => failedIds.has(id))));

    if (!failed.length) {
      toast.success(items.length === 1 ? `已${verb}，并通知了用户` : `已${verb} ${succeeded.length} 条`);
      setReview(null);
    } else if (succeeded.length) {
      toast.warning(`已${verb} ${succeeded.length} 条，${failed.length} 条失败，原因见列表上方`);
      setReview(null);
    } else if (requestError && items.length === 1) {
      toast.error(errorMessage(requestError, `${verb}失败`));
    } else {
      toast.error(items.length === 1 ? `${verb}失败：${nextFailures[0]?.message ?? ""}` : `${failed.length} 条全部${verb}失败，原因见列表上方`);
      setReview(null);
    }

    if (detail) {
      const outcome = all.find((item) => item.id === detail.id);
      if (outcome?.ok) {
        setDetail({
          ...detail,
          status: outcome.status ?? (approve ? "approved" : "rejected"),
          rejectReason: approve ? detail.rejectReason : trimmed,
          results: outcome.results ?? detail.results
        });
      }
    }
  };

  const userHref = (id: number) => `/app-users/${encodeURIComponent(appKey)}/${id}`;

  const columns: Array<Column<PromoSubmission>> = [
    {
      id: "select",
      width: "36px",
      header: (
        <Checkbox
          checked={allPendingSelected ? true : selectedRows.length ? "indeterminate" : false}
          disabled={!pendingRows.length}
          onCheckedChange={(next) => setSelectedIds(next ? new Set(pendingRows.map((row) => row.id)) : new Set())}
          aria-label="选择本页待审核"
        />
      ),
      cell: (row) =>
        row.status === "pending" ? (
          <span onClick={(event) => event.stopPropagation()}>
            <Checkbox checked={selectedIds.has(row.id)} onCheckedChange={(next) => toggle(row.id, Boolean(next))} aria-label="选择" />
          </span>
        ) : null
    },
    {
      id: "user",
      header: "用户",
      cell: (row) => (
        <Link
          href={userHref(row.userId)}
          onClick={(event) => event.stopPropagation()}
          className="group inline-flex min-w-0 items-center gap-2"
        >
          <Avatar className="size-7">
            {row.userAvatar ? <AvatarImage src={row.userAvatar} alt="" /> : null}
            <AvatarFallback className="text-xs">{userInitials(row.userNickname, row.userAccount)}</AvatarFallback>
          </Avatar>
          <span className="flex min-w-0 flex-col">
            <span className="truncate text-sm font-medium group-hover:underline">{whoOf(row)}</span>
            <span className="truncate text-xs text-muted-foreground">
              {row.userNickname && row.userAccount ? row.userAccount : `ID ${row.userId}`}
            </span>
          </span>
        </Link>
      )
    },
    {
      id: "article",
      header: "文章",
      cell: (row) => (
        <div className="flex max-w-72 min-w-0 flex-col gap-0.5">
          <div className="flex min-w-0 items-center gap-1.5">
            <Badge variant="outline" size="sm" className="shrink-0 font-normal">
              {row.platformName || platformName(row.platform, catalog)}
            </Badge>
            {isHttpUrl(row.url) ? (
              <a
                href={row.url}
                target="_blank"
                rel="noopener noreferrer"
                onClick={(event) => event.stopPropagation()}
                className="inline-flex min-w-0 items-center gap-1 text-xs text-primary hover:underline"
                title={row.url}
              >
                <span className="truncate font-mono">{shortUrl(row.url)}</span>
                <ExternalLink className="size-3 shrink-0" />
              </a>
            ) : (
              <span className="truncate font-mono text-xs" title={row.url}>
                {row.url}
              </span>
            )}
          </div>
          {row.note ? (
            <span className="truncate text-xs text-muted-foreground" title={row.note}>
              {row.note}
            </span>
          ) : null}
        </div>
      )
    },
    {
      id: "campaign",
      header: "活动",
      hideBelow: "xl",
      cell: (row) => <span className="line-clamp-2 text-xs">{row.campaignTitle || `#${row.campaignId}`}</span>
    },
    {
      id: "status",
      header: "状态",
      cell: (row) => (
        <div className="flex flex-col items-start gap-0.5">
          <SubmissionStatusBadge status={row.status} />
          {row.reviewedBy ? <span className="text-xs text-muted-foreground">{row.reviewedBy}</span> : null}
        </div>
      )
    },
    {
      id: "outcome",
      header: "结论",
      hideBelow: "lg",
      cell: (row) =>
        row.status === "rejected" ? (
          <span className="line-clamp-2 max-w-56 text-xs text-red-600 dark:text-red-400" title={row.rejectReason}>
            {row.rejectReason || "—"}
          </span>
        ) : row.status === "approved" ? (
          <ResultChips results={row.results} />
        ) : (
          <span className="text-xs text-muted-foreground">—</span>
        )
    },
    {
      id: "time",
      header: "提交时间",
      align: "right",
      hideBelow: "md",
      cell: (row) => (
        <div className="flex flex-col items-end">
          <span className="whitespace-nowrap tabular-nums">{formatDateTime(row.createdAt)}</span>
          <span className="text-xs text-muted-foreground">{formatRelative(row.createdAt)}</span>
        </div>
      )
    },
    {
      id: "actions",
      header: "",
      align: "right",
      width: "120px",
      cell: (row) =>
        row.status === "pending" ? (
          <div className="flex justify-end gap-1.5" onClick={(event) => event.stopPropagation()}>
            <Button size="xs" variant="outline" onClick={() => openReview("approve", [row])}>
              通过
            </Button>
            <Button size="xs" variant="ghost" className="text-muted-foreground" onClick={() => openReview("reject", [row])}>
              驳回
            </Button>
          </div>
        ) : null
    }
  ];

  const approving = review?.action === "approve";
  const single = review?.items.length === 1 ? review.items[0] : null;

  return (
    <div className="flex flex-col gap-4">
      {failures.length ? (
        <div className="flex flex-col gap-2 rounded-xl border border-red-200 bg-red-50 px-4 py-3 dark:border-red-900/60 dark:bg-red-950/40">
          <div className="flex items-center gap-2">
            <AlertTriangle className="size-4 shrink-0 text-red-600 dark:text-red-400" />
            <span className="text-sm font-medium text-red-700 dark:text-red-300">{failures.length} 条审核未成功</span>
            <Button
              size="icon-xs"
              variant="ghost"
              className="ml-auto text-muted-foreground"
              aria-label="关闭"
              onClick={() => setFailures([])}
            >
              <X />
            </Button>
          </div>
          <ul className="flex max-h-48 flex-col gap-1 overflow-y-auto text-xs">
            {failures.map((failure) => (
              <li key={failure.id} className="flex min-w-0 flex-wrap items-baseline gap-x-2">
                <span className="font-mono text-muted-foreground">#{failure.id}</span>
                <span className="font-medium">{failure.who}</span>
                {failure.url ? (
                  <span className="max-w-72 truncate font-mono text-muted-foreground" title={failure.url}>
                    {shortUrl(failure.url)}
                  </span>
                ) : null}
                <span className="text-red-700 dark:text-red-300">{failure.message}</span>
              </li>
            ))}
          </ul>
        </div>
      ) : null}

      <Section
        flush
        title="提交审核"
        icon={ClipboardCheck}
        tone="primary"
        description="先打开原文核对，再通过或驳回；结论会以站内信通知用户"
        actions={
          <Button variant="ghost" size="icon-sm" aria-label="刷新" onClick={() => listQuery.refetch()}>
            <RotateCw className={listQuery.isFetching ? "animate-spin" : undefined} />
          </Button>
        }
        toolbar={
          selectedRows.length ? (
            <div className="flex flex-wrap items-center gap-2">
              <span className="text-sm font-medium tabular-nums">已选 {selectedRows.length} 条待审核</span>
              <div className="mx-1 h-4 w-px bg-border" />
              <Button size="sm" onClick={() => openReview("approve", selectedRows)}>
                <Check />
                批量通过
              </Button>
              <Button
                size="sm"
                variant="outline"
                className="text-red-600 dark:text-red-400"
                onClick={() => openReview("reject", selectedRows)}
              >
                <X />
                批量驳回
              </Button>
              <Button size="sm" variant="ghost" className="ml-auto text-muted-foreground" onClick={() => setSelectedIds(new Set())}>
                取消选择
              </Button>
            </div>
          ) : (
            <FilterBar>
              <SearchField
                value={keyword}
                onChange={(next) => {
                  setKeyword(next);
                  refilter();
                }}
                placeholder="搜索链接、备注、账号"
              />
              <FilterSelect
                label="活动"
                allLabel="全部活动"
                value={campaignId ? String(campaignId) : ""}
                onChange={(next) => {
                  onCampaignIdChange(next ? Number(next) : undefined);
                  refilter();
                }}
                options={(campaignsQuery.data?.items ?? []).map((item) => ({ value: String(item.id), label: item.title }))}
                className="max-w-[240px]"
              />
              <FilterSelect
                label="状态"
                allLabel="全部"
                value={status ?? ""}
                onChange={(next) => {
                  onStatusChange((next || undefined) as PromoSubmissionStatus | undefined);
                  refilter();
                }}
                options={SUBMISSION_STATUSES.map((value) => ({ value, label: SUBMISSION_STATUS_META[value].label }))}
              />
              <FilterSelect
                label="平台"
                allLabel="全部"
                value={platform}
                onChange={(next) => {
                  setPlatform(next);
                  refilter();
                }}
                options={catalogPlatforms(catalog).map((item) => ({ value: item.key, label: item.name }))}
              />
              <Input
                value={userIdInput}
                inputMode="numeric"
                placeholder="用户 ID"
                className="h-8 w-28 text-sm"
                onChange={(event) => {
                  setUserIdInput(event.target.value.replace(/\D/g, ""));
                  refilter();
                }}
              />
              <ResetButton
                visible={filtered}
                onClick={() => {
                  setKeyword("");
                  setPlatform("");
                  setUserIdInput("");
                  onCampaignIdChange(undefined);
                  onStatusChange(undefined);
                  refilter();
                }}
              />
            </FilterBar>
          )
        }
        footer={
          listQuery.data?.total ? (
            <Pager
              page={paging.page}
              limit={paging.limit}
              total={listQuery.data.total}
              onPage={(page) => {
                paging.setPage(page);
                setSelectedIds(new Set());
              }}
              onLimit={(limit) => {
                paging.setLimit(limit);
                setSelectedIds(new Set());
              }}
            />
          ) : null
        }
      >
        <RecordTable
          columns={columns}
          rows={rows}
          rowKey={(row) => row.id}
          loading={listQuery.isLoading}
          fetching={listQuery.isFetching}
          error={listQuery.error}
          onRetry={() => listQuery.refetch()}
          onRowClick={setDetail}
          rowClassName={(row) => (selectedIds.has(row.id) ? "bg-primary/5" : undefined)}
          minWidth={880}
          empty={
            <StateBlock
              icon={ClipboardCheck}
              title={filtered ? "没有符合条件的提交" : "暂无提交"}
              description={filtered ? undefined : "用户在分享任务里提交的文章链接会出现在这里"}
            />
          }
        />
      </Section>

      <RecordSheet
        open={Boolean(detail)}
        onOpenChange={(open) => (!open ? setDetail(null) : undefined)}
        title="提交详情"
        description={detail ? `编号 ${detail.id}，${formatDateTime(detail.createdAt)}` : undefined}
        hero={
          detail ? (
            <div className="flex flex-col gap-3 rounded-xl border p-4">
              <div className="flex flex-wrap items-center gap-2">
                <Badge variant="outline" size="sm" className="font-normal">
                  {detail.platformName || platformName(detail.platform, catalog)}
                </Badge>
                <SubmissionStatusBadge status={detail.status} />
              </div>
              {isHttpUrl(detail.url) ? (
                <a
                  href={detail.url}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="inline-flex items-start gap-1.5 font-mono text-xs break-all text-primary hover:underline"
                >
                  {detail.url}
                  <ExternalLink className="mt-0.5 size-3.5 shrink-0" />
                </a>
              ) : (
                <span className="font-mono text-xs break-all">{detail.url}</span>
              )}
              {detail.note ? <p className="text-sm whitespace-pre-wrap">{detail.note}</p> : null}
              <div className="flex items-center justify-between gap-3 border-t pt-3 text-sm">
                <div className="flex min-w-0 flex-col">
                  <span className="truncate font-medium">{whoOf(detail)}</span>
                  <span className="truncate text-xs text-muted-foreground">
                    {detail.userAccount ? `${detail.userAccount}，` : ""}ID {detail.userId}
                  </span>
                </div>
                <Button asChild variant="outline" size="sm">
                  <Link href={userHref(detail.userId)}>用户详情</Link>
                </Button>
              </div>
            </div>
          ) : null
        }
        fields={
          detail
            ? [
                { label: "活动", value: detail.campaignTitle || `#${detail.campaignId}` },
                { label: "提交时间", value: formatDateTime(detail.createdAt) },
                { label: "审核人", value: detail.reviewedBy },
                { label: "审核时间", value: detail.reviewedAt ? formatDateTime(detail.reviewedAt) : undefined },
                { label: "驳回原因", value: detail.rejectReason, wide: true },
                { label: "设备", value: detail.deviceId, mono: true, copy: detail.deviceId || null },
                { label: "IP", value: detail.clientIp, mono: true, copy: detail.clientIp || null }
              ]
            : undefined
        }
        raw={detail}
        actions={
          detail?.status === "pending" ? (
            <>
              <Button variant="outline" size="sm" onClick={() => openReview("reject", [detail])}>
                <X />
                驳回
              </Button>
              <Button size="sm" onClick={() => openReview("approve", [detail])}>
                <Check />
                通过
              </Button>
            </>
          ) : undefined
        }
      >
        {detail ? <ResultList results={detail.results} title="发放结果" /> : null}
      </RecordSheet>

      <ConfirmActionDialog
        open={Boolean(review)}
        onOpenChange={(open) => (!open && !busy ? setReview(null) : undefined)}
        title={
          single
            ? approving
              ? "通过这条提交"
              : "驳回这条提交"
            : approving
              ? `批量通过 ${review?.items.length ?? 0} 条`
              : `批量驳回 ${review?.items.length ?? 0} 条`
        }
        description={
          approving
            ? single
              ? `通过 ${whoOf(single)} 提交的文章，按活动配置发放每篇奖励并发送站内信。超出通过篇数上限的不能通过。`
              : "逐条通过并发放奖励，每位用户都会收到站内信；超出通过篇数上限的条目会失败并列出原因。"
            : single
              ? `驳回 ${whoOf(single)} 提交的文章，原因会通过站内信发给用户，用户可以重新提交这个链接。`
              : "逐条驳回，原因相同，会通过站内信发给每位用户；被驳回的链接用户可以重新提交。"
        }
        confirmLabel={approving ? "确认通过" : "确认驳回"}
        destructive={!approving}
        pending={busy}
        note={
          approving
            ? undefined
            : {
                value: reason,
                onChange: setReason,
                placeholder: "例如：文章未提及应用名称",
                hint: "必填，写给用户看",
                max: limits.maxRejectReasonLength,
                required: true
              }
        }
        onConfirm={() => void submitReview()}
      >
        {progress && progress.total > limits.maxReviewBatch ? (
          <div className="flex flex-col gap-1.5">
            <Progress value={(progress.done / progress.total) * 100} />
            <span className="text-xs tabular-nums text-muted-foreground">
              {progress.done} / {progress.total}
            </span>
          </div>
        ) : null}
      </ConfirmActionDialog>
    </div>
  );
}
