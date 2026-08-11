package receipt

import (
	"bytes"
	"compress/zlib"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/signintech/gopdf"
)

// money / qty / one 让上面的明细表读起来是一张账单，而不是一屏 decimal.RequireFromString。
func money(value string) decimal.Decimal { return decimal.RequireFromString(value) }
func qty(value int64) decimal.Decimal    { return decimal.NewFromInt(value) }

var one = decimal.NewFromInt(1)

func sampleDocument() Document {
	paidAt := time.Date(2026, 3, 21, 10, 30, 0, 0, time.UTC)
	refundAt := time.Date(2026, 3, 25, 9, 0, 0, 0, time.UTC)
	refundAt2 := time.Date(2026, 3, 27, 14, 20, 0, 0, time.UTC)
	return Document{
		Type:     TypeReceipt,
		Status:   StatusPartiallyRefunded,
		Number:   "R-2026-000128",
		OrderNo:  "P120260321103000123456",
		IssuedAt: time.Date(2026, 3, 26, 8, 0, 0, 0, time.UTC),
		Brand:    "Aegis",
		Issuer: Party{
			Name:      "示例商户 Example Merchant",
			Subtitle:  "Application #12",
			Lines:     []string{"上海市浦东新区世纪大道 100 号", "support@example.com"},
			Reference: "MID-000012",
		},
		Customer: Party{
			Name:      "张三 Zhang San",
			Subtitle:  "Customer #90210",
			Email:     "zhangsan@example.com",
			Reference: "UID-90210",
		},
		Currency: "CNY",
		// 明细刻意做出差异，让样张能压到版式的各个角落：
		// 数量为 1 与不为 1、有描述与无描述、超长品名（触发折行）、
		// 中英混排（触发字体选择）、小数与整数单价。
		Items: []LineItem{
			{Name: "黄金会员 12 个月", Description: "Gold membership · 365 days · 含 2000 赠送积分",
				Quantity: one, UnitPrice: money("1288.00"), Amount: money("1288.00")},
			{Name: "积分补充包 500 分", Description: "Points top-up pack · 500 points each",
				Quantity: qty(3), UnitPrice: money("99.90"), Amount: money("299.70")},
			{Name: "短信通知包 10,000 条", Description: "SMS notification bundle · 有效期 12 个月",
				Quantity: qty(2), UnitPrice: money("168.00"), Amount: money("336.00")},
			{Name: "对象存储扩容 100 GB", Description: "Object storage add-on · 按月计费",
				Quantity: qty(6), UnitPrice: money("29.00"), Amount: money("174.00")},
			{Name: "独立应用坐席", Description: "Additional application seat",
				Quantity: qty(5), UnitPrice: money("49.90"), Amount: money("249.50")},
			// 无描述：验证行高按实际内容收缩，而不是恒定两行
			{Name: "自定义域名与证书托管",
				Quantity: one, UnitPrice: money("128.00"), Amount: money("128.00")},
			// 超长品名：验证描述列折行且不挤压右侧金额列
			{Name: "优先技术支持服务（季度）· 7×24 小时响应 · 专属技术顾问 · 季度架构评审",
				Description: "Priority support (quarterly) · 24/7 response · dedicated solution architect",
				Quantity:    one, UnitPrice: money("899.00"), Amount: money("899.00")},
			{Name: "数据导出与合规报表", Description: "Data export & compliance report",
				Quantity: one, UnitPrice: money("259.00"), Amount: money("259.00")},
		},
		// 以下四个数字必须与明细自洽：
		// 小计 3633.20 − 优惠 333.20 + 税额 198.00 = 合计 3498.00。
		// 一份加不起来的样张会让整个版式评审失去意义，改明细时务必一起改。
		Discount:      money("333.20"),
		TaxRate:       decimal.RequireFromString("0.06"),
		TaxAmount:     money("198.00"),
		Total:         money("3498.00"),
		RefundedTotal: money("435.90"),
		Payment: PaymentInfo{
			MethodKey:       "alipay_native",
			MethodLabel:     "Alipay",
			ProviderType:    "page",
			ProviderOrderNo: "2026032122001489121234567890",
			PaidAt:          &paidAt,
			ClientIP:        "203.0.113.42",
		},
		// 两笔退款：验证退款表的多行渲染与「原因」折行
		Refunds: []RefundInfo{
			{Number: "RF20260325000001", Amount: money("336.00"), Status: "success",
				Reason: "客户申请退订短信通知包 / customer cancelled the SMS bundle", At: &refundAt},
			{Number: "RF20260325000002", Amount: money("99.90"), Status: "success",
				Reason: "重复下单，退回一份积分补充包 / duplicate purchase, one points pack returned", At: &refundAt2},
		},
		Metadata: []KeyValue{
			{Key: "meta.purpose", ValueKey: "purpose.vip_purchase"},
			{Key: "meta.appId", Value: "12"},
			{Key: "meta.userId", Value: "90210"},
		},
		Notes:     []Note{{Key: "notes.partiallyRefunded"}},
		VerifyURL: "https://example.com/receipts/R-2026-000128",
	}
}

func newTestRenderer(t *testing.T, cfg FontConfig) *Renderer {
	t.Helper()
	r, err := NewRenderer(Config{Fonts: cfg, Producer: "Aegis Test"})
	if err != nil {
		t.Fatalf("构造渲染器失败：%v", err)
	}
	return r
}

// 样张上的数字必须真的加得起来。
//
// 这是给「以后有人往明细里加一行却忘了改合计」准备的：
// 一份自己都对不上账的样张会让整个版式评审失去意义 —— 评审的人会先怀疑代码算错了。
func TestSampleDocumentArithmeticIsConsistent(t *testing.T) {
	doc := sampleDocument()

	sum := decimal.Zero
	for _, item := range doc.Items {
		// 每行自身也要自洽：数量 × 单价 = 金额
		if want := item.UnitPrice.Mul(item.Quantity); !want.Equal(item.Amount) {
			t.Errorf("明细「%s」：%s × %s = %s，但金额写的是 %s",
				item.Name, item.UnitPrice, item.Quantity, want, item.Amount)
		}
		sum = sum.Add(item.Amount)
	}
	if !sum.Equal(doc.Subtotal()) {
		t.Fatalf("小计 %s 与明细合计 %s 不符", doc.Subtotal(), sum)
	}

	want := sum.Sub(doc.Discount).Add(doc.TaxAmount)
	if !want.Equal(doc.Total) {
		t.Errorf("小计 %s − 优惠 %s + 税额 %s = %s，但合计写的是 %s",
			sum, doc.Discount, doc.TaxAmount, want, doc.Total)
	}
	if rate := doc.TaxAmount.Div(sum.Sub(doc.Discount)); rate.Round(4).Cmp(doc.TaxRate.Round(4)) != 0 {
		t.Errorf("税额对应的税率是 %s，与标注的 %s 不符", rate.Round(4), doc.TaxRate)
	}

	refunded := decimal.Zero
	for _, refund := range doc.Refunds {
		refunded = refunded.Add(refund.Amount)
	}
	if !refunded.Equal(doc.RefundedTotal) {
		t.Errorf("退款明细合计 %s 与已退款总额 %s 不符", refunded, doc.RefundedTotal)
	}
	if doc.NetPaid().IsNegative() {
		t.Error("实收净额为负")
	}
}

// 每个内置语言都必须渲染得出来。这条用例是新增语言时的守门人：
// 目录里写错一个键、缺一条复数形式，都会在这里当场失败。
func TestRendersEveryBundledLocale(t *testing.T) {
	r := newTestRenderer(t, FontConfig{})
	doc := sampleDocument()
	for _, info := range r.Locales() {
		res, err := r.Render(doc, Options{LocalePrefs: []string{info.Tag}, Timezone: time.UTC})
		if err != nil {
			t.Fatalf("%s 渲染失败：%v", info.Tag, err)
		}
		if !bytes.HasPrefix(res.PDF, []byte("%PDF-")) {
			t.Fatalf("%s 输出不是 PDF", info.Tag)
		}
		if res.Pages < 1 {
			t.Fatalf("%s 页数异常：%d", info.Tag, res.Pages)
		}
		assertDrawingsWithinPage(t, info.Tag, res.PDF)
	}
}

// 默认语言必须是英文：凭证可能寄往任何地方，也可能在没有中日韩字体的容器里生成。
func TestDefaultsToEnglish(t *testing.T) {
	r := newTestRenderer(t, FontConfig{})
	for _, prefs := range [][]string{nil, {""}, {"xx-YY"}, {"  "}} {
		res, err := r.Render(sampleDocument(), Options{LocalePrefs: prefs})
		if err != nil {
			t.Fatal(err)
		}
		if res.Locale != "en" {
			t.Fatalf("偏好 %q 应回落到 en，实得 %s", prefs, res.Locale)
		}
	}
}

func TestNegotiatesFromAcceptLanguageHeader(t *testing.T) {
	r := newTestRenderer(t, FontConfig{})
	cases := map[string]string{
		"zh-CN,zh;q=0.9,en;q=0.8": "zh-Hans",
		"zh-TW":                   "zh-Hant",
		"ja-JP":                   "ja",
		"pt-BR,pt;q=0.9":          "pt-BR",
		"de-AT":                   "de",
	}
	for header, want := range cases {
		res, err := r.Render(sampleDocument(), Options{LocalePrefs: []string{header}})
		if err != nil {
			t.Fatal(err)
		}
		// 没有中日韩字体的机器上会降级，此时看协商结果而不是最终语言
		got := res.Locale
		if res.LocaleFallback {
			got = res.RequestedLocale
		}
		if got != want {
			t.Errorf("Accept-Language %q → %s，期望 %s", header, got, want)
		}
	}
}

// 没有中日韩字体时，中文凭证降级成英文而不是产出一份满是豆腐块的 PDF。
func TestFallsBackToEnglishWithoutCJKFont(t *testing.T) {
	r := newTestRenderer(t, FontConfig{DisableSystemScan: true, Dirs: []string{filepath.Join(t.TempDir(), "empty")}})
	if r.SupportsCJK() {
		t.Skip("该环境仍解析到了中日韩字体")
	}
	res, err := r.Render(sampleDocument(), Options{LocalePrefs: []string{"zh-CN"}})
	if err != nil {
		t.Fatalf("缺字体时也必须能出具凭证：%v", err)
	}
	if !res.LocaleFallback || res.Locale != "en" {
		t.Fatalf("期望降级为 en，实得 locale=%s fallback=%v", res.Locale, res.LocaleFallback)
	}
	if res.RequestedLocale != "zh-Hans" {
		t.Errorf("降级前的语言应如实上报，实得 %s", res.RequestedLocale)
	}
	// 用户数据里的中文画不出来这件事必须被上报，不能悄悄替换掉
	if len(res.MissingGlyphs) == 0 {
		t.Error("数据中的中文缺字应当出现在 MissingGlyphs 中")
	}
}

// 有中日韩字体时不降级，且没有任何缺字。
func TestKeepsChineseWhenCJKFontAvailable(t *testing.T) {
	r := newTestRenderer(t, FontConfig{})
	if !r.SupportsCJK() {
		t.Skipf("该环境没有中日韩字体：%s", r.FontStatus())
	}
	res, err := r.Render(sampleDocument(), Options{LocalePrefs: []string{"zh-CN"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.LocaleFallback || res.Locale != "zh-Hans" {
		t.Fatalf("不该降级：locale=%s fallback=%v", res.Locale, res.LocaleFallback)
	}
	if len(res.MissingGlyphs) > 0 {
		t.Errorf("中文字体下不该缺字：%q", string(res.MissingGlyphs))
	}
}

// 纯拉丁凭证走内嵌字体，产出的 PDF 明显小于嵌入中日韩字形子集的版本。
func TestLatinOnlyDocumentAvoidsCJKFontEmbedding(t *testing.T) {
	r := newTestRenderer(t, FontConfig{})
	doc := sampleDocument()
	doc.Issuer = Party{Name: "Example Merchant", Subtitle: "Application #12"}
	doc.Customer = Party{Name: "John Doe", Email: "john@example.com"}
	doc.Items = []LineItem{{Name: "Gold membership", Amount: decimal.RequireFromString("1288.00")}}
	doc.Refunds = nil
	doc.RefundedTotal = decimal.Zero
	doc.Notes = nil

	res, err := r.Render(doc, Options{LocalePrefs: []string{"en"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Font, "embedded:") {
		t.Errorf("纯拉丁内容应使用内嵌字体，实得 %s", res.Font)
	}
	if len(res.MissingGlyphs) > 0 {
		t.Errorf("内嵌拉丁字体不该缺字：%q", string(res.MissingGlyphs))
	}
}

// 内容超过一页时必须自动分页，并且页码占位得到回填。
func TestPaginatesLongDocument(t *testing.T) {
	r := newTestRenderer(t, FontConfig{})
	doc := sampleDocument()
	doc.Items = nil
	for i := range 40 {
		doc.Items = append(doc.Items, LineItem{
			Name:        fmt.Sprintf("Service subscription line %d", i+1),
			Description: "A deliberately long description used to force the item table across several pages of the document.",
			Quantity:    decimal.NewFromInt(int64(i%3 + 1)),
			UnitPrice:   decimal.RequireFromString("19.90"),
			Amount:      decimal.RequireFromString("19.90").Mul(decimal.NewFromInt(int64(i%3 + 1))),
		})
	}
	res, err := r.Render(doc, Options{LocalePrefs: []string{"en"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Pages < 2 {
		t.Fatalf("40 行明细应当分页，实得 %d 页", res.Pages)
	}
	assertDrawingsWithinPage(t, "en/long", res.PDF)
}

// 未支付订单出的是账单而不是收据 —— 给一笔没收到的钱开收据是在伪造凭证。
// 这里验证抬头确实换成了「Invoice」，而不是只看它渲染没报错。
func TestUnpaidOrderRendersAsInvoice(t *testing.T) {
	r := newTestRenderer(t, FontConfig{})
	doc := sampleDocument()
	doc.Type = TypeInvoice
	doc.Status = StatusPending
	doc.Payment.PaidAt = nil
	doc.Refunds = nil
	doc.RefundedTotal = decimal.Zero
	res, err := r.Render(doc, Options{LocalePrefs: []string{"en"}})
	if err != nil {
		t.Fatal(err)
	}
	text := allText(res.PDF)
	if !strings.Contains(text, "INVOICE") {
		t.Error("抬头未换成账单")
	}
	if strings.Contains(text, "RECEIPT") {
		t.Error("未支付的单子上不该出现「收据」字样")
	}
	// 徽标是全大写的，按大小写不敏感比对
	if !strings.Contains(strings.ToUpper(text), "UNPAID") {
		t.Error("状态徽标未标出未支付")
	}
}

// 一份典型收据（单条明细、无退款无附注）必须一页装得下。
// 这是版式松紧的下限：常见场景多印一张纸，成本会乘以订单量。
func TestTypicalReceiptFitsOnePage(t *testing.T) {
	r := newTestRenderer(t, FontConfig{})
	doc := sampleDocument()
	doc.Items = doc.Items[:1]
	doc.Discount, doc.TaxAmount, doc.TaxRate = decimal.Zero, decimal.Zero, decimal.Zero
	doc.Status = StatusPaid
	doc.Refunds, doc.RefundedTotal = nil, decimal.Zero
	doc.Metadata, doc.Notes = nil, nil

	for _, tag := range []string{"en", "zh-Hans", "ru", "ja"} {
		res, err := r.Render(doc, Options{LocalePrefs: []string{tag}, Timezone: time.UTC})
		if err != nil {
			t.Fatalf("%s：%v", tag, err)
		}
		if res.Pages != 1 {
			t.Errorf("%s：典型收据用了 %d 页", tag, res.Pages)
		}
	}
}

// 语言目录必须键完整：以英文为基准，任何一个语言少一条键都会在凭证上露出原始 key。
func TestLocaleCatalogsAreKeyComplete(t *testing.T) {
	base := loadCatalogKeys(t, "en")
	entries, err := localeFS.ReadDir("locales")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		tag := strings.TrimSuffix(entry.Name(), ".json")
		if tag == "en" {
			continue
		}
		keys := loadCatalogKeys(t, tag)
		var missing, extra []string
		for key := range base {
			if _, ok := keys[key]; !ok {
				missing = append(missing, key)
			}
		}
		for key := range keys {
			if _, ok := base[key]; !ok {
				extra = append(extra, key)
			}
		}
		if len(missing) > 0 {
			t.Errorf("%s 缺少 %d 条译文：%v", tag, len(missing), missing)
		}
		if len(extra) > 0 {
			t.Errorf("%s 有 %d 条英文目录里没有的键（多半是拼写错误）：%v", tag, len(extra), extra)
		}
	}
}

func loadCatalogKeys(t *testing.T, tag string) map[string]struct{} {
	t.Helper()
	raw, err := localeFS.ReadFile("locales/" + tag + ".json")
	if err != nil {
		t.Fatalf("读取 %s 目录失败：%v", tag, err)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("解析 %s 目录失败：%v", tag, err)
	}
	keys := make(map[string]struct{}, len(doc))
	for key := range doc {
		if strings.HasPrefix(key, "$") {
			continue
		}
		keys[key] = struct{}{}
	}
	return keys
}

// 每个支付渠道都要有译名。少一个，凭证上的「支付方式」就会显示成渠道键。
func TestEveryPaymentMethodHasLabel(t *testing.T) {
	methods := []string{
		"epay", "rainbow_epay", "xunhupay", "payjs", "qrpay", "vmqpay",
		"alipay_native", "wechat_native", "stripe", "paypal", "balance",
		"paddle", "lemonsqueezy", "razorpay", "coinbase", "square",
	}
	bundle, err := Bundle()
	if err != nil {
		t.Fatal(err)
	}
	for _, info := range bundle.Locales() {
		loc := bundle.For(info.Tag)
		for _, method := range methods {
			if !loc.Has("method." + method) {
				t.Errorf("%s 缺少渠道 %q 的译名", info.Tag, method)
			}
		}
	}
}

// 翻页钩子必须在画首页之前挂上（否则第一次翻页时它还不存在），
// 因此首页也会调到续页页眉 —— 不挡住的话它会与正式抬头叠印在一起。
func TestContinuationHeaderSkipsFirstPage(t *testing.T) {
	r := newTestRenderer(t, FontConfig{})
	bundle, err := Bundle()
	if err != nil {
		t.Fatal(err)
	}
	loc := bundle.For("en")
	doc := sampleDocument()
	doc.normalize()

	pdf := &gopdf.GoPdf{}
	pdf.Start(gopdf.Config{PageSize: *gopdf.PageSizeA4, Unit: gopdf.UnitPT})
	plan := r.plan(&doc, Options{LocalePrefs: []string{"en"}})
	if err := registerFonts(pdf, plan, &glyphRecorder{}); err != nil {
		t.Fatal(err)
	}
	c := newCanvas(pdf, r.theme, plan.bold != nil)
	pdf.AddPage()

	c.page, c.y = 1, 999
	r.drawContinuationHeader(c, &doc, loc)
	if c.y != 999 {
		t.Fatalf("首页不该走续页页眉（游标被改成 %v）", c.y)
	}

	c.page, c.y = 2, 999
	r.drawContinuationHeader(c, &doc, loc)
	if c.y != r.theme.contTop {
		t.Fatalf("续页页眉未生效：游标 = %v，期望 %v", c.y, r.theme.contTop)
	}
}

// EffectiveLocale 必须与真正渲染出来的语言逐字一致。
//
// 订单列表用它标注「这单的凭证是什么语言」，点下载后拿到的却是另一种语言，
// 比不标更糟 —— 用户会以为是下载出了问题。含字体降级的情况也要一致。
func TestEffectiveLocaleMatchesRenderedLocale(t *testing.T) {
	for _, cfg := range []FontConfig{{}, {DisableSystemScan: true, Dirs: []string{filepath.Join(t.TempDir(), "empty")}}} {
		r := newTestRenderer(t, cfg)
		for _, prefs := range [][]string{
			{"zh-CN"},
			{"", "zh-CN", "", "en"}, // 优先级链里夹着空项：真实调用就是这个形状
			{"ja-JP"},
			{"", "", "de-AT"},
			{"xx-YY"},
			nil,
		} {
			res, err := r.Render(sampleDocument(), Options{LocalePrefs: prefs})
			if err != nil {
				t.Fatalf("prefs=%q：%v", prefs, err)
			}
			if got := r.EffectiveLocale(prefs...); got != res.Locale {
				t.Errorf("prefs=%q（cjk=%v）：预告 %s，实际渲染 %s", prefs, r.SupportsCJK(), got, res.Locale)
			}
		}
	}
}

// allText 把整份 PDF 上画出来的文字拼成一段，供内容断言使用。
func allText(pdf []byte) string {
	var b strings.Builder
	for _, runs := range extractPages(pdf) {
		b.WriteString(pageText(runs))
		b.WriteByte('\n')
	}
	return b.String()
}

// ── 版式守卫 ──

var textPosPattern = regexp.MustCompile(`(?m)^(-?\d+\.\d+) (-?\d+\.\d+) TD$`)

// assertDrawingsWithinPage 解出内容流里每一次落笔的坐标，确认没有画到纸外去。
//
// 排版 bug 最常见的形态就是「文字跑到页边之外」——它在代码里看不出来，
// 在 PDF 里也不会报错，只会在打印出来时缺一块。这里直接检查坐标。
func assertDrawingsWithinPage(t *testing.T, label string, pdf []byte) {
	t.Helper()
	th := defaultTheme()
	positions := 0
	for _, stream := range inflateStreams(pdf) {
		for _, match := range textPosPattern.FindAllStringSubmatch(stream, -1) {
			x, _ := strconv.ParseFloat(match[1], 64)
			y, _ := strconv.ParseFloat(match[2], 64)
			positions++
			if x < 0 || x > th.pageW {
				t.Errorf("%s：文字横坐标 %.1f 越出纸张宽度 %.1f", label, x, th.pageW)
			}
			// PDF 坐标原点在左下角，y 越小越靠近页脚
			if y < 0 || y > th.pageH {
				t.Errorf("%s：文字纵坐标 %.1f 越出纸张高度 %.1f", label, y, th.pageH)
			}
		}
	}
	if positions == 0 {
		t.Errorf("%s：内容流里没有任何文字，版式渲染可能整体失效", label)
	}
}

// inflateStreams 解压 PDF 里所有 FlateDecode 流。
func inflateStreams(pdf []byte) []string {
	var out []string
	for rest := pdf; ; {
		start := bytes.Index(rest, []byte("stream"))
		if start < 0 {
			return out
		}
		body := rest[start+len("stream"):]
		body = bytes.TrimLeft(body, "\r\n")
		end := bytes.Index(body, []byte("endstream"))
		if end < 0 {
			return out
		}
		if reader, err := zlib.NewReader(bytes.NewReader(body[:end])); err == nil {
			if data, err := io.ReadAll(reader); err == nil {
				out = append(out, string(data))
			}
			_ = reader.Close()
		}
		// 必须跨过整个 endstream 再找下一个，否则会命中 "endstream" 里的 "stream"
		// 并把紧随其后的真正内容流整个跳过
		rest = body[end+len("endstream"):]
	}
}

// TestWriteSamples 把各语言的样张写到 RECEIPT_SAMPLE_DIR，便于人工核对版式。
// 默认跳过 —— 它产出的是给人看的文件，不是断言。
func TestWriteSamples(t *testing.T) {
	dir := strings.TrimSpace(os.Getenv("RECEIPT_SAMPLE_DIR"))
	if dir == "" {
		t.Skip("未设置 RECEIPT_SAMPLE_DIR")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	r := newTestRenderer(t, FontConfig{})
	t.Logf("字体：%s", r.FontStatus())

	// 另外出一份「典型收据」：单条明细、无退款无附注。
	// 满配样张是用来压版式极限的，日常真正长这样的才是它。
	simple := sampleDocument()
	simple.Items = simple.Items[:1]
	simple.Discount, simple.TaxAmount, simple.TaxRate = decimal.Zero, decimal.Zero, decimal.Zero
	simple.Status = StatusPaid
	simple.Refunds, simple.RefundedTotal = nil, decimal.Zero
	simple.Metadata, simple.Notes = nil, nil
	for _, tag := range []string{"en", "zh-Hans"} {
		res, err := r.Render(simple, Options{LocalePrefs: []string{tag}, Timezone: time.UTC})
		if err != nil {
			t.Fatalf("%s：%v", tag, err)
		}
		path := filepath.Join(dir, "receipt_simple_"+tag+".pdf")
		if err := os.WriteFile(path, res.PDF, 0o600); err != nil {
			t.Fatal(err)
		}
		t.Logf("%-8s → %s（%d 页，%d 字节，典型收据）", "simple/"+tag, path, res.Pages, len(res.PDF))
	}

	for _, info := range r.Locales() {
		res, err := r.Render(sampleDocument(), Options{LocalePrefs: []string{info.Tag}, Timezone: time.UTC})
		if err != nil {
			t.Fatalf("%s：%v", info.Tag, err)
		}
		path := filepath.Join(dir, "receipt_"+info.Tag+".pdf")
		if err := os.WriteFile(path, res.PDF, 0o600); err != nil {
			t.Fatal(err)
		}
		t.Logf("%-8s → %s（%d 页，%d 字节，字型 %s）", info.Tag, path, res.Pages, len(res.PDF), res.Font)
	}
}
