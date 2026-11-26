"use client";

import { useState } from "react";
import { Loader2, RotateCcw, Save, ShieldCheck } from "lucide-react";
import { toast } from "sonner";
import { Section, StateBlock, errorMessage } from "@/components/users/detail/kit";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import type { PointsStoreGiftPolicyView, PointsStoreGiftRule, PointsStoreGiftRuleAction, PointsStoreGiftRuleSpec } from "@/lib/api/points-store";
import { formatDateTime } from "@/lib/datetime";
import { usePointsStoreGiftPolicyQuery, useSavePointsStoreGiftPolicyMutation } from "@/lib/points-store-hooks";
import { cn } from "@/lib/utils";

/**
 * 赠送风控（docs/points-store-gift.md）。
 *
 * 判据目录由后端下发（`/points-store/gift-policy` 的 catalog）：含义、阈值范围、可选处置都来自那里，
 * 这里不另抄一份。每条判据独立判，命中多条取最重的处置：拒绝不扣积分、直接告诉用户原因；
 * 审核先扣积分、进审核队列。自己送自己、对方停用 / 封禁 / 注销中、对方已拥有是硬性条件，不在这里配置。
 */
export function PointsStoreGiftPolicyPanel({ appKey }: { appKey: string }) {
  const query = usePointsStoreGiftPolicyQuery(appKey);
  if (query.isLoading) return <Skeleton className="h-[480px] w-full rounded-xl" />;
  if (query.error || !query.data) {
    return <StateBlock kind="error" title="赠送风控读取失败" description={errorMessage(query.error)} onRetry={() => query.refetch()} />;
  }
  return <PolicyForm key={query.data.policy.updatedAt ?? "default"} appKey={appKey} view={query.data} />;
}

function PolicyForm({ appKey, view }: { appKey: string; view: PointsStoreGiftPolicyView }) {
  const save = useSavePointsStoreGiftPolicyMutation(appKey);
  const [enabled, setEnabled] = useState(view.policy.enabled);
  const [rules, setRules] = useState<PointsStoreGiftRule[]>(view.policy.rules);
  const rule = (key: string) => rules.find((item) => item.key === key);
  const patch = (key: string, next: Partial<PointsStoreGiftRule>) =>
    setRules((current) => current.map((item) => (item.key === key ? { ...item, ...next } : item)));
  const dirty = enabled !== view.policy.enabled || JSON.stringify(rules) !== JSON.stringify(view.policy.rules);

  const submit = async () => {
    try {
      await save.mutateAsync({ enabled, rules });
      toast.success("赠送风控已保存，对新的赠送立即生效");
    } catch (error) {
      toast.error(errorMessage(error, "保存失败"));
    }
  };

  return (
    <Section
      title="赠送风控"
      icon={ShieldCheck}
      tone="warning"
      description={
        view.policy.configured
          ? `${view.policy.updatedAt ? formatDateTime(view.policy.updatedAt) : ""} 由${view.policy.updatedBy || "管理员"}保存。命中多条时取最重的处置：拒绝不扣积分，审核先扣积分、进审核队列`
          : "当前是推荐默认值，保存后按这里的配置执行。命中多条时取最重的处置：拒绝不扣积分，审核先扣积分、进审核队列"
      }
      actions={
        <>
          <Button
            variant="ghost"
            size="sm"
            onClick={() => {
              setEnabled(view.defaults.enabled);
              setRules(view.defaults.rules);
            }}
          >
            <RotateCcw />
            推荐值
          </Button>
          <Button size="sm" onClick={submit} disabled={!dirty || save.isPending}>
            {save.isPending ? <Loader2 className="animate-spin" /> : <Save />}
            保存
          </Button>
        </>
      }
      flush
    >
      <div className="flex flex-col divide-y">
        <label className="flex cursor-pointer items-center justify-between gap-4 px-4 py-3.5 sm:px-5">
          <span className="flex flex-col gap-0.5">
            <span className="text-sm font-medium">允许赠送</span>
            <span className="text-xs text-muted-foreground">
              关闭后赠送商品照常展示，但用户提交时提示「赠送功能暂未开放」；已送达的不受影响
            </span>
          </span>
          <Switch checked={enabled} onCheckedChange={setEnabled} aria-label="允许赠送" />
        </label>
        {view.catalog.map((spec) => {
          const current = rule(spec.key) ?? spec.default;
          return <RuleRow key={spec.key} spec={spec} rule={current} disabled={!enabled} onChange={(next) => patch(spec.key, next)} />;
        })}
      </div>
    </Section>
  );
}

const ACTION_LABEL: Record<PointsStoreGiftRuleAction, string> = { review: "送审核", reject: "拒绝" };

function RuleRow({
  spec,
  rule,
  disabled,
  onChange
}: {
  spec: PointsStoreGiftRuleSpec;
  rule: PointsStoreGiftRule;
  disabled: boolean;
  onChange: (next: Partial<PointsStoreGiftRule>) => void;
}) {
  const off = disabled || !rule.enabled;
  return (
    <div className={cn("flex flex-col gap-3 px-4 py-3.5 sm:flex-row sm:items-start sm:px-5", disabled && "opacity-60")}>
      <div className="flex min-w-0 flex-1 items-start gap-3">
        <Switch
          checked={rule.enabled}
          onCheckedChange={(checked) => onChange({ enabled: checked })}
          disabled={disabled}
          aria-label={spec.label}
          className="mt-0.5"
        />
        <div className="flex min-w-0 flex-col gap-0.5">
          <span className="text-sm font-medium">{spec.label}</span>
          <span className="text-xs leading-5 text-muted-foreground">{spec.hint}</span>
        </div>
      </div>
      <div className={cn("flex flex-wrap items-center gap-2 sm:justify-end", off && "pointer-events-none opacity-50")}>
        {spec.thresholdLabel ? (
          <NumberField
            label={spec.thresholdLabel}
            unit={spec.thresholdUnit}
            value={rule.threshold}
            min={spec.min}
            max={spec.max}
            onChange={(value) => onChange({ threshold: value })}
          />
        ) : null}
        {spec.window ? (
          <NumberField
            label="时间窗"
            unit="小时"
            value={rule.windowHours}
            min={1}
            max={spec.maxWindow}
            onChange={(value) => onChange({ windowHours: value })}
          />
        ) : null}
        {spec.actions.length > 1 ? (
          <ToggleGroup
            type="single"
            variant="outline"
            size="sm"
            value={rule.action}
            onValueChange={(value) => value && onChange({ action: value as PointsStoreGiftRuleAction })}
          >
            {spec.actions.map((action) => (
              <ToggleGroupItem key={action} value={action} className="px-3 text-xs">
                {ACTION_LABEL[action]}
              </ToggleGroupItem>
            ))}
          </ToggleGroup>
        ) : (
          <span className="rounded-md border px-2.5 py-1 text-xs text-muted-foreground">{ACTION_LABEL[spec.actions[0]]}</span>
        )}
      </div>
    </div>
  );
}

function NumberField({
  label,
  unit,
  value,
  min,
  max,
  onChange
}: {
  label: string;
  unit?: string;
  value?: number;
  min?: number;
  max?: number;
  onChange: (value: number) => void;
}) {
  return (
    <label className="flex items-center gap-1.5 text-xs text-muted-foreground">
      <span className="whitespace-nowrap">{label}</span>
      <Input
        type="number"
        inputMode="numeric"
        className="h-8 w-24 tabular-nums"
        value={value ?? ""}
        min={min}
        max={max}
        onChange={(event) => onChange(Math.max(0, Math.trunc(Number(event.target.value) || 0)))}
      />
      {unit ? <span className="whitespace-nowrap">{unit}</span> : null}
    </label>
  );
}
