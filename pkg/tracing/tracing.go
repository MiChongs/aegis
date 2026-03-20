package tracing

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/trace"
)

func Init() func(context.Context) error {
	provider := trace.NewTracerProvider()
	otel.SetTracerProvider(provider)
	return provider.Shutdown
}
