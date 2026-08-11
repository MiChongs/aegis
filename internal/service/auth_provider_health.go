package service

import (
	"aegis/pkg/egress"
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

// ProviderStatus 单个第三方认证源的健康快照（LDAP / OIDC / SAML）
type ProviderStatus struct {
	Source    string    `json:"source"`              // ldap / oidc / saml
	Enabled   bool      `json:"enabled"`             // 后台是否启用该认证源
	Available bool      `json:"available"`           // enabled && 探测可达
	Reason    string    `json:"reason,omitempty"`    // 不可用的中文原因
	LatencyMs int64     `json:"latencyMs,omitempty"` // 探测耗时
	CheckedAt time.Time `json:"checkedAt"`
}

// AuthProviderHealthSnapshot 三个第三方认证源的统一快照
type AuthProviderHealthSnapshot struct {
	LDAP ProviderStatus `json:"ldap"`
	OIDC ProviderStatus `json:"oidc"`
	SAML ProviderStatus `json:"saml"`
}

// ForSource 按 auth_source 字段（password / ldap / oidc / saml）取状态；
// password 和未知值统一返回 Available=true 的占位（本地账号不做外呼探测）。
func (s AuthProviderHealthSnapshot) ForSource(source string) ProviderStatus {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "ldap":
		return s.LDAP
	case "oidc":
		return s.OIDC
	case "saml":
		return s.SAML
	default:
		return ProviderStatus{Source: source, Enabled: true, Available: true}
	}
}

const (
	// 后台探测间隔：30 秒。平台设置改动（LDAP/OIDC/SAML 启停）最长 30s 生效。
	authProviderProbeInterval = 30 * time.Second

	// 单次探测超时：控制在 3 秒内，避免后台协程长时间占用。
	authProviderProbeTimeout = 3 * time.Second
)

// AuthProviderHealthService 统一探测 LDAP/OIDC/SAML 可达性。
//
// 工作模式：**后台常驻协程 + 纯读快照**
//  1. 构造时立即启动一个常驻 goroutine，每 30s 执行一次三路并行探测；
//  2. `Snapshot()` 只做一次 RLock 读内存缓存，**不阻塞请求**，100% 内存操作；
//  3. 启动时同步跑一次初始探测，避免首次 Snapshot() 返回全空；
//  4. 全进程仅一个实例共享，N 个管理员的列表查询不会产生额外 probe；
//  5. `Close()` 触发 ctx 取消，goroutine 优雅退出。
type AuthProviderHealthService struct {
	log  *zap.Logger
	ldap *LDAPService
	oidc *OIDCService
	saml *SAMLService

	mu    sync.RWMutex
	cache AuthProviderHealthSnapshot

	httpClient *http.Client

	// 生命周期控制：cancel 触发后台 goroutine 退出
	cancel   context.CancelFunc
	stopOnce sync.Once
	stopped  atomic.Bool
}

// NewAuthProviderHealthService 构造健康探测服务并立即启动后台探测循环。
//
//	服务对象本身是单例，会在构造时：
//	  1) 同步执行一次初始探测（确保首次 Snapshot 不为空）；
//	  2) 启动一个 goroutine 做 30s 周期性后台探测。
//	进程退出时 bootstrap 调用 `Close()` 停止 goroutine。
func NewAuthProviderHealthService(log *zap.Logger, ldap *LDAPService, oidc *OIDCService, saml *SAMLService) *AuthProviderHealthService {
	if log == nil {
		log = zap.NewNop()
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &AuthProviderHealthService{
		log:        log,
		ldap:       ldap,
		oidc:       oidc,
		saml:       saml,
		cancel:     cancel,
		httpClient: egress.NewClient(egress.Profile{Name: "auth.provider_health", Timeout: authProviderProbeTimeout}),
	}
	// 启动时同步执行一次，避免首次 Snapshot() 读到空
	s.refresh(ctx)
	// 启动后台 30s 定时循环
	go s.run(ctx)
	return s
}

// Snapshot 纯读最新缓存，**不做任何探测**、不阻塞请求、不持长锁。
// 后台 goroutine 每 30s 刷新一次；请求路径调用此方法只读内存。
func (s *AuthProviderHealthService) Snapshot() AuthProviderHealthSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cache
}

// Refresh 立即触发一次同步探测（使用带超时的 ctx）。
//
//	典型调用时机：
//	  1) bootstrap 执行完 `PlatformSettingsService.Initialize` 之后 —— 构造时
//	     LDAP/OIDC/SAML 配置还没从 DB 加载，初始探测拿不到真实配置；
//	     Initialize 完成后显式 Refresh，让首次请求就能看到真实健康状态。
//	  2) 管理员在平台设置改动 LDAP/OIDC/SAML 后，避免等到下一个 30s tick 才生效。
//	若并发调用，refresh 本身是幂等的：三路并行探测 + 单次原子写入缓存。
func (s *AuthProviderHealthService) Refresh(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	s.refresh(ctx)
}

// Close 停止后台探测循环（幂等）
func (s *AuthProviderHealthService) Close() {
	s.stopOnce.Do(func() {
		s.stopped.Store(true)
		if s.cancel != nil {
			s.cancel()
		}
	})
}

// run 后台常驻循环：每 30s 触发一次 refresh
func (s *AuthProviderHealthService) run(ctx context.Context) {
	ticker := time.NewTicker(authProviderProbeInterval)
	defer ticker.Stop()

	s.log.Info("auth provider health probe started", zap.Duration("interval", authProviderProbeInterval))
	for {
		select {
		case <-ctx.Done():
			s.log.Info("auth provider health probe stopped")
			return
		case <-ticker.C:
			// 每次探测使用独立 ctx，最长受 probeTimeout × 2 保护
			refreshCtx, cancel := context.WithTimeout(context.Background(), authProviderProbeTimeout*2)
			s.refresh(refreshCtx)
			cancel()
		}
	}
}

// refresh 并发探测三路并原子写入缓存
func (s *AuthProviderHealthService) refresh(ctx context.Context) {
	var (
		wg                                 sync.WaitGroup
		ldapStatus, oidcStatus, samlStatus ProviderStatus
	)
	wg.Add(3)
	go func() { defer wg.Done(); ldapStatus = s.probeLDAP(ctx) }()
	go func() { defer wg.Done(); oidcStatus = s.probeOIDC(ctx) }()
	go func() { defer wg.Done(); samlStatus = s.probeSAML(ctx) }()
	wg.Wait()

	s.mu.Lock()
	s.cache = AuthProviderHealthSnapshot{
		LDAP: ldapStatus,
		OIDC: oidcStatus,
		SAML: samlStatus,
	}
	s.mu.Unlock()
}

// probeLDAP TCP / TLS 握手探测 LDAP 服务器；不做 bind（避免无谓的认证尝试）
func (s *AuthProviderHealthService) probeLDAP(ctx context.Context) ProviderStatus {
	st := ProviderStatus{Source: "ldap", CheckedAt: time.Now()}
	if s.ldap == nil {
		st.Reason = "LDAP 服务未初始化"
		return st
	}
	cfg := s.ldap.CurrentConfig()
	st.Enabled = cfg.Enabled
	if !cfg.Enabled {
		st.Reason = "LDAP 认证已在平台设置中关闭"
		return st
	}
	if strings.TrimSpace(cfg.Server) == "" || cfg.Port == 0 {
		st.Reason = "LDAP 服务器地址或端口未配置"
		return st
	}

	timeout := authProviderProbeTimeout
	if cfg.ConnectionTimeoutSeconds > 0 {
		if t := time.Duration(cfg.ConnectionTimeoutSeconds) * time.Second; t < timeout {
			timeout = t
		}
	}

	addr := net.JoinHostPort(cfg.Server, strconv.Itoa(cfg.Port))
	start := time.Now()
	dialer := &net.Dialer{Timeout: timeout}

	var (
		conn net.Conn
		err  error
	)
	if cfg.UseTLS {
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{
			InsecureSkipVerify: cfg.SkipTLSVerify, //nolint:gosec // 配置项显式授权跳过 TLS 校验
			ServerName:         cfg.Server,
		})
	} else {
		dialCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		conn, err = dialer.DialContext(dialCtx, "tcp", addr)
	}
	st.LatencyMs = time.Since(start).Milliseconds()
	if err != nil {
		st.Reason = "LDAP 服务器无法连接：" + shortProbeErr(err)
		return st
	}
	_ = conn.Close()
	st.Available = true
	return st
}

// probeOIDC 拉取 issuer 的 /.well-known/openid-configuration 验证可达
func (s *AuthProviderHealthService) probeOIDC(ctx context.Context) ProviderStatus {
	st := ProviderStatus{Source: "oidc", CheckedAt: time.Now()}
	if s.oidc == nil {
		st.Reason = "OIDC 服务未初始化"
		return st
	}
	cfg := s.oidc.CurrentConfig()
	st.Enabled = cfg.Enabled
	if !cfg.Enabled {
		st.Reason = "OIDC 认证已在平台设置中关闭"
		return st
	}
	issuer := strings.TrimRight(strings.TrimSpace(cfg.IssuerURL), "/")
	if issuer == "" {
		st.Reason = "OIDC Issuer URL 未配置"
		return st
	}

	url := issuer + "/.well-known/openid-configuration"
	reqCtx, cancel := context.WithTimeout(ctx, authProviderProbeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		st.Reason = "OIDC 请求构建失败"
		return st
	}

	start := time.Now()
	resp, err := s.httpClient.Do(req)
	st.LatencyMs = time.Since(start).Milliseconds()
	if err != nil {
		st.Reason = "OIDC 发现文档拉取失败：" + shortProbeErr(err)
		return st
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		st.Reason = fmt.Sprintf("OIDC 发现文档返回 HTTP %d", resp.StatusCode)
		return st
	}
	st.Available = true
	return st
}

// probeSAML 探测 metadata URL 可达性。
// 若未配置 MetadataURL（走静态元数据），则不外呼，认为 enabled 即可用。
func (s *AuthProviderHealthService) probeSAML(ctx context.Context) ProviderStatus {
	st := ProviderStatus{Source: "saml", CheckedAt: time.Now()}
	if s.saml == nil {
		st.Reason = "SAML 服务未初始化"
		return st
	}
	cfg := s.saml.CurrentConfig()
	st.Enabled = cfg.Enabled
	if !cfg.Enabled {
		st.Reason = "SAML 认证已在平台设置中关闭"
		return st
	}
	metaURL := strings.TrimSpace(cfg.MetadataURL)
	if metaURL == "" {
		// 静态元数据模式：只要启用就认为可用
		st.Available = true
		return st
	}

	reqCtx, cancel := context.WithTimeout(ctx, authProviderProbeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, metaURL, nil)
	if err != nil {
		st.Reason = "SAML metadata 请求构建失败"
		return st
	}

	start := time.Now()
	resp, err := s.httpClient.Do(req)
	st.LatencyMs = time.Since(start).Milliseconds()
	if err != nil {
		st.Reason = "SAML metadata 拉取失败：" + shortProbeErr(err)
		return st
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		st.Reason = fmt.Sprintf("SAML metadata 返回 HTTP %d", resp.StatusCode)
		return st
	}
	st.Available = true
	return st
}

// shortProbeErr 截断超长错误信息（例如 TLS handshake 错误会携带整条证书链）
func shortProbeErr(err error) string {
	s := err.Error()
	if len(s) > 160 {
		return s[:160] + "…"
	}
	return s
}
