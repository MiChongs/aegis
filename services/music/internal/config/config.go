// Package config 音乐个性化服务的进程配置，全部来自环境变量（前缀 MUSIC_）。
// 三个入口（music-rec / music-ingest / music-trainer）共用一份，各取所需。
package config

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Config 进程配置。
type Config struct {
	Env        string
	InstanceID string
	Version    string

	// RPCAddr music-rec 的内部端口：Connect / gRPC、管理端 HTTP /v1、/metrics、健康检查。
	RPCAddr string
	// MetricsAddr ingest / trainer 的 /metrics 与健康检查。
	MetricsAddr string
	// InternalToken 主服务调用时带的令牌；为空时一切调用被拒。
	InternalToken string

	PGDSN string

	ClickHouseAddr     []string
	ClickHouseDB       string
	ClickHouseUser     string
	ClickHousePassword string

	RedisURL    string
	RedisPrefix string
	NATSURL     string

	// IngestBatch / IngestWait 落地批量：攒够条数或等够时间就写一批。
	IngestBatch int
	IngestWait  time.Duration

	// TrainWorkers 训练并行度（ALS 按用户 / 作品分片）。
	TrainWorkers int
	// TrainTimeout 单次训练上限。
	TrainTimeout time.Duration

	// ModelPoll music-rec 兜底轮询已发布模型的间隔（正常靠 NATS 通知）。
	ModelPoll time.Duration

	// TZ 按天 / 按小时统计、每日推荐换天用的时区。
	TZ string

	OTLPEndpoint string

	// 第三期：内容理解与曲库扩展用的大模型（OpenAI 兼容；为空时关闭，docs/music-personalization-3.md）
	LLMBaseURL     string
	LLMAPIKey      string
	LLMModel       string
	EmbedModel     string
	EmbedDim       int
	LLMConcurrency int
}

// Load 读取环境变量。
func Load() (Config, error) {
	v := viper.New()
	v.SetEnvPrefix("MUSIC")
	v.AutomaticEnv()
	v.SetDefault("ENV", "production")
	v.SetDefault("RPC_ADDR", ":8096")
	v.SetDefault("METRICS_ADDR", ":8097")
	v.SetDefault("CLICKHOUSE_DB", "aegis_music")
	v.SetDefault("CLICKHOUSE_USER", "default")
	v.SetDefault("REDIS_PREFIX", "mu:")
	v.SetDefault("INGEST_BATCH", 500)
	v.SetDefault("INGEST_WAIT", "1s")
	v.SetDefault("TRAIN_WORKERS", runtime.NumCPU())
	v.SetDefault("TRAIN_TIMEOUT", "45m")
	v.SetDefault("MODEL_POLL", "2m")
	v.SetDefault("TZ", "Asia/Shanghai")
	v.SetDefault("LLM_MODEL", "gpt-4o-mini")
	v.SetDefault("EMBED_MODEL", "text-embedding-3-small")
	v.SetDefault("EMBED_DIM", 256)
	v.SetDefault("LLM_CONCURRENCY", 2)

	cfg := Config{
		Env:                v.GetString("ENV"),
		InstanceID:         strings.TrimSpace(v.GetString("INSTANCE_ID")),
		Version:            strings.TrimSpace(v.GetString("VERSION")),
		RPCAddr:            v.GetString("RPC_ADDR"),
		MetricsAddr:        v.GetString("METRICS_ADDR"),
		InternalToken:      strings.TrimSpace(v.GetString("INTERNAL_TOKEN")),
		PGDSN:              strings.TrimSpace(v.GetString("PG_DSN")),
		ClickHouseAddr:     splitList(v.GetString("CLICKHOUSE_ADDR")),
		ClickHouseDB:       v.GetString("CLICKHOUSE_DB"),
		ClickHouseUser:     v.GetString("CLICKHOUSE_USER"),
		ClickHousePassword: v.GetString("CLICKHOUSE_PASSWORD"),
		RedisURL:           strings.TrimSpace(v.GetString("REDIS_URL")),
		RedisPrefix:        v.GetString("REDIS_PREFIX"),
		NATSURL:            strings.TrimSpace(v.GetString("NATS_URL")),
		IngestBatch:        v.GetInt("INGEST_BATCH"),
		IngestWait:         v.GetDuration("INGEST_WAIT"),
		TrainWorkers:       v.GetInt("TRAIN_WORKERS"),
		TrainTimeout:       v.GetDuration("TRAIN_TIMEOUT"),
		ModelPoll:          v.GetDuration("MODEL_POLL"),
		TZ:                 strings.TrimSpace(v.GetString("TZ")),
		OTLPEndpoint:       strings.TrimSpace(v.GetString("OTLP_ENDPOINT")),
		LLMBaseURL:         strings.TrimRight(strings.TrimSpace(v.GetString("LLM_BASE_URL")), "/"),
		LLMAPIKey:          strings.TrimSpace(v.GetString("LLM_API_KEY")),
		LLMModel:           strings.TrimSpace(v.GetString("LLM_MODEL")),
		EmbedModel:         strings.TrimSpace(v.GetString("EMBED_MODEL")),
		EmbedDim:           v.GetInt("EMBED_DIM"),
		LLMConcurrency:     v.GetInt("LLM_CONCURRENCY"),
	}
	if cfg.EmbedDim < 16 || cfg.EmbedDim > 4096 {
		cfg.EmbedDim = 256
	}
	if cfg.LLMConcurrency <= 0 || cfg.LLMConcurrency > 32 {
		cfg.LLMConcurrency = 2
	}
	if cfg.InstanceID == "" {
		host, _ := os.Hostname()
		buf := make([]byte, 3)
		_, _ = rand.Read(buf)
		cfg.InstanceID = host + "-" + hex.EncodeToString(buf)
	}
	if cfg.Version == "" {
		cfg.Version = "dev"
	}
	if cfg.IngestBatch <= 0 || cfg.IngestBatch > 5000 {
		cfg.IngestBatch = 500
	}
	if cfg.IngestWait <= 0 {
		cfg.IngestWait = time.Second
	}
	if cfg.TrainWorkers <= 0 {
		cfg.TrainWorkers = runtime.NumCPU()
	}
	if cfg.ModelPoll < 10*time.Second {
		cfg.ModelPoll = 2 * time.Minute
	}
	return cfg, nil
}

// Validate 三个入口都要的依赖。
func (c Config) Validate() error {
	var errs []error
	if c.PGDSN == "" {
		errs = append(errs, errors.New("MUSIC_PG_DSN 未配置"))
	}
	if len(c.ClickHouseAddr) == 0 {
		errs = append(errs, errors.New("MUSIC_CLICKHOUSE_ADDR 未配置"))
	}
	if c.RedisURL == "" {
		errs = append(errs, errors.New("MUSIC_REDIS_URL 未配置"))
	}
	if c.NATSURL == "" {
		errs = append(errs, errors.New("MUSIC_NATS_URL 未配置"))
	}
	if len(errs) > 0 {
		return fmt.Errorf("音乐服务配置不完整：%w", errors.Join(errs...))
	}
	return nil
}

// Development 开发环境（日志格式）。
func (c Config) Development() bool { return c.Env == "development" || c.Env == "dev" }

func splitList(s string) []string {
	var out []string
	for _, item := range strings.Split(s, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}
