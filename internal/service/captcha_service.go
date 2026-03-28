package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"regexp"
	"strings"
	"time"

	"aegis/internal/config"
	captchadomain "aegis/internal/domain/captcha"
	redisrepo "aegis/internal/repository/redis"
	apperrors "aegis/pkg/errors"

	dchestcaptcha "github.com/dchest/captcha"
	gojson "github.com/goccy/go-json"
	"github.com/google/uuid"
	"github.com/mojocn/base64Captcha"
	"go.uber.org/zap"
)

// phoneRegex 中国大陆手机号正则（1 开头 11 位数字）
var phoneRegex = regexp.MustCompile(`^1[3-9]\d{9}$`)

// CaptchaService 验证码服务
type CaptchaService struct {
	cfg          config.Config
	log          *zap.Logger
	repo         *redisrepo.CaptchaRepository
	smsProviders map[captchadomain.SMSProviderType]SMSProvider
}

// NewCaptchaService 创建验证码服务
func NewCaptchaService(cfg config.Config, log *zap.Logger, repo *redisrepo.CaptchaRepository) *CaptchaService {
	return &CaptchaService{
		cfg:          cfg,
		log:          log.Named("captcha"),
		repo:         repo,
		smsProviders: make(map[captchadomain.SMSProviderType]SMSProvider),
	}
}

// RegisterSMSProvider 注册短信服务商（支持运行时动态注册）
func (s *CaptchaService) RegisterSMSProvider(providerType captchadomain.SMSProviderType, provider SMSProvider) {
	s.smsProviders[providerType] = provider
}

// ────────────────────── 图形验证码生成 ──────────────────────

// GenerateImageCaptcha 生成图形字符验证码
func (s *CaptchaService) GenerateImageCaptcha(ctx context.Context, req captchadomain.GenerateRequest) (*captchadomain.GenerateResult, error) {
	if !s.cfg.Captcha.Enabled {
		return nil, apperrors.New(40310, http.StatusForbidden, "验证码服务未启用")
	}
	if !s.cfg.Captcha.Image.Enabled {
		return nil, apperrors.New(40311, http.StatusForbidden, "图形验证码未启用")
	}

	imgCfg := s.cfg.Captcha.Image
	driver := base64Captcha.NewDriverString(
		imgCfg.Height,
		imgCfg.Width,
		imgCfg.NoiseCount,
		base64Captcha.OptionShowHollowLine|base64Captcha.OptionShowSlimeLine,
		imgCfg.Length,
		base64Captcha.TxtAlphabet+base64Captcha.TxtNumbers,
		nil, nil, nil,
	)

	captchaID := s.generateID()
	_, content, answer := driver.GenerateIdQuestionAnswer()
	item, err := driver.DrawCaptcha(content)
	if err != nil {
		s.log.Error("生成图形验证码失败", zap.Error(err))
		return nil, apperrors.New(50010, http.StatusInternalServerError, "生成验证码失败")
	}

	b64 := item.EncodeB64string()
	expiresAt := time.Now().Add(s.cfg.Captcha.TTL)

	record := captchadomain.CaptchaRecord{
		Answer:    strings.ToLower(answer),
		Purpose:   req.Purpose,
		Scope:     req.Scope,
		AppID:     req.AppID,
		CreatedAt: time.Now(),
	}
	if err := s.repo.SetCaptcha(ctx, captchaID, record, s.cfg.Captcha.TTL); err != nil {
		s.log.Error("存储验证码失败", zap.Error(err))
		return nil, apperrors.New(50011, http.StatusInternalServerError, "存储验证码失败")
	}

	return &captchadomain.GenerateResult{
		CaptchaID: captchaID,
		ImageData: b64,
		ExpiresAt: expiresAt.Unix(),
	}, nil
}

// GenerateMathCaptcha 生成算术验证码
func (s *CaptchaService) GenerateMathCaptcha(ctx context.Context, req captchadomain.GenerateRequest) (*captchadomain.GenerateResult, error) {
	if !s.cfg.Captcha.Enabled {
		return nil, apperrors.New(40310, http.StatusForbidden, "验证码服务未启用")
	}
	if !s.cfg.Captcha.Math.Enabled {
		return nil, apperrors.New(40312, http.StatusForbidden, "算术验证码未启用")
	}

	mathCfg := s.cfg.Captcha.Math
	driver := base64Captcha.NewDriverMath(
		mathCfg.Height,
		mathCfg.Width,
		4,
		base64Captcha.OptionShowHollowLine|base64Captcha.OptionShowSlimeLine,
		nil, nil, nil,
	)

	captchaID := s.generateID()
	_, content, answer := driver.GenerateIdQuestionAnswer()
	item, err := driver.DrawCaptcha(content)
	if err != nil {
		s.log.Error("生成算术验证码失败", zap.Error(err))
		return nil, apperrors.New(50010, http.StatusInternalServerError, "生成验证码失败")
	}

	b64 := item.EncodeB64string()
	expiresAt := time.Now().Add(s.cfg.Captcha.TTL)

	record := captchadomain.CaptchaRecord{
		Answer:    answer,
		Purpose:   req.Purpose,
		Scope:     req.Scope,
		AppID:     req.AppID,
		CreatedAt: time.Now(),
	}
	if err := s.repo.SetCaptcha(ctx, captchaID, record, s.cfg.Captcha.TTL); err != nil {
		s.log.Error("存储验证码失败", zap.Error(err))
		return nil, apperrors.New(50011, http.StatusInternalServerError, "存储验证码失败")
	}

	return &captchadomain.GenerateResult{
		CaptchaID: captchaID,
		ImageData: b64,
		ExpiresAt: expiresAt.Unix(),
	}, nil
}

// GenerateDigitCaptcha 生成纯数字验证码
func (s *CaptchaService) GenerateDigitCaptcha(ctx context.Context, req captchadomain.GenerateRequest) (*captchadomain.GenerateResult, error) {
	if !s.cfg.Captcha.Enabled {
		return nil, apperrors.New(40310, http.StatusForbidden, "验证码服务未启用")
	}
	if !s.cfg.Captcha.Digit.Enabled {
		return nil, apperrors.New(40313, http.StatusForbidden, "数字验证码未启用")
	}

	digitCfg := s.cfg.Captcha.Digit
	driver := base64Captcha.NewDriverDigit(
		digitCfg.Height,
		digitCfg.Width,
		digitCfg.Length,
		0.7,
		80,
	)

	captchaID := s.generateID()
	_, content, answer := driver.GenerateIdQuestionAnswer()
	item, err := driver.DrawCaptcha(content)
	if err != nil {
		s.log.Error("生成数字验证码失败", zap.Error(err))
		return nil, apperrors.New(50010, http.StatusInternalServerError, "生成验证码失败")
	}

	b64 := item.EncodeB64string()
	expiresAt := time.Now().Add(s.cfg.Captcha.TTL)

	record := captchadomain.CaptchaRecord{
		Answer:    answer,
		Purpose:   req.Purpose,
		Scope:     req.Scope,
		AppID:     req.AppID,
		CreatedAt: time.Now(),
	}
	if err := s.repo.SetCaptcha(ctx, captchaID, record, s.cfg.Captcha.TTL); err != nil {
		s.log.Error("存储验证码失败", zap.Error(err))
		return nil, apperrors.New(50011, http.StatusInternalServerError, "存储验证码失败")
	}

	return &captchadomain.GenerateResult{
		CaptchaID: captchaID,
		ImageData: b64,
		ExpiresAt: expiresAt.Unix(),
	}, nil
}

// GenerateDynamicCaptcha 生成 GIF 动态验证码（使用 dchest/captcha）
func (s *CaptchaService) GenerateDynamicCaptcha(ctx context.Context, req captchadomain.GenerateRequest) (*captchadomain.GenerateResult, error) {
	if !s.cfg.Captcha.Enabled {
		return nil, apperrors.New(40310, http.StatusForbidden, "验证码服务未启用")
	}
	cfg := s.cfg.Captcha.Dynamic
	length := cfg.Length
	if length <= 0 {
		length = 6
	}
	width := cfg.Width
	if width <= 0 {
		width = 240
	}
	height := cfg.Height
	if height <= 0 {
		height = 80
	}

	digits := dchestcaptcha.RandomDigits(length)
	var buf bytes.Buffer
	if _, err := dchestcaptcha.NewImage("", digits, width, height).WriteTo(&buf); err != nil {
		return nil, apperrors.New(50010, http.StatusInternalServerError, "生成动态验证码失败")
	}
	b64 := "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())

	// 构建答案字符串
	answer := digitsToString(digits)
	captchaID := uuid.New().String()
	expiresAt := time.Now().Add(s.cfg.Captcha.TTL)
	record := captchadomain.CaptchaRecord{
		Answer:    answer,
		Purpose:   req.Purpose,
		Scope:     req.Scope,
		AppID:     req.AppID,
		CreatedAt: time.Now(),
	}
	if err := s.repo.SetCaptcha(ctx, captchaID, record, s.cfg.Captcha.TTL); err != nil {
		return nil, apperrors.New(50011, http.StatusInternalServerError, "存储验证码失败")
	}
	return &captchadomain.GenerateResult{
		CaptchaID: captchaID,
		ImageData: b64,
		MimeType:  "image/png",
		ExpiresAt: expiresAt.Unix(),
	}, nil
}

// GenerateAudioCaptcha 生成音频验证码（优先用 gTTS 微服务，降级用 dchest/captcha）
func (s *CaptchaService) GenerateAudioCaptcha(ctx context.Context, req captchadomain.GenerateRequest) (*captchadomain.GenerateResult, error) {
	if !s.cfg.Captcha.Enabled {
		return nil, apperrors.New(40310, http.StatusForbidden, "验证码服务未启用")
	}
	cfg := s.cfg.Captcha.Audio
	length := cfg.Length
	if length <= 0 {
		length = 6
	}
	lang := cfg.Lang
	if lang == "" {
		lang = "zh"
	}

	// 优先调用微服务（gTTS 清晰语音）
	result, err := s.callAudioService(ctx, length, lang)
	if err == nil && result != nil {
		b64 := "data:" + result.MimeType + ";base64," + result.Audio
		captchaID := uuid.New().String()
		expiresAt := time.Now().Add(s.cfg.Captcha.TTL)
		record := captchadomain.CaptchaRecord{
			Answer:    result.Answer,
			Purpose:   req.Purpose,
			Scope:     req.Scope,
			AppID:     req.AppID,
			CreatedAt: time.Now(),
		}
		if err := s.repo.SetCaptcha(ctx, captchaID, record, s.cfg.Captcha.TTL); err != nil {
			return nil, apperrors.New(50011, http.StatusInternalServerError, "存储验证码失败")
		}
		return &captchadomain.GenerateResult{
			CaptchaID: captchaID,
			AudioData: b64,
			MimeType:  result.MimeType,
			ExpiresAt: expiresAt.Unix(),
		}, nil
	}

	// 降级：dchest/captcha WAV
	s.log.Debug("gTTS 音频服务不可用，降级为 dchest/captcha", zap.Error(err))
	digits := dchestcaptcha.RandomDigits(length)
	var buf bytes.Buffer
	if _, err := dchestcaptcha.NewAudio("", digits, "en").WriteTo(&buf); err != nil {
		return nil, apperrors.New(50010, http.StatusInternalServerError, "生成音频验证码失败")
	}
	b64 := "data:audio/wav;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
	answer := digitsToString(digits)
	captchaID := uuid.New().String()
	expiresAt := time.Now().Add(s.cfg.Captcha.TTL)
	record := captchadomain.CaptchaRecord{
		Answer:    answer,
		Purpose:   req.Purpose,
		Scope:     req.Scope,
		AppID:     req.AppID,
		CreatedAt: time.Now(),
	}
	if err := s.repo.SetCaptcha(ctx, captchaID, record, s.cfg.Captcha.TTL); err != nil {
		return nil, apperrors.New(50011, http.StatusInternalServerError, "存储验证码失败")
	}
	return &captchadomain.GenerateResult{
		CaptchaID: captchaID,
		AudioData: b64,
		MimeType:  "audio/wav",
		ExpiresAt: expiresAt.Unix(),
	}, nil
}

// audioServiceResult gTTS 微服务返回
type audioServiceResult struct {
	Audio    string `json:"audio"`
	Answer   string `json:"answer"`
	MimeType string `json:"mimeType"`
	Error    string `json:"error,omitempty"`
}

// callAudioService 调用 gTTS 微服务
func (s *CaptchaService) callAudioService(ctx context.Context, length int, lang string) (*audioServiceResult, error) {
	url := strings.TrimRight(s.cfg.RDKitCaptchaURL, "/") + fmt.Sprintf("/generate-audio?length=%d&lang=%s", length, lang)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("audio service returned %d", resp.StatusCode)
	}
	var result audioServiceResult
	if err := gojson.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	if result.Error != "" {
		return nil, fmt.Errorf("audio service error: %s", result.Error)
	}
	return &result, nil
}

// digitsToString 将 dchest/captcha 的数字切片转为字符串答案
func digitsToString(digits []byte) string {
	result := make([]byte, len(digits))
	for i, d := range digits {
		result[i] = '0' + d
	}
	return string(result)
}

// Generate 统一生成入口，根据类型分发
func (s *CaptchaService) Generate(ctx context.Context, captchaType captchadomain.CaptchaType, req captchadomain.GenerateRequest) (*captchadomain.GenerateResult, error) {
	req.Type = captchaType
	switch captchaType {
	case captchadomain.TypeImage:
		return s.GenerateImageCaptcha(ctx, req)
	case captchadomain.TypeMath:
		return s.GenerateMathCaptcha(ctx, req)
	case captchadomain.TypeDigit:
		return s.GenerateDigitCaptcha(ctx, req)
	case captchadomain.TypeDynamic:
		return s.GenerateDynamicCaptcha(ctx, req)
	case captchadomain.TypeAudio:
		return s.GenerateAudioCaptcha(ctx, req)
	case captchadomain.TypeChiral:
		return s.GenerateChiralCaptcha(ctx, req)
	default:
		return nil, apperrors.New(40001, http.StatusBadRequest, fmt.Sprintf("不支持的验证码类型: %s", captchaType))
	}
}

// ────────────────────── 图形验证码校验 ──────────────────────

// Verify 校验图形/算术/数字验证码
func (s *CaptchaService) Verify(ctx context.Context, req captchadomain.VerifyRequest) (bool, error) {
	if !s.cfg.Captcha.Enabled {
		return false, apperrors.New(40310, http.StatusForbidden, "验证码服务未启用")
	}

	record, err := s.repo.GetCaptcha(ctx, req.CaptchaID)
	if err != nil {
		s.log.Error("获取验证码记录失败", zap.Error(err))
		return false, apperrors.New(50012, http.StatusInternalServerError, "验证码校验失败")
	}
	if record == nil {
		return false, apperrors.New(40020, http.StatusBadRequest, "验证码不存在或已过期")
	}

	// 防暴力破解：最多尝试 5 次
	if record.Attempts >= 5 {
		_ = s.repo.DeleteCaptcha(ctx, req.CaptchaID)
		return false, apperrors.New(42900, http.StatusTooManyRequests, "验证码尝试次数过多，请重新获取")
	}

	// 手性碳验证码已通过 VerifyClick 标记为 VERIFIED
	if record.Answer == "VERIFIED" {
		if req.Clear {
			_ = s.repo.DeleteCaptcha(ctx, req.CaptchaID)
		}
		return true, nil
	}

	answer := strings.TrimSpace(strings.ToLower(req.Answer))
	expected := strings.TrimSpace(strings.ToLower(record.Answer))

	if answer != expected {
		_, _ = s.repo.IncrementCaptchaAttempts(ctx, req.CaptchaID)
		return false, nil
	}

	// 验证成功，清除记录
	if req.Clear {
		_ = s.repo.DeleteCaptcha(ctx, req.CaptchaID)
	}
	return true, nil
}

// ────────────────────── 短信验证码 ──────────────────────

// SendSMSCode 发送短信验证码
//
// 安全校验链（按顺序执行，任一环节失败即拒绝）：
//  1. 手机号格式校验
//  2. 图形验证码前置校验（防机器调用，可配置关闭）
//  3. IP 小时级限流
//  4. IP 日级限流
//  5. 同一手机号发送间隔锁（per-AppID）
//  6. 同一手机号日限额（per-AppID）
//  7. 同一手机号全局日限额（跨 AppID，防轮换攻击）
func (s *CaptchaService) SendSMSCode(ctx context.Context, req captchadomain.SMSSendRequest, providerCfg *captchadomain.SMSProviderConfig) (*captchadomain.SMSSendResult, error) {
	if !s.cfg.Captcha.Enabled || !s.cfg.Captcha.SMS.Enabled {
		return nil, apperrors.New(40314, http.StatusForbidden, "短信验证码未启用")
	}

	smsCfg := s.cfg.Captcha.SMS

	// ── 1. 手机号格式校验 ──
	phone := strings.TrimSpace(req.Phone)
	if !phoneRegex.MatchString(phone) {
		return nil, apperrors.New(40022, http.StatusBadRequest, "手机号格式不正确")
	}

	// ── 2. 图形验证码前置校验（核心防轰炸手段） ──
	if smsCfg.RequireCaptcha {
		if strings.TrimSpace(req.CaptchaID) == "" || strings.TrimSpace(req.CaptchaAnswer) == "" {
			return nil, apperrors.New(40023, http.StatusBadRequest, "发送短信验证码需要先完成图形验证码校验")
		}
		valid, err := s.Verify(ctx, captchadomain.VerifyRequest{
			CaptchaID: req.CaptchaID,
			Answer:    req.CaptchaAnswer,
			Clear:     true, // 一次性消费，防重放
		})
		if err != nil {
			return nil, err
		}
		if !valid {
			return nil, apperrors.New(40024, http.StatusBadRequest, "图形验证码校验失败")
		}
	}

	clientIP := strings.TrimSpace(req.ClientIP)

	// ── 3. IP 小时级限流 ──
	if clientIP != "" {
		ipHourly, err := s.repo.GetIPHourlyCount(ctx, clientIP)
		if err != nil {
			s.log.Error("查询 IP 小时计数失败", zap.Error(err))
			return nil, apperrors.New(50013, http.StatusInternalServerError, "短信服务异常")
		}
		if ipHourly >= int64(smsCfg.IPHourlyLimit) {
			s.log.Warn("IP 小时短信限额触发",
				zap.String("ip", clientIP),
				zap.Int64("count", ipHourly),
			)
			return nil, apperrors.New(42904, http.StatusTooManyRequests, "当前网络环境短信发送过于频繁，请稍后再试")
		}

		// ── 4. IP 日级限流 ──
		ipDaily, err := s.repo.GetIPDailyCount(ctx, clientIP)
		if err != nil {
			s.log.Error("查询 IP 日计数失败", zap.Error(err))
			return nil, apperrors.New(50013, http.StatusInternalServerError, "短信服务异常")
		}
		if ipDaily >= int64(smsCfg.IPDailyLimit) {
			s.log.Warn("IP 日短信限额触发",
				zap.String("ip", clientIP),
				zap.Int64("count", ipDaily),
			)
			return nil, apperrors.New(42905, http.StatusTooManyRequests, "当前网络环境今日短信发送次数已达上限")
		}
	}

	// ── 5. 同一手机号发送间隔锁 ──
	canSend, err := s.repo.CheckSMSSendInterval(ctx, req.AppID, phone)
	if err != nil {
		return nil, apperrors.New(50013, http.StatusInternalServerError, "短信服务异常")
	}
	if !canSend {
		return nil, apperrors.New(42901, http.StatusTooManyRequests, "短信发送过于频繁，请稍后再试")
	}

	// ── 6. 同一手机号日限额（per-AppID） ──
	dailyCount, err := s.repo.GetSMSDailyCount(ctx, req.AppID, phone)
	if err != nil {
		return nil, apperrors.New(50013, http.StatusInternalServerError, "短信服务异常")
	}
	if dailyCount >= int64(smsCfg.DailyLimit) {
		return nil, apperrors.New(42902, http.StatusTooManyRequests, "今日短信发送次数已达上限")
	}

	// ── 7. 全局手机号日限额（跨 AppID，防轮换攻击） ──
	globalCount, err := s.repo.GetGlobalPhoneDailyCount(ctx, phone)
	if err != nil {
		return nil, apperrors.New(50013, http.StatusInternalServerError, "短信服务异常")
	}
	if globalCount >= int64(smsCfg.GlobalPhoneDailyLimit) {
		s.log.Warn("手机号全局日限额触发",
			zap.String("phone", maskPhone(phone)),
			zap.Int64("count", globalCount),
		)
		return nil, apperrors.New(42906, http.StatusTooManyRequests, "该手机号今日短信发送次数已达上限")
	}

	// ── 生成验证码 ──
	code := generateSMSCode(smsCfg.CodeLength)

	// 查找短信服务商
	if providerCfg == nil {
		return nil, apperrors.New(40461, http.StatusNotFound, "未配置短信服务商")
	}
	provider, ok := s.smsProviders[providerCfg.Provider]
	if !ok {
		return nil, apperrors.New(40462, http.StatusNotFound, fmt.Sprintf("不支持的短信服务商: %s", providerCfg.Provider))
	}

	// 发送短信
	requestID, err := provider.Send(ctx, phone, code, providerCfg)
	if err != nil {
		s.log.Error("发送短信失败",
			zap.String("phone", maskPhone(phone)),
			zap.String("provider", string(providerCfg.Provider)),
			zap.Error(err),
		)
		return nil, apperrors.New(50014, http.StatusInternalServerError, "短信发送失败")
	}

	// ── 存储验证码 ──
	record := captchadomain.SMSRecord{
		Code:      code,
		Purpose:   req.Purpose,
		Phone:     phone,
		AppID:     req.AppID,
		CreatedAt: time.Now(),
	}
	if err := s.repo.SetSMSCode(ctx, req.AppID, phone, req.Purpose, record, smsCfg.TTL); err != nil {
		s.log.Error("存储短信验证码失败", zap.Error(err))
		return nil, apperrors.New(50015, http.StatusInternalServerError, "短信验证码存储失败")
	}

	// ── 递增所有维度计数器 ──
	_ = s.repo.SetSMSSendLock(ctx, req.AppID, phone, smsCfg.SendInterval)
	_ = s.repo.IncrementSMSDailyCount(ctx, req.AppID, phone)
	_ = s.repo.IncrementGlobalPhoneDailyCount(ctx, phone)
	if clientIP != "" {
		_ = s.repo.IncrementIPHourlyCount(ctx, clientIP)
		_ = s.repo.IncrementIPDailyCount(ctx, clientIP)
	}

	s.log.Info("短信验证码已发送",
		zap.String("phone", maskPhone(phone)),
		zap.String("purpose", string(req.Purpose)),
		zap.Int64("appId", req.AppID),
		zap.String("ip", clientIP),
	)

	return &captchadomain.SMSSendResult{
		RequestID: requestID,
		ExpiresAt: time.Now().Add(smsCfg.TTL).Unix(),
	}, nil
}

// VerifySMSCode 校验短信验证码
func (s *CaptchaService) VerifySMSCode(ctx context.Context, req captchadomain.SMSVerifyRequest) (bool, error) {
	if !s.cfg.Captcha.Enabled || !s.cfg.Captcha.SMS.Enabled {
		return false, apperrors.New(40314, http.StatusForbidden, "短信验证码未启用")
	}

	record, err := s.repo.GetSMSCode(ctx, req.AppID, req.Phone, req.Purpose)
	if err != nil {
		return false, apperrors.New(50016, http.StatusInternalServerError, "短信验证码校验失败")
	}
	if record == nil {
		return false, apperrors.New(40021, http.StatusBadRequest, "短信验证码不存在或已过期")
	}

	// 防暴力破解
	if record.Attempts >= s.cfg.Captcha.SMS.MaxAttempts {
		_ = s.repo.DeleteSMSCode(ctx, req.AppID, req.Phone, req.Purpose)
		return false, apperrors.New(42903, http.StatusTooManyRequests, "短信验证码尝试次数过多，请重新获取")
	}

	if strings.TrimSpace(req.Code) != record.Code {
		_, _ = s.repo.IncrementSMSAttempts(ctx, req.AppID, req.Phone, req.Purpose)
		return false, nil
	}

	// 验证成功，清除记录
	_ = s.repo.DeleteSMSCode(ctx, req.AppID, req.Phone, req.Purpose)
	return true, nil
}

// ────────────────────── 辅助函数 ──────────────────────

func (s *CaptchaService) generateID() string {
	return uuid.New().String()
}

// generateSMSCode 生成安全的数字验证码
func generateSMSCode(length int) string {
	const digits = "0123456789"
	buf := make([]byte, length)
	for i := range buf {
		n, _ := rand.Int(rand.Reader, big.NewInt(10))
		buf[i] = digits[n.Int64()]
	}
	return string(buf)
}

// maskPhone 手机号脱敏（保留前3后4）
func maskPhone(phone string) string {
	if len(phone) <= 7 {
		return "***"
	}
	return phone[:3] + "****" + phone[len(phone)-4:]
}
