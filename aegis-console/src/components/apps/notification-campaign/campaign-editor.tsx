"use client";

import { useMemo, useState } from "react";
import { AlertTriangle, CalendarClock, Loader2, Send } from "lucide-react";
import { toast } from "sonner";
import {
  NotificationContentFields,
  NotificationField,
  checkNotificationContent,
  emptyNotificationContent,
  notificationContentPayload,
  type NotificationContentDraft
} from "@/components/notifications/notification-content-fields";
import { useNow } from "@/components/users/ban-form";
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
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import { ApiError } from "@/lib/api/client";
import type { NotificationAudience, NotificationCampaign } from "@/lib/api/notification-campaigns";
import { formatDateTimeMinute, fromLocalInputValue, toLocalInputValue } from "@/lib/datetime";
import {
  useCreateNotificationCampaignMutation,
  useNotificationAudiencePreviewQuery,
  useNotificationCampaignCatalogQuery
} from "@/lib/notification-campaign-hooks";
import { useDebouncedValue } from "@/lib/use-debounced-value";
import { AudienceBuilder, AudiencePreviewCard } from "./campaign-audience";
import {
  DEFAULT_AUDIENCE_LIMITS,
  buildAudience,
  emptyAudienceDraft,
  formatCount,
  newRequestId,
  type AudienceDraft
} from "./campaign-shared";

/**
 * 发起群发：内容、人群、实时预览、可选定时，确认后提交。
 *
 * - 每打开一次编辑器生成一个 `requestId`（调用方按打开次数换 key 重新挂载），重复点提交返回同一个任务。
 * - 预览在人群停止变化 400ms 后请求，不合法的人群不请求。
 * - 确认一步写明收件人数；全站发送要求输入「全站发送」才能确认。
 */

const PREVIEW_DEBOUNCE_MS = 400;
const WHOLE_SITE_PHRASE = "全站发送";
const HOUR = 60 * 60 * 1000;

export function CampaignEditor({
  appKey,
  open,
  onOpenChange,
  onCreated
}: {
  appKey: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onCreated: (campaign: NotificationCampaign) => void;
}) {
  const now = useNow();
  const catalogQuery = useNotificationCampaignCatalogQuery(appKey);
  const catalog = catalogQuery.data;
  const create = useCreateNotificationCampaignMutation(appKey);

  const [requestId] = useState(newRequestId);
  const [content, setContent] = useState<NotificationContentDraft>(emptyNotificationContent);
  const [audienceDraft, setAudienceDraft] = useState<AudienceDraft>(emptyAudienceDraft);
  const [scheduled, setScheduled] = useState(false);
  const [scheduledAt, setScheduledAt] = useState("");
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [phrase, setPhrase] = useState("");

  const limits = useMemo(
    () => ({
      maxUserIds: catalog?.limits.maxUserIds ?? DEFAULT_AUDIENCE_LIMITS.maxUserIds,
      maxDays: catalog?.limits.maxDays ?? DEFAULT_AUDIENCE_LIMITS.maxDays,
      maxLevel: catalog?.limits.maxLevel ?? DEFAULT_AUDIENCE_LIMITS.maxLevel
    }),
    [catalog]
  );
  const maxScheduleMs = (catalog?.limits.maxScheduleHours ?? 90 * 24) * HOUR;

  const built = useMemo(() => buildAudience(audienceDraft, limits), [audienceDraft, limits]);
  // 防抖的是序列化后的字符串：对象每次渲染都是新的，直接防抖会无限重设
  const audienceKey = built.audience ? JSON.stringify(built.audience) : "";
  const debouncedKey = useDebouncedValue(audienceKey, PREVIEW_DEBOUNCE_MS);
  const debouncedAudience = useMemo<NotificationAudience | null>(
    () => (debouncedKey ? (JSON.parse(debouncedKey) as NotificationAudience) : null),
    [debouncedKey]
  );
  const previewQuery = useNotificationAudiencePreviewQuery(appKey, debouncedAudience);
  const previewSettled = Boolean(audienceKey) && debouncedKey === audienceKey && !previewQuery.isPlaceholderData && !previewQuery.isFetching;
  const preview = previewQuery.data;
  const previewError = previewQuery.error ? (previewQuery.error instanceof ApiError ? previewQuery.error.message : "预览失败") : null;

  const scheduledIso = scheduled ? fromLocalInputValue(scheduledAt) : undefined;
  const scheduledMs = scheduledIso ? new Date(scheduledIso).getTime() : null;
  let scheduleError: string | null = null;
  if (scheduled) {
    if (scheduledMs === null) scheduleError = "请选择发送时间";
    else if (scheduledMs <= now) scheduleError = "必须晚于现在";
    else if (scheduledMs > now + maxScheduleMs) scheduleError = `不能晚于 ${Math.round(maxScheduleMs / (24 * HOUR))} 天后`;
  }

  const { errors, ready } = checkNotificationContent(content, {
    now,
    expiresAfter: scheduledMs ? { at: scheduledMs, message: "必须晚于定时发送时间" } : null
  });
  const canSubmit = ready && Boolean(built.audience) && !scheduleError;

  const wholeSite = built.audience?.scope === "all";
  const total = previewSettled ? (preview?.total ?? null) : null;
  const emptyNow = !scheduled && total === 0;
  const confirmBlocked = total === null || emptyNow || (wholeSite && phrase.trim() !== WHOLE_SITE_PHRASE) || create.isPending;

  async function submit() {
    if (!built.audience) return;
    try {
      const result = await create.mutateAsync({
        ...notificationContentPayload(content),
        requestId,
        scheduledAt: scheduledIso,
        audience: built.audience
      });
      toast.success(result.created ? (scheduledIso ? "已创建定时群发" : "群发任务已开始") : "该任务已提交过");
      setConfirmOpen(false);
      onOpenChange(false);
      onCreated(result.campaign);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "发起失败");
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[92vh] overflow-y-auto sm:max-w-5xl">
        <DialogHeader>
          <DialogTitle className="text-sm">发起群发</DialogTitle>
          <DialogDescription>发送到所选用户的消息中心，后台分批写入。</DialogDescription>
        </DialogHeader>

        <div className="grid gap-5 lg:grid-cols-[minmax(0,1fr)_300px]">
          <div className="min-w-0 space-y-5">
            <section className="space-y-3">
              <h4 className="text-xs font-semibold">收件人</h4>
              <AudienceBuilder
                draft={audienceDraft}
                onChange={setAudienceDraft}
                catalog={catalog}
                limits={limits}
                userIdCount={built.userIdCount}
                excludeCount={built.excludeCount}
                current={built.audience}
              />
            </section>

            <section className="space-y-3 border-t pt-4">
              <h4 className="text-xs font-semibold">内容</h4>
              <NotificationContentFields
                value={content}
                onChange={(patch) => setContent((current) => ({ ...current, ...patch }))}
                errors={errors}
                now={now}
                categories={catalog?.categories.map((item) => ({ value: item.key, label: item.label }))}
                contentOptional
              />
            </section>

            <section className="space-y-3 border-t pt-4">
              <div className="flex items-center justify-between gap-2">
                <h4 className="text-xs font-semibold">定时发送</h4>
                <Switch
                  checked={scheduled}
                  onCheckedChange={(checked) => {
                    setScheduled(checked);
                    if (checked && !scheduledAt) setScheduledAt(toLocalInputValue(now + HOUR));
                  }}
                />
              </div>
              {scheduled ? (
                <NotificationField label="发送时间" error={scheduleError}>
                  <Input
                    type="datetime-local"
                    value={scheduledAt}
                    min={toLocalInputValue(now)}
                    max={toLocalInputValue(now + maxScheduleMs)}
                    className="h-8 w-52 font-mono text-xs tabular-nums"
                    onChange={(event) => setScheduledAt(event.target.value)}
                  />
                </NotificationField>
              ) : null}
            </section>
          </div>

          <aside className="lg:sticky lg:top-0 lg:self-start">
            <AudiencePreviewCard
              preview={preview}
              loading={previewQuery.isLoading}
              fetching={previewQuery.isFetching || debouncedKey !== audienceKey}
              problem={built.problem}
              error={previewError}
            />
          </aside>
        </div>

        <DialogFooter>
          <Button size="sm" variant="ghost" onClick={() => onOpenChange(false)}>
            取消
          </Button>
          <Button
            size="sm"
            disabled={!canSubmit}
            onClick={() => {
              setPhrase("");
              setConfirmOpen(true);
            }}
          >
            {scheduled ? <CalendarClock className="size-3" /> : <Send className="size-3" />}
            {scheduled ? "创建定时群发" : "发起群发"}
          </Button>
        </DialogFooter>
      </DialogContent>

      <AlertDialog open={confirmOpen} onOpenChange={setConfirmOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{wholeSite ? "确认全站群发" : "确认群发"}</AlertDialogTitle>
            <AlertDialogDescription>
              {previewError
                ? `无法统计收件人数：${previewError}`
                : total === null
                ? "正在统计收件人数。"
                : emptyNow
                  ? "当前人群没有可发送的用户。"
                  : scheduledIso
                    ? `将于 ${formatDateTimeMinute(scheduledIso)} 开始发送，按当前条件约 ${formatCount(total)} 人，人数以开始发送时为准。`
                    : `将立即向 ${formatCount(total)} 人发送「${content.title.trim()}」。`}
            </AlertDialogDescription>
          </AlertDialogHeader>

          {wholeSite && total !== null && !emptyNow ? (
            <div className="space-y-3 rounded-lg border border-destructive/40 bg-destructive/5 p-3">
              <div className="flex items-start gap-2 text-xs text-destructive">
                <AlertTriangle className="mt-0.5 size-3.5 shrink-0" />
                <span>全站发送：应用下 {formatCount(total)} 位用户都会收到这条消息。</span>
              </div>
              <NotificationField label={`输入「${WHOLE_SITE_PHRASE}」以确认`}>
                <Input value={phrase} className="h-8 text-xs" onChange={(event) => setPhrase(event.target.value)} />
              </NotificationField>
            </div>
          ) : null}

          <AlertDialogFooter>
            <AlertDialogCancel>返回修改</AlertDialogCancel>
            <Button
              variant={wholeSite ? "destructive" : "default"}
              disabled={confirmBlocked}
              onClick={() => void submit()}
            >
              {create.isPending ? <Loader2 className="size-3.5 animate-spin" /> : null}
              {scheduledIso ? "确认创建" : "确认发送"}
            </Button>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </Dialog>
  );
}
