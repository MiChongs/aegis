"use client";

import { useState } from "react";
import { Eye, EyeOff, Gift, Loader2, Plug, Plus, Save, ShieldCheck, Trash2 } from "lucide-react";
import { toast } from "sonner";
import {
  FieldGroup,
  ModeCard,
  NumberField,
  SectionCard,
  StatusDot,
  SwitchRow
} from "@/components/apps/app-config-primitives";
import { VERIFY_MODE_META } from "@/components/apps/rewarded-ad/rewarded-ad-shared";
import { CopyButton } from "@/components/profile/profile-shared";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { RadioGroup } from "@/components/ui/radio-group";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { ApiError } from "@/lib/api/client";
import type { CardKeyReward, CardKeyRewardSpec } from "@/lib/api/card-key";
import type { RewardedAdConfig, RewardedAdScene, RewardedAdVerifyMode } from "@/lib/api/rewarded-ad";
import { useRewardedAdConfigQuery, useSaveRewardedAdConfigMutation } from "@/lib/rewarded-ad-hooks";
import { useAdminVipPlansQuery } from "@/lib/vip-hooks";

/** 灰鲸后台「配置回调URL」里默认的参数名，与后端回调处理器认的名字一致。 */
const CALLBACK_PARAMS = ["userId", "transId", "sign", "placementId", "rewardAmount", "rewardName", "extrainfo"];

const PROVIDER_LABELS: Record<string, string> = { huijing: "灰鲸" };

/**
 * 数字项一律按字符串存（理由同卡密批次编辑器：存 number 的话输入框清不空）。
 * uid 只给 React 列表用，场景标识可以被改，不能拿它当 key。
 */
type SceneDraft = {
  uid: string;
  key: string;
  name: string;
  placementId: string;
  enabled: boolean;
  dailyLimit: string;
  cooldownSeconds: string;
  rewards: Record<string, { amount: string; refId: number }>;
};

type Draft = {
  scope: string;
  enabled: boolean;
  provider: string;
  providerAppId: string;
  verifyMode: RewardedAdVerifyMode;
  dailyLimit: string;
  scenes: SceneDraft[];
};

let uidSeed = 0;
const nextUid = () => `scene-${++uidSeed}`;

// 从服务端数据播种的场景用下标作 uid：草稿未落地前每次渲染都会重新播种，
// 随机 uid 会让输入框在每次重渲染时被卸载重建。
function sceneToDraft(scene: RewardedAdScene, index: number): SceneDraft {
  const rewards: SceneDraft["rewards"] = {};
  scene.rewards.forEach((reward) => {
    rewards[reward.type] = { amount: String(reward.amount ?? 1), refId: reward.refId ?? 0 };
  });
  return {
    uid: `seed-${index}`,
    key: scene.key,
    name: scene.name,
    placementId: scene.placementId,
    enabled: scene.enabled,
    dailyLimit: String(scene.dailyLimit ?? 0),
    cooldownSeconds: String(scene.cooldownSeconds ?? 0),
    rewards
  };
}

function seedDraft(scope: string, config?: RewardedAdConfig): Draft {
  return {
    scope,
    enabled: config?.enabled ?? false,
    provider: config?.provider || "huijing",
    providerAppId: config?.providerAppId ?? "",
    verifyMode: config?.verifyMode ?? "dual",
    dailyLimit: String(config?.dailyLimit ?? 0),
    scenes: (config?.scenes ?? []).map((scene, index) => sceneToDraft(scene, index))
  };
}

function blankScene(index: number, placementId: string): SceneDraft {
  return {
    uid: nextUid(),
    key: index === 0 ? "vip" : `scene${index + 1}`,
    name: index === 0 ? "看广告领会员" : "",
    placementId,
    enabled: true,
    dailyLimit: "3",
    cooldownSeconds: "60",
    rewards: { vip_days: { amount: "1", refId: 0 } }
  };
}

export function RewardedAdConfigPanel({ appKey }: { appKey: string }) {
  const configQuery = useRewardedAdConfigQuery(appKey);
  const plansQuery = useAdminVipPlansQuery(appKey);
  const saveMutation = useSaveRewardedAdConfigMutation(appKey);

  const [draft, setDraft] = useState<Draft | null>(null);
  const [secret, setSecret] = useState<{ scope: string; value: string }>({ scope: appKey, value: "" });
  const [showSecret, setShowSecret] = useState(false);

  const config = configQuery.data;
  const current = draft?.scope === appKey ? draft : seedDraft(appKey, config);
  const secretValue = secret.scope === appKey ? secret.value : "";
  const patch = (changes: Partial<Draft>) => setDraft({ ...current, ...changes, scope: appKey });
  const patchScene = (uid: string, changes: Partial<SceneDraft>) =>
    patch({ scenes: current.scenes.map((scene) => (scene.uid === uid ? { ...scene, ...changes } : scene)) });

  const catalog = config?.catalog ?? [];
  const plans = plansQuery.data ?? [];
  const callbackUrl = config
    ? config.callbackAbsolute || typeof window === "undefined"
      ? config.callbackUrl
      : `${window.location.origin}${config.callbackUrl}`
    : "";

  const toggleReward = (scene: SceneDraft, spec: CardKeyRewardSpec, checked: boolean) => {
    const rewards = { ...scene.rewards };
    if (checked) {
      rewards[spec.type] = { amount: String(spec.min ?? 1), refId: plans[0]?.id ?? 0 };
    } else {
      delete rewards[spec.type];
    }
    patchScene(scene.uid, { rewards });
  };

  const buildRewards = (scene: SceneDraft): CardKeyReward[] =>
    catalog
      .filter((spec) => scene.rewards[spec.type])
      .map((spec) => {
        const value = scene.rewards[spec.type];
        return spec.value === "ref"
          ? { type: spec.type, refId: Number(value.refId) }
          : { type: spec.type, amount: Number(value.amount) };
      });

  const save = async () => {
    for (const scene of current.scenes) {
      if (!scene.key.trim() || !scene.name.trim() || !scene.placementId.trim()) {
        toast.error("场景的标识、名称与广告位 ID 都不能为空");
        return;
      }
      if (buildRewards(scene).length === 0) {
        toast.error(`场景「${scene.name || scene.key}」至少配置一项权益`);
        return;
      }
    }
    try {
      await saveMutation.mutateAsync({
        enabled: current.enabled,
        provider: current.provider,
        providerAppId: current.providerAppId.trim(),
        securityKey: secretValue.trim() || undefined,
        verifyMode: current.verifyMode,
        dailyLimit: Number(current.dailyLimit) || 0,
        scenes: current.scenes.map((scene) => ({
          key: scene.key.trim(),
          name: scene.name.trim(),
          placementId: scene.placementId.trim(),
          enabled: scene.enabled,
          rewards: buildRewards(scene),
          dailyLimit: Number(scene.dailyLimit) || 0,
          cooldownSeconds: Number(scene.cooldownSeconds) || 0
        }))
      });
      toast.success("激励广告配置已保存");
      setDraft(null);
      setSecret({ scope: appKey, value: "" });
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "保存失败");
    }
  };

  if (configQuery.isLoading) {
    return (
      <div className="space-y-4">
        {[0, 1, 2].map((index) => (
          <Skeleton key={index} className="h-40 w-full rounded-2xl" />
        ))}
      </div>
    );
  }

  return (
    <div className="space-y-4">
      <SectionCard
        icon={<Plug className="size-4" />}
        title="接入"
        aside={<StatusDot active={Boolean(config?.enabled)} labelActive="已启用" labelInactive="未启用" />}
      >
        <div className="space-y-5">
          <SwitchRow
            label="启用激励广告"
            checked={current.enabled}
            onChange={(value) => patch({ enabled: value })}
          />

          <div className="grid gap-4 sm:grid-cols-2">
            <FieldGroup label="广告平台">
              <Select value={current.provider} onValueChange={(value) => patch({ provider: value })}>
                <SelectTrigger className="h-8 text-xs">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {(config?.providers ?? ["huijing"]).map((provider) => (
                    <SelectItem key={provider} value={provider}>
                      {PROVIDER_LABELS[provider] ?? provider}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </FieldGroup>
            <FieldGroup label="平台应用 ID">
              <Input
                className="h-8 font-mono text-xs"
                value={current.providerAppId}
                placeholder="231525"
                onChange={(event) => patch({ providerAppId: event.target.value })}
              />
            </FieldGroup>
          </div>

          <FieldGroup
            label="Security Key"
            hint={config?.hasSecurityKey ? `已配置 ${config.securityKeyHint ?? ""}`.trim() : "未配置"}
          >
            <div className="relative">
              <Input
                type={showSecret ? "text" : "password"}
                autoComplete="off"
                className="pr-9 font-mono text-xs"
                placeholder={config?.hasSecurityKey ? "留空不修改" : "与平台「配置回调URL」中的 Security Key 一致"}
                value={secretValue}
                onChange={(event) => setSecret({ scope: appKey, value: event.target.value })}
              />
              {secretValue ? (
                <button
                  type="button"
                  className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
                  onClick={() => setShowSecret(!showSecret)}
                  aria-label={showSecret ? "隐藏密钥" : "显示密钥"}
                >
                  {showSecret ? <EyeOff className="size-4" /> : <Eye className="size-4" />}
                </button>
              ) : null}
            </div>
          </FieldGroup>

          <FieldGroup label="回调地址">
            <div className="flex items-center gap-2 rounded-lg border border-border bg-muted/40 px-3 py-2">
              <code className="min-w-0 flex-1 truncate font-mono text-xs" title={callbackUrl}>
                {callbackUrl}
              </code>
              <CopyButton value={callbackUrl} label="复制回调地址" />
            </div>
            <div className="flex flex-wrap items-center gap-1">
              <span className="mr-1 text-[11px] text-muted-foreground">参数</span>
              {CALLBACK_PARAMS.map((name) => (
                <Badge key={name} variant="outline" size="sm" className="font-mono font-normal">
                  {name}
                </Badge>
              ))}
            </div>
          </FieldGroup>
        </div>
      </SectionCard>

      <SectionCard icon={<ShieldCheck className="size-4" />} title="校验与限额">
        <div className="space-y-5">
          <FieldGroup label="校验模式">
            <RadioGroup
              value={current.verifyMode}
              onValueChange={(value) => patch({ verifyMode: value as RewardedAdVerifyMode })}
              className="grid gap-2 sm:grid-cols-3"
            >
              {(config?.verifyModes ?? (["dual", "server", "client"] as RewardedAdVerifyMode[])).map((mode) => (
                <ModeCard
                  key={mode}
                  value={mode}
                  active={current.verifyMode === mode}
                  title={VERIFY_MODE_META[mode]?.title ?? mode}
                  description={VERIFY_MODE_META[mode]?.description ?? ""}
                  icon={<ShieldCheck className="size-3.5" />}
                />
              ))}
            </RadioGroup>
          </FieldGroup>
          <div className="grid gap-4 sm:grid-cols-2">
            <NumberField
              label="每人每日总次数"
              unit="次"
              min={0}
              max={1000}
              hint="0 表示不限"
              value={current.dailyLimit}
              onChange={(value) => patch({ dailyLimit: value })}
            />
          </div>
        </div>
      </SectionCard>

      <SectionCard
        icon={<Gift className="size-4" />}
        title="奖励场景"
        aside={
          <Button
            size="sm"
            variant="outline"
            onClick={() =>
              patch({
                scenes: [
                  ...current.scenes,
                  blankScene(current.scenes.length, current.scenes[0]?.placementId ?? "")
                ]
              })
            }
          >
            <Plus className="size-3.5" />
            添加场景
          </Button>
        }
        footer={
          <div className="flex items-center justify-between gap-3">
            <span className="text-[11px] text-muted-foreground">
              {config?.updatedAt ? `上次保存 ${new Date(config.updatedAt).toLocaleString("zh-CN")}` : "尚未保存"}
              {config?.updatedBy ? ` · ${config.updatedBy}` : ""}
            </span>
            <Button size="sm" onClick={() => void save()} disabled={saveMutation.isPending}>
              {saveMutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : <Save className="size-3.5" />}
              保存
            </Button>
          </div>
        }
      >
        {current.scenes.length === 0 ? (
          <p className="rounded-xl border border-dashed border-border px-4 py-8 text-center text-xs text-muted-foreground">
            暂无场景
          </p>
        ) : (
          <div className="space-y-3">
            {current.scenes.map((scene) => (
              <div key={scene.uid} className="space-y-4 rounded-xl border border-border p-4">
                <div className="flex items-center justify-between gap-3">
                  <div className="flex min-w-0 items-center gap-2">
                    <span className="truncate text-sm font-medium">{scene.name || "未命名场景"}</span>
                    <code className="font-mono text-[11px] text-muted-foreground">{scene.key}</code>
                  </div>
                  <div className="flex items-center gap-2">
                    <Switch
                      checked={scene.enabled}
                      onCheckedChange={(value) => patchScene(scene.uid, { enabled: value })}
                      aria-label="启用场景"
                    />
                    <Button
                      size="icon"
                      variant="ghost"
                      className="size-7 text-muted-foreground hover:text-destructive"
                      aria-label="删除场景"
                      onClick={() => patch({ scenes: current.scenes.filter((item) => item.uid !== scene.uid) })}
                    >
                      <Trash2 className="size-3.5" />
                    </Button>
                  </div>
                </div>

                <div className="grid gap-3 sm:grid-cols-3">
                  <div className="space-y-1.5">
                    <Label className="text-[11px] font-medium">名称</Label>
                    <Input
                      className="h-8 text-xs"
                      value={scene.name}
                      maxLength={32}
                      onChange={(event) => patchScene(scene.uid, { name: event.target.value })}
                    />
                  </div>
                  <div className="space-y-1.5">
                    <Label className="text-[11px] font-medium">标识</Label>
                    <Input
                      className="h-8 font-mono text-xs"
                      value={scene.key}
                      maxLength={32}
                      onChange={(event) => patchScene(scene.uid, { key: event.target.value.toLowerCase() })}
                    />
                  </div>
                  <div className="space-y-1.5">
                    <Label className="text-[11px] font-medium">广告位 ID</Label>
                    <Input
                      className="h-8 font-mono text-xs"
                      value={scene.placementId}
                      onChange={(event) => patchScene(scene.uid, { placementId: event.target.value })}
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
                    onChange={(value) => patchScene(scene.uid, { dailyLimit: value })}
                  />
                  <NumberField
                    label="冷却"
                    unit="秒"
                    min={0}
                    max={86400}
                    hint="0 表示不限"
                    value={scene.cooldownSeconds}
                    onChange={(value) => patchScene(scene.uid, { cooldownSeconds: value })}
                  />
                </div>

                <FieldGroup label="奖励">
                  <div className="grid gap-2 sm:grid-cols-2">
                    {catalog.map((spec) => {
                      const value = scene.rewards[spec.type];
                      return (
                        <div key={spec.type} className="rounded-lg border border-border p-3">
                          <label className="flex items-center gap-2.5">
                            <Checkbox
                              checked={Boolean(value)}
                              onCheckedChange={(checked) => toggleReward(scene, spec, Boolean(checked))}
                            />
                            <span className="text-xs font-medium">{spec.label}</span>
                          </label>
                          {value ? (
                            <div className="mt-2.5 pl-7">
                              {spec.value === "ref" ? (
                                plans.length === 0 ? (
                                  <p className="text-xs text-muted-foreground">暂无会员套餐</p>
                                ) : (
                                  <Select
                                    value={String(value.refId || plans[0].id)}
                                    onValueChange={(next) =>
                                      patchScene(scene.uid, {
                                        rewards: { ...scene.rewards, [spec.type]: { ...value, refId: Number(next) } }
                                      })
                                    }
                                  >
                                    <SelectTrigger className="h-8 w-full text-xs">
                                      <SelectValue />
                                    </SelectTrigger>
                                    <SelectContent>
                                      {plans.map((plan) => (
                                        <SelectItem key={plan.id} value={String(plan.id)}>
                                          {plan.name}（{plan.durationDays} 天）
                                        </SelectItem>
                                      ))}
                                    </SelectContent>
                                  </Select>
                                )
                              ) : (
                                <div className="flex items-center gap-2">
                                  <Input
                                    type="number"
                                    className="h-8 w-28 font-mono text-xs"
                                    value={value.amount}
                                    min={spec.min}
                                    max={spec.max}
                                    onChange={(event) =>
                                      patchScene(scene.uid, {
                                        rewards: {
                                          ...scene.rewards,
                                          [spec.type]: { ...value, amount: event.target.value }
                                        }
                                      })
                                    }
                                  />
                                  <span className="text-xs text-muted-foreground">{spec.unit}</span>
                                </div>
                              )}
                            </div>
                          ) : null}
                        </div>
                      );
                    })}
                  </div>
                </FieldGroup>
              </div>
            ))}
          </div>
        )}
      </SectionCard>
    </div>
  );
}
