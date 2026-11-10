package aigateway

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func TestResolveUsageRange(t *testing.T) {
	shanghai, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Skip("缺时区数据")
	}
	// 上海 2026-11-10 08:30，即 UTC 00:30
	now := time.Date(2026, 11, 10, 0, 30, 0, 0, time.UTC)

	cases := []struct {
		in, name, bucket string
		from, to         time.Time
	}{
		{"today", UsageRangeToday, UsageBucketHour,
			time.Date(2026, 11, 10, 0, 0, 0, 0, shanghai), time.Date(2026, 11, 11, 0, 0, 0, 0, shanghai)},
		{"24H", UsageRange24h, UsageBucketHour,
			time.Date(2026, 11, 9, 9, 0, 0, 0, shanghai), time.Date(2026, 11, 10, 9, 0, 0, 0, shanghai)},
		{"7d", UsageRange7d, UsageBucketDay,
			time.Date(2026, 11, 4, 0, 0, 0, 0, shanghai), time.Date(2026, 11, 11, 0, 0, 0, 0, shanghai)},
		{"30d", UsageRange30d, UsageBucketDay,
			time.Date(2026, 10, 12, 0, 0, 0, 0, shanghai), time.Date(2026, 11, 11, 0, 0, 0, 0, shanghai)},
		{"90d", UsageRange90d, UsageBucketDay,
			time.Date(2026, 8, 13, 0, 0, 0, 0, shanghai), time.Date(2026, 11, 11, 0, 0, 0, 0, shanghai)},
		{"bogus", UsageRange7d, UsageBucketDay,
			time.Date(2026, 11, 4, 0, 0, 0, 0, shanghai), time.Date(2026, 11, 11, 0, 0, 0, 0, shanghai)},
	}
	for _, c := range cases {
		name, from, to, bucket := ResolveUsageRange(c.in, now, shanghai)
		if name != c.name || bucket != c.bucket || !from.Equal(c.from) || !to.Equal(c.to) {
			t.Errorf("%s → %s %s [%s, %s)，期望 %s %s [%s, %s)", c.in, name, bucket, from, to, c.name, c.bucket, c.from, c.to)
		}
	}
}

func TestFillUsageSeries(t *testing.T) {
	from := time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 0, 3)
	// 库里按时区截出来的桶可能在别的时区表示，按绝对时刻对齐。
	got := FillUsageSeries([]UsagePoint{
		{Time: from.AddDate(0, 0, 1).In(time.FixedZone("x", 8*3600)), Requests: 4, InputTokens: 100, Charged: decimal.NewFromInt(3)},
	}, from, to, UsageBucketDay)
	if len(got) != 3 {
		t.Fatalf("应补成 3 天，得到 %d", len(got))
	}
	if got[0].Requests != 0 || !got[0].Charged.IsZero() || got[1].Requests != 4 || got[1].InputTokens != 100 || got[2].Requests != 0 {
		t.Fatalf("补零或对齐不对：%+v", got)
	}
	if !got[1].Time.Equal(from.AddDate(0, 0, 1)) || got[1].Time.Location() != time.UTC {
		t.Fatalf("桶时间应统一成区间的时区：%v", got[1].Time)
	}

	hours := FillUsageSeries(nil, from, from.Add(24*time.Hour), UsageBucketHour)
	if len(hours) != 24 {
		t.Fatalf("按小时应有 24 个桶，得到 %d", len(hours))
	}
}

func TestFillUsageSeriesAcrossDST(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skip("缺时区数据")
	}
	// 2026-11-01 美东结束夏令时，这一天有 25 小时；按日分桶仍应是连续的本地零点。
	from := time.Date(2026, 10, 31, 0, 0, 0, 0, ny)
	got := FillUsageSeries(nil, from, from.AddDate(0, 0, 3), UsageBucketDay)
	if len(got) != 3 {
		t.Fatalf("应有 3 天，得到 %d", len(got))
	}
	for i, point := range got {
		if h := point.Time.In(ny).Hour(); h != 0 {
			t.Fatalf("第 %d 个桶应为本地零点，得到 %v", i, point.Time.In(ny))
		}
	}
}
