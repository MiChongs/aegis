package receipt

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func TestPreviewReceipt(t *testing.T) {
	if os.Getenv("RECEIPT_PREVIEW") == "" {
		t.Skip("set RECEIPT_PREVIEW=<locale>")
	}
	r := newTestRenderer(t, FontConfig{})
	locale := os.Getenv("RECEIPT_PREVIEW")
	doc := sampleDocument()
	if os.Getenv("RECEIPT_PREVIEW_LONG") != "" {
		doc.Items = nil
		for i := range 26 {
			doc.Items = append(doc.Items, LineItem{
				Name:        fmt.Sprintf("Service subscription line %d", i+1),
				Description: "A deliberately long description used to force the item table across several pages.",
				Quantity:    decimal.NewFromInt(int64(i%3 + 1)),
				UnitPrice:   decimal.RequireFromString("19.90"),
				Amount:      decimal.RequireFromString("19.90").Mul(decimal.NewFromInt(int64(i%3 + 1))),
			})
		}
	}
	res, err := r.Render(doc, Options{LocalePrefs: []string{locale}, Timezone: time.UTC})
	if err != nil {
		t.Fatal(err)
	}
	pages := extractPages(res.PDF)
	t.Logf("locale=%s pages=%d extractedPages=%d font=%s", res.Locale, res.Pages, len(pages), res.Font)
	for i, runs := range pages {
		t.Logf("\n===== PAGE %d (%d runs) =====\n%s", i+1, len(runs), asciiPage(runs, 118))
	}
	if len(pages) > 0 {
		t.Logf("\n--- plain text ---\n%s", strings.TrimSpace(pageText(pages[0])))
	}
}
