package postgres

import (
	"strings"
	"testing"
)

// 应用级下单（PaymentService.CreateOrder）传的 paymentMethod 恒为空串 ——
// 客户端根本没有指定渠道的字段，只能给 config_name 或什么都不给。
//
// 此前构造时无条件拼 `payment_method = $2`，而该列 NOT NULL、存的是 'epay'/'alipay'
// 这类真值，空串匹配零行：应用级下单**无论应用配了什么**都返回 40471「未找到可用支付配置」。
// 这是一次静默零匹配 —— SQL 合法、无报错、结果恒空，所以它在真实流量里活了很久。
//
// 这组用例守的就是「空渠道时不许拼出这个谓词」。
func TestPaymentConfigLookupOmitsMethodPredicateWhenUnscoped(t *testing.T) {
	cases := []struct {
		name          string
		paymentMethod string
		configName    string
		wantMethod    bool // 是否应出现 payment_method = 谓词
		wantName      bool // 是否应出现 config_name = 谓词
		wantEnabled   bool // 是否应出现 enabled = TRUE 过滤
		wantArgs      []any
	}{
		{
			name:        "应用级下单：不限渠道、不指定配置名 —— 取默认",
			wantEnabled: true,
			wantArgs:    []any{int64(10000)},
		},
		{
			name:       "应用级下单：不限渠道、指定配置名",
			configName: "default",
			wantName:   true,
			wantArgs:   []any{int64(10000), "default"},
		},
		{
			name:          "回调路径：指定渠道、不指定配置名",
			paymentMethod: "alipay",
			wantMethod:    true,
			wantEnabled:   true,
			wantArgs:      []any{int64(10000), "alipay"},
		},
		{
			name:          "管理端精确定位：渠道与配置名都给",
			paymentMethod: "epay",
			configName:    "主商户",
			wantMethod:    true,
			wantName:      true,
			wantArgs:      []any{int64(10000), "epay", "主商户"},
		},
		{
			// 只有空格的渠道名等同于没给：否则前端一个多余空格就让下单恒失败
			name:          "纯空白的渠道名按未指定处理",
			paymentMethod: "   ",
			wantEnabled:   true,
			wantArgs:      []any{int64(10000)},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			query, args := buildPaymentConfigLookup(10000, tc.paymentMethod, tc.configName)

			if got := strings.Contains(query, "payment_method = $"); got != tc.wantMethod {
				t.Errorf("payment_method 谓词：得到 %v，期望 %v\nSQL: %s", got, tc.wantMethod, query)
			}
			if got := strings.Contains(query, "config_name = $"); got != tc.wantName {
				t.Errorf("config_name 谓词：得到 %v，期望 %v\nSQL: %s", got, tc.wantName, query)
			}
			if got := strings.Contains(query, "enabled = TRUE"); got != tc.wantEnabled {
				t.Errorf("enabled 过滤：得到 %v，期望 %v\nSQL: %s", got, tc.wantEnabled, query)
			}

			if len(args) != len(tc.wantArgs) {
				t.Fatalf("参数个数：得到 %d %v，期望 %d %v", len(args), args, len(tc.wantArgs), tc.wantArgs)
			}
			for i := range args {
				if args[i] != tc.wantArgs[i] {
					t.Errorf("参数 $%d：得到 %v，期望 %v", i+1, args[i], tc.wantArgs[i])
				}
			}
		})
	}
}

// 占位符编号必须随实际拼进去的谓词连续递增。跳号或重号在 pgx 上是运行期错误，
// 而这个函数有四种分支组合，靠肉眼看不出来。
func TestPaymentConfigLookupPlaceholdersAreSequential(t *testing.T) {
	combos := []struct{ method, name string }{
		{"", ""},
		{"", "default"},
		{"alipay", ""},
		{"epay", "主商户"},
	}
	for _, c := range combos {
		query, args := buildPaymentConfigLookup(1, c.method, c.name)
		for i := 1; i <= len(args); i++ {
			want := "$" + string(rune('0'+i))
			if !strings.Contains(query, want) {
				t.Errorf("method=%q name=%q：SQL 里缺少占位符 %s\nSQL: %s", c.method, c.name, want, query)
			}
		}
		// 多出一个占位符意味着有谓词引用了不存在的参数
		extra := "$" + string(rune('0'+len(args)+1))
		if strings.Contains(query, extra) {
			t.Errorf("method=%q name=%q：SQL 引用了越界占位符 %s\nSQL: %s", c.method, c.name, extra, query)
		}
	}
}

// 不限渠道时 config_name 可能在多个渠道下重名（唯一约束是 appid+method+name 三元组）。
// 没有确定排序的话，选中哪条取决于物理行序 —— 同一次下单可能今天走支付宝、明天走易支付。
func TestPaymentConfigLookupIsDeterministic(t *testing.T) {
	query, _ := buildPaymentConfigLookup(10000, "", "default")
	if !strings.Contains(query, "ORDER BY enabled DESC, is_default DESC, id ASC") {
		t.Fatalf("缺少确定性排序\nSQL: %s", query)
	}
	if !strings.HasSuffix(query, "LIMIT 1") {
		t.Fatalf("应只取一条\nSQL: %s", query)
	}
}
