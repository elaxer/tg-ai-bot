package config

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"

	"golang.org/x/exp/constraints"
	"gopkg.in/yaml.v3"
)

const defaultSystemPrompt = "You are a friendly, informal chat companion in a Telegram group. Respond naturally, briefly, and like a regular friend. Avoid sounding formal, robotic, or like customer support."

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

	b, err := os.ReadFile(path)
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
	token := strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN"))
	if token == "" {
		return Runtime{}, fmt.Errorf("missing required env: TELEGRAM_BOT_TOKEN")
	}
	openAIAPIKey := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	if openAIAPIKey == "" {
		return Runtime{}, fmt.Errorf("missing required env: OPENAI_API_KEY")
	}
	return Runtime{
		TelegramToken: token,
		OpenAIAPIKey:  openAIAPIKey,
	}, nil
}

func LoadDotEnv(path string) error {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("open dotenv: %w", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		value = strings.TrimSpace(value)
		if len(value) >= 2 {
			if (value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'') {
				value = value[1 : len(value)-1]
			}
		}
		// Keep explicit environment variables as highest priority.
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		if err := os.Setenv(key, value); err != nil {
			return fmt.Errorf("set env %s: %w", key, err)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read dotenv: %w", err)
	}
	return nil
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

func (c Bot) validate() error {
	if err := c.Log.validate(); err != nil {
		return err
	}
	if err := c.Memes.validate(); err != nil {
		return err
	}

	if c.RandomReplyChance < 0 || c.RandomReplyChance > 1 {
		return fmt.Errorf("bot_random_reply_chance must be between 0 and 1")
	}
	if c.ReactionChance < 0 || c.ReactionChance > 1 {
		return fmt.Errorf("bot_reaction_chance must be between 0 and 1")
	}
	if len(c.Reactions) == 0 {
		return fmt.Errorf("bot_reactions must contain at least one reaction")
	}
	if c.RandomStickerChance < 0 || c.RandomStickerChance > 1 {
		return fmt.Errorf("bot_random_sticker_chance must be between 0 and 1")
	}
	if c.TTSReplyChance < 0 || c.TTSReplyChance > 1 {
		return fmt.Errorf("bot_tts_reply_chance must be between 0 and 1")
	}
	if c.DailyMessageInterval <= 0 {
		return fmt.Errorf("bot_daily_message_interval must be > 0")
	}
	if strings.TrimSpace(c.DBPath) == "" {
		return fmt.Errorf("conversation_db_path is required")
	}

	return nil
}
