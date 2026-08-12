package captcha

import (
	"encoding/json"
	"testing"
)

func TestDefaultDynamicConfigIsUsable(t *testing.T) {
	cfg := DefaultDynamicConfig()
	if cfg != cfg.Normalized() {
		t.Fatalf("默认值本身就该是合法值，却被夹成了 %+v", cfg.Normalized())
	}
	if cfg.Frames < 2 {
		t.Fatalf("默认帧数 %d 画不出动画", cfg.Frames)
	}
	if cfg.Mode == "" || cfg.FrameDelayMs <= 0 || cfg.Width <= 0 || cfg.Height <= 0 || cfg.Length <= 0 {
		t.Fatalf("默认值不完整: %+v", cfg)
	}
}

// TestAppConfigMergesPartialDynamicJSON 读取方式是「先铺默认值再反序列化」：
// 缺的字段保留默认值，显式写下的 0 必须留住。
func TestAppConfigMergesPartialDynamicJSON(t *testing.T) {
	defaults := DefaultDynamicConfig()

	cfg := DefaultCaptchaAppConfig()
	if err := json.Unmarshal([]byte(`{"dynamic":{"noise":0,"frames":6}}`), &cfg); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	if cfg.Dynamic.Noise != 0 {
		t.Fatalf("显式写下的 0 被改成了 %d", cfg.Dynamic.Noise)
	}
	if cfg.Dynamic.Frames != 6 {
		t.Fatalf("帧数未生效: %d", cfg.Dynamic.Frames)
	}
	if cfg.Dynamic.Width != defaults.Width || cfg.Dynamic.Mode != defaults.Mode {
		t.Fatalf("未提供的字段应保留默认值，实际 %+v", cfg.Dynamic)
	}

	// 完全没有 dynamic 键的存量配置（这是绝大多数应用的形态）
	legacy := DefaultCaptchaAppConfig()
	if err := json.Unmarshal([]byte(`{"imageEnabled":true}`), &legacy); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	if legacy.Dynamic != defaults {
		t.Fatalf("存量配置应原样拿到默认值，实际 %+v", legacy.Dynamic)
	}
}

func TestDynamicConfigNormalizedClampsAndFillsDefaults(t *testing.T) {
	defaults := DefaultDynamicConfig()

	// 全零 = 从没配过；noise / wobble 的 0 是有效取值
	zero := DynamicConfig{}.Normalized()
	if zero.Width != defaults.Width || zero.Height != defaults.Height ||
		zero.Length != defaults.Length || zero.Frames != defaults.Frames ||
		zero.FrameDelayMs != defaults.FrameDelayMs || zero.Mode != defaults.Mode {
		t.Fatalf("零值未回落到默认: %+v", zero)
	}
	if zero.Noise != 0 || zero.Wobble != 0 {
		t.Fatalf("noise / wobble 的 0 是有效取值，不该被改写: %+v", zero)
	}

	// 越界值夹回区间而不是报错
	wild := DynamicConfig{Width: 99999, Height: -3, Length: 99, Frames: 99999, FrameDelayMs: 1, Mode: "不存在", Noise: 900, Wobble: -900}.Normalized()
	if wild.Width > 640 || wild.Height < 40 || wild.Length > 8 || wild.Noise != 100 {
		t.Fatalf("越界值未被夹取: %+v", wild)
	}
	if wild.Mode != defaults.Mode {
		t.Fatalf("未知字符集档位应回落默认，实际 %q", wild.Mode)
	}
	if wild != wild.Normalized() {
		t.Fatal("Normalized 必须幂等")
	}
}
