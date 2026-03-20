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
	_, _ = js.AddStream(&nats.StreamConfig{
		Name:      cfg.StreamName,
		Subjects:  []string{"auth.>", "user.>"},
		Retention: nats.LimitsPolicy,
		Storage:   nats.FileStorage,
	})
	return conn, js, nil
}
