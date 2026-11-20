"use client";

import { Crown, Filter, Globe, Loader2, Users } from "lucide-react";
import { NotificationField } from "@/components/notifications/notification-content-fields";
import { Pills } from "@/components/apps/lottery/compute/lottery-compute-shared";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Textarea } from "@/components/ui/textarea";
import type {
  NotificationAudience,
  NotificationAudienceAccount,
  NotificationAudienceMembership,
  NotificationAudiencePreview,
  NotificationAudienceScope,
  NotificationCampaignCatalog
} from "@/lib/api/notification-campaigns";
import { formatRelative } from "@/lib/datetime";
import { cn } from "@/lib/utils";
import {
  audienceDraftFromPreset,
  formatCount,
  sameAudience,
  type ActivityMode,
  type AudienceDraft,
  type AudienceLimits
} from "./campaign-shared";

/**
 * 人群构建：预设、三种范围（一键全站 / 指定用户 / 条件筛选）、通用的账号状态、排除名单与人数上限。
 * 条件与取值都来自 `/campaigns/catalog`，这里不另抄。
 */

const ANY = "__any__";

const SCOPES: Array<{ value: NotificationAudienceScope; label: string; description: string; icon: typeof Globe }> = [
  { value: "all", label: "一键全站", description: "应用下全部用户", icon: Globe },
  { value: "users", label: "指定用户", description: "粘贴用户 ID", icon: Users },
  { value: "filter", label: "条件筛选", description: "会员、注册、活跃、等级、平台", icon: Filter }
];

const ACTIVITY_OPTIONS: Array<{ value: ActivityMode; label: string }> = [
  { value: "any", label: "不限" },
  { value: "active", label: "最近活跃" },
  { value: "inactive", label: "长期未活跃" }
];

function NumberInput({
  value,
  onChange,
  placeholder,
  max,
  disabled
}: {
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
  max?: number;
  disabled?: boolean;
}) {
  return (
    <Input
      type="number"
      inputMode="numeric"
      min={0}
      max={max}
      value={value}
      disabled={disabled}
      placeholder={placeholder}
      className="h-8 text-xs tabular-nums"
      onChange={(event) => onChange(event.target.value)}
    />
  );
}

export function AudienceBuilder({
  draft,
  onChange,
  catalog,
  limits,
  userIdCount,
  excludeCount,
  current
}: {
  draft: AudienceDraft;
  onChange: (next: AudienceDraft) => void;
  catalog?: NotificationCampaignCatalog;
  limits: AudienceLimits;
  userIdCount: number;
  excludeCount: number;
  /** 当前草稿归一后的人群（不合法时为 null），预设高亮用 */
  current: NotificationAudience | null;
}) {
  const patch = (value: Partial<AudienceDraft>) => onChange({ ...draft, ...value });
  const presets = catalog?.presets ?? [];

  return (
    <div className="space-y-4">
      {presets.length ? (
        <div className="flex flex-wrap gap-1.5">
          {presets.map((preset) => {
            const on = current ? sameAudience(current, preset.audience) : false;
            return (
              <button
                key={preset.key}
                type="button"
                title={preset.description}
                aria-pressed={on}
                onClick={() => onChange(audienceDraftFromPreset(preset.audience, draft))}
                className={cn(
                  "h-7 rounded-lg border px-2.5 text-[11px] font-medium transition-colors",
                  on
                    ? "border-foreground/80 bg-foreground text-background"
                    : "border-border bg-card text-muted-foreground hover:text-foreground"
                )}
              >
                {preset.label}
              </button>
            );
          })}
        </div>
      ) : null}

      <div role="radiogroup" className="grid gap-2 sm:grid-cols-[1.4fr_1fr_1fr]">
        {SCOPES.map((scope) => {
          const on = draft.scope === scope.value;
          const primary = scope.value === "all";
          return (
            <button
              key={scope.value}
              type="button"
              role="radio"
              aria-checked={on}
              onClick={() => patch({ scope: scope.value })}
              className={cn(
                "flex items-center gap-3 rounded-xl border px-3 text-left transition-colors",
                primary ? "py-3.5" : "py-2.5",
                on ? "border-primary bg-primary/5 ring-1 ring-primary" : "border-border bg-card hover:bg-accent/40"
              )}
            >
              <span
                className={cn(
                  "inline-flex shrink-0 items-center justify-center rounded-lg",
                  primary ? "size-9" : "size-7",
                  on ? "bg-primary text-primary-foreground" : "bg-muted text-muted-foreground"
                )}
              >
                <scope.icon className={primary ? "size-4.5" : "size-3.5"} />
              </span>
              <span className="min-w-0">
                <span className={cn("block font-medium", primary ? "text-sm" : "text-xs")}>{scope.label}</span>
                <span className="block truncate text-[11px] text-muted-foreground">{scope.description}</span>
              </span>
            </button>
          );
        })}
      </div>

      {draft.scope === "users" ? (
        <NotificationField label={`用户 ID（已识别 ${formatCount(userIdCount)} 个，最多 ${formatCount(limits.maxUserIds)} 个）`}>
          <Textarea
            rows={5}
            value={draft.userIdsText}
            placeholder="用逗号或换行分隔"
            className="font-mono text-xs"
            onChange={(event) => patch({ userIdsText: event.target.value })}
          />
        </NotificationField>
      ) : null}

      {draft.scope === "filter" ? (
        <div className="space-y-3 rounded-xl border bg-muted/20 p-3">
          <div className="grid gap-3 sm:grid-cols-2">
            <NotificationField label="会员身份">
              <Select
                value={draft.membership || ANY}
                onValueChange={(value) => patch({ membership: (value === ANY ? "" : value) as NotificationAudienceMembership })}
              >
                <SelectTrigger size="sm" className="h-8 w-full text-xs">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {(catalog?.memberships ?? [{ value: "", label: "不限" }]).map((item) => (
                    <SelectItem key={item.value || ANY} value={item.value || ANY} className="text-xs">
                      {item.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </NotificationField>
            <NotificationField label="关键词">
              <Input
                value={draft.keyword}
                maxLength={64}
                className="h-8 text-xs"
                placeholder="账号、昵称、邮箱、手机号或用户 ID"
                onChange={(event) => patch({ keyword: event.target.value })}
              />
            </NotificationField>
            <NotificationField label="注册不超过（天）">
              <NumberInput value={draft.registeredWithinDays} max={limits.maxDays} placeholder="不限" onChange={(value) => patch({ registeredWithinDays: value })} />
            </NotificationField>
            <NotificationField label="注册已满（天）">
              <NumberInput value={draft.registeredBeforeDays} max={limits.maxDays} placeholder="不限" onChange={(value) => patch({ registeredBeforeDays: value })} />
            </NotificationField>
            <NotificationField label="活跃情况">
              <div className="flex gap-2">
                <Select value={draft.activity} onValueChange={(value) => patch({ activity: value as ActivityMode })}>
                  <SelectTrigger size="sm" className="h-8 w-32 shrink-0 text-xs">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {ACTIVITY_OPTIONS.map((item) => (
                      <SelectItem key={item.value} value={item.value} className="text-xs">
                        {item.label}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <NumberInput
                  value={draft.activity === "any" ? "" : draft.activityDays}
                  max={limits.maxDays}
                  disabled={draft.activity === "any"}
                  placeholder={draft.activity === "inactive" ? "超过 N 天" : "最近 N 天"}
                  onChange={(value) => patch({ activityDays: value })}
                />
              </div>
            </NotificationField>
            <NotificationField label="等级范围">
              <div className="flex items-center gap-2">
                <NumberInput value={draft.minLevel} max={limits.maxLevel} placeholder="最低" onChange={(value) => patch({ minLevel: value })} />
                <span className="text-xs text-muted-foreground">至</span>
                <NumberInput value={draft.maxLevel} max={limits.maxLevel} placeholder="最高" onChange={(value) => patch({ maxLevel: value })} />
              </div>
            </NotificationField>
          </div>
          <NotificationField label="登录过的平台">
            <Pills
              multiple
              options={(catalog?.platforms ?? []).map((item) => ({ key: item.value, label: item.label }))}
              selected={draft.platforms}
              onToggle={(key) =>
                patch({
                  platforms: draft.platforms.includes(key) ? draft.platforms.filter((item) => item !== key) : [...draft.platforms, key]
                })
              }
            />
          </NotificationField>
        </div>
      ) : null}

      <div className="grid gap-3 sm:grid-cols-2">
        <NotificationField label="账号状态">
          <Select value={draft.account} onValueChange={(value) => patch({ account: value as NotificationAudienceAccount })}>
            <SelectTrigger size="sm" className="h-8 w-full text-xs">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {(catalog?.accounts ?? [{ value: "available", label: "可用账号" }]).map((item) => (
                <SelectItem key={item.value} value={item.value} className="text-xs">
                  {item.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </NotificationField>
        <NotificationField label="人数上限">
          <NumberInput value={draft.limit} placeholder="不限" onChange={(value) => patch({ limit: value })} />
        </NotificationField>
      </div>
      <NotificationField label={`排除用户 ID（${formatCount(excludeCount)} 个）`}>
        <Textarea
          rows={2}
          value={draft.excludeText}
          placeholder="用逗号或换行分隔"
          className="font-mono text-xs"
          onChange={(event) => patch({ excludeText: event.target.value })}
        />
      </NotificationField>
    </div>
  );
}

/** 实时预览：人数、人群摘要与抽样 */
export function AudiencePreviewCard({
  preview,
  loading,
  fetching,
  problem,
  error
}: {
  preview?: NotificationAudiencePreview;
  loading: boolean;
  fetching: boolean;
  problem: string | null;
  error: string | null;
}) {
  return (
    <div className="space-y-3 rounded-xl border bg-card p-4">
      <div className="flex items-center justify-between gap-2">
        <span className="text-xs text-muted-foreground">预计收件人</span>
        {fetching ? <Loader2 className="size-3.5 animate-spin text-muted-foreground" /> : null}
      </div>
      {problem ? (
        <p className="text-xs text-destructive">{problem}</p>
      ) : error ? (
        <p className="text-xs text-destructive">{error}</p>
      ) : loading || !preview ? (
        <Skeleton className="h-8 w-28" />
      ) : (
        <>
          <div className={cn("text-3xl font-semibold tracking-tight tabular-nums transition-opacity", fetching && "opacity-60")}>
            {formatCount(preview.total)}
            <span className="ml-1 text-sm font-normal text-muted-foreground">人</span>
          </div>
          <p className="text-xs leading-5 text-muted-foreground">{preview.label}</p>
        </>
      )}

      {!problem && !error && preview?.samples?.length ? (
        <div className="space-y-1 border-t pt-3">
          <div className="text-[11px] text-muted-foreground">抽样</div>
          <ul className="divide-y">
            {preview.samples.map((sample) => (
              <li key={sample.userId} className="flex items-center justify-between gap-2 py-1.5">
                <div className="min-w-0">
                  <div className="truncate text-xs font-medium">{sample.nickname || sample.account || `#${sample.userId}`}</div>
                  <div className="truncate text-[11px] text-muted-foreground tabular-nums">
                    #{sample.userId}
                    {sample.account ? ` ${sample.account}` : ""}
                  </div>
                </div>
                <div className="flex shrink-0 items-center gap-1.5 text-[11px] text-muted-foreground tabular-nums">
                  {sample.member ? (
                    <Badge variant="warning" size="sm" className="gap-0.5">
                      <Crown className="size-2.5" />
                      会员
                    </Badge>
                  ) : null}
                  <span>Lv.{sample.level}</span>
                  <span title="最近活跃">{sample.lastActiveAt ? formatRelative(sample.lastActiveAt) : "未活跃"}</span>
                </div>
              </li>
            ))}
          </ul>
        </div>
      ) : null}
    </div>
  );
}
