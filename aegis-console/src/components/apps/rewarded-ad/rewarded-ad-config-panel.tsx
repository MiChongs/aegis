"use client";

import { useMemo, useState } from "react";
import {
  AlertTriangle,
  CheckCircle2,
  CircleAlert,
  CirclePause,
  Clock3,
  Loader2,
  Plug,
  Save,
  ShieldCheck,
  Undo2
} from "lucide-react";
import { toast } from "sonner";
import {
  FieldGroup,
  ModeCard,
  NumberField,
  SectionCard,
  StatusDot,
  SwitchRow
} from "@/components/apps/app-config-primitives";
import {
  buildPayload,
  collectIssues,
  draftHasDefaultKey,
  draftKeyState,
  seedDraft,
  type Draft,
  type DraftKeyState,
  type Issue
} from "@/components/apps/rewarded-ad/rewarded-ad-config-draft";
import { PlacementsSection } from "@/components/apps/rewarded-ad/rewarded-ad-placements-section";
import { ScenesSection } from "@/components/apps/rewarded-ad/rewarded-ad-scenes-section";
import { VERIFY_MODE_META } from "@/components/apps/rewarded-ad/rewarded-ad-shared";
import { CopyButton } from "@/components/profile/profile-shared";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { RadioGroup } from "@/components/ui/radio-group";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { ApiError } from "@/lib/api/client";
import type { RewardedAdConfig, RewardedAdVerifyMode } from "@/lib/api/rewarded-ad";
import { formatDateTimeMinute } from "@/lib/datetime";
import { useRewardedAdConfigQuery, useSaveRewardedAdConfigMutation } from "@/lib/rewarded-ad-hooks";
import { cn } from "@/lib/utils";
import { useAdminVipFeaturesQuery, useAdminVipPlansQuery } from "@/lib/vip-hooks";

/**
 * Apex 回调地址要自带参数宏（见 doc.apexadx.com/server/reward-callback），平台看完广告后替换成实际值再 GET。
 * 参数名与后端回调处理器认的名字一致；placementId 决定用哪个广告位的 Key 验签，不能省。
 */
const CALLBACK_PARAMS: [string, string][] = [
  ["userId", "USER_ID"],
  ["transId", "TRANS_ID"],
  ["sign", "SIGN"],
  ["extrainfo", "EXTRAINFO"],
  ["placementId", "PLACEMENT_ID"]
];
const CALLBACK_QUERY = `?${CALLBACK_PARAMS.map(([name, macro]) => `${name}={{${macro}}}`).join("&")}`;

const PROVIDER_LABELS: Record<string, string> = { apex: "Apex" };

/**
 * 激励广告配置：上线检查 → 接入 → 广告位与 Key → 校验与限额 → 奖励场景，底部常驻保存栏。
 *
 * 草稿逻辑（播种、载荷、检查、粘贴解析）在 rewarded-ad-config-draft.ts，
 * 广告位与场景两大块各自一个文件；这里只负责串起来与保存。
 */
export function RewardedAdConfigPanel({ appKey }: { appKey: string }) {
  const configQuery = useRewardedAdConfigQuery(appKey);
  const plansQuery = useAdminVipPlansQuery(appKey);
  const featuresQuery = useAdminVipFeaturesQuery(appKey);
  const saveMutation = useSaveRewardedAdConfigMutation(appKey);

  const [draft, setDraft] = useState<Draft | null>(null);
  const config = configQuery.data;
  const seed = useMemo(() => seedDraft(appKey, config), [appKey, config]);
  const current = draft?.scope === appKey ? draft : seed;
  const patch = (changes: Partial<Draft>) => setDraft({ ...current, ...changes, scope: appKey });

  const catalog = useMemo(() => config?.catalog ?? [], [config]);
  // 永久套餐不能作为激励广告奖励（后端保存时拒绝），不列出来免得选了再报错
  const plans = (plansQuery.data ?? []).filter((plan) => !plan.lifetime);
  const vipFeatures = featuresQuery.data ?? [];
  const savedPlacements = config?.placements ?? [];
  const activityDays = config?.activityDays ?? 7;

  const issues = useMemo(() => collectIssues(current, config, catalog), [current, config, catalog]);
  const errors = issues.filter((issue) => issue.level === "error");
  const dirty = useMemo(
    () => JSON.stringify(buildPayload(current, catalog)) !== JSON.stringify(buildPayload(seed, catalog)),
    [current, seed, catalog]
  );
  const hasDefault = draftHasDefaultKey(current, config);
  const keyStates: Record<string, DraftKeyState> = Object.fromEntries(
    current.placements.map((placement) => [
      placement.uid,
      draftKeyState(placement, savedPlacements.find((item) => item.id === placement.savedId), hasDefault)
    ])
  );

  const callbackUrl = config
    ? `${
        config.callbackAbsolute || typeof window === "undefined"
          ? config.callbackUrl
          : `${window.location.origin}${config.callbackUrl}`
      }${CALLBACK_QUERY}`
    : "";

  const save = async () => {
    if (errors.length > 0) {
      toast.error(errors[0].text, {
        description: errors.length > 1 ? `另有 ${errors.length - 1} 项待处理，详见上线检查` : undefined
      });
      return;
    }
    try {
      await saveMutation.mutateAsync(buildPayload(current, catalog));
      toast.success("激励广告配置已保存");
      setDraft(null);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "保存失败");
    }
  };

  if (configQuery.isLoading) {
    return (
      <div className="space-y-4">
        {[0, 1, 2, 3].map((index) => (
          <Skeleton key={index} className="h-36 w-full rounded-2xl" />
        ))}
      </div>
    );
  }

  return (
    <div className="space-y-4">
      <ReadinessCard draft={current} config={config} issues={issues} keyStates={keyStates} dirty={dirty} />

      <SectionCard
        icon={<Plug className="size-4" />}
        title="接入"
        description="Apex 平台信息与服务端回调地址"
        aside={<StatusDot active={Boolean(config?.enabled)} labelActive="已启用" labelInactive="未启用" />}
      >
        <div className="space-y-5">
          <SwitchRow
            label="启用激励广告"
            hint="关闭后客户端不再展示激励广告，平台回调一律返回无效"
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
                  {(config?.providers ?? ["apex"]).map((provider) => (
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
                placeholder="Apex 后台的应用 ID"
                onChange={(event) => patch({ providerAppId: event.target.value })}
              />
            </FieldGroup>
          </div>

          <FieldGroup label="服务端回调地址" hint="所有广告位填写同一地址">
            <div className="flex items-center gap-2 rounded-lg border border-border bg-muted/40 px-3 py-2">
              <code className="min-w-0 flex-1 truncate font-mono text-xs" title={callbackUrl}>
                {callbackUrl}
              </code>
              <CopyButton value={callbackUrl} label="复制回调地址" />
            </div>
            <p className="text-[11px] leading-relaxed text-muted-foreground">
              在 Apex 后台为每个激励广告位配置此地址。地址已包含全部参数宏，其中 placementId
              用于选择对应广告位的 Security Key，请勿删除。
            </p>
            <div className="flex flex-wrap items-center gap-1">
              {CALLBACK_PARAMS.map(([name]) => (
                <Badge key={name} variant="outline" size="sm" className="font-mono font-normal">
                  {name}
                </Badge>
              ))}
            </div>
          </FieldGroup>
        </div>
      </SectionCard>

      <PlacementsSection
        placements={current.placements}
        scenes={current.scenes}
        saved={savedPlacements}
        activityDays={activityDays}
        defaultKey={current.defaultKey}
        hasSavedDefaultKey={Boolean(config?.hasSecurityKey)}
        savedDefaultKeyHint={config?.securityKeyHint}
        clearDefaultKey={current.clearDefaultKey}
        issues={issues}
        onChange={(placements, rename) =>
          patch({
            placements,
            scenes: rename
              ? current.scenes.map((scene) =>
                  scene.placementId === rename.from ? { ...scene, placementId: rename.to } : scene
                )
              : current.scenes
          })
        }
        onDefaultKeyChange={(changes) => patch(changes)}
      />

      <SectionCard icon={<ShieldCheck className="size-4" />} title="校验与限额" description="一次观看在什么条件下发放奖励">
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
              hint="所有场景合计，0 表示不限"
              value={current.dailyLimit}
              onChange={(value) => patch({ dailyLimit: value })}
            />
          </div>
        </div>
      </SectionCard>

      <ScenesSection
        scenes={current.scenes}
        placements={current.placements}
        keyStates={keyStates}
        config={config}
        catalog={catalog}
        plans={plans}
        features={vipFeatures}
        issues={issues}
        onChange={(scenes) => patch({ scenes })}
      />

      <SaveBar
        dirty={dirty}
        errorCount={errors.length}
        saving={saveMutation.isPending}
        config={config}
        onDiscard={() => setDraft(null)}
        onSave={() => void save()}
      />
    </div>
  );
}

/**
 * 上线检查：一眼看出「现在能不能用、差什么」。
 * 状态按优先级取：有阻断项 → 未启用 → 启用了但还没收到过回调 → 正常。
 */
function ReadinessCard({
  draft,
  config,
  issues,
  keyStates,
  dirty
}: {
  draft: Draft;
  config?: RewardedAdConfig;
  issues: Issue[];
  keyStates: Record<string, DraftKeyState>;
  dirty: boolean;
}) {
  const errors = issues.filter((issue) => issue.level === "error");
  const warnings = issues.filter((issue) => issue.level === "warning");
  const days = config?.activityDays ?? 7;
  const callbacks = (config?.placements ?? []).reduce((sum, item) => sum + (item.callbacks || 0), 0);
  const enabledScenes = draft.scenes.filter((scene) => scene.enabled).length;
  const states = Object.values(keyStates);
  const ownKeys = states.filter((state) => state === "own" || state === "new").length;
  const missingKeys = states.filter((state) => state === "missing").length;

  const status =
    errors.length > 0
      ? {
          tone: "danger" as const,
          icon: <CircleAlert className="size-5" />,
          title: `${errors.length} 项需要处理`,
          detail: "处理完成后才能保存"
        }
      : !draft.enabled
        ? {
            tone: "muted" as const,
            icon: <CirclePause className="size-5" />,
            title: "激励广告未启用",
            detail: "配置完成后打开「启用激励广告」"
          }
        : callbacks === 0
          ? {
              tone: "warning" as const,
              icon: <Clock3 className="size-5" />,
              title: dirty ? "配置就绪，等待保存" : "配置就绪，等待首次回调",
              detail: `近 ${days} 天未收到验签通过的回调，请确认 Apex 后台已配置回调地址`
            }
          : {
              tone: "success" as const,
              icon: <CheckCircle2 className="size-5" />,
              title: "运行正常",
              detail: `近 ${days} 天收到 ${callbacks} 次验签通过的回调`
            };

  const toneClass = {
    danger: "bg-red-500/10 text-red-600 ring-red-500/20 dark:text-red-400",
    warning: "bg-amber-500/10 text-amber-600 ring-amber-500/20 dark:text-amber-400",
    success: "bg-emerald-500/10 text-emerald-600 ring-emerald-500/20 dark:text-emerald-400",
    muted: "bg-muted text-muted-foreground ring-border"
  }[status.tone];

  const metrics = [
    { label: "启用场景", value: `${enabledScenes}/${draft.scenes.length}` },
    {
      label: "广告位",
      value: String(draft.placements.length),
      note: missingKeys > 0 ? `${missingKeys} 个缺少 Key` : ownKeys > 0 ? `${ownKeys} 个独立 Key` : undefined,
      alert: missingKeys > 0
    },
    { label: `近 ${days} 天回调`, value: String(callbacks) },
    { label: "校验模式", value: VERIFY_MODE_META[draft.verifyMode]?.title ?? draft.verifyMode }
  ];

  return (
    <section
      className="overflow-hidden rounded-2xl border border-border bg-card"
      style={{ boxShadow: "var(--shadow-soft)" }}
    >
      <div className="flex flex-col gap-4 p-4 sm:flex-row sm:items-center sm:p-5">
        <div className="flex min-w-0 flex-1 items-center gap-3">
          <div className={cn("grid size-11 shrink-0 place-items-center rounded-2xl ring-1 ring-inset", toneClass)}>
            {status.icon}
          </div>
          <div className="min-w-0 space-y-0.5">
            <div className="text-[10px] font-semibold uppercase tracking-[0.14em] text-muted-foreground">上线检查</div>
            <div className="text-base font-semibold tracking-tight">{status.title}</div>
            <p className="text-xs text-muted-foreground">{status.detail}</p>
          </div>
        </div>
        <div className="grid grid-cols-2 gap-2 sm:grid-cols-4 sm:gap-3">
          {metrics.map((metric) => (
            <div key={metric.label} className="min-w-24 rounded-xl bg-muted/60 px-3 py-2">
              <div className="text-[10px] text-muted-foreground">{metric.label}</div>
              <div className="truncate text-sm font-semibold tabular-nums">{metric.value}</div>
              {metric.note ? (
                <div
                  className={cn(
                    "truncate text-[10px]",
                    metric.alert ? "text-red-600 dark:text-red-400" : "text-muted-foreground"
                  )}
                >
                  {metric.note}
                </div>
              ) : null}
            </div>
          ))}
        </div>
      </div>
      {errors.length > 0 || warnings.length > 0 ? (
        <ul className="space-y-1.5 border-t border-border bg-muted/30 px-4 py-3 sm:px-5">
          {[...errors, ...warnings].map((issue, index) => (
            <li key={`${issue.text}-${index}`} className="flex items-start gap-2 text-xs">
              {issue.level === "error" ? (
                <CircleAlert className="mt-0.5 size-3.5 shrink-0 text-red-500" />
              ) : (
                <AlertTriangle className="mt-0.5 size-3.5 shrink-0 text-amber-500" />
              )}
              <span className={issue.level === "error" ? "text-foreground" : "text-muted-foreground"}>{issue.text}</span>
            </li>
          ))}
        </ul>
      ) : null}
    </section>
  );
}

/** 底部常驻保存栏：有改动时提示并给出放弃 / 保存，没改动时显示上次保存。 */
function SaveBar({
  dirty,
  errorCount,
  saving,
  config,
  onDiscard,
  onSave
}: {
  dirty: boolean;
  errorCount: number;
  saving: boolean;
  config?: RewardedAdConfig;
  onDiscard: () => void;
  onSave: () => void;
}) {
  return (
    <div className="sticky bottom-3 z-20">
      <div
        className={cn(
          "flex items-center justify-between gap-3 rounded-2xl border bg-card/95 px-4 py-2.5 shadow-lg backdrop-blur transition-colors",
          dirty ? "border-amber-300 dark:border-amber-800" : "border-border"
        )}
      >
        <div className="flex min-w-0 items-center gap-2 text-xs">
          {dirty ? (
            <>
              <span className="size-2 shrink-0 rounded-full bg-amber-500" />
              <span className="font-medium">有未保存的修改</span>
              {errorCount > 0 ? (
                <span className="text-red-600 dark:text-red-400">{errorCount} 项待处理</span>
              ) : null}
            </>
          ) : (
            <span className="truncate text-muted-foreground">
              {config?.updatedAt
                ? `上次保存于 ${formatDateTimeMinute(config.updatedAt)}${config.updatedBy ? `，操作人 ${config.updatedBy}` : ""}`
                : "尚未保存"}
            </span>
          )}
        </div>
        <div className="flex shrink-0 items-center gap-2">
          {dirty ? (
            <Button size="sm" variant="ghost" onClick={onDiscard} disabled={saving}>
              <Undo2 className="size-3.5" />
              放弃修改
            </Button>
          ) : null}
          <Button size="sm" onClick={onSave} disabled={!dirty || saving}>
            {saving ? <Loader2 className="size-3.5 animate-spin" /> : <Save className="size-3.5" />}
            保存
          </Button>
        </div>
      </div>
    </div>
  );
}
