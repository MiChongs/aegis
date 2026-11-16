package wallet

import (
	"errors"
	"testing"

	"github.com/shopspring/decimal"
)

func d(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func TestAllocateInflowDefaults(t *testing.T) {
	cases := []struct {
		typ, bucket string
		cash, bonus string
	}{
		{TxnTypeRecharge, "", "10", "0"},
		{TxnTypeRefund, "", "10", "0"},
		{TxnTypePointsExchange, "", "0", "10"},
		{TxnTypeCardKey, "", "0", "10"},
		{TxnTypeRechargeBonus, "", "0", "10"},
		{TxnTypeAIRefund, "", "0", "10"},
		{TxnTypeAdminAdjust, "", "0", "10"},
		{TxnTypeAdminAdjust, BucketCash, "10", "0"},
		{TxnTypeRecharge, BucketBonus, "0", "10"},
	}
	for _, c := range cases {
		got, err := Allocate(d("5"), d("1"), Change{Type: c.typ, Amount: d("10"), Bucket: c.bucket}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !got.Cash.Equal(d(c.cash)) || !got.Bonus.Equal(d(c.bonus)) {
			t.Errorf("%s/%s: got cash %s bonus %s", c.typ, c.bucket, got.Cash, got.Bonus)
		}
	}
}

func TestAllocateRestore(t *testing.T) {
	// 原出账扣了 3 赠送余额，退回 5：先补 3 赠送，余 2 进本金
	r := d("3")
	got, err := Allocate(d("0"), d("0"), Change{Type: TxnTypeRefund, Amount: d("5")}, &r)
	if err != nil || !got.Bonus.Equal(d("3")) || !got.Cash.Equal(d("2")) {
		t.Fatalf("got %+v %v", got, err)
	}
	// 退回额小于可补回额：全进赠送
	got, _ = Allocate(d("0"), d("0"), Change{Type: TxnTypeRefund, Amount: d("2")}, &r)
	if !got.Bonus.Equal(d("2")) || !got.Cash.IsZero() {
		t.Fatalf("got %+v", got)
	}
}

func TestAllocateOutflow(t *testing.T) {
	// 余额 10，其中赠送 4、本金 6
	cases := []struct {
		name       string
		typ, spend string
		amount     string
		cash       string
		bonus      string
		err        error
	}{
		{"先扣赠送", TxnTypeConsume, "", "-5", "-1", "-4", nil},
		{"赠送够用", TxnTypeConsume, "", "-3", "0", "-3", nil},
		{"总额不足", TxnTypeConsume, "", "-11", "", "", ErrInsufficientFunds},
		{"先扣本金", TxnTypeAdminAdjust, SpendCashFirst, "-8", "-6", "-2", nil},
		{"冲正默认先扣本金", TxnTypeRefund, "", "-7", "-6", "-1", nil},
		{"只扣本金", TxnTypeAdminAdjust, SpendCashOnly, "-6", "-6", "0", nil},
		{"只扣本金不足", TxnTypeAdminAdjust, SpendCashOnly, "-6.01", "", "", ErrInsufficientCash},
		{"只扣赠送不足", TxnTypeAdminAdjust, SpendBonusOnly, "-5", "", "", ErrInsufficientBonus},
		{"兑换无视传入策略", TxnTypeIntegralExchange, SpendBonusFirst, "-7", "", "", ErrInsufficientCash},
		{"兑换只扣本金", TxnTypeIntegralExchange, "", "-6", "-6", "0", nil},
	}
	for _, c := range cases {
		got, err := Allocate(d("10"), d("4"), Change{Type: c.typ, Spend: c.spend, Amount: d(c.amount)}, nil)
		if c.err != nil {
			if !errors.Is(err, c.err) {
				t.Errorf("%s: want %v got %v", c.name, c.err, err)
			}
			continue
		}
		if err != nil || !got.Cash.Equal(d(c.cash)) || !got.Bonus.Equal(d(c.bonus)) {
			t.Errorf("%s: got %+v %v", c.name, got, err)
		}
		if !got.Cash.Add(got.Bonus).Equal(d(c.amount)) {
			t.Errorf("%s: split does not sum", c.name)
		}
	}
}

func TestQuoteExchange(t *testing.T) {
	cases := []struct {
		amount string
		rate   int64
		points int64
		cost   string
	}{
		{"10", 100, 1000, "10"},
		{"0.01", 100, 1, "0.01"},
		{"1", 7, 7, "1"},
		{"0.5", 7, 3, "0.43"},
		{"0.1", 7, 0, "0"},
		{"3.33", 3, 9, "3"},
	}
	for _, c := range cases {
		points, cost, err := QuoteExchange(d(c.amount), c.rate)
		if err != nil || points != c.points || !cost.Equal(d(c.cost)) {
			t.Errorf("%s×%d: got %d %s %v", c.amount, c.rate, points, cost, err)
		}
		if cost.GreaterThan(d(c.amount)) {
			t.Errorf("cost exceeds amount")
		}
	}
	if _, _, err := QuoteExchange(d("1"), 0); err == nil {
		t.Fatal("rate 0 should fail")
	}
}

func TestExchangeQuotaAndCheck(t *testing.T) {
	s := DefaultSettings()
	s.ExchangeMax = d("50")
	s.ExchangeDailyMax = d("80")
	q := BuildExchangeQuota(s, 100, d("200"), d("40"))
	if !q.RemainingToday.Equal(d("40")) || !q.MaxNow.Equal(d("40")) {
		t.Fatalf("quota %+v", q)
	}
	if !errors.Is(CheckExchange(s, d("0.5"), d("0")), ErrExchangeTooSmall) {
		t.Fatal("min")
	}
	if !errors.Is(CheckExchange(s, d("51"), d("0")), ErrExchangeTooLarge) {
		t.Fatal("max")
	}
	if !errors.Is(CheckExchange(s, d("41"), d("40")), ErrExchangeDailyLimit) {
		t.Fatal("daily")
	}
	if CheckExchange(s, d("40"), d("40")) != nil {
		t.Fatal("exact daily should pass")
	}
}

func TestSettingsValidate(t *testing.T) {
	s := DefaultSettings()
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	s.RechargeMin = d("10")
	s.RechargeMax = d("5")
	if s.Validate() == nil {
		t.Fatal("min > max should fail")
	}
	s = DefaultSettings()
	s.ExchangeMin = d("0.001")
	if s.Validate() == nil {
		t.Fatal("3 decimals should fail")
	}
}
