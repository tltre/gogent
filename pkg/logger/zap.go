package logger

import (
	"context"
	"os"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type ZapLogger struct {
	logger *zap.Logger
	level  Level
	atom   zap.AtomicLevel
}

func NewDefault() *ZapLogger {
	return NewZapLogger(Config{
		Level:  "info",
		Format: "console",
		Output: "stderr",
	})
}

type Config struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
	Output string `yaml:"output"`
}

func NewZapLogger(cfg Config) *ZapLogger {
	level := parseLevel(cfg.Level)
	atom := zap.NewAtomicLevelAt(toZapLevel(level))

	encoderCfg := zap.NewDevelopmentEncoderConfig()
	encoderCfg.EncodeTime = zapcore.TimeEncoderOfLayout("2006-01-02T15:04:05.000Z07:00")
	encoderCfg.EncodeLevel = zapcore.CapitalLevelEncoder

	var encoder zapcore.Encoder
	switch cfg.Format {
	case "json":
		jsonCfg := zap.NewProductionEncoderConfig()
		jsonCfg.TimeKey = "ts"
		jsonCfg.LevelKey = "level"
		jsonCfg.MessageKey = "msg"
		jsonCfg.EncodeTime = zapcore.TimeEncoderOfLayout(time.RFC3339Nano)
		encoder = zapcore.NewJSONEncoder(jsonCfg)
	default:
		encoder = zapcore.NewConsoleEncoder(encoderCfg)
	}

	writeSyncer := zapcore.AddSync(os.Stderr)
	if cfg.Output == "stdout" {
		writeSyncer = zapcore.AddSync(os.Stdout)
	}

	core := zapcore.NewCore(encoder, writeSyncer, atom)
	return &ZapLogger{
		logger: zap.New(core, zap.AddCallerSkip(1)),
		level:  level,
		atom:   atom,
	}
}

func (l *ZapLogger) Log(_ context.Context, entry LogEntry) {
	if entry.Level < l.level {
		return
	}
	fields := make([]zap.Field, 0, len(entry.Fields)+3)
	fields = append(fields, zap.String("module", entry.Module))
	if entry.Duration > 0 {
		fields = append(fields, zap.Duration("dur", entry.Duration))
	}
	for _, f := range entry.Fields {
		fields = append(fields, zap.Any(f.Key, f.Value))
	}

	switch entry.Level {
	case DebugLevel:
		l.logger.Debug(entry.Message, fields...)
	case InfoLevel:
		l.logger.Info(entry.Message, fields...)
	case WarnLevel:
		l.logger.Warn(entry.Message, fields...)
	case ErrorLevel:
		l.logger.Error(entry.Message, fields...)
	}
}

func (l *ZapLogger) Level() Level { return l.level }

func (l *ZapLogger) Sync() error { return l.logger.Sync() }

func toZapLevel(l Level) zapcore.Level {
	switch l {
	case DebugLevel:
		return zapcore.DebugLevel
	case InfoLevel:
		return zapcore.InfoLevel
	case WarnLevel:
		return zapcore.WarnLevel
	case ErrorLevel:
		return zapcore.ErrorLevel
	default:
		return zapcore.InfoLevel
	}
}

func parseLevel(s string) Level {
	switch s {
	case "debug":
		return DebugLevel
	case "info":
		return InfoLevel
	case "warn":
		return WarnLevel
	case "error":
		return ErrorLevel
	default:
		return InfoLevel
	}
}
