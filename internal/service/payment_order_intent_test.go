package service

import (
	"testing"

	paymentdomain "aegis/internal/domain/payment"
)

func TestNormalizeOrderIntent(t *testing.T) {
	// 元匣工具箱：type 写成用途词、planId 是字符串 —— 归一化成规范键，子类型清空
	meta, payType, err := normalizeOrderIntent("vip", OrderIntent{}, map[string]any{"planId": "19", "renew": true})
	if err != nil {
		t.Fatal(err)
	}
	if payType != "" || meta["purpose"] != paymentdomain.PurposeVipPurchase || meta["vipPlanId"] != int64(19) || meta["renew"] != true {
		t.Fatalf("归一化结果不对：type=%q meta=%v", payType, meta)
	}
	// 充值：只有 type
	meta, payType, err = normalizeOrderIntent("wallet_recharge", OrderIntent{}, nil)
	if err != nil || payType != "" || meta["purpose"] != paymentdomain.PurposeWalletRecharge {
		t.Fatalf("充值归一化不对：type=%q meta=%v err=%v", payType, meta, err)
	}
	// 真正的子类型原样保留；顶层 purpose + plan_id
	meta, payType, err = normalizeOrderIntent("alipay", OrderIntent{Purpose: "vip", PlanID: 5}, nil)
	if err != nil || payType != "alipay" || meta["purpose"] != paymentdomain.PurposeVipPurchase || meta["vipPlanId"] != int64(5) {
		t.Fatalf("顶层用途归一化不对：type=%q meta=%v err=%v", payType, meta, err)
	}
	// 只给档位 ID 推断为充值
	meta, _, _ = normalizeOrderIntent("", OrderIntent{}, map[string]any{"packageId": float64(3)})
	if meta["purpose"] != paymentdomain.PurposeWalletRecharge || meta["walletPackageId"] != int64(3) {
		t.Fatalf("档位推断不对：%v", meta)
	}
	// 普通商品单不动
	meta, payType, _ = normalizeOrderIntent("wxpay", OrderIntent{}, map[string]any{"sku": "A"})
	if payType != "wxpay" || meta["purpose"] != nil {
		t.Fatalf("普通商品单不该被改：type=%q meta=%v", payType, meta)
	}
	// 拼错的用途与互相矛盾的用途都拒绝
	if _, _, err := normalizeOrderIntent("", OrderIntent{}, map[string]any{"purpose": "vipp"}); errCode(err) != 40091 {
		t.Fatalf("拼错的用途应报 40091，得到 %v", err)
	}
	if _, _, err := normalizeOrderIntent("", OrderIntent{Purpose: "recharge"}, map[string]any{"purpose": "vip_purchase"}); errCode(err) != 40091 {
		t.Fatalf("矛盾的用途应报 40091，得到 %v", err)
	}
	// 不改调用方的 map
	orig := map[string]any{"planId": "1"}
	_, _, _ = normalizeOrderIntent("", OrderIntent{}, orig)
	if _, ok := orig["purpose"]; ok {
		t.Fatal("归一化改了调用方的 metadata")
	}
}

func TestAfdianOrderNoFromRemark(t *testing.T) {
	if got := afdianOrderNo(afdianOrder{CustomOrderID: " P1000420261115092220249772 "}); got != "P1000420261115092220249772" {
		t.Fatalf("custom_order_id 优先，得到 %q", got)
	}
	if got := afdianOrderNo(afdianOrder{Remark: "订单 P1000420261115092220249772 谢谢"}); got != "P1000420261115092220249772" {
		t.Fatalf("custom_order_id 丢了应从留言取，得到 %q", got)
	}
	if got := afdianOrderNo(afdianOrder{Remark: "加油 P123"}); got != "" {
		t.Fatalf("留言里不是订单号就不认，得到 %q", got)
	}
}

func TestAfdianSettleScope(t *testing.T) {
	p := newAfdianProvider(nil)
	a := p.SettleScope(map[string]any{"userId": "u1", "token": "t1"})
	b := p.SettleScope(map[string]any{"userId": "u1", "token": "t1", "scanPages": 5})
	c := p.SettleScope(map[string]any{"userId": "u2", "token": "t1"})
	if a == "" || a != b || a == c {
		t.Fatalf("同账号同范围、不同账号不同范围：%q %q %q", a, b, c)
	}
}
