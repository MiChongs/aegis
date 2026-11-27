"use client";

import { Info, Plus, Trash2 } from "lucide-react";
import { nextRowKey, type RewardRow } from "@/components/apps/card-key/card-key-batch-form";
import { CosmeticRewardInput } from "@/components/apps/cosmetics/cosmetic-reward";
import { CouponRewardInput } from "@/components/apps/coupons/coupon-shared";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue
} from "@/components/ui/select";
import type { CardKeyCatalog, CardKeyKind, CardKeyRewardSpec } from "@/lib/api/card-key";
import { NUMBER_TICKET_LEVELS } from "@/lib/api/user-number";
import { cn } from "@/lib/utils";

/**
 * 权益编辑：一行一项，类型在添加时选定。
 *
 * 表单**完全由后端目录驱动**（`/card-keys/catalog`）：可选档位、单位、上下界、提示、能否重复都来自目录；
 * 套餐与功能的下拉也随目录一起下发。在这里另抄一份枚举会同时招来「后端加了一档控制台
 * 配不出来」与「控制台能配、保存时报不支持」。
 */
export function CardKeyRewardEditor({
  catalog,
  kind,
  rows,
  onChange,
  disabled
}: {
  catalog?: CardKeyCatalog | null;
  kind: CardKeyKind;
  rows: RewardRow[];
  onChange: (rows: RewardRow[]) => void;
  disabled?: boolean;
}) {
  const specs = catalog?.rewards ?? [];
  const maxRewards = catalog?.maxRewards ?? 8;
  const plans = catalog?.plans ?? [];
  const features = (catalog?.features ?? []).filter((feature) => feature.isActive);

  const canAdd = (spec: CardKeyRewardSpec) =>
    rows.length < maxRewards && (spec.repeatable || !rows.some((row) => row.type === spec.type));

  const add = (spec: CardKeyRewardSpec) => {
    const usedFeatures = new Set(rows.filter((row) => row.type === spec.type).map((row) => row.feature));
    onChange([
      ...rows,
      {
        key: nextRowKey(rows),
        type: spec.type,
        amount: spec.value === "feature" || spec.value === "cosmetic" ? "0" : String(spec.min ?? 1),
        money: "10.00",
        refId: spec.value === "number_ticket" ? 1 : spec.value === "cosmetic" || spec.value === "coupon" ? 0 : (plans.find((plan) => !plan.trial)?.id ?? plans[0]?.id ?? 0),
        feature: features.find((feature) => !usedFeatures.has(feature.tag))?.tag ?? ""
      }
    ]);
  };

  const patch = (key: number, next: Partial<RewardRow>) =>
    onChange(rows.map((row) => (row.key === key ? { ...row, ...next } : row)));

  const remove = (key: number) => onChange(rows.filter((row) => row.key !== key));

  return (
    <div className="space-y-2">
      {rows.length === 0 ? (
        <p className="rounded-xl border border-dashed border-border px-4 py-5 text-center text-xs text-muted-foreground">
          {kind === "login" ? "授权卡可以不带权益：卡本身就是登录凭证" : "兑换卡至少配置一项权益"}
        </p>
      ) : null}

      {rows.map((row) => {
        const spec = specs.find((item) => item.type === row.type);
        if (!spec) return null;
        return (
          <div key={row.key} className="rounded-xl border border-border bg-muted/30 p-3">
            <div className="flex items-start justify-between gap-2">
              <div className="min-w-0 space-y-0.5">
                <p className="text-xs font-medium">{spec.label}</p>
                <p className="text-[11px] leading-snug text-muted-foreground">{spec.hint}</p>
              </div>
              <Button
                type="button"
                size="icon"
                variant="ghost"
                className="size-7 shrink-0"
                onClick={() => remove(row.key)}
                disabled={disabled}
                aria-label={`移除${spec.label}`}
              >
                <Trash2 className="size-3.5" />
              </Button>
            </div>

            <div className="mt-2.5 grid gap-2 sm:grid-cols-[minmax(0,1fr)_auto]">
              {spec.value === "amount" ? (
                <AmountInput
                  value={row.amount}
                  unit={spec.unit}
                  min={spec.min}
                  max={spec.max}
                  disabled={disabled}
                  onChange={(amount) => patch(row.key, { amount })}
                />
              ) : null}
              {spec.value === "money" ? (
                <AmountInput
                  value={row.money}
                  unit="元"
                  step="0.01"
                  min={0.01}
                  disabled={disabled}
                  onChange={(money) => patch(row.key, { money })}
                />
              ) : null}
              {spec.value === "ref" ? (
                <Select
                  value={row.refId ? String(row.refId) : ""}
                  onValueChange={(value) => patch(row.key, { refId: Number(value) })}
                  disabled={disabled}
                >
                  <SelectTrigger className="h-9 w-full">
                    <SelectValue placeholder={plans.length ? "选择会员套餐" : "暂无会员套餐"} />
                  </SelectTrigger>
                  <SelectContent position="popper">
                    {plans.map((plan) => (
                      <SelectItem key={plan.id} value={String(plan.id)}>
                        {plan.name}
                        <span className="ml-1.5 text-[11px] text-muted-foreground">
                          {plan.lifetime ? "永久" : `${plan.durationDays} 天`}
                          {plan.trial ? "，试用" : ""}
                        </span>
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              ) : null}
              {spec.value === "number_ticket" ? (
                <>
                  <Select
                    value={String(row.refId || 1)}
                    onValueChange={(value) => patch(row.key, { refId: Number(value) })}
                    disabled={disabled}
                  >
                    <SelectTrigger className="h-9 w-full">
                      <SelectValue placeholder="选择等级" />
                    </SelectTrigger>
                    <SelectContent position="popper">
                      {NUMBER_TICKET_LEVELS.map((tier) => (
                        <SelectItem key={tier.level} value={String(tier.level)}>
                          {tier.name}靓号券
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                  <AmountInput
                    value={row.amount}
                    unit={spec.unit ?? "张"}
                    min={spec.min ?? 1}
                    max={spec.max}
                    disabled={disabled}
                    className="sm:w-44"
                    onChange={(amount) => patch(row.key, { amount })}
                  />
                </>
              ) : null}
              {spec.value === "feature" ? (
                <>
                  <Select
                    value={row.feature}
                    onValueChange={(feature) => patch(row.key, { feature })}
                    disabled={disabled}
                  >
                    <SelectTrigger className="h-9 w-full">
                      <SelectValue placeholder={features.length ? "选择功能" : "暂无启用中的功能"} />
                    </SelectTrigger>
                    <SelectContent position="popper">
                      {features.map((feature) => (
                        <SelectItem key={feature.tag} value={feature.tag}>
                          {feature.name || feature.tag}
                          <span className="ml-1.5 font-mono text-[11px] text-muted-foreground">{feature.tag}</span>
                        </SelectItem>
                      ))}
                      {row.feature && !features.some((feature) => feature.tag === row.feature) ? (
                        <SelectItem value={row.feature}>{row.feature}（已停用）</SelectItem>
                      ) : null}
                    </SelectContent>
                  </Select>
                  <AmountInput
                    value={row.amount}
                    unit={Number(row.amount) === 0 ? "天，0 为永久" : "天"}
                    min={0}
                    max={spec.max}
                    disabled={disabled}
                    className="sm:w-44"
                    onChange={(amount) => patch(row.key, { amount })}
                  />
                </>
              ) : null}
              {spec.value === "cosmetic" ? (
                <CosmeticRewardInput
                  refId={row.refId}
                  amount={row.amount}
                  max={spec.max}
                  disabled={disabled}
                  onChange={(next) => patch(row.key, next)}
                />
              ) : null}
              {spec.value === "coupon" ? (
                <CouponRewardInput
                  refId={row.refId}
                  amount={row.amount}
                  min={spec.min ?? 1}
                  max={spec.max ?? 20}
                  disabled={disabled}
                  onChange={(next) => patch(row.key, next)}
                />
              ) : null}
            </div>

            {spec.needsLoginCard ? (
              <p className="mt-2 flex items-center gap-1 text-[11px] text-amber-700 dark:text-amber-400">
                <Info className="size-3" />
                领取人名下必须有授权卡，否则兑换会被拒绝
              </p>
            ) : null}
          </div>
        );
      })}

      <div className="flex items-center justify-between gap-2">
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button type="button" size="sm" variant="outline" disabled={disabled || rows.length >= maxRewards}>
              <Plus className="size-3.5" /> 添加权益
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="start" className="w-64">
            {specs.map((spec) => (
              <DropdownMenuItem key={spec.type} disabled={!canAdd(spec)} onSelect={() => add(spec)}>
                <div className="min-w-0">
                  <p className="text-xs font-medium">{spec.label}</p>
                  <p className="truncate text-[11px] text-muted-foreground">{spec.hint}</p>
                </div>
              </DropdownMenuItem>
            ))}
          </DropdownMenuContent>
        </DropdownMenu>
        <span className="text-[11px] tabular-nums text-muted-foreground">
          {rows.length}/{maxRewards}
        </span>
      </div>
    </div>
  );
}

function AmountInput({
  value,
  onChange,
  unit,
  min,
  max,
  step,
  disabled,
  className
}: {
  value: string;
  onChange: (value: string) => void;
  unit?: string;
  min?: number;
  max?: number;
  step?: string;
  disabled?: boolean;
  className?: string;
}) {
  return (
    <div className={cn("relative", className)}>
      <Input
        type="number"
        inputMode="decimal"
        value={value}
        min={min}
        max={max}
        step={step ?? "1"}
        disabled={disabled}
        onChange={(event) => onChange(event.target.value)}
        className="h-9 pr-24 tabular-nums"
      />
      {unit ? (
        <span className="pointer-events-none absolute inset-y-0 right-3 flex items-center text-[11px] text-muted-foreground">
          {unit}
        </span>
      ) : null}
    </div>
  );
}
