package httptransport

import (
	"strconv"
	"strings"
	"time"
)

// 通用解析小工具（原先放在旧风控中心的处理器里，风控拆成独立平台后挪到这里）

func parseOptionalRFC3339(raw string) (*time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	at, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil, err
	}
	return &at, nil
}

func parseOptionalBool(raw string) (bool, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false, false
	}
	parsed, err := strconv.ParseBool(raw)
	if err != nil {
		return false, false
	}
	return parsed, true
}
