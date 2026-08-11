package httptransport

import (
	"errors"
	"net/http"
	"strings"

	captchadomain "aegis/internal/domain/captcha"
	"aegis/internal/service"
	apperrors "aegis/pkg/errors"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

// 应用侧登录 / 注册验证码接入
//
// 支持验证码类型：image / math / digit / dynamic / audio / chiral（手性碳点选）
//
// 逻辑：
//   1. 查询目标 App 的验证码配置（AppService.GetCaptchaConfig）
//   2. 如果该 App 未启用任何图形验证码类型，则跳过校验（向后兼容旧 App）
//   3. 启用时强制要求 captchaId（answer 可选 —— 手性碳场景下客户端先走 /api/captcha/verify-click
//      完成点选，record 被标记为 "VERIFIED" 后再提交登录；此时 captchaAnswer 传空即可）
//   4. 调用 captchaSvc.Verify 做实际校验：
//        - record.Answer == "VERIFIED"（手性碳预校验通过） → 直接放行
//        - 普通验证码：answer 与 record.Answer 比对
//        - 答案错误 → 40016 并返回 false
//        - 业务错误（过期 / 尝试过多 / 记录不存在）→ apperrors 透传
//   5. 所有失败分支都会完成 response.Error 写入，调用方 return 即可

const (
	errCodeCaptchaRequired = 40015 // 需要先获取验证码
	errCodeCaptchaInvalid  = 40016 // 验证码错误
)

// verifyAppCaptcha 对应用侧请求强制执行验证码校验
//
// 参数：appID 必填；purpose 用于未来审计扩展
// 返回 true 表示放行；false 表示已经 response.Error 过，调用方直接 return
func (h *Handler) verifyAppCaptcha(c *gin.Context, appID int64, captchaID, answer string, purpose captchadomain.Purpose) bool {
	if h == nil || h.captcha == nil || h.app == nil {
		// 验证码/应用服务未注入时视为无保护，保持可用
		return true
	}

	// 读取该 App 的验证码配置
	cfg, err := h.app.GetCaptchaConfig(c.Request.Context(), appID)
	if err != nil || cfg == nil {
		// 配置读取失败按"未开启"降级，不阻塞主业务（管理员感知不到可通过监控排查）
		return true
	}

	// 分场景判定：登录 / 注册分别看 RequireForLogin / RequireForRegister
	// 任一条件不满足（未启用任何类型 或 对应场景关闭）则放行
	if !service.IsCaptchaRequiredForScene(cfg, purpose) {
		return true
	}

	return h.enforceCaptcha(c, appID, captchaID, answer, purpose)
}

// enforceCaptcha 执行一次图形验证码校验。调用方已经判定「本次必须验证」。
//
// 判定与执行分开，是为了让网关侧能用 /config 下发给客户端的那份结论来判定，
// 而不必把判断逻辑再抄一遍 —— 见 verifyGatewayCaptcha。
func (h *Handler) enforceCaptcha(c *gin.Context, appID int64, captchaID, answer string, purpose captchadomain.Purpose) bool {
	if h.captcha == nil {
		// 判定结果是「需要验证码」，却没有验证码服务可用。此时放行等于让一处
		// 已经打开的防护静默失效，因此如实报错而不是当作没配置过。
		response.Error(c, http.StatusServiceUnavailable, 50371, "验证码服务不可用")
		return false
	}

	captchaID = strings.TrimSpace(captchaID)
	answer = strings.TrimSpace(answer)
	// 仅要求 captchaID 非空：手性碳（chiral）点选验证码的 answer 为空时也有效
	// —— 客户端在 /api/captcha/verify-click 成功后，后端会把 record.Answer 标记为 "VERIFIED"，
	//    此时 Verify 会自动放行，不需要前端再传 answer。
	// —— 普通验证码（image/math/digit/dynamic/audio）answer 为空会被下游 Verify 判定为答案错误，
	//    走统一的 40016 "验证码错误" 分支，与人为填错一致，不会泄漏验证码类型。
	if captchaID == "" {
		response.Error(c, http.StatusBadRequest, errCodeCaptchaRequired, "请先完成图形验证码")
		return false
	}

	valid, verr := h.captcha.Verify(c.Request.Context(), captchadomain.VerifyRequest{
		CaptchaID:       captchaID,
		Answer:          answer,
		Clear:           true,
		ExpectedAppID:   appID,
		ExpectedPurpose: purpose,
		ExpectedScope:   captchadomain.ScopeUser,
	})
	if verr != nil {
		var appErr *apperrors.AppError
		if errors.As(verr, &appErr) {
			response.Error(c, appErr.HTTPStatus, appErr.Code, appErr.Message)
			return false
		}
		response.Error(c, http.StatusInternalServerError, 50012, "验证码校验失败")
		return false
	}
	if !valid {
		response.Error(c, http.StatusBadRequest, errCodeCaptchaInvalid, "验证码错误")
		return false
	}
	_ = purpose // 预留给未来的审计钩子
	return true
}
