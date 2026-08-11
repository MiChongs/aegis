package receipt

import (
	"bytes"
	"embed"
	"fmt"
	"strings"
	"sync"
	"time"

	"aegis/pkg/fontkit"
	"aegis/pkg/i18n"

	"github.com/shopspring/decimal"
	"github.com/signintech/gopdf"
	"golang.org/x/text/language"
)

//go:embed locales/*.json
var localeFS embed.FS

// DefaultLocale 凭证的默认语言。凭证可能寄往任何地方，英文是最不会读不懂的兜底；
// 而且英文只需要内嵌的拉丁字体，因此「凭证出不来」这件事在任何环境下都不会发生。
var DefaultLocale = language.English

// bundleOnce 消息目录全局装配一次。目录是编译期内嵌的只读数据，没有必要每个渲染器一份。
var (
	bundleOnce sync.Once
	sharedBun  *i18n.Bundle
	bundleErr  error
)

// Bundle 凭证消息目录。对外暴露是为了让接口层能下发「支持哪些语言」。
func Bundle() (*i18n.Bundle, error) {
	bundleOnce.Do(func() {
		sharedBun, bundleErr = i18n.LoadFS(localeFS, "locales", DefaultLocale)
	})
	return sharedBun, bundleErr
}

// Config 渲染器配置。
type Config struct {
	Fonts FontConfig
	// Producer 写进 PDF 元数据的生成方，通常是平台名 + 版本
	Producer string
}

// Renderer 凭证渲染器。构造一次全局复用：字体解析（可能读取十几兆的字体文件）
// 只在构造时做一次，之后每份凭证只是把已在内存里的字节交给 PDF 引擎。
type Renderer struct {
	bundle   *i18n.Bundle
	fonts    *fontSet
	theme    theme
	producer string
}

// NewRenderer 构造渲染器。
func NewRenderer(cfg Config) (*Renderer, error) {
	bundle, err := Bundle()
	if err != nil {
		return nil, fmt.Errorf("receipt: 装配消息目录失败：%w", err)
	}
	fonts, err := loadFonts(cfg.Fonts)
	if err != nil {
		return nil, err
	}
	producer := strings.TrimSpace(cfg.Producer)
	if producer == "" {
		producer = "Aegis"
	}
	return &Renderer{bundle: bundle, fonts: fonts, theme: defaultTheme(), producer: producer}, nil
}

// Locales 支持的语言列表，供接口下发给客户端做语言选择。
func (r *Renderer) Locales() []i18n.LocaleInfo { return r.bundle.Locales() }

// Localizer 取某个语言的本地化器。
//
// 对外暴露是为了让凭证的**周边文案**（邮件正文、通知）复用同一份译文与金额格式：
// 一封写着「您的收据已生成」的中文邮件配一份英文 PDF，是最容易出现也最难解释的错位。
func (r *Renderer) Localizer(tag string) *i18n.Localizer { return r.bundle.For(tag) }

// EffectiveLocale 按**与真正渲染完全相同**的规则算出最终语言：先协商，
// 再把「缺中日韩字体会被降级」这件事算进去。
//
// 提供这个方法而不是让调用方自己拼一遍：订单列表上标注的语言若与点下载后
// 实际拿到的不一致，比不标更糟 —— 用户会以为是下载出了问题。
func (r *Renderer) EffectiveLocale(prefs ...string) string {
	tag := r.bundle.Match(prefs...)
	if scriptNeedsCJK(r.bundle.Localizer(tag).Meta().Script) && !r.fonts.hasCJK() {
		return r.bundle.DefaultTag().String()
	}
	return tag.String()
}

// TitleKey 凭证类型对应的译文键，供外部文案引用同一个标题。
func TitleKey(docType DocType) string { return docTitleKey(docType) }

// StatusKey 交易状态对应的译文键。
func StatusKey(status Status) string { return statusKey(status) }

// FontStatus 当前字体能力的一行描述，用于启动日志与运维自检。
func (r *Renderer) FontStatus() string { return r.fonts.describe() }

// FontNotes 字体解析过程中的诊断信息。
func (r *Renderer) FontNotes() []string { return r.fonts.notes }

// SupportsCJK 当前环境能否渲染中日韩凭证。
func (r *Renderer) SupportsCJK() bool { return r.fonts.hasCJK() }

// Options 单次渲染的选项。
type Options struct {
	// LocalePrefs 语言偏好，按优先级排列。每一项都按 Accept-Language 语法解析，
	// 因此可以把请求头整段塞进来。全不认识时用默认语言。
	LocalePrefs []string
	// Timezone 凭证上时间的展示时区，nil 为 UTC
	Timezone *time.Location
}

// Result 渲染结果。
type Result struct {
	PDF    []byte
	Locale string
	// LocaleFallback 是否因为缺少字体而把语言降级成了默认语言
	LocaleFallback bool
	// RequestedLocale 降级前协商到的语言
	RequestedLocale string
	// Font 实际使用的字型描述
	Font string
	// Pages 页数
	Pages int
	// MissingGlyphs 渲染时确实画不出来的字符。非空说明凭证上有内容被替换成了占位符，
	// 调用方应当记一条告警 —— 一份缺字的凭证不该悄悄发出去。
	MissingGlyphs []rune
}

// Render 渲染一份凭证。
func (r *Renderer) Render(doc Document, opts Options) (*Result, error) {
	doc.normalize()

	plan := r.plan(&doc, opts)
	loc := r.bundle.Localizer(plan.tag)
	tz := opts.Timezone
	if tz == nil {
		tz = time.UTC
	}

	pdf := &gopdf.GoPdf{}
	pdf.Start(gopdf.Config{PageSize: *gopdf.PageSizeA4, Unit: gopdf.UnitPT})

	missing := &glyphRecorder{}
	if err := registerFonts(pdf, plan, missing); err != nil {
		return nil, err
	}
	pdf.SetInfo(gopdf.PdfInfo{
		Title:        strings.TrimSpace(loc.T(docTitleKey(doc.Type)) + " " + doc.Number),
		Author:       firstNonEmpty(doc.Issuer.Name, doc.Brand, r.producer),
		Subject:      strings.TrimSpace(doc.OrderNo),
		Creator:      r.producer,
		Producer:     r.producer,
		CreationDate: doc.IssuedAt,
	})

	c := newCanvas(pdf, r.theme, plan.bold != nil)
	c.pageHeader = func(c *canvas) { r.drawContinuationHeader(c, &doc, loc) }
	c.newPage()
	r.drawHeader(c, &doc, loc, tz)
	c.y = r.theme.bodyTop

	r.drawParties(c, &doc, loc)
	r.drawItems(c, &doc, loc)
	r.drawTotals(c, &doc, loc)
	r.drawPayment(c, &doc, loc, tz)
	r.drawRefunds(c, &doc, loc, tz)
	r.drawMetadata(c, &doc, loc)
	r.drawNotes(c, &doc, loc)
	r.drawClosing(c, &doc, loc, tz)

	c.fillPageNumbers(func(page, total int) string {
		return loc.T("footer.page", i18n.Args{"page": page, "total": total})
	})

	var buf bytes.Buffer
	if _, err := pdf.WriteTo(&buf); err != nil {
		return nil, fmt.Errorf("receipt: 输出 PDF 失败：%w", err)
	}
	return &Result{
		PDF:             buf.Bytes(),
		Locale:          plan.tag.String(),
		LocaleFallback:  plan.fallback,
		RequestedLocale: plan.requested.String(),
		Font:            plan.regular.String(),
		Pages:           c.page,
		MissingGlyphs:   missing.runes(),
	}, nil
}

// ── 语言与字体的联合决策 ──

// renderPlan 一次渲染最终采用的语言与字型。
type renderPlan struct {
	tag       language.Tag
	requested language.Tag
	fallback  bool
	regular   *fontkit.Face
	bold      *fontkit.Face
}

// plan 决定「用哪个语言、用哪套字型」。
//
// 两者必须一起定：语言决定了要画哪些字，字体决定了哪些字画得出来。
// 分开定的结果就是「选了中文，然后发现没有中文字体」——
// 那时候纸已经排好了，只能产出一份满是豆腐块的凭证。
func (r *Renderer) plan(doc *Document, opts Options) renderPlan {
	requested := r.bundle.Match(opts.LocalePrefs...)
	plan := renderPlan{tag: requested, requested: requested, regular: r.fonts.latin, bold: r.fonts.latinBold}

	localeNeedsCJK := scriptNeedsCJK(r.bundle.Localizer(requested).Meta().Script)
	dataNeedsCJK := len(r.fonts.latin.Missing(doc.texts()...)) > 0

	switch {
	case !localeNeedsCJK && !dataNeedsCJK:
		// 纯拉丁：用内嵌字体。既最稳妥，产出的 PDF 也最小 —— 不必嵌入中日韩字形子集。
		return plan
	case r.fonts.hasCJK():
		plan.regular = r.fonts.cjk
		plan.bold = r.fonts.cjkBold
		return plan
	case localeNeedsCJK:
		// 没有中日韩字体却要求中文凭证：降级成默认语言，至少标签是读得懂的。
		// 用户数据里的中文仍会缺字，由 MissingGlyphs 如实上报。
		plan.tag = r.bundle.DefaultTag()
		plan.fallback = plan.tag != requested
		return plan
	default:
		// 语言是拉丁的，只是用户数据里带了中文（昵称、商品名）。
		// 照常出具，缺的字上报出去。
		return plan
	}
}

func scriptNeedsCJK(script string) bool {
	switch strings.ToLower(strings.TrimSpace(script)) {
	case "han", "hans", "hant", "kana", "hangul", "cjk":
		return true
	default:
		return false
	}
}

// glyphRecorder 收集渲染过程中确实画不出来的字符。
type glyphRecorder struct {
	mu   sync.Mutex
	seen map[rune]struct{}
}

func (g *glyphRecorder) record(r rune) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.seen == nil {
		g.seen = make(map[rune]struct{})
	}
	g.seen[r] = struct{}{}
}

func (g *glyphRecorder) runes() []rune {
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.seen) == 0 {
		return nil
	}
	out := make([]rune, 0, len(g.seen))
	for r := range g.seen {
		out = append(out, r)
	}
	return out
}

// registerFonts 把选定字型注册进 PDF 引擎。
//
// 没有粗体伴侣时**不注册假粗体**：描边加粗在 9pt 的表头上会糊成一团，
// 层级改用字号与颜色表达（见 canvas.bold）。
func registerFonts(pdf *gopdf.GoPdf, plan renderPlan, missing *glyphRecorder) error {
	option := gopdf.TtfOption{
		Style:           gopdf.Regular,
		OnGlyphNotFound: missing.record,
		// 画不出来的字符替换成问号而不是空格：凭证上留一片空白无从察觉，
		// 一串问号至少能让人看出「这里的字没渲染出来」。
		OnGlyphNotFoundSubstitute: func(rune) rune { return '?' },
	}
	if err := pdf.AddTTFFontDataWithOption(fontFamily, plan.regular.Data, option); err != nil {
		return fmt.Errorf("receipt: 加载字型 %s 失败：%w", plan.regular, err)
	}
	if plan.bold != nil {
		boldOption := option
		boldOption.Style = gopdf.Bold
		if err := pdf.AddTTFFontDataWithOption(fontFamily, plan.bold.Data, boldOption); err != nil {
			return fmt.Errorf("receipt: 加载粗体字型 %s 失败：%w", plan.bold, err)
		}
	}
	return pdf.SetFont(fontFamily, "", 10)
}

// ── 各区块绘制 ──

func (r *Renderer) drawHeader(c *canvas, doc *Document, loc *i18n.Localizer, tz *time.Location) {
	th := r.theme
	brand := firstNonEmpty(doc.Brand, doc.Issuer.Name)

	// 满幅象牙色带 + 底部珊瑚条。整份凭证只有这一处彩色块面，
	// 它替代了传统的「一条分隔线」——线只是分开，色带能立住抬头。
	c.fillRect(0, 0, th.pageW, th.bandHeight, th.band)
	c.fillRect(0, th.bandHeight-3, th.pageW, 3, th.accent)

	const markSize = 32
	c.brandMark(th.contentLeft(), 30, markSize, brandInitial(brand), th.accentMark, th.onAccent)
	textLeft := th.contentLeft() + markSize + 12

	c.styled(th.sizeBrand, true, th.ink)
	c.text(textLeft, 32, brand)
	c.styled(th.sizeSmall, false, th.muted)
	c.text(textLeft, 50, doc.Issuer.Subtitle)

	title := strings.ToUpper(loc.T(docTitleKey(doc.Type)))
	c.styled(th.sizeDisplay, true, th.ink)
	c.textRight(th.contentRight(), 26, title)
	c.styled(th.sizeSmall, false, th.muted)
	c.textRight(th.contentRight(), 58, loc.T("header.number", i18n.Args{"number": doc.Number}))

	c.styled(th.sizeSmall, false, th.muted)
	c.text(th.contentLeft(), 82, loc.T("header.issuedAt", i18n.Args{"time": loc.DateTime(doc.IssuedAt, tz)}))
	c.badge(th.contentRight(), 76, strings.ToUpper(loc.T(statusKey(doc.Status))), th.statusColor(doc.Status))
}

// drawContinuationHeader 续页页眉：只保留「这是谁的哪份凭证」，不重复抬头。
//
// 首页由 drawHeader 负责，这里必须显式跳过。翻页钩子是在画首页**之前**挂上的
// （否则第一次翻页时它还不存在），因此首页也会调到这里 ——
// 不挡住的话续页页眉会与正式抬头叠印在一起。
func (r *Renderer) drawContinuationHeader(c *canvas, doc *Document, loc *i18n.Localizer) {
	if c.page <= 1 {
		return
	}
	th := r.theme
	c.fillRect(0, 0, th.pageW, 62, th.band)
	c.fillRect(0, 60, th.pageW, 2, th.accent)

	brand := firstNonEmpty(doc.Brand, doc.Issuer.Name)
	c.brandMark(th.contentLeft(), 16, 22, brandInitial(brand), th.accentMark, th.onAccent)
	c.styled(th.sizeSmall, true, th.ink)
	c.text(th.contentLeft()+30, 22, brand)
	c.styled(th.sizeSmall, false, th.muted)
	c.textRight(th.contentRight(), 22, loc.T("header.continued", i18n.Args{
		"title": loc.T(docTitleKey(doc.Type)), "number": doc.Number,
	}))
	c.y = th.contTop
}

func (r *Renderer) drawParties(c *canvas, doc *Document, loc *i18n.Localizer) {
	th := r.theme
	colW := (th.contentWidth() - 28) / 2
	left, right := th.contentLeft(), th.contentLeft()+colW+28

	issuer, customer := doc.Issuer, doc.Customer
	// 收款方 / 付款方的名字都可能缺（后台建的订单没有用户），此时用译文占位，
	// 而不是留一片空白让人以为凭证漏印了
	issuer.Name = firstNonEmpty(issuer.Name, loc.T("party.merchant"))
	customer.Name = firstNonEmpty(customer.Name, loc.T("party.guest"))

	top := c.y
	leftBottom := r.drawParty(c, left, top, colW, loc.T("party.issuer"), issuer)
	rightBottom := r.drawParty(c, right, top, colW, loc.T("party.customer"), customer)
	c.y = max(leftBottom, rightBottom) + 18
}

func (r *Renderer) drawParty(c *canvas, x, y, width float64, label string, party Party) float64 {
	th := r.theme
	c.label(x, y, strings.ToUpper(label), th.faint)
	y += 17

	c.styled(th.sizeBody+1.5, true, th.ink)
	for _, line := range c.wrap(party.Name, width) {
		c.text(x, y, line)
		y += th.lineBody
	}

	c.styled(th.sizeSmall, false, th.muted)
	for _, text := range append([]string{party.Subtitle, party.Email, party.Reference}, party.Lines...) {
		if strings.TrimSpace(text) == "" {
			continue
		}
		for _, line := range c.wrap(text, width) {
			c.text(x, y, line)
			y += th.lineSmall
		}
	}
	return y
}

// itemColumns 行项目表的列宽。数量与单价列在没有意义时会被合并进描述列 ——
// 一份只买了一件东西的凭证上，「数量 1 单价 100 金额 100」是三倍的噪音。
type itemColumns struct {
	desc, qty, unit, amount float64
	showQtyUnit             bool
}

func (r *Renderer) itemColumns(doc *Document) itemColumns {
	th := r.theme
	cols := itemColumns{amount: 100, showQtyUnit: false}
	for _, item := range doc.Items {
		if !item.Quantity.IsZero() && !item.Quantity.Equal(decimal.NewFromInt(1)) {
			cols.showQtyUnit = true
			break
		}
	}
	if cols.showQtyUnit {
		cols.qty, cols.unit = 54, 96
	}
	cols.desc = th.contentWidth() - cols.qty - cols.unit - cols.amount
	return cols
}

func (r *Renderer) drawItems(c *canvas, doc *Document, loc *i18n.Localizer) {
	if len(doc.Items) == 0 {
		return
	}
	th := r.theme
	cols := r.itemColumns(doc)

	c.need(72)
	r.drawItemsHead(c, cols, loc)
	for _, item := range doc.Items {
		c.setFont(th.sizeBody, false)
		nameLines := c.wrap(item.Name, cols.desc-10)
		c.setFont(th.sizeSmall, false)
		descLines := c.wrap(item.Description, cols.desc-10)
		height := float64(len(nameLines))*th.lineBody + float64(len(descLines))*th.lineSmall + 12

		if c.need(height) {
			r.drawItemsHead(c, cols, loc)
		}
		y := c.y + 6
		c.styled(th.sizeBody, false, th.ink)
		for _, line := range nameLines {
			c.text(th.contentLeft(), y, line)
			y += th.lineBody
		}
		c.styled(th.sizeSmall, false, th.muted)
		for _, line := range descLines {
			c.text(th.contentLeft(), y, line)
			y += th.lineSmall
		}

		// 同一行里的数字必须与品名**同字号**：gopdf 按行盒顶部定位，
		// 字号不同的文字给同一个 y 会落在不同基线上，表格看起来就是错行的。
		// 层级差异改由颜色表达。
		valueY := c.y + 6
		right := th.contentRight()
		c.styled(th.sizeBody, false, th.ink)
		c.textRight(right, valueY, loc.Money(item.Amount, doc.Currency))
		if cols.showQtyUnit {
			c.styled(th.sizeBody, false, th.muted)
			c.textRight(right-cols.amount, valueY, loc.Money(item.UnitPrice, doc.Currency))
			c.textRight(right-cols.amount-cols.unit, valueY, trimDecimal(item.Quantity))
		}

		c.y += height
		c.rule(c.y-5, th.hairline)
	}
	c.y += 12
}

func (r *Renderer) drawItemsHead(c *canvas, cols itemColumns, loc *i18n.Localizer) {
	th := r.theme
	const headHeight = 24
	c.fillRoundedTop(th.contentLeft(), c.y, th.contentWidth(), headHeight, th.radius, th.band)
	c.label(th.contentLeft()+10, c.y+7.5, strings.ToUpper(loc.T("table.description")), th.muted)
	right := th.contentRight()
	c.labelRight(right-10, c.y+7.5, strings.ToUpper(loc.T("table.amount")), th.muted)
	if cols.showQtyUnit {
		c.labelRight(right-cols.amount, c.y+7.5, strings.ToUpper(loc.T("table.unitPrice")), th.muted)
		c.labelRight(right-cols.amount-cols.unit, c.y+7.5, strings.ToUpper(loc.T("table.quantity")), th.muted)
	}
	c.y += headHeight + 4
}

// totalsRow 汇总面板里的一行。
type totalsRow struct {
	label     string
	value     string
	emphasis  bool
	separator bool
	color     rgb
}

func (r *Renderer) totalsRows(doc *Document, loc *i18n.Localizer) []totalsRow {
	th := r.theme
	rows := []totalsRow{}
	if len(doc.Items) > 1 || doc.Discount.IsPositive() || doc.TaxAmount.IsPositive() {
		rows = append(rows, totalsRow{label: loc.T("totals.subtotal"), value: loc.Money(doc.Subtotal(), doc.Currency), color: th.muted})
	}
	if doc.Discount.IsPositive() {
		rows = append(rows, totalsRow{label: loc.T("totals.discount"), value: "-" + loc.Money(doc.Discount, doc.Currency), color: th.muted})
	}
	if doc.TaxAmount.IsPositive() {
		label := loc.T("totals.tax")
		if doc.TaxRate.IsPositive() {
			label = loc.T("totals.taxWithRate", i18n.Args{"rate": loc.Decimal(doc.TaxRate.Mul(decimal.NewFromInt(100)), 2)})
		}
		rows = append(rows, totalsRow{label: label, value: loc.Money(doc.TaxAmount, doc.Currency), color: th.muted})
	}
	rows = append(rows, totalsRow{
		label:     loc.T("totals.total"),
		value:     loc.MoneyWithCode(doc.Total, doc.Currency),
		emphasis:  true,
		separator: len(rows) > 0,
		color:     th.accentText,
	})
	if doc.RefundedTotal.IsPositive() {
		rows = append(rows,
			totalsRow{label: loc.T("totals.refunded"), value: "-" + loc.Money(doc.RefundedTotal, doc.Currency), color: th.warningText},
			totalsRow{
				label:     loc.T("totals.netPaid"),
				value:     loc.MoneyWithCode(doc.NetPaid(), doc.Currency),
				emphasis:  true,
				separator: true,
				color:     th.ink,
			})
	}
	return rows
}

// drawTotals 右侧的汇总面板 + 左侧的核验二维码。
//
// 汇总做成一块带底色的面板而不是几行右对齐文字：合计是整份凭证上**唯一**
// 被反复查看的数字，给它一个明确的边界，扫一眼就能定位。
//
// 二维码放在面板左边那片空白里：那块地方本来就闲着，而把它塞进下面的
// 「支付信息」区会把两列键值挤到「Payment meth…」这种程度 ——
// 对账字段被截断比没有二维码严重得多。
func (r *Renderer) drawTotals(c *canvas, doc *Document, loc *i18n.Localizer) {
	th := r.theme
	rows := r.totalsRows(doc, loc)

	const panelW, padX, padY = 272.0, 18.0, 16.0
	height := padY * 2
	for _, row := range rows {
		height += th.lineBody + 3
		if row.separator {
			height += 8
		}
	}
	c.need(height + 12)

	if url := strings.TrimSpace(doc.VerifyURL); url != "" {
		const qrSize = 78.0
		if c.drawQRCode(th.contentLeft(), c.y, qrSize, url, th.ink) {
			c.styled(th.sizeFooter, false, th.faint)
			c.textCenter(th.contentLeft()+qrSize/2, c.y+qrSize+2,
				c.truncate(loc.T("footer.scanToVerify"), qrSize+24))
		}
	}

	panelX := th.contentRight() - panelW
	c.fillRounded(panelX, c.y, panelW, height, th.radius, th.band)
	c.strokeRounded(panelX, c.y, panelW, height, th.radius, th.bandEdge, 0.6)

	labelRight := th.contentRight() - panelW/2 - 6
	valueRight := th.contentRight() - padX
	y := c.y + padY
	for _, row := range rows {
		if row.separator {
			c.line(panelX+padX, y-3, valueRight, y-3, th.bandEdge)
			y += 8
		}
		size := th.sizeSmall
		if row.emphasis {
			size = th.sizeTotal
		}
		c.styled(size, row.emphasis, row.color)
		// 标签截断到面板内的可用宽度：俄语的「Промежуточный итог」比中文长一倍，
		// 不截会从面板左边缘溢出去，压到旁边的二维码上。
		c.textRight(labelRight, y, c.truncate(row.label, labelRight-panelX-padX))
		c.styled(size, row.emphasis, row.color)
		c.textRight(valueRight, y, row.value)
		y += th.lineBody + 3
	}
	c.y += height + 20
}

func (r *Renderer) drawPayment(c *canvas, doc *Document, loc *i18n.Localizer, tz *time.Location) {
	pairs := []KeyValue{
		{Key: loc.T("payment.method"), Value: r.methodLabel(loc, doc.Payment)},
		{Key: loc.T("payment.orderNo"), Value: doc.OrderNo},
		{Key: loc.T("payment.transactionId"), Value: doc.Payment.ProviderOrderNo},
		{Key: loc.T("payment.channel"), Value: doc.Payment.ProviderType},
		{Key: loc.T("payment.clientIp"), Value: doc.Payment.ClientIP},
	}
	if doc.Payment.PaidAt != nil {
		pairs = append(pairs, KeyValue{Key: loc.T("payment.paidAt"), Value: loc.DateTime(*doc.Payment.PaidAt, tz)})
	}
	r.drawKeyValues(c, loc.T("payment.section"), pairs)
}

// methodLabel 渠道展示名：优先译文，其次业务侧给的兜底名，最后是渠道键本身。
// 新增渠道时漏了翻译不该让凭证上出现空白。
func (r *Renderer) methodLabel(loc *i18n.Localizer, payment PaymentInfo) string {
	key := "method." + strings.TrimSpace(payment.MethodKey)
	if payment.MethodKey != "" && loc.Has(key) {
		return loc.T(key)
	}
	return firstNonEmpty(payment.MethodLabel, payment.MethodKey)
}

func (r *Renderer) drawRefunds(c *canvas, doc *Document, loc *i18n.Localizer, tz *time.Location) {
	if !doc.HasRefunds() {
		return
	}
	th := r.theme
	r.drawSectionTitle(c, loc.T("refunds.section"))

	if len(doc.Refunds) == 0 {
		c.styled(th.sizeSmall, false, th.muted)
		c.text(th.contentLeft(), c.y, loc.T("refunds.summaryOnly", i18n.Args{
			"amount": loc.Money(doc.RefundedTotal, doc.Currency),
		}))
		c.y += th.lineSmall + 12
		return
	}

	const amountW, statusW, dateW = 96, 92, 120
	numberW := th.contentWidth() - amountW - statusW - dateW

	c.fillRoundedTop(th.contentLeft(), c.y, th.contentWidth(), 22, th.radius, th.band)
	c.label(th.contentLeft()+10, c.y+6.5, strings.ToUpper(loc.T("refunds.number")), th.muted)
	c.label(th.contentLeft()+numberW, c.y+6.5, strings.ToUpper(loc.T("refunds.status")), th.muted)
	c.label(th.contentLeft()+numberW+statusW, c.y+6.5, strings.ToUpper(loc.T("refunds.at")), th.muted)
	c.labelRight(th.contentRight()-10, c.y+6.5, strings.ToUpper(loc.T("refunds.amount")), th.muted)
	c.y += 28

	for _, refund := range doc.Refunds {
		c.need(th.lineBody + th.lineSmall + 10)
		c.styled(th.sizeSmall, false, th.ink)
		c.text(th.contentLeft(), c.y, c.truncate(refund.Number, numberW-8))
		c.styled(th.sizeSmall, false, th.statusTextColor(refundStatusToStatus(refund.Status)))
		c.text(th.contentLeft()+numberW, c.y, c.truncate(loc.T("refund.status."+refund.Status), statusW-8))
		c.styled(th.sizeSmall, false, th.muted)
		at := ""
		if refund.At != nil {
			at = loc.Date(*refund.At, tz)
		}
		c.text(th.contentLeft()+numberW+statusW, c.y, at)
		c.styled(th.sizeSmall, false, th.ink)
		c.textRight(th.contentRight(), c.y, "-"+loc.Money(refund.Amount, doc.Currency))
		c.y += th.lineSmall

		if reason := strings.TrimSpace(refund.Reason); reason != "" {
			c.styled(th.sizeFooter, false, th.faint)
			// 整句走译文而不是「标签 + 冒号 + 值」拼接：中文用全角冒号、
			// 法文的冒号前还要带一个窄空格，标点属于译文的一部分。
			for _, line := range c.wrap(loc.T("refunds.reasonLine", i18n.Args{"reason": reason}), numberW+statusW-8) {
				c.text(th.contentLeft(), c.y, line)
				c.y += th.lineSmall - 1
			}
		}
		c.y += 6
		c.rule(c.y-4, th.hairline)
	}
	c.y += 12
}

// refundStatusToStatus 把退款单状态映射到配色用的凭证状态，
// 让「已退款」是琥珀色、「退款失败」是砖红色，与徽标同一套语义。
func refundStatusToStatus(status string) Status {
	switch status {
	case "success":
		return StatusRefunded
	case "failed", "closed":
		return StatusFailed
	default:
		return StatusPending
	}
}

func (r *Renderer) drawMetadata(c *canvas, doc *Document, loc *i18n.Localizer) {
	if len(doc.Metadata) == 0 {
		return
	}
	pairs := make([]KeyValue, 0, len(doc.Metadata))
	for _, kv := range doc.Metadata {
		value := kv.Value
		if kv.ValueKey != "" {
			value = loc.T(kv.ValueKey)
		}
		pairs = append(pairs, KeyValue{Key: loc.T(kv.Key), Value: value})
	}
	r.drawKeyValues(c, loc.T("metadata.section"), pairs)
}

func (r *Renderer) drawNotes(c *canvas, doc *Document, loc *i18n.Localizer) {
	notes := make([]string, 0, len(doc.Notes))
	for _, note := range doc.Notes {
		text := note.Text
		if note.Key != "" {
			text = loc.T(note.Key)
		}
		if strings.TrimSpace(text) != "" {
			notes = append(notes, text)
		}
	}
	if len(notes) == 0 {
		return
	}
	th := r.theme
	r.drawSectionTitle(c, loc.T("notes.section"))

	// 附注放进一块浅色卡片：它们多半是「本单已部分退款」这类需要被读到的话，
	// 混在正文里会被当成页脚忽略过去。
	c.setFont(th.sizeSmall, false)
	lines := []string{}
	for _, note := range notes {
		lines = append(lines, c.wrap(note, th.contentWidth()-28)...)
	}
	height := float64(len(lines))*th.lineSmall + 20
	c.need(height + 8)
	c.fillRounded(th.contentLeft(), c.y, th.contentWidth(), height, th.radius, th.paper)
	c.strokeRounded(th.contentLeft(), c.y, th.contentWidth(), height, th.radius, th.hairline, 0.6)
	// 左侧珊瑚色竖条：这块卡片与汇总面板底色接近，需要一个记号把两者分开
	c.fillRect(th.contentLeft(), c.y+th.radius, 2.5, height-2*th.radius, th.accent)

	y := c.y + 11
	c.styled(th.sizeSmall, false, th.inkSoft)
	for _, line := range lines {
		c.text(th.contentLeft()+14, y, line)
		y += th.lineSmall
	}
	c.y += height + 12
}

// drawClosing 正文末尾的免责声明与核验地址。
func (r *Renderer) drawClosing(c *canvas, doc *Document, loc *i18n.Localizer, tz *time.Location) {
	th := r.theme
	note := firstNonEmpty(doc.FooterNote, loc.T("footer.disclaimer"))
	lines := []string{note}
	if url := strings.TrimSpace(doc.VerifyURL); url != "" {
		lines = append(lines, loc.T("footer.verify", i18n.Args{"url": url}))
	}
	lines = append(lines, loc.T("footer.generated", i18n.Args{
		"producer": r.producer,
		"time":     loc.DateTime(time.Now().UTC(), tz),
	}))

	// 预留 = 分隔线上下的留白 + 正文行高，与下面真正画的完全一致。
	// 预留多了会让一段本可以放下的页脚被推到下一页，白白多出一张纸。
	c.need(float64(len(lines))*th.lineSmall + 14)
	c.rule(c.y, th.hairline)
	c.y += 12
	c.styled(th.sizeFooter, false, th.faint)
	for _, text := range lines {
		for _, line := range c.wrap(text, th.contentWidth()) {
			c.need(th.lineSmall)
			c.text(th.contentLeft(), c.y, line)
			c.y += th.lineSmall
		}
	}
}

// ── 通用区块 ──

func (r *Renderer) drawSectionTitle(c *canvas, title string) {
	th := r.theme
	// 只预留标题自身 + 一行内容：预留过多会在页面还剩小半页时就提前翻页
	c.need(34)
	upper := strings.ToUpper(title)
	c.label(th.contentLeft(), c.y, upper, th.faint)
	// 标题右侧一段细线拉满整行：让区块之间有明确的起点，
	// 又不像整条分隔线那样把版面切得太碎。
	c.setFont(th.sizeLabel, true)
	c.setTracking(th.trackingLabel)
	titleWidth := c.measure(upper)
	c.setTracking(0)
	c.line(th.contentLeft()+titleWidth+10, c.y+4.5, th.contentRight(), c.y+4.5, th.hairline)
	c.y += 20
}

// drawKeyValues 两列键值表。
//
// 值**折行**而不是截断：交易流水号、上游订单号是对账的关键字段，
// 印一个带省略号的半截流水号等于这份凭证对不上账。
// 标签仍然截断 —— 它只是提示语，个别语言译得偏长时截掉尾巴不影响理解。
func (r *Renderer) drawKeyValues(c *canvas, title string, pairs []KeyValue) {
	visible := make([]KeyValue, 0, len(pairs))
	for _, kv := range pairs {
		if strings.TrimSpace(kv.Value) != "" {
			visible = append(visible, kv)
		}
	}
	if len(visible) == 0 {
		return
	}
	th := r.theme
	r.drawSectionTitle(c, title)

	colW := (th.contentWidth() - 24) / 2
	labelW := colW * 0.44
	valueW := colW - labelW

	for i := 0; i < len(visible); i += 2 {
		cells := visible[i:min(i+2, len(visible))]

		c.setFont(th.sizeSmall, false)
		wrapped := make([][]string, len(cells))
		height := th.lineSmall
		for j, kv := range cells {
			wrapped[j] = c.wrap(kv.Value, valueW)
			if h := float64(len(wrapped[j])) * th.lineSmall; h > height {
				height = h
			}
		}
		height += 6
		c.need(height)

		for j, kv := range cells {
			x := th.contentLeft() + float64(j)*(colW+24)
			c.styled(th.sizeSmall, false, th.faint)
			c.text(x, c.y, c.truncate(kv.Key, labelW-8))
			c.styled(th.sizeSmall, false, th.ink)
			y := c.y
			for _, line := range wrapped[j] {
				c.text(x+labelW, y, line)
				y += th.lineSmall
			}
		}
		c.y += height
	}
	c.y += 12
}

// brandInitial 取品牌名的首字母作为标记内容。
// 中日韩品牌名取第一个字，拉丁名取首字母大写。
func brandInitial(brand string) string {
	for _, r := range strings.TrimSpace(brand) {
		return strings.ToUpper(string(r))
	}
	return ""
}

// ── 小工具 ──

func docTitleKey(t DocType) string { return "doc.type." + string(t) }
func statusKey(s Status) string    { return "status." + string(s) }

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// trimDecimal 去掉无意义的尾随零：数量列上「2」比「2.00」好读。
func trimDecimal(value decimal.Decimal) string {
	if value.IsZero() {
		return ""
	}
	return value.String()
}
