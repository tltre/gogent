package logger

import (
	"context"
	"math/rand"
	"time"

	"go.opentelemetry.io/otel/trace"
)

type Level int8

const (
	DebugLevel Level = iota - 1
	InfoLevel
	WarnLevel
	ErrorLevel
)

func (l Level) String() string {
	switch l {
	case DebugLevel:
		return "DEBUG"
	case InfoLevel:
		return "INFO"
	case WarnLevel:
		return "WARN"
	case ErrorLevel:
		return "ERROR"
	default:
		return "INFO"
	}
}

type Field struct {
	Key   string
	Value any
}

type LogEntry struct {
	Timestamp time.Time
	Level     Level
	Module    string
	Message   string
	Duration  time.Duration
	Fields    []Field
}

type LogEvent struct {
	TraceID string
	Entry   LogEntry
}

type Logger interface {
	Log(ctx context.Context, entry LogEntry)
}

type NoopLogger struct{}

func (n *NoopLogger) Log(_ context.Context, _ LogEntry) {}

type contextKey struct{}

var traceIDKey = &contextKey{}

func WithTraceID(ctx context.Context) context.Context {
	traceID := generateTraceID()
	if existing := TraceIDFromContext(ctx); existing != "" {
		return ctx
	}
	return context.WithValue(ctx, traceIDKey, traceID)
}

func TraceIDFromContext(ctx context.Context) string {
	if span := trace.SpanFromContext(ctx); span.SpanContext().HasTraceID() {
		return span.SpanContext().TraceID().String()
	}
	if id, ok := ctx.Value(traceIDKey).(string); ok {
		return id
	}
	return ""
}

func generateTraceID() string {
	const charset = "abcdef0123456789"
	b := make([]byte, 16)
	for i := range b {
		b[i] = charset[rand.Intn(len(charset))]
	}
	return string(b)
}
