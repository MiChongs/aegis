package contract

// 内容标签表（docs/music-personalization-3.md「标签表」）。Python 的 music_deep/tags.py 有同样一份，改动要两边一起改。

// TagOption 一个标签键与中文名。
type TagOption struct {
	Key   string `json:"key"`
	Title string `json:"title"`
}

// Genres 曲风（每首最多 3 个）。
var Genres = []TagOption{
	{"pop", "流行"}, {"rock", "摇滚"}, {"folk", "民谣"}, {"electronic", "电子"}, {"hiphop", "嘻哈说唱"}, {"rnb", "R&B"},
	{"jazz", "爵士"}, {"classical", "古典"}, {"light", "轻音乐"}, {"guofeng", "国风古风"}, {"soundtrack", "影视原声"},
	{"acg", "动漫"}, {"game", "游戏音乐"}, {"children", "儿歌"}, {"metal", "金属"}, {"punk", "朋克"}, {"indie", "独立"},
	{"country", "乡村"}, {"blues", "布鲁斯"}, {"reggae", "雷鬼"}, {"latin", "拉丁"}, {"dance", "舞曲"}, {"postrock", "后摇"},
	{"instrumental", "器乐"}, {"chinese_opera", "戏曲"}, {"world", "世界音乐"}, {"ethnic", "民族"}, {"religious", "宗教"},
	{"internet", "网络歌曲"}, {"city_pop", "城市流行"}, {"ambient", "氛围"}, {"lofi", "Lo-fi"},
}

// Moods 情绪（每首最多 3 个）。
var Moods = []TagOption{
	{"happy", "快乐"}, {"sad", "伤感"}, {"calm", "平静"}, {"energetic", "活力"}, {"romantic", "浪漫"}, {"lonely", "孤独"},
	{"nostalgic", "怀旧"}, {"healing", "治愈"}, {"angry", "愤怒"}, {"dreamy", "梦幻"}, {"epic", "燃"}, {"chill", "松弛"},
	{"sweet", "甜蜜"}, {"dark", "暗黑"}, {"hopeful", "希望"}, {"tense", "紧张"},
}

// SceneTags 场景标签（每首最多 3 个）。
var SceneTags = []TagOption{
	{"workout", "运动"}, {"study", "学习"}, {"sleep", "睡前"}, {"commute", "通勤"}, {"party", "派对"}, {"driving", "开车"},
	{"work", "工作"}, {"relax", "放松"}, {"rainy", "雨天"}, {"night", "深夜"}, {"morning", "清晨"}, {"travel", "旅行"},
	{"gaming", "打游戏"}, {"wedding", "婚礼"},
}

// Eras 年代。
var Eras = []string{"1960s", "1970s", "1980s", "1990s", "2000s", "2010s", "2020s"}

// Vocals 人声类型。
var Vocals = []string{"male", "female", "group", "none"}

// Flags 版本标志。
var Flags = []string{"live", "cover", "instrumental", "remix", "acoustic"}

func tagSet(opts []TagOption) map[string]string {
	m := make(map[string]string, len(opts))
	for _, o := range opts {
		m[o.Key] = o.Title
	}
	return m
}

var (
	genreSet = tagSet(Genres)
	moodSet  = tagSet(Moods)
	sceneSet = tagSet(SceneTags)
)

// TagTitle 标签键的中文名（不认识的原样返回）。
func TagTitle(key string) string {
	for _, m := range []map[string]string{genreSet, moodSet, sceneSet} {
		if t, ok := m[key]; ok {
			return t
		}
	}
	return key
}

func keepKnown(in []string, set map[string]bool, max int) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, v := range in {
		if set[v] && !seen[v] {
			seen[v] = true
			out = append(out, v)
			if len(out) >= max {
				break
			}
		}
	}
	return out
}

func boolSet(keys []string) map[string]bool {
	m := make(map[string]bool, len(keys))
	for _, k := range keys {
		m[k] = true
	}
	return m
}

func optKeys(opts []TagOption) []string {
	out := make([]string, len(opts))
	for i, o := range opts {
		out[i] = o.Key
	}
	return out
}

// Tags 一首歌的内容标签。
type Tags struct {
	Genres     []string `json:"genres"`
	Moods      []string `json:"moods"`
	Scenes     []string `json:"scenes"`
	Era        string   `json:"era"`
	Energy     float64  `json:"energy"`
	Vocal      string   `json:"vocal"`
	Flags      []string `json:"flags"`
	Confidence float64  `json:"confidence"`
}

// Clean 丢掉表外的键、截断数量、限制取值范围。
func (t *Tags) Clean() {
	t.Genres = keepKnown(t.Genres, boolSet(optKeys(Genres)), 3)
	t.Moods = keepKnown(t.Moods, boolSet(optKeys(Moods)), 3)
	t.Scenes = keepKnown(t.Scenes, boolSet(optKeys(SceneTags)), 3)
	t.Flags = keepKnown(t.Flags, boolSet(Flags), len(Flags))
	if !boolSet(Eras)[t.Era] {
		t.Era = ""
	}
	if !boolSet(Vocals)[t.Vocal] {
		t.Vocal = ""
	}
	clamp := func(v float64) float64 {
		if v != v || v < 0 {
			return 0
		}
		if v > 1 {
			return 1
		}
		return v
	}
	t.Energy, t.Confidence = clamp(t.Energy), clamp(t.Confidence)
}

// Keys 全部标签（曲风、情绪、场景、年代、人声、标志），算标签相似度用。
func (t *Tags) Keys() []string {
	out := append(append(append([]string{}, t.Genres...), t.Moods...), t.Scenes...)
	if t.Era != "" {
		out = append(out, "era:"+t.Era)
	}
	if t.Vocal != "" {
		out = append(out, "vocal:"+t.Vocal)
	}
	for _, f := range t.Flags {
		out = append(out, "flag:"+f)
	}
	return out
}
