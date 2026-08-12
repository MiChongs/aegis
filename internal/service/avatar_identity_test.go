package service

import (
	"bytes"
	"image"
	"testing"
)

// 默认头像必须**确定性**：同一个人任何时候画出来都一样。
// 不确定就意味着无法缓存，且用户每次刷新都换一张脸。
func TestDefaultAvatarIsDeterministic(t *testing.T) {
	req := avatarIdentityRequest{Seed: "u1.42", Label: "Zhang Wei", Style: AvatarStyleIdenticon, Size: 128}
	first, ct, err := renderDefaultAvatar(req)
	if err != nil {
		t.Fatalf("渲染失败：%v", err)
	}
	second, _, err := renderDefaultAvatar(req)
	if err != nil {
		t.Fatalf("渲染失败：%v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("同一输入画出了两张不同的图")
	}
	if ct != "image/png" {
		t.Fatalf("默认头像应是 PNG，得到 %s", ct)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(first))
	if err != nil {
		t.Fatalf("产物解不开：%v", err)
	}
	if cfg.Width != 128 || cfg.Height != 128 {
		t.Fatalf("尺寸不对：%d×%d", cfg.Width, cfg.Height)
	}
}

// 不同的人必须画出不同的图，否则"默认头像"退化成一张全站通用的灰块。
func TestDefaultAvatarVariesByOwner(t *testing.T) {
	a, _, _ := renderDefaultAvatar(avatarIdentityRequest{Seed: "u1.42", Style: AvatarStyleIdenticon, Size: 64})
	b, _, _ := renderDefaultAvatar(avatarIdentityRequest{Seed: "u1.43", Style: AvatarStyleIdenticon, Size: 64})
	if bytes.Equal(a, b) {
		t.Fatal("两个不同主体画出了同一张图")
	}
}

// 尺寸必须被夹住：`?s=100000` 不能让服务端去申请 40GB。
func TestDefaultAvatarClampsSize(t *testing.T) {
	data, _, err := renderDefaultAvatar(avatarIdentityRequest{Seed: "u1.1", Style: AvatarStyleIdenticon, Size: 100000})
	if err != nil {
		t.Fatalf("渲染失败：%v", err)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("产物解不开：%v", err)
	}
	if cfg.Width > 512 {
		t.Fatalf("尺寸没有被夹住：%d", cfg.Width)
	}
}

// 首字母的三条规则，尤其是汉字取拼音首字母那一条 ——
// 内嵌字体没有中日韩字形，直接画汉字得到的是一个豆腐块。
func TestAvatarInitial(t *testing.T) {
	cases := map[string]string{
		"alice":           "A",
		"Bob":             "B",
		"7up":             "7",
		"张三":              "Z",
		"王小明":             "W",
		"zhangsan@qq.com": "Z",
		"  spaced":        "S",
		"":                "",
		"!!!":             "",
		"こんにちは":           "", // 假名不在 Han 里，取不到首字母 → 回落几何图案
		"Привет":          "", // 西里尔同理：内嵌字体没有这些字形
		// 日文汉字在 Unicode 里属于 Han，因此会走拼音那条分支（日 → rì）。
		// 这是可以接受的：拿不到语言标注时，按 Han 一律走拼音比画一个豆腐块好。
		"日本語": "R",
	}
	for input, want := range cases {
		if got := avatarInitial(input); got != want {
			t.Errorf("avatarInitial(%q) = %q, want %q", input, got, want)
		}
	}
}

// 取不到首字母时必须回落到几何图案，而不是画一个问号 ——
// 问号在界面上看起来像是加载失败。
func TestInitialsStyleFallsBackToIdenticon(t *testing.T) {
	blank, _, err := renderDefaultAvatar(avatarIdentityRequest{Seed: "u1.5", Label: "!!!", Style: AvatarStyleInitials, Size: 96})
	if err != nil {
		t.Fatalf("渲染失败：%v", err)
	}
	identicon, _, err := renderDefaultAvatar(avatarIdentityRequest{Seed: "u1.5", Label: "", Style: AvatarStyleIdenticon, Size: 96})
	if err != nil {
		t.Fatalf("渲染失败：%v", err)
	}
	if !bytes.Equal(blank, identicon) {
		t.Fatal("画不出首字母时没有回落到几何图案")
	}
}

// 配置值拼错一个字母不该让全站失去默认头像。
func TestNormalizeAvatarDefaultStyle(t *testing.T) {
	cases := map[string]string{
		"":            AvatarStyleIdenticon,
		"  Initials ": AvatarStyleInitials,
		"GRAVATAR":    AvatarStyleGravatar,
		"none":        AvatarStyleNone,
		"identicno":   AvatarStyleIdenticon,
	}
	for input, want := range cases {
		if got := normalizeAvatarDefaultStyle(input); got != want {
			t.Errorf("normalizeAvatarDefaultStyle(%q) = %q, want %q", input, got, want)
		}
	}
}
