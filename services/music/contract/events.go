// Package contract 音乐个性化的公开契约：客户端事件、总线上的事件、流与主题、作品键。
// 主服务（上报接口）与音乐服务（music-ingest / music-rec / music-trainer）共用这一份，
// 所以这里不依赖任何 internal 包。见 docs/music-personalization.md。
package contract

import (
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// 事件类型（docs「事件协议」）。
const (
	TypePlayStart   = "play_start"
	TypePlayEnd     = "play_end"
	TypePause       = "pause"
	TypeResume      = "resume"
	TypeSeek        = "seek"
	TypeLike        = "like"
	TypeUnlike      = "unlike"
	TypeDislike     = "dislike"
	TypePlaylistAdd = "playlist_add"
	TypeDownload    = "download"
	TypeShare       = "share"
	TypeSearch      = "search"
	TypeSearchClick = "search_click"
	TypeClick       = "click"
	TypeImpression  = "impression"
	TypeLyricsOpen  = "lyrics_open"
)

// play_end 的结束原因。
const (
	ReasonCompleted = "completed"
	ReasonSkipped   = "skipped"
	ReasonSwitched  = "switched"
	ReasonStopped   = "stopped"
	ReasonError     = "error"
)

// 推荐场景。
const (
	SceneHome    = "home"
	SceneDaily   = "daily"
	SceneRadio   = "radio"
	SceneSimilar = "similar"
)

// Scenes 全部场景（控制台、校验用）。
var Scenes = []string{SceneHome, SceneDaily, SceneRadio, SceneSimilar}

// ValidScene 场景是否合法。
func ValidScene(scene string) bool {
	for _, s := range Scenes {
		if s == scene {
			return true
		}
	}
	return false
}

// 每种类型是否必须带歌。
var typeNeedsTrack = map[string]bool{
	TypePlayStart: true, TypePlayEnd: true, TypePause: true, TypeResume: true, TypeSeek: true,
	TypeLike: true, TypeUnlike: true, TypeDislike: true, TypePlaylistAdd: true, TypeDownload: true,
	TypeShare: true, TypeSearch: false, TypeSearchClick: true, TypeClick: true, TypeImpression: false,
	TypeLyricsOpen: true,
}

// Types 全部事件类型（排序与控制台展示用）。
var Types = []string{
	TypePlayStart, TypePlayEnd, TypePause, TypeResume, TypeSeek, TypeLike, TypeUnlike, TypeDislike,
	TypePlaylistAdd, TypeDownload, TypeShare, TypeSearch, TypeSearchClick, TypeClick, TypeImpression, TypeLyricsOpen,
}

// ValidType 事件类型是否合法。
func ValidType(t string) bool { _, ok := typeNeedsTrack[t]; return ok }

// 上报限制。
const (
	MaxBatch       = 100
	MaxBodyBytes   = 256 << 10
	MaxProps       = 16
	MaxPropKey     = 32
	MaxPropValue   = 128
	MaxPast        = 30 * 24 * time.Hour
	MaxFuture      = 10 * time.Minute
	maxText        = 256
	maxQuery       = 128
	maxDurationMs  = 6 * 60 * 60 * 1000
	maxPositionVal = 10000
)

// Track 客户端看到的一首歌：某个音源里的某个 ID。
type Track struct {
	Source     string `json:"source"`
	SongID     string `json:"songId"`
	Title      string `json:"title"`
	Artist     string `json:"artist,omitempty"`
	Album      string `json:"album,omitempty"`
	Cover      string `json:"cover,omitempty"`
	DurationMs int64  `json:"durationMs,omitempty"`
}

// Key `source:songId`。
func (t *Track) Key() string {
	if t == nil {
		return ""
	}
	return t.Source + ":" + t.SongID
}

// Context 事件从哪里来。
type Context struct {
	Page        string `json:"page,omitempty"`
	Section     string `json:"section,omitempty"`
	Position    int32  `json:"position,omitempty"`
	Query       string `json:"query,omitempty"`
	RecID       string `json:"recId,omitempty"`
	Scene       string `json:"scene,omitempty"`
	PlaylistID  string `json:"playlistId,omitempty"`
	QueueSource string `json:"queueSource,omitempty"`
}

// ClientEvent 客户端上报的一条事件。
type ClientEvent struct {
	ID         string            `json:"id"`
	Type       string            `json:"type"`
	TS         int64             `json:"ts"`
	SessionID  string            `json:"sessionId,omitempty"`
	Track      *Track            `json:"track,omitempty"`
	PlayedMs   int64             `json:"playedMs,omitempty"`
	PositionMs int64             `json:"positionMs,omitempty"`
	Reason     string            `json:"reason,omitempty"`
	Context    Context           `json:"context"`
	Props      map[string]string `json:"props,omitempty"`
}

// ClientInfo 一批事件共用的客户端信息。
type ClientInfo struct {
	Platform   string `json:"platform,omitempty"`
	AppVersion string `json:"appVersion,omitempty"`
	Network    string `json:"network,omitempty"`
}

// Event 总线上的事件：客户端事件 + 服务端补齐的身份与作品键。扁平 JSON。
type Event struct {
	ID         string            `json:"id"`
	Type       string            `json:"type"`
	TS         int64             `json:"ts"`
	ReceivedAt int64             `json:"received_at"`
	AppID      int64             `json:"app_id"`
	UserID     int64             `json:"user_id"`
	DeviceID   string            `json:"device_id,omitempty"`
	SessionID  string            `json:"session_id,omitempty"`
	IP         string            `json:"ip,omitempty"`
	WorkKey    string            `json:"work_key,omitempty"`
	TrackKey   string            `json:"track_key,omitempty"`
	Track      *Track            `json:"track,omitempty"`
	PlayedMs   int64             `json:"played_ms,omitempty"`
	PositionMs int64             `json:"position_ms,omitempty"`
	Reason     string            `json:"reason,omitempty"`
	Context    Context           `json:"context"`
	Props      map[string]string `json:"props,omitempty"`
	Platform   string            `json:"platform,omitempty"`
	AppVersion string            `json:"app_version,omitempty"`
	Network    string            `json:"network,omitempty"`
}

// Served 推荐服务发出的一次推荐结果（落 ClickHouse impressions，算点击率）。
type Served struct {
	RecID        string   `json:"rec_id"`
	Scene        string   `json:"scene"`
	AppID        int64    `json:"app_id"`
	UserID       int64    `json:"user_id"`
	ModelVersion int64    `json:"model_version"`
	Personalized bool     `json:"personalized"`
	ColdStart    bool     `json:"cold_start"`
	Items        []string `json:"items"`
	TS           int64    `json:"ts"`
}

// 拒绝原因（响应 rejected[].reason）。
const (
	RejectInvalidID     = "invalid_id"
	RejectInvalidType   = "invalid_type"
	RejectInvalidTime   = "invalid_time"
	RejectMissingTrack  = "missing_track"
	RejectInvalidTrack  = "invalid_track"
	RejectMissingField  = "missing_field"
	RejectDuplicateInID = "duplicate"
)

var (
	idPattern     = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)
	sourcePattern = regexp.MustCompile(`^[a-z0-9_]{1,16}$`)
	songIDPattern = regexp.MustCompile(`^[A-Za-z0-9_.:=+/-]{1,256}$`)
	tokenPattern  = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,64}$`)
)

// Normalize 校验并规整一条客户端事件；返回空串表示通过，否则是拒绝原因。
// 规整：裁剪长度、去掉控制字符、限制 props、填作品键；不改语义。
func Normalize(ev *ClientEvent, now time.Time) string {
	ev.ID = strings.TrimSpace(ev.ID)
	if !idPattern.MatchString(ev.ID) {
		return RejectInvalidID
	}
	ev.Type = strings.TrimSpace(ev.Type)
	needsTrack, ok := typeNeedsTrack[ev.Type]
	if !ok {
		return RejectInvalidType
	}
	at := time.UnixMilli(ev.TS)
	if ev.TS <= 0 || at.Before(now.Add(-MaxPast)) || at.After(now.Add(MaxFuture)) {
		return RejectInvalidTime
	}
	ev.SessionID = cleanToken(ev.SessionID)
	if ev.Track != nil {
		t := ev.Track
		t.Source = strings.ToLower(strings.TrimSpace(t.Source))
		t.SongID = strings.TrimSpace(t.SongID)
		t.Title = cleanText(t.Title, maxText)
		t.Artist = cleanText(t.Artist, maxText)
		t.Album = cleanText(t.Album, maxText)
		t.Cover = cleanURL(t.Cover)
		if t.DurationMs < 0 || t.DurationMs > maxDurationMs {
			t.DurationMs = 0
		}
		if !sourcePattern.MatchString(t.Source) || !songIDPattern.MatchString(t.SongID) || t.Title == "" || t.Source == "local" {
			if needsTrack {
				return RejectInvalidTrack
			}
			ev.Track = nil
		}
	}
	if needsTrack && ev.Track == nil {
		return RejectMissingTrack
	}
	if ev.PlayedMs < 0 || ev.PlayedMs > maxDurationMs {
		ev.PlayedMs = 0
	}
	if ev.PositionMs < 0 || ev.PositionMs > maxDurationMs {
		ev.PositionMs = 0
	}
	switch ev.Type {
	case TypePlayEnd:
		switch ev.Reason {
		case ReasonCompleted, ReasonSkipped, ReasonSwitched, ReasonStopped, ReasonError:
		default:
			return RejectMissingField
		}
	default:
		ev.Reason = ""
	}
	c := &ev.Context
	c.Page = cleanToken(c.Page)
	c.Section = cleanToken(c.Section)
	c.RecID = cleanToken(c.RecID)
	c.Scene = cleanToken(c.Scene)
	c.PlaylistID = cleanToken(c.PlaylistID)
	c.QueueSource = cleanToken(c.QueueSource)
	c.Query = cleanText(c.Query, maxQuery)
	if c.Position < 0 || c.Position > maxPositionVal {
		c.Position = 0
	}
	switch ev.Type {
	case TypeSearch:
		if c.Query == "" {
			return RejectMissingField
		}
	case TypeImpression:
		if c.RecID == "" {
			return RejectMissingField
		}
	case TypePlaylistAdd:
		if c.PlaylistID == "" {
			c.PlaylistID = "unknown"
		}
	}
	if len(ev.Props) > 0 {
		props := make(map[string]string, len(ev.Props))
		for k, v := range ev.Props {
			if len(props) >= MaxProps {
				break
			}
			k = cleanToken(k)
			if k == "" || len(k) > MaxPropKey {
				continue
			}
			props[k] = cleanText(v, MaxPropValue)
		}
		ev.Props = props
	}
	return ""
}

// Enrich 把规整后的客户端事件变成总线事件。
func Enrich(ev ClientEvent, client ClientInfo, appID, userID int64, deviceID, ip string, now time.Time) Event {
	out := Event{
		ID: ev.ID, Type: ev.Type, TS: ev.TS, ReceivedAt: now.UnixMilli(), AppID: appID, UserID: userID,
		DeviceID: cleanText(deviceID, 128), SessionID: ev.SessionID, IP: ip, Track: ev.Track,
		PlayedMs: ev.PlayedMs, PositionMs: ev.PositionMs, Reason: ev.Reason, Context: ev.Context, Props: ev.Props,
		Platform: cleanToken(strings.ToLower(client.Platform)), AppVersion: cleanText(client.AppVersion, 32),
		Network: cleanToken(strings.ToLower(client.Network)),
	}
	if ev.Track != nil {
		out.TrackKey = ev.Track.Key()
		out.WorkKey = WorkKey(ev.Track.Title, ev.Track.Artist)
	}
	return out
}

func cleanToken(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if tokenPattern.MatchString(s) {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		if b.Len() >= 64 {
			break
		}
		if r < 128 && (r == '_' || r == '-' || r == '.' || r == ':' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func cleanText(s string, max int) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if !utf8.ValidString(s) {
		s = strings.ToValidUTF8(s, "")
	}
	var b strings.Builder
	n := 0
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			r = ' '
		}
		if n >= max {
			break
		}
		b.WriteRune(r)
		n++
	}
	return strings.TrimSpace(b.String())
}

func cleanURL(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 1024 || !(strings.HasPrefix(s, "https://") || strings.HasPrefix(s, "http://")) {
		return ""
	}
	return s
}
