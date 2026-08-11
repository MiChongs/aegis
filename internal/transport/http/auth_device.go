package httptransport

import (
	"net/http"
	"strings"

	captchadomain "aegis/internal/domain/captcha"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

// 设备信息采集 helper
//
// 字段来源优先级（用于填充 service 层实际使用的值）：
//   1. 请求体字段（deviceId / device）
//   2. HTTP Header（X-Device-Id / X-Device-Name）— 与 body 字段同级可信
//   3. 旧字段 markcode（向后兼容，仅 deviceId 回落）
//   4. User-Agent 自动推断（仅补充 device 名称，不算"客户端显式提供"）
//
// 校验：当应用开启 LoginCheckDevice 策略时，**必须**由客户端显式提供
//       （来源 1~3 任一非空即可），UA 自动推断不视为合法来源。
//
// 返回：(deviceID 设备唯一码, deviceName 设备可读名称)

// resolveClientDevice 只读取客户端显式提供的设备字段（body + header + legacy markcode）
// 不做 UA 回退，用于策略校验
func resolveClientDevice(c *gin.Context, bodyDeviceID, bodyDevice, legacyMarkCode string) (deviceID, device string) {
	deviceID = strings.TrimSpace(bodyDeviceID)
	if deviceID == "" {
		deviceID = strings.TrimSpace(c.GetHeader("X-Device-Id"))
	}
	if deviceID == "" {
		deviceID = strings.TrimSpace(legacyMarkCode)
	}

	device = strings.TrimSpace(bodyDevice)
	if device == "" {
		device = strings.TrimSpace(c.GetHeader("X-Device-Name"))
	}
	return
}

// resolveDeviceInfo 返回最终用于 service 层的设备值
// 在 resolveClientDevice 基础上，若 device 名称仍为空则用 UA 回退补齐
// （只影响 session.Device 的可读名称，不影响策略校验）
func resolveDeviceInfo(c *gin.Context, bodyDeviceID, bodyDevice, legacyMarkCode string) (string, string) {
	deviceID, device := resolveClientDevice(c, bodyDeviceID, bodyDevice, legacyMarkCode)
	if device == "" {
		device = guessDeviceFromUA(c.Request.UserAgent())
	}
	return deviceID, device
}

// enforceDevicePolicy 若目标 App 开启"登录设备检查"，则要求客户端显式提供 deviceId 与 device。
// 合法来源包括：
//   - JSON body：deviceId / device（及兼容字段 markcode）
//   - HTTP Header：X-Device-Id / X-Device-Name
//
// 未开启策略：直接放行，不做校验。
// 返回 true 放行；false 表示已 response.Error，handler 应直接 return。
//
// 设计上与 service 层 validateLoginPolicy 形成双层校验：
// 本层基于"客户端显式来源"早失败并给出精确错误码，避免 UA 推断绕过。
func (h *Handler) enforceDevicePolicy(c *gin.Context, appID int64, bodyDeviceID, bodyDevice, legacyMarkCode string, scene captchadomain.Purpose) bool {
	if h == nil || h.app == nil || appID <= 0 {
		return true
	}
	app, err := h.app.GetApp(c.Request.Context(), appID)
	if err != nil || app == nil {
		// 读取失败不阻塞业务，交由后续步骤处理
		return true
	}
	policy := h.app.ResolvePolicy(app)
	if !policy.LoginCheckDevice {
		return true
	}
	deviceID, device := resolveClientDevice(c, bodyDeviceID, bodyDevice, legacyMarkCode)
	if deviceID == "" {
		response.Error(c, http.StatusBadRequest, 40024,
			"当前应用开启登录设备检查，必须提供 deviceId（请求体 deviceId 或 Header X-Device-Id）")
		return false
	}
	if device == "" {
		response.Error(c, http.StatusBadRequest, 40026,
			"当前应用开启登录设备检查，必须提供 device（请求体 device 或 Header X-Device-Name）")
		return false
	}
	_ = scene // 预留给未来的审计扩展
	return true
}

// enrichDeviceFromDict 根据 device_marketing_names 字典把机器标识翻译成人类可读的
// "厂商 + 空格 + 营销名"（如 Build.MODEL = "SM-G998B" → "Samsung Galaxy S21 Ultra 5G"）。
//
// 参数：
//   deviceID     —— 设备唯一码（iOS 可能是 machine identifier）
//   clientDevice —— **客户端显式传入的 device 原值**（body.device 或 Header X-Device-Name），
//                  未命中字典时将**原样返回**此值（可能为空字符串）。
//                  调用方如需 UA 兜底，请在外层自行降级。
//
// 多平台适配（查询时）：
//   优先级：Header X-Device-Platform → UA 推断 → 默认 android 再 ios
//   Android 前端通常传 Build.MODEL 作为 device 字段
//   iOS 前端通常传 machine identifier（iPhone14,3）作为 device 或 deviceID
//
// 查询顺序：
//   1) 以 clientDevice 作为 key（典型 Android Build.MODEL）
//   2) 以 deviceID 作为 key（典型 iOS machine identifier）
//   命中：Manufacturer 非空 → "Manufacturer " + MarketingName
//         Manufacturer 为空 → 直接使用 MarketingName
//   未命中：返回 clientDevice（客户端原值，保证用户输入永远被尊重）
func (h *Handler) enrichDeviceFromDict(c *gin.Context, deviceID, clientDevice string) string {
	if h == nil || h.deviceMarketing == nil {
		return clientDevice
	}
	// 查询候选 key（去重 + 顺序保持）
	candidates := make([]string, 0, 2)
	seen := map[string]struct{}{}
	for _, v := range []string{clientDevice, deviceID} {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		candidates = append(candidates, v)
	}
	if len(candidates) == 0 {
		return clientDevice
	}

	platforms := resolveDevicePlatforms(c)
	ctx := c.Request.Context()
	for _, plat := range platforms {
		for _, key := range candidates {
			item, err := h.deviceMarketing.Lookup(ctx, plat, key)
			if err != nil || item == nil {
				continue
			}
			name := strings.TrimSpace(item.MarketingName)
			if name == "" {
				continue
			}
			if m := strings.TrimSpace(item.Manufacturer); m != "" {
				return m + " " + name
			}
			return name
		}
	}
	// 未命中：**优先使用前端传入的原值**，不做任何覆盖
	return clientDevice
}

// resolveDevicePlatforms 按可信度排序给出应尝试查询的平台列表
func resolveDevicePlatforms(c *gin.Context) []string {
	// 客户端显式声明
	if p := strings.ToLower(strings.TrimSpace(c.GetHeader("X-Device-Platform"))); p == "ios" || p == "android" {
		if p == "ios" {
			return []string{"ios", "android"}
		}
		return []string{"android", "ios"}
	}
	// UA 推断
	ua := strings.ToLower(c.Request.UserAgent())
	switch {
	case strings.Contains(ua, "iphone"), strings.Contains(ua, "ipad"), strings.Contains(ua, "ipod"):
		return []string{"ios", "android"}
	case strings.Contains(ua, "android"):
		return []string{"android", "ios"}
	}
	// 无上下文：Android 优先（线上 Android 占比更高）
	return []string{"android", "ios"}
}

// enforceDevicePolicyIDOnly 宽松版校验：仅要求 deviceId 由客户端显式提供
// 用于浏览器 302 链路（OAuth Web 起始 / Passkey Begin 等）—— 这些阶段 device 名称无法经由
// 浏览器跳转携带，由最终签发 session 时再次触发 validateLoginPolicy 做严格校验。
// 合法来源：body.deviceId / Header X-Device-Id / legacy markcode；UA 推断不算。
func (h *Handler) enforceDevicePolicyIDOnly(c *gin.Context, appID int64, bodyDeviceID, legacyMarkCode string) bool {
	if h == nil || h.app == nil || appID <= 0 {
		return true
	}
	app, err := h.app.GetApp(c.Request.Context(), appID)
	if err != nil || app == nil {
		return true
	}
	policy := h.app.ResolvePolicy(app)
	if !policy.LoginCheckDevice {
		return true
	}
	deviceID, _ := resolveClientDevice(c, bodyDeviceID, "", legacyMarkCode)
	if deviceID == "" {
		response.Error(c, http.StatusBadRequest, 40024,
			"当前应用开启登录设备检查，必须提供 deviceId（请求体 deviceId 或 Header X-Device-Id）")
		return false
	}
	return true
}

// guessDeviceFromUA 从 UA 粗略推断设备描述
// 不依赖任何第三方库，提取关键词作为 device name 回退
func guessDeviceFromUA(ua string) string {
	if ua == "" {
		return ""
	}
	lower := strings.ToLower(ua)

	// 操作系统 / 平台关键字
	os := ""
	switch {
	case strings.Contains(lower, "android"):
		os = "Android"
	case strings.Contains(lower, "iphone") || strings.Contains(lower, "ios") || strings.Contains(lower, "ipad"):
		os = "iOS"
	case strings.Contains(lower, "macintosh") || strings.Contains(lower, "mac os"):
		os = "macOS"
	case strings.Contains(lower, "windows"):
		os = "Windows"
	case strings.Contains(lower, "linux"):
		os = "Linux"
	}

	// 浏览器 / App 关键字
	browser := ""
	switch {
	case strings.Contains(lower, "micromessenger"):
		browser = "WeChat"
	case strings.Contains(lower, "mqqbrowser"), strings.Contains(lower, "qqbrowser"):
		browser = "QQ Browser"
	case strings.Contains(lower, "edg/"):
		browser = "Edge"
	case strings.Contains(lower, "opr/"), strings.Contains(lower, "opera"):
		browser = "Opera"
	case strings.Contains(lower, "firefox"):
		browser = "Firefox"
	case strings.Contains(lower, "chrome"):
		browser = "Chrome"
	case strings.Contains(lower, "safari"):
		browser = "Safari"
	}

	switch {
	case os != "" && browser != "":
		return browser + " on " + os
	case os != "":
		return os
	case browser != "":
		return browser
	}
	// 截断前 80 字符保底
	if len(ua) > 80 {
		return ua[:80]
	}
	return ua
}
