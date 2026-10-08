// Package hydra 是 Ory Hydra（v2 / v25 / v26）Admin API 的最小类型化客户端。
//
// Aegis 在 OAuth2 / OIDC 授权服务器这件事上的分工是：
//
//   - Hydra 负责协议本身：授权端点、令牌端点、JWKS、发现文档、令牌签发与吊销；
//   - Aegis 是它的 login / consent / logout provider：认人、问同意、出 claims。
//
// 两边只经由 Admin API 对话，因此这里只覆盖 Aegis 用得到的那部分端点，
// 不引入官方生成的 SDK（几十个包、上万行代码，只为调十几个接口）。
// 字段名与 Hydra 的 JSON 保持一字不差，方便对照官方文档排障。
package hydra

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Client Hydra Admin API 客户端。零值不可用，用 New 构造。
type Client struct {
	adminURL string
	http     *http.Client
}

// New 构造客户端。adminURL 形如 http://hydra:4445（不带 /admin 前缀）。
func New(adminURL string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &Client{adminURL: strings.TrimRight(strings.TrimSpace(adminURL), "/"), http: httpClient}
}

// Error Hydra 返回的非 2xx 响应。
type Error struct {
	StatusCode  int
	Code        string `json:"error"`
	Description string `json:"error_description"`
	Hint        string `json:"error_hint"`
}

func (e *Error) Error() string {
	msg := e.Description
	if msg == "" {
		msg = e.Code
	}
	if e.Hint != "" {
		msg += "（" + e.Hint + "）"
	}
	return fmt.Sprintf("hydra: HTTP %d: %s", e.StatusCode, msg)
}

// IsNotFound 资源不存在（含已被消费、已过期的 challenge）。
func IsNotFound(err error) bool {
	var he *Error
	return errors.As(err, &he) && he.StatusCode == http.StatusNotFound
}

// IsGone challenge 已被处理过（Hydra 对重复提交回 410 + redirect_to）。
func IsGone(err error) bool {
	var he *Error
	return errors.As(err, &he) && he.StatusCode == http.StatusGone
}

// ── OAuth2 客户端 ───────────────────────────────────────────────────

// OAuth2Client Hydra 的客户端模型。只列出 Aegis 读写的字段；
// 未列出的字段在 Update 时由 Hydra 保留默认值，因此更新一律走「读-改-写」。
type OAuth2Client struct {
	ClientID                string         `json:"client_id,omitempty"`
	ClientName              string         `json:"client_name,omitempty"`
	ClientSecret            string         `json:"client_secret,omitempty"`
	RedirectURIs            []string       `json:"redirect_uris"`
	PostLogoutRedirectURIs  []string       `json:"post_logout_redirect_uris"`
	GrantTypes              []string       `json:"grant_types"`
	ResponseTypes           []string       `json:"response_types"`
	Scope                   string         `json:"scope"`
	Audience                []string       `json:"audience"`
	Owner                   string         `json:"owner,omitempty"`
	PolicyURI               string         `json:"policy_uri,omitempty"`
	TosURI                  string         `json:"tos_uri,omitempty"`
	ClientURI               string         `json:"client_uri,omitempty"`
	LogoURI                 string         `json:"logo_uri,omitempty"`
	Contacts                []string       `json:"contacts"`
	AllowedCORSOrigins      []string       `json:"allowed_cors_origins"`
	SubjectType             string         `json:"subject_type,omitempty"`
	TokenEndpointAuthMethod string         `json:"token_endpoint_auth_method,omitempty"`
	FrontchannelLogoutURI   string         `json:"frontchannel_logout_uri,omitempty"`
	BackchannelLogoutURI    string         `json:"backchannel_logout_uri,omitempty"`
	SkipConsent             bool           `json:"skip_consent"`
	SkipLogoutConsent       *bool          `json:"skip_logout_consent,omitempty"`
	AccessTokenStrategy     string         `json:"access_token_strategy,omitempty"`
	Metadata                map[string]any `json:"metadata,omitempty"`
	CreatedAt               time.Time      `json:"created_at,omitzero"`
	UpdatedAt               time.Time      `json:"updated_at,omitzero"`
}

// ListClientsQuery 列表过滤。PageToken 来自上一页的 NextPageToken。
type ListClientsQuery struct {
	Owner      string
	ClientName string
	PageSize   int
	PageToken  string
}

// ClientPage 一页客户端。NextPageToken 为空表示没有下一页。
type ClientPage struct {
	Items         []OAuth2Client
	NextPageToken string
}

func (c *Client) ListClients(ctx context.Context, q ListClientsQuery) (*ClientPage, error) {
	params := url.Values{}
	if q.Owner != "" {
		params.Set("owner", q.Owner)
	}
	if q.ClientName != "" {
		params.Set("client_name", q.ClientName)
	}
	if q.PageSize > 0 {
		params.Set("page_size", fmt.Sprint(q.PageSize))
	}
	if q.PageToken != "" {
		params.Set("page_token", q.PageToken)
	}
	var items []OAuth2Client
	header, err := c.do(ctx, http.MethodGet, "/admin/clients", params, nil, &items)
	if err != nil {
		return nil, err
	}
	return &ClientPage{Items: items, NextPageToken: nextPageToken(header)}, nil
}

func (c *Client) GetClient(ctx context.Context, clientID string) (*OAuth2Client, error) {
	var out OAuth2Client
	if _, err := c.do(ctx, http.MethodGet, "/admin/clients/"+url.PathEscape(clientID), nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateClient 创建客户端。返回值里的 ClientSecret 只有这一次是明文。
func (c *Client) CreateClient(ctx context.Context, in OAuth2Client) (*OAuth2Client, error) {
	var out OAuth2Client
	if _, err := c.do(ctx, http.MethodPost, "/admin/clients", nil, in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SetClient 整体替换客户端。ClientSecret 留空表示保持原密钥，非空即轮换为该值。
func (c *Client) SetClient(ctx context.Context, in OAuth2Client) (*OAuth2Client, error) {
	var out OAuth2Client
	if _, err := c.do(ctx, http.MethodPut, "/admin/clients/"+url.PathEscape(in.ClientID), nil, in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DeleteClient(ctx context.Context, clientID string) error {
	_, err := c.do(ctx, http.MethodDelete, "/admin/clients/"+url.PathEscape(clientID), nil, nil, nil)
	return err
}

// ── 登录 / 同意 / 登出 / 设备授权 ──────────────────────────────────────

// OIDCContext 授权请求携带的 OIDC 参数。
type OIDCContext struct {
	ACRValues         []string       `json:"acr_values,omitempty"`
	Display           string         `json:"display,omitempty"`
	IDTokenHintClaims map[string]any `json:"id_token_hint_claims,omitempty"`
	LoginHint         string         `json:"login_hint,omitempty"`
	UILocales         []string       `json:"ui_locales,omitempty"`
}

// LoginRequest GET /admin/oauth2/auth/requests/login 的响应。
type LoginRequest struct {
	Challenge                    string       `json:"challenge"`
	Client                       OAuth2Client `json:"client"`
	RequestURL                   string       `json:"request_url"`
	RequestedScope               []string     `json:"requested_scope"`
	RequestedAccessTokenAudience []string     `json:"requested_access_token_audience"`
	Skip                         bool         `json:"skip"`
	Subject                      string       `json:"subject"`
	SessionID                    string       `json:"session_id,omitempty"`
	OIDCContext                  OIDCContext  `json:"oidc_context"`
}

// AcceptLogin 认证通过。RememberFor 单位秒；Remember=false 时浏览器关闭即失效。
type AcceptLogin struct {
	Subject     string         `json:"subject"`
	Remember    bool           `json:"remember"`
	RememberFor int64          `json:"remember_for"`
	ACR         string         `json:"acr,omitempty"`
	AMR         []string       `json:"amr,omitempty"`
	Context     map[string]any `json:"context,omitempty"`
	// ExtendSessionLifespan 跳过登录（skip=true）时顺延已有会话
	ExtendSessionLifespan bool `json:"extend_session_lifespan,omitempty"`
}

// RejectRequest 拒绝登录 / 同意 / 设备请求。Error 取 OAuth2 标准错误码。
type RejectRequest struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description,omitempty"`
	ErrorHint        string `json:"error_hint,omitempty"`
	StatusCode       int    `json:"status_code,omitempty"`
}

// Redirect 接受 / 拒绝之后 Hydra 让浏览器去的地方。
type Redirect struct {
	RedirectTo string `json:"redirect_to"`
}

func (c *Client) GetLoginRequest(ctx context.Context, challenge string) (*LoginRequest, error) {
	var out LoginRequest
	if err := c.getFlow(ctx, "login", "login_challenge", challenge, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) AcceptLoginRequest(ctx context.Context, challenge string, body AcceptLogin) (string, error) {
	return c.putFlow(ctx, "login/accept", "login_challenge", challenge, body)
}

func (c *Client) RejectLoginRequest(ctx context.Context, challenge string, body RejectRequest) (string, error) {
	return c.putFlow(ctx, "login/reject", "login_challenge", challenge, body)
}

// ConsentRequest GET /admin/oauth2/auth/requests/consent 的响应。
type ConsentRequest struct {
	Challenge                    string         `json:"challenge"`
	Client                       OAuth2Client   `json:"client"`
	RequestURL                   string         `json:"request_url"`
	RequestedScope               []string       `json:"requested_scope"`
	RequestedAccessTokenAudience []string       `json:"requested_access_token_audience"`
	Skip                         bool           `json:"skip"`
	Subject                      string         `json:"subject"`
	LoginSessionID               string         `json:"login_session_id,omitempty"`
	ACR                          string         `json:"acr,omitempty"`
	AMR                          []string       `json:"amr,omitempty"`
	Context                      map[string]any `json:"context,omitempty"`
	OIDCContext                  OIDCContext    `json:"oidc_context"`
}

// ConsentSession 写进令牌的 claims。AccessToken 进访问令牌（及内省结果），IDToken 进 ID Token 与 userinfo。
type ConsentSession struct {
	AccessToken map[string]any `json:"access_token,omitempty"`
	IDToken     map[string]any `json:"id_token,omitempty"`
}

// AcceptConsent 用户同意授权。
type AcceptConsent struct {
	GrantScope               []string       `json:"grant_scope"`
	GrantAccessTokenAudience []string       `json:"grant_access_token_audience"`
	Remember                 bool           `json:"remember"`
	RememberFor              int64          `json:"remember_for"`
	Session                  ConsentSession `json:"session"`
}

func (c *Client) GetConsentRequest(ctx context.Context, challenge string) (*ConsentRequest, error) {
	var out ConsentRequest
	if err := c.getFlow(ctx, "consent", "consent_challenge", challenge, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) AcceptConsentRequest(ctx context.Context, challenge string, body AcceptConsent) (string, error) {
	return c.putFlow(ctx, "consent/accept", "consent_challenge", challenge, body)
}

func (c *Client) RejectConsentRequest(ctx context.Context, challenge string, body RejectRequest) (string, error) {
	return c.putFlow(ctx, "consent/reject", "consent_challenge", challenge, body)
}

// LogoutRequest GET /admin/oauth2/auth/requests/logout 的响应。
// RPInitiated=true 表示由客户端经 /oauth2/sessions/logout 发起（带 id_token_hint）。
type LogoutRequest struct {
	Challenge   string        `json:"challenge"`
	Subject     string        `json:"subject"`
	SessionID   string        `json:"sid"`
	RequestURL  string        `json:"request_url"`
	RPInitiated bool          `json:"rp_initiated"`
	Client      *OAuth2Client `json:"client,omitempty"`
}

func (c *Client) GetLogoutRequest(ctx context.Context, challenge string) (*LogoutRequest, error) {
	var out LogoutRequest
	if err := c.getFlow(ctx, "logout", "logout_challenge", challenge, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) AcceptLogoutRequest(ctx context.Context, challenge string) (string, error) {
	return c.putFlow(ctx, "logout/accept", "logout_challenge", challenge, nil)
}

// RejectLogoutRequest 用户取消登出。Hydra 对此回 204、不给跳转地址。
func (c *Client) RejectLogoutRequest(ctx context.Context, challenge string) error {
	_, err := c.putFlow(ctx, "logout/reject", "logout_challenge", challenge, nil)
	return err
}

// AcceptDeviceRequest 设备授权（RFC 8628）：用户在验证页输入的用户码交给 Hydra 核对，
// 通过后 Hydra 让浏览器继续走登录与同意。Hydra 没有读取设备请求与拒绝设备请求的接口，
// 用户码错误时由这里的错误返回告诉用户重新输入。
func (c *Client) AcceptDeviceRequest(ctx context.Context, challenge, userCode string) (string, error) {
	return c.putFlow(ctx, "device/accept", "device_challenge", challenge, map[string]string{"user_code": userCode})
}

// ── 会话与授权记录 ──────────────────────────────────────────────────

// PreviousConsentSession 用户对某个客户端的一条已记住的授权。
type PreviousConsentSession struct {
	ConsentRequestID string         `json:"consent_request_id"`
	ConsentRequest   ConsentRequest `json:"consent_request"`
	GrantScope       []string       `json:"grant_scope"`
	Remember         bool           `json:"remember"`
	RememberFor      int64          `json:"remember_for"`
	HandledAt        time.Time      `json:"handled_at"`
	ExpiresAt        map[string]any `json:"expires_at,omitempty"`
}

// ListConsentSessions 列出某个主体的全部已记住授权（自动翻页）。
func (c *Client) ListConsentSessions(ctx context.Context, subject string) ([]PreviousConsentSession, error) {
	var all []PreviousConsentSession
	token := ""
	for range 50 {
		params := url.Values{"subject": {subject}, "page_size": {"250"}}
		if token != "" {
			params.Set("page_token", token)
		}
		var page []PreviousConsentSession
		header, err := c.do(ctx, http.MethodGet, "/admin/oauth2/auth/sessions/consent", params, nil, &page)
		if err != nil {
			return nil, err
		}
		all = append(all, page...)
		token = nextPageToken(header)
		if token == "" || len(page) == 0 {
			break
		}
	}
	return all, nil
}

// RevokeConsentSessions 撤销授权。clientID 为空时撤销该主体对全部客户端的授权；
// 撤销会一并吊销由这些授权签发的访问令牌与刷新令牌。
func (c *Client) RevokeConsentSessions(ctx context.Context, subject, clientID string) error {
	params := url.Values{"subject": {subject}}
	if clientID != "" {
		params.Set("client", clientID)
	} else {
		params.Set("all", "true")
	}
	_, err := c.do(ctx, http.MethodDelete, "/admin/oauth2/auth/sessions/consent", params, nil, nil)
	return err
}

// RevokeLoginSessions 让主体在所有浏览器上的 Hydra 登录态失效（下次授权必须重新登录）。
func (c *Client) RevokeLoginSessions(ctx context.Context, subject string) error {
	_, err := c.do(ctx, http.MethodDelete, "/admin/oauth2/auth/sessions/login", url.Values{"subject": {subject}}, nil, nil)
	return err
}

// RevokeLoginSessionByID 只让某一个浏览器会话失效。
func (c *Client) RevokeLoginSessionByID(ctx context.Context, sessionID string) error {
	_, err := c.do(ctx, http.MethodDelete, "/admin/oauth2/auth/sessions/login", url.Values{"sid": {sessionID}}, nil, nil)
	return err
}

// ── 令牌 ────────────────────────────────────────────────────────────

// Introspection RFC 7662 内省结果。
type Introspection struct {
	Active    bool           `json:"active"`
	ClientID  string         `json:"client_id,omitempty"`
	Subject   string         `json:"sub,omitempty"`
	Scope     string         `json:"scope,omitempty"`
	TokenUse  string         `json:"token_use,omitempty"`
	ExpiresAt int64          `json:"exp,omitempty"`
	IssuedAt  int64          `json:"iat,omitempty"`
	Audience  []string       `json:"aud,omitempty"`
	Ext       map[string]any `json:"ext,omitempty"`
}

// IntrospectToken 内省访问令牌或刷新令牌。scope 非空时要求令牌覆盖这些权限。
func (c *Client) IntrospectToken(ctx context.Context, token, scope string) (*Introspection, error) {
	form := url.Values{"token": {token}}
	if scope != "" {
		form.Set("scope", scope)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.adminURL+"/admin/oauth2/introspect", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	var out Introspection
	if _, err := c.send(req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteClientTokens 吊销某个客户端签发出去的全部访问令牌（轮换密钥、停用客户端时用）。
func (c *Client) DeleteClientTokens(ctx context.Context, clientID string) error {
	_, err := c.do(ctx, http.MethodDelete, "/admin/oauth2/tokens", url.Values{"client_id": {clientID}}, nil, nil)
	return err
}

// Ready 探测 Hydra 管理端是否就绪（含数据库连通）。
func (c *Client) Ready(ctx context.Context) error {
	_, err := c.do(ctx, http.MethodGet, "/health/ready", nil, nil, nil)
	return err
}

// ── 传输 ────────────────────────────────────────────────────────────

func (c *Client) getFlow(ctx context.Context, kind, param, challenge string, out any) error {
	if strings.TrimSpace(challenge) == "" {
		return &Error{StatusCode: http.StatusBadRequest, Code: "invalid_request", Description: param + " 缺失"}
	}
	_, err := c.do(ctx, http.MethodGet, "/admin/oauth2/auth/requests/"+kind, url.Values{param: {challenge}}, nil, out)
	return err
}

func (c *Client) putFlow(ctx context.Context, action, param, challenge string, body any) (string, error) {
	if strings.TrimSpace(challenge) == "" {
		return "", &Error{StatusCode: http.StatusBadRequest, Code: "invalid_request", Description: param + " 缺失"}
	}
	var out Redirect
	if body == nil {
		body = struct{}{}
	}
	_, err := c.do(ctx, http.MethodPut, "/admin/oauth2/auth/requests/"+action, url.Values{param: {challenge}}, body, &out)
	if err != nil {
		return "", err
	}
	return out.RedirectTo, nil
}

func (c *Client) do(ctx context.Context, method, path string, query url.Values, body, out any) (http.Header, error) {
	if c == nil || c.adminURL == "" {
		return nil, errors.New("hydra: 未配置 Admin 地址")
	}
	target := c.adminURL + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.send(req, out)
}

func (c *Client) send(req *http.Request, out any) (http.Header, error) {
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("hydra: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, fmt.Errorf("hydra: 读取响应失败: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		he := &Error{StatusCode: resp.StatusCode}
		_ = json.Unmarshal(raw, he)
		if he.Code == "" && he.Description == "" {
			he.Description = strings.TrimSpace(string(raw))
		}
		return resp.Header, he
	}
	if out != nil && len(bytes.TrimSpace(raw)) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return resp.Header, fmt.Errorf("hydra: 解析响应失败: %w", err)
		}
	}
	return resp.Header, nil
}

var linkNextPattern = regexp.MustCompile(`<([^>]+)>;\s*rel="next"`)

// nextPageToken 从 Link 头里取下一页的 page_token（Hydra 的分页是 token 式的）。
func nextPageToken(header http.Header) string {
	for _, link := range header.Values("Link") {
		match := linkNextPattern.FindStringSubmatch(link)
		if match == nil {
			continue
		}
		parsed, err := url.Parse(match[1])
		if err != nil {
			continue
		}
		return parsed.Query().Get("page_token")
	}
	return ""
}
