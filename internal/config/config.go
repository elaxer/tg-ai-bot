package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

const DefaultSystemPrompt = "You are a friendly, informal chat companion in a Telegram group. Respond naturally, briefly, and like a regular friend. Avoid sounding formal, robotic, or like customer support."

type Bot struct {
	Debug               bool     `yaml:"bot_debug"`
	ResponseDelayMinMS  int      `yaml:"bot_response_delay_min_ms"`
	ResponseDelayMaxMS  int      `yaml:"bot_response_delay_max_ms"`
	RandomReplyChance   float64  `yaml:"bot_random_reply_chance"`
	StickerFileIDs      []string `yaml:"bot_sticker_file_ids"`
	RandomStickerChance float64  `yaml:"bot_random_sticker_chance"`
	TTSReplyChance      float64  `yaml:"bot_tts_reply_chance"`
	TTSVoice            string   `yaml:"bot_tts_voice"`
}

type Runtime struct {
	TelegramToken  string
	OpenAIAPIKey   string
	OpenAIModel    string
	OpenAITTSModel string
	SystemPrompt   string
	BotTag         string
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
	openAIModel := strings.TrimSpace(os.Getenv("OPENAI_MODEL"))
	if openAIModel == "" {
		openAIModel = "gpt-4.1-mini"
	}
	openAITTSModel := strings.TrimSpace(os.Getenv("OPENAI_TTS_MODEL"))
	if openAITTSModel == "" {
		openAITTSModel = "gpt-4o-mini-tts"
	}
	systemPrompt := strings.TrimSpace(os.Getenv("OPENAI_SYSTEM_PROMPT"))
	if systemPrompt == "" {
		systemPrompt = DefaultSystemPrompt
	}

	return Runtime{
		TelegramToken:  token,
		OpenAIAPIKey:   openAIAPIKey,
		OpenAIModel:    openAIModel,
		OpenAITTSModel: openAITTSModel,
		SystemPrompt:   systemPrompt,
	}, nil
}

func (c *Bot) applyDefaults() {
	if c.ResponseDelayMinMS == 0 {
		c.ResponseDelayMinMS = 900
	}
	if c.ResponseDelayMaxMS == 0 {
		c.ResponseDelayMaxMS = 2200
	}
	if c.RandomReplyChance == 0 {
		c.RandomReplyChance = 0.1
	}
	if c.RandomStickerChance == 0 {
		c.RandomStickerChance = 0.2
	}
	if c.TTSReplyChance == 0 {
		c.TTSReplyChance = 0.5
	}
	if strings.TrimSpace(c.TTSVoice) == "" {
		c.TTSVoice = "alloy"
	}

	cleaned := make([]string, 0, len(c.StickerFileIDs))
	for _, id := range c.StickerFileIDs {
		v := strings.TrimSpace(id)
		if v != "" {
			cleaned = append(cleaned, v)
		}
	}
	c.StickerFileIDs = cleaned
}

func (c Bot) validate() error {
	if c.ResponseDelayMinMS < 0 || c.ResponseDelayMaxMS < 0 {
		return fmt.Errorf("response delay values must be >= 0")
	}
	if c.ResponseDelayMaxMS < c.ResponseDelayMinMS {
		return fmt.Errorf("bot_response_delay_max_ms must be >= bot_response_delay_min_ms")
	}
	if c.RandomReplyChance < 0 || c.RandomReplyChance > 1 {
		return fmt.Errorf("bot_random_reply_chance must be between 0 and 1")
	}
	if c.RandomStickerChance < 0 || c.RandomStickerChance > 1 {
		return fmt.Errorf("bot_random_sticker_chance must be between 0 and 1")
	}
	if c.TTSReplyChance < 0 || c.TTSReplyChance > 1 {
		return fmt.Errorf("bot_tts_reply_chance must be between 0 and 1")
	}
	return nil
}
