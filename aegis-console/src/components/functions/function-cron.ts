/**
 * 定时触发器的 cron 辅助：人话描述 + 下几次执行时间的预览。
 *
 * 语法与后端一致（robfig/cron v3，五段：分 时 日 月 周，外加 @hourly 这类描述符）。
 * 这里**只做预览**：保存时由后端解析并算出 nextRunAt，以后端为准 ——
 * 前端算错了顶多是预览不准，不会让触发器在错误的时刻跑起来。
 *
 * 日与周的组合沿用 robfig 的规则：两段里只要有一段以 `*` / `?` 开头就取交集，
 * 否则取并集（与 Vixie cron 相同）。
 */

export const CRON_PRESETS: Array<{ label: string; cron: string }> = [
  { label: "每 5 分钟", cron: "*/5 * * * *" },
  { label: "每 15 分钟", cron: "*/15 * * * *" },
  { label: "每小时", cron: "0 * * * *" },
  { label: "每天 0 点", cron: "0 0 * * *" },
  { label: "每天 9 点", cron: "0 9 * * *" },
  { label: "工作日 9 点", cron: "0 9 * * 1-5" },
  { label: "每周一 0 点", cron: "0 0 * * 1" },
  { label: "每月 1 日 0 点", cron: "0 0 1 * *" }
];

export const TIMEZONES: Array<{ value: string; label: string }> = [
  { value: "Asia/Shanghai", label: "北京时间（Asia/Shanghai）" },
  { value: "Asia/Hong_Kong", label: "香港（Asia/Hong_Kong）" },
  { value: "Asia/Taipei", label: "台北（Asia/Taipei）" },
  { value: "Asia/Tokyo", label: "东京（Asia/Tokyo）" },
  { value: "Asia/Singapore", label: "新加坡（Asia/Singapore）" },
  { value: "UTC", label: "协调世界时（UTC）" },
  { value: "Europe/London", label: "伦敦（Europe/London）" },
  { value: "Europe/Berlin", label: "柏林（Europe/Berlin）" },
  { value: "America/New_York", label: "纽约（America/New_York）" },
  { value: "America/Los_Angeles", label: "洛杉矶（America/Los_Angeles）" }
];

export const DEFAULT_TIMEZONE = "Asia/Shanghai";

const DESCRIPTORS: Record<string, string> = {
  "@yearly": "0 0 1 1 *",
  "@annually": "0 0 1 1 *",
  "@monthly": "0 0 1 * *",
  "@weekly": "0 0 * * 0",
  "@daily": "0 0 * * *",
  "@midnight": "0 0 * * *",
  "@hourly": "0 * * * *"
};

const MONTH_NAMES = ["JAN", "FEB", "MAR", "APR", "MAY", "JUN", "JUL", "AUG", "SEP", "OCT", "NOV", "DEC"];
const DOW_NAMES = ["SUN", "MON", "TUE", "WED", "THU", "FRI", "SAT"];
const DOW_LABELS = ["日", "一", "二", "三", "四", "五", "六"];

type Field = { values: Set<number>; star: boolean; raw: string };
type Parsed = { minute: Field; hour: Field; dom: Field; month: Field; dow: Field };

function parseValue(token: string, min: number, max: number, names?: string[]) {
  const upper = token.toUpperCase();
  if (names) {
    const index = names.indexOf(upper);
    if (index >= 0) return index + min;
  }
  if (!/^\d+$/.test(token)) throw new Error(`无法识别「${token}」`);
  const value = Number(token);
  if (value < min || value > max) throw new Error(`${token} 超出 ${min}-${max}`);
  return value;
}

function parseField(raw: string, min: number, max: number, names?: string[]): Field {
  const values = new Set<number>();
  let star = false;
  for (const part of raw.split(",")) {
    if (!part) throw new Error("存在空的列表项");
    const [rangePart, stepPart] = part.split("/");
    const step = stepPart === undefined ? 1 : Number(stepPart);
    if (!Number.isInteger(step) || step <= 0) throw new Error(`步长「${stepPart}」无效`);
    let start: number;
    let end: number;
    if (rangePart === "*" || rangePart === "?") {
      start = min;
      end = max;
      star = true;
    } else if (rangePart.includes("-")) {
      const [a, b] = rangePart.split("-");
      start = parseValue(a, min, max, names);
      end = parseValue(b, min, max, names);
      if (start > end) throw new Error(`范围「${rangePart}」起点大于终点`);
    } else {
      start = parseValue(rangePart, min, max, names);
      // 「5/10」表示从 5 开始每 10 一次，直到上界
      end = stepPart === undefined ? start : max;
    }
    for (let value = start; value <= end; value += step) values.add(value);
  }
  return { values, star, raw };
}

export type CronParseResult =
  | { ok: true; parsed: Parsed | null; every?: string }
  | { ok: false; error: string };

export function parseCron(expression: string): CronParseResult {
  const spec = expression.trim();
  if (!spec) return { ok: false, error: "请填写 cron 表达式" };
  if (spec.startsWith("@every ")) {
    const every = spec.slice(7).trim();
    if (!/^(\d+(\.\d+)?(ns|us|µs|ms|s|m|h))+$/.test(every)) return { ok: false, error: "@every 后应为时长，如 1h30m" };
    return { ok: true, parsed: null, every };
  }
  const normalized = DESCRIPTORS[spec.toLowerCase()] ?? spec;
  if (normalized.startsWith("@")) return { ok: false, error: `未知描述符 ${spec}` };
  const parts = normalized.split(/\s+/);
  if (parts.length !== 5) return { ok: false, error: "需要 5 段：分 时 日 月 周" };
  try {
    return {
      ok: true,
      parsed: {
        minute: parseField(parts[0], 0, 59),
        hour: parseField(parts[1], 0, 23),
        dom: parseField(parts[2], 1, 31),
        month: parseField(parts[3], 1, 12, MONTH_NAMES),
        dow: parseField(parts[4], 0, 6, DOW_NAMES)
      }
    };
  } catch (error) {
    return { ok: false, error: error instanceof Error ? error.message : "表达式无效" };
  }
}

// ── 人话描述 ──

const pad = (value: number) => String(value).padStart(2, "0");

/** 一段数值列表的写法：连续区间写成「a 至 b」，其余用顿号 */
function joinValues(values: number[], render: (value: number) => string) {
  const sorted = [...values].sort((a, b) => a - b);
  const runs: string[] = [];
  let i = 0;
  while (i < sorted.length) {
    let j = i;
    while (j + 1 < sorted.length && sorted[j + 1] === sorted[j] + 1) j++;
    if (j - i >= 2) runs.push(`${render(sorted[i])}至${render(sorted[j])}`);
    else for (let k = i; k <= j; k++) runs.push(render(sorted[k]));
    i = j + 1;
  }
  return runs.join("、");
}

function single(field: Field) {
  return !field.star && field.values.size === 1 ? [...field.values][0] : null;
}

function stepOf(field: Field) {
  const match = /^\*\/(\d+)$/.exec(field.raw);
  return match ? Number(match[1]) : null;
}

export function describeCron(expression: string): string | null {
  const result = parseCron(expression);
  if (!result.ok) return null;
  if (result.every) return `每隔 ${result.every}`;
  const p = result.parsed;
  if (!p) return null;

  // 日期部分
  let day = "";
  const allDays = p.dom.star && p.dow.star;
  if (!p.month.star) day += `每年 ${joinValues([...p.month.values], (v) => String(v))} 月`;
  if (!p.dom.star && p.dow.star) {
    day += `${p.month.star ? "每月 " : " "}${joinValues([...p.dom.values], (v) => String(v))} 日`;
  } else if (p.dom.star && !p.dow.star) {
    const dows = [...p.dow.values].sort((a, b) => a - b);
    day += (day ? " " : "") + (dows.join(",") === "1,2,3,4,5" ? "工作日" : dows.join(",") === "0,6" ? "周末" : `每周${joinValues(dows, (v) => DOW_LABELS[v])}`);
  } else if (!p.dom.star && !p.dow.star) {
    day += `${day ? " " : ""}每月 ${joinValues([...p.dom.values], (v) => String(v))} 日或每周${joinValues([...p.dow.values], (v) => DOW_LABELS[v])}`;
  }

  const minute = single(p.minute);
  const hour = single(p.hour);
  const minuteStep = stepOf(p.minute);
  const hourStep = stepOf(p.hour);

  let time: string | null = null;
  if (minute !== null && hour !== null) {
    time = `${pad(hour)}:${pad(minute)}`;
    if (allDays && p.month.star) return `每天 ${time}`;
  } else if (minute !== null && p.hour.raw === "*") {
    time = minute === 0 ? "每小时整点" : `每小时第 ${minute} 分`;
  } else if (minute !== null && hourStep) {
    time = minute === 0 ? `每 ${hourStep} 小时整点` : `每 ${hourStep} 小时的第 ${minute} 分`;
  } else if (minute !== null && !p.hour.star) {
    time = `${joinValues([...p.hour.values], (v) => String(v))} 点${minute === 0 ? "整" : ` ${pad(minute)} 分`}`;
  } else if (minuteStep && p.hour.raw === "*") {
    time = `每 ${minuteStep} 分钟`;
  } else if (p.minute.raw === "*" && p.hour.raw === "*") {
    time = "每分钟";
  }
  if (!time) return null;
  if (!day) return allDays && minute !== null && hour !== null ? `每天 ${time}` : time;
  return `${day} ${time}`;
}

// ── 下几次执行（按时区的墙上时间逐步推进） ──

type ZonedParts = { month: number; day: number; hour: number; minute: number; weekday: number };

const WEEKDAY_INDEX: Record<string, number> = { Sun: 0, Mon: 1, Tue: 2, Wed: 3, Thu: 4, Fri: 5, Sat: 6 };

function zonedFormatter(timezone: string) {
  return new Intl.DateTimeFormat("en-US", {
    timeZone: timezone,
    hourCycle: "h23",
    month: "numeric",
    day: "numeric",
    hour: "numeric",
    minute: "numeric",
    weekday: "short"
  });
}

function zonedParts(formatter: Intl.DateTimeFormat, at: Date): ZonedParts {
  const parts: Record<string, string> = {};
  for (const part of formatter.formatToParts(at)) parts[part.type] = part.value;
  return {
    month: Number(parts.month),
    day: Number(parts.day),
    hour: Number(parts.hour) % 24,
    minute: Number(parts.minute),
    weekday: WEEKDAY_INDEX[parts.weekday] ?? 0
  };
}

export function nextCronRuns(expression: string, timezone: string, count = 3, from = new Date()): Date[] {
  const result = parseCron(expression);
  if (!result.ok || !result.parsed) return [];
  const p = result.parsed;
  let formatter: Intl.DateTimeFormat;
  try {
    formatter = zonedFormatter(timezone || DEFAULT_TIMEZONE);
  } catch {
    return [];
  }
  const MINUTE = 60 * 1000;
  const runs: Date[] = [];
  let t = Math.floor(from.getTime() / MINUTE) * MINUTE + MINUTE;
  // 上界约等于「逐日跳过两年」：表达式永远不触发时（如 2 月 30 日）也能停下
  for (let guard = 0; guard < 20000 && runs.length < count; guard++) {
    const z = zonedParts(formatter, new Date(t));
    const domMatch = p.dom.values.has(z.day);
    const dowMatch = p.dow.values.has(z.weekday);
    const dayMatch = p.dom.star || p.dow.star ? domMatch && dowMatch : domMatch || dowMatch;
    if (!p.month.values.has(z.month) || !dayMatch) {
      t += (24 * 60 - (z.hour * 60 + z.minute)) * MINUTE;
      continue;
    }
    if (!p.hour.values.has(z.hour)) {
      t += (60 - z.minute) * MINUTE;
      continue;
    }
    if (!p.minute.values.has(z.minute)) {
      t += MINUTE;
      continue;
    }
    runs.push(new Date(t));
    t += MINUTE;
  }
  return runs;
}

/** 按触发器所在时区显示一个时刻，不在同一时区时把时区缩写也带上 */
export function formatInZone(value: Date | string, timezone?: string) {
  const date = typeof value === "string" ? new Date(value) : value;
  try {
    return date.toLocaleString("zh-CN", {
      timeZone: timezone || DEFAULT_TIMEZONE,
      month: "numeric",
      day: "numeric",
      weekday: "short",
      hour: "2-digit",
      minute: "2-digit"
    });
  } catch {
    return date.toLocaleString("zh-CN");
  }
}
