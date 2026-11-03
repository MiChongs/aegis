"use client";

import {
  Anchor,
  Award,
  BadgeCheck,
  Compass,
  Crown,
  Diamond,
  Download,
  Feather,
  Flame,
  Footprints,
  Gem,
  Gift,
  Heart,
  Layers,
  Leaf,
  Medal,
  MessageCircle,
  Milestone,
  Moon,
  Mountain,
  Orbit,
  Palette,
  Rocket,
  ShieldCheck,
  Shield,
  Sparkles,
  Sprout,
  Star,
  Sun,
  Trophy,
  Zap,
  type LucideIcon
} from "lucide-react";
import { describeItem, rulesExpr } from "@/components/apps/item-grant/item-grant-meta";
import type { Tone } from "@/components/users/detail/kit";
import type { ItemGrantCatalog } from "@/lib/api/item-grant";
import type {
  LevelCatalog,
  LevelEventReason,
  LevelGateData,
  LevelJobStatus,
  LevelLevelData,
  LevelNodeType,
  LevelPrivilegeData,
  LevelPrivilegeKind,
  LevelRewardData,
  LevelRewardStatus,
  LevelSchemeData
} from "@/lib/api/levels";

/**
 * 等级体系的展示元数据：节点图标与色调、等级图标（目录里的 lucide 名 → 组件）、状态与原因的文案、
 * 节点在画布上的一句话摘要，以及把等级目录适配成物品发放组件（物品编辑器、条件构建器）认的目录形状。
 */

export type LevelNodeVisual = { icon: LucideIcon; tone: Tone; accent: string };

export const LEVEL_NODE_VISUAL: Record<LevelNodeType, LevelNodeVisual> = {
  scheme: { icon: Layers, tone: "violet", accent: "#8b5cf6" },
  level: { icon: Milestone, tone: "primary", accent: "#6366f1" },
  gate: { icon: ShieldCheck, tone: "warning", accent: "#f59e0b" },
  reward: { icon: Gift, tone: "success", accent: "#10b981" },
  privilege: { icon: Sparkles, tone: "info", accent: "#0ea5e9" }
};

/** 目录里的图标键（lucide 名）→ 组件。App 端各自映射到本地图标。 */
export const LEVEL_ICON: Record<string, LucideIcon> = {
  sprout: Sprout,
  leaf: Leaf,
  feather: Feather,
  footprints: Footprints,
  sparkles: Sparkles,
  shield: Shield,
  award: Award,
  medal: Medal,
  anchor: Anchor,
  compass: Compass,
  mountain: Mountain,
  orbit: Orbit,
  rocket: Rocket,
  zap: Zap,
  flame: Flame,
  sun: Sun,
  moon: Moon,
  star: Star,
  gem: Gem,
  diamond: Diamond,
  crown: Crown,
  trophy: Trophy,
  heart: Heart,
  gift: Gift,
  "badge-check": BadgeCheck,
  palette: Palette,
  download: Download,
  "message-circle": MessageCircle
};

/** 等级颜色的预设色板（也可以填任意 #RRGGBB）。 */
export const LEVEL_COLORS = [
  "#94a3b8",
  "#64748b",
  "#b45309",
  "#ca8a04",
  "#65a30d",
  "#16a34a",
  "#0d9488",
  "#0891b2",
  "#2563eb",
  "#4f46e5",
  "#7c3aed",
  "#9333ea",
  "#c026d3",
  "#db2777",
  "#dc2626",
  "#ea580c"
];

export const PRIVILEGE_ICON: Record<LevelPrivilegeKind, LucideIcon> = {
  exp_boost: Zap,
  integral_boost: Sparkles,
  display: BadgeCheck,
  custom: Diamond
};

export const REWARD_STATUS_META: Record<LevelRewardStatus, { label: string; variant: "success" | "warning" | "danger" | "info" | "secondary" }> = {
  queued: { label: "发放中", variant: "info" },
  claimable: { label: "待领取", variant: "secondary" },
  granted: { label: "已到账", variant: "success" },
  pending: { label: "待人工处理", variant: "warning" },
  failed: { label: "发放失败", variant: "danger" }
};

export const EVENT_REASON_LABEL: Record<LevelEventReason, string> = {
  experience: "经验增长",
  sync: "补判",
  recalc: "重算任务",
  admin: "管理员调整",
  penalty: "封顶降级"
};

export const JOB_STATUS_META: Record<LevelJobStatus, { label: string; variant: "info" | "success" | "danger" | "secondary" }> = {
  running: { label: "进行中", variant: "info" },
  succeeded: { label: "已完成", variant: "success" },
  failed: { label: "失败", variant: "danger" },
  cancelled: { label: "已取消", variant: "secondary" }
};

export function formatNumber(value?: number | null) {
  if (value === undefined || value === null || Number.isNaN(value)) return "—";
  return value.toLocaleString("zh-CN");
}

export function formatMultiplier(value?: number) {
  if (!value) return "1 倍";
  return `${Number(value.toFixed(2))} 倍`;
}

/** 等级目录 → 物品发放组件认的目录（物品编辑器、条件构建器只读其中几项）。 */
export function asItemGrantCatalog(catalog: LevelCatalog): ItemGrantCatalog {
  return {
    items: catalog.items,
    itemGroups: catalog.itemGroups,
    nodes: catalog.nodes.map((spec) => ({
      type: spec.type as never,
      label: spec.label,
      hint: spec.hint,
      category: spec.category,
      outputs: spec.outputs,
      singleton: spec.single,
      noInput: !spec.inputs
    })),
    categories: catalog.categories,
    triggers: [],
    events: [],
    fields: catalog.fields,
    operators: catalog.operators,
    scopes: [],
    periods: [],
    routes: catalog.routes,
    plans: catalog.plans,
    features: catalog.features,
    limits: catalog.limits,
    timezone: catalog.timezone,
    statuses: [],
    templates: []
  };
}

export function privilegeValueText(data: { kind: LevelPrivilegeKind; value?: number; key?: string }, catalog?: LevelCatalog) {
  const spec = catalog?.privileges.find((item) => item.kind === data.kind);
  switch (data.kind) {
    case "exp_boost":
    case "integral_boost":
      return formatMultiplier(data.value);
    case "custom":
      return `${data.key || "未填键"}${data.value ? ` = ${data.value}` : ""}`;
    default:
      return spec?.label ?? "展示权益";
  }
}

/** 节点在画布上的摘要（第一行醒目，其余是补充）。 */
export function summarizeLevelNode(type: LevelNodeType, data: Record<string, unknown>, catalog?: LevelCatalog): string[] {
  switch (type) {
    case "scheme": {
      const cfg = data as LevelSchemeData;
      return [
        cfg.notify ? `升级通知：${cfg.notifyTitle || "未填标题"}` : "不发升级通知",
        cfg.hideLocked ? "App 只展示到下一级" : "App 展示全部等级"
      ];
    }
    case "level": {
      const cfg = data as LevelLevelData;
      return [`累计经验 ${formatNumber(cfg.threshold ?? 0)}`, cfg.description || ""].filter(Boolean);
    }
    case "gate": {
      const cfg = data as LevelGateData;
      const fieldLabel = (key: string) => catalog?.fields.find((field) => field.key === key)?.label ?? key;
      const lines =
        cfg.mode === "expr"
          ? [cfg.expr || "表达式为空"]
          : (cfg.rules ?? []).map((rule) => {
              const op = catalog?.operators.find((candidate) => candidate.op === rule.op);
              const value = op?.arity === 0 ? "" : ` ${(rule.value ?? []).join(op?.arity === 2 ? " 至 " : "、")}`;
              return `${fieldLabel(rule.field)} ${op?.label ?? rule.op}${value}`;
            });
      if (!lines.length) lines.push("还没有条件");
      if (cfg.mode === "any" && lines.length > 1) lines[0] = `任一：${lines[0]}`;
      if (cfg.hint) lines.push(`用户看到：${cfg.hint}`);
      return lines;
    }
    case "reward": {
      const cfg = data as LevelRewardData;
      return [cfg.claim === "manual" ? "用户在 App 里领取" : "升级后自动到账"];
    }
    case "privilege": {
      const cfg = data as LevelPrivilegeData;
      return [privilegeValueText(cfg, catalog), cfg.text || "", cfg.scope === "only" ? "仅本级" : "本级及以上"].filter(Boolean);
    }
  }
}

export function gateExpr(data: LevelGateData) {
  return data.mode === "expr" ? (data.expr ?? "") : rulesExpr(data.mode, data.rules ?? []);
}

export function describeRewardItems(data: LevelRewardData, catalog?: LevelCatalog) {
  const adapted = catalog ? asItemGrantCatalog(catalog) : undefined;
  return (data.items ?? []).map((item) => describeItem(item, adapted));
}

/** 新节点的默认配置。 */
export function defaultLevelNodeData(type: LevelNodeType, context: { nextThreshold?: number } = {}): Record<string, unknown> {
  switch (type) {
    case "scheme":
      return { label: "成长等级", notify: true, notifyTitle: "恭喜升到 Lv.{level} {name}", notifyContent: "{rewards}" };
    case "level":
      return { label: "新等级", threshold: context.nextThreshold ?? 100, icon: "star", color: "#6366f1" };
    case "gate":
      return { mode: "all", rules: [{ field: "user.member", op: "true" }], hint: "需要开通会员" };
    case "reward":
      return { label: "升级礼包", claim: "manual", items: [{ type: "points", amount: 100 }] };
    case "privilege":
      return { label: "签到经验加成", kind: "exp_boost", value: 1.1, icon: "zap", scope: "inherit" };
  }
}
