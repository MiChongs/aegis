package service

import (
	"context"
	"net/http"
	"strings"

	"github.com/shopspring/decimal"

	paymentdomain "aegis/internal/domain/payment"
	apperrors "aegis/pkg/errors"
)

// ── 订单用途归一化 ──
//
// 自动履约只认 metadata 里的 purpose 与它的快照参数（vip_purchase + vipPlanId 等）。
// 这套约定此前只写在 Voyage 客户端与 Kotlin SDK 里，别的应用接入时照着直觉传参：
//
//	type: "vip"、metadata: {"planId": "19"}            （元匣工具箱，2026-11 线上）
//	type: "wallet_recharge"、不带 metadata             （同上）
//
// 网关此前一律放行：type 被当成子类型原样记进 provider_type，planId 没人认，
// 订单成了「普通商品单」—— 付款、回查、结单都正常，唯独不发货，也不报任何错。
// 应用管理员只能手工补会员、另配一条物品发放规则按金额补余额。
//
// 这里把各种写法收成同一份意图，下单时写回规范键，履约链路不必再关心客户端怎么传：
//
//	用途：metadata.purpose → 请求顶层 purpose → 请求的 type（是用途词时）→ 带套餐 ID 推断为会员 → 带档位 ID 推断为充值
//	套餐 ID：vipPlanId / planId / plan_id / vip_plan_id，或请求顶层的 plan_id
//	充值档位：walletPackageId / packageId / package_id / rechargePackageId，或请求顶层的 package_id
//
// 用途词占了 type 时同时清空子类型：它从来不是付款方式，不能再交给上游或印到凭证上。
// 词表在 domain/payment/intent.go，读订单判断「该不该发东西」用的是同一份。

// OrderIntent 下单请求里与履约有关、metadata 之外的部分
type OrderIntent struct {
	// Purpose 请求顶层的用途（可选，与 metadata.purpose 等价）
	Purpose string
	// PlanID 请求顶层的会员套餐 ID（可选）
	PlanID int64
	// PackageID 请求顶层的充值档位 ID（可选）
	PackageID int64
}

// normalizeOrderIntent 把请求里的用途写法收成规范的 metadata。
//
// 返回新的 metadata（不改调用方的 map）与实际要交给渠道的子类型。
// 显式写了却不认识的 purpose 报 40091：拼错用途比当成普通商品单把钱收了好得多。
func normalizeOrderIntent(payType string, intent OrderIntent, metadata map[string]any) (map[string]any, string, error) {
	out := make(map[string]any, len(metadata)+3)
	for k, v := range metadata {
		out[k] = v
	}

	purpose := ""
	if raw := metaString(metadata, paymentdomain.MetaKeyPurpose); raw != "" {
		if purpose = paymentdomain.CanonicalPurpose(raw); purpose == "" {
			return nil, "", apperrors.New(40091, http.StatusBadRequest, "不支持的订单用途: "+raw)
		}
	}
	if raw := strings.TrimSpace(intent.Purpose); raw != "" {
		explicit := paymentdomain.CanonicalPurpose(raw)
		if explicit == "" {
			return nil, "", apperrors.New(40091, http.StatusBadRequest, "不支持的订单用途: "+raw)
		}
		if purpose != "" && purpose != explicit {
			return nil, "", apperrors.New(40091, http.StatusBadRequest, "purpose 与 metadata.purpose 不一致")
		}
		purpose = explicit
	}
	// type 写成了用途词：它不是付款方式，挪进用途后清掉
	if fromType := paymentdomain.CanonicalPurpose(payType); fromType != "" {
		if purpose == "" {
			purpose = fromType
		}
		payType = ""
	}

	planID := intent.PlanID
	if planID <= 0 {
		planID = paymentdomain.IntentInt64(metadata, paymentdomain.VipPlanIDKeys...)
	}
	packageID := intent.PackageID
	if packageID <= 0 {
		packageID = paymentdomain.IntentInt64(metadata, paymentdomain.WalletPackageIDKeys...)
	}
	// 只给了套餐 ID：除了买会员没有别的意思；只给了档位 ID 同理
	if purpose == "" && planID > 0 {
		purpose = paymentdomain.PurposeVipPurchase
	}
	if purpose == "" && packageID > 0 {
		purpose = paymentdomain.PurposeWalletRecharge
	}

	if purpose == "" {
		return out, payType, nil
	}
	out[paymentdomain.MetaKeyPurpose] = purpose
	switch purpose {
	case paymentdomain.PurposeVipPurchase:
		if planID > 0 {
			out[paymentdomain.MetaKeyVipPlanID] = planID
		}
	case paymentdomain.PurposeWalletRecharge:
		if packageID > 0 {
			out[paymentdomain.MetaKeyWalletPackageID] = packageID
		}
	}
	return out, payType, nil
}

// resolveOrderPricing 金额或标题没填时按用途补上：会员取套餐价与套餐名，按档位充值取档位价。
//
// 只补空的，填了的原样返回 —— 填了却对不上价，由 prepareFulfillmentMetadata 照旧拒掉（40089 等）。
// 套餐 / 档位查不到也不在这里报错，交给后面的校验给出准确的错误码。
func (s *PaymentService) resolveOrderPricing(ctx context.Context, appID int64, metadata map[string]any, amount, subject string) (string, string, error) {
	amount = strings.TrimSpace(amount)
	subject = strings.TrimSpace(subject)
	if amount != "" && subject != "" {
		return amount, subject, nil
	}
	switch metaString(metadata, paymentdomain.MetaKeyPurpose) {
	case paymentdomain.PurposeVipPurchase:
		planID := metaInt64(metadata, paymentdomain.MetaKeyVipPlanID)
		if planID <= 0 || s.pg == nil {
			break
		}
		plan, err := s.pg.GetVipPlan(ctx, appID, planID)
		if err != nil {
			return "", "", err
		}
		if plan == nil {
			break
		}
		if amount == "" {
			amount = plan.Price.StringFixed(2)
		}
		if subject == "" {
			subject = plan.Name
		}
	case paymentdomain.PurposeWalletRecharge:
		needSubject := subject == ""
		if needSubject {
			subject = "余额充值"
		}
		packageID := metaInt64(metadata, paymentdomain.MetaKeyWalletPackageID)
		if amount == "" && packageID > 0 && s.pg != nil {
			if p, err := s.pg.GetWalletPackage(ctx, appID, packageID); err == nil && p != nil {
				amount = p.Amount.StringFixed(2)
				if needSubject && p.Title != "" {
					subject = "余额充值：" + p.Title
				}
			}
		}
	case paymentdomain.PurposeIntegralPurchase:
		if subject == "" {
			subject = "积分充值"
		}
	}
	return amount, subject, nil
}

// QuoteIntent 试算用：把请求里的用途写法收成规范的用途、套餐 ID 与档位 ID（与下单同一套归一化）。
func (s *PaymentService) QuoteIntent(payType string, intent OrderIntent, metadata map[string]any) (OrderIntent, error) {
	meta, _, err := normalizeOrderIntent(payType, intent, metadata)
	if err != nil {
		return OrderIntent{}, err
	}
	return OrderIntent{
		Purpose:   metaString(meta, paymentdomain.MetaKeyPurpose),
		PlanID:    metaInt64(meta, paymentdomain.MetaKeyVipPlanID),
		PackageID: metaInt64(meta, paymentdomain.MetaKeyWalletPackageID),
	}, nil
}

// ListPrice 一笔订单的原价：会员取套餐价，按档位充值取档位价，否则用请求里的金额（试算用，与下单同一个口径）。
func (s *PaymentService) ListPrice(ctx context.Context, appID int64, purpose string, planID, packageID int64, amount string) (decimal.Decimal, error) {
	meta := map[string]any{}
	if purpose != "" {
		meta[paymentdomain.MetaKeyPurpose] = purpose
	}
	if planID > 0 {
		meta[paymentdomain.MetaKeyVipPlanID] = planID
	}
	if packageID > 0 {
		meta[paymentdomain.MetaKeyWalletPackageID] = packageID
	}
	resolved, _, err := s.resolveOrderPricing(ctx, appID, meta, amount, "-")
	if err != nil {
		return decimal.Zero, err
	}
	price, err := decimal.NewFromString(strings.TrimSpace(resolved))
	if err != nil || !price.IsPositive() {
		return decimal.Zero, apperrors.New(40072, http.StatusBadRequest, "支付金额无效")
	}
	return price.Round(2), nil
}
