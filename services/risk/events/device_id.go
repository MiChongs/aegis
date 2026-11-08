package events

import "strings"

// NormalizeDeviceID 设备码归一：不能代表一台设备的值一律当作没有设备码（返回空串）。
//
//   - 占位值：只含 0 与分隔符（部分厂商关闭广告追踪后返回 64 位全零 OAID）、9774d56d682e549c（模拟器的固定 ANDROID_ID）、
//     unknown / null 这类字面值。主服务 devicedomain.NormalizeID 已在入口归一，这里兜住历史数据与其它调用方。
//   - UA 摘要：主服务在客户端没报设备码时用 User-Agent 摘要凑的「ua:」键，同型号同版本的手机会得到同一个值，
//     进关系图就会把互不相识的人连成「共用设备」，进向量就会让同型号的设备彼此「高度相似」。
//
// 合法设备码原样返回（只去掉首尾空白）。
func NormalizeDeviceID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	lower := strings.ToLower(id)
	if strings.HasPrefix(lower, "ua:") {
		return ""
	}
	switch lower {
	case "9774d56d682e549c", "unknown", "null", "nil", "none", "undefined", "0":
		return ""
	}
	for _, r := range id {
		switch r {
		case '0', '-', ':', '_', ' ', '.':
		default:
			return id
		}
	}
	return ""
}
