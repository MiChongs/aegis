package room

import (
	"net/url"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

var (
	sourcePattern = regexp.MustCompile(`^[a-z0-9_]{1,16}$`)
	songIDPattern = regexp.MustCompile(`^[A-Za-z0-9_\-]{1,256}$`)
	itemIDPattern = regexp.MustCompile(`^[A-Za-z0-9_\-]{1,64}$`)
	codePattern   = regexp.MustCompile(`^[0-9]{6}$`)
)

const (
	maxNameRunes  = 40
	maxTextRunes  = 300
	maxFieldRunes = 200
	maxCoverBytes = 1024
	maxEmojiBytes = 32
)

// CleanText 去掉首尾空白与控制字符，按字符数截断。
func CleanText(value string, limit int) string {
	value = strings.TrimSpace(value)
	if !utf8.ValidString(value) {
		value = strings.ToValidUTF8(value, "")
	}
	var b strings.Builder
	count := 0
	for _, r := range value {
		if unicode.IsControl(r) && r != '\n' {
			continue
		}
		if count >= limit {
			break
		}
		b.WriteRune(r)
		count++
	}
	return strings.TrimSpace(b.String())
}

// ValidCode 房间号是否合法（6 位数字）。
func ValidCode(code string) bool { return codePattern.MatchString(code) }

// NormalizeTrack 校验并规整客户端给的歌；needItem 为真时要求带队列项标识。
func NormalizeTrack(t Track, needItem bool) (Track, error) {
	t.Source = strings.ToLower(strings.TrimSpace(t.Source))
	t.SongID = strings.TrimSpace(t.SongID)
	t.ItemID = strings.TrimSpace(t.ItemID)
	if !sourcePattern.MatchString(t.Source) || !songIDPattern.MatchString(t.SongID) {
		return t, Invalid("歌曲来源或编号无效")
	}
	if needItem && !itemIDPattern.MatchString(t.ItemID) {
		return t, Invalid("队列项标识无效")
	}
	if !needItem {
		t.ItemID = ""
	}
	t.Title = CleanText(t.Title, maxFieldRunes)
	if t.Title == "" {
		return t, Invalid("歌名不能为空")
	}
	t.Artist = CleanText(t.Artist, maxFieldRunes)
	t.Album = CleanText(t.Album, maxFieldRunes)
	t.Cover = cleanCover(t.Cover)
	if t.DurationMs < 0 || t.DurationMs > 24*3600*1000 {
		t.DurationMs = 0
	}
	if t.RequestedBy < 0 {
		t.RequestedBy = 0
	}
	return t, nil
}

func cleanCover(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > maxCoverBytes {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" {
		return ""
	}
	return raw
}

// NormalizeEmoji 表情只收短的可见文本，别拿它当聊天。
func NormalizeEmoji(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > maxEmojiBytes || !utf8.ValidString(raw) {
		return "", Invalid("表情无效")
	}
	for _, r := range raw {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return "", Invalid("表情无效")
		}
	}
	return raw, nil
}

func validState(state string) bool {
	switch state {
	case StateIdle, StateBuffering, StatePlaying, StatePaused, StateError:
		return true
	}
	return false
}

func validAction(action string) bool {
	switch action {
	case ActionPlay, ActionPause, ActionSeek, ActionNext, ActionPrevious, ActionJump:
		return true
	}
	return false
}
