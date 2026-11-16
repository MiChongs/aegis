"use client";

import type { ReactNode } from "react";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { useNow } from "@/components/users/ban-form";
import { compareDecimal, isPositiveDecimal } from "@/components/users/detail/user-assets-adjust";
import type { WalletPackage } from "@/lib/api/types";
import { cn } from "@/lib/utils";

/**
 * 钱包设置与充值档位共用的表单部件与金额校验。
 *
 * 金额全程是字符串（后端 shopspring/decimal），比较走 `compareDecimal`（BigInt 放大），不经过 Number。
 * 校验口径与后端 `internal/domain/wallet/settings.go` 一致：最多两位小数、不超过 1,000,000；
 * 前端先拦一遍只为少一次往返，后端仍会按同样规则再校验。
 */

const MONEY_RE = /^\d+(\.\d{1,2})?$/;
export const MAX_WALLET_AMOUNT = "1000000";

/** 金额格式：非负、最多两位小数 */
export function isMoneyText(value: string) {
  return MONEY_RE.test(value.trim());
}

/**
 * 校验一个金额字段，返回问题描述或 null。
 * `allowZero` 为真时 0（或留空）表示「不限」。
 */
export function moneyProblem(label: string, value: string, allowZero = false): string | null {
  const text = value.trim();
  if (!text) return allowZero ? null : `请填写${label}`;
  if (!isMoneyText(text)) return `${label}最多保留两位小数`;
  if (!allowZero && !isPositiveDecimal(text)) return `${label}必须大于 0`;
  if ((compareDecimal(text, MAX_WALLET_AMOUNT) ?? 0) > 0) return `${label}不能超过 1,000,000`;
  return null;
}

/** 只留数字与小数点，输入框里不出现负号与字母 */
export function sanitizeMoney(value: string) {
  return value.replace(/[^\d.]/g, "");
}

export function sanitizeInteger(value: string) {
  return value.replace(/[^\d]/g, "");
}

export function WalletField({
  label,
  hint,
  counter,
  children,
  className
}: {
  label: string;
  hint?: ReactNode;
  counter?: string;
  children: ReactNode;
  className?: string;
}) {
  return (
    <div className={cn("flex min-w-0 flex-col gap-1.5", className)}>
      <div className="flex items-baseline justify-between gap-2">
        <Label className="text-xs font-medium">{label}</Label>
        {counter ? <span className="text-xs tabular-nums text-muted-foreground">{counter}</span> : null}
      </div>
      {children}
      {hint ? <p className="text-xs text-muted-foreground">{hint}</p> : null}
    </div>
  );
}

/** 带单位的金额 / 整数输入框 */
export function AmountInput({
  value,
  onChange,
  suffix,
  placeholder,
  disabled,
  integer = false,
  invalid = false
}: {
  value: string;
  onChange: (value: string) => void;
  suffix?: string;
  placeholder?: string;
  disabled?: boolean;
  integer?: boolean;
  invalid?: boolean;
}) {
  return (
    <div className="relative">
      <Input
        value={value}
        inputMode={integer ? "numeric" : "decimal"}
        placeholder={placeholder ?? (integer ? "0" : "0.00")}
        disabled={disabled}
        maxLength={integer ? 10 : 12}
        aria-invalid={invalid || undefined}
        className={cn("tabular-nums", suffix && "pr-14")}
        onChange={(event) => onChange(integer ? sanitizeInteger(event.target.value) : sanitizeMoney(event.target.value))}
      />
      {suffix ? (
        <span className="pointer-events-none absolute inset-y-0 right-3 flex items-center text-xs text-muted-foreground">
          {suffix}
        </span>
      ) : null}
    </div>
  );
}

/** 一行开关：标题、说明、右侧 Switch */
export function WalletToggleRow({
  label,
  hint,
  checked,
  onChange,
  disabled
}: {
  label: string;
  hint: ReactNode;
  checked: boolean;
  onChange: (value: boolean) => void;
  disabled?: boolean;
}) {
  return (
    <label
      className={cn(
        "flex items-center justify-between gap-3 rounded-lg border px-3 py-2.5 transition-colors",
        disabled ? "cursor-not-allowed opacity-60" : "cursor-pointer hover:bg-muted/40"
      )}
    >
      <div className="flex min-w-0 flex-col gap-0.5">
        <span className="text-sm font-medium">{label}</span>
        <span className="text-xs text-muted-foreground">{hint}</span>
      </div>
      <Switch checked={checked} onCheckedChange={onChange} disabled={disabled} />
    </label>
  );
}

// ── 档位状态 ──

export type WalletPackageState = "on_sale" | "inactive" | "not_started" | "ended";

/** 与后端 RechargePackage.Availability 同序：先看上下架，再看起止时间 */
export function walletPackageState(item: Pick<WalletPackage, "isActive" | "startsAt" | "endsAt">, now: number): WalletPackageState {
  if (!item.isActive) return "inactive";
  const starts = item.startsAt ? new Date(item.startsAt).getTime() : Number.NaN;
  const ends = item.endsAt ? new Date(item.endsAt).getTime() : Number.NaN;
  if (Number.isFinite(starts) && now < starts) return "not_started";
  if (Number.isFinite(ends) && now >= ends) return "ended";
  return "on_sale";
}

const STATE_META: Record<WalletPackageState, { label: string; variant: "success" | "secondary" | "warning" | "outline" }> = {
  on_sale: { label: "在售", variant: "success" },
  not_started: { label: "未开始", variant: "warning" },
  ended: { label: "已结束", variant: "outline" },
  inactive: { label: "已下架", variant: "secondary" }
};

export const WALLET_PACKAGE_STATE_LABEL: Record<WalletPackageState, string> = {
  on_sale: STATE_META.on_sale.label,
  not_started: STATE_META.not_started.label,
  ended: STATE_META.ended.label,
  inactive: STATE_META.inactive.label
};

export function WalletPackageStatusBadge({ item }: { item: Pick<WalletPackage, "isActive" | "startsAt" | "endsAt"> }) {
  const now = useNow();
  const meta = STATE_META[walletPackageState(item, now)];
  return (
    <Badge variant={meta.variant} size="sm">
      {meta.label}
    </Badge>
  );
}
