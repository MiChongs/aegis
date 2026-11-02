"use client";

import { useMemo, useState } from "react";
import {
  BarChart3,
  ClipboardCheck,
  Megaphone,
  MoreHorizontal,
  Pencil,
  Plus,
  Power,
  PowerOff,
  RotateCw,
  Share2,
  Trash2,
  UserPlus
} from "lucide-react";
import { toast } from "sonner";
import { PromoCampaignEditor, type PromoEditorTarget } from "@/components/apps/promo/promo-campaign-editor";
import {
  PhaseBadge,
  STATUSES,
  STATUS_META,
  StatusBadge,
  formatCount,
  platformName
} from "@/components/apps/promo/promo-shared";
import { ConfirmActionDialog, usePaging } from "@/components/users/detail/records/records-shared";
import {
  FilterBar,
  FilterSelect,
  Pager,
  RecordTable,
  ResetButton,
  SearchField,
  Section,
  StateBlock,
  errorMessage,
  useDebouncedValue,
  type Column
} from "@/components/users/detail/kit";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu";
import type { PromoAdminCampaign, PromoStatus, PromoSubmissionStatus } from "@/lib/api/promo";
import { formatDateTimeMinute } from "@/lib/datetime";
import {
  useDeletePromoCampaignMutation,
  usePromoCampaignsQuery,
  usePromoCatalogQuery,
  useSetPromoCampaignStatusMutation
} from "@/lib/promo-hooks";

type StatusRequest = { campaign: PromoAdminCampaign; status: PromoStatus } | null;

/**
 * 活动列表。
 *
 * 一行一期活动：时间与阶段（未开始 / 进行中 / 已结束）、运营设置的状态（草稿 / 上线 / 下线）、
 * 开了哪些任务、简要统计。上下线与删除都二次确认；能不能删由后端判定（上线中、已有人领取或提交过的不能删），
 * 被拒时原样显示后端的原因。点一行打开编辑。
 */
export function PromoCampaignsPanel({
  appKey,
  onOpenSubmissions,
  onOpenStats
}: {
  appKey: string;
  onOpenSubmissions: (campaignId: number, status?: PromoSubmissionStatus) => void;
  onOpenStats: (campaignId: number) => void;
}) {
  const [status, setStatus] = useState<PromoStatus | "">("");
  const [keyword, setKeyword] = useState("");
  const paging = usePaging(20);
  const debouncedKeyword = useDebouncedValue(keyword.trim());
  const [editor, setEditor] = useState<PromoEditorTarget | null>(null);
  const [statusRequest, setStatusRequest] = useState<StatusRequest>(null);
  const [pendingDelete, setPendingDelete] = useState<PromoAdminCampaign | null>(null);

  const listQuery = usePromoCampaignsQuery(appKey, {
    status: status || undefined,
    keyword: debouncedKeyword || undefined,
    page: paging.page,
    limit: paging.limit
  });
  const catalogQuery = usePromoCatalogQuery(appKey);
  const statusMutation = useSetPromoCampaignStatusMutation(appKey);
  const deleteMutation = useDeletePromoCampaignMutation(appKey);

  const rows = useMemo(() => listQuery.data?.items ?? [], [listQuery.data]);
  const filtered = Boolean(keyword || status);
  const catalog = catalogQuery.data;

  const confirmStatus = async () => {
    if (!statusRequest) return;
    const { campaign, status: next } = statusRequest;
    try {
      await statusMutation.mutateAsync({ id: campaign.id, status: next });
      toast.success(next === "online" ? `「${campaign.title}」已上线` : `「${campaign.title}」已下线`);
      setStatusRequest(null);
    } catch (error) {
      toast.error(errorMessage(error, "操作失败"));
    }
  };

  const confirmDelete = async () => {
    if (!pendingDelete) return;
    try {
      await deleteMutation.mutateAsync(pendingDelete.id);
      toast.success(`已删除「${pendingDelete.title}」`);
      setPendingDelete(null);
    } catch (error) {
      toast.error(errorMessage(error, "删除失败"));
    }
  };

  const columns: Array<Column<PromoAdminCampaign>> = [
    {
      id: "title",
      header: "活动",
      cell: (row) => (
        <div className="flex min-w-0 flex-col gap-0.5">
          <span className="truncate font-medium">{row.title || "未命名"}</span>
          <span className="truncate text-xs text-muted-foreground">
            {row.subtitle ? row.subtitle : `编号 ${row.id}`}
          </span>
        </div>
      )
    },
    {
      id: "period",
      header: "时间",
      cell: (row) => (
        <div className="flex flex-col gap-1">
          <span className="text-xs whitespace-nowrap tabular-nums">
            {formatDateTimeMinute(row.startsAt)}
            <span className="text-muted-foreground"> 至</span>
          </span>
          <span className="text-xs whitespace-nowrap tabular-nums">{formatDateTimeMinute(row.endsAt)}</span>
        </div>
      )
    },
    {
      id: "state",
      header: "状态",
      cell: (row) => (
        <div className="flex flex-wrap items-center gap-1">
          <StatusBadge status={row.status} />
          {row.phase ? <PhaseBadge phase={row.phase} /> : null}
        </div>
      )
    },
    {
      id: "tasks",
      header: "任务",
      hideBelow: "md",
      cell: (row) => {
        const invite = row.config?.invite;
        const share = row.config?.share;
        const platforms = share?.platforms ?? [];
        return (
          <div className="flex flex-wrap gap-1">
            {invite?.enabled ? (
              <Badge variant="outline" size="sm" className="gap-1 font-normal">
                <UserPlus className="size-3" />
                邀请 {invite.tiers?.length ?? 0} 档
              </Badge>
            ) : null}
            {share?.enabled ? (
              <Badge
                variant="outline"
                size="sm"
                className="gap-1 font-normal"
                title={platforms.map((key) => platformName(key, catalog)).join("、")}
              >
                <Share2 className="size-3" />
                分享 {platforms.length} 个平台
              </Badge>
            ) : null}
            {!invite?.enabled && !share?.enabled ? <span className="text-xs text-muted-foreground">未设任务</span> : null}
          </div>
        );
      }
    },
    {
      id: "stats",
      header: "统计",
      hideBelow: "lg",
      cell: (row) => (
        <div className="grid grid-cols-2 gap-x-3 gap-y-0.5 text-xs whitespace-nowrap tabular-nums">
          <span>
            <span className="text-muted-foreground">参与 </span>
            {formatCount(row.participants)}
          </span>
          <span>
            <span className="text-muted-foreground">领取 </span>
            {formatCount(row.tierClaims)}
          </span>
          <button
            type="button"
            className="text-left hover:underline disabled:no-underline"
            disabled={!row.pendingSubmissions}
            onClick={(event) => {
              event.stopPropagation();
              onOpenSubmissions(row.id, "pending");
            }}
          >
            <span className="text-muted-foreground">待审 </span>
            <span className={row.pendingSubmissions ? "font-medium text-amber-600 dark:text-amber-400" : undefined}>
              {formatCount(row.pendingSubmissions)}
            </span>
          </button>
          <span>
            <span className="text-muted-foreground">通过 </span>
            {formatCount(row.approvedSubmissions)}
          </span>
        </div>
      )
    },
    {
      id: "actions",
      header: "",
      align: "right",
      width: "120px",
      cell: (row) => (
        <div className="flex justify-end gap-1" onClick={(event) => event.stopPropagation()}>
          <Button size="xs" variant="ghost" onClick={() => setEditor({ mode: "edit", campaign: row })}>
            <Pencil />
            编辑
          </Button>
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button size="icon-xs" variant="ghost" aria-label="更多操作">
                <MoreHorizontal />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-40">
              {row.status === "online" ? (
                <DropdownMenuItem onSelect={() => setStatusRequest({ campaign: row, status: "offline" })}>
                  <PowerOff />
                  下线
                </DropdownMenuItem>
              ) : (
                <DropdownMenuItem onSelect={() => setStatusRequest({ campaign: row, status: "online" })}>
                  <Power />
                  上线
                </DropdownMenuItem>
              )}
              <DropdownMenuItem onSelect={() => onOpenStats(row.id)}>
                <BarChart3 />
                活动统计
              </DropdownMenuItem>
              <DropdownMenuItem onSelect={() => onOpenSubmissions(row.id)}>
                <ClipboardCheck />
                提交审核
              </DropdownMenuItem>
              <DropdownMenuSeparator />
              <DropdownMenuItem variant="destructive" onSelect={() => setPendingDelete(row)}>
                <Trash2 />
                删除
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      )
    }
  ];

  const statusTarget = statusRequest?.campaign;
  const goingOnline = statusRequest?.status === "online";

  return (
    <>
      <Section
        flush
        title="活动"
        icon={Megaphone}
        tone="primary"
        description="用户端只看得到上线的活动；点开一行编辑"
        actions={
          <>
            <Button variant="ghost" size="icon-sm" aria-label="刷新" onClick={() => listQuery.refetch()}>
              <RotateCw className={listQuery.isFetching ? "animate-spin" : undefined} />
            </Button>
            <Button size="sm" onClick={() => setEditor({ mode: "create" })}>
              <Plus />
              新建活动
            </Button>
          </>
        }
        toolbar={
          <FilterBar>
            <SearchField
              value={keyword}
              onChange={(next) => {
                setKeyword(next);
                paging.reset();
              }}
              placeholder="搜索标题"
            />
            <FilterSelect
              label="状态"
              allLabel="全部"
              value={status}
              onChange={(next) => {
                setStatus(next as PromoStatus | "");
                paging.reset();
              }}
              options={STATUSES.map((value) => ({ value, label: STATUS_META[value].label }))}
            />
            <ResetButton
              visible={filtered}
              onClick={() => {
                setKeyword("");
                setStatus("");
                paging.reset();
              }}
            />
          </FilterBar>
        }
        footer={
          listQuery.data?.total ? (
            <Pager
              page={paging.page}
              limit={paging.limit}
              total={listQuery.data.total}
              onPage={paging.setPage}
              onLimit={paging.setLimit}
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
          onRowClick={(row) => setEditor({ mode: "edit", campaign: row })}
          minWidth={820}
          empty={
            <StateBlock
              icon={Megaphone}
              title={filtered ? "没有符合条件的活动" : "暂无活动"}
              description={filtered ? undefined : "一期活动可以设邀请好友与分享推广两项任务，也可以只开一项"}
              action={
                filtered ? undefined : (
                  <Button size="sm" onClick={() => setEditor({ mode: "create" })}>
                    <Plus />
                    新建活动
                  </Button>
                )
              }
            />
          }
        />
      </Section>

      <PromoCampaignEditor appKey={appKey} target={editor} onClose={() => setEditor(null)} />

      <ConfirmActionDialog
        open={Boolean(statusRequest)}
        onOpenChange={(open) => (!open ? setStatusRequest(null) : undefined)}
        title={goingOnline ? `上线「${statusTarget?.title ?? ""}」` : `下线「${statusTarget?.title ?? ""}」`}
        description={
          goingOnline
            ? "上线后用户端即可看到这期活动，到开始时间后可以参与。"
            : "下线后用户端不再展示，活动按已结束处理：不能再领取档位或提交文章，已发放的奖励不受影响，待审核的提交仍可在这里审核。"
        }
        confirmLabel={goingOnline ? "确认上线" : "确认下线"}
        destructive={!goingOnline}
        pending={statusMutation.isPending}
        onConfirm={() => void confirmStatus()}
      />

      <ConfirmActionDialog
        open={Boolean(pendingDelete)}
        onOpenChange={(open) => (!open ? setPendingDelete(null) : undefined)}
        title={`删除「${pendingDelete?.title ?? ""}」`}
        description="只有草稿与下线的活动可以删除；已经有人领取档位或提交过文章的活动不能删除，只能下线。此操作不可撤销。"
        confirmLabel="确认删除"
        destructive
        pending={deleteMutation.isPending}
        onConfirm={() => void confirmDelete()}
      />
    </>
  );
}
