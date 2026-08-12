"use client";

import { Badge } from "@/components/ui/badge";
import type { AdminWalletTransaction } from "@/lib/api/types";

/**
 * 交易中心里被反复用到的格式化与徽标。
 *
 * 集中在一处的理由是**口径一致**：金额的小数位、方向的正负号、
 * 流水类型的中文名如果各面板各写一份，同一笔钱在概览页和流水页
 * 就会显示成两个数字，而对账的人无从判断哪个是对的。
 */

/** 金额：定点两位 + 千分位 + 币种代码。绝不用 Number 转换 —— 浮点会吃掉分。 */
export function formatMoney(value?: string | null, currency?: string) {
  const raw = (value ?? "0").trim();
  const negative = raw.startsWith("-");
  const digits = negative ? raw.slice(1) : raw;
  const [intPart = "0", decPart = ""] = digits.split(".");
  const grouped = intPart.replace(/\B(?=(\d{3})+(?!\d))/g, ",");
  const decimals = (decPart + "00").slice(0, 2);
  const body = `${negative ? "-" : ""}${grouped}.${decimals}`;
  return currency ? `${body} ${currency}` : body;
}

/** 带符号的流水金额：入账 +、出账 −，颜色跟着方向走。 */
export function SignedAmount({ value, currency }: { value: string; currency?: string }) {
  const credit = !value.trim().startsWith("-");
  return (
    <span className={credit ? "font-mono text-emerald-600" : "font-mono text-foreground"}>
      {credit ? "+" : ""}
      {formatMoney(value, currency)}
    </span>
  );
}

export const walletTypeLabels: Record<string, string> = {
  recharge: "余额充值",
  consume: "余额消费",
  refund: "退款入账",
  admin_adjust: "管理员调整",
  vip_purchase: "会员开通",
  order_pay: "订单支付"
};

export function walletTypeLabel(type: string) {
  return walletTypeLabels[type] ?? type;
}

export function WalletTypeBadge({ type }: { type: string }) {
  switch (type) {
    case "recharge":
      return <Badge className="bg-emerald-500/15 text-emerald-600 hover:bg-emerald-500/15">余额充值</Badge>;
    case "refund":
      return <Badge className="bg-sky-500/15 text-sky-600 hover:bg-sky-500/15">退款入账</Badge>;
    case "admin_adjust":
      return <Badge className="bg-amber-500/15 text-amber-600 hover:bg-amber-500/15">管理员调整</Badge>;
    case "vip_purchase":
      return <Badge className="bg-violet-500/15 text-violet-600 hover:bg-violet-500/15">会员开通</Badge>;
    case "order_pay":
      return <Badge variant="secondary">订单支付</Badge>;
    case "consume":
      return <Badge variant="outline">余额消费</Badge>;
    default:
      return <Badge variant="outline">{type}</Badge>;
  }
}

export function formatTime(value?: string | null) {
  if (!value) return "—";
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? String(value) : date.toLocaleString("zh-CN", { hour12: false });
}

/** 流水的人称展示：昵称优先，退回账号，再退回用户号。 */
export function transactionActor(item: AdminWalletTransaction) {
  return item.nickname?.trim() || item.account?.trim() || `UID-${item.userId}`;
}
