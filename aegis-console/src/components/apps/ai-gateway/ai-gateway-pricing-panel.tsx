"use client";

import { useMemo, useState } from "react";
import { Calculator, Clock3, Crown, Loader2, RotateCcw, Save } from "lucide-react";
import { toast } from "sonner";
import {
  DECIMAL_RE,
  formatFactor,
  parseClock,
  resolveWindow,
  trimDecimal,
  useNow,
  zonedNow
} from "@/components/apps/ai-gateway/ai-gateway-billing-kit";
import {
  MemberTiersSection,
  type GeneralVipDraft,
  type MemberTierDraft,
  type TierOption
} from "@/components/apps/ai-gateway/ai-gateway-member-tiers";
import { PriceSimulator } from "@/components/apps/ai-gateway/ai-gateway-price-simulator";
import { TimePricingSection } from "@/components/apps/ai-gateway/ai-gateway-time-pricing";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import {
  useAIGatewayCatalogQuery,
  useAIGatewayModelsQuery,
  useAIGatewaySettingsQuery,
  useSaveAIGatewaySettingsMutation
} from "@/lib/ai-gateway-hooks";
import { ApiError } from "@/lib/api/client";
import type { AIGatewaySettings, AIGatewayTimePricing } from "@/lib/api/ai-gateway";
import { formatDateTime } from "@/lib/datetime";
import { useAdminVipTiersQuery } from "@/lib/vip-hooks";
import { cn } from "@/lib/utils";

/**
 * 计费方案（docs/ai-billing.md「价格怎么算」）：时段价格、会员档位优惠与价格模拟放在一个视图里，
 * 共用一份草稿 —— 模拟器按草稿算，改完时段立刻能看到某个档位的会员此刻付多少。
 *
 * 设置接口是整份 PUT：保存时以服务端当前设置为底，只换掉这里管的字段（时段、档位、通用会员），
 * 「设置」视图里的其他字段原样带回去。
 */

const TABS = [
  { key: "time", label: "时段价格", icon: Clock3 },
  { key: "member", label: "会员优惠", icon: Crown },
  { key: "simulate", label: "价格模拟", icon: Calculator }
] as const;

type TabKey = (typeof TABS)[number]["key"];

type PricingDraft = {
  scope: string;
  timePricing: AIGatewayTimePricing;
  tiers: MemberTierDraft[];
  general: GeneralVipDraft;
};

function seedDraft(scope: string, settings?: AIGatewaySettings): PricingDraft {
  const time = settings?.timePricing;
  return {
    scope,
    timePricing: {
      enabled: time?.enabled ?? false,
      stacking: time?.stacking ?? "multiply",
      windows: (time?.windows ?? []).map((window, index) => ({
        ...window,
        id: window.id || `auto-${index + 1}`,
        weekdays: window.weekdays ?? [],
        multiplier: trimDecimal(window.multiplier, "1"),
        models: window.models ?? [],
        kinds: window.kinds ?? [],
        from: window.from ?? null,
        to: window.to ?? null
      }))
    },
    tiers: (settings?.memberTiers ?? []).map((tier, index) => ({
      uid: `tier-${tier.tierId}-${index}`,
      tierId: tier.tierId,
      label: tier.label ?? "",
      multiplier: trimDecimal(tier.multiplier, "1"),
      daily: trimDecimal(tier.daily),
      monthly: trimDecimal(tier.monthly),
      packDiscount: trimDecimal(tier.packDiscount, "1"),
      packBonusRate: trimDecimal(tier.packBonusRate),
      rpm: String(tier.rpm ?? 0),
      maxConcurrent: String(tier.maxConcurrent ?? 0)
    })),
    general: {
      vipMultiplier: trimDecimal(settings?.vipMultiplier, "1"),
      vipDaily: trimDecimal(settings?.vipDaily),
      vipMonthly: trimDecimal(settings?.vipMonthly),
      vipPackDiscount: trimDecimal(settings?.vipPackDiscount, "1"),
      vipPackBonusRate: trimDecimal(settings?.vipPackBonusRate)
    }
  };
}

function serialize(draft: PricingDraft) {
  return JSON.stringify({
    timePricing: draft.timePricing,
    tiers: draft.tiers.map(({ uid: _uid, ...rest }) => rest),
    general: draft.general
  });
}

/** 草稿 → 整份设置（以服务端设置为底）。 */
function buildSettings(settings: AIGatewaySettings, draft: PricingDraft): AIGatewaySettings {
  const { updatedAt: _updatedAt, updatedBy: _updatedBy, ...rest } = settings;
  return {
    ...rest,
    timePricing: {
      enabled: draft.timePricing.enabled,
      stacking: draft.timePricing.stacking,
      windows: draft.timePricing.windows.map((window) => ({
        ...window,
        name: window.name.trim(),
        multiplier: window.multiplier.trim(),
        from: window.from || null,
        to: window.to || null
      }))
    },
    memberTiers: draft.tiers.map((tier) => ({
      tierId: tier.tierId,
      label: tier.label.trim(),
      multiplier: tier.multiplier.trim() || "1",
      daily: tier.daily.trim() || "0",
      monthly: tier.monthly.trim() || "0",
      packDiscount: tier.packDiscount.trim() || "1",
      packBonusRate: tier.packBonusRate.trim() || "0",
      rpm: Math.max(0, Math.floor(Number(tier.rpm) || 0)),
      maxConcurrent: Math.max(0, Math.floor(Number(tier.maxConcurrent) || 0))
    })),
    vipMultiplier: draft.general.vipMultiplier.trim() || "1",
    vipDaily: draft.general.vipDaily.trim() || "0",
    vipMonthly: draft.general.vipMonthly.trim() || "0",
    vipPackDiscount: draft.general.vipPackDiscount.trim() || "1",
    vipPackBonusRate: draft.general.vipPackBonusRate.trim() || "0"
  };
}

function validate(draft: PricingDraft, tierName: (id: number) => string): string | null {
  const windows = draft.timePricing.windows;
  if (windows.length > 32) return "最多 32 个时段";
  for (const [index, window] of windows.entries()) {
    const label = `时段 #${index + 1}「${window.name || "未命名"}」`;
    if (!window.name.trim()) return `时段 #${index + 1} 需要名称`;
    if (parseClock(window.start) === null || parseClock(window.end) === null) return `${label}的时间需为 HH:MM`;
    const multiplier = Number(window.multiplier);
    if (!DECIMAL_RE.test(window.multiplier.trim()) || multiplier < 0.01 || multiplier > 100) return `${label}的倍率需在 0.01–100 之间`;
    if (window.from && window.to && window.from > window.to) return `${label}的活动开始日期晚于结束日期`;
  }
  const seen = new Set<number>();
  for (const tier of draft.tiers) {
    const label = `档位「${tier.label || tierName(tier.tierId)}」`;
    if (seen.has(tier.tierId)) return `${label}重复配置`;
    seen.add(tier.tierId);
    for (const [field, value] of [["调用倍率", tier.multiplier], ["每日额度", tier.daily], ["每月额度", tier.monthly], ["额度包多送", tier.packBonusRate]] as const) {
      if (!DECIMAL_RE.test(value.trim() || "0")) return `${label}的${field}需为非负数，最多 6 位小数`;
    }
    if (!(Number(tier.multiplier) > 0)) return `${label}的调用倍率需大于 0`;
    const discount = Number(tier.packDiscount);
    if (!DECIMAL_RE.test(tier.packDiscount.trim()) || discount <= 0 || discount > 1) return `${label}的额度包折扣需在 0–1 之间（0.85 = 85 折）`;
    for (const [field, value] of [["每分钟请求", tier.rpm], ["同时进行", tier.maxConcurrent]] as const) {
      if (!/^\d+$/.test(value.trim() || "0")) return `${label}的${field}需为非负整数`;
    }
  }
  const general = draft.general;
  for (const [field, value] of [["会员倍率", general.vipMultiplier], ["会员每日额度", general.vipDaily], ["会员每月额度", general.vipMonthly], ["会员额度包多送", general.vipPackBonusRate]] as const) {
    if (!DECIMAL_RE.test(value.trim() || "0")) return `通用会员的${field}需为非负数，最多 6 位小数`;
  }
  const discount = Number(general.vipPackDiscount);
  if (!DECIMAL_RE.test(general.vipPackDiscount.trim()) || discount <= 0 || discount > 1) return "通用会员的额度包折扣需在 0–1 之间";
  return null;
}

export function AIGatewayPricingPanel({ appKey }: { appKey: string }) {
  const catalogQuery = useAIGatewayCatalogQuery(appKey);
  const settingsQuery = useAIGatewaySettingsQuery(appKey);
  const modelsQuery = useAIGatewayModelsQuery(appKey);
  const catalog = catalogQuery.data;
  // 目录没带档位（后端还没上 vipTiers）时退回会员档位接口
  const vipTiersQuery = useAdminVipTiersQuery(catalog && !catalog.vipTiers ? appKey : null);
  const saveMutation = useSaveAIGatewaySettingsMutation(appKey);
  const [tab, setTab] = useState<TabKey>("time");
  const [draft, setDraft] = useState<PricingDraft | null>(null);
  const now = useNow(60_000);

  const settings = settingsQuery.data;
  const seed = useMemo(() => seedDraft(appKey, settings), [appKey, settings]);
  const current = draft?.scope === appKey ? draft : seed;
  const dirty = draft?.scope === appKey && serialize(draft) !== serialize(seed);
  const patch = (changes: Partial<PricingDraft>) => setDraft({ ...current, ...changes, scope: appKey });

  const tiers = useMemo<TierOption[]>(
    () => catalog?.vipTiers ?? (vipTiersQuery.data ?? []).map((tier) => ({ id: tier.id, name: tier.name })),
    [catalog?.vipTiers, vipTiersQuery.data]
  );
  const tierName = (id: number) => tiers.find((tier) => tier.id === id)?.name ?? `档位 #${id}`;
  const models = useMemo(() => modelsQuery.data?.items ?? [], [modelsQuery.data]);
  const kinds = catalog?.kinds ?? [];
  const timezone = settings?.timezone || "Asia/Shanghai";
  const draftSettings = useMemo(() => (settings ? buildSettings(settings, current) : null), [settings, current]);

  const zoned = zonedNow(new Date(now), timezone);
  const nowWindow = current.timePricing.enabled ? resolveWindow(current.timePricing.windows, zoned.day, zoned.minute) : null;

  const save = async () => {
    if (!settings) return;
    const error = validate(current, tierName);
    if (error) {
      toast.error(error);
      return;
    }
    try {
      await saveMutation.mutateAsync(buildSettings(settings, current));
      toast.success("计费方案已保存");
      setDraft(null);
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "保存失败");
    }
  };

  if (settingsQuery.isLoading || catalogQuery.isLoading) {
    return (
      <div className="space-y-4">
        <Skeleton className="h-12 w-full rounded-xl" />
        <Skeleton className="h-80 w-full rounded-2xl" />
      </div>
    );
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-3 rounded-2xl border border-border bg-card px-4 py-3">
        <div className="flex gap-1 rounded-xl bg-muted/60 p-1">
          {TABS.map((item) => (
            <button
              key={item.key}
              type="button"
              onClick={() => setTab(item.key)}
              className={cn(
                "flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-xs font-medium transition-colors",
                tab === item.key ? "bg-background text-foreground shadow-sm" : "text-muted-foreground hover:text-foreground"
              )}
            >
              <item.icon className="size-3.5" />
              {item.label}
            </button>
          ))}
        </div>
        <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-[11px] text-muted-foreground">
          <span>
            此刻
            <span className="ml-1 font-mono font-semibold text-foreground">{formatFactor(nowWindow?.multiplier ?? "1")}</span>
            <span className="ml-1">{nowWindow?.name ?? "标准价"}</span>
          </span>
          <span>{current.timePricing.windows.length} 个时段{current.timePricing.enabled ? "" : "（未开启）"}</span>
          <span>{current.tiers.length} 个档位优惠</span>
        </div>
        <div className="ml-auto flex items-center gap-2">
          <span className="hidden text-[11px] text-muted-foreground md:inline">
            {settings?.updatedAt ? `上次保存 ${formatDateTime(settings.updatedAt)}` : "尚未保存"}
            {settings?.updatedBy ? ` · ${settings.updatedBy}` : ""}
          </span>
          <Button size="sm" variant="ghost" disabled={!dirty} onClick={() => setDraft(null)}>
            <RotateCcw className="size-3.5" />
            还原
          </Button>
          <Button size="sm" onClick={() => void save()} disabled={!dirty || saveMutation.isPending}>
            {saveMutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : <Save className="size-3.5" />}
            保存
          </Button>
        </div>
      </div>

      {tab === "time" ? (
        <TimePricingSection
          value={current.timePricing}
          onChange={(timePricing) => patch({ timePricing })}
          timezone={timezone}
          models={models}
          kinds={kinds}
        />
      ) : null}
      {tab === "member" ? (
        <MemberTiersSection
          tiers={tiers}
          value={current.tiers}
          onChange={(next) => patch({ tiers: next })}
          general={current.general}
          onGeneralChange={(general) => patch({ general })}
          appRpm={settings?.rpm ?? 0}
          appConcurrent={settings?.maxConcurrent ?? 0}
        />
      ) : null}
      {tab === "simulate" && draftSettings ? (
        <PriceSimulator appKey={appKey} settings={draftSettings} models={models} kinds={kinds} tiers={tiers} />
      ) : null}

      {dirty ? (
        <div className="sticky bottom-4 z-20 mx-auto flex w-fit items-center gap-3 rounded-full border border-border bg-background/95 py-1.5 pl-4 pr-1.5 text-xs shadow-lg backdrop-blur">
          <span className="size-2 rounded-full bg-amber-500" />
          计费方案有未保存的改动
          <Button size="xs" variant="ghost" onClick={() => setDraft(null)}>
            还原
          </Button>
          <Button size="xs" onClick={() => void save()} disabled={saveMutation.isPending}>
            {saveMutation.isPending ? <Loader2 className="size-3 animate-spin" /> : <Save className="size-3" />}
            保存
          </Button>
        </div>
      ) : null}
    </div>
  );
}
