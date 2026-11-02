"use client";

import { useRef, useState, type ComponentType, type ReactNode } from "react";
import {
  AlertCircle,
  ArrowDownUp,
  Ban,
  CalendarClock,
  CheckCircle2,
  FileText,
  Gift,
  Loader2,
  Lock,
  Plus,
  RotateCcw,
  Save,
  Share2,
  Trash2,
  UserPlus,
  X
} from "lucide-react";
import { toast } from "sonner";
import {
  DEFAULT_BLOCKED_DOMAINS,
  StatusBadge,
  catalogForbidden,
  catalogPlatforms,
  normalizeDomain,
  promoLimits
} from "@/components/apps/promo/promo-shared";
import {
  RewardListEditor,
  rewardDraftsFrom,
  rewardsFromDrafts,
  type RewardDraft
} from "@/components/apps/rewards/reward-list-editor";
import { ToneIcon, errorMessage, type Tone } from "@/components/users/detail/kit";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import type {
  PromoAdminCampaign,
  PromoCampaign,
  PromoCampaignPayload,
  PromoCatalog,
  PromoConfig
} from "@/lib/api/promo";
import type { PointsStoreRewardSpec } from "@/lib/api/points-store";
import { browserTimeZone, formatDateTimeMinute, fromLocalInputValue, toLocalInputValue } from "@/lib/datetime";
import { usePromoCampaignQuery, usePromoCatalogQuery, useSavePromoCampaignMutation } from "@/lib/promo-hooks";
import { cn } from "@/lib/utils";
import { useAdminVipFeaturesQuery, useAdminVipPlansQuery } from "@/lib/vip-hooks";

export type PromoEditorTarget = { mode: "create" } | { mode: "edit"; campaign: PromoAdminCampaign };

const SHARE_TOKENS = [
  { token: "{code}", label: "邀请码" },
  { token: "{link}", label: "邀请链接" },
  { token: "{app}", label: "应用名称" },
  { token: "{platform}", label: "平台名称" }
];

const DAY_MS = 24 * 60 * 60 * 1000;

type TierDraft = { key: number; count: string; title: string; rewards: RewardDraft[] };

type Form = {
  title: string;
  subtitle: string;
  rulesText: string;
  startsAt: string;
  endsAt: string;
  claimGraceDays: string;
  inviteEnabled: boolean;
  tiers: TierDraft[];
  leaderboard: boolean;
  leaderboardSize: string;
  shareEnabled: boolean;
  platforms: string[];
  shareRewards: RewardDraft[];
  maxApprovedPerUser: string;
  maxApprovedPerPlatform: string;
  dailySubmitLimit: string;
  blockedDomains: string[];
  shareTemplate: string;
};

/** 兜底默认配置：目录里没下发 defaults 时用，与 docs/promo.md 一致 */
function fallbackConfig(catalog?: PromoCatalog): PromoConfig {
  return {
    claimGraceDays: 7,
    invite: { enabled: true, tiers: [], leaderboard: true, leaderboardSize: 20 },
    share: {
      enabled: true,
      platforms: catalogPlatforms(catalog)
        .filter((platform) => platform.defaultOn)
        .map((platform) => platform.key),
      rewards: [],
      maxApprovedPerUser: 3,
      maxApprovedPerPlatform: 1,
      dailySubmitLimit: 5,
      blockedDomains: DEFAULT_BLOCKED_DOMAINS,
      shareTemplate: ""
    }
  };
}

/** 新建时的默认时间：下一个整点开始，持续 14 天（浏览器时区） */
function defaultWindow() {
  const start = new Date();
  start.setMinutes(0, 0, 0);
  start.setHours(start.getHours() + 1);
  const end = new Date(start.getTime() + 14 * DAY_MS);
  return { startsAt: toLocalInputValue(start), endsAt: toLocalInputValue(end) };
}

function seed(campaign: PromoCampaign | null, catalog?: PromoCatalog): Form {
  const fallback = fallbackConfig(catalog);
  const defaults = catalog?.defaults;
  const config: PromoConfig = campaign?.config
    ? {
        claimGraceDays: campaign.config.claimGraceDays ?? fallback.claimGraceDays,
        invite: { ...fallback.invite, ...campaign.config.invite },
        share: { ...fallback.share, ...campaign.config.share }
      }
    : {
        claimGraceDays: defaults?.claimGraceDays ?? fallback.claimGraceDays,
        invite: { ...fallback.invite, ...defaults?.invite },
        share: { ...fallback.share, ...defaults?.share }
      };
  const period = campaign
    ? { startsAt: toLocalInputValue(campaign.startsAt), endsAt: toLocalInputValue(campaign.endsAt) }
    : defaultWindow();
  return {
    title: campaign?.title ?? "",
    subtitle: campaign?.subtitle ?? "",
    rulesText: campaign?.rulesText ?? "",
    startsAt: period.startsAt,
    endsAt: period.endsAt,
    claimGraceDays: String(config.claimGraceDays),
    inviteEnabled: Boolean(config.invite.enabled),
    tiers: (config.invite.tiers ?? []).map((tier, index) => ({
      key: index + 1,
      count: String(tier.count),
      title: tier.title ?? "",
      rewards: rewardDraftsFrom(tier.rewards)
    })),
    leaderboard: Boolean(config.invite.leaderboard),
    leaderboardSize: String(config.invite.leaderboardSize || 20),
    shareEnabled: Boolean(config.share.enabled),
    platforms: [...(config.share.platforms ?? [])],
    shareRewards: rewardDraftsFrom(config.share.rewards),
    maxApprovedPerUser: String(config.share.maxApprovedPerUser ?? 0),
    maxApprovedPerPlatform: String(config.share.maxApprovedPerPlatform ?? 0),
    dailySubmitLimit: String(config.share.dailySubmitLimit ?? 0),
    blockedDomains: [...(config.share.blockedDomains ?? [])],
    shareTemplate: config.share.shareTemplate ?? ""
  };
}

const toInt = (value: string) => {
  const parsed = Number.parseInt(value, 10);
  return Number.isFinite(parsed) ? parsed : 0;
};

function toPayload(form: Form, catalog: PointsStoreRewardSpec[]): PromoCampaignPayload {
  return {
    title: form.title.trim(),
    subtitle: form.subtitle.trim(),
    rulesText: form.rulesText.trim(),
    startsAt: fromLocalInputValue(form.startsAt) ?? "",
    endsAt: fromLocalInputValue(form.endsAt) ?? "",
    config: {
      claimGraceDays: toInt(form.claimGraceDays),
      invite: {
        enabled: form.inviteEnabled,
        tiers: form.tiers.map((tier) => ({
          count: toInt(tier.count),
          title: tier.title.trim(),
          rewards: rewardsFromDrafts(tier.rewards, catalog)
        })),
        leaderboard: form.leaderboard,
        leaderboardSize: toInt(form.leaderboardSize)
      },
      share: {
        enabled: form.shareEnabled,
        platforms: form.platforms,
        rewards: rewardsFromDrafts(form.shareRewards, catalog),
        maxApprovedPerUser: toInt(form.maxApprovedPerUser),
        maxApprovedPerPlatform: toInt(form.maxApprovedPerPlatform),
        dailySubmitLimit: toInt(form.dailySubmitLimit),
        blockedDomains: form.blockedDomains,
        shareTemplate: form.shareTemplate.trim()
      }
    }
  };
}

const inRange = (value: string, min: number, max: number) => {
  const parsed = toInt(value);
  return String(parsed) === value.trim() && parsed >= min && parsed <= max;
};

/**
 * 新建 / 编辑一期活动。
 *
 * 左边是分区表单，右边是保存前检查与要点摘要（窄屏收到表单下方）。
 * 平台目录、写死的禁用平台、权益目录与上限都来自 `/promo/catalog`；时间按浏览器时区录入与显示。
 * 编辑时先取详情（`GET /campaigns/{id}`），取不到就用列表里的那份。草稿跟着打开对象走，关闭即丢弃。
 */
export function PromoCampaignEditor({
  appKey,
  target,
  onClose
}: {
  appKey: string;
  target: PromoEditorTarget | null;
  onClose: () => void;
}) {
  // 关闭动画期间 target 已为空，沿用上一次的对象渲染
  const [shown, setShown] = useState<PromoEditorTarget | null>(target);
  if (target && target !== shown) setShown(target);

  return (
    <Sheet open={Boolean(target)} onOpenChange={(open) => (!open ? onClose() : undefined)}>
      <SheetContent side="right" className="flex w-full flex-col gap-0 p-0 sm:max-w-4xl">
        {shown ? (
          <EditorLoader
            key={shown.mode === "edit" ? `edit-${shown.campaign.id}` : "create"}
            appKey={appKey}
            target={shown}
            onClose={onClose}
          />
        ) : null}
      </SheetContent>
    </Sheet>
  );
}

function EditorLoader({ appKey, target, onClose }: { appKey: string; target: PromoEditorTarget; onClose: () => void }) {
  const editingId = target.mode === "edit" ? target.campaign.id : null;
  const catalogQuery = usePromoCatalogQuery(appKey);
  const detailQuery = usePromoCampaignQuery(appKey, editingId);

  if (catalogQuery.isLoading || (editingId && detailQuery.isLoading)) {
    return (
      <>
        <SheetHeader className="shrink-0 gap-1 border-b px-5 py-4 pr-12">
          <SheetTitle className="text-base">{editingId ? "编辑活动" : "新建活动"}</SheetTitle>
          <SheetDescription>正在加载</SheetDescription>
        </SheetHeader>
        <div className="flex flex-col gap-4 p-5">
          <Skeleton className="h-32 w-full rounded-xl" />
          <Skeleton className="h-48 w-full rounded-xl" />
          <Skeleton className="h-48 w-full rounded-xl" />
        </div>
      </>
    );
  }

  const campaign: PromoCampaign | null =
    target.mode === "edit" ? (detailQuery.data ?? target.campaign) : null;

  return <EditorBody appKey={appKey} campaign={campaign} catalog={catalogQuery.data} onClose={onClose} />;
}

function EditorBody({
  appKey,
  campaign,
  catalog,
  onClose
}: {
  appKey: string;
  campaign: PromoCampaign | null;
  catalog?: PromoCatalog;
  onClose: () => void;
}) {
  const plansQuery = useAdminVipPlansQuery(appKey);
  const featuresQuery = useAdminVipFeaturesQuery(appKey);
  const saveMutation = useSavePromoCampaignMutation(appKey);
  const [form, setForm] = useState<Form>(() => seed(campaign, catalog));
  const [domainInput, setDomainInput] = useState("");
  const templateRef = useRef<HTMLTextAreaElement>(null);
  const patch = <K extends keyof Form>(key: K, value: Form[K]) => setForm((current) => ({ ...current, [key]: value }));

  const limits = promoLimits(catalog);
  const rewardCatalog = catalog?.rewards ?? [];
  const platforms = catalogPlatforms(catalog);
  const forbidden = catalogForbidden(catalog);
  const plans = (plansQuery.data ?? []).filter((plan) => plan.kind !== "trial");
  const features = featuresQuery.data ?? [];
  const timezone = browserTimeZone();

  const startsAt = fromLocalInputValue(form.startsAt);
  const endsAt = fromLocalInputValue(form.endsAt);
  const startMs = startsAt ? new Date(startsAt).getTime() : 0;
  const endMs = endsAt ? new Date(endsAt).getTime() : 0;
  const graceDays = toInt(form.claimGraceDays);
  const claimUntil = endMs ? new Date(endMs + Math.max(0, graceDays) * DAY_MS).toISOString() : undefined;
  const tierCounts = form.tiers.map((tier) => toInt(tier.count));
  const tiersIncreasing = tierCounts.every((count, index) => index === 0 || count > tierCounts[index - 1]);

  const problems: Array<{ ok: boolean; label: string }> = [
    { ok: Boolean(form.title.trim()), label: "填写活动标题" },
    { ok: Boolean(startsAt && endsAt), label: "填写开始与结束时间" },
    { ok: !startsAt || !endsAt || endMs > startMs, label: "结束时间晚于开始时间" },
    {
      ok: !startsAt || !endsAt || endMs - startMs <= limits.maxDurationDays * DAY_MS,
      label: `活动时长不超过 ${limits.maxDurationDays} 天`
    },
    { ok: inRange(form.claimGraceDays, 0, limits.maxClaimGraceDays), label: `领取宽限为 0–${limits.maxClaimGraceDays} 天` },
    { ok: form.inviteEnabled || form.shareEnabled, label: "至少开启一项任务" }
  ];
  if (form.inviteEnabled) {
    problems.push(
      { ok: form.tiers.length > 0, label: "邀请任务至少配置一档" },
      {
        ok: tierCounts.every((count) => count >= 1 && count <= limits.maxTierCount),
        label: `每档人数为 1–${formatInt(limits.maxTierCount)}`
      },
      { ok: tiersIncreasing, label: "档位人数严格递增" },
      {
        ok: form.tiers.every((tier) => tier.rewards.length >= 1 && tier.rewards.length <= limits.maxRewards),
        label: `每档配置 1–${limits.maxRewards} 项奖励`
      }
    );
    if (form.leaderboard) {
      problems.push({
        ok: inRange(form.leaderboardSize, limits.minLeaderboardSize, limits.maxLeaderboardSize),
        label: `排行人数为 ${limits.minLeaderboardSize}–${limits.maxLeaderboardSize}`
      });
    }
  }
  if (form.shareEnabled) {
    problems.push(
      { ok: form.platforms.length > 0, label: "分享任务至少启用一个平台" },
      {
        ok: form.shareRewards.length >= 1 && form.shareRewards.length <= limits.maxRewards,
        label: `每篇奖励配置 1–${limits.maxRewards} 项`
      },
      {
        ok:
          inRange(form.maxApprovedPerUser, 1, limits.maxPerUserLimit) &&
          inRange(form.maxApprovedPerPlatform, 1, limits.maxPerUserLimit) &&
          inRange(form.dailySubmitLimit, 1, limits.maxPerUserLimit),
        label: `三个次数上限均为 1–${limits.maxPerUserLimit}`
      },
      { ok: form.blockedDomains.length <= limits.maxBlockedDomains, label: `禁用域名不超过 ${limits.maxBlockedDomains} 个` }
    );
  }
  const blocked = problems.some((problem) => !problem.ok);

  const patchTier = (key: number, next: Partial<TierDraft>) =>
    patch(
      "tiers",
      form.tiers.map((tier) => (tier.key === key ? { ...tier, ...next } : tier))
    );

  const addTier = () => {
    const last = form.tiers.reduce((max, tier) => Math.max(max, toInt(tier.count)), 0);
    const key = form.tiers.reduce((max, tier) => Math.max(max, tier.key), 0) + 1;
    patch("tiers", [...form.tiers, { key, count: String(last ? last + 5 : 3), title: "", rewards: [] }]);
  };

  const sortTiers = () => patch("tiers", [...form.tiers].sort((a, b) => toInt(a.count) - toInt(b.count)));

  const togglePlatform = (key: string, checked: boolean) =>
    patch(
      "platforms",
      checked
        ? platforms.map((platform) => platform.key).filter((item) => item === key || form.platforms.includes(item))
        : form.platforms.filter((item) => item !== key)
    );

  const addDomain = () => {
    const domain = normalizeDomain(domainInput);
    if (!domain || !domain.includes(".")) {
      toast.error("请输入有效的域名，例如 kuaishou.com");
      return;
    }
    if (form.blockedDomains.includes(domain)) {
      setDomainInput("");
      return;
    }
    if (form.blockedDomains.length >= limits.maxBlockedDomains) {
      toast.error(`禁用域名最多 ${limits.maxBlockedDomains} 个`);
      return;
    }
    patch("blockedDomains", [...form.blockedDomains, domain]);
    setDomainInput("");
  };

  /** 在光标处插入变量，插完把光标放到变量后面 */
  const insertToken = (token: string) => {
    const element = templateRef.current;
    const current = form.shareTemplate;
    const start = element?.selectionStart ?? current.length;
    const end = element?.selectionEnd ?? current.length;
    patch("shareTemplate", `${current.slice(0, start)}${token}${current.slice(end)}`.slice(0, limits.maxShareLength));
    requestAnimationFrame(() => {
      if (!element) return;
      element.focus();
      element.setSelectionRange(start + token.length, start + token.length);
    });
  };

  const submit = async () => {
    const first = problems.find((problem) => !problem.ok);
    if (first) {
      toast.error(`请先满足：${first.label}`);
      return;
    }
    try {
      await saveMutation.mutateAsync({ id: campaign?.id, payload: toPayload(form, rewardCatalog) });
      toast.success(campaign ? "活动已保存" : "活动已创建，当前为草稿");
      onClose();
    } catch (error) {
      toast.error(errorMessage(error, "保存失败"));
    }
  };

  return (
    <>
      <SheetHeader className="shrink-0 gap-1 border-b px-5 py-4 pr-12">
        <SheetTitle className="flex items-center gap-2 text-base">
          {campaign ? "编辑活动" : "新建活动"}
          {campaign ? <StatusBadge status={campaign.status} /> : null}
        </SheetTitle>
        <SheetDescription>
          {campaign
            ? campaign.status === "online"
              ? `编号 ${campaign.id}，上线中。保存后规则、奖励与时间立即对用户生效，已发放的奖励不受影响。`
              : `编号 ${campaign.id}，保存后不改变上线状态。`
            : "新建的活动为草稿，在列表里上线后用户才看得到。"}
        </SheetDescription>
      </SheetHeader>

      <div className="grid min-h-0 flex-1 overflow-y-auto lg:grid-cols-[minmax(0,1fr)_300px] lg:overflow-hidden">
        <div className="flex flex-col gap-0 divide-y lg:overflow-y-auto">
          <FormBlock icon={FileText} tone="primary" title="基本信息">
            <Field label="标题" counter={`${form.title.length}/${limits.maxTitleLength}`}>
              <Input
                value={form.title}
                maxLength={limits.maxTitleLength}
                onChange={(event) => patch("title", event.target.value)}
                placeholder="例如：推荐有礼"
              />
            </Field>
            <Field label="副标题" counter={`${form.subtitle.length}/${limits.maxSubtitleLength}`}>
              <Input
                value={form.subtitle}
                maxLength={limits.maxSubtitleLength}
                onChange={(event) => patch("subtitle", event.target.value)}
                placeholder="一句话说明，显示在标题下方"
              />
            </Field>
            <Field label="活动规则" counter={`${form.rulesText.length}/${limits.maxRulesLength}`} hint="用户端原样展示">
              <Textarea
                value={form.rulesText}
                rows={5}
                maxLength={limits.maxRulesLength}
                onChange={(event) => patch("rulesText", event.target.value)}
              />
            </Field>
          </FormBlock>

          <FormBlock
            icon={CalendarClock}
            tone="info"
            title="时间"
            description={`按浏览器时区 ${timezone} 填写；活动最长 ${limits.maxDurationDays} 天`}
          >
            <div className="grid gap-4 sm:grid-cols-2">
              <Field label="开始">
                <Input type="datetime-local" value={form.startsAt} onChange={(event) => patch("startsAt", event.target.value)} />
              </Field>
              <Field label="结束">
                <Input type="datetime-local" value={form.endsAt} onChange={(event) => patch("endsAt", event.target.value)} />
              </Field>
            </div>
            <div className="grid gap-4 sm:grid-cols-2">
              <Field label="领取宽限" hint={`活动结束后还能领取邀请档位的天数，0–${limits.maxClaimGraceDays}`}>
                <NumberInput
                  value={form.claimGraceDays}
                  min={0}
                  max={limits.maxClaimGraceDays}
                  onChange={(value) => patch("claimGraceDays", value)}
                  suffix="天"
                />
              </Field>
            </div>
          </FormBlock>

          <FormBlock
            icon={UserPlus}
            tone="violet"
            title="邀请好友"
            description="活动期间成功邀请满某档人数，用户自己领取这档奖励；人数与防刷沿用邀请的规则"
            aside={<Switch checked={form.inviteEnabled} onCheckedChange={(value) => patch("inviteEnabled", value)} aria-label="开启邀请任务" />}
          >
            {form.inviteEnabled ? (
              <>
                <div className="flex items-center justify-between gap-2">
                  <Label className="text-xs font-medium">档位</Label>
                  <div className="flex items-center gap-2">
                    {!tiersIncreasing ? (
                      <Button size="xs" variant="outline" onClick={sortTiers}>
                        <ArrowDownUp />
                        按人数排序
                      </Button>
                    ) : null}
                    <span className="text-xs tabular-nums text-muted-foreground">
                      {form.tiers.length}/{limits.maxTiers}
                    </span>
                  </div>
                </div>
                {form.tiers.length === 0 ? (
                  <p className="rounded-lg border border-dashed px-3 py-4 text-center text-xs text-muted-foreground">暂无档位</p>
                ) : null}
                {form.tiers.map((tier, index) => {
                  const count = toInt(tier.count);
                  const outOfOrder = index > 0 && count <= toInt(form.tiers[index - 1].count);
                  return (
                    <div key={tier.key} className="flex flex-col gap-3 rounded-xl border p-4">
                      <div className="flex items-end gap-3">
                        <div className="grid flex-1 gap-4 sm:grid-cols-2">
                          <Field label={`第 ${index + 1} 档：邀请满`}>
                            <NumberInput
                              value={tier.count}
                              min={1}
                              max={limits.maxTierCount}
                              invalid={outOfOrder}
                              onChange={(value) => patchTier(tier.key, { count: value })}
                              suffix="人"
                            />
                          </Field>
                          <Field label="档位名称" counter={`${tier.title.length}/${limits.maxTierTitleLength}`}>
                            <Input
                              value={tier.title}
                              maxLength={limits.maxTierTitleLength}
                              placeholder="不设置"
                              onChange={(event) => patchTier(tier.key, { title: event.target.value })}
                            />
                          </Field>
                        </div>
                        <Button
                          size="icon"
                          variant="ghost"
                          className="size-9 shrink-0"
                          aria-label="移除档位"
                          onClick={() => patch("tiers", form.tiers.filter((item) => item.key !== tier.key))}
                        >
                          <Trash2 className="size-3.5" />
                        </Button>
                      </div>
                      {outOfOrder ? (
                        <p className="-mt-1 text-xs text-red-600 dark:text-red-400">人数须多于上一档</p>
                      ) : null}
                      <RewardListEditor
                        catalog={rewardCatalog}
                        maxRewards={limits.maxRewards}
                        rewards={tier.rewards}
                        plans={plans}
                        features={features}
                        emptyText="暂无奖励"
                        addLabel="添加奖励"
                        onChange={(rewards) => patchTier(tier.key, { rewards })}
                      />
                    </div>
                  );
                })}
                {form.tiers.length < limits.maxTiers ? (
                  <Button
                    variant="outline"
                    size="sm"
                    className="h-8 w-full justify-start border-dashed text-xs font-normal text-muted-foreground"
                    onClick={addTier}
                  >
                    <Plus className="size-3.5" /> 添加档位
                  </Button>
                ) : null}

                <ToggleRow
                  label="展示排行"
                  hint={form.leaderboard ? "用户端按邀请人数展示前若干名，昵称打码" : "用户端不展示排行"}
                  checked={form.leaderboard}
                  onChange={(value) => patch("leaderboard", value)}
                />
                {form.leaderboard ? (
                  <div className="grid gap-4 sm:grid-cols-2">
                    <Field label="排行人数" hint={`${limits.minLeaderboardSize}–${limits.maxLeaderboardSize}`}>
                      <NumberInput
                        value={form.leaderboardSize}
                        min={limits.minLeaderboardSize}
                        max={limits.maxLeaderboardSize}
                        onChange={(value) => patch("leaderboardSize", value)}
                        suffix="名"
                      />
                    </Field>
                  </div>
                ) : null}
              </>
            ) : (
              <p className="text-xs text-muted-foreground">本期不设邀请任务</p>
            )}
          </FormBlock>

          <FormBlock
            icon={Share2}
            tone="success"
            title="分享推广"
            description="用户在允许的平台发文推荐，提交文章链接，审核通过后发放奖励"
            aside={<Switch checked={form.shareEnabled} onCheckedChange={(value) => patch("shareEnabled", value)} aria-label="开启分享任务" />}
          >
            {form.shareEnabled ? (
              <>
                <Field label="允许的平台" hint="域名按标签边界后缀匹配，子域名同样认可">
                  <div className="grid gap-2 sm:grid-cols-2">
                    {platforms.map((platform) => {
                      const checked = form.platforms.includes(platform.key);
                      return (
                        <label
                          key={platform.key}
                          className={cn(
                            "flex cursor-pointer items-start gap-2.5 rounded-lg border px-3 py-2.5 transition-colors hover:bg-muted/40",
                            checked && "border-primary/40 bg-primary/5"
                          )}
                        >
                          <Checkbox
                            className="mt-0.5"
                            checked={checked}
                            onCheckedChange={(next) => togglePlatform(platform.key, Boolean(next))}
                          />
                          <span className="flex min-w-0 flex-col gap-0.5">
                            <span className="text-sm font-medium">{platform.name}</span>
                            <span className="truncate font-mono text-[11px] text-muted-foreground">
                              {platform.domains.join(" ")}
                            </span>
                          </span>
                        </label>
                      );
                    })}
                    {forbidden.map((platform) => (
                      <div
                        key={platform.name}
                        className="flex items-start gap-2.5 rounded-lg border border-dashed bg-muted/40 px-3 py-2.5"
                        aria-disabled="true"
                      >
                        <Lock className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
                        <span className="flex min-w-0 flex-col gap-0.5">
                          <span className="flex flex-wrap items-center gap-1.5 text-sm font-medium text-muted-foreground">
                            {platform.name}
                            <Badge variant="danger" size="sm">
                              始终禁止，不可关闭
                            </Badge>
                          </span>
                          <span className="truncate font-mono text-[11px] text-muted-foreground" title={platform.domains.join(" ")}>
                            {platform.domains.join(" ")}
                          </span>
                        </span>
                      </div>
                    ))}
                  </div>
                </Field>

                <Field label="每篇奖励" counter={`${form.shareRewards.length}/${limits.maxRewards}`} hint="每篇审核通过的文章发放一次">
                  <RewardListEditor
                    catalog={rewardCatalog}
                    maxRewards={limits.maxRewards}
                    rewards={form.shareRewards}
                    plans={plans}
                    features={features}
                    emptyText="暂无奖励"
                    addLabel="添加奖励"
                    onChange={(rewards) => patch("shareRewards", rewards)}
                  />
                </Field>

                <div className="grid gap-4 sm:grid-cols-3">
                  <Field label="每人最多通过" hint={`1–${limits.maxPerUserLimit}`}>
                    <NumberInput
                      value={form.maxApprovedPerUser}
                      min={1}
                      max={limits.maxPerUserLimit}
                      onChange={(value) => patch("maxApprovedPerUser", value)}
                      suffix="篇"
                    />
                  </Field>
                  <Field label="每人每平台最多" hint="待审核的也占名额">
                    <NumberInput
                      value={form.maxApprovedPerPlatform}
                      min={1}
                      max={limits.maxPerUserLimit}
                      onChange={(value) => patch("maxApprovedPerPlatform", value)}
                      suffix="篇"
                    />
                  </Field>
                  <Field label="每人每天最多提交" hint={`1–${limits.maxPerUserLimit}`}>
                    <NumberInput
                      value={form.dailySubmitLimit}
                      min={1}
                      max={limits.maxPerUserLimit}
                      onChange={(value) => patch("dailySubmitLimit", value)}
                      suffix="次"
                    />
                  </Field>
                </div>

                <Field
                  label="额外禁用域名"
                  counter={`${form.blockedDomains.length}/${limits.maxBlockedDomains}`}
                  hint="写死的禁用平台之外再拒绝的域名，子域名一并拒绝"
                >
                  <div className="flex flex-wrap gap-1.5">
                    {form.blockedDomains.length === 0 ? (
                      <span className="text-xs text-muted-foreground">未设置</span>
                    ) : null}
                    {form.blockedDomains.map((domain) => (
                      <Badge key={domain} variant="outline" className="gap-1 pr-1 font-mono font-normal">
                        {domain}
                        <button
                          type="button"
                          aria-label={`移除 ${domain}`}
                          className="inline-flex size-4 items-center justify-center rounded text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
                          onClick={() => patch("blockedDomains", form.blockedDomains.filter((item) => item !== domain))}
                        >
                          <X className="size-3" />
                        </button>
                      </Badge>
                    ))}
                  </div>
                  <div className="flex flex-wrap items-center gap-2">
                    <Input
                      value={domainInput}
                      className="h-8 w-56 font-mono text-xs"
                      placeholder="例如 kuaishou.com"
                      onChange={(event) => setDomainInput(event.target.value)}
                      onKeyDown={(event) => {
                        if (event.key === "Enter") {
                          event.preventDefault();
                          addDomain();
                        }
                      }}
                    />
                    <Button size="sm" variant="outline" onClick={addDomain} disabled={!domainInput.trim()}>
                      <Plus />
                      添加
                    </Button>
                    <Button
                      size="sm"
                      variant="ghost"
                      className="text-muted-foreground"
                      onClick={() => patch("blockedDomains", [...(catalog?.defaults?.share?.blockedDomains ?? DEFAULT_BLOCKED_DOMAINS)])}
                    >
                      <RotateCcw />
                      恢复默认
                    </Button>
                  </div>
                </Field>

                <Field label="推荐文案" counter={`${form.shareTemplate.length}/${limits.maxShareLength}`} hint="用户端按当前用户与所选平台替换变量后供复制">
                  <Textarea
                    ref={templateRef}
                    value={form.shareTemplate}
                    rows={3}
                    maxLength={limits.maxShareLength}
                    onChange={(event) => patch("shareTemplate", event.target.value)}
                  />
                  <div className="flex flex-wrap items-center gap-1.5">
                    <span className="text-[11px] text-muted-foreground">插入</span>
                    {SHARE_TOKENS.map((item) => (
                      <button
                        key={item.token}
                        type="button"
                        onClick={() => insertToken(item.token)}
                        className="rounded-md border bg-muted px-1.5 py-0.5 text-[11px] transition-colors hover:bg-muted-foreground/10"
                      >
                        <span className="font-mono">{item.token}</span>
                        <span className="ml-1 text-muted-foreground">{item.label}</span>
                      </button>
                    ))}
                  </div>
                </Field>
              </>
            ) : (
              <p className="text-xs text-muted-foreground">本期不设分享任务</p>
            )}
          </FormBlock>
        </div>

        <aside className="flex flex-col gap-4 border-t bg-muted/30 p-5 lg:overflow-y-auto lg:border-t-0 lg:border-l">
          <div className="flex flex-col gap-2">
            <div className="text-xs font-medium text-muted-foreground">要点</div>
            <dl className="flex flex-col gap-2 rounded-xl border bg-card p-3 text-xs">
              <SummaryRow label="开始" value={formatDateTimeMinute(startsAt)} />
              <SummaryRow label="结束" value={formatDateTimeMinute(endsAt)} />
              <SummaryRow label="领取截止" value={form.inviteEnabled ? formatDateTimeMinute(claimUntil) : "—"} />
              <SummaryRow label="邀请档位" value={form.inviteEnabled ? `${form.tiers.length} 档` : "未开启"} />
              <SummaryRow
                label="分享平台"
                value={
                  form.shareEnabled
                    ? form.platforms.length
                      ? platforms
                          .filter((platform) => form.platforms.includes(platform.key))
                          .map((platform) => platform.name)
                          .join("、")
                      : "未选择"
                    : "未开启"
                }
              />
            </dl>
          </div>
          <div className="flex flex-col gap-2 rounded-xl border border-dashed bg-card p-3">
            <div className="flex items-center gap-1.5 text-xs font-medium">
              <Ban className="size-3.5 text-red-500" />
              始终禁止的平台
            </div>
            <p className="text-xs leading-5 text-muted-foreground">
              {forbidden.map((platform) => platform.name).join("、")}的链接任何活动都不认，提交时直接拒绝，控制台不可关闭。
            </p>
          </div>
          <div className="flex flex-col gap-2">
            <div className="text-xs font-medium text-muted-foreground">保存前检查</div>
            <ul className="flex flex-col gap-1.5">
              {problems.map((problem) => (
                <li key={problem.label} className="flex items-center gap-2 text-xs">
                  {problem.ok ? (
                    <CheckCircle2 className="size-3.5 shrink-0 text-emerald-500" />
                  ) : (
                    <AlertCircle className="size-3.5 shrink-0 text-red-500" />
                  )}
                  <span className={problem.ok ? "text-muted-foreground" : ""}>{problem.label}</span>
                </li>
              ))}
            </ul>
          </div>
          <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
            <Gift className="size-3.5" />
            奖励引用的套餐与功能在保存与发放时由后端核对
          </div>
        </aside>
      </div>

      <div className="flex shrink-0 flex-wrap items-center justify-between gap-3 border-t px-5 py-3">
        <span className="text-xs text-muted-foreground">
          {campaign?.updatedAt
            ? `${campaign.updatedBy ? `${campaign.updatedBy} ` : ""}保存于 ${formatDateTimeMinute(campaign.updatedAt)}`
            : "尚未保存"}
        </span>
        <div className="flex items-center gap-2">
          <Button variant="ghost" size="sm" onClick={onClose} disabled={saveMutation.isPending}>
            取消
          </Button>
          <Button size="sm" onClick={submit} disabled={saveMutation.isPending || blocked}>
            {saveMutation.isPending ? <Loader2 className="animate-spin" /> : <Save />}
            {campaign ? "保存修改" : "创建活动"}
          </Button>
        </div>
      </div>
    </>
  );
}

function formatInt(value: number) {
  return value.toLocaleString("zh-CN");
}

function FormBlock({
  icon,
  tone,
  title,
  description,
  aside,
  children
}: {
  icon: ComponentType<{ className?: string }>;
  tone: Tone;
  title: string;
  description?: string;
  aside?: ReactNode;
  children: ReactNode;
}) {
  return (
    <section className="flex flex-col gap-4 px-5 py-5">
      <div className="flex items-center gap-2.5">
        <ToneIcon icon={icon} tone={tone} size="sm" />
        <div className="flex min-w-0 flex-1 flex-col">
          <h3 className="text-sm font-semibold">{title}</h3>
          {description ? <p className="text-xs text-muted-foreground">{description}</p> : null}
        </div>
        {aside ? <div className="shrink-0">{aside}</div> : null}
      </div>
      <div className="flex flex-col gap-4">{children}</div>
    </section>
  );
}

function Field({ label, hint, counter, children }: { label: string; hint?: string; counter?: string; children: ReactNode }) {
  return (
    <div className="flex min-w-0 flex-col gap-1.5">
      <div className="flex items-baseline justify-between gap-2">
        <Label className="text-xs font-medium">{label}</Label>
        {counter ? <span className="text-xs tabular-nums text-muted-foreground">{counter}</span> : null}
      </div>
      {children}
      {hint ? <p className="text-xs text-muted-foreground">{hint}</p> : null}
    </div>
  );
}

function NumberInput({
  value,
  onChange,
  min,
  max,
  suffix,
  invalid
}: {
  value: string;
  onChange: (value: string) => void;
  min?: number;
  max?: number;
  suffix?: string;
  invalid?: boolean;
}) {
  return (
    <div
      className={cn(
        "flex h-9 items-center rounded-md border bg-transparent shadow-xs focus-within:border-ring focus-within:ring-[3px] focus-within:ring-ring/50 dark:bg-input/30",
        invalid && "border-red-500/60"
      )}
    >
      <input
        type="number"
        inputMode="numeric"
        min={min}
        max={max}
        value={value}
        onChange={(event) => onChange(event.target.value)}
        className="h-full min-w-0 flex-1 bg-transparent px-3 text-sm tabular-nums outline-none placeholder:text-muted-foreground"
      />
      {suffix ? <span className="pr-3 text-xs text-muted-foreground">{suffix}</span> : null}
    </div>
  );
}

function ToggleRow({
  label,
  hint,
  checked,
  onChange
}: {
  label: string;
  hint?: string;
  checked: boolean;
  onChange: (value: boolean) => void;
}) {
  return (
    <label className="flex cursor-pointer items-center justify-between gap-3 rounded-lg border px-3 py-2.5 transition-colors hover:bg-muted/40">
      <span className="flex min-w-0 flex-col gap-0.5">
        <span className="text-sm font-medium">{label}</span>
        {hint ? <span className="text-xs text-muted-foreground">{hint}</span> : null}
      </span>
      <Switch checked={checked} onCheckedChange={onChange} />
    </label>
  );
}

function SummaryRow({ label, value }: { label: string; value: ReactNode }) {
  return (
    <div className="flex items-start justify-between gap-3">
      <dt className="shrink-0 text-muted-foreground">{label}</dt>
      <dd className="min-w-0 text-right break-all">{value}</dd>
    </div>
  );
}
