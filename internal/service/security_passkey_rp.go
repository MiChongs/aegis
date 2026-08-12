package service

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"aegis/internal/config"
	apperrors "aegis/pkg/errors"

	"github.com/go-webauthn/webauthn/protocol"
	webauthnlib "github.com/go-webauthn/webauthn/webauthn"
)

// Passkey 的 RP ID 必须跟着**浏览器地址栏的域名**走
// ─────────────────────────────────────────────────────────────────────────
// WebAuthn 规定 rp.id 只能等于当前页面的有效域名、或它的可注册域名后缀。
// 不满足时浏览器在 navigator.credentials.create() 阶段就抛：
//
//	The relying party ID is not a registrable domain suffix of,
//	nor equal to the current domain.
//
// 旧实现把 RP ID 当成一个启动期就固定下来的静态值（缺省 "localhost"），
// 于是同一套部署换个访问地址就废：
//
//   - 开发时从 http://127.0.0.1:3000 打开控制台 —— "localhost" 不是 "127.0.0.1"
//     的后缀，直接报上面那句。而这恰恰是缺省配置自带的组合：缺省 RP ID 是
//     localhost，缺省允许来源里却写着 127.0.0.1:3000，两者自相矛盾。
//   - 上线后从 https://console.example.com 打开，没人记得改 SECURITY_PASSKEY_RP_ID，
//     同样报错。
//
// 现在改成按请求来源解析：来源域先过一遍**管理员配置的允许来源白名单**
// （安全边界仍然只有这一条），通过之后再决定 RP ID —— 配置了 RP ID 且它是
// 来源域的可注册后缀就沿用（让 a.example.com / b.example.com 共享同一批凭据），
// 否则直接用来源域本身。
//
// Finish 阶段不重新推导，而是用 Begin 时实际下发的那个 RP ID：它已经被
// go-webauthn 写进 SessionData.RelyingPartyID 并随挑战一起落库，
// 两阶段因此不可能各算各的。

type requestOriginContextKey struct{}

// ContextWithRequestOrigin 把「浏览器地址栏的源」放进 context。
// 由 middleware.RequestOrigin 统一写入，service 层只读。
func ContextWithRequestOrigin(ctx context.Context, origin string) context.Context {
	origin = strings.TrimSpace(origin)
	if origin == "" {
		return ctx
	}
	return context.WithValue(ctx, requestOriginContextKey{}, origin)
}

// RequestOriginFromContext 取回请求来源，取不到时返回空串。
func RequestOriginFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	origin, _ := ctx.Value(requestOriginContextKey{}).(string)
	return origin
}

// ResolveRequestOrigin 从 HTTP 请求还原浏览器看到的源。
//
// 优先级 Origin > Referer > Host，理由是可靠性递减：
// 跨源与同源 POST 都会带 Origin；Referer 可能被引荐策略删掉；
// Host 在反向代理之后是网关地址，未必等于用户地址栏里的域名，只能垫底。
func ResolveRequestOrigin(r *http.Request) string {
	if r == nil {
		return ""
	}
	if origin := canonicalOrigin(r.Header.Get("Origin")); origin != "" {
		return origin
	}
	if origin := canonicalOrigin(r.Header.Get("Referer")); origin != "" {
		return origin
	}

	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); forwarded != "" {
		scheme = strings.ToLower(strings.TrimSpace(strings.Split(forwarded, ",")[0]))
	}
	host := strings.TrimSpace(r.Host)
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-Host")); forwarded != "" {
		host = strings.TrimSpace(strings.Split(forwarded, ",")[0])
	}
	if host == "" {
		return ""
	}
	return canonicalOrigin(scheme + "://" + host)
}

// canonicalOrigin 归一化成 scheme://host[:port]，非 http(s) 一律丢弃。
// 沙箱 iframe 会送 "null"，那不是一个可用的源。
func canonicalOrigin(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.EqualFold(raw, "null") {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return ""
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return ""
	}
	return scheme + "://" + strings.ToLower(parsed.Host)
}

// originHost 取源的主机名（不含端口）。RP ID 里不允许出现端口。
func originHost(origin string) string {
	parsed, err := url.Parse(origin)
	if err != nil {
		return ""
	}
	return strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
}

// passkeyOriginAllowed 按 go-webauthn 的语义比对允许来源：
// 带 scheme 的按 scheme+host+port 归一化后比，其余退回字符串全等。
func passkeyOriginAllowed(allowed []string, origin string) bool {
	for _, item := range allowed {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if canonical := canonicalOrigin(item); canonical != "" {
			if canonical == origin {
				return true
			}
			continue
		}
		if strings.EqualFold(item, origin) {
			return true
		}
	}
	return false
}

// resolvePasskeyRPID 决定这次仪式用哪个 RP ID。
//
// 配置值只有在「等于来源域或是它的可注册后缀」时才被采用 —— 这正是浏览器
// 自己的判据，配了个用不了的值不如不配。
func resolvePasskeyRPID(configured, host string) string {
	configured = strings.ToLower(strings.TrimSpace(configured))
	if host == "" {
		return configured
	}
	if configured == "" {
		return host
	}
	if configured == host || strings.HasSuffix(host, "."+configured) {
		return configured
	}
	return host
}

// buildWebAuthn 按给定 RP ID 造一个实例。
//
// 允许来源列表**原样透传**，不掺入本次请求的源：白名单是唯一的安全边界，
// 把当前请求自动加进去等于取消这道检查。
func buildPasskeyWebAuthn(cfg config.PasskeyConfig, rpID string) (*webauthnlib.WebAuthn, error) {
	instance, err := webauthnlib.New(&webauthnlib.Config{
		RPDisplayName: cfg.RPDisplayName,
		RPID:          rpID,
		RPOrigins:     cloneStrings(cfg.RPOrigins),
		RPTopOrigins:  cloneStrings(cfg.RPTopOrigins),
		AuthenticatorSelection: protocol.AuthenticatorSelection{
			UserVerification: userVerificationRequirement(cfg.UserVerification),
		},
	})
	if err != nil {
		return nil, apperrors.New(50325, http.StatusServiceUnavailable, fmt.Sprintf("Passkey 配置无效：%s", err.Error()))
	}
	return instance, nil
}

// webAuthnForRequest 供 Begin 阶段使用：按本次请求来源解析 RP ID。
func (s *SecurityService) webAuthnForRequest(ctx context.Context) (*webauthnlib.WebAuthn, error) {
	if s.currentWebAuthn() == nil {
		return nil, apperrors.New(50322, http.StatusServiceUnavailable, "当前安全模块暂不可用")
	}
	cfg := s.currentConfig().Passkey

	origin := RequestOriginFromContext(ctx)
	if origin == "" {
		// 拿不到来源（非浏览器调用、或反代把 Origin / Referer 都剥了）时退回静态配置，
		// 与旧行为一致。这里不能猜：猜错的 RP ID 会把凭据绑到一个谁都用不上的域上。
		return buildPasskeyWebAuthn(cfg, cfg.RPID)
	}

	if len(cfg.RPOrigins) > 0 && !passkeyOriginAllowed(cfg.RPOrigins, origin) {
		return nil, apperrors.New(40039, http.StatusBadRequest, fmt.Sprintf(
			"当前访问来源 %s 不在 Passkey 允许来源列表内（现为 %s）。请在「平台配置 · 系统安全 · Passkey」把它补进去。",
			origin, strings.Join(cfg.RPOrigins, "、"),
		))
	}

	return buildPasskeyWebAuthn(cfg, resolvePasskeyRPID(cfg.RPID, originHost(origin)))
}

// webAuthnForCeremony 供 Finish 阶段使用：沿用 Begin 时下发的 RP ID。
//
// 不重新按当前请求推导 —— 用户可能在 Begin 之后换了地址栏（比如从 localhost
// 跳到 127.0.0.1），那时应当如实校验失败，而不是拿新域名去凑一个"能过"的结果。
func (s *SecurityService) webAuthnForCeremony(ctx context.Context, rpID string) (*webauthnlib.WebAuthn, error) {
	rpID = strings.TrimSpace(rpID)
	if rpID == "" {
		// 挑战是在本次改动之前签发的（SessionData 里没有 RelyingPartyID），按请求重新解析
		return s.webAuthnForRequest(ctx)
	}
	if s.currentWebAuthn() == nil {
		return nil, apperrors.New(50322, http.StatusServiceUnavailable, "当前安全模块暂不可用")
	}
	return buildPasskeyWebAuthn(s.currentConfig().Passkey, rpID)
}
