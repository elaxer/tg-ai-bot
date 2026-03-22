package config

import (
	"errors"
	"strings"
)

var (
	errMissingLogFilePath = errors.New("log_file_path is required")
	errInvalidLogMaxSize  = errors.New("log_max_size_mb must be > 0")
	errInvalidLogBackups  = errors.New("log_max_backups must be > 0")
	errInvalidLogMaxAge   = errors.New("log_max_age_days must be > 0")
)

type LogConfig struct {
	FilePath   string `yaml:"log_file_path"`
	Level      string `yaml:"log_level"`
	MaxSizeMB  int    `yaml:"log_max_size_mb"`
	MaxBackups int    `yaml:"log_max_backups"`
	MaxAgeDays int    `yaml:"log_max_age_days"`
	Compress   bool   `yaml:"log_compress"`
}

func (c *LogConfig) applyDefaults() {
	setDefaultStr(&c.FilePath, "logs/bot.log")
	setDefaultStr(&c.Level, "info")
	setDefaultNum(&c.MaxSizeMB, 50)
	setDefaultNum(&c.MaxBackups, 10)
	setDefaultNum(&c.MaxAgeDays, 30)
}

func (c *LogConfig) validate() error {
	if strings.TrimSpace(c.FilePath) == "" {
		return errMissingLogFilePath
	}
	if c.MaxSizeMB <= 0 {
		return errInvalidLogMaxSize
	}
	if c.MaxBackups <= 0 {
		return errInvalidLogBackups
	}
	if c.MaxAgeDays <= 0 {
		return errInvalidLogMaxAge
	}

	return nil
}
