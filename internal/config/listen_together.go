package config

import (
	"strings"
	"time"

	"github.com/spf13/viper"
)

// ListenTogetherConfig 一起听（独立微服务 services/listentogether）在主服务这一侧的配置。
//
// 主服务只做三件事：给有 listen_together 权益的用户签票据、发布票据公钥（JWKS）、
// 把控制台的管理操作经内部 RPC 转给一起听服务。一起听的长连接与房间状态都不在主服务进程里。
// 详见 docs/listen-together.md。
//
// 相关环境变量：
//
//	LISTEN_TOGETHER_ENDPOINTS       逗号分隔的 wss 地址，客户端按顺序尝试（为空表示未开通，签票返回 50360）
//	LISTEN_TOGETHER_TICKET_KEYS     逗号分隔的 Ed25519 种子（base64，32 字节），第一把签名、其余只在 JWKS 里供轮换；
//	                                为空时从 JWT_SECRET 派生，多实例无需额外配置
//	LISTEN_TOGETHER_TICKET_TTL      票据有效期，默认 10m
//	LISTEN_TOGETHER_RPC_URL         一起听服务的内部地址（Connect/gRPC），如 http://listen-together:8091
//	LISTEN_TOGETHER_INTERNAL_TOKEN  调内部接口的令牌（与一起听服务的 LT_INTERNAL_TOKEN 一致）
type ListenTogetherConfig struct {
	Endpoints     []string
	TicketKeys    string
	TicketTTL     time.Duration
	RPCURL        string
	InternalToken string
	// DeriveSecret TicketKeys 为空时用来派生签名钥匙的主服务密钥（JWT_SECRET）。
	DeriveSecret string
}

// Enabled 是否开通（配置了客户端可连的地址）。
func (c ListenTogetherConfig) Enabled() bool { return len(c.Endpoints) > 0 }

func loadListenTogetherConfig(v *viper.Viper, jwtSecret string) ListenTogetherConfig {
	cfg := ListenTogetherConfig{
		TicketKeys:    strings.TrimSpace(v.GetString("LISTEN_TOGETHER_TICKET_KEYS")),
		TicketTTL:     v.GetDuration("LISTEN_TOGETHER_TICKET_TTL"),
		RPCURL:        strings.TrimRight(strings.TrimSpace(v.GetString("LISTEN_TOGETHER_RPC_URL")), "/"),
		InternalToken: strings.TrimSpace(v.GetString("LISTEN_TOGETHER_INTERNAL_TOKEN")),
		DeriveSecret:  jwtSecret,
	}
	for _, item := range strings.Split(v.GetString("LISTEN_TOGETHER_ENDPOINTS"), ",") {
		item = strings.TrimSpace(item)
		if strings.HasPrefix(item, "wss://") || strings.HasPrefix(item, "ws://") {
			cfg.Endpoints = append(cfg.Endpoints, item)
		}
	}
	if cfg.TicketTTL <= 0 {
		cfg.TicketTTL = 10 * time.Minute
	}
	if cfg.TicketTTL < time.Minute {
		cfg.TicketTTL = time.Minute
	}
	if cfg.TicketTTL > time.Hour {
		cfg.TicketTTL = time.Hour
	}
	return cfg
}
