package postgres

import (
	emaildomain "aegis/internal/domain/email"
	"encoding/json"
	"testing"
)

// 存量行的形态是「扁平 SMTP 字段 + 可选的 zeabur 子对象」。
// 解不出来的表现不是报错，而是配置**看起来是空的** —— 管理员会以为配置丢了，
// 于是重新填一遍，把已经能用的通道覆盖掉。因此这条读兼容必须有测试盯着。
func TestLegacySMTPPayloadHydratesIntoGenericSettings(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"host": "smtp.example.com",
		"port": 465,
		"username": "user@example.com",
		"password": "plain-secret",
		"fromAddress": "noreply@example.com",
		"fromName": "Aegis",
		"replyTo": "support@example.com",
		"useTLS": true,
		"insecureSkipVerify": true,
		"maxConnections": 5
	}`)

	item := hydrate(t, emaildomain.ProviderSMTP, raw)

	if item.Setting("host") != "smtp.example.com" || item.Setting("port") != "465" {
		t.Fatalf("SMTP 服务器信息未解出：%+v", item.Settings)
	}
	if item.Setting(emaildomain.KeyFromAddress) != "noreply@example.com" ||
		item.Setting(emaildomain.KeyFromName) != "Aegis" ||
		item.Setting(emaildomain.KeyReplyTo) != "support@example.com" {
		t.Fatalf("发件人身份未解出：%+v", item.Settings)
	}
	// useTLS=true 必须翻成 ssl。不翻的话，存量的 465 配置会在控制台上显示成
	// STARTTLS，而管理员保存一次就真的变成 STARTTLS 了 —— 一次静默的配置损坏。
	if item.Setting("encryption") != emaildomain.SMTPEncryptionSSL {
		t.Fatalf("useTLS=true 应翻成 ssl，实际: %q", item.Setting("encryption"))
	}
	if !item.SettingBool("insecureSkipVerify") {
		t.Fatal("跳过证书校验的开关未解出")
	}
	// 旧版 SMTP 密码是**明文**落库的，这里如实解进明文袋子，
	// 服务层下次保存时会加密写回（自愈），因此不需要迁移脚本。
	if item.Secret("password") != "plain-secret" {
		t.Fatalf("存量明文密码应被解出以便自愈，实际: %q", item.Secret("password"))
	}
	if !item.HasSecret("password") {
		t.Fatal("解出明文密码后应判定为已配置")
	}
}

// useTLS=false 的存量行翻成 STARTTLS：旧代码里 false 走的正是 TLSMandatory 分支。
func TestLegacySMTPPayloadMapsPlainTLSToStartTLS(t *testing.T) {
	t.Parallel()

	item := hydrate(t, emaildomain.ProviderSMTP, []byte(`{"host":"smtp.example.com","port":587}`))
	if item.Setting("encryption") != emaildomain.SMTPEncryptionSTARTTLS {
		t.Fatalf("useTLS=false 应翻成 starttls，实际: %q", item.Setting("encryption"))
	}
}

// 旧代码无论服务商是什么都会把 SMTP 段原样写进去（那一段是内嵌的），
// 因此一条 zeabur 配置的 JSON 里同样有 host / fromAddress。
// 不按 provider 分流的话，Zeabur 的发件地址会被 SMTP 段那个覆盖掉。
func TestLegacyZeaburPayloadIgnoresCoexistingSMTPFields(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"host": "smtp.example.com",
		"port": 465,
		"fromAddress": "smtp@example.com",
		"zeabur": {
			"apiKeyCipher": "cipher-key",
			"webhookSecretCipher": "cipher-hook",
			"baseUrl": "https://api.example.com/zsend",
			"fromAddress": "zeabur@example.com",
			"fromName": "Aegis",
			"tags": {"env": "prod"}
		}
	}`)

	item := hydrate(t, emaildomain.ProviderZeabur, raw)

	if item.Setting(emaildomain.KeyFromAddress) != "zeabur@example.com" {
		t.Fatalf("Zeabur 配置的发件地址被 SMTP 段覆盖了：%q", item.Setting(emaildomain.KeyFromAddress))
	}
	if item.Setting("host") != "" {
		t.Fatalf("Zeabur 配置不该带上 SMTP 段的字段：%+v", item.Settings)
	}
	if item.Setting("baseUrl") != "https://api.example.com/zsend" {
		t.Fatalf("自定义 API 地址未解出：%q", item.Setting("baseUrl"))
	}
	if item.SecretsCipher["apiKey"] != "cipher-key" ||
		item.SecretsCipher[emaildomain.KeyWebhookSecret] != "cipher-hook" {
		t.Fatalf("密文未按新键名解出：%+v", item.SecretsCipher)
	}
	if tags := item.SettingMap(emaildomain.KeyTags); tags["env"] != "prod" {
		t.Fatalf("标签未解出：%v", tags)
	}
}

// 新形态优先：一旦写过一次（options/secrets 存在），就不再回落去解存量字段。
// 两套形态同时生效会让「清空一个字段」变成「回落到旧值」。
func TestGenericPayloadTakesPrecedenceOverLegacyFields(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"host": "legacy.example.com",
		"options": {"host": "current.example.com"},
		"secrets": {"password": "cipher"}
	}`)
	item := hydrate(t, emaildomain.ProviderSMTP, raw)

	if item.Setting("host") != "current.example.com" {
		t.Fatalf("应优先采用新形态，实际: %q", item.Setting("host"))
	}
	if item.SecretsCipher["password"] != "cipher" {
		t.Fatalf("密文未解出：%+v", item.SecretsCipher)
	}
}

// 写入只产出新形态，且**不落任何明文密钥**。
func TestMarshalWritesGenericShapeWithoutPlaintextSecrets(t *testing.T) {
	t.Parallel()

	encoded, err := marshalEmailConfigPayload(emaildomain.Config{
		Provider:      emaildomain.ProviderSES,
		Settings:      map[string]string{"region": "us-east-1", "empty": ""},
		Secrets:       map[string]string{"secretAccessKey": "PLAINTEXT-MUST-NOT-LEAK"},
		SecretsCipher: map[string]string{"secretAccessKey": "cipher"},
	})
	if err != nil {
		t.Fatalf("序列化失败：%v", err)
	}
	if got := string(encoded); contains(got, "PLAINTEXT-MUST-NOT-LEAK") {
		t.Fatalf("明文密钥进了数据库载荷：%s", got)
	}

	var payload emailConfigPayload
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatalf("反序列化失败：%v", err)
	}
	if payload.Options["region"] != "us-east-1" {
		t.Fatalf("非密钥字段未落库：%+v", payload.Options)
	}
	// 空值键不落库：留着它们只会让 config 这一列随服务商切换不断堆积空字段。
	if _, exists := payload.Options["empty"]; exists {
		t.Fatal("空值键不该落库")
	}
	if payload.Secrets["secretAccessKey"] != "cipher" {
		t.Fatalf("密文未落库：%+v", payload.Secrets)
	}
	if payload.LegacyHost != "" || payload.LegacyZeabur != nil {
		t.Fatal("写入不该再产出存量形态")
	}
}

// 作用域过滤必须分成 IS NULL 与等值两条。
// 写成 COALESCE(appid,0)=$1 会让 appid 上的所有索引失效，而投递记录表会长到几百万行。
func TestEmailScopeConditionUsesIndexFriendlyPredicates(t *testing.T) {
	t.Parallel()

	args := make([]any, 0, 1)
	if got := emailScopeCondition(emaildomain.PlatformAppID, &args); got != "appid IS NULL" {
		t.Fatalf("平台级应走 IS NULL，实际: %q", got)
	}
	if len(args) != 0 {
		t.Fatalf("平台级不该占位参数，实际: %v", args)
	}

	args = append(args, "existing")
	if got := emailScopeCondition(42, &args); got != "appid = $2" {
		t.Fatalf("应用级应走等值并接在已有参数之后，实际: %q", got)
	}
	if len(args) != 2 || args[1] != int64(42) {
		t.Fatalf("参数未正确追加：%v", args)
	}
}

// 0 ↔ NULL 的映射只在仓储层出现，上层看到的永远是一个 int64。
func TestNullableAppIDMapsPlatformToNull(t *testing.T) {
	t.Parallel()

	if nullableAppID(emaildomain.PlatformAppID) != nil {
		t.Fatal("平台级应落成 NULL")
	}
	if nullableAppID(7) != int64(7) {
		t.Fatal("应用级应原样落库")
	}
}

func hydrate(t *testing.T, provider string, raw []byte) emaildomain.Config {
	t.Helper()
	var payload emailConfigPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("反序列化存量载荷失败：%v", err)
	}
	item := emaildomain.Config{
		Provider:      provider,
		Settings:      map[string]string{},
		Secrets:       map[string]string{},
		SecretsCipher: map[string]string{},
	}
	if len(payload.Options) > 0 || len(payload.Secrets) > 0 {
		for key, value := range payload.Options {
			item.Settings[key] = value
		}
		for key, value := range payload.Secrets {
			item.SecretsCipher[key] = value
		}
		return item
	}
	hydrateLegacyEmailPayload(&item, payload)
	return item
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
