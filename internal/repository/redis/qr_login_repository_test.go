package redis

import (
	"context"
	"testing"
	"time"

	miniredis "github.com/alicebob/miniredis/v2"
	redislib "github.com/redis/go-redis/v9"
)

func newQRLoginTestRepo(t *testing.T) (*SessionRepository, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis: %v", err)
	}
	t.Cleanup(mr.Close)
	client := redislib.NewClient(&redislib.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return NewSessionRepository(client, "test"), mr
}

func seedQRLoginTicket(t *testing.T, repo *SessionRepository, ticketID string) {
	t.Helper()
	now := time.Now().UTC()
	err := repo.CreateQRLoginTicket(context.Background(), QRLoginTicket{
		TicketID:  ticketID,
		AppID:     7,
		Status:    QRLoginStatusPending,
		PollHash:  "hash-1",
		DeviceID:  "web-1",
		Device:    "Chrome（Windows）",
		IP:        "203.0.113.9",
		CreatedAt: now,
		ExpiresAt: now.Add(2 * time.Minute),
	}, 2*time.Minute)
	if err != nil {
		t.Fatalf("create ticket: %v", err)
	}
}

func transition(t *testing.T, repo *SessionRepository, op string, userID int64, pollHash string) (QRLoginTransition, string) {
	t.Helper()
	outcome, status, err := repo.TransitionQRLoginTicket(context.Background(), QRLoginTransitionInput{
		AppID: 7, TicketID: "ticket-aaaaaaaaaaaaaaaa", Op: op, UserID: userID, PollHash: pollHash,
		ScannerName: "alice", Now: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("%s: %v", op, err)
	}
	return outcome, status
}

// 完整的确认链路：扫码 → 他人扫码被拒 → 他人确认被拒 → 本人确认（可重放）→ 领取一次。
func TestQRLoginTicketConfirmFlow(t *testing.T) {
	repo, _ := newQRLoginTestRepo(t)
	seedQRLoginTicket(t, repo, "ticket-aaaaaaaaaaaaaaaa")

	if got, _ := transition(t, repo, "confirm", 1, ""); got != QRLoginTransitionWrongState {
		t.Fatalf("confirm before scan = %s, want state", got)
	}
	if got, status := transition(t, repo, "scan", 1, ""); got != QRLoginTransitionOK || status != QRLoginStatusScanned {
		t.Fatalf("first scan = %s/%s", got, status)
	}
	if got, _ := transition(t, repo, "scan", 1, ""); got != QRLoginTransitionOK {
		t.Fatalf("rescan by same user = %s, want ok", got)
	}
	if got, _ := transition(t, repo, "scan", 2, ""); got != QRLoginTransitionOtherUser {
		t.Fatalf("scan by other user = %s, want other", got)
	}
	if got, _ := transition(t, repo, "confirm", 2, ""); got != QRLoginTransitionOtherUser {
		t.Fatalf("confirm by other user = %s, want other", got)
	}
	if got, _ := transition(t, repo, "consume", 0, "hash-1"); got != QRLoginTransitionWrongState {
		t.Fatalf("consume before confirm = %s, want state", got)
	}
	if got, _ := transition(t, repo, "confirm", 1, ""); got != QRLoginTransitionOK {
		t.Fatalf("confirm = %s", got)
	}
	if got, _ := transition(t, repo, "confirm", 1, ""); got != QRLoginTransitionOK {
		t.Fatalf("repeated confirm = %s, want ok", got)
	}
	if got, _ := transition(t, repo, "consume", 0, "wrong"); got != QRLoginTransitionTokenMismatch {
		t.Fatalf("consume with wrong token = %s, want token", got)
	}
	if got, _ := transition(t, repo, "consume", 0, "hash-1"); got != QRLoginTransitionOK {
		t.Fatalf("consume = %s", got)
	}
	if got, status := transition(t, repo, "consume", 0, "hash-1"); got != QRLoginTransitionWrongState || status != QRLoginStatusConsumed {
		t.Fatalf("second consume = %s/%s, want state/consumed", got, status)
	}

	ticket, err := repo.GetQRLoginTicket(context.Background(), 7, "ticket-aaaaaaaaaaaaaaaa")
	if err != nil || ticket == nil {
		t.Fatalf("get ticket: %v %v", ticket, err)
	}
	if ticket.UserID != 1 || ticket.ScannerName != "alice" || ticket.Device != "Chrome（Windows）" {
		t.Fatalf("ticket fields not kept: %+v", ticket)
	}
}

// 拒绝之后票据作废，确认与领取都不再成功；拒绝只能由扫码人发起。
func TestQRLoginTicketCancel(t *testing.T) {
	repo, _ := newQRLoginTestRepo(t)
	seedQRLoginTicket(t, repo, "ticket-aaaaaaaaaaaaaaaa")

	if got, _ := transition(t, repo, "cancel", 1, ""); got != QRLoginTransitionWrongState {
		t.Fatalf("cancel before scan = %s, want state", got)
	}
	transition(t, repo, "scan", 1, "")
	if got, _ := transition(t, repo, "cancel", 2, ""); got != QRLoginTransitionOtherUser {
		t.Fatalf("cancel by other user = %s, want other", got)
	}
	if got, _ := transition(t, repo, "cancel", 1, ""); got != QRLoginTransitionOK {
		t.Fatalf("cancel = %s", got)
	}
	if got, status := transition(t, repo, "confirm", 1, ""); got != QRLoginTransitionWrongState || status != QRLoginStatusCancelled {
		t.Fatalf("confirm after cancel = %s/%s", got, status)
	}
}

// TTL 到期后票据消失，任何迁移都报 missing，读取返回 nil。
func TestQRLoginTicketExpires(t *testing.T) {
	repo, mr := newQRLoginTestRepo(t)
	seedQRLoginTicket(t, repo, "ticket-aaaaaaaaaaaaaaaa")
	mr.FastForward(3 * time.Minute)

	if got, _ := transition(t, repo, "scan", 1, ""); got != QRLoginTransitionMissing {
		t.Fatalf("scan after expiry = %s, want missing", got)
	}
	ticket, err := repo.GetQRLoginTicket(context.Background(), 7, "ticket-aaaaaaaaaaaaaaaa")
	if err != nil || ticket != nil {
		t.Fatalf("get after expiry = %v, %v", ticket, err)
	}
}
