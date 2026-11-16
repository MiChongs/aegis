package config

import (
	"strings"

	"github.com/spf13/viper"
)

// MusicConfig 音乐个性化（独立微服务 services/music）在主服务这一侧的配置。
//
// 主服务只做三件事：校验客户端上报的行为事件并发布到 JetStream（MUSIC 流）、把推荐 / 画像 / 清除数据
// 经内部 RPC 转给 music-rec、把控制台的管理接口反向代理到 music-rec。详见 docs/music-personalization.md。
//
// 相关环境变量：
//
//	MUSIC_RPC_URL         music-rec 的内部地址（Connect + 管理端 HTTP），如 http://music-rec:8096；为空时推荐、画像与管理端回 50396
//	MUSIC_INTERNAL_TOKEN  内部接口令牌（与音乐服务的 MUSIC_INTERNAL_TOKEN 一致）
//	MUSIC_EVENTS_ENABLED  上报总开关，默认 true；关掉后上报接口直接 200 且不发布
type MusicConfig struct {
	RPCURL        string
	InternalToken string
	EventsEnabled bool
}

// Enabled 是否接入了音乐服务。
func (c MusicConfig) Enabled() bool { return c.RPCURL != "" }

func loadMusicConfig(v *viper.Viper) MusicConfig {
	v.SetDefault("MUSIC_EVENTS_ENABLED", true)
	return MusicConfig{
		RPCURL:        strings.TrimRight(strings.TrimSpace(v.GetString("MUSIC_RPC_URL")), "/"),
		InternalToken: strings.TrimSpace(v.GetString("MUSIC_INTERNAL_TOKEN")),
		EventsEnabled: v.GetBool("MUSIC_EVENTS_ENABLED"),
	}
}
