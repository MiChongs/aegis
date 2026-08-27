"use client";

import { useMemo } from "react";
import { AlertTriangle, ShieldAlert, ShieldCheck, User } from "lucide-react";
import { ApiError } from "@/lib/api/client";
import {
  CAPABILITY_GROUP_LABELS,
  CAPABILITY_RISK_LABELS,
  type AppFunctionEffect,
  type FunctionCapability
} from "@/lib/api/app-functions";
import { Badge } from "@/components/ui/badge";
import { Checkbox } from "@/components/ui/checkbox";
import { cn } from "@/lib/utils";

export function errorMessage(error: unknown) {
  return error instanceof ApiError
    ? error.message
    : error instanceof Error
      ? error.message
      : "操作失败";
}

export function formatTime(value?: string | null) {
  return value ? new Date(value).toLocaleString("zh-CN") : "—";
}

export function formatBytes(value: number) {
  if (value < 1024) return `${value} B`;
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} KB`;
  return `${(value / 1024 / 1024).toFixed(1)} MB`;
}

export function formatDuration(ms: number) {
  return ms >= 1000 ? `${(ms / 1000).toFixed(2)} s` : `${ms.toFixed(1)} ms`;
}

/**
 * 成功率在没有任何调用时显示「—」而不是 0%。
 *
 * 「没人调过」和「调了但全失败」会导出完全相反的结论，
 * 把前者显示成 0% 会让人以为函数坏了（与 Banner 点击率同一条约定）。
 */
export function formatRate(rate: number, total: number) {
  if (!total) return "—";
  return `${(rate * 100).toFixed(1)}%`;
}

const RISK_VARIANT: Record<string, "success" | "warning" | "danger"> = {
  low: "success",
  medium: "warning",
  high: "danger"
};

export function RiskBadge({ risk }: { risk: string }) {
  return (
    <Badge variant={RISK_VARIANT[risk] ?? "secondary"} size="sm">
      {CAPABILITY_RISK_LABELS[risk] ?? risk}
    </Badge>
  );
}

export function StatTile({
  label,
  value,
  hint,
  tone
}: {
  label: string;
  value: string;
  hint?: string;
  tone?: "default" | "danger" | "success";
}) {
  return (
    <div className="rounded-lg border p-3">
      <p className="text-xs text-muted-foreground">{label}</p>
      <p
        className={cn(
          "mt-1 font-mono text-lg",
          tone === "danger" && "text-destructive",
          tone === "success" && "text-emerald-600 dark:text-emerald-400"
        )}
      >
        {value}
      </p>
      {hint ? <p className="mt-0.5 text-[11px] text-muted-foreground">{hint}</p> : null}
    </div>
  );
}

/**
 * 能力勾选树。分组、风险、说明全部来自后端目录 ——
 * 后端新增一种能力，这里零改动即自动出现。
 *
 * 旧能力名（deprecated）只在**已经勾着**时才渲染：它们对新函数没有任何作用，
 * 但直接藏掉会让存量函数的设置页看起来少了一项，保存时又被悄悄清掉。
 */
export function CapabilityPicker({
  catalog,
  selected,
  onChange,
  disabled
}: {
  catalog: FunctionCapability[];
  selected: string[];
  onChange: (next: string[]) => void;
  disabled?: boolean;
}) {
  const groups = useMemo(() => {
    const visible = catalog.filter((item) => !item.deprecated || selected.includes(item.key));
    const map = new Map<string, FunctionCapability[]>();
    for (const item of visible) {
      if (!map.has(item.group)) map.set(item.group, []);
      map.get(item.group)!.push(item);
    }
    return Array.from(map.entries());
  }, [catalog, selected]);

  function toggle(key: string, checked: boolean) {
    onChange(checked ? [...selected, key] : selected.filter((item) => item !== key));
  }

  if (!catalog.length) {
    return <p className="text-xs text-muted-foreground">能力目录加载中…</p>;
  }

  return (
    <div className="space-y-4">
      {groups.map(([group, items]) => (
        <div key={group} className="space-y-2">
          <p className="text-xs font-medium text-muted-foreground">
            {CAPABILITY_GROUP_LABELS[group] ?? group}
          </p>
          <div className="grid gap-2 sm:grid-cols-2">
            {items.map((item) => {
              const checked = selected.includes(item.key);
              return (
                <label
                  key={item.key}
                  className={cn(
                    "flex cursor-pointer items-start gap-2.5 rounded-lg border p-2.5 text-xs transition-colors",
                    checked ? "border-primary/60 bg-muted/50" : "hover:bg-muted/40",
                    disabled && "pointer-events-none opacity-60"
                  )}
                >
                  <Checkbox
                    className="mt-0.5"
                    checked={checked}
                    disabled={disabled}
                    onCheckedChange={(value) => toggle(item.key, value === true)}
                  />
                  <span className="min-w-0 flex-1">
                    <span className="flex flex-wrap items-center gap-1.5">
                      <span className="font-medium">{item.label}</span>
                      <RiskBadge risk={item.risk} />
                      {item.requiresUser ? (
                        <Badge variant="outline" size="sm" className="gap-1">
                          <User className="size-3" />
                          需用户身份
                        </Badge>
                      ) : null}
                      {item.deprecated ? (
                        <Badge variant="outline" size="sm">
                          已废弃
                        </Badge>
                      ) : null}
                    </span>
                    <code className="mt-1 block font-mono text-[10px] text-muted-foreground">
                      {item.api}
                    </code>
                    <span className="mt-0.5 block text-[11px] leading-snug text-muted-foreground">
                      {item.hint}
                    </span>
                  </span>
                </label>
              );
            })}
          </div>
        </div>
      ))}
      <p className="flex items-start gap-1.5 text-xs text-muted-foreground">
        <ShieldCheck className="mt-0.5 size-3.5 shrink-0" />
        声明即授权：未勾选的能力在脚本中不可用（对应对象为{" "}
        <code className="font-mono">undefined</code>），编辑器亦不提供提示。
      </p>
    </div>
  );
}

/**
 * 副作用清单。
 *
 * 试跑产生的每一条都带「未执行」标记 —— 少了这个标记，一份看起来
 * 「发了 100 积分、开了 30 天会员」的清单会被当成真的发生过。
 */
export function EffectList({ effects }: { effects: AppFunctionEffect[] }) {
  if (!effects.length) {
    return <p className="py-6 text-center text-xs text-muted-foreground">无副作用</p>;
  }
  return (
    <div className="space-y-1.5">
      {effects.map((effect, index) => (
        <div
          key={`${effect.type}-${index}`}
          className="flex items-start gap-2 rounded-lg border p-2 text-xs"
        >
          <Badge variant={effect.simulated ? "outline" : "secondary"} size="sm" className="shrink-0">
            {effect.type}
          </Badge>
          <code className="min-w-0 flex-1 break-all font-mono text-[11px] text-muted-foreground">
            {JSON.stringify(effect.arguments)}
          </code>
          {effect.simulated ? (
            <Badge variant="warning" size="sm" className="shrink-0 gap-1">
              <AlertTriangle className="size-3" />
              未执行
            </Badge>
          ) : null}
        </div>
      ))}
    </div>
  );
}

/** 只在勾了高危能力时出现的提示条：默认状态下不该有常驻警告。 */
export function HighRiskNotice({
  catalog,
  selected
}: {
  catalog: FunctionCapability[];
  selected: string[];
}) {
  const risky = catalog.filter((item) => item.risk === "high" && selected.includes(item.key));
  if (!risky.length) return null;
  return (
    <div className="flex items-start gap-2 rounded-lg border border-destructive/40 bg-destructive/5 p-2.5 text-xs">
      <ShieldAlert className="mt-0.5 size-3.5 shrink-0 text-destructive" />
      <span className="min-w-0">
        已启用高风险能力：
        <span className="font-medium">{risky.map((item) => item.label).join("、")}</span>。
        相关操作将直接作用于真实用户与资金，发布前请通过试跑验证。
      </span>
    </div>
  );
}
