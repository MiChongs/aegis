package service

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	authdomain "aegis/internal/domain/auth"
	paymentdomain "aegis/internal/domain/payment"
	platformdomain "aegis/internal/domain/platform"
	apperrors "aegis/pkg/errors"
)

// ── 子类型路由 ──

// payTypeRouter 由「子类型决定用户被带去哪个应用付款」的渠道实现。
//
// 易支付系、码支付、V免签：同一套商户凭据，下单参数里的 type 直接决定跳支付宝还是微信。
// 这类渠道在用户端按子类型逐个列出，下单时类型必须是这条配置放行的其中之一。
//
// 其余渠道的 PayTypes 只是能力说明 —— Stripe、PayPal 的托管收银台自己让用户选卡还是钱包，
// 下单参数里的类型不起作用。它们在用户端只列一项，下单不带类型。
type payTypeRouter interface {
	// RoutedPayTypes 这条配置放行的子类型（按管理员填写的顺序）与请求未指明类型时的默认值。
	RoutedPayTypes(config map[string]any) (types []paymentdomain.PayTypeOption, fallback string)
}

// legacyDefaultPayType 请求不带类型时的历史默认值。
//
// 老客户端从来不传类型，一直被带去微信。默认值换成别的，等于在它们不知情的情况下
// 换掉了付款应用；所以只要这条配置还放行 wxpay，就继续用它。
const legacyDefaultPayType = "wxpay"

var epayPayTypes = []paymentdomain.PayTypeOption{
	payType("alipay", "支付宝", "跳转支付宝收银台"),
	payType("wxpay", "微信支付", "跳转微信收银台"),
	payType("qqpay", "QQ 钱包", ""),
	payType("bank", "网银", "银行卡快捷支付"),
}

var qrpayPayTypes = []paymentdomain.PayTypeOption{
	payType("alipay", "支付宝", ""),
	payType("wxpay", "微信支付", ""),
	payType("qqpay", "QQ 钱包", ""),
}

var vmqpayPayTypes = []paymentdomain.PayTypeOption{
	payType("alipay", "支付宝", ""),
	payType("wxpay", "微信支付", ""),
}

// knownPayTypeLabels 渠道没声明、但易支付站点常见的类型。管理员把它们写进 supportedTypes 时，
// 用户端至少能显示一个认得出的名字。
var knownPayTypeLabels = map[string]string{
	"alipay": "支付宝",
	"wxpay":  "微信支付",
	"qqpay":  "QQ 钱包",
	"bank":   "网银",
	"jdpay":  "京东支付",
	"usdt":   "USDT",
	"paypal": "PayPal",
	"unipay": "云闪付",
}

func (p *epayProvider) RoutedPayTypes(config map[string]any) ([]paymentdomain.PayTypeOption, string) {
	// 易支付把 type 原样交给站点，站点自己加的类型（jdpay、usdt）也能用，因此放行自定义类型。
	return routedPayTypes(epayPayTypes, config, true)
}

func (p *qrpayProvider) RoutedPayTypes(config map[string]any) ([]paymentdomain.PayTypeOption, string) {
	// 码支付把类型映射成 1/2/3，表外的类型会被悄悄当成微信，所以只认声明过的。
	return routedPayTypes(qrpayPayTypes, config, false)
}

func (p *vmqpayProvider) RoutedPayTypes(config map[string]any) ([]paymentdomain.PayTypeOption, string) {
	return routedPayTypes(vmqpayPayTypes, config, false)
}

// routedPayTypes 按配置里的 supportedTypes 过滤渠道声明的子类型。
//
// supportedTypes 留空表示放行声明的全部类型；填了就按填写顺序，用户端列表的顺序就是它。
// allowCustom 为真时，填了声明之外的类型也照样放行（名字查不到就原样显示）——
// 管理员明确写进去的，网关不该替他拦。
func routedPayTypes(declared []paymentdomain.PayTypeOption, config map[string]any, allowCustom bool) ([]paymentdomain.PayTypeOption, string) {
	configured := configStringList(config, "supportedTypes")
	types := make([]paymentdomain.PayTypeOption, 0, len(declared))
	if len(configured) == 0 {
		types = append(types, declared...)
	} else {
		seen := make(map[string]bool, len(configured))
		for _, raw := range configured {
			value := canonicalPayType(raw)
			if value == "" || seen[value] {
				continue
			}
			option, ok := findPayType(declared, value)
			if !ok {
				if !allowCustom {
					continue
				}
				option = payType(value, pickString(knownPayTypeLabels[value], strings.ToUpper(value)), "")
			}
			seen[value] = true
			types = append(types, option)
		}
		// 填的全是不认识的类型：与其一种都不放行（这条配置就此不能收款），不如退回声明的全部。
		if len(types) == 0 {
			types = append(types, declared...)
		}
	}
	fallback := ""
	if _, ok := findPayType(types, legacyDefaultPayType); ok {
		fallback = legacyDefaultPayType
	} else if len(types) > 0 {
		fallback = types[0].Value
	}
	return types, fallback
}

func findPayType(options []paymentdomain.PayTypeOption, value string) (paymentdomain.PayTypeOption, bool) {
	for _, option := range options {
		if option.Value == value {
			return option, true
		}
	}
	return paymentdomain.PayTypeOption{}, false
}

// canonicalPayType 类型的规范写法：小写，wechat 是 wxpay 的别名。
func canonicalPayType(value string) string {
	v := strings.ToLower(strings.TrimSpace(value))
	if v == "wechat" {
		return "wxpay"
	}
	return v
}

// resolveOrderPayType 下单时实际使用的子类型。
//
// 分子类型的渠道：未指明时取默认，指明了就必须在这条配置的放行范围内。此前类型原样
// 交给上游，后台「启用的支付类型」只是个摆设 —— 管理员关掉的类型，客户端改个参数照样能用。
//
// 返回值落进订单。此前订单里存的是请求原文，不传类型的订单 provider_type 永远是空串，
// 订单详情与凭证都说不出这笔钱是从哪个应用付的。
func resolveOrderPayType(provider paymentProvider, config *paymentdomain.Config, requested string) (string, error) {
	router, ok := provider.(payTypeRouter)
	if !ok || config == nil {
		return strings.TrimSpace(requested), nil
	}
	types, fallback := router.RoutedPayTypes(config.ConfigData)
	want := canonicalPayType(requested)
	if want == "" {
		return fallback, nil
	}
	if _, ok := findPayType(types, want); ok {
		return want, nil
	}
	return "", apperrors.New(errCodePayTypeUnavailable, http.StatusBadRequest, "该付款方式未开放")
}

// errCodePayTypeUnavailable 请求的子类型不在这条支付配置的放行范围内。
const errCodePayTypeUnavailable = 40116

// ── 用户端付款方式 ──

// UserPaymentMethods 当前应用对用户开放的付款方式。
//
// 用户端此前没有任何途径知道应用配了哪些渠道，只能不带参数下单、永远落在默认配置的
// 默认类型上 —— 后台配了支付宝、微信、QQ 钱包，App 里永远只有微信。
//
// purpose 为 wallet_recharge 时不列余额支付：充值订单不能用余额付（40092）。
func (s *PaymentService) UserPaymentMethods(ctx context.Context, session *authdomain.Session, purpose string) (*paymentdomain.UserPayMethods, error) {
	if session == nil {
		return nil, apperrors.New(40170, http.StatusUnauthorized, "未认证")
	}
	// 与下单同一道闸：应用被冻结收款时，列表里的每一项点下去都会失败，不如当场说清楚。
	if s.governance != nil {
		if err := s.governance.EnsureCapability(session.AppID, platformdomain.CapabilityPayment); err != nil {
			return nil, err
		}
	}
	configs, err := s.pg.ListPaymentConfigs(ctx, session.AppID, "", true)
	if err != nil {
		return nil, err
	}
	return buildUserPayMethods(s.providers, configs, strings.TrimSpace(purpose), s.resolveConfigCurrency), nil
}

// buildUserPayMethods 把支付配置展开成用户可选的付款方式。单独拆出来是为了不连库也能测。
//
// configs 必须按 is_default DESC, id ASC 排好（ListPaymentConfigs 的顺序）：第一条就是
// 不指定配置下单时 GetPaymentConfig 选中的那条，它的默认类型就是「默认付款方式」。
//
// 凭据不完整的配置不列：它在下单时一定失败，列出来只是让用户点一个必然报错的选项。
func buildUserPayMethods(
	providers map[string]paymentProvider,
	configs []paymentdomain.Config,
	purpose string,
	currencyOf func(method string, data map[string]any) string,
) *paymentdomain.UserPayMethods {
	result := &paymentdomain.UserPayMethods{Items: []paymentdomain.UserPayMethod{}}
	for index, config := range configs {
		if !config.Enabled {
			continue
		}
		provider, ok := providers[strings.TrimSpace(config.PaymentMethod)]
		if !ok {
			continue
		}
		if config.PaymentMethod == paymentdomain.MethodBalance && purpose == paymentdomain.PurposeWalletRecharge {
			continue
		}
		if err := provider.ValidateConfig(config.ConfigData); err != nil {
			continue
		}
		meta := provider.Describe()
		base := paymentdomain.UserPayMethod{
			Method:     config.PaymentMethod,
			ConfigName: config.ConfigName,
			Channel:    userChannelName(config.PaymentMethod, meta.Name),
			Currency:   currencyOf(config.PaymentMethod, config.ConfigData),
			MinAmount:  configAmountString(config.ConfigData, "minAmount"),
			MaxAmount:  configAmountString(config.ConfigData, "maxAmount"),
		}
		isDefaultConfig := index == 0

		router, routed := provider.(payTypeRouter)
		if !routed {
			item := base
			item.Key = userPayMethodKey(config.PaymentMethod, config.ConfigName, "")
			item.Label = base.Channel
			item.IsDefault = isDefaultConfig
			result.Items = append(result.Items, item)
			if item.IsDefault {
				result.DefaultKey = item.Key
			}
			continue
		}
		types, fallback := router.RoutedPayTypes(config.ConfigData)
		for _, option := range types {
			item := base
			item.Type = option.Value
			item.Key = userPayMethodKey(config.PaymentMethod, config.ConfigName, option.Value)
			item.Label = option.Label
			item.Description = option.Description
			item.IsDefault = isDefaultConfig && option.Value == fallback
			result.Items = append(result.Items, item)
			if item.IsDefault {
				result.DefaultKey = item.Key
			}
		}
	}
	return result
}

func userPayMethodKey(method, configName, payType string) string {
	return fmt.Sprintf("%s:%s:%s", method, configName, payType)
}

// userChannelName 给用户看的渠道名：去掉控制台里那半截英文注解（「易支付 (Epay)」→「易支付」），
// 直连渠道直接叫它的付款应用名，「支付宝原生」这种说法是写给管理员的。
func userChannelName(method, name string) string {
	switch method {
	case paymentdomain.MethodAlipayNative:
		return "支付宝"
	case paymentdomain.MethodWechatNative:
		return "微信支付"
	}
	trimmed := strings.TrimSpace(name)
	for _, open := range []string{" (", "（", "("} {
		if i := strings.Index(trimmed, open); i > 0 {
			trimmed = strings.TrimSpace(trimmed[:i])
			break
		}
	}
	return pickString(trimmed, method)
}

// configAmountString 限额转成十进制字符串；未配置（0 或缺失）时返回空串，表示不限。
func configAmountString(data map[string]any, key string) string {
	value := configFloat(data, key)
	if value <= 0 {
		return ""
	}
	return strconv.FormatFloat(value, 'f', 2, 64)
}

// configStringList 读取配置里的字符串数组。控制台的标签输入存成 JSON 数组，
// 早期手写的配置里也见过逗号分隔的字符串，两种都认。
func configStringList(data map[string]any, key string) []string {
	if data == nil {
		return nil
	}
	var raw []string
	switch v := data[key].(type) {
	case []string:
		raw = v
	case []any:
		for _, item := range v {
			if text, ok := item.(string); ok {
				raw = append(raw, text)
			}
		}
	case string:
		raw = strings.Split(v, ",")
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
