package postgres

import (
	"testing"
	"time"

	paymentdomain "aegis/internal/domain/payment"
)

// 粒度由跨度自动决定。让调用方指定的结果是「拉了两年、按天分桶、七百个点」。
func TestTrendBucketUnitFollowsSpan(t *testing.T) {
	base := time.Date(2026, 3, 21, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		days int
		want string
	}{
		{"一天", 1, paymentdomain.TrendBucketDay},
		{"近 30 天", 30, paymentdomain.TrendBucketDay},
		{"两个月边界内", 62, paymentdomain.TrendBucketDay},
		{"超过两个月转周", 63, paymentdomain.TrendBucketWeek},
		{"两年边界内仍是周", 730, paymentdomain.TrendBucketWeek},
		{"超过两年转月", 731, paymentdomain.TrendBucketMonth},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := trendBucketUnit(base, base.AddDate(0, 0, tc.days))
			if got != tc.want {
				t.Fatalf("跨 %d 天得到 %q，期望 %q", tc.days, got, tc.want)
			}
		})
	}
}

// 补空桶用的对齐必须与 Postgres 的 date_trunc 一致（周起于**周一**）。
// 对不上的话补出来的桶落不到真实数据那一格，同一天会出现两个点。
func TestTruncateToBucketMatchesPostgres(t *testing.T) {
	// 2026-03-21 是周六，所属周应对齐到 2026-03-16（周一）
	saturday := time.Date(2026, 3, 21, 15, 30, 45, 0, time.UTC)
	cases := map[string]struct {
		unit string
		want time.Time
	}{
		"日": {paymentdomain.TrendBucketDay, time.Date(2026, 3, 21, 0, 0, 0, 0, time.UTC)},
		"周": {paymentdomain.TrendBucketWeek, time.Date(2026, 3, 16, 0, 0, 0, 0, time.UTC)},
		"月": {paymentdomain.TrendBucketMonth, time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)},
	}
	for name, tc := range cases {
		if got := truncateToBucket(tc.unit, saturday); !got.Equal(tc.want) {
			t.Errorf("%s：得到 %s，期望 %s", name, got, tc.want)
		}
	}

	// 周一自身对齐后不该被推到上一周
	monday := time.Date(2026, 3, 16, 8, 0, 0, 0, time.UTC)
	if got := truncateToBucket(paymentdomain.TrendBucketWeek, monday); !got.Equal(time.Date(2026, 3, 16, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("周一被推到了 %s", got)
	}
	// 周日属于**上一个**周一开头的那一周（ISO 周），不是下一周
	sunday := time.Date(2026, 3, 22, 8, 0, 0, 0, time.UTC)
	if got := truncateToBucket(paymentdomain.TrendBucketWeek, sunday); !got.Equal(time.Date(2026, 3, 16, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("周日归到了 %s，期望 2026-03-16", got)
	}
}

// 步进必须跨得过月末与闰年，否则补桶循环会卡死或错位。
func TestStepBucketCrossesBoundaries(t *testing.T) {
	if got := stepBucket(paymentdomain.TrendBucketDay, time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC)); got.Month() != time.February || got.Day() != 1 {
		t.Errorf("跨月失败：%s", got)
	}
	if got := stepBucket(paymentdomain.TrendBucketMonth, time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)); got.Year() != 2027 || got.Month() != time.January {
		t.Errorf("跨年失败：%s", got)
	}
	if got := stepBucket(paymentdomain.TrendBucketWeek, time.Date(2028, 2, 28, 0, 0, 0, 0, time.UTC)); !got.Equal(time.Date(2028, 3, 6, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("闰年跨周失败：%s", got)
	}
}

// 标签的精度跟着粒度走：按月分桶还显示 03-21 会让人以为是某一天的数。
func TestTrendLabelPrecisionFollowsBucket(t *testing.T) {
	at := time.Date(2026, 3, 21, 0, 0, 0, 0, time.UTC)
	if got := trendLabel(paymentdomain.TrendBucketDay, at); got != "03-21" {
		t.Errorf("日标签 = %q", got)
	}
	if got := trendLabel(paymentdomain.TrendBucketMonth, at); got != "2026-03" {
		t.Errorf("月标签 = %q", got)
	}
}
