package otel

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

var tp trace.TracerProvider = noop.NewTracerProvider()

// SetTracerProvider sets the global TracerProvider used by this package and the
// OpenTelemetry SDK. Callers should invoke this during initialisation, typically
// from InitFromConfig.
func SetTracerProvider(provider trace.TracerProvider) {
	tp = provider
	otel.SetTracerProvider(provider)
}

// Tracer returns a named tracer from the configured TracerProvider. When no
// provider has been set, a no-op tracer is returned so that callers do not need
// nil checks.
func Tracer(name string) trace.Tracer {
	return tp.Tracer(name, trace.WithInstrumentationVersion("0.10.0"))
}

// GetPropagator returns the global TextMapPropagator registered with the
// OpenTelemetry SDK.
func GetPropagator() propagation.TextMapPropagator {
	return otel.GetTextMapPropagator()
}
