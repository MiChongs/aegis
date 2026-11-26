"use client";

import { useState } from "react";
import { ArrowDown, ArrowUp, ArrowUpRight, Gauge, Gift, Layers, Loader2, Percent, Plus, Power, Save, Store, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { FieldGroup, NumberField, SectionCard, StatusDot, SwitchRow } from "@/components/apps/app-config-primitives";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Textarea } from "@/components/ui/textarea";
import { useAIGatewayCatalogQuery, useAIGatewaySettingsQuery, useSaveAIGatewaySettingsMutation } from "@/lib/ai-gateway-hooks";
import { ApiError } from "@/lib/api/client";
import type { AIGatewaySettings, AIGatewaySourceKey } from "@/lib/api/ai-gateway";
import { formatDateTime } from "@/lib/datetime";

type LevelDraft = { uid: string; level: string; multiplier: string };

/**
 * 数字一律按字符串存（理由同云存储配置）；扣费来源存「全部来源的顺序 + 哪些启用」，取消勾选不丢位置。
 *
 * 会员倍率与会员额度、时段价格归「计费方案」视图管；这里保存时以服务端设置为底，那些字段原样带回去。
 * 积分（integral）已不是扣费来源（AI 额度与积分不互通），旧设置里残留的也不再显示、保存时去掉。
 */
type Draft = {
  scope: string;
  enabled: boolean;
  allowSessionToken: boolean;
  redactContent: boolean;
  catalogAutoSync: boolean;
  audience: "all" | "vip";
  order: AIGatewaySourceKey[];
  active: AIGatewaySourceKey[];
  priceMultiplier: string;
  levels: LevelDraft[];
  freeDaily: string;
  walletRate: string;
  welcomeEnabled: boolean;
  welcomeAmount: string;
  welcomeDays: string;
  storeEnabled: boolean;
  storeNotice: string;
  dailySpendLimit: string;
  rpm: string;
  maxConcurrent: string;
  maxKeysPerUser: string;
  defaultMaxTokens: string;
  timezone: string;
};

let levelSeed = 0;
const DECIMAL_RE = /^\d+(\.\d{1,6})?$/;
const LEGACY_SOURCES = new Set<AIGatewaySourceKey>(["integral"]);

function trim(value?: string) {
  if (!value) return "0";
  return value.includes(".") ? value.replace(/0+$/, "").replace(/\.$/, "") : value;
}

function seedDraft(scope: string, settings: AIGatewaySettings | undefined, allSources: AIGatewaySourceKey[]): Draft {
  const active = (settings?.sources ?? []).filter((source) => !LEGACY_SOURCES.has(source));
  const all = allSources.filter((source) => !LEGACY_SOURCES.has(source));
  return {
    scope,
    enabled: settings?.enabled ?? false,
    allowSessionToken: settings?.allowSessionToken ?? true,
    redactContent: settings?.redactContent ?? false,
    catalogAutoSync: settings?.catalogAutoSync ?? false,
    audience: settings?.audience ?? "all",
    order: [...active, ...all.filter((source) => !active.includes(source))],
    active,
    priceMultiplier: trim(settings?.priceMultiplier ?? "1"),
    levels: (settings?.levelMultipliers ?? []).map((item, index) => ({
      uid: `seed-${index}`,
      level: String(item.level),
      multiplier: trim(item.multiplier)
    })),
    freeDaily: trim(settings?.freeDaily),
    walletRate: trim(settings?.walletRate ?? "0.01"),
    welcomeEnabled: settings?.welcome?.enabled ?? false,
    welcomeAmount: trim(settings?.welcome?.amount),
    welcomeDays: String(settings?.welcome?.validityDays ?? 30),
    storeEnabled: settings?.store?.enabled ?? false,
    storeNotice: settings?.store?.notice ?? "",
    dailySpendLimit: trim(settings?.dailySpendLimit),
    rpm: String(settings?.rpm ?? 60),
    maxConcurrent: String(settings?.maxConcurrent ?? 5),
    maxKeysPerUser: String(settings?.maxKeysPerUser ?? 10),
    defaultMaxTokens: String(settings?.defaultMaxTokens ?? 4096),
    timezone: settings?.timezone ?? "Asia/Shanghai"
  };
}

export function AIGatewaySettingsPanel({
  appKey,
  onOpenPricing,
  onOpenStore
}: {
  appKey: string;
  onOpenPricing?: () => void;
  onOpenStore?: () => void;
}) {
  const catalogQuery = useAIGatewayCatalogQuery(appKey);
  const settingsQuery = useAIGatewaySettingsQuery(appKey);
  const saveMutation = useSaveAIGatewaySettingsMutation(appKey);
  const [draft, setDraft] = useState<Draft | null>(null);

  const sources = catalogQuery.data?.sources ?? [];
  const settings = settingsQuery.data;
  const current = draft?.scope === appKey ? draft : seedDraft(appKey, settings, sources.map((item) => item.key));
  const patch = (changes: Partial<Draft>) => setDraft({ ...current, ...changes, scope: appKey });
  const sourceMeta = (key: AIGatewaySourceKey) => sources.find((item) => item.key === key);

  const move = (index: number, delta: number) => {
    const order = [...current.order];
    const target = index + delta;
    if (target < 0 || target >= order.length) return;
    [order[index], order[target]] = [order[target], order[index]];
    patch({ order });
  };
  const toggleSource = (key: AIGatewaySourceKey, on: boolean) =>
    patch({ active: on ? [...current.active, key] : current.active.filter((item) => item !== key) });

  const save = async () => {
    const enabledSources = current.order.filter((key) => current.active.includes(key));
    if (enabledSources.length === 0) {
      toast.error("至少启用一个扣费来源");
      return;
    }
    const decimals: Array<[string, string]> = [
      ["全局倍率", current.priceMultiplier], ["每日免费额度", current.freeDaily],
      ["余额直付单价", current.walletRate], ["每日消费上限", current.dailySpendLimit], ["新人礼额度", current.welcomeAmount]
    ];
    for (const [label, value] of decimals) {
      if (!DECIMAL_RE.test(value.trim() || "0")) {
        toast.error(`${label}需为非负数，最多 6 位小数`);
        return;
      }
    }
    const levels = new Set<number>();
    for (const item of current.levels) {
      const level = Number(item.level);
      if (!Number.isInteger(level) || level < 1 || levels.has(level) || !DECIMAL_RE.test(item.multiplier.trim())) {
        toast.error(`等级倍率「Lv.${item.level || "?"}」无效或重复`);
        return;
      }
      levels.add(level);
    }
    if (current.welcomeEnabled && !(Number(current.welcomeAmount) > 0)) {
      toast.error("开启新人礼时额度需大于 0");
      return;
    }
    if (!/^\d+$/.test(current.welcomeDays.trim() || "0")) {
      toast.error("新人礼有效天数需为非负整数（0 为永久）");
      return;
    }
    if (current.storeNotice.length > 2000) {
      toast.error("购买须知最多 2000 字");
      return;
    }
    const { updatedAt: _updatedAt, updatedBy: _updatedBy, ...base } = settings ?? ({} as AIGatewaySettings);
    try {
      await saveMutation.mutateAsync({
        ...base,
        enabled: current.enabled,
        allowSessionToken: current.allowSessionToken,
        redactContent: current.redactContent,
        catalogAutoSync: current.catalogAutoSync,
        audience: current.audience,
        sources: enabledSources,
        priceMultiplier: current.priceMultiplier.trim(),
        vipMultiplier: base.vipMultiplier ?? "1",
        levelMultipliers: current.levels.map((item) => ({ level: Number(item.level), multiplier: item.multiplier.trim() })),
        freeDaily: current.freeDaily.trim() || "0",
        vipDaily: base.vipDaily ?? "0",
        vipMonthly: base.vipMonthly ?? "0",
        walletRate: current.walletRate.trim() || "0",
        welcome: {
          enabled: current.welcomeEnabled,
          amount: current.welcomeAmount.trim() || "0",
          validityDays: Number(current.welcomeDays) || 0
        },
        store: { enabled: current.storeEnabled, notice: current.storeNotice },
        dailySpendLimit: current.dailySpendLimit.trim() || "0",
        rpm: Number(current.rpm) || 0,
        maxConcurrent: Number(current.maxConcurrent) || 0,
        maxKeysPerUser: Number(current.maxKeysPerUser) || 0,
        defaultMaxTokens: Number(current.defaultMaxTokens) || 0,
        timezone: current.timezone.trim()
      });
      toast.success("设置已保存");
      setDraft(null);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "保存失败");
    }
  };

  if (settingsQuery.isLoading || catalogQuery.isLoading) {
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
        icon={<Power className="size-4" />}
        title="开关"
        aside={<StatusDot active={Boolean(settings?.enabled)} labelActive="已开启" labelInactive="未开启" />}
      >
        <div className="space-y-4">
          <SwitchRow label="开启 AI 中转" checked={current.enabled} onChange={(value) => patch({ enabled: value })} />
          <SwitchRow label="允许用登录令牌调用" hint="关闭后只接受用户的 API 密钥"
            checked={current.allowSessionToken} onChange={(value) => patch({ allowSessionToken: value })} />
          <SwitchRow label="调用记录不留内容" hint="开启后调用详情只记参数与长度，不保存提示词与回复"
            checked={current.redactContent} onChange={(value) => patch({ redactContent: value })} />
          <SwitchRow label="每天从模型目录更新" hint="按 models.dev 等公开目录更新对话模型的能力、上下文与名称说明，不改价格"
            checked={current.catalogAutoSync} onChange={(value) => patch({ catalogAutoSync: value })} />
          <FieldGroup label="使用对象">
            <Select value={current.audience} onValueChange={(value) => patch({ audience: value as "all" | "vip" })}>
              <SelectTrigger className="h-8 w-40 text-xs">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">所有用户</SelectItem>
                <SelectItem value="vip">仅会员</SelectItem>
              </SelectContent>
            </Select>
          </FieldGroup>
        </div>
      </SectionCard>

      <SectionCard icon={<Layers className="size-4" />} title="扣费来源与顺序">
        <div className="space-y-1.5">
          {current.order.map((key, index) => {
            const meta = sourceMeta(key);
            const on = current.active.includes(key);
            return (
              <div key={key} className="flex items-center gap-3 rounded-xl border border-border px-3 py-2">
                <Checkbox checked={on} onCheckedChange={(checked) => toggleSource(key, checked === true)} />
                <div className="min-w-0 flex-1">
                  <div className={on ? "text-xs font-medium" : "text-xs text-muted-foreground"}>{meta?.label ?? key}</div>
                  {meta?.hint ? <div className="text-[11px] text-muted-foreground">{meta.hint}</div> : null}
                </div>
                <Button size="icon" variant="ghost" className="size-7" aria-label="上移" disabled={index === 0}
                  onClick={() => move(index, -1)}>
                  <ArrowUp className="size-3.5" />
                </Button>
                <Button size="icon" variant="ghost" className="size-7" aria-label="下移"
                  disabled={index === current.order.length - 1} onClick={() => move(index, 1)}>
                  <ArrowDown className="size-3.5" />
                </Button>
              </div>
            );
          })}
        </div>
        <div className="mt-4 grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          <NumberField label="每日免费额度" unit="额度" min={0} value={current.freeDaily}
            onChange={(value) => patch({ freeDaily: value })} />
          <NumberField label="余额直付：每额度多少元" unit="元 / 额度" min={0} value={current.walletRate}
            hint={Number(current.walletRate) > 0 ? `1 元约合 ${Number((1 / Number(current.walletRate)).toFixed(2)).toLocaleString("zh-CN")} 额度` : undefined}
            disabled={!current.active.includes("wallet")} onChange={(value) => patch({ walletRate: value })} />
          <div className="flex flex-col justify-end gap-1.5 rounded-xl bg-muted/50 px-3 py-2.5 text-[11px] text-muted-foreground">
            会员每日 / 每月额度、会员倍率按档位配置
            {onOpenPricing ? (
              <Button size="xs" variant="outline" className="w-fit" onClick={onOpenPricing}>
                计费方案 · 会员优惠
                <ArrowUpRight className="size-3" />
              </Button>
            ) : null}
          </div>
        </div>
      </SectionCard>

      <SectionCard
        icon={<Percent className="size-4" />}
        title="价格倍率"
        aside={
          <Button size="sm" variant="outline"
            onClick={() => patch({ levels: [...current.levels, { uid: `level-${++levelSeed}`, level: "", multiplier: "1" }] })}>
            <Plus className="size-3.5" />
            等级倍率
          </Button>
        }
      >
        <div className="space-y-4">
          <div className="grid gap-4 sm:grid-cols-2">
            <NumberField label="全局倍率" min={0} value={current.priceMultiplier}
              onChange={(value) => patch({ priceMultiplier: value })} />
            <div className="flex items-end text-[11px] text-muted-foreground">
              合计倍率 = 全局 × 会员 × 等级 × 时段；会员与时段在「计费方案」里配置
            </div>
          </div>
          {current.levels.length > 0 ? (
            <div className="grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)_2rem] gap-2 text-[10px] text-muted-foreground">
              <span>等级不低于</span>
              <span>倍率</span>
            </div>
          ) : null}
          {current.levels.map((item) => (
            <div key={item.uid} className="grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)_2rem] items-center gap-2">
              <Input className="h-8 text-xs" inputMode="numeric" placeholder="等级 ≥" value={item.level}
                onChange={(event) => patch({ levels: current.levels.map((entry) => entry.uid === item.uid ? { ...entry, level: event.target.value } : entry) })} />
              <Input className="h-8 text-xs" inputMode="decimal" placeholder="倍率" value={item.multiplier}
                onChange={(event) => patch({ levels: current.levels.map((entry) => entry.uid === item.uid ? { ...entry, multiplier: event.target.value } : entry) })} />
              <Button size="icon" variant="ghost" className="size-8 text-muted-foreground hover:text-destructive" aria-label="删除"
                onClick={() => patch({ levels: current.levels.filter((entry) => entry.uid !== item.uid) })}>
                <Trash2 className="size-3.5" />
              </Button>
            </div>
          ))}
        </div>
      </SectionCard>

      <div className="grid gap-4 lg:grid-cols-2">
        <SectionCard
          icon={<Gift className="size-4" />}
          title="新人礼"
          description="每个用户可领一次，得到一个限期额度批次"
          aside={<StatusDot active={Boolean(settings?.welcome?.enabled)} labelActive="已开放" labelInactive="未开放" />}
        >
          <div className="space-y-4">
            <SwitchRow label="开放新人礼" checked={current.welcomeEnabled} onChange={(value) => patch({ welcomeEnabled: value })} />
            <div className="grid gap-4 sm:grid-cols-2">
              <NumberField label="额度" unit="额度" min={0} value={current.welcomeAmount} disabled={!current.welcomeEnabled}
                onChange={(value) => patch({ welcomeAmount: value })} />
              <NumberField label="有效天数" unit="天" min={0} hint="0 为永久" value={current.welcomeDays} disabled={!current.welcomeEnabled}
                onChange={(value) => patch({ welcomeDays: value })} />
            </div>
            <div className="flex flex-wrap gap-1.5">
              {[7, 30, 90, 0].map((days) => (
                <Button key={days} size="xs" variant={current.welcomeDays === String(days) ? "secondary" : "outline"}
                  disabled={!current.welcomeEnabled} onClick={() => patch({ welcomeDays: String(days) })}>
                  {days === 0 ? "永久" : `${days} 天`}
                </Button>
              ))}
            </div>
          </div>
        </SectionCard>

        <SectionCard
          icon={<Store className="size-4" />}
          title="额度商店"
          description="用户用钱包余额买额度包"
          aside={<StatusDot active={Boolean(settings?.store?.enabled)} labelActive="营业中" labelInactive="已关闭" />}
        >
          <div className="space-y-4">
            <SwitchRow label="开放额度商店" hint="关闭后用户看不到额度包，也不能购买"
              checked={current.storeEnabled} onChange={(value) => patch({ storeEnabled: value })} />
            <FieldGroup label="购买须知" hint={`${current.storeNotice.length} / 2000`}>
              <Textarea rows={4} maxLength={2000} className="text-xs"
                placeholder="纯文本，显示在商店页，例如：购买后即时到账；额度包到期后剩余额度清零，不可转让"
                value={current.storeNotice} onChange={(event) => patch({ storeNotice: event.target.value })} />
            </FieldGroup>
            {onOpenStore ? (
              <Button size="xs" variant="outline" onClick={onOpenStore}>
                管理额度包
                <ArrowUpRight className="size-3" />
              </Button>
            ) : null}
          </div>
        </SectionCard>
      </div>

      <SectionCard
        icon={<Gauge className="size-4" />}
        title="限制"
        footer={
          <div className="flex flex-wrap items-center justify-between gap-3">
            <span className="text-[11px] text-muted-foreground">
              {settings?.updatedAt ? `上次保存 ${formatDateTime(settings.updatedAt)}` : "尚未保存"}
              {settings?.updatedBy ? ` · ${settings.updatedBy}` : ""}
            </span>
            <Button size="sm" onClick={() => void save()} disabled={saveMutation.isPending}>
              {saveMutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : <Save className="size-3.5" />}
              保存
            </Button>
          </div>
        }
      >
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          <NumberField label="每日消费上限" unit="额度" min={0} hint="0 不限" value={current.dailySpendLimit}
            onChange={(value) => patch({ dailySpendLimit: value })} />
          <NumberField label="每分钟请求" unit="次" min={0} hint="0 不限" value={current.rpm}
            onChange={(value) => patch({ rpm: value })} />
          <NumberField label="同时进行" unit="个" min={1} value={current.maxConcurrent}
            onChange={(value) => patch({ maxConcurrent: value })} />
          <NumberField label="每人密钥" unit="把" min={0} hint="0 不允许创建" value={current.maxKeysPerUser}
            onChange={(value) => patch({ maxKeysPerUser: value })} />
          <NumberField label="默认输出估算" unit="token" min={1} value={current.defaultMaxTokens}
            onChange={(value) => patch({ defaultMaxTokens: value })} />
          <FieldGroup label="时区">
            <Input className="h-8 font-mono text-xs" value={current.timezone}
              onChange={(event) => patch({ timezone: event.target.value })} />
          </FieldGroup>
        </div>
      </SectionCard>
    </div>
  );
}
