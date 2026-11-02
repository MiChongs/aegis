"use client";

import { Badge } from "@/components/ui/badge";
import type {
  PromoCampaign,
  PromoCatalog,
  PromoCatalogLimits,
  PromoForbiddenPlatform,
  PromoPhase,
  PromoPlatform,
  PromoStatus,
  PromoSubmissionStatus
} from "@/lib/api/promo";
import { formatDateTimeMinute, parseInstant } from "@/lib/datetime";

/**
 * 推广活动的展示口径：状态与阶段文案、审核结论、平台目录兜底与上限。集中在一处，
 * 活动列表、编辑器、提交审核与统计说的是同一套话。
 */

type BadgeVariant = "success" | "warning" | "secondary" | "info" | "outline" | "danger";

export const STATUS_META: Record<PromoStatus, { label: string; variant: BadgeVariant }> = {
  draft: { label: "草稿", variant: "outline" },
  online: { label: "上线", variant: "success" },
  offline: { label: "下线", variant: "secondary" }
};

export const STATUSES: PromoStatus[] = ["draft", "online", "offline"];

export const PHASE_META: Record<PromoPhase, { label: string; variant: BadgeVariant }> = {
  upcoming: { label: "未开始", variant: "info" },
  active: { label: "进行中", variant: "success" },
  ended: { label: "已结束", variant: "secondary" }
};

export const SUBMISSION_STATUS_META: Record<PromoSubmissionStatus, { label: string; variant: BadgeVariant }> = {
  pending: { label: "待审核", variant: "warning" },
  approved: { label: "已通过", variant: "success" },
  rejected: { label: "已驳回", variant: "danger" }
};

export const SUBMISSION_STATUSES: PromoSubmissionStatus[] = ["pending", "approved", "rejected"];

/** 审核失败的原因（后端没带 message 时用） */
export const REVIEW_REASON_LABEL: Record<string, string> = {
  already_reviewed: "已经审核过",
  not_found: "提交不存在",
  user_limited: "该用户通过篇数已满",
  platform_limited: "该用户在这个平台的篇数已满",
  daily_limited: "今天的提交次数已用完",
  reward_unavailable: "奖励引用的套餐或功能已失效",
  share_disabled: "本期没有分享任务"
};

export function StatusBadge({ status }: { status: PromoStatus }) {
  const meta = STATUS_META[status] ?? { label: status, variant: "outline" as const };
  return (
    <Badge variant={meta.variant} size="sm">
      {meta.label}
    </Badge>
  );
}

export function PhaseBadge({ phase }: { phase: PromoPhase }) {
  const meta = PHASE_META[phase] ?? { label: phase, variant: "outline" as const };
  return (
    <Badge variant={meta.variant} size="sm">
      {meta.label}
    </Badge>
  );
}

export function SubmissionStatusBadge({ status }: { status: PromoSubmissionStatus }) {
  const meta = SUBMISSION_STATUS_META[status] ?? { label: status, variant: "outline" as const };
  return (
    <Badge variant={meta.variant} size="sm">
      {meta.label}
    </Badge>
  );
}

/**
 * 活动阶段，与后端 `promo.Phase` 一致：下线即已结束；否则按时间。
 * 列表项自带 `phase` 时优先用后端的，这里只给详情接口这类不带阶段的兜底。
 */
export function campaignPhase(campaign: Pick<PromoCampaign, "status" | "startsAt" | "endsAt">, now = Date.now()): PromoPhase {
  if (campaign.status === "offline") return "ended";
  const starts = parseInstant(campaign.startsAt);
  const ends = parseInstant(campaign.endsAt);
  if (starts && now < starts.getTime()) return "upcoming";
  if (ends && now >= ends.getTime()) return "ended";
  return "active";
}

export function periodLabel(startsAt?: string, endsAt?: string) {
  return `${formatDateTimeMinute(startsAt)} 至 ${formatDateTimeMinute(endsAt)}`;
}

export function formatCount(value?: number | null) {
  return (value ?? 0).toLocaleString("zh-CN");
}

/** 目录没到时的平台兜底，与后端 promo.PlatformCatalog 一致 */
const FALLBACK_PLATFORMS: PromoPlatform[] = [
  { key: "wechat_mp", name: "微信公众号", domains: ["mp.weixin.qq.com"], defaultOn: true },
  { key: "xiaohongshu", name: "小红书", domains: ["xiaohongshu.com", "xhslink.com"], packageName: "com.xingin.xhs", defaultOn: true },
  { key: "coolapk", name: "酷安", domains: ["coolapk.com", "coolapk1s.com"], packageName: "com.coolapk.market", defaultOn: true },
  { key: "zhihu", name: "知乎", domains: ["zhihu.com"], packageName: "com.zhihu.android" },
  { key: "tieba", name: "百度贴吧", domains: ["tieba.baidu.com"], packageName: "com.baidu.tieba" },
  { key: "weibo", name: "微博", domains: ["weibo.com", "weibo.cn"], packageName: "com.sina.weibo" }
];

/** 写死的禁用平台兜底，与后端 promo.ForbiddenPlatforms 一致 */
const FALLBACK_FORBIDDEN: PromoForbiddenPlatform[] = [
  { name: "抖音", domains: ["douyin.com", "iesdouyin.com", "amemv.com", "douyinvod.com", "douyinpic.com"] },
  { name: "抖音火山版", domains: ["huoshan.com"] }
];

export const DEFAULT_BLOCKED_DOMAINS = ["kuaishou.com", "gifshow.com", "kwai.com", "chenzhongtech.com"];

export function catalogPlatforms(catalog?: PromoCatalog) {
  return catalog?.platforms?.length ? catalog.platforms : FALLBACK_PLATFORMS;
}

export function catalogForbidden(catalog?: PromoCatalog) {
  return catalog?.forbidden?.length ? catalog.forbidden : FALLBACK_FORBIDDEN;
}

export function platformName(key: string, catalog?: PromoCatalog) {
  return catalogPlatforms(catalog).find((platform) => platform.key === key)?.name ?? key;
}

/** 上限兜底，与后端 promo/types.go 一致 */
const DEFAULT_LIMITS: Required<PromoCatalogLimits> = {
  maxRewards: 6,
  maxTiers: 10,
  maxTierCount: 100_000,
  maxTitleLength: 32,
  maxSubtitleLength: 64,
  maxRulesLength: 2000,
  maxTierTitleLength: 16,
  maxShareLength: 300,
  maxDurationDays: 180,
  maxClaimGraceDays: 30,
  minLeaderboardSize: 10,
  maxLeaderboardSize: 100,
  maxPerUserLimit: 50,
  maxBlockedDomains: 20,
  maxRejectReasonLength: 200,
  maxReviewBatch: 100
};

export function promoLimits(catalog?: PromoCatalog): Required<PromoCatalogLimits> {
  return { ...DEFAULT_LIMITS, ...(catalog?.limits ?? {}) };
}

/** 与后端 NormalizeDomain 一致：去协议、路径、端口与首尾的点，转小写 */
export function normalizeDomain(raw: string) {
  let value = raw.trim().toLowerCase();
  const scheme = value.indexOf("://");
  if (scheme >= 0) value = value.slice(scheme + 3);
  const cut = value.search(/[/?#]/);
  if (cut >= 0) value = value.slice(0, cut);
  const port = value.lastIndexOf(":");
  if (port >= 0) value = value.slice(0, port);
  return value.replace(/^\.+|\.+$/g, "");
}
