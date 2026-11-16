// Package appcfg 每个应用自己的个性化配置（控制台「配置」页签）：开关、事件权重、模型参数、融合权重、定时。
// 训练与推荐都只读这里，取值范围在 Validate 里集中校验。
package appcfg

import (
	"fmt"
	"strings"

	"github.com/robfig/cron/v3"
)

// Weights 事件对 (用户, 作品) 偏好分的贡献。
type Weights struct {
	Complete    float64 `json:"complete"`
	Partial     float64 `json:"partial"`
	Skip        float64 `json:"skip"`
	Like        float64 `json:"like"`
	Dislike     float64 `json:"dislike"`
	PlaylistAdd float64 `json:"playlistAdd"`
	Download    float64 `json:"download"`
	Share       float64 `json:"share"`
	SearchClick float64 `json:"searchClick"`
	Click       float64 `json:"click"`
	SeekBack    float64 `json:"seekBack"`
}

// ALS 隐式 ALS 参数。
type ALS struct {
	Factors        int     `json:"factors"`
	Iterations     int     `json:"iterations"`
	Regularization float64 `json:"regularization"`
	Alpha          float64 `json:"alpha"`
}

// ItemCF 参数。
type ItemCF struct {
	Neighbors    int     `json:"neighbors"`
	SessionBoost float64 `json:"sessionBoost"`
}

// Blend 召回通道的融合权重。
type Blend struct {
	ALS      float64 `json:"als"`
	ItemCF   float64 `json:"itemcf"`
	Artist   float64 `json:"artist"`
	Trending float64 `json:"trending"`
	Explore  float64 `json:"explore"`
}

// Config 应用配置。
type Config struct {
	Enabled           bool    `json:"enabled"`
	CollectEnabled    bool    `json:"collectEnabled"`
	WindowDays        int     `json:"windowDays"`
	HalfLifeDays      float64 `json:"halfLifeDays"`
	MinItemUsers      int     `json:"minItemUsers"`
	Weights           Weights `json:"weights"`
	ALS               ALS     `json:"als"`
	ItemCF            ItemCF  `json:"itemcf"`
	Blend             Blend   `json:"blend"`
	ArtistCap         int     `json:"artistCap"`
	RecentExcludeDays int     `json:"recentExcludeDays"`
	ExploreRatio      float64 `json:"exploreRatio"`
	Schedule          string  `json:"schedule"`
	AutoPublish       bool    `json:"autoPublish"`
	MinLift           float64 `json:"minLift"`
	UpdatedAt         int64   `json:"updatedAt"`
	UpdatedBy         string  `json:"updatedBy"`
}

// Default 新应用的默认配置（docs「应用配置」）。
func Default() Config {
	return Config{
		Enabled: true, CollectEnabled: true,
		WindowDays: 180, HalfLifeDays: 45, MinItemUsers: 2,
		Weights: Weights{
			Complete: 1.0, Partial: 0.4, Skip: -0.6, Like: 3.0, Dislike: -6.0, PlaylistAdd: 2.0,
			Download: 2.0, Share: 1.5, SearchClick: 0.3, Click: 0.2, SeekBack: -0.1,
		},
		ALS:               ALS{Factors: 48, Iterations: 12, Regularization: 0.05, Alpha: 20},
		ItemCF:            ItemCF{Neighbors: 50, SessionBoost: 0.3},
		Blend:             Blend{ALS: 1.0, ItemCF: 0.8, Artist: 0.5, Trending: 0.3, Explore: 0.1},
		ArtistCap:         2,
		RecentExcludeDays: 2,
		ExploreRatio:      0.1,
		Schedule:          "0 4 * * *",
		AutoPublish:       true,
		MinLift:           1.0,
	}
}

// Validate 校验取值范围；错误信息直接给控制台看，说清楚是哪一项。
func (c *Config) Validate() error {
	c.Schedule = strings.TrimSpace(c.Schedule)
	type rng struct {
		name     string
		v        float64
		min, max float64
	}
	checks := []rng{
		{"统计窗口（天）", float64(c.WindowDays), 7, 730},
		{"半衰期（天）", c.HalfLifeDays, 1, 365},
		{"作品最少听众数", float64(c.MinItemUsers), 1, 50},
		{"ALS 维度", float64(c.ALS.Factors), 8, 256},
		{"ALS 迭代次数", float64(c.ALS.Iterations), 1, 50},
		{"ALS 正则", c.ALS.Regularization, 0.0001, 10},
		{"ALS 置信度系数", c.ALS.Alpha, 1, 200},
		{"ItemCF 邻居数", float64(c.ItemCF.Neighbors), 5, 200},
		{"会话相邻加成", c.ItemCF.SessionBoost, 0, 2},
		{"同一歌手上限", float64(c.ArtistCap), 1, 10},
		{"近期播放降权天数", float64(c.RecentExcludeDays), 0, 30},
		{"探索比例", c.ExploreRatio, 0, 0.5},
		{"发布门槛", c.MinLift, 0, 3},
	}
	w := c.Weights
	for _, item := range []struct {
		name string
		v    float64
	}{
		{"听完", w.Complete}, {"部分播放", w.Partial}, {"跳过", w.Skip}, {"收藏", w.Like}, {"不感兴趣", w.Dislike},
		{"加入歌单", w.PlaylistAdd}, {"下载", w.Download}, {"分享", w.Share}, {"点搜索结果", w.SearchClick},
		{"点击", w.Click}, {"向后拖动", w.SeekBack},
	} {
		checks = append(checks, rng{"事件权重「" + item.name + "」", item.v, -20, 20})
	}
	b := c.Blend
	for _, item := range []struct {
		name string
		v    float64
	}{{"ALS", b.ALS}, {"ItemCF", b.ItemCF}, {"歌手偏好", b.Artist}, {"热度", b.Trending}, {"探索", b.Explore}} {
		checks = append(checks, rng{"融合权重「" + item.name + "」", item.v, 0, 5})
	}
	for _, ch := range checks {
		if ch.v < ch.min || ch.v > ch.max || ch.v != ch.v {
			return fmt.Errorf("%s应在 %g 到 %g 之间", ch.name, ch.min, ch.max)
		}
	}
	if c.Schedule != "" {
		if _, err := cron.ParseStandard(c.Schedule); err != nil {
			return fmt.Errorf("训练定时不是有效的五段 cron 表达式：%v", err)
		}
	}
	return nil
}
