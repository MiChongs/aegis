package payment

import (
	"encoding/json"
	"strconv"
	"strings"
)

// ── 订单用途（purpose）的写法与推断 ──
//
// 自动履约只认 metadata.purpose 与它的快照参数。别的应用接入时常照直觉传参
// （type: "vip"、metadata: {"planId": "19"}），这里收录这些写法，服务层下单时据此归一化，
// 读订单时据此判断「这笔钱是不是该发东西」。见 docs/payment-fulfillment.md。

// purposeAliases 用途的各种写法 → 规范值
var purposeAliases = map[string]string{
	PurposeVipPurchase: PurposeVipPurchase,
	"vip":              PurposeVipPurchase,
	"vip_plan":         PurposeVipPurchase,
	"membership":       PurposeVipPurchase,
	"member":           PurposeVipPurchase,
	"subscription":     PurposeVipPurchase,

	PurposeWalletRecharge: PurposeWalletRecharge,
	"recharge":            PurposeWalletRecharge,
	"wallet":              PurposeWalletRecharge,
	"balance_recharge":    PurposeWalletRecharge,
	"topup":               PurposeWalletRecharge,
	"top_up":              PurposeWalletRecharge,
	"deposit":             PurposeWalletRecharge,

	PurposeIntegralPurchase: PurposeIntegralPurchase,
	"integral":              PurposeIntegralPurchase,
	"points":                PurposeIntegralPurchase,
	"points_purchase":       PurposeIntegralPurchase,
}

// VipPlanIDKeys / WalletPackageIDKeys metadata 里套餐与档位 ID 的各种写法（第一个是规范键）
var (
	VipPlanIDKeys       = []string{MetaKeyVipPlanID, "planId", "plan_id", "vip_plan_id", "planID"}
	WalletPackageIDKeys = []string{MetaKeyWalletPackageID, "packageId", "package_id", "rechargePackageId", "recharge_package_id"}
)

// CanonicalPurpose 用途词的规范值；不是用途词返回空串。大小写、连字符都不讲究。
func CanonicalPurpose(value string) string {
	v := strings.ToLower(strings.TrimSpace(value))
	v = strings.ReplaceAll(v, "-", "_")
	return purposeAliases[v]
}

// LegacyIntentMetaKeys 出现了就说明「这单要发东西」的 metadata 键（仓储层筛「待履约」用）
func LegacyIntentMetaKeys() []string {
	keys := []string{MetaKeyPurpose}
	keys = append(keys, VipPlanIDKeys...)
	return append(keys, WalletPackageIDKeys...)
}

// LegacyIntentTypes 被老客户端写进 provider_type 的用途词（全部小写）
func LegacyIntentTypes() []string {
	out := make([]string, 0, len(purposeAliases))
	for alias := range purposeAliases {
		out = append(out, alias)
	}
	return out
}

// IntentInt64 从 metadata 里按顺序取第一个正整数（数字、数字字符串都认 —— JSON 往返后数字是 float64）
func IntentInt64(m map[string]any, keys ...string) int64 {
	for _, key := range keys {
		var n int64
		switch v := m[key].(type) {
		case float64:
			n = int64(v)
		case int64:
			n = v
		case int:
			n = int64(v)
		case json.Number:
			n, _ = v.Int64()
		case string:
			n, _ = strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		}
		if n > 0 {
			return n
		}
	}
	return 0
}

func intentString(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

// InferIntent 宽松地推断一笔订单想买什么：metadata.purpose → provider_type 上的用途词 → 有套餐 ID 即会员 → 有档位 ID 即充值。
// 推断不出返回空串（普通商品单）。写错了的 metadata.purpose 视为没写，交给后面几条去猜。
func InferIntent(providerType string, metadata map[string]any) (purpose string, planID int64, packageID int64) {
	purpose = CanonicalPurpose(intentString(metadata, MetaKeyPurpose))
	if purpose == "" {
		purpose = CanonicalPurpose(providerType)
	}
	planID = IntentInt64(metadata, VipPlanIDKeys...)
	packageID = IntentInt64(metadata, WalletPackageIDKeys...)
	if purpose == "" && planID > 0 {
		purpose = PurposeVipPurchase
	}
	if purpose == "" && packageID > 0 {
		purpose = PurposeWalletRecharge
	}
	return purpose, planID, packageID
}

// DeriveFulfillment 填上 Purpose 与 FulfillmentState（读订单时调用，不改库）。
func (o *Order) DeriveFulfillment() {
	if o == nil {
		return
	}
	explicit := CanonicalPurpose(intentString(o.Metadata, MetaKeyPurpose))
	purpose := explicit
	if purpose == "" {
		purpose, _, _ = InferIntent(o.ProviderType, o.Metadata)
	}
	o.Purpose = purpose
	switch {
	case o.FulfillmentStatus == FulfillmentDone:
		o.FulfillmentState = FulfillmentStateDone
	case o.FulfillmentStatus == FulfillmentManual:
		o.FulfillmentState = FulfillmentStateManual
	case o.Status != "paid" || purpose == "":
		o.FulfillmentState = FulfillmentStateNotRequired
	case explicit != "":
		o.FulfillmentState = FulfillmentStatePending
	default:
		o.FulfillmentState = FulfillmentStateUnrecognized
	}
}
