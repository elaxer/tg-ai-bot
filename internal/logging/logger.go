// Package logging provides process-wide structured logging helpers.
package logging

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

var (
	mu     sync.RWMutex
	logger *zap.SugaredLogger

	errMissingLogPath = errors.New("log file path is required")
)

type Config struct {
	FilePath   string
	Level      string
	MaxSizeMB  int
	MaxBackups int
	MaxAgeDays int
	Compress   bool
}

func Init(cfg Config) error {
	if cfg.FilePath == "" {
		return errMissingLogPath
	}
	if err := os.MkdirAll(filepath.Dir(cfg.FilePath), 0o750); err != nil {
		return fmt.Errorf("create log dir: %w", err)
	}

	level := zapcore.InfoLevel
	if strings.TrimSpace(cfg.Level) != "" {
		parsed, err := zapcore.ParseLevel(strings.ToLower(strings.TrimSpace(cfg.Level)))
		if err != nil {
			return fmt.Errorf("invalid log level %q: %w", cfg.Level, err)
		}
		level = parsed
	}
	atomicLevel := zap.NewAtomicLevelAt(level)

	encCfg := zap.NewProductionEncoderConfig()
	encCfg.TimeKey = "ts"
	encCfg.LevelKey = "level"
	encCfg.MessageKey = "msg"
	encCfg.CallerKey = "caller"
	encCfg.EncodeTime = zapcore.ISO8601TimeEncoder
	encCfg.EncodeDuration = zapcore.StringDurationEncoder
	encCfg.EncodeLevel = zapcore.LowercaseLevelEncoder
	fileEncoder := zapcore.NewJSONEncoder(encCfg)

	consoleEncCfg := encCfg
	consoleEncCfg.EncodeLevel = zapcore.CapitalColorLevelEncoder
	consoleEncoder := zapcore.NewConsoleEncoder(consoleEncCfg)

	rotator := &lumberjack.Logger{
		Filename:   cfg.FilePath,
		MaxSize:    cfg.MaxSizeMB,
		MaxBackups: cfg.MaxBackups,
		MaxAge:     cfg.MaxAgeDays,
		Compress:   cfg.Compress,
	}

	core := zapcore.NewTee(
		zapcore.NewCore(consoleEncoder, zapcore.AddSync(os.Stdout), atomicLevel),
		zapcore.NewCore(fileEncoder, zapcore.AddSync(rotator), atomicLevel),
	)
	base := zap.New(core, zap.AddCaller(), zap.AddStacktrace(zapcore.ErrorLevel))
	sugar := base.Sugar()

	mu.Lock()
	defer mu.Unlock()
	if logger != nil {
		_ = logger.Sync()
	}
	logger = sugar

	return nil
}

func Sync() {
	mu.RLock()
	defer mu.RUnlock()
	if logger != nil {
		_ = logger.Sync()
	}
}

func Infof(format string, args ...any) {
	withLogger(func(l *zap.SugaredLogger) {
		l.Infof(format, args...)
	})
}

func Infow(msg string, keysAndValues ...any) {
	withLogger(func(l *zap.SugaredLogger) {
		l.Infow(msg, keysAndValues...)
	})
}

func Errorf(format string, args ...any) {
	withLogger(func(l *zap.SugaredLogger) {
		l.Errorf(format, args...)
	})
}

func Errorw(msg string, keysAndValues ...any) {
	withLogger(func(l *zap.SugaredLogger) {
		l.Errorw(msg, keysAndValues...)
	})
}

func Fatalf(format string, args ...any) {
	withLogger(func(l *zap.SugaredLogger) {
		l.Fatalf(format, args...)
	})
}

func Fatalw(msg string, keysAndValues ...any) {
	withLogger(func(l *zap.SugaredLogger) {
		l.Fatalw(msg, keysAndValues...)
	})
}

func withLogger(fn func(*zap.SugaredLogger)) {
	mu.RLock()
	l := logger
	mu.RUnlock()
	if l == nil {
		fallback, _ := zap.NewProduction()
		s := fallback.Sugar()
		defer func() { _ = s.Sync() }()
		fn(s)

		return
	}
	fn(l)
}
