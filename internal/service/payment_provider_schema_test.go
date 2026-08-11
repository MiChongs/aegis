package service

import (
	"strings"
	"testing"

	paymentdomain "aegis/internal/domain/payment"

	"github.com/go-resty/resty/v2"
	"github.com/shopspring/decimal"
)

// newTestPaymentGateway 构造只含渠道注册表的服务实例。
// 不走 NewPaymentService，避免后台清扫协程在无数据库连接时解引用 nil 仓储。
func newTestPaymentGateway() *PaymentService {
	s := &PaymentService{providers: make(map[string]paymentProvider)}
	s.registerBuiltinProviders(resty.New())
	return s
}

// 合法的配置字段类型集合
var validFieldTypes = map[string]bool{
	paymentdomain.FieldText:     true,
	paymentdomain.FieldSecret:   true,
	paymentdomain.FieldNumber:   true,
	paymentdomain.FieldSwitch:   true,
	paymentdomain.FieldSelect:   true,
	paymentdomain.FieldTextarea: true,
	paymentdomain.FieldTags:     true,
	paymentdomain.FieldURL:      true,
}

var validCategories = map[string]bool{
	paymentdomain.CategoryInternal:      true,
	paymentdomain.CategoryAggregate:     true,
	paymentdomain.CategoryOfficialCN:    true,
	paymentdomain.CategoryInternational: true,
	paymentdomain.CategoryCrypto:        true,
}

// TestProviderDescriptorsAreComplete 渠道自描述是控制台动态表单的唯一数据源，
// 任何一项缺失都会导致前端渲染出无标签/无类型的空控件，因此逐项断言。
func TestProviderDescriptorsAreComplete(t *testing.T) {
	gateway := newTestPaymentGateway()

	for method, provider := range gateway.providers {
		meta := provider.Describe()

		if meta.Method != method {
			t.Errorf("%s: Describe().Method = %q，与注册键不一致", method, meta.Method)
		}
		if strings.TrimSpace(meta.Name) == "" {
			t.Errorf("%s: 缺少展示名", method)
		}
		if strings.TrimSpace(meta.Description) == "" {
			t.Errorf("%s: 缺少渠道说明（渠道市场卡片需要）", method)
		}
		if !validCategories[meta.Category] {
			t.Errorf("%s: 分组 %q 不是已知分组", method, meta.Category)
		}
		if strings.TrimSpace(meta.CategoryName) == "" {
			t.Errorf("%s: finalizeMeta 未补齐分组中文名", method)
		}
		if strings.TrimSpace(meta.Icon) == "" {
			t.Errorf("%s: 缺少图标 slug", method)
		}
		if len(meta.PayTypes) == 0 {
			t.Errorf("%s: 未声明任何子支付类型", method)
		}
		if len(meta.SupportedTypes) != len(meta.PayTypes) {
			t.Errorf("%s: SupportedTypes 未由 PayTypes 派生（%d vs %d）",
				method, len(meta.SupportedTypes), len(meta.PayTypes))
		}

		// 有回调能力的渠道必须给出回调路径，否则控制台无法提示接入地址
		if meta.Capabilities.Webhook && strings.TrimSpace(meta.CallbackPath) == "" {
			t.Errorf("%s: 声明支持 Webhook 却未给出 callbackPath", method)
		}

		seen := make(map[string]bool, len(meta.Fields))
		for _, field := range meta.Fields {
			if strings.TrimSpace(field.Key) == "" {
				t.Errorf("%s: 存在无 key 的配置字段", method)
				continue
			}
			if seen[field.Key] {
				t.Errorf("%s: 配置字段 %q 重复声明", method, field.Key)
			}
			seen[field.Key] = true

			if strings.TrimSpace(field.Label) == "" {
				t.Errorf("%s.%s: 缺少字段标签", method, field.Key)
			}
			if !validFieldTypes[field.Type] {
				t.Errorf("%s.%s: 字段类型 %q 前端无对应控件", method, field.Key, field.Type)
			}
			if field.Type == paymentdomain.FieldSelect && len(field.Options) == 0 {
				t.Errorf("%s.%s: select 字段没有可选项", method, field.Key)
			}
		}
	}
}

// TestAvailableMethodsIsStableAndComplete 渠道列表顺序必须稳定（map 迭代顺序随机会让控制台列表跳动），
// 且不能漏掉任何已注册渠道。
func TestAvailableMethodsIsStableAndComplete(t *testing.T) {
	gateway := newTestPaymentGateway()

	first := gateway.AvailableMethods()
	if len(first) != len(gateway.providers) {
		t.Fatalf("AvailableMethods 返回 %d 项，已注册 %d 个渠道", len(first), len(gateway.providers))
	}

	// 连续多次调用必须给出完全相同的顺序
	for i := 0; i < 20; i++ {
		again := gateway.AvailableMethods()
		for idx := range first {
			if again[idx].Method != first[idx].Method {
				t.Fatalf("第 %d 次调用顺序漂移：位置 %d 为 %q，首次为 %q",
					i, idx, again[idx].Method, first[idx].Method)
			}
		}
	}

	// methodOrder 覆盖全部已注册渠道，新增渠道时不会被排到兜底段
	ordered := make(map[string]bool, len(methodOrder))
	for _, m := range methodOrder {
		ordered[m] = true
	}
	for method := range gateway.providers {
		if !ordered[method] {
			t.Errorf("渠道 %q 未登记进 methodOrder，展示顺序不稳定", method)
		}
	}
}

// TestEnforceAmountLimits 限额此前只有 2/11 个渠道自行校验，现由网关统一执行，
// 因此需要覆盖「读取任意渠道配置 map」这条通路。
func TestEnforceAmountLimits(t *testing.T) {
	gateway := newTestPaymentGateway()
	provider := gateway.providers[paymentdomain.MethodStripe]

	cases := []struct {
		name    string
		data    map[string]any
		amount  string
		wantErr bool
	}{
		{"未配置限额则放行", map[string]any{}, "0.01", false},
		{"低于下限被拒", map[string]any{"minAmount": 10.0}, "9.99", true},
		{"等于下限放行", map[string]any{"minAmount": 10.0}, "10", false},
		{"高于上限被拒", map[string]any{"maxAmount": 100.0}, "100.01", true},
		{"等于上限放行", map[string]any{"maxAmount": 100.0}, "100", false},
		{"区间内放行", map[string]any{"minAmount": 1.0, "maxAmount": 100.0}, "50", false},
		{"零值视为不限制", map[string]any{"minAmount": 0.0, "maxAmount": 0.0}, "999999", false},
		// 配置经 JSON 往返后数值可能是字符串，configFloat 需要能解出来
		{"字符串下限同样生效", map[string]any{"minAmount": "10"}, "9.99", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			amount, err := decimal.NewFromString(tc.amount)
			if err != nil {
				t.Fatalf("测试金额无效: %v", err)
			}
			config := &paymentdomain.Config{ConfigData: tc.data}
			err = enforceAmountLimits(provider, config, amount)
			if tc.wantErr && err == nil {
				t.Errorf("金额 %s 应被限额拦截，实际放行", tc.amount)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("金额 %s 应放行，实际被拒: %v", tc.amount, err)
			}
		})
	}
}

// TestEnforceAmountLimitsIgnoresNilConfig 兜底：配置缺失时不应 panic
func TestEnforceAmountLimitsIgnoresNilConfig(t *testing.T) {
	gateway := newTestPaymentGateway()
	if err := enforceAmountLimits(gateway.providers[paymentdomain.MethodBalance], nil, decimal.NewFromInt(1)); err != nil {
		t.Errorf("配置为空时应放行，实际: %v", err)
	}
}
