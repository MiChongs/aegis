package service

import (
	apperrors "aegis/pkg/errors"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

// Cloud Mail（docs.skymail.ink）的 HTTP 客户端。
//
// Cloud Mail 是跑在 Cloudflare Workers 上的自建邮局：既能收信也能发信，管理员还能管用户、
// 邮箱账号与注册码。Aegis 用它做两件事 —— 作为一档发信服务商（skymail_sender），
// 以及在控制台里代管整套邮局（skymail_admin）。两边共用这一个客户端，因此令牌只有一份。
//
// 接口约定与常见 REST 服务不同，几处都踩过才看出来：
//
//   - 鉴权头是 `Authorization: <token>`，**不带 Bearer 前缀**。
//   - 业务错误一律 HTTP 200，真正的结果在 JSON 的 code 里（401 登录失效、403 无权限、500 业务拒绝）。
//     只看 HTTP 状态码会把「账号被禁用」当成发信成功。
//   - 同一用户最多保留约 11 个登录令牌，再登录就挤掉最早的那个。每封信都重新登录，
//     会把管理员浏览器里的会话挤下线，所以令牌按凭据缓存，只在 401 时重登一次。
//   - 令牌有效期 30 天；时间字段是 UTC 的 `yyyy-MM-dd HH:mm:ss`，没有时区后缀。
//   - GET / DELETE 的参数全部走查询串。

const (
	skymailHTTPTimeout = 20
	// skymailMaxBodyRead 管理面会拉带正文和附件信息的邮件列表，1MB 的通用上限不够用。
	skymailMaxBodyRead = 16 << 20
	// skymailTokenTTL 本地缓存令牌多久。服务端是 30 天，提前几天换掉，避免卡在过期边上。
	skymailTokenTTL = 25 * 24 * time.Hour
	// skymailAccountTTL 「发件地址 → 邮箱账号 id」的缓存时长。账号很少变，删掉重建时最多错这么久。
	skymailAccountTTL = 10 * time.Minute
)

// skymailCredentials 一条 Cloud Mail 配置的登录凭据。
type skymailCredentials struct {
	BaseURL  string
	Email    string
	Password string
}

// key 令牌缓存键。密码进摘要而不是明文，改了密码自然换一个键。
func (c skymailCredentials) key() string {
	sum := sha256.Sum256([]byte(c.BaseURL + "\x00" + strings.ToLower(c.Email) + "\x00" + c.Password))
	return hex.EncodeToString(sum[:])
}

// host 站点主机名，拼进服务商邮件 ID，避免两套 Cloud Mail 的自增 id 撞在一起。
func (c skymailCredentials) host() string {
	parsed, err := url.Parse(c.BaseURL)
	if err != nil || parsed.Host == "" {
		return "skymail"
	}
	return strings.ToLower(parsed.Host)
}

// normalizeSkymailBaseURL 管理员常把文档里的完整接口地址整段贴进来，
// 这里裁成站点根：去掉末尾的 `/`，以及误带的 `/api`、`/api/login`。
func normalizeSkymailBaseURL(raw string) string {
	value := strings.TrimSpace(raw)
	value = strings.TrimRight(value, "/")
	for _, suffix := range []string{"/api/login", "/api"} {
		if strings.HasSuffix(strings.ToLower(value), suffix) {
			value = value[:len(value)-len(suffix)]
			value = strings.TrimRight(value, "/")
		}
	}
	return value
}

type skymailEnvelope struct {
	Code    skymailCode     `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

// skymailCode 信封里的业务码。通常是数字（200 / 401 / 403 / 500），但 Cloud Mail 透传
// 发信渠道的错误时会原样带出对方的字符串码，例如 Cloudflare Email 的
// `{"code":"E_RECIPIENT_NOT_ALLOWED"}`。只按数字解析会把这类回包当成「不是接口数据」，
// 报一句「请确认站点地址」把人带偏。
type skymailCode struct {
	Num int
	Str string
}

func (c *skymailCode) UnmarshalJSON(raw []byte) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return nil
	}
	if trimmed[0] == '"' {
		var value string
		if err := json.Unmarshal(trimmed, &value); err != nil {
			return err
		}
		value = strings.TrimSpace(value)
		if n, err := strconv.Atoi(value); err == nil {
			c.Num = n
		} else {
			c.Str = value
		}
		return nil
	}
	return json.Unmarshal(trimmed, &c.Num)
}

func (c skymailCode) present() bool { return c.Num != 0 || c.Str != "" }

func (c skymailCode) ok() bool { return c.Str == "" && c.Num == http.StatusOK }

func (c skymailCode) unauthorized() bool { return c.Str == "" && c.Num == http.StatusUnauthorized }

type skymailCachedToken struct {
	token     string
	expiresAt time.Time
}

type skymailCachedAccount struct {
	accountID int64
	expiresAt time.Time
}

type skymailClient struct {
	log    *zap.Logger
	client *http.Client
	now    func() time.Time

	mu       sync.Mutex
	tokens   map[string]skymailCachedToken
	accounts map[string]skymailCachedAccount
	// logins 同一凭据的并发登录合并成一次：一批验证码同时发出时，
	// 各自登录会在几毫秒内把令牌槽位用光。
	logins map[string]*skymailLoginFlight
}

type skymailLoginFlight struct {
	done  chan struct{}
	token string
	err   error
}

func newSkymailClient(log *zap.Logger) *skymailClient {
	return &skymailClient{
		log:      log,
		client:   newEmailHTTPClient("email.skymail", skymailHTTPTimeout),
		now:      time.Now,
		tokens:   map[string]skymailCachedToken{},
		accounts: map[string]skymailCachedAccount{},
		logins:   map[string]*skymailLoginFlight{},
	}
}

// skymailCodeError 带着 Cloud Mail 业务码的错误，供上层区分「重登」「改配置」「等一会儿」。
type skymailCodeError struct {
	Code    int
	Message string
}

func (e *skymailCodeError) Error() string {
	return fmt.Sprintf("cloud mail code %d: %s", e.Code, e.Message)
}

// call 带令牌调用一个接口，令牌失效时重新登录一次再试。
//
// query 用于 GET / DELETE；body 非 nil 时按 JSON 发送。out 为 nil 时丢弃 data。
func (c *skymailClient) call(ctx context.Context, cred skymailCredentials, method string, path string, query url.Values, body any, out any) error {
	token, err := c.token(ctx, cred)
	if err != nil {
		return err
	}
	envelope, err := c.do(ctx, cred.BaseURL, method, path, query, body, token)
	if err == nil && envelope.Code.unauthorized() {
		c.dropToken(cred, token)
		if token, err = c.token(ctx, cred); err != nil {
			return err
		}
		envelope, err = c.do(ctx, cred.BaseURL, method, path, query, body, token)
	}
	if err != nil {
		return err
	}
	if !envelope.Code.ok() {
		if envelope.Code.unauthorized() {
			c.dropToken(cred, token)
		}
		return classifySkymailCode(envelope.Code, envelope.Message)
	}
	if out == nil || len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		return nil
	}
	if err := json.Unmarshal(envelope.Data, out); err != nil {
		return apperrors.New(50060, http.StatusBadGateway, "Cloud Mail 返回的数据无法解析："+clampDetail(err.Error()))
	}
	return nil
}

// token 取缓存的令牌，没有就登录。同一凭据的并发登录只发一次请求。
func (c *skymailClient) token(ctx context.Context, cred skymailCredentials) (string, error) {
	key := cred.key()
	c.mu.Lock()
	if cached, ok := c.tokens[key]; ok && c.now().Before(cached.expiresAt) {
		c.mu.Unlock()
		return cached.token, nil
	}
	if flight, ok := c.logins[key]; ok {
		c.mu.Unlock()
		select {
		case <-flight.done:
			return flight.token, flight.err
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	flight := &skymailLoginFlight{done: make(chan struct{})}
	c.logins[key] = flight
	c.mu.Unlock()

	flight.token, flight.err = c.login(ctx, cred)

	c.mu.Lock()
	delete(c.logins, key)
	if flight.err == nil {
		c.tokens[key] = skymailCachedToken{token: flight.token, expiresAt: c.now().Add(skymailTokenTTL)}
	}
	c.mu.Unlock()
	close(flight.done)
	return flight.token, flight.err
}

func (c *skymailClient) dropToken(cred skymailCredentials, token string) {
	key := cred.key()
	c.mu.Lock()
	if cached, ok := c.tokens[key]; ok && cached.token == token {
		delete(c.tokens, key)
	}
	c.mu.Unlock()
}

// forget 丢掉某套凭据的全部缓存（配置被改或删除后调用）。
func (c *skymailClient) forget(cred skymailCredentials) {
	key := cred.key()
	c.mu.Lock()
	delete(c.tokens, key)
	for cacheKey := range c.accounts {
		if strings.HasPrefix(cacheKey, key+"|") {
			delete(c.accounts, cacheKey)
		}
	}
	c.mu.Unlock()
}

func (c *skymailClient) login(ctx context.Context, cred skymailCredentials) (string, error) {
	if cred.BaseURL == "" || cred.Email == "" || cred.Password == "" {
		return "", apperrors.New(40068, http.StatusBadRequest, "Cloud Mail 的站点地址、登录邮箱与密码都不能为空")
	}
	envelope, err := c.do(ctx, cred.BaseURL, http.MethodPost, "/api/login", nil,
		map[string]string{"email": cred.Email, "password": cred.Password}, "")
	if err != nil {
		return "", err
	}
	if !envelope.Code.ok() {
		detail := clampDetail(envelope.Message)
		return "", apperrors.New(40071, http.StatusUnauthorized, withDetail("Cloud Mail 登录失败，请检查登录邮箱与密码", detail))
	}
	var data struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(envelope.Data, &data); err != nil || strings.TrimSpace(data.Token) == "" {
		return "", apperrors.New(50060, http.StatusBadGateway, "Cloud Mail 登录成功却没有返回令牌")
	}
	return strings.TrimSpace(data.Token), nil
}

// do 发一次请求并解出外层信封。HTTP 层面的失败（连不上、5xx、地址不对）在这里就变成错误；
// 信封里的业务码留给调用方判断。
func (c *skymailClient) do(ctx context.Context, baseURL string, method string, path string, query url.Values, body any, token string) (skymailEnvelope, error) {
	endpoint := baseURL + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return skymailEnvelope{}, apperrors.New(50060, http.StatusInternalServerError, "构造 Cloud Mail 请求失败")
		}
		reader = bytes.NewReader(raw)
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return skymailEnvelope{}, apperrors.New(40070, http.StatusBadRequest, "Cloud Mail 站点地址不合法："+clampDetail(err.Error()))
	}
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		request.Header.Set("Authorization", token)
	}

	response, err := c.client.Do(request)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return skymailEnvelope{}, ctxErr
		}
		return skymailEnvelope{}, apperrors.New(50060, http.StatusBadGateway, "连接 Cloud Mail 失败："+clampDetail(err.Error()))
	}
	defer response.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(response.Body, skymailMaxBodyRead))

	if response.StatusCode == http.StatusNotFound {
		return skymailEnvelope{}, apperrors.New(40070, http.StatusBadRequest,
			"Cloud Mail 接口不存在（HTTP 404）：站点地址应填邮局的根地址，例如 https://mail.example.com，不要带 /api")
	}
	if response.StatusCode >= 500 {
		return skymailEnvelope{}, apperrors.New(50060, http.StatusBadGateway,
			withDetail(fmt.Sprintf("Cloud Mail 返回 %d，请稍后重试", response.StatusCode), string(raw)))
	}
	var envelope skymailEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil || !envelope.Code.present() {
		return skymailEnvelope{}, apperrors.New(50060, http.StatusBadGateway,
			withDetail(fmt.Sprintf("Cloud Mail 返回的不是接口数据（HTTP %d），请确认站点地址", response.StatusCode), string(raw)))
	}
	return envelope, nil
}

// classifySkymailCode 把信封里的业务码翻译成平台错误码。
//
// Cloud Mail 的业务拒绝大多是 500 + 一句中文（发件账号不存在、域名未配置、超出发件次数……），
// 这些都是「改配置」类问题，不该重试，也不该进熔断。
func classifySkymailCode(code skymailCode, message string) error {
	detail := clampDetail(message)
	if code.Str != "" {
		return classifySkymailChannelCode(code.Str, detail)
	}
	switch code.Num {
	case http.StatusUnauthorized:
		return apperrors.New(40071, http.StatusUnauthorized, withDetail("Cloud Mail 登录已失效", detail))
	case http.StatusForbidden:
		return apperrors.New(40072, http.StatusForbidden, withDetail("Cloud Mail 拒绝了本次操作（账号权限不足、发件被禁用或次数用尽）", detail))
	case http.StatusBadGateway:
		return apperrors.New(50060, http.StatusBadGateway, withDetail("Cloud Mail 站点自身配置不完整", detail))
	default:
		if detail == "" {
			detail = "code " + strconv.Itoa(code.Num)
		}
		return apperrors.New(40070, http.StatusBadRequest, "Cloud Mail："+detail)
	}
}

// classifySkymailChannelCode Cloud Mail 透传的发信渠道错误（字符串码）。
//
// 目前见过的是 Cloudflare Email（邮局绑定了 Workers 的 send_email）：它只能发给
// 在 Email Routing 里验证过的目标地址，发给任意用户的验证码会被 E_RECIPIENT_NOT_ALLOWED 拒掉。
// 这是邮局的发信渠道选错了，不是平台或站点地址的问题，提示里要把出路说清楚。
func classifySkymailChannelCode(code string, detail string) error {
	upstream := code
	if detail != "" {
		upstream += "：" + detail
	}
	switch code {
	case "E_RECIPIENT_NOT_ALLOWED":
		return apperrors.New(40072, http.StatusForbidden,
			"Cloud Mail 用 Cloudflare Email 发信，而 Cloudflare 只允许发给在 Email Routing 里验证过的地址。"+
				"要发给任意收件人，请在邮局后台为该域名配置 Resend（"+upstream+"）")
	case "E_SENDER_NOT_VERIFIED", "E_SENDER_DOMAIN_NOT_AVAILABLE":
		return apperrors.New(40072, http.StatusForbidden,
			"Cloud Mail 的发件域名没有在 Cloudflare Email 里启用（"+upstream+"）")
	default:
		return apperrors.New(40070, http.StatusBadRequest, "Cloud Mail 发信渠道拒绝："+clampDetail(upstream))
	}
}

// ── 业务接口 ──

// skymailText 兼容字符串、数组、null 的文本字段（抄送 / 密送在不同版本里有过两种形态）。
type skymailText string

func (t *skymailText) UnmarshalJSON(raw []byte) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		*t = ""
		return nil
	}
	if trimmed[0] == '"' {
		var value string
		if err := json.Unmarshal(trimmed, &value); err != nil {
			return err
		}
		*t = skymailText(value)
		return nil
	}
	*t = skymailText(trimmed)
	return nil
}

// skymailTime 把 Cloud Mail 的 UTC 时间（无时区后缀）转成 RFC 3339，控制台按浏览器时区显示。
type skymailTime string

func (t *skymailTime) UnmarshalJSON(raw []byte) error {
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		*t = ""
		return nil
	}
	value = strings.TrimSpace(value)
	for _, layout := range []string{"2006-01-02 15:04:05", "2006-01-02T15:04:05", "2006-01-02"} {
		if parsed, err := time.ParseInLocation(layout, value, time.UTC); err == nil {
			*t = skymailTime(parsed.UTC().Format(time.RFC3339))
			return nil
		}
	}
	*t = skymailTime(value)
	return nil
}

// SkymailRole 权限身份（发件额度与可加邮箱数）。
type SkymailRole struct {
	RoleID       int64  `json:"roleId,omitempty"`
	Name         string `json:"name"`
	SendCount    int64  `json:"sendCount"`
	SendType     string `json:"sendType,omitempty"`
	AccountCount int64  `json:"accountCount"`
	IsDefault    int    `json:"isDefault,omitempty"`
}

// SkymailProfile 登录账号的信息：GET /api/my/loginUserInfo。
type SkymailProfile struct {
	UserID    int64    `json:"userId"`
	Email     string   `json:"email"`
	Name      string   `json:"name"`
	Type      int      `json:"type"`
	SendCount int64    `json:"sendCount"`
	PermKeys  []string `json:"permKeys"`
	Account   struct {
		AccountID int64  `json:"accountId"`
		Email     string `json:"email"`
		Name      string `json:"name"`
	} `json:"account"`
	Role SkymailRole `json:"role"`
}

// SkymailAttachment 邮件附件的元信息。
type SkymailAttachment struct {
	AttID    int64  `json:"attId"`
	Filename string `json:"filename"`
	MimeType string `json:"mimeType"`
	Size     int64  `json:"size"`
}

// SkymailEmail 一封信。用户邮件与全部邮件两个接口的字段并集。
type SkymailEmail struct {
	EmailID    int64               `json:"emailId"`
	SendEmail  string              `json:"sendEmail"`
	Name       string              `json:"name"`
	Subject    string              `json:"subject"`
	ToEmail    string              `json:"toEmail"`
	ToName     string              `json:"toName,omitempty"`
	UserEmail  string              `json:"userEmail,omitempty"`
	AccountID  int64               `json:"accountId"`
	UserID     int64               `json:"userId"`
	Type       int                 `json:"type"`
	Status     int                 `json:"status"`
	Unread     int                 `json:"unread"`
	IsDel      int                 `json:"isDel"`
	IsStar     int                 `json:"isStar"`
	Content    string              `json:"content,omitempty"`
	Text       string              `json:"text,omitempty"`
	Cc         skymailText         `json:"cc,omitempty"`
	Bcc        skymailText         `json:"bcc,omitempty"`
	CreateTime skymailTime         `json:"createTime"`
	AttList    []SkymailAttachment `json:"attList"`
}

// SkymailEmailCursor 最新一封邮件的游标，轮询用。
type SkymailEmailCursor struct {
	EmailID   int64 `json:"emailId"`
	AccountID int64 `json:"accountId"`
	UserID    int64 `json:"userId"`
}

// SkymailEmailPage 邮件列表（游标分页）。
type SkymailEmailPage struct {
	Total       int64               `json:"total"`
	LatestEmail *SkymailEmailCursor `json:"latestEmail,omitempty"`
	List        []SkymailEmail      `json:"list"`
}

// SkymailAccount 一个邮箱账号（一个用户可以有多个邮箱地址）。
type SkymailAccount struct {
	AccountID     int64       `json:"accountId"`
	Email         string      `json:"email"`
	Name          string      `json:"name"`
	UserID        int64       `json:"userId"`
	AllReceive    int         `json:"allReceive"`
	Sort          int64       `json:"sort"`
	IsDel         int         `json:"isDel"`
	CreateTime    skymailTime `json:"createTime"`
	AddVerifyOpen *bool       `json:"addVerifyOpen,omitempty"`
}

// SkymailUser 一个邮局用户（管理）。
type SkymailUser struct {
	UserID               int64       `json:"userId"`
	Email                string      `json:"email"`
	Type                 int         `json:"type"`
	Status               int         `json:"status"`
	IsDel                int         `json:"isDel"`
	SendCount            int64       `json:"sendCount"`
	CreateTime           skymailTime `json:"createTime"`
	ActiveTime           skymailTime `json:"activeTime"`
	CreateIP             string      `json:"createIp"`
	ActiveIP             string      `json:"activeIp"`
	OS                   string      `json:"os"`
	Browser              string      `json:"browser"`
	Device               string      `json:"device"`
	ReceiveEmailCount    int64       `json:"receiveEmailCount"`
	SendEmailCount       int64       `json:"sendEmailCount"`
	AccountCount         int64       `json:"accountCount"`
	DelReceiveEmailCount int64       `json:"delReceiveEmailCount"`
	DelSendEmailCount    int64       `json:"delSendEmailCount"`
	DelAccountCount      int64       `json:"delAccountCount"`
	Username             *string     `json:"username"`
	Name                 *string     `json:"name"`
	Avatar               *string     `json:"avatar"`
	Platform             *string     `json:"platform"`
	TrustLevel           *int        `json:"trustLevel"`
	SendAction           struct {
		HasPerm   bool   `json:"hasPerm"`
		SendType  string `json:"sendType"`
		SendCount int64  `json:"sendCount"`
	} `json:"sendAction"`
}

// SkymailUserPage 用户列表（页码分页）。
type SkymailUserPage struct {
	Total int64         `json:"total"`
	List  []SkymailUser `json:"list"`
}

// SkymailRegKey 注册码。ExpireTime 为 null 表示已过期。
type SkymailRegKey struct {
	RegKeyID   int64        `json:"regKeyId"`
	Code       string       `json:"code"`
	Count      int64        `json:"count"`
	RoleID     int64        `json:"roleId"`
	RoleName   string       `json:"roleName"`
	UserID     int64        `json:"userId"`
	ExpireTime *skymailTime `json:"expireTime"`
	CreateTime skymailTime  `json:"createTime"`
}

// SkymailRegKeyUse 注册码的一次使用记录。
type SkymailRegKeyUse struct {
	Email      string      `json:"email"`
	CreateTime skymailTime `json:"createTime"`
}

// skymailSendAttachment 发信附件：内容是 Base64。
type skymailSendAttachment struct {
	Filename    string `json:"filename"`
	Content     string `json:"content"`
	ContentType string `json:"contentType,omitempty"`
	Size        int64  `json:"size,omitempty"`
}

// skymailSendRequest POST /api/email/send 的请求体。
type skymailSendRequest struct {
	AccountID    int64                   `json:"accountId"`
	ReceiveEmail []string                `json:"receiveEmail"`
	Subject      string                  `json:"subject"`
	Content      string                  `json:"content"`
	Text         string                  `json:"text,omitempty"`
	Name         string                  `json:"name,omitempty"`
	SendType     string                  `json:"sendType,omitempty"`
	EmailID      int64                   `json:"emailId,omitempty"`
	Attachments  []skymailSendAttachment `json:"attachments,omitempty"`
}

func (c *skymailClient) profile(ctx context.Context, cred skymailCredentials) (*SkymailProfile, error) {
	var out SkymailProfile
	if err := c.call(ctx, cred, http.MethodGet, "/api/my/loginUserInfo", nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *skymailClient) send(ctx context.Context, cred skymailCredentials, req skymailSendRequest) ([]SkymailEmail, error) {
	var out []SkymailEmail
	if err := c.call(ctx, cred, http.MethodPost, "/api/email/send", nil, req, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *skymailClient) listEmails(ctx context.Context, cred skymailCredentials, query url.Values) (*SkymailEmailPage, error) {
	var out SkymailEmailPage
	if err := c.call(ctx, cred, http.MethodGet, "/api/email/list", query, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *skymailClient) deleteEmails(ctx context.Context, cred skymailCredentials, ids []int64) error {
	return c.call(ctx, cred, http.MethodDelete, "/api/email/delete", url.Values{"emailIds": {joinSkymailIDs(ids)}}, nil, nil)
}

func (c *skymailClient) listAccounts(ctx context.Context, cred skymailCredentials, query url.Values) ([]SkymailAccount, error) {
	var out []SkymailAccount
	if err := c.call(ctx, cred, http.MethodGet, "/api/account/list", query, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *skymailClient) addAccount(ctx context.Context, cred skymailCredentials, email string, token string) (*SkymailAccount, error) {
	body := map[string]string{"email": email}
	if token != "" {
		body["token"] = token
	}
	var out SkymailAccount
	if err := c.call(ctx, cred, http.MethodPost, "/api/account/add", nil, body, &out); err != nil {
		return nil, err
	}
	c.forgetAccounts(cred)
	return &out, nil
}

func (c *skymailClient) deleteAccount(ctx context.Context, cred skymailCredentials, accountID int64) error {
	err := c.call(ctx, cred, http.MethodDelete, "/api/account/delete",
		url.Values{"accountId": {strconv.FormatInt(accountID, 10)}}, nil, nil)
	if err == nil {
		c.forgetAccounts(cred)
	}
	return err
}

func (c *skymailClient) listUsers(ctx context.Context, cred skymailCredentials, query url.Values) (*SkymailUserPage, error) {
	var out SkymailUserPage
	if err := c.call(ctx, cred, http.MethodGet, "/api/user/list", query, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *skymailClient) addUser(ctx context.Context, cred skymailCredentials, email string, password string) error {
	return c.call(ctx, cred, http.MethodPost, "/api/user/add", nil,
		map[string]string{"email": email, "password": password}, nil)
}

func (c *skymailClient) setUserStatus(ctx context.Context, cred skymailCredentials, userID int64, status int) error {
	return c.call(ctx, cred, http.MethodPut, "/api/user/setStatus", nil,
		map[string]int64{"userId": userID, "status": int64(status)}, nil)
}

func (c *skymailClient) deleteUsers(ctx context.Context, cred skymailCredentials, ids []int64) error {
	return c.call(ctx, cred, http.MethodDelete, "/api/user/delete", url.Values{"userIds": {joinSkymailIDs(ids)}}, nil, nil)
}

func (c *skymailClient) listAllEmails(ctx context.Context, cred skymailCredentials, query url.Values) (*SkymailEmailPage, error) {
	var out SkymailEmailPage
	if err := c.call(ctx, cred, http.MethodGet, "/api/allEmail/list", query, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *skymailClient) deleteAllEmails(ctx context.Context, cred skymailCredentials, ids []int64) error {
	return c.call(ctx, cred, http.MethodDelete, "/api/allEmail/delete", url.Values{"emailIds": {joinSkymailIDs(ids)}}, nil, nil)
}

func (c *skymailClient) listRegKeys(ctx context.Context, cred skymailCredentials, code string) ([]SkymailRegKey, error) {
	query := url.Values{}
	if code != "" {
		query.Set("code", code)
	}
	var out []SkymailRegKey
	if err := c.call(ctx, cred, http.MethodGet, "/api/regKey/list", query, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *skymailClient) addRegKey(ctx context.Context, cred skymailCredentials, code string, roleID int64, count int64, expireTime string) error {
	return c.call(ctx, cred, http.MethodPost, "/api/regKey/add", nil, map[string]any{
		"code": code, "roleId": roleID, "count": count, "expireTime": expireTime,
	}, nil)
}

func (c *skymailClient) regKeyHistory(ctx context.Context, cred skymailCredentials, regKeyID int64) ([]SkymailRegKeyUse, error) {
	var out []SkymailRegKeyUse
	if err := c.call(ctx, cred, http.MethodGet, "/api/regKey/history",
		url.Values{"regKeyId": {strconv.FormatInt(regKeyID, 10)}}, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *skymailClient) deleteRegKeys(ctx context.Context, cred skymailCredentials, ids []int64) error {
	return c.call(ctx, cred, http.MethodDelete, "/api/regKey/delete", url.Values{"regKeyIds": {joinSkymailIDs(ids)}}, nil, nil)
}

// roles 可选的权限身份。文档没有列这个接口；它是 Cloud Mail 前端「添加注册码」下拉用的
// `/role/selectUse`，任何登录用户都能调。注册码必须指定身份 id，没有它管理员只能去对方后台查。
func (c *skymailClient) roles(ctx context.Context, cred skymailCredentials) ([]SkymailRole, error) {
	var out []SkymailRole
	if err := c.call(ctx, cred, http.MethodGet, "/api/role/selectUse", nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// resolveAccountID 发件地址 → 邮箱账号 id。地址为空或等于登录邮箱时用登录主邮箱。
func (c *skymailClient) resolveAccountID(ctx context.Context, cred skymailCredentials, address string) (int64, error) {
	address = strings.ToLower(strings.TrimSpace(address))
	if address == "" {
		address = strings.ToLower(cred.Email)
	}
	cacheKey := cred.key() + "|" + address
	c.mu.Lock()
	if cached, ok := c.accounts[cacheKey]; ok && c.now().Before(cached.expiresAt) {
		c.mu.Unlock()
		return cached.accountID, nil
	}
	c.mu.Unlock()

	accountID, err := c.lookupAccountID(ctx, cred, address)
	if err != nil {
		return 0, err
	}
	c.mu.Lock()
	c.accounts[cacheKey] = skymailCachedAccount{accountID: accountID, expiresAt: c.now().Add(skymailAccountTTL)}
	c.mu.Unlock()
	return accountID, nil
}

func (c *skymailClient) lookupAccountID(ctx context.Context, cred skymailCredentials, address string) (int64, error) {
	if address == strings.ToLower(cred.Email) {
		profile, err := c.profile(ctx, cred)
		if err != nil {
			return 0, err
		}
		if profile.Account.AccountID > 0 {
			return profile.Account.AccountID, nil
		}
	}
	// 账号列表只有游标分页：下一页带上一页最后一条的 accountId 与 sort。封顶 40 页（1200 个邮箱）。
	query := url.Values{"size": {"30"}}
	for page := 0; page < 40; page++ {
		items, err := c.listAccounts(ctx, cred, query)
		if err != nil {
			return 0, err
		}
		for _, item := range items {
			if item.IsDel == 0 && strings.EqualFold(strings.TrimSpace(item.Email), address) {
				return item.AccountID, nil
			}
		}
		if len(items) < 30 {
			break
		}
		last := items[len(items)-1]
		query.Set("accountId", strconv.FormatInt(last.AccountID, 10))
		query.Set("lastSort", strconv.FormatInt(last.Sort, 10))
	}
	return 0, apperrors.New(40070, http.StatusBadRequest,
		"发件地址 "+address+" 不在这个 Cloud Mail 账号名下，请先在「邮局 → 邮箱账号」里添加，或改用登录邮箱发信")
}

func (c *skymailClient) forgetAccounts(cred skymailCredentials) {
	prefix := cred.key() + "|"
	c.mu.Lock()
	for cacheKey := range c.accounts {
		if strings.HasPrefix(cacheKey, prefix) {
			delete(c.accounts, cacheKey)
		}
	}
	c.mu.Unlock()
}

func joinSkymailIDs(ids []int64) string {
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		parts = append(parts, strconv.FormatInt(id, 10))
	}
	return strings.Join(parts, ",")
}
