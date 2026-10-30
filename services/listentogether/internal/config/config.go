// Package config 一起听服务的配置，全部来自环境变量（前缀 LT_），十二要素式部署。
package config

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Config 一起听服务配置。
type Config struct {
	// Env development / production；影响日志格式。
	Env string
	// InstanceID 实例标识；为空时用主机名加随机后缀。多实例部署时用于在线判定与连接路由。
	InstanceID string
	Version    string

	// PublicAddr 对外 WebSocket 与健康检查（终端用户连这里，前面挂 CDN / 反代）。
	PublicAddr string
	// InternalAddr 内部管理接口（Connect / gRPC）与 Prometheus 指标，只在内网暴露。
	InternalAddr string
	// InternalToken 主服务调内部接口时带的 Bearer 令牌；为空时内部接口拒绝一切调用。
	InternalToken string

	// RedisURL 房间状态（redis://user:pass@host:6379/0）。为空时用进程内存储，只适合单实例与本地开发。
	RedisURL string
	// RedisPrefix 键前缀，与主服务共用一个 Redis 时避免冲突。
	RedisPrefix string
	// NATSURL 跨实例事件总线。为空时用进程内总线，只适合单实例。
	NATSURL string

	// TicketJWKSURL 主服务发布票据公钥的地址，如 http://aegis:8088/api/listen-together/jwks.json。
	TicketJWKSURL string
	// TicketJWKS 直接给出 JWKS 文本（与 URL 二选一；离线部署与测试用）。
	TicketJWKS string
	// TicketLeeway 验票时容忍的时钟差。
	TicketLeeway time.Duration
	// TicketGrace 票据过期后还允许连接存活多久等客户端续期。
	TicketGrace time.Duration

	// AllowedOrigins 浏览器来源白名单（* 为不限）；原生客户端不带 Origin，不受影响。
	AllowedOrigins []string

	// RoomCapacity 每个房间默认人数上限；RoomCapacityMax 房主可调到的最大值。
	RoomCapacity    int
	RoomCapacityMax int
	// QueueLimit 共享队列最多多少首（房主本地队列更长时按当前位置截一段）。
	QueueLimit int
	// ChatHistory 房间保留的最近聊天条数。
	ChatHistory int
	// MemberGrace 成员掉线后保留席位多久，过后移出房间（房主则转交）。
	MemberGrace time.Duration
	// RoomIdleTTL 房间里没有在线成员多久后解散。
	RoomIdleTTL time.Duration
	// MaxConnsPerInstance 单实例连接上限，超出时拒绝新连接让负载均衡换实例。
	MaxConnsPerInstance int

	// OTLPEndpoint OpenTelemetry 链路上报地址（为空则不上报）。
	OTLPEndpoint string
}

// Load 读取环境变量并校验。
func Load() (Config, error) {
	v := viper.New()
	v.SetEnvPrefix("LT")
	v.AutomaticEnv()
	v.SetDefault("ENV", "production")
	v.SetDefault("PUBLIC_ADDR", ":8090")
	v.SetDefault("INTERNAL_ADDR", ":8091")
	v.SetDefault("REDIS_PREFIX", "lt:")
	v.SetDefault("TICKET_LEEWAY", "30s")
	v.SetDefault("TICKET_GRACE", "60s")
	v.SetDefault("ALLOWED_ORIGINS", "*")
	v.SetDefault("ROOM_CAPACITY", 20)
	v.SetDefault("ROOM_CAPACITY_MAX", 50)
	v.SetDefault("QUEUE_LIMIT", 200)
	v.SetDefault("CHAT_HISTORY", 50)
	v.SetDefault("MEMBER_GRACE", "3m")
	v.SetDefault("ROOM_IDLE_TTL", "10m")
	v.SetDefault("MAX_CONNS_PER_INSTANCE", 20000)

	cfg := Config{
		Env:                 v.GetString("ENV"),
		InstanceID:          strings.TrimSpace(v.GetString("INSTANCE_ID")),
		Version:             strings.TrimSpace(v.GetString("VERSION")),
		PublicAddr:          v.GetString("PUBLIC_ADDR"),
		InternalAddr:        v.GetString("INTERNAL_ADDR"),
		InternalToken:       strings.TrimSpace(v.GetString("INTERNAL_TOKEN")),
		RedisURL:            strings.TrimSpace(v.GetString("REDIS_URL")),
		RedisPrefix:         v.GetString("REDIS_PREFIX"),
		NATSURL:             strings.TrimSpace(v.GetString("NATS_URL")),
		TicketJWKSURL:       strings.TrimSpace(v.GetString("TICKET_JWKS_URL")),
		TicketJWKS:          strings.TrimSpace(v.GetString("TICKET_JWKS")),
		TicketLeeway:        v.GetDuration("TICKET_LEEWAY"),
		TicketGrace:         v.GetDuration("TICKET_GRACE"),
		AllowedOrigins:      splitList(v.GetString("ALLOWED_ORIGINS")),
		RoomCapacity:        v.GetInt("ROOM_CAPACITY"),
		RoomCapacityMax:     v.GetInt("ROOM_CAPACITY_MAX"),
		QueueLimit:          v.GetInt("QUEUE_LIMIT"),
		ChatHistory:         v.GetInt("CHAT_HISTORY"),
		MemberGrace:         v.GetDuration("MEMBER_GRACE"),
		RoomIdleTTL:         v.GetDuration("ROOM_IDLE_TTL"),
		MaxConnsPerInstance: v.GetInt("MAX_CONNS_PER_INSTANCE"),
		OTLPEndpoint:        strings.TrimSpace(v.GetString("OTLP_ENDPOINT")),
	}
	if cfg.Version == "" {
		cfg.Version = "dev"
	}
	if cfg.InstanceID == "" {
		cfg.InstanceID = defaultInstanceID()
	}
	return cfg, cfg.Validate()
}

// Validate 配置自检：错的配置应当启动即失败，而不是跑起来以后每个连接都失败。
func (c Config) Validate() error {
	var problems []string
	if c.TicketJWKSURL == "" && c.TicketJWKS == "" {
		problems = append(problems, "LT_TICKET_JWKS_URL 或 LT_TICKET_JWKS 至少配置一项")
	}
	if c.RoomCapacity < 2 || c.RoomCapacityMax < c.RoomCapacity || c.RoomCapacityMax > 500 {
		problems = append(problems, "LT_ROOM_CAPACITY 须 ≥2 且不超过 LT_ROOM_CAPACITY_MAX（≤500）")
	}
	if c.QueueLimit < 1 || c.QueueLimit > 2000 {
		problems = append(problems, "LT_QUEUE_LIMIT 须在 1–2000")
	}
	if c.ChatHistory < 0 || c.ChatHistory > 500 {
		problems = append(problems, "LT_CHAT_HISTORY 须在 0–500")
	}
	if c.MemberGrace < 10*time.Second || c.RoomIdleTTL < time.Minute {
		problems = append(problems, "LT_MEMBER_GRACE ≥10s、LT_ROOM_IDLE_TTL ≥1m")
	}
	if c.PublicAddr == c.InternalAddr {
		problems = append(problems, "LT_PUBLIC_ADDR 与 LT_INTERNAL_ADDR 不能相同：内部接口不能暴露到公网")
	}
	if len(problems) > 0 {
		return errors.New("一起听服务配置无效：" + strings.Join(problems, "；"))
	}
	return nil
}

// Development 是否开发环境。
func (c Config) Development() bool {
	return strings.EqualFold(c.Env, "development") || strings.EqualFold(c.Env, "dev")
}

func splitList(raw string) []string {
	var out []string
	for _, item := range strings.Split(raw, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

func defaultInstanceID() string {
	host, _ := os.Hostname()
	if host == "" {
		host = "lt"
	}
	suffix := make([]byte, 3)
	_, _ = rand.Read(suffix)
	return fmt.Sprintf("%s-%s", host, hex.EncodeToString(suffix))
}
