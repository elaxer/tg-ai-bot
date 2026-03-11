package chatbot

import (
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func (p *Processor) shouldIgnoreMessage(msg *tgbotapi.Message) bool {
	if msg == nil {
		return true
	}
	if strings.TrimSpace(msg.Text) == "" && strings.TrimSpace(msg.Caption) == "" && len(msg.Photo) == 0 {
		return true
	}
	if msg.From != nil && msg.From.ID == p.bot.Self.ID {
		return true
	}
	chatType := msg.Chat.Type
	if chatType != "private" && chatType != "group" && chatType != "supergroup" {
		return true
	}
	return false
}

func (p *Processor) buildResponseDecision(msg *tgbotapi.Message, isPrivate bool) (ResponseDecision, bool) {
	if isPrivate {
		return p.buildPrivateDecision(msg)
	}
	return p.buildGroupDecision(msg)
}

func (p *Processor) buildPrivateDecision(msg *tgbotapi.Message) (ResponseDecision, bool) {
	decision := ResponseDecision{
		Trigger:             TriggerPrivate,
		Sender:              extractSenderInfo(msg),
		PhotoFileID:         extractBestPhotoFileID(msg),
		PromptText:          strings.TrimSpace(getMessageText(msg)),
		ConversationContext: p.getChatHistoryContext(msg.Chat.ID),
	}
	decision.SenderPersona = p.getUserPersona(decision.Sender.ID)
	if msg.ReplyToMessage != nil {
		decision.ReplyContext = buildReplyContext(msg.ReplyToMessage)
	}
	decision.ShouldSendSticker = decision.PhotoFileID == "" &&
		len(p.cfg.StickerFileIDs) > 0 &&
		p.rng.Float64() < p.cfg.RandomStickerChance

	return decision, true
}

func (p *Processor) buildGroupDecision(msg *tgbotapi.Message) (ResponseDecision, bool) {
	messageText := getMessageText(msg)
	tagged := strings.Contains(strings.ToLower(messageText), p.runtime.BotTag)
	replyToBot := msg.ReplyToMessage != nil &&
		msg.ReplyToMessage.From != nil &&
		msg.ReplyToMessage.From.ID == p.bot.Self.ID

	randomReply := p.rng.Float64() < p.cfg.RandomReplyChance

	if !tagged && !replyToBot && !randomReply {
		return ResponseDecision{}, false
	}

	decision := ResponseDecision{
		Sender:              extractSenderInfo(msg),
		PhotoFileID:         extractBestPhotoFileID(msg),
		PromptText:          extractPromptText(messageText, p.runtime.BotTag, tagged),
		ConversationContext: p.getChatHistoryContext(msg.Chat.ID),
	}

	decision.Trigger = TriggerTag
	if replyToBot {
		decision.Trigger = TriggerReplyToBot
	} else if randomReply {
		decision.Trigger = TriggerRandom
	}

	decision.SenderPersona = p.getUserPersona(decision.Sender.ID)
	if msg.ReplyToMessage != nil {
		decision.ReplyContext = buildReplyContext(msg.ReplyToMessage)
	}
	decision.ShouldSendSticker = decision.PhotoFileID == "" &&
		len(p.cfg.StickerFileIDs) > 0 &&
		p.rng.Float64() < p.cfg.RandomStickerChance

	return decision, true
}
