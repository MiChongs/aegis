package receipt

// theme 版式与配色。全部以 PDF 点（1/72 英寸）为单位。
//
// 数值集中在这里而不是散在绘制代码里，是为了让「改一处间距」不必翻遍整个渲染器 ——
// 凭证版式的调整永远是成批的。
type theme struct {
	pageW, pageH float64
	margin       float64
	bandHeight   float64 // 首页抬头色带高度
	bodyTop      float64 // 首页正文起始 y
	contTop      float64 // 续页正文起始 y
	bodyBottom   float64 // 正文可用区底部，越过即翻页
	footerY      float64
	radius       float64 // 圆角半径（面板、表头、徽标）

	// 字号
	sizeDisplay float64 // 文档抬头（RECEIPT）
	sizeBrand   float64
	sizeTotal   float64 // 合计行
	sizeHeading float64 // 区块标题
	sizeLabel   float64 // 表头 / 小标签
	sizeBody    float64
	sizeSmall   float64
	sizeFooter  float64

	// 行高
	lineBody  float64
	lineSmall float64

	// 字距。小标签用正字距拉开，是这套版式里辨识度最高的一处细节：
	// 8.5pt 的全大写标签不拉字距会挤成一团黑块。
	trackingLabel float64

	// 配色
	ink      rgb
	inkSoft  rgb
	muted    rgb
	faint    rgb
	hairline rgb
	band     rgb
	bandEdge rgb
	paper    rgb
	accent   rgb
	// accentText 珊瑚色的深色调。珊瑚原色在象牙底上只有 2.6:1，
	// 够画线不够印字，所有彩色文字走这一支。
	accentText rgb
	// accentMark 品牌标记的底色。比原色再深一档，让上面的白字站得住 ——
	// 标记是整页最显眼的元素，它上面的字母糊掉最扎眼。
	accentMark rgb
	onAccent   rgb
	success    rgb
	warning    rgb
	danger     rgb
	// successText / warningText / dangerText 状态色的**可作文字用**深色调。
	// 原色是给徽标块面用的，直接拿去印 8.8pt 的字对比度不够 ——
	// 与 accentText 同一条理由：颜色能不能画线和能不能印字是两个门槛。
	successText rgb
	warningText rgb
	dangerText  rgb
}

// contentLeft / contentRight / contentWidth 正文可用横向范围。
func (t theme) contentLeft() float64  { return t.margin }
func (t theme) contentRight() float64 { return t.pageW - t.margin }
func (t theme) contentWidth() float64 { return t.pageW - 2*t.margin }

// defaultTheme A4 版式 + Claude 暖调配色。
func defaultTheme() theme {
	const pageW, pageH, margin = 595.28, 841.89, 52
	return theme{
		pageW:      pageW,
		pageH:      pageH,
		margin:     margin,
		bandHeight: 112,
		bodyTop:    142,
		contTop:    84,
		bodyBottom: pageH - 64,
		footerY:    pageH - 46,
		radius:     6,

		sizeDisplay: 25,
		sizeBrand:   15,
		sizeTotal:   14,
		sizeHeading: 8.5,
		sizeLabel:   8,
		sizeBody:    10,
		sizeSmall:   8.8,
		sizeFooter:  7.8,

		lineBody:      14.5,
		lineSmall:     12,
		trackingLabel: 0.9,

		ink:         colorInk,
		inkSoft:     colorInkSoft,
		muted:       colorMuted,
		faint:       colorFaint,
		hairline:    colorHairline,
		band:        colorBand,
		bandEdge:    colorBandEdge,
		paper:       colorPaper,
		accent:      colorAccent,
		accentText:  shade(colorAccent, 0.42),
		accentMark:  shade(colorAccent, 0.16),
		onAccent:    colorOnAccent,
		success:     colorPaid,
		warning:     colorRefund,
		danger:      colorFailed,
		successText: shade(colorPaid, 0.28),
		warningText: shade(colorRefund, 0.34),
		dangerText:  shade(colorFailed, 0.22),
	}
}

// statusTextColor 状态色的文字版本，用于直接印在纸上的状态文字。
func (t theme) statusTextColor(status Status) rgb {
	switch status {
	case StatusPaid:
		return t.successText
	case StatusRefunded, StatusPartiallyRefunded:
		return t.warningText
	case StatusFailed, StatusExpired, StatusCancelled:
		return t.dangerText
	default:
		return colorMuted
	}
}

// statusColor 状态徽标的块面配色。已收款是绿的、退款是琥珀的、失败是砖红的 ——
// 这份凭证经常只被扫一眼，颜色要能独立于文字传达结论。
func (t theme) statusColor(status Status) rgb {
	switch status {
	case StatusPaid:
		return t.success
	case StatusRefunded, StatusPartiallyRefunded:
		return t.warning
	case StatusFailed, StatusExpired, StatusCancelled:
		return t.danger
	default:
		return colorNeutral
	}
}
