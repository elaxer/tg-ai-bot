package chatbot

import (
	"context"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func (p *Processor) handleContextCommand(msg *tgbotapi.Message) bool {
	if msg == nil {
		return false
	}
	cmdToken, ok := parseContextCommand(getMessageText(msg))
	if !ok {
		return false
	}

	switch cmdToken {
	case "/context_clear", "!context_clear":
		p.processClearContextCommand(msg)
	case "/context_show", "!context_show":
		p.processShowContextCommand(msg)
		return true
	default:
		return false
	}

	return true
}

func parseContextCommand(text string) (string, bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", false
	}
	fields := strings.Fields(text)
	if len(fields) != 1 {
		return "", false
	}
	cmdToken := strings.ToLower(fields[0])
	if idx := strings.Index(cmdToken, "@"); idx >= 0 {
		cmdToken = cmdToken[:idx]
	}

	switch cmdToken {
	case "/context_clear", "!context_clear":
		return cmdToken, true
	case "/context_show", "!context_show":
		return cmdToken, true
	default:
		return "", false
	}
}

func (p *Processor) processClearContextCommand(msg *tgbotapi.Message) {
	traceID := newTraceID(msg.Chat.ID, msg.MessageID)
	response := p.executeClearContext(msg.Chat.ID, traceID)
	p.sendSystemMessage(msg, response, traceID)
}

func (p *Processor) processShowContextCommand(msg *tgbotapi.Message) {
	traceID := newTraceID(msg.Chat.ID, msg.MessageID)
	response := p.executeShowContext(msg.Chat.ID)
	p.sendSystemMessage(msg, response, traceID)
}

func (p *Processor) executeClearContext(chatID int64, traceID string) string {
	if err := p.clearChatContext(chatID); err != nil {
		logError("context clear failed", "trace_id", traceID, "chat_id", chatID, "err", err)

		return "Failed to clear context."
	}

	return "Context cleared for this chat."
}

func (p *Processor) executeShowContext(chatID int64) string {
	return formatContextShowMessage(p.getChatHistoryContext(chatID))
}

func formatContextShowMessage(contextText string) string {
	contextText = strings.TrimSpace(contextText)
	if contextText == "" {
		return "Context is empty for this chat."
	}

	return "Current context:\n" + contextText
}

func (p *Processor) clearChatContext(chatID int64) error {
	if p.history == nil {
		return errHistoryStoreUnavailable
	}

	return p.history.ClearChat(context.Background(), chatID)
}
