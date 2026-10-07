package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"regexp"
	"strings"
	"time"

	authdomain "aegis/internal/domain/auth"
	plugindomain "aegis/internal/domain/plugin"
	redisrepo "aegis/internal/repository/redis"
	apperrors "aegis/pkg/errors"
)

// 网页扫码登录（QR login）。
//
// 三端协作，网页从头到尾拿不到移动端的令牌：
//
//  1. 网页（应持有 appSecret 的网站服务端代为调用）POST /auth/qr/create，得到公开的 ticketId
//     与只给发起端的 pollToken，把 ticketId 画成二维码；
//  2. 已登录的移动端扫码后 POST /auth/qr/scan，票据记下扫码人并返回发起端的设备、IP 与位置；
//  3. 用户在移动端确认（/auth/qr/confirm）或拒绝（/auth/qr/cancel）；
//  4. 网页轮询 POST /auth/qr/poll，凭 ticketId + pollToken 在确认后领取一份**新签发**的会话。
//
// 确认时并不签发令牌，只记下「谁确认了」；会话在网页领取的那一刻才经 finalizeLogin 签发，
// 绑定网页自己的设备与 IP。令牌因此不会在 Redis 里以票据的形式停留。
// 移动端会话本身已经通过了密码与二次认证，与 Passkey 同理，这里不再叠加二次认证。

const (
	qrLoginTTL = 2 * time.Minute
	// qrLoginPollIntervalSeconds 建议的网页轮询间隔。
	qrLoginPollIntervalSeconds = 2

	errCodeQRLoginTicketInvalid  = 40405 // 404 票据不存在或 pollToken 不匹配
	errCodeQRLoginScannedByOther = 40906 // 409 已被其他账号扫描
	errCodeQRLoginNotScanned     = 40907 // 409 尚未扫码
	errCodeQRLoginTicketUsed     = 40908 // 409 已确认、已取消或已被领取
	errCodeQRLoginTicketExpired  = 41005 // 410 已过期
)

var qrLoginTicketPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{16,128}$`)

// QRLoginCreateInput 网页发起扫码登录时的设备信息。
type QRLoginCreateInput struct {
	AppID     int64
	DeviceID  string
	Device    string
	IP        string
	UserAgent string
	Location  string
}

// QRLoginCreateResult /auth/qr/create 的响应。pollToken 只此一次以明文出现。
type QRLoginCreateResult struct {
	TicketID  string    `json:"ticketId"`
	PollToken string    `json:"pollToken"`
	ExpiresAt time.Time `json:"expiresAt"`
	Interval  int       `json:"interval"`
}

// QRLoginRequester 发起登录的网页端，展示在移动端的确认页上。
type QRLoginRequester struct {
	Device    string    `json:"device,omitempty"`
	IP        string    `json:"ip,omitempty"`
	Location  string    `json:"location,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

// QRLoginScanResult /auth/qr/scan 的响应。
type QRLoginScanResult struct {
	TicketID  string           `json:"ticketId"`
	Status    string           `json:"status"`
	ExpiresAt time.Time        `json:"expiresAt"`
	AppName   string           `json:"appName,omitempty"`
	Requester QRLoginRequester `json:"requester"`
}

// QRLoginScanner 扫码人，网页在「已扫码，请在手机上确认」时展示。
type QRLoginScanner struct {
	Nickname string `json:"nickname,omitempty"`
	Avatar   string `json:"avatar,omitempty"`
}

// QRLoginPollResult /auth/qr/poll 的响应。session 只在第一次读到 confirmed 时出现。
type QRLoginPollResult struct {
	Status    string                  `json:"status"`
	ExpiresAt *time.Time              `json:"expiresAt,omitempty"`
	Scanner   *QRLoginScanner         `json:"scanner,omitempty"`
	Session   *authdomain.LoginResult `json:"session,omitempty"`
}

// CreateQRLogin 签发一张扫码登录票据。
func (s *AuthService) CreateQRLogin(ctx context.Context, input QRLoginCreateInput) (*QRLoginCreateResult, error) {
	if s.sessions == nil {
		return nil, apperrors.New(50320, http.StatusServiceUnavailable, "安全会话服务不可用")
	}
	if s.app != nil {
		app, err := s.app.EnsureLoginAllowed(ctx, input.AppID)
		if err != nil {
			return nil, err
		}
		if err := s.validateLoginPolicy(app, input.DeviceID, input.Device); err != nil {
			return nil, err
		}
	}
	ticketID, err := randomURLToken(18)
	if err != nil {
		return nil, err
	}
	pollToken, err := randomURLToken(32)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	expiresAt := now.Add(qrLoginTTL)
	err = s.sessions.CreateQRLoginTicket(ctx, redisrepo.QRLoginTicket{
		TicketID:  ticketID,
		AppID:     input.AppID,
		Status:    redisrepo.QRLoginStatusPending,
		PollHash:  hashPollToken(pollToken),
		DeviceID:  strings.TrimSpace(input.DeviceID),
		Device:    strings.TrimSpace(input.Device),
		IP:        input.IP,
		UserAgent: input.UserAgent,
		Location:  input.Location,
		CreatedAt: now,
		ExpiresAt: expiresAt,
	}, qrLoginTTL)
	if err != nil {
		return nil, err
	}
	return &QRLoginCreateResult{
		TicketID:  ticketID,
		PollToken: pollToken,
		ExpiresAt: expiresAt,
		Interval:  qrLoginPollIntervalSeconds,
	}, nil
}

// PollQRLogin 网页轮询票据状态；第一次读到 confirmed 时签发会话并作废票据。
//
// 票据不存在（含已过期、已被领取）时返回 expired 而不是错误：对轮询方而言这就是一个状态。
// pollToken 不匹配则按「票据不存在」报错，不透露票据是否存在。
func (s *AuthService) PollQRLogin(ctx context.Context, appID int64, ticketID, pollToken, ip, userAgent string) (*QRLoginPollResult, error) {
	if s.sessions == nil {
		return nil, apperrors.New(50320, http.StatusServiceUnavailable, "安全会话服务不可用")
	}
	if !qrLoginTicketPattern.MatchString(ticketID) || strings.TrimSpace(pollToken) == "" {
		return nil, qrLoginInvalidError()
	}
	ticket, err := s.sessions.GetQRLoginTicket(ctx, appID, ticketID)
	if err != nil {
		return nil, err
	}
	if ticket == nil {
		return &QRLoginPollResult{Status: "expired"}, nil
	}
	pollHash := hashPollToken(pollToken)
	if subtle.ConstantTimeCompare([]byte(ticket.PollHash), []byte(pollHash)) != 1 {
		return nil, qrLoginInvalidError()
	}
	expiresAt := ticket.ExpiresAt

	switch ticket.Status {
	case redisrepo.QRLoginStatusPending:
		return &QRLoginPollResult{Status: "pending", ExpiresAt: &expiresAt}, nil
	case redisrepo.QRLoginStatusScanned:
		return &QRLoginPollResult{
			Status:    "scanned",
			ExpiresAt: &expiresAt,
			Scanner:   &QRLoginScanner{Nickname: ticket.ScannerName, Avatar: ticket.ScannerAvatar},
		}, nil
	case redisrepo.QRLoginStatusCancelled:
		return &QRLoginPollResult{Status: "cancelled"}, nil
	case redisrepo.QRLoginStatusConfirmed:
		// 先原子地把票据标为已领取，再签发：并发的两次轮询只有一次拿得到会话。
		outcome, _, err := s.sessions.TransitionQRLoginTicket(ctx, redisrepo.QRLoginTransitionInput{
			AppID: appID, TicketID: ticketID, Op: "consume", PollHash: pollHash, Now: time.Now().UTC(),
		})
		if err != nil {
			return nil, err
		}
		if outcome != redisrepo.QRLoginTransitionOK {
			return &QRLoginPollResult{Status: "expired"}, nil
		}
		session, err := s.issueQRLoginSession(ctx, appID, ticket, ip, userAgent)
		if err != nil {
			return nil, err
		}
		return &QRLoginPollResult{Status: "confirmed", Session: session}, nil
	default:
		return &QRLoginPollResult{Status: "expired"}, nil
	}
}

// ScanQRLogin 移动端扫码：把票据标为已扫码并返回发起端信息。同一用户重复扫码是幂等的。
func (s *AuthService) ScanQRLogin(ctx context.Context, session *authdomain.Session, ticketID string, scanner QRLoginScanner) (*QRLoginScanResult, error) {
	if err := s.transitionQRLogin(ctx, session, ticketID, "scan", scanner); err != nil {
		return nil, err
	}
	ticket, err := s.sessions.GetQRLoginTicket(ctx, session.AppID, ticketID)
	if err != nil {
		return nil, err
	}
	if ticket == nil {
		return nil, qrLoginExpiredError()
	}
	result := &QRLoginScanResult{
		TicketID:  ticket.TicketID,
		Status:    ticket.Status,
		ExpiresAt: ticket.ExpiresAt,
		Requester: QRLoginRequester{
			Device:    ticket.Device,
			IP:        ticket.IP,
			Location:  ticket.Location,
			CreatedAt: ticket.CreatedAt,
		},
	}
	if s.app != nil {
		if app, err := s.app.GetApp(ctx, session.AppID); err == nil && app != nil {
			result.AppName = app.Name
		}
	}
	return result, nil
}

// ConfirmQRLogin 移动端确认登录。只记下确认，会话在网页领取时签发。
func (s *AuthService) ConfirmQRLogin(ctx context.Context, session *authdomain.Session, ticketID string) error {
	return s.transitionQRLogin(ctx, session, ticketID, "confirm", QRLoginScanner{})
}

// CancelQRLogin 移动端拒绝登录，票据作废。
func (s *AuthService) CancelQRLogin(ctx context.Context, session *authdomain.Session, ticketID string) error {
	return s.transitionQRLogin(ctx, session, ticketID, "cancel", QRLoginScanner{})
}

func (s *AuthService) transitionQRLogin(ctx context.Context, session *authdomain.Session, ticketID, op string, scanner QRLoginScanner) error {
	if s.sessions == nil {
		return apperrors.New(50320, http.StatusServiceUnavailable, "安全会话服务不可用")
	}
	if session == nil {
		return apperrors.New(40100, http.StatusUnauthorized, "未认证")
	}
	if !qrLoginTicketPattern.MatchString(ticketID) {
		return qrLoginInvalidError()
	}
	outcome, status, err := s.sessions.TransitionQRLoginTicket(ctx, redisrepo.QRLoginTransitionInput{
		AppID:         session.AppID,
		TicketID:      ticketID,
		Op:            op,
		UserID:        session.UserID,
		ScannerName:   scanner.Nickname,
		ScannerAvatar: scanner.Avatar,
		Now:           time.Now().UTC(),
	})
	if err != nil {
		return err
	}
	switch outcome {
	case redisrepo.QRLoginTransitionOK:
		return nil
	case redisrepo.QRLoginTransitionMissing:
		return qrLoginExpiredError()
	case redisrepo.QRLoginTransitionOtherUser:
		return apperrors.New(errCodeQRLoginScannedByOther, http.StatusConflict, "该二维码已被其他账号扫描")
	default:
		if status == redisrepo.QRLoginStatusPending {
			return apperrors.New(errCodeQRLoginNotScanned, http.StatusConflict, "请先扫描网页上的登录二维码")
		}
		return apperrors.New(errCodeQRLoginTicketUsed, http.StatusConflict, "该二维码已失效，请在网页上刷新后重新扫描")
	}
}

// issueQRLoginSession 为确认过的扫码人签发一份网页会话，设备信息取自发起端。
func (s *AuthService) issueQRLoginSession(ctx context.Context, appID int64, ticket *redisrepo.QRLoginTicket, ip, userAgent string) (*authdomain.LoginResult, error) {
	if s.pg == nil {
		return nil, apperrors.New(50320, http.StatusServiceUnavailable, "安全会话服务不可用")
	}
	if s.app != nil {
		if _, err := s.app.EnsureLoginAllowed(ctx, appID); err != nil {
			return nil, err
		}
	}
	user, err := s.pg.GetUserByID(ctx, ticket.UserID)
	if err != nil {
		return nil, err
	}
	if user == nil || user.AppID != appID {
		return nil, apperrors.New(40103, http.StatusUnauthorized, "会话用户不存在")
	}
	if err := s.ensureUserLoginState(ctx, user); err != nil {
		return nil, err
	}
	// 领取时的 IP 与 UA 才是网页此刻的真实环境；取不到时用创建票据时记下的。
	if strings.TrimSpace(ip) == "" {
		ip = ticket.IP
	}
	if strings.TrimSpace(userAgent) == "" {
		userAgent = ticket.UserAgent
	}
	result, err := s.finalizeLogin(ctx, nil, user, "qr", "qr", ticket.DeviceID, ticket.Device, ip, userAgent)
	if err != nil {
		return nil, err
	}
	if s.plugin != nil {
		uid := user.ID
		account := user.Account
		s.runDetached("login.qr_session_hook", 5*time.Second, func(actx context.Context) {
			s.plugin.ExecuteHook(actx, HookAuthSessionIssued, map[string]any{"userId": uid, "account": account, "appId": appID}, plugindomain.HookMetadata{IP: ip, AppID: &appID, UserID: &uid})
		})
	}
	return result, nil
}

func qrLoginInvalidError() error {
	return apperrors.New(errCodeQRLoginTicketInvalid, http.StatusNotFound, "登录二维码无效")
}

func qrLoginExpiredError() error {
	return apperrors.New(errCodeQRLoginTicketExpired, http.StatusGone, "登录二维码已过期，请在网页上刷新后重新扫描")
}

func hashPollToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func randomURLToken(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
