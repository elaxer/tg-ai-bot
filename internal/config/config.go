package config

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const DefaultSystemPrompt = "You are a friendly, informal chat companion in a Telegram group. Respond naturally, briefly, and like a regular friend. Avoid sounding formal, robotic, or like customer support."

type Bot struct {
	Debug                 bool          `yaml:"bot_debug"`
	LogFilePath           string        `yaml:"log_file_path"`
	LogLevel              string        `yaml:"log_level"`
	LogMaxSizeMB          int           `yaml:"log_max_size_mb"`
	LogMaxBackups         int           `yaml:"log_max_backups"`
	LogMaxAgeDays         int           `yaml:"log_max_age_days"`
	LogCompress           bool          `yaml:"log_compress"`
	OpenAIModel           string        `yaml:"openai_model"`
	OpenAITTSModel        string        `yaml:"openai_tts_model"`
	OpenAISystemPrompt    string        `yaml:"openai_system_prompt"`
	OpenAITTSVoice        string        `yaml:"openai_tts_voice"`
	OpenAITTSInstructions string        `yaml:"openai_tts_instructions"`
	Reactions             []string      `yaml:"bot_reactions"`
	ReactionChance        float64       `yaml:"bot_reaction_chance"`
	RandomReplyChance     float64       `yaml:"bot_random_reply_chance"`
	StickerFileIDs        []string      `yaml:"bot_sticker_file_ids"`
	RandomStickerChance   float64       `yaml:"bot_random_sticker_chance"`
	TTSReplyChance        float64       `yaml:"bot_tts_reply_chance"`
	DailyMessageInterval  time.Duration `yaml:"bot_daily_message_interval"`
	MemeSubreddits        []string      `yaml:"bot_meme_subreddits"`
	MemeIntervalMin       time.Duration `yaml:"bot_meme_interval_min"`
	MemeIntervalMax       time.Duration `yaml:"bot_meme_interval_max"`
}

type Runtime struct {
	TelegramToken string
	OpenAIAPIKey  string
	BotTag        string
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

func (c *Bot) applyDefaults() {
	if strings.TrimSpace(c.LogFilePath) == "" {
		c.LogFilePath = "logs/bot.log"
	}
	if strings.TrimSpace(c.LogLevel) == "" {
		c.LogLevel = "info"
	}
	if c.LogMaxSizeMB == 0 {
		c.LogMaxSizeMB = 50
	}
	if c.LogMaxBackups == 0 {
		c.LogMaxBackups = 10
	}
	if c.LogMaxAgeDays == 0 {
		c.LogMaxAgeDays = 30
	}
	if strings.TrimSpace(c.OpenAIModel) == "" {
		c.OpenAIModel = "gpt-4.1-mini"
	}
	if strings.TrimSpace(c.OpenAITTSModel) == "" {
		c.OpenAITTSModel = "gpt-4o-mini-tts"
	}
	if strings.TrimSpace(c.OpenAISystemPrompt) == "" {
		c.OpenAISystemPrompt = DefaultSystemPrompt
	}
	if strings.TrimSpace(c.OpenAITTSVoice) == "" {
		c.OpenAITTSVoice = "alloy"
	}
	if strings.TrimSpace(c.OpenAITTSInstructions) == "" {
		c.OpenAITTSInstructions = "Speak with a natural England (British English) accent."
	}
	if c.RandomReplyChance == 0 {
		c.RandomReplyChance = 0.1
	}
	if c.ReactionChance == 0 {
		c.ReactionChance = 0.2
	}
	if c.RandomStickerChance == 0 {
		c.RandomStickerChance = 0.2
	}
	if c.TTSReplyChance == 0 {
		c.TTSReplyChance = 0.5
	}
	if c.DailyMessageInterval == 0 {
		c.DailyMessageInterval = 24 * time.Hour
	}
	if c.MemeIntervalMin <= 0 {
		c.MemeIntervalMin = 5 * time.Hour
	}
	if c.MemeIntervalMax <= 0 {
		c.MemeIntervalMax = 6 * time.Hour
	}
	if len(c.Reactions) == 0 {
		c.Reactions = []string{"👍", "💩", "🤡", "💯", "🤣"}
	}

	subreddits := make([]string, 0, len(c.MemeSubreddits))
	for _, sub := range c.MemeSubreddits {
		v := strings.ToLower(strings.TrimSpace(sub))
		if v != "" {
			subreddits = append(subreddits, v)
		}
	}
	if len(subreddits) == 0 {
		subreddits = []string{"memes", "dankmemes", "me_irl"}
	}
	c.MemeSubreddits = subreddits

	cleaned := make([]string, 0, len(c.StickerFileIDs))
	for _, id := range c.StickerFileIDs {
		v := strings.TrimSpace(id)
		if v != "" {
			cleaned = append(cleaned, v)
		}
	}
	c.StickerFileIDs = cleaned

	reactions := make([]string, 0, len(c.Reactions))
	for _, r := range c.Reactions {
		v := strings.TrimSpace(r)
		if v != "" {
			reactions = append(reactions, v)
		}
	}
	c.Reactions = reactions
}

func (c Bot) validate() error {
	if strings.TrimSpace(c.LogFilePath) == "" {
		return fmt.Errorf("log_file_path is required")
	}
	if c.LogMaxSizeMB <= 0 {
		return fmt.Errorf("log_max_size_mb must be > 0")
	}
	if c.LogMaxBackups <= 0 {
		return fmt.Errorf("log_max_backups must be > 0")
	}
	if c.LogMaxAgeDays <= 0 {
		return fmt.Errorf("log_max_age_days must be > 0")
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
	if len(c.MemeSubreddits) == 0 {
		return fmt.Errorf("bot_meme_subreddits must contain at least one subreddit name")
	}
	if c.MemeIntervalMin <= 0 {
		return fmt.Errorf("bot_meme_interval_min must be > 0")
	}
	if c.MemeIntervalMax <= 0 {
		return fmt.Errorf("bot_meme_interval_max must be > 0")
	}
	if c.MemeIntervalMax < c.MemeIntervalMin {
		return fmt.Errorf("bot_meme_interval_max must be >= bot_meme_interval_min")
	}
	return nil
}
