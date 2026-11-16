"use client";

import { Badge } from "@/components/ui/badge";
import type { AdminWalletTransaction, WalletAdjustBucket } from "@/lib/api/types";

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
  recharge_bonus: "充值赠送",
  integral_exchange: "兑换积分",
  consume: "余额消费",
  refund: "退款入账",
  admin_adjust: "管理员调整",
  vip_purchase: "会员开通",
  order_pay: "订单支付",
  card_key: "卡密入账",
  points_exchange: "积分兑换",
  invite: "邀请奖励",
  leaderboard: "排行榜奖励",
  item_grant: "物品发放",
  ai_refund: "AI 调用退款"
};

export function walletTypeLabel(type: string) {
  return walletTypeLabels[type] ?? type;
}

export function WalletTypeBadge({ type }: { type: string }) {
  switch (type) {
    case "recharge":
      return <Badge className="bg-emerald-500/15 text-emerald-600 hover:bg-emerald-500/15">余额充值</Badge>;
    case "recharge_bonus":
      return <Badge className="bg-teal-500/15 text-teal-600 hover:bg-teal-500/15">充值赠送</Badge>;
    case "integral_exchange":
      return <Badge className="bg-orange-500/15 text-orange-600 hover:bg-orange-500/15">兑换积分</Badge>;
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
      return <Badge variant="outline">{walletTypeLabel(type)}</Badge>;
  }
}

function isZeroMoney(value?: string | null) {
  return !value || /^[+-]?0*(\.0*)?$/.test(value.trim());
}

function unsigned(value: string) {
  return value.trim().replace(/^[+-]/, "");
}

/**
 * 一笔流水在两桶上的拆分（docs/wallet.md）：只动本金时不标注；只动赠送余额时标「赠送余额」；
 * 两桶都动时写成「充值 x / 赠送 y」。符号由主金额表达，这里只写绝对值。
 * 拆分字段缺失（旧后端）时返回 null。
 */
export function transactionSplitText(item: { cashAmount?: string; bonusAmount?: string }) {
  if (item.bonusAmount == null || isZeroMoney(item.bonusAmount)) return null;
  if (isZeroMoney(item.cashAmount)) return "赠送余额";
  return `充值 ${formatMoney(unsigned(item.cashAmount ?? "0"))} / 赠送 ${formatMoney(unsigned(item.bonusAmount))}`;
}

/** 余额的两桶拆分「充值 x / 赠送 y」；分桶字段缺失（旧后端）时返回 null。 */
export function walletSplitText(wallet?: { cashBalance?: string; bonusBalance?: string } | null, currency?: string) {
  if (!wallet || wallet.cashBalance == null || wallet.bonusBalance == null) return null;
  return `充值 ${formatMoney(wallet.cashBalance, currency)} / 赠送 ${formatMoney(wallet.bonusBalance, currency)}`;
}

// ── 调账分桶 ──

/** 调账落在哪一桶（docs/wallet.md）。界面用 "default" 表示不指定，提交时转成空。 */
export type BucketChoice = "default" | "cash" | "bonus";

export const WALLET_BUCKET_OPTIONS: Array<{ value: BucketChoice; label: string }> = [
  { value: "default", label: "默认" },
  { value: "cash", label: "充值余额" },
  { value: "bonus", label: "赠送余额" }
];

/** 各桶的一句说明，随方向变化 */
export function walletBucketHint(bucket: BucketChoice, credit: boolean) {
  switch (bucket) {
    case "cash":
      return credit ? "计入充值余额（本金），可用于消费和兑换积分。" : "只从充值余额（本金）扣减。";
    case "bonus":
      return credit ? "计入赠送余额，只能消费，不能兑换积分。" : "只从赠送余额扣减。";
    default:
      return credit ? "入账计入赠送余额，只能消费，不能兑换积分。" : "扣减时先扣赠送余额，不足部分再扣充值余额。";
  }
}

export function toAdjustBucket(choice: BucketChoice): WalletAdjustBucket {
  return choice === "default" ? "" : choice;
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
