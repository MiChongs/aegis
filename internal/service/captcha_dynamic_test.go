package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"image/gif"
	"strings"
	"testing"

	"aegis/internal/config"
	captchadomain "aegis/internal/domain/captcha"
	redisrepo "aegis/internal/repository/redis"
	apperrors "aegis/pkg/errors"

	miniredis "github.com/alicebob/miniredis/v2"
	redislib "github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

func newCaptchaServiceForTest(t *testing.T, mutate func(*config.CaptchaConfig)) (*CaptchaService, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("启动 miniredis 失败: %v", err)
	}
	t.Cleanup(mr.Close)

	client := redislib.NewClient(&redislib.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	captchaCfg := config.CaptchaConfig{Enabled: true}
	captchaCfg.Dynamic.Enabled = true
	captchaCfg.Audio.Enabled = true
	if mutate != nil {
		mutate(&captchaCfg)
	}
	cfg := config.Config{Captcha: config.NormalizeCaptchaConfig(captchaCfg)}
	return NewCaptchaService(cfg, zap.NewNop(), redisrepo.NewCaptchaRepository(client, "test")), mr
}

// TestGenerateDynamicCaptchaIsAnimatedGIF 类型 / MimeType / 产物三者必须都是动画 GIF
// （重构前这条路径出的是单帧 PNG）。
func TestGenerateDynamicCaptchaIsAnimatedGIF(t *testing.T) {
	svc, _ := newCaptchaServiceForTest(t, nil)
	ctx := context.Background()

	result, err := svc.Generate(ctx, captchadomain.TypeDynamic, captchadomain.GenerateRequest{
		Purpose: captchadomain.PurposeLogin,
		Scope:   captchadomain.ScopeUser,
		AppID:   10000,
	})
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}
	if result.Type != captchadomain.TypeDynamic {
		t.Fatalf("下发类型 = %q，应为 dynamic", result.Type)
	}
	if result.MimeType != "image/gif" {
		t.Fatalf("MimeType = %q，应为 image/gif", result.MimeType)
	}
	const prefix = "data:image/gif;base64,"
	if !strings.HasPrefix(result.ImageData, prefix) {
		t.Fatalf("data URL 前缀与 MimeType 对不上: %.32s", result.ImageData)
	}
	if result.ImageWidth <= 0 || result.ImageHeight <= 0 {
		t.Fatalf("未下发尺寸: %dx%d", result.ImageWidth, result.ImageHeight)
	}

	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(result.ImageData, prefix))
	if err != nil {
		t.Fatalf("base64 解码失败: %v", err)
	}
	anim, err := gif.DecodeAll(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("产物不是合法 GIF: %v", err)
	}
	if len(anim.Image) < 2 {
		t.Fatalf("只有 %d 帧，静态图套一层 GIF 容器不算动态验证码", len(anim.Image))
	}
	if got := anim.Image[0].Bounds().Dx(); got != result.ImageWidth {
		t.Fatalf("下发宽度 %d 与实际 %d 不一致", result.ImageWidth, got)
	}
}

// TestDynamicCaptchaVerifiesCaseInsensitively 答案含字母，校验不分大小写
func TestDynamicCaptchaVerifiesCaseInsensitively(t *testing.T) {
	svc, _ := newCaptchaServiceForTest(t, nil)
	ctx := context.Background()

	result, err := svc.Generate(ctx, captchadomain.TypeDynamic, captchadomain.GenerateRequest{
		Purpose: captchadomain.PurposeLogin,
		Scope:   captchadomain.ScopeUser,
		AppID:   10000,
	})
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}
	record, err := svc.repo.GetCaptcha(ctx, result.CaptchaID)
	if err != nil || record == nil {
		t.Fatalf("验证码未落库: %v", err)
	}

	verify := func(answer string) bool {
		ok, err := svc.Verify(ctx, captchadomain.VerifyRequest{
			CaptchaID:       result.CaptchaID,
			Answer:          answer,
			ExpectedAppID:   10000,
			ExpectedPurpose: captchadomain.PurposeLogin,
			ExpectedScope:   captchadomain.ScopeUser,
		})
		if err != nil {
			t.Fatalf("校验出错: %v", err)
		}
		return ok
	}

	if !verify(strings.ToUpper(record.Answer)) {
		t.Fatal("大写答案应通过")
	}
	if !verify(strings.ToLower(record.Answer)) {
		t.Fatal("小写答案应通过")
	}
	if verify(record.Answer + "x") {
		t.Fatal("错误答案不该通过")
	}
}

// TestDynamicAndAudioCaptchaRespectEnableSwitch 两个开关都要有执行点
func TestDynamicAndAudioCaptchaRespectEnableSwitch(t *testing.T) {
	ctx := context.Background()
	req := captchadomain.GenerateRequest{Purpose: captchadomain.PurposeLogin, Scope: captchadomain.ScopeUser}

	cases := []struct {
		name string
		typ  captchadomain.CaptchaType
		off  func(*config.CaptchaConfig)
		code int
	}{
		{"动态", captchadomain.TypeDynamic, func(c *config.CaptchaConfig) { c.Dynamic.Enabled = false }, 40323},
		{"音频", captchadomain.TypeAudio, func(c *config.CaptchaConfig) { c.Audio.Enabled = false }, 40324},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, _ := newCaptchaServiceForTest(t, tc.off)
			_, err := svc.Generate(ctx, tc.typ, req)
			if err == nil {
				t.Fatal("关掉之后仍然生成成功")
			}
			appErr, ok := err.(*apperrors.AppError)
			if !ok || appErr.Code != tc.code {
				t.Fatalf("错误码 = %v，应为 %d", err, tc.code)
			}
		})
	}
}

// TestDynamicCaptchaConfigReachesRenderer 传进来的配置必须真的作用到产物上
func TestDynamicCaptchaConfigReachesRenderer(t *testing.T) {
	svc, _ := newCaptchaServiceForTest(t, nil)
	ctx := context.Background()

	result, err := svc.Generate(ctx, captchadomain.TypeDynamic, captchadomain.GenerateRequest{
		Purpose: captchadomain.PurposeLogin,
		Scope:   captchadomain.ScopeUser,
		Dynamic: &captchadomain.DynamicConfig{
			Length:       7,
			Width:        320,
			Height:       120,
			Frames:       8,
			FrameDelayMs: 120,
			Mode:         "digit",
			Noise:        30,
			Wobble:       40,
		},
	})
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}
	if result.ImageWidth != 320 || result.ImageHeight != 120 {
		t.Fatalf("尺寸未生效: %dx%d", result.ImageWidth, result.ImageHeight)
	}

	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(result.ImageData, "data:image/gif;base64,"))
	if err != nil {
		t.Fatalf("base64 解码失败: %v", err)
	}
	anim, err := gif.DecodeAll(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("解码 GIF 失败: %v", err)
	}
	if len(anim.Image) != 8 {
		t.Fatalf("帧数未生效: %d", len(anim.Image))
	}
	if anim.Delay[0] != 12 { // 120ms = 12 × 1/100 秒
		t.Fatalf("帧间隔未生效: %d", anim.Delay[0])
	}

	record, err := svc.repo.GetCaptcha(ctx, result.CaptchaID)
	if err != nil || record == nil {
		t.Fatalf("验证码未落库: %v", err)
	}
	if len(record.Answer) != 7 {
		t.Fatalf("字符数未生效: %q", record.Answer)
	}
	if strings.Trim(record.Answer, "0123456789") != "" {
		t.Fatalf("digit 档答案里出现了非数字: %q", record.Answer)
	}
}

// TestPreviewDynamicCaptchaLeavesNoRecord 样张不落库，否则它就能被拿去过校验
func TestPreviewDynamicCaptchaLeavesNoRecord(t *testing.T) {
	svc, mr := newCaptchaServiceForTest(t, nil)

	preview, err := svc.PreviewDynamicCaptcha(captchadomain.DynamicConfig{
		Frames: 999, // 越界，应被夹回并在 Applied 里如实回报
		Noise:  0,   // 0 是有效取值，不能被当成"没填"
	})
	if err != nil {
		t.Fatalf("渲染样张失败: %v", err)
	}
	if preview.Answer == "" || preview.ByteSize == 0 || preview.DurationMs <= 0 {
		t.Fatalf("样张信息不完整: %+v", preview)
	}
	if preview.Applied.Frames != preview.Frames {
		t.Fatalf("回报的生效帧数 %d 与实际 %d 不一致", preview.Applied.Frames, preview.Frames)
	}
	if preview.Applied.Frames >= 999 {
		t.Fatalf("越界帧数未被夹取: %d", preview.Applied.Frames)
	}
	if preview.Applied.Noise != 0 {
		t.Fatalf("显式填 0 的干扰强度被改成了 %d", preview.Applied.Noise)
	}

	// Redis 里必须一个键都没多出来
	if keys := mr.Keys(); len(keys) != 0 {
		t.Fatalf("样张不该落库，却写了 %v", keys)
	}
}
