import type { MusicTags, MusicTagsInput } from "@/lib/api/music";

/**
 * 内容标签表（docs/music-personalization-3.md「二、内容理解」）：键固定，与 Go `contract/tags.go`、
 * Python `music_deep/tags.py` 同一份。界面只显示中文名；人工修改只能从这里选，不在表里的键保存时丢弃。
 * 表改了三处一起改，顺序即展示顺序。
 */

export type TagOption = { key: string; label: string };

const opts = (pairs: Array<[string, string]>): TagOption[] => pairs.map(([key, label]) => ({ key, label }));

export const GENRE_OPTIONS = opts([
  ["pop", "流行"],
  ["rock", "摇滚"],
  ["folk", "民谣"],
  ["electronic", "电子"],
  ["hiphop", "嘻哈说唱"],
  ["rnb", "R&B"],
  ["jazz", "爵士"],
  ["classical", "古典"],
  ["light", "轻音乐"],
  ["guofeng", "国风古风"],
  ["soundtrack", "影视原声"],
  ["acg", "动漫"],
  ["game", "游戏音乐"],
  ["children", "儿歌"],
  ["metal", "金属"],
  ["punk", "朋克"],
  ["indie", "独立"],
  ["country", "乡村"],
  ["blues", "布鲁斯"],
  ["reggae", "雷鬼"],
  ["latin", "拉丁"],
  ["dance", "舞曲"],
  ["postrock", "后摇"],
  ["instrumental", "器乐"],
  ["chinese_opera", "戏曲"],
  ["world", "世界音乐"],
  ["ethnic", "民族"],
  ["religious", "宗教"],
  ["internet", "网络歌曲"],
  ["city_pop", "城市流行"],
  ["ambient", "氛围"],
  ["lofi", "Lo-fi"]
]);

export const MOOD_OPTIONS = opts([
  ["happy", "快乐"],
  ["sad", "伤感"],
  ["calm", "平静"],
  ["energetic", "活力"],
  ["romantic", "浪漫"],
  ["lonely", "孤独"],
  ["nostalgic", "怀旧"],
  ["healing", "治愈"],
  ["angry", "愤怒"],
  ["dreamy", "梦幻"],
  ["epic", "燃"],
  ["chill", "松弛"],
  ["sweet", "甜蜜"],
  ["dark", "暗黑"],
  ["hopeful", "希望"],
  ["tense", "紧张"]
]);

export const TAG_SCENE_OPTIONS = opts([
  ["workout", "运动"],
  ["study", "学习"],
  ["sleep", "睡前"],
  ["commute", "通勤"],
  ["party", "派对"],
  ["driving", "开车"],
  ["work", "工作"],
  ["relax", "放松"],
  ["rainy", "雨天"],
  ["night", "深夜"],
  ["morning", "清晨"],
  ["travel", "旅行"],
  ["gaming", "打游戏"],
  ["wedding", "婚礼"]
]);

export const ERA_OPTIONS = opts([
  ["1960s", "60 年代"],
  ["1970s", "70 年代"],
  ["1980s", "80 年代"],
  ["1990s", "90 年代"],
  ["2000s", "2000 年代"],
  ["2010s", "2010 年代"],
  ["2020s", "2020 年代"]
]);

export const VOCAL_OPTIONS = opts([
  ["male", "男声"],
  ["female", "女声"],
  ["group", "组合"],
  ["none", "无人声"]
]);

export const FLAG_OPTIONS = opts([
  ["live", "现场"],
  ["cover", "翻唱"],
  ["instrumental", "纯音乐"],
  ["remix", "混音"],
  ["acoustic", "原声"]
]);

/** genres / moods / scenes 各最多 3 个 */
export const TAG_MAX = 3;

export type TagKind = "genres" | "moods" | "scenes" | "era" | "vocal" | "flags";

export const TAG_KIND_LABEL: Record<TagKind, string> = {
  genres: "曲风",
  moods: "情绪",
  scenes: "场景",
  era: "年代",
  vocal: "人声",
  flags: "版本"
};

const TABLE: Record<TagKind, TagOption[]> = {
  genres: GENRE_OPTIONS,
  moods: MOOD_OPTIONS,
  scenes: TAG_SCENE_OPTIONS,
  era: ERA_OPTIONS,
  vocal: VOCAL_OPTIONS,
  flags: FLAG_OPTIONS
};

const LABELS: Record<TagKind, Map<string, string>> = Object.fromEntries(
  (Object.keys(TABLE) as TagKind[]).map((kind) => [kind, new Map(TABLE[kind].map((o) => [o.key, o.label]))])
) as Record<TagKind, Map<string, string>>;

/** 键 → 中文名；表外的键原样显示（服务端与前端表不同步时也能看出是哪个键） */
export function tagLabel(kind: TagKind, key: string) {
  return LABELS[kind].get(key) ?? key;
}

export const genreLabel = (key: string) => tagLabel("genres", key);
export const moodLabel = (key: string) => tagLabel("moods", key);

/** 只保留表里的键、去重、按表的顺序，最多 `max` 个 */
export function keepKnown(kind: TagKind, keys: string[] | null | undefined, max?: number) {
  const set = new Set(keys ?? []);
  const known = TABLE[kind].filter((o) => set.has(o.key)).map((o) => o.key);
  return typeof max === "number" ? known.slice(0, max) : known;
}

/** 编辑表单的初值：没有标签时为空表单，能量取中间值 */
export function toTagsInput(tags: MusicTags | null | undefined): MusicTagsInput {
  return {
    genres: keepKnown("genres", tags?.genres, TAG_MAX),
    moods: keepKnown("moods", tags?.moods, TAG_MAX),
    scenes: keepKnown("scenes", tags?.scenes, TAG_MAX),
    era: tags?.era && LABELS.era.has(tags.era) ? tags.era : "",
    energy: typeof tags?.energy === "number" && Number.isFinite(tags.energy) ? Math.min(1, Math.max(0, tags.energy)) : 0.5,
    vocal: tags?.vocal && LABELS.vocal.has(tags.vocal) ? tags.vocal : "",
    flags: keepKnown("flags", tags?.flags)
  };
}

/** 提交前再过一遍表：防止表单状态里混进表外的键 */
export function sanitizeTagsInput(input: MusicTagsInput): MusicTagsInput {
  return toTagsInput({ ...input, confidence: 1, model: "", taggedAt: 0, manual: true });
}

/** 能量：0–1 → 「舒缓」「适中」「强烈」 */
export function energyWord(energy: number) {
  if (!Number.isFinite(energy)) return "—";
  if (energy < 0.34) return "舒缓";
  if (energy < 0.67) return "适中";
  return "强烈";
}
