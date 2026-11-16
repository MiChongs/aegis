package trainer

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"

	"aegis/services/music/contract"
	"aegis/services/music/internal/llm"
	"aegis/services/music/internal/store"
)

// 内容理解（docs/music-personalization-3.md「二、内容理解」）：曲库里没打过标签的歌 → 大模型标签 → 语义向量。
// 多个 trainer 实例里只有抢到 Redis 锁的那个干活；每个应用每天最多 content.dailyLimit 首。

const tagBatch = 20

// Enricher 打标签的后台循环。
type Enricher struct {
	T   *Trainer
	LLM *llm.Client
}

// TagSystemPrompt 打标签的系统提示词（键表从 contract 生成，与 Python 的 tags.py 同一份）。
func TagSystemPrompt() string {
	keys := func(opts []contract.TagOption) string {
		var b []string
		for _, o := range opts {
			b = append(b, o.Key+"（"+o.Title+"）")
		}
		return strings.Join(b, "、")
	}
	return "你是专业的音乐资料编辑。根据歌名、歌手、专辑给每首歌打标签，只能使用下面列出的键，不在表里的不要写。\n" +
		"genres 曲风（最多 3 个）：" + keys(contract.Genres) + "\n" +
		"moods 情绪（最多 3 个）：" + keys(contract.Moods) + "\n" +
		"scenes 适合的场景（最多 3 个）：" + keys(contract.SceneTags) + "\n" +
		"era 发行年代：" + strings.Join(contract.Eras, "、") + "，不确定留空\n" +
		"energy 能量 0 到 1（安静 0，激烈 1）；vocal 人声：" + strings.Join(contract.Vocals, "、") + "，不确定留空\n" +
		"flags 版本标志（可多选，可为空）：" + strings.Join(contract.Flags, "、") + "\n" +
		"confidence 0 到 1：你对这首歌本身有多熟悉。不认识的歌按歌手的常见风格推断，并把 confidence 设到 0.3 以下。\n" +
		`只输出一个 JSON 对象：{"items":[{"i":0,"genres":[],"moods":[],"scenes":[],"era":"","energy":0.5,"vocal":"","flags":[],"confidence":0.5}]}，` +
		"i 与输入一致，每首都要有。"
}

// ArtistSystemPrompt 歌手知识的系统提示词。
func ArtistSystemPrompt() string {
	var g []string
	for _, o := range contract.Genres {
		g = append(g, o.Key)
	}
	return "你是专业的音乐资料编辑。给出每位歌手风格最相近的歌手（最多 10 位，必须是真实存在的歌手，用他们最常见的写法），" +
		"主要曲风（只能用这些键：" + strings.Join(g, "、") + "，最多 3 个），主要演唱语种（zh、en、ja、ko、other）。" +
		`只输出一个 JSON 对象：{"items":[{"i":0,"similar":[],"genres":[],"lang":"zh"}]}，i 与输入一致。不认识的歌手 similar 留空。`
}

// EmbedText 语义向量的输入文本。
func EmbedText(t contract.Track, tags contract.Tags) string {
	parts := []string{strings.TrimSpace(t.Title + " - " + t.Artist)}
	if t.Album != "" {
		parts = append(parts, "专辑："+t.Album)
	}
	names := func(keys []string) string {
		var out []string
		for _, k := range keys {
			out = append(out, contract.TagTitle(k))
		}
		return strings.Join(out, "、")
	}
	if len(tags.Genres) > 0 {
		parts = append(parts, "曲风："+names(tags.Genres))
	}
	if len(tags.Moods) > 0 {
		parts = append(parts, "情绪："+names(tags.Moods))
	}
	if len(tags.Scenes) > 0 {
		parts = append(parts, "场景："+names(tags.Scenes))
	}
	if tags.Era != "" {
		parts = append(parts, "年代："+tags.Era)
	}
	return strings.Join(parts, "；")
}

// Run 循环：每 30 秒一轮，抢到锁才干活。
func (e *Enricher) Run(ctx context.Context) {
	if !e.LLM.Enabled() {
		e.T.Log.Info("MUSIC_LLM_BASE_URL not set, content enrichment disabled")
		return
	}
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		if e.lock(ctx) {
			e.round(ctx)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (e *Enricher) lock(ctx context.Context) bool {
	key := e.T.Prefix + "enrich:lock"
	ok, err := e.T.Redis.SetNX(ctx, key, "1", 2*time.Minute).Result()
	if err != nil {
		return false
	}
	return ok
}

func (e *Enricher) round(ctx context.Context) {
	defer e.T.Redis.Del(context.Background(), e.T.Prefix+"enrich:lock")
	apps, err := e.T.PG.TagApps(ctx)
	if err != nil {
		e.T.Log.Warn("list apps for enrichment failed", zap.Error(err))
		return
	}
	for _, app := range apps {
		cfg, err := e.T.PG.Config(ctx, app)
		if err != nil || !cfg.Enabled || !cfg.Content.Enabled {
			continue
		}
		done, _ := e.T.PG.TaggedToday(ctx, app)
		budget := int64(cfg.Content.DailyLimit) - done
		// 每轮每个应用最多 10 批（200 首），其余下一轮
		for n := 0; n < 10 && budget > 0 && ctx.Err() == nil; n++ {
			tracks, err := e.T.PG.PendingTracks(ctx, app, int(min(budget, tagBatch)))
			if err != nil || len(tracks) == 0 {
				break
			}
			if err := e.TagBatch(ctx, app, tracks); err != nil {
				e.T.Log.Warn("tag batch failed", zap.Int64("app", app), zap.Error(err))
				break
			}
			budget -= int64(len(tracks))
		}
		if names, err := e.T.PG.PendingArtists(ctx, app, 20); err == nil && len(names) > 0 {
			if err := e.ArtistBatch(ctx, app, names); err != nil {
				e.T.Log.Warn("artist batch failed", zap.Int64("app", app), zap.Error(err))
			}
		}
	}
}

// TagBatch 一批歌：标签 → 向量 → 写库；失败的记失败次数。
func (e *Enricher) TagBatch(ctx context.Context, app int64, tracks []store.TrackToTag) error {
	type in struct {
		I      int    `json:"i"`
		Title  string `json:"title"`
		Artist string `json:"artist"`
		Album  string `json:"album,omitempty"`
	}
	input := make([]in, len(tracks))
	keys := make([]string, len(tracks))
	for i, t := range tracks {
		input[i] = in{I: i, Title: t.Track.Title, Artist: t.Track.Artist, Album: t.Track.Album}
		keys[i] = t.WorkKey
	}
	raw, _ := json.Marshal(input)
	var out struct {
		Items []struct {
			I int `json:"i"`
			contract.Tags
		} `json:"items"`
	}
	cctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	usage, err := e.LLM.ChatJSON(cctx, TagSystemPrompt(), string(raw), &out)
	_ = e.T.PG.AddUsage(ctx, app, "tag", 1, usage.TotalTokens, 0)
	if err != nil {
		_ = e.T.PG.MarkTagFailed(ctx, app, keys, err.Error())
		return err
	}
	tags := make([]*contract.Tags, len(tracks))
	for _, it := range out.Items {
		if it.I >= 0 && it.I < len(tracks) {
			t := it.Tags
			t.Clean()
			tags[it.I] = &t
		}
	}
	var ok []int
	var missing []string
	texts := []string{}
	for i, t := range tags {
		if t == nil {
			missing = append(missing, keys[i])
			continue
		}
		ok = append(ok, i)
		texts = append(texts, EmbedText(tracks[i].Track, *t))
	}
	if len(missing) > 0 {
		_ = e.T.PG.MarkTagFailed(ctx, app, missing, "大模型漏了这首")
	}
	if len(ok) == 0 {
		return nil
	}
	vecs, eu, err := e.LLM.Embed(cctx, texts)
	_ = e.T.PG.AddUsage(ctx, app, "embed", 1, eu.TotalTokens, len(texts))
	if err != nil {
		// 向量失败不影响标签入库（向量下次人工重打或重训时补）
		e.T.Log.Warn("embedding failed", zap.Int64("app", app), zap.Error(err))
		vecs = nil
	}
	rows := make([]store.TagRow, 0, len(ok))
	for k, i := range ok {
		r := store.TagRow{WorkKey: keys[i], Tags: *tags[i], Model: e.LLM.Model}
		if vecs != nil {
			r.Embedding = vecs[k]
		}
		rows = append(rows, r)
	}
	if err := e.T.PG.SaveTags(ctx, app, rows); err != nil {
		return fmt.Errorf("写标签失败：%w", err)
	}
	return e.T.PG.AddUsage(ctx, app, "tag", 0, 0, len(rows))
}

// ArtistBatch 一批歌手的相似歌手、曲风、语种。
func (e *Enricher) ArtistBatch(ctx context.Context, app int64, names []string) error {
	type in struct {
		I      int    `json:"i"`
		Artist string `json:"artist"`
	}
	input := make([]in, len(names))
	for i, n := range names {
		input[i] = in{I: i, Artist: n}
	}
	raw, _ := json.Marshal(input)
	var out struct {
		Items []struct {
			I       int      `json:"i"`
			Similar []string `json:"similar"`
			Genres  []string `json:"genres"`
			Lang    string   `json:"lang"`
		} `json:"items"`
	}
	cctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	usage, err := e.LLM.ChatJSON(cctx, ArtistSystemPrompt(), string(raw), &out)
	_ = e.T.PG.AddUsage(ctx, app, "artist", 1, usage.TotalTokens, len(names))
	if err != nil {
		return err
	}
	list := make([]store.ArtistKnowledge, 0, len(out.Items))
	for _, it := range out.Items {
		if it.I < 0 || it.I >= len(names) {
			continue
		}
		t := contract.Tags{Genres: it.Genres}
		t.Clean()
		lang := it.Lang
		if !contract.ValidLang(lang) {
			lang = ""
		}
		var similar []string
		for _, s := range it.Similar {
			if s = strings.TrimSpace(s); s != "" && len(similar) < 10 && contract.NormalizeArtist(s) != contract.NormalizeArtist(names[it.I]) {
				similar = append(similar, s)
			}
		}
		if similar == nil {
			similar = []string{}
		}
		list = append(list, store.ArtistKnowledge{Artist: contract.NormalizeArtist(names[it.I]), Name: names[it.I],
			Similar: similar, Genres: t.Genres, Lang: lang})
	}
	return e.T.PG.SaveArtists(ctx, app, list)
}
