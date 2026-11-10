"use client";

import { useState } from "react";
import { ChevronDown, Gift, Plus, Trash2 } from "lucide-react";
import { FieldGroup, NumberField, SectionCard, SwitchRow } from "@/components/apps/app-config-primitives";
import {
  MAX_SCENES,
  OWNABLE,
  VIP_DAYS,
  VIP_PLAN,
  blankScene,
  buildRewards,
  FALLBACK_CONVERT_TARGETS,
  FALLBACK_LIFETIME_POLICIES,
  type DraftKeyState,
  type Issue,
  type PlacementDraft,
  type SceneDraft
} from "@/components/apps/rewarded-ad/rewarded-ad-config-draft";
import {
  LifetimeField,
  MembershipFeaturePicker,
  RewardPicker
} from "@/components/apps/rewarded-ad/rewarded-ad-scene-fields";
import { describeRewards, formatCooldown } from "@/components/apps/rewarded-ad/rewarded-ad-shared";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import type { CardKeyRewardSpec } from "@/lib/api/card-key";
import type { RewardedAdConfig } from "@/lib/api/rewarded-ad";
import type { VipFeature, VipPlan } from "@/lib/api/vip";
import { cn } from "@/lib/utils";

const KEY_DOT: Record<DraftKeyState, string> = {
  new: "bg-sky-500",
  own: "bg-emerald-500",
  default: "bg-muted-foreground/50",
  missing: "bg-red-500"
};

/**
 * 奖励场景：每个场景一张可折叠的卡，收起时一行说清「用哪个广告位、发什么、限多少」。
 * 广告位从上方登记的列表里选，不再手填 ID —— 手填的 ID 对不上登记的广告位，就找不到验签用的 Key。
 */
export function ScenesSection({
  scenes,
  placements,
  keyStates,
  config,
  catalog,
  plans,
  features,
  issues,
  onChange
}: {
  scenes: SceneDraft[];
  placements: PlacementDraft[];
  /** 广告位 uid → 保存后的 Key 状态 */
  keyStates: Record<string, DraftKeyState>;
  config?: RewardedAdConfig;
  catalog: CardKeyRewardSpec[];
  plans: VipPlan[];
  features: VipFeature[];
  issues: Issue[];
  onChange: (scenes: SceneDraft[]) => void;
}) {
  // 只有一个场景时默认展开；新加的场景也展开
  const [expanded, setExpanded] = useState<Set<string>>(() => new Set(scenes.length === 1 ? [scenes[0].uid] : []));
  const toggle = (uid: string, open: boolean) =>
    setExpanded((current) => {
      const next = new Set(current);
      if (open) next.add(uid);
      else next.delete(uid);
      return next;
    });
  const patch = (uid: string, changes: Partial<SceneDraft>) =>
    onChange(scenes.map((scene) => (scene.uid === uid ? { ...scene, ...changes } : scene)));

  const planNames = Object.fromEntries(plans.map((plan) => [plan.id, plan.name]));
  const featureNames = Object.fromEntries(features.map((feature) => [feature.tag, feature.name]));
  const policies = config?.lifetimePolicies?.length ? config.lifetimePolicies : FALLBACK_LIFETIME_POLICIES;
  const targets = (config?.convertTargets?.length ? config.convertTargets : FALLBACK_CONVERT_TARGETS)
    .map((type) => catalog.find((spec) => spec.type === type))
    .filter((spec): spec is CardKeyRewardSpec => Boolean(spec));
  const firstPlacement = placements.find((placement) => placement.id.trim())?.id.trim() ?? "";

  const add = () => {
    const scene = blankScene(scenes, firstPlacement);
    onChange([...scenes, scene]);
    toggle(scene.uid, true);
  };

  const enabledCount = scenes.filter((scene) => scene.enabled).length;

  return (
    <SectionCard
      icon={<Gift className="size-4" />}
      title="奖励场景"
      description={
        scenes.length > 0 ? `共 ${scenes.length} 个场景，${enabledCount} 个启用中` : "用户看完一次广告获得的奖励"
      }
      aside={
        <Button size="sm" variant="outline" disabled={scenes.length >= MAX_SCENES} onClick={add}>
          <Plus className="size-3.5" />
          添加场景
        </Button>
      }
    >
      {scenes.length === 0 ? (
        <div className="flex flex-col items-center gap-3 rounded-xl border border-dashed border-border px-4 py-10 text-center">
          <span className="grid size-10 place-items-center rounded-full bg-muted text-muted-foreground">
            <Gift className="size-5" />
          </span>
          <div className="space-y-1">
            <p className="text-sm font-medium">尚未配置奖励场景</p>
            <p className="text-xs text-muted-foreground">
              {placements.length === 0 ? "请先在上方登记广告位" : "每个场景对应一种奖励，例如领会员或领积分"}
            </p>
          </div>
          <Button size="sm" disabled={placements.length === 0} onClick={add}>
            <Plus className="size-3.5" />
            添加场景
          </Button>
        </div>
      ) : (
        <div className="space-y-2.5">
          {scenes.map((scene) => {
            const sceneIssues = issues.filter((issue) => issue.uid === scene.uid && issue.level === "error");
            const placement = placements.find((item) => item.id.trim() === scene.placementId);
            const open = expanded.has(scene.uid);
            const dailyLimit = Number(scene.dailyLimit) || 0;
            return (
              <Collapsible
                key={scene.uid}
                open={open}
                onOpenChange={(value) => toggle(scene.uid, value)}
                className={cn(
                  "overflow-hidden rounded-xl border bg-card transition-colors",
                  sceneIssues.length > 0 ? "border-red-300 dark:border-red-900" : "border-border",
                  !scene.enabled && "bg-muted/30"
                )}
              >
                <div className="flex items-center gap-2 px-3.5 py-3 sm:px-4">
                  <CollapsibleTrigger asChild>
                    <button type="button" className="flex min-w-0 flex-1 items-center gap-3 text-left">
                      <ChevronDown
                        className={cn(
                          "size-4 shrink-0 text-muted-foreground transition-transform duration-200",
                          !open && "-rotate-90"
                        )}
                      />
                      <div className="min-w-0 flex-1 space-y-1">
                        <div className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
                          <span className={cn("truncate text-sm font-medium", !scene.enabled && "text-muted-foreground")}>
                            {scene.name || "未命名场景"}
                          </span>
                          <code className="font-mono text-[11px] text-muted-foreground">{scene.key}</code>
                          {!scene.enabled ? (
                            <Badge variant="outline" size="sm">
                              已停用
                            </Badge>
                          ) : null}
                          {sceneIssues.length > 0 ? (
                            <Badge variant="danger" size="sm">
                              {sceneIssues.length} 项待处理
                            </Badge>
                          ) : null}
                        </div>
                        <div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-0.5 text-[11px] text-muted-foreground">
                          <span className="inline-flex min-w-0 items-center gap-1.5">
                            <span
                              className={cn(
                                "size-1.5 shrink-0 rounded-full",
                                placement ? KEY_DOT[keyStates[placement.uid] ?? "missing"] : "bg-red-500"
                              )}
                            />
                            <span className="truncate">{placement ? placement.name || placement.id : "未选择广告位"}</span>
                          </span>
                          <span className="truncate">
                            {describeRewards(buildRewards(scene, catalog), catalog, planNames, featureNames)}
                          </span>
                          <span>
                            {dailyLimit > 0 ? `每日 ${dailyLimit} 次` : "不限次数"}，冷却{" "}
                            {formatCooldown(Number(scene.cooldownSeconds) || 0)}
                          </span>
                        </div>
                      </div>
                    </button>
                  </CollapsibleTrigger>
                  <Switch
                    checked={scene.enabled}
                    onCheckedChange={(value) => patch(scene.uid, { enabled: value })}
                    aria-label="启用场景"
                  />
                  <Button
                    size="icon"
                    variant="ghost"
                    className="size-7 text-muted-foreground hover:text-destructive"
                    aria-label="删除场景"
                    onClick={() => onChange(scenes.filter((item) => item.uid !== scene.uid))}
                  >
                    <Trash2 className="size-3.5" />
                  </Button>
                </div>

                <CollapsibleContent>
                  <div className="space-y-5 border-t border-border px-3.5 py-4 sm:px-4">
                    {sceneIssues.length > 0 ? (
                      <ul className="space-y-0.5 rounded-lg bg-red-50 px-3 py-2 dark:bg-red-950/40">
                        {sceneIssues.map((issue) => (
                          <li key={issue.text} className="text-[11px] text-red-700 dark:text-red-300">
                            {issue.text}
                          </li>
                        ))}
                      </ul>
                    ) : null}

                    <div className="grid gap-3 sm:grid-cols-3">
                      <div className="space-y-1.5">
                        <Label className="text-[11px] font-medium">名称</Label>
                        <Input
                          className="h-8 text-xs"
                          value={scene.name}
                          maxLength={32}
                          placeholder="如 看广告领会员"
                          onChange={(event) => patch(scene.uid, { name: event.target.value })}
                        />
                      </div>
                      <div className="space-y-1.5">
                        <Label className="text-[11px] font-medium">标识</Label>
                        <Input
                          className="h-8 font-mono text-xs"
                          value={scene.key}
                          maxLength={32}
                          placeholder="客户端上报时使用"
                          onChange={(event) => patch(scene.uid, { key: event.target.value.toLowerCase() })}
                        />
                      </div>
                      <div className="space-y-1.5">
                        <Label className="text-[11px] font-medium">广告位</Label>
                        <PlacementSelect
                          value={scene.placementId}
                          placements={placements}
                          keyStates={keyStates}
                          onChange={(value) => patch(scene.uid, { placementId: value })}
                        />
                      </div>
                    </div>

                    <div className="grid gap-3 sm:grid-cols-3">
                      <NumberField
                        label="每日次数"
                        unit="次"
                        min={0}
                        max={1000}
                        hint="0 表示只受总次数约束"
                        value={scene.dailyLimit}
                        onChange={(value) => patch(scene.uid, { dailyLimit: value })}
                      />
                      <NumberField
                        label="冷却"
                        unit="秒"
                        min={0}
                        max={86400}
                        hint="两次领取的最短间隔，0 表示不限"
                        value={scene.cooldownSeconds}
                        onChange={(value) => patch(scene.uid, { cooldownSeconds: value })}
                      />
                    </div>

                    <FieldGroup label="奖励">
                      <RewardPicker
                        scene={scene}
                        catalog={catalog}
                        plans={plans}
                        features={features}
                        onChange={(rewards) => patch(scene.uid, { rewards })}
                      />
                    </FieldGroup>

                    {scene.rewards[VIP_DAYS] || scene.rewards[VIP_PLAN] ? (
                      <FieldGroup
                        label="会员权益"
                        hint={scene.rewards[VIP_DAYS] ? `已选 ${scene.features.length} / ${features.length}` : undefined}
                      >
                        <div className="space-y-2">
                          <SwitchRow
                            label="会员期间免广告"
                            checked={scene.adFree}
                            onChange={(value) => patch(scene.uid, { adFree: value })}
                          />
                          {scene.rewards[VIP_DAYS] ? (
                            <MembershipFeaturePicker
                              selected={scene.features}
                              features={features}
                              onChange={(next) => patch(scene.uid, { features: next })}
                            />
                          ) : (
                            <p className="text-[11px] text-muted-foreground">会员套餐的功能随所选套餐</p>
                          )}
                        </div>
                      </FieldGroup>
                    ) : null}

                    {OWNABLE.some((type) => scene.rewards[type]) ? (
                      <LifetimeField
                        value={scene.lifetime}
                        policies={policies}
                        targets={targets}
                        onChange={(next) => patch(scene.uid, { lifetime: { ...scene.lifetime, ...next } })}
                      />
                    ) : null}
                  </div>
                </CollapsibleContent>
              </Collapsible>
            );
          })}
        </div>
      )}
    </SectionCard>
  );
}

const KEY_STATE_TEXT: Record<DraftKeyState, string> = {
  new: "新 Key",
  own: "独立 Key",
  default: "默认 Key",
  missing: "缺少 Key"
};

function PlacementSelect({
  value,
  placements,
  keyStates,
  onChange
}: {
  value: string;
  placements: PlacementDraft[];
  keyStates: Record<string, DraftKeyState>;
  onChange: (value: string) => void;
}) {
  const options = placements.filter((placement) => placement.id.trim());
  const orphan = value && !options.some((placement) => placement.id.trim() === value) ? value : "";
  if (options.length === 0 && !orphan) {
    return <p className="pt-1.5 text-xs text-muted-foreground">请先登记广告位</p>;
  }
  return (
    <Select value={value} onValueChange={onChange}>
      <SelectTrigger className="h-8 w-full text-xs">
        <SelectValue placeholder="选择广告位" />
      </SelectTrigger>
      <SelectContent position="popper">
        {orphan ? (
          <SelectItem value={orphan} disabled>
            <span className="font-mono">{orphan}</span>
            <span className="ml-1.5 text-[10px] text-muted-foreground">未登记</span>
          </SelectItem>
        ) : null}
        {options.map((placement) => {
          const state = keyStates[placement.uid] ?? "missing";
          return (
            <SelectItem key={placement.uid} value={placement.id.trim()}>
              <span className={cn("size-1.5 shrink-0 rounded-full", KEY_DOT[state])} />
              {placement.name || placement.id}
              <span className="ml-1 font-mono text-[10px] text-muted-foreground">{placement.id}</span>
              <span className="ml-1 text-[10px] text-muted-foreground">{KEY_STATE_TEXT[state]}</span>
            </SelectItem>
          );
        })}
      </SelectContent>
    </Select>
  );
}
