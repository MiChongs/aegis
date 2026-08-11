package service

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	realtimedomain "aegis/internal/domain/realtime"
	"aegis/internal/event"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"go.uber.org/zap"
)

// natsUserEventPublisher 只发布、不持有 WebSocket 连接的实时事件发布器。
//
// 为什么需要它：Worker 进程没有任何客户端连着自己，但它产生的事件
// （SLA 预警 / 超时）必须送达连在 API 实例上的管理员。
// RealtimeService 已经订阅了 `realtime.user.*.*` 并把消息分发给本地连接，
// 因此 Worker 只要把消息投进同一个 NATS 主题即可 —— 载荷格式必须与
// RealtimeService.PublishUserEvent 完全一致，否则前端解不出来。
type natsUserEventPublisher struct {
	log  *zap.Logger
	conn *nats.Conn
}

// NewNATSUserEventPublisher 构造一个仅发布的实时事件发布器。
// conn 为 nil 时退化为 no-op（不影响主流程，仅记 debug 日志）。
func NewNATSUserEventPublisher(log *zap.Logger, conn *nats.Conn) UserEventPublisher {
	if log == nil {
		log = zap.NewNop()
	}
	return &natsUserEventPublisher{log: log, conn: conn}
}

func (p *natsUserEventPublisher) PublishUserEvent(ctx context.Context, appID int64, userID int64, eventType string, data map[string]any) error {
	if p == nil || p.conn == nil || !p.conn.IsConnected() {
		// 没有 NATS 时静默跳过：实时推送是增强项，不能反过来拖垮 SLA 巡检
		p.log.Debug("NATS 不可用，跳过实时推送",
			zap.String("event", eventType), zap.Int64("appid", appID), zap.Int64("userId", userID))
		return nil
	}
	payload, err := json.Marshal(realtimedomain.Event{
		ID:        uuid.NewString(),
		Type:      strings.TrimSpace(eventType),
		AppID:     appID,
		UserID:    userID,
		Timestamp: time.Now().UTC(),
		Data:      data,
	})
	if err != nil {
		return err
	}
	return p.conn.Publish(event.SubjectRealtimeUser(appID, userID), payload)
}
