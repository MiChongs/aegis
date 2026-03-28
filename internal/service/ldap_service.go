package service

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	systemdomain "aegis/internal/domain/system"
	apperrors "aegis/pkg/errors"

	ldapv3 "github.com/go-ldap/ldap/v3"
	"go.uber.org/zap"
)

// LDAPService 管理员 LDAP 认证服务（热重载配置）
type LDAPService struct {
	log           *zap.Logger
	mu            sync.RWMutex
	config        systemdomain.LDAPConfig
	encryptionKey []byte
}

// NewLDAPService 创建 LDAP 服务
func NewLDAPService(log *zap.Logger, masterKey string) *LDAPService {
	return &LDAPService{
		log:           log,
		encryptionKey: securityKeyMaterial(masterKey),
	}
}

// IsEnabled 线程安全检查 LDAP 是否启用
func (s *LDAPService) IsEnabled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.config.Enabled
}

// IsReady 检查 LDAP 是否已启用且具备可执行认证的最小配置。
func (s *LDAPService) IsReady() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return ldapConfigReady(s.config)
}

// CurrentConfig 返回当前配置副本
func (s *LDAPService) CurrentConfig() systemdomain.LDAPConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.config
}

// Reload 热重载 LDAP 配置
func (s *LDAPService) Reload(cfg systemdomain.LDAPConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.config = cfg
	s.log.Info("LDAP 配置已重载",
		zap.Bool("enabled", cfg.Enabled),
		zap.String("server", cfg.Server),
		zap.Int("port", cfg.Port),
	)
	return nil
}

// EncryptBindPassword 加密绑定密码
func (s *LDAPService) EncryptBindPassword(plaintext string) (string, error) {
	return encryptSecret(s.encryptionKey, plaintext)
}

// DecryptBindPassword 解密绑定密码
func (s *LDAPService) DecryptBindPassword(ciphertext string) (string, error) {
	return decryptSecret(s.encryptionKey, ciphertext)
}

// Authenticate 对管理员账号执行 LDAP 绑定验证
// 返回 nil,nil = 用户不存在或密码不匹配（可回退本地验证）
// 返回 nil,AppError = 业务拒绝（如不在管理员组）
func (s *LDAPService) Authenticate(ctx context.Context, account, password string) (*systemdomain.LDAPUser, error) {
	s.mu.RLock()
	cfg := s.config
	s.mu.RUnlock()

	if !ldapConfigReady(cfg) {
		return nil, nil
	}
	if strings.TrimSpace(account) == "" || strings.TrimSpace(password) == "" {
		return nil, nil
	}

	timeout := ldapOperationTimeout(cfg)
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	conn, err := s.dial(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("ldap dial: %w", err)
	}
	defer conn.Close()

	// 解密 BindPassword 并绑定
	bindPwd, err := s.DecryptBindPassword(cfg.BindPassword)
	if err != nil {
		return nil, fmt.Errorf("decrypt bind password: %w", err)
	}
	if err := conn.Bind(cfg.BindDN, bindPwd); err != nil {
		return nil, fmt.Errorf("ldap bind: %w", err)
	}
	s.log.Info("LDAP bind 成功", zap.String("bindDN", cfg.BindDN), zap.String("baseDN", cfg.BaseDN))

	// 搜索用户
	filter := fmt.Sprintf(cfg.UserFilter, ldapv3.EscapeFilter(account))
	sr, err := conn.Search(ldapv3.NewSearchRequest(
		cfg.BaseDN,
		ldapv3.ScopeWholeSubtree,
		ldapv3.NeverDerefAliases,
		1,
		cfg.SearchTimeoutSeconds,
		false,
		filter,
		s.searchAttributes(cfg),
		nil,
	))
	if err != nil {
		return nil, fmt.Errorf("ldap search: %w", err)
	}
	if len(sr.Entries) == 0 {
		return nil, nil
	}
	entry := sr.Entries[0]

	// 用户 DN 绑定验证密码
	if err := conn.Bind(entry.DN, password); err != nil {
		return nil, nil
	}

	// 可选：组成员检查
	if cfg.AdminGroupDN != "" {
		if !s.checkGroupMembership(conn, cfg, entry.DN) {
			return nil, apperrors.New(40392, http.StatusForbidden, "LDAP 用户不在管理员组中")
		}
	}

	return s.mapEntryToUser(cfg, entry), nil
}

// TestConnection 测试 LDAP 连接（4 步）
func (s *LDAPService) TestConnection(ctx context.Context, req systemdomain.LDAPTestRequest) *systemdomain.LDAPTestResult {
	start := time.Now()
	result := &systemdomain.LDAPTestResult{}

	cfg := s.testReqToConfig(req)
	timeout := ldapOperationTimeout(cfg)
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	conn, err := s.dial(ctx, cfg)
	if err != nil {
		result.Error = err.Error()
		result.LatencyMs = time.Since(start).Milliseconds()
		return result
	}
	defer conn.Close()
	result.Connected = true

	// Bind
	if err := conn.Bind(req.BindDN, req.BindPassword); err != nil {
		result.Error = "绑定失败: " + err.Error()
		result.LatencyMs = time.Since(start).Milliseconds()
		return result
	}
	result.BindSuccess = true

	// BaseDN 搜索
	_, err = conn.Search(ldapv3.NewSearchRequest(
		req.BaseDN, ldapv3.ScopeBaseObject, ldapv3.NeverDerefAliases,
		0, 5, false, "(objectClass=*)", nil, nil,
	))
	if err != nil {
		result.Error = "BaseDN 搜索失败: " + err.Error()
		result.LatencyMs = time.Since(start).Milliseconds()
		return result
	}
	result.SearchOK = true

	// 可选：测试搜索用户
	if req.TestAccount != "" && req.UserFilter != "" {
		filter := fmt.Sprintf(req.UserFilter, ldapv3.EscapeFilter(req.TestAccount))
		sr, err := conn.Search(ldapv3.NewSearchRequest(
			req.BaseDN, ldapv3.ScopeWholeSubtree, ldapv3.NeverDerefAliases,
			1, 5, false, filter, nil, nil,
		))
		if err == nil && len(sr.Entries) > 0 {
			result.UserFound = true
			result.UserDN = sr.Entries[0].DN
		}
	}

	result.LatencyMs = time.Since(start).Milliseconds()
	return result
}

// ── 内部方法 ──

func (s *LDAPService) dial(ctx context.Context, cfg systemdomain.LDAPConfig) (*ldapv3.Conn, error) {
	timeout := ldapOperationTimeout(cfg)
	address := fmt.Sprintf("%s:%d", cfg.Server, cfg.Port)
	tlsCfg := &tls.Config{
		InsecureSkipVerify: cfg.SkipTLSVerify,
		ServerName:         cfg.Server,
		MinVersion:         tls.VersionTLS12,
	}
	dialer := &net.Dialer{Timeout: timeout}
	if deadline, ok := ctx.Deadline(); ok {
		dialer.Deadline = deadline
	}

	var conn *ldapv3.Conn
	var err error
	if cfg.UseTLS {
		conn, err = ldapv3.DialURL(
			fmt.Sprintf("ldaps://%s", address),
			ldapv3.DialWithTLSDialer(tlsCfg, dialer),
		)
	} else {
		conn, err = ldapv3.DialURL(
			fmt.Sprintf("ldap://%s", address),
			ldapv3.DialWithDialer(dialer),
		)
	}
	if err != nil {
		return nil, err
	}
	conn.SetTimeout(timeout)

	if cfg.UseStartTLS && !cfg.UseTLS {
		if err := conn.StartTLS(tlsCfg); err != nil {
			conn.Close()
			return nil, fmt.Errorf("starttls: %w", err)
		}
	}
	return conn, nil
}

func (s *LDAPService) searchAttributes(cfg systemdomain.LDAPConfig) []string {
	attrs := []string{"dn"}
	for _, a := range []string{cfg.AttrMapping.Account, cfg.AttrMapping.DisplayName, cfg.AttrMapping.Email, cfg.AttrMapping.Phone} {
		if a != "" {
			attrs = append(attrs, a)
		}
	}
	return attrs
}

func ldapConfigReady(cfg systemdomain.LDAPConfig) bool {
	if !cfg.Enabled {
		return false
	}
	if strings.TrimSpace(cfg.Server) == "" {
		return false
	}
	if cfg.Port <= 0 {
		return false
	}
	if strings.TrimSpace(cfg.BindDN) == "" || strings.TrimSpace(cfg.BindPassword) == "" {
		return false
	}
	if strings.TrimSpace(cfg.BaseDN) == "" || strings.TrimSpace(cfg.UserFilter) == "" {
		return false
	}
	return true
}

func ldapOperationTimeout(cfg systemdomain.LDAPConfig) time.Duration {
	timeout := time.Duration(cfg.ConnectionTimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	if timeout > 3*time.Second {
		timeout = 3 * time.Second
	}
	return timeout
}

func (s *LDAPService) mapEntryToUser(cfg systemdomain.LDAPConfig, entry *ldapv3.Entry) *systemdomain.LDAPUser {
	return &systemdomain.LDAPUser{
		DN:          entry.DN,
		Account:     entry.GetAttributeValue(cfg.AttrMapping.Account),
		DisplayName: entry.GetAttributeValue(cfg.AttrMapping.DisplayName),
		Email:       entry.GetAttributeValue(cfg.AttrMapping.Email),
		Phone:       entry.GetAttributeValue(cfg.AttrMapping.Phone),
	}
}

func (s *LDAPService) checkGroupMembership(conn *ldapv3.Conn, cfg systemdomain.LDAPConfig, userDN string) bool {
	if cfg.GroupBaseDN == "" || cfg.GroupFilter == "" {
		return false
	}
	filter := fmt.Sprintf(cfg.GroupFilter, ldapv3.EscapeFilter(userDN))
	sr, err := conn.Search(ldapv3.NewSearchRequest(
		cfg.GroupBaseDN, ldapv3.ScopeWholeSubtree, ldapv3.NeverDerefAliases,
		0, cfg.SearchTimeoutSeconds, false, filter, []string{"dn"}, nil,
	))
	if err != nil {
		return false
	}
	for _, e := range sr.Entries {
		if strings.EqualFold(e.DN, cfg.AdminGroupDN) {
			return true
		}
	}
	return false
}

func (s *LDAPService) testReqToConfig(req systemdomain.LDAPTestRequest) systemdomain.LDAPConfig {
	port := req.Port
	if port == 0 {
		if req.UseTLS {
			port = 636
		} else {
			port = 389
		}
	}
	timeout := req.ConnectionTimeoutSeconds
	if timeout == 0 {
		timeout = 10
	}
	return systemdomain.LDAPConfig{
		Enabled: true, Server: req.Server, Port: port,
		UseTLS: req.UseTLS, UseStartTLS: req.UseStartTLS, SkipTLSVerify: req.SkipTLSVerify,
		BindDN: req.BindDN, BindPassword: req.BindPassword,
		BaseDN: req.BaseDN, UserFilter: req.UserFilter,
		ConnectionTimeoutSeconds: timeout, SearchTimeoutSeconds: 15,
	}
}
