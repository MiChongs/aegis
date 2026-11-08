// Package config 风控各服务共用的环境变量配置（deploy/docker/docker-compose.yml 的 x-risk-env）。
package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// Config 连接配置
type Config struct {
	EtcdEndpoints []string

	RedisAddrs    []string
	RedisPassword string
	// RedisAddrMap 本机调试：集群公告的是容器主机名，这里把它映射成宿主机地址（risk-redis-1:7001=127.0.0.1:7001,…）
	RedisAddrMap map[string]string

	PGDSN      string
	PGAdminDSN string // 建库用（连主库）

	NATSURL string

	ClickHouseAddr     string
	ClickHouseUser     string
	ClickHousePassword string
	ClickHouseDB       string

	NebulaAddrs    []string
	NebulaUser     string
	NebulaPassword string
	NebulaSpace    string
	NebulaStoraged []string

	MilvusAddr string

	RisingWaveDSN string

	S3Endpoint  string
	S3AccessKey string
	S3SecretKey string
	S3Bucket    string
	S3UseSSL    bool

	InternalToken string
	AdminURL      string

	DecisionAddr string
	DecisionHTTP string
	AdminAddr    string
	SpoolDir     string
	Instance     string
}

// Load 读环境变量
func Load() Config {
	host, _ := os.Hostname()
	return Config{
		EtcdEndpoints:      list("RISK_ETCD_ENDPOINTS", "127.0.0.1:2379"),
		RedisAddrs:         list("RISK_REDIS_ADDRS", "127.0.0.1:7001"),
		RedisPassword:      os.Getenv("RISK_REDIS_PASSWORD"),
		RedisAddrMap:       pairs("RISK_REDIS_ADDR_MAP"),
		PGDSN:              str("RISK_PG_DSN", "postgres://aegis:aegis@127.0.0.1:15432/aegis_risk?sslmode=disable"),
		PGAdminDSN:         os.Getenv("RISK_PG_ADMIN_DSN"),
		NATSURL:            str("RISK_NATS_URL", "nats://127.0.0.1:4222"),
		ClickHouseAddr:     str("RISK_CLICKHOUSE_ADDR", "127.0.0.1:19000"),
		ClickHouseUser:     str("RISK_CLICKHOUSE_USER", "aegis"),
		ClickHousePassword: os.Getenv("RISK_CLICKHOUSE_PASSWORD"),
		ClickHouseDB:       str("RISK_CLICKHOUSE_DB", "aegis_risk"),
		NebulaAddrs:        list("RISK_NEBULA_ADDRS", "127.0.0.1:9669"),
		NebulaUser:         str("RISK_NEBULA_USER", "root"),
		NebulaPassword:     str("RISK_NEBULA_PASSWORD", "nebula"),
		NebulaSpace:        str("RISK_NEBULA_SPACE", "aegis_risk"),
		NebulaStoraged:     list("RISK_NEBULA_STORAGED", ""),
		MilvusAddr:         str("RISK_MILVUS_ADDR", "127.0.0.1:19530"),
		RisingWaveDSN:      str("RISK_RISINGWAVE_DSN", "postgres://root@127.0.0.1:4566/dev?sslmode=disable"),
		S3Endpoint:         str("RISK_S3_ENDPOINT", "127.0.0.1:19100"),
		S3AccessKey:        str("RISK_S3_ACCESS_KEY", "minioadmin"),
		S3SecretKey:        str("RISK_S3_SECRET_KEY", "minioadmin"),
		S3Bucket:           str("RISK_S3_BUCKET", "aegis-risk"),
		S3UseSSL:           os.Getenv("RISK_S3_SSL") == "true",
		InternalToken:      os.Getenv("RISK_INTERNAL_TOKEN"),
		AdminURL:           strings.TrimRight(str("RISK_ADMIN_URL", "http://127.0.0.1:8891"), "/"),
		DecisionAddr:       str("RISK_DECISION_ADDR", "0.0.0.0:8890"),
		DecisionHTTP:       str("RISK_DECISION_HTTP_ADDR", "0.0.0.0:8892"),
		AdminAddr:          str("RISK_ADMIN_ADDR", "0.0.0.0:8891"),
		SpoolDir:           str("RISK_SPOOL_DIR", "/var/lib/aegis-risk"),
		Instance:           str("RISK_INSTANCE", host),
	}
}

func str(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func list(key, def string) []string {
	raw := str(key, def)
	var out []string
	for _, p := range strings.Split(raw, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func pairs(key string) map[string]string {
	out := map[string]string{}
	for _, p := range list(key, "") {
		if k, v, ok := strings.Cut(p, "="); ok {
			out[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return out
}

// Duration 读一个时长环境变量
func Duration(key string, def time.Duration) time.Duration {
	if v, err := time.ParseDuration(os.Getenv(key)); err == nil && v > 0 {
		return v
	}
	return def
}

// Int 读一个整数环境变量
func Int(key string, def int) int {
	if v, err := strconv.Atoi(os.Getenv(key)); err == nil {
		return v
	}
	return def
}
