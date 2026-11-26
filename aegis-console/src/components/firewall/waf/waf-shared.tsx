"use client";

import { useState } from "react";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { NumberField } from "@/components/traffic/traffic-kit";
import type { FirewallRuleGroup, FirewallRuleInfo } from "@/lib/api/firewall";
import { cn } from "@/lib/utils";
import type { SettingsForm } from "./settings-draft";

export type FirewallTab = "overview" | "events" | "rules" | "waf" | "exclusions" | "access" | "bans" | "sandbox";

/* ── 规则在「草稿」下的状态 ── */

export type RuleStatus = "active" | "removed" | "group_off" | "outbound_off" | "above_pl";

export const RULE_STATUS_META: Record<RuleStatus, { label: string; className: string }> = {
  active: { label: "生效", className: "border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-400" },
  removed: { label: "已移除", className: "border-rose-500/30 bg-rose-500/10 text-rose-700 dark:text-rose-400" },
  group_off: { label: "组已停用", className: "border-border bg-muted text-muted-foreground" },
  outbound_off: { label: "出站未开启", className: "border-border bg-muted text-muted-foreground" },
  above_pl: { label: "高于检测等级", className: "border-amber-500/30 bg-amber-500/10 text-amber-700 dark:text-amber-400" }
};

export function RuleStatusBadge({ status, className }: { status: RuleStatus; className?: string }) {
  const meta = RULE_STATUS_META[status];
  return (
    <span className={cn("inline-flex h-5 shrink-0 items-center rounded-md border px-1.5 text-[11px] font-medium whitespace-nowrap", meta.className, className)}>
      {meta.label}
    </span>
  );
}

/**
 * 与后端 catalog.go 的 groupEnabled / Active 判定一致，但按**草稿**算：
 * 改了偏执等级、停了某组、移除了某条，保存之前就能在规则库里看到效果。
 */
export function makeRuleStatus(form: SettingsForm, groups: FirewallRuleGroup[]) {
  const removed = new Set(form.corazaRemovedRules.map((id) => Number(id)));
  const disabled = new Set(form.corazaDisabledGroups);
  const locked = new Set(groups.filter((group) => group.locked).map((group) => group.code));
  const response = new Set(groups.filter((group) => group.kind === "response").map((group) => group.code));
  return (rule: Pick<FirewallRuleInfo, "id" | "group" | "paranoia">): RuleStatus => {
    if (response.has(rule.group) && !form.corazaResponseInspection) return "outbound_off";
    if (!locked.has(rule.group) && disabled.has(rule.group)) return "group_off";
    if (removed.has(rule.id)) return "removed";
    if (rule.paranoia > form.corazaDetectionParanoia) return "above_pl";
    return "active";
  };
}

export const KIND_LABEL: Record<FirewallRuleGroup["kind"], string> = {
  request: "请求",
  response: "出站",
  aegis: "Aegis",
  core: "核心"
};

export const PHASE_LABEL: Record<number, string> = {
  1: "请求头",
  2: "请求体",
  3: "响应头",
  4: "响应体",
  5: "日志"
};

/* ── 字节数输入：KiB / MiB 切换，存字节 ── */

const UNITS = { KiB: 1024, MiB: 1024 * 1024 } as const;
type Unit = keyof typeof UNITS;

export function ByteField({
  value,
  onChange,
  min,
  max,
  ariaLabel,
  className
}: {
  value: number;
  onChange: (bytes: number) => void;
  /** 字节 */
  min?: number;
  /** 字节 */
  max?: number;
  ariaLabel?: string;
  className?: string;
}) {
  const [unit, setUnit] = useState<Unit>(() => (value >= UNITS.MiB && value % (UNITS.MiB / 4) === 0 ? "MiB" : "KiB"));
  const factor = UNITS[unit];
  const shown = Math.round((value / factor) * 100) / 100;
  return (
    <div className={cn("flex min-w-0 items-center gap-1.5", className)}>
      <NumberField
        value={shown}
        min={min !== undefined ? Math.ceil((min / factor) * 100) / 100 : 0}
        max={max !== undefined ? Math.floor((max / factor) * 100) / 100 : undefined}
        step={unit === "MiB" ? 0.25 : 16}
        onChange={(next) => onChange(Math.max(0, Math.round(next * factor)))}
        className="w-24"
        ariaLabel={ariaLabel}
      />
      <Select value={unit} onValueChange={(next) => setUnit(next as Unit)}>
        <SelectTrigger size="sm" className="h-8 w-[76px]" aria-label="单位">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="KiB">KiB</SelectItem>
          <SelectItem value="MiB">MiB</SelectItem>
        </SelectContent>
      </Select>
    </div>
  );
}
