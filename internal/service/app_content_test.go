package service

import (
	"strings"
	"testing"
	"time"

	appdomain "aegis/internal/domain/app"
)

// 公告正文是管理员用富文本编辑器写的 HTML，最终会被注入到控制台预览、
// 客户端 WebView 和公告邮件里。净化漏一次就是一次存储型 XSS，而且是持久的 ——
// 存进去之后，每一个打开这条公告的人都会中招。
func TestSanitizeRichTextStripsScriptVectors(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		absent  []string
		present []string
	}{
		{
			name:   "内联脚本",
			input:  `<p>正常段落</p><script>fetch('/api/admin/users')</script>`,
			absent: []string{"<script", "fetch("},
			// 净化不是删掉整段：正常内容必须原样留下，否则管理员会以为公告没保存成功
			present: []string{"正常段落"},
		},
		{
			name:    "事件属性",
			input:   `<img src="x" onerror="alert(1)">`,
			absent:  []string{"onerror"},
			present: []string{"<img"},
		},
		{
			name:   "javascript: 协议链接",
			input:  `<a href="javascript:alert(1)">点我</a>`,
			absent: []string{"javascript:"},
			// 协议非法时 bluemonday 去掉的是 href，文字要留下
			present: []string{"点我"},
		},
		{
			name:   "style 属性",
			input:  `<p style="background:url(javascript:alert(1))">文案</p>`,
			absent: []string{"style=", "javascript:"},
		},
		{
			name:    "iframe",
			input:   `<iframe src="https://evil.example"></iframe><p>之后</p>`,
			absent:  []string{"<iframe"},
			present: []string{"之后"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sanitizeRichText(tc.input)
			for _, needle := range tc.absent {
				if strings.Contains(strings.ToLower(got), strings.ToLower(needle)) {
					t.Errorf("净化后仍含有 %q：%s", needle, got)
				}
			}
			for _, needle := range tc.present {
				if !strings.Contains(got, needle) {
					t.Errorf("净化后丢失了正常内容 %q：%s", needle, got)
				}
			}
		})
	}
}

// 编辑器产出的排版标签必须活下来。放行不全的表现是「编辑器里有下划线、
// 存完就没了」，没有任何报错，管理员只会觉得这个编辑器坏了。
func TestSanitizeRichTextKeepsEditorFormatting(t *testing.T) {
	input := `<h2>标题</h2><p><strong>粗</strong><em>斜</em><u>下划线</u><s>删除线</s></p>` +
		`<ul><li>一</li></ul><blockquote>引用</blockquote><pre><code>code</code></pre>` +
		`<p class="text-center">居中</p><a href="https://example.com">外链</a>`
	got := sanitizeRichText(input)

	for _, tag := range []string{"<h2", "<strong", "<em", "<u", "<s", "<ul", "<li", "<blockquote", "<pre", "<code"} {
		if !strings.Contains(got, tag) {
			t.Errorf("排版标签 %s 被净化掉了：%s", tag, got)
		}
	}
	// 对齐、高亮这类排版信息在 tiptap 里是 class，丢了排版就塌了
	if !strings.Contains(got, `class="text-center"`) {
		t.Errorf("class 属性被净化掉了：%s", got)
	}
	// 正文外链会在宿主 WebView 里被点开，必须带 nofollow 与 target
	if !strings.Contains(got, `rel=`) || !strings.Contains(got, `target="_blank"`) {
		t.Errorf("外链缺少 rel/target：%s", got)
	}
}

// 富文本编辑器在用户清空之后留下的是 `<p></p>` —— 长度不为 0，内容一个字没有。
// 只判空串会让这种公告存进去，客户端上就是一条点开什么都没有的公告。
func TestRichTextIsEmptyCatchesBlankMarkup(t *testing.T) {
	blank := []string{"", "   ", "<p></p>", "<p><br></p>", "<div>\n  <p>  </p>\n</div>"}
	for _, input := range blank {
		if !richTextIsEmpty(input) {
			t.Errorf("%q 应当判为空内容", input)
		}
	}
	if richTextIsEmpty("<p>一个字</p>") {
		t.Error("有内容的正文被判成了空")
	}
}

// 摘要按 rune 截断。按字节切会把一个汉字劈成两半，末尾出现乱码方块。
func TestDeriveNoticeSummaryTruncatesByRune(t *testing.T) {
	long := "<p>" + strings.Repeat("公告内容", 100) + "</p>"
	summary := deriveNoticeSummary(long)

	runes := []rune(summary)
	if len(runes) != noticeSummaryMaxRunes+1 { // +1 是省略号
		t.Fatalf("摘要长度应为 %d 个字符加省略号，实际 %d", noticeSummaryMaxRunes, len(runes))
	}
	if !strings.HasSuffix(summary, "…") {
		t.Errorf("截断后应带省略号：%q", summary)
	}
	if !strings.ContainsRune(summary, '公') || strings.ContainsRune(summary, '�') {
		t.Errorf("摘要里出现了半个汉字：%q", summary)
	}
}

// 摘要是给列表和推送用的一段纯文本，不该带标签，也不该带一串 URL。
func TestDeriveNoticeSummaryFlattensMarkup(t *testing.T) {
	summary := deriveNoticeSummary(`<h2>维护通知</h2><p>今晚 <strong>22:00</strong> 起停机，详见 <a href="https://example.com/very/long/path">公告</a>。</p>`)
	for _, unwanted := range []string{"<", ">", "https://"} {
		if strings.Contains(summary, unwanted) {
			t.Errorf("摘要里出现了 %q：%q", unwanted, summary)
		}
	}
	if !strings.Contains(summary, "维护通知") || !strings.Contains(summary, "22:00") {
		t.Errorf("摘要丢了正文信息：%q", summary)
	}
	// 换行与连续空白压成单个空格，否则列表里一行会被撑成三行
	if strings.Contains(summary, "\n") || strings.Contains(summary, "  ") {
		t.Errorf("摘要没有压掉空白：%q", summary)
	}
}

// 零值时间是前端清空输入框时发来的。照原样存下去会得到 0001-01-01，
// 那个时间永远早于 now，于是「不限开始时间」和「从公元一年开始」在库里长得一样 ——
// 前者是没约束，后者是已生效，判定结果相同但语义完全不同，改起来无从下手。
func TestNormalizeContentTimeTreatsZeroAsUnset(t *testing.T) {
	var zero time.Time
	if normalizeContentTime(&zero) != nil {
		t.Error("零值时间应当归一成「未设置」")
	}
	if normalizeContentTime(nil) != nil {
		t.Error("nil 应当保持 nil")
	}
	now := time.Now()
	if got := normalizeContentTime(&now); got == nil || !got.Equal(now) {
		t.Error("有效时间不应被改动")
	}
}

func TestValidateContentWindowRejectsInvertedRange(t *testing.T) {
	start := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	end := start.Add(-time.Hour)

	if err := validateContentWindow(&start, &end); err == nil {
		t.Error("结束早于开始应当被拒绝")
	}
	if err := validateContentWindow(&start, &start); err == nil {
		t.Error("起止相同的窗口一秒都不生效，应当被拒绝")
	}
	if err := validateContentWindow(&start, nil); err != nil {
		t.Errorf("只有开始时间是合法的：%v", err)
	}
	if err := validateContentWindow(nil, nil); err != nil {
		t.Errorf("不限时间窗是合法的：%v", err)
	}
}

// 枚举白名单与控制台的下拉选项必须对得上。多一档少一档的表现是
// 「控制台里选得出来，保存时报不支持」，或者反过来存进一个客户端不认识的值。
func TestContentEnumCatalogs(t *testing.T) {
	if len(appdomain.ValidBannerTypes) != 5 {
		t.Errorf("Banner 展示位应有 5 档，实际 %d", len(appdomain.ValidBannerTypes))
	}
	for _, slot := range []string{
		appdomain.BannerSlotHero, appdomain.BannerSlotPopup,
		appdomain.BannerSlotSplash, appdomain.BannerSlotNotice, appdomain.BannerSlotCard,
	} {
		if _, ok := appdomain.ValidBannerTypes[slot]; !ok {
			t.Errorf("展示位常量 %q 不在白名单里", slot)
		}
	}
	for _, status := range []string{
		appdomain.NoticeStatusDraft, appdomain.NoticeStatusPublished, appdomain.NoticeStatusArchived,
	} {
		if _, ok := appdomain.ValidNoticeStatuses[status]; !ok {
			t.Errorf("公告状态常量 %q 不在白名单里", status)
		}
	}
}
