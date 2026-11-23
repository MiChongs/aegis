"use client";

import { useState } from "react";
import {
  AlertTriangle,
  AppWindow,
  Bell,
  Bot,
  CheckCircle2,
  CircleDashed,
  Hash,
  Loader2,
  Mail,
  MessagesSquare,
  Radio,
  RefreshCw,
  Webhook,
  XCircle,
  Zap,
  type LucideIcon
} from "lucide-react";
import { notify } from "@/lib/notify";
import { ApiError } from "@/lib/api/client";
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
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty";
import { cn } from "@/lib/utils";

// 通知出口各分节共用的展示件：渠道标记、状态徽标、空 / 错误态、删除确认。

export function reportError(error: unknown, fallback: string) {
  notify.error(error instanceof ApiError ? error.message : fallback);
}

/* ── 渠道标记：类型 → 底色 + 图形 ── */

type ChannelLook = { icon: LucideIcon; plate: string };

// 飞书、钉钉、企业微信、Slack 都没有可用的官方矢量 mark（Simple Icons 未收录或已下架），
// 这里只借品牌主色区分类型，图形用中性线条图标，不冒充品牌标志。
const CHANNEL_LOOK: Record<string, ChannelLook> = {
  feishu_bot: { icon: Bot, plate: "bg-[#3370FF]/12 text-[#3370FF]" },
  feishu_app: { icon: AppWindow, plate: "bg-[#3370FF]/12 text-[#3370FF]" },
  dingtalk_bot: { icon: Zap, plate: "bg-[#1677FF]/12 text-[#1677FF]" },
  wecom_bot: { icon: MessagesSquare, plate: "bg-[#07C160]/12 text-[#07A653]" },
  slack_webhook: { icon: Hash, plate: "bg-[#611F69]/12 text-[#611F69] dark:text-[#E01E8A]" },
  webhook: { icon: Webhook, plate: "bg-slate-500/12 text-slate-600 dark:text-slate-300" },
  email: { icon: Mail, plate: "bg-amber-500/12 text-amber-600 dark:text-amber-400" },
  inapp: { icon: Bell, plate: "bg-violet-500/12 text-violet-600 dark:text-violet-400" },
  realtime: { icon: Radio, plate: "bg-emerald-500/12 text-emerald-600 dark:text-emerald-400" }
};

const FALLBACK_LOOK: ChannelLook = { icon: Bell, plate: "bg-muted text-muted-foreground" };

export function ChannelMark({ kind, size = "default", className }: { kind?: string; size?: "sm" | "default" | "lg"; className?: string }) {
  const look = (kind && CHANNEL_LOOK[kind]) || FALLBACK_LOOK;
  const Icon = look.icon;
  return (
    <span
      aria-hidden
      className={cn(
        "grid shrink-0 place-items-center",
        size === "sm" && "size-6 rounded-md",
        size === "default" && "size-9 rounded-lg",
        size === "lg" && "size-11 rounded-xl",
        look.plate,
        className
      )}
    >
      <Icon className={size === "sm" ? "size-3.5" : size === "lg" ? "size-5.5" : "size-4.5"} />
    </span>
  );
}

/* ── 状态 ── */

export const PRIORITY_TEXT: Record<string, string> = {
  low: "低及以上",
  normal: "中及以上",
  high: "高及以上",
  urgent: "仅紧急"
};

export const DELIVERY_STATUS_LABEL: Record<string, string> = {
  pending: "投递中",
  success: "成功",
  failed: "失败",
  dropped: "已丢弃",
  skipped: "已跳过"
};

export function DeliveryStatusBadge({ status }: { status: string }) {
  if (status === "success") {
    return (
      <Badge variant="success" size="sm">
        <CheckCircle2 className="size-3" />
        成功
      </Badge>
    );
  }
  if (status === "failed" || status === "dropped") {
    return (
      <Badge variant="danger" size="sm">
        <AlertTriangle className="size-3" />
        {DELIVERY_STATUS_LABEL[status]}
      </Badge>
    );
  }
  if (status === "skipped") {
    return (
      <Badge variant="secondary" size="sm">
        <CircleDashed className="size-3" />
        已跳过
      </Badge>
    );
  }
  return (
    <Badge variant="outline" size="sm">
      <Loader2 className="size-3 animate-spin" />
      投递中
    </Badge>
  );
}

/** 渠道最近一次投递的结论 */
export function ChannelHealth({ status, error }: { status?: string; error?: string }) {
  if (!status) {
    return (
      <span className="inline-flex items-center gap-1.5 text-[11px] text-muted-foreground">
        <span className="size-1.5 rounded-full bg-muted-foreground/40" />
        未投递过
      </span>
    );
  }
  if (status === "success") {
    return (
      <span className="inline-flex items-center gap-1.5 text-[11px] text-emerald-600 dark:text-emerald-400">
        <span className="relative flex size-1.5">
          <span className="absolute inline-flex size-full animate-ping rounded-full bg-emerald-500/60" />
          <span className="relative inline-flex size-1.5 rounded-full bg-emerald-500" />
        </span>
        运行正常
      </span>
    );
  }
  return (
    <span className="inline-flex items-center gap-1.5 text-[11px] text-red-600 dark:text-red-400" title={error}>
      <XCircle className="size-3" />
      最近一次失败
    </span>
  );
}

/* ── 空 / 错误态 ── */

export function SectionEmpty({
  icon: Icon,
  title,
  description,
  action
}: {
  icon: LucideIcon;
  title: string;
  description?: string;
  action?: React.ReactNode;
}) {
  return (
    <Empty className="min-h-64 border bg-muted/20">
      <EmptyHeader>
        <EmptyMedia variant="icon" className="size-10 text-muted-foreground">
          <Icon className="size-5" />
        </EmptyMedia>
        <EmptyTitle className="text-sm">{title}</EmptyTitle>
        {description ? <EmptyDescription className="text-xs">{description}</EmptyDescription> : null}
      </EmptyHeader>
      {action ? <EmptyContent>{action}</EmptyContent> : null}
    </Empty>
  );
}

export function SectionError({ error, onRetry }: { error: unknown; onRetry: () => void }) {
  return (
    <Empty className="min-h-64 border border-destructive/30 bg-destructive/5">
      <EmptyHeader>
        <EmptyMedia variant="icon" className="size-10 bg-destructive/10 text-destructive">
          <AlertTriangle className="size-5" />
        </EmptyMedia>
        <EmptyTitle className="text-sm">加载失败</EmptyTitle>
        <EmptyDescription className="text-xs">{error instanceof ApiError ? error.message : "请稍后重试"}</EmptyDescription>
      </EmptyHeader>
      <EmptyContent>
        <Button size="sm" variant="outline" onClick={onRetry}>
          <RefreshCw />
          重试
        </Button>
      </EmptyContent>
    </Empty>
  );
}

/** 分节标题栏：标题 + 说明 + 右侧操作 */
export function SectionHeader({
  title,
  description,
  children
}: {
  title: string;
  description?: string;
  children?: React.ReactNode;
}) {
  return (
    <div className="flex flex-wrap items-end justify-between gap-3">
      <div className="min-w-0 space-y-0.5">
        <h3 className="text-base font-semibold tracking-tight text-foreground">{title}</h3>
        {description ? <p className="text-xs text-muted-foreground">{description}</p> : null}
      </div>
      {children ? <div className="flex flex-wrap items-center gap-2">{children}</div> : null}
    </div>
  );
}

/* ── 删除确认 ── */

export function useConfirm() {
  const [pending, setPending] = useState<{
    title: string;
    description: string;
    action: string;
    run: () => Promise<void>;
  } | null>(null);
  const [busy, setBusy] = useState(false);

  const dialog = (
    <AlertDialog open={pending !== null} onOpenChange={(open) => !open && !busy && setPending(null)}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{pending?.title}</AlertDialogTitle>
          <AlertDialogDescription>{pending?.description}</AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel disabled={busy}>取消</AlertDialogCancel>
          <AlertDialogAction
            disabled={busy}
            className="bg-destructive text-white hover:bg-destructive/90"
            onClick={async (event) => {
              event.preventDefault();
              if (!pending) return;
              setBusy(true);
              try {
                await pending.run();
                setPending(null);
              } finally {
                setBusy(false);
              }
            }}
          >
            {busy ? <Loader2 className="animate-spin" /> : null}
            {pending?.action}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );

  return { confirm: setPending, dialog };
}

/* ── 模板变量 ── */

export const TEMPLATE_VARS: { key: string; label: string; sample: string }[] = [
  { key: "TicketNo", label: "工单编号", sample: "TK20260810000042" },
  { key: "TicketTitle", label: "工单标题", sample: "支付未到账" },
  { key: "StatusLabel", label: "状态", sample: "处理中" },
  { key: "PriorityText", label: "优先级", sample: "紧急" },
  { key: "Requester", label: "提单人", sample: "张三" },
  { key: "Assignee", label: "受理人", sample: "李四" },
  { key: "Group", label: "处理组", sample: "支付支持组" },
  { key: "Category", label: "分类", sample: "支付问题" },
  { key: "AppName", label: "应用", sample: "示例应用" },
  { key: "Link", label: "工单链接", sample: "https://console.example.com/tickets?id=42" },
  { key: "Time", label: "时间", sample: "2026-08-10 15:04:05" }
];

export const SAMPLE_VARS: Record<string, string> = Object.fromEntries(TEMPLATE_VARS.map((item) => [item.key, item.sample]));
