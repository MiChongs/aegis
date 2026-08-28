package service

import (
	"testing"

	aidomain "aegis/internal/domain/ai"
)

// TestNormalizeAIBaseURL 站点地址 → 端点根地址的归一规则：
// NewAPI / OneAPI 这类分发站只填站点根地址也必须落到 /v1 形状。
func TestNormalizeAIBaseURL(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"裸站点地址补 /v1（NewAPI 形态）", "https://api.example.com", "https://api.example.com/v1"},
		{"带端口的内网地址补 /v1", "http://10.0.0.8:11434", "http://10.0.0.8:11434/v1"},
		{"已带 /v1 原样保留", "https://api.example.com/v1", "https://api.example.com/v1"},
		{"智谱 /v4 原样保留", "https://open.bigmodel.cn/api/paas/v4", "https://open.bigmodel.cn/api/paas/v4"},
		{"豆包 /api/v3 原样保留", "https://ark.cn-beijing.volces.com/api/v3", "https://ark.cn-beijing.volces.com/api/v3"},
		{"Gemini 版本段在中间也不再补", "https://generativelanguage.googleapis.com/v1beta/openai",
			"https://generativelanguage.googleapis.com/v1beta/openai"},
		{"粘了完整 OpenAI 端点 → 掐掉端点段", "https://api.example.com/v1/chat/completions", "https://api.example.com/v1"},
		{"任意挂载前缀的完整端点 → 掐完不补版本段", "https://gw.internal/llm/chat/completions", "https://gw.internal/llm"},
		{"粘了完整 Anthropic 端点 → 掐掉端点段", "https://api.example.com/v1/messages", "https://api.example.com/v1"},
		{"大小写不敏感", "https://API.example.com/V1", "https://API.example.com/V1"},
		{"空串原样返回", "", ""},
	}
	for _, tc := range cases {
		if got := normalizeAIBaseURL(tc.in); got != tc.want {
			t.Errorf("%s：normalizeAIBaseURL(%q) = %q，想要 %q", tc.name, tc.in, got, tc.want)
		}
	}
}

// TestAIVersionSegment 版本段判定只认 v+数字开头的路径段。
func TestAIVersionSegment(t *testing.T) {
	for segment, want := range map[string]bool{
		"v1": true, "v4": true, "v1beta": true, "v1alpha1": true, "V2": true,
		"openai": false, "api": false, "v": false, "version": false, "vx": false, "": false,
	} {
		if got := aiVersionSegment(segment); got != want {
			t.Errorf("aiVersionSegment(%q) = %v，想要 %v", segment, got, want)
		}
	}
}

// TestBaseURLPerProvider baseURL 的供应商差异：
// 普通供应商自动补全，custom-* 与 Azure 保持原样拼接契约。
func TestBaseURLPerProvider(t *testing.T) {
	client := newAILLMClient(nil)
	build := func(provider, base string) aidomain.Config {
		return aidomain.Config{
			Provider: provider,
			Settings: map[string]string{aidomain.KeyBaseURL: base},
		}
	}

	// NewAPI 卡片：裸站点地址补全 /v1。
	newapiMeta, _ := aidomain.ProviderByKey(aidomain.ProviderNewAPI)
	if got := client.baseURL(build(aidomain.ProviderNewAPI, "https://api.example.com/"), newapiMeta); got != "https://api.example.com/v1" {
		t.Errorf("NewAPI 裸站点应补 /v1，得到 %q", got)
	}

	// OpenAI 官方：留空回落目录默认端点（已带 /v1，原样）。
	openaiMeta, _ := aidomain.ProviderByKey(aidomain.ProviderOpenAI)
	if got := client.baseURL(build(aidomain.ProviderOpenAI, ""), openaiMeta); got != "https://api.openai.com/v1" {
		t.Errorf("OpenAI 默认端点不对：%q", got)
	}

	// 自定义兼容端点：按原样使用，不补版本段。
	customMeta, _ := aidomain.ProviderByKey(aidomain.ProviderCustomOpenAI)
	if got := client.baseURL(build(aidomain.ProviderCustomOpenAI, "https://gw.internal/llm"), customMeta); got != "https://gw.internal/llm" {
		t.Errorf("custom-openai 不该被改写，得到 %q", got)
	}

	// Azure：资源端点原样保留，路径由调用方拼部署段。
	azureMeta, _ := aidomain.ProviderByKey(aidomain.ProviderAzureOpenAI)
	if got := client.baseURL(build(aidomain.ProviderAzureOpenAI, "https://res.openai.azure.com"), azureMeta); got != "https://res.openai.azure.com" {
		t.Errorf("Azure 资源端点不该被改写，得到 %q", got)
	}
}
