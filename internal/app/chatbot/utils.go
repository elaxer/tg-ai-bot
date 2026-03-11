package chatbot

import (
	"fmt"
	"strings"
	"time"

	"telegram-bot/internal/logging"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func chatName(chat *tgbotapi.Chat) string {
	if chat == nil {
		return ""
	}
	if strings.TrimSpace(chat.Title) != "" {
		return chat.Title
	}
	if strings.TrimSpace(chat.UserName) != "" {
		return "@" + chat.UserName
	}
	return ""
}

func newTraceID(chatID int64, msgID int) string {
	return fmt.Sprintf("%d-%d-%d", time.Now().UnixNano(), chatID, msgID)
}

func userLoginForLog(sender SenderInfo) string {
	if strings.TrimSpace(sender.Username) != "" {
		return "@" + strings.TrimSpace(sender.Username)
	}
	if strings.TrimSpace(sender.DisplayName) != "" {
		return strings.TrimSpace(sender.DisplayName)
	}
	if sender.ID != 0 {
		return fmt.Sprintf("id:%d", sender.ID)
	}
	return "unknown"
}

func shorten(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

func logInfo(msg string, kv ...any) {
	logging.Infow(msg, kv...)
}

func logError(msg string, kv ...any) {
	logging.Errorw(msg, kv...)
}

func getMessageText(msg *tgbotapi.Message) string {
	if msg == nil {
		return ""
	}
	if strings.TrimSpace(msg.Text) != "" {
		return msg.Text
	}
	return msg.Caption
}

func extractBestPhotoFileID(msg *tgbotapi.Message) string {
	if len(msg.Photo) == 0 {
		return ""
	}
	return msg.Photo[len(msg.Photo)-1].FileID
}

func extractSenderInfo(msg *tgbotapi.Message) SenderInfo {
	if msg == nil || msg.From == nil {
		return SenderInfo{}
	}
	return SenderInfo{
		ID:          msg.From.ID,
		Username:    strings.TrimSpace(msg.From.UserName),
		DisplayName: strings.TrimSpace(strings.TrimSpace(msg.From.FirstName + " " + msg.From.LastName)),
	}
}

func extractPromptText(text, botTag string, tagged bool) string {
	if tagged {
		text = strings.TrimSpace(strings.ReplaceAll(text, botTag, ""))
	}
	return strings.TrimSpace(text)
}

func buildReplyContext(msg *tgbotapi.Message) string {
	if msg == nil {
		return ""
	}
	replyText := strings.TrimSpace(getMessageText(msg))
	if replyText == "" {
		replyText = "[non-text message]"
	}

	sender := extractSenderInfo(msg)
	replyHandle := "unknown"
	if sender.Username != "" {
		replyHandle = "@" + sender.Username
	}
	replyDisplayName := "unknown"
	if sender.DisplayName != "" {
		replyDisplayName = sender.DisplayName
	}

	return fmt.Sprintf(
		"id=%d, handle=%s, display_name=%q, text=%q",
		sender.ID,
		replyHandle,
		replyDisplayName,
		shorten(replyText, 500),
	)
}

func delayFromMessageLength(message string) time.Duration {
	length := len([]rune(strings.TrimSpace(message)))
	ms := 300 + (length * 45)
	return time.Duration(ms) * time.Millisecond
}
