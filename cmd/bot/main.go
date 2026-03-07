package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"gopkg.in/yaml.v3"
)

const defaultSystemPrompt = "You are a friendly, informal chat companion in a Telegram group. Respond naturally, briefly, and like a regular friend. Avoid sounding formal, robotic, or like customer support."

var defaultReactions = []string{"👍", "💩", "🤡", "💯", "🤣"}

type Config struct {
	BotDebug               bool     `yaml:"bot_debug"`
	BotResponseDelayMinMS  int      `yaml:"bot_response_delay_min_ms"`
	BotResponseDelayMaxMS  int      `yaml:"bot_response_delay_max_ms"`
	BotRandomReplyChance   float64  `yaml:"bot_random_reply_chance"`
	BotStickerFileIDs      []string `yaml:"bot_sticker_file_ids"`
	BotRandomStickerChance float64  `yaml:"bot_random_sticker_chance"`
}

func main() {
	log.SetFlags(log.LstdFlags)

	configPath := strings.TrimSpace(os.Getenv("BOT_CONFIG_PATH"))
	if configPath == "" {
		configPath = "bot.config.yaml"
	}

	cfg, err := loadConfig(configPath)
	if err != nil {
		logErrorf("failed to load config %q: %v", configPath, err)
		os.Exit(1)
	}
	token := strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN"))
	if token == "" {
		logErrorf("missing required env: TELEGRAM_BOT_TOKEN")
		os.Exit(1)
	}
	openAIAPIKey := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	if openAIAPIKey == "" {
		logErrorf("missing required env: OPENAI_API_KEY")
		os.Exit(1)
	}
	openAIModel := strings.TrimSpace(os.Getenv("OPENAI_MODEL"))
	if openAIModel == "" {
		openAIModel = "gpt-4.1-mini"
	}
	openAISystemPrompt := strings.TrimSpace(os.Getenv("OPENAI_SYSTEM_PROMPT"))
	if openAISystemPrompt == "" {
		openAISystemPrompt = defaultSystemPrompt
	}

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	bot, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		logErrorf("failed to create telegram bot client: %v", err)
		os.Exit(1)
	}

	bot.Debug = cfg.BotDebug
	logInfof(
		"bot started config=%s username=@%s id=%d model=%s debug=%t response_delay_ms=%d-%d random_reply=%.2f stickers=%d sticker_chance=%.2f reaction_chance=0.20",
		configPath,
		bot.Self.UserName,
		bot.Self.ID,
		openAIModel,
		bot.Debug,
		cfg.BotResponseDelayMinMS,
		cfg.BotResponseDelayMaxMS,
		cfg.BotRandomReplyChance,
		len(cfg.BotStickerFileIDs),
		cfg.BotRandomStickerChance,
	)
	botTag := "@" + strings.ToLower(bot.Self.UserName)

	updateCfg := tgbotapi.NewUpdate(0)
	updateCfg.Timeout = 30

	updates := bot.GetUpdatesChan(updateCfg)

	for update := range updates {
		if update.Message == nil || update.Message.Text == "" {
			continue
		}
		if update.Message.From != nil && update.Message.From.ID == bot.Self.ID {
			continue
		}

		chatType := update.Message.Chat.Type
		if chatType != "group" && chatType != "supergroup" {
			continue
		}
		if rng.Float64() < 0.2 {
			emoji := defaultReactions[rng.Intn(len(defaultReactions))]
			if err := setMessageReaction(bot, update.Message.Chat.ID, update.Message.MessageID, emoji); err != nil {
				logErrorf(
					"telegram set reaction failed chat_id=%d msg_id=%d emoji=%q err=%v",
					update.Message.Chat.ID,
					update.Message.MessageID,
					emoji,
					err,
				)
			} else {
				logInfof(
					"reacted to message chat_id=%d msg_id=%d emoji=%q",
					update.Message.Chat.ID,
					update.Message.MessageID,
					emoji,
				)
			}
		}

		tagged := strings.Contains(strings.ToLower(update.Message.Text), botTag)
		replyToBot := update.Message.ReplyToMessage != nil &&
			update.Message.ReplyToMessage.From != nil &&
			update.Message.ReplyToMessage.From.ID == bot.Self.ID
		randomReply := rng.Float64() < cfg.BotRandomReplyChance
		if !tagged && !replyToBot && !randomReply {
			continue
		}
		trigger := "tag"
		if replyToBot {
			trigger = "reply_to_bot"
		} else if randomReply {
			trigger = "random"
		}
		userID := int64(0)
		userUsername := ""
		userDisplayName := ""
		replyContext := ""
		if update.Message.From != nil {
			userID = update.Message.From.ID
			userUsername = strings.TrimSpace(update.Message.From.UserName)
			userDisplayName = strings.TrimSpace(strings.TrimSpace(update.Message.From.FirstName + " " + update.Message.From.LastName))
		}
		if update.Message.ReplyToMessage != nil {
			replyContext = buildReplyContext(update.Message.ReplyToMessage)
		}

		promptText := update.Message.Text
		if tagged {
			promptText = strings.TrimSpace(strings.ReplaceAll(promptText, botTag, ""))
		}
		if promptText == "" {
			logInfof(
				"skip empty prompt chat_id=%d msg_id=%d trigger=%s user_id=%d",
				update.Message.Chat.ID,
				update.Message.MessageID,
				trigger,
				userID,
			)
			continue
		}
		stopTyping := startTypingIndicator(bot, update.Message.Chat.ID, update.Message.MessageID)
		shouldSendSticker := len(cfg.BotStickerFileIDs) > 0 && rng.Float64() < cfg.BotRandomStickerChance

		logInfof(
			"incoming message chat_id=%d msg_id=%d trigger=%s user_id=%d use_sticker=%t text=%q",
			update.Message.Chat.ID,
			update.Message.MessageID,
			trigger,
			userID,
			shouldSendSticker,
			shorten(promptText, 160),
		)

		replyText := ""
		if !shouldSendSticker {
			var err error
			replyText, err = askChatGPT(promptText, openAIAPIKey, openAIModel, openAISystemPrompt, userID, userUsername, userDisplayName, replyContext)
			if err != nil {
				logErrorf(
					"chatgpt request failed chat_id=%d msg_id=%d trigger=%s err=%v",
					update.Message.Chat.ID,
					update.Message.MessageID,
					trigger,
					err,
				)
				replyText = "I couldn't generate a response right now."
			}
		}

		delay := randomDelay(cfg.BotResponseDelayMinMS, cfg.BotResponseDelayMaxMS, rng)
		logInfof(
			"delaying response chat_id=%d reply_to=%d delay_ms=%d",
			update.Message.Chat.ID,
			update.Message.MessageID,
			delay.Milliseconds(),
		)
		time.Sleep(delay)

		if shouldSendSticker {
			fileID := cfg.BotStickerFileIDs[rng.Intn(len(cfg.BotStickerFileIDs))]
			sticker := tgbotapi.NewSticker(update.Message.Chat.ID, tgbotapi.FileID(fileID))
			sticker.ReplyToMessageID = update.Message.MessageID
			sent, err := bot.Send(sticker)
			stopTyping()
			if err != nil {
				logErrorf(
					"telegram sticker send failed chat_id=%d reply_to=%d err=%v",
					update.Message.Chat.ID,
					update.Message.MessageID,
					err,
				)
				continue
			}
			logInfof(
				"sent sticker chat_id=%d msg_id=%d reply_to=%d",
				sent.Chat.ID,
				sent.MessageID,
				update.Message.MessageID,
			)
			continue
		}

		msg := tgbotapi.NewMessage(update.Message.Chat.ID, replyText)
		msg.ReplyToMessageID = update.Message.MessageID

		sent, err := bot.Send(msg)
		stopTyping()
		if err != nil {
			logErrorf(
				"telegram send failed chat_id=%d reply_to=%d err=%v",
				update.Message.Chat.ID,
				update.Message.MessageID,
				err,
			)
			continue
		}
		logInfof(
			"sent message chat_id=%d msg_id=%d reply_to=%d",
			sent.Chat.ID,
			sent.MessageID,
			update.Message.MessageID,
		)
	}
}

func loadConfig(path string) (Config, error) {
	var cfg Config

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

func (c *Config) applyDefaults() {
	if c.BotResponseDelayMinMS == 0 {
		c.BotResponseDelayMinMS = 900
	}
	if c.BotResponseDelayMaxMS == 0 {
		c.BotResponseDelayMaxMS = 2200
	}
	if c.BotRandomReplyChance == 0 {
		c.BotRandomReplyChance = 0.1
	}
	if c.BotRandomStickerChance == 0 {
		c.BotRandomStickerChance = 0.2
	}

	cleaned := make([]string, 0, len(c.BotStickerFileIDs))
	for _, id := range c.BotStickerFileIDs {
		v := strings.TrimSpace(id)
		if v != "" {
			cleaned = append(cleaned, v)
		}
	}
	c.BotStickerFileIDs = cleaned
}

func (c Config) validate() error {
	if c.BotResponseDelayMinMS < 0 || c.BotResponseDelayMaxMS < 0 {
		return fmt.Errorf("response delay values must be >= 0")
	}
	if c.BotResponseDelayMaxMS < c.BotResponseDelayMinMS {
		return fmt.Errorf("bot_response_delay_max_ms must be >= bot_response_delay_min_ms")
	}
	if c.BotRandomReplyChance < 0 || c.BotRandomReplyChance > 1 {
		return fmt.Errorf("bot_random_reply_chance must be between 0 and 1")
	}
	if c.BotRandomStickerChance < 0 || c.BotRandomStickerChance > 1 {
		return fmt.Errorf("bot_random_sticker_chance must be between 0 and 1")
	}
	return nil
}

func askChatGPT(userText, apiKey, model, systemPrompt string, senderID int64, senderUsername, senderDisplayName, replyContext string) (string, error) {
	userHandle := "unknown"
	if senderUsername != "" {
		userHandle = "@" + senderUsername
	}
	displayName := senderDisplayName
	if displayName == "" {
		displayName = "unknown"
	}
	contextPrompt := fmt.Sprintf(
		"Telegram sender context: id=%d, handle=%s, display_name=%q.\n",
		senderID,
		userHandle,
		displayName,
	)
	if replyContext != "" {
		contextPrompt += "Replied message context:\n" + replyContext + "\n"
	}
	contextPrompt += "Current message:\n" + userText

	reqBody := map[string]any{
		"model":        model,
		"instructions": systemPrompt,
		"input": []map[string]any{
			{
				"role": "user",
				"content": []map[string]string{
					{
						"type": "input_text",
						"text": contextPrompt,
					},
				},
			},
		},
		"max_output_tokens": 300,
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.openai.com/v1/responses", bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("api status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBytes)))
	}

	var parsed struct {
		Output []struct {
			Type    string `json:"type"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
	}
	if err := json.Unmarshal(respBytes, &parsed); err != nil {
		return "", fmt.Errorf("unmarshal response: %w", err)
	}

	for _, item := range parsed.Output {
		if item.Type != "message" {
			continue
		}
		for _, content := range item.Content {
			if content.Type == "output_text" && strings.TrimSpace(content.Text) != "" {
				return strings.TrimSpace(content.Text), nil
			}
		}
	}

	return "", fmt.Errorf("no output_text in response")
}

func logInfof(format string, args ...any) {
	log.Printf("[INFO] "+format, args...)
}

func logErrorf(format string, args ...any) {
	log.Printf("[ERROR] "+format, args...)
}

func shorten(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

func randomDelay(minMS, maxMS int, rng *rand.Rand) time.Duration {
	if maxMS <= minMS {
		return time.Duration(minMS) * time.Millisecond
	}
	return time.Duration(minMS+rng.Intn(maxMS-minMS+1)) * time.Millisecond
}

func setMessageReaction(bot *tgbotapi.BotAPI, chatID int64, messageID int, emoji string) error {
	reaction := fmt.Sprintf(`[{"type":"emoji","emoji":%q}]`, emoji)
	_, err := bot.MakeRequest(
		"setMessageReaction",
		tgbotapi.Params{
			"chat_id":    strconv.FormatInt(chatID, 10),
			"message_id": strconv.Itoa(messageID),
			"reaction":   reaction,
		},
	)
	return err
}

func buildReplyContext(msg *tgbotapi.Message) string {
	if msg == nil {
		return ""
	}
	replyText := strings.TrimSpace(msg.Text)
	if replyText == "" {
		replyText = strings.TrimSpace(msg.Caption)
	}
	if replyText == "" {
		replyText = "[non-text message]"
	}

	replyUserID := int64(0)
	replyHandle := "unknown"
	replyDisplayName := "unknown"
	if msg.From != nil {
		replyUserID = msg.From.ID
		if strings.TrimSpace(msg.From.UserName) != "" {
			replyHandle = "@" + strings.TrimSpace(msg.From.UserName)
		}
		fullName := strings.TrimSpace(strings.TrimSpace(msg.From.FirstName + " " + msg.From.LastName))
		if fullName != "" {
			replyDisplayName = fullName
		}
	}

	return fmt.Sprintf(
		"id=%d, handle=%s, display_name=%q, text=%q",
		replyUserID,
		replyHandle,
		replyDisplayName,
		shorten(replyText, 500),
	)
}

func startTypingIndicator(bot *tgbotapi.BotAPI, chatID int64, replyTo int) func() {
	stop := make(chan struct{})
	go func() {
		sendTyping := func() {
			if _, err := bot.Send(tgbotapi.NewChatAction(chatID, tgbotapi.ChatTyping)); err != nil {
				logErrorf(
					"telegram typing action failed chat_id=%d reply_to=%d err=%v",
					chatID,
					replyTo,
					err,
				)
			}
		}

		sendTyping()
		ticker := time.NewTicker(4 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				sendTyping()
			}
		}
	}()

	return func() {
		close(stop)
	}
}
