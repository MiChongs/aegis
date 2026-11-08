// Package redisx Redis Cluster 客户端
package redisx

import (
	"context"
	"net"
	"time"

	"aegis/services/risk/internal/config"

	"github.com/redis/go-redis/v9"
)

// New 连 Redis Cluster。本机调试时集群节点公告的是容器主机名，按 RedisAddrMap 改拨宿主机端口。
func New(cfg config.Config) *redis.ClusterClient {
	dialer := &net.Dialer{Timeout: 2 * time.Second, KeepAlive: 30 * time.Second}
	opts := &redis.ClusterOptions{
		Addrs:                 cfg.RedisAddrs,
		Password:              cfg.RedisPassword,
		ReadOnly:              false,
		RouteByLatency:        false,
		DialTimeout:           2 * time.Second,
		ReadTimeout:           500 * time.Millisecond,
		WriteTimeout:          500 * time.Millisecond,
		PoolSize:              64,
		MinIdleConns:          4,
		MaxRedirects:          4,
		ContextTimeoutEnabled: true,
	}
	if len(cfg.RedisAddrMap) > 0 {
		remap := cfg.RedisAddrMap
		opts.Dialer = func(ctx context.Context, network, addr string) (net.Conn, error) {
			if to, ok := remap[addr]; ok {
				addr = to
			}
			return dialer.DialContext(ctx, network, addr)
		}
	}
	return redis.NewClusterClient(opts)
}
