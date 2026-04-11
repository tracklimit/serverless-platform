package tracing_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"

	"serverless-platform/internal/tracing"
)

func TestInit_SucceedsAndShutsDown(t *testing.T) {
	ctx := context.Background()

	shutdown, err := tracing.Init(ctx, "test-service", "localhost:4317")
	require.NoError(t, err)
	require.NotNil(t, shutdown)

	// Shutdown must complete even though the OTLP endpoint is unreachable —
	// the batcher has no spans to flush, so this should return promptly.
	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	assert.NoError(t, shutdown(shutdownCtx))
}

func TestTracer_RecordsSpans(t *testing.T) {
	// Swap in an in-memory exporter so we can inspect spans without an
	// OTLP collector. This exercises the same OTel API the app uses at
	// runtime, proving that otel.Tracer() → Start() → End() lands in an
	// exporter.
	exporter := tracetest.NewInMemoryExporter()

	res, err := resource.New(context.Background(),
		resource.WithAttributes(semconv.ServiceNameKey.String("test-service")),
	)
	require.NoError(t, err)

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
	})

	tracer := otel.Tracer("test")
	_, span := tracer.Start(context.Background(), "unit-test-span")
	span.End()

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, "unit-test-span", spans[0].Name)
	assert.Equal(t, "test-service", spans[0].Resource.Attributes()[0].Value.AsString())
}
