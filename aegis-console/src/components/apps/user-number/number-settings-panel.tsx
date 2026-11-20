"use client";

import { useMemo } from "react";
import { Bar, BarChart, XAxis, YAxis } from "recharts";
import { Clock, Dices, Hash, ListChecks, Loader2, Power, RotateCcw, Save, Sparkles, UserPen } from "lucide-react";
import { toast } from "sonner";
import { FieldGroup, NumberField, SectionCard, SliderField, SwitchRow } from "@/components/apps/app-config-primitives";
import { buildChartConfig } from "@/components/commerce/commerce-charts";
import { useDebouncedValue } from "@/components/users/detail/kit";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ChartContainer, ChartTooltip, ChartTooltipContent } from "@/components/ui/chart";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { ApiError } from "@/lib/api-client";
import { NUMBER_TIERS, type NumberCatalog, type NumberSettings, type NumberTierSpec } from "@/lib/api/user-number";
import { formatDateTimeMinute } from "@/lib/datetime";
import { useNumberOddsQuery, useSaveNumberSettingsMutation } from "@/lib/user-number-hooks";
import { cn } from "@/lib/utils";
import { NumberTierBadge, tierSpec } from "./number-shared";

const LUCK_SAMPLES = [0.5, 1, 2] as const;
const DISABLED = -1;

const toInt = (value: string) => {
  const parsed = Number.parseInt(value, 10);
  return Number.isFinite(parsed) ? parsed : 0;
};

const toFloat = (value: string) => {
  const parsed = Number.parseFloat(value);
  return Number.isFinite(parsed) ? parsed : 0;
};

/** 与 usernumber.Settings.Validate 同一套边界；只做保存前的提示，最终以后端为准 */
export function validateNumberSettings(s: NumberSettings, limits?: NumberCatalog["limits"]): string | null {
  const hardMin = limits?.hardMinLength ?? 5;
  const hardMax = limits?.hardMaxLength ?? 12;
  const minDefault = limits?.minDefaultLength ?? 8;
  if (s.minLength < hardMin || s.maxLength > hardMax || s.minLength > s.maxLength) return `位数范围须在 ${hardMin}–${hardMax} 之间且最短不大于最长`;
  if (s.defaultLength < minDefault || s.defaultLength > hardMax) return `默认位数须在 ${minDefault}–${hardMax} 之间`;
  if (s.cooldownDays < 0 || s.cooldownDays > 365) return "换号冷却须在 0–365 天之间";
  if (s.freezeDays < 0 || s.freezeDays > 3650) return "旧号冻结须在 0–3650 天之间";
  if (s.drawCandidates < 1 || s.drawCandidates > 12) return "候选数须在 1–12 之间";
  if (s.drawHoldMinutes < 1 || s.drawHoldMinutes > 120) return "锁定时间须在 1–120 分钟之间";
  if (s.drawWeights.some((w) => w < 0 || !Number.isFinite(w))) return "摇号权重不能为负";
  if (s.drawWeights.reduce((sum, w) => sum + w, 0) <= 0) return "摇号权重至少要有一级大于 0";
  if (s.luckSpread < 0 || s.luckSpread > 1) return "号运差异须在 0–1 之间";
  return null;
}

/**
 * 设置。草稿放在区块骨架那一层：切到别的视图再回来不丢改动。
 * 概率试算拿草稿（防抖后）请求 `/settings/odds`，与保存后用户端看到的公示同一个函数。
 */
export function NumberSettingsPanel({
  appKey,
  settings,
  saved,
  catalog,
  loading,
  dirty,
  canWrite,
  onChange,
  onReset,
  onSaved
}: {
  appKey: string;
  settings: NumberSettings;
  saved?: NumberSettings;
  catalog?: NumberCatalog;
  loading: boolean;
  dirty: boolean;
  canWrite: boolean;
  onChange: (next: NumberSettings) => void;
  onReset: () => void;
  onSaved: () => void;
}) {
  const save = useSaveNumberSettingsMutation(appKey);
  const tiers = catalog?.tiers?.length ? catalog.tiers : NUMBER_TIERS;
  const hardMin = catalog?.limits.hardMinLength ?? 5;
  const hardMax = catalog?.limits.hardMaxLength ?? 12;
  const minDefault = catalog?.limits.minDefaultLength ?? 8;
  const error = validateNumberSettings(settings, catalog?.limits);
  const patch = <K extends keyof NumberSettings>(key: K, value: NumberSettings[K]) => onChange({ ...settings, [key]: value });
  const readOnly = !canWrite;

  const lengths = (from: number, to: number) => Array.from({ length: Math.max(0, to - from + 1) }, (_, i) => from + i);

  const setWeight = (level: number, value: number) => {
    const next = [...settings.drawWeights];
    while (next.length < tiers.length) next.push(0);
    next[level] = value;
    patch("drawWeights", next);
  };

  const setPatternTier = (key: string, defaultTier: number, tier: number) => {
    const next = { ...(settings.patternTiers ?? {}) };
    if (tier === defaultTier) delete next[key];
    else next[key] = tier;
    patch("patternTiers", Object.keys(next).length ? next : null);
  };

  const submit = async () => {
    if (error) {
      toast.error(error);
      return;
    }
    try {
      await save.mutateAsync(settings);
      toast.success("已保存");
      onSaved();
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "保存失败");
    }
  };

  if (loading) {
    return (
      <div className="space-y-4">
        <Skeleton className="h-40 w-full rounded-2xl" />
        <Skeleton className="h-64 w-full rounded-2xl" />
      </div>
    );
  }

  return (
    <div className="space-y-4">
      <div className="grid gap-4 lg:grid-cols-2">
        <SectionCard icon={<Power className="size-4" />} title="开放">
          <div className="space-y-2">
            <SwitchRow
              label="允许用户换号"
              icon={<Hash className="size-3.5" />}
              checked={settings.enabled}
              onChange={(value) => patch("enabled", value)}
              disabled={readOnly}
            />
            <SwitchRow
              label="允许自选号码"
              icon={<UserPen className="size-3.5" />}
              checked={settings.selfPick}
              onChange={(value) => patch("selfPick", value)}
              disabled={readOnly}
            />
          </div>
        </SectionCard>

        <SectionCard icon={<Hash className="size-4" />} title="位数">
          <div className="grid grid-cols-3 gap-3">
            <LengthSelect label="最短" value={settings.minLength} options={lengths(hardMin, hardMax)} disabled={readOnly} onChange={(v) => patch("minLength", v)} />
            <LengthSelect label="最长" value={settings.maxLength} options={lengths(hardMin, hardMax)} disabled={readOnly} onChange={(v) => patch("maxLength", v)} />
            <LengthSelect label="新用户默认" value={settings.defaultLength} options={lengths(minDefault, hardMax)} disabled={readOnly} onChange={(v) => patch("defaultLength", v)} />
          </div>
        </SectionCard>
      </div>

      <SectionCard icon={<Clock className="size-4" />} title="换号规则">
        <div className="grid gap-3 sm:grid-cols-2">
          <NumberField
            label="换号冷却"
            unit="天"
            min={0}
            max={365}
            value={String(settings.cooldownDays)}
            onChange={(v) => patch("cooldownDays", toInt(v))}
            hint="0 不限"
            disabled={readOnly}
          />
          <NumberField
            label="旧号冻结"
            unit="天"
            min={0}
            max={3650}
            value={String(settings.freezeDays)}
            onChange={(v) => patch("freezeDays", toInt(v))}
            hint="0 立即放出"
            disabled={readOnly}
          />
        </div>
      </SectionCard>

      <SectionCard icon={<Dices className="size-4" />} title="摇号">
        <div className="space-y-5">
          <div className="grid gap-3 sm:grid-cols-2">
            <SliderField
              label="每次候选数"
              value={settings.drawCandidates}
              min={1}
              max={12}
              unit="个"
              disabled={readOnly}
              onChange={(v) => patch("drawCandidates", v)}
            />
            <SliderField
              label="候选锁定"
              value={settings.drawHoldMinutes}
              min={1}
              max={120}
              unit="分钟"
              disabled={readOnly}
              onChange={(v) => patch("drawHoldMinutes", v)}
            />
          </div>

          <FieldGroup label="各级权重">
            <div className="grid grid-cols-2 gap-2 sm:grid-cols-5">
              {tiers.map((tier) => (
                <div key={tier.key} className="space-y-1.5 rounded-xl bg-muted px-3 py-2.5">
                  <NumberTierBadge tier={tier.level} tiers={tiers} />
                  <Input
                    type="number"
                    inputMode="decimal"
                    min={0}
                    step={0.1}
                    value={String(settings.drawWeights[tier.level] ?? 0)}
                    disabled={readOnly}
                    onChange={(event) => setWeight(tier.level, toFloat(event.target.value))}
                    className="h-8 bg-background font-mono text-xs tabular-nums"
                  />
                </div>
              ))}
            </div>
          </FieldGroup>

          <div className="grid gap-3 sm:grid-cols-2">
            <SliderField
              label="号运差异 σ"
              value={settings.luckSpread}
              min={0}
              max={1}
              step={0.05}
              valueLabel={settings.luckSpread === 0 ? "关闭" : settings.luckSpread.toFixed(2)}
              disabled={readOnly}
              onChange={(v) => patch("luckSpread", Math.round(v * 100) / 100)}
            />
            <SwitchRow
              label="叠加抽奖至尊加成"
              icon={<Sparkles className="size-3.5" />}
              checked={settings.luckSupreme}
              onChange={(value) => patch("luckSupreme", value)}
              disabled={readOnly || settings.luckSpread === 0}
            />
          </div>

          <OddsPreview appKey={appKey} settings={settings} tiers={tiers} invalid={Boolean(error)} />
        </div>
      </SectionCard>

      <SectionCard
        icon={<ListChecks className="size-4" />}
        title="号码规律"
        aside={
          settings.patternTiers && Object.keys(settings.patternTiers).length ? (
            <Badge variant="outline">已调整 {Object.keys(settings.patternTiers).length} 条</Badge>
          ) : null
        }
      >
        <PatternTable
          catalog={catalog}
          tiers={tiers}
          overrides={settings.patternTiers ?? {}}
          disabled={readOnly}
          onChange={setPatternTier}
        />
      </SectionCard>

      <div className="sticky bottom-3 z-10 flex flex-wrap items-center justify-between gap-2 rounded-xl border border-border bg-card px-4 py-3">
        <span className={cn("text-xs", error ? "text-red-600 dark:text-red-400" : "text-muted-foreground")}>
          {error ??
            (saved?.updatedAt
              ? `${saved.updatedBy ? `${saved.updatedBy} ` : ""}保存于 ${formatDateTimeMinute(saved.updatedAt)}`
              : "使用默认设置")}
        </span>
        {canWrite ? (
          <div className="flex items-center gap-2">
            {dirty ? (
              <Button variant="outline" size="sm" className="h-8 gap-1.5" disabled={save.isPending} onClick={onReset}>
                <RotateCcw className="size-3.5" />
                放弃修改
              </Button>
            ) : null}
            <Button size="sm" className="h-8 gap-1.5" disabled={!dirty || save.isPending || Boolean(error)} onClick={submit}>
              {save.isPending ? <Loader2 className="size-3.5 animate-spin" /> : <Save className="size-3.5" />}
              保存
            </Button>
          </div>
        ) : null}
      </div>
    </div>
  );
}

function LengthSelect({
  label,
  value,
  options,
  disabled,
  onChange
}: {
  label: string;
  value: number;
  options: number[];
  disabled?: boolean;
  onChange: (value: number) => void;
}) {
  return (
    <div className="space-y-1.5">
      <Label className="text-[11px] font-medium">{label}</Label>
      <Select value={String(value)} onValueChange={(next) => onChange(Number(next))} disabled={disabled}>
        <SelectTrigger size="sm" className="h-8 w-full font-mono text-xs">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {options.map((option) => (
            <SelectItem key={option} value={String(option)} className="font-mono text-xs">
              {option} 位
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  );
}

/** 三档号运下各级出现的概率：堆叠条形图 + 数字表 */
function OddsPreview({
  appKey,
  settings,
  tiers,
  invalid
}: {
  appKey: string;
  settings: NumberSettings;
  tiers: NumberTierSpec[];
  invalid: boolean;
}) {
  const debounced = useDebouncedValue(settings, 400);
  const payload = invalid ? null : debounced;
  const low = useNumberOddsQuery(appKey, payload, LUCK_SAMPLES[0]);
  const mid = useNumberOddsQuery(appKey, payload, LUCK_SAMPLES[1]);
  const high = useNumberOddsQuery(appKey, payload, LUCK_SAMPLES[2]);
  const queries = [low, mid, high];
  const loading = queries.some((query) => query.isLoading);
  const fetching = queries.some((query) => query.isFetching);

  const config = useMemo(
    () => buildChartConfig(tiers.map((tier) => ({ key: tier.key, label: tier.name, color: { light: tier.color, dark: tier.color } }))),
    [tiers]
  );

  const rows = LUCK_SAMPLES.map((luck, index) => {
    const odds = queries[index].data?.odds ?? [];
    const row: Record<string, number | string> = { label: `号运 ${luck}` };
    tiers.forEach((tier) => {
      row[tier.key] = odds[tier.level] ?? 0;
    });
    return row;
  });

  return (
    <FieldGroup label="概率试算" hint={fetching && !loading ? "计算中" : undefined}>
      {invalid ? (
        <p className="rounded-xl bg-muted px-3 py-4 text-center text-xs text-muted-foreground">设置有误，暂不试算</p>
      ) : loading ? (
        <Skeleton className="h-[168px] w-full rounded-xl" />
      ) : (
        <div className={cn("grid gap-4 transition-opacity lg:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]", fetching && "opacity-60")}>
          <ChartContainer config={config} className="h-[150px] w-full">
            <BarChart data={rows} layout="vertical" margin={{ left: 0, right: 8, top: 4, bottom: 4 }} barCategoryGap={10}>
              <XAxis type="number" domain={[0, 100]} hide />
              <YAxis type="category" dataKey="label" tickLine={false} axisLine={false} fontSize={11} width={56} />
              <ChartTooltip content={<ChartTooltipContent formatter={(value, name) => `${config[String(name)]?.label ?? name} ${value}%`} />} />
              {tiers.map((tier, index) => (
                <Bar
                  key={tier.key}
                  dataKey={tier.key}
                  stackId="odds"
                  fill={`var(--color-${tier.key})`}
                  radius={index === tiers.length - 1 ? [0, 3, 3, 0] : index === 0 ? [3, 0, 0, 3] : 0}
                />
              ))}
            </BarChart>
          </ChartContainer>
          <table className="w-full text-xs">
            <thead>
              <tr className="text-muted-foreground">
                <th className="pb-1.5 text-left font-medium">等级</th>
                {LUCK_SAMPLES.map((luck) => (
                  <th key={luck} className="pb-1.5 text-right font-medium">
                    号运 {luck}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {tiers.map((tier) => (
                <tr key={tier.key} className="border-t">
                  <td className="py-1.5">
                    <span className="inline-flex items-center gap-1.5">
                      <span className="size-2 rounded-full" style={{ backgroundColor: tierSpec(tier.level, tiers).color }} aria-hidden />
                      {tier.name}
                    </span>
                  </td>
                  {LUCK_SAMPLES.map((luck, index) => (
                    <td key={luck} className="py-1.5 text-right font-mono tabular-nums">
                      {(queries[index].data?.odds?.[tier.level] ?? 0).toFixed(2)}%
                    </td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </FieldGroup>
  );
}

/** 规律表：每条规律落在哪一级（或停用），带示例号码 */
function PatternTable({
  catalog,
  tiers,
  overrides,
  disabled,
  onChange
}: {
  catalog?: NumberCatalog;
  tiers: NumberTierSpec[];
  overrides: Record<string, number>;
  disabled: boolean;
  onChange: (key: string, defaultTier: number, tier: number) => void;
}) {
  const patterns = catalog?.patterns ?? [];
  if (!catalog) return <Skeleton className="h-48 w-full rounded-xl" />;
  if (!patterns.length) return <p className="py-6 text-center text-xs text-muted-foreground">暂无规律</p>;
  return (
    <div className="-mx-4 overflow-x-auto sm:-mx-5">
      <table className="w-full min-w-[560px] text-xs">
        <thead>
          <tr className="border-b bg-muted/30 text-muted-foreground">
            <th className="h-8 px-4 text-left font-medium sm:pl-5">规律</th>
            <th className="h-8 px-3 text-left font-medium">示例</th>
            <th className="h-8 px-3 text-left font-medium">默认</th>
            <th className="h-8 w-40 px-4 text-left font-medium sm:pr-5">本应用</th>
          </tr>
        </thead>
        <tbody>
          {patterns.map((pattern) => {
            const value = overrides[pattern.key] ?? pattern.defaultTier;
            const changed = pattern.key in overrides;
            return (
              <tr key={pattern.key} className={cn("border-b last:border-b-0", value === DISABLED && "text-muted-foreground")}>
                <td className="px-4 py-2 sm:pl-5">
                  <div className="font-medium">{pattern.name}</div>
                  <div className="font-mono text-[10px] text-muted-foreground">{pattern.key}</div>
                </td>
                <td className="px-3 py-2 font-mono tabular-nums">{pattern.example}</td>
                <td className="px-3 py-2">
                  <NumberTierBadge tier={pattern.defaultTier} tiers={tiers} />
                </td>
                <td className="px-4 py-2 sm:pr-5">
                  <Select value={String(value)} onValueChange={(next) => onChange(pattern.key, pattern.defaultTier, Number(next))} disabled={disabled}>
                    <SelectTrigger size="sm" className={cn("h-7 w-full text-xs", changed && "ring-1 ring-amber-500/50")}>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {tiers.map((tier) => (
                        <SelectItem key={tier.key} value={String(tier.level)} className="text-xs">
                          <span className="inline-flex items-center gap-1.5">
                            <span className="size-2 rounded-full" style={{ backgroundColor: tier.color }} aria-hidden />
                            {tier.name}
                          </span>
                        </SelectItem>
                      ))}
                      <SelectItem value={String(DISABLED)} className="text-xs">
                        停用
                      </SelectItem>
                    </SelectContent>
                  </Select>
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}
