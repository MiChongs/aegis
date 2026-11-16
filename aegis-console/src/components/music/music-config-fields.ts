import type { MusicConfig, MusicConfigInput } from "@/lib/api/music";

/**
 * 音乐推荐配置项目录：配置表单与训练详情里的「参数快照」共用一份，叫法与取值范围只在这里定义。
 *
 * 取值范围照抄契约「PUT /config 校验」：前端先拦一次，越界时保存按钮不可用、字段下方给出范围；
 * 后端仍会再校验（40000，message 指出哪一项）。
 */

export type NumberFieldSpec = {
  kind: "number";
  /** 点分路径，如 `weights.complete` */
  path: string;
  label: string;
  min: number;
  max: number;
  step: number;
  unit?: string;
};

export type BoolFieldSpec = { kind: "bool"; path: string; label: string };
export type CronFieldSpec = { kind: "cron"; path: string; label: string };

export type FieldSpec = NumberFieldSpec | BoolFieldSpec | CronFieldSpec;

export type FieldGroup = { key: string; title: string; fields: FieldSpec[] };

const n = (path: string, label: string, min: number, max: number, step: number, unit?: string): NumberFieldSpec => ({
  kind: "number",
  path,
  label,
  min,
  max,
  step,
  unit
});

export const CONFIG_GROUPS: FieldGroup[] = [
  {
    key: "switch",
    title: "开关",
    fields: [
      { kind: "bool", path: "enabled", label: "推荐服务" },
      { kind: "bool", path: "collectEnabled", label: "行为采集" }
    ]
  },
  {
    key: "window",
    title: "采集与窗口",
    fields: [
      n("windowDays", "训练取近多少天的行为", 7, 730, 1, "天"),
      n("halfLifeDays", "行为分量减半的时间", 1, 365, 1, "天"),
      n("minItemUsers", "作品至少被多少人听过才进模型", 1, 50, 1, "人")
    ]
  },
  {
    key: "weights",
    title: "事件权重",
    fields: [
      n("weights.complete", "听完一首", -20, 20, 0.1),
      n("weights.partial", "听了一部分", -20, 20, 0.1),
      n("weights.skip", "开头就跳过", -20, 20, 0.1),
      n("weights.like", "收藏", -20, 20, 0.1),
      n("weights.dislike", "不感兴趣", -20, 20, 0.1),
      n("weights.playlistAdd", "加入歌单", -20, 20, 0.1),
      n("weights.download", "下载", -20, 20, 0.1),
      n("weights.share", "分享", -20, 20, 0.1),
      n("weights.searchClick", "点击搜索结果", -20, 20, 0.1),
      n("weights.click", "点击歌曲", -20, 20, 0.1),
      n("weights.seekBack", "大幅拖动进度", -20, 20, 0.1)
    ]
  },
  {
    key: "als",
    title: "口味模型（ALS）",
    fields: [
      n("als.factors", "口味维度", 8, 256, 1),
      n("als.iterations", "迭代次数", 1, 50, 1, "次"),
      n("als.regularization", "正则强度", 0.0001, 10, 0.0001),
      n("als.alpha", "置信度系数", 1, 200, 1)
    ]
  },
  {
    key: "itemcf",
    title: "相似歌曲（ItemCF）",
    fields: [n("itemcf.neighbors", "每首保留的相似作品数", 5, 200, 1, "首"), n("itemcf.sessionBoost", "同一次收听里相邻播放的加成", 0, 2, 0.05)]
  },
  {
    key: "blend",
    title: "融合权重",
    fields: [
      n("blend.als", "口味模型", 0, 5, 0.05),
      n("blend.itemcf", "相似歌曲", 0, 5, 0.05),
      n("blend.artist", "常听歌手", 0, 5, 0.05),
      n("blend.trending", "近期热门", 0, 5, 0.05),
      n("blend.explore", "探索", 0, 5, 0.05)
    ]
  },
  {
    key: "diversity",
    title: "多样性",
    fields: [
      n("artistCap", "前 20 首里同一歌手最多", 1, 10, 1, "首"),
      n("recentExcludeDays", "近期播放过的降权", 0, 30, 1, "天"),
      n("exploreRatio", "探索结果占比", 0, 0.5, 0.01)
    ]
  },
  {
    key: "publish",
    title: "定时与发布",
    fields: [
      { kind: "cron", path: "schedule", label: "定时训练" },
      { kind: "bool", path: "autoPublish", label: "达标后自动发布" },
      n("minLift", "自动发布要求 NDCG@20 不低于基线的倍数", 0, 3, 0.05, "倍")
    ]
  }
];

export const CRON_PRESETS = [
  { value: "0 4 * * *", label: "每天 4 点" },
  { value: "0 4,16 * * *", label: "每天 4 点和 16 点" },
  { value: "0 */6 * * *", label: "每 6 小时" },
  { value: "0 4 * * 1", label: "每周一 4 点" },
  { value: "", label: "不定时" }
] as const;

export function cronLabel(value: string) {
  return CRON_PRESETS.find((p) => p.value === value)?.label ?? value;
}

/** 五段 cron 或空（不定时）。只做形状校验，语义由后端判定 */
export function cronError(value: string): string | undefined {
  const v = value.trim();
  if (!v) return undefined;
  const parts = v.split(/\s+/);
  if (parts.length !== 5) return "需要五段：分 时 日 月 星期";
  if (!parts.every((p) => /^[0-9A-Za-z*/,\-?]+$/.test(p))) return "含有不支持的字符";
  return undefined;
}

export function getPath(source: unknown, path: string): unknown {
  return path.split(".").reduce<unknown>((acc, key) => (acc && typeof acc === "object" ? (acc as Record<string, unknown>)[key] : undefined), source);
}

export function setPath<T>(source: T, path: string, value: unknown): T {
  const [head, ...rest] = path.split(".");
  const obj = (source ?? {}) as Record<string, unknown>;
  if (!head) return source;
  return { ...obj, [head]: rest.length ? setPath(obj[head], rest.join("."), value) : value } as T;
}

export function numberError(spec: NumberFieldSpec, value: unknown): string | undefined {
  const v = typeof value === "number" ? value : Number.NaN;
  if (!Number.isFinite(v)) return "请输入数字";
  if (v < spec.min || v > spec.max) return `范围 ${spec.min} 到 ${spec.max}`;
  return undefined;
}

/** 全部字段的校验结果：path → 错误 */
export function validateConfig(config: MusicConfigInput): Record<string, string> {
  const errors: Record<string, string> = {};
  for (const group of CONFIG_GROUPS) {
    for (const field of group.fields) {
      const value = getPath(config, field.path);
      const error = field.kind === "number" ? numberError(field, value) : field.kind === "cron" ? cronError(String(value ?? "")) : undefined;
      if (error) errors[field.path] = error;
    }
  }
  return errors;
}

export function toConfigInput(config: MusicConfig): MusicConfigInput {
  // 去掉服务端写的两个字段，其余原样提交（含将来后端新增、前端尚未展示的字段）
  const { updatedAt, updatedBy, ...rest } = config;
  return rest;
}

/** 数值的展示：小数位跟着步长走 */
export function formatFieldValue(spec: FieldSpec, value: unknown): string {
  if (spec.kind === "bool") return value ? "开启" : "关闭";
  if (spec.kind === "cron") return cronLabel(String(value ?? "")) || "不定时";
  const v = typeof value === "number" ? value : Number.NaN;
  if (!Number.isFinite(v)) return "—";
  const decimals = spec.step >= 1 ? 0 : Math.min(4, String(spec.step).split(".")[1]?.length ?? 2);
  return `${Number(v.toFixed(decimals))}${spec.unit ? ` ${spec.unit}` : ""}`;
}
