package chatbot

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/elaxer/tg-ai-bot/internal/infra/openai"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func (p *Processor) respondToDecision(traceID string, msg *tgbotapi.Message, decision ResponseDecision) {
	if decision.PromptText == "" && decision.PhotoFileID == "" {
		p.logEmptyPrompt(traceID, msg, decision)

		return
	}

	p.recordUserTurn(msg, decision)

	shouldSendTTS := !decision.ShouldSendSticker && p.rng.Float64() < p.cfg.TTSReplyChance
	stopIndicator := p.beginIndicator(decision, msg, traceID, shouldSendTTS)
	defer stopIndicator()

	p.logIncomingMessage(traceID, msg, decision)

	replyText, imageURL := p.resolveReply(decision, traceID)
	p.logImageMessage(traceID, imageURL)

	p.sleepBeforeResponse(traceID, msg, decision)

	if decision.ShouldSendSticker {
		p.sendStickerReply(msg, traceID)

		return
	}

	if p.tryVoiceReply(shouldSendTTS, replyText, msg, traceID) {
		return
	}

	if _, ok := p.sendTextReply(msg, replyText, traceID); ok {
		p.appendChatHistory(msg.Chat.ID, chatRoleAssistant, replyText)
	}
}

func (p *Processor) recordUserTurn(msg *tgbotapi.Message, decision ResponseDecision) {
	if strings.TrimSpace(decision.PromptText) != "" {
		p.appendChatHistory(msg.Chat.ID, chatRoleUser, formatUserTurn(decision.Sender, decision.PromptText))
	}
}

func (p *Processor) resolveReply(decision ResponseDecision, traceID string) (string, string) {
	if decision.ShouldSendSticker {
		return "", ""
	}

	reply, imageURL, err := p.generateModelReply(decision)
	if err != nil {
		logError("model request failed", "trace_id", traceID, "err", err)

		return "I couldn't generate a response right now.", imageURL
	}

	return reply, imageURL
}

func (p *Processor) generateModelReply(decision ResponseDecision) (string, string, error) {
	input := openai.ReplyInput{
		MessageText:         decision.PromptText,
		SenderID:            decision.Sender.ID,
		SenderUsername:      decision.Sender.Username,
		SenderDisplayName:   decision.Sender.DisplayName,
		ReplyContext:        decision.ReplyContext,
		ConversationContext: decision.ConversationContext,
		SenderPersona:       decision.SenderPersona,
	}
	imageURL := ""

	if decision.PhotoFileID != "" {
		resolvedURL, err := p.bot.GetFileDirectURL(decision.PhotoFileID)
		if err != nil {
			return "", "", fmt.Errorf("telegram file url: %w", err)
		}
		imageURL = resolvedURL
		input.ImageURL = imageURL
		if strings.TrimSpace(input.MessageText) == "" {
			input.MessageText = "Please react on this image."
		}
	}

	replyText, err := p.openai.GenerateReply(context.Background(), input)

	return replyText, imageURL, err
}

func (p *Processor) sleepBeforeResponse(traceID string, msg *tgbotapi.Message, decision ResponseDecision) {
	delay := p.determineResponseDelay(decision, msg)
	logInfo("delaying response", "trace_id", traceID, "delay_ms", delay.Milliseconds())
	time.Sleep(delay)
}

func (p *Processor) determineResponseDelay(decision ResponseDecision, msg *tgbotapi.Message) time.Duration {
	if decision.ShouldSendSticker {
		return p.randomStickerDelay()
	}

	return delayFromMessageLength(getMessageText(msg))
}

func (p *Processor) randomStickerDelay() time.Duration {
	if stickerDelayMax <= stickerDelayMin {
		return stickerDelayMin
	}
	span := stickerDelayMax - stickerDelayMin

	return stickerDelayMin + time.Duration(p.rng.Int63n(int64(span)+1))
}

func (p *Processor) beginIndicator(
	decision ResponseDecision,
	msg *tgbotapi.Message,
	traceID string,
	shouldSendTTS bool,
) func() {
	var stop func()
	switch {
	case decision.ShouldSendSticker:
		stop = p.startChoosingStickerIndicator(msg.Chat.ID, msg.MessageID, traceID)
	case shouldSendTTS:
		stop = p.startRecordingVoiceIndicator(msg.Chat.ID, msg.MessageID, traceID)
	default:
		stop = p.startTypingIndicator(msg.Chat.ID, msg.MessageID, traceID)
	}

	return onceStop(stop)
}

func (p *Processor) sendStickerReply(msg *tgbotapi.Message, traceID string) bool {
	fileID := p.cfg.StickerFileIDs[p.rng.Intn(len(p.cfg.StickerFileIDs))]
	sticker := tgbotapi.NewSticker(msg.Chat.ID, tgbotapi.FileID(fileID))
	if msg.Chat.Type != chatTypePrivate {
		sticker.ReplyToMessageID = msg.MessageID
	}

	sent, err := p.bot.Send(sticker)
	if err != nil {
		logError("sticker send failed", "trace_id", traceID, "err", err)

		return false
	}
	p.logSentResponse("sticker", traceID, sent, fileID)

	return true
}

func (p *Processor) tryVoiceReply(
	shouldSendTTS bool,
	replyText string,
	msg *tgbotapi.Message,
	traceID string,
) bool {
	if !shouldSendTTS || strings.TrimSpace(replyText) == "" {
		return false
	}
	audioData, err := p.openai.GenerateSpeech(context.Background(), replyText)
	if err != nil {
		logError("tts request failed", "trace_id", traceID, "err", err)

		return false
	}
	if _, ok := p.sendVoiceReply(msg, audioData, traceID, replyText); ok {
		p.appendChatHistory(msg.Chat.ID, chatRoleAssistant, replyText)

		return true
	}

	return false
}

func (p *Processor) sendTextReply(msg *tgbotapi.Message, replyText string, traceID string) (int, bool) {
	outMsg := tgbotapi.NewMessage(msg.Chat.ID, replyText)
	if msg.Chat.Type != chatTypePrivate {
		outMsg.ReplyToMessageID = msg.MessageID
	}

	sent, err := p.bot.Send(outMsg)
	if err != nil {
		logError("text send failed", "trace_id", traceID, "err", err)

		return 0, false
	}
	p.logSentResponse("message", traceID, sent, outMsg.Text)

	return sent.MessageID, true
}

func (p *Processor) sendVoiceReply(msg *tgbotapi.Message, audioData []byte, traceID, replyText string) (int, bool) {
	voice := tgbotapi.NewVoice(msg.Chat.ID, tgbotapi.FileBytes{
		Name:  "reply.ogg",
		Bytes: audioData,
	})
	if msg.Chat.Type != chatTypePrivate {
		voice.ReplyToMessageID = msg.MessageID
	}

	sent, err := p.bot.Send(voice)
	if err != nil {
		logError("voice send failed", "trace_id", traceID, "err", err)

		return 0, false
	}
	p.logSentResponse("voice", traceID, sent, replyText)

	return sent.MessageID, true
}

func (p *Processor) startTypingIndicator(chatID int64, replyTo int, traceID string) func() {
	return p.startChatActionIndicator(chatID, replyTo, tgbotapi.ChatTyping, "typing", traceID)
}

func (p *Processor) startRecordingVoiceIndicator(chatID int64, replyTo int, traceID string) func() {
	return p.startChatActionIndicator(chatID, replyTo, tgbotapi.ChatRecordVoice, "record_voice", traceID)
}

func (p *Processor) startChoosingStickerIndicator(chatID int64, replyTo int, traceID string) func() {
	return p.startChatActionIndicator(chatID, replyTo, "choose_sticker", "choose_sticker", traceID)
}

func (p *Processor) startChatActionIndicator(chatID int64, replyTo int, action, actionName, traceID string) func() {
	_ = replyTo
	stop := make(chan struct{})
	go func() {
		ticker := time.NewTicker(4 * time.Second)
		defer ticker.Stop()
		p.sendChatAction(chatID, action, actionName, traceID)
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				p.sendChatAction(chatID, action, actionName, traceID)
			}
		}
	}()

	return func() { close(stop) }
}

func (p *Processor) sendChatAction(chatID int64, action, actionName, traceID string) {
	if _, err := p.bot.Request(tgbotapi.NewChatAction(chatID, action)); err != nil {
		logError("chat action failed", "trace_id", traceID, "action", actionName, "err", err)
	}
}

func (p *Processor) setMessageReaction(chatID int64, messageID int, emoji string) error {
	reaction := fmt.Sprintf(`[{"type":"emoji","emoji":%q}]`, emoji)
	_, err := p.bot.MakeRequest(
		"setMessageReaction",
		tgbotapi.Params{
			"chat_id":    strconv.FormatInt(chatID, 10),
			"message_id": strconv.Itoa(messageID),
			"reaction":   reaction,
		},
	)

	return err
}

func (p *Processor) logEmptyPrompt(traceID string, msg *tgbotapi.Message, decision ResponseDecision) {
	logInfo("skip empty prompt", "trace_id", traceID, "chat_id", msg.Chat.ID, "trigger", decision.Trigger)
}

func (p *Processor) logIncomingMessage(traceID string, msg *tgbotapi.Message, decision ResponseDecision) {
	logInfo(
		"incoming message",
		"trace_id", traceID,
		"chat_id", msg.Chat.ID,
		"trigger", decision.Trigger,
		"text", shorten(decision.PromptText, 160),
	)
}

func (p *Processor) logImageMessage(traceID string, imageURL string) {
	if imageURL != "" {
		logInfo("image message", "trace_id", traceID, "image_url", imageURL)
	}
}

func (p *Processor) logSentResponse(respType, traceID string, sent tgbotapi.Message, data any) {
	logInfo("sent "+respType, "trace_id", traceID, "chat_id", sent.Chat.ID, "msg_id", sent.MessageID, "data", data)
}

func onceStop(fn func()) func() {
	var once sync.Once

	return func() {
		once.Do(fn)
	}
}
