package service

import (
	"testing"
	"time"

	"aegis/internal/authz"
	"aegis/internal/config"
	admindomain "aegis/internal/domain/admin"
	pgrepo "aegis/internal/repository/postgres"
	redisrepo "aegis/internal/repository/redis"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-webauthn/webauthn/protocol"
	redislib "github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// 控制台登录页的通行密钥登录：真实 Postgres（全部迁移）+ miniredis + 软件认证器。
// 钉住：选中通行密钥即登录且不再要 TOTP、要求用户验证、挑战只能用一次、
// 来源与 user handle 对不上被拒、停用账号被拒、应用用户的挑战不能拿来用。
//
// AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55471/postgres go test ./internal/service -run TestAdminPasskeyLoginIntegration
func TestAdminPasskeyLoginIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	mr := miniredis.RunT(t)
	rdb := redislib.NewClient(&redislib.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	const rpID = "console.example.com"
	const origin = "https://console.example.com"
	cfg := config.Config{AdminSessionTTL: time.Hour}
	cfg.JWT.Secret = "admin-passkey-test-secret-admin-passkey"
	cfg.JWT.Issuer = "aegis-test"
	cfg.Security.MasterKey = "admin-passkey-test-master-key"
	cfg.Security.Modules = config.SecurityModulesConfig{TOTPEnabled: true, RecoveryCodesEnabled: true, PasskeyEnabled: true}
	cfg.Security.Passkey = config.PasskeyConfig{
		Enabled: true, RPDisplayName: "Aegis", RPID: rpID, RPOrigins: []string{origin},
		ChallengeTTL: 5 * time.Minute, UserVerification: "preferred",
	}

	pg := pgrepo.New(pool)
	sessions := redisrepo.NewSessionRepository(rdb, "t")
	security := NewSecurityService(cfg, zap.NewNop(), pg, sessions, nil)
	engine, err := authz.New(ctx, zap.NewNop(), nil, authz.BuiltinPolicies())
	if err != nil {
		t.Fatal(err)
	}
	admins, err := NewAdminService(cfg, zap.NewNop(), pg, sessions, engine)
	if err != nil {
		t.Fatal(err)
	}
	admins.SetSecurityService(security)

	var adminID int64
	if err := pool.QueryRow(ctx, `INSERT INTO admin_accounts (account, password_hash, display_name, is_super_admin)
VALUES ('pk-admin', 'x', '通行密钥管理员', TRUE) RETURNING id`).Scan(&adminID); err != nil {
		t.Fatal(err)
	}
	// 开着 TOTP：通行密钥登录不应再要第二步
	if _, err := pool.Exec(ctx, `INSERT INTO admin_totp_secrets (admin_id, secret_ciphertext, issuer, account_name, enabled, enabled_at)
VALUES ($1, 'x', 'Aegis', 'pk-admin', TRUE, NOW())`, adminID); err != nil {
		t.Fatal(err)
	}

	octx := ContextWithRequestOrigin(ctx, origin)
	access := &admindomain.AccessContext{Session: admindomain.Session{AdminID: adminID, Account: "pk-admin"}}
	device := newSoftAuthenticator(t)
	regSession, creation, err := security.BeginAdminPasskeyRegistration(octx, access)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := security.FinishAdminPasskeyRegistration(octx, access, regSession.ChallengeID,
		device.register(t, rpID, creation.Response.Challenge.String(), origin), "测试密钥"); err != nil {
		t.Fatalf("注册应当通过：%v", err)
	}

	handle := adminPasskeyUserHandle(adminID)
	begin := func() (string, string) {
		t.Helper()
		session, assertion, err := admins.BeginPasskeyLogin(octx, "203.0.113.7", "test-agent")
		if err != nil {
			t.Fatal(err)
		}
		if assertion.Response.UserVerification != protocol.VerificationRequired {
			t.Fatalf("登录必须要求用户验证，得到 %q", assertion.Response.UserVerification)
		}
		if len(assertion.Response.AllowedCredentials) != 0 {
			t.Fatal("可发现凭据登录不应列出允许的凭据（会泄露账号拥有哪些凭据）")
		}
		return session.ChallengeID, assertion.Response.Challenge.String()
	}
	login := func(challengeID string, body []byte) (*admindomain.LoginResult, error) {
		return admins.LoginWithPasskey(octx, challengeID, body, "203.0.113.7", "test-agent")
	}

	// 选中通行密钥即登录，开着 TOTP 也不再要第二步
	challengeID, challenge := begin()
	body := device.assert(t, rpID, challenge, origin, handle)
	result, err := login(challengeID, body)
	if err != nil {
		t.Fatalf("通行密钥登录应当通过：%v", err)
	}
	if result.RequiresSecondFactor || result.AccessToken == "" || result.Admin.ID != adminID {
		t.Fatalf("应直接签发会话，得到 %+v", result)
	}
	if _, err := admins.ValidateAccessToken(ctx, result.AccessToken); err != nil {
		t.Fatalf("签发的令牌应当可用：%v", err)
	}
	var lastUsed *time.Time
	var signCount int64
	if err := pool.QueryRow(ctx, `SELECT last_used_at, sign_count FROM admin_passkeys WHERE admin_id = $1`, adminID).
		Scan(&lastUsed, &signCount); err != nil {
		t.Fatal(err)
	}
	if lastUsed == nil || signCount != int64(device.signCount) {
		t.Fatalf("应回写最近使用时间与签名计数，得到 %v / %d", lastUsed, signCount)
	}

	// 同一份断言再交一次：挑战已被取走
	if _, err := login(challengeID, body); errCode(err) != 40057 {
		t.Fatalf("挑战只能用一次，得到 %v", err)
	}

	// 来源不对（钓鱼站点转发的断言）
	challengeID, challenge = begin()
	if _, err := login(challengeID, device.assert(t, rpID, challenge, "https://evil.example.com", handle)); errCode(err) != 40107 {
		t.Fatalf("来源不符应被拒，得到 %v", err)
	}

	// 没有用户验证（只按了一下、没验指纹 / PIN）
	challengeID, challenge = begin()
	if _, err := login(challengeID, device.assertWithFlags(t, rpID, challenge, origin, handle, 0x01)); errCode(err) != 40107 {
		t.Fatalf("没有用户验证应被拒，得到 %v", err)
	}

	// user handle 指向别的管理员
	challengeID, challenge = begin()
	if _, err := login(challengeID, device.assert(t, rpID, challenge, origin, adminPasskeyUserHandle(adminID+999))); errCode(err) != 40107 {
		t.Fatalf("user handle 对不上应被拒，得到 %v", err)
	}

	// 没登记过的凭据（别的设备、或应用用户的通行密钥）
	stranger := newSoftAuthenticator(t)
	challengeID, challenge = begin()
	if _, err := login(challengeID, stranger.assert(t, rpID, challenge, origin, handle)); errCode(err) != 40107 {
		t.Fatalf("未登记的凭据应被拒，得到 %v", err)
	}

	// 不是本流程签发的挑战（比如应用用户的通行密钥登录挑战）
	if _, err := login("passkey_login_0123456789abcdef", body); errCode(err) != 40057 {
		t.Fatalf("非管理员挑战应被拒，得到 %v", err)
	}

	// 停用的账号：凭据是对的，也不签发会话；失败结果带上认出的管理员供审计
	if _, err := pool.Exec(ctx, `UPDATE admin_accounts SET status = 'disabled' WHERE id = $1`, adminID); err != nil {
		t.Fatal(err)
	}
	challengeID, challenge = begin()
	result, err = login(challengeID, device.assert(t, rpID, challenge, origin, handle))
	if errCode(err) != 40310 || result == nil || result.Admin.ID != adminID {
		t.Fatalf("停用账号应被拒并带上管理员，得到 %+v / %v", result, err)
	}
}
