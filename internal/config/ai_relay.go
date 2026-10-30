package config

import (
	"strings"
	"time"

	"github.com/spf13/viper"
)

// AIRelayConfig 用户 AI 中转（/api/ai/v1，docs/ai-gateway.md）的传输层参数。
//
// 环境变量：
//
//	AI_RELAY_MAX_BODY_BYTES   请求体上限（字节），默认 32 MiB。Agent 多轮对话带着工具结果与截图，
//	                          历史越滚越大，8 MiB 的旧上限会让后面的轮次直接 400。
//	AI_RELAY_READ_TIMEOUT     读完请求体的期限，默认 5m。全局 READ_TIMEOUT（5s）挡慢速攻击，
//	                          却会让手机上行慢的大请求体读到一半被掐断。
//	AI_UPSTREAM_IDLE_TIMEOUT  上游响应开始后连续多久收不到一个字节就判上游卡死、主动断开，
//	                          默认 5m；填 0 关闭。推理模型思考时可能长时间不出字，别设得太短。
type AIRelayConfig struct {
	MaxBodyBytes        int64
	ReadTimeout         time.Duration
	UpstreamIdleTimeout time.Duration
}

const (
	defaultAIRelayMaxBodyBytes        = 32 << 20
	defaultAIRelayReadTimeout         = 5 * time.Minute
	defaultAIRelayUpstreamIdleTimeout = 5 * time.Minute
)

func loadAIRelayConfig(v *viper.Viper) AIRelayConfig {
	cfg := AIRelayConfig{
		MaxBodyBytes:        v.GetInt64("AI_RELAY_MAX_BODY_BYTES"),
		ReadTimeout:         getDuration(v, "AI_RELAY_READ_TIMEOUT", defaultAIRelayReadTimeout),
		UpstreamIdleTimeout: defaultAIRelayUpstreamIdleTimeout,
	}
	if cfg.MaxBodyBytes <= 0 {
		cfg.MaxBodyBytes = defaultAIRelayMaxBodyBytes
	}
	// 0 是「关闭」而不是「用缺省」，getDuration 区分不了，这里自己读。
	if v.IsSet("AI_UPSTREAM_IDLE_TIMEOUT") {
		raw := strings.TrimSpace(v.GetString("AI_UPSTREAM_IDLE_TIMEOUT"))
		if raw == "0" {
			cfg.UpstreamIdleTimeout = 0
		} else if d := v.GetDuration("AI_UPSTREAM_IDLE_TIMEOUT"); d > 0 {
			cfg.UpstreamIdleTimeout = d
		}
	}
	return cfg
}
