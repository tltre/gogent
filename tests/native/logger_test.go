package native_test

import (
	"context"
	"testing"

	"github.com/tltre/gogent/pkg/component"
	"github.com/tltre/gogent/pkg/logger"
)

func TestLoggerComponent(t *testing.T) {
	l := logger.NewDefaultLogger(logger.Config{
		Level:  "info",
		Format: "console",
	})
	comp := logger.NewComponent("test-logger", l)
	ctx := context.Background()

	if got := comp.GetName(); got != "test-logger" {
		t.Errorf("GetName() = %q, want %q", got, "test-logger")
	}
	if got := comp.GetType(); got != component.ComponentLogger {
		t.Errorf("GetType() = %q, want %q", got, component.ComponentLogger)
	}
	if err := comp.Initialize(ctx, component.NewRegistry()); err != nil {
		t.Fatalf("Initialize() = %v", err)
	}
	if err := comp.Start(ctx); err != nil {
		t.Fatalf("Start() = %v", err)
	}

	if err := comp.Stop(ctx); err != nil {
		t.Fatalf("Stop() = %v", err)
	}
}

func TestLoggerDefault(t *testing.T) {
	l := logger.Default()
	if l == nil {
		t.Fatal("Default() returned nil")
	}
	l.Log(context.Background(), logger.LogEntry{
		Level:   logger.InfoLevel,
		Module:  "test",
		Message: "default logger works",
	})
}

func TestLoggerLevels(t *testing.T) {
	l := logger.NewDefaultLogger(logger.Config{Level: "warn", Format: "console"})
	ctx := context.Background()

	l.Log(ctx, logger.LogEntry{Level: logger.DebugLevel, Module: "test", Message: "should not appear"})
	l.Log(ctx, logger.LogEntry{Level: logger.InfoLevel, Module: "test", Message: "should not appear"})
	l.Log(ctx, logger.LogEntry{Level: logger.WarnLevel, Module: "test", Message: "warn appears"})
	l.Log(ctx, logger.LogEntry{Level: logger.ErrorLevel, Module: "test", Message: "error appears"})
	l.Sync()
}

func TestLoggerJSONFormat(t *testing.T) {
	l := logger.NewDefaultLogger(logger.Config{Level: "debug", Format: "json"})
	l.Log(context.Background(), logger.LogEntry{
		Level:    logger.InfoLevel,
		Module:   "transport",
		Message:  "provider/generate OK",
		Duration: 200 * 1000 * 1000, // 200ms
		Fields: []logger.Field{
			{Key: "tokens", Value: 150},
		},
	})
	l.Sync()
}
