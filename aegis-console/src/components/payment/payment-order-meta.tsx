"use client";

import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";

/**
 * 订单的展示约定：状态、退款状态、时间。列表与详情抽屉共用，
 * 同一笔订单在两处不能出现两种叫法。
 */

export type OrderStatusMeta = { label: string; dot: string; badge: string };

const STATUS_META: Record<string, OrderStatusMeta> = {
  paid: { label: "已支付", dot: "bg-emerald-500", badge: "border-emerald-500/20 bg-emerald-500/10 text-emerald-600 dark:text-emerald-400" },
  pending: { label: "待支付", dot: "bg-amber-500", badge: "border-amber-500/20 bg-amber-500/10 text-amber-600 dark:text-amber-400" },
  expired: { label: "已过期", dot: "bg-muted-foreground/50", badge: "border-border bg-muted text-muted-foreground" },
  failed: { label: "支付失败", dot: "bg-red-500", badge: "border-red-500/20 bg-red-500/10 text-red-600 dark:text-red-400" },
  closed: { label: "已关闭", dot: "bg-muted-foreground/50", badge: "border-border bg-muted text-muted-foreground" },
  refunded: { label: "已退款", dot: "bg-sky-500", badge: "border-sky-500/20 bg-sky-500/10 text-sky-600 dark:text-sky-400" }
};

export const ORDER_STATUS_FILTERS = [
  { value: "", label: "全部" },
  { value: "paid", label: "已支付" },
  { value: "pending", label: "待支付" },
  { value: "expired", label: "已过期" },
  { value: "failed", label: "支付失败" }
];

export function orderStatusMeta(status?: string): OrderStatusMeta {
  return STATUS_META[status ?? ""] ?? { label: status || "未知", dot: "bg-muted-foreground/50", badge: "border-border bg-muted text-muted-foreground" };
}

export function OrderStatusBadge({ status, className }: { status?: string; className?: string }) {
  const meta = orderStatusMeta(status);
  return (
    <Badge variant="outline" className={cn("gap-1.5 font-medium", meta.badge, className)}>
      <span className={cn("size-1.5 rounded-full", meta.dot)} />
      {meta.label}
    </Badge>
  );
}

/** 订单退款汇总状态 */
export function orderRefundLabel(status?: string) {
  switch (status) {
    case "full":
      return "已全额退款";
    case "partial":
      return "部分退款";
    default:
      return "";
  }
}

const pad = (n: number) => String(n).padStart(2, "0");

/** 绝对时间：同年省略年份。 */
export function formatOrderTime(value?: string | null, withYear = false) {
  if (!value) return "";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return String(value);
  const sameYear = date.getFullYear() === new Date().getFullYear();
  const day = `${pad(date.getMonth() + 1)}-${pad(date.getDate())}`;
  const time = `${pad(date.getHours())}:${pad(date.getMinutes())}`;
  return withYear || !sameYear ? `${date.getFullYear()}-${day} ${time}` : `${day} ${time}`;
}

/** 相对时间：刚刚、5 分钟前、3 小时前、昨天，再早给日期。 */
export function relativeOrderTime(value?: string | null) {
  if (!value) return "";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return String(value);
  const diff = Date.now() - date.getTime();
  const minute = 60_000;
  if (diff < minute) return "刚刚";
  if (diff < 60 * minute) return `${Math.floor(diff / minute)} 分钟前`;
  if (diff < 24 * 60 * minute) return `${Math.floor(diff / (60 * minute))} 小时前`;
  const yesterday = new Date();
  yesterday.setDate(yesterday.getDate() - 1);
  if (date.toDateString() === yesterday.toDateString()) return `昨天 ${pad(date.getHours())}:${pad(date.getMinutes())}`;
  return formatOrderTime(value);
}

/** 回调验签结果 */
export function callbackVerified(status: string) {
  const normalized = status.toLowerCase();
  return normalized === "ok" || normalized.includes("success") || normalized === "verified";
}
