package service

import (
	paymentdomain "aegis/internal/domain/payment"
)

// ── 渠道自描述构造工具 ──
//
// 每个 Provider 的 Describe() 用这里的构造器声明「配置字段 schema」，
// 控制台据此动态渲染表单。新增渠道只需在 Go 侧补一个 Describe()，
// 前端渠道市场与配置表单自动生效，无需改动任何 TSX。

// ── 字段构造器 ──

func fText(key, label, placeholder, help string, required bool) paymentdomain.ConfigField {
	return paymentdomain.ConfigField{
		Key: key, Label: label, Type: paymentdomain.FieldText,
		Placeholder: placeholder, Help: help, Required: required,
	}
}

func fSecret(key, label, placeholder, help string, required bool) paymentdomain.ConfigField {
	return paymentdomain.ConfigField{
		Key: key, Label: label, Type: paymentdomain.FieldSecret,
		Placeholder: placeholder, Help: help, Required: required,
	}
}

// fArea 多行文本：证书 / PEM 私钥这类无法单行录入的凭据
func fArea(key, label, placeholder, help string, required bool) paymentdomain.ConfigField {
	return paymentdomain.ConfigField{
		Key: key, Label: label, Type: paymentdomain.FieldTextarea,
		Placeholder: placeholder, Help: help, Required: required,
	}
}

func fURL(key, label, placeholder, help string) paymentdomain.ConfigField {
	return paymentdomain.ConfigField{
		Key: key, Label: label, Type: paymentdomain.FieldURL,
		Placeholder: placeholder, Help: help,
	}
}

func fNum(key, label, placeholder, help string, def any) paymentdomain.ConfigField {
	return paymentdomain.ConfigField{
		Key: key, Label: label, Type: paymentdomain.FieldNumber,
		Placeholder: placeholder, Help: help, Default: def,
	}
}

func fSwitch(key, label, help string, def bool) paymentdomain.ConfigField {
	return paymentdomain.ConfigField{
		Key: key, Label: label, Type: paymentdomain.FieldSwitch,
		Help: help, Default: def,
	}
}

func fSelect(key, label, help string, def string, options ...paymentdomain.FieldOption) paymentdomain.ConfigField {
	return paymentdomain.ConfigField{
		Key: key, Label: label, Type: paymentdomain.FieldSelect,
		Help: help, Default: def, Options: options,
	}
}

func fTags(key, label, placeholder, help string) paymentdomain.ConfigField {
	return paymentdomain.ConfigField{
		Key: key, Label: label, Type: paymentdomain.FieldTags,
		Placeholder: placeholder, Help: help,
	}
}

func opt(value, label string) paymentdomain.FieldOption {
	return paymentdomain.FieldOption{Value: value, Label: label}
}

func payType(value, label, description string) paymentdomain.PayTypeOption {
	return paymentdomain.PayTypeOption{Value: value, Label: label, Description: description}
}

// ── 字段编组 ──

// inGroup 把一批字段归入同一分区（前端按分区折叠渲染）
func inGroup(group string, fields ...paymentdomain.ConfigField) []paymentdomain.ConfigField {
	out := make([]paymentdomain.ConfigField, 0, len(fields))
	for _, f := range fields {
		f.Group = group
		out = append(out, f)
	}
	return out
}

// advanced 把一批字段标记为高级选项（前端默认折叠）
func advanced(fields ...paymentdomain.ConfigField) []paymentdomain.ConfigField {
	out := make([]paymentdomain.ConfigField, 0, len(fields))
	for _, f := range fields {
		f.Advanced = true
		out = append(out, f)
	}
	return out
}

// fields 串接多个分组，保持声明顺序
func fields(groups ...[]paymentdomain.ConfigField) []paymentdomain.ConfigField {
	total := 0
	for _, g := range groups {
		total += len(g)
	}
	out := make([]paymentdomain.ConfigField, 0, total)
	for _, g := range groups {
		out = append(out, g...)
	}
	return out
}

// ── 通用分区 ──

// callbackFields 网关分区的通用回调地址字段（几乎所有渠道都有）
func callbackFields(notifyHelp, returnHelp string) []paymentdomain.ConfigField {
	return inGroup(paymentdomain.GroupGateway,
		fURL("notifyUrl", "异步通知地址", "https://your-domain.com/api/public/pay/callback/...", notifyHelp),
		fURL("returnUrl", "同步跳转地址", "https://your-domain.com/pay/result", returnHelp),
	)
}

// limitFields 交易限额分区（网关层统一强制执行，见 enforceAmountLimits）
func limitFields(minPlaceholder, maxPlaceholder string) []paymentdomain.ConfigField {
	return inGroup(paymentdomain.GroupLimit,
		fNum("minAmount", "单笔最小金额", minPlaceholder, "低于该金额的下单请求会被网关拒绝；0 表示不限制", nil),
		fNum("maxAmount", "单笔最大金额", maxPlaceholder, "高于该金额的下单请求会被网关拒绝；0 表示不限制", nil),
	)
}

// ── 元数据收尾 ──

// categoryNames 分组标识 → 中文名
var categoryNames = map[string]string{
	paymentdomain.CategoryInternal:      "内部通道",
	paymentdomain.CategoryAggregate:     "聚合支付",
	paymentdomain.CategoryOfficialCN:    "国内官方直连",
	paymentdomain.CategoryInternational: "国际收单",
	paymentdomain.CategoryCrypto:        "加密货币",
}

// finalizeMeta 补齐派生字段：分组中文名、兼容用的扁平子类型列表。
// 所有 Describe() 的返回值都应经过它，避免各处重复填写。
func finalizeMeta(meta paymentdomain.ProviderMeta) paymentdomain.ProviderMeta {
	if meta.CategoryName == "" {
		meta.CategoryName = categoryNames[meta.Category]
	}
	if len(meta.SupportedTypes) == 0 && len(meta.PayTypes) > 0 {
		types := make([]string, 0, len(meta.PayTypes))
		for _, t := range meta.PayTypes {
			types = append(types, t.Value)
		}
		meta.SupportedTypes = types
	}
	return meta
}
