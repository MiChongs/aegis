package aigateway

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func planSettings(t *testing.T, windows ...TimeWindow) Settings {
	t.Helper()
	settings := DefaultSettings()
	settings.TimePricing = TimePricing{Enabled: true, Windows: windows}
	normalized, err := NormalizeSettings(settings)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	return normalized
}

func shanghai(t *testing.T, value string) time.Time {
	t.Helper()
	loc, _ := time.LoadLocation("Asia/Shanghai")
	at, err := time.ParseInLocation("2006-01-02 15:04", value, loc)
	if err != nil {
		t.Fatal(err)
	}
	return at
}

func TestTimeWindowMatching(t *testing.T) {
	from, to := "2026-11-11", "2026-11-11"
	settings := planSettings(t,
		TimeWindow{ID: "promo", Name: "双十一", Tone: TonePromo, Start: "00:00", End: "00:00", Multiplier: dec("0.3"), From: &from, To: &to, Enabled: true},
		TimeWindow{ID: "night", Name: "夜间", Tone: ToneOffPeak, Start: "22:00", End: "02:00", Weekdays: []int{5}, Multiplier: dec("0.5"), Enabled: true},
		TimeWindow{ID: "peak", Name: "高峰", Tone: TonePeak, Start: "09:00", End: "18:00", Weekdays: []int{1, 2, 3, 4, 5}, Multiplier: dec("1.2"), Models: []string{"m1"}, Enabled: true},
		TimeWindow{ID: "off", Name: "停用", Start: "00:00", End: "00:00", Multiplier: dec("0.1"), Enabled: false},
	)
	cases := []struct {
		at, model, want string
	}{
		{"2026-11-27 23:00", "", "night"}, // 周五 23:00
		{"2026-11-28 01:30", "", "night"}, // 周六凌晨，按开始日（周五）判
		{"2026-11-28 02:00", "", ""},      // 半开区间
		{"2026-11-28 23:00", "", ""},      // 周六 23:00 不在周五的时段里
		{"2026-11-11 15:00", "", "promo"}, // 活动日全天，排在前面优先
		{"2026-11-26 10:00", "", ""},      // 全部模型口径不算限定模型的时段
		{"2026-11-26 10:00", "m1", "peak"},
		{"2026-11-26 10:00", "m2", ""},
	}
	for _, tc := range cases {
		match := settings.Match(tc.model, "chat", shanghai(t, tc.at))
		if tc.model == "" {
			match = settings.Match("", "", shanghai(t, tc.at))
		}
		got := ""
		if match != nil {
			got = match.ID
		}
		if got != tc.want {
			t.Errorf("%s %s: got %q want %q", tc.at, tc.model, got, tc.want)
		}
	}
}

func TestNextChangeAndTimeline(t *testing.T) {
	settings := planSettings(t,
		TimeWindow{ID: "night", Name: "夜间半价", Tone: ToneOffPeak, Start: "00:30", End: "08:30", Multiplier: dec("0.5"), Enabled: true},
	)
	now := shanghai(t, "2026-11-26 23:10")
	next := settings.NextChange("", "", now)
	if next == nil || !next.Equal(shanghai(t, "2026-11-27 00:30")) {
		t.Fatalf("next change = %v", next)
	}
	inside := shanghai(t, "2026-11-27 03:00")
	match := settings.Match("", "", inside)
	if match == nil || match.EndsAt == nil || !match.EndsAt.Equal(shanghai(t, "2026-11-27 08:30")) {
		t.Fatalf("match = %+v", match)
	}
	timeline := settings.Timeline("", "", now, 2)
	// 每天：00:00–00:30 标准、00:30–08:30 夜间、08:30–24:00 标准；跨天的标准段合并
	if len(timeline) != 5 {
		t.Fatalf("timeline len = %d: %+v", len(timeline), timeline)
	}
	if timeline[1].WindowID != "night" || !timeline[1].Start.Equal(shanghai(t, "2026-11-26 00:30")) {
		t.Fatalf("segment 1 = %+v", timeline[1])
	}
	if timeline[2].WindowID != "" || !timeline[2].End.Equal(shanghai(t, "2026-11-27 00:30")) {
		t.Fatalf("merged standard segment = %+v", timeline[2])
	}
	if disabled := DefaultSettings(); disabled.NextChange("", "", now) != nil || len(disabled.Timeline("", "", now, 7)) != 1 {
		t.Fatal("disabled time pricing should be one flat segment")
	}
}

func TestPricingStackingAndMembers(t *testing.T) {
	settings := planSettings(t,
		TimeWindow{ID: "night", Name: "夜间", Start: "00:00", End: "00:00", Multiplier: dec("0.5"), Enabled: true},
	)
	settings.VipMultiplier = dec("0.9")
	settings.MemberTiers = []MemberTier{
		{TierID: 2, Multiplier: dec("0.7"), PackDiscount: dec("0.8"), Daily: dec("500")},
		{TierID: 5, Label: "SVIP", Multiplier: dec("0.8"), PackDiscount: dec("0.85"), PackBonusRate: dec("0.1"), RPM: 120},
	}
	now := shanghai(t, "2026-11-26 12:00")
	profile := Profile{IsVip: true, Level: 1, TierIDs: []int64{5, 2}}.WithTierNames(map[int64]string{2: "领航者", 5: "SVIP"})
	member := settings.Member(profile)
	if member.TierID != 2 || member.Name != "领航者" || !member.Multiplier.Equal(dec("0.7")) {
		t.Fatalf("member = %+v (want first configured tier)", member)
	}
	if got := settings.Pricing(member, 1, "", "", now).Total; !got.Equal(dec("0.35")) {
		t.Fatalf("multiply total = %s", got)
	}
	settings.TimePricing.Stacking = StackBest
	if got := settings.Pricing(member, 1, "", "", now).Total; !got.Equal(dec("0.5")) {
		t.Fatalf("best total = %s", got)
	}
	generic := settings.Member(Profile{IsVip: true, TierIDs: []int64{99}})
	if generic.Source != MemberSourceVip || !generic.Multiplier.Equal(dec("0.9")) {
		t.Fatalf("generic = %+v", generic)
	}
	if none := settings.Member(Profile{IsVip: false, TierIDs: []int64{2}}); none.Source != MemberSourceNone {
		t.Fatalf("non member = %+v", none)
	}
	if settings.QuotaMicroFor(SourceVipDaily, member) != 500*MicroPerPoint || settings.EffectiveRPM(member) != settings.RPM {
		t.Fatal("tier quota / rpm")
	}
	svip := settings.TierMember(settings.MemberTiers[1], "")
	if settings.EffectiveRPM(svip) != 120 {
		t.Fatal("tier rpm override")
	}
}

func TestSettingsRejectIntegralAndBadWindows(t *testing.T) {
	settings := DefaultSettings()
	settings.Sources = []string{SourceCredit, SourceIntegral}
	if _, err := NormalizeSettings(settings); err == nil {
		t.Fatal("integral source should be rejected")
	}
	for name, window := range map[string]TimeWindow{
		"bad clock":    {Name: "x", Start: "25:00", End: "01:00", Multiplier: dec("1")},
		"bad weekday":  {Name: "x", Start: "01:00", End: "02:00", Weekdays: []int{8}, Multiplier: dec("1")},
		"zero mult":    {Name: "x", Start: "01:00", End: "02:00", Multiplier: decimal.Zero},
		"bad tone":     {Name: "x", Start: "01:00", End: "02:00", Multiplier: dec("1"), Tone: "neon"},
		"bad kind":     {Name: "x", Start: "01:00", End: "02:00", Multiplier: dec("1"), Kinds: []string{"video"}},
		"empty name":   {Start: "01:00", End: "02:00", Multiplier: dec("1")},
		"inverse date": {Name: "x", Start: "01:00", End: "02:00", Multiplier: dec("1"), From: strPtr("2026-12-02"), To: strPtr("2026-12-01")},
	} {
		s := DefaultSettings()
		s.TimePricing.Windows = []TimeWindow{window}
		if _, err := NormalizeSettings(s); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
	s := DefaultSettings()
	s.TimePricing.Windows = []TimeWindow{{Name: "a", Start: "1:00", End: "02:00", Multiplier: dec("1")}}
	if _, err := NormalizeSettings(s); err == nil {
		t.Error("H:MM should be rejected")
	}
	s.TimePricing.Windows = []TimeWindow{{Name: "a", Start: "01:00", End: "02:00", Multiplier: dec("1"), Weekdays: []int{1, 2, 3, 4, 5, 6, 7}}}
	normalized, err := NormalizeSettings(s)
	if err != nil || normalized.TimePricing.Windows[0].ID != "w1" || len(normalized.TimePricing.Windows[0].Weekdays) != 0 {
		t.Fatalf("auto id / all weekdays: %+v %v", normalized.TimePricing.Windows, err)
	}
}

func strPtr(v string) *string { return &v }

func TestQuotePackage(t *testing.T) {
	pkg := Package{Name: "标准包", Points: dec("10000"), BonusPoints: dec("1000"), Price: dec("10.00"), MemberDiscount: true}
	member := MemberBenefit{Source: MemberSourceTier, Name: "SVIP", Multiplier: dec("0.8"), PackDiscount: dec("0.85"), PackBonusRate: dec("0.1")}
	quote := QuotePackage(pkg, member)
	if !quote.FinalPrice.Equal(dec("8.5")) || !quote.MemberSaving.Equal(dec("1.5")) || !quote.MemberBonusPoints.Equal(dec("1000")) ||
		!quote.TotalPoints.Equal(dec("12000")) || quote.DiscountLabel != "SVIP 85 折·多送 10%" || !quote.UnitPrice.Equal(dec("7.08")) {
		t.Fatalf("quote = %+v", quote)
	}
	plain := QuotePackage(pkg, NoMember())
	if !plain.FinalPrice.Equal(dec("10")) || plain.DiscountLabel != "" || !plain.TotalPoints.Equal(dec("11000")) {
		t.Fatalf("plain = %+v", plain)
	}
	pkg.MemberDiscount = false
	if QuotePackage(pkg, member).DiscountLabel != "" {
		t.Fatal("member discount off")
	}
	tiny := QuotePackage(Package{Points: dec("1"), Price: dec("0.01"), MemberDiscount: true}, member)
	if !tiny.FinalPrice.Equal(dec("0.01")) {
		t.Fatalf("tiny price floor = %s", tiny.FinalPrice)
	}
	if ZheLabel(dec("0.8")) != "8 折" || ZheLabel(dec("0.85")) != "85 折" || ZheLabel(dec("1")) != "" || ZheLabel(dec("0.875")) != "8.75 折" {
		t.Fatal("zhe labels")
	}
}

func TestPackageAvailabilityAndRefund(t *testing.T) {
	now := shanghai(t, "2026-11-26 12:00")
	later := now.Add(time.Hour)
	base := Package{Enabled: true, Audience: AudienceAll}
	cases := []struct {
		name      string
		pkg       Package
		profile   Profile
		store     bool
		bought    int
		monthly   int
		wantOK    bool
		wantCause string
	}{
		{"open", base, Profile{}, true, 0, 0, true, ""},
		{"store closed", base, Profile{}, false, 0, 0, false, ReasonStoreClosed},
		{"vip only", Package{Enabled: true, Audience: AudienceVip}, Profile{}, true, 0, 0, false, ReasonMembersOnly},
		{"tier", Package{Enabled: true, Audience: AudienceVip, TierIDs: []int64{5}}, Profile{IsVip: true, TierIDs: []int64{1}}, true, 0, 0, false, ReasonTierRequired},
		{"not started", Package{Enabled: true, StartsAt: &later}, Profile{}, true, 0, 0, false, ReasonNotStarted},
		{"sold out", Package{Enabled: true, Stock: 3, Sold: 3}, Profile{}, true, 0, 0, false, ReasonSoldOut},
		{"limit", Package{Enabled: true, PerUserLimit: 2}, Profile{}, true, 2, 0, false, ReasonLimitReached},
		{"monthly", Package{Enabled: true, MonthlyLimit: 1}, Profile{}, true, 5, 1, false, ReasonMonthlyLimitReached},
	}
	for _, tc := range cases {
		ok, reason := PackageAvailability(tc.pkg, tc.profile, tc.store, tc.bought, tc.monthly, now)
		if ok != tc.wantOK || reason != tc.wantCause {
			t.Errorf("%s: got %v %q", tc.name, ok, reason)
		}
	}
	if got := RefundByRemaining(dec("8.50"), 6000, 12000); !got.Equal(dec("4.25")) {
		t.Fatalf("half refund = %s", got)
	}
	if got := RefundByRemaining(dec("8.50"), 1, 12000); !got.Equal(decimal.Zero) {
		t.Fatalf("floor refund = %s", got)
	}
	if got := RefundByRemaining(dec("8.50"), 12000, 12000); !got.Equal(dec("8.5")) {
		t.Fatalf("full refund = %s", got)
	}
}

func TestNormalizePackage(t *testing.T) {
	valid := PackageInput{Name: "标准包", Points: dec("10000"), Price: dec("10"), OriginalPrice: dec("12"), ValidityDays: 30}
	pkg, err := NormalizePackage(valid)
	if err != nil || pkg.Tone != "blue" || !pkg.MemberDiscount || !pkg.Enabled || pkg.Audience != AudienceAll {
		t.Fatalf("pkg = %+v err %v", pkg, err)
	}
	for name, mutate := range map[string]func(*PackageInput){
		"no name":         func(p *PackageInput) { p.Name = "" },
		"fraction points": func(p *PackageInput) { p.Points = dec("1.5") },
		"price cents":     func(p *PackageInput) { p.Price = dec("1.234") },
		"original low":    func(p *PackageInput) { p.OriginalPrice = dec("9") },
		"bad tone":        func(p *PackageInput) { p.Tone = "pink" },
		"monthly > total": func(p *PackageInput) { p.PerUserLimit, p.MonthlyLimit = 1, 2 },
	} {
		in := valid
		mutate(&in)
		if _, err := NormalizePackage(in); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
	tiered := valid
	tiered.TierIDs = []int64{5, 5}
	pkg, _ = NormalizePackage(tiered)
	if pkg.Audience != AudienceVip || len(pkg.TierIDs) != 1 {
		t.Fatalf("tier restricted = %+v", pkg)
	}
}
