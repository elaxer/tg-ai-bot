package chatbot

import (
	"context"
	"fmt"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func (p *Processor) appendChatHistory(chatID int64, role, text string) {
	text = strings.TrimSpace(text)
	if text == "" || p.history == nil {
		return
	}
	if err := p.history.Append(context.Background(), chatID, role, text); err != nil {
		logError("append chat history failed", "chat_id", chatID, "role", role, "err", err)
	}
}

func (p *Processor) getChatHistoryContext(chatID int64) string {
	if p.history == nil {
		return ""
	}
	turns, err := p.history.Recent(context.Background(), chatID, chatHistoryMaxTurns)
	if err != nil {
		logError("load chat history failed", "chat_id", chatID, "err", err)
		return ""
	}
	if len(turns) == 0 {
		return ""
	}
	var builder strings.Builder
	for _, turn := range turns {
		builder.WriteString(turn.Role)
		builder.WriteString(": ")
		builder.WriteString(turn.Text)
		builder.WriteByte('\n')
	}
	return strings.TrimSpace(builder.String())
}

func (p *Processor) ensureUserRecord(user *tgbotapi.User) {
	if p.history == nil || user == nil {
		return
	}
	name := strings.TrimSpace(user.UserName)
	if name == "" {
		name = strings.TrimSpace(strings.TrimSpace(user.FirstName + " " + user.LastName))
	}
	if err := p.history.EnsureUser(context.Background(), user.ID, name); err != nil {
		logError("ensure user record failed", "user_id", user.ID, "err", err)
	}
}

func formatUserTurn(info SenderInfo, text string) string {
	label := getUserLabel(info)
	return fmt.Sprintf("%s: %s", label, strings.TrimSpace(text))
}

func getUserLabel(info SenderInfo) string {
	if strings.TrimSpace(info.Username) != "" {
		return "@" + strings.TrimSpace(info.Username)
	}
	if strings.TrimSpace(info.DisplayName) != "" {
		return strings.TrimSpace(info.DisplayName)
	}
	if info.ID != 0 {
		return fmt.Sprintf("user#%d", info.ID)
	}
	return "user"
}
