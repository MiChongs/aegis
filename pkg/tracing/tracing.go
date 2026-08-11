// Package tracing 封装 OpenTelemetry 分布式追踪的初始化 / 关闭 / 中间件。
//
// 支持三种 Exporter：
//   - otlp-http  (默认)     : OTLP/HTTP to collector endpoint (e.g. http://tempo:4318)
//   - otlp-grpc             : OTLP/gRPC
//   - stdout                : 控制台打印（调试用）
//   - none / ""             : 只在进程内生成 TraceID，不导出
//
// 支持三种 Sampler：
//   - always_on  (默认)     : 全量采样
//   - traceid_ratio         : 按 TRACING_SAMPLE_RATIO (0~1) 概率采样
//   - parentbased_always_on : 继承上游决策、孤儿全采
//
// 传播格式：W3C TraceContext + Baggage。
package tracing

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/gin-gonic/gin"
)

// Config 追踪配置。字段通常由 internal/config 从环境变量加载后传入。
type Config struct {
	Enabled        bool          // 是否启用（默认 true）
	ServiceName    string        // OTel resource service.name（默认 "aegis"）
	ServiceVersion string        // service.version
	Environment    string        // deployment.environment
	Exporter       string        // otlp-http / otlp-grpc / stdout / none
	Endpoint       string        // 如 "http://otel-collector:4318" 或 "otel-collector:4317"
	Insecure       bool          // OTLP 连接是否跳过 TLS
	Headers        map[string]string // OTLP 附加请求头（如 authorization token）
	Sampler        string        // always_on / traceid_ratio / parentbased_always_on
	SampleRatio    float64       // traceid_ratio 下的采样率 0~1
	BatchTimeout   time.Duration // 导出批次超时
	ExportTimeout  time.Duration // 单次导出 deadline
}

// DefaultConfig 给未接入 config 包的场景使用。
func DefaultConfig(serviceName string) Config {
	return Config{
		Enabled:        true,
		ServiceName:    firstNonEmpty(serviceName, "aegis"),
		ServiceVersion: "dev",
		Environment:    "development",
		Exporter:       "none",
		Sampler:        "always_on",
		SampleRatio:    1.0,
		BatchTimeout:   5 * time.Second,
		ExportTimeout:  10 * time.Second,
	}
}

// ConfigFromEnv 从环境变量读取追踪配置，可作为默认入口：
//
//	TRACING_ENABLED           bool, default true
//	TRACING_EXPORTER          otlp-http / otlp-grpc / stdout / none (default otlp-http if endpoint set, else none)
//	TRACING_ENDPOINT          OTLP endpoint
//	TRACING_INSECURE          bool, default true
//	TRACING_HEADERS           逗号分隔 k=v 列表
//	TRACING_SAMPLER           always_on / traceid_ratio / parentbased_always_on
//	TRACING_SAMPLE_RATIO      float 0~1
//	TRACING_SERVICE_NAME      service name override
//	TRACING_SERVICE_VERSION   service version
//	TRACING_ENVIRONMENT       environment name
//	TRACING_BATCH_TIMEOUT     duration (e.g. 5s)
//	TRACING_EXPORT_TIMEOUT    duration (e.g. 10s)
//
// 同时识别 OTEL_* 官方标准变量作为回退：
//
//	OTEL_EXPORTER_OTLP_ENDPOINT / OTEL_EXPORTER_OTLP_HEADERS
//	OTEL_SERVICE_NAME / OTEL_SERVICE_VERSION
//	OTEL_RESOURCE_ATTRIBUTES (deployment.environment=xxx 会被提取)
//	OTEL_TRACES_SAMPLER / OTEL_TRACES_SAMPLER_ARG
func ConfigFromEnv(defaultService string) Config {
	cfg := DefaultConfig(defaultService)

	if v := strings.TrimSpace(os.Getenv("TRACING_ENABLED")); v != "" {
		cfg.Enabled = parseBool(v, cfg.Enabled)
	}
	if v := firstEnv("TRACING_SERVICE_NAME", "OTEL_SERVICE_NAME"); v != "" {
		cfg.ServiceName = v
	}
	if v := firstEnv("TRACING_SERVICE_VERSION", "OTEL_SERVICE_VERSION"); v != "" {
		cfg.ServiceVersion = v
	}
	if v := strings.TrimSpace(os.Getenv("TRACING_ENVIRONMENT")); v != "" {
		cfg.Environment = v
	}
	if attrs := os.Getenv("OTEL_RESOURCE_ATTRIBUTES"); attrs != "" && cfg.Environment == "development" {
		for _, kv := range strings.Split(attrs, ",") {
			p := strings.SplitN(strings.TrimSpace(kv), "=", 2)
			if len(p) == 2 && p[0] == "deployment.environment" {
				cfg.Environment = p[1]
			}
		}
	}

	endpoint := firstEnv("TRACING_ENDPOINT", "OTEL_EXPORTER_OTLP_ENDPOINT", "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT")
	cfg.Endpoint = endpoint

	exporter := strings.ToLower(strings.TrimSpace(os.Getenv("TRACING_EXPORTER")))
	if exporter == "" {
		// 根据 endpoint 自动推断
		if endpoint != "" {
			if strings.HasPrefix(endpoint, "http://") || strings.HasPrefix(endpoint, "https://") {
				exporter = "otlp-http"
			} else {
				exporter = "otlp-grpc"
			}
		} else {
			exporter = "none"
		}
	}
	cfg.Exporter = exporter

	if v := os.Getenv("TRACING_INSECURE"); v != "" {
		cfg.Insecure = parseBool(v, true)
	} else {
		cfg.Insecure = !strings.HasPrefix(endpoint, "https://")
	}

	if h := firstEnv("TRACING_HEADERS", "OTEL_EXPORTER_OTLP_HEADERS"); h != "" {
		cfg.Headers = parseHeaders(h)
	}

	if s := firstEnv("TRACING_SAMPLER", "OTEL_TRACES_SAMPLER"); s != "" {
		cfg.Sampler = strings.ToLower(strings.TrimSpace(s))
	}
	if r := firstEnv("TRACING_SAMPLE_RATIO", "OTEL_TRACES_SAMPLER_ARG"); r != "" {
		if f, err := parseFloat(r); err == nil && f >= 0 && f <= 1 {
			cfg.SampleRatio = f
		}
	}

	if v := os.Getenv("TRACING_BATCH_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.BatchTimeout = d
		}
	}
	if v := os.Getenv("TRACING_EXPORT_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.ExportTimeout = d
		}
	}

	return cfg
}

// ShutdownFunc 关闭 tracer provider。
type ShutdownFunc func(ctx context.Context) error

// Init 初始化全局 TracerProvider + Propagator；返回一个 shutdown 函数。
// 即使 cfg.Enabled=false 或 Exporter=none，也会保证有一个 TracerProvider 存在，
// 以便业务代码始终能 trace.SpanFromContext(ctx) 取到 SpanContext。
func Init(ctx context.Context, cfg Config) (ShutdownFunc, error) {
	if cfg.ServiceName == "" {
		cfg.ServiceName = "aegis"
	}

	res, err := buildResource(ctx, cfg)
	if err != nil {
		return noopShutdown, fmt.Errorf("tracing: build resource: %w", err)
	}

	opts := []sdktrace.TracerProviderOption{
		sdktrace.WithResource(res),
		sdktrace.WithSampler(buildSampler(cfg)),
	}

	// 构造 Exporter（可能为 nil = 不导出）
	exp, err := buildExporter(ctx, cfg)
	if err != nil {
		return noopShutdown, fmt.Errorf("tracing: build exporter: %w", err)
	}
	if exp != nil {
		opts = append(opts, sdktrace.WithBatcher(exp,
			sdktrace.WithBatchTimeout(cfg.BatchTimeout),
			sdktrace.WithExportTimeout(cfg.ExportTimeout),
		))
	}

	tp := sdktrace.NewTracerProvider(opts...)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
	otel.SetErrorHandler(&silentErrorHandler{})

	shutdown := func(sCtx context.Context) error {
		if sCtx == nil {
			sCtx = context.Background()
		}
		return tp.Shutdown(sCtx)
	}
	return shutdown, nil
}

func buildResource(ctx context.Context, cfg Config) (*resource.Resource, error) {
	return resource.New(ctx,
		resource.WithProcess(),
		resource.WithHost(),
		resource.WithOS(),
		resource.WithAttributes(
			semconv.ServiceName(cfg.ServiceName),
			semconv.ServiceVersion(cfg.ServiceVersion),
			attribute.String("deployment.environment", firstNonEmpty(cfg.Environment, "development")),
		),
	)
}

func buildSampler(cfg Config) sdktrace.Sampler {
	switch strings.ToLower(cfg.Sampler) {
	case "always_on", "always-on", "on", "":
		return sdktrace.AlwaysSample()
	case "always_off", "off":
		return sdktrace.NeverSample()
	case "traceid_ratio", "traceidratio", "ratio":
		return sdktrace.TraceIDRatioBased(cfg.SampleRatio)
	case "parentbased_always_on", "parentbased_alwayson":
		return sdktrace.ParentBased(sdktrace.AlwaysSample())
	case "parentbased_traceid_ratio", "parentbased_ratio":
		return sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.SampleRatio))
	default:
		return sdktrace.AlwaysSample()
	}
}

func buildExporter(ctx context.Context, cfg Config) (sdktrace.SpanExporter, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	switch strings.ToLower(cfg.Exporter) {
	case "none", "":
		return nil, nil
	case "stdout":
		return stdouttrace.New(stdouttrace.WithPrettyPrint())
	case "otlp-http", "otlphttp", "http":
		opts := []otlptracehttp.Option{}
		if cfg.Endpoint != "" {
			ep := strings.TrimPrefix(strings.TrimPrefix(cfg.Endpoint, "https://"), "http://")
			opts = append(opts, otlptracehttp.WithEndpoint(ep))
		}
		if cfg.Insecure {
			opts = append(opts, otlptracehttp.WithInsecure())
		}
		if len(cfg.Headers) > 0 {
			opts = append(opts, otlptracehttp.WithHeaders(cfg.Headers))
		}
		client := otlptracehttp.NewClient(opts...)
		return otlptrace.New(ctx, client)
	case "otlp-grpc", "otlpgrpc", "grpc":
		opts := []otlptracegrpc.Option{}
		if cfg.Endpoint != "" {
			opts = append(opts, otlptracegrpc.WithEndpoint(cfg.Endpoint))
		}
		if cfg.Insecure {
			opts = append(opts, otlptracegrpc.WithInsecure())
		}
		if len(cfg.Headers) > 0 {
			opts = append(opts, otlptracegrpc.WithHeaders(cfg.Headers))
		}
		return otlptracegrpc.New(ctx, opts...)
	default:
		return nil, fmt.Errorf("unknown exporter: %s", cfg.Exporter)
	}
}

// GinMiddleware 返回为 gin 注入 OpenTelemetry Span 的中间件。
//
// - 从请求头提取 W3C TraceContext / Baggage（上游链路透传）
// - 创建 server span，SpanKind=Server，attributes=http.method/http.target/http.status_code
// - 把 TraceID 写入 gin.Context (key="trace_id") 和响应头 X-Trace-ID（**在 c.Next() 之前**，确保 handler 写响应前 header 已就绪）
// - 通过 skipPaths 跳过健康检查等端点
func GinMiddleware(service string, skipPaths ...string) gin.HandlerFunc {
	if service == "" {
		service = "aegis"
	}
	skip := map[string]bool{}
	for _, p := range skipPaths {
		skip[p] = true
	}
	tracer := otel.Tracer(service)

	return func(c *gin.Context) {
		if skip[c.FullPath()] || skip[c.Request.URL.Path] {
			c.Next()
			return
		}

		// 1. 提取上游链路
		ctx := otel.GetTextMapPropagator().Extract(
			c.Request.Context(),
			propagation.HeaderCarrier(c.Request.Header),
		)

		// 2. 创建 Server Span
		spanName := c.FullPath()
		if spanName == "" {
			spanName = c.Request.URL.Path
		}
		ctx, span := tracer.Start(ctx, c.Request.Method+" "+spanName,
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(
				attribute.String("http.method", c.Request.Method),
				attribute.String("http.target", c.Request.URL.Path),
				attribute.String("http.route", c.FullPath()),
				attribute.String("http.user_agent", c.Request.UserAgent()),
				attribute.String("net.peer.ip", c.ClientIP()),
			),
		)
		defer span.End()

		// 3. 绑定到 Request Context
		c.Request = c.Request.WithContext(ctx)

		// 4. 在 handler 写响应之前注入 TraceID 到 gin context + header
		sc := span.SpanContext()
		if sc.IsValid() {
			tid := sc.TraceID().String()
			c.Set("trace_id", tid)
			c.Set("span_id", sc.SpanID().String())
			c.Header("X-Trace-ID", tid)
		}

		// 5. 执行后续
		c.Next()

		// 6. 补充响应态 attribute
		status := c.Writer.Status()
		span.SetAttributes(attribute.Int("http.status_code", status))
		if status >= 500 {
			span.SetStatus(codes.Error, "server error")
		}
	}
}

// SpanFromContext 对外暴露常用快捷方法。
func SpanFromContext(ctx context.Context) trace.Span { return trace.SpanFromContext(ctx) }

// TraceIDFromContext 从 context 读取当前 TraceID，不存在时返回空串。
func TraceIDFromContext(ctx context.Context) string {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return ""
	}
	return sc.TraceID().String()
}

// ── 内部工具 ──

var noopShutdown ShutdownFunc = func(context.Context) error { return nil }

type silentErrorHandler struct {
	mu sync.Mutex
}

func (s *silentErrorHandler) Handle(err error) {
	// 避免大量 exporter 失败噪声污染 stderr；仅调试时可自行替换为 log
	_ = err
}

func parseBool(s string, def bool) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "t", "true", "on", "yes":
		return true
	case "0", "f", "false", "off", "no":
		return false
	}
	return def
}

func parseFloat(s string) (float64, error) {
	var f float64
	_, err := fmt.Sscanf(strings.TrimSpace(s), "%f", &f)
	return f, err
}

func parseHeaders(s string) map[string]string {
	m := map[string]string{}
	for _, kv := range strings.Split(s, ",") {
		p := strings.SplitN(strings.TrimSpace(kv), "=", 2)
		if len(p) == 2 && p[0] != "" {
			m[p[0]] = p[1]
		}
	}
	return m
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func firstEnv(keys ...string) string {
	for _, k := range keys {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return ""
}
