package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	paymentdomain "aegis/internal/domain/payment"
	userdomain "aegis/internal/domain/user"
	"aegis/pkg/receipt"

	"github.com/shopspring/decimal"
)

func newReceiptEmailService(t *testing.T) *PaymentService {
	t.Helper()
	renderer, err := receipt.NewRenderer(receipt.Config{Producer: "Aegis Test"})
	if err != nil {
		t.Fatalf("构造渲染器失败：%v", err)
	}
	s := &PaymentService{providers: map[string]paymentProvider{}, receipts: renderer}
	s.receiptCfg.SigningKey = "test-master-key"
	s.receiptCfg.PublicBaseURL = "https://api.example.com"
	s.receiptCfg.DefaultLocale = "en"
	s.receiptCfg.EmailPerDay = 10
	return s
}

func sampleReceiptDoc() receipt.Document {
	return receipt.Document{
		Type:     receipt.TypeReceipt,
		Status:   receipt.StatusPaid,
		Number:   "RCP-P1220260321100000123456",
		OrderNo:  "P1220260321100000123456",
		Currency: "CNY",
		Total:    decimal.RequireFromString("1288.00"),
	}
}

// 签名必须覆盖应用、凭证标识、失效时刻三者。
// 少签任何一项，都能让拿到一条合法链接的人改出另一条合法链接。
func TestReceiptLinkSignatureCoversEveryField(t *testing.T) {
	s := newReceiptEmailService(t)
	const appID, billID = int64(12), "a1b2c3d4e5f60718"
	expires := time.Now().Add(time.Hour).Unix()
	token := s.signReceiptLink(appID, billID, expires)

	if token == "" {
		t.Fatal("签名为空")
	}
	if s.signReceiptLink(appID+1, billID, expires) == token {
		t.Error("换一个应用得到了相同签名")
	}
	if s.signReceiptLink(appID, "ffffffffffffffff", expires) == token {
		t.Error("换一个凭证标识得到了相同签名")
	}
	if s.signReceiptLink(appID, billID, expires+1) == token {
		t.Error("改掉失效时刻得到了相同签名")
	}
	// 换了平台主密钥，旧链接必须全部失效
	other := newReceiptEmailService(t)
	other.receiptCfg.SigningKey = "another-master-key"
	if other.signReceiptLink(appID, billID, expires) == token {
		t.Error("换了主密钥仍能签出相同结果")
	}
}

func TestDownloadSignedReceiptRejectsTamperedLinks(t *testing.T) {
	s := newReceiptEmailService(t)
	const appID, billID = int64(12), "a1b2c3d4e5f60718"

	// 过期链接：签名对但时间到了
	past := time.Now().Add(-time.Minute).Unix()
	_, _, err := s.DownloadSignedReceipt(t.Context(), appID, billID, past, s.signReceiptLink(appID, billID, past))
	if err == nil || !strings.Contains(err.Error(), "过期") {
		t.Errorf("过期链接应被拒绝，实得 %v", err)
	}

	// 伪造签名
	future := time.Now().Add(time.Hour).Unix()
	if _, _, err := s.DownloadSignedReceipt(t.Context(), appID, billID, future, "deadbeef"); err == nil {
		t.Error("伪造签名应被拒绝")
	}
	// 把有效期改远：签名不再匹配
	valid := s.signReceiptLink(appID, billID, future)
	if _, _, err := s.DownloadSignedReceipt(t.Context(), appID, billID, future+86400, valid); err == nil {
		t.Error("篡改失效时刻应被拒绝")
	}
	// 未配置签名密钥时直接拒绝，而不是拿空密钥签出一个人人可复现的签名
	s.receiptCfg.SigningKey = ""
	if _, _, err := s.DownloadSignedReceipt(t.Context(), appID, billID, future, valid); err == nil {
		t.Error("未配置签名密钥时应拒绝服务")
	}
}

// 未配置 API_BASE_URL 时不拼链接：邮件里放相对路径点不开，放了反而误导。
func TestSignedReceiptURLNeedsPublicBaseURL(t *testing.T) {
	s := newReceiptEmailService(t)
	expiry := time.Now().Add(time.Hour)
	if url := s.signedReceiptURL(12, "a1b2c3d4e5f60718", expiry); !strings.HasPrefix(url, "https://api.example.com/api/pay/receipts/12/") {
		t.Fatalf("链接形态异常：%s", url)
	} else if !strings.Contains(url, "token=") || !strings.Contains(url, "expires=") {
		t.Fatalf("链接缺少签名参数：%s", url)
	}
	s.receiptCfg.PublicBaseURL = ""
	if url := s.signedReceiptURL(12, "a1b2c3d4e5f60718", expiry); url != "" {
		t.Fatalf("未配置对外地址时应返回空串，实得 %s", url)
	}
}

// 邮件正文与 PDF 必须同语言：中文邮件配英文 PDF 是最难向用户解释的错位。
func TestReceiptEmailFollowsDocumentLocale(t *testing.T) {
	s := newReceiptEmailService(t)
	paidAt := time.Date(2026, 3, 21, 10, 0, 0, 0, time.UTC)
	order := paidOrder()
	order.PaidAt = &paidAt

	// amount 各不相同正是重点：金额格式必须跟着语言走，
	// 俄语用空格分组、逗号小数点，拿英文的写法去断言等于没测本地化。
	cases := map[string]struct{ subject, body, amount string }{
		"en":      {subject: "Receipt", body: "Order number", amount: "1,288.00"},
		"zh-Hans": {subject: "收据", body: "订单号", amount: "1,288.00"},
		"ja":      {subject: "領収書", body: "注文番号", amount: "1,288.00"},
		"ru":      {subject: "Квитанция", body: "Номер заказа", amount: "288,00"},
	}
	for tag, want := range cases {
		loc := s.receipts.Localizer(tag)
		subject, body := s.buildReceiptEmail(loc, receiptEmailView{
			Brand:       "Aegis",
			Doc:         sampleReceiptDoc(),
			Order:       order,
			Customer:    &userdomain.Profile{Nickname: "张三"},
			Attached:    true,
			DownloadURL: "https://api.example.com/api/pay/receipts/12/abc/download?expires=1&token=x",
			LinkExpiry:  time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
			Timezone:    time.UTC,
		})
		if !strings.Contains(subject, want.subject) {
			t.Errorf("%s 标题未本地化：%q", tag, subject)
		}
		if !strings.Contains(body, want.body) {
			t.Errorf("%s 正文未本地化（缺 %q）", tag, want.body)
		}
		if !strings.Contains(body, `<html lang="`+tag+`"`) {
			t.Errorf("%s 的 html lang 标记不正确", tag)
		}
		// 订单号与金额必须出现在正文里，否则这封信等于只说了「有份收据」
		if !strings.Contains(body, order.OrderNo) {
			t.Errorf("%s 正文缺少订单号", tag)
		}
		if !strings.Contains(body, want.amount) {
			t.Errorf("%s 正文缺少按该语言格式化的金额 %q", tag, want.amount)
		}
		// 币种代码必须出现：¥ 既是人民币也是日元，$ 有十几个国家在用
		if !strings.Contains(body, "CNY") {
			t.Errorf("%s 正文金额缺少币种代码", tag)
		}
	}
}

// 带不带附件，正文的措辞必须跟着变；下载链接则恒在。
func TestReceiptEmailWordingMatchesAttachmentCapability(t *testing.T) {
	s := newReceiptEmailService(t)
	loc := s.receipts.Localizer("en")
	view := receiptEmailView{
		Brand:       "Aegis",
		Doc:         sampleReceiptDoc(),
		Order:       paidOrder(),
		Attached:    true,
		DownloadURL: "https://api.example.com/receipt",
		LinkExpiry:  time.Now().Add(time.Hour),
		Timezone:    time.UTC,
	}
	_, attached := s.buildReceiptEmail(loc, view)
	if !strings.Contains(attached, "attached to this email") {
		t.Error("带附件时正文应说明附件")
	}

	view.Attached = false
	_, linkOnly := s.buildReceiptEmail(loc, view)
	if strings.Contains(linkOnly, "attached to this email") {
		t.Error("不带附件时不能声称有附件")
	}
	// 两种情况都必须给出下载入口：附件会被邮件网关剥离，链接是最后一条退路
	for name, body := range map[string]string{"attached": attached, "linkOnly": linkOnly} {
		if !strings.Contains(body, "https://api.example.com/receipt") {
			t.Errorf("%s 正文缺少下载链接", name)
		}
	}
}

// 没有收件人昵称时用译文占位，而不是在邮件里出现「Hi ,」。
func TestReceiptEmailGreetingFallsBack(t *testing.T) {
	s := newReceiptEmailService(t)
	_, body := s.buildReceiptEmail(s.receipts.Localizer("en"), receiptEmailView{
		Brand: "Aegis", Doc: sampleReceiptDoc(), Order: paidOrder(), Timezone: time.UTC,
	})
	if strings.Contains(body, "Hi ,") {
		t.Error("缺昵称时出现了空称呼")
	}
	if !strings.Contains(body, "Guest") {
		t.Error("缺昵称时应使用译文占位")
	}
}

// 订单列表上的 receipt 区块要随订单状态给出正确的凭证类型与入口。
func TestOrderReceiptBlockReflectsOrderState(t *testing.T) {
	s := newReceiptEmailService(t)
	info := orderReceiptContext{locale: "en", emailable: true}

	order := paidOrder()
	block := s.buildOrderReceipt(t.Context(), order, info)
	if !block.Available || block.DocumentType != string(receipt.TypeReceipt) {
		t.Fatalf("已支付订单应出收据：%+v", block)
	}
	if block.DownloadURL != "/api/pay/orders/"+order.OrderNo+"/receipt" {
		t.Errorf("下载地址不正确：%s", block.DownloadURL)
	}
	if block.EmailURL != "/api/pay/orders/"+order.OrderNo+"/receipt/email" {
		t.Errorf("寄送地址不正确：%s", block.EmailURL)
	}
	if !block.Emailable || block.EmailHint != "" {
		t.Errorf("已绑邮箱时应可寄送：%+v", block)
	}

	order.Status = "pending"
	if block := s.buildOrderReceipt(t.Context(), order, info); block.DocumentType != string(receipt.TypeInvoice) {
		t.Errorf("未支付订单应出账单，实得 %s", block.DocumentType)
	}

	// 没绑邮箱时不但要置为不可寄送，还要给出可直接展示的原因
	noEmail := orderReceiptContext{locale: "en", emailHint: "账号尚未绑定邮箱"}
	if block := s.buildOrderReceipt(t.Context(), paidOrder(), noEmail); block.Emailable || block.EmailHint == "" {
		t.Errorf("未绑邮箱时应给出原因：%+v", block)
	}

	// 渲染器不可用时整块置为不可用，而不是给出一堆点了会 503 的地址
	broken := &PaymentService{providers: map[string]paymentProvider{}}
	if block := broken.buildOrderReceipt(t.Context(), paidOrder(), info); block.Available || block.DownloadURL != "" {
		t.Errorf("渲染器不可用时不该给出入口：%+v", block)
	}
}

// 应用级的凭证语言必须当场校验：存一个渲染器不认识的语言，
// 表现是几个月后某封凭证邮件悄悄变成了英文。
func TestReceiptLocaleValidation(t *testing.T) {
	for _, tag := range []string{"en", "zh-Hans", "pt-BR", "ru"} {
		if !receiptLocaleSupported(tag) {
			t.Errorf("%s 应被接受", tag)
		}
	}
	for _, tag := range []string{"xx", "zh-CN", "klingon"} {
		if receiptLocaleSupported(tag) {
			t.Errorf("%s 不该被接受（必须是目录里的精确标签）", tag)
		}
	}
}

// 凭证邮件的用途标识是投递留痕与频次限制的共同维度，改动会让历史统计对不上。
func TestReceiptEmailPurposeIsStable(t *testing.T) {
	if receiptEmailPurpose != "payment_receipt" {
		t.Fatalf("用途标识被改动：%s", receiptEmailPurpose)
	}
}

var _ = paymentdomain.ReceiptOptions{}

// TestWriteReceiptEmailSamples 把各语言的凭证邮件正文写到 RECEIPT_SAMPLE_DIR，
// 便于人工核对排版与措辞。默认跳过 —— 它产出的是给人看的文件，不是断言。
func TestWriteReceiptEmailSamples(t *testing.T) {
	dir := strings.TrimSpace(os.Getenv("RECEIPT_SAMPLE_DIR"))
	if dir == "" {
		t.Skip("未设置 RECEIPT_SAMPLE_DIR")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	s := newReceiptEmailService(t)
	paidAt := time.Date(2026, 3, 21, 10, 0, 0, 0, time.UTC)
	order := paidOrder()
	order.PaidAt = &paidAt
	for _, info := range s.receipts.Locales() {
		subject, body := s.buildReceiptEmail(s.receipts.Localizer(info.Tag), receiptEmailView{
			Brand:       "Aegis",
			Doc:         sampleReceiptDoc(),
			Order:       order,
			Customer:    &userdomain.Profile{Nickname: "张三 Zhang San"},
			Attached:    true,
			DownloadURL: "https://api.example.com/api/pay/receipts/12/9f2c/download?expires=1774000000&token=abcdef",
			LinkExpiry:  time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC),
			Timezone:    time.UTC,
		})
		path := filepath.Join(dir, "receipt_email_"+info.Tag+".html")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Logf("%-8s %s\n         → %s", info.Tag, subject, path)
	}
}
