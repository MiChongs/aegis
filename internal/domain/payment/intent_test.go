package payment

import "testing"

func TestCanonicalPurpose(t *testing.T) {
	cases := map[string]string{
		"vip_purchase": PurposeVipPurchase, "VIP": PurposeVipPurchase, "membership": PurposeVipPurchase,
		"wallet_recharge": PurposeWalletRecharge, "wallet-recharge": PurposeWalletRecharge, "recharge": PurposeWalletRecharge,
		"integral": PurposeIntegralPurchase, "alipay": "", "wxpay": "", "": "",
	}
	for in, want := range cases {
		if got := CanonicalPurpose(in); got != want {
			t.Errorf("CanonicalPurpose(%q) = %q，期望 %q", in, got, want)
		}
	}
}

func TestInferIntent(t *testing.T) {
	// 元匣工具箱线上的写法：type=vip、metadata 里 planId 是字符串
	if p, plan, _ := InferIntent("vip", map[string]any{"planId": "19", "renew": true}); p != PurposeVipPurchase || plan != 19 {
		t.Errorf("type=vip + planId 应推断为会员 19，得到 %q %d", p, plan)
	}
	if p, _, _ := InferIntent("wallet_recharge", nil); p != PurposeWalletRecharge {
		t.Errorf("type=wallet_recharge 应推断为充值，得到 %q", p)
	}
	// 只有套餐 ID（JSON 往返后是 float64）
	if p, plan, _ := InferIntent("", map[string]any{"plan_id": float64(7)}); p != PurposeVipPurchase || plan != 7 {
		t.Errorf("只有 plan_id 应推断为会员，得到 %q %d", p, plan)
	}
	if p, _, pkg := InferIntent("alipay", map[string]any{"packageId": "3"}); p != PurposeWalletRecharge || pkg != 3 {
		t.Errorf("只有 packageId 应推断为充值，得到 %q %d", p, pkg)
	}
	if p, _, _ := InferIntent("alipay", map[string]any{"note": "x"}); p != "" {
		t.Errorf("普通商品单不该推断出用途，得到 %q", p)
	}
}

func TestDeriveFulfillment(t *testing.T) {
	cases := []struct {
		name  string
		order Order
		state string
	}{
		{"规范用途未履约", Order{Status: "paid", FulfillmentStatus: FulfillmentNone, Metadata: map[string]any{"purpose": "vip_purchase"}}, FulfillmentStatePending},
		{"老客户端未履约", Order{Status: "paid", FulfillmentStatus: FulfillmentNone, ProviderType: "vip", Metadata: map[string]any{"planId": "19"}}, FulfillmentStateUnrecognized},
		{"已履约", Order{Status: "paid", FulfillmentStatus: FulfillmentDone, Metadata: map[string]any{"purpose": "vip_purchase"}}, FulfillmentStateDone},
		{"人工补发", Order{Status: "paid", FulfillmentStatus: FulfillmentManual, ProviderType: "vip"}, FulfillmentStateManual},
		{"未付款", Order{Status: "pending", FulfillmentStatus: FulfillmentNone, Metadata: map[string]any{"purpose": "vip_purchase"}}, FulfillmentStateNotRequired},
		{"普通商品", Order{Status: "paid", FulfillmentStatus: FulfillmentNone, ProviderType: "alipay"}, FulfillmentStateNotRequired},
	}
	for _, c := range cases {
		o := c.order
		o.DeriveFulfillment()
		if o.FulfillmentState != c.state {
			t.Errorf("%s：状态 %q，期望 %q", c.name, o.FulfillmentState, c.state)
		}
	}
}
