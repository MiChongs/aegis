package receipt

import (
	"regexp"
	"strings"
	"testing"
	"time"
)

// 配色不能只靠「看着还行」。这份凭证会被打印成灰度、会被视力不佳的人阅读，
// 因此每一组「文字 / 底色」都必须过 WCAG 的对比度门槛：
// 正文 4.5:1，大字与图形元素 3:1。
func TestPaletteMeetsContrastRequirements(t *testing.T) {
	th := defaultTheme()
	white := rgb{255, 255, 255}

	// 这张表**穷举渲染器真正画出来的每一组「文字色 / 底色」**，
	// 而不是把调色板里的颜色两两组合 —— 后者会为了让不存在的组合达标而把配色改坏。
	// 新增一处上色时，这里也要补一行；漏了就等于那处颜色没人守。
	body := []struct {
		name   string
		fg, bg rgb
	}{
		// 白底（正文区）
		{"正文 / 白底", th.ink, white},
		{"次级正文 / 白底", th.inkSoft, white},
		{"说明文字 / 白底", th.muted, white},
		{"区块标签 / 白底", th.faint, white},
		{"退款状态·已退款 / 白底", th.warningText, white},
		{"退款状态·失败 / 白底", th.dangerText, white},
		{"退款状态·处理中 / 白底", th.statusTextColor(StatusPending), white},
		// 象牙带（页首、表头、汇总面板）
		{"品牌名 / 象牙带", th.ink, th.band},
		{"副标题与表头标签 / 象牙带", th.muted, th.band},
		{"合计金额 / 象牙面板", th.accentText, th.band},
		{"实收净额 / 象牙面板", th.ink, th.band},
		{"已退款行 / 象牙面板", th.warningText, th.band},
		// 附注卡片
		{"附注正文 / 卡片", th.inkSoft, th.paper},
		// 状态徽标
		{"徽标文字 / 徽标底（已支付）", shade(th.success, 0.22), tint(th.success, 0.88)},
		{"徽标文字 / 徽标底（退款）", shade(th.warning, 0.22), tint(th.warning, 0.88)},
		{"徽标文字 / 徽标底（失败）", shade(th.danger, 0.22), tint(th.danger, 0.88)},
		{"徽标文字 / 徽标底（待支付）", shade(colorNeutral, 0.22), tint(colorNeutral, 0.88)},
	}
	for _, pair := range body {
		if ratio := contrastRatio(pair.fg, pair.bg); ratio < 4.5 {
			t.Errorf("%s 对比度 %.2f，低于正文要求的 4.5", pair.name, ratio)
		}
	}

	// 大字按 3:1 判，这里留出余量取 3.4。品牌标记里的字母是 16.6pt 粗体，属于大字。
	// 其余所有文字都是小字，一律走上面的 4.5:1。
	graphic := []struct {
		name   string
		fg, bg rgb
	}{
		{"品牌标记文字 / 标记底", th.onAccent, th.accentMark},
	}
	for _, pair := range graphic {
		if ratio := contrastRatio(pair.fg, pair.bg); ratio < 3.4 {
			t.Errorf("%s 对比度 %.2f，低于大字要求的 3.4", pair.name, ratio)
		}
	}
}

// 珊瑚原色够画一条线，不够印一行字 —— 这条差异正是 accentText 存在的理由。
// 哪天有人图省事把强调文字换回原色，这里会拦住。
func TestAccentColourIsDarkenedForText(t *testing.T) {
	th := defaultTheme()
	raw := contrastRatio(th.accent, th.band)
	text := contrastRatio(th.accentText, th.band)
	if raw >= 4.5 {
		t.Skip("珊瑚原色已满足正文对比度，无需深色调")
	}
	if text < 4.5 {
		t.Fatalf("强调文字色对比度 %.2f 仍不达标（原色 %.2f）", text, raw)
	}
}

// Lab 空间的浅色调必须比 sRGB 线性插值更「干净」。
// 判据用饱和度：sRGB 插值会把珊瑚的浅调拉灰，Lab 插值保留暖橘感。
func TestTintKeepsChromaInLabSpace(t *testing.T) {
	base := colorAccent
	lab := tint(base, 0.85)

	naive := rgb{
		uint8(float64(base.r) + (255-float64(base.r))*0.85),
		uint8(float64(base.g) + (255-float64(base.g))*0.85),
		uint8(float64(base.b) + (255-float64(base.b))*0.85),
	}
	chroma := func(c rgb) float64 {
		hi := float64(max(max(c.r, c.g), c.b))
		lo := float64(min(min(c.r, c.g), c.b))
		return hi - lo
	}
	if chroma(lab) <= chroma(naive) {
		t.Fatalf("Lab 浅调的彩度 %.1f 没有超过 sRGB 插值的 %.1f，换库没换来任何东西",
			chroma(lab), chroma(naive))
	}
}

// 圆角矩形是用多边形逼近的，必须严格落在给定矩形内 ——
// 溢出会让面板边缘压到相邻内容上。
func TestRoundedRectStaysInsideBounds(t *testing.T) {
	const x, y, w, h, r = 40.0, 100.0, 300.0, 80.0, 6.0
	for _, points := range [][]struct{ X, Y float64 }{} {
		_ = points
	}
	for _, p := range roundedRectPath(x, y, w, h, r) {
		if p.X < x-0.01 || p.X > x+w+0.01 || p.Y < y-0.01 || p.Y > y+h+0.01 {
			t.Fatalf("圆角顶点 (%.2f, %.2f) 越出矩形 [%.0f,%.0f]+[%.0f,%.0f]", p.X, p.Y, x, y, w, h)
		}
	}
	// 半径大于半宽/半高时必须自动收敛，而不是画出一个自交的形状
	for _, p := range roundedRectPath(x, y, 10, 10, 40) {
		if p.X < x-0.01 || p.X > x+10.01 || p.Y < y-0.01 || p.Y > y+10.01 {
			t.Fatalf("超大圆角未收敛：(%.2f, %.2f)", p.X, p.Y)
		}
	}
}

// 有核验地址就必须真的画出二维码。
// 只断言「没报错」是不够的 —— 画了零个模块同样不会报错。
// gopdf 把矩形写成 "x y w h re F"，绘制样式跟在 re 后面同一行
var rectOpPattern = regexp.MustCompile(`(?m)^-?[\d.]+ -?[\d.]+ -?[\d.]+ -?[\d.]+ re [A-Za-z*]+$`)

func TestVerifyURLProducesVectorQRCode(t *testing.T) {
	r := newTestRenderer(t, FontConfig{})
	doc := sampleDocument()

	withQR, err := r.Render(doc, Options{LocalePrefs: []string{"en"}, Timezone: time.UTC})
	if err != nil {
		t.Fatal(err)
	}
	doc.VerifyURL = ""
	withoutQR, err := r.Render(doc, Options{LocalePrefs: []string{"en"}, Timezone: time.UTC})
	if err != nil {
		t.Fatal(err)
	}

	rects := func(pdf []byte) int {
		total := 0
		for _, stream := range pdfStreams(pdf) {
			total += len(rectOpPattern.FindAllString(stream, -1))
		}
		return total
	}
	// 一个能装下核验地址的二维码至少是 29×29 模块，暗模块通常在 300 个以上。
	// 阈值取 200 是为了给不同长度的地址留余量，同时仍能挡住「一个模块都没画」。
	if delta := rects(withQR.PDF) - rects(withoutQR.PDF); delta < 200 {
		t.Fatalf("二维码只多画了 %d 个矩形，几乎可以肯定没渲染出来", delta)
	}
	// 二维码是矢量的：内容流里不该出现图片 XObject
	for _, stream := range pdfStreams(withQR.PDF) {
		if strings.Contains(stream, "/I0 Do") {
			t.Error("二维码被画成了位图，打印放大后会发糊")
		}
	}
}

// 品牌标记取品牌名首字，中日韩品牌也要能取到。
func TestBrandInitial(t *testing.T) {
	cases := map[string]string{
		"Aegis":  "A",
		"  acme": "A",
		"示例商户":   "示",
		"":       "",
		"   ":    "",
		"Ωmega":  "Ω",
	}
	for input, want := range cases {
		if got := brandInitial(input); got != want {
			t.Errorf("brandInitial(%q) = %q，期望 %q", input, got, want)
		}
	}
}
