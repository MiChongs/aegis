/**
 * 访问控制页签的客户端校验。与后端 firewall.ParseIPSet（netip.ParseAddr / ParsePrefix）、
 * ulule/limiter 的 NewRateFromFormatted 保持一致，提交前就把格式错误挡下来。
 */

const IPV4 = /^(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)(\.(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)){3}$/;

export function isIPv4(value: string) {
  return IPV4.test(value);
}

export function isIPv6(value: string) {
  if (!value.includes(":") || value.includes("%") || /[^0-9a-fA-F:.]/.test(value)) return false;
  // 浏览器的 URL 解析器按 RFC 4291 校验 IPv6（含 :: 压缩与内嵌 IPv4）
  try {
    return new URL(`http://[${value}]/`).hostname.length > 2;
  } catch {
    return false;
  }
}

/** IP 或 CIDR（IPv4 / IPv6） */
export function isIPOrCIDR(raw: string) {
  const value = raw.trim();
  if (!value) return false;
  const slash = value.indexOf("/");
  if (slash < 0) return isIPv4(value) || isIPv6(value);
  const addr = value.slice(0, slash);
  const bits = value.slice(slash + 1);
  if (!/^\d{1,3}$/.test(bits)) return false;
  const n = Number(bits);
  if (isIPv4(addr)) return n >= 0 && n <= 32;
  if (isIPv6(addr)) return n >= 0 && n <= 128;
  return false;
}

export function normalizeIP(raw: string) {
  const value = raw.trim();
  return value.includes(":") ? value.toLowerCase() : value;
}

/* ── 限流：ulule 格式 "1200-M" ── */

export const RATE_UNITS = [
  { value: "S", label: "秒", word: "每秒" },
  { value: "M", label: "分钟", word: "每分钟" },
  { value: "H", label: "小时", word: "每小时" },
  { value: "D", label: "天", word: "每天" }
] as const;

export type RateUnit = (typeof RATE_UNITS)[number]["value"];

export function parseRate(raw: string): { count: number; unit: RateUnit } | null {
  const match = /^\s*(\d+)\s*-\s*([SMHD])\s*$/i.exec(raw ?? "");
  if (!match) return null;
  return { count: Number(match[1]), unit: match[2].toUpperCase() as RateUnit };
}

export function formatRate(count: number, unit: RateUnit) {
  return `${Math.max(0, Math.round(count))}-${unit}`;
}

export function describeRate(raw: string) {
  const rate = parseRate(raw);
  if (!rate) return "格式无效";
  const unit = RATE_UNITS.find((item) => item.value === rate.unit);
  return `每 IP ${unit?.word ?? ""} ${rate.count.toLocaleString("zh-CN")} 次`;
}

export function isValidRate(raw: string) {
  const rate = parseRate(raw);
  return Boolean(rate && rate.count > 0);
}

/** 重定向地址：站内路径或 http(s) 绝对地址 */
export function isRedirectURL(raw: string) {
  const value = raw.trim();
  if (!value) return true;
  if (value.startsWith("/") && !value.startsWith("//")) return true;
  try {
    const url = new URL(value);
    return url.protocol === "http:" || url.protocol === "https:";
  } catch {
    return false;
  }
}
