package tracing

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/trace"
)

func init() { gin.SetMode(gin.TestMode) }

func TestInitNoExporter_StillProducesTraceID(t *testing.T) {
	shutdown, err := Init(context.Background(), Config{
		Enabled:     true,
		ServiceName: "aegis-test",
		Exporter:    "none",
		Sampler:     "always_on",
	})
	if err != nil {
		t.Fatalf("Init err: %v", err)
	}
	defer shutdown(context.Background())

	r := gin.New()
	r.Use(GinMiddleware("aegis-test"))
	var gotTraceID string
	r.GET("/ping", func(c *gin.Context) {
		gotTraceID = TraceIDFromContext(c.Request.Context())
		c.String(200, "ok")
	})

	req := httptest.NewRequest("GET", "/ping", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if gotTraceID == "" {
		t.Fatalf("TraceID should be generated even with 'none' exporter")
	}
	if len(gotTraceID) != 32 {
		t.Fatalf("TraceID should be 32 hex chars, got %q", gotTraceID)
	}
	if w.Header().Get("X-Trace-ID") != gotTraceID {
		t.Errorf("X-Trace-ID header should match context trace id")
	}
}

func TestGinMiddlewareSkipPaths(t *testing.T) {
	shutdown, _ := Init(context.Background(), Config{Enabled: true, Exporter: "none", Sampler: "always_on"})
	defer shutdown(context.Background())

	r := gin.New()
	r.Use(GinMiddleware("svc", "/healthz"))
	r.GET("/healthz", func(c *gin.Context) {
		sc := trace.SpanFromContext(c.Request.Context()).SpanContext()
		if sc.IsValid() {
			t.Errorf("healthz should not have a valid span")
		}
		c.String(200, "ok")
	})
	req := httptest.NewRequest("GET", "/healthz", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Header().Get("X-Trace-ID") != "" {
		t.Errorf("X-Trace-ID should NOT be set on skipped paths")
	}
}

func TestSamplerBuilder(t *testing.T) {
	cases := map[string]bool{
		"always_on":             true,
		"always_off":            true,
		"traceid_ratio":         true,
		"parentbased_always_on": true,
		"garbage":               true, // default to always_on
	}
	for k := range cases {
		s := buildSampler(Config{Sampler: k, SampleRatio: 0.5})
		if s == nil {
			t.Errorf("sampler %q produced nil", k)
		}
	}
}

func TestParseHelpers(t *testing.T) {
	if !parseBool("true", false) {
		t.Fatal("parseBool(true)")
	}
	if parseBool("false", true) {
		t.Fatal("parseBool(false)")
	}
	if v, _ := parseFloat("0.25"); v != 0.25 {
		t.Fatalf("parseFloat got %v", v)
	}
	h := parseHeaders("a=b,c=d")
	if h["a"] != "b" || h["c"] != "d" {
		t.Fatalf("parseHeaders: %v", h)
	}
}

func TestTraceIDFromContext_Empty(t *testing.T) {
	if v := TraceIDFromContext(context.Background()); v != "" {
		t.Errorf("expected empty, got %q", v)
	}
}
