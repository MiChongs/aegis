import { MUSIC_CONFIG_V2_DEFAULTS, MUSIC_CONFIG_V3_DEFAULTS, type MusicConfig, type MusicConfigInput } from "@/lib/api/music";

/**
 * 音乐推荐配置项目录：配置表单与训练详情里的「参数快照」共用一份，叫法与取值范围只在这里定义。
 *
 * 取值范围照抄契约「PUT /config 校验」（第二期的几组见 docs/music-personalization-2.md「应用配置增量」，
 * 第三期的深度模型、内容理解与 blend.seq 见 docs/music-personalization-3.md「五、管理端增量」）：
 * 前端先拦一次，越界时保存按钮不可用、字段下方给出范围；后端仍会再校验（40000，message 指出哪一项）。
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
/** 只能取几个固定数值（第三期的表示维度、注意力头数） */
export type SelectFieldSpec = { kind: "select"; path: string; label: string; options: number[]; unit?: string };

export type FieldSpec = NumberFieldSpec | BoolFieldSpec | CronFieldSpec | SelectFieldSpec;

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
      n("blend.explore", "探索", 0, 5, 0.05),
      n("blend.seq", "序列模型", 0, 5, 0.05)
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
  },
  {
    key: "ranker",
    title: "实时口味与精排",
    fields: [
      { kind: "bool", path: "foldIn", label: "实时折入（训练间隙按最新行为更新口味）" },
      { kind: "bool", path: "ranker.enabled", label: "精排（达标后才会真正启用）" },
      n("ranker.minSamples", "精排至少需要的样本数", 200, 1000000, 1, "条"),
      n("ranker.l2", "精排正则强度", 0, 1, 0.0001),
      n("ranker.epochs", "精排训练轮数", 1, 50, 1, "轮")
    ]
  },
  {
    key: "fatigue",
    title: "曝光疲劳与暂不推荐",
    fields: [
      n("fatigue.threshold", "近 7 天展示未点击达到多少次开始降权", 1, 20, 1, "次"),
      n("fatigue.decay", "每多一次的衰减系数", 0.1, 1, 0.05),
      n("suppressDays", "「听腻了」之后暂不推荐", 1, 180, 1, "天")
    ]
  },
  {
    key: "scenes",
    title: "每周雷达、风格歌单与智能续播",
    fields: [
      n("discover.size", "每周雷达的歌曲数", 10, 50, 1, "首"),
      n("discover.popularityPenalty", "每周雷达偏向冷门的强度", 0, 2, 0.05),
      n("mixes.max", "风格歌单最多几个（0 为关闭）", 0, 10, 1, "个"),
      n("mixes.size", "每个风格歌单的歌曲数", 10, 50, 1, "首"),
      n("autoplay.size", "智能续播每批的歌曲数", 5, 30, 1, "首")
    ]
  },
  {
    key: "deep",
    title: "深度序列模型",
    fields: [
      { kind: "bool", path: "deep.enabled", label: "深度模型（口味模型训练成功后自动训练）" },
      { kind: "select", path: "deep.d", label: "表示维度", options: [32, 48, 64, 96, 128] },
      n("deep.layers", "Transformer 层数", 1, 4, 1, "层"),
      { kind: "select", path: "deep.heads", label: "注意力头数（需能整除表示维度）", options: [1, 2, 4], unit: "个" },
      n("deep.maxLen", "序列最多取最近几首", 10, 200, 1, "首"),
      n("deep.epochs", "最多训练轮数", 1, 100, 1, "轮"),
      n("deep.minItemCount", "作品至少被多少人听过才有独立 ID 向量", 1, 50, 1, "人"),
      n("deep.minLift", "自动发布要求 NDCG@20 不低于热度基线的倍数", 0, 3, 0.05, "倍")
    ]
  },
  {
    key: "content",
    title: "内容理解与曲库扩展",
    fields: [
      { kind: "bool", path: "content.enabled", label: "内容理解（大模型打标签与语义向量）" },
      n("content.dailyLimit", "每天最多给多少首新歌打标签", 100, 200000, 1, "首"),
      { kind: "bool", path: "content.llmExpand", label: "大模型曲库扩展（行为少的人补推曲库外的歌）" },
      n("content.expandRatio", "一页里扩展歌最多占比", 0, 0.5, 0.01)
    ]
  }
];

/** 只属于深度模型与内容理解的分组：口味模型的参数快照里不列 */
export const V3_GROUP_KEYS = new Set(["deep", "content"]);

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

export function selectError(spec: SelectFieldSpec, value: unknown): string | undefined {
  if (typeof value !== "number" || !spec.options.includes(value)) return `只能选 ${spec.options.join("、")}`;
  return undefined;
}

/** 全部字段的校验结果：path → 错误 */
export function validateConfig(config: MusicConfigInput): Record<string, string> {
  const errors: Record<string, string> = {};
  for (const group of CONFIG_GROUPS) {
    for (const field of group.fields) {
      const value = getPath(config, field.path);
      const error =
        field.kind === "number"
          ? numberError(field, value)
          : field.kind === "select"
            ? selectError(field, value)
            : field.kind === "cron"
              ? cronError(String(value ?? ""))
              : undefined;
      if (error) errors[field.path] = error;
    }
  }
  // 跨字段：注意力头数要能整除表示维度（每个头 d / heads 维）
  const d = config.deep?.d;
  const heads = config.deep?.heads;
  if (!errors["deep.heads"] && !errors["deep.d"] && typeof d === "number" && typeof heads === "number" && heads > 0 && d % heads !== 0) {
    errors["deep.heads"] = `需要能整除表示维度 ${d}`;
  }
  return errors;
}

export function toConfigInput(config: MusicConfig): MusicConfigInput {
  // 去掉服务端写的两个字段，其余原样提交（含将来后端新增、前端尚未展示的字段）
  const { updatedAt, updatedBy, ...rest } = config;
  // 第二期字段在服务端升级前缺省：按契约默认值补齐，免得表单出现空值而无法保存
  const d = MUSIC_CONFIG_V2_DEFAULTS;
  return {
    ...rest,
    foldIn: rest.foldIn ?? d.foldIn,
    ranker: { ...d.ranker, ...rest.ranker },
    fatigue: { ...d.fatigue, ...rest.fatigue },
    suppressDays: rest.suppressDays ?? d.suppressDays,
    discover: { ...d.discover, ...rest.discover },
    mixes: { ...d.mixes, ...rest.mixes },
    autoplay: { ...d.autoplay, ...rest.autoplay },
    // 第三期字段同理
    blend: { ...rest.blend, seq: rest.blend?.seq ?? MUSIC_CONFIG_V3_DEFAULTS.blendSeq },
    deep: { ...MUSIC_CONFIG_V3_DEFAULTS.deep, ...rest.deep },
    content: { ...MUSIC_CONFIG_V3_DEFAULTS.content, ...rest.content }
  };
}

/** 数值的展示：小数位跟着步长走 */
export function formatFieldValue(spec: FieldSpec, value: unknown): string {
  // 旧版本的参数快照没有第二期字段，显示「—」而不是「关闭」
  if (spec.kind === "bool") return typeof value === "boolean" ? (value ? "开启" : "关闭") : "—";
  if (spec.kind === "cron") return cronLabel(String(value ?? "")) || "不定时";
  if (spec.kind === "select") return typeof value === "number" ? `${value}${spec.unit ? ` ${spec.unit}` : ""}` : "—";
  const v = typeof value === "number" ? value : Number.NaN;
  if (!Number.isFinite(v)) return "—";
  const decimals = spec.step >= 1 ? 0 : Math.min(4, String(spec.step).split(".")[1]?.length ?? 2);
  return `${Number(v.toFixed(decimals))}${spec.unit ? ` ${spec.unit}` : ""}`;
}
