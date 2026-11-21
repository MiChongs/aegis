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

/* ── 按分计算 ──
 * 概览要对金额做求和、均值与比例。全程用 BigInt 的「分」，只在最后一步转回字符串，
 * 与 formatMoney 同一口径：两位小数、超出的位数截掉。比例只用于展示，可以转 number。 */

/** "1234.5" → 123450n；无法解析时按 0 处理 */
export function toCents(value?: string | null): bigint {
  const raw = (value ?? "").trim();
  if (!raw) return BigInt(0);
  const negative = raw.startsWith("-");
  const digits = negative || raw.startsWith("+") ? raw.slice(1) : raw;
  const [intPart = "0", decPart = ""] = digits.split(".");
  if (!/^\d*$/.test(intPart) || !/^\d*$/.test(decPart)) return BigInt(0);
  const cents = BigInt(intPart || "0") * BigInt(100) + BigInt((decPart + "00").slice(0, 2));
  return negative ? -cents : cents;
}

/** 123450n → "1234.50" */
export function centsToMoney(cents: bigint): string {
  const negative = cents < BigInt(0);
  const abs = negative ? -cents : cents;
  const hundred = BigInt(100);
  return `${negative ? "-" : ""}${abs / hundred}.${(abs % hundred).toString().padStart(2, "0")}`;
}

export function sumMoney(values: Array<string | null | undefined>): string {
  return centsToMoney(values.reduce((sum, value) => sum + toCents(value), BigInt(0)));
}

/** 金额 ÷ 笔数（客单价、人均），四舍五入到分；除数为 0 时返回 null */
export function divideMoney(value: string | null | undefined, count: number): string | null {
  if (!Number.isFinite(count) || count <= 0) return null;
  const cents = toCents(value);
  const divisor = BigInt(Math.round(count));
  const half = divisor / BigInt(2);
  const rounded = cents >= BigInt(0) ? (cents + half) / divisor : (cents - half) / divisor;
  return centsToMoney(rounded);
}

/** a / b 的比例（0–1），b 为 0 时返回 null —— 没有分母不等于 0% */
export function moneyRatio(a: string | null | undefined, b: string | null | undefined): number | null {
  const denominator = toCents(b);
  if (denominator === BigInt(0)) return null;
  return Number((toCents(a) * BigInt(1_000_000)) / denominator) / 1_000_000;
}

export function formatPercent(ratio: number | null, digits = 1) {
  if (ratio === null || !Number.isFinite(ratio)) return "—";
  return `${(ratio * 100).toFixed(digits)}%`;
}

/** 坐标轴与大号数字上的紧凑金额：`128000` → `12.8万`。轴上写全位数会把标签挤成一团。 */
export function compactMoney(value: number) {
  if (!Number.isFinite(value)) return "0";
  const abs = Math.abs(value);
  if (abs >= 100_000_000) return `${(value / 100_000_000).toFixed(1)}亿`;
  if (abs >= 10_000) return `${(value / 10_000).toFixed(1)}万`;
  if (abs >= 1_000) return `${(value / 1_000).toFixed(1)}k`;
  return String(Math.round(value));
}
