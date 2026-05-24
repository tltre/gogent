package otel

import (
	"context"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.27.0"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Config holds the configuration for OpenTelemetry tracing initialisation.
type Config struct {
	Enabled        bool    `yaml:"enabled"`
	Endpoint       string  `yaml:"endpoint"`        // OTLP gRPC endpoint (e.g. "localhost:4317") or "console" for stdout
	ServiceName    string  `yaml:"service_name"`    // logical service name
	ServiceVersion string  `yaml:"service_version"` // service version string
	Environment    string  `yaml:"environment"`     // deployment environment, e.g. "production"
	SampleRate     float64 `yaml:"sample_rate"`     // 0.0 – 1.0, default 1.0 (always sample)
}

// InitFromConfig initialises the OpenTelemetry tracing pipeline based on the
// supplied Config. When tracing is disabled or no endpoint is provided the
// function returns a no-op shutdown function.
//
// The returned shutdown function must be called before the process exits so
// that any buffered spans are flushed.
func InitFromConfig(ctx context.Context, cfg Config) (func(context.Context) error, error) {
	if !cfg.Enabled || cfg.Endpoint == "" {
		return func(ctx context.Context) error { return nil }, nil
	}

	// Set propagator for W3C TraceContext + Baggage.
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{}, propagation.Baggage{},
	))

	// Create the trace exporter: OTLP gRPC or console.
	var traceExporter sdktrace.SpanExporter
	if cfg.Endpoint == "console" {
		var err error
		traceExporter, err = stdouttrace.New(
			stdouttrace.WithWriter(os.Stderr),
			stdouttrace.WithPrettyPrint(),
		)
		if err != nil {
			return nil, err
		}
	} else {
		conn, err := grpc.NewClient(cfg.Endpoint,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		)
		if err != nil {
			return nil, err
		}
		traceExporter, err = otlptracegrpc.New(ctx, otlptracegrpc.WithGRPCConn(conn))
		if err != nil {
			return nil, err
		}
	}

	// Choose a sampler: always sample unless an explicit ratio is given.
	sampler := sdktrace.AlwaysSample()
	if cfg.SampleRate > 0 && cfg.SampleRate < 1.0 {
		sampler = sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.SampleRate))
	}

	// Build the resource describing this service.
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(cfg.ServiceName),
			semconv.ServiceVersion(cfg.ServiceVersion),
			semconv.DeploymentEnvironmentName(cfg.Environment),
		),
	)
	if err != nil {
		return nil, err
	}

	// Create the SDK TracerProvider.
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExporter),
		sdktrace.WithSampler(sampler),
		sdktrace.WithResource(res),
	)
	SetTracerProvider(provider)

	return provider.Shutdown, nil
}
