"use client";

import { FieldGroup, NumberField } from "@/components/apps/app-config-primitives";
import {
  DEFAULT_FEATURE_MINUTES,
  type LifetimeDraft,
  type SceneDraft
} from "@/components/apps/rewarded-ad/rewarded-ad-config-draft";
import { vipPlanTerm } from "@/components/apps/vip/vip-shared";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import type { CardKeyRewardSpec } from "@/lib/api/card-key";
import type { RewardedAdLifetimePolicy, RewardedAdLifetimePolicySpec } from "@/lib/api/rewarded-ad";
import type { VipFeature, VipPlan } from "@/lib/api/vip";
import { cn } from "@/lib/utils";

/**
 * 场景编辑器里的几块字段：奖励选择、功能时长、已拥有权益的处理。
 * 权益目录由后端随配置下发（见控制台 CLAUDE.md「激励广告区块」第 1 条），这里只按目录渲染。
 */

type RewardValue = SceneDraft["rewards"][string];

/** 奖励：每档一张可勾选的小卡，勾上后在卡内填数量 / 选套餐 / 选功能。 */
export function RewardPicker({
  scene,
  catalog,
  plans,
  features,
  onChange
}: {
  scene: SceneDraft;
  catalog: CardKeyRewardSpec[];
  plans: VipPlan[];
  features: VipFeature[];
  onChange: (rewards: SceneDraft["rewards"]) => void;
}) {
  const toggle = (spec: CardKeyRewardSpec, checked: boolean) => {
    const rewards = { ...scene.rewards };
    if (checked) {
      const amount =
        spec.value === "feature"
          ? Math.min(Math.max(DEFAULT_FEATURE_MINUTES, spec.min ?? 1), spec.max ?? DEFAULT_FEATURE_MINUTES)
          : spec.min ?? 1;
      rewards[spec.type] = {
        amount: String(amount),
        refId: plans[0]?.id ?? 0,
        feature: features.find((feature) => feature.isActive)?.tag ?? ""
      };
    } else {
      delete rewards[spec.type];
    }
    onChange(rewards);
  };
  const update = (type: string, value: RewardValue) => onChange({ ...scene.rewards, [type]: value });

  return (
    <div className="grid gap-2 sm:grid-cols-2">
      {catalog.map((spec) => {
        const value = scene.rewards[spec.type];
        return (
          <div
            key={spec.type}
            className={cn(
              "rounded-xl border p-3 transition-colors duration-150",
              value ? "border-emerald-500/40 bg-emerald-500/5" : "border-border hover:bg-muted/40"
            )}
          >
            <label className="flex cursor-pointer items-center gap-2.5">
              <Checkbox checked={Boolean(value)} onCheckedChange={(checked) => toggle(spec, Boolean(checked))} />
              <span className="text-xs font-medium">{spec.label}</span>
            </label>
            {value ? (
              <div className="mt-2.5 pl-7">
                {spec.value === "ref" ? (
                  plans.length === 0 ? (
                    <p className="text-xs text-muted-foreground">暂无可用的会员套餐</p>
                  ) : (
                    <Select
                      value={String(value.refId || plans[0].id)}
                      onValueChange={(next) => update(spec.type, { ...value, refId: Number(next) })}
                    >
                      <SelectTrigger className="h-8 w-full text-xs">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        {plans.map((plan) => (
                          <SelectItem key={plan.id} value={String(plan.id)}>
                            {plan.name}（{vipPlanTerm(plan)}）
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  )
                ) : spec.value === "feature" ? (
                  <FeatureAmountField
                    spec={spec}
                    value={value}
                    features={features}
                    onChange={(next) => update(spec.type, { ...value, ...next })}
                  />
                ) : (
                  <AmountInput
                    spec={spec}
                    value={value.amount}
                    onChange={(amount) => update(spec.type, { ...value, amount })}
                  />
                )}
              </div>
            ) : null}
          </div>
        );
      })}
    </div>
  );
}

function AmountInput({
  spec,
  value,
  onChange
}: {
  spec: CardKeyRewardSpec;
  value: string;
  onChange: (value: string) => void;
}) {
  return (
    <div className="flex items-center gap-2">
      <Input
        type="number"
        inputMode="numeric"
        className="h-8 w-28 font-mono text-xs tabular-nums"
        value={value}
        min={spec.min}
        max={spec.max}
        onChange={(event) => onChange(event.target.value)}
      />
      <span className="text-xs text-muted-foreground">{spec.unit}</span>
    </div>
  );
}

/**
 * 功能时长：选一个会员功能，再填分钟数。
 * 已保存的标识若已从功能目录删除，仍列出来并标「未登记」，避免下拉框显示为空。
 */
function FeatureAmountField({
  spec,
  value,
  features,
  onChange
}: {
  spec: CardKeyRewardSpec;
  value: { amount: string; feature: string };
  features: VipFeature[];
  onChange: (next: { amount?: string; feature?: string }) => void;
}) {
  const orphan = value.feature && !features.some((feature) => feature.tag === value.feature) ? value.feature : "";
  if (features.length === 0 && !orphan) {
    return <p className="text-xs text-muted-foreground">暂无功能标识</p>;
  }
  return (
    <div className="space-y-2">
      <Select value={value.feature} onValueChange={(next) => onChange({ feature: next })}>
        <SelectTrigger className="h-8 w-full text-xs">
          <SelectValue placeholder="选择功能" />
        </SelectTrigger>
        {/* 还没选功能时没有对齐项，弹层改为贴着触发器向下展开 */}
        <SelectContent position="popper">
          {orphan ? (
            <SelectItem value={orphan} disabled>
              {orphan}
              <span className="ml-1.5 text-[10px] text-muted-foreground">未登记</span>
            </SelectItem>
          ) : null}
          {features.map((feature) => (
            <SelectItem
              key={feature.tag}
              value={feature.tag}
              disabled={!feature.isActive && feature.tag !== value.feature}
            >
              {feature.name || feature.tag}
              <span className="ml-1.5 font-mono text-[10px] text-muted-foreground">{feature.tag}</span>
              {!feature.isActive ? <span className="ml-1.5 text-[10px] text-muted-foreground">已停用</span> : null}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      <AmountInput spec={spec} value={value.amount} onChange={(amount) => onChange({ amount })} />
    </div>
  );
}

/** 「会员天数」带哪些功能。功能只对会员天数生效，会员套餐的功能跟随套餐。 */
export function MembershipFeaturePicker({
  selected,
  features,
  onChange
}: {
  selected: string[];
  features: VipFeature[];
  onChange: (next: string[]) => void;
}) {
  if (features.length === 0) {
    return (
      <p className="rounded-xl border border-dashed border-border px-3 py-4 text-center text-[11px] text-muted-foreground">
        暂无功能标识
      </p>
    );
  }
  return (
    <div className="grid gap-1 rounded-xl border border-border p-2 sm:grid-cols-2">
      {features.map((feature) => (
        <label
          key={feature.tag}
          className={cn(
            "flex cursor-pointer items-center gap-2.5 rounded-lg px-2 py-1.5 transition-colors hover:bg-muted/60",
            !feature.isActive && "opacity-60"
          )}
        >
          <Checkbox
            checked={selected.includes(feature.tag)}
            onCheckedChange={(checked) =>
              onChange(checked ? [...selected, feature.tag] : selected.filter((tag) => tag !== feature.tag))
            }
          />
          <span className="truncate text-xs font-medium">{feature.name}</span>
          <code className="rounded bg-muted px-1 text-[10px] text-muted-foreground">{feature.tag}</code>
          {!feature.isActive ? <span className="text-[10px] text-muted-foreground">已停用</span> : null}
        </label>
      ))}
    </div>
  );
}

/**
 * 永久会员（或已永久拥有某功能的人）看这个场景时，已拥有的会员、套餐、功能时长怎么办。
 * 只在场景配了这几档时出现；判定与折算都在服务端结算事务里。
 */
export function LifetimeField({
  value,
  policies,
  targets,
  onChange
}: {
  value: LifetimeDraft;
  policies: RewardedAdLifetimePolicySpec[];
  targets: CardKeyRewardSpec[];
  onChange: (next: Partial<LifetimeDraft>) => void;
}) {
  const active = policies.find((policy) => policy.key === value.policy);
  const target = targets.find((spec) => spec.type === value.target);
  return (
    <FieldGroup label="已拥有的权益" hint="永久会员或已永久解锁功能的用户">
      <div className="space-y-3 rounded-xl border border-border p-3">
        <div className="space-y-1.5">
          <Select value={value.policy} onValueChange={(next) => onChange({ policy: next as RewardedAdLifetimePolicy })}>
            <SelectTrigger className="h-8 w-full text-xs sm:w-56">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {policies.map((policy) => (
                <SelectItem key={policy.key} value={policy.key}>
                  {policy.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          {active ? <p className="text-[11px] text-muted-foreground">{active.hint}</p> : null}
        </div>

        {value.policy === "convert" || value.policy === "hide" ? (
          <div className="grid gap-3 sm:grid-cols-3">
            <div className="space-y-1.5">
              <Label className="text-[11px] font-medium">折算为</Label>
              <Select value={value.target} onValueChange={(next) => onChange({ target: next })}>
                <SelectTrigger className="h-8 w-full text-xs">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {targets.map((spec) => (
                    <SelectItem key={spec.type} value={spec.type}>
                      {spec.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <NumberField
              label="会员每天"
              unit={target?.unit ?? ""}
              min={0}
              max={100000}
              hint="会员天数与套餐天数"
              value={value.perVipDay}
              onChange={(next) => onChange({ perVipDay: next })}
            />
            <NumberField
              label="功能时长每小时"
              unit={target?.unit ?? ""}
              min={0}
              max={100000}
              hint="不足一小时按比例向上取整"
              value={value.perFeatureHour}
              onChange={(next) => onChange({ perFeatureHour: next })}
            />
          </div>
        ) : null}

        {value.policy === "replace" ? (
          <div className="grid gap-2 sm:grid-cols-2">
            {targets.map((spec) => {
              const amount = value.rewards[spec.type];
              return (
                <div key={spec.type} className="flex items-center gap-2.5 rounded-lg border border-border px-3 py-2">
                  <Checkbox
                    checked={amount !== undefined}
                    onCheckedChange={(checked) => {
                      const rewards = { ...value.rewards };
                      if (checked) {
                        rewards[spec.type] = String(spec.min ?? 1);
                      } else {
                        delete rewards[spec.type];
                      }
                      onChange({ rewards });
                    }}
                  />
                  <span className="min-w-0 flex-1 truncate text-xs font-medium">{spec.label}</span>
                  {amount !== undefined ? (
                    <>
                      <Input
                        type="number"
                        className="h-7 w-24 font-mono text-xs"
                        value={amount}
                        min={spec.min}
                        max={spec.max}
                        onChange={(event) =>
                          onChange({ rewards: { ...value.rewards, [spec.type]: event.target.value } })
                        }
                      />
                      <span className="text-xs text-muted-foreground">{spec.unit}</span>
                    </>
                  ) : null}
                </div>
              );
            })}
          </div>
        ) : null}
      </div>
    </FieldGroup>
  );
}
