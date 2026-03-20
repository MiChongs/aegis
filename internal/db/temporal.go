package db

import (
	"aegis/internal/config"
	pkglogger "aegis/pkg/logger"
	"time"

	"go.temporal.io/sdk/client"
	"go.uber.org/zap"
)

func NewTemporal(cfg config.TemporalConfig, logger *zap.Logger) (client.Client, error) {
	return client.Dial(client.Options{
		HostPort:                cfg.HostPort,
		Namespace:               cfg.Namespace,
		Logger:                  pkglogger.NewTemporalLogger(logger),
		WorkerHeartbeatInterval: -1 * time.Second,
	})
}
