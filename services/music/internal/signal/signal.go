// Package signal 一条事件对 (用户, 作品) 偏好的贡献：训练（ClickHouse 聚合）与实时状态（Redis）共用同一套判据。
package signal

import (
	"math"

	"aegis/services/music/contract"
	"aegis/services/music/internal/appcfg"
)

// Kind 事件被归成哪一类反馈。
type Kind int

const (
	KindNone Kind = iota
	KindComplete
	KindPartial
	KindSkip
	KindLike
	KindUnlike
	KindDislike
	KindPlaylistAdd
	KindDownload
	KindShare
	KindSearchClick
	KindClick
	KindSeekBack
)

// 跳过与听完的判据（与客户端 MusicListenTracker 一致）。
const (
	SkipMaxMs      = 30_000
	SkipMaxRatio   = 0.3
	CompleteRatio  = 0.8
	SeekBackRatio  = 0.3
	minMeaningfuls = 3_000
)

// Classify 把一条事件归类。durationMs 为 0（客户端不知道时长）时只按 reason 与绝对时长判。
func Classify(ev *contract.Event) Kind {
	switch ev.Type {
	case contract.TypePlayEnd:
		dur := int64(0)
		if ev.Track != nil {
			dur = ev.Track.DurationMs
		}
		return classifyPlay(ev.Reason, ev.PlayedMs, dur)
	case contract.TypeLike:
		return KindLike
	case contract.TypeUnlike:
		return KindUnlike
	case contract.TypeDislike:
		return KindDislike
	case contract.TypePlaylistAdd:
		return KindPlaylistAdd
	case contract.TypeDownload:
		return KindDownload
	case contract.TypeShare:
		return KindShare
	case contract.TypeSearchClick:
		return KindSearchClick
	case contract.TypeClick:
		return KindClick
	case contract.TypeSeek:
		if ev.Track != nil && ev.Track.DurationMs > 0 {
			from := parseInt(ev.Props["from"])
			if from-ev.PositionMs > int64(float64(ev.Track.DurationMs)*SeekBackRatio) {
				return KindSeekBack
			}
		}
	}
	return KindNone
}

func classifyPlay(reason string, played, dur int64) Kind {
	if played < minMeaningfuls && reason != contract.ReasonCompleted {
		if reason == contract.ReasonSkipped || reason == contract.ReasonSwitched {
			return KindSkip
		}
		return KindNone
	}
	ratio := 0.0
	if dur > 0 {
		ratio = float64(played) / float64(dur)
	}
	switch {
	case reason == contract.ReasonCompleted || (dur > 0 && ratio >= CompleteRatio):
		return KindComplete
	case (reason == contract.ReasonSkipped || reason == contract.ReasonSwitched) &&
		played < SkipMaxMs && (dur == 0 || ratio < SkipMaxRatio):
		return KindSkip
	case dur > 0 && ratio >= SkipMaxRatio:
		return KindPartial
	case dur == 0 && played >= 60_000:
		return KindPartial
	}
	return KindNone
}

// Weight 某类反馈的分。
func Weight(k Kind, w appcfg.Weights) float64 {
	switch k {
	case KindComplete:
		return w.Complete
	case KindPartial:
		return w.Partial
	case KindSkip:
		return w.Skip
	case KindLike:
		return w.Like
	case KindUnlike:
		return -w.Like
	case KindDislike:
		return w.Dislike
	case KindPlaylistAdd:
		return w.PlaylistAdd
	case KindDownload:
		return w.Download
	case KindShare:
		return w.Share
	case KindSearchClick:
		return w.SearchClick
	case KindClick:
		return w.Click
	case KindSeekBack:
		return w.SeekBack
	}
	return 0
}

// Decay 时间衰减：0.5^(ageDays / halfLife)。
func Decay(ageDays, halfLifeDays float64) float64 {
	if ageDays <= 0 || halfLifeDays <= 0 {
		return 1
	}
	return math.Pow(0.5, ageDays/halfLifeDays)
}

// Confidence 隐式 ALS 的置信度 c = 1 + α·ln(1 + |score| / ε)。
func Confidence(score, alpha float64) float64 {
	return 1 + alpha*math.Log1p(math.Abs(score)/0.5)
}

// Positive 是否为正反馈（训练里 p=1）。
func Positive(k Kind) bool {
	switch k {
	case KindComplete, KindPartial, KindLike, KindPlaylistAdd, KindDownload, KindShare, KindSearchClick:
		return true
	}
	return false
}

// Negative 是否为负反馈（实时状态里进负反馈集合）。
func Negative(k Kind) bool { return k == KindDislike || k == KindSkip }

func parseInt(s string) int64 {
	var n int64
	for _, r := range s {
		if r < '0' || r > '9' {
			return n
		}
		n = n*10 + int64(r-'0')
		if n > 1<<40 {
			return n
		}
	}
	return n
}
