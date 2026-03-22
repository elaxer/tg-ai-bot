// Package config loads bot configuration from files and environment variables.
package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/exp/constraints"
	"gopkg.in/yaml.v3"
)

const (
	defaultSystemPrompt = "You are a friendly, informal chat companion in a Telegram group. " +
		"Respond naturally, briefly, and like a regular friend. " +
		"Avoid sounding formal, robotic, or like customer support."
	//nolint:gosec // Environment variable names are not credentials.
	envTelegramBotToken = "TELEGRAM_BOT_TOKEN"
	//nolint:gosec // Environment variable names are not credentials.
	envOpenAIAPIKey = "OPENAI_API_KEY"
)

var (
	errMissingTelegramBotToken = errors.New("missing required env: TELEGRAM_BOT_TOKEN")
	errMissingOpenAIAPIKey     = errors.New("missing required env: OPENAI_API_KEY")
	errInvalidReplyChance      = errors.New("bot_random_reply_chance must be between 0 and 1")
	errInvalidReactionChance   = errors.New("bot_reaction_chance must be between 0 and 1")
	errMissingReactions        = errors.New("bot_reactions must contain at least one reaction")
	errInvalidStickerChance    = errors.New("bot_random_sticker_chance must be between 0 and 1")
	errInvalidTTSReplyChance   = errors.New("bot_tts_reply_chance must be between 0 and 1")
	errInvalidDailyInterval    = errors.New("bot_daily_message_interval must be > 0")
	errMissingConversationDB   = errors.New("conversation_db_path is required")
)

type Bot struct {
	Debug                bool          `yaml:"bot_debug"`
	Log                  LogConfig     `yaml:",inline"`
	OpenAI               OpenAIConfig  `yaml:",inline"`
	Memes                MemeConfig    `yaml:",inline"`
	Reactions            []string      `yaml:"bot_reactions"`
	ReactionChance       float64       `yaml:"bot_reaction_chance"`
	RandomReplyChance    float64       `yaml:"bot_random_reply_chance"`
	StickerFileIDs       []string      `yaml:"bot_sticker_file_ids"`
	RandomStickerChance  float64       `yaml:"bot_random_sticker_chance"`
	TTSReplyChance       float64       `yaml:"bot_tts_reply_chance"`
	DailyMessageInterval time.Duration `yaml:"bot_daily_message_interval"`
	DBPath               string        `yaml:"conversation_db_path"`
}

func LoadBot(path string) (Bot, error) {
	var cfg Bot

	cleanPath := filepath.Clean(path)
	// #nosec G304 -- config path is intentionally configurable.
	b, err := os.ReadFile(cleanPath)
	if err != nil {
		return cfg, fmt.Errorf("read file: %w", err)
	}
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return cfg, fmt.Errorf("parse yaml: %w", err)
	}

	cfg.applyDefaults()
	if err := cfg.validate(); err != nil {
		return cfg, err
	}

	return cfg, nil
}

func LoadRuntimeFromEnv() (Runtime, error) {
	token := strings.TrimSpace(os.Getenv(envTelegramBotToken))
	if token == "" {
		return Runtime{}, errMissingTelegramBotToken
	}
	openAIAPIKey := strings.TrimSpace(os.Getenv(envOpenAIAPIKey))
	if openAIAPIKey == "" {
		return Runtime{}, errMissingOpenAIAPIKey
	}

	return Runtime{
		TelegramToken: token,
		OpenAIAPIKey:  openAIAPIKey,
	}, nil
}

func LoadDotEnv(path string) error {
	cleanPath := filepath.Clean(path)
	// #nosec G304 -- dotenv path is intentionally configurable.
	f, err := os.Open(cleanPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}

		return fmt.Errorf("open dotenv: %w", err)
	}
	defer func() {
		_ = f.Close()
	}()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		key, value, ok := parseDotEnvLine(scanner.Text())
		if !ok {
			continue
		}

		if err := setDefaultEnv(key, value); err != nil {
			return fmt.Errorf("set env %s: %w", key, err)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read dotenv: %w", err)
	}

	return nil
}

func parseDotEnvLine(raw string) (string, string, bool) {
	line := strings.TrimSpace(raw)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}

	key, value, ok := strings.Cut(line, "=")
	if !ok {
		return "", "", false
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return "", "", false
	}

	return key, trimOptionalQuotes(strings.TrimSpace(value)), true
}

func trimOptionalQuotes(value string) string {
	if len(value) < 2 {
		return value
	}
	if (value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'') {
		return value[1 : len(value)-1]
	}

	return value
}

func setDefaultEnv(key, value string) error {
	// Keep explicit environment variables as highest priority.
	if _, exists := os.LookupEnv(key); exists {
		return nil
	}

	return os.Setenv(key, value)
}

func setDefaultNum[T constraints.Integer | constraints.Float | time.Duration](ptr *T, defaultVal T) {
	if *ptr == 0 {
		*ptr = defaultVal
	}
}

func setDefaultStr(ptr *string, defaultVal string) {
	if strings.TrimSpace(*ptr) == "" {
		*ptr = defaultVal
	}
}

func (c *Bot) applyDefaults() {
	c.Log.applyDefaults()
	c.OpenAI.applyDefaults()
	c.Memes.applyDefaults()

	setDefaultNum(&c.DailyMessageInterval, 24*time.Hour)
	setDefaultStr(&c.DBPath, "data/conversations.db")

	cleanedStickers := make([]string, 0, len(c.StickerFileIDs))
	for _, id := range c.StickerFileIDs {
		if v := strings.TrimSpace(id); v != "" {
			cleanedStickers = append(cleanedStickers, v)
		}
	}
	c.StickerFileIDs = cleanedStickers

	cleanedReactions := make([]string, 0, len(c.Reactions))
	for _, r := range c.Reactions {
		if v := strings.TrimSpace(r); v != "" {
			cleanedReactions = append(cleanedReactions, v)
		}
	}
	c.Reactions = cleanedReactions
}

func (c *Bot) validate() error {
	if err := c.Log.validate(); err != nil {
		return err
	}
	if err := c.Memes.validate(); err != nil {
		return err
	}

	return validateBotRanges(c)
}

func validateBotRanges(c *Bot) error {
	checks := []struct {
		valid bool
		err   error
	}{
		{valid: c.RandomReplyChance >= 0 && c.RandomReplyChance <= 1, err: errInvalidReplyChance},
		{valid: c.ReactionChance >= 0 && c.ReactionChance <= 1, err: errInvalidReactionChance},
		{valid: len(c.Reactions) > 0, err: errMissingReactions},
		{valid: c.RandomStickerChance >= 0 && c.RandomStickerChance <= 1, err: errInvalidStickerChance},
		{valid: c.TTSReplyChance >= 0 && c.TTSReplyChance <= 1, err: errInvalidTTSReplyChance},
		{valid: c.DailyMessageInterval > 0, err: errInvalidDailyInterval},
		{valid: strings.TrimSpace(c.DBPath) != "", err: errMissingConversationDB},
	}
	for _, check := range checks {
		if !check.valid {
			return check.err
		}
	}

	return nil
}
