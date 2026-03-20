package db

import (
	"context"
	"time"

	"aegis/internal/config"
	redis "github.com/redis/go-redis/v9"
)

func NewRedis(_ context.Context, cfg config.RedisConfig) *redis.Client {
	client := redis.NewClient(&redis.Options{
		Addr:         cfg.Addr,
		Password:     cfg.Password,
		DB:           cfg.DB,
		DialTimeout:  3 * time.Second,
		ReadTimeout:  2 * time.Second,
		WriteTimeout: 2 * time.Second,
	})
	return client
}
