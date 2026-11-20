"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { Loader2, Send } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle
} from "@/components/ui/dialog";
import {
  NotificationContentFields,
  checkNotificationContent,
  emptyNotificationContent,
  notificationContentPayload,
  type NotificationContentDraft
} from "@/components/notifications/notification-content-fields";
import { useNow } from "@/components/users/ban-form";
import { ApiError } from "@/lib/api-client";
import { useBulkNotifyAppUsersMutation } from "@/lib/app-user-hooks";
import { appSectionHref } from "@/lib/app-sections";

/**
 * 给选中的用户发站内信（docs/notification-center.md「管理端」与「旧群发接口」）。
 *
 * 字段与校验和应用详情的「站内信群发」共用（components/notifications/notification-content-fields）。
 * 后端对不超过 2000 人就地发完；更多时转后台群发任务（`async=true`、`delivered=0`），
 * 这时不能说「已发送 N 人」，改为指向「站内信群发」区块看进度。
 */
export function BulkNotifyDialog({
  open,
  onOpenChange,
  appKey,
  selectedIds,
  onDone
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  appKey: string | null;
  selectedIds: number[];
  onDone: () => void;
}) {
  const mutation = useBulkNotifyAppUsersMutation(appKey);
  const router = useRouter();
  const now = useNow();
  const [draft, setDraft] = useState<NotificationContentDraft>(emptyNotificationContent);
  const { errors, ready } = checkNotificationContent(draft, { now, requireContent: true });

  async function submit() {
    try {
      const content = notificationContentPayload(draft);
      const result = await mutation.mutateAsync({ ...content, content: content.content ?? "", userIds: selectedIds, type: "system" });
      if (result.async) {
        const href = appKey ? appSectionHref(appKey, "notification-campaigns") : null;
        toast.success("已转为后台群发任务", {
          description: "可在应用详情的「站内信群发」查看进度",
          action: href ? { label: "查看", onClick: () => router.push(href) } : undefined
        });
      } else {
        toast.success(`已向 ${result.delivered} 个用户发送站内信`);
      }
      setDraft(emptyNotificationContent());
      onOpenChange(false);
      onDone();
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "发送失败");
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-lg">
        <DialogHeader>
          <DialogTitle className="text-sm">给 {selectedIds.length} 个用户发站内信</DialogTitle>
          <DialogDescription>发送到用户的消息中心，在线设备实时收到。</DialogDescription>
        </DialogHeader>

        <NotificationContentFields
          value={draft}
          onChange={(patch) => setDraft((current) => ({ ...current, ...patch }))}
          errors={errors}
          now={now}
        />

        <DialogFooter>
          <Button size="sm" variant="ghost" onClick={() => onOpenChange(false)}>
            取消
          </Button>
          <Button size="sm" disabled={!ready || mutation.isPending} onClick={submit}>
            {mutation.isPending ? <Loader2 className="size-3 animate-spin" /> : <Send className="size-3" />}
            发送
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
