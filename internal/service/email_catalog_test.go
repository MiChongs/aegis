package service

import (
	emaildomain "aegis/internal/domain/email"
	"strings"
	"testing"

	"go.uber.org/zap"
)

// newCatalogTestService 只装发送器，不碰数据库与 Redis。
// 目录相关的判定全是纯函数，没有理由为了测它去起一套依赖。
func newCatalogTestService(t *testing.T) *EmailService {
	t.Helper()
	log := zap.NewNop()
	return &EmailService{
		log: log,
		senders: map[string]emailSender{
			emaildomain.ProviderSMTP:     newSMTPEmailSender(log),
			emaildomain.ProviderZeabur:   newZeaburEmailSender(log),
			emaildomain.ProviderSES:      newSESEmailSender(log),
			emaildomain.ProviderResend:   newResendEmailSender(log),
			emaildomain.ProviderSendGrid: newSendGridEmailSender(log),
			emaildomain.ProviderMailgun:  newMailgunEmailSender(log),
			emaildomain.ProviderPostmark: newPostmarkEmailSender(log),
			emaildomain.ProviderAliyun:   newAliyunEmailSender(log),
			emaildomain.ProviderTencent:  newTencentEmailSender(log),
		},
	}
}

// 目录是「服务端校验 + 控制台表单 + 能力自述」三处的**单一事实源**，
// 因此它自身的完整性必须有测试盯着：这里错一条，表现是控制台上少一个输入框
// 或多一个存不进去的字段，而两边都不会报错。
func TestEmailProviderCatalogIsWellFormed(t *testing.T) {
	t.Parallel()

	service := newCatalogTestService(t)
	for _, meta := range service.ProviderCatalog() {
		if meta.Provider == "" || meta.Name == "" {
			t.Fatalf("服务商自述缺少标识或名称：%+v", meta)
		}
		// Describe() 里的 provider 必须与注册键一致，否则控制台按 provider 反查
		// 元数据时会落空，表单变成一片空白且不报错。
		if _, ok := service.senders[meta.Provider]; !ok {
			t.Errorf("%s：Describe() 自报的 provider 与注册键对不上", meta.Provider)
		}
		if meta.CategoryName == "" {
			t.Errorf("%s：分类中文名为空，控制台的服务商分组会出现一个没有标题的组", meta.Provider)
		}

		seen := map[string]bool{}
		hasFromAddress := false
		for _, field := range meta.Fields {
			if field.Key == "" || field.Label == "" || field.Type == "" {
				t.Errorf("%s：字段声明不完整 %+v", meta.Provider, field)
			}
			if seen[field.Key] {
				// 重键会让后一个字段静默覆盖前一个：控制台上两个框、库里一个值。
				t.Errorf("%s：字段键 %q 重复", meta.Provider, field.Key)
			}
			seen[field.Key] = true
			if field.Type == emaildomain.FieldSelect && len(field.Options) == 0 {
				t.Errorf("%s：字段 %q 是下拉但没有选项", meta.Provider, field.Key)
			}
			if field.Key == emaildomain.KeyFromAddress {
				hasFromAddress = true
			}
			// Secret 与 FieldSecret 必须成对：只标其一时，密钥要么不加密落库、
			// 要么在控制台上以明文输入框出现，两种都不会报错。
			if field.Secret != (field.Type == emaildomain.FieldSecret) {
				t.Errorf("%s：字段 %q 的 Secret 标记与类型不一致", meta.Provider, field.Key)
			}
		}
		if !hasFromAddress {
			t.Errorf("%s：没有声明发件地址字段，SenderIdentity 会取到空值", meta.Provider)
		}
		if meta.WebhookPath != "" && !strings.Contains(meta.WebhookPath, "{scope}") {
			t.Errorf("%s：回执地址模板缺少 {scope} 占位符，平台级与应用级会共用同一个地址", meta.Provider)
		}
		if meta.Capabilities.Webhook && meta.WebhookPath == "" {
			t.Errorf("%s：自称支持投递回执却没给回调地址，管理员无从配置", meta.Provider)
		}
	}
}

// 展示顺序必须覆盖全部已注册服务商。
// 漏登记一个的表现是它排到列表末尾并随 map 迭代顺序在刷新之间跳动。
func TestEmailProviderDisplayOrderCoversAllSenders(t *testing.T) {
	t.Parallel()

	service := newCatalogTestService(t)
	ordered := map[string]bool{}
	for _, key := range emailProviderDisplayOrder {
		if ordered[key] {
			t.Fatalf("展示顺序里 %q 出现了两次", key)
		}
		ordered[key] = true
		if _, ok := service.senders[key]; !ok {
			t.Errorf("展示顺序里的 %q 没有对应的发送器", key)
		}
	}
	for key := range service.senders {
		if !ordered[key] {
			t.Errorf("发送器 %q 没有登记进展示顺序，列表位置会随每次刷新跳动", key)
		}
	}
	// 顺序稳定：连续两次取应当完全一致（map 迭代顺序是随机的）。
	first := strings.Join(service.providerKeys(), ",")
	for i := 0; i < 20; i++ {
		if got := strings.Join(service.providerKeys(), ","); got != first {
			t.Fatalf("服务商顺序不稳定：%s != %s", got, first)
		}
	}
}

// 出网响应绝不能带出任何密钥。判据来自目录而不是一串写死的字段名 ——
// 新增一个密钥字段时忘了在 sanitize 里补一行，表现是密钥经管理接口回流到浏览器，
// 而那种泄露不会有任何报错。
func TestSanitizeStripsEverySecretDeclaredInCatalog(t *testing.T) {
	t.Parallel()

	service := newCatalogTestService(t)
	for _, meta := range service.ProviderCatalog() {
		secretKeys := meta.SecretKeys()
		if len(secretKeys) == 0 {
			continue
		}
		config := emaildomain.Config{
			Provider:      meta.Provider,
			Secrets:       map[string]string{},
			SecretsCipher: map[string]string{},
		}
		for _, key := range secretKeys {
			config.Secrets[key] = "plain-" + key
			config.SecretsCipher[key] = "cipher-" + key
		}
		service.sanitizeConfig(&config)

		if config.Secrets != nil || config.SecretsCipher != nil {
			t.Fatalf("%s：脱敏后仍带着密钥 %+v", meta.Provider, config)
		}
		for _, key := range secretKeys {
			if !config.SecretSet[key] {
				t.Errorf("%s：密钥 %q 脱敏后丢了「已配置」的布尔位，前端会把它显示成未配置",
					meta.Provider, key)
			}
		}
	}
}

// 目录驱动的通用校验：必填缺失要报错，且报错里必须带字段中文名 ——
// 九家服务商共用同一个错误码，只说「不能为空」的话没人知道是哪个框。
func TestValidateByCatalogReportsFieldName(t *testing.T) {
	t.Parallel()

	meta := emaildomain.ProviderMeta{
		Provider: "demo",
		Fields: []emaildomain.ConfigField{
			{Key: "host", Label: "服务器地址", Type: emaildomain.FieldText, Required: true},
			{Key: emaildomain.KeyFromAddress, Label: "发件地址", Type: emaildomain.FieldEmail, Required: true},
			{Key: "region", Label: "地域", Type: emaildomain.FieldSelect,
				Options: []emaildomain.FieldOption{{Value: "cn", Label: "中国"}}},
		},
	}

	err := validateByCatalog(meta, emaildomain.Config{})
	if err == nil || !strings.Contains(err.Error(), "服务器地址") {
		t.Fatalf("必填缺失应指名字段，实际: %v", err)
	}

	config := emaildomain.Config{Settings: map[string]string{
		"host":                     "smtp.example.com",
		emaildomain.KeyFromAddress: "not-an-email",
	}}
	if err := validateByCatalog(meta, config); err == nil || !strings.Contains(err.Error(), "发件地址") {
		t.Fatalf("非法邮箱应指名字段，实际: %v", err)
	}

	config.Settings[emaildomain.KeyFromAddress] = "noreply@example.com"
	config.Settings["region"] = "us"
	if err := validateByCatalog(meta, config); err == nil || !strings.Contains(err.Error(), "地域") {
		t.Fatalf("下拉取值越界应被拒绝，实际: %v", err)
	}

	config.Settings["region"] = "cn"
	if err := validateByCatalog(meta, config); err != nil {
		t.Fatalf("合法配置不应报错，实际: %v", err)
	}
}

// 密钥的「已配置」必须认密文：编辑配置时前端不回传原值（留空即不修改），
// 只看明文会把一条配好的通道判成没配，于是保存时报「API Key 不能为空」。
func TestValidateByCatalogAcceptsCipherOnlySecret(t *testing.T) {
	t.Parallel()

	meta := emaildomain.ProviderMeta{
		Fields: []emaildomain.ConfigField{
			{Key: "apiKey", Label: "API Key", Type: emaildomain.FieldSecret, Secret: true, Required: true},
		},
	}
	config := emaildomain.Config{SecretsCipher: map[string]string{"apiKey": "cipher"}}
	if err := validateByCatalog(meta, config); err != nil {
		t.Fatalf("仅有密文的密钥应判定为已配置，实际: %v", err)
	}
	if err := validateByCatalog(meta, emaildomain.Config{}); err == nil {
		t.Fatal("完全没有密钥时应校验失败")
	}
}

// 不支持附件的通道必须如实自述。
//
// 这一条直接决定凭证邮件的措辞：能带附件的写「收据见附件」，不能的写
// 「点下面的按钮下载」。乐观地一律返回 true 的后果是一封「您的收据见附件」
// 但没有附件的邮件，用户和运维都不会收到任何错误。
func TestAttachmentCapabilityMatchesSelfDescription(t *testing.T) {
	t.Parallel()

	service := newCatalogTestService(t)
	for key, sender := range service.senders {
		meta := sender.Describe()
		if meta.Capabilities.Attachments != sender.SupportsAttachments() {
			t.Errorf("%s：Describe() 说附件能力是 %v，SupportsAttachments() 说是 %v",
				key, meta.Capabilities.Attachments, sender.SupportsAttachments())
		}
	}
	// 已知带不了附件的两家，钉住以防哪天被顺手改成 true。
	for _, provider := range []string{emaildomain.ProviderZeabur, emaildomain.ProviderAliyun} {
		if service.senders[provider].SupportsAttachments() {
			t.Errorf("%s 的接口没有二进制附件字段，不能声明支持附件", provider)
		}
	}
}

// 平台作用域的判定与标签。
func TestPlatformScopeHelpers(t *testing.T) {
	t.Parallel()

	platform := emaildomain.Config{AppID: emaildomain.PlatformAppID}
	if !platform.IsPlatform() {
		t.Fatal("appid=0 应判定为平台级")
	}
	scoped := emaildomain.Config{AppID: 7}
	if scoped.IsPlatform() {
		t.Fatal("appid=7 不该被判定为平台级")
	}
	if emaildomain.ScopeLabel(emaildomain.PlatformAppID) != "平台级" ||
		emaildomain.ScopeLabel(7) != "应用级" {
		t.Fatal("作用域中文名有误")
	}
}

// 开关字段只认真值字面量。「解析不出来就当开」会让一个拼错的值
// （比如把 insecureSkipVerify 填成 "yes please"）静默关掉证书校验。
func TestSettingBoolOnlyAcceptsTruthyLiterals(t *testing.T) {
	t.Parallel()

	config := emaildomain.Config{Settings: map[string]string{
		"a": "true", "b": "1", "c": "on", "d": "yes",
		"e": "false", "f": "0", "g": "", "h": "maybe",
	}}
	for _, key := range []string{"a", "b", "c", "d"} {
		if !config.SettingBool(key) {
			t.Errorf("%q 应解析为 true", config.Setting(key))
		}
	}
	for _, key := range []string{"e", "f", "g", "h"} {
		if config.SettingBool(key) {
			t.Errorf("%q 应解析为 false", config.Setting(key))
		}
	}
}

// kv 字段以 JSON 对象字符串存放，解析失败时返回 nil 而不是半个 map。
func TestSettingMapParsesJSONObject(t *testing.T) {
	t.Parallel()

	config := emaildomain.Config{Settings: map[string]string{
		"tags": `{"env":"prod","team":"growth"}`,
		"bad":  `not-json`,
	}}
	tags := config.SettingMap("tags")
	if tags["env"] != "prod" || tags["team"] != "growth" {
		t.Fatalf("标签解析有误：%v", tags)
	}
	if config.SettingMap("bad") != nil || config.SettingMap("missing") != nil {
		t.Fatal("解析失败或缺失时应返回 nil")
	}
}
