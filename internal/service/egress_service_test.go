package service

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"encoding/pem"
	"strings"
	"testing"

	systemdomain "aegis/internal/domain/system"
	"aegis/pkg/egress"
	"golang.org/x/crypto/ssh"
)

func newTestEgressService(t *testing.T, cfg egress.Config) *EgressService {
	t.Helper()
	gw, err := egress.New(cfg, nil)
	if err != nil {
		t.Fatalf("构造网关失败: %v", err)
	}
	t.Cleanup(gw.Close)
	return NewEgressService(nil, nil, gw, "test-master-key", cfg)
}

// testSSHPrivateKey 生成一把真实可解析的 ed25519 私钥。
// 端点构造会真的去解析私钥（这正是 ValidateConfig 拦坏配置的地方），
// 所以测试里不能用占位字符串。
func testSSHPrivateKey(t *testing.T) string {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(priv, "aegis-test")
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(block))
}

func secretEndpointConfig(t *testing.T) egress.Config {
	t.Helper()
	return egress.Config{
		Enabled:       true,
		DefaultAction: egress.ActionDirect,
		Endpoints: []egress.EndpointConfig{
			{
				Name: "hk", Protocol: egress.ProtocolSOCKS5H, Address: "10.0.0.1:1080",
				Username: "alice", Password: "s3cret",
			},
			{
				Name: "jump", Protocol: egress.ProtocolSSH, Address: "bastion:22",
				SSH: egress.SSHConfig{User: "ops", PrivateKeyPEM: testSSHPrivateKey(t)},
			},
		},
	}
}

func TestEgressStoredSecretsAreEncrypted(t *testing.T) {
	svc := newTestEgressService(t, egress.Config{})
	cfg := secretEndpointConfig(t).Normalize()

	payload, err := svc.encodeStored(cfg)
	if err != nil {
		t.Fatalf("编码失败: %v", err)
	}
	raw := string(payload)
	// 落库的 JSON 里不能出现任何明文密钥。
	for _, secret := range []string{"s3cret", "OPENSSH PRIVATE KEY"} {
		if strings.Contains(raw, secret) {
			t.Fatalf("密钥 %q 以明文落库", secret)
		}
	}
	// 非密钥字段应保持明文，便于运维直接查库排障。
	if !strings.Contains(raw, "alice") || !strings.Contains(raw, "10.0.0.1:1080") {
		t.Fatal("用户名 / 地址不应被加密")
	}

	decoded, err := svc.decodeStored(payload)
	if err != nil {
		t.Fatalf("解码失败: %v", err)
	}
	if decoded.Endpoints[0].Password != "s3cret" {
		t.Errorf("口令未还原: %q", decoded.Endpoints[0].Password)
	}
	if !strings.Contains(decoded.Endpoints[1].SSH.PrivateKeyPEM, "OPENSSH PRIVATE KEY") {
		t.Errorf("SSH 私钥未还原: %q", decoded.Endpoints[1].SSH.PrivateKeyPEM)
	}
}

func TestEgressDecodeToleratesLegacyPlaintext(t *testing.T) {
	// 升级期数据库里可能残留明文，解密失败要按原值处理而不是把配置整份丢掉。
	svc := newTestEgressService(t, egress.Config{})
	payload, err := json.Marshal(secretEndpointConfig(t).Normalize())
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := svc.decodeStored(payload)
	if err != nil {
		t.Fatalf("解码失败: %v", err)
	}
	if decoded.Endpoints[0].Password != "s3cret" {
		t.Fatalf("明文口令被破坏: %q", decoded.Endpoints[0].Password)
	}
}

func TestEgressViewMasksSecrets(t *testing.T) {
	svc := newTestEgressService(t, secretEndpointConfig(t))
	view := svc.view()

	byName := map[string]systemdomain.EgressEndpointView{}
	for _, item := range view.Endpoints {
		byName[item.Name] = item
	}
	hk := byName["hk"]
	if hk.Password != "" || hk.EndpointConfig.Password != "" {
		t.Fatal("口令不应出现在视图里")
	}
	if !hk.PasswordSet {
		t.Fatal("已配置口令应回传 passwordSet=true")
	}
	if hk.Username != "alice" {
		t.Errorf("用户名不该被抹掉: %q", hk.Username)
	}
	jump := byName["jump"]
	if jump.SSH.PrivateKeyPEM != "" || !jump.PrivateKeySet {
		t.Fatalf("SSH 私钥应被脱敏并标记已配置: %+v", jump.SSH)
	}

	// 序列化后同样不能带出密钥（内嵌字段被同名外层字段遮蔽）。
	encoded, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "s3cret") || strings.Contains(string(encoded), "OPENSSH PRIVATE KEY") {
		t.Fatal("序列化后的视图仍包含明文密钥")
	}
}

func TestEgressUpdateInheritsOmittedSecrets(t *testing.T) {
	svc := newTestEgressService(t, secretEndpointConfig(t))

	// 模拟前端把脱敏后的视图原样回传：密钥字段是空的。
	update := systemdomain.EgressSettingsUpdate{
		Enabled:       true,
		DefaultAction: string(egress.ActionDirect),
		Endpoints: []systemdomain.EgressEndpointUpdate{
			{EndpointConfig: egress.EndpointConfig{
				Name: "hk", Protocol: egress.ProtocolSOCKS5H, Address: "10.0.0.1:1080",
				Username: "alice", Remark: "改了备注",
			}},
			{EndpointConfig: egress.EndpointConfig{
				Name: "jump", Protocol: egress.ProtocolSSH, Address: "bastion:22",
				SSH: egress.SSHConfig{User: "ops"},
			}},
		},
	}
	next, err := svc.materialize(update)
	if err != nil {
		t.Fatalf("合并失败: %v", err)
	}
	if next.Endpoints[0].Password != "s3cret" {
		t.Errorf("留空的口令应继承原值，实际 %q", next.Endpoints[0].Password)
	}
	if !strings.Contains(next.Endpoints[1].SSH.PrivateKeyPEM, "OPENSSH PRIVATE KEY") {
		t.Error("留空的 SSH 私钥应继承原值")
	}
	if next.Endpoints[0].Remark != "改了备注" {
		t.Error("非密钥字段应按提交值更新")
	}

	// clearSecrets 才是真正清空的唯一途径。
	update.Endpoints[0].ClearSecrets = true
	cleared, err := svc.materialize(update)
	if err != nil {
		t.Fatalf("合并失败: %v", err)
	}
	if cleared.Endpoints[0].Password != "" {
		t.Errorf("clearSecrets 应清空口令，实际 %q", cleared.Endpoints[0].Password)
	}
}

func TestEgressApplyEnvConfigRespectsDatabaseOverride(t *testing.T) {
	svc := newTestEgressService(t, egress.Config{Enabled: false})

	// 没有数据库覆盖时，.env 热重载应当生效。
	svc.ApplyEnvConfig(egress.Config{Enabled: true, DefaultAction: egress.ActionDirect})
	if !svc.CurrentConfig().Enabled {
		t.Fatal(".env 热重载未生效")
	}

	// 一旦控制台改过配置，再保存 .env 不应该把它冲掉。
	svc.mu.Lock()
	svc.dbOverride = true
	svc.mu.Unlock()
	if err := svc.Reload(egress.Config{Enabled: true, DefaultAction: egress.ActionReject}); err != nil {
		t.Fatal(err)
	}
	svc.ApplyEnvConfig(egress.Config{Enabled: false})
	current := svc.CurrentConfig()
	if !current.Enabled || current.DefaultAction != egress.ActionReject {
		t.Fatalf("数据库覆盖被 .env 冲掉了: %+v", current)
	}
}
