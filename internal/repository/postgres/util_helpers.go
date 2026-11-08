package postgres

// 通用小工具（原先放在风控仓储里，风控拆成独立平台后挪到这里）

func normalizePage(page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 200 {
		pageSize = 20
	}
	return page, pageSize
}

// truncateColumn 按 rune 截断，避免超长输入撞上 VARCHAR 上限报错。
// UA 与账号都可能被恶意构造成超长串，让它把一次评估的留痕整条写失败并不划算。
func truncateColumn(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

func firstNonEmptyString(current, next string) string {
	if current != "" {
		return current
	}
	return next
}
