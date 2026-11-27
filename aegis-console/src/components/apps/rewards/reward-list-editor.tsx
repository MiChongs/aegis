"use client";

import { Plus, Trash2 } from "lucide-react";
import { CosmeticRewardInput } from "@/components/apps/cosmetics/cosmetic-reward";
import { CouponRewardInput } from "@/components/apps/coupons/coupon-shared";
import { termLabel } from "@/components/apps/points-store/points-store-shared";
import { vipPlanTerm } from "@/components/apps/vip/vip-shared";
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
import type { PointsStoreReward, PointsStoreRewardSpec } from "@/lib/api/points-store";
import { NUMBER_TICKET_LEVELS } from "@/lib/api/user-number";
import type { VipFeature, VipPlan } from "@/lib/api/vip";

/**
 * 目录驱动的权益列表编辑器：积分商城的商品与邀请奖励共用。
 *
 * 能加哪些、单位与上下界、哪一档能重复（功能解锁）、哪一档只能单独存在（人工发放）都来自后端目录，
 * 这里不另抄一份。会员套餐只给非试用套餐（调用方传入时已过滤）。
 */

/** 一项权益的草稿。数字一律按字符串存：存 number 的输入框清不空。 */
export type RewardDraft = {
  key: number;
  type: string;
  amount: string;
  money: string;
  refId: number;
  tag: string;
  text: string;
};

/** 行键只在一份草稿内唯一即可：新加的一行取现有最大值加一 */
const nextRewardKey = (rewards: RewardDraft[]) => rewards.reduce((max, reward) => Math.max(max, reward.key), 0) + 1;

export function rewardDraftsFrom(rewards?: PointsStoreReward[] | null): RewardDraft[] {
  return (rewards ?? []).map((reward, index) => ({
    key: index + 1,
    type: reward.type,
    amount: String(reward.amount ?? 0),
    money: reward.money && reward.money !== "0" ? reward.money : "",
    refId: reward.refId ?? 0,
    tag: reward.tag ?? "",
    text: reward.text ?? ""
  }));
}

/** 草稿 → 载荷：只带取值形态需要的字段，金额按字符串 */
export function rewardsFromDrafts(drafts: RewardDraft[], catalog: PointsStoreRewardSpec[]): PointsStoreReward[] {
  return drafts.map((reward) => {
    const spec = catalog.find((item) => item.type === reward.type);
    switch (spec?.value) {
      case "money":
        return { type: reward.type, money: reward.money.trim() };
      case "ref":
        return { type: reward.type, refId: Number(reward.refId) };
      case "number_ticket":
        return { type: reward.type, amount: Number(reward.amount) || 0, refId: Number(reward.refId) || 1 };
      case "feature":
        return { type: reward.type, tag: reward.tag, amount: Number(reward.amount) || 0 };
      case "cosmetic":
        return { type: reward.type, refId: Number(reward.refId) || 0, amount: Number(reward.amount) || 0 };
      case "coupon":
        return { type: reward.type, refId: Number(reward.refId) || 0, amount: Number(reward.amount) || 1 };
      case "text":
        return { type: reward.type, text: reward.text.trim() };
      default:
        return { type: reward.type, amount: Number(reward.amount) || 0 };
    }
  });
}

export function RewardListEditor({
  catalog,
  maxRewards,
  rewards,
  plans,
  features,
  onChange,
  emptyText = "暂无权益",
  addLabel = "添加权益"
}: {
  catalog: PointsStoreRewardSpec[];
  maxRewards: number;
  rewards: RewardDraft[];
  /** 可选的会员套餐（已去掉试用套餐） */
  plans: VipPlan[];
  features: VipFeature[];
  onChange: (rewards: RewardDraft[]) => void;
  emptyText?: string;
  addLabel?: string;
}) {
  const exclusiveChosen = rewards.some((reward) => catalog.find((spec) => spec.type === reward.type)?.exclusive);

  const addable = catalog.filter((spec) => {
    if (rewards.length >= maxRewards || exclusiveChosen) return false;
    if (spec.exclusive && rewards.length > 0) return false;
    if (!spec.repeatable && rewards.some((reward) => reward.type === spec.type)) return false;
    return true;
  });

  const addReward = (type: string) => {
    const spec = catalog.find((item) => item.type === type);
    if (!spec) return;
    const usedTags = rewards.map((reward) => reward.tag);
    onChange([
      ...rewards,
      {
        key: nextRewardKey(rewards),
        type,
        amount: String(spec.value === "feature" ? 7 : spec.value === "cosmetic" ? 0 : spec.min ?? 1),
        money: "1.00",
        refId: spec.value === "number_ticket" ? 1 : spec.value === "cosmetic" || spec.value === "coupon" ? 0 : (plans[0]?.id ?? 0),
        tag: features.find((feature) => feature.isActive && !usedTags.includes(feature.tag))?.tag ?? "",
        text: ""
      }
    ]);
  };

  const patchReward = (key: number, next: Partial<RewardDraft>) =>
    onChange(rewards.map((reward) => (reward.key === key ? { ...reward, ...next } : reward)));

  const removeReward = (key: number) => onChange(rewards.filter((reward) => reward.key !== key));

  return (
    <div className="space-y-2">
      {rewards.length === 0 ? (
        <p className="rounded-lg border border-dashed border-border px-3 py-4 text-center text-xs text-muted-foreground">
          {emptyText}
        </p>
      ) : null}
      {rewards.map((reward) => (
        <RewardRow
          key={reward.key}
          reward={reward}
          spec={catalog.find((item) => item.type === reward.type)}
          plans={plans}
          features={features}
          takenTags={rewards.filter((other) => other.key !== reward.key).map((other) => other.tag)}
          onChange={(next) => patchReward(reward.key, next)}
          onRemove={() => removeReward(reward.key)}
        />
      ))}
      {addable.length > 0 ? (
        // 「添加」是动作不是选值：用菜单而不是 Select。Select 没有选中值时
        // 默认的 item-aligned 定位找不到对齐项，弹层会偏出视野。
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button
              variant="outline"
              size="sm"
              className="h-8 w-full justify-start border-dashed text-xs font-normal text-muted-foreground"
            >
              <Plus className="size-3.5" /> {addLabel}
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="start" className="w-(--radix-dropdown-menu-trigger-width)">
            {addable.map((spec) => (
              <DropdownMenuItem key={spec.type} onSelect={() => addReward(spec.type)}>
                {spec.label}
              </DropdownMenuItem>
            ))}
          </DropdownMenuContent>
        </DropdownMenu>
      ) : null}
    </div>
  );
}

function RewardRow({
  reward,
  spec,
  plans,
  features,
  takenTags,
  onChange,
  onRemove
}: {
  reward: RewardDraft;
  spec?: PointsStoreRewardSpec;
  plans: VipPlan[];
  features: VipFeature[];
  takenTags: string[];
  onChange: (next: Partial<RewardDraft>) => void;
  onRemove: () => void;
}) {
  return (
    <div className="flex items-start gap-2 rounded-lg border border-border p-3">
      <div className="min-w-0 flex-1 space-y-2">
        <span className="block text-xs font-medium">{spec?.label ?? reward.type}</span>

        {spec?.value === "amount" ? (
          <div className="flex items-center gap-2">
            <Input
              type="number"
              className="h-8 w-32"
              value={reward.amount}
              min={spec.min}
              max={spec.max}
              onChange={(event) => onChange({ amount: event.target.value })}
            />
            <span className="text-xs text-muted-foreground">
              {spec.unit}（{spec.min}–{spec.max}）
            </span>
          </div>
        ) : null}

        {spec?.value === "money" ? (
          <div className="flex items-center gap-2">
            {/* 金额按字符串走：JSON number 是双精度浮点，0.1 会失真 */}
            <Input
              className="h-8 w-32"
              value={reward.money}
              inputMode="decimal"
              onChange={(event) => onChange({ money: event.target.value })}
            />
            <span className="text-xs text-muted-foreground">元</span>
          </div>
        ) : null}

        {spec?.value === "ref" ? (
          plans.length === 0 ? (
            <p className="text-xs text-muted-foreground">暂无会员套餐</p>
          ) : (
            <Select value={String(reward.refId || plans[0].id)} onValueChange={(next) => onChange({ refId: Number(next) })}>
              <SelectTrigger className="h-8 w-full sm:w-64">
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
        ) : null}

        {spec?.value === "feature" ? (
          features.length === 0 ? (
            <p className="text-xs text-muted-foreground">暂无功能标识</p>
          ) : (
            <div className="flex flex-wrap items-center gap-2">
              <Select value={reward.tag} onValueChange={(next) => onChange({ tag: next })}>
                <SelectTrigger className="h-8 w-full sm:w-52">
                  <SelectValue placeholder="选择功能" />
                </SelectTrigger>
                {/* 还没选功能时没有对齐项，弹层改为贴着触发器向下展开 */}
                <SelectContent position="popper">
                  {features.map((feature) => (
                    <SelectItem
                      key={feature.tag}
                      value={feature.tag}
                      disabled={!feature.isActive || takenTags.includes(feature.tag)}
                    >
                      {feature.name || feature.tag}
                      <span className="ml-1.5 font-mono text-[10px] text-muted-foreground">{feature.tag}</span>
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <Input
                type="number"
                className="h-8 w-24"
                value={reward.amount}
                min={0}
                max={spec.max}
                onChange={(event) => onChange({ amount: event.target.value })}
              />
              <span className="text-xs text-muted-foreground">
                天（{termLabel(Number(reward.amount) || 0)}）
              </span>
            </div>
          )
        ) : null}

        {spec?.value === "number_ticket" ? (
          <div className="flex flex-wrap items-center gap-2">
            <Select value={String(reward.refId || 1)} onValueChange={(next) => onChange({ refId: Number(next) })}>
              <SelectTrigger className="h-8 w-full sm:w-40">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {NUMBER_TICKET_LEVELS.map((tier) => (
                  <SelectItem key={tier.level} value={String(tier.level)}>
                    {tier.name}靓号券
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Input
              type="number"
              className="h-8 w-24"
              value={reward.amount}
              min={spec.min ?? 1}
              max={spec.max}
              onChange={(event) => onChange({ amount: event.target.value })}
            />
            <span className="text-xs text-muted-foreground">{spec.unit || "张"}</span>
          </div>
        ) : null}

        {spec?.value === "cosmetic" ? (
          <CosmeticRewardInput
            refId={reward.refId}
            amount={reward.amount}
            max={spec.max}
            onChange={(next) => onChange(next)}
          />
        ) : null}

        {spec?.value === "coupon" ? (
          <CouponRewardInput
            refId={reward.refId}
            amount={reward.amount}
            min={spec.min ?? 1}
            max={spec.max ?? 20}
            onChange={(next) => onChange(next)}
          />
        ) : null}

        {spec?.value === "text" ? (
          <Input
            className="h-8"
            value={reward.text}
            maxLength={64}
            placeholder="例如：定制贴纸一套"
            onChange={(event) => onChange({ text: event.target.value })}
          />
        ) : null}
      </div>
      <Button size="icon" variant="ghost" className="size-8 shrink-0" onClick={onRemove} aria-label="移除权益">
        <Trash2 className="size-3.5" />
      </Button>
    </div>
  );
}
