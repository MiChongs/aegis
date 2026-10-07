package redis

import (
	"context"
	"fmt"
	"strconv"
	"time"

	redislib "github.com/redis/go-redis/v9"
)

// QRLoginTicket 网页扫码登录票据。
//
// 一张票就是一个 hash，生存期由 key 的 TTL 决定（创建时设好，状态迁移不续期）。
// 发起端（网页）的信息在创建时写入；扫码端（已登录的移动端用户）在 scan 时写入。
// pollHash 是 pollToken 的 SHA-256，明文只在创建响应里出现一次。
type QRLoginTicket struct {
	TicketID      string
	AppID         int64
	Status        string
	PollHash      string
	DeviceID      string
	Device        string
	IP            string
	UserAgent     string
	Location      string
	UserID        int64
	ScannerName   string
	ScannerAvatar string
	CreatedAt     time.Time
	ExpiresAt     time.Time
}

// 票据状态。consumed 是 confirmed 被网页领走会话之后的终态，对外按 expired 呈现。
const (
	QRLoginStatusPending   = "pending"
	QRLoginStatusScanned   = "scanned"
	QRLoginStatusConfirmed = "confirmed"
	QRLoginStatusCancelled = "cancelled"
	QRLoginStatusConsumed  = "consumed"
)

// QRLoginTransition 状态迁移的结果。
type QRLoginTransition string

const (
	// QRLoginTransitionOK 迁移成功（或幂等重放：同一用户重复扫码 / 重复确认）。
	QRLoginTransitionOK QRLoginTransition = "ok"
	// QRLoginTransitionMissing 票据不存在或已过期。
	QRLoginTransitionMissing QRLoginTransition = "missing"
	// QRLoginTransitionOtherUser 票据已被另一个账号扫描。
	QRLoginTransitionOtherUser QRLoginTransition = "other"
	// QRLoginTransitionTokenMismatch pollToken 不匹配。
	QRLoginTransitionTokenMismatch QRLoginTransition = "token"
	// QRLoginTransitionWrongState 当前状态不允许这一步；具体状态见返回的 status。
	QRLoginTransitionWrongState QRLoginTransition = "state"
)

// qrLoginTransitionScript 在一次 EVAL 里完成「读状态 → 校验 → 写状态」，
// 两部手机同时扫同一张票、确认与网页领取并发时都只会有一方成功。
//
// KEYS[1] 票据 key
// ARGV    op, userId, pollHash, scannerName, scannerAvatar, nowUnix
// 返回    {结果, 当前状态}
var qrLoginTransitionScript = redislib.NewScript(`
local key = KEYS[1]
if redis.call('EXISTS', key) == 0 then return {'missing', ''} end
local op = ARGV[1]
local status = redis.call('HGET', key, 'status') or ''
local owner = redis.call('HGET', key, 'userId') or ''
if op == 'scan' then
  if status == 'pending' then
    redis.call('HSET', key, 'status', 'scanned', 'userId', ARGV[2],
      'scannerName', ARGV[4], 'scannerAvatar', ARGV[5], 'scannedAt', ARGV[6])
    return {'ok', 'scanned'}
  end
  if status == 'scanned' then
    if owner == ARGV[2] then return {'ok', status} end
    return {'other', status}
  end
  return {'state', status}
elseif op == 'confirm' then
  if status == 'scanned' or status == 'confirmed' then
    if owner ~= ARGV[2] then return {'other', status} end
    if status == 'scanned' then
      redis.call('HSET', key, 'status', 'confirmed', 'confirmedAt', ARGV[6])
    end
    return {'ok', 'confirmed'}
  end
  return {'state', status}
elseif op == 'cancel' then
  if status == 'scanned' then
    if owner ~= ARGV[2] then return {'other', status} end
    redis.call('HSET', key, 'status', 'cancelled')
    return {'ok', 'cancelled'}
  end
  if status == 'cancelled' and owner == ARGV[2] then return {'ok', status} end
  return {'state', status}
elseif op == 'consume' then
  if (redis.call('HGET', key, 'pollHash') or '') ~= ARGV[3] then return {'token', status} end
  if status == 'confirmed' then
    redis.call('HSET', key, 'status', 'consumed')
    return {'ok', 'consumed'}
  end
  return {'state', status}
end
return {'state', status}
`)

func (r *SessionRepository) qrLoginKey(appID int64, ticketID string) string {
	return fmt.Sprintf("%s:auth:qr:%d:%s", r.keyPrefix, appID, ticketID)
}

// CreateQRLoginTicket 写入一张新票据并设置 TTL。
func (r *SessionRepository) CreateQRLoginTicket(ctx context.Context, ticket QRLoginTicket, ttl time.Duration) error {
	key := r.qrLoginKey(ticket.AppID, ticket.TicketID)
	pipe := r.client.TxPipeline()
	pipe.HSet(ctx, key, map[string]any{
		"ticketId":  ticket.TicketID,
		"appId":     strconv.FormatInt(ticket.AppID, 10),
		"status":    ticket.Status,
		"pollHash":  ticket.PollHash,
		"deviceId":  ticket.DeviceID,
		"device":    ticket.Device,
		"ip":        ticket.IP,
		"userAgent": ticket.UserAgent,
		"location":  ticket.Location,
		"createdAt": strconv.FormatInt(ticket.CreatedAt.Unix(), 10),
		"expiresAt": strconv.FormatInt(ticket.ExpiresAt.Unix(), 10),
	})
	pipe.Expire(ctx, key, ttl)
	_, err := pipe.Exec(ctx)
	return err
}

// GetQRLoginTicket 读取票据；不存在（含已过期）时返回 nil, nil。
func (r *SessionRepository) GetQRLoginTicket(ctx context.Context, appID int64, ticketID string) (*QRLoginTicket, error) {
	values, err := r.client.HGetAll(ctx, r.qrLoginKey(appID, ticketID)).Result()
	if err != nil {
		return nil, err
	}
	if len(values) == 0 {
		return nil, nil
	}
	parseInt := func(name string) int64 {
		n, _ := strconv.ParseInt(values[name], 10, 64)
		return n
	}
	return &QRLoginTicket{
		TicketID:      values["ticketId"],
		AppID:         parseInt("appId"),
		Status:        values["status"],
		PollHash:      values["pollHash"],
		DeviceID:      values["deviceId"],
		Device:        values["device"],
		IP:            values["ip"],
		UserAgent:     values["userAgent"],
		Location:      values["location"],
		UserID:        parseInt("userId"),
		ScannerName:   values["scannerName"],
		ScannerAvatar: values["scannerAvatar"],
		CreatedAt:     time.Unix(parseInt("createdAt"), 0).UTC(),
		ExpiresAt:     time.Unix(parseInt("expiresAt"), 0).UTC(),
	}, nil
}

// QRLoginTransitionInput 一次状态迁移的参数。op 取 scan / confirm / cancel / consume。
type QRLoginTransitionInput struct {
	AppID         int64
	TicketID      string
	Op            string
	UserID        int64
	PollHash      string
	ScannerName   string
	ScannerAvatar string
	Now           time.Time
}

// TransitionQRLoginTicket 原子地推进票据状态，返回迁移结果与迁移后（或拒绝时）的状态。
func (r *SessionRepository) TransitionQRLoginTicket(ctx context.Context, input QRLoginTransitionInput) (QRLoginTransition, string, error) {
	raw, err := qrLoginTransitionScript.Run(ctx, r.client,
		[]string{r.qrLoginKey(input.AppID, input.TicketID)},
		input.Op,
		strconv.FormatInt(input.UserID, 10),
		input.PollHash,
		input.ScannerName,
		input.ScannerAvatar,
		strconv.FormatInt(input.Now.Unix(), 10),
	).StringSlice()
	if err != nil {
		return "", "", err
	}
	if len(raw) != 2 {
		return "", "", fmt.Errorf("qr login transition: unexpected script result %v", raw)
	}
	return QRLoginTransition(raw[0]), raw[1], nil
}
