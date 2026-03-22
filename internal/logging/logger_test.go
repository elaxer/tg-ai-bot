package logging

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestInitValidation(t *testing.T) {
	t.Parallel()

	if err := Init(Config{}); !errors.Is(err, errMissingLogPath) {
		t.Fatalf("Init() error = %v, want %v", err, errMissingLogPath)
	}
}

func TestInitCreatesLogFileAndWrites(t *testing.T) {
	t.Parallel()

	logPath := filepath.Join(t.TempDir(), "logs", "bot.log")
	if err := Init(Config{
		FilePath:   logPath,
		Level:      "debug",
		MaxSizeMB:  1,
		MaxBackups: 1,
		MaxAgeDays: 1,
	}); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	Infow("test log entry", "key", "value")
	Sync()

	info, err := os.Stat(logPath)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if info.Size() == 0 {
		t.Fatalf("log file %s is empty", logPath)
	}
}
