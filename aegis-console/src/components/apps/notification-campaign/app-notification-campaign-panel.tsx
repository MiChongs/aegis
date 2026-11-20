"use client";

import { useState } from "react";
import { BellRing, Plus } from "lucide-react";
import { NoAppSelected } from "@/components/apps/app-config-primitives";
import { FilterBar, FilterSelect, Pager, RecordTable, Section, StateBlock, type Column } from "@/components/users/detail/kit";
import { Button } from "@/components/ui/button";
import type { NotificationCampaign } from "@/lib/api/notification-campaigns";
import { useAppKeyPermissionChecker } from "@/lib/app-permission-hooks";
import { formatDateTime } from "@/lib/datetime";
import { useNotificationCampaignCatalogQuery, useNotificationCampaignsQuery } from "@/lib/notification-campaign-hooks";
import { CampaignDetailSheet } from "./campaign-detail";
import { CampaignEditor } from "./campaign-editor";
import { CampaignProgress, CampaignStatusBadge, STATUS_OPTIONS } from "./campaign-shared";

/**
 * 站内信群发区块（docs/notification-center.md「群发任务」）。
 *
 * 任务列表（状态筛选，有未结束的任务时轮询）+ 发起编辑器 + 详情抽屉。
 * 发起、取消、撤回需要 `app:notification:write`，没有时只读。
 */
export function AppNotificationCampaignPanel({ appKey }: { appKey?: string | null }) {
  if (!appKey) return <NoAppSelected icon={<BellRing className="size-5" />} />;
  return <CampaignWorkspace appKey={appKey} />;
}

function CampaignWorkspace({ appKey }: { appKey: string }) {
  const can = useAppKeyPermissionChecker(appKey);
  const canWrite = can("app:notification:write");
  const catalogQuery = useNotificationCampaignCatalogQuery(appKey);
  const [status, setStatus] = useState("");
  const [page, setPage] = useState(1);
  const [limit, setLimit] = useState(20);
  const listQuery = useNotificationCampaignsQuery(appKey, { status: status || undefined, page, limit });
  const data = listQuery.data;
  // 每次打开编辑器换一个 key：重新挂载即重新生成幂等键、清空上一次的草稿
  const [editorKey, setEditorKey] = useState(0);
  const [editorOpen, setEditorOpen] = useState(false);
  const [detailId, setDetailId] = useState<number | null>(null);

  const openEditor = () => {
    setEditorKey((key) => key + 1);
    setEditorOpen(true);
  };

  const statusOptions = catalogQuery.data?.statuses ?? STATUS_OPTIONS;

  const columns: Array<Column<NotificationCampaign>> = [
    {
      id: "title",
      header: "任务",
      width: "24%",
      cell: (campaign) => (
        <div className="min-w-0">
          <div className="truncate text-xs font-medium" title={campaign.title}>
            {campaign.title}
          </div>
          <div className="truncate text-[11px] text-muted-foreground">
            #{campaign.id} {campaign.categoryLabel || campaign.category}
          </div>
        </div>
      )
    },
    {
      id: "audience",
      header: "人群",
      width: "20%",
      hideBelow: "md",
      cell: (campaign) => (
        <div className="line-clamp-2 text-xs text-muted-foreground" title={campaign.audienceLabel}>
          {campaign.audienceLabel}
        </div>
      )
    },
    {
      id: "status",
      header: "状态",
      width: "96px",
      cell: (campaign) => <CampaignStatusBadge campaign={campaign} />
    },
    {
      id: "progress",
      header: "进度",
      cell: (campaign) => <CampaignProgress campaign={campaign} detailed />
    },
    {
      id: "operator",
      header: "操作人",
      width: "96px",
      hideBelow: "lg",
      cell: (campaign) => <span className="truncate text-xs text-muted-foreground">{campaign.operator || "—"}</span>
    },
    {
      id: "time",
      header: "创建 / 定时",
      width: "150px",
      hideBelow: "lg",
      cell: (campaign) => (
        <div className="text-[11px] tabular-nums text-muted-foreground">
          <div>{formatDateTime(campaign.createdAt)}</div>
          {campaign.scheduledAt ? <div>定时 {formatDateTime(campaign.scheduledAt)}</div> : null}
        </div>
      )
    }
  ];

  return (
    <div className="space-y-4">
      <Section
        title="群发任务"
        icon={BellRing}
        tone="info"
        flush
        actions={
          canWrite ? (
            <Button size="sm" onClick={openEditor}>
              <Plus className="size-3.5" />
              发起群发
            </Button>
          ) : null
        }
        toolbar={
          <FilterBar>
            <FilterSelect
              label="状态"
              value={status}
              onChange={(value) => {
                setStatus(value);
                setPage(1);
              }}
              options={statusOptions}
              allLabel="全部状态"
            />
          </FilterBar>
        }
        footer={
          data ? (
            <Pager
              page={page}
              limit={limit}
              total={data.total}
              onPage={setPage}
              onLimit={(value) => {
                setLimit(value);
                setPage(1);
              }}
            />
          ) : null
        }
      >
        <RecordTable
          columns={columns}
          rows={data?.items}
          rowKey={(campaign) => campaign.id}
          loading={listQuery.isLoading}
          fetching={listQuery.isFetching}
          error={listQuery.error}
          onRetry={() => void listQuery.refetch()}
          onRowClick={(campaign) => setDetailId(campaign.id)}
          empty={
            <StateBlock
              compact
              icon={BellRing}
              title="暂无群发任务"
              action={
                canWrite ? (
                  <Button size="sm" variant="outline" onClick={openEditor}>
                    <Plus className="size-3.5" />
                    发起群发
                  </Button>
                ) : undefined
              }
            />
          }
        />
      </Section>

      {editorOpen ? (
        <CampaignEditor
          key={editorKey}
          appKey={appKey}
          open={editorOpen}
          onOpenChange={setEditorOpen}
          onCreated={(campaign) => setDetailId(campaign.id)}
        />
      ) : null}

      <CampaignDetailSheet
        appKey={appKey}
        campaignId={detailId}
        onOpenChange={(open) => !open && setDetailId(null)}
        canWrite={canWrite}
      />
    </div>
  );
}
