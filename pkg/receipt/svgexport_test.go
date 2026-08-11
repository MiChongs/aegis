package receipt

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

// TestWriteSVGPreviews 把样张的 PDF 反解成 SVG 写出来，供人工核对版面。
// 默认跳过 —— 它产出的是给人看的文件，不是断言。
func TestWriteSVGPreviews(t *testing.T) {
	dir := strings.TrimSpace(os.Getenv("RECEIPT_SVG_DIR"))
	if dir == "" {
		t.Skip("未设置 RECEIPT_SVG_DIR")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	r := newTestRenderer(t, FontConfig{})
	t.Logf("字体：%s", r.FontStatus())

	simple := sampleDocument()
	simple.Items = simple.Items[:1]
	simple.Discount, simple.TaxAmount, simple.TaxRate = decimal.Zero, decimal.Zero, decimal.Zero
	simple.Status = StatusPaid
	simple.Refunds, simple.RefundedTotal = nil, decimal.Zero
	simple.Metadata, simple.Notes = nil, nil

	cases := []struct {
		name string
		tag  string
		doc  Document
	}{
		{"full_en", "en", sampleDocument()},
		{"full_zh", "zh-Hans", sampleDocument()},
		{"simple_en", "en", simple},
	}
	for _, tc := range cases {
		res, err := r.Render(tc.doc, Options{LocalePrefs: []string{tc.tag}, Timezone: time.UTC})
		if err != nil {
			t.Fatalf("%s：%v", tc.name, err)
		}
		for i, svg := range pdfToSVG(res.PDF) {
			path := filepath.Join(dir, tc.name+"_p"+strconv.Itoa(i+1)+".svg")
			if err := os.WriteFile(path, []byte(svg), 0o600); err != nil {
				t.Fatal(err)
			}
			t.Logf("%-12s 第 %d 页 → %s（%d 字节）", tc.name, i+1, path, len(svg))
		}
	}
}
