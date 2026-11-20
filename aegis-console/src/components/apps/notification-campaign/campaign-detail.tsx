"use client";

import { useState } from "react";
import Link from "next/link";
import { Ban, ExternalLink, Loader2, Undo2 } from "lucide-react";
import { toast } from "sonner";
import { MetricCard, RecordSheet, StateBlock, errorMessage, type InfoItem } from "@/components/users/detail/kit";
import {
  AlertDialog,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { ApiError } from "@/lib/api/client";
import type { NotificationCampaign } from "@/lib/api/notification-campaigns";
import { formatDateTime } from "@/lib/datetime";
import {
  useCancelNotificationCampaignMutation,
  useNotificationCampaignQuery,
  useRecallNotificationCampaignMutation
} from "@/lib/notification-campaign-hooks";
import { describeUserNotificationAction, userNotificationLevelLabel } from "@/lib/user-notification-meta";
import {
  CampaignProgress,
  CampaignStatusBadge,
  campaignNotificationsHref,
  canCancelCampaign,
  canRecallCampaign,
  formatCount
} from "./campaign-shared";

/**
 * 任务详情：完整统计（含现算的已读人数与已读率）、内容、人群；取消与撤回各有一步确认。
 * 撤回会把这次群发写出的消息从每位收件人的消息中心删掉，按破坏性操作处理。
 */
export function CampaignDetailSheet({
  appKey,
  campaignId,
  onOpenChange,
  canWrite
}: {
  appKey: string;
  campaignId: number | null;
  onOpenChange: (open: boolean) => void;
  canWrite: boolean;
}) {
  const query = useNotificationCampaignQuery(appKey, campaignId);
  const campaign = query.data;
  const cancel = useCancelNotificationCampaignMutation(appKey);
  const recall = useRecallNotificationCampaignMutation(appKey);
  const [confirm, setConfirm] = useState<"cancel" | "recall" | null>(null);

  const run = (kind: "cancel" | "recall") => {
    if (!campaign) return;
    const mutation = kind === "cancel" ? cancel : recall;
    mutation.mutate(campaign.id, {
      onSuccess: () => {
        toast.success(kind === "cancel" ? "已取消" : "已开始撤回");
        setConfirm(null);
      },
      onError: (error) => toast.error(error instanceof ApiError ? error.message : kind === "cancel" ? "取消失败" : "撤回失败")
    });
  };

  const actions =
    campaign && canWrite && (canCancelCampaign(campaign) || canRecallCampaign(campaign)) ? (
      <>
        {canCancelCampaign(campaign) ? (
          <Button size="sm" variant="outline" onClick={() => setConfirm("cancel")}>
            <Ban className="size-3.5" />
            取消任务
          </Button>
        ) : null}
        {canRecallCampaign(campaign) ? (
          <Button size="sm" variant="destructive" onClick={() => setConfirm("recall")}>
            <Undo2 className="size-3.5" />
            撤回
          </Button>
        ) : null}
      </>
    ) : null;

  return (
    <>
      <RecordSheet
        open={campaignId !== null}
        onOpenChange={onOpenChange}
        title={campaign ? campaign.title : "群发任务"}
        description={campaign ? `#${campaign.id} ${campaign.categoryLabel}` : undefined}
        hero={campaign ? <CampaignHero appKey={appKey} campaign={campaign} /> : undefined}
        fields={campaign ? campaignFields(campaign) : undefined}
        actions={actions}
      >
        {query.isLoading ? (
          <div className="space-y-3">
            <Skeleton className="h-6 w-40" />
            <Skeleton className="h-24 w-full" />
          </div>
        ) : query.error && !campaign ? (
          <StateBlock kind="error" description={errorMessage(query.error)} onRetry={() => void query.refetch()} />
        ) : campaign ? (
          <div className="space-y-2 rounded-xl border bg-muted/20 p-4">
            <div className="text-sm font-medium">{campaign.title}</div>
            <p className="text-xs leading-5 whitespace-pre-wrap text-muted-foreground">{campaign.content || campaign.title}</p>
          </div>
        ) : null}
      </RecordSheet>

      <AlertDialog open={confirm !== null} onOpenChange={(open) => !open && setConfirm(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{confirm === "recall" ? "撤回这次群发？" : "取消这个任务？"}</AlertDialogTitle>
            <AlertDialogDescription>
              {confirm === "recall"
                ? `将从全部收件人的消息中心删除这条消息（已写入 ${formatCount(campaign?.delivered)} 条），无法恢复。`
                : campaign && campaign.delivered > 0
                  ? `停止后续发送，已发出的 ${formatCount(campaign.delivered)} 条保留。`
                  : "任务将不再发送。"}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>返回</AlertDialogCancel>
            <Button
              variant={confirm === "recall" ? "destructive" : "default"}
              disabled={cancel.isPending || recall.isPending}
              onClick={() => confirm && run(confirm)}
            >
              {cancel.isPending || recall.isPending ? <Loader2 className="size-3.5 animate-spin" /> : null}
              {confirm === "recall" ? "确认撤回" : "确认取消"}
            </Button>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}

function CampaignHero({ appKey, campaign }: { appKey: string; campaign: NotificationCampaign }) {
  const readRate =
    campaign.readCount !== null && campaign.delivered > 0 ? `${((campaign.readCount / campaign.delivered) * 100).toFixed(1)}%` : "—";
  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <CampaignStatusBadge campaign={campaign} />
        {campaign.delivered > 0 ? (
          <Link
            href={campaignNotificationsHref(appKey, campaign.id)}
            className="inline-flex items-center gap-1 text-xs text-primary hover:underline"
          >
            查看已发消息
            <ExternalLink className="size-3" />
          </Link>
        ) : null}
      </div>
      <CampaignProgress campaign={campaign} />
      {campaign.message ? <p className="text-xs text-muted-foreground">{campaign.message}</p> : null}
      <div className="grid grid-cols-2 gap-2 sm:grid-cols-3">
        <MetricCard label="目标人数" value={formatCount(campaign.total)} />
        <MetricCard label="已处理" value={formatCount(campaign.processed)} />
        <MetricCard label="已送达" value={formatCount(campaign.delivered)} tone="success" />
        <MetricCard label="已跳过" value={formatCount(campaign.skipped)} />
        <MetricCard label="已读" value={formatCount(campaign.readCount)} sub={`已读率 ${readRate}`} tone="info" />
        <MetricCard label="已撤回" value={formatCount(campaign.recalled)} tone={campaign.recalled ? "warning" : "neutral"} />
      </div>
    </div>
  );
}

function campaignFields(campaign: NotificationCampaign): InfoItem[] {
  return [
    { label: "人群", value: campaign.audienceLabel, wide: true },
    { label: "分类", value: campaign.categoryLabel || campaign.category },
    { label: "级别", value: userNotificationLevelLabel(campaign.level) },
    { label: "跳转", value: describeUserNotificationAction(campaign.action) },
    { label: "按钮文案", value: campaign.action?.label },
    { label: "配图", value: campaign.image, mono: true, wide: true },
    { label: "过期时间", value: campaign.expiresAt ? formatDateTime(campaign.expiresAt) : "长期有效" },
    { label: "定时", value: campaign.scheduledAt ? formatDateTime(campaign.scheduledAt) : "立即" },
    { label: "开始", value: campaign.startedAt ? formatDateTime(campaign.startedAt) : null },
    { label: "结束", value: campaign.finishedAt ? formatDateTime(campaign.finishedAt) : null },
    { label: "撤回时间", value: campaign.recalledAt ? formatDateTime(campaign.recalledAt) : null },
    { label: "创建", value: formatDateTime(campaign.createdAt) },
    { label: "操作人", value: campaign.operator },
    { label: "执行次数", value: String(campaign.attempts) },
    { label: "请求标识", value: campaign.requestId, mono: true, copy: campaign.requestId, wide: true }
  ];
}
