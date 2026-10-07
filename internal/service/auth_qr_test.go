package service

import (
	"context"
	"errors"
	"testing"
	"time"

	authdomain "aegis/internal/domain/auth"
	redisrepo "aegis/internal/repository/redis"
	apperrors "aegis/pkg/errors"
	miniredis "github.com/alicebob/miniredis/v2"
	redislib "github.com/redis/go-redis/v9"
)

// 扫码登录对客户端可见的错误码必须登记在网关错误目录里，客户端靠它分类与展示。
func TestQRLoginErrorCodesAreInGatewayCatalog(t *testing.T) {
	t.Parallel()

	registered := map[int]bool{}
	for _, item := range gatewayErrors {
		registered[item.Code] = true
	}
	for _, code := range []int{
		errCodeQRLoginTicketInvalid, errCodeQRLoginScannedByOther, errCodeQRLoginNotScanned,
		errCodeQRLoginTicketUsed, errCodeQRLoginTicketExpired,
	} {
		if !registered[code] {
			t.Errorf("扫码登录错误码 %d 没有登记在网关错误目录（auth_protocol_catalog.go）里", code)
		}
	}
}

func newQRLoginTestService(t *testing.T) (*AuthService, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis: %v", err)
	}
	t.Cleanup(mr.Close)
	client := redislib.NewClient(&redislib.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return &AuthService{sessions: redisrepo.NewSessionRepository(client, "test")}, mr
}

func appErrorCode(t *testing.T, err error) int {
	t.Helper()
	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("want AppError, got %v", err)
	}
	return appErr.Code
}

func TestQRLoginServiceStatesAndErrors(t *testing.T) {
	svc, mr := newQRLoginTestService(t)
	ctx := context.Background()

	created, err := svc.CreateQRLogin(ctx, QRLoginCreateInput{AppID: 7, DeviceID: "web-1", Device: "Chrome（Windows）", IP: "203.0.113.9"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !qrLoginTicketPattern.MatchString(created.TicketID) || created.PollToken == "" || created.Interval <= 0 {
		t.Fatalf("unexpected create result: %+v", created)
	}

	poll, err := svc.PollQRLogin(ctx, 7, created.TicketID, created.PollToken, "", "")
	if err != nil || poll.Status != "pending" {
		t.Fatalf("poll pending = %+v, %v", poll, err)
	}
	if _, err := svc.PollQRLogin(ctx, 7, created.TicketID, "wrong-token", "", ""); appErrorCode(t, err) != errCodeQRLoginTicketInvalid {
		t.Fatalf("poll with wrong token should be 40405, got %v", err)
	}

	alice := &authdomain.Session{AppID: 7, UserID: 1, Account: "alice"}
	bob := &authdomain.Session{AppID: 7, UserID: 2, Account: "bob"}

	if err := svc.ConfirmQRLogin(ctx, alice, created.TicketID); appErrorCode(t, err) != errCodeQRLoginNotScanned {
		t.Fatalf("confirm before scan should be 40907, got %v", err)
	}
	scan, err := svc.ScanQRLogin(ctx, alice, created.TicketID, QRLoginScanner{Nickname: "Alice"})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if scan.Status != "scanned" || scan.Requester.Device != "Chrome（Windows）" || scan.Requester.IP != "203.0.113.9" {
		t.Fatalf("unexpected scan result: %+v", scan)
	}
	if _, err := svc.ScanQRLogin(ctx, bob, created.TicketID, QRLoginScanner{}); appErrorCode(t, err) != errCodeQRLoginScannedByOther {
		t.Fatalf("scan by other user should be 40906, got %v", err)
	}

	poll, err = svc.PollQRLogin(ctx, 7, created.TicketID, created.PollToken, "", "")
	if err != nil || poll.Status != "scanned" || poll.Scanner == nil || poll.Scanner.Nickname != "Alice" {
		t.Fatalf("poll scanned = %+v, %v", poll, err)
	}

	if err := svc.CancelQRLogin(ctx, alice, created.TicketID); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	poll, err = svc.PollQRLogin(ctx, 7, created.TicketID, created.PollToken, "", "")
	if err != nil || poll.Status != "cancelled" {
		t.Fatalf("poll cancelled = %+v, %v", poll, err)
	}
	if err := svc.ConfirmQRLogin(ctx, alice, created.TicketID); appErrorCode(t, err) != errCodeQRLoginTicketUsed {
		t.Fatalf("confirm after cancel should be 40908, got %v", err)
	}

	// 另一张票过期之后：网页读到 expired，移动端扫码得到 41005。
	other, err := svc.CreateQRLogin(ctx, QRLoginCreateInput{AppID: 7})
	if err != nil {
		t.Fatalf("create second: %v", err)
	}
	mr.FastForward(qrLoginTTL + time.Second)
	poll, err = svc.PollQRLogin(ctx, 7, other.TicketID, other.PollToken, "", "")
	if err != nil || poll.Status != "expired" {
		t.Fatalf("poll expired = %+v, %v", poll, err)
	}
	if _, err := svc.ScanQRLogin(ctx, alice, other.TicketID, QRLoginScanner{}); appErrorCode(t, err) != errCodeQRLoginTicketExpired {
		t.Fatalf("scan after expiry should be 41005, got %v", err)
	}

	// 票据按应用隔离：另一个应用的令牌扫不到这张票。
	third, _ := svc.CreateQRLogin(ctx, QRLoginCreateInput{AppID: 7})
	foreign := &authdomain.Session{AppID: 8, UserID: 1}
	if _, err := svc.ScanQRLogin(ctx, foreign, third.TicketID, QRLoginScanner{}); appErrorCode(t, err) != errCodeQRLoginTicketExpired {
		t.Fatalf("scan from another app should not find the ticket, got %v", err)
	}
}
