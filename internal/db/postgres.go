package db

import (
	"context"
	"strings"
	"time"

	"aegis/internal/config"
	"aegis/pkg/timeutil"
	"github.com/jackc/pgx/v5/pgxpool"
)

func NewPostgres(ctx context.Context, cfg config.PostgresConfig) (*pgxpool.Pool, error) {
	poolCfg, err := pgxpool.ParseConfig(cfg.DSN)
	if err != nil {
		return nil, err
	}
	poolCfg.MaxConns = cfg.MaxConns
	poolCfg.MinConns = cfg.MinConns
	poolCfg.MaxConnLifetime = cfg.MaxConnLifetime
	poolCfg.MaxConnIdleTime = 5 * time.Minute
	if poolCfg.ConnConfig.RuntimeParams == nil {
		poolCfg.ConnConfig.RuntimeParams = map[string]string{}
	}
	sessionTimezone := strings.TrimSpace(cfg.SessionTimezone)
	if sessionTimezone == "" {
		sessionTimezone = "UTC"
	}
	if loc, err := timeutil.LoadLocation(sessionTimezone); err == nil {
		sessionTimezone = loc.String()
	}
	poolCfg.ConnConfig.RuntimeParams["timezone"] = sessionTimezone
	return pgxpool.NewWithConfig(ctx, poolCfg)
}
