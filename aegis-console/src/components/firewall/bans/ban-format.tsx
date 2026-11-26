"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { Hand, ListFilter, Radar, Zap, type LucideIcon } from "lucide-react";
import type { KitTone } from "@/components/apps/detail/app-detail-kit";
import { useIPBanModesQuery } from "@/lib/admin-hooks";
import type { BanFilter } from "@/lib/api/firewall-bans";
import type { IPBanEntry, IPBanMode } from "@/lib/api/types";
import { formatDateTime } from "@/lib/datetime";
import { cn } from "@/lib/utils";
import { countryName } from "../overview/overview-parts";

/**
 * 封禁页签自用的说法与小工具：来源、状态、剩余时长、响应模式的短名、IP 解析、CSV。
 * 列表、对话框、IP 查询都从这里取，同一个词不在两处各写一遍。
 */

/* ── 来源 ── */

export const SOURCE_META: Record<string, { label: string; hint: string; icon: LucideIcon; tone: KitTone; className: string }> = {
  manual: {
    label: "手动",
    hint: "管理员手动封禁",
    icon: Hand,
    tone: "sky",
    className: "border-sky-500/30 bg-sky-500/10 text-sky-700 dark:text-sky-300"
  },
  auto: {
    label: "自动封禁",
    hint: "拦截次数超过阈值后自动封禁（只数真实拦截）",
    icon: Zap,
    tone: "amber",
    className: "border-amber-500/30 bg-amber-500/10 text-amber-700 dark:text-amber-300"
  },
  risk: {
    label: "风控处置",
    hint: "风控平台处置下发的封禁",
    icon: Radar,
    tone: "violet",
    className: "border-violet-500/30 bg-violet-500/10 text-violet-700 dark:text-violet-300"
  },
  rule: {
    label: "自定义规则",
    hint: "防火墙自定义规则的封禁动作",
    icon: ListFilter,
    tone: "emerald",
    className: "border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300"
  }
};

export const SOURCE_KEYS = ["manual", "auto", "risk", "rule"] as const;

export function sourceLabel(source: string) {
  return SOURCE_META[source]?.label ?? (source || "未知");
}

export function SourceBadge({ source, className }: { source: string; className?: string }) {
  const meta = SOURCE_META[source];
  const Icon = meta?.icon;
  return (
    <span
      title={meta?.hint}
      className={cn(
        "inline-flex h-5 max-w-full shrink-0 items-center gap-1 rounded-md border px-1.5 text-[11px] font-medium whitespace-nowrap",
        meta?.className ?? "border-border text-muted-foreground",
        className
      )}
    >
      {Icon ? <Icon className="size-3 shrink-0" /> : null}
      {sourceLabel(source)}
    </span>
  );
}

/* ── 状态 ── */

export const STATUS_META: Record<string, { label: string; dot: string; text: string }> = {
  active: { label: "生效中", dot: "bg-rose-500", text: "text-rose-700 dark:text-rose-300" },
  expired: { label: "已过期", dot: "bg-muted-foreground/50", text: "text-muted-foreground" },
  revoked: { label: "已解除", dot: "bg-sky-500", text: "text-sky-700 dark:text-sky-300" }
};

export function statusLabel(status: string) {
  return STATUS_META[status]?.label ?? status;
}

/** 当前时刻；每 `interval` 毫秒前移一次（剩余时长、相对时间用，渲染期不直接读时钟） */
export function useNow(interval = 30_000) {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), interval);
    return () => window.clearInterval(timer);
  }, [interval]);
  return now;
}

/** 生效中的封禁还剩多久；永久返回 null */
export function remainingMs(ban: Pick<IPBanEntry, "expiresAt">, now: number) {
  if (!ban.expiresAt) return null;
  const at = Date.parse(ban.expiresAt);
  return Number.isNaN(at) ? null : at - now;
}

/** 毫秒 → 「3 小时」「2 天 5 小时」「12 分钟」 */
export function formatSpan(ms: number) {
  const minutes = Math.max(1, Math.ceil(ms / 60_000));
  if (minutes < 60) return `${minutes} 分钟`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours} 小时`;
  const days = Math.floor(hours / 24);
  const rest = hours % 24;
  return days < 7 && rest > 0 ? `${days} 天 ${rest} 小时` : `${days} 天`;
}

export type BanTimeState = { text: string; tone: "danger" | "warning" | "muted" | "info"; title?: string };

/** 行上的「状态 + 剩余」：生效中给倒计时，失效的给原因 */
export function banTimeState(ban: IPBanEntry, now: number): BanTimeState {
  if (ban.status === "revoked") {
    return { text: "已解除", tone: "info", title: ban.revokeNote ? `解除备注：${ban.revokeNote}` : ban.revokedAt ? `解除于 ${formatDateTime(ban.revokedAt)}` : undefined };
  }
  const left = remainingMs(ban, now);
  if (ban.status === "expired" || (left !== null && left <= 0)) {
    return { text: "已过期", tone: "muted", title: ban.expiresAt ? `到期于 ${formatDateTime(ban.expiresAt)}` : undefined };
  }
  if (left === null) return { text: "永久", tone: "danger", title: "不会自动解除" };
  return {
    text: `剩 ${formatSpan(left)}`,
    tone: left <= 86_400_000 ? "warning" : "danger",
    title: `到期于 ${formatDateTime(ban.expiresAt)}`
  };
}

export const TIME_TONE: Record<BanTimeState["tone"], string> = {
  danger: "text-foreground",
  warning: "text-amber-700 dark:text-amber-400",
  muted: "text-muted-foreground",
  info: "text-sky-700 dark:text-sky-300"
};

export function isActiveBan(ban: IPBanEntry, now: number) {
  if (ban.status !== "active") return false;
  const left = remainingMs(ban, now);
  return left === null || left > 0;
}

/* ── 时长 ── */

export const DURATION_PRESETS = [
  { value: 3600, label: "1 小时" },
  { value: 86_400, label: "24 小时" },
  { value: 604_800, label: "7 天" },
  { value: 2_592_000, label: "30 天" },
  { value: 0, label: "永久" }
] as const;

export function durationLabel(seconds: number) {
  if (seconds <= 0) return "永久";
  const preset = DURATION_PRESETS.find((item) => item.value === seconds);
  if (preset) return preset.label;
  if (seconds % 86_400 === 0) return `${seconds / 86_400} 天`;
  if (seconds % 3600 === 0) return `${seconds / 3600} 小时`;
  if (seconds % 60 === 0) return `${seconds / 60} 分钟`;
  return `${seconds} 秒`;
}

/* ── 响应模式 ── */

/** 行内徽标用的短名（下拉里用服务端下发的全名） */
const MODE_SHORT: Record<string, string> = {
  forbidden: "HTTP 403",
  silent_drop: "不响应",
  connection_reset: "连接重置",
  tarpit: "拖延",
  stealth_404: "伪装 404",
  stealth_503: "伪装 503",
  teapot: "418 彩蛋",
  gone: "410 移除",
  rate_choke: "严格限速",
  redirect: "302 重定向",
  honeypot: "蜜罐",
  fake_empty: "200 空响应",
  random_error: "随机 5xx",
  slow_response: "慢响应",
  bandwidth_choke: "带宽抑制",
  random_delay: "随机延迟",
  zip_bomb: "压缩炸弹",
  chunked_infinite: "无限流",
  infinite_redirect: "无限跳转",
  mirror_request: "请求回显",
  fake_login: "伪装登录",
  random_garbage: "随机垃圾",
  cursed_headers: "诅咒头部",
  json_bomb: "JSON 炸弹",
  cookie_bomb: "Cookie 膨胀",
  reverse_slowloris: "反向 Slowloris"
};

/** 服务端的模式名前面带装饰符号，界面上去掉 */
function cleanLabel(label: string) {
  return label.replace(/^[\p{Extended_Pictographic}️‍\s]+/u, "").trim();
}

export type BanModes = {
  modes: Array<{ value: IPBanMode; label: string; description: string }>;
  defaultMode: IPBanMode;
  loading: boolean;
  /** 全名；空 / "default" → 「默认（当前 X）」 */
  label: (mode: string | null | undefined) => string;
  /** 短名；空 / "default" → 「默认」 */
  short: (mode: string | null | undefined) => string;
  describe: (mode: string | null | undefined) => string;
};

export function useBanModes(): BanModes {
  const query = useIPBanModesQuery();
  const modes = useMemo(
    () => (query.data?.modes ?? []).map((item) => ({ value: item.value, label: cleanLabel(item.label), description: item.description })),
    [query.data?.modes]
  );
  const defaultMode = query.data?.default || "forbidden";
  const full = useCallback((mode: string) => modes.find((item) => item.value === mode)?.label ?? MODE_SHORT[mode] ?? mode, [modes]);
  const label = useCallback(
    (mode: string | null | undefined) => (!mode || mode === "default" ? `默认（当前 ${full(defaultMode)}）` : full(mode)),
    [full, defaultMode]
  );
  const short = useCallback((mode: string | null | undefined) => (!mode || mode === "default" ? "默认" : (MODE_SHORT[mode] ?? full(mode))), [full]);
  const describe = useCallback(
    (mode: string | null | undefined) => {
      if (!mode || mode === "default") return `沿用全局默认响应模式，当前是「${full(defaultMode)}」；全局默认改了也会跟着变。`;
      return modes.find((item) => item.value === mode)?.description ?? "";
    },
    [modes, full, defaultMode]
  );
  return { modes, defaultMode, loading: query.isLoading, label, short, describe };
}

/* ── 归属地 ── */

export function banPlace(ban: Pick<IPBanEntry, "country" | "countryCode" | "region" | "city">) {
  const country = ban.countryCode ? countryName(ban.countryCode) : ban.country;
  const parts = [country, ban.region, ban.city].filter(Boolean);
  // 「中国 · 北京 · 北京」这种省市同名的只留一个
  return parts.filter((part, index) => parts.indexOf(part) === index).join(" · ");
}

/* ── IP 解析 ── */

const IPV4 = /^(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)(\.(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)){3}$/;

/** 合法 IP → 规范写法（IPv6 压缩小写，IPv4 映射地址还原成 IPv4）；不合法返回 null */
export function normalizeIP(raw: string): string | null {
  const text = raw.trim();
  if (!text) return null;
  if (IPV4.test(text)) return text;
  if (!text.includes(":") || /[^0-9a-fA-F:.]/.test(text)) return null;
  try {
    const host = new URL(`http://[${text}]/`).hostname.replace(/^\[|\]$/g, "");
    const mapped = /^::ffff:([0-9a-f]{1,4}):([0-9a-f]{1,4})$/.exec(host);
    if (mapped) {
      const hi = parseInt(mapped[1]!, 16);
      const lo = parseInt(mapped[2]!, 16);
      return `${hi >> 8}.${hi & 255}.${lo >> 8}.${lo & 255}`;
    }
    return host;
  } catch {
    return null;
  }
}

export type ParsedIPList = {
  valid: string[];
  duplicates: string[];
  invalid: string[];
  cidrs: string[];
};

/** 粘贴的一段文字 → 逐个分类。分隔符：换行、逗号、分号、空白（含全角） */
export function parseIPList(text: string): ParsedIPList {
  const out: ParsedIPList = { valid: [], duplicates: [], invalid: [], cidrs: [] };
  const seen = new Set<string>();
  for (const token of text.split(/[\s,;，；、]+/)) {
    const raw = token.trim();
    if (!raw) continue;
    if (raw.includes("/")) {
      out.cidrs.push(raw);
      continue;
    }
    const ip = normalizeIP(raw);
    if (!ip) {
      out.invalid.push(raw);
      continue;
    }
    if (seen.has(ip)) {
      out.duplicates.push(raw);
      continue;
    }
    seen.add(ip);
    out.valid.push(ip);
  }
  return out;
}

/* ── 筛选摘要 ── */

export function describeFilter(filter: BanFilter, modes: BanModes) {
  const parts: string[] = [];
  if (filter.source && filter.source !== "all") parts.push(`来源：${sourceLabel(filter.source)}`);
  if (filter.mode) parts.push(`响应模式：${modes.short(filter.mode)}`);
  if (filter.country) parts.push(`国家：${countryName(filter.country)}`);
  if (filter.keyword) parts.push(`关键字：${filter.keyword}`);
  if (filter.expiringWithin) parts.push("24 小时内到期");
  if (filter.permanent === "true") parts.push("仅永久");
  if (filter.permanent === "false") parts.push("仅限期");
  return parts;
}

/* ── 导出 ── */

function csvCell(value: string | number | null | undefined) {
  const text = value === null || value === undefined ? "" : String(value);
  return /[",\r\n]/.test(text) ? `"${text.replace(/"/g, '""')}"` : text;
}

export function downloadBansCSV(rows: IPBanEntry[], modes: BanModes, name = "ip-bans") {
  const header = ["IP", "状态", "来源", "响应模式", "原因", "触发规则", "触发次数", "国家", "省份", "城市", "运营商", "创建时间", "到期时间", "解除时间", "解除备注"];
  const lines = rows.map((ban) =>
    [
      ban.ip,
      statusLabel(ban.status),
      sourceLabel(ban.source),
      modes.short(ban.mode),
      ban.reason,
      ban.triggerRule,
      ban.triggerCount,
      ban.countryCode ? countryName(ban.countryCode) : ban.country,
      ban.region,
      ban.city,
      ban.isp,
      formatDateTime(ban.createdAt, ""),
      ban.expiresAt ? formatDateTime(ban.expiresAt, "") : "永久",
      ban.revokedAt ? formatDateTime(ban.revokedAt, "") : "",
      ban.revokeNote ?? ""
    ]
      .map(csvCell)
      .join(",")
  );
  // 带 BOM，Excel 直接打开不乱码
  const blob = new Blob(["﻿" + [header.join(","), ...lines].join("\r\n")], { type: "text/csv;charset=utf-8" });
  const url = URL.createObjectURL(blob);
  const link = document.createElement("a");
  const stamp = new Date().toISOString().slice(0, 19).replace(/[-:T]/g, "");
  link.href = url;
  link.download = `${name}-${stamp}.csv`;
  document.body.appendChild(link);
  link.click();
  link.remove();
  window.setTimeout(() => URL.revokeObjectURL(url), 1000);
}

/* ── 地域规则 ── */

export const GEO_SCOPE_META: Record<string, { label: string; hint: string; placeholder: string }> = {
  country: { label: "国家", hint: "两位 ISO 国家代码，如 CN、US", placeholder: "CN" },
  region: { label: "省 / 州", hint: "与 IP 归属地里的省份名称一致；可加国家前缀限定，如 CN-广东", placeholder: "CN-广东" },
  city: { label: "城市", hint: "与 IP 归属地里的城市名称完全一致（不区分大小写）", placeholder: "深圳" },
  asn: { label: "ASN", hint: "自治系统号，写 AS15169 或 15169 均可", placeholder: "AS15169" },
  isp: { label: "运营商", hint: "按名称包含匹配（不区分大小写），如 Amazon 命中 Amazon.com, Inc.", placeholder: "Amazon" }
};

export function geoScopeLabel(scope: string) {
  return GEO_SCOPE_META[scope]?.label ?? scope;
}
