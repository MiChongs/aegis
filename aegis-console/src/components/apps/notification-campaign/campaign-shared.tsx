"use client";

import { Loader2 } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Progress } from "@/components/ui/progress";
import type {
  NotificationAudience,
  NotificationAudienceAccount,
  NotificationAudienceMembership,
  NotificationAudienceScope,
  NotificationCampaign,
  NotificationCampaignStatus
} from "@/lib/api/notification-campaigns";
import { appSectionHref } from "@/lib/app-sections";

/**
 * 站内信群发的共用件：状态徽标、进度条、人群草稿与归一、幂等键、跳到消息列表的链接。
 * 人群的校验与后端 `notification.NormalizeAudience` 同规则同说法，提交仍以后端为准。
 */

export const STATUS_META: Record<
  NotificationCampaignStatus,
  { label: string; variant: "info" | "success" | "danger" | "warning" | "secondary" | "outline"; spinning?: boolean }
> = {
  scheduled: { label: "定时", variant: "info" },
  queued: { label: "排队中", variant: "secondary" },
  running: { label: "发送中", variant: "info", spinning: true },
  succeeded: { label: "已完成", variant: "success" },
  failed: { label: "失败", variant: "danger" },
  cancelled: { label: "已取消", variant: "secondary" },
  recalling: { label: "撤回中", variant: "warning", spinning: true },
  recalled: { label: "已撤回", variant: "outline" }
};

export const STATUS_OPTIONS = (Object.keys(STATUS_META) as NotificationCampaignStatus[]).map((value) => ({
  value,
  label: STATUS_META[value].label
}));

export function formatCount(value?: number | null) {
  return typeof value === "number" ? value.toLocaleString("zh-CN") : "—";
}

export function CampaignStatusBadge({ campaign }: { campaign: Pick<NotificationCampaign, "status" | "statusLabel" | "message"> }) {
  const meta = STATUS_META[campaign.status] ?? { label: campaign.statusLabel || campaign.status, variant: "secondary" as const };
  return (
    <Badge variant={meta.variant} size="sm" className="gap-1" title={campaign.message || undefined}>
      {meta.spinning ? <Loader2 className="size-3 animate-spin" /> : null}
      {campaign.statusLabel || meta.label}
    </Badge>
  );
}

const RECALL_STATUSES = new Set<NotificationCampaignStatus>(["recalling", "recalled"]);

/**
 * 进度：发送阶段是「扫过的人 / 人群人数」，撤回阶段是「撤回的条数 / 写入的条数」。
 * 人群人数在开始发送时才定格（之前为 null），这时不画进度条。
 */
export function campaignProgress(campaign: NotificationCampaign) {
  if (RECALL_STATUSES.has(campaign.status)) {
    const total = campaign.delivered;
    const percent = total > 0 ? Math.min(100, (campaign.recalled / total) * 100) : campaign.status === "recalled" ? 100 : 0;
    return { phase: "recall" as const, done: campaign.recalled, total, percent };
  }
  const total = campaign.total;
  const percent =
    total && total > 0 ? Math.min(100, (campaign.processed / total) * 100) : campaign.status === "succeeded" ? 100 : 0;
  return { phase: "send" as const, done: campaign.processed, total, percent };
}

export function CampaignProgress({ campaign, detailed }: { campaign: NotificationCampaign; detailed?: boolean }) {
  const progress = campaignProgress(campaign);
  if (progress.phase === "send" && progress.total === null) {
    return <span className="text-[11px] text-muted-foreground">未开始</span>;
  }
  return (
    <div className="min-w-40 space-y-1">
      <div className="flex items-center justify-between text-[11px] tabular-nums text-muted-foreground">
        <span>
          {progress.phase === "recall" ? "撤回 " : ""}
          {formatCount(progress.done)} / {formatCount(progress.total)}
        </span>
        <span>{progress.percent.toFixed(0)}%</span>
      </div>
      <Progress value={progress.percent} className="h-1.5" />
      {detailed ? (
        <div className="text-[11px] tabular-nums text-muted-foreground">
          送达 <span className="text-emerald-600 dark:text-emerald-400">{formatCount(campaign.delivered)}</span>
          ，跳过 {formatCount(campaign.skipped)}
          {campaign.recalled > 0 ? `，撤回 ${formatCount(campaign.recalled)}` : ""}
        </div>
      ) : null}
    </div>
  );
}

/** 可以取消：还没结束发送的任务 */
export const canCancelCampaign = (campaign: NotificationCampaign) =>
  campaign.status === "scheduled" || campaign.status === "queued" || campaign.status === "running";

/** 可以撤回：已经写出过消息、且没在撤回的任务（还没开始的用取消即可） */
export const canRecallCampaign = (campaign: NotificationCampaign) =>
  !RECALL_STATUSES.has(campaign.status) && (campaign.delivered > 0 || campaign.status === "running");

/** 应用详情审计区块里按这个任务筛选的消息列表 */
export function campaignNotificationsHref(appKey: string, campaignId: number) {
  const params = new URLSearchParams({ tab: "audit", view: "notification", campaignId: String(campaignId) });
  return `${appSectionHref(appKey, "audit").split("?")[0]}?${params.toString()}`;
}

/**
 * 幂等键：每打开一次编辑器生成一个，重复提交返回同一个任务。
 * 不用 crypto.randomUUID：它在非安全上下文（http 局域网访问控制台）下不存在，getRandomValues 一直都在。
 */
export function newRequestId() {
  const bytes = new Uint8Array(16);
  crypto.getRandomValues(bytes);
  bytes[6] = (bytes[6] & 0x0f) | 0x40;
  bytes[8] = (bytes[8] & 0x3f) | 0x80;
  const hex = Array.from(bytes, (byte) => byte.toString(16).padStart(2, "0")).join("");
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
}

// ── 人群草稿 ──

export type ActivityMode = "any" | "active" | "inactive";

/** 编辑器里的人群：数字先按字符串存（允许清空），提交前统一转换 */
export type AudienceDraft = {
  scope: NotificationAudienceScope;
  userIdsText: string;
  excludeText: string;
  account: NotificationAudienceAccount;
  membership: NotificationAudienceMembership;
  registeredWithinDays: string;
  registeredBeforeDays: string;
  /** 活跃与沉默二选一（后端不允许同时设） */
  activity: ActivityMode;
  activityDays: string;
  minLevel: string;
  maxLevel: string;
  platforms: string[];
  keyword: string;
  limit: string;
};

export function emptyAudienceDraft(): AudienceDraft {
  return {
    scope: "all",
    userIdsText: "",
    excludeText: "",
    account: "available",
    membership: "",
    registeredWithinDays: "",
    registeredBeforeDays: "",
    activity: "any",
    activityDays: "",
    minLevel: "",
    maxLevel: "",
    platforms: [],
    keyword: "",
    limit: ""
  };
}

const numText = (value?: number) => (value && value > 0 ? String(value) : "");

/** 预设人群 → 草稿（保留当前的排除名单与账号状态以外的条件全部按预设重置） */
export function audienceDraftFromPreset(audience: NotificationAudience, current: AudienceDraft): AudienceDraft {
  const activity: ActivityMode = audience.activeWithinDays ? "active" : audience.inactiveDays ? "inactive" : "any";
  return {
    ...emptyAudienceDraft(),
    excludeText: current.excludeText,
    account: audience.account ?? current.account,
    limit: current.limit,
    scope: audience.scope,
    userIdsText: (audience.userIds ?? []).join("\n"),
    membership: audience.membership ?? "",
    registeredWithinDays: numText(audience.registeredWithinDays),
    registeredBeforeDays: numText(audience.registeredBeforeDays),
    activity,
    activityDays: numText(audience.activeWithinDays || audience.inactiveDays),
    minLevel: numText(audience.minLevel),
    maxLevel: numText(audience.maxLevel),
    platforms: audience.platforms ?? [],
    keyword: audience.keyword ?? ""
  };
}

/** 逗号、顿号、分号、空白、换行分隔的用户 ID；返回合法 ID（去重）与认不出的片段 */
export function parseUserIds(text: string): { ids: number[]; invalid: string[] } {
  const ids: number[] = [];
  const invalid: string[] = [];
  const seen = new Set<number>();
  for (const token of text.split(/[\s,，、;；]+/)) {
    if (!token) continue;
    if (!/^\d+$/.test(token) || Number(token) <= 0 || !Number.isSafeInteger(Number(token))) {
      invalid.push(token);
      continue;
    }
    const id = Number(token);
    if (!seen.has(id)) {
      seen.add(id);
      ids.push(id);
    }
  }
  return { ids, invalid };
}

export type AudienceLimits = { maxUserIds: number; maxDays: number; maxLevel: number };

export const DEFAULT_AUDIENCE_LIMITS: AudienceLimits = { maxUserIds: 10000, maxDays: 3650, maxLevel: 1000 };

const intOf = (text: string) => (text.trim() ? Number(text.trim()) : 0);
const badInt = (text: string, max: number) => {
  if (!text.trim()) return false;
  const value = Number(text.trim());
  return !Number.isInteger(value) || value < 0 || value > max;
};

/**
 * 草稿 → 请求里的人群，以及第一条问题（与后端 NormalizeAudience 的说法一致）。
 * 有问题时 `audience` 为 null，预览与提交都不发。
 */
export function buildAudience(
  draft: AudienceDraft,
  limits: AudienceLimits
): { audience: NotificationAudience | null; problem: string | null; userIdCount: number; excludeCount: number } {
  const users = parseUserIds(draft.userIdsText);
  const exclude = parseUserIds(draft.excludeText);
  const fail = (problem: string) => ({ audience: null, problem, userIdCount: users.ids.length, excludeCount: exclude.ids.length });

  if (exclude.invalid.length) return fail(`排除名单里有无法识别的 ID：${exclude.invalid.slice(0, 3).join("、")}`);
  if (exclude.ids.length > limits.maxUserIds) return fail(`排除名单最多 ${limits.maxUserIds} 人`);
  if (draft.limit.trim() && (!Number.isInteger(Number(draft.limit.trim())) || Number(draft.limit.trim()) < 0)) {
    return fail("人数上限必须是非负整数");
  }

  const audience: NotificationAudience = { scope: draft.scope, account: draft.account };
  if (exclude.ids.length) audience.excludeUserIds = exclude.ids;
  const limit = intOf(draft.limit);
  if (limit > 0) audience.limit = limit;

  if (draft.scope === "users") {
    if (users.invalid.length) return fail(`有无法识别的 ID：${users.invalid.slice(0, 3).join("、")}`);
    if (!users.ids.length) return fail("指定用户不能为空");
    if (users.ids.length > limits.maxUserIds) return fail(`指定用户最多 ${limits.maxUserIds} 人，更多请改用条件筛选`);
    audience.userIds = users.ids;
  }

  if (draft.scope === "filter") {
    const days = [draft.registeredWithinDays, draft.registeredBeforeDays, draft.activity === "any" ? "" : draft.activityDays];
    if (days.some((text) => badInt(text, limits.maxDays))) return fail(`天数只能在 0 到 ${limits.maxDays} 之间`);
    if (badInt(draft.minLevel, limits.maxLevel) || badInt(draft.maxLevel, limits.maxLevel)) {
      return fail(`等级只能在 0 到 ${limits.maxLevel} 之间`);
    }
    const within = intOf(draft.registeredWithinDays);
    const before = intOf(draft.registeredBeforeDays);
    if (within > 0 && before > 0 && before > within) return fail("注册天数的范围是空的：注册满的天数大于注册不超过的天数");
    const minLevel = intOf(draft.minLevel);
    const maxLevel = intOf(draft.maxLevel);
    if (minLevel > 0 && maxLevel > 0 && minLevel > maxLevel) return fail("最低等级不能高于最高等级");
    const keyword = draft.keyword.trim();
    if ([...keyword].length > 64) return fail("关键词不能超过 64 个字");
    const activityDays = intOf(draft.activityDays);
    if (draft.activity !== "any" && activityDays <= 0) return fail(draft.activity === "active" ? "请填写活跃天数" : "请填写未活跃天数");

    if (draft.membership) audience.membership = draft.membership;
    if (within > 0) audience.registeredWithinDays = within;
    if (before > 0) audience.registeredBeforeDays = before;
    if (draft.activity === "active") audience.activeWithinDays = activityDays;
    if (draft.activity === "inactive") audience.inactiveDays = activityDays;
    if (minLevel > 0) audience.minLevel = minLevel;
    if (maxLevel > 0) audience.maxLevel = maxLevel;
    if (draft.platforms.length) audience.platforms = draft.platforms;
    if (keyword) audience.keyword = keyword;
  }

  return { audience, problem: null, userIdCount: users.ids.length, excludeCount: exclude.ids.length };
}

/** 两个人群是否相同（预设高亮用；账号状态缺省按 available） */
export function sameAudience(a: NotificationAudience, b: NotificationAudience) {
  const norm = (value: NotificationAudience) =>
    JSON.stringify({
      scope: value.scope,
      membership: value.membership ?? "",
      registeredWithinDays: value.registeredWithinDays ?? 0,
      registeredBeforeDays: value.registeredBeforeDays ?? 0,
      activeWithinDays: value.activeWithinDays ?? 0,
      inactiveDays: value.inactiveDays ?? 0,
      minLevel: value.minLevel ?? 0,
      maxLevel: value.maxLevel ?? 0,
      platforms: [...(value.platforms ?? [])].sort(),
      keyword: value.keyword ?? "",
      userIds: value.userIds ?? []
    });
  return norm(a) === norm(b);
}
