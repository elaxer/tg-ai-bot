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
)

func main() {
	log.SetFlags(log.LstdFlags)

	token := os.Getenv("TELEGRAM_BOT_TOKEN")
	if token == "" {
		logErrorf("missing required env: TELEGRAM_BOT_TOKEN")
		os.Exit(1)
	}
	openAIAPIKey := os.Getenv("OPENAI_API_KEY")
	if openAIAPIKey == "" {
		logErrorf("missing required env: OPENAI_API_KEY")
		os.Exit(1)
	}
	openAIModel := os.Getenv("OPENAI_MODEL")
	if openAIModel == "" {
		openAIModel = "gpt-4.1-mini"
	}
	openAISystemPrompt := os.Getenv("OPENAI_SYSTEM_PROMPT")
	if openAISystemPrompt == "" {
		openAISystemPrompt = "You are a friendly, informal chat companion in a Telegram group. Respond naturally, briefly, and like a regular friend. Avoid sounding formal, robotic, or like customer support."
	}
	delayMinMS := getEnvInt("BOT_RESPONSE_DELAY_MIN_MS", 900)
	delayMaxMS := getEnvInt("BOT_RESPONSE_DELAY_MAX_MS", 2200)
	if delayMaxMS < delayMinMS {
		logErrorf(
			"invalid delay config: BOT_RESPONSE_DELAY_MAX_MS(%d) < BOT_RESPONSE_DELAY_MIN_MS(%d), using min for both",
			delayMaxMS,
			delayMinMS,
		)
		delayMaxMS = delayMinMS
	}
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	bot, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		logErrorf("failed to create telegram bot client: %v", err)
		os.Exit(1)
	}

	bot.Debug = os.Getenv("BOT_DEBUG") == "true"
	logInfof(
		"bot started username=@%s id=%d model=%s debug=%t response_delay_ms=%d-%d",
		bot.Self.UserName,
		bot.Self.ID,
		openAIModel,
		bot.Debug,
		delayMinMS,
		delayMaxMS,
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

		tagged := strings.Contains(strings.ToLower(update.Message.Text), botTag)
		replyToBot := update.Message.ReplyToMessage != nil &&
			update.Message.ReplyToMessage.From != nil &&
			update.Message.ReplyToMessage.From.ID == bot.Self.ID
		randomReply := rng.Intn(7) == 0
		if !tagged && !replyToBot && !randomReply {
			continue
		}
		trigger := "tag"
		if replyToBot {
			trigger = "reply_to_bot"
		} else if randomReply {
			trigger = "random_1_in_10"
		}
		userID := int64(0)
		if update.Message.From != nil {
			userID = update.Message.From.ID
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
		logInfof(
			"incoming message chat_id=%d msg_id=%d trigger=%s user_id=%d text=%q",
			update.Message.Chat.ID,
			update.Message.MessageID,
			trigger,
			userID,
			shorten(promptText, 160),
		)

		stopTyping := startTypingIndicator(bot, update.Message.Chat.ID, update.Message.MessageID)
		replyText, err := askChatGPT(promptText, openAIAPIKey, openAIModel, openAISystemPrompt)
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

		msg := tgbotapi.NewMessage(update.Message.Chat.ID, replyText)
		msg.ReplyToMessageID = update.Message.MessageID

		delay := randomDelay(delayMinMS, delayMaxMS, rng)
		logInfof(
			"delaying response chat_id=%d reply_to=%d delay_ms=%d",
			update.Message.Chat.ID,
			update.Message.MessageID,
			delay.Milliseconds(),
		)
		time.Sleep(delay)

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

func askChatGPT(userText, apiKey, model, systemPrompt string) (string, error) {
	reqBody := map[string]any{
		"model":        model,
		"instructions": systemPrompt,
		"input": []map[string]any{
			{
				"role": "user",
				"content": []map[string]string{
					{
						"type": "input_text",
						"text": userText,
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

func getEnvInt(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		logErrorf("invalid %s=%q, using default %d", key, raw, fallback)
		return fallback
	}
	return value
}

func randomDelay(minMS, maxMS int, rng *rand.Rand) time.Duration {
	if maxMS <= minMS {
		return time.Duration(minMS) * time.Millisecond
	}
	return time.Duration(minMS+rng.Intn(maxMS-minMS+1)) * time.Millisecond
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
