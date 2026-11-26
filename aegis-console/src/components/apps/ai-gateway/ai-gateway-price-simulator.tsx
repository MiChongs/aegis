"use client";

import { useMemo, useState } from "react";
import { Calculator, CircleAlert, Clock3, Crown, Loader2, Timer } from "lucide-react";
import {
  FactorChain,
  WindowHitLine,
  addDays,
  discountLabel,
  formatFactor,
  formatRate,
  toneMeta,
  toneStrength
} from "@/components/apps/ai-gateway/ai-gateway-billing-kit";
import { PRICE_LABEL, formatPoints } from "@/components/apps/ai-gateway/ai-gateway-shared";
import type { TierOption } from "@/components/apps/ai-gateway/ai-gateway-member-tiers";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { useAIGatewayPricingPreviewQuery } from "@/lib/ai-gateway-hooks";
import { ApiError } from "@/lib/api/client";
import type {
  AIGatewayKindMeta,
  AIGatewayModel,
  AIGatewayPriceKey,
  AIGatewayPricingPreviewPayload,
  AIGatewaySettings,
  AIGatewayTimelineSegment
} from "@/lib/api/ai-gateway";
import { formatDateTimeMinute, fromLocalInputValue, parseInstant, toLocalInputValue } from "@/lib/datetime";
import { useDebouncedValue } from "@/lib/use-debounced-value";
import { cn } from "@/lib/utils";

const ALL_PRICES: AIGatewayPriceKey[] = ["inputPrice", "outputPrice", "requestPrice", "imagePrice", "charPrice", "minutePrice"];

/**
 * 价格模拟：挑一个模型、一种身份、一个时刻，按**草稿**设置（还没保存的改动也算）调 `POST /pricing/preview`，
 * 看倍率怎么一步步乘出来、这个模型最后多少钱。
 */
export function PriceSimulator({
  appKey,
  settings,
  models,
  kinds,
  tiers
}: {
  appKey: string;
  settings: AIGatewaySettings;
  models: AIGatewayModel[];
  kinds: AIGatewayKindMeta[];
  tiers: TierOption[];
}) {
  const [modelName, setModelName] = useState<string>("");
  const [identity, setIdentity] = useState<string>("none");
  const [level, setLevel] = useState("1");
  const [atMode, setAtMode] = useState<"now" | "custom">("now");
  const [at, setAt] = useState(() => toLocalInputValue(new Date()));

  const model = models.find((item) => item.model === modelName) ?? models.find((item) => item.enabled) ?? models[0];
  const payload = useMemo<AIGatewayPricingPreviewPayload>(() => {
    const tierId = identity.startsWith("tier:") ? Number(identity.slice(5)) : undefined;
    return {
      settings,
      model: model?.model,
      kind: model?.kind,
      tierId,
      isVip: identity !== "none",
      level: Math.max(0, Math.floor(Number(level) || 0)),
      at: atMode === "custom" ? fromLocalInputValue(at) : undefined
    };
  }, [settings, model, identity, level, atMode, at]);
  const debounced = useDebouncedValue(payload, 400);
  const query = useAIGatewayPricingPreviewQuery(appKey, debounced);
  const preview = query.data;
  const pending = query.isFetching || debounced !== payload;

  const kindMeta = kinds.find((item) => item.key === model?.kind);
  const priceKeys = (kindMeta?.prices ?? ALL_PRICES).filter((key) => model && (Number(model[key]) > 0 || kindMeta));
  const total = Number(preview?.breakdown.total ?? 1);

  return (
    <div className="grid gap-4 lg:grid-cols-[320px_minmax(0,1fr)]">
      <section className="h-fit space-y-4 rounded-2xl border border-border bg-card p-4" style={{ boxShadow: "var(--shadow-soft)" }}>
        <div className="flex items-center gap-2">
          <span className="grid size-8 place-items-center rounded-xl bg-sky-500/10 text-sky-600 ring-1 ring-inset ring-sky-500/20 dark:text-sky-400">
            <Calculator className="size-4" />
          </span>
          <div>
            <h3 className="text-sm font-semibold">模拟条件</h3>
            <p className="text-[11px] text-muted-foreground">按当前草稿计算，未保存的改动也算</p>
          </div>
        </div>
        <SimField label="模型">
          <Select value={model?.model ?? ""} onValueChange={setModelName}>
            <SelectTrigger className="h-8 w-full text-xs">
              <SelectValue placeholder="还没有模型" />
            </SelectTrigger>
            <SelectContent>
              {models.map((item) => (
                <SelectItem key={item.id} value={item.model}>
                  <span className="font-mono">{item.model}</span>
                  {!item.enabled ? <span className="text-muted-foreground">（未开放）</span> : null}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </SimField>
        <SimField label="身份">
          <Select value={identity} onValueChange={setIdentity}>
            <SelectTrigger className="h-8 w-full text-xs">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="none">非会员</SelectItem>
              <SelectItem value="vip">会员（不挂档位）</SelectItem>
              {tiers.map((tier) => (
                <SelectItem key={tier.id} value={`tier:${tier.id}`}>
                  {tier.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </SimField>
        <SimField label="等级">
          <Input className="h-8 font-mono text-xs" inputMode="numeric" value={level} onChange={(event) => setLevel(event.target.value)} />
        </SimField>
        <SimField label="时间">
          <div className="space-y-2">
            <ToggleGroup type="single" size="sm" value={atMode} onValueChange={(next) => next && setAtMode(next as "now" | "custom")}>
              <ToggleGroupItem value="now" className="h-7 px-3 text-xs">现在</ToggleGroupItem>
              <ToggleGroupItem value="custom" className="h-7 px-3 text-xs">指定时间</ToggleGroupItem>
            </ToggleGroup>
            {atMode === "custom" ? (
              <Input type="datetime-local" className="h-8 font-mono text-xs" value={at} onChange={(event) => setAt(event.target.value)} />
            ) : null}
            <p className="text-[10px] text-muted-foreground">输入的是浏览器时区的时间；时段按应用时区 {settings.timezone} 判断</p>
          </div>
        </SimField>
      </section>

      <div className="min-w-0 space-y-4">
        {query.isError ? (
          <div className="flex items-start gap-2 rounded-xl border border-destructive/30 bg-destructive/5 px-3 py-2 text-xs text-destructive">
            <CircleAlert className="mt-0.5 size-3.5 shrink-0" />
            {query.error instanceof ApiError ? query.error.message : "模拟失败"}
          </div>
        ) : null}
        {!preview ? (
          query.isError ? null : <Skeleton className="h-72 w-full rounded-2xl" />
        ) : (
          <>
            <section className="relative overflow-hidden rounded-2xl border border-border bg-card p-5" style={{ boxShadow: "var(--shadow-soft)" }}>
              {pending ? <Loader2 className="absolute right-4 top-4 size-4 animate-spin text-muted-foreground" /> : null}
              <div className="flex flex-wrap items-end gap-x-6 gap-y-3">
                <div>
                  <div className="text-[11px] text-muted-foreground">合计倍率</div>
                  <div className="flex items-baseline gap-2">
                    <span className="font-mono text-4xl font-bold tracking-tight tabular-nums">{formatFactor(preview.breakdown.total)}</span>
                    <span className={cn("text-sm font-semibold", total < 1 ? "text-emerald-600 dark:text-emerald-400" : total > 1 ? "text-rose-600 dark:text-rose-400" : "text-muted-foreground")}>
                      {discountLabel(total)}
                    </span>
                  </div>
                </div>
                <div className="space-y-1 text-xs">
                  <WindowHitLine breakdown={preview.breakdown} />
                  {preview.nextChangeAt ? (
                    <div className="flex items-center gap-1.5 text-muted-foreground">
                      <Timer className="size-3" />
                      下次变价 {formatDateTimeMinute(preview.nextChangeAt)}
                    </div>
                  ) : null}
                </div>
              </div>
              <FactorChain breakdown={preview.breakdown} className="mt-4" />
              <FactorSteps breakdown={preview.breakdown} />
            </section>

            <div className="grid gap-4 xl:grid-cols-2">
              <section className="rounded-2xl border border-border bg-card p-4" style={{ boxShadow: "var(--shadow-soft)" }}>
                <h4 className="mb-3 text-sm font-semibold">
                  <span className="font-mono">{model?.model}</span> 的单价
                </h4>
                {!model ? (
                  <p className="text-xs text-muted-foreground">还没有模型</p>
                ) : model.free ? (
                  <p className="text-xs text-muted-foreground">免费模型，不扣额度</p>
                ) : (
                  <div className="divide-y">
                    {priceKeys.map((key) => {
                      const base = Number(model[key]) || 0;
                      const final = preview.price ? Number(preview.price[key]) || 0 : base * total;
                      return (
                        <div key={key} className="grid grid-cols-[5rem_minmax(0,1fr)_auto] items-center gap-2 py-2 text-xs">
                          <span className="text-muted-foreground">{PRICE_LABEL[key].label}</span>
                          <span className="flex items-baseline gap-2 font-mono tabular-nums">
                            {final !== base ? <span className="text-muted-foreground line-through">{formatPoints(base)}</span> : null}
                            <span className="text-sm font-semibold">{formatPoints(final)}</span>
                          </span>
                          <span className="text-[10px] text-muted-foreground">{PRICE_LABEL[key].unit.replace("点", "额度")}</span>
                        </div>
                      );
                    })}
                    {!preview.price ? <p className="pt-2 text-[10px] text-muted-foreground">按模型原价 × 合计倍率估算</p> : null}
                  </div>
                )}
              </section>

              <section className="rounded-2xl border border-border bg-card p-4" style={{ boxShadow: "var(--shadow-soft)" }}>
                <h4 className="mb-3 flex items-center gap-1.5 text-sm font-semibold">
                  <Crown className="size-3.5 text-amber-500" />
                  {preview.member.name || (preview.member.source === "vip" ? "通用会员" : preview.member.source === "tier" ? "会员档位" : "非会员")}
                  <span className="text-[11px] font-normal text-muted-foreground">
                    {preview.member.source === "tier" ? "按档位配置" : preview.member.source === "vip" ? "按通用会员配置" : "无会员优惠"}
                  </span>
                </h4>
                <div className="grid grid-cols-2 gap-2 text-xs">
                  <Benefit label="调用价" value={discountLabel(preview.member.multiplier)} />
                  <Benefit label="额度包" value={`${discountLabel(preview.member.packDiscount)}${Number(preview.member.packBonusRate) > 0 ? ` · 多送 ${formatRate(preview.member.packBonusRate)}` : ""}`} />
                  <Benefit label="每日额度" value={Number(preview.member.daily) > 0 ? formatPoints(preview.member.daily) : "—"} />
                  <Benefit label="每月额度" value={Number(preview.member.monthly) > 0 ? formatPoints(preview.member.monthly) : "—"} />
                  <Benefit label="每分钟请求" value={preview.member.rpm ? `${preview.member.rpm}` : "沿用应用"} />
                  <Benefit label="同时进行" value={preview.member.maxConcurrent ? `${preview.member.maxConcurrent}` : "沿用应用"} />
                </div>
              </section>
            </div>

            {preview.timeline.length > 0 ? <TimelineStrip segments={preview.timeline} /> : null}
          </>
        )}
      </div>
    </div>
  );
}

function SimField({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="space-y-1.5">
      <Label className="text-[11px] font-medium text-muted-foreground">{label}</Label>
      {children}
    </div>
  );
}

function Benefit({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-lg bg-muted/50 px-2.5 py-1.5">
      <div className="text-[10px] text-muted-foreground">{label}</div>
      <div className="truncate font-medium">{value}</div>
    </div>
  );
}

/** 倍率一步步乘出来：每一步的累计值画成一根条，1 的位置画一条竖线。 */
function FactorSteps({ breakdown }: { breakdown: { base: string; member: string; level: string; time: string; total: string; stacking: string } }) {
  const time = Number(breakdown.time);
  const member = Number(breakdown.member);
  const best = breakdown.stacking === "best" && time < 1;
  const steps: Array<{ label: string; factor: number; skipped?: boolean }> = [
    { label: "全局", factor: Number(breakdown.base) },
    { label: "会员", factor: member, skipped: best && time <= member },
    { label: "等级", factor: Number(breakdown.level) },
    { label: "时段", factor: time, skipped: best && time > member }
  ];
  let running = 1;
  const rows = steps.map((step) => {
    if (!step.skipped) running *= step.factor;
    return { ...step, cumulative: running };
  });
  const max = Math.max(1, ...rows.map((row) => row.cumulative));
  return (
    <div className="mt-5 space-y-1.5">
      {rows.map((row) => (
        <div key={row.label} className="grid grid-cols-[3rem_minmax(0,1fr)_4.5rem] items-center gap-2 text-[11px]">
          <span className={cn("text-muted-foreground", row.skipped && "line-through")}>{row.label}</span>
          <div className="relative h-3 rounded-full bg-muted">
            <div
              className={cn(
                "absolute inset-y-0 left-0 rounded-full transition-all",
                row.cumulative < 1 ? "bg-emerald-500/80" : row.cumulative > 1 ? "bg-rose-500/80" : "bg-muted-foreground/40"
              )}
              style={{ width: `${Math.min(100, (row.cumulative / max) * 100)}%` }}
            />
            <div className="absolute -inset-y-0.5 w-px bg-foreground/50" style={{ left: `${(1 / max) * 100}%` }} />
          </div>
          <span className="text-right font-mono tabular-nums">
            {row.skipped ? "舍掉" : `×${Number(row.factor.toFixed(4))}`}
          </span>
        </div>
      ))}
    </div>
  );
}

/** 服务端给的 7 天时间轴（对这个模型生效的时段），一条长带，按天分隔。 */
function TimelineStrip({ segments }: { segments: AIGatewayTimelineSegment[] }) {
  const start = parseInstant(segments[0].start)?.getTime() ?? 0;
  const end = parseInstant(segments[segments.length - 1].end)?.getTime() ?? start + 1;
  const span = Math.max(end - start, 1);
  const days = Math.max(1, Math.round(span / 86_400_000));
  // 时间轴的端点带应用时区的偏移（2026-11-26T00:00:00+08:00），前 10 位就是应用时区的日期
  const firstDay = /^\d{4}-\d{2}-\d{2}/.test(segments[0].start) ? segments[0].start.slice(0, 10) : "";
  return (
    <section className="rounded-2xl border border-border bg-card p-4" style={{ boxShadow: "var(--shadow-soft)" }}>
      <h4 className="mb-3 flex items-center gap-1.5 text-sm font-semibold">
        <Clock3 className="size-3.5" />
        未来 {days} 天的价格
        <span className="text-[11px] font-normal text-muted-foreground">服务端时间轴 · 从今天零点起</span>
      </h4>
      <div className="relative flex h-8 overflow-hidden rounded-lg bg-muted">
        {segments.map((segment) => {
          const from = parseInstant(segment.start)?.getTime() ?? start;
          const to = parseInstant(segment.end)?.getTime() ?? from;
          const tone = segment.windowId ? toneMeta(segment.tone) : null;
          return (
            <span
              key={`${segment.start}-${segment.windowId}`}
              className={cn("h-full border-r border-background/60", tone ? tone.solid : "bg-transparent")}
              style={{ width: `${((to - from) / span) * 100}%`, opacity: tone ? toneStrength(Number(segment.multiplier)) : undefined }}
              title={`${formatDateTimeMinute(segment.start)} – ${formatDateTimeMinute(segment.end)} · ${segment.name} ${formatFactor(segment.multiplier)}`}
            />
          );
        })}
        {Array.from({ length: Math.max(days - 1, 0) }, (_, index) => (
          <span
            key={index}
            className="pointer-events-none absolute inset-y-0 w-px bg-foreground/30"
            style={{ left: `${((index + 1) / days) * 100}%` }}
          />
        ))}
      </div>
      <div className="mt-1 flex justify-between font-mono text-[10px] text-muted-foreground">
        {Array.from({ length: days }, (_, index) => (
          <span key={index}>{firstDay ? addDays(firstDay, index).slice(5) : `第 ${index + 1} 天`}</span>
        ))}
      </div>
    </section>
  );
}
