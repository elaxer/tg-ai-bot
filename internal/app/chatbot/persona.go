package chatbot

import (
	"context"
	"errors"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

const (
	personaCommandClear personaCommandAction = "clear"
	personaCommandShow  personaCommandAction = "show"
)

var errHistoryStoreUnavailable = errors.New("history store unavailable")

type personaCommandAction string

func (p *Processor) handlePersonaCommand(msg *tgbotapi.Message) bool {
	if msg == nil || msg.From == nil {
		return false
	}
	text := strings.TrimSpace(getMessageText(msg))
	if text == "" {
		return false
	}
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return false
	}
	cmdToken, args, action, ok := parsePersonaCommand(text)
	if !ok {
		return false
	}

	switch cmdToken {
	case "/persona", "!persona":
		p.processPersonaSetCommand(msg, args)
	case "/persona_clear", "!persona_clear":
		p.processPersonaControlCommand(msg, action)
	case "/persona_show", "!persona_show":
		p.processPersonaControlCommand(msg, action)
	}

	return true
}

func parsePersonaCommand(text string) (cmdToken string, args string, action personaCommandAction, ok bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", "", "", false
	}
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return "", "", "", false
	}
	cmdToken = strings.ToLower(fields[0])
	if idx := strings.Index(cmdToken, "@"); idx >= 0 {
		cmdToken = cmdToken[:idx]
	}

	switch cmdToken {
	case "/persona", "!persona":
		return cmdToken, strings.TrimSpace(text[len(fields[0]):]), "", true
	case "/persona_clear", "!persona_clear":
		return cmdToken, "", personaCommandClear, true
	case "/persona_show", "!persona_show":
		return cmdToken, "", personaCommandShow, true
	default:
		return "", "", "", false
	}
}

func (p *Processor) processPersonaSetCommand(msg *tgbotapi.Message, args string) {
	traceID := newTraceID(msg.Chat.ID, msg.MessageID)
	senderInfo := extractSenderInfo(msg)
	if args == "" {
		p.sendSystemMessage(msg, "Usage: /persona <instructions>", traceID)

		return
	}

	response := p.executeSetPersona(senderInfo, args, traceID)
	p.sendSystemMessage(msg, response, traceID)
}

func (p *Processor) processPersonaControlCommand(msg *tgbotapi.Message, action personaCommandAction) {
	traceID := newTraceID(msg.Chat.ID, msg.MessageID)
	senderInfo := extractSenderInfo(msg)

	response := ""
	switch action {
	case personaCommandClear:
		response = p.executeClearPersona(senderInfo.ID, traceID)
	case personaCommandShow:
		response = p.executeShowPersona(senderInfo.ID)
	}

	p.sendSystemMessage(msg, response, traceID)
}

func (p *Processor) executeClearPersona(userID int64, traceID string) string {
	if err := p.clearUserPersona(userID); err != nil {
		logError("persona clear failed", "trace_id", traceID, "user_id", userID, "err", err)

		return "Failed to clear persona."
	}

	return "Persona cleared. I'll use the default style again."
}

func (p *Processor) executeShowPersona(userID int64) string {
	persona := p.getUserPersona(userID)
	if persona == "" {
		return "You don't have a persona set."
	}

	return "Your persona:\n" + persona
}

func (p *Processor) executeSetPersona(info SenderInfo, persona, traceID string) string {
	if err := p.setUserPersona(info, persona); err != nil {
		logError("persona update failed", "trace_id", traceID, "user_id", info.ID, "err", err)

		return "Failed to save persona."
	}

	return "Persona saved. I'll follow it in future replies."
}

func (p *Processor) sendSystemMessage(msg *tgbotapi.Message, text string, traceID string) {
	text = strings.TrimSpace(text)
	if text == "" || p.bot == nil {
		return
	}
	out := tgbotapi.NewMessage(msg.Chat.ID, text)
	if msg.Chat.Type != "private" {
		out.ReplyToMessageID = msg.MessageID
	}
	if _, err := p.bot.Send(out); err != nil {
		logError("system message send failed", "trace_id", traceID, "err", err)
	}
}

func (p *Processor) setUserPersona(info SenderInfo, persona string) error {
	if p.history == nil {
		return errHistoryStoreUnavailable
	}
	name := strings.TrimSpace(info.Username)
	if name == "" {
		name = strings.TrimSpace(info.DisplayName)
	}

	return p.history.SetPersona(context.Background(), info.ID, name, persona)
}

func (p *Processor) clearUserPersona(userID int64) error {
	if p.history == nil {
		return errHistoryStoreUnavailable
	}

	return p.history.ClearPersona(context.Background(), userID)
}

func (p *Processor) getUserPersona(userID int64) string {
	if p.history == nil {
		return ""
	}
	persona, err := p.history.Persona(context.Background(), userID)
	if err != nil {
		logError("load persona failed", "user_id", userID, "err", err)

		return ""
	}

	return persona
}
