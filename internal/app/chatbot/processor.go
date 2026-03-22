package chatbot

import (
	"math/rand"
	"strings"

	"github.com/elaxer/tg-ai-bot/internal/config"
	"github.com/elaxer/tg-ai-bot/internal/infra/openai"
	"github.com/elaxer/tg-ai-bot/internal/storage/history"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func NewProcessor(
	bot *tgbotapi.BotAPI,
	cfg config.Bot,
	runtime config.Runtime,
	openaiClient *openai.Client,
	rng *rand.Rand,
	historyStore *history.Store,
) *Processor {
	runtime.BotTag = "@" + strings.ToLower(bot.Self.UserName)
	p := &Processor{
		bot:       bot,
		cfg:       cfg,
		runtime:   runtime,
		openai:    openaiClient,
		rng:       rng,
		reactions: append([]string(nil), cfg.Reactions...),
		history:   historyStore,
	}

	return p
}

func (p *Processor) HandleUpdate(update tgbotapi.Update) {
	if msg := update.Message; msg != nil {
		p.processIncomingMessage(msg)
	}
}

func (p *Processor) processIncomingMessage(msg *tgbotapi.Message) {
	if p.shouldIgnoreMessage(msg) {
		return
	}
	if msg.From != nil {
		p.ensureUserRecord(msg.From)
	}
	if p.handlePersonaCommand(msg) {
		return
	}

	isPrivate := msg.Chat.Type == chatTypePrivate

	traceID := newTraceID(msg.Chat.ID, msg.MessageID)
	p.maybeReactToMessage(msg, traceID)

	decision, ok := p.buildResponseDecision(msg, isPrivate)
	if !ok {
		return
	}
	p.respondToDecision(traceID, msg, decision)
}

func (p *Processor) maybeReactToMessage(msg *tgbotapi.Message, traceID string) {
	if len(p.reactions) == 0 || p.rng.Float64() >= p.cfg.ReactionChance {
		return
	}
	emoji := p.reactions[p.rng.Intn(len(p.reactions))]
	if err := p.setMessageReaction(msg.Chat.ID, msg.MessageID, emoji); err != nil {
		logError("set reaction failed", "trace_id", traceID, "err", err)

		return
	}
	logInfo("reacted to message", "trace_id", traceID, "emoji", emoji)
}
