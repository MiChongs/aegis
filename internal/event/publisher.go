package event

import (
	"context"
	"encoding/json"

	"github.com/nats-io/nats.go"
)

type Publisher struct {
	js   nats.JetStreamContext
	conn *nats.Conn
}

func NewPublisher(js nats.JetStreamContext) *Publisher {
	return &Publisher{js: js}
}

// SetConn 注入原生 NATS 连接（用于 Fire-and-forget 广播）
func (p *Publisher) SetConn(conn *nats.Conn) {
	p.conn = conn
}

// PublishJSON 通过 JetStream 发布（持久化，等待 ACK）
func (p *Publisher) PublishJSON(_ context.Context, subject string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = p.js.Publish(subject, data)
	return err
}

// PublishFire 通过原生 NATS 发布（fire-and-forget，微秒级，无持久化）
// 用于临时广播事件（如系统公告），不需要 JetStream 存储保证
func (p *Publisher) PublishFire(_ context.Context, subject string, payload any) error {
	if p.conn == nil {
		return p.PublishJSON(nil, subject, payload) // fallback
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return p.conn.Publish(subject, data)
}
