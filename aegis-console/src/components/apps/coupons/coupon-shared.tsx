"use client";

import { useState } from "react";
import { Check, ChevronsUpDown, TicketPercent } from "lucide-react";
import { useRouteAppKey } from "@/components/apps/cosmetics/cosmetic-reward";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Command, CommandEmpty, CommandGroup, CommandInput, CommandItem, CommandList } from "@/components/ui/command";
import { Input } from "@/components/ui/input";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import type {
  AudienceKind,
  CouponAudience,
  CouponScope,
  CouponStatus,
  CouponTemplate,
  CouponTheme,
  DiscountMode,
  OrderPromotionStatus,
  Promotion,
  PromotionKind,
  RebateStatus,
  UserCouponStatus
} from "@/lib/api/coupons";
import { useAllCouponTemplatesQuery } from "@/lib/coupon-hooks";
import { useCouponNamesVersion } from "@/lib/coupon-names";
import { formatDate, formatDateTimeMinute } from "@/lib/datetime";
import { cn } from "@/lib/utils";

// ── 文案 ──

export const KIND_LABEL: Record<string, string> = { amount_off: "立减", percent_off: "折扣" };

export const SCOPE_LABEL: Record<string, string> = {
  vip_purchase: "会员套餐",
  integral_purchase: "积分充值",
  goods: "普通商品",
  wallet_recharge: "余额充值"
};

export const PRICE_SCOPES: CouponScope[] = ["vip_purchase", "integral_purchase", "goods"];
export const REBATE_SCOPES: CouponScope[] = ["vip_purchase", "integral_purchase", "goods", "wallet_recharge"];

export const STATUS_META: Record<CouponStatus, { label: string; className: string }> = {
  draft: { label: "草稿", className: "border-border bg-muted text-muted-foreground" },
  active: { label: "进行中", className: "border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300" },
  paused: { label: "已暂停", className: "border-amber-500/30 bg-amber-500/10 text-amber-700 dark:text-amber-300" },
  ended: { label: "已结束", className: "border-border bg-muted/60 text-muted-foreground" }
};

export const USER_COUPON_STATUS_META: Record<UserCouponStatus, { label: string; className: string }> = {
  available: { label: "可用", className: "border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300" },
  not_started: { label: "未生效", className: "border-sky-500/30 bg-sky-500/10 text-sky-700 dark:text-sky-300" },
  locked: { label: "占用中", className: "border-amber-500/30 bg-amber-500/10 text-amber-700 dark:text-amber-300" },
  used: { label: "已使用", className: "border-violet-500/30 bg-violet-500/10 text-violet-700 dark:text-violet-300" },
  expired: { label: "已过期", className: "border-border bg-muted/60 text-muted-foreground" },
  revoked: { label: "已收回", className: "border-rose-500/30 bg-rose-500/10 text-rose-700 dark:text-rose-300" }
};

export const SOURCE_LABEL: Record<string, string> = {
  claim: "领券中心",
  admin: "管理员发放",
  rebate: "下单返券",
  card_key: "卡密兑换",
  points_store: "积分商城",
  lottery: "抽奖",
  item_grant: "物品发放",
  invite: "邀请与推广",
  leaderboard: "排行榜",
  refund: "退款返还"
};

export const AUDIENCE_LABEL: Record<AudienceKind, string> = {
  all: "所有用户",
  new_user: "新用户",
  first_order: "首次付款",
  non_vip: "非会员",
  vip: "会员"
};

export const PROMOTION_KIND_LABEL: Record<PromotionKind, string> = {
  discount: "限时折扣",
  threshold: "满减",
  rebate: "支付返券"
};

export const DISCOUNT_MODE_LABEL: Record<DiscountMode, string> = {
  percent: "打折",
  amount_off: "立减",
  fixed_price: "特价"
};

export const ORDER_STATUS_META: Record<OrderPromotionStatus, { label: string; className: string }> = {
  reserved: { label: "待付款", className: "border-amber-500/30 bg-amber-500/10 text-amber-700 dark:text-amber-300" },
  settled: { label: "已结算", className: "border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300" },
  released: { label: "已释放", className: "border-border bg-muted/60 text-muted-foreground" },
  refunded: { label: "已退款", className: "border-rose-500/30 bg-rose-500/10 text-rose-700 dark:text-rose-300" }
};

export const REBATE_STATUS_LABEL: Record<RebateStatus, string> = {
  none: "—",
  pending: "待付款后发放",
  granted: "已发放",
  skipped: "未达成",
  revoked: "已收回",
  failed: "发放失败"
};

export const REASON_LABEL: Record<string, string> = {
  locked: "被订单占用",
  not_started: "未生效",
  expired: "已过期",
  scope: "订单类型不适用",
  plan: "套餐不适用",
  threshold: "未达门槛",
  stack: "不能同享",
  audience: "人群不符",
  limit: "次数已满",
  sold_out: "已发完",
  budget: "预算不足",
  window: "不在时段内",
  no_discount: "已是最低价",
  status: "不可用"
};

// ── 色调：主题色板（不下发色值到客户端，控制台自己映射；深浅两套） ──

export const THEMES: CouponTheme[] = ["red", "orange", "gold", "green", "blue", "purple"];

export const THEME_META: Record<CouponTheme, { label: string; light: string; dark: string }> = {
  red: { label: "朱红", light: "#e5484d", dark: "#ff6369" },
  orange: { label: "橙", light: "#f76b15", dark: "#ff8b3e" },
  gold: { label: "金", light: "#c58b00", dark: "#f5c542" },
  green: { label: "青绿", light: "#12a594", dark: "#2ec8b0" },
  blue: { label: "蓝", light: "#0090ff", dark: "#3fa9ff" },
  purple: { label: "紫", light: "#8e4ec6", dark: "#b07ee8" }
};

/** 色调的 CSS 变量：组件里用 `var(--coupon)` 取主色（深浅色由 .dark 切换） */
export function themeStyle(theme?: string | null): React.CSSProperties {
  const meta = THEME_META[(theme as CouponTheme) || "red"] ?? THEME_META.red;
  return { ["--coupon-light" as string]: meta.light, ["--coupon-dark" as string]: meta.dark };
}

/** 用法：在根上挂 themeStyle，再给需要主色的元素加这个类 */
export const THEME_VAR = "[--coupon:var(--coupon-light)] dark:[--coupon:var(--coupon-dark)]";

// ── 金额 ──

/** "20.00" → "20"，"8.50" → "8.5"（按字符串处理，不转浮点） */
export function trimMoney(value?: string | null) {
  const raw = (value ?? "0").trim() || "0";
  if (!raw.includes(".")) return raw;
  return raw.replace(/0+$/, "").replace(/\.$/, "");
}

export function isPositive(value?: string | null) {
  const raw = (value ?? "").trim();
  return /^\+?\d*\.?\d+$/.test(raw) && /[1-9]/.test(raw);
}

/** 付款比例 → 「8.5 折」 */
export function rateLabel(rate?: number | null) {
  const r = Math.round(rate ?? 0);
  return r % 10 === 0 ? `${r / 10} 折` : `${Math.floor(r / 10)}.${r % 10} 折`;
}

/** 券面一句话，与服务端 coupon.TemplateFace 同一口径 */
export function templateFace(t: Pick<CouponTemplate, "kind" | "amount" | "rate" | "minAmount" | "maxDiscount" | "name">) {
  if (t.kind === "amount_off") {
    return isPositive(t.minAmount) ? `满 ${trimMoney(t.minAmount)} 减 ${trimMoney(t.amount)}` : `无门槛减 ${trimMoney(t.amount)}`;
  }
  if (t.kind === "percent_off") {
    let label = rateLabel(t.rate);
    if (isPositive(t.minAmount)) label = `满 ${trimMoney(t.minAmount)} 享 ${label}`;
    if (isPositive(t.maxDiscount)) label += `，最多减 ${trimMoney(t.maxDiscount)}`;
    return label;
  }
  return t.name;
}

/** 活动规则一句话 */
export function promotionRuleText(p: Pick<Promotion, "kind" | "rule">) {
  const rule = p.rule;
  switch (p.kind) {
    case "discount":
      if (rule.discountMode === "percent") {
        return `${rateLabel(rule.rate)}${isPositive(rule.maxDiscount) ? `，最多减 ${trimMoney(rule.maxDiscount)}` : ""}`;
      }
      if (rule.discountMode === "amount_off") return `立减 ${trimMoney(rule.amount)} 元`;
      if (rule.discountMode === "fixed_price") return `特价 ${trimMoney(rule.amount)} 元`;
      return "限时折扣";
    case "threshold":
      return (rule.tiers ?? []).map((tier) => `满 ${trimMoney(tier.minAmount)} 减 ${trimMoney(tier.off)}`).join("；") || "满减";
    case "rebate": {
      const parts: string[] = [];
      const coupons = (rule.rebateCoupons ?? []).reduce((sum, item) => sum + item.count, 0);
      if (coupons > 0) parts.push(`返券 ${coupons} 张`);
      if ((rule.rebateIntegral ?? 0) > 0) parts.push(`返积分 ${rule.rebateIntegral}`);
      const head = isPositive(rule.minAmount) ? `实付满 ${trimMoney(rule.minAmount)} ` : "付款后";
      return `${head}${parts.join("、") || "返利"}`;
    }
  }
  return "";
}

/** 活动默认角标，与服务端 coupon.PromotionTag 同一口径（tag 为空时用） */
export function defaultPromotionTag(p: Pick<Promotion, "kind" | "rule">) {
  const rule = p.rule;
  switch (p.kind) {
    case "discount":
      if (rule.discountMode === "percent") return `限时 ${rateLabel(rule.rate)}`;
      if (rule.discountMode === "amount_off") return `立减 ${trimMoney(rule.amount)}`;
      if (rule.discountMode === "fixed_price") return `特价 ${trimMoney(rule.amount)}`;
      return "限时折扣";
    case "threshold": {
      const tier = [...(rule.tiers ?? [])].sort((x, y) => Number(x.minAmount) - Number(y.minAmount))[0];
      return tier ? `满 ${trimMoney(tier.minAmount)} 减 ${trimMoney(tier.off)}` : "满减";
    }
    case "rebate":
      return (rule.rebateCoupons ?? []).length > 0 ? "下单返券" : "下单返积分";
  }
  return "";
}

export function audienceText(a?: CouponAudience | null) {
  if (!a || a.kind === "all") return "所有用户";
  if (a.kind === "new_user") return `注册 ${a.newUserDays ?? 0} 天内的新用户`;
  if (a.kind === "first_order") return "首次付款的用户";
  if (a.kind === "non_vip") return "非会员用户";
  if (a.kind === "vip") return "会员用户";
  return a.kind;
}

/** 有效期一句话 */
export function validityText(t: Pick<CouponTemplate, "validityMode" | "validFrom" | "validUntil" | "validDays" | "validDelayDays">) {
  if (t.validityMode === "relative") {
    const delay = t.validDelayDays ?? 0;
    return `${delay > 0 ? `领取 ${delay} 天后生效，` : "领取后"}${t.validDays ?? 0} 天内有效`;
  }
  const from = t.validFrom ? formatDate(t.validFrom) : "发放时";
  return `${from} 至 ${formatDateTimeMinute(t.validUntil)}`;
}

// ── 徽标 ──

export function StatusBadge({ status }: { status: CouponStatus }) {
  const meta = STATUS_META[status] ?? STATUS_META.draft;
  return (
    <Badge variant="outline" className={cn("h-5 px-1.5 text-[10px] font-medium", meta.className)}>
      {meta.label}
    </Badge>
  );
}

export function UserCouponStatusBadge({ status }: { status: string }) {
  const meta = USER_COUPON_STATUS_META[status as UserCouponStatus] ?? { label: status, className: "" };
  return (
    <Badge variant="outline" className={cn("h-5 px-1.5 text-[10px] font-medium", meta.className)}>
      {meta.label}
    </Badge>
  );
}

export function OrderStatusBadge({ status }: { status: OrderPromotionStatus }) {
  const meta = ORDER_STATUS_META[status] ?? { label: status, className: "" };
  return (
    <Badge variant="outline" className={cn("h-5 px-1.5 text-[10px] font-medium", meta.className)}>
      {meta.label}
    </Badge>
  );
}

export function ScopeChips({ scopes, className }: { scopes?: string[] | null; className?: string }) {
  return (
    <span className={cn("flex flex-wrap gap-1", className)}>
      {(scopes ?? []).map((scope) => (
        <span key={scope} className="rounded border border-border bg-muted/50 px-1.5 py-px text-[10px] text-muted-foreground">
          {SCOPE_LABEL[scope] ?? scope}
        </span>
      ))}
    </span>
  );
}

// ── 券面 ──

/** 券面左侧的大字：「¥20」「8.5折」 */
function FaceValue({ template, size }: { template: Pick<CouponTemplate, "kind" | "amount" | "rate">; size: "sm" | "md" }) {
  const big = size === "md" ? "text-3xl" : "text-xl";
  const small = size === "md" ? "text-sm" : "text-xs";
  if (template.kind === "percent_off") {
    const r = Math.round(template.rate ?? 0);
    const main = r % 10 === 0 ? String(r / 10) : `${Math.floor(r / 10)}.${r % 10}`;
    return (
      <span className="flex items-baseline gap-0.5 font-semibold tracking-tight tabular-nums">
        <span className={big}>{main}</span>
        <span className={small}>折</span>
      </span>
    );
  }
  return (
    <span className="flex items-baseline gap-0.5 font-semibold tracking-tight tabular-nums">
      <span className={small}>¥</span>
      <span className={big}>{trimMoney(template.amount)}</span>
    </span>
  );
}

/**
 * 一张券：左侧面额、右侧规则，中间一列齿孔（两端半圆缺口 + 虚线）。
 * 色调用 THEME_META 映射成主色，券底是主色 8%（深色 14%）叠在卡片色上，面额与边框用主色本身。
 */
export function CouponTicket({
  template,
  size = "md",
  dimmed,
  footer,
  aside,
  className
}: {
  template: Pick<
    CouponTemplate,
    "name" | "theme" | "kind" | "amount" | "rate" | "minAmount" | "maxDiscount" | "validityMode" | "validFrom" | "validUntil" | "validDays" | "validDelayDays" | "scopes"
  >;
  size?: "sm" | "md";
  dimmed?: boolean;
  /** 规则区底部附加一行（状态、计数） */
  footer?: React.ReactNode;
  /** 右上角（操作按钮） */
  aside?: React.ReactNode;
  className?: string;
}) {
  const stub = size === "md" ? "w-28" : "w-20";
  return (
    <div
      style={themeStyle(template.theme)}
      className={cn(
        THEME_VAR,
        "relative flex min-w-0 overflow-hidden rounded-xl border border-[color-mix(in_oklab,var(--coupon)_35%,transparent)] bg-[color-mix(in_oklab,var(--coupon)_8%,var(--card))] dark:bg-[color-mix(in_oklab,var(--coupon)_14%,var(--card))]",
        dimmed && "opacity-55 saturate-50",
        className
      )}
    >
      <div className={cn("flex shrink-0 flex-col items-center justify-center gap-0.5 px-2 py-3 text-[var(--coupon)]", stub)}>
        <FaceValue template={template} size={size} />
        <span className="max-w-full truncate text-[10px] opacity-80">
          {isPositive(template.minAmount) ? `满 ${trimMoney(template.minAmount)} 可用` : "无门槛"}
        </span>
      </div>
      {/* 齿孔：上下两个半圆缺口挖在卡片底色上，中间一条虚线 */}
      <div className="relative w-0 shrink-0">
        <span className="absolute -top-2 -left-2 size-4 rounded-full border border-[color-mix(in_oklab,var(--coupon)_35%,transparent)] bg-background" />
        <span className="absolute -bottom-2 -left-2 size-4 rounded-full border border-[color-mix(in_oklab,var(--coupon)_35%,transparent)] bg-background" />
        <span className="absolute inset-y-3 left-0 border-l border-dashed border-[color-mix(in_oklab,var(--coupon)_45%,transparent)]" />
      </div>
      <div className={cn("flex min-w-0 flex-1 flex-col justify-center gap-1 py-2.5 pr-3", size === "md" ? "pl-4" : "pl-3")}>
        <div className="flex items-start gap-2">
          <div className="min-w-0 flex-1">
            <div className={cn("truncate font-semibold", size === "md" ? "text-sm" : "text-xs")}>{template.name || "未命名的券"}</div>
            <div className="truncate text-[11px] text-muted-foreground">{templateFace(template)}</div>
          </div>
          {aside ? <div className="shrink-0">{aside}</div> : null}
        </div>
        <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-[10px] text-muted-foreground">
          <span className="truncate">{validityText(template)}</span>
          {size === "md" ? <ScopeChips scopes={template.scopes} /> : null}
        </div>
        {footer ? <div className="text-[11px]">{footer}</div> : null}
      </div>
    </div>
  );
}

/** 色调色块选择 */
export function ThemePicker({ value, onChange, disabled }: { value: CouponTheme; onChange: (theme: CouponTheme) => void; disabled?: boolean }) {
  return (
    <div className="flex flex-wrap gap-2">
      {THEMES.map((theme) => {
        const meta = THEME_META[theme];
        return (
          <button
            key={theme}
            type="button"
            disabled={disabled}
            title={meta.label}
            aria-label={meta.label}
            aria-pressed={value === theme}
            onClick={() => onChange(theme)}
            style={themeStyle(theme)}
            className={cn(
              THEME_VAR,
              "flex size-7 items-center justify-center rounded-full border-2 bg-[var(--coupon)] text-white transition-transform",
              value === theme ? "border-foreground" : "border-transparent hover:scale-110",
              disabled && "opacity-50"
            )}
          >
            {value === theme ? <Check className="size-3.5" /> : null}
          </button>
        );
      })}
    </div>
  );
}

// ── 奖励编辑器接入（目录 value="coupon"：refId = 券编号、amount = 张数） ──

/** 让奖励摘要里的券名可用：取一次券列表登记名字，并订阅登记表 */
export function useCouponRewardNames(appKey?: string | null) {
  const key = useRouteAppKey(appKey);
  useAllCouponTemplatesQuery(key);
  return useCouponNamesVersion();
}

/** 券选择器：只列没结束的（草稿可以先配上，上线后才发得出去） */
export function CouponPicker({
  appKey,
  value,
  onChange,
  disabled,
  className,
  excludeIds
}: {
  appKey?: string | null;
  value: number;
  onChange: (id: number, template?: CouponTemplate) => void;
  disabled?: boolean;
  className?: string;
  /** 不可再选的券（同一份奖励里已经配过的） */
  excludeIds?: number[];
}) {
  const key = useRouteAppKey(appKey);
  const [open, setOpen] = useState(false);
  const query = useAllCouponTemplatesQuery(key);
  const all = query.data?.items ?? [];
  const items = all.filter((item) => item.status !== "ended");
  const selected = all.find((item) => item.id === value);

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          type="button"
          variant="outline"
          disabled={disabled || !key}
          className={cn("h-8 w-full justify-between gap-2 px-2 font-normal sm:w-72", className)}
        >
          {selected ? (
            <span className="flex min-w-0 items-center gap-2" style={themeStyle(selected.theme)}>
              <span className={cn(THEME_VAR, "size-2.5 shrink-0 rounded-full bg-[var(--coupon)]")} />
              <span className="truncate text-sm">{selected.name}</span>
              <span className="shrink-0 truncate text-xs text-muted-foreground">{templateFace(selected)}</span>
              {selected.status !== "active" ? (
                <span className="shrink-0 text-xs text-amber-600 dark:text-amber-400">{STATUS_META[selected.status].label}</span>
              ) : null}
            </span>
          ) : (
            <span className="flex items-center gap-2 text-sm text-muted-foreground">
              <TicketPercent className="size-4" />
              {value ? `优惠券 #${value}` : "选择优惠券"}
            </span>
          )}
          <ChevronsUpDown className="size-3.5 shrink-0 text-muted-foreground" />
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-[360px] p-0" align="start">
        <Command>
          <CommandInput placeholder="搜索优惠券" />
          <CommandList className="max-h-80">
            <CommandEmpty>{query.isLoading ? "加载中" : query.isError ? "无法读取优惠券" : "还没有可用的优惠券"}</CommandEmpty>
            <CommandGroup>
              {items.map((item) => {
                const taken = excludeIds?.includes(item.id) && item.id !== value;
                return (
                  <CommandItem
                    key={item.id}
                    value={`${item.name} ${item.id}`}
                    disabled={taken}
                    onSelect={() => {
                      onChange(item.id, item);
                      setOpen(false);
                    }}
                    className="gap-2"
                  >
                    <span style={themeStyle(item.theme)} className={cn(THEME_VAR, "size-2.5 shrink-0 rounded-full bg-[var(--coupon)]")} />
                    <span className="flex min-w-0 flex-1 flex-col">
                      <span className="truncate text-sm">{item.name}</span>
                      <span className="truncate text-[11px] text-muted-foreground">{templateFace(item)}</span>
                    </span>
                    <StatusBadge status={item.status} />
                    {item.id === value ? <Check className="size-3.5" /> : null}
                  </CommandItem>
                );
              })}
            </CommandGroup>
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  );
}

/** 奖励行里的优惠券：选择器 + 张数 */
export function CouponRewardInput({
  appKey,
  refId,
  amount,
  min = 1,
  max = 20,
  onChange,
  disabled
}: {
  appKey?: string | null;
  refId: number;
  amount: string | number;
  min?: number;
  max?: number;
  onChange: (next: { refId?: number; amount?: string }) => void;
  disabled?: boolean;
}) {
  return (
    <div className="flex flex-wrap items-center gap-2">
      <CouponPicker appKey={appKey} value={refId} onChange={(id) => onChange({ refId: id })} disabled={disabled} />
      <Input
        type="number"
        className="h-8 w-20"
        value={String(amount)}
        min={min}
        max={max}
        disabled={disabled}
        onChange={(event) => onChange({ amount: event.target.value })}
      />
      <span className="text-xs text-muted-foreground">
        张（{min}–{max}）
      </span>
    </div>
  );
}
