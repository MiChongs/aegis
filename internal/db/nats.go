package db

import (
	"context"
	"time"

	"aegis/internal/config"
	"github.com/nats-io/nats.go"
)

func NewNATS(_ context.Context, cfg config.NATSConfig) (*nats.Conn, nats.JetStreamContext, error) {
	conn, err := nats.Connect(cfg.URL, nats.Name("aegis"), nats.Timeout(5*time.Second))
	if err != nil {
		return nil, nil, err
	}
	js, err := conn.JetStream()
	if err != nil {
		conn.Close()
		return nil, nil, err
	}
	streamCfg := &nats.StreamConfig{
		Name:      cfg.StreamName,
		Subjects:  []string{"auth.>", "user.>", "firewall.>"},
		Retention: nats.LimitsPolicy,
		Storage:   nats.FileStorage,
	}
	if _, err := js.AddStream(streamCfg); err != nil {
		// stream 已存在时尝试更新 subjects
		_, _ = js.UpdateStream(streamCfg)
	}
	return conn, js, nil
}
