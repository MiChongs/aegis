import { CHARSET_META } from "@/components/apps/card-key/card-key-shared";
import type {
  CardKeyAudience,
  CardKeyBatch,
  CardKeyCatalog,
  CardKeyCharset,
  CardKeyKind,
  CardKeyReward,
  CardKeyValidityMode,
  GenerateCardKeysPayload,
  UpdateCardKeyBatchPayload
} from "@/lib/api/card-key";
import { fromLocalInputValue, toLocalInputValue } from "@/lib/datetime";

/**
 * 批次编辑器的表单模型：草稿、校验、提交载荷。
 *
 * 校验照抄后端 `normalizeGenerateInput` / `ValidateRewards` 的规则与说法，
 * 让大多数错误在提交前就说清楚；后端的报错仍然原样 toast，那是最终的判据。
 * 数字项一律按字符串存：存 number 的话输入框清不空（`Number("") === 0`）。
 */

export type EditorMode = "create" | "import" | "edit";

export type RewardRow = {
  /** 本地行号，只用于 React key */
  key: number;
  type: string;
  amount: string;
  money: string;
  refId: number;
  feature: string;
};

export type BatchForm = {
  name: string;
  kind: CardKeyKind;
  /** 兑换卡的用法：一卡一用 / 活动码 */
  usage: "single" | "multi";
  maxUses: string;
  count: string;
  importText: string;
  codePrefix: string;
  segments: string;
  segmentLength: string;
  charset: CardKeyCharset;
  validityMode: CardKeyValidityMode;
  validityDays: string;
  validUntil: string;
  maxDevices: string;
  selfUnbindLimit: string;
  perUserLimit: string;
  audience: CardKeyAudience;
  startsAt: string;
  channel: string;
  faceValue: string;
  remark: string;
  rewards: RewardRow[];
};

export const LIMITS = {
  name: 64,
  channel: 64,
  prefix: 16,
  maxCount: 10000,
  maxUses: 1_000_000,
  maxDevices: 64,
  perUserLimit: 100_000,
  selfUnbindLimit: 100,
  validityDays: 36500,
  faceValue: 1_000_000,
  money: 1_000_000,
  importMin: 4,
  importMax: 64
} as const;

export function seedForm(): BatchForm {
  return {
    name: "",
    kind: "redeem",
    usage: "single",
    maxUses: "100",
    count: "100",
    importText: "",
    codePrefix: "",
    segments: "4",
    segmentLength: "4",
    charset: "safe",
    validityMode: "permanent",
    validityDays: "30",
    validUntil: "",
    maxDevices: "1",
    selfUnbindLimit: "0",
    perUserLimit: "0",
    audience: "all",
    startsAt: "",
    channel: "",
    faceValue: "",
    remark: "",
    rewards: []
  };
}

function rowsOf(rewards: CardKeyReward[]): RewardRow[] {
  return rewards.map((reward, index) => ({
    key: index + 1,
    type: reward.type,
    amount: String(reward.amount ?? 0),
    money: reward.money ?? "",
    refId: reward.refId ?? 0,
    feature: reward.feature ?? ""
  }));
}

/**
 * 从已有批次起草。`template` 为「以此为模板新建」：名称加后缀、数量回到默认、
 * 不带统一到期时间（多半已经过了）；`edit` 原样带入。
 */
export function formFromBatch(batch: CardKeyBatch, mode: "edit" | "template"): BatchForm {
  const template = mode === "template";
  const faceValue = Number(batch.faceValue);
  return {
    ...seedForm(),
    name: template ? `${batch.name} 副本` : batch.name,
    kind: batch.kind,
    usage: batch.maxUses > 1 ? "multi" : "single",
    maxUses: String(batch.maxUses > 1 ? batch.maxUses : 100),
    count: template ? "100" : String(batch.total),
    codePrefix: batch.codePrefix ?? "",
    segments: String(batch.segments),
    segmentLength: String(batch.segmentLength),
    charset: batch.charset,
    validityMode: batch.validityMode,
    validityDays: String(batch.validityDays || 30),
    validUntil: template ? "" : toLocalInputValue(batch.validUntil),
    maxDevices: String(batch.maxDevices),
    selfUnbindLimit: String(batch.selfUnbindLimit),
    perUserLimit: String(batch.perUserLimit),
    audience: batch.audience,
    startsAt: template ? "" : toLocalInputValue(batch.startsAt),
    channel: batch.channel ?? "",
    faceValue: Number.isFinite(faceValue) && faceValue > 0 ? faceValue.toFixed(2) : "",
    remark: batch.remark ?? "",
    rewards: rowsOf(batch.rewards)
  };
}

export function nextRowKey(rows: RewardRow[]) {
  return rows.reduce((max, row) => Math.max(max, row.key), 0) + 1;
}

// ── 卡面 ──

/** 与后端 cardkey.NormalizeCode 同一口径：大写、只留 A–Z0–9、分隔符收成一个 `-`、去首尾分隔符 */
export function normalizeCode(code: string) {
  let out = "";
  let pendingDash = false;
  for (const char of code.trim().toUpperCase()) {
    if (/[A-Z0-9]/.test(char)) {
      if (pendingDash && out) out += "-";
      pendingDash = false;
      out += char;
    } else if (/[-_ —－]/.test(char)) {
      pendingDash = true;
    }
  }
  return out;
}

/** 导入文本 → 卡面。逗号、分号、换行都算分隔；归一化后相同的视为重复 */
export function parseImportCodes(text: string) {
  const seen = new Set<string>();
  const codes: string[] = [];
  const duplicates: string[] = [];
  const invalid: string[] = [];
  for (const raw of text.split(/[\n\r,;，；\t]+/)) {
    const code = normalizeCode(raw);
    if (!code) continue;
    const letters = code.replaceAll("-", "").length;
    if (letters < LIMITS.importMin || code.length > LIMITS.importMax) {
      invalid.push(code);
      continue;
    }
    if (seen.has(code)) {
      duplicates.push(code);
      continue;
    }
    seen.add(code);
    codes.push(code);
  }
  return { codes, duplicates, invalid };
}

/** 格式能表达的组合数，与后端 checkCodeCapacity 同一算法（装得下 count 的 4 倍才算够） */
export function codeCapacity(charset: CardKeyCharset, segments: number, segmentLength: number) {
  return Math.pow(CHARSET_META[charset].alphabet.length, segments * segmentLength);
}

export function capacityEnough(form: BatchForm, extra = 0) {
  const segments = Number(form.segments) || 4;
  const length = Number(form.segmentLength) || 4;
  const count = (Number(form.count) || 0) + extra;
  return count <= codeCapacity(form.charset, segments, length) / 4;
}

/**
 * 卡面样例：只用于预览格式，不是会被生成的那张卡。
 * 用确定性的伪随机（按格式取种子），渲染期没有不纯调用，同一格式每次显示同一张样例。
 */
export function sampleCode(form: BatchForm) {
  const alphabet = CHARSET_META[form.charset].alphabet;
  const segments = clamp(Number(form.segments) || 4, 1, 8);
  const length = clamp(Number(form.segmentLength) || 4, 3, 12);
  let seed = 7 + segments * 31 + length * 17 + alphabet.length;
  const next = () => {
    seed = (seed * 1103515245 + 12345) % 2147483648;
    return seed;
  };
  const parts: string[] = [];
  const prefix = form.codePrefix.trim().toUpperCase();
  if (prefix) parts.push(prefix);
  for (let index = 0; index < segments; index += 1) {
    let part = "";
    for (let offset = 0; offset < length; offset += 1) {
      part += alphabet[next() % alphabet.length];
    }
    parts.push(part);
  }
  return parts.join("-");
}

function clamp(value: number, min: number, max: number) {
  return Math.min(max, Math.max(min, value));
}

// ── 校验 ──

export function validateForm(
  form: BatchForm,
  mode: EditorMode,
  catalog?: CardKeyCatalog | null,
  original?: BatchForm
): string[] {
  const issues: string[] = [];
  const name = form.name.trim();
  if (!name) issues.push("批次名称不能为空");
  else if (Array.from(name).length > LIMITS.name) issues.push("批次名称过长");

  if (mode !== "edit") {
    if (mode === "import") {
      const parsed = parseImportCodes(form.importText);
      if (parsed.invalid.length) issues.push(`「${parsed.invalid[0]}」太短或太长，卡面需要 4–64 个字符`);
      if (parsed.codes.length === 0) issues.push("没有可导入的卡面");
      if (parsed.codes.length > (catalog?.maxCount ?? LIMITS.maxCount)) issues.push("单次最多导入 10000 张卡密");
    } else {
      const count = Number(form.count);
      const maxCount = catalog?.maxCount ?? LIMITS.maxCount;
      if (!Number.isInteger(count) || count < 1 || count > maxCount) {
        issues.push(`单次生成数量需要在 1–${maxCount} 之间`);
      }
      const prefix = form.codePrefix.trim();
      if (prefix.length > LIMITS.prefix) issues.push("前缀最多 16 个字符");
      if (prefix && !/^[A-Za-z0-9]+$/.test(prefix)) issues.push("前缀只能使用大写字母与数字");
      const segments = Number(form.segments);
      const length = Number(form.segmentLength);
      if (!(segments >= 1 && segments <= 8 && length >= 3 && length <= 12)) {
        issues.push("卡面格式需要在 1–8 段、每段 3–12 位之间");
      } else if (!capacityEnough(form)) {
        issues.push("当前卡面格式能表达的组合太少，装不下这个数量；请增加段数或每段位数");
      }
    }

    if (form.validityMode === "days_from_first_use") {
      const days = Number(form.validityDays);
      if (!Number.isInteger(days) || days < 1 || days > LIMITS.validityDays) {
        issues.push("「激活即计时」需要填写 1–36500 之间的有效天数");
      }
    }
    if (form.kind === "login") {
      const devices = Number(form.maxDevices);
      if (!Number.isInteger(devices) || devices < 1 || devices > LIMITS.maxDevices) {
        issues.push("单卡可绑定设备数需要在 1–64 台之间");
      }
    } else if (form.usage === "multi") {
      const uses = Number(form.maxUses);
      if (!Number.isInteger(uses) || uses < 2 || uses > LIMITS.maxUses) {
        issues.push("活动码的可用人次需要在 2–1000000 之间");
      }
    }
  }

  // 修改时只在到期时间真的改了才校验：一批已经过了统一到期日的卡，改个名字不该被它挡住
  if (form.validityMode === "fixed_until" && (mode !== "edit" || form.validUntil !== original?.validUntil)) {
    const until = fromLocalInputValue(form.validUntil);
    if (!until) issues.push("「统一到期」需要填写到期时间");
    else if (new Date(until).getTime() <= Date.now()) issues.push("到期时间必须晚于当前时间");
  }

  if (Array.from(form.channel.trim()).length > LIMITS.channel) issues.push("渠道名称过长");
  if (form.faceValue.trim()) {
    const face = Number(form.faceValue);
    if (!Number.isFinite(face) || face < 0) issues.push("面值不能为负数");
    else if (face > LIMITS.faceValue) issues.push("面值过大");
  }
  if (form.kind === "redeem") {
    const perUser = Number(form.perUserLimit || 0);
    if (!Number.isInteger(perUser) || perUser < 0 || perUser > LIMITS.perUserLimit) {
      issues.push("每人限兑张数需要在 0–100000 之间（0 为不限）");
    }
  } else {
    const unbind = Number(form.selfUnbindLimit || 0);
    if (!Number.isInteger(unbind) || unbind < 0 || unbind > LIMITS.selfUnbindLimit) {
      issues.push("自助解绑次数需要在 0–100 之间");
    }
  }

  issues.push(...validateRewards(form, catalog));
  return issues;
}

function validateRewards(form: BatchForm, catalog?: CardKeyCatalog | null): string[] {
  const issues: string[] = [];
  const rows = form.rewards;
  if (rows.length === 0) {
    if (form.kind === "redeem") issues.push("至少要配置一项权益");
    return issues;
  }
  const maxRewards = catalog?.maxRewards ?? 8;
  if (rows.length > maxRewards) issues.push(`一张卡最多配置 ${maxRewards} 项权益`);
  const seenTypes = new Set<string>();
  const seenFeatures = new Set<string>();
  for (const row of rows) {
    const spec = catalog?.rewards.find((item) => item.type === row.type);
    if (!spec) continue;
    if (!spec.repeatable && seenTypes.has(row.type)) issues.push(`「${spec.label}」重复配置了两次`);
    seenTypes.add(row.type);
    switch (spec.value) {
      case "amount": {
        const amount = Number(row.amount);
        if (!Number.isInteger(amount) || amount < (spec.min ?? 1) || amount > (spec.max ?? Infinity)) {
          issues.push(`「${spec.label}」需要在 ${spec.min ?? 1}–${spec.max ?? ""} ${spec.unit ?? ""}之间`);
        }
        break;
      }
      case "money": {
        const money = Number(row.money);
        if (!Number.isFinite(money) || money <= 0) issues.push(`「${spec.label}」必须大于 0`);
        else if (money > LIMITS.money) issues.push(`「${spec.label}」单张不能超过 1000000`);
        else if (!/^\d+(\.\d{1,2})?$/.test(row.money.trim())) issues.push(`「${spec.label}」最多两位小数`);
        break;
      }
      case "ref":
        if (!row.refId) issues.push(`「${spec.label}」没有选择具体对象`);
        break;
      case "number_ticket": {
        const amount = Number(row.amount);
        if (!Number.isInteger(amount) || amount < (spec.min ?? 1) || amount > (spec.max ?? Infinity)) {
          issues.push(`「${spec.label}」需要在 ${spec.min ?? 1}–${spec.max ?? ""} 张之间`);
        }
        if (!(row.refId >= 1 && row.refId <= 4)) issues.push(`「${spec.label}」没有选择等级`);
        break;
      }
      case "feature": {
        if (!row.feature) {
          issues.push(`「${spec.label}」没有选择功能`);
          break;
        }
        if (seenFeatures.has(row.feature)) issues.push(`「${spec.label}」里功能「${row.feature}」配置了两次`);
        seenFeatures.add(row.feature);
        const days = Number(row.amount || 0);
        if (!Number.isInteger(days) || days < 0 || days > (spec.max ?? 3650)) {
          issues.push(`「${spec.label}」的天数需要在 0–${spec.max ?? 3650} 之间（0 为永久）`);
        }
        break;
      }
      case "cosmetic": {
        if (!row.refId) issues.push(`「${spec.label}」没有选择装扮`);
        const days = Number(row.amount || 0);
        if (!Number.isInteger(days) || days < 0 || days > (spec.max ?? 3650)) {
          issues.push(`「${spec.label}」的天数需要在 0–${spec.max ?? 3650} 之间（0 为永久）`);
        }
        break;
      }
    }
  }
  return issues;
}

// ── 提交载荷 ──

function rewardsPayload(form: BatchForm, catalog?: CardKeyCatalog | null): CardKeyReward[] {
  return form.rewards.flatMap((row): CardKeyReward[] => {
    const spec = catalog?.rewards.find((item) => item.type === row.type);
    if (!spec) return [];
    switch (spec.value) {
      case "amount":
        return [{ type: row.type, amount: Number(row.amount) }];
      case "money":
        return [{ type: row.type, money: row.money.trim() }];
      case "ref":
        return [{ type: row.type, refId: row.refId }];
      case "number_ticket":
        return [{ type: row.type, amount: Number(row.amount), refId: row.refId }];
      case "feature":
        return [{ type: row.type, feature: row.feature, amount: Number(row.amount || 0) }];
      case "cosmetic":
        return [{ type: row.type, refId: row.refId, amount: Number(row.amount || 0) }];
      default:
        return [];
    }
  });
}

function optionalTime(value: string) {
  return fromLocalInputValue(value) ?? null;
}

export function toGeneratePayload(form: BatchForm, mode: EditorMode, catalog?: CardKeyCatalog | null): GenerateCardKeysPayload {
  const login = form.kind === "login";
  const payload: GenerateCardKeysPayload = {
    name: form.name.trim(),
    kind: form.kind,
    remark: form.remark.trim(),
    rewards: rewardsPayload(form, catalog),
    validityMode: form.validityMode,
    validityDays: form.validityMode === "days_from_first_use" ? Number(form.validityDays) : 0,
    validUntil: form.validityMode === "fixed_until" ? optionalTime(form.validUntil) : null,
    startsAt: optionalTime(form.startsAt),
    channel: form.channel.trim(),
    faceValue: form.faceValue.trim() || "0",
    maxDevices: login ? Number(form.maxDevices) : 1,
    selfUnbindLimit: login ? Number(form.selfUnbindLimit || 0) : 0,
    maxUses: !login && form.usage === "multi" ? Number(form.maxUses) : 1,
    perUserLimit: login ? 0 : Number(form.perUserLimit || 0),
    audience: login ? "all" : form.audience
  };
  if (mode === "import") {
    payload.codes = parseImportCodes(form.importText).codes;
  } else {
    payload.count = Number(form.count);
    payload.codePrefix = form.codePrefix.trim().toUpperCase();
    payload.segments = Number(form.segments);
    payload.segmentLength = Number(form.segmentLength);
    payload.charset = form.charset;
  }
  return payload;
}

export function toUpdatePayload(
  form: BatchForm,
  catalog?: CardKeyCatalog | null,
  original?: BatchForm
): UpdateCardKeyBatchPayload {
  const login = form.kind === "login";
  return {
    name: form.name.trim(),
    remark: form.remark.trim(),
    channel: form.channel.trim(),
    faceValue: form.faceValue.trim() || "0",
    rewards: rewardsPayload(form, catalog),
    perUserLimit: login ? 0 : Number(form.perUserLimit || 0),
    audience: login ? "all" : form.audience,
    startsAt: optionalTime(form.startsAt),
    // 不改就不传：后端收到 null 时保留原值
    validUntil:
      form.validityMode === "fixed_until" && form.validUntil !== original?.validUntil ? optionalTime(form.validUntil) : null,
    selfUnbindLimit: login ? Number(form.selfUnbindLimit || 0) : 0
  };
}
