"use client";

/**
 * 扣减经验时的等级处理：两张可选卡片（保留现有等级 / 按经验重新判定）+ 调整后等级的预估。
 *
 * 预估用等级体系的推演接口按「扣减后的经验」算一次（不含这个人的晋级条件画像，所以标「预计」），
 * 再按锁定的保底与封顶截一下；最终结果以提交后的返回为准。推演结果按经验值缓存，表单与确认框共用。
 */

import * as React from "react";
import { useQuery } from "@tanstack/react-query";
import { ArrowRight, Loader2, Lock, ShieldMinus, TrendingDown } from "lucide-react";
import type { ExperienceLevelMode } from "@/lib/api/apps";
import { simulateLevel } from "@/lib/api/levels";
import { useAdminToken } from "@/lib/admin-hooks";
import { useUserGrowthQuery } from "@/lib/growth-hooks";
import { cn } from "@/lib/utils";
import { useDebouncedValue } from "./kit";

const MODES: Array<{ value: ExperienceLevelMode; title: string; description: string; icon: React.ComponentType<{ className?: string }> }> = [
  { value: "keep", title: "保留现有等级", description: "只扣经验，已达到的等级不变；之后要重新攒够经验才会再升级", icon: Lock },
  { value: "recalc", title: "按经验重新判定", description: "按扣减后的经验重新判定，可能降级；不低于锁定的保底", icon: TrendingDown }
];

type Prediction = { current: number; next: number | null; locked: number; capped: number; loading: boolean };

/** 预估调整后的等级。amount 为扣减数量（正数字符串），无效时为 null。 */
function usePrediction(appKey: string, userId: number, experience: number, amount: string | null, mode: ExperienceLevelMode): Prediction {
  const token = useAdminToken();
  const growth = useUserGrowthQuery(appKey, userId);
  const debounced = useDebouncedValue(amount, 300);
  const target = debounced !== null ? Math.max(experience - Number(debounced), 0) : null;
  const simulation = useQuery({
    queryKey: ["admin-level-simulate-exp", token, appKey, target],
    queryFn: () => simulateLevel(token as string, appKey, { experience: target as number }),
    enabled: Boolean(token && appKey && mode === "recalc" && target !== null),
    staleTime: 60_000
  });
  const state = growth.data?.level;
  const current = state?.level ?? 0;
  const locked = state?.locked ?? 0;
  const capped = state?.capped ?? 0;
  if (mode === "keep") return { current, next: current || null, locked, capped, loading: growth.isLoading };
  const simulated = simulation.data?.level;
  const next = simulated !== undefined && current ? Math.min(Math.max(simulated, locked, 1), current) : null;
  return { current, next, locked, capped, loading: growth.isLoading || simulation.isFetching };
}

export function ExperienceLevelChoice({
  appKey,
  userId,
  experience,
  amount,
  mode,
  onModeChange
}: {
  appKey: string;
  userId: number;
  experience: number;
  amount: string | null;
  mode: ExperienceLevelMode;
  onModeChange: (mode: ExperienceLevelMode) => void;
}) {
  const prediction = usePrediction(appKey, userId, experience, amount, mode);
  return (
    <div className="flex flex-col gap-2">
      <span className="text-xs text-muted-foreground">等级处理</span>
      <div role="radiogroup" aria-label="扣减经验后的等级处理" className="grid gap-2 sm:grid-cols-2">
        {MODES.map((item) => {
          const active = mode === item.value;
          return (
            <button
              key={item.value}
              type="button"
              role="radio"
              aria-checked={active}
              onClick={() => onModeChange(item.value)}
              className={cn(
                "flex items-start gap-2.5 rounded-lg border p-3 text-left transition-colors",
                active ? "border-primary bg-primary/5" : "hover:bg-accent/50"
              )}
            >
              <span
                className={cn(
                  "mt-0.5 flex size-4 shrink-0 items-center justify-center rounded-full border",
                  active ? "border-primary" : "border-muted-foreground/40"
                )}
              >
                {active ? <span className="size-2 rounded-full bg-primary" /> : null}
              </span>
              <span className="flex min-w-0 flex-col gap-0.5">
                <span className="flex items-center gap-1.5 text-sm font-medium">
                  <item.icon className="size-3.5 text-muted-foreground" />
                  {item.title}
                </span>
                <span className="text-xs leading-5 text-muted-foreground">{item.description}</span>
              </span>
            </button>
          );
        })}
      </div>
      <LevelOutcome prediction={prediction} mode={mode} amount={amount} />
      <p className="text-[11px] leading-4 text-muted-foreground">已发放的等级奖励不收回；以后再升回同一级也不会重复发放。</p>
    </div>
  );
}

function LevelOutcome({ prediction, mode, amount }: { prediction: Prediction; mode: ExperienceLevelMode; amount: string | null }) {
  if (!prediction.current) return null;
  const down = prediction.next !== null && prediction.next < prediction.current;
  return (
    <div
      className={cn(
        "flex flex-wrap items-center gap-x-2 gap-y-1 rounded-lg px-3 py-2 text-xs",
        down ? "bg-amber-500/10 text-amber-800 dark:text-amber-200" : "bg-muted/60 text-muted-foreground"
      )}
    >
      <span>调整后等级</span>
      <span className="font-semibold tabular-nums text-foreground">Lv.{prediction.current}</span>
      <ArrowRight className="size-3" />
      {prediction.loading && mode === "recalc" && amount ? (
        <Loader2 className="size-3 animate-spin" />
      ) : (
        <span className={cn("font-semibold tabular-nums", down ? "text-amber-700 dark:text-amber-300" : "text-foreground")}>
          {prediction.next !== null && amount ? `Lv.${prediction.next}` : "—"}
        </span>
      )}
      {mode === "recalc" ? <span className="opacity-70">（预计，不含晋级条件）</span> : null}
      {prediction.locked > 0 ? (
        <span className="inline-flex items-center gap-1">
          <Lock className="size-3" />
          保底 Lv.{prediction.locked}
        </span>
      ) : null}
      {prediction.capped > 0 ? (
        <span className="inline-flex items-center gap-1">
          <ShieldMinus className="size-3" />
          封顶 Lv.{prediction.capped}
        </span>
      ) : null}
    </div>
  );
}

/** 确认框里的一行：等级处理方式与预计结果。 */
export function ExperienceLevelConfirm({
  appKey,
  userId,
  experience,
  amount,
  mode
}: {
  appKey: string;
  userId: number;
  experience: number;
  amount: string | null;
  mode: ExperienceLevelMode;
}) {
  const prediction = usePrediction(appKey, userId, experience, amount, mode);
  const label = MODES.find((item) => item.value === mode)?.title ?? mode;
  const down = prediction.next !== null && prediction.current > 0 && prediction.next < prediction.current;
  return (
    <span className="inline-flex flex-wrap items-center justify-end gap-1.5 text-sm">
      <span>{label}</span>
      {prediction.current ? (
        <span className={cn("tabular-nums", down ? "font-medium text-amber-600 dark:text-amber-400" : "text-muted-foreground")}>
          Lv.{prediction.current} → {prediction.next !== null ? `Lv.${prediction.next}` : "—"}
        </span>
      ) : null}
    </span>
  );
}
