package observability

import (
	"context"
	"fmt"
	"os"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// TraceConfig configures OpenTelemetry tracing.
type TraceConfig struct {
	Enabled       bool
	Exporter      string // "none", "file", "otlp-http"
	Endpoint      string // OTLP HTTP endpoint (host:port)
	File          string // trace file path when exporter=file
	SamplingRatio float64
	ServiceName   string
}

// SetupTracing initialises the global tracer provider and propagator.
// Returns a shutdown function and any error.
func SetupTracing(ctx context.Context, cfg TraceConfig) (func(context.Context) error, error) {
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	if !cfg.Enabled || strings.EqualFold(cfg.Exporter, "none") {
		return func(context.Context) error { return nil }, nil
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(semconv.ServiceNameKey.String(cfg.ServiceName)),
	)
	if err != nil {
		return nil, fmt.Errorf("create trace resource: %w", err)
	}

	sampler := sdktrace.AlwaysSample()
	if cfg.SamplingRatio >= 0 && cfg.SamplingRatio < 1 {
		sampler = sdktrace.TraceIDRatioBased(cfg.SamplingRatio)
	}

	var exporter sdktrace.SpanExporter
	switch strings.ToLower(cfg.Exporter) {
	case "file":
		path := cfg.File
		if path == "" {
			path = "./traces.json"
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return nil, fmt.Errorf("open trace file: %w", err)
		}
		exporter, err = stdouttrace.New(
			stdouttrace.WithWriter(f),
			stdouttrace.WithPrettyPrint(),
		)
		if err != nil {
			_ = f.Close()
			return nil, fmt.Errorf("create file trace exporter: %w", err)
		}
	case "otlp-http", "otlp", "otlphttp":
		opts := []otlptracehttp.Option{}
		if cfg.Endpoint != "" {
			opts = append(opts, otlptracehttp.WithEndpoint(cfg.Endpoint), otlptracehttp.WithInsecure())
		}
		exporter, err = otlptracehttp.New(ctx, opts...)
		if err != nil {
			return nil, fmt.Errorf("create otlp-http exporter: %w", err)
		}
	default:
		return nil, fmt.Errorf("unsupported trace exporter %q", cfg.Exporter)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sampler),
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)

	return tp.Shutdown, nil
}
